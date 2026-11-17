package policy

// ════════════════════════════════════════════════════════════════════════════
// The one formatter of what a policy DOES (S45 T2, PM5; D9's policy-side twin
// of Verdict.Sentence).
//
// # One sentence, fixed templates, everywhere
//
// `policy.list` rows, `policy.get`, the app's detail drawer and (from T8) the
// CLI all print `Describe(p)` VERBATIM. A second renderer is a second truth —
// the exact failure verdict.go exists to prevent for decisions — so the
// grammar is closed here and the surfaces add nothing. The templates are the
// design's (PM5):
//
//	tool_access     "<allows|denies|asks before> tool %q for <target phrase>"
//	command_access  "<allows|denies|asks before> commands matching %q for …"
//	route_access    "<verb> routes matching %q for …"
//	skill_access    "<verb> skill %q for …"
//	trigger_access  "<verb> trigger %q for …"
//	effect_class    "<verb> effect %q for …"
//	cost_cap        "caps %q spend at %s for <target phrase>"
//
// plus ` — expires <RFC3339>` when NotAfter is set and ` — authored by
// <source>` always. The per-type clause is built beside the validator that
// reads that spec (the spec struct and the sentence about it are one thing
// and must not live apart — the same rule spec_stubs states for shape
// strings); this file owns the shared half: the verb, the target phrase, the
// two suffixes, and the fallbacks.
//
// # Never assembled from raw text
//
// Every value that reaches a sentence travels through %q (SEC-033/SEC-046):
// a crafted toolName cannot smuggle a newline, an ESC sequence or a BiDi
// override into an operator's console — the sentence is exactly as hostile as
// strconv.Quote allows, which is inert. And a row whose members are not
// closed-set values, whose spec does not decode, or whose spec contradicts
// its action gets NO sentence about its content at all: the fixed fallback,
// naming the type and stating that nothing enforces it. Total, like the
// evaluator — Describe cannot panic and cannot be made to quote raw bytes.
//
// # No switch on the type (PL3)
//
// Dispatch is the registry: Describe asks ValidatorFor and the validator
// answers through the describer seam. The five types whose validator is the
// shared stubValidator contribute their clause builders from their own
// files' init (sibling code beside their registration), never from a branch
// here — a type arrives by registering, and the describing arrives with it.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// describer is the describing half of a validator — the seam Describe
// dispatches through. Unexported on purpose: only this package's validators
// (the registry's init-only rule) can answer, so a sentence always comes from
// the code that validates the spec it describes.
type describer interface {
	describe(p Policy) string
}

// placeholderDescribes and placeholderFields carry the clause builders and
// field descriptors for the types whose registered validator is the shared
// stubValidator. Written only at code time — each per-type file adds its own
// entry from its own init beside its own registration, the discipline
// RegisterType is held to (SEC-010) — and read unsynchronised thereafter.
var (
	placeholderDescribes = map[PolicyType]func(Policy) string{}
	placeholderFields    = map[PolicyType]func() []SpecField{}
)

// registerPlaceholderDescribing is how a type whose validator is the shared
// stubValidator contributes its sentence and its field descriptors: called
// from that type's own init, in its own file, never at runtime.
func registerPlaceholderDescribing(typ PolicyType, clause func(Policy) string, fields func() []SpecField) {
	placeholderDescribes[typ] = clause
	placeholderFields[typ] = fields
}

// Describe renders the one sentence for one policy. Every surface that shows
// what a rule DOES prints this, whole (D9); nothing above this file
// assembles a description from spec fields, and nothing below it refuses —
// the fallback is a sentence too, so the UI never renders an empty string
// beside a real rule.
func Describe(p Policy) string {
	v, ok := ValidatorFor(p.Type)
	if !ok {
		return unregisteredSentence(p.Type)
	}
	if d, ok := v.(describer); ok {
		return d.describe(p)
	}
	// A validator from outside this package satisfies neither describer nor
	// the registry's init-only rule; the answer is the same as for no
	// validator at all: say nothing enforces it, invent nothing.
	return unregisteredSentence(p.Type)
}

