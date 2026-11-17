package policy

// ════════════════════════════════════════════════════════════════════════════
// THE ONE FORMATTER IN THE PRODUCT.
//
// Every explanation of a policy decision that any human or machine ever sees
// is produced by Verdict.Sentence() below, and by nothing else. Its consumers,
// named so nobody has to guess:
//
//   - T8  record.go — policy_event.detail is rendered from Sentence() (or from
//         a fixed reason string), rune-capped by MaxEventDetailRunes. It is
//         never assembled by hand at the call site.
//   - T9  the runtime gate (internal/runtime) — the refusal text the model is
//         handed when a tool call is denied is Sentence(), verbatim.
//   - T10 the manifest / disagreement record — what a refused attempt says it
//         was refused by is Sentence().
//   - T12 `aeon policy why` — prints Sentence().
//   - T13 policy.why (the socket) — returns Sentence() as the field the app
//         prints.
//   - T14 PolicyPage.tsx — renders that field verbatim, no client-side
//         re-formatting.
//
// A consumer PRINTS the string. It does not re-case it, re-segment it,
// re-order its segments, drop a segment, translate it, or substitute its own
// prefix: a surface that turns "refused:" into "DENIED" has built a second
// formatter, and the four surfaces will drift exactly the way this file exists
// to prevent. A status column is fine as long as it is ADDITIVE and the
// sentence below is still present whole. TestSentenceIsTheOnlyFormatterInThe
// Module enforces the rule across the module rather than trusting it.
//
// Why one sentence and not four: "denied" is unactionable, and the difference
// between a user filing a bug and a user changing a policy is one sentence
// (PL8). Three of those surfaces show the same refusal to the same person for
// the same decision; two spellings of it is a bug report nobody can reproduce.
//
// Why this file does no I/O and reads no clock: it renders a value it was
// handed (D5). The instant an expiry prints is the instant the policy carries,
// never now — a sentence that read the clock would make a recorded detail
// disagree with the row it came from, and a surface that re-rendered later
// would watch the same refusal change tense. TestTheRenderingHasNoIO asserts
// it as syntax.
//
// Why there is no error anywhere in here (DB19): a verdict is a domain
// outcome the caller branches on, not an infrastructure failure. Sentence()
// cannot fail — every Verdict, including the degenerate ones, renders a
// sentence. TestVerdictIsNotAnErrorType asserts that too.
// ════════════════════════════════════════════════════════════════════════════

import (
	"strings"
	"time"
)

// sep is the segment separator. One constant so no surface can "tidy" the
// sentence into commas and call it the same thing.
const sep = " · "

// Placeholders for a field a well-formed verdict always carries. They exist so
// that "never silently omitted" is a property of the renderer rather than a
// property of the data: a blank segment is indistinguishable from a segment
// nobody checked, which is the same failure PL7 rules out for an expiry.
// Reachable only for a Verdict assembled by hand — Evaluate fills all of these
// (the subject and type come off the Request, the rest off the policy that
// fired) — and each one is pinned by a test.
const (
	unnamedType      = "unnamed type"
	unnamedTarget    = "unnamed target"
	unrecordedSource = "unrecorded source"
)

