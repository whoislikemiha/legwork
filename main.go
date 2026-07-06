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
	"github.com/whoislikemiha/legwork/internal/config"
	"github.com/whoislikemiha/legwork/internal/doctor"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/fakeagent"
	"github.com/whoislikemiha/legwork/internal/gc"
	"github.com/whoislikemiha/legwork/internal/guide"
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
		Use:   "legwork",
		Short: "Delegate the legwork to headless coding agents",
		Long: `Delegate the legwork to headless coding agents: dispatch tasks as supervised
jobs, observe structured events, review diffs, steer with follow-up turns.

The loop: run -> (notification or status) -> done? verify : answer/resume -> close.
Run 'legwork guide' for the full orchestrator guide (notifications, workspaces,
health, recipes).`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(runCmd(), resumeCmd(), answerCmd(), statusCmd(), eventsCmd(),
		lsCmd(), watchCmd(), cancelCmd(), wsCmd(), diffCmd(), closeCmd(),
		noteCmd(), doctorCmd(), gcCmd(), guideCmd(), runnerCmd(), fakeAgentCmd())
	return root
}

// doctorCmd is preflight: validate the exact agent/model a run would use, plus
// the state dir, git, workstree pairing, and notifier — before dispatching.
// Exit 0 = no failures, 1 = one or more checks failed (report still printed),
// 2 = usage error (unknown agent, from adapter.New).
func doctorCmd() *cobra.Command {
	var agent, model, dir string
	var noProbe, asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Preflight: agent binary, auth, model, state dir, notifier before dispatching",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ad, err := adapter.New(agent)
			if err != nil {
				return err // unknown agent is a usage error -> exit 2
			}
			checks, ok := doctor.Run(doctor.Options{
				Adapter: ad, Model: model, Dir: dir, NoProbe: noProbe,
			})
			if asJSON {
				_ = printJSON(doctor.Report{OK: ok, Checks: checks})
			} else {
				for _, ck := range checks {
					fmt.Printf("%-10s %-5s %s\n", ck.Name, ck.Status, ck.Detail)
				}
			}
			if !ok {
				// Print the report, then signal failure without turning a
				// normal preflight failure into a usage/internal (exit 2) error.
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter to validate (claude, codex, fake)")
	c.Flags().StringVar(&model, "model", "", "model to validate (default: agent default)")
	c.Flags().StringVar(&dir, "dir", "", "repo to check for the worktree.toml/workstree pairing (default: cwd)")
	c.Flags().BoolVar(&noProbe, "no-probe", false, "skip the paid live-turn check (static checks only, offline-safe)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func guideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print the orchestrator guide (the loop, notifications, workspaces, health)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(guide.Text)
			return nil
		},
	}
}

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

// fakeAgentCmd must ship in the real binary: the fake adapter execs
// os.Executable() so tests exercise the actual spawn path.
func fakeAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_fake-agent",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fakeagent.Replay(os.Stdout)
		},
	}
}

func openStore() (*job.Store, error) { return job.OpenStore() }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// --- run ---

