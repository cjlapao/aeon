package policy

import "strings"

// spec_skillaccess.go — S14 fills the SkillAccess placeholder (T7). The
// schema, validator and spec struct stay beside their siblings in
// spec_stubs.go: an operator can write a `skill_access` row TODAY, against
// the same pinned shape, and the moment this file linked in, the engine
// started honouring what was already stored. That is the placeholder design
// working as written — the row shape ships first, the decision arrives with
// the slice that owns it.
//
// A2A ships per-skill `securitySchemes`; Aeon expresses per-skill security
// THROUGH THIS TYPE (AS8) rather than building a second authorization system
// to hold them. A card declaring its own skills open is a hint, not an
// authority — a third-party card cannot author an allow, and the rule that
// opens a skill is authored here, under S10's authorship boundary (an allow
// only from `operator`), recorded in policy rows, and spoken only in S10's
// sentence. MC4 refused exactly this second mechanism for tools; the
// reasoning did not change because the subject is a skill.
//
// The matching domain is S14's canonical reference: `callee/skill` (the same
// spelling `agentsvc.SkillRef` produces, and the spelling policy authors
// type into `aeon policy add`). Exact comparison, no wildcards: the pinned
// shape promises a skill name, and a wildcard here would be a grant whose
// breadth nobody typed.

// skillAccessEvaluator is the S14 real evaluator. It replaces
// stubEvaluator{SkillAccess} for that type: Matched means a rule SPOKE, and
// the engine folds deny over ask over allow (PL4) — this file decides
// nothing on its own, which is the whole discipline.
type skillAccessEvaluator struct{}

// Match reports whether p's spec names the skill this request wants.
//
// The order is the argument, as in every sibling: read the row; ask whether
// the request is even about this type; reject rows that contradict
// themselves; reject rows (or requests) that name nothing — an empty spec
// skill would match an empty request skill, and a vacuous allow is how every
// unnamed ask inherits every grant. Only then compare the names, exactly.
func (skillAccessEvaluator) Match(p Policy, r Request) MatchRes {
	var spec SkillAccessSpec
	if rej := skillAccessSchema.decode(p.Spec, &spec); rej != nil {
		return MatchRes{Note: "the spec is not the pinned SkillAccess shape"}
	}
	if r.Type != SkillAccess {
		// The engine never hands an evaluator another type's request; if it
		// ever did, the answer is still "not me", never "yes".
		return MatchRes{Note: "the request asks about a different type"}
	}
	if strings.TrimSpace(spec.Skill) == "" {
		// Belt to the validator's braces: a row that LOOKS like a permission
		// and matches nothing (or everything) is the failure mode this file
		// is written against. Inert toward deny — PL5 does the refusing.
		return MatchRes{Note: "an empty skill reference matches nothing"}
	}
	if spec.State != string(p.Action) {
		// A row the write boundary would never have admitted. Choosing either
		// half of the contradiction would invent an intention nobody wrote,
		// so it decides nothing — and contributes no permission.
		return MatchRes{Note: "the spec's state contradicts the row's action"}
	}
	if strings.TrimSpace(r.Skill) == "" {
		return MatchRes{Note: "the request names no skill to match"}
	}
	if spec.Skill != r.Skill {
		// Not a denial — this rule is about a different skill. The engine's
		// fold sees only rules that spoke.
		return MatchRes{Note: "the row names a different skill"}
	}
	return MatchRes{Matched: true}
}

func init() {
	// The validator is shared with the placeholder (same schema, same write
	// boundary); only the evaluator was a placeholder, and only it is
	// replaced. Registered at code time like every other name.
	RegisterType(SkillAccess, stubValidator{skillAccessSchema}, skillAccessEvaluator{})
}

// --- describing this type (S45 T2, PM5/PM6) -----------------------------------
//
// Additive: Match above is untouched. The clause builder lives beside the
// deciding this file owns (the same split that moved the evaluator here and
// left the schema in spec_stubs.go), and registers itself from its own init
// — sibling code to the registration, never a branch elsewhere (PL3).

// describeSkillAccess renders the skill_access sentence:
// "<verb> skill %q for <target phrase>". No "matching": the comparison is
// exact — a wildcard here would be a grant whose breadth nobody typed, and
// the sentence says the plain name it actually is.
func describeSkillAccess(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(SkillAccess)
	}
	verb, _ := describeVerb(p.Action)
	var spec SkillAccessSpec
	if rej := skillAccessSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(SkillAccess)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(SkillAccess)
	}
	return describeClause(verb+" skill "+describeQuoted(spec.Skill), p)
}

// skillAccessFields derives the descriptor from the pinned schema the write
// boundary enforces.
func skillAccessFields() []SpecField { return skillAccessSchema.describeFields() }

func init() {
	registerPlaceholderDescribing(SkillAccess, describeSkillAccess, skillAccessFields)
}
