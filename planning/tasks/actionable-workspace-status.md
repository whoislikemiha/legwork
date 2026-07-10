# Actionable workspace and job status

Status: next · Priority: P1 · Origin: 2026-07-10 orchestration dogfood · Depends: quality-receipts, external-verification-receipts · Workspace: —

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
