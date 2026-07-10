package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/notify"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

const verificationOutputLimit = 64 * 1024

var secretEnvName = regexp.MustCompile(`(?i)(secret|token|pass(word|wd)?|api[_-]?key|authorization|credential|private[_-]?key)`)

type verifyOutput struct {
	OK      bool                     `json:"ok"`
	State   string                   `json:"state,omitempty"`
	Receipt *job.VerificationReceipt `json:"receipt,omitempty"`
	Retry   []string                 `json:"retry,omitempty"`
	Blocked *verifyBlocked           `json:"blocked,omitempty"`
}

type verifyBlocked struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// verifyCmd is intentionally exact-job-only: verification is a bounded host
// handoff for one already-blocked worker turn, not a general command runner.
func verifyCmd() *cobra.Command {
	var timeout string
	var asJSON bool
	c := &cobra.Command{
		Use:   "verify <job> -- <argv...>",
		Short: "Run an explicit host-side verification for a blocked workspace job",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 {
				return fmt.Errorf("verification command must follow -- as argv")
			}
			d, err := time.ParseDuration(timeout)
			if err != nil || d <= 0 {
				return fmt.Errorf("--timeout must be a positive duration: %q", timeout)
			}
			s, wss, err := openWorkspaces()
			if err != nil {
				return err
			}
			m, wm, err := verifyPreconditions(s, wss, args[0])
			if err != nil {
				return verifyError(asJSON, args[0], err)
			}
			lease, err := acquireVerificationLease(s, m.ID, d)
			if err != nil {
				return verifyError(asJSON, m.ID, err)
			}

			r, startErr := runVerification(m.ID, wm.ID, wm.Tree, m.Turns, args[1:], d)
			if startErr != nil {
				clearVerificationLease(s, m.ID, lease.ID)
				appendVerificationStartRefusal(s, m.ID, wm.ID, m.Turns, args[1:], startErr)
				return verifyRefusal(asJSON, m.ID, "command-start", fmt.Sprintf("verification command could not start: %v", startErr))
			}

			// The command ran against the live worktree. Snapshot it immediately
			// afterwards so a receipt names one immutable tested tree.
			if freshWM, loadErr := wss.Load(wm.ID); loadErr == nil {
				wm = freshWM
				if snap, snapErr := wss.ReviewSnapshot(wm); snapErr == nil {
					r.CheckpointRef, r.CheckpointOID, r.DiffSHA256 = snap.CheckpointRef, snap.CheckpointOID, snap.DiffSHA256
				} else {
					r.HistoryError = appendReceiptHistoryError(r.HistoryError, "post-command checkpoint: "+snapErr.Error())
				}
			} else {
				r.HistoryError = appendReceiptHistoryError(r.HistoryError, "reload workspace: "+loadErr.Error())
			}

			promoted, finishErr := finishVerification(s, wss, r, lease)
			if finishErr != nil {
				return finishErr
			}
			if promoted {
				sendVerificationNotification(m, r)
			}
			out := verifyOutput{OK: promoted && r.Passed, State: "completed", Receipt: r, Retry: verifyRetry(m.ID, r.Argv)}
			if !promoted {
				out.State = "stale"
			}
			if asJSON {
				_ = printJSON(out)
			} else {
				printVerification(out)
			}
			if !out.OK {
				return commandError{code: 1, silent: true}
			}
			return nil
		},
	}
	c.Flags().StringVar(&timeout, "timeout", "30m", "wall-clock limit for the verification command")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func verifyPreconditions(s *job.Store, wss *workspace.Store, id string) (*job.Meta, *workspace.Meta, error) {
	m, err := s.LoadMeta(id)
	if err != nil {
		return nil, nil, err
	}
	s.Reconcile(m)
	if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "verify" {
		return nil, nil, fmt.Errorf("%s is %s, not blocked.kind=verify", m.ID, m.State)
	}
	if m.Workspace == "" {
		return nil, nil, fmt.Errorf("%s is not attached to a workspace; host verification requires a workspace job", m.ID)
	}
	wm, err := wss.Load(m.Workspace)
	if err != nil {
		return nil, nil, err
	}
	if wm.State != "open" {
		return nil, nil, fmt.Errorf("workspace %s is %s", wm.ID, wm.State)
	}
	if info, err := os.Stat(wm.Tree); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return nil, nil, fmt.Errorf("workspace %s worktree %s is unavailable: %v", wm.ID, wm.Tree, err)
	}
	if active, err := activeJobIn(s, wm.ID); err != nil {
		return nil, nil, err
	} else if active != "" && active != m.ID {
		return nil, nil, fmt.Errorf("workspace %s has active job %s; wait for the turn or cancel it before verifying", wm.ID, active)
	}
	return m, wm, nil
}

