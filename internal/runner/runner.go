// Package runner is the detached job runner: the only code that touches a
// live agent process. `legwork run` spawns `legwork _runner --job N` in its
// own session and returns immediately; the runner execs the agent CLI, tees
// its stream to the transcript, derives index events, writes the result, and
// exits (DESIGN.md §11).
package runner

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/notify"
	"github.com/whoislikemiha/legwork/internal/rules"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

// finishJob centralizes end-of-turn bookkeeping: run-log marker + notifier.
func finishJob(store *job.Store, m *job.Meta, state string, preview string) {
	if m.Run != "" {
		if path, err := store.RunEventsPath(m.Run); err == nil {
			if rl, err := events.Open(path); err == nil {
				_, _ = rl.Append(events.Event{Type: events.TypeFinished, Actor: "runner",
					Preview: events.Truncate(preview),
					Fields:  map[string]any{"job": m.ID, "state": state}})
			}
		}
	}
	if cfg, err := notify.Load(); err == nil {
		_ = cfg.Send(notify.Payload{
			Event: state, Job: m.ID, Run: m.Run, Agent: m.Agent,
			Task: m.Task, Question: m.Question, Result: events.Truncate(m.Result),
			CostUSD: m.CostUSD, Context: m.Context,
		})
	}
}

// Spawn launches the detached runner for a job and records its PID. All
// per-job configuration travels in meta.json (never env), so resumed turns
// run with the same dispatch options as the first.
func Spawn(store *job.Store, m *job.Meta) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(store.JobDir(m.ID), "runner.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(self, "_runner", "--job", m.ID)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	// New session: survives the CLI exiting and the ssh connection dropping.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	m.RunnerPID = cmd.Process.Pid
	m.State = job.StateActive
	if err := store.SaveMeta(m); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Run executes one turn for the job; this is the _runner entrypoint.
func Run(store *job.Store, id string) error {
	m, err := store.LoadMeta(id)
	if err != nil {
		return err
	}
	dir := store.JobDir(id)
	log, err := events.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return err
	}

	fail := func(state job.State, format string, args ...any) error {
		msg := fmt.Sprintf(format, args...)
		m.State = state
		m.Result = msg
		m.RunnerPID = 0
		_ = store.SaveMeta(m)
		_, _ = log.Append(events.Event{Type: events.TypeFinished, Actor: "runner",
			Preview: msg, Fields: map[string]any{"state": string(state)}})
		finishJob(store, m, string(state), msg)
		return fmt.Errorf("%s", msg)
	}

	ad, err := adapter.New(m.Agent)
	if err != nil {
		return fail(job.StateFailed, "adapter: %v", err)
	}

	var ws *workspace.Meta
	var wsStore *workspace.Store
	workDir := m.Dir
	if m.Workspace != "" {
		wsStore, err = workspace.Open(store.Root)
		if err != nil {
			return fail(job.StateFailed, "workspace store: %v", err)
		}
		ws, err = wsStore.Load(m.Workspace)
		if err != nil {
			return fail(job.StateFailed, "workspace: %v", err)
		}
		workDir = ws.Tree
	}
	if workDir == "" {
		workDir = filepath.Join(dir, "scratch")
		if err := os.MkdirAll(workDir, 0o700); err != nil {
			return fail(job.StateFailed, "scratch dir: %v", err)
		}
	}

	req := adapter.TurnRequest{
		Task:          m.Task,
		SystemPrompt:  rules.Compose(m.AppendPrompt),
		SessionID:     m.SessionID,
		Model:         m.Model,
		WorkDir:       workDir,
		ReadOnly:      m.ReadOnly,
		Effort:        m.Effort,
		FallbackModel: m.FallbackModel,
	}
	tmp, err := prepareJobTemp(dir)
	if err != nil {
		return fail(job.StateFailed, "job temp: %v", err)
	}
	req.TempDir = tmp.Root
	cmd, err := ad.Command(req)
	if err != nil {
		return fail(job.StateFailed, "command: %v", err)
	}
	cmd.Env = tmp.ApplyEnv(cmd.Env, m.Agent)

	transcript, err := os.OpenFile(filepath.Join(dir, "transcript.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(job.StateFailed, "transcript: %v", err)
	}
	defer transcript.Close()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(job.StateFailed, "pipe: %v", err)
	}
	cmd.Stderr = transcript

	if err := cmd.Start(); err != nil {
		return fail(job.StateFailed, "starting %s: %v", m.Agent, err)
	}
	_, _ = log.Append(events.Event{Type: events.TypeStarted, Actor: "runner",
		Preview: events.Truncate(m.Task)})

	// Wall-clock guard: budgets cap tokens, this caps time. A hung agent
	// (or hung test suite inside it) must not hold a job open forever.
	var timedOut bool
	if m.Timeout != "" {
		if d, perr := time.ParseDuration(m.Timeout); perr == nil && d > 0 {
			timer := time.AfterFunc(d, func() {
				timedOut = true
				_ = cmd.Process.Kill()
			})
			defer timer.Stop()
		}
	}

	// Tee: every raw line to the transcript; parsed events to the index.
	parser := ad.Parser()
	var result *adapter.TurnResult
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		transcript.Write(raw)
		transcript.Write([]byte{'\n'})
		evs, res, perr := parser.Line(raw)
		if perr != nil {
			continue
		}
		for _, e := range evs {
			_, _ = log.Append(e)
		}
		if res != nil {
			result = res
		}
	}

	waitErr := cmd.Wait()
	if timedOut {
		return fail(job.StateInterrupted, "turn exceeded --timeout %s; session survives, resume or restart fresh", m.Timeout)
	}
	if result == nil {
		// Mid-turn death: process ended without a result line.
		return fail(job.StateInterrupted, "agent exited without a result (%v)", waitErr)
	}

	// Persist outcome. Only overwrite the session id when the turn emitted one:
	// a resumable session id, once known, must survive a turn that completes
	// without re-reporting it (protects every adapter, not just codex).
	if result.SessionID != "" {
		m.SessionID = result.SessionID
	}
	m.Result = result.Result
	m.Question = result.Question
	m.CostUSD += result.CostUSD
	m.Turns += result.Turns
	m.TokensIn += result.TokensIn
	m.TokensOut += result.TokensOut
	m.Context = result.Context
	m.State = job.State(result.State)
	m.RunnerPID = 0
	if err := store.SaveMeta(m); err != nil {
		return err
	}

	_, _ = log.Append(events.Event{Type: events.TypeUsage, Actor: "runner", Fields: map[string]any{
		"cost_usd": result.CostUSD, "tokens_in": result.TokensIn, "tokens_out": result.TokensOut}})
	// Workspace turns end with a checkpoint: the diff timeline spans the lineage.
	if ws != nil {
		if ref, oid, cerr := wsStore.Checkpoint(ws); cerr == nil {
			_, _ = log.Append(events.Event{Type: events.TypeCheckpoint, Actor: "runner",
				Preview: ref, Fields: map[string]any{"ref": ref, "oid": oid}})
		} else {
			_, _ = log.Append(events.Event{Type: events.TypeProgress, Actor: "runner",
				Preview: "checkpoint failed: " + cerr.Error()})
		}
	}
	if result.State == "needs-input" {
		_, _ = log.Append(events.Event{Type: events.TypeNeedsInput, Actor: "main",
			Preview: events.Truncate(result.Question)})
	}
	_, _ = log.Append(events.Event{Type: events.TypeFinished, Actor: "runner",
		Preview: events.Truncate(result.Result),
		Fields:  map[string]any{"state": result.State}})
	finishJob(store, m, result.State, firstNonEmpty(result.Question, result.Result))
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type jobTemp struct {
	Root    string
	GoCache string
	GoMod   string
	GoTmp   string
}

