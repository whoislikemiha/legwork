package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/skill"
)

//go:embed skills/legwork/SKILL.md
var legworkSkillText string

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
			report, err := skill.Install([]byte(legworkSkillText), skill.Options{
				Target: target,
				Force:  force,
				Now:    time.Now(),
			})
			if err != nil {
				if asJSON {
					report := skill.Report{
						OK:      false,
						Results: []skill.Result{},
						Error:   &skill.ReportError{Code: "usage", Message: err.Error()},
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
