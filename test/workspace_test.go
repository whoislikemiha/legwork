package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireString(t *testing.T, values map[string]any, key string) string {
	t.Helper()
	value, ok := values[key]
	if !ok {
		t.Fatalf("missing %q in %+v", key, values)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("%q = %T, want string", key, value)
	}
	return text
}

func requireObject(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := values[key]
	if !ok {
		t.Fatalf("missing %q in %+v", key, values)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%q = %T, want object", key, value)
	}
	return object
}

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
	var closed map[string]any
	if err := json.Unmarshal([]byte(out), &closed); err != nil {
		t.Fatalf("bad closed job json: %v\n%s", err, out)
	}
	if closed["state"] != "closed" {
		t.Fatalf("job not closed with workspace: %+v", closed)
	}
	outcome, ok := closed["last_outcome"].(map[string]any)
	if !ok || outcome["state"] != "done" || outcome["reason"] != "finished" {
		t.Fatalf("closed job lost worker outcome: %+v", closed)
	}
	if human := e.legwork(t, "status", id); !strings.Contains(human, "last outcome: done — finished") {
		t.Fatalf("closed job did not show its outcome to humans:\n%s", human)
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
	meta := e.wsStatus(t, wsID)
	final, ok := meta["final_commit"].(map[string]any)
	if !ok || final["oid"] == "" || !strings.Contains(final["summary"].(string), "land workspace output") {
		t.Fatalf("final commit not recorded in workspace meta: %v", meta)
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
	} else if m["closed_at"] == "" || m["merged_into"] == "" {
		t.Fatalf("merged close metadata missing: %v", m)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("merged close should drop worktree cache: %v", err)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("merged close should keep branch %s", branch)
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

func TestWorkspaceReviewDispatchesReadOnlyDiffJob(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("changed for review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "review.txt"), []byte("new review target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, resultDone)

	out := e.legwork(t, "ws", "review", wsID, "--agent", "fake", "--run", "pipe", "--json")
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("bad review json: %v\n%s", err, out)
	}
	id := m["id"].(string)
	if m["workspace"] != wsID || m["run"] != "pipe" || m["agent"] != "fake" {
		t.Fatalf("review job not attached correctly: %+v", m)
	}
	if m["read_only"] != true || m["effort"] != "high" {
		t.Fatalf("review defaults not persisted: %+v", m)
	}
	task := m["task"].(string)
	for _, want := range []string{
		"independent reviewer",
		`"verdict":"SHIP|FIX"`,
		"changed for review",
		"review.txt",
	} {
		if !strings.Contains(task, want) {
			t.Fatalf("review prompt missing %q:\n%s", want, task)
		}
	}
	e.waitState(t, id, "done")
	if evs := e.legwork(t, "events", "pipe", "--run", "--json"); !strings.Contains(evs, id) || !strings.Contains(evs, "queued") {
		t.Fatalf("run events missing review job:\n%s", evs)
	}
}

func TestWorkspaceReviewReceiptsAreStrictAndSnapshotBound(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := requireString(t, ws, "id")
	tree := requireString(t, ws, "tree")
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("review receipt target\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reviewLine, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false, "num_turns": 1, "session_id": "s1",
		"result": "I found one landing blocker.\n```json\n{\"verdict\":\"FIX\",\"findings\":[{\"file\":\"README.md\",\"line\":1,\"severity\":\"high\",\"detail\":\"missing test\"}]}\n```\nPlease address it.\n\nstate: done",
	})
	if err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, string(reviewLine))
	id := strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	jm := e.waitState(t, id, "done")
	request := requireObject(t, jm, "review")
	if requireString(t, request, "checkpoint_ref") == "" || requireString(t, request, "checkpoint_oid") == "" || len(requireString(t, request, "diff_sha256")) != 64 {
		t.Fatalf("review job missing immutable dispatch receipt: %+v", jm)
	}
	wm := e.wsStatus(t, wsID)
	receipt := requireObject(t, wm, "latest_review")
	if receipt["job"] != id || receipt["agent"] != "fake" || receipt["state"] != "done" || receipt["parsed"] != true || receipt["verdict"] != "FIX" || receipt["checkpoint_ref"] != request["checkpoint_ref"] || receipt["checkpoint_oid"] != request["checkpoint_oid"] || receipt["diff_sha256"] != request["diff_sha256"] {
		t.Fatalf("bad review receipt: %+v", receipt)
	}
	counts := requireObject(t, receipt, "findings")
	if counts["total"] != float64(1) || counts["high"] != float64(1) {
		t.Fatalf("bad finding counts: %+v", counts)
	}
	if evs := e.legwork(t, "events", id, "--json"); !strings.Contains(evs, "review-verdict") {
		t.Fatalf("review receipt event missing:\n%s", evs)
	}

	// Prose around a bare JSON object must never be guessed into a SHIP verdict.
	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s2","result":"looks good\n{\"verdict\":\"SHIP\",\"findings\":[]}\n\nstate: done"}`)
	badID := strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	e.waitState(t, badID, "done")
	wm = e.wsStatus(t, wsID)
	receipt = requireObject(t, wm, "latest_review")
	if receipt["job"] != badID || receipt["parsed"] != false || receipt["verdict"] != nil || receipt["parse_error"] == "" {
		t.Fatalf("malformed review was not explicit fail-closed: %+v", receipt)
	}
}

func TestWorkspaceReviewDigestBindsExactPromptDiff(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "README.md"), "first\ncontext\n\n")
	commit := exec.Command("git", "-C", repo, "add", "README.md")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("stage base fixture: %v\n%s", err, out)
	}
	commit = exec.Command("git", "-C", repo, "commit", "-m", "blank context")
	commit.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit base fixture: %v\n%s", err, out)
	}

	ws := e.wsNew(t, repo)
	wsID := requireString(t, ws, "id")
	tree := requireString(t, ws, "tree")
	write(filepath.Join(tree, "README.md"), "changed\ncontext\n\n")
	e.writeScript(t, `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"{\"verdict\":\"SHIP\",\"findings\":[]}\n\nstate: done"}`)
	id := strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	jm := e.waitState(t, id, "done")
	request := requireObject(t, jm, "review")
	task := requireString(t, jm, "task")
	const marker = "Workspace diff from legwork diff "
	start := strings.Index(task, "```diff\n")
	if !strings.Contains(task, marker) || start < 0 {
		t.Fatalf("review prompt missing diff fence:\n%s", task)
	}
	start += len("```diff\n")
	end := strings.LastIndex(task, "\n```")
	if end < start {
		t.Fatalf("review prompt has no closing diff fence:\n%s", task)
	}
	promptDiff := task[start:end]
	if !strings.HasSuffix(promptDiff, " \n") {
		t.Fatalf("prompt diff lost blank context: %q", promptDiff)
	}
	sum := sha256.Sum256([]byte(promptDiff))
	if got, want := requireString(t, request, "diff_sha256"), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("digest = %s, want %s for exact prompt diff", got, want)
	}
	base := requireString(t, e.wsStatus(t, wsID), "base_oid")
	oid := requireString(t, request, "checkpoint_oid")
	direct := exec.Command("git", "-C", tree, "diff", base, oid)
	out, err := direct.Output()
	if err != nil {
		t.Fatalf("render direct checkpoint diff: %v", err)
	}
	if promptDiff != string(out) {
		t.Fatalf("prompt diff differs from checkpoint diff:\nprompt=%q\ndirect=%q", promptDiff, out)
	}
	apply := exec.Command("git", "-C", repo, "apply", "--check", "-")
	apply.Stdin = strings.NewReader(promptDiff)
	if out, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("prompt diff is not applyable: %v\n%s", err, out)
	}
}

