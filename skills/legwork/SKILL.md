---
name: legwork
description: Dispatch and supervise headless coding-agent jobs (Claude Code, Codex) via the legwork CLI — locally or over ssh. Use when delegating coding/research tasks to worker agents, orchestrating plan/implement/review pipelines, checking on running jobs, answering worker questions, reviewing workspace diffs, or when the user mentions legwork.
---

# legwork — orchestrating headless agent workers

legwork runs agent turns as detached **jobs**: dispatch a task, get a job ID
instantly, read structured events, act on the final state. Every command works over
ssh (`ssh host legwork ...`) and takes `--json`. Full built-in reference:
`legwork guide`.

When recording field notes or diagnosing version skew, run `legwork version --json`.
It reports version (or `dev`), commit, dirty flag, and date, with Go VCS metadata as
the fallback for ordinary local builds.

## Rules of engagement

- **Your task prompt is only the task.** legwork injects the worker contract itself
  (status block, ask-early, no commit/push, sandbox anti-workaround guard). Never
  repeat or paraphrase it — use `--append-prompt` for task-specific guidance instead,
  or `--append-prompt-file <path|->` for multi-line guidance.
- **Never trust `done` blindly.** Verify: `legwork diff <ws>` non-empty, test runs
  visible in `legwork events <job>`.
- **Mutating work goes in a workspace.** Plain `run` = scratch dir;
  `--dir` = in-place (combine with `--read-only` for research); `--workspace` = the
  reviewable-diff flow.
- **Pick the agent with `--agent`** (`claude` | `codex`). claude uses a permission
  mode; codex runs in a kernel sandbox (`--read-only` → read-only sandbox, else
  workspace-write). Loop, states, resume, status block are identical. On codex's
  subscription auth, cost is reported as 0 — watch `context` for health. Every job
  gets a per-job `TMPDIR`; in codex workspace-write turns it is a writable sandbox
  root with per-job Go cache dirs. Codex read-only has no writable-root exception,
  so temp-writing suites may need workspace-write verification.

## Preflight

Before dispatching into a fresh machine, `legwork doctor` catches a misconfigured
environment (agent not logged in, bad model, unwritable state dir, broken notifier)
up front instead of after a failed turn:

```bash
legwork doctor --agent claude --model <m> --json   # ok:true / exit 0 when healthy
```

The `probe` check runs one real turn (a few tokens) to validate auth + model;
`--no-probe` is static/offline-safe, `--agent fake` probes for free. Exit `1` = a
check failed, `2` = usage error. Checks: state-dir, git, agent, probe, workstree,
notifier — each `ok | warn | fail | skip`.

## The loop

```bash
job=$(legwork run --agent claude "the task")     # returns immediately
legwork status "$job" --json                      # poll, or configure wake-on-event
legwork result "$job"                             # raw final report once done
```

Act on `state`:
- `done` — verify, then next phase (`legwork resume "$job" "..."` continues the same
  session; dispatch options — `--read-only`, `--append-prompt` /
  `--append-prompt-file`, `--timeout`, `--effort` (codex clamps xhigh/max to high),
  `--fallback-model` (claude only), model — stick for every turn),
  `legwork ack "$job"` for reviewed workspace-less jobs, or `legwork close <ws>` for
  workspace jobs.
- `needs-input` — `legwork answer "$job" "<decision>"`; escalate to the human only
  if it is genuinely their call.
- `blocked` — read `legwork status "$job" --json` and inspect `blocked.kind`.
  `provision` means the worker supplied an exact command; run `legwork approve
  "$job"` only when you agree to execute it outside the sandbox; use `--timeout` to
  bound long installs. `verify` means run verification outside the sandbox and attach
  the result with `resume` or a run note. `decision` should be escalated like any
  other judgment call.
- `failed` — read `legwork events "$job"`; fix and resume, or start a fresh job.
- `auth-required` — tell the human to log the agent in on that machine
  (`claude /login`, `codex login`).
- `interrupted` — turn died (crash/cancel); session survives, `resume` continues.

