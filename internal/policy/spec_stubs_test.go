package policy_test

// The five types S10 does not implement, and the registry that holds them.
//
// CommandAccess is not among them: S11 gave it a matcher, a corpus, a validator
// and an evaluator (spec_commandaccess.go), so it decides — and a type that
// decides has no business in the table below, whose whole job is to pin that the
// ones that do NOT decide are distinguishable from the ones that do. Its row
// lives in spec_commandaccess_test.go as `commandAccessCase`, and the schema
// lock below appends it so all seven shapes stay locked.
//
// Three claims, all asserted by enumeration rather than by sampling:
//
//  1. every one of the five has a schema that validates and an evaluator that
//     REFUSES, and the refusal is distinguishable from a policy deny and from
//     the default deny — three shapes, three facts (D10/PL12, and T11's
//     could-not-determine depends on the distinction existing in the verdict);
//  2. the registry covers all seven with no gaps, and production code registers
//     nothing outside the closed set (PL3's "extend by registering");
//  3. a half-finished registration is a refusal rather than a panic (SEC-009).
//
// The structural guards over the package's own source — no `switch` on the type,
// registration only in init (SEC-010), no compiled-in blocklist — are in
// spec_registry_guards_test.go, along with the AST readers they share.
//
// Plus the path T4 left unpinned: the engine's `sawUnimplemented` return is the
// one Verdict no test reached until now, and the types that have no evaluator
// make it hot — so it is pinned here, with its subject and its type (DEF-016).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/policy"
)

// ── the four, as data ────────────────────────────────────────────────────────

// stubCase is one unimplemented type and everything a test needs to exercise it
// exactly as the others. Driving them from a table is what makes "all of them"
// a claim the suite checks: a type arriving without a row here fails
// TestTheRegistryCoversEveryType, and a type that stops being a stub must leave
// the table (CommandAccess did, in S11 — see commandAccessCase in
// spec_commandaccess_test.go; SkillAccess did in S14 — see skillAccessCase in
// spec_skillaccess_test.go).
type stubCase struct {
	typ      policy.PolicyType
	shape    string // the exact text every refusal quotes
	key      string // the required name field
	value    string // a non-empty value for it
	number   string // the cap field, for the one schema that has one; "" otherwise
	hasState bool
	valid    string // a spec that must be accepted, state and all
	// set puts the value on the Request field this type will one day read.
	set func(*policy.Request, string)
}

var stubCases = []stubCase{
	// RouteAccess left this list at S21 T4 — its evaluator is real
	// (spec_routeaccess.go, the subject extended to `remote:` agent egress by
	// RA4); its deciding tests ride internal/remote/egress_test.go through
	// Evaluate, exactly as SkillAccess's and TriggerAccess's moved at S14 and
	// S20. The row shape stays pinned above, because storage never changed.
	{
		typ: policy.EffectClass, shape: `{"effect": string, "state": "allow"|"deny"}`,
		key: "effect", value: "write", hasState: true,
		valid: `{"effect":"write","state":"deny"}`,
		set:   func(r *policy.Request, v string) { r.Effect = v },
	},
	{
		typ: policy.CostCap, shape: `{"token": string, "max": number}`,
		key: "token", value: "output_tokens", hasState: false, number: "max",
		valid: `{"token":"output_tokens","max":1000}`,
		set:   func(r *policy.Request, v string) { r.Cost = v },
	},
	// TriggerAccess left this list at S20 T7 — its evaluator is real
	// (spec_triggeraccess.go) and its decision tests live in
	// spec_triggeraccess_test.go, exactly as SkillAccess's moved at S14.
}

