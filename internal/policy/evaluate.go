package policy

import (
	"fmt"
	"reflect"
	"time"
)

// Request is the whole question the evaluator is asked: which type is being
// decided, who is asking, and what they want to do.
//
// # One struct for all seven types (Q4 — answered: yes, one struct)
//
// The alternative was seven per-type request shapes, or seven Evaluate
// functions, and both were rejected for the same reason: the totality of the
// evaluator — every path returns a Verdict, never a Reject, never an error —
// is provable because there is exactly one function to prove it over. Seven
// entry points is seven contracts, seven purity audits, and seven places for
// the deny-wins order to drift out of sync with the sixth. So one struct, with
// optional per-type fields: exactly one of the value fields is meaningful for
// a given Type, and it is the registered Evaluator's job — not the engine's —
// to know which. A field left empty for a type that ignores it costs nothing
// and means nothing.
//
// The identity fields (Agent, Loop, LaneType, MCPServer) are what a policy's
// Target matches against, one per target kind. They are strings rather than a
// Target because a request is not a target: it names who is asking under every
// kind at once, and it is the policy that decides which of those it governs.
type Request struct {
	// Type selects the registered evaluator and the meaningful value field.
	Type PolicyType

	// Identity fields: which of these a policy's target matches on.
	Agent     string
	Loop      string
	LaneType  string
	MCPServer string

	// Per-type values; exactly one is meaningful, chosen by Type.
	Tool    string // ToolAccess
	Command string // CommandAccess
	Route   string // RouteAccess
	Effect  string // EffectClass
	Cost    string // CostCap (token name; value checks live with S17's evaluator)
	Skill   string // SkillAccess
	Trigger string // TriggerAccess

	// ToolAddress is the tool's S74 address (`<category>.<tool>.<access>`) when
	// the caller knows it; when empty, the engine asks the resolver seam for a
	// system tool's. A tool whose address cannot be resolved matches bare-name
	// rows only, never an address pattern (absent is not zero).
	ToolAddress string
}

// canAskByDefault reports whether the unmatched default may be an ask for this type.
func canAskByDefault(t PolicyType) bool { return t == ToolAccess || t == CommandAccess }

// EvaluateAskingWhenUnmatched is Evaluate with one amendment to PL5, for the two types a person can
// answer in the moment (a tool call, a shell command): when NOTHING matched, the decision is to ask
// instead of to refuse. It changes only that fall-through. A deny still denies, an allow still allows,
// an explicit ask is unchanged, and the fail-closed answers (an unknown type, an evaluator that could
// not decide) stay refusals. The caller chooses this function only on the ordinary path, when the
// settings say "ask"; a gate that could not read its set calls plain Evaluate and still denies.
func EvaluateAskingWhenUnmatched(ps []Policy, req Request, at time.Time) Verdict {
	v := Evaluate(ps, req, at)
	if v.DefaultDeny && canAskByDefault(req.Type) {
		v.DefaultDeny = false
		v.Asked = true
		v.DefaultAsk = true
	}
	return v
}

// Subject renders who the request is about, in the canonical subject form the
// verdicts use (PL8): the loop when the request was made inside one, the agent
// otherwise. A loop-scoped request still knows its agent, but the loop is the
// more specific answer to "who is this about" — a rule about this loop outranks
// a rule about the agent in general.
func (r Request) Subject() string {
	if r.Loop != "" {
		return "loop:" + r.Loop
	}
	return "agent:" + r.Agent
}

// MatchRes is an evaluator's answer about one policy: does its spec match what
// this request wants to do.
type MatchRes struct {
	// Matched is the decision half: the spec names this request.
	Matched bool

	// Unimplemented says the evaluator could not make this kind of decision
	// yet — it is a refusal to decide, NOT a denial, and the verdict built
	// from it says so (D10/PL12). A later slice's evaluator answers instead.
	// The flag is meaningful when Matched is false: a decision actually made
	// (Matched true) decides whatever the flag says, so a registered type can
	// never be talked into Unimplemented by a policy that matched it.
	Unimplemented bool

	// Note is an evaluator-supplied diagnostic for logs. It never reaches a
	// Verdict — the verdict's Reason holds fixed strings only — and it must
	// never carry policy content or secrets, the same rule as every detail
	// field in this slice.
	Note string
}

