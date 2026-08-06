# Field notes: the first eval-measured improvement session

Written by the orchestrating agent (Claude, Fable 5) after the 2026-08-06
session that landed and closed [orchestration-eval](../planning/done/orchestration-eval.md)
and [actionable-workspace-status](../planning/done/actionable-workspace-status.md).
`legwork version`: dev, built from source at commit `7f51dcf` (the session's
own head — the tool changed five times while being measured).

This session was unusual: the "workers" were the eval's scripted fakes and
live haiku *orchestrators*, not dispatched implementation agents — so there
are no worker `## Friction` sections to harvest. The friction record for this
session IS [eval/FINDINGS.md](../eval/FINDINGS.md): fourteen findings, each
with a status, most measured before and after a fix. Highlights rather than
repetition:

- **The loop works.** Three measure→fix→re-measure iterations in one session:
  F1–F4 CLI fixes (false-claim/feature-pipeline 2/5 → 4/5), `ws status`
  (verify-gate 3/5 → 5/5, workspace-flow 1/5 → 3/5, verb adopted 9/10 runs
  from skill text alone), failure-injection scenarios (which promptly found
  two harness gaps of their own, F13/F14). Total live spend ~$11. Numbers
  replaced dogfooding intuition exactly as the task hoped.
- **Weak-tier measurement finds product truth intuition missed.** Nobody
  predicted "instructed confirmation beats doc notes" (residual F1) or that
  flag-surface inconsistency was *the* thing drowning multi-step recovery
  (F2, false-claim). Both came straight out of failing runs.
- **Evals rot toward their harness.** Half the findings (F9–F14) were the
  harness lying, not the product or model — spec routing, host config leaks,
  denial conflation, real agents leaking into the sandbox, wrong-job scoring.
  Every hard scenario's first batch found a harness bug before it found a
  product one. Budget for that when reading any new scenario's first numbers.
- **Orchestrator-side friction of the eval itself**: none worth a task. The
  `-baseline` compare removed the last hand-work (diffing two markdown files).
  Remaining wish: a `rescore` mode that re-runs the scorer over an existing
  results dir after a scorer fix (F14 was verified by reasoning instead of
  re-scoring; cheap but should have been free).

Follow-ups filed: [eval-tier-ladder](../planning/tasks/eval-tier-ladder.md),
[eval-codex-orchestrator](../planning/tasks/eval-codex-orchestrator.md).
