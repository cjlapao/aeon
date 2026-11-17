package protocol

// # `step.steer` / `step.steers` / `step.steer.cancel` (S31 T4)
//
// The store keeps the rows honest (its own tests say so); this file tests the
// DOOR — the refusals a caller has to be told about, and the announcement a
// queueing has to make. LW4 in particular: every refusal names the state
// found AND the method that fits it, because "step not steerable" would leave
// the person guessing whether `answer` or `step.continue` was the right door.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/runtime"
)

// steerSpy is the durable queue as the handlers see it: rows keyed by id,
// cancellable only from pending, refusals carried as classified *Errors —
// exactly what the daemon adapter will do with store.StepSteerResult.
type steerSpy struct {
	mu           sync.Mutex
	entries      []steerEntry
	next         int64
	queueErr     error
	queueSkipped []string // S68 B6: what the queue says did not ride
}

type steerEntry struct {
	stepID int64
	view   SteerView
}

func newSteerSpy() *steerSpy {
	return &steerSpy{next: 10}
}

func (s *steerSpy) QueueSteer(stepID int64, text, route, effort string, skills []string, attachments []AttachmentRef) (SteerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queueErr != nil {
		return SteerView{}, s.queueErr
	}
	s.next++
	v := SteerView{SteerID: s.next, Text: text, Route: route, ReasoningEffort: effort,
		State: "pending", CreatedAt: "2026-10-24T12:00:00Z", Skills: skills, Skipped: s.queueSkipped}
	s.entries = append(s.entries, steerEntry{stepID: stepID, view: v})
	return v, nil
}

// lastSkills returns the chosen names the last QueueSteer received — what
// the door passed to the queue verbatim (it carries, it does not interpret).
func (s *steerSpy) lastSkills() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return nil
	}
	return s.entries[len(s.entries)-1].view.Skills
}

func (s *steerSpy) ListSteers(stepID int64) ([]SteerView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []SteerView
	for _, e := range s.entries {
		if e.stepID == stepID {
			out = append(out, e.view)
		}
	}
	return out, nil
}

func (s *steerSpy) CancelSteer(steerID int64) (SteerView, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.entries {
		if s.entries[i].view.SteerID != steerID {
			continue
		}
		if s.entries[i].view.State != "pending" {
			return SteerView{}, 0, ErrSteerAlreadySettled.WithWhat(
				"steer %d could not be cancelled: the steer is already %s", steerID, s.entries[i].view.State)
		}
		s.entries[i].view.State = "cancelled"
		return s.entries[i].view, s.entries[i].stepID, nil
	}
	return SteerView{}, 0, ErrSteerUnknown.WithWhat("no steer row carries steerId %d", steerID)
}

// seed drops a row in without going through the door — the test is about the
// door, and delivered/cancelled rows cannot arrive through it by construction.
func (s *steerSpy) seed(stepID int64, state, text string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	s.entries = append(s.entries, steerEntry{stepID: stepID, view: SteerView{
		SteerID: s.next, Text: text, State: state, CreatedAt: "2026-10-24T12:00:00Z",
		DeliveredAt: map[bool]string{true: "2026-10-24T12:00:05Z"}[state == "delivered"],
	}})
	return s.next
}

func (s *steerSpy) state(steerID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.view.SteerID == steerID {
			return e.view.State
		}
	}
	return ""
}

// steerTable wires the three handlers' seams: a running step 41 unless the
// caller overrides the state reader, a spy queue, and loop identity.
func steerTable(t *testing.T) (*Table, *eventLog, *steerSpy) {
	t.Helper()
	tbl, events := stepTable(t, &scriptedRunner{}, &spyStepStore{})
	spy := newSteerSpy()
	tbl.SteerStore = spy
	tbl.StepSteerState = func(int64) (string, string, string, string, string, bool) {
		return "running", "", "default", "/srv/proj", "lane-a", true
	}
	tbl.LookupLoopID = func(string, string) string { return "loop-1" }
	initializedOnce(t, tbl, "c1")
	return tbl, events, spy
}

func dispatchSteer(t *testing.T, tbl *Table, params any) (any, error) {
	t.Helper()
	return tbl.Dispatch(context.Background(), "c1", MethodStepSteer, mustJSON(t, params))
}

// ── the happy path ───────────────────────────────────────────────────────────

