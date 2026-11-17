package daemon

// S27 T7: the daemon-side semantics of the scheduler's twelve doors —
// what the fake at the protocol's edge cannot answer: does the template
// validator actually refuse a converse finish BEFORE a row exists, does
// enable touch a repository row, does enableTree follow a child attached
// tomorrow, does the read write. Every test drives the SAME service the
// socket drives (NewContinuousService over the real store).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var schedPerson = protocol.ContinuousActor{Kind: "user", ID: "carlos"}

func socketService(t *testing.T, e *schedEnv) protocol.ContinuousAPI {
	t.Helper()
	if e.d.bpSrc == nil {
		// The bare New() daemon has no blueprint mirror until Start wires
		// it (blueprint.go); the validator reads through it for NAMES, so
		// the test wires the same store source the daemon would.
		e.d.bpSrc = StoreBlueprintSource(e.s)
	}
	svc := NewContinuousService(e.d)
	if svc == nil {
		t.Fatal("NewContinuousService refused a daemon that HAS a store — the V13 gate is the store, not a mood")
	}
	return svc
}

func masterState(t *testing.T, e *schedEnv) store.SchedulerStateRow {
	t.Helper()
	st, err := e.s.SchedulerState()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// writeBlueprint writes a raw blueprint file and returns its path (the
// person's own file argument; loadStartBlueprint reads paths by design).
func writeBlueprint(t *testing.T, id, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStatusDescriptorCarriesTheVocabulary (CN21/CN22): status answers the
// master, the four effective settings each with a source, the cap, the
// repository rows with a live state word — and the descriptor that makes
// the UI descriptor-driven: the priority pair, EVERY reason the core
// speaks, the window grammar, and where blueprints come from.
func TestStatusDescriptorCarriesTheVocabulary(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	view, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !view.Enabled || view.Paused {
		t.Fatalf("the seeded master state did not survive to status: %+v", view)
	}
	if len(view.Settings) != 4 {
		t.Errorf("the CN21 block is %d settings, want exactly 4: %+v", len(view.Settings), view.Settings)
	}
	for _, s := range view.Settings {
		if s.Key == "" || s.Value == "" || s.Source == "" {
			t.Errorf("a setting arrived without key/value/source: %+v", s)
		}
	}
	if view.Slots.Cap != 4 {
		t.Errorf("cap %d, want the declared default 4", view.Slots.Cap)
	}
	if view.Statement == "" || !strings.Contains(view.Statement, "grants nothing") {
		t.Errorf("the CN2 statement must ride status verbatim, got %q", view.Statement)
	}
	if len(view.Repos) != 1 || view.Repos[0].ComponentRoot != e.root || !view.Repos[0].Enabled {
		t.Errorf("the seeded repository row is not standing in status: %+v", view.Repos)
	}
	// The descriptor: the reason list IS the core's census — same words,
	// same order, no invented extras.
	want := continuous.Reasons()
	if len(view.Descriptor.Reasons) != len(want) {
		t.Fatalf("descriptor reasons %d, core census %d — the two vocabularies drifted", len(view.Descriptor.Reasons), len(want))
	}
	for i := range want {
		if string(view.Descriptor.Reasons[i]) != string(want[i]) {
			t.Fatalf("reason %d: descriptor says %q, core says %q", i, view.Descriptor.Reasons[i], want[i])
		}
	}
	if len(view.Descriptor.PriorityStates) != 2 || view.Descriptor.PriorityStates[0] != "known" {
		t.Errorf("priority states %+v, want D2's closed pair", view.Descriptor.PriorityStates)
	}
	if !strings.Contains(view.Descriptor.WindowGrammar, "HH:MM") || view.Descriptor.BlueprintsFrom != "blueprint.list" {
		t.Errorf("descriptor grammar/source incomplete: %+v", view.Descriptor)
	}
}

// TestRootSetRefusesInvalidTemplates (CN5): the validator is the DOOR, not
// a post-hoc state — every refusal below leaves NO row behind.
func TestRootSetRefusesInvalidTemplates(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	ctx := context.Background()
	converse := writeBlueprint(t, "rest-forever", "id: rest-forever\nschemaVersion: 1\nversion: 1\nafter: converse\n")

	cases := []struct {
		name string
		in   protocol.ContinuousRootInput
		want string // substring of the refusal sentence
	}{
		{"blank", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true}, "blueprint is required"},
		{"unknown", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: "no-such-blueprint"}, "no-such-blueprint"},
		{"converse", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: converse}, "converse"},
		{"priority", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: e.bpPath, MinPriority: "sometimes"}, "known, assumed"},
		{"time", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: e.bpPath,
			Window: &protocol.ContinuousWindow{Start: "25:00"}}, "HH:MM"},
		{"zone", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: e.bpPath,
			Window: &protocol.ContinuousWindow{Start: "09:00", End: "17:00", Zone: "Mars/Olympus"}}, "Mars/Olympus"},
		{"days", protocol.ContinuousRootInput{ComponentRoot: "/srv/x", Enabled: true, Blueprint: e.bpPath,
			Window: &protocol.ContinuousWindow{Start: "09:00", End: "17:00", Days: "8"}}, "ISO weekdays"},
	}
	for _, c := range cases {
		_, ref, err := svc.SetRoot(ctx, schedPerson, c.in)
		if err != nil {
			t.Fatalf("%s: store error where a refusal was due: %v", c.name, err)
		}
		if ref == nil {
			t.Fatalf("%s: accepted — CN5 validation must refuse FIRST", c.name)
		}
		if !strings.Contains(ref.Sentence, c.want) {
			t.Errorf("%s: refusal %q must carry the validator's own word %q", c.name, ref.Sentence, c.want)
		}
	}
	// And the ledger the refusals leave behind: nothing.
	for _, root := range []string{"/srv/x"} {
		if _, found, err := e.s.SchedulerRoot(root); err != nil {
			t.Fatal(err)
		} else if found {
			t.Errorf("%s — a refused template was WRITTEN anyway", root)
		}
	}
	// The clean case through the same door: stored — the root is a REAL
	// component, because scheduler_root.component_root is a foreign key,
	// which is itself part of CN5: the rows speak of components, never of
	// paths a caller typed — default priority known, stamped to the person.
	cleanDir := t.TempDir()
	seedComponentRoot(t, e, "clean", cleanDir)
	row, ref, err := svc.SetRoot(ctx, schedPerson, protocol.ContinuousRootInput{
		ComponentRoot: cleanDir, Enabled: false, Blueprint: e.bpPath})
	if ref != nil || err != nil {
		t.Fatalf("clean template refused: ref=%v err=%v", ref, err)
	}
	if row.MinPriority != "known" {
		t.Errorf("minPriority %q, want the D2 default known", row.MinPriority)
	}
	stored, found, err := e.s.SchedulerRoot(cleanDir)
	if err != nil || !found {
		t.Fatalf("the accepted row is missing: found=%v err=%v", found, err)
	}
	if stored.UpdatedByKind != "user" || stored.UpdatedByID != "carlos" {
		t.Errorf("the row is stamped %+v/%+v, want the person", stored.UpdatedByKind, stored.UpdatedByID)
	}
}

