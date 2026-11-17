package protocol

// scorecard.go — S63 T5 (RT10, RT3, P34): the scorecard doors.
//
// Five reads over ONE seam, registered together or absent (V13): the
// daemon answers them from `internal/scorecard`, and this file knows that
// package by NOTHING — the seam's inputs and results are wire structs
// defined here (the local-interface pattern every plane sets), because
// RT3.1's import law is what makes "a score never reaches dispatch"
// structural: the packages that dispatch must never import learn nothing
// of this plane, and this plane learns nothing of them.
//
// Three rules the wire shape itself carries:
//
//   - Every result carries `statement` — RT3's sentence, the SAME
//     constant on every card, every digest, every comparison
//     (`ScorecardStatement`, pinned by TestScorecardResultsAlwaysCarry-
//     TheStatement). A score reads as a measurement on every door or it
//     reads as an instruction on none of them.
//   - No identity in params (RT3.1/Do-NOT): `actor` and its vocabulary
//     are refused by key before decoding. A card is about a subject
//     named in the params; who asks is the connection's, and it changes
//     no number.
//   - Every metric answers with `n` and a `state` (RT7/RT9). The doors
//     never strip an unpopulated metric to make a page prettier; a
//     `too-few` metric with its n is more honest than a missing row.
//
// `ErrScorecardRefused` (-31962, T0's block) is the plane's one refusal
// voice — unknown subject, bad window, the service's own sentence in
// `what`, never a paraphrase.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// --- the method names -------------------------------------------------------

const (
	// MethodScorecardAgent is one agent's card, keyed by SLUG
	// (scorecard.agent {agent, since?, until?, last?, rootId?}).
	MethodScorecardAgent MethodName = "scorecard.agent"
	// MethodScorecardBlueprint is one blueprint's card, keyed by id.
	MethodScorecardBlueprint MethodName = "scorecard.blueprint"
	// MethodScorecardComponent is one component's card, id-resolved
	// through the tree, optionally rolled up over its children.
	MethodScorecardComponent MethodName = "scorecard.component"
	// MethodScorecardCompare answers two cards over two windows — the
	// two numbers a person reads; NO delta is computed or carried
	// (RT8: subtraction invents a derived metric the vocabulary never
	// published).
	MethodScorecardCompare MethodName = "scorecard.compare"
	// MethodScorecardVocabulary publishes the closed sets: the metrics
	// with their descriptions, the states, MinSample, the window
	// presets, the milestone ladder (RT7).
	MethodScorecardVocabulary MethodName = "scorecard.vocabulary"
)

// scorecardMethodNames is the plane's exact five.
func scorecardMethodNames() []MethodName {
	return []MethodName{
		MethodScorecardAgent, MethodScorecardBlueprint, MethodScorecardComponent,
		MethodScorecardCompare, MethodScorecardVocabulary,
	}
}

// ScorecardStatement is RT3's sentence, verbatim, on the `statement`
// field of every result of this plane (and the digest plane beside it).
// It is ONE constant: a client that renders it renders the rule, and a
// second copy of the sentence is a second rule.
const ScorecardStatement = "A score is a measurement of what happened. It does not change what any agent, blueprint or project is allowed or asked to do."

// --- the params (RT10's table) -----------------------------------------------

// ScorecardWindow is the optional window on every card door. Empty Since
// means the last 30 days (RT8) — resolved by the service, echoed in the
// card. Last caps the unit to the last N loops.
type ScorecardWindow struct {
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	Last  int    `json:"last,omitempty"`
}

type ScorecardAgentParams struct {
	Agent  string `json:"agent"`
	RootID string `json:"rootId,omitempty"`
	ScorecardWindow
}

type ScorecardBlueprintParams struct {
	Blueprint string `json:"blueprint"`
	RootID    string `json:"rootId,omitempty"`
	ScorecardWindow
}

type ScorecardComponentParams struct {
	ID              string `json:"id"`
	IncludeChildren bool   `json:"includeChildren,omitempty"`
	ScorecardWindow
}

