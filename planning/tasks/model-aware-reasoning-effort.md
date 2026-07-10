# Model-aware reasoning effort

Status: in flight · Priority: P1 · Origin: Codex CLI 0.144.1 / `gpt-5.6-sol` direct `xhigh` probe + explicit GPT-5.6 Sol/xhigh orchestration policy · Depends: — · Workspace: —

## Goal

Honor an orchestrator's reasoning-effort policy according to the selected agent and model, and make the exact value sent to the provider auditable. An explicit GPT-5.6 Sol/`xhigh` dispatch must no longer be silently reduced to `high`.

Keep this first change narrow: replace the stale global Codex ceiling with a small adapter-owned resolution policy, persist requested and resolved effort separately, and use `doctor` to validate the exact live combination. Leave a clean path for more provider/model capability entries without inventing a universal capability service.

## Problem and evidence

- The public vocabulary is `low|medium|high|xhigh|max`, but Codex command construction currently maps both `xhigh` and `max` to `model_reasoning_effort="high"` for every model.
- Job metadata stores only `effort`, which is the value the orchestrator typed. For a Codex `--effort xhigh|max` job, `run --json` and `status --json` therefore report one value while the adapter dispatches another. The guide, skill, and README currently tell orchestrators to treat `model`/`effort` in status as a dispatch receipt; that receipt is not truthful for clamped Codex jobs.
- On this machine, Codex CLI `0.144.1` and model `gpt-5.6-sol` were directly probed with `-c model_reasoning_effort="xhigh"`; the turn was accepted successfully. This disproves the blanket claim that Codex tops out at `high`.
- The evidence is deliberately narrow. It does **not** prove that every Codex model, Codex version, configured provider, or model alias accepts `xhigh`, and it does not prove that Codex accepts the literal value `max`.
- The current clamp prevents the orchestrator from honoring an explicit GPT-5.6 Sol/`xhigh` policy for high-value planning and task-writing turns. Worse, the downgrade is only discoverable by reading adapter code.

## Product contract

### Requested effort versus resolved effort

Use these terms consistently in code, metadata, JSON, help, and docs:

- **Requested effort** is the user's intent exactly as supplied by `--effort` (or, later, by a profile): `low`, `medium`, `high`, `xhigh`, or `max`.
- **Resolved effort** is the concrete value Legwork sends to the selected agent for every turn in the job. It must never be a claim that the provider accepted the value; a successful `doctor` probe or completed turn supplies that evidence.
- `max` is adaptive intent: use the highest level Legwork conservatively knows for the selected adapter/model. `xhigh` is exact intent: send `xhigh`, do not silently reduce it. If that model/provider rejects it, preflight or the job must fail clearly.
- An omitted `--effort` remains omitted. Legwork must not inject an effort override or manufacture requested/resolved values when the user chose the agent default.

The initial resolution matrix is intentionally small:

| Adapter/model selection | Requested | Resolved | Policy |
|---|---:|---:|---|
| Claude, any model | `low|medium|high|xhigh|max` | same value | Preserve the existing Claude passthrough; the Claude CLI/provider validates it. |
| Codex, any model | `low|medium|high` | same value | Existing native values remain exact passthroughs. |
| Codex, any model | `xhigh` | `xhigh` | Exact request; provider validation is authoritative. Never downgrade it. |
| Codex, explicit exact model `gpt-5.6-sol` | `max` | `xhigh` | Highest directly evidenced level for this model. |
| Codex, omitted or any other model | `max` | `high` | Conservative compatibility fallback; do not infer support from a family prefix or future-looking model name. |

For this task, the adapter (`claude` or `codex`) is the provider namespace Legwork actually knows. Do not parse private Codex configuration to guess a custom backend. If Legwork later exposes an explicit provider selection, the resolver key can grow from adapter/model to adapter/provider/model without changing the requested/resolved JSON contract.

### CLI behavior

