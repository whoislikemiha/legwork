# Stable structured operation surface

Status: later · Priority: P1 · Origin: repeated JSON-shape discovery in orchestrators · Depends: unified-addressing, actionable-workspace-status · Workspace: —

## Goal

Let Hermes and other agent harnesses drive the core Legwork loop through documented,
versioned CLI operations without parsing human tables or sampling commands to discover
whether JSON is an array, object, selector, or error string.

## Product scope

Define a stable structured contract for the operations an orchestrator actually
composes:

- dispatch and continuation;
- job/workspace status and wait;
- events and result;
- diff, review, verification, commit, and close;
- run notes and artifacts.

The contract includes:

- one selector grammar from [unified addressing](unified-addressing.md);
- versioned success and error envelopes with stable machine codes;
- explicit resolved IDs and next actions;
- schema/examples that can be inspected without reading Go source;
- bounded text fields, with full results/diffs available through dedicated verbs.

Human output remains optimized independently. CLI-over-ssh is canonical; a future
Hermes/plugin wrapper may translate these operations but does not own the contract.

## Compatibility strategy

Before changing existing `--json` output, choose and document an additive opt-in
migration path. Existing scripts must not silently receive a new top-level shape. The
public event schema remains separately versioned and is not wrapped or rewritten by
this task.

## Acceptance criteria

- Every core operation has a documented JSON example, stable error code, and contract
  test/snapshot.
- A harness can discover the supported operation/schema version locally.
- Success, usage error, missing selector, attention state, conflict, and guard refusal
  are structurally distinguishable without parsing prose.
- Existing JSON consumers have an explicit compatibility period and migration guide.
- The surface works identically over local shell and ssh.

## Non-goals

- MCP, a daemon, database, network API, orchestration tree, or generated SDK suite.

## Log
