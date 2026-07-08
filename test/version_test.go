package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	e := newEnv(t)
	out := e.legwork(t, "version", "--json")
	var v struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Dirty   bool   `json:"dirty"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("bad version json: %v\n%s", err, out)
	}
	if v.Version == "" {
		t.Fatalf("missing version:\n%s", out)
	}

	text := e.legwork(t, "version")
	for _, want := range []string{"version:", "commit:", "dirty:", "date:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("version output missing %q:\n%s", want, text)
		}
	}
}
