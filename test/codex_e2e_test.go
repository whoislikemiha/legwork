package e2e

import (
	"strings"
	"testing"
)

// The codex e2e tests drive the fake agent (zero spend) with codex-shaped
// JSONL and set LEGWORK_FAKE_PARSER=codex so the production codexParser runs
// through the real runner: spawn, detach, tee, parse, status block, persist.

const codexDone = `{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":300,"output_tokens":80}}`

func TestCodexHappyPath(t *testing.T) {
	e := newEnv(t)
	e.parser = "codex"
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-1"}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"go build","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"wired it up\n\nstate: done"}}`,
		codexDone,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "do it"))
	m := e.waitState(t, id, "done")
	if m["session_id"] != "cx-1" {
		t.Fatalf("codex thread id not persisted as session: %+v", m)
	}
	// Cost is nominal on codex (0 → omitted); context (input + cached) is the
	// real health metric and must be persisted.
	if _, ok := m["cost_usd"]; ok {
		t.Fatalf("codex cost must be 0/absent: %+v", m["cost_usd"])
	}
	if m["context"].(float64) != 1500 {
		t.Fatalf("context (input+cached) not persisted: %+v", m["context"])
	}
	evs := e.legwork(t, "events", id)
	for _, want := range []string{"started", "tool-call", "text", "usage", "finished"} {
		if !strings.Contains(evs, want) {
			t.Fatalf("events missing %q:\n%s", want, evs)
		}
	}
}

func TestCodexNeedsInputResumeLoop(t *testing.T) {
	e := newEnv(t)
	e.parser = "codex"
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-2"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"which db?\n\nstate: needs-input\nquestion: postgres or sqlite?"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5}}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "add persistence"))
	m := e.waitState(t, id, "needs-input")
	if m["question"] != "postgres or sqlite?" {
		t.Fatalf("question not surfaced: %+v", m)
	}

	// Orchestrator answers; the resume turn completes.
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-2"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done\n\nstate: done"}}`,
		codexDone,
	)
	e.legwork(t, "answer", id, "postgres")
	e.waitState(t, id, "done")
	evs := e.legwork(t, "events", id)
	if !strings.Contains(evs, "needs-input") || !strings.Contains(evs, "answer") {
		t.Fatalf("question/answer not in event log:\n%s", evs)
	}
}

func TestCodexTurnFailed(t *testing.T) {
	e := newEnv(t)
	e.parser = "codex"
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-3"}`,
		`{"type":"turn.failed","error":{"message":"model stream disconnected"}}`,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "flaky"))
	e.waitState(t, id, "failed")
}

// Mid-turn death with no terminal event -> interrupted, never a lie.
func TestCodexMidTurnDeath(t *testing.T) {
	e := newEnv(t)
	e.parser = "codex"
	e.writeScript(t,
		`{"type":"thread.started","thread_id":"cx-4"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"about to crash"}}`,
		"#die",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "doomed"))
	e.waitState(t, id, "interrupted")
}
