package main

// environment_proposal_seams.go — S59 T7 (EV16, EV3, TD3): the three seams the
// `environment_restrict` kind runs through, registered in deskSeams() so the
// create door and the commit door call literally the same functions (TD3), and
// in proposalValidators so `proposal.create` validates before it stores one
// byte.
//
// What the kind IS: a person-shaped ask to make one environment's spec
// SMALLER — drop a passed-through name, add a cap, cut egress. The whole
// decision lives in one line of this file, `execenv.Restricts` coming back
// EMPTY: every way the proposed spec is looser than the row as it stands —
// one more name passed through, one cap raised, egress switched on, the home
// moved onto the real one, the backend moved at all — is a widening, and a
// widening never becomes a row. The refusal names each axis it found, verbatim
// (TD3's discipline: the same sentence at both doors, so a person reading a
// rejected proposal reads the truth the validator used).
//
// What this file does NOT have: a `backend` anywhere in the payload's shape
// (EV16 — a backend's strength moves by a person through environment.update,
// and there is no environment_widen/create/bind/secretref_* kind to file it
// through), a value or a secret (EV7 — a spec names references), a second
// write, or an actor taken from the payload. The actor is the COMMITTER from
// the commit door (desk.DeciderFrom) and the row's audit line carries the
// PROPOSAL's id beside it (desk.ProposalIDFrom) — two identities, both
// recorded, neither guessed (S56 T8's shape). Fail-closed on both channels:
// an apply with no committer is not a commit, and an apply with no proposal id
// has lost the fact the audit exists to keep.
//
// The baseline is the payload's `baseRevision`, carried into the store's
// compare-and-set: an environment someone edited since the ask is a
// `revision-stale` refusal, not a silent clobber. The desk's AlreadyApplied
// answers on the CANONICAL bytes, so a spec that differs only in key order or
// whitespace is "already applied" and commits clean instead of burning a
// revision nobody changed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
)

// seamValidateEnvironmentRestrict is the kind's validator, at BOTH doors. The
// order is the order the refusals read best in: shape, then the row, then the
// one rule that matters.
func seamValidateEnvironmentRestrict(ctx context.Context, payload []byte) error {
	// Shape first, through the desk's own strict parse (unknown fields
	// refused, the caps applied) — the same function the registry runs when
	// no seam replaces it, reached here so the store half sits behind it.
	if err := desk.ValidateEnvironmentRestrict(ctx, payload); err != nil {
		return err
	}
	_, row, oldSpec, newSpec, err := environmentRestrictFacts(payload)
	if err != nil {
		return err
	}
	// Already-applied is not a failure here (TD4's gate answers that
	// separately, and validate must stay safe to call after it): an equal
	// spec simply has nothing to widen.
	widenings := execenv.Restricts(oldSpec, newSpec, row.Backend, row.Backend)
	if len(widenings) > 0 {
		axes := make([]string, 0, len(widenings))
		for _, w := range widenings {
			axes = append(axes, w.Sentence())
		}
		return fmt.Errorf("refused: environment_restrict %s would WIDEN what the loop runs with — %d loosening(s): %s. A proposal of this kind tightens, and every loosening is refused at create and again at commit (EV16). A backend change is not a proposal shape at all: environment.update, by a person, is that door.",
			row.Slug, len(widenings), strings.Join(axes, "; "))
	}
	// The proposed spec must be one the WRITE door would accept under the
	// row's own backend — same function, same shape of opts the daemon's door
	// runs, so a spec that cannot be written cannot be proposed either (TD3).
	// The refusals travel verbatim, joined as the plane joins them (§4d's
	// one-formatter rule carried). A `vm` environment is refused here because
	// no vm driver is built (EV3's registry is the truth, not a flag).
	if problems := execenv.Validate(newSpec, row.Backend, execenv.ValidateOpts{}); len(problems) > 0 {
		sentences := make([]string, 0, len(problems))
		for i := range problems {
			sentences = append(sentences, problems[i].What)
		}
		return fmt.Errorf("refused: environment_restrict's proposed spec is not valid under the %s backend — %d problem(s): %s",
			row.Backend, len(problems), strings.Join(sentences, " | "))
	}
	return nil
}

