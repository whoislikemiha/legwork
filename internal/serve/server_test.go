package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

func TestValidateAddrRejectsRemoteByDefault(t *testing.T) {
	ok := []string{"127.0.0.1:0", "localhost:0", "[::1]:0"}
	for _, addr := range ok {
		if err := ValidateAddr(addr, false); err != nil {
			t.Fatalf("%s should be accepted: %v", addr, err)
		}
	}

	rejected := []string{":0", "0.0.0.0:8080", "[::]:8080", "192.168.1.10:8080", "example.com:8080"}
	for _, addr := range rejected {
		if err := ValidateAddr(addr, false); err == nil {
			t.Fatalf("%s should be rejected without --allow-remote", addr)
		}
		if err := ValidateAddr(addr, true); err != nil {
			t.Fatalf("%s should be accepted with --allow-remote: %v", addr, err)
		}
	}
}

func TestReadOnlyMethodsRejected(t *testing.T) {
	h := NewHandler(&job.Store{Root: tempState(t)}, 150000)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/snapshot", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s returned %d, want 405", method, rec.Code)
		}
	}
}

func TestIndexEndpointServesDashboardShell(t *testing.T) {
	h := NewHandler(&job.Store{Root: tempState(t)}, 150000)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / returned %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"legwork serve", "/api/snapshot", "/events", "read-only"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard shell missing %q", want)
		}
	}
}

func TestSnapshotEndpointReturnsLiveState(t *testing.T) {
	s := &job.Store{Root: tempState(t)}
	id1 := createJob(t, s, &job.Meta{
		ID:    "job-1",
		Run:   "pipe",
		Agent: "fake",
		Task:  "interrupted job",
		State: job.StateInterrupted,
	})
	id2 := createJob(t, s, &job.Meta{
		ID:       "job-2",
		Run:      "pipe",
		Agent:    "fake",
		Task:     "choose storage",
		State:    job.StateNeedsInput,
		Question: "postgres or sqlite?",
		Context:  200000,
	})
	appendJobEvent(t, s, id1, events.Event{Type: events.TypeStarted, Actor: "runner", Preview: "started stale job"})
	appendJobEvent(t, s, id2, events.Event{Type: events.TypeNeedsInput, Actor: "main", Preview: "postgres or sqlite?"})
	runPath, err := s.RunEventsPath("pipe")
	if err != nil {
		t.Fatal(err)
	}
	appendEvent(t, runPath, events.Event{Type: events.TypeNote, Actor: "orchestrator", Preview: "split into two jobs"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot?job=job-2", nil)
	NewHandler(s, 150000).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot status %d: %s", rec.Code, rec.Body.String())
	}

	var snap Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if !snap.ReadOnly || !snap.LocalOnly {
		t.Fatalf("snapshot should advertise read-only local mode: %+v", snap)
	}
	if len(snap.Runs) != 1 || snap.Runs[0].Label != "pipe" {
		t.Fatalf("run rollup missing pipe: %+v", snap.Runs)
	}
	if got := stateOf(snap.Jobs, "job-1"); got != string(job.StateInterrupted) {
		t.Fatalf("job-1 state = %q", got)
	}
	if got := stateOf(snap.Jobs, "job-2"); got != string(job.StateNeedsInput) {
		t.Fatalf("job-2 state = %q", got)
	}
	if len(snap.Attention) < 2 {
		t.Fatalf("expected interrupted + needs-input attention items: %+v", snap.Attention)
	}
	if snap.Selected == nil || snap.Selected.ID != "job-2" || len(snap.Selected.Events) == 0 {
		t.Fatalf("selected job detail missing: %+v", snap.Selected)
	}
	if !containsTimeline(snap.Timeline, "split into two jobs") {
		t.Fatalf("timeline missing run note: %+v", snap.Timeline)
	}
}

func TestSnapshotEndpointDoesNotReconcileStaleActiveJob(t *testing.T) {
	s := &job.Store{Root: tempState(t)}
	id := createJob(t, s, &job.Meta{
		ID:        "job-1",
		Run:       "pipe",
		Agent:     "fake",
		Task:      "stale active job",
		State:     job.StateActive,
		RunnerPID: 0,
	})
	appendJobEvent(t, s, id, events.Event{Type: events.TypeStarted, Actor: "runner", Preview: "started stale job"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	NewHandler(s, 150000).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot status %d: %s", rec.Code, rec.Body.String())
	}

	m, err := s.LoadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != job.StateActive {
		t.Fatalf("snapshot must not mutate stale active job; state = %s", m.State)
	}
	evs, err := events.Read(filepath.Join(s.JobDir(id), "events.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == events.TypeInterrupted {
			t.Fatalf("snapshot must not append interrupted event: %+v", ev)
		}
	}
}

func TestEventsEndpointEmitsInitialSnapshotEvent(t *testing.T) {
	s := &job.Store{Root: tempState(t)}
	createJob(t, s, &job.Meta{ID: "job-1", Agent: "fake", Task: "hello", State: job.StateDone})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()
	NewHandler(s, 150000).ServeHTTP(rec, req)

	sc := bufio.NewScanner(rec.Body)
	var lines []string
	deadline := time.After(2 * time.Second)
	for len(lines) < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for SSE lines; got %v", lines)
		default:
		}
		if !sc.Scan() {
			t.Fatalf("SSE stream ended early: %v", sc.Err())
		}
		lines = append(lines, sc.Text())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "event: snapshot") || !strings.Contains(joined, "data: ") {
		t.Fatalf("initial SSE event not emitted: %q", joined)
	}
}

func tempState(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func createJob(t *testing.T, s *job.Store, m *job.Meta) string {
	t.Helper()
	if err := os.MkdirAll(s.JobDir(m.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(m); err != nil {
		t.Fatal(err)
	}
	return m.ID
}

func appendJobEvent(t *testing.T, s *job.Store, id string, e events.Event) {
	t.Helper()
	appendEvent(t, filepath.Join(s.JobDir(id), "events.jsonl"), e)
}

func appendEvent(t *testing.T, path string, e events.Event) {
	t.Helper()
	l, err := events.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(e); err != nil {
		t.Fatal(err)
	}
}

func stateOf(js []JobSummary, id string) string {
	for _, j := range js {
		if j.ID == id {
			return j.State
		}
	}
	return ""
}

func containsTimeline(items []TimelineItem, preview string) bool {
	for _, it := range items {
		if it.Preview == preview {
			return true
		}
	}
	return false
}
