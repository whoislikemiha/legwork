# legwork serve mockup/spec

Status: historical mockup/spec baseline. The HTML direction was approved and `legwork serve` v1 is now implemented as a live read-only localhost surface; keep this file as the original design contract and update it only when it remains useful as design context.

Artifacts:

- `docs/design/mockups/serve.html` is the visual mockup and should be treated as the primary design contract.
- This file is the concise implementation spec for the eventual read-only localhost web UI.

## Source Material

Read before this spec was written:

- `ROADMAP.md` `## Next`, especially the promoted `legwork serve` item.
- `DESIGN.md` §7 telemetry/observability and checkpoints/review-as-you-go.
- `internal/timeline`: discovery, `Poll` cursor, significance filter, rollups.
- `present_cmds.go`: `runs`, `tail`, `dashboard` command behavior.
- `internal/dashboard`: current TUI model/view and dogfood constraints.
- Current state-dir shape under `~/.local/state/legwork`: `jobs/*/{meta.json,events.jsonl,transcript.jsonl[.gz],runner.log,artifacts}`, `runs/*/events.jsonl`, run-attached notes/plans, `workspaces/*/{meta.json,tree}`, lock/counter/gc files.

The mockup embeds representative real state-dir data from the current dogfood run:

- Current run `roadmap-next-dogfood-20260707-185304` with active Codex jobs `job-18` and `job-19`.
- Presentation dogfood run where `job-16` completed useful work but was blocked from committing by Codex sandbox/gitdir permissions.
- High-context planner examples from `passthrough` and `ctx-hint`.
- `codex-adapter` run notes showing real-smoke, resume, and orchestrator recovery history.

## Product Shape

`legwork serve` is the browser counterpart to `runs`, `tail`, and `dashboard`: a human-readable, live, scrollable observability surface over the same files.

Principles:

- Read-only only. No mutation endpoints, no shell-command buttons, no answer/resume/close UI.
- Localhost only. Bind `127.0.0.1` and/or `[::1]`; remote access is via Tailscale or `ssh -L`.
- Same truth as CLI. Use `internal/timeline` and job/workspace metadata; do not create a second data model.
- Browser stays dumb. SSE announces changed indexes/fragments; browser refetches rendered fragments.
- Native browser strengths. Use real scrolling, deep links, text selection, typography, and responsive layout instead of TUI-style pane gymnastics.

## Visual Contract

The first viewport should show:

- Left rail with read-only/local status, summary counts, run navigation, and state-dir provenance.
- Main run rollup table equivalent to `legwork runs`.
- Live merged timeline equivalent to curated `legwork tail`.
- Selected-job detail fragment.
- Diff preview area with since-last-review highlighting as a visual placeholder.

Needs-input and blocked states must be visually loud. Context-high should be visible but less loud than needs-input.

The page must remain usable at narrow widths. Tables may scroll horizontally on mobile; timeline and detail stack vertically.

## Data Layer

Reuse `internal/timeline` directly:

- `timeline.ScopeAll(store)` for default merged stream.
- `timeline.ScopeRun(store, label)` for run-specific stream.
- `timeline.ScopeJob(store, id)` for job detail stream.
- `timeline.Curated` for default timelines.
- Firehose view is the same stream without `timeline.Curated`.
- `timeline.RunLogs` + `timeline.Rollups` for the run table.

Store reads:

- `job.Store.List` and `LoadMeta` for persisted job state. Do **not** call `Reconcile` from `serve`; CLI/status/gc own reconciliation because it writes `meta.json` and appends events.
- Workspace metadata from existing workspace package or direct read-only helper.
- Diffs from worktree ground truth, matching current CLI diff behavior.

Do not write review cursor state in v1 unless the CLI has the same read/write review-cursor behavior. Until then, since-last-review highlighting may be rendered from an explicit query/baseline only.

## Candidate HTTP Shape

All endpoints are `GET` or `HEAD`.

```text
GET /                                   shell + initial rendered fragments
GET /events                            text/event-stream
GET /fragments/runs                    run rollup table
GET /fragments/timeline?scope=&id=&since=&full=
GET /fragments/jobs/{jobID}
GET /fragments/workspaces/{wsID}
GET /fragments/diff/{wsID}?since_review=1
GET /assets/{embedded-asset}
```

No `POST`, `PUT`, `PATCH`, or `DELETE`. Reject non-read methods with `405`.

Use `go:embed` for static assets. No `node_modules` on the target machine; if a build step is ever introduced, it runs at release time.

## SSE Contract

The SSE loop should be built from `internal/timeline.Poll` plus file notification if available. A polling fallback is required.

Browser behavior:

1. Connect to `/events`.
2. On `timeline`, `rollup`, `job`, `workspace`, or `diff` event, refetch the named fragment(s).
3. On disconnect, reconnect with browser-native `EventSource` retry.
4. Do not apply business logic to raw event JSON beyond deciding which fragment to refetch.

Example:

```text
event: timeline
data: {"scope":"all","fragments":["runs","timeline","job:job-19"],"ts":"2026-07-07T16:55:00Z"}

event: heartbeat
data: {"ts":"2026-07-07T16:55:15Z"}
```

## Robustness

- Missing run logs, missing job event files, and legacy state dirs must render partially instead of failing the whole page.
- Corrupt JSONL lines should behave like `events.Read`: skip the bad line and keep reading.
- A run-log lifecycle marker duplicated by a separately sourced job log should follow `timeline.Poll` semantics and avoid duplicate timeline rows.
- Large logs need bounded initial rendering: newest N curated events, with explicit "load older" read-only pagination later.
- Very long prompts/results must be clipped in tables and fully available in detail/preformatted blocks.
- Active runners can disappear; render the last persisted state without mutating it. CLI/status/gc reconciliation owns writing `interrupted` state and events; opening `serve` must not write state.
- The server must not hold exclusive locks or create state directories while discovering sources.

## UX Notes From Codex Dogfood

Legwork/Codex works well as a headless subagent harness for parallel read/implement turns: the current run gave two independent active jobs, stable event logs, and a usable state-dir story for a mockup.

The weak spot is commit ownership and sandbox boundaries. A Codex worker may be able to edit the workspace tree but not write the gitdir when the worktree gitdir points back to the main repo outside the writable root. That makes browser observability more valuable, but it also reinforces that workers should return artifacts and status; the orchestrator should own commits and all write-side decisions.

Compared with native Hermes-style subagents, the legwork/codex UX is more explicit and auditable because every turn leaves JSONL and workspace state. It is also rougher interactively: progress is turn-boundary and log-driven, and human comprehension depends heavily on presentation surfaces. `serve` should therefore optimize for "what is happening, what needs attention, what changed" rather than becoming a control plane.
