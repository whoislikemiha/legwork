# Cursor docs + live smoke — surface the third dialect honestly

Status: next · Priority: P2 · Umbrella: **Cursor agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: cursor-adapter · Workspace: —

## Goal

Every orchestrator-facing surface that says "claude, codex" says "claude, codex,
cursor" with the differences stated honestly, and AGENTS.md gains the cursor
real-agent smoke recipe so future adapter/runner/rules changes get verified against
it like the other dialects.

## Design

Docs travel in threes (+ the contributor file):

- `internal/guide/guide.md` (canonical): agent list; the `--agent` section gets a
  cursor paragraph — permission story (`--mode plan` read-only vs `-f` mutating,
  sandbox flag status per capture evidence), auth (`cursor-agent login` /
  `CURSOR_API_KEY`), resume semantics, telemetry availability (whatever the parser
  actually surfaces — if context is unavailable, the guide says the health line lacks
  it for cursor rather than implying parity), `--effort`/`--fallback-model` rejected.
- `skills/legwork/SKILL.md`: same content, skill-length; `skill install` target list
  stays as-is (cursor's harness skill dir is out of scope unless trivially known).
- `README.md`: roster line, auth-required hint (`cursor-agent login` alongside
  `claude /login` / `codex login`), status paragraph.
- `AGENTS.md`: add the cursor smoke to "Verify before claiming done", modeled on the
  claude one (task-shaped prompt, temp state dir, expected `state done` + result):

  ```bash
  (
    export LEGWORK_STATE_DIR=$(mktemp -d)
    go build -o /tmp/lw . && /tmp/lw doctor --agent cursor   # auth guard
    /tmp/lw run --agent cursor "Reply with exactly the word PLUMBING-OK. No tools."
    sleep 20 && /tmp/lw status job-1
  )
  ```

  Adjust prompt guidance if the adapter task's smoke showed cursor mishandles the
  "reply with exactly" style the way codex does — copy the measured truth, not the
  template.
- Capability-flag docs: wherever caps are rendered (`status`/guide), cursor's row
  matches `Caps()` exactly.

## Acceptance criteria

- The three docs are mutually consistent (guide is canonical) and consistent with
  `Caps()` and the run-flag rejections; no doc claims a capability the capture task
  didn't prove.
- AGENTS.md smoke recipe runs clean on an authenticated machine and its expected
  output matches reality.
- Live smoke receipts (job IDs, `status --json` output) pasted into this task file's
  Log before review.
- `gofmt -l . && go vet ./... && go test ./... -count=1` green (docs-only changes
  still run the suite).

## Non-goals

- Guide restructuring; only additive edits.
- Upstream-drift CI automation.

## Log
