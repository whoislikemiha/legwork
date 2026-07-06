package adapter

import (
	"io"
	"strings"
	"testing"

	"github.com/whoislikemiha/legwork/internal/events"
)

// drive feeds lines through a fresh codex parser and returns the collected
// events and the (single) TurnResult.
func driveCodex(t *testing.T, lines ...string) ([]events.Event, *TurnResult) {
	t.Helper()
	p := (&Codex{}).Parser()
	var got []events.Event
	var res *TurnResult
	for _, l := range lines {
		evs, r, err := p.Line([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, evs...)
		if r != nil {
			if res != nil {
				t.Fatalf("result emitted more than once")
			}
			res = r
		}
	}
	return got, res
}

func TestCodexParserStream(t *testing.T) {
	got, res := driveCodex(t,
		`{"type":"thread.started","thread_id":"th-99"}`,
		`not json`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"type":"agent_message","text":"partial"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"thinking hard"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"ls -la","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.go","kind":"modify"}],"status":"completed"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"all wired up\n\nstate: done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":300,"output_tokens":80,"reasoning_output_tokens":40}}`,
	)
	// reasoning + command + file_change + agent_message = 4 events.
	if len(got) != 4 {
		t.Fatalf("events = %d: %+v", len(got), got)
	}
	if got[0].Type != events.TypeText || got[1].Type != events.TypeToolCall ||
		got[2].Type != events.TypeToolCall || got[3].Type != events.TypeText {
		t.Fatalf("event types = %+v", got)
	}
	if got[1].Fields["command"] != "ls -la" || got[1].Fields["exit_code"] != 0 {
		t.Fatalf("command fields = %+v", got[1].Fields)
	}
	if res == nil {
		t.Fatal("no result")
	}
	if res.State != "done" || res.SessionID != "th-99" {
		t.Fatalf("result = %+v", res)
	}
	if res.CostUSD != 0 || res.Turns != 1 {
		t.Fatalf("cost/turns = %+v", res)
	}
	if res.TokensIn != 1200 || res.TokensOut != 80 {
		t.Fatalf("tokens = %+v", res)
	}
	if res.Context != 1500 { // input + cached_input
		t.Fatalf("context = %d, want 1500", res.Context)
	}
	if strings.Contains(res.Result, "state:") {
		t.Fatalf("status block not stripped: %q", res.Result)
	}
}

func TestCodexParserNeedsInput(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"which db?\n\nstate: needs-input\nquestion: postgres or sqlite?"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5}}`,
	)
	if res == nil || res.State != "needs-input" || res.Question != "postgres or sqlite?" {
		t.Fatalf("needs-input not mapped: %+v", res)
	}
}

func TestCodexParserMissingStatusBlock(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"I finished everything!"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1}}`,
	)
	if res == nil || res.State != "blocked" {
		t.Fatalf("missing block must be blocked: %+v", res)
	}
}

func TestCodexParserTurnFailed(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"turn.failed","error":{"message":"model stream disconnected"}}`,
	)
	if res == nil || res.State != "failed" || !res.IsError {
		t.Fatalf("turn.failed not mapped: %+v", res)
	}
	if res.Result != "model stream disconnected" {
		t.Fatalf("failure message not surfaced: %+v", res)
	}
}

func TestCodexParserAuthError(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"turn.failed","error":{"message":"Not logged in. Run codex login."}}`,
	)
	if res == nil || res.State != "auth-required" {
		t.Fatalf("auth error not mapped: %+v", res)
	}
}

func TestCodexParserTopLevelError(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"error","message":"unexpected server error"}`,
	)
	if res == nil || res.State != "failed" || res.Result != "unexpected server error" {
		t.Fatalf("top-level error not mapped: %+v", res)
	}
}

// Only the first result is emitted; trailing lines after a terminal event are
// ignored (result exactly once).
func TestCodexParserResultOnce(t *testing.T) {
	_, res := driveCodex(t,
		`{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1}}`,
		`{"type":"turn.failed","error":{"message":"late failure"}}`,
	)
	if res == nil || res.State != "blocked" {
		t.Fatalf("first terminal event must win: %+v", res)
	}
}