// specWith builds a spec carrying the named fields and nothing else, so a test
// can isolate one missing or empty field without tripping the unknown-key check
// first — the order of the refusals is part of the design, and the tests follow
// it rather than fighting it. A cap is written as a bare JSON number (an empty
// number argument leaves the field out entirely).
func specWith(c stubCase, key, number string) string {
	parts := []string{fmt.Sprintf("%q:%q", c.key, key)}
	if c.number != "" && number != "" {
		parts = append(parts, fmt.Sprintf("%q:%s", c.number, number))
	}
	if c.hasState {
		parts = append(parts, `"state":"allow"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// stubPolicy builds a row of a stub type with a spec an operator could write
// today. Its action agrees with its spec, so the refusal under test is the
// type's unimplemented-ness and nothing else.
func stubPolicy(tb testing.TB, c stubCase, id string, kind policy.TargetKind, targetID string) policy.Policy {
	tb.Helper()
	return specPolicy(tb, c.typ, id, kind, targetID, actionFor(c.valid), c.valid)
}

// actionFor reads the state a spec declares, so a table row's action always
// agrees with its spec. (The contradiction is tested on purpose elsewhere.)
func actionFor(spec string) policy.Action {
	if strings.Contains(spec, `"state":"deny"`) {
		return policy.Deny
	}
	return policy.Allow
}

// flippedSpec returns the same spec with the state reversed, for the rows that
// must be well-formed on the deny side.
func flippedSpec(c stubCase) string {
	if !c.hasState {
		return c.valid
	}
	return strings.Replace(c.valid, `"state":"allow"`, `"state":"deny"`, 1)
}

func stubReq(c stubCase, agent string) policy.Request {
	r := policy.Request{Type: c.typ, Agent: agent, Loop: "l1", LaneType: "review", MCPServer: "github"}
	c.set(&r, c.value)
	return r
}

// ── 1. each one refuses, and says so differently from a denial ───────────────

func TestEachUnimplementedTypeRefusesAndIsDistinguishableFromAPolicyDeny(t *testing.T) {
	// The D10 shape, once per type. Three answers must stay three:
	//
	//	unimplemented  — nobody can decide this yet  (By == nil, DefaultDeny == false)
	//	policy deny    — a rule refused you           (By != nil)
	//	default deny   — nothing matched              (DefaultDeny == true)
	//
	// Collapsing the first into the second is the failure that costs an operator
	// an hour: they go looking for the policy that refused them, find nothing,
	// and conclude the engine is broken. T11's could-not-determine is the third
	// state, and it exists only if the verdict can already tell them apart.
	for _, c := range stubCases {
		t.Run(string(c.typ), func(t *testing.T) {
			ev, ok := policy.EvaluatorFor(c.typ)
			if !ok {
				t.Fatalf("%s has no evaluator registered; an UNREGISTERED type refuses one step "+
					"earlier than a registered one that declines, and only the second is the shape S10 ships", c.typ)
			}
			row := stubPolicy(t, c, string(c.typ)+"-rule", policy.AgentKind, "main")
			req := stubReq(c, "main")

			v := policy.Evaluate([]policy.Policy{row}, req, evalAt)
			assertVerdict(t, v, false, false, true, "", "")
			if v.Reason == "" || !strings.Contains(v.Reason, strconv.Quote(string(c.typ))) {
				t.Errorf("the refusal %q does not quote the type nobody has implemented", v.Reason)
			}

			// The evaluator refuses to decide rather than to deny: Matched false
			// AND Unimplemented true. Matched true with Unimplemented set would
			// be a refusal that can still grant something.
			res := ev.Match(row, req)
			if res.Matched || !res.Unimplemented {
				t.Errorf("Match(%s) = %+v; a stub decides nothing, which is neither a match nor a denial", c.typ, res)
			}
			if !strings.Contains(res.Note, strconv.Quote(string(c.typ))) {
				t.Errorf("the Note %q does not name the type it cannot decide", res.Note)
			}

			sentence := v.Sentence()
			if !strings.Contains(sentence, string(c.typ)) || !strings.Contains(sentence, req.Subject()) {
				t.Errorf("the D10 sentence %q names neither the type nor the subject; those are the only "+
					"two things a reader can act on when no policy decided", sentence)
			}
			assertDistinguishedFromBothDenials(t, c.typ, sentence)
		})
	}

	t.Run("the four refusals are four distinct sentences", func(t *testing.T) {
		// "Nothing was evaluated for this type" has to say WHICH type, or an
		// operator holding a skill_access rule and a route_access rule cannot
		// tell the two situations apart from one string.
		seen := map[string]policy.PolicyType{}
		for _, c := range stubCases {
			v := policy.Evaluate([]policy.Policy{stubPolicy(t, c, "rule", policy.AgentKind, "main")},
				stubReq(c, "main"), evalAt)
			s := v.Sentence()
			if prior, dup := seen[s]; dup {
				t.Errorf("%s and %s render the identical refusal %q; each must name its own type", prior, c.typ, s)
			}
			seen[s] = c.typ
		}
		if len(seen) != len(stubCases) {
			t.Errorf("%d distinct refusal sentences for %d types", len(seen), len(stubCases))
		}
	})

	t.Run("a stub row cannot be talked into deciding", func(t *testing.T) {
		// Whatever the rows say — an allow, a deny, both together, one targeting
		// everyone — a type nothing decides decides nothing, and never permits.
		for _, c := range stubCases {
			rows := []policy.Policy{
				specPolicy(t, c.typ, "allow-agent", policy.AgentKind, "main", policy.Allow, c.valid),
				specPolicy(t, c.typ, "deny-all", policy.AllAgents, "", policy.Deny, flippedSpec(c)),
			}
			v := policy.Evaluate(rows, stubReq(c, "main"), evalAt)
			if v.Allowed {
				t.Errorf("%s PERMITTED a request with %d authored rows; an unimplemented type must refuse, "+
					"never allow", c.typ, len(rows))
			}
			assertVerdict(t, v, false, false, true, "", "")
		}
	})
}

// assertDistinguishedFromBothDenials is the enumeration's core comparison: the
// refusal for typ must equal neither a real policy deny nor the default deny.
// The two controls are checked against each other too, so the comparison cannot
// be vacuous.
func assertDistinguishedFromBothDenials(tb testing.TB, typ policy.PolicyType, sentence string) {
	tb.Helper()

	deny := toolPolicy(tb, "a-real-deny", policy.AgentKind, "main", policy.Deny,
		`{"toolName":"bash","state":"deny"}`)
	denied := policy.Evaluate([]policy.Policy{deny}, toolReq("main", "bash"), evalAt)
	if denied.By == nil {
		tb.Fatalf("the control deny did not decide; the comparison would be vacuous")
	}
	if denied.Sentence() == sentence {
		tb.Errorf("the refusal of %s renders exactly as a policy denial (%q); they are different facts "+
			"and T11 must be able to tell them apart", typ, sentence)
	}

	defaulted := policy.Evaluate(nil, toolReq("main", "bash"), evalAt)
	if !defaulted.DefaultDeny {
		tb.Fatalf("the control default-deny is not a default deny; the comparison would be vacuous")
	}
	if defaulted.Sentence() == sentence {
		tb.Errorf("the refusal of %s renders exactly as the default deny (%q); \"nobody could decide\" "+
			"and \"nothing matched\" are different answers", typ, sentence)
	}
	if denied.Sentence() == defaulted.Sentence() {
		tb.Errorf("the two controls render identically (%q); the comparison above would prove nothing",
			denied.Sentence())
	}
}

// ── 2. every one of them validates ───────────────────────────────────────────

func TestEachStubSchemaIsExactlyItsPinnedShape(t *testing.T) {
	// "A schema so a policy can be stored and validated today" is the whole
	// difference between an unimplemented type and an unvalidated one. The
	// operator who writes the rule now gets their typo refused NOW, with the
	// field named — not in S17, when the row that has sat inert for two slices
	// finally starts deciding and turns out to have meant nothing at all.
	for _, c := range stubCases {
		t.Run(string(c.typ), func(t *testing.T) {
			if rej := validateWith(t, c.typ, specPolicy(t, c.typ, "good", policy.AgentKind, "main",
				actionFor(c.valid), c.valid)); rej != nil {
				t.Fatalf("the pinned shape %s was refused: %s", c.shape, rej.Reason)
			}

			unknown := strings.TrimSuffix(specWith(c, "x", "1"), "}") + `,"glob":"*"}`
			emptyKey := specWith(c, "", "1")
			spacedKey := specWith(c, "   ", "1")
			missingKey := `{}`
			if c.number != "" {
				missingKey = fmt.Sprintf(`{%q:1}`, c.number)
			}

			type refusalCase struct {
				name    string
				spec    string
				action  policy.Action
				wantErr string
			}
			refusals := []refusalCase{
				{"an unknown field is refused by name", unknown, policy.Allow, `"glob"`},
				{"a missing required field", missingKey, policy.Allow, strconv.Quote(c.key)},
				{"an empty required field", emptyKey, policy.Allow, strconv.Quote(c.key)},
				{"a whitespace-only required field", spacedKey, policy.Allow, strconv.Quote(c.key)},
				{"not an object at all", `"x"`, policy.Allow, "JSON object"},
				{"null", `null`, policy.Allow, "null"},
			}
			if c.hasState {
				refusals = append(refusals,
					refusalCase{`a state outside allow|deny`,
						fmt.Sprintf(`{%q:"x","state":"maybe"}`, c.key), policy.Allow, `"state"`},
					refusalCase{"a state that contradicts the row", specWith(c, "x", "1"), policy.Deny, `"deny"`},
					refusalCase{"a missing state", fmt.Sprintf(`{%q:"x"}`, c.key), policy.Allow, `"state"`},
				)
			} else {
				// CostCap has no state: an allow IS the grant and the presence of
				// the row IS the cap, so a state field is not a redundant copy of
				// a fact — it is a fact nobody asked for.
				refusals = append(refusals,
					refusalCase{`a state field is unknown here`, fmt.Sprintf(`{%q:"x","max":1,"state":"allow"}`, c.key),
						policy.Allow, `"state"`},
					refusalCase{"a missing cap", fmt.Sprintf(`{%q:"x"}`, c.key), policy.Allow, `"max"`},
					refusalCase{"a negative cap", fmt.Sprintf(`{%q:"x","max":-1}`, c.key), policy.Allow, "negative"},
					refusalCase{"a cap that is not a number at all", fmt.Sprintf(`{%q:"x","max":lots}`, c.key),
						policy.Allow, "number"},
					refusalCase{"a cap of the wrong JSON type", fmt.Sprintf(`{%q:"x","max":true}`, c.key),
						policy.Allow, "number"},
					refusalCase{"a cap quoted as a string", fmt.Sprintf(`{%q:"x","max":"1000"}`, c.key),
						policy.Allow, "number"},
					refusalCase{"a cap that is not finite", fmt.Sprintf(`{%q:"x","max":1e999}`, c.key),
						policy.Allow, "finite"},
					refusalCase{"a fractional cap", fmt.Sprintf(`{%q:"x","max":0.5}`, c.key),
						policy.Allow, ""}, // accepted: a cap is a number, not an integer
				)
			}

			for _, r := range refusals {
				t.Run(r.name, func(t *testing.T) {
					rej := validateWith(t, c.typ,
						specPolicy(t, c.typ, "bad", policy.AgentKind, "main", r.action, r.spec))
					if r.wantErr == "" {
						if rej != nil {
							t.Fatalf("refused a spec that is valid (%s): %s", r.spec, rej.Reason)
						}
						return
					}
					if rej == nil {
						t.Fatalf("accepted %s (%s)", r.name, r.spec)
					}
					if !strings.Contains(rej.Reason, r.wantErr) {
						t.Errorf("the refusal %q does not name %q", rej.Reason, r.wantErr)
					}
					if !strings.Contains(rej.Reason, c.shape) {
						t.Errorf("the refusal %q does not quote the shape it wanted (%s)", rej.Reason, c.shape)
					}
				})
			}

			if c.number != "" {
				// Zero is a real cap. "spend nothing" is a decision, and it is not
				// the same policy as the absence of a row — refusing zero would
				// push operators towards deleting the rule instead of tightening it.
				if rej := validateWith(t, c.typ, specPolicy(t, c.typ, "zero", policy.AgentKind, "main",
					policy.Allow, fmt.Sprintf(`{%q:"%s","max":0}`, c.key, c.value))); rej != nil {
					t.Errorf("a zero cap was refused: %s", rej.Reason)
				}
			}
		})
	}
}

func TestThePinnedSpecShapesMatchTheirGoStructs(t *testing.T) {
	// The schema lock — the house pattern helper/plai-backend applies to every
	// wire type. The shape string in each refusal is typed by hand and the
	// accepted keys are derived from the Go struct; two descriptions of one
	// thing drift, so every declared key must appear in the shape, in
	// declaration order, and a cap must be described as a number. A rename that
	// updates the struct and not the prose fails here.
	quotedKey := regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*)"\s*:`)
	// All seven, from four homes: ToolAccess's row is spelled here, the four
	// stubs come from stubCases, CommandAccess's row comes from
	// spec_commandaccess_test.go beside the struct it describes, and
	// SkillAccess's from spec_skillaccess_test.go (S14). A filled type leaving
	// the stub table must not also leave the lock — the shape an operator
	// writes and the code that reads it are still one thing.
	all := append([]stubCase{
		{
			typ: policy.ToolAccess, shape: `{"toolName": string, "state": "allow"|"deny"|"ask"}`,
			key: "toolName", value: "bash", hasState: true,
			valid: `{"toolName":"bash","state":"allow"}`,
			set:   func(r *policy.Request, v string) { r.Tool = v },
		},
		commandAccessCase,
		skillAccessCase,
	}, stubCases...)

	for _, c := range all {
		t.Run(string(c.typ), func(t *testing.T) {
			schema := schemaOf(t, c.typ)
			if schema.Type != c.typ {
				t.Errorf("the validator reports type %q, want %q", schema.Type, c.typ)
			}
			if schema.Shape != c.shape {
				t.Errorf("the shape the refusals quote is %q, want %q", schema.Shape, c.shape)
			}
			var described []string
			for _, m := range quotedKey.FindAllStringSubmatch(schema.Shape, -1) {
				described = append(described, m[1])
			}
			if len(described) == 0 {
				t.Fatalf("the shape %s names no fields at all", schema.Shape)
			}
			var declared []string
			for _, k := range schema.Keys {
				declared = append(declared, k.Name)
			}
			if !slices.Equal(described, declared) {
				t.Errorf("the shape describes %v but the Go struct declares %v", described, declared)
			}
			for _, k := range schema.Keys {
				asNumber := fmt.Sprintf("%q: number", k.Name)
				if k.Number != strings.Contains(schema.Shape, asNumber) {
					t.Errorf("field %q has Number=%v but the shape renders it that way: %v",
						k.Name, k.Number, strings.Contains(schema.Shape, asNumber))
				}
			}
			if len(schema.Keys) < 2 {
				t.Errorf("%s declares %d spec keys; a one-field schema cannot express a rule, and an "+
					"empty one accepts nothing — which decode relies on", c.typ, len(schema.Keys))
			}
		})
	}
}

