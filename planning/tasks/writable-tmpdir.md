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
