package adapter

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/whoislikemiha/legwork/internal/events"
)

func TestHermesCommand(t *testing.T) {
	t.Setenv("LEGWORK_HERMES_BIN", "/opt/hermes-custom")
	tmp := t.TempDir()
	h := &Hermes{}
	cmd, err := h.Command(TurnRequest{
		Task: "continue", SystemPrompt: "rules", SessionID: "old-session",
		Model: "anthropic/claude-sonnet-4.6", WorkDir: "/work", TempDir: tmp,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/opt/hermes-custom", "-z", "rules\n\n# Task\n\ncontinue",
		"--usage-file", filepath.Join(tmp, hermesUsageFile), "--accept-hooks",
		"-m", "anthropic/claude-sonnet-4.6", "--resume", "old-session",
	}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args:\n got %q\nwant %q", cmd.Args, want)
	}
	if cmd.Dir != "/work" {
		t.Fatalf("dir = %q", cmd.Dir)
	}
	if h.Parser().(*hermesParser).usagePath != filepath.Join(tmp, hermesUsageFile) {
		t.Fatal("parser did not receive command sidecar path")
	}
}

func TestHermesCommandRejectsReadOnly(t *testing.T) {
	_, err := (&Hermes{}).Command(TurnRequest{ReadOnly: true, TempDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "no harness-enforced read-only") {
		t.Fatalf("error = %v", err)
	}
}

func TestHermesCommandClearsPriorSidecar(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, hermesUsageFile)
	if err := os.WriteFile(path, []byte(`{"completed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Hermes{}).Command(TurnRequest{Task: "x", TempDir: tmp}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("prior sidecar still exists: %v", err)
	}
}

func TestHermesParserFinalize(t *testing.T) {
	cases := []struct {
		name       string
		stdout     string
		sidecar    string
		wantState  string
		wantResult string
		wantSID    string
		wantCost   float64
		wantNil    bool
	}{
		{
			name:   "success and subscription cost",
			stdout: "implemented\n\nstate: done",
			sidecar: `{"estimated_cost_usd":1.25,"cost_status":"included","input_tokens":18000,` +
				`"output_tokens":120,"cache_read_tokens":300,"session_id":"new-1","completed":true,"failed":false}`,
			wantState: "done", wantResult: "implemented", wantSID: "new-1",
		},
		{
			name:   "metered estimated cost",
			stdout: "done\n\nstate: done",
			sidecar: `{"estimated_cost_usd":0.42,"cost_status":"estimated","session_id":"new-2",` +
				`"completed":true,"failed":false}`,
			wantState: "done", wantResult: "done", wantSID: "new-2", wantCost: 0.42,
		},
		{
			name:      "provider failure never parses status",
			stdout:    "HTTP 400: out of extra usage\n\nstate: done",
			sidecar:   `{"completed":false,"failed":true,"session_id":null}`,
			wantState: "failed", wantResult: "HTTP 400: out of extra usage\n\nstate: done",
		},
		{
			name:      "auth required from sidecar failure",
			stdout:    "",
			sidecar:   `{"completed":false,"failed":true,"session_id":null,"failure":"401 unauthorized; run codex login"}`,
			wantState: "auth-required", wantResult: "401 unauthorized; run codex login",
		},
		{
			name:      "empty successful stdout fails closed",
			stdout:    "",
			sidecar:   `{"completed":true,"failed":false,"session_id":"new-3"}`,
			wantState: "blocked", wantSID: "new-3",
		},
		{
			name:    "incomplete sidecar",
			stdout:  "partial",
			sidecar: `{"completed":false,"failed":false}`,
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			path := filepath.Join(tmp, hermesUsageFile)
			if err := os.WriteFile(path, []byte(tc.sidecar), 0o600); err != nil {
				t.Fatal(err)
			}
			p := &hermesParser{usagePath: path}
			if tc.stdout != "" {
				for _, line := range strings.Split(tc.stdout, "\n") {
					if _, _, err := p.Line([]byte(line)); err != nil {
						t.Fatal(err)
					}
				}
			}
			evs, got, err := p.Finalize()
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("result = %+v, want nil", got)
				}
				return
			}
			if got == nil || got.State != tc.wantState || got.Result != tc.wantResult ||
				got.SessionID != tc.wantSID || got.CostUSD != tc.wantCost {
				t.Fatalf("result = %+v", got)
			}
			if tc.name == "success and subscription cost" {
				if got.TokensIn != 18000 || got.TokensOut != 120 || got.Context != 18300 {
					t.Fatalf("telemetry = %+v", got)
				}
			}
			if tc.stdout == "" {
				if len(evs) != 0 {
					t.Fatalf("empty stdout events = %+v", evs)
				}
			} else if len(evs) != 1 || evs[0].Type != events.TypeText {
				t.Fatalf("events = %+v", evs)
			}
		})
	}
}

func TestHermesParserMissingAndMalformedSidecar(t *testing.T) {
	p := &hermesParser{usagePath: filepath.Join(t.TempDir(), "missing.json")}
	if _, got, err := p.Finalize(); err != nil || got != nil {
		t.Fatalf("missing sidecar result=%+v err=%v", got, err)
	}

	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	p = &hermesParser{usagePath: path}
	if _, _, err := p.Finalize(); err == nil || !strings.Contains(err.Error(), "parse hermes usage sidecar") {
		t.Fatalf("malformed sidecar error = %v", err)
	}
}

func TestHermesCaps(t *testing.T) {
	got := (&Hermes{}).Caps()
	if got.Fork || got.OSSandbox || got.Subagents || got.ReadOnly ||
		got.StructuredStatus != "convention" {
		t.Fatalf("caps = %+v", got)
	}
}
