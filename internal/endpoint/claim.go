// Package endpoint decides whether this process may own the daemon's endpoint.
//
// It exists because of an ordering problem: localrpc binds during Init, so a
// second `aeon serve` fails inside the framework with a bind error. That error
// is true and useless — it does not say another daemon is already running, or
// which one.
//
// So Aeon claims the endpoint first, and says something better.
package endpoint

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/osutils"
)

// dialTimeout bounds the liveness probe.
//
// Short on purpose: this runs before anything else on every start, and the
// only question is whether something answers. A process too busy to accept
// within this window is still a running daemon — the probe is not a health
// check, and treating a slow answer as "dead" is how two daemons end up on one
// endpoint.
const dialTimeout = 2 * time.Second

const (
	endpointPathEnv = "LOCALRPC_ENDPOINT_PATH"
	namespaceEnv    = "LOCALRPC_APP_NAMESPACE"
	// defaultEndpointName matches localrpc's models.DefaultEndpointName.
	defaultEndpointName = "rpc"
	defaultNamespace    = "aeon"
)

// ErrAlreadyRunning is returned when a live daemon holds the endpoint.
var ErrAlreadyRunning = errors.New("another aeon daemon is already running")

// Claim verifies that no live daemon holds this endpoint, reclaiming it if the
// holder is gone.
//
// Three outcomes, and the middle one is the one worth getting right:
//
//   - nothing there            → proceed
//   - something answers        → refuse, naming the endpoint and the peer
//   - a socket with no holder  → RECLAIM it and proceed
//
// **A stale socket is reclaimed, not refused** (O5). The alternative is a
// daemon that cannot start after a crash — which is the exact opposite of what
// S7's recovery exists for, and it would turn one bad shutdown into a manual
// `rm` nobody documented.
func Claim() error {
	path, err := Path()
	if err != nil {
		// Not fatal here: the framework resolves and validates the path itself
		// during Init, and reports it properly. Failing the boot now would turn
		// a resolvable problem into a daemon that will not start, with a worse
		// message than the one the framework already has.
		return nil //nolint:nilerr // reported by the framework's own validation
	}

	// A named pipe on Windows has no filesystem presence to stat, so the probe
	// is the whole check there; Dial failing means nothing is listening.
	if _, statErr := os.Stat(path); statErr != nil && !isWindowsPipe(path) {
		return nil // nothing there
	}

	conn, dialErr := net.DialTimeout(network(path), path, dialTimeout)
	if dialErr == nil {
		defer conn.Close()
		return fmt.Errorf("%w\n  endpoint  %s\n  holder    %s\n\nstop it first, or set %s to run a second daemon on a different endpoint",
			ErrAlreadyRunning, path, describePeer(conn), endpointPathEnv)
	}

	// Nothing answered. On POSIX that leaves a socket file behind that would
	// make bind fail with "address already in use" — so remove it.
	if isWindowsPipe(path) {
		return nil // a pipe with no server simply does not exist
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("a stale endpoint at %s could not be removed: %w", path, rmErr)
	}
	return nil
}

// Path resolves where the endpoint will be, before any service initialises.
//
// It mirrors localrpc's own resolution: an explicit path wins, otherwise the
// app namespace resolves one. `osutils.EndpointPath` is documented as a pure
// function needing no initialised service, which is what makes this callable
// this early.
func Path() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv(endpointPathEnv)); explicit != "" {
		return explicit, nil
	}
	ns := strings.TrimSpace(os.Getenv(namespaceEnv))
	if ns == "" {
		ns = defaultNamespace
	}
	return osutils.EndpointPath(ns, defaultEndpointName)
}

func network(path string) string {
	if isWindowsPipe(path) {
		return "unix" // unreachable on POSIX; Windows uses a pipe dialer
	}
	return "unix"
}

func isWindowsPipe(path string) bool {
	return strings.HasPrefix(path, `\\.\pipe\`)
}

// describePeer names the holder as precisely as the platform allows.
//
// Peer credentials are a POSIX affordance and the reason P26 works at all, so
// where they are available the pid is the most useful thing a refusal can say.
// Where they are not, saying so is better than inventing a number.
func describePeer(conn net.Conn) string {
	if pid, ok := peerPID(conn); ok {
		return fmt.Sprintf("pid %d", pid)
	}
	return "a live process (pid unavailable on this platform)"
}
