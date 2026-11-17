package daemon

// records_learnings.go — S52 T4 (LM1, LM4, LM5, LM7): the memory family's
// four arms of the management catalog. learning_query and learning_show
// are the agent's OWN view: the readable set is resolved from the step's
// stamped identity (LM1's scopes — the person's word, the projects this
// agent has run in, its own agent scope), read through the ONE MineQuery
// the learnings.forAgent door answers through (rule 4), and rendered by
// the ONE protocol renderer (byte parity by construction, not by test).
// A row outside the readable set is refused with the SAME sentence a
// nonexistent id gets (LM5: no oracle for a slug-guesser, in success or
// in error).
//
// learning_add is the agent's own filing: observed or assumed (LM4 —
// told says "a person said it" and is refused here by schema AND by this
// arm, relayed by the plane below the schema), scope stamped from the
// identity (an agent scope is the caller's own slug; a project scope its
// own root; a param naming another key is REFUSED, not honoured), body
// and tags bounded, and the whole door capped per agent at
// learnings.MaxSelfLogAddsPerAgent counted off the append-only audit
// ledger BEFORE the write — a counted refusal that names its number.
//
// learning_propose_retract files the INERT learning_retract ask through
// the shared inert-proposal path (createInertProposal: the payload cap,
// the per-agent open-proposal cap, the seam's shared desk-then-store
// validator, TD3 — what create refuses, commit refuses in the same
// words). The readable check happens HERE, at create, and its out-of-
// scope answer is the no-such-learning sentence: an ask is only ever
// about a memory this agent can see, and an ask about what you cannot
// see is indistinguishable from an ask about nothing.
//
// There is no retract arm, no restore arm, no edit arm and no relay arm
// to reach — retract and restore are the person's doors (LM7/LM8; T3's
// guards refuse each such name by word), and the person reaching these
// four names is turned back before the switch by personDoorRefusal with
// the door that does answer them.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/protocol"
)

// The self-log door's bounds (T4): a body the size of a short paragraph
// and a handful of tags — a learning is a sentence worth keeping, not a
// document; the documents have their attachments plane.
const (
	maxMineBodyBytes = 2048
	maxMineTags      = 8
)

// memoryLessRefusal is V13's voice for this family: the plane is not
// wired, the door says so by name, and answers zero it never.
func memoryLessRefusal(name string) *Failure {
	return &Failure{Sentence: fmt.Sprintf("refused: %s needs the learnings plane, which this runtime does not wire — nothing is answered, and nothing is answered empty (V13)", name)}
}

// learningSelf is mcpSelf's law for memory (LM1): the agent is the step's
// stamped slug. Identity is read from the step, never guessed, and never
// typed.
func learningSelf(tool string, ident Identity) (string, *Failure) {
	if ident.Agent == "" {
		return "", &Failure{Sentence: fmt.Sprintf("refused: %s answers from the agent's own view, and this step stamps no agent — identity is read from the store, never guessed (OL3)", tool)}
	}
	return ident.Agent, nil
}

// noSuchLearningSentence is LM5's no-oracle answer: ONE sentence for the
// row that does not exist, the row that is somebody else's, and the row
// that was retracted (its body is history, not memory, on every live
// view). A difference between the three would be an oracle.
func noSuchLearningSentence(id int64) string {
	return fmt.Sprintf("refused: no such learning with id %d — the memory doors answer rows your own scopes can reach (the person's word, the projects you have run in, your own memory), and what they cannot reach answers exactly like what does not exist (S52 LM1, LM5)", id)
}

func (r *RecordsService) learningQuery(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.learningsPlane == nil {
		return nil, memoryLessRefusal("learning_query"), nil
	}
	self, f := learningSelf("learning_query", ident)
	if f != nil {
		return nil, f, nil
	}
	var in protocol.LearningsMineQueryParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	var tags []learnings.TagPattern
	for _, t := range in.Tags {
		tags = append(tags, learnings.TagPattern{Pattern: t.Pattern, Prefix: t.Prefix})
	}
	hits, ref := r.learningsPlane.MineQuery(ctx, self, learnings.MineFilter{Query: in.QueryText, Tags: tags, Limit: in.Limit})
	if ref != nil {
		return nil, &Failure{Sentence: ref.Reason}, nil
	}
	return protocol.AnswerLearningsForAgent(hits), nil, nil
}

func (r *RecordsService) learningShow(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.learningsPlane == nil {
		return nil, memoryLessRefusal("learning_show"), nil
	}
	self, f := learningSelf("learning_show", ident)
	if f != nil {
		return nil, f, nil
	}
	var in protocol.LearningsMineShowParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.ID <= 0 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: learning_show names a learning by its positive id — %d is not a row this plane has ever filed", in.ID)}, nil
	}
	roots, ref := r.learningsPlane.ResolveReadable(self)
	if ref != nil {
		return nil, &Failure{Sentence: ref.Reason}, nil
	}
	lr, found, err := r.learningsPlane.Show(in.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: learning_show %d: %w", in.ID, err)
	}
	if !found || lr.Retracted || !learnings.MayRead(lr, self, roots) {
		return nil, &Failure{Sentence: noSuchLearningSentence(in.ID)}, nil
	}
	return protocol.AnswerLearningsItem(lr), nil, nil
}

