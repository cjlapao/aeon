package daemon

// S29 T5 — the S28 learning-feed hook (D10). A validation attempt's
// conclusion is filed as an S28 Learning: observed provenance,
// project-scoped, automatic. design.md §2's D10 — resolved there against
// S28's own M3 (no review gate; provenance-honesty at retrieval is the
// safeguard there too).
//
// This file is a CALLER of internal/learnings.Append, never a contributor
// to it. The task's own gate: its diff adds zero lines to internal/learnings
// itself — the feed re-implements none of S28's own write path. S28's M1
// (relaxed is written only by the board-answer hook), M3 (the strength is
// the observed count, set once at write), and §3 (the agent-scope Go check)
// are the law; this hook rides them, it does not restate them.
//
// The one call site is the reactor (handleTriageTerminal): on a validation
// attempt's completion, after the bug has moved out of unvalidated. No new
// gate, no review step, no second mechanism — exactly one Append.

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/triage"
)

// triageFeedObservationCount is the strength of a validation-attempt
// learning (M3's observed count, set once at write): the attempt ran
// once, the conclusion was seen once.
const triageFeedObservationCount = 1

// feedValidationLearning files a validation attempt's conclusion as an S28
// Learning — observed, project-scoped, automatic (D10). It is the ONE call
// site the task adds: it calls internal/learnings.AppendAs and nothing
// else, so S28's own write path is the law and this hook re-implements
// none of it. S52 T1 gave the call an actor: the feed is a daemon hook, so
// it stamps itself {system, "triage-feed"} — the audit line it leaves says
// which hook wrote, and an observed provenance is exactly what a system
// kind may say (it saw the attempt conclude; no person's word is claimed).
//
// Scope is ALWAYS project (never agent): the hook files what a PROJECT
// learned (that its validation attempt found X), scoped to the bug's
// component root — it never infers which agent to credit. A validation
// attempt is a fact about the project's defect, not about an agent's
// competence; crediting an agent would be a scope this hook does not have
// a basis to decide. provenance is 'observed' (the attempt saw it); the
// observed count is 1 (the attempt ran once — M3's strength, set once).
func (d *Daemon) feedValidationLearning(bug triage.Bug, validation triage.ValidationState) (learnings.Learning, *learnings.Refusal) {
	count := triageFeedObservationCount
	// The body summarizes what the attempt found — the conclusion the
	// attempt landed (could-not-tell / reproduced / not-reproduced), so a
	// later retrieval (S28's Query) has the fact, not the bare verdict.
	body := fmt.Sprintf("validation of %q (bug #%d): the attempt concluded %s — %s",
		bug.Title, bug.ID, validation, bug.Body)
	return learnings.New(d.store).AppendAs(learnings.AppendInput{
		Scope:         learnings.ScopeProject,
		ScopeKey:      bug.ComponentRoot,
		Body:          body,
		Provenance:    learnings.ProvenanceObserved,
		ObservedCount: &count,
	}, learnings.Actor{Kind: "system", ID: "triage-feed"})
}
