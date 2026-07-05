package events

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendReadCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := l.Append(Event{Type: TypeProgress, Preview: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := Read(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 4 || evs[1].Seq != 5 {
		t.Fatalf("cursor read wrong: %+v", evs)
	}
	if evs[0].V != SchemaVersion {
		t.Fatal("events must be version-stamped")
	}

	// Reopen: seq continues, never restarts.
	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := l2.Append(Event{Type: TypeNote})
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 6 {
		t.Fatalf("seq after reopen = %d, want 6", e.Seq)
	}
}

func TestPreviewTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := Open(path)
	long := strings.Repeat("x", 5000)
	e, err := l.Append(Event{Type: TypeText, Preview: long})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Preview) > 250 {
		t.Fatalf("preview not truncated: %d bytes", len(e.Preview))
	}
}
