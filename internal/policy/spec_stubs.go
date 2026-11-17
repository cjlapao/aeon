package policy

// ════════════════════════════════════════════════════════════════════════════
// Five types that have a schema today and an evaluator later — and refuse,
// rather than permit, in between.
//
// SkillAccess (S14) · EffectClass (S16) · CostCap (S17) · RouteAccess (S18) ·
// TriggerAccess (S20 — its evaluator lives in spec_triggeraccess.go, the
// way SkillAccess's moved to spec_skillaccess.go at S14; the shape stays here
// so rows authored under the placeholder decide the moment S20 linked in).
//
// CommandAccess (S11) was the sixth and is not: S11 landed its matcher and its
// corpus, and the type is registered in spec_commandaccess.go with a validator
// and an evaluator that decide. It left this file in that diff — the schema,
// the shape string and the struct moved with it, because a stored row's shape
// and the code that reads it are one thing and must not live apart. What this
// file still owns is the placeholder machinery every later type arrives with
// (stubValidator, stubEvaluator, StubMarker), and the four names below —
// SkillAccess filled in at S14, its evaluator and init living in
// spec_skillaccess.go while its schema stays here.
//
// The rule these five exist to make unrepeatable:
//
//	A `default:` branch that permitted would silently grant everything an
//	unimplemented type governs, and the grant would be invisible because
//	nobody authored it.
//
// So each type arrives with THREE things, and the third is a refusal:
//
//  1. a schema, pinned here, so a policy of that type can be stored and
//     validated today — the operator writes the rule now and the engine honours
//     it the moment the slice that decides it lands;
//  2. a validator on the same schema-driven core as ToolAccess, so the shape is
//     checked at the write boundary and not at first use;
//  3. an evaluator that answers Unimplemented. Not "no", not "denied": the
//     engine could not decide, which is a DIFFERENT FACT from "a rule refused
//     you" and must stay different all the way to T11's three-state answer
//     (D10/PL12). A refusal that rendered as a denial would tell an operator
//     to go delete a policy that has nothing to do with it. And it DECLARES
//     itself the placeholder — `Stub() bool`, the seam's one optional method,
//     read by inforce.go and nothing else (see StubMarker below), so the
//     could-not-determine answer has exactly one cause and never a guess.
//
// The schemas are pinned now, deliberately, even though the slices named above
// will know better by then. A stored row needs a shape, or the write boundary
// has nothing to check and any JSON becomes a policy; and the shape that ships
// is the shape the app will render. Where a later slice wants a richer shape
// (S17's per-loop budget, the design's `{scope, limitUSD}` sketch), it replaces
// the EVALUATOR and extends the schema in its own reviewed diff with its own
// migration — it does not silently reinterpret rows stored under this one.
// CommandAccess is the precedent for how that reads when it is done: S11 kept
// the pinned `{command, state}` object, widened only the state vocabulary, and
// replaced the evaluator (spec_commandaccess.go). TestThePinnedSpecShapes
// MatchTheirGoStructs locks each shape against the Go struct that defines it.
//
// Registration is by enumeration below: five explicit calls naming five
// constants. A type that arrives without one fails TestTheRegistryCoversEvery
// Type — a test, not a review comment.
// ════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// RouteAccessSpec is the spec of a RouteAccess policy: which model route a
// target may use — the shape behind "never send code to a non-local route"
// (S18). It will match req.Route.
type RouteAccessSpec struct {
	Route string `json:"route"`
	State string `json:"state"` // "allow" | "deny"
}

// EffectClassSpec is the spec of an EffectClass policy: a tool governed by the
// KIND of effect it has (reading, writing, spending) rather than by its name
// (S16). It will match req.Effect.
type EffectClassSpec struct {
	Effect string `json:"effect"`
	State  string `json:"state"` // "allow" | "deny"
}

// CostCapSpec is the spec of a CostCap policy: a cap on one named token.
//
// It is the one schema without a `state`, and the reason is in the type's
// meaning: a cap is not something you deny. There is nothing to refuse, so an
// Allow IS the grant and the presence of the row IS the cap — a `state` field
// here would be a second, contradictable copy of a fact the row already states.
// `max` is a json.Number, so `1000`, `1000.50` and `1e3` all mean a cap and a
// string never does; it is validated finite and non-negative (zero is a real
// cap — nothing may be spent — and it is not the same as no row). S17 decides
// what the cap caps; it will read req.Cost, the token name.
type CostCapSpec struct {
	Token string      `json:"token"`
	Max   json.Number `json:"max"`
}

// SkillAccessSpec is the spec of a SkillAccess policy: which skill an agent may
// load (S14, A2A's per-skill `security` expressed as policy). It will match
// req.Skill.
type SkillAccessSpec struct {
	Skill string `json:"skill"`
	State string `json:"state"` // "allow" | "deny"
}

