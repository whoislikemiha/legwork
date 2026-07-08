# Working on legwork

legwork is a job-control substrate for headless coding agents. Read
[DESIGN.md](DESIGN.md) before changing architecture — most "why is it like this"
questions are answered there, and [planning/ROADMAP.md](planning/ROADMAP.md) is the
live work board — what's next (one task per file in [planning/tasks/](planning/tasks/))
plus **rejected ideas with reasons** (don't re-propose those without new arguments). The
2026-07-08 dogfood review that seeded the current open items is frozen in
[planning/AUDIT.md](planning/AUDIT.md).

## Dogfooding — you are the user

legwork is developed by running legwork on itself: agents reading this file are the
target users of the surface they're changing. Every rough edge you hit while doing
your job is product signal — the current roadmap was largely seeded this way (see
[planning/AUDIT.md](planning/AUDIT.md)). The two roles touch different surfaces:
**orchestrators use the CLI; workers never do** — a worker lives inside the contract
legwork wraps around it (the sandbox, the injected rules, the status protocol, how
its final report is treated), and that surface is just as much the product.

So while you work, notice friction. Orchestrator-side: a verb you wished existed, a
status that lied to you, output you had to parse by hand, a wait you had to hand-roll.
Worker-side: a sandbox limit that blocked legitimate work, a rule that pushed you
toward a workaround, no way to signal the state you were actually in. Don't silently
absorb it, and don't fix it mid-task (scope stays with your task) — capture it:

- **Workers**: append a short `## Friction` section to your own task file (the one
  file in `planning/` you may write) and/or put it in your final report. One or two
  lines per item: what you were doing, what got in the way, what you wished the tool
  did instead.
- **Orchestrators**: harvest `## Friction` sections when landing each task (you're
  reading the task file to append the verdict anyway — don't let friction get archived
  unread in `done/`), fold it plus your own into dated field notes
  (`docs/field-notes-YYYY-MM-DD.md` — the 2026-07-07 one shows the shape), then turn
  durable items into `planning/tasks/` entries via the ROADMAP. Note the `legwork
  version` output in the field notes — the tool changes in parallel with use, so
  friction is only triageable against the build that produced it.

Before proposing, check ROADMAP's rejected ideas — don't re-file those without a new
argument. "Nothing to report" is a fine outcome; invented feedback is worse than none.

## Layout

- `main.go`, `workspace_cmds.go` — thin cobra wiring only; logic lives in `internal/`
- `internal/job` — job store, meta, liveness; `internal/events` — the versioned
  JSONL event index; `internal/adapter` — agent normalization (claude, fake) +
  status-block parser; `internal/runner` — the detached runner (`_runner`);
  `internal/workspace` — worktrees, checkpoints, diff, close;
  `internal/rules` — injected worker rules; `internal/notify` — notifier;
  `internal/guide` — embedded orchestrator guide; `internal/fakeagent` — scripted
  test agent behind `_fake-agent`
- `test/` — e2e contract suite: builds the real binary, drives it like an
  orchestrator would, fake agent behind it
- `skills/legwork/SKILL.md` — loadable skill for agent harnesses
- `planning/` — the work board: `ROADMAP.md` (source of truth; orchestrator is its only
  writer), one task per file in `tasks/` (goal/design/constraints/blockers — dispatch a
  worker with "read the task file"), landed tasks move to `done/`, frozen origin in `AUDIT.md`

## Verify before claiming done

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
```

The e2e suite covers detachment, needs-input→answer, mid-turn death, workspaces,
locks, notifier, timeouts — all via the fake agent, zero API spend. When you touch
`internal/adapter`, `internal/runner`, or `internal/rules`, ALSO verify against a
real agent (cheap, ~$0.02):

```bash
# subshell: the state-dir override must not leak into later legwork calls
(
  export LEGWORK_STATE_DIR=$(mktemp -d)   # never pollute the real state dir
  # task-shaped prompt: a bare greeting makes haiku ask "what's the task?" -> needs-input
  go build -o /tmp/lw . && /tmp/lw run --agent claude --model haiku "Reply with exactly the word PLUMBING-OK. No tools."
  sleep 15 && /tmp/lw status job-1        # expect: state done, result PLUMBING-OK, sane ctx
)
```

For codex (needs `codex login`; cost is 0 on subscription → check `context`):

```bash
(
  export LEGWORK_STATE_DIR=$(mktemp -d)
  go build -o /tmp/lw . && /tmp/lw doctor --agent codex   # auth guard for codex
  /tmp/lw run --agent codex "Reply with exactly the word PLUMBING-OK. No tools."
  sleep 20 && /tmp/lw status job-1        # expect: state done, PLUMBING-OK, context>0
)
```

## Hard rules (from DESIGN.md — do not erode)

- **No database, no daemon.** State is files; jobs survive via the detached runner.
- **The event schema is a public interface.** New event types are fine; changing
  existing fields needs a `v` bump and a migration story.
- **The status-block contract and its parser version together** — if you change
  `internal/rules`, check `internal/adapter`'s parser (and vice versa).
- **Missing status block → `blocked`, never `done`.** Safety direction is fixed.
- **`close` tripwire stays**: unreviewed changes require an explicit disposition.
- **gc/close blast radius**: only things legwork created (its worktrees, its
  `legwork/*` branches, `refs/legwork/*`, its state dir). Never repo content.
- **Injected worker rules are tool-owned**: orchestrators add via
  `--append-prompt`, never by paraphrasing the contract.

## Conventions

- Docs travel in threes: a behavior change usually touches
  `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md` — keep
  them consistent (the guide is the canonical text).
- New verbs: `--json` support, stable exit codes, never interactively prompt.
- Commit messages: plain, no Co-Authored-By or other trailers.
- Do not tag releases or publish packages unless explicitly asked.
- CI must be green before tagging; releases go through goreleaser on `v*` tags.
