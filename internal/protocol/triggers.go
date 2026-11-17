package protocol

// triggers.go — S40's automation desk on the socket (AU14): three reads over
// the EventTriggerReader seam, and four write doors over EventTriggerWriter,
// human-only (AU1) — the actor is the connection's peer, never a params
// claim, and an agent is refused before anything is judged. The views carry
// the trigger's canonical match object and its kickoff TEMPLATE — never a
// rendered body — so a read can never leak a goal's text through a preview.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// EventFireView is one row of the fire log. Outcome "" is the claimed,
// not-yet-decided state — the crash window AU13 answers — and it is shown,
// never hidden: a page that cannot show "in flight" will show a confident
// wrong answer (P31).
type EventFireView struct {
	ID          int64  `json:"id"`
	TriggerID   string `json:"triggerId"`
	OutboxID    int64  `json:"outboxId"`
	SubjectKind string `json:"subjectKind"`
	SubjectID   string `json:"subjectId"`
	Depth       int    `json:"depth"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason"`
	LoopID      string `json:"loopId"`
	LoopSlug    string `json:"loopSlug"`
	CostNote    string `json:"costNote"`
	ClaimedAt   string `json:"claimedAt"`
	DecidedAt   string `json:"decidedAt"`
}

// EventTriggerView is one trigger as the desk sees it. LastFire is the
// trigger's most recent recorded answer — null when it never fired, which
// the page renders as "never", never as broken.
type EventTriggerView struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Source           string          `json:"source"`
	RootID           string          `json:"rootId"`
	ComponentID      string          `json:"componentId"`
	Match            json.RawMessage `json:"match"`
	Blueprint        string          `json:"blueprint"`
	Kickoff          string          `json:"kickoff"`
	MaxRunsPerHour   int             `json:"maxRunsPerHour"`
	MaxCostPerDayUSD *float64        `json:"maxCostPerDayUsd"`
	Enabled          bool            `json:"enabled"`
	DisabledReason   string          `json:"disabledReason"`
	CreatedByKind    string          `json:"createdByKind"`
	CreatedByID      string          `json:"createdById"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
	LastFire         *EventFireView  `json:"lastFire"`
}

type EventTriggerListParams struct {
	RootID      string `json:"rootId,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type EventTriggerListResult struct {
	Triggers []EventTriggerView `json:"triggers"`
}

type EventTriggerShowParams struct {
	ID string `json:"id"`
}

type EventTriggerFiresParams struct {
	TriggerID string `json:"triggerId,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	BeforeID  int64  `json:"beforeId,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type EventTriggerFiresResult struct {
	Fires  []EventFireView `json:"fires"`
	Counts map[string]int  `json:"counts"`
}

// eventFireOutcomeNames is AU12's closed five, spelled once. The daemon's
// reader test pins this list against the store's EventFireOutcomes() — the
// seam cannot drift, because protocol must not import store (the onion) and
// two lists without a pin are two truths.
var eventFireOutcomeNames = []string{"started", "refused", "limited", "skipped", "could-not-say"}

func isEventFireOutcome(s string) bool {
	for _, o := range eventFireOutcomeNames {
		if o == s {
			return true
		}
	}
	return false
}

// EventTriggerReader is the daemon's share: it maps store rows to views.
// Store failures arrive as plain errors (the handler layer wraps nothing);
// ErrStoreUnavailable is for the unwired seam only.
type EventTriggerReader interface {
	ListEventTriggers(p EventTriggerListParams) ([]EventTriggerView, error)
	ShowTrigger(id string) (EventTriggerView, bool, error) // bool = found
	ListEventFires(p EventTriggerFiresParams) ([]EventFireView, map[string]int, error)
}

func (t *Table) handleAutomationTriggerList(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p EventTriggerListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if t.TriggerReads == nil {
		return nil, ErrStoreUnavailable
	}
	trs, err := t.TriggerReads.ListEventTriggers(p)
	if err != nil {
		return nil, err
	}
	if trs == nil {
		trs = []EventTriggerView{} // an empty page is [], never null — the desk iterates without ?.
	}
	return EventTriggerListResult{Triggers: trs}, nil
}

