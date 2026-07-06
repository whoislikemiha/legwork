package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// doctor runs the doctor command with the env's state/script plus any extra
// env, returning combined output and the process exit code.
func (e *env) doctor(extraEnv []string, args ...string) (string, int) {
	cmd := exec.Command(binPath, append([]string{"doctor"}, args...)...)
	cmd.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state,
		"LEGWORK_FAKE_SCRIPT="+e.script,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code
}

func TestDoctorHealthy(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	// --dir at a clean temp dir keeps the workstree check deterministic (skip).
	out, code := e.doctor(nil, "--agent", "fake", "--dir", t.TempDir(), "--json")
	if code != 0 {
		t.Fatalf("want exit 0, got %d\n%s", code, out)
	}
	var rep struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name, Status, Detail string
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if !rep.OK {
		t.Fatalf("want ok:true\n%s", out)
	}
	byName := map[string]string{}
	for _, c := range rep.Checks {
		byName[c.Name] = c.Status
	}
	for _, name := range []string{"state-dir", "git", "agent", "probe", "workstree", "notifier"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing check %q\n%s", name, out)
		}
	}
	if byName["probe"] != "ok" {
		t.Fatalf("probe should be ok with a done script: %s", byName["probe"])
	}
}

func TestDoctorProbeFailure(t *testing.T) {
	e := newEnv(t)
	// A result line that errors with an auth marker -> probe must fail.
	e.writeScript(t,
		`{"type":"result","subtype":"error","is_error":true,"result":"oauth token has expired","session_id":"s1"}`,
	)
	out, code := e.doctor(nil, "--agent", "fake", "--dir", t.TempDir(), "--json")
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, `"name": "probe"`) || !strings.Contains(out, "auth-required") {
		t.Fatalf("probe failure not surfaced:\n%s", out)
	}
}

func TestDoctorNoProbe(t *testing.T) {
	e := newEnv(t)
	// No script needed: --no-probe must not spawn the agent.
	out, code := e.doctor(nil, "--agent", "fake", "--dir", t.TempDir(), "--no-probe")
	if code != 0 {
		t.Fatalf("want exit 0, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "probe") || !strings.Contains(out, "skip") {
		t.Fatalf("probe should be skipped:\n%s", out)
	}
}

func TestDoctorProbeTimeout(t *testing.T) {
	e := newEnv(t)
	// Agent sleeps past the probe deadline without ever emitting a result:
	// the watchdog must kill it and report a timeout.
	e.writeScript(t,
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		"#sleep 5000",
		resultDone,
	)
	out, code := e.doctor([]string{"LEGWORK_DOCTOR_PROBE_TIMEOUT=300ms"},
		"--agent", "fake", "--dir", t.TempDir(), "--json")
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, `"name": "probe"`) || !strings.Contains(out, "timed out") {
		t.Fatalf("probe timeout not surfaced:\n%s", out)
	}
}

func TestDoctorUnknownAgent(t *testing.T) {
	e := newEnv(t)
	out, code := e.doctor(nil, "--agent", "nope")
	if code != 2 {
		t.Fatalf("unknown agent should be a usage error (exit 2), got %d\n%s", code, out)
	}
}

func TestDoctorNotifierPayload(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	sink := filepath.Join(t.TempDir(), "payload.json")
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := "[notify]\ncommand = \"cat > " + sink + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := e.doctor([]string{"LEGWORK_CONFIG=" + cfgPath},
		"--agent", "fake", "--dir", t.TempDir())
	if code != 0 {
		t.Fatalf("want exit 0, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "notifier") || !strings.Contains(out, "ok") {
		t.Fatalf("notifier check not ok:\n%s", out)
	}
	data, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("notifier command did not run: %v", err)
	}
	var p map[string]any
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("bad payload: %v\n%s", err, data)
	}
	if p["event"] != "doctor" {
		t.Fatalf("payload event = %v, want doctor\n%s", p["event"], data)
	}
}
