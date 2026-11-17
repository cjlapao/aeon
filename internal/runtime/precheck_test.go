package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T4: the pre-call capability check. The tests here are mostly
// ABSENCES — no request, no event, no checkpoint — because the whole value
// of this gate is what does NOT happen when it fires (M29, C6).

// declaringRoute returns the runtime's test route with an honest wire-flag
// declaration — the difference between "the operator said no" and "nobody
// ever asked" is what this file is about.
func declaringRoute(caps ...string) model.Route {
	r := testRoute()
	r.ID = "local-notools"
	// APPEND TO A NON-NIL EMPTY: `declaringRoute()` must produce [] — the
	// operator DECLARING none — not nil, which would be nobody ever having
	// said anything. The distinction is the whole subject of this file.
	list := append([]string{}, caps...)
	r.WireCapabilities = list
	return r
}

func tool() model.ToolSpec { return toolSpec("bash") }

func TestMissingCapabilityRefusesBeforeTheCall(t *testing.T) {
	// The route has declared it serves usage and nothing else. A step
	// offering `bash` needs `tools` it does not have.
	req := testRequest(tool())
	req.Route = declaringRoute("streaming-usage")
	res := RunStep(context.Background(), req, runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))

	if res.Outcome != OutcomeFailed || res.Kind != ResultComplete {
		t.Fatalf("a step needing tools on a no-tools route reported %s/%s, want complete/failed", res.Kind, res.Outcome)
	}
	if !strings.Contains(res.Reason, "step requires capability 'tools'") {
		t.Errorf("the refusal does not name the missing capability: %q", res.Reason)
	}
}

func TestClientSpyIsAtZero(t *testing.T) {
	// D8/C6: the claim "no model call was made" is asserted on the SPY, not
	// on the error's own words. A refusal that still paid for the request
	// would print the same sentence and be a lie.
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	req := testRequest(tool())
	req.Route = declaringRoute("streaming-usage")
	RunStep(context.Background(), req, runOpts(client, nil, nil))

	if n := client.requestCount(); n != 0 {
		t.Fatalf("%d model requests behind a refusal that says none were made", n)
	}
}

