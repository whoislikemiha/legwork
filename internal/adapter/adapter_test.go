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
		blockedKind        string
		blockedCommand     string
		restMustNotContain string
	}{
		{"all good\n\nstate: done", "done", "", "", "", "state:"},
		{"which db?\n\nstate: needs-input\nquestion: postgres or sqlite?", "needs-input", "postgres or sqlite?", "", "", "question:"},
		{"stuck on X\n\nstate: blocked", "blocked", "", "", "", "state:"},
		{`stuck

state: blocked
blocked: {"kind":"provision","command":"uv add slowapi","detail":"no network"}`, "blocked", "", "provision", "uv add slowapi", "blocked:"},
		{`verify outside

state: blocked
blocked: {"kind":"verify","detail":"go test needs writable cache"}`, "blocked", "", "verify", "", "blocked:"},
		{`choose

state: blocked
blocked: {"kind":"decision","detail":"which API should be public?"}`, "blocked", "", "decision", "", "blocked:"},
		{`pretty

state: blocked
blocked: {
  "kind": "provision",
  "command": "npm install",
  "detail": "network blocked"
}`, "blocked", "", "provision", "npm install", "blocked:"},
		{`no command

state: blocked
blocked: {"kind":"provision","detail":"missing command"}`, "blocked", "", "", "", "blocked:"},
		{`done?

state: blocked
blocked: {"kind":"nonsense","command":"rm -rf ."}`, "blocked", "", "", "", "blocked:"},
		// Missing block: never assume done.
		{"I finished everything!", "blocked", "", "", "", ""},
		// Case-insensitive.
		{"ok\n\nState: DONE", "done", "", "", "", "state:"},
	}
	for _, c := range cases {
		state, q, blocked, rest := ParseStatusBlock(c.in)
		if state != c.state || q != c.question {
			t.Errorf("ParseStatusBlock(%q) = (%s, %q), want (%s, %q)", c.in, state, q, c.state, c.question)
		}
		if c.blockedKind == "" {
			if blocked != nil {
				t.Errorf("ParseStatusBlock(%q) blocked = %+v, want nil", c.in, blocked)
			}
		} else if blocked == nil || blocked.Kind != c.blockedKind || blocked.Command != c.blockedCommand {
			t.Errorf("ParseStatusBlock(%q) blocked = %+v, want kind=%q command=%q", c.in, blocked, c.blockedKind, c.blockedCommand)
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

// TestClaudeParserContextIsLastCallWindow: Context must be the last assistant
// call's prompt window, not the result line's usage — that one sums cache reads
// across every call in the turn and overreports by ~turns×.
func TestClaudeParserContextIsLastCallWindow(t *testing.T) {
	p := (&Claude{}).Parser()
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":90,"cache_read_input_tokens":100000,"output_tokens":50}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"b"}],"usage":{"input_tokens":5,"cache_creation_input_tokens":200,"cache_read_input_tokens":110000,"output_tokens":30}}}`,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":2,"total_cost_usd":0.5,"usage":{"input_tokens":15,"output_tokens":80,"cache_creation_input_tokens":290,"cache_read_input_tokens":210000},"session_id":"s","result":"ok\n\nstate: done"}`,
	}
	var res *TurnResult
	for _, l := range lines {
		if _, r, err := p.Line([]byte(l)); err != nil {
			t.Fatal(err)
		} else if r != nil {
			res = r
		}
	}
	if want := 5 + 200 + 110000; res == nil || res.Context != want {
		t.Fatalf("Context = %+v, want %d (last call's window, not the turn sum)", res, want)
	}
	// No assistant usage at all (e.g. immediate error): fall back to the
	// result line's sum rather than reporting zero.
	p2 := (&Claude{}).Parser()
	_, r2, _ := p2.Line([]byte(`{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":5000,"cache_read_input_tokens":140000},"session_id":"s","result":"ok\n\nstate: done"}`))
	if r2 == nil || r2.Context != 200+5000+140000 {
		t.Fatalf("fallback Context = %+v", r2)
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

func TestClaudeCommandEffortAndFallback(t *testing.T) {
	cmd, err := (&Claude{}).Command(TurnRequest{Task: "do it", Effort: "low", FallbackModel: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "--effort low") {
		t.Fatalf("--effort not passed through: %v", cmd.Args)
	}
	if !strings.Contains(joined, "--fallback-model sonnet") {
		t.Fatalf("--fallback-model not passed through: %v", cmd.Args)
	}
}

func TestClaudeCommandOmitsUnsetPassthroughs(t *testing.T) {
	cmd, err := (&Claude{}).Command(TurnRequest{Task: "do it"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "--effort") || strings.Contains(joined, "--fallback-model") {
		t.Fatalf("unset passthroughs must not appear: %v", cmd.Args)
	}
}
