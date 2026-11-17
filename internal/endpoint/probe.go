// Probe is the read-only sibling of Claim (S62 T7, BK9 step 1).
//
// Claim asks "may this process own the endpoint" and reclaims a stale socket
// on the way. Probe asks one weaker question — "is something alive on the
// endpoint" — and touches nothing: a stale socket is REPORTED (Alive:false,
// Stale:true) and left exactly where it is, because its removal is Claim's
// job, and the restore path must not be widening what it cleans up.
package endpoint

import (
	"net"
	"os"
)

// Result is the probe's answer.
type Result struct {
	// Path is where the endpoint resolves (the resolved value of Path()).
	Path string

	// Alive is true when something answered the dial.
	Alive bool

	// Stale is true when a socket file is present but nothing answers —
	// the holder is gone. The file is NOT removed by the probe; it is
	// reported so the caller can say so and let Claim reclaim it later.
	// On Windows there is no file to inspect, so Stale is always false:
	// a pipe with no server simply does not exist.
	Stale bool

	// Peer names the holder as precisely as the platform allows,
	// as in Claim's refusal. Empty when Alive is false.
	Peer string
}

// Probe reports whether a live daemon holds this endpoint, without
// reclaiming anything. It mirrors Claim's resolution and dial exactly —
// the same Path(), the same 2 s dial, the same peer naming — and drops
// only the removal: dialing succeeds means alive; a present file with a
// failed dial means stale; a missing file means free.
func Probe() (Result, error) {
	path, err := Path()
	if err != nil {
		return Result{}, err
	}
	res := Result{Path: path}

	// A named pipe on Windows has no filesystem presence to stat, so the
	// dial is the whole probe there, and a failed dial means simply
	// "not alive" — there is no file behind it to be stale.
	if _, statErr := os.Stat(path); statErr == nil && !isWindowsPipe(path) {
		res.Stale = true // a file is there; the dial below decides whether it answers
	}

	conn, dialErr := net.DialTimeout(network(path), path, dialTimeout)
	if dialErr != nil {
		return res, nil // free, or stale with the file left in place
	}
	defer conn.Close()
	res.Alive = true
	res.Stale = false
	res.Peer = describePeer(conn)
	return res, nil
}
