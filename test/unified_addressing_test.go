package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnifiedJobAndRunAddressing(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"session_id":"one","result":"first\n\nstate: done"}`)
	first := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "job-1", "first"))
	e.waitState(t, first, "done")
	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"session_id":"two","result":"second\n\nstate: done"}`)
	second := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--run", "job-1", "second"))
	e.waitState(t, second, "done")

	var exact, forced map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "status", "job-1", "--json")), &exact); err != nil {
		t.Fatal(err)
	}
	if exact["selector"] != "job-1" || exact["selector_kind"] != "job" || exact["resolved_job"] != first {
		t.Fatalf("automatic selector did not prefer job ID: %+v", exact)
	}
	if err := json.Unmarshal([]byte(e.legwork(t, "status", "--run", "job-1", "--json")), &forced); err != nil {
		t.Fatal(err)
	}
	if forced["selector_kind"] != "run" || forced["resolved_job"] != second {
		t.Fatalf("forced run selector did not choose newest job: %+v", forced)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(e.legwork(t, "result", "--run", "job-1", "--json")), &result); err != nil {
		t.Fatal(err)
	}
	if result["selector"] != "job-1" || result["selector_kind"] != "run" || result["resolved_job"] != second || result["result"] != "second" {
		t.Fatalf("run result envelope = %+v", result)
	}

	type runEvent struct {
		Seq    int            `json:"seq"`
		Type   string         `json:"type"`
		Fields map[string]any `json:"fields"`
	}
	var runEvents []runEvent
	if err := json.Unmarshal([]byte(e.legwork(t, "events", "job-1", "--run", "--json")), &runEvents); err != nil {
		t.Fatal(err)
	}
	if len(runEvents) < 2 || runEvents[0].Fields["job"] != first || runEvents[1].Fields["job"] != first {
		t.Fatalf("run event index lost its raw event shape or job markers: %+v", runEvents)
	}
	if runEvents[0].Seq != 1 || runEvents[0].Type != "queued" {
		t.Fatalf("unexpected run event cursor/index: %+v", runEvents[0])
	}
	var afterFirst []runEvent
	if err := json.Unmarshal([]byte(e.legwork(t, "events", "job-1", "--run", "--since", "1", "--json")), &afterFirst); err != nil {
		t.Fatal(err)
	}
	if len(afterFirst) == 0 || afterFirst[0].Seq != 2 {
		t.Fatalf("run --since did not retain per-log cursor semantics: %+v", afterFirst)
	}
	legacyEvents := e.legwork(t, "events", "job-1", "--run", "--json")
	if strings.Contains(legacyEvents, "__positional_run__") {
		t.Fatalf("legacy events selector exposed an implementation sentinel:\n%s", legacyEvents)
	}

	jsonlJobs := func(output string) map[string]bool {
		jobs := map[string]bool{}
		output = strings.TrimSpace(output)
		if output == "" {
			return jobs
		}
		for _, line := range strings.Split(output, "\n") {
			var item struct {
				Job string `json:"job"`
			}
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				t.Fatalf("tail emitted invalid JSONL: %v\n%s", err, output)
			}
			if item.Job != "" {
				jobs[item.Job] = true
			}
		}
		return jobs
	}
	tail := e.legwork(t, "tail", "job-1", "--until-idle", "--json", "-n", "30")
	if jobs := jsonlJobs(tail); jobs[second] || !jobs[first] {
		t.Fatalf("positional tail did not prefer exact job ID:\n%s", tail)
	}
	tail = e.legwork(t, "tail", "--run", "job-1", "--until-idle", "--json", "-n", "30")
	if !jsonlJobs(tail)[second] {
		t.Fatalf("run tail missing newest job:\n%s", tail)
	}

	if out, err := e.legworkErr("status", "job-1", "--job", "job-1"); err == nil || !strings.Contains(out, "cannot be combined") {
		t.Fatalf("positional + forced selector should fail, got: %s", out)
	}
	e.legwork(t, "note", "log-only-run", "created without jobs")
	if out := e.legwork(t, "events", "log-only-run"); !strings.Contains(out, "created without jobs") {
		t.Fatalf("log-only run should be a valid events scope, got: %s", out)
	}
	for _, verb := range []string{"status", "result"} {
		if out, err := e.legworkErr(verb, "log-only-run"); err == nil || !strings.Contains(out, "has no jobs") {
			t.Fatalf("%s should reject a log-only run clearly, got: %s", verb, out)
		}
	}
	if out, err := e.legworkErr("events", "missing-run"); err == nil || !strings.Contains(out, "no job or run event log") {
		t.Fatalf("missing selector should fail clearly, got: %s", out)
	}
}
