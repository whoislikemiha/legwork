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
and `--timeout` are recorded in the job and apply to every resumed turn too.
The job record also keeps the original dispatch prompt (`initial_task` once
resumed) and the model — `status --json` reconstructs any job cold.

Never trust `done` blindly: verify the diff is non-empty and tests ran (visible as
tool-call events) before building on it. A missing/unparseable status block surfaces
as `blocked` — treat it as needs-review, not failure.

## Preflight: doctor before you dispatch

A misconfigured machine (agent not logged in, bad model name, unwritable state dir,
broken notifier) otherwise only surfaces *after* a job is spawned and its turn fails.
`legwork doctor` moves that discovery up front — run it once before dispatching:

```
legwork doctor [--agent claude] [--model <m>] [--dir <repo>] [--no-probe] [--json]
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
legwork close ws-N --merged|--discard    -> reclaims worktree/branch/refs
```

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

## Quick reference

```
doctor [--agent A] [--model M] [--dir R] [--no-probe]   (preflight before dispatch)
run [--agent A] [--model M] [--workspace W | --dir D] [--read-only]
    [--run L] [--append-prompt P] <task>
resume <job> <msg>   answer <job> <msg>   cancel <job>
status <job>         events <job|run> [--run] [--since N]   ls   watch <job>
ws new --repo R      ws ls               diff <ws> [--stat]
close <ws> [--merged [--into <ref>] [--force]|--discard|--keep-worktree]
gc [--dry-run] [--close-merged [--close-merged-into <ref>]] [--json]
note <run> <text>    guide
```

Exit code 0 = success; non-zero = the command failed (message on stderr).
Smoke-test any setup without API spend: `legwork run --agent fake "test"`.
