package store

// continuous_schema_test.go — S27 T2: the teeth of the two scheduler
// migrations and the singletons' inactivity. These tests run SQL directly
// on s.w/s.r ON PURPOSE: they exist to prove what the DATABASE refuses, so
// routing them through Go wrappers would test the wrong layer. The Go
// wrappers' behavior is scheduler_test.go's job.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

var s27Actor = SchedulerActor{Kind: "user", ID: "uid-s27"}

// s27PreIndex returns the cut that makes an OLD-vintage store: every
// migration BEFORE the S27 T2 pair. The cut is by NAME, not by position —
// same-day files from other slices (S61's goal_intake and
// template_revision_system_actor both landed within the hour) move what
// sits at the tail, and a positional cut would quietly start testing the
// wrong vintage. The name guard keeps the cut honest either way.
func s27PreIndex(t *testing.T) int {
	t.Helper()
	all := Migrations()
	idx := -1
	for i, m := range all {
		if strings.Contains(m.Name, "20261105025313_scheduler") {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(all) || !strings.HasSuffix(all[idx+1].Name, "_loop_run_scheduler") {
		t.Fatalf("the _scheduler pair is not ordered as 20261105025313 then _loop_run_scheduler in the %d-migration set — the drill cannot find its vintage", len(all))
	}
	return idx
}

// Test 1 (CN2): nothing that works is on by default, fresh or upgraded.
func TestSchedulerStartsOffByDefault(t *testing.T) {
	fresh := openTemp(t)
	st, err := fresh.SchedulerState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Paused {
		t.Fatalf("a fresh store is enabled=%v paused=%v — CN2: the scheduler ships OFF", st.Enabled, st.Paused)
	}
	if roots, err := fresh.SchedulerRoots(); err != nil || len(roots) != 0 {
		t.Fatalf("fresh scheduler_root rows = %d (%v), want 0 — authority is never laid by default", len(roots), err)
	}

	// The upgrade arm: a pre-S27 store opened forward lands in the SAME
	// shape and the SAME inert state.
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	applyMigrations(t, old.GormDB(), Migrations()[:s27PreIndex(t)])
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	defer upgraded.Close()
	ust, err := upgraded.SchedulerState()
	if err != nil {
		t.Fatal(err)
	}
	if ust.Enabled || ust.Paused {
		t.Fatalf("the upgrade turned the scheduler on (enabled=%v paused=%v) — an upgrade must never start work", ust.Enabled, ust.Paused)
	}
	fpFresh, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	fpUp, err := upgraded.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if !fpFresh.Equal(fpUp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(fpFresh.Diff(fpUp), "\n"))
	}
}

// Test 2: the decision log is append-only AT THE DATABASE, with its
// sentence. (The Go wrappers expose no update/delete — there is nothing to
// call except raw SQL, which is exactly the point.)
func TestSchedulerDecisionIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	id, err := s.AppendDecision(SchedulerDecision{
		SubjectKind: "daemon", Decision: "enabled", Summary: "enabled scheduler",
		ActorKind: "user", ActorID: "uid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE scheduler_decision SET decision = 'settings' WHERE id = ?`, id); err == nil {
		t.Fatal("an UPDATE reached the decision log — the audit is mutable")
	} else if !strings.Contains(err.Error(), "scheduler_decision is append-only") {
		t.Fatalf("the UPDATE died with the wrong sentence: %v", err)
	}
	if _, err := s.w.Exec(`DELETE FROM scheduler_decision WHERE id = ?`, id); err == nil {
		t.Fatal("a DELETE reached the decision log — history can be erased")
	} else if !strings.Contains(err.Error(), "scheduler_decision is append-only") {
		t.Fatalf("the DELETE died with the wrong sentence: %v", err)
	}
}

// Test 3 (CN14): the PRIMARY KEY IS the one-attempt rule.
func TestSchedulerStartPrimaryKeyIsTheOneAttempt(t *testing.T) {
	s := openTemp(t)
	claimed, err := s.ClaimStart("queue", 7, "/repo/one", 1, "2026-11-05T00:00:00.000Z")
	if err != nil || !claimed {
		t.Fatalf("first claim: claimed=%v err=%v", claimed, err)
	}
	claimed, err = s.ClaimStart("queue", 7, "/repo/one", 2, "2026-11-05T00:00:01.000Z")
	if err != nil {
		t.Fatalf("the second claim errored instead of LOSING: %v", err)
	}
	if claimed {
		t.Fatal("the same (kind, id) claimed TWICE — CN14's one attempt is not in the key")
	}
	// roadmap 7 is a different subject and still claims.
	if claimed, err := s.ClaimStart("roadmap", 7, "/repo/one", 3, "2026-11-05T00:00:02.000Z"); err != nil || !claimed {
		t.Fatalf("roadmap 7: claimed=%v err=%v — the key is (kind, id), not id alone", claimed, err)
	}
	rec, found, err := s.StartRecord("queue", 7)
	if err != nil || !found || rec.DecisionID != 1 || rec.Outcome != "" {
		t.Fatalf("StartRecord = %+v found=%v err=%v — the FIRST claim must be the record", rec, found, err)
	}
}

// Test 4: 1,000 identical deferrals are ONE row; a changed reason or a
// `started` always writes.
func TestDecisionDedupe(t *testing.T) {
	s := openTemp(t)
	base := SchedulerDecision{
		ComponentRoot: "/repo/dedupe", SubjectKind: "queue", SubjectID: 11,
		Decision: "deferred", Reason: "capacity", Summary: "deferred queue item 11",
		ActorKind: "scheduler", ActorID: "tick-s27",
	}
	var first int64
	for i := 0; i < 1000; i++ {
		id, err := s.AppendDecision(base)
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if i == 0 {
			first = id
		} else if id != first {
			t.Fatalf("append %d returned id %d, want the held id %d", i, id, first)
		}
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM scheduler_decision`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("1,000 identical deferrals wrote %d rows — the log flooded", n)
	}
	// A changed reason is a new fact.
	base.Reason = "grant-expired"
	if id, err := s.AppendDecision(base); err != nil || id == first {
		t.Fatalf("a changed reason returned id %d (%v), want a new row", id, err)
	}
	// `started` NEVER dedupes — two starts are two facts.
	start := SchedulerDecision{
		ComponentRoot: "/repo/dedupe", SubjectKind: "queue", SubjectID: 11,
		Decision: "started", Summary: "started queue item 11", ActorKind: "scheduler", ActorID: "tick-s27",
	}
	a, err := s.AppendDecision(start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AppendDecision(start)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two identical `started` lines collapsed — only the chatty words may dedupe")
	}
	// The dedupe window is the LAST line: deferred(capacity) → started →
	// deferred(capacity) is two rows, not one.
	third := SchedulerDecision{
		ComponentRoot: "/repo/dedupe", SubjectKind: "queue", SubjectID: 12,
		Decision: "deferred", Reason: "capacity", Summary: "deferred queue item 12", ActorKind: "scheduler", ActorID: "tick-s27",
	}
	if _, err := s.AppendDecision(third); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendDecision(SchedulerDecision{
		ComponentRoot: "/repo/dedupe", SubjectKind: "queue", SubjectID: 12,
		Decision: "started", Summary: "started queue item 12", ActorKind: "scheduler", ActorID: "tick-s27",
	}); err != nil {
		t.Fatal(err)
	}
	if id, err := s.AppendDecision(third); err != nil {
		t.Fatal(err)
	} else {
		var cnt int
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM scheduler_decision WHERE subject_id = 12`).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		if cnt != 3 {
			t.Fatalf("subject 12 has %d rows, want 3 — the dedupe must compare the LAST line only (id %d)", cnt, id)
		}
	}
}

