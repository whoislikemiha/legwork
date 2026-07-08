# Worktree reclamation — branch is durable, worktree is cache

Status: next · Priority: P1 · Origin: AUDIT G (disk review 2026-07-08) · Depends: — · Workspace: —

## Goal

Stop worktrees from being the dominant disk cost. legwork's own data is tiny (32 M for 96 jobs);
the state dir was 2.5 G, ~1.6 G of it **gitignored** `node_modules`/`.next` inside worktrees. The
durable artifact of a workspace is its **branch/commit in the shared git object store**, not the
on-disk checkout — so once `ws commit` records the tree, the worktree directory is disposable
cache and should be removed.

## Context & design

Measured 2026-07-08 (real state dir):

- `jobs/` 32 M, `runs/` 308 K — legwork's own logs are bounded (transcripts gzip after 1 h,
  delete 7 d post-close; two-tier index). Not the problem.
- `workspaces/` 2.0 G — the whole problem. `ws-51/tree` 1.2 G (open), `ws-27/tree` 814 M
  (orphan, meta-less). `node_modules` 683 M + 643 M, `.next` 300 M — all gitignored, zero review
  value. `ws-27` has 0 tracked changes: 814 M pinning ~0 bytes of diff.
- **`ws-13`/`ws-18`/`ws-24`/`ws-25`/`ws-26` are closed but keep both worktree AND branch** —
  closed with `--keep-worktree`/`--preserve` (reclaim skipped). These are legwork-repo trees (KB,
  not the byte problem), but they expose the real gap: **there is no "keep the branch, drop the
  local worktree" mode.** `reclaim()` (workspace.go:399) fuses three deletions — checkpoint refs
  + `git worktree remove --force` + `git branch -D` — and the only way to keep the branch
  (`--keep-worktree`/`--preserve`) also keeps the whole checkout. For a money_intelligence
  workspace that means pinning ~650 M of npm cache just to keep a branch that costs ~nothing.

Design (policy: branches are free, worktrees cost disk):

- **Decouple `reclaim()`.** Split "drop the local worktree + checkpoint refs" (pure local cache,
  always safe once the tree is committed) from "delete the branch" (throwing work away). Today
  they are one fused operation; that is the root cause.
- **Default close = drop the worktree, KEEP the branch.** A git worktree shares the main repo's
  object store, so removing the directory loses nothing while `legwork/ws-N` survives; the commit
  stays reachable and `git worktree add` rehydrates the checkout on demand. The branch (source
  objects only — `node_modules` is gitignored, never in git) costs ~nothing.
- **Push the branch to origin, do not delete it** (`ws publish` / `close --push`, the ROADMAP
  archive/publish item). Branches are free on GitHub; a pushed branch is durable off-machine, so
  the local ref can even be pruned later without losing history. Branch deletion is **opt-in only**
  (`--discard` = deliberately throw away); it is never the default, and merged branches are kept,
  not auto-deleted.
- **`--preserve` should mean keep the branch, not the checkout.** Preserving for analysis keeps
  the branch (+ optional checkpoint refs); rehydrate the tree only if someone needs it — never pin
  650 M of cache for a KB of diff.
- **Make orphan-tree reclaim actionable.** gc pass 5 only *reports* a legwork-owned meta-less
  `workspaces/<id>/tree` ("a repo we can't identify safely is a human decision"), so `ws-27`'s
  814 M lingers; auto-gc is 24 h-gated. Force-remove trees that are provably ours (under our state
  dir) and resolve the pass-2-removes / pass-5-reports disagreement — but still only ever touch
  worktrees, never branches.
- **Keep caches out of the reviewed tree (secondary).** Point setup-hook installs
  (`npm ci` / `uv sync`) at a shared cache / `node_modules` outside the worktree so even live
  workspaces stay lean and diffs are not polluted — overlaps writable-tmpdir.md.

## Constraints

- **Never delete the only copy.** The `close` tripwire stays: refuse to drop a worktree whose
  changes are not committed/reachable without an explicit disposition. Worktree removal is only
  safe after `ws commit` (or a merged/discard disposition).
- **Branches are kept by default; deletion is opt-in.** Never `git branch -D` on a normal close —
  keep the ref (and prefer pushing it to origin). Only `--discard` deletes a branch, and only
  after the tripwire is satisfied. This inverts today's `reclaim()`, which always deletes.
- **Blast radius unchanged** (AGENTS.md): only legwork-created worktrees/branches/refs under the
  state dir; never repo content.
- Event schema: a `worktree-reclaimed` / rehydrate event may be a new type; don't redefine
  existing fields without a `v` bump.

## Blockers

None. Immediate mitigation works today (`legwork gc` frees ~740 MB; `--close-merged` /
manual removal for orphan + closed-but-kept trees); this task makes reclamation automatic and
total. Coordinate the "caches outside the tree" sub-item with writable-tmpdir.md.

## Log
