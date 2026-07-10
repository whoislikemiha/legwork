# Durable quality and lifecycle receipts

Status: next · Priority: P1 · Origin: AUDIT C1–C4 · Depends: — · Workspace: —

## Goal

Make the decisions that advance or close work as queryable as the commit itself.
After a workspace is closed, an orchestrator should still be able to answer: what did
the worker conclude, who reviewed it, what was the verdict, and why was it closed?

## Product contract

Persist compact, structured receipts for these boundaries:

1. **Turn outcome** — preserve the last worker state/reason after lifecycle state
   changes to `closed`.
2. **Review verdict** — latest `SHIP|FIX`, reviewer job/model, finding counts, and
   the checkpoint/diff reviewed.
3. **Close disposition** — merged/discarded/clean, reason, target, actor, and time.
4. **Commit identity** — one stable workspace/commit identity even when the same
   workspace appears under multiple run labels.

The existing event log remains history; metadata is the current rollup. New event
types and additive metadata fields are allowed, but existing event fields are not
redefined without a schema version and migration story.

Legacy workspace metadata should be versioned and best-effort backfilled from
Legwork-owned event files only. Backfill is idempotent and never touches git history.

## Boundaries and sequencing

- Ship last-turn preservation and structured review verdicts first; they unlock the
  largest status improvements.
- [External verification receipts](external-verification-receipts.md) owns running
  host-side commands and records its receipt in this accountability shape.
- [Actionable workspace status](actionable-workspace-status.md) consumes receipts; it
  does not invent them by parsing prose.
- Cost accounting remains separate from quality/lifecycle decisions.

## Acceptance criteria

- Closed jobs retain their final worker outcome without event-log archaeology.
- `ws review` produces a machine-readable verdict tied to the reviewed diff.
- Close emits a structured event and metadata with explicit disposition/reason.
- Duplicate run labels do not make one workspace commit look like multiple commits.
- Current and legacy records have deterministic human/JSON behavior and migration
  tests.
- The close tripwire and gc blast-radius rules remain unchanged.

## Non-goals

- A database, daemon, compliance system, or pipeline state machine.
- Reconstructing facts that are not present in Legwork-owned records.

## Log
