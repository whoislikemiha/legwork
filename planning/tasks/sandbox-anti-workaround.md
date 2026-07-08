# Sandbox anti-workaround rule

Status: next · Priority: P0 · Origin: AUDIT A1 (+ prior ROADMAP Next #1) · Depends: — · Workspace: —

## Goal

Add one line to the **injected worker contract**: never modify the test harness, build config,
or dependencies to accommodate a sandbox limitation — report `blocked` with the exact failing
command instead. Highest-leverage fix in the review: it kills the silent-workaround failure mode
where a worker ships a *worse product* to make a command pass.

## Context & design

- The failure mode is not the block, it is the workaround. Evidence: `ws-21`/`job-37`
  (p2-polish) had to revert its own env workarounds before landing; the field notes recorded
  workers deleting Google fonts + rewriting `next build` to pass offline, and monkeypatching
  `fastapi.testclient` globally when pytest hung. After an explicit anti-workaround line was
  added to `--append-prompt` in the 2026-07-07 run, workers began self-reverting — the rule
  works; it belongs in the injected contract, not in every orchestrator's prompt.
- Injected rules are tool-owned and live in `internal/rules`. This is a rules-text change plus
  its worker-facing wording per adapter (claude / codex) if they diverge.
- Wording should be concrete and bounded: it must forbid *editing harness/build/deps to dodge a
  sandbox limit*, while NOT forbidding legitimate dependency changes the task actually calls for.
  Pair it with the escalation target from the structured-blocked task (report the failing command
  as `blocked`, ideally `needs-provision`).

## Constraints

- Injected rules are tool-owned (AGENTS.md hard rule): orchestrators add via `--append-prompt`,
  never by paraphrasing the contract. This task moves the proven line *into* the contract.
- The status-block contract and its parser version travel together — if the rule text is bundled
  with the status-block rules, keep `internal/rules` and `internal/adapter`'s parser consistent.
- Do not weaken "missing status block → blocked, never done".
- Docs travel in threes: update `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and
  `README.md` if the injected contract's documented summary changes.

## Blockers

None. Ships independently; strongest when combined with structured-blocked-provision (gives the
worker a clean place to escalate instead of working around).

## Log
