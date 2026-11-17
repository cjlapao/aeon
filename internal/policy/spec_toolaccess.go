package policy

// ════════════════════════════════════════════════════════════════════════════
// ToolAccess — the first real spec, and the shape every later type copies.
//
// It absorbs the grant and the blocklist into ONE object (D1/PL1/PL4): an
// operator `Allow` is a grant, a `Deny` from anyone is a blocklist entry, and
// deny wins. There is no second mechanism to keep in sync, so there is nothing
// to drift out of sync.
//
// # The spec is validated on the write path, and strictly
//
// {"toolName": string, "state": "allow"|"deny"|"ask"} — and nothing else. An
// unknown (SEC-022: ask joined the vocabulary with S11 and the shape string
// joined it too — a refusal that quotes a shape narrower than enforcement
// admits tells an author their valid row is invalid). An unknown
// field is REFUSED, not ignored: `{"tool": "bash"}` decoding into a zero-value
// grant is the failure this file exists to prevent, because
//
//	a policy that matches nothing is indistinguishable from a policy that
//	permits everything.
//
// That sentence is the whole argument for strictness, and it is why the checks
// here are syntactic rather than charitable: no case-insensitive matching, no
// alias for `toolName`, no defaulting of `state`, no coercion of `5` into
// `"5"`. A typo costs its author one error message; the same typo read
// leniently costs every reader a permission that is invisible because nothing
// authored it. S4 put CHECK constraints on the columns for the same reason;
// this is that argument one level in, where the value is a JSON object instead
// of a column.
//
// # Where these validators run, and where they do not
//
// Validate is the per-type half of the WRITE boundary (T6 installs it on the
// insert path). It never runs during Evaluate: the evaluator is total (PL6)
// and must decide from whatever rows the store holds, so a row that escaped
// validation is handled by the evaluator refusing to MATCH it — inert, toward
// deny — and never by an error, a panic, or a permission.
//
// # The schema is the Go struct
//
// The accepted key set is derived from the exported spec structs by reflection,
// never re-typed into a second list that could disagree with the first. The
// `shape` string each refusal quotes is the one thing written by hand, and
// TestThePinnedSpecShapesMatchTheirGoStructs locks it against the derived keys,
// so a rename that updates one and not the other fails the build of the suite.
// ════════════════════════════════════════════════════════════════════════════

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ToolAccessSpec is the whole ToolAccess schema: one tool, and what the row
// does with it.
//
// ToolName is matched EXACTLY and case-sensitively when it is a bare tool name.
// No prefixes. S74 (operator ruling 2026-11-07) adds ONE other form: a tool
// ADDRESS pattern, `<category>.<tool>.<access>`, whose segments are literals or
// a whole `*` (toolpattern.go). That is a way to name a set over a closed
// vocabulary the daemon computes and can list, not a second rule language. `"ba"` therefore
// governs a tool called `ba` and nothing else — never `bash`.
//
// State repeats the row's action inside the spec, which looks redundant and is
// deliberate: the spec is what an operator writes and what the app renders, and
// a spec whose state contradicts its action is a policy whose author meant one
// thing and stored another. The validator refuses the contradiction, and the
// evaluator refuses to match a row that escaped the validator.
type ToolAccessSpec struct {
	ToolName string `json:"toolName"`
	State    string `json:"state"` // "allow" | "deny" | "ask"
}

// toolAccessShape is quoted verbatim in every refusal, so an operator who never
// read this file can still see the shape that was wanted.
const toolAccessShape = `{"toolName": string, "state": "allow"|"deny"|"ask"}`

// ── the schema machinery every type shares ───────────────────────────────────

// specFieldKind is how one declared field is read. Two kinds is the whole set
// in this slice: a name (a string) and a cap (a json.Number). A later slice
// that needs a bool or a list adds a kind here, in the open, with its check.
type specFieldKind int

const (
	stringKind specFieldKind = iota
	numberKind
)

// specField is one declared field of a spec schema, derived from the struct.
type specField struct {
	goName   string        // the Go field name, to read the decoded value back
	name     string        // the JSON key, the thing an operator types
	kind     specFieldKind // numberKind is decoded as json.Number and must be >= 0
	required bool          // false only for a field tagged ,omitempty
}

// specSchema is one type's pinned spec shape.
type specSchema struct {
	typ        PolicyType
	shape      string       // the refusal text; locked against fields by a test
	structType reflect.Type // the Go struct that IS the schema
	fields     []specField  // derived from structType, in declaration order
}

