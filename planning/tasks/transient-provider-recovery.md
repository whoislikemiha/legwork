# Transient provider failure recovery

Status: next · Priority: P1 · Origin: job-147 GPT-5.6 Terra capacity failure after successful tool work · Depends: quality-receipts · Workspace: —

## Goal

Do not collapse a turn with preserved workspace progress into an opaque terminal failure when the provider returns a transient capacity/overload error late in the turn. Give orchestrators a truthful, recoverable state and first-class retry path.

## Context & design

- Classify provider overload/capacity/rate-limit failures separately from code/task failures.
- Preserve the last successful text/tool/checkpoint evidence and expose whether workspace progress exists.
- Retry transient failures with bounded backoff when safe; support configured same-agent fallback where the adapter/provider can guarantee session semantics.
- If automatic recovery exhausts, emit an actionable state such as `interrupted` or a typed transient block with `next_actions`, not generic `failed`.
- `resume`/retry should make model/session/fallback behavior explicit in events and status.
- Distinguish provider capacity from quota-reset exhaustion; coordinate with the existing quota/limit observability remainder.

## Constraints

- Never report `done` without a valid final status block.
- Never replay non-idempotent orchestrator actions; retries apply to provider turns, not external side effects.
- Preserve checkpoint/diff evidence and public event-schema compatibility; add versioned event types if needed.
- Noninteractive, `--json`, stable classification/error codes, bounded retries.
- Tests cover capacity before tools, after tool work, after checkpoint-worthy edits, retry success, retry exhaustion, and explicit cancel.

## Blockers

Define adapter-level transient error classification for Claude and Codex.

## Log