- Keep the accepted `--effort low|medium|high|xhigh|max` vocabulary for `run` and `ws review`; invalid values still fail before a job ID or state is allocated.
- Apply one shared dispatch resolution path to `run` and `ws review`. Review dispatch must not retain a separate clamp or help string.
- `run --agent codex --model gpt-5.6-sol --effort xhigh` sends `model_reasoning_effort="xhigh"`.
- `--effort max` follows the matrix above. It may resolve lower than the requested symbolic maximum, but the difference must be visible in JSON and human status.
- Do not add a synchronous paid probe to `run`/`ws review`; detached dispatch must stay fast. An explicit `xhigh` against an unsupported model is allowed to fail loudly at the provider unless the orchestrator preflights it first.
- Human `status` should show the selected model when present and an effort line. Show one value when requested and resolved match, and an explicit transition such as `effort: max -> xhigh (resolved)` when they differ. Keep normal `run` stdout as the job ID only so existing shell/ssh command substitution remains safe.
- Provider rejection of an exact effort is a normal failed preflight/turn, not permission to retry silently at a lower effort. Transient retry/fallback policy belongs to the separate provider-recovery work.

### JSON and persisted metadata

New jobs with an effort must persist and expose:

```json
{
  "effort": "max",
  "requested_effort": "max",
  "resolved_effort": "xhigh"
}
```

- `requested_effort` is the canonical new field for user intent.
- `resolved_effort` is the concrete dispatch value and is the only effort value the runner/adapter may use.
- Preserve the existing `effort` field as a backwards-compatible alias of requested effort for current scripts. Do not repurpose it to mean resolved effort. Removal or reinterpretation is outside this task.
- The fields must be present consistently on the existing metadata-returning JSON surfaces: successful `run --json`, `ws review --json`, `resume|answer|approve --json`, and `status --json`. Omit all three when no effort was requested, following the existing `omitempty` convention.
- The persisted job record is the receipt and source of truth for resumes. No new event type or event-schema change is required for this task. If implementation adds effort fields to an existing event, they must be additive and respect the public event-version rules.
- Do not add a vague boolean such as `xhigh_supported`. Requested and resolved values are sufficient for dispatch truth; live acceptance belongs in doctor/turn results.

## Capability strategy

Implement a narrow, adapter-owned resolver that takes the dispatch identity Legwork actually has (agent plus explicit model, if any) and returns requested/resolved effort. It may be a small table or equivalent policy; it is not a new public universal capability API.

- Seed only the directly evidenced Codex entry: exact `gpt-5.6-sol` supports `xhigh` for adaptive `max` resolution.
- Match known model IDs exactly after only harmless whitespace normalization. Do not use prefix/family matching, assume aliases, or infer that later `gpt-5.6-*` names inherit the capability.
- Unknown/omitted Codex models keep the conservative `max -> high` behavior. Explicit `xhigh` is passed through and validated by Codex rather than downgraded.
- New table entries require evidence recorded in tests/task logs: provider/adapter, exact model ID, CLI version, requested config, and observed acceptance. A CLI-version dimension can be added when evidence shows it is needed; do not block this task on a general discovery framework.
- Prefer a machine-readable upstream capability command in the future if Codex provides a stable one. Codex CLI `0.144.1` help exposes generic `-c` configuration but no model/effort capability listing, so parsing help text or maintaining a network-fed registry is not part of this implementation.
- Do not cache live probes, add background refresh, or introduce new state outside the existing filesystem job records.

## Doctor and preflight

Add `--effort` to `legwork doctor` and resolve it through the exact same policy as dispatch.

- The live probe must receive both the selected `--model` and the **resolved** effort. A green probe then means auth, model selection, and the concrete effort were accepted together.
- `doctor --agent codex --model gpt-5.6-sol --effort xhigh --json` must show requested `xhigh`, resolved `xhigh`, and probe success on a healthy machine.
- `doctor --agent codex --model gpt-5.6-sol --effort max` must show requested `max`, resolved `xhigh`, and probe that concrete value.
- When `--model` is omitted, keep the existing meaning: Codex's configured default is used. Do not claim a model ID Legwork did not resolve. An explicit `xhigh` probe still validates that configured default at runtime; adaptive `max` resolves conservatively to `high` because the model identity is unknown.
- Extend doctor JSON with a small machine-readable dispatch receipt, for example:

  ```json
  {
    "ok": true,
    "dispatch": {
      "agent": "codex",
      "model": "gpt-5.6-sol",
      "requested_effort": "max",
      "resolved_effort": "xhigh"
    },
    "checks": []
  }
  ```

  Omit `model` when the agent default is used. Human output must name the requested/resolved effort in the probe detail rather than only saying “model accepted.”
