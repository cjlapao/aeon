package daemon

// S55 T9 — the watchdog service's BEHAVIOURS, each one the spec's table names:
// a running loop with no activity fires once per episode (and only then); the
// move taken is the attendedness, not the rule (LC17); the actor is the
// daemon's {system, watchdog}, never a user (LC17); only RUNNING loops are
// observed (a waiting/blocked/idle loop is never "stuck"); the stuck block is
// resumable by a person and NOT raisable (it is not a cap-hit); the seeded
// set is system-origin, idempotent and never re-seeds an operator's choice
// (P8, SD6); a disable is live at the next tick (no cache); and the door's
// confirmation binds the digest to the statement (PM7.1, T8's pattern).
//
// The clock is the service's own injectable now: the test reads the store's
// last activity (the real wall clock, written when the rows were created) and
// sets now past the threshold, so the episode maths are asserted against the
// store's own timestamps, never a sleep. A tick is called directly — the
// ticker's rhythm is a constant, not behaviour under test.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/watchdog/watchdogseed"
)

// --- the test's share of the daemon's job ------------------------------------

// wdBlueprint is the minimal parseable blueprint the test loops pin: the
// control core's doors (resume, raise) parse the pin before they act, so an
// unparseable pin is a pin-unparseable error the refusal under test never
// reaches. No lane here is ever dispatched: the test daemon has no runner
// (StartRunner is a nil no-op), so the pin's lanes are never run.
const wdBlueprint = "id: dev-loop\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: build\n    step: {kind: model-step, agent: default, produces: OUT.md}\n"

// wdLoop creates a pinned loop at the status the test names (the default is
// the created-state "active", which is not what any of these tests wants) and
// returns its id.
func wdLoop(t *testing.T, s *store.Store, root, slug, status string, attended *bool) string {
	t.Helper()
	pin, err := s.CreatePinnedLoop(store.NewLoop{
		Slug: slug, ComponentRoot: root, Root: root, Title: "W", Goal: "g",
		Status: status, Attended: attended,
	}, "dev-loop", 1, wdBlueprint, "{}")
	if err != nil {
		t.Fatalf("create loop %s: %v", slug, err)
	}
	return pin.ID
}

// wdStep writes one step row (the no-activity clock's source, the
// same-position count's row, the step-over-deadline's subject).
func wdStep(t *testing.T, s *store.Store, root, slug, phase, taskID string) int64 {
	t.Helper()
	st, err := s.CreateStep(store.Scope{LoopSlug: slug, ComponentRoot: root},
		store.NewStep{TaskID: taskID, Phase: phase, Agent: "x", Status: store.StepRunning})
	if err != nil {
		t.Fatalf("create step %s/%s: %v", slug, phase, err)
	}
	return st.ID
}

// wdRule creates a rule through the store's one write path and returns its id.
func wdRule(t *testing.T, s *store.Store, signal string, threshold int64, action string) string {
	t.Helper()
	r, err := s.WatchdogSet(store.WatchdogSetIn{
		Scope: "workspace", SubjectID: "workspace", Signal: signal,
		Threshold: threshold, Action: action,
		Origin: "user", ActorKind: "user", ActorID: "test",
	})
	if err != nil {
		t.Fatalf("create rule %s/%s %d/%s: %v", signal, "", threshold, action, err)
	}
	return r.ID
}

// wdSvc builds the service the way RunWatchdog does, over the test daemon.
func wdSvc(t *testing.T, d *Daemon) *watchdogService {
	t.Helper()
	svc := d.newWatchdogService()
	return svc
}

// wdNow sets the service's clock to the store's last activity for the loop,
// plus after — the test's control of "the loop has been silent for X". The
// store's own timestamp is the T0, so the episode identity (which embeds it)
// and the threshold comparison both read from what the store wrote.
func wdNow(t *testing.T, s *store.Store, svc *watchdogService, loopID string, after time.Duration) {
	t.Helper()
	last, err := s.WatchdogLastActivity(loopID)
	if err != nil {
		t.Fatalf("last activity of %s: %v", loopID, err)
	}
	if last.IsZero() {
		t.Fatalf("the loop %s has no activity source — the test must create a step or pin first", loopID)
	}
	svc.now = func() time.Time { return last.Add(after) }
}

