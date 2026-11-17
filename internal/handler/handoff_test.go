package handler

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

func mustSchema(t *testing.T, m map[string]string) HandoffSchema {
	t.Helper()
	s, rej := BuildSchema(m, "test")
	if rej != nil {
		t.Fatalf("BuildSchema: %v", rej)
	}
	return s
}

func TestDeclaredFieldNotProducedIsAReject(t *testing.T) {
	s := mustSchema(t, map[string]string{
		"summary": "the paragraph a reader of this step's output needs",
	})
	claimed := Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"other": "x"}}
	got := s.Check(claimed)
	if got.Outcome != runtime.OutcomeReject {
		t.Fatalf("a declared field withheld under a claimed ok must be a REJECT (D12), not ok-with-a-missing-key; got outcome %q", got.Outcome)
	}
	if !strings.Contains(got.Reason, "summary") {
		t.Errorf("the reject reason must name the field; got %q", got.Reason)
	}
	// The model's claim is an input, never the verdict — assert the claimed
	// value is untouched (purity) while the verdict moved.
	if claimed.Outcome != runtime.OutcomeOK {
		t.Error("Check mutated the claimed handoff")
	}
	// Present-and-empty is not produced either: the next step got nothing.
	empty := Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"summary": "   "}}
	if got := s.Check(empty); got.Outcome != runtime.OutcomeReject {
		t.Errorf("present-but-empty must reject like absent; got %q", got.Outcome)
	}
	// And the honest complete handoff sails through.
	good := Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"summary": "did the thing"}}
	if got := s.Check(good); got.Outcome != runtime.OutcomeOK || got.Reason != "" {
		t.Errorf("a complete ok handoff was altered: %+v", got)
	}
}

func TestUndeclaredFieldIsIgnoredNotAnError(t *testing.T) {
	s := mustSchema(t, map[string]string{"summary": "the needed paragraph"})
	claimed := Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{
		"summary":  "fine",
		"surprise": "the model volunteered useful context nobody declared",
	}}
	got := s.Check(claimed)
	if got.Outcome != runtime.OutcomeOK {
		t.Fatalf("extra fields punished a step — the schema is a floor, not a ceiling; volunteering useful context must never fail a step")
	}
	if got.Fields["surprise"] == "" {
		t.Error("extra fields were dropped, not ignored-and-kept — the next step may want what the model offered")
	}
}

func TestOptionalFieldMayBeAbsent(t *testing.T) {
	s := mustSchema(t, map[string]string{
		"summary":      "needed paragraph",
		"tests_added?": "paths of tests written, or none",
	})
	got := s.Check(Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"summary": "x"}})
	if got.Outcome != runtime.OutcomeOK {
		t.Fatalf("the optional field was enforced; got %+v", got)
	}
	// Required still enforced in the same schema.
	got = s.Check(Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"tests_added": "a_test.go"}})
	if got.Outcome != runtime.OutcomeReject || !strings.Contains(got.Reason, "summary") {
		t.Fatalf("required field skipped while optional enforced: %+v", got)
	}
}

func TestFourOutcomesRoundTrip(t *testing.T) {
	for _, o := range []runtime.Outcome{runtime.OutcomeOK, runtime.OutcomeReject, runtime.OutcomeBlocked, runtime.OutcomeFailed} {
		h := Handoff{Outcome: o, Reason: "r", Fields: map[string]string{"summary": "s"}}
		raw, err := h.MarshalRoundTrip()
		if err != nil {
			t.Fatalf("Marshal(%s): %v", o, err)
		}
		back, err := UnmarshalHandoff(raw)
		if err != nil {
			t.Fatalf("Unmarshal(%s): %v", o, err)
		}
		if back.Outcome != o || back.Reason != "r" || back.Fields["summary"] != "s" {
			t.Errorf("outcome %s did not survive persistence: %+v", o, back)
		}
		// TYPE IDENTITY, not value equality: a locally-defined string type
		// with the same four values would pass == and be the second set.
		if reflect.TypeOf(back.Outcome) != reflect.TypeOf(runtime.OutcomeOK) {
			t.Fatalf("Handoff.Outcome is %s, not runtime.Outcome — this is S8's set or it is nothing (L4)", reflect.TypeOf(back.Outcome))
		}
	}
}

// TestNoSecondOutcomeSet: the second set always starts as one convenient
// local constant. This greps for it before it grows users.
func TestNoSecondOutcomeSet(t *testing.T) {
	decl := regexp.MustCompile(`(type\s+Outcome\b|Outcome\s+\w*\s*=\s*")`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			st := strings.TrimSpace(line)
			if strings.HasPrefix(st, "//") {
				continue
			}
			if decl.MatchString(st) {
				t.Errorf("%s:%d defines a competing outcome enumeration — the four outcomes are S8's (L4): %s", e.Name(), i+1, st)
			}
		}
	}
	// Positive control: S8's four exist and are the ones round-tripped.
	for _, want := range []runtime.Outcome{runtime.OutcomeOK, runtime.OutcomeReject, runtime.OutcomeBlocked, runtime.OutcomeFailed} {
		if want == "" {
			t.Fatal("an S8 outcome constant is empty — the positive control failed, and the grep above proves nothing without it")
		}
	}
}

