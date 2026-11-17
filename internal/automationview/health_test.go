package automationview

import "testing"

func allGood() HealthInputs {
	return HealthInputs{
		BlueprintState: BlueprintOK, WorkspaceExists: true, ScopeExists: true,
		Enabled: true, SecretEnvSet: true, SecretEnvName: "HOOK",
	}
}

func TestComposeHealthTruthTable(t *testing.T) {
	type tc struct {
		name    string
		mutate  func(*HealthInputs)
		canFire bool
	}
	cases := []tc{
		{"all-ok", func(*HealthInputs) {}, true},
		{"blueprint-missing", func(h *HealthInputs) { h.BlueprintState = BlueprintMissing }, false},
		{"blueprint-invalid", func(h *HealthInputs) { h.BlueprintState = BlueprintInvalid }, false},
		{"blueprint-unverifiable", func(h *HealthInputs) { h.BlueprintState = BlueprintUnverifiable }, false},
		{"workspace-gone", func(h *HealthInputs) { h.WorkspaceExists = false }, false},
		{"scope-gone", func(h *HealthInputs) { h.ScopeExists = false }, false},
		{"disabled", func(h *HealthInputs) { h.Enabled = false }, false},
		{"archived", func(h *HealthInputs) { h.Archived = true; h.Enabled = false }, false},
		{"paused", func(h *HealthInputs) { h.Paused = true }, false},
		{"webhook-secret-unset", func(h *HealthInputs) { h.IsWebhook = true; h.SecretEnvSet = false }, false},
		{"webhook-no-secret-name", func(h *HealthInputs) { h.IsWebhook = true; h.SecretEnvName = "" }, false},
		{"secret-ignored-for-schedule", func(h *HealthInputs) { h.SecretEnvSet = false }, true},
		{"warnings-do-not-block", func(h *HealthInputs) { h.BlueprintWarnings = 3 }, true},
	}
	for _, c := range cases {
		in := allGood()
		c.mutate(&in)
		h := ComposeHealth(in)
		if h.CanFire != c.canFire {
			t.Errorf("%s: CanFire = %v, want %v (reasons %v)", c.name, h.CanFire, c.canFire, h.Reasons)
		}
		if c.canFire && len(h.Reasons) != 0 {
			t.Errorf("%s: CanFire but reasons printed: %v", c.name, h.Reasons)
		}
		if !c.canFire && len(h.Reasons) == 0 {
			t.Errorf("%s: cannot fire with no reason printed — AN8 says every no says why", c.name)
		}
	}
}

// TestComposeHealthIsPure is the rule-5 guard in its only honest form: the
// function takes NO clock, NO env, NO paths — every input is a value field,
// and calling it twice with equal inputs answers identically (a read of the
// clock or env inside would eventually diverge or panic under a different
// environment; there is nothing here it could read).
func TestComposeHealthIsPure(t *testing.T) {
	in := allGood()
	a, b := ComposeHealth(in), ComposeHealth(in)
	if a.CanFire != b.CanFire || len(a.Checks) != len(b.Checks) {
		t.Fatal("two identical calls disagreed")
	}
	for i := range a.Checks {
		if a.Checks[i] != b.Checks[i] {
			t.Fatalf("check %d differs: %+v vs %+v", i, a.Checks[i], b.Checks[i])
		}
	}
	// Order is stable and named: blueprint first, workspace+scope next,
	// switches last (the UI renders rows in this order).
	if a.Checks[0].Name != "blueprint" || a.Checks[len(a.Checks)-1].Name != "switches" {
		t.Fatalf("check order changed: %+v", a.Checks)
	}
}

func TestComposeHealthUnverifiableNamesItself(t *testing.T) {
	in := allGood()
	in.BlueprintState = "not-a-state" // any unrecognised state reads unverifiable, never ok
	h := ComposeHealth(in)
	if h.CanFire || h.Checks[0].State != BlueprintUnverifiable {
		t.Errorf("an unknown blueprint state must answer unverifiable: %+v", h.Checks[0])
	}
}
