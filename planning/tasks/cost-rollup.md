# Honest cost accounting and run rollups

Status: later · Priority: P2 · Origin: 2026-07-08 orchestration dogfood · Depends: — · Workspace: —

## Goal

Answer “what did this run consume?” without presenting subscription-backed Codex jobs
as literally free or mixing incomparable accounting into one misleading dollar total.

## Product contract

Every job reports an accounting basis alongside its existing usage:

- `metered` — the provider reported a monetary amount;
- `subscription` — tokens/context are known but no per-turn price exists;
- `unknown` — neither a trustworthy amount nor subscription basis is available.

Human output renders these honestly: `$1.23`, `subscription`, or `unknown`, never a
fabricated estimate. Existing monetary fields remain backward compatible; additive
basis fields explain how to interpret zero or missing values.

Run-level totals belong in the run overview, for example `legwork runs --totals`:

- metered currency total only across metered jobs;
- token/context totals grouped by agent and model;
- counts of subscription and unknown jobs;
- optional phase/job breakdown without duplicating one workspace commit across labels.

## Acceptance criteria

- Mixed Claude-metered, Codex-subscription, fake, and missing-usage runs render without
  converting subscription use to `$0.00` spend.
- Human and JSON totals state their basis and grouping.
- Resumed jobs aggregate turns once; repeated run labels do not duplicate a job.
- Legacy metadata remains readable and is classified conservatively.
- No static price table or guessed provider cost is introduced.

## Boundaries

- [Quality receipts](quality-receipts.md) owns lifecycle/identity deduplication; cost
  rollup consumes the stable job/workspace identity when available.
- This task does not add budgets, billing enforcement, or quota prediction.

## Log
