package daemon

// S36-mcp-dispatch T1 — the seam and the composite.
//
// One sacred line (brief §0, MC11): this file, and all of internal/daemon,
// must never import internal/mcp. The MCP world reaches the step as a function
// value (Options.StepMCP, set by cmd/aeon — the only sanctioned importer), and
// a function value carries no import, so absent_test.go's banned list (which
// names internal/daemon explicitly) survives with zero diff lines. What lives
// here is the shape the daemon agrees on; the implementation that fills it
// lives in cmd/aeon (T2/T3/T4) and knows the word "mcp".

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// MCPCall dispatches one offered MCP tool: its qualified name (server.tool)
// plus the model's args, returning the result text (MD4's error-text result
// on failure) or a dispatch error. A func value, so internal/daemon never
// imports internal/mcp — the seam is the whole point (MD1).
type MCPCall func(ctx context.Context, qualified string, args json.RawMessage) (string, error)

// MCPWithheld is a candidate the offer did NOT offer, with the sentence that
// says why. The sentence is grants.go's own words, carried (MD9) — never
// re-typed here — so the three doors (CLI, view, runtime) speak one sentence.
type MCPWithheld struct {
	Qualified string
	Sentence  string
}

// MCPBundle is the one thing a step's MCP offer produces. A nil bundle (the
// seam is nil, or the offer is empty) means exactly today's behavior (MD8):
// BuildTools returns the first-party caller and its specs unchanged.
type MCPBundle struct {
	// Specs are the OFFERED tools only — with the server's live schema
	// (MD2: no synthesized specs), so the model is told these exist and only
	// these. Every name is a qualified server.tool name (MD10 pin #2).
	Specs []model.ToolSpec
	// Call dispatches a tool-use whose name is in Specs. A name is reached
	// only for offered tools (MD3); a forged name never lands here.
	Call MCPCall
	// Withheld is every non-offered candidate + grants.go's sentence (MD7),
	// surfaced once at step start (T4).
	Withheld []MCPWithheld
	// Close reaps the cached server children; idempotent. T4 wires it at all
	// four BuildTools callers (MD5); a nil Close means nothing to close.
	Close func() error
}

// compositeCaller is the runtime.ToolCaller a step gets once the MCP seam is
// wired: a tool-use name containing a dot routes to the bundle's Call (the MCP
// side), everything else routes to the first-party caller unchanged (MD1,
// MD10). The first-party caller is the SAME value today's BuildTools returns,
// so a wired step is today's step plus an MCP side — never a different one.
type compositeCaller struct {
	firstParty runtime.ToolCaller
	bundle     *MCPBundle
}

// Call routes by the dot. A dot name goes to the bundle; everything else goes
// to the first-party caller byte-for-byte, including its error handling. The
// bundle's (string, error) is converted to a ToolOutcome here: an error is the
// MD4 error-text result, not a panic.
func (c compositeCaller) Call(ctx context.Context, name string, input json.RawMessage) runtime.ToolOutcome {
	if c.bundle != nil && strings.Contains(name, ".") {
		out, err := c.bundle.Call(ctx, name, input)
		return runtime.ToolOutcome{Content: out, IsError: err != nil}
	}
	return c.firstParty.Call(ctx, name, input)
}

// Granted is the union of the first-party grant and the offered MCP names —
// the names this caller can actually call. (Granted is not read elsewhere in
// production, but the composite must answer it correctly rather than lie.)
func (c compositeCaller) Granted() []string {
	g := make([]string, 0, len(c.firstParty.Granted())+len(c.bundle.Specs))
	g = append(g, c.firstParty.Granted()...)
	for _, s := range c.bundle.Specs {
		g = append(g, s.Name)
	}
	return g
}

// Class preserves the first-party rerun-class the loop reads at
// runtime/receipt.go:42 (the unsafe/receipt path, d.Tools.(ToolClass)). A
// first-party name is delegated verbatim — so wiring MCP never changes a
// first-party tool's class. An MCP (dot) name is not first-party rerun-classed
// at all: it is the policy gate's decision, so it reports not-unsafe.
func (c compositeCaller) Class(name string) string {
	if strings.Contains(name, ".") {
		return ""
	}
	if classer, ok := c.firstParty.(runtime.ToolClass); ok {
		return classer.Class(name)
	}
	return ""
}

// Close reaps the cached server children (MD5). T4 wires it at the callers;
// the method exists here so the composite is closeable the day the wiring
// lands, and a nil bundle's Close is a no-op.
func (c compositeCaller) Close() error {
	if c.bundle != nil && c.bundle.Close != nil {
		return c.bundle.Close()
	}
	return nil
}

// Withheld returns the MCP offer's withheld candidate lines — the sentences
// grants.go wrote (MD9, carried verbatim), one per withheld candidate — so the
// daemon can ride them additively on the step.started event (Q3). A nil or
// empty bundle yields nil (MD8: no MCP offer, the event is byte-identical to
// today). The daemon reads this by type-asserting the composite caller; the
// first-party caller has no Withheld and the assertion fails cleanly.
func (c compositeCaller) Withheld() []string {
	if c.bundle == nil {
		return nil
	}
	lines := make([]string, 0, len(c.bundle.Withheld))
	for _, w := range c.bundle.Withheld {
		lines = append(lines, w.Sentence)
	}
	return lines
}
