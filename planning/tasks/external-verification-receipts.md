# External verification receipts

Status: next · Priority: P1 · Origin: job-144/job-146 `blocked: verify` dogfood · Depends: quality-receipts · Workspace: —

## Goal

Make “worker implementation is complete; verification must run outside its sandbox” a first-class, auditable transition instead of leaving a finished implementation permanently `blocked` and forcing the orchestrator to hand-write notes or resume poisoned context.

## Context & design

- Add an explicit orchestrator-side verification operation for a job/workspace, tentatively `legwork verify <job> -- <command>` plus configured `worktree.toml` verification.
- Run outside the worker sandbox in the correct workspace tree, capture command, exit code, bounded output, duration, actor, and timestamp as a durable receipt/event.
- A passing receipt may resolve only `blocked.kind=verify` to a reviewable completed state; it must never turn provision/decision/auth failures into success.
- A failed receipt remains attention-worthy and exposes the exact retry command/result.
- `status`, `ws status`, notifications, and JSON surfaces consume the receipt; no transcript surgery.

## Constraints

- Explicit orchestrator action; never execute worker-supplied shell text automatically.
- New event types are allowed, but existing event fields are immutable without a schema bump.
- No daemon/database/pipeline engine.
- Noninteractive, `--json`, stable exit codes, output bounds and secret redaction.
- Tests cover pass/fail, wrong blocked kind, active job refusal, missing workspace, audit failure, and command injection boundaries.

## Blockers

Define the completed state transition alongside quality receipts so `blocked` history remains truthful while current readiness becomes actionable.

## Log
