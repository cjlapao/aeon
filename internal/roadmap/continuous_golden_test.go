package roadmap

// S27 T1 — the D4 definition of `ready`, recorded as bytes. S27's
// scheduler will call readiness per tick (T3's dependency edges arrive
// beside a re-decision of what ready MEANS); this golden is the
// before-picture: THREE conditions, no fourth, and the refusal sentence
// that says WHICH is missing.
//
// Deviation from T1's file list, recorded in the task Notes: the task
// parked this table in internal/store, but `roadmap` imports `store` —
// the store test importing back would be a cycle. The table belongs with
// the function it tests anyway.
//
// WHO MAY CHANGE THIS AND HOW:
//   - T3 ADDS dependency satisfaction to the SCHEDULER's readiness gate;
//     IsReady itself (the door's D4 re-check) stays byte-identical unless
//     the proposal says so — testdata/continuous_golden/is_ready.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsReadyGoldenTable(t *testing.T) {
	type readyCase struct {
		Name     string `json:"name"`
		Brief    string `json:"brief"`
		Plan     string `json:"plan"`
		Priority string `json:"priority"`
		Ready    bool   `json:"ready"`
		Why      string `json:"why"`
	}
	cases := []Item{
		{BriefPath: "/b.md", PlanPath: "/p.md", Priority: PriorityKnown},
		{BriefPath: "", PlanPath: "/p.md", Priority: PriorityKnown},
		{BriefPath: "/b.md", PlanPath: "", Priority: PriorityKnown},
		{BriefPath: "/b.md", PlanPath: "/p.md", Priority: PriorityUnknown},
		{BriefPath: "", PlanPath: "", Priority: PriorityUnknown},
	}
	names := []string{"all_three", "no_brief", "no_plan", "priority_unknown", "nothing_at_all"}
	var gold []readyCase
	for i, it := range cases {
		ready, why := IsReady(it)
		gold = append(gold, readyCase{Name: names[i], Brief: it.BriefPath, Plan: it.PlanPath,
			Priority: string(it.Priority), Ready: ready, Why: why})
	}
	raw, err := json.MarshalIndent(gold, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("testdata", "continuous_golden", "is_ready.json")
	if os.Getenv("S27_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (%v) — generate with S27_GOLDEN_UPDATE=1 on the PRE-change code", path, err)
	}
	if string(want) != string(raw) {
		t.Errorf("golden %s drifted.\n--- got ---\n%s\n--- want ---\n%s", path, raw, want)
	}
}
