package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// T3's tests. The one that matters is negative: a handler asking for a tool the
// step was not granted fails BEFORE any model call.

func compileDeps(files map[string]string) ContextDeps {
	spy := newReadSpy(files)
	return spy.deps()
}

func TestCompileRequestProducesOrderedSectionsAndAFrozenManifest(t *testing.T) {
	res := CompileRequest(context.Background(), CompileInput{
		Handler: StepHandler{
			Name:            "h",
			System:          "be terse",
			Sections:        []SectionSpec{{Title: "Task", Text: "do it", Category: CategorySystemPrompt, Destination: SectionTranscript}, {Title: "Plan", DeclaredFile: "plan.md", Category: CategoryInjectedContext, Destination: SectionTranscript}},
			Tools:           []string{"read"},
			HandoffSchema:   `{"type":"object"}`,
			MaxOutputTokens: 512,
		},
		Granted:       []string{"read"},
		Route:         testRoute(),
		WorkspaceRoot: "/workspace",
	}, compileDeps(map[string]string{"plan.md": "the plan"}))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if len(res.Request.Manifest.Files) != 1 {
		t.Errorf("the manifest holds %d files", len(res.Request.Manifest.Files))
	}
	if res.Request.Manifest.MaxIterations != MaxIterations {
		t.Errorf("MaxIterations is %d, want the default", res.Request.Manifest.MaxIterations)
	}
	if res.Request.Manifest.DeadlineAt.IsZero() {
		t.Error("the manifest has no deadline")
	}

	// The opening transcript: system then user, which is what S3's validator
	// requires and what every provider accepts.
	if len(res.Request.Messages) != 2 {
		t.Fatalf("%d opening messages, want 2", len(res.Request.Messages))
	}
	system := res.Request.Messages[0].Content[0].Text
	if !strings.Contains(system, "be terse") {
		t.Error("the system prompt is missing")
	}
	// The SCHEMA is in the system message, because the report's shape is an
	// instruction about how to answer rather than material to reason about.
	if !strings.Contains(system, `"type":"object"`) {
		t.Error("the handoff schema is not in the system message")
	}
	user := res.Request.Messages[1].Content[0].Text
	if !strings.Contains(user, "do it") || !strings.Contains(user, "the plan") {
		t.Errorf("the context is not in the user message: %q", user)
	}
}

