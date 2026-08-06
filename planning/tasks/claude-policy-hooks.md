# Claude policy hook layer (`legwork _hook`)

Status: next · Priority: P0 · Origin: 2026-08-06 DESIGN-vs-code audit (finding 1) · Depends: — · Workspace: —

## Goal

Close the largest posture gap against DESIGN §9: claude mutating jobs currently run
`--dangerously-skip-permissions` with **no compensating policy layer** — no PreToolUse
hooks, no `_hook` subcommand, no generated settings. Containment is worktree
discipline plus prose in the injected rules. The design's stated posture is bypass
**plus hooks as the real policy layer**: denies win, and they fire under bypass.

## Shape of the work

- New hidden subcommand `legwork _hook` — the binary calling itself, zero
  dependencies (DESIGN §9 already specifies this shape).
- At dispatch, generate a job-scoped claude settings snippet pointing
  PreToolUse/PostToolUse at `legwork _hook`, wired via the adapter (settings dir
  inside the job dir, never the user's global settings).
- PreToolUse denies, fail-closed: no `git push`, no writes outside the job's
  worktree/target dir, denylisted paths (the legwork state dir itself). Deny
  reasons surface to the worker so it can route to `blocked` instead of flailing.
- PostToolUse becomes the §3 activity-event enrichment source (`tool-call` with
  file targets, `command-run`) — emit through the existing event index; do not
  invent new schema fields without a `v` bump.
- Update `Caps`: this is where the design's `sandbox: os|policy|none` tier beats
  today's `OSSandbox bool` — claude becomes `policy`, codex stays `os`, hermes
  `none`. Adapter caps change ripples to doctor and the skill.

## Acceptance criteria

- A claude workspace job with hooks active cannot push or write outside its
  worktree; the denial is visible in the event index and the worker's turn
  continues (deny, not kill).
- Hook wiring is per-job and leaves no trace after close/gc (blast-radius rule).
- Fake-agent e2e covers: deny fires, deny reason recorded, job still completes.
- Real-agent smoke (per CLAUDE.md, touches `internal/adapter` + `internal/rules`):
  a claude job instructed to push is denied and reports it.
- Capability tier change covered by contract tests; docs updated per the doc model
  current at landing time (guide trio today, per-verb help if retire-the-guide has
  landed).

## Non-goals

- Stop-hook status-block enforcement (`structured-status: enforced`) — separate
  "Needs a decision" ROADMAP item; this task is PreToolUse/PostToolUse only.
- Codex changes (its Landlock/seatbelt containment already exceeds this).
- `--permission-prompt-tool` / `needs-decision` routing (tracked separately).

## Log
