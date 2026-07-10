# legwork — Design

> Delegate the legwork to headless coding agents; keep the judgment.

Status: design accepted, not yet implemented. This document seeds the future public
`legwork` repo. Successor to `claude-worker` (tmux screen-scraping); sibling of
`workstree` (separate repo, ships first).

## 1. Thesis

legwork lets **any orchestrator agent** (Hermes, or a human at a shell) drive
**headless coding-agent workers** (Claude Code, Codex, future agents) — locally or over
ssh — through one normalized contract, instead of learning each CLI separately.

**The product is the contract, not the CLI**: a small verb set, one event schema, one
status convention, and the promise that any agent behind it honors them. The CLI is the
transport for the contract.

Core stance, decided and non-negotiable:

- **Headless-only control plane.** Workers run via `claude -p --output-format
  stream-json` / `codex exec --json`. No TUI scraping, no prompt-marker heuristics, no
  tmux as a control primitive. Readiness = process state; results = structured output.
  (tmux/herdr remain fine as optional *viewers*.)
- **CLI is the API; ssh is the transport.** `ssh homepc legwork ...` and `legwork ...`
  are indistinguishable to the caller. All state lives on the machine doing the work.
  No listening daemon that can mutate anything.
- **Explicitly no MCP-server integration** (README gets a "why not MCP" section): the
  CLI contract is transport-agnostic, agent-agnostic, auditable via the job dir, and
  doesn't require the orchestrator to hold a live connection for a two-hour job.
- **Two planes, never conflated**: control plane (orchestrator ↔ worker, structured,
  deterministic) and observation plane (humans watching/attaching). The old design's
  fragility came from machines talking through a human interface.
- **Dumb tool, smart orchestrator.** legwork runs one job well and enforces the
  contract. Pipeline opinion (plan→implement→review, retries, escalation policy) lives
  in *recipes* (documentation), not verbs. Orchestrators compose.
- **Agent-first ergonomics**: `--json` on everything, stable exit codes, never an
  interactive prompt, cursor-based reads, idempotent verbs.

## 2. Object model

### Job — the primitive

One headless agent session doing one scoped piece of work. Owns: a job dir (events,
transcript, artifacts, metadata), an agent session ID, telemetry, a lifecycle
(`queued → active → finished(done|needs-input|blocked|failed|interrupted) → closed`).

Three **target modes**:

1. **Workspace job** (`--workspace ws-7` / `--new-workspace`): attached to a workspace
   (below). For work that lands as a diff.
2. **In-place job** (`--dir <path>`, git or not): runs directly in an existing
   directory. **Read-only sandbox by default**; mutation requires explicit
   `--allow-write` (skill: "you almost never want this; want writes? use a workspace").
   For research/audit/explain delegation with zero worktree ceremony.
3. **Scratch job** (no target): empty scratch dir. Pure research, doc drafting.
   Artifacts land in the job dir as always.

Everything above the target — events, status, health, resume, fork, notifications,
transcripts — is identical across modes. Non-workspace jobs auto-close on completion
(their artifact was the answer; nothing to reclaim but transcripts).

### Workspace — the accountability unit

**One workspace = one worktree = one branch = one diff = one review gate = one merge
decision = one close.** Jobs are turns taken *in* a workspace: plan (read-only),
implement (mutating), review (read-only), fix-findings (mutating) all attach to the
same lineage. Rules:

- **One active job per workspace at a time** (flock). Two agents editing one tree
  concurrently is never OK.
- Checkpoint refs are per-workspace and span the lineage.
- Parallel work = multiple workspaces. Conflict resolution when landing = either
  another job in the conflicted workspace ("rebase onto main, resolve, re-run tests" —
  a reviewable turn like any other) or the orchestrator by hand. The **orchestrator
  owns merge sequencing**; the tool's affordance is `status` flagging a workspace as
  behind/conflicting with its base.
