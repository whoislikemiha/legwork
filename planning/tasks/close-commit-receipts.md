# Close and commit receipts

Status: in flight · Priority: P1 · Origin: AUDIT C1–C4 + quality-receipts split · Depends: quality-receipts · Workspace: ws-80

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
4. **Workspace history.** Each successful workspace commit and close attempts to append
   one structured event to that workspace's own Legwork event log. If the operation is
   already durable and the history append fails, it remains successful and its receipt
   carries `history_error`; it must not be replayed. Existing job/run commit events may
   remain for compatibility, but they reference the same workspace receipt ID; multiple
   run labels do not create distinct commit identities.
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

- Terra job `job-185` implemented the first pass; the full host gate passed before
  review.
- Opus/xhigh job `job-186` returned `FIX`: two high-severity partial-success traps
  plus workspace-history queryability, future-schema, unknown-timestamp, and actor
  findings. All were accepted.
- Fresh Terra job `job-187` implemented the adjudicated corrections. The host gate
  found two stale test-contract mismatches that the orchestrator corrected; focused
  tests and the complete repository gate then passed. No mechanical second review was
  run because the bounded review reproduced no critical defect.
- Dogfooding the new workspace event query on this workspace exposed an oversized
  embedded git diffstat. Event copies now truncate receipt detail while metadata keeps
  the full rollup; focused coverage and the complete host gate pass after that fix.

## Friction

- Full Go verification could not start because this sandbox has no cached module
  dependencies and blocks the required `proxy.golang.org` downloads; the focused
  dependency-free workspace package test still ran.
- The CLI/e2e and gc packages remain host-gate work here: `go test . ./test
  ./internal/gc ./internal/workspace -count=1` attempted uncached dependency
  downloads and the sandbox blocked DNS/network access to `proxy.golang.org`.

## Verification

- `gofmt -l .` — passed (no output).
- `go test ./internal/workspace ./internal/events -count=1` — passed.
- `git diff --check` — passed.
- Host: `go vet ./... && go test ./... -count=1` — passed after the adjudicated
  corrections and test-contract updates.
- `go test . ./test ./internal/gc ./internal/workspace -count=1` — blocked by
  sandboxed network/DNS while downloading uncached Go modules; no workaround or
  dependency change attempted.
