# Field notes: active-job observability and the next roadmap

Written by the orchestrating agent on 2026-07-10 after the
`human-active-job-observability` run (`ws-67`, jobs `job-146`–`job-149`).

legwork version during the run: `dev`, commit `867bb64f7ee5`, clean, build date
`2026-07-10T07:40:59Z`. The workspace was based on that commit; the installed binary
therefore did not yet contain the `ls` behavior being developed.

## Outcome

`ws-67` landed human-first active-job observability and the board for the next work:
plain `legwork ls` now hides closed history, sorts attention before active and
unreviewed work, keeps one physical line per job, and provides built-in
workspace/run/state/limit filters in both human and JSON modes. The workspace also
promoted or added the P1 tasks exposed by this dogfood run.

The first Opus review (`job-148`) returned **FIX**: explicit `--limit 0` had conflicting
semantics, empty filtered results could lie, copy-pasteable selectors were clipped,
four promoted task headers had drifted from the board, and `DESIGN.md` still described
all-history `ls`. All five were corrected and regression-covered. Re-review `job-149`
returned **SHIP**. The focused `TestLS` run and the full `gofmt`/`go vet`/`go test`
gate passed in the workspace; the full gate passed again on `main` after merge.

## Product evidence and friction

1. **The default job list hid current work in history.** A multiline closed `job-96`
   consumed terminal scrollback while current jobs sat below it. This directly produced
   `human-active-jobs`; the fix landed in this run.
2. **There is still no per-job blocking wait.** Waiting for `job-149` required
   `tail --job job-149 --until-idle`, a run-tail surface used as a supervisor. The
   existing `per-job-wait` task is now P1/Next.
3. **External verification has no receipt/state transition.** `job-146` correctly
   reported `blocked.kind=verify` because its sandbox lacked uncached Go modules and
   network access. The host gate passed, but recording that fact required prose in a
   note/task file while the job remained blocked. Filed as
   `external-verification-receipts`, paired with `quality-receipts`.
4. **A transient provider failure erased the turn's useful terminal meaning.**
   `job-147` made and checkpointed valid changes, then GPT-5.6 Terra returned a capacity
   error; Legwork surfaced the job as generic `failed` with only the provider sentence
   as its result. The diff survived. Filed as `transient-provider-recovery`.
5. **Workspace readiness still requires joining surfaces.** Confirming which workspace
   was open and ready to land involved `status`, `events`, `diff`, `ws ls`, review
   results, and git state. `ws ls` is also a long all-history list and has no JSON flag.
   This is covered by `actionable-workspace-status` and the structured-operation work,
   rather than patched ad hoc here.

## Worker friction harvest

The worker's only friction note was concrete: its sandbox had no cached third-party Go
modules and DNS/network access to `proxy.golang.org` was blocked, so focused e2e and
full verification could not compile there. The desired preflight is useful, but the
larger durable requirement is an orchestrator-side verification receipt; that task is
now P1/Next.

## Actions taken

- Landed `ws-67` after FIX → corrections → SHIP and repeated host verification.
- Promoted `per-job-wait`, `quality-receipts`, `codex-health-signal`, and
  `unified-addressing` to P1/Next with task headers reconciled to the board.
- Added P1 tasks for external verification receipts, actionable workspace/job status,
  transient provider recovery, orchestrator profiles, and a native structured
  operation surface; each preserves the no-daemon/no-database/no-MCP boundaries.
- Moved human-readable active-job observability to `planning/done/` and left the next
  implementation wave explicit in `planning/ROADMAP.md`.

## Roadmap authoring correction

Later the same day, the roadmap rewrite campaign exposed a failure in orchestration
proportionality. Installed Legwork was `dev`, commit `49e024612dec`, clean, build date
`2026-07-10T08:41:44Z`.

The requested campaign was to improve the existing task descriptions. Preflight found
that Legwork silently clamped Codex `xhigh` to `high`, while a direct Codex CLI 0.144.1
probe confirmed `gpt-5.6-sol` accepted `xhigh`. Instead of recording that friction and
continuing, the orchestrator made it a prerequisite, delegated an exhaustive task
specification, and treated the generated 190-line task as an approved contract.

