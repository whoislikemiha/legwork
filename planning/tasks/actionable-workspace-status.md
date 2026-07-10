# Actionable workspace and job status

Status: next · Priority: P1 · Origin: 2026-07-10 orchestration dogfood · Depends: quality-receipts, external-verification-receipts · Workspace: —

## Goal

One read-side command should answer “what happened, is this workspace ready, and what should I do next?” without making an orchestrator join `status`, `events`, `diff`, reviewer JSON, verification notes, and git state by hand.

## Context & design

- Add `legwork ws status <ws>` with human and JSON output.
- Summarize workspace lifecycle, latest implementation/review jobs, structured review verdict, verification receipts, dirty/diff state, commit/ahead/behind/merged facts, active lock, and retention/disposition.
- Emit deterministic `attention` and `next_actions` entries such as review, external verify, fix findings, commit, merge, close, or resolve conflict.
- Improve job `status` with the same action vocabulary where applicable; `blocked: verify` must point at the verification operation rather than generic resume advice.
- Read persisted facts only. Do not infer SHIP from prose or silently mutate state while rendering.

## Constraints

- No database/daemon and no pipeline semantics; this is a truthful rollup over existing files/git facts.
- Never weaken close/review gates.
- Noninteractive, `--json`, stable exit codes, no giant task/result bodies.
- Tests cover empty/new workspace, active implementation, blocked verify, SHIP/FIX review, dirty/untracked, committed/unmerged, merged/unclosed, and closed states.

## Blockers

Structured review verdicts and external verification receipts improve fidelity; ship a useful partial rollup first if dependencies are staged.

## Log
