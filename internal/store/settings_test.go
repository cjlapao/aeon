package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The tests for the setting and setting_event tables (S44 T1).
//
// Every test opens a fresh, fully-migrated store (openTemp), so the migration
// is applied through the same framework worker a real open uses — the append
// trigger is proven by an UPDATE/DELETE the trigger refuses, not by a shape
// assertion (open item 1: the trigger IS the enforcement; the test is the
// proof). A raw write reaches the store's own writer (s.w) because these
// tests live in the store package (the only place the unexported writer is
// reachable, as agent_migration_test.go does); a raw write in a test is the
// sanctioned way to plant a row, never a domain rule (I2).

// settingAt is a fixed RFC3339 instant for the timestamps a test supplies —
// the store takes no clock, so the instant is the caller's.
const settingAt = "2026-10-15T09:41:12Z"

// settingsMigrationName finds the settings migration's ledger name in
// Migrations() without freezing the timestamp (the file name is the ID, set
// at creation time, so the test looks it up rather than hard-coding it).
func settingsMigrationName(t *testing.T) string {
	t.Helper()
	for _, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_settings") {
			return m.Name
		}
	}
	t.Fatal("no _settings migration in Migrations()")
	return ""
}

// ctx is the test context; the accessors take one so a real caller's
// cancellation is honoured, but a test wants no deadline.
func ctx() context.Context { return context.Background() }

// putEvent is a "set" audit event the caller fills with the fields it knows;
// the store fills OldValue and NewValue (from the read and the row,
// respectively), so the caller never races a read against the write.
func putEvent(key, value string) SettingEvent {
	return SettingEvent{Key: key, Op: "set", Actor: "tester", OccurredAt: settingAt}
}

// unsetEvent is the "unset" audit event a delete appends; the store fills
// OldValue (the value it removed) and NewValue (nil, the absence).
func unsetEvent(key string) SettingEvent {
	return SettingEvent{Key: key, Op: "unset", Actor: "tester", OccurredAt: settingAt}
}

// TestSettingMigrationCreatesBothTables is the migration's own proof: the two
// tables (with their columns) exist, and the index and the two append triggers
// the migration wrote are in the catalogue. It fails when the DDL drifted from
// the file the migration carries.
func TestSettingMigrationCreatesBothTables(t *testing.T) {
	s := openTemp(t)
	assertColumns(t, s, "setting", "key", "value", "updated_at", "updated_by")
	assertColumns(t, s, "setting_event", "id", "key", "op", "old_value",
		"new_value", "sensitive", "shadowed_by", "actor", "occurred_at")
	assertCatalogue(t, s, "index", "idx_setting_event_key")
	assertCatalogue(t, s, "trigger", "setting_event_no_update")
	assertCatalogue(t, s, "trigger", "setting_event_no_delete")
}

// TestSettingMigrationIsInTheLedger proves the migration is picked up, on a
// fresh apply and on an upgrade of a database seeded just before it — the
// "the ledger recorded it" half of forward-only. The name is looked up, never
// frozen (the file name is the ID); the "before it" vintage is computed
// dynamically as Migrations()[:len-1] (the settings file is the newest, so it
// is last), which is why a literal here would move when a later slice lands a
// later file (the S22 T1 lesson, recorded in automation_schema_test.go).
func TestSettingMigrationIsInTheLedger(t *testing.T) {
	want := settingsMigrationName(t)

	// 1. A fresh apply records it.
	s := openTemp(t)
	if err := assertLedgerHas(s.AppliedMigrations, want); err != nil {
		t.Fatalf("fresh apply: %v", err)
	}

	// 2. Seeding a database just before the migration and then upgrading it
	//    (the runner applies only what is pending) also records it — the file
	//    is not silently skipped on an existing install.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, Migrations()[:len(Migrations())-1])
	closeDB()

	// Open migrates the pending one (this file) and records it.
	s = openAt(t, dir)
	if err := assertLedgerHas(s.AppliedMigrations, want); err != nil {
		t.Fatalf("upgraded install: %v", err)
	}
}