- PR creation: orchestrator via `gh`, or delegated as a job with explicit
  `--allow-push` (push is default-deny; deliberate override per job).
- Worktrees live **outside the repo** (under state dir / configurable), branches
  namespaced `legwork/ws-7-auth-refactor`, tool-created and tool-deleted only.
- Base ref defaults to default-branch tip; `--base` overrides.
- Workspace creation = `git worktree add` + **`workstree init`** (the sibling
  convention; see its DESIGN.md) + checkpoint-ref setup. Missing `worktree.toml` →
  `needs-bootstrap` event; recipe dispatches the one-shot bootstrap job. **Jobs refuse
  to start in a workspace whose setup failed** — an agent must never begin work in a
  half-built environment.
- `--no-worktree` escape hatch exists for cheap single-job cases (injected rules
  tighten accordingly — no isolation).

### Run — the grouping

`--run auth-refactor`: a label + a run dir, zero semantics (not a pipeline engine).
Run-level `events.jsonl` holds job lifecycle markers and **orchestrator narration**:
`legwork note --run auth-refactor "splitting into 3 workspaces; ws-2 blocked on human"`.
The orchestrator is a first-class *producer* in the log — every `resume`/`answer`/
`approve`/`close` it issues is an attributed event. **No privileged side-channels**:
everything the orchestrator knows is reconstructable from the job/run dirs, so "what
did Hermes see when it approved this" is a query. Reports from orchestrator to human
must be summaries grounded in citable artifacts (job IDs, events, files) — recipe-level
requirement.

## 3. The contract

### Verbs (v1)

```
legwork run       # start a job (see flags below); prints job ID immediately
legwork resume    # continue a job's session with a new instruction / answer
legwork events    # read a job's or run's event index; --since <cursor>, --json
legwork status    # rollup: state, phase, health line; --json
legwork ls        # attention/active/unreviewed jobs; --all includes closed history
legwork watch     # human: live-rendered stream (index; --full taps transcript)
legwork diff      # workspace diff; --since-last-review; --at <ckpt>
legwork answer    # answer a needs-input / needs-decision by ID
legwork approve   # approve a needs-provision command (future: needs-decision); fails closed
legwork cancel    # SIGINT the turn; session survives, resumable
legwork close     # acknowledge + reclaim (see §8); --merged | --discard | --keep-worktree
legwork gc        # reclamation per policy; --dry-run; --close-merged (opt-in)
legwork note      # orchestrator narration into run events
legwork artifact  # save/list/get run-attached orchestration artifacts
legwork guide     # print the orchestrator skill (docs travel with the binary)
```

Deferred to v1.x (contract-compatible): `fork` (branch a session), `ask` (interrogate a
running/finished job via fork without disturbing it), `dashboard` (TUI), `serve` (web),
`takeover` (interactive `claude --resume` in the human's terminal; holds the job lock —
orchestrator actions on a taken-over job fail loud with a distinct state).

Key `run` flags: `--agent claude|codex`, `--run <label>`, `--workspace/--new-workspace/
--dir/--no-worktree`, `--phase plan|implement|review` (maps to access level),
`--allow-write`, `--allow-push`, `--base`, `--model`, `--effort` (thinking/reasoning
pass-through), `--budget` (tokens/$), `--max-turns`, `--timeout` (wall-clock),
`--append-prompt/--rules-file`, `--json`.

### Event schema — the stable public interface

Append-only JSONL, **versioned from day one** (`v` field per line), documented as a
stable interface so third parties build viewers (waybar, Grafana, `tail -f | jq`)
without us shipping them. Event families:

- Job lifecycle: `queued, started, finished, interrupted, closed` (+ disposition, actor)
- Turn semantics: `needs-input, needs-provision, needs-decision, blocked, done, failed, auth-required`
- Activity (from harness hooks, not transcript parsing): `tool-call` (name, target
  file), `file-edited`, `command-run`, `subagent-started/finished` (tagged)
