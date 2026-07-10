# Exact Codex `xhigh` passthrough

Status: later · Priority: P2 · Origin: 2026-07-10 roadmap dogfood · Depends: — · Workspace: —

## Goal

When an orchestrator explicitly requests Codex `--effort xhigh`, Legwork sends
`model_reasoning_effort="xhigh"` instead of silently reducing it to `high`.

## Desired behavior

- Preserve the existing effort vocabulary: `low|medium|high|xhigh|max`.
- Pass `low`, `medium`, `high`, and `xhigh` through to Codex unchanged.
- Keep `max` conservative until Codex exposes a stable capability signal; this task
  does not need a model registry or adaptive effort policy.
- If a selected model/provider rejects `xhigh`, surface that provider failure clearly.
  Do not retry at a lower effort without an explicit orchestrator decision.
- Remove help and documentation claims that Codex always tops out at `high`.

The existing persisted `effort` field remains the dispatch receipt. This small fix
does not introduce requested/resolved metadata, migration logic, or new status fields.

## Acceptance criteria

- Codex command tests prove `xhigh` reaches the CLI unchanged and `max` retains its
  documented conservative mapping.
- `run --help`, `ws review --help`, the guide, skill, and README describe the same
  behavior.
- A temporary-state real Codex smoke with `gpt-5.6-sol` and `--effort xhigh`
  completes successfully and reports non-zero context.
- `gofmt -l . && go vet ./... && go test ./... -count=1` passes.

## Non-goals

- Model capability discovery or per-model effort tables.
- New metadata fields, legacy-job migration, or `doctor --effort`.
- Profiles, fallback policy, automatic retries, or broader JSON-envelope work.

## Log

- 2026-07-10: Direct Codex CLI 0.144.1 probe confirmed `gpt-5.6-sol` accepts
  `model_reasoning_effort="xhigh"` while installed Legwork still clamps it to `high`.
- 2026-07-10: `ws-68` was intentionally discarded without landing. The first task
  draft expanded this passthrough into a 16-file policy system; this version restores
  the proportional product scope.