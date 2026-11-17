package main

// gate_proposal_seams.go — S57 T8: the two RESTRICTIVE gate proposal kinds'
// store halves, arriving as desk.Seams (the autonomy pattern, S56 T8/AL11).
//
// The desk (internal/desk/verify_kinds.go) holds the pure halves — the
// payload shapes, the op:"add" shape proof, the reason discipline — and
// this file supplies what the desk may not import: the component exists
// and is not archived, the gate name is free on it, the rule's fields are
// in the closed sets, and the proposed rule TIGHTENS the current one
// field by field — "current" being the component's EFFECTIVE rule, and
// the baseline for a component with no row (T8's wording; the store's
// EffectiveMergeRule already answers that way). A loosening is refused at
// CREATE, each loosened field named (S35 TD3: the commit door re-runs the
// same function, so nothing inert-and-invalid is persisted, and a
// proposal that could never apply is never one that gets a row).
//
// There is deliberately no third gate kind: a proposal that loosens a
// gate or a rule is not a shape this package implements either.
//
// Both doors run these same seam values: the CLI's `aeon proposal commit`
// (the one map, deskSeams, registered once by desk.RegisterSeams in
// proposal_decide.go) and the daemon's proposal.commit — one definition,
// two doors, no daemon-side table to drift from (PM4). The committer is
// the decider desk.Commit installs on the context immediately before
// Apply (the only code that sees it); a context without it is refused,
// fail-closed, and a write with a guessed actor is the audit gap.
//
// The sentences re-stated below are the store's, byte for byte: a
// create-time refusal is the create door's sentence, and the store re-runs
// the same checks at apply (AddVerifyCommand / SetMergeRule), so a drift
// is refused there with the store's own words — the re-declaration the
// desk's own header endorses for exactly this reason.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/store"
)

// S57 T8 (2026-11-02): the two RESTRICTIVE gate kinds' validators — the
// autonomy one-line shape. The rule lives in the desk's registry, which
// for these kinds runs the full desk-then-store Validate seam, so the
// commit door re-runs exactly what the create door checked (TD3), and
// the store half opens the CLI's justified second handle like policy and
// proposal already do.
func validateVerifyCommandProposal(payload []byte) error {
	return validateKindPayload("verify_command", payload)
}

func validateMergeRuleProposal(payload []byte) error {
	return validateKindPayload("merge_rule", payload)
}

// — verify_command —

// seamValidateVerifyCommand is verify_command's full Validate: the desk
// half (the shape — op:"add" only, the slug, the command's bounds, the
// requirement's closed set, the timeout's range, the reason — THE
// function the create door runs, TD3), then the store half: the
// component exists and is not archived, and the name is free on it among
// the LIVE gates (an archived twin does not block the add — the store's
// own door says the same at apply, so the create door and the commit
// door cannot disagree about what blocks).
func seamValidateVerifyCommand(_ context.Context, payload []byte) error {
	if err := desk.ValidateVerifyCommand(context.Background(), payload); err != nil {
		return err
	}
	var p desk.VerifyCommandProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	c, err := st.ReadComponent(p.ComponentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("refused: %s is not a component — a gate needs a node it can sit on", p.ComponentID)
		}
		return err
	}
	if c.ArchivedAt != "" {
		return fmt.Errorf("refused: %s is archived — its gates cannot be written", p.ComponentID)
	}
	gates, err := st.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: p.ComponentID})
	if err != nil {
		return err
	}
	for _, g := range gates {
		if g.Name == p.Name {
			return fmt.Errorf("refused: %s already has a live gate named %q (archive it first)", p.ComponentID, p.Name)
		}
	}
	return nil
}

// seamAlreadyAppliedVerifyCommand is TD4's answer for an add: a LIVE gate
// with the proposed name already stands on the component — the effect
// landed, so the commit flips the row without re-applying (a crash
// between the add and the flip completes instead of re-adding; the
// store's own name check is the backstop for the race). An archived twin
// answers false — it is not the standing gate — and a component the list
// cannot read answers false so validate+apply run and the store's own
// doors re-decide the real write.
func seamAlreadyAppliedVerifyCommand(_ context.Context, payload []byte) (bool, error) {
	var p desk.VerifyCommandProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	gates, err := st.ListVerifyCommands(store.VerifyCommandFilter{ComponentID: p.ComponentID})
	if err != nil {
		return false, err
	}
	for _, g := range gates {
		if g.Name == p.Name {
			return true, nil
		}
	}
	return false, nil
}

