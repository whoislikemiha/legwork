# Roadmap

Working list, roughly priority-ordered. Informed by DESIGN.md and an external
review (2026-07-05: an orchestrator agent drove legwork cold, including an Opus
worker reviewing this repo through legwork itself). Done items move to the
changelog (release notes); rejected items stay documented below so they don't get
re-proposed.

## Next

_(Codex adapter and `legwork gc` shipped — see below / the changelog. The three
bugs the 2026-07-06 dogfood run surfaced — watch replaying a finished turn on
resumed jobs, context telemetry summing cache reads across a turn's calls, and
`close --merged` trusting the caller's claim — are fixed: watch seeks past
finished turns, claude ctx is the last call's real window, and `--merged` is
verified via merge-base with `--into <ref>` / `--force` escapes. The codex
adapter's ctx may have the same summing issue — verify against a live multi-call
turn before changing it.)_

## Soon

- **`approve` / `needs-decision`** — route genuine permission judgment calls to
  the orchestrator via `--permission-prompt-tool` (approval gates fail closed);
  hooks handle policy denies (push, out-of-worktree writes) without a round-trip.
- **`fork` / `ask`** — session forking: interrogate a running/finished job
  without disturbing it (`ask`), A/B branch from a plan turn (`fork`).
- **Claude flag passthroughs** — `--allowed-tools` / `--disallowed-tools`
  (optional tightening for callers who want it; see rejected below for why it's
  not the default) remain. `--effort` and `--fallback-model` shipped: both
  persist in meta and stick across resume, and are rejected for `--agent codex`
  (claude-specific). `--max-turns` was verified absent from the installed claude
  CLI (only `--max-budget-usd` exists) and dropped — not invented. Dogfood
  (2026-07-06, doctor run) motivated `--effort`: a "Fable reasoning-low plan →
  Opus reasoning-high implement" recipe couldn't be expressed and fell back to
  prompt guidance; it now can.
- **Run artifacts** — a home for orchestration artifacts (plans, process notes)
  attached to the run record instead of the workspace diff. Dogfood: a read-only
  planner wrote its plan to `~/.claude/plans/`, it got copied into the repo for
  the implementer, and review had to catch it as a scratch file that must not
  ship. Something like `legwork artifact save/get --run L` keeps
  intended-for-merge (workspace diff) and intended-for-the-record (run) cleanly
  separate; interacts with the close tripwire, so design before building.
  Second dogfood hit (2026-07-06): the orchestrator's running feedback notes
  lived in a session scratchpad and were lost between sessions — run-attached
  artifacts are the durable home for orchestrator process notes too.
- **Actionable context threshold** — `ls`/`status` already report context size;
  add a visible hint when it crosses a threshold (e.g. "context high — prefer a
  fresh job over resume"). Dogfood: the signal was noticed but the orchestrator
  had to invent the policy; a built-in nudge makes the fresh-reviewer pattern
  the default.
- **Job acknowledge/archive** — `close` is workspace-only, so done
  workspace-less jobs (read-only planners, reviewers) linger in `ls` forever
  with no way to say "reviewed, done with this". Either a job-level
  close/archive verb or an `ls` default that hides acknowledged jobs
  (`--all` to show); gc retention covers reclamation but not the signal.
- **`ws refresh`** — reconcile an open workspace with a moved base. Dogfood:
  two parallel workspaces both landed; the second had to be told to
  `git merge main` by hand inside its tree and resolve conflicts. A first-class
  verb (fetch base, merge/rebase, report conflicts as needs-input) makes the
  parallel-workspace pattern safe by default.
- **Mid-turn cost/context signal** — telemetry only lands at the turn boundary,
  so a $13 turn is invisible until it ends. Even a coarse heartbeat (tokens so
  far, derived from the transcript tee) in `status` while a runner is live
  would let orchestrators catch runaways. Overlaps with the mid-turn toolbelt
  (Later) but is read-side only, so it may be much cheaper to ship.
- **Profiles** — named presets: agent + model + access + rules additions +
  timeout (`legwork run --profile opus-review "..."`). Config-file defined.
  Dogfood: the planner recipe (`run --model opus --read-only --dir <repo>`)
  and the fresh-reviewer recipe were retyped constantly — exactly what
  profiles are for; also document both as recipes in the guide.
- **`max_concurrent`** — cap simultaneous runners with a pending queue (queued
  jobs visible in `ls`).
- **Enforced structured status on codex** — the codex adapter ships with
  convention-based status blocks (`StructuredStatus: "convention"`), same as
  claude. codex's `--output-schema` could force `{state, question, summary}` JSON
  (`"enforced"`), killing the missing-block ambiguity — deferred from the initial
  adapter because it changes `Result` format and needs a worker-rules variant.

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
  daily CI diff auto-opens an issue; nightly canary against real CLIs. Dogfood
  (2026-07-06): validated hard — the fake-agent suite was green while real
  `codex exec resume` rejected `-s/--sandbox` (0.118.0); only the live smoke
  caught it. The AGENTS.md real-agent smoke is the manual version of this.
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
