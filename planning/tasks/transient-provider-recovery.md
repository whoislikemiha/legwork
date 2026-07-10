# Transient provider failure recovery

Status: next · Priority: P1 · Origin: job-147 capacity failure after useful tool work · Depends: quality-receipts · Workspace: —

## Goal

When a provider fails because of capacity, overload, rate limits, or a temporary
transport problem, preserve useful workspace progress and give the orchestrator an
honest recovery path instead of collapsing everything into generic `failed`.

## Desired behavior

- Adapters classify transient provider failures separately from task/code failure,
  auth failure, quota exhaustion, and explicit cancellation.
- Status exposes a stable kind, retryability, provider message, retry/reset time when
  known, and whether tool/file/checkpoint progress exists.
- The latest valid checkpoint, diff, transcript, and successful events remain visible.
- Recovery actions state whether they will resume the same provider session or start a
  fresh job from preserved artifacts.
- Model/fallback/retry choices are recorded as events; a provider failure never erases
  which model actually ran.

## Retry safety

- Before any external tool or file-changing work, a small bounded automatic retry may
  be allowed when the adapter can prove the turn is safe to replay.
- After useful tool work, Legwork does not automatically replay the turn. It surfaces
  an explicit orchestrator action because commands and side effects may not be
  idempotent.
- Exhausted retries remain actionable and never become `done` without a valid worker
  status block.

## Acceptance criteria

- Capacity before work, capacity after edits, rate limit with reset, quota exhaustion,
  auth failure, transport interruption, and explicit cancel are distinguishable.
- A late provider failure leaves the workspace diff/checkpoint reviewable.
- Human and JSON status recommend a bounded next action rather than generic “retry”.
- Retry attempts and model/session changes are auditable.
- Fake-adapter tests cover classification and replay guards; live probes cover provider
  wording without depending on a real outage.

## Non-goals

- A general scheduler, infinite retries, or replaying arbitrary side effects.
- Hiding provider failures behind silent lower-quality fallback.

## Log