// newSpecSchema builds the schema of one type from its Go struct. The struct is
// the single source of the accepted key set; the shape string is the rendering
// of it that a refusal quotes.
func newSpecSchema(typ PolicyType, shape string, structType reflect.Type) specSchema {
	s := specSchema{typ: typ, shape: shape, structType: structType}
	if structType.Kind() != reflect.Struct {
		// A schema with no fields accepts nothing (see specSchema.validate), so
		// wiring the wrong Go type in fails closed rather than permitting all.
		return s
	}
	for i := 0; i < structType.NumField(); i++ {
		f := structType.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		field := specField{goName: f.Name, name: name, required: !strings.Contains(opts, "omitempty")}
		if f.Type == reflect.TypeOf(json.Number("")) {
			field.kind = numberKind
		}
		s.fields = append(s.fields, field)
	}
	return s
}

// keys returns the declared JSON keys in declaration order.
func (s specSchema) keys() []string {
	out := make([]string, 0, len(s.fields))
	for _, f := range s.fields {
		out = append(out, f.name)
	}
	return out
}

// SpecKey is the public description of one declared spec field: what an
// operator types, whether it must be there, and whether it is a cap.
type SpecKey struct {
	Name     string
	Required bool
	Number   bool
}

// SpecSchema is the public answer to "what spec does this type accept". It is
// what T6's write boundary reports when it refuses, and what `aeon policy add`
// and the app render as the shape to write — so the shape exists once, in the
// validator, and is never copied into a help string.
type SpecSchema struct {
	Type  PolicyType
	Shape string
	Keys  []SpecKey
}

// SchemaCarrier is implemented by every validator this package installs. It is
// a SEPARATE, small interface rather than a third method on Validator, so a
// caller that only needs to check a spec does not have to know the schema
// exists; a caller that has to explain a refusal asks for it (and the type
// assertion failing means someone registered a validator from outside this
// package, which the registry's init-only rule already forbids).
type SchemaCarrier interface {
	Validator
	// SpecSchema returns the pinned shape: the type, the quoted rendering every
	// refusal uses, and the keys derived from the Go struct.
	SpecSchema() SpecSchema
}

func (s specSchema) public() SpecSchema {
	out := SpecSchema{Type: s.typ, Shape: s.shape, Keys: make([]SpecKey, 0, len(s.fields))}
	for _, f := range s.fields {
		out.Keys = append(out.Keys, SpecKey{Name: f.name, Required: f.required, Number: f.kind == numberKind})
	}
	return out
}

