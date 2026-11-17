package policy

// The corpus reader: the one way corpus.json's bytes become Corpus data.
//
// T1's generator writes the file; the matcher's tests (T3), the corpus-driven
// evaluation (T6) and the seeding verb (T9) all read it through THIS loader,
// so "the corpus is the specification" is one shape agreed in one place rather
// than three ad-hoc decoders that can disagree about what a rule is.
//
// The surface takes BYTES, not a path, because of the purity claim in
// types.go (D5): nothing in this package reaches the filesystem, so no file
// here opens a file. The read lives in the caller — in the test, and in
// cmd/aeon for the seeding verb. The one exception is the go:embed below,
// which is not a filesystem read at run time: the bytes are compiled into the
// binary, which is exactly what lets `aeon policy seed-corpus` (T9) work from
// a built binary on a machine with no source tree, with no path to opencode/
// to get wrong.

import (
	"bytes"
	_ "embed" // required by the go:embed below; the bytes are a []byte, not an embed.FS
	"encoding/json"
	"io"
	"slices"
	"strings"
)

// Corpus is one reading of the generated corpus.json: per-agent rules in
// authored order, plus the structural properties the generator measured. It
// is DATA, not an authority — nothing here decides. A decision is Evaluate
// (evaluate.go).
type Corpus struct {
	// GeneratedBy names the emitter, verbatim from the file. A corpus from
	// anything else is not the corpus this slice measures.
	GeneratedBy string

	// Source names where the rules were read from at generation time
	// (opencode/agents, stable-relative). Provenance, carried through.
	Source string

	// Properties are the generator's own measurements. They are carried so a
	// test can disagree with them — the property tests in corpus_test.go
	// re-derive every one from Agents rather than trusting this block,
	// because agreement between a file and itself proves nothing.
	Properties CorpusProperties

	// Agents are the per-agent rule sets, in the order the file lists them.
	Agents []CorpusAgent
}

// CorpusAgent is one agent's rules as its operators authored them.
type CorpusAgent struct {
	Agent string

	// Rules are in AUTHORED order. The generator preserved the order the
	// .md file was written in and this loader preserves it again;
	// TestRuleOrderIsPreserved is what keeps both claims honest. Nothing
	// sorts this slice — the authoring order is the operator's statement,
	// and the decision rule that reads it is the engine's (PL4), a separate
	// claim from the order itself.
	Rules []CorpusRule
}

// CorpusRule is one pattern/verdict pair.
type CorpusRule struct {
	// Pattern is the shell-command pattern as authored: '*' is the only
	// wildcard the matcher (T3) will know.
	Pattern string

	// Verdict is the closed set, NOT a string: a corpus value outside the
	// vocabulary is a refusal here, never a permission somewhere downstream.
	Verdict Action
}

// CorpusProperties mirrors the properties block the generator measured. It is
// carried, not consulted: the engine decides from Agents alone.
type CorpusProperties struct {
	WildcardChars       []string
	HasMidStarPattern   bool
	HasExactPattern     bool
	ConflictingPatterns []string
}

// The wire shapes. Separate from the domain types so a JSON key renamed by a
// future generator is a change here and not a change to what the package
// exports. Pointers and raw presence distinguish "absent" from "zero" — a
// corpus with no properties block is a shape the generator never emitted, and
// ParseCorpus says so rather than defaulting it.
type (
	corpusWire struct {
		GeneratedBy string            `json:"generated_by"`
		Source      string            `json:"source"`
		Properties  *corpusPropsWire  `json:"properties"`
		Agents      []corpusAgentWire `json:"agents"`
	}
	corpusPropsWire struct {
		WildcardChars       []string `json:"wildcard_chars"`
		HasMidStarPattern   bool     `json:"has_mid_star_pattern"`
		HasExactPattern     bool     `json:"has_exact_pattern"`
		ConflictingPatterns []string `json:"conflicting_patterns"`
	}
	corpusAgentWire struct {
		Agent string           `json:"agent"`
		Rules []corpusRuleWire `json:"rules"`
	}
	corpusRuleWire struct {
		Pattern string `json:"pattern"`
		Verdict string `json:"verdict"`
	}
)

