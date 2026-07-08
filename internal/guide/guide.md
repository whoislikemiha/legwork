# legwork — orchestrator guide

legwork runs headless coding-agent turns (claude, codex, fake) as supervised
**jobs**: you dispatch a task, the agent works detached, you read structured events
and a final state, you steer with new turns. Locally or over ssh — every command
below works as `ssh host legwork ...`. All verbs take `--json`.

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
`--append-prompt` instead.

## The loop

```
legwork run --agent claude "task"        -> prints job ID immediately
  ... get notified, or poll ...
legwork status <job> --json              -> state decides your next move:
  done         verify it (diff, tests in events), then next phase or close
  needs-input  legwork answer <job> "<decision>"   (same session continues)
  blocked      inspect status.blocked; approve provision, verify outside, or escalate
  failed       read events; retry as a fresh job or escalate
  auth-required tell the human: agent login needed on this machine (claude /login, codex login)
  interrupted  the turn died mid-flight (crash/cancel); session survives -> resume
legwork result <job|run>                 -> print the final report, raw
legwork resume <job> "next instruction"  -> another turn in the same session
legwork approve <job> [--timeout 30m]    -> run approved needs-provision command, then resume
```

Dispatch options stick for the job's lifetime: `--read-only`, `--append-prompt`,
`--timeout`, `--effort`, and the claude-only `--fallback-model` are recorded in
the job and apply to every resumed turn too. The job record also keeps the
original dispatch prompt (`initial_task` once resumed) and the model —
`status --json` reconstructs any job cold. `--effort` (`low|medium|high|xhigh|max`)
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
suite needs an outside-sandbox verification run; attach the result with `resume` or
your run notes. `decision` should be escalated like any other judgment call.

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
events  = ["needs-input", "needs-provision", "done", "blocked", "failed", "auth-required", "interrupted"]
```

The payload: `{"event", "job", "run", "agent", "task", "question", "blocked", "result",
"cost_usd", "context"}` — often enough to decide without another round-trip.

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
legwork ws commit ws-N -m "message" --json -> orchestrator commit, recorded as final_commit
legwork close ws-N --merge-into main     -> no-ff merge locally, then close as merged
legwork close ws-N --merged|--discard [--reason TEXT] [--retention POLICY]
                                         -> records disposition metadata, then drops
                                            the local worktree cache
```

**You own git history; workers never commit.** The injected contract forbids
worker commits — do not override it in your prompts ("commit when done" turns a
worker into a historian without the bigger picture, and codex's sandbox can't
write the worktree gitdir anyway). Workers produce tree states; legwork
checkpoints them automatically after every turn. When the diff passes review,
*you* commit with `legwork ws commit <ws> -m <message>` — you know what's one
logical change, what's scratch, and what the message should say. The command
stages the workspace tree, refuses empty commits, records `final_commit` in
workspace metadata, and writes an attributed `commit` event in the workspace
lineage's job/run logs. Then land it.

`close` without a flag refuses if there are unreviewed changes — that's the review
gate. After review, the usual local landing path is
`legwork close <ws> --merge-into main`: legwork requires the workspace tree to be
committed, switches the source checkout to that local target branch, runs
`git merge --no-ff`, aborts cleanly on conflicts, records `merged_into`, then
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

Independent review is first-class: `legwork ws review <ws>` dispatches a
read-only workspace job seeded with `legwork diff <ws>` output, so the reviewer
starts from the change under review instead of rediscovering it. It defaults to
`--effort high` and the selected agent's default model; pass `--model` for your
configured big reviewer model. The reviewer prompt asks for a structured
`{"verdict":"SHIP|FIX","findings":[...]}` report before the normal status block.
It does not auto-fix, auto-merge, or own the landing decision — route `FIX` with
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

`legwork ls` shows `ctx:145k` per job — the live window of the session's most
recent agent call (what the next call will pay to re-read).
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

Group a pipeline's jobs with `--run <label>`; narrate your decisions:

```
legwork run --run auth-refactor --read-only --agent claude "plan the refactor"
legwork note auth-refactor "plan approved; splitting implement into 2 workspaces"
legwork artifact save --run auth-refactor --name plan.md ./plan.md
legwork artifact list --run auth-refactor
legwork artifact get --run auth-refactor plan.md
legwork events auth-refactor --run      -> merged timeline: jobs + your notes
```

Notes make your reasoning auditable — report decisions as you make them.
Artifacts make your process durable without polluting workspace diffs: plans,
review notes, job/workspace maps, comparison notes, and other orchestration files
belong under the run record. Save from stdin with `-`; use `--overwrite` to replace
an existing artifact deliberately. v1 accepts UTF-8 text/markdown artifacts and
rejects binary data. `artifact save/list/get` support `--json`; `save` records an
`artifact` event in the run log.

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
orchestrator can replace a polling loop with `legwork tail --run L --until-idle`.
`--json` emits the merged events as JSONL (raw event + `job`/`run` provenance).

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

## Quick reference

```
doctor [--agent A] [--model M] [--dir R] [--no-probe]   (preflight before dispatch)
run [--agent A] [--model M] [--workspace W | --dir D] [--read-only]
    [--run L] [--append-prompt P] [--effort E] [--fallback-model M] <task>
resume <job> <msg>   answer <job> <msg>   approve <job> [--timeout D]   cancel <job>
status <job>         result <job|run> [--turn N]            ls   watch <job>
events <job|run> [--run] [--since N]
ack <job> [--force] [--json]
runs                 tail [--run L | --job J] [-n N] [--full] [--until-idle]
dashboard            serve [--addr 127.0.0.1:0] [--allow-remote]
ws new --repo R      ws ls               ws review <ws> [--model M] [--effort high]
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