func prepareJobTemp(jobDir string) (jobTemp, error) {
	t := jobTemp{
		Root:    filepath.Join(jobDir, "tmp"),
		GoCache: filepath.Join(jobDir, "tmp", "go-build"),
		GoMod:   filepath.Join(jobDir, "tmp", "go-mod"),
		GoTmp:   filepath.Join(jobDir, "tmp", "go-tmp"),
	}
	for _, dir := range []string{t.Root, t.GoCache, t.GoMod, t.GoTmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return jobTemp{}, err
		}
	}
	return t, nil
}

func (t jobTemp) ApplyEnv(base []string, agent string) []string {
	env := base
	if len(env) == 0 {
		env = os.Environ()
	}
	overrides := []string{"TMPDIR=" + t.Root}
	if agent == "codex" {
		overrides = append(overrides,
			"GOCACHE="+t.GoCache,
			"GOMODCACHE="+t.GoMod,
			"GOTMPDIR="+t.GoTmp,
		)
	}
	return mergeEnv(env, overrides...)
}

func mergeEnv(base []string, overrides ...string) []string {
	out := append([]string{}, base...)
	index := map[string]int{}
	for i, kv := range out {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			index[kv[:eq]] = i
		}
	}
	for _, kv := range overrides {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k := kv[:eq]
		if i, ok := index[k]; ok {
			out[i] = kv
			continue
		}
		index[k] = len(out)
		out = append(out, kv)
	}
	return out
}
