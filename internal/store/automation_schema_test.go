package store

// automation_schema_test.go — S40 T1: the shape 0039_automation gives the
// database, and nothing else. Every claim here is a row attempted: CHECKs are
// proven by the insert that fails and the insert that succeeds, the manual-only
// rule by a loop task refused and a manual task kept, the fire ledger's claim
// state by the UNIQUE that refuses a second claim, and the loop_run rebuild by
// rows that ride through the upgrade with started_by_kind 'event' accepted
// afterwards (S40 T0-Q2/Q7 measured the before-state; this is the after).
// S20's tables are asserted UNTOUCHED — same columns before the migration as
// after — because AU7 put these tables beside them, not inside them.

import (
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// seedTaskForAutomation inserts one task row directly (the store functions
// belong to S38's tasks files; T1's Do-NT keeps Go store functions out of
// this task). manual=1 is a person's task — and per 0036 it carries NO loop
// (its (NULL, NULL, taskid) row is why the loop-key index is PARTIAL);
// manual=0 is the engine board's, whose (component_root, loop_slug) FK needs
// the loop row to exist.
func seedTaskForAutomation(t *testing.T, g *gorm.DB, id string, manual int) {
	t.Helper()
	if manual == 0 {
		if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
			VALUES ('l1','l1','t','g','/r','/r','standard','preflight','running','2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z')`).Error; err != nil && !strings.Contains(err.Error(), "UNIQUE") {
			t.Fatalf("seed loop for task %s: %v", id, err)
		}
		if err := g.Exec(`INSERT INTO task (id,loop_slug,component_root,taskid,ord,title,tech,engineer,status,manual,created_at,updated_at)
			VALUES (?, 'l1','/r','1',1,'t','x','e','pending',0,'2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z')`, id).Error; err != nil {
			t.Fatalf("seed loop-task %s: %v", id, err)
		}
		return
	}
	if err := g.Exec(`INSERT INTO task (id,taskid,ord,title,tech,engineer,status,manual,created_at,updated_at)
		VALUES (?,'m1',1,'m','x','e','pending',1,'2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z')`, id).Error; err != nil {
		t.Fatalf("seed manual task %s: %v", id, err)
	}
}

func tryInsertRawEvent(s *Store, event, kind, subjectID string) error {
	return s.gw.Exec(`INSERT INTO automation_event (event,subject_kind,subject_id,created_at)
		VALUES (?,?,?,'2026-10-28T06:00:00.000Z')`, event, kind, subjectID).Error
}

func TestTheAutomationMigrationUpgradesAPriorStoreWithRowsIntact(t *testing.T) {
	// A database built at the PREVIOUS migration, with a loop row and a run
	// row planted, is the exact shape a live install holds; Open() is the
	// exact path the new binary takes. The rows must ride through with the
	// new columns defaulted — automation_depth 0 says "nobody cascaded this".
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	func() {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		applyMigrations(t, g, Migrations()[:38]) // the S39 vintage — LITERAL, the way loop_run_test pins [:36]; S22 T1 measured that `[:len(Migrations())-1]` silently moved the vintage when 0040 arrived
		if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
			VALUES ('l1','s1','t','g','/r','/r','standard','preflight','running','2026-10-27T00:00:00.000Z','2026-10-27T00:00:00.000Z')`).Error; err != nil {
			t.Fatalf("seed loop at the prior vintage: %v", err)
		}
		if err := g.Exec(`INSERT INTO loop_run (loop_id,run_seq,blueprint,blueprint_version,content,runtime_json,state,started_by_kind,started_at)
			VALUES ('l1',1,'dev-loop',1,'{}','{}','running','agent','2026-10-27T00:00:00.000Z')`).Error; err != nil {
			t.Fatalf("seed loop_run at the prior vintage: %v", err)
		}
	}()

	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the prior-vintage store: %v", err)
	}
	defer s.Close()

	var startedBy string
	var depth int
	if err := s.gw.Raw(`SELECT started_by, automation_depth FROM loop WHERE id='l1'`).Row().Scan(&startedBy, &depth); err != nil {
		t.Fatalf("read the upgraded loop row: %v", err)
	}
	if startedBy != "" || depth != 0 {
		t.Errorf("the old loop row = (started_by %q, depth %d), want (\"\", 0) — defaults invented provenance or a cascade", startedBy, depth)
	}
	var kind string
	if err := s.gw.Raw(`SELECT started_by_kind FROM loop_run WHERE loop_id='l1' AND run_seq=1`).Row().Scan(&kind); err != nil {
		t.Fatalf("the planted run row did not survive the rebuild: %v", err)
	}
	if kind != "agent" {
		t.Errorf("the rebuilt loop_run kept started_by_kind %q, want the planted \"agent\"", kind)
	}
	for _, tb := range []string{"automation_event", "automation_cursor", "event_trigger", "event_trigger_fire"} {
		countTable(t, s.gw, tb) // fatals if the table is absent
	}
	if got := countTable(t, s.gw, "automation_event"); got != 0 {
		t.Errorf("a fresh outbox holds %d rows, want 0", got)
	}
	applied, err := s.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(Migrations()) {
		t.Errorf("the ledger holds %d rows after the upgrade, want one per migration (%d)", len(applied), len(Migrations()))
	}
}

