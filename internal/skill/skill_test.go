package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var canonical = []byte("canonical skill content\n")

func TestInstallAllWritesCanonicalContent(t *testing.T) {
	home := tempHome(t)

	report, err := Install(canonical, Options{
		Target: "all",
		Now:    time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Results) != 3 {
		t.Fatalf("bad report: %+v", report)
	}
	for _, rel := range []string{
		".hermes/skills/legwork/SKILL.md",
		".claude/skills/legwork/SKILL.md",
		".codex/skills/legwork/SKILL.md",
	} {
		got, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(canonical) {
			t.Fatalf("%s did not receive the exact canonical skill", rel)
		}
	}
}

func TestInstallIdenticalContentIsNoop(t *testing.T) {
	home := tempHome(t)
	path := filepath.Join(home, ".claude", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, canonical, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Install(canonical, Options{Target: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Results) != 1 || report.Results[0].Status != StatusUnchanged {
		t.Fatalf("identical content should be unchanged: %+v", report)
	}
}

func TestInstallConflictRequiresForce(t *testing.T) {
	home := tempHome(t)
	path := filepath.Join(home, ".codex", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Install(canonical, Options{Target: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Error == nil || report.Error.Code != "skill-conflict" {
		t.Fatalf("expected stable conflict: %+v", report)
	}
	if got := mustReadString(t, path); got != "local edit\n" {
		t.Fatalf("conflict overwrote file: %q", got)
	}
}

func TestInstallForceBacksUpOutsideSkillPaths(t *testing.T) {
	home := tempHome(t)
	path := filepath.Join(home, ".hermes", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Install(canonical, Options{
		Target: "hermes",
		Force:  true,
		Now:    time.Date(2026, 7, 10, 12, 13, 14, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Results[0].Status != StatusReplaced {
		t.Fatalf("force should replace: %+v", report)
	}
	if got := mustReadString(t, path); got != string(canonical) {
		t.Fatalf("force did not install canonical skill")
	}
	backup := report.Results[0].Backup
	if !strings.Contains(backup, filepath.Join(".local", "share", "legwork", "skill-backups", "hermes")) {
		t.Fatalf("backup should be outside harness skill paths: %s", backup)
	}
	if got := mustReadString(t, backup); got != "local edit\n" {
		t.Fatalf("backup mismatch: %q", got)
	}
}

func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	return home
}

func mustReadString(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}
