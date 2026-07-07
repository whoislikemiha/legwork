# legwork — orchestrator guide

legwork runs headless coding-agent turns (claude, codex, fake) as supervised
**jobs**: you dispatch a task, the agent works detached, you read structured events
and a final state, you steer with new turns. Locally or over ssh — every command
below works as `ssh host legwork ...`. All verbs take `--json`.

Agents differ; legwork normalizes them, it doesn't pretend they're identical.
`--agent claude` uses a permission mode; `--agent codex` runs in a kernel sandbox
(`--read-only` → codex's read-only sandbox, otherwise workspace-write) and both
fork sessions and run subagents. The loop, states, resume, and status block are
identical across agents. On codex's subscription auth, per-turn cost is nominal
(reported as 0) — watch `context`, not cost.

**Your task prompt is ONLY the task.** legwork itself injects the worker's rules —
the status block contract (`state: done|needs-input|blocked`), ask-early behavior,
no-commit/no-push — into every turn. Do not repeat or paraphrase any of that in your
prompts; a slightly different paraphrase competes with the injected contract. Add
task-specific guidance with `--append-prompt` instead.

## The loop

```
legwork run --agent claude "task"        -> prints job ID immediately
  ... get notified, or poll ...
legwork status <job> --json              -> state decides your next move:
  done         verify it (diff, tests in events), then next phase or close
  needs-input  legwork answer <job> "<decision>"   (same session continues)
  blocked      read status/events; fix the blocker or escalate to the human
  failed       read events; retry as a fresh job or escalate
  auth-required tell the human: agent login needed on this machine (claude /login, codex login)
  interrupted  the turn died mid-flight (crash/cancel); session survives -> resume
legwork resume <job> "next instruction"  -> another turn in the same session
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
events  = ["needs-input", "done", "blocked", "failed", "auth-required", "interrupted"]
```

The payload: `{"event", "job", "run", "agent", "task", "question", "result",
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
legwork resume <job> "review feedback: fix Y"
legwork ws commit ws-N -m "message" --json -> orchestrator commit of the workspace diff
legwork close ws-N --merged|--discard    -> reclaims worktree/branch/refs
```

**You own git history; workers never commit.** The injected contract forbids
worker commits — do not override it in your prompts ("commit when done" turns a
worker into a historian without the bigger picture, and codex's sandbox can't
write the worktree gitdir anyway). Workers produce tree states; legwork
checkpoints them automatically after every turn. When the diff passes review,
*you* commit with `legwork ws commit <ws> -m <message>` — you know what's one
logical change, what's scratch, and what the message should say. The command
stages the workspace tree, refuses empty commits, and records an attributed
`commit` event in the workspace lineage's job/run logs. Then land it.

`close` without a flag refuses if there are unreviewed changes — that's the review
gate. You (or the human) land the diff (PR, merge), then close `--merged`.
`--merged` is verified, not trusted: the branch must actually be an ancestor of
the default branch (or `--into <ref>`), else close refuses — this catches the
classic mistake of running the merge inside the worktree (a no-op) and then
destroying the branch. Merge from the main checkout. `--force` skips the check
for work that landed somewhere legwork can't see (cherry-pick, another remote).
Scratch/research jobs need no workspace: plain `run` gets a scratch dir;
`run --dir <path>` works in-place — combine with `--read-only` for plan/research
turns (harness-enforced: the agent cannot edit).

## Cleanup: close + gc

Two separate acts. `close` **acknowledges** one workspace with a disposition and
reclaims its worktree/branch/refs immediately — you own that call (it's the final
pipeline step after the diff lands). `gc` **reclaims opportunistically**: closed and
provably-orphaned things only, **never unclosed work**.

```
legwork gc                    -> reconcile dead runners, compress/retire transcripts,
                                 sweep orphan refs/worktrees, report orphan branches
legwork gc --dry-run          -> same summary prefixed "would"; mutates nothing
legwork gc --close-merged     -> also close open workspaces whose branch has landed
legwork gc --close-merged --close-merged-into origin/main   -> explicit target ref
```

What gc does: flips dead-runner jobs to `interrupted` (resumable, never deleted);
gzips a finished job's transcript, then deletes it past the retention horizon while
the event index + artifacts persist as the audit trail; prunes stale worktree
registrations and deletes `refs/legwork/*` with no owning workspace. `--close-merged`
(opt-in) closes an open workspace only when its committed branch is an ancestor of the
default branch (`git merge-base --is-ancestor`) and the tree has no uncommitted
changes — dirty or unmerged workspaces are always left for human judgment. gc's blast
radius is strictly what legwork created; repo branches/refs/worktrees are untouchable.

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
legwork events auth-refactor --run      -> merged timeline: jobs + your notes
```

Notes make your reasoning auditable — report decisions as you make them.

## Watching a pipeline

Three read-only surfaces render the same event logs at different zoom levels.
All are strictly read-only; `runs` and `tail` are plain-stdout (work over
`ssh host legwork ...`, no TTY), `dashboard` needs a terminal.

```
legwork runs                 -> one line per run label, rolled up (the overview)
legwork tail                 -> tail -f across all jobs + run logs (the live feed)
legwork dashboard            -> interactive TUI: runs + selected-job + timeline
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

## Quick reference

```
doctor [--agent A] [--model M] [--dir R] [--no-probe]   (preflight before dispatch)
run [--agent A] [--model M] [--workspace W | --dir D] [--read-only]
    [--run L] [--append-prompt P] [--effort E] [--fallback-model M] <task>
resume <job> <msg>   answer <job> <msg>   cancel <job>
status <job>         events <job|run> [--run] [--since N]   ls   watch <job>
runs                 tail [--run L | --job J] [-n N] [--full] [--until-idle]   dashboard
ws new --repo R      ws ls               ws commit <ws> -m M      diff <ws> [--stat]
close <ws> [--merged [--into <ref>] [--force]|--discard|--keep-worktree]
gc [--dry-run] [--close-merged [--close-merged-into <ref>]] [--json]
note <run> <text>    guide
```

Exit code 0 = success; non-zero = the command failed (message on stderr).
Smoke-test any setup without API spend: `legwork run --agent fake "test"`.
