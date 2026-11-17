package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// webhook.go — S20 T6: the webhook delivery as a METHOD, not a listener.
//
// TR11 is the shape: a webhook is a TRIGGER MOUNTING on the surface that
// already exists, not a second server to secure, document and keep narrow
// (P25's one-protocol rule). That makes the delivery a normal protocol
// method with a normal fixture — and it forces the one design consequence
// that matters on stdio (N18, R-6): **the signature travels as a param**,
// because stdio has no headers. The HTTP mounting accepts GitHub's
// `X-Hub-Signature-256` as sugar for the same param; both fold into
// `signature` before this method runs, and the daemon sees one request
// shape from every transport.
//
// The answer is the FIVE-WORD vocabulary (TR4), not an error: a delivery
// that did not fire says WHY in the same outcome language the trigger
// records use, because "did it fire?" asked through a POST deserves the
// same five answers the schedule's rows hold. A refused signature is a
// 401 OUTCOME (auth failed) carried in a 200-shaped transport — the
// transport layer only ever failed if the socket did.

// WebhookDeliverParams is the one request shape both mountings fold into.
// Payload is the EXACT bytes the sender signed, carried as a string
// (Go strings are byte-exact; the HMAC runs over these bytes and nothing
// else — no re-serialization stands between the signature and its proof).
type WebhookDeliverParams struct {
	ID         string `json:"id"`
	Payload    string `json:"payload"`
	Signature  string `json:"signature,omitempty"`
	// Event is the sender's event kind (GitHub's `X-GitHub-Event`), folded from the header like the signature. A webhook with an
	// event filter acts only on the events it names.
	Event string `json:"event,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	DeliveryID string `json:"deliveryId,omitempty"`
}

// WebhookDeliverResult is the discriminated answer. HTTPStatus is the
// mounting's rendering hint (401 for a failed signature, 404 for an
// unknown id, 200 for fired) — narrow renders it, the daemon only speaks
// the outcome.
type WebhookDeliverResult struct {
	TriggerID  string `json:"triggerId"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	LoopID     string `json:"loopId,omitempty"`
	HTTPStatus int    `json:"httpStatus"`
}

// handleWebhookDeliver validates SHAPE only — presence of an id, nothing
// else. The signature is verified (or refused) one layer down where the
// trigger row and its secret's env-var NAME live; validating it here would
// put authentication in the transport, and authentication is not this
// layer's answer to anything (TR9).
func (t *Table) handleWebhookDeliver(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p WebhookDeliverParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("a webhook delivery must name its trigger id")}
	}
	if t.DeliverWebhook == nil {
		return nil, ErrStoreUnavailable.WithWhat("this daemon has no webhook deliverer wired")
	}
	return t.DeliverWebhook(ctx, p)
}
