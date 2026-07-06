package timeline

import (
	"os"
	"sort"
	"strings"
	"time"

	"github.com/whoislikemiha/legwork/internal/events"
	"github.com/whoislikemiha/legwork/internal/job"
)

// RunRollup is the per-run overview: how its jobs are distributed across states,
// the run's total spend, whether any live job's context is high, when it last
// moved, and the most recent orchestrator note. The empty label ("") is the
// catch-all bucket for jobs dispatched without a --run.
type RunRollup struct {
	Label       string         `json:"label"`
	Jobs        map[string]int `json:"jobs"` // state -> count
	CostUSD     float64        `json:"cost_usd"`
	ContextHigh bool           `json:"context_high"`
	Updated     time.Time      `json:"updated"`
	LastNote    string         `json:"last_note,omitempty"`
}

// RunLog is the slice of a run's event log the rollup needs: its most recent
// note and its most recent activity timestamp.
type RunLog struct {
	LastNote     string
	LastNoteTime time.Time
	LastActivity time.Time
}

// LoadRunLog reads a run's events.jsonl and derives its RunLog. A missing file
// yields a zero RunLog and no error (a run label can exist in job metas before
// its log does).
func LoadRunLog(path string) (RunLog, error) {
	var rl RunLog
	evs, err := events.Read(path, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return rl, nil
		}
		return rl, err
	}
	for _, e := range evs {
		if e.Time.After(rl.LastActivity) {
			rl.LastActivity = e.Time
		}
		if e.Type == events.TypeNote {
			rl.LastNote = e.Preview
			rl.LastNoteTime = e.Time
		}
	}
	return rl, nil
}

// Rollups groups job metas by run label and computes one RunRollup per label
// (including "" for the no-run bucket), folding in each run's RunLog for note
// and activity. Context-high is true when any non-closed job crosses the health
// threshold (0 disables it). The result is sorted by most recent activity,
// newest first. This is the single function `runs` and the dashboard header
// share, so the two can never disagree.
func Rollups(metas []*job.Meta, runLogs map[string]RunLog, threshold int) []RunRollup {
	byLabel := map[string]*RunRollup{}
	get := func(label string) *RunRollup {
		r := byLabel[label]
		if r == nil {
			r = &RunRollup{Label: label, Jobs: map[string]int{}}
			byLabel[label] = r
		}
		return r
	}
	for _, m := range metas {
		r := get(m.Run)
		r.Jobs[string(m.State)]++
		r.CostUSD += m.CostUSD
		if m.Updated.After(r.Updated) {
			r.Updated = m.Updated
		}
		if m.State != job.StateClosed && m.ContextHigh(threshold) {
			r.ContextHigh = true
		}
	}
	// Fold in run-log data. A run may have a log with no (surviving) jobs, so
	// ensure a rollup exists for every logged label too — except "", which is
	// the no-run bucket and never has a run log.
	for label, rl := range runLogs {
		if label == "" {
			continue
		}
		r := get(label)
		r.LastNote = rl.LastNote
		if rl.LastActivity.After(r.Updated) {
			r.Updated = rl.LastActivity
		}
	}

	out := make([]RunRollup, 0, len(byLabel))
	for _, r := range byLabel {
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated) // newest first
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// RunLogs loads the derived RunLog for every discovered run label. It is the
// companion to Rollups: `runs` and the dashboard both build the rollup input
// from it.
func RunLogs(s *job.Store) (map[string]RunLog, error) {
	labels, err := RunLabels(s)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RunLog, len(labels))
	for _, l := range labels {
		rl, err := LoadRunLog(runEventsPath(s, l))
		if err != nil {
			return nil, err
		}
		out[l] = rl
	}
	return out, nil
}

// statePriority orders states worst-first for the STATE rollup: the states a
// human must act on (needs-input, blocked, failed, auth-required) surface ahead
// of in-flight and terminal-ok ones. Unknown states sort last.
var statePriority = map[job.State]int{
	job.StateNeedsInput:  0,
	job.StateBlocked:     1,
	job.StateFailed:      2,
	job.StateAuthNeeded:  3,
	job.StateInterrupted: 4,
	job.StateActive:      5,
	job.StateQueued:      6,
	job.StateDone:        7,
	job.StateClosed:      8,
}

func stateRank(s string) int {
	if p, ok := statePriority[job.State(s)]; ok {
		return p
	}
	return len(statePriority)
}

// StateSummary renders a job-state-count map worst-first. When every job shares
// one state, it prints the bare state name ("done"); otherwise it prints the
// per-state counts joined by " · " ("1 needs-input · 1 active · 2 done"). Empty
// input yields "".
func StateSummary(jobs map[string]int) string {
	total := 0
	states := make([]string, 0, len(jobs))
	for s, n := range jobs {
		if n == 0 {
			continue
		}
		total += n
		states = append(states, s)
	}
	if total == 0 {
		return ""
	}
	sort.SliceStable(states, func(i, j int) bool {
		if stateRank(states[i]) != stateRank(states[j]) {
			return stateRank(states[i]) < stateRank(states[j])
		}
		return states[i] < states[j]
	})
	if len(states) == 1 {
		return states[0]
	}
	parts := make([]string, len(states))
	for i, s := range states {
		parts[i] = itoa(jobs[s]) + " " + s
	}
	return strings.Join(parts, " · ")
}

// itoa avoids an fmt import for the hot rollup path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
