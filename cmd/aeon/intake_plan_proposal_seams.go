// intake_plan_proposal_seams.go — S61 T5 (GI7): the charter and store
// halves of the intake_plan proposal kind, as a Validate seam BOTH doors
// run — and ONLY Validate.
//
// The desk (internal/desk/intake_plan.go) holds the closed GI7 schema, the
// caps, the reference integrity and the role law. What it may not read, the
// seam reads: charter.Validate over the plan's charter body (the ONE charter
// validator — the same lines the person's charter doors answer from), and
// the template registry (does each component's template exist at all — the
// plan names WHAT to scaffold from, and a plan naming no such template is a
// plan that cannot be carried out). The seam's Validate REPLACES the desk's
// shape half with the full desk-then-charter-then-store function (the merge
// rule in desk.New), so create and commit refuse the same malformed plan in
// the same words (S35 TD3), and a plan a person commits blind is never one
// create would have taken.
//
// What this seam deliberately does NOT supply: AlreadyApplied and Apply. The
// desk's built-ins are the kind's DESIGN, not a missing wire — an
// intake_plan is never "already applied" through the generic door (its
// real apply is intake.commit's ledger, T6), and its generic apply is the
// fixed sentence naming intake.commit (GI7/OD4). Registering a seam half
// here would REPLACE that refusal, which is precisely what must never
// happen: the payload carries no target path and no publish choice to apply
// with, and inventing them at the desk is what the dedicated door exists to
// keep impossible.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
)

// seamValidateIntakePlan is the full Validate both doors run: the desk's
// shape half first (its sentences travel verbatim), then the charter body
// the plan will write, then the templates the plan will scaffold from.
func seamValidateIntakePlan(ctx context.Context, payload []byte) error {
	if err := desk.ValidateIntakePlan(ctx, payload); err != nil {
		return err
	}
	var p desk.IntakePlanProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("refused: the stored intake_plan payload cannot be decoded: %v", err)
	}
	var body charter.Body
	if err := json.Unmarshal(p.Charter, &body); err != nil {
		return fmt.Errorf("refused: the intake_plan charter is not a charter: %v", err)
	}
	if r := charter.Validate(body, charter.Vocabulary(nil)); r != nil {
		return errors.New(r.Sentence)
	}
	return intakePlanTemplatesExist(p)
}

// intakePlanTemplatesExist reads the template registry once and names the
// FIRST unknown slug — a plan that names several missing templates is
// refused one sentence at a time, same as the desk's rule walk: fix the
// named thing, come back, hear the next.
func intakePlanTemplatesExist(p desk.IntakePlanProposal) error {
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	seen := map[string]bool{}
	for _, c := range p.Components {
		if seen[c.Template] {
			continue
		}
		seen[c.Template] = true
		if _, err := st.GetTemplate(c.Template); err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				return fmt.Errorf("refused: component %q names template %q and no such scaffold template is installed — the plan must name something to scaffold from", c.Slug, c.Template)
			}
			return err
		}
	}
	return nil
}

// validateIntakePlanProposal is the create door's entry (TD3): the same
// registry entry the commit door re-runs, reached from the one call site
// this package keeps for it.
func validateIntakePlanProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindIntakePlan), payload)
}
