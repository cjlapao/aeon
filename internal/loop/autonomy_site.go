package loop

// autonomy_site.go — S56 T5's decision site (AL6, AL9, AL10). The one place a
// running loop consults its pinned grant against the live store facts, at the
// moment of an action. It bridges the store's AutonomyLive read (AL6) to the
// pure action.ResolveWithGrant verdict (AL9) and the action-time conditions
// (AL4) through the seams, and it is where the use log is written (AL10).
//
// Everything after the verdict switch in execute.go is unchanged: this file only
// builds the GrantContext the verdict reads and the grantUse the use log writes.
// Attended loops (and, downstream, local actions) get an empty context — no read,
// no log — so the legacy ask/run is byte-identical (OD2). A store read error is
// returned, never folded into "no pin": the runner turns it into runner-error,
// exactly as LoopAllowOutward's error does (fail-toward-deny, AL9).

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// grantUse is the handle the use log writes for one unattended effect taken
// under a grant (AL10): the grant id, the pinned revision and the action. A nil
// handle means the action ran without a grant (a no-grant, a local, or an
// attended loop) and writes no use row. It is created only when the action is in
// the pin's snapshot; the decision site decides whether it runs.
type grantUse struct {
	GrantID string
	Rev     int64
	Action  string
}

// AutonomySeams is the cross-slice condition set the decision site is handed
// (AL4, R1, R2). nil seams are cannot-say — a merge grant is inert until S57
// wires Gates, a budget grant inert until S55 wires Budget. The daemon passes
// them at Deps construction; the loop never builds them itself (it would be a
// second authority for what the seams own).
type AutonomySeams struct {
	Seams autonomy.Seams
}

// grantContextFor builds the decision input for one action (AL9). It is the one
// place a running loop reads the autonomy store; the result enters the verdict
// only through ResolveWithGrant, exactly as the pin entered the start through
// startExtras (T4). It returns the context and — when the action is in the pin's
// snapshot — a grantUse handle for the use log (AL10).
//
//  1. attended  ⇒  GrantContext{} (no read; the attended ask/run is untouched, OD2).
//  2. AutonomyLive  ⇒  a read error is returned (never folded into "no pin").
//  3. LiveState from the raw facts: the veto, the ceiling's live status
//     (revoked/expired), and the covered grant's action-time conditions, each
//     evaluated through the seams (a nil seam is cannot-say, AL4).
func grantContextFor(ctx context.Context, dep Deps, st *LoopState, name string, attended bool) (action.GrantContext, *grantUse, error) {
	// 1. An attended loop is not this slice's business — no read, no log, the
	// legacy ask is untouched (OD2). A local action is decided downstream by
	// ResolveWithGrant, which returns base for a local class.
	if attended {
		return action.GrantContext{}, nil, nil
	}
	// 2. The live read. The outward expansion is the caller's (the store never
	// encodes the registry, AL2); a read error is returned, never folded into a
	// "no pin" — the runner turns it into runner-error (the same as LoopAllowOutward).
	outward := outwardActions()
	view, err := dep.Store.AutonomyLive(ctx, st.ID, name, outward, time.Now())
	if err != nil {
		return action.GrantContext{}, nil, err
	}
	// 3. The LiveState from the raw facts. A veto (with or without a pin) and a
	// live ceiling failure (revoked/expired) are carried as facts; ResolveWithGrant
	// decides. The conditions are the covered grant's, evaluated at action time.
	live := action.LiveState{
		Vetoed:    view.Vetoed,
		VetoID:    view.VetoGrant,
		Revoked:   view.Revoked,
		RevokedBy: "",
		RevokedAt: "",
		Expired:   view.Expired,
		GrantID:   view.CoveringGrant,
		Rev:       view.Rev,
	}
	var use *grantUse
	if view.Covered {
		// The covered grant's row: its live status (the store read may have seen a
		// different revocation) and its conditions, evaluated through the seams.
		g, gerr := dep.Store.ReadAutonomyGrant(view.CoveringGrant)
		if gerr != nil {
			return action.GrantContext{}, nil, gerr
		}
		if g.RevokedAt != nil {
			live.Revoked = true
			live.RevokedAt = *g.RevokedAt
		}
		if g.RevokedByKind != nil {
			live.RevokedBy = *g.RevokedByKind
		}
		// The action-time conditions (AL4): requireGates (forced on the merge
		// grant), requireBudget (on by default), maxPerDay. A nil seam is
		// cannot-say, never met. The merge gate's PR number is the one the
		// evidence rows recorded, exactly as actionArgs supplies it.
		live.Conditions = autonomy.Evaluate(ctx, decodeConditions(g.Conditions), g.ID, st.ID, prNumber(st), dep.Autonomy.Seams, time.Now())
		use = &grantUse{GrantID: g.ID, Rev: view.Rev, Action: name}
	}
	gctx := action.GrantContext{Pinned: view.Pinned, InPin: view.Covered, Live: live}
	return gctx, use, nil
}

// outwardActions is the current set of outward action names, read from the
// registry — the expansion the store's veto check needs (AL2). The store never
// encodes it; the decision site supplies it, so a class/outward veto cannot
// overreach a local or irreversible action.
func outwardActions() []string {
	classes := action.Classes()
	var out []string
	for _, name := range action.Names() {
		if classes[name] == action.EffectOutward {
			out = append(out, name)
		}
	}
	return out
}

// prNumber is the PR the merge gate (requireGates) asks about, from the evidence
// rows the actions recorded — the first non-zero pr_number, exactly the fallback
// actionArgs uses. Numbers come from evidence, never from prose (Q7/AL4).
func prNumber(st *LoopState) int {
	for _, ev := range st.Evidence {
		if p := atoiSafe(ev["pr_number"]); p != 0 {
			return p
		}
	}
	return 0
}

// decodeConditions turns a stored grant's canonical conditions JSON into the typed
// set Evaluate decides with. The store's CHECK is json_valid, so a malformed value
// is a broken store; the decision site answers cannot-say (a nil Conditions reads
// as a condition that cannot be met), never a silent allow.
func decodeConditions(s string) autonomy.Conditions {
	var c autonomy.Conditions
	if s == "" {
		return c
	}
	_ = json.Unmarshal([]byte(s), &c)
	return c
}

// recordAutonomyUse writes one use row and, only when that write LANDS,
// publishes the used event (AL10, R5): the event is the signal of a row
// that exists — a write that failed publishes nothing, because an event for
// a row that is not there is a second truth. A write failure does not block
// the step: the effect has already happened, and blocking here would punish
// the loop for an audit failure (the pre-S56 call sites ignored the error
// for the same reason). The payload carries the loop's OWN project root
// (S31 D1: a loop's events are filed under the loop row's real root), and
// the daemon's publishLoop routes it there through ComponentRooted.
func recordAutonomyUse(dep Deps, st *LoopState, use *grantUse, phase string, begunID int64) {
	if _, err := dep.Store.RecordAutonomyUse(st.ID, use.GrantID, use.Rev, use.Action, phase, begunID, ""); err != nil {
		return
	}
	emit(dep, protocol.KindAutonomyUsed, protocol.UsedEventFor(st.ID, use.GrantID, use.Action, phase, begunID, st.ComponentRoot))
}
