package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

func openWorkspaces() (*job.Store, *workspace.Store, error) {
	s, err := openStore()
	if err != nil {
		return nil, nil, err
	}
	ws, err := workspace.Open(s.Root)
	if err != nil {
		return nil, nil, err
	}
	return s, ws, nil
}

// activeJobIn enforces the one-active-job-per-workspace lock.
func activeJobIn(s *job.Store, wsID string) (string, error) {
	metas, err := s.List()
	if err != nil {
		return "", err
	}
	for _, m := range metas {
		if m.Workspace != wsID {
			continue
		}
		s.Reconcile(m)
		if m.State == job.StateActive || m.State == job.StateQueued || m.VerificationLeaseLive(time.Now().UTC()) {
			return m.ID, nil
		}
	}
	return "", nil
}

func wsCmd() *cobra.Command {
	ws := &cobra.Command{
		Use:   "ws",
		Short: "Manage workspaces (worktree + branch + diff + review gate)",
	}

	var repo, base string
	var asJSON bool
	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Create a workspace: worktree on a namespaced branch, workstree bootstrap",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Create(repo, base)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Printf("%s\n  tree:   %s\n  branch: %s\n  setup:  %s\n", m.ID, m.Tree, m.Branch, m.Setup)
			return nil
		},
	}
	newCmd.Flags().StringVar(&repo, "repo", ".", "source repo")
	newCmd.Flags().StringVar(&base, "base", "", "base ref (default: current HEAD)")
	newCmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")

	var lsJSON bool
	lsCmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List workspaces",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			metas, err := wss.List()
			if err != nil {
				return err
			}
			if lsJSON {
				out := make([]*workspace.Meta, 0, len(metas))
				for _, m := range metas {
					copy := *m
					if workspaceCurrentVerification(s, m) == nil {
						copy.LatestVerification = nil
					}
					out = append(out, &copy)
				}
				return printJSON(out)
			}
			for _, m := range metas {
				state := m.State
				if m.Disposition != "" {
					state += "/" + m.Disposition
				}
				if r := workspaceCurrentVerification(s, m); r != nil {
					if r.Passed {
						state += "/verified"
					} else if r.TimedOut {
						state += "/verify-timeout"
					} else {
						state += "/verify-failed"
					}
				}
				fmt.Printf("%-8s %-14s ckpts:%-3d %6s  %s\n",
					m.ID, state, m.Checkpoints, time.Since(m.Updated).Round(time.Second), m.Repo)
			}
			return nil
		},
	}
	lsCmd.Flags().BoolVar(&lsJSON, "json", false, "JSON output")

	var message string
	var commitJSON bool
	commitCmd := &cobra.Command{
		Use:   "commit <workspace> -m <message>",
		Short: "Commit the workspace diff as the orchestrator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Load(args[0])
			if err != nil {
				return err
			}
			if id, err := activeJobIn(s, m.ID); err != nil {
				return err
			} else if id != "" {
				return fmt.Errorf("%s has active job %s; wait for the turn or cancel it before committing", m.ID, id)
			}
			res, err := wss.Commit(m, message)
			if err != nil {
				return err
			}
			if err := appendWorkspaceCommitEvents(s, m, message, res); err != nil {
				wss.RecordCommitHistoryError(m, res.Receipt, err)
			}
			if commitJSON {
				out := workspaceCommitOutput{
					Workspace:   m.ID,
					Branch:      m.Branch,
					OID:         res.OID,
					Summary:     res.Summary,
					FinalCommit: res.Receipt,
				}
				return printJSON(out)
			}
			if res.Receipt != nil && res.Receipt.HistoryError != "" {
				fmt.Printf("%s committed %s (history warning: %s)\n", m.ID, res.OID, res.Receipt.HistoryError)
			} else {
				fmt.Printf("%s committed %s\n", m.ID, res.OID)
			}
			return nil
		},
	}
	commitCmd.Flags().StringVarP(&message, "message", "m", "", "commit message")
	commitCmd.Flags().BoolVar(&commitJSON, "json", false, "JSON output")
	if err := commitCmd.MarkFlagRequired("message"); err != nil {
		panic(err)
	}

	var reviewAgent, reviewModel, reviewRun, reviewTimeout string
	var reviewEffort, reviewFallbackModel, reviewAppendPrompt, reviewAppendPromptFile string
	var reviewJSON bool
	reviewCmd := &cobra.Command{
		Use:   "review <workspace>",
		Short: "Dispatch a read-only independent reviewer over the workspace diff",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedAppendPrompt, err := resolveAppendPrompt(reviewAppendPrompt, reviewAppendPromptFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Load(args[0])
			if err != nil {
				return err
			}
			if m.State == "closed" {
				return fmt.Errorf("%s is closed", m.ID)
			}
			if active, err := activeJobIn(s, m.ID); err != nil {
				return err
			} else if active != "" {
				return fmt.Errorf("%s already has active job %s (one active job per workspace)", m.ID, active)
			}
			snapshot, err := wss.ReviewSnapshot(m)
			if err != nil {
				return err
			}
			jm, err := dispatchJob(dispatchOptions{
				Agent: reviewAgent, Task: workspaceReviewPrompt(m.ID, snapshot.Diff),
				Workspace: m.ID, RunLabel: reviewRun, Timeout: reviewTimeout,
				Model: reviewModel, Effort: reviewEffort,
				FallbackModel: reviewFallbackModel, AppendPrompt: resolvedAppendPrompt,
				ReadOnly: true, Review: &job.ReviewRequest{CheckpointRef: snapshot.CheckpointRef,
					CheckpointOID: snapshot.CheckpointOID, DiffSHA256: snapshot.DiffSHA256},
			})
			if err != nil {
				return err
			}
			if reviewJSON {
				return printJSON(jm)
			}
			fmt.Println(jm.ID)
			return nil
		},
	}
	reviewCmd.Flags().StringVar(&reviewAgent, "agent", "claude", "reviewer agent adapter (claude, codex, hermes, fake)")
	reviewCmd.Flags().StringVar(&reviewModel, "model", "", "reviewer model override (default: agent default)")
	reviewCmd.Flags().StringVar(&reviewEffort, "effort", "high", "reviewer reasoning effort (low|medium|high|xhigh|max); codex clamps xhigh/max to high")
	reviewCmd.Flags().StringVar(&reviewFallbackModel, "fallback-model", "", "claude only: reviewer model to retry with when overloaded")
	reviewCmd.Flags().StringVar(&reviewRun, "run", "", "group the reviewer job under a run label")
	reviewCmd.Flags().StringVar(&reviewTimeout, "timeout", "", "wall-clock limit for the review turn (e.g. 30m); exceeded -> interrupted")
	reviewCmd.Flags().StringVar(&reviewAppendPrompt, "append-prompt", "", "orchestrator additions to the injected worker rules")
	reviewCmd.Flags().StringVar(&reviewAppendPromptFile, "append-prompt-file", "", "read orchestrator additions from a UTF-8 text file, or - for stdin")
	reviewCmd.Flags().BoolVar(&reviewJSON, "json", false, "JSON output")

	ws.AddCommand(newCmd, lsCmd, commitCmd, reviewCmd, wsStatusCmd())
	return ws
}

