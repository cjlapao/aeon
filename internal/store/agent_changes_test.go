// agent_changes_test.go — S73 T5 (AR6, PG1–PG5): the ledger's window, over a
// real store, with the rows written through the doors that mint them.
//
// # Why these rows are laid by the doors and not by raw SQL
//
// agent_page_test.go is the opposite case, and says so: it tests a READ of the
// agent table, so it seeds that table directly. Here the thing under test is a
// window over records that only the write doors know how to make — an
// `AgentChange` written by hand would prove the SELECT and nothing else, while
// a page over door-minted rows also proves that what T3 writes is what T5 can
// find. That is the task's own sentence: the two tasks meet in a test, not in a
// hope.
//
// # The four properties, and where their twins live
//
// Filtered count before the window, the clamped echo, the empty-but-not-error
// shape, a filter that cannot see past itself. Those are the four T2 pinned for
// `agent.rows`, re-pinned here on a second table, because a convention that has
// held once is an anecdote. What is NEW here is the ledger's own shape: one
// order with no sort offered, and the one row whose slug is NULL.

package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/cjlapao/aeon/internal/paging"
)

// changesPage is the call every test below makes, normalised through the bounds
// the ledger publishes — the same reason `pageAgent` exists (agent_page_test.go):
// a test that invented its own bounds would be testing a fiction.
func changesPage(t *testing.T, s *Store, slug string, req paging.Request) ([]AgentChange, int) {
	t.Helper()
	normalised, err := req.Normalise(AgentChangeBounds)
	if err != nil {
		t.Fatalf("normalise %#v: %v", req, err)
	}
	rows, total, err := s.AgentChangesPage(context.Background(), slug, normalised)
	if err != nil {
		t.Fatalf("agent changes %#v: %v", normalised, err)
	}
	return rows, total
}

// changeLabels names each row by the three things a reader of a history looks
// at, so a failure prints the history instead of printing indexes.
func changeLabels(rows []AgentChange) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		slug := "—"
		if r.Slug != nil {
			slug = *r.Slug
		}
		out = append(out, r.Op+"/"+slug+" by "+r.ActorKind+":"+r.ActorID)
	}
	return out
}

// seedOneAgent lays one system row through the seed plane's WRITER — the
// function T4's applier calls — on a transaction of this store's own, stamped
// with the actor the applier passes. That is the honest shape of "a ledger row
// written by T3's seed path": the applier's own package test (T4) proves it
// stamps; this test proves the row it stamps is findable through T5's window.
func seedOneAgent(t *testing.T, s *Store, slug string) {
	t.Helper()
	c := AgentContent{
		Slug: slug, Name: "Seed " + slug, Description: "seeded for the window test",
		BodyHash: "sha256:" + slug, Origin: "system",
	}
	err := s.AgentWithTx(context.Background(), func(tx *sql.Tx) error {
		return s.SeedAgentInsert(context.Background(), tx, c, seedTestActor, agentChangeAt)
	})
	if err != nil {
		t.Fatalf("seed %s: %v", slug, err)
	}
}

