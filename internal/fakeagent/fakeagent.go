// Package fakeagent is the contract-test agent behind the hidden
// `legwork _fake-agent` subcommand. It replays a scripted stream of
// claude-shaped stream-json lines so the full pipeline — spawn, detach, tee,
// parse, status block — is exercised for real with zero API spend, including
// misbehavior a live agent can't produce on demand.
//
// Script directives:
//
//	#sleep <ms>            pause (readiness/watch/cancel tests)
//	#die                   exit 1 mid-turn (interrupted-state tests)
//	#write <path> <text>   write a file relative to the cwd (workspace tests)
//
// Any other non-empty line is emitted verbatim.
package fakeagent

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// ScriptEnv names the script file; without it Replay plays a minimal happy
// path ending in state: done.
const ScriptEnv = "LEGWORK_FAKE_SCRIPT"

// Replay writes the scripted stream to w.
func Replay(w io.Writer) error {
	script := os.Getenv(ScriptEnv)
	if script == "" {
		fmt.Fprintln(w, `{"type":"system","subtype":"init","session_id":"fake-session-1"}`)
		fmt.Fprintln(w, `{"type":"assistant","message":{"content":[{"type":"text","text":"working on it"}]}}`)
		fmt.Fprintln(w, `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50},"session_id":"fake-session-1","result":"did the thing\n\nstate: done"}`)
		return nil
	}
	f, err := os.Open(script)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#sleep "):
			ms, _ := strconv.Atoi(strings.TrimPrefix(line, "#sleep "))
			time.Sleep(time.Duration(ms) * time.Millisecond)
		case line == "#die":
			os.Exit(1)
		case strings.HasPrefix(line, "#write "):
			parts := strings.SplitN(strings.TrimPrefix(line, "#write "), " ", 2)
			if len(parts) == 2 {
				if err := os.WriteFile(parts[0], []byte(parts[1]+"\n"), 0o644); err != nil {
					return err
				}
			}
		case strings.TrimSpace(line) == "":
		default:
			fmt.Fprintln(w, line)
		}
	}
	return sc.Err()
}
