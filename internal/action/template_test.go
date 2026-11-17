package action

// T8's tests, written first. The template is the join of L3's split:
// identifiers from code, one paragraph from the model, assembled into
// something a person reads. It is the last thing between Aeon and a pull
// request other people will see — which is why an unresolved slot fails
// the action rather than shipping its placeholder in a permanent record.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

func filledValues() *SlotValues {
	return &SlotValues{
		// The manifest side speaks the manifest's OWN vocabulary (L18);
		// identity facts are code-side evidence, recorded by the loop.
		Manifest: blueprint.Values{"mode": "standard"},
		Evidence: map[string]string{
			"loop_slug":    "4a17-version",
			"task_id":      "4a17",
			"task_title":   "Add version endpoint",
			"head_ref":     "task/4a17-version/4a17",
			"base_ref":     "feature/4a17-version",
			"issue_number": "4",
			"pr_number":    "3",
			"commit_sha":   "abc123def",
			"merge_sha":    "cafe0000",
			"review_id":    "9",
			"verdict":      "approve",
		},
		Handoff: map[string]string{
			"pr_summary":          "Adds GET /version returning the build stamp.",
			"acceptance_criteria": "GET /version answers 200 with a non-empty stamp.",
			"review_summary":      "Verified against the running daemon.",
		},
	}
}

// TestUndeclaredSlotIsInvalidAtLoad (D10, L20) — a template naming a slot
// no producing handler declares is invalid AT LOAD, with the diagnostic's
// path naming the template and the slot. Discovering `{{pr_summary}}` is
// empty while opening a real pull request means a PR with a hole in it,
// in front of people; a load-time check runs before anything is
// outward-facing.
func TestUndeclaredSlotIsInvalidAtLoad(t *testing.T) {
	good := TemplateSpec{
		Name:   "pr-body",
		Source: "Task {{task_id}}: {{pr_summary}}",
		Slots:  map[string]SlotKind{"task_id": SlotManifest, "pr_summary": SlotHandoff},
	}
	if diags := ValidateTemplates([]TemplateSpec{good}); len(diags) != 0 {
		t.Fatalf("the declared set is honest, no diagnostics: %+v", diags)
	}
	bad := TemplateSpec{
		Name:   "pr-body",
		Source: "Task {{task_id}}: {{totally_made_up}}",
		Slots:  map[string]SlotKind{"task_id": SlotManifest},
	}
	diags := ValidateTemplates([]TemplateSpec{bad})
	if len(diags) != 1 {
		t.Fatalf("the undeclared slot is invalid at load, got %+v", diags)
	}
	d := diags[0]
	if d.State != blueprint.Invalid {
		t.Fatalf("invalid, not warned: %+v", d)
	}
	if d.Kind != "undeclared-slot" {
		t.Fatalf("machine-readable kind (S8's O1): %+v", d)
	}
	if !strings.Contains(d.Path, "pr-body") || !strings.Contains(d.Path, "totally_made_up") {
		t.Fatalf("the path names the template AND the slot: %q", d.Path)
	}
	if d.Slice != "S16" {
		t.Fatalf("the diagnostic carries its slice: %+v", d)
	}
}

// The committed registry itself must pass its own load check — the real
// three templates, no fixtures.
func TestCommittedTemplatesAreValid(t *testing.T) {
	if diags := ValidateTemplates(TemplateRegistry()); len(diags) != 0 {
		t.Fatalf("the shipped templates are invalid at load: %+v", diags)
	}
}

// TestUnresolvedSlotFailsTheAction (D11, L23) — rendering with a missing
// value FAILS. A template engine's default is to emit the placeholder,
// and a PR body containing {{pr_summary}} is worse than no PR: visibly
// broken, in a permanent record, attributable to whoever opened it.
func TestUnresolvedSlotFailsTheAction(t *testing.T) {
	v := filledValues()
	delete(v.Handoff, "pr_summary")
	_, err := Render("pr-body", v)
	if err == nil {
		t.Fatal("a missing value must fail the render, not emit the placeholder or a hole")
	}
	if !strings.Contains(err.Error(), "pr_summary") {
		t.Fatalf("the failure names the unresolved slot: %v", err)
	}
}