- Progress: `progress` (worker narration), `phase-complete`, `plan-ready`
- Workspace: `checkpoint`, `setup-started/ok/failed`, `needs-bootstrap`,
  `behind-base/conflicts`
- Telemetry: usage snapshots (tokens, cost, context %)
- Orchestrator: `note`, plus attributed `resume/answer/approve/close` records

### Two-tier logs (sizing decision)

- **`events.jsonl` — the index.** One compact line per event: type, ts, actor
  (main/subagent-id/orchestrator), summary fields, truncated previews (~200 chars).
  Everything that renders continuously (ls, status, dashboard, notifier, orchestrator
  polling) reads ONLY this. Low MBs worst case; cursor reads cheap at any size.
- **`transcript.jsonl` — the raw stream capture**, full payloads, referenced from index
  by event ID. Deep dives only. Compressed at job finish; short retention after close.
- Why keep our own transcript when claude/codex persist sessions locally: theirs is
  internal format, per-agent, unstable, with a lifecycle we don't control. Ours is
  normalized and makes the job dir **self-contained** (archivable, auditable). Theirs =
  live resume state (when it expires, `resume/ask/fork` die — surfaced in `ls` as
  "session expired"); ours = the record. Duplication is real but bounded to days.

### Status block — the turn-end convention

Injected worker rules require every turn to end with a parseable block:

```
state: done | needs-input | blocked
question: <present iff needs-input>
blocked: <JSON object iff blocked: kind provision|verify|decision, detail, command>
```

Enforcement is per-adapter (**capability flag** `structured-status: enforced |
convention`): codex via `--output-schema` (schema-guaranteed); claude via Stop-hook
validation (reject turn end until the block parses). The initial codex adapter ships
`convention` (shared parser, no schema); `--output-schema` enforcement is a documented
follow-up (ROADMAP). Missing/unparseable block →
classify as `needs-review`, never assume done. Cheap-model classifier as fallback.
Orchestrator never trusts `done` blindly anyway: verification (diff nonempty, tests
event present, review phase) is recipe-mandated.

Schema-enforced output generalizes beyond status: any machine-consumed phase — review
findings as structured list (file/line/severity) to gate merges on "zero critical",
plan task lists with dependency annotations (→ free parallelization decisions). On
claude: ask for fenced JSON + validate; on parse failure resume with "reply with only
the corrected JSON".

### Capability flags per adapter

Adapters are honest about differences, never pretend the CLIs are identical:
`fork: yes|no`, `sandbox: os|policy`, `structured-status: enforced|convention`,
`subagents: native|none`, `mid-turn-tools: yes|no`. The skill keys guidance off these.

### Exit codes

Stable, documented, part of the contract (exact table = implementation task; classes:
ok / job-level failure / needs-attention / usage error / infra error).

## 4. Prompt composition — the tool injects the rules

**Worker rules are injected by legwork, not pasted by orchestrators.** If the adapter
parses `state:`, the same release must be what taught the worker to emit `state:` —
versioned together, no drift, and an orchestrator that knows nothing beyond "run this
task" still gets parseable behavior. Composition per job:

1. **Baked-in worker rules** (tool-owned): status block format; **ask-early bias** —
   "if a decision is ambiguous and materially affects the outcome, end your turn with
   `state: needs-input` rather than guessing" (round-trips are cheap; this inverts the
   usual autonomous-mode plow-ahead bias, deliberately); no commit/push (default-deny);
   stay in the worktree; update progress at milestones. Adapted per job mode/caps.
2. **Orchestrator additions** (`--append-prompt` / `--rules-file`): per-job/pipeline
   guidance ("follow plan.md", "tests must pass before done").
3. **The task.**

## 5. Questions, approvals, escalation

- **Clarifying questions**: turn ends with `state: needs-input` → `needs-input` event →
  orchestrator wakes, answers from its context via `legwork resume` (same session, full
  context) — or escalates to the human if it's genuinely their call. **Escalation chain:
  worker → orchestrator → human**; the human is in the loop *through* the orchestrator
  by default and *around* it (watch/diff/takeover) for independent verification.
