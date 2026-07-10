# Verify the ask-early contract with real agents

Status: later · Priority: P2 · Origin: AUDIT A4 · Depends: — · Workspace: —

## Goal

Determine whether the injected “ask early instead of guessing” rule changes real
Claude and Codex behavior. The plumbing is tested, but the production corpus recorded
zero `needs-input` turns and zero `answer` round-trips across 96 jobs.

## Evaluation

Run a small, repeatable real-agent matrix in temporary state directories:

- a clearly specified task that should complete;
- a materially ambiguous but safe task that should ask before acting;
- a preference question the orchestrator can answer and resume;
- an impossible/environmental case that should be `blocked`, not `needs-input`.

Record agent, model, rules version, outcome, question quality, and whether the answer
continuation completes in the same session. Prompts must be task-shaped and must not
directly tell the model which status to emit.

## Decision after evidence

- If both adapters reliably ask, keep the contract and add the real-smoke recipe.
- If wording changes improve behavior, make the smallest rules update and re-run the
  parser/real-agent checks together.
- If behavior remains unreliable, document ask-early as best-effort rather than a
  guarantee; do not keep strengthening prompt prose indefinitely.

## Acceptance criteria

- The fake-agent `needs-input -> answer` contract remains zero-spend CI coverage.
- At least one real Claude and one real Codex model are tested against the same
  behavioral cases.
- Results are reproducible enough to justify either the wording or a truthful docs
  downgrade.
- Any rules change is tool-owned, parser-compatible, and verified with the full gate
  plus real-agent smoke.

## Non-goals

- Forcing every ambiguity into a question or adding a classifier for model intent.

## Log
