// Package main is the legwork orchestration eval harness (Layer 0/1).
//
// The subject under test is an orchestrator model driving the real legwork
// CLI; workers are the scripted fake agent, so a run costs only orchestrator
// tokens. The same binary doubles as the `legwork` PATH shim inside the
// orchestrator's environment: every invocation is logged (fumble-rate ground
// truth) and each turn-spawning command gets LEGWORK_FAKE_SCRIPT pointed at
// the scenario's script for that job's next turn — resumes inherit the
// invoking command's env, so per-invocation routing is race-free.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Scenario is the on-disk spec (scenario.json in the scenario dir).
type Scenario struct {
	Name string `json:"name"`
	// Goal is the orchestrator's prompt. The harness preamble (fake-agent
	// instructions, autonomy framing) is prepended by the runner.
	Goal string `json:"goal"`
	// Jobs maps worker roles to turn scripts. Match is a regex tried against
	// the full argv of `legwork run`; first match wins.
	Jobs []JobSpec `json:"jobs"`
	// ReviewScript serves any `ws review` dispatch (path relative to the
	// scenario dir). Empty means reviews replay the default done script.
	ReviewScript string `json:"review_script,omitempty"`
	// ReviewScripts, when set, serve successive `ws review` dispatches in
	// order (the last repeats). This is how a scenario stages a FIX verdict
	// followed by a SHIP on re-review.
	ReviewScripts []string `json:"review_scripts,omitempty"`
	// GitFixture provisions ./repo (init + initial commit on main) in the
	// scenario workdir for workspace flows.
	GitFixture bool `json:"git_fixture,omitempty"`
	// TimeoutS bounds the orchestrator process wall clock (default 600).
	TimeoutS int `json:"timeout_s,omitempty"`
	// Quiz, when set, is asked in a resumed orchestrator session after the
	// main run, scoring situational awareness from memory.
	Quiz   *Quiz   `json:"quiz,omitempty"`
	Checks []Check `json:"checks"`
}

type JobSpec struct {
	Name  string   `json:"name"`
	Match string   `json:"match"`
	Turns []string `json:"turns"`
	// Branches, when set, pick the post-answer turn script from what the
	// orchestrator actually answered — this is what makes "guessing instead
	// of consulting the constraints" produce a detectably different worker
	// outcome. First matching regex wins; no match falls through to Turns.
	Branches []AnswerBranch `json:"on_answer,omitempty"`
}

type AnswerBranch struct {
	Regex  string `json:"regex"`
	Script string `json:"script"`
}

type Quiz struct {
	Ask string `json:"ask"`
	// ExpectRegex must all match the quiz answer (case-insensitive unless
	// the pattern says otherwise).
	ExpectRegex []string `json:"expect_regex"`
}

// Check is one declarative assertion scored against the state dir, the
// invocation log, or the workdir after the orchestrator finishes.
type Check struct {
	// Kind: job_state | event | no_event | file_contains | max_fumbles |
	// max_denials | min_jobs
	Kind string `json:"kind"`
	// Job is a JobSpec name (resolved to the real job ID via the shim's
	// mapping). Used by job_state, event, no_event.
	Job string `json:"job,omitempty"`
	// Want is the target state for job_state.
	Want string `json:"want,omitempty"`
	// Type is the event type for event / no_event.
	Type string `json:"type,omitempty"`
	// Regex is matched against the event preview (event) or file content
	// (file_contains).
	Regex string `json:"regex,omitempty"`
	// Path is workdir-relative for file_contains.
	Path string `json:"path,omitempty"`
	// N is the threshold for max_fumbles.
	N int `json:"n,omitempty"`
}

func loadScenario(dir string) (*Scenario, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		return nil, err
	}
	var s Scenario
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if s.Name == "" {
		s.Name = filepath.Base(dir)
	}
	if s.TimeoutS == 0 {
		s.TimeoutS = 600
	}
	for _, j := range s.Jobs {
		for _, t := range j.Turns {
			if _, err := os.Stat(filepath.Join(dir, t)); err != nil {
				return nil, fmt.Errorf("%s: job %s turn script: %w", s.Name, j.Name, err)
			}
		}
	}
	return &s, nil
}
