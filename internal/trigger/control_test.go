package trigger

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// control_test.go — T8: the operational plane, proven distinct from the
// governance plane, and fire-now proven honest about who the loop runs
// for. The fake starter records the Fire it was handed, so "as a trigger,
// unattended" is asserted from the value that reached the start door —
// not from a comment saying so.

func controlRow() Schedule {
	return Schedule{
		ID: "nightly", Kind: KindSchedule, Blueprint: "two-step",
		Expr: "30 3 * * *", Zone: "UTC", Workspace: "/tmp/ws", Goal: "survey",
		CreatedAt: "2026-10-27T00:00:00Z",
	}
}

func controlSched(t *testing.T, at time.Time) (*fakeStore, *fakeStarter, *Scheduler) {
	t.Helper()
	st := &fakeStore{scheds: []Schedule{controlRow()}, covered: map[string]string{}}
	fa := &fakeStarter{id: "L-fire"}
	sc := NewScheduler(st, fa, &fakeClock{t: at})
	return st, fa, sc
}

func TestFireNowFiresAsATriggerAndTheLoopIsUnattended(t *testing.T) {
	st, fa, sc := controlSched(t, time.Date(2026, 10, 27, 9, 15, 30, 0, time.UTC))
	a, err := sc.FireNow(context.Background(), UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Found || a.Outcome != string(OutcomeFired) || a.LoopID != "L-fire" {
		t.Fatalf("fire-now did not fire: %+v", a)
	}
	if fa.call != 1 || len(fa.got) != 1 {
		t.Fatalf("fire-now did not go through the one start door: call=%d got=%d", fa.call, len(fa.got))
	}
	f := fa.got[0]
	if f.Provenance != ProvenanceTrigger {
		t.Fatalf("fire-now fired as %q — it must fire AS A TRIGGER (TR2: no attendedness through the back door)", f.Provenance)
	}
	// The same door, no fields that could speak otherwise: the reflection
	// rule lives with T2's pin on Fire itself — here the assertion is
	// that fire-now walked it with a trigger's provenance and no
	// invoker-naming field carried in the value that reached the start.
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeFired {
		t.Fatalf("the record must show the fire: %+v", st.fires)
	}
	// The instant is the EXACT moment the human asked (T9 walk finding #4:
	// truncating to the minute collided with the tick's UNIQUE
	// (schedule_id, instant) row and hid fire-now from its own overlap
	// query). fire-now has no scheduled instant; the ask IS the instant.
	if want := time.Date(2026, 10, 27, 9, 15, 30, 0, time.UTC); !st.fires[0].Instant.Equal(want) {
		t.Fatalf("fire-now instant %v, want the exact now %v", st.fires[0].Instant, want)
	}
}

func TestFireNowWalksEveryGate(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 15, 0, 0, time.UTC)
	_, fa, sc := controlSched(t, at)
	order := []string{}
	sc.Gate.Overlap = func(Schedule, time.Time) (bool, string) { order = append(order, "overlap"); return false, "" }
	sc.Gate.Access = func(kind, id string) (bool, string) {
		order = append(order, "access:"+kind+":"+id)
		return false, "refused: agent:nightly · trigger_access · rule `no-schedules` (deny) matched · PL4"
	}
	a, err := sc.FireNow(context.Background(), UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if a.Outcome != string(OutcomeRefused) || fa.call != 0 {
		t.Fatalf("governance said no and fire-now walked anyway: %+v", a)
	}
	if len(order) != 2 || order[0] != "overlap" || order[1] != "access:schedule:nightly" {
		t.Fatalf("gate order/kind: %v", order)
	}
	if !strings.Contains(a.Reason, "no-schedules") {
		t.Fatalf("the operator's sentence reaches the person who pressed the button: %q", a.Reason)
	}
	// (The design's third kind — `manual` — lives in the POLICY vocabulary
	// but has no row producer yet: migration 0032's CHECK admits only
	// schedule and webhook, and inventing a kind without a migration would
	// be the schema silently disagreeing with itself. When a manual kind
	// ships, fireAt asks governance for string(s.Kind) — not the literal
	// "schedule" — so the closed three will mean something there too: the
	// shape is ready; the row type awaits its producer and its migration.)
}

func TestFireNowRefusesAWebhookRow(t *testing.T) {
	// The ruling with teeth: fire-now must not become the bypass around
	// the signature that is the webhook's ENTIRE authentication.
	st, fa, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	st.scheds[0].Kind, st.scheds[0].Expr, st.scheds[0].Zone = KindWebhook, "", ""
	st.scheds[0].SecretEnv = "AEON_WEBHOOK_SECRET_SHIP"
	a, err := sc.FireNow(context.Background(), UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if a.Outcome != string(OutcomeRefused) || fa.call != 0 {
		t.Fatalf("fire-now fired a webhook row without any signature: %+v", a)
	}
	if !strings.Contains(a.Reason, "DELIVERED, not fired") {
		t.Fatalf("the refusal must say WHY it is the bypass: %q", a.Reason)
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeRefused {
		t.Fatalf("the attempt is recorded: %+v", st.fires)
	}
}

func TestPausedFireNowHonoursTheOff(t *testing.T) {
	// TR12: "not right now" means not when asked nicely either. did_not_
	// fire with the OPERATIONAL sentence — the word refused (governance's
	// word) appears nowhere.
	st, fa, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	st.scheds[0].Paused = true
	a, err := sc.FireNow(context.Background(), UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if a.Outcome != string(OutcomeDidNotFire) || !a.Paused || fa.call != 0 {
		t.Fatalf("the operational off did not hold: %+v", a)
	}
	if !strings.Contains(a.Reason, "TR12") || strings.Contains(a.Reason, "refused") {
		t.Fatalf("operational words only — governance opened its mouth nowhere here: %q", a.Reason)
	}
	if len(st.fires) != 1 || st.fires[0].Outcome != OutcomeDidNotFire {
		t.Fatalf("a human's explicit ask is ANSWERED in the record (Q3's silence governs the automatic paths): %+v", st.fires)
	}
}

func TestPauseAndResumeAreRecorded(t *testing.T) {
	st, _, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if a, err := sc.Pause(ctx, UserActor("operator"), "nightly"); err != nil || !a.Found || !a.Paused {
		t.Fatalf("pause: %+v %v", a, err)
	}
	if !st.scheds[0].Paused {
		t.Fatal("the pause is not persisted — 'recorded' means the ROW says it")
	}
	// Idempotent, and says so.
	if a, err := sc.Pause(ctx, UserActor("operator"), "nightly"); err != nil || !strings.Contains(a.Reason, "already paused") {
		t.Fatalf("second pause must be a true answer, not a second write: %+v %v", a, err)
	}
	// The clock honours it: Tick answers NOTHING for a paused row.
	before := len(st.fires)
	if err := sc.Tick(ctx, time.Date(2026, 10, 27, 3, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if len(st.fires) != before {
		t.Fatalf("a paused schedule answered the tick — Q3's off must be silent on automatic paths: %+v", st.fires)
	}
	if a, err := sc.Resume(ctx, UserActor("operator"), "nightly"); err != nil || a.Paused {
		t.Fatalf("resume: %+v %v", a, err)
	}
	if st.scheds[0].Paused {
		t.Fatal("resume did not lift the bar on the row")
	}
	// The frontier stands across both flips (the upsert keeps it).
	if _, ok := st.covered["nightly"]; ok {
		t.Fatal("this fixture never advanced the frontier; the fake's save must still preserve whatever was there")
	}
	// Unknown id: an answer, not a crash, and it leaks nothing.
	if a, err := sc.Pause(ctx, UserActor("operator"), "nobody-home"); err != nil || a.Found || !strings.Contains(a.Reason, "no trigger nobody-home") {
		t.Fatalf("unknown id: %+v %v", a, err)
	}
}

func TestPauseAndTriggerAccessAreDistinct(t *testing.T) {
	// The task's two-off-switches table, as an executable difference:
	// SAME schedule, TWO different blocks, and no reader can confuse the
	// answers — different outcome words, different sentences, different
	// planes.
	st, _, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()
	sc.Gate.Access = func(string, string) (bool, string) {
		return false, "refused: agent:nightly · trigger_access · rule `no-unattended-in-prod` (deny) matched · PL4"
	}
	governed, err := sc.FireNow(ctx, UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if governed.Outcome != string(OutcomeRefused) || !strings.Contains(governed.Reason, "trigger_access") {
		t.Fatalf("the governance answer: %+v", governed)
	}
	if strings.Contains(governed.Reason, "TR12") || strings.Contains(governed.Reason, "paused") {
		t.Fatalf("governance must not speak operational words: %q", governed.Reason)
	}
	st.scheds[0].Paused = true
	st.fires = nil
	off, err := sc.FireNow(ctx, UserActor("operator"), "nightly")
	if err != nil {
		t.Fatal(err)
	}
	if off.Outcome != string(OutcomeDidNotFire) || !strings.Contains(off.Reason, "TR12") {
		t.Fatalf("the operational answer: %+v", off)
	}
	if strings.Contains(off.Reason, "trigger_access") || strings.Contains(off.Reason, "refused") {
		t.Fatalf("the operational plane must not borrow governance's words (it did NOT evaluate policy): %q", off.Reason)
	}
	// And the LISTING speaks only the operational word — a policy verdict
	// never appears there, because this plane cannot evaluate it and an
	// implicit "allowed" is how governance dies.
	views, err := sc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(views)
	if !strings.Contains(string(raw), `"paused":true`) {
		t.Fatalf("the listing must show PAUSED: %s", raw)
	}
	for _, leak := range []string{"trigger_access", "deny", "refused", "PL5", "PL4"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("the operational listing carries governance's word %q — the two planes report apart: %s", leak, raw)
		}
	}
}

func TestListReportsOperationalFactsIncludingNextFire(t *testing.T) {
	st, _, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	st.scheds[0].WindowStart, st.scheds[0].WindowEnd, st.scheds[0].WindowDays = "09:00", "17:00", "1,2,3,4,5"
	views, err := sc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := views[0]
	if v.NextFire != "2026-10-28T03:30:00Z (UTC)" {
		t.Fatalf("nextFire is the SAME computation the clock uses, printed with its zone: %q", v.NextFire)
	}
	if !strings.Contains(v.Window, "09:00") || !strings.Contains(v.Window, "17:00") {
		t.Fatalf("the window must read back: %q", v.Window)
	}
	// A row nobody can retire: broken zone still LISTED, saying so — the
	// listing's job is what is configured, brokenness included.
	st.scheds = append(st.scheds, Schedule{ID: "bad", Kind: KindSchedule, Zone: "Mars/Olympus", Blueprint: "b"})
	views, err = sc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || !strings.Contains(views[1].NextFire, "zone does not load") {
		t.Fatalf("a broken row vanished or lied: %+v", views[1])
	}
	// The view struct itself cannot CARRY a secret: field-name sweep —
	// only SecretEnv (a NAME) is admitted.
	rt := reflect.TypeOf(TriggerView{})
	for i := 0; i < rt.NumField(); i++ {
		n := rt.Field(i).Name
		if strings.Contains(strings.ToLower(n), "secret") && n != "SecretEnv" {
			t.Fatalf("the listing view grew a secret-carrying field %q", n)
		}
	}
}

func TestAddAnswersInValidatorsOwnSentences(t *testing.T) {
	// T9's finding made whole: with no Add there was no door to create a
	// row — the demo could not start. The door now opens only through
	// T1's validator: the refusal IS the write-time sentence.
	st, _, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()
	// T1's validator checks the workspace EXISTS (refuse at write; a
	// schedule that stores cleanly and fires into nothing is the worst
	// failure here) — the fixture owes it a real directory.
	ws := t.TempDir()
	bad := Schedule{ID: "offset-zone", Kind: KindSchedule, Blueprint: "b", Expr: "0 3 * * *", Zone: "+01:00", Workspace: ws}
	a, err := sc.Add(ctx, UserActor("operator"), bad)
	if err != nil {
		t.Fatal(err)
	}
	if a.Found || !strings.Contains(a.Reason, "+01:00") {
		t.Fatalf("an offset zone must be refused with what was TYPED quoted back: %+v", a)
	}
	if len(st.scheds) != 1 {
		t.Fatal("a refused add wrote a row")
	}
	good := Schedule{ID: "night-two", Kind: KindSchedule, Blueprint: "b", Expr: "0 4 * * *", Zone: "UTC", Workspace: ws}
	if a, err := sc.Add(ctx, UserActor("operator"), good); err != nil || !a.Found {
		t.Fatalf("a well-formed row must register: %+v %v", a, err)
	}
	if len(st.scheds) != 2 {
		t.Fatal("the registered row is not persisted")
	}
	// Duplicate refused — an upsert on add is how a typo retires a job.
	if a, err := sc.Add(ctx, UserActor("operator"), good); err != nil || a.Found || !strings.Contains(a.Reason, "already exists") {
		t.Fatalf("add must never silently rewrite: %+v %v", a, err)
	}
	// The webhook twin: zoneless accepted, secret by NAME only.
	hook := Schedule{ID: "door", Kind: KindWebhook, Blueprint: "b", Workspace: ws, SecretEnv: "AEON_WEBHOOK_SECRET_DOOR"}
	if a, err := sc.Add(ctx, UserActor("operator"), hook); err != nil || !a.Found {
		t.Fatalf("the zoneless webhook row must register (R-19): %+v %v", a, err)
	}
	hook2 := hook
	hook2.ID, hook2.Zone = "door-zoned", "UTC"
	if a, err := sc.Add(ctx, UserActor("operator"), hook2); err != nil || a.Found {
		t.Fatalf("a zone-carrying webhook row is a field nobody fires: %+v %v", a, err)
	}
}

func TestAddSeedsTheReconciliationFrontier(t *testing.T) {
	// Finding #5, pinned (T9 walk): an empty CoveredUntil parses as ZERO
	// time, the walk caps ("unrealistically wide — left untouched") and a
	// brand-new schedule never answers — tick included. Add is the field's
	// only producer; it starts the trigger from now, never replays the past.
	_, _, sc := controlSched(t, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	ws := t.TempDir()
	a, err := sc.Add(context.Background(), UserActor("operator"), Schedule{ID: "fresh", Kind: KindSchedule, Blueprint: "b",
		Expr: "*/5 * * * *", Zone: "UTC", Workspace: ws})
	if err != nil || !a.Found {
		t.Fatalf("add: %+v %v", a, err)
	}
	st := sc.Store.(*fakeStore)
	var got Schedule
	for _, r := range st.scheds {
		if r.ID == "fresh" {
			got = r
		}
	}
	if got.ID == "" {
		t.Fatal("the fresh row is not in the store")
	}
	if got.CoveredUntil == "" || got.CoveredUntil != got.CreatedAt {
		t.Fatalf("a fresh schedule must start its frontier at registration (covered=%q created=%q) — an empty frontier is zero time and zero time silences the schedule forever", got.CoveredUntil, got.CreatedAt)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.CoveredUntil); err != nil {
		t.Fatalf("the frontier is a real instant the clock can parse: %v", err)
	}
}
