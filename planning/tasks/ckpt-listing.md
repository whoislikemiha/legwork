# Checkpoint discoverability — `ws ckpts`

Status: later · Priority: P2 · Origin: 2026-07-08 delta-review dogfood · Depends: — · Workspace: —

## Goal

Make workspace checkpoints discoverable through Legwork so orchestrators can review a
specific implementation or fix delta without remembering hidden ref names or running
`git for-each-ref` themselves.

## Desired experience

```bash
legwork ws ckpts ws-49
legwork ws ckpts ws-49 --json
```

Each checkpoint reports, when known:

- stable checkpoint ID/ref and numeric order;
- creation time;
- creating job and turn;
- commit/tree object;
- short diffstat from the previous checkpoint;
- whether it is the latest reviewed checkpoint.

Legacy refs with missing metadata remain listable and mark unknown fields explicitly.
Ordering is by checkpoint number, not filesystem or lexical accident. The latest
checkpoint may also appear in workspace status, but the full list belongs here.

This complements the landed [`ws review`](../done/ws-review-verb.md) flow. Support for
reviewing `--since <checkpoint>` or `diff --since-last-review` should consume these
stable IDs but remains a separate review-cursor capability.

## Acceptance criteria

- Empty, open, closed-preserved, legacy, and missing-workspace cases have stable human
  and JSON output.
- Checkpoint IDs round-trip into existing `diff --at` behavior.
- Listing is read-only and never creates, deletes, or advances checkpoints/review
  cursors.
- The guide documents the delta-review recipe using the command rather than raw refs.

## Non-goals

- A checkpoint browser UI, semantic diff summary, or automatic review approval.

## Log