// Sentence renders the verdict as the sentence every surface prints. Pure
// string building over the fields it was handed: no I/O, no clock, no error.
//
// The shapes, pinned character for character by verdict_test.go and
// verdict_ask_test.go:
//
//	refused: <subject> · <type> "<id>" · target <target> · source <source> · <expiry>
//	allowed: <subject> · <type> "<id>" · target <target> · source <source> · <expiry>
//	asked:   <subject> · <type> "<id>" · target <target> · source <source> · <expiry>
//	                                            … plus, when an allow was beaten:
//	 (an allow exists: "<allow-id>" · <allow-target> — asked anyway, PL4)
//	refused: <subject> · no allow matched · default deny (PL5)
//	                                            … plus, when an allow was beaten:
//	 (an allow exists: "<allow-id>" · <allow-target> — denied anyway, PL4)
//	refused: <subject> · <type> has no evaluator yet · refusing rather than permitting (D10)
//
// The dispatch is on the fields, in this order, and the order is the argument:
//
//   - Unimplemented first. "Nobody can answer this yet" outranks every other
//     shape, because it is the one answer a reader can act on differently
//     (T11's could-not-determine maps here, and it must never be reachable by
//     reading a denial). The engine never sets it alongside the others; if a
//     hand-built verdict claims both, this is the honest one.
//   - DefaultDeny next: PL5, nothing matched, so nothing is permitted.
//   - Anything naming no policy at all also renders the PL5 line: a verdict
//     that permits nothing and names no rule has said exactly what the default
//     deny says, and the renderer fails closed rather than printing "allowed:"
//     for an allow it cannot name — or "asked:" for an ask it cannot name,
//     which would be a refusal to decide wearing a routing to a human.
//   - Otherwise the policy line, Asked picking the ask prefix and Allowed
//     (only) the permission prefix, with the override suffix whenever
//     OverriddenAllow is set (the engine sets it only when a deny or an ask
//     beat an allow — that is what makes it the visible half of PL4).
//
// <target> is rendered by Target.String() in types.go, deliberately: it is
// already the one target rendering in the product (`lane-type:review`, the
// bare kind for `all-agents`), and a second target renderer here is precisely
// the drift this file was created to make impossible. TestTargetRendersCome
// FromTargetString pins the delegation.
func (v Verdict) Sentence() string {
	const (
		refusedPrefix = "refused: "
		allowedPrefix = "allowed: "
		askedPrefix   = "asked: "
	)

	switch {
	case v.Unimplemented:
		// D10, and it reads nothing like a denial: a denial means a rule
		// refused you, this means the binary cannot decide yet.
		return refusedPrefix + v.subjectLabel() + sep + v.typeLabel() +
			" has no evaluator yet" + sep + "refusing rather than permitting (D10)"
	case v.Asked && v.DefaultAsk && v.By == nil:
		return askedPrefix + v.subjectLabel() + sep + "no rule covers this" + sep + "the default is to ask"
	case v.DefaultDeny:
		return refusedPrefix + v.subjectLabel() + sep + "no allow matched" + sep + "default deny (PL5)"
	case v.By == nil:
		// Fail closed, and say the true thing: nothing matched, so nothing is
		// permitted. Never "allowed:" for a permission nobody can name, and
		// never "asked:" for a routing to a human nobody can name.
		return refusedPrefix + v.subjectLabel() + sep + "no allow matched" + sep + "default deny (PL5)"
	}

	prefix := refusedPrefix
	switch {
	case v.Asked:
		prefix = askedPrefix
	case v.Allowed:
		prefix = allowedPrefix
	}
	s := prefix + v.subjectLabel() + sep + v.policyTypeLabel() + ` "` + v.By.ID + `"` +
		sep + "target " + targetLabel(v.By.Target) +
		sep + "source " + sourceLabel(v.By.Source) +
		sep + expiryLabel(v.By.NotAfter)

	if v.OverriddenAllow != nil {
		// PL4 made visible: the more specific grant lost, and the sentence
		// names it, because "I allowed that!" is answered here rather than by
		// someone reading the table. The verb follows the deciding tier: a
		// deny denies the grant away, an ask routes the question upward
		// past it.
		lost := "denied anyway, PL4"
		if v.Asked {
			lost = "asked anyway, PL4"
		}
		s += ` (an allow exists: "` + v.OverriddenAllow.ID + `" · ` +
			targetLabel(v.OverriddenAllow.Target) + " — " + lost + ")"
	}
	return s
}

// subjectLabel renders <subject>. It is copied off the Request by Evaluate; a
// verdict built without one renders what an anonymous request renders — the
// same answer from the same subject renderer (Request.Subject), so a missing
// subject reads as "nobody was named" rather than as a formatting hole.
func (v Verdict) subjectLabel() string {
	if strings.TrimSpace(v.Subject) != "" {
		return v.Subject
	}
	return Request{}.Subject()
}

// typeLabel renders <type> for the D10 line: what was asked about, which is
// the only thing a reader can go and register.
func (v Verdict) typeLabel() string { return typeLabel(v.Type) }

// policyTypeLabel renders <type> on the policy line: the type of the rule that
// fired. It equals v.Type by construction (Evaluate drops every policy of
// another type before matching anything), and the verdict's own type is the
// fallback when a hand-built By leaves it off — the type is never blank.
func (v Verdict) policyTypeLabel() string {
	if v.By != nil && v.By.Type != "" {
		return string(v.By.Type)
	}
	return typeLabel(v.Type)
}

func typeLabel(t PolicyType) string {
	if strings.TrimSpace(string(t)) == "" {
		return unnamedType
	}
	return string(t)
}

// targetLabel renders <target> through the one target renderer the product has.
func targetLabel(t Target) string {
	if s := strings.TrimSpace(t.String()); s != "" {
		return s
	}
	return unnamedTarget
}

// sourceLabel renders <source>: "who decided this", which is the field an
// audit answers with, so an absent source is said rather than skipped.
func sourceLabel(s Source) string {
	if strings.TrimSpace(string(s)) == "" {
		return unrecordedSource
	}
	return string(s)
}

// expiryLabel renders <expiry>, and it is never empty (PL7). A policy with no
// lifetime has to SAY so: silence is indistinguishable from a lifetime nobody
// checked, and the second one is a policy about to stop working. The instant
// is the one the row carries, formatted RFC3339 — never now, because the
// sentence a log holds has to keep matching the row it came from.
func expiryLabel(notAfter *time.Time) string {
	if notAfter == nil {
		return "no expiry"
	}
	return "expires " + notAfter.Format(time.RFC3339)
}
