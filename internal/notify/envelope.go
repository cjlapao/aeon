package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// envelope.go — S58 T5 (SV6): everything that leaves the machine on a
// channel is this closed struct and nothing else. Field set, key order
// and cap are the contract; the builder has no parameter that could
// carry a prompt, a goal, a message, a path, a command, a cost, a
// component NAME, a branch or a token — not filtered out on the way
// past, unreachable in the first place (the only way to keep such a
// promise is a type with nowhere to put it).

// MaxEnvelopeBytes is the design's cap on the wire body.
const MaxEnvelopeBytes = 1024

// ResolvedEvent is the closed fact-set the envelope is built from: ids,
// slugs (grammar-checked), enums and the template-rendered title. It is
// produced by the resolver and the wire layer from event payloads and
// rows — and it is the ONLY input Build takes. There is no path from a
// protocol.Item or an event payload type into an Envelope.
type ResolvedEvent struct {
	// Seq is the durable event's sequence number (0 for a test or a
	// digest — nothing to point at).
	Seq int64
	// DeliveryID is the unique id for THIS envelope: deterministic from
	// (seq, channel) for an event, so a re-send carries the same id and
	// the receiver can dedup honestly. Minted by the dispatcher.
	DeliveryID string
	// At is the RFC3339 instant of delivery (the envelope's own clock).
	At string
	// Kind is the wire kind (loop.blocked); Variant the class variant
	// ("" for a base row). The envelope's `kind` field is their
	// class-name join (`loop.blocked.action-denied`), the string the
	// class table speaks.
	Kind    string
	Variant string
	// Severity is the attention enum value the class maps to.
	Severity string
	// RootSlug/LoopSlug are store slugs (grammar-checked), never names.
	RootSlug string
	LoopSlug string
	// ComponentRoot is the event's component root PATH — a routing fact
	// for subscriptions, deliberately NOT part of the envelope (paths are
	// on SV6's never-list: they disclose layout).
	ComponentRoot string
	// LoopID is the routing id (deepLink + delivery log correlation); the
	// deepLink itself is the receiver's navigation target.
	LoopID       string
	Title        string
	AttentionKey string
	DeepLink     string
	// IsTest / IsDigest mark the two synthetic deliveries; the envelope's
	// event field carries "test" / "inbox.digest" / "rate.summary"
	// accordingly (Kind stays the source kind, empty for a test).
	IsTest   bool
	IsDigest bool
	// DigestCount rides the digest envelope's title ("N items need
	// attention") — a count, never the items.
	DigestCount int
}

// Envelope is the wire body. The field ORDER is the key order
// (encoding/json emits struct order) — a fixed order makes the signature and the
// golden byte-stable without a canonicaliser.
type Envelope struct {
	V        int    `json:"v"`
	ID       string `json:"id"`
	At       string `json:"at"`
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Kind     string `json:"kind,omitempty"`
	RootSlug string `json:"rootSlug,omitempty"`
	LoopSlug string `json:"loopSlug,omitempty"`
	Title    string `json:"title"`
	DeepLink string `json:"deepLink,omitempty"`
}

// Build turns the closed facts into the closed body.
func Build(ev ResolvedEvent) Envelope {
	e := Envelope{V: 1, ID: ev.DeliveryID, At: ev.At, Title: ev.Title,
		Severity: ev.Severity, DeepLink: ev.DeepLink}
	switch {
	case ev.IsTest:
		e.Event = "test"
		e.Kind = "test" // a receiver sorts on one field; test names itself there too
	case ev.IsDigest:
		e.Event = "inbox.digest"
	default:
		e.Event = ev.Kind
		e.Kind = ev.Kind
		if ev.Variant != "" {
			e.Kind = ev.Kind + "." + ev.Variant
		}
		e.RootSlug = ev.RootSlug
		e.LoopSlug = ev.LoopSlug
	}
	return e
}

// BuildDigest is the one synthetic-with-content: a COUNT and nothing else
// (SV-D6 — the compensation for "daemon was down" is honest arithmetic,
// never a replay of stale per-event messages).
func BuildDigest(ev ResolvedEvent) Envelope {
	e := Build(ev)
	e.Event = "inbox.digest"
	e.Kind = "" // about no single thing — no kind to name
	e.Title = DigestTitle(ev.DigestCount)
	return e
}

// DigestTitle is the digest's whole message: a count and the word, in
// honest English singular or plural (SV-D6 — the compensation for "the
// daemon was down" is arithmetic, never a replay of stale messages).
func DigestTitle(count int) string {
	if count == 1 {
		return "1 item needs attention"
	}
	return fmt.Sprintf("%d items need attention", count)
}

// Marshal enforces the 1 KiB cap. An over-long TITLE is truncated at a
// rune boundary — never the whole event dropped: a short notification
// that arrived beats a perfect one that did not (the truncation is a
// display fact, the delivery fact is already true). A body over the cap
// with an empty title cannot exist (fixed keys bound it), so truncation
// always terminates.
func Marshal(e Envelope) ([]byte, error) {
	for {
		body, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		if len(body) <= MaxEnvelopeBytes {
			return body, nil
		}
		if e.Title == "" {
			return nil, fmt.Errorf("notify: envelope exceeds %d bytes with an empty title — impossible by construction", MaxEnvelopeBytes)
		}
		// Cut a rune at a time from the tail.
		_, w := utf8.DecodeLastRuneInString(e.Title)
		e.Title = e.Title[:len(e.Title)-w]
	}
}

// Sign is the outbound mirror of trigger.VerifyWebhookSignature:
// HMAC-SHA256 over the EXACT body bytes, hex, sent as
// `X-Aeon-Signature: sha256=<hex>`. The round trip (Sign → the inbound
// verifier accepts; one flipped byte → it refuses) is the test that
// keeps the mirror unbroken.
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignatureHeader is the header value's spelling the inbound verifier
// accepts as an alias.
func SignatureHeader(secret, body []byte) string { return "sha256=" + Sign(secret, body) }
