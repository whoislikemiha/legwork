# legwork Dogfood Review — 2026-07-08 (FROZEN)

> **This file is a historical snapshot.** It records the 2026-07-08 dogfood review of legwork
> and the findings it produced. It is no longer updated. Live tracking:
> [ROADMAP.md](ROADMAP.md); open items below were migrated to [tasks/](tasks/). Architecture
> rationale and invariants live in [../DESIGN.md](../DESIGN.md). Checkboxes reflect state at
> freeze time (all findings open unless marked already-shipped).

## How this review was done

legwork was used to review itself. Four `codex gpt-5.5` (medium effort) read-only research
jobs were dispatched against the real state directory (`~/.local/state/legwork`), each scoped
to one slice — pipeline/runs, per-job turn UX, workspace/review flow, and the money_intelligence
production run — and their reports were cross-checked against independent `jq`/Python aggregates.
Every claim below is grounded in a concrete `job-N` / `ws-N` / `run` id.

**Corpus:** 96 jobs, 43 workspaces, 43 runs, 2.5 GB. 121 finished turns (113 `done`, 6
`blocked`, 2 `failed`, 3 `interrupted`, **0 `needs-input`**). 39 workspaces merged, 2 discarded
(`ws-7`, `ws-13`), 3 preserved (`ws-24/25/26`), 2 open (`ws-43/44`). Spend: claude **$81.61**
across 30 jobs; codex **$0** across 66 (subscription). Two projects drove the corpus:
legwork's own development (~17 workspaces) and the money_intelligence app (~26 workspaces).

**Trajectory this fits into:** 2026-07-05 external cold review → 2026-07-06 codex-adapter/gc
dogfood (12 findings) → 2026-07-07 money_intelligence field notes
([../docs/field-notes-2026-07-07.md](../docs/field-notes-2026-07-07.md)) → this review. The
07-06 loop was fast: `--effort`, `artifact save`, the watch-replays-finished-turn bug,
`close --merged` merge-base verification, and the `ack` verb all shipped within ~a day.

Priorities: **P0** = contract safety/correctness (a worker can silently ship worse work) ·
**P1** = native-feel, high leverage · **P2** = ergonomics & observability · **P3** = platform.

---

## A — Worker contract & sandbox behavior

- [ ] **A1 (P0) Workers silently bend the product to fit the sandbox.** The failure mode that
  costs quality is not the block — it is the workaround. Evidence: `ws-21`/`job-37` (p2-polish)
  had to *revert its own env workarounds* before landing; recurring `uv`/`GOCACHE`/`/tmp`
  read-only reruns across p-run jobs. Fix at the source: an injected anti-workaround rule.
  → [tasks/sandbox-anti-workaround.md](tasks/sandbox-anti-workaround.md)
- [ ] **A2 (P0) Read-only and workspace-write sandboxes cannot run the test suite.** `--read-only`
  reviewers cannot run pytest ("no usable temp dir"); FastAPI `TestClient`/anyio portal
  reproducibly hangs. Evidence: `job-28`, `job-38`, `job-44` all implemented real changes then
  finished `blocked` because the in-sandbox suite hung or had no temp dir; `job-16` blocked on a
  git worktree lock. → [tasks/writable-tmpdir.md](tasks/writable-tmpdir.md)
- [ ] **A3 (P0/P1) `blocked` is one state hiding three, and "verify" is the common one.** Real
  kinds seen: *provision* — `job-29` "`uv add slowapi` cannot reach PyPI"; *verify* — `job-16`
  (done, commit blocked), `job-28`/`job-38`/`job-44` (done, suite un-runnable in sandbox);
  *decision* — design/policy calls. Orchestrators had to parse prose `result` to route. Needs a
  structured reason and a `needs-provision` escalation.
  → [tasks/structured-blocked-provision.md](tasks/structured-blocked-provision.md)
- [ ] **A4 (P2) The ask-early contract never fired: 0 `needs-input` across 96 jobs.** No
  `finished.state=="needs-input"`, no `answer` events; the whole corpus leaned on `resume`.
  Either every task was well-specified, or the ask-early bias is not triggering. Verify the path
  works before trusting it in the contract.
  → [tasks/verify-ask-early.md](tasks/verify-ask-early.md)

## B — Health & telemetry

- [ ] **B1 (P1) codex `ctx` is noise; the `!` high-context hint cries wolf.** codex ctx median
  **1.44M**, max **30.7M** (`job-78`); 63/66 codex jobs exceed the 150k threshold — including
  this review's own pure-read research jobs (431k–1056k). It is summed across a turn's calls,
  not the live window (roadmap already suspected this — confirmed against `job-78`/`job-33`,
  3-turn jobs). The one signal meant to say "fresh job vs resume" is unusable on codex.
