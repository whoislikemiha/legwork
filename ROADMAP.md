# Roadmap

Working list, roughly priority-ordered. Informed by DESIGN.md and an external
review (2026-07-05: an orchestrator agent drove legwork cold, including an Opus
worker reviewing this repo through legwork itself). Done items move to the
changelog (release notes); rejected items stay documented below so they don't get
re-proposed.

## Next

- **Codex adapter** — the abstraction-prover: second dialect (`codex exec --json`,
  OS sandbox modes mapping to `--read-only`/workspace-write, session resume).
  Expected to flush out claude-shaped assumptions in the adapter interface.
- **`legwork gc`** — reclamation for closed jobs/workspaces: transcript
  compression + retention, orphan sweeps (worktrees without meta, refs without
  workspaces, dead runners), `--dry-run`, opt-in `--close-merged`
  (machine-verifiable via `git branch --merged`). Runs opportunistically
  (git-style auto), never touches unclosed work.

## Soon

- **`approve` / `needs-decision`** — route genuine permission judgment calls to
  the orchestrator via `--permission-prompt-tool` (approval gates fail closed);
  hooks handle policy denies (push, out-of-worktree writes) without a round-trip.
- **`fork` / `ask`** — session forking: interrogate a running/finished job
  without disturbing it (`ask`), A/B branch from a plan turn (`fork`).
- **Claude flag passthroughs** — `--max-turns`, `--effort`, `--allowed-tools` /
  `--disallowed-tools` (optional tightening for callers who want it; see
  rejected below for why it's not the default), `--fallback-model`.
- **Profiles** — named presets: agent + model + access + rules additions +
  timeout (`legwork run --profile opus-review "..."`). Config-file defined.
- **`max_concurrent`** — cap simultaneous runners with a pending queue (queued
  jobs visible in `ls`).

## Later

- **Dashboard TUI** (`legwork dashboard`) — htop-for-jobs: job table + merged
  worker/orchestrator timeline. Read-only.
- **Read-only web UI** (`legwork serve`) — localhost + SSE live updates, diffs
  with since-last-review highlighting, go:embed'd assets. Binds localhost only;
  no mutation endpoints ever (the CLI, hence ssh, is the only write path).
- **`.legwork.md` project context** — per-repo standing instructions appended to
  worker rules (like CLAUDE.md, but for workers dispatched into the repo).
- **Mid-turn toolbelt** (stdio shim, same binary): `ask_orchestrator`,
  `report_progress`, `request_approval`, `get_artifact`. Designed in DESIGN.md §5;
  turn-boundary protocol remains the guaranteed baseline.
- **npm/PyPI wrapper packages** — turn the name-reservation stubs into
  binary-fetching installers (the esbuild/ruff pattern).
- **Upstream-drift tripwire** — committed `--help`/stream-format snapshots,
  daily CI diff auto-opens an issue; nightly canary against real CLIs.
- **diff `--since-last-review`** — per-workspace review cursor shared by CLI and
  web UI.

## Rejected (with reasons — reopen only with new arguments)

- **Permission allowlists as the default worker mode.** Headless mode
  auto-denies anything not allowed, and no allowlist can enumerate arbitrary
  build/test commands — workers would silently lose tools mid-task. The layered
  model stands: read-only harness modes for plan/research, bypass + PreToolUse
  hook denies + worktree blast-radius for mutating turns, OS sandbox on codex.
  (`--allowed-tools` as an *optional* passthrough is fine — see Soon.)
- **`legwork pr` verb.** Landing work is orchestrator territory (gh CLI plus
  judgment). The tool stays a job/workspace substrate; PR creation is a recipe,
  optionally a worker job with `--allow-push`.
- **Kanban/queue semantics, cron triggers, delegation trees.** That's the
  orchestrator's layer (Hermes has all of it). legwork stays the substrate:
  tiny, scriptable, ssh-friendly, structured, detached, review-gated.
- **MCP server integration.** The CLI-over-ssh contract is the product; see
  DESIGN.md §1.
- **Database / daemon.** Files + detached runners, permanently (DESIGN.md §11).
