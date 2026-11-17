// retro_test.go — S63 T1: the fact half's contract. Every guard here is the
// spec's table of tests: append-only by trigger (a rewrite is refused even
// through raw SQL), one transaction (a mid-write failure leaves NO outcome),
// idempotent replay (the terminal key dedupes, and the replay answer names
// the row it deduped against), NULL-not-zero round trip (P31 — the pointers
// survive a store cycle unchanged), the tally derived (no stored number
// anywhere), no FK from a fact to loop (archive survives), milestone
// selection honest to the facts, and the fresh-equals-upgraded migration
// proof.

package store

import (
	"strings"
	"testing"
)

// retroFactsMigrationIndex locates this slice's migration in the ordered
// list, so the vintage below is provably the version BEFORE it.
func retroFactsMigrationIndex() int {
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_retro_facts") {
			return i
		}
	}
	panic("the retro_facts migration is missing from Migrations()")
}

func TestRetroFactsMigrationFreshAndUpgraded(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}
	assertRetroCatalogue(t, fresh)

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	all := Migrations()
	applyMigrations(t, old.GormDB(), all[:retroFactsMigrationIndex()])
	var absent int
	if err := old.GormDB().Raw(`SELECT count(*) FROM retro_outcome`).Scan(&absent).Error; err == nil {
		t.Fatal("the vintage store has retro_outcome before its migration — the vintage is wrong")
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
	assertRetroCatalogue(t, s)
}

// assertRetroCatalogue names every object the migration creates in
// sqlite_master — the migration's whole content, counted.
func assertRetroCatalogue(t *testing.T, s *Store) {
	t.Helper()
	g := s.GormDB()
	for _, tbl := range []string{"retro_outcome", "retro_agent", "retro_learning", "retro_template"} {
		if n := countCatalogueEntries(t, g, "table", tbl); n != 1 {
			t.Fatalf("table %s: %d entries, want 1", tbl, n)
		}
	}
	for _, idx := range []string{"idx_retro_outcome_kind", "idx_retro_outcome_blueprint", "idx_retro_outcome_recorded", "idx_retro_agent"} {
		if n := countCatalogueEntries(t, g, "index", idx); n != 1 {
			t.Fatalf("index %s: %d entries, want 1", idx, n)
		}
	}
	for _, trig := range []string{
		"retro_outcome_no_update", "retro_outcome_no_delete",
		"retro_agent_no_update", "retro_agent_no_delete",
		"retro_learning_no_update", "retro_learning_no_delete",
	} {
		if n := countCatalogueEntries(t, g, "trigger", trig); n != 1 {
			t.Fatalf("trigger %s: %d entries, want 1", trig, n)
		}
	}
}

func TestRetroFactsAreAppendOnly(t *testing.T) {
	_, s := testStore(t)
	id, inserted, err := s.RecordRetroOutcome(RetroOutcome{
		ComponentRoot: "root-a", LoopSlug: "loop-1", TerminalKey: "done@t1",
		OutcomeKind: "done", Source: "recorder", Complete: true,
	}, []RetroAgent{{Agent: "golang-engineer"}})
	if err != nil || !inserted {
		t.Fatalf("record: id %d inserted %v err %v", id, inserted, err)
	}
	if _, err := s.LinkRetroLearning(id, 7); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE retro_outcome SET outcome_kind = 'done(rewritten)' WHERE id = ?`, id); err == nil {
		t.Fatal("an UPDATE of an outcome row was accepted — the trigger is missing")
	} else if !strings.Contains(err.Error(), "retro facts are append-only") {
		t.Errorf("UPDATE refused with the wrong sentence: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM retro_outcome WHERE id = ?`, id); err == nil {
		t.Fatal("a DELETE of an outcome row was accepted")
	}
	if _, err := s.w.Exec(`UPDATE retro_agent SET steps = 99 WHERE outcome_id = ?`, id); err == nil {
		t.Fatal("an UPDATE of an agent row was accepted")
	}
	if _, err := s.w.Exec(`DELETE FROM retro_agent WHERE outcome_id = ?`, id); err == nil {
		t.Fatal("a DELETE of an agent row was accepted")
	}
	if _, err := s.w.Exec(`UPDATE retro_learning SET learning_id = 8 WHERE outcome_id = ?`, id); err == nil {
		t.Fatal("an UPDATE of the link row was accepted")
	}
	if _, err := s.w.Exec(`DELETE FROM retro_learning WHERE outcome_id = ?`, id); err == nil {
		t.Fatal("a DELETE of the link row was accepted")
	}
}

