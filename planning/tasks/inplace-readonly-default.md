# In-place jobs: read-only by default

Status: next · Priority: P0 · Origin: 2026-08-06 DESIGN-vs-code audit (finding 2) · Depends: — · Workspace: —

## Goal

Restore the inverted safety default. DESIGN §2 mandates that `--dir` (in-place)
jobs run in a **read-only sandbox by default**, with mutation as an explicit
opt-in (`--allow-write`; skill: "you almost never want this — want writes? use a
workspace"). The code shipped the opposite polarity: `--dir` is writable unless
`--read-only`. The design's reasoning stands and is prompt-injection-shaped:
in-place/scratch jobs are the research path, research reads hostile content.

## Shape of the work

- `--dir` without other flags dispatches read-only (claude plan mode / codex
  read-only sandbox — the existing `--read-only` machinery, now the default).
- New `--allow-write` flag flips it, valid only with `--dir`. Rejected at dispatch
  for agents with `readonly`-incapable harnesses? No — inverse: read-only-default
  on an agent with no harness read-only (hermes) must **fail loud at dispatch**
  telling the caller to pass `--allow-write` or use a workspace, never silently
  run writable (the §3 no-silent-degradation rule).
- `--read-only` stays as an explicit no-op-on-`--dir` / meaningful-on-workspace
  flag; passing both `--read-only` and `--allow-write` is a usage error.
- Scratch jobs (no target) keep writing to their scratch dir — the blast radius
  is already tool-owned; only `--dir` changes polarity.
- This is a **breaking behavior change** for existing orchestrator habits: note it
  in the changelog/help prominently; the eval harness and any scenario using
  `--dir` mutation needs `--allow-write` added.

## Acceptance criteria

- `run --dir` on claude/codex dispatches with the harness read-only mode; e2e
  proves a write attempt fails inside the sandbox.
- `run --dir` on hermes (readonly: none) errors at dispatch with an actionable
  message; `--allow-write` unblocks it.
- `--allow-write` + `--read-only` together → usage error, stable exit code.
- Injected rules text for read-only in-place jobs matches the actual mode.
- eval scenarios and docs updated; grep the repo for `--dir` usage to catch
  callers relying on the old default.

## Non-goals

- `--allow-push` (belongs to the hook policy layer task).
- Changing workspace-job defaults (already correct).

## Log
