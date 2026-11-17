package protocol

// automation_write.go — S49 T4's three NEW doors beside the strictness
// they taught to the eleven that came before (AU1's plane made whole):
//
//   - trigger.update — the clock-and-webhook family's edit door, beside
//     trigger.add's creation door. Params arrive as sparse edits (a field
//     that stays absent stays unchanged; clearWindow is the pair's own
//     emptier — half a window is refused by the validator, never stored).
//     The result carries BEFORE and AFTER views, the changed flag, and the
//     clock's own next instants for schedules; a validation refusal is an
//     ANSWER (reason set, changed false) exactly as trigger.add answers —
//     the control plane speaks in sentences, and this door does not
//     invent a second grammar for them.
//   - automation.archive / automation.unarchive — cross-family: the source
//     is part of the name, and archive is a STATE (AN6), never a delete:
//     before/after views beside a health answer, because "could it fire
//     now" is the question an archive exists to answer no to.
//
// # Identity, params, and the standing decode law
//
// Every write here (and on the S20/S40 doors T4 tightened) refuses the
// identity KEYS by name before anything else: a params bag carrying
// actor/actorKind/createdBy/origin is answered the shape error naming the
// key. This is the package's standing stand-in for DisallowUnknownFields
// (mcp_admin's rejectIdentityKeys, policy_authoring's list) — strict
// decoding is forbidden package-wide (TestNothingCallsDisallowUnknownFields,
// T3's additive-evolution rule), so a forged identity is refused by KEY and
// an innocent unknown stays ignored. The actor is the CONNECTION's
// (table.ActorFor, peer-stamped); unresolvable is ErrAutomationNoActor
// (-31892) and a non-person is ErrAutomationRefused (-31893) carrying
// S40's own sentence VERBATIM — "triggers are written by a person; this
// actor is %q". One sentence, four doors, three stores, no paraphrase.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ── the seam (V13: nil ⇒ all three doors ABSENT) ───────────────────────────

// AutomationWrite is the daemon's write service for the three new doors.
// The actor is resolved by the HANDLER (from the connection) and handed
// down; the seam must not re-derive it and has no way to.
type AutomationWrite interface {
	TriggerUpdate(ctx context.Context, a Actor, p TriggerUpdateParams) (TriggerUpdateResult, error)
	Archive(ctx context.Context, a Actor, p AutomationArchiveParams) (AutomationArchiveResult, error)
	Unarchive(ctx context.Context, a Actor, p AutomationArchiveParams) (AutomationArchiveResult, error)
}

// RegisterAutomationWrites registers the three write doors iff the seam is
// wired (V13 — a storeless daemon's plane is ABSENT, not refusing). The
// S20/S40 doors that predate S49 keep their registration where it always
// lived; what T4 changed about them is who may call them, not whether the
// door exists.
func (t *Table) RegisterAutomationWrites() {
	if t.AutomationWrite == nil {
		return
	}
	t.register(&Method{Name: MethodTriggerUpdate, RequiresInitialize: true, Handle: t.handleTriggerUpdate})
	t.register(&Method{Name: MethodAutomationArchive, RequiresInitialize: true, Handle: t.handleAutomationArchive})
	t.register(&Method{Name: MethodAutomationUnarchive, RequiresInitialize: true, Handle: t.handleAutomationUnarchive})
}

// ── params and results ──────────────────────────────────────────────────────

