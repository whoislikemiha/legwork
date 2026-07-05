package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/runner"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "legwork: %v\n", err)
		os.Exit(2)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "legwork",
		Short:         "Delegate the legwork to headless coding agents",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(runCmd(), resumeCmd(), answerCmd(), statusCmd(), eventsCmd(),
		lsCmd(), watchCmd(), cancelCmd(), runnerCmd(), fakeAgentCmd())
	return root
}

func openStore() (*job.Store, error) { return job.OpenStore() }

// reconcile flips a stale active job (dead runner) to interrupted.
func reconcile(s *job.Store, m *job.Meta) {
	if m.State == job.StateActive && !s.Alive(m) {
		m.State = job.StateInterrupted
		m.RunnerPID = 0
		_ = s.SaveMeta(m)
		log, err := events.Open(filepath.Join(s.JobDir(m.ID), "events.jsonl"))
		if err == nil {
			_, _ = log.Append(events.Event{Type: events.TypeInterrupted, Actor: "runner",
				Preview: "runner died without finishing the turn"})
		}
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- run ---

func runCmd() *cobra.Command {
	var agent, dir, model, appendPrompt string
	var readOnly, asJSON bool
	c := &cobra.Command{
		Use:   "run <task>",
		Short: "Start a job; prints the job ID immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			id, err := s.NewID()
			if err != nil {
				return err
			}
			m := &job.Meta{ID: id, Agent: agent, Task: args[0], Model: model, State: job.StateQueued}
			if dir != "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				m.Dir = abs
			}
			if _, err := adapter.New(agent); err != nil {
				return err
			}
			if err := s.Create(m); err != nil {
				return err
			}
			log, err := events.Open(filepath.Join(s.JobDir(id), "events.jsonl"))
			if err != nil {
				return err
			}
			_, _ = log.Append(events.Event{Type: events.TypeQueued, Actor: "orchestrator",
				Preview: events.Truncate(m.Task)})

			env := []string{"LEGWORK_APPEND_PROMPT=" + appendPrompt}
			if readOnly {
				env = append(env, "LEGWORK_READ_ONLY=1")
			}
			if err := runner.Spawn(s, m, env); err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Println(id)
			return nil
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter (claude, fake)")
	c.Flags().StringVar(&dir, "dir", "", "run in-place in this directory (default: scratch dir)")
	c.Flags().StringVar(&model, "model", "", "model override (passed through to the agent)")
	c.Flags().StringVar(&appendPrompt, "append-prompt", "", "orchestrator additions to the injected worker rules")
	c.Flags().BoolVar(&readOnly, "read-only", false, "read-only turn (plan/research)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

// --- resume / answer ---

func doResume(id, message, eventType string) (*job.Meta, error) {
	s, err := openStore()
	if err != nil {
		return nil, err
	}
	m, err := s.LoadMeta(id)
	if err != nil {
		return nil, err
	}
	reconcile(s, m)
	if m.State == job.StateActive {
		return nil, fmt.Errorf("%s is active; cancel it first or wait for the turn to end", id)
	}
	if m.State == job.StateClosed {
		return nil, fmt.Errorf("%s is closed", id)
	}
	log, err := events.Open(filepath.Join(s.JobDir(id), "events.jsonl"))
	if err != nil {
		return nil, err
	}
	_, _ = log.Append(events.Event{Type: eventType, Actor: "orchestrator",
		Preview: events.Truncate(message)})
	m.Task = message
	m.Question = ""
	if err := s.SaveMeta(m); err != nil {
		return nil, err
	}
	if err := runner.Spawn(s, m, nil); err != nil {
		return nil, err
	}
	return m, nil
}

func resumeCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "resume <job> <message>",
		Short: "Continue a job's session with a new instruction",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := doResume(args[0], args[1], events.TypeResume)
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
	return c
}

func answerCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "answer <job> <answer>",
		Short: "Answer a needs-input question and continue the job",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := doResume(args[0], args[1], events.TypeAnswer)
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
	return c
}

// --- status / events / ls / watch / cancel ---

func statusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status <job>",
		Short: "Job rollup: state, telemetry, question/result",
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
			reconcile(s, m)
			if asJSON {
				return printJSON(m)
			}
			fmt.Printf("job:    %s (%s)\nstate:  %s\ntask:   %s\n", m.ID, m.Agent, m.State, m.Task)
			fmt.Printf("cost:   $%.4f  turns: %d  tokens: %d in / %d out\n",
				m.CostUSD, m.Turns, m.TokensIn, m.TokensOut)
			if m.Question != "" {
				fmt.Printf("question: %s\n", m.Question)
			}
			if m.Result != "" {
				fmt.Printf("result:\n%s\n", m.Result)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func eventsCmd() *cobra.Command {
	var since int
	var asJSON bool
	c := &cobra.Command{
		Use:   "events <job>",
		Short: "Read a job's event index (cursor with --since)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if _, err := s.LoadMeta(args[0]); err != nil {
				return err
			}
			evs, err := events.Read(filepath.Join(s.JobDir(args[0]), "events.jsonl"), since)
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
		},
	}
	c.Flags().IntVar(&since, "since", 0, "only events with seq greater than this")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func printEvent(e events.Event) {
	fmt.Printf("%4d  %s  %-12s %s\n", e.Seq, e.Time.Format("15:04:05"), e.Type, e.Preview)
}

func lsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "ls",
		Short: "All jobs: state, age, telemetry",
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
				reconcile(s, m)
			}
			if asJSON {
				return printJSON(metas)
			}
			for _, m := range metas {
				age := time.Since(m.Updated).Round(time.Second)
				fmt.Printf("%-8s %-7s %-13s %6s  $%.4f  %s\n",
					m.ID, m.Agent, m.State, age, m.CostUSD, events.Truncate(m.Task))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
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
			path := filepath.Join(s.JobDir(args[0]), "events.jsonl")
			cursor := 0
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
				reconcile(s, m)
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
			log, err := events.Open(filepath.Join(s.JobDir(m.ID), "events.jsonl"))
			if err == nil {
				_, _ = log.Append(events.Event{Type: events.TypeCancel, Actor: "orchestrator"})
			}
			fmt.Printf("%s: interrupt sent\n", m.ID)
			return nil
		},
	}
	return c
}

// --- hidden entrypoints ---

func runnerCmd() *cobra.Command {
	var jobID string
	c := &cobra.Command{
		Use:    "_runner",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			return runner.Run(s, jobID)
		},
	}
	c.Flags().StringVar(&jobID, "job", "", "job id")
	_ = c.MarkFlagRequired("job")
	return c
}