- `--no-probe` performs only static resolution and keeps the existing `probe: skip` behavior. It may report the dispatch receipt, but must not imply provider acceptance.
- If the live provider rejects the resolved effort, doctor returns its existing preflight failure exit (`1`) with an actionable probe detail naming agent, model/default, requested effort, and resolved effort. Invalid effort syntax remains a usage error (`2`) before checks run.
- Doctor remains opt-in preflight. `run` must not automatically invoke a paid turn or depend on a prior doctor cache.

## Resume and legacy consistency

- Resolve once, before the initial job is spawned, and persist the result. Every `resume`, `answer`, and provision-approved continuation uses the persisted `resolved_effort`; never recompute it from a newer Legwork binary, changed capability table, changed Codex config, or current model defaults.
- The initial model selection and resolved effort form one dispatch policy for the session. This task does not add per-resume model/effort overrides.
- Existing job records have only `effort`. They must remain resumable without silently changing the effective effort mid-session:
  - legacy Claude jobs resolve the old stored value to itself;
  - legacy Codex `low|medium|high` jobs resolve to the same value;
  - legacy Codex `xhigh|max` jobs resolve to `high`, matching what the old adapter actually sent, even when the stored model is `gpt-5.6-sol`.
- Status may synthesize requested/resolved fields for a legacy record without mutating it. On the next mutating continuation, persist the compatibility resolution before spawning so subsequent turns are stable.
- A new `gpt-5.6-sol` job is eligible for the new resolution; an old session is not silently upgraded from `high` to `xhigh`.

## Backwards compatibility and docs truthfulness

- Existing commands, effort spellings, default effort omission, job-ID stdout, and exit-code classes remain intact.
- `effort` stays in JSON as the requested-value compatibility alias. New fields are additive; old consumers continue to work, while new consumers must use `requested_effort`/`resolved_effort` when policy fidelity matters.
- The intentional semantic change is limited to **new** Codex jobs: explicit `xhigh` is no longer downgraded, and `max` resolves to `xhigh` only for the directly known model. Unsupported explicit `xhigh` may now fail instead of silently running at `high`; document this as fail-loud policy, not a regression.
- Update both `run --help` and `ws review --help`: remove the blanket “codex clamps xhigh/max to high” claim; explain that `xhigh` is exact/model-dependent and `max` resolves to the highest conservatively known level. Add `doctor --effort` help.
- Docs travel in threes: update the canonical `internal/guide/guide.md`, `skills/legwork/SKILL.md`, and `README.md` together. Replace every global Codex-ceiling statement, explain the status receipt fields, show the GPT-5.6 Sol/`xhigh` preflight, and tell orchestrators to probe exact model/effort pairs when policy matters.
- Do not say that all Codex models support `xhigh`, that `resolved_effort` proves provider acceptance, or that an omitted model was resolved to a particular default.

## Acceptance criteria

- A fresh Codex command for `--model gpt-5.6-sol --effort xhigh` contains `model_reasoning_effort="xhigh"`, never `high`.
- A fresh Codex command for explicit `xhigh` on an unknown model still contains `xhigh`; no silent downgrade path remains.
- Codex `max` resolves to `xhigh` for exact `gpt-5.6-sol` and to `high` for an omitted or unknown model. Claude values remain unchanged. No requested effort produces no override.
- `run` and `ws review` use identical resolution and persist `effort`, `requested_effort`, and `resolved_effort` before the detached runner starts.
- `status --json` and every existing metadata-returning dispatch/continuation JSON surface expose the same requested/resolved values. Human status makes a differing resolution obvious without changing job-ID-only run output.
- The runner passes only persisted `resolved_effort` to the adapter on initial and resumed turns.
- A legacy Codex job stored with `effort: xhigh` or `effort: max` resumes at `high`; a new GPT-5.6 Sol job does not inherit that legacy clamp.
- `doctor --effort` probes the same concrete value dispatch will use, reports the requested/resolved pair in human and JSON output, and fails with exit `1` when the provider rejects it. `--no-probe` makes no acceptance claim.
- Invalid effort still fails before allocation; no paid discovery is added to normal dispatch.
- CLI help, guide, skill, and README contain no blanket Codex `high`-ceiling statement and accurately describe exact `xhigh`, adaptive `max`, model uncertainty, and the receipt fields.
- `gofmt -l . && go vet ./... && go test ./... -count=1` is clean, followed by the real Codex probe below because the adapter/doctor path changes.

