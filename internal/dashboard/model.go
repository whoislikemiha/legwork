// Package dashboard is the read-only bubbletea TUI: an htop-for-jobs built on
// internal/timeline. Three stacked panes — runs rollup, selected-job detail,
// and the curated timeline — refreshed on a ~1s tick. It mutates nothing; every
// number it shows is a render of the same JSONL every other surface reads.
//
// The Model's Update/View are kept pure (data arrives as messages, never read
// from disk inside Update) so selection movement and needs-input highlighting
// are unit-testable without a PTY.
package dashboard

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/timeline"
)

const maxStream = 500 // bound the retained curated stream

// jobRef is one selectable row: a job shown under its run.
type jobRef struct {
	run  string
	meta *job.Meta
}

// Model is the dashboard state. Construct with New, then run under bubbletea; or
// build one directly in tests and drive Update with messages.
type Model struct {
	store     *job.Store
	threshold int
	tl        *timeline.Timeline

	width, height int

	metas   []*job.Meta
	rollups []timeline.RunRollup
	jobs    []jobRef // flattened selectable rows, aligned with the runs pane
	sel     int      // index into jobs

	stream       []timeline.Item // curated, newest last, bounded
	detail       []timeline.Item // selected job's recent events
	detailScroll int             // 0 = newest detail events; larger scrolls toward older events

	firehose bool // f: firehose vs curated in the detail pane
	focus    bool // enter: detail focused
	err      error
}

// New builds a dashboard Model over a store and health threshold.
func New(store *job.Store, threshold int) Model {
	return Model{
		store:     store,
		threshold: threshold,
		tl:        timeline.New(timeline.ScopeAll(store)),
		width:     80,
		height:    24,
	}
}

// --- messages ---

type tickMsg time.Time

// dataMsg carries a read-only refresh produced off the Update goroutine.
type dataMsg struct {
	metas     []*job.Meta
	rollups   []timeline.RunRollup
	newStream []timeline.Item
	detail    []timeline.Item
	err       error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.reload(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// reload reads the state dir read-only and returns a dataMsg. It captures the
// current selection so it can fetch that job's detail events in the same pass.
func (m Model) reload() tea.Cmd {
	store, tl, threshold := m.store, m.tl, m.threshold
	selJob := m.selectedJobID()
	firehose := m.firehose
	return func() tea.Msg {
		metas, err := store.List()
		if err != nil {
			return dataMsg{err: err}
		}
		for _, mm := range metas {
			store.Reconcile(mm)
		}
		runLogs, err := timeline.RunLogs(store)
		if err != nil {
			return dataMsg{err: err}
		}
		rollups := timeline.Rollups(metas, runLogs, threshold)
		newStream, err := tl.Poll()
		if err != nil {
			return dataMsg{err: err}
		}
		newStream = timeline.Curated(newStream)
		var detail []timeline.Item
		if selJob != "" {
			items, _ := timeline.New(timeline.ScopeJob(store, selJob)).Poll()
			if !firehose {
				items = timeline.Curated(items)
			}
			detail = items
		}
		return dataMsg{metas: metas, rollups: rollups, newStream: newStream, detail: detail}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.reload(), tick())
	case dataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.metas = msg.metas
		m.rollups = msg.rollups
		m.jobs = flattenJobs(msg.rollups, msg.metas)
		m.clampSel()
		m.stream = append(m.stream, msg.newStream...)
		if len(m.stream) > maxStream {
			m.stream = m.stream[len(m.stream)-maxStream:]
		}
		m.detail = msg.detail
		m.clampDetailScroll()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey applies a keypress. Selection/firehose changes trigger an immediate
// reload so the detail pane tracks the new selection without waiting a tick.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.focus {
			m.detailScroll--
			m.clampDetailScroll()
			return m, nil
		}
		m.sel++
		m.clampSel()
		m.detailScroll = 0
		return m, m.reload()
	case "k", "up":
		if m.focus {
			m.detailScroll++
			m.clampDetailScroll()
			return m, nil
		}
		m.sel--
		m.clampSel()
		m.detailScroll = 0
		return m, m.reload()
	case "enter":
		m.focus = true
		return m, nil
	case "esc":
		m.focus = false
		return m, nil
	case "f":
		m.firehose = !m.firehose
		m.detailScroll = 0
		return m, m.reload()
	}
	return m, nil
}

func (m *Model) clampSel() {
	if m.sel < 0 {
		m.sel = 0
	}
	if m.sel >= len(m.jobs) {
		m.sel = len(m.jobs) - 1
	}
	if m.sel < 0 {
		m.sel = 0
	}
}

func (m *Model) clampDetailScroll() {
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
	max := len(m.detail) - 1
	if max < 0 {
		max = 0
	}
	if m.detailScroll > max {
		m.detailScroll = max
	}
}

func (m Model) selectedJobID() string {
	if m.sel >= 0 && m.sel < len(m.jobs) {
		return m.jobs[m.sel].meta.ID
	}
	return ""
}

func (m Model) selectedJob() *job.Meta {
	if m.sel >= 0 && m.sel < len(m.jobs) {
		return m.jobs[m.sel].meta
	}
	return nil
}

// flattenJobs builds the selectable-row order: jobs grouped under their run in
// rollup order (so the runs pane and the selection cursor stay aligned), with
// action-needed jobs before routine/closed rows and numeric ID as the tiebreaker.
func flattenJobs(rollups []timeline.RunRollup, metas []*job.Meta) []jobRef {
	byRun := map[string][]*job.Meta{}
	for _, mm := range metas {
		byRun[mm.Run] = append(byRun[mm.Run], mm)
	}
	var out []jobRef
	for _, r := range rollups {
		js := byRun[r.Label]
		sortMetasForAttention(js)
		for _, mm := range js {
			out = append(out, jobRef{run: r.Label, meta: mm})
		}
	}
	return out
}

func sortMetasForAttention(ms []*job.Meta) {
	// Insertion sort; job lists are tiny.
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && metaLess(ms[j], ms[j-1]); j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

func metaLess(a, b *job.Meta) bool {
	if jobStateRank(a.State) != jobStateRank(b.State) {
		return jobStateRank(a.State) < jobStateRank(b.State)
	}
	return idLess(a.ID, b.ID)
}

func jobStateRank(s job.State) int {
	switch s {
	case job.StateNeedsInput:
		return 0
	case job.StateBlocked:
		return 1
	case job.StateFailed:
		return 2
	case job.StateAuthNeeded:
		return 3
	case job.StateInterrupted:
		return 4
	case job.StateActive:
		return 5
	case job.StateQueued:
		return 6
	case job.StateDone:
		return 7
	case job.StateClosed:
		return 8
	default:
		return 9
	}
}

// idLess orders "job-2" before "job-10" (numeric suffix).
func idLess(a, b string) bool {
	na, oka := jobNum(a)
	nb, okb := jobNum(b)
	if oka && okb {
		return na < nb
	}
	return a < b
}

func jobNum(id string) (int, bool) {
	i := strings.LastIndexByte(id, '-')
	if i < 0 {
		return 0, false
	}
	n := 0
	for _, c := range id[i+1:] {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// fmtContext mirrors main.fmtContext for the detail header (kept local so the
// dashboard package has no dependency on package main).
func fmtContext(tokens int) string {
	if tokens == 0 {
		return "-"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	return fmt.Sprintf("%dk", tokens/1000)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
