package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/fakeagent"
	"github.com/whoislikemiha/legwork/internal/runner"
)

// fakeAgentCmd must ship in the real binary: the fake adapter execs
// os.Executable() so tests exercise the actual spawn path.
func fakeAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_fake-agent",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fakeagent.Replay(os.Stdout)
		},
	}
}

func runnerCmd() *cobra.Command {
	var jobID string
	c := &cobra.Command{
		Use:    "_runner",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			return runner.Run(s, jobID)
		},
	}
	c.Flags().StringVar(&jobID, "job", "", "job id")
	_ = c.MarkFlagRequired("job")
	return c
}
