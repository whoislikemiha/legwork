# legwork

> Delegate the legwork; keep the judgment.

legwork is a CLI for dispatching and supervising **headless coding-agent jobs**
(Claude Code and Codex) — built so that *another agent* can be the one
driving. An orchestrator (or you) runs tasks as detached jobs, reads structured
events, reviews diffs behind a gate, answers the worker's questions, and closes the
work when it lands. Workspace-less read-only jobs can be acknowledged with `ack`
once reviewed. Locally, or over plain ssh — the CLI is the API.

```console
$ legwork ws new --repo ~/code/myapp
ws-1
$ legwork run --workspace ws-1 --agent claude "add rate limiting to the API, run the tests"
job-7
$ legwork watch job-7          # live events: tool calls, text, checkpoint, finished
$ legwork result job-7         # raw final report
$ legwork diff ws-1            # the reviewable diff (incl. untracked files)
$ legwork answer job-7 "use the token-bucket approach"   # if it asked
$ legwork approve job-7        # if it needs an explicit provisioning command
$ legwork verify job-7 -- go test ./... -count=1  # host-side receipt for blocked.kind=verify
$ legwork ws review ws-1 --model opus    # independent read-only review of the diff
$ legwork ws commit ws-1 -m "add API rate limiting"
$ legwork close ws-1 --merge-into main --reason "landed in main" # merges, records, reclaims
```

## Why

Orchestrating coding agents by scraping their TUIs breaks constantly, and every
agent CLI speaks a different dialect. legwork normalizes them behind one contract:

- **Headless-only**: agents run via their native non-interactive modes
  (`claude -p --output-format stream-json`, `codex exec --json`); readiness is
  process state, results are structured output. No terminal scraping, no tmux
  control, no MCP required.
- **Per-agent, not lowest-common-denominator**: `--agent claude` uses a permission
  mode; `--agent codex` runs in a kernel sandbox (`--read-only` → read-only sandbox,
  else workspace-write) and needs `codex login`. The loop, states, and status-block
  contract are identical. Every job gets a per-job `TMPDIR`; for codex
  workspace-write turns, that temp tree is a writable sandbox root and codex also
  gets per-job Go cache dirs (`GOCACHE`, `GOMODCACHE`, `GOTMPDIR`) so build/test
  caches stay out of reviewed worktrees. Codex read-only has no writable-root
  exception, so temp-writing suites may need workspace-write verification. codex's
  subscription auth reports cost as 0 — watch `context` for health. Agent-specific passthroughs stay explicit: `--effort`
  reaches both claude and codex (codex clamps `xhigh`/`max` to its `high` ceiling),
  while `--fallback-model` is claude-only and rejected for codex rather than dropped.
- **Jobs are detached**: `run` returns an ID immediately; the runner survives your
  ssh session dropping. State is append-only JSONL files you can `tail -f | jq`.
- **Every turn ends in a machine-parsed state**: `done`, `needs-input` (with the
  question), `blocked`, `failed`, `auth-required`. A worker asking a clarifying
  question is a normal turn boundary — `answer` continues the same session.
  Blocked turns can carry `blocked.kind` (`provision`, `verify`, `decision`);
  provision blocks include an exact command and require explicit
  `legwork approve <job>` before legwork runs it outside the sandbox, bounded by
  `--timeout`, and resumes. A workspace job blocked specifically with `verify`
  can instead use `legwork verify <job> -- <argv...>`: argv runs directly in its
  worktree, and the pass/fail receipt is retained without rewriting the worker turn.
