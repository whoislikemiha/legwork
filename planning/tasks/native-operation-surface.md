# Native-feel structured operation surface

Status: later · Priority: P1 · Origin: repeated orchestrator-side JSON parsing and command-shape discovery · Depends: unified-addressing · Workspace: —

## Goal

Let agent harnesses drive Legwork through stable structured operations without parsing human tables, sampling unknown JSON shapes, or memorizing inconsistent selectors.

## Context & design

- Define and document stable JSON envelopes for the core loop: dispatch, status, wait, events/result, workspace status/diff/review/commit/close, artifact, and verification.
- Use one selector grammar for job/run/workspace where meaningful; self-describing error envelopes include code, message, selector, and next actions.
- Provide machine-readable command/schema discovery from the CLI so wrappers can generate operations without MCP.
- Keep human CLI output optimized for humans; structured operations are the agent control plane.
- A thin Hermes/plugin adapter may wrap the CLI later, but the CLI-over-ssh contract remains canonical.

## Constraints

- Do not add MCP, a daemon, a database, or orchestration-tree semantics (ROADMAP rejected ideas).
- Existing event schema remains public and versioned independently.
- Additive migration path for current `--json` consumers; document envelope versions.
- Contract tests/snapshots for every operation and stable exit code.

## Blockers

Resolve unified addressing and decide the JSON envelope migration strategy.

## Log
