package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

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
		code := 2
		if e, ok := err.(interface{ ExitCode() int }); ok {
			code = e.ExitCode()
		}
		if e, ok := err.(interface{ Silent() bool }); !ok || !e.Silent() {
			fmt.Fprintf(os.Stderr, "legwork: %v\n", err)
		}
		os.Exit(code)
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
		Version:       versionSummary(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(runCmd(), resumeCmd(), answerCmd(), approveCmd(), statusCmd(), eventsCmd(),
		resultCmd(), lsCmd(), watchCmd(), cancelCmd(), ackCmd(), wsCmd(), diffCmd(), closeCmd(),
		noteCmd(), doctorCmd(), gcCmd(), guideCmd(), runnerCmd(), fakeAgentCmd(),
		runsCmd(), tailCmd(), dashboardCmd(), serveCmd(), artifactCmd(), versionCmd(), rulesCmd(),
		skillCmd(), waitCmd())
	return root
}

type commandError struct {
	code    int
	message string
	silent  bool
}

func (e commandError) Error() string { return e.message }
func (e commandError) ExitCode() int { return e.code }
func (e commandError) Silent() bool  { return e.silent }

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

func resolveAppendPrompt(appendPrompt, appendPromptFile string, stdin io.Reader) (string, error) {
	if appendPrompt != "" && appendPromptFile != "" {
		return "", fmt.Errorf("--append-prompt and --append-prompt-file are mutually exclusive")
	}
	if appendPromptFile == "" {
		return appendPrompt, nil
	}

	var (
		b   []byte
		err error
	)
	if appendPromptFile == "-" {
		b, err = io.ReadAll(stdin)
	} else {
		b, err = os.ReadFile(appendPromptFile)
	}
	if err != nil {
		return "", fmt.Errorf("--append-prompt-file: %w", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return "", fmt.Errorf("--append-prompt-file: empty append prompt")
	}
	if !utf8.Valid(b) || bytes.Contains(b, []byte{0}) {
		return "", fmt.Errorf("--append-prompt-file: input must be UTF-8 text")
	}
	return string(b), nil
}

type dispatchOptions struct {
	Agent         string
	Task          string
	Dir           string
	Workspace     string
	RunLabel      string
	Timeout       string
	Model         string
	Effort        string
	FallbackModel string
	AppendPrompt  string
	ReadOnly      bool
	Review        *job.ReviewRequest
}

func dispatchJob(o dispatchOptions) (*job.Meta, error) {
	if o.RunLabel != "" {
		if err := job.ValidateRunLabel(o.RunLabel); err != nil {
			return nil, err
		}
	}
	if o.Timeout != "" {
		if _, err := time.ParseDuration(o.Timeout); err != nil {
			return nil, fmt.Errorf("--timeout: %w", err)
		}
	}
	// --effort reaches both claude and codex (codex clamps xhigh/max to its
	// "high" ceiling). --fallback-model is claude-specific — codex has no
	// such flag — so reject it loudly rather than silently dropping it.
	if o.Agent == "codex" && o.FallbackModel != "" {
		return nil, fmt.Errorf("--fallback-model is claude-specific; not supported by --agent codex")
	}
	if o.Effort != "" && !validEffort(o.Effort) {
		return nil, fmt.Errorf("--effort: %q not in low|medium|high|xhigh|max", o.Effort)
	}
	if o.Dir != "" && o.Workspace != "" {
		return nil, fmt.Errorf("--dir and --workspace are mutually exclusive")
	}
	if _, err := adapter.New(o.Agent); err != nil {
		return nil, err
	}

	s, err := openStore()
	if err != nil {
		return nil, err
	}
	m := &job.Meta{Agent: o.Agent, Task: o.Task, Model: o.Model, Run: o.RunLabel,
		AppendPrompt: o.AppendPrompt, ReadOnly: o.ReadOnly, Timeout: o.Timeout,
		Effort: o.Effort, FallbackModel: o.FallbackModel,
		Review: o.Review, State: job.StateQueued}
	if o.Workspace != "" {
		_, wss, err := openWorkspaces()
		if err != nil {
			return nil, err
		}
		wm, err := wss.Load(o.Workspace)
		if err != nil {
			return nil, err
		}
		if wm.State == "closed" {
			return nil, fmt.Errorf("%s is closed", o.Workspace)
		}
		if active, err := activeJobIn(s, o.Workspace); err != nil {
			return nil, err
		} else if active != "" {
			return nil, fmt.Errorf("%s already has active job %s (one active job per workspace)", o.Workspace, active)
		}
		m.Workspace = o.Workspace
	}
	if o.Dir != "" {
		abs, err := filepath.Abs(o.Dir)
		if err != nil {
			return nil, err
		}
		m.Dir = abs
	}

	id, err := s.NewID()
	if err != nil {
		return nil, err
	}
	m.ID = id
	if err := s.Create(m); err != nil {
		return nil, err
	}
	log, err := events.Open(filepath.Join(s.JobDir(id), "events.jsonl"))
	if err != nil {
		return nil, err
	}
	_, _ = log.Append(events.Event{Type: events.TypeQueued, Actor: "orchestrator",
		Preview: events.Truncate(m.Task)})
	if o.RunLabel != "" {
		if path, err := s.RunEventsPath(o.RunLabel); err == nil {
			if rl, err := events.Open(path); err == nil {
				_, _ = rl.Append(events.Event{Type: events.TypeQueued, Actor: "orchestrator",
					Preview: events.Truncate(m.Task), Fields: map[string]any{"job": id}})
			}
		}
	}

	if err := runner.Spawn(s, m); err != nil {
		return nil, err
	}
	gc.MaybeAuto(s)
	return m, nil
}

func runCmd() *cobra.Command {
	var agent, dir, model, appendPrompt, appendPromptFile, wsID, runLabel, timeout string
	var effort, fallbackModel string
	var readOnly, asJSON bool
	c := &cobra.Command{
		Use:   "run <task>",
		Short: "Start a job; prints the job ID immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedAppendPrompt, err := resolveAppendPrompt(appendPrompt, appendPromptFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			m, err := dispatchJob(dispatchOptions{
				Agent: agent, Task: args[0], Dir: dir, Workspace: wsID,
				RunLabel: runLabel, Timeout: timeout, Model: model, Effort: effort,
				FallbackModel: fallbackModel, AppendPrompt: resolvedAppendPrompt,
				ReadOnly: readOnly,
			})
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
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter (claude, codex, fake)")
	c.Flags().StringVar(&dir, "dir", "", "run in-place in this directory (default: scratch dir)")
	c.Flags().StringVar(&wsID, "workspace", "", "attach the job to a workspace (see: legwork ws new)")
	c.Flags().StringVar(&runLabel, "run", "", "group the job under a run label")
	c.Flags().StringVar(&timeout, "timeout", "", "wall-clock limit for the turn (e.g. 30m); exceeded -> interrupted, session survives")
	c.Flags().StringVar(&model, "model", "", "model override (passed through to the agent)")
	c.Flags().StringVar(&effort, "effort", "", "reasoning effort (low|medium|high|xhigh|max); codex clamps xhigh/max to high")
	c.Flags().StringVar(&fallbackModel, "fallback-model", "", "claude only: model to retry with when overloaded")
	c.Flags().StringVar(&appendPrompt, "append-prompt", "", "orchestrator additions to the injected worker rules")
	c.Flags().StringVar(&appendPromptFile, "append-prompt-file", "", "read orchestrator additions from a UTF-8 text file, or - for stdin")
	c.Flags().BoolVar(&readOnly, "read-only", false, "read-only turn (plan/research)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

// --- resume / answer ---

func doResume(id, message, eventType string) (*job.Meta, error) {
	return doResumeWithEvent(id, message, eventType, events.Truncate(message), nil)
}

func doResumeWithEvent(id, message, eventType, preview string, fields map[string]any) (*job.Meta, error) {
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
		Preview: events.Truncate(preview), Fields: fields})
	// Task becomes the new turn's instruction; keep the dispatch prompt
	// recoverable (a cold orchestrator reconstructs jobs from meta alone).
	if m.InitialTask == "" {
		m.InitialTask = m.Task
	}
	m.Task = message
	m.Question = ""
	m.Blocked = nil
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

func approveCmd() *cobra.Command {
	var asJSON bool
	var provisionTimeout string
	c := &cobra.Command{
		Use:   "approve <job>",
		Short: "Approve a needs-provision command and continue the job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, err := time.ParseDuration(provisionTimeout)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("--timeout must be a positive duration: %q", provisionTimeout)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadMeta(args[0])
			if err != nil {
				return err
			}
			s.Reconcile(m)
			if m.State == job.StateActive {
				return fmt.Errorf("%s is active; cancel it first or wait for the turn to end", m.ID)
			}
			if m.State == job.StateClosed {
				return fmt.Errorf("%s is closed", m.ID)
			}
			if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "provision" {
				return fmt.Errorf("%s is %s, not needs-provision", m.ID, m.State)
			}
			if strings.TrimSpace(m.Blocked.Command) == "" {
				return fmt.Errorf("%s needs-provision has no command", m.ID)
			}
			if m.Workspace != "" {
				if active, err := activeJobIn(s, m.Workspace); err != nil {
					return err
				} else if active != "" && active != m.ID {
					return fmt.Errorf("workspace %s has active job %s", m.Workspace, active)
				}
			}
			workDir, err := jobWorkDir(s, m)
			if err != nil {
				return err
			}
			out, exitCode, runErr := runProvision(workDir, m.Blocked.Command, timeout)
			fields := map[string]any{
				"blocked":   m.Blocked,
				"command":   m.Blocked.Command,
				"exit_code": exitCode,
				"output":    events.Truncate(strings.TrimSpace(out)),
			}
			if runErr != nil {
				if log, lerr := events.Open(filepath.Join(s.JobDir(m.ID), "events.jsonl")); lerr == nil {
					_, _ = log.Append(events.Event{Type: events.TypeApprove, Actor: "orchestrator",
						Preview: "provision command failed: " + events.Truncate(m.Blocked.Command),
						Fields:  fields})
				}
				return fmt.Errorf("provision command failed: %v\n%s", runErr, strings.TrimSpace(out))
			}
			message := "Provisioning command was approved and completed outside the sandbox. Continue the task.\n\nCommand: " + m.Blocked.Command
			resumed, err := doResumeWithEvent(m.ID, message, events.TypeApprove,
				"approved provision: "+m.Blocked.Command, fields)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(resumed)
			}
			fmt.Println(resumed.ID)
			return nil
		},
	}
	c.Flags().StringVar(&provisionTimeout, "timeout", "30m", "wall-clock limit for the provision command")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func jobWorkDir(s *job.Store, m *job.Meta) (string, error) {
	if m.Workspace != "" {
		_, wss, err := openWorkspaces()
		if err != nil {
			return "", err
		}
		wm, err := wss.Load(m.Workspace)
		if err != nil {
			return "", err
		}
		return wm.Tree, nil
	}
	if m.Dir != "" {
		return m.Dir, nil
	}
	dir := filepath.Join(s.JobDir(m.ID), "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func runProvision(workDir, command string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), -1, fmt.Errorf("timed out after %s", timeout)
	}
	exitCode := 0
	if err != nil {
		exitCode = 1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}
	return string(out), exitCode, err
}

