# Unified job and run addressing

Status: in flight · Priority: P1 · Origin: AUDIT E3 + repeated orchestration dogfood · Depends: — · Workspace: ws-74

## Goal

An orchestrator should not need to remember which commands take a positional job ID,
which take `--job`, and which take `--run`. Core read-side verbs use one predictable
target grammar for jobs and run labels.

## Desired experience

```bash
legwork status job-110
legwork status totp-review
legwork events totp-review
legwork result totp-review
legwork tail totp-review
```

This first slice covers the read-side commands in the example: `status`, `result`,
`events`, and `tail`. Resolution rules must be explicit and shared:

- A value matching an existing job selects that job.
- Any other value is a run label. `tail` selects the merged run timeline;
  `events` keeps its existing run-log event index and cursor semantics.
- Read-only verbs that need one job may select the newest job, but human and JSON
  output must expose both the supplied selector and the resolved job ID.
- A log-only run is valid for `events` and `tail`; a one-job verb explains that no
  job is available rather than choosing one.
- Existing `--run` and `--job` flags explicitly force their namespace. This keeps a
  run named like a real job reachable without making positional selection ambiguous.
- Mutating verbs remain exact-job-only in this slice; they must not gain implicit
  newest-job behavior.
- Workspace IDs remain a separate, explicit namespace.

Existing `--job` and `--run` flags remain supported during migration and resolve
through the same implementation. Combining a positional selector with a legacy scope
flag is a usage error. A run with an existing event log is a valid set scope even
before its first job (for notes and artifacts); only selectors with neither jobs
nor a run event log fail with a stable, actionable error rather than guessing.

## Acceptance criteria

- `status`, `result`, `events`, and `tail` share one resolver and one documented rule
  set; the implementation does not duplicate newest-job selection in each command.
- Human resolution messages stay off scriptable stdout. Single-target JSON includes
  `selector`, `selector_kind`, and the resolved job ID. `events` preserves its existing
  bare-event JSON array and per-log `--since` cursor; `tail` preserves its existing
  provenance-bearing JSONL.
- Exact job IDs, multi-job runs, log-only runs, missing selectors, job-shaped run
  names, and legacy flags have contract coverage.
- Help examples use the same positional grammar consistently.

## Non-goals

- Pipeline, queue, or delegation-tree semantics.
- Making workspace IDs interchangeable with job/run selectors.
- Removing legacy selector flags in the first release.

## Log

- 2026-07-10: Implemented in `ws-74` by Terra/high (`job-159`, fresh fix job
  `job-166`). Host verification passed: `gofmt -l .`, `go vet ./...`, and
  `go test ./... -count=1`.

## Friction

- Full Go verification could not start because this sandbox has no network access and its module cache lacks project dependencies; it attempted to contact `proxy.golang.org`.

## Review 1 — FIX

Opus/xhigh found one critical liveness/data-loss regression and four in-scope
compatibility defects. Required corrections:

1. `tail <job> --until-idle` must reload job metadata on every liveness poll; never
   reconcile a stale cached `Meta` over a runner's newer terminal record. Assert the
   final on-disk state/result after the tail exits.
2. Keep `events <run>` on the existing run-log `events.Event` JSON shape and per-log
   `--since` cursor. A merged, provenance-wrapped event index needs an explicit
   versioned design and is outside this selector task; `tail` remains the merged view.
3. Fall through from exact-job lookup to run resolution only for not-found errors;
   preserve corrupt/permission metadata errors.
4. Preserve legacy `events <label> --run` without a user-visible sentinel. It may
   remain the command's existing boolean namespace override rather than adopting the
   string-valued form in this slice.
5. Add focused coverage for live tail liveness, log-only single-job errors, run event
   cursor/JSON compatibility, and non-empty positional job tail output.

## Review 2 — SHIP

Opus/xhigh (`job-165`) independently rebuilt the binary, reproduced the original
tail metadata-loss case and its queued/positional variants, verified all five Review 1
corrections, and ran the full gate successfully. Two low non-blocking observations
were deferred: reconciliation behavior when metadata cannot be persisted, and the
README's abbreviated explanation of the legacy boolean `events <label> --run` form.

## Review 1 implementation

- Restored `events` run selectors to the run-local `events.Event` index and its
  per-log cursor; `tail` remains the merged, provenance-bearing surface.
- Reloaded metadata before tail liveness reconciliation and made reconciliation
  itself refuse to overwrite a newer terminal meta record. Added focused selector,
  event-compatibility, and live-tail regressions.
