# Unified job/run addressing

Status: later · Priority: P2 · Origin: run-selector piece of the "command grammar" remainder, promoted with field evidence 2026-07-08 · Depends: — · Workspace: —

## Goal

Every verb accepts the same address: a job id (`job-110`) or a run name
(`totp-2fa-delta-review`), positional. A run name resolves to its jobs (most recent when
one is needed).

## Context & design

- Observed friction: `status` takes a positional job id while `tail` takes `--run <name>`
  — the orchestrator maintained a mental job↔run map all session (job-108 ↔ totp-2fa,
  job-110 ↔ the delta review, ...). Two addressing schemes for one object graph.
- Design: a single resolver (`internal/`): exact job id → that job; otherwise treat as run
  label → its job set. Verbs that need exactly one job (`status`, `result`, `resume`,
  `wait`) take the newest and say so (`resolved 'pwa-review' → job-113`); verbs that
  operate on sets (`tail`, `events`, `ls`) take them all. Ambiguity (a run literally named
  like a job id) → error, never guess.
- Subsumes only the run-selector part of the "command grammar + self-describing JSON"
  remainder; the JSON-envelope and help-examples parts stay in that remainder.

## Constraints

- Backward compatible: existing `--run` flags keep working; positional forms are additive.
- Resolution messages go to stderr so stdout stays scriptable.

## Blockers

None.

## Log