// --- status / events / ls / watch / cancel ---

// metaOut wraps a persisted Meta with the derived context_high signal, so
// --json carries it without touching the persisted struct. omitempty means the
// field only appears when high — additive, no schema break.
type metaOut struct {
	*job.Meta
	ContextHigh  bool   `json:"context_high,omitempty"`
	Selector     string `json:"selector"`
	SelectorKind string `json:"selector_kind"`
	ResolvedJob  string `json:"resolved_job"`
}

// waitOut is deliberately a small envelope around the persisted job record:
// scripts get the same final metadata that status reads, plus the wait result
// without having to infer it from an exit status alone.
type waitOut struct {
	Job       *job.Meta   `json:"job"`
	Outcome   string      `json:"outcome"` // reached | timeout | terminal-mismatch
	Reached   job.State   `json:"reached"`
	WaitedFor []job.State `json:"waited_for"`
	ElapsedMS int64       `json:"elapsed_ms"`
}

type resultOut struct {
	Job          string `json:"job"`
	Run          string `json:"run,omitempty"`
	Turn         int    `json:"turn,omitempty"`
	State        string `json:"state"`
	Result       string `json:"result"`
	Selector     string `json:"selector"`
	SelectorKind string `json:"selector_kind"`
	ResolvedJob  string `json:"resolved_job"`
}