func TestSteerQueuesTheRowAndAnnouncesItUnderTheComponent(t *testing.T) {
	tbl, events, spy := steerTable(t)

	res, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "summarize instead"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.(StepSteerResult)
	if !ok || got.SteerID == 0 || got.State != "pending" {
		t.Fatalf("result = %+v, want {steerId, state:pending} — brief §5", res)
	}
	if state := spy.state(got.SteerID); state != "pending" {
		t.Fatalf("row state after queueing = %q, want pending", state)
	}

	queued := events.matching("step.steer.queued")
	if len(queued) != 1 {
		t.Fatalf("%d queued events, want exactly 1", len(queued))
	}
	var p StepEventPayload
	if err := json.Unmarshal(queued[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.StepID != 41 || p.SteerID != got.SteerID || !p.HasText {
		t.Fatalf("the announcement is not about the row just queued: %+v", p)
	}
	// The ROOT itself is asserted where the publisher reads it
	// (internal/daemon): this log marshals payloads, and the root rides
	// unexported on purpose. Agent and loop are the wire-visible identity.
	if p.Agent != "default" || p.LoopID != "loop-1" {
		t.Fatalf("an event without identity is hard to file (D1): %+v", p)
	}
	// The announcement announces; it does not broadcast the instruction. The
	// row carries the text, and the next boundary puts it in the transcript.
	if strings.Contains(string(queued[0].Payload), "summarize instead") {
		t.Fatal("the queued event carries the instruction's text — the row is its home")
	}
}

func TestSteerAcceptsRouteAndEffortOverrides(t *testing.T) {
	tbl, _, spy := steerTable(t)
	res, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Route: "r1", ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(StepSteerResult)
	if got.State != "pending" {
		t.Fatalf("an override steer must still answer pending: %+v", got)
	}
	view, _ := spy.ListSteers(41)
	if len(view) != 1 || view[0].Route != "r1" || view[0].ReasoningEffort != "low" || view[0].Text != "" {
		t.Fatalf("the row did not carry the overrides exactly: %+v", view)
	}
}

// ── the door ─────────────────────────────────────────────────────────────────

func TestSteerRefusalsByName(t *testing.T) {
	long := strings.Repeat("x", maxSteerTextBytes+1)
	cases := []struct {
		name     string
		params   StepSteerParams
		wantCode int
		inWhat   string
		setup    func(*Table, *steerSpy)
	}{
		{"no stepId", StepSteerParams{Text: "hi"}, 0, "stepId", nil},
		{"nothing to inject", StepSteerParams{StepID: 41}, 0, "no text", nil},
		{"oversized text", StepSteerParams{StepID: 41, Text: long}, 0,
			fmt.Sprintf("%d byte bound", maxSteerTextBytes), nil},
		{"unknown effort", StepSteerParams{StepID: 41, ReasoningEffort: "ultra"}, 0,
			"not one of", nil},
		{"unknown route", StepSteerParams{StepID: 41, Route: "ghost"}, 0,
			`"ghost"`, nil},
		{"stopped step points at step.continue", StepSteerParams{StepID: 41, Text: "hi"},
			ErrSteerTargetNotRunning.Code, "step.continue",
			func(tbl *Table, _ *steerSpy) {
				tbl.StepSteerState = func(int64) (string, string, string, string, string, bool) {
					return "suspended", "user", "default", "/srv/proj", "lane-a", true
				}
			}},
		{"question step points at answer", StepSteerParams{StepID: 41, Text: "hi"},
			ErrSteerTargetNotRunning.Code, "`answer`",
			func(tbl *Table, _ *steerSpy) {
				tbl.StepSteerState = func(int64) (string, string, string, string, string, bool) {
					return "suspended", "", "default", "/srv/proj", "lane-a", true
				}
			}},
		{"done step names the status", StepSteerParams{StepID: 41, Text: "hi"},
			ErrSteerTargetNotRunning.Code, "done",
			func(tbl *Table, _ *steerSpy) {
				tbl.StepSteerState = func(int64) (string, string, string, string, string, bool) {
					return "done", "", "default", "/srv/proj", "lane-a", true
				}
			}},
		{"step that never was", StepSteerParams{StepID: 404, Text: "hi"},
			ErrStepNotFound.Code, "does not exist",
			func(tbl *Table, _ *steerSpy) {
				tbl.StepSteerState = func(int64) (string, string, string, string, string, bool) {
					return "", "", "", "", "", false
				}
			}},
		{"full queue names the remedy", StepSteerParams{StepID: 41, Text: "seventeenth"},
			0, "step.steer.cancel",
			func(_ *Table, spy *steerSpy) {
				for i := 0; i < maxPendingSteers; i++ {
					spy.seed(41, "pending", fmt.Sprintf("queued %d", i))
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl, _, spy := steerTable(t)
			if tc.setup != nil {
				tc.setup(tbl, spy)
			}
			_, err := dispatchSteer(t, tbl, tc.params)
			if tc.wantCode == 0 {
				// Param-shape refusals are JSON-RPC's own (-32602 at the
				// wire), carried as ErrBadParams — the message is where the
				// naming happens, so that is what we read.
				var bp ErrBadParams
				if !errors.As(err, &bp) {
					t.Fatalf("got %v, want ErrBadParams", err)
				}
				if !strings.Contains(bp.Error(), tc.inWhat) {
					t.Errorf("the refusal does not name %q: %s", tc.inWhat, bp.Error())
				}
				return
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.wantCode {
				t.Fatalf("got %v, want code %d", err, tc.wantCode)
			}
			if !strings.Contains(e.Data.What, tc.inWhat) {
				t.Errorf("the refusal does not name %q: %s", tc.inWhat, e.Data.What)
			}
		})
	}
}

func TestSteerRefusesWithoutItsWiring(t *testing.T) {
	// A daemon that has not wired the queue or the state read says so, rather
	// than queueing into the void or guessing the step is running.
	tbl, _, _ := steerTable(t)
	tbl.SteerStore = nil
	if _, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "hi"}); err == nil {
		t.Fatal("a table with no queue accepted a steer")
	}
	tbl, _, _ = steerTable(t)
	tbl.StepSteerState = nil
	if _, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "hi"}); err == nil {
		t.Fatal("a table that cannot read step state queued a steer anyway")
	}
}

