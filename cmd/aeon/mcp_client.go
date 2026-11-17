package main

// mcp_client.go — S46 T9 (MG5, MG6, P34). The two doors onto the one MCP
// implementation, and the selection between them — S45 T8's policy_client.go
// pattern, one implementation and a second CALLER.
//
// # One implementation, two doors
//
// `aeon mcp <verb>` is a CLIENT of the daemon's socket methods (T3's five
// reads, T4's import door, T5's seven writes). selectMCPDoor dials once; a
// successful handshake speaks the methods over the socket (mcpSocketDoor);
// a missing endpoint runs the SAME handler functions the daemon's dispatch
// calls — mcpAdminHandleFor over a directly opened store, mcpHandleFor for
// the two S34 probe reads (mcpDirectDoor) — saying the fixed line on stderr
// FIRST. Any OTHER dial failure (permission denied on the endpoint, a
// handshake refusal, a version skew) REFUSES: falling back from an error
// that is not "nobody is home" would route around a daemon that exists.
//
// Both doors relay through the SAME boundary: one method name, one params
// blob, one protocol result pointer. The socket door decodes the wire's
// bytes into that pointer; the direct door calls the handler the daemon
// would have called and round-trips its answer through JSON into the SAME
// pointer — so the printed bytes cannot depend on which door answered,
// because there is no second implementation to depend on. TestDirectDoor-
// UsesTheSameAdminFunctions pins the absence of copies by import graph.
//
// # The actor is the connection or the cli — never a param
//
// The socket door's actor is the kernel peer the daemon stamps (the CLI
// never sends one). The direct door stamps {cli, cli} onto the SAME channel
// (protocol.WithMCPActor) the dispatch stamps, so the audit rows it pairs
// with its writes carry actor_kind='cli' — the offline door names itself;
// it never impersonates a peer. Neither door reads an actor from a flag.
//
// # The CLI re-validates NOTHING
//
// Flag parsing refuses only what cannot be a value (an env NAME=value, a
// malformed --tool triple, a relative --path — input parsing, C17's rule
// applied before a value ever exists). Every other refusal is the
// validator's or the seam's sentence printed VERBATIM — the socket door
// maps the wire's typed refusal to its `what`, the direct door prints the
// seam's own sentence, and both are the same string because both come from
// the same function (TestMcpAddRefusalsVerbatimBothDoors).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpDirectNotice is the FIXED stderr line the offline door says before any
// output (S45's rule — the operator always knows which store was written).
const mcpDirectNotice = "daemon not running — applied directly to the store"

// mcpDoor is the two doors onto the one MCP implementation. The relay is
// deliberately narrow (method name in, protocol result out): a door that
// could special-case a method could special-case it differently.
type mcpDoor interface {
	// relay speaks one method with one params blob into one protocol
	// result pointer. A refusal arrives as an error carrying the refusing
	// function's own sentence — never a reworded one.
	relay(ctx context.Context, method protocol.MethodName, params any, out any) error
	close() error
}

// mcpDial is THE dial behind a seam variable (S45's policyDial precedent):
// the selection test drives the three outcomes — live, nobody-home, and
// the refusals that must never fall back — without spawning a daemon.
var mcpDial = dial

// selectMCPDoor is the selection: dial; success is the socket door, a "no
// endpoint" is the direct door (with the fixed line on errOut), and any
// other dial failure is a REFUSAL — never a fallback.
func selectMCPDoor(errOut io.Writer) (mcpDoor, error) {
	c, err := mcpDial(0)
	if err == nil {
		return &mcpSocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, mcpDirectNotice)
		return newMCPDirectDoor()
	}
	return nil, err
}

// mcpVerbatim maps a wire refusal to the refusing function's own sentence:
// the typed errors carry it in `what`; the direct door's seam sentences
// arrive as plain errors already. Anything without a sentence (a transport
// fault, an unknown method) travels as the error itself.
func mcpVerbatim(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.Data.What != "" {
		return errors.New(pe.Data.What)
	}
	return err
}

// --- the socket door ---------------------------------------------------------

type mcpSocketDoor struct{ c *protocol.Client }

func (d *mcpSocketDoor) relay(_ context.Context, method protocol.MethodName, params, out any) error {
	if err := d.c.Call(method, params, out); err != nil {
		return mcpVerbatim(err)
	}
	return nil
}

func (d *mcpSocketDoor) close() error { return d.c.Close() }

// --- the direct door ----------------------------------------------------------

// mcpDirectDoor runs the daemon's OWN handler functions over a store the
// CLI opened itself. newMCPAdmin's family (mcpAdminHandleFor) answers the
// management methods; the two probe reads ride mcpHandleFor — the same
// constructors the daemon's serve wiring mounts (cmd/aeon/mcp_dispatch_*.go
// builds them the same way), so "the same functions T5 wrote" is true by
// construction, not by copy.
type mcpDirectDoor struct {
	st    *store.Store
	admin func(ctx context.Context, method string, params json.RawMessage) (any, string, error)
	probe func(ctx context.Context, method string, params json.RawMessage) (any, string, error)
}

func newMCPDirectDoor() (mcpDoor, error) {
	st, err := openStoreDirectly(false)
	if err != nil {
		return nil, err
	}
	// ONE probe cache per door: `status` after `check` sees the check's
	// answer — the same shared-cache behaviour the daemon has (MG5's
	// single-flight family, offline).
	cache := newMCPProbeCache()
	return &mcpDirectDoor{
		st:    st,
		admin: mcpAdminHandleFor(st, cache),
		probe: mcpHandleFor(st, cache),
	}, nil
}

func (d *mcpDirectDoor) relay(ctx context.Context, method protocol.MethodName, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("cannot speak %s to the store: %w", method, err)
	}
	handler := d.admin
	switch method {
	case protocol.MethodMCPStatus, protocol.MethodMCPCheck:
		handler = d.probe
	}
	// The actor channel: the SAME context seam the dispatch stamps — the
	// offline door stamps itself as {cli, cli}, and the audit rows the
	// writes pair carry actor_kind='cli'. Reads ignore it; the writes
	// REFUSE without it (never an empty audit line).
	ctx = protocol.WithMCPActor(ctx, "cli", "cli")
	v, sentence, err := handler(ctx, string(method), json.RawMessage(raw))
	if err != nil {
		return mcpVerbatim(err)
	}
	if sentence != "" {
		return errors.New(sentence) // the seam's own words, verbatim
	}
	// Round-trip through JSON into the caller's PROTOCOL result pointer —
	// the bytes the socket would have carried, decoded the same way. This
	// is the "both doors print one truth" mechanism: no field is reachable
	// that the wire does not also carry.
	blob, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cannot render the store's %s answer: %w", method, err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(blob, out); err != nil {
		return fmt.Errorf("the store's %s answer does not fit the protocol shape: %w", method, err)
	}
	return nil
}

func (d *mcpDirectDoor) close() error {
	if d.st == nil {
		return nil
	}
	return d.st.Close()
}
