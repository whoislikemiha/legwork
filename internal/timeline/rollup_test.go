package timeline

import (
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

func TestStateSummary(t *testing.T) {
	cases := []struct {
		name string
		jobs map[string]int
		want string
	}{
		{"empty", map[string]int{}, ""},
		{"all-same bare", map[string]int{"done": 2}, "done"},
		{"worst-first", map[string]int{"done": 2, "active": 1, "needs-input": 1},
			"1 needs-input · 1 active · 2 done"},
		{"blocked before done", map[string]int{"done": 3, "blocked": 1}, "1 blocked · 3 done"},
		{"zero counts ignored", map[string]int{"done": 1, "failed": 0}, "done"},
	}
	for _, c := range cases {
		if got := StateSummary(c.jobs); got != c.want {
			t.Errorf("%s: StateSummary=%q want %q", c.name, got, c.want)
		}
	}
}

func TestRollups(t *testing.T) {
	base := time.Date(2026, 7, 7, 19, 0, 0, 0, time.UTC)
	metas := []*job.Meta{
		{ID: "job-1", Run: "alpha", State: job.StateDone, CostUSD: 1.5, Context: 10_000, Updated: base.Add(1 * time.Minute)},
		{ID: "job-2", Run: "alpha", State: job.StateActive, CostUSD: 1.0, Context: 200_000, Updated: base.Add(2 * time.Minute)},
		{ID: "job-3", Run: "beta", State: job.StateNeedsInput, CostUSD: 0.5, Context: 5_000, Updated: base.Add(5 * time.Minute)},
		{ID: "job-4", State: job.StateDone, CostUSD: 0.1, Updated: base.Add(30 * time.Second)}, // no run
	}
	runLogs := map[string]RunLog{
		"alpha": {LastNote: "shipped it", LastActivity: base.Add(3 * time.Minute)},
		"beta":  {LastActivity: base.Add(4 * time.Minute)}, // older than job-3's Updated
	}
	got := Rollups(metas, runLogs, 150_000)

	// Sorted newest-activity first: beta (5m via job-3) > alpha (3m via note) > (no run) (30s).
	if len(got) != 3 {
		t.Fatalf("want 3 rollups, got %d", len(got))
	}
	if got[0].Label != "beta" || got[1].Label != "alpha" || got[2].Label != "" {
		t.Fatalf("sort order wrong: %s, %s, %s", got[0].Label, got[1].Label, got[2].Label)
	}

	alpha := got[1]
	if alpha.CostUSD != 2.5 {
		t.Errorf("alpha cost=%v want 2.5", alpha.CostUSD)
	}
	if !alpha.ContextHigh {
		t.Errorf("alpha should be context-high (job-2 at 200k >= 150k)")
	}
	if alpha.LastNote != "shipped it" {
		t.Errorf("alpha note=%q", alpha.LastNote)
	}
	if alpha.Updated != base.Add(3*time.Minute) {
		t.Errorf("alpha updated should fold in the note activity (3m), got %v", alpha.Updated)
	}
	if s := StateSummary(alpha.Jobs); s != "1 active · 1 done" {
		t.Errorf("alpha state summary=%q", s)
	}

	beta := got[0]
	if beta.ContextHigh {
		t.Errorf("beta should not be context-high")
	}
	if beta.Updated != base.Add(5*time.Minute) {
		t.Errorf("beta updated should be job-3's Updated (5m), got %v", beta.Updated)
	}

	norun := got[2]
	if norun.Label != "" || norun.LastNote != "" {
		t.Errorf("no-run bucket should have empty label and no note: %+v", norun)
	}
}

func TestRollupsClosedJobExcludedFromContextHigh(t *testing.T) {
	metas := []*job.Meta{
		{ID: "job-1", Run: "r", State: job.StateClosed, Context: 300_000, Updated: time.Now()},
	}
	got := Rollups(metas, nil, 150_000)
	if len(got) != 1 || got[0].ContextHigh {
		t.Fatalf("closed job must not trip context-high: %+v", got)
	}
}

func TestRollupsThresholdZeroDisables(t *testing.T) {
	metas := []*job.Meta{{ID: "job-1", Run: "r", State: job.StateActive, Context: 999_999, Updated: time.Now()}}
	got := Rollups(metas, nil, 0)
	if got[0].ContextHigh {
		t.Fatalf("threshold 0 must disable context-high")
	}
}

func TestIsCurated(t *testing.T) {
	for _, ty := range []string{events.TypeNote, events.TypeFinished, events.TypeText,
		events.TypeCheckpoint, events.TypeNeedsInput} {
		if !IsCurated(ty) {
			t.Errorf("%s should be curated", ty)
		}
	}
	for _, ty := range []string{events.TypeToolCall, events.TypeProgress, events.TypeUsage} {
		if IsCurated(ty) {
			t.Errorf("%s should be firehose, not curated", ty)
		}
	}
}

func TestCuratedFilter(t *testing.T) {
	items := []Item{
		{Event: events.Event{Type: events.TypeToolCall}},
		{Event: events.Event{Type: events.TypeNote}},
		{Event: events.Event{Type: events.TypeUsage}},
		{Event: events.Event{Type: events.TypeFinished}},
	}
	got := Curated(items)
	if len(got) != 2 || got[0].Event.Type != events.TypeNote || got[1].Event.Type != events.TypeFinished {
		t.Fatalf("Curated filter wrong: %+v", got)
	}
}
