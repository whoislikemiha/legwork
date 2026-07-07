// Package serve implements legwork's read-only local browser surface.
package serve

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/timeline"
)

const defaultTimelineLimit = 80

// Snapshot is the JSON envelope rendered by the browser shell. It is deliberately
// read-model shaped: enough for the first v1 screen without exposing mutation
// affordances or inventing a second persistence layer.
type Snapshot struct {
	GeneratedAt time.Time      `json:"generated_at"`
	StateDir    string         `json:"state_dir"`
	ReadOnly    bool           `json:"read_only"`
	LocalOnly   bool           `json:"local_only"`
	Health      HealthSummary  `json:"health"`
	Runs        []RunSummary   `json:"runs"`
	Jobs        []JobSummary   `json:"jobs"`
	Attention   []Attention    `json:"attention"`
	Timeline    []TimelineItem `json:"timeline"`
	Selected    *JobDetail     `json:"selected,omitempty"`
}

type HealthSummary struct {
	ContextThreshold int `json:"context_threshold"`
}

type RunSummary struct {
	Label       string         `json:"label"`
	Display     string         `json:"display"`
	Jobs        map[string]int `json:"jobs"`
	State       string         `json:"state"`
	CostUSD     float64        `json:"cost_usd"`
	ContextHigh bool           `json:"context_high"`
	Updated     time.Time      `json:"updated"`
	LastNote    string         `json:"last_note,omitempty"`
}

type JobSummary struct {
	ID          string    `json:"id"`
	Run         string    `json:"run"`
	Agent       string    `json:"agent"`
	Model       string    `json:"model,omitempty"`
	Task        string    `json:"task"`
	Workspace   string    `json:"workspace,omitempty"`
	State       string    `json:"state"`
	Question    string    `json:"question,omitempty"`
	Result      string    `json:"result,omitempty"`
	CostUSD     float64   `json:"cost_usd"`
	Turns       int       `json:"turns"`
	Context     int       `json:"context"`
	ContextHigh bool      `json:"context_high"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

type Attention struct {
	JobID    string    `json:"job_id"`
	Run      string    `json:"run"`
	State    string    `json:"state"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	Updated  time.Time `json:"updated"`
}