func TestSteerDoesNotCountSettledRowsAgainstTheCap(t *testing.T) {
	// Sixteen DELIVERED instructions is a busy step, not a jammed queue: the
	// cap guards the unread backlog, which is what the next boundary would
	// assemble into one prompt.
	tbl, _, spy := steerTable(t)
	for i := 0; i < maxPendingSteers; i++ {
		spy.seed(41, "delivered", fmt.Sprintf("read %d", i))
	}
	if _, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "one more"}); err != nil {
		t.Fatalf("a step with a full history and an empty backlog was refused: %v", err)
	}
}

// ── step.steers ──────────────────────────────────────────────────────────────

func TestSteersListsEveryStateInQueueOrder(t *testing.T) {
	tbl, _, spy := steerTable(t)
	spy.seed(41, "delivered", "first")
	spy.seed(41, "cancelled", "withdrawn")
	queued, _ := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "third"})

	res, err := tbl.Dispatch(context.Background(), "c1", MethodStepSteers,
		mustJSON(t, StepSteersParams{StepID: 41}))
	if err != nil {
		t.Fatal(err)
	}
	list := res.([]SteerView)
	if len(list) != 3 {
		t.Fatalf("%d rows, want all three states — the list is the person's audit trail", len(list))
	}
	if list[0].State != "delivered" || list[1].State != "cancelled" ||
		list[2].State != "pending" {
		t.Fatalf("states out of queue order: %+v", list)
	}
	if list[0].DeliveredAt == "" {
		t.Error("the delivered row does not say when it was read")
	}
	if list[2].SteerID != queued.(StepSteerResult).SteerID {
		t.Error("the row queued through the door is not the last row in the list")
	}
}