- **Workspaces are review gates**: one worktree + one branch + one diff + one close.
  Workers never commit; the orchestrator reviews the diff directly or dispatches
  `legwork ws review <ws>` for a high-effort read-only reviewer seeded with
  the exact checkpointed `legwork diff <ws>` output and asked for a structured
  `SHIP|FIX` verdict. Workspace metadata retains the latest review receipt
  (reviewer job/model, checkpoint, diff digest, verdict, and finding counts);
  malformed JSON is explicit fail-closed, never a guessed `SHIP`. Closed jobs retain
  their last worker outcome too. Then
  the orchestrator runs `legwork ws commit <ws> -m <message>` to make an attributed
  non-empty commit and lands it — `legwork close <ws> --merge-into main` performs
  the local `--no-ff` merge and closes in one guarded step: conflicts abort
  cleanly, dirty target checkouts and self-merges are refused, and `--json`
  distinguishes `conflict` from `guard-refused`. The workspace `meta.json` records
  the final commit and close disposition fields (`closed_at`, `reason`,
  `superseded_by`, `merged_into`, `retention`) plus versioned commit/close
  receipts and an append-only workspace event log for later audit. Bootstrap uses the
  [workstree](https://github.com/whoislikemiha/workstree) convention when the repo
  declares it.
- **Workspace-less jobs can be acknowledged**: `legwork ack <job>` marks a terminal
  planner/reviewer/read-only job closed and stamps the retention anchor. `close`
  stays workspace-only because it also records the disposition and reclaims local
  workspace cache. Both `ack` and workspace `close` best-effort remove each closed
  job's per-job temp/cache tree while preserving events, transcripts, and artifacts.
- **Wake-on-event**: a configurable notifier command receives JSON payloads — point
  it at ntfy for your phone, or at whatever re-invokes your orchestrator.
- **A presentation layer that finds the story**: `runs` rolls a whole pipeline up
  to one line per `--run` label (state, cost, context health, latest note); `tail`
  is `tail -f` across every job and run log, worker events and your notes
  interleaved — `--until-idle` turns it into a scriptable *wait for my pipeline*;
  `result <selector>` prints the worker's final report raw (with `--turn N` for an
  earlier retained turn). The core read commands accept one selector: an exact
  job ID wins, otherwise it is a run label; use `--job` or `--run` to force a
  namespace when they collide (`events` keeps its compatible
  `events <label> --run` boolean form; `events ws-N --workspace` is the separate
  workspace-history scope). `status` and `result` select a run's newest job;
  `events` reads its run event log and `tail` includes its whole timeline,
  including a run with only notes or artifacts. `ls` shows attention/active/unreviewed jobs first,
  hides closed history by default in both human and JSON modes, and takes
  `--all`, `--workspace`, `--run`, `--state`, and `--limit`; `dashboard` is a
  read-only TUI; `serve` is the local live browser console for human operators
  during multi-agent runs. Every surface is a renderer over the same JSONL, so
  they can never disagree.
- **Exact-job waits without polling**: `legwork wait job-7` blocks until that job
  leaves `queued|active`; `--until needs-input,blocked,done` waits for named job
  states, and `--timeout 20m` bounds it. It reloads persisted metadata and
  reconciles dead runners to `interrupted`, returns a concise human line or a
  JSON envelope with final metadata/outcome/elapsed time, and never treats a run
  label as a job ID. Use `tail --until-idle` for a run or pipeline instead.
- **Run artifacts stay out of diffs**: `legwork artifact save/list/get --run <label>`
  stores plans, review notes, job maps, and process notes under the state dir's run
  record, not in repo worktrees. v1 accepts UTF-8 text/markdown artifacts; binary
  blobs are rejected. Long run-specific append prompts can be stored once as an
  artifact and piped back into dispatch with `--append-prompt-file -`, avoiding
  multi-line shell quoting.
- **Context as the health metric**: `ls` shows each non-closed session's context
  footprint (`ctx:145k`) — the early-warning signal for a worker spinning in
  circles. Once a job crosses a threshold, `ls` marks it `ctx:180k!` and `status`
  prints a `hint:` line (`--json`: `context_high`) — the built-in cue to start
  fresh over resume. Tune it with `[health] context_threshold` in `config.toml`
  (default 150000; `0` disables).
- **Reclamation without a daemon**: `close` records disposition/retention metadata
  in one close receipt (including actor, target, and final commit), appends a
  workspace-history event, and drops the local worktree cache; branches are kept by default as the durable
  artifact. `--preserve` records `retention=preserve` and keeps branch/checkpoint
  refs for analysis; `--keep-worktree` keeps the checkout and checkpoint refs.
  `gc` compresses/retires transcripts and sweeps orphans (dead runners, stale
  worktrees, refs with no workspace); opt-in `--close-merged` auto-closes landed
  work without deleting its branch. Runs opportunistically on dispatch; unclosed
  work is never touched.
