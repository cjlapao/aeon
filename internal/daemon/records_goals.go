package daemon

// records_goals.go — the goal/brief/epic arms of the management catalog
// (S38 T9's one implementation, split for SIZE1 in S47 T7 by cohesion: this
// file is the goal family and the person-scope resolution only its
// person-path doors use). The rule that governs every one of them lives in
// records.go and is not restated here: the identity a record carries —
// project, component, source loop, source step, author — is read from the
// STEP whose id rides the context, never from the input the model typed
// (OL3).

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

func (r *RecordsService) goalCreate(ident Identity, by Actor, input json.RawMessage) (any, *Failure, error) {
	if ident.User {
		// The person's goal door (S38 T16, P34): the terminal says everything
		// a step would have stamped, because there is no step to derive it
		// from — and the store still refuses what it always refuses.
		return r.goalCreateAsPerson(by, input)
	}
	var in struct {
		Title      string `json:"title"`
		Type       string `json:"type"`
		BodyNote   string `json:"body_note"`
		EpicID     string `json:"epic_id"`
		Document   string `json:"document"`
		Components []struct {
			Component   string   `json:"component"`
			Rationale   string   `json:"rationale"`
			EstimateUSD *float64 `json:"estimate_usd"`
			Confidence  string   `json:"confidence"`
			Basis       []string `json:"basis"`
		} `json:"components"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	// The stamped half — none of this came from `in`, because the schema has
	// no field for any of it and the decoder refused one that arrived.
	sourceKind := "manual"
	if bp, err := r.st.LoopBlueprintID(store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}); err == nil && bp == "brainstorm" {
		sourceKind = "brainstorm"
	}
	step := ident.StepID
	in2 := store.NewGoal{ComponentID: ident.RootID, EpicID: in.EpicID, Title: in.Title, Type: in.Type,
		BodyNote: in.BodyNote, SourceKind: sourceKind, SourceLoop: ident.LoopSlug, SourceStep: &step,
		By: store.Actor{Kind: by.Kind, ID: by.ID}, Submit: true}
	for _, c := range in.Components {
		comp, f := r.resolveComponent(ident, c.Component)
		if f != nil {
			return nil, f, nil
		}
		conf, f := parseConfidence(c.Confidence)
		if f != nil {
			return nil, f, nil
		}
		in2.Components = append(in2.Components, store.GoalComponent{ComponentID: comp.ID,
			Rationale: c.Rationale, EstimateUSD: c.EstimateUSD, Confidence: conf, Basis: c.Basis})
	}
	goal, rf, err := r.st.CreateGoal(in2)
	if err != nil {
		return nil, nil, err
	}
	if rf != nil {
		return nil, &Failure{Sentence: rf.Sentence}, nil
	}
	out := map[string]any{"goal": goal}
	// The goal OWNS what this loop produced so far (document_owner.go): the
	// loop keeps its rows and the link back (goal.source_loop), the goal gets
	// the documents. A goal the loop already adopted into stays the owner.
	if _, err := r.st.AdoptDocuments(store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}, goal.ID); err != nil {
		return nil, nil, err
	}
	if in.Document != "" {
		sc := store.Scope{LoopSlug: ident.LoopSlug, ComponentRoot: ident.ComponentRoot}
		a, arf, err := r.st.Attach(sc, "goal", goal.ID, in.Document, &step)
		if err != nil {
			return nil, nil, err
		}
		if arf != nil {
			return nil, &Failure{Sentence: arf.Sentence}, nil
		}
		out["attachment"] = a
	}
	return out, nil, nil
}

func (r *RecordsService) goalUpdate(ident Identity, by Actor, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		GoalID   string `json:"goal_id"`
		Title    string `json:"title"`
		BodyNote string `json:"body_note"`
		EpicID   string `json:"epic_id"`
		Reason   string `json:"reason"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	g, err := r.st.GetGoal(in.GoalID)
	if err != nil {
		return nil, notFoundFailure(in.GoalID, err), nil
	}
	if g.ComponentID != ident.RootID {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: goal %s belongs to another root — an agent edits within its own (OL3)", in.GoalID)}, nil
	}
	goal, rf, err := r.st.UpdateGoal(store.GoalEdit{ID: in.GoalID, Title: in.Title, BodyNote: in.BodyNote,
		EpicID: in.EpicID, By: store.Actor{Kind: by.Kind, ID: by.ID}})
	if err != nil {
		return nil, nil, err
	}
	if rf != nil {
		return nil, &Failure{Sentence: rf.Sentence}, nil
	}
	// The stated reason rides the RESULT (and so the transcript, where the
	// review trail is read); GoalEdit itself stays reason-free — a review row
	// belongs to a DECISION, and this is an edit, not one.
	return map[string]any{"goal": goal, "reason": in.Reason}, nil, nil
}

// resolveComponent accepts a component's id, or its EXACT name within the
// step's root — and nothing else. Not a prefix, not a fuzzy match: a goal
// staked on the wrong component is a wrong goal. (A goal's members are the
// root itself or its children, CT10 — so "within" is exactly the root or a
// direct child here; deeper work is named by the component that owns it.)
// ——— the person's door (S38 T16) ——————————————————————————————————————————
//
// A person carries no step, no loop and no project, so everything the step
// half STAMPS the person must SAY — and the saying happens here, in the same
// arm, with the same store calls and the same refusals. This is not a second
// door onto different rules; it is the same door with the stamped half
// supplied by the only party who can supply it honestly (OL3's other side).

func (r *RecordsService) goalCreateAsPerson(by Actor, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		Title      string `json:"title"`
		Type       string `json:"type"`
		BodyNote   string `json:"body_note"`
		EpicID     string `json:"epic_id"`
		Root       string `json:"root"`
		Components []struct {
			Component   string   `json:"component"`
			Rationale   string   `json:"rationale"`
			EstimateUSD *float64 `json:"estimate_usd"`
			Confidence  string   `json:"confidence"`
			Basis       []string `json:"basis"`
		} `json:"components"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Title == "" || in.Type == "" {
		return nil, &Failure{Sentence: "refused: a goal needs a title and a type — the store names the five; what it is for rides body_note"}, nil
	}
	rootID, f := r.resolvePersonRoot(in.Root)
	if f != nil {
		return nil, f, nil
	}
	in2 := store.NewGoal{ComponentID: rootID, EpicID: in.EpicID, Title: in.Title, Type: in.Type,
		BodyNote: in.BodyNote, SourceKind: "manual",
		By: store.Actor{Kind: by.Kind, ID: by.ID}, Submit: true}
	for _, c := range in.Components {
		comp, f := r.resolvePersonComponent(rootID, c.Component)
		if f != nil {
			return nil, f, nil
		}
		conf, f := parseConfidence(c.Confidence)
		if f != nil {
			return nil, f, nil
		}
		in2.Components = append(in2.Components, store.GoalComponent{ComponentID: comp.ID,
			Rationale: c.Rationale, EstimateUSD: c.EstimateUSD, Confidence: conf, Basis: c.Basis})
	}
	goal, rf, err := r.st.CreateGoal(in2)
	if err != nil {
		return nil, nil, err
	}
	if rf != nil {
		return nil, &Failure{Sentence: rf.Sentence}, nil
	}
	// Filed IN_REVIEW, exactly as the step's arm files it: authoring and
	// judging are different people, and the review queue is the proof.
	return map[string]any{"goal": goal}, nil, nil
}

func (r *RecordsService) taskCreateAsPerson(by Actor, input json.RawMessage) (any, *Failure, error) {
	var in struct {
		Component       string `json:"component"`
		Title           string `json:"title"`
		Type            string `json:"type"`
		BodyNote        string `json:"body_note"`
		Parent          string `json:"parent"`
		AssigneeKind    string `json:"assignee_kind"`
		Assignee        string `json:"assignee"`
		RequireApproval *bool  `json:"require_approval"`
	}
	if f, err := decode(input, &in); f != nil || err != nil {
		return nil, f, err
	}
	if in.Title == "" {
		return nil, &Failure{Sentence: "refused: a task needs a title — what it does, not that it exists"}, nil
	}
	if in.Component == "" {
		return nil, &Failure{Sentence: "refused: a hand-made task names its component — a task of nowhere cannot be filed (OL14)"}, nil
	}
	if in.Assignee != "" && in.AssigneeKind == "" {
		return nil, &Failure{Sentence: "refused: an assignee names its kind too — user:NAME or agent:NAME, not a bare name"}, nil
	}
	if in.AssigneeKind != "" && in.AssigneeKind != "user" && in.AssigneeKind != "agent" {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: assignee_kind %q is neither user nor agent", in.AssigneeKind)}, nil
	}
	comp, f := r.resolvePersonComponent("", in.Component)
	if f != nil {
		return nil, f, nil
	}
	in2 := store.NewManualTask{ComponentID: comp.ID, Title: in.Title, Type: in.Type,
		Body: in.BodyNote, ParentTaskID: in.Parent,
		AssigneeKind: in.AssigneeKind, Assignee: in.Assignee,
		By: store.Actor{Kind: by.Kind, ID: by.ID}}
	if in.RequireApproval != nil && *in.RequireApproval {
		in2.RequireApproval = true
	}
	t, rf, err := r.st.CreateManualTask(in2)
	if err != nil {
		return nil, nil, err
	}
	if rf != nil {
		return nil, &Failure{Sentence: rf.Sentence}, nil
	}
	if r.taskChanged != nil {
		r.taskChanged(t, "")
	}
	return t, nil, nil
}

// resolvePersonRoot resolves the root a person's goal hangs under by id or exact
// name — a "project" is a root component (S44-extra). Empty is a refusal, not
// a default: "the first root" is exactly the plausible wrong answer the
// resolver exists to prevent.
func (r *RecordsService) resolvePersonRoot(nameOrID string) (string, *Failure) {
	if nameOrID == "" {
		return "", &Failure{Sentence: "refused: a person's goal names its root — id or exact name (aeon component list shows them)"}
	}
	roots, err := r.st.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil {
		return "", &Failure{Sentence: fmt.Sprintf("refused: the roots could not be read (%v)", err)}
	}
	for _, p := range roots {
		if p.ID == nameOrID || p.Name == nameOrID {
			return p.ID, nil
		}
	}
	return "", &Failure{Sentence: fmt.Sprintf("refused: root %q is not one this daemon has — `aeon component list` shows what is here", nameOrID)}
}

// resolvePersonComponent resolves a component by root, id, exact name or
// slug — across every component when rootID (a root) is empty (a hand-made
// task is component-scoped and the component names its root by being it).
func (r *RecordsService) resolvePersonComponent(rootID, ref string) (store.Component, *Failure) {
	if ref == "" {
		return store.Component{}, &Failure{Sentence: "refused: a component is named by root, id, exact name or slug"}
	}
	if c, err := r.st.ComponentForRoot(ref); err == nil {
		if rootID == "" || c.ParentID == rootID {
			return c, nil
		}
		return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: the component at %s belongs to another root", ref)}
	}
	comps, err := r.st.ListComponents(store.ComponentFilter{})
	if err != nil {
		return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: the components could not be read (%v)", err)}
	}
	for _, c := range comps {
		if rootID != "" && c.ParentID != rootID {
			continue
		}
		if c.ID == ref || c.Name == ref || c.Slug == ref {
			return c, nil
		}
	}
	return store.Component{}, &Failure{Sentence: fmt.Sprintf("refused: component %q is not one this daemon has — `aeon component list` shows the roots", ref)}
}

// personLoopScope resolves a loop coordinate from the slug or id a person
// names — the coordinate a step has stamped and a person must say.
func (r *RecordsService) personLoopScope(loop, componentHint string) (store.Scope, *Failure) {
	if componentHint != "" {
		// The disambiguator a person carries by name: two components may run
		// the same slug (measured, db_test's two repoA/repoB "refactor"
		// loops), so a bare slug across the machine is a guess the daemon
		// refuses to make.
		c, err := r.st.ComponentForRoot(componentHint)
		if err != nil {
			return store.Scope{}, &Failure{Sentence: fmt.Sprintf("refused: --component %q is not a component root this daemon has", componentHint)}
		}
		loops, err := r.st.ListLoops(store.ComponentScope{ComponentRoot: c.Root})
		if err != nil {
			return store.Scope{}, &Failure{Sentence: fmt.Sprintf("refused: the loops could not be read (%v)", err)}
		}
		for _, l := range loops {
			if l.Slug == loop {
				return store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, nil
			}
		}
		return store.Scope{}, &Failure{Sentence: fmt.Sprintf("refused: no loop %q under component %s", loop, c.Root)}
	}
	if pin, err := r.st.ReadPinnedLoop(loop); err == nil {
		return store.Scope{LoopSlug: pin.Loop.Slug, ComponentRoot: pin.Loop.ComponentRoot}, nil
	}
	loops, err := r.st.ListLoops(store.ComponentScope{})
	if err != nil {
		return store.Scope{}, &Failure{Sentence: fmt.Sprintf("refused: the loops could not be read (%v)", err)}
	}
	for _, l := range loops {
		if l.Slug == loop {
			return store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}, nil
		}
	}
	return store.Scope{}, &Failure{Sentence: fmt.Sprintf("refused: loop %q is not a loop this daemon has — its slug or id names it", loop)}
}
