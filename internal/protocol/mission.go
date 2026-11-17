package protocol

// mission.go — S58 T6 (SV11): the wire shape of `mission.status`, the
// per-root overview. All reading and derivation lives in
// `internal/mission`; this file is the one adapter between those values and
// the socket (the same one-adapter rule as inbox.go: field-for-field twins,
// mapped, never recomputed).
//
// # Absence (V13)
//
// The method registers only over a non-nil Mission seam. An unwired build
// answers method-not-found, never a half-overview.
//
// # What the result never carries
//
// No health score, no traffic light (SV11's no-composite-judgment rule);
// spend is a FLOOR labelled by its cost state (P32), and an unwired seam
// says `not-wired` — never a zero that would read as "checked, nothing
// found" (T6's Do-NOT list).

import (
	"context"
	"encoding/json"
)

// MissionService is the daemon seam: the evaluation, assembled. The daemon
// builds `internal/mission`'s deps (its store, the SAME InboxService that
// answers inbox.summary, the two registrars' seams) and maps the result.
type MissionService interface {
	MissionStatus(ctx context.Context, p MissionStatusParams) (MissionStatusResult, error)
}

// MissionStatusParams: the optional root. Empty is every live root, the
// inbox scope convention (SV1) restated — one component id scopes to that
// component's tree.
type MissionStatusParams struct {
	RootID string `json:"rootId,omitempty"`
}

// MissionLoopsCounts is the four-live-status rollup — no terminal counters
// (the statuses the engine ends loops on are excluded, not invented).
type MissionLoopsCounts struct {
	Running int `json:"running"`
	Waiting int `json:"waiting"`
	Blocked int `json:"blocked"`
	Idle    int `json:"idle"`
}

// MissionSpend is the CT7 rollup of the root's row (the root's row IS the
// subtree total). UsdAtLeast is a FLOOR whenever CostState is partial or
// unknown (P32) — the name itself says at-least; a surface may round it for
// display but may never drop the word.
type MissionSpend struct {
	State      string  `json:"state"` // "reporting"
	UsdAtLeast float64 `json:"usdAtLeast"`
	CostState  string  `json:"costState"` // known|partial|unknown
}

// MissionCap relays the S55 seam: not-wired (no BudgetReader), none (no cap
// over this subtree), in-force (the budget's own summary words).
type MissionCap struct {
	State    string  `json:"state"`
	Summary  string  `json:"summary,omitempty"`
	LimitUSD float64 `json:"limitUsd,omitempty"`
	Period   string  `json:"period,omitempty"`
}

// MissionNextReady relays the S27 seam, the queue's own words when ready.
type MissionNextReady struct {
	State    string `json:"state"`
	LoopSlug string `json:"loopSlug,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// MissionComponentStatus is one root's five facts and its last movement.
// The needs-you block is the inbox.summary result for the same scope — the
// consistency test pins the equality; a second truth about what waits would
// be exactly the drift SV11 refuses to ship.
type MissionComponentStatus struct {
	RootID         string             `json:"rootId"`
	Name           string             `json:"name"`
	Loops          MissionLoopsCounts `json:"loops"`
	NeedsYou       InboxSummaryResult `json:"needsYou"`
	Spend          MissionSpend       `json:"spend"`
	Cap            MissionCap         `json:"cap"`
	NextReady      MissionNextReady   `json:"nextReady"`
	LastActivityAt string             `json:"lastActivityAt,omitempty"`
}

// MissionStatusResult is the overview: one entry per evaluated root, the
// order the daemon's root listing gives.
type MissionStatusResult struct {
	Components []MissionComponentStatus `json:"components"`
}

func (t *Table) handleMissionStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p MissionStatusParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	res, err := t.Mission.MissionStatus(ctx, p)
	if err != nil {
		// The same scope refusal the inbox renders (SV1's one scope
		// convention; an unknown rootId is the same fact on either door).
		return nil, inboxScopeRefusal(err)
	}
	return res, nil
}

// RegisterMission registers the read iff its seam is wired (V13, the
// RegisterInbox shape), returning whether it did.
func (t *Table) RegisterMission() bool {
	if t.Mission == nil {
		return false
	}
	t.register(&Method{Name: MethodMissionStatus, RequiresInitialize: true, Handle: t.handleMissionStatus})
	return true
}
