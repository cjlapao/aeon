package daemon

// records_scorecard_test.go — S63 T6 (RT11, rule 4): the three scorecard
// arms of the records service. What each test pins, in the order the spec
// lists them:
//
//   - OWN SCOPE ONLY: the subject of every arm is the step's stamped
//     identity — ident.Agent for scorecard_mine, ident.RootID for the
//     component and digest arms — and an identity word smuggled into the
//     input is a PARSE error, never a re-aimed subject (T6.1);
//   - NO OTHER AGENT'S DATA and NO RANKING BY THE BACK DOOR: the
//     component twin drops the person's per-child breakdown (T6.2);
//   - ONE IMPLEMENTATION, TWO DOORS: every arm answers byte-identically to
//     the very service call the person's socket door makes (T6 / rule 4),
//     compared through canonical JSON;
//   - a PERSON session reaching the agent-scoped names is turned back with
//     the pointer sentence to the person's own door (the MC2/LM7 shape);
//   - unwired, the arms say so BY NAME (V13) — no card from nothing.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
)

// newScorecardRecordsEnv wires the arms over the SAME constructor the boot
// uses (daemon.go S63 T5/T6: table.Scorecard/newScorecardReads and the
// recordsSvc assignment beside it).
func newScorecardRecordsEnv(t *testing.T) recordsEnv {
	t.Helper()
	e := newRecordsEnv(t, "")
	e.svc.scorecardPlane = newScorecardReads(e.s)
	e.svc.digestPlane = newDigestReads(e.s)
	return e
}

// recordWalkOutcome files one finished loop's fact, with its agents.
func recordWalkOutcome(t *testing.T, e recordsEnv, slug, kind string, complete bool, terminal string, agents ...string) {
	t.Helper()
	ag := make([]store.RetroAgent, 0, len(agents))
	for _, a := range agents {
		ag = append(ag, store.RetroAgent{Agent: a, AgentSlugKnown: true})
	}
	zero := 0
	o := store.RetroOutcome{
		ComponentRoot: e.comp.Root, LoopSlug: slug, TerminalKey: slug + terminal,
		OutcomeKind: kind, Complete: complete, Source: "recorder",
		SecurityRounds: &zero, QARounds: &zero, TotalFailedGates: &zero,
		Escalations: &zero, UnverifiedSteps: &zero,
	}
	if _, _, err := e.s.RecordRetroOutcome(o, ag); err != nil {
		t.Fatalf("record %s: %v", slug, err)
	}
}

// seedTwoAgents files one finished loop for the harness agent and one for
// another agent in the SAME tree — the shape where a leak or a ranking
// would show: the only difference between the callers is who is stamped.
func seedTwoAgents(t *testing.T) (recordsEnv, int64) {
	t.Helper()
	e := newScorecardRecordsEnv(t)
	recordWalkOutcome(t, e, "walk", "done", true, ":done:1", "implementer")
	recordWalkOutcome(t, e, "scout-loop", "failed", false, ":failed:1", "scout")
	// A second step for the other agent in the SAME loop tree, to prove the
	// stamp is the STEP's agent, not a name in the input.
	scoutStep, err := e.s.CreateStep(store.Scope{LoopSlug: "walk", ComponentRoot: e.comp.Root},
		store.NewStep{TaskID: "T2", Phase: "build", Agent: "scout"})
	if err != nil {
		t.Fatalf("scout step: %v", err)
	}
	return e, scoutStep.ID
}

func scorecardCanon(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// T6 / rule 4 — one implementation, two doors: each arm IS the socket
// service call, field for field.
func TestToolResultsMatchSocketResults(t *testing.T) {
	e, _ := seedTwoAgents(t)
	sc := e.svc.scorecardPlane
	dg := e.svc.digestPlane

	twin := func(tool string, in string, door func() (any, error)) {
		t.Helper()
		toolOut, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, tool, json.RawMessage(in))
		if err != nil || f != nil {
			t.Fatalf("%s %s: f=%v err=%v", tool, in, f, err)
		}
		doorOut, err := door()
		if err != nil {
			t.Fatalf("door: %v", err)
		}
		if a, b := scorecardCanon(t, toolOut), scorecardCanon(t, doorOut); a != b {
			t.Errorf("%s drifted from its socket twin:\n tool: %s\n door: %s", tool, a, b)
		}
	}

	// Explicit windows: the parity claim is that two doors answer the SAME
	// thing, and it must not be a coin-flip on the clock's second.
	const win = `"since":"2026-01-01T00:00:00Z","until":"2027-01-01T00:00:00Z"`
	twin("scorecard_mine", `{`+win+`}`, func() (any, error) {
		return sc.Agent(e.ctx, protocol.ScorecardAgentParams{Agent: "implementer", RootID: e.proj,
			ScorecardWindow: protocol.ScorecardWindow{Since: "2026-01-01T00:00:00Z", Until: "2027-01-01T00:00:00Z"}})
	})
	twin("digest_learned", `{`+win+`}`, func() (any, error) {
		return dg.Learned(e.ctx, protocol.DigestParams{ID: e.proj, Since: "2026-01-01T00:00:00Z", Until: "2027-01-01T00:00:00Z"})
	})
	twin("scorecard_component", `{`+win+`}`, func() (any, error) {
		res, err := sc.Component(e.ctx, protocol.ScorecardComponentParams{ID: e.proj, IncludeChildren: true,
			ScorecardWindow: protocol.ScorecardWindow{Since: "2026-01-01T00:00:00Z", Until: "2027-01-01T00:00:00Z"}})
		if err != nil {
			return nil, err
		}
		// The twin of the DOOR is the door's own answer minus the breakdown
		// the tool may not carry — the card and the sentence cannot drift.
		return protocol.ScorecardCardResult{Card: res.Card, Statement: res.Statement}, nil
	})
}

