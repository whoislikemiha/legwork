# External verification receipts

Status: in flight · Priority: P1 · Origin: job-146/job-150/job-152 `blocked.kind=verify` dogfood · Depends: quality-receipts, close-commit-receipts · Workspace: ws-81

## Goal

Turn “implementation is complete, but the worker sandbox cannot run the required
checks” into an explicit, auditable handoff instead of leaving the job permanently
blocked or resuming a high-context worker merely to obtain `done`.

## Desired experience

```bash
legwork verify job-146 -- go test ./... -count=1
legwork verify job-146 -- sh -lc 'gofmt -l . && go vet ./...'
```

- Verification runs only after an explicit orchestrator command, in the job's actual
  workspace directory and outside the worker sandbox.
- Arguments after `--` are executed as an argv; shell syntax requires an explicit
  `sh -lc`, so worker-provided prose is never implicitly evaluated.
- The receipt records job/workspace, command argv, cwd, exit code, duration, bounded
  output, actor, and timestamp.
- A passing receipt makes the workspace reviewable/verified without rewriting the
  historical worker turn from `blocked` to `done`.
- A failed receipt remains attention-worthy and exposes a copyable retry.
- Verification is accepted only for an exact workspace-attached job with
  `blocked.kind=verify`; it cannot resolve provision, decision, auth, or task failure.

## Product contract

This slice is exact-job-only. A standalone workspace verification request is deferred;
the job must be terminal, attached to an open workspace with a present worktree, and
have `state=blocked` plus `blocked.kind=verify`. Another active job in that workspace
is a refusal. The worker state, result, blocked detail, last outcome, and turn count are
historical facts and are never rewritten by verification.

`verify <job> -- <argv...>` executes the argv directly on the host with the workspace
tree as cwd. Legwork never inserts a shell; pipelines and shell syntax require an
explicit `sh -lc`. `--timeout` bounds the process, and timeout terminates the process
group so descendants cannot outlive the receipt. Exit 0 is a passing receipt; any
other exit or timeout is a completed failing receipt, printed in human/JSON form with
exit 1 rather than disguised as an internal error.

The job owns the authoritative latest verification rollup. Workspace metadata mirrors
that receipt for readiness queries, and both job and workspace append-only event logs
record every attempt with the same receipt ID. A receipt includes job/workspace, argv,
cwd, pass/fail/timeout, exit code when known, duration, bounded combined output, actor,
start/completion times, and an optional history warning. Workspace metadata advances
additively from schema v1 to v2; legacy v0/v1 remains readable and future versions
still fail closed.

Captured output is bounded at the writer, not only truncated after execution. Legwork
does not serialize the host environment and redacts values of obvious secret-bearing
environment variables from captured output. The explicitly supplied argv remains the
auditable command of record.

Status and the default job list derive attention from the receipt without changing the
worker state: a passing verification is reviewable/unreviewed work, while a failed or
timed-out verification remains verify attention and exposes the exact retry argv.
Verification completion emits an additive `verification` event and a notifier payload
that distinguishes pass from fail.

## Acceptance criteria

- Pass, fail, timeout, active-job refusal, missing workspace, and wrong-blocked-kind
  paths are covered in human and JSON modes.
- Output is bounded and obvious secret-bearing environment data is not copied into
  receipts by default.
- Status, notifications, and workspace readiness consume the receipt directly.
- The public event schema evolves additively and the worker's original outcome
  remains reconstructable.
- Repeated attempts replace only the current rollup; event history retains each receipt
  with a distinct stable ID.

## Non-goals

- Automatically executing commands supplied by a worker.
- A general CI service, build cache, or arbitrary remote executor.
- Treating verification success as review or merge approval.
- Standalone `legwork verify <workspace>` or verification of workspace-less jobs.

## Log

- Terra job `job-188` implemented the first slice. The workspace-built command
  immediately dogfooded itself: its first receipt truthfully captured four host-only
  test mismatches; after correction, the same job received a passing full-gate receipt.
- Opus/xhigh job `job-189` returned `FIX` with three high, four medium, and three low
  findings. Reproductions included stale-pass triage, a verify/resume lost update,
  oversized notifier output, post-cap redaction leaks, unbounded timeout descendants,
  and command-start ambiguity.
- Fresh Terra job `job-190` implemented the adjudicated corrections. The orchestrator
  corrected one stale human-output assertion; the full host gate then passed through
  `legwork verify` itself as receipt `verification:job-190:1783697125410173671`.
- No mechanical second review was dispatched: the independent review found no critical
  defect, all accepted findings have focused regression coverage, and the authoritative
  full gate passed after the correction set.

## Verification

- Host: `gofmt -l . && go vet ./... && go test ./... -count=1 && git diff --check` passed.
- Dogfood: fail and pass receipts remained immutable, the worker stayed historically
  `blocked.kind=verify`, and the passing receipt names turn 1 plus checkpoint/diff identity.

## Friction

- 2026-07-10: the worker sandbox can run dependency-free focused Go tests, but the
  full repository gate cannot fetch the uncached module zip files because DNS/network
  access to `proxy.golang.org` is denied. No dependency or harness workaround was used.
- The workspace-built verifier upgraded `ws-81` metadata to v2 before landing, so the
  installed v1 binary correctly refused to read it. Orchestration had to stay on the
  workspace binary until merge/install; this is the documented one-way upgrade boundary,
  not a new roadmap item.
