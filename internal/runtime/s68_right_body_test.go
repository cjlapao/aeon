package runtime

// S68 — "the right body". The loop-start block (workspace rules, how-to-work,
// policy, skills) rides the SYSTEM message; the operator's own words ride the
// USER message alone; the conversation history rides as real alternating
// messages. These tests pin the B-rulings of
// docs/superpowers/specs/2026-10-31-loop-start-right-body-design.md at the
// choke, so the inversion cannot silently return.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestEverySectionMustChooseADestination(t *testing.T) {
	// B1: unset destination is a construction failure, refused by name —
	// the same discipline D4 enforces for Category.
	for _, tc := range []struct {
		name string
		dest SectionDestination
	}{
		{"unset", ""},
		{"invented", SectionDestination("sideways")},
	} {
		res := AssembleContext(context.Background(), []SectionSpec{
			{Title: "Rules", Text: "be kind", Category: CategorySystemPrompt, Destination: tc.dest},
		}, ContextDeps{})
		if res.OK {
			t.Fatalf("%s: assembly accepted a section that never chose a message", tc.name)
		}
		if !strings.Contains(res.Reason, "Rules") || !strings.Contains(res.Reason, string(tc.dest)) {
			t.Errorf("%s: the refusal must name the section and the value it refused: %q", tc.name, res.Reason)
		}
	}
	// And the refusal lands at COMPILE too (nothing reaches a provider).
	res := CompileRequest(context.Background(), CompileInput{
		Handler: StepHandler{
			Name: "h", System: "s", MaxOutputTokens: 64,
			Sections: []SectionSpec{{Title: "Task", Text: "go", Category: CategorySystemPrompt}},
		},
		Granted: []string{"read"}, Route: testRoute(),
	}, compileDeps(nil))
	if res.OK || !strings.Contains(res.Reason, "destination") {
		t.Fatalf("compile must refuse an unlabelled section, got OK=%v reason=%q", res.OK, res.Reason)
	}
}

