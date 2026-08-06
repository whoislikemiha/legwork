package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// ShimConfigEnv points the shim at its per-scenario config. The runner sets
// it in the orchestrator's environment.
const ShimConfigEnv = "LEGWORK_EVAL_SHIM_CONFIG"

// shimConfig is written by the runner into the scenario workdir.
type shimConfig struct {
	RealBin       string    `json:"real_bin"`
	StateDir      string    `json:"state_dir"`
	ScenarioDir   string    `json:"scenario_dir"` // turn scripts resolve against this
	Log           string    `json:"log"`          // invocations.jsonl
	Mapping       string    `json:"mapping"`      // mapping.json
	Specs         []JobSpec `json:"specs"`
	ReviewScripts []string  `json:"review_scripts,omitempty"` // served in dispatch order, last repeats
	DefaultScript string    `json:"default_script"`           // served when a spec runs out of turns
}

// mapping records which spec each real job ID resolved to and the turn index
// (1-based) most recently dispatched for it. The scorer reads it to resolve
// scenario job names to job IDs.
type mapping struct {
	Jobs map[string]*mappedJob `json:"jobs"`
}

type mappedJob struct {
	Spec string `json:"spec"`
	Turn int    `json:"turn"`
}

type invocation struct {
	TS   time.Time `json:"ts"`
	Argv []string  `json:"argv"`
	Exit int       `json:"exit"`
	MS   int64     `json:"ms"`
}

// runShim is the process entrypoint when argv[0] is "legwork". It never
// prompts and mirrors the real binary's stdio and exit code exactly — the
// orchestrator must not be able to tell it is shimmed.
func runShim() int {
	cfgPath := os.Getenv(ShimConfigEnv)
	cfg, err := loadShimConfig(cfgPath)
	if err != nil {
		io.WriteString(os.Stderr, "legwork eval shim: "+err.Error()+"\n")
		return 3
	}
	args := os.Args[1:]

	script := cfg.scriptFor(args)

	start := time.Now()
	exit, stdout := cfg.execReal(args, script)
	cfg.logInvocation(invocation{TS: start.UTC(), Argv: args, Exit: exit, MS: time.Since(start).Milliseconds()})

	if exit == 0 {
		cfg.recordDispatch(args, stdout)
	}
	return exit
}

func loadShimConfig(path string) (*shimConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg shimConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// scriptFor picks the fake-agent script for whatever turn this invocation
// will spawn, or "" for commands that spawn none.
func (cfg *shimConfig) scriptFor(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "run":
		if spec := cfg.matchSpec(args); spec != nil && len(spec.Turns) > 0 {
			return cfg.resolve(spec.Turns[0])
		}
		return cfg.resolve(cfg.DefaultScript)
	case "answer", "resume", "approve":
		if len(args) < 2 {
			return cfg.resolve(cfg.DefaultScript)
		}
		m, unlock, err := cfg.lockMapping()
		if err != nil {
			return cfg.resolve(cfg.DefaultScript)
		}
		defer unlock()
		if mj, ok := m.Jobs[args[1]]; ok {
			if spec := cfg.specByName(mj.Spec); spec != nil {
				// The orchestrator's own words can steer the worker's next
				// turn: answer branches make a wrong guess produce a
				// detectably wrong worker outcome instead of the same
				// script either way.
				if args[0] == "answer" && len(spec.Branches) > 0 {
					answer := strings.Join(args[2:], " ")
					for _, br := range spec.Branches {
						re, err := regexp.Compile("(?i)" + br.Regex)
						if err != nil {
							continue
						}
						if re.MatchString(answer) {
							return cfg.resolve(br.Script)
						}
					}
				}
				if mj.Turn < len(spec.Turns) {
					return cfg.resolve(spec.Turns[mj.Turn])
				}
			}
		}
		return cfg.resolve(cfg.DefaultScript)
	case "ws":
		if len(args) >= 2 && args[1] == "review" {
			if len(cfg.ReviewScripts) == 0 {
				return cfg.resolve(cfg.DefaultScript)
			}
			// The Nth review dispatch gets the Nth script: count review
			// jobs already mapped.
			idx := 0
			if m, unlock, err := cfg.lockMapping(); err == nil {
				for _, mj := range m.Jobs {
					if mj.Spec == "_review" {
						idx++
					}
				}
				unlock()
			}
			if idx >= len(cfg.ReviewScripts) {
				idx = len(cfg.ReviewScripts) - 1
			}
			return cfg.resolve(cfg.ReviewScripts[idx])
		}
	}
	return ""
}

// execReal runs the real binary with stdio passed through. Stdout is teed for
// dispatch commands so job IDs can be recovered; the tee is transparent.
func (cfg *shimConfig) execReal(args []string, script string) (int, string) {
	cmd := exec.Command(cfg.RealBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	var buf bytes.Buffer
	if dispatches(args) {
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	} else {
		cmd.Stdout = os.Stdout
	}
	env := append(os.Environ(), "LEGWORK_STATE_DIR="+cfg.StateDir)
	if script != "" {
		env = append(env, "LEGWORK_FAKE_SCRIPT="+script)
	}
	cmd.Env = env
	err := cmd.Run()
	if err == nil {
		return 0, buf.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), buf.String()
	}
	io.WriteString(os.Stderr, "legwork eval shim: "+err.Error()+"\n")
	return 3, buf.String()
}