// decode decodes raw into dst — which must be a pointer to this schema's own
// struct — and refuses anything that is not exactly that object.
//
// Three separate refusals, because three different mistakes deserve three
// different sentences: not an object at all, a key nobody declared, and
// content after the object. Each names what it found; the unknown-key refusal
// names every offending key (sorted, so the message is stable) and suggests a
// near-miss when one key is close enough that the typo is obvious.
func (s specSchema) decode(raw []byte, dst any) *Reject {
	if len(s.fields) == 0 {
		return rejectf("%s has no spec schema wired; refusing rather than accepting any spec", string(s.typ))
	}
	dstType := reflect.TypeOf(dst)
	if dstType == nil || dstType.Kind() != reflect.Pointer || dstType.Elem() != s.structType {
		return rejectf("%s spec was handed the wrong Go type (%T) to decode into; this is a defect in "+
			"the schema wiring, not in the policy", string(s.typ), dst)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return rejectf("%s spec is empty; it must be exactly %s — a spec that names nothing is "+
			"indistinguishable from one that permits everything", string(s.typ), s.shape)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		if strings.Contains(err.Error(), "after top-level value") {
			return rejectf("%s spec has trailing content after the object; it must be exactly %s",
				string(s.typ), s.shape)
		}
		return rejectf("%s spec must be a JSON object (%s): %v", string(s.typ), s.shape, err)
	}
	if got == nil { // the literal null unmarshals to a nil map, not an error
		return rejectf("%s spec is null; it must be exactly %s", string(s.typ), s.shape)
	}
	keys := s.keys()
	var unknown []string
	for name := range got {
		if !slices.Contains(keys, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		rej := rejectf("%s spec has unknown %s %s; the spec is exactly %s", string(s.typ),
			plural(len(unknown), "field", "fields"), quoteAll(unknown), s.shape)
		if alt := didYouMean(unknown[0], keys); alt != "" {
			return rejectf("%s — did you mean %q?", rej.Reason, alt)
		}
		return rej
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // the belt to the map's braces: nothing slips past either one
	if err := dec.Decode(dst); err != nil {
		return rejectf("%s spec is not %s: %v", string(s.typ), s.shape, err)
	}
	if dec.More() {
		return rejectf("%s spec has trailing content after the object; it must be exactly %s", string(s.typ), s.shape)
	}
	return nil
}

// validate is the write-path check: decode strictly, then insist every
// required field carries something, that a cap is a number that is not
// negative, and that a state agrees with the row's action.
//
// It returns *Reject, never an error (DB19): a bad spec is a wrong answer from
// the caller, not a failure of the machine.
func (s specSchema) validate(p Policy) *Reject {
	value := reflect.New(s.structType).Elem()
	if rej := s.decode(p.Spec, value.Addr().Interface()); rej != nil {
		return rej
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(p.Spec), &present); err != nil {
		// decode already read the same bytes into a map; arriving here means
		// the two readers disagreed, which is a defect, and the answer is the
		// same as for any unreadable spec: refuse.
		return rejectf("%s spec could not be read; it must be exactly %s", string(s.typ), s.shape)
	}
	// The state check runs after the loop, not inside it: every field gets its
	// own refusal whichever position it is declared in, so reordering a struct
	// can never silently skip a check.
	state, hasState := "", false
	for _, f := range s.fields {
		raw, there := present[f.name]
		if !there {
			if f.required {
				return rejectf(`%s spec is missing %q; the spec is exactly %s — a field left out is not `+
					`a field left blank`, string(s.typ), f.name, s.shape)
			}
			continue
		}
		if f.kind == numberKind {
			// A cap is a number. Go's decoder will take a quoted one ("1000"),
			// and this package does not: the shape says number, and a second
			// spelling of the same fact is how two rows that look different
			// come to mean the same thing — the exact drift `state` carries a
			// rule of its own to prevent.
			if text := bytes.TrimSpace(raw); len(text) > 0 && text[0] == '"' {
				return rejectf(`%s spec field %q must be a number, not a string that holds one; `+
					`the spec is exactly %s`, string(s.typ), f.name, s.shape)
			}
			number := value.FieldByName(f.goName).String()
			parsed, err := strconv.ParseFloat(number, 64)
			switch {
			case err != nil && !errors.Is(err, strconv.ErrRange):
				return rejectf("%s spec field %q must be a number: %v; the spec is exactly %s",
					string(s.typ), f.name, err, s.shape)
			case math.IsNaN(parsed) || math.IsInf(parsed, 0):
				// 1e999 parses to +Inf with a range error: a cap nothing can
				// exceed is not a cap, and a cap that is not a number is not
				// one either. Say which the value failed to be.
				return rejectf("%s spec field %q must be a finite number, got %s; the spec is exactly %s",
					string(s.typ), f.name, number, s.shape)
			}
			if parsed < 0 {
				return rejectf("%s spec field %q must not be negative, got %s — a negative cap is not a "+
					"tighter cap, it is a value nothing can satisfy; the spec is exactly %s",
					string(s.typ), f.name, number, s.shape)
			}
			continue
		}
		text := value.FieldByName(f.goName).String()
		if strings.TrimSpace(text) == "" {
			return rejectf(`%s spec field %q must name the thing the policy governs; %q matches nothing, `+
				`and a policy that matches nothing reads as one that permits everything; the spec is exactly %s`,
				string(s.typ), f.name, text, s.shape)
		}
		if f.name == "state" {
			state, hasState = text, true
		}
	}
	if hasState {
		return stateAgreesWithAction(s, p, state)
	}
	return nil
}

// stateAgreesWithAction enforces the one cross-field rule every state-bearing
// spec has: the spec's state is the row's action restated, and the two must
// say the same thing. The refusal names both, because the fix is "change one
// of them" and a person cannot know which from "inconsistent".
//
// An action outside the closed set is refused here too. The validator is handed
// stored rows, and a row whose action escaped the vocabulary has no answer to
// compare against — so it gets no permission.
func stateAgreesWithAction(s specSchema, p Policy, state string) *Reject {
	if _, rej := ParseAction(state); rej != nil {
		return rejectf(`%s spec field "state" is %q; the spec is exactly %s`, string(s.typ), state, s.shape)
	}
	if _, rej := ParseAction(string(p.Action)); rej != nil {
		return rejectf("%s policy has action %q, which is outside the closed set (%q, %q, %q); state must "+
			"be able to agree with it; the spec is exactly %s",
			string(s.typ), string(p.Action), string(Allow), string(Deny), string(Ask), s.shape)
	}
	if state != string(p.Action) {
		return rejectf(`%s spec says "state": %q but the policy's action is %q; they must say the same `+
			"thing — fix the one that is wrong rather than letting the row mean two things; the spec is exactly %s",
			string(s.typ), state, string(p.Action), s.shape)
	}
	return nil
}

// ── the ToolAccess validator and evaluator ───────────────────────────────────

var toolAccessSchema = newSpecSchema(ToolAccess, toolAccessShape, reflect.TypeOf(ToolAccessSpec{}))

// toolAccessValidator is the write boundary's per-type check for ToolAccess.
type toolAccessValidator struct{}

// Validate refuses any ToolAccess spec that is not exactly
// {"toolName": string, "state": "allow"|"deny"|"ask"} with a non-empty toolName whose
// state matches the row's action.
func (toolAccessValidator) Validate(p Policy) *Reject {
	if rej := toolAccessSchema.validate(p); rej != nil {
		return rej
	}
	// S74 TA6: an address pattern is checked segment by segment, and the
	// refusal names the offending segment.
	var spec ToolAccessSpec
	if toolAccessSchema.decode(p.Spec, &spec) == nil {
		if why := ValidateToolPattern(spec.ToolName); why != "" {
			return rejectf("%s; the spec is exactly %s", why, toolAccessShape)
		}
	}
	return nil
}

// SpecSchema reports the pinned ToolAccess shape (SchemaCarrier).
func (toolAccessValidator) SpecSchema() SpecSchema { return toolAccessSchema.public() }

// ToolNameCanonicalizer maps a registered key to the tool's ONE honest name
// (SEC-059): the alias write_file answers "write", honest names answer
// themselves. internal/tools supplies the real one at init (its Registry is
// the single source of the mapping, and the write_file literal keeps its
// ONE production home in toolset.go); policy must not import tools — tools
// imports policy. nil means identity: an unwired engine compares names
// exactly as they are spelled, the pre-SEC-059 behaviour, which is inert
// rather than wrong, and TestTheCanonicalizerWiringAppearsInExactlyOnePlace
// pins that production never runs unwired.
type ToolNameCanonicalizer func(string) string

// toolNameCanonical is written only at init (by SetToolNameCanonicalizer) and
// read at decide time; every write precedes every Evaluate, so the race
// detector stays quiet and no lock is needed — the same init-only discipline
// SEC-010 enforces on the evaluator registry itself.
var toolNameCanonical ToolNameCanonicalizer

// SetToolNameCanonicalizer is an INIT-TIME seam: exactly one production call
// site (internal/tools/toolset.go), pinned by a module-wide grep test, so no
// runtime caller can quietly re-shape what a ToolAccess row matches.
// nil restores identity.
func SetToolNameCanonicalizer(fn ToolNameCanonicalizer) { toolNameCanonical = fn }

// ToolAddressResolver maps a tool name to its S74 address, or "" when the
// name is not a tool the resolver knows. Like the canonicalizer it is an
// init-time seam: internal/tools installs it, because the registry is its
// knowledge and this package must not import it.
type ToolAddressResolver func(string) string

var toolAddressOf ToolAddressResolver

// SetToolAddressResolver is the INIT-TIME seam; exactly one production call.
func SetToolAddressResolver(fn ToolAddressResolver) { toolAddressOf = fn }

// canonicalToolName is the one read of the seam. An unwired engine (nil) or a
// canonicalizer that answers nothing ("" for this name) falls back to the
// spelling as given: identity, never a guess.
func canonicalToolName(name string) string {
	if toolNameCanonical != nil {
		if out := toolNameCanonical(name); out != "" {
			return out
		}
	}
	return name
}

// toolAccessEvaluator answers the type this slice implements. It never reports
// Unimplemented: a ToolAccess row is either named by the spec or it is not.
//
// Every refusal to match is INERT — Matched false, no Unimplemented — which is
// the totality claim D10 hands to the evaluator: a row that cannot be read
// permits nothing and panics nothing. The Note is a fixed string naming the
// shape that failed, never the bytes that failed it: a Note is a log line, and
// a log line that carries spec content is how policy text leaks into logs the
// slice promises contain none (legal constraint 1).
type toolAccessEvaluator struct{}

// Match reports whether p's spec names what r wants to do. The final name
// comparison reads the CANONICAL form of BOTH sides (SEC-059): this is the
// one judge the gate, the CLI why and the socket why all share
// (evaluate.go's ev.Match), so judging canonical names here is what makes a
// row authored write_file decide identically on every surface — the alias
// knowledge is injected from internal/tools at init (SetToolNameCanonicalizer),
// never duplicated in this package.
func (toolAccessEvaluator) Match(p Policy, r Request) MatchRes {
	var spec ToolAccessSpec
	if rej := toolAccessSchema.decode(p.Spec, &spec); rej != nil {
		return MatchRes{Note: "the spec is not the pinned ToolAccess shape"}
	}
	if r.Type != ToolAccess {
		// The engine never hands an evaluator another type's request; if it
		// ever did, the answer is still "not me", never "yes".
		return MatchRes{Note: "the request asks about a different type"}
	}
	if strings.TrimSpace(spec.ToolName) == "" {
		return MatchRes{Note: "an empty toolName names no tool"}
	}
	if spec.State != string(p.Action) {
		// A row the validator would have refused (a hand-written row, or one
		// stored before this validator existed): contradictory, so it decides
		// nothing. Picking either half of the contradiction would be the
		// evaluator inventing an intention nobody wrote.
		return MatchRes{Note: "the spec's state contradicts the row's action"}
	}
	// Both sides speak the honest name (SEC-059): the gate already asks in it
	// (gateQuestion), but why passes the caller's spelling raw — judging the
	// CANONICAL form of both is what makes one row decide identically on every
	// surface. Unknown names canonicalize to themselves (CanonicalToolName's
	// contract), so exactness for every other tool is untouched.
	if IsToolAddressForm(spec.ToolName) {
		// S74: an address pattern needs the tool's address. One that cannot be
		// resolved matches no pattern — it is never guessed.
		addr := r.ToolAddress
		if addr == "" && toolAddressOf != nil {
			addr = toolAddressOf(r.Tool)
		}
		if MatchToolAddress(spec.ToolName, addr) {
			return MatchRes{Matched: true}
		}
		return MatchRes{Note: "the toolName pattern does not match the request's tool address"}
	}
	if want := canonicalToolName(r.Tool); want != "" && want == canonicalToolName(spec.ToolName) {
		return MatchRes{Matched: true}
	}
	return MatchRes{Note: "the toolName does not name the request's tool"}
}

func init() {
	RegisterType(ToolAccess, toolAccessValidator{}, toolAccessEvaluator{})
}

// ── small text helpers, shared by every refusal in this file ─────────────────

// plural picks the word for a count, so a refusal reads as prose.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// quoteAll renders the values a refusal names, in the order it was given them.
func quoteAll(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Quote(v))
	}
	return strings.Join(parts, ", ")
}

