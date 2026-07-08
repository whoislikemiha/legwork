# Field notes: watching another orchestrator drive legwork cold

Written by the orchestrating agent (Claude, Fable 5) on 2026-07-08 after analyzing
the reasoning transcript of a *different* orchestrator (Claude Opus 4.8, high
effort) dispatching wave 1 of the `next-roadmap` run — the seven "Next" tasks,
seven codex gpt-5.5 implementers in ws-53…ws-59 — and then taking the run over
mid-flight. This is a rare artifact: not how *I* experienced the tool, but how a
capable orchestrator navigates it with only the skill/guide as preparation. The
gaps below are weighted accordingly — everything Opus had to *derive*, a
smaller orchestrator model will guess at or get wrong.

legwork version: no `version` verb exists yet (see `version-stamp` task);
installed binary provenance per doctor = commit 52f4374, repo HEAD at dispatch
= eaffaef. Docs-only skew, tolerable, but establishing that took a detour —
see below.

## The headline

Opus made no wrong decisions, but roughly 70% of the transcript's thinking was
spent re-deriving strategy the docs don't carry. The skill and guide teach the
**verbs** and the **single-job loop** well; they do not teach the **campaign
shape** — N tasks → preflight → conflict plan → parallel implement → review →
serial land → cleanup. Every phase boundary was improvised from first
principles, correctly, at Opus-high prices. That's the definition of a doc gap:
the knowledge exists (prior field notes, run history, source code), it just
isn't delivered where the next orchestrator starts.

## What the docs steered well

