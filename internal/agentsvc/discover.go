package agentsvc

import (
	"os"
	"sort"
	"strings"
)

// discover.go — S22's read surface, and the shape A13 demands of it.
//
// The obvious-and-wrong next step is *"find an agent that can do X, then
// call it."* A13 refused that shape for the handler registry — no generic
// fallback, because "a generic handler writing Elixir produces something
// plausible, the loop reports ok, and the badness surfaces three steps later
// attributed to the model." Proximity-based dispatch is that failure with a
// better excuse: a near match is not a capability claim, it is a sentence
// that looked nearby. The line this package holds, in the design's words:
//
//	**proximity picks what to read about. A NAME picks what to call.**
//
// So discovery here is ENUMERATION ONLY: every loadable card with its tags
// and examples — the material a reader (human at a screen now, an index at
// S22 later) judges fit from. Nothing in this file numbers, orders-by-fit,
// vectorises or picks, and a grep test holds that no exported path leads
// from a closeness figure to Invoke: candidates come out of here, a NAME
// goes into the ask. (The file deliberately avoids the forbidden vocabulary
// even in prose — the grep is the test, and this comment is why the grep
// does not strip comments.)
//
// AS9's honesty rides along: what is enumerable is exactly what is
// invokable — cards enumerated through the same load path Invoke uses
// (LoadCardFile: signature over exact bytes, tamper → excluded), so
// discovery cannot advertise a card the invocation door will refuse. A
// broken card is invisible here and unhonoured there; one truth, read once.

// SkillCandidate is one skill as discovery renders it: the identity fields
// plus the two the reader actually judges fit from — tags and examples
// (T2: examples are what make a skill discoverable). Input/output modes are
// carried because a caller that chooses by name still has to choose a mode
// that the skill answers in.
type SkillCandidate struct {
	Agent       string   `json:"agent"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// CandidateSkillRef is the invocation spelling of a discovered skill — the
// same canonical form the policy door reads (callee/skill). Discovery hands
// over a NAME, spelled the one way that works at the door.
func (sc SkillCandidate) CandidateSkillRef() string { return SkillRef(sc.Agent, sc.ID) }

// AgentCandidate is a card as discovery renders it. The card-level fields
// carried are the ones a reader or an index needs to answer "who is this";
// security bytes are NOT re-exposed here — a projection that reshapes the
// opaque hint into something listable is how a hint starts looking like
// authority (AS8/AS12's rule, read forward).
type AgentCandidate struct {
	Agent           string             `json:"agent"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Version         string             `json:"version"`
	Provider        string             `json:"provider,omitempty"`
	DefaultInModes  []string           `json:"defaultInputModes,omitempty"`
	DefaultOutModes []string           `json:"defaultOutputModes,omitempty"`
	Skills          []SkillCandidate   `json:"skills"`
	Capabilities    *AgentCapabilities `json:"capabilities,omitempty"`
}

// Discover enumerates every loadable card, deterministically ordered. It
// returns CANDIDATES — plural, unordered by merit, because nothing here has
// a merit to order by (scope 3: a function returning *the* agent for a
// request makes every caller of it a dispatch). Cards that fail the load
// path are excluded and named in refuses, never silently dropped: an
// operator who published a broken card deserves to find out from the
// listing, not from its absence.
type Discovery struct {
	Candidates []AgentCandidate
	Refuses    []string // "<agent>: <why>" — broken cards, excluded from Candidates
}

func (cs *CardStore) Discover() Discovery {
	entries, err := os.ReadDir(cs.dir)
	d := Discovery{Candidates: []AgentCandidate{}}
	if err != nil {
		d.Refuses = append(d.Refuses, cs.dir+": "+err.Error())
		return d
	}
	var names []string
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(en.Name(), ".json"))
	}
	sort.Strings(names) // deterministic listing: same directory, same answer
	for _, name := range names {
		card, err := cs.Card(name)
		if err != nil || card == nil {
			why := "card could not be loaded"
			if err != nil {
				why = err.Error()
			}
			d.Refuses = append(d.Refuses, name+": "+why)
			continue
		}
		// The same door Invoke reads through — LoadCardFile's checks already
		// ran inside cs.Card; enumeration borrows that verdict, it does not
		// re-implement it (one verification path, read once).
		c := AgentCandidate{
			Agent: name, Name: card.Name, Description: card.Description, Version: card.Version,
			DefaultInModes:  append([]string(nil), card.DefaultInputModes...),
			DefaultOutModes: append([]string(nil), card.DefaultOutputModes...),
			Capabilities:    card.Capabilities,
			Skills:          []SkillCandidate{},
		}
		if card.Provider != nil {
			c.Provider = card.Provider.Organization
		}
		for _, sk := range card.Skills {
			c.Skills = append(c.Skills, SkillCandidate{
				Agent: name, ID: sk.ID, Name: sk.Name, Description: sk.Description,
				Tags: append([]string(nil), sk.Tags...), Examples: append([]string(nil), sk.Examples...),
				InputModes: append([]string(nil), sk.InputModes...), OutputModes: append([]string(nil), sk.OutputModes...),
			})
		}
		d.Candidates = append(d.Candidates, c)
	}
	return d
}

// SkillCandidates flattens to the unit S22 will index on: one row per
// invokable skill, agent-qualified, name-spelled. It is still a LIST —
// the flattening is for reading, never for choosing.
func (d Discovery) SkillCandidates() []SkillCandidate {
	var out []SkillCandidate
	for _, a := range d.Candidates {
		out = append(out, a.Skills...)
	}
	return out
}

// (This file deliberately carries none of the closeness vocabulary the grep
// test forbids — not even in prose. That is not a style rule: the grep is
// the test, comments included, and a comment containing the forbidden shape
// would trip it as surely as code — this task measured exactly that.)
