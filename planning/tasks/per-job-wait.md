# Per-job blocking wait

Status: in flight · Priority: P1 · Origin: AUDIT E1 + 2026-07-10 dogfood · Depends: — · Workspace: ws-75

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

- 2026-07-10: Implemented in `ws-75` by Terra/high (`job-170`). Host verification
  passed the full Go gate before and after review fixes. Opus/xhigh (`job-171`)
  reproduced a critical default-timeout hang/hot-loop, returned `FIX`, then directly
  reproduced the correction and returned `SHIP` on the focused re-review. The sole
  non-blocking re-review nit (deadline-guard the explicit-state timeout test too) was
  applied as landing hardening.

## Friction

- 2026-07-10: The Terra workspace-write job's isolated Go cache was empty and could
  not reach `proxy.golang.org`, so the orchestrator ran the full gate successfully on
  the host. The later Claude read-only review had access to a populated host cache;
  these were different sandbox/cache conditions, not contradictory verification.

## Review 1 — FIX

Opus/xhigh reproduced one critical bounded-wait failure: `wait <job> --timeout D`
without `--until` never checked the deadline and then busy-spun after it. Required
corrections:

1. Evaluate the deadline for default and explicit-state waits; a supplied timeout must
   always bound the command.
2. Keep a positive sleep/backoff even if a deadline has just passed so no future logic
   error can create a hot polling loop.
3. Add a default-form timeout regression protected by a test-level deadline.
4. Emit an empty `waited_for` array for the default “leave queued/active” form rather
   than listing the states being left.
5. Make explicit empty `--until` return the intended “requires at least one state”
   usage error; remove the unreachable parser branch.

## Review 2 — SHIP

Opus/xhigh reproduced the original invocation against old and corrected binaries:
the old binary required `SIGKILL`; the corrected command returned timeout at the
deadline with exit 1, no CPU spin, and `waited_for: []`. Full verification passed.
