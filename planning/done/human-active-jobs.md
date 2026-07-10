# Human-readable active-job observability

Status: in flight · Priority: P1 · Origin: 2026-07-10 live dogfood (`legwork ls` buried job-145 behind giant closed job-96 output) · Depends: — · Workspace: ws-67

## Goal

Make plain `legwork ls` the human answer to “what agents/jobs need my attention right now?” without requiring `--json`, `jq`, shell functions, or knowledge of job IDs.

## Problem observed

The current command prints every job oldest-first, including closed history, and includes task previews containing newlines. A historical job such as `job-96` can consume pages, while the newest active/review jobs appear near the bottom or outside terminal scrollback. The data exists, but the default surface provides no usable operator observability.

This is a human UX problem, not a request for more scriptability. Preserve `--json` for automation, but do not make humans assemble the basic state view themselves.

## Required behavior

1. Plain `legwork ls` defaults to jobs that are not `closed`; this includes terminal-but-unacknowledged work (`done`, `blocked`, `failed`, `interrupted`) because it still needs review/disposition.
2. Sort by attention first, then recency:
   - needs-input/auth-required/blocked/failed/interrupted
   - active/queued
   - done and other non-closed terminal states
   - newest `Updated` first within a tier
3. Every job occupies exactly one physical terminal line. Collapse whitespace/newlines in task/run text and clip the final summary to terminal width using the existing rune-safe presentation helpers.
4. Add built-in filters sufficient for normal operation:
   - `--all` includes closed history
   - `--workspace <ws>` filters by workspace
   - `--run <label>` filters by run
   - `--state <comma-separated>` filters by state
   - `--limit <n>` limits after filtering/sorting; reject negative values
5. Human output must retain job ID, agent/model where available, state, age, context health, workspace, and run/task summary in a stable readable layout. Do not print full result bodies.
6. `--json` applies the same explicit filters/sort/limit and remains machine-readable. Decide and document whether unfiltered `--json` follows the new non-closed default or preserves all-history compatibility; prefer semantic consistency unless a concrete compatibility test requires otherwise.
7. Empty results should print a concise human message that points to `--all` when appropriate; JSON should emit `[]`.

## Tests

- Default hides closed jobs and shows latest active/attention jobs.
- Attention ordering and newest-within-tier ordering.
- Multiline/very long task text renders as exactly one clipped line.
- `--all`, `--workspace`, `--run`, `--state`, and `--limit` work in human and JSON modes.
- Invalid state and negative limit return stable noninteractive usage errors.
- Regression fixture matching the observed failure: huge multiline closed history cannot bury a newer job.

## Constraints

- No database or daemon; read existing job metadata only.
- Do not change the public event schema.
- Reuse presentation helpers rather than inventing another truncation implementation.
- Docs travel in threes where behavior overlaps: guide canonical, skill, README.
- Full verification: `gofmt -l . && go vet ./... && go test ./... -count=1`.

## Blockers

None.

## Log

## Implementation Evidence

- Implemented `legwork ls` filters in `main.go`: default excludes `closed`, `--all` restores history, `--workspace`, `--run`, and `--state` apply before sorting in both human and JSON modes; `--limit` applies after sorting. Negative limits and unknown states fail noninteractively.
- Sorting is attention tier first (`needs-input`, `auth-required`, `blocked`, `failed`, `interrupted`; then `active`/`queued`; then remaining non-closed terminal; then closed history) and newest `Updated` first within a tier.
- Human rows retain job id, agent/model, state, updated age, context health marker, workspace, and collapsed run/task summary. Summary clipping uses the existing `termWidth`, `clip`, and `fmtAge` presentation helpers, so multiline task/run text stays on one physical line.
- Documented the compatibility decision: unfiltered `ls --json` follows the new semantic default and hides closed history; explicit `--state closed` or `--all` returns closed jobs.
- Added e2e regression coverage in `test/present_test.go` for the observed huge multiline closed-history failure, attention/recency ordering, all filters in human/JSON modes, and stable usage errors.

Verification run:

- `gofmt -l .` -> pass (no output)
- `git diff --check` -> pass
- `go test ./internal/job ./internal/events -count=1` -> pass
- `go test ./test -run 'TestLS' -count=1` -> blocked before compile because the sandbox cannot download uncached modules from `proxy.golang.org`.
- Full gate `go vet ./... && go test ./... -count=1` is blocked for the same dependency-cache/network reason.

Orchestrator verification outside the worker sandbox:

- `git diff --check && gofmt -l . && go test ./test -run 'TestLS' -count=1` -> pass.
- `gofmt -l . && go vet ./... && go test ./... -count=1` -> pass; full e2e suite completed in 17.333s.

Independent review:

- `job-148` verdict `FIX`: explicit `--limit 0`, filtered empty-state messaging, selector clipping, board/task-header drift, and `DESIGN.md` drift required correction.
- Corrections applied and covered by focused tests.
- `job-149` re-review verdict `SHIP`: all five findings resolved; no landing blockers.

## Friction

- Verification in this worker sandbox had no cached third-party Go modules, and network DNS to `proxy.golang.org` is blocked. A worker-facing preflight that reports "module cache incomplete for this repo" before the test run would make the failure mode quicker to classify.

## Verdict

Implemented in `ws-67` by jobs `job-146` and `job-147`. Independent review `job-148`
returned **FIX** for five correctness and consistency issues; the orchestrator corrected
them and added regression coverage. Re-review `job-149` returned **SHIP** with no landing
blockers. Focused tests and the full repository gate passed before merge and again on
`main`. Landed 2026-07-10 via `close --merge-into main`.
