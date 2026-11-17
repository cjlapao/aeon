package protocol

// inbox_write.go — S58 T3: the visibility writes (SV3, P3, SV12).
//
// Three doors, one act each: hide what I have seen (`inbox.ack`), hide it
// until a moment (`inbox.snooze`), bring it back (`inbox.unack`). They are
// the WRITE half of the same honesty the reads carry: the actor is the peer
// the socket authenticated, never a param; the key must name an item that
// is CURRENTLY needing attention (acking a ghost is refused so the table
// cannot be filled with arbitrary keys); an already-acked ack and an
// un-acked unack are no-ops that write NO row (P3).
//
// # The person, not the agent (SV12)
//
// Hiding a problem is a person's decision. A connection whose peer
// identity is an agent is refused with the sentence the spec fixed:
// "acknowledging is a person's act". There is no tool twin, no narrow
// projection, no CLI-as-sole-door (T6 adds the CLI as a client).
//
// # Absence (V13)
//
// All three register only over a non-nil InboxWrite seam.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// InboxWriteService is the daemon seam over the attention AckService. The
// actor arrives ALREADY validated as a person by the handler — the seam
// takes the identity, never re-derives it from params.
type InboxWriteService interface {
	// InboxAck hides the key; result carries the item as it now reads
	// (acked, still counted) or nothing when the write was a no-op.
	InboxAck(ctx context.Context, actor Actor, key string) (InboxAckResult, error)
	// InboxSnooze hides until a moment inside the horizon; `until` is the
	// parsed instant so the service owns the horizon's clock.
	InboxSnooze(ctx context.Context, actor Actor, key string, until string) (InboxAckResult, error)
	// InboxUnack returns the item to the default view.
	InboxUnack(ctx context.Context, actor Actor, key string) (InboxAckResult, error)
}

// --- params -----------------------------------------------------------------

// InboxAckParams names the ONE thing a write may name: the key of the item
// being acted on. An actor in params is refused before parsing (SV12).
type InboxAckParams struct {
	Key string `json:"key"`
}

// InboxSnoozeParams: key + the RFC 3339 instant to hide until.
type InboxSnoozeParams struct {
	Key   string `json:"key"`
	Until string `json:"until"`
}

// InboxUnackParams: key.
type InboxUnackParams struct {
	Key string `json:"key"`
}

// InboxAckResult carries the item as it reads AFTER the mark — a client
// updates its row without a re-list. Absent (`null`) when the write was an
// idempotent no-op: nothing changed, so there is no new reading to hand
// back, and an empty result is honest where a stale item would lie.
type InboxAckResult struct {
	Item *AttentionItem `json:"item,omitempty"`
}

// --- handlers ---------------------------------------------------------------

// inboxIdentityKeys names the identity keys an inbox write may never carry:
// the actor is stamped by the door that holds the connection; a param that
// names it would let a caller forge who saw the problem (SV12's wire rule —
// the same shape BK13 draws for backups).
var inboxIdentityKeys = []string{"actor", "actorKind", "who"}

func rejectInboxIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range inboxIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — acknowledging is a person's act and the person is the peer the socket authenticated, never a name in the request (SV12)",
				method, key)}
		}
	}
	return nil
}

// inboxPersonActor resolves the acting identity from the CONNECTION. The
// honest absence (no identity seam) and an empty id refuse like backup's
// door; an agent-kind peer is the refusal SV12 exists for.
func (t *Table) inboxPersonActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("acknowledging is a person's act and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("acknowledging is a person's act and no identity is available on this connection — the daemon stamped none")}
	}
	if a.Kind == "agent" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("acknowledging is a person's act — this connection acts as agent %q, and an agent cannot hide what needs a person (SV12)", a.ID)}
	}
	return a, nil
}

func inboxKeyOrRefuse(key string, method string) error {
	if strings.TrimSpace(key) == "" {
		return ErrBadParams{Err: fmt.Errorf("%s needs the key of the item you are marking (from inbox.list)", method)}
	}
	return nil
}

func (t *Table) handleInboxAck(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectInboxIdentityKeys("inbox.ack", params); err != nil {
		return nil, err
	}
	var p InboxAckParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := inboxKeyOrRefuse(p.Key, "inbox.ack"); err != nil {
		return nil, err
	}
	actor, err := t.inboxPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.InboxWrite.InboxAck(ctx, actor, p.Key)
}

func (t *Table) handleInboxSnooze(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectInboxIdentityKeys("inbox.snooze", params); err != nil {
		return nil, err
	}
	var p InboxSnoozeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := inboxKeyOrRefuse(p.Key, "inbox.snooze"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Until) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("inbox.snooze needs an `until` instant (RFC 3339) — a snooze without a wake-up time is an ack with extra steps")}
	}
	actor, err := t.inboxPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.InboxWrite.InboxSnooze(ctx, actor, p.Key, p.Until)
}

func (t *Table) handleInboxUnack(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectInboxIdentityKeys("inbox.unack", params); err != nil {
		return nil, err
	}
	var p InboxUnackParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := inboxKeyOrRefuse(p.Key, "inbox.unack"); err != nil {
		return nil, err
	}
	actor, err := t.inboxPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.InboxWrite.InboxUnack(ctx, actor, p.Key)
}

// RegisterInboxWrites registers the three writes over a non-nil seam (V13:
// a supervision-less build answers method-not-found, never a refusal).
func (t *Table) RegisterInboxWrites() bool {
	if t.InboxWrite == nil {
		return false
	}
	t.register(&Method{
		Name:               MethodInboxAck,
		RequiresInitialize: true,
		Handle:             t.handleInboxAck,
	})
	t.register(&Method{
		Name:               MethodInboxSnooze,
		RequiresInitialize: true,
		Handle:             t.handleInboxSnooze,
	})
	t.register(&Method{
		Name:               MethodInboxUnack,
		RequiresInitialize: true,
		Handle:             t.handleInboxUnack,
	})
	return true
}