// TriggerAccessSpec is the spec of a TriggerAccess policy: which trigger may
// start a loop for a target — "no webhooks here", "no unattended schedules"
// (S20). It will match req.Trigger.
type TriggerAccessSpec struct {
	Trigger string `json:"trigger"`
	State   string `json:"state"` // "allow" | "deny"
}

// The five pinned shapes. Each string is what every refusal quotes, so the
// operator sees the shape that was wanted; the accepted keys themselves come
// from the structs above (see newSpecSchema), and the lock test keeps the two
// readings of each schema in step. (CommandAccess's shape lives beside its
// struct, in spec_commandaccess.go.)
const (
	routeAccessShape   = `{"route": string, "state": "allow"|"deny"}`
	effectClassShape   = `{"effect": string, "state": "allow"|"deny"}`
	costCapShape       = `{"token": string, "max": number}`
	skillAccessShape   = `{"skill": string, "state": "allow"|"deny"}`
	triggerAccessShape = `{"trigger": string, "state": "allow"|"deny"}`
)

// The five schemas, in the order the design names the types.
var (
	routeAccessSchema   = newSpecSchema(RouteAccess, routeAccessShape, reflect.TypeOf(RouteAccessSpec{}))
	effectClassSchema   = newSpecSchema(EffectClass, effectClassShape, reflect.TypeOf(EffectClassSpec{}))
	costCapSchema       = newSpecSchema(CostCap, costCapShape, reflect.TypeOf(CostCapSpec{}))
	skillAccessSchema   = newSpecSchema(SkillAccess, skillAccessShape, reflect.TypeOf(SkillAccessSpec{}))
	triggerAccessSchema = newSpecSchema(TriggerAccess, triggerAccessShape, reflect.TypeOf(TriggerAccessSpec{}))
)

// stubValidator is the write-boundary check for a type with no evaluator yet.
// It is a full validator, not a placeholder: the row an operator stores today
// is checked against the shape its evaluator will read later, so "unimplemented"
// never means "unvalidated" and a typo surfaces at authoring rather than in the
// slice that finally implements the type.
type stubValidator struct {
	schema specSchema
}

// Validate refuses any spec that is not exactly the type's pinned shape.
func (v stubValidator) Validate(p Policy) *Reject { return v.schema.validate(p) }

// SpecSchema reports the pinned shape (SchemaCarrier), so T6 and `aeon policy
// add` can tell an operator what to write for a type nothing evaluates yet.
func (v stubValidator) SpecSchema() SpecSchema { return v.schema.public() }

// stubEvaluator is the D10 shape: registered, consulted, and refusing.
//
// It carries its type so the refusal names the thing nobody has implemented —
// the one word an operator can act on ("register skill_access" is a task;
// "not supported" is a shrug). It never sets Matched: a refusal to decide must
// not be able to contribute a permission OR a denial, so the engine sees
// sawUnimplemented and answers Unimplemented, distinguishable from a policy deny
// (By == nil) and from the default deny (DefaultDeny == false) — T11's third
// state depends on those three shapes being three shapes.
//
// It is also total by construction: it reads nothing from the policy and the
// request, so no row, however malformed, can make it panic or make it decide.
type stubEvaluator struct {
	typ PolicyType
}

// Match refuses to decide, always.
func (e stubEvaluator) Match(Policy, Request) MatchRes {
	return MatchRes{Unimplemented: true, Note: fmt.Sprintf("no evaluator for %q yet", e.typ)}
}

// Stub declares, in the evaluator's own words, that it is the placeholder —
// registered so the type is known and refuses rather than permits, and holding
// no decision. It is the ONE cause T11's could-not-determine answer accepts
// (inforce.go's noEvaluatorYet): the type is a stub because it SAYS so.
//
// The day a real evaluator replaces this one, the method goes away with it, in
// the same reviewed diff, and the in-force answer moves from "could not
// determine" to an answer about enforcement automatically — which is the
// property the marker buys: nothing downstream infers the state from behaviour
// it could misread, and nothing has to remember to edit a second file.
// CommandAccess is the proof, not the promise: S11's diff deleted both this
// method and its registration for that type, and the in-force answer moved on
// its own.
func (stubEvaluator) Stub() bool { return true }

// StubMarker is the seam's one optional method: an evaluator that implements it
// is saying what it is. Every evaluator that decides for real leaves it
// unimplemented, and absence IS the false — that is the default.
//
// Why it is not a method of Evaluator: every later slice (S11's matcher, S14's,
// S16's, S17's, S18's, S20's) would have to write `Stub() bool { return false }`
// — boilerplate whose answer never varies, in a diff whose whole job is to
// arrive and start deciding. The registry grows by arriving (PL3), so the
// question grows the same way: a stub declares itself, a real evaluator does
// not get asked. The alternative — reading stub-ness off MatchRes.Unimplemented
// — would collapse the two facts T3 kept apart (T11's third state exists
// precisely because "nobody can evaluate this type" is not "a rule refused
// you"), so it is not offered, and evaluatorIsStub below is the only reader.
type StubMarker interface {
	// Stub reports whether this evaluator is a placeholder with no decision in
	// it. Only a true answer means anything: implementing it to return false
	// is the same as not implementing it at all.
	Stub() bool
}

