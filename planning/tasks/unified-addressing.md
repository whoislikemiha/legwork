# Unified job and run addressing

Status: next · Priority: P1 · Origin: AUDIT E3 + repeated orchestration dogfood · Depends: — · Workspace: —

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

Resolution rules must be explicit and shared:

- An exact `job-N` selects that job.
- Any other value is a run label and selects the jobs in that run.
- Set-oriented verbs (`events`, `tail`, filtered lists) operate on the whole run.
- Read-only verbs that need one job may select the newest job, but human and JSON
  output must expose both the supplied selector and the resolved job ID.
- Mutating verbs must never silently choose among multiple jobs. They require an
  exact job ID unless the run has exactly one eligible target.
- Workspace IDs remain a separate, explicit namespace.

Existing `--job` and `--run` flags remain supported during migration and resolve
through the same implementation. Ambiguous or empty runs fail with a stable,
actionable error rather than guessing.

## Acceptance criteria

- The core control-loop verbs share one resolver and one documented rule set.
- Human resolution messages stay off scriptable stdout; JSON includes
  `selector`, `selector_kind`, and resolved IDs where relevant.
- Exact job IDs, multi-job runs, empty runs, job-shaped run names, and legacy flags
  have contract coverage.
- Help examples use the same positional grammar consistently.

## Non-goals

- Pipeline, queue, or delegation-tree semantics.
- Making workspace IDs interchangeable with job/run selectors.
- Removing legacy selector flags in the first release.

## Log
