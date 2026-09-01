// Package verify implements host-side verification for blocked workspace
// jobs: precondition checks, the persisted lease that serializes a
// verification with resume and other workspace activity, the bounded
// subprocess run with secret redaction, and receipt persistence into job
// metadata and event history.
package verify

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/notify"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

// OutputLimit bounds the receipt's captured command output.
const OutputLimit = 64 * 1024

// Refusal kinds, part of the CLI's blocked-output surface.
const (
	KindPrecondition     = "precondition"
	KindMissingWorkspace = "missing-workspace"
	KindActiveJob        = "active-job"
	KindWrongBlockedKind = "wrong-blocked-kind"
	KindWorkspaceClosed  = "workspace-closed"
	KindCommandStart     = "command-start"
)

// Error is a refusal tagged with a machine-readable kind.
type Error struct {
	Kind string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func refusal(kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
}

// Preconditions is intentionally exact-job-only: verification is a bounded
// host handoff for one already-blocked worker turn, not a general command
// runner.
func Preconditions(s *job.Store, wss *workspace.Store, id string) (*job.Meta, *workspace.Meta, error) {
	m, err := s.LoadMeta(id)
	if err != nil {
		return nil, nil, &Error{Kind: KindPrecondition, Err: err}
	}
	s.Reconcile(m)
	if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "verify" {
		return nil, nil, refusal(KindWrongBlockedKind, "%s is %s, not blocked.kind=verify", m.ID, m.State)
	}
	if m.Workspace == "" {
		return nil, nil, refusal(KindMissingWorkspace, "%s is not attached to a workspace; host verification requires a workspace job", m.ID)
	}
	wm, err := wss.Load(m.Workspace)
	if err != nil {
		return nil, nil, &Error{Kind: KindMissingWorkspace, Err: err}
	}
	if wm.State != "open" {
		kind := KindMissingWorkspace
		if wm.State == "closed" {
			kind = KindWorkspaceClosed
		}
		return nil, nil, refusal(kind, "workspace %s is %s", wm.ID, wm.State)
	}
	if info, err := os.Stat(wm.Tree); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("not a directory")
		}
		return nil, nil, refusal(KindMissingWorkspace, "workspace %s worktree %s is unavailable: %v", wm.ID, wm.Tree, err)
	}
	if active, err := s.ActiveJobIn(wm.ID); err != nil {
		return nil, nil, &Error{Kind: KindPrecondition, Err: err}
	} else if active != "" && active != m.ID {
		return nil, nil, refusal(KindActiveJob, "workspace %s has active job %s; wait for the turn or cancel it before verifying", wm.ID, active)
	}
	return m, wm, nil
}

func AcquireLease(s *job.Store, id string, timeout time.Duration) (*job.VerificationLease, error) {
	unlock, err := job.LockMeta(s.Root, id)
	if err != nil {
		return nil, &Error{Kind: KindPrecondition, Err: err}
	}
	defer unlock()
	m, err := s.LoadMeta(id)
	if err != nil {
		return nil, &Error{Kind: KindPrecondition, Err: err}
	}
	s.Reconcile(m)
	if m.State != job.StateBlocked || m.Blocked == nil || m.Blocked.Kind != "verify" {
		return nil, refusal(KindWrongBlockedKind, "%s is %s, not blocked.kind=verify", m.ID, m.State)
	}
	now := time.Now().UTC()
	if m.VerificationLeaseLive(now) {
		return nil, refusal(KindActiveJob, "%s already has live host verification lease", m.ID)
	}
	// Expired leases are safe to clear while holding the job's metadata lock.
	m.VerificationLease = &job.VerificationLease{ID: fmt.Sprintf("verify:%d", now.UnixNano()), Turn: m.Turns,
		StartedAt: now, ExpiresAt: now.Add(timeout + 5*time.Second)}
	if err := s.SaveMeta(m); err != nil {
		return nil, &Error{Kind: KindPrecondition, Err: err}
	}
	return m.VerificationLease, nil
}

