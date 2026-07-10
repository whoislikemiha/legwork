# Quality receipts / accountability shape

Status: next · Priority: P1 · Origin: AUDIT C1–C4 (+ prior ROADMAP "Quality receipts" lane) · Depends: — · Workspace: —

## Goal

Make the review/close loop as queryable as the commit loop. Right now `commit` is a first-class
structured event and everything else around landing is prose or lifecycle-only, forcing log
archaeology. Sub-items (each independently shippable):

1. **Persist last-turn state in `meta.json`** (cheapest, highest-value).
2. **First-class `close` event** in the run/job stream.
3. **Dedupe cross-label commit events.**
4. **Backfill version-skewed workspace metadata + stamp a schema version.**
5. **Structured review verdicts** (pairs with the `ws review` verb).

## Context & design

- **C1 last-turn state.** Every terminal job is `closed`/`done` in `meta.state`;
  `failed`/`blocked`/`interrupted` survive only in `events.jsonl` (`job-48` failed on usage limit
  → now `closed`). After close, `status` can't answer "what did the worker actually say?". Add a
  `last_turn_state` (and reason) to `meta.json` written at finish, preserved through close.
- **C2 first-class close.** 32 `commit` events carry workspace/branch/oid/summary; closure is
  reconstructed from `ws/meta.json` + prose notes. Emit a structured `close` event
  (disposition, reason, merged_into, retention, actor) into the run + job logs so
  close/merged/discard is aggregatable, not text-grepped.
- **C3 dedupe commits.** The same oid appears in multiple run logs (`ws-41`×3 across
  `p3-assistant-hardening{,-review,-rereview}`, `ws-40`×2). Decide the ownership rule (a workspace
  commit belongs to one run, or events carry an explicit dedupe key) so `runs` isn't ambiguous.
- **C4 metadata backfill.** Only 21/41 closed workspaces have `closed_at`; 18 have
  `reason`/`merged_into`/`final_commit`; early `ws-1..16` have only state/disposition/checkpoints.
  One-shot backfill (best-effort from event logs) + a `meta_version` stamp so future skew is
  detectable.
- **5 structured verdicts.** Review results are text (`FIX`/`SHIP`/`ACCEPT` grep). Normalize to a
  field so status/`serve` can show the latest verdict + reviewer + findings count without digging.

## Constraints

- **Event schema is a public interface** (AGENTS.md): new `close` event type is fine; new
  `meta.json` fields need care and a `meta_version` bump; changing existing event fields needs a
  `v` bump + migration story.
- Backfill must be idempotent and read-only w.r.t. git (reconstruct from `events.jsonl`, never
  re-derive by touching repo history). Respect the gc/close blast-radius rule.
- Keep the `close` tripwire: unreviewed changes still require an explicit disposition.

## Blockers

None hard. Ship sub-item 1 (last-turn state) first — small and immediately useful. Coordinate the
verdict shape with [ws-review-verb.md](ws-review-verb.md).

## Log
