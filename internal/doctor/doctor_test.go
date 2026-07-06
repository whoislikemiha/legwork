package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whoislikemiha/legwork/internal/adapter"
)

func fakeAdapter(t *testing.T) adapter.Adapter {
	t.Helper()
	ad, err := adapter.New("fake")
	if err != nil {
		t.Fatal(err)
	}
	return ad
}

func TestCheckStateDirOK(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEGWORK_STATE_DIR", dir)
	c := checkStateDir()
	if c.Status != StatusOK {
		t.Fatalf("want ok, got %s (%s)", c.Status, c.Detail)
	}
	// The probe file must not linger.
	if _, err := os.Stat(filepath.Join(dir, ".doctor-probe")); !os.IsNotExist(err) {
		t.Fatalf("probe file not cleaned up")
	}
}

func TestCheckWorkstree(t *testing.T) {
	t.Run("no worktree.toml skips", func(t *testing.T) {
		c := checkWorkstree(t.TempDir())
		if c.Status != StatusSkip {
			t.Fatalf("want skip, got %s (%s)", c.Status, c.Detail)
		}
	})

	t.Run("toml present but workstree missing warns", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "worktree.toml"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Empty PATH so workstree cannot be found.
		t.Setenv("PATH", "")
		c := checkWorkstree(dir)
		if c.Status != StatusWarn {
			t.Fatalf("want warn, got %s (%s)", c.Status, c.Detail)
		}
	})

	t.Run("toml present and workstree on PATH is ok", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "worktree.toml"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		binDir := t.TempDir()
		stub := filepath.Join(binDir, "workstree")
		if err := os.WriteFile(stub, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir)
		c := checkWorkstree(dir)
		if c.Status != StatusOK {
			t.Fatalf("want ok, got %s (%s)", c.Status, c.Detail)
		}
	})
}

func writeNotifyConfig(t *testing.T, command string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	body := ""
	if command != "" {
		body = "[notify]\ncommand = \"" + command + "\"\n"
	}
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGWORK_CONFIG", cfg)
}

func TestCheckNotifier(t *testing.T) {
	ad := fakeAdapter(t)

	t.Run("no command skips", func(t *testing.T) {
		writeNotifyConfig(t, "")
		c := checkNotifier(ad)
		if c.Status != StatusSkip {
			t.Fatalf("want skip, got %s (%s)", c.Status, c.Detail)
		}
	})

	t.Run("command exit 0 is ok", func(t *testing.T) {
		writeNotifyConfig(t, "exit 0")
		c := checkNotifier(ad)
		if c.Status != StatusOK {
			t.Fatalf("want ok, got %s (%s)", c.Status, c.Detail)
		}
	})

	t.Run("command exit 1 fails", func(t *testing.T) {
		writeNotifyConfig(t, "exit 1")
		c := checkNotifier(ad)
		if c.Status != StatusFail {
			t.Fatalf("want fail, got %s (%s)", c.Status, c.Detail)
		}
	})

	t.Run("payload reaches the command on stdin", func(t *testing.T) {
		sink := filepath.Join(t.TempDir(), "payload.json")
		writeNotifyConfig(t, "cat > "+sink)
		c := checkNotifier(ad)
		if c.Status != StatusOK {
			t.Fatalf("want ok, got %s (%s)", c.Status, c.Detail)
		}
		data, err := os.ReadFile(sink)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(data); !strings.Contains(got, `"event":"doctor"`) {
			t.Fatalf("payload missing doctor event: %s", got)
		}
	})
}

func TestAllOK(t *testing.T) {
	cases := []struct {
		name     string
		statuses []Status
		want     bool
	}{
		{"all ok", []Status{StatusOK, StatusOK}, true},
		{"warn and skip allowed", []Status{StatusOK, StatusWarn, StatusSkip}, true},
		{"a fail flips it", []Status{StatusOK, StatusFail, StatusWarn}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var checks []Check
			for _, s := range tc.statuses {
				checks = append(checks, Check{Status: s})
			}
			if got := allOK(checks); got != tc.want {
				t.Fatalf("allOK=%v, want %v", got, tc.want)
			}
		})
	}
}