// TriggerUpdateParams is the sparse edit. Every pointer is "only if
// named": absent means unchanged, and there is no value that means
// unchanged — an empty cron is refused by the validator, not swallowed.
// created_by_*, kind and id are not fields at all (immutables have no
// door); secretEnv names the ENVIRONMENT VARIABLE and nothing else — a
// value in a param is a credential in transit (§4c), so there is no field
// that could carry one.
type TriggerUpdateParams struct {
	ID          string  `json:"id"`
	Blueprint   *string `json:"blueprint,omitempty"`
	Cron        *string `json:"cron,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
	Workspace   *string `json:"workspace,omitempty"`
	Goal        *string `json:"goal,omitempty"`
	WindowStart *string `json:"windowStart,omitempty"`
	WindowEnd   *string `json:"windowEnd,omitempty"`
	WindowDays  *string `json:"windowDays,omitempty"`
	SecretEnv   *string `json:"secretEnv,omitempty"`
	// EventFilter sets a webhook's event filter (`issues:opened`); an empty string clears it.
	EventFilter *string `json:"eventFilter,omitempty"`
	// ClearWindow empties the whole pair at once. It exists because an
	// empty windowStart with a set windowEnd is half a window — refused by
	// the validator — and the emptier must not be that ambiguity.
	ClearWindow bool `json:"clearWindow,omitempty"`
}

// TriggerUpdateResult answers before/after views, whether anything moved,
// the clock's next instants (schedules), and the refusal sentence when the
// edit was declined — add-shaped honesty: a declined write is an answer
// with a sentence, not a transport error.
type TriggerUpdateResult struct {
	Before    AutomationView      `json:"before"`
	After     AutomationView      `json:"after"`
	Changed   bool                `json:"changed"`
	NextFires []AutomationInstant `json:"nextFires,omitempty"`
	Reason    string              `json:"reason,omitempty"`
}

// AutomationArchiveParams names the family and the row. Reason is the
// archive's companion sentence (1–256 characters; archived_reason holds
// it, the audit's summary holds ids and enums only); unarchive takes the
// same shape and simply does not ask why.
type AutomationArchiveParams struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// AutomationArchiveResult carries the before/after views and the health
// answer beside them — the door that flips the engine-visible state is the
// door that owes the "could it fire now" that changed.
type AutomationArchiveResult struct {
	Before  AutomationView       `json:"before"`
	After   AutomationView       `json:"after"`
	Changed bool                 `json:"changed"`
	Health  AutomationHealthView `json:"health"`
}

// ── the identity rejection (standing decode law, automation's keys) ────────

// automationIdentityKeys are the params keys that would let a caller stamp
// its own identity or a row's authorship on a trigger write. The package
// decodes leniently (T3's additive-evolution rule); this list is the hard
// edge: a bag carrying one is refused the shape error, naming the key.
var automationIdentityKeys = []string{"actor", "actorKind", "createdBy", "createdByKind", "origin", "changedBy", "archivedBy"}

// rejectAutomationIdentityKeys refuses any params bag that carries an
// identity key. -32602 is JSON-RPC's own for a wrong-shaped request, and
// "you named your own identity" is exactly that.
func rejectAutomationIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope, in its own words
	}
	for _, key := range automationIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor and the authorship are stamped by the daemon from the connection, never named by the caller (S49 AU1)",
				method, key)}
		}
	}
	return nil
}

// ── handlers ────────────────────────────────────────────────────────────────

func (t *Table) handleTriggerUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodTriggerUpdate), raw); err != nil {
		return nil, err
	}
	var p TriggerUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a trigger update must name the trigger id it edits")}
	}
	a, err := t.triggerActor(ctx, sess) // FIRST: before any state, any judgement
	if err != nil {
		return nil, err
	}
	return t.AutomationWrite.TriggerUpdate(ctx, a, p)
}

func (t *Table) handleAutomationArchive(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	return t.automationArchiveDoor(ctx, sess, MethodAutomationArchive, raw, true)
}

func (t *Table) handleAutomationUnarchive(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	return t.automationArchiveDoor(ctx, sess, MethodAutomationUnarchive, raw, false)
}

// automationArchiveDoor is the two doors' shared body — the source check,
// the reason bound (archive only), the actor, and the seam call in the
// order that never lets an unauthenticated byte touch a query.
func (t *Table) automationArchiveDoor(ctx context.Context, sess *Session, method MethodName, raw json.RawMessage, withReason bool) (any, error) {
	if err := rejectAutomationIdentityKeys(string(method), raw); err != nil {
		return nil, err
	}
	var p AutomationArchiveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.Source = strings.TrimSpace(p.Source)
	p.ID = strings.TrimSpace(p.ID)
	switch p.Source {
	case "schedule", "webhook", "event":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("%q is not a source — schedule, webhook and event are the three families, and the source is part of the name", p.Source)}
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("an archive door must name the trigger id it changes")}
	}
	if withReason {
		n := len([]rune(strings.TrimSpace(p.Reason)))
		if n < 1 || n > 256 {
			return nil, ErrBadParams{Err: fmt.Errorf("an archive needs a reason of 1–256 characters — 'off' with no sentence is a trigger nobody can review back (got %d)", n)}
		}
	}
	a, err := t.triggerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	if withReason {
		return t.AutomationWrite.Archive(ctx, a, p)
	}
	return t.AutomationWrite.Unarchive(ctx, a, p)
}
