package runtime

import (
	"context"
	"strings"
	"testing"
)

// S66 T2. The invariant that matters most here is the ABSENCE one: an
// un-wired daemon must compile byte-identical transcripts to itself
// without the seam — "the catalog changed my prompts" may only be
// true of daemons that opted in.

func skillsTestInput() CompileInput {
	return CompileInput{
		Handler: StepHandler{
			Name:   "h",
			System: "be terse",
			Sections: []SectionSpec{
				{Title: "Task", Text: "do it", Category: CategorySystemPrompt, Destination: SectionTranscript},
				{Title: "Plan", Text: "the plan", Category: CategoryInjectedContext, Destination: SectionTranscript},
			},
			Tools:           []string{"read"},
			MaxOutputTokens: 512,
		},
		Granted:       []string{"read"},
		Route:         testRoute(),
		WorkspaceRoot: "/workspace",
	}
}

func compileWithSkills(t *testing.T, in CompileInput) CompileResult {
	t.Helper()
	res := CompileRequest(context.Background(), in, compileDeps(nil))
	if !res.OK {
		t.Fatalf("compile refused: %s", res.Reason)
	}
	return res
}

// Operator ruling 2026-11-06 (reversing S66): the catalog is never offered to
// a step. A skill reaches the model only when the operator chose it with "/".
// A wired provider must therefore change nothing about the compile.
func TestAWiredCatalogIsNeverOfferedToAStep(t *testing.T) {
	t.Cleanup(func() { SetSkillsProvider(nil) })
	SetSkillsProvider(nil)
	before := compileWithSkills(t, skillsTestInput())
	SetSkillsProvider(func() *SkillsSection {
		return &SkillsSection{Text: "CATALOG", Names: []string{"x"}, Count: 1}
	})
	wired := compileWithSkills(t, skillsTestInput())

	if wired.Request.SkillsInjected != nil {
		t.Fatal("a wired catalog must not be reported as injected: the offer is gone")
	}
	for _, n := range wired.Notes {
		if strings.Contains(n, "skills injected") {
			t.Fatalf("no skills-injected note may ride a step: %v", wired.Notes)
		}
	}
	for i := range before.Request.Messages {
		if before.Request.Messages[i].Content[0].Text != wired.Request.Messages[i].Content[0].Text {
			t.Fatalf("a wired catalog changed message %d — it must not reach the model", i)
		}
		if strings.Contains(wired.Request.Messages[i].Content[0].Text, "CATALOG") {
			t.Fatal("the catalog leaked into a compile")
		}
	}
}

func TestMarkerSentenceShapes(t *testing.T) {
	s := &SkillsSection{Names: []string{"a", "b"}, Count: 2}
	if got := s.MarkerSentence(); got != "skills injected: 2 — a, b" {
		t.Fatalf("default marker: %q", got)
	}
	s.Truncated = true
	if got := s.MarkerSentence(); !strings.HasSuffix(got, "(+more)") {
		t.Fatalf("truncated marker: %q", got)
	}
	s.Marker = "the daemon says it"
	if got := s.MarkerSentence(); got != "the daemon says it" {
		t.Fatal("the daemon's ONE sentence outranks the default")
	}
}

func TestStepStartedCarriesTheSkillsMarker(t *testing.T) {
	// The dispatcher contract (T3): the marker rides step.started, once,
	// with the section's own words — the chat renders it verbatim.
	sink := &recordingSink{}
	req := testRequest(toolSpec("read"))
	req.SkillsInjected = &SkillsSection{
		Text: "…", Names: []string{"caveman", "docx"}, Count: 2, Marker: "skills injected: 2 — caveman, docx",
	}
	res := RunStep(context.Background(), req, runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), newSpyTools("read"), sink))
	if res.Outcome != OutcomeOK {
		t.Fatalf("the step did not complete: %s %s", res.Outcome, res.Reason)
	}
	var started []StepEvent
	for _, e := range sink.all() {
		if e.Kind != EventStepStarted {
			continue
		}
		started = append(started, e)
	}
	if len(started) != 1 {
		t.Fatalf("step.started fired %d times, want exactly one", len(started))
	}
	got := started[0].Skills
	if got == nil || got.Count != 2 || got.Names[0] != "caveman" || got.Marker != "skills injected: 2 — caveman, docx" {
		t.Fatalf("the marker did not ride step.started: %+v", got)
	}
	// And it is a one-time opening fact: later events carry none.
	for _, e := range sink.events {
		if e.Kind != EventStepStarted && e.Skills != nil {
			t.Fatalf("only the opening carries the marker, %s did too", e.Kind)
		}
	}
}

func TestStepStartedCarriesNoMarkerWhenUnwired(t *testing.T) {
	sink := &recordingSink{}
	res := RunStep(context.Background(), testRequest(toolSpec("read")), runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), newSpyTools("read"), sink))
	if res.Outcome != OutcomeOK {
		t.Fatalf("the step did not complete: %s %s", res.Outcome, res.Reason)
	}
	for _, e := range sink.all() {
		if e.Kind != EventStepStarted {
			continue
		}
		if e.Skills != nil {
			t.Fatalf("an un-wired step must emit no marker at all, got %+v", e.Skills)
		}
	}
}