// evaluatorIsStub is the ONE question the registry is asked to answer
// "could this type be decided yet" for the in-force answer: no marker, no
// claim, so the default is false (a registered evaluator that says nothing
// decides). The type assertion is the only branch, and there is no failure
// channel to check — a registry question that could fail would be an answer
// surfaces drop, and "could not determine" would go missing exactly when the
// infrastructure is worst.
func evaluatorIsStub(e Evaluator) bool {
	m, ok := e.(StubMarker)
	if !ok {
		return false
	}
	return m.Stub()
}

// init registers the two that remain — by enumeration, one call per type, each
// naming its constant so the registry guard (TestRegisterTypeIsCodeTimeOnly) can see that
// what is registered is exactly the closed set and that it happens at code time,
// never at runtime (the map is read unsynchronised by every Evaluate).
//
// CommandAccess is absent because it is no longer a placeholder: its validator
// and evaluator are its own, in spec_commandaccess.go, and so is its init. The
// seventh name, ToolAccess, is registered in spec_toolaccess.go the same way.
//
// SkillAccess is absent for the same reason it arrived: S14 filled it in
// (spec_skillaccess.go registers the real evaluator over THIS file's schema
// and validator — the shape shipped first so operators could author rows, and
// the decision arrived with the slice that owns it). The spec struct, shape
// string and schema stay here: storage never changed, only the deciding.
//
// RouteAccess left the same way at S21 T4 (spec_routeaccess.go registers the
// real evaluator over THIS file's schema and validator): the type arrived for
// model routes and S21 EXTENDED ITS SUBJECT to agent egress (`remote:` route
// values) rather than minting a second access type — one type, one authorship
// story, one thing to remember to lock (RA4).
func init() {
	RegisterType(EffectClass, stubValidator{effectClassSchema}, stubEvaluator{EffectClass})
	RegisterType(CostCap, stubValidator{costCapSchema}, stubEvaluator{CostCap})
}

// ════════════════════════════════════════════════════════════════════════════
// Describing the placeholder types (S45 T2, PM5/PM6) — additive; every
// Validate/Match above is byte-identical.
//
// The five types this file's stubValidator serves split their describing the
// way their evaluators split: EffectClass and CostCap still live wholly
// here, so their clause builders do too; RouteAccess, SkillAccess and
// TriggerAccess filled their deciding out into their own files, so their
// clause builders live beside that deciding (spec_routeaccess.go,
// spec_skillaccess.go, spec_triggeraccess.go) and register themselves into
// the describing map from their own init — sibling code to their
// registration, never a branch here (PL3, and the no-switch guard).
// ════════════════════════════════════════════════════════════════════════════

// describe answers the describer seam for every type this shared validator
// serves: the map lookup is the dispatch (a type whose describing never
// registered says so as the fallback, inventing nothing).
func (v stubValidator) describe(p Policy) string {
	if f, ok := placeholderDescribes[v.schema.typ]; ok {
		return f(p)
	}
	return unregisteredSentence(v.schema.typ)
}

// describeFields answers the fieldsDescriber seam the same way — the schema
// the write boundary enforces is the descriptor's one source either way.
func (v stubValidator) describeFields() []SpecField {
	if f, ok := placeholderFields[v.schema.typ]; ok {
		return f()
	}
	return v.schema.describeFields()
}

// describeEffectClass renders the effect_class sentence:
// "<verb> effect %q for <target phrase>" — the field named is `effect`, the
// kind of effect (reading, writing, spending) a tool of this class has.
func describeEffectClass(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(EffectClass)
	}
	verb, _ := describeVerb(p.Action)
	var spec EffectClassSpec
	if rej := effectClassSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(EffectClass)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(EffectClass)
	}
	return describeClause(verb+" effect "+describeQuoted(spec.Effect), p)
}

// describeCostCap renders the design's cost_cap sentence:
// "caps %q spend at %s for <target phrase>". %s is deliberate: max is a
// NUMBER — the json.Number token the strict decoder admitted, digits and
// exponent only, which quoting would render a string (the grammar quotes the
// token named, never the cap).
func describeCostCap(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(CostCap)
	}
	var spec CostCapSpec
	if rej := costCapSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(CostCap)
	}
	if spec.Max == "" {
		return unreadableSentence(CostCap)
	}
	return describeClause("caps "+describeQuoted(spec.Token)+" spend at "+string(spec.Max), p)
}

func init() {
	registerPlaceholderDescribing(EffectClass, describeEffectClass,
		func() []SpecField { return effectClassSchema.describeFields() })
	registerPlaceholderDescribing(CostCap, describeCostCap,
		func() []SpecField { return costCapSchema.describeFields() })
}