// seamApplyVerifyCommand performs the committed add through the store's
// own door with the COMMITTER as actor — the decider desk.Commit
// installed on the context immediately before Apply (the only code that
// sees it). The store stamps origin "user" itself: the gate is a
// person's declaration, whoever's loop proposed it, and the created row
// records the decision, not the proposal. Fail-closed on a context
// without the decider, as the autonomy halves.
func seamApplyVerifyCommand(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return fmt.Errorf("refused: verify_command's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S57 T8)")
	}
	var p desk.VerifyCommandProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	w, err := st.AddVerifyCommand(store.Actor{Kind: dec.Kind, ID: dec.ID}, store.NewVerifyCommand{
		ComponentID: p.ComponentID,
		Name:        p.Name,
		Command:     p.Command,
		Requirement: p.Requirement,
		TimeoutS:    p.TimeoutS,
	})
	if err != nil {
		return err
	}
	if !w.OK() {
		return fmt.Errorf("%s", w.Reason)
	}
	return nil
}

// — merge_rule —

// decodeMergeRuleProposal decodes the payload twice, strictly: the
// payload is {rule, reason} and nothing else (the desk half said so),
// and the rule is the store's OWN shape — the store's SetMergeRule
// shape, every field by its Go name (T8's wording; the rule speaks the
// store's vocabulary, and the store overwrites the provenance fields —
// Revision, Source, UpdatedAt — at write, so carrying them changes
// nothing). A key nobody validates is a parse-level refusal, so a
// loosening cannot hide in a field the strict decode does not name.
func decodeMergeRuleProposal(payload []byte) (desk.MergeRuleProposal, mergerule.Rule, error) {
	var p desk.MergeRuleProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, mergerule.Rule{}, err
	}
	var r mergerule.Rule
	dec := json.NewDecoder(bytes.NewReader(p.Rule))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return p, mergerule.Rule{}, fmt.Errorf("refused: the rule is the store's own shape — {\"ComponentID\": \"<node>\", \"Enabled\": <bool>, and every other rule field by its Go name} — and %v is not one", err)
	}
	return p, r, nil
}

// ruleFieldRefusals re-declares SetMergeRule's five closed sets, the
// store's words byte for byte (the re-declaration the desk's header
// endorses): the values come from the mergerule package's own constants,
// so only the sentences can drift, and the store re-runs the same
// switches at apply with the same sentences — a drift is refused there
// before SQL, and TestProposalKindsAreRestrictiveOnly pins the words.
func ruleFieldRefusals(r mergerule.Rule) error {
	switch {
	case r.RequireDoD != mergerule.RequireDoDNone && r.RequireDoD != mergerule.RequireDoDAllMet:
		return fmt.Errorf("refused: require_dod is %q; the closed set is none|all_met", r.RequireDoD)
	case r.ChecksPolicy != mergerule.ChecksReported && r.ChecksPolicy != mergerule.ChecksRequired:
		return fmt.Errorf("refused: checks_policy is %q; the closed set is reported_green|required_only", r.ChecksPolicy)
	case r.NoChecks != mergerule.NoChecksAllow && r.NoChecks != mergerule.NoChecksRefuse:
		return fmt.Errorf("refused: no_checks is %q; the closed set is allow|refuse", r.NoChecks)
	case r.ReviewMode != mergerule.ReviewModeForge && r.ReviewMode != mergerule.ReviewModeAeon &&
		r.ReviewMode != mergerule.ReviewModeEither && r.ReviewMode != mergerule.ReviewModeBoth:
		return fmt.Errorf("refused: review_mode is %q; the closed set is forge|aeon|either|both", r.ReviewMode)
	case r.Method != mergerule.MethodSquash && r.Method != mergerule.MethodMerge && r.Method != mergerule.MethodRebase:
		return fmt.Errorf("refused: method is %q; the closed set is squash|merge|rebase", r.Method)
	}
	return nil
}