// wsStatusAction is one deterministic next step: what to do, why, and a
// copyable command when one is safe to suggest.
type wsStatusAction struct {
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Command string `json:"command,omitempty"`
}

type wsStatusJob struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type wsStatusOut struct {
	Workspace    string                   `json:"workspace"`
	Repo         string                   `json:"repo"`
	Branch       string                   `json:"branch"`
	BaseOID      string                   `json:"base_oid"`
	State        string                   `json:"state"`
	Disposition  string                   `json:"disposition,omitempty"`
	MergedInto   string                   `json:"merged_into,omitempty"`
	Checkpoints  int                      `json:"checkpoints"`
	Jobs         []wsStatusJob            `json:"jobs"`
	DiffStat     string                   `json:"diff_stat,omitempty"`
	FinalCommit  *workspace.CommitInfo    `json:"final_commit,omitempty"`
	CloseReceipt *workspace.CloseReceipt  `json:"close_receipt,omitempty"`
	Review       *workspace.ReviewReceipt `json:"latest_review,omitempty"`
	Verification *job.VerificationReceipt `json:"latest_verification,omitempty"`
	Attention    []string                 `json:"attention"`
	NextActions  []wsStatusAction         `json:"next_actions"`
}

// wsStatusCmd is the one-command workspace rollup (see
// planning/tasks/actionable-workspace-status.md): what happened, what needs
// attention, and the next safe action — without joining job status, events,
// diffs, review prose, and receipts by hand. Strictly read-only; it reads
// persisted receipts, never infers a verdict from prose, and never advances
// state. For a closed workspace the close receipt shown here IS the landing
// proof — the answer to "confirm the change landed".
func wsStatusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status <workspace>",
		Short: "Workspace rollup: facts, receipts, attention, next safe action",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Load(args[0])
			if err != nil {
				return err
			}
			out, err := buildWSStatus(s, wss, m)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(out)
			}
			printWSStatus(out)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func buildWSStatus(s *job.Store, wss *workspace.Store, m *workspace.Meta) (*wsStatusOut, error) {
	out := &wsStatusOut{
		Workspace: m.ID, Repo: m.Repo, Branch: m.Branch, BaseOID: m.BaseOID,
		State: m.State, Disposition: m.Disposition, MergedInto: m.MergedInto,
		Checkpoints: m.Checkpoints, FinalCommit: m.FinalCommit, CloseReceipt: m.CloseReceipt,
		Review: m.LatestReview, Verification: workspaceCurrentVerification(s, m),
		Attention: []string{}, NextActions: []wsStatusAction{},
	}
	metas, err := s.List()
	if err != nil {
		return nil, err
	}
	var attached []*job.Meta
	for _, jm := range metas {
		if jm.Workspace != m.ID {
			continue
		}
		s.Reconcile(jm)
		attached = append(attached, jm)
		out.Jobs = append(out.Jobs, wsStatusJob{ID: jm.ID, State: string(jm.State)})
	}
	if m.State == "open" {
		if stat, err := wss.Diff(m, true); err != nil {
			out.DiffStat = "unavailable: " + err.Error()
		} else if stat = strings.TrimSpace(stat); stat == "" {
			out.DiffStat = "empty"
		} else {
			lines := strings.Split(stat, "\n")
			out.DiffStat = strings.TrimSpace(lines[len(lines)-1])
		}
	}
	wsStatusAdvice(out, m, attached)
	if len(out.Attention) == 0 {
		out.Attention = append(out.Attention, "none")
	}
	return out, nil
}

