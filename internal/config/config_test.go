package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHealthDefault(t *testing.T) {
	// Point at a nonexistent file: a missing config yields defaults.
	t.Setenv("LEGWORK_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	h, err := LoadHealth()
	if err != nil {
		t.Fatal(err)
	}
	if h.ContextThreshold != 150000 {
		t.Fatalf("default threshold = %d, want 150000", h.ContextThreshold)
	}
}

func TestLoadHealthOverride(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{"[health]\ncontext_threshold = 280000\n", 280000},
		{"[health]\ncontext_threshold = 0\n", 0}, // 0 disables the marker
	} {
		p := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("LEGWORK_CONFIG", p)
		h, err := LoadHealth()
		if err != nil {
			t.Fatal(err)
		}
		if h.ContextThreshold != tc.want {
			t.Fatalf("threshold = %d, want %d (body %q)", h.ContextThreshold, tc.want, tc.body)
		}
	}
}
