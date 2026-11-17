package policy

import "strings"

// spec_triggeraccess.go — S20 T7 fills the TriggerAccess placeholder S10
// shipped. As with SkillAccess at S14: the schema, validator and spec struct
// stay beside their siblings in spec_stubs.go, an operator could author a
// `trigger_access` row on the day S10 closed, and the moment this file
// linked in, the engine started honouring what was already stored.
//
// # What this type is FOR
//
// The schedule and the webhook are AUTHENTICATED mechanisms — a cron tick
// authenticates itself (the clock owns it) and a delivery proves its sender
// by HMAC. Neither answers the operator's question, which is different:
// **may this kind of start happen here at all?** "no webhooks here", "no
// unattended schedules in production" (the design's own examples) are policy
// sentences, and putting them in config would reopen exactly what policy
// replaced — a second, unaudited, unversioned place where permission lives.
// So they are rows: authored under S10's authorship boundary, decided by the
// engine's fold (PL4), spoken in S10's sentence.
//
// # Authentication is not authorization (TR9, the half that gets forgotten)
//
// The webhook path runs this AFTER the signature verifies, never instead
// of it — a provably-GitHub delivery still needs an authored allow, and the
// refusal says so: the verified parenthesis rides the fire record, because
// "signature good, proceed" is exactly how a trusted stranger starts owning
// the deployment one delivery at a time.
//
// # The matching domain
//
// `trigger` is the KIND word, the closed three: manual, schedule, webhook —
// the same words trigger.Schedule.Kind speaks. Exact comparison, no
// wildcards: the pinned shape promises a kind name, and `*` here would be a
// grant whose breadth nobody typed.

// triggerAccessEvaluator is the S20 real evaluator. It replaces
// stubEvaluator{TriggerAccess} for that type. Matched means a rule SPOKE;
// the engine folds deny over ask over allow (PL4) — this file decides
// nothing on its own, which is the whole discipline.
type triggerAccessEvaluator struct{}

// Match reports whether p's spec names the trigger kind this request is.
// The order is the argument, as in every sibling: read the row; ask whether
// the request is even about this type; reject rows that contradict
// themselves; reject rows (or requests) that name nothing — an empty spec
// trigger would match an empty request trigger, and a vacuous allow is how
// every unnamed ask inherits every grant. Only then compare the words.
func (triggerAccessEvaluator) Match(p Policy, r Request) MatchRes {
	var spec TriggerAccessSpec
	if rej := triggerAccessSchema.decode(p.Spec, &spec); rej != nil {
		return MatchRes{Note: "the spec is not the pinned TriggerAccess shape"}
	}
	if r.Type != TriggerAccess {
		// The engine never hands an evaluator another type's request; if it
		// ever did, the answer is still "not me", never "yes".
		return MatchRes{Note: "the request asks about a different type"}
	}
	if strings.TrimSpace(spec.Trigger) == "" {
		// Belt to the validator's braces: a row that LOOKS like a permission
		// and matches nothing (or everything) is the failure mode this file
		// is written against. Inert toward deny — PL5 does the refusing.
		return MatchRes{Note: "an empty trigger kind matches nothing"}
	}
	if spec.State != string(p.Action) {
		// A row the write boundary would never have admitted. Choosing either
		// half of the contradiction would invent an intention nobody wrote,
		// so it decides nothing — and contributes no permission.
		return MatchRes{Note: "the spec's state contradicts the row's action"}
	}
	if strings.TrimSpace(r.Trigger) == "" {
		return MatchRes{Note: "the request names no trigger kind to match"}
	}
	if spec.Trigger != r.Trigger {
		// Not a denial — this rule is about a different kind. The engine's
		// fold sees only rules that spoke.
		return MatchRes{Note: "the row names a different trigger kind"}
	}
	return MatchRes{Matched: true}
}

func init() {
	// The validator is shared with the placeholder (same schema, same write
	// boundary); only the evaluator was a placeholder, and only it is
	// replaced. Registered at code time like every other name.
	RegisterType(TriggerAccess, stubValidator{triggerAccessSchema}, triggerAccessEvaluator{})
}

// --- describing this type (S45 T2, PM5/PM6) -----------------------------------
//
// Additive: Match above is untouched. The clause builder lives beside the
// deciding this file owns (as SkillAccess's moved at S14), registering from
// its own init — sibling code to the registration, never a branch elsewhere
// (PL3).

// describeTriggerAccess renders the trigger_access sentence:
// "<verb> trigger %q for <target phrase>". The quoted word is the KIND
// (manual, schedule, webhook) — the closed three the schedule speaks; the
// sentence says which kind of start this rule governs, and authentication
// (TR9) stays what it is: verified or not, no rule here grants by silence.
func describeTriggerAccess(p Policy) string {
	if !describeReadable(p) {
		return unreadableSentence(TriggerAccess)
	}
	verb, _ := describeVerb(p.Action)
	var spec TriggerAccessSpec
	if rej := triggerAccessSchema.decode(p.Spec, &spec); rej != nil {
		return unreadableSentence(TriggerAccess)
	}
	if spec.State != string(p.Action) {
		return unreadableSentence(TriggerAccess)
	}
	return describeClause(verb+" trigger "+describeQuoted(spec.Trigger), p)
}

// triggerAccessFields derives the descriptor from the pinned schema the
// write boundary enforces.
func triggerAccessFields() []SpecField { return triggerAccessSchema.describeFields() }

func init() {
	registerPlaceholderDescribing(TriggerAccess, describeTriggerAccess, triggerAccessFields)
}