func TestCodexCommand(t *testing.T) {
	// Fresh mutating turn.
	cmd, err := (&Codex{}).Command(TurnRequest{
		Task: "do it", SystemPrompt: "RULES", WorkDir: "/tmp/ws",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	// Sandbox is a -c override (not -s), since exec resume rejects -s in
	// codex-cli 0.118.0; same path on fresh and resume turns.
	for _, want := range []string{"exec", "--json", "--skip-git-repo-check", `-c sandbox_mode="workspace-write"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("fresh turn missing %q: %v", want, cmd.Args)
		}
	}
	if strings.Contains(joined, "--sandbox") || strings.Contains(joined, "-s ") {
		t.Fatalf("fresh turn must not use the -s/--sandbox flag: %v", cmd.Args)
	}
	if strings.Contains(joined, "exec resume") {
		t.Fatalf("fresh turn must not resume: %v", cmd.Args)
	}
	if cmd.Args[len(cmd.Args)-1] != "-" {
		t.Fatalf("prompt must come from stdin (- last arg): %v", cmd.Args)
	}
	if cmd.Dir != "/tmp/ws" {
		t.Fatalf("cmd.Dir = %q", cmd.Dir)
	}
	stdinBytes, _ := io.ReadAll(cmd.Stdin)
	stdin := string(stdinBytes)
	if !strings.Contains(stdin, "RULES") || !strings.Contains(stdin, "do it") {
		t.Fatalf("stdin must carry rules + task: %q", stdin)
	}

	// Read-only turn.
	cmd, _ = (&Codex{}).Command(TurnRequest{Task: "plan it", ReadOnly: true})
	joined = strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, `-c sandbox_mode="read-only"`) {
		t.Fatalf("read-only turn must use read-only sandbox: %v", cmd.Args)
	}
	if strings.Contains(joined, "workspace-write") {
		t.Fatalf("read-only turn must not be writable: %v", cmd.Args)
	}

	// Model passthrough only when set.
	cmd, _ = (&Codex{}).Command(TurnRequest{Task: "x"})
	if strings.Contains(strings.Join(cmd.Args, " "), "-m ") {
		t.Fatalf("no -m expected without model: %v", cmd.Args)
	}
	cmd, _ = (&Codex{}).Command(TurnRequest{Task: "x", Model: "gpt-5"})
	if !strings.Contains(strings.Join(cmd.Args, " "), "-m gpt-5") {
		t.Fatalf("model not passed: %v", cmd.Args)
	}

	// Resume: subcommand + positional id before the stdin marker.
	cmd, _ = (&Codex{}).Command(TurnRequest{Task: "more", SessionID: "th-42"})
	joined = strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "exec resume") {
		t.Fatalf("resume must use exec resume subcommand: %v", cmd.Args)
	}
	if !strings.Contains(joined, "th-42 -") {
		t.Fatalf("resume must pass session id then stdin marker: %v", cmd.Args)
	}
	// The -c sandbox override must ride on resume too (exec resume rejects -s).
	if !strings.Contains(joined, `-c sandbox_mode="workspace-write"`) {
		t.Fatalf("resume must carry the sandbox_mode override: %v", cmd.Args)
	}
	if strings.Contains(joined, "--sandbox") || strings.Contains(joined, "-s ") {
		t.Fatalf("resume must not use the -s/--sandbox flag: %v", cmd.Args)
	}
}

func TestCodexCommandEffort(t *testing.T) {
	// No effort -> no reasoning override.
	cmd, _ := (&Codex{}).Command(TurnRequest{Task: "x"})
	if strings.Contains(strings.Join(cmd.Args, " "), "model_reasoning_effort") {
		t.Fatalf("no reasoning override expected without effort: %v", cmd.Args)
	}

	// Native levels pass straight through; xhigh/max clamp to codex's "high".
	for effort, want := range map[string]string{
		"low":    "low",
		"medium": "medium",
		"high":   "high",
		"xhigh":  "high",
		"max":    "high",
	} {
		cmd, _ := (&Codex{}).Command(TurnRequest{Task: "x", Effort: effort})
		joined := strings.Join(cmd.Args, " ")
		if !strings.Contains(joined, `-c model_reasoning_effort="`+want+`"`) {
			t.Fatalf("effort %q must map to %q: %v", effort, want, cmd.Args)
		}
	}
}
