package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/config"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/verify"
)

func lsCmd() *cobra.Command {
	var asJSON bool
	var includeAll bool
	var workspaceFilter string
	var runFilter string
	var stateFilter string
	var limit int
	c := &cobra.Command{
		Use:   "ls",
		Short: "List jobs needing attention, active work, and unreviewed results",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			metas, err := s.List()
			if err != nil {
				return err
			}
			stateSet, err := parseLSStateFilter(stateFilter)
			if err != nil {
				return err
			}
			if limit < 0 {
				return fmt.Errorf("--limit must be non-negative")
			}
			for _, m := range metas {
				s.Reconcile(m)
			}
			metas = filterLSMetas(metas, lsFilters{
				includeAll: includeAll,
				workspace:  workspaceFilter,
				run:        runFilter,
				states:     stateSet,
			})
			sortLSMetas(metas)
			if limit > 0 && limit < len(metas) {
				metas = metas[:limit]
			}
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			if asJSON {
				out := make([]metaOut, len(metas))
				for i, m := range metas {
					out[i] = metaOut{Meta: m, ContextHigh: m.ContextHigh(health.ContextThreshold)}
				}
				return printJSON(out)
			}
			if len(metas) == 0 {
				if workspaceFilter != "" || runFilter != "" || len(stateSet) > 0 {
					fmt.Println("no jobs match filters")
				} else if includeAll {
					fmt.Println("no jobs")
				} else {
					fmt.Println("no non-closed jobs (use --all to include closed history)")
				}
				return nil
			}
			width := termWidth()
			for _, m := range metas {
				where := collapseWhitespace(m.Workspace)
				if where == "" {
					where = "-"
				}
				// ctx cell padded as one token so the "!" marker never shoves
				// later columns; field widened 7->9 to fit "ctx:180k!".
				ctx := "ctx:" + fmtContext(m.Context)
				if m.ContextHigh(health.ContextThreshold) {
					ctx += "!"
				}
				agent := collapseWhitespace(m.Agent)
				if m.Model != "" {
					agent += "/" + collapseWhitespace(m.Model)
				}
				prefix := fmt.Sprintf("%-8s %-18s %-14s %6s %-9s %-6s ",
					collapseWhitespace(m.ID), clip(agent, 18),
					clip(collapseWhitespace(lsState(m)), 14), fmtAge(m.Updated), ctx, where)
				if len([]rune(prefix)) >= width {
					fmt.Println(clip(prefix, width))
					continue
				}
				fmt.Println(prefix + clip(lsSummary(m), width-len([]rune(prefix))))
				if r := m.CurrentVerification(); r != nil && !r.Passed {
					fmt.Printf("  retry: %s\n", shellCommand(verify.RetryArgv(m.ID, r.Argv)))
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().BoolVar(&includeAll, "all", false, "include closed history")
	c.Flags().StringVar(&workspaceFilter, "workspace", "", "only jobs attached to this workspace")
	c.Flags().StringVar(&runFilter, "run", "", "only jobs in this run label")
	c.Flags().StringVar(&stateFilter, "state", "", "only comma-separated job states")
	c.Flags().IntVar(&limit, "limit", 0, "maximum jobs to show after filtering and sorting")
	return c
}

type lsFilters struct {
	includeAll bool
	workspace  string
	run        string
	states     map[job.State]bool
}

func parseLSStateFilter(raw string) (map[job.State]bool, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return parseJobStates(raw)
}

func parseJobStates(raw string) (map[job.State]bool, error) {
	valid := map[job.State]bool{
		job.StateQueued:      true,
		job.StateActive:      true,
		job.StateDone:        true,
		job.StateNeedsInput:  true,
		job.StateBlocked:     true,
		job.StateFailed:      true,
		job.StateAuthNeeded:  true,
		job.StateInterrupted: true,
		job.StateClosed:      true,
	}
	out := map[job.State]bool{}
	for _, part := range strings.Split(raw, ",") {
		state := job.State(strings.TrimSpace(part))
		if state == "" || !valid[state] {
			return nil, fmt.Errorf("invalid state %q", part)
		}
		out[state] = true
	}
	return out, nil
}

func filterLSMetas(metas []*job.Meta, f lsFilters) []*job.Meta {
	out := metas[:0]
	for _, m := range metas {
		if f.workspace != "" && m.Workspace != f.workspace {
			continue
		}
		if f.run != "" && m.Run != f.run {
			continue
		}
		if len(f.states) > 0 {
			if !f.states[m.State] {
				continue
			}
		} else if !f.includeAll && m.State == job.StateClosed {
			continue
		}
		out = append(out, m)
	}
	return out
}

func sortLSMetas(metas []*job.Meta) {
	sort.SliceStable(metas, func(i, j int) bool {
		a, b := metas[i], metas[j]
		if ar, br := lsAttentionRank(a), lsAttentionRank(b); ar != br {
			return ar < br
		}
		if !a.Updated.Equal(b.Updated) {
			return a.Updated.After(b.Updated)
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.After(b.Created)
		}
		return a.ID > b.ID
	})
}

func lsAttentionRank(m *job.Meta) int {
	if r := m.CurrentVerification(); r != nil {
		if r.Passed {
			return 2 // verification passed: reviewable work, not a host-check alert
		}
		return 0 // failed/timed-out host check remains explicit attention
	}
	switch m.State {
	case job.StateNeedsInput, job.StateAuthNeeded, job.StateBlocked, job.StateFailed, job.StateInterrupted:
		return 0
	case job.StateActive, job.StateQueued:
		return 1
	case job.StateClosed:
		return 3
	default:
		return 2
	}
}

func lsSummary(m *job.Meta) string {
	if r := m.CurrentVerification(); r != nil {
		if r.Passed {
			return "verification passed — reviewable"
		}
		if r.TimedOut {
			return "verification timed out — retry below"
		}
		return "verification failed — retry below"
	}
	task := collapseWhitespace(m.Task)
	if task == "" {
		task = "-"
	}
	if run := collapseWhitespace(m.Run); run != "" {
		return "[" + run + "] " + task
	}
	return task
}

func lsState(m *job.Meta) string { return string(m.State) }

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
