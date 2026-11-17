// roadmap_kinds.go — S50 T5: the fifteenth proposal kind, roadmap_idea, as
// a registry entry in this package.
//
// The desk keeps only the pure half, exactly the automation_kinds.go shape:
// the payload SHAPE (a strict decode — a smuggled "actor", "status",
// "provenance", "priorityState" or "estimate" is a PARSE refusal, so no
// payload can name the state it wants or the actor it claims; the payload
// names WHICH IDEA and NOTE, never where it lands on the ladder), the
// non-blank component root, the non-blank title, and the required note of
// 1–1024 bytes (RM8's ask-a-person rule: the note is the sentence a person
// reads at the desk; the epic is the one optional tag).
//
// The title rules mirror what roadmap.add itself demands (the service's
// createRow: a non-blank title and a non-blank component root — the same
// two sentences in spirit, re-spelled here because the desk imports neither
// the roadmap package nor the store). The bounds above are the desk's own
// (the columns carry no length CHECK; a proposal payload is something the
// desk reads, so it is bounded): a board renders titles, an epic is a tag,
// and a note is a note — not an essay.
//
// What the desk may NOT do is read the component or the board. The store
// halves — the component root exists in the registry, and the identical
// OPEN item does not already exist — arrive as `Seams` from `cmd/aeon`,
// replacing this file's shape half with the full desk-then-store function
// (the merge rule in desk.New), so the create door and the commit door run
// ONE function (S35 TD3). The item is then written by roadmap's own create
// with the COMMITTING PERSON as actor and provenance `authored` — the
// proposer stays where it belongs, in the row's created_by_*, and the audit
// is the one 'add' line CreateAudited writes in the same transaction.
//
// The kind adds and nothing else, in RM8's sense, beside AN9's and EV16's:
// there is deliberately no roadmap_update, roadmap_cancel, roadmap_estimate,
// roadmap_priority or dispatch kind — editing, retiring, sizing, ranking or
// starting a row all move by a person alone, through the doors T2 built and
// T4's guards pinned human-only.

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

// KindRoadmapIdea is S50 T5's kind, fifteenth in the store's order
// (migration 20261104101500; the store mirror is store.ProposalKinds).
const KindRoadmapIdea Kind = "roadmap_idea"

// The payload bounds (the importer pin — see the file header): the root is
// a path-shaped name, a title is what a board renders, an epic is a tag,
// and the note is 1–1024 — long enough for the idea a person decides on,
// short enough that the desk reads all of it.
const (
	maxRoadmapIdeaRootBytes  = 512
	maxRoadmapIdeaTitleBytes = 300
	maxRoadmapIdeaEpicBytes  = 200
	maxRoadmapIdeaNoteBytes  = 1024
)

// RoadmapIdeaProposal is roadmap_idea's payload — WHICH IDEA and NOTE,
// never WHERE on the ladder. Title and note are the person's sentences;
// the epic is the one optional tag (R2: a tag, not a row). There is no
// priority, estimate, status or actor field because an idea that arrives
// ranked would be an idea that arrived decided.
type RoadmapIdeaProposal struct {
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Epic          string `json:"epic,omitempty"`
	Note          string `json:"note"`
}

// validateRoadmapIdeaPayload is the desk's pure half of Validate (the
// shape). The store halves — the component root exists, and the identical
// open item does not — are the seam's, and the seam's Validate (which runs
// this function first) is what BOTH doors run.
func validateRoadmapIdeaPayload(_ context.Context, payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	var p RoadmapIdeaProposal
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: roadmap_idea payload is not {\"componentRoot\":…, \"title\":…, \"epic\":…, \"note\":…} and carries no field besides them: %v", err)
	}
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("refused: roadmap_idea payload carries trailing JSON: %v", err)
	}
	if strings.TrimSpace(p.ComponentRoot) == "" {
		return fmt.Errorf("refused: roadmap_idea names the project root it plans — a roadmap item without a root is not a plan of intent")
	}
	if len(p.ComponentRoot) > maxRoadmapIdeaRootBytes {
		return fmt.Errorf("refused: roadmap_idea componentRoot is longer than %d — a root is a path, not an essay", maxRoadmapIdeaRootBytes)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("refused: roadmap_idea needs a title — the forward Gantt renders rows, and a row without a name is not a plan of intent")
	}
	if len(p.Title) > maxRoadmapIdeaTitleBytes {
		return fmt.Errorf("refused: roadmap_idea title is %d bytes and the cap is %d — a board renders titles, not paragraphs", len(p.Title), maxRoadmapIdeaTitleBytes)
	}
	if len(p.Epic) > maxRoadmapIdeaEpicBytes {
		return fmt.Errorf("refused: roadmap_idea epic is %d bytes and the cap is %d — an epic is a tag, not a row (R2)", len(p.Epic), maxRoadmapIdeaEpicBytes)
	}
	if strings.TrimSpace(p.Note) == "" {
		return fmt.Errorf("refused: roadmap_idea needs a note — the sentence a person reads before adding your idea is the ask itself (RM8)")
	}
	if len(p.Note) > maxRoadmapIdeaNoteBytes {
		return fmt.Errorf("refused: roadmap_idea note is %d bytes and the cap is %d — a note for the desk, not a justification essay", len(p.Note), maxRoadmapIdeaNoteBytes)
	}
	return nil
}

// ValidateRoadmapIdea is the desk half exported for the seam wrapper (the
// seam runs it, then the store halves — one function per door, TD3).
func ValidateRoadmapIdea(ctx context.Context, payload []byte) error {
	return validateRoadmapIdeaPayload(ctx, payload)
}