func acquireVerificationLease(s *job.Store, id string, timeout time.Duration) (*job.VerificationLease, error) {
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
	if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "verify" {
		return nil, fmt.Errorf("%s is %s, not blocked.kind=verify", m.ID, m.State)
	}
	now := time.Now().UTC()
	if m.VerificationLeaseLive(now) {
		return nil, fmt.Errorf("%s already has live host verification lease", m.ID)
	}
	// Expired leases are safe to clear while holding the job's metadata lock.
	m.VerificationLease = &job.VerificationLease{ID: fmt.Sprintf("verify:%d", now.UnixNano()), Turn: m.Turns,
		StartedAt: now, ExpiresAt: now.Add(timeout + 5*time.Second)}
	if err := s.SaveMeta(m); err != nil {
		return nil, err
	}
	return m.VerificationLease, nil
}

func clearVerificationLease(s *job.Store, id, leaseID string) {
	unlock, err := job.LockMeta(s.Root, id)
	if err != nil {
		return
	}
	defer unlock()
	m, err := s.LoadMeta(id)
	if err == nil && m.VerificationLease != nil && m.VerificationLease.ID == leaseID {
		m.VerificationLease = nil
		_ = s.SaveMeta(m)
	}
}

// finishVerification reloads metadata after exec and merges no worker fields.
// A legacy/uncooperative client may have resumed the job while the host command
// ran; retain history for audit, but never re-promote its old receipt.
func finishVerification(s *job.Store, wss *workspace.Store, receipt *job.VerificationReceipt, lease *job.VerificationLease) (bool, error) {
	unlock, err := job.LockMeta(s.Root, receipt.Job)
	if err != nil {
		return false, err
	}
	defer unlock()
	m, err := s.LoadMeta(receipt.Job)
	if err != nil {
		return false, err
	}
	if m.VerificationLease != nil && m.VerificationLease.ID == lease.ID {
		m.VerificationLease = nil
	}
	promoted := m.State == job.StateBlocked && m.Blocked != nil && m.Blocked.Kind == "verify" && m.Turns == receipt.Turn && receipt.CheckpointOID != ""
	if promoted {
		m.LatestVerification = receipt
	}
	if err := s.SaveMeta(m); err != nil {
		return false, fmt.Errorf("verification completed but could not save %s receipt: %w", m.ID, err)
	}
	if err := appendVerificationJobEvent(s, receipt); err != nil {
		receipt.HistoryError = appendReceiptHistoryError(receipt.HistoryError, err.Error())
		if promoted {
			_ = s.SaveMeta(m)
		}
	}
	if promoted {
		if err := wss.RecordVerification(receipt.Workspace, receipt); err != nil {
			receipt.HistoryError = appendReceiptHistoryError(receipt.HistoryError, "record workspace verification: "+err.Error())
			_ = s.SaveMeta(m)
		}
		// RecordVerification may have added a soft workspace-history warning
		// after the job rollup was first persisted.
		_ = s.SaveMeta(m)
	} else if err := appendVerificationWorkspaceHistory(wss, receipt); err != nil {
		receipt.HistoryError = appendReceiptHistoryError(receipt.HistoryError, err.Error())
	}
	return promoted, nil
}

func appendVerificationJobEvent(s *job.Store, receipt *job.VerificationReceipt) error {
	log, err := events.Open(filepath.Join(s.JobDir(receipt.Job), "events.jsonl"))
	if err != nil {
		return fmt.Errorf("open job event log: %w", err)
	}
	if _, err := log.Append(job.VerificationEvent(receipt)); err != nil {
		return fmt.Errorf("append job event log: %w", err)
	}
	return nil
}

func appendVerificationWorkspaceHistory(wss *workspace.Store, receipt *job.VerificationReceipt) error {
	log, err := events.Open(wss.EventsPath(receipt.Workspace))
	if err != nil {
		return fmt.Errorf("open workspace event log: %w", err)
	}
	_, err = log.Append(job.VerificationEvent(receipt))
	return err
}

func appendVerificationStartRefusal(s *job.Store, jobID, workspaceID string, turn int, argv []string, startErr error) {
	log, err := events.Open(filepath.Join(s.JobDir(jobID), "events.jsonl"))
	if err != nil {
		return
	}
	_, _ = log.Append(events.Event{Type: events.TypeVerificationRefused, Actor: "orchestrator", Preview: "verification command start refused",
		Fields: map[string]any{"kind": "command-start", "job": jobID, "workspace": workspaceID, "turn": turn,
			"argv": compactArgv(argv), "detail": events.Truncate(startErr.Error())}})
}

func compactArgv(argv []string) []string {
	return job.CompactVerificationReceipt(&job.VerificationReceipt{Argv: argv}).Argv
}

func verifyError(asJSON bool, id string, err error) error {
	kind := "precondition"
	if strings.Contains(err.Error(), "workspace") {
		kind = "missing-workspace"
	}
	if strings.Contains(err.Error(), "active job") || strings.Contains(err.Error(), "lease") {
		kind = "active-job"
	}
	if strings.Contains(err.Error(), "not blocked.kind=verify") {
		kind = "wrong-blocked-kind"
	}
	if strings.Contains(err.Error(), " is closed") {
		kind = "workspace-closed"
	}
	return verifyRefusal(asJSON, id, kind, err.Error())
}

