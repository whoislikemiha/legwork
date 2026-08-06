# Eval findings log

Running log of what the orchestration eval has surfaced. One entry per finding,
newest state wins; flip Status when a change lands and note what changed it. The
point: when scenarios get harder or models change, diff against this file to know
whether a regression is the harness, the product, or the model.

Statuses: **open-product** (legwork should change) · **open-model** (orchestrator
behavior to track) · **fixed-harness** (eval bug, resolved) · **observation**
(baseline fact, no action).

## Product findings (legwork)

### F1 · Landing confirmation gap — orchestrators reach for git after close
Status: **open-product, fix landed 2026-08-06 (pending re-measure)** · seen: every
live workspace run (2026-08-01, haiku) · Fix: close now prints the landing proof
(landed target, merge commit, receipt ID) in human output, and the guide/skill
state explicitly that the close receipt IS the proof — no git check needed.
Re-run verify-gate/workspace-flow to confirm the denial count drops.

After `close --merge-into main`, haiku consistently tries `git log` / `git status`
to confirm the change landed (4+ denied attempts per run in verify-gate, also seen
in workspace-flow). Legwork *has* the answer — close receipts record disposition
and final commit — but orchestrators don't reach for them. Either the guide
undersells receipts or a "prove the close" surface is missing (relates to
`planning/tasks/actionable-workspace-status.md`). Caveat: the eval permits ONLY
legwork commands, stricter than real use where the orchestrator can run git — but
that strictness is what exposed the reach.

Also seen pre-dispatch: `ls`/`find` exploration of the repo before creating the
workspace. That one is arguably out of legwork's scope by design; not actionable.

### F2 · Flag-surface inconsistencies fumble real orchestrators
Status: **open-product, fix landed 2026-08-06 (pending re-measure)** · seen:
feature-pipeline + verify-gate live runs (haiku); frequency confirmed by the
5-rep baseline (feature-pipeline: fumbles in 5/5 reps) · Fix: `resume`/`answer`
accept the read-side selector surface (`--job`/`--run`, run → newest job);
`diff --json`; `runs --run <label>` filter; `ws list` aliases `ws ls`; unknown
dispatch-time flags on resume/answer/approve error with "set at dispatch" hints.
Multiline `commit -m` needed no fix — the message passes through to `git commit
-m` verbatim; the observed fumble was orchestrator-side shell quoting.
Re-run feature-pipeline/false-claim to measure the fumble-rate delta.

The pattern: orchestrators assume dispatch-time flags work on every verb.
Observed nonzero-exit guesses: `resume --run/--agent/--append-prompt/--json`,
`runs --run <label>`, `ws review --model fake` (meant --agent), `diff --json`,
multiline `commit -m`. Each is a wasted turn + context. Candidates: accept the
orthogonal flags where meaningful (--run on resume/runs at least), add --json to
diff, error messages that suggest the right form. Related trap: `ack` refused on
a workspace-attached reviewer job — the ack/close terminal-verb split predicted
by AUDIT.md, now observed live.

### F3 · Selector ambiguity is real at the weak tier
Status: **open-product, fix landed 2026-08-06 (pending re-measure)** · seen:
first live run (haiku, workspace-flow)

`result ws-1` — a workspace ID passed to a job/run selector command, exactly the
trap predicted by planning/AUDIT.md. One occurrence so far; watch the rate.
Fix: job selectors (status/result/events/resume/answer/ack) that fail on an
existing workspace ID now say "is a workspace, not a job" and point at
`ls --workspace` and the workspace verbs.

### F4 · doctor's exit semantics are blunt
Status: **open-product, fix landed 2026-08-06** · seen: first live run +
pre-existing e2e failures · Fix: notifier-command failure is now `warn`/exit 0
(fail is reserved for what would break a subsequent run: agent, auth/model,
state dir, unloadable config — an unloadable config also blocks dispatch, so it
stays fail); doctor e2e tests pin a hermetic LEGWORK_CONFIG so the host
notifier can't leak in (the local doctor test failures are gone). The `verify`
non-workspace message already existed
("host verification requires a workspace job").

`doctor --agent fake --json` exits 1 when any check fails (here: the host notifier
leaking into the sandbox — harness side since fixed with a hermetic config). An
orchestrator doing the *right* thing (preflight) gets a fumble for an advisory
failure. Also: three doctor e2e tests fail on this machine because the user's real
notifier config exits 2 — unrelated to eval changes, worth fixing locally.
Related polish: `verify` on a non-workspace job fails without saying the job must
be a workspace job (cost one scripted-orchestrator debugging round).

## Model behavior findings

### F5 · Autonomy variance: clarification-stop on turn 1
Status: **open-model** · seen: 1 of 3 parallel-streams runs (haiku)

One run ended after a single turn with haiku asking the user to supply the three
task descriptions — zero legwork commands, despite the autonomy instruction.
Hardening the eval preamble ("asking the user ends the session as a failure")
fixed the observed case. Track as a *rate* once repetition lands; this is the
canonical example of why single runs prove nothing.

## Baseline observations (haiku, 2026-08-01, 1–2 runs each — NOT yet rates)

### F6 · Context stays flat as supervision scales
Status: **observation**

Peak context 33–42k (16–21% of 200k) across ALL scenarios, of which ~25k is fixed
baseline (Claude Code system prompt + legwork skill). Marginal orchestration state
is ~1–2k per supervised job: 8 concurrent streams peaked at 39k vs 33k for one job.
Fresh (uncached) input for an entire run: 42–180 tokens. The two-tier event design
is doing its job — no transcript ever entered the orchestrator's context.

