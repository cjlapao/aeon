package daemon

// start_loop.go (S40 T4) lifts the StartBlueprintLoop closure body out of
// blueprint.go so an event fire can ride the same door with provenance,
// depth and a kickoff. A lift is only safe when what it carries is recorded
// first: this file pins the socket start path's success shape BEFORE the
// move (TestTheSocketStartPathSuccessShapeIsRecorded was written and green
// against the unextracted closure) and keeps S20's fire door honest after it
// (TestAScheduleFireStillStampsTriggerProvenanceAfterTheExtraction).
//
// Gate-blueprint harness: a gate step parks the loop waiting for an answer
// with no model behind it — the start completes, the runner idles, and the
// assertions read what the PIN wrote, which is exactly the surface T4
// touches.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/automation"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// writeGateBlueprint files a gate blueprint (the loop parks at the question;
// no model needed) and returns its path — the shape
// TestBlueprintStartTrimsWorkspaceBeforePinning pins.
func writeGateBlueprint(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), id+".yaml")
	content := "id: " + id + "\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: approve\n    step: {kind: gate, question: proceed}\nquestions:\n  proceed:\n    prompt: Continue?\n    answers: [yes, no]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// waitGateParked blocks until the loop parks at its gate and the runner is
// idle — the same 2-second bargain blueprint_workspace_test.go makes, so a
// leaked runner can never outlive the store into somebody else's test.
func waitGateParked(t *testing.T, s *store.Store, d *Daemon, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		pin, err := s.ReadPinnedLoop(id)
		if err != nil {
			t.Fatal(err)
		}
		if pin.Loop.Status == "waiting" && !d.loopRunner.Active(id) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gate runner did not finish before store cleanup: %s", pin.Loop.Status)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestTheSocketStartPathSuccessShapeIsRecorded is the pre-extraction record
// (T4 step 1, green BEFORE startLoopWith existed): the socket's start answers
// with exactly id, slug, blueprint, status=running, and a person's loop
// carries no provenance and depth 0. If the extraction changes any byte of
// this answer, this test says so.
func TestTheSocketStartPathSuccessShapeIsRecorded(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	bp := writeGateBlueprint(t, "socket-shape")
	res, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{
		Blueprint: bp, Goal: "recorded shape", WorkspaceRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("the socket start returned %T, want map[string]any", res)
	}
	if len(m) != 4 {
		t.Errorf("result keys = %v, want exactly id, slug, blueprint, status", m)
	}
	for _, k := range []string{"id", "slug", "blueprint", "status"} {
		if _, ok := m[k].(string); !ok {
			t.Errorf("result key %q is missing or not a string: %v", k, m)
		}
	}
	if m["status"] != "running" {
		t.Errorf("status = %v, want running", m["status"])
	}
	id, _ := m["id"].(string)
	waitGateParked(t, s, d, id)
	pin, err := s.ReadPinnedLoop(id)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.StartedBy != "" || pin.Loop.TriggerID != "" {
		t.Errorf("a person's loop carries provenance %q/%q, want none", pin.Loop.StartedBy, pin.Loop.TriggerID)
	}
	if pin.Loop.AutomationDepth != 0 {
		t.Errorf("automation_depth = %d on a socket start, want 0", pin.Loop.AutomationDepth)
	}
}

// TestAScheduleFireStillStampsTriggerProvenanceAfterTheExtraction is the
// S20 half of the same promise: the schedule door was not edited to reach
// S40, so its fire still stamps started_by='trigger', the trigger id,
// attended=0 and depth 0 — the extraction must not have moved its columns.
func TestAScheduleFireStillStampsTriggerProvenanceAfterTheExtraction(t *testing.T) {
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	bp := writeGateBlueprint(t, "schedule-shape")
	f := trigger.Fire{TriggerID: "sched1", Provenance: trigger.ProvenanceTrigger, Blueprint: bp,
		Workspace: root, Instant: time.UnixMilli(1_800_000_000_000).UTC()}
	id, err := d.StartFromTrigger(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, s, d, id)
	pin, err := s.ReadPinnedLoop(id)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.StartedBy != "trigger" || pin.Loop.TriggerID != "sched1" {
		t.Errorf("S20's provenance moved: %q/%q, want trigger/sched1", pin.Loop.StartedBy, pin.Loop.TriggerID)
	}
	if pin.Attended == nil || *pin.Attended {
		t.Errorf("a schedule fire attended = %v, want a declared false", pin.Attended)
	}
	if pin.Loop.AutomationDepth != 0 {
		t.Errorf("a schedule fire carries automation_depth %d, want 0 (S20 knows nothing of depth)", pin.Loop.AutomationDepth)
	}
}

// ── the event door (T4 steps 5–6) ──────────────────────────────────────────

// seedApprovedGoal files a goal through S38's doors and approves it; the
// return is the id events fire at.
func seedApprovedGoal(t *testing.T, s *store.Store) string {
	t.Helper()
	p, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(p) == 0 {
		t.Fatalf("no project to hang a goal on: %v", err)
	}
	g, rf, err := s.CreateGoal(store.NewGoal{ComponentID: p[0].ID, Title: "event goal", Type: "feature",
		BodyNote: "the body says go", SourceKind: "manual", SourceRef: "test",
		By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("goal: %v %v", err, rf)
	}
	if _, rf, err := s.DecideGoal(g.ID, g.Version, "approved", store.Actor{Kind: "user", ID: "operator"}, "ok"); err != nil || rf != nil {
		t.Fatalf("approve: %v %v", err, rf)
	}
	return g.ID
}

func TestAnEventStartIsUnattendedAndRecordsEventProvenanceAndDepth(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	bp := writeGateBlueprint(t, "ev-shape")
	gid := seedApprovedGoal(t, s)
	st, err := d.eventStarter().Start(context.Background(), automation.EventStartRequest{
		TriggerID: "tr1", TriggerName: "auto-dev", Blueprint: bp, WorkspaceRoot: root,
		Slug: automation.AutoSlug("tr1", 7), Event: "goal.approved", SubjectKind: "goal",
		SubjectID: gid, SubjectVersion: 2, Depth: 2,
	})
	if err != nil {
		t.Fatalf("event start: %v", err)
	}
	if st.Slug != automation.AutoSlug("tr1", 7) {
		t.Errorf("slug = %q, want the deterministic %q", st.Slug, automation.AutoSlug("tr1", 7))
	}
	waitGateParked(t, s, d, st.LoopID)
	pin, err := s.ReadPinnedLoop(st.LoopID)
	if err != nil {
		t.Fatal(err)
	}
	if pin.Loop.StartedBy != "event" || pin.Loop.TriggerID != "tr1" {
		t.Errorf("provenance = %q/%q, want event/tr1", pin.Loop.StartedBy, pin.Loop.TriggerID)
	}
	if pin.Loop.AutomationDepth != 2 {
		t.Errorf("automation_depth = %d, want 2 (the request's depth, written at the pin)", pin.Loop.AutomationDepth)
	}
	att, err := s.LoopAttended(context.Background(), st.LoopID)
	if err != nil || att == nil || *att {
		t.Errorf("attended = %v (err %v), want a DECLARED false — never-recorded would be a leak of AU2)", pin.Attended, err)
	}
	out, err := s.LoopAllowOutward(context.Background(), st.LoopID)
	if err != nil || out {
		t.Errorf("allow_outward = %v (err %v), want false — an unattended start opts into nothing outward (AU2)", out, err)
	}
}

func TestTheKickoffIsTheFirstMessageInLoopsOwnTransaction(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	bp := writeGateBlueprint(t, "ev-kickoff")
	gid := seedApprovedGoal(t, s)
	st, err := d.eventStarter().Start(context.Background(), automation.EventStartRequest{
		TriggerID: "tr1", TriggerName: "auto-dev", Blueprint: bp, WorkspaceRoot: root,
		Slug: automation.AutoSlug("tr1", 8), Event: "goal.approved", SubjectKind: "goal",
		SubjectID: gid, Depth: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Read BEFORE parking: the kickoff must be visible the moment Start
	// returns — it rides the pin transaction, which committed before Start
	// did. (The gate's question row is seq 2, after it.)
	msgs, err := s.Thread(store.Scope{LoopSlug: st.Slug, ComponentRoot: root}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("the thread has %d messages, want exactly the kickoff: %+v", len(msgs), msgs)
	}
	waitGateParked(t, s, d, st.LoopID) // then rest, before t.Cleanup closes the store
	m := msgs[0]
	if m.Source != "automation" || m.SourceRef != "tr1" {
		t.Errorf("kickoff source = %q/%q, want automation/tr1 (which trigger spoke)", m.Source, m.SourceRef)
	}
	if m.Author != "system" {
		t.Errorf("kickoff author = %q, want system (S39's KickoffAuthor rule)", m.Author)
	}
	// No template: S39's default text is the goal body (the gate blueprint
	// carries no kickoff of its own).
	if m.Body != "event goal" {
		t.Errorf("kickoff body = %q, want the goal's default text", m.Body)
	}
	if m.Seq != 1 {
		t.Errorf("kickoff seq = %d, want 1 — the first words", m.Seq)
	}
}

func TestAnEventLoopHasItsKickoffBeforeTheRunnerCouldStep(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	gid := seedApprovedGoal(t, s)
	slug := automation.AutoSlug("tr9", 12)
	// The scripted runner records the thread at the FIRST model dispatch —
	// the earliest moment a step could have run anything. If the kickoff
	// were stamped after the runner started (S20's order), this read would
	// catch an empty thread.
	var mu sync.Mutex
	var seen []store.ChatMessage
	dispatched := false
	dep := loop.Deps{Store: s, Event: func(string, any) {}, Records: recordsReaderFor(s),
		Model: func(_ context.Context, state loop.LoopState, _ blueprint.Step, _ string, _ string) (loop.ModelResult, error) {
			mu.Lock()
			if !dispatched {
				dispatched = true
				seen, _ = s.Thread(store.Scope{LoopSlug: slug, ComponentRoot: root}, 0)
			}
			mu.Unlock()
			return loop.ModelResult{StepID: 1, Kind: "completed", Outcome: "ok"}, nil
		}}
	d.loopRunner = loop.NewRunner(dep)
	st, err := d.eventStarter().Start(context.Background(), automation.EventStartRequest{
		TriggerID: "tr9", TriggerName: "brief", Blueprint: "goal-to-brief", WorkspaceRoot: root,
		Slug: slug, Event: "goal.approved", SubjectKind: "goal", SubjectID: gid, Depth: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		done := dispatched
		mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if !dispatched {
		t.Fatalf("the scripted model never dispatched for loop %s — the test would pass vacuously, so it fails", st.LoopID)
	}
	// Let the scripted run reach its end before t.Cleanup closes the
	// store: an alive runner mid-advance against a closed pool is the
	// panic this family has always been.
	deadline = time.Now().Add(5 * time.Second)
	for d.loopRunner.Active(st.LoopID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(seen) != 1 || seen[0].Source != "automation" || seen[0].SourceRef != "tr9" {
		t.Errorf("at the first step, the thread was %+v — the kickoff must exist BEFORE the runner could step (AU9)", seen)
	}
}

func TestAKickoffTemplateOnTheTriggerOverridesTheBlueprintDefault(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	comp, err := s.ComponentForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	task, rf, err := s.CreateManualTask(store.NewManualTask{ComponentID: comp.ID, Title: "Fix the sink", Type: "chore",
		By: store.Actor{Kind: "user", ID: "operator"}})
	if err != nil || rf != nil {
		t.Fatalf("task: %v %v", err, rf)
	}
	fire := func(tmpl string, outbox int64) string {
		t.Helper()
		st, err := d.eventStarter().Start(context.Background(), automation.EventStartRequest{
			TriggerID: "tr-t", TriggerName: "task-bot", Blueprint: writeGateBlueprint(t, "tmpl"), WorkspaceRoot: root,
			Slug: automation.AutoSlug("tr-t", outbox), Event: "task.approved",
			SubjectKind: "task", SubjectID: task.ID, KickoffTemplate: tmpl, Depth: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		msgs, _ := s.Thread(store.Scope{LoopSlug: st.Slug, ComponentRoot: root}, 0)
		if len(msgs) != 1 {
			t.Fatalf("want exactly one kickoff message (the gate question comes after), got %d: %+v", len(msgs), msgs)
		}
		body := msgs[0].Body
		waitGateParked(t, s, d, st.LoopID)
		return body
	}
	if got := fire("Fix it: {{.Task}}", 1); !strings.HasPrefix(got, "Fix it: Fix the sink") {
		t.Errorf("trigger template rendered %q — AU10's precedence says the trigger's template wins, with the task's words in it", got)
	}
	if got := fire("", 2); got != "Fix the sink" {
		t.Errorf("empty template gave %q, want S39's default body text (the task's own words)", got)
	}
}

func TestEventStartRefusalsAreStructuredAndVerbatim(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	gid := seedApprovedGoal(t, s)
	proj, err := s.ListComponents(store.ComponentFilter{RootsOnly: true})
	if err != nil || len(proj) == 0 {
		t.Fatal("no project")
	}
	lonely, rf, err := s.CreateGoal(store.NewGoal{ComponentID: proj[0].ID, Title: "unjudged", Type: "feature",
		SourceKind: "manual", SourceRef: "test", By: store.Actor{Kind: "user", ID: "operator"}, Submit: true})
	if err != nil || rf != nil {
		t.Fatalf("in_review goal: %v %v", err, rf)
	}
	base := func() automation.EventStartRequest {
		return automation.EventStartRequest{TriggerID: "tr-r", TriggerName: "r", Blueprint: writeGateBlueprint(t, "refuse"),
			WorkspaceRoot: root, Slug: automation.AutoSlug("tr-r", time.Now().UnixNano()%1_000_000),
			Event: "goal.approved", SubjectKind: "goal", SubjectID: gid, Depth: 1}
	}
	cases := []struct {
		what, reason, contains string
		mutate                 func(*automation.EventStartRequest)
	}{
		{"unknown blueprint", "blueprint-refused", "no-such-bp", func(r *automation.EventStartRequest) { r.Blueprint = "no-such-bp" }},
		{"missing workspace", "workspace-invalid", "", func(r *automation.EventStartRequest) { r.WorkspaceRoot = filepath.Join(root, "gone") }},
		{"vanished subject", "subject-missing", "does not exist", func(r *automation.EventStartRequest) { r.SubjectID = "goal-nope" }},
		{"bad template field", "kickoff-invalid", "", func(r *automation.EventStartRequest) { r.KickoffTemplate = "{{.Nope}}" }},
		{"requires unmet", "requires-unmet", "approved", func(r *automation.EventStartRequest) {
			r.Blueprint = "goal-to-brief"
			r.SubjectID = lonely.ID // in_review — the clause wants approved
		}},
	}
	for i, c := range cases {
		req := base()
		req.Slug = automation.AutoSlug("tr-r", int64(i+1))
		c.mutate(&req)
		_, err := d.eventStarter().Start(context.Background(), req)
		var ref *automation.Refusal
		if !errors.As(err, &ref) {
			t.Errorf("%s: err = %v, want a *automation.Refusal", c.what, err)
			continue
		}
		if ref.Reason != c.reason {
			t.Errorf("%s: reason = %q, want %q", c.what, ref.Reason, c.reason)
		}
		if c.contains != "" && !strings.Contains(ref.Error(), c.contains) {
			t.Errorf("%s: sentence %q does not contain %q", c.what, ref.Error(), c.contains)
		}
		if ref.Sentence == "" {
			t.Errorf("%s: refusal carries no verbatim sentence", c.what)
		}
	}
	// None of the refusals left a loop behind.
	loops, err := s.ListLoops(store.ComponentScope{ComponentRoot: root})
	if err != nil || len(loops) != 0 {
		t.Errorf("%d loops after only refusals: %v (err %v)", len(loops), loops, err)
	}
}

func TestAnUnclassifiedStartErrorIsNotARefusal(t *testing.T) {
	s, root := t14Env(t)
	d := New(Options{Store: s})
	bp := writeGateBlueprint(t, "plain-err")
	comp, err := s.ComponentForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	task, rf, err := s.CreateManualTask(store.NewManualTask{ComponentID: comp.ID, Title: "link me", Type: "chore",
		By: store.Actor{Kind: "user", ID: "operator"}})
	if err != nil || rf != nil {
		t.Fatalf("task: %v %v", err, rf)
	}
	req := automation.EventStartRequest{TriggerID: "tr-d", TriggerName: "dup", Blueprint: bp, WorkspaceRoot: root,
		Slug: automation.AutoSlug("tr-d", 3), Event: "task.approved", SubjectKind: "task", SubjectID: task.ID, Depth: 1}
	first, err := d.eventStarter().Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitGateParked(t, s, d, first.LoopID)
	// The same (trigger, outbox row) replayed. Measured at T4: `loop` has NO
	// uniqueness on (component_root, slug) — the deterministic claim is
	// T5's fire ledger, not a slug constraint — so the twin pin lands and
	// the task's worker link is what rejects it: task_loop's unique index
	// answers with a plain store error. That is "cannot say", not a
	// dressed-up decision. If this returns a *Refusal, somebody is
	// flattening infrastructure into verdicts.
	_, err = d.eventStarter().Start(context.Background(), req)
	if err == nil {
		t.Fatal("the twin start succeeded — the worker link did not reject it after all")
	}
	var ref *automation.Refusal
	if errors.As(err, &ref) {
		t.Errorf("a plain database constraint came back as %q — only the five listed classes are refusals (AU12)", ref.Reason)
	}
}

func TestTheEventStarterHasExactlyOneAttendedAndNoOutwardAssignment(t *testing.T) {
	raw, err := os.ReadFile("event_fire.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if n := len(regexp.MustCompile(`Attended:`).FindAllString(src, -1)); n != 1 {
		t.Errorf("event_fire.go assigns Attended: %d times, want exactly 1 (AU2: authored here, once)", n)
	}
	if n := len(regexp.MustCompile(`attended := false`).FindAllString(src, -1)); n != 1 {
		t.Errorf("`attended := false` occurs %d times, want exactly 1", n)
	}
	if strings.Contains(src, "AllowOutward:") {
		t.Error("event_fire.go sets AllowOutward — the outward lever is absent from the event path, not defaulted (AU2, brief §0.1)")
	}
}

func TestStartFromTriggerCarriesTheAutonomyConsultation(t *testing.T) {
	// S40's extraction kept the fire door (trigger_fire.go) untouched and ran
	// through new files only; S56 T4 is the task that legitimately edits it —
	// to add the AL8 autonomy consultation at pin time. This inverts S40's old
	// guard (TestStartFromTriggerIsUntouched): the fire door now routes through
	// the ONE start door, carrying the pin as an extra and consulting
	// resolveAutonomy once — while the S20 provenance authorship (SetLoopProvenance)
	// stays. A fire that consulted the policy by a second, parallel path would
	// drift from the start door, so the wiring is pinned here, not by rumour.
	raw, err := os.ReadFile("trigger_fire.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, "resolveAutonomy") {
		t.Error("the fire door must consult resolveAutonomy at pin time (S56 AL8)")
	}
	if !strings.Contains(src, "startLoopWith") {
		t.Error("the fire door must route through the one start door (S56 T4), not StartBlueprintLoop")
	}
	if !strings.Contains(src, "Autonomy:") {
		t.Error("the fire door must carry the pin as a startExtra (S56 AL8), never on the wire")
	}
	if !strings.Contains(src, "SetLoopProvenance") {
		t.Error("the S20 provenance authorship must stay — the fire door records started_by/trigger_id")
	}
}

// TestAnAgentFiledTaskCarriesItsLoopsAsCause wires T2's flagged line: the
// stamped task_create passes the step's loop as the write's CAUSE, so the
// outbox row copies that loop's depth and trigger (AU5) instead of today's
// honest zero. Without the wiring the task event says depth 0 and no
// trigger — a true statement about a person, and a lie about this agent.
func TestAnAgentFiledTaskCarriesItsLoopsAsCause(t *testing.T) {
	e := newRecordsEnv(t, "feature-loop")
	in := store.NewLoop{Slug: "auto-trorig-5", ComponentRoot: e.comp.Root, Title: "chat",
		StartedBy: "event", TriggerID: "tr-orig", AutomationDepth: 2}
	pin, err := e.s.CreatePinnedLoop(in, "bp-cause", 1, "raw bytes", `{"route":""}`)
	if err != nil {
		t.Fatal(err)
	}
	sid := startLaneStep(t, e.s, in.Slug, e.comp.Root, "planner")
	prev := isPlainChat // the door is chat-only (OL14); stubbed per the spec's own pattern
	isPlainChat = func(Identity) bool { return true }
	defer func() { isPlainChat = prev }()
	content, isErr := e.svc.Invoker()(stepctx.WithStepID(context.Background(), sid), "task_create",
		json.RawMessage(`{"title":"from the chat","manual":true,"type":"chore"}`))
	if isErr {
		t.Fatalf("manual task_create: %s", content)
	}
	events, err := e.s.ReadAutomationEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.AutomationEvent
	for i, ev := range events {
		if ev.SubjectKind == "task" {
			found = &events[i]
		}
	}
	if found == nil {
		t.Fatalf("the manual task emitted no outbox row (content %q)", content)
	}
	if found.Depth != 2 || found.CausedByTrigger != "tr-orig" {
		t.Errorf("task event depth/cause = %d/%q, want 2/tr-orig — read from loop %s (AU5's cascade)", found.Depth, found.CausedByTrigger, pin.ID)
	}
}