// dispatches reports whether the command starts a new job whose ID appears on
// stdout.
func dispatches(args []string) bool {
	if len(args) == 0 {
		return false
	}
	return args[0] == "run" || (args[0] == "ws" && len(args) >= 2 && args[1] == "review")
}

// recordDispatch resolves the new job ID from stdout and advances turn
// counters for answer/resume/approve.
func (cfg *shimConfig) recordDispatch(args []string, stdout string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "run", "ws":
		if !dispatches(args) {
			return
		}
		id := jobIDFrom(args, stdout)
		if id == "" {
			return
		}
		specName := "_review"
		if args[0] == "run" {
			specName = "_unmatched"
			if spec := cfg.matchSpec(args); spec != nil {
				specName = spec.Name
			}
		}
		cfg.updateMapping(func(m *mapping) {
			m.Jobs[id] = &mappedJob{Spec: specName, Turn: 1}
		})
	case "answer", "resume", "approve":
		if len(args) < 2 {
			return
		}
		cfg.updateMapping(func(m *mapping) {
			if mj, ok := m.Jobs[args[1]]; ok {
				mj.Turn++
			}
		})
	}
}

// jobIDFrom extracts the dispatched job ID: plain output is the bare ID,
// --json output carries it in the "id" field.
func jobIDFrom(args []string, stdout string) string {
	for _, a := range args {
		if a == "--json" {
			var m map[string]any
			if err := json.Unmarshal([]byte(stdout), &m); err == nil {
				if id, ok := m["id"].(string); ok {
					return id
				}
			}
			return ""
		}
	}
	id := strings.TrimSpace(stdout)
	if regexp.MustCompile(`^job-\d+$`).MatchString(id) {
		return id
	}
	return ""
}

// runValueFlags are `legwork run` flags that consume the next argv element.
// Needed to isolate the task positional; a --run label like
// "cache-queue-design" must not participate in spec matching.
var runValueFlags = map[string]bool{
	"--agent": true, "--dir": true, "--workspace": true, "--run": true,
	"--timeout": true, "--model": true, "--effort": true,
	"--fallback-model": true, "--append-prompt": true, "--append-prompt-file": true,
}

// taskText extracts the task positional from a `run` argv. Falls back to the
// full argv join if no positional is found.
func taskText(args []string) string {
	var positionals []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") && runValueFlags[a] {
				i++
			}
			continue
		}
		positionals = append(positionals, a)
	}
	if len(positionals) == 0 {
		return strings.Join(args, " ")
	}
	return strings.Join(positionals, " ")
}

// matchSpec routes a dispatch to a job spec by its task text. Earliest match
// position wins, not spec order: orchestrators write rich task texts that
// mention sibling streams ("docs for the frontend..."), and a task's primary
// subject appears before incidental mentions. Stateless, so the pre-exec
// script choice and the post-exec mapping record always agree.
func (cfg *shimConfig) matchSpec(args []string) *JobSpec {
	joined := taskText(args)
	best, bestPos := -1, -1
	for i := range cfg.Specs {
		re, err := regexp.Compile("(?i)" + cfg.Specs[i].Match)
		if err != nil {
			continue
		}
		if loc := re.FindStringIndex(joined); loc != nil {
			if best == -1 || loc[0] < bestPos {
				best, bestPos = i, loc[0]
			}
		}
	}
	if best == -1 {
		return nil
	}
	return &cfg.Specs[best]
}

func (cfg *shimConfig) specByName(name string) *JobSpec {
	for i := range cfg.Specs {
		if cfg.Specs[i].Name == name {
			return &cfg.Specs[i]
		}
	}
	return nil
}

func (cfg *shimConfig) resolve(script string) string {
	if script == "" {
		return ""
	}
	if filepath.IsAbs(script) {
		return script
	}
	return filepath.Join(cfg.ScenarioDir, script)
}

// lockMapping opens the mapping under an exclusive flock so concurrent shim
// invocations (parallel dispatches) can't lose updates.
func (cfg *shimConfig) lockMapping() (*mapping, func(), error) {
	f, err := os.OpenFile(cfg.Mapping+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, nil, err
	}
	m := &mapping{Jobs: map[string]*mappedJob{}}
	if raw, err := os.ReadFile(cfg.Mapping); err == nil {
		json.Unmarshal(raw, m)
		if m.Jobs == nil {
			m.Jobs = map[string]*mappedJob{}
		}
	}
	return m, func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func (cfg *shimConfig) updateMapping(mut func(*mapping)) {
	m, unlock, err := cfg.lockMapping()
	if err != nil {
		return
	}
	defer unlock()
	mut(m)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(cfg.Mapping, raw, 0o644)
}

// logInvocation appends one JSONL line; O_APPEND keeps concurrent shims safe
// for lines under PIPE_BUF.
func (cfg *shimConfig) logInvocation(inv invocation) {
	f, err := os.OpenFile(cfg.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	raw, err := json.Marshal(inv)
	if err != nil {
		return
	}
	f.Write(append(raw, '\n'))
}
