package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Parallel dispatch is a core use case: concurrent runs must never collide
// on job IDs (found by external review: scan-max+1 raced).
func TestParallelRunsGetUniqueIDs(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)

	const n = 8
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := e.legworkErr("run", "--agent", "fake", "parallel job")
			if err != nil {
				t.Errorf("run: %v\n%s", err, out)
				return
			}
			ids <- strings.TrimSpace(out)
		}()
	}
	wg.Wait()
	close(ids)

	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate job id allocated: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d unique ids, want %d", len(seen), n)
	}
	// Let the detached runners finish before the tempdir is torn down, else
	// cleanup races a live runner still writing into a job dir.
	for id := range seen {
		e.waitState(t, id, "done")
	}
}

// A hung turn must not hold a job open past --timeout; the session survives
// as interrupted.
func TestTimeoutInterruptsTurn(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"stalling"}]}}`,
		"#sleep 30000",
		resultDone,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--timeout", "1s", "hung task"))
	m := e.waitState(t, id, "interrupted")
	if !strings.Contains(m["result"].(string), "timeout") {
		t.Fatalf("result should name the timeout: %v", m["result"])
	}
}

func TestTimeoutRejectsBadDuration(t *testing.T) {
	e := newEnv(t)
	if out, err := e.legworkErr("run", "--agent", "fake", "--timeout", "banana", "x"); err == nil {
		t.Fatalf("bad --timeout accepted:\n%s", out)
	}
}

func TestRunRejectsBadRunLabelBeforeAllocatingJob(t *testing.T) {
	e := newEnv(t)
	if out, err := e.legworkErr("run", "--agent", "fake", "--run", "../pipe", "x"); err == nil {
		t.Fatalf("bad --run accepted:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(e.state, "jobs", "job-1")); !os.IsNotExist(err) {
		t.Fatalf("invalid --run allocated job-1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.state, "counters.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid --run advanced counters: %v", err)
	}

	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "valid job"))
	if id != "job-1" {
		t.Fatalf("first valid job id = %q, want job-1", id)
	}
	e.waitState(t, id, "done")
}

// Dispatch options live in meta.json, not env: a resumed turn must run with
// the same --timeout (and --read-only / --append-prompt, which travel the
// same path) as the dispatch turn, and the dispatch prompt must stay
// recoverable after resume overwrites task.
func TestResumePreservesDispatchOptions(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake",
		"--timeout", "1s", "--read-only", "--append-prompt", "house rules", "the original task"))
	e.waitState(t, id, "done")

	// Second turn hangs: only the persisted timeout can interrupt it.
	e.writeScript(t,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"stalling"}]}}`,
		"#sleep 30000",
		resultDone,
	)
	e.legwork(t, "resume", id, "follow-up instruction")
	m := e.waitState(t, id, "interrupted")
	if !strings.Contains(m["result"].(string), "timeout") {
		t.Fatalf("resumed turn ignored persisted --timeout: %v", m["result"])
	}
	if m["read_only"] != true {
		t.Fatalf("read_only not persisted in meta: %v", m["read_only"])
	}
	if m["append_prompt"] != "house rules" {
		t.Fatalf("append_prompt not persisted in meta: %v", m["append_prompt"])
	}
	if m["initial_task"] != "the original task" {
		t.Fatalf("initial_task should preserve the dispatch prompt: %v", m["initial_task"])
	}
	if m["task"] != "follow-up instruction" {
		t.Fatalf("task should be the latest instruction: %v", m["task"])
	}
}

// The claude-specific passthroughs (--effort, --fallback-model) persist in
// meta.json and, like the other dispatch options, survive a resume so every
// turn runs under the same contract.
func TestEffortAndFallbackPersistThroughResume(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake",
		"--effort", "low", "--fallback-model", "sonnet", "the original task"))
	e.waitState(t, id, "done")

	e.writeScript(t, resultDone)
	e.legwork(t, "resume", id, "follow-up instruction")
	m := e.waitState(t, id, "done")
	if m["effort"] != "low" {
		t.Fatalf("effort not persisted through resume: %v", m["effort"])
	}
	if m["fallback_model"] != "sonnet" {
		t.Fatalf("fallback_model not persisted through resume: %v", m["fallback_model"])
	}
}

// --effort is validated against claude's accepted set at dispatch.
func TestEffortRejectsBadLevel(t *testing.T) {
	e := newEnv(t)
	if out, err := e.legworkErr("run", "--agent", "fake", "--effort", "turbo", "x"); err == nil {
		t.Fatalf("bad --effort accepted:\n%s", out)
	}
}

// --effort reaches codex (mapped onto its reasoning scale); --fallback-model
// stays claude-specific and is rejected for codex rather than silently dropped.
func TestCodexPassthroughs(t *testing.T) {
	e := newEnv(t)
	if out, err := e.legworkErr("run", "--agent", "codex", "--effort", "max", "x"); err != nil {
		t.Fatalf("codex rejected --effort:\n%s", out)
	}
	if out, err := e.legworkErr("run", "--agent", "codex", "--fallback-model", "sonnet", "x"); err == nil {
		t.Fatalf("codex accepted --fallback-model:\n%s", out)
	}
}
