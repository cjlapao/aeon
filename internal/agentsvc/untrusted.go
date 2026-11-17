package agentsvc

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/label"
)

// untrusted.go — AS12: a skill RESULT is untrusted, the same rule a tool
// result (W23) and a tool description (MC7) already live under. All three
// are prose from a stranger landing in a prompt, and S15 makes the stranger
// importable from anywhere, which is why this arrives before the problem
// does.
//
// # One mechanism, not three
//
// The labelling grammar lives in internal/label — S12's mechanism, extracted
// at S14 T8 rather than duplicated (a third labelling implementation is two
// too many). This file adds NO boundary logic: no crypto, no delimiter, no
// closer. A grep test holds that line.
//
// # Q3 answered — first-party is a COMPILE-TIME fact, read from an injected
// predicate, never a registry field
//
// The design's premise is that first-party results need no label because
// those bodies were reviewed in a pull request — a property of CODE, earned
// through review, not a claim a manifest can make. S15 will make the
// registry somebody else's, so a registry field named `firstParty` would be
// an imported agent marking its own homework. The boundary here:
//
//   - `TaskStore.SetFirstParty(fn)` is the read point — a predicate the
//     ENTRY POINT injects (cmd/aeon wires it from `handler.RegisteredBodies()`
//     — the bodies compiled into this binary, the exact set the PR-review
//     premise covers). One production call, same seam discipline as
//     loop.SetSkillKnown.
//   - The AgentCard has NO field to claim it (the test holds that with
//     reflect — the same sweep that keeps securitySchemes a hint), and
//     CardStore never reads one.
//   - nil predicate — the unwired state — labels EVERYTHING. Absence of the
//     wiring degrades toward the label, never past it: a daemon that forgot
//     to wire speaks as if every callee were a stranger, which is the safe
//     sentence, not the confident wrong one.

// LabelSkillResult renders one skill result as what it is: data from a
// named stranger, verbatim inside the shared untrusted region. The label
// names the agent and the skill, because "untrusted FROM WHERE" is the
// question a reader (human or model) asks next.
func LabelSkillResult(agent, skill, text string) string {
	return label.Region(fmt.Sprintf("skill result from agent %q skill %q", agent, skill), text)
}

// ModelFacingResult is where AS12 reads: the answer as the model is about to
// see it. A first-party callee's result goes through verbatim and
// unlabelled — labelling our own reviewed prose is not caution, it is noise
// that teaches the model to ignore the labels that matter. Everything else
// rides the region. The stored row never changes (scope 3: verbatim, byte
// for byte — the label belongs where the text REACHES A MODEL, never in the
// record, so a transcript can still show what the agent actually said).
func (ts *TaskStore) ModelFacingResult(task Task) string {
	// S15 T5 (AP7): a refused Task's stored result IS our own refusal
	// sentence, written at the arrival gate — it is not a stranger's words
	// and must not be region-wrapped (wrapping it would hand the label
	// back to the very text that replaces the stranger). This short-circuit
	// is WHY "the refused result never reaches a prompt" is structural:
	// every model-facing path goes through HERE, and here refuses twice —
	// once at the door, once at the mouth.
	if task.Status == TaskRefused {
		return task.ResultText()
	}
	answer := task.ResultText()
	if ts.IsFirstParty(task.CalleeAgent) {
		return answer
	}
	return LabelSkillResult(task.CalleeAgent, task.SkillID, answer)
}

// IsFirstParty reads the boundary through the injected predicate; unwired
// means nobody is first-party (the safe reading, argued above).
func (ts *TaskStore) IsFirstParty(agent string) bool {
	return ts.firstParty != nil && ts.firstParty(agent)
}

// SetFirstParty injects the compile-time boundary. SEC-059's seam discipline:
// the loop and this package never import the handler registry; the entry
// point says which agents its OWN binary implements, and the exact-once
// wiring is test-pinned at the entry point.
func (ts *TaskStore) SetFirstParty(fn func(agent string) bool) {
	ts.firstParty = fn
}