type TimelineItem struct {
	JobID   string         `json:"job_id,omitempty"`
	Run     string         `json:"run,omitempty"`
	Type    string         `json:"type"`
	Actor   string         `json:"actor,omitempty"`
	Preview string         `json:"preview,omitempty"`
	Time    time.Time      `json:"ts"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type JobDetail struct {
	JobSummary
	Events []TimelineItem `json:"events"`
}

type server struct {
	store     *job.Store
	threshold int
	localOnly bool
}

type Options struct {
	ContextThreshold int
	LocalOnly        bool
}

// NewHandler returns the complete read-only HTTP surface.
func NewHandler(store *job.Store, contextThreshold int) http.Handler {
	return NewHandlerWithOptions(store, Options{ContextThreshold: contextThreshold, LocalOnly: true})
}

func NewHandlerWithOptions(store *job.Store, opts Options) http.Handler {
	s := &server{store: store, threshold: opts.ContextThreshold, localOnly: opts.LocalOnly}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/api/snapshot", s.snapshot)
	mux.HandleFunc("/events", s.events)
	return readOnly(mux)
}

func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "legwork serve is read-only", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *server) snapshot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/snapshot" {
		http.NotFound(w, r)
		return
	}
	snap, err := buildSnapshot(s.store, s.threshold, r.URL.Query().Get("job"), s.localOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snap)
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/events" {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeSSE(w, "snapshot", "initial")
	flusher.Flush()

	tl := timeline.New(timeline.ScopeAll(s.store))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			items, err := tl.Poll()
			if err != nil {
				writeSSE(w, "error", err.Error())
				flusher.Flush()
				continue
			}
			if len(items) == 0 {
				writeSSE(w, "heartbeat", time.Now().UTC().Format(time.RFC3339Nano))
			} else {
				writeSSE(w, "snapshot", fmt.Sprintf("%d events", len(items)))
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// BuildSnapshot reads the existing file-backed state and derives the dashboard
// model without mutating it. `serve` is a browser observability surface, not a
// CLI/status reconciliation pass; opening it must not write meta.json or events.
func BuildSnapshot(store *job.Store, threshold int, selectedID string) (Snapshot, error) {
	return buildSnapshot(store, threshold, selectedID, true)
}

func buildSnapshot(store *job.Store, threshold int, selectedID string, localOnly bool) (Snapshot, error) {
	metas, err := store.List()
	if err != nil {
		return Snapshot{}, err
	}
	sortMetas(metas)

	runLogs, err := timeline.RunLogs(store)
	if err != nil {
		return Snapshot{}, err
	}
	rollups := timeline.Rollups(metas, runLogs, threshold)

	jobs := make([]JobSummary, 0, len(metas))
	attention := make([]Attention, 0)
	for _, m := range metas {
		jobs = append(jobs, summarizeJob(m, threshold))
		attention = appendAttention(attention, m, threshold)
	}
	sort.SliceStable(attention, func(i, j int) bool {
		if severityRank(attention[i].Severity) != severityRank(attention[j].Severity) {
			return severityRank(attention[i].Severity) < severityRank(attention[j].Severity)
		}
		return attention[i].Updated.After(attention[j].Updated)
	})

	runSummaries := make([]RunSummary, 0, len(rollups))
	for _, r := range rollups {
		runSummaries = append(runSummaries, RunSummary{
			Label:       r.Label,
			Display:     displayRun(r.Label),
			Jobs:        r.Jobs,
			State:       timeline.StateSummary(r.Jobs),
			CostUSD:     r.CostUSD,
			ContextHigh: r.ContextHigh,
			Updated:     r.Updated,
			LastNote:    r.LastNote,
		})
	}

	items, err := timeline.New(timeline.ScopeAll(store)).Poll()
	if err != nil {
		return Snapshot{}, err
	}
	items = timeline.Curated(items)
	timelineItems := latestTimelineItems(items, defaultTimelineLimit)

	selected := selectJobID(selectedID, metas, attention)
	var detail *JobDetail
	if selected != "" {
		if m := findMeta(metas, selected); m != nil {
			evs, _ := timeline.New(timeline.ScopeJob(store, selected)).Poll()
			evs = timeline.Curated(evs)
			detail = &JobDetail{
				JobSummary: summarizeJob(m, threshold),
				Events:     latestTimelineItems(evs, 50),
			}
		}
	}

	if runSummaries == nil {
		runSummaries = []RunSummary{}
	}
	if jobs == nil {
		jobs = []JobSummary{}
	}
	if attention == nil {
		attention = []Attention{}
	}
	if timelineItems == nil {
		timelineItems = []TimelineItem{}
	}
	return Snapshot{
		GeneratedAt: time.Now().UTC(),
		StateDir:    store.Root,
		ReadOnly:    true,
		LocalOnly:   localOnly,
		Health:      HealthSummary{ContextThreshold: threshold},
		Runs:        runSummaries,
		Jobs:        jobs,
		Attention:   attention,
		Timeline:    timelineItems,
		Selected:    detail,
	}, nil
}

func summarizeJob(m *job.Meta, threshold int) JobSummary {
	return JobSummary{
		ID:          m.ID,
		Run:         m.Run,
		Agent:       m.Agent,
		Model:       m.Model,
		Task:        m.Task,
		Workspace:   m.Workspace,
		State:       string(m.State),
		Question:    m.Question,
		Result:      m.Result,
		CostUSD:     m.CostUSD,
		Turns:       m.Turns,
		Context:     m.Context,
		ContextHigh: m.ContextHigh(threshold),
		Created:     m.Created,
		Updated:     m.Updated,
	}
}

func appendAttention(out []Attention, m *job.Meta, threshold int) []Attention {
	switch m.State {
	case job.StateNeedsInput:
		msg := m.Question
		if msg == "" {
			msg = "worker needs input"
		}
		return append(out, Attention{JobID: m.ID, Run: m.Run, State: string(m.State), Severity: "urgent", Message: msg, Updated: m.Updated})
	case job.StateBlocked, job.StateFailed, job.StateAuthNeeded:
		msg := firstNonEmpty(m.Result, string(m.State))
		return append(out, Attention{JobID: m.ID, Run: m.Run, State: string(m.State), Severity: "urgent", Message: msg, Updated: m.Updated})
	case job.StateInterrupted:
		return append(out, Attention{JobID: m.ID, Run: m.Run, State: string(m.State), Severity: "warn", Message: "runner died mid-turn; session is resumable", Updated: m.Updated})
	}
	if m.ContextHigh(threshold) && m.State != job.StateClosed {
		return append(out, Attention{JobID: m.ID, Run: m.Run, State: "context-high", Severity: "warn", Message: fmt.Sprintf("context at %s", formatContext(m.Context)), Updated: m.Updated})
	}
	return out
}

func latestTimelineItems(items []timeline.Item, limit int) []TimelineItem {
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	out := make([]TimelineItem, 0, len(items))
	for _, it := range items {
		out = append(out, TimelineItem{
			JobID:   it.JobID,
			Run:     it.Run,
			Type:    it.Event.Type,
			Actor:   it.Event.Actor,
			Preview: it.Event.Preview,
			Time:    it.Event.Time,
			Fields:  it.Event.Fields,
		})
	}
	return out
}

func selectJobID(want string, metas []*job.Meta, attention []Attention) string {
	if want != "" && findMeta(metas, want) != nil {
		return want
	}
	if len(attention) > 0 {
		return attention[0].JobID
	}
	if len(metas) > 0 {
		return metas[0].ID
	}
	return ""
}

func findMeta(metas []*job.Meta, id string) *job.Meta {
	for _, m := range metas {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func sortMetas(ms []*job.Meta) {
	sort.SliceStable(ms, func(i, j int) bool {
		if stateRank(ms[i].State) != stateRank(ms[j].State) {
			return stateRank(ms[i].State) < stateRank(ms[j].State)
		}
		if !ms[i].Updated.Equal(ms[j].Updated) {
			return ms[i].Updated.After(ms[j].Updated)
		}
		return idLess(ms[i].ID, ms[j].ID)
	})
}

func stateRank(s job.State) int {
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

func severityRank(s string) int {
	switch s {
	case "urgent":
		return 0
	case "warn":
		return 1
	default:
		return 2
	}
}

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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func displayRun(label string) string {
	if label == "" {
		return "(no run)"
	}
	return label
}

func formatContext(n int) string {
	if n <= 0 {
		return "-"
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dk", n/1000)
}

// ValidateAddr enforces the v1 safety default: no remote browser exposure unless
// the caller explicitly opts in with the CLI flag documented as unsafe for shared
// networks.
func ValidateAddr(addr string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--addr must be host:port: %w", err)
	}
	if allowRemote {
		return nil
	}
	if host == "" {
		return fmt.Errorf("--addr %q binds all interfaces; use 127.0.0.1:PORT or pass --allow-remote", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("--addr host %q is not a loopback address; pass --allow-remote to expose remotely", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("--addr host %q is not loopback; pass --allow-remote to expose remotely", host)
	}
	return nil
}

// URL returns the browser URL for a listener.
func URL(addr net.Addr) string {
	if tcp, ok := addr.(*net.TCPAddr); ok {
		host := tcp.IP.String()
		if host == "<nil>" || host == "" {
			host = "127.0.0.1"
		}
		return "http://" + net.JoinHostPort(host, fmt.Sprint(tcp.Port))
	}
	return "http://" + addr.String()
}
