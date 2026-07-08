# `ws review` verb — first-class implement→review→fix

Status: next · Priority: P1 · Origin: AUDIT D1 (+ field-notes 2026-07-07 §"shipped quality") · Depends: — · Workspace: —

## Goal

Make the independent-reviewer step a first-class verb: `legwork ws review <ws>` dispatches a
read-only reviewer against the workspace diff with the right defaults (big model, high effort,
"review the diff, not the tree"). Encodes the one opinion the corpus proves about quality:
**never land implementer output without an independent high-effort reviewer.**

## Context & design

The data is the argument:

- First-pass SHIP was **exactly 3/8** on P1 features (SHIP: `job-71`, `job-76`, `job-81`; FIX:
  `job-58`, `job-61`, `job-70`, `job-75`, `job-83`). Across all 17 first-pass implementation
  reviews: **7 SHIP / 10 FIX** — review caught real, shippable-looking bugs on ~62% of first
  passes.
- Bugs an independent reviewer caught that would otherwise have shipped: XFF rate-limit bypass
  (`job-42`), quarterly-as-monthly (`job-58`), refresh-rotation race → two token pairs
  (`job-59`), unescaped LIKE wildcards (`job-61`), CSV expense↔income flip + OFX XML DoS
  (`job-70`), a net-worth regression the *fix turn itself introduced* caught on re-review
  (`job-75`), a `formatAmount` crash (`job-83`), an infinite retry loop (`job-93`).

Design:

- `ws review <ws>` = a read-only job (claude plan mode / codex read-only sandbox) seeded with the
  workspace diff (reuse `legwork diff <ws>`, incl. untracked) so the reviewer never has to
  rediscover the change vs base — the recurring "reviewers need the diff, not the tree" friction.
- Sensible defaults surfaced as flags: `--model` (default the configured big model),
  `--effort high`, an adversarial review prompt template, and structured-verdict output
  (`{verdict: SHIP|FIX, findings:[{file,line,severity,detail}]}`) so merges can gate on "zero
  critical". Ties into the quality-receipts task (structured verdicts, visible in status/serve).
- It is a recipe made first-class, not a pipeline engine: it dispatches one reviewer job attached
  to the workspace lineage; the orchestrator still owns routing the verdict (resume implementer /
  fresh fix job / land). Do NOT auto-merge or auto-fix.

## Constraints

- Read-only is harness-guaranteed (plan mode / read-only sandbox), not prompted.
- Must not become PR ownership or auto-merge (both rejected). Landing stays orchestrator judgment.
- Reviewer runs cheap-per-token on codex subscription but the *value* is high effort — default to
  the big model + high effort; document the "reviewer jobs are the highest-value tokens" recipe.
- Docs travel in threes (guide/SKILL/README) — this is a new verb.

## Blockers

Overlaps `diff --since-last-review` (nice-to-have for incremental re-review) but does not depend
on it — first pass reviews the whole diff. Structured verdict shape should be designed with the
quality-receipts task.

## Log

- Implemented `legwork ws review <ws>` as a read-only workspace-attached job seeded with the current workspace diff, defaulting reviewer effort to `high` and returning normal job output/`--json`.
- Refactored run dispatch into shared command-layer plumbing so `run` and `ws review` use the same validation, workspace lock, run-event, spawn, and gc path.
- Added e2e coverage for review job metadata/prompt/run events and for active-workspace lock refusal; updated `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md`.
- Verification blocked by sandbox read-only Go cache on exact command: `go test ./test -run 'TestWorkspaceReviewDispatchesReadOnlyDiffJob|TestWorkspaceLock' -count=1` failed with `open /home/miha/.cache/go-build/ee/ee923af65ddaac673e591eb0a4c329a85ea5e98d5c02ff7525461c5ec9371366-d: read-only file system`.

## Friction

- Running Go verification used the default `GOCACHE` outside the writable workspace and failed on a read-only cache; worker instructions say to stop rather than set a writable cache ad hoc. A per-job writable Go cache/TMPDIR default would let workers verify without policy ambiguity.

## Verdict

Review job-124 (opus, high): **SHIP** first pass. Orchestrator verification: suite green in
worktree and on main after merge (README/guide conflicts vs ws-58/ws-59 resolved by union;
one pre-existing flake TestCodexPassthroughs teardown race, 5/5 green on retry). Landed on
main 2026-07-08 via merge of legwork/ws-54.