// TestRejectReasonNamesTheField stands alone because S17's redo edges act on
// the REASON: a reject that cannot say which field went missing gives an
// edge nothing to route on.
func TestRejectReasonNamesTheField(t *testing.T) {
	s := mustSchema(t, map[string]string{
		"summary":      "the paragraph a reader needs",
		"test_results": "the command run and the output line that shows it",
	})
	got := s.Check(Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{"summary": "ok"}})
	if got.Outcome != runtime.OutcomeReject {
		t.Fatalf("missing test_results must reject; got %+v", got)
	}
	if !strings.Contains(got.Reason, "test_results") {
		t.Fatalf("the reason must name the missing field for S17's edges; got %q", got.Reason)
	}
	// The purpose rides along: the redo knows WHAT to produce, not just what
	// was missing.
	if !strings.Contains(got.Reason, "the command run") {
		t.Errorf("the reason should carry the field's purpose so a redo has direction; got %q", got.Reason)
	}
}

func TestEveryFieldHasAStatedPurpose(t *testing.T) {
	_, rej := BuildSchema(map[string]string{"notes": ""}, "x.md")
	if rej == nil || !strings.Contains(rej.Reason, "notes") {
		t.Fatalf("a purposeless field must fail at load naming it (O6); got %v", rej)
	}
	if !strings.Contains(rej.Reason, "purpose") {
		t.Errorf("the refusal must say what is missing; got %q", rej.Reason)
	}
	if _, rej := BuildSchema(map[string]string{"notes": "   "}, "x.md"); rej == nil {
		t.Fatal("whitespace is not a purpose")
	}
	// And the duplicate claim: `foo` and `foo?` cannot both be the contract.
	if _, rej := BuildSchema(map[string]string{"x": "p", "x?": "q"}, "x.md"); rej == nil {
		t.Fatal("a field declared both required and optional is two contracts for one field")
	}
}

// S16 T9 replaced TestPrSummaryIsNotHere — the N4 guard it pinned was
// built to be lifted WHEN THE READER ARRIVED, and the reader arrived.
// What replaces the guard is stricter, not looser: the field is legal
// now, but its PURPOSE is enforced verbatim.

func TestPrSummaryIsNoLongerPremature(t *testing.T) {
	s, rej := BuildSchema(map[string]string{"pr_summary": PrSummaryPurpose}, "x.md")
	if rej != nil {
		t.Fatalf("pr_summary landed with its reader (S16): %v", rej)
	}
	if spec, ok := s["pr_summary"]; !ok || !spec.Required {
		t.Fatalf("declared and required — D12's promise: %+v", s)
	}
}

// TestPurposeIsTheFraming (S13's O6, L3) — the wording IS the
// specification: a field described as "a summary" gets a restatement of
// the title, so the exact framing is what load enforces.
func TestPurposeIsTheFraming(t *testing.T) {
	for _, bad := range []string{"a summary", "summary", "the PR summary", "notes for the PR", ""} {
		_, rej := BuildSchema(map[string]string{"pr_summary": bad}, "x.md")
		if rej == nil || !strings.Contains(rej.Reason, "paragraph a human will read") {
			t.Fatalf("purpose %q must be refused quoting the required framing verbatim: %v", bad, rej)
		}
	}
}

// TestMissingFieldStillRejects — S13's T3 behaviour unchanged: a
// declared-and-absent field is still a reject naming the field. pr_summary
// gets the same treatment as any declared field, because it IS one now.
func TestMissingFieldStillRejects(t *testing.T) {
	s := mustSchema(t, map[string]string{"pr_summary": PrSummaryPurpose})
	got := s.Check(Handoff{Outcome: runtime.OutcomeOK, Fields: map[string]string{}})
	if got.Outcome != runtime.OutcomeReject {
		t.Fatalf("declared and absent is a reject (S13's T3), not an ok with a missing key: %+v", got)
	}
	if !strings.Contains(got.Reason, "pr_summary") {
		t.Fatalf("the reject names the field for S17's redo edges: %q", got.Reason)
	}
}

// The handoff the loader reads: front matter `handoff:` → Definition.Handoff
// → BuildSchema, so the schema a step is checked against is the one its file
// declared, through one path.
func TestSchemaBuildsFromLoadedFrontMatter(t *testing.T) {
	p := writeHandler(t, `---
name: schema-loaded
description: declares its handoff honestly.
temperature: 0.3
tools: [read, write]
handoff:
  summary: the paragraph a reader needs
  tests_added?: paths written or none
---
Write the test, then the code.
`)
	d, rej := Load(p)
	if rej != nil {
		t.Fatalf("Load: %v", rej)
	}
	s, rej := BuildSchema(d.Handoff, p)
	if rej != nil {
		t.Fatalf("BuildSchema: %v", rej)
	}
	if len(s) != 2 || !s["summary"].Required || s["tests_added"].Required {
		t.Fatalf("schema shape wrong: %+v", s)
	}
	if s["summary"].Purpose == "" {
		t.Error("purpose lost between front matter and spec")
	}
	// json tags stay camel-stable for the persistence path.
	raw, _ := json.Marshal(Handoff{Outcome: runtime.OutcomeBlocked, Reason: "provider", Fields: map[string]string{"k": "v"}})
	for _, key := range []string{`"outcome"`, `"reason"`, `"fields"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("wire key %s missing: %s", key, raw)
		}
	}
}
