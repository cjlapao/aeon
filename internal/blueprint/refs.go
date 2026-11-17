package blueprint

// refs.go — S48 T2 (BP11): the ONE extraction of what a blueprint references.
// The derived `blueprint_ref` table is rebuilt from this; "who uses this
// agent" (S43's delete refusal, the pickers) asks SQL, and SQL holds exactly
// what this function returns. Pure: no I/O, no registry — and it must AGREE
// with what Analyze validates (one extraction, two uses; a test pins the
// equality over every shipped blueprint and fixture).

// Ref is one reference found in a blueprint. Kind is the closed five —
// agent | converse | skill | action | resolver — and Target is the name as
// written: an agent name, a converse agent name, the CALLEE part of a
// `callee/skill-id`, a code-action name, or a resolver name/tech tag.
type Ref struct {
	Kind   string
	Target string
}

// The closed kinds (the same five the schema CHECKs).
const (
	RefAgent    = "agent"
	RefConverse = "converse"
	RefSkill    = "skill"
	RefAction   = "action"
	RefResolver = "resolver"
)

// resolverNames mirrors handler.resolvers — the closed named-resolver
// registry (one entry, in code, by design L5). The blueprint package cannot
// import handler (pin.go's direction is loop→handler→…→blueprint validation),
// so the rule is MIRRORED here and pinned from the testable side:
// TestResolverNamesEqualTheRegistry runs handler.Resolvers() against this
// list (the s16ActionSet precedent in shipped_test.go).
var resolverNames = []string{"by-tech-tag"}

// References extracts every reference from a blueprint: each step's Agent
// (lanes and ladder steps; a resolver name counts as resolver, not agent —
// a resolver is not an agent body, and an agent-delete must not refuse for
// a name that never dispatches alone), the blueprint's Converse agent, each
// skill target's CALLEE, each code action, and resolver tech tags spelled in
// `params.tech` (mirroring Resolve's tag rule: the tag is what selects the
// handler). Sorted by (kind, target), deduped, empties skipped — the derived
// table needs stability, not discovery order.
func References(bp *Blueprint) []Ref {
	seen := map[Ref]bool{}
	add := func(kind, target string) {
		if target == "" {
			return
		}
		seen[Ref{Kind: kind, Target: target}] = true
	}
	add(RefConverse, bp.Converse)
	for _, l := range bp.Lanes {
		for _, s := range laneStepSlice(l) {
			if s.Agent != "" {
				if contains(resolverNames, s.Agent) {
					add(RefResolver, s.Agent)
				} else {
					add(RefAgent, s.Agent)
				}
			}
			if s.Skill != "" {
				// The CALLEE part of `callee/skill-id`; a malformed target
				// (no slash) extracts nothing — Analyze calls it invalid.
				if callee, skill, ok := cutSkill(s.Skill); ok {
					add(RefSkill, callee)
					_ = skill
				}
			}
			if s.Action != "" {
				add(RefAction, s.Action)
			}
			// The tech tag spelled in params (Resolve's `with["tech"]` rule
			// mirrored for the authored form): a pinned tag is a resolver
			// reference to that handler class.
			if v, ok := s.Params["tech"].(string); ok {
				add(RefResolver, v)
			}
		}
	}
	out := make([]Ref, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sortRefs(out)
	return out
}

// cutSkill is validateSkillTarget's rule restated once (the callee half).
func cutSkill(target string) (callee, skill string, ok bool) {
	for i := 0; i < len(target); i++ {
		if target[i] == '/' {
			callee, skill = target[:i], target[i+1:]
			return callee, skill, callee != "" && skill != "" && !containsSlash(skill)
		}
	}
	return "", "", false
}

func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

func sortRefs(refs []Ref) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && lessRef(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

func lessRef(a, b Ref) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Target < b.Target
}
