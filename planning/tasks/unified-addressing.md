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
- Any other value is a run label and selects the jobs in that run.
- Set-oriented verbs (`events`, `tail`, filtered lists) operate on the whole run.
- Read-only verbs that need one job may select the newest job, but human and JSON
  output must expose both the supplied selector and the resolved job ID.
- Existing `--run` and `--job` flags explicitly force their namespace. This keeps a
  run named like a real job reachable without making positional selection ambiguous.
- Mutating verbs remain exact-job-only in this slice; they must not gain implicit
  newest-job behavior.
- Workspace IDs remain a separate, explicit namespace.

Existing `--job` and `--run` flags remain supported during migration and resolve
through the same implementation. Combining a positional selector with a legacy scope
flag is a usage error. Empty runs fail with a stable, actionable error rather than
guessing.

## Acceptance criteria

- `status`, `result`, `events`, and `tail` share one resolver and one documented rule
  set; the implementation does not duplicate newest-job selection in each command.
- Human resolution messages stay off scriptable stdout. Single-target JSON includes
  `selector`, `selector_kind`, and the resolved job ID; event arrays/JSONL keep their
  existing top-level shape and already carry job/run provenance.
- Exact job IDs, multi-job runs, empty runs, job-shaped run names, and legacy flags
  have contract coverage.
- Help examples use the same positional grammar consistently.

## Non-goals

- Pipeline, queue, or delegation-tree semantics.
- Making workspace IDs interchangeable with job/run selectors.
- Removing legacy selector flags in the first release.

## Log
