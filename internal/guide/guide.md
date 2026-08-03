# legwork — orchestrator guide

legwork runs headless coding-agent turns (claude, codex, fake) as supervised
**jobs**: you dispatch a task, the agent works detached, you read structured events
and a final state, you steer with new turns. Locally or over ssh — every command
below works as `ssh host legwork ...`. All verbs take `--json`.

Record `legwork version --json` in field notes and handoffs when build identity
matters. It reports the release version (or `dev`), commit, dirty flag, and date;
ordinary `go build`/`go install` builds fall back to Go's embedded VCS metadata.

## Installing the loadable skill

The repository's single maintained skill source is `skills/legwork/SKILL.md`, and
release binaries embed that exact file. Install or update it for any supported
agent harness with:

```
legwork skill install --target hermes   # ~/.hermes/skills/legwork/SKILL.md
legwork skill install --target claude   # ~/.claude/skills/legwork/SKILL.md
legwork skill install --target codex    # ~/.codex/skills/legwork/SKILL.md
legwork skill install --target all --json
```

The command is noninteractive. Identical installed content is a no-op; differing
content is a stable `skill-conflict` error and is not overwritten unless you pass
`--force`. Forced replacements save the previous file under
`~/.local/share/legwork/skill-backups/<target>/`, outside harness-scanned skill
directories, so backup copies do not appear as duplicate skills. Keep manual
backups there too, not beside `SKILL.md`.

`install.sh` installs the binary first, then best-effort installs the skill for
detected harnesses on `PATH` (`hermes`, `claude`, `codex`). A skill conflict prints
a note but never turns a successful binary install into a failed install.

Hermes users who normally manage skills with `skills.sh` can use
`legwork skill install --target hermes` as the update step; it writes the standard
Hermes user skill path without adding another maintained copy. For local repo
development, symlink the skill directory instead of copying it, and keep only one
legwork skill visible to the harness:

```
ln -sfn "$PWD/skills/legwork" ~/.hermes/skills/legwork
ln -sfn "$PWD/skills/legwork" ~/.claude/skills/legwork
ln -sfn "$PWD/skills/legwork" ~/.codex/skills/legwork
```

After installing or updating a skill, start a new agent session or use the
harness's skill reload/rescan command. Running sessions may keep the skill text
they loaded at startup.