// seamAlreadyAppliedEnvironmentRestrict answers TD4 for the kind: the row
// ALREADY says exactly this. Compared on the canonical bytes, so the answer
// means "this effect has landed" rather than "these strings match" — and a
// mismatch answers false, letting validate and apply run and refuse loudly
// rather than this gate papering over a real conflict.
func seamAlreadyAppliedEnvironmentRestrict(ctx context.Context, payload []byte) (bool, error) {
	if err := desk.ValidateEnvironmentRestrict(ctx, payload); err != nil {
		return false, nil // a broken payload is not "already applied"; validate refuses it
	}
	_, _, oldSpec, newSpec, err := environmentRestrictFacts(payload)
	if err != nil {
		return false, nil // an unreadable row is not "already applied" either — validate refuses it
	}
	return string(execenv.Canonical(newSpec)) == string(execenv.Canonical(oldSpec)), nil
}

// seamApplyEnvironmentRestrict performs the committed tightening through the
// store's own CAS door — the COMMITTER as actor (never this payload's author,
// whose identity lives in the row's proposed_by columns) and the PROPOSAL'S id
// in the change's audit summary, so the environment's history says which
// decision produced it and who turned it into a fact.
//
// The spec rides the canonical bytes: the validator has already proved it
// parses and validates, and a row is written in one form rather than whatever
// spelling the proposer typed. `ExpectedRevision` is the payload's baseline —
// the store refuses `revision-stale` rather than let a committed ask clobber an
// edit that landed after the ask was filed.
func seamApplyEnvironmentRestrict(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return errors.New("refused: environment_restrict's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S59 T7)")
	}
	id, ok := desk.ProposalIDFrom(ctx)
	if !ok {
		return errors.New("refused: environment_restrict's apply needs the proposal's id from the commit door — the environment's audit line names the proposal that produced it, and an update that cannot say so is the gap the audit exists to catch (S59 T7)")
	}
	p, _, _, newSpec, err := environmentRestrictFacts(payload)
	if err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()

	note := fmt.Sprintf("tightened by committed proposal #%d · %s", id, p.Reason)
	_, ref, err := st.UpdateEnvironment(
		store.Actor{Kind: dec.Kind, ID: dec.ID},
		store.UpdateEnvironment{
			ID:               p.EnvironmentID,
			Spec:             string(execenv.Canonical(newSpec)),
			ExpectedRevision: p.BaseRevision,
			Note:             note,
		},
	)
	if err != nil {
		return err
	}
	if ref != nil {
		return errors.New(ref.Sentence)
	}
	return nil
}

// validateEnvironmentRestrictProposal is the create door's entry (TD3): the
// same registry entry the commit door re-runs, reached from the one call site
// this package keeps for it.
func validateEnvironmentRestrictProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindEnvironmentRestrict), payload)
}

// environmentRestrictFacts is the shared read behind all three seams: the
// decoded payload, the row it asks about (unknown and archived refused here,
// with the store's own ErrNotFound turned into a sentence naming environment_list),
// and both specs parsed. One read, one order, so the doors cannot disagree
// about what the row said.
func environmentRestrictFacts(payload []byte) (desk.EnvironmentRestrictProposal, store.Environment, execenv.Spec, execenv.Spec, error) {
	var p desk.EnvironmentRestrictProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return p, store.Environment{}, execenv.Spec{}, execenv.Spec{}, err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return p, store.Environment{}, execenv.Spec{}, execenv.Spec{}, err
	}
	defer st.Close()

	row, err := st.GetEnvironment(p.EnvironmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return p, row, execenv.Spec{}, execenv.Spec{}, fmt.Errorf("refused: %s is not an environment (id or slug) — environment_list shows what exists (S59 T7)", p.EnvironmentID)
		}
		return p, row, execenv.Spec{}, execenv.Spec{}, err
	}
	if row.Archived() {
		return p, row, execenv.Spec{}, execenv.Spec{}, fmt.Errorf("refused: %s is archived — an archived row is history, not a draft, and environment.restore is the person's door (S59 T7)", row.Slug)
	}
	oldSpec, err := execenv.ParseSpec([]byte(row.Spec))
	if err != nil {
		return p, row, execenv.Spec{}, execenv.Spec{}, fmt.Errorf("refused: the stored spec of %s does not parse (%v) — a proposal is written against a readable row, and a row that cannot be read is not a baseline", row.Slug, err)
	}
	newSpec, err := execenv.ParseSpec([]byte(p.Spec))
	if err != nil {
		return p, row, oldSpec, execenv.Spec{}, fmt.Errorf("refused: the proposed spec does not parse (%v) — it must be the FULL spec, JSON or YAML, as environment.show would print it", err)
	}
	return p, row, oldSpec, newSpec, nil
}
