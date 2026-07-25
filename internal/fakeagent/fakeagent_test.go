package fakeagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayWritesTempSidecar(t *testing.T) {
	script := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(script, []byte(
		"#write-temp nested/usage.json {\"completed\":true}\nstate: done\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	t.Setenv(ScriptEnv, script)
	t.Setenv(TempDirEnv, tempDir)

	var stdout strings.Builder
	if err := Replay(&stdout); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tempDir, "nested", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != `{"completed":true}` {
		t.Fatalf("sidecar = %q", got)
	}
	if got := strings.TrimSpace(stdout.String()); got != "state: done" {
		t.Fatalf("stdout = %q", got)
	}
}
