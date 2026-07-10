package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const resultBlockedVerify = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-verify","result":"host checks need the real cache\n\nstate: blocked\nblocked: {\"kind\":\"verify\",\"detail\":\"go test needs writable cache\"}"}`

func blockedVerifyWorkspace(t *testing.T) (*env, string, map[string]any) {
	t.Helper()
	e := newEnv(t)
	ws := e.wsNew(t, initRepo(t))
	e.writeScript(t, resultBlockedVerify)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", ws["id"].(string), "run host checks"))
	e.waitState(t, id, "blocked")
	return e, id, ws
}

func decodeVerify(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad verification JSON: %v\n%s", err, out)
	}
	return got
}

func TestVerifyPassRecordsReceiptWithoutRewritingWorker(t *testing.T) {
	e, id, ws := blockedVerifyWorkspace(t)
	sink := filepath.Join(t.TempDir(), "verification-notifications.jsonl")
	const secret = "verification-secret-value"
	old, hadOld := os.LookupEnv("VERIFY_SECRET_TOKEN")
	if err := os.Setenv("VERIFY_SECRET_TOKEN", secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv("VERIFY_SECRET_TOKEN", old)
		} else {
			_ = os.Unsetenv("VERIFY_SECRET_TOKEN")
		}
	})

	// No implicit shell: this semicolon is plain argv data, not a second command.
	out := e.legwork(t, "verify", id, "--json", "--", "printf", "literal; still argv")
	first := decodeVerify(t, out)
	if first["ok"] != true {
		t.Fatalf("literal argv verification failed: %v", first)
	}
	firstReceiptID := first["receipt"].(map[string]any)["receipt_id"].(string)
	e.config = filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(e.config, []byte("[notify]\ncommand = \"cat >> "+sink+"\"\nevents = [\"verification-passed\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out = e.legwork(t, "verify", id, "--json", "--", "sh", "-lc", "printf '%s\\n' \"$VERIFY_SECRET_TOKEN\"")
	got := decodeVerify(t, out)
	if got["ok"] != true {
		t.Fatalf("verification did not pass: %v", got)
	}
	receipt := got["receipt"].(map[string]any)
	if receipt["receipt_id"] == firstReceiptID {
		t.Fatalf("repeated attempts reused receipt ID %q", firstReceiptID)
	}
	if receipt["job"] != id || receipt["workspace"] != ws["id"] || receipt["cwd"] != ws["tree"] || receipt["passed"] != true {
		t.Fatalf("receipt identity = %v", receipt)
	}
	if receipt["turn"] != float64(1) || receipt["checkpoint_ref"] == "" || receipt["checkpoint_oid"] == "" || receipt["diff_sha256"] == "" {
		t.Fatalf("receipt is not bound to the blocked turn and tested tree: %v", receipt)
	}
	argv := receipt["argv"].([]any)
	if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-lc" {
		t.Fatalf("argv was not preserved: %v", argv)
	}
	if output := receipt["output"].(string); strings.Contains(output, secret) || !strings.Contains(output, "[REDACTED]") {
		t.Fatalf("secret was not redacted: %q", output)
	}
	if retry := got["retry"].([]any); len(retry) != 7 || retry[0] != "legwork" || retry[2] != id || retry[3] != "--" {
		t.Fatalf("retry argv = %v", retry)
	}

	status := e.waitState(t, id, "blocked")
	if status["blocked"].(map[string]any)["kind"] != "verify" || status["result"] != "host checks need the real cache" {
		t.Fatalf("worker outcome was rewritten: %v", status)
	}
	latest := status["latest_verification"].(map[string]any)
	if latest["receipt_id"] != receipt["receipt_id"] || latest["passed"] != true {
		t.Fatalf("job rollup = %v", latest)
	}
	wm := e.wsStatus(t, ws["id"].(string))
	if wr := wm["latest_verification"].(map[string]any); wr["receipt_id"] != receipt["receipt_id"] || wr["passed"] != true {
		t.Fatalf("workspace rollup = %v", wr)
	}
	for _, evs := range []string{e.legwork(t, "events", id, "--json"), e.legwork(t, "events", ws["id"].(string), "--workspace", "--json")} {
		if !strings.Contains(evs, `"type": "verification"`) || !strings.Contains(evs, receipt["receipt_id"].(string)) || !strings.Contains(evs, firstReceiptID) {
			t.Fatalf("verification history missing receipt:\n%s", evs)
		}
	}
	if human := e.legwork(t, "status", id); !strings.Contains(human, "verification: passed (reviewable)") {
		t.Fatalf("human status missing passed receipt:\n%s", human)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(sink); err == nil && len(data) > 0 {
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil || payload["event"] != "verification-passed" || payload["verification"].(map[string]any)["passed"] != true {
				t.Fatalf("verification notification = %v err=%v", payload, err)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("verification pass did not notify")
}

func TestVerifyFailureAndTimeoutRemainAttentionWithRetry(t *testing.T) {
	e, id, ws := blockedVerifyWorkspace(t)
	out, err := e.legworkErr("verify", id, "--json", "--", "sh", "-lc", "printf fail; exit 7")
	if err == nil {
		t.Fatalf("failing verification succeeded:\n%s", out)
	}
	got := decodeVerify(t, out)
	receipt := got["receipt"].(map[string]any)
	if got["ok"] != false || receipt["passed"] != false || receipt["exit_code"].(float64) != 7 || !strings.Contains(receipt["output"].(string), "fail") {
		t.Fatalf("failed receipt = %v", got)
	}
	if human, err := e.legworkErr("verify", id, "--timeout", "40ms", "--", "sh", "-lc", "sleep 3 & wait"); err == nil || !strings.Contains(human, "verification timed out") {
		t.Fatalf("timeout output/exit = %v\n%s", err, human)
	}
	status := e.waitState(t, id, "blocked")
	latest := status["latest_verification"].(map[string]any)
	if latest["timed_out"] != true || latest["passed"] != false {
		t.Fatalf("timeout was not the latest attention receipt: %v", latest)
	}
	if list := e.legwork(t, "ls", "--workspace", ws["id"].(string)); !strings.Contains(list, "blocked") || !strings.Contains(list, "verification timed out") || !strings.Contains(list, "legwork verify "+id) {
		t.Fatalf("failed verification was hidden from job attention:\n%s", list)
	}
}

func TestVerifyRefusesActiveWrongKindAndMissingWorkspace(t *testing.T) {
	e, id, ws := blockedVerifyWorkspace(t)
	e.writeScript(t, "#sleep 600", resultDone)
	active := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", ws["id"].(string), "other work"))
	if out, err := e.legworkErr("verify", id, "--", "true"); err == nil || !strings.Contains(out, "active job "+active) {
		t.Fatalf("active-workspace refusal = %v\n%s", err, out)
	}
	e.waitState(t, active, "done")

	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s-provision","result":"host install\n\nstate: blocked\nblocked: {\"kind\":\"provision\",\"command\":\"true\"}"}`)
	wrong := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", ws["id"].(string), "provision instead"))
	e.waitState(t, wrong, "blocked")
	if out, err := e.legworkErr("verify", wrong, "--", "true"); err == nil || !strings.Contains(out, "not blocked.kind=verify") {
		t.Fatalf("human wrong-kind refusal = %v\n%s", err, out)
	}
	if out, err := e.legworkErr("verify", wrong, "--json", "--", "true"); err == nil {
		t.Fatalf("wrong blocked kind succeeded:\n%s", out)
	} else if got := decodeVerify(t, out); got["state"] != "blocked" || got["blocked"].(map[string]any)["kind"] != "wrong-blocked-kind" {
		t.Fatalf("wrong blocked kind refusal = %v", got)
	}

	if err := os.RemoveAll(filepath.Join(e.state, "workspaces", ws["id"].(string))); err != nil {
		t.Fatal(err)
	}
	if out, err := e.legworkErr("verify", id, "--", "true"); err == nil || !strings.Contains(out, "workspace "+ws["id"].(string)) {
		t.Fatalf("missing workspace refusal = %v\n%s", err, out)
	}
	if out, err := e.legworkErr("verify", id, "--json", "--", "true"); err == nil {
		t.Fatalf("missing workspace JSON refusal succeeded:\n%s", out)
	} else if got := decodeVerify(t, out); got["blocked"].(map[string]any)["kind"] != "missing-workspace" {
		t.Fatalf("missing workspace JSON refusal = %v", got)
	}
}

func TestVerifyOutputIsBounded(t *testing.T) {
	e, id, _ := blockedVerifyWorkspace(t)
	out, err := e.legworkErr("verify", id, "--json", "--", "sh", "-lc", "yes x | head -c 100000")
	if err != nil {
		t.Fatalf("large successful verification failed: %v\n%s", err, out)
	}
	receipt := decodeVerify(t, out)["receipt"].(map[string]any)
	if receipt["output_truncated"] != true || len(receipt["output"].(string)) > 64*1024 {
		t.Fatalf("output was not capture-bounded: truncated=%v size=%d", receipt["output_truncated"], len(receipt["output"].(string)))
	}
	// A short wait makes accidental surviving descendants visible in tests that
	// run under the race detector too; it should be a no-op after a success.
	time.Sleep(10 * time.Millisecond)
}
