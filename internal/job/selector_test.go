package job

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