// TestSettingKeyCheckAgreesWithTheGoGrammar is the CHECK net's proof: the
// grammar the registry will later own (CS3) is the same set the table admits
// — a key the boundary would refuse is also refused here, and a key it admits
// is admitted here. A mismatch means the net and the boundary would disagree,
// which is the gap the net is there to close.
func TestSettingKeyCheckAgreesWithTheGoGrammar(t *testing.T) {
	s := openTemp(t)

	// The one key the registry admits — it must pass the net.
	if err := rawInsertSetting(t, s, "agents.allowUnsignedImport", "true"); err != nil {
		t.Fatalf("a valid key was refused by the CHECK net: %v", err)
	}

	// Each of these the registry refuses — so the net must refuse them too.
	bad := []string{
		"",                       // length 0, below the 3 floor
		"a",                      // length 1, and no dot
		"a b.c",                  // a space, outside [A-Za-z0-9.]
		".x",                     // a leading dot
		"x.",                     // a trailing dot
		"a..b",                   // a double dot
		strings.Repeat("a", 129), // 129 chars, above the 128 ceiling
		"agents.x-y",             // a hyphen, outside [A-Za-z0-9.]
	}
	for _, k := range bad {
		if err := rawInsertSetting(t, s, k, "true"); err == nil {
			t.Fatalf("key %q passed the CHECK net; the net should refuse it (the boundary would)", k)
		}
	}
}

// TestSettingPutThenGetRoundTrips is the accessor mapping's proof: a value put
// through the store reads back identical, field for field.
func TestSettingPutThenGetRoundTrips(t *testing.T) {
	s := openTemp(t)
	row := SettingRow{Key: "agents.allowUnsignedImport", Value: "true", UpdatedAt: settingAt, UpdatedBy: "tester"}
	if err := s.SettingPut(ctx(), row, putEvent(row.Key, row.Value)); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := s.SettingGet(ctx(), row.Key)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Key != row.Key || got.Value != row.Value || got.UpdatedAt != row.UpdatedAt || got.UpdatedBy != row.UpdatedBy {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, row)
	}
}

// TestSettingPutOverwriteRecordsOldValue is the upsert's proof: a second put of
// the same key is a replace, not a duplicate — one setting row, and an event
// whose old_value is the value it replaced and new_value the value it set.
func TestSettingPutOverwriteRecordsOldValue(t *testing.T) {
	s := openTemp(t)
	key := "agents.allowUnsignedImport"
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v1")); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v2", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v2")); err != nil {
		t.Fatalf("second put: %v", err)
	}

	// One setting row (the upsert did not duplicate).
	if n, err := settingRowCount(s, key); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("setting rows for %q = %d; want 1 (the upsert replaced, not duplicated)", key, n)
	}

	// The newest event names the replace: old_value = v1, new_value = v2.
	events, err := s.SettingEventList(ctx(), key, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events for %q = %d; want 2 (one per put)", key, len(events))
	}
	last := events[0] // newest first
	if got := deref(last.OldValue); got != "v1" {
		t.Fatalf("second put's old_value = %s; want v1 (the value it replaced)", got)
	}
	if got := deref(last.NewValue); got != "v2" {
		t.Fatalf("second put's new_value = %s; want v2 (the value it set)", got)
	}
}

// TestSettingFirstPutHasNullOldValue is DB19's proof at the column: the first
// put of a key has no old value, so the event's old_value is NULL — a nil
// pointer, never "" (the empty string is a stored blank, a different fact).
func TestSettingFirstPutHasNullOldValue(t *testing.T) {
	s := openTemp(t)
	key := "agents.firstKey"
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	events, err := s.SettingEventList(ctx(), key, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d; want 1", len(events))
	}
	if events[0].OldValue != nil {
		t.Fatalf("first put's old_value = %q; want nil (NULL — no row existed, the default was in force)", *events[0].OldValue)
	}
}

// TestSettingPutAndEventAreAtomic is the one-transaction proof: if the event
// row cannot be written (an op the CHECK refuses), the whole transaction
// rolls back, so the setting row is not written either. The audit trail can
// never name a change the table did not make (CS8).
func TestSettingPutAndEventAreAtomic(t *testing.T) {
	s := openTemp(t)
	key := "agents.allowUnsignedImport"
	// A put whose event op the CHECK refuses: the row is written first, then
	// the event fails, so the whole transaction must roll back.
	bad := putEvent(key, "true")
	bad.Op = "not-a-real-op"
	err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "true", UpdatedAt: settingAt, UpdatedBy: "tester"}, bad)
	if err == nil {
		t.Fatal("a put with an unallowed op succeeded; the CHECK should have refused the event")
	}
	// The setting row is not written (the transaction rolled back).
	if _, ok, gerr := s.SettingGet(ctx(), key); gerr != nil || ok {
		t.Fatalf("after a refused put, the setting row exists (ok=%v err=%v); the transaction should have rolled back", ok, gerr)
	}
	// And no event was written.
	if n, err := eventCount(s); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("events = %d; want 0 (the refused put wrote nothing)", n)
	}
}