// T6.1 — the stamp is the step's agent. A step stamped implementer can
// never answer scout's card, whatever the input claims; and scout's own
// step answers scout's card. The cards differ because the loops differ.
func TestScorecardMineIsStampedToTheCaller(t *testing.T) {
	e, scoutStepID := seedTwoAgents(t)

	outA, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "scorecard_mine", json.RawMessage(`{}`))
	if err != nil || f != nil {
		t.Fatalf("implementer: f=%v err=%v", f, err)
	}
	jsonA := scorecardCanon(t, outA)
	if !strings.Contains(jsonA, `"implementer"`) || strings.Contains(jsonA, `"scout"`) {
		t.Errorf("implementer's card is not its own: %s", jsonA)
	}

	scoutCtx := stepctx.WithStepID(e.ctx, scoutStepID)
	outB, f, err := e.svc.Do(scoutCtx, Actor{Kind: "agent", ID: "scout"}, "scorecard_mine", json.RawMessage(`{}`))
	if err != nil || f != nil {
		t.Fatalf("scout: f=%v err=%v", f, err)
	}
	jsonB := scorecardCanon(t, outB)
	if !strings.Contains(jsonB, `"scout"`) || strings.Contains(jsonB, `"implementer"`) {
		t.Errorf("scout's card is not its own: %s", jsonB)
	}
	if jsonA == jsonB {
		t.Errorf("the two agents' cards are identical — the stamp did not reach the computation:\n %s", jsonA)
	}

	// The self-chosen subject is a PARSE error, not a shrug: additional-
	// Properties:false means {"agent":...} never even names a subject.
	for _, smuggle := range []string{`{"agent":"scout"}`, `{"slug":"scout"}`, `{"rootId":"/repo/alpha"}`} {
		if _, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "scorecard_mine", json.RawMessage(smuggle)); f == nil && err == nil {
			t.Errorf("smuggled %s was not a parse error", smuggle)
		}
	}
}

// T6.2 — no per-agent rows and no breakdown by the back door. The person's
// component door answers the breakdown; the tool's answer CANNOT carry it.
func TestScorecardComponentOmitsPerAgentRows(t *testing.T) {
	e, _ := seedTwoAgents(t)
	out, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, "scorecard_component", json.RawMessage(`{}`))
	if err != nil || f != nil {
		t.Fatalf("f=%v err=%v", f, err)
	}
	jsonTool := scorecardCanon(t, out)
	for _, banned := range []string{"breakdown", `"agents"`, "scout"} {
		if strings.Contains(jsonTool, banned) {
			t.Errorf("the component twin leaks %q: %s", banned, jsonTool)
		}
	}
	// The DOOR carries it (so the omission above has teeth): same store,
	// same call, the person's shape keeps the per-child rows.
	res, err := e.svc.scorecardPlane.Component(e.ctx, protocol.ScorecardComponentParams{ID: e.proj, IncludeChildren: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Breakdown) == 0 {
		t.Fatal("the door's breakdown is empty on this fixture — the comparison above proves nothing")
	}
	if !strings.Contains(scorecardCanon(t, res), "breakdown") {
		t.Fatal("the door answer does not carry the breakdown word — fixture changed shape")
	}
}

// Person sessions never ride the agent's stamped view (MC2/LM7 shape):
// each name refuses with the pointer to the person's OWN door.
func TestPersonSessionRefusedOnMineTool(t *testing.T) {
	e, _ := seedTwoAgents(t)
	for tool, pointer := range map[string]string{
		"scorecard_mine":      "scorecard.agent",
		"scorecard_component": "scorecard.component",
		"digest_learned":      "digest.learned",
	} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "user", ID: "user"}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: err=%v", tool, err)
		}
		if f == nil {
			t.Fatalf("%s: a person was SERVED through the agent's stamped view", tool)
		}
		if !strings.Contains(f.Sentence, pointer) {
			t.Errorf("%s refusal does not point at %s: %s", tool, pointer, f.Sentence)
		}
	}
}

// V13: an unwired plane refuses BY NAME — never a card from nothing.
func TestScorecardArmsRefuseUnwired(t *testing.T) {
	e := newRecordsEnv(t, "")
	for _, tool := range []string{"scorecard_mine", "scorecard_component", "digest_learned"} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "agent", ID: e.agent}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: err=%v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "V13") || !strings.Contains(f.Sentence, tool) {
			t.Errorf("%s: unwired answer is %v — want the by-name V13 refusal naming the tool", tool, f)
		}
	}
}
