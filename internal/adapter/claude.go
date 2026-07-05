package adapter

import (
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/whoislikemiha/legwork/internal/events"
)

// Claude drives Claude Code headless: claude -p --output-format stream-json.
type Claude struct{}

func (c *Claude) Name() string { return "claude" }

func (c *Claude) Caps() Caps {
	return Caps{Fork: true, OSSandbox: false, StructuredStatus: "convention", Subagents: true}
}

func (c *Claude) Command(req TurnRequest) (*exec.Cmd, error) {
	args := []string{
		"-p", req.Task,
		"--output-format", "stream-json",
		"--verbose",
	}
	if req.ReadOnly {
		args = append(args, "--permission-mode", "plan")
	} else {
		args = append(args, "--dangerously-skip-permissions")
	}
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	cmd := exec.Command("claude", args...)
	cmd.Dir = req.WorkDir
	return cmd, nil
}

func (c *Claude) Parser() Parser { return &claudeParser{} }

// claudeParser normalizes Claude Code's stream-json lines.
type claudeParser struct {
	sessionID string
}

// Minimal shapes of the stream we consume.
type claudeLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Message   *struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	// result line
	Result    string  `json:"result"`
	IsError   bool    `json:"is_error"`
	NumTurns  int     `json:"num_turns"`
	TotalCost float64 `json:"total_cost_usd"`
	Usage     *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (p *claudeParser) Line(raw []byte) ([]events.Event, *TurnResult, error) {
	var l claudeLine
	if err := json.Unmarshal(raw, &l); err != nil {
		// Non-JSON noise on stdout: ignore for the index, transcript has it.
		return nil, nil, nil
	}
	if l.SessionID != "" {
		p.sessionID = l.SessionID
	}

	switch l.Type {
	case "assistant":
		if l.Message == nil {
			return nil, nil, nil
		}
		var evs []events.Event
		for _, block := range l.Message.Content {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					evs = append(evs, events.Event{
						Type: events.TypeText, Actor: "main",
						Preview: events.Truncate(block.Text),
					})
				}
			case "tool_use":
				evs = append(evs, events.Event{
					Type: events.TypeToolCall, Actor: "main",
					Preview: block.Name,
					Fields:  map[string]any{"tool": block.Name, "input": events.Truncate(string(block.Input))},
				})
			}
		}
		return evs, nil, nil

	case "result":
		res := &TurnResult{
			SessionID: p.sessionID,
			CostUSD:   l.TotalCost,
			Turns:     l.NumTurns,
			IsError:   l.IsError,
		}
		if l.Usage != nil {
			res.TokensIn = l.Usage.InputTokens
			res.TokensOut = l.Usage.OutputTokens
		}
		if l.IsError {
			res.State = "failed"
			res.Result = l.Result
			if isAuthError(l.Result) {
				res.State = "auth-required"
			}
			return nil, res, nil
		}
		state, question, rest := ParseStatusBlock(l.Result)
		res.State, res.Question, res.Result = state, question, rest
		return nil, res, nil
	}
	return nil, nil, nil
}

func isAuthError(s string) bool {
	ls := strings.ToLower(s)
	for _, marker := range []string{"invalid api key", "not logged in", "please run /login", "authentication_error", "oauth token has expired"} {
		if strings.Contains(ls, marker) {
			return true
		}
	}
	return false
}
