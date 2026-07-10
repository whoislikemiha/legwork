# Truthful health signal (codex ctx + mid-turn + diff-progress)

Status: next · Priority: P1 · Origin: AUDIT B1–B2 (+ prior ROADMAP "mid-turn cost/context", ctx-hint follow-up) · Depends: — · Workspace: —

## Goal

Make the health surface tell the truth. Three linked problems: codex `ctx` is inflated garbage,
running jobs show `ctx:-` (no live signal), and there is no agent-agnostic "is it spinning?"
metric. Fix the lie, add a heartbeat, and add a diff-progress signal.

## Context & design

- **B1 codex ctx inflation.** codex `ctx` median **1.44M**, max **30.7M** (`job-78`); 63/66
  codex jobs exceed the 150k `context_threshold`, so the `!` marker and `context_high` json fire
  on nearly every codex job — including this review's own pure-read research jobs (431k–1056k). It
  is summed across a turn's calls, not the live window: `job-78`/`job-33` are 3-turn jobs with
  ~30M. The claude summing bug was already fixed to "last call's real window"; do the same for
  codex, or if the codex stream genuinely doesn't expose a live window, **suppress `!`/`context_high`
  for codex** rather than emit a known-false alarm.
- **B2a mid-turn signal.** Telemetry only lands at the turn boundary, so a $13 / runaway turn is
  invisible until it ends and running jobs show `ctx:-`. Add a coarse heartbeat (tokens-so-far
  from the transcript tee the runner already writes) to `status`/`ls` while a runner is live.
  Read-side only — cheaper than the mid-turn toolbelt.
- **B2b diff-progress = the real health metric.** Bytes/files changed in the worktree over the
  last N minutes, shown in `ls`. High tool activity + zero diff-progress = spinning, and it is
  agent-agnostic (no dependence on either CLI's ctx accounting). The runner already snapshots the
  worktree for checkpoints — derive progress from consecutive checkpoint trees.

## Constraints

- Do not change the event schema's existing telemetry fields without a `v` bump; prefer adding new
  fields (heartbeat, diff-progress) over redefining `context`.
- The codex ctx fix must be verified against a **live multi-call codex turn** (AGENTS.md: the
  codex adapter's ctx "may have the same summing issue — verify before changing"). Fake agent
  can't reproduce real ctx accounting.
- Keep `[health] context_threshold` config working; `0` disables. If suppressing `!` on codex,
  document it in the capability flags.

## Blockers

None. Sub-items are independent: ctx fix (adapter), heartbeat (runner+status, read-only),
diff-progress (runner checkpoints + ls). Ship in that rough order of leverage.

## Log
