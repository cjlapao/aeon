// Command gencerts writes the S21 testdata certificates — the fake peer's
// whole PKI — so no fixture in this package is one an expiry notice buries.
// Run it whenever a certificate drifts near its NotAfter:
//
//	cd testdata/certs && go run gencerts.go   (from aeon/internal/remote/)
//
// It writes, into its own directory:
//
//	ca.pem / ca.key                the test CA the client's RootCAs stand in for
//	peer.pem / peer.key            the fake peer, DNS peer.example, ~10 years
//	peer-rotated.pem / .key        the SAME CA re-issuing with a NEW key (the
//	                               rotation story made bytes: the SPKI pin
//	                               fires against peer's pin, and the refusal
//	                               is correct until the operator re-reads)
//	PINS.md                        the sha-256 values of every pin-able
//	                               artefact, in the exact lowercase-hex form
//	                               a config's pin field carries
//
// The unit tests do NOT read these files — they mint their own certificates
// in memory (only that way can a test mint an expired one to order). These
// files exist for the DEMO walk: a fake peer that must actually TERMINATE a
// TLS handshake from disk needs bytes on disk, and those bytes must never be
// a fixture that expires under someone mid-walk.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func writePEM(dir, name, blockType string, der []byte) {
	must(os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600))
}

func writeKey(dir, name string, key *ecdsa.PrivateKey) {
	der, err := x509.MarshalECPrivateKey(key)
	must(err)
	writePEM(dir, name, "EC PRIVATE KEY", der)
}

func mint(tmpl *x509.Certificate, parent *x509.Certificate, key, parentKey *ecdsa.PrivateKey) *x509.Certificate {
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	must(err)
	cert, err := x509.ParseCertificate(der)
	must(err)
	return cert
}

func main() {
	dir := "."
	now := time.Now().UTC()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(20261028),
		Subject:               pkix.Name{CommonName: "aeon-s21-test-CA", Organization: []string{"aeon testdata"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	ca := mint(caTmpl, caTmpl, caKey, caKey)

	leafTmpl := func(sn int64) *x509.Certificate {
		return &x509.Certificate{
			SerialNumber: big.NewInt(sn),
			Subject:      pkix.Name{CommonName: "peer.example", Organization: []string{"fake peer — never a real remote"}},
			NotBefore:    now.Add(-time.Hour),
			NotAfter:     now.Add(10 * 365 * 24 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			DNSNames:     []string{"peer.example"},
			// the DEMO dials the fake peer on loopback; the SAN admits the
			// address the walk reaches, the CN keeps the identity story.
			IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		}
	}
	peerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	peer := mint(leafTmpl(20261029), ca, peerKey, caKey)

	rotKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	rotated := mint(leafTmpl(20261030), ca, rotKey, caKey) // same CA, NEW key: the rotation made bytes

	writePEM(dir, "ca.pem", "CERTIFICATE", ca.Raw)
	writeKey(dir, "ca.key", caKey)
	writePEM(dir, "peer.pem", "CERTIFICATE", peer.Raw)
	writeKey(dir, "peer.key", peerKey)
	writePEM(dir, "peer-rotated.pem", "CERTIFICATE", rotated.Raw)
	writeKey(dir, "peer-rotated.key", rotKey)

	hexOf := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	pins := fmt.Sprintf(`# S21 testdata pins — regenerate with gencerts.go, never hand-edit.

These are the sha-256 values a peer configuration's pin field carries
(PINS.md exists so the DEMO walk quotes a file, not a calculation).

| artefact                | kind        | sha-256                                    |
| ----------------------- | ----------- | ------------------------------------------ |
| peer.pem                | certificate | %s |
| peer.pem                | spki        | %s |
| peer-rotated.pem        | certificate | %s |
| peer-rotated.pem        | spki        | %s |

**The rotation story, on disk:** pin peer's spki, swap the server to
peer-rotated.pem/.key, and the refusal arrives saying "pin no longer
matches" with BOTH values printed — correct until the operator re-reads the
pin from this table after confirming out-of-band. Pinning the certificate
kind instead would fire on renewal too, which is the story the design
refuses to live with (trust.go Q1).
`, hexOf(peer.Raw), hexOf(peer.RawSubjectPublicKeyInfo),
		hexOf(rotated.Raw), hexOf(rotated.RawSubjectPublicKeyInfo))
	must(os.WriteFile(filepath.Join(dir, "PINS.md"), []byte(pins), 0o644))
	fmt.Println("wrote ca.pem ca.key peer.pem peer.key peer-rotated.pem peer-rotated.key PINS.md")
}