// Test 5: authority and audit are ONE transaction — each fails with the
// other. Injection is SQL-side triggers (the same instrumented-abort
// technique settings_test uses): nothing about the store's own code is
// touched.
func TestDecisionWrittenInTheSameTransaction(t *testing.T) {
	s := openTemp(t)
	// The state write dies → no decision row may survive it.
	if _, err := s.w.Exec(`CREATE TRIGGER zz_s27_boom_state BEFORE UPDATE ON scheduler_state
		BEGIN SELECT RAISE(ABORT, 'injected: state boom') WHERE NEW.paused_reason = 'boom'; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSchedulerState(s27Actor, true, true, "boom"); err == nil {
		t.Fatal("the injected state failure was swallowed")
	}
	dropTrigger := `DROP TRIGGER zz_s27_boom_state`
	if _, err := s.w.Exec(dropTrigger); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.SchedulerState(); st.Enabled || st.Paused {
		t.Fatal("the state moved despite the injected failure")
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM scheduler_decision WHERE decision = 'enabled'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d audit rows for a control write that never happened — an unattributed change in reverse", n)
	}

	// The decision write dies → the authority write must roll back too.
	if _, err := s.w.Exec(`CREATE TRIGGER zz_s27_boom_audit BEFORE INSERT ON scheduler_decision
		BEGIN SELECT RAISE(ABORT, 'injected: audit boom') WHERE NEW.summary = 'enabled scheduler'; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSchedulerState(s27Actor, true, false, ""); err == nil {
		t.Fatal("the injected decision failure was swallowed")
	}
	if _, err := s.w.Exec(`DROP TRIGGER zz_s27_boom_audit`); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.SchedulerState(); st.Enabled {
		t.Fatal("the scheduler ENABLED itself while its audit row was refused — an unattributed change")
	}
	// And the working path writes BOTH.
	if _, err := s.SetSchedulerState(s27Actor, true, false, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := s.SchedulerState()
	last, found, err := s.LastDecision("daemon", 0)
	if err != nil || !found {
		t.Fatalf("daemon decision missing: found=%v err=%v", found, err)
	}
	if !st.Enabled || last.Decision != "enabled" || last.ActorKind != "user" || last.ActorID != "uid-s27" {
		t.Fatalf("state=%+v last=%+v — the working path must write the row AND its attributed audit", st, last)
	}
}

// Test 6: the graph refuses self-edges, cross-repository edges, 2-cycles,
// longer cycles and duplicates — each named.
func TestDependencyRefusals(t *testing.T) {
	s := openTemp(t)
	seedRoadmapComponent(t, s, "alpha", "/repo/alpha")
	seedRoadmapComponent(t, s, "beta", "/repo/beta")
	mk := func(root, title string) int64 {
		res, err := s.w.Exec(roadmapItemSQL, root, title, "ready", "authored", "now", "now")
		if err != nil {
			t.Fatalf("item %s: %v", title, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	a1, a2, a3 := mk("/repo/alpha", "a1"), mk("/repo/alpha", "a2"), mk("/repo/alpha", "a3")
	b1 := mk("/repo/beta", "b1")
	n1, n2, n3 := mk("/repo/alpha", "n1"), mk("/repo/alpha", "n2"), mk("/repo/alpha", "n3")
	q1, q2 := mk("/repo/alpha", "q1"), mk("/repo/alpha", "q2")
	_ = a3

	cases := []struct {
		name          string
		item, depends int64
		want          string
		pre           func()
	}{
		{"self", a1, a1, "cannot depend on itself", nil},
		{"cross-root", a1, b1, "different repositories", nil},
		{"missing", a1, 9999, "does not exist", nil},
		{"direct-reverse-2cycle", a2, a1, "the reverse edge already exists", func() {
			if err := s.AddDependency(s27Actor, a1, a2); err != nil {
				t.Fatalf("edge a1→a2: %v", err)
			}
		}},
		{"duplicate", a1, a2, "already depends", nil},
		{"longer-cycle", n3, n1, "would cycle", func() {
			// n1 → n2 and n2 → n3 already stand, so n3 → n1 would close
			// the ring n1 → n2 → n3 → n1 — three hops, invisible to the
			// 2-cycle trigger.
			if err := s.AddDependency(s27Actor, n1, n2); err != nil {
				t.Fatalf("edge n1→n2: %v", err)
			}
			if err := s.AddDependency(s27Actor, n2, n3); err != nil {
				t.Fatalf("edge n2→n3: %v", err)
			}
		}},
	}
	for _, c := range cases {
		if c.pre != nil {
			c.pre()
		}
		err := s.AddDependency(s27Actor, c.item, c.depends)
		if err == nil {
			t.Fatalf("%s: the edge was ACCEPTED — %s", c.name, c.want)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: refused with the wrong sentence: %v (want %q)", c.name, err, c.want)
		}
	}
	// A legal edge lands, and the reads answer around it.
	a4 := mk("/repo/alpha", "a4")
	if err := s.AddDependency(s27Actor, a4, a1); err != nil {
		t.Fatalf("a4→a1 is legal (nothing under a1 reaches a4): %v", err)
	}
	open, err := s.OpenDependencies(a4)
	if err != nil || len(open) != 1 || open[0].ID != a1 {
		t.Fatalf("OpenDependencies(a4) = %+v (%v)", open, err)
	}
	// done upstreams are not open.
	if _, err := s.w.Exec(`UPDATE roadmap_item SET status = 'done' WHERE id = ?`, a1); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.OpenDependencies(a4); len(open) != 0 {
		t.Fatalf("a done item still blocks: %+v", open)
	}
	// Dependents names the downstream for the cancel sentence.
	if deps, err := s.Dependents(a1); err != nil || len(deps) != 1 || deps[0].ID != a4 {
		t.Fatalf("Dependents(a1) = %+v (%v), want exactly a4", deps, err)
	}
	if gone, err := s.RemoveDependency(a4, a1); err != nil || !gone {
		t.Fatalf("RemoveDependency = %v (%v)", gone, err)
	}
	if gone, err := s.RemoveDependency(a4, a1); err != nil || gone {
		t.Fatalf("removing a gone edge: %v (%v), want false,nil", gone, err)
	}
	// The 2-cycle refusal is the TRIGGER's too, not just Go's: with Go's
	// door walk planted to see nothing, the database still refuses.
	if _, err := s.w.Exec(`INSERT INTO roadmap_dependency (item_id, depends_on_id, created_at, created_by_kind, created_by_id)
		VALUES (?,?,?,?,?)`, q1, q2, "now", "user", "uid"); err != nil {
		t.Fatalf("the raw q1→q2 edge: %v", err)
	}
	if _, err := s.w.Exec(`INSERT INTO roadmap_dependency (item_id, depends_on_id, created_at, created_by_kind, created_by_id)
		VALUES (?,?,?,?,?)`, q2, q1, "now", "user", "uid"); err == nil {
		t.Fatal("a raw 2-cycle INSERT was accepted — the trigger has no teeth")
	} else if !strings.Contains(err.Error(), "the reverse edge already exists") {
		t.Fatalf("raw 2-cycle refused with the wrong sentence: %v", err)
	}
	// And the same-root trigger answers a raw cross-root INSERT too.
	if _, err := s.w.Exec(`INSERT INTO roadmap_dependency (item_id, depends_on_id, created_at, created_by_kind, created_by_id)
		VALUES (?,?,?,?,?)`, a1, b1, "now", "user", "uid"); err == nil || !strings.Contains(err.Error(), "same repository") {
		t.Fatalf("raw cross-root INSERT: %v — want the same-repository sentence", err)
	}
}

// Test 7 (CN14/CN19): the cap vocabulary is EXACTLY grant|none|outward —
// no widening value is even representable, on either item table.
func TestAutonomyCapHasNoWidenerValue(t *testing.T) {
	s := openTemp(t)
	seedRoadmapComponent(t, s, "cap", "/repo/cap")
	for _, bad := range []string{"merge", "all", "GRANT", "grant ", ""} {
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, provenance, created_at, updated_at, autonomy_cap)
			VALUES (?,?,?,?,?,?,?)`, "/repo/cap", "t", "idea", "authored", "now", "now", bad); err == nil {
			t.Fatalf("roadmap cap %q was ACCEPTED — a widener is representable", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("roadmap cap %q refused for the wrong reason: %v", bad, err)
		}
	}
	seedQueueComponent(t, s, "capq", "/repo/cap")
	for _, bad := range []string{"merge", "all"} {
		if _, err := s.w.Exec(`INSERT INTO queue_item
			(component_root, position, status, blueprint, goal, workspace_root, created_at, updated_at, autonomy_cap)
			VALUES (?,?,?,?,?,?,?,?,?)`, "/repo/cap", 1, "queued", "bp", "g", "/ws", "now", "now", bad); err == nil {
			t.Fatalf("queue cap %q was ACCEPTED — a widener is representable", bad)
		}
	}
	for _, ok := range []string{"grant", "none", "outward"} {
		if _, err := s.w.Exec(`INSERT INTO roadmap_item
			(component_root, title, status, provenance, created_at, updated_at, autonomy_cap)
			VALUES (?,?,?,?,?,?,?)`, "/repo/cap", ok, "idea", "authored", "now", "now", ok); err != nil {
			t.Errorf("the legal cap %q was refused: %v", ok, err)
		}
	}
	// The DEFAULT: an item written without the column reads grant — every
	// pre-S27 row lives under its S56 grant unchanged.
	if _, err := s.w.Exec(roadmapItemSQL, "/repo/cap", "legacy", "idea", "authored", "now", "now"); err != nil {
		t.Fatal(err)
	}
	var cap string
	if err := s.r.QueryRow(`SELECT autonomy_cap FROM roadmap_item WHERE title = 'legacy'`).Scan(&cap); err != nil || cap != "grant" {
		t.Fatalf("the default cap = %q (%v), want grant", cap, err)
	}
}

// Test 8 (the 0019 lesson, told twice): the rebuild keeps EVERY row, every
// index BY NAME, and the whole database stays FK-consistent.
func TestLoopRunRebuildKeepsEveryRow(t *testing.T) {
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatal(err)
	}
	applyMigrations(t, old.GormDB(), Migrations()[:s27PreIndex(t)])
	// A population with teeth: three kinds, two run states, NULL columns,
	// a second run, and a row the CASCADE owns.
	mk := func(id, slug, startedBy, triggerID string) {
		if _, err := old.w.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at,started_by,trigger_id)
			VALUES (?,?,'t','g','/r','/r','standard','preflight','running','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',?,?)`,
			id, slug, startedBy, triggerID); err != nil {
			t.Fatalf("seed loop %s: %v", slug, err)
		}
	}
	mk("l-a", "sla", "user", "")
	mk("l-b", "slb", "trigger", "tr-9")
	mk("l-c", "slc", "event", "tr-e")
	ins := `INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,goal_id,started_at,ended_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	if _, err := old.w.Exec(ins, "l-a", 1, "bp", 3, "content-a", "done", "user", "", "g1", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.w.Exec(ins, "l-a", 2, "bp", 4, "content-a2", "running", "trigger", "tr-1", "g1", "2026-01-03T00:00:00Z", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := old.w.Exec(ins, "l-b", 1, "bp", 1, "content-b", "failed", "trigger", "tr-9", nil, "2026-01-01T00:00:00Z", "2026-01-04T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.w.Exec(ins, "l-c", 1, "bp", 2, "content-c", "cancelled", "event", "tr-e", "g9", "2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.w.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,kickoff_message_id,task_id,started_at,ended_at)
		VALUES ('l-c',2,'bp',2,'content-c2','done','agent','agent-x',42,'task-7','2026-01-06T00:00:00Z','2026-01-07T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	before := loopRunFold(t, old.GormDB())
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	defer s.Close()
	after := loopRunFold(t, s.GormDB())
	if before != after {
		t.Fatalf("the rebuild changed the ledger:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// The index exists BY NAME.
	var idx string
	if err := s.r.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_loop_run_state'`).Scan(&idx); err != nil {
		t.Fatalf("idx_loop_run_state did not survive the rebuild: %v", err)
	}
	if !strings.Contains(idx, "loop_run (loop_id, state)") {
		t.Fatalf("the index is a different thing: %s", idx)
	}
	// FK is clean database-wide.
	var bad int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check()`).Scan(&bad); err != nil || bad != 0 {
		t.Fatalf("foreign_key_check after the rebuild: %d bad (%v)", bad, err)
	}
	// A CASCADE still rides the rebuilt FK.
	if _, err := s.w.Exec(`DELETE FROM loop WHERE id = 'l-b'`); err != nil {
		t.Fatal(err)
	}
	var runs int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM loop_run WHERE loop_id='l-b'`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("CASCADE missed: %d loop_run rows for a deleted loop (%v)", runs, err)
	}
}

func loopRunFold(t *testing.T, g *gorm.DB) string {
	t.Helper()
	rows, err := g.Raw(`SELECT loop_id, run_seq, blueprint, blueprint_version, content, COALESCE(runtime_json,'~'),
		state, started_by_kind, started_by_id, COALESCE(kickoff_message_id,-1), COALESCE(goal_id,'~'),
		COALESCE(goal_version,-1), COALESCE(task_id,'~'), started_at, COALESCE(ended_at,'~')
		FROM loop_run ORDER BY loop_id, run_seq`).Rows()
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var loopID, bp, content, runtime, state, kind, byID, goalID, taskID, started, ended string
		var seq, version, kickoff, goalVer int
		if err := rows.Scan(&loopID, &seq, &bp, &version, &content, &runtime, &state, &kind, &byID,
			&kickoff, &goalID, &goalVer, &taskID, &started, &ended); err != nil {
			t.Fatalf("fold scan: %v", err)
		}
		fmt.Fprintf(&b, "%s|%d|%s|%d|%s|%s|%s|%s|%s|%d|%s|%d|%s|%s|%s\n",
			loopID, seq, bp, version, content, runtime, state, kind, byID, kickoff, goalID, goalVer, taskID, started, ended)
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("the fold covered %d rows, want the 5 seeded — the census is the claim", n)
	}
	return b.String()
}

