# First-class Legwork flows

Status: done · Priority: P1 · Origin: Miha 2026-08-02 · Depends: existing recipes and quality/verification receipts · Workspace: ws-89

## Goal

Make **flows** the documented multi-role delivery architecture above Legwork's existing job/workspace substrate. A flow is a recipe with explicit role contracts, handoff artifacts, risk-proportional routing, evidence hygiene, and receipt-backed transitions. It is not a pipeline engine, scheduler, new job state, or `flow` verb: Legwork remains a dumb substrate and the orchestrator composes the work.

## Accepted architecture

### Roles

Define explicit duties, inputs, outputs, escalation rules, and forbidden behavior for:

- **Orchestrator:** persistent decision-maker and sole CLI/roadmap/landing owner. Preserves user intent and scope across projects, routes work (direct handling vs. the full delivery flow vs. the high-risk extension) and handles simple questions/truly trivial changes directly, approves plans, decomposes work, routes model/quota use, answers routine worker questions, supervises context health, curates compact evidence, enforces verification and independent-review gates, serializes landing, harvests friction, and escalates only genuine product decisions. It must react to stale diffs, repeated failure, poisoned context, stale receipts, and scope drift rather than blindly resuming.
- **Planner:** fresh read-only architect. Investigates before prescribing; locks down contracts, interfaces where compatibility matters, invariants, data flow, failure semantics, files, dependencies, risks, migrations, and acceptance/verification criteria. It is precise about contracts but not speculative private mechanics, and aims for contracts precise enough that implementation is nearly mechanical execution without removing implementer judgment. Produces a durable plan artifact and never edits code.
- **Implementer:** mutating workspace worker. Implements the approved plan, writes appropriate tests, reports deviations and risks, keeps scope bounded, and returns a compact evidence-oriented result rather than logs. It never commits, silently changes a public contract, invents success criteria after implementation, or expands scope without asking.
- **Verifier/tester:** an independent verification stack, not automatically another standing agent. The planner defines acceptance gates; a deterministic host-side boundary executes them and records receipts; the reviewer independently reproduces targeted checks. Architectural/security/data-integrity work adds a fresh adversarial test engineer. User-visible UI changes add a fresh experience verifier using a real browser and retaining screenshots, console/network evidence, and a concise usability/accessibility verdict. Normal test code may be written by the implementer; adversarial/acceptance criteria must not originate solely from the implementer.
- **Command/evidence distiller:** the one shared mechanical role — a fresh cheap-model job in a disposable context, dispatched by the orchestrator, used proactively by both the orchestrator and the implementer whenever a command is expected to emit chunky output. Inputs are the exact approved command/working directory, the question the caller needs answered, an output budget/shape, and a redaction/retention policy. It preserves deterministic exit status, argv, duration, and a sanitized artifact pointer/digest, and returns only salient failures/warnings/metrics — never the full transcript, never an edit to implementation files, never a converted nonzero exit.
- **Independent reviewer:** fresh read-only session, different model family from the implementer, seeded with approved plan + exact diff but not the implementer's self-assessment. It checks plan traceability, correctness/edge cases, test adequacy, security/trust boundaries, data integrity/concurrency/idempotency, compatibility/migrations/public contracts, simplicity/complexity/scope, cleanliness/maintainability/drift, relevant performance/operability, and documentation consistency. Findings are concrete, evidenced, severity-ranked, and identified for FIX routing. It never edits code or owns landing.

### Model policy example

Document the accepted high-intelligence/cost-balanced example without hard-coding it into substrate semantics:

- persistent orchestrator: Sol 5.6, high, standard speed;
- planner: Fable 5, default/high, fresh read-only;
- implementer: Sonnet 5, default, workspace;
- independent reviewer: fresh Sol 5.6, high, read-only;
- command/evidence distiller: Luna low/medium;
- max effort and fast mode only by exception.

Profiles are the eventual wiring; flows name roles and remain valid as model rosters change.

### Routing

1. **Direct/orchestrator path:** simple project questions, lookups, and truly trivial changes are handled by the orchestrator directly, no planner/implementer/reviewer ceremony; if files change, a focused deterministic check runs and its result is reported. Promote immediately on ambiguity, multiple interacting files, public-contract/security/data/concurrency implications, migration risk, non-obvious acceptance criteria, or UI-visibility.
2. **Full delivery flow (default for all non-trivial implementation):** planner → approved plan artifact → Sonnet implementer → deterministic verification → fresh Sol review → FIX/reverify/re-review as needed → orchestrator lands.
3. **High-risk extension:** the same full flow, adding design-only Fable plan + adversarial design review before code; decomposed implementation; fresh adversarial tester where useful; real-browser experience verification for UI; fresh Sol review each round; optional second independent review for security/public-contract/data-integrity changes; human checkpoint only for genuine product/risk decisions.

