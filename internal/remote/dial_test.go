package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// lowerBands drops the two seams so "bounded" is testable in milliseconds.
// Restored by cleanup — the seams are package state precisely so the tests
// can borrow them, and nothing but the tests ever reads them.
func lowerBands(t *testing.T, call time.Duration) {
	t.Helper()
	oldCall, oldDial := callTimeout, dialTimeout
	callTimeout, dialTimeout = call, 2*time.Second
	t.Cleanup(func() { callTimeout, dialTimeout = oldCall, oldDial })
}

// countingDial wraps the raw seam and counts attempts — the number that
// settles "a failed call is not retried" as measurement, not prose.
func countingDial(t *testing.T) *atomic.Int32 {
	t.Helper()
	tries := &atomic.Int32{}
	old := rawDial
	rawDial = func(ctx context.Context, addr string) (net.Conn, error) {
		tries.Add(1)
		return old(ctx, addr)
	}
	t.Cleanup(func() { rawDial = old })
	return tries
}

// servingPeer is the fake peer with a settable app-level behaviour under a
// real TLS shell. conns counts accepted connections; closed counts the ones
// that reached EOF or error on the server side — a client that HELD a
// connection would keep the second number short.
func servingPeer(t *testing.T, cert *x509.Certificate, key *ecdsa.PrivateKey, behavior func(c *tls.Conn)) (addr string, conns, closed *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	conns, closed = &atomic.Int32{}, &atomic.Int32{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func() {
				sc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
				if err := sc.Handshake(); err != nil {
					sc.Close()
					return
				}
				behavior(sc)
				sc.Close()
				closed.Add(1)
			}()
		}
	}()
	return ln.Addr().String(), conns, closed
}

func dialerFor(ca testCA) *Dialer {
	return &Dialer{Roots: func() (*x509.CertPool, error) { return rootsOf(ca), nil }}
}

func peerAt(addr string, cert *x509.Certificate) Peer {
	return Peer{ID: "peer-alpha", Address: addr, Pin: &Pin{SHA256: spkiHexOf(cert), Kind: PinSPKI}, Key: &KeyRef{Env: "AEON_TEST_PEER_KEY"}}
}

// spkiHexOf is the test-side pin computation, independent of Offered (the
// test must be able to say Offered is wrong).
func spkiHexOf(cert *x509.Certificate) string {
	return hexOf(cert.RawSubjectPublicKeyInfo)
}

func TestNoPlaintextModeExists(t *testing.T) {
	raw, err := os.ReadFile("dial.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	banned := []string{
		"ws://", "http://", "net.Dial(", // bare dialer: the plaintext door
		"localhost", // no exception for localhost — the socket already covers local (P26)
		"net/http",  // no HTTP fallback hiding a plaintext scheme
		"Listen",    // this package dials; it never serves
	}
	for _, b := range banned {
		if strings.Contains(src, b) {
			t.Errorf("dial.go contains %q — there is no plaintext mode: not a flag, not a scheme, not a localhost exception (RA1)", b)
		}
	}
}

// TestExactlyOneDialPathAndItVerifies is the plan's ordering risk made
// unfashionable: grep says one tls.Config, one tls.Client, one hook mount;
// behaviour says a pin-less peer dials NOTHING — the seam counter stays at
// zero, so "no second path" is not just shape, it is observed.
func TestExactlyOneDialPathAndItVerifies(t *testing.T) {
	raw, err := os.ReadFile("dial.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, probe := range []struct {
		token string
		want  int
	}{
		{"tls.Client(", 1},
		{"tls.Config{", 1},
		{"VerifyPeerCertificate:", 1},
		{"func dialVerified(", 1},
		{"tls.Dial(", 0},
	} {
		if got := strings.Count(src, probe.token); got != probe.want {
			t.Errorf("dial.go has %d occurrences of %q, want %d — exactly one dial path and it verifies (scope 2)", got, probe.token, probe.want)
		}
	}

	// Behavioural half: a pin-less peer (impossible by Validate, possible by
	// struct literal) is refused BEFORE the wire is touched.
	tries := countingDial(t)
	d := &Dialer{}
	out := d.Call(context.Background(), Peer{ID: "ghost", Address: "127.0.0.1:1"}, []byte("hi"))
	if out.Kind != OutcomeError || out.Err == nil || !strings.Contains(out.Err.Error(), "carries no pin") {
		t.Errorf("pin-less peer was not refused by name: %+v", out)
	}
	if tries.Load() != 0 {
		t.Errorf("the wire was touched %d times for an unverified peer — the unverified path must not EXIST", tries.Load())
	}
}

