// Package protocol is Aeon's wire contract: the method table, its types, its
// error registry, subscriptions and events.
//
// # It knows nothing about transports (T11, D11, I1)
//
// No `net`, no listener, no `localrpc`. The same method table is mounted on the
// local socket today and, narrowed, on HTTP and MCP-stdio at S19 — and a
// package that imports a listener cannot be mounted twice. `import_test.go`
// asserts this, because it is the invariant whose breach is invisible:
// everything keeps working, and the cost arrives fifteen slices later.
package protocol

// PROTOCOL_VERSION is the version this daemon speaks (T2).
//
// # The rule, which matters more than the number
//
// It is one integer, not a semantic version, because a semantic version invites
// arguing about what a minor is. What makes an integer workable is a written
// rule about what counts as additive — so the rule lives here, beside the
// constant, where somebody about to change it will be standing.
//
// ADDITIVE — does NOT bump this number:
//   - a new method
//   - a new OPTIONAL field, in params or in a result
//   - a new event kind
//   - a new capability
//   - a new error code
//
// BREAKING — bumps it:
//   - removing or renaming a method or a field
//   - making an optional field required
//   - changing a field's type or its meaning
//   - changing an error code's meaning
//   - changing `seq` semantics
//
// A slice that bumps this must say why in its design, against that rule.
//
// # What makes the additive column safe
//
// T3: clients ignore unknown fields. A client that rejects one turns every
// additive change into a breaking one, which is why it is tested in both
// directions and why `conformance/forward-compat.json` exists.
//
// S38 T2 bumped it 1→2, and the bump is the BREAKING column of the rule
// above, twice over, and cannot be additive: OL1 RENAMED methods
// (`project.create/update/list` became `component.*`) and renamed the scope
// field in every params and result that carries it (`projectRoot` →
// `componentRoot`). A version-1 client speaking the old names gets
// method-not-found, and a version-1 client that reads `projectRoot` gets an
// empty scope — a plausible wrong answer, which is exactly what the version
// gate exists to refuse. MinSupportedVersion moves with it: nothing shipped
// version 1 (brief §0.2), so there is nobody to keep speaking to.
const PROTOCOL_VERSION = 2

// MinSupportedVersion is the oldest version this daemon will still speak.
//
// Equal to PROTOCOL_VERSION today because there is only one version. It is a
// separate constant rather than a literal `1` so that the first bump has one
// place to record what is still supported, instead of a comparison somebody has
// to find.
const MinSupportedVersion = 2

// Capabilities are optional feature names, as strings (O5).
//
// Strings rather than a bitfield or a version implication, so a feature can be
// added without a version change (T2). An unknown name is ignored, never an
// error — otherwise `capabilities` becomes a version by another name, which is
// the thing T2 exists to avoid.
func Capabilities() []string { return []string{"subscribe"} }

// Negotiate decides which version the daemon will speak to a client that needs
// `want`.
//
// Three outcomes, and the middle one is the whole point of negotiation:
//
//   - want > PROTOCOL_VERSION  → refused, naming BOTH numbers. A daemon that
//     silently downgrades a client asking for v5 fails later, somewhere else,
//     with an error about something unrelated.
//   - MinSupportedVersion <= want < PROTOCOL_VERSION → the daemon speaks THAT
//     version. This is the case that keeps an old editor extension working
//     across a daemon upgrade, and a daemon that only knows how to refuse has
//     version CHECKING rather than version negotiation.
//   - want < MinSupportedVersion → refused, naming both.
//
// A zero `want` is treated as "the oldest I support", not as 0: a client that
// omits the field is an old or sloppy client, and refusing it with a message
// about version 0 explains nothing.
func Negotiate(want int) (speak int, err error) {
	return negotiate(want, MinSupportedVersion, PROTOCOL_VERSION)
}

// negotiate is the rule, with its bounds as parameters.
//
// Separate from Negotiate for one reason, and it is a testing reason worth
// stating: today MinSupportedVersion EQUALS PROTOCOL_VERSION, so against the
// real constants the "serve an older version" branch cannot be reached — and
// that branch is the whole point of negotiation (T2). Testing it through the
// constants would mean a skipped test, and a skipped test is not a passing one.
// With the bounds injected, the downward case is exercised for real at
// min=1,max=2 today, years before the first bump makes it live.
func negotiate(want, min, max int) (speak int, err error) {
	if want == 0 {
		return min, nil
	}
	if want > max {
		return 0, ErrProtocolVersionUnsupported.WithWhat(
			"client needs protocol version %d; this daemon speaks %d (oldest supported: %d)",
			want, max, min)
	}
	if want < min {
		return 0, ErrProtocolVersionUnsupported.WithWhat(
			"client needs protocol version %d, which this daemon no longer speaks; it speaks %d (oldest supported: %d)",
			want, max, min)
	}
	return want, nil
}
