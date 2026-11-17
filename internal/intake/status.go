// status.go — S61 T6 (GI6): an intake's state is DERIVED, never stored.
// The row says what was linked, the ledger says how far the run got, the
// proposal says who was asked; the state word is a pure function of those,
// so no column can ever lie about a run and a crash cannot leave a session
// stuck on a word nobody can recompute.
package intake

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

// Status is the derived answer. State is one word from the closed set:
//
//	interviewing — no plan proposal yet (or only rejected ones); the session
//	             is still gathering (GI1's interview).
//	planned    — an open intake_plan exists and nothing has run.
//	committing — the ledger has rows; at least one step is not done.
//	committed  — the intake row names a root: the link step held.
//	abandoned  — the newest plan was rejected or discarded and no other is
//	             open (the person said no; nothing was committed).
type Status struct {
	IntakeID     string             `json:"intakeId"`
	State        string             `json:"state"`
	ProposalID   int64              `json:"proposalId,omitempty"`
	ProposalStat string             `json:"proposalStatus,omitempty"`
	RootID       string             `json:"rootId,omitempty"`
	TargetPath   string             `json:"targetPath,omitempty"`
	Steps        []store.IntakeStep `json:"steps,omitempty"`
	FailedStep   string             `json:"failedStep,omitempty"` // the last row that says failed, if any
}

// Status derives the answer. The proposal scan reads the OPEN proposals'
// payloads (an intake's plan names its own intake id — the chain is one
// link deep and this walk is over a desk that holds what a person was
// asked, not a haystack); the ledger arrives already ordered by ordinal.
func (e *Engine) Status(ctx context.Context, intakeID string) (Status, error) {
	if e.Store == nil {
		return Status{}, fmt.Errorf("intake refuses: the engine is not wired")
	}
	row, err := e.Store.GetIntake(intakeID)
	if err != nil {
		return Status{}, err
	}
	steps, err := e.Store.IntakeSteps(intakeID)
	if err != nil {
		return Status{}, err
	}
	st := Status{IntakeID: intakeID, RootID: row.RootID, TargetPath: row.TargetPath, Steps: steps}
	for _, s := range steps { // the last row that still says failed, if any
		if s.Outcome == "failed" && lastWord(steps, s.Step) == "failed" {
			st.FailedStep = s.Step
		}
	}
	// The link is the whole answer's anchor: a linked intake is committed,
	// whatever the ledger's oldest worries once said.
	if row.RootID != "" {
		if last := lastWord(steps, "link"); last == "done" {
			st.State = "committed"
			return st, nil
		}
	}
	proposals, err := e.Store.ListProposals("")
	if err != nil {
		return Status{}, err
	}
	var newest *store.Proposal
	newestOpen := false
	for _, p := range proposals {
		if p.Kind != string(desk.KindIntakePlan) || !namesIntake(p.Payload, intakeID) {
			continue
		}
		if newest == nil || p.ID > newest.ID {
			cp := p
			newest = &cp
		}
		if p.Status == "proposed" {
			newestOpen = true
			if newest == nil || p.ID > newest.ID {
				cp := p
				newest = &cp
			}
		}
	}
	if newest != nil {
		st.ProposalID = newest.ID
		st.ProposalStat = newest.Status
	}
	switch {
	case row.RootID != "":
		// linked without a done link row: the link step is mid-flight or
		// the ledger lied — either way, still committing, and the ledger's
		// last word names where.
		st.State = "committing"
	case newestOpen:
		if len(steps) == 0 {
			st.State = "planned"
		} else {
			st.State = "committing"
		}
	case newest != nil:
		st.State = "abandoned" // the newest plan was decided and not committed
	default:
		st.State = "interviewing"
	}
	return st, nil
}

// lastWord is the ledger's final word for one step name (rows arrive in
// ordinal order; the last row is the current truth for that step).
func lastWord(steps []store.IntakeStep, name string) string {
	out := ""
	for _, s := range steps {
		if s.Step == name {
			out = s.Outcome
		}
	}
	return out
}

// namesIntake peeks at one proposal payload for the intake it plans. A
// payload that does not even decode is not about this intake — Status is a
// read and never fails on somebody else's malformed row.
func namesIntake(payload, intakeID string) bool {
	var peek struct {
		IntakeID string `json:"intakeId"`
	}
	if err := json.NewDecoder(strings.NewReader(payload)).Decode(&peek); err != nil {
		return false
	}
	return peek.IntakeID == intakeID
}
