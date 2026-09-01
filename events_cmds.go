package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

// noteCmd is orchestrator narration: cross-job reasoning goes into the run's
// event log so "what has the orchestrator decided" is a query, not a question.
func noteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "note <run> <text>",
		Short: "Append orchestrator narration to a run's event log",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			path, err := s.RunEventsPath(args[0])
			if err != nil {
				return err
			}
			rl, err := events.Open(path)
			if err != nil {
				return err
			}
			_, err = rl.Append(events.Event{Type: events.TypeNote, Actor: "orchestrator",
				Preview: events.Truncate(args[1])})
			return err
		},
	}
}

func eventsCmd() *cobra.Command {
	var since int
	var asJSON, isRun, isWorkspace bool
	var jobID string
	c := &cobra.Command{
		Use:   "events [selector] [--job <id> | --run | --workspace]",
		Short: "Read a job, run, or workspace event index (cursor with --since)",
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
			if isWorkspace {
				if jobID != "" || isRun {
					return fmt.Errorf("--workspace cannot be combined with --job or --run")
				}
				if positional == "" {
					return fmt.Errorf("workspace selector is required")
				}
				_, wss, err := openWorkspaces()
				if err != nil {
					return err
				}
				if _, err := wss.Load(positional); err != nil {
					return err
				}
				// Workspace history is intentionally separate from job/run
				// selector logic: --workspace keeps every pre-existing
				// selector meaning unchanged.
				return printEventLog(wss.EventsPath(positional), since, asJSON)
			}
			// events <label> --run is the existing boolean namespace override.
			// Keep it directly rather than encoding it as a string sentinel.
			if isRun {
				if jobID != "" {
					return fmt.Errorf("--job and --run are mutually exclusive")
				}
				if positional == "" {
					return fmt.Errorf("selector is required")
				}
				sel, err := job.Resolve(s, positional, job.SelectorRun)
				if err != nil {
					return err
				}
				return printEvents(s, sel, since, asJSON)
			}
			sel, err := resolveReadSelector(s, positional, jobID, "")
			if err != nil {
				return err
			}
			return printEvents(s, sel, since, asJSON)
		},
	}
	c.Flags().IntVar(&since, "since", 0, "only events with seq greater than this")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().BoolVar(&isRun, "run", false, "the selector is a run label, not a job ID")
	c.Flags().BoolVar(&isWorkspace, "workspace", false, "the selector is a workspace ID")
	return c
}

// printEventLog renders one event index (job, run, or workspace — each is a
// distinct log with its own sequence cursor; tail is the provenance-bearing
// merged view across them). A missing log reads as empty, not an error.
func printEventLog(path string, since int, asJSON bool) error {
	evs, err := events.Read(path, since)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if asJSON {
		return printJSON(evs)
	}
	for _, e := range evs {
		printEvent(e)
	}
	return nil
}

func printEvents(s *job.Store, sel job.Selection, since int, asJSON bool) error {
	if sel.Kind == job.SelectorJob {
		return printEventLog(s.EventsPath(sel.Newest().ID), since, asJSON)
	}
	path, err := s.RunEventsPath(sel.Selector)
	if err != nil {
		return err
	}
	return printEventLog(path, since, asJSON)
}

func printEvent(e events.Event) {
	fmt.Printf("%4d  %s  %-16s %s\n", e.Seq, e.Time.Format("15:04:05"), e.Type, e.Preview)
}

func watchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "watch <job>",
		Short: "Live-render a job's events until the turn ends",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			path := s.EventsPath(args[0])
			// A resumed job's log already holds earlier turns, each ending in a
			// terminal event. Start the cursor past finished turns so watch
			// follows the live one instead of replaying an old terminal event
			// and exiting immediately. For a job that isn't running, replay
			// just the most recent turn.
			cursor := 0
			m0, err := s.LoadMeta(args[0])
			if err != nil {
				return err
			}
			s.Reconcile(m0)
			live := m0.State == job.StateActive || m0.State == job.StateQueued
			var terminals []int
			if evs, err := events.Read(path, 0); err == nil {
				for _, e := range evs {
					if e.Type == events.TypeFinished || e.Type == events.TypeInterrupted {
						terminals = append(terminals, e.Seq)
					}
				}
			}
			if live && len(terminals) > 0 {
				cursor = terminals[len(terminals)-1]
			} else if !live && len(terminals) > 1 {
				cursor = terminals[len(terminals)-2]
			}
			for {
				evs, err := events.Read(path, cursor)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				for _, e := range evs {
					printEvent(e)
					cursor = e.Seq
					if e.Type == events.TypeFinished || e.Type == events.TypeInterrupted {
						return nil
					}
				}
				m, err := s.LoadMeta(args[0])
				if err != nil {
					return err
				}
				s.Reconcile(m)
				if m.State != job.StateActive && m.State != job.StateQueued {
					// Terminal and no more events coming.
					evs, _ := events.Read(path, cursor)
					for _, e := range evs {
						printEvent(e)
						cursor = e.Seq
					}
					return nil
				}
				time.Sleep(300 * time.Millisecond)
			}
		},
	}
	return c
}
