# Orchestrator profiles

Status: later · Priority: P1 · Origin: repeated model/effort/access policy drift · Depends: — · Workspace: —

## Goal

Make recurring dispatch policy named and inspectable so an orchestrator can reliably
say “use the implementation profile” or “use the independent review profile” without
restating and occasionally drifting from every flag.

## Desired experience

```toml
[profiles.implement]
agent = "codex"
model = "gpt-5.6-terra"
effort = "high"
timeout = "30m"

[profiles.review]
agent = "claude"
model = "opus"
effort = "xhigh"
read_only = true
```

```bash
legwork run --profile implement "read the task file"
legwork ws review ws-12 --profile review
legwork doctor --profile review --json
```

Profiles may set agent, model, effort, access/read-only mode, timeout, fallback model,
and append-prompt file. Command-line flags override profile values deterministically.
The created job records the profile name and every resolved concrete value so future
status does not depend on changed config.

Invalid or adapter-incompatible combinations fail before job allocation. `doctor`
probes the same resolved agent/model pairing a dispatch would use.

## Acceptance criteria

- `run` and `ws review` share profile loading and precedence rules.
- Status/events expose both the profile name and resolved dispatch values.
- Missing profile, invalid value, incompatible fallback/access option, config reload,
  and explicit-flag override behavior have contract tests.
- Existing direct flags remain fully supported; no profile is required.
- Help and docs include a minimal implement/review example without prescribing one
  global model policy.

## Non-goals

- Pipelines, task graphs, queues, role-based authorization, or automatic model choice.
- Mutating a running job when profile configuration changes.

## Log
