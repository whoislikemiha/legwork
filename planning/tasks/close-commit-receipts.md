# Close and commit receipts

Status: next · Priority: P1 · Origin: AUDIT C1–C4 + quality-receipts split · Depends: quality-receipts · Workspace: —

## Goal

Complete the workspace half of durable accountability so a closed workspace has one
queryable identity and an explicit explanation of what was landed or discarded.

## Product contract

- Version workspace metadata additively and keep legacy records readable.
- Close records `merged|discard|clean`, reason, target, actor, and time in metadata and
  a structured Legwork-owned event.
- Commit identity belongs to the workspace, not to each run label that happened to
  contain one of its jobs. Multiple run labels must not create multiple apparent
  commits or close decisions.
- Merge, explicit `--merged`, discard, clean close, and gc close paths produce the same
  receipt shape without weakening the existing tripwire or blast-radius rules.
- Legacy backfill is deterministic, idempotent, and limited to facts already present in
  Legwork workspace/job/run records; unknown facts stay unknown.

## Acceptance criteria

- Current and legacy workspace metadata have stable human and JSON behavior.
- Every successful close path leaves one explicit receipt; failed/blocked close attempts
  cannot look successful.
- The final workspace commit and merge target remain queryable after worktree cleanup.
- Duplicate run labels reference the same workspace receipt rather than duplicating it.
- Migration, close-path, and multi-run tests cover the public contract.

## Non-goals

- Running external verification commands; that is owned by
  `external-verification-receipts.md`.
- A database, daemon, compliance system, or reconstruction from arbitrary git history.

## Log
