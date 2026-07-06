// Package config resolves the single legwork config file shared by the
// notifier and gc ([notify] and [gc] tables in one config.toml).
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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

// Health is the [health] table of config.toml. ContextThreshold is the last-turn
// context window (in tokens) at or above which ls/status flag a job as high — the
// cue to start a fresh job instead of resuming. 0 disables the marker.
type Health struct{ ContextThreshold int }

var healthDefaults = Health{ContextThreshold: 150000}

// LoadHealth reads the [health] table, applying the default when unset. A missing
// config file yields defaults; a malformed one surfaces its error.
func LoadHealth() (Health, error) {
	var raw struct {
		Health struct {
			ContextThreshold *int `toml:"context_threshold"`
		} `toml:"health"`
	}
	h := healthDefaults
	if _, err := toml.DecodeFile(Path(), &raw); err != nil && !os.IsNotExist(err) {
		return h, err
	}
	if raw.Health.ContextThreshold != nil {
		h.ContextThreshold = *raw.Health.ContextThreshold
	}
	return h, nil
}
