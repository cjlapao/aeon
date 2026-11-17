// Package agentcard is S43's card envelope — the AM3 upgrade of a card from a
// bare ed25519 public key to Ed25519 over an X.509 certificate, stdlib only.
//
// Why this is its own package and not a member of internal/agentsvc.
//
// internal/agentsvc is S14's agent-services layer, guarded as OPTIONAL and
// deletable (AS11): a runtime-side package may not import it, or "optional"
// quietly stops being true (its absent_test proves it). S43's agent-management
// domain (internal/agentmgr) imports its own card types — SignedAgentCard,
// CardSignature, the CardState three-state, VerifyCardSignature — and if those
// lived in agentsvc then agentmgr would import agentsvc, and deleting the
// agentsvc layer would break the daemon. So the card format lives HERE, in a
// stdlib-only package neither agentsvc nor the runtime is forbidden from
// importing. The real verifier (VerifyCardSignature) is handed to agentmgr
// through its func seam; the daemon/protocol layers hold only the func.
//
// # What a verified card proves — and what it does not
//
// Verification proves exactly ONE thing (AM3): the card was not altered after
// it was signed — the Ed25519 signature matches the certificate's public key
// over the card's own bytes. It does NOT judge whether the signer is who they
// claim, and does NOT refuse an unfamiliar signer: this slice holds no
// trust-root and no signer registry (the marketplace is another website). The
// caller shows the verification result (verified / tampered / unsigned) and
// the signer's claimed identity from the certificate; a human decides whether
// that is good enough. A future slice can add a trust root — out of scope here.
//
// # Three states, never two (P31)
//
// A card is `verified` (signature matched), `tampered` (signature present but
// did not match), or `unsigned` (no signature at all). Unsigned is a refusal
// BY DEFAULT, behind the allowUnsignedImport override that imports it marked
// `unsigned`. A tampered card is refused, named, never silently accepted.

package agentcard

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
)

// SignedAgentCard is S43's card envelope: the card's exact bytes, the
// Ed25519 signature over them, and the signer's X.509 certificate (PEM). The
// signature rides beside the card (detached); the certificate in place of a
// bare public key is the AM3 upgrade.
type SignedAgentCard struct {
	Card        json.RawMessage `json:"card"`
	Signature   string          `json:"signature,omitempty"`   // base64 std, ed25519 over Card's exact bytes
	Certificate string          `json:"certificate,omitempty"` // PEM X.509; empty when unsigned
}

// CardSignature is the verification answer — the three states (AM3/P31), plus
// the signer's claimed identity (from the certificate) for the UI to show.
type CardSignature struct {
	// State is one of the CardState values.
	State CardState
	// Signer is the certificate's Subject (the claimed identity), for the UI
	// to show on a verified card. Empty when unsigned or tampered.
	Signer string
	// Detail names the refusal, verbatim, for the refusal the caller renders.
	Detail string
}

// CardState is the three-state honesty the card verification must give (P31).
type CardState string

const (
	// CardStateVerified is the signature matched the certificate's key.
	CardStateVerified CardState = "verified"
	// CardStateTampered is a signature that did not verify — the card was
	// altered after signing, or is not from the key it claims.
	CardStateTampered CardState = "tampered"
	// CardStateUnsigned is no signature at all — a refusal by default.
	CardStateUnsigned CardState = "unsigned"
)

// VerifyCardSignature checks the Ed25519 signature against the certificate's
// public key over the card's exact bytes (AM3 — tamper-evidence only). It
// returns the signer's claimed identity on success. A tampered card is a
// refusal that names it; an unsigned card is CardStateUnsigned so the caller
// can apply the allowUnsignedImport override.
func VerifyCardSignature(card, certPEM []byte, sig []byte) (*CardSignature, error) {
	if len(certPEM) == 0 || len(sig) == 0 {
		return &CardSignature{State: CardStateUnsigned}, nil
	}
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return &CardSignature{State: CardStateTampered, Detail: "refused: the certificate is not a valid X.509 certificate"}, err
	}
	pub, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return &CardSignature{State: CardStateTampered, Detail: "refused: the certificate carries no ed25519 public key"}, nil
	}
	sigBytes, err := base64.StdEncoding.DecodeString(string(sig))
	if err != nil {
		return &CardSignature{State: CardStateTampered, Detail: "refused: the signature is not base64"}, nil
	}
	if !ed25519.Verify(pub, card, sigBytes) {
		return &CardSignature{State: CardStateTampered, Detail: "refused: the signature does not match the certificate over the card — the card was tampered with or is not from the key it claims"}, nil
	}
	return &CardSignature{State: CardStateVerified, Signer: CertificateSubject(cert)}, nil
}

// ParseCertificate reads a PEM X.509 certificate (stdlib only, AM3).
func ParseCertificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("refused: no PEM block in the certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

// CertificateSubject renders the signer's claimed identity from the
// certificate's Subject — the one fact the UI shows, that a human decides on
// (AM3: the app does not judge the signer).
func CertificateSubject(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	if s := cert.Subject.CommonName; s != "" {
		return s
	}
	return cert.Subject.String()
}

// SignAgentCardCard is the authoring helper (the operator path and the
// fixtures) — nothing at verification time calls it. It signs the card's
// exact (compact) bytes with the signer's key, storing the signer's X.509
// certificate PEM beside the signature, so a re-marshalled card is a
// different card and a tamper after signing fails. The caller supplies the
// certificate's PEM so no cert→DER conversion is needed at signing (the
// verify path is the only one that reads the PEM back).
func SignAgentCardCard(card []byte, certPEM string, key ed25519.PrivateKey) (*SignedAgentCard, error) {
	if len(card) == 0 {
		return nil, fmt.Errorf("refused: cannot sign an empty card")
	}
	if certPEM == "" {
		return nil, fmt.Errorf("refused: cannot sign without the signer's certificate")
	}
	var probe map[string]any
	if err := json.Unmarshal(card, &probe); err != nil {
		return nil, fmt.Errorf("refused: cannot sign a card that is not valid JSON: %w", err)
	}
	signed, err := compactJSON(card)
	if err != nil {
		return nil, fmt.Errorf("refused: cannot canonicalise the card: %w", err)
	}
	return &SignedAgentCard{
		Card:        json.RawMessage(append([]byte(nil), signed...)),
		Signature:   base64.StdEncoding.EncodeToString(ed25519.Sign(key, signed)),
		Certificate: certPEM,
	}, nil
}

// compactJSON is the signature's over-the-exact-bytes half: the card's bytes
// with no whitespace and stable key order, so a re-marshalled card is a
// different card and a post-signing tamper fails.
func compactJSON(card []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, card); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
