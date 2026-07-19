package notify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	originVersion      = 1
	originFilename     = ".notify-origin.json"
	MaxCaptureEnvNames = 32
	MaxCaptureEnvValue = 64 * 1024
	maxCaptureEnvName  = 255
)

var posixEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Origin is private job state, deliberately separate from public metadata and
// notifier payloads. An entry distinguishes an absent variable from a present
// variable whose value is empty.
type Origin struct {
	Version int                    `json:"version"`
	Values  map[string]OriginValue `json:"values"`
}

type OriginValue struct {
	Present bool   `json:"present"`
	Value   string `json:"value,omitempty"`
}

func ValidateCaptureEnv(names []string) error {
	if len(names) > MaxCaptureEnvNames {
		return fmt.Errorf("notify.capture_env: %d names exceeds maximum %d", len(names), MaxCaptureEnvNames)
	}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if len(name) == 0 || len(name) > maxCaptureEnvName || !posixEnvName.MatchString(name) {
			return fmt.Errorf("notify.capture_env: invalid environment name %q", name)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("notify.capture_env: duplicate environment name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// CaptureOrigin snapshots only explicitly allowed names from env.
func CaptureOrigin(names, env []string) (*Origin, error) {
	if err := ValidateCaptureEnv(names); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	current := environmentMap(env)
	origin := &Origin{Version: originVersion, Values: make(map[string]OriginValue, len(names))}
	for _, name := range names {
		value, present := current[name]
		if present && len(value) > MaxCaptureEnvValue {
			return nil, fmt.Errorf("notify.capture_env: value for %q exceeds maximum %d bytes", name, MaxCaptureEnvValue)
		}
		origin.Values[name] = OriginValue{Present: present, Value: value}
	}
	return origin, nil
}

func CaptureCurrent(names []string) (*Origin, error) {
	return CaptureOrigin(names, os.Environ())
}

func OriginPath(jobDir string) string { return filepath.Join(jobDir, originFilename) }

// SaveOrigin atomically persists an initial snapshot with private permissions.
func SaveOrigin(jobDir string, origin *Origin) error {
	if origin == nil {
		return nil
	}
	if err := validateOrigin(origin); err != nil {
		return err
	}
	data, err := json.Marshal(origin)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(jobDir, originFilename+".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, OriginPath(jobDir)); err != nil {
		return err
	}
	ok = true
	dir, err := os.Open(jobDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func LoadOrigin(jobDir string) (*Origin, error) {
	data, err := os.ReadFile(OriginPath(jobDir))
	if err != nil {
		return nil, err
	}
	var origin Origin
	if err := json.Unmarshal(data, &origin); err != nil {
		return nil, fmt.Errorf("invalid notifier origin snapshot")
	}
	if err := validateOrigin(&origin); err != nil {
		return nil, err
	}
	return &origin, nil
}

func validateOrigin(origin *Origin) error {
	if origin == nil || origin.Version != originVersion {
		return fmt.Errorf("unsupported notifier origin snapshot version")
	}
	if len(origin.Values) > MaxCaptureEnvNames {
		return fmt.Errorf("invalid notifier origin snapshot: too many names")
	}
	for name, value := range origin.Values {
		if err := ValidateCaptureEnv([]string{name}); err != nil {
			return fmt.Errorf("invalid notifier origin snapshot: %w", err)
		}
		if len(value.Value) > MaxCaptureEnvValue {
			return fmt.Errorf("invalid notifier origin snapshot: value for %q is too large", name)
		}
		if !value.Present && value.Value != "" {
			return fmt.Errorf("invalid notifier origin snapshot: absent value for %q is non-empty", name)
		}
	}
	return nil
}

// OverlayEnvironment scrubs every snapshotted and currently configured name,
// then restores only the intersection from the immutable snapshot. Missing
// entries and explicitly absent entries remain absent.
func OverlayEnvironment(base []string, origin *Origin, allowed []string) []string {
	scrub := append([]string(nil), allowed...)
	if origin != nil {
		for name := range origin.Values {
			scrub = append(scrub, name)
		}
	}
	out := ScrubEnvironment(base, scrub)
	if origin == nil {
		return out
	}
	for _, name := range allowed {
		if captured, ok := origin.Values[name]; ok && captured.Present {
			out = append(out, name+"="+captured.Value)
		}
	}
	return out
}

func ScrubEnvironment(base, names []string) []string {
	remove := make(map[string]struct{}, len(names))
	for _, name := range names {
		remove[name] = struct{}{}
	}
	out := make([]string, 0, len(base))
	for _, item := range base {
		name, _, ok := strings.Cut(item, "=")
		if _, scrub := remove[name]; ok && scrub {
			continue
		}
		out = append(out, item)
	}
	return out
}

// JobScrubNames returns the union of the current allowlist and names stored in
// a job snapshot. Corruption is fail-closed for current names and surfaced.
func (c *Config) JobScrubNames(jobDir string) ([]string, error) {
	names := append([]string(nil), c.Notify.CaptureEnv...)
	origin, err := LoadOrigin(jobDir)
	if os.IsNotExist(err) {
		return names, nil
	}
	if err != nil {
		return names, err
	}
	for name := range origin.Values {
		names = append(names, name)
	}
	return names, nil
}

func (c *Config) JobNotifierEnv(jobDir string, base []string) ([]string, error) {
	origin, err := LoadOrigin(jobDir)
	if os.IsNotExist(err) {
		return OverlayEnvironment(base, nil, c.Notify.CaptureEnv), nil
	}
	if err != nil {
		return OverlayEnvironment(base, nil, c.Notify.CaptureEnv), err
	}
	return OverlayEnvironment(base, origin, c.Notify.CaptureEnv), nil
}

func environmentMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, item := range env {
		if name, value, ok := strings.Cut(item, "="); ok {
			out[name] = value
		}
	}
	return out
}
