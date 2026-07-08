// Package gc is opportunistic reclamation (DESIGN.md §8): it retires
// transcripts and sweeps orphans left by crashes, but only on things legwork
// created and only on work that is closed or provably orphaned — never on
// unclosed work. gc's blast radius is exactly the tool's own artifacts: its
// worktrees (under the state dir), its legwork/* branches, refs/legwork/*, and
// its state dir. Repo content and anything legwork didn't make are untouchable.
package gc

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/whoislikemiha/legwork/internal/config"
	"github.com/whoislikemiha/legwork/internal/job"
	"github.com/whoislikemiha/legwork/internal/workspace"
)

// Config is the [gc] table of config.toml. Durations arrive as strings and are
// parsed by Load; absent values fall back to the defaults below.
type Config struct {
	Auto             bool
	AutoInterval     time.Duration
	CompressAfter    time.Duration
	TranscriptRetain time.Duration
	OrphanGrace      time.Duration
}

// Defaults (orchestrator-approved): compress finished transcripts after 1h,
// retain them 7d past close/finish, auto-run at most every 24h, and treat a
// meta-less dir as half-created only once it's 2h old.
var defaults = Config{
	Auto:             true,
	AutoInterval:     24 * time.Hour,
	CompressAfter:    time.Hour,
	TranscriptRetain: 168 * time.Hour,
	OrphanGrace:      2 * time.Hour,
}

// Load reads the [gc] table, applying defaults for anything unset or blank.
func Load() (Config, error) {
	var raw struct {
		GC struct {
			Auto             *bool  `toml:"auto"`
			AutoInterval     string `toml:"auto_interval"`
			CompressAfter    string `toml:"transcript_compress_after"`
			TranscriptRetain string `toml:"transcript_retain"`
			OrphanGrace      string `toml:"orphan_grace"`
		} `toml:"gc"`
	}
	if _, err := toml.DecodeFile(config.Path(), &raw); err != nil && !os.IsNotExist(err) {
		return defaults, err
	}
	c := defaults
	if raw.GC.Auto != nil {
		c.Auto = *raw.GC.Auto
	}
	parse := func(s string, dst *time.Duration) error {
		if s == "" {
			return nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("[gc] duration %q: %w", s, err)
		}
		*dst = d
		return nil
	}
	for _, p := range []struct {
		s   string
		dst *time.Duration
	}{
		{raw.GC.AutoInterval, &c.AutoInterval},
		{raw.GC.CompressAfter, &c.CompressAfter},
		{raw.GC.TranscriptRetain, &c.TranscriptRetain},
		{raw.GC.OrphanGrace, &c.OrphanGrace},
	} {
		if err := parse(p.s, p.dst); err != nil {
			return c, err
		}
	}
	return c, nil
}

// Options are the per-invocation gc flags.
type Options struct {
	DryRun          bool
	Auto            bool   // opportunistic run; controls only the gate/lock, not passes
	CloseMerged     bool   // opt-in pass 8
	CloseMergedInto string // target ref override; "" -> detect default branch
}

