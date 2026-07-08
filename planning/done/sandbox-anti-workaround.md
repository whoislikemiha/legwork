# Sandbox anti-workaround rule

Status: next · Priority: P0 · Origin: AUDIT A1 (+ prior ROADMAP Next #1) · Depends: — · Workspace: —

## Goal

Add one line to the **injected worker contract**: never modify the test harness, build config,
or dependencies to accommodate a sandbox limitation — report `blocked` with the exact failing
command instead. Highest-leverage fix in the review: it kills the silent-workaround failure mode
where a worker ships a *worse product* to make a command pass.

## Context & design

- The failure mode is not the block, it is the workaround. Evidence: `ws-21`/`job-37`
  (p2-polish) had to revert its own env workarounds before landing; the field notes recorded
  workers deleting Google fonts + rewriting `next build` to pass offline, and monkeypatching
  `fastapi.testclient` globally when pytest hung. After an explicit anti-workaround line was
  added to `--append-prompt` in the 2026-07-07 run, workers began self-reverting — the rule
  works; it belongs in the injected contract, not in every orchestrator's prompt.
- Injected rules are tool-owned and live in `internal/rules`. This is a rules-text change plus
  its worker-facing wording per adapter (claude / codex) if they diverge.
- Wording should be concrete and bounded: it must forbid *editing harness/build/deps to dodge a
  sandbox limit*, while NOT forbidding legitimate dependency changes the task actually calls for.
  Pair it with the escalation target from the structured-blocked task (report the failing command
  as `blocked`, ideally `needs-provision`).

## Constraints

- Injected rules are tool-owned (AGENTS.md hard rule): orchestrators add via `--append-prompt`,
  never by paraphrasing the contract. This task moves the proven line *into* the contract.
- The status-block contract and its parser version travel together — if the rule text is bundled
  with the status-block rules, keep `internal/rules` and `internal/adapter`'s parser consistent.
- Do not weaken "missing status block → blocked, never done".
- Docs travel in threes: update `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and
  `README.md` if the injected contract's documented summary changes.

## Blockers

None. Ships independently; strongest when combined with structured-blocked-provision (gives the
worker a clean place to escalate instead of working around).

## Log

- Added the sandbox anti-workaround line to `internal/rules/rules.go`: workers must not edit test harness, build config, or dependencies to dodge sandbox limits, and must report `blocked` with the exact failing command instead.
- Added `internal/rules/rules_test.go` covering the injected wording and its ordering before orchestrator additions.
- Updated contract summaries in `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md`.
- Verification passed with a writable Go cache: `gofmt -l .`; `GOCACHE=/tmp/legwork-go-cache go vet ./...`; `GOCACHE=/tmp/legwork-go-cache go build ./...`; `GOCACHE=/tmp/legwork-go-cache go test ./internal/rules ./internal/adapter`; `GOCACHE=/tmp/legwork-go-cache go test ./... -count=1`.
- Sandbox-limited checks: default `go test ./internal/rules ./internal/adapter` failed because `/home/miha/.cache/go-build` is read-only; reran with `GOCACHE=/tmp/legwork-go-cache` rather than changing build config. Real-agent verification did not complete in this sandbox: `timeout 90s bash -lc 'tmp=$(mktemp -d); export LEGWORK_STATE_DIR="$tmp"; GOCACHE=/tmp/legwork-go-cache go build -o /tmp/lw . && /tmp/lw doctor --agent codex && /tmp/lw run --agent codex "Reply with exactly the word PLUMBING-OK. No tools."; sleep 20; /tmp/lw status job-1'` failed at the codex doctor probe with `Read-only file system`; `timeout 120s bash -lc 'tmp=$(mktemp -d); export LEGWORK_STATE_DIR="$tmp"; GOCACHE=/tmp/legwork-go-cache go build -o /tmp/lw . && /tmp/lw run --agent claude --model haiku "Reply with exactly the word PLUMBING-OK. No tools."; sleep 60; /tmp/lw status job-1; if /tmp/lw status job-1 | grep -q "state:  active"; then /tmp/lw cancel job-1; fi; /tmp/lw events job-1 --json'` remained active with 0 tokens after 60s and was canceled.

## Friction

- The default Go cache points outside the writable sandbox, so ordinary `go test` fails before tests run; a worker-safe default `GOCACHE` would make verification less noisy without touching repo config.
- Real-agent plumbing is awkward from inside a worker sandbox: codex tries to write PATH aliases outside the writable roots, and detached runner checks can outlive short wrapper commands unless the worker hand-rolls wait/cancel logic.

## Verdict

Review job-125 (opus, high): **SHIP** first pass. Orchestrator verification: full suite green
on merge (gofmt/vet/test), real-agent claude smoke green (PLUMBING-OK, done, ctx 28k).
Landed on main 2026-07-08 via merge of legwork/ws-55 (17a1f58). Wave-1 evidence the rule
works: 7/7 sibling workers blocked honestly rather than bending the harness.
