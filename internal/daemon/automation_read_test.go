package daemon

// automation_read_test.go — S49 T3's daemon half of the unified READS: the
// ten socket methods against a real store carrying both families. The load-
// bearing pins: the list covers BOTH tables and hides archived by default
// (AN6); automation.fires answers exactly what the T1 view answers, which is
// the union of the two fire tables with the NULL-claim rows gone (AN5's
// differential); the cursor pages newest-first without re-reading a page;
// reads answer unknown ids with near-name refusals; and NOTHING any of them
// do writes (the row-count snapshot). V13's absence is pinned store-side.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autoActor is the person the fixtures sign with (AU1's only writer kind).
var autoActor = store.Actor{Kind: "user", ID: "operator"}

// automationEnv: a real store (fixed clock so CreatedAt strings are stable),
// a wired daemon over it (initConn done), and the seeded world —
//
//	sch-morning  schedule 0 7 * * * Europe/Lisbon   — fired + skipped
//	whk-deploy   webhook  secret AEON_T3_SECRET     — never fires on a clock
//	evt-bugs     event    task.created task_type=bug — started + limited + a live claim
//	evt-old      event, archived (archived_at set)  — invisible by default
func automationEnv(t *testing.T) (*store.Store, *Daemon, string, store.Trigger) {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" },
		Clock: func() time.Time { return time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws := t.TempDir()
	ctx := context.Background()
	pc, err := s.CreateComponent(store.NewComponent{Name: "auto", Slug: "auto", Root: ws})
	if err != nil || !pc.OK() {
		t.Fatal(err)
	}
	morning, err := s.CreateLoop(store.NewLoop{Slug: "morning-1", ComponentRoot: ws, Goal: "morning sweep", StartedBy: "trigger", TriggerID: "sch-morning"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "sch-morning", Kind: "schedule", Blueprint: "bp-morning", Workspace: ws, Goal: "morning sweep",
		Expr: "0 7 * * *", Zone: "Europe/Lisbon", CreatedAt: "2026-10-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTriggerSchedule(ctx, store.TriggerScheduleRow{
		ID: "whk-deploy", Kind: "webhook", Blueprint: "bp-deploy", Workspace: ws, Goal: "deploy the release",
		SecretEnv: "AEON_T3_SECRET", CreatedAt: "2026-10-02T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Two fires ride S20's table: one started a loop, one is outside a
	// window. Their words are the CHECK's (fired / did_not_fire) — the
	// view renames them.
	if err := s.RecordTriggerFire(ctx, store.TriggerFireRow{ScheduleID: "sch-morning", Instant: "2026-11-02T07:00:00Z", Outcome: "fired", LoopID: morning.ID, At: "2026-11-02T07:00:01Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTriggerFire(ctx, store.TriggerFireRow{ScheduleID: "sch-morning", Instant: "2026-11-01T07:00:00Z", Outcome: "did_not_fire", Reason: "outside window", At: "2026-11-01T07:00:01Z"}); err != nil {
		t.Fatal(err)
	}
	tr, err := s.CreateEventTrigger(autoActor, store.NewEventTrigger{
		Name: "manual task squad", Blueprint: "bp-bugfix", Kickoff: "fix it",
		Match: store.Match{Event: "task.created", TaskType: []string{"bug"}}, MaxRunsPerHour: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	buggy, err := s.CreateLoop(store.NewLoop{Slug: "bug-run-1", ComponentRoot: ws, Goal: "fix", StartedBy: "event", TriggerID: tr.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The outbox rows ride the DOMAIN writes (AU4's own emitters): two
	// manual bug tasks the trigger's match admits and one manual feature
	// task it does not. AU3's DB trigger means a hand-written task.created
	// row would need the task row anyway — the honest fixture writes tasks.
	var taskIDs []string
	for i, tt := range []string{"bug", "feature", "bug"} {
		task, r, err := s.CreateManualTask(store.NewManualTask{
			ComponentID: pc.Component.ID, Title: fmt.Sprintf("manual task %d", i+1), Type: tt, By: autoActor,
		})
		if err != nil || r != nil {
			t.Fatalf("manual task %d: refusal=%v err=%v", i+1, r, err)
		}
		taskIDs = append(taskIDs, fmt.Sprint(task.ID))
	}
	// Event fires: one decided (started), one mid-air claim. The claim
	// (outcome NULL) must be ABSENT from the unified log (AN5: an answer
	// not yet given is not an outcome).
	db, err := s.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RecordEventFire(store.NewEventFire{TriggerID: tr.ID, OutboxID: 1, SubjectKind: "task", SubjectID: taskIDs[0]},
		store.EventFireDecision{Outcome: "started", LoopID: buggy.ID, LoopSlug: "bug-run-1"}, time.Date(2026, 11, 2, 8, 0, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_trigger_fire (trigger_id,outbox_id,source,subject_kind,subject_id,depth,claimed_at)
		VALUES (?,3,'event','task',?,0,'2026-11-03T09:01:00.000Z')`, tr.ID, taskIDs[2]); err != nil {
		t.Fatal(err)
	}
	// evt-old: archived — AN6 wants it invisible to lists and health by
	// default, reachable only by the explicit archived filter.
	old, err := s.CreateEventTrigger(autoActor, store.NewEventTrigger{
		Name: "retired", Blueprint: "bp-old", Kickoff: "gone",
		Match: store.Match{Event: "goal.approved"}, MaxRunsPerHour: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_trigger SET enabled = 0, archived_at = '2026-11-01T00:00:00Z', archived_reason = 'retired by the person' WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	// The audit: two changes, newest first expected.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AutomationChangeInsertTx(tx, store.AutomationChange{Source: "schedule", TriggerID: "sch-morning", Op: "create", Summary: "created the morning sweep", ActorKind: "user", ActorID: "operator", OccurredAt: "2026-10-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AutomationChangeInsertTx(tx, store.AutomationChange{Source: "event", TriggerID: tr.ID, Op: "update", Summary: "raised the hourly cap to 4", ActorKind: "user", ActorID: "operator", OccurredAt: "2026-10-05T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	d, _ := newTestDaemonWithStore(t, s)
	return s, d, ws, tr
}

// autoCall dispatches over the REAL socket table — every test below walks
// the same door the UI and CLI walk, not an internal function.
func autoCall(t *testing.T, d *Daemon, m protocol.MethodName, params string) (any, error) {
	t.Helper()
	return d.Table.Dispatch(context.Background(), "c1", m, json.RawMessage(params))
}

// auto answers with the typed result, failing on any error or shape drift.
func auto[T any](t *testing.T, d *Daemon, m protocol.MethodName, params string) T {
	t.Helper()
	res, err := autoCall(t, d, m, params)
	if err != nil {
		t.Fatalf("%s %s: %v", m, params, err)
	}
	v, ok := res.(T)
	if !ok {
		t.Fatalf("%s result %T, want %T", m, res, *new(T))
	}
	return v
}

// autoCodes decodes a result to its wire JSON and returns it — the canary
// check reads bytes, not fields.
func autoJSON(t *testing.T, res any) string {
	t.Helper()
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// V13 store-side: a storeless daemon never registers the ten (the S58 inbox
// pattern — absent, not refusing).
func TestAutomationReadsAbsentWithoutAStore(t *testing.T) {
	d, _ := newTestDaemon(t)
	names := map[protocol.MethodName]bool{
		protocol.MethodAutomationList: true, protocol.MethodAutomationShow: true,
		protocol.MethodAutomationFires: true, protocol.MethodAutomationRuns: true,
		protocol.MethodAutomationHealth: true, protocol.MethodAutomationVocabulary: true,
		protocol.MethodAutomationPreview: true, protocol.MethodAutomationTest: true,
		protocol.MethodAutomationDryRun: true, protocol.MethodAutomationChanges: true,
	}
	for _, m := range d.Table.PublicMethods() {
		if names[m] {
			t.Fatalf("%s is registered on a storeless daemon (V13: ABSENT, not refusing)", m)
		}
	}
}

// AN2/AN6: the list covers both families, hides the archived by default,
// and reaches it ONLY through the explicit filter.
func TestAutomationListCoversBothFamiliesAndHidesArchived(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	got := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{}`)
	if len(got.Automations) != 3 {
		t.Fatalf("live automations = %d (%s), want 3 — sch-morning, whk-deploy, evt-bugs", len(got.Automations), autoJSON(t, got))
	}
	sources := map[string]string{}
	for _, v := range got.Automations {
		sources[v.Source] = v.ID
		if v.Archived {
			t.Errorf("%s/%s listed as archived while archived is excluded (AN6)", v.Source, v.ID)
		}
		if v.Source == "event" && v.ID != tr.ID {
			t.Errorf("event row %q, want %s", v.ID, tr.ID)
		}
	}
	if sources["schedule"] != "sch-morning" || sources["webhook"] != "whk-deploy" || sources["event"] == "" {
		t.Errorf("sources = %v — the list must cover BOTH trigger families", sources)
	}
	only := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"archived":"only"}`)
	if len(only.Automations) != 1 || only.Automations[0].ArchivedReason != "retired by the person" {
		t.Fatalf("archived-only = %s", autoJSON(t, only))
	}
	inc := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"archived":"include"}`)
	if len(inc.Automations) != 4 {
		t.Errorf("archived-include = %d, want 4", len(inc.Automations))
	}
}

// The filters answer from the SAME store reads narrowed daemon-side, and an
// empty filtered list says so — the filter's sentence, never the store's.
func TestAutomationListFiltersAndTheTwoEmpties(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	sched := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"source":"schedule"}`)
	if len(sched.Automations) != 1 || sched.Automations[0].ID != "sch-morning" || sched.Automations[0].LastFire == nil {
		t.Fatalf("source=schedule = %s — and the row must carry its last fire", autoJSON(t, sched))
	}
	q := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"q":"squad"}`)
	if len(q.Automations) != 1 || q.Automations[0].ID != tr.ID {
		t.Fatalf("q=squad = %s", autoJSON(t, q))
	}
	empty := auto[protocol.AutomationListResult](t, d, protocol.MethodAutomationList, `{"q":"no-such-thing"}`)
	if len(empty.Automations) != 0 || empty.Statement != "no automations match this filter." {
		t.Fatalf("filtered-empty = %s — the FILTER's sentence expected", autoJSON(t, empty))
	}
	// The store-empty sentence needs its own pristine store.
	s2, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	d2, _ := newTestDaemonWithStore(t, s2)
	quiet := auto[protocol.AutomationListResult](t, d2, protocol.MethodAutomationList, `{}`)
	if len(quiet.Automations) != 0 || quiet.Statement != "no automations. nothing starts agents on its own." {
		t.Fatalf("store-empty = %s — the STORE's sentence expected", autoJSON(t, quiet))
	}
}

// AN5's differential, measured: the socket's unified log equals BOTH raw
// fire tables hand-unioned (renames applied, the NULL-claim row gone, the
// view's words and counts).
func TestAutomationFiresEqualsBothTablesViaTheView(t *testing.T) {
	s, d, _, tr := automationEnv(t)
	got := auto[protocol.AutomationFiresResult](t, d, protocol.MethodAutomationFires, `{}`)
	// Hand-build the expected union straight from the two tables.
	type row struct{ source, id, at, outcome string }
	var want []row
	schedFires, err := s.TriggerFireHistory(context.Background(), "sch-morning", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range schedFires {
		out := f.Outcome
		switch out { // AN12's renames, spelled out by the TEST, not borrowed
		case "fired":
			out = "started"
		case "did_not_fire":
			out = "skipped"
		case "could_not_say":
			out = "could-not-say"
		}
		want = append(want, row{"schedule", "sch-morning", f.Instant, out})
	}
	evtFires, err := s.ListEventFires(store.EventFireFilter{TriggerID: tr.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range evtFires {
		if f.DecidedAt == "" {
			continue // the claim is not an outcome (AN5)
		}
		want = append(want, row{"event", tr.ID, f.DecidedAt, f.Outcome})
	}
	sort.Slice(want, func(i, j int) bool { return want[i].at > want[j].at })
	if len(got.Fires) != len(want) {
		t.Fatalf("fires = %s, want %d rows (%+v)", autoJSON(t, got), len(want), want)
	}
	for i, w := range want {
		g := got.Fires[i]
		if g.Source != w.source || g.TriggerID != w.id || g.At != w.at || g.Outcome != w.outcome {
			t.Errorf("fire %d = %+v, want %+v — the view and the socket disagree (at DESC ordering included)", i, g, w)
		}
	}
	// The raw event table holds TWO rows (the started fire and the mid-air
	// claim); the log answers ONE. That gap is AN5: the claim is not an
	// outcome.
	undecided := 0
	for _, f := range evtFires {
		if f.DecidedAt == "" {
			undecided++
		}
	}
	socketEventRows := 0
	for _, f := range got.Fires {
		if f.Source == "event" {
			socketEventRows++
		}
	}
	if undecided != 1 || socketEventRows != 1 {
		t.Errorf("raw undecided=%d but socket event rows=%d — the NULL-claim row must be invisible (AN5)", undecided, socketEventRows)
	}
	// The counts come with the listing and speak the view's five.
	if got.Counts.Started != 2 || got.Counts.Skipped != 1 || got.Counts.Limited != 0 || got.Counts.Refused != 0 || got.Counts.CouldNotSay != 0 {
		t.Errorf("counts = %+v, want started 2, skipped 1", got.Counts)
	}
}

// The cursor pages newest-first and never re-reads a page.
func TestAutomationFiresCursorPagesWithoutOverlap(t *testing.T) {
	_, d, _, _ := automationEnv(t)
	p1 := auto[protocol.AutomationFiresResult](t, d, protocol.MethodAutomationFires, `{"limit":2}`)
	if len(p1.Fires) != 2 || p1.NextBeforeId == "" {
		t.Fatalf("page one = %s", autoJSON(t, p1))
	}
	p2 := auto[protocol.AutomationFiresResult](t, d, protocol.MethodAutomationFires, `{"limit":2,"beforeId":"`+p1.NextBeforeId+`"}`)
	if len(p2.Fires) != 1 {
		t.Fatalf("page two = %s, want the last of three", autoJSON(t, p2))
	}
	seen := map[string]bool{}
	for _, f := range append(append([]protocol.AutomationFireView{}, p1.Fires...), p2.Fires...) {
		k := f.Source + "|" + f.TriggerID + "|" + f.At
		if seen[k] {
			t.Errorf("row %s appears on both pages", k)
		}
		seen[k] = true
	}
	// Newest-first across families (fixture ats: 11-02 08:00:05 started
	// (event), 11-02 07:00 started (schedule), 11-01 07:00 skipped).
	if p1.Fires[0].Source != "event" || p1.Fires[0].Outcome != "started" || p1.Fires[1].Source != "schedule" || p1.Fires[1].Outcome != "started" {
		t.Errorf("page one ordering = %s", autoJSON(t, p1.Fires))
	}
	if p2.Fires[0].Outcome != "skipped" {
		t.Errorf("page two ordering = %s", autoJSON(t, p2.Fires))
	}
}

// automation.runs answers the loops THIS trigger started — provenance rows,
// both families, and it answers a typo honestly.
func TestAutomationRunsListsLoopsStartedByTheTrigger(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	ev := auto[protocol.AutomationRunsResult](t, d, protocol.MethodAutomationRuns, `{"source":"event","id":"`+tr.ID+`"}`)
	if ev.Total != 1 || len(ev.Loops) != 1 || ev.Loops[0].Slug != "bug-run-1" {
		t.Fatalf("event runs = %s", autoJSON(t, ev))
	}
	sc := auto[protocol.AutomationRunsResult](t, d, protocol.MethodAutomationRuns, `{"source":"schedule","id":"sch-morning"}`)
	if sc.Total != 1 || sc.Loops[0].Slug != "morning-1" {
		t.Fatalf("schedule runs = %s", autoJSON(t, sc))
	}
	_, err := autoCall(t, d, protocol.MethodAutomationRuns, `{"source":"schedule","id":"sch-mornin"}`)
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != -31890 {
		t.Fatalf("typo'd runs: err = %v, want %d", err, -31890)
	}
	if !strings.Contains(e.Data.What, "sch-morning") {
		t.Errorf("the unknown-automation refusal must name what it meant: %q", e.Data.What)
	}
}

// automation.changes: newest first, and the empty audit carries the
// audit's own sentence.
func TestAutomationChangesNewestFirstAndEmptyStatement(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	got := auto[protocol.AutomationChangesResult](t, d, protocol.MethodAutomationChanges, `{}`)
	// T4's event writes audit their own creation: the two seeded event
	// triggers now carry their own create rows beside the two hand-inserted
	// history rows. Insertion order IS the page's newest-first.
	if len(got.Changes) != 4 || got.Statement != "" {
		t.Fatalf("changes = %s", autoJSON(t, got))
	}
	if got.Changes[0].Op != "update" || got.Changes[0].TriggerID != tr.ID || got.Changes[1].Op != "create" {
		t.Errorf("changes not newest-first: %s", autoJSON(t, got.Changes))
	}
	if got.Changes[2].Op != "create" || got.Changes[3].Op != "create" {
		t.Errorf("the two seeded triggers owe their own create audits: %s", autoJSON(t, got.Changes[2:]))
	}
	seen := map[string]bool{}
	seen[got.Changes[2].Summary] = true
	seen[got.Changes[3].Summary] = true
	if !seen["created event "+tr.ID] {
		t.Errorf("the shown trigger's own create audit missing from the tail: %s", autoJSON(t, got.Changes))
	}
	one := auto[protocol.AutomationChangesResult](t, d, protocol.MethodAutomationChanges, `{"source":"schedule"}`)
	if len(one.Changes) != 1 || one.Changes[0].ActorID != "operator" {
		t.Errorf("filtered changes = %s", autoJSON(t, one))
	}
	s2, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	d2, _ := newTestDaemonWithStore(t, s2)
	quiet := auto[protocol.AutomationChangesResult](t, d2, protocol.MethodAutomationChanges, `{}`)
	if quiet.Statement != "no automation changes recorded." {
		t.Errorf("empty audit statement = %q", quiet.Statement)
	}
}

// automation.show answers with the union row PLUS the source's canonical
// form, the plain-words description, health, five recent fires and the
// audit tail — the Settings drawer's one call.
func TestShowCarriesCanonicalFormDescriptionHealthAndRecent(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	got := auto[protocol.AutomationShowResult](t, d, protocol.MethodAutomationShow, `{"source":"schedule","id":"sch-morning"}`)
	if got.Schedule == nil || got.Schedule.Expr != "0 7 * * *" || got.Schedule.Zone != "Europe/Lisbon" {
		t.Fatalf("canonical schedule form missing or wrong: %s", autoJSON(t, got))
	}
	if got.Webhook != nil || got.Event != nil {
		t.Errorf("a schedule answered the OTHER families' canonical forms: %s", autoJSON(t, got))
	}
	if !strings.Contains(got.Description, "07:00") || !strings.Contains(got.Description, "Europe/Lisbon") {
		t.Errorf("description = %q — must say the schedule in plain words", got.Description)
	}
	if len(got.RecentFires) != 2 || got.RecentFires[0].At != "2026-11-02T07:00:00Z" {
		t.Errorf("recent fires = %s", autoJSON(t, got.RecentFires))
	}
	if len(got.Changes) != 1 {
		t.Errorf("show carries %d changes, want the trigger's own 1", len(got.Changes))
	}
	ev := auto[protocol.AutomationShowResult](t, d, protocol.MethodAutomationShow, `{"source":"event","id":"`+tr.ID+`"}`)
	if ev.Event == nil || !strings.Contains(string(ev.Event.Match), `"task_type"`) || !strings.Contains(string(ev.Event.Match), "bug") {
		t.Errorf("event canonical form = %s", autoJSON(t, ev.Event))
	}
	if ev.Automation.Match == nil {
		t.Errorf("the union row lost the match: %s", autoJSON(t, ev.Automation))
	}
}

// A typo'd or wrong-family name refuses with the -31890 shape and NAMES the
// near candidates — the refusal that teaches the namespace.
func TestUnknownAutomationRefusalsNameCandidates(t *testing.T) {
	_, d, _, tr := automationEnv(t)
	_, err := autoCall(t, d, protocol.MethodAutomationShow, `{"source":"schedule","id":"sch-mornin"}`)
	var e *protocol.Error
	if !errors.As(err, &e) || e.Code != -31890 {
		t.Fatalf("err = %v, want -31890", err)
	}
	if !strings.Contains(e.Data.What, "sch-morning") || !strings.Contains(e.Data.Means, "unknown") || e.Data.Todo == "" || e.Data.Fatal {
		t.Errorf("refusal parts = %+v", e.Data)
	}
	// The cross-family typo: an id that EXISTS under another source is
	// named as such (the source is part of the name).
	_, err = autoCall(t, d, protocol.MethodAutomationShow, `{"source":"schedule","id":"`+tr.ID+`"}`)
	if !errors.As(err, &e) || e.Code != -31890 || !strings.Contains(e.Data.What, "is a event") {
		t.Errorf("cross-family typo refusal = %v (%q) — must say the id is an event trigger", err, e.Data.What)
	}
}

// THE AN7 classification, daemon-side: every S49 read stands classified as
// a read, on no agent or debug plane. (The live narrow projection is pinned
// by AN7's own name in internal/narrow's caller tests — cmd/aeon's guard
// suite joins it at T5; this is the table itself speaking.) T7 (2026-11-04)
// amended ONE leg: the tool twin. Four of these ten reads gained a catalog
// action by T7's own grant (AN9's SEE half — automation_list/show/fires/
// health); the assertion now demands exactly those four twins and still
// empty on the six reads the agent was not given. Narrow and AgentService
// keep the original zero for all ten.
func TestNoAutomationReadReachesTheNarrowSurface(t *testing.T) {
	_, d, _, _ := automationEnv(t)
	entries := map[protocol.MethodName]protocol.SurfaceEntry{}
	for _, e := range d.Table.Surface() {
		entries[e.Method] = e
	}
	for _, m := range []protocol.MethodName{
		protocol.MethodAutomationList, protocol.MethodAutomationShow, protocol.MethodAutomationFires,
		protocol.MethodAutomationRuns, protocol.MethodAutomationHealth, protocol.MethodAutomationVocabulary,
		protocol.MethodAutomationPreview, protocol.MethodAutomationTest, protocol.MethodAutomationDryRun,
		protocol.MethodAutomationChanges,
	} {
		e, ok := entries[m]
		if !ok {
			t.Fatalf("%s is absent from the surface answer of a fully wired daemon (S47 completeness)", m)
		}
		if e.Kind != protocol.SurfaceKind("read") {
			t.Errorf("%s classified %s, want read", m, e.Kind)
		}
		if e.Debug || e.Doors.AgentService {
			t.Errorf("%s reaches an agent or debug plane: %+v — AN7", m, e)
		}
		// Amended by T7 (2026-11-04): four of these ten reads now carry a
		// tool twin — automation_list/show/fires/health are the agent's
		// standing eyes (AN9's SEE half), which is the whole of T7. What
		// the amendment tightens rather than loosens: the twin must be
		// EXACTLY that catalog action on those four, and still empty on
		// the other six (runs, vocabulary, preview, test, dryrun, changes
		// are the person's desk, not the agent's surface — a twin growing
		// there is drift with a name). Narrow and AgentService keep the
		// original zero: the catalog action is the agent plane T7 built;
		// the narrow projection and the agent-services enumeration stay
		// closed to this whole plane (AN7 stands).
		wantTwin := ""
		switch m {
		case protocol.MethodAutomationList:
			wantTwin = "automation_list"
		case protocol.MethodAutomationShow:
			wantTwin = "automation_show"
		case protocol.MethodAutomationFires:
			wantTwin = "automation_fires"
		case protocol.MethodAutomationHealth:
			wantTwin = "automation_health"
		}
		if e.Doors.ToolTwin != wantTwin {
			t.Errorf("%s's tool twin = %q, want %q (T7 grants exactly the four read twins, no others) — AN9/AN7", m, e.Doors.ToolTwin, wantTwin)
		}
		if e.Doors.Narrow != "unknown" { // the test daemon never wired NarrowMethods
			t.Errorf("%s narrow door = %q — a S49 read must not project", m, e.Doors.Narrow)
		}
	}
}
