package protocol

// loop_control_test.go — S55 T5. The two handler-level proofs the guard
// amendment names from the protocol side: the audit actor is the PEER the
// daemon stamped (never a param a caller chose), and every inapplicable
// control act is a TYPED refusal — one code, the state in Data.Kind, the
// remedy in the What sentence — with the one exception the plane makes: a
// lost resume race is a RESULT carrying refused data, not an error.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// controlRecordingService is the seam the handler tests dispatch against. It
// records the actor each call arrives with (the audit identity under test)
// and answers per-test results/refusals; it never touches a store, the way
// settingsStore never does.
type controlRecordingService struct {
	actors     []Actor
	calls      int
	stopRef    *ControlRefusal
	resumeRef  *ControlRefusal
	cancelRef  *ControlRefusal
	reqRef     *ControlRefusal
	dismissRef *ControlRefusal
}

func (s *controlRecordingService) record(a Actor) { s.actors = append(s.actors, a); s.calls++ }

func (s *controlRecordingService) Stop(ctx context.Context, actor Actor, id, reason string) (LoopStopResult, *ControlRefusal, error) {
	s.record(actor)
	return LoopStopResult{LoopID: id, Stopped: true, Status: "waiting"}, s.stopRef, nil
}
func (s *controlRecordingService) Resume(ctx context.Context, actor Actor, id, message string) (LoopResumeResult, *ControlRefusal, error) {
	s.record(actor)
	return LoopResumeResult{LoopID: id, Resumed: true, Status: "running", How: "runner"}, s.resumeRef, nil
}
func (s *controlRecordingService) Cancel(ctx context.Context, actor Actor, id, reason string) (LoopCancelResult, *ControlRefusal, error) {
	s.record(actor)
	return LoopCancelResult{LoopID: id, Status: "aborted"}, s.cancelRef, nil
}
func (s *controlRecordingService) Control(ctx context.Context, id string) (LoopControlResult, error) {
	return LoopControlResult{LoopID: id, Status: "running"}, nil
}
func (s *controlRecordingService) RequestStop(ctx context.Context, loopID, reason string) (RequestStopResult, *ControlRefusal, error) {
	return RequestStopResult{RequestID: 1, Status: "open"}, s.reqRef, nil
}
func (s *controlRecordingService) Requests(ctx context.Context, loopID, status string) ([]ControlRequestView, error) {
	return []ControlRequestView{}, nil
}
func (s *controlRecordingService) Dismiss(ctx context.Context, actor Actor, requestID int64) (ControlRequestView, *ControlRefusal, error) {
	s.record(actor)
	return ControlRequestView{ID: requestID, Status: "dismissed"}, s.dismissRef, nil
}

// controlTable builds an initialised table with the loop-control seam wired
// and (unless actorFor is false) a peer identity stamped from the connection,
// the way the daemon's table.ActorFor stamps it.
func controlTable(t *testing.T, svc *controlRecordingService, actorFor bool) (*Table, *controlRecordingService) {
	t.Helper()
	tbl := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tbl.LoopControl = svc
	if !tbl.RegisterLoopControl() {
		t.Fatal("RegisterLoopControl refused a non-nil seam")
	}
	if actorFor {
		tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "uid-1000"} }
	}
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl, svc
}

// TestControlActorIsThePeer (LC5): the audit actor is the connection's peer
// credential the daemon stamped — never a value a caller chose. A params
// actor is REJECTED by name (ErrBadParams, -32602), and the seam is never
// reached with it; a table with no identity seam refuses the write as the
// honest absence.
func TestControlActorIsThePeer(t *testing.T) {
	svc := &controlRecordingService{}
	tbl, rec := controlTable(t, svc, true)
	peer := Actor{Kind: "user", ID: "uid-1000"}

	// The stop's audit actor is the peer, stamped from the connection.
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodLoopStop, mustJSON(t, LoopStopParams{ID: "loop-9"})); err != nil {
		t.Fatalf("loop.stop: %v", err)
	}
	if len(rec.actors) != 1 || rec.actors[0] != peer {
		t.Fatalf("the seam received %v, want %v — the audit actor is the connection, never a param", rec.actors, peer)
	}

	// A params actor on any write door is a named parse error, and the seam
	// is never reached with it.
	forged := map[MethodName]json.RawMessage{
		MethodLoopStop:           json.RawMessage(`{"id":"loop-9","actor":"forged"}`),
		MethodLoopResume:         json.RawMessage(`{"id":"loop-9","actor":"forged"}`),
		MethodLoopCancel:         json.RawMessage(`{"id":"loop-9","reason":"why","actor":"forged"}`),
		MethodLoopRequestStop:    json.RawMessage(`{"loopId":"loop-9","reason":"why","actor":"forged"}`),
		MethodLoopControlDismiss: json.RawMessage(`{"requestId":1,"actor":"forged"}`),
	}
	before := rec.calls
	for m, raw := range forged {
		_, err := tbl.Dispatch(context.Background(), "c1", m, raw)
		var bad ErrBadParams
		if err == nil {
			t.Fatalf("%s accepted a params actor — a self-chosen actor would forge the audit identity of a control write", m)
		}
		if !errors.As(err, &bad) {
			t.Fatalf("%s with a params actor: want ErrBadParams (-32602), got %T: %v", m, err, err)
		}
	}
	if rec.calls != before {
		t.Fatalf("the seam was reached %d time(s) with a forged actor — the refusal must land BEFORE the service", rec.calls-before)
	}

	// No identity seam: the door is a person's door and there are no persons
	// — the write is refused, not silently stamped with an empty actor.
	tblNoActor, _ := controlTable(t, &controlRecordingService{}, false)
	if _, err := tblNoActor.Dispatch(context.Background(), "c1", MethodLoopStop, mustJSON(t, LoopStopParams{ID: "loop-9"})); err == nil {
		t.Fatal("loop.stop succeeded on a table with no identity seam — the write would carry an empty audit actor")
	} else if !errors.As(err, new(ErrBadParams)) {
		t.Fatalf("want ErrBadParams (the honest absence), got %T: %v", err, err)
	}
}

