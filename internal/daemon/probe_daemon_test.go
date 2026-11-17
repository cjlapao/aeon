package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// S18 T3 at the daemon: the probe cache is WIRED (the policy-gate argument
// again — nil means silently unwired, and only a booted table proves the
// production claim), and the step path refuses on a cached contradiction
// before its first request is paid for.

func claimingRoute() model.Route {
	r := model.Route{ID: "claiming", Provider: "openai", Model: "llama3.2:1b", MaxOutputTokens: 4096, NoPrice: true}
	r.WireCapabilities = []string{"tools", "streaming-usage", "system-role"}
	return r
}

func contradictedTools() model.ProbeResult {
	return model.ProbeResult{
		Capability: model.CapTools, State: model.StateContradicted,
		Detail:   `the server refused the tool declaration: function tools not supported`,
		ProbedAt: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
		Model:    "llama3.2:1b",
	}
}

// TestProductionDaemonWiresTheProbeCache — `aeon serve` always passes a
// store, so this assertion IS the production claim: routes.list carries
// states, routes.probe saves, and the step gate is live.
func TestProductionDaemonWiresTheProbeCache(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	if d.Table.Probes == nil {
		t.Fatal("the store-backed daemon has NO probe cache wired — routes.probe would answer " +
			"'not saved' forever and routes.list could never show a verified state")
	}
	if d0 := New(Options{}); d0.Table.Probes != nil {
		t.Error("the storeless (test-mode) daemon grew a probe cache; with no store there are no " +
			"earned verdicts, and the handlers' honest no-cache paths are the correct behaviour")
	}
}

// TestStepRefusedByCachedContradiction — the door that protects the loop:
// the Monday probe's contradiction refuses Friday's step run, verbatim, and
// the step never reaches the runtime — no request, no cost, no surprise.
func TestStepRefusedByCachedContradiction(t *testing.T) {
	s := recoveryStore(t)
	route := claimingRoute()
	if err := s.UpsertRoute(route); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults(route.ID, []model.ProbeResult{contradictedTools()}); err != nil {
		t.Fatal(err)
	}

	r := StepRunner{ContradictionCheck: ContradictionCheckerFor(s, nil)}
	res := r.Run(context.Background(), runtime.StepRequest{
		Route:    route,
		Messages: []model.Message{{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: "go"}}}},
	}, runtime.RunOpts{})

	if res.Kind != runtime.ResultComplete || res.Outcome != runtime.OutcomeFailed {
		t.Fatalf("a contradicted step reported %s/%s, want complete/failed — refused, and reported honestly", res.Kind, res.Outcome)
	}
	for _, want := range []string{"refused", "claiming", "tools", "2026-10-20", "llama3.2:1b", "function tools not supported"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("the refusal omits %q (D6: the sentence is the whole point): %s", want, res.Reason)
		}
	}
	if len(res.Messages) != 1 {
		t.Errorf("the refusal dropped the opening transcript; the record of what was asked must survive a refusal")
	}
}

// TestStepRunsOnCleanVerdicts — the gate must not become a wall: a route
// whose flags are verified or merely declared runs.
func TestStepRunsOnCleanVerdicts(t *testing.T) {
	s := recoveryStore(t)
	route := claimingRoute()
	if err := s.UpsertRoute(route); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults(route.ID, []model.ProbeResult{
		{Capability: model.CapTools, State: model.StateVerified, Detail: "accepted",
			ProbedAt: time.Now(), Model: route.Model},
	}); err != nil {
		t.Fatal(err)
	}
	// system-role stays declared (no row): the honest third state runs.
	if got := ContradictionCheckerFor(s, nil)(route); got != "" {
		t.Errorf("a verified tool route with a merely-declared system-role was refused: %s", got)
	}

	// A withdrawn declaration is no longer contradicted either (I5: the
	// declaration leads, the row only evidences).
	route.WireCapabilities = []string{"streaming-usage"}
	if got := ContradictionCheckerFor(s, nil)(route); got != "" {
		t.Errorf("the step refuses for a claim the operator withdrew: %s", got)
	}
}
