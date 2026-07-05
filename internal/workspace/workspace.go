// Package workspace implements the accountability unit: one workspace = one
// worktree = one branch = one diff = one review gate = one close
// (DESIGN.md §2). Jobs are turns taken in a workspace, one active at a time.
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Meta is the persisted workspace record.
type Meta struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`   // source checkout root
	Tree    string `json:"tree"`   // worktree path (outside the repo)
	Branch  string `json:"branch"` // legwork/<id>, tool-created and tool-deleted only
	BaseOID string `json:"base_oid"`
	State   string `json:"state"` // open | closed
	// Disposition records why it closed: merged | discard | (empty while open).
	Disposition string    `json:"disposition,omitempty"`
	Checkpoints int       `json:"checkpoints"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
	// Setup notes: "ok", "skipped: <why>", captured for observability.
	Setup string `json:"setup,omitempty"`
}

// Store manages <root>/workspaces/ws-N/{meta.json,tree/}.
type Store struct{ Root string }

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "workspaces"), 0o700); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) dir(id string) string { return filepath.Join(s.Root, "workspaces", id) }

func (s *Store) newID() (string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "workspaces"))
	if err != nil {
		return "", err
	}
	max := 0
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "ws-%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("ws-%d", max+1), nil
}

func (s *Store) save(m *Meta) error {
	m.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir(m.ID), "meta.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) Load(id string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir(id), "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", id, err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) List() ([]*Meta, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "workspaces"))
	if err != nil {
		return nil, err
	}
	var out []*Meta
	for _, e := range entries {
		if m, err := s.Load(e.Name()); err == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Create makes a workspace: worktree on a namespaced branch, then workstree
// bootstrap when the repo declares the convention.
func (s *Store) Create(repo, base string) (*Meta, error) {
	repoRoot, err := git(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repo: %w", repo, err)
	}
	id, err := s.newID()
	if err != nil {
		return nil, err
	}
	m := &Meta{ID: id, Repo: repoRoot, State: "open", Created: time.Now().UTC()}
	m.Tree = filepath.Join(s.dir(id), "tree")
	m.Branch = "legwork/" + id
	if err := os.MkdirAll(s.dir(id), 0o700); err != nil {
		return nil, err
	}

	args := []string{"worktree", "add", "-b", m.Branch, m.Tree}
	if base != "" {
		args = append(args, base)
	}
	if _, err := git(repoRoot, args...); err != nil {
		return nil, err
	}
	if m.BaseOID, err = git(m.Tree, "rev-parse", "HEAD"); err != nil {
		return nil, err
	}

	if err := s.bootstrap(m); err != nil {
		// A half-built environment must not accept jobs: undo everything.
		_, _ = git(repoRoot, "worktree", "remove", "--force", m.Tree)
		_, _ = git(repoRoot, "branch", "-D", m.Branch)
		_ = os.RemoveAll(s.dir(id))
		return nil, err
	}
	return m, s.save(m)
}

// bootstrap runs workstree against the new tree when the convention applies.
func (s *Store) bootstrap(m *Meta) error {
	hasConfig := false
	for _, dir := range []string{m.Tree, m.Repo} {
		if _, err := os.Stat(filepath.Join(dir, "worktree.toml")); err == nil {
			hasConfig = true
			break
		}
	}
	if !hasConfig {
		m.Setup = "skipped: no worktree.toml (needs-bootstrap)"
		return nil
	}
	bin, err := exec.LookPath("workstree")
	if err != nil {
		m.Setup = "skipped: worktree.toml present but workstree not installed"
		return nil
	}
	logPath := filepath.Join(s.dir(m.ID), "setup.log")
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(bin, "init", m.Tree)
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("workstree init failed (see %s): %w", logPath, err)
	}
	m.Setup = "ok"
	return nil
}

// Checkpoint snapshots the worktree (tracked + untracked) as a tree object
// under refs/legwork/<id>/ckpt-N, without commits or touching the real index.
func (s *Store) Checkpoint(m *Meta) (ref, oid string, err error) {
	tmpIndex := filepath.Join(s.dir(m.ID), "ckpt-index")
	defer os.Remove(tmpIndex)

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", m.Tree}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := run("read-tree", "HEAD"); err != nil {
		return "", "", err
	}
	if _, err := run("add", "-A", "."); err != nil {
		return "", "", err
	}
	oid, err = run("write-tree")
	if err != nil {
		return "", "", err
	}
	m.Checkpoints++
	ref = fmt.Sprintf("refs/legwork/%s/ckpt-%d", m.ID, m.Checkpoints)
	if _, err := git(m.Tree, "update-ref", ref, oid); err != nil {
		return "", "", err
	}
	return ref, oid, s.save(m)
}

// Diff returns changes since the workspace base: a fresh (unpersisted)
// snapshot diffed against the base tree, so untracked files are included.
func (s *Store) Diff(m *Meta, stat bool) (string, error) {
	tmpIndex := filepath.Join(s.dir(m.ID), "diff-index")
	defer os.Remove(tmpIndex)

	env := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", m.Tree}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	if _, err := run("read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := run("add", "-A", "."); err != nil {
		return "", err
	}
	oid, err := run("write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"diff", m.BaseOID, strings.TrimSpace(oid)}
	if stat {
		args = append(args, "--stat")
	}
	return run(args...)
}

// Dirty reports whether the workspace differs from its base.
func (s *Store) Dirty(m *Meta) (bool, error) {
	out, err := s.Diff(m, true)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Close acknowledges the workspace and reclaims worktree, branch, and
// checkpoint refs. Unreviewed changes require an explicit disposition.
func (s *Store) Close(m *Meta, disposition string, keepWorktree bool) error {
	if m.State == "closed" {
		return fmt.Errorf("%s is already closed", m.ID)
	}
	if disposition == "" {
		dirty, err := s.Dirty(m)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("%s has changes vs its base; use --merged (landed elsewhere, verified) or --discard (throw away)", m.ID)
		}
		disposition = "clean"
	}

	if !keepWorktree {
		// Delete checkpoint refs first: they pin objects.
		if refs, err := git(m.Repo, "for-each-ref", "--format=%(refname)", "refs/legwork/"+m.ID); err == nil {
			for _, ref := range strings.Fields(refs) {
				_, _ = git(m.Repo, "update-ref", "-d", ref)
			}
		}
		if _, err := git(m.Repo, "worktree", "remove", "--force", m.Tree); err != nil {
			return err
		}
		if _, err := git(m.Repo, "branch", "-D", m.Branch); err != nil {
			return err
		}
	}
	m.State = "closed"
	m.Disposition = disposition
	return s.save(m)
}
