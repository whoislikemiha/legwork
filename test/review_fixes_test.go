package e2e

import (
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
