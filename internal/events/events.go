package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// SchemaVersion stamps every event line; the event index is a stable public
// interface and must survive format evolution.
const SchemaVersion = 2

// Event is one line of a job's or run's events.jsonl — the compact index.
// Full payloads live in transcript.jsonl; Preview is truncated on purpose.
type Event struct {
	V       int       `json:"v"`
	Seq     int       `json:"seq"`
	Time    time.Time `json:"ts"`
	Type    string    `json:"type"`
	Actor   string    `json:"actor,omitempty"` // main | subagent:<id> | orchestrator | human | runner
	Preview string    `json:"preview,omitempty"`
	// Fields is small structured extra data (tool name, file, cost...).
	Fields map[string]any `json:"fields,omitempty"`
}

// Event types (families per DESIGN.md §3).
const (
	TypeQueued         = "queued"
	TypeStarted        = "started"
	TypeFinished       = "finished" // fields.state = done|needs-input|blocked|failed|auth-required; v2 may include fields.blocked
	TypeInterrupted    = "interrupted"
	TypeClosed         = "closed"
	TypeNeedsInput     = "needs-input"
	TypeNeedsProvision = "needs-provision"
	TypeAnswer         = "answer"
	TypeApprove        = "approve"
	TypeResume         = "resume"
	TypeCancel         = "cancel"
	TypeText           = "text"      // assistant text (preview)
	TypeToolCall       = "tool-call" // fields: tool, target
	TypeProgress       = "progress"
	TypeUsage          = "usage"      // fields: cost_usd, tokens_in, tokens_out, context
	TypeNote           = "note"       // orchestrator narration
	TypeArtifact       = "artifact"   // fields: name, size_bytes
	TypeCommit         = "commit"     // orchestrator-owned workspace commit
	TypeCheckpoint     = "checkpoint" // fields: ref, oid
)

const previewMax = 200

// Truncate shortens s to the index preview budget, never splitting a rune —
// a byte-sliced multibyte character would put invalid UTF-8 in every preview
// surface downstream.
func Truncate(s string) string {
	rs := []rune(s)
	if len(rs) <= previewMax {
		return s
	}
	return string(rs[:previewMax]) + "…"
}

// Log appends events to one events.jsonl. Appends are O_APPEND single writes
// (atomic for our line sizes on POSIX).
type Log struct {
	path string
	seq  int
}

func Open(path string) (*Log, error) {
	l := &Log{path: path}
	// Recover seq from the last line, if any.
	evs, err := Read(path, 0)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if n := len(evs); n > 0 {
		l.seq = evs[n-1].Seq
	}
	return l, nil
}

// Append writes one event, assigning seq and timestamp.
func (l *Log) Append(e Event) (Event, error) {
	l.seq++
	e.V = SchemaVersion
	e.Seq = l.seq
	e.Time = time.Now().UTC()
	if len(e.Preview) > previewMax+3 {
		e.Preview = Truncate(e.Preview)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return e, err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return e, err
	}
	return e, nil
}

// Read returns events with Seq > since (cursor semantics for --since).
func Read(path string, since int) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			// A torn/corrupt line must not hide the rest of the log.
			continue
		}
		if e.Seq > since {
			out = append(out, e)
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("reading %s: %w", path, err)
	}
	return out, nil
}
