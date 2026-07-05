# legwork

> Delegate the legwork; keep the judgment.

legwork is a CLI for dispatching and supervising **headless coding-agent jobs**
(Claude Code today, Codex planned) — built so that *another agent* can be the one
driving. An orchestrator (or you) runs tasks as detached jobs, reads structured
events, reviews diffs behind a gate, answers the worker's questions, and closes the
work when it lands. Locally, or over plain ssh — the CLI is the API.

```console
$ legwork ws new --repo ~/code/myapp
ws-1
$ legwork run --workspace ws-1 --agent claude "add rate limiting to the API, run the tests"
job-7
$ legwork watch job-7          # live events: tool calls, text, checkpoint, finished
$ legwork diff ws-1            # the reviewable diff (incl. untracked files)
$ legwork answer job-7 "use the token-bucket approach"   # if it asked
$ legwork close ws-1 --merged  # after you land it; reclaims worktree/branch/refs
```

## Why

Orchestrating coding agents by scraping their TUIs breaks constantly, and every
agent CLI speaks a different dialect. legwork normalizes them behind one contract:

- **Headless-only**: agents run via their native non-interactive modes
  (`claude -p --output-format stream-json`); readiness is process state, results are
  structured output. No terminal scraping, no tmux control, no MCP required.
- **Jobs are detached**: `run` returns an ID immediately; the runner survives your
  ssh session dropping. State is append-only JSONL files you can `tail -f | jq`.
- **Every turn ends in a machine-parsed state**: `done`, `needs-input` (with the
  question), `blocked`, `failed`, `auth-required`. A worker asking a clarifying
  question is a normal turn boundary — `answer` continues the same session.
- **Workspaces are review gates**: one worktree + one branch + one diff + one close.
  Closing a workspace with unreviewed changes requires an explicit decision.
  Bootstrap uses the [workstree](https://github.com/whoislikemiha/workstree)
  convention when the repo declares it.
- **Wake-on-event**: a configurable notifier command receives JSON payloads — point
  it at ntfy for your phone, or at whatever re-invokes your orchestrator.
- **Context as the health metric**: `ls` shows each session's context footprint
  (`ctx:145k`) — the early-warning signal for a worker spinning in circles.

## Install

```console
$ go install github.com/whoislikemiha/legwork@latest
```

Binary releases (curl installer) coming with v0.1. Only the machine *running jobs*
needs legwork — from anywhere else, `ssh host legwork ...`.

## For orchestrators (agents)

Everything an agent needs to drive legwork is in the built-in guide:

```console
$ legwork guide
```

It covers the run→observe→steer loop, hooking notifications up as your wake-up
signal, workspace review flow, and health recipes (spotting and recovering a
poisoned-context worker). Smoke-test any setup without API spend:
`legwork run --agent fake "test"`.

A loadable skill for agent harnesses ships at
[`skills/legwork/SKILL.md`](skills/legwork/SKILL.md) — for Claude Code:

```console
$ mkdir -p ~/.claude/skills/legwork
$ curl -fsSL https://raw.githubusercontent.com/whoislikemiha/legwork/main/skills/legwork/SKILL.md \
    -o ~/.claude/skills/legwork/SKILL.md
```

One rule worth knowing before the guide: **your task prompt is only the task** —
legwork injects the worker contract (status block, ask-early, no commit/push)
itself; don't repeat it in prompts.

## Status

Early. Implemented: jobs, detached runner, claude + fake adapters, status-block
contract, workspaces/checkpoints/diff/close, runs + narration, notifier, context
tracking, `guide`. Planned (see [DESIGN.md](DESIGN.md)): codex adapter, approval
gates, session fork/interrogation, `gc`, terminal dashboard, read-only web UI,
concurrency limits.

## License

MIT
