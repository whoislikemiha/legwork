# General workspace evidence/check receipts

Status: later · Priority: P2 · Origin: [Flows](../done/flows.md) evidence-hygiene gap · Depends: quality-receipts, external-verification-receipts · Workspace: —

## Goal

`legwork verify` is truthfully documented as an exact-job `blocked.kind=verify`
handoff — it only fires after a worker has already gone terminal-blocked on a
verification need. Every other verification need (the full flow's deterministic
verification step, an orchestrator's focused check on a direct trivial change, a
plain `done` job an orchestrator wants to double-check) currently has no
first-class way to run a host-side command against a workspace and get a receipt;
orchestrators hand-run the suite and eyeball the output, which is exactly the
raw-log-into-context problem [Flows](../done/flows.md)'s evidence-hygiene section names.

## Desired experience

```bash
legwork ws check ws-12 -- go test ./... -count=1
legwork ws check ws-12 -- sh -lc 'gofmt -l . && go vet ./...'
```

- Runs for any flow, any workspace state (not gated on `blocked.kind=verify` or
  even a terminal job) — this is the general form; `legwork verify` keeps its
  narrower exact-job contract unchanged.
- Executes argv directly on the host in the workspace worktree; shell syntax needs an
  explicit `sh -lc`, same discipline as `verify`.
- Full combined output is captured losslessly to an addressable, environment-redacted
  artifact outside orchestrator context; the command's own return value is a compact
  receipt: exit code, argv, cwd, duration, pass/fail, a summary line, and the artifact
  pointer/digest.
- Receipt is recorded in workspace metadata (latest-per-command or latest overall —
  decide during design) and the workspace event log, the same durability shape
  `verify` already uses.

### Distiller integration

`ws check`'s lossless capture plus compact receipt is exactly the deterministic-
reducer path the flows shared command/evidence distiller contract prefers over a
model when it can produce the receipt without one. The distiller consumes the
captured artifact for genuinely unstructured residue instead of re-running the
command itself. Check receipts should make full capture plus compact distillation
the native ergonomic path.

## Acceptance criteria

- A passing and a failing check both produce a receipt without ever putting full
  output in `status`/`ws review`/notifier payloads.
- Existing `legwork verify` behavior, receipt shape, and `blocked.kind=verify`
  semantics are unchanged.
- Receipt storage reuses the schema-versioned workspace metadata shape from
  [quality receipts](../done/quality-receipts.md) and
  [external verification receipts](../done/external-verification-receipts.md) rather
  than inventing a parallel format.
- Redaction/bounding rules for the artifact are documented and tested (no secrets,
  no unbounded growth).

## Non-goals

- A general verification *policy* engine (which commands, when) — that stays
  orchestrator/flow judgment, expressed in the task file or append-prompt.
- Replacing `legwork verify`'s exact-job blocked handoff.

## Log
