package remote

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
)

// trust.go is RA2 as code: a valid certificate proves TLS to SOME host; it
// does not prove this is the peer you consented to. The system CA set
// answers "is this a real certificate for this hostname" — the gap between
// that and "is this the agent we pinned" is every mis-issuance, every
// compromised CA, and every hostname somebody else can obtain a certificate
// for. This file is S15's signed-card argument one layer up, and it is
// written BEFORE the dial exists (the plan's one ordering to get right: a
// trust store retro-fitted onto a working dial is a trust store with a
// bypass).
//
// Q1 — what is pinned (answered here, per the task):
//
//	SPKI. The pin hashes the leaf's SubjectPublicKeyInfo, not the whole
//	certificate. Certificates renew — routinely, automatically, every ninety
//	days in a lot of deployments — and a certificate pin breaks on the first
//	renewal, which looks exactly like an attack: somebody investigates,
//	finds nothing, and disables pinning to stop the noise. That is how
//	pinning dies in month four. An SPKI pin survives renewal because the key
//	is reused, so pinning that lasts is pinning that does not fire on the
//	boring path. ("certificate" remains a valid kind — a stricter choice for
//	peers whose operators promise never to re-key — but the SPKI kind is the
//	recommended one, and the rotation story below is the answer that makes
//	the pin permanent rather than temporary.)
//
// THE ROTATION STORY (what happens when a peer legitimately rotates):
//
//	Renewal (same key, new certificate): an SPKI pin rides through
//	unnoticed; a certificate pin fires once and the operator re-reads the
//	pin against the new certificate — the refusal sentence says to do
//	exactly that, with both values printed, so the fix is a diff and not an
//	investigation.
//	Key rotation (new key, same or new certificate): the pin fires and the
//	REFUSAL IS CORRECT — until the operator confirms out-of-band with the
//	peer and re-reads the pin. There is no automatic path: a pin that
//	re-pins itself on first encounter is TOFU, and TOFU's first encounter is
//	the attacker's best day. The refusal is a refusal, never a warning.
//
// Q2 — CA in the trust store, or leaf pins only (answered here):
//
//	LEAF PINS ONLY. A private CA in the store scales to many peers but
//	widens: the moment Aeon trusts a CA, every certificate that CA mints for
//	the pinned hostname is "the peer" — the CA's whole issuance policy
//	becomes Aeon's security policy. For a handful of known peers leaf pins
//	are right. What that costs at ten peers, stated plainly: every
//	coordinated key rotation is ten config edits, and there is no way to
//	amortize it that does not re-open the CA door — the mitigation is the
//	kind choice (SPKI + long-lived keys keeps the edit rate near zero), not
//	a store that trusts more.
//
// Verification is ADDITIONAL to normal TLS validation, never instead of it
// (scope 5): the hook below runs after chain, expiry and hostname have all
// passed, and an expired or wrong-hostname pinned certificate is still
// refused — a pin is a narrowing, not a replacement. That property lives in
// the transport (T3's tls.Config wiring: normal verification ON, plus this
// hook); this file provides the hook and the arithmetic, and the tests here
// prove both halves over real handshakes.

// PinKindNote is exported so the daemon's config-help can name the rotation
// story without duplicating it (the one-paragraph version).
const PinKindNote = "pin the SPKI sha-256, not the certificate: certificates renew every ninety days and a certificate pin breaks on the first renewal — the key is reused across renewals, so an SPKI pin survives them"

// VerifierFor returns the hook that checks the presented leaf against pin,
// naming the peer in every refusal. It is a pure function of the pin and the
// bytes presented: no store, no clock, no network — the same argument
// pattern as the policy evaluator. It never bypasses anything: whatever it
// does NOT check, normal TLS verification already did (see the file's
// doc-comment).
func VerifierFor(peerID string, pin Pin) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("pin check: peer %q presented no certificate to check against the pin — there is nothing to consent to", peerID)
		}
		// The leaf as PRESENTED, not the verified chain's leaf: the pin is
		// about what arrived on the wire.
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("pin check: peer %q presented a certificate that does not parse (%v) — refusing rather than pinning to bytes nobody understood", peerID, err)
		}
		offered, err := pin.Offered(leaf)
		if err != nil {
			return fmt.Errorf("pin check: peer %q: %w", peerID, err)
		}
		if offered != pin.SHA256 {
			// Both values print (scope 1): the difference must be visible,
			// not asserted — an operator comparing two hex strings in two
			// terminals is how pin incidents become 2 a.m.
			return fmt.Errorf("pin no longer matches: peer %q presents %s sha-256 %s, the pin on file says %s — this peer is not who this configuration consented to (RA2). If the peer legitimately rotated, confirm out-of-band and re-read the pin against the new material; if it did not, this refusal is the whole point of the pin", peerID, pin.Kind, offered, pin.SHA256)
		}
		return nil
	}
}

// Offered computes what pin's kind says about cert — the value the peer is
// actually offering, in the same lowercase-hex space as the pin. Exported
// because the rotation tooling (an operator re-reading a pin) computes the
// same value with the same function, so "the refusal said one thing and the
// tool another" cannot happen.
func (p Pin) Offered(cert *x509.Certificate) (string, error) {
	var raw []byte
	switch p.Kind {
	case PinSPKI:
		// RawSubjectPublicKeyInfo: the DER the hash must cover — hashing the
		// public key's Go struct would depend on Go's types, not on the wire.
		if len(cert.RawSubjectPublicKeyInfo) == 0 {
			return "", fmt.Errorf("certificate carries no SPKI bytes to hash")
		}
		raw = cert.RawSubjectPublicKeyInfo
	case PinCertificate:
		raw = cert.Raw
	default:
		// Validate closed the set at config time (peer.go); a store-bypassed
		// Pin reaching here is programming, not input, and still refuses.
		return "", fmt.Errorf("pin kind %q is not %q or %q", p.Kind, PinCertificate, PinSPKI)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