// Action records one reclaimed (or, in dry-run, intended) item.
type Action struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Bytes  int64  `json:"bytes,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Action kinds.
const (
	KindTranscriptCompress = "transcript-compress"
	KindTranscriptDelete   = "transcript-delete"
	KindReconcile          = "reconcile"
	KindHalfCreated        = "half-created"
	KindWorktreePrune      = "worktree-prune"
	KindOrphanRef          = "orphan-ref"
	KindOrphanTree         = "orphan-tree"
	KindOrphanBranch       = "orphan-branch" // report-only
	KindCloseMerged        = "close-merged"
	KindCloseClean         = "close-clean"
	KindSkip               = "skip" // reported, not acted on
)

// Report is the outcome of a gc run.
type Report struct {
	Actions []Action `json:"actions"`
	Bytes   int64    `json:"bytes"`
	Failed  int      `json:"failed"`
}

// engine threads shared state through the passes.
type engine struct {
	js  *job.Store
	ws  *workspace.Store
	cfg Config
	o   Options
	rep Report
	now time.Time
}

func (e *engine) add(a Action) { e.rep.Actions = append(e.rep.Actions, a) }

// do performs f unless dry-run, recording a failure (non-fatal) if it errors.
// In dry-run it only records the intended action.
func (e *engine) do(a Action, f func() error) {
	if e.o.DryRun {
		e.add(a)
		e.rep.Bytes += a.Bytes
		return
	}
	if err := f(); err != nil {
		e.rep.Failed++
		a.Note = strings.TrimSpace(a.Note + " (failed: " + err.Error() + ")")
		e.add(a)
		return
	}
	e.add(a)
	e.rep.Bytes += a.Bytes
}

// Run executes the sweep. Passes 1–7 always run; pass 8 (--close-merged) is
// opt-in. In dry-run it collects intended actions and mutates nothing.
func Run(js *job.Store, ws *workspace.Store, cfg Config, o Options) (Report, error) {
	e := &engine{js: js, ws: ws, cfg: cfg, o: o, now: time.Now()}

	jobs, err := js.List()
	if err != nil {
		return e.rep, err
	}
	wss, err := ws.List()
	if err != nil {
		return e.rep, err
	}

	e.reconcileDead(jobs)   // 1
	e.halfCreated()         // 2
	e.transcripts(jobs)     // 3 + 4
	e.worktreesAndRefs(wss) // 5 + 6
	e.orphanBranches(wss)   // 7
	if o.CloseMerged {      // 8
		e.closeMerged(wss)
	}

	// Refresh .gc-last: the auto gate. Best-effort; not a failure if it can't
	// be written. Skipped in dry-run (nothing was reclaimed).
	if !o.DryRun {
		_ = touch(gcLastPath(js.Root))
	}
	return e.rep, nil
}

// --- pass 1: reconcile dead runners ---

func (e *engine) reconcileDead(jobs []*job.Meta) {
	for _, m := range jobs {
		if e.o.DryRun {
			if m.State == job.StateActive && !e.js.Alive(m) {
				e.add(Action{Kind: KindReconcile, Target: m.ID, Note: "dead runner -> interrupted"})
			}
			continue
		}
		if e.js.Reconcile(m) {
			e.add(Action{Kind: KindReconcile, Target: m.ID, Note: "dead runner -> interrupted"})
		}
	}
}

// --- pass 2: half-created dirs ---

// halfCreated removes meta-less dirs older than the grace window. It holds the
// alloc lock briefly so it can't race AllocID+mkdir (the meta-less window
// between mkdir and the first SaveMeta is ~ms; grace covers it).
func (e *engine) halfCreated() {
	unlock, err := job.LockAlloc(e.js.Root)
	if err != nil {
		return
	}
	defer unlock()

	for _, sub := range []struct{ dir, meta string }{
		{filepath.Join(e.js.Root, "jobs"), "meta.json"},
		{filepath.Join(e.js.Root, "workspaces"), "meta.json"},
	} {
		entries, err := os.ReadDir(sub.dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			dir := filepath.Join(sub.dir, ent.Name())
			if _, err := os.Stat(filepath.Join(dir, sub.meta)); err == nil {
				continue // valid meta: never touch
			}
			info, err := ent.Info()
			if err != nil || e.now.Sub(info.ModTime()) < e.cfg.OrphanGrace {
				continue // inside the grace window: could be mid-Create
			}
			sz := dirSize(dir)
			e.do(Action{Kind: KindHalfCreated, Target: dir, Bytes: sz, Note: "no meta.json past grace"},
				func() error { return os.RemoveAll(dir) })
		}
	}
}

// --- passes 3 + 4: transcript compress + retention delete ---

func (e *engine) transcripts(jobs []*job.Meta) {
	for _, m := range jobs {
		if !terminal(m.State) {
			continue
		}
		dir := e.js.JobDir(m.ID)
		plain := filepath.Join(dir, "transcript.jsonl")
		gz := plain + ".gz"

		// Pass 4 first: if past retention, delete rather than bother compressing.
		if anchor, ok := retentionAnchor(m); ok && e.now.Sub(anchor) >= e.cfg.TranscriptRetain {
			for _, p := range []string{plain, gz} {
				if info, err := os.Stat(p); err == nil {
					path := p
					e.do(Action{Kind: KindTranscriptDelete, Target: m.ID, Bytes: info.Size(),
						Note: "past retention"}, func() error { return os.Remove(path) })
				}
			}
			continue
		}

		// Pass 3: compress a still-retained transcript once it's old enough.
		info, err := os.Stat(plain)
		if err != nil {
			continue
		}
		if _, err := os.Stat(gz); err == nil {
			continue // already compressed (shouldn't coexist, but be safe)
		}
		if e.now.Sub(info.ModTime()) < e.cfg.CompressAfter {
			continue
		}
		e.do(Action{Kind: KindTranscriptCompress, Target: m.ID, Bytes: info.Size()},
			func() error { return gzipFile(plain, gz) })
	}
}

// --- passes 5 + 6: worktree prune + orphan refs ---

func (e *engine) worktreesAndRefs(wss []*workspace.Meta) {
	// Distinct repos referenced by surviving metas, and the set of workspace IDs
	// that still own checkpoint refs. Open workspaces own their checkpoint refs;
	// closed preserve/archive workspaces intentionally keep them for later analysis.
	repos := map[string]bool{}
	refOwners := map[string]bool{}
	for _, m := range wss {
		if m.Repo != "" {
			repos[m.Repo] = true
		}
		if workspaceOwnsRefs(m) {
			refOwners[m.ID] = true
		}
	}

	// Pass 5: prune stale worktree registrations, but only ones whose path is
	// under our state dir's workspaces/. A blanket `git worktree prune` would
	// also drop a foreign prunable worktree, so we enumerate and remove each
	// legwork-owned stale registration by path — the blast-radius rule stays
	// airtight.
	wsRoot := filepath.Join(e.js.Root, "workspaces") + string(os.PathSeparator)
	for repo := range repos {
		repo := repo
		for _, wt := range e.stalePrunableWorktrees(repo, wsRoot) {
			wt := wt
			e.do(Action{Kind: KindWorktreePrune, Target: wt, Note: "stale legwork worktree registration"},
				func() error { _, err := gitOut(repo, "worktree", "remove", "--force", wt); return err })
		}
	}

	// Orphan trees under <root>/workspaces with no loadable owning meta are
	// legwork-owned cache once they are past the same grace window as
	// half-created dirs. The grace window protects workspace.Create through
	// worktree add/bootstrap/first meta save; the alloc lock matches the
	// existing half-created sweep discipline while scanning workspace dirs.
	knownWS := map[string]bool{}
	for _, m := range wss {
		knownWS[m.ID] = true
	}
	if unlock, err := job.LockAlloc(e.js.Root); err == nil {
		e.orphanTrees(knownWS)
		unlock()
	}

	// Pass 6: delete refs/legwork/ws-N owned by no workspace. Closed
	// retention=preserve workspaces remain owners; other closed workspaces have
	// already acknowledged reclamation. The refs/legwork/ namespace is strictly
	// tool-created.
	for repo := range repos {
		out, err := gitOut(repo, "for-each-ref", "--format=%(refname)", "refs/legwork/")
		if err != nil {
			continue
		}
		for _, ref := range strings.Fields(out) {
			id := refWorkspaceID(ref)
			if id == "" || refOwners[id] {
				continue
			}
			ref, repo := ref, repo
			e.do(Action{Kind: KindOrphanRef, Target: ref, Note: "no owning workspace"},
				func() error { _, err := gitOut(repo, "update-ref", "-d", ref); return err })
		}
	}
}

func (e *engine) orphanTrees(knownWS map[string]bool) {
	entries, err := os.ReadDir(filepath.Join(e.js.Root, "workspaces"))
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() || knownWS[ent.Name()] {
			continue
		}
		dir := filepath.Join(e.js.Root, "workspaces", ent.Name())
		info, err := ent.Info()
		if err != nil || e.now.Sub(info.ModTime()) < e.cfg.OrphanGrace {
			continue
		}
		tree := filepath.Join(dir, "tree")
		if info, err := os.Stat(tree); err == nil && info.IsDir() {
			tree := tree
			// With no loadable repo metadata, reclaim the disk cache now. Any
			// stale git worktree registration is pruned later when a surviving
			// workspace gives gc the repo to scan.
			e.do(Action{Kind: KindOrphanTree, Target: tree, Bytes: dirSize(tree), Note: "tree with no loadable meta past grace"},
				func() error { return os.RemoveAll(tree) })
		}
	}
}

// stalePrunableWorktrees returns registered worktree paths in repo that are
// (a) under the legwork state dir's workspaces/ and (b) stale — their working
// directory no longer exists on disk (exactly `git worktree prune`'s criterion,
// e.g. after pass 2 removed a half-created ws dir). Scoping by path guarantees
// a foreign prunable worktree is never touched.
func (e *engine) stalePrunableWorktrees(repo, wsRoot string) []string {
	out, err := gitOut(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var stale []string
	for _, line := range strings.Split(out, "\n") {
		path := strings.TrimPrefix(line, "worktree ")
		if path == line {
			continue // not a "worktree <path>" line
		}
		if !strings.HasPrefix(path, wsRoot) {
			continue // foreign worktree: never touch
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			stale = append(stale, path)
		}
	}
	return stale
}

// --- pass 7: orphan branches (report-only) ---

func (e *engine) orphanBranches(wss []*workspace.Meta) {
	knownBranch := map[string]bool{} // "repo\x00legwork/ws-N" for branch-owning workspaces
	repos := map[string]bool{}
	for _, m := range wss {
		if m.Repo != "" {
			repos[m.Repo] = true
		}
		if workspaceOwnsBranch(m) {
			knownBranch[m.Repo+"\x00"+m.Branch] = true
		}
	}
	for repo := range repos {
		out, err := gitOut(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/legwork/")
		if err != nil {
			continue
		}
		for _, br := range strings.Fields(out) {
			if knownBranch[repo+"\x00"+br] {
				continue
			}
			e.add(Action{Kind: KindOrphanBranch, Target: br,
				Note: "no open workspace (report-only; may hold unmerged work)"})
		}
	}
}

// --- pass 8: --close-merged ---

func (e *engine) closeMerged(wss []*workspace.Meta) {
	for _, m := range wss {
		if m.State != "open" {
			continue
		}
		if e.hasActiveJob(m.ID) {
			continue // a running/queued turn is human territory
		}
		// A missing worktree is an expected anomaly (e.g. a crash between
		// creation and the meta save, or a manual removal): report a skip
		// rather than inflating the failure count / exit code.
		if _, err := os.Stat(m.Tree); os.IsNotExist(err) {
			e.add(Action{Kind: KindSkip, Target: m.ID, Note: "worktree missing; nothing to close-merge"})
			continue
		}
		// Uncommitted (not vs-base) changes are the human-judgment signal:
		// committed work that has landed is exactly what --close-merged closes.
		uncommitted, err := e.ws.Uncommitted(m)
		if err != nil {
			e.rep.Failed++
			e.add(Action{Kind: KindSkip, Target: m.ID, Note: "status check failed: " + err.Error()})
			continue
		}
		if uncommitted {
			e.add(Action{Kind: KindSkip, Target: m.ID, Note: "uncommitted changes; human review"})
			continue
		}

		target := e.o.CloseMergedInto
		if target == "" {
			ref, _ := e.ws.DefaultBranchTip(m.Repo)
			if ref == "" {
				e.add(Action{Kind: KindSkip, Target: m.ID, Note: "no default branch resolved; pass --close-merged-into"})
				continue
			}
			target = ref
		}

		// Nothing committed on the branch -> close clean.
		if tip := e.ws.BranchTip(m); tip != "" && tip == m.BaseOID {
			e.closeOne(m, "clean", "", KindCloseClean, "nothing committed")
			continue
		}
		merged, err := e.ws.MergedInto(m, target)
		if err != nil {
			e.rep.Failed++
			e.add(Action{Kind: KindSkip, Target: m.ID, Note: "merge check failed: " + err.Error()})
			continue
		}
		if merged {
			e.closeOne(m, "merged", target, KindCloseMerged, "landed in "+target)
			continue
		}
		e.add(Action{Kind: KindSkip, Target: m.ID, Note: "unmerged, needs human review"})
	}
}

func (e *engine) closeOne(m *workspace.Meta, disposition, mergedInto, kind, note string) {
	if e.o.DryRun {
		e.add(Action{Kind: kind, Target: m.ID, Note: note})
		return
	}
	if err := e.ws.CloseMerged(m, disposition, mergedInto); err != nil {
		e.rep.Failed++
		e.add(Action{Kind: kind, Target: m.ID, Note: note + " (failed: " + err.Error() + ")"})
		return
	}
	_ = e.js.CloseJobsForWorkspace(m.ID)
	e.add(Action{Kind: kind, Target: m.ID, Note: note})
}

func (e *engine) hasActiveJob(wsID string) bool {
	jobs, err := e.js.List()
	if err != nil {
		return true // fail safe: don't close if we can't tell
	}
	for _, m := range jobs {
		if m.Workspace != wsID {
			continue
		}
		e.js.Reconcile(m)
		if m.State == job.StateActive || m.State == job.StateQueued {
			return true
		}
	}
	return false
}

// --- MaybeAuto: git-style opportunistic trigger ---

// MaybeAuto spawns a detached `legwork gc --auto` if auto is enabled and the
// interval has elapsed. The gate is a single stat; the child is released, not
// awaited — the sacred detachment latency of run/resume is preserved.
func MaybeAuto(store *job.Store) {
	// Escape hatch: an operator (or the test suite) can suppress the
	// opportunistic fork entirely without touching config.
	if os.Getenv("LEGWORK_NO_AUTO_GC") != "" {
		return
	}
	cfg, err := Load()
	if err != nil || !cfg.Auto {
		return
	}
	if info, err := os.Stat(gcLastPath(store.Root)); err == nil {
		if time.Since(info.ModTime()) < cfg.AutoInterval {
			return // not due: no fork
		}
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	logf, err := os.OpenFile(filepath.Join(store.Root, "gc.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer logf.Close()

	cmd := exec.Command(self, "gc", "--auto")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	_ = cmd.Process.Release()
}

// TryLockAuto takes a non-blocking exclusive lock so only one auto-gc runs at a
// time (no thundering herd). ok=false means another auto-gc holds it. The
// returned func releases the lock; call it only when ok is true.
func TryLockAuto(root string) (release func(), ok bool) {
	f, err := os.OpenFile(filepath.Join(root, ".gc-auto.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return func() {}, false
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true
}

// AutoDue reports whether an auto-run's interval gate has elapsed.
func AutoDue(root string, interval time.Duration) bool {
	info, err := os.Stat(gcLastPath(root))
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) >= interval
}

// --- helpers ---

func gcLastPath(root string) string { return filepath.Join(root, ".gc-last") }

func touch(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// terminal reports whether a job turn has ended in a state whose transcript is
// stable (safe to compress/retire). Interrupted/needs-input/queued are still
// in flight or resumable and are left alone.
func terminal(s job.State) bool {
	switch s {
	case job.StateDone, job.StateFailed, job.StateBlocked, job.StateClosed:
		return true
	}
	return false
}

// retentionAnchor is the clock start for transcript deletion: Closed when set,
// else a non-workspace terminal job's finish time (Updated). Workspace-attached
// jobs only retention-collect after close sets Closed (ok=false until then).
func retentionAnchor(m *job.Meta) (time.Time, bool) {
	if !m.Closed.IsZero() {
		return m.Closed, true
	}
	if m.Workspace != "" {
		return time.Time{}, false
	}
	if m.State == job.StateDone || m.State == job.StateFailed || m.State == job.StateBlocked {
		return m.Updated, true
	}
	return time.Time{}, false
}

// refWorkspaceID extracts ws-N from refs/legwork/ws-N/ckpt-K.
func refWorkspaceID(ref string) string {
	rest := strings.TrimPrefix(ref, "refs/legwork/")
	if rest == ref {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func workspaceOwnsRefs(m *workspace.Meta) bool {
	if m.State == "open" {
		return true
	}
	if m.State != "closed" {
		return false
	}
	if m.Retention == "preserve" {
		return true
	}
	return worktreeExists(m)
}

func workspaceOwnsBranch(m *workspace.Meta) bool {
	if m.State == "open" {
		return true
	}
	if m.State != "closed" {
		return false
	}
	return m.Disposition != "discard" || m.Retention == "preserve" || worktreeExists(m)
}

func worktreeExists(m *workspace.Meta) bool {
	if m.Tree == "" {
		return false
	}
	info, err := os.Stat(m.Tree)
	return err == nil && info.IsDir()
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		zw.Close()
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}
