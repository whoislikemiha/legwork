package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// fakeAgentCmd is the contract-test agent. It replays the script file named
// by LEGWORK_FAKE_SCRIPT to stdout, one line at a time, emitting
// claude-shaped stream-json. Directives:
//
//	#sleep <ms>   pause (readiness/watch tests)
//	#die          exit 1 mid-turn (interrupted-state tests)
//
// Without a script it plays a minimal happy path ending in state: done.
func fakeAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_fake-agent",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			script := os.Getenv("LEGWORK_FAKE_SCRIPT")
			if script == "" {
				fmt.Println(`{"type":"system","subtype":"init","session_id":"fake-session-1"}`)
				fmt.Println(`{"type":"assistant","message":{"content":[{"type":"text","text":"working on it"}]}}`)
				fmt.Println(`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50},"session_id":"fake-session-1","result":"did the thing\n\nstate: done"}`)
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
				case strings.TrimSpace(line) == "":
				default:
					fmt.Println(line)
				}
			}
			return sc.Err()
		},
	}
}
