# Retire the guide — progressive disclosure via the binary

Status: next · Priority: P1 · Origin: PR #1 review, 2026-08-06 (guide grew into a 1500-line process spec) · Depends: — · Workspace: —

## Goal

Kill `legwork guide` as a preloaded 43KB manual. Agent-facing depth moves into the
binary itself — per-verb long `--help` plus a small set of embedded help topics — so
an orchestrator discovers detail on demand instead of front-loading a document. One
thin agent doc remains (SKILL.md: the loop, a routing table of "which command to ask
about what", the few cross-verb plays); README stays human-facing.

The property being preserved is DESIGN §12's real constraint: docs travel with the
binary over ssh. Cobra help text is embedded exactly like the guide was, and it has a
property the guide never had — it is always version-matched to the binary answering,
where a preloaded guide can describe a different build than the one installed.

## Desired experience

```bash
legwork ws review --help    # reviewer seeding, independence (agent/model), context bundle
legwork verify --help       # receipt bounds (64KiB, redacted), blocked.kind=verify handoff
legwork close --help        # the tripwire, dispositions
legwork help campaigns      # cross-verb play: wave shape, parallel implement, serial land
legwork help recovery       # poisoned context, reboot, stale-job sweep
```

A cold agent over ssh with no skill installed gets a correct happy path from
`legwork --help` alone (already the §12 contract) and can pull depth per verb as
needed. Nothing requires a doc that isn't inside the binary.

## Shape of the work

- Move each guide section onto the verb it describes as cobra long help. Content is
  distilled during the move, not pasted — per-verb help is a screen or two, never a
  manual chapter.
- Cross-verb plays (campaign shape, proportionality, poisoned-context recovery,
  append-prompt norms) become embedded help topics — a handful, ~100 lines total.
  These are the only content with no natural verb to carry it.
- SKILL.md shrinks to: the loop, the help-topic/verb routing table, the plays in one
  line each with "see `legwork help <topic>`" pointers. It stops shadowing the guide.
- Delete `internal/guide`; `legwork guide` either goes away or becomes a one-screen
  index of verbs + help topics (decide during implementation; a hard error on a verb
  agents have memorized is worse than a redirect).
- Rewrite the "docs travel in threes" convention (CLAUDE.md) and DESIGN §12 to the
  new model: self-describing binary + thin skill + human README.
- Add to DESIGN §13 rejected: process specifications in docs — machinery belongs in
  verbs, docs carry recipes. Rule of thumb: any documented procedure needing `jq`,
  `sha256sum`, `mktemp`, or a cross-stage shell variable is a feature request, not a
  recipe.

## Acceptance criteria

- Every durable claim in the current guide either lives in a verb's `--help`, a help
  topic, SKILL.md, or is deliberately dropped — accounted for, not silently lost.
- `legwork --help` and each verb's long help render as embedded text with no repo
  checkout (the ssh contract).
- SKILL.md is materially smaller than today and contains no procedure that a
  `--help` already carries.
- CLAUDE.md, DESIGN §12/§13, and README reflect the new doc model.
- e2e: help topics exist and print (cheap contract test, keeps them from rotting).

## Non-goals

- New verbs or behavior changes — this is a documentation-architecture task. The
  machinery gaps the old guide papered over (check receipts, freshness query,
  review-context assembly) are separate tasks.
- Generated man pages, web docs, or any doc surface outside the binary + SKILL.md +
  README.

## Log