func (r *RecordsService) learningAdd(ctx context.Context, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.learningsPlane == nil {
		return nil, memoryLessRefusal("learning_add"), nil
	}
	self, f := learningSelf("learning_add", ident)
	if f != nil {
		return nil, f, nil
	}
	var in protocol.LearningsMineAddParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	// LM4's half below the schema's enum: the arm re-checks, because a
	// schema is a promise and this line is the enforcement.
	if in.Provenance != string(learnings.ProvenanceObserved) && in.Provenance != string(learnings.ProvenanceAssumed) {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: an agent's own log is observed or assumed — %q is not nameable here: told says \"a person said it\" and belongs to the human door, relayed is the board hook's word (S52 LM4)", in.Provenance)}, nil
	}
	if in.Body == "" {
		return nil, &Failure{Sentence: "refused: learning_add needs a body — the sentence worth keeping is the learning"}, nil
	}
	if len(in.Body) > maxMineBodyBytes {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: the body is %d bytes and this door's cap is %d — a learning is a sentence worth keeping, not a document; documents ride attachments (S52 T4)", len(in.Body), maxMineBodyBytes)}, nil
	}
	if len(in.Tags) > maxMineTags {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %d tags and this door's cap is %d — tags that need more than eight slots are a document with a table of contents", len(in.Tags), maxMineTags)}, nil
	}
	// The scope is STAMPED: agent → the caller's own slug, project → the
	// identity's own running root. A typed key may confirm the stamp,
	// never replace it — a stamp that honours a foreign key is a stamp
	// that was typed.
	key := in.ScopeKey
	switch in.Scope {
	case "agent":
		if key != "" && key != self {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: learning_add writes agent scope %q and you are %q — an agent's own memory is filed under its own slug, never a named one (S52 LM1); drop scopeKey to stamp yourself", key, self)}, nil
		}
		key = self
	case "project":
		if key != "" && key != ident.ComponentRoot {
			return nil, &Failure{Sentence: fmt.Sprintf("refused: learning_add writes project scope %q and this step runs in %q — a project learning is filed where the work ran, never a named root (S52 LM1); drop scopeKey to stamp your own", key, ident.ComponentRoot)}, nil
		}
		if ident.ComponentRoot == "" {
			return nil, &Failure{Sentence: "refused: this step carries no component root, so no project learning can be stamped to one — file it in your own agent scope instead (S52 LM1)"}, nil
		}
		key = ident.ComponentRoot
	default:
		return nil, &Failure{Sentence: fmt.Sprintf("refused: learning_add takes scope agent or project — %q is not one; the person's scope is the person's door, learnings.add (S52 LM7)", in.Scope)}, nil
	}
	// The provenance pointers ride the stamp too: the loop and step this
	// learning was seen in, from the identity, never typed (OL3).
	var loopSlug *string
	if ident.LoopSlug != "" {
		s := ident.LoopSlug
		loopSlug = &s
	}
	var stepID *int
	if ident.StepID > 0 {
		id := int(ident.StepID)
		stepID = &id
	}
	got, ref := r.learningsPlane.SelfLogAdd(ctx, learnings.AppendInput{
		Scope:          learnings.Scope(in.Scope),
		ScopeKey:       key,
		Body:           in.Body,
		Provenance:     learnings.Provenance(in.Provenance),
		Tags:           in.Tags,
		SourceLoopSlug: loopSlug,
		SourceStepID:   stepID,
	}, learnings.Actor{Kind: "agent", ID: self})
	if ref != nil {
		return nil, &Failure{Sentence: ref.Reason}, nil
	}
	return protocol.AnswerLearningsItem(got), nil, nil
}

func (r *RecordsService) learningProposeRetract(ctx context.Context, by Actor, ident Identity, input json.RawMessage) (any, *Failure, error) {
	if r.learningsPlane == nil || r.proposals == nil {
		return nil, memoryLessRefusal("learning_propose_retract"), nil
	}
	self, f := learningSelf("learning_propose_retract", ident)
	if f != nil {
		return nil, f, nil
	}
	var in protocol.LearningsMineProposeRetractParams
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.ID <= 0 {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: learning_propose_retract names a learning by its positive id — %d is not a row this plane has ever filed", in.ID)}, nil
	}
	// The readable check, at create: out-of-scope, retracted and
	// nonexistent all answer the SAME sentence — an ask is only ever
	// about a memory this agent can see (TD3's nothing-inert asks are
	// refused here; the person at the desk later decides with the reason
	// in front of them).
	roots, ref := r.learningsPlane.ResolveReadable(self)
	if ref != nil {
		return nil, &Failure{Sentence: ref.Reason}, nil
	}
	lr, found, err := r.learningsPlane.Show(in.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: learning_propose_retract %d: %w", in.ID, err)
	}
	if !found || lr.Retracted || !learnings.MayRead(lr, self, roots) {
		return nil, &Failure{Sentence: noSuchLearningSentence(in.ID)}, nil
	}
	// The payload is built field by field from the decoded input — never
	// the raw bytes forwarded, so a smuggled field cannot ride into the
	// proposal row (the mcp/roadmap/environment arms' law, restated).
	payload, err := json.Marshal(desk.LearningRetractProposal{ID: in.ID, Reason: in.Reason})
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: learning_propose_retract: the payload will not encode: %w", err)
	}
	return r.createInertProposal(ctx, by, string(desk.KindLearningRetract), payload)
}
