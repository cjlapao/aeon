// blueprint_kinds.go — S48 T9: the twelfth proposal kind, blueprint_save,
// as a registry entry in this package.
//
// The desk keeps only the pure half, exactly the verify_kinds.go shape: the
// payload SHAPE (a strict decode — a smuggled "mode", "origin" or "actor"
// key is a parse refusal, so an agent cannot choose the validation mode nor
// stamp itself an actor), the non-blank name, the 1 MiB source bound and
// the 8 KiB envelope re-declared. The store halves — the write path's own
// Analyze with the write path's own options, the id/name cross-check, the
// createOnly collision, and the APPLY through S48 T6's one write path —
// arrive as `Seams` from `cmd/aeon`: the desk's dependency rule is stdlib +
// policy + paths, and importing internal/blueprint (the validator) or
// internal/daemon (the writer) would drag both engines into the decision
// code. The seam's Validate REPLACES this file's shape half with the full
// desk-then-store function (the merge rule in desk.New), so the create door
// and the commit door run one function (TD3) and the effect is performed
// with the COMMITTING PERSON as the revision's actor — the proposer stays
// where it belongs, in the row's created_by_* (the S56 T8 rule, carried).
//
// The kind is not restrictive in the AL11 sense (it can widen what an agent
// family later runs — a blueprint names agents and tools); what keeps it
// honest is that it is INERT: nothing lands until a person commits, the
// commit runs the save door's own refusals verbatim, and the actor on the
// row is the person, not the proposer (BP9, S35 TD1/TD3/TD4).
package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// KindBlueprintSave is S48 T9's kind, twelfth in the store's order
// (migration 20261103004700; the store mirror is store.ProposalKinds).
const KindBlueprintSave Kind = "blueprint_save"

// The payload bounds, re-declared (the importer pin — see the file
// header): the blueprint source's 1 MiB (the save door's own bound,
// duplicated HERE so a payload the save door would refuse never becomes an
// inert row), and the 8 KiB envelope for the JSON around it.
const (
	MaxBlueprintSourceBytes = 1 << 20
	blueprintEnvelopeBytes  = 8 << 10
	maxBlueprintNameBytes   = 64 // the name grammar's own ceiling (lowercase words joined by single hyphens, ≤ 64)
)

// BlueprintSaveProposal is blueprint_save's payload — the whole proposed
// source, never a patch (a patch would let a change hide in the lines the
// patch does not name; a blueprint is decided whole). createOnly says
// "this is a CREATE": a create against an existing name is refused at
// commit with the save door's collision sentence (and at create, by the
// same function). note is the revision note a person may attach.
type BlueprintSaveProposal struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	CreateOnly bool   `json:"createOnly"`
	Note       string `json:"note"`
}

// validateBlueprintSavePayload is the desk's pure half of Validate (the
// shape). The store half — Analyze with the write path's options, the
// id/name cross-check, the createOnly collision — is the seam's, and the
// seam's Validate (which runs this function first) is what BOTH doors run.
func validateBlueprintSavePayload(_ context.Context, payload []byte) error {
	if len(payload) > MaxBlueprintSourceBytes+blueprintEnvelopeBytes {
		return fmt.Errorf("refused: the blueprint_save payload exceeds the %d KiB envelope (1 MiB source + 8 KiB of JSON around it)", (MaxBlueprintSourceBytes+blueprintEnvelopeBytes)/1024)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p BlueprintSaveProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: blueprint_save payload is not {\"name\":…, \"content\":…, \"createOnly\"?:bool, \"note\"?:string}: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: blueprint_save payload carries trailing JSON: %v", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("refused: blueprint_save needs a non-blank name")
	}
	if len(p.Name) > maxBlueprintNameBytes {
		return fmt.Errorf("refused: blueprint_save name is longer than %d — the name grammar's ceiling", maxBlueprintNameBytes)
	}
	if p.Content == "" {
		return fmt.Errorf("refused: blueprint_save needs the FULL proposed source in content — never a patch, never empty")
	}
	if len(p.Content) > MaxBlueprintSourceBytes {
		return fmt.Errorf("refused: blueprint source exceeds 1 MiB")
	}
	return nil
}

// ValidateBlueprintSave is the desk half exported for the seam wrapper
// (the seam runs it, then the store halves — one function per door, TD3).
func ValidateBlueprintSave(ctx context.Context, payload []byte) error {
	return validateBlueprintSavePayload(ctx, payload)
}
