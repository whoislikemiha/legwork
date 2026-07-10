# Event Schema

Every `events.jsonl` line carries `v`. Readers should ignore unknown event types and
unknown `fields` keys.

## v2

v2 adds structured blocked reasons without changing existing v1 fields:

- `finished.fields.blocked` may appear when `finished.fields.state == "blocked"`.
  Shape: `{"kind":"provision|verify|decision","detail":"...","command":"..."}`.
  `command` is meaningful for `kind:"provision"`.
- `needs-provision` is a new event type emitted before `finished` when a blocked
  turn can continue after explicit approval.
- `approve` is a new orchestrator event type for `legwork approve`.
- `closed.fields.outcome` records the last terminal worker state, compact reason,
  structured question or blocked reason when present, turn, and timestamp before
  the job lifecycle state advances to `closed`.
- `review-verdict` is emitted by a workspace review job after its current
  workspace receipt is written. `fields.review` contains the reviewer identity,
  checkpoint and diff digest, parsed `SHIP|FIX` verdict, finding counts, and
  completion time. Malformed or ambiguous reviewer output is retained with
  `parsed:false` and `parse_error`; it is never promoted to `SHIP`.

Migration story: v1 event logs remain readable as-is; absent `fields.blocked` means
the blocked reason is unknown. v1 consumers that ignore unknown fields/types continue
to work, while v2-aware consumers can route provision blocks to `legwork approve`.