That produced `ws-68`: two Terra/high implementation turns, an Opus/xhigh FIX review,
host verification, and a second Opus review over a 16-file, 547-addition diff. The
scope had expanded from one adapter mapping into effort receipts, doctor behavior,
legacy metadata recovery, and model normalization. `job-153` was interrupted and
`ws-68` was discarded; none of that code landed.

The durable product issue remains real but is now the proportional
`model-aware-reasoning-effort` task: pass an explicit Codex `xhigh` through, keep
`max` conservative, update focused tests/docs, and add no metadata system. The twelve
pre-existing task files plus that new task were rewritten directly by the orchestrator
as concise product contracts; no task-writing agent was used.

Process lesson: dogfood friction becomes a roadmap input, not an automatic blocker.
Delegated task drafts are research until the orchestrator approves scope. Small fixes
need an explicit size/time budget and bounded review; the full implement→review→fix→
re-review loop is reserved for changes whose user-visible risk justifies it. This is
now stated in the guide and bundled skill.

## Unified addressing dogfood run

The first implementation from the rewritten board used installed Legwork `dev`,
commit `49e024612dec`, clean, build date `2026-07-10T08:41:44Z`. Terra/high
implemented the bounded read-side selector slice in `ws-74` (`job-159`); host
verification caught one missing import and then two focused compatibility-test
failures before review. The worker sandbox again could not fetch uncached Go modules,
so every full gate ran from the orchestrator seat.

Opus/xhigh review (`job-165`) returned **FIX** after reproducing a critical regression:
`tail <job> --until-idle` reconciled a cached active `Meta` over the runner's newer
terminal record, erasing result and telemetry and appending a false interrupted event.
It also caught broken run-event cursor semantics, an unversioned JSON element-shape
change, masked metadata corruption, and a user-visible flag sentinel. A fresh
Terra/high fix job (`job-166`) corrected only those findings. Focused re-review returned
**SHIP** after rebuilding the binary and reproducing the original failure and queued /
positional variants. `ws-74` landed as merge `b38f9ae`.

The full gate passed in the workspace. Post-merge, all packages and focused selector /
tail / artifact tests passed; the known `TestCodexPassthroughs` detached-runner cleanup
race failed twice and remains the existing P2 small remainder rather than interrupting
this task. Codex context telemetry climbed above six million during the three-turn
implementation lineage, reinforcing the existing truthful-health task rather than
triggering an unrelated fix.

One new low-severity edge from re-review was added to the roadmap: when reconciliation
cannot persist metadata, `tail --until-idle` can keep waiting even though stale-meta
clobber is prevented. Worker verification limitations continue to support the existing
external-verification-receipts task; no duplicate task was filed.

## Per-job wait dogfood run

The next bounded task used the same installed Legwork build (`dev`, commit
`49e024612dec`) and a fresh workspace, `ws-75`. Terra/high (`job-170`) implemented
the exact-job `wait` verb, stable reached/timeout/terminal-mismatch outcomes, JSON
metadata, documentation, and focused lifecycle tests. The host full gate passed
before review because the Terra workspace-write sandbox again lacked the populated
Go module cache available to the orchestrator.

Opus/xhigh (`job-171`) returned **FIX** after directly reproducing a critical contract
failure that the initial tests missed: `wait <job> --timeout 1s` without `--until`
never checked its deadline and then busy-spun at roughly 77% of one core. The review
also identified the missing deadline-guarded regression and two low-cost JSON/parser
cleanups. Terra corrected the bounded set. Focused re-review compared the old and new
binaries: the old invocation required `SIGKILL`, while the corrected command returned
at the deadline with exit 1, `outcome: timeout`, `waited_for: []`, and no measurable
spin. Verdict: **SHIP**. The review's sole non-blocking test-hardening nit was applied
before landing.

The full repository gate passed in the workspace and again after merge `48128f4`.
`legwork wait` now replaces per-job supervisor loops; `tail --until-idle` remains the
run-level primitive. The worker/host cache difference is further evidence for the
existing external-verification-receipts task, not a new duplicate. No additional
roadmap item emerged from this run.

