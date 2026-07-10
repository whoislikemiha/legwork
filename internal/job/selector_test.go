package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
)

func TestResolvePrefersExactJobAndAllowsForcedRun(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	for _, m := range []*Meta{
		{ID: "job-1", Run: "job-1", Agent: "fake", Created: time.Unix(1, 0)},
		{ID: "job-2", Run: "job-1", Agent: "fake", Created: time.Unix(2, 0)},
	} {
		if err := s.Create(m); err != nil {
			t.Fatal(err)
		}
	}

	auto, err := Resolve(s, "job-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if auto.Kind != SelectorJob || auto.Newest().ID != "job-1" {
		t.Fatalf("automatic resolution = %+v, want exact job-1", auto)
	}

	run, err := Resolve(s, "job-1", SelectorRun)
	if err != nil {
		t.Fatal(err)
	}
	if run.Kind != SelectorRun || len(run.Jobs) != 2 || run.Newest().ID != "job-2" {
		t.Fatalf("forced run resolution = %+v, want job-2", run)
	}
}

func TestLoadMetaBackfillsLegacyClosedOutcome(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	m := &Meta{ID: "job-1", Agent: "fake", State: StateClosed, Turns: 2}
	if err := s.Create(m); err != nil {
		t.Fatal(err)
	}
	log, err := events.Open(filepath.Join(s.JobDir(m.ID), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(events.Event{Type: events.TypeNeedsInput, Preview: "postgres or sqlite?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(events.Event{Type: events.TypeFinished, Preview: "", Fields: map[string]any{"state": "needs-input"}}); err != nil {
		t.Fatal(err)
	}
	// A newer malformed event must not invent the non-existent state
	// "finished" or hide the preceding valid turn outcome.
	if _, err := log.Append(events.Event{Type: events.TypeFinished, Preview: "bad legacy event"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.JobDir(m.ID), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadMeta(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastOutcome == nil || got.LastOutcome.State != StateNeedsInput || got.LastOutcome.Question != "postgres or sqlite?" || got.LastOutcome.Reason != "postgres or sqlite?" || got.LastOutcome.Turn != 2 {
		t.Fatalf("legacy outcome was not backfilled: %+v", got.LastOutcome)
	}
	after, err := os.ReadFile(filepath.Join(s.JobDir(m.ID), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("legacy read rewrote metadata:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestSaveTerminalMetaKeepsFullResultAndCompactOutcome(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	m := &Meta{ID: "job-1", Agent: "fake", State: StateDone, Turns: 3,
		Result: strings.Repeat("x", 300)}
	if err := s.Create(m); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTerminalMeta(m); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMeta(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastOutcome == nil {
		t.Fatal("terminal outcome missing")
	}
	if got.Result != m.Result || len(got.LastOutcome.Reason) >= len(got.Result) {
		t.Fatalf("result/detail compactness lost: %+v", got)
	}
	if !got.LastOutcome.At.Equal(got.Updated) {
		t.Fatalf("outcome time %s != terminal updated %s", got.LastOutcome.At, got.Updated)
	}
}

func TestResolveRejectsEmptyRun(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Root, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(s, "empty", SelectorRun); err == nil || err.Error() != `no job or run event log "empty"; use --job <id> for an exact job` {
		t.Fatalf("empty run error = %v", err)
	}
}

func TestResolveAcceptsLogOnlyRun(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Root, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := s.RunEventsPath("notes")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	sel, err := Resolve(s, "notes", SelectorRun)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Kind != SelectorRun || len(sel.Jobs) != 0 || sel.Newest() != nil {
		t.Fatalf("log-only run resolution = %+v", sel)
	}
}

func TestResolvePreservesCorruptJobMetaError(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	m := &Meta{ID: "job-1", Agent: "fake"}
	if err := s.Create(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.JobDir(m.ID), "meta.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := s.RunEventsPath(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve(s, m.ID, ""); err == nil {
		t.Fatal("automatic resolution fell through from corrupt job metadata to a run")
	}
}

func TestReconcileDoesNotOverwriteNewerTerminalMeta(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	if err := s.Create(&Meta{ID: "job-1", Agent: "fake", State: StateActive, RunnerPID: 999999}); err != nil {
		t.Fatal(err)
	}
	stale, err := s.LoadMeta("job-1")
	if err != nil {
		t.Fatal(err)
	}
	terminal := *stale
	terminal.State = StateDone
	terminal.RunnerPID = 0
	terminal.Result = "finished"
	if err := s.SaveMeta(&terminal); err != nil {
		t.Fatal(err)
	}

	if s.Reconcile(stale) {
		t.Fatal("reconcile changed a newer terminal record")
	}
	if stale.State != StateDone || stale.Result != "finished" {
		t.Fatalf("stale pointer was not refreshed: %+v", stale)
	}
	persisted, err := s.LoadMeta("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != StateDone || persisted.Result != "finished" {
		t.Fatalf("terminal meta was overwritten: %+v", persisted)
	}
}

func TestReconcileRefreshesOutcomeAfterPreviousTerminalTurn(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	previousAt := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	m := &Meta{ID: "job-1", Agent: "fake", State: StateActive, RunnerPID: 999999,
		Result: "old answer", Turns: 1, LastOutcome: &Outcome{
			State: StateNeedsInput, Reason: "postgres or sqlite?", Question: "postgres or sqlite?", At: previousAt,
		}}
	if err := s.Create(m); err != nil {
		t.Fatal(err)
	}
	stale, err := s.LoadMeta(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Reconcile(stale) {
		t.Fatal("expected dead active runner to reconcile")
	}
	if stale.State != StateInterrupted || stale.Result != "runner died without finishing the turn" {
		t.Fatalf("interrupted terminal state was not persisted: %+v", stale)
	}
	if stale.LastOutcome == nil || stale.LastOutcome.State != StateInterrupted || stale.LastOutcome.Reason != stale.Result {
		t.Fatalf("stale prior-turn outcome was not refreshed: %+v", stale.LastOutcome)
	}
	if !stale.LastOutcome.At.Equal(stale.Updated) || stale.LastOutcome.At.Equal(previousAt) {
		t.Fatalf("interrupted outcome time is stale: outcome=%s updated=%s", stale.LastOutcome.At, stale.Updated)
	}
}
