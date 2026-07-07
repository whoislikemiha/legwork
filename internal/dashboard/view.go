package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/timeline"
)

// Styles degrade to plain text when the renderer has no color (NO_COLOR, or a
// non-TTY like the test harness), so View's text tokens survive substring
// assertions.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	bannerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("4"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	loudStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1"))
	highStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

// View renders the three stacked panes plus a footer. It never reads disk;
// everything comes from Model state set by dataMsg.
func (m Model) View() string {
	if m.err != nil {
		return "dashboard error: " + m.err.Error() + "\n"
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	// Budget body lines across the three panes (minus status + 3 headers + footer).
	body := h - 5
	if body < 6 {
		body = 6
	}
	runsBudget := body / 3
	detailBudget := body / 3
	tlBudget := body - runsBudget - detailBudget
	if runsBudget < 2 {
		runsBudget = 2
	}
	if detailBudget < 2 {
		detailBudget = 2
	}
	if tlBudget < 2 {
		tlBudget = 2
	}

	var b strings.Builder
	b.WriteString(statusBanner(m.statusLine(), w) + "\n")
	b.WriteString(header("runs — overview", w) + "\n")
	b.WriteString(strings.Join(m.runsPane(w, runsBudget), "\n"))
	b.WriteString("\n")

	title := "detail"
	if j := m.selectedJob(); j != nil {
		title = "detail — " + j.ID
	}
	if m.focus {
		title += " (focused)"
	}
	b.WriteString(header(title, w) + "\n")
	b.WriteString(strings.Join(m.detailPane(w, detailBudget), "\n"))
	b.WriteString("\n")

	b.WriteString(header("timeline — curated", w) + "\n")
	b.WriteString(strings.Join(m.timelinePane(w, tlBudget), "\n"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(clip(m.footer(), w)))
	return b.String()
}

// header renders "title ──────" padded to width.
func header(title string, w int) string {
	t := titleStyle.Render(title)
	rule := w - lipgloss.Width(t) - 1
	if rule < 0 {
		rule = 0
	}
	return t + " " + dimStyle.Render(strings.Repeat("─", rule))
}

func statusBanner(s string, w int) string {
	return bannerStyle.Render(clip(s, w))
}

func (m Model) footer() string {
	mode := "curated"
	if m.firehose {
		mode = "full"
	}
	if m.focus {
		return "detail: j/k scroll events · esc overview · f " + mode + "/full · q quit"
	}
	return "overview: j/k select · enter focus detail · f " + mode + "/full · q quit"
}

func (m Model) statusLine() string {
	if len(m.metas) == 0 {
		return "EMPTY: no jobs yet"
	}
	if j := firstJobWithState(m.metas, job.StateNeedsInput); j != nil {
		msg := j.Question
		if msg == "" {
			msg = firstLine(j.Task)
		}
		if msg != "" {
			return fmt.Sprintf("ATTENTION: %s needs input — %s", j.ID, msg)
		}
		return fmt.Sprintf("ATTENTION: %s needs input", j.ID)
	}
	for _, s := range []job.State{job.StateBlocked, job.StateFailed, job.StateAuthNeeded, job.StateInterrupted} {
		if n := countState(m.metas, s); n > 0 {
			return fmt.Sprintf("ATTENTION: %d %s", n, pluralState(s, n))
		}
	}
	active := countState(m.metas, job.StateActive)
	queued := countState(m.metas, job.StateQueued)
	if active+queued > 0 {
		parts := []string{}
		if active > 0 {
			parts = append(parts, fmt.Sprintf("%d active", active))
		}
		if queued > 0 {
			parts = append(parts, fmt.Sprintf("%d queued", queued))
		}
		return "RUNNING: " + strings.Join(parts, " · ")
	}
	return fmt.Sprintf("CLEAR: %d complete/closed", len(m.metas))
}

func firstJobWithState(metas []*job.Meta, state job.State) *job.Meta {
	for _, m := range metas {
		if m.State == state {
			return m
		}
	}
	return nil
}

func countState(metas []*job.Meta, state job.State) int {
	n := 0
	for _, m := range metas {
		if m.State == state {
			n++
		}
	}
	return n
}

func pluralState(s job.State, n int) string {
	if n == 1 {
		return string(s)
	}
	return string(s) + " jobs"
}

// runsPane lists each run rollup and, under it, its jobs. The selected job is
// marked; needs-input rows get the loud treatment. When the pane can't hold
// every row it windows around the selection.
func (m Model) runsPane(w, budget int) []string {
	var lines []string
	selIdx := -1 // index into lines of the selected job row
	jobCursor := 0
	for _, r := range m.rollups {
		label := r.Label
		if label == "" {
			label = "(no run)"
		}
		ctx := "ctx ok"
		if m.threshold > 0 && r.ContextHigh {
			ctx = highStyle.Render("ctx !")
		} else if m.threshold <= 0 {
			ctx = ""
		}
		runLine := fmt.Sprintf("%-13s %-16s $%-6.2f %s",
			clip(label, 13), timeline.StateSummary(r.Jobs), r.CostUSD, ctx)
		// A run awaiting input gets the loud treatment; the color carries the
		// signal (the state summary already names it).
		if hasState(r.Jobs, job.StateNeedsInput) {
			lines = append(lines, loudStyle.Render(clip(runLine, w)))
		} else {
			lines = append(lines, clip(runLine, w))
		}

		// This run's jobs, in the same flattened order as selection.
		for jobCursor < len(m.jobs) && m.jobs[jobCursor].run == r.Label {
			jr := m.jobs[jobCursor]
			marker := "  └ "
			if jobCursor == m.sel {
				marker = "  ▶ "
				selIdx = len(lines)
			}
			row := fmt.Sprintf("%s%-8s %-12s ctx:%-6s %-4s %s",
				marker, jr.meta.ID, jr.meta.State, fmtContext(jr.meta.Context),
				ageShort(jr.meta), clip(firstLine(jr.meta.Task), 30))
			switch {
			case jr.meta.State == job.StateNeedsInput:
				row = loudStyle.Render(clip(row, w))
			case jobCursor == m.sel:
				row = selStyle.Render(clip(row, w))
			default:
				row = clip(row, w)
			}
			lines = append(lines, row)
			jobCursor++
		}
	}
	return window(lines, selIdx, budget)
}

func (m Model) detailPane(w, budget int) []string {
	j := m.selectedJob()
	if j == nil {
		return []string{dimStyle.Render("no jobs")}
	}
	var lines []string
	head := fmt.Sprintf("state: %-11s turns:%-3d $%.2f  ctx:%s",
		j.State, j.Turns, j.CostUSD, fmtContext(j.Context))
	if m.threshold > 0 && j.ContextHigh(m.threshold) {
		head = highStyle.Render(head + "  HIGH")
	}
	lines = append(lines, clip(head, w))
	// needs-input must be impossible to miss.
	if j.State == job.StateNeedsInput && j.Question != "" {
		lines = append(lines, loudStyle.Render(clip("NEEDS INPUT: "+j.Question, w)))
	}
	lines = append(lines, clip("task: "+firstLine(j.Task), w))

	mode := "curated"
	if m.firehose {
		mode = "full"
	}
	label := "events (" + mode + "):"
	if m.focus {
		label += " scroll j/k"
	}
	if len(m.detail) > 0 && m.detailScroll > 0 {
		label += fmt.Sprintf(" older +%d", m.detailScroll)
	}
	lines = append(lines, dimStyle.Render(label))
	evLines := budget - len(lines)
	for _, it := range detailWindow(m.detail, evLines, m.detailScroll) {
		lines = append(lines, clip(detailEventLine(it), w))
	}
	return window(lines, 0, budget)
}

func (m Model) timelinePane(w, budget int) []string {
	items := lastItems(m.stream, budget)
	if len(items) == 0 {
		return []string{dimStyle.Render("(no events yet)")}
	}
	var lines []string
	for _, it := range items {
		line := fmt.Sprintf("%s %s %s %s",
			it.Event.Time.Local().Format("15:04"), it.Badge(),
			shortType(it.Event.Type), firstLine(it.Event.Preview))
		if it.Event.Type == events.TypeNeedsInput {
			line = loudStyle.Render(clip(line, w))
		} else {
			line = clip(line, w)
		}
		lines = append(lines, line)
	}
	return lines
}

// --- small helpers ---

func detailEventLine(it timeline.Item) string {
	return fmt.Sprintf("▸ %s %s %s",
		it.Event.Time.Local().Format("15:04"), shortType(it.Event.Type), firstLine(it.Event.Preview))
}

func shortType(t string) string {
	switch t {
	case events.TypeCheckpoint:
		return "ckpt"
	case events.TypeNeedsInput:
		return "needs-input"
	case events.TypeToolCall:
		return "tool"
	default:
		return t
	}
}

func hasState(jobs map[string]int, s job.State) bool { return jobs[string(s)] > 0 }

func ageShort(m *job.Meta) string {
	if m.Updated.IsZero() {
		return "-"
	}
	d := time.Since(m.Updated)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// lastItems returns the last n items (newest-last order preserved).
func lastItems(items []timeline.Item, n int) []timeline.Item {
	if n <= 0 {
		return nil
	}
	if len(items) > n {
		return items[len(items)-n:]
	}
	return items
}

// detailWindow returns n contiguous detail events, newest-last, offset from the
// newest window by scroll. scroll=0 follows the tail; scroll=1 moves one event
// older, matching a log viewer's "scroll up for history" model.
func detailWindow(items []timeline.Item, n, scroll int) []timeline.Item {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	if n >= len(items) {
		return items
	}
	if scroll < 0 {
		scroll = 0
	}
	maxScroll := len(items) - n
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := len(items) - scroll
	start := end - n
	return items[start:end]
}

// window returns at most budget lines, keeping the line at keep visible by
// scrolling the slice when it would overflow.
func window(lines []string, keep, budget int) []string {
	if budget <= 0 {
		budget = 1
	}
	if len(lines) <= budget {
		return lines
	}
	start := 0
	if keep >= budget {
		start = keep - budget + 1
	}
	if start+budget > len(lines) {
		start = len(lines) - budget
	}
	if start < 0 {
		start = 0
	}
	return lines[start : start+budget]
}

// clip truncates s to w display columns, rune-safe, appending "…" when cut.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// Cut on runes; lipgloss.Width accounts for wide runes but plain slicing is
	// adequate for our ASCII-dominant content.
	r := []rune(s)
	if w <= 1 {
		return string(r[:w])
	}
	out := string(r[:0])
	width := 0
	for _, c := range r {
		if width+1 >= w {
			break
		}
		out += string(c)
		width++
	}
	return out + "…"
}
