package store

import "testing"

// S55 T8 (LC13): the epic's budget is the budget row (scope='epic',
// window='lifetime') — the single truth. The old budget_usd column is dead:
// the store's writers no longer name it and its readers derive the view's
// budgetUsd from the row. This test pins both halves: the derivation
// (create/update/remove the budget row, watch the view follow) and the
// column's silence (the epic row never carries a money figure of its own).
func TestEpicRoundTrip(t *testing.T) {
	e := newRecordsEnv(t)
	ep, err := e.s.CreateEpic(NewEpic{ComponentID: e.projectID, Title: "Q4 stability", Objective: "no flakes",
		Owner: "cj", TargetDate: "2026-12-15"})
	if err != nil {
		t.Fatalf("create epic: %v", err)
	}
	// No budget row yet: the view reads no USD cap (nil, not zero — P31).
	got, err := e.s.GetEpic(ep.ID)
	if err != nil || got.Title != "Q4 stability" || got.BudgetUSD != nil || got.TargetDate != "2026-12-15" {
		t.Fatalf("round trip without a budget: %+v %v", got, err)
	}
	// The budget row IS the epic's budget: create it, the view follows.
	// (A user-created budget mints its own id — 'bud-<epic id>' is the T6
	// backfill's shape for MIGRATED budgets only, so the remove below names
	// the returned id, not a guess at it.)
	cap := 900.0
	bud, ref, err := e.s.CreateBudget(BudgetActor{Kind: "user", ID: "1"}, NewBudgetIn{
		Scope: "epic", Subject: ep.ID, Window: "lifetime", USD: &cap,
	})
	if err != nil || ref.Kind != "" {
		t.Fatalf("create the epic's budget: %v %v", ref, err)
	}
	got, err = e.s.GetEpic(ep.ID)
	if err != nil || got.BudgetUSD == nil || *got.BudgetUSD != 900 {
		t.Fatalf("the view did not derive the budget row's cap: %+v %v", got, err)
	}
	list, err := e.s.ListEpics(e.projectID)
	if err != nil || len(list) != 1 || list[0].ID != ep.ID || list[0].BudgetUSD == nil || *list[0].BudgetUSD != 900 {
		t.Fatalf("listing: %+v %v", list, err)
	}
	// Keep-semantics: naming nothing changes nothing; clearing the date is its
	// own intention. An update never touches the cap (budget.set is the
	// cap's door) — the derived figure survives the plan edit.
	up, err := e.s.UpdateEpic(EpicEdit{ID: ep.ID})
	if err != nil || up.Title != "Q4 stability" || up.TargetDate != "2026-12-15" {
		t.Fatalf("no-op edit changed the epic: %+v %v", up, err)
	}
	if up.BudgetUSD == nil || *up.BudgetUSD != 900 {
		t.Fatalf("a plan edit moved the budget: %+v", up)
	}
	up, err = e.s.UpdateEpic(EpicEdit{ID: ep.ID, Title: "Q4 stability v2", ClearTargetDate: true})
	if err != nil || up.Title != "Q4 stability v2" || up.TargetDate != "" {
		t.Fatalf("clear edit: %+v %v", up, err)
	}
	// Removing the budget row removes the cap from the view — one truth,
	// no ghost in the column.
	if err := e.s.RemoveBudget(BudgetActor{Kind: "user", ID: "1"}, bud.ID); err != nil {
		t.Fatalf("remove the epic's budget: %v", err)
	}
	got, err = e.s.GetEpic(ep.ID)
	if err != nil || got.BudgetUSD != nil {
		t.Fatalf("a removed budget still reads as a cap: %+v %v", got, err)
	}
	// The dead column: the epic row never carries a money figure of its
	// own — the tests may read the column directly; production may not
	// (TestNoProductionReadOfEpicBudgetColumn pins the latter).
	var colNull int
	if err := e.s.r.QueryRow(`SELECT count(*) FROM epic WHERE budget_usd IS NOT NULL AND id = ?`, ep.ID).Scan(&colNull); err != nil {
		t.Fatalf("read the dead column: %v", err)
	}
	if colNull != 0 {
		t.Fatal("the epic row was written a budget — the column is dead (LC13)")
	}
	// Unknown project is refused with a sentence at the door.
	if _, err := e.s.CreateEpic(NewEpic{ComponentID: "prj-nope", Title: "x"}); err == nil {
		t.Fatal("an epic filed under a root that does not exist was accepted")
	}
	// No status column exists (the schema test cannot see absence of a
	// future one — this test names the design here).
	if _, err := e.s.w.Exec(`UPDATE epic SET status='done' WHERE id=?`, ep.ID); err == nil {
		t.Fatal("epics grew a status column — OL7 says a future status is a future MIGRATION")
	}
}
