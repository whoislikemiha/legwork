package rules

import (
	"strings"
	"testing"
)

func TestComposeIncludesSandboxAntiWorkaroundRule(t *testing.T) {
	prompt := Compose("")

	for _, want := range []string{
		"Do not modify the test harness, build config, or dependencies",
		"to work around a\n  sandbox limitation",
		"report blocked with the exact failing command",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestComposeKeepsOrchestratorAdditionsAfterWorkerRules(t *testing.T) {
	addition := "Run the narrow package tests."
	prompt := Compose(addition)

	ruleIndex := strings.Index(prompt, "Do not modify the test harness")
	additionIndex := strings.Index(prompt, addition)
	if ruleIndex < 0 {
		t.Fatalf("prompt missing sandbox anti-workaround rule:\n%s", prompt)
	}
	if additionIndex < 0 {
		t.Fatalf("prompt missing orchestrator addition:\n%s", prompt)
	}
	if additionIndex < ruleIndex {
		t.Fatalf("orchestrator addition must follow worker rules:\n%s", prompt)
	}
}