// TestAgentChangesAnswersNewestFirstAcrossThreeActors is the meeting of T3 and
// T5: three writes, three kinds of actor, one window, and the order a person
// reading a history expects. The third write is the seed plane's own, so the
// task's "the two tasks meet in a test" bullet is proved by the row arriving
// through the door T3 ships, not by a hand-made one.
func TestAgentChangesAnswersNewestFirstAcrossThreeActors(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.CreateAgentAudited(ctx, userAgentActor, Agent{Slug: "window-a", Name: "Window A", Version: "1"}, agentChangeAt); err != nil {
		t.Fatalf("create: %v", err)
	}
	row, found, err := s.AgentBySlug("window-a")
	if err != nil || !found {
		t.Fatalf("read back the row to edit it: found=%v err=%v", found, err)
	}
	if _, err := s.UpdateAgentAudited(ctx, cliActor, Agent{ID: row.ID, Slug: "window-a", Name: "Window A", Version: "2"}, agentChangeAt); err != nil {
		t.Fatalf("edit: %v", err)
	}
	seedOneAgent(t, s, "window-seed")

	rows, total := changesPage(t, s, "", paging.Request{})
	if total != 3 || len(rows) != 3 {
		t.Fatalf("total=%d len=%d, want 3 and 3: %v", total, len(rows), changeLabels(rows))
	}
	want := []string{
		"seed/window-seed by system:seed-engine",
		"edit/window-a by cli:cli",
		"create/window-a by user:1000",
	}
	got := changeLabels(rows)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is %q, want %q (the whole window: %v)", i, got[i], want[i], got)
		}
	}
	// The id order is what makes the window stable; OCCURRED_AT is the same
	// string in every row here, so a test that only checked the timestamps
	// would pass on an unordered SELECT.
	for i := 1; i < len(rows); i++ {
		if rows[i-1].ID <= rows[i].ID {
			t.Fatalf("rows %d and %d are not descending by id (%d, %d)", i-1, i, rows[i-1].ID, rows[i].ID)
		}
	}
	// T3's seed stamp, read back through T5's door with the names the applier
	// passes — AR4's path 9 and AR6 in one assertion.
	if rows[0].ActorKind != AgentActorSystem || rows[0].ActorID != "seed-engine" {
		t.Fatalf("the seed plane's row says %s:%s", rows[0].ActorKind, rows[0].ActorID)
	}
}

// TestASlugFilterCannotSeeAnotherAgentsRows is the filter's whole job: narrowing.
// Two agents, three rows between them; the window for one never mentions the
// other, and the TOTAL narrows with the window (PG3) — a count of the whole
// ledger under a filtered table is the number that disagrees with the screen.
func TestASlugFilterCannotSeeAnotherAgentsRows(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for _, slug := range []string{"filter-mine", "filter-yours"} {
		if err := s.CreateAgentAudited(ctx, userAgentActor, Agent{Slug: slug, Name: slug, Version: "1"}, agentChangeAt); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
	}
	if err := s.DeleteAgentAudited(ctx, userAgentActor, "filter-yours", agentChangeAt); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rows, total := changesPage(t, s, "filter-mine", paging.Request{})
	if total != 1 || len(rows) != 1 {
		t.Fatalf("filtered total=%d len=%d, want 1 and 1: %v", total, len(rows), changeLabels(rows))
	}
	if rows[0].Slug == nil || *rows[0].Slug != "filter-mine" {
		t.Fatalf("the filtered window holds %v", changeLabels(rows))
	}
	// And the unfiltered count still says three, so the filter narrowed a
	// number rather than replacing one.
	if _, total := changesPage(t, s, "", paging.Request{}); total != 3 {
		t.Fatalf("the unfiltered total is %d, want 3", total)
	}
}

