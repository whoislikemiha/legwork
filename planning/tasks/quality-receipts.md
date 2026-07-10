# Durable quality and lifecycle receipts

Status: in flight · Priority: P1 · Origin: AUDIT C1–C4 · Depends: — · Workspace: ws-76

## Goal

Make the decisions that advance or close work as queryable as the commit itself.
After a workspace is closed, an orchestrator should still be able to answer: what did
the worker conclude, who reviewed it, what was the verdict, and why was it closed?

## First-slice contract

This task ships the two highest-leverage boundaries only:

1. **Turn outcome** — preserve the last worker state/reason after lifecycle state
   changes to `closed`.
2. **Review verdict** — latest `SHIP|FIX`, reviewer job/model, finding counts, and
   the checkpoint/diff reviewed.

`ws review` already owns a strict verdict JSON contract. Persist its parsed result in
workspace metadata when the reviewer turn finishes, tied to the exact checkpoint and
diff it received. Missing or malformed verdict JSON is recorded as unparsed/fail-closed,
never guessed from prose. The reviewer job remains the historical source.

The existing event log remains history; metadata is the current rollup. New event
types and additive metadata fields are allowed, but existing event fields are not
redefined without a schema version and migration story.

Legacy closed jobs get deterministic, best-effort last-turn backfill from their own
Legwork event log. Historical review verdicts are not reconstructed when no structured
receipt exists.

## Boundaries and sequencing

- [Close and commit receipts](close-commit-receipts.md) owns the remaining workspace
  schema/versioning, close actor/event, disposition, and stable commit-identity work.
- [External verification receipts](external-verification-receipts.md) owns running
  host-side commands and records its receipt in this accountability shape.
- [Actionable workspace status](actionable-workspace-status.md) consumes receipts; it
  does not invent them by parsing prose.
- Cost accounting remains separate from quality/lifecycle decisions.

## Acceptance criteria

- Closed jobs retain their final worker outcome without event-log archaeology.
- `ws review` produces a machine-readable verdict tied to the reviewed diff.
- Missing/malformed review JSON is explicit and cannot become a false `SHIP`.
- Current and legacy closed jobs have deterministic human/JSON behavior and focused
  backfill tests.
- The close tripwire and gc blast-radius rules remain unchanged.

## Non-goals

- A database, daemon, compliance system, or pipeline state machine.
- Reconstructing facts that are not present in Legwork-owned records.
- Changing close/commit semantics, workspace metadata versioning, or run-label rollups
  in this slice.

## Log

- Terra job `job-172` implemented the first slice; the host full gate and a real
  Claude/Haiku runner smoke passed. Opus/xhigh `job-176` returned `FIX` and the
  feature's own first receipt reproduced the primary failure as `parsed:false` on
  ordinary prose plus fenced JSON. The review also reproduced terminal-result loss
  on receipt-write failure and trimmed/corrupt review diffs.
- A fresh Terra job, `job-180`, applied the accepted corrections after the resumed
  implementer crossed the context-health threshold. The host gate passed again.
  Focused Opus re-review `job-181` proved the corrected receipt path end-to-end:
  workspace metadata recorded `parsed:true`, `verdict:FIX`, and exact finding counts.
  Its remaining medium interrupted-turn receipt defect and three low findings were
  fixed directly by the orchestrator; focused and full repository gates then passed.

## Friction

- 2026-07-10: `go vet ./...` / `go test ./...` cannot fetch uncached Go modules in
  the worker sandbox because outbound DNS/network is denied. Focused standard-library
  package tests can run, but full repository verification needs a warm module cache or
  host-side execution.
- 2026-07-10: resuming `job-172` exposed `context_high:true` with a 6.42M context
  rollup only after dispatch. The turn was cancelled and reseeded into fresh `job-180`;
  a pre-resume guard would avoid starting work that the health signal already says to
  abandon.
