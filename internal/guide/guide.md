# legwork — orchestrator guide

legwork runs headless coding-agent turns (claude, fake; codex planned) as supervised
**jobs**: you dispatch a task, the agent works detached, you read structured events
and a final state, you steer with new turns. Locally or over ssh — every command
below works as `ssh host legwork ...`. All verbs take `--json`.

## The loop

```
legwork run --agent claude "task"        -> prints job ID immediately
  ... get notified, or poll ...
legwork status <job> --json              -> state decides your next move:
  done         verify it (diff, tests in events), then next phase or close
  needs-input  legwork answer <job> "<decision>"   (same session continues)
  blocked      read status/events; fix the blocker or escalate to the human
  failed       read events; retry as a fresh job or escalate
  auth-required tell the human: agent login needed on this machine (e.g. claude /login)
  interrupted  the turn died mid-flight (crash/cancel); session survives -> resume
legwork resume <job> "next instruction"  -> another turn in the same session
```

Never trust `done` blindly: verify the diff is non-empty and tests ran (visible as
tool-call events) before building on it. A missing/unparseable status block surfaces
as `blocked` — treat it as needs-review, not failure.

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
Scratch/research jobs need no workspace: plain `run` gets a scratch dir;
`run --dir <path>` works in-place — combine with `--read-only` for plan/research
turns (harness-enforced: the agent cannot edit).

## Health: watch context, not cost

`legwork ls` shows `ctx:145k(72%)` per job — the session's context footprint.
High context + no new diff progress = a spinning worker. The fix is NOT
`resume "keep going"`: cancel, then start a **fresh job** seeded with the artifacts
(the plan file, `legwork diff` output) — a poisoned context does not recover.
Costs are also tracked (`status`), cumulative per session.

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
run [--agent A] [--model M] [--workspace W | --dir D] [--read-only]
    [--run L] [--append-prompt P] <task>
resume <job> <msg>   answer <job> <msg>   cancel <job>
status <job>         events <job|run> [--run] [--since N]   ls   watch <job>
ws new --repo R      ws ls               diff <ws> [--stat]
close <ws> [--merged|--discard|--keep-worktree]
note <run> <text>    guide
```

Exit code 0 = success; non-zero = the command failed (message on stderr).
Smoke-test any setup without API spend: `legwork run --agent fake "test"`.
