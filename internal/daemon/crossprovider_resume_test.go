package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// TestStoreUnmodifiedAtEveryPoint (S18 T8, D2, C3) — the STORE half of the
// round trip, asserted at the store itself: the signed thinking block is
// read back after each of the three legs — the anthropic step that
// suspended, the resume that ran on OpenAI, and the next step back on
// Anthropic — byte-identical to what was written. A leak that passed
// unnoticed here is the failure mode the third leg exists to catch: a
// transcript quietly stripped of foreign-produced thinking would only
// surface when somebody tried to go back.
func TestStoreUnmodifiedAtEveryPoint(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	sc := store.Scope{LoopSlug: "trip", ComponentRoot: root}
	if _, err := s.CreateLoop(store.NewLoop{Slug: sc.LoopSlug, ComponentRoot: root, Root: root, Goal: "g", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	step, err := s.CreateStep(sc, store.NewStep{Phase: "build", Agent: "a", Source: "live"})
	if err != nil {
		t.Fatal(err)
	}
	signed := model.Block{Kind: model.BlockThinking, Text: "reasoned about it", Signature: "sig-anthropic-abc"}
	signedBytes, _ := json.Marshal(signed)
	if err := s.AppendMessages(step.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("do the thing")}},
		{Role: model.RoleAssistant, Content: []model.Block{signed, model.TextBlock("thought done")},
			Provenance: &model.Provenance{Route: "claude-route", Provider: "anthropic", Model: "claude-opus"}},
		{Role: model.RoleUser, Content: []model.Block{model.TextBlock("continue")}},
	}); err != nil {
		t.Fatal(err)
	}

	writer := daemonRecoveryWriter{st: s, stepID: step.ID, loopSlug: sc.LoopSlug}
	claude := model.Route{ID: "claude-route", Provider: "anthropic", Model: "claude-opus", MaxOutputTokens: 1024}
	gpt := model.Route{ID: "gpt-route", Provider: model.ProviderOpenAI, Model: "gpt-mini",
		MaxOutputTokens: 1024, OpenAIEndpoint: string(model.OpenAIEndpointChat)}
	runner := StepRunner{}
	caller, specs, err := runner.BuildTools([]string{"question"}, root, "a", runtime.FrozenManifest{})
	if err != nil {
		t.Fatal(err)
	}
	handoff := runtime.HandoffSpec{Required: []string{"status"}}
	newReq := func(route model.Route) runtime.StepRequest {
		tr := s.ReadTranscript(step.ID)
		if !tr.OK {
			t.Fatalf("transcript: %s", tr.Reason())
		}
		return runtime.StepRequest{Handler: runtime.DefaultHandler("a", []string{"question"}),
			Messages: tr.Messages, Manifest: runtime.ContextManifest{MaxIterations: 5},
			Route: route, Tools: specs, WorkspaceRoot: root, StepID: step.ID}
	}

	// --- leg 1: anthropic, suspends on a question ------------------------
	m1 := &scriptedModel{turns: []modelTurn{{toolUses: []model.Block{
		{Kind: model.BlockToolUse, ID: "q1", Name: "question", Input: json.RawMessage(`{"text":"Proceed?"}`)},
	}}}}
	resA := runtime.RunStep(context.Background(), newReq(claude), runtime.RunOpts{
		Client: m1, Tools: caller, Checkpoint: writer, HandoffSpec: handoff})
	if resA.Kind != runtime.ResultSuspended || resA.Question == nil {
		t.Fatalf("leg 1 did not suspend: %+v", resA)
	}
	assertSignedIntact(t, s, step.ID, string(signedBytes), "after the anthropic suspend")

	// --- leg 2: resume on OpenAI ------------------------------------------
	reqB := runtime.ResumeRequest{Request: newReq(gpt), Messages: resA.Messages, Manifest: resA.Manifest,
		Question: *resA.Question, Answer: "yes"}
	m2 := &scriptedModel{turns: []modelTurn{{text: `{"status":"ok","summary":"answered on gpt"}`}}}
	resB := runtime.ResumeStep(context.Background(), reqB, runtime.RunOpts{
		Client: m2, Tools: caller, Checkpoint: writer, HandoffSpec: handoff})
	if resB.Kind != runtime.ResultComplete || resB.Outcome != runtime.OutcomeOK {
		t.Fatalf("leg 2 ended %s/%s (%s)", resB.Kind, resB.Outcome, resB.Reason)
	}
	if m2.requests[0].Route.ID != "gpt-route" {
		t.Fatal("the resume did not run on the loop's current route (Q6)")
	}
	assertSignedIntact(t, s, step.ID, string(signedBytes), "after the OpenAI resume — the leg where a leak would hide")

	// --- leg 3: the next step, back on Anthropic ---------------------------
	reqC := newReq(claude)
	reqC.Messages = append(append([]model.Message(nil), resB.Messages...),
		model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock("next task")}})
	m3 := &scriptedModel{turns: []modelTurn{{text: `{"status":"ok","summary":"back home"}`}}}
	resC := runtime.RunStep(context.Background(), reqC, runtime.RunOpts{
		Client: m3, Tools: caller, Checkpoint: writer, HandoffSpec: handoff})
	if resC.Outcome != runtime.OutcomeOK {
		t.Fatalf("leg 3: %s (%s)", resC.Outcome, resC.Reason)
	}
	assertSignedIntact(t, s, step.ID, string(signedBytes), "after the return to anthropic — the record survived")

	// And the transcript now speaks both providers, in order (T1's column).
	tr := s.ReadTranscript(step.ID)
	var prov []string
	for _, m := range tr.Messages {
		if m.Role == model.RoleAssistant && m.Provenance != nil {
			prov = append(prov, m.Provenance.Provider)
		}
	}
	if len(prov) < 3 || prov[0] != "anthropic" || prov[len(prov)-1] != "anthropic" {
		t.Errorf("stored provenance across the trip = %v, want anthropic … anthropic with openai between (M26)", prov)
	}
	sawOpenAI := false
	for _, p := range prov {
		if p == "openai" {
			sawOpenAI = true
		}
	}
	if !sawOpenAI {
		t.Errorf("the resume turn reached the store without its openai provenance: %v", prov)
	}
}

