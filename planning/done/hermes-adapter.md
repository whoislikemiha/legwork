# Hermes adapter — oneshot worker turns with sidecar telemetry

Status: done · Priority: P2 · Umbrella: **Hermes agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: hermes-parser-finalize · Workspace: ws-87

## Goal

`--agent hermes` works end-to-end: dispatch a hermes oneshot turn through the real
runner, normalize its final-only output and usage sidecar to the contract, resume
across turns, classify failures honestly, and refuse the job shapes hermes cannot
enforce (read-only).

## Probe evidence (2026-07-25, hermes 0.18.2 / 2026.7.7.2, authenticated via OpenAI Codex)

All of the below was measured live on this machine; treat it as the fixture baseline
and re-verify anything the implementation contradicts:

- `hermes -z "<prompt>" --usage-file <f>`: stdout is **only** the final response
  text ("No banner, no spinner, no tool previews, no session_id line"); stderr empty
  on success; exit 0. Approvals are auto-bypassed in oneshot; tools, memory, rules,
  and AGENTS.md in CWD load as normal. Baseline context is heavy (~18k input tokens
  for a trivial prompt — hermes's own system prompt).
- `--usage-file` JSON (written even on failure): `estimated_cost_usd`, `cost_status`
  ("included" on subscription auth), `cost_source`, `input_tokens`, `output_tokens`,
  `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`, `total_tokens`,
  `api_calls`, `model` ("gpt-5.6-sol"), `provider` ("openai-codex"),
  `session_id` ("20260725_022419_cbb177"), `completed`, `failed`, `service_tier`.
- Resume: `hermes -z "<prompt>" --resume <session_id>` works and preserves context —
  but **each resumed turn mints a NEW session_id** in the sidecar. The adapter must
  persist the latest sidecar session_id after every turn (chain), not the first.
- Failure shape: with an unauthed/quota-exhausted provider, hermes **exited 0**,
  printed the provider error as the stdout "response" (`HTTP 400: You're out of
  extra usage...`), and the sidecar had `completed: false, failed: true,
  session_id: null`. **Exit code and stdout are not truth; the sidecar is.**
- Usage errors (e.g. `--provider` without `--model`) exit 2 with a message on
  stderr.
- Model override: `-m/--model` accepts provider-qualified names
  (`anthropic/claude-sonnet-4.6`); `--provider` exists but requires `--model`.
- No sandbox, no plan mode. `--yolo` bypasses dangerous-command approvals (oneshot
  already auto-bypasses); `--accept-hooks` avoids TTY hook prompts on headless runs.
- Sessions live in hermes's SQLite store (`hermes sessions list`); legwork keeps its
  own transcript as always — theirs is live resume state, ours is the record.

## Design

New `internal/adapter/hermes.go`:

- **Bin**: `hermes`, overridable via `LEGWORK_HERMES_BIN`.
- **Command**: `hermes -z <rules + "\n\n# Task\n\n" + task> --usage-file
  <tmpdir>/usage.json --accept-hooks` + `-m <model>` when set + `--resume
  <SessionID>` when resuming. No system-prompt flag → codex-style prepend; rules
  re-asserted every turn. Prompt goes as argv (probe first whether `-z` can read
  stdin; if not, document the ARG_MAX bound — rules + task is far below ~2 MB).
  `cmd.Dir = WorkDir`. Decide and document the config-hygiene stance: run with the
  user's hermes config (auth lives there) but consider `--ignore-rules` vs letting
  hermes inject its own AGENTS.md/memory on top of legwork's rules — recommendation:
  keep defaults (workers benefit from repo AGENTS.md; claude/codex also read repo
  context), note the memory-injection caveat in the guide.
- **ReadOnly**: `Command` returns an error for `ReadOnly` turns and `run` rejects
  `--read-only`/read-only phases for hermes at dispatch (caps honesty; DESIGN §3/§9).
  Add the capability flag this needs (e.g. `Caps.ReadOnly bool`, true for
  claude/codex/cursor/fake, false for hermes) and gate in one place in `run`.
- **Parser** (final-only, per hermes-parser-finalize): `Line` accumulates raw stdout
  (it is plain text — emit nothing per-line, or at most one `text` index event for
  the accumulated body at finalize); `Finalize` reads the sidecar, then:
  - sidecar `failed: true` → state `failed` (or `auth-required` via marker match on
    the stdout text: `not logged in`, `hermes portal`, `codex login`, `401`,
    `unauthorized`, `invalid api key`, `token expired`; quota wording like
    `out of extra usage` stays `failed` until the transient-provider-recovery task
    gives it a home). **Never run `ParseStatusBlock` over error text.**
  - sidecar `completed: true` → `ParseStatusBlock(stdout)` as usual; missing block →
    blocked (fixed direction).
  - sidecar missing/unreadable → nil result → existing interrupted path.
  - Telemetry: `TokensIn/Out`, `Context` = `input_tokens + cache_read_tokens` of the
    turn (single-call oneshot; matches the codex convention), `CostUSD` from
    `estimated_cost_usd` only when `cost_status` is a metered value — subscription
    ("included") stays 0/absent like codex (see cost-rollup task vocabulary).
  - `SessionID` = sidecar `session_id` (the NEW one, every turn).
- **Caps**: `Fork: false`, `OSSandbox: false`, `StructuredStatus: "convention"`,
  `Subagents: false` (hermes MoA/subagent surfaces exist but are unverified —
  claim the weaker value), plus the new read-only flag false.
- **Effort/fallback**: rejected for hermes (no equivalent surface).
- **Wiring**: `adapter.New("hermes")`, `--agent` help strings, doctor (generic
  checks suffice; the live probe must pass through the finalize path).

## Tests

- **Unit**: command construction (resume, model, readonly rejection, env override,
  accept-hooks always present); parser finalize against fixture sidecars — success,
  failed:true, auth markers, missing sidecar, empty stdout; session chaining; the
  "never parse status block from error text" rule; cost gating on `cost_status`;
  doctor surfaces a concrete finalization error rather than the generic
  "agent exited without a result" diagnostic (carried from Opus job-222).
- **Fake seam + e2e** (`test/hermes_e2e_test.go`): via the final-only fake path —
  happy (`state: done` in final text + sidecar), needs-input → answer resume loop
  (verify the persisted session_id is the second turn's), provider failure
  (exit 0 + failed:true → `failed`, never `done`), auth-required, mid-turn death →
  interrupted, `--read-only` dispatch rejection with a clear error.

## Acceptance criteria

- Full suite green: `gofmt -l . && go vet ./... && go test ./... -count=1`.
- `legwork doctor --agent hermes` passes here (live probe through finalize).
- Live smoke (this machine, temp state dir): task-shaped prompt → `state done`,
  sane context (~18k+), no cost claimed on subscription; then `answer`/`resume`
  continues with context intact and meta shows the updated session_id.
- A hermes job with `--read-only` fails at dispatch with an error that names the
  reason (no harness read-only) and the alternative (use claude/codex/cursor).
- Events for a hermes job show started/text/usage/finished — and nothing invented
  mid-turn.

## Non-goals

- Docs trio + AGENTS.md smoke (next task).
- Streaming/ACP integration (`hermes acp` is bidirectional JSON-RPC — the parked
  "bidirectional persistent workers" lane, not this).
- Transient/quota failure taxonomy (transient-provider-recovery task owns it).

## Log

- 2026-07-25 implementation (job-223): added the Hermes oneshot adapter, argv
  prompt composition, environment-overridable binary, per-turn sidecar cleanup and
  finalization, latest-session chaining, sidecar-authoritative failure/auth handling,
  cost-status gating, truthful caps/read-only dispatch rejection, CLI wiring, and a
  final-only fake seam. Kept Hermes's normal user config and repo rules/memory
  injection; prompts use argv because `-z/--oneshot` requires a value (normal
  rules+task remain well below ARG_MAX).
- Deterministic evidence: adapter unit fixtures and `test/hermes_e2e_test.go` cover
  success, subscription/metered telemetry, needs-input→answer with the second
  session ID persisted, exit-0 provider failure, auth-required, missing/malformed
  sidecars, empty output, stale-sidecar prevention, mid-turn death, no invented
  tool events, unsupported model controls, and read-only refusal. `gofmt -l .`,
  `go vet ./...`, and `go test ./... -count=1` passed (the first full run hit the
  already-tracked `TestCodexPassthroughs` teardown race; the exact rerun passed).
- Worker-sandbox live evidence: Hermes 0.18.2 was found and invoked through both
  direct oneshot and `legwork doctor --agent hermes`. A writable temporary Hermes
  home was needed there; the adapter correctly surfaced `failed: API call failed
  after 3 retries: Connection error.` from the finalized sidecar/stdout, but outbound
  provider access was unavailable.
- Orchestrator live evidence with workspace-built `/tmp/lw-hermes-ws87`:
  - `doctor --agent hermes --json`: `ok: true`; live probe `state done`.
  - Normal real turn `job-1`: `state done`, sidecar session
    `20260725_030708_27a336`, `context: 124730`, subscription cost omitted.
  - Resume real turn `job-2`: first turn reached `needs-input` with question
    "Should the final marker be alpha or beta?" and session
    `20260725_030750_109ecb`; `answer` with alpha reached `done`, result `alpha`,
    turn count 2, and persisted the new session `20260725_030758_431769`.
  - Session advancement assertion passed.
- Host checkpoint receipt `verification:job-223:1784941579010127490` passed the
  complete formatting, vet, and full-suite gate on the reviewed tree.

## Friction

- Live Hermes probes need to write their own logs/session store under
  `~/.hermes`; the worker sandbox makes that tree read-only even though the task
  explicitly requires a real-agent smoke. A first-class writable agent-state mount
  (plus network for explicitly required live probes) would avoid copying a minimal
  config/auth home into the job temp directory.

### Review verdict

- Opus/high job-224: `SHIP`. Deterministic and authenticated live evidence accepted.
- Premature Cursor wording in the read-only alternative was corrected during
  closeout; Cursor can be added back once its adapter actually lands.
- Auth-marker precision is non-blocking and belongs with the existing
  transient-provider-recovery taxonomy work; argv/`ARG_MAX` remains a documented
  oneshot CLI limitation.