func (t *Table) handleAutomationTriggerShow(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p EventTriggerShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.TriggerReads == nil {
		return nil, ErrStoreUnavailable
	}
	v, found, err := t.TriggerReads.ShowTrigger(p.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrBadParams{Err: fmt.Errorf("no trigger %q", p.ID)}
	}
	return v, nil
}

func (t *Table) handleAutomationTriggerFires(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p EventTriggerFiresParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Outcome != "" && !isEventFireOutcome(p.Outcome) {
		return nil, ErrBadParams{Err: fmt.Errorf("outcome must be one of %s", strings.Join(eventFireOutcomeNames, ", "))}
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 500 {
		p.Limit = 500
	}
	if t.TriggerReads == nil {
		return nil, ErrStoreUnavailable
	}
	fires, counts, err := t.TriggerReads.ListEventFires(p)
	if err != nil {
		return nil, err
	}
	if fires == nil {
		fires = []EventFireView{}
	}
	return EventTriggerFiresResult{Fires: fires, Counts: counts}, nil
}

// ---------------------------------------------------------------------------
// The write surface (T8). AU14's other half — and AU1's: four human-only
// doors. The actor is derived from the CONNECTION, never from params: a
// params field named actor would die in decode like every smuggled actor
// before it (S38's rule). The order in every handler is decode → ACTOR →
// field validation → seam — an agent learns nothing about validity, because
// it is refused before anything is judged.

// Actor names who asked. "user" is the only kind that may write triggers
// (AU1); the daemon stamps it from the connection's peer credential, exactly
// as the records seam does — the uid is the kernel's, not a claim.
type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// EventTriggerCreateParams files a standing human instruction (AU1).
type EventTriggerCreateParams struct {
	Name             string          `json:"name"`
	RootID           string          `json:"rootId,omitempty"`
	ComponentID      string          `json:"componentId,omitempty"`
	Match            json.RawMessage `json:"match"`
	Blueprint        string          `json:"blueprint"`
	Kickoff          string          `json:"kickoff,omitempty"`
	MaxRunsPerHour   int             `json:"maxRunsPerHour"`
	MaxCostPerDayUSD *float64        `json:"maxCostPerDayUsd,omitempty"`
}

// EventTriggerUpdateParams edits the given fields. Scope ids are NOT
// editable — to move a trigger, create another (the fire ledger's history
// stays where its events are). ClearMaxCost is its own intention (the §4c
// discipline: absent means keep, clear means clear).
type EventTriggerUpdateParams struct {
	ID               string          `json:"id"`
	Name             *string         `json:"name,omitempty"`
	Match            json.RawMessage `json:"match,omitempty"`
	Blueprint        *string         `json:"blueprint,omitempty"`
	Kickoff          *string         `json:"kickoff,omitempty"`
	MaxRunsPerHour   *int            `json:"maxRunsPerHour,omitempty"`
	MaxCostPerDayUSD *float64        `json:"maxCostPerDayUsd,omitempty"`
	ClearMaxCost     bool            `json:"clearMaxCost,omitempty"`
}

type EventTriggerEnableParams struct {
	ID string `json:"id"`
}

type EventTriggerDisableParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// EventTriggerWriter is the daemon's write seam. The actor is a parameter
// the HANDLER supplies from the connection — the seam re-checks with the
// store (belt and braces) and no caller picks its own kind.
type EventTriggerWriter interface {
	CreateEventTrigger(a Actor, p EventTriggerCreateParams) (EventTriggerView, error)
	UpdateEventTrigger(a Actor, p EventTriggerUpdateParams) (EventTriggerView, error)
	EnableEventTrigger(a Actor, id string) (EventTriggerView, error)
	DisableEventTrigger(a Actor, id, reason string) (EventTriggerView, error)
}

// triggerActor answers WHO before anything else gets an answer. An unwired
// ActorFor is the honest absence (a storeless daemon has no persons), and an
// empty kind is NEVER defaulted to user — refusing the shapeless actor is
// what keeps an agent process from writing by omission.
// triggerActor is AU1's protocol lock, T4-shaped: the same two sentences
// S40 spoke (VERBATIM — the daemon's store backstop quotes them too), now
// carried by the codes S49 reserved for them. Unresolvable (no seam, no
// id) is ErrAutomationNoActor — the honest absence that stops a write; a
// named non-person is ErrAutomationRefused carrying S40's sentence. T4
// extended this lock to EVERY trigger write — the S20 doors included,
// where it previously refused only by the params-level absence of an
// actor field plus the store's backstop.
func (t *Table) triggerActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrAutomationNoActor.WithWhat("triggers are written by a person; no identity is available on this connection")
	}
	a := t.ActorFor(ctx, sess)
	if a.Kind != "user" {
		return Actor{}, ErrAutomationRefused.WithWhat("triggers are written by a person; this actor is %q", a.Kind)
	}
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrAutomationNoActor.WithWhat("triggers are written by a person; no identity is available on this connection")
	}
	return a, nil
}

