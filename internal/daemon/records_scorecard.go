// records_scorecard.go — S63 T6 (RT11, standing rule 2): the scorecard
// family's three arms of the records service. All three are reads that
// forward to the SAME services the person's socket doors answer through
// (protocol.ScorecardService / protocol.DigestService — the very seam
// values Table.Scorecard and Table.Digest hold, rule 4), so the tool door
// and the socket door cannot drift field for field: scorecard_mine twins
// scorecard.agent, scorecard_component twins scorecard.component, and
// digest_learned twins digest.learned — one implementation, two doors.
//
// What this file does NOT do, and each is a rule rather than an omission:
//
//   - it takes no subject from the input. The identity vocabulary —
//     agent, slug, id, root, component — appears in NO schema (T6's
//     catalog guard checks that); the subject is READ FROM THE STEP:
//     ident.Agent for the agent's own card (F13, OL3), ident.RootID for
//     the loop's own root component. A person session is turned back
//     before the switch by personDoorRefusal with a pointer to the
//     person's own door (the S46 MC2 / S52 LM7 shape);
//   - it holds no retro arm. retro.* rows are the digest's input and the
//     person's doors; no write exists in this family (RT11) — there is no
//     retro_backfill twin to reach, and the score never writes back to
//     anything (RT3; T5's applying-word guards);
//   - it drops, by construction, the one field the person's component
//     door carries that an agent may not see: the per-child BREAKDOWN
//     (T6.2 — a roster by the back door would rank; the aggregate and the
//     honesty coverage are the whole answer here), returned through the
//     card+statement pair the agent's own card already uses;
//   - it answers nothing when the plane is unwired: a storeless mount
//     refuses by name (V13), never a card computed from nothing.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// scorecardPlaneRefusal is the V13 sentence for this family.
func scorecardPlaneRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the scorecard plane, and this mount did not wire it — no card is answered, and no card is answered empty (V13)", name)}
}

// scorecardSubject is the stamped-subject refusal: a step whose identity
// names no agent (or no root) has no subject to measure — the identity is
// read from the store, never guessed (OL3, F13).
func scorecardSubject(tool, what string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s measures %s, and this step's identity stamps none — the subject is read from the store, never guessed (OL3, F13)", tool, what)}
}

// scorecardMine answers scorecard_mine through the SAME ScorecardService
// the person's scorecard.agent door runs, with the subject fixed to the
// caller's stamped slug. The window is the whole input: an agent, slug or
// root arriving in params is a PARSE error (additionalProperties:false in
// the catalog schema), never a re-aimed subject.
func (r *RecordsService) scorecardMine(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.scorecardPlane == nil {
		return nil, scorecardPlaneRefusal("scorecard_mine"), nil
	}
	if ident.Agent == "" {
		return nil, scorecardSubject("scorecard_mine", "the agent's own finished loops"), nil
	}
	var w protocol.ScorecardWindow
	if f, err := decode(input, &w); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.scorecardPlane.Agent(ctx, protocol.ScorecardAgentParams{
		Agent:           ident.Agent,
		RootID:          ident.RootID,
		ScorecardWindow: w,
	})
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// scorecardComponent answers scorecard_component through the SAME service
// the person's scorecard.component door runs, over the loop's own ROOT
// component with includeChildren fixed true (T6: the service fixes it —
// there is no input that turns it off), and returns the card and the
// statement WITHOUT the person's per-child breakdown (T6.2: the schema
// cannot rank anyone because the roster is not in this answer).
func (r *RecordsService) scorecardComponent(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.scorecardPlane == nil {
		return nil, scorecardPlaneRefusal("scorecard_component"), nil
	}
	if ident.RootID == "" {
		return nil, scorecardSubject("scorecard_component", "the loop's own root component"), nil
	}
	var w protocol.ScorecardWindow
	if f, err := decode(input, &w); f != nil || err != nil {
		return nil, f, err
	}
	out, err := r.scorecardPlane.Component(ctx, protocol.ScorecardComponentParams{
		ID:              ident.RootID,
		IncludeChildren: true,
		ScorecardWindow: w,
	})
	if err != nil {
		return nil, nil, err
	}
	return protocol.ScorecardCardResult{Card: out.Card, Statement: out.Statement}, nil, nil
}

// digestLearned answers digest_learned through the SAME DigestService the
// person's digest.learned door runs, over the loop's own root component.
// The schema is {since?, until?} — an id smuggled into params is a parse
// error, and the component is the stamped root, so the digest an agent
// reads is its own tree's: project-scope learnings only, which structurally
// excludes every other agent's agent-scoped memory (T6.2 — the store read
// filters scope='project'; nothing here re-implements that).
func (r *RecordsService) digestLearned(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.digestPlane == nil {
		return nil, scorecardPlaneRefusal("digest_learned"), nil
	}
	if ident.RootID == "" {
		return nil, scorecardSubject("digest_learned", "the loop's own root component"), nil
	}
	var p protocol.DigestParams
	if f, err := decode(input, &p); f != nil || err != nil {
		return nil, f, err
	}
	p.ID = ident.RootID
	out, err := r.digestPlane.Learned(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}
