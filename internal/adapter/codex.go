package adapter

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/whoislikemiha/legwork/internal/events"
)

// Codex drives OpenAI's codex CLI headless: codex exec --json, prompt on stdin.
// It is legwork's second real dialect and its first OS-sandboxed agent
// (DESIGN.md §3): permission control is the kernel sandbox, not a plan mode.
type Codex struct{}

func (c *Codex) Name() string { return "codex" }

// Bin is the codex executable; LEGWORK_CODEX_BIN overrides for self-hosted or
// pinned installs, mirroring how doctor probes presence + version.
func (c *Codex) Bin() string {
	if b := os.Getenv("LEGWORK_CODEX_BIN"); b != "" {
		return b
	}
	return "codex"
}

func (c *Codex) Caps() Caps {
	return Caps{Fork: true, OSSandbox: true, StructuredStatus: "convention", Subagents: true}
}

// Command builds one codex turn. codex exec is non-interactive (no approval
// prompt); the sandbox mode is the only permission control. Resume is a
// subcommand (exec resume <id>), not a flag — the one structural divergence
// from claude's --resume. The prompt (worker rules + task) is fed on stdin via
// the `-` arg to dodge ARG_MAX and `-`-prefix misparsing; codex has no
// system-prompt flag, so the rules are re-prepended every turn, reasserting the
// status-block contract on each resume/answer just as claude re-sends
// --append-system-prompt.
func (c *Codex) Command(req TurnRequest) (*exec.Cmd, error) {
	var args []string
	if req.SessionID != "" {
		args = []string{"exec", "resume"}
	} else {
		args = []string{"exec"}
	}
	args = append(args, "--json", "--skip-git-repo-check")
	// Sandbox as a `-c sandbox_mode=` override, not the -s/--sandbox flag:
	// codex-cli 0.118.0 `exec resume` rejects -s (exit 2), while the config
	// override is accepted by both `exec` and `exec resume`. One code path, no
	// flag divergence.
	sandbox := "workspace-write"
	if req.ReadOnly {
		sandbox = "read-only"
	}
	args = append(args, "-c", "sandbox_mode=\""+sandbox+"\"")
	if req.TempDir != "" && !req.ReadOnly {
		args = append(args,
			"-c", "sandbox_workspace_write.writable_roots=["+tomlString(req.TempDir)+"]",
			"-c", "sandbox_workspace_write.exclude_tmpdir_env_var=false",
			"-c", "sandbox_workspace_write.exclude_slash_tmp=true",
		)
	}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	// codex has no --effort flag; reasoning level is a config override, and its
	// scale tops out at "high" (no xhigh/max). codexEffort maps legwork's shared
	// vocabulary onto that ceiling so `--effort` means the same verb for both
	// agents even though codex clamps the top two levels.
	if e := codexEffort(req.Effort); e != "" {
		args = append(args, "-c", "model_reasoning_effort=\""+e+"\"")
	}
	if req.SessionID != "" {
		args = append(args, req.SessionID) // positional id, after options
	}
	args = append(args, "-") // read prompt from stdin

	cmd := exec.Command(c.Bin(), args...)
	cmd.Dir = req.WorkDir
	prompt := req.Task
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n# Task\n\n" + req.Task
	}
	cmd.Stdin = strings.NewReader(prompt)
	return cmd, nil
}

// codexEffort maps legwork's --effort vocabulary (low|medium|high|xhigh|max) to
// codex's model_reasoning_effort scale (low|medium|high). codex has no level
// above "high", so xhigh and max both clamp there. Empty in, empty out.
func codexEffort(e string) string {
	switch e {
	case "low", "medium", "high":
		return e
	case "xhigh", "max":
		return "high"
	}
	return ""
}

func tomlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func (c *Codex) Parser() Parser { return &codexParser{} }

// codexParser normalizes the codex `exec --json` event stream. Fresh per turn.
type codexParser struct {
	threadID     string
	lastAgentMsg string
	done         bool // result emitted; guards "result exactly once"
}

// codexItem is one `item` object; its discriminant is `type` (same key as the
// top-level line). Fields are a union across item variants.
type codexItem struct {
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	Status           string `json:"status"`
	Changes          []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Query     string          `json:"query"`
	Items     json.RawMessage `json:"items"`
	Message   string          `json:"message"`
}

