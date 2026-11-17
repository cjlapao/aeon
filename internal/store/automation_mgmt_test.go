package store

// automation_mgmt_test.go — S49 T1: the storage under the management slice.
// The three things a reviewer must be able to trust from these tests alone:
// the migration is the only DDL and it upgrades (never fabricates) old rows;
// the audit and the archive cannot be undone below the Go API (the CHECKs
// and triggers are the net); and the fire view is its two tables spoken in
// AU12's words — a mapping, never a third answer.

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const s49AutomationMigration = "20261103102501_automation_management"

func s49MigrationIndex(t *testing.T) int {
	t.Helper()
	for i, m := range Migrations() {
		if m.Name == s49AutomationMigration {
			return i
		}
	}
	t.Fatalf("the migration %s is not in Migrations()", s49AutomationMigration)
	return -1
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column)
	if err != nil {
		t.Fatalf("pragma_table_info %s.%s: %v", table, column, err)
	}
	defer rows.Close()
	var n int
	if !rows.Next() {
		t.Fatalf("pragma_table_info %s.%s: no row", table, column)
	}
	if err := rows.Scan(&n); err != nil {
		t.Fatalf("pragma_table_info %s.%s: %v", table, column, err)
	}
	return n == 1
}

func mgmtSchedule(t *testing.T, s *Store, id, kind string, paused bool) {
	t.Helper()
	row := TriggerScheduleRow{ID: id, Kind: kind, Blueprint: "dev-loop", Expr: "0 * * * *",
		Zone: "UTC", CreatedAt: "2026-11-01T00:00:00Z", Paused: paused}
	if kind == "webhook" {
		row.Expr, row.Zone = "", ""
	}
	if err := s.SaveTriggerSchedule(t.Context(), row); err != nil {
		t.Fatalf("save schedule %s: %v", id, err)
	}
}

