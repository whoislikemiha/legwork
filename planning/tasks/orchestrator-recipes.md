# Orchestrator recipes + doc consistency

Status: later · Priority: P2 · Origin: AUDIT E2, F1–F3 · Depends: — · Workspace: —

## Goal

Fold the orchestration patterns the corpus already proves into named, documented recipes in the
guide, and close the small doc/adoption gaps around notes. Recipes are the highest-value part of
the skill (DESIGN §12) and several battle-tested ones live only in run notes today.

## Context & design

Recipes to add (each grounded in the data):

- **F1 Competition shape.** Dispatch competing implementations, pick the winner, discard the
  loser (optionally grafting one fix). Evidence: `presentation` run — `job-15` (opus/`ws-6`) vs
  `job-16` (codex/`ws-7`); orchestrator chose opus, discarded codex except one grafted fix.
- **F2 Design-only pipeline.** design doc → adversarial *design* review → revise, no code.
  Evidence: `p1-bank-sync` — `job-31`→`job-35` (`needs-changes`)→`job-36`.
- **F3 Reviewer-diff handoff & poisoned-context restart.** Both work but live only as prose:
  seed a fresh reviewer with `legwork diff <ws>` (don't make it rediscover the change); on
  poisoned context, `cancel` + fresh job re-seeded from artifacts, never `resume "keep going"`.
- **E2 Phase-boundary note discipline.** Notes are unevenly adopted — self-dev runs (`48 notes /
  33 jobs`) read like an operator journal; p-runs (`8 notes / 46 jobs`) force preview
  reconstruction. Document a "note at each phase boundary" recipe (created / plan approved /
  dispatched / review verdict / landed) so pipeline narratives are legible.

## Constraints

- Docs travel in threes (AGENTS.md): the guide is canonical — update `internal/guide/guide.md`
  first, then reconcile `skills/legwork/SKILL.md` and `README.md`.
- Recipes are documentation, not verbs (DESIGN: dumb tool, smart orchestrator). The one exception
  being promoted to a verb is review — see [ws-review-verb.md](ws-review-verb.md); reference it,
  don't duplicate it.
- Injected worker rules stay tool-owned; recipes are orchestrator-facing.

## Blockers

None. Pure docs; can land anytime. Best written after `ws review` lands so the review recipe
points at the verb.

## Log
