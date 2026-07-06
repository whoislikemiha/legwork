package adapter

import (
	"os"
	"os/exec"
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
	cmd.Env = os.Environ()
	return cmd, nil
}

func (f *Fake) Parser() Parser { return &claudeParser{} }
