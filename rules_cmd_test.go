package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whoislikemiha/legwork/internal/rules"
)

func TestRulesCommandPrintsInjectedRulesVerbatim(t *testing.T) {
	root := rootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"rules", "--agent", "fake", "--read-only", "--workspace", "ws-1"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != rules.Text() {
		t.Fatalf("rules output drifted from injected text:\n%q", got)
	}
}

func TestRulesCommandJSON(t *testing.T) {
	root := rootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"rules", "--json"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int    `json:"version"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("rules --json did not emit JSON: %v\n%s", err, out.String())
	}
	if got.Version != rules.Version || got.Text != rules.Text() {
		t.Fatalf("rules --json = %+v, want version=%d exact text", got, rules.Version)
	}
}

func TestRulesCommandRejectsUnknownAgent(t *testing.T) {
	root := rootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"rules", "--agent", "nonesuch"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("rules --agent nonesuch error = %v", err)
	}
}