func resolveReadSelector(s *job.Store, positional, jobFlag, runFlag string) (job.Selection, error) {
	if positional != "" && (jobFlag != "" || runFlag != "") {
		return job.Selection{}, fmt.Errorf("a positional selector cannot be combined with --job or --run")
	}
	if jobFlag != "" && runFlag != "" {
		return job.Selection{}, fmt.Errorf("--job and --run are mutually exclusive")
	}
	if jobFlag != "" {
		return job.Resolve(s, jobFlag, job.SelectorJob)
	}
	if runFlag != "" {
		return job.Resolve(s, runFlag, job.SelectorRun)
	}
	return job.Resolve(s, positional, "")
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
			if asJSON {
				return printJSON(metaOut{Meta: m, ContextHigh: high, Selector: sel.Selector,
					SelectorKind: string(sel.Kind), ResolvedJob: m.ID})
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
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().StringVar(&runLabel, "run", "", "force a run label selector")
	return c
}

// waitCmd is the exact-job counterpart to tail --until-idle. It reloads the
// persisted metadata on each pass so a detached runner's terminal write wins
// over any older snapshot held by this process. Reconcile is safe here: it is
// the shared dead-runner path and reloads before it ever persists interruption.
func waitCmd() *cobra.Command {
	var until, timeout string
	var asJSON bool
	c := &cobra.Command{
		Use:   "wait <job>",
		Short: "Block until one exact job reaches attention or a requested state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wanted, err := parseWaitStates(until, cmd.Flags().Changed("until"))
			if err != nil {
				return err
			}
			var deadline time.Time
			if cmd.Flags().Changed("timeout") {
				d, err := time.ParseDuration(timeout)
				if err != nil || d <= 0 {
					return fmt.Errorf("--timeout must be a positive duration")
				}
				deadline = time.Now().Add(d)
			}

			s, err := openStore()
			if err != nil {
				return err
			}
			// Validate the exact ID before starting the clock. Unlike the other
			// read commands, wait intentionally never resolves a run label.
			if _, err := s.LoadMeta(args[0]); err != nil {
				return err
			}

			started := time.Now()
			for {
				m, err := s.LoadMeta(args[0])
				if err != nil {
					return err
				}
				s.Reconcile(m)

				outcome := ""
				if len(wanted) == 0 && !waitLive(m.State) {
					outcome = "reached"
				} else if len(wanted) > 0 && wanted[m.State] {
					outcome = "reached"
				} else if len(wanted) > 0 && !waitLive(m.State) {
					outcome = "terminal-mismatch"
				} else if !deadline.IsZero() && !time.Now().Before(deadline) {
					outcome = "timeout"
				}

				if outcome != "" {
					return finishWait(cmd, asJSON, m, outcome, waitStatesForOutput(wanted), time.Since(started))
				}

				delay := 100 * time.Millisecond
				if !deadline.IsZero() {
					remaining := time.Until(deadline)
					if remaining > 0 && remaining < delay {
						delay = remaining
					}
				}
				time.Sleep(delay)
			}
		},
	}
	c.Flags().StringVar(&until, "until", "", "comma-separated job states to wait for")
	c.Flags().StringVar(&timeout, "timeout", "", "positive maximum wait duration")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func parseWaitStates(raw string, supplied bool) (map[job.State]bool, error) {
	if !supplied {
		return nil, nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--until requires at least one state")
	}
	return parseJobStates(raw)
}

func waitLive(state job.State) bool {
	return state == job.StateQueued || state == job.StateActive
}

func waitStatesForOutput(wanted map[job.State]bool) []job.State {
	if len(wanted) == 0 {
		return []job.State{}
	}
	states := make([]job.State, 0, len(wanted))
	for state := range wanted {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })
	return states
}

