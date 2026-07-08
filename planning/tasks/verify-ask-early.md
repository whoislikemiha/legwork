# Verify the ask-early path actually fires

Status: later · Priority: P2 · Origin: AUDIT A4 · Depends: — · Workspace: —

## Goal

Prove the ask-early contract works. Across 96 real jobs there were **0 `needs-input`** finishes
and **0 `answer` round-trips** — the whole corpus leaned on `resume`. Either every task was
well-specified, or the injected ask-early bias is not actually triggering `state: needs-input`.
Establish which, and fix or document the result.

## Context & design

- The injected contract claims an ask-early bias: "if a decision is ambiguous and materially
  affects the outcome, end your turn with `state: needs-input` rather than guessing" (DESIGN §4).
  This deliberately inverts the usual plow-ahead bias. If it never fires in practice, the bias is
  either dead or the tasks never hit it.
- The fake-agent `needs-input→answer` loop is *already* an e2e test (AGENTS.md: the suite
  covers "needs-input→answer"), so the plumbing works. The gap this task closes is **real-agent
  behavior**: in 96 production jobs no worker ever chose `needs-input`. Confirm whether the
  injected wording actually makes a real claude/codex worker stop-and-ask on an ambiguous task.
- Then, with a real agent (cheap smoke), confirm an ambiguous task actually produces
  `needs-input` and not a guess — i.e. the injected wording earns its place. If real agents don't
  ask, either strengthen the wording (`internal/rules`) or accept resume-only and drop the claim
  from the docs.

## Constraints

- The fake-agent e2e must run in CI with zero API spend (the contract suite is the quality story).
- Any rules-text change is tool-owned and travels with the parser version.
- Don't invent a `needs-input` shape that diverges from the existing event schema / status-block
  contract — reuse `question:` from the status block.

## Blockers

None.

## Log
