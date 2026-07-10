package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoislikemiha/legwork/internal/job"
)

func TestLoadOldMetaCompatibility(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspaces", "ws-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{
  "id": "ws-1",
  "repo": "/repo",
  "tree": "/state/workspaces/ws-1/tree",
  "branch": "legwork/ws-1",
  "base_oid": "abc123",
  "state": "closed",
  "disposition": "discard",
  "checkpoints": 2,
  "created": "2026-07-01T00:00:00Z",
  "updated": "2026-07-01T00:01:00Z",
  "setup": "skipped: no worktree.toml (needs-bootstrap)"
}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Load("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "ws-1" || m.State != "closed" || m.Disposition != "discard" {
		t.Fatalf("old meta loaded incorrectly: %+v", m)
	}
	if m.ClosedAt != nil || m.FinalCommit != nil || m.MergedInto != "" || m.Retention != "" {
		t.Fatalf("old optional fields should stay zero: %+v", m)
	}
	if m.CloseReceipt == nil || m.CloseReceipt.ReceiptID != "legacy:ws-1:close" || m.CloseReceipt.Actor != "" || m.CloseReceipt.Disposition != "discard" {
		t.Fatalf("legacy compatibility receipt = %+v", m.CloseReceipt)
	}
	if listed, err := s.List(); err != nil || len(listed) != 1 || listed[0].CloseReceipt == nil {
		t.Fatalf("legacy metadata not readable through List: metas=%+v err=%v", listed, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != old {
		t.Fatalf("read-only legacy load rewrote metadata:\n%s", got)
	}
}

func TestLoadRejectsNewerSchemaIncludingList(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspaces", "ws-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"schema_version":3,"id":"ws-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, load := range []func() error{
		func() error { _, err := s.Load("ws-1"); return err },
		func() error { _, err := s.List(); return err },
	} {
		err := load()
		var unsupported *UnsupportedSchemaError
		if !errors.As(err, &unsupported) || unsupported.Found != 3 || unsupported.Supported != MetaSchemaVersion {
			t.Fatalf("newer schema error = %v, want UnsupportedSchemaError", err)
		}
	}
}

func TestLoadV1VerificationCompatibility(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspaces", "ws-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"schema_version":1,"id":"ws-1","state":"open"}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Load("ws-1")
	if err != nil || m.SchemaVersion != 1 || m.LatestVerification != nil {
		t.Fatalf("v1 workspace must remain readable without a receipt: meta=%+v err=%v", m, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil || string(got) != old {
		t.Fatalf("read-only v1 load rewrote metadata: err=%v got=%s", err, got)
	}
	if err := s.save(m); err != nil {
		t.Fatal(err)
	}
	upgraded, err := s.Load("ws-1")
	if err != nil || upgraded.SchemaVersion != MetaSchemaVersion {
		t.Fatalf("successful v1 write did not upgrade to v2: meta=%+v err=%v", upgraded, err)
	}
}

func TestRecordVerificationHistoryFailureIsSoft(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	m := &Meta{ID: "ws-1", State: "open"}
	if err := os.MkdirAll(s.dir(m.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.save(m); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.dir(m.ID), "events.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := &job.VerificationReceipt{ReceiptID: "r1", Job: "job-1", Workspace: m.ID, Turn: 1, Passed: true, Actor: "orchestrator"}
	if err := s.RecordVerification(m.ID, r); err != nil {
		t.Fatalf("durable verification should survive history failure: %v", err)
	}
	if r.HistoryError == "" {
		t.Fatal("history error was not retained on receipt")
	}
	stored, err := s.Load(m.ID)
	if err != nil || stored.LatestVerification == nil || stored.LatestVerification.ReceiptID != r.ReceiptID {
		t.Fatalf("receipt was not retained after history failure: meta=%+v err=%v", stored, err)
	}
}

func TestLegacyReceiptOmitsUnknownTimestamps(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspaces", "ws-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"id":"ws-1","state":"closed","created":"2026-07-01T00:00:00Z","updated":"2026-07-01T00:01:00Z","final_commit":{"oid":"abc"}}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Load("ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if m.FinalCommit.CommittedAt != nil || m.CloseReceipt.ClosedAt != nil {
		t.Fatalf("legacy missing timestamps became known: %+v", m)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "0001-01-01") || strings.Contains(string(encoded), "committed_at") || strings.Contains(string(encoded), "closed_at") {
		t.Fatalf("legacy output emitted unknown timestamp: %s", encoded)
	}
}

func TestReceiptEventsKeepDetailedMetadataOutOfCompactIndex(t *testing.T) {
	long := strings.Repeat("x", 500)
	commit := &CommitInfo{ReceiptID: "workspace:ws-1:commit:abc", OID: "abc", Summary: long, Message: long}
	close := &CloseReceipt{
		ReceiptID: "workspace:ws-1:close:now", Disposition: "merged", Reason: long,
		FinalCommit: commit,
	}

	for _, receipt := range []any{compactCommitInfo(commit), compactCloseReceipt(close)} {
		encoded, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), long) || !strings.Contains(string(encoded), "…") {
			t.Fatalf("receipt event was not compact: %s", encoded)
		}
	}
	if commit.Summary != long || close.Reason != long {
		t.Fatal("compacting an event mutated authoritative receipt metadata")
	}
}

func TestCloseRequiresExplicitActorBeforeReclaim(t *testing.T) {
	s := &Store{}
	m := &Meta{ID: "ws-1", State: "open"}
	if err := s.Close(m, CloseOptions{}); err == nil || !strings.Contains(err.Error(), "actor is required") {
		t.Fatalf("Close without actor = %v, want explicit actor refusal", err)
	}
	if err := s.CloseMerged(m, "clean", "", ""); err == nil || !strings.Contains(err.Error(), "actor is required") {
		t.Fatalf("CloseMerged without actor = %v, want explicit actor refusal", err)
	}
}

func TestCommitHistoryFailureReturnsDurableReceipt(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-qm", "base")

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Create(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Tree, "receipt.txt"), []byte("durable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory at the event-log path injects an append failure after git and
	// metadata have succeeded, without weakening filesystem permissions.
	if err := os.Mkdir(s.eventPath(m.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err := s.Commit(m, "commit despite history failure")
	if err != nil {
		t.Fatalf("durable commit returned a replayable error: %v", err)
	}
	if res.Receipt == nil || res.Receipt.HistoryError == "" {
		t.Fatalf("commit receipt omitted history warning: %+v", res)
	}
	reloaded, err := s.Load(m.ID)
	if err != nil || reloaded.FinalCommit == nil || reloaded.FinalCommit.HistoryError == "" {
		t.Fatalf("commit warning was not persisted when possible: meta=%+v err=%v", reloaded, err)
	}
}

func TestParseReviewReceiptIsStrictAndFailClosed(t *testing.T) {
	base := &job.Meta{
		ID: "job-9", Agent: "fake", Model: "reviewer", State: job.StateDone,
		Updated: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		Review:  &job.ReviewRequest{CheckpointRef: "refs/legwork/ws-1/ckpt-2", CheckpointOID: "abc", DiffSHA256: "digest"},
	}
	valid := *base
	valid.Result = `{"verdict":"FIX","findings":[{"file":"main.go","line":4,"severity":" Critical ","detail":"missing bounds check"}]}`
	receipt := ParseReviewReceipt(&valid)
	if !receipt.Parsed || receipt.Verdict != "FIX" || receipt.Findings.Total != 1 || receipt.Findings.Critical != 1 || receipt.CheckpointOID != "abc" || !receipt.CompletedAt.Equal(base.Updated) {
		t.Fatalf("valid receipt parsed incorrectly: %+v", receipt)
	}

	// Real reviewers commonly lead with prose and put the structured verdict in
	// a fenced json block. SHIP permits non-blocking findings by prompt contract.
	fenced := *base
	fenced.Result = "Review complete.\n```json\n{\"verdict\":\"SHIP\",\"findings\":[{\"file\":\"README.md\",\"line\":2,\"severity\":\" Low \",\"detail\":\"wording only\"}]}\n```\nNo landing blockers."
	receipt = ParseReviewReceipt(&fenced)
	if !receipt.Parsed || receipt.Verdict != "SHIP" || receipt.Findings.Total != 1 || receipt.Findings.Low != 1 {
		t.Fatalf("fenced SHIP receipt parsed incorrectly: %+v", receipt)
	}

	malformed := *base
	malformed.Result = `review says {"verdict":"SHIP","findings":[]}`
	receipt = ParseReviewReceipt(&malformed)
	if receipt.Parsed || receipt.Verdict != "" || receipt.ParseError == "" {
		t.Fatalf("malformed verdict must be explicit fail-closed: %+v", receipt)
	}

	for _, result := range []string{
		"```json\n{\"verdict\":\"SHIP\",\"findings\":[]}\n```\n```json\n{\"verdict\":\"FIX\",\"findings\":[]}\n```",
		"```json\n{\"verdict\":\"SHIP\",\"findings\":[]\n```",
	} {
		candidate := *base
		candidate.Result = result
		receipt = ParseReviewReceipt(&candidate)
		if receipt.Parsed || receipt.Verdict != "" || receipt.ParseError == "" {
			t.Fatalf("ambiguous or malformed receipt must fail closed: %+v", receipt)
		}
	}

	blocked := *base
	blocked.State = job.StateBlocked
	blocked.Result = `{"verdict":"SHIP","findings":[]}`
	receipt = ParseReviewReceipt(&blocked)
	if receipt.Parsed || receipt.Verdict != "" || receipt.ParseError == "" {
		t.Fatalf("non-done review must be fail-closed: %+v", receipt)
	}
}

func TestReviewSnapshotPreservesExactDiffBytes(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("first\ncontext\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "-qm", "base")

	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Create(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Tree, "README.md"), []byte("changed\ncontext\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.ReviewSnapshot(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(snapshot.Diff, " \n") {
		t.Fatalf("snapshot lost blank trailing context: %q", snapshot.Diff)
	}
	sum := sha256.Sum256([]byte(snapshot.Diff))
	if got, want := snapshot.DiffSHA256, hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("digest = %s, want %s for exact diff bytes", got, want)
	}
	cmd := exec.Command("git", "-C", m.Tree, "diff", m.BaseOID, snapshot.CheckpointOID)
	direct, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Diff != string(direct) {
		t.Fatalf("snapshot bytes differ from git output:\nsnapshot=%q\ndirect=%q", snapshot.Diff, direct)
	}
	apply := exec.Command("git", "-C", repo, "apply", "--check", "-")
	apply.Stdin = strings.NewReader(snapshot.Diff)
	if out, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("snapshot diff is not applyable: %v\n%s", err, out)
	}
}
