package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

// # The subscription registry (T5, O4, I6)
//
// **One registry, and this is it.** No per-handler bookkeeping: D6 asserts that
// dropping a connection empties the registry, and a second store of
// subscriptions anywhere would make that assertion meaningless while looking
// like belt and braces.
//
// # Subscriptions are per-connection and die with it
//
// A durable subscription is server-held state keyed to a client that may never
// come back — on a daemon meant to run for weeks, a slow leak with a
// client-shaped cause. Reconnection is a cursor's job (`after`), not a
// subscription's.

// The bounds (O4). **These numbers are guesses.**
//
// Nothing has measured a real client's subscription count, because no real
// client exists yet. They are here because an unbounded registry on a long-
// running daemon is a leak, and a refused subscription that says so is better
// than a daemon that slowly stops answering. **Revisit when S9's app and a
// real editor extension exist** — the app's dashboard plus a per-pane
// subscription is the case that will move MaxPerConnection first.
const (
	MaxSubscriptionsPerConnection = 32
	MaxSubscriptionsTotal         = 512
)

// Subscription is one client's standing request.
type Subscription struct {
	ID     string
	ConnID string
	Filter Filter
	// Next is the seq this subscription expects to deliver next. Advanced as
	// events are delivered, so `debug subscriptions` can show how far along a
	// client is without asking the client.
	Next uint64
	// delivered counts what has been handed to the transport for this subscription (see Event.Dseq).
	delivered uint64
	// ticked is `delivered` as of the last heartbeat; a subscription is due one while delivered has moved past it.
	ticked uint64
}

// Subscriptions holds every subscription, keyed by connection then id.
type Subscriptions struct {
	mu     sync.RWMutex
	byConn map[string]map[string]*Subscription
	total  int

	// newID is indirected so a test can make ids predictable without a global.
	newID func() string
}

func NewSubscriptions() *Subscriptions {
	return &Subscriptions{
		byConn: map[string]map[string]*Subscription{},
		newID:  randomSubscriptionID,
	}
}

func randomSubscriptionID() string {
	var b [4]byte
	// crypto/rand rather than math/rand: not for secrecy — a subscription id is
	// not a capability — but because math/rand without seeding gives every
	// daemon run the same ids, which makes two logs impossible to tell apart.
	_, _ = rand.Read(b[:])
	return "s-" + hex.EncodeToString(b[:])
}

// Add registers a subscription, or refuses it REPORTABLY (O4).
//
// Refused rather than silently dropped: a client whose subscription vanished
// looks exactly like a client whose filter matches nothing, which is the
// failure O1 spends a whole error code avoiding.
func (r *Subscriptions) Add(connID string, filter Filter, from uint64) (*Subscription, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.total >= MaxSubscriptionsTotal {
		return nil, ErrSubscriptionLimit.WithWhat(
			"the daemon already holds %d subscriptions, which is the total limit", r.total)
	}
	conn := r.byConn[connID]
	if len(conn) >= MaxSubscriptionsPerConnection {
		return nil, ErrSubscriptionLimit.WithWhat(
			"connection %s already holds %d subscriptions, which is the per-connection limit",
			connID, len(conn))
	}
	if conn == nil {
		conn = map[string]*Subscription{}
		r.byConn[connID] = conn
	}

	sub := &Subscription{ID: r.newID(), ConnID: connID, Filter: filter, Next: from}
	conn[sub.ID] = sub
	r.total++
	return sub, nil
}

// Remove drops one subscription. Removing one that is already gone is NOT an
// error (T3): a client racing its own teardown is ordinary, and making it an
// error would mean every client needs code to handle a race it did not cause.
func (r *Subscriptions) Remove(connID, subID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn, ok := r.byConn[connID]
	if !ok {
		return
	}
	if _, exists := conn[subID]; exists {
		delete(conn, subID)
		r.total--
	}
	if len(conn) == 0 {
		delete(r.byConn, connID)
	}
}

// RemoveConn drops EVERY subscription for a connection (T5).
//
// Called from the session's teardown, which the transport triggers by
// cancelling the connection's context — and it cancels before closing the
// socket, so this runs on the way down rather than after (Q5).
func (r *Subscriptions) RemoveConn(connID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total -= len(r.byConn[connID])
	delete(r.byConn, connID)
}