func (t *Table) handleAutomationTriggerCreate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodAutomationTriggerCreate), raw); err != nil {
		return nil, err
	}
	var p EventTriggerCreateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := t.triggerActor(ctx, sess) // FIRST: before any state, any judgement
	if err != nil {
		return nil, err
	}
	p.Name = strings.TrimSpace(p.Name)
	p.Blueprint = strings.TrimSpace(p.Blueprint)
	switch {
	case p.Name == "":
		return nil, ErrBadParams{Err: fmt.Errorf("name is required")}
	case len(p.Match) == 0 || string(p.Match) == "null":
		return nil, ErrBadParams{Err: fmt.Errorf("match is required — a trigger with no event to watch is a wish")}
	case p.Blueprint == "":
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint is required")}
	case p.MaxRunsPerHour < 1:
		return nil, ErrBadParams{Err: fmt.Errorf("maxRunsPerHour must be at least 1 (AU11: a trigger without a rate is an outage with a name)")}
	}
	if t.TriggerWrites == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TriggerWrites.CreateEventTrigger(a, p)
}

func (t *Table) handleAutomationTriggerUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodAutomationTriggerUpdate), raw); err != nil {
		return nil, err
	}
	var p EventTriggerUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := t.triggerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name must not be blank — omit it to keep it")}
	}
	if p.Blueprint != nil && strings.TrimSpace(*p.Blueprint) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint must not be blank — omit it to keep it")}
	}
	if p.MaxRunsPerHour != nil && *p.MaxRunsPerHour < 1 {
		return nil, ErrBadParams{Err: fmt.Errorf("maxRunsPerHour must be at least 1 (AU11)")}
	}
	if t.TriggerWrites == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TriggerWrites.UpdateEventTrigger(a, p)
}

func (t *Table) handleAutomationTriggerEnable(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodAutomationTriggerEnable), raw); err != nil {
		return nil, err
	}
	var p EventTriggerEnableParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := t.triggerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.TriggerWrites == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TriggerWrites.EnableEventTrigger(a, p.ID)
}

func (t *Table) handleAutomationTriggerDisable(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodAutomationTriggerDisable), raw); err != nil {
		return nil, err
	}
	var p EventTriggerDisableParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := t.triggerActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Reason = strings.TrimSpace(p.Reason)
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if p.Reason == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("reason is required — a pause nobody can explain is a switch, not a decision")}
	}
	if t.TriggerWrites == nil {
		return nil, ErrStoreUnavailable
	}
	return t.TriggerWrites.DisableEventTrigger(a, p.ID, p.Reason)
}
