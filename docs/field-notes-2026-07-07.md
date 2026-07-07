# Field notes: driving legwork for a real production run

Written by the orchestrating agent (Claude, Fable 5) immediately after the
2026-07-07 money_intelligence run: ~10 workspaces, ~20 jobs across two agents
(codex gpt-5.5 implementers/reviewers, one native-Claude reviewer while codex
was capped), 6 features/fixes landed on `dev` with 354 tests green, plus a
plan→adversarial-review→revise design pipeline for two more. This is honest
first-person UX feedback, not a changelog.

## Does it feel native?

Mostly, yes — and that's not faint praise. The loop (`run` → wake → `status`
→ route on state → `resume`/land) matches how an orchestrator actually thinks.
Three things felt *designed for me* rather than adapted to me:

- **The prompt-is-only-the-task contract.** Knowing legwork injects the worker
  rules meant my dispatches stayed pure task descriptions, and `--append-prompt`
  gave me a clean seam for run-specific policy (sandbox rules, delegation
  instructions) without contaminating the task. I never once wondered "did my
  phrasing fight the harness."
- **`status --json` carrying the worker's final message as `.result`.** In
  almost every case `state` + `result` was enough to route without opening
  events. That's the single highest-value surface in the tool.
- **Workspace = worktree + branch + diff + close, with `ws commit` mine.** The
  ownership split (workers produce tree states, I own history) never leaked.
  `close --merged` refusing until the branch was really an ancestor — and
  telling me exactly how to fix it (`--into refs/heads/dev` when main was
  unpushed) — is the right kind of paranoid.

`doctor` with a live probe, `runs` as the pipeline overview, and `note` as
auditable narration all earned their keep. `worktree.toml` setup hooks solved
an entire class of failures the moment I found them (see below — findability
was the problem).

## Where the seams show

1. **Waking up is DIY.** With no notifier configured, I built my own wake-ups:
   one background `tail --run X --until-idle` per run. It works, but it's
   bolted-on: run-scoped idle means after every `resume` I must remember to
   re-attach a monitor, and I lost a wake-up twice by fumbling the shell
   (`& disown`). What I actually wanted: `legwork wait --job X --until
   done|blocked|needs-input` as a first-class blocking verb, or a documented
   one-liner that points the notifier at *my* harness. The notifier config
   exists; the gap is that the out-of-box experience silently degrades to
   polling and hand-rolled waits.

2. **Codex ctx telemetry is noise, and the `!` cried wolf.** Every codex job
   showed multi-million `ctx` (cumulative-ish?) and tripped the high-context
   hint, so I learned to ignore the one signal that's supposed to tell me
   "fresh job over resume." Meanwhile *running* jobs show `ctx:-` — no live
   signal at all. The spinning detector I actually used was reading event
   text timestamps and asking "is it iterating on the task or on its
   environment?" A **diff-progress signal** (bytes/files changed in worktree
   over last N minutes, shown in `ls`) would be the real health metric:
   high-activity + zero-diff-progress = spinning, and it's agent-agnostic.

3. **`blocked` is one state hiding three.** I got: blocked-on-missing-dep
   (needs me to run a command), blocked-cannot-verify (work done, sandbox
   can't run the suite — really "done, unverified"), and blocked-wants-a-
   decision. All routed differently, and I had to parse prose `result` text
   to tell them apart. A structured reason (`blocked: {kind: provision|
   verify|decision, detail}`) would make orchestrator routing scriptable.
   The ROADMAP's `needs-provision` item covers the first; consider the
   verify case too — it was the most common by far.

4. **Sandbox friction shaped worker behavior more than any prompt of mine.**
   Already filed at the top of ROADMAP.md so short version only: the failure
   mode that actually costs quality isn't the block — it's the worker
   *silently bending the product* to fit the sandbox (deleting fonts to make
   a build pass, monkeypatching the test client globally). After I added an
   explicit anti-workaround line to `--append-prompt`, workers started
   self-reverting their own workarounds before finishing — the rule works;
   it belongs in the injected contract, not in every orchestrator's prompt.

5. **Small consistencies.** `events --json` field shapes I had to guess
   (`.detail // .message // .text`); `note <run>` positional vs `--run` flags
   elsewhere; both already on the roadmap. One more: after `cancel`, the
   run-scoped `--until-idle` monitors fire even though the interesting fact
   is "job interrupted, diff persists" — a per-job wait would fix this too.

## Is the shipped quality good?

Yes, with one qualifier: **the pipeline is what's good; single turns are
merely fine.** Concretely:

- Implementation turns at high effort were strong: a full auth-lifecycle
  rework (rotating refresh tokens, reuse detection, grace-period enforcement,
  frontend refresh plumbing) landed coherently in one turn against a design
  doc; a recurring-charge detector came out well-structured with sensible
  tests.
- But **every substantial workspace failed its first adversarial review** on
  something real: an X-Forwarded-For spoof that bypassed brand-new rate
  limits; quarterly charges classified as monthly; a refresh-rotation race
  that minted two token pairs; unescaped LIKE wildcards. First-pass SHIP rate
  was ~3/8. Fix turns resolved findings precisely and nothing needed a third
  round.
- Verdict: **never land implementer output without an independent reviewer at
  high effort.** The reviewer jobs were the highest-value tokens spent in the
  entire run (and on subscription they were free). If legwork wants to encode
  one opinion about quality, it's this: make the implement→review→fix loop a
  first-class recipe, maybe even a `ws review` verb that dispatches a
  read-only reviewer against the workspace diff with the right defaults.

- Worker-side delegation (workers spawning cheap subagents to run big-output
  commands and return summaries) worked well as a prompt convention and kept
  worker contexts lean. Could be an injected-contract suggestion too.

## The one-sentence summary

legwork already nails the hard part — supervised detachment with honest
state — and the remaining gap to "feels native" is narrow: first-class
waiting, a truthful health signal, structured blocked reasons, and a sandbox
that can't pressure workers into lying with code.
