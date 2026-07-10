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
