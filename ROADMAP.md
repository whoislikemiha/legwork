# Roadmap

Working list, roughly priority-ordered. Informed by DESIGN.md and an external
review (2026-07-05: an orchestrator agent drove legwork cold, including an Opus
worker reviewing this repo through legwork itself). Done items move to the
changelog (release notes); rejected items stay documented below so they don't get
re-proposed.

## Next

**Sandbox-friction fixes** — from the 2026-07-07 money_intelligence production run
(9 workspaces driven by an orchestrator; every stall traced to the codex sandbox's
no-network/no-tmp environment, and the worst failures were workers silently bending
the product around it: deleting Google fonts + rewriting the build script to make
`next build` pass offline, monkeypatching `fastapi.testclient` globally when pytest
hung). Priority order:

1. **Injected worker contract: anti-workaround rule.** Add one line to the injected
   rules: never modify the test harness, build config, or dependencies to accommodate
   sandbox limitations — report `blocked` with the exact failing command instead.
   Highest leverage; kills the silent-workaround failure mode at the source.
2. **Writable per-job `$TMPDIR` in every sandbox profile, including `--read-only`.**
   Read-only must mean repo read-only, not no-tmp. Today `--read-only` reviewers
   cannot run pytest at all ("no usable temp directory for capture", uv cache-lock
   failures), and anyio's `start_blocking_portal` (FastAPI `TestClient`) reproducibly
   hangs in the workspace-write sandbox — two independent jobs hit it. Diagnose the
   hang while at it: if tmp doesn't cure it, it's the thread/socketpair policy.
3. **`needs-provision` job state** (structured escalation): worker declares
   `{"command": "uv add slowapi"}`, legwork surfaces it like `needs-input`, the
   orchestrator approves, legwork runs it OUTSIDE the sandbox in the worktree, the
   turn resumes. The orchestrator did this by hand three times in one run
   (`uv add`, `uv sync` cache misses, `npm install`).
4. **Orchestrator-side verify hook in `worktree.toml`** (e.g.
   `verify = "uv run pytest tests/unit -q"`): legwork runs it outside the sandbox
   after each turn and attaches the result to job status. Workers stop needing to
   run suites inside a sandbox that can't; reviewers and the orchestrator get
   "308 passed" for free in `status`. (Mitigation already proven: a `worktree.toml`
   with `setup = [uv sync, npm ci]` eliminated the provisioning class entirely.)

_(Codex adapter and `legwork gc` shipped — see below / the changelog. The three
bugs the 2026-07-06 dogfood run surfaced — watch replaying a finished turn on
resumed jobs, context telemetry summing cache reads across a turn's calls, and
`close --merged` trusting the caller's claim — are fixed: watch seeks past
finished turns, claude ctx is the last call's real window, and `--merged` is
verified via merge-base with `--into <ref>` / `--force` escapes. The codex
adapter's ctx may have the same summing issue — verify against a live multi-call
turn before changing it.)_

_(The presentation layer shipped: `runs` (pipeline rollup, one line per run
label), `tail` (`tail -f` across all jobs + run logs, `--until-idle` as the
scriptable wait-for-my-pipeline primitive), a read-only `dashboard` TUI
(bubbletea/lipgloss), and `serve`, a local live browser console. All four are
renderers over shared JSONL/timeline plumbing. `serve` v1 keeps the hard safety
line: localhost by default, GET-only HTTP, no mutation endpoints, real
state-dir snapshots, and SSE refresh over `internal/timeline`. The selected-job
diff/review area is intentionally a read-only placeholder until
`diff --since-last-review` lands. `ls` stays the flat per-job table.)_

_(`ws commit <ws> -m <msg>` shipped: orchestrator-owned, non-empty workspace commits
with attributed `commit` events in the workspace lineage and `final_commit` persisted
in workspace metadata.)_

_(Workspace lifecycle metadata shipped: close records optional `closed_at`, `reason`,
`superseded_by`, `merged_into`, and `retention` fields; `--preserve` records
`retention=preserve` and keeps the worktree/branch/checkpoint refs for analysis.)_

## Soon

