package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/artifact"
	"github.com/whoislikemiha/legwork/internal/events"
)

type artifactListOut struct {
	Run       string          `json:"run"`
	Artifacts []artifact.Meta `json:"artifacts"`
}

type artifactGetOut struct {
	Artifact artifact.Meta `json:"artifact"`
	Content  string        `json:"content"`
}

func artifactCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "artifact",
		Short: "Save, list, and get run-attached text artifacts",
	}
	c.AddCommand(artifactSaveCmd(), artifactListCmd(), artifactGetCmd())
	return c
}

func artifactSaveCmd() *cobra.Command {
	var runLabel, name string
	var overwrite, asJSON bool
	c := &cobra.Command{
		Use:   "save --run <label> --name <name> <path|->",
		Short: "Save a text/markdown artifact under a run record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			name, err = artifact.SafeName(name)
			if err != nil {
				return err
			}
			data, err := artifact.ReadInput(args[0])
			if err != nil {
				return err
			}
			if !utf8.Valid(data) {
				return fmt.Errorf("artifact %s is not valid UTF-8; binary artifacts are not supported in v1", name)
			}
			dir, err := s.RunArtifactDir(runLabel, true)
			if err != nil {
				return err
			}
			path := filepath.Join(dir, name)
			if err := artifact.Write(path, data, overwrite); err != nil {
				return err
			}
			meta, err := artifact.LoadMeta(s.Root, runLabel, path)
			if err != nil {
				return err
			}
			evPath, err := s.RunEventsPath(runLabel)
			if err != nil {
				return fmt.Errorf("record artifact event: %w", err)
			}
			log, err := events.Open(evPath)
			if err != nil {
				return fmt.Errorf("record artifact event: %w", err)
			}
			if _, err := log.Append(events.Event{
				Type:    events.TypeArtifact,
				Actor:   "orchestrator",
				Preview: events.Truncate(name),
				Fields: map[string]any{
					"name":       name,
					"size_bytes": meta.SizeBytes,
				},
			}); err != nil {
				return fmt.Errorf("record artifact event: %w", err)
			}
			if asJSON {
				return printJSON(meta)
			}
			fmt.Printf("%s\n", name)
			return nil
		},
	}
	c.Flags().StringVar(&runLabel, "run", "", "run label to attach the artifact to")
	c.Flags().StringVar(&name, "name", "", "artifact name (single safe path component)")
	c.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing artifact")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	_ = c.MarkFlagRequired("run")
	_ = c.MarkFlagRequired("name")
	return c
}

func artifactListCmd() *cobra.Command {
	var runLabel string
	var asJSON bool
	c := &cobra.Command{
		Use:   "list --run <label>",
		Short: "List artifacts attached to a run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			dir, err := s.RunArtifactDir(runLabel, false)
			if err != nil {
				return err
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					entries = nil
				} else {
					return err
				}
			}
			var artifacts []artifact.Meta
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				meta, err := artifact.LoadMeta(s.Root, runLabel, filepath.Join(dir, e.Name()))
				if err != nil {
					return err
				}
				artifacts = append(artifacts, meta)
			}
			if artifacts == nil {
				artifacts = []artifact.Meta{}
			}
			if asJSON {
				return printJSON(artifactListOut{Run: runLabel, Artifacts: artifacts})
			}
			for _, a := range artifacts {
				fmt.Printf("%s\t%d\t%s\n", a.Name, a.SizeBytes, a.Updated.Format(time.RFC3339))
			}
			return nil
		},
	}
	c.Flags().StringVar(&runLabel, "run", "", "run label")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	_ = c.MarkFlagRequired("run")
	return c
}

func artifactGetCmd() *cobra.Command {
	var runLabel string
	var asJSON bool
	c := &cobra.Command{
		Use:   "get --run <label> <name>",
		Short: "Print a run artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			name, err := artifact.SafeName(args[0])
			if err != nil {
				return err
			}
			dir, err := s.RunArtifactDir(runLabel, false)
			if err != nil {
				return err
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !utf8.Valid(data) {
				return fmt.Errorf("artifact %s is not valid UTF-8; binary artifacts are not supported in v1", name)
			}
			meta, err := artifact.LoadMeta(s.Root, runLabel, path)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(artifactGetOut{Artifact: meta, Content: string(data)})
			}
			fmt.Print(string(data))
			return nil
		},
	}
	c.Flags().StringVar(&runLabel, "run", "", "run label")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	_ = c.MarkFlagRequired("run")
	return c
}
