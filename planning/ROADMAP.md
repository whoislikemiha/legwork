# Roadmap

The live work board. One task = one file in [tasks/](tasks/) — goal, design, constraints, and
blockers live in the task file; agents get dispatched with "read the task file". Landed tasks
move to [done/](done/) with implementation notes and the review verdict appended.

**Rules:** status lives HERE (not in folder location — the only file move is tasks/ → done/ at
landing). The orchestrator is the single writer of this file and performs the move; workers only
append to their own task file. Architecture invariants and the "why is it like this" answers live
in [../DESIGN.md](../DESIGN.md) (read it before changing architecture). The 2026-07-08 dogfood
review that seeded the current open items is frozen in [AUDIT.md](AUDIT.md).

Branch model: reviewed work lands on `main` (fast-forward after an independent review verdict +
orchestrator verification); releases are `v*` tags cut through goreleaser once CI is green. Never
tag or publish unless explicitly asked.

Priorities: **P0** = contract safety/correctness · **P1** = native-feel, high leverage ·
**P2** = ergonomics & observability · **P3** = platform.

---

## In flight

- [ ] [Human-readable active-job observability](tasks/human-active-jobs.md) — **P1.** Make plain
  `legwork ls` show current attention/active/unreviewed jobs newest-first in one physical line;
  hide closed history by default and add first-class workspace/run/state/limit filters. Promoted
  from live 2026-07-10 dogfood after a multiline closed `job-96` buried the current jobs.

## Next

- [ ] [Per-job blocking wait](tasks/per-job-wait.md) — **P1.** Replace hand-built supervisors with
  `legwork wait <job> --until done|blocked|needs-input`; already-terminal jobs return immediately.
- [ ] [External verification receipts](tasks/external-verification-receipts.md) — **P1.** Run and
  persist orchestrator-side verification for `blocked.kind=verify`, resolving the actionable state
  without resuming poisoned worker context.
- [ ] [Actionable workspace and job status](tasks/actionable-workspace-status.md) — **P1.** Add
  `ws status` and deterministic attention/next-actions over implementation, review, verification,
  diff, commit, merge, and close facts.
- [ ] [Quality receipts / accountability shape](tasks/quality-receipts.md) — **P1.** Persist
  last-turn state in meta; first-class `close` event; dedupe cross-label commit events; backfill
  version-skewed workspace metadata; structured review verdicts (AUDIT C1–C4).
- [ ] [Truthful health signal](tasks/codex-health-signal.md) — **P1.** Fix/suppress codex `ctx`
  inflation, add a mid-turn heartbeat and diff-progress so running health stops lying (AUDIT B1–B2).
- [ ] [Unified job/run addressing](tasks/unified-addressing.md) — **P1.** Every relevant verb takes
  a job ID or run label consistently; eliminate selector folklore and command-specific guessing.
- [ ] [Transient provider failure recovery](tasks/transient-provider-recovery.md) — **P1.** Classify
  capacity/overload separately from task failure, preserve checkpoint evidence, and provide bounded
  retry/actionable recovery when a provider fails after useful tool work.

## Later

- [ ] [Verify the ask-early path actually fires](tasks/verify-ask-early.md) — **P2.** 0
  `needs-input` in 96 jobs; prove the contract path works before trusting it (AUDIT A4).
- [ ] [Orchestrator profiles](tasks/orchestrator-profiles.md) — **P1.** Named, inspectable presets
  for agent/model/effort/access/timeout policy; explicit flags override resolved profile values.
- [ ] [Native-feel structured operation surface](tasks/native-operation-surface.md) — **P1.** Stable
  JSON envelopes and schema discovery for the core control loop without MCP or a daemon.
- [ ] [Checkpoint discoverability](tasks/ckpt-listing.md) — **P2.** `ws ckpts` lists ckpt refs;
  makes the delta-review pattern (used for the 2026-07-08 TOTP security fix) first-class instead
  of folklore. Pairs with `ws review`.
- [ ] [Honest cost accounting + per-run rollup](tasks/cost-rollup.md) — **P2.** codex jobs report
  `$0.00` (subscription) next to Opus `$1.23`; show basis + token totals, roll up per run. May
  fold into quality-receipts.