// Minimal shapes of the codex event stream we consume. The top-level
// discriminant is `type`; inside `item` the discriminant is also `type`.
type codexLine struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id"`
	Item     *codexItem `json:"item"`
	Usage    *struct {
		InputTokens         int `json:"input_tokens"`
		CachedInputTokens   int `json:"cached_input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		ReasoningOutputToks int `json:"reasoning_output_tokens"`
	} `json:"usage"`
	// turn.failed carries a nested error; top-level `error` carries a message.
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

func (p *codexParser) Line(raw []byte) ([]events.Event, *TurnResult, error) {
	var l codexLine
	if err := json.Unmarshal(raw, &l); err != nil {
		// Non-JSON noise on stdout: ignore for the index, transcript has it.
		return nil, nil, nil
	}
	if p.done {
		return nil, nil, nil
	}

	switch l.Type {
	case "thread.started":
		if l.ThreadID != "" {
			p.threadID = l.ThreadID
		}
		return nil, nil, nil

	case "item.completed":
		// Only completed items index; item.started/updated are dedup noise.
		if l.Item == nil {
			return nil, nil, nil
		}
		return p.itemEvents(l.Item), nil, nil

	case "turn.completed":
		res := &TurnResult{
			SessionID: p.threadID,
			CostUSD:   0, // subscription has no per-turn cost; Context is health.
			Turns:     1,
		}
		if l.Usage != nil {
			res.TokensIn = l.Usage.InputTokens
			res.TokensOut = l.Usage.OutputTokens
			// Context footprint of the last call: fresh input + cache reads.
			res.Context = l.Usage.InputTokens + l.Usage.CachedInputTokens
		}
		state, question, rest := ParseStatusBlock(p.lastAgentMsg)
		res.State, res.Question, res.Result = state, question, rest
		p.done = true
		return nil, res, nil

	case "turn.failed", "error":
		msg := l.Message
		if l.Error != nil && l.Error.Message != "" {
			msg = l.Error.Message
		}
		res := &TurnResult{
			SessionID: p.threadID,
			CostUSD:   0,
			Turns:     1,
			State:     "failed",
			Result:    msg,
			IsError:   true,
		}
		if codexAuthError(msg) {
			res.State = "auth-required"
		}
		p.done = true
		return nil, res, nil
	}
	return nil, nil, nil
}

// itemEvents maps one completed item to zero or more index events, recording
// the latest agent_message as the status-block source.
func (p *codexParser) itemEvents(it *codexItem) []events.Event {
	switch it.Type {
	case "agent_message":
		if strings.TrimSpace(it.Text) == "" {
			return nil
		}
		p.lastAgentMsg = it.Text
		return []events.Event{{
			Type: events.TypeText, Actor: "main",
			Preview: events.Truncate(it.Text),
		}}

	case "reasoning":
		if strings.TrimSpace(it.Text) == "" {
			return nil
		}
		return []events.Event{{
			Type: events.TypeText, Actor: "main",
			Preview: events.Truncate(it.Text),
		}}

	case "command_execution":
		f := map[string]any{"tool": "shell", "command": it.Command, "status": it.Status}
		if it.ExitCode != nil {
			f["exit_code"] = *it.ExitCode
		}
		return []events.Event{{
			Type: events.TypeToolCall, Actor: "main",
			Preview: events.Truncate(it.Command),
			Fields:  f,
		}}

	case "file_change":
		// Truncate the changes list in the index (a large diff touches many
		// files); full fidelity stays in the transcript, like claude's tool
		// input.
		changes, _ := json.Marshal(it.Changes)
		return []events.Event{{
			Type: events.TypeToolCall, Actor: "main",
			Preview: "edit",
			Fields:  map[string]any{"tool": "edit", "changes": events.Truncate(string(changes)), "status": it.Status},
		}}

	case "mcp_tool_call":
		return []events.Event{{
			Type: events.TypeToolCall, Actor: "main",
			Preview: it.Server + "/" + it.Tool,
			Fields:  map[string]any{"tool": it.Tool, "server": it.Server, "arguments": events.Truncate(string(it.Arguments))},
		}}

	case "web_search":
		return []events.Event{{
			Type: events.TypeToolCall, Actor: "main",
			Preview: events.Truncate(it.Query),
			Fields:  map[string]any{"tool": "web_search", "query": it.Query},
		}}

	case "todo_list":
		return []events.Event{{
			Type: events.TypeToolCall, Actor: "main",
			Preview: "todo",
			Fields:  map[string]any{"tool": "todo_list", "items": events.Truncate(string(it.Items))},
		}}
	}
	return nil
}

// codexAuthError flags codex-specific auth failures, distinct from claude's.
func codexAuthError(s string) bool {
	ls := strings.ToLower(s)
	for _, marker := range []string{"not logged in", "codex login", "401", "unauthorized", "authentication", "invalid api key", "token expired"} {
		if strings.Contains(ls, marker) {
			return true
		}
	}
	return false
}