// unregisteredSentence is the fixed fallback for a type nobody registered:
// the design's sentence, verbatim, with the offending type %q-quoted so the
// refusal names what it refuses (DB19) without echoing raw bytes.
func unregisteredSentence(typ PolicyType) string {
	return fmt.Sprintf("a policy of an unregistered type (%q) — nothing enforces it", string(typ))
}

// unreadableSentence is the fixed fallback for a row that could not have come
// through the write boundary: an action or source outside the closed set, a
// target kind nobody compiled, a spec that does not decode, a spec whose
// state contradicts the row's action. The evaluator refuses to MATCH such a
// row (the inert-toward-deny discipline every spec file keeps); the formatter
// refuses to describe it. Naming the type keeps the answer actionable;
// quoting the garbage would put the garbage in the console it warns about.
func unreadableSentence(typ PolicyType) string {
	return fmt.Sprintf("a policy of type %q whose row is not one the engine reads — nothing enforces it", string(typ))
}

// describeActions renders the three closed actions as the sentence's verb.
// Absence from this map IS the false: an action outside the closed set gets
// the fallback, never a guessed verb.
var describeActions = map[Action]string{
	Allow: "allows",
	Deny:  "denies",
	Ask:   "asks before",
}

// describeReadable is the gate every clause builder passes its row through
// first: the action renders as a closed verb, the source is a closed member
// (so `authored by <source>` can never print an invention), and the target
// kind is a closed member too. Closed sets parse through their own parsers —
// no second list of admissible values lives here.
func describeReadable(p Policy) bool {
	if _, ok := describeActions[p.Action]; !ok {
		return false
	}
	if _, rej := ParseSource(string(p.Source)); rej != nil {
		return false
	}
	if _, rej := ParseTargetKind(string(p.Target.Kind)); rej != nil {
		return false
	}
	return true
}

// describeQuoted is the one quoting seam for values a spec carries. %q
// throughout: the sentence is printed to terminals, so every value travels
// the same escape strconv.Quote does — controls, quotes and invisibles
// included (SEC-033/SEC-046).
func describeQuoted(value string) string { return strconv.Quote(value) }

// describeTargetPhrase renders the target half of the template: the five
// phrases the design pins, from a kind the closed set admits (Describe gates
// the row first, so the defensive arm below is unreachable from any stored
// row — it exists so the function is total against a hand-built Policy).
func describeTargetPhrase(t Target) string {
	switch t.Kind {
	case AllAgents:
		return "all agents"
	case AgentKind:
		return "agent " + describeQuoted(t.ID)
	case LoopKind:
		return "loop " + describeQuoted(t.ID)
	case LaneTypeKind:
		return "lane type " + describeQuoted(t.ID)
	case MCPServerKind:
		return "MCP server " + describeQuoted(t.ID)
	}
	return "target " + describeQuoted(t.String())
}

// describeClause closes the sentence around a per-type clause: the clause,
// the target phrase, and the two suffixes — expiry when the row carries one
// (RFC3339 in UTC, the rendering an instant is printed in everywhere), and
// the authorship attribution always. The attribution states the row's own
// source field (the field PL1 is checked against at the write boundary);
// the sentence never claims an operator beyond what the row says.
func describeClause(clause string, p Policy) string {
	var b strings.Builder
	b.WriteString(clause)
	b.WriteString(" for ")
	b.WriteString(describeTargetPhrase(p.Target))
	if p.NotAfter != nil {
		b.WriteString(" — expires ")
		b.WriteString(p.NotAfter.UTC().Format(time.RFC3339))
	}
	b.WriteString(" — authored by ")
	b.WriteString(string(p.Source))
	return b.String()
}

// describeVerb returns the row's action as the sentence's verb, or false for
// an action the closed set does not have.
func describeVerb(a Action) (string, bool) {
	v, ok := describeActions[a]
	return v, ok
}
