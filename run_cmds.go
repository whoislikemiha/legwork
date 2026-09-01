package main

import (
	"fmt"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/dispatch"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

func runCmd() *cobra.Command {
	var agent, dir, model, appendPrompt, appendPromptFile, wsID, runLabel, timeout string
	var effort, fallbackModel string
	var readOnly, asJSON bool
	c := &cobra.Command{
		Use:   "run <task>",
		Short: "Start a job; prints the job ID immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedAppendPrompt, err := dispatch.ResolveAppendPrompt(appendPrompt, appendPromptFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			m, err := dispatch.Dispatch(dispatch.Options{
				Agent: agent, Task: args[0], Dir: dir, Workspace: wsID,
				RunLabel: runLabel, Timeout: timeout, Model: model, Effort: effort,
				FallbackModel: fallbackModel, AppendPrompt: resolvedAppendPrompt,
				ReadOnly: readOnly,
			})
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Println(m.ID)
			return nil
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter (claude, codex, hermes, fake)")
	c.Flags().StringVar(&dir, "dir", "", "run in-place in this directory (default: scratch dir)")
	c.Flags().StringVar(&wsID, "workspace", "", "attach the job to a workspace (see: legwork ws new)")
	c.Flags().StringVar(&runLabel, "run", "", "group the job under a run label")
	c.Flags().StringVar(&timeout, "timeout", "", "wall-clock limit for the turn (e.g. 30m); exceeded -> interrupted, session survives")
	c.Flags().StringVar(&model, "model", "", "model override (passed through to the agent)")
	c.Flags().StringVar(&effort, "effort", "", "reasoning effort (low|medium|high|xhigh|max); codex clamps xhigh/max to high")
	c.Flags().StringVar(&fallbackModel, "fallback-model", "", "claude only: model to retry with when overloaded")
	c.Flags().StringVar(&appendPrompt, "append-prompt", "", "orchestrator additions to the injected worker rules")
	c.Flags().StringVar(&appendPromptFile, "append-prompt-file", "", "read orchestrator additions from a UTF-8 text file, or - for stdin")
	c.Flags().BoolVar(&readOnly, "read-only", false, "read-only turn (plan/research)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

// resolveResumeTarget mirrors the read-side selector surface (positional, or
// --job/--run) for the resume-family verbs: with a selector flag the single
// positional is the message; without one the classic <job> <message> form
// applies. Run labels resolve to the newest job, announced on stderr like the
// read commands do.
func resolveResumeTarget(cmd *cobra.Command, verb string, args []string, jobFlag, runFlag string) (id, message string, err error) {
	positional := ""
	switch {
	case len(args) == 2:
		positional, message = args[0], args[1]
	case jobFlag == "" && runFlag == "":
		return "", "", fmt.Errorf("%s needs a target: %s <job> <message>, or --job <id> / --run <label> with just the message", verb, verb)
	default:
		message = args[0]
	}
	s, err := openStore()
	if err != nil {
		return "", "", err
	}
	sel, err := resolveReadSelector(s, positional, jobFlag, runFlag)
	if err != nil {
		return "", "", err
	}
	m := sel.Newest()
	if m == nil {
		return "", "", fmt.Errorf("run %q has no jobs; %s requires a job", sel.Selector, verb)
	}
	resolutionNotice(cmd, sel, m)
	return m.ID, message, nil
}

func resumeFamilyCmd(use, short, verb, eventType string) *cobra.Command {
	var asJSON bool
	var jobID, runLabel string
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, message, err := resolveResumeTarget(cmd, verb, args, jobID, runLabel)
			if err != nil {
				return err
			}
			m, err := dispatch.Resume(id, message, eventType)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Println(m.ID)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().StringVar(&runLabel, "run", "", "target the run label's newest job")
	return c
}

func resumeCmd() *cobra.Command {
	return resumeFamilyCmd("resume [selector] <message> [--job <id> | --run <label>]",
		"Continue a job's session with a new instruction", "resume", events.TypeResume)
}

func answerCmd() *cobra.Command {
	return resumeFamilyCmd("answer [selector] <answer> [--job <id> | --run <label>]",
		"Answer a needs-input question and continue the job", "answer", events.TypeAnswer)
}

func approveCmd() *cobra.Command {
	var asJSON bool
	var provisionTimeout string
	c := &cobra.Command{
		Use:   "approve <job>",
		Short: "Approve a needs-provision command and continue the job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, err := time.ParseDuration(provisionTimeout)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("--timeout must be a positive duration: %q", provisionTimeout)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadMeta(args[0])
			if err != nil {
				return err
			}
			s.Reconcile(m)
			if m.State == job.StateActive {
				return fmt.Errorf("%s is active; cancel it first or wait for the turn to end", m.ID)
			}
			if m.State == job.StateClosed {
				return fmt.Errorf("%s is closed", m.ID)
			}
			if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "provision" {
				return fmt.Errorf("%s is %s, not needs-provision", m.ID, m.State)
			}
			if strings.TrimSpace(m.Blocked.Command) == "" {
				return fmt.Errorf("%s needs-provision has no command", m.ID)
			}
			if m.Workspace != "" {
				if active, err := s.ActiveJobIn(m.Workspace); err != nil {
					return err
				} else if active != "" && active != m.ID {
					return fmt.Errorf("workspace %s has active job %s", m.Workspace, active)
				}
			}
			workDir, err := dispatch.WorkDir(s, m)
			if err != nil {
				return err
			}
			out, exitCode, runErr := dispatch.Provision(workDir, m.Blocked.Command, timeout)
			fields := map[string]any{
				"blocked":   m.Blocked,
				"command":   m.Blocked.Command,
				"exit_code": exitCode,
				"output":    events.Truncate(strings.TrimSpace(out)),
			}
			if runErr != nil {
				if log, lerr := events.Open(s.EventsPath(m.ID)); lerr == nil {
					_, _ = log.Append(events.Event{Type: events.TypeApprove, Actor: "orchestrator",
						Preview: "provision command failed: " + events.Truncate(m.Blocked.Command),
						Fields:  fields})
				}
				return fmt.Errorf("provision command failed: %v\n%s", runErr, strings.TrimSpace(out))
			}
			message := "Provisioning command was approved and completed outside the sandbox. Continue the task.\n\nCommand: " + m.Blocked.Command
			resumed, err := dispatch.ResumeWithEvent(m.ID, message, events.TypeApprove,
				"approved provision: "+m.Blocked.Command, fields)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(resumed)
			}
			fmt.Println(resumed.ID)
			return nil
		},
	}
	c.Flags().StringVar(&provisionTimeout, "timeout", "30m", "wall-clock limit for the provision command")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func ackCmd() *cobra.Command {
	var asJSON, force bool
	c := &cobra.Command{
		Use:   "ack <job>",
		Short: "Acknowledge a terminal workspace-less job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadMeta(args[0])
			if err != nil {
				return workspaceSelectorHint(args[0], err)
			}
			s.Reconcile(m)
			if m.Workspace != "" {
				return fmt.Errorf("%s belongs to workspace %s; close the workspace with legwork close %s", m.ID, m.Workspace, m.Workspace)
			}
			if m.State == job.StateClosed {
				return fmt.Errorf("%s is already closed", m.ID)
			}
			switch m.State {
			case job.StateActive:
				return fmt.Errorf("%s is active; cancel it first or wait for the turn to end", m.ID)
			case job.StateQueued:
				return fmt.Errorf("%s is queued; cancel it first or wait for the turn to start", m.ID)
			}
			if !job.Terminal(m.State) && !force {
				return fmt.Errorf("%s is %s; only terminal jobs can be acknowledged without --force", m.ID, m.State)
			}
			if err := s.Close(m); err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Printf("%s acknowledged\n", m.ID)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "acknowledge a non-terminal workspace-less job after explicit operator review")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func cancelCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cancel <job>",
		Short: "Interrupt the running turn (session survives; resume later)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadMeta(args[0])
			if err != nil {
				return err
			}
			if m.State != job.StateActive || m.RunnerPID == 0 {
				return fmt.Errorf("%s is not active", m.ID)
			}
			// The runner is a session leader: signal its whole process group
			// so the agent child gets it too.
			if err := syscall.Kill(-m.RunnerPID, syscall.SIGINT); err != nil {
				return err
			}
			log, err := events.Open(s.EventsPath(m.ID))
			if err == nil {
				_, _ = log.Append(events.Event{Type: events.TypeCancel, Actor: "orchestrator"})
			}
			fmt.Printf("%s: interrupt sent\n", m.ID)
			return nil
		},
	}
	return c
}