// didYouMean returns the declared key a typo most likely meant, when one is
// close enough that saying it saves a round trip, and "" when none is. The
// measure is the shared prefix: "tool" for "toolName" and "stat" for "state"
// are the typos that actually happen, and they are the ones worth answering. A
// wild-key name gets no guess, because a wrong suggestion is worse than none.
func didYouMean(name string, keys []string) string {
	const minShared = 3
	lower := strings.ToLower(name)
	best, bestShared := "", 0
	for _, k := range keys {
		shared := sharedPrefixLen(lower, strings.ToLower(k))
		if shared > bestShared {
			best, bestShared = k, shared
		}
	}
	if bestShared < minShared {
		return ""
	}
	return best
}

func sharedPrefixLen(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// --- describing this type (S45 T2, PM5/PM6) -----------------------------------
//
// Additive: Validate above is untouched. The sentence is this validator's
// reading of the spec it just validated — the same decode, the same
// contradiction refusal the evaluator keeps — and the field descriptors are
// derived from the same schema that gates the write, so a UI form built from
// them cannot offer a value the write would refuse (PM6).

// describe renders the design's tool_access sentence:
// "<allows|denies|asks before> tool %q for <target phrase>" plus the shared
// suffixes. A row whose spec contradicts its action describes nothing — the
// evaluator refuses to match it and the formatter refuses to render it.
func (toolAccessValidator) describe(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(ToolAccess)
	}
	verb, _ := describeVerb(p.Action)
	var spec ToolAccessSpec
	if rej := toolAccessSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(ToolAccess)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(ToolAccess)
	}
	return describeClause(verb+" tool "+describeQuoted(spec.ToolName), p)
}

// describeFields derives the spec descriptor from toolAccessSchema — the one
// field list the write boundary enforces.
func (toolAccessValidator) describeFields() []SpecField {
	return toolAccessSchema.describeFields()
}
