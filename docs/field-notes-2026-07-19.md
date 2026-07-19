# Field notes: durable notifier-origin capture

Date: 2026-07-19

Legwork version used by the orchestrator during this run:

```text
version: dev
commit: 453ce1c7635c
dirty: false
```

## Run shape

A Sol/medium implementation job in `ws-84` followed by an Opus 4.8/high frozen-checkpoint review. The implementation added generic immutable notifier-origin capture so detached jobs can wake an external orchestrator without exposing routing authority to workers.

## Friction observed

The worker's injected job-local `GOMODCACHE` was empty while the host already had a complete module cache. Its first required Go verification therefore attempted blocked network downloads. It recovered by pointing `GOMODCACHE` at the host cache while retaining a job-local writable `GOCACHE`.

This is durable product signal: project-agent environments should expose reusable dependency caches read-only when possible while keeping build and temporary caches isolated and writable. The issue belongs with project-agent environment/bootstrap work rather than this notifier task.

## Orchestrator notes

The native `ws review` checkpoint and digest made the security review auditable. A tracked `legwork wait` was still required for this run because the new Hermes wake bridge was not yet installed—the exact gap this task closes.