func assertSignedIntact(t *testing.T, s *store.Store, stepID int64, wantJSON, when string) {
	t.Helper()
	tr := s.ReadTranscript(stepID)
	if !tr.OK {
		t.Fatalf("transcript after %s: %s", when, tr.Reason())
	}
	for _, m := range tr.Messages {
		for _, b := range m.Content {
			if b.Kind == model.BlockThinking {
				raw, _ := json.Marshal(b)
				if string(raw) != wantJSON {
					t.Fatalf("after %s the stored block reads %s, want %s — byte-identical is the standard (M17, D2)", when, raw, wantJSON)
				}
				return
			}
		}
	}
	t.Fatalf("after %s the thinking block is GONE from the store — the leak the third leg exists to catch (C3)", when)
}

// TestRecoveryAlsoHonoursTheCurrentRoute (S18 T8, Q6's other half) — S7
// re-issued a crashed step from the SAVED REQUEST's route, so a crash
// between a steered switch and the next dispatch silently reverted it.
// Recovery now reads the loop's CURRENT route; and when that route no longer
// resolves, recovery FAILS LOUDLY — running the old route instead would be
// the silent revert wearing reliability's clothes.
func TestRecoveryAlsoHonoursTheCurrentRoute(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	// A PINNED loop whose runtime state says the loop is on `gpt-route`.
	if _, err := s.CreatePinnedLoop(store.NewLoop{Slug: "recovery", ComponentRoot: root, Root: root, Goal: "test", Status: "running"},
		"dev-loop", 1, "name: dev-loop\n", `{"route":"gpt-route"}`); err != nil {
		t.Fatal(err)
	}
	id := seedRecoveryStep(t, s, root, "", false) // snapshot request froze route "r" — the pre-switch route
	_ = id

	gpt := model.Route{ID: "gpt-route", Provider: model.ProviderOpenAI, Model: "gpt-mini",
		MaxOutputTokens: 1024, OpenAIEndpoint: string(model.OpenAIEndpointChat)}
	m := &scriptedModel{turns: []modelTurn{{text: `{"status":"ok","summary":"recovered"}`}}}
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify, Models: m,
		Routes: protocol.NewStaticRoutes(gpt, model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024})})
	if _, err := d.Recover(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(m.requests) == 0 {
		t.Fatal("recovery issued no model call")
	}
	if got := m.requests[0].Route.ID; got != "gpt-route" {
		t.Fatalf("the re-issued call ran on %q — a crash must not silently revert a deliberate switch (Q6)", got)
	}

	// The loud half: a current route that is not configured fails recovery
	// rather than falling back.
	s2 := recoveryStore(t)
	root2 := t.TempDir()
	if _, err := s2.CreatePinnedLoop(store.NewLoop{Slug: "recovery", ComponentRoot: root2, Root: root2, Goal: "test", Status: "running"},
		"dev-loop", 1, "name: dev-loop\n", `{"route":"vanished"}`); err != nil {
		t.Fatal(err)
	}
	seedRecoveryStep(t, s2, root2, "", false)
	m2 := &scriptedModel{turns: []modelTurn{{text: `{"status":"ok","summary":"recovered"}`}}}
	w2 := &wire{}
	d2 := New(Options{Store: s2, Logger: w2, Notify: w2.notify, Models: m2,
		Routes: protocol.NewStaticRoutes(model.Route{ID: "r", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024})})
	summary, err := d2.Recover(context.Background(), false)
	if err == nil && summary.Failed == 0 {
		t.Fatal("recovery ran the pre-switch route quietly while the loop named a route that does not exist")
	}
	w2.mu.Lock()
	defer w2.mu.Unlock()
	for _, line := range w2.logs {
		if strings.Contains(line, "vanished") {
			return
		}
	}
	t.Error("the loud failure did not name the unresolvable route")
}
