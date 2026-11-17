package protocol

// ════════════════════════════════════════════════════════════════════════════
// automation_propose.go — S49 T7 (AN9): the automation plane's ONE agent
// door, `automation.proposeDisable`, answered by the records service
// (Table.Records — RecordsAPI.Do), NOT by the automation write service. It
// creates an INERT proposal row of kind automation_disable — never a change
// to a schedule, a webhook or an event trigger: the plane's eleven writes
// (the clock family's add/update/pause/resume/fire-now, the webhook's
// secret turns, the event family's create/update/enable/disable/archive and
// the cross-family archive pair) stay the person's human door, and no tool,
// no method and no proposal shape reaches them.
//
// An agent may SEE what runs on its own (the four read twins ride T3's
// standing doors, answered by the SAME services the person's panel reads),
// and ASK a person to switch one off; the proposal constrains nothing until
// a person commits it (S35 TD2), and the commit stamps the COMMITTER with
// T6's own write paths (AN9's narrowing-only: a disable can only take
// authority away — there is deliberately no propose-enable, -update or
// -archive ask, because a narrowing cannot widen).
//
// The catalog rows (internal/records' five `automation` specs) are the one
// definition; this file is the socket's half of the propose door, exactly as
// environment_propose.go does it for the environment revision: registered
// CONDITIONALLY (V13, the RegisterSeed rule) because this is a MUTATING
// catalog action and a table without the records service leaves it ABSENT,
// not registered-and-refusing. registerRecords skips the whole automation
// family (the four reads are owned by RegisterAutomationReads, which runs
// after NewTable, and this name by RegisterAutomationProposeDisable — a
// records projection here would double-register at the first wiring and keep
// the propose door alive in a build whose records service never linked).
//
// What the door refuses, and where: the catalog schema (additionalProperties:
// false) carries NO actor, status, kind, enabled, archived or target-state
// field — identity is the step's, stamped on the service side from the step
// context (OL3); the row's kind is implied (automation_disable); and there is
// no baseline to type because there is no target to choose: the effect can
// only disable. The service's strict decode (daemon/records_input.go) is
// where a smuggled "actor", "enabled" or "archived" meets a refusal that
// names it — before any validator, before any row.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"errors"
)

// automationProposeDisableTool is the catalog tool name the twin dispatches
// under (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not a
// second method table).
const automationProposeDisableTool = "automation_propose_disable"

// AutomationProposeDisableParams is the propose door's documented params —
// the catalog schema's shape as a Go type so the conformance fixtures
// round-trip through it. WHICH automation (the source is part of its name,
// AN1) and WHY, for the person deciding; nothing else has anywhere to go (a
// smuggled "actor", "enabled" or "status" meets the service's strict decode
// and its refusal names the field).
type AutomationProposeDisableParams struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// RegisterAutomationProposeDisable registers the twin when (and only when)
// the records service is wired — the RegisterSeed/V13 rule the policy,
// autonomy, blueprint and environment twins carry: a table without
// Table.Records leaves it ABSENT (ErrUnknownMethod), never
// registered-and-refusing, because the records service is what honours what
// makes this door safe (the stamped agent actor, the shared create validator
// of T6, the per-agent open-proposal cap). Returns true when it stands
// (already or just now); false when the seam is missing.
func (t *Table) RegisterAutomationProposeDisable() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodAutomationProposeDisable]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodAutomationProposeDisable,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// One definition, two doors: the SAME RecordsAPI.Do the tool path
			// calls, the actor stamped on the service side from the step's
			// identity, never decoded from params (OL3).
			out, sentence, err := t.Records.Do(ctx, sess, automationProposeDisableTool, params)
			if err != nil {
				return nil, err
			}
			if sentence != "" {
				return nil, ErrBadParams{Err: errors.New(sentence)}
			}
			return out, nil
		},
	})
	return true
}