func finishWait(cmd *cobra.Command, asJSON bool, m *job.Meta, outcome string, waitedFor []job.State, elapsed time.Duration) error {
	out := waitOut{Job: m, Outcome: outcome, Reached: m.State, WaitedFor: waitedFor, ElapsedMS: elapsed.Milliseconds()}
	if asJSON {
		if err := printJSON(out); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s (%s; %dms)\n", m.ID, m.State, outcome, out.ElapsedMS)
	}
	if outcome == "reached" {
		return nil
	}
	return commandError{code: 1, silent: true}
}

func resultCmd() *cobra.Command {
	var asJSON bool
	var turn int
	var jobID, runLabel string
	c := &cobra.Command{
		Use:   "result [selector] [--job <id> | --run <label>]",
		Short: "Print a final report; a run selector resolves to its newest job",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("turn") && turn <= 0 {
				return fmt.Errorf("--turn must be positive")
			}
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
				return fmt.Errorf("run %q has no jobs; result requires a job", sel.Selector)
			}
			s.Reconcile(m)

			var res resultOut
			if turn > 0 {
				res, err = retainedResult(s, m, turn)
				if err != nil {
					return err
				}
			} else {
				if m.State == job.StateQueued || m.State == job.StateActive {
					exitNoResult("%s has no result yet", m.ID)
				}
				res = resultOut{Job: m.ID, Run: m.Run, State: string(m.State), Result: m.Result}
				if asJSON {
					n, err := retainedResultCount(s, m)
					if err != nil {
						return err
					}
					res.Turn = n
				}
			}

			res.Selector, res.SelectorKind, res.ResolvedJob = sel.Selector, string(sel.Kind), m.ID
			if asJSON {
				return printJSON(res)
			}
			resolutionNotice(cmd, sel, m)
			fmt.Print(res.Result)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	c.Flags().IntVar(&turn, "turn", 0, "print the Nth retained turn result (1-based)")
	c.Flags().StringVar(&jobID, "job", "", "force an exact job ID selector")
	c.Flags().StringVar(&runLabel, "run", "", "force a run label selector")
	return c
}

