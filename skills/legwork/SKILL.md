---
name: legwork
description: Dispatch and supervise headless coding-agent jobs (Claude Code, Codex) via the legwork CLI — locally or over ssh. Use when delegating coding/research tasks to worker agents, orchestrating plan/implement/review pipelines, checking on running jobs, answering worker questions, reviewing workspace diffs, or when the user mentions legwork.
---

# legwork — orchestrating headless agent workers

legwork runs agent turns as detached **jobs**: dispatch a task, get a job ID
instantly, read structured events, act on the final state. Every command works over
ssh (`ssh host legwork ...`) and takes `--json`. Full built-in reference:
`legwork guide`.

## Rules of engagement

- **Your task prompt is only the task.** legwork injects the worker contract itself
  (status block, ask-early, no commit/push). Never repeat or paraphrase it — use
  `--append-prompt` for task-specific guidance instead.
- **Never trust `done` blindly.** Verify: `legwork diff <ws>` non-empty, test runs
  visible in `legwork events <job>`.
- **Mutating work goes in a workspace.** Plain `run` = scratch dir;
  `--dir` = in-place (combine with `--read-only` for research); `--workspace` = the
  reviewable-diff flow.
- **Pick the agent with `--agent`** (`claude` | `codex`). claude uses a permission
  mode; codex runs in a kernel sandbox (`--read-only` → read-only sandbox, else
  workspace-write). Loop, states, resume, status block are identical. On codex's
  subscription auth, cost is reported as 0 — watch `context` for health.

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
```

Act on `state`:
- `done` — verify, then next phase (`legwork resume "$job" "..."` continues the same
  session; dispatch options — `--read-only`, `--append-prompt`, `--timeout`, model —
  stick for every turn) or close.
- `needs-input` — `legwork answer "$job" "<decision>"`; escalate to the human only
  if it is genuinely their call.
- `blocked` / `failed` — read `legwork events "$job"`; fix and resume, or start a
  fresh job.
- `auth-required` — tell the human to log the agent in on that machine
  (`claude /login`, `codex login`).
- `interrupted` — turn died (crash/cancel); session survives, `resume` continues.

Wake-on-event instead of polling: set `[notify] command` in
`~/.config/legwork/config.toml` to anything that re-invokes you; it receives a JSON
payload (job, event, question, result, context) on stdin. See `legwork guide`.

## Workspace flow (reviewable changes)

```bash
ws=$(legwork ws new --repo ~/code/app --json | jq -r .id)   # worktree + branch;
                                                            # runs workstree init if configured
legwork run --workspace "$ws" --agent claude "implement X"
legwork diff "$ws"                     # changes vs base, incl. untracked
legwork resume <job> "fix review finding Y"                 # same lineage
legwork close "$ws" --merged           # after landing; verified via merge-base
                                       # against the default branch (--into <ref>
                                       # to override); --discard to throw away
```

One active job per workspace; parallelism = multiple workspaces. `close` refuses
unreviewed changes without an explicit disposition — that's the review gate, don't
bypass it reflexively.

## Cleanup: close + gc

`close` acknowledges + reclaims **one** workspace (you own it, after the diff lands).
`gc` reclaims opportunistically — closed/orphaned things only, **never unclosed work**:

```bash
legwork gc                     # reconcile dead runners -> interrupted; compress/retire
                               # transcripts; sweep orphan refs/worktrees (index kept)
legwork gc --dry-run           # same summary, mutates nothing
legwork gc --close-merged      # also close open workspaces whose branch landed in the
                               # default branch (git merge-base --is-ancestor); dirty or
                               # unmerged ones are left for human review
```

gc also auto-runs cheaply on `run`/`resume`/`answer` (git-style, gated ~24h). Its blast
radius is only what legwork created; repo branches/refs/worktrees are never touched.
Config: `[gc]` in `config.toml` (`auto`, `auto_interval`, `transcript_retain`, …).

## Health and recovery

`legwork ls` shows `ctx:145k` per job. High context + stale diff = spinning
worker. Do NOT resume with "keep going": `legwork cancel <job>`, then start a
**fresh job** seeded with the artifacts (plan file, `diff` output). Poisoned context
does not recover.

## Tips

- Group pipeline jobs: `--run <label>`; narrate decisions:
  `legwork note <label> "plan approved, splitting into 2 workspaces"`;
  read the merged timeline: `legwork events <label> --run`.
- Model policy: big model + `--read-only` for plan/review turns; cheaper `--model`
  for mechanical implementation of an approved plan.
- Smoke-test plumbing without API spend: `legwork run --agent fake "test"`.
