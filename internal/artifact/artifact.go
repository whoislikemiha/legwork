// Package artifact stores run-attached UTF-8 text artifacts: safe naming,
// exclusive-or-atomic writes, and the metadata surfaced by the CLI.
package artifact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Meta describes one stored artifact (path is state-root-relative).
type Meta struct {
	Run       string    `json:"run"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	Updated   time.Time `json:"updated"`
	Path      string    `json:"path"`
}

// SafeName rejects anything but a single safe path component.
func SafeName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("artifact name is required")
	}
	if name == "." || name == ".." || filepath.IsAbs(name) ||
		strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("invalid artifact name %q", name)
	}
	return name, nil
}

// ReadInput reads the artifact source ("-" means stdin).
func ReadInput(src string) ([]byte, error) {
	if src == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(src)
}

// Write creates the artifact exclusively, or replaces it atomically when
// overwrite is set.
func Write(path string, data []byte, overwrite bool) error {
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

// LoadMeta stats a stored artifact into its CLI metadata.
func LoadMeta(root, runLabel, path string) (Meta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Meta{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Meta{}, err
	}
	return Meta{
		Run:       runLabel,
		Name:      filepath.Base(path),
		SizeBytes: info.Size(),
		Updated:   info.ModTime().UTC(),
		Path:      rel,
	}, nil
}