func TestHungPeerDegradesTheStep(t *testing.T) {
	lowerBands(t, 200*time.Millisecond)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	// The truly silent server: TLS completes and then the handler blocks
	// FOREVER — application bytes never arrive and the connection is never
	// closed. (serveTLS would be wrong here: its polite Close sends
	// close_notify, which is an answer of zero bytes, not an outage.)
	addr, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) { select {} })
	d := dialerFor(ca)
	peer := peerAt(addr, leaf)

	started := time.Now()
	out := d.Call(context.Background(), peer, []byte("request"))
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the CALL held the caller for %v — a hung peer must never hang a step (RA6)", elapsed)
	}
	if out.Kind != OutcomeDeadline {
		t.Fatalf("hung peer gave %q, want deadline: %s", out.Kind, out.Render())
	}
	// The step continues: the caller holds a value and can proceed. A second
	// call proves the dialer itself was not wedged by the first.
	out2 := d.Call(context.Background(), peer, []byte("request"))
	if out2.Kind != OutcomeDeadline {
		t.Errorf("second call after a hang gave %q — the dialer wedged", out2.Kind)
	}
}

// TestDeadlineIsNotAnEmptyResult is D8's first half — the rendering itself
// carries the distinction, so no renderer downstream can merge them.
func TestDeadlineIsNotAnEmptyResult(t *testing.T) {
	deadline := Outcome{Peer: "p", Kind: OutcomeDeadline, Bound: 30 * time.Second}.Render()
	empty := Outcome{Peer: "p", Kind: OutcomeResult, Payload: nil}.Render()
	if deadline == empty {
		t.Fatal("a deadline renders identically to an empty result")
	}
	if !strings.Contains(deadline, "did not answer") || !strings.Contains(deadline, "different fact from the peer reporting nothing") {
		t.Errorf("deadline rendering must SAY the peer may have had an answer: %q", deadline)
	}
	if !strings.Contains(empty, "EMPTY result") || !strings.Contains(empty, "peer reported nothing") {
		t.Errorf("empty-result rendering must own the emptiness as the peer's answer: %q", empty)
	}
	if strings.Contains(deadline, "EMPTY result") || strings.Contains(empty, "did not answer") {
		t.Error("the two renderings must not borrow each other's words")
	}
}

// TestDeadlineIsDistinguishableFromAnError is D8's second half: a broken
// connection is not a bound firing — different kind, different sentence.
func TestDeadlineIsDistinguishableFromAnError(t *testing.T) {
	// Closed listener: nothing accepts, so this is a refusal, not a hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	lowerBands(t, 2*time.Second)
	out := (&Dialer{}).Call(context.Background(), peerAt(addr, &x509.Certificate{RawSubjectPublicKeyInfo: []byte("x")}), []byte("x"))
	if out.Kind != OutcomeError {
		t.Fatalf("refused connection gave %q (%s), want error", out.Kind, out.Render())
	}
	errRender := out.Render()
	deadlineRender := Outcome{Peer: "p", Kind: OutcomeDeadline, Bound: 2 * time.Second}.Render()
	if strings.Contains(errRender, "did not answer within") {
		t.Errorf("an error must not read as a deadline: %q", errRender)
	}
	if strings.Contains(deadlineRender, "failed —") {
		t.Errorf("a deadline must not read as a bare failure: %q", deadlineRender)
	}
}

// TestFailedCallNotRetried is the N5 count: one refusal, one attempt — a
// retry doubles the wait and the model cannot tell one outage from two.
func TestFailedCallNotRetried(t *testing.T) {
	lowerBands(t, 150*time.Millisecond)
	tries := countingDial(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	out := (&Dialer{}).Call(context.Background(), peerAt(addr, &x509.Certificate{RawSubjectPublicKeyInfo: []byte("x")}), []byte("x"))
	if out.Kind == OutcomeResult {
		t.Fatal("a refused port returned a result — what did it answer with?")
	}
	if got := tries.Load(); got != 1 {
		t.Errorf("refused call made %d raw dial attempts, want exactly 1 — no retry, ever (N5)", got)
	}

	// The hung peer gets one attempt too: the bound is the retry policy.
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	hung, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) { select {} })
	tries.Store(0)
	if out := dialerFor(ca).Call(context.Background(), peerAt(hung, leaf), []byte("x")); out.Kind != OutcomeDeadline {
		t.Fatalf("hung gave %q", out.Kind)
	}
	if got := tries.Load(); got != 1 {
		t.Errorf("hung call made %d attempts, want exactly 1", got)
	}
}

