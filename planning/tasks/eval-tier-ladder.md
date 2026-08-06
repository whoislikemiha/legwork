# Eval tier ladder: sonnet/opus runs + deltas vs haiku

Status: next · Priority: P2 · Origin: 2026-08-06 split from orchestration-eval at its close · Depends: — · Workspace: —

## Goal

Run the full scenario suite (15 scenarios, 5 reps) on sonnet and opus and report
deltas vs the haiku baselines in `eval/FINDINGS.md`. The point is attribution:
whatever haiku fails and sonnet passes is interface debt the harness/docs paper
over at higher tiers; whatever all tiers fail is a product gap. Invocation/token
deltas also calibrate what supervision costs at each tier.

## Design

Nothing to build — `go build -o /tmp/legwork-eval ./eval` then
`/tmp/legwork-eval -models sonnet,opus -reps 5 -baseline <haiku run>`.
Budget estimate before running: haiku full-suite 5-rep is ~$8; expect 3–10× per
higher tier. Get an explicit go-ahead for the spend.

## Constraints

- Compare against a same-commit haiku run (the tool changes in parallel with
  use; cross-build comparisons are noise — see FINDINGS' versioning note).
- Weak-tier deltas remain the optimization target; strong-tier results are the
  control, not the goal.
