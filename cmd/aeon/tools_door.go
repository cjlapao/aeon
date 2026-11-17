package main

// tools_door.go — S47 T8 (TC10, standing rule 1, P34, S45 PM4). The two doors
// onto the S47 tools reads, and the selection between them.
//
// # One implementation, two doors
//
// `aeon tools` is a CLIENT of the daemon's four `tools.*` reads (P34). With a
// daemon it speaks the socket (socketToolsDoor); with no daemon it opens the
// SAME adapter the daemon's table speaks — `daemon.NewToolsCatalogService` over
// a directly opened store, the one seed engine and the one MCP admin func — and
// says so on stderr (directToolsDoor). There is no third path and no CLI-side
// computation: not one state, reason, capability, drift field or empty-table
// sentence is decided in this package. `TestSocketAndDirectDoorsProduceTheSameOutput`
// pins the two doors byte-for-byte, and `TestDirectDoorUsesTheSamePackages`
// proves it at the parser: this file is the only place in the CLI that builds
// the adapter, and the CLI names no table, no row, no grant and no drift rule.
//
// # Why `surface` has no direct door at all
//
// The spec allowed either an offline surface listing or an explicit refusal, and
// the refusal is the honest one. What `surface.list` reports is WHICH METHODS A
// FULLY WIRED BUILD REGISTERS, and registration is seam-conditional (V13: an
// unwired seam's methods are simply absent). The wiring that decides it lives in
// `serve.go` — `wireAgentServices`, `wireMCPServices`, the autonomy and settings
// seams, the narrow door's actions, the agent services. A CLI that rebuilt a
// `protocol.Table` offline would either call `daemon.New` with a hand-written
// subset of that wiring (a second, drift-prone authority over what is wired, in
// the one listing whose whole subject is what is wired) or omit methods and
// print a partial table as the surface. Both are the confident wrong answer, so
// `aeon surface` refuses offline with `surfaceNeedsDaemon`. The tools reads have
// no such dependency: their world is the code registry, the `tool` rows, the
// policy set, the seed ledger and the MCP rows — all of them rows the CLI can
// open, which is why the parity test can build both doors over one store.
//
// # The --json byte rule
//
// The socket doors keep the wire's own bytes (`Call` into a `json.RawMessage`)
// and `--json` prints THOSE, not a re-encode: a printer that re-marshals can
// drop a `null` the wire carried, and a client scripting the CLI would never
// see it. The direct door has no wire, so its raw is the same typed value
// marshalled — which is what the daemon would have sent, because it is the same
// value from the same adapter (`TestJSONIsTheWireVerbatim` pins the socket half
// against bytes a stub server chose).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// toolsDirectFallbackLine is the fixed stderr line of the direct door (D9: a
// constant, never assembled from the error text). It says what happened and
// where the answer came from, because a read that silently switched doors is a
// read whose source nobody can tell afterwards.
const toolsDirectFallbackLine = "daemon not running — read from the store directly"

// surfaceNeedsDaemon is the offline refusal of the surface verbs, and the
// decision the spec left to T0/T6 — recorded here and in the task's Notes: the
// listing's subject is the wiring, and the wiring is serve.go's.
const surfaceNeedsDaemon = "surface needs the running daemon: what surface.list reports is which methods a fully wired build registers, and that wiring lives in serve.go — an offline table would be a partial one wearing the listing's clothes"

// toolsDoor is the four reads the CLI speaks. Each returns the typed result for
// the renderer and the RAW bytes for `--json`, plus the error the door answered
// with — a refusal travels as the daemon's own typed error, untouched (the CLI
// has nothing to add to a sentence that names the near names).
type toolsDoor interface {
	list(ctx context.Context, f protocol.ToolsListFilter) (protocol.ToolsListResult, json.RawMessage, error)
	get(ctx context.Context, name string) (protocol.ToolsGetResult, json.RawMessage, error)
	agent(ctx context.Context, name string) (protocol.ToolsByAgentResult, json.RawMessage, error)
	status(ctx context.Context) (protocol.ToolsStatusResult, json.RawMessage, error)
}

// toolsDial is the dial the selection uses; a package var so a test can stand in
// a dial that answers "no daemon" or refuses with a version fault without
// touching a real endpoint (the S43-extra T7 precedent, one dial).
var toolsDial = dial

