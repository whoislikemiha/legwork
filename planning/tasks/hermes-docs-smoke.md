# Hermes docs + live smoke — surface the fourth dialect honestly

Status: next · Priority: P2 · Umbrella: **Hermes agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: hermes-adapter · Workspace: —

## Goal

All orchestrator-facing docs describe the hermes dialect truthfully — especially its
sharp edges (no sandbox, no read-only, no mid-turn events, sidecar-derived truth) —
and AGENTS.md gains the hermes real-agent smoke recipe.

## Design

Docs travel in threes (+ the contributor file); the guide is canonical:

- `internal/guide/guide.md`: agent list gains hermes; a dialect paragraph covering —
  no harness sandbox or plan mode → read-only jobs rejected at dispatch, route
  plan/review/research turns to other agents; containment is worktree blast radius +
  injected rules only, so keep hostile-input work (web research) off hermes; no
  mid-turn activity events (`watch`/`events` are quiet until the turn ends — that is
  honesty, not breakage); telemetry from the usage sidecar with `cost_status`
  semantics (subscription = included, no $ claimed); resume works but session IDs
  chain (status/meta show the latest); auth fix-it line (`hermes portal` /
  provider keys, alongside `claude /login`, `codex login`, `cursor-agent login`);
  `--effort`/`--fallback-model` rejected; heavy context baseline (~18k) explained so
  orchestrators don't misread the health line.
- `skills/legwork/SKILL.md`: same, skill-length. Note `skill install --target
  hermes` already exists — confirm the installed skill text also reflects the new
  roster.
- `README.md`: roster + headless-invocation line (`hermes -z … --usage-file`),
  security-posture note for hermes mirroring DESIGN §9, auth-required hints.
- `AGENTS.md` "Verify before claiming done" gains the hermes smoke (subscription
  auth → cost 0, check `context`; task-shaped prompt — same reasoning as codex, do
  not use "reply with exactly ..." if it fights the status-block contract; measure
  once and write down what was measured):

  ```bash
  (
    export LEGWORK_STATE_DIR=$(mktemp -d)
    go build -o /tmp/lw . && /tmp/lw doctor --agent hermes   # auth guard
    /tmp/lw run --agent hermes "Create a file named smoke.txt containing the single word ok."
    sleep 60 && /tmp/lw status job-1   # expect: state done, context > 15000
  )
  ```

  (hermes startup is Python-slow; calibrate the sleep against a real run.)

## Acceptance criteria

- Guide/SKILL/README mutually consistent and consistent with `Caps()` and dispatch
  rejections; no doc overclaims (reviewer diffs claims against the probe evidence in
  hermes-adapter.md).
- AGENTS.md smoke runs clean on this machine; receipts (job ID, `status --json`)
  pasted into this task file's Log before review.
- The read-only rejection and its suggested alternative appear verbatim in the guide
  (orchestrators will hit it; the error and the doc must agree).
- `gofmt -l . && go vet ./... && go test ./... -count=1` green.

## Non-goals

- Guide restructuring; additive edits only.
- Hermes-as-orchestrator material (that's the existing skill-install audience, not
  the worker dialect).

## Log