- **Provision-shaped blockers** are explicit gates, not prose: a worker that cannot
  continue because the sandbox cannot run a required command ends with
  `state: blocked` plus `blocked.kind=provision` and an exact command. legwork emits
  `needs-provision`; `legwork approve` runs that command outside the sandbox in the
  job worktree, then resumes the same session. No approval means no command runs, and
  failed provision commands leave the job blocked.
- **Decision-shaped blocked reasons** escalate to the orchestrator/human as judgment
  calls; today the orchestrator answers with `resume`/`answer` after deciding. Future
  `needs-decision` support will extend `legwork approve` for permission-shaped
  judgments rather than conflicting with the provision gate.
- **Permission-shaped decisions** don't round-trip as text: hooks answer policy
  questions instantly (deny push, deny out-of-worktree writes). Future
  `--permission-prompt-tool` support can route genuine judgment calls out as
  `needs-decision` events answered by `legwork approve` — **approval gates fail
  closed** (no timeout-proceed).
- **Steering**: between turns, `resume` *is* the steering. Mid-turn: `cancel` (SIGINT;
  session survives) + `resume` with correction — the headless Escape key.

Parked, contract-compatible (v1.5, event types + verbs designed in NOW so the shim is
the same protocol at different granularity): a stdio toolbelt shim (`legwork _shim
--job N`, same binary, spawned worker-side, talks to the job dir — works over ssh
because it never needs a network path to the orchestrator): `ask_orchestrator`
(mid-turn Q&A; timeout → "proceed with stated safe default or end turn needs-input"),
`report_progress`, `request_approval` (fails closed), `get_artifact` (reads
orchestrator-updatable notes — soft steering without cancel). Toolbelt is
**read-and-request only; workers never mutate the job graph** (no spawn_subjob — a
worker wanting parallelism asks; decomposition is the orchestrator's decision). Also
parked: bidirectional stream-json persistent workers (adapter interface must not
preclude).

## 6. Phases, artifacts, subagents

- **Read-only phase enforcement**: `--phase plan|review` maps to claude plan mode /
  codex read-only sandbox — "nothing else happened" is harness-guaranteed, not
  prompted. Read-only still permits exploration (reads, greps, test runs).
- **The adapter writes phase artifacts** (plan → `plan.md` in the job dir) regardless
  of mode: artifact for the human, the orchestrator, the next phase — and for
  **restartability** (see poisoned-context recipe).
- **Subagents**: the worker's native harness handles intra-job fan-out (claude and
  codex: native → `Caps.Subagents`; where an agent lacks it, "parallelize" means
  decompose into workspaces).
  Subagent activity is tagged in the event index; usage rolls into job totals (budget
  binds — instructed fan-out deserves a deliberately raised budget). Rule of thumb
  (skill): *separately reviewable/abortable → separate workspaces; combined outcome →
  subagents.* Parallel *edits* in one worktree: never — separate workspaces or
  serialize. Injected rules must not accidentally forbid subagents; scope rules bind
  the job, subagents inherit.

## 7. Telemetry, health, observability

**Health line** in `status`/`ls`: context % (the leading indicator), cost $, turns,
last-activity age, diff-staleness. Cost tells you what you spent; **context tells you
what's about to go wrong**: high context + repetitive tool calls + stale diff =
spinning. Recipe (peak recipe material): *poisoned context does not recover by pushing
harder* — cancel, start a **fresh session re-seeded from artifacts** (plan file + diff
summary), never `resume "keep going"`.

Observability stack — every surface is a renderer over the same JSONL files (single
source of truth; orchestrator and human are equal readers; surfaces can never
disagree):

