# Structured blocked reasons + needs-provision

Status: next · Priority: P0/P1 · Origin: AUDIT A3 (+ prior ROADMAP Next #3, field-notes #3) · Depends: — · Workspace: —

## Goal

Split the overloaded `blocked` state into a structured, scriptable reason, and add a
`needs-provision` escalation the orchestrator can approve. Today `blocked` hides three cases that
route completely differently and the orchestrator must parse prose `result` to tell them apart.

## Context & design

Three kinds seen in the corpus, all currently flattened to `blocked`:

- **provision** — a command the sandbox cannot run because of no-network/no-write. `job-29`:
  "`uv add slowapi` cannot reach PyPI". The orchestrator did `uv add` / `uv sync` / `npm install`
  by hand three times in one run.
- **verify** (most common) — work is done but the suite can't run in the sandbox. `job-16`
  (done, commit blocked), `job-28`/`job-38`/`job-44` (done, `TestClient`/tmp block). This is
  really "done, unverified", not "no progress".
- **decision** — a genuine judgment call the worker should not make alone.

Design:

- Structured field on the blocked finish: `blocked: {kind: provision|verify|decision, detail}`
  (event schema + `status --json`). For `provision`, the worker declares the exact command, e.g.
  `{"kind":"provision","command":"uv add slowapi"}`.
- `needs-provision` flow: legwork surfaces it like `needs-input`; the orchestrator approves;
  legwork runs the command **outside the sandbox** in the worktree; the turn resumes. Fails
  closed — no auto-run without approval.
- The `verify` kind pairs with the writable-tmpdir task (fewer false verifies) and the
  `worktree.toml` verify hook (orchestrator runs the suite outside the sandbox and attaches
  "308 passed" to status).

## Constraints

- **Event schema is a public interface**: a new `needs-provision` event type is fine; adding
  fields to existing `blocked`/`finished` needs a `v` bump + migration note (AGENTS.md).
- The status-block contract and its parser version travel together — a new structured status
  shape means `internal/rules` and `internal/adapter` change in lockstep.
- Keep the safety direction: missing/unparseable block → `blocked`, never `done`.
- Approval gates fail closed (DESIGN §5); no timeout-proceed on `needs-provision`.

## Blockers

Sequencing: land the `blocked.kind` classification first (read-side, cheap), then the
`needs-provision` run-outside-sandbox flow (mutating, needs approval wiring). Overlaps the
`worktree.toml` verify-hook idea — coordinate so "verify" blocks shrink from both sides.

## Log