// selectToolsDoor is the selection (S45 PM4): dial; a success is the socket
// door; "no daemon" is the direct door with the fixed stderr line; ANY other
// dial failure refuses. A live-but-disagreed daemon is a state the operator must
// see, not one the CLI routes around — falling back then would read the store
// while a daemon that disagrees with it is running.
func selectToolsDoor(ctx context.Context, errOut io.Writer) (toolsDoor, error) {
	c, err := toolsDial(0)
	if err != nil {
		if isNoDaemon(err) {
			fmt.Fprintln(errOut, toolsDirectFallbackLine)
			return newDirectToolsDoorFunc(ctx)
		}
		return nil, err
	}
	return socketToolsDoor{c: c}, nil
}

// --- the socket door -------------------------------------------------------

// socketToolsDoor speaks the four tools.* methods. It stamps no actor and sends
// none: these are the person's reads and the actor is the connection's, so a
// CLI cannot ask about an agent it is not (and `tools.mine.*`, which CAN only be
// an agent's own view, has no CLI verb at all — an agent-tool asked of a person's
// door would answer the wrong question).
type socketToolsDoor struct {
	c *protocol.Client
}

// callRaw runs one call and keeps the result's bytes exactly as they arrived.
// `*json.RawMessage` is the one target whose UnmarshalJSON stores the input
// verbatim, so nothing between the daemon and the terminal can drop a null.
func callRaw[T any](c *protocol.Client, ctx context.Context, method protocol.MethodName, params any) (T, json.RawMessage, error) {
	var res T
	var raw json.RawMessage
	if err := c.Call(method, params, &raw); err != nil {
		var zero T
		return zero, nil, err
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		var zero T
		return zero, nil, fmt.Errorf("the daemon's %s answer does not decode into the shape this build expects: %w", method, err)
	}
	return res, raw, nil
}

func (d socketToolsDoor) list(ctx context.Context, f protocol.ToolsListFilter) (protocol.ToolsListResult, json.RawMessage, error) {
	return callRaw[protocol.ToolsListResult](d.c, ctx, protocol.MethodToolsList, f)
}

func (d socketToolsDoor) get(ctx context.Context, name string) (protocol.ToolsGetResult, json.RawMessage, error) {
	return callRaw[protocol.ToolsGetResult](d.c, ctx, protocol.MethodToolsGet, protocol.ToolsGetParams{Name: name})
}

func (d socketToolsDoor) agent(ctx context.Context, name string) (protocol.ToolsByAgentResult, json.RawMessage, error) {
	return callRaw[protocol.ToolsByAgentResult](d.c, ctx, protocol.MethodToolsByAgent, protocol.ToolsByAgentParams{Agent: name})
}

func (d socketToolsDoor) status(ctx context.Context) (protocol.ToolsStatusResult, json.RawMessage, error) {
	return callRaw[protocol.ToolsStatusResult](d.c, ctx, protocol.MethodToolsStatus, struct{}{})
}

// --- the direct door -------------------------------------------------------

// directToolsDoor is the SAME adapter the daemon's table speaks, built over a
// directly opened store. It holds no cache, opens no second registry, and
// re-derives nothing: each method forwards to the seam and shapes the answer
// through the protocol package's own shaper.
type directToolsDoor struct {
	svc protocol.ToolsCatalog
	st  *store.Store
}

// newDirectToolsDoorFunc is the construction, a package var so the fall-back
// test can substitute a stub door without opening a store.
var newDirectToolsDoorFunc = newDirectToolsDoor

func newDirectToolsDoor(ctx context.Context) (toolsDoor, error) {
	// S62 T5's ONE constructor for a CLI store open (the pre-migration hook
	// rides it, and the hook guard refuses a raw store.Options literal here).
	st, err := store.Open(storeOpenOptions(false, "cli"))
	if err != nil {
		return nil, err
	}
	return buildDirectToolsDoor(st)
}

// buildDirectToolsDoor wires the adapter over an ALREADY OPEN store. It is a
// separate function for exactly one reason: the parity test hands it the very
// handle the daemon's table was built over, so the two roads answer from one
// database and any difference between them is the doors' and not the data's.
func buildDirectToolsDoor(st *store.Store) (toolsDoor, error) {
	// The SAME seed engine `seed.status` reads (one ledger, so `tools.status`
	// cannot report a second, differently stamped one for `tools.core`), and
	// the SAME MCP admin func value the daemon's options carry (one probe truth,
	// MG11). Neither is a CLI copy: both are the constructors the daemon's own
	// wiring calls.
	engine, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	svc := daemon.NewToolsCatalogService(daemon.Options{
		Store:    st,
		Seed:     engine,
		MCPAdmin: mcpAdminHandleFor(st, newMCPProbeCache()),
	})
	if svc == nil {
		_ = st.Close()
		return nil, fmt.Errorf("refused: the tools catalog is not wired in this build, so the four tools.* doors are absent (V13) — and an empty catalog would be a claim about a surface this build never mounted")
	}
	return directToolsDoor{svc: svc, st: st}, nil
}