func TestCheckIsInTheRuntimeNotTheAdapter(t *testing.T) {
	// The grep form of I3: an adapter refusing a request it cannot serve is
	// too late — by then the step was compiled and its context resolved. The
	// check must be reachable purely in the runtime, and no adapter may
	// perform it.
	entries, err := os.ReadDir(filepath.Join("..", "model", "adapters"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("..", "model", "adapters", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		for _, forbidden := range []string{"CapabilityRefusal", "does not have it (no model call was made)", "Required(req"} {
			if strings.Contains(src, forbidden) {
				t.Errorf("adapters/%s performs the pre-call check (%q) — it must run before compilation reaches a provider (I3)", e.Name(), forbidden)
			}
		}
	}
	// And it is IN the runtime: RunStep's first act is the check.
	run, err := os.ReadFile("runstep.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(run), "Check(req.Route, req)") {
		t.Error("RunStep does not call Check — the gate has drifted out of the runtime")
	}
}

func TestRequiredDerivesFromTheToolset(t *testing.T) {
	// A non-empty toolset needs tools; an empty one does not need a thing.
	// Reasoning on the STEP needs thinking; effort inherited from the route
	// is the route owner's configuration and needs nothing from the step.
	cases := []struct {
		name   string
		tools  []model.ToolSpec
		effort string
		want   []model.Capability
	}{
		{"no tools no effort", nil, "", nil},
		{"tools", []model.ToolSpec{tool()}, "", []model.Capability{model.CapTools}},
		{"effort only", nil, "high", []model.Capability{model.CapThinking}},
		{"both, in required's order", []model.ToolSpec{tool()}, "low",
			[]model.Capability{model.CapTools, model.CapThinking}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testRequest(tc.tools...)
			req.ReasoningEffort = tc.effort
			got := Required(req)
			if len(got) != len(tc.want) {
				t.Fatalf("Required = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Required[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRefusalNamesBothSides(t *testing.T) {
	err := Check(declaringRoute("streaming-usage"), testRequest(tool()))
	if err == nil {
		t.Fatal("no refusal at all")
	}
	sentence := err.Error()
	for _, want := range []string{"refused", "'tools'", "'local-notools'", "does not have it", "(no model call was made)"} {
		if !strings.Contains(sentence, want) {
			t.Errorf("the refusal omits %q — %s is not actionable: %q", want, want, sentence)
		}
	}
}

func TestRefusalIsADomainError(t *testing.T) {
	// DB19: the caller must tell this apart from a network failure without
	// string-matching. The type is the discriminator.
	err := Check(declaringRoute(), testRequest(tool()))
	var refusal *CapabilityRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("the refusal is not a *CapabilityRefusal: %T", err)
	}
	if refusal.Capability != model.CapTools || refusal.RouteID != "local-notools" {
		t.Errorf("the structured refusal lost a side: %+v", refusal)
	}

	// And a transport failure is NOT one — the two must not blur.
	transport := errors.New("dial tcp: connection refused")
	if errors.As(transport, &refusal) {
		t.Error("a transport failure matches as a capability refusal — the caller could not tell them apart")
	}
}

func TestDistinctFromContradiction(t *testing.T) {
	// Two failures, two messages, OPPOSITE fixes: this one says change the
	// route; the probe's says the server contradicted the claim. Asserted
	// unequal, with neither's key phrase present in the other.
	mine := Check(declaringRoute(), testRequest(tool())).Error()
	theirs := model.ContradictionSentence("local-notools", string(model.CapTools),
		time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), "some-llm", "the server refused the tool declaration")
	if mine == theirs {
		t.Fatal("the two refusals are the same sentence")
	}
	if strings.Contains(mine, "probe contradicted") || strings.Contains(theirs, "does not have it") {
		t.Errorf("the sentences share the other's key phrase — one will be read as the other:\n  %s\n  %s", mine, theirs)
	}
	if !strings.Contains(mine, "no model call was made") {
		t.Error("this refusal alone can honestly promise nothing was sent; the probe's cannot, and neither may blur that")
	}
}

func TestCheckRunsBeforeContextResolution(t *testing.T) {
	// Ordered absences: a step with a LARGE declared context is refused with
	// no checkpoint written, no event emitted and no request sent. Today the
	// opening checkpoint is the first thing that persists resolved context;
	// after S22 it will be a vector query behind it — a check that ran after
	// either would be paying for work it throws away.
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	cp := &spyCheckpoint{}
	events := &spySink{}

	req := testRequest(tool())
	req.Route = declaringRoute("streaming-usage")
	req.Manifest.Files = make([]FileHash, 64) // a fat declared context
	for i := range req.Manifest.Files {
		req.Manifest.Files[i] = FileHash{Path: "big/context.go", Hash: "x", Bytes: 4096}
	}

	res := RunStep(context.Background(), req, RunOpts{
		Client: client, Checkpoint: cp, Sink: events,
		HandoffSpec: DefaultHandoffSpec(), StatusField: "status",
	})
	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome %s, want failed", res.Outcome)
	}
	if n := cp.count(); n != 0 {
		t.Errorf("%d checkpoints written — the step persisted context it was refused for", n)
	}
	if n := events.count(); n != 0 {
		t.Errorf("%d events emitted — a refused step announced itself (no `step.started` may exist for a step that never ran)", n)
	}
	if n := client.requestCount(); n != 0 {
		t.Errorf("%d model requests", n)
	}
}

// --- the two directions the gate must NOT go ---------------------------------

func TestUndeclaredRouteIsNotRefused(t *testing.T) {
	// nil WireCapabilities is not the operator saying "no" — it is nobody
	// ever having asked. Refusing on it would strand every pre-S18 route
	// that runs tools perfectly well. (I5's rule, read the other way: the
	// absence of a declaration is not a declaration of absence.)
	req := testRequest(tool())
	r := testRoute() // undeclared
	req.Route = r
	if err := Check(r, req); err != nil {
		t.Errorf("an undeclared route was refused: %v", err)
	}
	res := RunStep(context.Background(), req, runOpts(newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))
	if res.Outcome == OutcomeFailed && strings.Contains(res.Reason, "requires capability") {
		t.Errorf("RunStep refused an undeclared route: %s", res.Reason)
	}
}

func TestDeclaredSufficientRouteRuns(t *testing.T) {
	// The gate must not become a wall: everything needed, honestly declared
	// — the step runs and the client is reached.
	req := testRequest(tool())
	req.Route = declaringRoute("tools", "streaming-usage")
	client := newFakeClient(scriptedTurn{Text: okHandoff})
	res := RunStep(context.Background(), req, runOpts(client, nil, nil))
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s (reason %q) — a route that declared what the step needs must run", res.Outcome, res.Reason)
	}
	if n := client.requestCount(); n != 1 {
		t.Errorf("%d requests, want the step to have actually run", n)
	}
}

// --- spies --------------------------------------------------------------------

type spyCheckpoint struct {
	mu    sync.Mutex
	saves int
}

func (s *spyCheckpoint) Save(context.Context, Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	return nil
}

func (s *spyCheckpoint) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

type spySink struct {
	mu    sync.Mutex
	kinds []string
}

func (s *spySink) Emit(StepEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kinds = append(s.kinds, "event")
}

func (s *spySink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.kinds)
}
