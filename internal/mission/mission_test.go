package mission

import (
	"context"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// mission_test.go — S58 T6 (SV11): Evaluate over a hand-built Reader, so
// the unwired and the unknown render as THEMSELVES — `not-wired` and
// `unknown`, never zero, never a score.

type fakeReader struct {
	counts map[string]int
	last   string
	tel    store.ComponentTelemetry
	telErr error
}

func (f fakeReader) LoopStatusCountsForScope([]string) (map[string]int, error) {
	return f.counts, nil
}
func (f fakeReader) LastActivityForScope([]string) (string, error) { return f.last, nil }
func (f fakeReader) ComponentTelemetry(string) (store.ComponentTelemetry, error) {
	return f.tel, f.telErr
}

func oneEntryDeps(f fakeReader) (Deps, []Entry) {
	deps := Deps{
		Store: f,
		NeedsYou: func(context.Context, Entry) (NeedsYou, error) {
			return NeedsYou{Sources: []SourceState{{Name: "questions", State: "reporting"}}}, nil
		},
	}
	return deps, []Entry{{RootID: "cmp-root", Paths: []string{"/w/alpha"}, Name: "Alpha"}}
}

func TestMissionUnwiredGroupsAreNamed(t *testing.T) {
	deps, entries := oneEntryDeps(fakeReader{}) // no telemetry row, no seams
	res, err := Evaluate(context.Background(), deps, entries)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	cs := res.Components[0]
	if cs.Cap.State != "not-wired" {
		t.Errorf("cap = %+v — an unwired BudgetReader must read `not-wired`, not none and not a zeroed cap (SV11)", cs.Cap)
	}
	if cs.NextReady.State != "not-wired" {
		t.Errorf("nextReady = %+v — an unwired S27 seam must read `not-wired`", cs.NextReady)
	}
	if cs.Spend.State != "reporting" || cs.Spend.CostState != "unknown" {
		t.Errorf("spend = %+v — no rollup row is `reporting` with cost `unknown`, never an absent group", cs.Spend)
	}
	if cs.Spend.USDAtLeast != 0 {
		t.Errorf("floor = %v — unknown cost carries a floor of zero, which is true, without claiming exactness", cs.Spend.USDAtLeast)
	}
}

func TestMissionSpendIsAFloor(t *testing.T) {
	fake := fakeReader{tel: store.ComponentTelemetry{Exists: true, ComponentID: "cmp-root"}}
	fake.tel.CostUSDTotal = 12.4
	fake.tel.CostState = "partial"
	deps, entries := oneEntryDeps(fake)
	res, err := Evaluate(context.Background(), deps, entries)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	sp := res.Components[0].Spend
	if sp.State != "reporting" || sp.CostState != "partial" || sp.USDAtLeast != 12.4 {
		t.Fatalf("spend = %+v, want the partial rollup relayed exactly: reporting / partial / 12.4", sp)
	}
	// The type itself holds no field that could present the floor as exact:
	// no `exact`, no upper bound dressed as a total. The NAME of the number
	// carries the claim (P32); this asserts the name is all it carries.
	if sp.CostState == "known" {
		t.Fatal("a partial contributor came out `known` — D9's floor rule broken")
	}
}

func TestMissionWiredSeamsRelayAndNoneIsItsOwnWord(t *testing.T) {
	deps, entries := oneEntryDeps(fakeReader{
		tel: store.ComponentTelemetry{Exists: true},
	})
	// The seams wired: one root capped, one answer of "nothing ready" —
	// `none` must read differently from `not-wired`, and in-force carries
	// the OWNER's words untouched.
	deps.Budget = func(_ context.Context, rootID string) (CapInForce, bool, error) {
		if rootID != "cmp-root" {
			t.Errorf("cap seam got the wrong root %q", rootID)
		}
		return CapInForce{Summary: "at most $50 per calendar month", LimitUSD: 50, Period: "calendar-month"}, true, nil
	}
	deps.NextReady = func(context.Context, string) (NextReadyInfo, bool, error) {
		return NextReadyInfo{}, false, nil
	}
	res, err := Evaluate(context.Background(), deps, entries)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	cs := res.Components[0]
	if cs.Cap.State != "in-force" || cs.Cap.Summary != "at most $50 per calendar month" || cs.Cap.LimitUSD != 50 {
		t.Fatalf("cap relay = %+v", cs.Cap)
	}
	if cs.NextReady.State != "none" {
		t.Fatalf("nextReady = %+v, want `none` — the seam answered, nothing is ready; that is NOT the seam's absence", cs.NextReady)
	}
}
