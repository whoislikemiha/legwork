package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

//go:embed skills/legwork/SKILL.md
var legworkSkillText string

type skillInstallReport struct {
	OK      bool                 `json:"ok"`
	Results []skillInstallResult `json:"results"`
	Error   *skillInstallError   `json:"error,omitempty"`
}

type skillInstallResult struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Backup string `json:"backup,omitempty"`
	Error  string `json:"error,omitempty"`
}

type skillInstallError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type skillInstallOptions struct {
	Target string
	Force  bool
	Now    time.Time
}

type skillTarget struct {
	Name string
	Path string
}

const (
	skillStatusInstalled = "installed"
	skillStatusUnchanged = "unchanged"
	skillStatusReplaced  = "replaced"
	skillStatusConflict  = "conflict"
)

func skillCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "skill",
		Short: "Manage the loadable legwork orchestrator skill",
		Args:  cobra.NoArgs,
	}
	c.AddCommand(skillInstallCmd())
	return c
}

func skillInstallCmd() *cobra.Command {
	var target string
	var force, asJSON bool
	c := &cobra.Command{
		Use:   "install",
		Short: "Install the legwork skill for Hermes, Claude Code, Codex, or all",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			report, err := installLegworkSkill(skillInstallOptions{
				Target: target,
				Force:  force,
				Now:    time.Now(),
			})
			if err != nil {
				if asJSON {
					report := skillInstallReport{
						OK:      false,
						Results: []skillInstallResult{},
						Error:   &skillInstallError{Code: "usage", Message: err.Error()},
					}
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					if encErr := enc.Encode(report); encErr != nil {
						return encErr
					}
					return commandError{code: 2, message: err.Error(), silent: true}
				}
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
			} else {
				for _, r := range report.Results {
					if r.Error != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s: %s\n", r.Target, r.Status, r.Path, r.Error)
						continue
					}
					if r.Backup != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s (backup %s)\n", r.Target, r.Status, r.Path, r.Backup)
						continue
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", r.Target, r.Status, r.Path)
				}
			}
			if !report.OK {
				return commandError{code: 1, message: report.Error.Message, silent: asJSON}
			}
			return nil
		},
	}
	c.Flags().StringVar(&target, "target", "all", "skill target: hermes, claude, codex, or all")
	c.Flags().BoolVar(&force, "force", false, "replace a differing installed skill after backing it up")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

func installLegworkSkill(opts skillInstallOptions) (skillInstallReport, error) {
	targets, err := resolveSkillTargets(opts.Target)
	if err != nil {
		return skillInstallReport{}, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	report := skillInstallReport{OK: true}
	for _, target := range targets {
		result := installSkillTarget(target, opts.Force, opts.Now)
		if result.Status == skillStatusConflict {
			report.OK = false
		}
		report.Results = append(report.Results, result)
	}
	if !report.OK {
		report.Error = &skillInstallError{
			Code:    "skill-conflict",
			Message: "installed skill differs; rerun with --force to replace after backup",
		}
	}
	return report, nil
}

func resolveSkillTargets(target string) ([]skillTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	all := []skillTarget{
		{Name: "hermes", Path: filepath.Join(home, ".hermes", "skills", "legwork", "SKILL.md")},
		{Name: "claude", Path: filepath.Join(home, ".claude", "skills", "legwork", "SKILL.md")},
		{Name: "codex", Path: filepath.Join(codexHome(home), "skills", "legwork", "SKILL.md")},
	}
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "", "all":
		return all, nil
	case "hermes":
		return all[:1], nil
	case "claude", "claude-code", "claudecode":
		return all[1:2], nil
	case "codex":
		return all[2:3], nil
	default:
		return nil, fmt.Errorf("unknown skill target %q (want hermes, claude, codex, or all)", target)
	}
}

func codexHome(home string) string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

func installSkillTarget(target skillTarget, force bool, now time.Time) skillInstallResult {
	result := skillInstallResult{Target: target.Name, Path: target.Path}
	want := []byte(legworkSkillText)
	got, err := os.ReadFile(target.Path)
	if err == nil {
		if bytes.Equal(got, want) {
			result.Status = skillStatusUnchanged
			return result
		}
		if !force {
			result.Status = skillStatusConflict
			result.Error = "existing skill content differs; use --force to replace"
			return result
		}
		backup, err := backupSkill(target.Name, got, now)
		if err != nil {
			result.Status = skillStatusConflict
			result.Error = fmt.Sprintf("backup failed: %v", err)
			return result
		}
		if err := writeSkillFile(target.Path, want); err != nil {
			result.Status = skillStatusConflict
			result.Error = err.Error()
			return result
		}
		result.Status = skillStatusReplaced
		result.Backup = backup
		return result
	}
	if !os.IsNotExist(err) {
		result.Status = skillStatusConflict
		result.Error = err.Error()
		return result
	}
	if err := writeSkillFile(target.Path, want); err != nil {
		result.Status = skillStatusConflict
		result.Error = err.Error()
		return result
	}
	result.Status = skillStatusInstalled
	return result
}

func writeSkillFile(path string, content []byte) error {
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

func backupSkill(target string, content []byte, now time.Time) (string, error) {
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