Wake-on-event instead of polling: set `[notify] command` in
`~/.config/legwork/config.toml` to anything that re-invokes you; it receives a JSON
payload (job, event, question, blocked, result, context) on stdin. Subscribe to
`needs-provision` when you want approval gates to wake the orchestrator. See
`legwork guide`.

## Workspace flow (reviewable changes)

```bash
ws=$(legwork ws new --repo ~/code/app --json | jq -r .id)   # worktree + branch;
                                                            # runs workstree init if configured
legwork run --workspace "$ws" --agent claude "implement X"
legwork diff "$ws"                     # changes vs base, incl. untracked
legwork ws review "$ws" --model opus    # independent read-only review of that diff
legwork resume <job> "fix review finding Y"                 # same lineage
legwork ws commit "$ws" -m "message" --json   # records final_commit; refuses empty
legwork close "$ws" --merge-into main  # no-ff merge locally, records closed_at/merged_into
                                       # and closes; --discard throws work away
```

One active job per workspace; parallelism = multiple workspaces. `close` refuses
unreviewed changes without an explicit disposition — that's the review gate, don't
bypass it reflexively.

Use `legwork ws review <ws>` before landing implementer output. It dispatches a
read-only reviewer job attached to the workspace and seeded with `legwork diff <ws>`
(including untracked files), defaults to `--effort high`, and asks for a structured
`SHIP|FIX` verdict with findings. Pass `--model` for the configured big reviewer
model; the agent default is used when `--model` is omitted. The verb does not
auto-fix or auto-merge — you route the verdict.

