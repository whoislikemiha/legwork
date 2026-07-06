// Package doctor is legwork's preflight: one command an orchestrator (or the
// installer / skill) runs before dispatching a job, so a misconfigured
// environment — missing/expired auth, a bad model name, an unwritable state
// dir, a broken notifier — is discovered up front instead of after a job is
// spawned and its turn fails. Static checks are offline-safe; the live-turn
// probe (on by default) validates auth and model acceptance for real.
package doctor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/whoislikemiha/legwork/internal/adapter"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/notify"
	"github.com/whoislikemiha/legwork/internal/rules"
)

// Status is a single check's verdict. Top-level ok means no fail (warns and
// skips are allowed).
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Check is one preflight result. This is new surface parsed by orchestrators,
// not an event-schema type — see internal/guide/guide.md for the shape.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

// Report is the aggregate doctor output (the --json shape).
type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// Options configures a doctor run.
type Options struct {
	// Adapter is the agent a subsequent `run` would use (already validated;
	// an unknown agent is a usage error handled by the caller).
	Adapter adapter.Adapter
	Model   string // model override to validate, "" = agent default
	Dir     string // repo to check for the worktree.toml/workstree pairing; "" = cwd
	NoProbe bool   // skip the paid live-turn check (offline-safe)
}

// defaultProbeTimeout caps the live turn: a trivial "reply ok" is fast, but a
// hung agent must not wedge preflight. LEGWORK_DOCTOR_PROBE_TIMEOUT (a Go
// duration) overrides it — primarily a test seam, also handy for slow links.
const defaultProbeTimeout = 120 * time.Second

// versionTimeout caps the `--version` capture in the git/agent checks.
const versionTimeout = 5 * time.Second

func probeTimeout() time.Duration {
	if raw := os.Getenv("LEGWORK_DOCTOR_PROBE_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultProbeTimeout
}

// Run executes every check and reports whether any failed. Checks always run
// to completion (a fail never short-circuits the rest) so one command gives
// the whole picture.
func Run(opts Options) (checks []Check, ok bool) {
	checks = append(checks, checkBinary())
	checks = append(checks, checkStateDir())
	checks = append(checks, checkGit())
	checks = append(checks, checkAgent(opts.Adapter))
	if opts.NoProbe {
		checks = append(checks, Check{"probe", StatusSkip, "--no-probe: static checks only"})
	} else {
		checks = append(checks, checkProbe(opts.Adapter, opts.Model))
	}
	checks = append(checks, checkWorkstree(opts.Dir))
	checks = append(checks, checkNotifier(opts.Adapter))
	return checks, allOK(checks)
}

// allOK is the top-level verdict: no fail. Warns and skips are allowed.
func allOK(checks []Check) bool {
	for _, c := range checks {
		if c.Status == StatusFail {
			return false
		}
	}
	return true
}

// checkBinary surfaces which build is actually running — path plus the VCS
// stamp Go embeds at build time. Staleness (an installed binary older than the
// source it's dispatched from) otherwise only shows up as subtly wrong
// behavior; making the commit and build date visible turns it into a glance.
func checkBinary() Check {
	self, err := os.Executable()
	if err != nil {
		return Check{"binary", StatusWarn, fmt.Sprintf("cannot resolve own path: %v", err)}
	}
	detail := self
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev, at string
		dirty := false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.time":
				at = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			detail += fmt.Sprintf(" (built from %s", rev)
			if at != "" {
				detail += " committed " + at
			}
			if dirty {
				detail += ", dirty tree"
			}
			detail += ")"
		}
	}
	return Check{"binary", StatusOK, detail}
}

// checkStateDir confirms the state dir exists and a probe file round-trips.
func checkStateDir() Check {
	root := job.DefaultRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Check{"state-dir", StatusFail, fmt.Sprintf("%s: %v", root, err)}
	}
	probe := filepath.Join(root, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return Check{"state-dir", StatusFail, fmt.Sprintf("%s: not writable: %v", root, err)}
	}
	_ = os.Remove(probe)
	return Check{"state-dir", StatusOK, root + " (writable)"}
}

// checkGit warns (not fails) when git is missing: only workspaces need it, and
// scratch/in-place jobs run fine without it.
func checkGit() Check {
	path, err := exec.LookPath("git")
	if err != nil {
		return Check{"git", StatusWarn, "git not found on PATH (needed for workspaces)"}
	}
	ver := commandVersion(path, "version")
	if ver == "" {
		return Check{"git", StatusOK, path}
	}
	return Check{"git", StatusOK, fmt.Sprintf("%s (%s)", path, ver)}
}

// checkAgent verifies the agent binary is on PATH and captures its version.
func checkAgent(ad adapter.Adapter) Check {
	bin := ad.Bin()
	if bin == "" {
		return Check{"agent", StatusFail, fmt.Sprintf("%s: no binary resolved", ad.Name())}
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return Check{"agent", StatusFail, fmt.Sprintf("%s: binary %q not found on PATH", ad.Name(), bin)}
	}
	ver := commandVersion(path, "--version")
	if ver == "" {
		return Check{"agent", StatusOK, fmt.Sprintf("%s at %s", ad.Name(), path)}
	}
	return Check{"agent", StatusOK, fmt.Sprintf("%s %s at %s", ad.Name(), ver, path)}
}

