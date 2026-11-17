package daemon

// records_continuous_test.go — S27 T8 (CN23): the scheduler family's agent
// arms. What each test pins, in order:
//
//   - V13's shape: an unmounted plane is NAMED by every arm, never answered
//     zero (TestSchedulerArmsStateAnAbsentPlane);
//   - rule 4 in bytes: the tool's answer IS the door's answer for the same
//     repository — same service, same call, same marshalling
//     (TestSchedulerToolResultsMatchSocketResults);
//   - MC2's own-view: the step's root is stamped from the store, a foreign
//     repository is ABSENT (the door sees it; the tool does not name it,
//     count it, or refuse it with a revealing sentence), and a smuggled
//     componentRoot dies at the parse (TestSchedulerToolsAreScopedToTheirOwnRepository);
//   - the header's honesty: master, settings and slots are machine truth and
//     answer even for a repository the scheduler does not govern
//     (TestSchedulerStatusHeaderAnswersWithoutARootRow);
//   - both directions: a person session is turned back to the plane's own
//     doors, never served the step's narrowed view.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// schedRecordsEnv: the records identity harness (records_test.go's
// component tree, loop and step) plus the daemon whose Continuous seam the
// arms must ride, plus the door side (a SECOND ContinuousService over the
// same store — parity must be two instances answering one fact, not one
// instance compared with itself).
type schedRecordsEnv struct {
	base recordsEnv
	d    *Daemon
	s    *store.Store
	door protocol.ContinuousAPI
}

func newSchedRecordsEnv(t *testing.T) *schedRecordsEnv {
	t.Helper()
	base := newRecordsEnv(t, "")
	d := New(Options{Store: base.s})
	if d.recordsSvc == nil {
		t.Fatal("the daemon built no records service — the arms have nothing to run on")
	}
	if d.recordsSvc.continuousPlane == nil {
		t.Fatal("the daemon did not hand the records service the scheduler's own seam — the read arms would refuse (V13) on a wired mount")
	}
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := base.s.SetSchedulerState(user, true, false, ""); err != nil {
		t.Fatal(err)
	}
	return &schedRecordsEnv{base: base, d: d, s: base.s, door: NewContinuousService(d)}
}

// seedSchedulerRoot puts a registered component under the scheduler with
// the daemon's own write (the SAME store path the human door takes).
func seedSchedulerRoot(t *testing.T, st *store.Store, root, blueprint string) {
	t.Helper()
	user := store.SchedulerActor{Kind: "user", ID: "test"}
	if _, err := st.UpsertSchedulerRoot(user, store.SchedulerRootRow{
		ComponentRoot: root, Enabled: true, Blueprint: blueprint, Tech: "go",
	}); err != nil {
		t.Fatalf("seed scheduler root %s: %v", root, err)
	}
}

func seedDecision(t *testing.T, st *store.Store, root, subjectKind string, subjectID int64, decision, reason, summary, loopSlug string) {
	t.Helper()
	if _, err := st.AppendDecision(store.SchedulerDecision{
		ComponentRoot: root, SubjectKind: subjectKind, SubjectID: subjectID,
		Decision: decision, Reason: reason, Summary: summary, LoopSlug: loopSlug,
		ActorKind: "scheduler", ActorID: "tick",
	}); err != nil {
		t.Fatalf("seed decision: %v", err)
	}
}

// seedBlockedLoop files a loop on a root and walks its status to the
// derived openness Attention reads: a needs-you line is only OPEN while
// its loop still sits blocked or waiting (T7's derivation, unchanged).
func seedBlockedLoop(t *testing.T, st *store.Store, root, slug string) {
	t.Helper()
	if _, err := st.CreateLoop(store.NewLoop{Slug: slug, ComponentRoot: root, Title: "F", Goal: "g"}); err != nil {
		t.Fatalf("seed loop %s: %v", slug, err)
	}
	if err := st.SetLoopStatus(store.Scope{LoopSlug: slug, ComponentRoot: root}, "blocked"); err != nil {
		t.Fatalf("block loop %s: %v", slug, err)
	}
}

