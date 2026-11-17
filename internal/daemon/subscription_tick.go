package daemon

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
)

// subscription_tick.go — a heartbeat that makes the LAST lost event visible.
//
// Each subscription numbers its deliveries (Event.Dseq), and a client that sees the number jump knows something was dropped.
// But a jump needs a LATER event to show it: if the final event before a quiet spell is the one that was dropped (a loop
// resumed, then nothing for a minute), nothing ever reveals the gap. So a subscription that has been delivered something is
// sent one heartbeat a moment later. It is a delivery like any other and carries the next number, so a missing event before
// it shows as a jump right there, and the client re-reads. A subscription that has been delivered nothing is sent nothing,
// and a heartbeat does not cause another one.

const subscriptionTickEvery = 3 * time.Second

// RunSubscriptionTicks sends the heartbeats until ctx ends.
func (d *Daemon) RunSubscriptionTicks(ctx context.Context) {
	t := time.NewTicker(subscriptionTickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tickSubscriptions()
		}
	}
}

func (d *Daemon) tickSubscriptions() {
	for _, sub := range d.Table.Subs.DueForTick() {
		ev := protocol.Event{Seq: d.Table.Events.Head(), Kind: protocol.KindSubscriptionTick, At: time.Now().UTC().Format(time.RFC3339Nano)}
		d.deliver(sub, ev)
		d.Table.Subs.MarkTicked(sub.ConnID, sub.ID)
	}
}
