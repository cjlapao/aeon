package handler

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

// S17 T6 — the handler half of verify's three outcomes. The verifier's
// handoff schema did not allow it to REPORT unverified: the four outcomes
// (L4) stay exactly four — unverified is not a fifth handoff outcome — but
// the verifier needs a structured place to say "the check did not run",
// because a reject with only prose says "the code is wrong" and the redo
// goes off to fix code that may be fine.
//
// The place is a declared handoff FIELD, `verification`, whose purpose is
// pinned the way pr_summary's is (L3's discipline): the wording IS the
// specification. The engine consumes what THIS package renders from the
// field — the canonical "unverified: " reason — so the fact is structured
// end to end and the prose never decides anything.

func TestHandlerCanReportUnverified(t *testing.T) {
	d, rej := LoadDefinition("verifier-engineer")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	purpose, declared := d.Handoff[VerificationField]
	if !declared {
		t.Fatalf("the verifier declares no %q field — before T6 its schema had no way to report unverified", VerificationField)
	}
	if strings.TrimSpace(purpose) != VerificationPurpose {
		t.Fatalf("the %q purpose drifted from the pinned wording:\n got: %s\nwant: %s", VerificationField, purpose, VerificationPurpose)
	}
	// A reject carrying the field IS the report, and it survives Check
	// (a non-ok claim is never punished for fields a finished run would
	// have carried).
	schema, rej := BuildSchema(d.Handoff, "test")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	claimed := Handoff{
		Outcome: runtime.OutcomeReject,
		Reason:  "golangci-lint was KILLED",
		Fields:  map[string]string{"verification": "unverified", "summary": "the lint check did not run"},
	}
	got := schema.Check(claimed)
	if got.Outcome != runtime.OutcomeReject {
		t.Fatalf("outcome %v — the reject must survive as the reject it was", got.Outcome)
	}
	if !got.IsUnverified() {
		t.Fatalf("IsUnverified false on a reject that SAYS verification=unverified in the field: %+v", got)
	}
	if !strings.HasPrefix(got.Reason, "unverified: ") {
		t.Fatalf("Check rendered %q — the structured field must render the canonical reason the engine reads", got.Reason)
	}
}

func TestUnverifiedOkIsNeverStoredAsPass(t *testing.T) {
	// The collapse this task exists to refuse, caught at the door: an ok
	// handoff whose own field says the gate did not run is
	// self-contradictory, and the record — not the claim — is what the
	// next step reads (D8, C6).
	schema, rej := BuildSchema(map[string]string{
		"summary":      "what ran and what it showed",
		"verification": VerificationPurpose,
	}, "test")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	claimed := Handoff{
		Outcome: runtime.OutcomeOK,
		Fields:  map[string]string{"verification": "unverified", "summary": "lint clean"},
	}
	got := schema.Check(claimed)
	if got.Outcome == runtime.OutcomeOK {
		t.Fatal("an ok handoff reporting verification=unverified was STORED as ok — the green check on an unverified gate (P31's fifth instance)")
	}
	if !got.IsUnverified() || !strings.Contains(got.Reason, "unverified") {
		t.Fatalf("the downgrade does not say what it is: %+v", got)
	}
}

func TestUnverifiedFieldValuesAreTheClosedThree(t *testing.T) {
	// pass, fail, unverified — and nothing else. A typo is a refusal
	// naming the value, never an inert field nobody reads.
	for _, v := range []string{"pass", "fail", "unverified"} {
		h := Handoff{Outcome: runtime.OutcomeReject, Fields: map[string]string{"verification": v}}
		if h.Verification() != v {
			t.Fatalf("Verification() = %q, want %q", h.Verification(), v)
		}
	}
	h := Handoff{Outcome: runtime.OutcomeReject, Fields: map[string]string{"verification": "clean"}}
	if h.Verification() != "" {
		t.Fatalf("an off-vocabulary value read as %q — it must be inert-and-named, never trusted", h.Verification())
	}
	if h.IsUnverified() {
		t.Fatalf("IsUnverified true for %q", "clean")
	}
}

func TestOOMConsequenceStillInTheBody(t *testing.T) {
	// S13's QAC-05 lesson, re-pinned for THIS body: deleting the
	// DECODING (137 means OOM — the bash tool says so, W5) is right;
	// deleting the CONSEQUENCE edits correct code to satisfy a linter
	// that never ran. The verifier must still say the silence of an
	// OOM-killed check is not a pass.
	d, rej := LoadDefinition("verifier-engineer")
	if rej != nil {
		t.Fatalf("%v", rej)
	}
	lower := strings.ToLower(d.Body)
	if !strings.Contains(lower, "oom") || !strings.Contains(lower, "not a pass") {
		t.Fatal("the verifier body lost the consequence: an OOM-killed check's silence is not a pass and not a finding (QAC-05)")
	}
	if strings.Contains(d.Body, "Exit 137") || strings.Contains(d.Body, "exit 137") {
		t.Error("the DECODING survived too: 137/SIGKILL means oomKilled and the bash tool says so — keep the consequence, delete the decoding")
	}
}

func TestVerificationPurposeIsPinned(t *testing.T) {
	// The pr_summary discipline, applied to the field that carries the
	// third outcome: a body paraphrasing its own contract is twenty
	// bodies with twenty contracts.
	_, rej := BuildSchema(map[string]string{
		"summary":      "s",
		"verification": "whether it passed",
	}, "test-body")
	if rej == nil {
		t.Fatal("BuildSchema accepted a paraphrased verification purpose — the pinned wording IS the specification")
	}
	if !strings.Contains(rej.Reason, VerificationPurpose) {
		t.Fatalf("the refusal does not name the required wording: %s", rej.Reason)
	}
}
