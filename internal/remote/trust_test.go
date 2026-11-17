package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- certificate minting, in memory, to order -----------------------------
// The tests never read testdata/certs/*.pem: only in-memory minting can
// produce an EXPIRED certificate on demand, and it removes the expiry
// cliff entirely. The committed files exist for the demo walk (T9), and one
// test below sanity-checks them.

func newTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	key := newTestKey(t)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "aeon-s21-test-CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return testCA{cert: cert, key: key}
}

// mintLeaf signs a server certificate for dns with notBefore..notAfter using
// key (pass a FRESH key to model rotation; pass the SAME key to model
// renewal — the two are deliberately distinguishable here because that is
// exactly the distinction the pin kinds encode).
func (ca testCA) mintLeaf(t *testing.T, dns string, key *ecdsa.PrivateKey, notBefore, notAfter time.Time) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: dns},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{dns},
		// loopback handshakes (every test dials 127.0.0.1): the SAN admits
		// the address the test actually reaches; ServerName still carries
		// the identity story, and the wrong-hostname test is unaffected.
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func spkiHex(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	s := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(s[:])
}

func certHex(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	s := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(s[:])
}

// serveTLS runs the fake peer: one TLS server on loopback terminating with
// the given leaf, until the test ends.
func serveTLS(t *testing.T, cert *x509.Certificate, key *ecdsa.PrivateKey) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	tlsCert := tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12})
				_ = sc.Handshake() // the client's verdict is the test's subject
				sc.Close()
			}()
		}
	}()
	return ln.Addr().String()
}

// dialPeer is what T3's dial will do with this hook installed: NORMAL
// verification against roots, then the pin on top — never instead.
func dialPeer(t *testing.T, addr, serverName string, roots *x509.CertPool, hook func([][]byte, [][]*x509.Certificate) error) error {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		RootCAs:               roots,
		ServerName:            serverName,
		VerifyPeerCertificate: hook,
		MinVersion:            tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	return conn.Close()
}

func rootsOf(ca testCA) *x509.CertPool {
	p := x509.NewCertPool()
	if ca.cert != nil {
		p.AddCert(ca.cert)
	}
	return p
}

// --- the refusals -----------------------------------------------------------

