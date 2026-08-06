package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	// Same binary, two roles: invoked as `legwork` it is the PATH shim in
	// front of the real binary; invoked any other way it is the eval runner.
	if filepath.Base(os.Args[0]) == "legwork" {
		os.Exit(runShim())
	}
	if err := runEval(); err != nil {
		fmt.Fprintln(os.Stderr, "legwork-eval:", err)
		os.Exit(1)
	}
}

func runEval() error {
	var (
		scenariosDir = flag.String("scenarios", "eval/scenarios", "directory of scenario dirs")
		only         = flag.String("only", "", "comma-separated scenario names (default: all)")
		models       = flag.String("models", "haiku", "comma-separated model tier ladder")
		orch         = flag.String("orchestrator", "claude", "claude | cmd:<path to scripted orchestrator>")
		resultsDir   = flag.String("results", "eval/results", "where run artifacts land")
		repo         = flag.String("repo", ".", "legwork repo root (for go build and the skill)")
		realBin      = flag.String("real-bin", "", "prebuilt legwork binary (default: go build from -repo)")
		reps         = flag.Int("reps", 1, "repetitions per (model, scenario); rates replace pass/fail")
		parallel     = flag.Int("parallel", 3, "concurrent runs (each is an isolated sandbox)")
	)
	flag.Parse()

	scenarios, err := loadScenarios(*scenariosDir, *only)
	if err != nil {
		return err
	}
	if len(scenarios) == 0 {
		return fmt.Errorf("no scenarios found in %s", *scenariosDir)
	}

	bin := *realBin
	if bin == "" {
		bin = filepath.Join(os.TempDir(), fmt.Sprintf("legwork-eval-real-%d", os.Getpid()))
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = *repo
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("go build: %v\n%s", err, out)
		}
		defer os.Remove(bin)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	skill, err := os.ReadFile(filepath.Join(*repo, "skills", "legwork", "SKILL.md"))
	if err != nil {
		return fmt.Errorf("skill (orchestrator system prompt): %w", err)
	}

	runID := time.Now().UTC().Format("20060102-150405")
	runRoot := filepath.Join(*resultsDir, runID)

	// Flat work list; each run is a fully isolated sandbox so reps can run
	// concurrently without sharing anything but the API rate limit.
	type work struct {
		model string
		entry scenarioEntry
		rep   int
	}
	var jobs []work
	for _, model := range strings.Split(*models, ",") {
		model = strings.TrimSpace(model)
		for _, entry := range scenarios {
			for rep := 1; rep <= *reps; rep++ {
				jobs = append(jobs, work{model, entry, rep})
			}
		}
	}

	var (
		mu      sync.Mutex
		results []*scenarioResult
		firstEr error
		wg      sync.WaitGroup
		sem     = make(chan struct{}, max(1, *parallel))
	)
	for _, w := range jobs {
		wg.Add(1)
		go func(w work) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := runOne(runRoot, w.model, w.entry, w.rep, *reps, bin, self, string(skill), *orch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstEr == nil {
					firstEr = err
				}
				return
			}
			results = append(results, res)
		}(w)
	}
	wg.Wait()
	if firstEr != nil {
		return firstEr
	}

	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Scenario != b.Scenario {
			return a.Scenario < b.Scenario
		}
		return a.Rep < b.Rep
	})
	return writeSummary(runRoot, results, *reps)
}

func runOne(runRoot, model string, entry scenarioEntry, rep, reps int, bin, self, skill, orch string) (*scenarioResult, error) {
	sc, scDir := entry.sc, entry.dir
	name := model + "__" + sc.Name
	if reps > 1 {
		name = fmt.Sprintf("%s__r%02d", name, rep)
	}
	base := filepath.Join(runRoot, name)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	d, err := setupRun(base, sc, scDir, bin, self)
	if err != nil {
		return nil, fmt.Errorf("%s: setup: %w", sc.Name, err)
	}

	var drv driver
	switch {
	case orch == "claude":
		drv = &claudeDriver{model: model, system: skill}
	case strings.HasPrefix(orch, "cmd:"):
		path, err := filepath.Abs(strings.TrimPrefix(orch, "cmd:"))
		if err != nil {
			return nil, err
		}
		drv = &cmdDriver{path: path}
	default:
		return nil, fmt.Errorf("unknown orchestrator %q", orch)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(sc.TimeoutS)*time.Second)
	defer cancel()
	orchRes := drv.run(ctx, d, preamble+sc.Goal)
	if sc.Quiz != nil && orchRes.SessionID != "" && !orchRes.TimedOut {
		qr := drv.quiz(ctx, d, orchRes.SessionID, sc.Quiz.Ask)
		orchRes.QuizAnswer = qr.Result
		orchRes.CostUSD += qr.CostUSD
		orchRes.InputTokens += qr.InputTokens
		orchRes.OutputTokens += qr.OutputTokens
		orchRes.CacheReadTokens += qr.CacheReadTokens
		orchRes.CacheCreationTokens += qr.CacheCreationTokens
		if qr.PeakContextTokens > orchRes.PeakContextTokens {
			orchRes.PeakContextTokens = qr.PeakContextTokens
		}
	}

	res := score(sc, d, orchRes, model)
	res.Rep = rep
	res.Base = base
	raw, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(base, "result.json"), raw, 0o644); err != nil {
		return nil, err
	}
	fmt.Printf("▸ %s r%d  %s  fumbles=%d invocations=%d peak-ctx=%s cost=$%.4f\n",
		model+"/"+sc.Name, rep, passStr(res.Passed), res.Metrics.Fumbles,
		res.Metrics.Invocations, kTok(orchRes.PeakContextTokens), orchRes.CostUSD)
	return res, nil
}