func beginMgmtTx(t *testing.T, s *Store) *sql.Tx {
	t.Helper()
	tx, err := s.w.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func auditRow(source, id, op string) AutomationChange {
	return AutomationChange{Source: source, TriggerID: id, Op: op, Summary: op + " " + id,
		ActorKind: "user", ActorID: "op", OccurredAt: "2026-11-03T10:00:00Z"}
}

// 1 — the migration is the only DDL source, applies fresh, upgrades, and
// gives pre-S49 schedule rows the honest default (user kind, NO id).
func TestAutomationMigrationAppliesFreshAndUpgrade(t *testing.T) {
	s := openTemp(t)
	for _, tc := range []struct{ table, column string }{
		{"trigger_schedule", "created_by_kind"}, {"trigger_schedule", "created_by_id"},
		{"trigger_schedule", "archived_at"}, {"trigger_schedule", "archived_reason"},
		{"event_trigger", "archived_at"}, {"event_trigger", "archived_reason"},
	} {
		if !columnExists(t, s.r, tc.table, tc.column) {
			t.Fatalf("column %s.%s missing after a fresh apply", tc.table, tc.column)
		}
	}
	for _, tc := range []struct{ kind, name string }{
		{"table", "automation_change"}, {"index", "idx_automation_change_trigger"},
		{"trigger", "automation_change_no_update"}, {"trigger", "automation_change_no_delete"},
		{"view", "automation_fire_view"},
	} {
		if n := countCatalogueEntries(t, s.GormDB(), tc.kind, tc.name); n != 1 {
			t.Fatalf("%s %s: %d entries, want 1", tc.kind, tc.name, n)
		}
	}

	idx := s49MigrationIndex(t)
	g, closeDB := rawGorm(t, filepath.Join(t.TempDir(), "upgrade.db"))
	defer closeDB()
	applyMigrations(t, g, Migrations()[:idx])
	if err := g.Exec(`INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at)
		VALUES ('legacy','schedule','dev-loop','0 * * * *','UTC','2026-10-01T00:00:00Z')`).Error; err != nil {
		t.Fatalf("seed a pre-S49 schedule: %v", err)
	}
	applyMigrations(t, g, Migrations()[idx:idx+1])
	var kind, id string
	if err := g.Raw(`SELECT created_by_kind, created_by_id FROM trigger_schedule WHERE id='legacy'`).
		Row().Scan(&kind, &id); err != nil {
		t.Fatalf("read the upgraded legacy row: %v", err)
	}
	if kind != "user" || id != "" {
		t.Fatalf("legacy row got kind=%q id=%q: the upgrade must say 'user' with NO id — ''"+
			" means predates provenance, a fabricated id is the failure mode", kind, id)
	}
	var archived sql.NullString
	if err := g.Raw(`SELECT archived_at FROM trigger_schedule WHERE id='legacy'`).Row().Scan(&archived); err != nil || archived.Valid {
		t.Fatalf("legacy row archived_at=%v (valid=%v): archive state starts as absent, not ''", archived, archived.Valid)
	}
}

// 2 — lock 3 of AU1, extended to the schedule family: the CHECK refuses any
// author but a user, below every Go door.
func TestScheduleCreatorCheckRefusesNonUser(t *testing.T) {
	s := openTemp(t)
	_, err := s.w.Exec(`INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at,created_by_kind)
		VALUES ('evil','schedule','dev-loop','0 * * * *','UTC','2026-11-01T00:00:00Z','agent')`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("created_by_kind='agent': err=%v — the CHECK must refuse it", err)
	}
	mgmtSchedule(t, s, "s1", "schedule", false)
	tx := beginMgmtTx(t, s)
	if err := SetScheduleCreatorTx(t.Context(), tx, "s1", Actor{Kind: "agent", ID: "a1"}); err == nil {
		t.Fatal("SetScheduleCreatorTx accepted an agent actor")
	} else if !errors.Is(err, ErrActorNotHuman) {
		t.Fatalf("want ErrActorNotHuman, got %v", err)
	}
	if err := SetScheduleCreatorTx(t.Context(), tx, "s1", autoUser); err != nil {
		t.Fatalf("stamp the person: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r, err := s.TriggerSchedule(t.Context(), "s1")
	if err != nil || r.CreatedByKind != "user" || r.CreatedByID != "op" {
		t.Fatalf("creator after stamp: %+v (err %v)", r, err)
	}
}

// 3 — the audit is append-only by trigger, tested below the Go API (there
// deliberately is no update or delete accessor to test).
func TestChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	tx := beginMgmtTx(t, s)
	if err := AutomationChangeInsertTx(tx, auditRow("schedule", "s1", "create")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE automation_change SET summary='reworded history' WHERE id=1`); err == nil {
		t.Fatal("UPDATE into the audit succeeded — history is rewritable")
	}
	if _, err := s.w.Exec(`DELETE FROM automation_change WHERE id=1`); err == nil {
		t.Fatal("DELETE from the audit succeeded — history is erasable")
	}
	got, err := s.AutomationChangeList(t.Context(), "", "", 0)
	if err != nil || len(got) != 1 || got[0].Summary != "create s1" {
		t.Fatalf("audit after failed rewrites: %+v (err %v)", got, err)
	}
}

// 4 — the CHECK vocabulary is the net: bad source/op/actor/summary/empty id
// are refused at the row, and listing is newest-first with the clamp.
func TestChangeCheckConstraints(t *testing.T) {
	s := openTemp(t)
	tx := beginMgmtTx(t, s)
	bad := []AutomationChange{
		auditRow("timer", "s1", "create"),
		auditRow("event", "s1", "delete"),
		func() AutomationChange { c := auditRow("event", "s1", "create"); c.ActorKind = "agent"; return c }(),
		func() AutomationChange { c := auditRow("event", "s1", "create"); c.Summary = ""; return c }(),
		auditRow("event", "", "create"),
	}
	for i, c := range bad {
		if err := AutomationChangeInsertTx(tx, c); err == nil {
			t.Fatalf("bad audit %d (source=%q op=%q actor=%q): accepted", i, c.Source, c.Op, c.ActorKind)
		}
	}
	for _, c := range []AutomationChange{auditRow("schedule", "s1", "create"), auditRow("event", "e1", "disable")} {
		if err := AutomationChangeInsertTx(tx, c); err != nil {
			t.Fatalf("valid audit (%s/%s): %v", c.Source, c.Op, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.AutomationChangeList(t.Context(), "", "", 0)
	if err != nil || len(got) != 2 || got[0].Source != "event" {
		t.Fatalf("list must be newest-first, got %+v (err %v)", got, err)
	}
	got, err = s.AutomationChangeList(t.Context(), "schedule", "", 5000)
	if err != nil || len(got) != 1 || got[0].TriggerID != "s1" {
		t.Fatalf("source filter + clamp: %+v (err %v)", got, err)
	}
	if big, err := s.AutomationChangeList(t.Context(), "", "", -7); err != nil || len(big) != 2 {
		t.Fatalf("negative limit must fall back to the default, got %d rows (err %v)", len(big), err)
	}
}

// 5 — archive is a state on the columns the ENGINES read (T0 item 4: the
// clock and the webhook door read paused, the dispatcher reads enabled),
// and nothing above is destroyed.
func TestArchiveSetsAnEngineInvisibleOffStateAndKeepsHistory(t *testing.T) {
	s := openTemp(t)
	mgmtSchedule(t, s, "nightly", "schedule", false)
	tr := newTriggerRow(t, s, "on-approved")
	if err := s.RecordTriggerFire(t.Context(), TriggerFireRow{ScheduleID: "nightly",
		Instant: "2026-11-02T00:00:00Z", Outcome: "fired", LoopID: "l-1", At: "2026-11-02T00:00:01Z"}); err != nil {
		t.Fatal(err)
	}
	tx := beginMgmtTx(t, s)
	if err := ScheduleArchiveTx(t.Context(), tx, "nightly", "seasonal", "2026-11-03T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := AutomationChangeInsertTx(tx, auditRow("schedule", "nightly", "archive")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r, err := s.TriggerSchedule(t.Context(), "nightly")
	if err != nil || !r.Paused || r.ArchivedAt != "2026-11-03T09:00:00Z" || r.ArchivedReason != "seasonal" {
		t.Fatalf("archived schedule: %+v (err %v) — the clock reads paused and must skip it", r, err)
	}
	if hist, err := s.TriggerFireHistory(t.Context(), "nightly", 10); err != nil || len(hist) != 1 {
		t.Fatalf("fire history survived archive wrong: %d rows (err %v)", len(hist), err)
	}
	if ch, err := s.AutomationChangeList(t.Context(), "schedule", "nightly", 0); err != nil || len(ch) != 1 {
		t.Fatalf("audit survived archive wrong: %+v (err %v)", ch, err)
	}

	etx := beginMgmtTx(t, s)
	if err := EventArchiveTx(t.Context(), etx, tr.ID, "superseded", "2026-11-03T09:01:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := AutomationChangeInsertTx(etx, auditRow("event", tr.ID, "archive")); err != nil {
		t.Fatal(err)
	}
	if err := etx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEventTrigger(tr.ID)
	if err != nil || got.Enabled || got.ArchivedAt != "2026-11-03T09:01:00Z" || got.ArchivedReason != "superseded" {
		t.Fatalf("archived event trigger: %+v (err %v) — the dispatcher reads enabled and must skip it", got, err)
	}
	if got.DisabledReason != "archived" {
		t.Fatalf("archived event trigger disabled_reason=%q, want the truthful 'archived'", got.DisabledReason)
	}
}

// 6 — AN6: unarchive clears only the archive columns. The trigger comes
// back disabled/paused; re-enabling is its own separate act.
func TestUnarchiveLeavesTheTriggerDisabled(t *testing.T) {
	s := openTemp(t)
	mgmtSchedule(t, s, "nightly", "schedule", false)
	tx := beginMgmtTx(t, s)
	if err := ScheduleArchiveTx(t.Context(), tx, "nightly", "why", "2026-11-03T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	utx := beginMgmtTx(t, s)
	if err := ScheduleUnarchiveTx(t.Context(), utx, "nightly"); err != nil {
		t.Fatal(err)
	}
	if err := utx.Commit(); err != nil {
		t.Fatal(err)
	}
	r, _ := s.TriggerSchedule(t.Context(), "nightly")
	if r.ArchivedAt != "" || r.ArchivedReason != "" {
		t.Fatalf("unarchive left archive state: %+v", r)
	}
	if !r.Paused {
		t.Fatal("unarchive re-armed the clock — re-enabling must be its own act (AN6)")
	}

	tr := newTriggerRow(t, s, "on-approved")
	etx := beginMgmtTx(t, s)
	if err := EventArchiveTx(t.Context(), etx, tr.ID, "why", "2026-11-03T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := etx.Commit(); err != nil {
		t.Fatal(err)
	}
	u2 := beginMgmtTx(t, s)
	if err := EventUnarchiveTx(t.Context(), u2, tr.ID); err != nil {
		t.Fatal(err)
	}
	if err := u2.Commit(); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetEventTrigger(tr.ID)
	if got.ArchivedAt != "" || got.Enabled {
		t.Fatalf("unarchived event trigger still armed or still archived: %+v", got)
	}
}

// 7 — the operator's lists hide archived rows unless asked; the engines'
// own reads are untouched.
func TestListsExcludeArchivedByDefault(t *testing.T) {
	s := openTemp(t)
	mgmtSchedule(t, s, "live", "schedule", false)
	mgmtSchedule(t, s, "gone", "schedule", false)
	tx := beginMgmtTx(t, s)
	if err := ScheduleArchiveTx(t.Context(), tx, "gone", "done with it", "2026-11-03T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if all, err := s.TriggerSchedules(t.Context()); err != nil || len(all) != 2 {
		t.Fatalf("the engine's own list changed (an archived row is just paused there): %d (err %v)", len(all), err)
	}
	live, err := s.TriggerSchedulesScoped(t.Context(), false)
	if err != nil || len(live) != 1 || live[0].ID != "live" {
		t.Fatalf("scoped default: %+v (err %v)", live, err)
	}
	every, err := s.TriggerSchedulesScoped(t.Context(), true)
	if err != nil || len(every) != 2 {
		t.Fatalf("includeArchived: %d rows (err %v)", len(every), err)
	}

	tr := newTriggerRow(t, s, "on-approved")
	etx := beginMgmtTx(t, s)
	if err := EventArchiveTx(t.Context(), etx, tr.ID, "why", "2026-11-03T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := etx.Commit(); err != nil {
		t.Fatal(err)
	}
	def, err := s.ListEventTriggers(EventTriggerFilter{})
	if err != nil || len(def) != 0 {
		t.Fatalf("event list default: %d rows (err %v)", len(def), err)
	}
	withArchived, err := s.ListEventTriggers(EventTriggerFilter{IncludeArchived: true})
	if err != nil || len(withArchived) != 1 || withArchived[0].Enabled {
		t.Fatalf("includeArchived events: %+v (err %v)", withArchived, err)
	}
}

// seedMgmtFires fills both fire tables with every outcome once, plus one
// claimed-undecided event row the view must NOT show.
func seedMgmtFires(t *testing.T, s *Store) {
	t.Helper()
	mgmtSchedule(t, s, "nightly", "schedule", false)
	mgmtSchedule(t, s, "hook", "webhook", false)
	for i, o := range []string{"fired", "did_not_fire", "skipped", "refused", "could_not_say"} {
		if err := s.RecordTriggerFire(t.Context(), TriggerFireRow{ScheduleID: "nightly",
			Instant: fmt.Sprintf("2026-11-02T0%d:00:00Z", i), Outcome: o,
			Reason: "because " + o, LoopID: "l-1", At: fmt.Sprintf("2026-11-02T0%d:00:05Z", i)}); err != nil {
			t.Fatalf("seed schedule fire %s: %v", o, err)
		}
	}
	tr := newTriggerRow(t, s, "on-approved")
	decided := []EventFireDecision{
		{Outcome: "started", LoopID: "l-2", LoopSlug: "sl"},
		{Outcome: "refused", Reason: "depth-exceeded"},
		{Outcome: "limited", Reason: "max-runs-per-hour"},
		{Outcome: "skipped", Reason: "disabled"},
		{Outcome: "could-not-say", Reason: "start-error", CostNote: "cost-unknown"},
	}
	for i, d := range decided {
		if _, _, err := s.RecordEventFire(NewEventFire{TriggerID: tr.ID, OutboxID: int64(100 + i),
			SubjectKind: "goal", SubjectID: fmt.Sprintf("g-%d", i), Depth: 1},
			d, fireAt.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("seed event fire %s: %v", d.Outcome, err)
		}
	}
	if _, _, err := s.ClaimEventFire(NewEventFire{TriggerID: tr.ID, OutboxID: 999,
		SubjectKind: "goal", SubjectID: "g-undecided"}, fireAt.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// 8 — the unification IS its sources in AU12's words: every S20 outcome
// arrives mapped (T0 item 5), every S40 outcome arrives verbatim, the
// in-flight claim earns no row, and the fields ride across.
func TestFireViewEqualsBothTablesViaTheMapping(t *testing.T) {
	s := openTemp(t)
	seedMgmtFires(t, s)
	rows, counts, err := s.AutomationFires(t.Context(), FireViewFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 {
		t.Fatalf("view holds %d rows, want 10 (5 per family; the undecided claim is not an answer): %+v", len(rows), rows)
	}
	want := map[string]int{"started": 2, "skipped": 3, "refused": 2, "limited": 1, "could-not-say": 2}
	for o, n := range want {
		if counts[o] != n {
			t.Fatalf("count %s = %d, want %d (counts %v)", o, counts[o], n, counts)
		}
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != 10 {
		t.Fatalf("counts sum %d, want 10 — an outcome outside the five words appeared", total)
	}
	// did_not_fire AND skipped both map to skipped; the reason must carry
	// the distinction verbatim, since the mapping is many-to-one.
	sawDidNotFire, sawSkipped := false, false
	for _, r := range rows {
		if r.Source == "schedule" && r.Outcome == "skipped" {
			switch r.Reason {
			case "because did_not_fire":
				sawDidNotFire = true
			case "because skipped":
				sawSkipped = true
			}
		}
		if r.Source == "schedule" && r.Outcome == "started" && r.LoopID != "l-1" {
			t.Fatalf("schedule side lost its loop id: %+v", r)
		}
		if r.Source == "event" && r.Outcome == "started" && (r.LoopSlug != "sl" || r.SubjectID != "g-0" || r.Depth != 1) {
			t.Fatalf("event side lost its fields: %+v", r)
		}
		if r.Source == "event" && r.Outcome == "could-not-say" && r.CostNote != "cost-unknown" {
			t.Fatalf("event side lost its cost note: %+v", r)
		}
	}
	if !sawDidNotFire || !sawSkipped {
		t.Fatalf("reason no longer distinguishes the merged skips (did_not_fire=%v skipped=%v)", sawDidNotFire, sawSkipped)
	}
	// The view's source column comes from trigger_schedule.kind, so the
	// webhook family is tellable apart from the schedule family.
	hookFires := 0
	for _, r := range rows {
		if r.Source == "webhook" {
			hookFires++
		}
	}
	if hookFires != 0 {
		t.Fatalf("webhook fires %d: only 'nightly' fired, the 'hook' row is idle", hookFires)
	}
	// Filters: by source and by outcome.
	events, _, err := s.AutomationFires(t.Context(), FireViewFilter{Source: "event"})
	if err != nil || len(events) != 5 {
		t.Fatalf("source filter: %d rows (err %v)", len(events), err)
	}
	refused, _, err := s.AutomationFires(t.Context(), FireViewFilter{Outcome: "refused"})
	if err != nil || len(refused) != 2 {
		t.Fatalf("outcome filter: %d rows (err %v)", len(refused), err)
	}
}

// 9 — a view is a read. No door writes through it.
func TestFireViewHasNoWritePath(t *testing.T) {
	s := openTemp(t)
	if _, err := s.w.Exec(`INSERT INTO automation_fire_view (source,trigger_id,at,outcome) VALUES ('schedule','x','now','started')`); err == nil {
		t.Fatal("INSERT into the view succeeded — a second truth can be written")
	}
	if _, err := s.w.Exec(`UPDATE automation_fire_view SET outcome='started' WHERE 1=1`); err == nil {
		t.Fatal("UPDATE through the view succeeded")
	}
}

// 10 — keyset paging: interleaved families, newest first, and walking the
// cursor never repeats or drops a row.
func TestAutomationFiresPagesWithoutOverlap(t *testing.T) {
	s := openTemp(t)
	seedMgmtFires(t, s)
	seen := map[string]bool{}
	var cursor string
	pages := 0
	for {
		rows, _, err := s.AutomationFires(t.Context(), FireViewFilter{Limit: 3, BeforeID: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		pages++
		if pages > 12 {
			t.Fatal("paging does not terminate")
		}
		for _, r := range rows {
			key := fmt.Sprintf("%s|%s:%d", r.At, r.Source, r.FID)
			if seen[key] {
				t.Fatalf("cursor repeated %s", key)
			}
			seen[key] = true
		}
		last := rows[len(rows)-1]
		cursor = fmt.Sprintf("%s|%s:%d", last.At, last.Source, last.FID)
	}
	if len(seen) != 10 {
		t.Fatalf("cursor walk saw %d distinct rows, want 10", len(seen))
	}
	if _, _, err := s.AutomationFires(t.Context(), FireViewFilter{BeforeID: "garbage"}); err == nil {
		t.Fatal("a malformed cursor was accepted")
	}
}

// 11 — runs are loop rows read through their provenance (0033):
// loop.trigger_id plus the family's own started_by word, newest first.
func TestRunsFollowLoopTriggerId(t *testing.T) {
	s := openTemp(t)
	mk := func(slug, startedBy, triggerID string) string {
		l, err := s.CreateLoop(NewLoop{Slug: slug, ComponentRoot: "r", Goal: "g"})
		if err != nil {
			t.Fatalf("loop %s: %v", slug, err)
		}
		if err := s.SetLoopProvenance(t.Context(), l.ID, startedBy, triggerID); err != nil {
			t.Fatalf("provenance %s: %v", slug, err)
		}
		return l.ID
	}
	mk("nightly-a", "trigger", "nightly")
	mk("nightly-b", "trigger", "nightly")
	mk("hook-a", "webhook", "hook")
	mk("event-a", "event", "on-approved")
	// A person's loop carries NO provenance (started_by NULL) — it must not
	// surface in any family's runs. SetLoopProvenance refuses the empty word,
	// so absence is created by not calling it at all.
	if _, err := s.CreateLoop(NewLoop{Slug: "human-a", ComponentRoot: "r", Goal: "g"}); err != nil {
		t.Fatal(err)
	}

	runs, total, err := s.AutomationRuns(t.Context(), "schedule", "nightly", 0)
	if err != nil || total != 2 || len(runs) != 2 {
		t.Fatalf("schedule runs: %d/%d (err %v)", len(runs), total, err)
	}
	if runs[0].Slug != "nightly-a" || runs[1].Slug != "nightly-b" {
		// updated_at is the fixed clock for both, so the store's listing
		// convention (updated_at DESC, slug) orders them by slug — the
		// point is determinism, not luck: loop ids are random hex.
		t.Fatalf("runs are ordered by the store's convention (updated_at DESC, slug), got %q then %q", runs[0].Slug, runs[1].Slug)
	}
	if runs[0].Status == "" || runs[0].StartedAt == "" {
		t.Fatalf("run row is not enough to render: %+v", runs[0])
	}
	if _, _, err := s.AutomationRuns(t.Context(), "webhook", "hook", 0); err != nil {
		t.Fatal(err)
	}
	ev, evTotal, err := s.AutomationRuns(t.Context(), "event", "on-approved", 0)
	if err != nil || evTotal != 1 || ev[0].Slug != "event-a" {
		t.Fatalf("event runs: %+v/%d (err %v)", ev, evTotal, err)
	}
	if _, _, err := s.AutomationRuns(t.Context(), "timer", "nightly", 0); err == nil {
		t.Fatal("a source outside schedule|webhook|event was accepted")
	}
	page, total, err := s.AutomationRuns(t.Context(), "schedule", "nightly", 1)
	if err != nil || total != 2 || len(page) != 1 {
		t.Fatalf("limit applies to the page, not the total: %d/%d (err %v)", len(page), total, err)
	}
}