// schemaOf asks a registered validator for the shape it enforces: the
// SchemaCarrier seam T6 and `aeon policy add` will use to tell an operator what
// to write, instead of a help string copied from the code and drifting from it.
func schemaOf(tb testing.TB, typ policy.PolicyType) policy.SpecSchema {
	tb.Helper()
	v, ok := policy.ValidatorFor(typ)
	if !ok {
		tb.Fatalf("%s has no validator", typ)
	}
	carrier, ok := v.(policy.SchemaCarrier)
	if !ok {
		tb.Fatalf("%s's validator %T does not carry its schema; every validator this package installs "+
			"does, so a validator from outside the package has slipped in", typ, v)
	}
	return carrier.SpecSchema()
}

// ── 3. the registry is the seam, and nothing else is ─────────────────────────

func TestTheRegistryCoversEveryType(t *testing.T) {
	// PL3's upward seam, checked instead of trusted: all seven registered with
	// both halves, and production code registering nothing outside the seven.
	// A gap is a type whose policies silently never fire.
	types := policy.AllPolicyTypes()
	if len(types) != 7 {
		t.Fatalf("%d policy types; the closed set is seven, so the enumeration below is not the whole set",
			len(types))
	}
	for _, typ := range types {
		t.Run(string(typ), func(t *testing.T) {
			if _, ok := policy.EvaluatorFor(typ); !ok {
				t.Errorf("%s has no evaluator registered; a policy of it will never be decided", typ)
			}
			if _, ok := policy.ValidatorFor(typ); !ok {
				t.Errorf("%s has no validator registered; its spec would be stored unchecked", typ)
			}
			schema := schemaOf(t, typ)
			if schema.Type != typ || len(schema.Keys) == 0 || strings.TrimSpace(schema.Shape) == "" {
				t.Errorf("%s's schema is incomplete: %+v", typ, schema)
			}
		})
	}

	// What production code registers is exactly the seven, by constant name:
	// nothing missing, nothing invented, nothing computed. A computed first
	// argument would land here as "<not-a-constant>" and fail the comparison —
	// a type nobody can enumerate is a type nobody can audit.
	wantNames := []string{"CommandAccess", "CostCap", "EffectClass", "RouteAccess",
		"SkillAccess", "ToolAccess", "TriggerAccess"}
	if got := registeredTypeNames(t); !slices.Equal(got, wantNames) {
		t.Errorf("production code registers %v, want exactly %v; a type registered from outside the "+
			"closed set is a permission nobody granted, and a missing one is a policy that never fires",
			got, wantNames)
	}
}