// TestNoBracesReachARenderedBody (D11, C8) — asserted as a CLASS: no `{{`
// in ANY rendered output of any template. A per-slot test only covers the
// slots someone remembered.
func TestNoBracesReachARenderedBody(t *testing.T) {
	for _, spec := range TemplateRegistry() {
		out, err := Render(spec.Name, filledValues())
		if err != nil {
			t.Fatalf("%s: %v", spec.Name, err)
		}
		if strings.Contains(out, "{{") || strings.Contains(out, "}}") {
			t.Fatalf("%s leaked braces into a body people read:\n%s", spec.Name, out)
		}
	}
	// And the class holds even when a rogue value TRIES to smuggle one.
	v := filledValues()
	v.Handoff["pr_summary"] = "sneaky {{pr_number}} injection"
	if _, err := Render("pr-body", v); err == nil {
		t.Fatal("a brace pair smuggled in prose looks exactly like a failed render in a permanent record — the class assertion is LITERAL: nothing with braces reaches a body people read")
	}
	// Odd-but-braceless prose rides through unharmed.
	v.Handoff["pr_summary"] = "100% done — see diff --stat output: a[2] {x}"
	out, err := Render("pr-body", v)
	if err != nil || !strings.Contains(out, "100% done") {
		t.Fatalf("brackets are fine, brace pairs are not: %v", err)
	}
}

// TestEmptyStringIsNotSubstituted — an empty paragraph in a PR body is
// the same failure as a placeholder.
func TestEmptyStringIsNotSubstituted(t *testing.T) {
	v := filledValues()
	v.Handoff["pr_summary"] = "   "
	if _, err := Render("pr-body", v); err == nil {
		t.Fatal("whitespace-only prose must fail like absence — an empty paragraph is the same hole")
	}
}

// TestIdentifierSlotsCannotComeFromHandoffs (L3, I3) — a handoff field
// named pr_number CANNOT fill an identifier slot, or a model could supply
// a PR number. The resolver reads identifier slots from evidence and
// state only.
func TestIdentifierSlotsCannotComeFromHandoffs(t *testing.T) {
	v := filledValues()
	delete(v.Evidence, "pr_number") // the real evidence is gone
	v.Handoff["pr_number"] = "999"  // the model volunteers one
	_, err := Render("review-body", v)
	if err == nil {
		out, _ := Render("review-body", v)
		t.Fatalf("a handoff pr_number filled an identifier slot: %s", out)
	}
	// And when evidence exists, the handoff cannot override it.
	v.Evidence["pr_number"] = "3"
	if _, err := Render("review-body", v); err != nil {
		t.Fatalf("with real evidence the render succeeds: %v", err)
	}
}

// TestSlotsUseTheManifestDeclaration (S8's L18) — one field vocabulary:
// the resolver's manifest-side paths come from the SAME declaration the
// conditions evaluate against, asserted by set equality.
func TestSlotsUseTheManifestDeclaration(t *testing.T) {
	got := ManifestSlotPaths()
	want := blueprint.Slots()
	// The manifest-side vocabulary IS blueprint's — not a copy that can
	// drift: set equality both directions.
	if len(got) != len(want) {
		t.Fatalf("slot vocabulary diverged from the manifest declaration: %v vs %v", got, want)
	}
	set := map[string]bool{}
	for _, w := range want {
		set[w] = true
	}
	for _, g := range got {
		if !set[g] {
			t.Fatalf("%q is not in the manifest declaration (three copies drift — L18)", g)
		}
	}
}

// TestDiagnosticCarriesKindAndPath — like S8's seven classes (S8's O1).
func TestDiagnosticCarriesKindAndPath(t *testing.T) {
	diags := ValidateTemplates([]TemplateSpec{{
		Name:   "review-body",
		Source: "{{nope}}",
		Slots:  map[string]SlotKind{},
	}})
	if len(diags) != 1 || diags[0].Kind == "" || diags[0].Path == "" {
		t.Fatalf("kind and path are the contract: %+v", diags)
	}
}

// TestTemplatesRenderDeterministically — the same inputs give
// byte-identical output; a PR body that varies between runs makes a diff
// meaningless.
func TestTemplatesRenderDeterministically(t *testing.T) {
	for _, spec := range TemplateRegistry() {
		a, err := Render(spec.Name, filledValues())
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			b, err := Render(spec.Name, filledValues())
			if err != nil || a != b {
				t.Fatalf("%s render %d differs — a varying body makes diffs meaningless", spec.Name, i)
			}
		}
	}
}

// The registry's Q8 shape: three templates, each slot kinded, every slot
// of every template resolvable from one of the three sources.
func TestQ8SlotTableIsComplete(t *testing.T) {
	reg := TemplateRegistry()
	if len(reg) != 3 {
		t.Fatalf("pr-body, issue-body, review-body: %d", len(reg))
	}
	names := map[string]bool{}
	for _, s := range reg {
		names[s.Name] = true
		if len(s.Slots) == 0 {
			t.Fatalf("%s declares no slots", s.Name)
		}
		// Every slot appears in the source text (dead declarations are
		// their own kind of drift).
		for slot := range s.Slots {
			if !strings.Contains(s.Source, "{{"+slot+"}}") {
				t.Fatalf("%s declares %s but never uses it", s.Name, slot)
			}
		}
	}
	for _, want := range []string{"pr-body", "issue-body", "review-body"} {
		if !names[want] {
			t.Fatalf("missing %s", want)
		}
	}
}