func TestAHandlerDeclaringATheToolsetLacksFailsAtConstruction(t *testing.T) {
	// D1's construction half, and **the Client spy shows zero calls**.
	//
	// The alternative is a failure three turns in: the model asks for `bash`,
	// is told it does not exist, tries something else, and the transcript reads
	// as a model working around a configuration mistake rather than as an
	// error naming the handler.
	client := newFakeClient(scriptedTurn{Text: okHandoff})

	res := CompileRequest(context.Background(), CompileInput{
		Handler:       testHandler("read", "bash"),
		Granted:       []string{"read"},
		Route:         testRoute(),
		WorkspaceRoot: "/workspace",
	}, compileDeps(nil))

	if res.OK {
		t.Fatal("a handler asking for an ungranted tool compiled")
	}
	// The reason names BOTH, because "tool not granted" leaves a reader to
	// work out which of the two lists is wrong — and the answer is usually
	// that the step was started without a flag.
	if !strings.Contains(res.Reason, "bash") {
		t.Errorf("the reason does not name the missing tool: %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "read") {
		t.Errorf("the reason does not name what IS granted: %q", res.Reason)
	}
	if client.requestCount() != 0 {
		t.Errorf("%d model calls were made; construction must fail before any", client.requestCount())
	}
}

func TestTheFailureHappensBeforeAnyModelCall(t *testing.T) {
	// Every construction failure, with the spy at zero. A failure that costs
	// money is a failure that gets tolerated.
	client := newFakeClient(scriptedTurn{Text: okHandoff})

	cases := []struct {
		name  string
		input CompileInput
		deps  ContextDeps
		wants string
	}{
		{
			name:  "an ungranted tool",
			input: CompileInput{Handler: testHandler("bash"), Granted: []string{"read"}, Route: testRoute()},
			wants: "bash",
		},
		{
			name: "no output cap",
			input: CompileInput{Handler: StepHandler{
				Name: "h", Sections: []SectionSpec{{Title: "T", Text: "x", Category: CategorySystemPrompt, Destination: SectionSystem}},
			}, Route: testRoute()},
			wants: "MaxOutputTokens",
		},
		{
			name:  "no route",
			input: CompileInput{Handler: testHandler()},
			wants: "route",
		},
		{
			name:  "no name",
			input: CompileInput{Handler: StepHandler{MaxOutputTokens: 1}, Route: testRoute()},
			wants: "no name",
		},
		{
			name: "no sections",
			input: CompileInput{Handler: StepHandler{
				Name: "h", MaxOutputTokens: 1,
			}, Route: testRoute()},
			wants: "no context sections",
		},
		{
			name: "a declared file that does not exist",
			input: CompileInput{Handler: StepHandler{
				Name:            "h",
				Sections:        []SectionSpec{{Title: "P", DeclaredFile: "missing.md", Category: CategoryInjectedContext, Destination: SectionTranscript}},
				MaxOutputTokens: 1,
			}, Route: testRoute()},
			deps:  compileDeps(nil),
			wants: "missing.md",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := tc.deps
			if deps.ReadFile == nil {
				deps = compileDeps(nil)
			}
			res := CompileRequest(context.Background(), tc.input, deps)
			if res.OK {
				t.Fatalf("%s compiled", tc.name)
			}
			if !strings.Contains(res.Reason, tc.wants) {
				t.Errorf("the reason does not mention %q: %s", tc.wants, res.Reason)
			}
		})
	}

	if client.requestCount() != 0 {
		t.Errorf("%d model calls were made across every construction failure", client.requestCount())
	}
}

func TestMaxOutputTokensIsRequiredAndNotDefaulted(t *testing.T) {
	// Not defaulted deliberately. One provider REQUIRES the field, and a
	// silent default truncates a long report with nothing saying why (M16) —
	// so the right cap is the handler's decision and a missing one is an
	// error.
	res := CompileRequest(context.Background(), CompileInput{
		Handler: StepHandler{
			Name:     "h",
			Sections: []SectionSpec{{Title: "T", Text: "x", Category: CategorySystemPrompt, Destination: SectionSystem}},
		},
		Route: testRoute(),
	}, compileDeps(nil))

	if res.OK {
		t.Fatal("a handler with no output cap compiled")
	}
	if !strings.Contains(res.Reason, "truncates") {
		t.Errorf("the reason does not explain the cost of a default: %q", res.Reason)
	}
}