- [ ] **B2 (P1) Running jobs show `ctx:-` — no live health signal at all.** Confirmed live in
  `ls` during this review. Telemetry only lands at the turn boundary, so a runaway is invisible
  until it ends. A **diff-progress** metric (bytes/files changed in the worktree over last N
  min) would be the truthful, agent-agnostic health signal: high activity + zero diff-progress =
  spinning. B1+B2 → [tasks/codex-health-signal.md](tasks/codex-health-signal.md)

## C — Accountability & receipts

- [ ] **C1 (P1) `meta.state` hides the worker's terminal turn state after close.** Every terminal
  job is `closed`/`done` in `meta.json`; `failed`/`blocked`/`interrupted` survive only in
  `events.jsonl` (e.g. `job-48` failed on usage limit → now `closed`). After close, `status`
  cannot answer "what did the worker actually say?" without log archaeology.
- [ ] **C2 (P1) `close` is not a first-class event; `commit` is.** 32 structured `commit` events
  carry workspace/branch/oid/summary, but closure is reconstructed from `ws/meta.json` +
  prose notes. Close/merged/discard should be as queryable as commit.
- [ ] **C3 (P2) Workspace metadata is version-skewed.** Only 21/41 closed workspaces have
  `closed_at`; 18 have `reason`/`merged_into`/`final_commit`. Early `ws-1..16` record only
  state/disposition/checkpoints. A one-shot backfill + a schema-version stamp would end the
  archaeology.
- [ ] **C4 (P2) Duplicate `commit` events across run labels.** Same oid appears in multiple run
  logs (`ws-41`×3 across `p3-assistant-hardening{,-review,-rereview}`, `ws-40`×2), making run
  ownership ambiguous in `runs`. C1–C4 → [tasks/quality-receipts.md](tasks/quality-receipts.md)

## D — The quality thesis (strongest signal in the corpus)

- [ ] **D1 (P1) Make `implement → review → fix` a first-class recipe/verb.** First-pass SHIP was
  **exactly 3/8** on P1 features (SHIP: `job-71` llm-metering, `job-76` account-security,
  `job-81` notifications; FIX: `job-58`, `job-61`, `job-70`, `job-75`, `job-83`). Across all 17
  first-pass implementation reviews: **7 SHIP / 10 FIX**. Independent high-effort review caught
  real, shippable-looking bugs on ~62% of first passes:
  - XFF rate-limit bypass — `job-42` FIX → `job-44` (`app/main.py` trusts `["*"]` proxies).
  - quarterly-as-monthly — `job-58` FIX → `job-33` (`recurring.py:145` accepts gaps ≤3mo).
  - refresh-rotation race minting two token pairs — `job-59` FIX → `job-53` (non-atomic; CAS fix).
  - unescaped SQL LIKE wildcards — `job-61` FIX → `job-62` (`f"%{q}%"` unescaped `\ % _`).
  - **plus bugs the field notes missed**: CSV round-trip flipping expense↔income + OFX XML
    entity-expansion DoS (`job-70`); a net-worth phantom-negative-balance regression the *fix
    turn itself introduced*, caught on re-review (`job-75`); `formatAmount` `RangeError` chat
    crash (`job-83`); infinite retry loop from retry-state-in-`error_message` (`job-93`);
    IntegrityError discriminator fragility landed as SHIP-with-follow-up (`job-89`).

  A `ws review` verb dispatches a read-only reviewer against the workspace diff with the right
  defaults (big model, high effort, "give me the diff not the tree").
  → [tasks/ws-review-verb.md](tasks/ws-review-verb.md)

## E — Ergonomics & observability

- [ ] **E1 (P2) Per-job blocking wait.** `tail --until-idle` (used to drive this review) is the
  scriptable wait, but it is run-scoped; after `resume`/`cancel` the orchestrator must re-attach a
  monitor and a per-job fact ("this job hit needs-input") is lost in run-scoped idle. Add
  `legwork wait --job X --until done|blocked|needs-input`.
  → [tasks/per-job-wait.md](tasks/per-job-wait.md)
- [ ] **E2 (P2) Run notes are unevenly adopted, and it shows.** Self-development runs read like an
  excellent operator journal (**48 notes / 33 jobs** — capturing the leaked `LEGWORK_STATE_DIR`,
  a dangling-commit fsck recovery, "resume BROKEN: exec resume rejects --sandbox"). p-runs are
  sparse (**8 notes / 46 jobs**), so their narrative must be reconstructed from previews. The
  channel's value is proven; adoption is the gap. → [tasks/orchestrator-recipes.md](tasks/orchestrator-recipes.md)
- [ ] **E3 (P2) `--json` shapes and run selection are still guessed.** Carried from prior dogfoods
  and reconfirmed while driving this review (top-level arrays vs `{runs:[...]}`, positional run
  arg vs `--run`). Tracked in ROADMAP backlog (self-describing JSON + run selector consistency);
  no new task file until picked up.

## F — Recipes worth documenting (patterns already in the data)

