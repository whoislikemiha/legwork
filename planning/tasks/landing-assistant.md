# Landing assistant — `close --merge-into` (minimal) / `land` workflow (full)

Status: next · Priority: P1 · Origin: pre-system roadmap remainder, promoted with field evidence 2026-07-08 · Depends: — · Workspace: —

## Goal

Make landing a workspace a single safe operation instead of a hand-run `git merge` +
`close --merged` pair. Minimal form: `legwork close <ws> --merge-into <ref>` performs the
`--no-ff` merge itself (from anywhere) and closes. Full form (the original remainder): a
`land` workflow that prepares the target branch, applies the workspace commits, detects
conflicts, runs configured validation (`worktree.toml` verify hook), and prints a
checklist — explicitly not a `pr` verb.

## Context & design

- **Field evidence (2026-07-08)**: the orchestrator ran `git merge legwork/ws-52` *inside
  the workspace tree* by accident — HEAD there IS the branch, so git reported "Already up
  to date" and succeeded at doing nothing. Caught only because the subsequent
  `close --merged` state was double-checked. An orchestrator that misses this moves task
  files and flips its project board for an unmerged branch: silent false-landed state.
  The existing non-ancestor guard on `close --merged` is exactly right — this task moves
  that safety one step earlier so the slip cannot happen.
- Minimal step is cheap and independent: resolve `<ref>`, refuse if the merge would run
  with the workspace branch checked out as HEAD (the observed slip), perform `--no-ff`
  merge with a generated-or-provided message (`-m`), then run the existing close path.
  Conflicts → abort the merge, report `blocked: verify`-style structured output, leave
  the repo clean.
- Full `land` adds: pre-merge fetch/base-drift check (overlaps `ws refresh`), optional
  validation hook run OUTSIDE any sandbox, checklist output. Ship minimal first.

## Constraints

- Never force, never rebase, never push — landing is local; pushing stays the operator's
  call. Conflict = clean abort + non-zero exit, never a half-merged tree.
- `--json` envelope; stable exit codes distinguishing merged / conflict / guard-refused.

## Blockers

None.

## Log
