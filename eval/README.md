# Orchestration eval harness

Measures an orchestrator model driving the real legwork CLI against scripted fake
workers. Design, metrics, and open items: [planning/tasks/orchestration-eval.md](../planning/tasks/orchestration-eval.md).

```bash
# harness self-test, zero spend (scripted orchestrator, deterministic)
go build -o /tmp/legwork-eval ./eval
/tmp/legwork-eval -orchestrator cmd:eval/testdata/scripted-orchestrator.sh -results /tmp/eval-offline

# live: one scenario, one tier (~$0.07)
/tmp/legwork-eval -only clarify -models haiku

# rates, not pass/fail: 5 reps each, 3 sandboxes in parallel
/tmp/legwork-eval -reps 5 -parallel 3 -models haiku

# the ladder: full suite across tiers
/tmp/legwork-eval -models haiku,sonnet,opus

# the loop: diff a mutation against a prior run's summary.json
/tmp/legwork-eval -reps 5 -baseline eval/results/<prior-runid>
```

Findings from runs are tracked in [FINDINGS.md](FINDINGS.md) — diff new results
against it to attribute regressions to harness vs product vs model.

Run from the repo root (or pass `-repo`). Results land under `eval/results/<runid>/`
(gitignored): `result.json` per run, `summary.{json,md}` per suite, plus each run's
full state dir, invocation log, and job mapping for post-hoc digging.

Scenario = a dir under `eval/scenarios/<name>/` with `scenario.json` (goal prompt,
job specs, checks, optional quiz) and fake-agent turn scripts in the e2e suite's
stream-json dialect. The scorer is deterministic; scoring inputs are outside
anything the orchestrator can write.