// wdFireCount is the number of kind-'watchdog' control events a loop carries
// (the fire rows: the notify arm appends one, the block arm's CAS appends one;
// a stop rides kind-'stop' and is counted by its own kind).
func wdFireCount(t *testing.T, s *store.Store, loopID string) int {
	t.Helper()
	events, err := s.ListControlEvents(loopID, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == "watchdog" {
			n++
		}
	}
	return n
}

// wdStopCount is the number of kind-'stop' control events a loop carries (the
// attended move, ParkLoopStopped's row).
func wdStopCount(t *testing.T, s *store.Store, loopID string) int {
	t.Helper()
	events, err := s.ListControlEvents(loopID, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == "stop" {
			n++
		}
	}
	return n
}

// wdStatus reads a loop's current status straight from the store.
func wdStatus(t *testing.T, s *store.Store, loopID string) string {
	t.Helper()
	l, err := s.ReadLoopByID(loopID)
	if err != nil {
		t.Fatalf("read loop %s: %v", loopID, err)
	}
	return l.Status
}

// wdErrorOf is the house assertion for a refused act (budgetErrorOf's shape):
// the error is a *protocol.Error — compare the code, read the specifics out
// of Data.
func wdErrorOf(t *testing.T, err error) *protocol.Error {
	t.Helper()
	if err == nil {
		t.Fatal("the act was admitted; it must have refused")
	}
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		t.Fatalf("the refusal is %T (%v), want a protocol.Error", err, err)
	}
	return pe
}

// (1) Behaviour 1 — the once-per-episode rule: a running loop with no
// activity past a notify threshold fires ONCE; a second tick on the same
// episode does not refire; progress (a new step row — a new moment of
// activity) starts a new episode, and the fresh silence may fire again.
func TestWatchdogFiresOncePerEpisode(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	id := wdLoop(t, s, root, "wd-ep", "running", nil)
	wdStep(t, s, root, "wd-ep", "build", "T1")
	rule := wdRule(t, s, "no-activity", 900000, "notify") // 15 minutes
	svc := wdSvc(t, d)

	// Past the threshold: one tick, one fire, the loop keeps running.
	wdNow(t, s, svc, id, 16*time.Minute)
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 1 {
		t.Fatalf("after the first tick the loop carries %d fire rows, want 1", n)
	}
	if st := wdStatus(t, s, id); st != "running" {
		t.Fatalf("a notify fired and the loop is %q — a notify moves nothing", st)
	}

	// The same episode, ticked again: no refire.
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 1 {
		t.Fatalf("the second tick refired the same episode: %d fire rows, want 1", n)
	}

	// Progress: a new step row is a new moment of activity, and the silence
	// after it is a NEW episode — the rule may fire for it.
	wdStep(t, s, root, "wd-ep", "build", "T1")
	wdNow(t, s, svc, id, 16*time.Minute)
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 2 {
		t.Fatalf("after progress and fresh silence the loop carries %d fire rows, want 2 (a new episode may fire)", n)
	}
	if rule == "" {
		t.Fatal("the rule id is empty — the test mis-set up")
	}
}

