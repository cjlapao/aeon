package policy

import "strings"

// spec_routeaccess.go — S21 fills the RouteAccess placeholder (T4). The
// schema, validator and spec struct stay beside their siblings in
// spec_stubs.go, exactly as SkillAccess's did at S14: an operator could write
// a `route_access` row TODAY against the pinned shape, and the moment this
// file linked in, the engine started honouring what was already stored.
//
// **The subject is extended; no new type arrives (RA4).** "Never send code to
// a non-local route" was sayable about model routes; S21 makes the same
// sentence sayable about AGENTS, and the extension is in the VALUE the
// request carries (`Route: "remote:docs-peer"`), not in a new constant — a
// second access type would be a second authorship story, a second audit
// vocabulary, and a second thing an operator must remember to lock. One type,
// one formatter, one write boundary.
//
// What a route is, in this evaluator: anything routable the request can name
// — model routes as S18 names them, and remote peers under the `remote:`
// namespace (the canonical spelling is `EgressSubject` in
// internal/remote/egress.go; policy never imports remote, so the shape is
// documented on both sides and pinned by remote's tests). Wildcards take
// CommandAccess's shape, not ToolAccess's: a **single trailing `*`**, because
// a route is a name, not a command line, and there is nothing to segment.

// routeAccessEvaluator is the S21 real evaluator, replacing
// stubEvaluator{RouteAccess}: Matched means a rule SPOKE, and the engine
// folds deny over ask over allow (PL4) — this file decides nothing on its
// own, which is the whole discipline.
type routeAccessEvaluator struct{}

// Match reports whether p's spec names the route this request wants.
//
// The order is the argument, as in every sibling: read the row; ask whether
// the request is even about this type; reject rows that contradict themselves
// (spec state vs row action); reject rows that name nothing — an empty spec
// route matching an empty request route is how every unnamed call inherits
// every grant; reject patterns that are not in the single-trailing-star form
// (a pattern nobody can reason about is a grant nobody typed); only then
// compare.
func (routeAccessEvaluator) Match(p Policy, r Request) MatchRes {
	var spec RouteAccessSpec
	if rej := routeAccessSchema.decode(p.Spec, &spec); rej != nil {
		return MatchRes{Note: "the spec is not the pinned RouteAccess shape"}
	}
	if r.Type != RouteAccess {
		// The engine never hands an evaluator another type's request; if it
		// ever did, the answer is still "not me", never "yes".
		return MatchRes{Note: "the request asks about a different type"}
	}
	if strings.TrimSpace(spec.Route) == "" {
		return MatchRes{Note: "an empty route reference matches nothing"}
	}
	if spec.State != string(p.Action) {
		// A row the write boundary would never have admitted. Choosing either
		// half of the contradiction would invent an intention nobody wrote,
		// so it decides nothing — and contributes no permission.
		return MatchRes{Note: "the spec's state contradicts the row's action"}
	}
	if strings.TrimSpace(r.Route) == "" {
		return MatchRes{Note: "the request names no route to match"}
	}
	if i := strings.IndexByte(spec.Route, '*'); i >= 0 && !(i == len(spec.Route)-1 && !strings.ContainsRune(spec.Route[:i], '*')) {
		// The one pattern form is a single trailing star — mid-name stars and
		// doubled stars are refusals to decide, never accidental grants.
		return MatchRes{Note: "the route pattern is not the single-trailing-star form"}
	}
	if !matchRoute(spec.Route, r.Route) {
		// Not a denial — this rule is about a different route. The engine's
		// fold sees only rules that spoke.
		return MatchRes{Note: "the row names a different route"}
	}
	return MatchRes{Matched: true}
}

// matchRoute is the whole pattern language: exact, or a prefix ending in one
// trailing `*`. `*` alone matches every route (an operator who typed it owns
// it — audited, authorship-gated like every row); `remote:*` can never reach
// a model route, because the prefix IS the namespace.
func matchRoute(pattern, route string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(route, pattern[:len(pattern)-1])
	}
	return pattern == route
}

func init() {
	// The validator is shared with the placeholder (same schema, same write
	// boundary); only the evaluator was a placeholder, and only it is
	// replaced. Registered at code time like every other name.
	RegisterType(RouteAccess, stubValidator{routeAccessSchema}, routeAccessEvaluator{})
}

// --- describing this type (S45 T2, PM5/PM6) -----------------------------------
//
// Additive: Match above is untouched. The clause builder lives here beside
// the deciding this file owns, exactly as the evaluator does, and registers
// itself beside the registration rather than into a branch anywhere else
// (PL3). "routes matching" earns the word: the one pattern form is the
// single trailing star, so a route spec matches, it does not merely name.

// describeRouteAccess renders the route_access sentence:
// "<verb> routes matching %q for <target phrase>".
func describeRouteAccess(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(RouteAccess)
	}
	verb, _ := describeVerb(p.Action)
	var spec RouteAccessSpec
	if rej := routeAccessSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(RouteAccess)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(RouteAccess)
	}
	return describeClause(verb+" routes matching "+describeQuoted(spec.Route), p)
}

// routeAccessFields derives the descriptor from the pinned schema the write
// boundary enforces.
func routeAccessFields() []SpecField { return routeAccessSchema.describeFields() }

func init() {
	registerPlaceholderDescribing(RouteAccess, describeRouteAccess, routeAccessFields)
}
