# Cursor CLI capture — authenticated fixtures and behavior probes

Status: next · Priority: P2 · Umbrella: **Cursor agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: — · Blocked on: **human runs `cursor-agent login`
on this machine** · Workspace: —

## Goal

Capture ground truth for the cursor adapter: committed `--help` snapshots, real
`stream-json` transcripts for every result shape the parser must handle, and answers
to the open behavior questions below. The adapter task consumes these fixtures; nothing
about the stream may be assumed from Claude Code resemblance (DESIGN §3 roster:
"fixtures captured, never assumed").

## What is already known (probed 2026-07-25, cursor-agent 2026.01.28-fd13201, unauthenticated)

- Binary: `cursor-agent` (`/home/miha/.local/bin/cursor-agent`); `--version` →
  `2026.01.28-fd13201`; `cursor-agent status` / `about` exist.
- Headless: `-p/--print` ("has access to all tools, including write and bash"),
  `--output-format text|json|stream-json`, `--stream-partial-output` (text deltas).
- Modes: `--mode plan` (read-only planning), `--mode ask` (read-only Q&A); `--plan`
  shorthand.
- Permissions: `-f/--force` = "force allow commands unless explicitly denied";
  `--sandbox enabled|disabled` overrides config; `--approve-mcps` (headless-only).
- Sessions: `--resume [chatId]`, `--continue` (last chat), `create-chat` prints a new
  chat ID, `ls` lists chats.
- Model: `--model <m>` (e.g. `gpt-5`, `sonnet-4`), `--list-models`, `models`.
- Auth: `cursor-agent login` or `CURSOR_API_KEY` env; `-H` custom headers.
  Unauthenticated `-p --output-format stream-json` prints **plain text, not JSON**:
  `Error: Authentication required. Please run 'agent login' first, or set
  CURSOR_API_KEY environment variable.` — this is the auth-required classification
  hook.
- `--workspace <path>` selects the working directory (adapter also sets `cmd.Dir`;
  verify which wins / whether both are needed).

## Deliverables

1. Committed snapshots under `test/fixtures/cursor/` (or the drift-snapshot location
   if one exists by then): `cursor-agent --help`, `cursor-agent agent --help`, and one
   raw stream-json transcript per scenario below, plus a `NOTES.md` recording CLI
   version, date, and each probe's answer.
2. Scenario transcripts (run in a throwaway temp dir, trivial prompts, cheap model):
   - happy path, no tools ("Reply with just: ok");
   - tool use (one file write + one shell command) with `-f`;
   - `--mode plan` turn (confirm edits/commands are actually refused, not prompted);
   - resume: first turn, then `--resume <chatId>` second turn referencing the first;
   - failure: bad `--model` name; killed mid-turn (what, if anything, terminates the
     stream);
   - auth error under stream-json (logged-out or bad `CURSOR_API_KEY`) — capture exit
     code this time.
3. Answers to open questions, each recorded in NOTES.md:
   - Stream schema: event types, where the session/chat ID appears, where the final
     result text appears, whether usage/cost/token fields exist and their names. Is it
     actually Claude-Code-shaped (`system/init`, `assistant`, `result`)?
   - Exit codes: success, task failure, auth failure, bad flag.
   - Prompt transport: does `-p` read the prompt from stdin when no positional arg is
     given (ARG_MAX safety, `-`-prefix safety)? codex uses stdin for this reason.
   - System-prompt seam: any flag equivalent to `--append-system-prompt`? If none,
     rules get prepended to the prompt codex-style.
   - `--sandbox enabled` on Linux: does it observably contain (try a write outside the
     workspace)? Kernel-level or policy? This decides the `sandbox` capability flag.
   - Does `-p` without `-f` auto-deny or hang on command approval? (Headless must
     never hang.)
   - `--list-models` output shape (doctor's model validation may use it instead of a
     paid probe turn).
   - Does stderr carry anything the transcript should keep?

## Acceptance criteria

- Fixtures + NOTES.md committed; every scenario above present; every open question
  answered with the observed evidence (not inference).
- Total spend for the capture stated in NOTES.md (expect < $0.50 on a cheap model).
- No runtime code changes (this task is capture only).

## Non-goals

- Writing the adapter (next task).
- CI drift automation (separate ROADMAP item: upstream-drift tripwire).

## Log
