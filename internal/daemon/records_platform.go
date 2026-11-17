package daemon

// records_platform.go — S54 T4 (SP4): the platform family's ONE arm,
// `index_status`. T0's visibility ruling is the whole shape: freshness of
// the STEP'S OWN root, quoted from the same seams the person's
// `platform.status` composes over (the seam is built in platform_reads.go
// over the very PlatformDeps the panel uses — one implementation, two
// doors, scopes apart), minus the operator-view fields. The scope is the
// step's identity, never a parameter (OL3): there is no root param to lie
// with, and no call can ask about another component's index.
//
// What is NOT here, in the spec's words: no control tool, no proposal kind.
// There is no arm for starting, cancelling or pricing a pass — the four
// operations are a person's door (SP3), and T3's guards refuse the names
// (dotted and underscored) from ever joining this switch, the catalog, the
// registry or the agent surfaces.

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/records"
)

func (r *RecordsService) indexStatus(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.platformIndex == nil {
		return nil, &Failure{Sentence: "refused: index_status is not wired on this daemon — there is no retrieval status seam and no graph service to report freshness about (V13: absent, not answering unwired forever)"}, nil
	}
	if ident.ComponentRoot == "" {
		// Belt beside personDoorRefusal: a stepless identity has no root to
		// scope to, and guessing one would invent the very scope this door
		// exists to respect.
		return nil, &Failure{Sentence: "refused: index_status is scoped to the root of the step that calls it, and this call carries no step — a person reads platform.status, which answers every root the daemon knows"}, nil
	}
	var in struct{}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	// The catalog's own claim, re-checked at the door: this family has one
	// spec, and it is a read. A Mutates:true twin could not reach this arm
	// (the switch has one case), but the census here means the sentence the
	// tool ships cannot silently disagree with the arm that answers it.
	n := 0
	for _, sp := range records.Catalog() {
		if sp.Family == "platform" {
			n++
			if sp.Mutates {
				return nil, &Failure{Sentence: "refused: the platform family carries a mutating spec — SP3 permits no such thing; operations are a person's door"}, nil
			}
		}
	}
	if n != 1 {
		return nil, &Failure{Sentence: "refused: the platform family's census is not one — index_status cannot answer for a family the catalog has rewritten under it"}, nil
	}
	view, err := r.platformIndex(ctx, ident.ComponentRoot)
	if err != nil {
		return nil, nil, err
	}
	return view, nil, nil
}
