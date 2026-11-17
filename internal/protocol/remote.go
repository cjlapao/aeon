package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// remote.go — S21 T6 (RA7, RA8): the completion of an async remote Task as
// a METHOD, not a listener — the exact shape webhook.deliver established at
// S20 T6, one layer of the same argument up.
//
// RA7: an async remote Task is a SUSPENSION, not a held connection (a held
// connection dies with the daemon, and P2 exists precisely so a suspended
// step survives that). So the completion arrives the way the delivery does:
// an inbound call to the surface that already exists, mounted as one
// enumerated action (P25), signature-verified like a webhook (TR9) — and
// then BOUND, which is where it diverges: a webhook delivery names a trigger
// row; a completion names a TASK THIS DAEMON CREATED, still awaiting, whose
// CALLEE IS THE SENDER. Verification proves who sent it; binding proves what
// they were asked (RA8). Without binding the endpoint injects skill results
// into transcripts — N6's fabricated prompt wearing a valid signature.
//
// The signature rides as a PARAM (N18: stdio has no headers); the HTTP
// mounting's X-Aeon-Completion-* headers are sugar for the same fields, both
// fold into one request shape before this method runs.
//
// Authentication and binding live ONE LAYER DOWN (internal/remote's
// Callbacks, shared with the peer config that carries the keys) — validating
// signatures here would put authentication in the transport, and the
// transport answers only "did the socket fail".

// RemoteCompleteParams is the one request shape both mountings fold into.
// Task is the integer task id as a string — the same taskId the lifecycle
// events carry, echoed by the peer unchanged.
type RemoteCompleteParams struct {
	Task       string `json:"task"`
	Peer       string `json:"peer"`
	Result     string `json:"result"`
	Signature  string `json:"signature,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	DeliveryID string `json:"deliveryId,omitempty"`
	// Cost is the peer's optional claim about its own spend (RA10) — the
	// same fields the signed canonical binds one layer down; nil is the
	// normal case and lands as unknown, never zero.
	Cost *RemoteCostReport `json:"cost,omitempty"`
}

// RemoteCostReport mirrors remote.CostReport across the transport boundary
// (protocol never imports internal/remote — the daemon maps, as it maps
// every other seam type).
type RemoteCostReport struct {
	USD      float64 `json:"usd"`
	Currency string  `json:"currency,omitempty"`
}

// RemoteCompleteResult is the discriminated answer. Outcome is the closed
// three — landed / refused / could_not_say (the webhook's five-word
// vocabulary narrowed to what this surface can honestly mean) — and
// HTTPStatus is the mounting's rendering hint (a 200-on-a-refusal would
// teach senders to stop reading the body; the webhook route argued this
// exact point one slice ago).
type RemoteCompleteResult struct {
	TaskID     string `json:"taskId"`
	Peer       string `json:"peer"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	HTTPStatus int    `json:"httpStatus"`
}

// handleRemoteComplete validates SHAPE only — a completion must name its
// task and its peer. Everything else is the layer below's answer.
func (t *Table) handleRemoteComplete(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p RemoteCompleteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Task == "" || p.Peer == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a completion must name its task id and its peer")}
	}
	if t.CompleteRemote == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no remote completer wired")
	}
	return t.CompleteRemote(ctx, p)
}
