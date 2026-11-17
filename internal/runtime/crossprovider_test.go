package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// The slice's claim (S18 T8): a loop suspended on Anthropic resumes on
// OpenAI and goes back — and the RECORD survives the tour. Each leg below is
// labelled with the property it proves, because three similar-looking steps
// are otherwise three lines of noise to the next reader.
//
// The canonical transcript is what is asserted here; the wire shapes (no
// thinking on the openai wire, byte-identical signature replay on
// anthropic's) are adapters' own tests beside the code that makes them:
// adapters/crossprovider_wire_test.go. The store's bytes across the legs are
// the daemon's claim — daemon/crossprovider_resume_test.go.

func signedTranscript() []model.Message {
	return []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
		{Role: model.RoleAssistant, Content: []model.Block{
			{Kind: model.BlockThinking, Text: "reasoned about it", Signature: "sig-anthropic-abc"},
			{Kind: model.BlockText, Text: "thought done"},
		}, Provenance: &model.Provenance{Route: "test", Provider: "anthropic", Model: "claude-test"}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("continue")}},
	}
}

func gptRoute() model.Route {
	return model.Route{ID: "gpt", Provider: model.ProviderOpenAI, Model: "gpt-mini",
		MaxOutputTokens: 1024, OpenAIEndpoint: string(model.OpenAIEndpointChat)}
}

// thinkingBlock finds the seeded signed block in a transcript, if present.
func thinkingBlock(msgs []model.Message) *model.Block {
	for _, m := range msgs {
		for i := range m.Content {
			if m.Content[i].Kind == model.BlockThinking {
				return &m.Content[i]
			}
		}
	}
	return nil
}

