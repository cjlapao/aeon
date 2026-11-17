package tools

import "testing"

// S55 T10 — the seven loop-control / budget / watchdog tools are REGISTERED
// (TestRegistryExactNames counts them) and never GRANTED by default: a
// handler that wants one lists it explicitly, and DefaultGrant() is the pin
// that says the default is still read, list, write. Named TestS55ToolsNotIn
// DefaultGrant (2026-11-02 merge): main's S45/S56 tools carry their own
// TestNewToolsNotInDefaultGrant in toolset_test.go — the two pins measure
// different families and stand side by side.
func TestS55ToolsNotInDefaultGrant(t *testing.T) {
	seven := []string{
		"loop_control_state", "loop_request_stop",
		"budget_list", "budget_show", "budget_effective", "budget_spend",
		"watchdog_list",
	}
	granted := map[string]bool{}
	for _, g := range DefaultGrant() {
		granted[g] = true
	}
	for _, name := range seven {
		if granted[name] {
			t.Errorf("%s is in DefaultGrant() — the S55 T10 tools are registered, never granted; a handler that wants one lists it (OD10)", name)
		}
	}
}