## Quality receipts dogfood run

The first receipts slice started with installed Legwork `dev`, commit `caa965024967`,
clean, build date `2026-07-10T12:09:33Z`. The task was split before dispatch: this
workspace owned last-turn outcomes and structured review verdicts only; close and
commit identity remained a separate next task. Terra/high `job-172` implemented the
slice in `ws-76`; the host full gate and a real Claude/Haiku runner smoke passed.

Opus/xhigh `job-176` returned **FIX** and immediately dogfooded the core failure: its
ordinary prose plus fenced JSON verdict produced `latest_review.parsed:false`. The
review also reproduced two high-severity data-integrity problems: review completion
could be lost if workspace receipt persistence failed, and a trimmed git diff could
drop trailing context and become an invalid patch while still being hashed as the
review input. SHIP-with-low-findings, severity normalization, and legacy backfill
semantics needed correction too.

The resumed Terra lineage exposed `context_high:true` at a 6.42M Codex context rollup,
so it was cancelled immediately and the accepted findings were reseeded into fresh
`job-180`. That job corrected the bounded set; host verification passed again. Focused
Opus re-review `job-181` then proved the receipt path itself: workspace metadata stored
`parsed:true`, verdict `FIX`, one medium and three low findings, the exact checkpoint,
and the prompt-diff digest. The remaining interrupted-turn outcome refresh, duplicate
human status line, event-schema note, and stderr/diff separation were fixed directly.
Focused tests and the full `gofmt`/`go vet`/`go test` gate passed before landing and
again on `main` after merge `3802c33`.

The landed behavior keeps a compact structured outcome on terminal jobs and after
close, best-effort backfills legacy closed jobs without mutating them on read, binds
each review request to an immutable checkpoint and exact raw diff digest, and records
strict fail-closed review receipts plus `review-verdict` events. The close operation
itself was run with the workspace-built binary; `job-181` remained queryable as
`state:closed` with its final `state:done` outcome.

Worker sandbox module/DNS limits remain evidence for the existing external-
verification-receipts task. The multi-million Codex context value is cumulative rather
than an honest live-window measurement, exactly the existing codex-health-signal task;
no duplicate pre-resume task was filed.

## Close and commit receipts dogfood run

The workspace half of receipts started with installed Legwork `dev`, commit
`d596599a4193`, clean, build date `2026-07-10T13:09:52Z`. The bounded contract kept
workspace metadata authoritative: additive schema versioning, one final-commit
identity, one close decision across CLI and gc paths, and a separate workspace event
index without changing tripwire or reclamation policy.

Terra/high `job-185` implemented the first pass in `ws-80`; the full host gate passed.
Opus/xhigh `job-186` returned **FIX** with two high, three medium, and one low finding.
The reproduced high defects were non-replayable partial-success traps: a workspace
history append could fail after a commit or close was already durable, causing the CLI
to report failure and strand job closure. The review also found that workspace history
had no CLI read path, future workspace schemas could be destructively rewritten,
unknown legacy times serialized as year one, and default actors could misattribute a
future caller.

Fresh Terra/high `job-187` implemented the accepted corrections. Durable operations now
succeed with a persisted `history_error` warning instead of inviting replay;
`legwork events ws-N --workspace` exposes the workspace cursor; future schema versions
fail closed; unknown times stay absent; and close actors are explicit. The host gate
caught two stale test-contract mismatches that the worker sandbox could not compile;
those were corrected and the complete gate passed.

The feature then audited its own landing. The workspace-built binary committed `ws-80`
and returned a stable `final_commit.receipt_id`; querying workspace event sequence 1
exposed an oversized embedded diffstat. Event copies were made compact while the full
metadata rollup remained intact, a regression test was added, and the complete gate
passed again. A second commit produced compact event sequence 2. Closing through the
same binary produced an orchestrator-attributed merged receipt and queryable
`workspace-close` sequence 3 after worktree reclamation. The full repository gate passed
again on `main` after merge `edb4725`.

The worker sandbox's uncached module/DNS block remains direct evidence for the already
scheduled external-verification-receipts task. The oversized event was fixed in scope;
no new roadmap item emerged.
