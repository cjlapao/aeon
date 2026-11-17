package main

// tools_parity_test.go — S47 T8: the two doors, the selection between them, and
// the parser's view of what the CLI is allowed to compute.
//
// The parity claim (TC10, standing rule 1, P34) is that `aeon tools` answers from
// ONE implementation whichever door it speaks. The both-roads harness is the
// in-process one (the S56 autonomy precedent): a fully wired daemon built over one
// store, its table serving a real protocol.Client over an in-memory pipe, and the
// direct door built over the SAME handle — so a difference between the two answers
// can only be the doors' doing. The subprocess harness (policy_client_test.go)
// would compare two databases; this compares one database through two doors,
// which is the claim being made.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

// toolsRoadStore opens an isolated store for one test (the S45 pin, so an earlier
// in-process daemon's leaked DATABASE_SQLITE_STORAGE_PATH cannot make the two
// roads read two different databases).
func toolsRoadStore(t *testing.T) *store.Store {
	t.Helper()
	isolateStoreEnv(t)
	agentHome(t)
	return openAgentStore(t)
}

// toolsRoadDaemon builds the daemon exactly the way `aeon serve` wires the half
// this slice speaks: the store, the ONE seed engine, the MCP services through the
// entry point's own wiring function. "Registered" therefore means what it means in
// production, not what a hand-built table happens to hold.
func toolsRoadDaemon(t *testing.T) (*daemon.Daemon, *store.Store) {
	t.Helper()
	st := toolsRoadStore(t)
	engine, err := daemon.NewSeedEngine(st, seed.Options{}, mcpSeedValidator(st))
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	opts := daemon.Options{Store: st, Seed: engine}
	if err := wireMCPServices(st, &opts); err != nil {
		t.Fatalf("wire the MCP services: %v", err)
	}
	return daemon.New(opts), st
}

func toolsRoadTable(t *testing.T) *protocol.Table {
	t.Helper()
	d, _ := toolsRoadDaemon(t)
	return d.Table
}

// toolsRoadDoors returns the two doors over ONE store: the socket door speaking to
// the fully wired table over an in-memory pipe, and the direct door built by the
// production wiring function over the same handle.
func toolsRoadDoors(t *testing.T) (socketToolsDoor, toolsDoor) {
	t.Helper()
	d, st := toolsRoadDaemon(t)
	c := protocol.NewClient(parityPipeServer(t, d.Table), "parity-test", "0")
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake over the in-memory pipe: %v", err)
	}
	direct, err := buildDirectToolsDoor(st)
	if err != nil {
		t.Fatalf("the direct door over the daemon's own store: %v", err)
	}
	return socketToolsDoor{c: c}, direct
}

// --- the parity --------------------------------------------------------------

// parityCall runs one read through both doors and compares everything: the typed
// answer re-marshalled, the RAW bytes each door hands the printer, and the refusal
// text when the answer is a refusal.
func parityCall(t *testing.T, name string, call func(d toolsDoor) (any, json.RawMessage, error)) {
	t.Helper()
	sock, direct := toolsRoadDoors(t)
	typedA, rawA, errA := call(sock)
	typedB, rawB, errB := call(direct)
	switch {
	case (errA == nil) != (errB == nil):
		t.Fatalf("%s: one door refused and the other answered — socket=%v direct=%v", name, errA, errB)
	case errA != nil:
		if errA.Error() != errB.Error() {
			t.Fatalf("%s: the two doors refuse differently:\n socket: %v\n direct: %v", name, errA, errB)
		}
		return
	}
	ja, merr := json.Marshal(typedA)
	if merr != nil {
		t.Fatalf("%s: marshal the socket answer: %v", name, merr)
	}
	jb, merr := json.Marshal(typedB)
	if merr != nil {
		t.Fatalf("%s: marshal the direct answer: %v", name, merr)
	}
	if string(ja) != string(jb) {
		t.Fatalf("%s: the two doors answer differently:\n socket: %s\n direct: %s", name, ja, jb)
	}
	if string(rawA) != string(rawB) {
		t.Fatalf("%s: the --json bytes differ (a script would see two different CLIs):\n socket: %s\n direct: %s", name, rawA, rawB)
	}
}

