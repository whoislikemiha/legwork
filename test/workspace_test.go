package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initRepo creates a git repo with one commit.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func (e *env) wsNew(t *testing.T, repo string) map[string]any {
	t.Helper()
	out := e.legwork(t, "ws", "new", "--repo", repo, "--json")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("bad ws json: %v\n%s", err, out)
	}
	return m
}

func TestWorkspaceLifecycle(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	// Worktree lives outside the repo, on a namespaced branch.
	tree := ws["tree"].(string)
	if strings.HasPrefix(tree, repo) {
		t.Fatalf("worktree must live outside the repo: %s", tree)
	}
	if ws["branch"] != "legwork/"+wsID {
		t.Fatalf("branch = %v", ws["branch"])
	}

	// Fake worker edits a file and adds a new one, then finishes.
	e.writeScript(t,
		"#write README.md changed by worker",
		"#write newfile.txt hello",
		resultDone,
	)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "edit stuff"))
	e.waitState(t, id, "done")

	// Turn end produced a checkpoint event.
	evs := e.legwork(t, "events", id)
	if !strings.Contains(evs, "checkpoint") || !strings.Contains(evs, "refs/legwork/"+wsID+"/ckpt-1") {
		t.Fatalf("no checkpoint after workspace turn:\n%s", evs)
	}

	// Diff vs base includes the edit AND the untracked file.
	diff := e.legwork(t, "diff", wsID)
	if !strings.Contains(diff, "changed by worker") || !strings.Contains(diff, "newfile.txt") {
		t.Fatalf("diff incomplete:\n%s", diff)
	}

	// Close without disposition must refuse: there are unreviewed changes.
	if out, err := e.legworkErr("close", wsID); err == nil {
		t.Fatalf("close must refuse dirty workspace without disposition:\n%s", out)
	}

	// Explicit discard reclaims worktree, branch, and refs.
	e.legwork(t, "close", wsID, "--discard")
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatal("worktree not removed on close")
	}
	branches, _ := exec.Command("git", "-C", repo, "branch", "--list", "legwork/*").Output()
	if strings.TrimSpace(string(branches)) != "" {
		t.Fatalf("branch not deleted: %s", branches)
	}
	refs, _ := exec.Command("git", "-C", repo, "for-each-ref", "refs/legwork/").Output()
	if strings.TrimSpace(string(refs)) != "" {
		t.Fatalf("checkpoint refs not deleted: %s", refs)
	}

	// The workspace's job is closed with it.
	out := e.legwork(t, "status", id, "--json")
	if !strings.Contains(out, `"state": "closed"`) {
		t.Fatalf("job not closed with workspace:\n%s", out)
	}
}

func TestWorkspaceCommit(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)

	e.writeScript(t,
		"#write README.md changed by worker",
		"#write newfile.txt hello",
		resultDone,
	)
	jid := strings.TrimSpace(e.legwork(t, "run", "--run", "pipe", "--agent", "fake", "--workspace", wsID, "edit stuff"))
	e.waitState(t, jid, "done")

	out := e.legwork(t, "ws", "commit", wsID, "-m", "land workspace output")
	if !strings.Contains(out, wsID+" committed ") {
		t.Fatalf("commit output missing oid:\n%s", out)
	}
	if uncommitted, _ := gitInErr(tree, "status", "--porcelain"); uncommitted != "" {
		t.Fatalf("workspace left uncommitted changes:\n%s", uncommitted)
	}
	if diff := e.legwork(t, "diff", wsID); !strings.Contains(diff, "changed by worker") || !strings.Contains(diff, "newfile.txt") {
		t.Fatalf("committed workspace diff should still be reviewable vs base:\n%s", diff)
	}
	if msg, _ := gitInErr(tree, "log", "-1", "--format=%s"); msg != "land workspace output" {
		t.Fatalf("commit message = %q", msg)
	}

	for _, got := range []string{e.legwork(t, "events", jid, "--json"), e.legwork(t, "events", "pipe", "--run", "--json")} {
		if !strings.Contains(got, "commit") || !strings.Contains(got, "land workspace output") || !strings.Contains(got, wsID) {
			t.Fatalf("commit event missing attribution:\n%s", got)
		}
	}

	gitIn(t, repo, "merge", "--ff-only", branch)
	e.legwork(t, "close", wsID, "--merged")
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed merged after ws commit: %v", m)
	}
}

func TestWorkspaceCommitRefusesEmpty(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	if out, err := e.legworkErr("ws", "commit", wsID, "-m", "empty"); err == nil {
		t.Fatalf("empty workspace commit must be refused:\n%s", out)
	} else if !strings.Contains(out, "no uncommitted changes") {
		t.Fatalf("empty commit refusal should explain why:\n%s", out)
	}
}