func TestSteersAnswersAnEmptyArrayNotNothing(t *testing.T) {
	tbl, _, _ := steerTable(t)
	raw, err := tbl.Dispatch(context.Background(), "c1", MethodStepSteers,
		mustJSON(t, StepSteersParams{StepID: 41}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(raw)
	if string(encoded) != `[]` {
		t.Fatalf("a step with no steers answered %s — `null` reads as \"the list failed\", "+
			"which is a different fact", encoded)
	}
}

// ── step.steer.cancel ────────────────────────────────────────────────────────

func TestCancelWithdrawsAPendingRowAndAnnouncesIt(t *testing.T) {
	tbl, events, spy := steerTable(t)
	id := spy.seed(41, "pending", "ignore the tests")

	res, err := tbl.Dispatch(context.Background(), "c1", MethodStepSteerCancel,
		mustJSON(t, StepSteerCancelParams{SteerID: id}))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(StepSteerCancelResult); got.State != "cancelled" {
		t.Fatalf("state = %q, want cancelled — brief §5's {state} is the answer that settles the question", got.State)
	}
	if state := spy.state(id); state != "cancelled" {
		t.Fatalf("row state = %q", state)
	}
	cancelled := events.matching("step.steer.cancelled")
	if len(cancelled) != 1 {
		t.Fatalf("%d cancelled events, want 1", len(cancelled))
	}
	var p StepEventPayload
	if err := json.Unmarshal(cancelled[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	// The cancel params name only a steerId; the EVENT must still name its
	// step, because a step event without `stepId` belongs to no thread.
	if p.StepID != 41 || p.SteerID != id || p.Agent != "default" {
		t.Fatalf("the announcement lost its step: %+v", p)
	}
}

func TestCancelRefusalsTravelWithTheStatesName(t *testing.T) {
	tbl, _, spy := steerTable(t)
	delivered := spy.seed(41, "delivered", "already read")

	var e *Error
	_, err := tbl.Dispatch(context.Background(), "c1", MethodStepSteerCancel,
		mustJSON(t, StepSteerCancelParams{SteerID: delivered}))
	if !errors.As(err, &e) || e.Code != ErrSteerAlreadySettled.Code {
		t.Fatalf("cancelling a delivered steer: %v, want ErrSteerAlreadySettled", err)
	}
	if !strings.Contains(e.Data.What, "delivered") {
		t.Errorf("the refusal does not name the state found: %s", e.Data.What)
	}

	_, err = tbl.Dispatch(context.Background(), "c1", MethodStepSteerCancel,
		mustJSON(t, StepSteerCancelParams{SteerID: 999}))
	if !errors.As(err, &e) || e.Code != ErrSteerUnknown.Code {
		t.Fatalf("cancelling a steer that never was: %v, want ErrSteerUnknown", err)
	}
}

// ── the drain seam ───────────────────────────────────────────────────────────

func TestSteerSourceIsNilTolerant(t *testing.T) {
	// The runtime treats a nil source as "unsteerable, exactly as before T4";
	// the table's job is to hand it nil — not a half-wired source — when the
	// daemon has no queue, and to hand it the daemon's source otherwise.
	tbl, _, _ := steerTable(t)
	if got := tbl.steerSource(41); got != nil {
		t.Fatalf("a table with no Steers factory produced %T, want nil", got)
	}
	var sent bool
	tbl.Steers = func(stepID int64) runtime.SteerSource {
		sent = true
		if stepID != 41 {
			t.Errorf("the factory got step %d, want the run's own step", stepID)
		}
		return nil
	}
	if tbl.steerSource(41) != nil || !sent {
		t.Fatal("the factory was not consulted, or its nil answer was not passed through")
	}
}

// ── S68 B6/B7: the chosen skills ride the door verbatim ──────────────────────

func TestSteerHandsTheChosenNamesToTheQueueUntouched(t *testing.T) {
	// The door CARRIES the chosen skills, it does not interpret them: what
	// the caller sent is what the queue is asked to file. Whether a name
	// resolves is the queue's (the daemon's catalog) to answer.
	tbl, _, spy := steerTable(t)
	if _, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "go",
		Skills: []string{"caveman", "verify-before-done"}}); err != nil {
		t.Fatal(err)
	}
	if got := spy.lastSkills(); len(got) != 2 || got[0] != "caveman" || got[1] != "verify-before-done" {
		t.Fatalf("the queue received %v, want the names as sent", got)
	}
}

func TestSteerPassesTheQueuesSkillRefusalThroughAsBadParams(t *testing.T) {
	// A queue that refuses because a chosen skill is unknown is the CALLER's
	// mistake, not an outage: the error keeps its ErrBadParams class and its
	// offender-naming message intact.
	tbl, _, spy := steerTable(t)
	spy.queueErr = ErrBadParams{Err: errors.New("unknown skills: ghost — nothing was chosen")}
	_, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "go", Skills: []string{"ghost"}})
	var bp ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("a skill refusal must stay ErrBadParams, got %v", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the refusal must name the offender, got: %v", err)
	}
}

func TestSteerReportsTheSkippedNamesInTheResult(t *testing.T) {
	// A known-but-unpickable skill is skipped, not dropped silently: the
	// result says which did not ride (the row keeps only the ones that did).
	tbl, _, spy := steerTable(t)
	spy.queueSkipped = []string{"legacy-notes"}
	res, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "go", Skills: []string{"caveman", "legacy-notes"}})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := res.(StepSteerResult)
	if !ok || len(got.Skipped) != 1 || got.Skipped[0] != "legacy-notes" {
		t.Fatalf("the result must report the skipped skill, got %+v", res)
	}
}

func TestSteerQueueFaultIsNotMistakenForASkillRefusal(t *testing.T) {
	// The classification is honored both ways: a queue that fails for a
	// reason that is NOT ErrBadParams stays an infrastructure failure
	// (ErrStoreUnavailable), so a disk fault never reads as "you chose a bad
	// skill" and vice versa.
	tbl, _, spy := steerTable(t)
	spy.queueErr = errors.New("disk gone")
	_, err := dispatchSteer(t, tbl, StepSteerParams{StepID: 41, Text: "go", Skills: []string{"caveman"}})
	// The classified sentinel (WithWhat re-wraps, so the code is identity).
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrStoreUnavailable.Code {
		t.Fatalf("a queue fault must be ErrStoreUnavailable, got %v", err)
	}
	var bp ErrBadParams
	if errors.As(err, &bp) {
		t.Fatalf("an outage must not read as a caller error: %v", err)
	}
}