- [ ] **F1 (P2) Competition shape.** `presentation` run: `job-15` (opus/`ws-6`) vs `job-16`
  (codex/`ws-7`) implemented competing versions; orchestrator picked the winner, discarded the
  loser except one grafted fix.
- [ ] **F2 (P2) Design-only pipeline.** `p1-bank-sync`: design doc → adversarial *design* review
  (`needs-changes`) → revise, no code (`job-31`→`job-35`→`job-36`).
- [ ] **F3 (P2) Reviewer-diff handoff & poisoned-context restart** already work but live only in
  the guide's prose; fold F1–F3 into named recipes.
  → [tasks/orchestrator-recipes.md](tasks/orchestrator-recipes.md)

## G — Disk & reclamation

Measured on the real state dir 2026-07-08: **2.5 G total, but legwork's own data is 32 M**
(`jobs/`, 96 jobs) + 308 K (`runs/`). The 2.0 G is `workspaces/` — worktree *contents*, not
legwork bookkeeping.

- [ ] **G1 (P1) Worktrees are the entire disk cost; the branch, not the checkout, is durable.**
  `ws-51/tree` 1.2 G (open), `ws-27/tree` 814 M (orphan, meta-less). ~1.6 G is gitignored
  `node_modules` (683 M + 643 M) + Next `.next` (300 M) — zero review value. Once `ws commit`
  records the tree, the worktree directory is disposable cache (git shares the object store; the
  `legwork/ws-N` branch preserves the work). → [tasks/worktree-reclaim.md](tasks/worktree-reclaim.md)
- [ ] **G2 (P1) No "keep the branch, drop the local worktree" mode.** `reclaim()`
  (workspace.go:399) fuses three deletions — checkpoint refs + `git worktree remove --force`
  + `git branch -D` — so the only way to keep the branch (`--keep-worktree`/`--preserve`) also
  keeps the whole checkout. `ws-13`/`ws-18`/`ws-24`/`ws-25`/`ws-26` are closed but still hold
  both worktree and branch for this reason (legwork-repo, so KB — but the same close on a
  money_intelligence workspace pins ~650 M). Branches are free on GitHub; worktrees are the disk
  cost — decouple them. → [tasks/worktree-reclaim.md](tasks/worktree-reclaim.md)
- [ ] **G3 (P2) Default reclamation deletes branches; it should keep + push them.** Normal close
  `git branch -D`s the local branch, and `--preserve` pins the checkout instead of keeping just
  the branch. Desired policy: drop the worktree by default, keep the branch (push to origin),
  delete a branch only on explicit `--discard`. → [tasks/worktree-reclaim.md](tasks/worktree-reclaim.md)
- [ ] **G4 (P2) Orphan trees are report-only.** gc pass 5 reports a legwork-owned meta-less
  `tree/` as a "human decision" instead of reclaiming it, so `ws-27`'s 814 M lingers; auto-gc is
  24 h-gated (`.gc-last` Jul 7 20:28), compounding the delay.
  → [tasks/worktree-reclaim.md](tasks/worktree-reclaim.md)

**What legwork's own reclamation gets right (frozen, for contrast):** `jobs/` is bounded —
transcripts gzip after 1 h and delete 7 d post-close (two-tier logs, DESIGN §3). `gc --dry-run`
on the real dir would reclaim 740 MB (84 transcripts compressed, 1 half-created dir, 3 orphan
refs). The index/history growth the tool is often feared for is a non-issue by design; the disk
risk is the worktree caches setup hooks create.

---

## Already shipped (pre-system, frozen record)

Landed before this planning board existed (from the prior root ROADMAP changelog + trajectory):

- [x] `legwork doctor` preflight with live probe (`doctor-command-flow`, ws-1).
- [x] Codex adapter (convention status blocks, sandbox modes) (`codex-adapter`, ws-3).
- [x] `legwork gc` reclamation + `--close-merged` (`gc`, ws-4).
- [x] Presentation layer: `runs`, `tail` (`--until-idle`), read-only `dashboard` TUI, `serve`
  local browser console (`presentation`, `roadmap-next-dogfood`, `live-serve-v1`, `serve-dashboard-redesign`).
- [x] `ws commit <ws> -m` — orchestrator-owned non-empty commits with `final_commit` (`job-18`).
- [x] Workspace lifecycle metadata: `closed_at`/`reason`/`superseded_by`/`merged_into`/`retention`; `--preserve`.
- [x] `--effort` (claude+codex) and `--fallback-model` (claude) passthroughs (`passthrough`).
- [x] `[health] context_threshold` + `ctx` marker in `ls` + `status` hint + `context_high` json (`ctx-hint`).
- [x] `ack` for terminal workspace-less jobs; `close --merged` merge-base verification with `--into`/`--force`.
- [x] Watch seeks past finished turns on resumed jobs; claude ctx = last call's real window.

Their evidence lives in `~/.local/state/legwork/runs/{doctor-command-flow,codex-adapter,gc,presentation,ctx-hint,passthrough,...}`.