// Evaluator answers the per-type half of the question: whether one policy's
// spec matches one request. Implementations are pure functions of their two
// arguments — the same rule the package itself is held to (D5) — and they are
// never handed a policy whose type they did not register for.
type Evaluator interface {
	// Match reports whether p's spec names what r wants to do.
	Match(p Policy, r Request) MatchRes
}

// Validator is the per-type half of the write boundary: does this policy's
// spec mean anything for this type. It is consulted on the write path (T5/T6),
// never during Evaluate, which must accept any stored policy and decide, not
// refuse.
type Validator interface {
	// Validate returns nil for a spec the type accepts, and a refusal naming
	// what it rejected otherwise.
	Validate(p Policy) *Reject
}

// registry is the seam every later slice extends by registering rather than
// by editing a switch (PL3): S11/S14/S16/S17/S18/S20 add a type by arriving
// with its schema, validator and evaluator, and the engine never learns the
// name of the new type in code.
//
// It is a map, and that is the whole of the D10 argument at the registry
// level: a lookup either finds an evaluator or returns not-ok, and not-ok
// refuses. There is no default branch anywhere on this path because there is
// nowhere for one to go. RegisterType is called from code — a package's own
// init, as T5 will do — and never at runtime: the map is read unsynchronised
// by every Evaluate, so a runtime registration is a data race and a review
// rejection.
var registry = map[PolicyType]struct {
	Eval Evaluator
	Val  Validator
}{}

// RegisterType installs one type's validator and evaluator. Called from code
// (a package's own init, as T5's two spec files do), never at runtime: the map
// is read unsynchronised by every Evaluate, so a runtime registration is a data
// race — TestRegisterTypeIsCodeTimeOnly greps the module for it rather than
// trusting the comment (SEC-010).
//
// A nil evaluator — plain nil OR an interface holding a nil pointer, a nil map,
// a nil slice, a nil channel or a nil func — is normalised to plain nil on the
// way in, so EvaluatorFor reports not-ok and the type refuses (D10) instead of
// being called on a receiver that would panic (SEC-009: `e == nil` alone misses
// every typed nil, and a panic in the guard is a daemon that stops). The same
// normalisation covers the validator, so ValidatorFor cannot hand back a half
// that dereferences to nothing. Failing to finish a registration therefore
// costs a refusal, never a permission and never a crash: registering is not the
// same as deciding.
func RegisterType(t PolicyType, v Validator, e Evaluator) {
	registry[t] = struct {
		Eval Evaluator
		Val  Validator
	}{Eval: usableEvaluator(e), Val: usableValidator(v)}
}

// usableEvaluator normalises a typed nil to plain nil. See RegisterType.
func usableEvaluator(e Evaluator) Evaluator {
	if isNilish(e) {
		return nil
	}
	return e
}

// usableValidator normalises a typed nil to plain nil. See RegisterType.
func usableValidator(v Validator) Validator {
	if isNilish(v) {
		return nil
	}
	return v
}

