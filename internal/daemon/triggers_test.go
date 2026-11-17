package daemon

// triggers_test.go — the reader over a REAL store: rows become views, the
// last fire rides along, and the counts carry all five outcomes always
// (P31). The store is the vocabulary's owner; the reader test checks the
// counts against it so the protocol's copy cannot drift silently.

import (
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func readerStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedTriggerWorld(t *testing.T, s *store.Store) (string, string) {
	t.Helper()
	p, err := s.CreateComponent(store.NewComponent{Name: "tr group", Slug: "tr-group", Root: "/repo/tr-group"})
	if err != nil || !p.OK() {
		t.Fatal(err)
	}
	c, err := s.CreateComponent(store.NewComponent{Name: "alpha", Slug: "alpha",
		Root: t.TempDir(), ParentID: p.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	return p.Component.ID, c.Component.ID
}

func mkTrigger(t *testing.T, s *store.Store, projectID, compID string, name string) store.Trigger {
	t.Helper()
	tr, err := s.CreateEventTrigger(store.Actor{Kind: "user", ID: "operator"}, store.NewEventTrigger{
		Name: name, RootID: projectID, ComponentID: compID,
		Match:     store.Match{Event: "goal.approved", GoalType: []string{"feature"}},
		Blueprint: "dev-loop", MaxRunsPerHour: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// mkFire claims and decides — the two steps every real answer takes.
func mkFire(t *testing.T, s *store.Store, triggerID string, outbox int64, outcome, reason, note string, at time.Time) {
	t.Helper()
	f, fresh, err := s.ClaimEventFire(store.NewEventFire{TriggerID: triggerID, OutboxID: outbox,
		SubjectKind: "goal", SubjectID: "g1"}, at)
	if err != nil || !fresh {
		t.Fatalf("claim: fresh=%v err=%v", fresh, err)
	}
	if err := s.DecideEventFire(f.ID, store.EventFireDecision{Outcome: outcome, Reason: reason,
		LoopID: "l-" + triggerID, LoopSlug: "auto-" + triggerID, CostNote: note}, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestTheReaderTurnsTriggersAndFiresIntoViews(t *testing.T) {
	s := readerStore(t)
	projectID, compID := seedTriggerWorld(t, s)
	tr := mkTrigger(t, s, projectID, compID, "intake")
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	mkFire(t, s, tr.ID, 4, store.FireStarted, "", "", base)
	mkFire(t, s, tr.ID, 5, store.FireSkipped, "overlap: loop auto-x is still running", "", base.Add(time.Minute))
	mkFire(t, s, tr.ID, 6, store.FireLimited, "max-runs-per-hour: 10 started, limit 10", "cost-unknown", base.Add(2*time.Minute))

	r := NewTriggerReads(s)
	vs, err := r.ListEventTriggers(protocol.EventTriggerListParams{})
	if err != nil || len(vs) != 1 {
		t.Fatalf("views n=%d err=%v", len(vs), err)
	}
	v := vs[0]
	if v.ID != tr.ID || v.Name != "intake" || !v.Enabled || v.MaxRunsPerHour != 10 {
		t.Fatalf("view %+v — the trigger's own fields must arrive", v)
	}
	if string(v.Match) != `{"event":"goal.approved","goal_type":["feature"]}` {
		t.Errorf("match %s — the canonical object arrives verbatim", v.Match)
	}
	if v.LastFire == nil || v.LastFire.Outcome != store.FireLimited || v.LastFire.CostNote != "cost-unknown" {
		t.Fatalf("lastFire %+v — the newest recorded answer rides along (id DESC)", v.LastFire)
	}

	fs, counts, err := r.ListEventFires(protocol.EventTriggerFiresParams{TriggerID: tr.ID, Limit: 50})
	if err != nil || len(fs) != 3 {
		t.Fatalf("fires n=%d err=%v", len(fs), err)
	}
	// The five-key guarantee is the store's vocabulary — checked against
	// the owner, not a second literal list.
	for _, o := range store.EventFireOutcomes() {
		if _, ok := counts[o]; !ok {
			t.Errorf("counts lacks %q — all five always, zero included", o)
		}
	}
	if counts[store.FireStarted] != 1 || counts[store.FireLimited] != 1 || counts[store.FireSkipped] != 1 ||
		counts[store.FireRefused] != 0 || counts[store.FireCouldNotSay] != 0 {
		t.Errorf("counts %v — 1|0|1|0|1 was the truth", counts)
	}

	v2, found, err := r.ShowTrigger(tr.ID)
	if err != nil || !found || v2.LastFire == nil {
		t.Fatalf("show found=%v err=%v last=%+v", found, err, v2.LastFire)
	}
	if _, found, err := r.ShowTrigger("tr-nope"); err != nil || found {
		t.Fatalf("ghost trigger found=%v err=%v — false and no error is the honest answer", found, err)
	}
}

func TestTheReaderAnswersACountForAllTriggersWhenNoIDIsGiven(t *testing.T) {
	s := readerStore(t)
	projectID, compID := seedTriggerWorld(t, s)
	tr1 := mkTrigger(t, s, projectID, compID, "one")
	tr2 := mkTrigger(t, s, projectID, compID, "two")
	base := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	mkFire(t, s, tr1.ID, 1, store.FireStarted, "", "", base)
	mkFire(t, s, tr2.ID, 2, store.FireStarted, "", "", base)
	mkFire(t, s, tr2.ID, 3, store.FireRefused, "depth-exceeded: event depth 4 > 3", "", base)

	r := NewTriggerReads(s)
	// Per-trigger first, to prove the sums are sums and not leakage.
	_, c1, err := r.ListEventFires(protocol.EventTriggerFiresParams{TriggerID: tr1.ID})
	if err != nil || c1[store.FireStarted] != 1 || c1[store.FireRefused] != 0 {
		t.Fatalf("tr1 counts %v err=%v", c1, err)
	}
	fs, all, err := r.ListEventFires(protocol.EventTriggerFiresParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 3 {
		t.Errorf("%d fire rows for all-triggers — want every row", len(fs))
	}
	if all[store.FireStarted] != 2 || all[store.FireRefused] != 1 {
		t.Fatalf("all-triggers counts %v — want the sums 2|1", all)
	}
}
