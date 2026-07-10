# External verification receipts

Status: next · Priority: P1 · Origin: job-146/job-150/job-152 `blocked.kind=verify` dogfood · Depends: quality-receipts · Workspace: —

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
- Verification is accepted only for `blocked.kind=verify` or an explicit workspace
  verification request; it cannot resolve provision, decision, auth, or task failure.

## Acceptance criteria

- Pass, fail, timeout, active-job refusal, missing workspace, and wrong-blocked-kind
  paths are covered in human and JSON modes.
- Output is bounded and obvious secret-bearing environment data is not copied into
  receipts by default.
- Status, notifications, and workspace readiness consume the receipt directly.
- The public event schema evolves additively and the worker's original outcome
  remains reconstructable.

## Non-goals

- Automatically executing commands supplied by a worker.
- A general CI service, build cache, or arbitrary remote executor.
- Treating verification success as review or merge approval.

## Log
