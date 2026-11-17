package protocol

import (
	"context"
	"sync"
)

// # I3 — exactly ONE place decides whether a connection has initialised
//
// A second check drifts, the looser one wins, and T7 quietly becomes a
// suggestion. So every handler consults `Sessions` and none re-derives it;
// `session_test.go` asserts that `initialized` is read nowhere else.
//
// # Why this is keyed by connection id rather than held on a connection object
//
// The transport does not hand out a connection object — a handler gets a
// context carrying the connection's id (`localrpc.ConnIDFromContext`). That is
// enough, and it keeps this package transport-independent (T11): a connection
// id is a string, and S19's HTTP mount can mint one per request-stream without
// this package knowing what a socket is.

// Session is one connection's negotiated state.
type Session struct {
	ConnID string
	// Version is the NEGOTIATED protocol version for this connection — what the
	// daemon agreed to speak here, which may be older than PROTOCOL_VERSION.
	Version       int
	ClientName    string
	ClientVersion string
}

// Sessions is the registry of initialised connections.
//
// It is also where a connection's teardown is triggered from: `Open` parks a
// goroutine on the connection's context, which the transport cancels on
// disconnect, so `onClose` runs for every connection that ever initialised
// without this package knowing how a socket closes.
type Sessions struct {
	mu sync.RWMutex
	by map[string]*Session

	// onClose is called once per connection, after the transport cancels its
	// context. Set by the daemon to reap subscriptions and emit
	// `client.disconnected`.
	onClose func(connID string)
}

func NewSessions() *Sessions {
	return &Sessions{by: map[string]*Session{}}
}

// OnClose registers the teardown callback. Called once at wiring time.
func (s *Sessions) OnClose(fn func(connID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onClose = fn
}

// Open records a negotiated session and arms its teardown.
//
// Returns ErrAlreadyInitialized if this connection already has one: re-running
// negotiation would change the recorded version underneath any live
// subscription, so it is refused rather than allowed to silently succeed.
func (s *Sessions) Open(ctx context.Context, sess *Session) error {
	s.mu.Lock()
	if _, exists := s.by[sess.ConnID]; exists {
		s.mu.Unlock()
		return ErrAlreadyInitialized.WithWhat(
			"connection %s called `initialize` twice", sess.ConnID)
	}
	s.by[sess.ConnID] = sess
	s.mu.Unlock()

	// The connection's context, cancelled by the transport when the client
	// disconnects. This is the ONLY close signal available — the transport has
	// no close callback — so parking a goroutine here is what makes T5's
	// "subscriptions die with their connection" true rather than aspirational.
	go func() {
		<-ctx.Done()
		s.Close(sess.ConnID)
	}()
	return nil
}

// Close forgets a connection and runs the teardown callback, at most once.
func (s *Sessions) Close(connID string) {
	s.mu.Lock()
	_, existed := s.by[connID]
	delete(s.by, connID)
	fn := s.onClose
	s.mu.Unlock()

	// Only on the first close: the callback emits an event and reaps
	// subscriptions, and doing either twice would produce a duplicate
	// `client.disconnected`.
	if existed && fn != nil {
		fn(connID)
	}
}

// Get returns the session for a connection, and whether it has initialised.
//
// **This is the one place that decides** (I3). A handler that wants to know
// whether a connection may proceed calls Require, which calls this.
func (s *Sessions) Get(connID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.by[connID]
	return sess, ok
}

// Require returns the session or ErrNotInitialized, naming the method (T7).
//
// Every handler except `initialize` begins with this call. Naming the method in
// `what` matters: "connection is not initialized" leaves a client author
// guessing which of their calls was too early.
func (s *Sessions) Require(connID, method string) (*Session, error) {
	sess, ok := s.Get(connID)
	if !ok {
		return nil, ErrNotInitialized.WithWhat(
			"method %q was called before `initialize` on connection %s", method, connID)
	}
	return sess, nil
}

// Len is the number of initialised connections, for `debug` and for tests that
// assert the registry empties rather than inferring it from delivery stopping.
func (s *Sessions) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.by)
}
