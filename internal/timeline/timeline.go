// Package timeline is the read-only plumbing shared by every presentation
// surface (runs, tail, dashboard, and — by design — a future serve). It is a
// renderer's substrate over the same JSONL the rest of legwork writes: it
// discovers event sources, merges them into one time-ordered stream with a
// cursor for incremental follow, classifies events by significance, and rolls
// per-run state up for the overview. It creates no new state and mutates
// nothing — readers only.
package timeline

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

// Item is one merged event with its provenance. Exactly one of the two source
// kinds is authoritative: a job-log event has JobID set (Run mirrors the job's
// meta.Run, so run scoping/filtering works on the merged stream); a run-log
// event has JobID empty and Run set to the run label.
type Item struct {
	JobID string       `json:"job,omitempty"`
	Run   string       `json:"run,omitempty"`
	Event events.Event `json:"event"`
}

// IsRun reports whether the item came from a run's event log rather than a
// job's.
func (it Item) IsRun() bool { return it.JobID == "" }

// Badge is the short source label a stream renderer prints: the job ID for job
// events, or "[run-label]" for run-log events.
func (it Item) Badge() string {
	if it.JobID != "" {
		return it.JobID
	}
	return "[" + it.Run + "]"
}

// Source is one events.jsonl to merge, tagged with its provenance.
type Source struct {
	Path  string
	JobID string // set for a job source
	Run   string // run label (run source) or the job's meta.Run (job source)
}

// Timeline merges a scope's sources into a single time-ordered stream and
// remembers a per-file cursor so Poll returns only what is new. The scope is a
// thunk re-evaluated on every Poll, so a live follower automatically picks up
// jobs (and run logs) that appear after it started — new sources begin at seq 0
// and backfill naturally.
type Timeline struct {
	scope   func() ([]Source, error)
	cursors map[string]int // path -> highest seq consumed
}

// New builds a Timeline over a dynamic scope.
func New(scope func() ([]Source, error)) *Timeline {
	return &Timeline{scope: scope, cursors: map[string]int{}}
}

// Static builds a Timeline over a fixed source set (used by tests).
func Static(sources ...Source) *Timeline {
	return New(func() ([]Source, error) { return sources, nil })
}

// Poll reads every source past its cursor, merges the new events into Time
// order (stable tiebreak: source path, then seq), advances the cursors, and
// returns the new items. Run-log lifecycle markers that duplicate a job source
// in scope are dropped — the job's own log is authoritative for its lifecycle,
// so the run log contributes only narration (notes) and any run-level event not
// owned by a job. A missing file is not an error (a source can be created after
// discovery).
func (t *Timeline) Poll() ([]Item, error) {
	sources, err := t.scope()
	if err != nil {
		return nil, err
	}
	jobSources := map[string]bool{}
	for _, s := range sources {
		if s.JobID != "" {
			jobSources[s.JobID] = true
		}
	}

	type keyed struct {
		item Item
		path string
	}
	var batch []keyed
	for _, s := range sources {
		evs, err := events.Read(s.Path, t.cursors[s.Path])
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, e := range evs {
			if e.Seq > t.cursors[s.Path] {
				t.cursors[s.Path] = e.Seq
			}
			// Drop a run-log lifecycle marker whose job is separately
			// sourced: its job log already carries the richer lifecycle.
			if s.JobID == "" {
				if j, ok := e.Fields["job"].(string); ok && jobSources[j] {
					continue
				}
			}
			batch = append(batch, keyed{item: Item{JobID: s.JobID, Run: s.Run, Event: e}, path: s.Path})
		}
	}
	sort.SliceStable(batch, func(i, j int) bool {
		a, b := batch[i], batch[j]
		if !a.item.Event.Time.Equal(b.item.Event.Time) {
			return a.item.Event.Time.Before(b.item.Event.Time)
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return a.item.Event.Seq < b.item.Event.Seq
	})
	out := make([]Item, len(batch))
	for i := range batch {
		out[i] = batch[i].item
	}
	return out, nil
}

// --- source discovery ---

func jobEventsPath(s *job.Store, id string) string {
	return filepath.Join(s.JobDir(id), "events.jsonl")
}

func runEventsPath(s *job.Store, label string) string {
	// Build the path directly (not via store.RunEventsPath, which creates the
	// directory) — discovery must never mutate the state dir.
	return filepath.Join(s.Root, "runs", label, "events.jsonl")
}

// RunLabels returns the run labels that own an event log, sorted. A run is a
// directory <root>/runs/<label>/events.jsonl; stray files in the runs dir (and
// run subdirs without an events.jsonl) are ignored.
func RunLabels(s *job.Store) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var labels []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(runEventsPath(s, e.Name())); err == nil {
			labels = append(labels, e.Name())
		}
	}
	sort.Strings(labels)
	return labels, nil
}

// ScopeAll follows every job and every run log; re-evaluated each Poll so newly
// dispatched jobs join the stream.
func ScopeAll(s *job.Store) func() ([]Source, error) {
	return func() ([]Source, error) {
		metas, err := s.List()
		if err != nil {
			return nil, err
		}
		var srcs []Source
		for _, m := range metas {
			srcs = append(srcs, Source{Path: jobEventsPath(s, m.ID), JobID: m.ID, Run: m.Run})
		}
		labels, err := RunLabels(s)
		if err != nil {
			return nil, err
		}
		for _, l := range labels {
			srcs = append(srcs, Source{Path: runEventsPath(s, l), Run: l})
		}
		return srcs, nil
	}
}

// ScopeRun follows one run's log plus every job tagged with that run label.
func ScopeRun(s *job.Store, label string) func() ([]Source, error) {
	return func() ([]Source, error) {
		metas, err := s.List()
		if err != nil {
			return nil, err
		}
		srcs := []Source{{Path: runEventsPath(s, label), Run: label}}
		for _, m := range metas {
			if m.Run == label {
				srcs = append(srcs, Source{Path: jobEventsPath(s, m.ID), JobID: m.ID, Run: m.Run})
			}
		}
		return srcs, nil
	}
}

// ScopeJob follows a single job's log.
func ScopeJob(s *job.Store, id string) func() ([]Source, error) {
	return func() ([]Source, error) {
		return []Source{{Path: jobEventsPath(s, id), JobID: id}}, nil
	}
}
