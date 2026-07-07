package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/events"
)

type artifactMeta struct {
	Run       string    `json:"run"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	Updated   time.Time `json:"updated"`
	Path      string    `json:"path"`
}

type artifactListOut struct {
	Run       string         `json:"run"`
	Artifacts []artifactMeta `json:"artifacts"`
}

type artifactGetOut struct {
	Artifact artifactMeta `json:"artifact"`
	Content  string       `json:"content"`
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
			name, err = safeArtifactName(name)
			if err != nil {
				return err
			}
			data, err := readArtifactInput(args[0])
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
			if err := writeArtifact(path, data, overwrite); err != nil {
				return err
			}
			meta, err := loadArtifactMeta(s.Root, runLabel, path)
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
			var artifacts []artifactMeta
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				meta, err := loadArtifactMeta(s.Root, runLabel, filepath.Join(dir, e.Name()))
				if err != nil {
					return err
				}
				artifacts = append(artifacts, meta)
			}
			if artifacts == nil {
				artifacts = []artifactMeta{}
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
			name, err := safeArtifactName(args[0])
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
			meta, err := loadArtifactMeta(s.Root, runLabel, path)
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

func safeArtifactName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("artifact name is required")
	}
	if name == "." || name == ".." || filepath.IsAbs(name) ||
		strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("invalid artifact name %q", name)
	}
	return name, nil
}

func readArtifactInput(src string) ([]byte, error) {
	if src == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(src)
}

func writeArtifact(path string, data []byte, overwrite bool) error {
	if overwrite {
		tmp, err := os.CreateTemp(filepath.Dir(path), ".artifact-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			return err
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmpName, path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s exists; pass --overwrite to replace it", filepath.Base(path))
		}
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func loadArtifactMeta(root, runLabel, path string) (artifactMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return artifactMeta{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return artifactMeta{}, err
	}
	return artifactMeta{
		Run:       runLabel,
		Name:      filepath.Base(path),
		SizeBytes: info.Size(),
		Updated:   info.ModTime().UTC(),
		Path:      rel,
	}, nil
}
