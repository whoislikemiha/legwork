# Close and commit receipts

Status: next · Priority: P1 · Origin: AUDIT C1–C4 + quality-receipts split · Depends: quality-receipts · Workspace: —

## Goal

Complete the workspace half of durable accountability so a closed workspace has one
queryable identity and an explicit explanation of what was landed or discarded.

## Product contract

This task ships the complete second receipts slice, but keeps one authority: workspace
metadata. It does not introduce a general provenance framework.

1. **Versioned rollup.** New workspace writes carry an additive schema version. Legacy
   unversioned metadata remains readable and is not rewritten by read-only commands.
2. **Commit receipt.** `final_commit` remains the current workspace commit identity and
   gains a stable receipt ID, actor, message, and timestamp. The existing OID and summary
   fields remain compatible.
3. **Close receipt.** A closed workspace records one stable receipt ID, disposition
   (`merged|discard|clean`), reason, target, actor, close time, retention/supersession
   facts, and the final workspace commit when known. Existing top-level lifecycle fields
   remain compatibility mirrors, not a competing source of truth.
4. **Workspace history.** Each successful workspace commit and close appends one
   structured event to that workspace's own Legwork event log. Existing job/run commit
   events may remain for compatibility, but they reference the same workspace receipt
   ID; multiple run labels do not create distinct commit identities.
5. **One close shape.** Local merge, verified `--merged`, discard, clean close, and gc
   close populate the same receipt. CLI closes identify the actor as `orchestrator`; gc
   identifies itself as `gc`.

Failed tripwire, merge, or verification attempts do not write a close receipt or close
event. Metadata is the authoritative current rollup; event history is append-only detail.
This slice does not weaken reclamation order, branch/ref blast-radius limits, or the
existing explicit-disposition requirements.

Legacy closed workspaces may expose an in-memory compatibility receipt derived only from
their existing lifecycle fields. Unknown actor, commit provenance, or targets remain
unknown. Loading legacy data is deterministic, idempotent, and read-only.

## Acceptance criteria

- Current and legacy workspace metadata have stable human and JSON behavior.
- Every successful close path leaves one explicit receipt; failed/blocked close attempts
  cannot look successful.
- The final workspace commit and merge target remain queryable after worktree cleanup.
- Duplicate run labels reference the same workspace receipt rather than duplicating it.
- `ws commit --json`, `close --json`, and `ws ls --json` expose the same receipt IDs and
  facts without changing existing field meanings.
- Migration, workspace-event, close-path, failure-path, and multi-run tests cover the
  public contract.

## Non-goals

- Running external verification commands; that is owned by
  `external-verification-receipts.md`.
- A database, daemon, compliance system, or reconstruction from arbitrary git history.
- A new `ws status` presentation; actionable workspace rollups remain their own task.

## Log