// wsStatusAdvice derives the deterministic attention/next_actions codes.
// Review, verification, merge, and close remain separate gates — the advice
// names the next one, it never combines or skips them.
func wsStatusAdvice(out *wsStatusOut, m *workspace.Meta, attached []*job.Meta) {
	if m.State == "closed" {
		reason := "workspace is closed; the close receipt above is the durable record"
		if m.Disposition == "merged" {
			reason = "landed: the close receipt (target, final commit) is the proof — no git check needed"
		}
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "none", Reason: reason})
		return
	}
	for _, jm := range attached {
		switch {
		case jm.State == job.StateActive || jm.State == job.StateQueued:
			out.Attention = append(out.Attention, "job-running")
			out.NextActions = append(out.NextActions, wsStatusAction{Action: "wait",
				Reason:  jm.ID + " is " + string(jm.State),
				Command: "legwork wait " + jm.ID})
		case jm.State == job.StateNeedsInput:
			out.Attention = append(out.Attention, "needs-input")
			out.NextActions = append(out.NextActions, wsStatusAction{Action: "answer",
				Reason:  jm.ID + " asked: " + events.Truncate(jm.Question),
				Command: "legwork answer " + jm.ID + " \"<decision>\""})
		case jm.State == job.StateBlocked && jm.Blocked != nil && jm.Blocked.Kind == "verify":
			out.Attention = append(out.Attention, "blocked-verify")
			out.NextActions = append(out.NextActions, wsStatusAction{Action: "verify",
				Reason:  jm.ID + " requested host verification",
				Command: "legwork verify " + jm.ID + " -- <argv...>"})
		case jm.State == job.StateBlocked && jm.Blocked != nil && jm.Blocked.Kind == "provision":
			out.Attention = append(out.Attention, "needs-provision")
			out.NextActions = append(out.NextActions, wsStatusAction{Action: "approve",
				Reason:  jm.ID + " wants: " + events.Truncate(jm.Blocked.Command),
				Command: "legwork approve " + jm.ID})
		case jm.State == job.StateFailed || jm.State == job.StateInterrupted || jm.State == job.StateAuthNeeded:
			out.Attention = append(out.Attention, string(jm.State))
			out.NextActions = append(out.NextActions, wsStatusAction{Action: "inspect",
				Reason:  jm.ID + " is " + string(jm.State),
				Command: "legwork events " + jm.ID})
		}
	}
	if len(out.NextActions) > 0 {
		return // a job needs handling before any landing step is safe
	}
	if r := out.Review; r != nil && r.Parsed && r.Verdict == "FIX" {
		out.Attention = append(out.Attention, "review-fix")
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "fix-findings",
			Reason:  fmt.Sprintf("review %s returned FIX (%d findings); relay them to the implementer", r.Job, r.Findings.Total),
			Command: "legwork resume <implementer-job> \"<findings>\""})
		return
	}
	if v := out.Verification; v != nil && !v.Passed {
		out.Attention = append(out.Attention, "verification-failed")
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "verify",
			Reason:  "latest verification did not pass",
			Command: shellCommand(verifyRetry(v.Job, v.Argv))})
		return
	}
	switch {
	case out.DiffStat == "empty" && len(attached) == 0:
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "dispatch",
			Reason:  "no changes and no jobs yet",
			Command: "legwork run --workspace " + m.ID + " \"<task>\""})
	case out.DiffStat != "empty" && (out.Review == nil || !out.Review.Parsed):
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "review",
			Reason:  "unreviewed changes; close will refuse without a disposition",
			Command: "legwork ws review " + m.ID})
	case out.FinalCommit == nil && out.DiffStat != "empty":
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "commit",
			Reason:  "review verdict SHIP; commit as the orchestrator",
			Command: "legwork ws commit " + m.ID + " -m \"<message>\""})
	default:
		out.NextActions = append(out.NextActions, wsStatusAction{Action: "close",
			Reason:  "work is committed; land and close",
			Command: "legwork close " + m.ID + " --merge-into main"})
	}
}