func verifyRefusal(asJSON bool, jobID, kind, detail string) error {
	if asJSON {
		_ = printJSON(verifyOutput{OK: false, State: "blocked", Blocked: &verifyBlocked{Kind: kind, Detail: detail}})
	} else {
		fmt.Fprintln(os.Stderr, "legwork: "+detail)
	}
	return commandError{code: 2, silent: true}
}

func appendReceiptHistoryError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}

func runVerification(jobID, workspaceID, cwd string, turn int, argv []string, timeout time.Duration) (*job.VerificationReceipt, error) {
	started := time.Now().UTC()
	output := newRedactingOutput(verificationOutputLimit, secretValues())
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Keep a finite post-exit wait for any inherited descriptors. We still own
	// the process group and explicitly reap it below on timeout.
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	case <-timer.C:
		// The timer and Wait can race. Prefer a completed/reaped command over
		// signaling its process group, preserving its genuine exit result.
		select {
		case runErr = <-done:
		default:
			timedOut = true
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			runErr = <-done
		}
	}
	completed := time.Now().UTC()
	r := &job.VerificationReceipt{ReceiptID: "verification:" + jobID + ":" + fmt.Sprintf("%d", started.UnixNano()),
		Job: jobID, Workspace: workspaceID, Turn: turn, Argv: append([]string(nil), argv...), Cwd: cwd, Actor: "orchestrator",
		StartedAt: started, CompletedAt: completed, DurationMS: completed.Sub(started).Milliseconds(), TimedOut: timedOut}
	r.Output, r.OutputCut = output.Result()
	if runErr == nil && !timedOut {
		code := 0
		r.ExitCode, r.Passed = &code, true
		return r, nil
	}
	if !timedOut {
		if ee, ok := runErr.(*exec.ExitError); ok {
			code := ee.ExitCode()
			r.ExitCode = &code
		}
	}
	return r, nil
}

type redactingOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	rawLimit  int
	truncated bool
	secrets   []string
}

func secretValues() []string {
	var values []string
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if ok && secretEnvName.MatchString(name) && len(value) >= 4 {
			values = append(values, value)
		}
	}
	// Replace longer values first so one secret cannot leave a suffix of a
	// second, shorter configured value visible.
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if len(values[j]) > len(values[i]) {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
	return values
}

func newRedactingOutput(limit int, secrets []string) *redactingOutput {
	lookahead := 0
	for _, secret := range secrets {
		if len(secret) > lookahead {
			lookahead = len(secret)
		}
	}
	return &redactingOutput{limit: limit, rawLimit: limit + lookahead, secrets: secrets}
}

func (b *redactingOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	left := b.rawLimit - b.buf.Len()
	if left <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > left {
		_, _ = b.buf.Write(p[:left])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *redactingOutput) Result() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf.String()
	out = strings.ToValidUTF8(out, "�")
	for _, secret := range b.secrets {
		out = strings.ReplaceAll(out, secret, "[REDACTED]")
	}
	capped, cut := utf8Cap(out, b.limit)
	return capped, b.truncated || cut
}

func utf8Cap(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

func verifyRetry(id string, argv []string) []string {
	return append([]string{"legwork", "verify", id, "--"}, argv...)
}

func printVerification(out verifyOutput) {
	if out.Receipt == nil {
		return
	}
	state := "failed"
	if out.Receipt.Passed {
		state = "passed"
	} else if out.Receipt.TimedOut {
		state = "timed out"
	}
	if out.State == "stale" {
		state += " (stale; not current)"
	}
	fmt.Printf("verification %s: %s (%dms)\n", state, out.Receipt.ReceiptID, out.Receipt.DurationMS)
	if out.Receipt.Output != "" {
		fmt.Print(out.Receipt.Output)
		if !strings.HasSuffix(out.Receipt.Output, "\n") {
			fmt.Println()
		}
	}
	if out.Receipt.OutputCut {
		fmt.Printf("output truncated at %d bytes\n", verificationOutputLimit)
	}
	fmt.Printf("retry: %s\n", shellCommand(out.Retry))
	if out.Receipt.HistoryError != "" {
		fmt.Printf("history warning: %s\n", out.Receipt.HistoryError)
	}
}

func shellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" {
			quoted[i] = "''"
		} else if strings.ContainsAny(arg, " \t\n'\"\\$&;|<>()*?[]{}!") {
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}

func sendVerificationNotification(m *job.Meta, receipt *job.VerificationReceipt) {
	cfg, err := notify.Load()
	if err != nil {
		return
	}
	event := "verification-failed"
	if receipt.Passed {
		event = "verification-passed"
	}
	_ = cfg.Send(notify.Payload{Event: event, Job: m.ID, Run: m.Run, Agent: m.Agent, Task: m.Task,
		Blocked: m.Blocked, Result: events.Truncate(m.Result), CostUSD: m.CostUSD, Context: m.Context,
		Verification: job.CompactVerificationReceipt(receipt)})
}
