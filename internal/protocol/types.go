package protocol

import "encoding/json"

// # Every method's request and result type lives here (I4)
//
// Not inline in a handler. The conformance fixtures (T5) are tested against
// these types, and a type declared inside a handler cannot be.
//
// # Unknown fields are IGNORED, in both directions (T3, D3)
//
// Go's encoding/json ignores unknown fields unless asked not to, so this is the
// default — and that is exactly why it needs a test. Nothing here calls
// `DisallowUnknownFields`, and `types_test.go` asserts that a request carrying
// `somethingFromTheFuture` still decodes. **A client that rejects an unknown
// field turns every additive change into a breaking one**, which would quietly
// cost the next fifteen slices their additive evolution.

// --- initialize --------------------------------------------------------------

// InitializeParams is the client stating what it NEEDS.
type InitializeParams struct {
	ClientName    string `json:"clientName"`
	ClientVersion string `json:"clientVersion"`
	// ProtocolVersion is the version the client needs, not an announcement of
	// its own build. Zero means "whatever you support" (see Negotiate).
	ProtocolVersion int `json:"protocolVersion"`
}

// InitializeResult is the daemon stating what it will SPEAK.
type InitializeResult struct {
	// ProtocolVersion is the NEGOTIATED version — what the daemon will speak on
	// this connection, which may be older than PROTOCOL_VERSION.
	ProtocolVersion int      `json:"protocolVersion"`
	DaemonVersion   string   `json:"daemonVersion"`
	Capabilities    []string `json:"capabilities"`
}

// --- ping --------------------------------------------------------------------

// PingParams is empty. A type rather than nothing, so the fixtures have
// something to round-trip and a later optional field is additive.
type PingParams struct{}

// PingResult is empty on purpose: the round trip is measured by the client, and
// a field it does not need is a field that has to be kept forever (T2).
type PingResult struct{}

// --- version -----------------------------------------------------------------

type VersionParams struct{}

// VersionResult reports the daemon's build and the NEGOTIATED protocol version.
//
// Negotiated rather than PROTOCOL_VERSION, so `aeon version` can say
// "negotiated" honestly. A bare number reads as a constant, and a constant
// proves nothing about whether a handshake happened.
type VersionResult struct {
	DaemonVersion string `json:"daemonVersion"`
	// ProtocolVersion is this connection's negotiated version.
	ProtocolVersion int    `json:"protocolVersion"`
	BuildTime       string `json:"buildTime,omitempty"`
	GitCommit       string `json:"gitCommit,omitempty"`
}

// --- subscribe / unsubscribe -------------------------------------------------

// Filter selects what a subscription receives (T4).
//
// The filter IS the point. One daemon multiplexes every project on the machine,
// so an editor open on one repository must not receive every event from every
// other — and server push would make it filter traffic it never wanted.
type Filter struct {
	// ComponentRoot matches EXACTLY. No prefix matching on paths: `/repo` must
	// not match `/repo-backup`. Empty matches any project.
	ComponentRoot string `json:"componentRoot,omitempty"`
	// Kinds matches exactly, or by a single trailing `*`. Empty matches any
	// kind. Any other wildcard form is ErrInvalidFilter (O1).
	Kinds []string `json:"kinds,omitempty"`
	// NotifyOnly narrows the subscription to events that carry a notify hint (a toast, a bell entry), plus any `kinds` named
	// alongside it. With NotifyOnly set and no `kinds`, an event with no hint is not delivered at all. The always-on notification
	// listener uses it, so it no longer takes a copy of every event the daemon publishes.
	NotifyOnly bool `json:"notifyOnly,omitempty"`
}

type SubscribeParams struct {
	Filter Filter `json:"filter"`
	// After is the highest seq the client has already seen; the daemon replays
	// from After+1. Nil means "from now". A pointer because 0 is a meaningful
	// value — "I have seen nothing, send me everything you still hold" — and
	// distinguishing it from absent is the difference between replaying the
	// retained log and starting at the head.
	After *uint64 `json:"after,omitempty"`
}

type SubscribeResult struct {
	SubscriptionID string `json:"subscriptionId"`
	// From is the seq the subscription actually resumed at.
	//
	// It exists so the client can tell. If From is greater than After+1 the
	// daemon could not serve the whole cursor — the retained log no longer goes
	// back that far — and the client knows it missed From-(After+1) events
	// instead of assuming continuity. **Reporting where you actually resumed is
	// the difference between a gap and a silent gap.**
	From uint64 `json:"from"`
}

type UnsubscribeParams struct {
	SubscriptionID string `json:"subscriptionId"`
}

// UnsubscribeResult is empty. Unsubscribing an id that is already gone is not
// an error (T3): a client racing its own teardown is ordinary.
type UnsubscribeResult struct{}