func printWSStatus(out *wsStatusOut) {
	state := out.State
	if out.Disposition != "" {
		state += "/" + out.Disposition
	}
	fmt.Printf("%s  %s  repo: %s\n", out.Workspace, state, out.Repo)
	fmt.Printf("branch: %s  base: %s  checkpoints: %d\n", out.Branch, shortOID(out.BaseOID), out.Checkpoints)
	if len(out.Jobs) > 0 {
		parts := make([]string, len(out.Jobs))
		for i, j := range out.Jobs {
			parts[i] = j.ID + " " + j.State
		}
		fmt.Printf("jobs: %s\n", strings.Join(parts, ", "))
	}
	if out.DiffStat != "" {
		fmt.Printf("diff: %s\n", out.DiffStat)
	}
	if r := out.Review; r != nil {
		if r.Parsed {
			fmt.Printf("review: %s (%s, %d findings)\n", r.Verdict, r.Job, r.Findings.Total)
		} else {
			fmt.Printf("review: unparsed (%s): %s\n", r.Job, r.ParseError)
		}
	}
	if v := out.Verification; v != nil {
		state := "failed"
		if v.Passed {
			state = "passed"
		} else if v.TimedOut {
			state = "timed out"
		}
		fmt.Printf("verification: %s  receipt: %s\n", state, v.ReceiptID)
	}
	if fc := out.FinalCommit; fc != nil {
		fmt.Printf("final commit: %s %q\n", shortOID(fc.OID), fc.Message)
	}
	if cr := out.CloseReceipt; cr != nil {
		line := "close receipt: " + cr.ReceiptID
		if cr.Target != "" {
			line += "  merged into: " + cr.Target
		}
		if cr.ClosedAt != nil {
			line += "  at " + cr.ClosedAt.Format(time.RFC3339)
		}
		fmt.Println(line)
	}
	fmt.Printf("attention: %s\n", strings.Join(out.Attention, ", "))
	for _, a := range out.NextActions {
		line := "next: " + a.Action + " — " + a.Reason
		if a.Command != "" {
			line += "\n      " + a.Command
		}
		fmt.Println(line)
	}
}

func workspaceCurrentVerification(s *job.Store, wm *workspace.Meta) *job.VerificationReceipt {
	r := wm.LatestVerification
	if r == nil || r.Workspace != wm.ID {
		return nil
	}
	jm, err := s.LoadMeta(r.Job)
	if err != nil || jm.CurrentVerification() == nil || jm.CurrentVerification().ReceiptID != r.ReceiptID {
		return nil
	}
	return r
}