func (d directToolsDoor) list(ctx context.Context, f protocol.ToolsListFilter) (protocol.ToolsListResult, json.RawMessage, error) {
	tools, err := d.svc.ToolsList(ctx, f)
	if err != nil {
		return protocol.ToolsListResult{}, nil, err
	}
	// The protocol package's OWN shaper, the one the socket handler calls: the
	// empty-table sentence is decided once for both doors or not at all.
	res := protocol.ShapeToolsListResult(f, tools)
	raw, merr := json.Marshal(res)
	return res, raw, merr
}

func (d directToolsDoor) get(ctx context.Context, name string) (protocol.ToolsGetResult, json.RawMessage, error) {
	res, err := d.svc.ToolsGet(ctx, name)
	if err != nil {
		return protocol.ToolsGetResult{}, nil, err
	}
	raw, merr := json.Marshal(res)
	return res, raw, merr
}

func (d directToolsDoor) agent(ctx context.Context, name string) (protocol.ToolsByAgentResult, json.RawMessage, error) {
	res, err := d.svc.ToolsByAgent(ctx, name)
	if err != nil {
		return protocol.ToolsByAgentResult{}, nil, err
	}
	raw, merr := json.Marshal(res)
	return res, raw, merr
}

func (d directToolsDoor) status(ctx context.Context) (protocol.ToolsStatusResult, json.RawMessage, error) {
	res, err := d.svc.ToolsStatus(ctx)
	if err != nil {
		return protocol.ToolsStatusResult{}, nil, err
	}
	raw, merr := json.Marshal(res)
	return res, raw, merr
}

// --- the surface door: the socket only -------------------------------------

// surfaceDoor is the two surface reads. There is ONE implementation of it
// (socketSurfaceDoor) and the refusal above is its offline half; the direct door
// does not exist and the reason is in this file's header.
type surfaceDoor interface {
	list(ctx context.Context, f protocol.SurfaceListFilter) (protocol.SurfaceListResult, json.RawMessage, error)
	get(ctx context.Context, method string) (protocol.SurfaceGetResult, json.RawMessage, error)
}

type socketSurfaceDoor struct {
	c *protocol.Client
}

// selectSurfaceDoor dials, and there is no second branch: no daemon is the
// refusal, and so is every other dial fault. A refusal is an error (exit
// non-zero), not an empty listing — an empty listing offline would read as "this
// daemon exposes nothing", which is the most confident wrong answer this verb can
// produce.
func selectSurfaceDoor(ctx context.Context, errOut io.Writer) (surfaceDoor, error) {
	c, err := toolsDial(0)
	if err != nil {
		if isNoDaemon(err) {

			return nil, fmt.Errorf("%s", surfaceNeedsDaemon)
		}
		return nil, err
	}
	return socketSurfaceDoor{c: c}, nil
}

func (d socketSurfaceDoor) list(ctx context.Context, f protocol.SurfaceListFilter) (protocol.SurfaceListResult, json.RawMessage, error) {
	return callRaw[protocol.SurfaceListResult](d.c, ctx, protocol.MethodSurfaceList, f)
}

func (d socketSurfaceDoor) get(ctx context.Context, method string) (protocol.SurfaceGetResult, json.RawMessage, error) {
	return callRaw[protocol.SurfaceGetResult](d.c, ctx, protocol.MethodSurfaceGet, protocol.SurfaceGetParams{Method: method})
}

// toolsErrString renders a door's error for the terminal. The typed protocol
// error carries its own three-part sentence in `Data`; it is printed as the
// error's own text and NEVER reworded here (a CLI paraphrase of a daemon's
// refusal is a second authority over a fact only the daemon can see).
func toolsErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// stderrLine is the small writer the verbs use for a diagnostic that is not
// output (so `--json` stays parseable and the fixed lines never reach stdout).
func stderrLine(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}
