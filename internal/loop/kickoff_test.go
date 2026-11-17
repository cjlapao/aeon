package loop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
)

// kickoff_test.go pins S39 T7's two promises: the run's first message is a
// renderer's output (or a person's typing) that reads EXACTLY like the other
// (CR6/D7 — byte-identical model input), and the loop that was never given a
// kickoff sees the prompt it has always seen, byte for byte (D1/D11).

// kickoff_test.go pins S39 T7's two promises — restated by S68 (B3): the
// run's first message is a renderer's output (or a person's typing) that
// reads EXACTLY like the other (CR6/D7 — byte-identical model input), and
// the kickoff-less loop sees the goal it has always seen (D1/D11), now
// VERBATIM in the user message: no `Goal:` line, no lane, nothing but the
// words (S68's split).

func TestWordsGoldenForALoopWithNoKickoff(t *testing.T) {
	st := LoopState{Goal: "G", RuntimeState: RuntimeState{Position: Position{Lane: "only"}}}
	if got := ModelWords(st); got != "G" {
		t.Fatalf("a kickoff-less loop's user message is the goal VERBATIM (S68 B3):\n got %q\nwant %q", got, "G")
	}
	step := blueprint.Step{ID: "only", Kind: blueprint.ModelStep, Agent: "default", Produces: blueprint.Paths{"OUT.md"}}
	got := TaskFrame(st, step)
	const wantFrame = "The person's own words in your message are the goal. Work the blueprint position named here.\nCurrent blueprint lane: only\nFile the declared artifact(s) with `doc_write`, named exactly as declared: OUT.md. They are stored in the database, not on disk. If you do not hold `doc_write`, write each with `write` in the workspace and it is filed for you. Verify each exists before reporting ok."
	if got != wantFrame {
		t.Fatalf("the frame of a kickoff-less loop moved (D1/D11):\n got %q\nwant %q", got, wantFrame)
	}
	// And the frame speaks no goal of its own — the goal lives in the words
	// alone, one home per kind of word.
	if strings.Contains(got, "Goal: G") {
		t.Fatalf("the frame re-speaks the goal — the pre-S68 double-entry:\n%s", got)
	}
}

func TestModelPromptUsesTheRunKickoff(t *testing.T) {
	st := LoopState{Goal: "the goal text", RuntimeState: RuntimeState{Position: Position{Lane: "only"}, Kickoff: "K"}}
	// S68: the kickoff IS the user message, verbatim.
	if got := ModelWords(st); got != "K" {
		t.Fatalf("the kickoff must ride the user message verbatim: %q", got)
	}
	step := blueprint.Step{ID: "only", Kind: blueprint.ModelStep, Agent: "default"}
	frame := TaskFrame(st, step)
	// The frame carries POSITION, not words: it must not smuggle the
	// kickoff or the goal into the instruction half.
	if strings.Contains(frame, "the goal text") {
		t.Fatalf("the goal text leaked into the frame: %q", frame)
	}
}

func TestTypedAndTemplatedFirstMessagesGiveByteIdenticalModelInput(t *testing.T) {
	bp := &blueprint.Blueprint{Kickoff: "Work on {{.Goal}}"}
	data := blueprint.KickoffData{Goal: "X"}
	templated, tsrc, err := KickoffFor(bp, data, "")
	if err != nil {
		t.Fatal(err)
	}
	typed, usrc, err := KickoffFor(nil, blueprint.KickoffData{}, "Work on X")
	if err != nil {
		t.Fatal(err)
	}
	if templated != typed {
		t.Fatalf("the renderer and the typing disagree: %q vs %q", templated, typed)
	}
	if tsrc != "template" || usrc != "user" {
		t.Fatalf("the SOURCES must differ while the text does not (CR6): %q vs %q", tsrc, usrc)
	}
	step := blueprint.Step{ID: "only", Kind: blueprint.ModelStep, Agent: "default"}
	a := LoopState{Goal: "X", RuntimeState: RuntimeState{Position: Position{Lane: "only"}, Kickoff: templated}}
	b := LoopState{Goal: "X", RuntimeState: RuntimeState{Position: Position{Lane: "only"}, Kickoff: typed}}
	if ModelWords(a) != ModelWords(b) || TaskFrame(a, step) != TaskFrame(b, step) {
		t.Fatalf("D7 broken — model input differs by provenance:\n%q\n%q", ModelWords(a), ModelWords(b))
	}
	if !strings.Contains(ModelWords(a), "Work on X") {
		t.Fatalf("the kickoff never reached the words: %q", ModelWords(a))
	}
}

