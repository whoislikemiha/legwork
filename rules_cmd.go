package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/rules"
)

type rulesOutput struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
}

func rulesCmd() *cobra.Command {
	var agent, dir, wsID string
	var readOnly, asJSON bool
	c := &cobra.Command{
		Use:   "rules",
		Short: "Print the injected worker contract",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := adapter.New(agent); err != nil {
				return err
			}
			if dir != "" && wsID != "" {
				return fmt.Errorf("--dir and --workspace are mutually exclusive")
			}
			_ = readOnly // Accepted for symmetry with dispatch; current rules do not vary by mode.

			out := rulesOutput{Version: rules.Version, Text: rules.Text()}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), out.Text)
			return err
		},
	}
	c.Flags().StringVar(&agent, "agent", "claude", "agent adapter (claude, codex, fake)")
	c.Flags().StringVar(&dir, "dir", "", "show rules for an in-place job shape")
	c.Flags().StringVar(&wsID, "workspace", "", "show rules for a workspace job shape")
	c.Flags().BoolVar(&readOnly, "read-only", false, "show rules for a read-only turn")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}
