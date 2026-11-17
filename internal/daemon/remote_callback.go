package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/remote"
)

// remote_callback.go — the daemon seam for remote.complete (S21 T6, RA7/RA8).
//
// The trigger_webhook.go shape again, deliberately almost empty of LOGIC:
// verification, the ±window, the three binding refusals and the landing CAS
// all live one layer down in internal/remote, shared with the peer config
// that carries the keys — so the socket, the narrow HTTP route and the stdio
// tool answer to the SAME doors, and no mounting decides anything on its own.
// The daemon's whole job here is to hand the completion to that mount point
// and render its outcome — a refusal is an ANSWER (401/404/403 are outcomes
// the sender must be able to read); errors are for the transport having
// failed, and for the wiring being absent, stated as the wiring fault it is.

// remoteCost maps the transport's claim shape onto the package's (protocol
// never imports internal/remote — the daemon maps, as it maps every seam).
// Nil stays nil: the absence IS the fact (RA10), and inventing an empty
// report here would be inventing a claim.
func remoteCost(r *protocol.RemoteCostReport) *remote.CostReport {
	if r == nil {
		return nil
	}
	return &remote.CostReport{USD: r.USD, Currency: r.Currency}
}

func (d *Daemon) wireRemoteComplete() {
	d.Table.CompleteRemote = func(ctx context.Context, p protocol.RemoteCompleteParams) (any, error) {
		if d.remoteCallbacks == nil {
			// A wiring fault, stated as one — never a silent "refused",
			// which would blame the peer for the daemon's own shape. Peers
			// are off by default (RA9): an unwired daemon is CORRECT, but
			// it must say unwired, not untrusted.
			return nil, protocol.ErrStoreUnavailable.WithWhat("no remote peers are wired in this daemon — completions have nothing to bind to")
		}
		a := d.remoteCallbacks.Deliver(remote.Completion{
			Task:       p.Task,
			Peer:       p.Peer,
			Result:     p.Result,
			Signature:  p.Signature,
			Timestamp:  p.Timestamp,
			DeliveryID: p.DeliveryID,
			Cost:       remoteCost(p.Cost),
		})
		return protocol.RemoteCompleteResult{
			TaskID:     a.TaskID,
			Peer:       a.Peer,
			Outcome:    a.Outcome,
			Reason:     a.Reason,
			HTTPStatus: a.HTTPStatus,
		}, nil
	}
}