// TestSettingDeleteRemovesRowAndWritesUnsetEvent is the delete's proof: after
// a delete the row is gone, and the audit trail has one "unset" event whose
// old_value is the value that was removed and new_value is NULL.
func TestSettingDeleteRemovesRowAndWritesUnsetEvent(t *testing.T) {
	s := openTemp(t)
	key := "agents.allowUnsignedImport"
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.SettingDelete(ctx(), key, unsetEvent(key)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// The row is gone.
	if _, ok, err := s.SettingGet(ctx(), key); err != nil || ok {
		t.Fatalf("after delete, the row still exists (ok=%v err=%v)", ok, err)
	}
	// The audit trail has two events: the set and the unset.
	events, err := s.SettingEventList(ctx(), key, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d; want 2 (the set and the unset)", len(events))
	}
	unset := events[0] // newest first, the unset is last written
	if unset.Op != "unset" {
		t.Fatalf("newest event op = %q; want unset", unset.Op)
	}
	if got := deref(unset.OldValue); got != "v1" {
		t.Fatalf("unset old_value = %s; want v1 (the value removed)", got)
	}
	if unset.NewValue != nil {
		t.Fatalf("unset new_value = %q; want nil (NULL — the absence that replaced it)", *unset.NewValue)
	}
}

// TestSettingDeleteMissingWritesNoEvent is the "no noise" proof: deleting a key
// that was never stored returns existed=false and writes no event — an audit
// trail of no-ops is noise (CS8).
func TestSettingDeleteMissingWritesNoEvent(t *testing.T) {
	s := openTemp(t)
	// A baseline event, so "unchanged" means a real count, not an empty table.
	if err := s.SettingPut(ctx(), SettingRow{Key: "agents.allowUnsignedImport", Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent("agents.allowUnsignedImport", "v1")); err != nil {
		t.Fatalf("baseline put: %v", err)
	}
	// Delete a key that was never stored.
	if existed, err := s.SettingDelete(ctx(), "agents.neverStored", unsetEvent("agents.neverStored")); err != nil || existed {
		t.Fatalf("delete of an absent key: existed=%v err=%v; want (false, nil)", existed, err)
	}
	// The event count is unchanged.
	if n, err := eventCount(s); err != nil {
		t.Fatal(err)
	} else if n != 1 {
		t.Fatalf("events = %d; want 1 (the delete of an absent key wrote no event)", n)
	}
}

// TestSettingEventIsAppendOnly is open item 1's proof: the trigger the
// migration wrote is enforced — an UPDATE and a DELETE of an audit row each
// fail with the trigger's message, and the row survives. If this fails, the
// trigger did not apply or is not enforced, and the documented fallback is
// taken (delete the triggers, enforce append-only in code, and this becomes
// TestSettingEventHasNoUpdateOrDeletePath).
func TestSettingEventIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	key := "agents.allowUnsignedImport"
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	id := eventID(t, s, key)

	// An UPDATE is refused by the trigger.
	if _, err := s.w.Exec(`UPDATE setting_event SET occurred_at = '2026-10-15T09:42:00Z' WHERE id = ?`, id); err == nil {
		t.Fatal("UPDATE setting_event succeeded; the append-only trigger did not fire")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE refused by something other than the trigger: %v", err)
	}
	// A DELETE is refused by the trigger.
	if _, err := s.w.Exec(`DELETE FROM setting_event WHERE id = ?`, id); err == nil {
		t.Fatal("DELETE FROM setting_event succeeded; the append-only trigger did not fire")
	} else if !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE refused by something other than the trigger: %v", err)
	}
	// The row survived.
	if got := eventID(t, s, key); got != id {
		t.Fatalf("the audit row was lost (got id %d, want %d)", got, id)
	}
}

