package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/endpoint"
	"github.com/cjlapao/aeon/internal/protocol"
)

// # The thirty lines that import `net`
//
// `internal/protocol` may not (T11, I1), and its client therefore takes an
// already-open stream. This is the other half: open the stream, hand it over.
//
// Keeping it here rather than in the protocol package is what lets S19 reuse
// the same client over HTTP and stdio, and what lets every client test run on
// an in-memory pipe.

const dialTimeout = 5 * time.Second

// ClientName is what the CLI calls itself in `initialize`, and what shows up in
// another client's `client.connected`. A real name rather than "client", so a
// person watching can tell which of their terminals did something.
const ClientName = "aeon-cli"

// dial opens a connection to the daemon and performs the handshake.
//
// `want` is the protocol version to ask for; 0 means "whatever you support".
// `--protocol-version` sets it, and exists so the demo can PROVE a refusal —
// otherwise negotiation's refusal path is unobservable until the first real
// version bump, years from now.
func dial(want int) (*protocol.Client, error) {
	// S1's resolver, not a second copy of the rule. `localrpc.EndpointPath()`
	// answers only inside the daemon process — the service holds the resolved
	// value — so a client needs the derivation, and `endpoint.Path()` already
	// is it. Two resolutions of the same path would disagree eventually, and
	// the symptom would be a client looking at a socket nobody is serving.
	// endpoint.Path supplies Aeon's namespace without mutating process-wide
	// database defaults. A client must not pin later direct opens to the state
	// directory of its first dial; framework initialization belongs to serve.
	path, err := endpoint.Path()
	if err != nil {
		return nil, fmt.Errorf("cannot work out where the daemon's endpoint is: %w", err)
	}

	network, err := endpointNetwork(path)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout(network, path, dialTimeout)
	if err != nil {
		// The path is in the message because it is the first thing anyone
		// debugging this needs, and because the daemon prints its own path on
		// startup — so the two can be compared without reading source.
		// A TYPED error rather than a plain one (S55 T11): the S55 control
		// verbs map this one case — nobody is home — to their fixed sentence;
		// every other dial fault is refused as-is and never falls back (the
		// S45 T8 rule). Its Error string is the plain sentence the verbs
		// before T11 rendered, so their bytes are unchanged.
		return nil, &noDaemonError{path: path, cause: err}
	}

	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(want); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// noDaemonError is the one dial failure that means "nobody is home": the
// endpoint path is known, but no process is serving it. The S55 control
// verbs (T11) map it to their fixed sentence — a stopped daemon has nothing
// to stop and applies no budget — while every other dial fault is refused
// as-is and never falls back (the S45 T8 rule).
type noDaemonError struct {
	path  string
	cause error
}

func (e *noDaemonError) Error() string {
	return fmt.Sprintf("no daemon at %s: %v\n  start one with `aeon serve`", e.path, e.cause)
}

func (e *noDaemonError) Unwrap() error { return e.cause }

// dialControl is the S55 control verbs' dial (T11, the P5 deviation): the
// verbs act on a RUNNING daemon, so "nobody is home" is the answer, not an
// error to diagnose — it maps to the verb's fixed sentence. Any other dial
// fault is refused as-is (the S45 T8 rule: refuse, never fall back).
func dialControl(want int, noDaemon string) (*protocol.Client, error) {
	c, err := dial(want)
	if err == nil {
		return c, nil
	}
	var nd *noDaemonError
	if errors.As(err, &nd) {
		return nil, errors.New(noDaemon)
	}
	return nil, err
}

// endpointNetwork picks the dial network, or explains why there is none.
//
// # Windows has no endpoint to dial, and the reason is upstream
//
// Found while writing this: `framework/localrpc`'s Windows transport is
// `ErrPlatformNotImplemented` — "the named-pipe implementation is deliberately
// deferred (brief O4): CI builds linux only, so a pipe implementation would
// ship unverified". So on Windows there is no daemon to connect to, because the
// daemon cannot bind an endpoint there either.
//
// This says so, rather than handing `net.Dial` a network it does not know and
// letting the person read "unknown network npipe". `go-winio` is already in the
// module graph, so implementing it upstream needs no new dependency.
//
// **This is wider than this slice** and is recorded in `docs/BUILD.md`: the
// six-target build produces Windows binaries that cannot serve. They are not
// broken builds — `version` and `--vectors` work — but `serve` will not.
func endpointNetwork(path string) (string, error) {
	if strings.HasPrefix(path, `\\.\pipe\`) {
		return "", fmt.Errorf(
			"the daemon cannot run on Windows yet: the framework's named-pipe transport "+
				"is not implemented (localrpc.ErrPlatformNotImplemented), so there is no "+
				"endpoint at %s to connect to.\n"+
				"  see aeon/docs/BUILD.md — the Windows gap is upstream, not here", path)
	}
	return "unix", nil
}