// validEffort reports whether e is one of claude's accepted --effort levels.
func validEffort(e string) bool {
	switch e {
	case "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

func runCmd() *cobra.Command {
	var agent, dir, model, appendPrompt, wsID, runLabel, timeout string
	var effort, fallbackModel string
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
			if timeout != "" {
				if _, err := time.ParseDuration(timeout); err != nil {
					return fmt.Errorf("--timeout: %w", err)
				}
			}
			// --effort and --fallback-model are claude-specific; codex has
			// no --fallback-model and a different effort model, so reject
			// them loudly rather than silently dropping them.
			if agent == "codex" {
				if effort != "" {
					return fmt.Errorf("--effort is claude-specific; not supported by --agent codex")
				}
				if fallbackModel != "" {
					return fmt.Errorf("--fallback-model is claude-specific; not supported by --agent codex")
				}
			}
			if effort != "" && !validEffort(effort) {
				return fmt.Errorf("--effort: %q not in low|medium|high|xhigh|max", effort)
			}
			m := &job.Meta{ID: id, Agent: agent, Task: args[0], Model: model, Run: runLabel,
				AppendPrompt: appendPrompt, ReadOnly: readOnly, Timeout: timeout,
				Effort: effort, FallbackModel: fallbackModel,
				State: job.StateQueued}
			if dir != "" && wsID != "" {
				return fmt.Errorf("--dir and --workspace are mutually exclusive")
			}
			if wsID != "" {
				_, wss, err := openWorkspaces()
				if err != nil {
					return err
				}
				wm, err := wss.Load(wsID)
				if err != nil {
					return err
				}
				if wm.State == "closed" {
					return fmt.Errorf("%s is closed", wsID)
				}
				if active, err := activeJobIn(s, wsID); err != nil {
					return err
				} else if active != "" {
					return fmt.Errorf("%s already has active job %s (one active job per workspace)", wsID, active)
				}
				m.Workspace = wsID
			}
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
			if runLabel != "" {
				if path, err := s.RunEventsPath(runLabel); err == nil {
					if rl, err := events.Open(path); err == nil {
						_, _ = rl.Append(events.Event{Type: events.TypeQueued, Actor: "orchestrator",
							Preview: events.Truncate(m.Task), Fields: map[string]any{"job": id}})
					}
				}
			}

			if err := runner.Spawn(s, m); err != nil {
				return err
			}
			gc.MaybeAuto(s)
			if asJSON {
				return printJSON(m)
			}
			fmt.Println(id)
			return nil
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter (claude, codex, fake)")
	c.Flags().StringVar(&dir, "dir", "", "run in-place in this directory (default: scratch dir)")
	c.Flags().StringVar(&wsID, "workspace", "", "attach the job to a workspace (see: legwork ws new)")
	c.Flags().StringVar(&runLabel, "run", "", "group the job under a run label")
	c.Flags().StringVar(&timeout, "timeout", "", "wall-clock limit for the turn (e.g. 30m); exceeded -> interrupted, session survives")
	c.Flags().StringVar(&model, "model", "", "model override (passed through to the agent)")
	c.Flags().StringVar(&effort, "effort", "", "claude only: reasoning effort (low|medium|high|xhigh|max)")
	c.Flags().StringVar(&fallbackModel, "fallback-model", "", "claude only: model to retry with when overloaded")
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
	s.Reconcile(m)
	if m.State == job.StateActive {
		return nil, fmt.Errorf("%s is active; cancel it first or wait for the turn to end", id)
	}
	if m.State == job.StateClosed {
		return nil, fmt.Errorf("%s is closed", id)
	}
	if m.Workspace != "" {
		if active, err := activeJobIn(s, m.Workspace); err != nil {
			return nil, err
		} else if active != "" && active != m.ID {
			return nil, fmt.Errorf("workspace %s has active job %s", m.Workspace, active)
		}
	}
	log, err := events.Open(filepath.Join(s.JobDir(id), "events.jsonl"))
	if err != nil {
		return nil, err
	}
	_, _ = log.Append(events.Event{Type: eventType, Actor: "orchestrator",
		Preview: events.Truncate(message)})
	// Task becomes the new turn's instruction; keep the dispatch prompt
	// recoverable (a cold orchestrator reconstructs jobs from meta alone).
	if m.InitialTask == "" {
		m.InitialTask = m.Task
	}
	m.Task = message
	m.Question = ""
	if err := s.SaveMeta(m); err != nil {
		return nil, err
	}
	if err := runner.Spawn(s, m); err != nil {
		return nil, err
	}
	gc.MaybeAuto(s)
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

// metaOut wraps a persisted Meta with the derived context_high signal, so
// --json carries it without touching the persisted struct. omitempty means the
// field only appears when high — additive, no schema break.
type metaOut struct {
	*job.Meta
	ContextHigh bool `json:"context_high,omitempty"`
}

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
			s.Reconcile(m)
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			high := m.ContextHigh(health.ContextThreshold)
			if asJSON {
				return printJSON(metaOut{Meta: m, ContextHigh: high})
			}
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
	var asJSON, isRun bool
	c := &cobra.Command{
		Use:   "events <job|run>",
		Short: "Read a job's event index, or a run's with --run (cursor with --since)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			var path string
			if isRun {
				if path, err = s.RunEventsPath(args[0]); err != nil {
					return err
				}
			} else {
				if _, err := s.LoadMeta(args[0]); err != nil {
					return err
				}
				path = filepath.Join(s.JobDir(args[0]), "events.jsonl")
			}
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
		},
	}
	c.Flags().IntVar(&since, "since", 0, "only events with seq greater than this")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().BoolVar(&isRun, "run", false, "the argument is a run label, not a job ID")
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
				s.Reconcile(m)
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
			for _, m := range metas {
				age := time.Since(m.Updated).Round(time.Second)
				// where: workspace for the reviewable-diff flow, "-" for
				// scratch/in-place — keeps parallel pipelines tellable apart.
				where := m.Workspace
				if where == "" {
					where = "-"
				}
				task := events.Truncate(m.Task)
				if m.Run != "" {
					task = "[" + m.Run + "] " + task
				}
				// ctx cell padded as one token so the "!" marker never shoves
				// later columns; field widened 7->9 to fit "ctx:180k!".
				ctx := "ctx:" + fmtContext(m.Context)
				if m.ContextHigh(health.ContextThreshold) {
					ctx += "!"
				}
				fmt.Printf("%-8s %-7s %-13s %6s  %-9s %-6s %s\n",
					m.ID, m.Agent, m.State, age, ctx, where, task)
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
