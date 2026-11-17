package store

import (
	"context"
	"strings"
	"testing"
)

// TestTriggerTablesExistAfterUpgrade is migration 0032's ledger half: the
// tables are there, the ledger says so, and BOTH shapes — fresh and walked-
// forward — carry them (the fingerprint test covers equality; this names the
// feature the fingerprint would only notice by accident).
func TestTriggerTablesExistAfterUpgrade(t *testing.T) {
	for name, s := range map[string]*Store{"fresh": openTemp(t), "upgraded": upgradedStore(t)} {
		for _, table := range []string{"trigger_schedule", "trigger_fire"} {
			var got string
			if err := s.w.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&got); err != nil {
				t.Fatalf("%s store lacks %s: %v", name, table, err)
			}
		}
	}
	ledger, err := upgradedStore(t).AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range ledger {
		if m.Name == "0032_trigger_schedule_and_fire" && m.Status == "applied" {
			found = true
		}
	}
	if !found {
		t.Fatal("0032_trigger_schedule_and_fire missing from the ledger")
	}
}

// TestTriggerSchemaChecksEnforceTheContract pins that TR1's schema half and
// TR4's closed outcome set are COLUMN constraints, not prose: the row shapes
// that mean "half a window", "a webhook that also ticks", "a fourth fire
// answer" and "the same instant fired twice" are refused by SQLite itself,
// in addition to the Go validator. Two doors, both shut.
func TestTriggerSchemaChecksEnforceTheContract(t *testing.T) {
	s := upgradedStore(t)
	ctx := context.Background()
	_ = ctx
	w := s.w
	mustFail := func(why, q string, args ...any) {
		t.Helper()
		if _, err := w.Exec(q, args...); err == nil {
			t.Errorf("%s: the row stored — the CHECK is not there", why)
		} else if !strings.Contains(strings.ToLower(err.Error()), "check") &&
			!strings.Contains(strings.ToLower(err.Error()), "unique") &&
			!strings.Contains(strings.ToLower(err.Error()), "constraint") {
			t.Errorf("%s: failed for the wrong reason: %v", why, err)
		}
	}
	mustOK := func(why, q string, args ...any) {
		t.Helper()
		if _, err := w.Exec(q, args...); err != nil {
			t.Errorf("%s: %v", why, err)
		}
	}

	mustOK("a clean schedule row", `INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at) VALUES ('ok','schedule','dev-loop','30 3 * * *','Europe/London','2026-10-27T09:00:00Z')`)
	mustFail("a half window", `INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,window_start,created_at) VALUES ('half','schedule','dev-loop','30 3 * * *','Europe/London','09:00','2026-10-27T09:00:00Z')`)
	mustFail("a webhook that also ticks", `INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at) VALUES ('tick','webhook','dev-loop','30 3 * * *','Europe/London','2026-10-27T09:00:00Z')`)
	mustFail("an unknown kind", `INSERT INTO trigger_schedule (id,kind,blueprint,expr,zone,created_at) VALUES ('k','telegram','dev-loop','','Europe/London','2026-10-27T09:00:00Z')`)
	mustFail("a schedule without a zone", `INSERT INTO trigger_schedule (id,kind,blueprint,expr,created_at) VALUES ('noz','schedule','dev-loop','30 3 * * *','2026-10-27T09:00:00Z')`)

	mustOK("a fired record", `INSERT INTO trigger_fire (schedule_id,instant,outcome,reason,loop_id,at) VALUES ('ok','2026-10-27T03:30:00Z','fired','','loop-1','2026-10-27T03:30:01Z')`)
	mustOK("a could-not-say record", `INSERT INTO trigger_fire (schedule_id,instant,outcome,reason,at) VALUES ('ok','2026-10-26T03:30:00Z','could_not_say','daemon was down','2026-10-27T09:00:00Z')`)
	mustFail("a fourth fire answer", `INSERT INTO trigger_fire (schedule_id,instant,outcome,at) VALUES ('ok','2026-10-25T03:30:00Z','maybe','2026-10-27T09:00:00Z')`)
	mustFail("the same instant twice", `INSERT INTO trigger_fire (schedule_id,instant,outcome,at) VALUES ('ok','2026-10-27T03:30:00Z','skipped','2026-10-27T09:00:00Z')`)
	mustFail("a fire for no schedule", `INSERT INTO trigger_fire (schedule_id,instant,outcome,at) VALUES ('nobody','2026-10-24T03:30:00Z','fired','2026-10-27T09:00:00Z')`)
}
