# Orchestrator profiles

Status: later · Priority: P1 · Origin: user-standard GPT-5.6 Terra/high implement + Opus/high review pairing · Depends: — · Workspace: —

## Goal

Make repeatable agent/model/effort/access defaults first-class so orchestrators do not restate or accidentally drift from the intended implementation and review policy on every dispatch.

## Context & design

- Named profiles in config, for example `implement` and `review`, covering agent, model, effort, read-only mode, timeout, fallback model, and append-prompt file.
- `run --profile <name>` and `ws review --profile <name>` resolve to explicit dispatch metadata; returned JSON records both profile and fully resolved values.
- Explicit command flags override profile values deterministically.
- `doctor --profile <name>` probes the resolved agent/model pairing.
- Profiles are dispatch presets, not pipelines, task graphs, queues, or policy engines.

## Constraints

- Preserve existing direct flags and backward compatibility.
- Never hide the resolved model/effort/access in status/events.
- Reject incompatible combinations such as Codex fallback models.
- Noninteractive, `--json`, stable errors, config migration tests.

## Blockers

None.

## Log
