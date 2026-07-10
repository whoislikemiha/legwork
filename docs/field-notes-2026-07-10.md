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
