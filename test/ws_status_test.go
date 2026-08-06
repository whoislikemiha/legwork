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
