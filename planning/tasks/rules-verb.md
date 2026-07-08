# `legwork rules` — make the injected worker contract inspectable

Status: next · Priority: P1 · Origin: field-notes 2026-07-08 (orchestration handover analysis) · Depends: sandbox-anti-workaround, structured-blocked-provision (land first — they change the contract text) · Workspace: —

## Goal

`legwork rules` prints the injected worker contract verbatim, so an orchestrator can
*read* the thing it is told never to paraphrase. Today the contract text exists only
in `internal/rules/rules.go` — Go source. The 2026-07-08 transcript shows a
top-tier orchestrator reading that file to comply with the no-paraphrase rule;
smaller orchestrators won't, and will guess, and guessing produces paraphrase —
exactly the competing-contract failure mode the rule exists to prevent. A rule you
cannot see is a rule you cannot follow.

## Context & design

- `legwork rules` prints the exact text `run` would inject for the default agent;
  flags mirror dispatch where the text varies (`--agent`, `--read-only`,
  `--workspace`-shaped variants if the contract differs). Placeholders that are
  filled at dispatch time render as visible `<placeholders>`, not resolved values.
- `--json`: `{"version": N, "text": "..."}` — the rules/parser version travels with
  the text (AGENTS.md: rules and parser version together).
- Docs (threes): guide + skill get one line in the prompt-scoping section — "run
  `legwork rules` to see what the contract already covers before writing an
  `--append-prompt`". That converts the no-paraphrase rule from an act of faith
  into a checkable diff.
- Future idea, NOT in scope: a dispatch-time lint that warns when `--append-prompt`
  text overlaps contract phrases ("do not commit", "state: done"). Record here so
  it isn't lost; build only if AP-paraphrase keeps recurring after the verb + doc
  norms land.

## Constraints

- Read-only, no state dir required (must work before any job exists — it's a
  learning surface). Stable exit codes, `--json`, never prompts.
- The printed text must be the literal injected text (same constant/template), not
  a copy — zero drift by construction.

## Blockers

Land after the wave-1 contract changes (anti-workaround rule, structured blocked)
so the verb ships printing the new contract, not a text that changes a day later.

## Log
