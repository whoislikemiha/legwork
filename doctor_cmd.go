package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/doctor"
)

// doctorCmd is preflight: validate the exact agent/model a run would use, plus
// the state dir, git, workstree pairing, and notifier — before dispatching.
// Exit 0 = no failures, 1 = one or more checks failed (report still printed),
// 2 = usage error (unknown agent, from adapter.New).
func doctorCmd() *cobra.Command {
	var agent, model, dir string
	var noProbe, asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Preflight: agent binary, auth, model, state dir, notifier before dispatching",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ad, err := adapter.New(agent)
			if err != nil {
				return err // unknown agent is a usage error -> exit 2
			}
			checks, ok := doctor.Run(doctor.Options{
				Adapter: ad, Model: model, Dir: dir, NoProbe: noProbe,
			})
			if asJSON {
				_ = printJSON(doctor.Report{OK: ok, Checks: checks})
			} else {
				for _, ck := range checks {
					fmt.Printf("%-10s %-5s %s\n", ck.Name, ck.Status, ck.Detail)
				}
			}
			if !ok {
				// Report already printed; signal failure without turning a
				// normal preflight failure into a usage/internal (exit 2) error.
				return commandError{code: 1, silent: true}
			}
			return nil
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter to validate (claude, codex, hermes, fake)")
	c.Flags().StringVar(&model, "model", "", "model to validate (default: agent default)")
	c.Flags().StringVar(&dir, "dir", "", "repo to check for the worktree.toml/workstree pairing (default: cwd)")
	c.Flags().BoolVar(&noProbe, "no-probe", false, "skip the paid live-turn check (static checks only, offline-safe)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
