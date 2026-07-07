package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cmdEnv builds a legwork command with the base test env plus extras (e.g. a
// LEGWORK_CONFIG pointing at a [gc] toml, or LEGWORK_NO_AUTO_GC="" to re-enable
// the opportunistic fork suppressed globally in TestMain).
func (e *env) cmdEnv(extraEnv []string, args ...string) *exec.Cmd {
	c := exec.Command(binPath, args...)
	c.Env = append(os.Environ(),
		"LEGWORK_STATE_DIR="+e.state, "LEGWORK_FAKE_SCRIPT="+e.script)
	c.Env = append(c.Env, extraEnv...)
	return c
}

// gcConfig writes a [gc] config.toml with the given body and returns the
// LEGWORK_CONFIG env entry pointing at it.
func gcConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[gc]\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return "LEGWORK_CONFIG=" + p
}

// gcJSON runs `legwork gc <args>` with the config env and decodes the report.
func (e *env) gcJSON(t *testing.T, cfgEnv string, args ...string) map[string]any {
	t.Helper()
	full := append([]string{"gc", "--json"}, args...)
	out, err := e.cmdEnv([]string{cfgEnv}, full...).CombinedOutput()
	if err != nil {
		// gc exits 1 on partial failure but still prints the report; only a
		// non-1 exit is a hard error here.
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("gc %v: %v\n%s", args, err, out)
		}
	}
	var rep map[string]any
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("bad gc json: %v\n%s", err, out)
	}
	return rep
}

func kinds(rep map[string]any) map[string]int {
	counts := map[string]int{}
	if acts, ok := rep["actions"].([]any); ok {
		for _, a := range acts {
			if m, ok := a.(map[string]any); ok {
				counts[m["kind"].(string)]++
			}
		}
	}
	return counts
}

// TestIDNotReusedAfterGC is the core invariant gc introduces: deleting the
// top-numbered dir must NOT let the next allocation reuse that ID (which would
// collide two different jobs across event logs and notifier history).
func TestIDNotReusedAfterGC(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)

	var last string
	for i := 0; i < 3; i++ {
		last = strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "job"))
		e.waitState(t, last, "done")
	}
	if last != "job-3" {
		t.Fatalf("expected job-3, got %s", last)
	}
	// Delete the highest job dir, as gc's retention could.
	if err := os.RemoveAll(filepath.Join(e.state, "jobs", "job-3")); err != nil {
		t.Fatal(err)
	}
	next := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "after gc"))
	if next == "job-3" {
		t.Fatalf("job-3 was reused after deletion; counter not persisted")
	}
	if next != "job-4" {
		t.Fatalf("expected job-4, got %s", next)
	}

	// Same invariant for workspace IDs.
	repo := initRepo(t)
	var lastWS string
	for i := 0; i < 3; i++ {
		lastWS = e.wsNew(t, repo)["id"].(string)
	}
	if lastWS != "ws-3" {
		t.Fatalf("expected ws-3, got %s", lastWS)
	}
	if err := os.RemoveAll(filepath.Join(e.state, "workspaces", "ws-3")); err != nil {
		t.Fatal(err)
	}
	if got := e.wsNew(t, repo)["id"].(string); got != "ws-4" {
		t.Fatalf("expected ws-4 after ws-3 deletion, got %s", got)
	}
}

// TestGCHalfCreatedDir: a meta-less dir past the grace window is a crash
// remnant and is removed; one inside the grace window (a possible mid-Create)
// is left alone.
func TestGCHalfCreatedDir(t *testing.T) {
	e := newEnv(t)
	cfg := gcConfig(t, "orphan_grace = \"1s\"\n")

	old := filepath.Join(e.state, "jobs", "job-99")
	fresh := filepath.Join(e.state, "jobs", "job-98")
	for _, d := range []string{old, fresh} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	rep := e.gcJSON(t, cfg)
	if kinds(rep)[gcKindHalfCreated] != 1 {
		t.Fatalf("expected 1 half-created removal, got report: %v", rep)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale meta-less dir survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("in-grace meta-less dir was wrongly removed: %v", err)
	}
}

// TestGCCompressesTranscript: a finished job's transcript is gzipped once past
// compress-after, while its index and meta are untouched.
func TestGCCompressesTranscript(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "job"))
	e.waitState(t, id, "done")

	dir := filepath.Join(e.state, "jobs", id)
	plain := filepath.Join(dir, "transcript.jsonl")
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("no transcript to compress: %v", err)
	}

	cfg := gcConfig(t, "transcript_compress_after = \"0s\"\n")
	e.gcJSON(t, cfg)

	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("plain transcript should be gone after compression: %v", err)
	}
	if _, err := os.Stat(plain + ".gz"); err != nil {
		t.Fatalf("compressed transcript missing: %v", err)
	}
	for _, keep := range []string{"meta.json", "events.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("gc removed %s (must persist): %v", keep, err)
		}
	}
}

