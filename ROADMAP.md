# Roadmap

Working list, roughly priority-ordered. Informed by DESIGN.md and an external
review (2026-07-05: an orchestrator agent drove legwork cold, including an Opus
worker reviewing this repo through legwork itself). Done items move to the
changelog (release notes); rejected items stay documented below so they don't get
re-proposed.

## Next

_(Codex adapter and `legwork gc` shipped — see below / the changelog. The items
here come from the 2026-07-06 dogfood run that built them: two features
orchestrated through legwork itself, opus workers, split plan/implement/review.)_

- **Fix: `watch` on a resumed job exits immediately** — it replays the previous
  turn's `finished` event instead of waiting for the new turn to end. Forced the
  orchestrator back to sleep-poll loops, the exact thing `watch` exists to
  replace. Likely fix: `watch` after a resume should seek past events older than
  the current turn (or key on runner PID / a turn counter).
- **Fix: context telemetry is cumulative** — `status`/`ls` report ctx summed
  across turns (a long job showed 5.75M), not the live session window. Every
  context-based policy (including the planned threshold hint below) is
  meaningless until this reports the last turn's window size.
- **Verify `--merged` in `close`** — `close ws-N --merged` trusts the caller;
  a merge mistakenly run inside the worktree (a no-op) followed by
  `close --merged` left the branch's commit dangling (recovered via fsck).
  gc's `--close-merged` already verifies with `git merge-base --is-ancestor`
  through `workspace.MergedInto`; wire the same check into `close --merged` and
  refuse (or require `--force`) when the branch isn't actually an ancestor.

## Soon

- **`approve` / `needs-decision`** — route genuine permission judgment calls to
  the orchestrator via `--permission-prompt-tool` (approval gates fail closed);
  hooks handle policy denies (push, out-of-worktree writes) without a round-trip.
- **`fork` / `ask`** — session forking: interrogate a running/finished job
  without disturbing it (`ask`), A/B branch from a plan turn (`fork`).
- **Claude flag passthroughs** — `--max-turns`, `--effort`, `--allowed-tools` /
  `--disallowed-tools` (optional tightening for callers who want it; see
  rejected below for why it's not the default), `--fallback-model`. Dogfood
  (2026-07-06, doctor run): `--effort` is the sharpest gap — a "Fable
  reasoning-low plan → Opus reasoning-high implement" recipe couldn't be
  expressed and fell back to prompt guidance.
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