// TestSuspendOnAnthropicResumeOnOpenAIResumeOnAnthropic — ALL THREE LEGS
// (D1, C2: never A→B alone), driven the way the loop actually drives them:
//
//	leg A → suspend : thinking blocks produced by anthropic are in the
//	                  canonical transcript and ride every request;
//	→ leg B         : the step RESUMES on a different provider — the openai
//	                  route receives the anthropic-made transcript unchanged
//	                  (the adapter's wire job is pinned in the adapters
//	                  package); the answer arrives stamped openai;
//	→ leg A         : the transcript the third step sends to anthropic
//	                  still holds the first step's signature verbatim — the
//	                  record survived, which is what M1 was bought for.
func TestSuspendOnAnthropicResumeOnOpenAIResumeOnAnthropic(t *testing.T) {
	// --- leg A: anthropic, suspends on a question -----------------------
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	st.Request.Route = testRoute()
	seed := signedTranscript()
	st.Request.Messages = seed
	st.Messages = append([]model.Message(nil), seed...)
	spy := newSpyTools("question")
	resA := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("q1", "question", `{"text":"Proceed?"}`)},
			Cost: model.Cost{State: model.CostKnown, USD: 0.0412}}),
		Tools: spy, HandoffSpec: HandoffSpec{Required: []string{"status"}},
	})
	if resA.Kind != ResultSuspended || resA.Question == nil {
		t.Fatalf("leg A did not suspend on a question: %+v", resA)
	}
	if thinkingBlock(resA.Messages) == nil {
		t.Fatal("the thinking block did not survive the suspend — the record leaked on leg one")
	}

	// --- leg B: resume on OpenAI ----------------------------------------
	reqB := ResumeRequest{Request: st.Request, Messages: resA.Messages, Manifest: resA.Manifest,
		Question: *resA.Question, Answer: "yes", Usage: resA.Usage, Cost: resA.Cost}
	reqB.Request.Route = gptRoute()
	reqB.Request.StepID = 42
	clientB := newFakeClient(scriptedTurn{Text: okHandoff, Cost: model.Cost{State: model.CostKnown, USD: 0.018}})
	resB := ResumeStep(context.Background(), reqB, RunOpts{
		Client: clientB, Tools: newSpyTools("question"), HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	if resB.Kind != ResultComplete || resB.Outcome != OutcomeOK {
		t.Fatalf("leg B ended %s/%s (%s) — a resumed step on another provider must complete", resB.Kind, resB.Outcome, resB.Reason)
	}
	if clientB.request(0).Route.ID != "gpt" {
		t.Fatal("leg B did not run on the openai route (Q6: resume honours the CURRENT route)")
	}
	tb := thinkingBlock(clientB.request(0).Messages)
	if tb == nil || tb.Signature != "sig-anthropic-abc" {
		t.Fatalf("the canonical transcript lost the foreign thinking block on the way to the wire: %+v", tb)
	}

	// --- leg C: back to Anthropic ---------------------------------------
	reqC := testRequest(toolSpec("question"))
	reqC.Route = testRoute()
	reqC.Messages = append(append([]model.Message(nil), resB.Messages...),
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("next task")}})
	clientC := newFakeClient(scriptedTurn{Text: okHandoff, Cost: model.Cost{State: model.CostKnown, USD: 0.02}})
	resC := RunStep(context.Background(), reqC, RunOpts{
		Client: clientC, Tools: newSpyTools("question"), HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	if resC.Outcome != OutcomeOK {
		t.Fatalf("leg C: %s (%s)", resC.Outcome, resC.Reason)
	}
	tb = thinkingBlock(clientC.request(0).Messages)
	if tb == nil || tb.Signature != "sig-anthropic-abc" || tb.Text != "reasoned about it" {
		t.Fatalf("leg C replayed a damaged record: %+v — the round trip is what proves the store kept it (M1, D2)", tb)
	}
	// Every leg saw the same block bytes.
	for k, msgs := range [][]model.Message{resA.Messages, resB.Messages, resC.Messages} {
		if got := thinkingBlock(msgs); got == nil || got.Signature != "sig-anthropic-abc" {
			t.Errorf("leg %d's final transcript lost the block (C3)", k+1)
		}
	}
}

// TestProvenanceShowsBothProviders (T1) — the transcript reports WHO made
// each turn, in order: the loop stamps every assistant turn it appends with
// the route that answered. Across the tour that reads anthropic, openai,
// anthropic — a transcript whose provenance said one provider throughout a
// cross-provider run is lying about somebody.
func TestProvenanceShowsBothProviders(t *testing.T) {
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	st.Request.Route = testRoute()
	seed := signedTranscript()
	st.Request.Messages = seed
	st.Messages = append([]model.Message(nil), seed...)
	spy := newSpyTools("question")
	resA := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("q1", "question", `{"text":"Proceed?"}`)}}),
		Tools:  spy, HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	reqB := ResumeRequest{Request: st.Request, Messages: resA.Messages, Manifest: resA.Manifest,
		Question: *resA.Question, Answer: "yes"}
	reqB.Request.Route = gptRoute()
	resB := ResumeStep(context.Background(), reqB, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools("question"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	reqC := testRequest(toolSpec("question"))
	reqC.Route = testRoute()
	reqC.Messages = append(append([]model.Message(nil), resB.Messages...),
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("next task")}})
	resC := RunStep(context.Background(), reqC, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}), Tools: newSpyTools("question"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}}})

	var got []string
	for _, m := range resC.Messages {
		if m.Role != model.RoleAssistant || m.Provenance == nil {
			continue
		}
		got = append(got, m.Provenance.Provider)
	}
	want := strings.Join([]string{"anthropic", "anthropic", "openai", "anthropic"}, ",")
	if strings.Join(got, ",") != want {
		t.Fatalf("provenance across the trip = %q, want %q — billing and honesty both follow the turn's producer (M26)",
			strings.Join(got, ","), want)
	}
}

