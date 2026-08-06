package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// scenarioResult is the scored outcome of one (scenario, model) run —
// result.json in the run dir and one row of the summary table.
type scenarioResult struct {
	Scenario string `json:"scenario"`
	Model    string `json:"model"`
	// Rep is the 1-based repetition index; Base is this rep's artifact dir.
	Rep      int         `json:"rep"`
	Base     string      `json:"base"`
	Passed   bool        `json:"passed"`
	Checks   []checked   `json:"checks"`
	Metrics  metrics     `json:"metrics"`
	Orch     *orchResult `json:"orchestrator"`
	QuizPass *bool       `json:"quiz_pass,omitempty"`
}

type checked struct {
	Check
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type metrics struct {
	Invocations int `json:"invocations"`
	// Fumbles are CLI invocations that exited nonzero: the orchestrator
	// asked the surface for something it couldn't parse or permit. This is
	// the interface-complexity signal.
	Fumbles    int      `json:"fumbles"`
	FumbleArgv []string `json:"fumble_argv,omitempty"`
	// ResponseLatencyS: seconds from a job's needs-input/needs-provision
	// event to the orchestrator's answer/approve on that job.
	ResponseLatencyS map[string]float64 `json:"response_latency_s,omitempty"`
	JobsDispatched   int                `json:"jobs_dispatched"`
}

// eventLine mirrors internal/events' JSONL shape (schema v2) closely enough
// to score against; the eval reads it as the public interface it is.
type eventLine struct {
	Seq     int            `json:"seq"`
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Actor   string         `json:"actor"`
	Preview string         `json:"preview"`
	Fields  map[string]any `json:"fields"`
}

type jobMeta struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

func score(sc *Scenario, d *runDirs, orch *orchResult, model string) *scenarioResult {
	res := &scenarioResult{Scenario: sc.Name, Model: model, Orch: orch}
	m := readMapping(d.mapping)
	res.Metrics = computeMetrics(d, m)

	byName := jobsByName(m)
	for _, c := range sc.Checks {
		res.Checks = append(res.Checks, runCheck(c, d, byName, res.Metrics.JobsDispatched, orch.PermissionDenials))
	}
	res.Passed = orch.Err == "" && !orch.TimedOut
	for _, c := range res.Checks {
		if !c.OK {
			res.Passed = false
		}
	}
	if sc.Quiz != nil {
		ok := quizPass(sc.Quiz, orch.QuizAnswer)
		res.QuizPass = &ok
		if !ok {
			res.Passed = false
		}
	}
	return res
}

func readMapping(path string) *mapping {
	m := &mapping{Jobs: map[string]*mappedJob{}}
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, m)
		if m.Jobs == nil {
			m.Jobs = map[string]*mappedJob{}
		}
	}
	return m
}

// jobsByName resolves scenario job names to real job IDs. If the orchestrator
// dispatched a role more than once, the job with the most turns wins (ties:
// earliest ID) — a double-dispatched role is scored on the job the
// orchestrator actually engaged with, not an abandoned first attempt
// (observed live: double-fix r2 relayed both findings to the second exporter
// job while the scorer read the first).
func jobsByName(m *mapping) map[string]string {
	ids := make([]string, 0, len(m.Jobs))
	for id := range m.Jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	byName := map[string]string{}
	for _, id := range ids {
		spec := m.Jobs[id].Spec
		if prev, taken := byName[spec]; !taken || m.Jobs[id].Turn > m.Jobs[prev].Turn {
			byName[spec] = id
		}
	}
	return byName
}

func runCheck(c Check, d *runDirs, byName map[string]string, jobsDispatched, denials int) checked {
	out := checked{Check: c}
	fail := func(format string, a ...any) checked {
		out.Detail = fmt.Sprintf(format, a...)
		return out
	}
	jobID := byName[c.Job]
	if c.Job != "" && jobID == "" {
		return fail("no dispatched job matched role %q", c.Job)
	}
	switch c.Kind {
	case "job_state":
		meta, err := readMeta(d.state, jobID)
		if err != nil {
			return fail("meta: %v", err)
		}
		if meta.State != c.Want {
			return fail("state %s, want %s", meta.State, c.Want)
		}
	case "event", "no_event":
		evs, err := readEvents(d.state, jobID)
		if err != nil {
			return fail("events: %v", err)
		}
		found := findEvent(evs, c.Type, c.Regex)
		if c.Kind == "event" && found == nil {
			return fail("no %s event matching %q", c.Type, c.Regex)
		}
		if c.Kind == "no_event" && found != nil {
			return fail("unwanted %s event: %s", c.Type, found.Preview)
		}
	case "file_contains":
		raw, err := os.ReadFile(filepath.Join(d.work, c.Path))
		if err != nil {
			return fail("%v", err)
		}
		re, err := regexp.Compile("(?i)" + c.Regex)
		if err != nil {
			return fail("bad regex: %v", err)
		}
		if !re.Match(raw) {
			return fail("%s does not match %q", c.Path, c.Regex)
		}
	case "max_fumbles":
		fumbles := len(readInvocations(d.invLog, true))
		if fumbles > c.N {
			return fail("%d fumbles, max %d", fumbles, c.N)
		}
	case "max_denials":
		if denials > c.N {
			return fail("%d permission denials, max %d", denials, c.N)
		}
	case "min_jobs":
		if jobsDispatched < c.N {
			return fail("%d jobs dispatched, want at least %d", jobsDispatched, c.N)
		}
	case "no_git_after_close":
		// The F1 measurement, scoped to what the close receipt is supposed to
		// obviate: reaching for git AFTER legwork close to confirm the landing.
		// Pre-dispatch repo exploration is out of legwork's scope by design and
		// deliberately not counted here.
		if cmd := gitAfterClose(filepath.Join(filepath.Dir(d.shimCfg), "orchestrator-raw.json")); cmd != "" {
			return fail("git after close: %s", firstN(cmd, 120))
		}
	default:
		return fail("unknown check kind %q", c.Kind)
	}
	out.OK = true
	return out
}

