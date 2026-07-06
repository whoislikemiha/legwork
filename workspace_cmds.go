package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

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

	ws.AddCommand(newCmd, lsCmd)
	return ws
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
	var merged, discard, keepWorktree, force bool
	var mergedInto string
	c := &cobra.Command{
		Use:   "close <workspace>",
		Short: "Acknowledge a workspace and reclaim worktree, branch, checkpoint refs",
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
			case merged:
				disposition = "merged"
			case discard:
				disposition = "discard"
			}
			// --merged is a claim; verify it before destroying the branch.
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
					return fmt.Errorf("%s: branch %s is NOT an ancestor of %s — the work has not landed there; merge it first, or use --into <ref> / --discard / --force", m.ID, m.Branch, target)
				}
			}
			if err := wss.Close(m, disposition, keepWorktree); err != nil {
				return err
			}
			// Close the workspace's jobs too: the lineage is acknowledged, and
			// each job's Closed timestamp anchors gc's retention clock.
			_ = s.CloseJobsForWorkspace(m.ID)
			fmt.Printf("%s closed (%s)\n", m.ID, m.Disposition)
			return nil
		},
	}
	c.Flags().BoolVar(&merged, "merged", false, "changes landed elsewhere (verified via merge-base against --into or the default branch)")
	c.Flags().BoolVar(&discard, "discard", false, "throw the changes away")
	c.Flags().BoolVar(&keepWorktree, "keep-worktree", false, "acknowledge but keep the worktree on disk")
	c.Flags().StringVar(&mergedInto, "into", "", "target ref --merged is verified against (default: detected default branch)")
	c.Flags().BoolVar(&force, "force", false, "skip --merged verification")
	return c
}
