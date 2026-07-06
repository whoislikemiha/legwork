package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/gc"
)

// gcCmd is opportunistic reclamation (DESIGN.md §8): retire transcripts, sweep
// orphans, reconcile dead runners — only on closed/orphaned things, never on
// unclosed work. Exit 0 = success (incl. nothing to do); 1 = partial
// reclamation failure (report still printed); 2 = usage error.
func gcCmd() *cobra.Command {
	var dryRun, asJSON, closeMerged, auto bool
	var closeMergedInto string
	c := &cobra.Command{
		Use:   "gc",
		Short: "Reclaim closed/orphaned artifacts: transcripts, orphan refs/worktrees, dead runners",
		Long: `Opportunistic reclamation. Compresses and retires transcripts of finished/
closed jobs, sweeps orphans left by crashes (meta-less dirs, stale worktree
registrations, refs/legwork/* with no workspace, runners that died -> interrupted),
and reports orphan legwork/* branches. Never touches unclosed work.

--close-merged (opt-in) additionally closes open workspaces whose branch has
landed in the default branch (or --close-merged-into <ref>); dirty or unmerged
workspaces are always left for human review.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if closeMergedInto != "" && !closeMerged {
				return fmt.Errorf("--close-merged-into requires --close-merged")
			}
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			cfg, err := gc.Load()
			if err != nil {
				return err
			}

			if auto {
				// Honor the interval gate and the single-runner lock; a
				// non-due or already-running auto-gc is a silent no-op.
				if !cfg.Auto || !gc.AutoDue(s.Root, cfg.AutoInterval) {
					return nil
				}
				release, ok := gc.TryLockAuto(s.Root)
				if !ok {
					return nil
				}
				defer release()
			}

			rep, err := gc.Run(s, wss, cfg, gc.Options{
				DryRun: dryRun, Auto: auto,
				CloseMerged: closeMerged, CloseMergedInto: closeMergedInto,
			})
			if err != nil {
				return err
			}
			if asJSON {
				_ = printJSON(rep)
			} else if !auto {
				printGCSummary(rep, dryRun)
			}
			if rep.Failed > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be reclaimed; mutate nothing")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().BoolVar(&closeMerged, "close-merged", false, "also close open workspaces whose branch has landed (opt-in)")
	c.Flags().StringVar(&closeMergedInto, "close-merged-into", "", "target ref for --close-merged (default: detected default branch)")
	c.Flags().BoolVar(&auto, "auto", false, "opportunistic run: honor the interval gate and single-runner lock")
	return c
}

// printGCSummary renders a one-line-per-category human summary.
func printGCSummary(rep gc.Report, dryRun bool) {
	verb := "reclaimed"
	if dryRun {
		verb = "would reclaim"
	}
	counts := map[string]int{}
	for _, a := range rep.Actions {
		counts[a.Kind]++
	}
	if len(rep.Actions) == 0 {
		fmt.Println("gc: nothing to do")
		return
	}
	fmt.Printf("gc: %s (%s):\n", verb, fmtBytes(rep.Bytes))
	// Stable, human-meaningful ordering.
	order := []struct{ kind, label string }{
		{gc.KindReconcile, "dead runner(s) -> interrupted"},
		{gc.KindTranscriptCompress, "transcript(s) compressed"},
		{gc.KindTranscriptDelete, "transcript(s) retired"},
		{gc.KindHalfCreated, "half-created dir(s) removed"},
		{gc.KindWorktreePrune, "repo(s) worktree-pruned"},
		{gc.KindOrphanRef, "orphan ref(s) deleted"},
		{gc.KindOrphanTree, "orphan tree(s) (report-only)"},
		{gc.KindOrphanBranch, "orphan branch(es) (report-only)"},
		{gc.KindCloseClean, "workspace(s) closed clean"},
		{gc.KindCloseMerged, "workspace(s) closed merged"},
		{gc.KindSkip, "workspace(s) skipped (human review)"},
	}
	for _, o := range order {
		if n := counts[o.kind]; n > 0 {
			fmt.Printf("  %3d %s\n", n, o.label)
		}
	}
	if rep.Failed > 0 {
		fmt.Printf("  %3d action(s) FAILED (see notes)\n", rep.Failed)
	}
}

func fmtBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGT"[exp])
}
