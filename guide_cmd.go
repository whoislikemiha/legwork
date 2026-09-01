package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/guide"
)

func guideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print the orchestrator guide (the loop, notifications, workspaces, health)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(guide.Text)
			return nil
		},
	}
}
