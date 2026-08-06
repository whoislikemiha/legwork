# Orchestration eval harness (Layer 0/1)

Status: landed 2026-08-06 · Priority: P1 · Origin: 2026-08-01 auto-research discussion — measure whether an orchestrator model can drive the surface, and whether harness/doc changes help weaker tiers · Depends: — · Workspace: —

## Goal

Make orchestration quality measurable so surface and guide changes can be judged by
numbers instead of dogfooding intuition. The subject under test is an orchestrator
model driving the real legwork CLI; workers are the scripted fake agent, so a run
costs only orchestrator tokens (Layer 0). The same suite run across model tiers is
the ladder (Layer 1): the weak-tier score is the number to maximize, and it doubles
as the most sensitive interface-complexity detector. Real-worker end-to-end runs
(Layer 2) stay manual dogfooding for now.

## Design

`eval/` package, separate binary (`go build ./eval`), deliberately NOT a legwork verb.
One binary, two roles:

- **Runner** — per (scenario, model): isolated `LEGWORK_STATE_DIR`, optional git
  fixture, spawns the orchestrator (`claude -p` with the legwork skill as appended
  system prompt and Bash-only `legwork` permissions; `cmd:<script>` driver for
  deterministic self-tests and future non-Claude orchestrators), then scores.
- **Shim** — installed as `legwork` on the orchestrator's PATH in front of the real
  binary. Logs every invocation (argv, exit, duration) to `invocations.jsonl` —
  nonzero exits are *fumbles*, the interface-complexity metric — and routes
  `LEGWORK_FAKE_SCRIPT` per invocation so each job gets its scripted turn: resumes
  inherit the invoking command's env, which makes per-turn routing race-free with
  zero cooperation from the orchestrator.

Scoring is deterministic — checks read `meta.json`/`events.jsonl` and the invocation
log; no LLM judges. Metrics per run: check pass/fail, fumble rate (+argv), invocation
count, jobs dispatched, raise→response latency (needs-input/needs-provision →
answer/approve), orchestrator cost/turns/tokens. An optional quiz (resumed session,
regex-scored) probes situational awareness from memory.

v1 scenarios: `happy-path` (dispatch/wait/ack), `clarify` (needs-input answered from
stated constraints), `parallel-streams` (3 concurrent jobs, staggered needs-input +
provision approval, awareness quiz), `workspace-flow` (ws new → run → diff → review →
SHIP → commit → close merged).

## Constraints

- Scoring must stay outside anything the orchestrator can write: the optimizer may
  mutate guide/skill/rules/CLI, never the judge.
- Scenario fixtures follow the e2e suite's stream-json shapes; the eval reads the
  event schema as the public interface it is.
- LLM runs are stochastic: promote guide/surface changes only on repeated runs, and
  prefer weak-tier deltas (strong models paper over bad interfaces).

## Landed (2026-08-01)

Harness + 4 scenarios + scripted-orchestrator self-test. First live haiku run: 4/4
scenarios PASS, ~$0.37 total, 2 fumbles — both product signal: `doctor --agent fake
--json` exits 1 in-sandbox (blunt exit semantics? notifier check?), and `result ws-1`
(the selector-ambiguity trap from AUDIT.md, now measurable).

Token/context reporting landed same day: fresh-in vs cache traffic vs output, peak
context (max single API call) and ctx% of window, permission-denial count, plus
markdown reports with per-run check verdicts, fumble argv, and the full invocation
timeline; raw orchestrator transcripts are kept per run. Findings from the first
instrumented runs: (1) spec routing had to move from first-match to
earliest-match-position — orchestrators write rich task texts that mention sibling
streams; (2) haiku run-to-run variance is real — one parallel-streams run ended
turn 1 asking the user for task details; hardening the preamble's autonomy clause
fixed it (the auto-research loop in miniature: mutate prompt, rerun, compare);
(3) haiku's peak context for 3 supervised parallel streams was ~38k of which ~25k
is harness baseline (CC system prompt + skill) — the orchestration state itself
stayed lean, evidence the two-tier status surfaces do their job.

