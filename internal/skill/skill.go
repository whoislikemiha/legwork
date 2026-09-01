// Package skill installs the embedded legwork orchestrator skill into agent
// harness skill directories (hermes, claude, codex). The canonical SKILL.md
// content is embedded by the CLI (go:embed cannot cross package roots) and
// passed in.
package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Report struct {
	OK      bool         `json:"ok"`
	Results []Result     `json:"results"`
	Error   *ReportError `json:"error,omitempty"`
}

type Result struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Backup string `json:"backup,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ReportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Options struct {
	Target string
	Force  bool
	Now    time.Time
}

type target struct {
	Name string
	Path string
}

const (
	StatusInstalled = "installed"
	StatusUnchanged = "unchanged"
	StatusReplaced  = "replaced"
	StatusConflict  = "conflict"
)

// Install writes content to every resolved target. A differing installed
// skill is a conflict unless opts.Force, which backs the old file up outside
// the harness skill paths first.
func Install(content []byte, opts Options) (Report, error) {
	targets, err := resolveTargets(opts.Target)
	if err != nil {
		return Report{}, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	report := Report{OK: true}
	for _, t := range targets {
		result := installTarget(t, content, opts.Force, opts.Now)
		if result.Status == StatusConflict {
			report.OK = false
		}
		report.Results = append(report.Results, result)
	}
	if !report.OK {
		report.Error = &ReportError{
			Code:    "skill-conflict",
			Message: "installed skill differs; rerun with --force to replace after backup",
		}
	}
	return report, nil
}

func resolveTargets(name string) ([]target, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	all := []target{
		{Name: "hermes", Path: filepath.Join(home, ".hermes", "skills", "legwork", "SKILL.md")},
		{Name: "claude", Path: filepath.Join(home, ".claude", "skills", "legwork", "SKILL.md")},
		{Name: "codex", Path: filepath.Join(codexHome(home), "skills", "legwork", "SKILL.md")},
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "all":
		return all, nil
	case "hermes":
		return all[:1], nil
	case "claude", "claude-code", "claudecode":
		return all[1:2], nil
	case "codex":
		return all[2:3], nil
	default:
		return nil, fmt.Errorf("unknown skill target %q (want hermes, claude, codex, or all)", name)
	}
}

func codexHome(home string) string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

func installTarget(t target, want []byte, force bool, now time.Time) Result {
	result := Result{Target: t.Name, Path: t.Path}
	got, err := os.ReadFile(t.Path)
	if err == nil {
		if bytes.Equal(got, want) {
			result.Status = StatusUnchanged
			return result
		}
		if !force {
			result.Status = StatusConflict
			result.Error = "existing skill content differs; use --force to replace"
			return result
		}
		backup, err := backup(t.Name, got, now)
		if err != nil {
			result.Status = StatusConflict
			result.Error = fmt.Sprintf("backup failed: %v", err)
			return result
		}
		if err := writeFile(t.Path, want); err != nil {
			result.Status = StatusConflict
			result.Error = err.Error()
			return result
		}
		result.Status = StatusReplaced
		result.Backup = backup
		return result
	}
	if !os.IsNotExist(err) {
		result.Status = StatusConflict
		result.Error = err.Error()
		return result
	}
	if err := writeFile(t.Path, want); err != nil {
		result.Status = StatusConflict
		result.Error = err.Error()
		return result
	}
	result.Status = StatusInstalled
	return result
}

func writeFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".SKILL.md.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func backup(target string, content []byte, now time.Time) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "legwork", "skill-backups", target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "SKILL.md."+now.UTC().Format("20060102T150405Z"))
	return path, os.WriteFile(path, content, 0o644)
}