func TestSocketAndDirectDoorsProduceTheSameOutput(t *testing.T) {
	ctx := t.Context()
	parityCall(t, "tools list (unfiltered)", func(d toolsDoor) (any, json.RawMessage, error) {
		return d.list(ctx, protocol.ToolsListFilter{})
	})
	parityCall(t, "tools list --family workspace", func(d toolsDoor) (any, json.RawMessage, error) {
		f := "workspace"
		return d.list(ctx, protocol.ToolsListFilter{Family: &f})
	})
	parityCall(t, "tools list --family nope (the filtered empty)", func(d toolsDoor) (any, json.RawMessage, error) {
		f := "no-such-family"
		return d.list(ctx, protocol.ToolsListFilter{Family: &f})
	})
	parityCall(t, "tools list --available=false", func(d toolsDoor) (any, json.RawMessage, error) {
		no := false
		return d.list(ctx, protocol.ToolsListFilter{Available: &no})
	})
	parityCall(t, "tools get read", func(d toolsDoor) (any, json.RawMessage, error) {
		return d.get(ctx, "read")
	})
	parityCall(t, "tools get reed (the refusal)", func(d toolsDoor) (any, json.RawMessage, error) {
		return d.get(ctx, "reed")
	})
	parityCall(t, "tools status", func(d toolsDoor) (any, json.RawMessage, error) {
		return d.status(ctx)
	})

	// The agent view needs a real agent row: an unknown slug refuses on both
	// roads, so both halves of that answer are worth pinning.
	d, st := toolsRoadDaemon(t)
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "atlas", Name: "atlas", Version: "1"}, "2026-11-01T12:00:00Z"); err != nil {
		t.Fatalf("plant atlas: %v", err)
	}
	c := protocol.NewClient(parityPipeServer(t, d.Table), "parity-test", "0")
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	direct, err := buildDirectToolsDoor(st)
	if err != nil {
		t.Fatalf("direct door: %v", err)
	}
	for _, agent := range []string{"atlas", "nobody"} {
		a, rawA, errA := direct.agent(ctx, agent)
		b, rawB, errB := socketToolsDoor{c: c}.agent(ctx, agent)
		if (errA == nil) != (errB == nil) {
			t.Fatalf("agent %q: one door refused: %v / %v", agent, errA, errB)
		}
		if errA != nil {
			if errA.Error() != errB.Error() {
				t.Fatalf("agent %q: refusals differ:\n direct: %v\n socket: %v", agent, errA, errB)
			}
			continue
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("agent %q: the two views differ", agent)
		}
		if string(rawA) != string(rawB) {
			t.Fatalf("agent %q: the --json bytes differ", agent)
		}
	}
}

// --- the --json byte rule ----------------------------------------------------