func retainedResult(s *job.Store, m *job.Meta, turn int) (resultOut, error) {
	results, err := retainedResults(s, m)
	if err != nil {
		return resultOut{}, err
	}
	if turn == 0 || turn > len(results) {
		return resultOut{}, fmt.Errorf("%s turn %d result is not retained", m.ID, turn)
	}
	res := results[turn-1]
	return resultOut{Job: m.ID, Run: m.Run, Turn: turn, State: res.State, Result: res.Result}, nil
}

func retainedResultCount(s *job.Store, m *job.Meta) (int, error) {
	results, err := retainedResults(s, m)
	if err != nil {
		return 0, err
	}
	return len(results), nil
}

func retainedResults(s *job.Store, m *job.Meta) ([]*adapter.TurnResult, error) {
	r, err := openRetainedTranscript(s, m)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	defer r.Close()
	ad, err := adapter.New(m.Agent)
	if err != nil {
		return nil, err
	}
	parser := ad.Parser()
	var results []*adapter.TurnResult
	// transcript.jsonl also captures agent stderr. Non-JSON noise is ignored
	// by the adapters, so replaying it preserves the same parser behavior as
	// the live runner without treating the transcript as a clean stdout stream.
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for sc.Scan() {
		_, res, perr := parser.Line(sc.Bytes())
		if perr != nil {
			continue
		}
		if res != nil {
			results = append(results, res)
			parser = ad.Parser()
		}
	}
	return results, sc.Err()
}

