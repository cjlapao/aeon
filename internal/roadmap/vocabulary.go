package roadmap

// S50 T2 (RM6) — the vocabulary read's source. The legal next states are
// READ from AllowedTransitions(), never restated: a vocabulary file that
// copies the machine's edges is a second truth about the machine, and the
// day they disagree the forms render an illegal move. If a test mutates
// the transitions table, this output changes — that is the pin.

// VocabularyAnswer is everything a form needs to render, so the UI invents no
// words and no client hard-codes an edge the daemon does not allow.
type VocabularyAnswer struct {
	Statuses      []StatusTransition `json:"statuses"`
	Priorities    []string           `json:"priorities"`
	Provenances   []string           `json:"provenances"`
	EstimateRule  string             `json:"estimateRule"`
	PathRule      string             `json:"pathRule"`
	PathEditable  []Status           `json:"pathEditableStatuses"`
	TitleMaxBytes int                `json:"titleMaxBytes"`
	EpicMaxBytes  int                `json:"epicMaxBytes"`
	// DispatchStatement is the fixed authority sentence every surface
	// prints BEFORE a dispatch (S50 T6, RM9 — the CLI) and shows on its
	// dispatch confirm (RM10 — the UI). It is a const of THIS package so
	// the sentence cannot differ per surface; clients fill nothing in it
	// except placing it beside the item's own Describe sentence.
	DispatchStatement string `json:"dispatchStatement"`
}

// StatusTransition is one status and the moves the machine allows from it.
type StatusTransition struct {
	Status   Status   `json:"status"`
	Next     []Status `json:"next"`
	Terminal bool     `json:"terminal"`
}

// Vocabulary builds the answer from the live machine, not a copy.
func Vocabulary() VocabularyAnswer {
	table := AllowedTransitions()
	v := VocabularyAnswer{
		Statuses:      make([]StatusTransition, 0, 8),
		Priorities:    []string{string(PriorityKnown), string(PriorityAssumed), string(PriorityUnknown)},
		Provenances:   []string{string(ProvenanceAuthored), string(ProvenanceTriggered), string(ProvenanceTriaged)},
		EstimateRule:  "an estimate is the whole range and its basis together, or absent — never a bare number (D3/D8)",
		PathRule:      "artifact paths must be absolute under the item's component_root, and are editable only while the item has not started (statuses idea, ideating, brief-agreed, planned, ready); from running they are history",
		PathEditable:  PathEditableStatuses(),
		TitleMaxBytes: TitleMaxBytes,
		EpicMaxBytes:  EpicMaxBytes,
		DispatchStatement: "This starts agents with no one present unless the loop is declared attended. " +
			"Outward actions follow the standing policy — nothing here widens it. The rung moves only when the " +
			"dispatched work itself lands; a failed run leaves the item exactly where it stands. It is about: <item>",
	}
	for _, s := range Statuses() {
		next := append([]Status(nil), table[s]...)
		v.Statuses = append(v.Statuses, StatusTransition{
			Status:   s,
			Next:     next,
			Terminal: len(next) == 0,
		})
	}
	return v
}