type scenarioEntry struct {
	sc  *Scenario
	dir string
}

func loadScenarios(root, only string) ([]scenarioEntry, error) {
	wanted := map[string]bool{}
	for _, n := range strings.Split(only, ",") {
		if n = strings.TrimSpace(n); n != "" {
			wanted[n] = true
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []scenarioEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir, err := filepath.Abs(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		sc, err := loadScenario(dir)
		if err != nil {
			return nil, err
		}
		if len(wanted) > 0 && !wanted[sc.Name] {
			continue
		}
		out = append(out, scenarioEntry{sc, dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sc.Name < out[j].sc.Name })
	return out, nil
}

func writeSummary(runRoot string, results []*scenarioResult, reps int) error {
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(runRoot, "summary.json"), raw, 0o644); err != nil {
		return err
	}
	var b strings.Builder

	if reps > 1 {
		writeAggregate(&b, results)
		b.WriteString("\n## Failing runs\n")
		for _, r := range results {
			if !r.Passed {
				writeDetail(&b, r, runRoot)
			}
		}
		if err := os.WriteFile(filepath.Join(runRoot, "summary.md"), []byte(b.String()), 0o644); err != nil {
			return err
		}
		fmt.Println()
		fmt.Print(b.String())
		fmt.Println("\nartifacts:", runRoot)
		return nil
	}

	b.WriteString("## Outcomes\n\n")
	b.WriteString("| model | scenario | pass | fumbles | denials | invocations | jobs | latency(s) | quiz |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		quiz := "-"
		if r.QuizPass != nil {
			quiz = passStr(*r.QuizPass)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %d | %s | %s |\n",
			r.Model, r.Scenario, passStr(r.Passed), r.Metrics.Fumbles, r.Orch.PermissionDenials,
			r.Metrics.Invocations, r.Metrics.JobsDispatched, latencyStr(r.Metrics.ResponseLatencyS), quiz)
	}

	b.WriteString("\n## Tokens\n\n")
	b.WriteString("| model | scenario | turns | fresh-in | out | cache-read | cache-write | peak-ctx | ctx% | wall(s) | cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		o := r.Orch
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %s | %s | %.0f%% | %.0f | $%.4f |\n",
			r.Model, r.Scenario, o.NumTurns, kTok(o.InputTokens), kTok(o.OutputTokens),
			kTok(o.CacheReadTokens), kTok(o.CacheCreationTokens), kTok(o.PeakContextTokens),
			o.contextPct(), o.WallS, o.CostUSD)
	}

	b.WriteString("\n## Detail\n")
	for _, r := range results {
		writeDetail(&b, r, runRoot)
	}

	if err := os.WriteFile(filepath.Join(runRoot, "summary.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(b.String())
	fmt.Println("\nartifacts:", runRoot)
	return nil
}

// writeAggregate renders rate/percentile tables across repetitions — the
// trustworthy view. Medians resist single weird runs; peak-ctx reports the max
// because context safety is about the worst case.
func writeAggregate(b *strings.Builder, results []*scenarioResult) {
	type key struct{ model, scenario string }
	groups := map[key][]*scenarioResult{}
	var order []key
	for _, r := range results {
		k := key{r.Model, r.Scenario}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}

	b.WriteString("## Rates\n\n")
	b.WriteString("| model | scenario | pass | quiz | fumbles med(max) | denials med(max) | inv med | latency med(s) | peak-ctx max | wall med(s) | cost total |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, k := range order {
		g := groups[k]
		var pass, quizPass, quizTotal int
		var fumbles, denials, invs, walls, peaks []float64
		var lats []float64
		var cost float64
		for _, r := range g {
			if r.Passed {
				pass++
			}
			if r.QuizPass != nil {
				quizTotal++
				if *r.QuizPass {
					quizPass++
				}
			}
			fumbles = append(fumbles, float64(r.Metrics.Fumbles))
			denials = append(denials, float64(r.Orch.PermissionDenials))
			invs = append(invs, float64(r.Metrics.Invocations))
			walls = append(walls, r.Orch.WallS)
			peaks = append(peaks, float64(r.Orch.PeakContextTokens))
			for _, v := range r.Metrics.ResponseLatencyS {
				lats = append(lats, v)
			}
			cost += r.Orch.CostUSD
		}
		quiz := "-"
		if quizTotal > 0 {
			quiz = fmt.Sprintf("%d/%d", quizPass, quizTotal)
		}
		lat := "-"
		if len(lats) > 0 {
			lat = fmt.Sprintf("%.0f", median(lats))
		}
		fmt.Fprintf(b, "| %s | %s | %d/%d | %s | %.0f(%.0f) | %.0f(%.0f) | %.0f | %s | %s | %.0f | $%.2f |\n",
			k.model, k.scenario, pass, len(g), quiz,
			median(fumbles), maxF(fumbles), median(denials), maxF(denials),
			median(invs), lat, kTok(int64(maxF(peaks))), median(walls), cost)
	}
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func maxF(v []float64) float64 {
	out := 0.0
	for _, x := range v {
		if x > out {
			out = x
		}
	}
	return out
}

// writeDetail is the per-run drill-down: every check with its verdict, every
// fumble and denial, the full invocation timeline, and what the orchestrator
// said — enough to understand a run without opening artifact files.
func writeDetail(b *strings.Builder, r *scenarioResult, runRoot string) {
	o := r.Orch
	fmt.Fprintf(b, "\n### %s / %s r%d — %s\n\n", r.Model, r.Scenario, r.Rep, passStr(r.Passed))
	if o.Err != "" {
		fmt.Fprintf(b, "**Orchestrator error:** %s\n\n", o.Err)
	}
	if o.TimedOut {
		b.WriteString("**Timed out.**\n\n")
	}
	for _, c := range r.Checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
		}
		desc := c.Kind
		if c.Job != "" {
			desc += " " + c.Job
		}
		if c.Want != "" {
			desc += "=" + c.Want
		}
		if c.Type != "" {
			desc += " " + c.Type
		}
		if c.Regex != "" {
			desc += " ~" + c.Regex
		}
		if c.Detail != "" {
			desc += " — " + c.Detail
		}
		fmt.Fprintf(b, "- %s %s\n", mark, desc)
	}
	if len(o.DeniedTools) > 0 {
		fmt.Fprintf(b, "\n**Permission denials:** %s\n", strings.Join(o.DeniedTools, ", "))
	}
	if len(r.Metrics.FumbleArgv) > 0 {
		b.WriteString("\n**Fumbles:**\n")
		for _, f := range r.Metrics.FumbleArgv {
			fmt.Fprintf(b, "- `legwork %s`\n", f)
		}
	}
	base := r.Base
	if base == "" {
		base = filepath.Join(runRoot, r.Model+"__"+r.Scenario)
	}
	if invs := readInvocations(filepath.Join(base, "invocations.jsonl"), false); len(invs) > 0 {
		b.WriteString("\n**Invocation timeline:**\n\n")
		b.WriteString("| t(s) | command | exit | ms |\n|---|---|---|---|\n")
		t0 := invs[0].TS
		for _, inv := range invs {
			fmt.Fprintf(b, "| %.0f | `legwork %s` | %d | %d |\n",
				inv.TS.Sub(t0).Seconds(), strings.Join(inv.Argv, " "), inv.Exit, inv.MS)
		}
	}
	if o.Result != "" {
		fmt.Fprintf(b, "\n**Final report:** %s\n", firstN(strings.TrimSpace(o.Result), 600))
	}
	if o.QuizAnswer != "" {
		fmt.Fprintf(b, "\n**Quiz answer:** %s\n", firstN(strings.TrimSpace(o.QuizAnswer), 600))
	}
}

func latencyStr(m map[string]float64) string {
	if len(m) == 0 {
		return "-"
	}
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s:%.0f", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// kTok renders token counts compactly (12.3k) without hiding small values.
func kTok(n int64) string {
	if n >= 10000 {
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func passStr(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
