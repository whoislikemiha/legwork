// Package config resolves the single legwork config file shared by the
// notifier and gc ([notify] and [gc] tables in one config.toml).
package config

import (
	"os"
	"path/filepath"
)

// Path returns the config file location: $LEGWORK_CONFIG if set, else
// $XDG_CONFIG_HOME/legwork/config.toml (falling back to ~/.config).
func Path() string {
	if p := os.Getenv("LEGWORK_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "legwork", "config.toml")
}