- **Workspace event history**: `legwork events ws-N --workspace` reads that
  workspace's own append-only commit/close index with the normal `--since` cursor
  and `--json` output. If history recording fails after a commit or close is
  durable, the successful receipt/output includes `history_error`; do not replay
  the non-idempotent operation.

## Install

```console
$ go install github.com/whoislikemiha/legwork@latest
```

Binary releases use `install.sh` and install to `~/.local/bin` by default:

```console
$ curl -fsSL https://raw.githubusercontent.com/whoislikemiha/legwork/main/install.sh | sh
```

Only the machine *running jobs* needs legwork — from anywhere else,
`ssh host legwork ...`.

## For orchestrators (agents)

Everything an agent needs to drive legwork is in the built-in guide:

```console
$ legwork guide
```

It covers the run→observe→steer loop, hooking notifications up as your wake-up
signal, workspace review flow, health recipes (spotting and recovering a
poisoned-context worker), and orchestration recipes — the multi-task campaign
shape (parallel implement, serial land), a proportionality gate for keeping small
fixes small, append-prompt norms, and the
competition/design-only patterns. Preflight a machine before dispatching with
`legwork doctor` (agent binary, auth, model, state dir, notifier — machine-readable,
stable exit codes). Record `legwork version --json` in field notes when build
identity matters; it prints version (or `dev`), commit, dirty flag, and date.
Smoke-test any setup without API spend in a subshell so state-dir overrides do not leak:
`( export LEGWORK_STATE_DIR=$(mktemp -d); legwork run --agent fake "test" )`.
When model or effort matters, verify the receipt with
`legwork status <selector> --json` and check `model`/`effort`.

A loadable skill for agent harnesses ships at
[`skills/legwork/SKILL.md`](skills/legwork/SKILL.md), and release binaries embed
that exact file. Install or update it noninteractively:

```console
$ legwork skill install --target hermes   # ~/.hermes/skills/legwork/SKILL.md
$ legwork skill install --target claude   # ~/.claude/skills/legwork/SKILL.md
$ legwork skill install --target codex    # ~/.codex/skills/legwork/SKILL.md
$ legwork skill install --target all --json
```

`install.sh` best-effort installs the skill for detected harnesses on `PATH`
without clobbering modified skills or failing the binary install on a skill
conflict. Identical content is a no-op; differing content requires `--force`,
which writes backups under `~/.local/share/legwork/skill-backups/<target>/`
outside harness-scanned paths. Hermes users who normally use `skills.sh` can use
`legwork skill install --target hermes` as the update step. For local repo
development, symlink the harness skill directory to `skills/legwork`, keep only
one legwork skill visible to each harness, and restart/reload sessions after
updates.

One rule worth knowing before the guide: **your task prompt is only the task** —
legwork injects the worker contract (status block, ask-early, no commit/push,
sandbox anti-workaround guard) itself; don't repeat it in prompts. Run
`legwork rules` to inspect the exact contract, then use `--append-prompt` for
short task-specific additions, or `--append-prompt-file <path|->` for
multi-line UTF-8 text from a file/stdin.

## Status

Early. Implemented: jobs, detached runner, claude + codex + fake adapters, status-block
contract, workspaces/checkpoints/diff/review/commit/close, runs + narration/artifacts,
the `runs`/`tail`/`dashboard`/`serve` presentation layer, notifier, context tracking,
structured blocked reasons, needs-provision approval, job `result`/`wait`/`ack`, timeouts,
`doctor` preflight, `rules`, `gc` reclamation, `guide` + `skill install`. What's next lives in
[planning/ROADMAP.md](planning/ROADMAP.md) (the work board — one task per file, plus rejected
ideas and why); the full design rationale in [DESIGN.md](DESIGN.md).

## License

MIT