func TestWorkspaceReviewReceiptFailureKeepsTerminalJob(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := requireString(t, ws, "id")
	tree := requireString(t, ws, "tree")
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("review receipt failure\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.writeScript(t, "#sleep 500", `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"session_id":"s1","result":"{\"verdict\":\"SHIP\",\"findings\":[]}\n\nstate: done"}`)
	id := strings.TrimSpace(e.legwork(t, "ws", "review", wsID, "--agent", "fake"))
	e.waitState(t, id, "active")
	metaPath := filepath.Join(e.state, "workspaces", wsID, "meta.json")
	if err := os.Remove(metaPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metaPath, 0o700); err != nil {
		t.Fatal(err)
	}
	jm := e.waitState(t, id, "done")
	if requireString(t, jm, "result") == "" || requireString(t, jm, "state") != "done" {
		t.Fatalf("receipt failure lost terminal review metadata: %+v", jm)
	}
	events := e.legwork(t, "events", id, "--json")
	if !strings.Contains(events, "review receipt persistence failed") || !strings.Contains(events, "finished") {
		t.Fatalf("receipt failure did not remain observable and terminal:\n%s", events)
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
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("merged close should keep branch %s", branch)
	}
}

func TestCloseMergeIntoLandsAndCloses(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	branch := ws["branch"].(string)

	e.writeScript(t, "#write landed.txt content", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "do"))
	e.waitState(t, jid, "done")
	e.legwork(t, "ws", "commit", wsID, "-m", "workspace output")

	out := e.legwork(t, "close", wsID, "--merge-into", "main", "-m", "land workspace", "--json")
	var got struct {
		OK          bool   `json:"ok"`
		Workspace   string `json:"workspace"`
		State       string `json:"state"`
		Disposition string `json:"disposition"`
		MergedInto  string `json:"merged_into"`
		Merge       struct {
			Target       string `json:"target"`
			TargetBranch string `json:"target_branch"`
			Commit       string `json:"commit"`
			Message      string `json:"message"`
		} `json:"merge"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad close json: %v\n%s", err, out)
	}
	if !got.OK || got.Workspace != wsID || got.State != "closed" || got.Disposition != "merged" {
		t.Fatalf("unexpected close json: %+v", got)
	}
	if got.MergedInto != "refs/heads/main" || got.Merge.Target != "refs/heads/main" || got.Merge.TargetBranch != "main" || got.Merge.Commit == "" {
		t.Fatalf("merge metadata missing: %+v", got)
	}
	if got.Merge.Message != "land workspace" {
		t.Fatalf("merge message = %q", got.Merge.Message)
	}
	if msg, _ := gitInErr(repo, "log", "-1", "--format=%s"); msg != "land workspace" {
		t.Fatalf("target branch did not receive merge commit, log subject %q", msg)
	}
	if content, err := os.ReadFile(filepath.Join(repo, "landed.txt")); err != nil || string(content) != "content\n" {
		t.Fatalf("target branch missing workspace file, content=%q err=%v", content, err)
	}
	// Branch-durable close policy: a merged close keeps the branch and drops
	// only the local checkout cache.
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("workspace branch must survive a merged close")
	}
}

func TestCloseMergeIntoRefusesWorkspaceBranchTarget(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	branch := ws["branch"].(string)

	out, err := e.legworkErr("close", wsID, "--merge-into", branch, "--json")
	if err == nil {
		t.Fatalf("--merge-into workspace branch must fail:\n%s", out)
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("guard refusal exit code = %d, want 3\n%s", code, out)
	}
	var got closeBlockedJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad close error json: %v\n%s", err, out)
	}
	if got.OK || got.State != "blocked" || got.Blocked.Kind != "guard-refused" {
		t.Fatalf("unexpected guard json: %+v", got)
	}
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after guard refusal: %v", m)
	}
}

func TestCloseMergeIntoRefusesUncommittedWorkspace(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	if err := os.WriteFile(filepath.Join(tree, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := e.legworkErr("close", wsID, "--merge-into", "main", "--json")
	assertCloseMergeGuard(t, out, err)
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after uncommitted-work guard: %v", m)
	}
	if status, _ := gitInErr(tree, "status", "--porcelain"); !strings.Contains(status, "scratch.txt") {
		t.Fatalf("workspace uncommitted file should remain:\n%s", status)
	}
}

func TestCloseMergeIntoRefusesDirtyTargetCheckout(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	if err := os.WriteFile(filepath.Join(tree, "landed.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.legwork(t, "ws", "commit", wsID, "-m", "workspace output")
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := e.legworkErr("close", wsID, "--merge-into", "main", "--json")
	assertCloseMergeGuard(t, out, err)
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after dirty-target guard: %v", m)
	}
	if status, _ := gitInErr(repo, "status", "--porcelain"); !strings.Contains(status, "dirty.txt") {
		t.Fatalf("target checkout dirty file should remain:\n%s", status)
	}
}

func TestCloseMergeIntoRefusesRemoteTarget(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	remote := t.TempDir()
	gitIn(t, remote, "init", "-q", "--bare")
	gitIn(t, repo, "remote", "add", "origin", remote)
	gitIn(t, repo, "push", "-q", "origin", "main")
	gitIn(t, repo, "remote", "set-head", "origin", "main")

	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	out, err := e.legworkErr("close", wsID, "--merge-into", "origin/main", "--json")
	assertCloseMergeGuard(t, out, err)
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after remote-target guard: %v", m)
	}
}

func TestCloseMergeIntoRefusesUnresolvedTarget(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	out, err := e.legworkErr("close", wsID, "--merge-into", "no/such/ref", "--json")
	assertCloseMergeGuard(t, out, err)
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after unresolved-target guard: %v", m)
	}
}

func TestCloseMergeIntoConflictAbortsClean(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)

	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.legwork(t, "ws", "commit", wsID, "-m", "workspace readme")

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "main readme")
	gitIn(t, repo, "switch", "-q", "-c", "develop")

	out, err := e.legworkErr("close", wsID, "--merge-into", "main", "--json")
	if err == nil {
		t.Fatalf("conflicting --merge-into must fail:\n%s", out)
	}
	if code := exitCode(err); code != 1 {
		t.Fatalf("conflict exit code = %d, want 1\n%s", code, out)
	}
	var got closeBlockedJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad close conflict json: %v\n%s", err, out)
	}
	if got.OK || got.State != "blocked" || got.Blocked.Kind != "conflict" {
		t.Fatalf("unexpected conflict json: %+v", got)
	}
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace must stay open after conflict: %v", m)
	}
	if status, _ := gitInErr(repo, "status", "--porcelain"); status != "" {
		t.Fatalf("target checkout not clean after aborted conflict:\n%s", status)
	}
	if branch, _ := gitInErr(repo, "symbolic-ref", "--quiet", "--short", "HEAD"); branch != "develop" {
		t.Fatalf("source checkout should be restored to develop after conflict, got %q", branch)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatalf("MERGE_HEAD should be gone after abort, err=%v", err)
	}
	if content, err := os.ReadFile(filepath.Join(repo, "README.md")); err != nil || string(content) != "main\n" {
		t.Fatalf("target file not restored after abort, content=%q err=%v", content, err)
	}
}

type closeBlockedJSON struct {
	OK      bool   `json:"ok"`
	State   string `json:"state"`
	Blocked struct {
		Kind   string `json:"kind"`
		Detail string `json:"detail"`
	} `json:"blocked"`
}

func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func assertCloseMergeGuard(t *testing.T, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("--merge-into guard must fail:\n%s", out)
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("guard refusal exit code = %d, want 3\n%s", code, out)
	}
	var got closeBlockedJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad close guard json: %v\n%s", err, out)
	}
	if got.OK || got.State != "blocked" || got.Blocked.Kind != "guard-refused" {
		t.Fatalf("unexpected guard json: %+v", got)
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

	e.legwork(t, "close", wsID, "--merged", "--force", "--into", "refs/heads/release")
	if m := e.wsStatus(t, wsID); m["state"] != "closed" || m["disposition"] != "merged" {
		t.Fatalf("ws not closed with --force: %v", m)
	} else if m["merged_into"] != "refs/heads/release" {
		t.Fatalf("forced merged close did not record --into target: %v", m)
	}
}

func TestClosePreserveRejectsContradictoryRetention(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)

	out, err := e.legworkErr("close", wsID, "--discard", "--preserve", "--retention", "delete")
	if err == nil {
		t.Fatalf("contradictory --preserve --retention delete must fail:\n%s", out)
	}
	if !strings.Contains(out, "--preserve requires --retention preserve") {
		t.Fatalf("contradictory retention error should be clear:\n%s", out)
	}
	if m := e.wsStatus(t, wsID); m["state"] != "open" {
		t.Fatalf("workspace should stay open after rejected close: %v", m)
	}
}

func TestCloseRecordsArchiveMetadata(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)

	if err := os.WriteFile(filepath.Join(tree, "dead.txt"), []byte("dead branch evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.legwork(t, "close", wsID, "--discard",
		"--reason", "superseded by cleaner plan",
		"--superseded-by", "ws-99",
		"--retention", "preserve",
		"--preserve")

	m := e.wsStatus(t, wsID)
	if m["state"] != "closed" || m["disposition"] != "discard" {
		t.Fatalf("workspace not closed discarded: %v", m)
	}
	for k, want := range map[string]string{
		"reason":        "superseded by cleaner plan",
		"superseded_by": "ws-99",
		"retention":     "preserve",
	} {
		if m[k] != want {
			t.Fatalf("%s = %v, want %q in %v", k, m[k], want, m)
		}
	}
	if m["closed_at"] == "" {
		t.Fatalf("closed_at missing: %v", m)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Fatalf("--preserve should drop worktree cache: %v", err)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("--preserve should keep branch %s", branch)
	}
}

func TestCloseKeepWorktreeKeepsCheckpointRefs(t *testing.T) {
	e := newEnv(t)
	repo := initRepo(t)
	ws := e.wsNew(t, repo)
	wsID := ws["id"].(string)
	tree := ws["tree"].(string)
	branch := ws["branch"].(string)

	e.writeScript(t, "#write keep.txt inspect later", resultDone)
	jid := strings.TrimSpace(e.legwork(t, "run", "--agent", "fake", "--workspace", wsID, "archive worktree"))
	e.waitState(t, jid, "done")

	ref := "refs/legwork/" + wsID + "/ckpt-1"
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", ref); out == "" {
		t.Fatalf("checkpoint ref missing before close: %s", ref)
	}

	e.legwork(t, "close", wsID, "--discard", "--keep-worktree")
	e.gcJSON(t, gcConfig(t, ""))
	if _, err := os.Stat(tree); err != nil {
		t.Fatalf("--keep-worktree should keep worktree: %v", err)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); out == "" {
		t.Fatalf("--keep-worktree should keep checked-out branch %s", branch)
	}
	if out, _ := gitInErr(repo, "rev-parse", "--verify", "--quiet", ref); out == "" {
		t.Fatalf("--keep-worktree should keep checkpoint ref %s", ref)
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
	if out, err := e.legworkErr("ws", "review", wsID, "--agent", "fake"); err == nil {
		t.Fatalf("workspace review must respect active-job lock:\n%s", out)
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
