package daemon

// S55 T5 (LC15) — the stop request is INERT: it records that an agent asked
// a person to stop a loop, and NOTHING halts until a person acts (loop.stop)
// or dismisses it. The test runs the request through the daemon's own seam
// (the one the socket door loop.requestStop reaches) on a loop with a real
// in-flight step, and asserts the loop's pinned state and the step's row are
// BYTE-IDENTICAL before and after — the only new rows are the request itself
// and its audit event.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

func TestRequestStopIsInert(t *testing.T) {
	s := recoveryStore(t)
	root := t.TempDir()
	m := &lcInFlightModel{entered: make(chan struct{})}
	d, w := lcDaemon(t, s, m)
	loopID := lcStart(t, d, lcBlueprint(t, ""), root, "inert-request")
	stepID := waitStepInFlight(t, s, loopID)
	// The model call has begun: the step row is running and names its agent,
	// so the request has an agent to stamp (the daemon derives it from the
	// in-flight step — a params agent is not accepted anywhere).
	<-m.entered

	// The before: the pinned state and the step row, as bytes.
	before := func() (string, string) {
		t.Helper()
		st, err := loop.Pinned(context.Background(), s, loopID)
		if err != nil {
			t.Fatalf("pinned before: %v", err)
		}
		stRaw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		step, err := s.ReadStep(stepID)
		if err != nil {
			t.Fatalf("step before: %v", err)
		}
		stepRaw, err := json.Marshal(step)
		if err != nil {
			t.Fatal(err)
		}
		return string(stRaw), string(stepRaw)
	}
	stBefore, stepBefore := before()

	// A person is watching: a live subscription, the way the app holds one.
	// Without it the daemon's emit has no subscriber to fan out to — the
	// person "sees" the ask because a client is subscribed, not because the
	// event exists (LC20 is about the person seeing it).
	initConn(t, d, context.Background(), "c-inert", "inert-watcher")
	subscribe(t, d, "c-inert", protocol.SubscribeParams{})

	// The request, through the daemon's seam — the same code the socket door
	// loop.requestStop dispatches to.
	res, refusal, err := d.Table.LoopControl.RequestStop(context.Background(), loopID, "this loop is going in circles — a person should stop it")
	if err != nil {
		t.Fatalf("RequestStop: %v", err)
	}
	if refusal != nil {
		t.Fatalf("RequestStop was refused: %+v — a running loop with an in-flight agent step is exactly the state the request exists for", refusal)
	}
	if res.Status != "open" || res.RequestID <= 0 {
		t.Fatalf("request result %+v, want an open request with an id", res)
	}

	// The after: byte-identical. The request halts NOTHING.
	stAfter, stepAfter := before()
	if stAfter != stBefore {
		t.Fatalf("the request moved the loop's pinned state — a stop request is inert, a person's stop is what moves it:\nbefore: %s\nafter:  %s", stBefore, stAfter)
	}
	if stepAfter != stepBefore {
		t.Fatalf("the request touched the step's row — nothing halted:\nbefore: %s\nafter:  %s", stepBefore, stepAfter)
	}

	// The only new rows: the request (open, stamped with the in-flight
	// step's agent) and its audit event.
	reqs, err := s.ListControlRequests(store.ControlRequestFilter{LoopID: loopID, Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("open requests = %d, want 1", len(reqs))
	}
	if reqs[0].ID != res.RequestID || reqs[0].ByAgent != "default" {
		t.Fatalf("request row %+v, want id %d stamped with the in-flight agent (default)", reqs[0], res.RequestID)
	}
	var reqEv protocol.Event
	for _, sent := range w.delivered() {
		if sent.Event.Kind == protocol.KindLoopStopRequested {
			reqEv = sent.Event
		}
	}
	if reqEv.Kind == "" {
		t.Error("no loop.stop.requested event reached the subscriber — the person would never see the agent's ask (LC20)")
	}
	// The event names the request and the agent that asked, so the person
	// sees WHAT is being asked, from WHO, not just that something happened.
	var payload map[string]any
	if err := json.Unmarshal(reqEv.Payload, &payload); err != nil {
		t.Fatalf("the stop-request event's payload is not a JSON object: %v", err)
	}
	if payload["requestId"] != float64(res.RequestID) || payload["byAgent"] != "default" {
		t.Errorf("the stop-request event's payload %v, want requestId %d byAgent default", payload, res.RequestID)
	}

	// Settle the loop the way a person's stop would, so the test leaves no
	// runner in flight: the step parks suspended/user, the loop waiting/stopped.
	if _, refusal, err := d.StopLoop(context.Background(), loopID, store.Actor{Kind: "user", ID: "1000"}, "the test is done"); err != nil {
		t.Fatalf("StopLoop: %v", err)
	} else if refusal != nil {
		t.Fatalf("StopLoop refused: %+v", refusal)
	}
	st, err := loop.Pinned(context.Background(), s, loopID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "waiting" || st.LastReason != "stopped" {
		t.Fatalf("post-stop pin %q/%q, want waiting/stopped", st.Status, st.LastReason)
	}
	// And a request that was OPEN when a person stopped is settled honored —
	// the person acted on it (the store's stop cascade decides the desk).
	reqs, err = s.ListControlRequests(store.ControlRequestFilter{LoopID: loopID})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0].Status != "honored" {
		t.Fatalf("requests after the person's stop = %+v, want the one request honored", reqs)
	}
	// The protocol layer rendered the request's own event kind for the
	// stream — the daemon emitted the plane's kind, not a loop.* lifecycle one.
	_ = protocol.KindLoopStopRequested
}
