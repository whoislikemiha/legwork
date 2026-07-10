package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/job"
)

func TestWaitLeavesLiveStateAndReturnsFinalMetadata(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, "#sleep 450", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "slow wait"))

	start := time.Now()
	out := e.legwork(t, "wait", id, "--json")
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("wait returned before the active job settled: %v\n%s", elapsed, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("wait --json: %v\n%s", err, out)
	}
	if got["outcome"] != "reached" || got["reached"] != "done" {
		t.Fatalf("unexpected wait outcome: %+v", got)
	}
	meta, ok := got["job"].(map[string]any)
	if !ok || meta["id"] != id || meta["state"] != "done" || meta["result"] != "finished" {
		t.Fatalf("wait did not return final persisted metadata: %+v", got)
	}
	if got["elapsed_ms"].(float64) <= 0 {
		t.Fatalf("wait should report elapsed time: %+v", got)
	}
	if waitedFor, ok := got["waited_for"].([]any); !ok || len(waitedFor) != 0 {
		t.Fatalf("default wait should not claim requested states: %+v", got)
	}
}

func TestWaitRequestedStateMismatchAndTimeoutHaveStableOutcomes(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "already done"))
	e.waitState(t, id, "done")

	out, err := e.legworkErr("wait", id, "--until", "needs-input", "--json")
	if exitCode(err) != 1 {
		t.Fatalf("terminal mismatch exit = %v, want 1\n%s", err, out)
	}
	var mismatch map[string]any
	if err := json.Unmarshal([]byte(out), &mismatch); err != nil {
		t.Fatalf("mismatch JSON: %v\n%s", err, out)
	}
	if mismatch["outcome"] != "terminal-mismatch" || mismatch["reached"] != "done" {
		t.Fatalf("bad mismatch outcome: %+v", mismatch)
	}

	e.writeScript(t, "#sleep 700", resultDone)
	id = strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "timeout"))
	out, err = e.waitWithDeadline(t, 2*time.Second, "wait", id, "--until", "done", "--timeout", "75ms", "--json")
	if err == context.DeadlineExceeded {
		t.Fatalf("explicit-state wait exceeded its test deadline: %v\n%s", err, out)
	}
	if exitCode(err) != 1 {
		t.Fatalf("timeout exit = %v, want 1\n%s", err, out)
	}
	var timedOut map[string]any
	if err := json.Unmarshal([]byte(out), &timedOut); err != nil {
		t.Fatalf("timeout JSON: %v\n%s", err, out)
	}
	if timedOut["outcome"] != "timeout" {
		t.Fatalf("bad timeout outcome: %+v", timedOut)
	}
}

func TestWaitDefaultFormHonorsTimeout(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, "#sleep 30000", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "default timeout"))
	t.Cleanup(func() {
		if out, err := e.legworkErr("cancel", id); err == nil {
			e.waitState(t, id, "interrupted")
		} else if !strings.Contains(out, "is not active") {
			t.Errorf("cleanup cancel: %v\n%s", err, out)
		}
	})

	out, err := e.waitWithDeadline(t, 2*time.Second, "wait", id, "--timeout", "75ms", "--json")
	if err == context.DeadlineExceeded {
		t.Fatalf("default-form wait exceeded its test deadline: %v\n%s", err, out)
	}
	if exitCode(err) != 1 {
		t.Fatalf("default timeout exit = %v, want 1\n%s", err, out)
	}
	var timedOut map[string]any
	if err := json.Unmarshal([]byte(out), &timedOut); err != nil {
		t.Fatalf("default timeout JSON: %v\n%s", err, out)
	}
	if timedOut["outcome"] != "timeout" {
		t.Fatalf("bad default timeout outcome: %+v", timedOut)
	}
	if waitedFor, ok := timedOut["waited_for"].([]any); !ok || len(waitedFor) != 0 {
		t.Fatalf("default timeout should return empty waited_for: %+v", timedOut)
	}
}

