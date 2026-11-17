package endpoint

// S62 T1 · BK characterization golden at the CLAIM level.
//
// Claim() is the mutation T7's restore door must NOT reuse: T7 adds
// Probe() = Claim minus the removal — it reports the same three states
// (nothing there / live peer / stale file) without deleting anything,
// because a probe that reclaims a socket its caller never asked about is a
// write wearing a read's name. This file is the baseline Probe must DIFFER
// from: the same states, the same errors, and the stale case WITHOUT the
// unlink. The live and free states are pinned here too, because the
// difference lives in the stale case and a reviewer must see the whole
// contract to check the one line that changes.

import (
	"errors"
	"net"
	"os"
	"runtime"
	"testing"
)

// TestClaimReclaimsAStaleSocket pins the mutating half of Claim: a socket
// file left by a dead holder is REMOVED, so the bind that follows can
// succeed.
//
// T7's Probe() must return the SAME "stale" verdict and NOT remove the file.
// If this test ever passes without the removal, the probe's difference is
// gone and the restore door is writing again.
func TestClaimReclaimsAStaleSocket(t *testing.T) {
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

	if err := Claim(); err != nil {
		t.Fatalf("Claim refused a stale endpoint: %v", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("the stale endpoint was not removed — Claim's contract is reclaim, and T7's Probe must be the one that stops doing this")
	}
}

// TestClaimFreeAndLiveStatesPinned pins the other two states of the
// three-state contract, so the Probe contrast is reviewable against the
// whole set. Claim never BINDS — the daemon's localrpc service does that
// later — so the free state's golden is "nothing there, nothing created":
// Claim's entire observable mutation set is the stale-file removal, which is
// the one line T7's Probe stops doing. The live state keeps the holder's
// socket and answers with the refusal that names the holder.
func TestClaimFreeAndLiveStatesPinned(t *testing.T) {
	t.Run("nothing there, claim proceeds and creates nothing", func(t *testing.T) {
		path := tempEndpoint(t)
		if _, serr := os.Stat(path); !os.IsNotExist(serr) {
			t.Fatalf("the starting point was not a missing socket: %v", serr)
		}
		if err := Claim(); err != nil {
			t.Fatalf("Claim on a free endpoint: %v", err)
		}
		if _, serr := os.Stat(path); !os.IsNotExist(serr) {
			t.Fatal("Claim created a socket file; the bind belongs to the daemon's localrpc service, and Claim's only mutation is the stale removal")
		}
	})

	t.Run("a live peer refuses, and the file is untouched", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix socket")
		}
		path := tempEndpoint(t)
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

		err = Claim()
		if err == nil {
			t.Fatal("Claim succeeded while a live holder was listening")
		}
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("error = %v, want ErrAlreadyRunning", err)
		}
		if _, serr := os.Stat(path); serr != nil {
			t.Fatal("the live holder's socket file was disturbed by a refused claim")
		}
	})
}
