# Landing assistant — `close --merge-into` (minimal) / `land` workflow (full)

Status: next · Priority: P1 · Origin: pre-system roadmap remainder, promoted with field evidence 2026-07-08 · Depends: — · Workspace: —

## Goal

Make landing a workspace a single safe operation instead of a hand-run `git merge` +
`close --merged` pair. Minimal form: `legwork close <ws> --merge-into <ref>` performs the
`--no-ff` merge itself (from anywhere) and closes. Full form (the original remainder): a
`land` workflow that prepares the target branch, applies the workspace commits, detects
conflicts, runs configured validation (`worktree.toml` verify hook), and prints a
checklist — explicitly not a `pr` verb.

## Context & design

- **Field evidence (2026-07-08)**: the orchestrator ran `git merge legwork/ws-52` *inside
  the workspace tree* by accident — HEAD there IS the branch, so git reported "Already up
  to date" and succeeded at doing nothing. Caught only because the subsequent
  `close --merged` state was double-checked. An orchestrator that misses this moves task
  files and flips its project board for an unmerged branch: silent false-landed state.
  The existing non-ancestor guard on `close --merged` is exactly right — this task moves
  that safety one step earlier so the slip cannot happen.
- Minimal step is cheap and independent: resolve `<ref>`, refuse if the merge would run
  with the workspace branch checked out as HEAD (the observed slip), perform `--no-ff`
  merge with a generated-or-provided message (`-m`), then run the existing close path.
  Conflicts → abort the merge, report `blocked: verify`-style structured output, leave
  the repo clean.
- Full `land` adds: pre-merge fetch/base-drift check (overlaps `ws refresh`), optional
  validation hook run OUTSIDE any sandbox, checklist output. Ship minimal first.

## Constraints

- Never force, never rebase, never push — landing is local; pushing stays the operator's
  call. Conflict = clean abort + non-zero exit, never a half-merged tree.
- `--json` envelope; stable exit codes distinguishing merged / conflict / guard-refused.

## Blockers

None.

## Log

- Implemented minimal `close --merge-into <local-branch>` landing path. Touched
  `workspace_cmds.go`, `internal/workspace/workspace.go`, `test/workspace_test.go`,
  `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md`.
- Added guarded local `git merge --no-ff` support with generated/provided `-m`
  message, conflict abort, workspace-branch/self-merge guard, dirty target guard,
  local-branch-only target guard, JSON success/error envelope, and distinct exit
  codes: `0` merged, `1` conflict, `3` guard-refused.
- Added e2e coverage for successful `--merge-into`, workspace-branch target guard,
  and conflict abort/clean target checkout.
- Verification blocked by sandbox read-only Go cache before tests ran. Exact command:
  `go test ./test -run 'TestCloseMergeInto|TestCloseMerged' -count=1`; failure:
  `open /home/miha/.cache/go-build/a7/a78caedf02e275969223c8268f59c4a7fb706fa0495c86414cdee846f973b535-d: read-only file system`.
- Review finding 1: fixed post-switch failure cleanup in `internal/workspace/workspace.go`
  by recording the original source checkout and restoring it after target-HEAD
  verification failures, merge conflicts, and other post-switch merge failures.
- Review finding 2: extended `test/workspace_test.go` with guard e2e coverage for
  uncommitted workspace work, dirty target/source checkout, remote target refs, and
  unresolved target refs; the conflict test now proves the original branch is restored.
- Review findings 3-4: adjusted `internal/guide/guide.md` wording to emphasize that
  the merge runs from the source checkout with a post-switch HEAD recheck, and that
  `blocked.kind` is the authoritative JSON discriminator while `1`/`3` cover
  conflict/guard-refused for `--merge-into`.
- Verification completed with approved env-only cache override:
  `GOCACHE=/tmp/lw-go-cache go test ./test -run 'TestCloseMergeInto|TestCloseMerged' -count=1`,
  `GOCACHE=/tmp/lw-go-cache gofmt -l .`,
  `GOCACHE=/tmp/lw-go-cache go vet ./...`,
  `GOCACHE=/tmp/lw-go-cache go build ./...`, and
  `GOCACHE=/tmp/lw-go-cache go test ./... -count=1` all passed. Real-agent smoke
  checks intentionally skipped per orchestrator guidance.

## Friction

- Running Go verification in the worker sandbox failed because the default
  `/home/miha/.cache/go-build` is read-only; a worker-visible writable Go cache or
  injected `GOCACHE` would let verification run without changing project config.
