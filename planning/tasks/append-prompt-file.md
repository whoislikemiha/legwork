# `--append-prompt-file` — multi-line append-prompts without shell quoting

Status: next · Priority: P2 (small) · Origin: field-notes 2026-07-08 · Depends: — · Workspace: —

## Goal

`legwork run --append-prompt-file <path|->` reads the orchestrator additions from a
file (or stdin with `-`), so multi-line append-prompts stop riding on shell quoting.

## Context & design

- Observed 2026-07-08: the orchestrator assembled a ~40-line append-prompt as a
  shell variable inside a compound bash command. It survived, but the failure mode
  (literal `\n` sequences, mangled quotes, silently truncated text) is invisible —
  the degraded prompt just produces a worse worker with no error anywhere. Smaller
  orchestrator models hand-assembling prompts in shell will hit this.
- Mutually exclusive with `--append-prompt` (usage error if both). Applies to `run`
  and anything else that grows the flag later. Recorded in job meta identically —
  the file is read at dispatch; the job stores the text, not the path.
- Pairs naturally with run artifacts: save the AP once
  (`legwork artifact save --run L --name append-prompt.md`), dispatch each worker
  with the file. Mention that recipe in the docs (threes).

## Constraints

- UTF-8 text; reject binary. Empty file = usage error (an empty AP is always a
  mistake). `-` reads stdin.

## Blockers

None. Small; can ride along with any wave-2 workspace.

## Log
