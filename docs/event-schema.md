# Event Schema

Every `events.jsonl` line carries `v`. Readers should ignore unknown event types and
unknown `fields` keys.

Event logs are scoped. Existing `legwork events <selector>` reads a job or run log;
`legwork events ws-N --workspace` explicitly reads the workspace log. Every scope
has its own monotonically increasing `seq`, so `--since` is a cursor within the
selected log, not across scopes.

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
- `verification` records an orchestrator-run host check. Its `fields.receipt_id`
  identifies the attempt and `fields.verification` contains the compact receipt
  (job/workspace, argv, cwd, pass/fail/timeout, exit code when known, duration,
  bounded output, actor, timestamps, and any history warning). The same receipt ID
  is appended to the qualifying job and workspace logs; metadata retains only the
  latest rollup. It never rewrites `finished.fields.state` or its blocked reason.
- `verification-refused` records a command that could not start. It has no receipt
  and therefore cannot be mistaken for a pass or failure attempt.

Migration story: v1 event logs remain readable as-is; absent `fields.blocked` means
the blocked reason is unknown. v1 consumers that ignore unknown fields/types continue
to work, while v2-aware consumers can route provision blocks to `legwork approve`.
Workspace metadata v1 remains readable and is upgraded to v2 on the next successful
write; metadata with a future schema version is refused without rewriting it.

## Workspace receipt events

Workspace logs record orchestrator-owned `commit` events and `workspace-close`.
Both carry `fields.workspace` and `fields.receipt_id`. Commit events also carry
`fields.branch`, `fields.oid`, and `fields.final_commit`. A `workspace-close` event
carries `fields.receipt`, whose receipt fields are `receipt_id`, `disposition`,
optional `reason` and `target`, `actor`, optional `closed_at`, optional retention and
supersession facts, and optional `final_commit`. `history_error`, when present on a
receipt, means the durable operation completed but one or more history appends could
not be recorded; consumers must not infer that the operation should be replayed.
