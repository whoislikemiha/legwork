package adapter

import (
	"strings"
	"testing"

	"github.com/whoislikemiha/legwork/internal/events"
)

func TestParseStatusBlock(t *testing.T) {
	cases := []struct {
		in                 string
		state, question    string
		restMustNotContain string
	}{
		{"all good\n\nstate: done", "done", "", "state:"},
		{"which db?\n\nstate: needs-input\nquestion: postgres or sqlite?", "needs-input", "postgres or sqlite?", "question:"},
		{"stuck on X\n\nstate: blocked", "blocked", "", "state:"},
		// Missing block: never assume done.
		{"I finished everything!", "blocked", "", ""},
		// Case-insensitive.
		{"ok\n\nState: DONE", "done", "", "state:"},
	}
	for _, c := range cases {
		state, q, rest := ParseStatusBlock(c.in)
		if state != c.state || q != c.question {
			t.Errorf("ParseStatusBlock(%q) = (%s, %q), want (%s, %q)", c.in, state, q, c.state, c.question)
		}
		if c.restMustNotContain != "" && strings.Contains(strings.ToLower(rest), c.restMustNotContain) {
			t.Errorf("rest still contains %q: %q", c.restMustNotContain, rest)
		}
	}
}

func TestClaudeParserStream(t *testing.T) {
	p := (&Claude{}).Parser()
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"sess-42"}`,
		`not json at all`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"thinking"},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.25,"usage":{"input_tokens":1000,"output_tokens":200},"session_id":"sess-42","result":"done deal\n\nstate: done"}`,
	}
	var got []events.Event
	var res *TurnResult
	for _, l := range lines {
		evs, r, err := p.Line([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, evs...)
		if r != nil {
			res = r
		}
	}
	if len(got) != 2 || got[0].Type != events.TypeText || got[1].Type != events.TypeToolCall {
		t.Fatalf("events = %+v", got)
	}
	if res == nil || res.State != "done" || res.SessionID != "sess-42" || res.CostUSD != 0.25 || res.TokensIn != 1000 {
		t.Fatalf("result = %+v", res)
	}
	if strings.Contains(res.Result, "state:") {
		t.Fatalf("status block not stripped: %q", res.Result)
	}
}

func TestClaudeParserAuthError(t *testing.T) {
	p := (&Claude{}).Parser()
	_, res, _ := p.Line([]byte(`{"type":"result","subtype":"error","is_error":true,"result":"Invalid API key. Please run /login","session_id":"s"}`))
	if res == nil || res.State != "auth-required" {
		t.Fatalf("auth error not mapped: %+v", res)
	}
}

func TestClaudeCommandReadOnly(t *testing.T) {
	cmd, err := (&Claude{}).Command(TurnRequest{Task: "plan it", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "--permission-mode plan") {
		t.Fatalf("read-only turn must use plan mode: %v", cmd.Args)
	}
	if strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("read-only turn must NOT skip permissions: %v", cmd.Args)
	}
}