// TestDeadPeersSkillsAreAbsentAndReported is scope 6: the absence is a
// SPOKEN thing, naming the peer, the reason, and the consequence.
func TestDeadPeersSkillsAreAbsentAndReported(t *testing.T) {
	for _, o := range []Outcome{
		{Peer: "peer-beta", Kind: OutcomeDeadline, Bound: 30 * time.Second},
		{Peer: "peer-beta", Kind: OutcomeError, Err: io.ErrUnexpectedEOF},
	} {
		note := o.DegradeNote()
		for _, must := range []string{"peer-beta", "unavailable", "absent from this step's reach", "reported"} {
			if !strings.Contains(note, must) {
				t.Errorf("degradation note missing %q — a silent absence is a model wondering about its own prompt: %q", must, note)
			}
		}
	}
	if note := (Outcome{Peer: "peer-beta", Kind: OutcomeResult, Payload: []byte("ok")}).DegradeNote(); note != "" {
		t.Errorf("a live peer must degrade nothing, got %q", note)
	}
}

// TestCallerCanShortenNeverLengthen: bash's rule, measured.
func TestCallerCanShortenNeverLengthen(t *testing.T) {
	lowerBands(t, 300*time.Millisecond)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) { select {} }) // whatever bound applies, it is what fires
	d := dialerFor(ca)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	out := d.Call(ctx, peerAt(addr, leaf), []byte("x"))
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("caller's 150ms bound did not win: held for %v", elapsed)
	}
	if out.Kind != OutcomeDeadline || out.Bound > 150*time.Millisecond+50*time.Millisecond {
		t.Errorf("the BOUND spoken must be the caller's, got %v (%s)", out.Bound, out.Render())
	}
	// And the caller cannot raise the ceiling: a generous ctx still gets the
	// band's number, not its own — a tool that could raise its own ceiling
	// has no ceiling.
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Hour)
	defer cancel2()
	out2 := d.Call(ctx2, peerAt(addr, leaf), []byte("x"))
	if out2.Kind != OutcomeDeadline || out2.Bound > 300*time.Millisecond {
		t.Errorf("a caller raised the ceiling past the band: %v", out2.Bound)
	}
}

// TestTaskExpiryIsNotAConnectionTimeout is Q3's async half, as numbers.
func TestTaskExpiryIsNotAConnectionTimeout(t *testing.T) {
	if DialTimeout != 5*time.Second || DefaultCallTimeout != 30*time.Second || DefaultTaskExpiry != time.Hour {
		t.Errorf("Q3's numbers moved: connect %v, call %v, task %v", DialTimeout, DefaultCallTimeout, DefaultTaskExpiry)
	}
	fixed := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	d := &Dialer{Now: func() time.Time { return fixed.Add(59 * time.Minute) }}
	if d.TaskExpired(fixed) {
		t.Error("a 59-minute-old async task is not expired — expiry is on the task's schedule, not a socket's")
	}
	d.Now = func() time.Time { return fixed.Add(61 * time.Minute) }
	if !d.TaskExpired(fixed) {
		t.Error("a 61-minute-old async task must read expired — 'the peer never came back' becomes a fact on a schedule")
	}
}

// TestOneConnectionPerCall is RA7 on the wire: two calls, two connections,
// both closed by the client — there is nothing here to hold.
func TestOneConnectionPerCall(t *testing.T) {
	lowerBands(t, 2*time.Second)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr, conns, closed := servingPeer(t, leaf, key, func(c *tls.Conn) {
		_, _ = io.Copy(io.Discard, c) // read the request to EOF (client CloseWrites)
		_, _ = c.Write([]byte("pong"))
	})
	d := dialerFor(ca)
	peer := peerAt(addr, leaf)
	for i := 0; i < 2; i++ {
		out := d.Call(context.Background(), peer, []byte("ping"))
		if out.Kind != OutcomeResult || string(out.Payload) != "pong" {
			t.Fatalf("call %d: %s", i, out.Render())
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && closed.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if conns.Load() != 2 || closed.Load() != 2 {
		t.Errorf("connections accepted %d / closed %d, want 2/2 — one per call, none held (RA7)", conns.Load(), closed.Load())
	}
}

func TestGoodCallAndEmptyResult(t *testing.T) {
	lowerBands(t, 2*time.Second)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	d := dialerFor(ca)

	emptyAddr, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) {
		_, _ = io.Copy(io.Discard, c) // answered nothing, deliberately
	})
	out := d.Call(context.Background(), peerAt(emptyAddr, leaf), []byte("q"))
	if out.Kind != OutcomeResult || len(out.Payload) != 0 {
		t.Fatalf("silent-but-agreeing peer gave %s — an empty answer is the peer's ANSWER, not a failure", out.Render())
	}
	if !strings.Contains(out.Render(), "EMPTY result") {
		t.Errorf("empty answer must render as the peer reporting nothing: %q", out.Render())
	}
}
