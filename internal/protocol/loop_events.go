package protocol

import (
	"context"
	"encoding/json"
)

// `loop.events` — one loop's journal, read by window.
//
// # Why this exists beside `subscribe` and `loop.transcript`
//
// Every event the daemon publishes is already written to the durable event table. What was missing was a way to ask
// for ONE LOOP's events: a subscription replayed from cursor 0 returned the oldest thousand events of the whole
// daemon, and `loop.transcript` returns the model's messages only (no costs, no tool timings, no approvals, no
// agent state). This method returns the events themselves, the same payloads a live subscriber received, so a page
// can feed a reopened loop through the SAME reducer it uses for a live one.
//
// The cursor is the event's own seq, the number every subscription uses: read the latest window, then subscribe
// from that window's head, and the two join with no gap and no overlap. Streamed text chunks (`model.delta`) are
// left out unless asked for: a window of history already holds each finished turn.

// LoopEventsParams names the loop (by id) and the window. Exactly one of Before and After may be set; neither means
// the latest window.
type LoopEventsParams struct {
	ID     string `json:"id"`
	Before int64  `json:"before,omitempty"`
	After  int64  `json:"after,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	// StepID narrows the window to one step of the loop.
	StepID int64 `json:"stepId,omitempty"`
	// Kinds narrows to exact kinds or one trailing `*` wildcard; empty means every kind.
	Kinds []string `json:"kinds,omitempty"`
	// IncludeDeltas returns the streamed text chunks too.
	IncludeDeltas bool `json:"includeDeltas,omitempty"`
}

// LoopEventView is one journal row, in the shape a live event has (minus the subscription tag).
type LoopEventView struct {
	Seq           int64           `json:"seq"`
	Kind          string          `json:"kind"`
	At            string          `json:"at"`
	ComponentRoot string          `json:"componentRoot,omitempty"`
	StepID        int64           `json:"stepId,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// LoopEventsResult is one window, oldest first.
type LoopEventsResult struct {
	Events []LoopEventView `json:"events"`
	// HasOlder and HasNewer say whether the journal holds more in each direction.
	HasOlder bool `json:"hasOlder"`
	HasNewer bool `json:"hasNewer"`
	// Head is the highest event seq the daemon has allocated now: subscribe from it to receive what happens next.
	Head int64 `json:"head"`
}

func (t *Table) handleLoopEvents(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	mem, err := t.requireMemory()
	if err != nil {
		return nil, err
	}
	var p LoopEventsParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrInvalidScope.WithWhat("reading a loop's events needs the loop id")
	}
	if p.Before > 0 && p.After > 0 {
		return nil, ErrInvalidScope.WithWhat("loop.events takes before OR after, never both")
	}
	return mem.ReadLoopEvents(p)
}
