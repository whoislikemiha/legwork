// Package e2e is the contract test suite: it builds the real binary and
// drives it exactly as an orchestrator would, fake agent behind it.
package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "legwork-e2e")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "legwork")
	if out, err := exec.Command("go", "build", "-o", binPath, "github.com/whoislikemiha/legwork").CombinedOutput(); err != nil {
		panic(string(out))
	}
	// Suppress the opportunistic auto-gc fork by default: a detached child
	// racing a test's tempdir cleanup is flaky. Tests that exercise auto-gc
	// re-enable it per-command (LEGWORK_NO_AUTO_GC="").
	os.Setenv("LEGWORK_NO_AUTO_GC", "1")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type env struct {
	state  string
	script string
	parser string // LEGWORK_FAKE_PARSER: "" (claude) or "codex"
	config string // LEGWORK_CONFIG path; "" leaves it unset
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{state: t.TempDir(), script: filepath.Join(t.TempDir(), "script")}
}

func (e *env) writeScript(t *testing.T, lines ...string) {
	t.Helper()
	if err := os.WriteFile(e.script, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) legwork(t *testing.T, args ...string) string {
	t.Helper()
	out, err := e.legworkErr(args...)
	if err != nil {
		t.Fatalf("legwork %v: %v\n%s", args, err, out)
	}
	return out
}

func (e *env) legworkErr(args ...string) (string, error) {
	return e.legworkInputErr("", args...)
}

func (e *env) legworkInput(t *testing.T, input string, args ...string) string {
	t.Helper()
	out, err := e.legworkInputErr(input, args...)
	if err != nil {
		t.Fatalf("legwork %v: %v\n%s", args, err, out)
	}
	return out
}

func (e *env) legworkInputErr(input string, args ...string) (string, error) {
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state,
		"LEGWORK_FAKE_SCRIPT="+e.script,
	)
	if e.parser != "" {
		cmd.Env = append(cmd.Env, "LEGWORK_FAKE_PARSER="+e.parser)
	}
	if e.config != "" {
		cmd.Env = append(cmd.Env, "LEGWORK_CONFIG="+e.config)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *env) waitState(t *testing.T, id string, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out := e.legwork(t, "status", id, "--json")
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("bad status json: %v\n%s", err, out)
		}
		if m["state"] == want {
			return m
		}
		if s := m["state"].(string); s != "queued" && s != "active" && s != want {
			t.Fatalf("job %s reached %q while waiting for %q", id, s, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s to reach %s", id, want)
	return nil
}

const resultDone = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.02,"usage":{"input_tokens":10,"output_tokens":5},"session_id":"s1","result":"finished\n\nstate: done"}`

// The runner is detached: the CLI returns immediately while the agent is
// still sleeping, and the job completes with no parent process around.
func TestDetachedHappyPath(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
		"#sleep 700",
		resultDone,
	)
	start := time.Now()
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "do it"))
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("run did not return immediately (%v) — runner is not detached", time.Since(start))
	}
	m := e.waitState(t, id, "done")
	if m["cost_usd"].(float64) != 0.02 || m["session_id"] != "s1" {
		t.Fatalf("telemetry/session not persisted: %+v", m)
	}
	// Events must include the full lifecycle.
	evs := e.legwork(t, "events", id)
	for _, want := range []string{"queued", "started", "text", "usage", "finished"} {
		if !strings.Contains(evs, want) {
			t.Fatalf("events missing %q:\n%s", want, evs)
		}
	}
}

func TestNeedsInputAnswerLoop(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s2"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"s2","result":"state: needs-input\nquestion: postgres or sqlite?"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "add persistence"))
	m := e.waitState(t, id, "needs-input")
	if m["question"] != "postgres or sqlite?" {
		t.Fatalf("question not surfaced: %+v", m)
	}

	// Orchestrator answers; second turn completes.
	e.writeScript(t, resultDone)
	e.legwork(t, "answer", id, "postgres")
	m = e.waitState(t, id, "done")
	// Telemetry accumulates across turns.
	if m["cost_usd"].(float64) < 0.029 {
		t.Fatalf("cost not cumulative: %v", m["cost_usd"])
	}
	evs := e.legwork(t, "events", id)
	if !strings.Contains(evs, "needs-input") || !strings.Contains(evs, "answer") {
		t.Fatalf("question/answer not in event log:\n%s", evs)
	}
}

func TestResultPrintsRawJSONAndRunLatest(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.02,"usage":{"input_tokens":10,"output_tokens":5},"session_id":"s1","result":"line one\nline two\n\nstate: done"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "pipe", "do it"))
	e.waitState(t, id, "done")

	rawResult := func(args ...string) string {
		cmd := exec.Command(binPath, args...)
		cmd.Env = append(os.Environ(), "LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("result %v: %v", args, err)
		}
		return string(out)
	}
	if out := rawResult("result", id); out != "line one\nline two" {
		t.Fatalf("raw result mismatch:\n%q", out)
	}
	if out := rawResult("result", "pipe"); out != "line one\nline two" {
		t.Fatalf("run result mismatch:\n%q", out)
	}

	out := e.legwork(t, "result", id, "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad result json: %v\n%s", err, out)
	}
	if got["job"] != id || got["run"] != "pipe" || got["state"] != "done" || got["result"] != "line one\nline two" {
		t.Fatalf("bad result envelope: %+v", got)
	}
}

func TestResultTurnSelectsRetainedTurn(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"first report\n\nstate: done"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "first"))
	e.waitState(t, id, "done")

	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"second report\n\nstate: done"}`,
	)
	e.legwork(t, "resume", id, "second")
	e.waitState(t, id, "done")

	if out := e.legwork(t, "result", id); out != "second report" {
		t.Fatalf("default result should be latest:\n%q", out)
	}
	if out := e.legwork(t, "result", id, "--turn", "1"); out != "first report" {
		t.Fatalf("turn 1 result mismatch:\n%q", out)
	}
	if out := e.legwork(t, "result", id, "--turn", "2"); out != "second report" {
		t.Fatalf("turn 2 result mismatch:\n%q", out)
	}
	if out, err := e.legworkErr("result", id, "--turn", "3"); err == nil {
		t.Fatalf("missing retained turn must fail:\n%s", out)
	} else if !strings.Contains(out, "turn 3 result is not retained") {
		t.Fatalf("missing turn error should explain retention:\n%s", out)
	}
}

