package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/timeline"
)

const threshold = 150_000

// seed builds a Model populated as if a dataMsg had just landed — no store IO,
// so Update/View stay unit-testable.
func seed(t *testing.T, metas []*job.Meta) Model {
	t.Helper()
	m := New(nil, threshold)
	rollups := timeline.Rollups(metas, nil, threshold)
	tm, _ := m.Update(dataMsg{metas: metas, rollups: rollups})
	return tm.(Model)
}

func key(m Model, s string) Model {
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return tm.(Model)
}

func specialKey(m Model, k tea.KeyType) Model {
	tm, _ := m.Update(tea.KeyMsg{Type: k})
	return tm.(Model)
}

func metasFixture() []*job.Meta {
	now := time.Now()
	return []*job.Meta{
		{ID: "job-1", Run: "alpha", State: job.StateDone, Updated: now.Add(-2 * time.Minute)},
		{ID: "job-2", Run: "alpha", State: job.StateActive, Context: 200_000, Updated: now.Add(-1 * time.Minute)},
		{ID: "job-3", Run: "beta", State: job.StateNeedsInput, Question: "postgres or sqlite?", Updated: now},
	}
}

func TestSelectionMovesAcrossJobs(t *testing.T) {
	m := seed(t, metasFixture())
	if len(m.jobs) != 3 {
		t.Fatalf("want 3 selectable jobs, got %d", len(m.jobs))
	}
	if got := m.selectedJobID(); got == "" {
		t.Fatalf("initial selection should be set, got empty")
	}
	start := m.selectedJobID()

	m = key(m, "j")
	if m.sel != 1 {
		t.Fatalf("after j, sel=%d want 1", m.sel)
	}
	if m.selectedJobID() == start {
		t.Fatalf("selection did not move")
	}

	// Down arrow also moves.
	m = specialKey(m, tea.KeyDown)
	if m.sel != 2 {
		t.Fatalf("after down, sel=%d want 2", m.sel)
	}
	// Clamp at the end.
	m = key(m, "j")
	if m.sel != 2 {
		t.Fatalf("selection past end should clamp at 2, got %d", m.sel)
	}
	// Back up, and clamp at 0.
	m = key(m, "k")
	m = key(m, "k")
	m = key(m, "k")
	if m.sel != 0 {
		t.Fatalf("selection should clamp at 0, got %d", m.sel)
	}
}

func TestNeedsInputIsLoudInView(t *testing.T) {
	m := seed(t, metasFixture())
	// Select job-3 (needs-input), wherever it landed in the flattened order.
	for i := 0; i < len(m.jobs) && m.selectedJobID() != "job-3"; i++ {
		m = key(m, "j")
	}
	if m.selectedJobID() != "job-3" {
		t.Fatalf("expected job-3 selectable, got %s", m.selectedJobID())
	}
	view := m.View()
	if !strings.Contains(view, "postgres or sqlite?") {
		t.Fatalf("needs-input question must be surfaced in the view:\n%s", view)
	}
	if !strings.Contains(view, "NEEDS INPUT") {
		t.Fatalf("needs-input must get a loud label:\n%s", view)
	}
}

