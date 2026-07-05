// Package notify implements the pluggable notifier: exec a user-configured
// command with event JSON on stdin (DESIGN.md §7). ntfy, Telegram, a webhook,
// waking the orchestrator — their command, our five lines.
package notify

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config lives at $XDG_CONFIG_HOME/legwork/config.toml:
//
//	[notify]
//	command = "ntfy publish legwork"
//	events  = ["needs-input", "done", "blocked", "failed", "auth-required", "interrupted"]
type Config struct {
	Notify struct {
		Command string   `toml:"command"`
		Events  []string `toml:"events"`
	} `toml:"notify"`
}

// DefaultEvents when the config lists none.
var DefaultEvents = []string{"needs-input", "done", "blocked", "failed", "auth-required", "interrupted"}

func configPath() string {
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

func Load() (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(configPath(), &cfg); err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	if len(cfg.Notify.Events) == 0 {
		cfg.Notify.Events = DefaultEvents
	}
	return &cfg, nil
}

// Payload is what the notify command receives on stdin.
type Payload struct {
	Event    string  `json:"event"` // terminal state or event type
	Job      string  `json:"job"`
	Run      string  `json:"run,omitempty"`
	Agent    string  `json:"agent"`
	Task     string  `json:"task"`
	Question string  `json:"question,omitempty"`
	Result   string  `json:"result,omitempty"`
	CostUSD  float64 `json:"cost_usd,omitempty"`
	Context  int     `json:"context,omitempty"`
}

// Send fires the notifier if the event is subscribed. Failures are returned
// for logging but must never fail the job.
func (c *Config) Send(p Payload) error {
	if c.Notify.Command == "" {
		return nil
	}
	subscribed := false
	for _, e := range c.Notify.Events {
		if e == p.Event {
			subscribed = true
			break
		}
	}
	if !subscribed {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	cmd := exec.Command("sh", "-c", c.Notify.Command)
	cmd.Stdin = bytes.NewReader(data)
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		return nil // slow notifier must not wedge the runner
	}
}
