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

	"github.com/whoislikemiha/legwork/internal/job"
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
	Disposition  string      `json:"disposition,omitempty"`
	Checkpoints  int         `json:"checkpoints"`
	Created      time.Time   `json:"created"`
	Updated      time.Time   `json:"updated"`
	ClosedAt     *time.Time  `json:"closed_at,omitempty"`
	Reason       string      `json:"reason,omitempty"`
	SupersededBy string      `json:"superseded_by,omitempty"`
	FinalCommit  *CommitInfo `json:"final_commit,omitempty"`
	MergedInto   string      `json:"merged_into,omitempty"`
	Retention    string      `json:"retention,omitempty"`
	// Setup notes: "ok", "skipped: <why>", captured for observability.
	Setup string `json:"setup,omitempty"`
}

// Store manages <root>/workspaces/ws-N/{meta.json,tree/}.
type Store struct{ Root string }

// CommitResult is the durable git result of an orchestrator-owned workspace
// commit.
type CommitResult struct {
	OID     string
	Summary string
}

type CommitInfo struct {
	OID     string `json:"oid"`
	Summary string `json:"summary,omitempty"`
}

type CloseOptions struct {
	Disposition  string
	KeepWorktree bool
	Reason       string
	SupersededBy string
	MergedInto   string
	Retention    string
}

type reclaimOptions struct {
	KeepWorktree bool
	KeepBranch   bool
	KeepRefs     bool
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "workspaces"), 0o700); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) dir(id string) string { return filepath.Join(s.Root, "workspaces", id) }

