package store

// mergerule_test.go — S57 T5's migration drill (P9: M2 fresh vs upgraded,
// identical), the merge-rule audit discipline (append-only, one event per
// changing write, revisions never reset), the EffectiveMergeRule resolver
// (own | root | baseline) and the write doors (VG23's shape).

import (
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/mergerule"
)

const s57MergeRuleMigration = "20261102064454_merge_rules"

// s57M2Vintage splits the full migration set at S57's M2: before is
// everything that stands before it (the vintage a store would upgrade
// FROM, M1 included), m2 the single new file in apply order.
func s57M2Vintage(t *testing.T) (before, m2 []Migration) {
	t.Helper()
	for _, m := range Migrations() {
		if m.Name == s57MergeRuleMigration {
			m2 = append(m2, m)
			continue
		}
		before = append(before, m)
	}
	if len(m2) != 1 {
		t.Fatalf("the S57 M2 migration set = %d entries, want 1", len(m2))
	}
	return before, m2
}

// TestMigrationM2FreshAndUpgradedIdentical — the T5 name: a clean file
// walked to the head and a seeded store upgraded over M2 carry the SAME
// schema (sqlite_master, DDL text included); the upgrade preserved every
// pre-existing row byte for byte; the five new tables are present and
// empty; the ledger gained exactly one row; an idempotent re-open changes
// nothing.
func TestMigrationM2FreshAndUpgradedIdentical(t *testing.T) {
	// Fresh.
	fresh := openMigrated(t)
	freshDump := schemaDump(t, fresh)

	// Upgrade: the M1 vintage already applied and populated, then M2.
	path := t.TempDir() + "/state.db"
	g, closeDB := rawGorm(t, path)
	defer closeDB()
	before, m2 := s57M2Vintage(t)
	applyMigrations(t, g, before)
	seedVerifyVintage(t, g)
	// And a verify gate, so the M1 tables hold data the upgrade must keep.
	if err := g.Exec(`INSERT INTO verify_command (id,component_id,name,command,requirement,timeout_s,position,enabled,origin,created_at,updated_at)
		VALUES ('vgate','vroot','build','go build ./...','required',300,0,1,'user','t','t')`).Error; err != nil {
		t.Fatalf("seed the pre-M2 gate: %v", err)
	}
	pre, _ := allFingerprints(t, g)
	preLedger := ledgerCount(t, g)

	applyMigrations(t, g, m2)

	// Every pre-existing table keeps its row count AND its row checksum.
	for table, was := range pre {
		if table == "_migrations" {
			continue
		}
		n, sum := tableFingerprint(t, g, table)
		if n != was.count {
			t.Errorf("table %s: row count %d -> %d", table, was.count, n)
		}
		if sum != was.sum {
			t.Errorf("table %s: row checksum changed on the upgrade", table)
		}
	}
	// The five new tables exist with their indexes and triggers, and the
	// upgrade adds shape, never data.
	for _, table := range []string{"merge_rule", "merge_rule_event", "dod_item", "dod_event", "merge_event"} {
		if n, _ := tableFingerprint(t, g, table); n != 0 {
			t.Errorf("table %s has rows after a data-free upgrade", table)
		}
	}
	for _, index := range []string{
		"uq_merge_rule_event_revision", "idx_merge_rule_event_component",
		"idx_dod_item_scope", "idx_dod_event_item", "idx_merge_event_scope",
	} {
		if countCatalogueEntries(t, g, "index", index) != 1 {
			t.Errorf("index %s is absent", index)
		}
	}
	for _, trigger := range []string{
		"merge_rule_event_no_update", "merge_rule_event_no_delete",
		"dod_event_no_update", "dod_event_no_delete",
	} {
		if countCatalogueEntries(t, g, "trigger", trigger) != 1 {
			t.Errorf("trigger %s is absent", trigger)
		}
	}
	// The end guard: the FK check ran inside the migration and found
	// nothing — the guard table carries the proof (n CHECK n = 0).
	if countCatalogueEntries(t, g, "table", "zz_s57_merge_rules_fk_guard") != 1 {
		t.Fatalf("the end fk guard table is absent")
	}
	var guard int
	if err := g.Raw(`SELECT n FROM zz_s57_merge_rules_fk_guard`).Scan(&guard).Error; err != nil {
		t.Fatalf("read the fk guard: %v", err)
	}
	if guard != 0 {
		t.Errorf("the fk guard recorded %d dangling references, want 0", guard)
	}
	// The ledger gained exactly the one new migration.
	if got := ledgerCount(t, g); got != preLedger+1 {
		t.Errorf("ledger rows %d -> %d, want +1", preLedger, got)
	}

	// Fresh and upgraded are IDENTICAL schemas.
	if upgradedDump := schemaDump(t, g); upgradedDump != freshDump {
		t.Errorf("the upgraded schema drifted from the fresh one:\n--- fresh ---\n%s\n--- upgraded ---\n%s", freshDump, upgradedDump)
	}

	// Idempotent re-open: the runner skips the already-applied migrations
	// and the data comes back intact.
	closeDB()
	g2, closeDB2 := rawGorm(t, path)
	defer closeDB2()
	applyMigrations(t, g2, Migrations())
	if got := ledgerCount(t, g2); got != preLedger+1 {
		t.Errorf("re-open applied migrations again: ledger %d -> %d", preLedger+1, got)
	}
	if n := countTable(t, g2, "merge_rule"); n != 0 {
		t.Errorf("merge_rule rows after re-open = %d, want 0", n)
	}
	if n := countTable(t, g2, "verify_command"); n != 1 {
		t.Errorf("verify_command rows after re-open = %d, want 1", n)
	}
}