// Test 9: the CHECK admits the scheduler word; junk still dies.
func TestLoopRunAdmitsSchedulerAndStillRefusesJunk(t *testing.T) {
	s := openTemp(t)
	l, err := s.CreatePinnedLoop(NewLoop{Slug: "sched-word", ComponentRoot: "/repo/sw", Title: "sched-word",
		StartedBy: "scheduler", TriggerID: "loop:sw"}, "bp-s", 1, "raw bytes", `{"route":""}`)
	if err != nil {
		t.Fatalf("pin under the scheduler word: %v", err)
	}
	run, err := s.CurrentRun(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.StartedByKind != "scheduler" {
		t.Fatalf("run 1 kind = %q, want scheduler — CN15's word must survive", run.StartedByKind)
	}
	// BeginRun's Go admission matches the CHECK — on an IDLE loop: end
	// run 1 and close the loop first, or the active-loop claim refuses
	// for a reason that has nothing to do with the word.
	if err := s.EndRun(l.ID, 1, "done", `{"route":""}`); err != nil {
		t.Fatal(err)
	}
	// The engine's own step parks the loop idle between runs; here that
	// transition is the one line it is, straight on the row.
	if _, err := s.w.Exec(`UPDATE loop SET status='idle' WHERE id = ?`, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRun(l.ID, NewRun{Blueprint: "bp-s", Content: "again", StartedByKind: "scheduler", StartedByID: "sched-1"}, `{"route":""}`); err != nil {
		t.Fatalf("BeginRun refused the scheduler word: %v", err)
	}
	ins := `INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,state,started_by_kind,started_by_id,started_at)
		VALUES (?,3,'bp',1,'x','running',?,'',?)`
	for _, bad := range []string{"cron", "Scheduler", "system", ""} {
		if _, err := s.w.Exec(ins, l.ID, bad, "2026-01-01T00:00:00Z"); err == nil {
			t.Fatalf("kind %q was ACCEPTED — the vocabulary widened past scheduler", bad)
		} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
			t.Errorf("kind %q refused for the wrong reason: %v", bad, err)
		}
	}
}

// Test 10 (CN2): no seed set knows these tables exist. Authority that
// ships as a default seed row is the failure; the source of truth for the
// registry is internal/seed's own source.
func TestSchedulerRowsAreNotSeeded(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	seedDir := filepath.Join(root, "internal", "seed")
	var offenders []string
	err = filepath.WalkDir(seedDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, table := range []string{"scheduler_state", "scheduler_root", "scheduler_start", "scheduler_decision"} {
			if strings.Contains(string(blob), table) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Fatalf("the seed plane names the scheduler's tables (%v) — CN2: authority is a row a person lays, never a shipped default", offenders)
	}
}
