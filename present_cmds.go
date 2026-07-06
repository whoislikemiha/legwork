package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	xterm "github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/config"
	"github.com/whoislikemiha/legwork/internal/dashboard"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/timeline"
)

// --- runs: the pipeline-level overview ---

func runsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "runs",
		Short: "Pipeline overview: one line per run label, rolled up",
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
			for _, m := range metas {
				s.Reconcile(m)
			}
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			runLogs, err := timeline.RunLogs(s)
			if err != nil {
				return err
			}
			rollups := timeline.Rollups(metas, runLogs, health.ContextThreshold)
			if asJSON {
				// Always an array (never null) so consumers can iterate.
				if rollups == nil {
					rollups = []timeline.RunRollup{}
				}
				return printJSON(rollups)
			}
			if len(rollups) == 0 {
				return nil // empty state dir prints nothing
			}
			// Column widths; NOTE flexes to the remaining terminal width so long
			// narration is truncated to fit rather than wrapping.
			const (
				wRun   = 20
				wJobs  = 4
				wState = 22
				wCost  = 7
				wCtx   = 3
				wLast  = 5
			)
			used := wRun + 1 + wJobs + 1 + wState + 1 + wCost + 1 + wCtx + 1 + wLast + 1
			noteWidth := termWidth() - used
			if noteWidth < 12 {
				noteWidth = 12
			}
			fmt.Printf("%-*s %-*s %-*s %-*s %-*s %-*s %s\n",
				wRun, "RUN", wJobs, "JOBS", wState, "STATE", wCost, "COST",
				wCtx, "CTX", wLast, "LAST", "NOTE")
			for _, r := range rollups {
				label := r.Label
				if label == "" {
					label = "(no run)"
				}
				total := 0
				for _, n := range r.Jobs {
					total += n
				}
				ctx := "ok"
				if health.ContextThreshold <= 0 {
					ctx = "" // health signal disabled
				} else if r.ContextHigh {
					ctx = "!"
				}
				note := "—"
				if r.Label == "" {
					note = "" // the no-run bucket has no narration channel
				} else if r.LastNote != "" {
					note = clip(firstLine(r.LastNote), noteWidth)
				}
				fmt.Printf("%-*s %-*d %-*s %-*s %-*s %-*s %s\n",
					wRun, clip(label, wRun), wJobs, total,
					wState, clip(timeline.StateSummary(r.Jobs), wState),
					wCost, fmt.Sprintf("$%.2f", r.CostUSD),
					wCtx, ctx, wLast, fmtAge(r.Updated), note)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

// --- dashboard: the interactive TUI ---

func dashboardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dashboard",
		Short: "Interactive read-only TUI: runs, selected-job detail, live timeline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// ssh-first: the read-only surfaces work headless, but the TUI needs
			// a terminal. Fail cleanly toward the plain alternative.
			if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
				fmt.Fprintln(os.Stderr, "dashboard needs a TTY; try `legwork tail`")
				os.Exit(2)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			m := dashboard.New(s, health.ContextThreshold)
			_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
			return err
		},
	}
	return c
}

// --- tail: tail -f for the substrate ---

