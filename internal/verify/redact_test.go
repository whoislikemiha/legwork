package verify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRedactingOutputRedactsAcrossBoundaryBeforeCap(t *testing.T) {
	const secret = "long-secret-value"
	b := newRedactingOutput(32, []string{secret})
	// Split the secret across writes and place it past the final output cap.
	// The raw look-ahead must retain enough bytes to redact it first.
	_, _ = b.Write([]byte(strings.Repeat("x", 29) + secret[:5]))
	_, _ = b.Write([]byte(secret[5:] + " trailing"))
	out, cut := b.Result()
	if !cut || strings.Contains(out, secret) {
		t.Fatalf("output cut=%v still exposes secret: %q", cut, out)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("capped output is invalid UTF-8: %q", out)
	}
}

func TestRedactingOutputMarksSourceAndPostRedactionTruncation(t *testing.T) {
	b := newRedactingOutput(8, []string{"abcd"})
	_, _ = b.Write([]byte("1234567abcd")) // replacement expands beyond the cap
	out, cut := b.Result()
	if !cut || strings.Contains(out, "abcd") {
		t.Fatalf("post-redaction cap cut=%v output=%q", cut, out)
	}
	b = newRedactingOutput(8, nil)
	_, _ = b.Write([]byte("0123456789"))
	_, cut = b.Result()
	if !cut {
		t.Fatal("source truncation was not reported")
	}
}

func TestSecretValuesOrdersLongestFirst(t *testing.T) {
	t.Setenv("LEGWORK_TEST_TOKEN", "short-secret")
	t.Setenv("LEGWORK_TEST_API_KEY", "a-much-longer-secret-value")
	values := secretValues()
	seen := -1
	for i, v := range values {
		if v == "a-much-longer-secret-value" {
			seen = i
		}
		if v == "short-secret" && seen == -1 {
			t.Fatal("shorter secret ordered before longer one")
		}
		_ = i
	}
	if seen == -1 {
		t.Fatal("secret env value not collected")
	}
}
