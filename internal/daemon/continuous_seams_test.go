package daemon

// continuous_seams_test.go — S27 T6: the seams the scheduler borrows. Every
// test here crosses a real slice boundary through the production seam: the
// grant is seeded through the store's own door (AL8's lock), the pin is read
// from the pin table the store writes, the budget is S55's row and S55's
// verdict, the stop rides S55's own stop door, and the attention kinds are
// the words the sibling slices park with. Nothing here stubs a sibling —
// faking a slice would fake the exact seam the task exists to prove.

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// t6Grants seeds the two grants the narrowing tests need: the outward class
// and, beside it, the merge by its own name with the forced condition and a
// bounded expiry (S56: an irreversible action is granted only by its name).
// Returns the root component id and the two grant ids.
func t6Grants(t *testing.T, e *schedEnv) (string, store.AutonomyGrant, store.AutonomyGrant) {
	t.Helper()
	comp, err := e.s.ComponentForRoot(e.root)
	if err != nil {
		t.Fatalf("root component: %v", err)
	}
	exp := time.Now().Add(3 * 24 * time.Hour)
	outward := seedAutonomyGrant(t, e.s, comp.ID, store.NewAutonomyGrant{
		SubjectKind: "class", Subject: "outward", State: "allow", Conditions: `{}`,
	})
	merge := seedAutonomyGrant(t, e.s, comp.ID, store.NewAutonomyGrant{
		SubjectKind: "action", Subject: "forge.merge", State: "allow",
		Conditions: `{"requireGates":true}`, ExpiresAt: &exp,
	})
	return comp.ID, outward, merge
}

// t6Pin reads the pin row a scheduler start wrote (found is false when the
// loop pinned nothing — the local-only shape).
func t6Pin(t *testing.T, e *schedEnv, loopID string) (store.AutonomyPin, bool) {
	t.Helper()
	p, found, err := e.s.ReadAutonomyPin(loopID)
	if err != nil {
		t.Fatalf("read pin: %v", err)
	}
	if !found {
		return store.AutonomyPin{}, false
	}
	return *p, true
}

func t6Started(t *testing.T, e *schedEnv, itemID int64) store.Loop {
	t.Helper()
	l, ok := e.loop(expectedSlug("queue", itemID))
	if !ok {
		t.Fatalf("queue item %d never started — decisions: %+v", itemID, e.decisions("queue", itemID))
	}
	return l
}

// TestGrantPinnedAtStartNeverMidLoop pins one of S56's laws from the
// scheduler's side: the snapshot is taken at start and NEVER restated — a
// grant revoked a moment later changes the NEXT decision's revision, not the
// running loop's pin.
func TestGrantPinnedAtStartNeverMidLoop(t *testing.T) {
	e := newSchedEnv(t)
	rootID, outward, _ := t6Grants(t, e)
	revBefore := func() int64 {
		rev, err := e.s.AutonomyRev(rootID)
		if err != nil {
			t.Fatal(err)
		}
		return rev
	}
	first := e.queueReady("pin me once", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	loopID := t6Started(t, e, first.ID).ID
	pin, found := t6Pin(t, e, loopID)
	if !found {
		t.Fatal("a root with grants started without a pin row")
	}
	if pin.Rev != revBefore() {
		t.Fatalf("pin rev %d is not the live revision %d at start", pin.Rev, revBefore())
	}
	if !strings.Contains(pin.Allowed, "forge.openPR") {
		t.Fatalf("the pin dropped the outward class: %s", pin.Allowed)
	}
	if _, _, ref, err := e.s.RevokeAutonomyGrant(store.Actor{Kind: "user", ID: "u1"}, outward.ID, "mid-loop test"); err != nil || ref != nil {
		t.Fatalf("revoke: refusal %+v err %v", ref, err)
	}
	if revBefore() <= pin.Rev {
		t.Fatal("the revocation did not move the live revision — the test proves nothing")
	}
	again, _ := t6Pin(t, e, loopID)
	if again.Rev != pin.Rev || again.Allowed != pin.Allowed {
		t.Fatalf("the loop's pin moved mid-loop: rev %d→%d", pin.Rev, again.Rev)
	}
	// Let the first loop FINISH (the engine's own road to a terminal) and
	// let the queue row leave its live status, so the second candidate
	// faces no occupancy; then start again — the SECOND decision reads
	// the NEW world.
	awaitSettled(t, e.s, loopID)
	// Settle the loop and the queue row exactly as T5's seeds do — the
	// gate blueprint parks rather than finishes, and the SECOND decision
	// needs a repository that reads free.
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: expectedSlug("queue", first.ID), ComponentRoot: e.root}, "done"); err != nil {
		t.Fatal(err)
	}
	rows, err := e.s.ListQueueItems(e.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == first.ID {
			if _, ref := queue.New(e.s).Transition(queueItemFromRow(r), queue.StatusDone); ref != nil {
				t.Fatalf("settle the queue row: %s", ref.Reason)
			}
		}
	}
	second := e.queueReady("pin me next", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	secondLoop := t6Started(t, e, second.ID)
	pin2, found := t6Pin(t, e, secondLoop.ID)
	// Only the outward CLASS was revoked — the merge grant stands, so the
	// SECOND pin must carry merge and have lost every outward entry the
	// first pin held. A revocation that reaches the running loop, or a
	// revocation that never reaches the next start, both fail here.
	if !found {
		t.Fatal("the second start pinned nothing, though the merge grant is still active")
	}
	if strings.Contains(pin2.Allowed, "forge.openPR") {
		t.Fatalf("the revoked class still rides the next pin: %s", pin2.Allowed)
	}
	if !strings.Contains(pin2.Allowed, "forge.merge") {
		t.Fatalf("the standing merge grant vanished: %s", pin2.Allowed)
	}
	ds := e.decisions("queue", second.ID)
	var started *store.SchedulerDecision
	for i := range ds {
		if ds[i].Decision == "started" {
			started = &ds[i]
		}
	}
	if started == nil {
		t.Fatal("no started row for the second item")
	}
	if started.GrantRevision == strconv.FormatInt(pin.Rev, 10) || started.GrantRevision == "" {
		t.Fatalf("the second start's revision %q is not the post-revocation one (pinned first loop: %d)",
			started.GrantRevision, pin.Rev)
	}
}

