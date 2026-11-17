package protocol

import (
	"context"
	"encoding/json"
	"time"
)

// The handlers for the three methods that do not involve subscriptions.
//
// Each decodes its own params (SEC-T6-005: the transport validates the envelope
// and nothing else), and none of them re-checks whether the connection has
// initialised — Dispatch did that, in one place (I3).

// handleInitialize negotiates, records the session, and arms its teardown.
func (t *Table) handleInitialize(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p InitializeParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}

	// The client states what it NEEDS; the daemon answers with what it will
	// SPEAK. Negotiate owns the three outcomes, including the one that keeps an
	// old editor extension working.
	speak, err := Negotiate(p.ProtocolVersion)
	if err != nil {
		return nil, err
	}

	sess.Version = speak
	sess.ClientName = p.ClientName
	sess.ClientVersion = p.ClientVersion
	if err := t.Sessions.Open(ctx, sess); err != nil {
		return nil, err
	}

	// `client.connected` is emitted by the daemon's lifecycle wiring rather
	// than here, because it carries peer credentials that only the transport
	// knows (D12) — and this package must not know what a peer is.
	if t.OnInitialized != nil {
		t.OnInitialized(sess)
	}

	return InitializeResult{
		ProtocolVersion: speak,
		DaemonVersion:   t.DaemonVersion,
		Capabilities:    Capabilities(),
	}, nil
}

// handlePing answers with an empty result.
//
// The round trip is measured by the client, so the result carries nothing: a
// field a client does not need is a field that has to be kept forever (T2's
// breaking column includes removing one).
func (t *Table) handlePing(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p PingParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return PingResult{}, nil
}

// handleVersion reports the build and the NEGOTIATED protocol version.
//
// Negotiated, from the session — not PROTOCOL_VERSION. The difference is the
// whole point: a handler returning the constant would make `aeon version`'s
// "negotiated" label a lie that no test of the happy path would catch.
func (t *Table) handleVersion(_ context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p VersionParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return VersionResult{
		DaemonVersion:   t.DaemonVersion,
		ProtocolVersion: sess.Version,
		BuildTime:       t.BuildTime,
		GitCommit:       t.GitCommit,
	}, nil
}

// decodeParams decodes params into v, tolerating absence and unknown fields.
//
// Absent params are not an error: a method whose params are all optional is
// legitimately called with none, and JSON-RPC permits omitting the member.
//
// Unknown fields are IGNORED (T3) — this is where that property actually lives,
// and `types_test.go` asserts nothing in this package ever calls
// DisallowUnknownFields.
func decodeParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		// -32602 is JSON-RPC's own: the shape of the params is a protocol-level
		// fact, not an Aeon one, so it does not take an Aeon code.
		return ErrBadParams{Err: err}
	}
	return nil
}

// ErrBadParams marks a params decode failure so the transport layer answers
// with JSON-RPC's -32602 rather than an Aeon code.
type ErrBadParams struct{ Err error }

func (e ErrBadParams) Error() string { return "invalid params: " + e.Err.Error() }
func (e ErrBadParams) Unwrap() error { return e.Err }

// now is indirected so a test can fix the clock without reaching for a global.
var now = time.Now
