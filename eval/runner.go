package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// preamble frames the sandbox for the orchestrator. It is the same for every
// scenario so prompt-side changes under auto-research live in the skill/guide,
// not here.
const preamble = `You are an orchestrator driving the legwork CLI in an evaluation sandbox.
The legwork binary is on PATH and fully functional. Rules of engagement:
- Dispatch every worker with --agent fake. The workers are scripted stand-ins; treat their questions and outputs as real work product.
- Work autonomously until the goal is complete. No human is available, and asking the user anything ends the session as a failure: when a detail is unspecified, decide it yourself with a sensible default and proceed.
- Interact with the system only through Bash running legwork commands. Run them from the current directory.
- Prefer non-blocking supervision (status, ls, events, wait --timeout 60s) over long blocking calls.

Goal:

`

// orchResult is what a driver returns about the orchestrator process itself.
// Token fields separate fresh input, cache traffic, and output because "how
// much context does orchestration consume" is a primary eval question, not an
// accounting detail.
type orchResult struct {
	SessionID string  `json:"session_id,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
	NumTurns  int     `json:"num_turns"`
	// InputTokens is fresh (uncached) input across the run; cache fields
	// carry the rest of the prompt traffic.
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	// PeakContextTokens is the largest single API call (input + cache read +
	// cache creation + output): the orchestrator's context high-water mark.
	PeakContextTokens int64 `json:"peak_context_tokens"`
	ContextWindow     int64 `json:"context_window,omitempty"`
	// PermissionDenials counts tool calls outside the allowed surface —
	// the orchestrator reaching for something other than legwork.
	PermissionDenials int      `json:"permission_denials"`
	DeniedTools       []string `json:"denied_tools,omitempty"`
	Result            string   `json:"result"`
	TimedOut          bool     `json:"timed_out"`
	Err               string   `json:"error,omitempty"`
	WallS             float64  `json:"wall_s"`
	QuizAnswer        string   `json:"quiz_answer,omitempty"`
}

// contextPct is peak context as a share of the model's window.
func (o *orchResult) contextPct() float64 {
	if o.ContextWindow == 0 {
		return 0
	}
	return 100 * float64(o.PeakContextTokens) / float64(o.ContextWindow)
}

// runDirs is the per-(scenario, model) filesystem layout under the results
// dir. Everything the scorer needs survives here.
type runDirs struct {
	work     string // orchestrator cwd; git fixture lives at work/repo
	state    string // LEGWORK_STATE_DIR
	shimBin  string // dir holding the `legwork` shim, prepended to PATH
	shimCfg  string // shim.json
	mapping  string
	invLog   string
	defaults string // default-done fake script
}

func setupRun(base string, sc *Scenario, scenarioDir, realBin, selfBin string) (*runDirs, error) {
	d := &runDirs{
		work:     filepath.Join(base, "work"),
		state:    filepath.Join(base, "state"),
		shimBin:  filepath.Join(base, "bin"),
		shimCfg:  filepath.Join(base, "shim.json"),
		mapping:  filepath.Join(base, "mapping.json"),
		invLog:   filepath.Join(base, "invocations.jsonl"),
		defaults: filepath.Join(base, "default-done.jsonl"),
	}
	for _, dir := range []string{d.work, d.state, d.shimBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.Symlink(selfBin, filepath.Join(d.shimBin, "legwork")); err != nil {
		return nil, err
	}
	defaultScript := `{"type":"system","subtype":"init","session_id":"fake-default"}
{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5},"session_id":"fake-default","result":"done as instructed\n\nstate: done"}
`
	if err := os.WriteFile(d.defaults, []byte(defaultScript), 0o644); err != nil {
		return nil, err
	}
	reviews := sc.ReviewScripts
	if len(reviews) == 0 && sc.ReviewScript != "" {
		reviews = []string{sc.ReviewScript}
	}
	cfg := shimConfig{
		RealBin:       realBin,
		StateDir:      d.state,
		ScenarioDir:   scenarioDir,
		Log:           d.invLog,
		Mapping:       d.mapping,
		Specs:         sc.Jobs,
		ReviewScripts: reviews,
		DefaultScript: d.defaults,
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(d.shimCfg, raw, 0o644); err != nil {
		return nil, err
	}
	if sc.GitFixture {
		if err := gitFixture(filepath.Join(d.work, "repo")); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// gitFixture creates a repo with one commit on main, as workspace flows need
// a base to branch from and a target to merge into.
func gitFixture(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "eval@legwork.local"},
		{"config", "user.name", "legwork-eval"},
	}
	for _, s := range steps {
		if out, err := exec.Command("git", append([]string{"-C", dir}, s...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", s, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("eval fixture\n"), 0o644); err != nil {
		return err
	}
	for _, s := range [][]string{{"add", "."}, {"commit", "-q", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, s...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v\n%s", s, err, out)
		}
	}
	return nil
}

func (d *runDirs) env() []string {
	env := []string{}
	for _, kv := range os.Environ() {
		// The orchestrator must see only the shimmed world; scrub any
		// legwork state leaking from the invoking shell.
		if strings.HasPrefix(kv, "LEGWORK_") {
			continue
		}
		if strings.HasPrefix(kv, "PATH=") {
			kv = "PATH=" + d.shimBin + ":" + strings.TrimPrefix(kv, "PATH=")
		}
		env = append(env, kv)
	}
	// An empty config keeps the sandbox hermetic: the host's notifier or
	// other config must not shape (or fail) the orchestrator's runs.
	cfgPath := filepath.Join(filepath.Dir(d.shimCfg), "legwork-config.toml")
	if _, err := os.Stat(cfgPath); err != nil {
		os.WriteFile(cfgPath, nil, 0o644)
	}
	return append(env,
		ShimConfigEnv+"="+d.shimCfg,
		"LEGWORK_NO_AUTO_GC=1",
		"LEGWORK_CONFIG="+cfgPath,
	)
}

// claudeDriver runs the orchestrator as headless Claude Code. Only Bash
// invocations of legwork are permitted: everything the orchestrator needs
// must flow through the surface being measured.
type claudeDriver struct {
	model  string
	system string // appended system prompt (the legwork skill)
}

func (c *claudeDriver) run(ctx context.Context, d *runDirs, prompt string) *orchResult {
	args := []string{"-p", prompt,
		"--output-format", "json",
		"--allowedTools", "Bash(legwork:*)",
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	if c.system != "" {
		args = append(args, "--append-system-prompt", c.system)
	}
	return c.invoke(ctx, d, args, "orchestrator-raw.json")
}

func (c *claudeDriver) quiz(ctx context.Context, d *runDirs, sessionID, ask string) *orchResult {
	args := []string{"-p", ask,
		"--resume", sessionID,
		"--output-format", "json",
		"--allowedTools", "Bash(legwork:*)",
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	return c.invoke(ctx, d, args, "orchestrator-quiz-raw.json")
}

func (c *claudeDriver) invoke(ctx context.Context, d *runDirs, args []string, rawName string) *orchResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = d.work
	cmd.Env = d.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := &orchResult{WallS: time.Since(start).Seconds()}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	// The full message array is the post-hoc digging surface; keep it.
	os.WriteFile(filepath.Join(filepath.Dir(d.shimCfg), rawName), stdout.Bytes(), 0o644)
	if out, jerr := parseClaudeResult(stdout.Bytes()); jerr == nil {
		res.CostUSD = out.TotalCostUSD
		res.NumTurns = out.NumTurns
		res.SessionID = out.SessionID
		res.Result = out.Result
		res.InputTokens = out.Usage.InputTokens
		res.OutputTokens = out.Usage.OutputTokens
		res.CacheReadTokens = out.Usage.CacheReadTokens
		res.CacheCreationTokens = out.Usage.CacheCreationTokens
		res.PeakContextTokens = out.PeakContext
		for _, mu := range out.ModelUsage {
			if mu.ContextWindow > res.ContextWindow {
				res.ContextWindow = mu.ContextWindow
			}
		}
		res.PermissionDenials = len(out.PermissionDenials)
		for _, pd := range out.PermissionDenials {
			res.DeniedTools = append(res.DeniedTools, pd.ToolName)
		}
	} else if err == nil {
		err = fmt.Errorf("unparseable claude output: %s", firstN(stdout.String(), 400))
	}
	if err != nil && !res.TimedOut {
		res.Err = fmt.Sprintf("%v: %s", err, firstN(stderr.String(), 400))
	}
	return res
}

// cmdDriver runs a scripted orchestrator (a shell script driving legwork) in
// the same environment a model would get. It exists so the harness itself is
// testable deterministically with zero spend, and doubles as the seam for
// non-Claude orchestrators later.
type cmdDriver struct{ path string }

func (c *cmdDriver) run(ctx context.Context, d *runDirs, prompt string) *orchResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, c.path)
	cmd.Dir = d.work
	cmd.Env = append(d.env(), "EVAL_GOAL="+prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := &orchResult{WallS: time.Since(start).Seconds(), Result: stdout.String()}
	if ctx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	} else if err != nil {
		res.Err = fmt.Sprintf("%v: %s", err, firstN(stderr.String(), 400))
	}
	return res
}

func (c *cmdDriver) quiz(ctx context.Context, d *runDirs, sessionID, ask string) *orchResult {
	// A scripted orchestrator has no session memory; quiz is a no-op that
	// scores as unanswered.
	return &orchResult{}
}

// claudeResult is the terminal record of a headless claude run. Depending on
// CLI version, --output-format json emits either this object alone or the
// whole message array with it as the type=="result" element.
type claudeResult struct {
	Type         string  `json:"type"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	SessionID    string  `json:"session_id"`
	Result       string  `json:"result"`
	Usage        struct {
		InputTokens         int64 `json:"input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
		CacheReadTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	ModelUsage map[string]struct {
		ContextWindow int64 `json:"contextWindow"`
	} `json:"modelUsage"`
	PermissionDenials []struct {
		ToolName string `json:"tool_name"`
	} `json:"permission_denials"`
	// PeakContext is computed from per-message usage, not part of the CLI
	// output shape.
	PeakContext int64 `json:"-"`
}

func parseClaudeResult(raw []byte) (*claudeResult, error) {
	var one claudeResult
	if err := json.Unmarshal(raw, &one); err == nil && one.Type == "result" {
		return &one, nil
	}
	var many []json.RawMessage
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	var res *claudeResult
	var peak int64
	for _, el := range many {
		var probe struct {
			Type    string `json:"type"`
			Message struct {
				Usage struct {
					InputTokens         int64 `json:"input_tokens"`
					OutputTokens        int64 `json:"output_tokens"`
					CacheReadTokens     int64 `json:"cache_read_input_tokens"`
					CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(el, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "assistant":
			u := probe.Message.Usage
			if ctx := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens + u.OutputTokens; ctx > peak {
				peak = ctx
			}
		case "result":
			var r claudeResult
			if err := json.Unmarshal(el, &r); err == nil {
				res = &r
			}
		}
	}
	if res == nil {
		return nil, fmt.Errorf("no result element in claude output")
	}
	res.PeakContext = peak
	return res, nil
}

type driver interface {
	run(ctx context.Context, d *runDirs, prompt string) *orchResult
	quiz(ctx context.Context, d *runDirs, sessionID, ask string) *orchResult
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