// TestMergeRuleEventsAreAppendOnlyAndRevisionsNeverReset — the P3 half of
// the audit: the history table refuses UPDATE and DELETE at the trigger
// layer (the sentence names the table), a refused write writes no event,
// a no-op set writes no event, and the revisions run 1,2,3,4 WITHOUT
// resetting after the clear (a cleared rule's next set continues the
// sequence — P3's number, the UNIQUE(component_id, revision) index the
// backstop).
func TestMergeRuleEventsAreAppendOnlyAndRevisionsNeverReset(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "mr-root")
	user := s57User
	agent := Actor{Kind: "agent", ID: "a1"}

	// 1: set (the first — before NULL, after set).
	r1 := mergerule.Baseline()
	r1.ComponentID = "mr-root"
	r1.RequireLocal = true
	w, err := s.SetMergeRule(user, r1)
	if err != nil || !w.OK() {
		t.Fatalf("set 1: %v %v", w, err)
	}
	evs, err := s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(evs) != 1 || evs[0].Revision != 1 || evs[0].Op != "set" {
		t.Fatalf("after set 1: %d events, want one rev-1 set", len(evs))
	}
	if evs[0].BeforeJSON != nil || evs[0].AfterJSON == nil {
		t.Errorf("first set: before must be NULL and after set")
	}
	if evs[0].At != w.Rule.UpdatedAt {
		t.Errorf("event at %s != the row's updated_at %s — not the same clock reading", evs[0].At, w.Rule.UpdatedAt)
	}

	// 2: set a different value (both snapshots set).
	r2 := w.Rule
	r2.RequireDoD = "all_met"
	if w, err := s.SetMergeRule(user, r2); err != nil || !w.OK() {
		t.Fatalf("set 2: %v %v", w, err)
	}
	evs, _ = s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"})
	if len(evs) != 2 || evs[1].Revision != 2 {
		t.Fatalf("after set 2: want rev 2, got %d events", len(evs))
	}
	if evs[1].BeforeJSON == nil || evs[1].AfterJSON == nil {
		t.Errorf("set 2: both snapshots must be set")
	}

	// A no-op set writes nothing (the row did not move).
	if w, err := s.SetMergeRule(user, r2); err != nil || !w.OK() {
		t.Fatalf("no-op set: %v %v", w, err)
	}
	if evs, _ = s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"}); len(evs) != 2 {
		t.Fatalf("the no-op set wrote an event: %d events, want 2", len(evs))
	}

	// 3: clear (before set, after NULL; the row is gone, the revision stands).
	if w, err := s.ClearMergeRule(user, "mr-root"); err != nil || !w.OK() {
		t.Fatalf("clear: %v %v", w, err)
	}
	if _, err := s.GetMergeRule("mr-root"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after clear: GetMergeRule = %v, want ErrNotFound", err)
	}
	evs, _ = s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"})
	if len(evs) != 3 || evs[2].Revision != 3 || evs[2].Op != "clear" {
		t.Fatalf("after clear: want rev-3 clear, got %d events", len(evs))
	}
	if evs[2].BeforeJSON == nil || evs[2].AfterJSON != nil {
		t.Errorf("clear: before must be set and after NULL")
	}

	// 4: set again — the sequence CONTINUES, it does not reset to 1.
	r4 := mergerule.Baseline()
	r4.ComponentID = "mr-root"
	r4.Method = "rebase"
	w4, err := s.SetMergeRule(user, r4)
	if err != nil || !w4.OK() {
		t.Fatalf("set 4: %v %v", w4, err)
	}
	if got := w4.Rule.Revision; got != 4 {
		t.Fatalf("after clear+set: revision = %d, want 4 (the sequence never resets)", got)
	}
	if evs, _ = s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"}); len(evs) != 4 || evs[3].Revision != 4 {
		t.Fatalf("after set 4: want 4 events ending at rev 4, got %d", len(evs))
	}

	// A refused write (the agent) writes no row and no event.
	if w, err := s.SetMergeRule(agent, r4); err != nil || w.OK() {
		t.Fatalf("agent set: want the actor refusal, got %v %v", w, err)
	}
	if evs, _ = s.ListMergeRuleEvents(MergeRuleEventFilter{ComponentID: "mr-root"}); len(evs) != 4 {
		t.Fatalf("the refused write wrote an event: %d events, want 4", len(evs))
	}

	// The history table is append-only, at the trigger layer.
	for _, stmt := range []string{
		`UPDATE merge_rule_event SET summary = 'rewritten' WHERE 1=1`,
		`DELETE FROM merge_rule_event`,
	} {
		err := g.Exec(stmt).Error
		if err == nil {
			t.Errorf("%s was allowed — the audit is editable", stmt)
			continue
		}
		if !strings.Contains(err.Error(), "merge_rule_event is append-only") {
			t.Errorf("%s refused without naming its table: %v", stmt, err)
		}
	}
	if n := countTable(t, g, "merge_rule_event"); n != 4 {
		t.Errorf("events after the refused writes = %d, want 4", n)
	}
}

