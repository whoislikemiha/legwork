package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func routeNotifyConfig(sink string, events ...string) string {
	quoted := make([]string, len(events))
	for i, event := range events {
		quoted[i] = fmt.Sprintf("%q", event)
	}
	return fmt.Sprintf("[notify]\ncommand = '''payload=$(cat); printf '%%s\\t%%s\\n' \"${LEGWORK_TEST_ROUTE-absent}\" \"$payload\" >> %s'''\nevents = [%s]\ncapture_env = [\"LEGWORK_TEST_ROUTE\"]\n",
		sink, strings.Join(quoted, ", "))
}

func commandWithEnvironment(e *env, set map[string]string, unset []string, args ...string) (string, error) {
	remove := map[string]bool{"LEGWORK_STATE_DIR": true, "LEGWORK_FAKE_SCRIPT": true, "LEGWORK_CONFIG": true}
	for key := range set {
		remove[key] = true
	}
	for _, key := range unset {
		remove[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(set)+3)
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if !remove[name] {
			env = append(env, item)
		}
	}
	env = append(env, "LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script)
	if e.config != "" {
		env = append(env, "LEGWORK_CONFIG="+e.config)
	}
	for key, value := range set {
		env = append(env, key+"="+value)
	}
	cmd := exec.Command(binPath, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type routedNotification struct {
	Route   string
	Payload map[string]any
}

func waitRoutedNotifications(t *testing.T, sink string, count int) []routedNotification {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(sink)
		if err == nil {
			var got []routedNotification
			sc := bufio.NewScanner(strings.NewReader(string(data)))
			for sc.Scan() {
				route, body, ok := strings.Cut(sc.Text(), "\t")
				if !ok {
					continue
				}
				var payload map[string]any
				if json.Unmarshal([]byte(body), &payload) == nil {
					got = append(got, routedNotification{Route: route, Payload: payload})
				}
			}
			if len(got) >= count {
				return got
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d routed notifications", count)
	return nil
}

const resultWithContext = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"total_cost_usd":0.05,"usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":5000,"cache_read_input_tokens":140000},"session_id":"s9","result":"ok\n\nstate: done"}`

func TestRunGroupingNoteAndNotifier(t *testing.T) {
	e := newEnv(t)

	// Notifier config: append each payload to a file.
	sink := filepath.Join(t.TempDir(), "notifications.jsonl")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := "[notify]\ncommand = \"cat >> " + sink + "\"\nevents = [\"done\", \"needs-input\"]\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	e.writeScript(t, resultWithContext)
	cmd := exec.Command(binPath, "run", "--agent", "fake", "--run", "pipeline-x", "phase one")
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state,
		"LEGWORK_FAKE_SCRIPT="+e.script,
		"LEGWORK_CONFIG="+cfgPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	e.waitState(t, id, "done")

	// Orchestrator narration lands in the run log alongside lifecycle markers.
	e.legwork(t, "note", "pipeline-x", "phase one done, starting review")
	runEvents := e.legwork(t, "events", "pipeline-x", "--run")
	for _, want := range []string{"queued", "finished", "note", "phase one done"} {
		if !strings.Contains(runEvents, want) {
			t.Fatalf("run log missing %q:\n%s", want, runEvents)
		}
	}

	// Notifier fired with the payload (runner env carried LEGWORK_CONFIG).
	deadline := time.Now().Add(5 * time.Second)
	var payload map[string]any
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(sink); err == nil && len(data) > 0 {
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("bad notification: %v\n%s", err, data)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if payload == nil {
		t.Fatal("notifier never fired")
	}
	if payload["event"] != "done" || payload["job"] != id || payload["run"] != "pipeline-x" {
		t.Fatalf("payload = %v", payload)
	}

	// Context tracked from cache usage: 200 + 5000 + 140000.
	if payload["context"].(float64) != 145200 {
		t.Fatalf("context = %v, want 145200", payload["context"])
	}
	// Raw tokens, no percentage: window sizes are model-dependent.
	status := e.legwork(t, "status", id)
	if !strings.Contains(status, "context: 145k") || strings.Contains(status, "%") {
		t.Fatalf("status context format wrong:\n%s", status)
	}
}

func TestNotifierUnsubscribedEventSilent(t *testing.T) {
	e := newEnv(t)
	sink := filepath.Join(t.TempDir(), "n.jsonl")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	// Subscribed to needs-input only; a done job must not notify.
	if err := os.WriteFile(cfgPath, []byte("[notify]\ncommand = \"cat >> "+sink+"\"\nevents = [\"needs-input\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, resultDone)
	cmd := exec.Command(binPath, "run", "--agent", "fake", "quiet job")
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script, "LEGWORK_CONFIG="+cfgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	e.waitState(t, strings.TrimSpace(string(out)), "done")
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(sink); !os.IsNotExist(err) {
		t.Fatal("notifier fired for an unsubscribed event")
	}
}

func TestNeedsProvisionNotifiesBlockedSubscribers(t *testing.T) {
	e := newEnv(t)
	sink := filepath.Join(t.TempDir(), "n.jsonl")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[notify]\ncommand = \"cat >> "+sink+"\"\nevents = [\"blocked\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t,
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-provision","result":"needs host command\n\nstate: blocked\nblocked: {\"kind\":\"provision\",\"command\":\"printf ok > provisioned.txt\"}"}`,
	)
	cmd := exec.Command(binPath, "run", "--agent", "fake", "provision")
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script, "LEGWORK_CONFIG="+cfgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	id := strings.TrimSpace(string(out))
	e.waitState(t, id, "blocked")

	deadline := time.Now().Add(5 * time.Second)
	var payload map[string]any
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(sink); err == nil && len(data) > 0 {
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("bad notification: %v\n%s", err, data)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if payload == nil {
		t.Fatal("blocked subscriber did not receive needs-provision")
	}
	if payload["event"] != "needs-provision" || payload["job"] != id {
		t.Fatalf("payload = %v", payload)
	}
	blocked, ok := payload["blocked"].(map[string]any)
	if !ok || blocked["kind"] != "provision" || blocked["command"] != "printf ok > provisioned.txt" {
		t.Fatalf("blocked payload = %v", payload["blocked"])
	}
}

func TestNotifierOriginSurvivesResumeAndStaysOutOfWorkerAndPublicSurfaces(t *testing.T) {
	e := newEnv(t)
	sink := filepath.Join(t.TempDir(), "routed.jsonl")
	e.config = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(e.config, []byte(routeNotifyConfig(sink, "needs-input", "done")), 0o644); err != nil {
		t.Fatal(err)
	}
	const routeA = "session-A-$HOME;opaque"
	e.writeScript(t,
		"#require-env-absent LEGWORK_TEST_ROUTE",
		"#require-env UNRELATED_WORKER_VALUE=kept",
		`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-origin","result":"state: needs-input\nquestion: continue?"}`,
	)
	out, err := commandWithEnvironment(e, map[string]string{"LEGWORK_TEST_ROUTE": routeA, "UNRELATED_WORKER_VALUE": "kept"}, nil,
		"run", "--agent", "fake", "origin test")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	id := strings.TrimSpace(out)
	e.waitState(t, id, "needs-input")

	e.writeScript(t, "#require-env-absent LEGWORK_TEST_ROUTE", resultDone)
	out, err = commandWithEnvironment(e, map[string]string{"LEGWORK_TEST_ROUTE": "session-B"}, nil,
		"resume", id, "continue")
	if err != nil {
		t.Fatalf("resume: %v\n%s", err, out)
	}
	e.waitState(t, id, "done")

	notifications := waitRoutedNotifications(t, sink, 2)
	for _, n := range notifications {
		if n.Route != routeA {
			t.Fatalf("notification route = %q, want immutable %q", n.Route, routeA)
		}
		encoded, _ := json.Marshal(n.Payload)
		if strings.Contains(string(encoded), routeA) {
			t.Fatalf("notifier JSON exposed captured value: %s", encoded)
		}
	}
	for _, surface := range []string{
		e.legwork(t, "status", id, "--json"),
		e.legwork(t, "events", id, "--json"),
		mustReadString(t, filepath.Join(e.state, "jobs", id, "transcript.jsonl")),
	} {
		if strings.Contains(surface, routeA) {
			t.Fatal("public job surface exposed captured value")
		}
	}
}

func TestNotifierOriginAbsentRemainsAbsent(t *testing.T) {
	e := newEnv(t)
	sink := filepath.Join(t.TempDir(), "routed.jsonl")
	e.config = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(e.config, []byte(routeNotifyConfig(sink, "needs-input", "done")), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-absent","result":"state: needs-input\nquestion: continue?"}`)
	out, err := commandWithEnvironment(e, nil, []string{"LEGWORK_TEST_ROUTE"}, "run", "--agent", "fake", "absent origin")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out)
	e.waitState(t, id, "needs-input")
	e.writeScript(t, resultDone)
	if out, err := commandWithEnvironment(e, map[string]string{"LEGWORK_TEST_ROUTE": "session-B"}, nil, "resume", id, "continue"); err != nil {
		t.Fatalf("resume: %v\n%s", err, out)
	}
	e.waitState(t, id, "done")
	for _, n := range waitRoutedNotifications(t, sink, 2) {
		if n.Route != "absent" {
			t.Fatalf("absent origin became %q", n.Route)
		}
	}
}

func TestSimultaneousJobsDoNotCrossNotifierOrigins(t *testing.T) {
	e := newEnv(t)
	sink := filepath.Join(t.TempDir(), "routed.jsonl")
	e.config = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(e.config, []byte(routeNotifyConfig(sink, "done")), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, resultDone)
	type result struct {
		out string
		err error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, route := range []string{"route-A", "route-B"} {
		wg.Add(1)
		go func(route string) {
			defer wg.Done()
			out, err := commandWithEnvironment(e, map[string]string{"LEGWORK_TEST_ROUTE": route}, nil, "run", "--agent", "fake", route)
			results <- result{out, err}
		}(route)
	}
	wg.Wait()
	close(results)
	jobs := map[string]string{}
	for r := range results {
		if r.err != nil {
			t.Fatalf("parallel run: %v\n%s", r.err, r.out)
		}
		id := strings.TrimSpace(r.out)
		meta := e.waitState(t, id, "done")
		jobs[id] = meta["task"].(string)
	}
	for _, n := range waitRoutedNotifications(t, sink, 2) {
		id := n.Payload["job"].(string)
		if n.Route != jobs[id] {
			t.Fatalf("job %s route = %q, want %q", id, n.Route, jobs[id])
		}
	}
}

func mustReadString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
