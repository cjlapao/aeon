package seed

import (
	"bytes"
	"encoding/json"
	"testing"
)

// oneRecordSet is a set with one record at the given version and content.
func oneRecordSet(id string, key string, version int, content string) Set {
	return Set{
		ID:      id,
		Class:   ClassInert,
		Version: version,
		Records: func() []Record {
			return []Record{{Key: key, Version: version, Content: []byte(content)}}
		},
	}
}

// TestPlanTable is every row of the decision table (T1), asserting Op,
// State and the reason text. A row that is wrong is a named failure.
func TestPlanTable(t *testing.T) {
	cases := []struct {
		name   string
		set    Set
		ledger LedgerState
		live   LiveState
		// want is the single per-record decision; for the retire case the
		// decision is for a ledger key not in the set.
		wantOp    Op
		wantState State
		wantReason string
		// wantRefusal asserts a plan-level refusal (the downgrade) instead.
		wantRefusal bool
	}{
		{
			name: "none absent apply",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{State: StateAbsent},
			live:   LiveState{Present: false},
			wantOp: OpApply, wantState: StateApplied, wantReason: "apply",
		},
		{
			name: "none present adopt",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{State: StateAbsent},
			live:   LiveState{Present: true, Hash: "h", Adoptable: true},
			wantOp: OpAdopt, wantState: StateApplied, wantReason: "adopt",
		},
		{
			name: "none present conflict",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{State: StateAbsent},
			live:   LiveState{Present: true, Hash: "h", Adoptable: false},
			wantOp: OpSkip, wantReason: "conflict",
		},
		{
			name: "applied present equal higher update",
			set:  oneRecordSet("s", "a", 2, `{"x":2}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateApplied},
			live:   LiveState{Present: true, Hash: "H"},
			wantOp: OpUpdate, wantState: StateApplied, wantReason: "update",
		},
		{
			name: "applied present equal up to date",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateApplied},
			live:   LiveState{Present: true, Hash: "H"},
			wantOp: OpSkip, wantState: StateApplied, wantReason: "up to date",
		},
		{
			name: "applied present equal lower downgrade",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 2, Hash: "H", State: StateApplied},
			live:   LiveState{Present: true, Hash: "H"},
			wantRefusal: true,
		},
		{
			name: "applied present diverged",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H1", State: StateApplied},
			live:   LiveState{Present: true, Hash: "H2"},
			wantOp: OpSkip, wantState: StateDiverged, wantReason: "diverged",
		},
		{
			name: "applied absent removed",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateApplied},
			live:   LiveState{Present: false},
			wantOp: OpSkip, wantState: StateRemoved, wantReason: "removed",
		},
		{
			name: "removed skip",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateRemoved},
			live:   LiveState{Present: true, Hash: "H"},
			wantOp: OpSkip, wantState: StateRemoved, wantReason: "removed",
		},
		{
			name: "diverged skip",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateDiverged},
			live:   LiveState{Present: true, Hash: "H2"},
			wantOp: OpSkip, wantState: StateDiverged, wantReason: "diverged",
		},
		{
			name: "failed retry",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateFailed},
			live:   LiveState{Present: false},
			wantOp: OpApply, wantReason: "retry",
		},
		{
			name: "retired reapplied",
			set:  oneRecordSet("s", "a", 1, `{"x":1}`),
			ledger: LedgerState{Version: 1, Hash: "H", State: StateRetired},
			live:   LiveState{Present: true, Hash: "H"},
			wantOp: OpApply, wantReason: "re-adopted",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Plan(c.set, map[string]LedgerState{"a": c.ledger}, map[string]LiveState{"a": c.live})
			if c.wantRefusal {
				if res.Refusal.Key == "" {
					t.Fatalf("expected a plan-level refusal (downgrade), got none; decisions=%+v", res.Decisions)
				}
				if res.Refusal.Reason != "a lower version than applied — a downgrade is never silent" {
					t.Fatalf("refusal reason = %q", res.Refusal.Reason)
				}
				return
			}
			if res.Refusal.Key != "" {
				t.Fatalf("unexpected refusal %q", res.Refusal.Reason)
			}
			if len(res.Decisions) != 1 {
				t.Fatalf("expected 1 decision, got %d: %+v", len(res.Decisions), res.Decisions)
			}
			d := res.Decisions[0]
			if d.Op != c.wantOp {
				t.Errorf("op = %q, want %q", d.Op, c.wantOp)
			}
			if c.wantState != "" && d.State != c.wantState {
				t.Errorf("state = %q, want %q", d.State, c.wantState)
			}
			if c.wantReason != "" && !contains(d.Reason, c.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", d.Reason, c.wantReason)
			}
		})
	}
}

// TestPlanDowngradeIsARefusal is the one case that fails the whole apply:
// a lower version than applied is a Refusal, not a skip (SD6).
func TestPlanDowngradeIsARefusal(t *testing.T) {
	s := oneRecordSet("s", "a", 1, `{"x":1}`)
	res := Plan(s, map[string]LedgerState{"a": {Version: 5, Hash: "H", State: StateApplied}},
		map[string]LiveState{"a": {Present: true, Hash: "H"}})
	if res.Refusal.Key != "a" {
		t.Fatalf("a downgrade did not produce a plan-level refusal: %+v", res)
	}
	if len(res.Decisions) != 0 {
		t.Fatalf("a downgrade must apply no decision, got %d", len(res.Decisions))
	}
}

// TestPlanNeverOverwritesDivergedOrRemoved asserts a diverged or removed
// record is skipped even when a higher version exists (SD6).
func TestPlanNeverOverwritesDivergedOrRemoved(t *testing.T) {
	t.Run("diverged with a higher code version", func(t *testing.T) {
		s := oneRecordSet("s", "a", 9, `{"x":9}`)
		res := Plan(s, map[string]LedgerState{"a": {Version: 1, Hash: "H1", State: StateApplied}},
			map[string]LiveState{"a": {Present: true, Hash: "H2"}})
		if res.Decisions[0].Op != OpSkip || res.Decisions[0].State != StateDiverged {
			t.Fatalf("a diverged record was not skipped: %+v", res.Decisions)
		}
	})
	t.Run("removed with a higher code version", func(t *testing.T) {
		s := oneRecordSet("s", "a", 9, `{"x":9}`)
		res := Plan(s, map[string]LedgerState{"a": {Version: 1, Hash: "H", State: StateRemoved}},
			map[string]LiveState{"a": {Present: false}})
		if res.Decisions[0].Op != OpSkip || res.Decisions[0].State != StateRemoved {
			t.Fatalf("a removed record was not skipped: %+v", res.Decisions)
		}
	})
}

// TestPlanRetiresMissingRecords asserts a ledger row whose record left the
// code is retired, not deleted (the row is kept).
func TestPlanRetiresMissingRecords(t *testing.T) {
	s := oneRecordSet("s", "b", 1, `{"x":1}`) // "a" left the code
	res := Plan(s, map[string]LedgerState{"a": {Version: 1, Hash: "H", State: StateApplied}, "b": {Version: 1, Hash: "Hb", State: StateApplied}},
		map[string]LiveState{"a": {Present: true, Hash: "H"}, "b": {Present: true, Hash: "Hb"}})
	var retire *Decision
	for i := range res.Decisions {
		if res.Decisions[i].Key == "a" {
			retire = &res.Decisions[i]
		}
	}
	if retire == nil {
		t.Fatalf("expected a retire decision for the missing record 'a'")
	}
	if retire.Op != OpRetire || retire.State != StateRetired {
		t.Fatalf("a missing record was not retired: %+v", *retire)
	}
}

// TestPlanIsDeterministicAndSorted asserts shuffled input yields identical,
// sorted-by-key output.
func TestPlanIsDeterministicAndSorted(t *testing.T) {
	s := Set{
		ID: "s", Class: ClassInert, Version: 1,
		Records: func() []Record {
			return []Record{
				{Key: "c", Version: 1, Content: []byte(`{"x":1}`)},
				{Key: "a", Version: 1, Content: []byte(`{"x":1}`)},
				{Key: "b", Version: 1, Content: []byte(`{"x":1}`)},
			}
		},
	}
	ledgers := map[string]LedgerState{"c": {Version: 1, Hash: "H", State: StateApplied}, "a": {Version: 1, Hash: "H", State: StateApplied}, "b": {Version: 1, Hash: "H", State: StateApplied}}
	live := map[string]LiveState{"c": {Present: true, Hash: "H"}, "a": {Present: true, Hash: "H"}, "b": {Present: true, Hash: "H"}}
	// The records are in a fixed order in the set, but the maps are not
	// ordered; the output must be sorted by key regardless.
	got := Plan(s, ledgers, live)
	var keys []string
	for _, d := range got.Decisions {
		keys = append(keys, d.Key)
	}
	if !sortedKeys(keys) {
		t.Fatalf("decisions are not sorted by key: %v", keys)
	}
	// A second run with the same inputs is byte-identical.
	again := Plan(s, ledgers, live)
	if !sameDecisions(got, again) {
		t.Fatalf("two runs of the same inputs differ")
	}
}

// TestPlanIsPure asserts no clock or I/O: the same inputs over 1000 runs
// give the same output.
func TestPlanIsPure(t *testing.T) {
	s := oneRecordSet("s", "a", 1, `{"x":1}`)
	ledgers := map[string]LedgerState{"a": {Version: 1, Hash: "H", State: StateApplied}}
	live := map[string]LiveState{"a": {Present: true, Hash: "H"}}
	first := Plan(s, ledgers, live)
	for i := 0; i < 1000; i++ {
		if !sameDecisions(first, Plan(s, ledgers, live)) {
			t.Fatalf("Plan is not pure: run %d differs", i)
		}
	}
}

// --- helpers ---

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func sortedKeys(k []string) bool {
	for i := 1; i < len(k); i++ {
		if k[i-1] > k[i] {
			return false
		}
	}
	return true
}
func sameDecisions(a, b PlanResult) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}
