// ws status: the one-command workspace rollup
// (planning/tasks/actionable-workspace-status.md; residual F1 in
// eval/FINDINGS.md — the receipt-shaped answer to "confirm the landing").
package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func wsStatusJSON(t *testing.T, e *env, wsID string) map[string]any {
	t.Helper()
	out := e.legwork(t, "ws", "status", wsID, "--json")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("bad ws status json: %v\n%s", err, out)
	}
	return m
}

// writeReviewScript scripts one fake reviewer turn whose result carries the
// given verdict JSON in a fenced block, matching the ws review prompt contract.
func writeReviewScript(t *testing.T, e *env, session, verdict string) {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "num_turns": 1, "session_id": session,
		"result": "review complete\n```json\n" + verdict + "\n```\n\nstate: done",
	})
	if err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, string(line))
}

func nextActions(t *testing.T, m map[string]any) []string {
	t.Helper()
	var out []string
	for _, a := range m["next_actions"].([]any) {
		out = append(out, a.(map[string]any)["action"].(string))
	}
	return out
}

func TestWSStatusLifecycleAdvice(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	// Fresh workspace: no changes, no jobs -> dispatch.
	m := wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "dispatch" {
		t.Fatalf("fresh workspace next = %v, want [dispatch]", got)
	}

	// Worker leaves a question -> answer, with the question in the reason.
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"w1"}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1},"session_id":"w1","result":"state: needs-input\nquestion: tabs or spaces?"}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "edit stuff"))
	e.waitState(t, id, "needs-input")
	m = wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "answer" {
		t.Fatalf("needs-input next = %v, want [answer]", got)
	}

	// Turn completes with changes but no review -> review.
	e.writeScript(t, "#write feature.txt done", resultDone)
	e.legwork(t, "answer", id, "spaces")
	e.waitState(t, id, "done")
	m = wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "review" {
		t.Fatalf("unreviewed-diff next = %v, want [review]", got)
	}
	if !strings.Contains(m["diff_stat"].(string), "file") {
		t.Fatalf("diff_stat missing: %v", m["diff_stat"])
	}
}

// The review-gate advice sequence: FIX -> fix-findings, SHIP -> commit,
// committed -> close, with the ahead-of-base fact tracking the commit.
func TestWSStatusReviewAndLandingAdvice(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, "#write feature.txt done", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "add feature"))
	e.waitState(t, id, "done")

	m := wsStatusJSON(t, e, wsID)
	if got := m["commits_ahead"].(float64); got != 0 {
		t.Fatalf("commits_ahead before commit = %v, want 0", got)
	}

	// Reviewer returns FIX -> the next step is relaying findings.
	writeReviewScript(t, e, "s-fix",
		`{"verdict":"FIX","findings":[{"file":"feature.txt","line":1,"severity":"high","detail":"needs a test"}]}`)
	rid := strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	e.waitState(t, rid, "done")
	m = wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "fix-findings" {
		t.Fatalf("FIX verdict next = %v, want [fix-findings]", got)
	}

	// Re-review returns SHIP -> commit as the orchestrator.
	writeReviewScript(t, e, "s-ship", `{"verdict":"SHIP","findings":[]}`)
	rid = strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	e.waitState(t, rid, "done")
	m = wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "commit" {
		t.Fatalf("SHIP verdict next = %v, want [commit]", got)
	}

	// Committed -> close, and the branch is ahead of base.
	e.legwork(t, "ws", "commit", wsID, "-m", "add feature")
	m = wsStatusJSON(t, e, wsID)
	if got := nextActions(t, m); len(got) != 1 || got[0] != "close" {
		t.Fatalf("committed next = %v, want [close]", got)
	}
	if got := m["commits_ahead"].(float64); got != 1 {
		t.Fatalf("commits_ahead after commit = %v, want 1", got)
	}
}

// Job status shares the workspace vocabulary; blocked verify points at
// verification with the requested command inlined, never at a generic resume.
func TestJobStatusAdvice(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, resultBlockedVerify)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "build it"))
	e.waitState(t, id, "blocked")

	var st map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "status", id, "--json")), &st); err != nil {
		t.Fatal(err)
	}
	acts := st["next_actions"].([]any)
	if len(acts) != 1 {
		t.Fatalf("next_actions = %v", acts)
	}
	a := acts[0].(map[string]any)
	if a["action"] != "verify" || !strings.Contains(a["command"].(string), "legwork verify "+id+" --") {
		t.Fatalf("blocked-verify advice wrong: %+v", a)
	}
	human := e.legwork(t, "status", id)
	if !strings.Contains(human, "next: verify") {
		t.Fatalf("human status missing verify advice:\n%s", human)
	}

	// Done workspace job routes to the workspace, not ack.
	e.writeScript(t, resultDone)
	e.legwork(t, "resume", id, "verified externally; finish up")
	e.waitState(t, id, "done")
	if err := json.Unmarshal([]byte(e.legwork(t, "status", id, "--json")), &st); err != nil {
		t.Fatal(err)
	}
	a = st["next_actions"].([]any)[0].(map[string]any)
	if a["action"] != "workspace" || !strings.Contains(a["command"].(string), "ws status "+wsID) {
		t.Fatalf("done workspace-job advice wrong: %+v", a)
	}
}

func TestWSStatusClosedIsTheLandingProof(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, "#write feature.txt done", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "add feature"))
	e.waitState(t, id, "done")
	e.legwork(t, "ws", "commit", wsID, "-m", "add feature")
	e.legwork(t, "close", wsID, "--merge-into", "main")

	m := wsStatusJSON(t, e, wsID)
	if m["state"] != "closed" || m["disposition"] != "merged" || m["merged_into"] == nil {
		t.Fatalf("closed facts wrong: state=%v disposition=%v merged_into=%v",
			m["state"], m["disposition"], m["merged_into"])
	}
	if m["close_receipt"] == nil || m["final_commit"] == nil {
		t.Fatalf("receipts missing from closed rollup:\n%v", m)
	}
	if got := nextActions(t, m); len(got) != 1 || got[0] != "none" {
		t.Fatalf("closed next = %v, want [none]", got)
	}

	human := e.legwork(t, "ws", "status", wsID)
	for _, want := range []string{"closed/merged", "close receipt:", "final commit:", "no git check needed"} {
		if !strings.Contains(human, want) {
			t.Fatalf("human output missing %q:\n%s", want, human)
		}
	}

	// Read-only: two invocations agree and nothing advanced.
	if again := e.legwork(t, "ws", "status", wsID); again != human {
		t.Fatalf("ws status is not stable/read-only:\n%s\nvs\n%s", human, again)
	}
}
