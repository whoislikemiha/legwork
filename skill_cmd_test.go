package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSkillInstallAllUsesCanonicalEmbeddedSkill(t *testing.T) {
	home := tempSkillHome(t)

	report, err := installLegworkSkill(skillInstallOptions{
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
		if string(got) != legworkSkillText {
			t.Fatalf("%s did not receive the exact embedded skill", rel)
		}
	}
}

func TestSkillInstallIdenticalContentIsNoop(t *testing.T) {
	home := tempSkillHome(t)
	path := filepath.Join(home, ".claude", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legworkSkillText), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := installLegworkSkill(skillInstallOptions{Target: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || len(report.Results) != 1 || report.Results[0].Status != skillStatusUnchanged {
		t.Fatalf("identical content should be unchanged: %+v", report)
	}
}

func TestSkillInstallConflictRequiresForce(t *testing.T) {
	home := tempSkillHome(t)
	path := filepath.Join(home, ".codex", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := installLegworkSkill(skillInstallOptions{Target: "codex"})
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

func TestSkillInstallForceBacksUpOutsideSkillPaths(t *testing.T) {
	home := tempSkillHome(t)
	path := filepath.Join(home, ".hermes", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := installLegworkSkill(skillInstallOptions{
		Target: "hermes",
		Force:  true,
		Now:    time.Date(2026, 7, 10, 12, 13, 14, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Results[0].Status != skillStatusReplaced {
		t.Fatalf("force should replace: %+v", report)
	}
	if got := mustReadString(t, path); got != legworkSkillText {
		t.Fatalf("force did not install embedded skill")
	}
	backup := report.Results[0].Backup
	if !strings.Contains(backup, filepath.Join(".local", "share", "legwork", "skill-backups", "hermes")) {
		t.Fatalf("backup should be outside harness skill paths: %s", backup)
	}
	if got := mustReadString(t, backup); got != "local edit\n" {
		t.Fatalf("backup mismatch: %q", got)
	}
}

func TestSkillInstallCommandJSONConflictUsesStableError(t *testing.T) {
	home := tempSkillHome(t)
	path := filepath.Join(home, ".claude", "skills", "legwork", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := rootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected conflict")
	}
	if ce, ok := err.(interface{ ExitCode() int }); !ok || ce.ExitCode() != 1 {
		t.Fatalf("expected exit code 1 conflict, got %T %v", err, err)
	}
	var report skillInstallReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out.String())
	}
	if report.OK || report.Error == nil || report.Error.Code != "skill-conflict" {
		t.Fatalf("bad conflict json: %+v", report)
	}
}

func TestSkillInstallCommandJSONUsageError(t *testing.T) {
	tempSkillHome(t)

	cmd := rootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "vim", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected usage error")
	}
	if ce, ok := err.(interface{ ExitCode() int }); !ok || ce.ExitCode() != 2 {
		t.Fatalf("expected exit code 2 usage error, got %T %v", err, err)
	}
	var report skillInstallReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out.String())
	}
	if report.OK || report.Error == nil || report.Error.Code != "usage" {
		t.Fatalf("bad usage json: %+v", report)
	}
}

func tempSkillHome(t *testing.T) string {
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