// newID allocates ws-N (via the persisted high-water counter, so gc deletions
// never cause reuse) and reserves its directory.
func (s *Store) newID() (string, error) {
	n, err := job.AllocID(s.Root, "ws")
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("ws-%d", n)
	if err := os.Mkdir(filepath.Join(s.Root, "workspaces", id), 0o700); err != nil {
		return "", err
	}
	return id, nil
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

// Uncommitted reports whether the worktree has changes not yet committed
// (tracked or untracked) — i.e. it differs from its own HEAD. This is distinct
// from Dirty, which compares to the workspace base: committed work that has
// landed is not "uncommitted" and is safe for gc --close-merged to reclaim,
// whereas uncommitted work is always a human judgment call.
func (s *Store) Uncommitted(m *Meta) (bool, error) {
	out, err := git(m.Tree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Commit stages the whole workspace tree and creates one normal git commit on
// the workspace branch. It intentionally does not support allow-empty: legwork's
// review gate is a concrete diff, so an empty orchestrator commit is more likely
// to hide a mistaken close/merge sequence than to communicate useful state.
func (s *Store) Commit(m *Meta, message string) (*CommitResult, error) {
	if m.State == "closed" {
		return nil, fmt.Errorf("%s is closed", m.ID)
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, fmt.Errorf("commit message is required")
	}

	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", m.Tree}, args...)...)
		cmd.Env = commitEnv(m.Tree)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := run("add", "-A", "."); err != nil {
		return nil, err
	}

	cmd := exec.Command("git", "-C", m.Tree, "diff", "--cached", "--quiet", "--exit-code")
	cmd.Env = commitEnv(m.Tree)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil, fmt.Errorf("%s has no uncommitted changes to commit", m.ID)
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		return nil, fmt.Errorf("git diff --cached --quiet --exit-code: %v: %s", err, strings.TrimSpace(string(out)))
	}

	if _, err := run("commit", "-m", message); err != nil {
		return nil, err
	}
	oid, err := run("rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	summary, _ := run("show", "--stat", "--oneline", "--summary", "--no-renames", "--format=%h %s", "--no-ext-diff", "--no-color", "HEAD")
	m.FinalCommit = &CommitInfo{OID: oid, Summary: summary}
	if err := s.save(m); err != nil {
		return nil, err
	}
	return &CommitResult{OID: oid, Summary: summary}, nil
}

func commitEnv(tree string) []string {
	env := os.Environ()
	if _, err := git(tree, "config", "--get", "user.name"); err != nil {
		env = append(env, "GIT_AUTHOR_NAME=legwork orchestrator", "GIT_COMMITTER_NAME=legwork orchestrator")
	}
	if _, err := git(tree, "config", "--get", "user.email"); err != nil {
		env = append(env, "GIT_AUTHOR_EMAIL=legwork@localhost", "GIT_COMMITTER_EMAIL=legwork@localhost")
	}
	return env
}

// Close acknowledges the workspace and reclaims disposable local cache.
// Unreviewed changes require an explicit disposition.
func (s *Store) Close(m *Meta, opts CloseOptions) error {
	if m.State == "closed" {
		return fmt.Errorf("%s is already closed", m.ID)
	}
	disposition := opts.Disposition
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

	retention := strings.TrimSpace(opts.Retention)
	if err := s.reclaim(m, reclaimOptions{
		KeepWorktree: opts.KeepWorktree,
		KeepBranch:   keepBranch(disposition, opts.KeepWorktree, retention),
		KeepRefs:     opts.KeepWorktree || retention == "preserve",
	}); err != nil {
		return err
	}
	m.State = "closed"
	m.Disposition = disposition
	now := time.Now().UTC()
	m.ClosedAt = &now
	m.Reason = strings.TrimSpace(opts.Reason)
	m.SupersededBy = strings.TrimSpace(opts.SupersededBy)
	m.MergedInto = strings.TrimSpace(opts.MergedInto)
	m.Retention = retention
	return s.save(m)
}

// reclaim performs blast-radius-limited reclamation of a workspace's
// tool-created local cache. Branches are durable by default; only explicit
// discard paths delete the legwork/* branch.
func (s *Store) reclaim(m *Meta, opts reclaimOptions) error {
	if !opts.KeepRefs {
		// Delete checkpoint refs first: they pin objects.
		if refs, err := git(m.Repo, "for-each-ref", "--format=%(refname)", "refs/legwork/"+m.ID); err == nil {
			for _, ref := range strings.Fields(refs) {
				_, _ = git(m.Repo, "update-ref", "-d", ref)
			}
		}
	}
	if !opts.KeepWorktree {
		if _, err := git(m.Repo, "worktree", "remove", "--force", m.Tree); err != nil {
			return err
		}
	}
	if !opts.KeepBranch {
		if _, err := git(m.Repo, "branch", "-D", m.Branch); err != nil {
			return err
		}
	}
	return nil
}

func keepBranch(disposition string, keepWorktree bool, retention string) bool {
	if keepWorktree || retention == "preserve" {
		return true
	}
	return disposition != "discard"
}

// CloseMerged closes an open workspace whose work is machine-verified as
// landed (disposition "merged") or never-started (disposition "clean"),
// reclaiming its local worktree cache and checkpoint refs. It is gc's entry to
// the same reclamation policy Close uses; callers verify merged-ness first.
func (s *Store) CloseMerged(m *Meta, disposition, mergedInto string) error {
	if m.State == "closed" {
		return fmt.Errorf("%s is already closed", m.ID)
	}
	if err := s.reclaim(m, reclaimOptions{
		KeepBranch: true,
	}); err != nil {
		return err
	}
	m.State = "closed"
	m.Disposition = disposition
	now := time.Now().UTC()
	m.ClosedAt = &now
	m.MergedInto = strings.TrimSpace(mergedInto)
	return s.save(m)
}

// Dir returns the workspace's state directory (<root>/workspaces/<id>).
func (s *Store) Dir(id string) string { return s.dir(id) }

// DefaultBranchTip resolves the repo's default branch and returns its commit
// OID: origin/HEAD -> main -> master. Empty string (no error) if none resolve.
func (s *Store) DefaultBranchTip(repo string) (ref, oid string) {
	for _, cand := range []string{"refs/remotes/origin/HEAD", "refs/heads/main", "refs/heads/master"} {
		if out, err := git(repo, "rev-parse", "--verify", "--quiet", cand); err == nil && out != "" {
			return cand, out
		}
	}
	return "", ""
}

// MergedInto reports whether the workspace branch tip is an ancestor of target
// (i.e. its commits have landed there). `git merge-base --is-ancestor` exits 0
// for yes and exactly 1 for no; any other exit (e.g. 128 for a bad target ref)
// is a real error and is returned, so a typo'd --close-merged-into surfaces
// instead of quietly reporting everything unmerged.
func (s *Store) MergedInto(m *Meta, target string) (bool, error) {
	cmd := exec.Command("git", "-C", m.Repo, "merge-base", "--is-ancestor", m.Branch, target)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("merge-base --is-ancestor %s %s: %v: %s",
		m.Branch, target, err, strings.TrimSpace(string(out)))
}

// BranchTip returns the OID of the workspace's branch, or "" if it's gone.
func (s *Store) BranchTip(m *Meta) string {
	out, _ := git(m.Repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+m.Branch)
	return out
}