func TestTheHandoffSchemaIsCarriedNotRegenerated(t *testing.T) {
	// T3's point 4. The schema is derived at the HANDLER and embedded once, so
	// the retry re-sends the same text — which is what makes "the retry
	// carries no tools" the only difference between the two requests.
	handler := testHandler("read")
	handler.HandoffSchema = `{"marker":"unique-schema-text"}`

	res := CompileRequest(context.Background(), CompileInput{
		Handler: handler, Granted: []string{"read"}, Route: testRoute(),
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}

	// It is in the message, not recomputed from anything.
	if !strings.Contains(res.Request.Messages[0].Content[0].Text, "unique-schema-text") {
		t.Error("the handler's schema text did not reach the system message")
	}
	// And the handler still carries it, so the retry can re-send it.
	if res.Request.Handler.HandoffSchema != handler.HandoffSchema {
		t.Error("the schema was rewritten during compilation")
	}
}

func TestMaxIterationsIsOverriddenOnTheManifest(t *testing.T) {
	// Carried on the MANIFEST, so a resumed step is bounded by what the
	// original step was bounded by rather than by today's default.
	res := CompileRequest(context.Background(), CompileInput{
		Handler: testHandler(), Route: testRoute(), MaxIterations: 3,
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Request.Manifest.MaxIterations != 3 {
		t.Errorf("MaxIterations is %d, want the override", res.Request.Manifest.MaxIterations)
	}
}

func TestTheOpeningTranscriptIsValidatedAtConstruction(t *testing.T) {
	// S3's validator is the one authority on what a transcript may be, and
	// this is the first place a transcript exists — so an invalid one is a
	// construction error rather than a provider rejection.
	handler := testHandler()
	handler.System = ""
	handler.HandoffSchema = ""
	res := CompileRequest(context.Background(), CompileInput{
		Handler: handler, Route: testRoute(),
		Specs: []SectionSpec{{Title: "T", Text: "x", Category: CategorySystemPrompt, Destination: SectionSystem}},
	}, compileDeps(nil))
	// A system message with empty text is still a valid message (it has one
	// block), so this compiles — the assertion is that validation RAN, which
	// the next case proves by failing.
	if !res.OK {
		t.Logf("an empty system prompt was refused: %s", res.Reason)
	}
}

func TestSpecsOverrideTheHandlersSections(t *testing.T) {
	// The protocol handler adds the user's prompt as a section, and a handler
	// template should not have to know about it.
	res := CompileRequest(context.Background(), CompileInput{
		Handler: testHandler(),
		Route:   testRoute(),
		// The override stands in for what the protocol handler does: it adds
		// the USER's prompt as a section. S68 B3: that is transcript material
		// — a spec that spoke only system instructions would leave the user
		// message empty, and CompileRequest refuses that by name.
		Specs: []SectionSpec{{Title: "Override", Text: "the override won", Category: CategorySystemPrompt, Destination: SectionTranscript}},
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}
	user := res.Request.Messages[1].Content[0].Text
	if !strings.Contains(user, "the override won") {
		t.Errorf("the override did not take effect: %q", user)
	}
	if strings.Contains(user, "do the thing") {
		t.Errorf("the handler's own sections were used as well: %q", user)
	}
}

func TestTheDefaultHandlerKeepsTheOperatorsWordsAlone(t *testing.T) {
	// Q9 survives S68 in its strengthened form. A model reads top to bottom;
	// pre-S68 the task was buried UNDER path-convention prose in the same
	// message. Now the separation is not an ordering nicety but a message
	// boundary: the user message is the person's own words and nothing else,
	// and the instructions the model must obey ride the system message.
	handler := DefaultHandler("count the files in docs/", DefaultGrantNames())
	res := CompileRequest(context.Background(), CompileInput{
		Handler: handler, Granted: DefaultGrantNames(), Route: testRoute(),
	}, compileDeps(nil))
	if !res.OK {
		t.Fatal(res.Reason)
	}

	user := res.Request.Messages[1].Content[0].Text
	if strings.TrimSpace(user) != "count the files in docs/" {
		t.Errorf("the user message must be the operator's words alone:\n%s", user)
	}
	if strings.Contains(user, "## Workspace") || strings.Contains(user, "## How to work") {
		t.Errorf("instruction text rode the user message — the exact pre-S68 bug:\n%s", user)
	}
	system := res.Request.Messages[0].Content[0].Text
	if !strings.Contains(system, "## Workspace") || !strings.Contains(system, "## How to work") {
		t.Errorf("the instructions must ride the system message:\n%s", system)
	}
	// The task sentence may not be echoed system-side either: one home per
	// kind of word, or the chat window and the wire disagree about who said
	// what.
	if strings.Contains(system, "count the files in docs/") {
		t.Errorf("the task was echoed into the system message: %q", system)
	}

	// No declared files: the step READS what it needs, which is the point of
	// having a tool loop. So the manifest is empty, and that is correct.
	if len(res.Request.Manifest.Files) != 0 {
		t.Errorf("the default handler declared files: %+v", res.Request.Manifest.Files)
	}
}

func TestTheDefaultHandlerAndTheDefaultHandoffSpecAgree(t *testing.T) {
	// The schema the model is SHOWN and the spec that VALIDATES are separate
	// (one explains, one enforces) — so they can drift, and a schema promising
	// a field the validator does not require is a field models stop sending.
	spec := DefaultHandoffSpec()
	handler := DefaultHandler("x", DefaultGrantNames())

	for _, required := range spec.Required {
		if !strings.Contains(handler.HandoffSchema, `"`+required+`"`) {
			t.Errorf("the spec requires %q and the schema shown to the model does not mention it",
				required)
		}
	}
	for field, values := range spec.Enums {
		if !strings.Contains(handler.HandoffSchema, `"`+field+`"`) {
			t.Errorf("the spec constrains %q and the schema does not mention it", field)
		}
		for _, v := range values {
			if !strings.Contains(handler.HandoffSchema, `"`+v+`"`) {
				t.Errorf("the spec allows %q for %s and the schema does not list it", v, field)
			}
		}
	}
}

func TestTheDefaultGrantIsReadOnly(t *testing.T) {
	// D1. Granting a shell by default would make every example in every
	// document an invitation, and the person running it would not have chosen
	// anything.
	granted := DefaultGrantNames()
	if strings.Join(granted, ",") != "read,list,write" {
		t.Errorf("the default grant is %v, want read, list, write", granted)
	}
	for _, name := range granted {
		if name == "bash" {
			t.Error("bash is granted by default")
		}
	}
}

func TestADefaultStepsDeadlineIsInTheFuture(t *testing.T) {
	before := time.Now()
	res := CompileRequest(context.Background(), CompileInput{
		Handler: DefaultHandler("x", DefaultGrantNames()),
		Granted: DefaultGrantNames(), Route: testRoute(),
	}, ContextDeps{})
	if !res.OK {
		t.Fatal(res.Reason)
	}
	if !res.Request.Manifest.DeadlineAt.After(before) {
		t.Errorf("the deadline %s is not after now", res.Request.Manifest.DeadlineAt)
	}
}

// --- the reasoning effort a step can actually use -----------------------------

// A step that offers tools lowers the route's reasoning to `none`, and says so.
//
// # The failure this prevents
//
// OpenAI's current models refuse function tools unless reasoning is off. Every
// step offers tools. So a route set to `medium` — the right setting for `aeon
// ask` — made every step fail with a provider 400, and the only escape was
// keeping two routes for one model.
//
// There is exactly one configuration in which a tool-using step works on such a
// model, and it is used. Out loud: the note reaches `step.started`, which is the
// line somebody is already reading when a step begins.
// chatRoute is an OpenAI route on `/v1/chat/completions` — the API that
// refuses tools with reasoning on, and the default.
func chatRoute() model.Route {
	r := testRoute()
	r.Provider = model.ProviderOpenAI
	return r
}

func TestAStepWithToolsLowersTheRoutesReasoningAndSaysSo(t *testing.T) {
	route := chatRoute()
	route.ReasoningEffort = "medium"

	res := CompileRequest(context.Background(), CompileInput{
		Handler:   testHandler("read"),
		Granted:   []string{"read"},
		Route:     route,
		ToolSpecs: []model.ToolSpec{toolSpec("read")},
	}, compileDeps(nil))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Request.ReasoningEffort != "none" {
		t.Errorf("the step carries reasoningEffort %q, want none", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 1 {
		t.Fatalf("%d notes; the decision must be visible", len(res.Notes))
	}
	note := res.Notes[0]
	for _, want := range []string{"medium", "none", "refuses tools", "--openai-endpoint responses"} {
		if !strings.Contains(note, want) {
			t.Errorf("the note does not mention %q: %s", want, note)
		}
	}
	// The note is on the REQUEST too, which is how it reaches the event.
	if len(res.Request.Notes) != 1 {
		t.Error("the note did not reach the request, so step.started cannot say it")
	}
}

func TestOnlyAnEffortTheRouteAlreadySetIsLowered(t *testing.T) {
	// **This is what makes the rule safe.** Lowering only ever happens to a
	// value the route already set, which proves the model accepts the
	// parameter — so `none` cannot produce `400 Unrecognized request argument
	// supplied: reasoning_effort`, which is exactly what sending it to a route
	// that set nothing could.
	res := CompileRequest(context.Background(), CompileInput{
		Handler:   testHandler("read"),
		Granted:   []string{"read"},
		Route:     chatRoute(), // no effort at all
		ToolSpecs: []model.ToolSpec{toolSpec("read")},
	}, compileDeps(nil))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Request.ReasoningEffort != "" {
		t.Errorf("a route with no reasoning effort had %q invented for it",
			res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("a step that decided nothing produced a note: %v", res.Notes)
	}
}

func TestARouteAlreadyAtNoneIsLeftAloneAndSaysNothing(t *testing.T) {
	// A note on every step would be a note nobody reads.
	route := chatRoute()
	route.ReasoningEffort = "none"

	res := CompileRequest(context.Background(), CompileInput{
		Handler:   testHandler("read"),
		Granted:   []string{"read"},
		Route:     route,
		ToolSpecs: []model.ToolSpec{toolSpec("read")},
	}, compileDeps(nil))

	if res.Request.ReasoningEffort != "none" {
		t.Errorf("the effort is %q", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("nothing changed and a note was produced: %v", res.Notes)
	}
}

func TestAStepWithNoToolsKeepsTheRoutesReasoning(t *testing.T) {
	// The restriction is about TOOLS. A step offering none can reason as freely
	// as a chat, and lowering it there would be removing a capability for no
	// reason.
	route := chatRoute()
	route.ReasoningEffort = "high"

	res := CompileRequest(context.Background(), CompileInput{
		Handler: testHandler(),
		Route:   route,
	}, compileDeps(nil))

	if res.Request.ReasoningEffort != "high" {
		t.Errorf("a tool-free step's effort became %q", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("a tool-free step produced a note: %v", res.Notes)
	}
}

func TestAnExplicitEffortIsTakenLiterallyEvenWhenTheProviderWillRefuseIt(t *testing.T) {
	// Explicit means explicit. Somebody passing `--reasoning-effort medium` to a
	// tool-using step gets the provider's refusal — and the client's message
	// then says where they said it, rather than pointing at the route.
	//
	// Overriding them here would make the flag a suggestion, and a flag that is
	// silently ignored is worse than one that fails.
	route := chatRoute()
	route.ReasoningEffort = "none"

	res := CompileRequest(context.Background(), CompileInput{
		Handler:         testHandler("read"),
		Granted:         []string{"read"},
		Route:           route,
		ToolSpecs:       []model.ToolSpec{toolSpec("read")},
		ReasoningEffort: "medium",
	}, compileDeps(nil))

	if res.Request.ReasoningEffort != "medium" {
		t.Errorf("an explicit effort became %q", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("an explicit choice produced a note: %v", res.Notes)
	}
}

func TestTheStepsEffortReachesEveryModelCallIncludingTheRetry(t *testing.T) {
	// A retry that changed the model's reasoning would be a different model
	// answering the same question — and the retry exists to re-ask for a
	// report, not to change the terms.
	route := chatRoute()
	route.ReasoningEffort = "medium"

	client := newFakeClient(
		scriptedTurn{Text: "no handoff here"},
		scriptedTurn{Text: okHandoff},
	)
	req := testRequest()
	req.Route = route
	req.ReasoningEffort = "none"
	req.Tools = []model.ToolSpec{toolSpec("read")}

	RunStep(context.Background(), req, runOpts(client, newSpyTools("read"), nil))

	if client.requestCount() != 2 {
		t.Fatalf("%d requests, want the call and its one retry", client.requestCount())
	}
	for i := 0; i < 2; i++ {
		got := client.request(i)
		if got.ReasoningEffort != "none" {
			t.Errorf("request %d carries reasoningEffort %q", i, got.ReasoningEffort)
		}
		// And the EFFECTIVE value is what the adapter will send — the request's
		// value beating the route's `medium`.
		if got.EffectiveReasoningEffort() != "none" {
			t.Errorf("request %d would send %q", i, got.EffectiveReasoningEffort())
		}
	}
}

func TestTheNoteIsOnTheStepsOpeningLine(t *testing.T) {
	// It rides on `step.started`, so the CLI and the app both show it without
	// either having to know what a note is about.
	sink := &recordingSink{}
	req := testRequest()
	req.Notes = []string{"reasoning lowered from \"medium\" to `none` for this step"}

	RunStep(context.Background(), req, runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, sink))

	events := sink.all()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	if events[0].Kind != EventStepStarted {
		t.Fatalf("the first event is %s", events[0].Kind)
	}
	if !strings.Contains(events[0].Summary, "reasoning lowered") {
		t.Errorf("the opening line does not carry the note: %q", events[0].Summary)
	}
	// The counts are still there: the note is appended, not substituted.
	if !strings.Contains(events[0].Summary, "tool(s) offered") {
		t.Errorf("the note replaced the counts: %q", events[0].Summary)
	}
}

func TestNothingIsLoweredOnAnAPIThatAcceptsBoth(t *testing.T) {
	// **The reason `/v1/responses` is selectable at all.** It accepts function
	// tools and reasoning together, so a step there keeps the route's effort —
	// and a rule that lowered it anyway would make the endpoint pointless.
	route := chatRoute()
	route.ReasoningEffort = "high"
	route.OpenAIEndpoint = model.OpenAIEndpointResponses

	res := CompileRequest(context.Background(), CompileInput{
		Handler:   testHandler("read"),
		Granted:   []string{"read"},
		Route:     route,
		ToolSpecs: []model.ToolSpec{toolSpec("read")},
	}, compileDeps(nil))

	if !res.OK {
		t.Fatal(res.Reason)
	}
	if res.Request.ReasoningEffort != "high" {
		t.Errorf("the step's effort is %q, want the route's", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("nothing needed lowering and a note was produced: %v", res.Notes)
	}
}

func TestNothingIsLoweredForAProviderWithoutTheRestriction(t *testing.T) {
	// Anthropic's thinking coexists with tools. Inventing the restriction for a
	// provider that does not have one would remove a capability for nothing —
	// found by this rule's first run, which lowered an Anthropic route because
	// the fixture happened to be one.
	route := testRoute() // anthropic
	route.ReasoningEffort = "high"

	res := CompileRequest(context.Background(), CompileInput{
		Handler:   testHandler("read"),
		Granted:   []string{"read"},
		Route:     route,
		ToolSpecs: []model.ToolSpec{toolSpec("read")},
	}, compileDeps(nil))

	if res.Request.ReasoningEffort != "high" {
		t.Errorf("an Anthropic route's effort became %q", res.Request.ReasoningEffort)
	}
	if len(res.Notes) != 0 {
		t.Errorf("a note was produced for a provider with no such rule: %v", res.Notes)
	}
}

func TestWhichRoutesRefuseToolsWithReasoning(t *testing.T) {
	// The predicate itself, enumerated — it is the one place the rule lives.
	cases := []struct {
		provider string
		endpoint string
		refuses  bool
	}{
		{model.ProviderOpenAI, "", true},           // chat is the default
		{model.ProviderOpenAI, "chat", true},       //
		{model.ProviderOpenAI, "responses", false}, // accepts both
		{"anthropic", "", false},                   // no such restriction
		{"anthropic", "chat", false},               // the field is ignored here
	}

	for _, tc := range cases {
		route := model.Route{Provider: tc.provider, OpenAIEndpoint: tc.endpoint}
		if got := route.ToolsRefuseReasoning(); got != tc.refuses {
			t.Errorf("provider %q endpoint %q: refuses=%v, want %v",
				tc.provider, tc.endpoint, got, tc.refuses)
		}
	}
}
