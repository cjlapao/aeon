package endpoint

import (
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Every test binds a path under t.TempDir() (I6), the same way claim_test.go
// does. Probe is read-only, so each test additionally asserts the file the
// probe found — or did not find — is still there: the whole point of Probe
// (S62 T7) is that it reports where Claim would act, without acting.

// THE probe's reason for existing: a stale socket is REPORTED and LEFT.
// Claim reclaims the same shape (TestClaimReclaimsAStaleEndpoint pins that
// half); the restore path calls Probe so a refusal can say "the holder is
// gone" while still never widening what the restore deletes.
func TestProbeDoesNotReclaimAStaleSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket")
	}
	path := tempEndpoint(t)

	// Bind and close without unlinking — exactly what a killed process leaves.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the stale socket was not left behind; the test proves nothing: %v", serr)
	}

	res, err := Probe()
	if err != nil {
		t.Fatalf("Probe on a stale socket: %v", err)
	}
	if res.Alive {
		t.Fatal("the probe reported alive for a socket with no holder")
	}
	if !res.Stale {
		t.Fatal("the probe did not report the socket as stale")
	}
	if res.Peer != "" {
		t.Fatalf("a stale socket has no peer to name, got %q", res.Peer)
	}
	if res.Path != path {
		t.Fatalf("Probe.Path = %q, want %q", res.Path, path)
	}

	// The part that separates Probe from Claim: the file is still there.
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the probe REMOVED the stale socket — Probe must report, not reclaim: %v", serr)
	}

	// And Claim, called on the very same shape, still reclaims it: the two
	// siblings agree on what they see, and only Claim acts.
	if err := Claim(); err != nil {
		t.Fatalf("Claim on the shape the probe just reported stale: %v", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("Claim did not remove the stale socket — the siblings disagree about reality")
	}
}

// The three states, pinned the way Claim's golden pins its own: free
// (nothing there), stale (file, no answer), alive (something answers, named).
func TestProbeFreeAndLiveStates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket")
	}
	path := tempEndpoint(t)

	// Free: no file at all.
	res, err := Probe()
	if err != nil {
		t.Fatalf("Probe on a free endpoint: %v", err)
	}
	if res.Alive || res.Stale || res.Peer != "" {
		t.Fatalf("free endpoint: got %+v, want everything false/empty", res)
	}
	if res.Path != path {
		t.Fatalf("Probe.Path = %q, want %q", res.Path, path)
	}

	// Live: a listener answers, and the peer is named.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close()
		}
	}()

	res, err = Probe()
	if err != nil {
		t.Fatalf("Probe on a live endpoint: %v", err)
	}
	if !res.Alive || res.Stale {
		t.Fatalf("live endpoint: got %+v, want Alive:true, Stale:false", res)
	}
	if runtime.GOOS == "linux" && !strings.Contains(res.Peer, "pid") {
		t.Fatalf("live endpoint on Linux: the peer should name a pid, got %q", res.Peer)
	}
	if res.Peer == "" {
		t.Fatal("live endpoint: the peer should be named")
	}

	// The file itself is untouched either way.
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the probe disturbed the live endpoint's file: %v", serr)
	}
}
