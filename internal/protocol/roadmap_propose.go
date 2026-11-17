// roadmap_propose.go — S50 T5 (RM8): the roadmap plane's ONE agent door.
//
// roadmap.proposeIdea files an INERT proposal of kind roadmap_idea — the
// one thing an agent may ask of the roadmap (T4's guard family pinned the
// eight write doors as the person's alone; this is not a ninth door around
// them, it is a REQUEST for a person to open one). The door dispatches to
// the same RecordsAPI.Do the tool door calls, so the socket answer and the
// tool answer are one answer, not two renderings of it; the author is the
// STEP, stamped on the service side from the step's identity — params
// cannot name an author and do not try (OL3).
//
// V13: registered when and only when the records service stands — the
// service is what honours what makes the door safe (the stamped agent
// actor, the shared create validator of T5's seam, the per-agent
// open-proposal cap). Without Table.Records the method is ABSENT
// (ErrUnknownMethod), never registered-and-refusing.
//
// The fixture pair lives in conformance/roadmap-propose/: the inert row the
// door files, and the door's other half — a smuggled field refused at the
// strict parse before the plane is consulted, the refusal naming it.

package protocol

import (
	"context"
	"encoding/json"
	"errors"
)

// roadmapProposeIdeaTool is the catalog tool name the twin dispatches
// under (RecordsAPI.Do takes the tool name — the catalog's vocabulary, not
// a second method table).
const roadmapProposeIdeaTool = "roadmap_propose_idea"

// RoadmapProposeIdeaParams is the propose door's documented params — the
// catalog schema's shape as a Go type so the conformance fixtures
// round-trip through it. WHICH IDEA (the root it plans, the title a board
// will render, the optional epic tag) and the NOTE the person reads before
// deciding; nothing else has anywhere to go (a smuggled "actor", "status",
// "provenance", "priorityState" or "estimate" meets the service's strict
// decode and its refusal names the field — an idea that arrived ranked
// would be an idea that arrived decided).
type RoadmapProposeIdeaParams struct {
	ComponentRoot string `json:"componentRoot"`
	Title         string `json:"title"`
	Epic          string `json:"epic,omitempty"`
	Note          string `json:"note"`
}

// RegisterRoadmapProposeIdea registers the twin when (and only when) the
// records service is wired — the RegisterSeed/V13 rule the policy, autonomy,
// blueprint, environment and automation twins carry: a table without
// Table.Records leaves it ABSENT (ErrUnknownMethod), never
// registered-and-refusing, because the records service is what honours what
// makes this door safe (the stamped step actor, the shared create validator
// of T5's seam, the per-agent open-proposal cap). Returns true when it
// stands (already or just now); false when the seam is missing.
func (t *Table) RegisterRoadmapProposeIdea() bool {
	if t.Records == nil {
		return false
	}
	if _, ok := t.methods[MethodRoadmapProposeIdea]; ok {
		return true // already standing (a re-registration is a no-op, not a second door)
	}
	t.register(&Method{
		Name:               MethodRoadmapProposeIdea,
		RequiresInitialize: true,
		Handle: func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
			// One definition, two doors: the SAME RecordsAPI.Do the tool path
			// calls, the actor stamped on the service side from the step's
			// identity, never decoded from params (OL3).
			out, sentence, err := t.Records.Do(ctx, sess, roadmapProposeIdeaTool, params)
			if err != nil {
				return nil, err
			}
			if sentence != "" {
				return nil, ErrBadParams{Err: errors.New(sentence)}
			}
			return out, nil
		},
	})
	return true
}