func TestResultTurnReadsCompressedTranscript(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"compressed report\n\nstate: done"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "compress me"))
	e.waitState(t, id, "done")

	cfg := gcConfig(t, "transcript_compress_after = \"0s\"\n")
	e.gcJSON(t, cfg)

	plain := filepath.Join(e.state, "jobs", id, "transcript.jsonl")
	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("plain transcript should be compressed away: %v", err)
	}
	if _, err := os.Stat(plain + ".gz"); err != nil {
		t.Fatalf("compressed transcript missing: %v", err)
	}
	if out := e.legwork(t, "result", id, "--turn", "1"); out != "compressed report" {
		t.Fatalf("compressed transcript result mismatch:\n%q", out)
	}
}

func TestResultTurnReparseKeepsMissingStatusBlocked(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"I totally finished everything!"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "sloppy"))
	e.waitState(t, id, "blocked")

	out := e.legwork(t, "result", id, "--turn", "1", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad result json: %v\n%s", err, out)
	}
	if got["state"] != "blocked" || got["result"] != "I totally finished everything!" {
		t.Fatalf("missing status block did not reparse as blocked: %+v", got)
	}
}

func TestResultTurnUsesCodexParserAcrossTurns(t *testing.T) {
	e := newEnv(t)
	e.parser = "codex"
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-result"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"first codex report\n\nstate: done"}}`,
		codexDone,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "first"))
	e.waitState(t, id, "done")

	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-result"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"second codex report\n\nstate: done"}}`,
		codexDone,
	)
	e.legwork(t, "resume", id, "second")
	e.waitState(t, id, "done")

	if out := e.legwork(t, "result", id, "--turn", "1"); out != "first codex report" {
		t.Fatalf("codex turn 1 result mismatch:\n%q", out)
	}
	if out := e.legwork(t, "result", id, "--turn", "2"); out != "second codex report" {
		t.Fatalf("codex turn 2 result mismatch:\n%q", out)
	}
}

