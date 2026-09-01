package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/transcript"
)

type resultOut struct {
	Job          string `json:"job"`
	Run          string `json:"run,omitempty"`
	Turn         int    `json:"turn,omitempty"`
	State        string `json:"state"`
	Result       string `json:"result"`
	Selector     string `json:"selector"`
	SelectorKind string `json:"selector_kind"`
	ResolvedJob  string `json:"resolved_job"`
}

func resultCmd() *cobra.Command {
	var asJSON bool
	var turn int
	var jobID, runLabel string
	c := &cobra.Command{
		Use:   "result [selector] [--job <id> | --run <label>]",
		Short: "Print a final report; a run selector resolves to its newest job",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("turn") && turn <= 0 {
				return fmt.Errorf("--turn must be positive")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			positional := ""
			if len(args) == 1 {
				positional = args[0]
			}
			sel, err := resolveReadSelector(s, positional, jobID, runLabel)
			if err != nil {
				return err
			}
			m := sel.Newest()
			if m == nil {
				return fmt.Errorf("run %q has no jobs; result requires a job", sel.Selector)
			}
			s.Reconcile(m)

			var res resultOut
			if turn > 0 {
				res, err = retainedResult(s, m, turn)
				if err != nil {
					return err
				}
			} else {
				if m.State == job.StateQueued || m.State == job.StateActive {
					return commandError{code: 1, message: fmt.Sprintf("%s has no result yet", m.ID)}
				}
				res = resultOut{Job: m.ID, Run: m.Run, State: string(m.State), Result: m.Result}
				if asJSON {
					n, err := retainedResultCount(s, m)
					if err != nil {
						return err
					}
					res.Turn = n
				}
			}

			res.Selector, res.SelectorKind, res.ResolvedJob = sel.Selector, string(sel.Kind), m.ID
			if asJSON {
				return printJSON(res)
			}
			resolutionNotice(cmd, sel, m)
			fmt.Print(res.Result)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().IntVar(&turn, "turn", 0, "print the Nth retained turn result (1-based)")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().StringVar(&runLabel, "run", "", "force a run label selector")
	return c
}

func retainedResult(s *job.Store, m *job.Meta, turn int) (resultOut, error) {
	results, err := transcript.Results(s, m)
	if err != nil {
		return resultOut{}, err
	}
	if turn == 0 || turn > len(results) {
		return resultOut{}, fmt.Errorf("%s turn %d result is not retained", m.ID, turn)
	}
	res := results[turn-1]
	return resultOut{Job: m.ID, Run: m.Run, Turn: turn, State: res.State, Result: res.Result}, nil
}

func retainedResultCount(s *job.Store, m *job.Meta) (int, error) {
	results, err := transcript.Results(s, m)
	if err != nil {
		return 0, err
	}
	return len(results), nil
}
