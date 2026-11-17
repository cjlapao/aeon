package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// agent_messages.go is S70 T3: the read of the mailbox (MB7). One method,
// read-only. There is NO authoring method on the socket or the projections:
// the person's door to a loop is steer (S39) and an agent's door is the
// ask tool behind the policy gate — a mailbox row can only be filed by a
// step's own run, never by a caller with a socket. `agent.messages` lists
// rows for a loop, whole or by thread; MB6's stream carries the same rows
// as they are posted, delivered, and answered.
//
// MB7's scope rule: scope comes from the LOOP ROW (the id resolves to its
// slug and component root behind the seam), never from a flag a caller
// could set wrong. The third state is answered, not silenced (P31): naming
// a thread the loop does not have is `absent: true` with an empty list —
// an absence SAID, never a silence a client reads as empty.

// AgentMessagesParams is `agent.messages`' request: which loop, and
// optionally which thread (a thread is its first ask's id, MB3).
type AgentMessagesParams struct {
	LoopID string `json:"loopId"`
	// Thread names one conversation by its header row's id. Nil reads the
	// whole loop mailbox; a non-positive value is refused, not guessed.
	Thread *int64 `json:"thread,omitempty"`
}

// AgentMessageView is one agent_message row on the wire — the same fields,
// the same nulls, the same third states the row carries (MB8's byte
// identity runs row → view without invention: `resultOf` and the delivery
// pair appear only when they have happened).
type AgentMessageView struct {
	ID              int64  `json:"id"`
	Thread          int64  `json:"thread"`
	FromAgent       string `json:"fromAgent"`
	ToAgent         string `json:"toAgent"`
	FromStepID      int64  `json:"fromStepId"`
	Body            string `json:"body"`
	ExpectReply     bool   `json:"expectReply"`
	ResultOf        int64  `json:"resultOf,omitempty"`
	DeliveredAt     string `json:"deliveredAt,omitempty"`
	DeliveredToStep int64  `json:"deliveredToStepId,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

// AgentMessagesResponse is the read's answer. Messages is never null — an
// empty mailbox marshals [] (the list exists; it has no rows). Absent is
// the honest third state, true only when a thread was NAMED and the loop
// holds no such thread.
type AgentMessagesResponse struct {
	LoopID   string             `json:"loopId"`
	Thread   *int64             `json:"thread,omitempty"`
	Absent   bool               `json:"absent"`
	Messages []AgentMessageView `json:"messages"`
}

// handleAgentMessages is the read door. The seam (mounted by the daemon,
// whose store holds the rows) resolves the loop id to its own scope and
// reads; this handler guards the SHAPE of the question only.
func (t *Table) handleAgentMessages(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p AgentMessagesParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.LoopID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loopId is required — the mailbox belongs to a loop (MB7: scope comes from the loop row, never a flag)")}
	}
	if p.Thread != nil && *p.Thread <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("thread %d is not a thread id — a thread is named by its first ask's id (positive)", *p.Thread)}
	}
	if t.AgentMessages == nil {
		return nil, ErrStoreUnavailable
	}
	rows, absent, err := t.AgentMessages(p.LoopID, p.Thread)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []AgentMessageView{}
	}
	return AgentMessagesResponse{LoopID: p.LoopID, Thread: p.Thread, Absent: absent, Messages: rows}, nil
}
