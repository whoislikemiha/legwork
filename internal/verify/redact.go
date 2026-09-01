package verify

import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var secretEnvName = regexp.MustCompile(`(?i)(secret|token|pass(word|wd)?|api[_-]?key|authorization|credential|private[_-]?key)`)

// secretValues collects environment values whose names look secret-bearing,
// longest first so one secret cannot leave a suffix of a second, shorter
// configured value visible after replacement.
func secretValues() []string {
	var values []string
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if ok && secretEnvName.MatchString(name) && len(value) >= 4 {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

// redactingOutput buffers command output, replaces secret values, and caps the
// result. It keeps a raw look-ahead past the cap so a secret split across the
// boundary is still redacted before capping.
type redactingOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	rawLimit  int
	truncated bool
	secrets   []string
}

func newRedactingOutput(limit int, secrets []string) *redactingOutput {
	lookahead := 0
	for _, secret := range secrets {
		if len(secret) > lookahead {
			lookahead = len(secret)
		}
	}
	return &redactingOutput{limit: limit, rawLimit: limit + lookahead, secrets: secrets}
}

func (b *redactingOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	left := b.rawLimit - b.buf.Len()
	if left <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > left {
		_, _ = b.buf.Write(p[:left])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *redactingOutput) Result() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf.String()
	out = strings.ToValidUTF8(out, "�")
	for _, secret := range b.secrets {
		out = strings.ReplaceAll(out, secret, "[REDACTED]")
	}
	capped, cut := utf8Cap(out, b.limit)
	return capped, b.truncated || cut
}

func utf8Cap(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}
