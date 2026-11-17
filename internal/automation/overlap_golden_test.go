package automation

// S27 T1 — the overlap truth table golden. S20 ruled (Q2) which loop
// statuses OWN the one-at-a-time slot; the ruling lives twice — once in
// Go (trigger.StillGoing) and once in SQL (store.OverlappingEventFire's
// IN-list) — and S27's scheduler will ask the same question of a
// candidate. This golden drives BOTH sides over the same six rows and
// byte-records that they agree (testdata/continuous_golden/still_going.json,
// S27_GOLDEN_UPDATE=1).
//
// WHO MAY CHANGE THIS AND HOW:
//   - T2 (a `scheduler` word in provenance) and T5 (the scheduler's own
//     readiness gate) may extend the WORDS around this rule; they may not
//     move the six rows: `idle` releasing the slot (S39's rest) is already
//     law, and any seventh status must arrive here — truth table first —
//     before either side of the pair learns it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

func overlapCheck(t *testing.T, name string, got any) {
	t.Helper()
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("testdata", "continuous_golden", name+".json")
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
		t.Errorf("golden %s drifted.\n--- got ---\n%s\n--- want ---\n%s", name, raw, want)
	}
}

func TestStillGoingTruthTable(t *testing.T) {
	dir := t.TempDir()
	clock := func() time.Time { return time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC) }
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const root = "/r/overlap"
	if _, err := s.CreateComponent(store.NewComponent{Name: "ov", Slug: "ov", Root: root}); err != nil {
		t.Fatal(err)
	}
	const bp = "id: dev-loop\nschemaVersion: 1\nversion: 1\nlanes: []\n"
	type row struct {
		Status   string `json:"status"`
		GoSide   bool   `json:"goSide"`  // trigger.StillGoing — the Go ruling
		SQLFound bool   `json:"sqlFound"` // store.OverlappingEventFire — the SQL copy
	}
	var gold []row
	for i, status := range []string{"running", "waiting", "blocked", "done", "aborted", "idle"} {
		slug := "ov-" + status
		l, err := s.CreatePinnedLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Root: root,
			Goal: "overlap row", Status: "running"}, "dev-loop", 1, bp, "{}")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: root}, status); err != nil {
			t.Fatalf("status %s: %v", status, err)
		}
		if _, _, err := s.RecordEventFire(
			store.NewEventFire{TriggerID: "trS27", OutboxID: 7000 + int64(i), SubjectKind: "goal", SubjectID: "sub-" + status},
			store.EventFireDecision{Outcome: store.FireStarted, LoopID: l.ID, LoopSlug: slug}, clock()); err != nil {
			t.Fatal(err)
		}
		_, _, found, err := s.OverlappingEventFire("trS27", "goal", "sub-"+status)
		if err != nil {
			t.Fatal(err)
		}
		gold = append(gold, row{Status: status, GoSide: trigger.StillGoing(status), SQLFound: found})
	}
	// The unknown status: the Go side answers false for anything the set
	// does not name; the SQL side cannot even hold such a row (the loop
	// status CHECK refuses it), which is recorded here in prose, not bytes.
	if trigger.StillGoing("suspended") {
		t.Error("StillGoing(unknown)=true — the rule is a closed set")
	}
	overlapCheck(t, "still_going", gold)
	for _, r := range gold {
		if r.GoSide != r.SQLFound {
			t.Errorf("status %q: Go says %v, SQL says %v — the two copies of S20's ruling have diverged", r.Status, r.GoSide, r.SQLFound)
		}
	}
}
