// Package dispatch owns the job lifecycle verbs behind the CLI: validating
// and creating a job, spawning its detached runner, and resuming an existing
// session with a follow-up instruction. The CLI files only parse flags and
// print; every state transition lives here.
package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/gc"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/notify"
	"github.com/whoislikemiha/legwork/internal/runner"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

// Options are the validated dispatch inputs (one per `run`/`ws review` flag).
type Options struct {
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

// validEffort reports whether e is one of claude's accepted --effort levels.
func validEffort(e string) bool {
	switch e {
	case "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// ResolveAppendPrompt merges the --append-prompt / --append-prompt-file pair
// into the final orchestrator addition ("-" reads stdin).
func ResolveAppendPrompt(appendPrompt, appendPromptFile string, stdin io.Reader) (string, error) {
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

// Dispatch validates o, persists a queued job, and spawns its detached runner.
func Dispatch(o Options) (*job.Meta, error) {
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
	if (o.Agent == "codex" || o.Agent == "hermes") && o.FallbackModel != "" {
		return nil, fmt.Errorf("--fallback-model is claude-specific; not supported by --agent %s", o.Agent)
	}
	if o.Effort != "" && !validEffort(o.Effort) {
		return nil, fmt.Errorf("--effort: %q not in low|medium|high|xhigh|max", o.Effort)
	}
	if o.Dir != "" && o.Workspace != "" {
		return nil, fmt.Errorf("--dir and --workspace are mutually exclusive")
	}
	ad, err := adapter.New(o.Agent)
	if err != nil {
		return nil, err
	}
	if o.ReadOnly && !ad.Caps().ReadOnly {
		return nil, fmt.Errorf("--agent %s has no harness-enforced read-only mode; use claude or codex for --read-only jobs", o.Agent)
	}
	if o.Agent == "hermes" && o.Effort != "" {
		return nil, fmt.Errorf("--effort is not supported by --agent hermes")
	}

	s, err := job.OpenStore()
	if err != nil {
		return nil, err
	}
	notifyConfig, err := notify.Load()
	if err != nil {
		return nil, fmt.Errorf("notifier config: %w", err)
	}
	origin, err := notify.CaptureCurrent(notifyConfig.Notify.CaptureEnv)
	if err != nil {
		return nil, err
	}
	m := &job.Meta{Agent: o.Agent, Task: o.Task, Model: o.Model, Run: o.RunLabel,
		AppendPrompt: o.AppendPrompt, ReadOnly: o.ReadOnly, Timeout: o.Timeout,
		Effort: o.Effort, FallbackModel: o.FallbackModel,
		Review: o.Review, State: job.StateQueued}
	if o.Workspace != "" {
		wss, err := workspace.Open(s.Root)
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
		if active, err := s.ActiveJobIn(o.Workspace); err != nil {
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
	if err := notify.SaveOrigin(s.JobDir(id), origin); err != nil {
		return nil, fmt.Errorf("persist notifier origin: %w", err)
	}
	log, err := events.Open(s.EventsPath(id))
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

// Resume continues a job's session with a new instruction.
func Resume(id, message, eventType string) (*job.Meta, error) {
	return ResumeWithEvent(id, message, eventType, events.Truncate(message), nil)
}

// ResumeWithEvent is Resume with an explicit event preview and fields (used
// by approve, which records the provision command's outcome alongside).
func ResumeWithEvent(id, message, eventType, preview string, fields map[string]any) (*job.Meta, error) {
	s, err := job.OpenStore()
	if err != nil {
		return nil, err
	}
	unlock, err := job.LockMeta(s.Root, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
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
	if m.VerificationLeaseLive(time.Now().UTC()) {
		return nil, fmt.Errorf("%s has live host verification lease; wait for it to finish", id)
	}
	// A timed-out/crashed verifier cannot hold the job indefinitely. This
	// happens under the same metadata lock as the following resume save.
	if m.VerificationLease != nil {
		m.VerificationLease = nil
	}
	if m.Workspace != "" {
		if active, err := s.ActiveJobIn(m.Workspace); err != nil {
			return nil, err
		} else if active != "" && active != m.ID {
			return nil, fmt.Errorf("workspace %s has active job %s", m.Workspace, active)
		}
	}
	log, err := events.Open(s.EventsPath(id))
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
	m.LatestVerification = nil
	if err := s.SaveMeta(m); err != nil {
		return nil, err
	}
	if m.Workspace != "" {
		if wss, err := workspace.Open(s.Root); err == nil {
			if err := wss.ClearVerification(m.Workspace, m.ID); err != nil {
				return nil, err
			}
		}
	}
	if err := runner.Spawn(s, m); err != nil {
		return nil, err
	}
	gc.MaybeAuto(s)
	return m, nil
}

// WorkDir resolves where a job's commands run: its workspace worktree, the
// explicit --dir target, or the job's scratch directory.
func WorkDir(s *job.Store, m *job.Meta) (string, error) {
	if m.Workspace != "" {
		wss, err := workspace.Open(s.Root)
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

// Provision runs an approved needs-provision command outside the sandbox.
func Provision(workDir, command string, timeout time.Duration) (string, int, error) {
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
