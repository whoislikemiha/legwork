package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	status := e.legwork(t, "status", id)
	if !strings.Contains(status, "context: 145k(72%)") {
		t.Fatalf("status missing context health:\n%s", status)
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