func TestAnAutomationEventOutsideTheClosedSetCannotBeInserted(t *testing.T) {
	s := openTemp(t)
	seedTaskForAutomation(t, s.gw, "mt1", 1)
	if err := tryInsertRawEvent(s, "goal.rejected", "goal", "g1"); err == nil {
		t.Errorf("goal.rejected was inserted — the outbox vocabulary is wider than AU3's closed four")
	} else if !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("goal.rejected failed for the wrong reason: %v", err)
	}
	for _, ok := range [][3]string{
		{"goal.approved", "goal", "g1"},
		{"task.created", "task", "mt1"},
		{"task.approved", "task", "mt1"},
		{"task.completed", "task", "mt1"},
	} {
		if err := tryInsertRawEvent(s, ok[0], ok[1], ok[2]); err != nil {
			t.Errorf("the allowed event %s (%s) was refused: %v", ok[0], ok[1], err)
		}
	}
}

func TestAnAutomationEventNeedsItsOwnSubjectKind(t *testing.T) {
	s := openTemp(t)
	seedTaskForAutomation(t, s.gw, "mt1", 1)
	if err := tryInsertRawEvent(s, "goal.approved", "task", "mt1"); err == nil {
		t.Errorf("a goal.approved naming a TASK was inserted — the event/subject cross-check is missing")
	}
	if err := tryInsertRawEvent(s, "task.created", "goal", "g1"); err == nil {
		t.Errorf("a task.created naming a GOAL was inserted — the event/subject cross-check is missing")
	}
}

func TestTheManualOnlyTriggerRefusesALoopTaskAndAllowsAManualOne(t *testing.T) {
	// AU3's rule in the database itself. Both tasks go in through raw INSERT
	// with the names T0-Q3 recorded (surrogate id, manual column); the
	// trigger's WHEN text is design.md's verbatim — COALESCE OUTSIDE the
	// subquery, so a GHOST subject_id also aborts (T0-Q7's measured trap).
	s := openTemp(t)
	seedTaskForAutomation(t, s.gw, "te1", 0) // the engine board's
	seedTaskForAutomation(t, s.gw, "mt1", 1) // a person's
	before := countTable(t, s.gw, "automation_event")

	if err := tryInsertRawEvent(s, "task.created", "task", "te1"); err == nil || !strings.Contains(err.Error(), "manual tasks only") {
		t.Errorf("an event for a LOOP task returned %v, want the manual-only refusal", err)
	}
	if err := tryInsertRawEvent(s, "task.created", "task", "ghost"); err == nil || !strings.Contains(err.Error(), "manual tasks only") {
		t.Errorf("an event for a GHOST task id returned %v, want the manual-only refusal (COALESCE stays outside the subquery)", err)
	}
	if err := tryInsertRawEvent(s, "task.created", "task", "mt1"); err != nil {
		t.Errorf("an event for the MANUAL task was refused: %v", err)
	}
	if got := countTable(t, s.gw, "automation_event") - before; got != 1 {
		t.Errorf("the outbox grew by %d rows, want exactly 1 (the manual task's)", got)
	}
}

// triggerRow is a valid event_trigger row; mutate one field per probe.
func insertTriggerRow(s *Store, name, source, matchJSON, blueprint string, runsPerHour int, cost *float64, byKind string) error {
	return s.gw.Exec(`INSERT INTO event_trigger (id,name,source,match_json,blueprint,max_runs_per_hour,max_cost_per_day_usd,created_by_kind,created_by_id,created_at,updated_at,from_outbox_id)
		VALUES (?,?,'event',?,?,?,?,'user','op','2026-10-28T06:00:00.000Z','2026-10-28T06:00:00.000Z',0)`,
		"trg-"+name, name, matchJSON, blueprint, runsPerHour, cost).Error
}

