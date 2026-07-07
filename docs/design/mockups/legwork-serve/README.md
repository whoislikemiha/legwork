# `legwork serve` mockup-first artifact

Status: mockup/spec only — no Go implementation in this workspace.

Open the prototype directly:

```bash
xdg-open docs/design/mockups/legwork-serve/index.html
# or serve this docs directory with any static file server
```

## What this mockup is testing

The ROADMAP `## Next` requirement says `legwork serve` must be designed as an HTML mockup over real state-dir data and approved by Miha before any Go is written. This artifact is a companion interactive prototype used to explore layout/interaction. The primary design contract for implementation is the reconciled pair:

- `docs/design/mockups/serve.html` — primary visual mockup.
- `docs/design/mockups/serve-spec.md` — primary read-only localhost/SSE route and robustness spec.

This prototype contributes these UX findings to that contract:

- **Run overview stays visible**: left rail is one row per run label, newest first, mirroring `runs`/`dashboard` rollups.
- **Pipeline drill-in is one click**: center pane shows jobs for the selected run plus a merged timeline.
- **Job detail is persistent**: right pane keeps selected job health, task/result, and diff/review affordances visible while scrolling the timeline.
- **Browser strengths are used deliberately**: native scroll, wider typography, click targets, and side-by-side panes replace the TUI’s confusing/clunky scrolling from the dashboard dogfood.

The embedded data is a hand-curated snapshot from `~/.local/state/legwork` captured read-only on 2026-07-07T16:54:50Z:

- `roadmap-next-dogfood-20260707-185304` with active `job-18` / `job-19`.
- `presentation` run with `job-15` / `job-16` and the dogfood note that promoted browser `serve`.
- `ctx-hint` and `passthrough` runs for historical rollup examples.

## Read-only / localhost invariant

The UI intentionally makes the safety model visible:

- Bind only to `127.0.0.1` / `[::1]`; remote access is via Tailscale or `ssh -L`.
- Serve only `GET`/`HEAD` endpoints.
- No mutation endpoints ever: no `POST answer`, `resume`, `approve`, `close`, `gc`, or `commit` from the browser.
- Mutation-shaped affordances are disabled and should expose copyable CLI commands instead, preserving “CLI over ssh is the write path.”

The eventual implementation should follow `docs/design/mockups/serve-spec.md`'s candidate HTTP shape. In short:

```text
GET /                                   static shell, embedded assets
GET /events                            SSE invalidation stream
GET /fragments/runs                    rendered run rollups
GET /fragments/timeline?scope=&id=     selected timeline slice
GET /fragments/jobs/{jobID}            job drill-in panel
GET /fragments/diff/{wsID}             rendered diff, optional since-review view
```

## SSE and timeline assumptions

`internal/timeline` already has the right substrate:

- Source discovery re-evaluated per `Poll()` so new jobs and run logs appear without restarting the UI.
- Per-file cursors map naturally to browser SSE cursors.
- Curated/firehose filtering can be shared with `tail`/`dashboard` so surfaces do not disagree.
- Run rollups reuse `Rollups`/`RunLogs`; no second aggregation model.

The browser should stay dumb: receive event IDs/invalidation hints over SSE, then re-fetch rendered fragments. That avoids shipping a full client-side model, preserves Go as the renderer, and keeps assets `go:embed`-friendly with no runtime `node_modules`.

## Diff and review notes

Diffs must come from the worktree, not logs. The mockup shows the intended visual treatment for `diff --since-last-review`:

- normal additions/deletions use standard diff colors;
- lines newer than the shared review cursor get a stronger left rail highlight;
- the review cursor itself is server-side workspace state shared by CLI and web.

Open concern: the ROADMAP says `diff --since-last-review` is still Later, while `serve` wants the highlighting. A first `serve` implementation may need either a read-only preview of all diff lines plus a disabled “since-review pending” state, or it must pull the review cursor work forward as part of the approved `serve` slice.

## Robustness concerns before writing Go

- **Large logs**: timeline fragments need paging/windowing; do not send the whole historical stream on every refresh.
- **Escaping**: previews and diff lines must be HTML-escaped; event previews are externalized agent text.
- **Run labels in URLs**: labels like `roadmap-next-dogfood-20260707-185304` are safe, but implementation must URL-escape arbitrary labels.
- **Active runner liveness**: display `active` from reconciled meta, but avoid write-side reconciliation from web handlers unless that path is proven read-only.
- **Closed high-context jobs**: rollup `ContextHigh` deliberately ignores closed jobs; detail panes can still show historic context so humans understand why a job was risky.
- **No daemon creep**: `serve` is a foreground read-only viewer, not a mutating background service.