Agents differ; legwork normalizes them, it doesn't pretend they're identical.
`--agent claude` uses a permission mode; `--agent codex` runs in a kernel sandbox
(`--read-only` → codex's read-only sandbox, otherwise workspace-write) and both
fork sessions and run subagents. The loop, states, resume, and status block are
identical across agents. Every turn gets a per-job `TMPDIR`; in codex
workspace-write turns that temp tree is added as a writable sandbox root, and
codex also gets per-job `GOCACHE`, `GOMODCACHE`, and `GOTMPDIR` there so
build/test caches stay out of reviewed worktrees. Codex read-only is stricter:
the sandbox has no writable-root exception, so temp-writing test suites may still
need a workspace-write review/verification turn. On codex's subscription auth,
per-turn cost is nominal (reported as 0) — watch `context`, not cost.

**Your task prompt is ONLY the task.** legwork itself injects the worker's rules —
the status block contract (`state: done|needs-input|blocked`), ask-early behavior,
no-commit/no-push, and the sandbox anti-workaround guard — into every turn. Do not
repeat or paraphrase any of that in your prompts; a slightly different paraphrase
competes with the injected contract. Add task-specific guidance with
`--append-prompt` instead, or `--append-prompt-file <path|->` for multi-line
guidance without shell quoting. Run `legwork rules` to inspect the exact
contract before writing append-prompt text.

## The loop

```
legwork run --agent claude "task"        -> prints job ID immediately
  ... get notified, or poll ...
legwork status <selector> --json         -> job IDs win; a run selects its newest job:
  done         verify it (diff, tests in events), then next phase or close
  needs-input  legwork answer <job> "<decision>"   (same session continues)
  blocked      inspect status.blocked; approve provision, verify outside, or escalate
  failed       read events; retry as a fresh job or escalate
  auth-required tell the human: agent login needed on this machine (claude /login, codex login)
  interrupted  the turn died mid-flight (crash/cancel); session survives -> resume
legwork result <selector>                -> print the final report, raw
legwork resume <job> "next instruction"  -> another turn in the same session
legwork approve <job> [--timeout 30m]    -> run approved needs-provision command, then resume
legwork verify <job> [--timeout 30m] -- <argv...>
                                         -> host verification receipt; worker remains historical blocked
```

Dispatch options stick for the job's lifetime: `--read-only`, `--append-prompt`
(or `--append-prompt-file`), `--timeout`, `--effort`, and the claude-only
`--fallback-model` are recorded in the job and apply to every resumed turn too.
`--append-prompt-file` reads UTF-8 text from a file, or stdin with `-`, rejects
empty/binary input, and stores the text rather than the path. It is mutually
exclusive with `--append-prompt`. The job record also keeps the original dispatch
prompt (`initial_task` once resumed) and the model — `status --json`
reconstructs any job cold. `--effort` (`low|medium|high|xhigh|max`)
reaches both claude and codex, but codex's reasoning scale tops out at `high`, so
`xhigh` and `max` clamp there. `--fallback-model` is claude-specific; passing it
to `--agent codex` is rejected at dispatch.

Never trust `done` blindly: verify the diff is non-empty and tests ran (visible as
tool-call events) before building on it. A missing/unparseable status block surfaces
as `blocked` — treat it as needs-review, not failure.

Blocked jobs carry `status --json.blocked` when the worker could classify the
reason: `provision`, `verify`, or `decision`. `provision` includes an exact command
the sandbox could not run; `legwork approve <job>` is the explicit gate that runs
that command in the job worktree outside the sandbox, bounded by `--timeout`, and
resumes the same session. No approval means no command runs. `verify` means the work may be complete but the
suite needs an outside-sandbox verification run. For a terminal workspace job, run
`legwork verify <job> -- <argv...>`; it executes argv directly in the workspace
worktree (use `sh -lc` explicitly for shell syntax), records bounded/redacted output,
and leaves the worker's blocked turn intact. A passing receipt makes the work
reviewable; a failing receipt stays attention-worthy and prints a retry. `decision`
should be escalated like any other judgment call.

Workspace metadata schema v2 adds the current verification rollup. Schema v1 (and
legacy unversioned metadata) remains read-compatible and upgrades only on its next
successful metadata write; a newer schema version is refused rather than rewritten.

## Preflight: doctor before you dispatch

A misconfigured machine (agent not logged in, bad model name, unwritable state dir,
broken notifier) otherwise only surfaces *after* a job is spawned and its turn fails.
`legwork doctor` moves that discovery up front — run it once before dispatching:

```
legwork doctor [--agent claude] [--model <m>] [--dir <repo>] [--no-probe] [--json]
binary      ok    /usr/local/bin/legwork (built from 8665e992b364 committed 2026-07-06T19:34:59Z)
state-dir   ok    /home/you/.local/state/legwork (writable)
git         ok    /usr/bin/git (git version 2.50.0)
agent       ok    claude 2.x at /usr/local/bin/claude
probe       ok    live turn completed: state done, model accepted ($0.0012)
workstree   skip  no worktree.toml at .
notifier    ok    command exited 0 (event "doctor" sent)
```

The `probe` runs one real turn with the exact `--agent`/`--model` a `run` would use —
that's what validates auth and model acceptance, so it costs a few tokens. `--no-probe`
does static checks only (offline-safe); `--agent fake` probes for free. `--json`:

```json
{"ok": true, "checks": [
  {"name": "probe", "status": "ok",   "detail": "live turn completed: state done ..."},
  {"name": "notifier", "status": "skip", "detail": "no notify command configured"}
]}
```

`status` is `ok | warn | fail | skip`; top-level `ok` is true when nothing failed
(warns/skips are fine). Exit codes: `0` no failures, `1` one or more `fail`, `2` usage
error (e.g. unknown agent).

## Getting woken up instead of polling

Configure the notifier in `~/.config/legwork/config.toml` (or `$LEGWORK_CONFIG`):

```toml
[notify]
command = "<any shell command>"   # receives a JSON payload on stdin
events  = ["needs-input", "needs-provision", "done", "blocked", "failed", "auth-required", "interrupted", "verification-passed", "verification-failed"]
```

The payload: `{"event", "job", "run", "agent", "task", "question", "blocked", "result",
"cost_usd", "context", "verification"}` — verification completion uses
`verification-passed` or `verification-failed`, with its receipt in `verification`.

- **Human notifications**: `command = "jq -r '\"legwork \" + .job + \": \" + .event' | xargs -I{} ntfy publish mytopic {}"`
  (or any Telegram/webhook one-liner).
- **Orchestrator wake-up**: point `command` at whatever re-invokes *you* — a webhook
  your harness listens on, a queue push, a script that starts your next turn. Then
  your pipeline is event-driven: dispatch, go idle, get woken with the payload,
  decide, dispatch again.
- **Polling fallback** (no notifier): `legwork events <job> --since <last-seq> --json`
  is a cheap idempotent cursor read; `legwork ls` is the one-glance overview.

## Workspaces: any work that changes files

A workspace = one worktree + one branch + one reviewable diff + one close. Jobs are
turns inside it, one active at a time. Parallel work = multiple workspaces.

```
legwork ws new --repo <path>             -> ws-N (runs workstree init if the repo
                                            has worktree.toml; setup failure aborts)
legwork run --workspace ws-N --agent claude "implement X per plan.md"
legwork diff ws-N [--stat]               -> changes vs base, incl. untracked files
legwork ws review ws-N [--model M]       -> read-only independent review over that diff
legwork resume <job> "review feedback: fix Y"
legwork ws commit ws-N -m "message" --json -> orchestrator commit, recorded as final_commit receipt
legwork close ws-N --merge-into main     -> no-ff merge locally, then close as merged
legwork close ws-N --merged|--discard [--reason TEXT] [--retention POLICY]
                                         -> records a close receipt + workspace event, then drops
                                            the local worktree cache
legwork events ws-N --workspace [--since N] [--json]
                                         -> append-only workspace commit/close history
```

**You own git history; workers never commit.** The injected contract forbids
worker commits — do not override it in your prompts ("commit when done" turns a
worker into a historian without the bigger picture, and codex's sandbox can't
write the worktree gitdir anyway). Workers produce tree states; legwork
checkpoints them automatically after every turn. When the diff passes review,
*you* commit with `legwork ws commit <ws> -m <message>` — you know what's one
logical change, what's scratch, and what the message should say. The command
stages the workspace tree, refuses empty commits, records `final_commit` in
workspace metadata with a stable receipt ID, actor, message, and time, and writes
one attributed `commit` event to the workspace history (plus compatible job/run
events that carry that same receipt ID). Then land it.

Workspace history is a separate event index: query it with `legwork events ws-N
--workspace`, including its normal `--since` cursor and `--json` form. If the
commit or close is already durable but its history append cannot be recorded,
the command still succeeds and the receipt/output carries `history_error`; do
not retry a non-idempotent commit or close. The warning is persisted when the
metadata store remains writable.

`close` without a flag refuses if there are unreviewed changes — that's the review
gate. After review, the usual local landing path is
`legwork close <ws> --merge-into main`: legwork requires the workspace tree to be
committed, switches the source checkout to that local target branch, runs
`git merge --no-ff`, aborts cleanly on conflicts, records `merged_into` in one
close receipt (with disposition, actor, retention/supersession facts, and final
commit), appends a workspace-history event, then
closes. It refuses remote targets, self-merges, dirty target checkouts, and
rechecks HEAD after switching so the merge never runs from the workspace branch.
On a failed/conflicted merge it restores the checkout that was current before the
switch. Use `-m` to supply the merge commit message, and `--json` for
`{ok,state,blocked}` output where `blocked.kind` distinguishes `conflict` from
`guard-refused`; the `--merge-into` conflict path exits `1`, guard refusals exit
`3`, and ordinary CLI failures remain normal non-zero errors.

If the work landed by another path (PR, manual merge), close `--merged`.
`--merged` is verified, not trusted: the branch must actually be an ancestor of
the default branch (or `--into <ref>`), else close refuses. The verified target is
recorded as `merged_into`. `--force` skips the check for work that landed somewhere
legwork can't see (cherry-pick, another remote). For superseded/dead work, add
`--reason`, `--superseded-by`, and `--retention`; use `--preserve` when the
branch and checkpoint refs should remain available for analysis. Closed branches
are kept by default; the checkout is disposable cache and is removed unless
`--keep-worktree` is explicit, which also keeps checkpoint refs for inspection.
Non-preserved `--discard` is the destructive path that deletes the branch.

Independent review is first-class: `legwork ws review <ws>` checkpoints the exact
tree, dispatches a read-only workspace job seeded with that snapshot's diff, and
records the latest parsed review receipt in workspace metadata. The receipt names
the reviewer job/model, checkpoint, diff SHA-256, verdict, and finding counts; its
job event log remains the detailed historical source. It defaults to
`--effort high` and the selected agent's default model; pass `--model` for your
configured big reviewer model. The reviewer prompt asks for a structured
`{"verdict":"SHIP|FIX","findings":[...]}` report before the normal status block.
Malformed or missing verdict JSON is recorded as unparsed and fail-closed; it never
becomes a guessed `SHIP`. Closed job metadata likewise retains `last_outcome`, so
`status` can show the worker's final state and reason after lifecycle state becomes
`closed`. It does not auto-fix, auto-merge, or own the landing decision — route `FIX` with
`resume`/a fresh fix job, and land only after your judgment says the diff is ready.
Scratch/research jobs need no workspace: plain `run` gets a scratch dir;
`run --dir <path>` works in-place — combine with `--read-only` for plan/research
turns (harness-enforced: the agent cannot edit).

## Cleanup: ack, close + gc

Three separate acts. `ack` **acknowledges** one terminal workspace-less job
(planner, reviewer, read-only check) and stamps its retention anchor. It is job-level
only: workspace jobs are acknowledged by closing their workspace. `close`
**acknowledges** one workspace with a disposition, records archive metadata in
`workspaces/<ws>/meta.json`, and drops its local worktree cache immediately
unless `--keep-worktree` is set. Branches are durable and kept by default;
checkpoint refs are dropped unless `--preserve` or `--keep-worktree` keeps them
for archive analysis. `gc`
**reclaims opportunistically**: closed and provably-orphaned things only,
**never unclosed work**.
`ack` and `close` also remove each closed job's per-job temp/cache tree; events,
transcripts, and artifacts remain on the normal retention path.

```
legwork ack job-14            -> mark reviewed terminal workspace-less job closed
legwork ack job-14 --force    -> acknowledge a non-terminal workspace-less job
legwork gc                    -> reconcile dead runners, compress/retire transcripts,
                                 sweep orphan refs/worktrees, report orphan branches
legwork gc --dry-run          -> same summary prefixed "would"; mutates nothing
legwork gc --close-merged     -> also close open workspaces whose branch has landed
legwork gc --close-merged --close-merged-into origin/main   -> explicit target ref
```

What gc does: flips dead-runner jobs to `interrupted` (resumable, never deleted);
gzips a finished job's transcript, then deletes it past the retention horizon while
the event index + artifacts persist as the audit trail; prunes stale worktree
registrations and deletes `refs/legwork/*` with no owning workspace/archive policy.
Open workspaces and closed `retention=preserve` archive workspaces own their
checkpoint refs. `--close-merged` (opt-in) closes an open workspace only when its
committed branch is an ancestor of the default branch (`git merge-base
--is-ancestor`) and the tree has no uncommitted changes — dirty or unmerged
workspaces are always left for human judgment. Closing this way drops the local
worktree and leaves the branch reachable. gc's blast radius is strictly what
legwork created; non-legwork repo branches/refs/worktrees are untouchable.

gc also runs **automatically** and cheaply on dispatch (`run`/`resume`/`answer`),
git-style, gated to at most once per `auto_interval` (default 24h). Configure under
`[gc]` in `config.toml` (`auto`, `auto_interval`, `transcript_compress_after`,
`transcript_retain`, `orphan_grace`); `auto = false` disables the opportunistic run.

## Health: watch context, not cost

`legwork ls` shows attention/active/unreviewed jobs first and hides closed
history by default, in both human and JSON modes. Use `--all` for history,
`--workspace <ws>`, `--run <label>`, `--state <comma-separated>`, and
`--limit <n>` to narrow the view; explicit `--state closed` is a direct closed
history query. Each row is one terminal line, with multiline task/run text
collapsed and clipped to the terminal width.

The `ctx:145k` cell is the live window of the session's most recent agent call
(what the next call will pay to re-read).
High context + no new diff progress = a spinning worker. The fix is NOT
`resume "keep going"`: cancel, then start a **fresh job** seeded with the artifacts
(the plan file, `legwork diff` output) — a poisoned context does not recover.
Costs are also tracked (`status`), cumulative per session.

legwork flags this for you: once a job crosses a threshold, `ls` marks its ctx
cell `ctx:180k!` and `status` prints a `hint:` line (`--json` sets
`context_high: true`) — the built-in cue to start fresh over resume. Tune it under
`[health]` in `config.toml` with `context_threshold` (tokens, default 150000; set
`0` to disable — useful for large-window models).

## Grouping and narration

Core read commands (`status`, `result`, `events`, and `tail`) take one selector.
An existing job ID wins; any other selector names a run. `status` and `result`
resolve a run to its newest job. `events` reads that run's own event index with
its native cursor, while `tail` includes every job in the run. Use `--job <id>`
or `--run <label>` to force a namespace when a run is named like a job (for
`events`, the existing `events <label> --run` form remains the run override).
Only a selector with neither jobs nor a run event log fails clearly rather than
selecting an unrelated job. A log-only run (notes/artifacts) is valid for `events`
and `tail`; `status` and `result` explain that they require a job. Single-target
JSON includes `selector`, `selector_kind`, and `resolved_job`; human resolution
notices go to stderr so stdout stays scriptable.

Group a pipeline's jobs with `--run <label>`; narrate your decisions:

```
legwork run --run auth-refactor --read-only --agent claude "plan the refactor"
legwork note auth-refactor "plan approved; splitting implement into 2 workspaces"
legwork artifact save --run auth-refactor --name plan.md ./plan.md
legwork artifact list --run auth-refactor
legwork artifact get --run auth-refactor plan.md
legwork events auth-refactor            -> run event index: lifecycle + your notes
```

Notes make your reasoning auditable — report decisions as you make them.
Artifacts make your process durable without polluting workspace diffs: plans,
review notes, job/workspace maps, comparison notes, and other orchestration files
belong under the run record. Save from stdin with `-`; use `--overwrite` to replace
an existing artifact deliberately. v1 accepts UTF-8 text/markdown artifacts and
rejects binary data. `artifact save/list/get` support `--json`; `save` records an
`artifact` event in the run log.

For long run-specific append prompts, save the text once as a run artifact and
reuse it at dispatch:

```
legwork artifact save --run auth-refactor --name append-prompt.md ./append-prompt.md
legwork artifact get --run auth-refactor append-prompt.md |
  legwork run --run auth-refactor --append-prompt-file - --agent claude "task"
```

## Watching a pipeline

Four read-only surfaces render the same event logs at different zoom levels.
All are strictly read-only; `runs` and `tail` are plain-stdout (work over
`ssh host legwork ...`, no TTY), `dashboard` needs a terminal, and `serve`
starts a local browser console.

```
legwork runs                 -> one line per run label, rolled up (the overview)
legwork tail                 -> tail -f across all jobs + run logs (the live feed)
legwork dashboard            -> interactive TUI: runs + selected-job + timeline
legwork serve                -> browser operator console on localhost (GET-only)
```

`runs` is the pipeline overview `ls` never was — one line per `--run` label,
newest activity first, with a job-state rollup, total cost, a `!` when any live
job's context is high, and your most recent `note` for that run:

```
RUN           JOBS  STATE          COST    CTX  LAST   NOTE
passthrough   2     done           $3.39   ok   19h    merged 85bd6f9, smoke green
ctx-hint      2     1 active · 1 done  $2.76  ok  2m    dispatched implementer to ws-5
(no run)      1     needs-input    $1.10   !    5m
```

Jobs with no `--run` collapse into one `(no run)` line. `--json` emits the
rollup array (`label, jobs, cost_usd, context_high, updated, last_note`).

`result` is the pipe-friendly way to fetch the worker's final report. It prints
the raw `result` field for a job, or for the newest job in a run label; use
`--turn N` for an earlier retained turn and `--json` when a script needs an
envelope.

`tail` follows the merged stream — worker events and your notes interleaved by
time, newest at the bottom. It backfills the last `-n` events (default 30) then
follows live. Scope with `--run <label>` or `--job <id>`; add `--full` for the
firehose (tool calls, progress, usage). Finished lines carry the turn's
`state · cost · ctx`:

```
19:02 [ctx-hint]  note      dispatched implementer to ws-5
19:04 job-14      started   Implement the context threshold…
19:11 job-13      finished  done · $2.17 · ctx:73k
```

`--until-idle` makes `tail` the scriptable *wait for my pipeline* primitive: it
exits 0 once no job in scope is active or queued (after draining events), so an
orchestrator can replace a polling loop with `legwork tail L --until-idle`.
`--json` emits the merged events as JSONL (raw event + `job`/`run` provenance).

For one exact job, use `wait` instead of building a polling loop or relying on
run selection:

```
legwork wait job-149
legwork wait job-149 --until needs-input,blocked,done --timeout 20m --json
```

Without `--until`, it returns once that job leaves `queued|active`. With
`--until`, it returns when one of the named existing job states is reached.
`wait` reloads metadata and reconciles a dead runner to `interrupted`, so it
does not hang on stale liveness. JSON is one object with the final persisted
`job` metadata plus `outcome` (`reached`, `timeout`, or `terminal-mismatch`),
the actual `reached` state, `waited_for` (empty when `--until` is omitted), and
`elapsed_ms`; human output is one concise line. Reached outcomes exit 0; timeout
and a settled non-requested state exit 1; invalid state/duration syntax and an
unknown job exit 2. The positional
argument is always an exact job ID — runs remain the scope of `tail --until-idle`.

`dashboard` is htop-for-jobs: an attention banner, a prioritized runs/jobs
rollup, a detail pane for the selected job (status, task, recent events — `f`
toggles the firehose), and the curated timeline. Keys: in overview, `j/k` (or
arrows) move the selection across jobs and `enter` focuses detail; in detail,
`j/k` scroll recent events and `esc` returns to overview; `q` quits. needs-input
jobs get the loudest treatment on every pane. It needs a TTY; without one it
points you at `tail` and exits 2.

`serve` is the browser view for a human watching live multi-agent work. It prints
a URL and blocks until interrupted. By default it binds `127.0.0.1:0`; non-loopback
`--addr` values are rejected unless `--allow-remote` is explicit, because the page
shows local task, event, path, and result data. The HTTP surface is GET-only:
`/` serves embedded HTML/CSS/JS, `/api/snapshot` returns the run/job/attention/
timeline snapshot, and `/events` is an SSE stream that tells the browser to refresh.
Mutation-shaped controls are disabled; answer/resume/diff/close remain CLI actions.

## Flows

The verbs above are the mechanics; a **flow** is the documented multi-role delivery
architecture you compose them into. A flow is a recipe with explicit role contracts,
handoff artifacts, a lane sized to risk, evidence hygiene, and receipt-backed
transitions — it is not a pipeline engine, a scheduler, a new job state, or a `flow`
verb. legwork stays a dumb substrate: files, jobs, workspaces, events. The
orchestrator composes flows out of them, and the single-job loop (`run` → `status` →
route → `resume`/land) is still the primitive every role uses underneath.

### Roles

Duties are non-overlapping on purpose — that's what preserves independence without
adding redundant agents to normal work. Every role except the orchestrator runs as an
ordinary legwork job (fresh read-only, or a workspace job); "role" is a contract you
put in the task/append-prompt, not a new primitive.

- **Orchestrator** — the persistent decision-maker; sole owner of the CLI, the
  roadmap, and landing. Preserves user intent and scope, classifies the lane, sets
  the mandatory `adversarial_required`/`ui_verdict_required` flow-intake flags
  explicitly (never left as an implicit default) before context assembly, approves
  plans, decomposes work, routes model/quota choices, answers routine worker
  questions, watches context health, curates compact evidence, enforces verification
  and independent review, serializes landing, harvests friction, and escalates only
  genuine product decisions. React to stale diffs, repeated failure, poisoned context,
  and stale receipts rather than blindly resuming through them.
- **Planner** — a fresh **read-only** job run in-place in the repo (`run --read-only
  --dir R`) so it can inspect the task file and code instead of planning blind in a
  scratch dir. Investigates before prescribing: locks down
  contracts, compatibility-relevant interfaces, invariants, data flow, failure
  semantics, touched files/dependencies, risks, migrations, and acceptance/
  verification criteria. Precise about contracts, not speculative about private
  mechanics. Produces one durable plan artifact (`artifact save`); never edits code.
- **Implementer** — a mutating **workspace** job. Implements the approved plan, writes
  the tests the change needs, reports deviations/risks, keeps scope bounded, and
  returns a compact evidence-oriented result — not a log dump. Never commits (the
  injected contract already forbids it), never silently changes a public contract,
  never invents success criteria after the fact, never expands scope without asking.
- **Verifier/tester** — not automatically a standing agent. The planner defines
  acceptance gates; a deterministic host-side boundary executes them and records a
  receipt (today: `legwork verify` for the exact-job `blocked.kind=verify` handoff —
  see "Evidence hygiene" below for its real scope); the reviewer independently
  reproduces targeted checks. Architectural/security/data-integrity work adds a fresh
  adversarial test engineer (`--read-only`, attacks the change, doesn't write it).
  UI-visible work adds a fresh real-browser experience verifier that retains
  screenshots/console/network evidence and a concise usability/accessibility verdict.
  Ordinary test code may come from the implementer; adversarial/acceptance criteria
  must not originate solely from the implementer.
- **Evidence distiller** — not a standing role either. Deterministic, exit-code-bearing
  output is reduced by a tool boundary (`legwork verify`, `go test`, etc.); a worker
  uses its own native subagents for its own noisy exploration; only genuinely
  unstructured evidence (mixed UI/browser logs) gets a fresh cheap-model distiller job
  that returns verdict/failures/warnings/metrics/artifact-pointers and nothing raw.
- **Independent reviewer** — a fresh **read-only** job. `ws review` auto-seeds the
  exact diff; everything else the reviewer needs is the orchestrator's explicit
  responsibility, assembled as one compact **`review-context` artifact** and piped
  in via `--append-prompt-file` (see "Reviewer seeding" and Stage 4 below for the
  exact commands) — never left implicit. The bundle is exactly:
  - the approved plan/acceptance contract,
  - the independent deterministic verification receipt (the native `legwork
    verify` receipt, or the fallback receipt artifact for an ordinary `done` job),
  - the adversarial-test receipt, when one ran,
  - for every UI-visible change, the real-browser experience verifier's verdict
    plus its screenshot/console/network evidence pointers.

  It never includes the implementer's own self-assessment or raw logs — those are
  exactly what independence is checking against, not evidence for it. Independence
  in model/agent is also not automatic: pass explicit `--model` for a different
  model family, and explicit `--agent` when you want a different adapter entirely —
  `ws review`'s defaults (`--agent claude`, the agent's default model) do not
  guarantee independence from the implementer. Checks plan traceability,
  correctness/edge cases, test adequacy, security/trust boundaries,
  data-integrity/concurrency/idempotency, compatibility/migrations/public contracts,
  simplicity/complexity/scope, cleanliness/maintainability/drift, relevant
  performance/operability, and doc consistency. Findings are concrete, evidenced,
  severity-ranked, and routed as `FIX`. Never edits code, never owns landing.

### Model policy (example, not a rule)

One accepted high-intelligence/cost-balanced roster (accepted 2026-08-02), to make
"which model for which role" concrete without hard-coding it into substrate
semantics — swap models freely, the roles and lanes stay valid:

```
orchestrator (persistent)   Sol 5.6, high effort, standard speed
planner (fresh, read-only)  Fable 5, default/high effort
implementer (workspace)     Sonnet 5, default effort
independent reviewer        fresh Sol 5.6, high effort, read-only
mechanical worker/distiller Luna, low/medium effort
```

Max effort and fast mode are exceptions, not defaults. This is one accepted example
roster, not a hard-coded policy — the planned `orchestrator profiles` work
(`planning/tasks/orchestrator-profiles.md`) is where a roster like this becomes named,
inspectable dispatch config.

Luna is the mechanical option for throughput and quota preservation, not because a
small diff deserves weaker judgment. Use it only when the contract is already exact,
the change is narrow and reversible, and a focused deterministic gate can catch the
obvious failure modes. Model-family independence still outranks the roster: if Luna
mutates code, route review to a fresh non-OpenAI reviewer; if the reviewer is Sol,
keep Sonnet as the implementer. Promote out of Luna immediately when repository
inspection reveals ambiguity, public-contract impact, security/data/concurrency risk,
or unexpected files.

### Lanes — size the flow to the risk

Review of mutating code is mandatory in every lane; lanes change how much runs
*before* review, never whether review happens. Real-browser experience verification
is likewise not lane-scoped: it applies to every UI-visible change — mechanical and
normal lanes included, not only architectural — per the Verifier/tester role above;
lane only decides how much *else* runs alongside it.

1. **Mechanical** — skip the planner when the task file is already an adequate
   contract; cheap-model implementer; one focused deterministic gate; still a bounded
   independent review from a different model family for every mutating change (so a
   Luna implementation needs a non-OpenAI reviewer; Sonnet implementation may use
   Sol). Documentation-only/no-code changes
   may use orchestrator verification instead when there is no meaningful independent
   code review to run. Promote to Normal on scope growth, unexpected files, a failed
   gate, ambiguity, or a real review finding.
2. **Normal (default)** — planner → approved plan artifact → implementer → deterministic
   verification → fresh independent review → `FIX`/reverify/re-review as needed →
   orchestrator lands.
3. **Architectural/high-risk** — design-only plan + adversarial design review *before*
   any code; decomposed implementation; deterministic verification; a fresh adversarial
   tester where useful; real-browser experience verification for UI; a fresh
   independent review each round; an optional second independent review for
   security/public-contract/data-integrity changes; a human checkpoint only for genuine
   product/risk decisions.

### Context and evidence hygiene

Principle: **lossless capture outside decision-maker context; distilled evidence
inside it.**

- The orchestrator consumes status, compact receipts, results, notes, and artifact
  pointers — never raw transcripts, full build/test logs, or browser logs.
- A worker's final report is concise and decision-oriented; long evidence goes to an
  addressable artifact first, not into the report body.
- Exit code, command argv, duration, an environment-safe redacted log pointer/digest,
  failures, warnings, and summary metrics stay distinguishable from each other. A
  model's summary can never override a deterministic exit status.
- `legwork verify` today is exactly what it says: an exact-job `blocked.kind=verify`
  handoff for a terminal workspace job, not a general all-lane verification gate. Its
  receipt's captured output is capped at 64 KiB and redacted — a **bounded receipt,
  not a lossless capture**; when full evidence is required, pre-capture it through the
  sanitized host-side path first — see the "general workspace evidence/check receipt"
  roadmap item for the gap.
- Unstructured UI/mixed logs go through an on-demand cheap-model distiller: give it the
  evidence artifact in a disposable context, keep only its normalized report, and leave
  the raw evidence drillable by pointer.

### Flow ledger — states reconstructed from receipts, not new tool states

```
intake → planned → implemented → verified → reviewed → landed → harvested
```

- **intake** — guarded by a lane classification and a task/run note, plus two
  mandatory flow-intake classifications, `adversarial_required` and
  `ui_verdict_required` — not optional shell defaults. Both are set explicitly for
  every flow (from the lane and from whether the change is UI-visible), and each
  must be validated as exactly `true` or `false` before context assembly runs; an
  absent or invalid value is a hard stop, never a silent `false`. Only an explicit
  `false` permits omitting that evidence from Stage 4's bundle; an explicit `true`
  requires a successful read of the corresponding compact receipt before the
  reviewer dispatches — see Stage 4 below.
- **planned** — guarded by a plan artifact saved under the common,
  `$task_id`-qualified name `acceptance-contract-${task_id}.md` — never a bare
  `acceptance-contract.md`, since artifacts are run-scoped/create-only and a
  campaign runs many tasks under one shared run/wave (explicitly skipped for the
  mechanical lane as a ledger stage — no planner job runs — but the same qualified
  artifact name is still populated there, from the approved task file rather than a
  planner job, so Stage 4's review-context assembly never has to branch on lane).
- **implemented** — guarded by a non-empty diff plus a compact implementer result.
- **verified** — guarded by exactly one of two mutually exclusive paths, chosen by
  the implementer job's terminal state, never both and never skipped: `legwork
  verify`'s receipt for a job that went terminal `blocked.kind=verify`, or, for an
  ordinary `done` job, a **structured JSON receipt artifact** (passed/exit code,
  exact argv, duration, sanitized-log artifact name + digest, workspace, diff
  digest, round ID + attempt, completion time) saved immutably under a create-only,
  round-and-attempt-qualified name — **durable but provisional and non-native**: it
  persists like any other artifact, it just isn't mirrored onto a workspace/job
  rollup the way `legwork verify`'s receipt is, until `ws check` ships.
  The round ID is fresh per verification round (never reset to a bare `attempt=1`
  across a `FIX` round, which would collide with an earlier round's names); a
  `legwork note` is only a 200-rune preview and points at the receipt artifact's
  name, never carries the fields itself. Both paths gate on a deterministic
  pass/fail signal: only a pass marks this stage reached, and only while its
  recorded diff digest still equals the diff's *current* digest — natively via the
  workspace's `latest_verification.passed`/`diff_sha256` rollup, or, for the
  fallback, by re-reading the exact receipt artifact selected for this round (never
  a cached shell variable); a fail, or a digest that has since moved, returns to
  *implemented* (see Transitions below) and never advances to *reviewed*/*landed*.
- **reviewed** — guarded by the workspace's `latest_review` rollup (not just the
  reviewer job's own output) showing `job` matching the exact reviewer job dispatched
  for this round (poll boundedly for the match — the workspace save that mirrors a
  finished review can lag the job's own terminal status, and a stale prior-round
  receipt must never be accepted in its place), `parsed=true`, `state=done`,
  `verdict=SHIP`, and a `diff_sha256` matching the diff's *current* digest; any FIX,
  malformed receipt, job mismatch, or digest mismatch never authorizes landing. The
  reviewer is seeded with a compact `review-context` artifact (plan/acceptance +
  the verification receipt + adversarial-test receipt when present + UI verifier
  verdict/evidence for UI-visible changes) — never the implementer's
  self-assessment, never raw logs.
- **landed** — guarded by the `ws commit` + `close` receipts, and only once *both*
  the *verified* stage's passing digest and the *reviewed* stage's SHIP digest still
  equal the diff's current digest — reloaded fresh immediately before commit
  (`latest_review` re-fetched and re-checked for `job` match, `latest_verification`
  or the exact fallback receipt artifact re-read, the diff digest recomputed) rather
  than trusted from whatever was cached during the *verified*/*reviewed* stages,
  since either can go stale while the other stage runs.
- **harvested** — guarded by the roadmap/task-file move, friction harvested, and `gc`.

None of these is a legwork job/workspace state — they're read off existing receipts
(`status`, `ws review`, `verify`, close metadata, run notes/artifacts). Transitions:
`FIX` returns to *implemented* carrying findings; today's review receipt has no
persisted per-finding ID field (it records reviewer job/model, checkpoint, diff
SHA-256, verdict, and finding counts — see "Independent review is first-class"
above), so reference findings by a manual round/index convention, e.g.
`review-job-id#1` (the reviewing job's ID plus the finding's position in that
round's report). Stable persisted finding IDs are future work — see
`planning/tasks/review-finding-dimensions.md`. A failed verification returns to
*implemented* carrying only distilled failures and the raw-log pointer, never the raw
log itself; a poisoned context (see below) never resumes — it starts a fresh seeded
session and re-enters at the same ledger stage.

### Executable flow shapes

This is **a staged command skeleton, not a paste-and-run pipeline.** Legwork stays
orchestrator-driven: every stage below ends in a hard-stop condition, and nothing
after that condition is safe to run unless it holds. Treat each fenced block as one
stage you execute and check before typing the next one, never as a script you pipe
through top to bottom unattended.

**Every per-task artifact needs a `task_id`.** Artifacts are run-scoped and
create-only (`--overwrite` aside), and a campaign puts many tasks/workspaces under
one shared `--run <label>` wave — a bare `acceptance-contract.md` name collides the
moment a second task in the same wave tries to save its own contract under it.
Before Stage 1 (or, for the mechanical lane, before the contract save below), pick
one filesystem-safe, unique `task_id` for this flow instance — the planning task
file's slug is the default (e.g. `feature-x` for `planning/tasks/feature-x.md`):

```
task_id=feature-x   # filesystem-safe, unique per flow instance — default is the
                 # planning task file's slug; never leave per-task artifacts bare
                 # in a shared run/wave
```

Every common per-task artifact this shape saves is qualified by `$task_id` from
here on — never a bare `acceptance-contract.md` in a shared run.

**Intake also fixes `adversarial_required` and `ui_verdict_required`.** These are
mandatory flow-intake classifications, not optional shell defaults — set both
explicitly here, from the lane (architectural/high-risk work) and from whether the
change is UI-visible, before anything else in this flow instance runs:

```
adversarial_required=false   # explicit true for architectural/security/data-
                              # integrity work per the Lanes section; never left unset
ui_verdict_required=false    # explicit true for any UI-visible change, any lane;
                              # never left unset
```

Stage 4's review-context assembly validates both are exactly `true` or `false` and
hard-stops otherwise — an unset or malformed value there means intake was skipped,
not that the evidence is inapplicable. Only an explicit `false` permits omitting
that section from the bundle; an explicit `true` requires a successful read of the
corresponding compact receipt before the reviewer ever dispatches.

Mechanical-lane execution is the same shape starting from "implement" — no planner
job runs. Before Stage 2, the orchestrator instead saves the approved task file's
contract under the *same* artifact name Stage 1 would have used:

```
legwork artifact save --run <label> --name "acceptance-contract-${task_id}.md" <task-file> \
  || { echo "acceptance-contract save failed" >&2; exit 1; }
```

**HARD STOP.** A failed save must never be treated as if the contract now exists —
nothing downstream (Stage 2's dispatch, Stage 4's read) is safe to run until the save
above actually succeeds.

so Stage 4's review-context assembly can always read
`acceptance-contract-${task_id}.md` without branching on lane — only the ledger's
*planned* stage stays explicitly skipped for mechanical (see Flow ledger above).

**Capture a distinct ID for every dispatch** — `planner_job`, `ws`, `impl_job`,
`review_job` below. Never reuse one variable (or `job-N`) across roles: the loop, the
routing, and the hard-stop checks all key off "which job/workspace does this receipt
belong to," and a reused name silently points a later check at the wrong job.

**Every dispatch is followed by an explicit wait, then a routing check, before its
output is trusted.** `legwork wait <job>` (no `--until`) blocks until the job leaves
`queued`/`active` — never read a plan, a diff, or a review verdict off a job that
might still be running. Once it returns:

```
legwork status "$job" --json   # read .state
```

**HARD STOP.** `.state` must be `done` to continue past that stage. Anything else
routes via "The loop" above and stays there — it does not fall through:
`needs-input` → `answer`, `wait` again, re-check; `blocked` → inspect `.blocked.kind`
and handle it (see the `verify`-branch note below for the one case that is itself a
continuation, not a detour); `failed`/`auth-required`/`interrupted` → fix, replan, or
escalate. There is no default path forward from a non-`done` state.

**Stage 1 — plan:**

```
planner_job=$(legwork run --read-only --dir R --run <label> "produce a plan for <task file>")
# --dir R runs in-place in the repo, read-only, so the planner can read the task
# file and the code instead of planning blind in a scratch dir
legwork wait "$planner_job"
legwork status "$planner_job" --json   # HARD STOP: .state must be done, else route and stop here
```

```
plan_tmp="$(umask 077; mktemp)"   # private temp file, never a path inside the repo —
                                   # never redirect an orchestrator artifact into R
legwork result "$planner_job" > "$plan_tmp"
```

**HARD STOP.** Read `$plan_tmp` yourself and judge it fit for purpose before calling it
"approved" — a plan artifact existing is not the same as it being suitable. An
under-specified contract, invented scope, or missing acceptance criteria goes back to
the planner (`legwork resume "$planner_job" "revise: ..."`, wait, re-check `.state`),
never forward to an implementer.

```
legwork note <label> "plan approved: $planner_job"                        # explicit approval receipt
legwork artifact save --run <label> --name "acceptance-contract-${task_id}.md" "$plan_tmp" \
  || { echo "acceptance-contract save failed" >&2; shred -u "$plan_tmp" 2>/dev/null || rm -f "$plan_tmp"; exit 1; }
  # common handoff artifact name, qualified by $task_id — never bare in a shared
  # run/wave; the mechanical lane saves its approved task/acceptance contract under
  # this same qualified name, so Stage 4's review-context assembly is identical
  # regardless of lane
shred -u "$plan_tmp" 2>/dev/null || rm -f "$plan_tmp"                     # cleanup
```

**HARD STOP.** Same as the mechanical lane's save above: a failed save is not a
contract in hand — do not proceed to Stage 2 until this save has actually succeeded.

**Stage 2 — implement:**

```
ws=$(legwork ws new --repo R --json | jq -r .id)
# a workspace job does not automatically see run artifacts — materialize the plan
# into the turn explicitly. Read the exact qualified contract into a checked temp
# file first — never pipe an unchecked `artifact get` straight into dispatch: a
# collision, a stale/missing artifact, or a transient read failure would otherwise
# dispatch the implementer on empty/wrong input with no way to tell after the fact.
contract_tmp="$(umask 077; mktemp)"   # private temp file, never a path inside R
legwork artifact get --run <label> "acceptance-contract-${task_id}.md" > "$contract_tmp" \
  || { echo "implement: acceptance-contract read failed" >&2; rm -f "$contract_tmp"; exit 1; }
[ -s "$contract_tmp" ] \
  || { echo "implement: acceptance-contract empty" >&2; rm -f "$contract_tmp"; exit 1; }
impl_job=$(legwork run --workspace "$ws" --run <label> --append-prompt-file "$contract_tmp" \
  "implement the approved plan") \
  || { echo "implement dispatch failed" >&2; rm -f "$contract_tmp"; exit 1; }
shred -u "$contract_tmp" 2>/dev/null || rm -f "$contract_tmp"
legwork wait "$impl_job"
legwork status "$impl_job" --json   # read .state and, if blocked, .blocked.kind
```

**HARD STOP.** A failed or empty contract read must never fall through to dispatch —
a collision or stale read cannot continue past this point.

**HARD STOP — branch on `.state`, and the two branches below are mutually
exclusive: exactly one applies per attempt, never both, and neither one is skipped
or blanket-routed away.**

- `.state == "blocked"` and `.blocked.kind == "verify"` → **Stage 3a** below. This is
  the implementer's own terminal handoff; it is not "blocked, so escalate" — it is a
  continuation into the exact-job verify path, and *only* that path.
- `.state == "done"` → **Stage 3b** below (no native check receipt exists for an
  ordinary `done` job yet; this is the provisional host-side fallback).
- anything else (`needs-input`, `failed`, `auth-required`, `interrupted`) → route via
  "The loop," then re-enter this stage. Do not proceed to review from here.

**HARD STOP — implemented ledger guard, applies to both branches above.** A
`done`/`blocked:verify` status is necessary but not sufficient — the *implemented*
ledger stage additionally needs a non-empty diff and an implementer result you've
actually read, not just a terminal state:

```
legwork diff "$ws" | wc -l    # HARD STOP: must be non-empty — an empty diff means
                               # nothing to verify or review, regardless of job state
legwork result "$impl_job"    # inspect the compact result: deviations, risks, scope
                               # notes — before trusting Stage 3 to run against it
```

An empty diff, or a result flagging an unresolved deviation/risk/scope question, stops
here — resume the implementer; do not proceed to Stage 3.

```
round_id="$(basename "$(umask 077; mktemp -u)")"   # this verification round's identity,
                                                     # shared by whichever Stage 3 path
                                                     # runs and by Stage 4's review-context
                                                     # artifact name — a fresh Stage 2 entry
                                                     # after a FIX round gets a fresh round_id
```

**Stage 3a — verify (exact-job `blocked.kind=verify` handoff):**

```
verify_json="$(legwork verify "$impl_job" --json -- sh -lc '<suite>')"
  # sh -lc only when shell syntax is actually needed; otherwise pass argv directly,
  # e.g. `-- go test ./...`
verify_receipt_id="$(jq -r '.receipt.receipt_id' <<<"$verify_json")"
  # pins exactly which receipt this round selected — reloaded and re-matched before
  # Stage 4 and again immediately before Stage 5's commit, so a later, unrelated
  # verification on this job can never silently substitute for this one
verified_path=native   # which path ran — Stage 5's landing reload branches on this
```

**HARD STOP.** This records a pass/fail receipt on the job (and mirrors it onto the
workspace's `latest_verification` rollup) without resuming or rewriting the worker. A
failing receipt is not "verified" — treat it exactly like a nonzero exit in Stage 3b
below: it stays attention-worthy, and nothing forward of it (review, commit, close) may
run until a passing receipt exists for the current diff. Its captured `Output` is
capped at 64 KiB and redacted — **a bounded receipt, not a lossless capture.** If full
evidence is required (a large suite, or a failure that needs the complete log), pre-
capture it through Stage 3b's sanitized host-side path first; `legwork verify` alone is
only a bounded receipt of what ran, never the durable evidence store. `$verify_receipt_id`
is this round's pinned identity — the review-context bundle and the landing guard both
re-check it against whatever `latest_verification` holds later, never assume the
current rollup is still this receipt.

**Stage 3b — verify (host-side fallback for an ordinary `done` job, provisional
until `ws check` ships):**

```
tree="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .tree')"
argv=(<suite argv...>)   # direct argv — no shell, no eval. If the suite genuinely
                          # needs shell syntax, record it explicitly instead:
                          # argv=(sh -lc '<suite>')
workdir="$(umask 077; mktemp -d)"   # private, collision-resistant temp dir — never a
                                     # fixed or guessable /tmp path
trap 'rm -rf "$workdir"' EXIT       # cleanup even on early exit/escalation
attempt=1                           # this round's own counter — $round_id (set above,
                                     # before the Stage 3a/3b branch) is what keeps a
                                     # fresh attempt=1 from colliding with an earlier round
```

**Every step in the block below is a hard stop on failure.** A failed redactor,
`artifact save`, digest, or `note` means the verification did not happen — do not
advance past it, and do not treat the diff as verified:

```
raw="$workdir/raw"
start=$(date +%s)
( cd "$tree" && "${argv[@]}" ) >"$raw" 2>&1; ec=$?
dur=$(( $(date +%s) - start ))
sanitized="$workdir/verify-${ws}-${round_id}-attempt${attempt}.log"
<project-redactor> "$raw" >"$sanitized" \
  || { echo "redaction failed; do NOT save or digest $raw" >&2; exit 1; }
# HARD STOP: `artifact save` performs no redaction of its own. If this project has
# no trusted redaction step for this suite's output, stop here and escalate instead
# of running the rest of this block — an unsanitized artifact must not be persisted.
rm -f "$raw"
argv_str="$(printf '%q ' "${argv[@]}")"     # exact argv, shell-safely quoted
legwork artifact save --run <label> --name "$(basename "$sanitized")" "$sanitized" \
  || { echo "artifact save failed" >&2; exit 1; }
log_digest="$(sha256sum "$sanitized" | cut -d' ' -f1)" || { echo "digest failed" >&2; exit 1; }
current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"
```

**A `legwork note` is a 200-rune preview, not a receipt.** Its event text is
truncated at 200 runes — packing exit code, argv, duration, artifact name, and two
digests into one note string (as an earlier version of this recipe did) silently
loses fields past that budget. The **receipt artifact is the source of truth**:
build it as structured JSON with every field, save it immutably under a
create-only, round-and-attempt-qualified name, and let the note be nothing more
than a pointer to it:

```
receipt="$workdir/verify-${ws}-${round_id}-attempt${attempt}.json"
jq -n --arg ws "$ws" --arg round "$round_id" --argjson attempt "$attempt" \
  --argjson passed "$([ "$ec" -eq 0 ] && echo true || echo false)" \
  --argjson exit_code "$ec" --arg argv "$argv_str" --argjson duration_s "$dur" \
  --arg log_artifact "$(basename "$sanitized")" --arg log_sha256 "$log_digest" \
  --arg diff_sha256 "$current_digest" \
  --arg completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{workspace:$ws, round_id:$round, attempt:$attempt, passed:$passed,
    exit_code:$exit_code, argv:$argv, duration_s:$duration_s,
    log_artifact:$log_artifact, log_sha256:$log_sha256, diff_sha256:$diff_sha256,
    completed_at:$completed_at}' > "$receipt" \
  || { echo "receipt build failed" >&2; exit 1; }
receipt_name="$(basename "$receipt")"       # verify-<ws>-<round_id>-attempt<n>.json —
                                             # unique per round+attempt, create-only
legwork artifact save --run <label> --name "$receipt_name" "$receipt" \
  || { echo "receipt save failed" >&2; exit 1; }
legwork note <label> "verify $ws round=$round_id attempt=$attempt receipt=$receipt_name" \
  || { echo "note failed" >&2; exit 1; }   # pointer/narration only — the receipt
                                            # artifact carries every field; later
                                            # shells re-read the artifact, never the
                                            # note text, for any field but the name
verify_receipt="$receipt_name"
```

**HARD STOP.** `$ec` is the deterministic signal — a model's own summary never
overrides it. `$ec == 0` is the only value that reaches Stage 4, and the receipt
artifact's `diff_sha256` is the fallback path's durable record of *which* diff
passed — the verification-freshness gate below, and Stage 5's landing reload,
re-read it back from the artifact, never from in-shell variables carried across a
session boundary. `$ec != 0`:

```
# never feed the implementer `tail`/a raw log excerpt — only a deterministic
# reducer's or disposable distiller's normalized failure summary, plus the
# sanitized artifact pointer:
summary="$(<failure-reducer> "$sanitized")"
legwork resume "$impl_job" \
  "verify attempt $attempt failed (exit=$ec): $summary; artifact $(basename "$sanitized"); fix and I will re-run"
legwork wait "$impl_job"
legwork status "$impl_job" --json   # HARD STOP: branch on .state exactly like Stage 2's
                                     # exit — a resumed implementer can end this turn in
                                     # either terminal shape, and a failed fallback
                                     # attempt never authorizes blindly repeating the
                                     # fallback command against whatever comes back
```

**HARD STOP — branch on `.state`, the same three-way split as Stage 2's exit, never an
unconditional repeat of this stage:**

- `.state == "blocked"` and `.blocked.kind == "verify"` → the resumed fix now
  qualifies for the exact-job handoff. Stop this fallback loop and run **Stage 3a**
  instead — do not keep looping the host-side fallback once the native path is
  available.
- `.state == "done"` → the fallback is still the right path. Bump `attempt` and
  re-run this stage from `raw="$workdir/raw"`:
  ```
  attempt=$((attempt+1))
  ```
- anything else (`needs-input`, `failed`, `auth-required`, `interrupted`) → route via
  "The loop," then re-enter this branch's `resume`/status check once resolved. Never
  fall through to re-running the suite against a job that isn't `done`.

`$workdir` and `$round_id` both stay put for the life of this stage (the trap only
fires when the stage exits), so a bumped `attempt` on the `done` branch always writes a
new `raw`/`sanitized`/receipt triple, never overwrites the last one. Loop the `done`
branch until `$ec == 0`; nonzero never falls through to Stage 4. On the passing
attempt, carry the receipt name forward — it, not any shell variable, is what later
stages re-read:

```
verify_passed=true
verify_digest="$current_digest"   # the digest just recorded in $verify_receipt
verified_path=fallback   # which path ran — Stage 5's landing reload branches on this
```

**Verification freshness gate — required before Stage 4, and re-checked before
Stage 5** (the diff can move between verification, review, and landing; a passing
verification for a diff that has since changed is not verified for its current
state):

```
current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"
```

Branch on `$verified_path` — never guess which one ran, and never mix the two:

- `native` (Stage 3a ran): re-read the workspace's `latest_verification` rollup —
  never trust Stage 3b's shell variables, which weren't even set on this path — and
  require its `receipt_id` to still be the exact receipt Stage 3a selected, not
  merely *a* passing receipt for this job:
  ```
  verif="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .latest_verification')"
  verify_passed="$(jq -r '.passed // false' <<<"$verif")"
  verify_digest="$(jq -r '.diff_sha256 // empty' <<<"$verif")"
  verify_rid="$(jq -r '.receipt_id // empty' <<<"$verif")"
  [ "$verify_rid" = "$verify_receipt_id" ] && identity_ok=true || identity_ok=false
  ```
- `fallback` (Stage 3b ran): re-read the exact receipt artifact this round saved —
  `$verify_receipt` names it; never trust in-shell variables carried across a
  session boundary, and never assume `latest_verification` holds anything (only
  the native path populates that rollup). Identity is already pinned by naming the
  exact artifact to re-read, so there is nothing further to compare:
  ```
  receipt_json="$(legwork artifact get --run <label> "$verify_receipt")"
  verify_passed="$(jq -r '.passed' <<<"$receipt_json")"
  verify_digest="$(jq -r '.diff_sha256' <<<"$receipt_json")"
  identity_ok=true
  ```

**HARD STOP.** Require `verify_passed == "true"` and `verify_digest == "$current_digest"`
and `identity_ok == "true"` before dispatching Stage 4's reviewer, and again —
recomputed — before Stage 5 lands. For the native path, `identity_ok` failing means the
rollup has moved on to a different receipt than the one this round actually passed
(e.g. a later, unrelated verification on the same job) — that is a mismatch exactly
like a digest drift. Any mismatch routes back to Stage 3 for a fresh attempt; never
review or land against a verification whose digest, or whose pinned identity, no
longer matches.

**Stage 4 — review:** `ws review` auto-seeds the exact diff; assemble everything
else into one compact `review-context` artifact first — never the implementer's
self-assessment, never a raw log, never the full native verification receipt object
with its bounded `.output` field, and never the reviewer's job left to fetch these
itself.

**This assembly is fail-closed end to end — every read, every required-evidence
check, and the save are each checked individually, never rolled up into one group's
exit status.** A `{ ...; } > "$ctx_tmp"` block's exit status is only its *last*
command's — if an early read fails but a later, unrelated step (say an optional
branch that's simply not taken) still "succeeds," the group as a whole reports
success and silently ships a reviewer a truncated or wrong bundle. So build each
section into a variable, check its own read/jq/identity result before appending it,
and only then write to `$ctx_tmp`:

```
ctx_tmp="$(umask 077; mktemp)"       # private temp file — never write this into R

contract="$(legwork artifact get --run <label> "acceptance-contract-${task_id}.md")" \
  || { echo "review-context: contract read failed" >&2; exit 1; }
{ echo "## Approved plan / acceptance contract"; echo "$contract"; echo; } >> "$ctx_tmp"

if [ "$verified_path" = native ]; then
  # reload the rollup here too — never trust the freshness gate's variables as
  # still current by the time this stage actually runs
  verif="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .latest_verification')" \
    || { echo "review-context: verification rollup read failed" >&2; exit 1; }
  vrid="$(jq -r '.receipt_id // empty' <<<"$verif")" \
    && vpassed="$(jq -r '.passed // false' <<<"$verif")" \
    && vdigest="$(jq -r '.diff_sha256 // empty' <<<"$verif")" \
    || { echo "review-context: verification rollup unparsable" >&2; exit 1; }
  current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"
  [ "$vrid" = "$verify_receipt_id" ] && [ "$vpassed" = "true" ] \
    && [ "$vdigest" = "$current_digest" ] \
    || { echo "review-context: verification identity/pass/digest check failed" >&2; exit 1; }
  # compact projection only — never `.output`/`.output_truncated`, even though the
  # field is already bounded/redacted; the reviewer gets receipt identity and
  # outcome, not raw command output
  verify_section="$(jq -c '{receipt_id, job, turn, workspace, checkpoint_ref,
    checkpoint_oid, diff_sha256, argv, cwd, passed, exit_code, duration_ms,
    started_at, completed_at}' <<<"$verif")" \
    || { echo "review-context: verification projection failed" >&2; exit 1; }
else
  # the fallback receipt artifact is already this compact shape (no raw output
  # field exists on it — the sanitized log is a separate artifact, referenced by
  # name/digest only), so no further projection is needed here
  receipt_json="$(legwork artifact get --run <label> "$verify_receipt")" \
    || { echo "review-context: fallback receipt read failed" >&2; exit 1; }
  vpassed="$(jq -r '.passed' <<<"$receipt_json")" \
    && vdigest="$(jq -r '.diff_sha256' <<<"$receipt_json")" \
    || { echo "review-context: fallback receipt unparsable" >&2; exit 1; }
  current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"
  [ "$vpassed" = "true" ] && [ "$vdigest" = "$current_digest" ] \
    || { echo "review-context: fallback receipt not passing/current" >&2; exit 1; }
  verify_section="$receipt_json"
fi
{ echo "## Independent deterministic verification receipt"; echo "$verify_section"; echo; } >> "$ctx_tmp"

# $adversarial_required / $ui_verdict_required are mandatory flow-intake
# classifications, set explicitly at intake from the lane and from whether this
# change is UI-visible (see Flow ledger's *intake* bullet) — there is no default;
# an unset or non-boolean value here means intake never classified this flow, and
# that is a hard stop, not an implicit "false":
case "$adversarial_required" in
  true|false) ;;
  *) echo "review-context: adversarial_required unset/invalid at intake" >&2; exit 1 ;;
esac
case "$ui_verdict_required" in
  true|false) ;;
  *) echo "review-context: ui_verdict_required unset/invalid at intake" >&2; exit 1 ;;
esac
# Only an explicit "false" permits omitting that section. An explicit "true"
# requires a successful read of the corresponding compact receipt — a required
# piece of evidence that fails to read is a hard stop, not a silent omission.
if [ "$adversarial_required" = "true" ]; then
  adv="$(legwork artifact get --run <label> "$adversarial_receipt")" \
    || { echo "review-context: required adversarial-test receipt read failed" >&2; exit 1; }
  { echo "## Adversarial-test receipt"; echo "$adv"; echo; } >> "$ctx_tmp"
fi
if [ "$ui_verdict_required" = "true" ]; then
  ui="$(legwork artifact get --run <label> "$ui_verdict_artifact")" \
    || { echo "review-context: required real-browser verdict/evidence read failed" >&2; exit 1; }
  { echo "## Real-browser experience verifier verdict"; echo "$ui"; echo; } >> "$ctx_tmp"
fi

ctx_name="review-context-${task_id}-${round_id}.md"
legwork artifact save --run <label> --name "$ctx_name" "$ctx_tmp" \
  || { echo "review-context save failed" >&2; exit 1; }
review_job=$(legwork ws review "$ws" --agent <different-agent> --model <different-family-model> \
  --append-prompt-file "$ctx_tmp") || { echo "reviewer dispatch failed" >&2; exit 1; }
shred -u "$ctx_tmp" 2>/dev/null || rm -f "$ctx_tmp"
# ws review seeds only the diff by default — the review-context artifact and an
# explicit different agent+model are what make this independent; defaults alone are not
legwork wait "$review_job"
legwork status "$review_job" --json   # HARD STOP: .state must be done, else route and stop here
```

The optional branches above are the exact case the opening paragraph warns about:
an `if` that isn't taken still exits `0`, so it must never be the thing whose exit
status stands in for the required reads before it — that is why every required
read above is checked at the point it happens, not deferred to a trailing group
status.

**HARD STOP — landing reads the *workspace's* review rollup, not just the reviewer
job's own verdict, and that rollup must actually be this review's receipt.** The
workspace save that mirrors a finished reviewer job onto `latest_review` can lag the
job's own terminal `status` by a moment — poll boundedly for the exact receipt instead
of trusting whatever `latest_review` holds the instant `status` says `done`:

```
tries=0
until [ "$tries" -ge 10 ]; do
  review="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .latest_review')"
  rjob="$(jq -r '.job' <<<"$review")"
  [ "$rjob" = "$review_job" ] && break
  tries=$((tries+1))
  sleep 1
done
```

**HARD STOP.** If the loop exhausts without `rjob == "$review_job"`, stop — do not fall
through to the checks below. Whatever `latest_review` currently holds belongs to a
different job (possibly an older SHIP receipt from a prior round, on a since-changed
diff); accepting it would land on a stale receipt, never on the review you just ran.

```
current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"
parsed="$(jq -r '.parsed' <<<"$review")"
rstate="$(jq -r '.state' <<<"$review")"
verdict="$(jq -r '.verdict' <<<"$review")"
receipt_digest="$(jq -r '.diff_sha256' <<<"$review")"
```

Require **all five** to hold before anything downstream runs: `rjob == "$review_job"`
and `parsed == "true"` and `rstate == "done"` and `verdict == "SHIP"` and
`receipt_digest == current_digest`. Any FIX, any malformed/unparsed receipt, any digest
mismatch (diff moved since the review ran), or any receipt not belonging to this
`review_job` routes back to Stage 2 — implement the fix, re-verify (Stage 3, new
attempt), then a fresh Stage 4 review — never straight to Stage 5.

**Stage 5 — land (explicitly guarded, never unconditional).** Time can pass between
Stage 4's checks and this step, so **reload everything fresh right here — never
reuse Stage 4's shell variables**, which describe the tree as of the last poll, not
now:

```
current_digest="$(legwork diff "$ws" | sha256sum | cut -d' ' -f1)"

review="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .latest_review')"
rjob="$(jq -r '.job' <<<"$review")"
parsed="$(jq -r '.parsed' <<<"$review")"
rstate="$(jq -r '.state' <<<"$review")"
verdict="$(jq -r '.verdict' <<<"$review")"
receipt_digest="$(jq -r '.diff_sha256' <<<"$review")"

if [ "$verified_path" = native ]; then
  verif="$(legwork ws ls --json | jq -r --arg ws "$ws" '.[] | select(.id==$ws) | .latest_verification')"
  verify_passed="$(jq -r '.passed // false' <<<"$verif")"
  verify_digest="$(jq -r '.diff_sha256 // empty' <<<"$verif")"
  verify_rid="$(jq -r '.receipt_id // empty' <<<"$verif")"
  [ "$verify_rid" = "$verify_receipt_id" ] && identity_ok=true || identity_ok=false
else
  receipt_json="$(legwork artifact get --run <label> "$verify_receipt")"
  verify_passed="$(jq -r '.passed' <<<"$receipt_json")"
  verify_digest="$(jq -r '.diff_sha256' <<<"$receipt_json")"
  identity_ok=true   # pinned by the exact artifact name re-read above
fi
```

**HARD STOP.** Require `rjob == "$review_job"` (this is *this round's* review, not a
stale prior-round SHIP) and `parsed == "true"` and `rstate == "done"` and
`verdict == "SHIP"` and `receipt_digest == current_digest` and
`verify_passed == "true"` and `verify_digest == current_digest` and
`identity_ok == "true"` (for the native path, `latest_verification` must still be
the exact receipt Stage 3a selected, not a newer, unrelated one) — all eight, all
freshly reloaded, none carried over from Stage 4:

```
if [ "$rjob" = "$review_job" ] && [ "$parsed" = "true" ] && [ "$rstate" = "done" ] \
   && [ "$verdict" = "SHIP" ] && [ "$receipt_digest" = "$current_digest" ] \
   && [ "$verify_passed" = "true" ] && [ "$verify_digest" = "$current_digest" ] \
   && [ "$identity_ok" = "true" ]; then
  legwork ws commit "$ws" -m "..." && legwork close "$ws" --merge-into main
else
  echo "not shippable: review_job_match=$([ "$rjob" = "$review_job" ] && echo yes || echo no) parsed=$parsed state=$rstate verdict=$verdict review_digest_match=$([ "$receipt_digest" = "$current_digest" ] && echo yes || echo no) verify_passed=$verify_passed verify_digest_match=$([ "$verify_digest" = "$current_digest" ] && echo yes || echo no) verify_identity_ok=$identity_ok" >&2
  # do not commit/close — back to Stage 4's routing above
fi
```

**Stage 6 — harvest:** roadmap/task move + friction harvest + `gc`.

**Architectural/high-risk** wraps the same shape with a design phase up front (see
"Design-only pipeline" below) and an adversarial reviewer/tester in place of, or in
addition to, the single independent reviewer: design doc → adversarial design review
→ revise → only then the Normal shape above, with a fresh independent review each
round and an optional second review for security/public-contract/data-integrity work.

### Proportionality before orchestration

The pipeline is a quality multiplier, not a reason to expand every change. Before
writing a task or dispatching a worker, classify the work by user-visible risk and
expected size — that classification *is* picking a lane above. Small, obvious fixes
need a short task, focused tests, and a bounded review; architectural or security work
earns the full design/review loop.

Treat task-agent output as research until the orchestrator approves its scope. If a
one-flag fix becomes a multi-surface metadata system, or the task description takes
longer than the likely implementation, stop and rescope before dispatch. Likewise,
dogfood friction should be recorded as a roadmap item but does not automatically
block the campaign that discovered it. Agents explore and implement; the orchestrator
owns the final task contract and decides which review findings are in scope.

### The campaign shape (running a wave of tasks)

This is the top-level recipe the others slot into. Given N tasks (e.g. a set of
`planning/tasks/*.md` files), run them as one supervised wave:

1. **Preflight once.** `legwork doctor --agent <A> [--model <M>]` for each
   agent+model you will dispatch. A green probe validates auth and model
   acceptance before you spawn anything. Omit `--model` to accept the agent's
   default (see preflight facts below).
2. **Sketch the conflict graph.** Read the task set and list which tasks touch
   the same files or packages. That is the only planning artifact you need —
   overlaps decide landing order, not dispatch order. Save it as a run artifact
   (`artifact save`), don't hand-maintain a table.
3. **One workspace per task; implement in parallel.** `legwork ws new --repo R`
   per task, then `legwork run --workspace ws-N --run <wave> ...` for each. All
   implementers run at once — parallelism is workspaces, and `ws new` is safe to
   call back-to-back (facts below). Dispatch cheap implementers; save the big
   model for review. **Every task gets its own `task_id`** (its task file's slug,
   per task) — a wave shares one `--run <wave>` label across all N workspaces, so a
   bare `acceptance-contract.md`/`review-context-*.md` would collide the moment two
   tasks in the wave save one; qualify every per-task artifact by that task's
   `task_id` (see "Executable flow shapes" above).
4. **Verify outside the sandbox, before review.** Run the repo's suite yourself
   (the orchestrator seat), not in a worker turn — nested real-agent runs and some
   test suites cannot complete inside a worker sandbox, so verification is
   orchestrator-side by construction. For this repo: `gofmt -l . && go vet ./... &&
   go test ./... -count=1`. Deterministic verification comes first because it's the
   cheap, unambiguous gate — no point spending a big-model review pass on a diff
   that doesn't even build or pass its own tests.
5. **Review each diff before trusting it.** `legwork ws review ws-N` per workspace
   (big model, high effort by default), seeded with a compact `review-context`
   bundle — the plan/acceptance contract plus the deterministic verification
   receipt from step 4 (and adversarial-test/UI-verifier evidence when either ran)
   — never the implementer's self-assessment or raw logs (see "Reviewer seeding"
   and Stage 4 of the executable shape above for the exact assembly). This is not
   optional discipline — first-pass SHIP across the corpus was ~3/8; independent
   high-effort review caught real, shippable-looking bugs on ~62% of first passes.
   Route each verdict: `SHIP` → land; `FIX` → `resume` the implementer (or a fresh
   fix job) with the findings, re-verify, then re-review.
6. **Land serially, most-isolated first.** Land the workspaces that touch nothing
   else first, the high-overlap ones last, one at a time:
   `legwork ws commit ws-N -m "..."` then `legwork close ws-N --merge-into main`.
   Conflicts are expected exactly where your conflict graph predicted them —
   resolve them at merge time as orchestrator work (they are usually unions).
   **Re-run the suite on the target branch after every merge**: some conflicts
   are semantic and invisible to git (in the 2026-07-08 wave, one workspace's
   test asserted behavior another workspace's landed policy reversed — caught
   only by the post-merge suite run).
7. **Update the board and clean up.** Move task files / flip your project board
   *after* the branch is truly merged (`close --merge-into` verifies this; a raw
   `git merge` run from inside the workspace tree is a no-op because HEAD there is
   already the branch). Then `legwork gc` to reclaim closed worktrees.

Worked instance (2026-07-08 `next-roadmap` wave): 7 tasks, 4 known file-overlap
pairs, 7 codex implementers in ws-53…ws-59 running in parallel, reviewed one by
one, landed serially most-isolated-first. 2/7 shipped on first review, 5 took one
FIX round each; every conflict landed as a union except the one semantic
collision above.

### Append-prompt: what belongs where

Three channels carry three different things — keep them separate:

- **The prompt** is *only the task pointer* ("implement the task in
  `planning/tasks/foo.md`"). legwork injects the worker contract itself.
- **The task file** carries scope, design, and constraints — the durable "what
  and why" that outlives any single run.
- **`--append-prompt` / `--append-prompt-file`** carries *run-specific policy
  only*: the verification reality of this sandbox, doc conventions, repo
  invariants the worker must not erode.

**The rule with teeth:** run `legwork rules` first and read what the contract
already says. If your append-prompt restates anything in it — commit/push policy,
the `state:` values, how to report `blocked` — delete those lines. A paraphrase
does not reinforce the contract; it competes with it. (This is easy to slip:
in the 2026-07-08 handover a top-tier orchestrator wrote a ~40-line append-prompt
that re-specified commit policy and a full blocked-reporting protocol *minutes
after* acknowledging the no-paraphrase rule. Reading `legwork rules` first is what
prevents it.)

A known-good append-prompt for this repo (note what it does *not* say — nothing
about commits, status blocks, or blocked kinds; the contract owns those):

```text
Verification before claiming done: gofmt -l . && go vet ./... && go test ./... -count=1.
Docs travel in threes: internal/guide/guide.md is canonical — write there first, then
reconcile skills/legwork/SKILL.md and README.md. Keep the three consistent.
Do not erode the DESIGN.md hard rules (no database/daemon; the event schema is a public
interface; the close review tripwire stays).
Delegate any large-output command to a cheap subagent and return only a summary, so your
context stays lean.
```

Multi-line text belongs in a file, not a shell variable:
`--append-prompt-file <path>` (or `-` for stdin) reads it verbatim and stores the
text, sidestepping shell-quoting corruption. Reuse one across a wave by saving it
as a run artifact (see "Grouping and narration").

### Preflight facts (each saves a round of derivation)

- **Model names:** omit `--model` to use the agent's configured default; `doctor`'s
  probe confirms whatever that resolves to. You do not need to discover the exact
  model string before dispatching.
- **Build identity:** verify the binary with `legwork version --json` before filing
  field notes or diagnosing skew; do not infer from a checkout path.
- **Smoke in a subshell:** isolate plumbing tests so state-dir overrides do not leak:
  `( export LEGWORK_STATE_DIR=$(mktemp -d); legwork run --agent fake "test" )`.
- **Model/effort receipt:** when a run depends on a specific model or effort, verify
  the persisted dispatch metadata, not just the command you typed:
  `legwork status <selector> --json` includes `model` and `effort`.
- **`ws new` concurrency:** back-to-back and concurrent `ws new` calls are safe.
  ID allocation is serialized internally by an exclusive lock, so there is no
  duplicate-ID or lost-update risk — no need to serialize creation yourself.
- **Go caches in codex sandboxes are handled:** codex workspace-write turns get a
  per-job writable `TMPDIR`, `GOCACHE`, `GOMODCACHE`, and `GOTMPDIR` automatically.
  Do not inject a `GOCACHE=/tmp/...` override in an append-prompt — it is
  unnecessary and can point at a read-only path.
- **Duplicate skills:** each harness should see only one legwork skill directory.
  Remove old copied installs before adding a symlink or another target path.
- **The ws↔task map is `runs`/`ls` plus an artifact**, not a table you maintain by
  hand — `legwork runs` already rolls the wave up per label.

### Competition: two implementations, keep the winner

When a task is high-stakes or the right approach is genuinely uncertain, dispatch
two implementers on the *same* task into *separate* workspaces (e.g.
`--agent claude` in one, `--agent codex` in another), `ws review` both, pick the
stronger diff, and `close --discard` the loser — optionally grafting one good fix
from the discarded branch first. Evidence: the `presentation` run pitted job-15
(opus/ws-6) against job-16 (codex/ws-7); the orchestrator kept opus and discarded
codex except for one grafted fix. Use sparingly — it doubles implementer cost.

### Design-only pipeline (no code)

For work where the design is the risk, run the whole loop without writing code:

1. `legwork run --read-only --run <label> "produce a design doc for X"` — a
   read-only turn cannot edit, so it can only think and write prose.
2. Save the output as an artifact; `legwork ws review`-style, dispatch an
   *adversarial design review* (`--read-only`) that attacks the design, not code.
3. Route the verdict like any review: revise on findings, re-review, stop when the
   design holds. No workspace, no diff, no merge.

Evidence: `p1-bank-sync` ran design doc → adversarial design review
(`needs-changes`) → revision entirely in read-only turns (job-31 → job-35 →
job-36).

### Reviewer seeding & poisoned-context restart

- **Seed reviewers with the diff, not the tree.** `legwork ws review` already does
  this (it dispatches the reviewer seeded with `legwork diff <ws>` output). If you
  hand-roll a reviewer for any reason, seed it the same way — pipe `legwork diff
  <ws>` into the prompt so it starts from the change under review instead of
  rediscovering it against base.
- **`ws review` seeds only the diff — the rest of the input bundle is not
  automatic.** Assemble one `review-context` artifact (plan/acceptance contract +
  the deterministic verification receipt + adversarial-test receipt when present +
  UI verifier verdict/evidence pointers for UI-visible changes — see the
  Independent reviewer role above for the exact contents and exclusions) and pipe
  it in: `legwork artifact get --run <label> review-context-<task_id>-<round>.md |
  legwork ws review ws-N --append-prompt-file -`; Stage 4 below shows how the
  artifact is built. Independence is not the default either — `ws review --agent`
  defaults to `claude` regardless of which agent the implementer ran on, and
  `--model` defaults to that agent's default model, so a review with no flags at
  all can land on the exact same model the implementer used. Pass explicit
  `--model` for a different model family, and explicit `--agent` when you want a
  different adapter entirely (e.g. implementer on `claude`, reviewer on `codex`) —
  defaults do not guarantee independence.
- **A poisoned context does not recover.** When a job shows high `ctx` and no new
  diff progress, do **not** `resume "keep going"` — that pays to re-read the stuck
  context every turn. `legwork cancel <job>`, then start a **fresh job** re-seeded
  from artifacts (the plan file, `legwork diff` output). Fresh-and-seeded beats
  resume-and-hope every time this pattern appears.

### Note at every phase boundary

Run notes are what make a pipeline legible after the fact — and adoption is
uneven (self-dev runs in the corpus averaged ~1.5 notes/job and read like an
operator journal; sparse runs force you to reconstruct the story from previews).
Drop one `legwork note <run> "..."` at each phase boundary:

- workspace created / task dispatched,
- plan or design approved,
- review verdict (SHIP/FIX + the gist),
- landed (with the merge commit).

Five short notes turn an opaque run into a readable narrative for the next
orchestrator (often future-you) at trivial cost.

## Quick reference

```
doctor [--agent A] [--model M] [--dir R] [--no-probe]   (preflight before dispatch)
rules [--agent A] [--read-only] [--workspace W | --dir D] [--json]
skill install [--target hermes|claude|codex|all] [--force] [--json]
run [--agent A] [--model M] [--workspace W | --dir D] [--read-only]
    [--run L] [--append-prompt P | --append-prompt-file PATH|-]
    [--effort E] [--fallback-model M] <task>
resume <job> <msg>   answer <job> <msg>   approve <job> [--timeout D]
verify <job> [--timeout D] -- <argv...>   cancel <job>
status [selector] [--job ID | --run L]
result [selector] [--job ID | --run L] [--turn N]
wait <job> [--until state[,state...]] [--timeout D] [--json]
ls [--all] [--workspace W] [--run L] [--state S[,S...]] [--limit N] [--json]
watch <job>
events [selector] [--job ID | --run | --workspace] [--since N] [--json]
ack <job> [--force] [--json]
runs                 tail [selector] [--run L | --job J] [-n N] [--full] [--until-idle]
dashboard            serve [--addr 127.0.0.1:0] [--allow-remote]
ws new --repo R      ws ls               ws review <ws> --agent A --model M [--effort high]
                                          [--append-prompt P | --append-prompt-file PATH|-]
                                          (both --agent/--model explicit for an independent reviewer)
ws commit <ws> -m M  diff <ws> [--stat]
close <ws> [--merge-into <branch> [-m <message>]|--merged [--into <ref>] [--force]|--discard|--keep-worktree|--preserve] [--json]
           [--reason TEXT] [--superseded-by ID] [--retention POLICY]
gc [--dry-run] [--close-merged [--close-merged-into <ref>]] [--json]
note <run> <text>
artifact save --run L --name N [--overwrite] <path|->
artifact list --run L          artifact get --run L N
guide
```

Exit code 0 = success; non-zero = the command failed (message on stderr).
Smoke-test any setup without API spend: `legwork run --agent fake "test"`.