// seamValidateMergeRule is merge_rule's full Validate: the desk half
// (the shape — the payload is the FULL proposed rule plus the reason,
// nothing else — THE function the create door runs, TD3), then the store
// half: the rule decodes strictly into the store's own shape; the
// component exists and is not archived; the rule's fields are in the
// closed sets (the store re-runs them at apply); and the rule TIGHTENS
// the current one field by field — the component's EFFECTIVE rule, the
// baseline for a component with no row — refusing the loosening and
// naming each loosened field. The one incomparable pair (a forge↔aeon
// review-mode switch) is refused too, both directions: Tightens answers
// not-a-tightening with an EMPTY loosened list for exactly that case,
// and a proposal may not authorise a switch either way.
func seamValidateMergeRule(_ context.Context, payload []byte) error {
	if err := desk.ValidateMergeRule(context.Background(), payload); err != nil {
		return err
	}
	_, r, err := decodeMergeRuleProposal(payload)
	if err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	c, err := st.ReadComponent(r.ComponentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("refused: %s is not a component", r.ComponentID)
		}
		return err
	}
	if c.ArchivedAt != "" {
		return fmt.Errorf("refused: %s is archived — its rule is history", r.ComponentID)
	}
	if err := ruleFieldRefusals(r); err != nil {
		return err
	}
	current, _, err := st.EffectiveMergeRule(r.ComponentID)
	if err != nil {
		return err
	}
	if tightened, loosened := mergerule.Tightens(current, r); !tightened {
		if len(loosened) == 0 {
			return fmt.Errorf("refused: the proposed rule switches the review mode between forge and aeon — the one incomparable pair, and a proposal may not authorise a switch either way (S57 T8)")
		}
		return fmt.Errorf("refused: the proposed rule loosens %s — a merge_rule proposal may only tighten, and a loosening is a person's act (S57 T8)", strings.Join(loosened, ", "))
	}
	return nil
}

// seamAlreadyAppliedMergeRule is TD4's answer for a rule: the component's
// OWN row already IS the proposed rule in behaviour — the effect landed,
// so the commit flips without re-applying (the store's own no-op is the
// backstop for the race: an identical set is refused a new revision).
// "Is" is Tightens in BOTH directions — neither tightens the other means
// the behaviour is the same — which reads the two list fields by set
// semantics (order never tightens) and leaves the provenance fields
// (ComponentID, Revision, Source, UpdatedAt) out of the comparison: the
// stamp is not the effect. A missing row answers false, and a row whose
// behaviour differs answers false so validate+apply run and the store's
// own doors re-decide the real write.
func seamAlreadyAppliedMergeRule(_ context.Context, payload []byte) (bool, error) {
	_, r, err := decodeMergeRuleProposal(payload)
	if err != nil {
		return false, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	cur, err := st.GetMergeRule(r.ComponentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	sameWay, _ := mergerule.Tightens(cur, r)
	otherWay, _ := mergerule.Tightens(r, cur)
	return sameWay && otherWay, nil
}

// seamApplyMergeRule performs the committed rule through the store's own
// door with the COMMITTER as actor — the decider desk.Commit installed
// on the context immediately before Apply (the only code that sees it).
// The rule row and the change event record the person who decided, never
// the agent who proposed: the author's identity rides on the proposal
// row, and the rule's provenance is the decision. The store re-runs its
// own doors (actor, component, closed sets, the no-op) — the seam adds
// the decider and nothing else. Fail-closed, as the autonomy halves.
func seamApplyMergeRule(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return fmt.Errorf("refused: merge_rule's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S57 T8)")
	}
	_, r, err := decodeMergeRuleProposal(payload)
	if err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	w, err := st.SetMergeRule(store.Actor{Kind: dec.Kind, ID: dec.ID}, r)
	if err != nil {
		return err
	}
	if !w.OK() {
		return fmt.Errorf("%s", w.Reason)
	}
	return nil
}