// ParseCorpus reads corpus.json's bytes, refusing (a *Reject naming the field
// and the value it refused, per DB19) any shape the generator did not emit:
// an unknown verdict, an empty pattern, a missing or empty agents array, an
// agent with no rules, a duplicated agent name, a missing properties block,
// an unknown field at any level.
//
// Strictness is the point. The corpus is the specification the matcher and
// the seeding verb are held to (T3, T6, T9); a loader that tolerated a shape
// the generator does not write would let one reader's private tolerance become
// the next reader's missing rule. The zero Corpus comes back with every
// refusal — no partial data to misuse.
func ParseCorpus(raw []byte) (Corpus, *Reject) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w corpusWire
	if err := dec.Decode(&w); err != nil {
		return Corpus{}, rejectf("corpus: %v", err)
	}
	// More() lies when the trailing bytes parse as no token at all, so the
	// check is explicit: anything after the first document — a second one,
	// stray digits, half a file — is refused. A corpus that is not exactly
	// one JSON document is a shape the generator did not emit.
	if tok, err := dec.Token(); err != io.EOF {
		return Corpus{}, rejectf("corpus: trailing data after the first JSON document (%v)", tok)
	}

	if w.GeneratedBy == "" {
		return Corpus{}, rejectf("corpus: empty generated_by %q", w.GeneratedBy)
	}
	if w.Source == "" {
		return Corpus{}, rejectf("corpus: empty source %q", w.Source)
	}
	if w.Properties == nil {
		return Corpus{}, rejectf("corpus: missing properties — the generator always measures them")
	}
	if len(w.Agents) == 0 {
		return Corpus{}, rejectf("corpus: missing or empty agents %v", w.Agents)
	}

	c := Corpus{
		GeneratedBy: w.GeneratedBy,
		Source:      w.Source,
		Properties: CorpusProperties{
			WildcardChars:       w.Properties.WildcardChars,
			HasMidStarPattern:   w.Properties.HasMidStarPattern,
			HasExactPattern:     w.Properties.HasExactPattern,
			ConflictingPatterns: w.Properties.ConflictingPatterns,
		},
	}

	seen := make(map[string]bool, len(w.Agents))
	for _, aw := range w.Agents {
		if aw.Agent == "" {
			return Corpus{}, rejectf("corpus: empty agent name %q", aw.Agent)
		}
		if seen[aw.Agent] {
			return Corpus{}, rejectf("corpus: duplicate agent %q", aw.Agent)
		}
		seen[aw.Agent] = true

		if len(aw.Rules) == 0 {
			return Corpus{}, rejectf("corpus: agent %q has no rules", aw.Agent)
		}
		rules := make([]CorpusRule, 0, len(aw.Rules))
		for _, rw := range aw.Rules {
			if strings.TrimSpace(rw.Pattern) == "" {
				return Corpus{}, rejectf("corpus: agent %q: empty pattern %q", aw.Agent, rw.Pattern)
			}
			verdict, rej := parseCorpusVerdict(rw.Verdict)
			if rej != nil {
				return Corpus{}, rejectf("corpus: agent %q: %v", aw.Agent, rej)
			}
			rules = append(rules, CorpusRule{Pattern: rw.Pattern, Verdict: verdict})
		}
		c.Agents = append(c.Agents, CorpusAgent{Agent: aw.Agent, Rules: rules})
	}
	return c, nil
}

// parseCorpusVerdict maps the JSON verdict onto the Action vocabulary: the
// closed set from types.go, and nothing else. Since migration 0019 admitted
// the third action, the corpus's wire values and the engine's vocabulary are
// the SAME set, so this loop is ParseAction's loop with the field name the
// file actually spells. Anything outside is refused naming the value — the
// same DB19 discipline as ParseAction.
func parseCorpusVerdict(s string) (Action, *Reject) {
	for _, a := range AllActions() {
		if string(a) == s {
			return a, nil
		}
	}
	return "", rejectf("unknown verdict %q", s)
}

// RulesFor returns one agent's rules in authored order and reports false when
// the agent is absent — "no rules" and "no such agent" are different answers
// (P31). The result is a copy: a caller sorting or truncating what it got
// tells it about its own copy, nothing about the corpus.
func (c Corpus) RulesFor(agent string) ([]CorpusRule, bool) {
	for _, a := range c.Agents {
		if a.Agent == agent {
			return append([]CorpusRule(nil), a.Rules...), true
		}
	}
	return nil, false
}

// DistinctPatterns lists every pattern the corpus uses, sorted and
// de-duplicated. Tests enumerate over this. It never encodes a count.
func (c Corpus) DistinctPatterns() []string {
	seen := make(map[string]bool)
	var out []string
	for _, a := range c.Agents {
		for _, r := range a.Rules {
			if !seen[r.Pattern] {
				seen[r.Pattern] = true
				out = append(out, r.Pattern)
			}
		}
	}
	slices.Sort(out)
	return out
}

// embeddedCorpus is the committed corpus, compiled into the binary. The path
// is the one T1 writes and T6 and T9 cite; go:embed accepts testdata (the
// documented exclusion is names beginning '.' or '_', not this directory).
//
//go:embed testdata/corpus.json
var embeddedCorpus []byte

// EmbeddedCorpus is the committed corpus, compiled in, so that
// `aeon policy seed-corpus` (T9) works from a built binary on a machine with
// no source tree, and so the CLI never needs a path to opencode/.
//
// A fresh copy comes back on every call: embedded bytes live in read-only
// data, so handing one out would leave the caller one mutation away from a
// SIGSEGV; a copy keeps every caller free to do what it likes with its own.
func EmbeddedCorpus() []byte {
	return append([]byte(nil), embeddedCorpus...)
}