func TestTheTriggerTableEnforcesItsShape(t *testing.T) {
	s := openTemp(t)
	cost := 5.0
	if err := insertTriggerRow(s, "ok", "event", `{"event":"goal.approved"}`, "dev-loop", 4, &cost, "user"); err != nil {
		t.Fatalf("the valid trigger row was refused: %v", err)
	}
	probes := []struct {
		what string
		err  error
	}{
		{"created_by_kind agent", s.gw.Exec(`INSERT INTO event_trigger (id,name,source,match_json,blueprint,max_runs_per_hour,created_by_kind,created_by_id,created_at,updated_at,from_outbox_id)
			VALUES ('trg-a','a','event','{}','dev-loop',4,'agent','op','t','t',0)`).Error},
		{"source cron", s.gw.Exec(`INSERT INTO event_trigger (id,name,source,match_json,blueprint,max_runs_per_hour,created_by_kind,created_by_id,created_at,updated_at,from_outbox_id)
			VALUES ('trg-c','c','cron','{}','dev-loop',4,'user','op','t','t',0)`).Error},
		{"match_json invalid", s.gw.Exec(`INSERT INTO event_trigger (id,name,source,match_json,blueprint,max_runs_per_hour,created_by_kind,created_by_id,created_at,updated_at,from_outbox_id)
			VALUES ('trg-m','m','event','nope','dev-loop',4,'user','op','t','t',0)`).Error},
		{"empty blueprint", s.gw.Exec(`INSERT INTO event_trigger (id,name,source,match_json,blueprint,max_runs_per_hour,created_by_kind,created_by_id,created_at,updated_at,from_outbox_id)
			VALUES ('trg-b','b','event','{}','',4,'user','op','t','t',0)`).Error},
	}
	for _, p := range probes {
		if p.err == nil {
			t.Errorf("a trigger with %s was inserted — a CHECK is missing", p.what)
		}
	}
	// max_runs_per_hour's bounds and the negative cost, through the helper.
	if err := insertTriggerRow(s, "zero", "event", "{}", "dev-loop", 0, &cost, "user"); err == nil {
		t.Errorf("max_runs_per_hour=0 was inserted — the 1..1000 CHECK is missing")
	}
	if err := insertTriggerRow(s, "many", "event", "{}", "dev-loop", 1001, &cost, "user"); err == nil {
		t.Errorf("max_runs_per_hour=1001 was inserted — the 1..1000 CHECK is missing")
	}
	neg := -1.0
	if err := insertTriggerRow(s, "neg", "event", "{}", "dev-loop", 4, &neg, "user"); err == nil {
		t.Errorf("a negative max_cost_per_day_usd was inserted — the >= 0 CHECK is missing")
	}
	if err := insertTriggerRow(s, "ok", "event", "{}", "dev-loop", 4, nil, "user"); err == nil {
		t.Errorf("a duplicate trigger name was inserted — the UNIQUE(name) is missing")
	}
	if err := insertTriggerRow(s, "nulls", "event", "{}", "dev-loop", 4, nil, "user"); err != nil {
		t.Errorf("a trigger with no cost cap (NULL) was refused — AU11 leaves the cap optional: %v", err)
	}
}

func insertFireRow(s *Store, trig string, outbox int, outcome, costNote *string) error {
	note := "" // NOT NULL DEFAULT '' — an explicit NULL would bypass the default
	if costNote != nil {
		note = *costNote
	}
	return s.gw.Exec(`INSERT INTO event_trigger_fire (trigger_id,outbox_id,subject_kind,subject_id,depth,outcome,claimed_at,cost_note)
		VALUES (?,?, 'task','mt1',1,?,'2026-10-28T06:00:00.000Z',?)`, trig, outbox, outcome, note).Error
}

func TestATriggerFireIsUniquePerTriggerAndOutboxRow(t *testing.T) {
	s := openTemp(t)
	// The claim: outcome NULL is the claimed-undecided state — insertable.
	if err := insertFireRow(s, "trg1", 10, nil, nil); err != nil {
		t.Fatalf("the claimed (NULL-outcome) fire row was refused: %v", err)
	}
	if err := insertFireRow(s, "trg1", 10, nil, nil); err == nil {
		t.Errorf("a second claim of the same (trigger, outbox) was inserted — AU8's claim-before-start rests on this UNIQUE")
	}
	started := "started"
	if err := insertFireRow(s, "trg1", 11, &started, nil); err != nil {
		t.Errorf("a decided (started) fire row was refused: %v", err)
	}
	retried := "retried"
	if err := insertFireRow(s, "trg1", 12, &retried, nil); err == nil {
		t.Errorf("outcome 'retried' was inserted — the five-word AU12 vocabulary gained a sixth")
	}
	badNote := "x"
	if err := insertFireRow(s, "trg1", 13, &started, &badNote); err == nil {
		t.Errorf("cost_note 'x' was inserted — the three-state cost CHECK is missing")
	}
	// A DIFFERENT trigger may claim the same outbox row: the pair is the key.
	if err := insertFireRow(s, "trg2", 10, nil, nil); err != nil {
		t.Errorf("a second trigger claiming the same outbox row was refused: %v", err)
	}
}