## Landed (2026-08-01, second pass)

Findings now live in `eval/FINDINGS.md` (product/model/harness split, statuses) —
diff against it when scenarios or models change. Five harder scenarios landed:
`verify-gate` (blocked:verify → legwork verify → receipt → land), `constraint-recall`
(buried-constraint ambiguity trap), `wrong-job-trap` (opposite answers, concurrent),
`context-budget` (8 streams), `feature-pipeline` (plan → implement → review FIX →
relay corrections → re-review SHIP → merge; multi-agent, not 1-job-1-task). Harness
grew answer-branching (the orchestrator's answer selects the worker's next script —
wrong guesses produce detectably wrong outcomes), sequential review scripts
(FIX-then-SHIP), task-text-only spec routing, and max_denials/min_jobs checks.
Live haiku: 8/9 pass; the one stable failure is the F1 landing-confirmation gap
(orchestrators reach for git after close instead of receipts) — product signal.

## Landed (2026-08-06)

First full loop iteration: the F1–F4 product fixes landed and were re-measured
(FINDINGS.md "Re-measure" table). false-claim and feature-pipeline doubled
(2/5 → 4/5); verify-gate 0/5 → 3/5 once the denial check was rescoped
(`no_git_after_close`, F12 — walks the transcript for git-after-close instead
of counting all denials). Residual F1: an *instructed* "confirm it landed"
still routes to git 4/5 despite the receipt being in hand — spontaneous
re-verification is gone, instructed confirmation needs a receipt-shaped surface.

## Open

- ~~Repetition~~ · ~~Landing-proof measurement~~ — landed (F12 turns F1 into a
  choice measurement inside verify-gate/workspace-flow).
- Tier ladder runs (sonnet/opus) + report deltas vs haiku.
- Failure-injection scenarios (mid-turn death, failing provision, double-FIX).
- ~~Failure-injection scenarios~~ — landed 2026-08-06: `failing-provision`
  (3/3 live) and `double-fix` (strict FIX-loop gate; residual failures are the
  F1 rate). Validation surfaced and fixed two harness gaps: fake-agent
  enforcement in the shim (F13) and most-turns job resolution (F14).
  Mid-turn death was already covered by `flaky-worker`.
- ~~Receipt-shaped confirmation surface~~ — landed as `ws status`
  (first slice of actionable-workspace-status): verify-gate 3/5 → 5/5,
  workspace-flow 1/5 → 3/5, verb adopted in 9/10 runs from skill text alone.
  Residual git-after-close is now a tracked rate (2/5), not a structural gap.
- ~~Results history/regression compare~~ — landed as `-baseline <prior run>`:
  aggregates the stored summary.json and appends a Delta table (Δpass in pp,
  fumble/denial medians, cost). The auto-research loop is now
  mutate → `-reps 5 -baseline <prior>` → read one table.
- Codex orchestrator driver via `cmd:` — split to
  [eval-codex-orchestrator.md](eval-codex-orchestrator.md).
- Tier ladder — split to [eval-tier-ladder.md](eval-tier-ladder.md).

## Verdict

Landed 2026-08-06. The harness is complete: runner + PATH shim (with in-sandbox
fake-agent enforcement), deterministic receipt-reading scorer, 15 scenarios
(happy path through messy workers and failure injection), repetition with
rates, `-baseline` regression compare, and `eval/FINDINGS.md` as the
attribution log. It ran three full measure→fix→re-measure loops within its own
task window: the F1–F4 product fixes (false-claim and feature-pipeline 2/5 →
4/5), `ws status` (verify-gate 3/5 → 5/5, workspace-flow 1/5 → 3/5, verb
adopted 9/10 runs), and its own hardening (F9–F14 all found by running it).
Total live spend across every batch: ~$11. The auto-research premise the task
was created to test — that surface/doc changes can be judged by weak-tier
numbers instead of dogfooding intuition — is demonstrated; remaining
extensions (tier ladder, codex orchestrator) are their own tasks.
