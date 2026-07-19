package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whoislikemiha/legwork/internal/notify"
)

func TestPrepareJobTempAndEnv(t *testing.T) {
	dir := t.TempDir()
	tmp, err := prepareJobTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{tmp.Root, tmp.GoCache, tmp.GoMod, tmp.GoTmp} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("temp path missing: %s err=%v", path, err)
		}
	}
	if tmp.Root != filepath.Join(dir, "tmp") {
		t.Fatalf("root = %q", tmp.Root)
	}

	env := tmp.ApplyEnv([]string{"TMPDIR=/old", "PATH=/bin", "GOCACHE=/old-cache"}, "codex")
	want := map[string]string{
		"TMPDIR":     tmp.Root,
		"GOCACHE":    tmp.GoCache,
		"GOMODCACHE": tmp.GoMod,
		"GOTMPDIR":   tmp.GoTmp,
		"PATH":       "/bin",
	}
	for k, v := range want {
		if got := envValue(env, k); got != v {
			t.Fatalf("%s = %q, want %q in %#v", k, got, v, env)
		}
	}

	claudeEnv := tmp.ApplyEnv([]string{"PATH=/bin"}, "claude")
	if got := envValue(claudeEnv, "TMPDIR"); got != tmp.Root {
		t.Fatalf("claude TMPDIR = %q, want %q", got, tmp.Root)
	}
	if got := envValue(claudeEnv, "GOCACHE"); got != "" {
		t.Fatalf("claude should not get codex Go cache env, got %q", got)
	}
}

func TestWorkerEnvironmentScrubsNotifierOrigin(t *testing.T) {
	dir := t.TempDir()
	origin, err := notify.CaptureOrigin([]string{"ROUTE", "ABSENT"}, []string{"ROUTE=session-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := notify.SaveOrigin(dir, origin); err != nil {
		t.Fatal(err)
	}
	cfg := &notify.Config{}
	cfg.Notify.CaptureEnv = []string{"ROUTE", "ABSENT"}
	names, err := cfg.JobScrubNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := notify.ScrubEnvironment([]string{"ROUTE=session-b", "ABSENT=session-b", "UNRELATED=kept"}, names)
	if envValue(got, "ROUTE") != "" || envValue(got, "ABSENT") != "" {
		t.Fatalf("captured names reached worker: %#v", got)
	}
	if envValue(got, "UNRELATED") != "kept" {
		t.Fatalf("unrelated variable removed: %#v", got)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if len(kv) >= len(prefix) && kv[:len(prefix)] == prefix {
			return kv[len(prefix):]
		}
	}
	return ""
}
