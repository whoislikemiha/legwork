# Honest cost accounting + per-run rollup

Status: later · Priority: P2 · Origin: orchestrator field feedback 2026-07-08 · Depends: — · Workspace: —

## Goal

"What did this wave cost?" must be answerable. Per-job costs are honest about their
accounting basis, and `legwork ls`/`status` can roll up totals per run.

## Context & design

- Observed: an Opus review job reports `$1.23`; every codex job reports `$0.00` because
  subscription usage has no per-call price. `$0.00` reads as "free", which is a lie of
  presentation — a wave (implement + review + fix + delta-review) currently sums to a
  meaningless number.
- Design: cost becomes `{amount?, basis: metered|subscription|unknown}`; human output
  shows `$1.23` / `n/a (subscription)` instead of a fake zero. Tokens are always known —
  report tokens_in/out per job regardless of basis, and let the rollup show both: metered
  $ total + token totals per agent/model. `legwork ls --run X --totals` (or a `costs`
  verb) prints the per-run rollup.
- Overlaps [quality-receipts.md](quality-receipts.md) (accountability shape) — if that
  task restructures job metadata, land this as part of it rather than separately.

## Constraints

- Never invent prices for subscription usage; no price tables to maintain unless an agent
  reports its own.

## Blockers

None.

## Log