// TestItemCapOnlyNarrows walks the cap ladder over a root that grants
// everything: none pins nothing, outward keeps the outward entries and
// drops the merge, grant takes the pin as given — and no rung ever adds an
// entry the grant did not carry.
func TestItemCapOnlyNarrows(t *testing.T) {
	for _, tc := range []struct {
		cap     string
		wantPin bool
		wantPR  bool
		wantMrg bool
		eff     string
	}{
		{cap: "none", wantPin: false, wantPR: false, wantMrg: false, eff: "effective none"},
		{cap: "outward", wantPin: true, wantPR: true, wantMrg: false, eff: "effective outward"},
		{cap: "grant", wantPin: true, wantPR: true, wantMrg: true, eff: "effective outward+merge"},
	} {
		t.Run(tc.cap, func(t *testing.T) {
			e := newSchedEnv(t)
			t6Grants(t, e)
			item := e.queueReady("narrow me", e.bpPath)
			if err := e.s.SetItemAutonomyCap("queue", item.ID, tc.cap); err != nil {
				t.Fatal(err)
			}
			if err := e.tick(); err != nil {
				t.Fatal(err)
			}
			loopID := t6Started(t, e, item.ID).ID
			pin, found := t6Pin(t, e, loopID)
			if found != tc.wantPin {
				t.Fatalf("cap %q pinned %v, want %v (allowed %q)", tc.cap, found, tc.wantPin, pin.Allowed)
			}
			if got := strings.Contains(pin.Allowed, "forge.openPR"); got != tc.wantPR {
				t.Fatalf("cap %q outward entries: %v, want %v", tc.cap, got, tc.wantPR)
			}
			if got := strings.Contains(pin.Allowed, "forge.merge"); got != tc.wantMrg {
				t.Fatalf("cap %q merge entry: %v, want %v — a cap may only narrow", tc.cap, got, tc.wantMrg)
			}
			var started *store.SchedulerDecision
			for _, d := range e.decisions("queue", item.ID) {
				if d.Decision == "started" {
					started = &d
				}
			}
			if started == nil {
				t.Fatal("no started row")
			}
			if !strings.Contains(started.Summary, "cap "+tc.cap) || !strings.Contains(started.Summary, tc.eff) {
				t.Fatalf("the started row does not say the cap and the effective word (%q, %q): %s",
					"cap "+tc.cap, tc.eff, started.Summary)
			}
		})
	}
}

