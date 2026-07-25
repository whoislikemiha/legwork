# Hermes docs + live smoke — surface the fourth dialect honestly

Status: done · Priority: P2 · Umbrella: **Hermes agent support** · Origin: 2026-07-25
agent-roster design (ws-85) · Depends: hermes-adapter · Workspace: ws-88

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

- 2026-07-25 documentation implementation (job-225): added Hermes to the canonical
  guide and synchronized the skill, README, and contributor smoke recipe. The docs
  record final-only observation, sidecar-authoritative telemetry/session chaining,
  subscription cost semantics, the heavy context baseline, unsupported flags,
  auth fix-its, and the no-sandbox/no-read-only threat model.
- Per the orchestrator instruction, no additional provider turn was spent. Reused
  the authenticated live receipt already verified in `planning/done/hermes-adapter.md`
  (job-223). The relevant `status --json` fields from normal smoke `job-1` were:
  `{"id":"job-1","agent":"hermes","state":"done","session_id":"20260725_030708_27a336","context":124730}`.
  The cost field was omitted rather than presented as metered spend. The
  resume receipt was `job-2`: session advanced from
  `20260725_030750_109ecb` to `20260725_030758_431769` and the answered turn ended
  `done`.
- Verification in job-225: `gofmt -l .` and `git diff --check` were clean;
  `go vet ./...` passed using the host's already-populated read-only module cache;
  `TestSkillInstallAllUsesCanonicalEmbeddedSkill` passed, confirming the Hermes
  install target receives the updated roster text. The full suite passed every
  package except the already-tracked `TestCodexPassthroughs` detached-runner
  `t.TempDir` cleanup race, which reproduced on the full rerun and two focused
  reruns (`unlinkat .../jobs/job-1: directory not empty`). The first unisolated run
  also inherited the worker job's notifier config; with `LEGWORK_CONFIG=/dev/null`,
  all affected doctor tests passed.

## Friction

- The injected per-job Go module cache began empty while worker network access was
  denied, so verification required pointing Go at the host's existing read-only
  module cache. A read-through shared module cache would make the isolated default
  work without a manual override.
- E2E subprocesses inherited the worker's default notifier configuration, making
  otherwise healthy doctor tests attempt an unavailable supervised-job callback.
  The test harness would be more hermetic if its baseline environment cleared
  `LEGWORK_CONFIG` unless a test explicitly supplies one.

### Review verdict

- Opus/high job-226: `SHIP`. Guide, SKILL, README, AGENTS smoke, adapter behavior,
  and authenticated receipts are mutually consistent.
- The empty Hermes umbrella remainder on ROADMAP was removed during closeout.
- Host receipt `verification:job-225:1784958749490478519` passed the complete suite.
