package daemon

// inbox_write.go — S58 T3: the inbox visibility WRITES on the human door
// (SV3, SV12, P3, SV-D11). The daemon owns the clock, the actor's landing
// in the row, and the error translation from attention's sentinels to the
// protocol's named refusals. The write itself is the store's append-only
// marker; the ghost check is a FRESH evaluation, so "no longer needing
// attention" is measured at the moment of the act.

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/protocol"
)

// ackService builds the write service over the same registry the reads
// evaluate. Stateless by construction (fresh per call, like everything
// here): the LiveItems closure re-evaluates, the store stamps occurred_at.
func (svc *InboxService) ackService() *attention.AckService {
	return &attention.AckService{
		Write: svc.d.store,
		LiveItems: func(ctx context.Context) ([]attention.Item, error) {
			scope, err := attention.EveryScope(svc.d.store)
			if err != nil {
				return nil, err
			}
			// ACKED INCLUDED: the ghost set is everything CURRENTLY
			// needing attention (an already-acked key is not a ghost —
			// it is a no-op), and the no-op rule reads each item's
			// Acked state.
			res, err := svc.registry.Evaluate(ctx, attention.Query{
				Scope:        scope,
				IncludeAcked: true,
				Limit:        attention.MaxLimit,
			})
			if err != nil {
				return nil, err
			}
			return res.Items, nil
		},
	}
}

func (svc *InboxService) InboxAck(ctx context.Context, actor protocol.Actor, key string) (protocol.InboxAckResult, error) {
	wrote, err := svc.ackService().Ack(ctx, actor.Kind, actor.ID, key)
	if err != nil {
		return protocol.InboxAckResult{}, inboxWriteRefusal(err)
	}
	return svc.ackResult(ctx, wrote, key)
}

func (svc *InboxService) InboxSnooze(ctx context.Context, actor protocol.Actor, key, until string) (protocol.InboxAckResult, error) {
	t, ok := parseRFC3339(until)
	if !ok {
		return protocol.InboxAckResult{}, protocol.ErrBadParams{Err: fmt.Errorf("inbox.snooze `until` must be an RFC 3339 instant — %q does not parse (try `2026-11-05T09:00:00Z`)", until)}
	}
	wrote, err := svc.ackService().Snooze(ctx, actor.Kind, actor.ID, key, t)
	if err != nil {
		return protocol.InboxAckResult{}, inboxWriteRefusal(err)
	}
	return svc.ackResult(ctx, wrote, key)
}

func (svc *InboxService) InboxUnack(ctx context.Context, actor protocol.Actor, key string) (protocol.InboxAckResult, error) {
	wrote, err := svc.ackService().Unack(ctx, actor.Kind, actor.ID, key)
	if err != nil {
		return protocol.InboxAckResult{}, inboxWriteRefusal(err)
	}
	return svc.ackResult(ctx, wrote, key)
}

// ackResult hands back the item as it reads AFTER the mark — the client
// updates its row without a re-list. A no-op write answers with NOTHING
// (nothing changed; a stale item would be a lie of effort), and an item
// that left the world between the check and the read (a recurrence
// cleared) likewise has no current reading to hand back.
func (svc *InboxService) ackResult(ctx context.Context, wrote bool, key string) (protocol.InboxAckResult, error) {
	if !wrote {
		return protocol.InboxAckResult{}, nil
	}
	items, err := svc.ackService().LiveItems(ctx)
	if err != nil {
		// The WRITE landed; refusing the whole call because the
		// after-picture could not be drawn would make the client believe
		// the mark itself failed. Say what is true: no item, no lie.
		return protocol.InboxAckResult{}, nil
	}
	for _, it := range items {
		if it.Key == key {
			mapped := mapItems([]attention.Item{it})
			return protocol.InboxAckResult{Item: &mapped[0]}, nil
		}
	}
	return protocol.InboxAckResult{}, nil
}

// inboxWriteRefusal translates attention's sentinels into the protocol's
// named refusals. Anything else rides through untouched (the handler
// already owns the wire shape of an unknown failure).
func inboxWriteRefusal(err error) error {
	switch {
	case errors.Is(err, attention.ErrGhostKey):
		return protocol.ErrInboxScope.WithWhat("%v — an ack may only name an item that is CURRENTLY needing attention (from a fresh inbox.list), so the marker table cannot be filled with ghosts", err)
	case errors.Is(err, attention.ErrSnoozeHorizon):
		return protocol.ErrSnoozeHorizon.WithWhat("%v", err)
	default:
		return err
	}
}
