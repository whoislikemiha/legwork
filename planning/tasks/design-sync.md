# DESIGN.md sync — ratify reality, mark the debt

Status: next · Priority: P2 · Origin: 2026-08-06 DESIGN-vs-code audit · Depends: — (lands after the P0 posture tasks decide their direction) · Workspace: —

## Goal

DESIGN.md describes a tool that no longer exists in places — starting with its own
header ("design accepted, not yet implemented"). Sync it to the code: ratify the
deliberate drift, keep unbuilt items only where they're still intended (and mark
them as such), and let the posture deviations be fixed by their own tasks rather
than papered over in prose. The audit that seeded this is in the 2026-08-06
conversation; findings are restated below so the task is self-contained.

## Ratify (code is right, doc is stale)

- §3 verb table: add `verify` (+ `blocked.kind=verify` handoff), `wait`, `result`,
  `ack`, the `ws` family (`new/ls/commit/review/status`), `doctor`, `runs`, `tail`,
  `rules`, `skill`, `version`; move `dashboard`/`serve` from "deferred v1.x" to
  shipped (serve: read-only, loopback, SSE-by-poll, placeholder diff pane).
- Missing status block → `blocked` (there is no `needs-review` state); §3's
  "classify as needs-review" wording goes — CLAUDE.md's hard rule is already the
  canonical statement.
- Renames: `--rules-file` → `--append-prompt-file`; ctx-hint → `context_high`;
  serve embeds its UI as a string constant and polls (functionally equivalent to
  the stated go:embed/inotify — say what it does).
- Close exceeds spec: `--merged` is verified via merge-base ancestry; document it.
- Event schema §3: `done/blocked/failed/auth-required` are states on `finished`,
  not distinct event types; activity is `tool-call`/`text` (no hook enrichment
  yet); extra types `artifact`, `commit`, `review-verdict`, `verification`,
  `verification-refused`, `cancel` exist. The documented families must match
  `internal/events/events.go`.
- `--dir` covers the design's `--no-worktree`/scratch intent; `ws new` +
  `run --workspace` covers `--new-workspace`. Drop the unshipped flag names or
  mark them rejected.

## Keep as intended-but-unbuilt (mark explicitly, point at tracking)

`--phase` vocabulary + adapter-written phase artifacts; `--budget`/`--max-turns`;
`max_concurrent` + queue; `fork`/`ask`/`takeover`; per-subscriber notifier
granularity; diff-staleness health signal; upstream-drift machinery; index→
transcript event-ID cross-reference; capability tiers (`sandbox: os|policy|none` —
the hooks task reintroduces the need); subagent event tagging; `needs-decision`.
Each keeps one line and a pointer (ROADMAP item or "no task yet").

## Do NOT touch here

The six posture deviations (claude bypass without hooks, `--dir` writable default,
push-as-prose, gc/auto-close mismatch, scan-not-flock + PID-reuse liveness,
base-ref = HEAD) — those are code fixes with their own tasks/remainder entries;
DESIGN stays aspirationally correct on them. §12 is being rewritten by
retire-the-guide — coordinate, don't collide.

## Acceptance criteria

- Header status line reflects reality (shipped, versioned, dogfooding).
- Every §3 verb/flag in the doc exists in `--help` output, or is explicitly
  marked unbuilt with a pointer; nothing in the code's top-level verb set is
  absent from the doc.
- Event family list diff against `internal/events/events.go` constants is empty
  modulo explicitly-marked future types.
- No behavior changes, no code changes beyond comments — this is a docs task.

## Non-goals

- Rewriting §12 (retire-the-guide owns it) or relitigating §13 rejections.

## Log