func tailCmd() *cobra.Command {
	var runLabel, jobID string
	var n int
	var full, untilIdle, asJSON bool
	c := &cobra.Command{
		Use:   "tail",
		Short: "Follow the merged event stream (all jobs + run logs); scriptable with --until-idle",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if runLabel != "" && jobID != "" {
				return fmt.Errorf("--run and --job are mutually exclusive")
			}
			var scope func() ([]timeline.Source, error)
			switch {
			case jobID != "":
				if _, err := s.LoadMeta(jobID); err != nil {
					return err
				}
				scope = timeline.ScopeJob(s, jobID)
			case runLabel != "":
				scope = timeline.ScopeRun(s, runLabel)
			default:
				scope = timeline.ScopeAll(s)
			}
			tl := timeline.New(scope)

			out := json.NewEncoder(os.Stdout)
			emit := func(items []timeline.Item) {
				for _, it := range items {
					if !full && !timeline.IsCurated(it.Event.Type) {
						continue
					}
					if asJSON {
						_ = out.Encode(it)
						continue
					}
					fmt.Println(tailLine(s, it))
				}
			}

			// Backfill: the last -n merged events, then follow live.
			first, err := tl.Poll()
			if err != nil {
				return err
			}
			if !full {
				first = timeline.Curated(first)
			}
			if n >= 0 && len(first) > n {
				first = first[len(first)-n:]
			}
			// Re-apply the firehose filter inside emit is redundant for the
			// backfill (already filtered) but harmless; print directly.
			for _, it := range first {
				if asJSON {
					_ = out.Encode(it)
				} else {
					fmt.Println(tailLine(s, it))
				}
			}

			for {
				if untilIdle && !scopeActive(s, runLabel, jobID) {
					// Drain any stragglers that landed with the terminal state,
					// then exit cleanly — the scriptable "pipeline is done".
					if items, err := tl.Poll(); err == nil {
						emit(items)
					}
					return nil
				}
				time.Sleep(500 * time.Millisecond)
				items, err := tl.Poll()
				if err != nil {
					return err
				}
				emit(items)
			}
		},
	}
	c.Flags().StringVar(&runLabel, "run", "", "scope to one run label (its log + its jobs)")
	c.Flags().StringVar(&jobID, "job", "", "scope to one job")
	c.Flags().IntVarP(&n, "lines", "n", 30, "backfill the last N events before following")
	c.Flags().BoolVar(&full, "full", false, "include the firehose (tool calls, progress, usage)")
	c.Flags().BoolVar(&untilIdle, "until-idle", false, "exit 0 once no job in scope is active/queued")
	c.Flags().BoolVar(&asJSON, "json", false, "emit merged events as JSONL (provenance + raw event)")
	return c
}

// scopeActive reports whether any job in the tail's scope is still active or
// queued (reconciling stale runners first). This is the --until-idle gate.
func scopeActive(s *job.Store, runLabel, jobID string) bool {
	metas, err := s.List()
	if err != nil {
		return false
	}
	for _, m := range metas {
		if jobID != "" && m.ID != jobID {
			continue
		}
		if runLabel != "" && m.Run != runLabel {
			continue
		}
		s.Reconcile(m)
		if m.State == job.StateActive || m.State == job.StateQueued {
			return true
		}
	}
	return false
}

// tailLine renders one merged item as "HH:MM:SS  <badge>  <type>  <preview>".
// A finished job line replaces the (long) result preview with the turn's
// telemetry summary read from meta.
func tailLine(s *job.Store, it timeline.Item) string {
	preview := it.Event.Preview
	if it.Event.Type == events.TypeFinished && it.JobID != "" {
		if m, err := s.LoadMeta(it.JobID); err == nil {
			preview = fmt.Sprintf("%s · $%.2f · ctx:%s", m.State, m.CostUSD, fmtContext(m.Context))
		}
	}
	return fmt.Sprintf("%s  %-12s %-9s %s",
		it.Event.Time.Local().Format("15:04:05"), it.Badge(), typeLabel(it.Event.Type), oneLine(preview))
}

// oneLine collapses whitespace/newlines so each event stays one visual line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// typeLabel keeps the type column short and aligned (max 9 runes).
func typeLabel(t string) string {
	switch t {
	case events.TypeCheckpoint:
		return "ckpt"
	case events.TypeNeedsInput:
		return "needs-inp"
	case events.TypeInterrupted:
		return "interrupt"
	default:
		return t
	}
}

// termWidth is the stdout terminal width, or 100 when stdout isn't a terminal
// (ssh pipe, redirect) — a generous default so notes still truncate sanely.
func termWidth() int {
	if w, _, err := xterm.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}
	return 100
}

// clip truncates s to w columns (rune-safe), appending "…" when cut.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// fmtAge renders a timestamp as a compact age (2m, 19h, 3d). Zero time -> "-".
func fmtAge(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	d := time.Since(ts)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