// TestEffectiveMergeRuleOwnReplacesRootElseBaseline — the VG3 resolver:
// a child's own row REPLACES the root's whole row (it is not merged);
// without one, a child of a live root takes the root's; without either,
// the BASELINE — never a row; an archived node takes and supplies
// nothing; an unknown component is an error, not an empty answer.
func TestEffectiveMergeRuleOwnReplacesRootElseBaseline(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "er-root")
	s57SeedChild(t, g, "er-child", "er-root")
	user := s57User

	rootRule := mergerule.Baseline()
	rootRule.ComponentID = "er-root"
	rootRule.RequireDoD = "all_met"
	rootRule.Method = "rebase"
	if w, err := s.SetMergeRule(user, rootRule); err != nil || !w.OK() {
		t.Fatalf("seed the root's rule: %v %v", w, err)
	}

	// The root sees its own.
	r, src, err := s.EffectiveMergeRule("er-root")
	if err != nil || src != "own" || r.RequireDoD != "all_met" || r.Method != "rebase" {
		t.Fatalf("root: src %s rule %+v err %v, want own + the root's values", src, r, err)
	}

	// The child, with no row of its own, takes the ROOT's whole row —
	// but judged as itself (ComponentID is the child's).
	r, src, err = s.EffectiveMergeRule("er-child")
	if err != nil || src != "root" {
		t.Fatalf("child without a row: src %s err %v, want root", src, err)
	}
	if r.RequireDoD != "all_met" || r.Method != "rebase" || r.ComponentID != "er-child" {
		t.Errorf("child takes the root's row judged as itself: %+v", r)
	}

	// The child's own row REPLACES the root's whole row.
	childRule := mergerule.Baseline()
	childRule.ComponentID = "er-child"
	childRule.RequireLocal = true
	if w, err := s.SetMergeRule(user, childRule); err != nil || !w.OK() {
		t.Fatalf("set the child's rule: %v %v", w, err)
	}
	r, src, err = s.EffectiveMergeRule("er-child")
	if err != nil || src != "own" {
		t.Fatalf("child with a row: src %s err %v, want own", src, err)
	}
	if r.RequireDoD != "none" || r.RequireLocal != true || r.Method != "squash" {
		t.Errorf("the child's row replaced the root's whole row: %+v", r)
	}
	// The root is untouched by the child's write.
	if r, src, _ := s.EffectiveMergeRule("er-root"); src != "own" || r.RequireDoD != "all_met" {
		t.Errorf("the root's rule moved when the child wrote its own: %+v (%s)", r, src)
	}

	// Clear the child's row: back to the root's.
	if w, err := s.ClearMergeRule(user, "er-child"); err != nil || !w.OK() {
		t.Fatalf("clear the child's rule: %v %v", w, err)
	}
	if _, src, err := s.EffectiveMergeRule("er-child"); err != nil || src != "root" {
		t.Fatalf("child after its clear: src %s err %v, want root", src, err)
	}

	// A rootless child: baseline (never a row).
	seedComponent(t, g, "er-lone")
	s57SeedChild(t, g, "er-orphan", "er-lone")
	r, src, err = s.EffectiveMergeRule("er-orphan")
	if err != nil || src != "baseline" {
		t.Fatalf("orphan child: src %s err %v, want baseline", src, err)
	}
	if !r.IsBaseline() || r.Revision != 0 || r.Source != "baseline" {
		t.Errorf("the baseline is the defaults, revision 0, source baseline: %+v", r)
	}

	// An archived root supplies nothing.
	seedComponent(t, g, "er-arch-root")
	s57SeedChild(t, g, "er-arch-child", "er-arch-root")
	arch := mergerule.Baseline()
	arch.ComponentID = "er-arch-root"
	arch.RequireLocal = true
	if w, err := s.SetMergeRule(user, arch); err != nil || !w.OK() {
		t.Fatalf("set the archived root's rule: %v %v", w, err)
	}
	s57ArchiveComponent(t, g, "er-arch-root")
	if _, src, err := s.EffectiveMergeRule("er-arch-child"); err != nil || src != "baseline" {
		t.Fatalf("child of an archived root: src %s err %v, want baseline", src, err)
	}
	// An archived node takes nothing, even with its own row.
	s57ArchiveComponent(t, g, "er-arch-child")
	if r, src, err := s.EffectiveMergeRule("er-arch-child"); err != nil || src != "baseline" || r.RequireLocal {
		t.Fatalf("archived child with its own row: src %s %+v, want the baseline", src, r)
	}

	// Unknown is an error (a read that cannot resolve its target is not
	// an empty answer).
	if _, _, err := s.EffectiveMergeRule("no-such-component"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown component: err %v, want ErrNotFound", err)
	}
}

