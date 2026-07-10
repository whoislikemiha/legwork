# Per-job blocking wait

Status: next · Priority: P1 · Origin: AUDIT E1 + 2026-07-10 dogfood · Depends: — · Workspace: —

## Goal

Provide the scriptable “wake me when this specific job needs attention” primitive so
orchestrators do not build polling loops or misuse run-scoped `tail --until-idle`.

## Desired experience

```bash
legwork wait job-149
legwork wait job-149 --until needs-input,blocked,done --timeout 20m --json
```

- With no `--until`, wait until the job leaves `queued|active`.
- With `--until`, return when one of the requested states is reached.
- An already-matching job returns immediately with the same result shape.
- A dead runner is reconciled to `interrupted`; wait must not hang on stale liveness.
- Timeout, unknown job, target reached, and terminal-but-not-requested are distinct,
  documented outcomes.
- JSON returns the final job status plus `reached`, `waited_for`, and elapsed time.

This first slice accepts an exact job ID only. Run-level waiting remains
`tail --run <label> --until-idle`; `wait` must not silently select the newest job in a
run. Valid `--until` values are the existing job states.

## Outcome and exit contract

- Exit `0`: the requested state was reached, or (without `--until`) the job left
  `queued|active`.
- Exit `1`: timeout, or the job settled in a non-requested state. JSON distinguishes
  these as `outcome: timeout|terminal-mismatch`.
- Exit `2`: invalid state/timeout syntax, missing arguments, or unknown job, matching
  the existing command-error class.

Human output is one concise line and never includes the full task/result body. JSON
returns `{job, outcome, reached, waited_for, elapsed_ms}` where `job` is the final
persisted metadata. Omitted `--timeout` means no deadline; a supplied duration must be
positive.

The implementation reloads persisted metadata while waiting and uses the existing
liveness reconciliation path for a dead runner. It does not create a new event type or
mutate a healthy job. Concurrent waiters must not clobber terminal metadata or produce
different outcomes for the same transition.

## Acceptance criteria

- Waiting works across initial runs, resumes, answers, cancellation, and mid-turn
  runner death.
- Multiple waiters can observe one job without interfering with each other.
- Human output is one concise terminal result; JSON and exit behavior are stable.
- Already-settled target/mismatch, timeout, invalid state, unknown job, and dead-runner
  paths have focused contract tests.
- `tail --until-idle` remains the run/pipeline-scoped primitive and notifier hooks
  remain the push alternative.

## Non-goals

- Scheduling jobs, reserving runners, or implementing a pipeline engine.
- Waiting for workspace lifecycle milestones such as merge or close.

## Log
