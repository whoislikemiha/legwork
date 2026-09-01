package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/job"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		code := 2
		if e, ok := err.(interface{ ExitCode() int }); ok {
			code = e.ExitCode()
		}
		if e, ok := err.(interface{ Silent() bool }); !ok || !e.Silent() {
			fmt.Fprintf(os.Stderr, "legwork: %v\n", err)
		}
		os.Exit(code)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "legwork",
		Short: "Delegate the legwork to headless coding agents",
		Long: `Delegate the legwork to headless coding agents: dispatch tasks as supervised
jobs, observe structured events, review diffs, steer with follow-up turns.

The loop: run -> (notification or status) -> done? verify : answer/resume -> close.
Run 'legwork guide' for the full orchestrator guide (notifications, workspaces,
health, recipes).`,
		Version:       versionSummary(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(runCmd(), resumeCmd(), answerCmd(), approveCmd(), verifyCmd(), statusCmd(), eventsCmd(),
		resultCmd(), lsCmd(), watchCmd(), cancelCmd(), ackCmd(), wsCmd(), diffCmd(), closeCmd(),
		noteCmd(), doctorCmd(), gcCmd(), guideCmd(), runnerCmd(), fakeAgentCmd(),
		runsCmd(), tailCmd(), dashboardCmd(), serveCmd(), artifactCmd(), versionCmd(), rulesCmd(),
		skillCmd(), waitCmd())
	root.SetFlagErrorFunc(flagErrorHint)
	return root
}

// flagErrorHint enriches cobra's unknown-flag errors on the resume-family
// verbs. Orchestrators habitually assume dispatch-time flags work everywhere
// (F2 in eval/FINDINGS.md); when the guess fails, name where the flag actually
// lives so the retry is right instead of another guess.
func flagErrorHint(cmd *cobra.Command, err error) error {
	msg := err.Error()
	flag, ok := strings.CutPrefix(msg, "unknown flag: ")
	if !ok {
		return err
	}
	switch cmd.Name() {
	case "resume", "answer", "approve":
		switch flag {
		case "--agent", "--model", "--effort", "--append-prompt", "--append-prompt-file", "--fallback-model", "--read-only":
			return fmt.Errorf("%s: %s is set at dispatch (legwork run / ws review); %s continues the job's session with its existing agent, model, and rules", msg, flag, cmd.Name())
		}
	}
	return err
}

type commandError struct {
	code    int
	message string
	silent  bool
}

func (e commandError) Error() string { return e.message }

func (e commandError) ExitCode() int { return e.code }

func (e commandError) Silent() bool { return e.silent }

func openStore() (*job.Store, error) { return job.OpenStore() }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
