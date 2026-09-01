package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/job"
)

// waitOut is deliberately a small envelope around the persisted job record:
// scripts get the same final metadata that status reads, plus the wait result
// without having to infer it from an exit status alone.
type waitOut struct {
	Job       *job.Meta   `json:"job"`
	Outcome   string      `json:"outcome"` // reached | timeout | terminal-mismatch
	Reached   job.State   `json:"reached"`
	WaitedFor []job.State `json:"waited_for"`
	ElapsedMS int64       `json:"elapsed_ms"`
}

// waitCmd is the exact-job counterpart to tail --until-idle. It reloads the
// persisted metadata on each pass so a detached runner's terminal write wins
// over any older snapshot held by this process. Reconcile is safe here: it is
// the shared dead-runner path and reloads before it ever persists interruption.
func waitCmd() *cobra.Command {
	var until, timeout string
	var asJSON bool
	c := &cobra.Command{
		Use:   "wait <job>",
		Short: "Block until one exact job reaches attention or a requested state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wanted, err := parseWaitStates(until, cmd.Flags().Changed("until"))
			if err != nil {
				return err
			}
			var deadline time.Time
			if cmd.Flags().Changed("timeout") {
				d, err := time.ParseDuration(timeout)
				if err != nil || d <= 0 {
					return fmt.Errorf("--timeout must be a positive duration")
				}
				deadline = time.Now().Add(d)
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			// Validate the exact ID before starting the clock. Unlike the other
			// read commands, wait intentionally never resolves a run label.
			if _, err := s.LoadMeta(args[0]); err != nil {
				return err
			}

			started := time.Now()
			for {
				m, err := s.LoadMeta(args[0])
				if err != nil {
					return err
				}
				s.Reconcile(m)

				outcome := ""
				if len(wanted) == 0 && !waitLive(m.State) {
					outcome = "reached"
				} else if len(wanted) > 0 && wanted[m.State] {
					outcome = "reached"
				} else if len(wanted) > 0 && !waitLive(m.State) {
					outcome = "terminal-mismatch"
				} else if !deadline.IsZero() && !time.Now().Before(deadline) {
					outcome = "timeout"
				}

				if outcome != "" {
					return finishWait(cmd, asJSON, m, outcome, waitStatesForOutput(wanted), time.Since(started))
				}

				delay := 100 * time.Millisecond
				if !deadline.IsZero() {
					remaining := time.Until(deadline)
					if remaining > 0 && remaining < delay {
						delay = remaining
					}
				}
				time.Sleep(delay)
			}
		},
	}
	c.Flags().StringVar(&until, "until", "", "comma-separated job states to wait for")
	c.Flags().StringVar(&timeout, "timeout", "", "positive maximum wait duration")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func parseWaitStates(raw string, supplied bool) (map[job.State]bool, error) {
	if !supplied {
		return nil, nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--until requires at least one state")
	}
	return parseJobStates(raw)
}

func waitLive(state job.State) bool {
	return state == job.StateQueued || state == job.StateActive
}

func waitStatesForOutput(wanted map[job.State]bool) []job.State {
	if len(wanted) == 0 {
		return []job.State{}
	}
	states := make([]job.State, 0, len(wanted))
	for state := range wanted {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })
	return states
}

func finishWait(cmd *cobra.Command, asJSON bool, m *job.Meta, outcome string, waitedFor []job.State, elapsed time.Duration) error {
	out := waitOut{Job: m, Outcome: outcome, Reached: m.State, WaitedFor: waitedFor, ElapsedMS: elapsed.Milliseconds()}
	if asJSON {
		if err := printJSON(out); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (%s; %dms)\n", m.ID, m.State, outcome, out.ElapsedMS)
	}
	if outcome == "reached" {
		return nil
	}
	return commandError{code: 1, silent: true}
}