// TestSettingEventsOutliveTheirKey is the "no FK / no cascade" proof (CS8):
// deleting a setting row leaves its audit trail — the set and the unset both
// survive, because an unset deletes the setting row and the history must
// outlive it (there is no foreign key from setting_event to setting).
func TestSettingEventsOutliveTheirKey(t *testing.T) {
	s := openTemp(t)
	key := "agents.allowUnsignedImport"
	if err := s.SettingPut(ctx(), SettingRow{Key: key, Value: "v1", UpdatedAt: settingAt, UpdatedBy: "tester"},
		putEvent(key, "v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.SettingDelete(ctx(), key, unsetEvent(key)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Both events survive the delete.
	events, err := s.SettingEventList(ctx(), key, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events after delete = %d; want 2 (the set and the unset outlive the row)", len(events))
	}
	ops := map[string]bool{}
	for _, e := range events {
		ops[e.Op] = true
	}
	if !ops["set"] || !ops["unset"] {
		t.Fatalf("event ops = %v; want both set and unset", ops)
	}
}

// TestSettingEventListOrderAndLimit is the paging contract's proof: newest
// first, an unset limit defaults to 50, a limit above the cap clamps to 500,
// and key="" spans every key.
func TestSettingEventListOrderAndLimit(t *testing.T) {
	s := openTemp(t)
	// Plant 600 audit rows directly (the test is the listing's contract, not
	// the write), across three keys so key="" is proven to span them.
	const total = 600
	keys := []string{"agents.x", "agents.y", "agents.z"}
	for i := 0; i < total; i++ {
		if err := rawInsertEvent(t, s, keys[i%len(keys)], "set", "v", 0, nil); err != nil {
			t.Fatalf("plant event %d: %v", i, err)
		}
	}

	// Newest first: the highest id leads. (A limit above the cap clamps to
	// 500, so a small limit is used to read the top of the trail.)
	top, err := s.SettingEventList(ctx(), "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 5 {
		t.Fatalf("limit=5 returned %d; want 5", len(top))
	}
	if top[0].ID != int64(total) || top[1].ID != int64(total-1) {
		t.Fatalf("not newest first: ids %d, %d; want %d, %d", top[0].ID, top[1].ID, total, total-1)
	}

	// limit=0 defaults to 50.
	got, err := s.SettingEventList(ctx(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("limit=0 returned %d; want 50 (the default)", len(got))
	}
	// limit=9999 clamps to 500.
	got, err = s.SettingEventList(ctx(), "", 9999)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 500 {
		t.Fatalf("limit=9999 returned %d; want 500 (the cap)", len(got))
	}

	// key="" spans keys: the 500 returned events carry more than one key.
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.Key] = true
	}
	if len(seen) < 2 {
		t.Fatalf("key='' spanned %d keys; want more than one", len(seen))
	}

	// A named key filters to that key's trail (200 of the 600; 600 clamps to
	// 500, but only 200 exist, so all of them come back).
	got, err = s.SettingEventList(ctx(), "agents.x", 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 200 {
		t.Fatalf("key=agents.x returned %d; want 200 (only that key's trail)", len(got))
	}
	for _, e := range got {
		if e.Key != "agents.x" {
			t.Fatalf("key=agents.x returned a row for %q", e.Key)
		}
	}
}

// TestSettingListIsOrderedByKey is the stable-ordering proof: a list inserted
// out of order comes back sorted by key — the order the UI inherits.
func TestSettingListIsOrderedByKey(t *testing.T) {
	s := openTemp(t)
	for _, k := range []string{"zeta.key", "mid.key", "alpha.key"} {
		if err := s.SettingPut(ctx(), SettingRow{Key: k, Value: k, UpdatedAt: settingAt, UpdatedBy: "tester"},
			putEvent(k, k)); err != nil {
			t.Fatalf("put %q: %v", k, err)
		}
	}
	got, err := s.SettingList(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("list = %d; want 3", len(got))
	}
	want := []string{"alpha.key", "mid.key", "zeta.key"}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("list[%d] = %q; want %q (ordered by key)", i, got[i].Key, k)
		}
	}
}

// TestSettingValueCap is the 4096 net's proof: a value past the cap is refused
// by the CHECK (and a value at the cap is admitted, so the cap is exactly
// 4096, not a rounder number).
func TestSettingValueCap(t *testing.T) {
	s := openTemp(t)
	// 4096 is admitted.
	if err := rawInsertSetting(t, s, "agents.value", strings.Repeat("a", 4096)); err != nil {
		t.Fatalf("a 4096-byte value was refused: %v", err)
	}
	// 4097 is refused by the CHECK.
	if err := rawInsertSetting(t, s, "agents.valueTwo", strings.Repeat("a", 4097)); err == nil {
		t.Fatal("a 4097-byte value was admitted; the CHECK (length(value) <= 4096) should refuse it")
	}
}

