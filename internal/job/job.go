package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
)

// State is a job's lifecycle state. Terminal turn states (done, needs-input,
// blocked, failed) mean the agent turn ended; the job remains resumable until
// closed.
type State string

const (
	StateQueued      State = "queued"
	StateActive      State = "active"
	StateDone        State = "done"
	StateNeedsInput  State = "needs-input"
	StateBlocked     State = "blocked"
	StateFailed      State = "failed"
	StateAuthNeeded  State = "auth-required"
	StateInterrupted State = "interrupted"
	StateClosed      State = "closed"
)

// Meta is the persisted job record (meta.json in the job dir).
type Meta struct {
	ID        string `json:"id"`
	Run       string `json:"run,omitempty"`
	Agent     string `json:"agent"`
	Task      string `json:"task"`
	Dir       string `json:"dir,omitempty"`       // in-place target; empty = scratch
	Workspace string `json:"workspace,omitempty"` // workspace ID when attached
	// Dispatch options, persisted so every turn — including resumed ones —
	// runs with the same contract as the first (rules additions, access
	// mode, wall clock). The runner reads these; never plumb them via env.
	AppendPrompt string `json:"append_prompt,omitempty"`
	ReadOnly     bool   `json:"read_only,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	// InitialTask preserves the dispatch prompt once resume/answer
	// overwrite Task with a follow-up message. Empty until first resume.
	InitialTask string `json:"initial_task,omitempty"`
	State       State  `json:"state"`
	// Closed is set when the job transitions to closed (via workspace close or
	// gc). It anchors gc's transcript-retention clock.
	Closed    time.Time `json:"closed,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	RunnerPID int       `json:"runner_pid,omitempty"`
	Model     string    `json:"model,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	// Question set when State == needs-input.
	Question string `json:"question,omitempty"`
	// Result is the final status-block-stripped output of the last turn.
	Result string `json:"result,omitempty"`
	// Telemetry, cumulative across turns.
	CostUSD   float64 `json:"cost_usd,omitempty"`
	Turns     int     `json:"turns,omitempty"`
	TokensIn  int     `json:"tokens_in,omitempty"`
	TokensOut int     `json:"tokens_out,omitempty"`
	// Context is the session's footprint after the last turn (not
	// cumulative): the health metric for spotting spinning workers.
	Context int `json:"context,omitempty"`
}

// ContextHigh reports whether the last turn's window crossed the health
// threshold — the cue to start a fresh job instead of resuming. threshold<=0
// disables the signal.
func (m *Meta) ContextHigh(threshold int) bool {
	return threshold > 0 && m.Context >= threshold
}

// Store manages the state directory layout:
//
//	<root>/jobs/<id>/meta.json
//	<root>/jobs/<id>/events.jsonl
//	<root>/jobs/<id>/transcript.jsonl
//	<root>/jobs/<id>/artifacts/
type Store struct{ Root string }

func DefaultRoot() string {
	if s := os.Getenv("LEGWORK_STATE_DIR"); s != "" {
		return s
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "legwork")
}

func OpenStore() (*Store, error) {
	root := DefaultRoot()
	if err := os.MkdirAll(filepath.Join(root, "jobs"), 0o700); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) JobDir(id string) string { return filepath.Join(s.Root, "jobs", id) }

// RunEventsPath is the run-level event log: job lifecycle markers plus
// orchestrator narration (legwork note). A run is a label, zero semantics.
func (s *Store) RunEventsPath(label string) (string, error) {
	dir := filepath.Join(s.Root, "runs", label)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.jsonl"), nil
}

// Counters persists the high-water mark of every ID sequence — the highest ID
// ever *allocated*, not merely the highest still on disk. Without it, deleting
// the top dir (which gc can now do) would make scan-max+1 hand out a reused ID,
// colliding two different jobs across event logs and notifier history.
type Counters struct {
	Job int `json:"job"`
	Ws  int `json:"ws"`
}

func countersPath(root string) string { return filepath.Join(root, "counters.json") }

func loadCounters(root string) Counters {
	var c Counters
	data, err := os.ReadFile(countersPath(root))
	if err != nil {
		return c // absent (legacy state dir) -> zero; scanMax carries it
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func saveCounters(root string, c Counters) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := countersPath(root)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AllocID allocates the next ID for a sequence (kind ∈ {"job","ws"}) under the
// shared allocation lock and persists the bumped counter. It does NOT create
// the directory — the caller reserves its own dir shape. Because the persisted
// counter, not the on-disk scan, is the source of truth, releasing the lock
// before the caller mkdirs is safe: a subsequent alloc reads the counter and
// never reuses the number even if the caller's dir doesn't exist yet.
//
// next = max(scanMax, counter) + 1 is self-healing: it works with no counter
// file (legacy dirs — scanMax carries the value, and the first alloc writes the
// file), and never reuses a live dir even if counters.json is lost.
func AllocID(root, kind string) (int, error) {
	var subdir, prefix string
	switch kind {
	case "job":
		subdir, prefix = "jobs", "job-%d"
	case "ws":
		subdir, prefix = "workspaces", "ws-%d"
	default:
		return 0, fmt.Errorf("unknown id kind %q", kind)
	}
	unlock, err := LockAlloc(root)
	if err != nil {
		return 0, err
	}
	defer unlock()

	entries, err := os.ReadDir(filepath.Join(root, subdir))
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), prefix, &n); err == nil && n > max {
			max = n
		}
	}
	c := loadCounters(root)
	cur := c.Job
	if kind == "ws" {
		cur = c.Ws
	}
	if cur > max {
		max = cur
	}
	next := max + 1
	if kind == "ws" {
		c.Ws = next
	} else {
		c.Job = next
	}
	if err := saveCounters(root, c); err != nil {
		return 0, err
	}
	return next, nil
}

// NewID allocates the next sequential job id (job-1, job-2, ...) and reserves
// its directory. The persisted counter (not scan-max) guarantees IDs are never
// reused after a gc deletion.
func (s *Store) NewID() (string, error) {
	n, err := AllocID(s.Root, "job")
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("job-%d", n)
	if err := os.Mkdir(filepath.Join(s.Root, "jobs", id), 0o700); err != nil {
		return "", err
	}
	return id, nil
}

// LockAlloc takes the state-wide allocation lock (shared by jobs and
// workspaces). The returned func releases it.
func LockAlloc(root string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(root, ".alloc.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Create initializes a job dir and persists initial meta.
func (s *Store) Create(m *Meta) error {
	dir := s.JobDir(m.ID)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o700); err != nil {
		return err
	}
	m.Created = time.Now().UTC()
	m.Updated = m.Created
	return s.SaveMeta(m)
}

// SaveMeta atomically persists meta.json.
func (s *Store) SaveMeta(m *Meta) error {
	m.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.JobDir(m.ID), "meta.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) LoadMeta(id string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.JobDir(id), "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("job %s: %w", id, err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns all job metas, oldest first.
func (s *Store) List() ([]*Meta, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "jobs"))
	if err != nil {
		return nil, err
	}
	var out []*Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.LoadMeta(e.Name())
		if err != nil {
			continue // half-created dirs are skipped, not fatal
		}
		out = append(out, m)
	}
	return out, nil
}

// Reconcile flips a stale active job (state==active but the runner PID is
// gone) to interrupted, persists it, and records an interrupted event.
// Returns true if it changed anything. Interrupted jobs are resumable and are
// never deleted by gc. This is the single source of truth for the reconcile
// step, shared by the CLI read paths and gc.
func (s *Store) Reconcile(m *Meta) bool {
	if m.State != StateActive || s.Alive(m) {
		return false
	}
	m.State = StateInterrupted
	m.RunnerPID = 0
	_ = s.SaveMeta(m)
	if log, err := events.Open(filepath.Join(s.JobDir(m.ID), "events.jsonl")); err == nil {
		_, _ = log.Append(events.Event{Type: events.TypeInterrupted, Actor: "runner",
			Preview: "runner died without finishing the turn"})
	}
	return true
}

// CloseJobsForWorkspace marks every non-closed job attached to wsID as closed
// and stamps Closed (the retention clock anchor). Used when a workspace closes,
// so its whole job lineage is acknowledged in one place.
func (s *Store) CloseJobsForWorkspace(wsID string) error {
	metas, err := s.List()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, jm := range metas {
		if jm.Workspace == wsID && jm.State != StateClosed {
			jm.State = StateClosed
			jm.Closed = now
			if err := s.SaveMeta(jm); err != nil {
				return err
			}
		}
	}
	return nil
}

// Alive reports whether the job's runner process is still running, and
// reconciles a stale "active" state to interrupted.
func (s *Store) Alive(m *Meta) bool {
	if m.State != StateActive || m.RunnerPID == 0 {
		return false
	}
	// Signal 0: existence check.
	return syscall.Kill(m.RunnerPID, syscall.Signal(0)) == nil
}