### F7 · Haiku handles the multi-agent pipeline
Status: **observation**

feature-pipeline (plan → implement → review FIX → relay corrections → re-review
SHIP → merge) passed live including the memory quiz: haiku collected the blueprint,
seeded the implementer with it, relayed the reviewer's V-42 finding as a resume,
re-reviewed, landed. 2 fumbles were F2 flag guesses, not flow errors. Also passed:
8-stream context-budget with 2 mid-run questions answered correctly to the right
jobs, and wrong-job-trap (opposite-answer questions on 2 concurrent jobs, both
relayed correctly ~10s each).

### F8 · Baseline pass RATES — haiku, 5 reps, 2026-08-01
The reference numbers future changes get compared against
(`/tmp/eval-baseline/20260801-142651`, ~$4.90 total):

| scenario | pass | notes |
|---|---|---|
| happy-path | 5/5 | |
| clarify | 5/5 | |
| constraint-recall | 5/5 | buried constraint recalled every time |
| wrong-job-trap | 5/5 | opposite answers routed correctly every time |
| workspace-flow | 5/5 | but median 3 denials/run — F1 |
| context-budget | 5/5 | quiz 5/5; 8 streams, peak-ctx max 41k |
| parallel-streams | 4/5 | residual variance (F5 family) |
| **feature-pipeline** | **2/5** | the real weak spot — see below |
| verify-gate | 0/5 | fails ONLY max_denials=0 → F1 is deterministic |

**feature-pipeline failure taxonomy** (the multi-agent plan→implement→review→
correct→land loop): (a) flag guessing over fumble budget — `resume --agent`,
`resume --append-prompt`, `ws review --model`, `runs --run`, none exist (F2,
now with a pattern: haiku expects dispatch-time flags to work on every verb);
(b) **landing despite FIX** — 2/5 runs merged without the correction round-trip,
validation.txt never written; the review gate's verdict was read but not acted
on; (c) one run skipped the planner stage entirely. Also new: `ack` refused on
the workspace-attached reviewer job (terminal-verb split trap — AUDIT.md's
ack/close confusion, observed live), and multiline `commit -m` failed.
Single-run n=1 had shown feature-pipeline as a clean PASS — repetition earns
its keep.

**Messy-worker baseline** (5 reps, `/tmp/eval-baseline2/20260801-144007`, ~$2.75):

| scenario | pass | notes |
|---|---|---|
| sloppy-worker | 5/5 | missing status block diagnosed + steered every time |
| flaky-worker | 5/5 | mid-turn crash recovered via same-session resume every time |
| evidence-recall | 5/5 (quiz 5/5) | cross-job info routing solid — audit finding relayed over the wrong-answer bait |
| **false-claim** | **2/5** | judgment succeeded, workflow drowned — see below |

**false-claim taxonomy**: haiku caught the false completion claim in 5/5 runs
(diff inspected, worker confronted via resume — the *judgment* is intact even at
the weak tier). The failures are pure surface friction compounding under a
multi-step recovery: `diff --json` and `resume --agent` fumbled in nearly every
run (F2), `status ws-1` (F3 selector trap), `ws list` (verb guess for `ws ls`),
one failed `close --merge-into`. Effect: 32–50 invocations, 4–5 min walls, peak
context 60k (highest observed anywhere), and only 2/5 landing the corrected work.
F2 is not cosmetic: it breaks recovery workflows at the weak tier. This is the
strongest argument yet for the flag-consistency pass.

## Harness findings (fixed)

### F9 · claude -p JSON output is a message array in CLI ≥2.1.x
Status: **fixed-harness** — parser accepts both shapes; raw transcript kept per run.

### F10 · Spec routing must match task text only, earliest position wins
Status: **fixed-harness** — two incidents: rich task texts mentioning sibling
streams (docs task containing "frontend"), and a `--run cache-queue-design` label
matching the cache spec for both jobs. Routing now parses the task positional out
of `run` argv and picks the earliest regex match. Watch for recurrence; if task
texts still cross-match, per-scenario dispatch instructions (unique keywords) are
the fallback.

### F11 · Host config leaked into the sandbox
Status: **fixed-harness** — runs now get an empty LEGWORK_CONFIG; the host
notifier had been failing doctor inside the eval (and still fails the repo's own
doctor e2e tests outside it, see F4).

## Next discriminators (open work)

- ~~Repetition~~ — landed: `-reps N -parallel M`, Rates table (pass x/N, medians,
  peak-ctx max), failing-run details only. 5-rep haiku baseline in progress.
- ~~Messy workers / judgment~~ — landed 4 scenarios attacking the "compliant
  workers, complete goals" caveats: `sloppy-worker` (no status block → blocked →
  steer to completion), `flaky-worker` (mid-turn death → recover same session; NB:
  death surfaces as a `finished` "agent exited without a result" event + failed
  state, not `interrupted`), `false-claim` (worker claims work it didn't do; catch
  via empty diff before landing), `evidence-recall` (answer lives in a prior job's
  findings, not the goal — cross-job information routing, branch-scored).
- Tier ladder: same suite on sonnet/opus — invocation/token deltas vs haiku
  measure what the harness fails to provide to weak models.
- Landing-proof scenario variant that *allows* git but scores whether receipts
  were consulted instead (turns F1 from a denial count into a choice measurement).
- Worker-side rambling that buries the actual question mid-prose with a clean
  status block; double-FIX review loops; failing provision commands.
