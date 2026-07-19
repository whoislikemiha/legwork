package notify

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateCaptureEnv(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		ok    bool
	}{
		{"empty", nil, true},
		{"posix names", []string{"ROUTE", "_route_2"}, true},
		{"duplicate", []string{"ROUTE", "ROUTE"}, false},
		{"shell syntax", []string{"ROUTE;echo"}, false},
		{"assignment", []string{"ROUTE=value"}, false},
		{"digit prefix", []string{"2ROUTE"}, false},
		{"too many", repeatedNames(MaxCaptureEnvNames + 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCaptureEnv(tc.names)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidateCaptureEnv(%q) error = %v, want ok=%v", tc.names, err, tc.ok)
			}
		})
	}
}

func TestCaptureOriginSetAbsentEmptyAndBounds(t *testing.T) {
	origin, err := CaptureOrigin([]string{"SET", "ABSENT", "EMPTY"}, []string{"SET=route-a", "EMPTY="})
	if err != nil {
		t.Fatal(err)
	}
	if got := origin.Values["SET"]; !got.Present || got.Value != "route-a" {
		t.Fatalf("SET = %+v", got)
	}
	if got := origin.Values["ABSENT"]; got.Present || got.Value != "" {
		t.Fatalf("ABSENT = %+v", got)
	}
	if got := origin.Values["EMPTY"]; !got.Present || got.Value != "" {
		t.Fatalf("EMPTY = %+v", got)
	}
	if _, err := CaptureOrigin([]string{"SET"}, []string{"SET=" + strings.Repeat("x", MaxCaptureEnvValue+1)}); err == nil {
		t.Fatal("oversize value accepted")
	}
}

func TestOriginStorageAtomicPrivateAndCorruption(t *testing.T) {
	dir := t.TempDir()
	first, _ := CaptureOrigin([]string{"ROUTE"}, []string{"ROUTE=one"})
	if err := SaveOrigin(dir, first); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(OriginPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("snapshot mode = %o, want 600", got)
	}
	second, _ := CaptureOrigin([]string{"ROUTE"}, []string{"ROUTE=two"})
	if err := SaveOrigin(dir, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrigin(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Values["ROUTE"].Value; got != "two" {
		t.Fatalf("replacement value = %q", got)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, originFilename+".tmp-*")); len(matches) != 0 {
		t.Fatalf("temporary snapshots remain: %v", matches)
	}
	if err := os.WriteFile(OriginPath(dir), []byte(`{"version":1,"values":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrigin(dir); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
}

func TestOverlayAndScrubEnvironment(t *testing.T) {
	origin, _ := CaptureOrigin([]string{"SET", "ABSENT", "META"}, []string{
		"SET=$HOME; 'literal'", "META=original",
	})
	base := []string{"PATH=/bin", "SET=session-b", "ABSENT=session-b", "META=session-b", "OTHER=kept"}
	got := OverlayEnvironment(base, origin, []string{"SET", "ABSENT"})
	want := map[string]string{"PATH": "/bin", "SET": "$HOME; 'literal'", "OTHER": "kept"}
	if values := environmentMap(got); !reflect.DeepEqual(values, want) {
		t.Fatalf("overlay = %#v, want %#v", values, want)
	}
	if values := environmentMap(ScrubEnvironment(base, []string{"SET", "ABSENT"})); reflect.DeepEqual(values, environmentMap(base)) || values["OTHER"] != "kept" {
		t.Fatalf("scrub = %#v", values)
	}
}

func TestJobNotifierEnvLegacyRevocationAndCorruption(t *testing.T) {
	cfg := &Config{}
	cfg.Notify.CaptureEnv = []string{"ROUTE"}
	base := []string{"ROUTE=session-b", "OTHER=kept"}

	legacy := t.TempDir()
	got, err := cfg.JobNotifierEnv(legacy, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := environmentMap(got)["ROUTE"]; ok {
		t.Fatal("legacy job inherited configured route")
	}

	dir := t.TempDir()
	origin, _ := CaptureOrigin([]string{"ROUTE", "REVOKED"}, []string{"ROUTE=origin", "REVOKED=old"})
	if err := SaveOrigin(dir, origin); err != nil {
		t.Fatal(err)
	}
	got, err = cfg.JobNotifierEnv(dir, append(base, "REVOKED=session-b"))
	if err != nil {
		t.Fatal(err)
	}
	values := environmentMap(got)
	if values["ROUTE"] != "origin" {
		t.Fatalf("route = %q", values["ROUTE"])
	}
	if _, ok := values["REVOKED"]; ok {
		t.Fatal("removed name was not revoked")
	}

	if err := os.WriteFile(OriginPath(dir), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = cfg.JobNotifierEnv(dir, base)
	if err == nil {
		t.Fatal("corrupt snapshot did not fail")
	}
	if _, ok := environmentMap(got)["ROUTE"]; ok {
		t.Fatal("corrupt snapshot fell back to caller route")
	}
}

func repeatedNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "NAME_" + strings.Repeat("X", i)
	}
	return out
}
