# Truthful live job health

Status: next · Priority: P1 · Origin: AUDIT B1–B2 · Depends: — · Workspace: —

## Goal

Make `ls` and `status` answer the operational question: should I keep waiting, inspect
the job, or start fresh? A health warning must be trustworthy enough to change an
orchestrator's behavior.

## Product signals

### Context truth

Codex currently reports cumulative token accounting as if it were the live context
window, producing routine multi-million-token `ctx!` alarms. Report the measurement
basis explicitly (`window`, `cumulative`, or `unknown`) and set `context_high` only
when the value is comparable to the configured window threshold. Unknown is better
than a known-false warning.

### Mid-turn heartbeat

While a runner is active, show last provider activity and any usage snapshot the
adapter can truthfully derive. A live job should not display `ctx:-` with no indication
of whether events are still arriving.

### Workspace progress

For mutating workspace jobs, show recent file/diff progress: changed file count,
diff-size movement, and age of the latest change/checkpoint. High activity with no
workspace progress is the agent-agnostic spinning signal.

Human output stays compact; JSON exposes values, timestamps, and measurement basis so
orchestrators can apply their own policy.

## Acceptance criteria

- A real multi-call Codex turn establishes what the current CLI actually reports;
  tests do not encode an assumed accounting model.
- Codex cumulative totals no longer trigger a false context-window alarm.
- Active jobs show a heartbeat age even when exact context is unavailable.
- Workspace progress distinguishes active editing, long-running verification, and
  stale/no-change work without declaring any of them failed.
- Claude, Codex, resumed jobs, read-only jobs, and dead runners have explicit behavior.
- Existing event fields are not redefined without a schema version; new telemetry is
  additive.

## Non-goals

- Automatic cancellation, token budgets, or judging semantic code quality.
- Pretending all adapters expose the same telemetry.

## Log