// isNilish reports whether an interface value carries nothing — either the
// plain nil, or a nil of some concrete type that satisfies the interface. The
// second is the one a bare `== nil` test waves through: an interface holding a
// nil pointer is not equal to nil, so it passes for a registered evaluator
// until the day something calls a method on it.
func isNilish(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// ValidatorFor returns the validator registered for t, reporting false when
// the type is unregistered or registered without one.
func ValidatorFor(t PolicyType) (Validator, bool) {
	r, ok := registry[t]
	if !ok || r.Val == nil {
		return nil, false
	}
	return r.Val, true
}

// EvaluatorFor returns the evaluator registered for t, reporting false when
// the type is unregistered or registered with a nil evaluator. Evaluate and
// every caller ask the same question with this, so "is this type decided" has
// exactly one answer.
func EvaluatorFor(t PolicyType) (Evaluator, bool) {
	r, ok := registry[t]
	if !ok || r.Eval == nil {
		return nil, false
	}
	return r.Eval, true
}

// Verdict is the engine's answer, and the only thing a caller gets.
//
// It is a value, not an error: the evaluator is total (PL6), so every input
// produces one of these and there is no failure channel to check. A caller
// that branches on error would be handling a case that cannot happen, and the
// test that a signature has no error return is what keeps anyone from adding
// one.
//
// The nine fields are the whole shape the surfaces (T4's sentence, the CLI,
// the socket, the app) render from. By and OverriddenAllow alias elements of
// the caller's own policy slice — the verdict names which rules decided, it
// does not copy them — so treat them as read-only views of the input.
//
// Subject and Type are copied off the Request on every return path, for one
// reason: Sentence() is a method on the Verdict, so a verdict that does not
// carry who asked and what about can only be rendered by a caller that still
// holds the Request — and a caller that re-supplies a request is a caller that
// can supply a different one. The D10 line names the type and the PL5 line
// names the subject, and neither has a policy to read either off (By is nil on
// both paths). They are the verdict's, not the caller's.
//
// Sentence() is deliberately NOT declared here: verdict.go (T4) is the one
// formatter every surface uses, and a method stubbed in this file is a second
// rendering waiting to drift.
type Verdict struct {
	// Subject is Request.Subject() as it stood when the decision was made:
	// `loop:<slug>` inside a loop, `agent:<name>` otherwise. A refusal is
	// about somebody; a sentence that cannot say who is not actionable (PL8).
	Subject string

	// Type is Request.Type — what was asked about. A decided verdict could
	// read it off By (By.Type always equals it: Evaluate drops every other
	// type before matching), but the two refusals that name no policy have
	// nowhere else to get it from.
	Type PolicyType

	// Allowed is true only when an Allow matched and no Deny or Ask did.
	Allowed bool

	// Asked is the third decision: a rule matched and routed the question to
	// a human. Allowed is FALSE for an asked verdict — nobody may treat it as
	// a permission — and DefaultDeny and Unimplemented are false too, because
	// it is a decision the engine made, distinguishable from a denial, from a
	// default deny, and from "the binary cannot decide" (T11's three-state
	// answer depends on those being FOUR shapes, not three).
	Asked bool

	// DefaultAsk is true when the ask came from the fall-through, not from a rule: nothing matched
	// and the settings say to ask. By is nil. It is a different fact from an explicit ask rule, and
	// the sentence says so — "no rule covers this" is the answer to "why am I being asked".
	DefaultAsk bool

	// By is the policy that decided the verdict: the deny that fired, or the
	// allow that was allowed, chosen by the specificity rule documented at
	// mostSpecific — never by slice position, which a caller cannot control.
	By *Policy

	// OverriddenAllow is set exactly when a deny beat a matching allow: the
	// allow the denial overrode. This is what makes PL4 visible to a human
	// instead of surprising ("I allowed that!"), and T4 renders it. It is the
	// most specific matching allow under the same determinism rule as By.
	OverriddenAllow *Policy

	// DefaultDeny is PL5: nothing matched, so nothing is permitted. It is a
	// decision — the engine decided to permit nothing — and it is set only
	// when the engine could decide, i.e. never alongside Unimplemented.
	DefaultDeny bool

	// Unimplemented is D10: the registry has no evaluator for req.Type (or
	// the registered one refused to decide), so the engine refuses rather
	// than permits. It is distinguishable from a policy deny (By == nil) and
	// from the default deny (DefaultDeny == false), because "nobody can
	// answer yet" must never render as "a rule refused you" — T11's third
	// state depends on that distinction.
	Unimplemented bool

	// Reason holds fixed strings only — no policy content, no request
	// content, nothing that came from a spec (legal constraint 1, and the
	// reason Note never arrives here). It is set only on the unimplemented
	// paths; every other rendering is verdict.go's job (T4), which is why a
	// deny and an allow carry no prose.
	Reason string
}

// Evaluate is the engine: given the whole policy set, one request, and the
// instant the decision is made, it returns the verdict.
//
// # Total (PL6)
//
// Every input produces a Verdict. It never returns a Reject, never an error,
// never panics on a bad spec — a policy nothing matches is dropped, and a set
// in which nothing matches denies. A guard that can fail open is not a guard,
// and the failure this design is defending against is the evaluation-time
// surprise: a malformed row must not become either a panic in the runtime or,
// worse, a permission.
//
// # Pure (D5)
//
// The clock arrives as at and nothing else: no time.Now, no environment, no
// store, no network (asserted by TestTheEvaluatorIsPure, not promised here).
// That parameter is what makes the expiry boundary testable at
// NotAfter±1ns without a sleep, which is what makes the boundary provable.
//
// # The order of operations, and it is not negotiable
//
//  1. no registered evaluator for req.Type → refuse (Unimplemented), do not
//     permit; nothing else can be said about a type the binary cannot
//     evaluate, so this runs before ps is even read (D10);
//  2. drop every policy `expired` says has stopped existing at `at` — the
//     predicate is a function of the row's NotAfter and this instant, and it
//     is shared with the in-force answer so the two cannot disagree about
//     whether a rule is alive at the same moment (D6/D7: an expired deny does
//     not linger as a phantom denial, and does not fall through to an allow
//     either; the default takes over);
//  3. drop every policy whose target does not name this request (AllAgents
//     matches everything; every other kind compares its ID to the identity
//     field its kind governs);
//  4. ask the registered Evaluator whether the spec matches; non-matches
//     dropped;
//  5. any Deny remaining → denied. The verdict carries the deny (By) and the
//     allow it overrode (OverriddenAllow) — effective = (union of allows) −
//     (union of denies); specificity NEVER rescues an allow (D1/PL4), so a
//     broad deny stands over a narrow allow no matter which was written
//     first or how specific the allow is;
//  6. else any Ask → asked (S11's D1). The middle tier: deny > ask > allow.
//     The verdict carries the ask (By, chosen by the same determinism rule)
//     and the allow it outranked (OverriddenAllow) — a rule that routes to a
//     human outranks a rule that does not, and the narrower grant may not
//     silently close the door a broader rule opened onto a person;
//  7. else any Allow → allowed, and no deny or ask beat it;
//  8. else DefaultDeny (PL5).
//
// Policies whose Type is not req.Type are inert for this request: they are
// other questions' answers. Only req.Type's registration is load-bearing.
func Evaluate(ps []Policy, req Request, at time.Time) Verdict {
	// Every verdict carries who asked and what about, taken from the request
	// and nowhere else, and EVERY return path goes through about() so the two
	// fields cannot be forgotten by a later branch: the refusals that name no
	// policy (Unimplemented, DefaultDeny) have no By to read them off, and the
	// verdict outlives the request in the caller's hands — the runtime keeps
	// it, the store records it, the CLI prints it — so the answer has to travel
	// inside the verdict, which is the only thing a caller keeps (T4 renders it
	// from there and never asks the caller to re-supply a Request).
	about := func(v Verdict) Verdict {
		v.Subject, v.Type = req.Subject(), req.Type
		return v
	}

	ev, ok := EvaluatorFor(req.Type)
	if !ok {
		// D10, before anything else: an unregistered type is not permitted,
		// not defaulted, not skipped. It refuses, and says so in the shape
		// T11 renders as "could not determine" rather than "denied".
		return about(Verdict{Unimplemented: true,
			Reason: fmt.Sprintf("no evaluator for %q yet", req.Type)})
	}

	var denies, asks, allows []*Policy
	sawUnimplemented := false

	for i := range ps {
		p := &ps[i]
		if p.Type != req.Type {
			continue // another type's rule cannot name this request
		}
		if expired(*p, at) {
			continue // expired is gone, not denied (D6): it stops existing
		}
		if !targetMatches(p.Target, req) {
			continue
		}
		res := ev.Match(*p, req)
		if res.Unimplemented && !res.Matched {
			// A refusal to decide is recorded; it decides nothing, but it
			// means the verdict, if there is one to make, must not claim the
			// engine decided. (Matched true is a decision — it stands.)
			sawUnimplemented = true
		}
		if !res.Matched {
			continue
		}
		switch p.Action {
		case Deny:
			denies = append(denies, p)
		case Ask:
			asks = append(asks, p)
		case Allow:
			allows = append(allows, p)
		}
		// No default: an action outside the closed set (a row that skipped
		// it) matches nothing and permits nothing — inert, toward deny,
		// which is the only safe direction (D10).
	}

	if len(denies) > 0 {
		v := about(Verdict{By: mostSpecific(denies)})
		if len(allows) > 0 {
			// D1's visible half: the denial names the allow it beat, the
			// most specific one, so "the most specific allow loses to the
			// broadest deny" is auditable from the verdict alone.
			v.OverriddenAllow = mostSpecific(allows)
		}
		return v
	}
	if len(asks) > 0 {
		v := about(Verdict{Asked: true, By: mostSpecific(asks)})
		if len(allows) > 0 {
			// Visible for the same reason OverriddenAllow is on a deny:
			// "but I had an allow!" is answered by the sentence, not by an
			// operator reading the table. The ask outranks the grant; the
			// human being asked is the answer.
			v.OverriddenAllow = mostSpecific(allows)
		}
		return v
	}
	if len(allows) > 0 {
		return about(Verdict{Allowed: true, By: mostSpecific(allows)})
	}
	if sawUnimplemented {
		return about(Verdict{Unimplemented: true,
			Reason: fmt.Sprintf("the evaluator for %q could not decide it yet", req.Type)})
	}
	// PL5: nothing matched. That is a decision to permit nothing.
	return about(Verdict{DefaultDeny: true})
}

// targetMatches is the target half of the filter: a policy governs this
// request only if its target names it. AllAgents governs every agent, present
// and future, so it matches everything; every other kind compares its ID to
// the one identity field its kind governs.
//
// A named-kind target with an empty ID matches nothing, even against an
// empty request field. ParseTarget makes that shape unconstructable through
// the parser, but Evaluate is handed stored rows, and a target that names
// nobody must not read as a target that names everyone — the equality alone
// would let a corrupt agent-kind row match an agent-less request. Refuse,
// never permit (D10).
//
// An unknown kind matches nothing. The switch has no default branch that
// could match something: the kinds are five, and the sixth does not evaluate.
func targetMatches(t Target, r Request) bool {
	switch t.Kind {
	case AllAgents:
		return true
	case AgentKind:
		return t.ID != "" && t.ID == r.Agent
	case LoopKind:
		return t.ID != "" && t.ID == r.Loop
	case LaneTypeKind:
		return t.ID != "" && t.ID == r.LaneType
	case MCPServerKind:
		return t.ID != "" && t.ID == r.MCPServer
	}
	return false
}

// expired is the ONE expiry rule: a policy has stopped existing at `at` when
// it carries a NotAfter that is not strictly after `at`. The comparison is the
// claim, so it is spelled once, here, and everyone asks this one — Evaluate
// above, and the in-force answer (inforce.go), which must not be able to call
// a rule enforced at the same instant the engine has already dropped:
//
//   - a nil NotAfter never expires (PL7 — no expiry is a decision, not a
//     far-future sentinel; a sentinel is a rule that silently lapses on a day
//     nobody chose, and the lapse presents as a bug in whatever it guarded);
//   - `at` exactly equal to NotAfter is expired, `at` one nanosecond earlier
//     is alive. That boundary is what T3 proved at the nanosecond, and it is
//     the same one a surface explores with an instant parameter — two
//     spellings of it would be two answers about whether one rule is alive at
//     one moment (D6/D7: expired is gone, not denied, and it does not fall
//     through to an allow either).
func expired(p Policy, at time.Time) bool {
	return p.NotAfter != nil && !at.Before(*p.NotAfter)
}

// mostSpecific picks which matching policy the verdict names, and it is the
// determinism rule for both By and OverriddenAllow: a verdict must not depend
// on the order the caller's slice happened to arrive in, because slice order
// is nobody's decision and a verdict that moved when a row was appended is a
// verdict nobody can trust.
//
// The rule, in order:
//
//   - target-specific beats all-agents — the rule that named this subject
//     specifically is the one worth naming in a verdict (it is also the
//     interesting half of D1: when a broad deny overrides allows, the allow
//     shown is the most specific thing that was lost);
//   - ties break by policy ID, lexicographically smallest first — IDs are
//     unique in the table, so the order is total and every caller, every run,
//     agrees on which policy the verdict names.
func mostSpecific(ps []*Policy) *Policy {
	best := ps[0]
	for _, p := range ps[1:] {
		bp, bb := specificity(p), specificity(best)
		if bp > bb || (bp == bb && p.ID < best.ID) {
			best = p
		}
	}
	return best
}

// specificity is the order mostSpecific compares: a target that names a
// subject outranks one that names everyone.
func specificity(p *Policy) int {
	if p.Target.Kind == AllAgents {
		return 0
	}
	return 1
}
