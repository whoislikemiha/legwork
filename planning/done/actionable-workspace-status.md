# Actionable workspace and job status

Status: landed 2026-08-06 · Priority: P1 · Origin: 2026-07-10 orchestration dogfood · Depends: quality-receipts, external-verification-receipts · Workspace: —

## Goal

One command should answer: what happened in this workspace, what still needs
attention, and what is the next safe action? Orchestrators should not join job status,
events, diffs, review prose, verification notes, and git state by hand.

## Desired experience

```bash
legwork ws status ws-68
legwork ws status ws-68 --json
```

The rollup includes:

- workspace/base/branch identity and open/closed disposition;
- active implementation or review job and lock state;
- dirty/untracked/diff summary and latest checkpoint;
- latest structured review verdict and what diff it covered;
- latest external verification receipt;
- final commit, ahead/behind/merged facts, and close readiness;
- deterministic `attention` and `next_actions` codes.

Examples of next actions are `wait`, `answer`, `verify`, `review`, `fix-findings`,
`commit`, `merge`, `resolve-conflict`, and `close`. Each action includes the reason
and a copyable CLI command where it is safe to do so.

Job `status` uses the same attention/action vocabulary for job-local states. In
particular, `blocked.kind=verify` points to verification rather than generic resume.

## Truth and safety rules

- Read persisted receipts and current git facts; never infer `SHIP` from prose.
- Status rendering is read-only and never advances state, runs a command, or closes a
  workspace.
- Unknown or version-skewed facts are shown as unknown, not guessed.
- Review, verification, merge, and close remain separate gates.

## Acceptance criteria

- New, active, needs-input, blocked-verify, FIX, SHIP, dirty, committed, merged,
  conflicted, and closed workspaces have stable fixtures.
- Human output is compact and decision-oriented; JSON exposes the underlying facts,
  attention codes, and next actions without embedding full task/result bodies.
- A workspace with incomplete receipts remains useful but explicitly says which facts
  are unavailable.
- No status invocation mutates metadata, refs, worktrees, or job state.

## Non-goals

- A pipeline engine, auto-merge, policy daemon, or replacement for `diff`/`events`.

## Log

2026-08-06 — first slice landed, driven by the orchestration eval's residual F1
(instructed "confirm it landed" routes to git even with the close receipt in
hand): `legwork ws status <ws> [--json]` ships facts (identity, jobs, diff
stat, latest review/verification receipts, final commit, close receipt) plus
deterministic `attention` and `next_actions` (wait/answer/verify/approve/
inspect/dispatch/review/fix-findings/commit/close/none) with copyable commands.
Read-only, receipts-only, per this spec. Still open from the spec: job
`status` sharing the attention/action vocabulary; ahead/behind git facts;
fixtures for FIX/SHIP/conflicted states beyond the current e2e coverage.

2026-08-06 (second pass) — remaining spec items landed: job `status` ends with
the same `attention`/`next_actions` vocabulary (JSON additive on `metaOut`;
`blocked.kind=verify` points at `legwork verify` with the requested command
inlined, never a generic resume); `ws status` reports commits-ahead-of-base
(unknown shown as `commits_ahead_unknown` with the reason, never guessed);
e2e fixtures now cover fresh/needs-input/unreviewed/FIX/SHIP/committed/closed
plus blocked-verify and done-workspace-job on the job side.

## Verdict

Landed. The measured effect (orchestration-eval FINDINGS, F1): with `ws status`
as the confirmation surface, verify-gate went 3/5 → 5/5 and workspace-flow
1/5 → 3/5 at the weak tier, with the verb adopted in 9/10 runs from the skill
text alone. Deliberate deviations from the original spec, accepted at close:
ahead/behind is reported as ahead-of-base only (behind/`ws refresh` territory
is its own roadmap item); a conflicted-state fixture is not modeled because
conflicts surface at `close --merge-into` time with their own exit path, not
as a workspace state; `next_actions` names the next *gate*, one at a time,
rather than a full plan — review/verification/merge/close remain separate
gates per the truth-and-safety rules.
