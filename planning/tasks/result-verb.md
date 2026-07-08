# `result` verb — first-class access to a job's final report

Status: next · Priority: P1 · Origin: orchestrator field feedback 2026-07-08 (money_intelligence waves 9–11) · Depends: — · Workspace: —

## Goal

`legwork result <job|run>` prints the job's final message (the `result` field) to stdout,
raw. The worker's final report is the *product* of every dispatch turn; fetching it should
not require JSON surgery.

## Context & design

- Observed friction: the single most-repeated command in the 2026-07-08 orchestrator
  session was `legwork status job-X --json | python3 -c "...print(d['result'])"`. Monitor
  `tail` truncates `text` events, so the final report is effectively unreachable except
  through that pipeline.
- Behavior: latest turn's result by default; `--turn N` for earlier turns if turn results
  are retained; `--json` wraps it in the standard envelope for scripts. Exit non-zero if
  the job has no result yet (still running) — that makes `legwork wait ... && legwork
  result ...` a natural pair (see [per-job-wait.md](per-job-wait.md)).
- Accept either a job id or a run name (a run name resolves to its most recent job) —
  consistent with [unified-addressing.md](unified-addressing.md).

## Constraints

- Read-side only; no new state. Raw output to stdout by default (pipe-friendly), no
  decoration — decoration belongs to `status`.

## Blockers

None.

## Log