// ── 4. a half-finished registration refuses, it does not panic ───────────────

// The shapes SEC-009 is about, as fixtures. Each satisfies an interface while
// carrying nothing, so a bare `x == nil` test would let each through to a
// method call.
const (
	testNilPtrType  policy.PolicyType = "test_nil_ptr"
	testNilFuncType policy.PolicyType = "test_nil_func"
)

// nilPtrEvaluator dereferences its receiver, which is what a real evaluator
// does with every field it reads.
type nilPtrEvaluator struct{ n int }

func (e *nilPtrEvaluator) Match(policy.Policy, policy.Request) policy.MatchRes {
	return policy.MatchRes{Matched: e.n == 0}
}

type nilPtrValidator struct{ n int }

func (v *nilPtrValidator) Validate(policy.Policy) *policy.Reject {
	if v.n < 0 {
		return &policy.Reject{Reason: "negative"}
	}
	return nil
}

// nilFuncEvaluator calls the func value it is, which a nil one cannot do.
type nilFuncEvaluator func(policy.Policy, policy.Request) policy.MatchRes

func (f nilFuncEvaluator) Match(p policy.Policy, r policy.Request) policy.MatchRes { return f(p, r) }

// Registered from init, as every registration must be (TestRegisterTypeIsCodeTimeOnly).
func init() {
	policy.RegisterType(testNilPtrType, (*nilPtrValidator)(nil), (*nilPtrEvaluator)(nil))
	policy.RegisterType(testNilFuncType, nil, nilFuncEvaluator(nil))
}

