package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/config"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/verify"
)

// metaOut wraps a persisted Meta with the derived context_high signal, so
// --json carries it without touching the persisted struct. omitempty means the
// field only appears when high — additive, no schema break. Attention and
// NextActions share ws status's vocabulary (additive; only status populates
// them, ls stays a compact listing).
type metaOut struct {
	*job.Meta
	ContextHigh  bool             `json:"context_high,omitempty"`
	Selector     string           `json:"selector"`
	SelectorKind string           `json:"selector_kind"`
	ResolvedJob  string           `json:"resolved_job"`
	Attention    []string         `json:"attention,omitempty"`
	NextActions  []wsStatusAction `json:"next_actions,omitempty"`
}

// jobStatusAdvice derives the deterministic attention/next_actions codes for
// one job, in the same vocabulary ws status uses for a whole workspace. In
// particular, blocked.kind=verify points at verification — never at a generic
// resume that would rewrite the worker's blocked turn.
func jobStatusAdvice(m *job.Meta, contextHigh bool) (attention []string, next []wsStatusAction) {
	switch {
	case m.State == job.StateActive || m.State == job.StateQueued:
		next = append(next, wsStatusAction{Action: "wait",
			Reason: m.ID + " is " + string(m.State), Command: "legwork wait " + m.ID})
	case m.State == job.StateNeedsInput:
		attention = append(attention, "needs-input")
		next = append(next, wsStatusAction{Action: "answer",
			Reason:  m.ID + " asked: " + events.Truncate(m.Question),
			Command: "legwork answer " + m.ID + " \"<decision>\""})
	case m.State == job.StateBlocked && m.Blocked != nil && m.Blocked.Kind == "verify":
		attention = append(attention, "blocked-verify")
		cmd := "legwork verify " + m.ID + " -- <argv...>"
		if m.Blocked.Command != "" {
			cmd = shellCommand([]string{"legwork", "verify", m.ID, "--", "sh", "-lc", m.Blocked.Command})
		}
		next = append(next, wsStatusAction{Action: "verify",
			Reason: m.ID + " requested host verification", Command: cmd})
	case m.State == job.StateBlocked && m.Blocked != nil && m.Blocked.Kind == "provision":
		attention = append(attention, "needs-provision")
		next = append(next, wsStatusAction{Action: "approve",
			Reason:  m.ID + " wants: " + events.Truncate(m.Blocked.Command),
			Command: "legwork approve " + m.ID})
	case m.State == job.StateBlocked:
		attention = append(attention, "blocked")
		reason := "worker is blocked"
		if m.Blocked != nil {
			reason = "worker is blocked (" + m.Blocked.Kind + "): " + events.Truncate(m.Blocked.Detail)
		}
		next = append(next, wsStatusAction{Action: "inspect", Reason: reason,
			Command: "legwork events " + m.ID})
	case m.State == job.StateAuthNeeded:
		attention = append(attention, "auth-required")
		next = append(next, wsStatusAction{Action: "escalate",
			Reason: "agent login needed on this machine (human action)"})
	case m.State == job.StateInterrupted:
		attention = append(attention, "interrupted")
		next = append(next, wsStatusAction{Action: "resume",
			Reason:  "turn died mid-flight; the session survives",
			Command: "legwork resume " + m.ID + " \"<instruction>\""})
	case m.State == job.StateFailed:
		attention = append(attention, "failed")
		next = append(next, wsStatusAction{Action: "inspect",
			Reason:  "read the events, then retry as a fresh job or resume",
			Command: "legwork events " + m.ID})
	case m.State == job.StateDone && m.Workspace != "":
		next = append(next, wsStatusAction{Action: "workspace",
			Reason:  "turn done; review and land via the workspace",
			Command: "legwork ws status " + m.Workspace})
	case m.State == job.StateDone:
		next = append(next, wsStatusAction{Action: "ack",
			Reason:  "verify the result first, then acknowledge",
			Command: "legwork ack " + m.ID})
	case m.State == job.StateClosed:
		next = append(next, wsStatusAction{Action: "none", Reason: "job is closed"})
	}
	if contextHigh {
		attention = append(attention, "context-high")
	}
	return attention, next
}

func resolveReadSelector(s *job.Store, positional, jobFlag, runFlag string) (job.Selection, error) {
	if positional != "" && (jobFlag != "" || runFlag != "") {
		return job.Selection{}, fmt.Errorf("a positional selector cannot be combined with --job or --run")
	}
	if jobFlag != "" && runFlag != "" {
		return job.Selection{}, fmt.Errorf("--job and --run are mutually exclusive")
	}
	var sel job.Selection
	var err error
	switch {
	case jobFlag != "":
		sel, err = job.Resolve(s, jobFlag, job.SelectorJob)
	case runFlag != "":
		sel, err = job.Resolve(s, runFlag, job.SelectorRun)
	default:
		sel, err = job.Resolve(s, positional, "")
	}
	if err != nil {
		selector := positional
		if jobFlag != "" {
			selector = jobFlag
		} else if runFlag != "" {
			selector = runFlag
		}
		err = workspaceSelectorHint(selector, err)
	}
	return sel, err
}

