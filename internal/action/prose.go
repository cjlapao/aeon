package action

// pr_summary — the ONE authored paragraph per PR (plus the review body);
// everything else is a template. The checks here are existence and shape
// (L21/D12): empty, placeholder-echo, floor, title-restatement. They
// refuse as a ROUTABLE `reject` naming the reason (O4) — the handler can
// be asked again; a failed loop cannot — and they are NOT a quality gate:
// a well-formed paragraph describing the wrong change passes every check
// here, and TestQualityIsNotChecked records that limit so nobody later
// mistakes these checks for one, or adds a "quality score" and puts a
// number nobody should trust in front of something outward-facing. The
// control that exists for quality is the same one that exists for a
// handler's code: a human reads the PR, at the `ask` on forge.openPR.

import (
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/runtime"
)

// Q9's thresholds, committed where they are used — a floor nobody wrote
// down gets adjusted until it passes (TestThresholdsAreCommitted pins
// both numbers against the task record).
const (
	// PrSummaryMinRunes is 120 RUNES — not bytes; non-English summaries
	// are real. 120 runes is two ordinary sentences: below it, a
	// sentence fragment is posing as a paragraph.
	PrSummaryMinRunes = 120
	// PrSummaryRestatementSlack is 15 runes: a summary that is the title
	// plus at most this much filler adds nothing a reader does not
	// already have. Beyond the slack, new substance has arrived.
	PrSummaryRestatementSlack = 15
)

// ProseReject is a routable refusal, not an infrastructure failure: the
// outcome is S8's reject so the blueprint's rework machinery routes it
// (O4), and Reason names the case for S17's redo edges.
type ProseReject struct {
	Field   string
	Reason  string
	Outcome runtime.Outcome
}

var proseNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// normalize folds case, punctuation and spacing away — the comparisons
// below are about CONTENT, and "Done." vs "done" must not matter.
func normalize(s string) string {
	return proseNonAlnum.ReplaceAllString(strings.ToLower(s), "")
}

// CheckPRSummary returns nil for a paragraph worth reading, or a reject
// naming which shape-check failed. Order: absence, echo, restatement,
// floor — and restatement is judged BEFORE the floor deliberately: a
// title is always under the floor, and the deeper diagnosis must be the
// one that names itself.
func CheckPRSummary(summary, title string) *ProseReject {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return &ProseReject{Field: "pr_summary", Outcome: runtime.OutcomeReject,
			Reason: "pr_summary is empty — nothing to read (L21): the paragraph a human will read on the pull request cannot be nothing"}
	}
	// The placeholder echo: the model copied the instruction instead of
	// answering it. Compared against the COMMITTED templates' own token
	// spellings (normalized), so "spelling variations of the same token"
	// are one class, not an enumeration someone must maintain.
	norm := normalize(trimmed)
	for _, spec := range TemplateRegistry() {
		for slot := range spec.Slots {
			if norm == normalize("{{"+slot+"}}") {
				return &ProseReject{Field: "pr_summary", Outcome: runtime.OutcomeReject,
					Reason: "pr_summary is the placeholder text itself — the instruction was echoed, not answered; the template's own token is not a paragraph"}
			}
		}
	}
	nt := normalize(title)
	if nt != "" && (norm == nt || (strings.HasPrefix(norm, nt) && len(norm)-len(nt) <= PrSummaryRestatementSlack)) {
		return &ProseReject{Field: "pr_summary", Outcome: runtime.OutcomeReject,
			Reason: "pr_summary restates the PR title — it adds nothing a reader does not already have (the title is on the page above it)"}
	}
	if len([]rune(trimmed)) < PrSummaryMinRunes {
		return &ProseReject{Field: "pr_summary", Outcome: runtime.OutcomeReject,
			Reason: "pr_summary is below the 120-rune floor (Q9, committed in prose.go) — a sentence fragment is not a paragraph"}
	}
	return nil
}

// PRProducingHandlers is the committed set of bodies whose output can
// reach a PR body: the implement-capable engineers — the handlers that
// occupy `implement` and whose work a forge.openPR carries. Reviewer,
// QA, planner, writer and advisor bodies deliberately do NOT declare
// pr_summary: a handler that never runs in a PR-producing lane declaring
// it is noise, and a declared-but-unused field is a field nobody
// maintains (TestOnlyPRReachingHandlersDeclareIt checks BOTH directions
// against the bodies on disk). security-engineer stays out: its body
// audits rather than ships.
func PRProducingHandlers() map[string]bool {
	return map[string]bool{
		"golang-engineer": true, "javascript-engineer": true, "python-engineer": true,
		"rust-engineer": true, "react-typescript-engineer": true, "vue-typescript-engineer": true,
		"tauri-react-engineer": true, "tauri-vue-engineer": true, "sql-engineer": true,
		"terraform-engineer": true, "docker-engineer": true, "websocket-engineer": true,
		"debugger-engineer": true, // a fix is a change; a change reaches a PR
	}
}