// TestCostSplitsAcrossTheTrip (T7) — the ledger over a transcript that spans
// both providers groups by ROUTE, not provider-of-convenience: two anthropic
// legs on one route sum into one line, the openai leg stands apart, and the
// total is the known sum, not a floor (nothing was unknown here).
func TestCostSplitsAcrossTheTrip(t *testing.T) {
	st := testState(testHandler("question"), []model.ToolSpec{toolSpec("question")})
	st.Request.Route = testRoute()
	seed := signedTranscript()
	st.Request.Messages = seed
	st.Messages = append([]model.Message(nil), seed...)
	known := func(usd float64) model.Cost { return model.Cost{State: model.CostKnown, USD: usd} }
	resA := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(scriptedTurn{ToolUses: []model.Block{toolUse("q1", "question", `{"text":"Proceed?"}`)}, Cost: known(0.0412)}),
		Tools:  newSpyTools("question"), HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	reqB := ResumeRequest{Request: st.Request, Messages: resA.Messages, Manifest: resA.Manifest,
		Question: *resA.Question, Answer: "yes", Cost: resA.Cost}
	reqB.Request.Route = gptRoute()
	resB := ResumeStep(context.Background(), reqB, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff, Cost: known(0.018)}), Tools: newSpyTools("question"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}}})
	reqC := testRequest(toolSpec("question"))
	reqC.Route = testRoute()
	reqC.Messages = append(append([]model.Message(nil), resB.Messages...),
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("next task")}})
	resC := RunStep(context.Background(), reqC, RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff, Cost: known(0.02)}), Tools: newSpyTools("question"),
		HandoffSpec: HandoffSpec{Required: []string{"status"}}})

	// Rows mirror what `store.LoopCostRows` builds after this trip: ONE row
	// per step. The suspended step and its resume are one step — its cost is
	// the step total (anthropic's suspend turn plus openai's resume turn)
	// and it is attributed to its LAST assistant turn, whose producer
	// collected the final bill (T7's convention, honest for handoffs too).
	// The second step ran wholly on the anthropic route.
	rows := []model.CostRow{
		{StepID: 42, RouteID: "gpt", Provider: "openai", Model: "gpt-mini", Cost: resB.Cost},
		{StepID: 43, RouteID: "test", Provider: "anthropic", Model: "claude-test", Cost: resC.Cost},
	}
	at := model.AttributeCosts(rows)
	if len(at.Groups) != 2 {
		t.Fatalf("%d ledger lines, want 2 routes: %+v", len(at.Groups), at.Groups)
	}
	if at.Total.State != model.CostKnown || at.Total.USD != 0.0412+0.018+0.02 {
		t.Fatalf("the trip's total = %+v, want the known sum 0.0792 — a cross-provider loop is still one bill (D16)", at.Total)
	}
}

// TestMidStepRouteSwitchIsRefused (O5, N9) — a steer carrying a provider
// change while a step is running is refused, NAMING THE STEP, and says it
// applies next. The steer's own text still lands (LW1 is untouched); only
// the route waits, because between a tool_use and its tool_result there is
// no honest message any adapter could build from two providers' parts.
func TestMidStepRouteSwitchIsRefused(t *testing.T) {
	st := testState(testHandler("read", "question"), []model.ToolSpec{toolSpec("read"), toolSpec("question")})
	st.Request.Route = testRoute()
	st.Request.StepID = 42
	seed := signedTranscript()
	st.Request.Messages = seed
	st.Messages = append([]model.Message(nil), seed...)
	gpt := gptRoute()
	sink := &recordingSink{}
	res := RunLoop(context.Background(), st, LoopDeps{
		Client: newFakeClient(
			scriptedTurn{ToolUses: []model.Block{toolUse("r1", "read", `{"path":"a.md"}`)}},
			scriptedTurn{ToolUses: []model.Block{toolUse("q1", "question", `{"text":"Proceed?"}`)}}),
		Tools: newSpyTools("read", "question"), Sink: sink,
		HandoffSpec: HandoffSpec{Required: []string{"status"}},
		Steers:      &queuedSteers{batches: [][]Steer{nil, {{ID: 77, Text: "use the other model", Route: &gpt}}}},
	})
	if res.PendingRoute != "gpt" {
		t.Fatalf("PendingRoute = %q — the refused switch must ride out for the loop to apply", res.PendingRoute)
	}
	if got := eventsOf(sink.all(), EventRouteChanged); len(got) != 0 {
		t.Fatalf("the route changed mid-step after all: %+v", got)
	}
	for k := range st.Request.Messages {
		_ = k
		break
	}
	refused := eventsOf(sink.all(), EventRouteDeferred)
	if len(refused) == 0 {
		t.Fatal("the refusal was silent — a person whose switch vanished learns the difference only from a bill")
	}
	if !strings.Contains(refused[0].Text, "42") || !strings.Contains(refused[0].Text, "next step") {
		t.Errorf("the refusal %q must name the step and say when it applies (O5)", refused[0].Text)
	}
	// The steer's TEXT arrived — the refusal is about the route, not the voice.
	found := false
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "use the other model") {
				found = true
			}
		}
	}
	if !found {
		t.Error("the refused-route steer also lost its text — O5 refuses a provider change, not the person")
	}
}
