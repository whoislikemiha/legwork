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

None.

## Next

- [ ] [Claude policy hook layer](tasks/claude-policy-hooks.md) — **P0.** Close the largest §9
  posture gap: claude mutating jobs run bypass with no PreToolUse policy layer; ship
  `legwork _hook`, per-job generated settings, fail-closed denies (push, out-of-worktree
  writes), and the `sandbox: os|policy|none` capability tier. Origin: 2026-08-06 audit.
- [ ] [In-place jobs read-only by default](tasks/inplace-readonly-default.md) — **P0.** `--dir`
  shipped writable-unless-`--read-only`; DESIGN §2 mandates read-only-unless-`--allow-write`
  (prompt-injection posture). Breaking change, loud dispatch error on readonly-incapable
  agents. Origin: 2026-08-06 audit.
- [ ] [Non-workspace auto-close + gc retention coherence](tasks/nonworkspace-autoclose.md) —
  **P1.** gc's retention clock assumes the never-shipped §2 auto-close, so unacked
  non-workspace transcripts timer-delete; pick auto-close or ack-anchored retention —
  deletion never on time alone. Origin: 2026-08-06 audit.
- [ ] [DESIGN.md sync](tasks/design-sync.md) — **P2.** Ratify deliberate drift (verb table,
  missing-block→blocked wording, renames, shipped serve/dashboard, event families), mark
  intended-but-unbuilt items with pointers; posture fixes stay with their own tasks. Lands
  after the P0s pick their direction. Origin: 2026-08-06 audit.
- [ ] [Retire the guide — progressive disclosure via the binary](tasks/retire-the-guide.md) — **P1.**
  Kill the preloaded 43KB guide; depth moves into per-verb long `--help` + embedded help topics
  (still ssh-embedded, now version-matched), SKILL.md shrinks to the loop + routing table.
  Origin: PR #1 review 2026-08-06.
- [ ] [Transient provider failure recovery](tasks/transient-provider-recovery.md) — **P1.** Classify
  temporary provider failures, preserve useful progress, and make replay safety explicit.
- [ ] [Truthful live job health](tasks/codex-health-signal.md) — **P1.** Stop false Codex context
  alarms and expose heartbeat plus workspace progress with an honest measurement basis.

## Next — agent roster umbrellas

Accepted 2026-07-25 (DESIGN.md §3 "Agent roster"; design job on ws-85). Two umbrella
features, each decomposed into ordered task files; dependency chains are noted in the
task headers.

**Cursor agent support** — `--agent cursor` (`cursor-agent -p --output-format
stream-json`; claude-shaped surface, fixtures never assumed):

- [ ] [Cursor CLI capture](tasks/cursor-cli-capture.md) — **P2.** Authenticated
  stream-json fixtures + behavior probes. **Blocked on a human running
  `cursor-agent login` on this machine.**
- [ ] [Cursor adapter](tasks/cursor-adapter.md) — **P2.** Adapter, parser, caps,
  dispatch validation, fake-parser seam, e2e, doctor. Depends: capture.
- [ ] [Cursor docs + smoke](tasks/cursor-docs-smoke.md) — **P2.** Guide/SKILL/README
  trio + AGENTS.md smoke recipe + live receipts. Depends: adapter.

## Later

- [ ] [Eval tier ladder](tasks/eval-tier-ladder.md) — **P2.** Full suite on sonnet/opus with
  `-baseline` deltas vs same-commit haiku; attributes failures to interface debt vs product gaps.
  Split from orchestration-eval at its 2026-08-06 close. Spend needs a go-ahead (~$25–80).
- [ ] [Eval codex orchestrator driver](tasks/eval-codex-orchestrator.md) — **P2.** Measure a codex
  orchestrator via the existing `cmd:` seam. Split from orchestration-eval at its close;
  blocked on codex login.
- [ ] [Orchestrator profiles](tasks/orchestrator-profiles.md) — **P1.** Named, inspectable presets
  for agent/model/effort/access/timeout policy, with explicit resolved dispatch values.
- [ ] [Stable structured operation surface](tasks/native-operation-surface.md) — **P1.** Versioned
  JSON operations and schema discovery for the core CLI-over-ssh loop without MCP or a daemon.
- [ ] [Exact Codex `xhigh` passthrough](tasks/model-aware-reasoning-effort.md) — **P2.** Stop
  silently clamping an explicit supported `xhigh` request; keep the fix adapter-local and small.
- [ ] [Verify the ask-early contract](tasks/verify-ask-early.md) — **P2.** Use controlled real-agent
  cases to prove or truthfully downgrade a behavior that fired 0 times in the 96-job audit.
- [ ] [Checkpoint discoverability](tasks/ckpt-listing.md) — **P2.** `ws ckpts` lists ckpt refs;
  makes the delta-review pattern (used for the 2026-07-08 TOTP security fix) first-class instead
  of folklore. Pairs with `ws review`.
- [ ] [Honest cost accounting and run rollups](tasks/cost-rollup.md) — **P2.** Distinguish metered,
  subscription, and unknown usage; aggregate only comparable values.
- [ ] Small remainders — carried from the pre-system roadmap; **no task file yet, create one when
  picked up** (each is a real item, just not currently scheduled):
  - **Workspace dispatch flock** (P1, small) — the one-active-job-per-workspace invariant is a
    scan-then-create (`workspace_cmds.go` active-job check), not the DESIGN §2 flock; TOCTOU
    race under concurrent dispatch. Take a per-workspace flock across check+create.
    (2026-08-06 audit.)
  - **Runner liveness PID-reuse hazard** (P2, small) — liveness is `kill(pid, 0)` on
    `RunnerPID` from meta; after reboot a reused PID can make a dead runner look alive.
    Validate process start time or parentage before trusting the signal. (2026-08-06 audit.)
  - **`ws new` base ref default** (P2, small) — branches off current HEAD; DESIGN §2 says
    default-branch tip (`DefaultBranchTip` exists but is only used at close/gc). Silently
    basing on the operator's checkout is surprise-prone; default to the default branch,
    `--base` already overrides. (2026-08-06 audit.)
  - **Command grammar + self-describing JSON** (P2) — wrapped/documented `--json` envelopes,
    examples in help (AUDIT E3). Run-selector consistency promoted to
    [unified-addressing.md](done/unified-addressing.md); envelope work promoted to
    [native-operation-surface.md](tasks/native-operation-surface.md).
  - **`needs-decision` via `approve`** (P2) — route permission judgment calls via
    `--permission-prompt-tool`; gates fail closed; hooks handle policy denies. `legwork
    approve` shipped 2026-07-08 gating `needs-provision`; this item extends the same verb
    to permission-shaped decisions (DESIGN §5 updated accordingly).
  - **`TestCodexPassthroughs` teardown flake** (P2, small) — recurring `TempDir RemoveAll:
    directory not empty` race between the detached runner's writes and test cleanup (bit 3×
    on 2026-07-08, in worker sandboxes and on the host; auto-gc already suppressed). Make the
    test wait for runner exit or retire the job dir teardown-safe.
  - **Reconciliation persistence-error liveness** (P3, small) — stale-meta hardening now
    refuses to overwrite a newer terminal record, but `tail --until-idle` can keep waiting when
    the reconciled state cannot be persisted. Return the storage error or advance the read-side
    state without weakening the no-clobber guarantee.
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