// --- events ------------------------------------------------------------------

// Event is the notification payload, sent as JSON-RPC `method: "event"`.
type Event struct {
	// SubscriptionID tags which subscription delivered this, so two
	// subscriptions on one connection are distinguishable (D7).
	SubscriptionID string `json:"subscriptionId"`
	// Seq is monotonic within the stream. See eventLog for its scope and the
	// limitation this slice ships with.
	Seq uint64 `json:"seq"`
	// Dseq is this subscription's own delivery counter: 1, 2, 3, with no gaps, assigned as each event is handed to the transport.
	// `seq` cannot show a loss, because a filtered subscription skips most of the log's numbers on purpose. A gap in `dseq`
	// can only mean a notification was dropped on the way (the connection's queue was full), and tells the client to re-read.
	Dseq uint64 `json:"dseq,omitempty"`
	Kind string `json:"kind"`
	// ComponentRoot is the project the event belongs to, empty for daemon-wide
	// events such as `daemon.started`.
	ComponentRoot string `json:"componentRoot,omitempty"`
	// At is RFC3339 with nanoseconds.
	At string `json:"at"`
	// Payload is kind-specific and deliberately opaque to the envelope, so a
	// new kind is additive (T2).
	Payload json.RawMessage `json:"payload,omitempty"`
	// Notify is a delivery-time-only annotation: present when this moment is
	// worth a human-facing toast, on top of whatever state update Payload
	// already carries for a subscribed screen. One event, both effects.
	//
	// Deliberately never persisted: it is attached to the in-memory Event
	// AFTER EventLog.Append returns, so the durable log and anything replayed
	// from it (a cursor resume, `debug roundtrip`) never carries it. A
	// reconnect's catch-up must not re-surface a toast for something already
	// shown live — see NotifyHint.
	Notify *NotifyHint `json:"notify,omitempty"`
}

// NotifyHint says a published event is worth surfacing as a toast, and how.
//
// Never a reason to add a new event kind: the kind and Payload already say
// what happened and keep a subscribed screen's state correct. NotifyHint is
// the human-facing sentence layered on top, for whoever is not looking at
// that screen right now.
type NotifyHint struct {
	Title string     `json:"title"`
	Body  string     `json:"body,omitempty"`
	Tone  NotifyTone `json:"tone"`
	// AttentionKey (S58 SV4/SV7) is the inbox subject key this event
	// names — the same `<source>:<id>:<since>` string the inbox itself
	// mints, so the app and the dispatcher can dedup the two events a
	// single question raises (`loop.waiting` and `question.asked`) as ONE
	// attention item. Empty for subjects the inbox does not carry (a
	// finished loop, a budget warning): absence is honest, not a gap.
	AttentionKey string `json:"attentionKey,omitempty"`
	// DeepLink (S58 SV9) is a navigation target and nothing else:
	// aeon://loop/<id> or aeon://inbox, no token, no answer, no
	// capability. The shell routes it; opening it shows the normal local
	// door. Nothing remote is answered by a link (S19 T5's permanence).
	DeepLink string `json:"deepLink,omitempty"`
}

// NotifyTone is closed, matching the app's own toast/pill vocabulary.
type NotifyTone string

const (
	NotifyInfo    NotifyTone = "info"
	NotifySuccess NotifyTone = "success"
	NotifyWarning NotifyTone = "warning"
	NotifyDanger  NotifyTone = "danger"
)

// --- debug subscriptions -----------------------------------------------------

// DebugSubscriptionsResult lists the registry (T6).
//
// A sixth method, added for one reason: D6 asserts that dropping a connection
// empties the registry, and a registry that empties is a CLAIM where a command
// that prints it empty is a demonstration. It is read-only and names nothing
// a peer could not already see about itself.
type DebugSubscriptionsResult struct {
	Connections   int                 `json:"connections"`
	Subscriptions []DebugSubscription `json:"subscriptions"`
}

// DebugGoroutinesResult is the daemon's own goroutine stacks — S14 T3, for
// one demonstration: DEMO §4 shows `aeon debug goroutines | grep -c agentsvc`
// answering 0. That is AS4 as DATA: an async Task is a suspension, not a
// concurrency model, and the daemon's stacks are the evidence. Marked Debug
// for the same reason debug.subscriptions is — on a public HTTP surface this
// would be operational control (indeed an information leak) by another name.
type DebugGoroutinesResult struct {
	Goroutines int    `json:"goroutines"`
	Stacks     string `json:"stacks"`
}

type DebugSubscription struct {
	SubscriptionID string `json:"subscriptionId"`
	ConnID         string `json:"connId"`
	Filter         Filter `json:"filter"`
	Next           uint64 `json:"next"`
}
