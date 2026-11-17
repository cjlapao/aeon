package blueprint

import (
	"strings"
	"testing"
	"time"
)

// lint_test.go — S48 T2 rule 4: lint is ADVISORY. It never errors, never
// gates, never touches a Report; the state table decides; the reason rides
// verbatim; the clock is the injected instant rendered RFC3339 UTC.

type fakeTools struct {
	m map[string][2]string // "agent\x00tool" → state, reason
}

func (f fakeTools) State(agent, tool string) (string, string, bool) {
	v, ok := f.m[agent+"\x00"+tool]
	return v[0], v[1], ok
}

func lintBP() *Blueprint {
	return &Blueprint{
		ID: "linted", SchemaVersion: 1, Version: 1,
		Lanes: []Lane{{ID: "file", Step: &Step{
			Kind: ModelStep, Agent: "planner",
			Emits:    []Emit{{Type: "goal"}, {Type: "brief", Attach: Paths{"out.md"}}, {Type: "banana"}},
			Produces: Paths{"out.md"},
		}}},
	}
}

func TestLintStateTable(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.FixedZone("EST", -5*3600))
	for state, want := range map[string]int{"allowed": 0, "ask": 0, "granted_denied": 3, "granted_no_rule": 3, "not_granted": 3} {
		f := fakeTools{m: map[string][2]string{}}
		// EVERY tool the linted blueprint implies answers with `state`
		// (goal_create, brief_create, attach — three tools, so three
		// advisories when the state is advisory).
		for _, tool := range []string{"goal_create", "brief_create", "attach"} {
			f.m["planner\x00"+tool] = [2]string{state, "the daemon's own sentence · " + state}
		}
		adv := Lint(lintBP(), f, at)
		if len(adv) != want {
			t.Fatalf("state %s: %d advisories, want %d", state, len(adv), want)
		}
		if len(adv) == 0 {
			continue
		}
		for _, a := range adv {
			if a.Reason != "the daemon's own sentence · "+state {
				t.Fatalf("reason not verbatim: %q", a.Reason)
			}
			if a.State != state || a.Agent != "planner" || a.Lane != "file" || a.Kind != "grant" {
				t.Fatalf("advisory shape: %+v", a)
			}
			if a.EvaluatedAt != "2026-11-02T17:00:00Z" {
				t.Fatalf("EvaluatedAt %q — not the injected instant rendered RFC3339 UTC", a.EvaluatedAt)
			}
			if a.Tool != "goal_create" && a.Tool != "brief_create" && a.Tool != "attach" {
				t.Fatalf("unexpected tool %q", a.Tool)
			}
		}
	}
	// Unknown answers (ok=false) say nothing — the third state is silence,
	// not a guess.
	if adv := Lint(lintBP(), fakeTools{}, at); len(adv) != 0 {
		t.Fatalf("unknown grants produced %d advisories", len(adv))
	}
	// nil ToolState: nothing, no panic.
	if adv := Lint(lintBP(), nil, at); adv != nil {
		t.Fatalf("nil ToolState gave %+v", adv)
	}
	// An emit type outside the table (banana) asks nothing — Analyze's line.
	f := fakeTools{m: map[string][2]string{}}
	f.m["planner\x00banana"] = [2]string{"not_granted", "should never be asked"}
	if adv := Lint(lintBP(), f, at); len(adv) != 0 {
		t.Fatalf("banana queried the catalog: %+v", adv)
	}
}

func TestLintNeverTouchesReport(t *testing.T) {
	raw := []byte("id: linted\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: file\n    step:\n      kind: model-step\n      agent: planner\n      produces: out.md\n      emits:\n        - type: goal\n")
	before := Analyze(raw, ValidationOptions{Mode: "standard"}).Diagnostics
	f := fakeTools{m: map[string][2]string{"planner\x00goal_create": {"not_granted", "no rule grants it"}}}
	for i := 0; i < 3; i++ {
		_ = Lint(mustLoad(t, raw), f, time.Unix(0, 0).UTC())
	}
	after := Analyze(raw, ValidationOptions{Mode: "standard"}).Diagnostics
	if len(before) != len(after) {
		t.Fatalf("lint changed the report: %d → %d diagnostics", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("diagnostic %d changed: %+v → %+v", i, before[i], after[i])
		}
	}
	// And a fully-advised blueprint still validates as before: lint output
	// never enters a Report.
	rep := Analyze(raw, ValidationOptions{Mode: "standard"})
	for _, d := range rep.Diagnostics {
		if strings.Contains(d.Kind, "lint") || strings.Contains(d.Msg, "not_granted") {
			t.Fatalf("lint leaked into the report: %+v", d)
		}
	}
}

func mustLoad(t *testing.T, raw []byte) *Blueprint {
	t.Helper()
	b, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &b
}
