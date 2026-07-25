package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/whoislikemiha/legwork/internal/events"
)

const hermesUsageFile = "hermes-usage.json"

// Hermes drives Hermes Agent's final-only oneshot mode. Hermes reads its
// normal user config because that is where provider authentication lives, and
// keeps its normal repo rules/memory injection just as the other agents load
// repository context. Legwork's rules are still prepended on every turn.
type Hermes struct {
	usagePath string
}

func (h *Hermes) Name() string { return "hermes" }

func (h *Hermes) Bin() string {
	if b := os.Getenv("LEGWORK_HERMES_BIN"); b != "" {
		return b
	}
	return "hermes"
}

func (h *Hermes) Caps() Caps {
	return Caps{
		Fork: false, OSSandbox: false, StructuredStatus: "convention",
		Subagents: false, ReadOnly: false,
	}
}

func (h *Hermes) Command(req TurnRequest) (*exec.Cmd, error) {
	if req.ReadOnly {
		return nil, errors.New("hermes has no harness-enforced read-only mode")
	}
	if req.TempDir == "" {
		return nil, errors.New("hermes requires a tool-owned temp directory for usage telemetry")
	}
	h.usagePath = filepath.Join(req.TempDir, hermesUsageFile)
	// TempDir persists across resumed turns. Remove the prior report before
	// launch so a process that dies before writing its new sidecar cannot be
	// mistaken for the previous turn's completed result.
	if err := os.Remove(h.usagePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("clear prior hermes usage sidecar: %w", err)
	}
	prompt := req.Task
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n# Task\n\n" + req.Task
	}
	// Hermes -z requires the prompt as the option value (there is no stdin
	// sentinel). This is bounded by the platform's argv/ARG_MAX limit; normal
	// legwork rules plus one task are far below Linux's roughly 2 MiB bound.
	args := []string{"-z", prompt, "--usage-file", h.usagePath, "--accept-hooks"}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	cmd := exec.Command(h.Bin(), args...)
	cmd.Dir = req.WorkDir
	return cmd, nil
}

func (h *Hermes) Parser() Parser {
	return &hermesParser{usagePath: h.usagePath}
}

type hermesUsage struct {
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	CostStatus       string  `json:"cost_status"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	SessionID        *string `json:"session_id"`
	Completed        bool    `json:"completed"`
	Failed           bool    `json:"failed"`
	Failure          string  `json:"failure"`
}

type hermesParser struct {
	usagePath string
	lines     []string
	finalized bool
}

func (p *hermesParser) Line(raw []byte) ([]events.Event, *TurnResult, error) {
	p.lines = append(p.lines, string(raw))
	return nil, nil, nil
}

func (p *hermesParser) Finalize() ([]events.Event, *TurnResult, error) {
	if p.finalized {
		return nil, nil, nil
	}
	p.finalized = true
	if p.usagePath == "" {
		return nil, nil, errors.New("hermes usage sidecar path was not configured")
	}
	data, err := os.ReadFile(p.usagePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read hermes usage sidecar: %w", err)
	}
	var usage hermesUsage
	if err := json.Unmarshal(data, &usage); err != nil {
		return nil, nil, fmt.Errorf("parse hermes usage sidecar: %w", err)
	}

	text := strings.TrimSpace(strings.Join(p.lines, "\n"))
	var evs []events.Event
	if text != "" {
		evs = append(evs, events.Event{
			Type: events.TypeText, Actor: "main", Preview: events.Truncate(text),
		})
	}
	res := &TurnResult{
		Result: text, Turns: 1, TokensIn: usage.InputTokens,
		TokensOut: usage.OutputTokens,
		Context:   usage.InputTokens + usage.CacheReadTokens,
	}
	if usage.SessionID != nil {
		res.SessionID = *usage.SessionID
	}
	// Hermes currently reports monetary provider accounting as "estimated" or
	// "exact". Subscription ("included") and unknown statuses must not be
	// presented as zero-dollar metered spend.
	switch strings.ToLower(strings.TrimSpace(usage.CostStatus)) {
	case "estimated", "exact", "metered":
		res.CostUSD = usage.EstimatedCostUSD
	}

	if usage.Failed {
		if res.Result == "" {
			res.Result = strings.TrimSpace(usage.Failure)
		}
		res.State = "failed"
		res.IsError = true
		if hermesAuthError(res.Result) {
			res.State = "auth-required"
		}
		// Error stdout may accidentally contain status-shaped text. It is
		// provider output, never a source for ParseStatusBlock.
		return evs, res, nil
	}
	if !usage.Completed {
		return evs, nil, nil
	}
	state, question, blocked, rest := ParseStatusBlock(text)
	res.State, res.Question, res.Blocked, res.Result = state, question, blocked, rest
	return evs, res, nil
}

func hermesAuthError(s string) bool {
	ls := strings.ToLower(s)
	for _, marker := range []string{
		"not logged in", "hermes portal", "codex login", "401", "unauthorized",
		"invalid api key", "token expired",
	} {
		if strings.Contains(ls, marker) {
			return true
		}
	}
	return false
}
