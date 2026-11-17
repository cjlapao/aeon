// tighten_floor_test.go — the tightening table (spec test 13) and the
// autonomy floor table (spec test 14), asserted as tables: every field,
// both directions, plus the reflection pin that keeps the table
// field-complete.

package mergerule

import (
	"reflect"
	"testing"
)

func withRule(mut func(*Rule)) Rule {
	r := Baseline()
	r.ComponentID = "c"
	mut(&r)
	return r
}

func TestTightensTable(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Rule)
		wantOK    bool
		wantLoose []string
	}{
		// identity: the same rule tightens itself (neutral).
		{"identical", func(r *Rule) {}, true, nil},

		// the switches.
		{"enabled-off-loosens", func(r *Rule) { r.Enabled = false }, false, []string{"Enabled"}}, // baseline is on
		{"require-local-on", func(r *Rule) { r.RequireLocal = true }, true, nil},
		{"require-local-off", func(r *Rule) { r.RequireLocal = false }, true, nil}, // baseline is off: on tightens, off is neutral
		{"threads-on", func(r *Rule) { r.RequireThreadsResolved = true }, true, nil},

		// the enums.
		{"dod-all-met-tightens", func(r *Rule) { r.RequireDoD = RequireDoDAllMet }, true, nil},
		{"dod-none-is-neutral", func(r *Rule) {}, true, nil}, // baseline is none
		{"no-checks-refuse-tightens", func(r *Rule) { r.NoChecks = NoChecksRefuse }, true, nil},
		{"policy-required-only-loosens", func(r *Rule) { r.ChecksPolicy = ChecksRequired }, false, []string{"ChecksPolicy"}},

		// the lists.
		{"checks-add-tightens", func(r *Rule) { r.RequiredChecks = []string{"build"} }, true, nil},
		{"checks-empty-is-neutral", func(r *Rule) {}, true, nil},
		{"agents-fewer-tightens", func(r *Rule) { r.AeonReviewAgents = []string{} }, true, nil},
		{"agents-more-loosens", func(r *Rule) { r.AeonReviewAgents = []string{"code-reviewer", "second"} }, false, []string{"AeonReviewAgents"}},

		// the mode: the order and the incomparable pair.
		{"mode-both-tightens", func(r *Rule) { r.ReviewMode = ReviewModeBoth }, true, nil},
		{"mode-aeon-incomparable", func(r *Rule) { r.ReviewMode = ReviewModeAeon }, false, nil},
		{"mode-either-loosens", func(r *Rule) { r.ReviewMode = ReviewModeEither }, false, []string{"ReviewMode"}},

		// the neutral.
		{"method-neutral", func(r *Rule) { r.Method = MethodRebase }, true, nil},

		// compound: one tighten + one loosen = not a tightening, named.
		{"compound-named", func(r *Rule) {
			r.RequireLocal = true
			r.ChecksPolicy = ChecksRequired
		}, false, []string{"ChecksPolicy"}},
	}
	for _, c := range cases {
		current := withRule(func(*Rule) {})
		proposed := withRule(c.mutate)
		ok, loosened := Tightens(current, proposed)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v, want %v (loosened=%v)", c.name, ok, c.wantOK, loosened)
			continue
		}
		if !reflect.DeepEqual(loosened, c.wantLoose) {
			t.Errorf("%s: loosened=%v, want %v", c.name, loosened, c.wantLoose)
		}
	}

	// The drop case needs a CURRENT with names: tightening is relative.
	cur := withRule(func(r *Rule) { r.RequiredChecks = []string{"build", "test"} })
	// a superset tightens
	ok, _ := Tightens(cur, withRule(func(r *Rule) { r.RequiredChecks = []string{"build", "test", "lint"} }))
	if !ok {
		t.Error("a check superset is not a tightening")
	}
	// a drop loosens, named
	ok, loose := Tightens(cur, withRule(func(r *Rule) { r.RequiredChecks = []string{"build"} }))
	if ok || !reflect.DeepEqual(loose, []string{"RequiredChecks"}) {
		t.Errorf("a check drop: ok=%v loose=%v, want false/[RequiredChecks]", ok, loose)
	}
	// an equality is neutral
	ok, _ = Tightens(cur, withRule(func(r *Rule) { r.RequiredChecks = []string{"build", "test"} }))
	if !ok {
		t.Error("an equal check list is not neutral")
	}
	// a swap (neither subset nor superset) is fail-closed
	ok, loose = Tightens(cur, withRule(func(r *Rule) { r.RequiredChecks = []string{"build", "lint"} }))
	if ok || !reflect.DeepEqual(loose, []string{"RequiredChecks"}) {
		t.Errorf("a check swap: ok=%v loose=%v, want false/[RequiredChecks]", ok, loose)
	}

	// The mode's full order, from both directions.
	mode := func(m string) Rule { return withRule(func(r *Rule) { r.ReviewMode = m }) }
	for _, c := range []struct {
		cur, prop string
		wantOK    bool
		wantLoose []string
	}{
		{ReviewModeForge, ReviewModeBoth, true, nil},
		{ReviewModeAeon, ReviewModeBoth, true, nil},
		{ReviewModeBoth, ReviewModeForge, false, []string{"ReviewMode"}},
		{ReviewModeBoth, ReviewModeAeon, false, []string{"ReviewMode"}},
		{ReviewModeForge, ReviewModeEither, false, []string{"ReviewMode"}},
		{ReviewModeAeon, ReviewModeEither, false, []string{"ReviewMode"}},
		{ReviewModeEither, ReviewModeForge, true, nil},
		{ReviewModeEither, ReviewModeAeon, true, nil},
		{ReviewModeEither, ReviewModeBoth, true, nil},
		{ReviewModeForge, ReviewModeAeon, false, nil}, // incomparable, both directions
		{ReviewModeAeon, ReviewModeForge, false, nil},
	} {
		ok, loose := Tightens(mode(c.cur), mode(c.prop))
		if ok != c.wantOK || !reflect.DeepEqual(loose, c.wantLoose) {
			t.Errorf("mode %s→%s: ok=%v loose=%v, want ok=%v loose=%v", c.cur, c.prop, ok, loose, c.wantOK, c.wantLoose)
		}
	}
}

