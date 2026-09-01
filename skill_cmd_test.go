package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/whoislikemiha/legwork/internal/skill"
)

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
	var report skill.Report
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
	var report skill.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out.String())
	}
	if report.OK || report.Error == nil || report.Error.Code != "usage" {
		t.Fatalf("bad usage json: %+v", report)
	}
}

func mustReadString(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func tempSkillHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	return home
}
