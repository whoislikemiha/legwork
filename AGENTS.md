# Working on legwork

legwork is a job-control substrate for headless coding agents. Read
[DESIGN.md](DESIGN.md) before changing architecture — most "why is it like this"
questions are answered there, and [ROADMAP.md](ROADMAP.md) lists what's next plus
**rejected ideas with reasons** (don't re-propose those without new arguments).

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

## Verify before claiming done

```bash
gofmt -l . && go vet ./... && go test ./... -count=1
```

The e2e suite covers detachment, needs-input→answer, mid-turn death, workspaces,
locks, notifier, timeouts — all via the fake agent, zero API spend. When you touch
`internal/adapter`, `internal/runner`, or `internal/rules`, ALSO verify against a
real agent (cheap, ~$0.02):

```bash
export LEGWORK_STATE_DIR=$(mktemp -d)   # never pollute the real state dir
go build -o /tmp/lw . && /tmp/lw run --agent claude --model haiku "Say hello briefly. No tools."
sleep 12 && /tmp/lw status job-1        # expect: state done, sane ctx, no phantom question
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