func openRetainedTranscript(s *job.Store, m *job.Meta) (io.ReadCloser, error) {
	plain := filepath.Join(s.JobDir(m.ID), "transcript.jsonl")
	f, err := os.Open(plain)
	if err == nil {
		return f, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	gzf, err := os.Open(plain + ".gz")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	zr, err := gzip.NewReader(gzf)
	if err != nil {
		gzf.Close()
		return nil, err
	}
	return multiReadCloser{Reader: zr, closers: []io.Closer{zr, gzf}}, nil
}

type multiReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (m multiReadCloser) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); first == nil && err != nil {
			first = err
		}
	}
	return first
}

func exitNoResult(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func eventsCmd() *cobra.Command {
	var since int
	var asJSON, isRun bool
	var jobID string
	c := &cobra.Command{
		Use:   "events [selector] [--job <id> | --run]",
		Short: "Read a job or run event index (cursor with --since)",
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
	return c
}

// printEvents preserves the stable per-log event API. Run logs are a distinct
// index with their own sequence cursor; tail is the provenance-bearing merged
// view across job and run logs.
func printEvents(s *job.Store, sel job.Selection, since int, asJSON bool) error {
	if sel.Kind == job.SelectorJob {
		evs, err := events.Read(filepath.Join(s.JobDir(sel.Newest().ID), "events.jsonl"), since)
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
	path, err := s.RunEventsPath(sel.Selector)
	if err != nil {
		return err
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
	fmt.Printf("%4d  %s  %-16s %s\n", e.Seq, e.Time.Format("15:04:05"), e.Type, e.Preview)
}

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
				prefix := fmt.Sprintf("%-8s %-18s %-13s %6s %-9s %-6s ",
					collapseWhitespace(m.ID), clip(agent, 18),
					clip(collapseWhitespace(string(m.State)), 13), fmtAge(m.Updated), ctx, where)
				if len([]rune(prefix)) >= width {
					fmt.Println(clip(prefix, width))
					continue
				}
				fmt.Println(prefix + clip(lsSummary(m), width-len([]rune(prefix))))
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
		if ar, br := lsAttentionRank(a.State), lsAttentionRank(b.State); ar != br {
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

func lsAttentionRank(s job.State) int {
	switch s {
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
	task := collapseWhitespace(m.Task)
	if task == "" {
		task = "-"
	}
	if run := collapseWhitespace(m.Run); run != "" {
		return "[" + run + "] " + task
	}
	return task
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func ackCmd() *cobra.Command {
	var asJSON, force bool
	c := &cobra.Command{
		Use:   "ack <job>",
		Short: "Acknowledge a terminal workspace-less job",
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
			if m.Workspace != "" {
				return fmt.Errorf("%s belongs to workspace %s; close the workspace with legwork close %s", m.ID, m.Workspace, m.Workspace)
			}
			if m.State == job.StateClosed {
				return fmt.Errorf("%s is already closed", m.ID)
			}
			switch m.State {
			case job.StateActive:
				return fmt.Errorf("%s is active; cancel it first or wait for the turn to end", m.ID)
			case job.StateQueued:
				return fmt.Errorf("%s is queued; cancel it first or wait for the turn to start", m.ID)
			}
			if !job.Terminal(m.State) && !force {
				return fmt.Errorf("%s is %s; only terminal jobs can be acknowledged without --force", m.ID, m.State)
			}
			if err := s.Close(m); err != nil {
				return err
			}
			if asJSON {
				return printJSON(m)
			}
			fmt.Printf("%s acknowledged\n", m.ID)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "acknowledge a non-terminal workspace-less job after explicit operator review")
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