Independent review is mandatory for every non-trivial change; routing never downgrades it — do not weaken the existing close/review discipline to sampling.

### Context and evidence hygiene

Principle: **lossless capture outside decision-maker context; distilled evidence inside it.**

- Orchestrators consume status, compact receipts, results, notes, and artifact pointers. Raw transcripts, full build/test output, and giant browser logs do not enter the persistent orchestrator context routinely.
- Worker final reports are concise and decision-oriented. Long evidence is written to an addressable artifact first.
- Exit code, command argv, duration, environment-safe redacted log pointer/digest, failures, warnings, and summary metrics must remain distinguishable. A model summary cannot override deterministic exit status.
- Current `legwork verify` is truthfully documented as an exact-job `blocked.kind=verify` handoff, not a general verification gate. File a separate substrate task for a general workspace evidence/check receipt that captures full redacted output outside the caller context and prints a compact receipt.
- The shared command/evidence distiller is dispatched proactively by the orchestrator around commands expected to be chunky (and requestable by the implementer through the orchestrator boundary), not summoned on-demand after ingesting a transcript. It gets the evidence artifact in a disposable context and returns at most the normalized report; raw evidence remains drillable by pointer.

### Flow ledger

Document recipe-layer states reconstructed from run notes and existing receipts, not new tool states:

`intake → planned → implemented → verified → reviewed → landed → harvested`

- routing/task note guards intake;
- plan artifact guards planned;
- non-empty diff + compact implementer result guards implemented;
- deterministic gate/check receipt guards verified;
- current-diff SHIP receipt guards reviewed;
- commit + close receipts guard landed;
- roadmap/task move + friction harvest + gc guard harvested.

FIX returns to implemented with finding IDs; failed verification returns with only distilled failures and the raw-log pointer; poisoned context starts a fresh seeded session; stale review receipts never authorize landing.

## Deliverables

1. Restructure the canonical `internal/guide/guide.md` Recipes material into a first-class `## Flows` section rather than appending a duplicate playbook. Include compact lane, role, evidence, state, and reviewer-contract material plus executable normal/high-risk flow shapes.
2. Mirror a condensed version in `skills/legwork/SKILL.md`, keeping fixed prompt growth tight.
3. Update `README.md` with one concise flows explanation and guide pointer.
4. Update `DESIGN.md` documentation section to freeze flows at the recipe/orchestrator layer, not as verbs.
5. Add bounded follow-up planning tasks for:
   - general workspace evidence/check receipts with lossless redacted log artifacts and compact output;
   - richer structured review dimensions/findings tied to the current diff;
   - role profiles/model wiring (amend the existing profile task rather than duplicating it).
6. Update the ROADMAP with one Flows umbrella and dependencies. Preserve unrelated current roadmap work.

## Constraints

- Guide is canonical; docs travel in threes.
- No new verbs, states, scheduler, daemon, database, queue, or pipeline semantics in this task.
- Do not claim current `verify` supports ordinary done jobs.
- Do not make implementer self-reports evidence of correctness.
- Do not duplicate injected worker rules in role preludes or docs.
- Keep public examples generic; no private project/profile identifiers.
- Keep existing review tripwires and fail-closed receipt behavior.

## Acceptance criteria

- A cold orchestrator can determine role duties, choose a lane, name each handoff artifact/receipt, handle FIX/failure/context drift, and know what it must never ingest into persistent context without re-deriving policy.
- Planner/implementer/verifier/reviewer responsibilities are non-overlapping enough to preserve independence without adding redundant agents to normal work.
- Reviewer expectations explicitly include correctness, tests, security, data/concurrency, compatibility, complexity/cleanliness/drift, operability/performance when relevant, and docs.
- UI work has an explicit real-browser independent verification path.
- Current substrate capabilities and limitations are stated truthfully.
- `gofmt -l .`, `go vet ./...`, and `git diff --check` pass. `go test ./... -count=1`
  introduces no failure category beyond failures reproduced on the clean base tree.

## Log

- 2026-08-03: Final orchestrator verification after independent Sol review job-246
  returned `SHIP` with no findings. On both ws-89 and a clean detached worktree at its
  base commit, `git diff --check`, `gofmt -l .`, and `go vet ./...` passed. Full
  `go test ./... -count=1` reproduced the same local notifier `TestDoctor*` failures
  on both trees. ws-89 additionally hit the tracked `TestCodexPassthroughs` temporary-
  directory teardown race; an immediate targeted `-count=1` rerun passed. No new
  change-related failure category remains.

