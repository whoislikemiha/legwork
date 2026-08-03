# Richer structured review findings

Status: later · Priority: P2 · Origin: [Flows](../done/flows.md) independent-reviewer contract · Depends: quality-receipts · Workspace: —

## Goal

[Flows](../done/flows.md)'s independent-reviewer contract spans correctness/edge-cases, test
adequacy, security/trust boundaries, data-integrity/concurrency/idempotency,
compatibility/migrations/public contracts, simplicity/complexity/scope,
cleanliness/maintainability/drift, relevant performance/operability, and docs. Today's
`ws review` receipt (`internal/workspace/workspace.go` `ReviewReceipt`/
`FindingCounts`) only persists a verdict and a severity-bucketed count; individual
findings — file, line, severity, detail, and now a dimension — live only in the
reviewer job's raw result text. An orchestrator routing `FIX` back to an implementer,
or reconciling re-review, has no addressable, per-finding identity to cite or to check
off.

## Desired experience

- The requested verdict JSON gains a stable per-finding `id` and a `dimension` drawn
  from a fixed set matching the reviewer contract above (e.g. `correctness`, `tests`,
  `security`, `data-integrity`, `compatibility`, `complexity`, `cleanliness`,
  `performance`, `docs`).
- Individual findings (not just counts) persist in workspace metadata against the
  reviewed checkpoint/diff digest, so `status`/`ws review` history can list them
  without re-reading the job's raw result.
- A `FIX` route can cite stable, persisted finding IDs directly, replacing today's
  manual `review-job-id#index` convention documented in the flow ledger (the ledger
  does not assume persisted IDs exist yet — this task is what would introduce them);
  re-review can mark which prior IDs were addressed.
- Existing `verdict`/`FindingCounts` fields and current parsing behavior remain
  read-compatible; this is additive, versioned the same way workspace metadata already
  is (v1 → v2 in [external verification receipts](../done/external-verification-receipts.md)).

## Acceptance criteria

- Malformed/missing per-finding fields fail closed the same way a malformed verdict
  does today — never a guessed dimension or silently dropped finding.
- Legacy receipts without dimensions/IDs remain readable; `status`/`ws review` degrade
  to the current counts-only view for them.
- The fixed dimension set is documented once (guide + this task) and reused by
  `legwork guide`'s reviewer-contract text rather than duplicated.
- A re-review can reference which prior finding IDs it considers resolved.

## Non-goals

- Auto-fixing findings or auto-routing FIX without orchestrator judgment.
- A general free-form tagging system — the dimension set is fixed and small.

## Log
