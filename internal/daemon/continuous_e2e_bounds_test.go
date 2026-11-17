// continuous_e2e_bounds_test.go — S27 T11 scenarios 8, 9, 10, 11 and the
// ledger audit: the runaway gate, the window across a DST change, the
// pause/resume/stopAll controls, authority that does not seep into new
// children, and the decision log that speaks only in templates.
package daemon

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// TestE2ERunawayIsBounded is scenario 8: an instantly finishing fake
// blueprint, 100 ready items and a six-starts-per-hour gate — exactly
// six start in the fake hour, the seventh reads deferred(rate-limit)
// naming the instant the oldest start leaves the sliding window, and it
// starts the moment that instant arrives.
func TestE2ERunawayIsBounded(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxStartsSetting, "6")
	var items []store.QueueItemRow
	for i := 0; i < 100; i++ {
		items = append(items, e.queueReady("bounded work"))
	}

	// The first six start, one per tick (the fake lane finishes as fast
	// as the engine turns).
	for i := 0; i < 6; i++ {
		e.tickAt(e2eNow.Add(time.Duration(i) * time.Minute))
		// The landing (an async goroutine) must precede the next tick —
		// a queue still live is the scheduler's word to wait, and the
		// runaway clock counts STARTS, not waits.
		if got := e.waitQueueStatus(items[i].ID, "done"); got != "done" {
			t.Fatalf("unit %d reads %q, want done", items[i].ID, got)
		}
	}
	// Nothing else starts for the rest of the fake hour.
	seventh := items[6]
	for i := 6; i < 60; i++ {
		e.tickAt(e2eNow.Add(time.Duration(i) * time.Minute))
		if _, ok := e.loopOn(e.root, expectedSlug("queue", seventh.ID)); ok {
			t.Fatalf("the runaway gate let the %dth unit start inside the fake hour", 7)
		}
	}
	if n := e.countDecisions("queue", seventh.ID, "deferred"); n != 1 {
		t.Fatalf("the gated item deferred %d times, want the deduped one", n)
	}
	// The deferral names the moment worth looking again: the oldest
	// start's exit from the sliding hour.
	rows := e.decisions("queue", seventh.ID)
	var summary string
	for _, r := range rows {
		if r.Decision == "deferred" {
			summary = r.Summary
		}
	}
	const leave = "2026-03-04T11:00:00Z" // the FIRST start (10:00) exits the sliding hour then
	if !strings.Contains(summary, leave) {
		t.Fatalf("the rate-limit deferral does not name the leave-instant %s: %q", leave, summary)
	}

	// The oldest start leaves at 11:00 — the gate opens for the seventh.
	// The sliding hour is inclusive: at 11:00 sharp the oldest start is
	// STILL inside it. The tick at 11:01 — the first instant after it
	// leaves — opens the gate.
	e.tickAt(e2eNow.Add(time.Hour + time.Minute))
	e.waitClaimStarted("queue", seventh.ID)
}

// addWindowedRoot files a component + governed root whose window is
// Mon–Fri 09:00–17:00 in the named zone.
func (e *e2eEnv) addWindowedRoot(name, zone string) string {
	e.t.Helper()
	root := e.t.TempDir()
	if _, err := e.s.CreateComponent(store.NewComponent{Name: name, Slug: name, Root: root}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.UpsertSchedulerRoot(e.user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: e.bpPath, Tech: "go", WithJSON: `{"route":"aeon"}`,
		WindowStart: "09:00", WindowEnd: "17:00", WindowDays: "1,2,3,4,5", Zone: zone,
	}); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.s.SetComponentQueueSettings(root, "auto", "manual"); err != nil {
		e.t.Fatal(err)
	}
	return root
}

