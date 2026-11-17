package loop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// RecordsReader is the loop's read seam over the records tier (S38 T12):
// what the runtime asks when a lane declares `emits:` and the model reports
// ok. It is an interface so the loop package imports no store type beyond
// what it already does, and the daemon wires the real one (blueprint.go).
// nil — unwired — never means "pass": a step that declares emits and cannot
// be checked BLOCKS with contract-unverifiable (P31: the third state is
// blocked-honestly, never silently-ok).
type RecordsReader interface {
	CountByStep(kind string, stepID int64) (int, error)
	AttachedByStep(stepID int64) ([]string, error)
}

// contractRejection is the ONE renderer of the rejection phrase (DoD: the
// sentence appears exactly once outside tests). advance and continue both
// route through it, so a rewording can only happen here, in one diff.
func contractRejection(why string) string {
	return "declared record was not created: " + why
}

// contractUnverifiableReason is the blocked-loop reason when the reader is
// not wired; the Kind is what the protocol carries, the Detail explains.
const (
	contractUnverifiableKind     = "contract-unverifiable"
	contractUnverifiableDetail   = "the lane declares emits: but no records reader is wired — the contract cannot be checked, and an unchecked contract is not a passed one"
	contractUnreadableKindSuffix = " (unreadable)"
)

// contractVerifiable answers whether the step's contract CAN be checked at
// all: emits without a reader is the unverifiable third state (blocked),
// not a verdict.
func contractVerifiable(dep Deps, step blueprint.Step) bool {
	return len(step.Emits) == 0 || dep.Records != nil
}

// missingRecord is the whole contract decision, pure apart from the two
// reader calls: "" when satisfied, else ONE sentence naming every gap
// (records short of their minimum, documents not pinned), joined so the
// agent reads its full to-do in one refusal rather than rediscovering it
// rung by rung. A reader that errors reads as unreadable — and unreadable
// is refused as "could not check", never as "absent".
func missingRecord(dep Deps, step blueprint.Step, stepID int64) string {
	if len(step.Emits) == 0 {
		return ""
	}
	if dep.Records == nil {
		return contractUnverifiableDetail
	}
	var gaps []string
	var attached []string
	attachedRead := false
	for _, emit := range step.Emits {
		min := emit.Min
		if min < 1 {
			min = 1
		}
		n, err := dep.Records.CountByStep(emit.Type, stepID)
		if err != nil {
			gaps = append(gaps, fmt.Sprintf("the %s count could not be read (%v) — an unreadable contract is refused, never waved through", emit.Type, err))
			continue
		}
		if n < min {
			tool, ok := blueprint.EmitTool(emit.Type)
			if !ok {
				tool = "?" + emit.Type + "_create"
			}
			gaps = append(gaps, fmt.Sprintf("lane step must create %d %s record(s), found %d — call %s", min, emit.Type, n, tool))
		}
		if len(emit.Attach) == 0 {
			continue
		}
		if !attachedRead {
			a, err := dep.Records.AttachedByStep(stepID)
			if err != nil {
				gaps = append(gaps, fmt.Sprintf("the step's attachments could not be read (%v) — an unreadable contract is refused, never waved through", err))
				continue
			}
			attached, attachedRead = a, true
		}
		have := map[string]bool{}
		for _, d := range attached {
			have[d] = true
		}
		for _, want := range emit.Attach {
			if !have[want] {
				gaps = append(gaps, fmt.Sprintf("%s is not pinned — attach %s to the %s with `attach`", want, want, emit.Type))
			}
		}
	}
	return strings.Join(gaps, "; ")
}

// contractPrompt is the prompt half of OL5: the step is TOLD its contract
// before it runs, in one short paragraph after the produces sentence. The
// wording is golden-tested; keep it one paragraph.
func contractPrompt(emits []blueprint.Emit) string {
	if len(emits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThis step is not complete until these records exist (they are separate from the files): ")
	parts := make([]string, 0, len(emits))
	for _, emit := range emits {
		min := emit.Min
		if min < 1 {
			min = 1
		}
		tool, ok := blueprint.EmitTool(emit.Type)
		if !ok {
			tool = "?" + emit.Type + "_create"
		}
		part := "call " + tool + " at least " + strconv.Itoa(min) + " " + emit.Type + " record(s)"
		for _, want := range emit.Attach {
			part += " and attach " + want + " to the " + emit.Type + " with `attach`"
		}
		parts = append(parts, part)
	}
	b.WriteString(strings.Join(parts, "; "))
	b.WriteString(".")
	return b.String()
}
