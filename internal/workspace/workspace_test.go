package workspace

import (
	"crypto/sha256"
	"encoding/hex"
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
