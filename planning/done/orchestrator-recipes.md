# Orchestrator recipes + doc consistency

Status: next · Priority: P1 (promoted from P2/later, field-notes 2026-07-08) · Origin: AUDIT E2, F1–F3; field-notes 2026-07-08 · Depends: ws-review-verb, rules-verb (reference the verbs, don't duplicate) · Workspace: —

## Goal

Fold the orchestration patterns the corpus already proves into named, documented
recipes in the guide, and close the small doc/adoption gaps around notes. Recipes
are the highest-value part of the skill (DESIGN §12) and several battle-tested ones
live only in run notes today.

**Why promoted:** the 2026-07-08 handover analysis showed a top-tier orchestrator
(Opus 4.8-high) spending ~70% of its reasoning re-deriving exactly this material —
correctly, but at prices only frontier models can pay. The docs teach the verbs and
the single-job loop; they don't teach the campaign. This task is the cheapest
capability uplift on the board: it moves strategy from per-run model reasoning into
the guide, once. It is the difference between "usable by Opus" and "usable by the
models orchestration will actually run on."

## Context & design

Recipes to add (each grounded in the data):

- **NEW — The campaign shape (multi-task wave).** The top-level recipe the others
  slot into: preflight (`doctor` per agent+model) → read the task set and sketch
  the conflict graph (which tasks touch the same files/packages) → one workspace
  per task, implement in parallel → review each (see `ws review`) → verify outside
  the sandbox → **land serially, most-isolated first**, resolving conflicts at
  merge time as orchestrator work → move task files / update the board → cleanup
  (`close`, `gc`). Evidence: 2026-07-08 `next-roadmap` wave — 7 tasks, 4 known
  file-overlap pairs, parallel implement + serial land derived from scratch; it
  belongs in the guide as one paragraph and a checklist.
- **NEW — Append-prompt norms + one worked example.** What belongs where: the
  *prompt* is the task pointer; the *task file* carries scope/design/constraints;
  `--append-prompt` carries run-specific policy only (verification reality of the
  sandbox, doc conventions, repo invariants). Include one known-good AP (~10
  lines) and the negative rule with teeth: run `legwork rules` first — if your AP
  restates anything the contract already says (commit policy, status states,
  blocked reporting), delete those lines. Evidence: the 2026-07-08 AP restated
  contract territory minutes after the orchestrator acknowledged the
  no-paraphrase rule; the rule is unfollowable without the verb + a norm.
- **NEW — Small preflight facts** (one line each, huge derivation cost when
  missing): omit `--model` to use the agent's default and let doctor's probe
  confirm it; `ws new` calls are safe to issue back-to-back (verify & state the
  actual concurrency contract while writing this); use `artifact save` for the
  ws↔task map instead of a hand-built table.
- **F1 Competition shape.** Dispatch competing implementations, pick the winner,
  discard the loser (optionally grafting one fix). Evidence: `presentation` run —
  `job-15` (opus/`ws-6`) vs `job-16` (codex/`ws-7`); orchestrator chose opus,
  discarded codex except one grafted fix.
- **F2 Design-only pipeline.** design doc → adversarial *design* review → revise,
  no code. Evidence: `p1-bank-sync` — `job-31`→`job-35` (`needs-changes`)→`job-36`.
- **F3 Reviewer-diff handoff & poisoned-context restart.** Both work but live only
  as prose: seed a fresh reviewer with `legwork diff <ws>` (don't make it
  rediscover the change); on poisoned context, `cancel` + fresh job re-seeded from
  artifacts, never `resume "keep going"`.
- **E2 Phase-boundary note discipline.** Notes are unevenly adopted — self-dev runs
  (`48 notes / 33 jobs`) read like an operator journal; p-runs (`8 notes / 46
  jobs`) force preview reconstruction. Document a "note at each phase boundary"
  recipe (created / plan approved / dispatched / review verdict / landed) so
  pipeline narratives are legible.

## Constraints

- Docs travel in threes (AGENTS.md): the guide is canonical — update
  `internal/guide/guide.md` first, then reconcile `skills/legwork/SKILL.md` and
  `README.md`.
- Recipes are documentation, not verbs (DESIGN: dumb tool, smart orchestrator). The
  one exception being promoted to a verb is review — see
  [ws-review-verb.md](ws-review-verb.md); reference it, don't duplicate it.
- Injected worker rules stay tool-owned; recipes are orchestrator-facing.
- Write for the *smaller* orchestrator: every recipe should be executable without
  re-derivation — concrete commands, one decision rule per step, no "use judgment"
  where a default exists.

## Blockers

Land after wave 1 (all seven wave-1 workspaces touch guide/SKILL/README — writing
recipes concurrently guarantees three-way doc conflicts) and after `rules-verb`
(the AP norm references it).

## Log

- 2026-07-08: Wrote the `## Recipes` section in `internal/guide/guide.md` (canonical):
  campaign shape (preflight → conflict graph → parallel implement → `ws review` →
  orchestrator-side verify → serial land most-isolated-first with post-merge suite
  re-run → board update → gc), append-prompt norms with a known-good worked example
  and the `legwork rules`-first negative rule, preflight facts (model default via
  probe, `ws new` back-to-back safety, auto per-job codex Go caches, ws↔task map via
  `runs`+artifact), and the F1 competition / F2 design-only / F3 reviewer-seeding +
  poisoned-context / E2 phase-boundary-note recipes. Condensed the same into
  `skills/legwork/SKILL.md` and added a one-line mention to `README.md`.
- Current-state reconciliation applied: referenced `ws review`/`result`/`close
  --merge-into`/`approve`/`rules`/`version`/`--append-prompt-file` as verbs (no flag
  restatement); did NOT document the retired `GOCACHE=/tmp` workaround (codex
  workspace-write turns get per-job writable caches automatically).
- Verified `ws new` concurrency from source: `newID` → `job.AllocID` takes an
  exclusive `flock` (`LockAlloc` on `.alloc.lock`), so concurrent/back-to-back
  `ws new` is safe — no duplicate-ID or lost-update risk. Documented as fact, not
  speculation.
- Spot-checked every documented command against `--help` and ran the fake-agent flow
  end-to-end (`ws new` → `run --agent fake` → `ls`/`runs` → `ws review --agent fake`;
  `doctor --agent fake`; `--append-prompt-file -`; `result`; `rules`) — all pass,
  zero API spend. `gofmt`/`go vet` clean; full suite green except the known
  pre-existing `TestCodexPassthroughs` teardown race (passed on isolated retry with
  no code change).

## Verdict

Implemented by opus 4.8 high (job-141, $2.33), cross-reviewed by codex gpt-5.5 (job-142):
**SHIP, zero findings**. Suite green on main. Landed 2026-07-08 via close --merge-into main.
Dispatch itself dogfooded the norms it documents: task prompt was a pointer, policy arrived via
--append-prompt-file, nothing restated the contract.