You own git history — workers never commit (the injected contract forbids it;
don't override with "commit when done"). Review the diff, then use
`legwork ws commit <ws> -m <message>` so the workspace tree is committed and the
decision is recorded in the job/run event logs, then land it with
`legwork close <ws> --merge-into <local-branch>`. It refuses dirty target checkouts,
remote targets, self-merges, and conflicts (aborted cleanly; `--json` distinguishes
`conflict` from `guard-refused`). If work landed some other way, use
`close --merged --into <ref>` for verified acknowledgment.

For dead or superseded work that is still useful for later analysis, record a run
note, commit the final workspace tree with `legwork ws commit`, then close with
explicit metadata:

```bash
legwork close "$ws" --discard \
  --reason "superseded by <replacement>" \
  --superseded-by "<replacement>" \
  --preserve
```

Closed workspace branches are kept locally by default because the branch/commit is
the durable artifact and the checkout is cache. Non-preserved `--discard` deletes
the branch; `--preserve` keeps branch/checkpoint refs for later analysis without
pinning the worktree; `--keep-worktree` keeps the checkout and checkpoint refs.
Push/archive workspace branches only when the orchestrator explicitly decides to
publish them; do not ask worker agents to `git commit` or `git push` directly.

Keep worker prompts scoped to the assigned repo and task. Do not mention
unrelated repositories, workstreams, or things the worker should "ignore" unless
that context is required to complete the task; negative context can make workers
wander toward irrelevant systems.

## Cleanup: ack, close + gc

`ack` acknowledges **one terminal workspace-less job** (planner, reviewer,
read-only check) and stamps the retention anchor. `close` acknowledges **one**
workspace and drops its local worktree cache (you own it, after the diff lands).
`gc` reclaims opportunistically — closed/orphaned things only, **never unclosed
work**:

```bash
legwork ack job-14             # mark reviewed terminal workspace-less job closed
legwork ack job-14 --force     # acknowledge a non-terminal workspace-less job
legwork gc                     # reconcile dead runners -> interrupted; compress/retire
                               # transcripts; sweep orphan refs/worktrees (index kept)
legwork gc --dry-run           # same summary, mutates nothing
legwork gc --close-merged      # also close open workspaces whose branch landed in the
                               # default branch (git merge-base --is-ancestor); dirty or
                               # unmerged ones are left for human review
```

gc also auto-runs cheaply on `run`/`resume`/`answer` (git-style, gated ~24h). Its blast
radius is only what legwork created; non-legwork repo branches/refs/worktrees are never touched.
Config: `[gc]` in `config.toml` (`auto`, `auto_interval`, `transcript_retain`, …).
`ack` and `close` remove each closed job's per-job temp/cache tree while keeping
events, transcripts, and artifacts on the normal retention path.

## Health and recovery

`legwork ls` shows `ctx:145k` per job. High context + stale diff = spinning
worker. Do NOT resume with "keep going": `legwork cancel <job>`, then start a
**fresh job** seeded with the artifacts (plan file, `diff` output). Poisoned context
does not recover. legwork flags the crossing for you: `ls` marks the cell
`ctx:180k!`, `status` prints a `hint:` line, and `--json` sets `context_high`.
Tune the trip point with `[health] context_threshold` in `config.toml` (tokens,
default 150000; `0` disables).

## Watching a pipeline

Four read-only surfaces over the same event logs (`runs`/`tail` are ssh-friendly,
`dashboard` needs a TTY, `serve` is a local browser surface):

```bash
legwork result <job|run>          # raw final report; run resolves to newest job
legwork runs                       # one line per --run label: state rollup, cost,
                                   # ctx health, your latest note (the overview)
legwork tail                       # tail -f across all jobs + run logs, notes
                                   # interleaved; --run/--job scope, --full firehose
legwork tail --run L --until-idle  # blocks, exits 0 when no job in scope is
                                   # active/queued — the scriptable "wait for my pipeline"
legwork dashboard                  # interactive TUI (needs a TTY): attention banner,
                                   # prioritized runs/jobs, detail focus + event scroll
legwork serve                      # local browser console: prints a localhost URL,
                                   # GET-only, live via SSE, no mutation endpoints
```

Prefer `result` over `status --json` surgery when you need the worker's final
report; use `--turn N` for an earlier retained turn. Prefer `runs` over `ls` for
the pipeline view; `tail --until-idle` replaces a status-polling loop; both take
`--json`. Prefer `serve` when a human needs a run-centered browser view during
live multi-agent work. It binds
`127.0.0.1:0` by default; non-loopback `--addr` values require the explicit
`--allow-remote` opt-in because the page exposes local job paths, tasks, events,
and results. The v1 browser is observational: answer, resume, diff, close, and
other mutations stay in the CLI/ssh path.

## Run artifacts

Keep orchestration files out of workspace diffs. Plans, review notes,
job/workspace maps, comparison notes, and process notes belong under the run
record:

```bash
legwork artifact save --run <label> --name plan.md ./plan.md
legwork artifact save --run <label> --name notes.md -     # stdin
legwork artifact list --run <label> --json
legwork artifact get --run <label> plan.md
```

Names are single safe path components; traversal is rejected. Existing artifacts
are not replaced unless `--overwrite` is explicit. v1 stores UTF-8 text/markdown
artifacts and rejects binary data. `artifact save` records an `artifact` event in
the run log, so `tail --run <label>` and `events <label> --run` show when the
record changed.

For long run-specific append prompts, save once as an artifact and reuse it without
shell quoting:

```bash
legwork artifact save --run <label> --name append-prompt.md ./append-prompt.md
legwork artifact get --run <label> append-prompt.md |
  legwork run --run <label> --append-prompt-file - --agent claude "task"
```

## Tips

- Group pipeline jobs: `--run <label>`; narrate decisions:
  `legwork note <label> "plan approved, splitting into 2 workspaces"`;
  watch the merged timeline live with `legwork tail --run <label>` (or the
  snapshot `legwork events <label> --run`).
- Model policy: big model + `--read-only` for plan/review turns; cheaper `--model`
  for mechanical implementation of an approved plan. Dial reasoning with `--effort`
  (`low` for mechanical edits, `high`/`max` for hard design work; codex clamps
  `xhigh`/`max` to its `high` ceiling). On claude, set `--fallback-model` to survive
  overload without failing the turn.
- Smoke-test plumbing without API spend: `legwork run --agent fake "test"`.
