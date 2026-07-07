package workspace

import (
	"os"
	"path/filepath"
	"testing"
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
