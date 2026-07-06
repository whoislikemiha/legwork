package timeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
)

// writeLog appends events to a JSONL file with explicit times, bypassing
// events.Log (which stamps time.Now) so tests control ordering precisely.
func writeLog(t *testing.T, path string, evs ...events.Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range evs {
		if e.V == 0 {
			e.V = events.SchemaVersion
		}
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
}

func at(base time.Time, secs int) time.Time { return base.Add(time.Duration(secs) * time.Second) }

func TestPollMergeOrdersByTime(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	writeLog(t, a,
		events.Event{Seq: 1, Time: at(base, 0), Type: events.TypeStarted},
		events.Event{Seq: 2, Time: at(base, 4), Type: events.TypeText, Preview: "a-late"},
	)
	writeLog(t, b,
		events.Event{Seq: 1, Time: at(base, 2), Type: events.TypeNote, Preview: "b-mid"},
	)
	tl := Static(
		Source{Path: a, JobID: "job-1"},
		Source{Path: b, Run: "r"},
	)
	items, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, it := range items {
		got = append(got, it.Badge()+":"+it.Event.Type)
	}
	want := []string{"job-1:started", "[r]:note", "job-1:text"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d]=%q want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestPollStableTiebreakBySourcePath(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	a := filepath.Join(dir, "a.jsonl")
	z := filepath.Join(dir, "z.jsonl")
	// Identical timestamps: source path breaks the tie deterministically.
	writeLog(t, z, events.Event{Seq: 1, Time: ts, Type: events.TypeText, Preview: "z"})
	writeLog(t, a, events.Event{Seq: 1, Time: ts, Type: events.TypeText, Preview: "a"})
	tl := Static(Source{Path: z, JobID: "z"}, Source{Path: a, JobID: "a"})
	items, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Event.Preview != "a" || items[1].Event.Preview != "z" {
		t.Fatalf("tiebreak not by path: %+v", items)
	}
}

func TestPollIsIncremental(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	a := filepath.Join(dir, "a.jsonl")
	writeLog(t, a, events.Event{Seq: 1, Time: at(base, 0), Type: events.TypeStarted})
	tl := Static(Source{Path: a, JobID: "job-1"})
	first, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first poll got %d, want 1", len(first))
	}
	// No new events -> empty poll.
	if again, _ := tl.Poll(); len(again) != 0 {
		t.Fatalf("second poll returned %d, want 0 (cursor not advancing)", len(again))
	}
	// Append; only the new event comes back.
	writeLog(t, a, events.Event{Seq: 2, Time: at(base, 1), Type: events.TypeFinished})
	third, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 1 || third[0].Event.Type != events.TypeFinished {
		t.Fatalf("third poll = %+v, want just finished", third)
	}
}

func TestPollMissingFileIsNotAnError(t *testing.T) {
	tl := Static(Source{Path: filepath.Join(t.TempDir(), "nope.jsonl"), JobID: "job-1"})
	items, err := tl.Poll()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("missing file should yield no items, got %d", len(items))
	}
}

func TestPollDropsRunLogDuplicateLifecycle(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	jobLog := filepath.Join(dir, "job.jsonl")
	runLog := filepath.Join(dir, "run.jsonl")
	writeLog(t, jobLog, events.Event{Seq: 1, Time: at(base, 0), Type: events.TypeFinished, Preview: "job-copy"})
	writeLog(t, runLog,
		// duplicate lifecycle marker (carries fields.job -> the sourced job)
		events.Event{Seq: 1, Time: at(base, 0), Type: events.TypeFinished, Preview: "run-copy",
			Fields: map[string]any{"job": "job-7", "state": "done"}},
		// unique narration (no fields.job) -> kept
		events.Event{Seq: 2, Time: at(base, 1), Type: events.TypeNote, Preview: "the decision"},
	)
	tl := Static(
		Source{Path: jobLog, JobID: "job-7", Run: "r"},
		Source{Path: runLog, Run: "r"},
	)
	items, err := tl.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 items (job finished + run note), got %d: %+v", len(items), items)
	}
	if items[0].Event.Preview != "job-copy" || items[0].Badge() != "job-7" {
		t.Fatalf("first item should be the job-log finished, got %+v", items[0])
	}
	if items[1].Event.Type != events.TypeNote || items[1].Badge() != "[r]" {
		t.Fatalf("second item should be the run note, got %+v", items[1])
	}
}

func TestPollKeepsRunLifecycleWhenJobNotSourced(t *testing.T) {
	// In run scope a job might be gone (gc'd) but its run-log marker survives;
	// with no job source to defer to, the run-log marker must still show.
	dir := t.TempDir()
	ts := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	runLog := filepath.Join(dir, "run.jsonl")
	writeLog(t, runLog, events.Event{Seq: 1, Time: ts, Type: events.TypeFinished, Preview: "orphan",
		Fields: map[string]any{"job": "job-99", "state": "done"}})
	tl := Static(Source{Path: runLog, Run: "r"})
	items, _ := tl.Poll()
	if len(items) != 1 {
		t.Fatalf("orphaned run-log marker should be kept, got %d", len(items))
	}
}