func TestATypedNilEvaluatorRefusesRatherThanPanicking(t *testing.T) {
	// SEC-009. An interface holding a nil pointer is not == nil, so before T5's
	// normalisation a typed-nil evaluator passed for a registered type and then
	// panicked the daemon on the first request of that type. A guard that fails
	// STOP is still a guard that fails, and this one fails inside a step.
	for _, typ := range []policy.PolicyType{testNilPtrType, testNilFuncType} {
		t.Run(string(typ), func(t *testing.T) {
			if _, ok := policy.EvaluatorFor(typ); ok {
				t.Error("EvaluatorFor reports a typed-nil evaluator as available")
			}
			if _, ok := policy.ValidatorFor(typ); ok {
				t.Error("ValidatorFor reports a typed-nil validator as available")
			}
			// not-ok is only useful because the engine's answer to it is a
			// refusal that permits nothing (D10).
			v := policy.Evaluate([]policy.Policy{
				specPolicy(t, typ, "would-allow", policy.AgentKind, "main", policy.Allow,
					`{"toolName":"bash","state":"allow"}`),
			}, policy.Request{Type: typ, Agent: "main", Tool: "bash"}, evalAt)
			assertVerdict(t, v, false, false, true, "", "")
		})
	}

	t.Run("the panic it prevents is real", func(t *testing.T) {
		// Without this, the test above might be proving nothing was ever there
		// to panic. Each fixture genuinely dies on a nil receiver.
		for name, call := range map[string]func(){
			"nil pointer evaluator": func() { var e *nilPtrEvaluator; e.Match(policy.Policy{}, policy.Request{}) },
			"nil pointer validator": func() { var v *nilPtrValidator; v.Validate(policy.Policy{}) },
			"nil func evaluator":    func() { var f nilFuncEvaluator; f.Match(policy.Policy{}, policy.Request{}) },
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("%s did not panic on a nil receiver; the normalisation it tests is unproven", name)
					}
				}()
				call()
			}()
		}
	})

	t.Run("a real evaluator is still stored as given", func(t *testing.T) {
		// The normalisation must not be a filter that drops working types.
		if _, ok := policy.EvaluatorFor(policy.ToolAccess); !ok {
			t.Error("ToolAccess's evaluator was normalised away")
		}
		if _, ok := policy.ValidatorFor(policy.CostCap); !ok {
			t.Error("CostCap's validator was normalised away")
		}
	})
}

