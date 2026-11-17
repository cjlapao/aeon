package execenv

// describe.go — S59 T6 (EV16, EV18): the ONE formatter for what a bind does.
//
// `environment.bind {dryRun}` answers with a sentence, and the task's rule is
// that the dry-run answer and the applied answer are THE SAME TEXT — a person
// who read the preview must not be shown a different sentence when the write
// lands, and the CLI (`aeon env bind`) and the Settings panel (T11) print this
// one string, not three paraphrases of it. That is only true if the sentence
// is built in one place, which is this file: the daemon composes its statement
// here and nowhere else, the same way `policy.Verdict.Sentence()` is the only
// renderer of a decision (§4d).
//
// What a statement may name: the scope, the component, the environment by
// slug and id, the backends and their strengths, and the secret references by
// NAME (EV7 — a value has no representation here to leak: the caller holds
// `[]string`, not `[]Injection`, so a value cannot reach this function even by
// accident). What it may never do is promise an effect it has not checked:
// the unattended clause states EV6 as a property of the plane, and the
// strength comparison is `BackendStrength` spoken verbatim, not a guess about
// whether this bind is "safer".

import (
	"fmt"
	"strings"
)

// BindStatement is everything DescribeBind needs, and the shape is the proof
// that no value can ride along: the secret references arrive as NAMES.
// PreviousBackend is the backend that was effective for that scope BEFORE this
// bind — "" means nothing was bound, which for a component means the
// workspace default, and for a workspace means the legacy inherit (the daemon
// resolves that and passes the answer; this formatter never looks it up).
type BindStatement struct {
	ScopeKind       string   // "workspace" | "component"
	ComponentID     string   // required for a component scope, empty otherwise
	EnvironmentSlug string   // the row's slug, as a person reads it
	EnvironmentID   string   // the row's id, so the sentence names one row
	Backend         string   // the environment's backend
	PreviousBackend string   // "" when nothing was bound at that scope
	SecretRefNames  []string // names only — there is no field for a value
	DryRun          bool
}

// maxStatementNames caps how many secret-reference names one statement
// enumerates before collapsing the tail to a count. A statement is a line a
// person reads, not a dump; the cap is a legibility bound and the count it
// replaces is exact.
const maxStatementNames = 8

// DescribeBind renders the statement for one bind. It is pure: no store, no
// clock, no backend call — the caller has already resolved what the rows say,
// and the same inputs always produce the same sentence, which is what makes
// `TestBindDryRunStatementEqualsAppliedStatement` a real test rather than a
// comparison of two timings.
func DescribeBind(in BindStatement) string {
	parts := []string{"bind"}

	// The scope, named. A component scope names its component; a workspace
	// scope is the workspace and needs no id.
	scope := strings.TrimSpace(in.ScopeKind)
	if scope == "" {
		scope = "component"
	}
	if scope == "component" && in.ComponentID != "" {
		parts = append(parts, "scope:component", "component "+in.ComponentID)
	} else {
		parts = append(parts, "scope:workspace")
	}

	// The environment by slug and id, then its backend with the strength
	// number beside it — the number is `BackendStrength`, not an adjective.
	env := "environment " + displayToken(in.EnvironmentSlug, in.EnvironmentID)
	parts = append(parts, env, fmt.Sprintf("backend %s (strength %d)", in.Backend, BackendStrength(in.Backend)))

	// The comparison against what was effective before. Three honest states,
	// never two: up is tighter, down is a WIDENING (named as one), equal says
	// so — and "" is its own state, because "nothing was bound" is not the
	// same fact as "host-inherit was".
	parts = append(parts, strengthNote(in.PreviousBackend, in.Backend))

	// The secret references, by name.
	parts = append(parts, secretRefNote(in.SecretRefNames))

	// EV6 as a property of the plane, stated on every bind so a person is
	// never told "an unattended loop runs with your environment" by
	// implication.
	parts = append(parts, "unattended loops never inherit")

	if in.DryRun {
		parts = append(parts, "dry-run: nothing applied")
	}
	return strings.Join(parts, " · ")
}

// strengthNote renders the before/after comparison. The word "widening" is
// deliberate and appears in no other sentence: an audit reader must be able to
// grep for the binds that loosened something.
func strengthNote(prev, next string) string {
	nextStrength := BackendStrength(next)
	if prev == "" {
		return fmt.Sprintf("nothing was bound at this scope (an attended step inherits the daemon's environment — strength %d; unattended never does) → %d",
			BackendStrength("host-inherit"), nextStrength)
	}
	prevStrength := BackendStrength(prev)
	switch {
	case nextStrength > prevStrength:
		return fmt.Sprintf("tighter: %s (%d) → %s (%d)", prev, prevStrength, next, nextStrength)
	case nextStrength < prevStrength:
		return fmt.Sprintf("widening: %s (%d) → %s (%d)", prev, prevStrength, next, nextStrength)
	default:
		return fmt.Sprintf("same strength: %s (%d)", next, nextStrength)
	}
}

// secretRefNote names the references the bind will inject — and says "no
// secret references" out loud rather than leaving the clause out, so an
// absence is a stated fact and not a reader's inference.
func secretRefNote(names []string) string {
	if len(names) == 0 {
		return "injects no secret references"
	}
	shown := names
	suffix := ""
	if len(names) > maxStatementNames {
		shown = names[:maxStatementNames]
		suffix = fmt.Sprintf(" (+%d more)", len(names)-maxStatementNames)
	}
	return fmt.Sprintf("injects %d secret reference(s) by name: %s%s", len(names), strings.Join(shown, ", "), suffix)
}

// displayToken prefers the human's slug and falls back to the id when the slug
// is blank — a row always has an id, and a statement that names nothing is
// worse than one that names the id.
func displayToken(slug, id string) string {
	slug = strings.TrimSpace(slug)
	if slug != "" {
		if id = strings.TrimSpace(id); id != "" {
			return slug + " (" + id + ")"
		}
		return slug
	}
	return strings.TrimSpace(id)
}
