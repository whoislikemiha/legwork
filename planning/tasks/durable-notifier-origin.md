# Durable notifier origin environment

**Priority:** P1

## Goal

Allow a detached Legwork job to invoke a generic notifier command with the immutable origin context captured at initial dispatch, so external orchestrators such as Hermes can provide an opaque return capability without Legwork knowing platform or session semantics.

## Design

Add an opt-in `capture_env` list under `[notify]`.

At initial `run`, Legwork validates the exact configured environment names, snapshots present and absent values once, and stores the versioned origin privately under the job directory with mode `0600` and atomic replacement. The snapshot is not part of public job metadata, status, events, transcripts, or notifier JSON.

Captured names are removed from the detached runner and worker environments. Every notifier call loads the original snapshot and restores only names still allowed by current `capture_env`; resumes, answers, approvals, and verification never replace the original route with their caller's environment. Jobs launched with a variable absent remain normal and notify with that variable explicitly absent even when later actions come from another session.

The notifier payload and event schema remain unchanged. Legwork stays generic: no Hermes, Discord, webhook, profile, or session parsing in core.

Initial integration target:

```toml
[notify]
command = "hermes events publish"
capture_env = ["HERMES_EXTERNAL_EVENTS_TOKEN_FILE"]
```

## Constraints

- Exact environment-name allowlist only; no prefix or wildcard forwarding.
- Validate POSIX-style names, reject duplicates, and bound count/value size.
- Never capture arbitrary `HERMES_*` variables or document secrets as suitable values.
- Failure to persist a configured present origin fails dispatch before runner spawn.
- Legacy jobs without a snapshot fail closed by clearing configured captured names for notifier execution.
- Removing a name from current config immediately revokes future notifier injection for existing snapshots.
- Captured values must never reach worker commands.
- Existing notifier configs without `capture_env` retain current behavior.
- No daemon, database, network transport, or notifier payload schema change.

## Acceptance criteria

- Unit tests cover validation, set/absent snapshots, atomic `0600` storage, overlay/scrub behavior, corruption, and shell-metacharacter values as data.
- E2E fake-agent tests prove session A remains the notifier origin after resume/verify from session B, absent remains absent, simultaneous jobs do not cross routes, and public surfaces never expose captured values.
- Runner tests prove captured names are absent from worker environments while unrelated variables remain.
- Doctor validates capture configuration and probes the notifier with a transient caller snapshot.
- Docs stay synchronized in `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md`; `DESIGN.md` records the generic boundary.
- `gofmt -l .`, `go vet ./...`, and `go test ./... -count=1` pass.

## Non-goals

- A Hermes-specific notifier implementation in Legwork.
- Publish-time routing fields or platform identifiers in notifier payloads.
- General notifier sandboxing or secret-store integration.
- Changing the worker status contract or adapter command construction beyond environment scrubbing.