func schedArmDo(e *schedRecordsEnv, tool, input string) (any, *Failure, error) {
	return e.d.recordsSvc.Do(e.base.ctx, Actor{Kind: "agent", ID: e.base.agent}, tool, json.RawMessage(input))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestSchedulerArmsStateAnAbsentPlane is V13's shape for the family: a
// mount without the Continuous seam names the absence in every one of the
// four doors — never zero, never an empty plan.
func TestSchedulerArmsStateAnAbsentPlane(t *testing.T) {
	e := newSchedRecordsEnv(t)
	e.d.recordsSvc.continuousPlane = nil
	for _, tc := range []struct{ tool, input string }{
		{"scheduler_status", `{}`},
		{"scheduler_preview", `{}`},
		{"scheduler_decisions", `{}`},
		{"scheduler_attention", `{}`},
	} {
		_, f, err := schedArmDo(e, tc.tool, tc.input)
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "needs the scheduler plane") {
			t.Errorf("%s answered %v — the absent plane is stated, never faked (V13)", tc.tool, f)
		}
	}
}

// TestSchedulerToolResultsMatchSocketResults is CN23's parity law — T8's
// named test — rule 4 pinned in bytes: for the step's own repository the
// arm's answer is the DOOR's answer, field-for-field (marshalled), of two
// service instances over one store.
func TestSchedulerToolResultsMatchSocketResults(t *testing.T) {
	e := newSchedRecordsEnv(t)
	bp := writeGateBlueprint(t, "sched-arm")
	seedSchedulerRoot(t, e.s, "/repo/alpha", bp)
	seedSchedulerRoot(t, e.s, "/repo/org-group", bp)
	seedDecision(t, e.s, "/repo/alpha", "queue", 7, "deferred", "capacity", "queue/7 waits for a slot", "")
	seedDecision(t, e.s, "/repo/alpha", "queue", 8, "started", "", "queue/8 started a loop", "")
	// An OPEN needs-you on the step's own root (walk is blocked) and an
	// OPEN one on the foreign root (its own filed loop) — attention has
	// real lines both sides of the scope line.
	// walk is recordsEnv's own loop on /repo/alpha — walk its status to the
	// derived openness, do not re-file it.
	if err := e.s.SetLoopStatus(store.Scope{LoopSlug: "walk", ComponentRoot: "/repo/alpha"}, "blocked"); err != nil {
		t.Fatal(err)
	}
	seedDecision(t, e.s, "/repo/alpha", "roadmap", 9, "needs-you", "cap-hit", "roadmap/9 waits for a person", "walk")
	seedBlockedLoop(t, e.s, "/repo/org-group", "foreign")
	seedDecision(t, e.s, "/repo/org-group", "roadmap", 10, "needs-you", "conflicted", "roadmap/10 waits outside this step's repository", "foreign")
	ctx := context.Background()

	// decisions: the door over the OWN root, filter grammar and all.
	doorRows, err := e.door.Decisions(ctx, protocol.ContinuousDecisionQuery{ComponentRoot: "/repo/alpha"})
	if err != nil {
		t.Fatal(err)
	}
	got, f, err := schedArmDo(e, "scheduler_decisions", `{}`)
	if err != nil || f != nil {
		t.Fatalf("arm decisions: f=%v err=%v", f, err)
	}
	if b, want := mustMarshal(t, got), mustMarshal(t, doorRows); string(b) != string(want) {
		t.Errorf("the ledger tool answered %s, the door %s — the two doors are drifting", b, want)
	}
	// A filter the door would honour is honoured identically (limit bound
	// shared through protocol.ParseContinuousDecisionFilters).
	limited, err := json.Marshal(map[string]any{"decision": "deferred", "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	doorOne, err := e.door.Decisions(ctx, protocol.ContinuousDecisionQuery{ComponentRoot: "/repo/alpha", Decision: "deferred", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	got, f, err = schedArmDo(e, "scheduler_decisions", string(limited))
	if err != nil || f != nil {
		t.Fatalf("arm decisions (filtered): f=%v err=%v", f, err)
	}
	if b, want := mustMarshal(t, got), mustMarshal(t, doorOne); string(b) != string(want) {
		t.Errorf("the filtered ledger tool answered %s, the door %s", b, want)
	}
	// attention: the open needs-you rows for the own root, byte for byte.
	doorAtt, err := e.door.Attention(ctx, "/repo/alpha")
	if err != nil {
		t.Fatal(err)
	}
	got, f, err = schedArmDo(e, "scheduler_attention", `{}`)
	if err != nil || f != nil {
		t.Fatalf("arm attention: f=%v err=%v", f, err)
	}
	if b, want := mustMarshal(t, got), mustMarshal(t, doorAtt); string(b) != string(want) {
		t.Errorf("the attention tool answered %s, the door %s", b, want)
	}
	// preview: the same plan the next tick would execute, line for line
	// (the snapshot's clock word is the only field that may differ between
	// two calls, so the decision lines are compared as the plan itself).
	doorPrev, err := e.door.Preview(ctx, "/repo/alpha")
	if err != nil {
		t.Fatal(err)
	}
	got, f, err = schedArmDo(e, "scheduler_preview", `{}`)
	if err != nil || f != nil {
		t.Fatalf("arm preview: f=%v err=%v", f, err)
	}
	armPrev, ok := got.(protocol.ContinuousPreview)
	if !ok {
		t.Fatalf("preview arm answered %T", got)
	}
	if b, want := mustMarshal(t, armPrev.Decisions), mustMarshal(t, doorPrev.Decisions); string(b) != string(want) {
		t.Errorf("the preview tool answered %s, the door %s", b, want)
	}
	// status: the header is the door's header, and the repository's own
	// rows are the door's rows for that repository.
	doorView, err := e.door.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, f, err = schedArmDo(e, "scheduler_status", `{}`)
	if err != nil || f != nil {
		t.Fatalf("arm status: f=%v err=%v", f, err)
	}
	view, ok := got.(protocol.ContinuousStatusView)
	if !ok {
		t.Fatalf("status arm answered %T", got)
	}
	if view.Enabled != doorView.Enabled || view.Paused != doorView.Paused {
		t.Errorf("the header is %v/%v, the door's %v/%v", view.Enabled, view.Paused, doorView.Enabled, doorView.Paused)
	}
	if b, want := mustMarshal(t, view.Settings), mustMarshal(t, doorView.Settings); string(b) != string(want) {
		t.Errorf("the settings list drifted: tool %s, door %s", b, want)
	}
	var ownRows []protocol.ContinuousRepoView
	for _, r := range doorView.Repos {
		if r.ComponentRoot == "/repo/alpha" {
			ownRows = append(ownRows, r)
		}
	}
	if len(ownRows) != 1 {
		t.Fatalf("the door lists no single alpha row: %v", doorView.Repos)
	}
	if b, want := mustMarshal(t, view.Repos), mustMarshal(t, ownRows); string(b) != string(want) {
		t.Errorf("the own rows drifted: tool %s, door %v", b, mustMarshal(t, doorView.Repos))
	}
	// The needs-you count the tool answers is the OWN repository's — the
	// door's own attention read, not the machine sum.
	doorForeign, err := e.door.Attention(ctx, "/repo/org-group")
	if err != nil {
		t.Fatal(err)
	}
	if doorForeign.Open == 0 {
		t.Fatal("the fixture's foreign needs-you row vanished — the scoping assertion below would be vacuous")
	}
	if view.Attention.Open != doorAtt.Open {
		t.Errorf("the tool counted %d open items, the own-root door %d — never the machine's sum (MC2)", view.Attention.Open, doorAtt.Open)
	}
}

// TestSchedulerToolsAreScopedToTheirOwnRepository is MC2: the rows exist —
// the door sees them — and the step's tools simply do not have them. A
// smuggled scope field is a parse error, not a query.
func TestSchedulerToolsAreScopedToTheirOwnRepository(t *testing.T) {
	e := newSchedRecordsEnv(t)
	bp := writeGateBlueprint(t, "sched-arm")
	seedSchedulerRoot(t, e.s, "/repo/alpha", bp)
	seedSchedulerRoot(t, e.s, "/repo/org-group", bp)
	seedBlockedLoop(t, e.s, "/repo/org-group", "foreign")
	seedDecision(t, e.s, "/repo/org-group", "roadmap", 11, "needs-you", "conflicted", "roadmap/11 waits outside this step's repository", "foreign")
	ctx := context.Background()
	doorView, err := e.door.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(doorView.Repos) != 2 {
		t.Fatalf("the door sees %d repositories — the fixture lost its foreign root", len(doorView.Repos))
	}
	got, f, err := schedArmDo(e, "scheduler_status", `{}`)
	if err != nil || f != nil {
		t.Fatalf("status: f=%v err=%v", f, err)
	}
	view := got.(protocol.ContinuousStatusView)
	if len(view.Repos) != 1 || view.Repos[0].ComponentRoot != "/repo/alpha" {
		t.Errorf("the step saw %v — a foreign repository is absent, never listed (MC2)", view.Repos)
	}
	// The ledger: the foreign row exists at the door and is not in the
	// tool's answer, and the tool cannot ask for it by name.
	doorRows, err := e.door.Decisions(ctx, protocol.ContinuousDecisionQuery{ComponentRoot: "/repo/org-group", Decision: "needs-you"})
	if err != nil || len(doorRows) != 1 {
		t.Fatalf("the door's foreign needs-you ledger answered %v rows, err %v", len(doorRows), err)
	}
	got, f, err = schedArmDo(e, "scheduler_decisions", `{}`)
	if err != nil || f != nil {
		t.Fatalf("decisions: f=%v err=%v", f, err)
	}
	if b := mustMarshal(t, got); strings.Contains(string(b), "roadmap/11") || strings.Contains(string(b), "/repo/org-group") {
		t.Errorf("the step's ledger read reached a foreign repository: %s", b)
	}
	for _, tool := range []string{"scheduler_status", "scheduler_preview", "scheduler_decisions", "scheduler_attention"} {
		_, f, err := schedArmDo(e, tool, `{"componentRoot":"/repo/org-group"}`)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "stamped by the step") {
			t.Errorf("%s accepted a smuggled componentRoot (f=%v) — a scope field a model could type is a scope field that lies (OL3)", tool, f)
		}
	}
	// preview and attention: only own-root facts.
	got, f, err = schedArmDo(e, "scheduler_preview", `{}`)
	if err != nil || f != nil {
		t.Fatalf("preview: f=%v err=%v", f, err)
	}
	if b := mustMarshal(t, got); strings.Contains(string(b), "/repo/org-group") {
		t.Errorf("the preview named a foreign repository: %s", b)
	}
	got, f, err = schedArmDo(e, "scheduler_attention", `{}`)
	if err != nil || f != nil {
		t.Fatalf("attention: f=%v err=%v", f, err)
	}
	att := got.(protocol.ContinuousAttention)
	if att.Open != 0 || len(att.Items) != 0 {
		t.Errorf("the step's needs-you list holds %v — the foreign repository's open item is absent, never named (MC2)", att.Items)
	}
}

// TestSchedulerStatusHeaderAnswersWithoutARootRow pins the honest degraded
// shape: the master, the settings and the slots are machine truth and
// answer; the step's repository has no rows because the scheduler does not
// govern it — zero rows and zero open items, with the header intact. This
// is NOT an empty pretending to be a board: the header is what the spec
// calls the status header, and it is true.
func TestSchedulerStatusHeaderAnswersWithoutARootRow(t *testing.T) {
	e := newSchedRecordsEnv(t)
	got, f, err := schedArmDo(e, "scheduler_status", `{}`)
	if err != nil || f != nil {
		t.Fatalf("status: f=%v err=%v", f, err)
	}
	view := got.(protocol.ContinuousStatusView)
	if !view.Enabled {
		t.Error("the seeded master is enabled and the header says otherwise")
	}
	if len(view.Settings) != 4 || view.Slots.Cap != declaredMaxSlotsDefault {
		t.Errorf("the header's settings/slots are %v / %v — the declared default answers and says so", view.Settings, view.Slots)
	}
	if len(view.Repos) != 0 || view.Attention.Open != 0 {
		t.Errorf("an ungoverned repository answered %v repos, %d open — zero rows is the honest zero, the header is the truth around it", view.Repos, view.Attention.Open)
	}
}

// TestSchedulerArmsRefuseAnUnresolvedRoot is OL3's other edge: a step whose
// loop's root cannot be resolved meets the named refusal — the header-only
// answer above requires a RESOLVED identity with no scheduler row; an
// identity that resolves nothing answers nothing.
func TestSchedulerArmsRefuseAnUnresolvedRoot(t *testing.T) {
	e := newSchedRecordsEnv(t)
	bp := writeGateBlueprint(t, "sched-arm")
	seedSchedulerRoot(t, e.s, "/repo/alpha", bp)
	// An identity with no component row: a context without a step at all.
	_, f, err := e.d.recordsSvc.Do(context.Background(), Actor{Kind: "agent", ID: e.base.agent}, "scheduler_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "need a step") {
		t.Errorf("a stepless call answered %v — identity is read from the store, never guessed (OL3)", f)
	}
}

// TestSchedulerToolsTurnPersonsToTheirOwnDoors is the both-directions law:
// a person holding the endpoint holds scheduler.status unscoped — the
// records door is the step's narrowed view and answering a person through
// it would make a stamped root an oracle.
func TestSchedulerToolsTurnPersonsToTheirOwnDoors(t *testing.T) {
	e := newSchedRecordsEnv(t)
	for tool, door := range map[string]string{
		"scheduler_status":    "scheduler.status",
		"scheduler_preview":   "scheduler.preview",
		"scheduler_decisions": "scheduler.decisions",
		"scheduler_attention": "scheduler.attention",
	} {
		_, f, err := e.d.recordsSvc.Do(e.base.ctx, Actor{Kind: "user", ID: "operator"}, tool, json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, door) {
			t.Errorf("%s served a person the step's view (f=%v) — the sentence must name %s", tool, f, door)
		}
	}
}
