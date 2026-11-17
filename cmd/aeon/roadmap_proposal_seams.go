// roadmap_proposal_seams.go — S50 T5 (RM8): the store halves of the
// roadmap_idea proposal kind, as ONE seam map that BOTH doors run.
//
// The desk (internal/desk/roadmap_kinds.go) holds the pure shape: a strict
// payload of {componentRoot, title, epic?, note} — no rank, no size, no
// status, no actor anywhere. What it may not read, the seam reads: the
// component registry (does the root this idea names exist at all?) and the
// board (does an identical OPEN item already exist?). The seam's Validate
// REPLACES the desk's shape half with the full desk-then-store function —
// the merge rule in desk.New — so the create door and the commit door run
// ONE function (S35 TD3): what create refuses, commit refuses, in the same
// words, and what create accepts is never a surprise a person commits blind.
//
// The refusal of the ALREADY-EXISTS ask is RM8's, and it is the automation
// family's "already off" refusal in the roadmap's words: an ask that adds
// what the board already carries changes nothing, and an ask that changes
// nothing is refused at create, not carried to a commit nobody should have
// to make. The crash-retry is safe beside it: decide.go runs AlreadyApplied
// BEFORE Validate, so a commit that died mid-apply completes rather than
// meeting its own footprint as a refusal.
//
// Apply is roadmap's own create — CreateAudited, provenance authored, the
// COMMITTING PERSON as actor, one 'add' audit line in the same transaction.
// Not the proposer, never a placeholder: a write whose actor is guessed is
// the audit gap the fail-closed pair below exists to refuse (S49 T6's
// discipline, restated at S50's table).

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// seamValidateRoadmapIdea is the full Validate both doors run: the desk's
// shape half first (its sentences travel verbatim), then the two facts a
// shape cannot carry.
func seamValidateRoadmapIdea(ctx context.Context, payload []byte) error {
	if err := desk.ValidateRoadmapIdea(ctx, payload); err != nil {
		return err
	}
	p, _, dup, err := roadmapIdeaFacts(payload)
	if err != nil {
		return err
	}
	if dup {
		return fmt.Errorf("refused: %q is already an open roadmap item under %q — an ask that adds what the board carries changes nothing, and asks that change nothing are refused at create (S50 T5, RM8)", p.Title, p.ComponentRoot)
	}
	return nil
}

// seamAlreadyAppliedRoadmapIdea answers the crash-retry: the identical open
// item EXISTS, so the effect has already landed and the decide flow marks
// the row committed without re-writing anything (decide.go runs this BEFORE
// Validate, so the already-exists refusal above can never trap a retry in
// its own footprint). A missing component or an unreadable board says NOT
// applied in the honest way — a store failure is an error, never a guess.
func seamAlreadyAppliedRoadmapIdea(ctx context.Context, payload []byte) (bool, error) {
	_, rootExists, dup, err := roadmapIdeaFacts(payload)
	if err != nil {
		if !rootExists {
			return false, nil // the root is gone: not applied, and commit's Validate will name it
		}
		return false, err
	}
	return dup, nil
}

// seamApplyRoadmapIdea writes the item — roadmap's own create, never a
// hand-rolled INSERT, so the row shape, the vocabulary and the ONE audit
// line are exactly what a person's roadmap.add writes. The committer comes
// from the commit door's context and the proposal id rides with it; a
// context missing either is broken wiring, not a shortcut.
func seamApplyRoadmapIdea(ctx context.Context, w io.Writer, payload []byte) error {
	a, ok := desk.DeciderFrom(ctx)
	if !ok || a.Kind != "user" || a.ID == "" {
		return errors.New("refused: roadmap_idea's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S50 T5, RM2)")
	}
	proposalID, ok := desk.ProposalIDFrom(ctx)
	if !ok || proposalID <= 0 {
		return errors.New("refused: roadmap_idea's apply needs the proposal's id from the commit door — the item traces back through it, and an apply that lost it is broken wiring, not a shortcut (S50 T5)")
	}
	p, _, _, err := roadmapIdeaFacts(payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Committing roadmap_idea: %q under %s — %s (proposed by ask %d, committed by user %s)\n", p.Title, p.ComponentRoot, p.Note, proposalID, a.ID)
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	r := roadmap.New(st)
	_, ref := r.CreateAudited(p.ComponentRoot, roadmap.NewItem{
		Title:      p.Title,
		Epic:       p.Epic,
		Provenance: roadmap.ProvenanceAuthored,
	}, roadmap.Actor{Kind: "user", ID: a.ID})
	if ref != nil {
		return fmt.Errorf("roadmap_idea %q: %s", p.Title, ref.Reason)
	}
	fmt.Fprintf(w, "Committed: %q stands on the roadmap under %s — provenance authored, audited by its own roadmap_change line, with ask %d recorded on the proposal\n", p.Title, p.ComponentRoot, proposalID)
	return nil
}

// validateRoadmapIdeaProposal is the create door's entry (TD3): the same
// registry entry the commit door re-runs, reached from the one call site
// this package keeps for it.
func validateRoadmapIdeaProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindRoadmapIdea), payload)
}

// roadmapIdeaFacts is the shared read behind all three seams: the decoded
// payload, whether the component root EXISTS in the registry, and whether
// an IDENTICAL OPEN item (same root, same title, not done, not cancelled)
// already stands on the board. The unknown root is refused here, in the
// store's own not-found turned into a sentence naming roadmap.board — the
// person's door that shows what roots carry items. Terminal twins do NOT
// count as duplicates: D7 keeps done and cancelled as visible history, and
// re-adding what was deliberately cancelled is a decision, not a no-op.
//
// One read, one order, so the doors cannot disagree about what the board
// said.
func roadmapIdeaFacts(payload []byte) (p desk.RoadmapIdeaProposal, rootExists, dupOpen bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, false, false, fmt.Errorf("refused: roadmap_idea payload will not decode: %v", err)
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return p, false, false, err
	}
	defer st.Close()
	if _, cerr := st.ComponentForRoot(p.ComponentRoot); cerr != nil {
		if errors.Is(cerr, store.ErrNotFound) {
			return p, false, false, fmt.Errorf("refused: no component is rooted at %q — roadmap.board shows what roots exist (S50 T5)", p.ComponentRoot)
		}
		return p, false, false, cerr
	}
	items, lerr := roadmap.New(st).List(p.ComponentRoot, roadmap.ListFilter{})
	if lerr != nil {
		return p, true, false, lerr
	}
	for _, it := range items {
		if it.Status == roadmap.StatusDone || it.Status == roadmap.StatusCancelled {
			continue
		}
		if it.Title == p.Title {
			return p, true, true, nil
		}
	}
	return p, true, false, nil
}