func workspaceReviewPrompt(wsID, diff string) string {
	if strings.TrimSpace(diff) == "" {
		diff = "(empty diff)"
	}
	return fmt.Sprintf(`You are an independent reviewer for workspace %s.

Review only the workspace diff below. Do not review unrelated tree state unless a finding is directly evidenced by the diff. Look for correctness, security, data loss, concurrency, compatibility, and missing tests. Do not modify files.

Return a structured verdict before the required legwork status block, using exactly this JSON shape:
{"verdict":"SHIP|FIX","findings":[{"file":"path","line":123,"severity":"critical|high|medium|low","detail":"..."}]}

Use "SHIP" only when there are no findings that should block landing. Use "FIX" when the orchestrator should send the work back for changes. For line, use the nearest changed line; use 0 only when no line applies. Keep findings concise and actionable.

Workspace diff from legwork diff %s:
`+"```diff"+`
%s
`+"```"+`
`, wsID, wsID, diff)
}

type workspaceCommitOutput struct {
	Workspace   string                `json:"workspace"`
	Branch      string                `json:"branch"`
	OID         string                `json:"oid"`
	Summary     string                `json:"summary,omitempty"`
	FinalCommit *workspace.CommitInfo `json:"final_commit,omitempty"`
}

func appendWorkspaceCommitEvents(s *job.Store, m *workspace.Meta, message string, res *workspace.CommitResult) error {
	metas, err := s.List()
	if err != nil {
		return err
	}
	var historyErrs []error
	fields := map[string]any{
		"workspace": m.ID,
		"branch":    m.Branch,
		"oid":       res.OID,
	}
	if res.Receipt != nil {
		fields["receipt_id"] = res.Receipt.ReceiptID
		fields["final_commit"] = compactCommitEventReceipt(res.Receipt)
	}
	if res.Summary != "" {
		fields["summary"] = events.Truncate(res.Summary)
	}
	ev := events.Event{
		Type:    events.TypeCommit,
		Actor:   "orchestrator",
		Preview: events.Truncate(message),
		Fields:  fields,
	}
	runs := map[string]bool{}
	for _, jm := range metas {
		if jm.Workspace != m.ID {
			continue
		}
		if jm.Run != "" {
			runs[jm.Run] = true
		}
		log, err := events.Open(filepath.Join(s.JobDir(jm.ID), "events.jsonl"))
		if err != nil {
			historyErrs = append(historyErrs, fmt.Errorf("open job %s event %s: %w", jm.ID, filepath.Join(s.JobDir(jm.ID), "events.jsonl"), err))
			continue
		}
		if _, err := log.Append(ev); err != nil {
			historyErrs = append(historyErrs, fmt.Errorf("append job %s event %s: %w", jm.ID, filepath.Join(s.JobDir(jm.ID), "events.jsonl"), err))
		}
	}
	for run := range runs {
		path, err := s.RunEventsPath(run)
		if err != nil {
			historyErrs = append(historyErrs, fmt.Errorf("open run %s event path: %w", run, err))
			continue
		}
		rl, err := events.Open(path)
		if err != nil {
			historyErrs = append(historyErrs, fmt.Errorf("open run %s event %s: %w", run, path, err))
			continue
		}
		runFields := map[string]any{
			"workspace": m.ID,
			"branch":    m.Branch,
			"oid":       res.OID,
		}
		if res.Receipt != nil {
			runFields["receipt_id"] = res.Receipt.ReceiptID
			runFields["final_commit"] = compactCommitEventReceipt(res.Receipt)
		}
		if res.Summary != "" {
			runFields["summary"] = events.Truncate(res.Summary)
		}
		if _, err := rl.Append(events.Event{
			Type:    events.TypeCommit,
			Actor:   "orchestrator",
			Preview: events.Truncate(message),
			Fields:  runFields,
		}); err != nil {
			historyErrs = append(historyErrs, fmt.Errorf("append run %s event %s: %w", run, path, err))
		}
	}
	return errors.Join(historyErrs...)
}

func compactCommitEventReceipt(info *workspace.CommitInfo) *workspace.CommitInfo {
	if info == nil {
		return nil
	}
	compact := *info
	compact.Summary = events.Truncate(compact.Summary)
	compact.Message = events.Truncate(compact.Message)
	compact.HistoryError = events.Truncate(compact.HistoryError)
	return &compact
}