func TestRuntimeJSONOmitsAnEmptyKickoff(t *testing.T) {
	raw, err := json.Marshal(RuntimeState{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"kickoff"`) {
		t.Fatalf("an empty kickoff wrote a key — the saved state of EVERY existing loop would change (D1): %s", raw)
	}
	st := LoopState{RuntimeState: RuntimeState{Kickoff: "first message"}}
	state, err := st.RuntimeJSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := runtimeFromJSON(state)
	if err != nil {
		t.Fatal(err)
	}
	if back.Kickoff != "first message" {
		t.Fatalf("the kickoff did not round-trip through the saved state: %q", back.Kickoff)
	}
}

// runtimeFromJSON decodes a runtime state the way FromPinned does — the
// round-trip is the promise, so the test uses the same codec path.
func runtimeFromJSON(raw string) (RuntimeState, error) {
	var rs RuntimeState
	err := json.Unmarshal([]byte(raw), &rs)
	return rs, err
}

func TestKickoffForTypedWins(t *testing.T) {
	bp := &blueprint.Blueprint{Kickoff: "template says {{.Goal}}"}
	text, source, err := KickoffFor(bp, blueprint.KickoffData{Goal: "X"}, "the person typed this")
	if err != nil {
		t.Fatal(err)
	}
	if text != "the person typed this" || source != "user" {
		t.Fatalf("typed must win over any template: %q/%q", text, source)
	}
}

func TestKickoffForRendersTheTemplate(t *testing.T) {
	bp := &blueprint.Blueprint{Kickoff: "Work on {{.Goal}} for {{.Component}}"}
	text, source, err := KickoffFor(bp, blueprint.KickoffData{Goal: "X", Component: "alpha"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Work on X for alpha" || source != "template" {
		t.Fatalf("got %q/%q", text, source)
	}
}

func TestKickoffForDefaultsToTheBodies(t *testing.T) {
	if got, src, _ := KickoffFor(nil, blueprint.KickoffData{Goal: "G"}, ""); got != "G" || src != "template" {
		t.Fatalf("goal only: %q/%q", got, src)
	}
	if got, src, _ := KickoffFor(nil, blueprint.KickoffData{Task: "T"}, ""); got != "T" || src != "template" {
		t.Fatalf("task only: %q/%q", got, src)
	}
	got, src, _ := KickoffFor(nil, blueprint.KickoffData{Goal: "G", Task: "T"}, "")
	if got != "G\n\nT" || src != "template" {
		t.Fatalf("both: %q/%q, want G\\n\\nT/template", got, src)
	}
}

func TestKickoffForNothingIsEmpty(t *testing.T) {
	text, source, err := KickoffFor(nil, blueprint.KickoffData{}, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if text != "" || source != "" {
		t.Fatalf("nothing to say must say NOTHING (no source to file): %q/%q", text, source)
	}
}

func TestKickoffAuthor(t *testing.T) {
	for source, want := range map[string]string{"user": "human", "template": "human", "automation": "system", "blueprint": "system"} {
		if got := KickoffAuthor(source); got != want {
			t.Errorf("KickoffAuthor(%q)=%q, want %q", source, got, want)
		}
	}
}