func TestRecordRetroOutcomeOneTransaction(t *testing.T) {
	g, s := testStore(t)
	bad := -1
	_, _, err := s.RecordRetroOutcome(RetroOutcome{
		ComponentRoot: "root-a", LoopSlug: "loop-1", TerminalKey: "done@t1",
		OutcomeKind: "done", Source: "recorder", Complete: true,
	}, []RetroAgent{
		{Agent: "golang-engineer"},
		{Agent: "reviewer", Steps: &bad}, // a count is never negative — the CHECK refuses, mid-transaction
	})
	if err == nil {
		t.Fatal("an agent row with a negative count was accepted")
	}
	if n := countTable(t, g, "retro_outcome"); n != 0 {
		t.Fatalf("the failed write left %d outcome rows — the agent rows must NOT half-land", n)
	}
	if n := countTable(t, g, "retro_agent"); n != 0 {
		t.Fatalf("the failed write left %d agent rows", n)
	}
}

func TestRecordRetroOutcomeIdempotentReplay(t *testing.T) {
	g, s := testStore(t)
	o := RetroOutcome{
		ComponentRoot: "root-a", LoopSlug: "loop-1", TerminalKey: "blocked/budget-unreadable@t1",
		OutcomeKind: "blocked/budget-unreadable", Source: "recorder",
	}
	a := []RetroAgent{{Agent: "golang-engineer"}}
	id1, ins1, err := s.RecordRetroOutcome(o, a)
	if err != nil || !ins1 {
		t.Fatalf("first record: %v %v", ins1, err)
	}
	id2, ins2, err := s.RecordRetroOutcome(o, a)
	if err != nil {
		t.Fatalf("replay must not fail: %v", err)
	}
	if ins2 {
		t.Fatal("the replay inserted a second fact")
	}
	if id2 != id1 {
		t.Fatalf("the replay answered id %d, want the pre-existing %d", id2, id1)
	}
	if n := countTable(t, g, "retro_outcome"); n != 1 {
		t.Fatalf("%d outcome rows after a replay, want 1", n)
	}
	if n := countTable(t, g, "retro_agent"); n != 1 {
		t.Fatalf("%d agent rows after a replay, want 1", n)
	}
}

func TestRetroNullIsNotZero(t *testing.T) {
	_, s := testStore(t)
	id, _, err := s.RecordRetroOutcome(RetroOutcome{
		ComponentRoot: "root-a", LoopSlug: "loop-1", TerminalKey: "aborted@t1",
		OutcomeKind: "aborted", Source: "backfill", Complete: false,
		// every measured field left nil on purpose
	}, nil)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	back, ok, err := s.RetroOutcomeByID(id)
	if err != nil || !ok {
		t.Fatalf("read back: %v %v", ok, err)
	}
	// Typed pointers, not `any` boxes: a nil *int inside an interface is
	// != nil, and a test that compared boxes would pass while lying.
	for name, p := range map[string]*int{
		"BlueprintVersion": back.BlueprintVersion, "GoalVersion": back.GoalVersion,
		"CostSteps": back.CostSteps, "CostPartialSteps": back.CostPartialSteps,
		"CostUnknownSteps": back.CostUnknownSteps, "TasksDone": back.TasksDone,
		"TasksFailed": back.TasksFailed, "Retries": back.Retries,
		"ReviewRounds": back.ReviewRounds, "SecurityRounds": back.SecurityRounds,
		"QARounds": back.QARounds, "TotalFailedGates": back.TotalFailedGates,
		"SplitsDone": back.SplitsDone, "Escalations": back.Escalations,
		"UnverifiedSteps": back.UnverifiedSteps, "VerifiedPass": back.VerifiedPass,
		"VerifiedFail": back.VerifiedFail,
	} {
		if p != nil {
			t.Errorf("%s round-tripped as %d — a nil must come back nil, never zero", name, *p)
		}
	}
	for name, p := range map[string]*int64{"WallMS": back.WallMS} {
		if p != nil {
			t.Errorf("%s round-tripped as %d — a nil must come back nil, never zero", name, *p)
		}
	}
	for name, p := range map[string]*float64{"CostUSDFloor": back.CostUSDFloor} {
		if p != nil {
			t.Errorf("%s round-tripped as %v — a nil must come back nil, never zero", name, *p)
		}
	}
	for name, p := range map[string]*bool{"Attended": back.Attended} {
		if p != nil {
			t.Errorf("%s round-tripped as %v — a nil must come back nil, never false", name, *p)
		}
	}
	if back.Complete {
		t.Error("complete=false came back true")
	}
	if back.Source != "backfill" {
		t.Errorf("source = %q, want backfill", back.Source)
	}
	// The row in SQL, not through Go: attended must be NULL, not 0.
	var isNull int
	if err := s.GormDB().Raw(`SELECT attended IS NULL FROM retro_outcome WHERE id = ?`, id).Scan(&isNull).Error; err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if isNull != 1 {
		t.Error("attended landed as 0 — attended must be NULL when unrecorded")
	}
}