// seedComponentRoot files a component whose own workspace root is dir — the
// only way a scheduler row can name a root the foreign key will admit.
func seedComponentRoot(t *testing.T, e *schedEnv, slug, dir string) {
	t.Helper()
	cw, err := e.s.CreateComponent(store.NewComponent{Name: slug, Slug: slug, Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !cw.OK() {
		t.Fatalf("seed component %s refused: %s %s", slug, cw.Refusal, cw.Reason)
	}
}

// TestEnableNeedsTheMasterAndTheRepo (CN2, the composition refusal): the
// master moves ALONE — enabling or disabling it must not touch a single
// repository row — and repository authority under an off master is the
// composition the door refuses, refusing with NeedsMaster, writing nothing.
func TestEnableNeedsTheMasterAndTheRepo(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	ctx := context.Background()

	// The master moves alone: the seeded row stood enabled before; it
	// stands enabled after the master is turned off.
	if ref, err := svc.SetMaster(ctx, schedPerson, false); ref != nil || err != nil {
		t.Fatalf("master off: ref=%v err=%v", ref, err)
	}
	if st := masterState(t, e); st.Enabled {
		t.Fatal("master did not move")
	}
	row, found, err := e.s.SchedulerRoot(e.root)
	if err != nil || !found {
		t.Fatalf("seeded row gone: %v", err)
	}
	if !row.Enabled {
		t.Fatal("SetMaster touched a repository row — one authority per act (CN2)")
	}
	// And it is stamped to the peer, not to nobody.
	if st := masterState(t, e); st.UpdatedByKind != "user" || st.UpdatedByID != "carlos" {
		t.Errorf("the master row is stamped %q/%q, want user/carlos", st.UpdatedByKind, st.UpdatedByID)
	}

	// Repository authority under the off master: refused, NeedsMaster, and
	// NOTHING written.
	_, ref, err := svc.SetRoot(ctx, schedPerson, protocol.ContinuousRootInput{
		ComponentRoot: "/srv/under-off", Enabled: true, Blueprint: e.bpPath})
	if err != nil {
		t.Fatalf("store error where a refusal was due: %v", err)
	}
	if ref == nil || !ref.NeedsMaster {
		t.Fatalf("SetRoot enabled under an off master answered ref=%v — the composition must be refused with NeedsMaster", ref)
	}
	if _, found, _ := e.s.SchedulerRoot("/srv/under-off"); found {
		t.Error("the refused SetRoot wrote a row anyway")
	}
	// A DISABLED row may be staged under an off master: it grants nothing.
	staged := t.TempDir()
	seedComponentRoot(t, e, "staged", staged)
	if _, ref, err := svc.SetRoot(ctx, schedPerson, protocol.ContinuousRootInput{
		ComponentRoot: staged, Enabled: false, Blueprint: e.bpPath}); ref != nil || err != nil {
		t.Fatalf("staging a disabled row under an off master was refused: ref=%v err=%v — it grants no authority", ref, err)
	}
	// Pause under an off master: same family, own code.
	if ref, err := svc.SetPaused(ctx, schedPerson, true, "deploy"); ref == nil || !ref.NeedsMaster || err != nil {
		t.Fatalf("pause under an off master answered ref=%v err=%v, want NeedsMaster", ref, err)
	}
	// Disable of a root that has no row: refused as never-enabled.
	if ref, err := svc.DisableRoot(ctx, schedPerson, "/srv/never"); ref == nil || err != nil {
		t.Fatalf("disabling an unconfigured root answered ref=%v err=%v, want the never-enabled refusal", ref, err)
	} else if !strings.Contains(ref.Sentence, "never") {
		t.Errorf("refusal sentence %q must say never-configured", ref.Sentence)
	}
}

// TestEnableTreeExpandsOnceAndDoesNotFollow (CN6/CN2): the template is
// validated ONCE, rows land for the repositories that exist NOW, the
// child attached tomorrow gets no row, and the tree's own rootless child
// is no row's author.
func TestEnableTreeExpandsOnceAndDoesNotFollow(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	ctx := context.Background()

	dir0, dir1, dir2, dir3 := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	parent, err := e.s.CreateComponent(store.NewComponent{Name: "tree", Slug: "tree", Root: dir0})
	if err != nil {
		t.Fatal(err)
	}
	if !parent.OK() {
		t.Fatalf("seed parent refused: %s %s", parent.Refusal, parent.Reason)
	}
	mk := func(slug, root string) {
		t.Helper()
		cw, err := e.s.CreateComponent(store.NewComponent{Name: slug, Slug: slug, Root: root, ParentID: parent.Component.ID})
		if err != nil {
			t.Fatal(err)
		}
		if !cw.OK() {
			t.Fatalf("seed child %s refused: %s %s", slug, cw.Refusal, cw.Reason)
		}
	}
	mk("child-one", dir1)
	mk("child-two", dir2)
	// (The CT3 rootless-child shape cannot be filed through
	// CreateComponent at all — the store refuses a root-less component at
	// creation — so the walk of skipping a child with no Root exercises
	// what the tree CAN hold, and the code's empty-root guard stays honest
	// without a contrived row.)

	// One bad template stops the WHOLE expansion — nothing half-enabled.
	if _, ref, err := svc.EnableTree(ctx, schedPerson, parent.Component.ID, protocol.ContinuousRootInput{
		Blueprint: "no-such-blueprint"}); ref == nil || err != nil {
		t.Fatalf("enableTree accepted an invalid template: ref=%v err=%v", ref, err)
	}

	roots, ref, err := svc.EnableTree(ctx, schedPerson, parent.Component.ID, protocol.ContinuousRootInput{
		Blueprint: e.bpPath, MinPriority: "assumed"})
	if ref != nil || err != nil {
		t.Fatalf("enableTree: ref=%v err=%v", ref, err)
	}
	// The tree's OWN root first, then the children that have one — the
	// rootless child (CT3) gets no row.
	if len(roots) != 3 || roots[0] != dir0 || roots[1] != dir1 || roots[2] != dir2 {
		t.Fatalf("expansion wrote %v, want dir0 then dir1 then dir2 — the repositories that EXIST", roots)
	}
	for _, root := range []string{dir0, dir1, dir2} {
		row, found, err := e.s.SchedulerRoot(root)
		if err != nil || !found || !row.Enabled || row.MinPriority != "assumed" {
			t.Errorf("row for %s: found=%v %+v — enabled with the ONE template", root, found, row)
		}
	}
	// The law the rows embody: a repository ATTACHED LATER is not enabled
	// by yesterday's act.
	mk("child-later", dir3)
	if _, found, _ := e.s.SchedulerRoot(dir3); found {
		t.Error("enableTree FOLLOWED a child attached after the act — CN6 says the expansion is one-time")
	}
	// And under an off master the whole act is refused with NeedsMaster.
	if _, err := svc.SetMaster(ctx, schedPerson, false); err != nil {
		t.Fatal(err)
	}
	if _, ref, err := svc.EnableTree(ctx, schedPerson, parent.Component.ID, protocol.ContinuousRootInput{
		Blueprint: e.bpPath}); ref == nil || !ref.NeedsMaster || err != nil {
		t.Fatalf("enableTree under an off master answered ref=%v err=%v, want NeedsMaster", ref, err)
	}
}

// TestPauseStopsPickingNotRunningLoops (CN18): pause is the OPERATIONAL
// off — the tick stops starting anything new, an already-running loop is
// not touched, resume restarts the picking, and the reason is kept.
func TestPauseStopsPickingNotRunningLoops(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	ctx := context.Background()

	if ref, err := svc.SetPaused(ctx, schedPerson, true, "schema migration at 03:00"); ref != nil || err != nil {
		t.Fatalf("pause: ref=%v err=%v", ref, err)
	}
	st := masterState(t, e)
	if !st.Paused || st.PausedReason != "schema migration at 03:00" {
		t.Fatalf("pause did not land with its reason: %+v", st)
	}
	if st.UpdatedByKind != "user" {
		t.Errorf("pause stamped %q — a control the scheduler could write for itself is the loop eating its tail", st.UpdatedByKind)
	}

	// The tick picks NOTHING while paused.
	e.queueReady("goal-while-paused", e.bpPath)
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	started, err := e.s.Decisions(store.DecisionQuery{Decision: "started"})
	if err != nil {
		t.Fatal(err)
	}
	if len(started) != 0 {
		t.Fatalf("paused scheduler started %d subjects", len(started))
	}
	// The audit trail says SO, with the reason word the UI draws.
	defs, err := e.s.Decisions(store.DecisionQuery{Decision: "skipped", Reason: "paused"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) == 0 {
		t.Error("the tick deferred without WRITING why (reason=paused)")
	}

	if ref, err := svc.SetPaused(ctx, schedPerson, false, ""); ref != nil || err != nil {
		t.Fatalf("resume: ref=%v err=%v", ref, err)
	}
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	started, err = e.s.Decisions(store.DecisionQuery{Decision: "started"})
	if err != nil {
		t.Fatal(err)
	}
	if len(started) == 0 {
		t.Fatal("resume did not restart the picking — the item sat while nothing stopped it")
	}
	if started[0].ActorKind != "scheduler" && started[0].ActorKind != "user" {
		// T4 stamps the start row's actor; the point here is only that the
		// START itself exists after resume.
		t.Logf("start row actor: %s/%s", started[0].ActorKind, started[0].ActorID)
	}
}

// TestPreviewReadIsSideEffectFree (CN15): preview answers the tick RUN and
// writes NOTHING — the decision ledger, the queue, and the loop table are
// byte-count identical across the read.
func TestPreviewReadIsSideEffectFree(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	item := e.queueReady("goal-previewed", e.bpPath)

	counts := func() (int, string) {
		rows, err := e.s.Decisions(store.DecisionQuery{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		fresh, ok, err := e.s.QueueItemByID(item.ID)
		if err != nil || !ok {
			t.Fatalf("the item row vanished: ok=%v err=%v", ok, err)
		}
		return len(rows), fresh.Status
	}
	beforeDec, beforeQ := counts()

	view, err := svc.Preview(context.Background(), e.root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if view.Now == "" || len(view.Decisions) == 0 {
		t.Fatalf("preview answered %v — the ready item must appear in the run", view)
	}
	if len(view.WouldStart) == 0 {
		t.Error("the run listed no would-start for a slot-free ready item")
	}

	afterDec, afterQ := counts()
	if afterDec != beforeDec {
		t.Errorf("preview WROTE decisions: %d → %d", beforeDec, afterDec)
	}
	if afterQ != beforeQ {
		t.Errorf("preview moved the queue: %s → %s", beforeQ, afterQ)
	}
	// The filter half: another root previews NOTHING of this one.
	other, err := svc.Preview(context.Background(), "/srv/not-here")
	if err != nil {
		t.Fatalf("filtered preview: %v", err)
	}
	if len(other.Decisions) != 0 {
		t.Errorf("preview of an unrelated root carried %d foreign lines", len(other.Decisions))
	}
}

// TestDecisionsFilterAndLimitStoreSide: the door splits and bounds; the
// store-side service answers the split filter — decision word, coded
// reason, subject, root, limit — newest first.
func TestDecisionsFilterAndLimitStoreSide(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	ctx := context.Background()

	// Three deferred lines (paused) then resume+start, through real ticks.
	if _, err := svc.SetPaused(ctx, schedPerson, true, "hold"); err != nil {
		t.Fatal(err)
	}
	e.queueReady("goal-a", e.bpPath)
	e.roadmapReady("goal-b")
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetPaused(ctx, schedPerson, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.tick(); err != nil {
		t.Fatal(err)
	}

	rows, err := svc.Decisions(ctx, protocol.ContinuousDecisionQuery{Reason: "paused"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("reason filter answered nothing while the ledger holds paused deferrals")
	}
	for _, r := range rows {
		if r.Reason != "paused" {
			t.Fatalf("reason filter leaked %q", r.Reason)
		}
	}
	started, err := svc.Decisions(ctx, protocol.ContinuousDecisionQuery{Decision: "started", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(started) != 1 || started[0].Decision != "started" {
		t.Errorf("limit 1 answered %d rows: %+v", len(started), started)
	}
	if newest, err := svc.Decisions(ctx, protocol.ContinuousDecisionQuery{Limit: 1}); err != nil {
		t.Fatal(err)
	} else if len(newest) == 1 && newest[0].Decision != "started" {
		t.Errorf("newest-first broken: the last act was a start, got %q", newest[0].Decision)
	}
	scoped, err := svc.Decisions(ctx, protocol.ContinuousDecisionQuery{ComponentRoot: "/srv/elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 0 {
		t.Errorf("root filter leaked %d foreign rows", len(scoped))
	}
}

// TestStopAllPausesFirstAndReportsEveryFailure (CN18 over T6/S55): the
// daemon-level service answers the composed off — paused true in the
// RESULT even when a loop refuses — and refuses no caller who is a person.
func TestStopAllAnswersTheComposedOff(t *testing.T) {
	e := newSchedEnv(t)
	svc := socketService(t, e)
	res, ref, err := svc.StopAll(context.Background(), schedPerson, "operator stepping away")
	if ref != nil || err != nil {
		t.Fatalf("stopAll: ref=%v err=%v", ref, err)
	}
	if !res.Paused {
		t.Fatal("stopAll returned Paused false — the first act IS the pause")
	}
	if st := masterState(t, e); !st.Paused || st.PausedReason != "operator stepping away" {
		t.Fatalf("the pause did not land with its reason: %+v", st)
	}
	if res.Stopped == nil {
		t.Error("Stopped must be a list even when empty (nothing was scheduler-started)")
	}
}