// checkProbe runs one real turn through the adapter's Command/Parser — no job
// store, no runner, nothing persisted — to validate auth and model acceptance.
// auth-required and failed map to fail; any completed state is ok.
func checkProbe(ad adapter.Adapter, model string) Check {
	workDir, err := os.MkdirTemp("", "legwork-doctor-probe")
	if err != nil {
		return Check{"probe", StatusFail, "temp dir: " + err.Error()}
	}
	defer os.RemoveAll(workDir)

	cmd, err := ad.Command(adapter.TurnRequest{
		Task:         "Reply with just: ok",
		SystemPrompt: rules.Compose(""),
		Model:        model,
		WorkDir:      workDir,
	})
	if err != nil {
		return Check{"probe", StatusFail, "command: " + err.Error()}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Check{"probe", StatusFail, "pipe: " + err.Error()}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Check{"probe", StatusFail, "starting agent: " + err.Error()}
	}

	// Bind a deadline to the (adapter-constructed) process: a watcher kills it
	// when the context expires, and ctx.Err() — safe to read across goroutines —
	// tells us afterward whether that happened. cancel() on the natural-exit
	// path retires the watcher without killing.
	timeout := probeTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.DeadlineExceeded {
			_ = cmd.Process.Kill()
		}
	}()

	parser := ad.Parser()
	var result *adapter.TurnResult
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	for sc.Scan() {
		_, res, perr := parser.Line(sc.Bytes())
		if perr != nil {
			continue
		}
		if res != nil {
			result = res
		}
	}
	_ = cmd.Wait()
	cancel()

	// A completed turn is reported by its result regardless of timing; the
	// timeout only explains a missing result.
	if result != nil {
		switch result.State {
		case "auth-required":
			return Check{"probe", StatusFail, "auth-required: " + oneline(result.Result)}
		case "failed":
			return Check{"probe", StatusFail, "failed: " + oneline(result.Result)}
		default:
			return Check{"probe", StatusOK,
				fmt.Sprintf("live turn completed: state %s, model accepted ($%.4f)", result.State, result.CostUSD)}
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return Check{"probe", StatusFail, fmt.Sprintf("timed out after %s", timeout)}
	}
	detail := "agent exited without a result"
	if s := oneline(stderr.String()); s != "" {
		detail += ": " + s
	}
	return Check{"probe", StatusFail, detail}
}

// checkWorkstree mirrors workspace bootstrap: if the repo declares
// worktree.toml but workstree isn't installed, workspace setup would skip
// silently — surface it as a warning now.
func checkWorkstree(dir string) Check {
	if dir == "" {
		dir = "."
	}
	tomlPath := filepath.Join(dir, "worktree.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		return Check{"workstree", StatusSkip, "no worktree.toml at " + dir}
	}
	if _, err := exec.LookPath("workstree"); err != nil {
		return Check{"workstree", StatusWarn, "worktree.toml present but workstree not on PATH"}
	}
	return Check{"workstree", StatusOK, "worktree.toml present, workstree on PATH"}
}

// checkNotifier runs the configured notify command with a doctor payload and
// reports its exit status. No command configured -> skip.
//
// A broken command fails doctor here, even though the runner deliberately
// tolerates notify failures at job time (a slow/erroring notifier must never
// wedge a job — notify.Send swallows it). That asymmetry is the point: doctor
// is preflight, so surfacing a misconfigured notifier now is exactly when the
// operator can still fix it, before jobs start silently losing notifications.
func checkNotifier(ad adapter.Adapter) Check {
	cfg, err := notify.Load()
	if err != nil {
		return Check{"notifier", StatusFail, "config load: " + err.Error()}
	}
	if cfg.Notify.Command == "" {
		return Check{"notifier", StatusSkip, "no notify command configured"}
	}
	// Send only fires for subscribed events; force delivery of the doctor
	// probe regardless of the configured event list.
	cfg.Notify.Events = []string{"doctor"}
	if err := cfg.Send(notify.Payload{
		Event: "doctor", Job: "doctor", Agent: ad.Name(), Task: "doctor preflight",
	}); err != nil {
		return Check{"notifier", StatusFail, "command failed: " + err.Error()}
	}
	return Check{"notifier", StatusOK, `command exited 0 (event "doctor" sent)`}
}

// commandVersion runs `<path> <arg>` with a short timeout and returns the
// trimmed first line, or "" if it fails.
func commandVersion(path, arg string) string {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, arg).Output()
	if err != nil {
		return ""
	}
	return oneline(string(out))
}

// oneline collapses text to its first non-empty line, trimmed and length-capped.
func oneline(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 160 {
			line = line[:157] + "..."
		}
		return line
	}
	return ""
}