// The table is field-complete: every field of Rule is classified. A new
// field that someone forgot to classify lands here (and fail-closes in
// Tightens), so the table and the struct cannot drift apart silently.
func TestTightensIsFieldComplete(t *testing.T) {
	tt := reflect.TypeOf(Rule{})
	for i := 0; i < tt.NumField(); i++ {
		name := tt.Field(i).Name
		if _, found := classifiers[name]; !found {
			t.Errorf("Rule.%s is not classified in tighten.go — the table is no longer field-complete", name)
		}
	}
	// And the table holds no phantom: every entry names a real field.
	for name := range classifiers {
		if _, found := tt.FieldByName(name); !found {
			t.Errorf("classifiers holds %q, which Rule has no field for", name)
		}
	}
}

// --- the autonomy floor (spec test 14) ----------------------------------------

// The floor's table: each ingredient absent (and present). The rule's
// full-autonomy shape is the only green.
func TestAutonomyFloorTable(t *testing.T) {
	gates := EffectiveGates{
		ComponentID: "c",
		Gates: []EffectiveGate{
			{ID: "g1", Name: "unit", Requirement: "required", Enabled: true},
			{ID: "g2", Name: "lint", Requirement: "advisory", Enabled: true},
		},
	}
	full := func() Rule {
		r := Baseline()
		r.ComponentID = "c"
		r.Enabled = true
		r.RequireLocal = true
		r.RequireDoD = RequireDoDAllMet
		r.NoChecks = NoChecksRefuse
		r.ReviewMode = ReviewModeAeon
		return r
	}

	// (a) the full rule over a gate set with a required gate: safe.
	ok, missing := AutonomyFloor(full(), gates)
	if !ok || len(missing) != 0 {
		t.Fatalf("the full rule is not safe: missing=%v", missing)
	}
	// ... and over a set whose only gate is advisory: the required
	// ingredient is absent (an advisory gate runs, but nothing demands
	// it — the floor counts REQUIRED gates, VG12).
	advOnly := EffectiveGates{ComponentID: "c",
		Gates: []EffectiveGate{{ID: "g2", Name: "lint", Requirement: "advisory", Enabled: true}}}
	ok, missing = AutonomyFloor(full(), advOnly)
	if ok || len(missing) != 1 || missing[0].Code != FloorNoRequiredGates {
		t.Fatalf("an advisory-only set: ok=%v missing=%v, want no-required-gates", ok, missing)
	}
	// ... and over an EMPTY set: the same ingredient (no gate at all).
	ok, missing = AutonomyFloor(full(), EffectiveGates{ComponentID: "c"})
	if ok || missing[0].Code != FloorNoRequiredGates {
		t.Fatalf("an empty set: ok=%v missing=%v", ok, missing)
	}

	// (b) each ingredient absent, one at a time, named.
	absent := []struct {
		name   string
		mutate func(*Rule)
		want   string
	}{
		{"disabled", func(r *Rule) { r.Enabled = false }, FloorDisabled},
		{"local-not-required", func(r *Rule) { r.RequireLocal = false }, FloorLocalNotRequired},
		{"dod-not-required", func(r *Rule) { r.RequireDoD = RequireDoDNone }, FloorDoDNotRequired},
		{"no-checks-allow", func(r *Rule) { r.NoChecks = NoChecksAllow }, FloorChecksNotBound},
		{"refuse-but-unbound", func(r *Rule) { r.RequiredChecks = nil; r.ChecksPolicy = ChecksRequired }, FloorChecksNotBound},
		{"forge-only", func(r *Rule) { r.ReviewMode = ReviewModeForge }, FloorForgeOnly},
	}
	for _, c := range absent {
		r := full()
		c.mutate(&r)
		ok, missing := AutonomyFloor(r, gates)
		if ok {
			t.Errorf("%s: the floor passed with %s absent", c.name, c.want)
			continue
		}
		found := false
		for _, m := range missing {
			if m.Code == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing list %v does not name %s", c.name, missing, c.want)
		}
	}

	// (c) the modes that admit autonomy: aeon, either, both.
	for _, m := range []string{ReviewModeAeon, ReviewModeEither, ReviewModeBoth} {
		r := full()
		r.ReviewMode = m
		ok, missing := AutonomyFloor(r, gates)
		if !ok || len(missing) != 0 {
			t.Errorf("mode %s: ok=%v missing=%v, want safe", m, ok, missing)
		}
	}

	// (d) two absent at once: both named (the list is a table, not a
	// headline).
	r := full()
	r.RequireLocal = false
	r.ReviewMode = ReviewModeForge
	ok, missing = AutonomyFloor(r, gates)
	if ok || len(missing) != 2 {
		t.Fatalf("two absent: ok=%v missing=%v, want both named", ok, missing)
	}
}
