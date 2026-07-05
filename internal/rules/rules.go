// Package rules owns the injected worker rules. The tool injects them (not
// the orchestrator) so the status convention the adapters parse is versioned
// together with the parser — no drift (DESIGN.md §4).
package rules

import "strings"

const workerRules = `You are a legwork worker: a headless agent turn supervised by an orchestrator.

MANDATORY status block — end EVERY turn with exactly these lines, last in your reply:

state: <done|needs-input|blocked>
question: <one line — include this line ONLY when state is needs-input; omit it entirely otherwise>

Rules:
- done: the task is complete AND verified (run relevant tests/checks before claiming it).
- needs-input: a decision is ambiguous and materially affects the outcome. Ask EARLY
  rather than guessing — round-trips are cheap; wrong assumptions are not.
- blocked: you cannot proceed and no question would unblock you; explain why above the block.
- Work only inside your working directory.
- Do not commit or push unless the task explicitly instructs it.
- Report progress on milestones as you work.`

// Compose builds the injected system prompt for a turn: baked-in worker
// rules, then optional orchestrator additions.
func Compose(orchestratorAdditions string) string {
	if strings.TrimSpace(orchestratorAdditions) == "" {
		return workerRules
	}
	return workerRules + "\n\n# Orchestrator instructions\n\n" + orchestratorAdditions
}