// Matching returns the subscriptions an event should be delivered to, in a
// stable order so two runs of the same test deliver in the same sequence.
func (r *Subscriptions) Matching(kind, componentRoot string) []*Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*Subscription
	for _, conn := range r.byConn {
		for _, sub := range conn {
			if sub.Filter.Matches(kind, componentRoot) {
				out = append(out, sub)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// All returns every subscription, for `aeon debug subscriptions`.
func (r *Subscriptions) All() []*Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Subscription
	for _, conn := range r.byConn {
		for _, sub := range conn {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Find returns one subscription on one connection, or nil.
//
// Scoped to the connection deliberately: a subscription id from another
// connection must not resolve, or one client could stream into another's
// subscription (T10).
func (r *Subscriptions) Find(connID, subID string) *Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byConn[connID][subID]
}

// Len is the total count. D6 asserts on this — the registry emptying is the
// thing that proves nothing was retained, where delivery stopping is merely
// what you observe when a connection closes regardless.
func (r *Subscriptions) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.total
}

// LenConn is one connection's count.
func (r *Subscriptions) LenConn(connID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byConn[connID])
}

// NextDelivery numbers the next event handed to the transport for one subscription: 1, 2, 3.
func (r *Subscriptions) NextDelivery(connID, subID string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.byConn[connID][subID]
	if !ok {
		return 0
	}
	sub.delivered++
	return sub.delivered
}

// MatchingHinted is Matching for a delivery whose notify hint is already resolved (see Filter.MatchesEvent).
func (r *Subscriptions) MatchingHinted(kind, componentRoot string, hinted bool) []*Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Subscription
	for _, conn := range r.byConn {
		for _, sub := range conn {
			if sub.Filter.MatchesEvent(kind, componentRoot, hinted) {
				out = append(out, sub)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DueForTick lists the subscriptions that have been handed something since their last heartbeat.
func (r *Subscriptions) DueForTick() []*Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Subscription
	for _, conn := range r.byConn {
		for _, sub := range conn {
			if sub.delivered > sub.ticked {
				out = append(out, sub)
			}
		}
	}
	return out
}

// MarkTicked records that a subscription has had its heartbeat, counted as a delivery itself, so it is not due again until
// something else is delivered.
func (r *Subscriptions) MarkTicked(connID, subID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sub, ok := r.byConn[connID][subID]; ok {
		sub.ticked = sub.delivered
	}
}

// Advance records that a subscription has been delivered up to and including
// seq, so `debug subscriptions` can report progress.
func (r *Subscriptions) Advance(connID, subID string, seq uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sub, ok := r.byConn[connID][subID]; ok && seq >= sub.Next {
		sub.Next = seq + 1
	}
}

// --- the handlers -----------------------------------------------------------

// handleSubscribe registers a subscription and reports where it resumed.
func (t *Table) handleSubscribe(_ context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p SubscribeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := p.Filter.Validate(); err != nil {
		return nil, err
	}

	from, replay := t.Events.Resume(p.After, p.Filter)

	sub, err := t.Subs.Add(sess.ConnID, p.Filter, from)
	if err != nil {
		return nil, err
	}

	// The replay and `subscription.created` are delivered by the daemon, which
	// owns the wire. Handing them back through a hook keeps this package free
	// of any notion of sending (T11).
	if t.OnSubscribed != nil {
		t.OnSubscribed(sub, replay)
	}
	return SubscribeResult{SubscriptionID: sub.ID, From: from}, nil
}

// handleUnsubscribe removes a subscription. Idempotent by design (T3).
func (t *Table) handleUnsubscribe(_ context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p UnsubscribeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	t.Subs.Remove(sess.ConnID, p.SubscriptionID)
	return UnsubscribeResult{}, nil
}

// handleDebugSubscriptions lists the registry so D6 can be shown.
func (t *Table) handleDebugSubscriptions(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	subs := t.Subs.All()
	out := DebugSubscriptionsResult{
		Connections:   t.Sessions.Len(),
		Subscriptions: make([]DebugSubscription, 0, len(subs)),
	}
	for _, sub := range subs {
		out.Subscriptions = append(out.Subscriptions, DebugSubscription{
			SubscriptionID: sub.ID,
			ConnID:         sub.ConnID,
			Filter:         sub.Filter,
			Next:           sub.Next,
		})
	}
	return out, nil
}
