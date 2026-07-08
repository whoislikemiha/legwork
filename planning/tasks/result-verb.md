# `result` verb — first-class access to a job's final report

Status: next · Priority: P1 · Origin: orchestrator field feedback 2026-07-08 (money_intelligence waves 9–11) · Depends: — · Workspace: —

## Goal

`legwork result <job|run>` prints the job's final message (the `result` field) to stdout,
raw. The worker's final report is the *product* of every dispatch turn; fetching it should
not require JSON surgery.

## Context & design

- Observed friction: the single most-repeated command in the 2026-07-08 orchestrator
  session was `legwork status job-X --json | python3 -c "...print(d['result'])"`. Monitor
  `tail` truncates `text` events, so the final report is effectively unreachable except
  through that pipeline.
- Behavior: latest turn's result by default; `--turn N` for earlier turns if turn results
  are retained; `--json` wraps it in the standard envelope for scripts. Exit non-zero if
  the job has no result yet (still running) — that makes `legwork wait ... && legwork
  result ...` a natural pair (see [per-job-wait.md](per-job-wait.md)).
- Accept either a job id or a run name (a run name resolves to its most recent job) —
  consistent with [unified-addressing.md](unified-addressing.md).

## Constraints

- Read-side only; no new state. Raw output to stdout by default (pipe-friendly), no
  decoration — decoration belongs to `status`.

## Blockers

None.

## Log

- Implemented `legwork result <job|run>` in `main.go`: raw stdout by default,
  `--json` envelope, run labels resolving to the newest job, `--turn N` for retained
  transcript results, and non-zero exit for active/queued jobs with no latest result.
- Added e2e coverage in `test/e2e_test.go` for raw output, JSON output, run-label
  resolution, retained turn selection, and active-job refusal.
- Updated docs in `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and
  `README.md`.
- Verification blocked by sandbox read-only Go build cache. Exact failing command:
  `go test ./test -run 'TestResult' -count=1`; failure:
  `open /home/miha/.cache/go-build/88/8823b627c5300e97093d7e7ff4f9f7b0ff54aa2c34d487daa81e2abcfe0959a8-d: read-only file system`.
- Review fix: `retainedResults` now reads retained compressed transcripts by falling
  back from `transcript.jsonl` to `transcript.jsonl.gz`; added
  `TestResultTurnReadsCompressedTranscript`.
- Review fix: default raw `legwork result <job|run>` no longer reparses the transcript
  just to populate `turn`; the count is computed only for `--json`.
- Review fix: added e2e coverage for missing-status-block reparse preserving
  `blocked`, and for codex-parser replay across retained turns.
- Review fix: lengthened the active-job result refusal test sleep window to avoid CI
  timing flakes, and made explicit `--turn 0` invalid.
- Verification passed with sandbox cache override:
  `GOCACHE=/tmp/lw-go-cache go test ./test -run TestResult -count=1`,
  `gofmt -l .`, `GOCACHE=/tmp/lw-go-cache go vet ./...`,
  `GOCACHE=/tmp/lw-go-cache go build ./...`, and
  `GOCACHE=/tmp/lw-go-cache go test ./... -count=1`.