Preflight `doctor` with live probes before any dispatch; `--run` labeling from
the start; one workspace per task with parallelism-as-workspaces; planning to
merge from the main checkout (the guide's explicit warning landed); model
policy (big-model reviewers, cheap implementers); conscious awareness of the
no-paraphrase rule; self-correcting out of a deep-dive ("I should delegate the
detailed reading to the implementers"). The verb layer is genuinely learnable
from the docs alone.

## Where thinking burned on gaps already filed

Strong validation for the existing board — five of the heaviest deliberations
map to open tasks:

- **"Is my binary current?"** → tried `legwork version`, doesn't exist, fell
  back to trust. `version-stamp` (now promoted; CLAUDE.md literally instructs
  orchestrators to record `legwork version` — a dangling reference until it
  lands).
- **Sandbox verification anxiety** → hand-wrote a long anti-workaround +
  blocked-reporting paragraph into the append-prompt; that's
  `sandbox-anti-workaround` + the GOCACHE/TMPDIR items, being implemented by
  the very workers he was dispatching.
- **Reviewer seeding** → recalled "seed the reviewer with the diff, that's
  what the prior dogfood did" — folklore, not docs. `ws-review-verb`.
- **Result routing** → planned a `status --json | python -c` harvest.
  `result-verb`.
- **No notifier → DIY polling** → noted and absorbed. `per-job-wait` /
  notifier out-of-box story.

## New gaps (filed today)

1. **The injected contract is not inspectable.** To obey "never paraphrase the
   contract," Opus read `internal/rules/rules.go` — Go source — because that is
   the only place the contract text exists. A smaller model won't do that; it
   will guess, and guessing produces paraphrase. Filed: `rules-verb`
   (`legwork rules` prints the contract verbatim).
2. **Append-prompt has no norms or worked example.** The skill says
   "task-specific guidance" and stops. Opus's AP grew to ~40 lines and ended up
   restating contract territory ("Do not commit or push", a full
   blocked-reporting protocol) *minutes after* twice acknowledging the
   no-paraphrase rule. If Opus-high slips on this, the rule as written is
   unenforceable — the fix is delivery (show a good AP, state what belongs in
   AP vs task file vs prompt), folded into `orchestrator-recipes`.
3. **No multi-task campaign recipe.** The single biggest thinking sink (~40%)
   was the conflict-graph across 7 tasks and deriving "implement in parallel,
   land serially, order by isolation." Correct, reusable, undocumented.
   `orchestrator-recipes` promoted from Later/P2 to Next/P1 and expanded.
4. **Model-name discovery is unguided.** Three commands of grepping
   `codex --help` / `~/.codex/config.toml` / `claude --help` to establish
   `gpt-5.5` and `opus`. Doctor *validates* a model but nothing helps you
   *pick*. One doc line fixes it ("omit `--model` for the agent's default; the
   probe confirms it") — folded into `orchestrator-recipes`.
5. **Multi-line append-prompts ride on shell quoting.** Opus assembled the AP
   as a shell variable; it survived (job meta shows real newlines — an earlier
   read of the transcript wrongly flagged this as corrupted, corrected here),
   but the failure mode is silent and smaller models will hit it. Filed:
   `append-prompt-file`.

Small ones: `ws new` concurrency is unspecified (Opus serialized creation "to
avoid index-lock contention" — pure speculation; one sentence in the guide
settles it); the hand-built ws↔task mapping table duplicated what `runs`/`ls`
show and belonged in `artifact save`.

## Wave-1 outcome as product evidence

All seven implementers finished in a single turn each and **all seven ended
`blocked`** — every one the *blocked-cannot-verify* case: codex's sandbox
mounts `~/.cache/go-build` read-only, so `go test` can't even compile. Two
workers (ws-55, ws-57) independently discovered `GOCACHE=/tmp/...` and got the
full suite green (legitimate — env var, not harness edit); the other five
stopped and reported the exact failing command, as instructed. Nobody bent the
product to fit the sandbox — the anti-workaround rule works as prompt text and
belongs in the contract (which ws-55 just implemented).

Reading seven `blocked` states meant parsing seven prose `result` blobs to
confirm they were all really "done, unverified" — the precise pain
`structured-blocked-provision` (ws-57, in this same wave) addresses. And codex
`ctx` telemetry showed 1.8M–7.9M for single-turn jobs — the crying-wolf signal
from `codex-health-signal`, still lying.

## The smaller-model thesis

The pattern is: **docs cover mechanics, the model supplies strategy.** The
strategic joints Opus improvised — binary currency, model strings, task
interaction/conflict planning, AP composition, landing order, reviewer seeding
— each had no documented answer. Opus can afford an internal design review at
every joint. A smaller orchestrator will freeze, guess wrong, or hand-roll a
competing worker contract (the failure mode the 2026-07-07 notes already
identified as the one that costs quality). The cheapest capability uplift
legwork can buy is not a verb — it's moving the campaign strategy from
expensive model reasoning into the guide/skill, once, correctly.

## Worker friction harvest (wave 1)

Five of seven task files carried `## Friction`; deduped:

- **Writable Go cache, unanimously.** Every note is a variation of "default
  `GOCACHE` is read-only, verification dies before compiling" — the
  writable-tmpdir task confirmed from the worker seat, five independent times.
- **Real-agent smoke checks can't run from inside a worker sandbox** (ws-55,
  ws-57): nested codex fails writing PATH aliases before producing a result,
  and detached-runner waits outlive short wrapper commands. Consequence for
  docs: the AGENTS.md real-agent verification is *orchestrator-side by
  construction* — say so explicitly in the recipes, so workers stop attempting
  it and orchestrators stop asking them to.
- **Codex environment-setup failure is invisible in the state model** (ws-57):
  the PATH-alias failure surfaced as a bare `interrupted`, indistinguishable
  from an agent crash without reading runner output. Filed mentally against the
  codex-observability remainder; promote if it recurs.

## Landing retrospective (added after wave 1 landed)

All seven landed on main the same day. Shape of the run: 2/7 SHIP on first
review (matching the corpus's ~3/8 rate), 5 FIX rounds — every one resolved in
a single resume, every re-review SHIP. The reviewers again earned their tokens:
the gc orphan-tree race (a live `ws new` could be swept) and `--merge-into`
leaving the operator's checkout switched on failure paths were both
first-review catches on never-executed code paths.

Orchestrator-side landing notes:

- **Serial landing worked as planned**; conflicts appeared exactly where the
  conflict graph predicted (docs trio, `main.go` verb registration, the ws-58 ×
  ws-59 close path) and were all resolvable as unions. One conflict was
  *semantic* and invisible to git: ws-59's test asserted branch deletion on
  merged close, ws-58's landed policy is branch-durable — caught by running the
  full suite on main after every merge, which is the argument for that
  discipline.
- **The `close --merged` tripwire fired correctly** on the first landing
  (local main unpushed → not an ancestor of origin/HEAD → refused with the
  exact `--into` fix). The right kind of paranoid, confirmed in anger.
- **The AGENTS.md codex smoke was lying to us.** "Reply with exactly the word
  PLUMBING-OK" contradicts the injected status-block contract; codex often
  obeys the task literally, omits the block, and the parser correctly reports
  `blocked`. Measured: old rules 1/2 blocked, new rules 4/4 blocked on that
  prompt — but 2/2 `done` on tiny real tasks. Not a rules regression; a bad
  smoke prompt. AGENTS.md snippet fixed to use a real micro-task.
- **Pre-existing flake**: `TestCodexPassthroughs` can fail teardown
  (`TempDir RemoveAll: directory not empty`) when the detached runner writes
  during cleanup. 5/5 green on retry; worth a small fix someday.

## Actions taken

Promoted `orchestrator-recipes` (Later/P2 → Next/P1, scope expanded) and
`version-stamp` (Later → Next); filed `rules-verb` (Next/P1) and
`append-prompt-file` (Next/P2); recorded the seven wave-1 tasks as In flight
with their ws/job mapping; took over review + verification + serial landing of
wave 1.

## Wave 2 close-out (same day)

All four gap-fix tasks landed: `legwork version`, `legwork rules`,
`--append-prompt-file` (codex gpt-5.5 implementers, `ws review` opus reviewers,
all SHIP first pass), then `orchestrator-recipes` (opus implementer at high
effort — prose task, big model; codex cross-review: SHIP, zero findings). The
guide now carries a `## Recipes` section with the campaign shape, append-prompt
norms anchored on `legwork rules`, and the preflight facts; the skill and README
are reconciled. Every gap this file identifies is now either landed or a filed
board item.

Wave 2 also dogfooded everything wave 1 built, and it held: `ws review`
replaced the hand-assembled reviewer prompts (one command vs a 15-line script);
`close --merge-into` landed three workspaces in one step each and its
conflict path aborted cleanly and restored HEAD when ws-61 collided;
`result` fed verdict extraction; ws-61's worker reported the structured
`blocked: {kind: verify}` and routing needed zero prose parsing; a worker
friction note flagged that my append-prompt's `GOCACHE=/tmp` advice had been
obsoleted mid-run by wave 1's per-job caches — the tool now outpaces
orchestrator habits within a single session, which is exactly the drift
`legwork rules` + the recipes section exist to absorb.
