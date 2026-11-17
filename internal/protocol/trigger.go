package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// trigger.go — S20 T8: the OPERATIONAL control plane over the socket, so
// the app and the CLI are the same program asking. These are the four
// methods `aeon trigger list|pause|resume|fire-now` dial; each daemon-side
// handler calls the identical internal/trigger/control.go function its CLI
// sibling reaches (TestTriggerSocketAndCLICallSameControlFunctions is the
// call-site check — one control-plane implementation, P34).
//
// What is NOT here matters as much: none of these methods exists on the
// narrow surface (R6: operational control there hands a caller the means
// to bypass the guards written to constrain it — ForbiddenOperational-
// Control carries the words, and the pin test scans the whole table).
// And fire-now is honest over the socket exactly as over argv: the daemon
// starts the loop THROUGH THE TRIGGER PATH, unattended — a person
// watching is not a party the loop runs for, and TR2 admits no attended
// exception just because a human was in the room.

// TriggerListParams is empty by design: the listing is the whole
// operational state of the deployment's triggers — there is no filter
// field, because a filter would be the first step toward the narrow
// surface's read-projection of control state.
type TriggerListParams struct{}

// TriggerListView is one row's operational facts. Paused is the ONLY
// permission-shaped word in it: policy answers arrive in fire records,
// in S10's sentence, never through the listing — a plane that cannot
// evaluate governance must not print its implicit consent.
type TriggerListView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Blueprint string `json:"blueprint"`
	Expr      string `json:"expr,omitempty"`
	Zone      string `json:"zone,omitempty"`
	Window    string `json:"window,omitempty"`
	Workspace string `json:"workspace"`
	Goal      string `json:"goal,omitempty"`
	Paused    bool   `json:"paused"`
	NextFire  string `json:"nextFire,omitempty"`
}

type TriggerListResult struct {
	Triggers []TriggerListView `json:"triggers"`
}

// TriggerControlParams is the body of pause, resume and fire-now: a name,
// nothing else. No "as-user", no "attended", no "reason to override" —
// the parameter a caller could misuse is the parameter that someday is.
type TriggerControlParams struct {
	ID string `json:"id"`
}

// TriggerControlResult carries the answer. Found/Outcome are outcomes
// (T6's ruling: 404/409-shaped facts travel in the result, not as
// transport errors — a missing trigger is an answer, not a failure).
type TriggerControlResult struct {
	TriggerID string `json:"triggerId"`
	Outcome   string `json:"outcome,omitempty"`
	Reason    string `json:"reason,omitempty"`
	LoopID    string `json:"loopId,omitempty"`
	Paused    bool   `json:"paused"`
	Found     bool   `json:"found"`
}

func (t *Table) handleTriggerList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p TriggerListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if t.TriggerList == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger listing wired")
	}
	return t.TriggerList(ctx)
}

func (t *Table) handleTriggerPause(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodTriggerPause), raw); err != nil {
		return nil, err
	}
	var p TriggerControlParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a trigger pause must name its trigger id")}
	}
	if t.TriggerPause == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger pauser wired")
	}
	a, err := t.triggerActor(ctx, sess) // S49 AU1: the connection's person, first
	if err != nil {
		return nil, err
	}
	return t.TriggerPause(ctx, a, p.ID)
}

func (t *Table) handleTriggerResume(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodTriggerResume), raw); err != nil {
		return nil, err
	}
	var p TriggerControlParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a trigger resume must name its trigger id")}
	}
	if t.TriggerResume == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger resumer wired")
	}
	a, err := t.triggerActor(ctx, sess) // S49 AU1: the connection's person, first
	if err != nil {
		return nil, err
	}
	return t.TriggerResume(ctx, a, p.ID)
}

func (t *Table) handleTriggerFireNow(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodTriggerFireNow), raw); err != nil {
		return nil, err
	}
	var p TriggerControlParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a trigger fire-now must name its trigger id")}
	}
	if t.TriggerFireNow == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger fire-now wired")
	}
	a, err := t.triggerActor(ctx, sess) // S49 AU1: the connection's person, first
	if err != nil {
		return nil, err
	}
	return t.TriggerFireNow(ctx, a, p.ID)
}

// TriggerHistoryParams asks one trigger what it has ANSWERED. History is
// operational reporting, so it lives beside its verbs — and it is a read
// with no filter fields beyond the id: the five answers arrive as they
// were recorded, in S10's and the scheduler's own words.
type TriggerHistoryParams struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

type TriggerFireView struct {
	Instant string `json:"instant"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	LoopID  string `json:"loopId,omitempty"`
	At      string `json:"at"`
}

// TriggerGapView is the second half of TR4's honesty: when the daemon was
// DOWN, the instants are recorded as ONE range and count (0034), not
// invented answers — history shows what was answered AND what silence
// means.
type TriggerGapView struct {
	CoveredFrom string `json:"coveredFrom"`
	CoveredTo   string `json:"coveredTo"`
	Missed      int    `json:"missed"`
	At          string `json:"at"`
}

type TriggerHistoryResult struct {
	TriggerID string            `json:"triggerId"`
	Fires     []TriggerFireView `json:"fires"`
	Gaps      []TriggerGapView  `json:"gaps"`
}

func (t *Table) handleTriggerHistory(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p TriggerHistoryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("trigger history must name its trigger id")}
	}
	if t.TriggerHistory == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger history reader wired")
	}
	return t.TriggerHistory(ctx, p.ID, p.Limit)
}

// TriggerAddParams is one row as typed: TR1's named blueprint, the
// wall-clock contract, and — for the webhook kind — the SECRET'S ENV VAR
// NAME only (C17: a value typed anywhere in this params would be shell
// history and process lists; the row can name the variable and nothing
// else).
type TriggerAddParams struct {
	ID          string `json:"id"`
	Kind        string `json:"kind,omitempty"`
	Blueprint   string `json:"blueprint"`
	Expr        string `json:"expr,omitempty"`
	Zone        string `json:"zone,omitempty"`
	Workspace   string `json:"workspace"`
	Goal        string `json:"goal,omitempty"`
	WindowStart string `json:"windowStart,omitempty"`
	WindowEnd   string `json:"windowEnd,omitempty"`
	WindowDays  string `json:"windowDays,omitempty"`
	SecretEnv   string `json:"secretEnv,omitempty"`
	// EventFilter limits a webhook to one event (and optionally actions): `issues:opened`. Empty listens to every signed delivery.
	EventFilter string `json:"eventFilter,omitempty"`
}

func (t *Table) handleTriggerAdd(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectAutomationIdentityKeys(string(MethodTriggerAdd), raw); err != nil {
		return nil, err
	}
	var p TriggerAddParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a trigger must be added with an id — it is what pause, history and every fire record name")}
	}
	if t.TriggerAdd == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no trigger registrar wired")
	}
	a, err := t.triggerActor(ctx, sess) // S49 AU1: the connection's person, first
	if err != nil {
		return nil, err
	}
	return t.TriggerAdd(ctx, a, p)
}