// fixedBytesServer answers every call with bytes the test chose, so the printer is
// compared against a wire the CLI did not author. The chosen answer carries an
// explicit `"mcp":null` — the field a re-encode of the decoded value drops, which
// is exactly what this test exists to catch.
func fixedBytesServer(t *testing.T, answer string) *protocol.Client {
	t.Helper()
	cr, cs := net.Pipe()
	t.Cleanup(func() { _ = cr.Close(); _ = cs.Close() })
	go func() {
		defer cs.Close()
		buf := make([]byte, 4096)
		for {
			n, err := cs.Read(buf)
			if err != nil || n == 0 {
				return
			}
			var req struct {
				ID     *int   `json:"id"`
				Method string `json:"method"`
			}
			if uerr := json.Unmarshal(buf[:n], &req); uerr != nil || req.ID == nil {
				continue
			}
			frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`+"\n", *req.ID, answer)
			if _, werr := cs.Write([]byte(frame)); werr != nil {
				return
			}
		}
	}()
	c := protocol.NewClient(cr, "byte-test", "0")
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return c
}

func TestJSONIsTheWireVerbatim(t *testing.T) {
	const answer = `{"tool":{"name":"read","family":"workspace","source":"system","description":"","schema":null,"schemaHash":"","rerunClass":"safe","capabilities":[],"governed":[],"conditional":false,"available":true,"classified":false,"version":0,"origin":"system","rowCurrent":true},"agents":[],"policies":[],"mcp":null}`
	door := socketToolsDoor{c: fixedBytesServer(t, answer)}
	res, raw, err := door.get(t.Context(), "read")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != answer {
		t.Fatalf("the door did not keep the wire's bytes:\n got: %s\nwant: %s", raw, answer)
	}
	var out bytes.Buffer
	if err := printRawJSON(&out, raw); err != nil {
		t.Fatal(err)
	}
	if out.String() != answer+"\n" {
		t.Errorf("--json is not the wire verbatim:\n got: %s\nwant: %s", out.String(), answer+"\n")
	}
	// The test has teeth: the same value printed the way a re-encoding client
	// would print it LOSES the null. If this ever stops being true, `printRawJSON`
	// and `printJSON` have converged and the byte rule is unenforced.
	reEncoded, merr := json.Marshal(res)
	if merr != nil {
		t.Fatal(merr)
	}
	if bytes.Contains(reEncoded, []byte(`"mcp"`)) {
		t.Errorf("the re-encode kept the null after all — this test no longer distinguishes the two printers")
	}
}

func TestJSONVerbPrintsTheDoorsBytesAndNothingElse(t *testing.T) {
	raw := json.RawMessage(`{"tools":[],"statement":"no tools match this filter."}`)
	door := &fakeToolsDoor{listRaw: raw}
	useDoor(t, door)
	var out bytes.Buffer
	if err := cmdTools(&out, []string{"list", "--family", "nope", "--json"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != string(raw)+"\n" {
		t.Errorf("--json printed %q, want the door's bytes", out.String())
	}
}

// --- the selection -----------------------------------------------------------

// noDaemonErrForTest reproduces dial.go's wrap shape, which is what isNoDaemon
// reads (a fabricated "connection refused" that does not say "no daemon at" would
// test nothing).
func noDaemonErrForTest() error {
	return fmt.Errorf("no daemon at /tmp/definitely-not-here/aeon.sock: connect: no such file or directory\n  start one with `aeon serve`")
}

// TestSelectToolsDoorFallsBackOnlyWhenNoDaemon is the spec's
// TestSelectDoorFallsBackOnlyWhenNoDaemon, named for this family: the seed slice
// already owns the bare name in this package (seed_test.go), and two tests in
// one package cannot answer to one name.
func TestSelectToolsDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	var errOut bytes.Buffer
	builds := 0
	prevBuild := newDirectToolsDoorFunc
	newDirectToolsDoorFunc = func(context.Context) (toolsDoor, error) {
		builds++
		return &fakeToolsDoor{}, nil
	}
	t.Cleanup(func() { newDirectToolsDoorFunc = prevBuild })

	// (a) no daemon → the direct door, with the fixed line on stderr.
	prevDial := toolsDial
	toolsDial = func(int) (*protocol.Client, error) { return nil, noDaemonErrForTest() }
	d, err := selectToolsDoor(t.Context(), &errOut)
	if err != nil {
		t.Fatalf("no daemon should fall back, not refuse: %v", err)
	}
	if _, ok := d.(*fakeToolsDoor); !ok {
		t.Fatalf("no daemon did not select the direct door: %T", d)
	}
	if builds != 1 {
		t.Errorf("the direct door was built %d times", builds)
	}
	got := errOut.String()
	if got != toolsDirectFallbackLine+"\n" {
		t.Errorf("the fallback said %q, want the fixed line %q", got, toolsDirectFallbackLine+"\n")
	}
	if !strings.Contains(got, "daemon not running") {
		t.Errorf("the fixed line does not say what happened: %q", got)
	}
	toolsDial = prevDial

	// (b) any OTHER dial fault → refusal, no fallback. A daemon that is alive but
	// disagrees about the protocol version is a state to see, not to route around.
	builds = 0
	errOut.Reset()
	toolsDial = func(int) (*protocol.Client, error) {
		return nil, errors.New("protocol version mismatch: the daemon speaks 3, this client speaks 1")
	}
	if _, err := selectToolsDoor(t.Context(), &errOut); err == nil {
		t.Fatal("a version mismatch fell back instead of refusing")
	} else if !strings.Contains(err.Error(), "version mismatch") {
		t.Errorf("the refusal lost the dial's own reason: %v", err)
	}
	if builds != 0 {
		t.Errorf("a non-'no daemon' dial failure still opened the store directly (%d builds)", builds)
	}
	if errOut.Len() != 0 {
		t.Errorf("a refusal printed the fallback line anyway: %q", errOut.String())
	}

	// (c) a permission fault is a refusal too (isNoDaemon's second branch).
	builds = 0
	errOut.Reset()
	toolsDial = func(int) (*protocol.Client, error) {
		return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("permission denied: %w", os.ErrPermission)}
	}
	if _, err := selectToolsDoor(t.Context(), &errOut); err == nil {
		t.Fatal("a permission fault fell back instead of refusing")
	}
	if builds != 0 {
		t.Errorf("a permission fault opened the store directly (%d builds)", builds)
	}

	// (d) a reachable daemon → the socket door, and the fallback line never prints.
	builds = 0
	errOut.Reset()
	toolsDial = prevDial
	toolsDial = func(int) (*protocol.Client, error) {
		c := protocol.NewClient(parityPipeServer(t, toolsRoadTable(t)), "select-test", "0")
		if _, err := c.Connect(0); err != nil {
			return nil, err
		}
		return c, nil
	}
	t.Cleanup(func() { toolsDial = prevDial })
	sock, err := selectToolsDoor(t.Context(), &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sock.(socketToolsDoor); !ok {
		t.Errorf("a reachable daemon did not select the socket door: %T", sock)
	}
	if builds != 0 || errOut.Len() != 0 {
		t.Errorf("the socket road announced a fallback it did not take: %d builds, %q", builds, errOut.String())
	}
}

func TestSurfaceOfflineRefusesRatherThanList(t *testing.T) {
	// The decision the spec left to T0/T6, pinned: no daemon is a REFUSAL, never
	// an empty listing. An offline listing would read as "this daemon exposes
	// nothing", the most confident wrong answer this verb can produce.
	prevDial := toolsDial
	toolsDial = func(int) (*protocol.Client, error) { return nil, noDaemonErrForTest() }
	t.Cleanup(func() { toolsDial = prevDial })
	var out bytes.Buffer
	err := cmdSurface(&out, []string{"list"})
	if err == nil {
		t.Fatal("`aeon surface list` answered with no daemon — the offline listing is the failure this forbids")
	}
	if !strings.Contains(err.Error(), "surface needs the running daemon") {
		t.Errorf("the refusal did not say why: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the refusal printed a listing anyway: %q", out.String())
	}
}

// --- the parser's view (TC10: the CLI computes nothing) ----------------------

// parseToolsFiles parses the two production files of this verb family.
func parseToolsFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, name := range []string{"tools_door.go", "tools_cmd.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		f, perr := parser.ParseFile(fset, name, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		out[name] = f
	}
	return out
}

func TestDirectDoorUsesTheSamePackages(t *testing.T) {
	files := parseToolsFiles(t)

	// (1) The printer file imports NOTHING that lets it compute an answer: no
	// store, no catalog engine, no seed engine, no daemon. It speaks to the door
	// and to `internal/protocol`'s types, and that is the whole of it.
	cmdImports := importedPaths(files["tools_cmd.go"])
	for _, banned := range []string{
		"github.com/cjlapao/aeon/internal/store",
		"github.com/cjlapao/aeon/internal/toolcatalog",
		"github.com/cjlapao/aeon/internal/seed",
		"github.com/cjlapao/aeon/internal/daemon",
		"github.com/cjlapao/aeon/internal/toolseed",
		"github.com/cjlapao/aeon/internal/tools",
	} {
		if cmdImports[banned] {
			t.Errorf("tools_cmd.go imports %s — the state, reason and drift it prints would then be the CLI's own (TC10)", banned)
		}
	}

	// (2) The door file builds the adapter through the production constructors and
	// nothing else: the store through the ONE hook-bearing constructor, the seed
	// engine and MCP admin through the entry point's own functions, the catalog
	// through daemon.NewToolsCatalogService.
	doorImports := importedPaths(files["tools_door.go"])
	for _, want := range []string{
		"github.com/cjlapao/aeon/internal/store",
		"github.com/cjlapao/aeon/internal/daemon",
		"github.com/cjlapao/aeon/internal/seed",
	} {
		if !doorImports[want] {
			t.Errorf("tools_door.go no longer imports %s — the documented direct door is gone", want)
		}
	}
	calls := selectorCalls(files["tools_door.go"])
	for _, want := range []string{
		"store.Open", "daemon.NewToolsCatalogService", "daemon.NewSeedEngine", "mcpAdminHandleFor", "newMCPProbeCache", "storeOpenOptions", "mcpSeedValidator",
	} {
		if !calls[want] {
			t.Errorf("tools_door.go does not call %s — the direct door must reach the store through the entry point's own constructors, not a copy of their wiring", want)
		}
	}
	// (3) Each direct-door method forwards to exactly one seam method: a door that
	// computes something locally instead of asking is how a second truth appears.
	wantSeam := map[string]string{
		"list": "ToolsList", "get": "ToolsGet", "agent": "ToolsByAgent", "status": "ToolsStatus",
	}
	for _, fd := range funcDecls(files["tools_door.go"]) {
		if len(fd.Recv.List) == 0 {
			continue
		}
		rec := fd.Recv.List[0]
		recvName := receiverIdent(rec.Type)
		if recvName != "directToolsDoor" {
			continue
		}
		if len(rec.Names) == 0 {
			continue
		}
		var seam []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if inner, ok := sel.X.(*ast.SelectorExpr); ok {
				if id, ok := inner.X.(*ast.Ident); ok && id.Name == "d" && inner.Sel.Name == "svc" {
					seam = append(seam, sel.Sel.Name)
				}
			}
			return true
		})
		verb := fd.Name.Name
		if len(seam) != 1 || seam[0] != wantSeam[verb] {
			t.Errorf("directToolsDoor.%s forwards to %v, want exactly [%s] — the door asks the adapter, it does not answer", verb, seam, wantSeam[verb])
		}
	}
	// (4) No CLI file outside the door reaches for the catalog engine or a store.
	bad := []string{}
	matches, gerr := filepath.Glob("*.go")
	if gerr != nil {
		t.Fatal(gerr)
	}
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") || name == "tools_door.go" {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		f, perr := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for path := range importedPaths(f) {
			if path == "github.com/cjlapao/aeon/internal/toolcatalog" || path == "github.com/cjlapao/aeon/internal/toolseed" {
				bad = append(bad, name+": "+path)
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("the catalog engine is imported outside the direct door (a second implementation of the answer): %v", bad)
	}
}

// TestNoWriteVerbExists is TC1 made executable at the CLI: the verbs of this
// family speak only the six registered reads, carry no `--yes`, and no verb name
// suggests a change.
func TestNoWriteVerbExists(t *testing.T) {
	files := parseToolsFiles(t)
	allowed := map[string]bool{
		"MethodToolsList": true, "MethodToolsGet": true, "MethodToolsByAgent": true, "MethodToolsStatus": true,
		"MethodSurfaceList": true, "MethodSurfaceGet": true,
	}
	for name, f := range files {
		seen := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "protocol" && strings.HasPrefix(sel.Sel.Name, "Method") {
				if sel.Sel.Name == "MethodName" {
					return true // the TYPE, not a method constant
				}
				seen[sel.Sel.Name] = true
			}
			return true
		})
		for m := range seen {
			if !allowed[m] {
				t.Errorf("%s speaks protocol.%s — this slice ships reads only (TC1), and a write verb here is a door the spec never opened", name, m)
			}
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), `"yes"`) {
			t.Errorf("%s carries a --yes flag — nothing here needs confirming, because nothing here changes anything", name)
		}
	}
	for _, c := range toolsVerbTable {
		for _, banned := range []string{"add", "remove", "set", "apply", "delete", "update", "grant", "revoke", "seed", "repair", "sync"} {
			if strings.Contains(strings.ToLower(c.verb), banned) {
				t.Errorf("the verb %q reads like a write: TC1 ships no write tool, method or verb", c.verb)
			}
		}
	}
}

// TestHandlersListUnchanged pins the neighbour this slice must not have disturbed:
// `aeon handlers list` still answers the registry's names, one per line, with the
// same three-code contract.
func TestHandlersListUnchanged(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdHandlers(&out, &errOut, []string{"list"})
	if code != 0 {
		t.Fatalf("handlers list exited %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 5 {
		t.Fatalf("handlers list printed %d lines — the registry has not shrunk to nothing:\n%s", len(lines), out.String())
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] < lines[i-1] {
			t.Fatalf("handlers list is no longer sorted: %q after %q", lines[i], lines[i-1])
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("handlers list wrote to stderr: %q", errOut.String())
	}
}

// --- ast helpers -------------------------------------------------------------

func importedPaths(f *ast.File) map[string]bool {
	out := map[string]bool{}
	for _, imp := range f.Imports {
		out[strings.Trim(imp.Path.Value, `"`)] = true
	}
	return out
}

func funcDecls(f *ast.File) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && len(fd.Recv.List) > 0 {
			out = append(out, fd)
		}
	}
	return out
}

func receiverIdent(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// selectorCalls collects `pkg.Func` and `x.Method` call names from a file.
func selectorCalls(f *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fn.X.(*ast.Ident); ok {
				out[id.Name+"."+fn.Sel.Name] = true
			}
		case *ast.Ident:
			// A same-package constructor (storeOpenOptions, mcpAdminHandleFor,
			// mcpSeedValidator) is a bare call — and those are exactly the ones
			// this guard is about: reaching the entry point's OWN wiring
			// functions instead of a copy of them.
			out[fn.Name] = true
		}
		return true
	})
	return out
}