type diffOutput struct {
	Workspace string `json:"workspace"`
	Branch    string `json:"branch"`
	Stat      bool   `json:"stat"`
	Diff      string `json:"diff"`
}

func diffCmd() *cobra.Command {
	var stat, asJSON bool
	c := &cobra.Command{
		Use:   "diff <workspace>",
		Short: "Changes vs the workspace base (includes untracked files)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Load(args[0])
			if err != nil {
				return err
			}
			if m.State == "closed" {
				return fmt.Errorf("%s is closed", m.ID)
			}
			out, err := wss.Diff(m, stat)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(diffOutput{Workspace: m.ID, Branch: m.Branch, Stat: stat, Diff: out})
			}
			fmt.Print(out)
			return nil
		},
	}
	c.Flags().BoolVar(&stat, "stat", false, "diffstat only")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func closeCmd() *cobra.Command {
	var merged, discard, keepWorktree, preserve, force bool
	var mergedInto, mergeTarget, mergeMessage, reason, supersededBy, retention string
	var asJSON bool
	c := &cobra.Command{
		Use:   "close <workspace>",
		Short: "Acknowledge a workspace and reclaim its local worktree cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, err := wss.Load(args[0])
			if err != nil {
				return err
			}
			if id, err := activeJobIn(s, m.ID); err != nil {
				return err
			} else if id != "" {
				return fmt.Errorf("%s has active job %s; cancel it or wait", m.ID, id)
			}
			disposition := ""
			switch {
			case merged && discard:
				return fmt.Errorf("--merged and --discard are mutually exclusive")
			case mergeTarget != "" && discard:
				return fmt.Errorf("--merge-into and --discard are mutually exclusive")
			case mergeTarget != "" && merged:
				return fmt.Errorf("--merge-into already implies --merged")
			case mergeTarget != "" && mergedInto != "":
				return fmt.Errorf("--merge-into cannot be combined with --into")
			case mergeTarget != "" && force:
				return fmt.Errorf("--merge-into cannot be combined with --force")
			case mergeMessage != "" && mergeTarget == "":
				return fmt.Errorf("-m/--message is only valid with --merge-into")
			case mergeTarget != "":
				disposition = "merged"
			case merged:
				disposition = "merged"
			case discard:
				disposition = "discard"
			}
			if preserve {
				if retention != "" && retention != "preserve" {
					return fmt.Errorf("--preserve requires --retention preserve, got %q", retention)
				}
				if retention == "" {
					retention = "preserve"
				}
			}
			verifiedTarget := ""
			var merge *workspace.MergeResult
			if mergeTarget != "" {
				res, err := wss.MergeInto(m, mergeTarget, mergeMessage)
				if err != nil {
					var mergeErr *workspace.MergeError
					if errors.As(err, &mergeErr) {
						exit := 3
						if mergeErr.Kind == workspace.MergeErrorConflict {
							exit = 1
						}
						closeFail(asJSON, m.ID, string(mergeErr.Kind), mergeErr.Error(), exit)
						return nil
					}
					return err
				}
				merge = res
				verifiedTarget = res.Target
			}
			// --merged is a claim; verify it before closing the review gate.
			// (A merge mistakenly run inside the worktree is a no-op — closing
			// on top of that leaves the work dangling.)
			if merged && !force {
				target := mergedInto
				if target == "" {
					if target, _ = wss.DefaultBranchTip(m.Repo); target == "" {
						return fmt.Errorf("no default branch resolved; pass --into <ref> (or --force to skip verification)")
					}
				}
				ok, err := wss.MergedInto(m, target)
				if err != nil {
					return err
				}
				if !ok {
					// The auto-detected target prefers origin/HEAD. The common
					// near-miss is work merged into the local default branch
					// but not pushed yet — name that case instead of a generic
					// refusal.
					if mergedInto == "" {
						for _, local := range []string{"refs/heads/main", "refs/heads/master"} {
							if landed, lerr := wss.MergedInto(m, local); lerr == nil && landed {
								return fmt.Errorf("%s: branch %s has landed in %s but not in %s — push first, or close with --into %s", m.ID, m.Branch, local, target, local)
							}
						}
					}
					return fmt.Errorf("%s: branch %s is NOT an ancestor of %s — the work has not landed there; merge it first, or use --into <ref> / --discard / --force", m.ID, m.Branch, target)
				}
				verifiedTarget = target
			}
			if merged && force && mergedInto != "" {
				verifiedTarget = mergedInto
			}
			if err := wss.Close(m, workspace.CloseOptions{
				Disposition:  disposition,
				KeepWorktree: keepWorktree,
				Reason:       reason,
				SupersededBy: supersededBy,
				MergedInto:   verifiedTarget,
				Retention:    retention,
				Actor:        "orchestrator",
			}); err != nil {
				return err
			}
			// Close the workspace's jobs too: the lineage is acknowledged, and
			// each job's Closed timestamp anchors gc's retention clock.
			_ = s.CloseJobsForWorkspace(m.ID)
			if asJSON {
				return printJSON(closeOutput{
					OK:           true,
					Workspace:    m.ID,
					State:        m.State,
					Disposition:  m.Disposition,
					MergedInto:   m.MergedInto,
					Merge:        merge,
					CloseReceipt: m.CloseReceipt,
					FinalCommit:  m.FinalCommit,
				})
			}
			if m.CloseReceipt != nil && m.CloseReceipt.HistoryError != "" {
				fmt.Printf("%s closed (%s; history warning: %s)\n", m.ID, m.Disposition, m.CloseReceipt.HistoryError)
			} else {
				fmt.Printf("%s closed (%s)\n", m.ID, m.Disposition)
			}
			// The receipt is the landing proof — print it so confirming the
			// close never requires reaching for git (F1 in eval/FINDINGS.md).
			if merge != nil {
				fmt.Printf("  landed: %s @ %s (merge commit)\n", merge.TargetBranch, shortOID(merge.Commit))
			} else if m.MergedInto != "" {
				fmt.Printf("  landed: verified ancestor of %s\n", m.MergedInto)
			}
			if m.CloseReceipt != nil {
				fmt.Printf("  receipt: %s\n", m.CloseReceipt.ReceiptID)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&merged, "merged", false, "changes landed elsewhere (verified via merge-base against --into or the default branch)")
	c.Flags().StringVar(&mergeTarget, "merge-into", "", "merge the workspace branch into this local branch, then close as merged")
	c.Flags().StringVarP(&mergeMessage, "message", "m", "", "merge commit message for --merge-into (default: generated)")
	c.Flags().BoolVar(&discard, "discard", false, "throw the changes away")
	c.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "acknowledge but keep the worktree on disk")
	c.Flags().BoolVar(&preserve, "preserve", false, "record retention=preserve and keep branch/checkpoint refs")
	c.Flags().StringVar(&mergedInto, "into", "", "target ref --merged is verified against (default: detected default branch)")
	c.Flags().StringVar(&reason, "reason", "", "human-readable close/archive reason recorded in workspace metadata")
	c.Flags().StringVar(&supersededBy, "superseded-by", "", "workspace/run/branch that supersedes this workspace")
	c.Flags().StringVar(&retention, "retention", "", "retention policy recorded in workspace metadata (for example preserve, compress, prune-after:<duration>, delete)")
	c.Flags().BoolVar(&force, "force", false, "skip --merged verification")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func shortOID(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	return oid
}

type closeOutput struct {
	OK           bool                    `json:"ok"`
	Workspace    string                  `json:"workspace"`
	State        string                  `json:"state"`
	Disposition  string                  `json:"disposition,omitempty"`
	MergedInto   string                  `json:"merged_into,omitempty"`
	Merge        *workspace.MergeResult  `json:"merge,omitempty"`
	CloseReceipt *workspace.CloseReceipt `json:"close_receipt,omitempty"`
	FinalCommit  *workspace.CommitInfo   `json:"final_commit,omitempty"`
	Blocked      *closeBlocked           `json:"blocked,omitempty"`
}

type closeBlocked struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

func closeFail(asJSON bool, workspaceID, kind, detail string, code int) {
	if asJSON {
		_ = printJSON(closeOutput{
			OK:        false,
			Workspace: workspaceID,
			State:     "blocked",
			Blocked:   &closeBlocked{Kind: kind, Detail: detail},
		})
	} else {
		fmt.Fprintf(os.Stderr, "legwork: %s\n", detail)
	}
	os.Exit(code)
}
