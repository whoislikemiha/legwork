package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
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
	ID        string    `json:"id"`
	Run       string    `json:"run,omitempty"`
	Agent     string    `json:"agent"`
	Task      string    `json:"task"`
	Dir       string    `json:"dir,omitempty"`       // in-place target; empty = scratch
	Workspace string    `json:"workspace,omitempty"` // workspace ID when attached
	State     State     `json:"state"`
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

// NewID allocates the next sequential job id (job-1, job-2, ...).
func (s *Store) NewID() (string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "jobs"))
	if err != nil {
		return "", err
	}
	max := 0
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "job-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("job-%d", max+1), nil
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

// Alive reports whether the job's runner process is still running, and
// reconciles a stale "active" state to interrupted.
func (s *Store) Alive(m *Meta) bool {
	if m.State != StateActive || m.RunnerPID == 0 {
		return false
	}
	// Signal 0: existence check.
	return syscall.Kill(m.RunnerPID, syscall.Signal(0)) == nil
}