## Test and real-agent probe strategy

Automated coverage:

- Table-test the resolver across Claude/Codex, empty effort, all five accepted values, exact `gpt-5.6-sol`, omitted model, unknown model, and invalid input. Include an exact-match regression so a lookalike/prefix model does not gain `max -> xhigh` accidentally.
- Adapter command tests assert the final config override, not merely the requested value. Cover fresh and resume command construction and prove no second adapter clamp remains.
- E2E tests cover `run --json`, `ws review --json`, human/JSON status, resume stability, and legacy metadata with only `effort` present. Make a persisted-resolution test insensitive to later table changes by seeding metadata directly.
- Doctor tests assert the effort reaches the probe command, success/failure exit codes, dispatch JSON, human detail, omitted-model behavior, `max` resolution, `--no-probe`, and invalid effort before any probe is spawned. Use the fake/stub adapter to simulate an unsupported-model rejection rather than spending real calls guessing which model rejects `xhigh`.
- Keep existing low/medium/high, fallback-model rejection, detachment, and workspace-review defaults covered.

Real-agent verification (in a subshell with a temporary state dir; record `legwork version --json` and `codex --version` in the task Log when implemented):

```bash
(
  export LEGWORK_STATE_DIR=$(mktemp -d)
  go build -o /tmp/lw .
  /tmp/lw doctor --agent codex --model gpt-5.6-sol --effort xhigh --json
  /tmp/lw run --agent codex --model gpt-5.6-sol --effort xhigh \
    "Create a file named smoke.txt containing the single word ok."
  sleep 25
  /tmp/lw status job-1 --json
)
```

Expect doctor success; a completed task-shaped turn with `context > 0`; and status showing requested/resolved `xhigh`. Do not use an “exact reply” smoke prompt that conflicts with the injected status-block contract. If no known supported Codex model is available at implementation time, report the real probe as blocked and rely on the deterministic adapter/doctor stub tests—do not weaken the mapping or invent evidence.

## Invariants and non-goals

- Preserve the headless CLI-over-ssh contract, detached runner, filesystem state, and fast `run` return.
- Preserve **no daemon, no database, and no MCP integration**. There is no capability service, background refresher, or network registry in this task.
- Preserve the public event schema and status-block contract; missing/unparseable status still resolves toward `blocked`, never `done`.
- Do not add model enumeration, auto-updates, remote provider discovery, per-resume effort changes, fallback-on-effort-rejection, profiles, queues, or a pipeline policy engine.
- Do not silently try `xhigh` and then retry at `high`. A lower-effort retry changes the orchestrator's explicit policy and requires a separate explicit design.
- Do not broaden the effort vocabulary beyond the existing five values in this task.
- Keep provider/model knowledge local to the adapter/dispatch policy. A future generalized capability surface must be justified by multiple real consumers, not pre-built for hypothetical agents.

## Dependencies and open decisions

- **Hard dependencies:** none. The existing shared dispatch path, persisted job metadata, Codex adapter, and doctor probe are sufficient.
- **Orchestrator profiles:** later profile resolution must feed its requested effort into this same resolver and expose both fields; this task should not wait for profiles or implement them.
- **Quality receipts/meta versioning:** additive effort fields do not require that larger task. Coordinate if a `meta_version` lands concurrently, but do not couple delivery.
- **Transient provider recovery:** an effort-capability rejection remains a normal probe/job failure here. Typed retry/fallback behavior belongs in that task and must not silently lower effort.
- **Capability evidence:** the initial exact model entry is `gpt-5.6-sol` only. Before adding aliases, other models, provider IDs, or CLI-version gates, capture direct evidence. If implementation discovers a stable machine-readable Codex capability command, record it for follow-up; adopting a universal interface is not required to ship this fix.

## Log

- 2026-07-10: Task drafted from the confirmed mismatch between Codex CLI `0.144.1` / `gpt-5.6-sol` accepting `model_reasoning_effort="xhigh"` and Legwork's unconditional `xhigh|max -> high` adapter clamp. No implementation workspace assigned.