func TestWorkspaceCommitJSON(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)

	if err := os.WriteFile(filepath.Join(tree, "json.txt"), []byte("json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := e.legwork(t, "ws", "commit", wsID, "-m", "json commit", "--json")
	var got struct {
		Workspace string `json:"workspace"`
		Branch    string `json:"branch"`
		OID       string `json:"oid"`
		Summary   string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad commit json: %v\n%s", err, out)
	}
	if got.Workspace != wsID || got.Branch != branch || got.OID == "" {
		t.Fatalf("unexpected commit json: %+v", got)
	}
	if head, _ := gitInErr(tree, "rev-parse", "HEAD"); head != got.OID {
		t.Fatalf("json oid %q != HEAD %q", got.OID, head)
	}
}

func TestWorkspaceCommitReportsEventAppendFailure(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	e.writeScript(t, "#write event-failure.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "edit stuff"))
	e.waitState(t, jid, "done")

	eventsPath := filepath.Join(e.state, "jobs", jid, "events.jsonl")
	if err := os.Remove(eventsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(e.state, "missing-dir", "events.jsonl"), eventsPath); err != nil {
		t.Fatal(err)
	}

	out, err := e.legworkErr("ws", "commit", wsID, "-m", "durable event")
	if err == nil {
		t.Fatalf("commit must report event append failure:\n%s", out)
	}
	oid, _ := gitInErr(tree, "rev-parse", "HEAD")
	for _, want := range []string{"event write failed", oid, eventsPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("commit error missing %q:\n%s", want, out)
		}
	}
	if uncommitted, _ := gitInErr(tree, "status", "--porcelain"); uncommitted != "" {
		t.Fatalf("workspace left uncommitted changes:\n%s", uncommitted)
	}
}

// close --merged is a claim, not a fact: it must be verified against the
// target branch before the branch (and with it the work) is destroyed.
func TestCloseMergedVerifies(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)

	e.writeScript(t, "#write landed.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "do"))
	e.waitState(t, jid, "done")
	gitIn(t, tree, "add", ".")
	gitIn(t, tree, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "work")

	// Not landed yet: --merged must refuse (this is the dangling-commit trap).
	if out, err := e.legworkErr("close", wsID, "--merged"); err == nil {
		t.Fatalf("close --merged of an unmerged branch must refuse:\n%s", out)
	}
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after refused close: %v", m)
	}

	// Land it in main; now --merged verifies and the close goes through.
	gitIn(t, repo, "merge", "--ff-only", branch)
	e.legwork(t, "close", wsID, "--merged")
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed merged: %v", m)
	}
}

// With a remote configured, the auto-detected target is origin/HEAD: work
// merged into local main but not pushed must refuse with a message naming the
// push, and closing succeeds once pushed.
func TestCloseMergedDetectsUnpushed(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare")
	gitIn(t, repo, "remote", "add", "origin", remote)
	gitIn(t, repo, "push", "-q", "origin", "main")
	gitIn(t, repo, "remote", "set-head", "origin", "main")

	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)
	e.writeScript(t, "#write landed.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "do"))
	e.waitState(t, jid, "done")
	gitIn(t, tree, "add", ".")
	gitIn(t, tree, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "work")
	gitIn(t, repo, "merge", "--ff-only", branch) // landed locally, not pushed

	out, err := e.legworkErr("close", wsID, "--merged")
	if err == nil {
		t.Fatalf("close --merged must refuse unpushed work:\n%s", out)
	}
	if !strings.Contains(out, "push first") {
		t.Fatalf("refusal should name the unpushed near-miss:\n%s", out)
	}

	gitIn(t, repo, "push", "-q", "origin", "main")
	e.legwork(t, "close", wsID, "--merged")
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed after push: %v", m)
	}
}

// --force skips the verification for targets legwork can't see (e.g. the work
// landed in another repo or was cherry-picked).
func TestCloseMergedForceSkipsVerification(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	e.writeScript(t, "#write landed.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "do"))
	e.waitState(t, jid, "done")
	gitIn(t, tree, "add", ".")
	gitIn(t, tree, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "work")

	e.legwork(t, "close", wsID, "--merged", "--force")
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed with --force: %v", m)
	}
}

func TestWorkspaceCleanCloseNeedsNoFlag(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	// No changes: close succeeds without a disposition.
	e.legwork(t, "close", ws["id"].(string))
}

func TestWorkspaceLock(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	e.writeScript(t, "#sleep 5000", resultDone)
	e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "slow job")
	time.Sleep(300 * time.Millisecond)

	// Second concurrent job in the same workspace must be refused.
	if out, err := e.legworkErr("run", "--agent", "fake", "--workspace", wsID, "second job"); err == nil {
		t.Fatalf("workspace lock not enforced:\n%s", out)
	}
	// Closing while a job runs must be refused too.
	if out, err := e.legworkErr("close", wsID, "--discard"); err == nil {
		t.Fatalf("close of busy workspace must be refused:\n%s", out)
	}
}

func TestWorkspaceWorkstreeBootstrap(t *testing.T) {
	if _, err := exec.LookPath("workstree"); err != nil {
		t.Skip("workstree not installed")
	}
	e := newEnv(t)
	repo := initRepo(t)
	// Commit a worktree.toml whose setup leaves a marker and whose ready
	// check requires it: proves bootstrap actually ran in the new tree.
	if err := os.WriteFile(filepath.Join(repo, "worktree.toml"),
		[]byte("setup = [\"touch bootstrapped\"]\nready = \"test -f bootstrapped\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "add", ".")
	cmd.Run()
	cmd = exec.Command("git", "-C", repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "add worktree.toml")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	ws := e.wsNew(t, repo)
	if ws["setup"] != "ok" {
		t.Fatalf("setup = %v", ws["setup"])
	}
	if _, err := os.Stat(filepath.Join(ws["tree"].(string), "bootstrapped")); err != nil {
		t.Fatal("bootstrap marker missing in worktree")
	}
	// The marker isn't gitignored (unlike real setup artifacts), so the
	// workspace counts as dirty — discard explicitly.
	e.legwork(t, "close", ws["id"].(string), "--discard")
}