- **`serve` information density + progressive disclosure** — tighten the live
  browser console so overview text is compact and predictable: clamp long run
  notes/tasks/results to one or two lines, avoid mid-word/awkward wrapping,
  show full prompts/results only in an explicit inspector or expandable detail,
  prioritize counts/attention/state over raw prose, and keep selected-job detail
  scannable with tabs/sections rather than a wall of prompt text. Dogfood: v1 is
  useful, but the screenshot showed text cut all over the place and unrelated
  prompt/context prose dominating the selected-job panel.
- **`approve` / `needs-decision`** — route genuine permission judgment calls to
  the orchestrator via `--permission-prompt-tool` (approval gates fail closed);
  hooks handle policy denies (push, out-of-worktree writes) without a round-trip.
- **`fork` / `ask`** — session forking: interrogate a running/finished job
  without disturbing it (`ask`), A/B branch from a plan turn (`fork`).
- **Claude flag passthroughs** — `--allowed-tools` / `--disallowed-tools`
  (optional tightening for callers who want it; see rejected below for why it's
  not the default) remain. `--effort` shipped for both claude and codex (codex
  clamps `xhigh`/`max` to high), while `--fallback-model` is claude-specific
  and rejected for `--agent codex`. `--max-turns` was verified absent from the
  installed claude CLI (only `--max-budget-usd` exists) and dropped — not
  invented. Dogfood (2026-07-06, doctor run) motivated `--effort`: a "Fable
  reasoning-low plan → Opus reasoning-high implement" recipe couldn't be
  expressed and fell back to prompt guidance; it now can.
- **Self-describing JSON shapes** — audit every `--json` surface for predictable,
  documented envelopes and examples. Dogfood: `runs --json` returned a top-level
  array, but the orchestrator instinctively queried `.runs[]`; either wrap list
  outputs (`{"runs":[...]}` etc.) or make the exact shape prominent in help,
  docs, and guide examples. Include shape consistency for single objects vs
  lists, JSONL streams, and run/job provenance fields.
- **Run selector consistency** — normalize how commands take run labels.
  Dogfood repeatedly mixed the positional run argument in
  `legwork note <run> <text>` with `--run` flags on dispatch and presentation
  commands. Pick a small grammar and make it uniform in help/examples: either
  run labels are always flag values on
  multi-scope commands, or positional labels are accepted consistently with clear
  disambiguation from job IDs.
- **Codex sandbox validation ergonomics** — make Go/build validation work in
  read-only or workspace-write Codex sandboxes without each worker rediscovering
  `GOCACHE`, `GOMODCACHE`, `TMPDIR`, or `GOTMPDIR`. Dogfood jobs repeatedly hit
  read-only `$HOME/.cache/go-build` and recovered with `/tmp` or `/dev/shm`
  overrides. Consider injected env defaults, `doctor` checks, profile snippets,
  or guide recipes that keep caches out of the reviewed worktree.
- **Codex quota/limit observability** — surface subscription usage/quota/reset
  signals when the CLI or provider exposes them, or classify limit failures and
  notify orchestrators immediately when they hit. Dogfood: current Codex CLI help
  exposes no quota/reset status, so legwork can only infer exhaustion after a
  failed run; if proactive reset times stay unavailable, support
  user-configured reset windows plus a documented manual fallback.
- **Workspace archive / publish workflow** — formalize the post-review flow for
  both shipped and dead work: commit the workspace branch with
  `legwork ws commit`, close with an explicit disposition/reason/retention,
  keep the branch as the durable artifact by default, treat the worktree as cache,
  and make remote branch publishing an explicit
  orchestrator decision (`ws publish` or `close --archive --push`) rather than a
  worker default. GC should report/prune archived artifacts only through
  explicit archive/prune policy, not by silently deleting analyzable branches.
- **Closed-job visibility in `ls`** — `legwork ack <job>` now acknowledges
  terminal workspace-less jobs and stamps `closed`; decide whether `ls` should
  hide acknowledged jobs by default with an `--all` escape hatch. Keep this
  separate from the acknowledgment verb because it affects dashboard/run
  expectations too.
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
