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
		if m.State == job.StateActive || m.State == job.StateQueued {
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
		Use:   "ls",
		Short: "List workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			metas, err := wss.List()
			if err != nil {
				return err
			}
			if lsJSON {
				return printJSON(metas)
			}
			for _, m := range metas {
				state := m.State
				if m.Disposition != "" {
					state += "/" + m.Disposition
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
	reviewCmd.Flags().StringVar(&reviewAgent, "agent", "claude", "reviewer agent adapter (claude, codex, fake)")
	reviewCmd.Flags().StringVar(&reviewModel, "model", "", "reviewer model override (default: agent default)")
	reviewCmd.Flags().StringVar(&reviewEffort, "effort", "high", "reviewer reasoning effort (low|medium|high|xhigh|max); codex clamps xhigh/max to high")
	reviewCmd.Flags().StringVar(&reviewFallbackModel, "fallback-model", "", "claude only: reviewer model to retry with when overloaded")
	reviewCmd.Flags().StringVar(&reviewRun, "run", "", "group the reviewer job under a run label")
	reviewCmd.Flags().StringVar(&reviewTimeout, "timeout", "", "wall-clock limit for the review turn (e.g. 30m); exceeded -> interrupted")
	reviewCmd.Flags().StringVar(&reviewAppendPrompt, "append-prompt", "", "orchestrator additions to the injected worker rules")
	reviewCmd.Flags().StringVar(&reviewAppendPromptFile, "append-prompt-file", "", "read orchestrator additions from a UTF-8 text file, or - for stdin")
	reviewCmd.Flags().BoolVar(&reviewJSON, "json", false, "JSON output")

	ws.AddCommand(newCmd, lsCmd, commitCmd, reviewCmd)
	return ws
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
		fields["final_commit"] = res.Receipt
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
			runFields["final_commit"] = res.Receipt
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

func diffCmd() *cobra.Command {
	var stat bool
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
			fmt.Print(out)
			return nil
		},
	}
	c.Flags().BoolVar(&stat, "stat", false, "diffstat only")
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