type ScorecardCompareParams struct {
	Kind    string          `json:"kind"`
	Subject string          `json:"subject"`
	WindowA ScorecardWindow `json:"windowA"`
	WindowB ScorecardWindow `json:"windowB"`
}

// --- the results (wire views of scorecard's types; RT3.1 keeps the
// packages apart, so these structs MIRROR and a daemon-side test pins the
// mirror) ---------------------------------------------------------------------

// MetricWire is one metric: the value (NULL for cannot-say AND for
// too-few — RT7), the n behind it, the state word, an optional note, and
// the detail map (n counts for a floor, p90 for a median — facts, never
// a derived headline).
type MetricWire struct {
	Value  *float64       `json:"value"`
	N      int            `json:"n"`
	State  string         `json:"state"`
	Note   string         `json:"note,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

type SubjectWire struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	IncludeChildren bool   `json:"includeChildren,omitempty"`
	RootID          string `json:"rootId,omitempty"`
}

type WindowWire struct {
	Since string `json:"since"`
	Until string `json:"until"`
	Last  int    `json:"last,omitempty"`
}

// CoverageWire is RT9's honesty strip. It rides EVERY card even when the
// subject has nothing recorded — a card without coverage is a number
// without a denominator.
type CoverageWire struct {
	RecordedSince    string `json:"recordedSince"`
	LoopsInHistory   int    `json:"loopsInHistory"`
	LoopsRecorded    int    `json:"loopsRecorded"`
	LoopsNotRecorded int    `json:"loopsNotRecorded"`
}

type BreakdownWire struct {
	Component string `json:"component"`
	Root      string `json:"root"`
	Loops     int    `json:"loops"`
	Done      int    `json:"done"`
	Blocked   int    `json:"blocked"`
}

type CardWire struct {
	Subject   SubjectWire           `json:"subject"`
	Window    WindowWire            `json:"window"`
	Coverage  CoverageWire          `json:"coverage"`
	Statement string                `json:"statement"`
	Metrics   map[string]MetricWire `json:"metrics"`
}

type ScorecardCardResult struct {
	Card      CardWire `json:"card"`
	Statement string   `json:"statement"`
}

type ScorecardComponentResult struct {
	Card      CardWire        `json:"card"`
	Breakdown []BreakdownWire `json:"breakdown"`
	Statement string          `json:"statement"`
}

// ScorecardCompareResult is two cards and the two windows they were read
// over — nothing else. There is no delta field to fill (RT8).
type ScorecardCompareResult struct {
	A         CardWire        `json:"a"`
	B         CardWire        `json:"b"`
	WindowA   ScorecardWindow `json:"windowA"`
	WindowB   ScorecardWindow `json:"windowB"`
	Statement string          `json:"statement"`
}

type MetricInfoWire struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

type WindowPresetWire struct {
	Name string `json:"name"`
	Days int    `json:"days"`
}

type ScorecardVocabularyResult struct {
	Metrics    []MetricInfoWire   `json:"metrics"`
	States     []string           `json:"states"`
	MinSample  int                `json:"minSample"`
	Windows    []WindowPresetWire `json:"windows"`
	Milestones []int              `json:"milestones"`
	Statement  string             `json:"statement"`
}

// --- the seam -----------------------------------------------------------------

// ScorecardService is the daemon's read side over internal/scorecard.
// Every handler answers with RT3's statement already in place; a refusal
// is an *Error of this plane (ErrScorecardRefused) with its own sentence.
type ScorecardService interface {
	Agent(ctx context.Context, p ScorecardAgentParams) (ScorecardCardResult, error)
	Blueprint(ctx context.Context, p ScorecardBlueprintParams) (ScorecardCardResult, error)
	Component(ctx context.Context, p ScorecardComponentParams) (ScorecardComponentResult, error)
	Compare(ctx context.Context, p ScorecardCompareParams) (ScorecardCompareResult, error)
	Vocabulary(ctx context.Context) (ScorecardVocabularyResult, error)
}

// --- registration ---------------------------------------------------------------

// RegisterScorecard registers the five reads over a non-nil seam; a nil
// seam means the build has no store, and the methods are ABSENT, never
// registered-and-refusing (V13). Returns whether they are in.
func (t *Table) RegisterScorecard() bool {
	if t.Scorecard == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodScorecardAgent, RequiresInitialize: true, Handle: t.handleScorecardAgent},
		{Name: MethodScorecardBlueprint, RequiresInitialize: true, Handle: t.handleScorecardBlueprint},
		{Name: MethodScorecardComponent, RequiresInitialize: true, Handle: t.handleScorecardComponent},
		{Name: MethodScorecardCompare, RequiresInitialize: true, Handle: t.handleScorecardCompare},
		{Name: MethodScorecardVocabulary, RequiresInitialize: true, Handle: t.handleScorecardVocabulary},
	} {
		t.register(m)
	}
	return true
}

// --- the handlers -------------------------------------------------------------

// scorecardIdentityKeys names the keys no scorecard param may carry
// (Do-NOT: no actor, no identity). A card is about its subject; the
// caller's identity changes no number on it.
var scorecardIdentityKeys = []string{"actor", "actorKind", "identity", "peer", "uid"}

func rejectScorecardIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams will say it better
	}
	for _, k := range scorecardIdentityKeys {
		if _, ok := bag[k]; ok {
			return ErrBadParams{Err: fmt.Errorf(
				"%s takes no %q — a card is about the subject it names; who asks changes no number", method, k)}
		}
	}
	return nil
}

func (t *Table) scorecardSeam() error {
	if t.Scorecard == nil {
		return ErrScorecardRefused.WithWhat("no scorecard service is wired — this daemon has no store to read outcomes from")
	}
	return nil
}

func (t *Table) handleScorecardAgent(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectScorecardIdentityKeys(string(MethodScorecardAgent), raw); err != nil {
		return nil, err
	}
	if err := t.scorecardSeam(); err != nil {
		return nil, err
	}
	var p ScorecardAgentParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Agent) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scorecard.agent needs an agent slug — cards are keyed by slug, never by a display name")}
	}
	return t.Scorecard.Agent(ctx, p)
}

func (t *Table) handleScorecardBlueprint(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectScorecardIdentityKeys(string(MethodScorecardBlueprint), raw); err != nil {
		return nil, err
	}
	if err := t.scorecardSeam(); err != nil {
		return nil, err
	}
	var p ScorecardBlueprintParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Blueprint) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scorecard.blueprint needs a blueprint id")}
	}
	return t.Scorecard.Blueprint(ctx, p)
}

func (t *Table) handleScorecardComponent(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectScorecardIdentityKeys(string(MethodScorecardComponent), raw); err != nil {
		return nil, err
	}
	if err := t.scorecardSeam(); err != nil {
		return nil, err
	}
	var p ScorecardComponentParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scorecard.component needs a component id (an unregistered id is read as a component_root verbatim)")}
	}
	return t.Scorecard.Component(ctx, p)
}

func (t *Table) handleScorecardCompare(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectScorecardIdentityKeys(string(MethodScorecardCompare), raw); err != nil {
		return nil, err
	}
	if err := t.scorecardSeam(); err != nil {
		return nil, err
	}
	var p ScorecardCompareParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	switch p.Kind {
	case "component", "agent", "blueprint":
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("scorecard.compare kind %q — the three subjects are component, agent and blueprint", p.Kind)}
	}
	if strings.TrimSpace(p.Subject) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scorecard.compare needs the subject the two windows are read over")}
	}
	return t.Scorecard.Compare(ctx, p)
}

func (t *Table) handleScorecardVocabulary(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if err := rejectScorecardIdentityKeys(string(MethodScorecardVocabulary), raw); err != nil {
		return nil, err
	}
	if err := t.scorecardSeam(); err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		var bag map[string]json.RawMessage
		if err := json.Unmarshal(raw, &bag); err == nil && len(bag) > 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("scorecard.vocabulary takes no params — the published sets are the same for every caller")}
		}
	}
	return t.Scorecard.Vocabulary(ctx)
}
