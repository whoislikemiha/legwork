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

The implementation reads the existing event/liveness sources. It does not create a
new event type or mutate a healthy job.

## Acceptance criteria

- Waiting works across initial runs, resumes, answers, cancellation, and mid-turn
  runner death.
- Multiple waiters can observe one job without interfering with each other.
- Human output is one concise terminal result; JSON and exit behavior are stable.
- `tail --until-idle` remains the run/pipeline-scoped primitive and notifier hooks
  remain the push alternative.

## Non-goals

- Scheduling jobs, reserving runners, or implementing a pipeline engine.
- Waiting for workspace lifecycle milestones such as merge or close.

## Log
