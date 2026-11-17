package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
)

// trigger_webhook.go — the daemon seam for webhook.deliver (S20 T6, TR11).
//
// This file is deliberately almost empty of LOGIC: authentication,
// replay, labelling and the gates all live one layer down in
// internal/trigger, shared with the clock's fires, so a delivery and a
// schedule answer to the same doors. The daemon's whole job here is to
// hand the delivery to that layer and render its five-answer outcome —
// never an error path for a refused signature (a 401 is an ANSWER, and
// errors are for the transport having failed).

func (d *Daemon) wireWebhookDeliver() {
	d.Table.DeliverWebhook = func(ctx context.Context, p protocol.WebhookDeliverParams) (any, error) {
		if d.triggers == nil {
			// A wiring fault, stated as one — never a silent "did not fire",
			// which would blame the sender for the daemon's own shape.
			return nil, protocol.ErrStoreUnavailable.WithWhat("the trigger scheduler is not wired in this daemon")
		}
		a := d.triggers.DeliverWebhook(ctx, p.ID, []byte(p.Payload), p.Signature, p.Timestamp, p.DeliveryID, p.Event, d.triggers.Clock.Now())
		return protocol.WebhookDeliverResult{
			TriggerID:  a.TriggerID,
			Outcome:    string(a.Outcome),
			Reason:     a.Reason,
			LoopID:     a.LoopID,
			HTTPStatus: a.HTTPStatus,
		}, nil
	}
}