// TestGCTranscriptRetention: a closed job past its retention horizon loses only
// its transcript; the index and meta remain as the long-horizon audit trail.
func TestGCTranscriptRetention(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	e.writeScript(t, "#write note.txt hi", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "work"))
	e.waitState(t, id, "done")
	e.legwork(t, "close", wsID, "--discard") // sets job state=closed + Closed=now

	dir := filepath.Join(e.state, "jobs", id)
	// Backdate the job's Closed timestamp beyond any small retain window.
	backdateClosed(t, filepath.Join(dir, "meta.json"), time.Now().Add(-72*time.Hour))

	plain := filepath.Join(dir, "transcript.jsonl")
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("no transcript present pre-gc: %v", err)
	}
	cfg := gcConfig(t, "transcript_retain = \"1s\"\n")
	e.gcJSON(t, cfg)

	if _, err := os.Stat(plain); !os.IsNotExist(err) {
		t.Fatalf("transcript should be retention-deleted: %v", err)
	}
	if _, err := os.Stat(plain + ".gz"); !os.IsNotExist(err) {
		t.Fatalf("compressed transcript should also be gone: %v", err)
	}
	for _, keep := range []string{"meta.json", "events.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("retention removed %s (must persist): %v", keep, err)
		}
	}
}

// TestGCReconcilesDeadRunner: a job whose runner died is flipped to interrupted
// by gc (never deleted), with an interrupted event recorded.
func TestGCReconcilesDeadRunner(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, "#sleep 30000", resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "slow"))

	// Wait for the runner PID, then kill its whole process group.
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && pid == 0 {
		out := e.legwork(t, "status", id, "--json")
		var m map[string]any
		_ = json.Unmarshal([]byte(out), &m)
		if p, ok := m["runner_pid"].(float64); ok && p != 0 {
			pid = int(p)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("never observed a runner pid")
	}
	// Kill the runner's whole process group and wait for it to actually die
	// (init reaps the detached leader) so gc sees a dead runner, not a live one.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	for i := 0; i < 100; i++ {
		if syscall.Kill(pid, 0) != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	rep := e.gcJSON(t, gcConfig(t, ""))
	if kinds(rep)[gcKindReconcile] != 1 {
		t.Fatalf("expected gc to reconcile the dead runner, report: %v", rep)
	}
	if m := e.status(t, id); m["state"] != "interrupted" {
		t.Fatalf("state should be interrupted, got %v", m["state"])
	}
	if evs := e.legwork(t, "events", id); !strings.Contains(evs, "interrupted") {
		t.Fatalf("no interrupted event:\n%s", evs)
	}
}

// TestGCDryRunMutatesNothing: --dry-run reports intended reclamation but leaves
// every file in place.
func TestGCDryRunMutatesNothing(t *testing.T) {
	e := newEnv(t)
	e.writeScript(t, resultDone)
	id := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "job"))
	e.waitState(t, id, "done")
	plain := filepath.Join(e.state, "jobs", id, "transcript.jsonl")

	cfg := gcConfig(t, "transcript_compress_after = \"0s\"\n")
	rep := e.gcJSON(t, cfg, "--dry-run")
	if kinds(rep)[gcKindCompress] != 1 {
		t.Fatalf("dry-run should list an intended compress, report: %v", rep)
	}
	if b, _ := rep["bytes"].(float64); b <= 0 {
		t.Fatalf("dry-run should report reclaimable bytes, got %v", rep["bytes"])
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("dry-run mutated the transcript: %v", err)
	}
	if _, err := os.Stat(plain + ".gz"); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a .gz: %v", err)
	}
	// A subsequent non-dry-run must still refresh the gate (real run).
	if _, err := os.Stat(filepath.Join(e.state, ".gc-last")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write .gc-last")
	}
}