func TestViewShowsRunsAndJobs(t *testing.T) {
	m := seed(t, metasFixture())
	view := m.View()
	for _, want := range []string{"alpha", "beta", "job-1", "job-2", "job-3", "runs", "timeline"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestViewLeadsWithHumanStatusAndModeHelp(t *testing.T) {
	m := seed(t, metasFixture())
	view := m.View()
	for _, want := range []string{
		"ATTENTION", "job-3 needs input", "postgres or sqlite?",
		"overview:", "j/k select", "enter focus detail",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing hierarchy/help token %q:\n%s", want, view)
		}
	}

	m = specialKey(m, tea.KeyEnter)
	view = m.View()
	for _, want := range []string{"detail:", "j/k scroll events", "esc overview"} {
		if !strings.Contains(view, want) {
			t.Fatalf("focused view missing help token %q:\n%s", want, view)
		}
	}
}

func TestActionNeededJobsSortBeforeRoutineRows(t *testing.T) {
	now := time.Now()
	m := seed(t, []*job.Meta{
		{ID: "job-2", Run: "alpha", State: job.StateActive, Updated: now, Task: "routine active"},
		{ID: "job-10", Run: "alpha", State: job.StateNeedsInput, Question: "approve?", Updated: now, Task: "needs a decision"},
		{ID: "job-1", Run: "alpha", State: job.StateDone, Updated: now, Task: "done"},
	})
	if got := m.jobs[0].meta.ID; got != "job-10" {
		t.Fatalf("first selectable job should be action-needed, got %s; order=%v", got, jobIDs(m.jobs))
	}
	if got := m.jobs[len(m.jobs)-1].meta.ID; got != "job-1" {
		t.Fatalf("done job should sort after active work, got last=%s; order=%v", got, jobIDs(m.jobs))
	}
}

func TestFocusedDetailScrollsEventsInsteadOfChangingSelection(t *testing.T) {
	m := seed(t, metasFixture())
	m.width = 100
	m.height = 22 // leaves room for one visible event in the detail pane.
	items := []timeline.Item{
		{JobID: "job-3", Event: events.Event{Type: events.TypeText, Preview: "event 01 oldest", Time: time.Now().Add(-4 * time.Minute)}},
		{JobID: "job-3", Event: events.Event{Type: events.TypeText, Preview: "event 02 middle", Time: time.Now().Add(-3 * time.Minute)}},
		{JobID: "job-3", Event: events.Event{Type: events.TypeText, Preview: "event 03 middle", Time: time.Now().Add(-2 * time.Minute)}},
		{JobID: "job-3", Event: events.Event{Type: events.TypeText, Preview: "event 04 newest", Time: time.Now().Add(-time.Minute)}},
	}
	m.detail = items
	startJob := m.selectedJobID()

	view := m.View()
	if !strings.Contains(view, "event 04 newest") {
		t.Fatalf("detail pane should default to newest event:\n%s", view)
	}

	m = specialKey(m, tea.KeyEnter)
	m = key(m, "k")
	if got := m.selectedJobID(); got != startJob {
		t.Fatalf("focused j/k should scroll detail, not change selection: got %s want %s", got, startJob)
	}
	view = m.View()
	if !strings.Contains(view, "event 03 middle") || strings.Contains(view, "event 04 newest") {
		t.Fatalf("scrolling up should show the previous detail event only:\n%s", view)
	}
}

func jobIDs(refs []jobRef) []string {
	out := make([]string, len(refs))
	for i, ref := range refs {
		out[i] = ref.meta.ID
	}
	return out
}

func TestFirehoseToggle(t *testing.T) {
	m := seed(t, metasFixture())
	if m.firehose {
		t.Fatal("firehose should default off")
	}
	m = key(m, "f")
	if !m.firehose {
		t.Fatal("f should toggle firehose on")
	}
	if !strings.Contains(m.View(), "events (full)") {
		t.Fatalf("detail pane should show full mode after toggle")
	}
	m = key(m, "f")
	if m.firehose {
		t.Fatal("f should toggle firehose back off")
	}
}

func TestFocusToggle(t *testing.T) {
	m := seed(t, metasFixture())
	m = specialKey(m, tea.KeyEnter)
	if !m.focus {
		t.Fatal("enter should focus detail")
	}
	m = specialKey(m, tea.KeyEsc)
	if m.focus {
		t.Fatal("esc should unfocus")
	}
}

func TestQuit(t *testing.T) {
	m := seed(t, metasFixture())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should return a command (Quit)")
	}
	// tea.Quit returns a QuitMsg when invoked.
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q's command should be tea.Quit")
	}
}

func TestStreamAppendAndTimelinePane(t *testing.T) {
	m := seed(t, metasFixture())
	item := timeline.Item{JobID: "job-2", Event: events.Event{
		Type: events.TypeText, Preview: "hello world", Time: time.Now()}}
	tm, _ := m.Update(dataMsg{metas: metasFixture(),
		rollups:   timeline.Rollups(metasFixture(), nil, threshold),
		newStream: []timeline.Item{item}})
	m = tm.(Model)
	if !strings.Contains(m.View(), "hello world") {
		t.Fatalf("timeline pane should show streamed event:\n%s", m.View())
	}
}

func TestContextHighColoringDoesNotCorruptText(t *testing.T) {
	m := seed(t, metasFixture())
	// job-2 has 200k context (> threshold); its run alpha should be context-high.
	if len(m.rollups) == 0 {
		t.Fatal("no rollups")
	}
	view := m.View()
	// The run header still contains the label even with high-context styling.
	if !strings.Contains(view, "alpha") {
		t.Fatalf("high-context styling corrupted the run label:\n%s", view)
	}
}
