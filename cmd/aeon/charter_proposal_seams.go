// charter_proposal_seams.go — S61 T5 (GI4): the charter halves of the
// charter_save proposal kind, as ONE seam map that BOTH doors run.
//
// The desk (internal/desk/charter_save.go) holds the pure shape: {rootId,
// body, reason} — no actor, no source, no note anywhere. What it may not
// read, the seam reads: charter.Validate over the decoded body (the ONE
// charter validator — the person's write door and these proposal doors
// answer from the same lines, parity by construction) and the component
// registry (does the root exist, and is it a root — the store's own two
// refusal sentences, quoted verbatim so create and commit read alike). The
// seam's Validate REPLACES the desk's shape half with the full
// desk-then-charter function — the merge rule in desk.New — so what create
// refuses, commit refuses, and what create accepts is never a surprise a
// person commits blind (S35 TD3).
//
// Apply is the charter's own Save — never a hand-rolled INSERT — with the
// COMMITTING PERSON as the revision actor, source 'proposal' and the
// proposal's id as source_ref (GI4). The proposer stays where it belongs,
// in the proposal's created_by_*; a context missing the committer or the id
// is broken wiring, refused fail-closed (S49 T6's discipline, restated at
// GI4's table). No note rides the save: the charter package's law keeps a
// proposal's reason on the proposal row (write.go's note-source rule).
// AlreadyApplied answers "the CURRENT revision IS this exact body" (TD4):
// the crash-retry completes without churning append-only history, and a
// mismatch answers false so validate+apply still run and the real writer
// refuses a genuine conflict loudly.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

// seamValidateCharterSave is the full Validate both doors run: the desk's
// shape half first (its sentences travel verbatim), then the charter's own
// body validation, then the two facts a shape cannot carry.
func seamValidateCharterSave(ctx context.Context, payload []byte) error {
	if err := desk.ValidateCharterSave(ctx, payload); err != nil {
		return err
	}
	p, body, err := charterSaveFacts(payload)
	if err != nil {
		return err
	}
	if r := charter.Validate(body, charter.Vocabulary(nil)); r != nil {
		return errors.New(r.Sentence)
	}
	return charterRootFacts(p.RootID)
}

// charterSaveFacts decodes the (already shape-validated) payload into its
// typed halves once, for the seam's own reads. The charter body decodes
// through the charter package's own type — one definition of the shape.
func charterSaveFacts(payload []byte) (desk.CharterSaveProposal, charter.Body, error) {
	var p desk.CharterSaveProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, charter.Body{}, fmt.Errorf("refused: the stored charter_save payload cannot be decoded: %v", err)
	}
	var body charter.Body
	if err := json.Unmarshal(p.Body, &body); err != nil {
		return p, body, fmt.Errorf("refused: the charter_save body is not a charter: %v", err)
	}
	return p, body, nil
}

// charterRootFacts tastes the two facts the store's AppendCharterRevision
// law enforces at write time (store/charter.go), quoting ITS sentences
// verbatim so the create door's refusal reads exactly like the commit
// door's writer would answer: the root exists, and it is a root.
func charterRootFacts(rootID string) error {
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	comp, err := st.ReadComponent(rootID)
	if err != nil {
		return fmt.Errorf("refused: component %q does not exist — a charter belongs to a project that exists", rootID)
	}
	if comp.ParentID != "" {
		pname := comp.ParentID
		if parent, perr := st.ReadComponent(comp.ParentID); perr == nil {
			pname = parent.Name
		}
		return fmt.Errorf("refused: %s is a component of project %s — the charter lives on the project", rootID, pname)
	}
	return nil
}

// seamAlreadyAppliedCharterSave answers TD4's question with the charter's
// own no-op law: the CURRENT revision's body canonicalizes to the very
// bytes the proposal carries, so the effect has already landed and the
// decide flow marks the row committed without churning history. A root
// without a charter is honestly NOT applied; a store failure is an error,
// never a guess.
func seamAlreadyAppliedCharterSave(ctx context.Context, payload []byte) (bool, error) {
	p, body, err := charterSaveFacts(payload)
	if err != nil {
		return false, nil // the commit's Validate will name what create already refused
	}
	canonical, err := charter.Canonical(body)
	if err != nil {
		return false, nil
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	svc := &charter.Service{Store: st}
	current, err := svc.Get(ctx, p.RootID)
	if err != nil {
		return false, nil // no charter yet at this root: not applied
	}
	currentCanonical, err := charter.Canonical(current.Body)
	if err != nil {
		return false, err
	}
	return string(canonical) == string(currentCanonical), nil
}

// seamApplyCharterSave writes the revision — the charter's own Save, so the
// canonical bytes, the summary template, the revision number and the
// append-only law are exactly what a person's charter.save writes. The
// committer comes from the commit door's context and the proposal id rides
// as source_ref; a context missing either is broken wiring, not a shortcut.
func seamApplyCharterSave(ctx context.Context, w io.Writer, payload []byte) error {
	a, ok := desk.DeciderFrom(ctx)
	if !ok || a.Kind != "user" || a.ID == "" {
		return errors.New("refused: charter_save's apply needs the committer's identity from the commit door — a context without it is not a commit, and a revision with a guessed actor is the audit gap (S61 T5, GI4)")
	}
	proposalID, ok := desk.ProposalIDFrom(ctx)
	if !ok || proposalID <= 0 {
		return errors.New("refused: charter_save's apply needs the proposal's id from the commit door — the revision traces back through it, and an apply that lost it is broken wiring, not a shortcut (S61 T5)")
	}
	p, body, err := charterSaveFacts(payload)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Committing charter_save: root %s (proposed by ask %d, committed by user %s)\n", p.RootID, proposalID, a.ID)
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := &charter.Service{Store: st}
	rev, ref, err := svc.Save(ctx, store.Actor{Kind: "user", ID: a.ID}, p.RootID, body, "", "proposal", fmt.Sprintf("%d", proposalID))
	if err != nil {
		return err
	}
	if ref != nil {
		return errors.New(ref.Sentence)
	}
	fmt.Fprintf(w, "Committed: charter r%d for %s stands — actor user:%s, source proposal, source_ref %d, in the append-only history with ask %d recorded on the proposal\n",
		rev.Revision, p.RootID, a.ID, proposalID, proposalID)
	return nil
}

// validateCharterSaveProposal is the create door's entry (TD3): the same
// registry entry the commit door re-runs, reached from the one call site
// this package keeps for it.
func validateCharterSaveProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindCharterSave), payload)
}
