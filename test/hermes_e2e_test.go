package e2e

import (
	"strings"
	"testing"
)

func TestHermesHappyPath(t *testing.T) {
	e := newEnv(t)
	e.parser = "hermes"
	e.writeScript(t,
		`#write-temp hermes-usage.json {"estimated_cost_usd":1.5,"cost_status":"included","input_tokens":18000,"output_tokens":100,"cache_read_tokens":400,"session_id":"hm-1","completed":true,"failed":false}`,
		"wired it up",
		"",
		"state: done",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "do it"))
	m := e.waitState(t, id, "done")
	if m["session_id"] != "hm-1" || m["context"].(float64) != 18400 {
		t.Fatalf("hermes metadata = %+v", m)
	}
	if _, ok := m["cost_usd"]; ok {
		t.Fatalf("included subscription cost must be absent: %+v", m)
	}
	evs := e.legwork(t, "events", id)
	for _, want := range []string{"started", "text", "usage", "finished"} {
		if !strings.Contains(evs, want) {
			t.Fatalf("events missing %q:\n%s", want, evs)
		}
	}
	if strings.Contains(evs, "tool-call") {
		t.Fatalf("final-only adapter invented mid-turn events:\n%s", evs)
	}
}

func TestHermesNeedsInputResumeChainsLatestSession(t *testing.T) {
	e := newEnv(t)
	e.parser = "hermes"
	e.writeScript(t,
		`#write-temp hermes-usage.json {"cost_status":"included","session_id":"hm-first","completed":true,"failed":false}`,
		"which db?",
		"",
		"state: needs-input",
		"question: postgres or sqlite?",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "add persistence"))
	m := e.waitState(t, id, "needs-input")
	if m["session_id"] != "hm-first" {
		t.Fatalf("first session not persisted: %+v", m)
	}

	e.writeScript(t,
		`#write-temp hermes-usage.json {"cost_status":"included","session_id":"hm-second","completed":true,"failed":false}`,
		"implemented postgres",
		"",
		"state: done",
	)
	e.legwork(t, "answer", id, "postgres")
	m = e.waitState(t, id, "done")
	if m["session_id"] != "hm-second" {
		t.Fatalf("latest resumed session not persisted: %+v", m)
	}
}

func TestHermesProviderFailureExitZero(t *testing.T) {
	e := newEnv(t)
	e.parser = "hermes"
	e.writeScript(t,
		`#write-temp hermes-usage.json {"cost_status":"included","session_id":null,"completed":false,"failed":true}`,
		"HTTP 400: You're out of extra usage.",
		"",
		"state: done",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "provider failure"))
	m := e.waitState(t, id, "failed")
	if !strings.Contains(m["result"].(string), "state: done") {
		t.Fatalf("error text was parsed as a status block: %+v", m)
	}
}

func TestHermesAuthRequired(t *testing.T) {
	e := newEnv(t)
	e.parser = "hermes"
	e.writeScript(t,
		`#write-temp hermes-usage.json {"session_id":null,"completed":false,"failed":true}`,
		"401 unauthorized: run codex login",
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "auth failure"))
	e.waitState(t, id, "auth-required")
}

func TestHermesMidTurnDeathWithoutNewSidecar(t *testing.T) {
	e := newEnv(t)
	e.parser = "hermes"
	e.writeScript(t, "partial response", "#die")
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "doomed"))
	e.waitState(t, id, "interrupted")
}

func TestHermesReadOnlyRejectedAtDispatch(t *testing.T) {
	e := newEnv(t)
	out, err := e.legworkErr("run", "--agent", "hermes", "--read-only", "review")
	if err == nil {
		t.Fatalf("read-only hermes dispatch unexpectedly succeeded: %s", out)
	}
	for _, want := range []string{"no harness-enforced read-only mode", "claude or codex"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dispatch error missing %q:\n%s", want, out)
		}
	}
}

func TestHermesUnsupportedModelControlsRejected(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"run", "--agent", "hermes", "--effort", "high", "task"},
		{"run", "--agent", "hermes", "--fallback-model", "backup", "task"},
	} {
		out, err := e.legworkErr(args...)
		if err == nil {
			t.Fatalf("%v unexpectedly succeeded: %s", args, out)
		}
		if !strings.Contains(out, "not supported by --agent hermes") {
			t.Fatalf("%v error is not specific:\n%s", args, out)
		}
	}
}
