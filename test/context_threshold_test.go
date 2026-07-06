package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resultWithContext yields a 145200-token context (cache_read 140000 +
// cache_creation 5000 + input 200). With a [health] threshold of 100000 it
// trips the marker; against the built-in 150000 default it stays healthy.

func TestContextThresholdMarker(t *testing.T) {
	e := newEnv(t)
	e.config = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(e.config, []byte("[health]\ncontext_threshold = 100000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.writeScript(t, resultWithContext)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "high-ctx"))
	e.waitState(t, id, "done")

	if ls := e.legwork(t, "ls"); !strings.Contains(ls, "ctx:145k!") {
		t.Fatalf("ls missing ctx:145k! marker:\n%s", ls)
	}

	status := e.legwork(t, "status", id)
	if !strings.Contains(status, "hint:") || !strings.Contains(status, "context high") {
		t.Fatalf("status missing hint line:\n%s", status)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "status", id, "--json")), &m); err != nil {
		t.Fatal(err)
	}
	if m["context_high"] != true {
		t.Fatalf("context_high = %v, want true", m["context_high"])
	}
}

func TestContextThresholdHealthy(t *testing.T) {
	e := newEnv(t) // no [health] table -> 150000 default; 145k stays below

	e.writeScript(t, resultWithContext)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "healthy-ctx"))
	e.waitState(t, id, "done")

	if ls := e.legwork(t, "ls"); strings.Contains(ls, "!") {
		t.Fatalf("ls should have no marker at default threshold:\n%s", ls)
	}

	if status := e.legwork(t, "status", id); strings.Contains(status, "hint:") {
		t.Fatalf("status should have no hint at default threshold:\n%s", status)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "status", id, "--json")), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["context_high"]; ok {
		t.Fatalf("context_high should be absent when healthy:\n%v", m)
	}
}
