// Package scorecard turns recorded outcome facts into cards: derived
// reads with sample sizes, honest states and floors (S63, RT7–RT9).
//
// It is a PURE READ package (N8): no writes, no cache, no stored score —
// it reads the store's retro surface and history's merged projection,
// computes, and forgets. Two reads of the same window agree because
// they redo the same work, not because anything was kept (T-S1).
//
// Every metric carries its OWN sample count n and a state from a closed
// vocabulary; a number appears only when the state is `ok` or `partial`,
// and the package NEVER answers 0 where the truth is "unknown" (P31).
// No ranking function exists (RT11): the package exports no sort-by-
// score, no "best", no "worst" — a card is read by a person, and
// dispatch never consumes one (RT1).
package scorecard

import (
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// MinSample is the floor below which a metric refuses to state a number
// (RT7, OD6). A named constant, surfaced through the vocabulary; its
// later adoption as an S44 setting is T5's, not this package's.
const MinSample = 5

// State is the closed vocabulary every metric answers with (RT7).
type State string

const (
	// StateOk: the number stands, n ≥ MinSample, nothing was skipped.
	StateOk State = "ok"
	// StateTooFew: 0 < n < MinSample — the computation was done and
	// refused: value stays null, the sample cannot carry the claim.
	StateTooFew State = "too-few"
	// StateCannotSay: no samples, or a prerequisite slice is absent.
	// The note says which.
	StateCannotSay State = "cannot-say"
	// StatePartial: the value is stated but rests on floors or rows that
	// could not be fully read; the note names the count and the reason.
	StatePartial State = "partial"
)

// States is the vocabulary in card order.
func States() []State {
	return []State{StateOk, StateTooFew, StateCannotSay, StatePartial}
}

// Metric is one measured thing: value, the sample size its OWN
// denominator used (rule 2 — never the card's loop count), the state,
// and the why/extra of a partial or a refusal.
type Metric struct {
	Value  *float64       `json:"value"`
	N      int            `json:"n"`
	State  State          `json:"state"`
	Note   string         `json:"note,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// SubjectKind is what a card is about (RT7's three subjects).
type SubjectKind string

const (
	SubjectComponent SubjectKind = "component"
	SubjectAgent     SubjectKind = "agent"
	SubjectBlueprint SubjectKind = "blueprint"
)

// Subject names the card's subject. For a component, ID is the
// component's id (resolved through the S44-extra tree reads to its
// repository root); an unregistered id is treated AS a component_root
// verbatim — the honest reading of "the card for this directory".
// IncludeChildren is honoured for components only (RT8's tree roll-up).
type Subject struct {
	Kind            SubjectKind `json:"kind"`
	ID              string      `json:"id"`
	IncludeChildren bool        `json:"includeChildren,omitempty"`
	// RootID narrows an agent or blueprint card to one component's
	// subtree (RT10's optional rootId). Empty means workspace-wide — the
	// card a person asks for when they name only the agent or blueprint.
	RootID string `json:"rootId,omitempty"`
}

// Window is the requested window; empty Since means "the last 30 days"
// (RT8) and is RESOLVED before computing, so every card echoes the
// effective window. LastN caps the unit to the last N loops (RT8).
type Window struct {
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	LastN int    `json:"last,omitempty"`
}

// Coverage is the honesty strip on every card (RT9): "we only have facts
// for 12 of 47 loops" must be ON the card, never inferable only from a
// person noticing the number is small.
type Coverage struct {
	RecordedSince    string `json:"recordedSince"`
	LoopsInHistory   int    `json:"loopsInHistory"`
	LoopsRecorded    int    `json:"loopsRecorded"`
	LoopsNotRecorded int    `json:"loopsNotRecorded"`
}

// BreakdownEntry is one child's counts — loops, done, blocked ONLY
// (RT8: no scores per child, RT11: nothing to rank).
type BreakdownEntry struct {
	Component string `json:"component"`
	Root      string `json:"root"`
	Loops     int    `json:"loops"`
	Done      int    `json:"done"`
	Blocked   int    `json:"blocked"`
}

// Card is the answer: subject, effective window, coverage, metrics, and
// (components with children) the breakdown. A subject with no facts
// still gets a card — empty metrics carry cannot-say states, and the
// Statement says so (RT7: not an error, not zeros).
type Card struct {
	Subject   Subject           `json:"subject"`
	Window    Window            `json:"window"`
	Coverage  Coverage          `json:"coverage"`
	Statement string            `json:"statement"`
	Metrics   map[string]Metric `json:"metrics"`
	Breakdown []BreakdownEntry  `json:"breakdown,omitempty"`
}

// Deps is the package's whole world: the store (reads only — the
// compiler cannot see this, the guard test can) and the clock, so tests
// pin the default window without sleeping. A nil Now is time.Now.
type Deps struct {
	S   *store.Store
	Now func() time.Time
}

// nowOrDefault resolves the clock.
func (d Deps) nowOrDefault() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}