// (2) Behaviour 2 + LC17 — the move is the attendedness, not the rule. A
// STOP rule on an attended loop parks it waiting/stopped; the same rule on an
// unattended or never-declared loop blocks it stuck. Both audited with the
// daemon's {system, watchdog}.
func TestWatchdogActionByAttendedness(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	yes := true

	attID := wdLoop(t, s, root, "wd-att", "running", &yes)
	wdStep(t, s, root, "wd-att", "build", "T1")
	unattID := wdLoop(t, s, root, "wd-unatt", "running", nil)
	wdStep(t, s, root, "wd-unatt", "build", "T1")
	// One rule covers both (workspace scope): the attendedness picks the move.
	wdRule(t, s, "no-activity", 3600000, "stop")

	svc := wdSvc(t, d)
	wdNow(t, s, svc, attID, 2*time.Hour)
	svc.tick(context.Background())
	wdNow(t, s, svc, unattID, 2*time.Hour)
	svc.tick(context.Background())

	// The attended loop is parked waiting/stopped — a person can pick it up.
	if st := wdStatus(t, s, attID); st != "waiting" {
		t.Fatalf("the attended loop is %q after the stop rule fired, want waiting", st)
	}
	if n := wdStopCount(t, s, attID); n != 1 {
		t.Fatalf("the attended loop carries %d stop rows, want 1", n)
	}
	// The unattended loop is blocked stuck — only a person's resume moves it.
	if st := wdStatus(t, s, unattID); st != "blocked" {
		t.Fatalf("the unattended loop is %q after the stop rule fired, want blocked (LC17: the attendedness, not the rule, picked the move)", st)
	}
	if n := wdFireCount(t, s, unattID); n != 1 {
		t.Fatalf("the unattended loop carries %d fire rows, want 1", n)
	}
	// The fire row's detail says the move TAKEN (block), not the rule's action.
	events, err := s.ListControlEvents(unattID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sawBlock bool
	for _, e := range events {
		if e.Kind != "watchdog" {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &d); err != nil {
			t.Fatalf("the fire detail does not parse: %v", err)
		}
		if d["action"] == "block" {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("the unattended fire's detail does not name the move taken (action=block) — the audit must say what happened to the loop: %+v", events)
	}
}

// (3) Behaviour 3 — the observation filter: a loop waiting on a question for
// days, a blocked loop, a never-dispatched one is never touched, no matter
// how far the clock runs. The service's whole filter is
// PinnedLoopsByStatus("running"); the test proves the other states survive a
// tick at 2× every threshold.
func TestWatchdogObservesRunningLoopsOnly(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	yes := true
	cases := []struct {
		slug   string
		status string
		att    *bool
	}{
		{"wd-wait", "waiting", &yes}, // a question, days old
		{"wd-blk", "blocked", &yes},  // a cap-hit block
		{"wd-act", "active", &yes},   // created, never dispatched
		{"wd-act2", "active", nil},   // and never-declared
	}
	var ids []string
	for _, c := range cases {
		id := wdLoop(t, s, root, c.slug, c.status, c.att)
		wdStep(t, s, root, c.slug, "build", "T1")
		ids = append(ids, id)
	}
	wdRule(t, s, "no-activity", 900000, "notify")
	wdRule(t, s, "no-activity", 3600000, "stop")
	wdRule(t, s, "same-position", 6, "stop")
	wdRule(t, s, "repeat-block-reason", 3, "stop")
	wdRule(t, s, "step-over-deadline", 0, "block")

	svc := wdSvc(t, d)
	// The clock runs past every threshold of every rule (the clock is global
	// to the tick — one loop's last activity sets it for the whole pass).
	wdNow(t, s, svc, ids[0], 48*time.Hour)
	svc.tick(context.Background())

	for i, c := range cases {
		if st := wdStatus(t, s, ids[i]); st != c.status {
			t.Errorf("the %s loop is %q after the tick, want %q — a non-running loop is never touched", c.status, st, c.status)
		}
	}
}

// (4) Behaviour 4 — the actor: every audit row the watchdog writes carries
// {system, watchdog}. A stop that reads as a person's stop is an audit a
// person would trust — the service stamps it, and the test reads it back.
func TestWatchdogActorIsSystem(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	yes := true
	attID := wdLoop(t, s, root, "wd-actor-a", "running", &yes)
	wdStep(t, s, root, "wd-actor-a", "build", "T1")
	unattID := wdLoop(t, s, root, "wd-actor-u", "running", nil)
	wdStep(t, s, root, "wd-actor-u", "build", "T1")
	wdRule(t, s, "no-activity", 3600000, "stop")

	svc := wdSvc(t, d)
	wdNow(t, s, svc, attID, 2*time.Hour)
	svc.tick(context.Background())
	wdNow(t, s, svc, unattID, 2*time.Hour)
	svc.tick(context.Background())

	for name, v := range map[string]struct {
		id   string
		kind string
	}{
		"the stop (attended move)":    {attID, "stop"},
		"the block (unattended move)": {unattID, "watchdog"},
	} {
		id, kind := v.id, v.kind
		events, err := s.ListControlEvents(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		var saw bool
		for _, e := range events {
			if e.Kind != kind {
				continue
			}
			saw = true
			if e.ActorKind != "system" || e.ActorID != "watchdog" {
				t.Errorf("%s: the audit row is %s/%s, want system/watchdog — a watchdog act that reads as a person's act is a forged audit (LC17)", name, e.ActorKind, e.ActorID)
			}
		}
		if !saw {
			t.Errorf("%s: no %s-kind audit row at all — the act was never recorded", name, kind)
		}
	}
}

// (5) Behaviour 4, observation half — same-position counts the step rows the
// loop has at its current position (phase, taskid): the re-dispatches the
// reject counters did not cap. The decision over the count is the pure
// package's (TestEvaluateTable); this proves the store serves the count the
// decision reads. (The service-level path is covered the same way: the
// loop's now_task column has no writer yet — it is the engine's bookkeeping
// — so a service tick with a named position is not constructible at the
// store's door, and counting nothing for an unnamed position is the guard
// in observe() the note above names.)
func TestWatchdogSamePositionCountsTheRows(t *testing.T) {
	_, _, s, root := t15Daemon(t)
	wdLoop(t, s, root, "wd-pos", "running", nil)
	sc := store.Scope{LoopSlug: "wd-pos", ComponentRoot: root}
	for i := 0; i < 6; i++ {
		wdStep(t, s, root, "wd-pos", "build", "T1")
	}
	// A different position (a different task) is a different count.
	wdStep(t, s, root, "wd-pos", "build", "T2")

	n, err := s.WatchdogPositionDispatches(sc, "build", "T1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("the position (build, T1) counts %d dispatches, want 6 — the same-position signal reads the wrong number", n)
	}
	n2, err := s.WatchdogPositionDispatches(sc, "build", "T2")
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("the position (build, T2) counts %d dispatches, want 1 — a new position is progress", n2)
	}
}

// (6) Behaviour 5 — step-over-deadline: a running step whose manifest
// deadline has passed (and the grace of 0 ms has too) blocks the loop. The
// runtime was supposed to stop the step; the watchdog is the net.
func TestWatchdogStepOverDeadlineBlocks(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	id := wdLoop(t, s, root, "wd-dl", "running", nil)
	stepID := wdStep(t, s, root, "wd-dl", "build", "T1")

	// The manifest's deadline is in the PAST, written by the store's own
	// clock read — the step started "now" (the store's T0) and its deadline
	// is an hour before that.
	last, err := s.WatchdogLastActivity(id)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]string{
		"deadlineAt": last.Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetContextManifestJSON(stepID, manifest); err != nil {
		t.Fatal(err)
	}

	wdRule(t, s, "step-over-deadline", 0, "block")
	svc := wdSvc(t, d)
	wdNow(t, s, svc, id, time.Minute) // the deadline + 0 grace is an hour past
	svc.tick(context.Background())

	if st := wdStatus(t, s, id); st != "blocked" {
		t.Fatalf("the loop with a step over its deadline is %q, want blocked — the runtime failed to stop the step and the watchdog is the net", st)
	}
	if n := wdFireCount(t, s, id); n != 1 {
		t.Fatalf("the over-deadline block carried %d fire rows, want 1", n)
	}
}

// (7) Behaviour 6 — the stuck block is a person's to move, not a raise's to
// lift: loop.resume resumes a watchdog stuck block and a watchdog stop, and
// loop.raise refuses the stuck block (it is not a cap-hit — there is no cap
// record to lift).
func TestWatchdogStuckIsResumableAndRaiseRefuses(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	// The control door is what is under test, not the re-dispatch the resume
	// would kick: with a live runner the resume's goroutine would outlive
	// the test's store (it settles on the next advance) and touch a closed
	// one. A nil runner makes StartRunner a no-op, and the CAS the core
	// writes is the resume the assertions read.
	d.loopRunner = nil
	yes := true
	wdRule(t, s, "no-activity", 3600000, "stop")
	svc := wdSvc(t, d)

	// The unattended loop: blocked stuck.
	unattID := wdLoop(t, s, root, "wd-stuck", "running", nil)
	wdStep(t, s, root, "wd-stuck", "build", "T1")
	wdNow(t, s, svc, unattID, 2*time.Hour)
	svc.tick(context.Background())
	if st := wdStatus(t, s, unattID); st != "blocked" {
		t.Fatalf("setup: the unattended loop is %q, want blocked", st)
	}

	// Raise refuses it: a raise is for a loop parked on a cap (the param
	// names the new cap the raise lifts), and CheckRaise gates on the STATE
	// first — a stuck block is not a cap-hit, so the one sentence per state
	// names the door that does apply: resume. (The no-cap kind is the
	// cap-hit crash window's, where a cap record is expected and missing.)
	usd := 500.0
	_, err := t15Dispatch(t, d, protocol.MethodLoopRaise, protocol.LoopRaiseParams{ID: unattID, USDCap: &usd, Reason: "lift it"})
	pe := wdErrorOf(t, err)
	if pe.Code != -31861 || pe.Data.Kind != "blocked" {
		t.Fatalf("raise on a stuck block answered code %d kind %q, want -31861/blocked — a stuck block is not a cap-hit, and the refusal names the door that does apply (resume)", pe.Code, pe.Data.Kind)
	}

	// Resume moves it: a person's act, the only door back to running.
	if _, err := t15Dispatch(t, d, protocol.MethodLoopResume, protocol.LoopResumeParams{ID: unattID}); err != nil {
		t.Fatalf("resume of a stuck block: %v — the block is a person's to move", err)
	}
	if st := wdStatus(t, s, unattID); st != "running" {
		t.Fatalf("after resume the stuck loop is %q, want running", st)
	}

	// The attended loop: parked waiting/stopped, and resume admits it too.
	attID := wdLoop(t, s, root, "wd-stopped", "running", &yes)
	wdStep(t, s, root, "wd-stopped", "build", "T1")
	wdNow(t, s, svc, attID, 2*time.Hour)
	svc.tick(context.Background())
	if st := wdStatus(t, s, attID); st != "waiting" {
		t.Fatalf("setup: the attended loop is %q, want waiting", st)
	}
	if _, err := t15Dispatch(t, d, protocol.MethodLoopResume, protocol.LoopResumeParams{ID: attID}); err != nil {
		t.Fatalf("resume of a watchdog stop: %v", err)
	}
	if st := wdStatus(t, s, attID); st != "running" {
		t.Fatalf("after resume the stopped loop is %q, want running", st)
	}
}

// (8) Behaviour 7 + P8 — the seeded set's provenance, idempotence and the
// operator's choice outliving the set: the five rules arrive system-origin
// and enabled at startup (inert, applied before the first tick); a second
// apply writes nothing; a rule an operator disabled or removed is NOT
// re-seeded; and restore is the explicit op that brings one back.
func TestWatchdogSeedSetProvenanceIdempotenceDivergence(t *testing.T) {
	_, _, s, _ := t15Daemon(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Startup applies the inert set (the class gate: it is the registry's
	// ClassInert that admits it to ApplyInert, asserted by the seed package;
	// here the daemon's start hook is the call site).
	ApplyInertAtStart(ctx, e)
	rules, err := s.ListWatchdogRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 5 {
		t.Fatalf("the seeded set wrote %d rules, want 5 (the LC18 default table)", len(rules))
	}
	eventCounts := map[string]int{}
	for _, r := range rules {
		if r.Origin != "system" || !r.Enabled {
			t.Errorf("the seeded rule %s is origin=%s enabled=%v, want system/true — the set's provenance is the origin", r.ID, r.Origin, r.Enabled)
		}
		evs, err := s.ListWatchdogEvents(r.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		eventCounts[r.ID] = len(evs)
		if len(evs) != 1 || evs[0].Kind != "created" || evs[0].ActorKind != "system" {
			t.Errorf("the seeded rule %s carries %+v, want one kind-'created' event by the system", r.ID, evs)
		}
	}

	// A second startup apply writes nothing: idempotence is observable.
	ApplyInertAtStart(ctx, e)
	for _, r := range rules {
		evs, _ := s.ListWatchdogEvents(r.ID, 0)
		if len(evs) != eventCounts[r.ID] {
			t.Errorf("the re-apply wrote %d events for %s, want %d — a startup apply of a seeded set is idempotent", len(evs), r.ID, eventCounts[r.ID])
		}
	}

	// The operator's choice outlives the set: disable one, remove another,
	// and the next startup applies neither back.
	keepID, removedID := rules[0].ID, rules[1].ID
	if _, err := s.WatchdogSetEnabled(keepID, false, "user", "op"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WatchdogRemove(removedID, "user", "op"); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ListWatchdogEvents(keepID, 0)
	beforeRem, _ := s.ListWatchdogEvents(removedID, 0)
	ApplyInertAtStart(ctx, e)
	kept, err := s.GetWatchdogRule(keepID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Enabled {
		t.Fatal("a disabled system rule was re-seeded at startup — the operator's choice outlives the set (SD6)")
	}
	removed, err := s.GetWatchdogRule(removedID)
	if err != nil {
		t.Fatal(err)
	}
	if removed.Enabled {
		t.Fatal("a removed system rule was re-seeded at startup — removal is a disable that the set never undoes (SD6)")
	}
	after, _ := s.ListWatchdogEvents(keepID, 0)
	afterRem, _ := s.ListWatchdogEvents(removedID, 0)
	if len(after) != len(before) || len(afterRem) != len(beforeRem) {
		t.Errorf("the re-apply wrote rule events for the diverged rows (%d→%d, %d→%d) — a skip writes nothing (SD10)", len(before), len(after), len(beforeRem), len(afterRem))
	}

	// Restore is the explicit op: it brings the removed rule back (its row
	// is the disabled one — never deleted — and the restore re-applies the
	// record over it), and the ledger's consent is the engine's own.
	// rules[1]'s key — the set's key grammar is
	// scope:subject:signal:action:threshold, and ListWatchdogRules' order
	// (signal, then action) puts the no-activity stop at rules[1].
	key := "workspace:workspace:no-activity:stop:3600000"
	res, err := e.Restore(ctx, watchdogseed.SetID, key, seed.Actor{Kind: "user", ID: "op"})
	if err != nil {
		t.Fatalf("restore of %s: %v — restore is the explicit consent that brings a removed rule back", key, err)
	}
	if len(res.Records) != 1 || res.Records[0].Op != seed.OpRestore {
		t.Fatalf("restore answered %+v, want one OpRestore record", res.Records)
	}
	back, err := s.GetWatchdogRule(removedID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Enabled || back.Origin != "system" {
		t.Fatalf("after restore the rule is enabled=%v origin=%s, want true/system — the record's content over the live row", back.Enabled, back.Origin)
	}
}

// (9) Behaviour 7, the no-cache half — a disable changes behaviour at the
// NEXT tick: the rules are re-read from the store on every pass, so a rule a
// person disabled at the last tick does not fire at this one.
func TestWatchdogDisableIsLiveAtTheNextTick(t *testing.T) {
	d, _, s, root := t15Daemon(t)
	id := wdLoop(t, s, root, "wd-nocache", "running", nil)
	wdStep(t, s, root, "wd-nocache", "build", "T1")
	rule := wdRule(t, s, "no-activity", 3600000, "notify")
	svc := wdSvc(t, d)

	wdNow(t, s, svc, id, 2*time.Hour)
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 1 {
		t.Fatalf("setup: the first silence fired %d rows, want 1", n)
	}

	// Progress, then the same silence again: a new episode, and the rule —
	// still enabled — fires for it. The episode identity embeds the store's
	// millisecond activity stamp, so the progress step must land in a LATER
	// millisecond than the first; on a fast machine both can share one, the
	// episode id repeats, and the fire dedupes to a single row.
	time.Sleep(5 * time.Millisecond)
	wdStep(t, s, root, "wd-nocache", "build", "T1")
	wdNow(t, s, svc, id, 2*time.Hour)
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 2 {
		t.Fatalf("the new episode fired %d rows, want 2", n)
	}

	// The person disables the rule. Progress, the same silence, the next
	// tick: no third fire — the disable is live, with no cache between.
	if _, err := s.WatchdogSetEnabled(rule, false, "user", "op"); err != nil {
		t.Fatal(err)
	}
	wdStep(t, s, root, "wd-nocache", "build", "T1")
	wdNow(t, s, svc, id, 2*time.Hour)
	svc.tick(context.Background())
	if n := wdFireCount(t, s, id); n != 2 {
		t.Fatalf("after the disable the tick fired %d rows, want 2 — the rules are re-read every tick; a disable is live at the next one (behaviour 7)", n)
	}
}

// (10) The door's confirmation (PM7.1, the T8 shape): a disable and every
// removal commit only against the digest of the canonical statement the
// daemon rendered — the confirm-required refusal carries the statement and
// its digest (one round trip to completion), a wrong digest is a
// confirm-mismatch, and the committed act's audit shows the person the
// kernel stamped (user/4321), never a params field.
func TestWatchdogDisableNeedsDigest(t *testing.T) {
	d, _, s, _ := t15Daemon(t)
	ctx := context.Background()

	// A create tightens: it commits with no digest and is origin user.
	res, err := t15Dispatch(t, d, protocol.MethodWatchdogSet, protocol.WatchdogSetParams{
		Scope: "workspace", SubjectID: "workspace", Signal: "no-activity",
		Threshold: 1200000, Action: "notify",
	})
	if err != nil {
		t.Fatalf("watchdog.set (create): %v", err)
	}
	setRes := res.(protocol.WatchdogSetResult)
	if setRes.Effect != "created" || setRes.View == nil || setRes.View.Origin != "user" {
		t.Fatalf("the create answered effect=%s view=%+v, want created/user", setRes.Effect, setRes.View)
	}
	ruleID := setRes.View.ID

	// A disable loosens: without a digest it refuses confirm-required,
	// carrying the statement AND its digest (the client's one round trip).
	_, err = t15Dispatch(t, d, protocol.MethodWatchdogSetEnabled, protocol.WatchdogSetEnabledParams{ID: ruleID, Enabled: false})
	pe := wdErrorOf(t, err)
	if pe.Code != -31862 || pe.Data.Kind != "confirm-required" {
		t.Fatalf("the disable without a digest answered code %d kind %q, want -31862/confirm-required", pe.Code, pe.Data.Kind)
	}
	if pe.Data.Statement == "" || pe.Data.Confirm == "" {
		t.Fatalf("the confirm-required refusal carries no statement/digest (%+v) — the client cannot complete in one round trip", pe.Data)
	}
	sum := sha256.Sum256([]byte(pe.Data.Statement))
	if hex.EncodeToString(sum[:]) != pe.Data.Confirm {
		t.Fatalf("the digest is %s, want the sha256 of the statement it showed (%s) — a digest of anything else binds nothing", pe.Data.Confirm, hex.EncodeToString(sum[:]))
	}

	// A wrong digest is a confirm-mismatch (the row moved since the read).
	_, err = t15Dispatch(t, d, protocol.MethodWatchdogSetEnabled, protocol.WatchdogSetEnabledParams{ID: ruleID, Enabled: false, Confirm: strings.Repeat("0", 64)})
	pe = wdErrorOf(t, err)
	if pe.Code != -31862 || pe.Data.Kind != "confirm-mismatch" {
		t.Fatalf("the wrong digest answered kind %q, want confirm-mismatch", pe.Data.Kind)
	}

	// The right digest commits; the audit shows the kernel's user/4321.
	_, err = t15Dispatch(t, d, protocol.MethodWatchdogSetEnabled, protocol.WatchdogSetEnabledParams{ID: ruleID, Enabled: false, Confirm: pe.Data.Confirm})
	if err != nil {
		t.Fatalf("the disable with the right digest: %v", err)
	}
	r, err := s.GetWatchdogRule(ruleID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Enabled {
		t.Fatal("the disable committed but the row reads enabled")
	}
	evs, _ := s.ListWatchdogEvents(ruleID, 0)
	var sawDisabled bool
	for _, e := range evs {
		if e.Kind == "disabled" {
			sawDisabled = true
			if e.ActorKind != "user" || e.ActorID != "4321" {
				t.Fatalf("the disable audit is %s/%s, want user/4321 — the actor is the connection's peer credential", e.ActorKind, e.ActorID)
			}
		}
	}
	if !sawDisabled {
		t.Fatal("no kind-'disabled' audit row — the loosening act was not recorded")
	}

	// A re-enable tightens: it commits with NO digest (the default state).
	if _, err := t15Dispatch(t, d, protocol.MethodWatchdogSetEnabled, protocol.WatchdogSetEnabledParams{ID: ruleID, Enabled: true}); err != nil {
		t.Fatalf("the re-enable must be free (the tightening direction): %v", err)
	}
	r, _ = s.GetWatchdogRule(ruleID)
	if !r.Enabled {
		t.Fatal("the re-enable committed but the row reads disabled")
	}

	// A removal always confirms, any origin; the row is disabled, never
	// deleted (the audit and the id outlive it).
	_, err = t15Dispatch(t, d, protocol.MethodWatchdogRemove, protocol.WatchdogRemoveParams{ID: ruleID})
	pe = wdErrorOf(t, err)
	if pe.Code != -31862 || pe.Data.Kind != "confirm-required" {
		t.Fatalf("the removal without a digest answered kind %q, want confirm-required", pe.Data.Kind)
	}
	rem, err := t15Dispatch(t, d, protocol.MethodWatchdogRemove, protocol.WatchdogRemoveParams{ID: ruleID, Confirm: pe.Data.Confirm})
	if err != nil {
		t.Fatalf("the removal with the right digest: %v", err)
	}
	if got := rem.(protocol.WatchdogRemoveResult); !got.Removed {
		t.Fatalf("the removal answered %+v, want removed=true", got)
	}
	if _, err := s.GetWatchdogRule(ruleID); err != nil {
		t.Fatalf("the removed rule no longer resolves (%v) — a removal disables the row, never deletes it: the audit and the id must outlive it", err)
	}
	evs, _ = s.ListWatchdogEvents(ruleID, 0)
	var sawRemoved bool
	for _, e := range evs {
		if e.Kind == "removed" && e.ActorKind == "user" && e.ActorID == "4321" {
			sawRemoved = true
		}
	}
	if !sawRemoved {
		t.Fatal("no kind-'removed' audit row by user/4321 — the removal was not recorded as the person's act")
	}
	_ = ctx
}

// (11) P31's honest third state at the door — a rule on a signal whose source
// is absent in THIS store is refused (unavailable-signal), not silently
// inert; the vocabulary says available:false with the reason; and a signal
// whose source is present still commits. The partial schema is built the way
// a real one would become partial: every migration applied, then one source
// column dropped (step.context_manifest — the step-over-deadline's source).
func TestUnavailableSignalRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	// The raw build: the same driver and DSN store.Open composes, every
	// migration applied in order, then the one column dropped. SkipMigrations
	// on the real Open keeps the ledger out of it — this is a store a
	// migration did not reach, not one the runner half-walked.
	database.RegisterExtensions()
	raw, err := sql.Open(database.DriverName, "file:"+path+"?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range store.Migrations() {
		if _, err := raw.Exec(m.SQL); err != nil {
			raw.Close()
			t.Fatalf("migration %d (%s): %v", m.ID, m.Name, err)
		}
	}
	if _, err := raw.Exec("ALTER TABLE step DROP COLUMN context_manifest"); err != nil {
		raw.Close()
		t.Fatalf("drop the source column: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(store.Options{Dir: dir, File: "state.db", Env: func(string) string { return "" }, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open the partial store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// The daemon over the partial store, the way serve builds it.
	w := &wire{}
	d := New(Options{Store: s, Logger: w, Notify: w.notify,
		Connections: func() []localrpc.ConnInfo { return w.conns }})
	w.conns = []localrpc.ConnInfo{{ID: "c1", PeerUID: t15PeerUID, PeerPID: 99}}
	initConn(t, d, context.Background(), "c1", "board-app")

	// The vocabulary says available:false with the reason — the honest third
	// state, served, not omitted.
	res, err := t15Dispatch(t, d, protocol.MethodWatchdogVocabulary, nil)
	if err != nil {
		t.Fatalf("watchdog.vocabulary: %v", err)
	}
	vocab := res.(protocol.WatchdogVocabularyResult)
	var dead, alive *protocol.WatchdogSignalVocab
	for i := range vocab.Signals {
		switch vocab.Signals[i].Signal {
		case "step-over-deadline":
			dead = &vocab.Signals[i]
		case "no-activity":
			alive = &vocab.Signals[i]
		}
	}
	if dead == nil || dead.Available || dead.Reason == "" {
		t.Fatalf("step-over-deadline reads %+v, want available:false with the reason naming the missing source", dead)
	}
	if alive == nil || !alive.Available {
		t.Fatalf("no-activity reads %+v, want available:true — the probe is per-signal, not all-or-nothing", alive)
	}

	// A rule on the dead signal is refused at the door — never a silently
	// inert row.
	_, err = t15Dispatch(t, d, protocol.MethodWatchdogSet, protocol.WatchdogSetParams{
		Scope: "workspace", SubjectID: "workspace", Signal: "step-over-deadline",
		Threshold: 0, Action: "block",
	})
	pe := wdErrorOf(t, err)
	if pe.Code != -31862 || pe.Data.Kind != "unavailable-signal" {
		t.Fatalf("the rule on the dead signal answered code %d kind %q, want -31862/unavailable-signal", pe.Code, pe.Data.Kind)
	}
	// A rule on a live signal still commits (the refusal is the signal's,
	// not the store's).
	if _, err := t15Dispatch(t, d, protocol.MethodWatchdogSet, protocol.WatchdogSetParams{
		Scope: "workspace", SubjectID: "workspace", Signal: "no-activity",
		Threshold: 1200000, Action: "notify",
	}); err != nil {
		t.Fatalf("the rule on a live signal: %v — the door must not refuse what the store can serve", err)
	}
}