var gitCommand = regexp.MustCompile(`(^|[\s;&|(])git(\s|$)`)

// gitAfterClose walks the orchestrator transcript's Bash tool calls in order
// and returns the first git command attempted after a `legwork close`
// invocation ("" when none, including when no transcript exists — the cmd:
// driver keeps none and its script is the trusted harness self-test).
// Denied attempts count: the reach is the signal, not whether the sandbox
// let it through.
func gitAfterClose(rawPath string) string {
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		return ""
	}
	var msgs []struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Input struct {
					Command string `json:"command"`
				} `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &msgs) != nil {
		return ""
	}
	seenClose := false
	for _, m := range msgs {
		if m.Type != "assistant" {
			continue
		}
		for _, c := range m.Message.Content {
			if c.Type != "tool_use" || c.Name != "Bash" {
				continue
			}
			cmd := c.Input.Command
			if strings.Contains(cmd, "legwork close") {
				seenClose = true
				continue
			}
			if seenClose && !strings.Contains(cmd, "legwork") && gitCommand.MatchString(cmd) {
				return cmd
			}
		}
	}
	return ""
}

func findEvent(evs []eventLine, typ, pattern string) *eventLine {
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		if re, err = regexp.Compile("(?i)" + pattern); err != nil {
			return nil
		}
	}
	for i := range evs {
		if evs[i].Type != typ {
			continue
		}
		if re == nil || re.MatchString(evs[i].Preview) {
			return &evs[i]
		}
	}
	return nil
}

func readMeta(stateDir, jobID string) (*jobMeta, error) {
	raw, err := os.ReadFile(filepath.Join(stateDir, "jobs", jobID, "meta.json"))
	if err != nil {
		return nil, err
	}
	var m jobMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func readEvents(stateDir, jobID string) ([]eventLine, error) {
	f, err := os.Open(filepath.Join(stateDir, "jobs", jobID, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var evs []eventLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev eventLine
		if err := json.Unmarshal(sc.Bytes(), &ev); err == nil {
			evs = append(evs, ev)
		}
	}
	return evs, sc.Err()
}

func readInvocations(path string, failedOnly bool) []invocation {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var invs []invocation
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var inv invocation
		if err := json.Unmarshal(sc.Bytes(), &inv); err != nil {
			continue
		}
		if failedOnly && inv.Exit == 0 {
			continue
		}
		invs = append(invs, inv)
	}
	return invs
}

func computeMetrics(d *runDirs, m *mapping) metrics {
	all := readInvocations(d.invLog, false)
	mt := metrics{Invocations: len(all), JobsDispatched: len(m.Jobs), ResponseLatencyS: map[string]float64{}}
	for _, inv := range all {
		if inv.Exit != 0 {
			mt.Fumbles++
			mt.FumbleArgv = append(mt.FumbleArgv, strings.Join(inv.Argv, " "))
		}
	}
	// Latency: worker raised its hand (needs-input / needs-provision) →
	// orchestrator responded (answer / approve / resume). Multiple raises
	// per job report the first pair only — enough for v1.
	for id, mj := range m.Jobs {
		evs, err := readEvents(d.state, id)
		if err != nil {
			continue
		}
		var raised *eventLine
	scan:
		for i := range evs {
			switch evs[i].Type {
			case "needs-input", "needs-provision":
				if raised == nil {
					raised = &evs[i]
				}
			case "answer", "approve", "resume":
				if raised != nil {
					mt.ResponseLatencyS[mj.Spec] = evs[i].TS.Sub(raised.TS).Seconds()
					break scan
				}
			}
		}
	}
	return mt
}

func quizPass(q *Quiz, answer string) bool {
	if answer == "" {
		return false
	}
	for _, p := range q.ExpectRegex {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil || !re.MatchString(answer) {
			return false
		}
	}
	return true
}