1. **Notifications (push)** — pluggable notifier = exec user-configured command with
   event JSON on stdin (ntfy/Telegram/webhook — their problem, our five lines).
   Content-bearing ("plan ready — 4 steps, 9 files, flags migration. Approve?"), not
   "something happened". Per-subscriber granularity: human takes
   needs-decision/done/failed; **orchestrator takes everything — wake-on-event** is
   what makes long pipelines cheap (idle between checkpoints, full context at them).
2. **`legwork ls`** — one-glance dashboard; also the passive nag surface (unclosed
   jobs, disk, expired sessions).
3. **`legwork watch`** / **`dashboard`** (TUI, v1.x) — merged live timeline: worker
   events and orchestrator notes interleaved.
4. **`legwork serve`** (v1.x) — read-only localhost web UI; SSE live updates (inotify →
   push; browser stays dumb: receives index events, re-fetches rendered fragments);
   diffs render with new-since-review highlighting; reached via Tailscale or `ssh -L`.
   **Invariant: binds localhost, strictly read-only, no mutation endpoints** — the
   network can watch; only the CLI (ssh-authenticated) can act. Assets `go:embed`ded —
   no node_modules on the home PC, any build step runs at release time.
5. **Per-job deep dives**: `watch --full`, `diff`, `ask`, `takeover`.
6. **Orchestrator checkpoint reports** at phase boundaries (recipe), grounded in
   citable artifacts.

### Checkpoints & review-as-you-go

Diffs come from the worktree (ground truth), never reconstructed from logs. The tool
(not the agent) snapshots the worktree tree object to hidden refs
(`refs/legwork/ws-7/ckpt-N`) on progress events / phase ends / timer — no commits, no
branch pollution, agent unaware. Enables: `diff` (all so far), `diff
--since-last-review` (incremental human review; the review cursor is server-side
per-workspace state shared by CLI and web), `diff --at ckpt-N`. Checkpoint refs are
deleted at normal close (they pin objects), unless the workspace is explicitly
preserved for archive/analysis.

## 8. Cleanup — close + gc

