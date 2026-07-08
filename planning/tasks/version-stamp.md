# Version stamping — commit + date in `legwork version`

Status: next · Priority: P2 (small; promoted 2026-07-08) · Origin: Miha + orchestrator exchange 2026-07-08 · Depends: — · Workspace: —

**Why promoted:** CLAUDE.md instructs orchestrators to record `legwork version` in
field notes — a dangling reference until this lands — and the 2026-07-08 handover
transcript shows the "is my installed binary current?" question burning a detour on
every cold start. Small task, disproportionate friction.

## Goal

`legwork version` prints something identifying: version (or `dev`), commit hash, dirty
flag, build date. "Which build are you on" must not require file-mtime forensics.

## Context & design

- Observed: with legwork being developed in parallel with active orchestration use, the
  question "does my installed binary have change X?" came up and was only answerable by
  comparing the binary's mtime (22:32) against `git log` timestamps in the source repo.
- Standard Go pattern: `-ldflags "-X main.commit=$(git rev-parse --short HEAD) -X
  main.date=..."` in the Makefile/goreleaser config; fall back to
  `runtime/debug.ReadBuildInfo()` (vcs.revision, vcs.modified) so even a bare `go
  install`/`go build` gets stamped for free.
- Include the same fields in the `--json` envelope (self-describing output overlaps the
  command-grammar remainder).

## Constraints

- goreleaser release builds and local dev builds both stamped; no network at build time.

## Blockers

None.

## Log
