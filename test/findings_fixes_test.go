// Fixes for the 2026-08-01 orchestration-eval findings (eval/FINDINGS.md):
// F1 close landing proof, F2 flag-surface consistency, F3 selector hints.
package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

const needsInputResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"s9","result":"state: needs-input\nquestion: which db?"}`

// F2: resume/answer accept the read-side selector surface; --run targets the
// run label's newest job so orchestrators can steer without job-ID bookkeeping.
func TestAnswerAndResumeByRunLabel(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s9"}`,
		needsInputResult,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "pipeline", "add persistence"))
	e.waitState(t, id, "needs-input")

	e.writeScript(t, resultDone)
	out := e.legwork(t, "answer", "--run", "pipeline", "postgres")
	if !strings.Contains(out, id) {
		t.Fatalf("answer --run did not resolve to %s:\n%s", id, out)
	}
	if !strings.Contains(out, "resolved run") {
		t.Fatalf("run resolution must be announced:\n%s", out)
	}
	e.waitState(t, id, "done")

	e.writeScript(t, resultDone)
	out = e.legwork(t, "resume", "--run", "pipeline", "now polish it")
	if !strings.Contains(out, id) {
		t.Fatalf("resume --run did not resolve to %s:\n%s", id, out)
	}
	e.waitState(t, id, "done")

	// Without any selector the message alone is a usage error, not a guess.
	if out, err := e.legworkErr("resume", "just a message"); err == nil {
		t.Fatalf("selector-less resume must fail:\n%s", out)
	} else if !strings.Contains(out, "needs a target") {
		t.Fatalf("selector-less resume error unhelpful:\n%s", out)
	}
}

// F2: dispatch-time flags on resume get an error that names where they live.
func TestResumeDispatchFlagHint(t *testing.T) {
	e := newEnv(t)
	out, err := e.legworkErr("resume", "--agent", "claude", "job-1", "go on")
	if err == nil {
		t.Fatalf("resume --agent must fail:\n%s", out)
	}
	if !strings.Contains(out, "set at dispatch") {
		t.Fatalf("unknown-flag error missing dispatch hint:\n%s", out)
	}
}

// F2: diff is machine-readable like every other read surface.
func TestDiffJSON(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, "#write newfile.txt hello", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "edit"))
	e.waitState(t, id, "done")

	out := e.legwork(t, "diff", wsID, "--json")
	var d struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
		Diff      string `json:"diff"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("bad diff json: %v\n%s", err, out)
	}
	if d.Workspace != wsID || !strings.Contains(d.Diff, "newfile.txt") {
		t.Fatalf("diff json incomplete: %+v", d)
	}
}

// F2: ws list is an alias for ws ls.
func TestWSListAlias(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	out := e.legwork(t, "ws", "list")
	if !strings.Contains(out, ws["id"].(string)) {
		t.Fatalf("ws list did not list %v:\n%s", ws["id"], out)
	}
}

// F2: runs --run narrows the rollup to one label.
func TestRunsRunFilter(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	idA := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "alpha", "a"))
	e.waitState(t, idA, "done")
	e.writeScript(t, resultDone)
	idB := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "beta", "b"))
	e.waitState(t, idB, "done")

	out := e.legwork(t, "runs", "--run", "alpha")
	if !strings.Contains(out, "alpha") || strings.Contains(out, "beta") {
		t.Fatalf("runs --run alpha not filtered:\n%s", out)
	}
	if out := e.legwork(t, "runs", "--run", "nope"); !strings.Contains(out, `no run "nope"`) {
		t.Fatalf("missing label should say so:\n%s", out)
	}
}

// F3: a workspace ID handed to a job selector names the trap instead of the
// generic no-such-job error.
func TestWorkspaceIDInJobSelectorHint(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	for _, verb := range []string{"status", "result", "ack"} {
		out, err := e.legworkErr(verb, wsID)
		if err == nil {
			t.Fatalf("%s %s must fail:\n%s", verb, wsID, out)
		}
		if !strings.Contains(out, "is a workspace, not a job") || !strings.Contains(out, "ls --workspace") {
			t.Fatalf("%s %s error missing workspace hint:\n%s", verb, wsID, out)
		}
	}
	// A selector that is neither job nor workspace keeps the original error.
	if out, _ := e.legworkErr("status", "ws-99"); strings.Contains(out, "is a workspace") {
		t.Fatalf("nonexistent workspace must not claim to be one:\n%s", out)
	}
}

// F1: a successful close prints the landing proof — target, merge commit,
// receipt — so confirming the close never needs git.
func TestCloseMergePrintsLandingProof(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, "#write feature.txt done", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "add feature"))
	e.waitState(t, id, "done")
	e.legwork(t, "ws", "commit", wsID, "-m", "add feature")

	out := e.legwork(t, "close", wsID, "--merge-into", "main")
	if !strings.Contains(out, "closed (merged)") {
		t.Fatalf("close outcome missing:\n%s", out)
	}
	if !strings.Contains(out, "landed: main @") {
		t.Fatalf("close output missing landing proof:\n%s", out)
	}
	if !strings.Contains(out, "receipt: ") {
		t.Fatalf("close output missing receipt ID:\n%s", out)
	}
}