func TestResultRefusesActiveLatestTurn(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, "#sleep 5000", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "slow"))

	if out, err := e.legworkErr("result", id); err == nil {
		t.Fatalf("active job without latest result must fail:\n%s", out)
	} else if !strings.Contains(out, "has no result yet") {
		t.Fatalf("active result error should explain no result:\n%s", out)
	}
	e.waitState(t, id, "done")
}

func TestNeedsProvisionApproveLoop(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s-provision"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"s-provision","result":"dependency install needs the host\n\nstate: blocked\nblocked: {\"kind\":\"provision\",\"command\":\"printf provisioned > provisioned.txt\",\"detail\":\"sandbox cannot provision dependencies\"}"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "install dependency"))
	m := e.waitState(t, id, "blocked")
	blocked, ok := m["blocked"].(map[string]any)
	if !ok || blocked["kind"] != "provision" || blocked["command"] != "printf provisioned > provisioned.txt" {
		t.Fatalf("provision block not surfaced: %+v", m)
	}

	e.writeScript(t, resultDone)
	e.legwork(t, "approve", id)
	m = e.waitState(t, id, "done")
	if _, ok := m["blocked"]; ok {
		t.Fatalf("blocked reason should clear after approve/resume: %+v", m)
	}
	got, err := os.ReadFile(filepath.Join(tree, "provisioned.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "provisioned" {
		t.Fatalf("provision command wrote %q", got)
	}
	evs := e.legwork(t, "events", id)
	for _, want := range []string{"needs-provision", "approve", "finished"} {
		if !strings.Contains(evs, want) {
			t.Fatalf("events missing %q:\n%s", want, evs)
		}
	}
}

func TestApproveProvisionCommandFailureStaysBlocked(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-provision","result":"host command failed earlier\n\nstate: blocked\nblocked: {\"kind\":\"provision\",\"command\":\"printf failing && exit 7\"}"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "install dependency"))
	e.waitState(t, id, "blocked")

	out, err := e.legworkErr("approve", id)
	if err == nil {
		t.Fatalf("approve must fail when provision command fails:\n%s", out)
	}
	if !strings.Contains(out, "provision command failed") {
		t.Fatalf("approve failure should explain provision command failure:\n%s", out)
	}
	m := e.waitState(t, id, "blocked")
	if blocked, ok := m["blocked"].(map[string]any); !ok || blocked["command"] != "printf failing && exit 7" {
		t.Fatalf("failed approve should leave provision block intact: %+v", m)
	}
	evs := e.legwork(t, "events", id)
	if !strings.Contains(evs, "approve") || !strings.Contains(evs, "provision command failed") {
		t.Fatalf("failed approve event missing:\n%s", evs)
	}
}

func TestApproveRejectsNonProvisionBlockedJob(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-verify","result":"suite needs host tmp\n\nstate: blocked\nblocked: {\"kind\":\"verify\",\"detail\":\"go test needs writable cache\"}"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "verify"))
	e.waitState(t, id, "blocked")

	out, err := e.legworkErr("approve", id)
	if err == nil {
		t.Fatalf("approve must reject verify blocks:\n%s", out)
	}
	if !strings.Contains(out, "not needs-provision") {
		t.Fatalf("approve rejection should explain state:\n%s", out)
	}
	m := e.waitState(t, id, "blocked")
	if blocked, ok := m["blocked"].(map[string]any); !ok || blocked["kind"] != "verify" {
		t.Fatalf("verify block should remain intact: %+v", m)
	}
}

func TestAckWorkspaceLessTerminalJob(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--read-only", "review only"))
	e.waitState(t, id, "done")

	out := e.legwork(t, "ack", id, "--json")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("bad ack json: %v\n%s", err, out)
	}
	if m["state"] != "closed" {
		t.Fatalf("ack state = %v", m["state"])
	}
	closed, ok := m["closed"].(string)
	if !ok || closed == "" {
		t.Fatalf("closed timestamp missing: %+v", m)
	}
	if _, err := time.Parse(time.RFC3339Nano, closed); err != nil {
		t.Fatalf("closed timestamp not RFC3339: %q: %v", closed, err)
	}
	evs := e.legwork(t, "events", id)
	if !strings.Contains(evs, "closed") || !strings.Contains(evs, "job acknowledged") {
		t.Fatalf("ack event missing:\n%s", evs)
	}
}