Two acts, deliberately separate. Founding invariants collide here ("nothing disappears
before review" vs "tools that hoard disk get uninstalled") — resolution: **cleanup is
gated on acknowledgment, never on time alone**.

**`close`** = acknowledgment with disposition; belongs to whoever owns the outcome — in
the normal flow the **orchestrator, as the final pipeline step** (after merge
confirmation); human closes only abnormal leftovers. Sequence:

1. Safety check: uncommitted changes or commits unreachable from any other branch →
   **refuse** without explicit `--merged` (verified landed) or `--discard` (typed
   destructive intent). Closing can never lose the only copy.
2. Record: state → closed, timestamp, actor, disposition (merged/discarded/abandoned) —
   in the event log.
3. Reclaim local cache immediately: `git worktree remove` and delete
   `refs/legwork/<ws>/*` checkpoint refs. Keep the tool-created branch by default
   because it is the durable artifact; only non-preserved `--discard` deletes it.
   (Secrets note: worktree deletion is also secret cleanup — the workstree copy list
   put credentials there.)
4. Start clocks: transcript retention counts from close. Index + artifacts keep a long
   horizon (KBs; the audit trail). `--keep-worktree` keeps the checkout and its
   checkpoint refs; `--preserve` keeps branch/checkpoint refs without pinning the
   checkout.
5. Touch nothing else: agent session files aren't ours; branches/refs/worktrees we
   didn't create are radioactive.

**`gc`** = reclamation, runs **opportunistically** (git-style auto on any invocation,
no daemon, no cron required; manual `gc` + documented cron line for control freaks;
**auto-delete-by-timer stays off by default**). Jurisdiction: closed jobs only, plus
orphan sweeps (stale legwork worktree registrations, orphaned legwork tree cache past
the grace window, refs with no owning workspace/archive record, runners that died →
`interrupted`, marked never deleted). `--dry-run` prints reclaimable disk. **gc's
blast radius is provably limited to things the tool created.**

**Forgotten close fails safe and loud**: nothing is ever lost (cost of forgetting =
disk, bounded, enumerable); `ls` shows unclosed-finished jobs with age + footprint;
notifier digest past a threshold ("3 unclosed jobs, 14d, 2.1GB"); and the common case
self-heals — *merged-ness is machine-checkable* (`git branch --merged`), so `gc
--close-merged` (opt-in) / the orchestrator's start-of-run sweep auto-closes
landed-but-unacknowledged work. Only abandoned unmerged work waits for human judgment —
correctly, since "worth keeping?" is a judgment call.

Job/workspace footprint is enumerable by construction (one dir, one worktree, one ref
namespace, one branch) — cleanup is closed-form, no "scan the system".

## 9. Security posture & threat model (own section in README)

- **Headless permission stance is mandatory per job** — headless auto-DENIES prompts
  (no UI to answer), so "unset" is the one wrong answer. Mapping:
  - Read-only jobs (plan/review/research/in-place default): claude plan mode / codex
    read-only sandbox. No bypass at all. Most pipeline jobs run *tighter* than
    interactive sessions.
  - Workspace jobs: claude `bypassPermissions` **+ PreToolUse hooks as the real policy
    layer** (hooks fire under bypass; denies win: no push, no writes outside worktree,
    denylisted paths/commands). Codex: `--full-auto` (workspace-write, OS-enforced
    Landlock/seatbelt — *stronger*; never use codex's full bypass flag). Asymmetry
    documented in capability flags: codex containment is kernel-level, claude's is
    hooks + worktree discipline.
- **Prompt injection**: research jobs read hostile web content — which is exactly why
  in-place/scratch default read-only.
- **Credentials on the worker machine**: agent auth (human ritual, once per machine;
  subscription auth works with `-p`) + whatever workstree copies into worktrees.
  Adapter maps auth errors to a distinct **`auth-required`** state (not `failed`) so
  the notification says "run `claude /login` on homepc" instead of Hermes retrying
  into a wall.
- **No listening daemon; web UI read-only localhost** (see §7). ssh is the auth
  boundary for all mutation.
- Hooks wiring: legwork generates a settings snippet pointing PreToolUse/PostToolUse/
  Stop at `legwork _hook` — the binary calling itself; zero dependencies; also the
  event-enrichment source (§3 activity events).

## 10. Resources

- `max_concurrent` (default ~3) + pending queue (queued = event, visible in `ls`).
  Agents are thin API clients; the load is what they *run* (builds, tests).
- Per-job **wall-clock `--timeout`** (budget caps tokens/$ but not time; a hung test
  suite must not hold a slot forever).
- Reboot mid-job: runner dies → `interrupted`; agent session survives on disk;
  `resume` continues. Explicit test.

## 11. Implementation

**Go.** This is an IO-orchestration tool (spawn, tee, append, watch, serve) — Rust's
payoffs buy nothing; Go is the genre's language (gh, lazygit). Cross-compile trivial.

| Need | Choice |
|---|---|
| CLI | cobra |
| Git | **shell out to `git`** — NOT go-git (worktree/ref support patchy; git CLI is the compatibility target) |
| Processes | stdlib os/exec + goroutines |
| JSONL / TOML | stdlib json; BurntSushi/toml |
| Watch | fsnotify |
| Web/SSE | stdlib net/http + go:embed; SSE by hand (~30 lines) |
| TUI | bubbletea + lipgloss (v1.x) |
| Storage | **files only — NO database** (rule, stated now: SQLite would kill tail-f/ssh/schema-as-interface properties) |

**Daemon-less job survival** (build + test FIRST; everything sits on it): `legwork run`
never runs the agent itself — it spawns a detached `legwork _runner --job N` (own
session via setsid, stdio → files) and returns the job ID immediately. The runner is
the only thing touching a live agent process: execs the agent CLI, tees the stream to
transcript, derives index events, triggers checkpoints, writes the result, exits.
Survives ssh disconnect/laptop sleep (this replaces "tmux keeps it alive"). Pidfile +
liveness check (dead runner → `interrupted`, never lies "running"); flock per
workspace; stable exit codes throughout. One binary plays every role: CLI, runner,
hook target, shim (later), server.

**Testing**: fake-agent stub emitting scripted stream-json — including misbehavior
(missing status block, mid-turn death, token explosion) — so the whole pipeline runs in
CI with zero API spend. The contract test suite IS the quality story.

**Upstream drift** (adapters WILL rot; requirement: know ASAP, easy to maintain):
1. Committed snapshots of `claude --help` / `codex exec --help` + stream-format
   samples; daily CI diff → **auto-open issue with the diff attached** the day a flag
   changes.
2. Nightly canary: contract suite against real current CLIs (cheap; catches behavioral
   drift help text doesn't show).
3. The repo's own CLAUDE.md/AGENTS.md written **for the maintenance agent**: how to
   explore a drift issue, where adapters live, update snapshots + capability flags,
   verify via fake-agent suite + canary before claiming fixed. Adapters probe versions
   and fail loud ("codex X dropped --output-schema; update legwork"), never
   mysteriously.

**Install**: goreleaser → GitHub Releases; `curl -fsSL .../install.sh | sh` (OS/arch
detect, `~/.local/bin`, **no sudo, no deps** — works on a bare home PC and an agent can
self-install it); brew tap; `go install`; checksummed binaries. Only the job-running
machine needs it — the laptop needs only ssh.

## 12. Documentation — two audiences

- **Worker-facing rules**: injected at runtime (§4). Not documentation.
- **Orchestrator-facing skill**, split **how-to** (the loop: run → wake/poll → handle
  needs-input/done/blocked → resume → verify → close) vs **recipes** (the playbook —
  most of the skill's value): missing/unparseable status block; stuck-vs-thinking
  (read last events before deciding; then cancel + nudge); poisoned context → fresh
  session from artifacts; verify-before-trusting-done; answer-vs-escalate; model/effort
  policy (big model high effort for plan/review, cheap for mechanical implementation of
  an approved plan, cheap+fast for interrogation — halves pipeline cost); reboot
  recovery; parallel workspaces + merge sequencing (land sequentially; conflict → fix
  job in the workspace; decompose along file boundaries at plan time so conflicts are
  rare by construction); plan task-lists with dependency annotations → fan-out
  decisions; checkpoint reports at phase boundaries, citable; close as final pipeline
  step; start-of-run stale-job sweep; bootstrap a cold repo (workstree); raised budgets
  for instructed subagent fan-out.
- **`legwork guide`** prints the skill; `--help` is the fallback skill — one screen,
  verbs + the loop, sufficient for a cold agent to drive a correct happy path. Docs
  travel with the binary over ssh.

## 13. Out of scope / explicitly rejected

- Pipeline engine in the tool (runs are labels; orchestrators compose).
- MCP server integration (§1).
- `spawn_subjob` / workers mutating the job graph (§5).
- Databases, daemons (§11).
- Windows-native (WSL fine). Multi-host scheduling (orchestrator ssh's to N machines;
  legwork stays single-host). Containers (workstree + worktrees are the lighter answer).
- Auto-merge of parallel workspaces (orchestrator owns landing).
- Matching workstree's naming/branding (independent projects, clean seam).

## 14. Naming & logistics

- **Name: `legwork`** — "the agents do the legwork"; real phrase, instant story, verbs
  read naturally (`legwork run`). Fallback considered: `dispatch`. Availability
  (2026-07-05): no dev-tool collision; crates/brew free; **PyPI taken** (active
  astronomy package — moot for a Go binary); npm previously-unpublished → test-claim
  early. Claim registry names at repo creation.
- License: MIT or Apache-2 (boring, adoption-maximizing; final pick at repo creation).
- v1 verb cut: as §3 (fork/ask/dashboard/serve/takeover deferred; `note` kept — cheap).
- Ships **after** workstree.
