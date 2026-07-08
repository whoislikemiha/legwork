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

*(nothing — wave 1, all seven `next-roadmap` tasks, landed on main 2026-07-08; see
[done/](done/) for verdicts and the [field notes](../docs/field-notes-2026-07-08.md) for the
run retrospective)*

## Next

Wave 2 — how-to-orchestrate delivery, seeded by the 2026-07-08 handover analysis
([field notes](../docs/field-notes-2026-07-08.md)): a top-tier orchestrator spent ~70% of its
reasoning re-deriving campaign strategy the docs don't carry; smaller orchestrators can't. Lands
after wave 1 (doc files conflict; two tasks depend on wave-1 contract changes).

- [ ] [Orchestrator recipes + doc consistency](tasks/orchestrator-recipes.md) — **P1, promoted
  from Later/P2.** The campaign-shape recipe (conflict plan → parallel implement → serial land),
  append-prompt norms + a worked example, small preflight facts (model defaults, `ws new`
  concurrency), plus the original F1–F3/E2 recipes. The cheapest capability uplift on the board:
  moves strategy from per-run frontier-model reasoning into the guide, once (AUDIT E2, F1–F3;
  field-notes 2026-07-08).
- [ ] [`legwork rules` verb](tasks/rules-verb.md) — **P1, new.** Print the injected worker
  contract verbatim. "Never paraphrase the contract" is unfollowable when the contract text
  lives only in Go source — even Opus-high restated contract territory in its append-prompt
  minutes after acknowledging the rule (field-notes 2026-07-08). After wave 1's contract changes.
- [ ] [`--append-prompt-file`](tasks/append-prompt-file.md) — **P2, small, new.** Multi-line
  append-prompts from a file/stdin instead of shell quoting; the silent-degradation footgun
  (field-notes 2026-07-08).

## Later

- [ ] [Quality receipts / accountability shape](tasks/quality-receipts.md) — **P1.** Persist
  last-turn state in meta; first-class `close` event; dedupe cross-label commit events; backfill
  version-skewed workspace metadata; structured review verdicts (AUDIT C1–C4).
- [ ] [Truthful health signal](tasks/codex-health-signal.md) — **P1.** Fix/suppress codex `ctx`
  inflation, add a mid-turn heartbeat and a diff-progress metric so `ctx:-`-while-running and the
  crying-wolf `!` stop lying (AUDIT B1–B2).
- [ ] [Per-job blocking wait](tasks/per-job-wait.md) — **P2.** `legwork wait --job X --until
  done|blocked|needs-input` (AUDIT E1; field-notes 2026-07-07 #1).
- [ ] [Verify the ask-early path actually fires](tasks/verify-ask-early.md) — **P2.** 0
  `needs-input` in 96 jobs; prove the contract path works before trusting it (AUDIT A4).
- [ ] [Unified job/run addressing](tasks/unified-addressing.md) — **P2.** Every verb takes a job
  id OR a run name (`status` vs `tail --run` currently disagree). Promoted run-selector piece of
  the command-grammar remainder.
- [ ] [Checkpoint discoverability](tasks/ckpt-listing.md) — **P2.** `ws ckpts` lists ckpt refs;
  makes the delta-review pattern (used for the 2026-07-08 TOTP security fix) first-class instead
  of folklore. Pairs with `ws review`.
- [ ] [Honest cost accounting + per-run rollup](tasks/cost-rollup.md) — **P2.** codex jobs report
  `$0.00` (subscription) next to Opus `$1.23`; show basis + token totals, roll up per run. May
  fold into quality-receipts.
- [ ] Small remainders — carried from the pre-system roadmap; **no task file yet, create one when
  picked up** (each is a real item, just not currently scheduled):
  - **Native-feel adapter surface** (P1) — expose the loop (`run`/`status`/`events`/`diff`/`ws
    commit`/`close`/`artifact`) as structured operations so harnesses don't parse human output.
  - **Command grammar + self-describing JSON** (P2) — wrapped/documented `--json` envelopes,
    examples in help (AUDIT E3). Run-selector consistency promoted to
    [unified-addressing.md](tasks/unified-addressing.md).
  - **`needs-decision` via `approve`** (P2) — route permission judgment calls via
    `--permission-prompt-tool`; gates fail closed; hooks handle policy denies. `legwork
    approve` shipped 2026-07-08 gating `needs-provision`; this item extends the same verb
    to permission-shaped decisions (DESIGN §5 updated accordingly).
  - **Codex sandbox validation ergonomics** (P2) — inject `GOCACHE`/`GOMODCACHE`/`TMPDIR`
    defaults so Go build/test works in codex sandboxes (overlaps writable-tmpdir).
  - **`worktree.toml` verify hook** (P1) — `verify = "…"` run OUTSIDE the sandbox after each turn,
    result attached to job status, so workers/reviewers never run the suite in a sandbox that
    can't (pairs with the "verify" blocked kind and writable-tmpdir).
  - **Codex quota/limit observability** (P2) — classify usage-limit failures (`job-33`/`job-48`)
    distinctly from real failures; support configured reset windows.
  - **Closed-job visibility in `ls`** (P2) — decide whether `ack`'d jobs hide by default with
    `--all`.
  - **`ws refresh`** (P2) — reconcile an open workspace with a moved base (fetch/merge/report
    conflicts as needs-input).
  - **Profiles** (P2) — named agent+model+access+rules+timeout presets (`--profile opus-review`).
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