// TestControlRefusalsAreTyped (LC2/LC4/LC15, DB19): every inapplicable
// control act answers ErrLoopControlRefused — one code for the whole plane,
// the state found in Data.Kind (the machine branch), the remedy in the What
// sentence (the person's line), the same means/todo the plane carries. And
// the plane's one exception: a LOST resume race is a result with refused
// data, never an error — the act was eligible when gated.
func TestControlRefusalsAreTyped(t *testing.T) {
	assertRefusal := func(t *testing.T, what string, err error, verb, kind string) {
		t.Helper()
		var e *Error
		if err == nil {
			t.Fatalf("%s: no error — an inapplicable control act must answer the typed refusal", what)
		}
		if !errors.As(err, &e) {
			t.Fatalf("%s: want *protocol.Error, got %T: %v", what, err, err)
		}
		if e.Code != ErrLoopControlRefused.Code {
			t.Fatalf("%s: code = %d, want %d — one code for the whole plane", what, e.Code, ErrLoopControlRefused.Code)
		}
		if e.Data.Kind != kind {
			t.Errorf("%s: data.kind = %q, want %q — a client branches on the state, not the prose", what, e.Data.Kind, kind)
		}
		if !strings.Contains(e.Data.What, "loop "+verb+" refused:") {
			t.Errorf("%s: the what sentence does not name the act: %q", what, e.Data.What)
		}
		if e.Data.Means == "" || e.Data.Todo == "" {
			t.Errorf("%s: the three parts must ride the refusal: %+v", what, e.Data)
		}
	}

	// stop refused: the loop is already stopped.
	svc := &controlRecordingService{stopRef: &ControlRefusal{Kind: "stopped", Detail: "the loop is stopped — resume it (or cancel it)"}}
	tbl, _ := controlTable(t, svc, true)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodLoopStop, mustJSON(t, LoopStopParams{ID: "loop-9"}))
	assertRefusal(t, "loop.stop refused", err, "stop", "stopped")
	if err != nil {
		var e *Error
		errors.As(err, &e)
		if !strings.Contains(e.Data.What, "the loop is stopped") {
			t.Errorf("the what must carry the store's sentence verbatim: %q", e.Data.What)
		}
	}

	// cancel refused: a terminal loop has no control to apply.
	svc = &controlRecordingService{cancelRef: &ControlRefusal{Kind: "terminal", Detail: "the loop is done — it has no control to apply"}}
	tbl, _ = controlTable(t, svc, true)
	_, err = tbl.Dispatch(context.Background(), "c1", MethodLoopCancel, mustJSON(t, LoopCancelParams{ID: "loop-9", Reason: "why"}))
	assertRefusal(t, "loop.cancel refused", err, "cancel", "terminal")

	// a stop request refused: the desk is full for this loop.
	svc = &controlRecordingService{reqRef: &ControlRefusal{Kind: "request-limit", Detail: "the loop already has 5 open stop requests (the limit is 5)"}}
	tbl, _ = controlTable(t, svc, true)
	_, err = tbl.Dispatch(context.Background(), "c1", MethodLoopRequestStop, mustJSON(t, LoopRequestStopParams{LoopID: "loop-9", Reason: "why"}))
	assertRefusal(t, "loop.requestStop refused", err, "stop request", "request-limit")

	// a dismissal refused: the request is not open.
	svc = &controlRecordingService{dismissRef: &ControlRefusal{Kind: "not-open", Detail: "stop request 7 is not open — it is already settled or never filed (loop.control.requests names the desk)"}}
	tbl, _ = controlTable(t, svc, true)
	_, err = tbl.Dispatch(context.Background(), "c1", MethodLoopControlDismiss, mustJSON(t, LoopControlDismissParams{RequestID: 7}))
	assertRefusal(t, "loop.control.dismiss refused", err, "dismiss", "not-open")

	// The exception: a lost resume race is a RESULT. The act was eligible
	// when gated and the loop moved meanwhile — the answer is honest data
	// (resumed false, the refusal in Refusal), never an error.
	svc = &controlRecordingService{resumeRef: &ControlRefusal{Kind: "running", Detail: "the loop is running — stop it first (a resume needs a parked loop)"}}
	tbl, _ = controlTable(t, svc, true)
	res, err := tbl.Dispatch(context.Background(), "c1", MethodLoopResume, mustJSON(t, LoopResumeParams{ID: "loop-9"}))
	if err != nil {
		t.Fatalf("a lost resume race answered an error (%v) — it is a result with refused data, so a client can render it", err)
	}
	r, ok := res.(LoopResumeResult)
	if !ok {
		t.Fatalf("loop.resume answered %T, want LoopResumeResult", res)
	}
	if r.Refusal == nil || r.Refusal.Kind != "running" {
		t.Fatalf("the lost-race result carries refusal %+v, want the in-data refusal with kind running", r.Refusal)
	}
}
