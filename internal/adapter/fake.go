package adapter

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/fakeagent"
)

// Fake is the contract-test agent: it spawns this same binary's hidden
// `_fake-agent` subcommand, which replays a scripted stream (path in
// LEGWORK_FAKE_SCRIPT). It emits claude-shaped stream-json, so the whole
// pipeline — spawn, detach, tee, parse, status block — is exercised for real
// with zero API spend, including misbehavior (mid-turn death, missing status
// block) that a live agent can't produce on demand.
type Fake struct{}

func (f *Fake) Name() string { return "fake" }

// Bin is this same binary: the fake adapter execs os.Executable()'s hidden
// _fake-agent subcommand, so doctor's agent check is always satisfied.
func (f *Fake) Bin() string {
	self, _ := os.Executable()
	return self
}

func (f *Fake) Caps() Caps {
	return Caps{Fork: false, OSSandbox: false, StructuredStatus: "convention", Subagents: false}
}

func (f *Fake) Command(req TurnRequest) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "_fake-agent")
	cmd.Dir = req.WorkDir
	cmd.Env = append(os.Environ(), fakeagent.TempDirEnv+"="+req.TempDir)
	return cmd, nil
}

// Parser selects the production parser to drive the fake stream through. This
// lets the e2e suite exercise a real dialect's parser (codex), or the EOF
// finalization contract with plain text, at zero spend. Default stays
// claude-shaped.
func (f *Fake) Parser() Parser {
	switch os.Getenv("LEGWORK_FAKE_PARSER") {
	case "codex":
		return &codexParser{}
	case "final-only":
		return &finalOnlyFakeParser{}
	case "finalize-error":
		return &finalOnlyFakeParser{finalizeErr: errors.New("scripted finalize failure")}
	}
	return &claudeParser{}
}

// finalOnlyFakeParser is the production fake dialect for exercising agents
// that emit only their final response text. Hermes supplies its own parser;
// this deliberately models only the shared EOF contract.
type finalOnlyFakeParser struct {
	lines       []string
	finalized   bool
	finalizeErr error
}

func (p *finalOnlyFakeParser) Line(raw []byte) ([]events.Event, *TurnResult, error) {
	p.lines = append(p.lines, string(raw))
	return nil, nil, nil
}

func (p *finalOnlyFakeParser) Finalize() ([]events.Event, *TurnResult, error) {
	if p.finalizeErr != nil {
		return nil, nil, p.finalizeErr
	}
	if p.finalized || len(p.lines) == 0 {
		return nil, nil, nil
	}
	p.finalized = true
	text := strings.Join(p.lines, "\n")
	state, question, blocked, rest := ParseStatusBlock(text)
	return nil, &TurnResult{
		State: state, Question: question, Blocked: blocked, Result: rest, Turns: 1,
	}, nil
}