// TestSettingEventShadowedByOnlyEnv is CS4's vocabulary proof: the CHECK admits
// only NULL or 'env' for shadowed_by — a named value other than 'env' is
// refused, and 'env' and NULL are admitted.
func TestSettingEventShadowedByOnlyEnv(t *testing.T) {
	s := openTemp(t)
	// A value other than 'env' (or NULL) is refused.
	if err := rawInsertEvent(t, s, "agents.shadowX", "set", "v", 0, "x"); err == nil {
		t.Fatal("shadowed_by='x' was admitted; the CHECK admits only NULL or 'env'")
	}
	// 'env' is admitted.
	if err := rawInsertEvent(t, s, "agents.shadowEnv", "set", "v", 0, "env"); err != nil {
		t.Fatalf("shadowed_by='env' was refused: %v", err)
	}
	// NULL is admitted.
	if err := rawInsertEvent(t, s, "agents.shadowNull", "set", "v", 0, nil); err != nil {
		t.Fatalf("shadowed_by=NULL was refused: %v", err)
	}
}

// --- helpers for this file ---------------------------------------------------

// rawInsertSetting writes a setting row directly, so a test can probe the
// CHECK net without the accessor (value and metadata are valid; only the key
// is under test). It returns the raw error so a CHECK failure is the answer.
func rawInsertSetting(t *testing.T, s *Store, key, value string) error {
	t.Helper()
	_, err := s.w.Exec(`INSERT INTO setting (key, value, updated_at, updated_by)
		VALUES (?, ?, ?, ?)`, key, value, settingAt, "tester")
	return err
}

// rawInsertEvent writes a setting_event row directly, with the given
// shadowed_by (nil → NULL). The row is otherwise a valid "set", so only the
// shadowed_by CHECK is under test.
func rawInsertEvent(t *testing.T, s *Store, key, op, newValue string, sensitive int, shadowed any) error {
	t.Helper()
	_, err := s.w.Exec(`INSERT INTO setting_event (key, op, old_value, new_value, sensitive, shadowed_by, actor, occurred_at)
		VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`, key, op, newValue, sensitive, shadowed, "tester", settingAt)
	return err
}

// assertColumns checks that a table has the named columns — the migration's
// DDL, not a shape guess.
func assertColumns(t *testing.T, s *Store, table string, want ...string) {
	t.Helper()
	have := map[string]bool{}
	rows, err := s.r.QueryContext(ctx(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s: %v", table, err)
	}
	for _, w := range want {
		if !have[w] {
			t.Fatalf("table %s is missing column %s (have %v)", table, w, have)
		}
	}
}

// assertCatalogue checks that one named object of a kind exists in
// sqlite_master — the index and the triggers the migration wrote.
func assertCatalogue(t *testing.T, s *Store, kind, name string) {
	t.Helper()
	var n int
	if err := s.r.QueryRowContext(ctx(), "SELECT count(*) FROM sqlite_master WHERE type = ? AND name = ?", kind, name).
		Scan(&n); err != nil {
		t.Fatalf("read %s %s: %v", kind, name, err)
	}
	if n != 1 {
		t.Fatalf("%s %s found %d times; want 1", kind, name, n)
	}
}

// deref renders a *string for an assertion: nil reads as "(nil)", a value as
// its content — so a test can tell "no value" from "an empty value".
func deref(p *string) string {
	if p == nil {
		return "(nil)"
	}
	return *p
}

// settingRowCount counts a key's setting rows (the upsert did not duplicate).
func settingRowCount(s *Store, key string) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx(), "SELECT count(*) FROM setting WHERE key = ?", key).Scan(&n)
	return n, err
}

// eventCount counts the whole audit trail.
func eventCount(s *Store) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx(), "SELECT count(*) FROM setting_event").Scan(&n)
	return n, err
}

// eventID reads one key's audit row id (the newest, if more than one).
func eventID(t *testing.T, s *Store, key string) int64 {
	t.Helper()
	var id int64
	if err := s.r.QueryRowContext(ctx(),
		"SELECT id FROM setting_event WHERE key = ? ORDER BY id DESC LIMIT 1", key).Scan(&id); err != nil {
		t.Fatalf("read the event id for %q: %v", key, err)
	}
	return id
}

// assertLedgerHas checks that a migration name is in the ledger as applied.
func assertLedgerHas(applied func() ([]AppliedMigration, error), want string) error {
	ledger, err := applied()
	if err != nil {
		return fmt.Errorf("read the ledger: %w", err)
	}
	for _, m := range ledger {
		if m.Name == want && m.Status == "applied" {
			return nil
		}
	}
	return fmt.Errorf("migration %q is not in the ledger as applied", want)
}