func TestWaitFollowsAnswerResumeAndCancellation(t *testing.T) {
	e := newEnv(t)
	needsInput := `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-wait","result":"state: needs-input\nquestion: continue?"}`
	e.writeScript(t, needsInput)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "answer then resume"))
	if out := e.legwork(t, "wait", id, "--until", "needs-input", "--json"); !strings.Contains(out, `"reached": "needs-input"`) {
		t.Fatalf("wait did not observe the initial question:\n%s", out)
	}
	e.writeScript(t, resultDone)
	e.legwork(t, "answer", id, "yes")
	if out := e.legwork(t, "wait", id, "--until", "done", "--json"); !strings.Contains(out, `"reached": "done"`) {
		t.Fatalf("wait did not observe answer completion:\n%s", out)
	}
	e.writeScript(t, needsInput)
	e.legwork(t, "resume", id, "ask again")
	if out := e.legwork(t, "wait", id, "--until", "needs-input", "--json"); !strings.Contains(out, `"reached": "needs-input"`) {
		t.Fatalf("wait did not observe resumed turn:\n%s", out)
	}

	e.writeScript(t, "#sleep 30000", resultDone)
	id = strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "cancelled wait"))
	e.waitState(t, id, "active")
	done := make(chan result, 1)
	go func() {
		out, err := e.legworkErr("wait", id, "--json")
		done <- result{out, err}
	}()
	e.legwork(t, "cancel", id)
	got := <-done
	if got.err != nil || !strings.Contains(got.out, `"reached": "interrupted"`) {
		t.Fatalf("wait did not observe cancellation: err=%v\n%s", got.err, got.out)
	}
}

func TestWaitRejectsInvalidOrUnknownExactJob(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"wait", "job-999", "--json"},
		{"wait", "job-999", "--until", "not-a-state"},
		{"wait", "job-999", "--until", ""},
		{"wait", "job-999", "--timeout", "0s"},
		{"wait", "job-999", "--timeout", ""},
	} {
		out, err := e.legworkErr(args...)
		if exitCode(err) != 2 {
			t.Fatalf("%v exit = %v, want 2\n%s", args, err, out)
		}
		if len(args) == 4 && args[2] == "--until" && args[3] == "" && !strings.Contains(out, "--until requires at least one state") {
			t.Fatalf("explicit empty --until should explain the usage error:\n%s", out)
		}
	}
}

func TestWaitReconcilesDeadRunnerAndMultipleWaitersDoNotClobber(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	writeJobMeta(t, e, &job.Meta{
		ID: "job-9", Agent: "fake", Task: "dead runner", State: job.StateActive,
		RunnerPID: 999999, Created: now, Updated: now,
	})
	out := e.legwork(t, "wait", "job-9", "--until", "interrupted", "--json")
	var reconciled map[string]any
	if err := json.Unmarshal([]byte(out), &reconciled); err != nil {
		t.Fatalf("reconciled JSON: %v\n%s", err, out)
	}
	if reconciled["reached"] != "interrupted" || reconciled["job"].(map[string]any)["state"] != "interrupted" {
		t.Fatalf("dead runner was not reconciled: %+v", reconciled)
	}

	e.writeScript(t, "#sleep 350", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "two waiters"))
	results := make(chan result, 2)
	for range 2 {
		go func() {
			out, err := e.legworkErr("wait", id, "--until", "done", "--json")
			results <- result{out, err}
		}()
	}
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatalf("concurrent wait failed: %v\n%s", r.err, r.out)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(r.out), &got); err != nil || got["outcome"] != "reached" || got["reached"] != "done" {
			t.Fatalf("concurrent wait got %v, err=%v\n%s", got, err, r.out)
		}
	}
	final := e.waitState(t, id, "done")
	if final["result"] != "finished" {
		t.Fatalf("concurrent waiters clobbered terminal metadata: %+v", final)
	}
}

type result struct {
	out string
	err error
}

// waitWithDeadline keeps a regression in wait's polling loop from consuming the
// whole e2e suite: the subprocess is killed if its own timeout is ignored.
func (e *env) waitWithDeadline(t *testing.T, limit time.Duration, args ...string) (string, error) {
	t.Helper()
	deadline := time.Now().Add(limit)
	if d, ok := t.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = append(os.Environ(), "LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
	return string(out), err
}
