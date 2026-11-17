package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
)

// narrow.go is the CLI's side of the narrow surface (S19): the read of the
// table, and the glue that lets the HTTP mounting dispatch THROUGH the
// daemon's own table without the daemon importing the mounting.
//
// `aeon narrow list` is the N18 test made executable by a human:
//
//	diff <(aeon narrow list --mounting=stdio) <(aeon narrow list --mounting=http)
//
// must be empty, and both halves print from each mounting's OWN rendering
// function — the stdio names come from the server's emitted tool list, the
// http names from the mux's mounted entries — so a mounting-side tool or
// route that the table lacks shows up as a diff line, not as a passing
// test. A pure client verb: the question is about the binary's design, so
// it needs no daemon running.

func cmdNarrow(w io.Writer, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		_, err := fmt.Fprint(w, "usage:\n  aeon narrow list [--mounting=stdio|http]\n\nprints the narrow surface's action table — one action per line, in\ntable order. Both mountings render the same table (N18); the diff of\nthe two is the third-surface test.\n")
		return err
	}
	fs := flag.NewFlagSet("narrow list", flag.ContinueOnError)
	mounting := fs.String("mounting", "stdio", "which mounting's rendering to print (stdio|http)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var names []string
	switch *mounting {
	case "stdio":
		// The stdio mounting's emitted list — the same rendering the wire
		// carries to an editor (Server.ToolNames goes through toolsList).
		names = narrow.NewServer(nil, narrow.Actions()).ToolNames()
	case "http":
		// The HTTP mounting's mux — the same rendering HTTPRoutes() hands
		// the framework's server to mount.
		for _, e := range narrow.NewMux(nil, narrow.Actions()).Entries() {
			names = append(names, e.Action.Name)
		}
	default:
		return fmt.Errorf("unknown mounting %q — there is stdio and http; two transports, two surfaces (P25)", *mounting)
	}
	for _, n := range names {
		fmt.Fprintln(w, n)
	}
	return nil
}

// --- serve-side glue (wired by serveWithRecovery, never by the CLI path) ---

// splitHTTPAddr enforces "an address, not a port" (N15): a bare port is
// refused with the shape demanded, and an empty host (":7777") is said out
// loud as all-interfaces rather than silently bound.
func splitHTTPAddr(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("--http wants a whole address like 127.0.0.1:7777, not %q: %w", addr, err)
	}
	if port == "" {
		return "", "", fmt.Errorf("--http wants a whole address like 127.0.0.1:7777 — %q has no port", addr)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	return host, port, nil
}

// lateDaemon carries the daemon dispatcher from the moment it exists. The
// registry must be complete before Start; the dispatcher is constructed
// after it. The mutex makes the one write and the many reads honest.
type lateDaemon struct {
	mu sync.Mutex
	c  narrow.Caller
}

func (l *lateDaemon) set(c narrow.Caller) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.c = c
}

func (l *lateDaemon) get() narrow.Caller {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.c
}

// daemonCaller is the HTTP mounting's route into the daemon: the SAME
// *protocol.Table the socket serves, dispatched under one synthetic
// connection. It is NOT a bypass — the table's own validation, policy and
// session gates run exactly as they do for a socket client. initialize is
// mandatory and first (S18's F-7, applied from the inside this time), and
// it runs once for the narrow connection; every method here besides
// initialize carries RequiresInitialize, so a dispatcher that skipped the
// handshake would answer nothing but refusals.
type daemonCaller struct {
	table *protocol.Table

	mu      sync.Mutex
	ready   bool
	initErr error
}

const narrowConnID = "narrow-http"

func (c *daemonCaller) Call(method protocol.MethodName, params, result any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return narrow.NewMsg(narrow.ErrTransport, "cannot encode the request: "+err.Error())
	}
	c.mu.Lock()
	if !c.ready {
		iRaw, _ := json.Marshal(protocol.InitializeParams{
			ClientName:    "aeon-narrow-http",
			ClientVersion: Version,
			// An int, from the outside, on purpose — the socket speaks
			// integer versions; MCP's date strings stay on the stdio
			// transport where they belong.
			ProtocolVersion: protocol.PROTOCOL_VERSION,
		})
		_, c.initErr = c.table.Dispatch(context.Background(), narrowConnID, protocol.MethodInitialize, iRaw)
		if c.initErr == nil {
			c.ready = true
		}
	}
	initErr := c.initErr
	c.mu.Unlock()
	if initErr != nil {
		return narrow.NewMsg(narrow.ErrTransport, "the narrow surface could not open its daemon session: "+initErr.Error())
	}
	out, err := c.table.Dispatch(context.Background(), narrowConnID, method, json.RawMessage(raw))
	if err != nil {
		// The daemon's refusal verbatim (I5) — the mux renders the kind.
		return err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return narrow.NewMsg(narrow.ErrTransport, "cannot encode the daemon's answer: "+err.Error())
	}
	return json.Unmarshal(b, result)
}