func TestHalvesLandInTheirOwnMessages(t *testing.T) {
	// B1/B2/B3: two texts, two messages — never concatenated again.
	handler := StepHandler{
		Name: "h", System: "be terse", MaxOutputTokens: 64,
		Sections: []SectionSpec{
			{Title: "", Text: "rename the thing please", Category: CategorySystemPrompt, Destination: SectionTranscript},
			{Title: "Workspace", Text: "paths stay inside", Category: CategorySystemPrompt, Destination: SectionSystem},
		},
	}
	res := CompileRequest(context.Background(), CompileInput{
		Handler: handler, Granted: nil, Route: testRoute(),
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}
	if len(res.Request.Messages) != 2 {
		t.Fatalf("want [system, user], got %d messages", len(res.Request.Messages))
	}
	system := res.Request.Messages[0].Content[0].Text
	user := res.Request.Messages[1].Content[0].Text
	if strings.Contains(user, "paths stay inside") || strings.Contains(user, "be terse") {
		t.Errorf("instruction text rode the user message:\n%s", user)
	}
	if !strings.Contains(system, "paths stay inside") || !strings.Contains(system, "be terse") {
		t.Errorf("the system message is missing its halves:\n%s", system)
	}
	if strings.TrimSpace(user) != "rename the thing please" {
		t.Errorf("the user message must be the operator's words verbatim, got %q", user)
	}
	// The persona leads the system message; the declared sections follow it,
	// and each half keeps DECLARED order (K13 survives the split).
	if strings.Index(system, "be terse") > strings.Index(system, "## Workspace") {
		t.Errorf("the persona must lead its message:\n%s", system)
	}
}

func TestAStepWithNobodySpeakingIsRefused(t *testing.T) {
	// B3: every section on the system side means an empty user message — a
	// monologue, refused at construction rather than shipped.
	res := CompileRequest(context.Background(), CompileInput{
		Handler: StepHandler{
			Name: "h", System: "s", MaxOutputTokens: 64,
			Sections: []SectionSpec{
				{Title: "Workspace", Text: "paths stay inside", Category: CategorySystemPrompt, Destination: SectionSystem},
			},
		},
		Granted: []string{"read"}, Route: testRoute(),
	}, compileDeps(nil))
	if res.OK {
		t.Fatal("a step with no operator words compiled — the user message would be empty")
	}
	if !strings.Contains(res.Reason, "empty user message") {
		t.Errorf("the refusal must say what it refused: %q", res.Reason)
	}
}

func TestHistoryRidesAsRealMessagesInOrder(t *testing.T) {
	// B4: [system, …history, user]; only user/assistant may ride.
	handler := DefaultHandler("what changed since?", nil)
	history := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("first ask")}},
		{Role: model.RoleAssistant, Content: []model.Block{model.TextBlock("first answer")}},
	}
	res := CompileRequest(context.Background(), CompileInput{
		Handler: handler, Route: testRoute(), History: history,
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}
	msgs := res.Request.Messages
	if len(msgs) != 4 {
		t.Fatalf("want system + 2 history + user, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleSystem || msgs[3].Role != model.RoleUser {
		t.Fatalf("the pair must bracket the history: %v", []model.Role{msgs[0].Role, msgs[3].Role})
	}
	if msgs[1].Content[0].Text != "first ask" || msgs[2].Content[0].Text != "first answer" {
		t.Error("history lost its order or its words")
	}
	// The new words close the transcript VERBATIM — no seam newline from
	// the renderer rides the operator's message (B3; OpeningTranscript is
	// the single boundary that trims the renderer's own seam).
	if msgs[3].Content[0].Text != "what changed since?" {
		t.Errorf("the new words must close the transcript verbatim, got %q", msgs[3].Content[0].Text)
	}

	// A system-author row never rides mid-conversation (B4), and a tool row
	// smuggled in as history is refused by position and role.
	for _, bad := range []model.Role{model.RoleSystem, model.Role("tool")} {
		res := CompileRequest(context.Background(), CompileInput{
			Handler: handler, Route: testRoute(),
			History: []model.Message{
				{Role: bad, Content: []model.Block{model.TextBlock("smuggled")}},
			},
		}, compileDeps(nil))
		if res.OK || !strings.Contains(res.Reason, "history message 0") {
			t.Fatalf("role %q must be refused by position: OK=%v reason=%q", bad, res.OK, res.Reason)
		}
	}
}

func TestSpokenPolicySentencesAndUnknownTypeHonesty(t *testing.T) {
	// B5: known types speak as sentences; unknown types speak the literal
	// rather than vanishing; the section closes with the enforcement line.
	set := []PolicyEntry{
		{ID: "b", Type: "tool_access", TargetKind: "all-agents", Action: "deny", Source: "operator",
			Spec: json.RawMessage(`{"toolName":"bash"}`)},
		{ID: "a", Type: "command_access", TargetKind: "agent", TargetID: "boss", Action: "ask", Source: "operator",
			Spec: json.RawMessage(`{"command":"git push *"}`)},
		{ID: "c", Type: "weather_access", TargetKind: "all-agents", Action: "deny", Source: "operator"},
	}
	asm := AssembleContext(context.Background(), []SectionSpec{
		{Title: "", Text: "go", Category: CategorySystemPrompt, Destination: SectionTranscript},
	}, ContextDeps{Policies: set})
	if !asm.OK {
		t.Fatal(asm.Reason)
	}
	sys := asm.SystemText
	if strings.Contains(asm.TranscriptText, "Policy") {
		t.Error("policy rode the operator's message")
	}
	for _, want := range []string{
		"You may not use the bash tool.",
		"For agent boss: The shell command `git push *` needs a human's approval before it runs.",
		"Policy says: weather_access for all-agents: is deny.",
		"The daemon enforces these rules",
	} {
		if !strings.Contains(sys, want) {
			t.Errorf("the spoken section is missing %q:\n%s", want, sys)
		}
	}
	// Deterministic: the ID-sorted order the hash digests (a, b, c).
	if strings.Index(sys, "git push") > strings.Index(sys, "bash tool") {
		t.Error("spoken rules must follow the canonical (ID-sorted) order")
	}
	// No policies: NO heading at all (P31 — silence is not "no rules").
	asm2 := AssembleContext(context.Background(), []SectionSpec{
		{Title: "", Text: "go", Category: CategorySystemPrompt, Destination: SectionTranscript},
	}, ContextDeps{})
	if strings.Contains(asm2.SystemText, "## Policy") {
		t.Error("an unset policy set must not render a ## Policy heading")
	}
}