func TestAckRemovesJobTempDir(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "uses temp"))
	e.waitState(t, id, "done")

	tmp := filepath.Join(e.state, "jobs", id, "tmp")
	if info, err := os.Stat(tmp); err != nil || !info.IsDir() {
		t.Fatalf("job temp missing before ack: info=%v err=%v", info, err)
	}

	e.legwork(t, "ack", id)
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("job temp still exists after ack: %v", err)
	}
}

func TestAckRefusesNonTerminalWithoutForce(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s2"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"s2","result":"state: needs-input\nquestion: postgres or sqlite?"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "needs decision"))
	e.waitState(t, id, "needs-input")

	if out, err := e.legworkErr("ack", id); err == nil {
		t.Fatalf("ack must refuse needs-input without force:\n%s", out)
	} else if !strings.Contains(out, "only terminal jobs") {
		t.Fatalf("ack refusal should explain force:\n%s", out)
	}
	e.legwork(t, "ack", id, "--force")
	m := e.legwork(t, "status", id, "--json")
	if !strings.Contains(m, `"state": "closed"`) || strings.Contains(m, `"question"`) {
		t.Fatalf("forced ack did not close/clear question:\n%s", m)
	}
}

func TestAckRefusesWorkspaceJob(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "workspace work"))
	e.waitState(t, id, "done")

	if out, err := e.legworkErr("ack", id); err == nil {
		t.Fatalf("ack must refuse workspace job:\n%s", out)
	} else if !strings.Contains(out, "belongs to workspace "+wsID) {
		t.Fatalf("workspace refusal should point to workspace close:\n%s", out)
	}
}

// Mid-turn death: no result line -> interrupted, never a lie.
func TestMidTurnDeath(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"about to crash"}]}}`,
		"#die",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "doomed"))
	e.waitState(t, id, "interrupted")
}

// Missing status block: never assume done.
func TestMissingStatusBlock(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s3","result":"I totally finished everything!"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "sloppy agent"))
	e.waitState(t, id, "blocked")
}

func TestCancelInterruptsTurn(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"long haul"}]}}`,
		"#sleep 30000",
		resultDone,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "slow task"))
	// Wait until it's actually running.
	time.Sleep(300 * time.Millisecond)
	e.legwork(t, "cancel", id)
	e.waitState(t, id, "interrupted")
}

func TestResumeRefusedWhileActive(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, "#sleep 5000", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "busy"))
	time.Sleep(200 * time.Millisecond)
	if out, err := e.legworkErr("resume", id, "more work"); err == nil {
		t.Fatalf("resume of an active job must be refused:\n%s", out)
	}
	e.legwork(t, "cancel", id)
}

// watch on a resumed job must follow the live turn, not replay an earlier
// turn's finished event and exit immediately.
func TestWatchFollowsResumedTurn(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"s1","result":"first-turn-ok\n\nstate: done"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "first"))
	e.waitState(t, id, "done")

	// Second turn is slow: a watch that replays turn 1 would return long
	// before it ends.
	e.writeScript(t, "#sleep 1200", resultDone)
	e.legwork(t, "resume", id, "again")
	start := time.Now()
	out := e.legwork(t, "watch", id)
	if time.Since(start) < time.Second {
		t.Fatalf("watch exited before the resumed turn ended (%v):\n%s", time.Since(start), out)
	}
	if strings.Contains(out, "first-turn-ok") {
		t.Fatalf("watch replayed the previous turn:\n%s", out)
	}

	// On a job that isn't running, watch replays only the most recent turn.
	out = e.legwork(t, "watch", id)
	if strings.Contains(out, "first-turn-ok") || !strings.Contains(out, "finished") {
		t.Fatalf("watch of a finished multi-turn job should replay only the last turn:\n%s", out)
	}
}

func TestUnknownJobAndAgentFailCleanly(t *testing.T) {
	e := newEnv(t)
	if _, err := e.legworkErr("status", "job-999"); err == nil {
		t.Fatal("unknown job must error")
	}
	if _, err := e.legworkErr("run", "--agent", "gpt9000", "x"); err == nil {
		t.Fatal("unknown agent must error")
	}
}