// TestMergeRuleDoors — the write doors (VG23's shape): an agent or an
// anonymous author is refused before any SQL, an unknown or archived
// component is refused, every closed set is checked with a refusal that
// names the value, and a refusal writes nothing (no row, no event).
func TestMergeRuleDoors(t *testing.T) {
	g, s := testStore(t)
	seedComponent(t, g, "dr-root")
	user := s57User
	agent := Actor{Kind: "agent", ID: "a1"}
	anon := Actor{Kind: "user", ID: ""}

	base := func() mergerule.Rule {
		r := mergerule.Baseline()
		r.ComponentID = "dr-root"
		return r
	}

	// The actor doors.
	for name, a := range map[string]Actor{"agent": agent, "anonymous": anon} {
		w, err := s.SetMergeRule(a, base())
		if err != nil || w.OK() || w.Refusal != MergeRuleRefusalActorNotHuman {
			t.Fatalf("%s: want actor-not-human, got %v %v %v", name, w.Refusal, w.Reason, err)
		}
	}
	// The component doors.
	w, err := s.SetMergeRule(user, mergerule.Rule{ComponentID: "no-such"})
	if err != nil || w.OK() || w.Refusal != MergeRuleRefusalComponentUnknown {
		t.Fatalf("unknown component: %v %v", w, err)
	}
	s57ArchiveComponent(t, g, "dr-root")
	if w, err := s.SetMergeRule(user, base()); err != nil || w.OK() || w.Refusal != MergeRuleRefusalComponentArchived {
		t.Fatalf("archived component: %v %v", w, err)
	}
	if w, err := s.ClearMergeRule(user, "dr-root"); err != nil || w.OK() || w.Refusal != MergeRuleRefusalComponentArchived {
		t.Fatalf("clear on archived: %v %v", w, err)
	}
	// The live root the closed-set drill runs on.
	seedComponent(t, g, "dr-live")

	// Clear on a component with no row: a refusal, not an error.
	if w, err := s.ClearMergeRule(user, "dr-live"); err != nil || w.OK() || w.Refusal != MergeRuleRefusalRuleUnknown {
		t.Fatalf("clear without a row: %v %v", w, err)
	}

	// The closed sets, each naming its value.
	bad := []struct {
		mutate func(*mergerule.Rule)
		want   MergeRuleRefusal
	}{
		{func(r *mergerule.Rule) { r.RequireDoD = "some" }, MergeRuleRefusalRequireDoDInvalid},
		{func(r *mergerule.Rule) { r.ChecksPolicy = "greenish" }, MergeRuleRefusalChecksPolicyInvalid},
		{func(r *mergerule.Rule) { r.NoChecks = "maybe" }, MergeRuleRefusalNoChecksInvalid},
		{func(r *mergerule.Rule) { r.ReviewMode = "whichever" }, MergeRuleRefusalReviewModeInvalid},
		{func(r *mergerule.Rule) { r.Method = "amend" }, MergeRuleRefusalMethodInvalid},
	}
	for i, c := range bad {
		r := base()
		r.ComponentID = "dr-live"
		c.mutate(&r)
		w, err := s.SetMergeRule(user, r)
		if err != nil || w.OK() || w.Refusal != c.want {
			t.Errorf("closed set %d: want %s, got %v (%s) %v", i, c.want, w.Refusal, w.Reason, err)
		}
	}
	// The JSON lists, at the decoder: an absent list (null) normalises to
	// the empty list (Baseline() itself carries a nil RequiredChecks, so a
	// nil-rejecting door would make the baseline unsettable); a bare
	// string or an object is not a list.
	if out, ok := jsonStringList("null"); !ok || len(out) != 0 {
		t.Errorf("jsonStringList(null): want the empty list, got %v %v", out, ok)
	}
	if out, ok := jsonStringList(`"just-a-string"`); ok {
		t.Errorf("jsonStringList(bare string): want a refusal, got %v", out)
	}
	if out, ok := jsonStringList(`{"a":1}`); ok {
		t.Errorf("jsonStringList(object): want a refusal, got %v", out)
	}
	// The good one lands, and nothing the refusals touched was written.
	if w, err := s.SetMergeRule(user, func() mergerule.Rule {
		r := mergerule.Baseline()
		r.ComponentID = "dr-live"
		r.RequiredChecks = []string{"build", "test"}
		r.AeonReviewAgents = []string{"code-reviewer"}
		r.Method = "rebase"
		return r
	}()); err != nil || !w.OK() {
		t.Fatalf("the good set refused: %v %v", w, err)
	}
	got, err := s.GetMergeRule("dr-live")
	if err != nil {
		t.Fatalf("get the landed rule: %v", err)
	}
	if len(got.RequiredChecks) != 2 || got.RequiredChecks[0] != "build" || got.Method != "rebase" || got.Source != "own" {
		t.Errorf("round trip lost the lists: %+v", got)
	}
	if n := countTable(t, g, "merge_rule_event"); n != 1 {
		t.Errorf("events after the door drill = %d, want 1 (only the good set wrote)", n)
	}
}
