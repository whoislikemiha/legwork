# Writable per-job $TMPDIR in every sandbox profile

Status: next · Priority: P0 · Origin: AUDIT A2 (+ prior ROADMAP Next #2) · Depends: — · Workspace: —

## Goal

Every sandbox profile — **including `--read-only`** — gets a writable, per-job temp directory.
Read-only must mean *repo* read-only, not *no-tmp*. Diagnose the FastAPI `TestClient`/anyio hang
while here.

## Context & design

- Today `--read-only` reviewers cannot run pytest at all ("no usable temp directory for
  capture", uv cache-lock failures), so reviewers finish `blocked` with real work done but
  unverified. Evidence: `job-28` (MCP gate), `job-38` (assistant hardening), `job-44` (XFF fix)
  all implemented changes then blocked because the in-sandbox suite could not run; `job-16`
  blocked on a git worktree lock.
- `anyio.start_blocking_portal` (FastAPI `TestClient`) reproducibly hangs in the workspace-write
  sandbox — two independent jobs hit it. Provide a writable `$TMPDIR` first; if the hang persists
  with tmp available, it is the thread/socketpair policy, not tmp — diagnose the sandbox's
  socketpair/thread rules.
- Implementation surface: sandbox setup lives per-adapter (`internal/adapter` claude/codex) and
  the runner env in `internal/runner`. Point `TMPDIR` (and codex's Go caches — see the sandbox
  validation remainder: `GOCACHE`/`GOMODCACHE`/`GOTMPDIR`) at a per-job scratch path under the
  job dir or `/tmp`, writable in read-only mode, cleaned on close.
- Keep caches OUT of the reviewed worktree (they polluted diffs in the dogfood runs).

## Constraints

- Read-only sandbox must stay read-only for the *repo/worktree*; only the temp/cache path becomes
  writable. Do not widen the blast radius.
- Verify against a real agent (AGENTS.md smoke): a `--read-only` codex reviewer must run pytest;
  a workspace-write job must not hang in `TestClient`. The fake-agent suite cannot catch this.
- Applies to both adapters; document any asymmetry in capability flags.

## Blockers

None. Mechanical for the tmp part; the hang diagnosis may need a live repro.

## Log

- Implemented per-job `jobs/<id>/tmp` creation in the runner and inject `TMPDIR` into every agent process. Codex turns also get `GOCACHE`, `GOMODCACHE`, and `GOTMPDIR` under that temp tree; workspace-write codex turns add the temp tree as a sandbox writable root so caches stay out of the reviewed worktree.
- Cleanup: `ack` and workspace `close` remove each closed job's temp/cache tree while leaving events, transcripts, and artifacts on the normal retention path.
- Files touched: `internal/runner/runner.go`, `internal/adapter/adapter.go`, `internal/adapter/codex.go`, `internal/job/job.go`, `internal/adapter/codex_test.go`, `internal/runner/runner_test.go`, `test/e2e_test.go`, `internal/guide/guide.md`, `skills/legwork/SKILL.md`, `README.md`.
- Tests added: runner unit coverage for temp layout/env merging, Codex command coverage for workspace-write writable-root config and absence of inert read-only writable-root config, and e2e coverage that `ack` removes the job temp dir.
- anyio/FastAPI `TestClient` hang hypothesis: because the hang was observed in a different Python/FastAPI repo and not reproducible in this Go workspace, I did not attempt a local repro. If the hang persists after writable `TMPDIR`, the likely root is Codex sandbox syscall policy around thread creation and/or `socketpair`/loopback primitives used by `anyio.start_blocking_portal`, not temp-file creation. The next confirmation should run the original FastAPI test under a real Codex workspace-write job with this change; if temp/cache writes succeed but the portal still stalls, inspect denied syscalls or socket/thread restrictions in the sandbox.
- Review fix 1/3/4: verified against local Codex 0.142.5 help/binary strings that writable roots are a workspace-write facility and no read-only writable-root exception is exposed. Stopped emitting inert `sandbox_workspace_write.*` keys on codex read-only turns, changed the read-only unit test to assert those keys are absent, and documented the codex read-only asymmetry in the guide, README, and skill.
- Review fix 2: per orchestrator instruction for this follow-up, skipped real codex/claude smoke checks; orchestrator will run them outside this sandbox.
- Review fix 5: made job temp cleanup best-effort after close metadata/event recording so `ack`/workspace close cannot be wedged by a transient `RemoveAll` failure.

## Verdict

Review job-126 (opus, high): FIX round 1 (inert read-only writable_roots, skipped smoke,
presence-not-effect test, overclaiming docs, close-wedging cleanup); honest-asymmetry route
taken per orchestrator guidance; job-131: **SHIP** round 2. Orchestrator verification: suite
green on main; claude + codex real-agent smokes green (PLUMBING-OK both). Landed on main
2026-07-08 via merge of legwork/ws-56 (ad189f9).
