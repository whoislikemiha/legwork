# Final-only stream support — EOF finalization in the parser contract

Status: next · Priority: P2 · Umbrella: **Hermes agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: — · Workspace: —

## Goal

The adapter substrate supports agents whose stdout is not a JSONL event stream but
only the final response text (hermes `-z`), and whose turn result needs assembly at
process exit (from accumulated stdout plus a sidecar file). Today both the runner and
doctor's probe loop scan stdout lines and expect `Parser.Line` to return the result
"on the turn's final line" — with no terminal line, no result exists and every turn
would land `interrupted`/probe-fail.

## Design

- Extend the `Parser` contract (`internal/adapter/adapter.go`): after EOF, if no
  result was produced by `Line`, the consumer makes exactly one finalization call —
  e.g. `Finalize() (evs []events.Event, result *TurnResult)` — giving the parser its
  chance to assemble the result from accumulated state. Semantics:
  - `Line` keeps its "result exactly once" contract; `Finalize` fires only when no
    result was emitted, and at most once.
  - A `Finalize` returning nil result means the turn genuinely died mid-stream →
    existing `interrupted` handling is unchanged.
  - Keep the change minimal: either add the method to the interface with a no-op on
    claude/codex/fake parsers, or an optional interface upgrade
    (`if f, ok := p.(Finalizer); ok`). Pick one, justify in the PR; the optional
    interface avoids touching shipped parsers at all.
- Call sites (both, or the dialect behaves differently under doctor vs runner):
  - `internal/runner` — after the scan loop, before the no-result → interrupted
    decision.
  - `internal/doctor` `checkProbe` — after its scan loop, before "agent exited
    without a result".
- `TurnRequest` gains the sidecar seam the hermes adapter needs: a tool-owned
  per-turn scratch path (or reuse `TempDir`) where an adapter may tell its CLI to
  write a results file that `Finalize` reads back. Decide and document: simplest is
  the adapter deriving the path from `TempDir` (already plumbed per job) — no new
  field, sidecar named by the adapter. Prefer that unless a concrete problem appears.
- Fake agent (`internal/fakeagent` + `internal/adapter/fake.go`): support replaying a
  plain-text script (non-JSON lines + optional sidecar fixture write) through a
  production final-only parser via `LEGWORK_FAKE_PARSER`, so hermes e2e runs with
  zero spend. The `#die` mid-turn-death directive must keep working for the
  final-only path.

## Constraints

- No behavior change for claude/codex/fake existing flows: their e2e suites must
  pass untouched.
- The event schema does not change (no new event types needed here); this is an
  internal adapter-contract change, not public surface.
- Missing status block still → blocked; a finalized result with unparseable text
  goes through the same `ParseStatusBlock` path as everyone else.

## Acceptance criteria

- Unit tests: a toy final-only parser through the runner path produces a result at
  EOF; nil-finalize still lands `interrupted`; finalize never fires when `Line`
  already produced the result.
- Doctor probe with a final-only toy/fake parser reports the live-turn check
  correctly instead of "agent exited without a result".
- Full suite green: `gofmt -l . && go vet ./... && go test ./... -count=1`; existing
  claude/codex e2e untouched and passing.
- The contract comment on `Parser` documents the finalization semantics precisely
  (this comment is what the next dialect author reads).

## Non-goals

- The hermes adapter itself (next task).
- Synthesizing mid-turn activity events for final-only agents — quiet is honest.

## Log