- [ ] Small remainders — carried from the pre-system roadmap; **no task file yet, create one when
  picked up** (each is a real item, just not currently scheduled):
  - **Command grammar + self-describing JSON** (P2) — wrapped/documented `--json` envelopes,
    examples in help (AUDIT E3). Run-selector consistency promoted to
    [unified-addressing.md](tasks/unified-addressing.md); envelope work promoted to
    [native-operation-surface.md](tasks/native-operation-surface.md).
  - **`needs-decision` via `approve`** (P2) — route permission judgment calls via
    `--permission-prompt-tool`; gates fail closed; hooks handle policy denies. `legwork
    approve` shipped 2026-07-08 gating `needs-provision`; this item extends the same verb
    to permission-shaped decisions (DESIGN §5 updated accordingly).
  - **`TestCodexPassthroughs` teardown flake** (P2, small) — recurring `TempDir RemoveAll:
    directory not empty` race between the detached runner's writes and test cleanup (bit 3×
    on 2026-07-08, in worker sandboxes and on the host; auto-gc already suppressed). Make the
    test wait for runner exit or retire the job dir teardown-safe.
  - **Codex quota/limit observability** (P2) — classify usage-limit failures (`job-33`/`job-48`)
    distinctly from real failures; support configured reset windows.
  - **`ws refresh`** (P2) — reconcile an open workspace with a moved base (fetch/merge/report
    conflicts as needs-input).
  - **`max_concurrent`** (P2) — cap simultaneous runners with a visible pending queue.
  - **`diff --since-last-review`** (P2) — per-workspace review cursor shared by CLI and `serve`.
  - **`serve` information density** (P2) — clamp long notes/tasks/results, progressive disclosure.
  - **`fork` / `ask`** (P3) — interrogate/branch a session without disturbing it.
  - **`.legwork.md` project context** (P3) — per-repo standing instructions appended to worker rules.
  - **Upstream-drift tripwire** (P3) — committed `--help`/stream snapshots, daily CI diff, nightly
    canary (the manual version is the AGENTS.md real-agent smoke).
  - **npm/PyPI wrapper packages** (P3) — turn name-reservation stubs into binary-fetching installers.

## Needs a decision

- [ ] **Enforced structured status on codex** (P2) — codex `--output-schema` could force
  `{state, question, summary}` JSON (`"enforced"`), killing the missing-block ambiguity; deferred
  because it changes `Result` format and needs a worker-rules variant. Decide before building.

## Parked

- [ ] **Mid-turn toolbelt** (stdio shim, same binary): `ask_orchestrator`, `report_progress`,
  `request_approval`, `get_artifact`. Designed in DESIGN.md §5; the turn-boundary protocol stays
  the guaranteed baseline. Read-only mid-turn cost/context signal (see health task) may ship first.
- [ ] Bidirectional stream-json persistent workers — adapter interface must not preclude; not built.

## Rejected (with reasons — reopen only with new arguments)

- **Permission allowlists as the default worker mode.** Headless mode auto-denies anything not
  allowed, and no allowlist can enumerate arbitrary build/test commands — workers would silently
  lose tools mid-task. The layered model stands: read-only harness modes for plan/research, bypass
  + PreToolUse hook denies + worktree blast-radius for mutating turns, OS sandbox on codex.
  (`--allowed-tools` as an *optional* passthrough is fine.)
- **`legwork pr` verb.** Landing work is orchestrator territory (gh CLI plus judgment). The tool
  stays a job/workspace substrate; PR creation is a recipe, optionally a worker job with `--allow-push`.
- **Kanban/queue semantics, cron triggers, delegation trees.** That's the orchestrator's layer.
  legwork stays the substrate: tiny, scriptable, ssh-friendly, structured, detached, review-gated.
- **MCP server integration.** The CLI-over-ssh contract is the product; see DESIGN.md §1.
- **Database / daemon.** Files + detached runners, permanently (DESIGN.md §11).

## Done

Tasks landed under this system appear in [done/](done/) with notes + review verdict. Everything
that shipped before this board existed (doctor, codex adapter, gc, the presentation layer,
`ws commit`, lifecycle metadata, `--effort`/`--fallback-model`, the `ctx-hint` health surface,
`ack`, `close --merged` verification, watch/ctx fixes) is recorded in the frozen
[AUDIT.md](AUDIT.md) "Already shipped" section.