func ClearLease(s *job.Store, id, leaseID string) {
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

// Finish reloads metadata after exec and merges no worker fields. A
// legacy/uncooperative client may have resumed the job while the host command
// ran; retain history for audit, but never re-promote its old receipt.
func Finish(s *job.Store, wss *workspace.Store, receipt *job.VerificationReceipt, lease *job.VerificationLease) (bool, error) {
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
	if err := appendJobEvent(s, receipt); err != nil {
		receipt.HistoryError = AppendHistoryError(receipt.HistoryError, err.Error())
		if promoted {
			_ = s.SaveMeta(m)
		}
	}
	if promoted {
		if err := wss.RecordVerification(receipt.Workspace, receipt); err != nil {
			receipt.HistoryError = AppendHistoryError(receipt.HistoryError, "record workspace verification: "+err.Error())
			_ = s.SaveMeta(m)
		}
		// RecordVerification may have added a soft workspace-history warning
		// after the job rollup was first persisted.
		_ = s.SaveMeta(m)
	} else if err := appendWorkspaceHistory(wss, receipt); err != nil {
		receipt.HistoryError = AppendHistoryError(receipt.HistoryError, err.Error())
	}
	return promoted, nil
}

func appendJobEvent(s *job.Store, receipt *job.VerificationReceipt) error {
	log, err := events.Open(s.EventsPath(receipt.Job))
	if err != nil {
		return fmt.Errorf("open job event log: %w", err)
	}
	if _, err := log.Append(job.VerificationEvent(receipt)); err != nil {
		return fmt.Errorf("append job event log: %w", err)
	}
	return nil
}

func appendWorkspaceHistory(wss *workspace.Store, receipt *job.VerificationReceipt) error {
	log, err := events.Open(wss.EventsPath(receipt.Workspace))
	if err != nil {
		return fmt.Errorf("open workspace event log: %w", err)
	}
	_, err = log.Append(job.VerificationEvent(receipt))
	return err
}

func AppendStartRefusal(s *job.Store, jobID, workspaceID string, turn int, argv []string, startErr error) {
	log, err := events.Open(s.EventsPath(jobID))
	if err != nil {
		return
	}
	_, _ = log.Append(events.Event{Type: events.TypeVerificationRefused, Actor: "orchestrator", Preview: "verification command start refused",
		Fields: map[string]any{"kind": KindCommandStart, "job": jobID, "workspace": workspaceID, "turn": turn,
			"argv": compactArgv(argv), "detail": events.Truncate(startErr.Error())}})
}

func compactArgv(argv []string) []string {
	return job.CompactVerificationReceipt(&job.VerificationReceipt{Argv: argv}).Argv
}

// AppendHistoryError accumulates soft receipt-history warnings.
func AppendHistoryError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}

// Run executes the verification command in the worktree and returns a receipt.
// A non-nil error means the command never started (no receipt exists).
func Run(jobID, workspaceID, cwd string, turn int, argv []string, timeout time.Duration) (*job.VerificationReceipt, error) {
	started := time.Now().UTC()
	output := newRedactingOutput(OutputLimit, secretValues())
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

// RetryArgv is the copy-pasteable retry command surfaced in status/ls output.
func RetryArgv(id string, argv []string) []string {
	return append([]string{"legwork", "verify", id, "--"}, argv...)
}

func SendNotification(s *job.Store, m *job.Meta, receipt *job.VerificationReceipt) {
	cfg, err := notify.Load()
	if err != nil {
		return
	}
	event := "verification-failed"
	if receipt.Passed {
		event = "verification-passed"
	}
	_ = cfg.SendForJob(s.JobDir(m.ID), notify.Payload{Event: event, Job: m.ID, Run: m.Run, Agent: m.Agent, Task: m.Task,
		Blocked: m.Blocked, Result: events.Truncate(m.Result), CostUSD: m.CostUSD, Context: m.Context,
		Verification: job.CompactVerificationReceipt(receipt)})
}