- 2026-08-03: Implemented. `internal/guide/guide.md`'s `## Recipes` restructured into
  `## Flows` (roles, model-policy example, lanes, evidence hygiene, flow ledger,
  executable normal/high-risk shapes, then the existing campaign/append-prompt/
  preflight/competition/design-only/reviewer-seeding/notes material as flow detail).
  `skills/legwork/SKILL.md` mirrors a condensed version under the same heading.
  `README.md` gained one flows paragraph pointing at the guide. `DESIGN.md` §12 gained
  a "flows are frozen at the recipe/orchestrator layer" note (dated, cross-referenced
  to §13's existing pipeline-engine rejection) and §13 gained an explicit rejection of
  the ledger states as new job/workspace states. Added two bounded follow-up tasks
  ([workspace-check-receipts](../tasks/workspace-check-receipts.md),
  [review-finding-dimensions](../tasks/review-finding-dimensions.md)) and extended
  [orchestrator-profiles](../tasks/orchestrator-profiles.md) in place rather than forking a
  role-profile task. ROADMAP gained one Flows umbrella entry plus the three follow-ups
  under Later, dependencies noted; no unrelated roadmap entries touched.
  `gofmt -l . && go vet ./... && go test ./... -count=1` and `git diff --check` green.
- 2026-08-03: Fixed 4 independent review findings on `internal/guide/guide.md`
  (mirrored into `skills/legwork/SKILL.md` where affected): R1 the executable Normal
  flow now materializes the saved plan into the implementer turn explicitly
  (`artifact get | run --append-prompt-file -`) instead of claiming a workspace job
  can see run artifacts on its own. R2 `legwork verify` is shown only for a job that
  actually went terminal `blocked.kind=verify`; the executable flow's `verified` step
  now documents today's real fallback for an ordinary `done` job — the orchestrator
  runs the deterministic gate host-side against `legwork ws ls --json`'s reported
  `tree` path — until the general check-receipt task ships. R3 the Roles section and
  SKILL.md now state `ws review` seeds only the diff by default, and that plan
  traceability and a different reviewer model family require explicit
  `--append-prompt-file`/`--model`; "Reviewer seeding" gained a matching bullet. R4
  restored the accepted concrete example roster (Sol 5.6 orchestrator, Fable 5
  planner, Sonnet 5 implementer, fresh Sol 5.6 reviewer, Luna mechanical/distiller) in
  the guide's Model policy subsection, still marked example-not-substrate-semantics
  and cross-referenced to orchestrator-profiles. README's flows paragraph needed no
  change (made none of these claims). Re-verified: `gofmt -l .` and `go vet ./...`
  clean, `git diff --check` clean, but the combined gate is **not** green —
  `go test ./... -count=1` fails with two baseline-reproduced failure categories
  (the local-sandbox notifier check under `TestDoctor*`, and the tracked
  `TestCodexPassthroughs` teardown flake); both reproduce identically on the
  unmodified tree with no doc changes applied, so they are pre-existing and unrelated
  to this doc-only change, not a regression it introduced. The final orchestrator
  runs its own authoritative check before landing.
- 2026-08-03: Fixed 5 round-2 review findings on `internal/guide/guide.md` (mirrored
  into `skills/legwork/SKILL.md` where affected): R2-1 the planner's `run --read-only`
  now runs in-place in the repo (`--dir R`) in the Roles description and the
  executable Normal flow, so it can actually inspect the task file and code instead of
  planning blind in a scratch dir. R2-2 the executable flow's `verified` step for an
  ordinary `done` job no longer streams raw suite output into orchestrator context or
  implies a durable receipt: it now captures full (redacted) output to a file/artifact,
  keeps only a compact exit-code note, and is explicit that this is a provisional
  fallback — not a native receipt — until `ws check`/the check-receipt task ships; the
  flow-ledger `verified` bullet gained the same caveat. R2-3 the flow ledger no longer
  claims `FIX` carries finding IDs; today's `ws review` receipt has no persisted
  per-finding ID field (only finding counts), so the guide now documents a manual
  round/index convention (`review-job-id#1`) and points at
  `review-finding-dimensions.md` for stable persisted IDs as future work. R2-4 the
  Roles/reviewer-seeding text and SKILL.md now state that `ws review`'s defaults
  (`--agent claude`, that agent's default model) do not guarantee independence from
  the implementer — an independent review needs explicit `--model` for a different
  model family and explicit `--agent` for a different adapter. R2-5 this log's prior
  entry no longer calls the gate green while `go test` failed; it now records the
  combined gate as failing with the two baseline-reproduced failure categories while
  gofmt/vet/`git diff --check` passed. Also reviewed all touched command examples for
  shell correctness/consistency (fixed an `exit "$ec"` that would have exited the
  orchestrator's script mid-recipe, replaced with a plain status `echo`; tidied two
  line-wrap artifacts). Re-verified: `gofmt -l .` and `go vet ./...` clean,
  `git diff --check` clean; `go test ./... -count=1` still fails with the same two
  pre-existing baseline categories (local-sandbox notifier under `TestDoctor*`,
  `TestCodexPassthroughs` teardown flake) — unrelated to this doc-only change. The
  final orchestrator runs its own authoritative check before landing.
- 2026-08-03: Fixed 8 round-3 review findings, mostly on the executable Normal flow
  shape in `internal/guide/guide.md` (mirrored into `skills/legwork/SKILL.md`'s
  condensed Flows section and workspace-flow example where affected). R3-1 every
  dispatched planner/implementer/reviewer job in the executable shape is now followed
  by an explicit `legwork wait <job>`, with terminal-state routing (non-`done` fixes/
  replans/escalates) before its output is trusted — no more racing an active job.
  R3-2 an explicit orchestrator approval checkpoint sits between the planner's `done`
  and calling the plan "approved" (a `legwork note` receipt records it), with FIX/
  replan on an unsuitable plan. R3-3 the provisional host-side verification loop now
  gates on exit code: nonzero returns to *implemented* with only distilled failures +
  the artifact pointer and never advances to review/landing; only exit=0 proceeds
  (the flow-ledger `verified` bullet gained the same explicit gate). R3-4 the
  verification artifact is named uniquely per attempt (`verify-ws-N-<attempt>.log`,
  attempt counter bumped on retry) instead of a fixed `verify.log`, so no step depends
  on overwrite or risks stale evidence. R3-5 the compact verification note now records
  exit code, exact argv, duration, and the artifact pointer/digest together, matching
  the evidence-hygiene contract. R3-6 the reviewed step and the Quick Reference now
  pass both `--agent` and `--model` explicitly for the independent reviewer
  (`ws review ws-N --agent <adapter> --model <model>`), consistent across the guide's
  executable shape, its Quick Reference, and SKILL.md's condensed version and example.
  R3-7 `skills/legwork/SKILL.md` and the guide's Lanes section now state real-browser
  experience verification applies to every UI-visible change regardless of lane, not
  only architectural. R3-8 `review-finding-dimensions.md` no longer claims the flow
  ledger already assumes persisted finding IDs; it now says persisted IDs are this
  task's own future deliverable, replacing today's manual `review-job-id#index`
  convention. Re-verified: `gofmt -l .` and `go vet ./...` clean, `git diff --check`
  clean; `go test ./... -count=1` still reproduces only the same two pre-existing
  baseline failures (local-sandbox notifier under `TestDoctor*`,
  `TestCodexPassthroughs` teardown flake) on this doc-only change. The final
  orchestrator runs its own authoritative check before landing.
- 2026-08-03: Fixed 10 round-4 review findings, all on the executable Normal flow
  shape in `internal/guide/guide.md` (mirrored into `skills/legwork/SKILL.md`'s
  condensed Flows section and workspace-flow example). The single monolithic block
  is now an explicit **staged command skeleton with hard-stop conditions**
  (Stage 1 plan / Stage 2 implement / Stage 3a verify-via-`blocked.kind=verify` /
  Stage 3b host-side verify fallback / Stage 4 review / Stage 5 guarded land /
  Stage 6 harvest), never a paste-and-run pipeline. R4-1 every dispatch now
  captures a distinct variable (`planner_job`, `ws`, `impl_job`, `review_job`) —
  no role ever reuses `job-N`. R4-2 every wait is followed by an explicit
  `status --json` read with a HARD STOP: non-`done` states route via "The loop"
  and never fall through; `blocked.kind=verify` is its own mutually exclusive
  branch (Stage 3a), never blanket-routed away and never run alongside the
  ordinary-`done` fallback (Stage 3b). R4-3 `eval` is gone; the fallback verify
  block runs `argv=(...)` directly, or records an explicit `sh -lc '<suite>'`
  when shell syntax is genuinely needed (`legwork verify` shows the same pattern).
  R4-4 the fallback verify block now writes to a private `umask 077` raw temp
  file, runs an explicit project-specific `<project-redactor>` step into a
  separate sanitized file, deletes the raw file, and saves/digests only the
  sanitized output — with an explicit stop-and-escalate note if no trusted
  redactor exists for the project, since `artifact save` performs no redaction
  itself. R4-5 the resume-implementer step on a failed verify no longer uses
  `tail`; it feeds only a deterministic reducer's/disposable distiller's
  normalized failure summary plus the sanitized artifact pointer. R4-6 the
  fallback verify is an explicit attempt-numbered loop: nonzero exit routes back
  to Stage 2 and never reaches Stage 4/5, and each re-verification bumps
  `attempt` and writes a new artifact rather than overwriting. R4-7 Stage 3a/3b
  are documented as mutually exclusive per implementer terminal state, and both
  require a passing result before Stage 4 review runs (mirrored in the flow
  ledger's `verified` bullet). R4-8 Stage 4 now reads the *workspace's*
  `latest_review` rollup (`legwork ws ls --json`'s `latest_review`), requiring
  `parsed=true`, `state=done`, `verdict=SHIP`, and `diff_sha256` equal to the
  diff's current digest (`legwork diff <ws> | sha256sum`, the same hash the
  receipt itself is computed from) before Stage 5's landing step, which is now
  an explicit `if`/`else` guard rather than an unconditional `commit && close`;
  any FIX/malformed/stale outcome routes back through implement → verify →
  fresh review. R4-9 `skills/legwork/SKILL.md`'s workspace-flow example no
  longer pairs `--agent codex` with `--model opus` (a Claude model on a Codex
  adapter); it uses the supported `gpt-5.6-sol` Codex model and now routes
  terminal states after both waits and gates commit/close on the same
  parsed/state/verdict/digest check as the guide, instead of unconditionally
  resuming and committing. R4-10 audited other touched examples (competition
  section's `opus`/`codex` pairing, design-only pipeline) for the same
  mixed-adapter-model defect; none found — `opus` there always pairs with
  `--agent claude`. Role contracts and lane policy are unchanged; only the
  executable shape and its two mirrors changed. Re-verified: `gofmt -l .` and
  `go vet ./...` clean, `git diff --check` clean; `go test ./... -count=1`
  reproduces only the same two pre-existing baseline failure categories
  (local-sandbox notifier under `TestDoctor*`, `TestCodexPassthroughs` teardown
  flake) on this doc-only change. The final orchestrator runs its own
  authoritative check before landing.
- 2026-08-03: Fixed 9 round-5 review findings, all on `internal/guide/guide.md`'s
  executable Normal flow shape (mirrored into `skills/legwork/SKILL.md`'s condensed
  Flows section and Workspace-flow example). R5-1 Stage 1's plan capture no longer
  redirects the planner's result into the repo tree; it writes to a private
  `umask 077 mktemp` file, is read/approved/saved as the run artifact from there, then
  shredded. R5-2 Stage 3a and the evidence-hygiene bullet now state truthfully that
  `legwork verify`'s captured output is capped at 64 KiB and redacted, a bounded
  receipt, not a lossless capture, and point at the sanitized host-side path for full
  evidence when required. R5-3 Stage 3b's fallback now runs inside a private,
  collision-resistant `umask 077 mktemp -d` workdir with a cleanup trap (no more fixed,
  predictable `/tmp/verify-<ws>-attempt<n>.log`), and every redactor/`artifact
  save`/digest/`note` call is success-checked with a hard stop on failure. R5-4 the
  compact note now records the exact argv (`printf '%q '`-quoted); `attempt` is
  initialized once before the retry loop and an explicit `attempt=$((attempt+1))` runs
  before each re-verification, so artifacts can't collide. R5-5 added a "Verification
  freshness gate" run before Stage 4 and re-checked before Stage 5: it recomputes the
  current diff digest and requires it match the passing verification's digest, read
  from the workspace's `latest_verification.passed`/`diff_sha256` rollup for the native
  path, or the fallback note's recorded `diff_sha256` for the host-side path; the flow
  ledger's *verified*/*landed* bullets gained the matching language. R5-6 Stage 4 now
  polls (bounded, 10 tries) for the workspace's `latest_review.job` to actually equal
  `$review_job` before trusting `parsed`/`state`/`verdict`/`diff_sha256` off it, closing
  the race where a mirrored receipt lags the reviewing job's own terminal status; the
  ledger's *reviewed* bullet gained the same requirement, and exhausting the poll is
  itself a hard stop, never a fall-through to whatever `latest_review` currently holds.
  R5-7 added an explicit "implemented ledger guard" between Stage 2's branch routing and
  Stage 3: a non-empty `legwork diff` plus an inspected (not just fetched) compact
  implementer result are now required before either verify path runs. R5-8
  `skills/legwork/SKILL.md`'s prominent Workspace-flow example gained the same
  deterministic verification hard stop between implement and review that the guide's
  Stage 3/freshness-gate defines, with a pointer to `legwork guide` for the full secure
  capture recipe instead of showing an unverified path to commit. R5-9 Stage 5's landing
  guard (and the SKILL.md mirror) now requires both a current-diff SHIP review and a
  current-diff passing verification in the same conditional, re-derived at land time
  since either can go stale while the other stage runs. Re-verified: `gofmt -l .` and
  `go vet ./...` clean, `git diff --check` clean; `go test ./... -count=1` reproduces
  only the same two pre-existing baseline failure categories (local-sandbox notifier
  under `TestDoctor*`, `TestCodexPassthroughs` teardown flake) on this doc-only change.
  The final orchestrator runs its own authoritative check before landing.
- 2026-08-03: Fixed 7 round-6 review findings on `internal/guide/guide.md` (mirrored
  into `skills/legwork/SKILL.md`'s condensed Flows section and Workspace-flow
  example). R6-1 Stage 4 no longer pipes only the plan into `ws review`; it now
  assembles one compact `review-context` artifact first (plan/acceptance contract +
  the independent deterministic verification receipt, read from `latest_verification`
  or the fallback receipt artifact depending which path ran + the adversarial-test
  receipt when one ran +, for UI-visible changes, the real-browser verifier's verdict
  and evidence pointers) and pipes that in — explicitly never the implementer's
  self-assessment or a raw log; the exact diff is still auto-seeded by `ws review`
  itself. The Independent reviewer role bullet and "Reviewer seeding" now state this
  bundle explicitly instead of "pass the plan." R6-2 Stage 3b's fallback no longer
  packs every field into a `legwork note` string — a note's event text truncates at
  200 runes, so the old note silently lost fields past that budget. It now builds a
  structured JSON receipt (passed, exit code, exact argv, duration, sanitized-log
  artifact name + digest, workspace, diff digest, round ID + attempt, completion
  time), saves it immutably as its own artifact, and the note is reduced to a bare
  pointer (`verify $ws round=... attempt=... receipt=...`); the freshness gate and
  Stage 5's reload now re-read that receipt artifact, never note text or a cached
  variable. R6-3 Stage 3b no longer resets `attempt=1` in a way that can collide
  across `FIX` rounds: a `round_id` (the fallback's own `mktemp -d` random suffix)
  is introduced once per Stage 3b entry, artifact/receipt names are qualified by
  both `round_id` and `attempt`, and a fresh round always gets a fresh `round_id`
  even when its own `attempt` counter restarts at 1. R6-4 Stage 5 no longer lands on
  Stage 4's cached shell variables; immediately before commit it now re-fetches
  `latest_review` (re-checking `job` match), re-derives verification state by
  branching on `$verified_path` (native `latest_verification` rollup, or re-reading
  the exact fallback receipt artifact selected this round), and recomputes the diff
  digest — all seven landing conditions are revalidated fresh, none trusted from
  earlier stages. R6-5 the campaign shape (guide and SKILL.md) now runs deterministic
  verification *before* independent review (was reviewing before verifying) and
  states that the reviewer is seeded with the plan/acceptance contract plus compact
  verification/adversarial/UI evidence. R6-6 SKILL.md's Workspace-flow example's
  landing check no longer unconditionally reads `latest_verification` regardless of
  which verify path ran (a fallback-only run has no native rollup and would have
  wrongly blocked landing); it now branches on `$verified_path` the same way the
  guide does, and its ordinary-`done` fallback text explicitly says it must satisfy
  the guard without pretending a native rollup exists. R6-7 audited note-preview
  language throughout both files so every remaining `legwork note` reference is
  described as pointer/narration only, never a place fields live. Introduced
  `verified_path` (`native`/`fallback`) as the flag both Stage 4's review-context
  assembly and Stage 5's landing reload key off, set immediately after each Stage
  3a/3b run. Role contracts, lanes, and the ledger's five-state names are unchanged;
  only the executable shape, the reviewer/verifier role text, and the campaign
  recipe changed. Re-verified: `gofmt -l .` and `go vet ./...` clean, `git diff
  --check` clean; `go test ./... -count=1` reproduces only the same two pre-existing
  baseline failure categories (local-sandbox notifier under `TestDoctor*`,
  `TestCodexPassthroughs` teardown flake) on this doc-only change. The final
  orchestrator runs its own authoritative check before landing.
- 2026-08-03: Fixed 5 round-7 review findings on `internal/guide/guide.md` (mirrored
  into `skills/legwork/SKILL.md`'s condensed Flows section and Workspace-flow
  example). R7-1 Stage 4's review-context assembly no longer embeds the native
  `latest_verification` rollup wholesale (it carries a bounded `output` field, even
  though already capped/redacted, that must never reach reviewer context); it now
  jq-projects only the compact identity/outcome fields (`receipt_id`, `job`/`turn`,
  `workspace`, checkpoint/diff digest, `argv`/`cwd`, `passed`/`exit_code`,
  `duration_ms`, timestamps) — the fallback receipt artifact needed no change since
  it was already this compact shape. R7-2 Stage 3a now captures
  `legwork verify --json`'s `.receipt.receipt_id` into `verify_receipt_id`, pinning
  exactly which native receipt this round selected; the verification-freshness gate
  and Stage 5's landing reload both re-read `latest_verification.receipt_id` and
  require it still equal `verify_receipt_id` (a new `identity_ok` flag, alongside the
  existing passed/digest checks — fallback's `identity_ok` is trivially true since
  that path already pins identity by re-reading the exact named artifact), closing
  the gap where a later, unrelated verification on the same job could silently
  substitute for the one that actually gated review/landing. R7-3 planned and
  mechanical lanes now share one immutable contract artifact name,
  `acceptance-contract.md`: Stage 1 saves the approved plan under it; the mechanical
  lane note now says the orchestrator saves the approved task/acceptance contract
  under the same name before Stage 2 (the ledger's *planned* stage stays explicitly
  skipped for mechanical — only the artifact name is shared); Stage 2's materialize
  step and Stage 4's review-context assembly both read `acceptance-contract.md`
  unconditionally, no lane branch. R7-4 Stage 3b's failed-attempt branch no longer
  unconditionally loops the fallback after resuming the implementer; it now re-reads
  `.state` the same three-way way as Stage 2's exit — `blocked.kind=verify` switches
  to Stage 3a, `done` bumps `attempt` and retries the fallback, anything else routes
  through "The loop" — so a fix that now qualifies for the native path is never stuck
  repeating the host-side fallback. R7-5 the fallback JSON receipt artifact is now
  described consistently as **durable but provisional and non-native** (it persists
  like any other artifact; it just isn't mirrored onto a workspace/job rollup until
  `ws check` ships) everywhere it's discussed, removing SKILL.md's contradictory
  "provisional, not a durable receipt" phrasing. Role contracts, lanes, and the
  ledger's state names are otherwise unchanged. Re-verified: `gofmt -l .` and
  `go vet ./...` clean, `git diff --check` clean; `go test ./... -count=1`
  reproduces only the same two pre-existing baseline failure categories
  (local-sandbox notifier under `TestDoctor*`, `TestCodexPassthroughs` teardown
  flake) on this doc-only change. The final orchestrator runs its own authoritative
  check before landing.
- 2026-08-03: Fixed 2 round-8 review findings on `internal/guide/guide.md`
  (mirrored into `skills/legwork/SKILL.md`). R8-1 artifacts are run-scoped/
  create-only and a campaign wave puts many tasks/workspaces under one shared
  `--run <label>` — a bare `acceptance-contract.md` would collide the moment a
  second task in the wave saved its own contract under it. Introduced an explicit,
  filesystem-safe, unique `task_id` per flow instance (default: the planning task
  file's slug), set once before Stage 1/the mechanical lane's contract save. Every
  common per-task artifact is now qualified by it: the contract is
  `acceptance-contract-${task_id}.md` (Stage 1's save, the mechanical-lane note,
  Stage 2's materialize step, the flow ledger's *planned* bullet, Stage 4's read,
  and the SKILL.md mirror), and the per-round review-context artifact is
  `review-context-${task_id}-${round_id}.md` (was `review-context-${ws}-${round_id}.md`)
  in Stage 4, "Reviewer seeding," and the SKILL.md mirror. The campaign shape's
  step 3 gained an explicit "every task gets its own `task_id`" note. R8-2 Stage
  4's review-context assembly is now fail-closed end to end instead of relying on
  one `{ ...; } > "$ctx_tmp" || exit 1` group, whose exit status is only its last
  command's — an early failed read behind a later, taken optional branch would
  previously have shipped a broken bundle while still reporting success. It now
  reads the contract with its own checked assignment; on the native path it
  reloads the workspace's verification rollup, requires `receipt_id ==
  $verify_receipt_id` and `passed=true` and a current diff digest before
  projecting the compact fields (never the earlier freshness gate's variables,
  which can go stale by the time this stage runs); on the fallback path it reads
  the exact selected receipt artifact and requires `passed=true`/current digest;
  adversarial and UI-verdict evidence are now gated by explicit
  `adversarial_required`/`ui_verdict_required` flags (set from lane/UI-visibility)
  so required evidence that fails to read is a hard stop, never a silent omission,
  while genuinely inapplicable evidence is still skipped; the artifact save and the
  `ws review` dispatch are both individually checked, so a save or dispatch failure
  stops the flow instead of the optional branches' benign exit status masking it.
  Mirrored the same fail-closed shape into SKILL.md's Workspace-flow example and
  added `task_id`/`round_id` to that example's setup so the artifact names it
  builds are well-defined. Role contracts, lanes, and the ledger's state names are
  otherwise unchanged. Re-verified: `gofmt -l .` and `go vet ./...` clean, `git
  diff --check` clean; `go test ./... -count=1` reproduces only the same two
  pre-existing baseline failure categories (local-sandbox notifier under
  `TestDoctor*`, `TestCodexPassthroughs` teardown flake) on this doc-only change.
  The final orchestrator runs its own authoritative check before landing.
- 2026-08-03: Fixed 2 round-9 review findings on `internal/guide/guide.md`
  (mirrored into `skills/legwork/SKILL.md`). R9-1 every acceptance-contract save is
  now success-checked with a hard stop on failure — the mechanical lane's save
  (before Stage 2) and the planner lane's save (end of Stage 1) both gained `||`
  error handling instead of trusting the command's exit status implicitly. Stage
  2's implementer dispatch no longer pipes an unchecked `legwork artifact get`
  straight into `run --append-prompt-file -`; it now reads the exact
  `$task_id`-qualified contract into a private, checked `umask 077 mktemp` file
  first, requires it non-empty, and only then dispatches from that file (checked),
  so a collision or a stale/missing/failed read can no longer silently continue
  into an implementer turn on empty or wrong input. Mirrored into SKILL.md's Flows
  section (Lanes paragraph) as prose describing the same checked-read-before-
  dispatch rule, since SKILL.md's own Workspace-flow example doesn't carry the
  full acceptance-contract handoff. R9-2 `adversarial_required`/
  `ui_verdict_required` are now documented as mandatory flow-intake
  classifications, not optional shell defaults: the Flow ledger's *intake* bullet,
  a new intake step right after `task_id` is picked in "Executable flow shapes"
  (guide) and the Workspace-flow example's setup block (SKILL.md), and the
  Orchestrator role bullet in both files all now say both flags are set explicitly
  at intake, from the lane and UI-visibility. Stage 4's review-context assembly
  (guide) and its SKILL.md mirror no longer read them via `${var:-false}`; both now
  `case`-validate the value is exactly `true` or `false` before proceeding, with a
  hard stop on anything else (unset, empty, or malformed), and only an explicit
  `false` skips the corresponding evidence section — an explicit `true` still
  requires a successful compact-receipt read, as before. Role contracts, lanes, and
  the ledger's state names are otherwise unchanged; scope stayed to the two named
  gaps. Re-verified: `gofmt -l .` and `go vet ./...` clean, `git diff --check`
  clean; `go test ./... -count=1` reproduces only the same two pre-existing
  baseline failure categories (local-sandbox notifier under `TestDoctor*`,
  `TestCodexPassthroughs` teardown flake) on this doc-only change. The final
  orchestrator runs its own authoritative check before landing.
- 2026-08-03: Corrected routing per
  `planning/done/flow-routing-correction.md` — the accepted operating model
  rejects the delegated mechanical-implementation lane entirely. Removed the
  Luna-as-implementer policy, the mechanical lane, and the cross-family reviewer
  workaround it forced ("a Luna implementation needs a non-OpenAI reviewer") from
  every surface. Replaced "proportional lanes" with **risk-proportional routing**:
  (1) direct/orchestrator path for simple questions and truly trivial changes,
  with an explicit promotion boundary (ambiguity, multiple interacting files,
  public-contract/security/data/concurrency implications, migration risk,
  non-obvious acceptance criteria, or UI-visibility); (2) the full delivery flow —
  planner → implementer → deterministic verification → fresh independent review —
  as the default for all non-trivial implementation; (3) a high-risk extension
  adding only the already-documented adversarial/second-review/human-decision
  gates. Independent review is now stated as mandatory for every non-trivial
  change rather than "mandatory in every lane." Promoted the planner's duty to aim
  for contracts precise enough that implementation is nearly mechanical execution,
  without removing implementer judgment. Promoted the **command/evidence
  distiller** from "not a standing role either" to a first-class shared role with
  explicit inputs/duties/forbidden behavior and an invocation policy (the
  orchestrator dispatches it preemptively around chunky commands; the implementer
  requests it through the orchestrator turn boundary when its own native
  subagents can't provide the boundary). Reconciled `internal/guide/guide.md`
  (canonical), `skills/legwork/SKILL.md`, `README.md`, `DESIGN.md`, this file's
  Accepted-architecture sections, `planning/tasks/orchestrator-profiles.md`, and
  `planning/tasks/workspace-check-receipts.md`. The mechanical-lane branches in
  the guide's "Executable flow shapes" skeleton were deleted outright (the
  skeleton is now stated as the full delivery flow; direct-path work never enters
  it). Historical R1–R9 Log entries above and all other `done/` archives were left
  untouched as frozen history. Re-verified: `gofmt -l .` and `go vet ./...` clean,
  `git diff --check` clean; `go test ./... -count=1` reproduces only the same two
  pre-existing baseline failure categories (local-sandbox notifier under
  `TestDoctor*`, `TestCodexPassthroughs` teardown flake) on this doc-only change.
