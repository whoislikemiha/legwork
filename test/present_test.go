package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runWithConfig dispatches a run with an explicit LEGWORK_CONFIG (so a test can
// set a [health] threshold) and returns the job ID.
func (e *env) runWithConfig(t *testing.T, cfgPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, append([]string{"run", "--agent", "fake"}, args...)...)
	env := append(os.Environ(), "LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script)
	if cfgPath != "" {
		env = append(env, "LEGWORK_CONFIG="+cfgPath)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRunsRollupAndNoteSurfacing(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultWithContext) // cost 0.05, context 145200
	id := e.runWithConfig(t, "", "--run", "pipe", "phase one")
	e.waitState(t, id, "done")
	// A no-run job collapses into the (no run) line.
	e.writeScript(t, resultDone)
	id2 := e.runWithConfig(t, "", "loose job")
	e.waitState(t, id2, "done")

	e.legwork(t, "note", "pipe", "shipped clean, smoke green")

	// Plain output: the run, its rollup, and the note preview.
	out := e.legwork(t, "runs")
	for _, want := range []string{"pipe", "done", "$0.05", "shipped clean, smoke green", "(no run)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("runs output missing %q:\n%s", want, out)
		}
	}

	// JSON: an array of rollups with the documented fields.
	var rollups []map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "runs", "--json")), &rollups); err != nil {
		t.Fatalf("runs --json not an array: %v", err)
	}
	var pipe map[string]any
	for _, r := range rollups {
		if r["label"] == "pipe" {
			pipe = r
		}
	}
	if pipe == nil {
		t.Fatalf("pipe rollup absent:\n%v", rollups)
	}
	if pipe["last_note"] != "shipped clean, smoke green" {
		t.Fatalf("last_note not surfaced: %v", pipe["last_note"])
	}
	jobs, _ := pipe["jobs"].(map[string]any)
	if jobs["done"].(float64) != 1 {
		t.Fatalf("pipe should have 1 done job: %v", jobs)
	}
	if pipe["cost_usd"].(float64) < 0.049 {
		t.Fatalf("pipe cost wrong: %v", pipe["cost_usd"])
	}
}

func TestRunsContextHighMarker(t *testing.T) {
	e := newEnv(t)
	// Threshold below the fixture's 145200 context -> the run trips CTX "!".
	cfg := e.state + "-health.toml"
	if err := os.WriteFile(cfg, []byte("[health]\ncontext_threshold = 100000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, resultWithContext)
	id := e.runWithConfig(t, cfg, "--run", "hot", "heavy turn")
	e.waitState(t, id, "done")

	out := runWithEnv(t, e, cfg, "runs")
	// Find the hot line; it must carry the "!" health marker.
	var hotLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "hot") {
			hotLine = ln
		}
	}
	if hotLine == "" || !strings.Contains(hotLine, "!") {
		t.Fatalf("hot run should show CTX ! marker:\n%s", out)
	}

	var rollups []map[string]any
	_ = json.Unmarshal([]byte(runWithEnv(t, e, cfg, "runs", "--json")), &rollups)
	if len(rollups) == 0 || rollups[0]["context_high"] != true {
		t.Fatalf("context_high should be true in json:\n%v", rollups)
	}
}

// runWithEnv runs a read-only verb with an explicit config path.
func runWithEnv(t *testing.T, e *env, cfgPath string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "LEGWORK_STATE_DIR="+e.state,
		"LEGWORK_FAKE_SCRIPT="+e.script, "LEGWORK_CONFIG="+cfgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestTailBackfill(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working now"}]}}`,
		resultDone,
	)
	id := e.runWithConfig(t, "", "backfill task")
	e.waitState(t, id, "done")

	// -n 2: only the last two curated events (text, finished); queued excluded.
	out := e.legwork(t, "tail", "-n", "2", "--until-idle")
	lines := nonEmptyLines(out)
	if len(lines) != 2 {
		t.Fatalf("tail -n 2 should print exactly 2 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "finished") || strings.Contains(out, "queued") {
		t.Fatalf("tail -n 2 backfill window wrong:\n%s", out)
	}

	// -n 30: the whole curated lifecycle, including queued.
	full := e.legwork(t, "tail", "-n", "30", "--until-idle")
	for _, want := range []string{"queued", "started", "finished", "working now"} {
		if !strings.Contains(full, want) {
			t.Fatalf("tail -n 30 missing %q:\n%s", want, full)
		}
	}
	// finished line carries the telemetry summary from meta, not the raw result.
	if !strings.Contains(full, "done · $") {
		t.Fatalf("finished line should show state · cost · ctx:\n%s", full)
	}
}

func TestTailUntilIdleWaitsForFinish(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"chugging"}]}}`,
		"#sleep 800",
		resultDone,
	)
	id := e.runWithConfig(t, "", "slow one")

	// tail --until-idle must block until the job leaves active/queued.
	start := time.Now()
	out := e.legwork(t, "tail", "--job", id, "--until-idle", "-n", "0")
	elapsed := time.Since(start)
	if elapsed < 500*time.Millisecond {
		t.Fatalf("tail --until-idle returned too early (%v) — did not wait for the turn:\n%s", elapsed, out)
	}
	if !strings.Contains(out, "finished") {
		t.Fatalf("tail --until-idle should have drained the finished event:\n%s", out)
	}
}

func TestTailUntilIdleEmptyScopeReturns(t *testing.T) {
	e := newEnv(t)
	// No jobs at all: --until-idle must return immediately, exit 0, no hang.
	done := make(chan string, 1)
	go func() { done <- e.legwork(t, "tail", "--until-idle") }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("tail --until-idle hung on an empty state dir")
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}
