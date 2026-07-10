# Per-job blocking wait

Status: next · Priority: P1 · Origin: AUDIT E1 (+ field-notes 2026-07-07 #1) · Depends: — · Workspace: —

## Goal

A first-class blocking verb: `legwork wait --job X --until done|blocked|needs-input`. Exits 0 when
the job reaches a target state, non-zero on timeout. The scriptable "wake me when this specific
job needs me".

## Context & design

- `tail --until-idle` (used to drive the 2026-07-08 dogfood review) already blocks and exits when
  no job in scope is active — it is the run-scoped wait. The gap is **granularity**: it is
  run-scoped, so after every `resume` the orchestrator must re-attach a monitor, and per-job facts
  ("job interrupted, diff persists"; "this one hit needs-input") are lost in run-scoped idle.
  After `cancel`, run-scoped `--until-idle` fires even though the interesting fact is one job's.
- Design: `wait` blocks on the job's event stream (reuse the cursor reads that `events`/`tail`
  use in `internal/events`/`internal/timeline`) until `finished` with a matching state (or any
  terminal state if `--until` omitted). Support `--timeout`; exit codes distinguish
  reached-target / timeout / job-not-found, consistent with the existing exit-code table.
- Complements, does not replace, the notifier (push wake-on-event) and `tail --until-idle`
  (pipeline wait). This is the pull-side blocking primitive for a single job.

## Constraints

- Never interactively prompt; `--json` supported; stable exit codes (contract).
- Pure read-side over existing event logs — no new event types, no schema change.
- Must handle a job that is already terminal (return immediately) and a dead runner
  (liveness → `interrupted`, don't hang forever).

## Blockers

None.

## Log
