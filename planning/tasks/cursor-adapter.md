# Cursor adapter — normalize cursor-agent to the legwork contract

Status: next · Priority: P2 · Umbrella: **Cursor agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: cursor-cli-capture · Workspace: —

## Goal

`--agent cursor` works end-to-end through the real runner: dispatch, stream
normalization, status block, resume, doctor. Third real dialect, built strictly from
the fixtures the capture task committed (`test/fixtures/cursor/`), never from
Claude Code resemblance.

## Design

New `internal/adapter/cursor.go` implementing `Adapter` (see `codex.go` for the
shape of a second-dialect adapter; DESIGN §3 roster row for the accepted mapping):

- **Bin**: `cursor-agent`, overridable via `LEGWORK_CURSOR_BIN` (mirror
  `LEGWORK_CODEX_BIN`).
- **Command**: `cursor-agent -p <task> --output-format stream-json` + `--model` when
  set + `--resume <chatId>` when `SessionID` set + `--mode plan` when `ReadOnly` +
  `-f` when mutating. Prompt via stdin if capture confirmed it (ARG_MAX); otherwise
  positional with the `-`-prefix caveat documented. No system-prompt flag (unless
  capture found one) → prepend `SystemPrompt` to the prompt codex-style, so rules are
  re-asserted on every resume. `--approve-mcps` only if capture shows headless MCP
  approval can hang. `cmd.Dir = WorkDir`; add `--workspace` only if capture showed
  `cmd.Dir` alone is insufficient.
- **Parser**: fresh per turn, normalizing the captured schema to index events
  (`text`, `tool-call` with tool/input fields, matching claude/codex granularity) and
  exactly one `TurnResult` on the terminal event. Session ID from wherever the
  capture found it. `Context` from per-call usage if the stream carries it; if the
  stream has no usage at all, leave 0 and record that honestly in caps/docs (do not
  fake a health signal). Status block via the shared `ParseStatusBlock`; missing
  block → blocked, direction is fixed.
- **Failure mapping**: `IsError`/failed states per the captured failure fixtures;
  auth detection must catch the captured plain-text
  `Authentication required ... CURSOR_API_KEY` line (which is **non-JSON noise** to a
  JSONL scanner — the adapter needs a seam for it, e.g. retain unparsed lines and
  classify at EOF via the hermes finalize hook if the process dies without a result)
  → `auth-required`, never generic `failed`.
- **Caps**: `StructuredStatus: "convention"`; `ReadOnly` harness mode exists
  (`--mode plan`); `Fork`/`Subagents`/`OSSandbox` per capture evidence — when
  unproven, claim the weaker value.
- **Effort/fallback**: cursor has neither; `run` rejects `--effort` and
  `--fallback-model` for `--agent cursor` (same pattern as the codex
  `--fallback-model` rejection in `main.go`).
- **Wiring**: `adapter.New("cursor")`; `run`/`doctor` `--agent` help strings; doctor
  needs no cursor-specific code (Bin/version/probe are generic) but its probe must
  pass live.

## Tests

- **Unit** (`internal/adapter/cursor_test.go`): command construction (readonly vs
  mutating, resume, model, env override); parser against the committed fixtures —
  happy, tool-use, resume, bad-model failure, auth text, mid-turn truncation;
  session-ID extraction; context accounting.
- **Fake seam**: extend `internal/adapter/fake.go`'s `LEGWORK_FAKE_PARSER` to
  `cursor`, replaying cursor-shaped JSONL through the production parser.
- **E2E** (`test/cursor_e2e_test.go`, modeled on `codex_e2e_test.go`): happy path
  with session persisted, needs-input → answer loop, turn failure, mid-turn death →
  interrupted, auth-required classification.

## Acceptance criteria

- `gofmt -l . && go vet ./... && go test ./... -count=1` green; new e2e scenarios all
  covered by the fake agent (zero spend).
- `legwork doctor --agent cursor` passes on an authenticated machine (static checks
  offline; live probe completes with a sane state).
- Live smoke (authenticated, per AGENTS.md conventions, temp state dir):
  `lw run --agent cursor "Reply with exactly the word PLUMBING-OK. No tools."` →
  `status job-1` shows `state done`, result `PLUMBING-OK`; then a resume turn
  (`answer`/`resume`) continues the same chat.
- `--read-only` cursor job provably cannot edit (fixture-backed unit test + one live
  plan-mode smoke).
- Unknown-capability claims in `Caps()` match capture evidence (reviewer checks
  against `test/fixtures/cursor/NOTES.md`).

## Non-goals

- Docs trio + AGENTS.md smoke recipe (next task).
- Sandbox-flag layering beyond what capture proved.

## Log