// TestMergeNeverFromAllowOutward holds the merge line from both sides. An
// outward-grant-only root pins outward and NO forge.merge — outward never
// implies the irreversible action (S19's invariant). And when merge IS
// granted, its permission rides the pin alone: allow_outward stays false,
// because a pin is an addition to the loop, never a widening of the legacy
// flag (S56 AL8).
func TestMergeNeverFromAllowOutward(t *testing.T) {
	t.Run("outward never implies merge", func(t *testing.T) {
		e := newSchedEnv(t)
		compID, _, _ := t6Grants(t, e)
		// Drop the merge grant: the outward CLASS stands alone.
		rows, err := e.s.ListAutonomyGrants(store.AutonomyGrantFilter{RootID: compID})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range rows {
			if g.Subject == "forge.merge" {
				if _, _, ref, err := e.s.RevokeAutonomyGrant(store.Actor{Kind: "user", ID: "u1"}, g.ID, "outward-only root"); err != nil || ref != nil {
					t.Fatalf("revoke merge: %+v %v", ref, err)
				}
			}
		}
		item := e.queueReady("outward only", e.bpPath)
		if err := e.tick(); err != nil {
			t.Fatal(err)
		}
		l := t6Started(t, e, item.ID)
		pin, found := t6Pin(t, e, l.ID)
		if !found {
			t.Fatal("an outward-grant root pinned nothing")
		}
		if !strings.Contains(pin.Allowed, "forge.openPR") {
			t.Fatalf("outward entries missing: %s", pin.Allowed)
		}
		if strings.Contains(pin.Allowed, "forge.merge") {
			t.Fatalf("outward implied the merge: %s", pin.Allowed)
		}
	})
	t.Run("merge rides the pin and never the flag", func(t *testing.T) {
		e := newSchedEnv(t)
		t6Grants(t, e)
		item := e.queueReady("merge is pinned, not flagged", e.bpPath)
		if err := e.tick(); err != nil {
			t.Fatal(err)
		}
		l := t6Started(t, e, item.ID)
		out, err := e.s.LoopAllowOutward(context.Background(), l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if out {
			t.Fatal("the scheduler moved allow_outward — a pin is not the outward lever (AL8)")
		}
		pin, found := t6Pin(t, e, l.ID)
		if !found || !strings.Contains(pin.Allowed, "forge.merge") {
			t.Fatalf("merge permission did not ride the pin (found %v, allowed %q)", found, pin.Allowed)
		}
	})
}

// TestNoGrantIsLocalOnly is the build-only machine: a root with no grants
// at all is NOT a missing seam — the loop starts, pins nothing, carries no
// outward flag, and its decision says so in one clause.
func TestNoGrantIsLocalOnly(t *testing.T) {
	e := newSchedEnv(t)
	item := e.queueReady("build only", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l := t6Started(t, e, item.ID)
	if _, found := t6Pin(t, e, l.ID); found {
		t.Fatal("a grantless start pinned something")
	}
	out, err := e.s.LoopAllowOutward(context.Background(), l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out {
		t.Fatal("a grantless loop carries outward the machine never earned")
	}
	var started *store.SchedulerDecision
	for _, d := range e.decisions("queue", item.ID) {
		if d.Decision == "started" {
			started = &d
		}
	}
	if started == nil {
		t.Fatal("a grantless root never started at all — GrantAbsent is not a no-grant")
	}
	if started.GrantRevision != "" || !strings.Contains(started.Summary, "grant none, cap grant, effective none") {
		t.Fatalf("the started row does not speak the local-only shape: %q / %s", started.GrantRevision, started.Summary)
	}
}

// TestGrantRevisionIsInTheDecisionAndTheLoop is CN16's join: one revision
// number, written twice — the decision that admitted the start and the pin
// the loop runs under — so the audit reads back through one value.
func TestGrantRevisionIsInTheDecisionAndTheLoop(t *testing.T) {
	e := newSchedEnv(t)
	rootID, _, _ := t6Grants(t, e)
	rev, err := e.s.AutonomyRev(rootID)
	if err != nil {
		t.Fatal(err)
	}
	item := e.queueReady("one number twice", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	l := t6Started(t, e, item.ID)
	pin, found := t6Pin(t, e, l.ID)
	if !found {
		t.Fatal("no pin row")
	}
	var started *store.SchedulerDecision
	for _, d := range e.decisions("queue", item.ID) {
		if d.Decision == "started" {
			started = &d
		}
	}
	if started == nil {
		t.Fatal("no started row")
	}
	if pin.Rev != rev {
		t.Fatalf("pin rev %d, live rev %d", pin.Rev, rev)
	}
	if started.GrantRevision != strconv.FormatInt(rev, 10) {
		t.Fatalf("decision grant revision %q, live rev %d", started.GrantRevision, rev)
	}
}

// TestBudgetExhaustionDefersAndNothingIsClaimed is CN5's shape at the
// scheduler's door: S55's exhaustion stops the tick BEFORE the plan claims
// anything — the item stays untouched, no claim row exists, no loop exists,
// and the decision quotes S55's own sentence.
func TestBudgetExhaustionDefersAndNothingIsClaimed(t *testing.T) {
	e := newSchedEnv(t)
	zero := int64(0)
	b, ref, err := e.s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, store.NewBudgetIn{
		Scope: "workspace", Subject: "workspace", Window: "lifetime", Steps: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Kind != "" {
		t.Fatalf("seed budget refused: %s", ref.Error())
	}
	_ = b
	item := e.queueReady("never claimed", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	var deferred *store.SchedulerDecision
	for _, d := range e.decisions("queue", item.ID) {
		if d.Decision == "deferred" && d.Reason == "budget-exhausted" {
			deferred = &d
		}
	}
	if deferred == nil {
		t.Fatalf("no budget-exhausted deferral for the item: %+v", e.decisions("queue", item.ID))
	}
	if !strings.Contains(deferred.Summary, "budget") {
		t.Fatalf("the deferral does not quote S55's sentence: %s", deferred.Summary)
	}
	if _, found := e.claim("queue", item.ID); found {
		t.Fatal("exhaustion still claimed the item — the claim is the door's, not the plan's")
	}
	if _, found := e.loop(expectedSlug("queue", item.ID)); found {
		t.Fatal("exhaustion started a loop")
	}
}

// TestCapHitHoldsTheRepoAndNeverRestarts parks a loop the way S55 parks
// one — blocked — and lets the scheduler wake a hundred times over it. A
// parked loop is not the scheduler's to raise, resume or stop (CN21: the
// human's doors own the way out); the repository just stands still. Only
// then does a person move it, through a person's door.
func TestCapHitHoldsTheRepoAndNeverRestarts(t *testing.T) {
	e := newSchedEnv(t)
	one := 1.0
	b, bref, err := e.s.CreateBudget(store.BudgetActor{Kind: "user", ID: "u1"}, store.NewBudgetIn{
		Scope: "workspace", Subject: "workspace", Window: "lifetime", USD: &one,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bref.Kind != "" {
		t.Fatalf("seed budget refused: %s", bref.Error())
	}
	// The park as the engine leaves it: blocked, lastReason cap-hit, and
	// the cap record the block wrote into the loop's durable position —
	// exactly what a raise needs to find the budget it lifts.
	parked, err := e.s.CreatePinnedLoop(store.NewLoop{
		Slug: "cap-hit-parked", ComponentRoot: e.root, Root: e.root,
		Status: "blocked", StartedBy: "scheduler", TriggerID: "7",
	}, "sched-gate", 1, queueBlueprintYAML,
		`{"lastReason":"cap-hit","capHit":{"budgetId":"`+b.ID+`","scope":"workspace","subject":"workspace","dimension":"usd","spendFloor":1,"cap":1,"reason":"cap reached"}}`)
	if err != nil {
		t.Fatal(err)
	}
	item := e.queueReady("held behind a park", e.bpPath)
	eventsBefore := t6ControlEvents(t, e, parked.ID)
	for i := 0; i < 100; i++ {
		if err := e.tick(); err != nil {
			t.Fatal(err)
		}
	}
	if l, ok := e.loop("cap-hit-parked"); !ok || l.Status != "blocked" {
		t.Fatalf("the scheduler moved a parked loop: %+v, %v", l, ok)
	}
	if got := t6ControlEvents(t, e, parked.ID); got != eventsBefore {
		t.Fatalf("the scheduler touched the park's ledger: %d events appeared", got-eventsBefore)
	}
	if _, found := e.loop(expectedSlug("queue", item.ID)); found {
		t.Fatal("the scheduler started behind a parked loop")
	}
	// A person's door moves it: blocked/stuck is resume's to answer (S55
	// G) — the same door the operator reads in the park's remedy: the
	// raise lifts the cap the park recorded and moves the loop off
	// blocked (E1 — no suspended step row, a fresh runner re-dispatches).
	if _, refusal, err := e.d.RaiseLoop(context.Background(),
		store.Actor{Kind: "user", ID: "u1"}, parked.ID, 25, "operator raises"); err != nil || refusal != nil {
		t.Fatalf("a person could not lift the park: refusal %+v err %v", refusal, err)
	}
	l, ok := e.loop("cap-hit-parked")
	if !ok || l.Status == "blocked" {
		t.Fatalf("the person's door did not move the loop: %+v, %v", l, ok)
	}
}

func t6ControlEvents(t *testing.T, e *schedEnv, loopID string) int {
	t.Helper()
	rows, err := e.s.ListControlEvents(loopID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// TestStopAllPausesThenStops is CN18's order read from the rows: the pause
// lands first (the switch is off before the loops come down), every loop
// the scheduler started is put down through S55's own stop door, and a
// person's loop is none of the scheduler's business.
func TestStopAllPausesThenStops(t *testing.T) {
	e := newSchedEnv(t)
	for _, slug := range []string{"sched-stop-a", "sched-stop-b"} {
		if _, err := e.s.CreatePinnedLoop(store.NewLoop{
			Slug: slug, ComponentRoot: e.root, Root: e.root,
			Status: "running", StartedBy: "scheduler", TriggerID: "9",
		}, "sched-gate", 1, queueBlueprintYAML, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.s.CreatePinnedLoop(store.NewLoop{
		Slug: "persons-own", ComponentRoot: e.root, Root: e.root,
		Status: "running", StartedBy: "user",
	}, "sched-gate", 1, queueBlueprintYAML, "{}"); err != nil {
		t.Fatal(err)
	}
	res, err := e.d.SchedulerStopAll(context.Background(),
		store.SchedulerActor{Kind: "user", ID: "u1"}, "operator pulled the switch")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paused {
		t.Fatal("stopAll did not pause")
	}
	if len(res.Failures) > 0 {
		t.Fatalf("stopAll reported failures: %v", res.Failures)
	}
	if strings.Join(res.Stopped, ",") != "sched-stop-a,sched-stop-b" {
		t.Fatalf("stopped %v", res.Stopped)
	}
	st, err := e.s.SchedulerState()
	if err != nil {
		t.Fatal(err)
	}
	if !st.Paused {
		t.Fatal("the state row is not paused")
	}
	for _, slug := range []string{"sched-stop-a", "sched-stop-b"} {
		l, ok := e.loop(slug)
		if !ok || l.Status == "running" {
			t.Fatalf("%s is still running after stopAll: %+v", slug, l)
		}
		if evs, err := e.s.ListControlEvents(l.ID, 100); err != nil || len(evs) == 0 {
			t.Fatalf("%s left no stop on the control ledger: %d events err %v", slug, len(evs), err)
		}
	}
	if l, ok := e.loop("persons-own"); !ok || l.Status != "running" {
		t.Fatalf("stopAll touched a person's loop: %+v, %v", l, ok)
	}
}

// TestStopAllUnregisteredWithoutS55 is V13 in stopAll's shape: with no
// store wired there is nothing to pause and nothing to stop, and the
// command says so as a fact — the method itself stays unregistered on that
// daemon (T7's census), which the wiring gate the census consults reports.
func TestStopAllUnregisteredWithoutS55(t *testing.T) {
	if schedulerStopAllWired(nil) {
		t.Fatal("nil daemon reports a wired stopAll")
	}
	bare := New(Options{})
	if schedulerStopAllWired(bare) {
		t.Fatal("a storeless daemon reports a wired stopAll")
	}
	if _, err := bare.SchedulerStopAll(context.Background(),
		store.SchedulerActor{Kind: "user", ID: "u1"}, "there is no store"); err == nil {
		t.Fatal("a storeless stopAll succeeded — V13 wants the absent fact, not a success")
	}
	e := newSchedEnv(t)
	if !schedulerStopAllWired(e.d) {
		t.Fatal("a wired daemon reports no stopAll")
	}
}

// TestNeedsYouKindsVerbatim is CN24's display law: the kinds the sibling
// slices park with arrive UNCHANGED and carry only the note this map owns;
// an unknown kind rides verbatim with no note at all — S27 invents no
// sentences for kinds it did not park.
func TestNeedsYouKindsVerbatim(t *testing.T) {
	for _, tc := range []struct{ kind, note string }{
		{"cap-hit", "raise the cap with aeon loop raise or stop the loop"},
		{"gate-failed", ""},
		{"post-merge-red", ""},
		{"a-kind-nobody-has-seen", ""},
	} {
		gotKind, gotNote := attentionDisplay(tc.kind)
		if gotKind != tc.kind {
			t.Errorf("kind %q arrived as %q", tc.kind, gotKind)
		}
		if gotNote != tc.note {
			t.Errorf("kind %q carried note %q, want %q", tc.kind, gotNote, tc.note)
		}
	}
}