// ── DEF-016: the fifth return path ───────────────────────────────────────────

func TestTheSawUnimplementedPathCarriesTheSubjectAndTheType(t *testing.T) {
	// evaluate.go sets Subject and Type through one closure every return path
	// passes through. Four of the five paths had a test pinning it; the fifth —
	// the `sawUnimplemented` return, reached when a registered evaluator
	// declines to decide — had none, and deleting about() from just that return
	// left the whole suite green while the D10 line degraded to
	// "refused: agent: · unnamed type has no evaluator yet". S10's T5 registered
	// the placeholder evaluators whose normal answer IS that path, so the pin
	// belongs here, and it comes with a control that shows the pin can fail.
	// (The sweep below walks only the types that still have a placeholder: a
	// filled type like CommandAccess answers the path with a decision, and its
	// own file pins that.)
	type pathCase struct {
		name string
		typ  policy.PolicyType
		req  policy.Request
		spec json.RawMessage
	}
	cases := []pathCase{
		{"the test stub type, inside a loop", testStubType,
			policy.Request{Type: testStubType, Agent: "main", Loop: "l1", Tool: "bash"}, keySpec("bash")},
		{"the test stub type, as an agent", testStubType,
			policy.Request{Type: testStubType, Agent: "main", Tool: "bash"}, keySpec("bash")},
	}
	for _, c := range stubCases {
		cases = append(cases, pathCase{string(c.typ) + " through the production registry",
			c.typ, stubReq(c, "main"), json.RawMessage(c.valid)})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target, rej := policy.ParseTarget(policy.AgentKind, "main")
			if rej != nil {
				t.Fatalf("helper target: %s", rej.Reason)
			}
			row := policy.Policy{ID: "the-rule", Type: c.typ, Target: target,
				Action: policy.Deny, Spec: c.spec, Source: policy.Operator}

			v := policy.Evaluate([]policy.Policy{row}, c.req, evalAt)
			assertVerdict(t, v, false, false, true, "", "")
			if v.Subject != c.req.Subject() {
				t.Errorf("Subject = %q, want %q; the refusal was about somebody, and the verdict is the "+
					"only thing the caller keeps", v.Subject, c.req.Subject())
			}
			if v.Type != c.typ {
				t.Errorf("Type = %q, want %q; the D10 sentence names what was asked about and this path has "+
					"no policy to read it off", v.Type, c.typ)
			}
			s := v.Sentence()
			for _, want := range []string{c.req.Subject(), string(c.typ)} {
				if !strings.Contains(s, want) {
					t.Errorf("the sentence %q dropped %q; a refusal that cannot say who, or what, is not "+
						"actionable (PL8)", s, want)
				}
			}
			if strings.Contains(s, "unnamed type") {
				t.Errorf("the sentence %q fell back to the placeholder, so Type did not travel", s)
			}

			// The falsifiability control, in-test: those two fields must be what
			// makes this sentence what it is. A verdict with the same Reason and
			// no subject or type renders something else — so the assertions above
			// are load-bearing, not decoration any verdict would satisfy.
			bare := policy.Verdict{Unimplemented: true, Reason: v.Reason}
			if bare.Sentence() == s {
				t.Errorf("a verdict with no Subject and no Type renders the same sentence as one with both; "+
					"the pin above could not fail: %q", s)
			}
		})
	}
}
