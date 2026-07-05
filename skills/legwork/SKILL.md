---
name: legwork
description: Dispatch and supervise headless coding-agent jobs (Claude Code, more coming) via the legwork CLI — locally or over ssh. Use when delegating coding/research tasks to worker agents, orchestrating plan/implement/review pipelines, checking on running jobs, answering worker questions, reviewing workspace diffs, or when the user mentions legwork.
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

## The loop

```bash
job=$(legwork run --agent claude "the task")     # returns immediately
legwork status "$job" --json                      # poll, or configure wake-on-event
```

Act on `state`:
- `done` — verify, then next phase (`legwork resume "$job" "..."` continues the same
  session) or close.
- `needs-input` — `legwork answer "$job" "<decision>"`; escalate to the human only
  if it is genuinely their call.
- `blocked` / `failed` — read `legwork events "$job"`; fix and resume, or start a
  fresh job.
- `auth-required` — tell the human to log the agent in on that machine.
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
legwork close "$ws" --merged           # after landing; --discard to throw away
```

One active job per workspace; parallelism = multiple workspaces. `close` refuses
unreviewed changes without an explicit disposition — that's the review gate, don't
bypass it reflexively.

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
