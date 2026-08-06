# Non-workspace auto-close + gc retention coherence

Status: next · Priority: P1 · Origin: 2026-08-06 DESIGN-vs-code audit (finding 4) · Depends: — · Workspace: —

## Goal

Make close semantics and gc's retention clock tell the same story. DESIGN §2 says
non-workspace jobs auto-close on completion ("their artifact was the answer");
that never shipped — `ack` is manual — but gc already *assumes* it: for
non-workspace jobs the transcript-retention clock anchors at finish time
(`internal/gc/gc.go` retentionAnchor), so an unacknowledged job's transcript is
timer-deleted. Separately, auto-gc + timer deletion is **on** by default where
DESIGN §8 says auto-delete-by-timer stays off. Cleanup must be "gated on
acknowledgment, never on time alone" again.

## Decide, then implement

Two coherent resolutions — pick one at pickup (lean 1; it matches the design and
the existing `ack` verb stays for abnormal cases):

1. **Implement auto-close**: runner finishing a terminal non-workspace job stamps
   `Closed` (actor: tool, disposition recorded). Retention anchor stays as-is and
   becomes truthful. `ls` stops nagging about jobs that need no acknowledgment.
2. **Honor the gate**: keep manual `ack`, anchor retention at `Closed` for every
   job kind, and let `ls`/notifier nag on unacked finished jobs (the §8
   fails-safe-and-loud path).

Either way: transcript deletion for a job that was never closed/acked must not
happen on time alone.

## Acceptance criteria

- gc contract tests: an unacknowledged finished non-workspace job's transcript
  survives past the retention window under resolution 2, or the job is provably
  auto-closed at finish under resolution 1.
- Compression (lossless) may stay time-based; deletion may not.
- `interrupted` and `needs-input` jobs are never auto-closed under either
  resolution — only clean terminal states.
- DESIGN §8's "auto-delete-by-timer off by default" is either restored or
  explicitly amended with the rationale, in the same change.

## Non-goals

- Workspace close semantics (correct today, tripwire stays).
- Changing retention durations.

## Log
