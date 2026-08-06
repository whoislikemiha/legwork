# Eval codex orchestrator driver

Status: next · Priority: P2 · Origin: 2026-08-06 split from orchestration-eval at its close · Depends: codex login on this machine · Workspace: —

## Goal

Measure a codex-model orchestrator driving the legwork CLI, using the eval's
existing `cmd:` driver seam — the skill says orchestrators can be any agent;
today only claude is measured.

## Design

A wrapper script passed as `-orchestrator cmd:<script>`: invoke `codex exec`
with the legwork skill as instructions and `EVAL_GOAL` as the prompt, sandbox
config permitting `legwork` on PATH (the shim). Open questions the first run
answers: whether codex's sandbox tolerates the shim'd PATH, how to surface the
quiz (the claude driver resumes a session; codex chains session IDs), and
whether permission denials are observable enough to count fumble-equivalents.

## Constraints

- The scorer stays orchestrator-agnostic: only the driver layer may know codex
  specifics.
- Cost is 0 on subscription; still check `context` per the CLAUDE.md smoke
  guidance, and use task-shaped goals (already the case for all scenarios).