// TestUnpinnedCertificateRefused is D2: the certificate is VALID (real test
// CA in the pool), the hostname is right, the chain verifies — and the pin
// does not name it, so the handshake is refused.
func TestUnpinnedCertificateRefused(t *testing.T) {
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr := serveTLS(t, leaf, key)

	// A PIN FOR SOMEONE ELSE — the presented cert is "unpinned": nothing on
	// file names it. (T1 already refuses a peer with no pin at config time;
	// this is the runtime face of the same door.)
	other := newTestKey(t)
	otherLeaf := ca.mintLeaf(t, "peer.example", other, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	hook := VerifierFor("peer-alpha", Pin{SHA256: spkiHex(t, otherLeaf), Kind: PinSPKI})

	err := dialPeer(t, addr, "peer.example", rootsOf(ca), hook)
	if err == nil {
		t.Fatal("handshake accepted an unpinned certificate that the CA set happily verified — RA2 says the CA set is not identity")
	}
	if !strings.Contains(err.Error(), "pin no longer matches") {
		t.Errorf("refusal must be the pin's refusal, not a TLS accident: %v", err)
	}
}

// TestTheRefusalPrintsBothThePinnedAndTheOfferedValue keeps the incident
// diff-able: both hex strings reach the sentence.
func TestTheRefusalPrintsBothThePinnedAndTheOfferedValue(t *testing.T) {
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr := serveTLS(t, leaf, key)
	pinned := strings.Repeat("0a", 32) // 64 hex chars, matches nothing
	err := dialPeer(t, addr, "peer.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: pinned, Kind: PinSPKI}))
	if err == nil {
		t.Fatal("want refusal")
	}
	offered := spkiHex(t, leaf)
	if !strings.Contains(err.Error(), offered) {
		t.Errorf("refusal must print the OFFERED value (%s): %v", offered, err)
	}
	if !strings.Contains(err.Error(), pinned) {
		t.Errorf("refusal must print the PINNED value: %v", err)
	}
}

// TestRotatedKeyRefusedSayingPinMismatch is D3 — the rotation story's hard
// half: same CA, right hostname, NEW key. The refusal must SAY the pin no
// longer matches (renewal with the same key is the story's soft half — the
// spki pin rides through it, asserted right after).
func TestRotatedKeyRefusedSayingPinMismatch(t *testing.T) {
	ca := newTestCA(t)
	oldKey := newTestKey(t)
	oldLeaf := ca.mintLeaf(t, "peer.example", oldKey, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	// The peer rotates: new key, re-issued by the same CA. The config still
	// carries the old SPKI.
	rotKey := newTestKey(t)
	rotLeaf := ca.mintLeaf(t, "peer.example", rotKey, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr := serveTLS(t, rotLeaf, rotKey)

	err := dialPeer(t, addr, "peer.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: spkiHex(t, oldLeaf), Kind: PinSPKI}))
	if err == nil {
		t.Fatal("a rotated key sailed through — the SPKI pin is the thing this test exists to make fire")
	}
	if !strings.Contains(err.Error(), "pin no longer matches") {
		t.Errorf("the refusal must say the pin no longer matches, in those words: %v", err)
	}
	// And the operator's fix is VISIBLE: both values, so the re-read is a
	// diff against PINS.md and not a forensic exercise.
	if !strings.Contains(err.Error(), spkiHex(t, rotLeaf)) {
		t.Errorf("the refusal must print the new key's SPKI so re-pinning is mechanical: %v", err)
	}

	// The renewal half (same key, new certificate): the SPKI pin rides
	// through, which is exactly why Q1's answer is SPKI.
	renewedLeaf := ca.mintLeaf(t, "peer.example", rotKey, time.Now().Add(-time.Hour), time.Now().Add(72*time.Hour))
	addr2 := serveTLS(t, renewedLeaf, rotKey)
	if err := dialPeer(t, addr2, "peer.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: spkiHex(t, rotLeaf), Kind: PinSPKI})); err != nil {
		t.Errorf("an SPKI pin must survive renewal (same key, fresh certificate): %v", err)
	}
}

// TestSystemCASetAloneInsufficient demonstrates the gap in both directions
// with the SAME bytes: without the pin hook, the CA set accepts the peer
// (that is what a CA set IS); with the pin hook, the identical handshake is
// refused. Identity came from nowhere but the pin.
func TestSystemCASetAloneInsufficient(t *testing.T) {
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr := serveTLS(t, leaf, key)

	if err := dialPeer(t, addr, "peer.example", rootsOf(ca), nil); err != nil {
		t.Fatalf("baseline: the CA-verified handshake must succeed without the pin hook: %v", err)
	}
	err := dialPeer(t, addr, "peer.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: strings.Repeat("0b", 32), Kind: PinSPKI}))
	if err == nil {
		t.Fatal("the pin hook did not fire where the CA set said yes — the two answers must be able to disagree")
	}
}

// TestExpiredPinnedCertificateStillRefused is scope 5 made executable: the
// pin MATCHES and the certificate is EXPIRED — a pin is a narrowing, not a
// replacement, so normal verification still refuses. (The same argument
// covers hostname: asserted beside it.)
func TestExpiredPinnedCertificateStillRefused(t *testing.T) {
	ca := newTestCA(t)
	key := newTestKey(t)
	expired := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	addr := serveTLS(t, expired, key)

	err := dialPeer(t, addr, "peer.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: spkiHex(t, expired), Kind: PinSPKI}))
	if err == nil {
		t.Fatal("an expired PINNED certificate was accepted — that is a pin replacing verification, which scope 5 forbids")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("the refusal must be the expiry's, proving normal verification ran: %v", err)
	}

	// Wrong hostname, matching pin: also still refused by normal verification.
	good := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr2 := serveTLS(t, good, key)
	err = dialPeer(t, addr2, "evil.example", rootsOf(ca), VerifierFor("peer-alpha", Pin{SHA256: spkiHex(t, good), Kind: PinSPKI}))
	if err == nil || !strings.Contains(err.Error(), "evil.example") {
		t.Errorf("hostname must still be verified with a matching pin: %v", err)
	}
}

// TestInsecureSkipVerifyIsNeverSet is scope 4 over the WHOLE package, tests
// included — a test helper that sets it is the bypass the plan's ordering
// warns about. The banned token is assembled so this file does not contain
// what it hunts.
func TestInsecureSkipVerifyIsNeverSet(t *testing.T) {
	banned := []string{
		"InsecureSkip" + "Verify:",  // struct-literal field with any value
		"InsecureSkip" + "Verify =", // any assignment
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		seen++
		for _, b := range banned {
			if strings.Contains(string(raw), b) {
				t.Errorf("%s sets %s — normal verification is never skipped; the pin is ADDITIONAL (scope 4/5)", e.Name(), b)
			}
		}
	}
	if seen < 3 {
		t.Errorf("the scan read %d files — peer.go, trust.go and their tests must all be in view", seen)
	}
}

// --- the arithmetic ----------------------------------------------------------

func TestOfferedComputesBothKinds(t *testing.T) {
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	// Independent computation, so Offered cannot grade its own homework.
	spki, err := Pin{Kind: PinSPKI}.Offered(leaf)
	if err != nil || spki != hexOf(leaf.RawSubjectPublicKeyInfo) {
		t.Errorf("spki pin computation wrong: %q %v", spki, err)
	}
	cp, err := Pin{Kind: PinCertificate}.Offered(leaf)
	if err != nil || cp != hexOf(leaf.Raw) {
		t.Errorf("certificate pin computation wrong: %q %v", cp, err)
	}
	if _, err := (Pin{Kind: "fingerprint"}).Offered(leaf); err == nil {
		t.Error("an unclosed pin kind must be refused even past the config door")
	}
}

// TestVerifierRefusesGarbagePresentedBytes: the hook must refuse bytes that
// do not parse rather than pin to something nobody understood.
func TestVerifierRefusesGarbagePresentedBytes(t *testing.T) {
	hook := VerifierFor("peer-alpha", Pin{SHA256: strings.Repeat("0c", 32), Kind: PinSPKI})
	if err := hook(nil, nil); err == nil || !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("empty presentation must be refused by name: %v", err)
	}
	if err := hook([][]byte{{0xde, 0xad}}, nil); err == nil || !strings.Contains(err.Error(), "does not parse") {
		t.Errorf("garbage bytes must be refused by name: %v", err)
	}
}

// TestCommittedTestdataCertsAreScriptGenerated is the suite's link to the
// committed files gencerts.go writes: they load, they carry the pins PINS.md
// states, and none expires inside a year — so the suite never depends on a
// certificate expiring, and the demo's bytes are one command from fresh.
func TestCommittedTestdataCertsAreScriptGenerated(t *testing.T) {
	load := func(name string) *x509.Certificate {
		raw, err := os.ReadFile(filepath.Join("testdata", "certs", name))
		if err != nil {
			t.Fatalf("%s missing — regenerate: cd testdata/certs && go run gencerts.go: %v", name, err)
		}
		blk, _ := pem.Decode(raw)
		cert, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return cert
	}
	peer := load("peer.pem")
	rotated := load("peer-rotated.pem")
	if certHex(t, peer) == certHex(t, rotated) {
		t.Error("the rotation artefacts are the same certificate — the rotation story has no bytes")
	}
	pinsRaw, err := os.ReadFile(filepath.Join("testdata", "certs", "PINS.md"))
	if err != nil {
		t.Fatal(err)
	}
	pins := string(pinsRaw)
	for _, want := range []string{certHex(t, peer), spkiHex(t, peer), certHex(t, rotated), spkiHex(t, rotated)} {
		if !strings.Contains(pins, want) {
			t.Errorf("PINS.md lost pin %s — regenerate", want)
		}
	}
	if time.Until(peer.NotAfter) < 365*24*time.Hour {
		t.Error("peer.pem expires inside a year — regenerate with gencerts.go before the suite inherits a cliff")
	}
}

func hexOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