func TestTheEventTablesSitBesideS20sAndTouchNone(t *testing.T) {
	// AU7: beside, not inside. S20's two tables keep the exact columns they
	// had at the prior vintage; loop gains exactly automation_depth; and
	// chat_message gained ONLY source_ref over what S39 shipped.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	before := map[string][]string{}
	priorLoopCols := []string{}
	chatColsBefore := []string{}
	func() {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		applyMigrations(t, g, Migrations()[:38]) // the S39 vintage — LITERAL, the way loop_run_test pins [:36]; S22 T1 measured that `[:len(Migrations())-1]` silently moved the vintage when 0040 arrived
		for _, tb := range []string{"trigger_schedule", "trigger_fire"} {
			rows, err := g.Raw(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, tb).Rows()
			if err != nil {
				t.Fatalf("read %s columns at the prior vintage: %v", tb, err)
			}
			defer rows.Close()
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				before[tb] = append(before[tb], n)
			}
		}
		for _, q := range []struct {
			tb string
			to *[]string
		}{{"loop", &priorLoopCols}, {"chat_message", &chatColsBefore}} {
			rows, err := g.Raw(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, q.tb).Rows()
			if err != nil {
				t.Fatalf("read %s columns at the prior vintage: %v", q.tb, err)
			}
			defer rows.Close()
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				*q.to = append(*q.to, n)
			}
		}
	}()

	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the prior-vintage store: %v", err)
	}
	defer s.Close()

	// Amended 2026-11-03 by S49 T1, whose migration 20261103102501_
	// automation_management ALTERs trigger_schedule ON PURPOSE (AN3's creator
	// columns, AN6's archive columns — the ALTER-ADD-COLUMN pattern the S39
	// vintage itself used on loop/chat_message). The guard keeps all its
	// teeth: the added set is exactly the four named columns, appended in
	// order, and trigger_fire — whose rows the fire view reads — is still
	// byte-frozen. An unexpected column appearing here is still a failure.
	s49Added := []string{"created_by_kind", "created_by_id", "archived_at", "archived_reason"}
	for _, tb := range []string{"trigger_schedule", "trigger_fire"} {
		got := tableColumns(t, s, tb)
		want := before[tb]
		if tb == "trigger_schedule" {
			want = append(append([]string{}, want...), s49Added...)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s changed across 0039+00S49: before %v, after %v — S20's tables take only the columns S49 T1 named", tb, before[tb], got)
		}
	}
	countTable(t, s.gw, "event_trigger")
	countTable(t, s.gw, "event_trigger_fire")
	gained := map[string]bool{}
	after := tableColumns(t, s, "loop")
	if len(after) != len(priorLoopCols)+1 {
		t.Errorf("loop gained %d columns (%v over %v), want exactly one", len(after)-len(priorLoopCols), minus(after, priorLoopCols), priorLoopCols)
	}
	for _, c := range minus(after, priorLoopCols) {
		gained[c] = true
	}
	if !gained["automation_depth"] {
		t.Errorf("loop's new column set = %v, want automation_depth among them", minus(after, priorLoopCols))
	}
	if gained["started_by"] || gained["trigger_id"] {
		t.Errorf("0039 re-added S20's provenance columns — they arrive in 0033 and live there")
	}
	chatAfter := tableColumns(t, s, "chat_message")
	// Over the prior (S39) vintage chat_message gains 0039's source_ref and
	// — since S68 (B6) — the chosen-skills receipt column, and — since S69
	// (AT2) — the attachment receipt column. All are additive
	// ALTERs; nothing else may have crept onto the thread table.
	if got := minus(chatAfter, chatColsBefore); len(got) != 3 || got[0] != "source_ref" || got[1] != "skills" || got[2] != "attachments" {
		t.Errorf("chat_message gained %v over the prior vintage, want exactly [source_ref skills attachments]", got)
	}
}

// minus returns the members of a absent from b, order preserved.
func minus(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	return out
}