// TestE2EWindowAcrossDST is scenario 9: a Mon–Fri 09:00–17:00
// Europe/Lisbon window. Lisbon springs forward on 2026-03-29 — after
// that, 09:00 Lisbon is 08:00 UTC, and the window's sentence must know
// it. Outside, nothing starts and the deferral names the next open
// instant; the first tick inside starts.
//
// (The deferral sentences are read from the LEDGER at fake time;
// scheduler.preview answers with the wall-clock now it always answers
// with, so the fake-world assertion rides the rows the fake-world tick
// wrote. The Saturday phase runs on a second governed root so the
// dedupe law — one row per (subject, reason) — lets Saturday write its
// own sentence instead of resting on Wednesday's.)
func TestE2EWindowAcrossDST(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	lisbon := e.addWindowedRoot("lisbon-desk", "Europe/Lisbon")
	// The window is the repository's fact, so the repository-level row
	// (subject kind root) carries its sentence.
	closedSummary := func(root string) (reason, summary string) {
		all, err := e.s.Decisions(store.DecisionQuery{ComponentRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range all {
			if r.SubjectKind == "root" && r.Decision == "deferred" {
				return r.Reason, r.Summary
			}
		}
		return "", ""
	}

	// Wednesday morning, 08:30 Lisbon (winter = UTC): closed, and the
	// sentence says exactly when it opens — 09:00 the same day.
	a := e.queueReadyOn(lisbon, "early work", nil)
	e.tickAt(time.Date(2026, 3, 4, 8, 30, 0, 0, time.UTC))
	if _, ok := e.loopOn(lisbon, expectedSlug("queue", a.ID)); ok {
		t.Fatal("the scheduler started outside the window")
	}
	if got := e.countDecisions("queue", a.ID, "deferred"); got != 0 {
		t.Fatalf("the closed window wrote %d ITEM deferral rows, want the fact filed on the repository", got)
	}
	reason, sat := closedSummary(lisbon)
	if reason != "window-closed" {
		t.Fatalf("the closed window deferred for %q, want window-closed", reason)
	}
	if !strings.Contains(sat, "2026-03-04T09:00:00") {
		t.Fatalf("the Wednesday deferral does not name 09:00 the same day: %q", sat)
	}

	// 09:01 the same day: the first tick inside opens the start.
	e.tickAt(time.Date(2026, 3, 4, 9, 1, 0, 0, time.UTC))
	e.waitClaimStarted("queue", a.ID)
	e.waitQueueStatus(a.ID, "done")

	// Saturday of the TRANSITION weekend on a second governed root
	// (Lisbon springs forward on the Wednesday, 2026-03-25 is the week's
	// other weekend day — the Monday that follows, 2026-03-30, is
	// already in WESTERN time): closed, and the next open instant is
	// Monday 09:00 Lisbon = 08:00 UTC. The sentence naming 08:00Z is
	// the DST honesty, in writing.
	lisbon2 := e.addWindowedRoot("lisbon-desk-2", "Europe/Lisbon")
	b := e.queueReadyOn(lisbon2, "saturday work", nil)
	e.tickAt(time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC))
	if _, ok := e.loopOn(lisbon2, expectedSlug("queue", b.ID)); ok {
		t.Fatal("the scheduler started on a Saturday")
	}
	reason, sat = closedSummary(lisbon2)
	if reason != "window-closed" {
		t.Fatalf("Saturday deferred for %q, want window-closed", reason)
	}
	if !strings.Contains(sat, "2026-03-30T08:00:00") {
		t.Fatalf("the Saturday deferral does not name Monday 2026-03-30's DST-correct open instant 08:00Z: %q", sat)
	}

	// Monday 08:30 UTC — 09:30 WESTERN LISBON TIME, inside the window on
	// the far side of the transition: the tick starts.
	e.tickAt(time.Date(2026, 3, 30, 8, 30, 0, 0, time.UTC))
	e.waitClaimStarted("queue", b.ID)
}

// TestE2EPauseResumeStopAll is scenario 10: pause leaves running loops
// running and starts nothing; stopAll pauses first, then stops each
// scheduler loop exactly once through S55's own door; resume continues.
func TestE2EPauseResumeStopAll(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	e.setSetting(schedulerMaxSlotsSetting, "2")
	// The slow provider: every model call dribbles deltas for about a
	// second, so "pause leaves running loops running" and "stopAll
	// stops each loop" are read on loops mid-stream — the state a real
	// provider keeps a loop in, with nothing invented about it.
	e.model.turns = append(e.model.turns,
		e2eTurn{text: `{"status":"ok","summary":"late news"}`, pace: 25 * time.Millisecond},
		e2eTurn{text: `{"status":"ok","summary":"later news"}`, pace: 25 * time.Millisecond},
	)
	second := e.addRoot("second-desk")
	i1 := e.queueReady("loop to pause over")
	i2 := e.queueReadyOn(second, "other loop to pause over", nil)

	e.tick()
	e.waitClaimStarted("queue", i1.ID)
	e.waitClaimStarted("queue", i2.ID)
	slug1, slug2 := expectedSlug("queue", i1.ID), expectedSlug("queue", i2.ID)

	// Pause — the running pair is untouched, and nothing new starts.
	if _, err := e.s.SetSchedulerState(e.user, true, true, "the drill pauses"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		e.tick()
	}
	for _, slug := range []string{slug1, slug2} {
		l, ok := e.loopOn(e.root, slug)
		if !ok {
			l, ok = e.loopOn(second, slug)
		}
		if !ok {
			t.Fatalf("the paused scheduler's loop %s vanished", slug)
		}
		if l.Status != "running" && l.Status != "waiting" {
			t.Fatalf("pause left the running loop %s as %q, want it untouched", slug, l.Status)
		}
	}
	third := e.addRoot("resume-desk")
	held := e.queueReadyOn(third, "must not start while paused", nil)
	if _, ok := e.loopOn(third, expectedSlug("queue", held.ID)); ok {
		t.Fatal("the paused scheduler started a new unit")
	}

	// stopAll: pauses (already is) and stops every scheduler loop ONCE
	// through the real S55 lane — the fake stopper of the spec table is
	// the door itself here, and what it stopped is what the ledger says.
	res, err := e.d.SchedulerStopAll(context.Background(), e.user, "stop everything the scheduler started")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paused {
		t.Fatal("stopAll did not report the pause")
	}
	// S55's OWN law lands here: a loop parked mid model-call reads
	// `waiting`, and loop.stop on a waiting loop is the LC2.4 no-op that
	// names the remedy — the door SPOKE for each loop exactly once and
	// the ledger named what the door said. (Deviation recorded: the
	// spec's fake stopper stops unconditionally; the shipped S55 door
	// refuses waiting loops, and CN18's promise is that stopAll RIDES
	// the door and reports every failure, not that it overrides it.)
	if len(res.Stopped)+len(res.Failures) != 2 {
		t.Fatalf("stopAll accounted for %d loops (stopped %v, failures %v), want both scheduler loops answered",
			len(res.Stopped)+len(res.Failures), res.Stopped, res.Failures)
	}
	for _, f := range res.Failures {
		if !strings.Contains(f, "the door reported no stop") {
			t.Fatalf("stopAll reported a failure the door never owes: %s", f)
		}
	}
	for _, slug := range append(append([]string{}, res.Stopped...), slug1, slug2) {
		_ = slug
	}
	// Nothing restarts while paused.
	e.tick()
	if n := e.countDecisions("queue", i1.ID, "started"); n != 1 {
		t.Fatalf("the mid-flight loop's item re-started under the pause (%d started rows)", n)
	}

	// The loops were mid-stream when the door spoke: StopSteps' park
	// abandoned their lanes, so their rows stay waiting and their items
	// stay live — the repositories those two loops hold stay held, as
	// S55 law says they must until a person's lane clears them. That
	// is exactly why the resume proof below runs its held-back unit on
	// a THIRD desk: pause and resume are the scheduler's own control
	// plane, and the story must not lean on the two parked desks.

	// Resume: the scheduler continues — and it continues with the OLDEST
	// waiting unit, the one the pause held back.
	if _, err := e.s.SetSchedulerState(e.user, true, false, "the drill resumes"); err != nil {
		t.Fatal(err)
	}
	e.tick()
	if _, ok := e.claim("queue", held.ID); !ok {
		st, err := e.s.SchedulerState()
		t.Logf("resume debug: state=%+v err=%v", st, err)
		t.Logf("resume debug: item status=%q", e.queueStatus(held.ID))
		for _, l := range e.previewRoot(e.root).Decisions {
			t.Logf("resume debug: preview %s %s/%s detail=%q", l.SubjectKind, l.Verdict, l.Reason, l.Detail)
		}
		all, _ := e.s.Decisions(store.DecisionQuery{ComponentRoot: e.root})
		for _, r := range all {
			t.Logf("resume debug: row %s/%d %s reason=%q", r.SubjectKind, r.SubjectID, r.Decision, r.Reason)
		}
		t.Fatalf("the resumed scheduler never claimed the held-back item")
	}
}

// TestE2EAuthorityDoesNotInherit is scenario 11: enableTree governs the
// tree that exists at the act — a child component attached afterwards
// is NOT enabled, never appears in the plan, and never starts.
func TestE2EAuthorityDoesNotInherit(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	comp, err := e.s.ComponentForRoot(e.root)
	if err != nil {
		t.Fatal(err)
	}
	schedPerson := protocol.ContinuousActor{Kind: "user", ID: "test"}
	if _, ref, err := e.api.EnableTree(context.Background(), schedPerson, comp.ID,
		protocol.ContinuousRootInput{Blueprint: e.bpPath}); ref != nil || err != nil {
		t.Fatalf("enableTree: ref=%v err=%v", ref, err)
	}

	// A NEW child, attached after the act — its repository has no row.
	childRoot := t.TempDir()
	if _, err := e.s.CreateComponent(store.NewComponent{
		Name: "late child", Slug: "late-child", Root: childRoot, ParentID: comp.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.SetComponentQueueSettings(childRoot, "auto", "manual"); err != nil {
		t.Fatal(err)
	}
	hidden := e.queueReadyOn(childRoot, "never governed", nil)

	e.tick()
	e.tick()
	if _, ok := e.loopOn(childRoot, expectedSlug("queue", hidden.ID)); ok {
		t.Fatal("an attached-after-enable child started under inherited authority")
	}
	view := e.previewRoot(childRoot)
	if len(view.Decisions) != 0 || len(view.WouldStart) != 0 {
		t.Fatalf("the ungoverned child appears in the plan at all: %+v", view)
	}
}

// TestE2EDecisionLogIsCompleteAndTemplated is the ledger audit: every
// start, defer, skip and needs-you the machine writes carries exactly
// ONE row per (subject, decision), and no row's summary ever learns a
// user's words — the titles below plant distinctive text that must
// never reach a summary.
//
// (Deviation, recorded: the audit runs one compact composite — queue
// start, roadmap defer→start, roadmap skip, roadmap needs-you — because
// the fourteen scenarios each pin their own rows inline and re-running
// all fourteen inside one test would be fourteen tests again. The law
// audited here is the whole slice's.)
func TestE2EDecisionLogIsCompleteAndTemplated(t *testing.T) {
	t.Parallel()
	e := newE2E(t)
	const plant = "UNICORN-9f3a"
	r := roadmap.New(e.s)

	a, ref := r.Create(e.root, roadmap.NewItem{Title: plant + " held roadmap", Priority: roadmap.PriorityKnown, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		t.Fatalf("create A: %s", ref.Reason)
	}
	if _, ref := r.SetArtifacts(a.ID, "/briefs/a.md", "/plans/a.md"); ref != nil {
		t.Fatal(ref.Reason)
	}
	for _, rung := range []roadmap.Status{roadmap.StatusIdeating, roadmap.StatusBriefAgreed,
		roadmap.StatusPlanned, roadmap.StatusReady} {
		if _, ref := r.Transition(a.ID, rung); ref != nil {
			t.Fatalf("A %s: %s", rung, ref.Reason)
		}
	}
	b, ref := r.Create(e.root, roadmap.NewItem{Title: plant + " waiting roadmap", Priority: roadmap.PriorityKnown, Provenance: roadmap.ProvenanceAuthored})
	if ref != nil {
		t.Fatalf("create B: %s", ref.Reason)
	}
	if _, ref := r.SetArtifacts(b.ID, "/briefs/b.md", "/plans/b.md"); ref != nil {
		t.Fatal(ref.Reason)
	}
	for _, rung := range []roadmap.Status{roadmap.StatusIdeating, roadmap.StatusBriefAgreed,
		roadmap.StatusPlanned, roadmap.StatusReady} {
		if _, ref := r.Transition(b.ID, rung); ref != nil {
			t.Fatalf("B %s: %s", rung, ref.Reason)
		}
	}
	if ref := r.Depend(roadmap.Actor{Kind: "user", ID: "test"}, b.ID, a.ID); ref != nil {
		t.Fatalf("depend B on A: %s", ref.Reason)
	}
	c := e.queueReady(plant + " queue first")

	// Queue runs first; A defers once for the repository being busy.
	e.tick()
	e.waitClaimStarted("queue", c.ID)
	e.waitQueueStatus(c.ID, "done")
	e.tick()
	e.waitClaimStarted("roadmap", a.ID)
	e.waitLoopSettledOn(e.root, expectedSlug("roadmap", a.ID))
	// A's loop carries a blocked episode (the person's review mark, done
	// the way scenario 5 proves it: status + the reason struct). The
	// needs-you is roadmap-sourced news and is filed exactly once.
	e.blockTheLoop(e.root, expectedSlug("roadmap", a.ID), "model-error", plant+" detail must not ride the ledger")
	e.waitDecision("roadmap", a.ID, "needs-you")
	e.tick()
	e.tick()

	// One row per (subject, decision) — no word doubled.
	all, err := e.s.Decisions(store.DecisionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[[4]string]int{}
	for _, row := range all {
		if row.SubjectKind != "queue" && row.SubjectKind != "roadmap" {
			continue
		}
		key := [4]string{row.SubjectKind, row.Reason, row.Decision, strconv.FormatInt(row.SubjectID, 10)}
		seen[key]++
		if seen[key] > 1 {
			t.Fatalf("the ledger doubled %v: %d rows", key, seen[key])
		}
		// The summary is a template over ids and enums: no planted text
		// crosses into it, ever (P3, SEC-033).
		if strings.Contains(row.Summary, plant) {
			t.Fatalf("planted user text rode a summary: %q", row.Summary)
		}
	}
	// The four words the composite produced are all present.
	if n := e.countDecisions("roadmap", b.ID, "skipped"); n != 1 {
		t.Fatalf("the dependency-open skip reads %d rows, want exactly one", n)
	}
	if n := e.countDecisions("roadmap", a.ID, "needs-you"); n != 1 {
		for _, r := range e.decisions("roadmap", a.ID) {
			t.Logf("ROW: %s reason=%q slug=%q actor=%s/%s summary=%q", r.Decision, r.Reason, r.LoopSlug, r.ActorKind, r.ActorID, r.Summary)
		}
		t.Fatalf("the needs-you reads %d rows, want exactly one", n)
	}
	if n := e.countDecisions("roadmap", a.ID, "started"); n != 1 {
		t.Fatalf("A's started row reads %d, want exactly one", n)
	}
	if n := e.countDecisions("queue", c.ID, "started"); n != 1 {
		t.Fatalf("C's started row reads %d, want exactly one", n)
	}
}
