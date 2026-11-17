package main

// blueprint_proposal_seams.go — S48 T9: the blueprint_save seam halves
// (the ONE map in proposal_decide.go), the create door's per-kind arm, and
// the commit effect. The desk keeps the pure shape (internal/desk/
// blueprint_kinds.go); the store halves — the write path's own Analyze with
// the write path's own options, the id/name cross-check, the createOnly
// collision, the content-hash AlreadyApplied, and the Apply through T6's
// ONE save — live here, where the daemon door and the CLI door read the
// same values (PM4: one seam map, both doors).
//
// TD3, carried from S35: a payload refused by proposal.create is refused by
// proposal.commit for the SAME reason and in the SAME words — both doors
// run seamValidateBlueprintSave through the same registry. The report rides
// VERBATIM in the refusal (the save door's own sentence, no paraphrase):
// an inert nonsense proposal never becomes a row, and one that was valid
// at create and rotted since (an agent deleted meanwhile) is refused at
// commit with the report, applied_error set, the row staying proposed
// (S35 TD4).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/protocol"
)

// seamValidateBlueprintSave is blueprint_save's full Validate — the desk
// shape half, then the write path's judgment: the UNCHANGED blueprint.
// Analyze with the SAME options blueprintValidationOptions builds on the
// daemon side (mode "standard", registries read live), the id/name
// cross-check (the same sentence the save door gives), and the createOnly
// collision (name free — a CONFLICT read from the store at the door that
// decides). Identical bytes are NOT refused here: that is AlreadyApplied's
// answer at commit (a crash-retry is not a conflict).
func seamValidateBlueprintSave(_ context.Context, payload []byte) error {
	if err := desk.ValidateBlueprintSave(context.Background(), payload); err != nil {
		return err
	}
	var p desk.BlueprintSaveProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	if !blueprint.ValidBlueprintName(p.Name) {
		return fmt.Errorf("refused: blueprint name %q is outside the grammar (lowercase words joined by single hyphens, ≤ 64)", p.Name)
	}
	report := blueprint.Analyze([]byte(p.Content), blueprint.ValidationOptions{
		Mode:       "standard",
		Agents:     handler.Names(),
		Resolvers:  handler.Resolvers(),
		Actions:    action.Names(),
		AgentTools: handler.ToolsFor,
	})
	if !report.OK(false) {
		raw, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("refused: the blueprint candidate does not validate and its report could not be rendered: %v", err)
		}
		return fmt.Errorf("%s", string(raw))
	}
	if report.Blueprint != p.Name {
		return fmt.Errorf("refused: source id %q does not match selected blueprint %q", report.Blueprint, p.Name)
	}
	if p.CreateOnly {
		st, err := openStoreDirectly(false)
		if err != nil {
			return err
		}
		defer st.Close()
		if _, taken, err := st.BlueprintGet(context.Background(), p.Name); err != nil {
			return err
		} else if taken {
			return fmt.Errorf("refused: create refused: blueprint %q already exists — createOnly never overwrites; save without createOnly edits it", p.Name)
		}
	}
	return nil
}

// seamAlreadyAppliedBlueprintSave is TD4's answer: the stored row's
// content_hash equals the payload's — the effect landed, so a commit after
// a lost flip COMPLETES (no second revision for bytes already on the row).
// An unknown name answers false, not an error: the validator refuses it
// loudly, and a gate that guessed would paper over that refusal.
func seamAlreadyAppliedBlueprintSave(_ context.Context, payload []byte) (bool, error) {
	var p desk.BlueprintSaveProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return false, err
	}
	sum := sha256.Sum256([]byte(p.Content))
	hash := hex.EncodeToString(sum[:])
	st, err := openStoreDirectly(false)
	if err != nil {
		return false, err
	}
	defer st.Close()
	row, found, err := st.BlueprintGet(context.Background(), p.Name)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return row.ContentHash == hash, nil
}

// seamApplyBlueprintSave performs the committed save through S48 T6's ONE
// write path — daemon.BlueprintSave, THE same function the socket door
// runs — with the COMMITTING PERSON as the revision's actor (fail-closed:
// a context without the decider is not a commit, and a write with a
// guessed actor is the audit gap the decider exists to close; the
// proposer stays in the row's created_by_*, never on the revision).
// The save door's refusals (the report, the collision, the id mismatch)
// return as the commit's applied_error: the row stays proposed and a
// person sees the named reason.
func seamApplyBlueprintSave(ctx context.Context, _ io.Writer, payload []byte) error {
	dec, ok := desk.DeciderFrom(ctx)
	if !ok {
		return fmt.Errorf("refused: blueprint_save's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S48 T9)")
	}
	var p desk.BlueprintSaveProposal
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	st, err := openStoreDirectly(false)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := daemon.BlueprintSave(ctx, st, protocol.Actor{Kind: dec.Kind, ID: dec.ID}, protocol.BlueprintSaveParams{
		Name:       p.Name,
		Content:    p.Content,
		CreateOnly: p.CreateOnly,
		Note:       p.Note,
	}); err != nil {
		var perr *protocol.Error
		if errors.As(err, &perr) {
			return fmt.Errorf("%s", perr.Message+" — "+perr.Data.What)
		}
		return err
	}
	return nil
}

// validateBlueprintSaveProposal is the create door's arm (the map in
// proposal_validators.go): the desk registry's full Validate — the same
// function the commit door re-runs (TD3).
func validateBlueprintSaveProposal(payload []byte) error {
	return validateKindPayload(string(desk.KindBlueprintSave), payload)
}