// TestAgentChangesPastTheEndAnswersAnEmptyWindowAndTheHonestTotal is PG2 and PG3
// together: page 9 of a three-row ledger is a real question with a real answer,
// not an error — and the answer carries the total that tells the browser how
// many pages there are.
func TestAgentChangesPastTheEndAnswersAnEmptyWindowAndTheHonestTotal(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for _, n := range []string{"past-1", "past-2", "past-3"} {
		if err := s.CreateAgentAudited(ctx, userAgentActor, Agent{Slug: n, Name: n, Version: "1"}, agentChangeAt); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	rows, total := changesPage(t, s, "", paging.Request{Page: 9, PageSize: 10})
	if total != 3 {
		t.Fatalf("a past-the-end page lost the total: %d, want 3", total)
	}
	if rows == nil {
		t.Fatal("a past-the-end page returned a nil slice; the wire would say null")
	}
	if len(rows) != 0 {
		t.Fatalf("a past-the-end page holds %d rows: %v", len(rows), changeLabels(rows))
	}
	// The clamp, at this table's own bounds: 100 is the cap, and the page the
	// window answers is the clamped one.
	rows, _ = changesPage(t, s, "", paging.Request{Page: 1, PageSize: 5_000})
	if len(rows) != 3 {
		t.Fatalf("a clamped page holds %d rows, want the whole ledger's 3", len(rows))
	}
}

// TestAnEmptyLedgerIsNotAnError states the nothing case, which for a NEW install
// is the common one: the History tab opens before anything has happened.
func TestAnEmptyLedgerIsNotAnError(t *testing.T) {
	s := openTemp(t)
	rows, total := changesPage(t, s, "", paging.Request{})
	if total != 0 {
		t.Fatalf("a fresh store's ledger counts %d", total)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("a fresh store answers %#v, want an empty slice", rows)
	}
	// A slug nobody has ever written is the same answer, not a 404.
	rows, total = changesPage(t, s, "a-name-nobody-typed", paging.Request{})
	if total != 0 || rows == nil || len(rows) != 0 {
		t.Fatalf("an unknown slug says total=%d rows=%#v", total, rows)
	}
}

// TestTheMigrateRowIsVisibleOnlyUnfiltered is AR1's one NULL slug walking
// through AR6's read: the back-fill's row belongs to no agent, so it appears in
// the whole-ledger window and in nobody's history — and a query narrowed to a
// slug does not quietly include it.
func TestTheMigrateRowIsVisibleOnlyUnfiltered(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	plantPluginAgents(t, s, "Alpha Migrate")
	created, err := s.MigrateFirstPartyAgentsAudited(ctx, userAgentActor, agentChangeAt)
	if err != nil || created != 1 {
		t.Fatalf("migrate: created=%d err=%v", created, err)
	}

	rows, total := changesPage(t, s, "", paging.Request{})
	if total != 1 || len(rows) != 1 {
		t.Fatalf("the unfiltered window is total=%d rows=%v", total, changeLabels(rows))
	}
	if rows[0].Op != AgentOpMigrate || rows[0].Slug != nil {
		t.Fatalf("the run's row reads op=%q slug=%v, want migrate with a null slug", rows[0].Op, rows[0].Slug)
	}
	// The derived agent exists and has its OWN history — which is empty, because
	// the run minted one row for itself and nothing per agent (AR4).
	derived := SlugPtr("alpha-migrate")
	rows, total = changesPage(t, s, *derived, paging.Request{})
	if total != 0 || len(rows) != 0 {
		t.Fatalf("the derived agent's history holds %d rows (%v); the run was supposed to mint one row for itself", len(rows), changeLabels(rows))
	}
}

// TestTheLedgerOffersNoSortAndRefusesEveryWordItCouldBeAskedFor is AR6's
// asymmetry, proved at the bounds rather than at the door: a history has ONE
// order, so the bounds hold NO sort vocabulary — which means the one bridge from
// a wire word to a SQL word, paging.Request.Column, cannot answer even the
// convention's own default name. A plane that cannot resolve a sort must not
// accept one, and this is where that becomes checkable.
func TestTheLedgerOffersNoSortAndRefusesEveryWordItCouldBeAskedFor(t *testing.T) {
	if len(AgentChangeBounds.Sorts) != 0 {
		t.Fatalf("the ledger publishes %d sorts; it has one order and must publish none", len(AgentChangeBounds.Sorts))
	}
	for _, name := range []string{"", "name", "id", "occurredAt", "summary", "created_at"} {
		req := paging.Request{Sort: name}
		if _, _, err := req.Column(AgentChangeBounds); err == nil {
			t.Errorf("Column accepted sort %q on a read that offers no sorts", name)
		}
	}
	// The direction is still refused at Normalise, so a caller sending dir
	// learns the word is not theirs even though nothing here will use it.
	if _, err := (paging.Request{Dir: "newest"}).Normalise(AgentChangeBounds); err == nil {
		t.Error("Normalise accepted dir \"newest\"")
	}
	if _, err := (paging.Request{Dir: "desc"}).Normalise(AgentChangeBounds); err != nil {
		t.Errorf("Normalise refused a legal dir: %v", err)
	}
	// And the request survives a window call anyway — the store ignores the
	// words the DOOR is responsible for refusing, which is exactly why the door
	// refuses them by name (T5's step 3) instead of trusting this function.
	s := openTemp(t)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "sort-word", Name: "Sort Word", Version: "1"}, agentChangeAt); err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, total := changesPage(t, s, "", paging.Request{Sort: "anything", Dir: "desc", Q: "ignored"})
	if total != 1 || len(rows) != 1 || rows[0].Op != AgentOpCreate {
		t.Fatalf("the store's answer to an unspeakable sort is %v / total %d", changeLabels(rows), total)
	}
}
