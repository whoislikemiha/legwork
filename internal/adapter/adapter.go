package adapter

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/whoislikemiha/legwork/internal/events"
)

// Caps declares per-agent capabilities; the tool never pretends agents are
// identical (DESIGN.md §3).
type Caps struct {
	Fork             bool   // session forking
	OSSandbox        bool   // kernel-enforced sandbox (codex)
	StructuredStatus string // "enforced" | "convention"
	Subagents        bool
}

// TurnRequest describes one headless turn.
type TurnRequest struct {
	Task         string
	SystemPrompt string // injected worker rules + orchestrator additions
	SessionID    string // resume this session if set
	Model        string
	WorkDir      string
	TempDir      string
	ReadOnly     bool
	// Effort and FallbackModel are claude-specific passthroughs; other
	// adapters ignore them (the run command rejects them for non-claude).
	Effort        string // --effort: low|medium|high|xhigh|max
	FallbackModel string // --fallback-model: model to retry with when overloaded
}

// TurnResult is the normalized outcome of a completed turn.
type TurnResult struct {
	State     string // done | needs-input | blocked | failed | auth-required
	Question  string // set when needs-input
	Blocked   *BlockedReason
	Result    string // final text with the status block stripped
	SessionID string
	CostUSD   float64
	Turns     int
	TokensIn  int
	TokensOut int
	// Context is the session's current context footprint in tokens (fresh
	// input + cache reads/writes on the last call). On subscription plans
	// cost is nominal — context is the real health metric: high context +
	// stale diff = a spinning worker.
	Context int
	IsError bool
}

// BlockedReason is the structured reason emitted with state: blocked.
type BlockedReason struct {
	Kind    string `json:"kind,omitempty"` // provision | verify | decision
	Detail  string `json:"detail,omitempty"`
	Command string `json:"command,omitempty"` // required for provision
}

// Adapter normalizes one agent CLI to the legwork contract.
type Adapter interface {
	Name() string
	Caps() Caps
	// Bin is the executable that must be on PATH for this agent (or an
	// absolute path for self-hosted agents). Used by `legwork doctor` to
	// check presence and capture a version without hardcoding binaries.
	Bin() string
	// Command builds the process for one turn. Stdout must be a stream the
	// adapter's Parser understands, one JSON object per line.
	Command(req TurnRequest) (*exec.Cmd, error)
	// Parser returns a fresh stream parser for one turn.
	Parser() Parser
}

// Parser consumes raw stdout lines and produces normalized index events and,
// on the final line, a TurnResult.
type Parser interface {
	// Line parses one raw line. Returned events are appended to the index;
	// result is non-nil exactly once, on the turn's final line.
	Line(raw []byte) (evs []events.Event, result *TurnResult, err error)
}

// New returns the adapter for name.
func New(name string) (Adapter, error) {
	switch name {
	case "claude":
		return &Claude{}, nil
	case "codex":
		return &Codex{}, nil
	case "fake":
		return &Fake{}, nil
	default:
		return nil, fmt.Errorf("unknown agent %q (available: claude, codex, fake)", name)
	}
}

// --- status block convention ---

// The injected worker rules require every turn to end with:
//
//	state: done | needs-input | blocked
//	question: <one line, iff needs-input>
//	blocked: {"kind":"provision|verify|decision",...} (iff blocked)
//
// parsed from the tail of the final message.
var (
	stateRe    = regexp.MustCompile(`(?mi)^state:\s*(done|needs-input|blocked)\s*$`)
	questionRe = regexp.MustCompile(`(?mi)^question:\s*(.+)$`)
	blockedRe  = regexp.MustCompile(`(?mis)^blocked:\s*(\{.*\})\s*$`)
)

// ParseStatusBlock extracts the status convention from a final message.
// A missing/unparseable block yields state "blocked" (needs-review): never
// assume done (DESIGN.md §3).
func ParseStatusBlock(text string) (state, question string, blocked *BlockedReason, rest string) {
	m := stateRe.FindStringSubmatch(text)
	if m == nil {
		return "blocked", "", nil, strings.TrimSpace(text)
	}
	state = strings.ToLower(m[1])
	// A question is only meaningful on needs-input; agents sometimes emit
	// filler like "question: N/A" on other states.
	if state == "needs-input" {
		if qm := questionRe.FindStringSubmatch(text); qm != nil {
			question = strings.TrimSpace(qm[1])
		}
	}
	if state == "blocked" {
		if bm := blockedRe.FindStringSubmatch(text); bm != nil {
			var br BlockedReason
			if err := json.Unmarshal([]byte(bm[1]), &br); err == nil && validBlockedReason(&br) {
				br.Kind = strings.ToLower(strings.TrimSpace(br.Kind))
				br.Detail = strings.TrimSpace(br.Detail)
				br.Command = strings.TrimSpace(br.Command)
				blocked = &br
			}
		}
	}
	rest = stateRe.ReplaceAllString(text, "")
	rest = questionRe.ReplaceAllString(rest, "")
	rest = blockedRe.ReplaceAllString(rest, "")
	return state, question, blocked, strings.TrimSpace(rest)
}

func validBlockedReason(br *BlockedReason) bool {
	switch strings.ToLower(strings.TrimSpace(br.Kind)) {
	case "provision":
		return strings.TrimSpace(br.Command) != ""
	case "verify", "decision":
		return true
	}
	return false
}