// TestGCCloseMerged: an open workspace whose committed branch has landed in the
// default branch is closed (merged) and fully reclaimed; a workspace with
// uncommitted changes is left for human review.
func TestGCCloseMerged(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)

	// Merged workspace: commit in the tree, fast-forward main to include it.
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)
	e.writeScript(t, "#write landed.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "do"))
	e.waitState(t, jid, "done")
	gitIn(t, tree, "add", ".")
	gitIn(t, tree, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "work")
	gitIn(t, repo, "merge", "--ff-only", branch) // land it in main

	// Dirty workspace: uncommitted change, must be skipped.
	ws2 := e.wsNew(t, repo)
	ws2ID := ws2["id"].(string)
	e.writeScript(t, "#write scratch.txt wip", resultDone)
	j2 := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", ws2ID, "wip"))
	e.waitState(t, j2, "done")

	rep := e.gcJSON(t, gcConfig(t, ""), "--close-merged")
	k := kinds(rep)
	if k[gcKindCloseMerged] != 1 {
		t.Fatalf("expected 1 close-merged, report: %v", rep)
	}
	if k[gcKindSkip] != 1 {
		t.Fatalf("expected the dirty workspace skipped, report: %v", rep)
	}

	// Merged workspace fully reclaimed.
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed merged: %v", m)
	} else if m["closed_at"] == "" || m["merged_into"] == "" {
		t.Fatalf("gc close-merged metadata missing: %v", m)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("worktree should be removed: %v", err)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out != "" {
		t.Fatalf("branch %s should be deleted, tip: %s", branch, out)
	}
	if out, _ := gitInErr(repo, "for-each-ref", "refs/legwork/"+wsID); out != "" {
		t.Fatalf("checkpoint refs should be gone: %s", out)
	}
	// Its job is closed too.
	if m := e.status(t, jid); m["state"] != "closed" {
		t.Fatalf("merged workspace job not closed: %v", m["state"])
	}
	// Dirty workspace untouched.
	if m := e.wsStatus(t, ws2ID); m["state"] != "open" {
		t.Fatalf("dirty ws should stay open: %v", m)
	}
}

// TestGCCloseMergedBadRef: a typo'd --close-merged-into surfaces as a failure
// (exit 1), not a silent "everything unmerged" — the workspace stays open.
func TestGCCloseMergedBadRef(t *testing.T) {
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

	out, err := e.cmdEnv([]string{gcConfig(t, "")}, "gc", "--json",
		"--close-merged", "--close-merged-into", "no/such/ref").CombinedOutput()
	// A bad ref -> partial failure -> exit 1 (report still printed).
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("bad --close-merged-into should exit 1, got err=%v\n%s", err, out)
	}
	var rep map[string]any
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if f, _ := rep["failed"].(float64); f < 1 {
		t.Fatalf("expected failed>=1 for the bad ref, report: %v", rep)
	}
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open on a bad ref: %v", m)
	}
}

// TestGCNeverTouchesUnclosed: with plain gc (no --close-merged), an open dirty
// workspace and an unclosed finished job keep their state, worktree, branch,
// and meta intact.
func TestGCNeverTouchesUnclosed(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)
	e.writeScript(t, "#write wip.txt changes", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "work"))
	e.waitState(t, jid, "done")

	e.gcJSON(t, gcConfig(t, "")) // plain gc

	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("open workspace was closed by plain gc: %v", m)
	}
	if m := e.status(t, jid); m["state"] != "done" {
		t.Fatalf("finished job state changed: %v", m["state"])
	}
	if _, err := os.Stat(tree); err != nil {
		t.Fatalf("worktree removed by plain gc: %v", err)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("branch %s wrongly deleted", branch)
	}
}

func TestGCPreservesClosedPreserveCheckpointRefs(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	e.writeScript(t, "#write archived.txt keep this snapshot", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "archive work"))
	e.waitState(t, jid, "done")

	ref := "refs/legwork/" + wsID + "/ckpt-1"
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", ref); out == "" {
		t.Fatalf("checkpoint ref missing before close: %s", ref)
	}

	e.legwork(t, "close", wsID, "--discard", "--preserve")
	e.gcJSON(t, gcConfig(t, ""))

	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", ref); out == "" {
		t.Fatalf("plain gc deleted preserved checkpoint ref %s", ref)
	}
}

// TestGCBlastRadius (DESIGN hard rule): non-legwork branches, non-legwork refs,
// and unrelated worktrees are untouched by every sweep, including --close-merged.
func TestGCBlastRadius(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)

	gitIn(t, repo, "branch", "feature/x")
	gitIn(t, repo, "update-ref", "refs/other/y", "HEAD")
	outsideTree := filepath.Join(t.TempDir(), "other-wt")
	gitIn(t, repo, "worktree", "add", "-q", outsideTree, "feature/x")

	// A real workspace so the sweeps have something legit to consider.
	ws := e.wsNew(t, repo)
	e.writeScript(t, resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", ws["id"].(string), "x"))
	e.waitState(t, jid, "done")

	e.gcJSON(t, gcConfig(t, ""), "--close-merged")

	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/feature/x"); out == "" {
		t.Fatal("feature/x branch was deleted by gc")
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/other/y"); out == "" {
		t.Fatal("refs/other/y was deleted by gc")
	}
	if _, err := os.Stat(outsideTree); err != nil {
		t.Fatalf("unrelated worktree was removed: %v", err)
	}
	wts, _ := gitInErr(repo, "worktree", "list")
	if !strings.Contains(wts, outsideTree) {
		t.Fatalf("unrelated worktree deregistered: %s", wts)
	}
}

