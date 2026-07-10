package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/job"
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

func TestLSClosedMultilineHistoryCannotBuryCurrentJob(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	writeJobMeta(t, e, &job.Meta{
		ID:      "job-96",
		Agent:   "claude",
		Model:   "opus",
		Task:    strings.Repeat("historical closed job line one\nline two with lots of text ", 80),
		State:   job.StateClosed,
		Created: now.Add(-2 * time.Hour),
		Updated: now.Add(-90 * time.Minute),
	})
	writeJobMeta(t, e, &job.Meta{
		ID:        "job-144",
		Run:       "active wave",
		Agent:     "codex",
		Model:     "gpt-5",
		Task:      "newer active task\nwith multiline prompt that must collapse",
		Workspace: "ws-9",
		State:     job.StateActive,
		RunnerPID: os.Getpid(),
		Created:   now.Add(-10 * time.Minute),
		Updated:   now.Add(-1 * time.Minute),
	})

	out := e.legwork(t, "ls")
	lines := nonEmptyLines(out)
	if len(lines) != 1 {
		t.Fatalf("default ls should show one current job line, got %d:\n%s", len(lines), out)
	}
	if strings.Contains(out, "job-96") || strings.Contains(out, "historical closed") {
		t.Fatalf("closed multiline history leaked into default ls:\n%s", out)
	}
	line := lines[0]
	for _, want := range []string{"job-144", "codex/gpt-5", "active", "ws-9", "[active wave]", "newer active task"} {
		if !strings.Contains(line, want) {
			t.Fatalf("ls line missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "\n") || len([]rune(line)) > 100 {
		t.Fatalf("ls line should be one clipped terminal line (%d runes):\n%s", len([]rune(line)), line)
	}
}

func TestLSCollapsesAllHumanFieldsToOneLine(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	writeJobMeta(t, e, &job.Meta{
		ID:        "job-10000",
		Agent:     "codex\nworker",
		Model:     "long\nmodel-name-that-needs-clipping",
		Workspace: "ws-1000",
		Task:      "current task",
		State:     job.StateDone,
		Created:   now.Add(-time.Minute),
		Updated:   now,
	})

	out := e.legwork(t, "ls")
	lines := nonEmptyLines(out)
	if len(lines) != 1 || strings.Contains(lines[0], "\n") || len([]rune(lines[0])) > 100 {
		t.Fatalf("ls must render all persisted text as one clipped line:\n%s", out)
	}
	for _, selector := range []string{"job-10000", "ws-1000"} {
		if !strings.Contains(lines[0], selector) {
			t.Fatalf("ls must retain copy-pasteable selector %q:\n%s", selector, out)
		}
	}
}

func TestLSAttentionSortFiltersAndErrors(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	writeJobMeta(t, e, &job.Meta{ID: "job-1", Run: "alpha", Agent: "claude", Task: "done alpha", Workspace: "ws-1", State: job.StateDone, Created: now.Add(-50 * time.Minute), Updated: now.Add(-1 * time.Minute)})
	writeJobMeta(t, e, &job.Meta{ID: "job-2", Run: "beta", Agent: "codex", Task: "active beta", Workspace: "ws-1", State: job.StateActive, RunnerPID: os.Getpid(), Created: now.Add(-40 * time.Minute), Updated: now.Add(-3 * time.Minute)})
	writeJobMeta(t, e, &job.Meta{ID: "job-3", Run: "alpha", Agent: "claude", Task: "needs alpha", Workspace: "ws-2", State: job.StateNeedsInput, Created: now.Add(-30 * time.Minute), Updated: now.Add(-20 * time.Minute)})
	writeJobMeta(t, e, &job.Meta{ID: "job-4", Run: "alpha", Agent: "claude", Task: "newer blocked alpha", Workspace: "ws-2", State: job.StateBlocked, Created: now.Add(-20 * time.Minute), Updated: now.Add(-10 * time.Minute)})
	writeJobMeta(t, e, &job.Meta{ID: "job-5", Run: "alpha", Agent: "claude", Task: "closed alpha", Workspace: "ws-1", State: job.StateClosed, Created: now.Add(-10 * time.Minute), Updated: now})

	out := e.legwork(t, "ls")
	lines := nonEmptyLines(out)
	if got := idsFromLS(lines); strings.Join(got, ",") != "job-4,job-3,job-2,job-1" {
		t.Fatalf("attention/newest ordering wrong: %v\n%s", got, out)
	}
	if strings.Contains(out, "job-5") {
		t.Fatalf("default ls should hide closed jobs:\n%s", out)
	}
	if got := jsonLSIDs(t, e); strings.Join(got, ",") != "job-4,job-3,job-2,job-1" {
		t.Fatalf("default ls --json should hide closed jobs and preserve sorting: %v", got)
	}

	ws := e.legwork(t, "ls", "--workspace", "ws-1")
	if got := idsFromLS(nonEmptyLines(ws)); strings.Join(got, ",") != "job-2,job-1" {
		t.Fatalf("--workspace filter wrong: %v\n%s", got, ws)
	}
	if got := jsonLSIDs(t, e, "--workspace", "ws-1"); strings.Join(got, ",") != "job-2,job-1" {
		t.Fatalf("--workspace --json filter wrong: %v", got)
	}
	runState := e.legwork(t, "ls", "--run", "alpha", "--state", "done,blocked")
	if got := idsFromLS(nonEmptyLines(runState)); strings.Join(got, ",") != "job-4,job-1" {
		t.Fatalf("--run/--state filters wrong: %v\n%s", got, runState)
	}
	if got := jsonLSIDs(t, e, "--run", "alpha", "--state", "done,blocked"); strings.Join(got, ",") != "job-4,job-1" {
		t.Fatalf("--run/--state --json filters wrong: %v", got)
	}
	allLimit := e.legwork(t, "ls", "--all", "--limit", "5")
	if !strings.Contains(allLimit, "job-5") {
		t.Fatalf("--all should include closed history:\n%s", allLimit)
	}
	if got := jsonLSIDs(t, e, "--all", "--limit", "2"); strings.Join(got, ",") != "job-4,job-3" {
		t.Fatalf("--all/--limit --json should apply after sorting: %v", got)
	}
	if got := jsonLSIDs(t, e, "--limit", "0"); strings.Join(got, ",") != "job-4,job-3,job-2,job-1" {
		t.Fatalf("--limit 0 should mean unlimited: %v", got)
	}

	if got := jsonLSIDs(t, e, "--state", "closed"); strings.Join(got, ",") != "job-5" {
		t.Fatalf("--state --json should query closed history directly: %v", got)
	}
	if empty := strings.TrimSpace(e.legwork(t, "ls", "--json", "--state", "failed")); empty != "[]" {
		t.Fatalf("empty ls --json should emit []: %q", empty)
	}
	if empty := strings.TrimSpace(e.legwork(t, "ls", "--workspace", "ws-missing")); empty != "no jobs match filters" {
		t.Fatalf("empty filtered ls should explain that filters matched nothing: %q", empty)
	}
	if errOut, err := e.legworkErr("ls", "--state", "missing"); err == nil || !strings.Contains(errOut, "invalid state") {
		t.Fatalf("invalid state should fail noninteractively, err=%v out=%s", err, errOut)
	}
	if errOut, err := e.legworkErr("ls", "--limit", "-1"); err == nil || !strings.Contains(errOut, "--limit must be non-negative") {
		t.Fatalf("negative limit should fail noninteractively, err=%v out=%s", err, errOut)
	}
}

func writeJobMeta(t *testing.T, e *env, m *job.Meta) {
	t.Helper()
	dir := filepath.Join(e.state, "jobs", m.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func idsFromLS(lines []string) []string {
	var ids []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			ids = append(ids, fields[0])
		}
	}
	return ids
}

func jsonLSIDs(t *testing.T, e *env, args ...string) []string {
	t.Helper()
	args = append([]string{"ls", "--json"}, args...)
	var metas []map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, args...)), &metas); err != nil {
		t.Fatalf("ls --json: %v", err)
	}
	ids := make([]string, len(metas))
	for i, meta := range metas {
		ids[i], _ = meta["id"].(string)
	}
	return ids
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
	out := e.legwork(t, "tail", id, "--until-idle", "-n", "0")
	elapsed := time.Since(start)
	if elapsed < 500*time.Millisecond {
		t.Fatalf("tail --until-idle returned too early (%v) — did not wait for the turn:\n%s", elapsed, out)
	}
	if !strings.Contains(out, "finished") {
		t.Fatalf("tail --until-idle should have drained the finished event:\n%s", out)
	}
	// The runner owns terminal meta writes. Tail's repeated liveness checks
	// must reload that file rather than reconciling an old active snapshot over
	// the final state and result.
	final := e.waitState(t, id, "done")
	if final["result"] != "finished" {
		t.Fatalf("tail overwrote the runner's terminal result: %+v", final)
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