func TestRetroTallyIsDerived(t *testing.T) {
	g, s := testStore(t)
	for i, kind := range []string{"done", "done", "blocked/stuck"} {
		if _, _, err := s.RecordRetroOutcome(RetroOutcome{
			ComponentRoot: "root-a", LoopSlug: "loop-x",
			TerminalKey: kind + "@t" + string(rune('0'+i)),
			OutcomeKind: kind, Source: "recorder", Complete: true,
		}, nil); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	n, err := s.RetroTally("root-a", "done")
	if err != nil || n != 2 {
		t.Fatalf("tally done = %d (%v), want 2", n, err)
	}
	var raw int
	if err := g.Raw(`SELECT COUNT(*) FROM retro_outcome WHERE component_root='root-a' AND outcome_kind='done'`).Scan(&raw).Error; err != nil {
		t.Fatalf("raw count: %v", err)
	}
	if n != raw {
		t.Fatalf("RetroTally %d disagrees with the raw SELECT %d", n, raw)
	}
	// And no stored tally table exists anywhere to disagree with.
	var tables int
	if err := g.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE '%tally%'`).Scan(&tables).Error; err != nil {
		t.Fatalf("tally-table census: %v", err)
	}
	if tables != 0 {
		t.Fatalf("%d stored-tally tables exist — RT4 forbids them all", tables)
	}
}

func TestRetroFactsHaveNoFKToLoop(t *testing.T) {
	_, s := testStore(t)
	for tbl, want := range map[string][]string{
		"retro_outcome":  nil,
		"retro_learning": {"retro_outcome"},
		"retro_agent":    {"retro_outcome"},
	} {
		var rows []struct {
			Table string `gorm:"column:table"`
		}
		if err := s.GormDB().Raw(`PRAGMA foreign_key_list(` + tbl + `)`).Scan(&rows).Error; err != nil {
			t.Fatalf("pragma %s: %v", tbl, err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Table)
			if r.Table == "loop" || r.Table == "learning" {
				t.Fatalf("%s has FK to %s — facts outlive both (P3)", tbl, r.Table)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s FKs = %v, want %v", tbl, got, want)
		}
	}
}

func TestUnlinkedMilestoneOutcomes(t *testing.T) {
	_, s := testStore(t)
	// Four done outcomes in root-a (ranks 1..4), one in root-b (its own
	// rank 1). Milestones 1 and 3 select: a1, a3, b1.
	for i := 1; i <= 4; i++ {
		if _, _, err := s.RecordRetroOutcome(RetroOutcome{
			ComponentRoot: "root-a", LoopSlug: "loop-a", TerminalKey: "done@a" + string(rune('0'+i)),
			OutcomeKind: "done", Source: "recorder", Complete: true,
		}, nil); err != nil {
			t.Fatalf("record a%d: %v", i, err)
		}
	}
	if _, _, err := s.RecordRetroOutcome(RetroOutcome{
		ComponentRoot: "root-b", LoopSlug: "loop-b", TerminalKey: "done@b1",
		OutcomeKind: "done", Source: "recorder", Complete: true,
	}, nil); err != nil {
		t.Fatalf("record b1: %v", err)
	}
	got, err := s.UnlinkedMilestoneOutcomes([]int{1, 3, 10})
	if err != nil {
		t.Fatalf("unlinked: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("milestone set = %d rows, want 3 (ranks 1 and 3 of two roots)", len(got))
	}
	if got[0].Outcome.LoopSlug != "loop-a" || got[2].Outcome.LoopSlug != "loop-b" {
		t.Errorf("milestone rows = %s,%s,%s, want a1,a3,b1", got[0].Outcome.LoopSlug, got[1].Outcome.LoopSlug, got[2].Outcome.LoopSlug)
	}
	// Linking the rank-1 row removes it from the set — the link IS the lock.
	if inserted, err := s.LinkRetroLearning(got[0].Outcome.ID, 11); err != nil || !inserted {
		t.Fatalf("link: %v %v", inserted, err)
	}
	if inserted, err := s.LinkRetroLearning(got[0].Outcome.ID, 11); err != nil || inserted {
		t.Fatalf("double link must answer already-linked, got %v %v", inserted, err)
	}
	got, err = s.UnlinkedMilestoneOutcomes([]int{1, 3, 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("after linking: %d rows (%v), want 2", len(got), err)
	}
	// Milestones beyond the tallies cost nothing and fabricate nothing.
	got, err = s.UnlinkedMilestoneOutcomes([]int{100})
	if err != nil || len(got) != 0 {
		t.Fatalf("milestone 100: %d rows (%v), want 0", len(got), err)
	}
}

func TestRetroCoverage(t *testing.T) {
	_, s := testStore(t)
	since, loops, err := s.RetroCoverage()
	if err != nil || since != "" || loops != 0 {
		t.Fatalf("empty coverage = %q/%d (%v), want \"\"/0", since, loops, err)
	}
	for _, rec := range []RetroOutcome{
		{ComponentRoot: "root-a", LoopSlug: "loop-1", TerminalKey: "done@1", OutcomeKind: "done", Source: "recorder", RecordedAt: "2026-11-01T00:00:00.000Z"},
		{ComponentRoot: "root-a", LoopSlug: "loop-2", TerminalKey: "done@2", OutcomeKind: "done", Source: "recorder", RecordedAt: "2026-11-03T00:00:00.000Z"},
		{ComponentRoot: "root-b", LoopSlug: "loop-1", TerminalKey: "done@3", OutcomeKind: "done", Source: "recorder", RecordedAt: "2026-11-02T00:00:00.000Z"},
	} {
		if _, _, err := s.RecordRetroOutcome(rec, nil); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	since, loops, err = s.RetroCoverage()
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if since != "2026-11-01T00:00:00.000Z" || loops != 3 {
		t.Fatalf("coverage = %q/%d, want 2026-11-01T00:00:00.000Z/3", since, loops)
	}
}

func TestRetroTemplatesUpsert(t *testing.T) {
	_, s := testStore(t)
	if _, ok, err := s.GetRetroTemplate("done"); err != nil || ok {
		t.Fatalf("unread template must answer absent: %v %v", ok, err)
	}
	if err := s.UpsertRetroTemplate("done", 1, "Loop {{loop_slug}} finished.", "system"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t1, ok, err := s.GetRetroTemplate("done")
	if err != nil || !ok || t1.Version != 1 || t1.Origin != "system" || t1.CreatedAt == "" {
		t.Fatalf("read back: %+v ok=%v err=%v", t1, ok, err)
	}
	if err := s.UpsertRetroTemplate("done", 2, "Loop {{loop_slug}} finished (v2).", "system"); err != nil {
		t.Fatalf("upsert v2: %v", err)
	}
	t2, _, err := s.GetRetroTemplate("done")
	if err != nil || t2.Version != 2 || !strings.Contains(t2.Template, "v2") {
		t.Fatalf("v2 read back: %+v err=%v", t2, err)
	}
	if t2.CreatedAt != t1.CreatedAt {
		t.Error("an upsert rewrote created_at")
	}
	// A user origin survives the caller passing it — the divergence rule
	// lives in the seed engine above this plain upsert.
	if err := s.UpsertRetroTemplate("done", 3, "mine", "user"); err != nil {
		t.Fatalf("user upsert: %v", err)
	}
	all, err := s.ListRetroTemplates()
	if err != nil || len(all) != 1 || all[0].Origin != "user" {
		t.Fatalf("list: %+v err=%v", all, err)
	}
	if err := s.UpsertRetroTemplate("done", 0, "x", "system"); err == nil {
		t.Error("version 0 was accepted")
	}
	if err := s.UpsertRetroTemplate("done", 1, "x", "machine"); err == nil {
		t.Error("origin machine was accepted")
	}
}

func TestCountEscalationQuestions(t *testing.T) {
	_, s := testStore(t)
	sc := seedLoop(t, s, "esc-loop", "esc-root")
	raw := `INSERT INTO question (loop_slug, component_root, kind, body, options, asked_at)
		VALUES (?,?,?,?,?,'2026-11-04T00:00:00.000Z')`
	for _, q := range [][2]string{
		{"gate", `["continue","abort"]`},    // an escalation (the CommitGate signature)
		{"gate", `["continue","abort"]`},    // a second escalation in the same loop
		{"gate", `["approve","deny"]`},      // a policy gate question — not an escalation
		{"clarify", `["continue","abort"]`}, // wrong kind — not an escalation
	} {
		if _, err := s.w.Exec(raw, sc.LoopSlug, sc.ComponentRoot, q[0], "b", q[1]); err != nil {
			t.Fatalf("seed question: %v", err)
		}
	}
	n, err := s.CountEscalationQuestions(sc)
	if err != nil || n != 2 {
		t.Fatalf("escalation count = %d (%v), want 2", n, err)
	}
	if _, err := s.CountEscalationQuestions(Scope{}); err == nil {
		t.Error("a scopeless count was accepted")
	}
}