// TestGCWorktreePruneScoped: pass 5 cleans a stale legwork worktree
// registration (tree dir gone) but never a foreign prunable worktree — the
// blast-radius rule holds even for a whole-repo-prune-like operation.
func TestGCWorktreePruneScoped(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)

	// A surviving real workspace so gc scans this repo at all.
	ws := e.wsNew(t, repo)

	// A legwork-owned worktree with no meta, under workspaces/. Pass 2 removes
	// the meta-less dir (grace 1s, backdated); pass 5 then clears the now-stale
	// registration by path.
	staleTree := filepath.Join(e.state, "workspaces", "ws-99", "tree")
	gitIn(t, repo, "worktree", "add", "-q", staleTree, "-b", "legwork/ws-99")
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(e.state, "workspaces", "ws-99"), past, past)

	// A foreign worktree whose dir we delete: prunable, but NOT legwork's.
	foreign := filepath.Join(t.TempDir(), "foreign-wt")
	gitIn(t, repo, "worktree", "add", "-q", foreign, "-b", "feature/z")
	if err := os.RemoveAll(foreign); err != nil {
		t.Fatal(err)
	}

	e.gcJSON(t, gcConfig(t, "orphan_grace = \"1s\"\n"))

	list, _ := gitInErr(repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, staleTree) {
		t.Fatalf("stale legwork worktree registration not pruned:\n%s", list)
	}
	if !strings.Contains(list, foreign) {
		t.Fatalf("foreign prunable worktree was wrongly deregistered:\n%s", list)
	}
	// The surviving workspace's registration is intact.
	if !strings.Contains(list, ws["tree"].(string)) {
		t.Fatalf("live workspace worktree deregistered:\n%s", list)
	}
}

// TestGCAutoGated: `gc --auto` honors the interval gate — a second auto within
// the window is a no-op — and the run→auto fork never blocks dispatch latency.
func TestGCAutoGated(t *testing.T) {
	e := newEnv(t)
	cfg := gcConfig(t, "auto_interval = \"24h\"\norphan_grace = \"1s\"\n")

	// First auto-run: due (no .gc-last yet). It writes the gate.
	if out, err := e.cmdEnv([]string{cfg}, "gc", "--auto").CombinedOutput(); err != nil {
		t.Fatalf("gc --auto: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(e.state, ".gc-last")); err != nil {
		t.Fatalf("first auto-run should write .gc-last: %v", err)
	}

	// Plant a stale half-created dir; a gated (second) auto-run must NOT act.
	stale := filepath.Join(e.state, "jobs", "job-77")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	_ = os.Chtimes(stale, past, past)

	if out, err := e.cmdEnv([]string{cfg}, "gc", "--auto").CombinedOutput(); err != nil {
		t.Fatalf("gated gc --auto: %v\n%s", err, out)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("gated auto-run should be a no-op, but removed the dir: %v", err)
	}

	// The run→auto fork must not block: re-enable auto (globally suppressed in
	// TestMain) and drop the gate so a fork actually fires.
	_ = os.Remove(filepath.Join(e.state, ".gc-last"))
	e.writeScript(t, resultDone)
	start := time.Now()
	out, err := e.cmdEnv([]string{cfg, "LEGWORK_NO_AUTO_GC="}, "run", "--agent", "fake", "quick").CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("run took %v with auto-gc due; the fork must not block", elapsed)
	}
	// Let the detached auto-gc child finish before the tempdir is cleaned up.
	waitFor(t, filepath.Join(e.state, ".gc-last"))
}

// --- small helpers ---

const (
	gcKindHalfCreated = "half-created"
	gcKindReconcile   = "reconcile"
	gcKindCompress    = "transcript-compress"
	gcKindCloseMerged = "close-merged"
	gcKindSkip        = "skip"
)

func (e *env) status(t *testing.T, id string) map[string]any {
	t.Helper()
	var m map[string]any
	_ = json.Unmarshal([]byte(e.legwork(t, "status", id, "--json")), &m)
	return m
}

func (e *env) wsStatus(t *testing.T, id string) map[string]any {
	t.Helper()
	var metas []map[string]any
	_ = json.Unmarshal([]byte(e.legwork(t, "ws", "ls", "--json")), &metas)
	for _, m := range metas {
		if m["id"] == id {
			return m
		}
	}
	t.Fatalf("workspace %s not found", id)
	return nil
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := gitInErr(dir, args...); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitInErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func backdateClosed(t *testing.T, metaPath string, when time.Time) {
	t.Helper()
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["closed"] = when.UTC().Format(time.RFC3339Nano)
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(metaPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
