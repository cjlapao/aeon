// environment_kinds.go — S59 T7: the thirteenth proposal kind,
// environment_restrict, as a registry entry in this package.
//
// The desk keeps only the pure half, exactly the blueprint_kinds.go shape:
// the payload SHAPE (a strict decode — a smuggled "backend", "source",
// "actor", "status", "kind" or any value key is a PARSE refusal, so an agent
// cannot choose the backend its loop runs in, cannot name whose credential it
// wants, and cannot mark its own row committed), the non-blank environment id,
// the 1 MiB spec bound (the plane's own — the store refuses a spec this large
// too, so a payload the write door would refuse never becomes an inert row),
// and the required reason of 1–512 bytes (the records plane's own discipline,
// carried: an unreasoned ask is not an ask a person can decide).
//
// What the desk may NOT do is read the row. The store halves — the environment
// exists and is not archived, `execenv.ParseSpec` + `execenv.Validate` over
// the row's OWN backend, `execenv.Restricts(old, new)` returning nothing, and
// the baseline revision still being the row's — arrive as `Seams` from
// `cmd/aeon`: the desk's dependency rule is stdlib + policy + paths, and
// importing internal/execenv (the validator) or internal/store (the reader)
// would drag both engines into the decision code. The seam's Validate REPLACES
// this file's shape half with the full desk-then-store function (the merge rule
// in desk.New), so the create door and the commit door run ONE function (S35
// TD3) and the effect is performed with the COMMITTING PERSON as the update's
// actor — the proposer stays where it belongs, in the row's created_by_*.
//
// `baseRevision` is in the PAYLOAD and is NOT in the tool's schema: the records
// service reads the row and stamps it (OL3 — a value a model can type is not a
// fact). It is what makes a proposal written against a row that has since moved
// refuse at both doors instead of quietly overwriting somebody's edit.
//
// The kind is restrictive in EV16's sense and in AL11's: it can only take
// things away from what a loop's commands may see. That is also why it is the
// ONLY environment ask that exists — there is no widen, create, bind, unbind,
// teardown or secret-reference kind, and a backend's strength moves by a
// person alone (EV19).
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

// KindEnvironmentRestrict is S59 T7's kind, thirteenth in the store's order
// (migration 20261103181500; the store mirror is store.ProposalKinds).
const KindEnvironmentRestrict Kind = "environment_restrict"

// The payload bounds, re-declared (the importer pin — see the file header):
// the spec's 1 MiB (the store's `environmentSpecRefusal` bound, duplicated
// HERE so a spec the write door would refuse never becomes an inert row), and
// the 8 KiB envelope for the JSON around it.
const (
	MaxEnvironmentSpecBytes   = 1 << 20
	environmentEnvelopeBytes  = 8 << 10
	maxEnvironmentReasonBytes = 512 // the records plane's reason discipline
	maxEnvironmentIdBytes     = 64  // the plane's ids are env_<32 hex>; the slug grammar is ≤ 64
)

// EnvironmentRestrictProposal is environment_restrict's payload — the whole
// proposed spec, never a patch (a patch would let a widening hide in the lines
// the patch does not name; an environment is decided whole, the same argument
// blueprint_save makes for a source). environmentId is the row being tightened
// (id or slug — the plane reads either). baseRevision is the row's revision AT
// PROPOSE TIME, stamped by the service that read the row; the commit refuses
// the proposal whose row has moved. reason is the sentence a person reads at
// the desk.
type EnvironmentRestrictProposal struct {
	EnvironmentID string `json:"environmentId"`
	Spec          string `json:"spec"`
	BaseRevision  int64  `json:"baseRevision"`
	Reason        string `json:"reason"`
}

// validateEnvironmentRestrictPayload is the desk's pure half of Validate (the
// shape). The store half — the row exists, parses, validates against its own
// backend, and is no looser than it is today at the revision it was read at —
// is the seam's, and the seam's Validate (which runs this function first) is
// what BOTH doors run.
func validateEnvironmentRestrictPayload(_ context.Context, payload []byte) error {
	if len(payload) > MaxEnvironmentSpecBytes+environmentEnvelopeBytes {
		return fmt.Errorf("refused: the environment_restrict payload exceeds the %d KiB envelope (1 MiB spec + 8 KiB of JSON around it)", (MaxEnvironmentSpecBytes+environmentEnvelopeBytes)/1024)
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p EnvironmentRestrictProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: environment_restrict payload is not {\"environmentId\":…, \"spec\":…, \"baseRevision\":<int>, \"reason\":…}: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: environment_restrict payload carries trailing JSON: %v", err)
	}
	if strings.TrimSpace(p.EnvironmentID) == "" {
		return fmt.Errorf("refused: environment_restrict needs a non-blank environmentId — environment_list shows what exists")
	}
	if len(p.EnvironmentID) > maxEnvironmentIdBytes {
		return fmt.Errorf("refused: environment_restrict environmentId is longer than %d — an id or a slug is not an essay", maxEnvironmentIdBytes)
	}
	if strings.TrimSpace(p.Spec) == "" {
		return fmt.Errorf("refused: environment_restrict needs the FULL proposed spec in spec — never a patch, never empty (an environment is decided whole)")
	}
	if len(p.Spec) > MaxEnvironmentSpecBytes {
		return fmt.Errorf("refused: environment spec exceeds 1 MiB")
	}
	if r := strings.TrimSpace(p.Reason); r == "" {
		return fmt.Errorf("refused: environment_restrict needs a reason — an unreasoned ask is not an ask a person can decide")
	} else if len(p.Reason) > maxEnvironmentReasonBytes {
		return fmt.Errorf("refused: environment_restrict reason is %d bytes and the cap is %d — a sentence for the desk, not a justification essay", len(p.Reason), maxEnvironmentReasonBytes)
	}
	if p.BaseRevision < 1 {
		return fmt.Errorf("refused: environment_restrict needs the revision the proposal was written against (baseRevision ≥ 1) — a proposal with no baseline cannot be checked for staleness")
	}
	return nil
}

// ValidateEnvironmentRestrict is the desk half exported for the seam wrapper
// (the seam runs it, then the store halves — one function per door, TD3).
func ValidateEnvironmentRestrict(ctx context.Context, payload []byte) error {
	return validateEnvironmentRestrictPayload(ctx, payload)
}
