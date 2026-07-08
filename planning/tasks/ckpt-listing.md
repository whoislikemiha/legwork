# Checkpoint discoverability — `ws ckpts`

Status: later · Priority: P2 · Origin: orchestrator field feedback 2026-07-08 · Depends: — · Workspace: —

## Goal

`legwork ws ckpts <ws>` lists a workspace's checkpoint refs — one line per checkpoint:
ref, timestamp, creating job + turn, subject/short diffstat. Makes the delta-review
pattern first-class instead of folklore.

## Context & design

- Checkpoint refs (`refs/legwork/<ws>/ckpt-N`) are quietly one of legwork's best
  features: the 2026-07-08 TOTP delta security review was literally "verify
  ckpt-1..ckpt-3" — reviewing only what the fix turn changed. But nothing lists them;
  the orchestrator only knew they existed from `tail` output scrolling past. Ref names
  are undocumented API surface being used from memory.
- Include the ckpt list (or latest ckpt) in `ws ls`/`status --json` output so scripted
  orchestrators can construct delta ranges without shelling to `git for-each-ref`.
- Pairs with [ws-review-verb.md](ws-review-verb.md): a first-class reviewer wants
  `--since ckpt-N` and this listing is where those names become discoverable. Document
  the delta-review recipe in [orchestrator-recipes.md](orchestrator-recipes.md).

## Constraints

- Read-side only; `--json` supported; stable ordering (ckpt number).

## Blockers

None.

## Log
