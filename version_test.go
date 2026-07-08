package main

import "testing"

func TestBuildVersionUsesStampedValues(t *testing.T) {
	oldVersion, oldCommit, oldDate, oldDirty := version, commit, date, dirty
	t.Cleanup(func() {
		version, commit, date, dirty = oldVersion, oldCommit, oldDate, oldDirty
	})

	version = "v1.2.3"
	commit = "1234567890abcdef"
	date = "2026-07-08T20:00:00Z"
	dirty = "true"

	got := buildVersion()
	if got.Version != "v1.2.3" {
		t.Fatalf("version = %q", got.Version)
	}
	if got.Commit != "1234567890ab" {
		t.Fatalf("commit = %q", got.Commit)
	}
	if !got.Dirty {
		t.Fatal("dirty = false, want true")
	}
	if got.Date != "2026-07-08T20:00:00Z" {
		t.Fatalf("date = %q", got.Date)
	}
}

func TestVersionSummaryIncludesDirtyCommit(t *testing.T) {
	oldVersion, oldCommit, oldDate, oldDirty := version, commit, date, dirty
	t.Cleanup(func() {
		version, commit, date, dirty = oldVersion, oldCommit, oldDate, oldDirty
	})

	version = "dev"
	commit = "abc123"
	date = ""
	dirty = "dirty"

	got := versionSummary()
	if got != "dev, commit abc123-dirty" {
		t.Fatalf("summary = %q", got)
	}
}
