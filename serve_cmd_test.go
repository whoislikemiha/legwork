package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeCommandRegisteredHelp(t *testing.T) {
	root := rootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"serve", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	help := out.String()
	for _, want := range []string{"serve", "read-only browser dashboard", "--addr", "--allow-remote", "localhost"} {
		if !strings.Contains(help, want) {
			t.Fatalf("serve help missing %q:\n%s", want, help)
		}
	}
}