// workspaceSelectorHint targets the observed weak-tier trap (F3 in
// eval/FINDINGS.md): a workspace ID handed to a job selector ("result ws-1").
// When the failed selector names an existing workspace, say so and point at
// the workspace surfaces instead of the generic no-such-job error.
func workspaceSelectorHint(selector string, err error) error {
	if !strings.HasPrefix(selector, "ws-") {
		return err
	}
	_, wss, werr := openWorkspaces()
	if werr != nil {
		return err
	}
	if _, lerr := wss.Load(selector); lerr != nil {
		return err
	}
	return fmt.Errorf("%s is a workspace, not a job; list its jobs with legwork ls --workspace %s, or use the workspace verbs (ws, diff, close)", selector, selector)
}

// resolutionNotice is deliberately stderr-only: result's stdout is the raw
// worker report and JSON output must remain machine-consumable.
func resolutionNotice(cmd *cobra.Command, sel job.Selection, resolved *job.Meta) {
	if sel.Kind == job.SelectorRun {
		fmt.Fprintf(cmd.ErrOrStderr(), "resolved run %q to newest job %s\n", sel.Selector, resolved.ID)
	}
}

func statusCmd() *cobra.Command {
	var asJSON bool
	var jobID, runLabel string
	c := &cobra.Command{
		Use:   "status [selector] [--job <id> | --run <label>]",
		Short: "Job rollup; a run selector resolves to its newest job",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				return fmt.Errorf("run %q has no jobs; status requires a job", sel.Selector)
			}
			s.Reconcile(m)
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			high := m.ContextHigh(health.ContextThreshold)
			attention, next := jobStatusAdvice(m, high)
			if asJSON {
				// latest_verification is a current rollup in the public status
				// surface. Do not make an old-turn receipt look current to JSON
				// consumers that do not reconstruct the binding themselves.
				out := *m
				if out.CurrentVerification() == nil {
					out.LatestVerification = nil
				}
				return printJSON(metaOut{Meta: &out, ContextHigh: high, Selector: sel.Selector,
					SelectorKind: string(sel.Kind), ResolvedJob: m.ID,
					Attention: attention, NextActions: next})
			}
			resolutionNotice(cmd, sel, m)
			fmt.Printf("job:    %s (%s)\nstate:  %s\ntask:   %s\n", m.ID, m.Agent, m.State, m.Task)
			if m.Run != "" {
				fmt.Printf("run:    %s\n", m.Run)
			}
			fmt.Printf("turns: %d  context: %s  tokens: %d in / %d out  cost: $%.4f\n",
				m.Turns, fmtContext(m.Context), m.TokensIn, m.TokensOut, m.CostUSD)
			if high {
				fmt.Printf("hint:   context high — prefer a fresh job over resume\n")
			}
			if m.Question != "" {
				fmt.Printf("question: %s\n", m.Question)
			}
			if m.Blocked != nil {
				fmt.Printf("blocked: %s", m.Blocked.Kind)
				if m.Blocked.Command != "" {
					fmt.Printf(" command=%q", m.Blocked.Command)
				}
				if m.Blocked.Detail != "" {
					fmt.Printf(" detail=%q", m.Blocked.Detail)
				}
				fmt.Println()
			}
			if r := m.CurrentVerification(); r != nil {
				state := "failed"
				if r.Passed {
					state = "passed (reviewable)"
				} else if r.TimedOut {
					state = "timed out"
				}
				fmt.Printf("verification: %s  receipt: %s\n", state, r.ReceiptID)
				if !r.Passed {
					fmt.Printf("retry: %s\n", shellCommand(verify.RetryArgv(m.ID, r.Argv)))
				}
			}
			if m.State == job.StateClosed && m.LastOutcome != nil {
				fmt.Printf("last outcome: %s", m.LastOutcome.State)
				if m.LastOutcome.Reason != "" {
					fmt.Printf(" — %s", m.LastOutcome.Reason)
				}
				fmt.Println()
			}
			if m.Result != "" {
				fmt.Printf("result:\n%s\n", m.Result)
			}
			for _, a := range next {
				line := "next: " + a.Action + " — " + a.Reason
				if a.Command != "" {
					line += "\n      " + a.Command
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().StringVar(&runLabel, "run", "", "force a run label selector")
	return c
}

// fmtContext renders the session context footprint in tokens. No percentage:
// window sizes vary per model (an Opus session measured 280k — a hardcoded
// 200k window would render nonsense). Raw magnitude is the honest signal.
func fmtContext(tokens int) string {
	if tokens == 0 {
		return "-"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	return fmt.Sprintf("%dk", tokens/1000)
}
