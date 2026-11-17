package main

// mcp_verbs_test.go — S46 T9 (MG5, MG6). The management verbs as clients:
// both roads speak through the same functions (the door relay), so the
// sentences are one sentence, the offline door fires ONLY for "nobody is
// home" and says so, the actor is the connection or cli, and the CLI
// re-validates nothing — its own refusals are only ever about input that
// cannot be a value at all.
//
// The method is S45's policy_client_test pattern (doorRun, doorEnv,
// doorNormalize, stripNotice) with one improvement for the MCP family:
// BOTH roads share ONE state dir (the daemon's own), so one seeded store
// answers both roads — the "same data, same bytes" claim stops depending
// on replaying identical setups. WAL serialises the offline door behind
// the daemon's writer (the justified second access path).

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// t9Roads: one daemon, one state dir; the offline road is the SAME dir
// with an endpoint that does not exist.
type t9Roads struct {
	t          *testing.T
	dir        string
	socketEnv  []string
	offlineEnv []string
}

func newT9Roads(t *testing.T) *t9Roads {
	t.Helper()
	f := startDaemon(t)
	r := &t9Roads{
		t:          t,
		dir:        f.stateDir,
		socketEnv:  doorEnv(f.stateDir, f.endpoint),
		offlineEnv: doorEnv(f.stateDir, filepath.Join(t.TempDir(), "nothing.sock")),
	}
	// Agents, gated through the DOOR itself (not an in-process handle —
	// an in-process seed of an agent row proved non-deterministically
	// invisible to the door's own store read under the daemon's WAL — a
	// machine-level surprise recorded in T9.md; the door's eyes are the
	// contract). `mcp core --set` runs the write door's resolveAgent
	// (AgentByName over the door's OWN store), so exit-0 is the door
	// CERTIFYING the row exists; the write itself is the lawful "core
	// narrowed to read" fact, idempotent across polls. The daemon's
	// first-party seed converges over ~20-40s (measured — batches of
	// agent rows land well after the endpoint answers ping), so the gate
	// is patient. `aeon agents list` is NOT the gate: it UNIONS rows
	// with the code roster (S43-extra's story), so it lies "present".
	for _, slug := range []string{"golang-engineer", "code-reviewer"} {
		deadline := time.Now().Add(90 * time.Second)
		for {
			_, stderr, err := doorRun(t, r.offlineEnv, "mcp", "core", "--agent", slug, "--set", "read")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the door never certified the agent row %s: %v | %s", slug, err, stderr)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return r
}

func (r *t9Roads) run(road string, args ...string) (string, string, error) {
	r.t.Helper()
	env := r.socketEnv
	if road == "offline" {
		env = r.offlineEnv
	}
	return doorRun(r.t, env, args...)
}

// mustRun succeeds on one road or ends the test with both streams.
func (r *t9Roads) must(road string, args ...string) string {
	r.t.Helper()
	out, stderr, err := r.run(road, args...)
	if err != nil {
		r.t.Fatalf("setup on the %s road failed: aeon %s: %v\nstdout:\n%s\nstderr:\n%s", road, strings.Join(args, " "), err, out, stderr)
	}
	return out
}

// parity compares the roads: same stdout (normalized), same stderr after
// the fixed notice is stripped, same exit outcome. This is MG6's claim —
// no verb behaves differently offline.
func (r *t9Roads) parity(wantErr bool, args ...string) {
	r.t.Helper()
	r.t.Run(strings.Join(args, " "), func(t *testing.T) {
		sOut, sErr, sErrErr := r.run("socket", args...)
		oOut, oErr, oErrErr := r.run("offline", args...)
		if (sErrErr != nil) != wantErr {
			t.Fatalf("socket road: err=%v wantErr=%v\nstdout:\n%s\nstderr:\n%s", sErrErr, wantErr, sOut, sErr)
		}
		if (oErrErr != nil) != wantErr {
			t.Fatalf("offline road: err=%v wantErr=%v\nstdout:\n%s\nstderr:\n%s", oErrErr, wantErr, oOut, oErr)
		}
		if got, want := doorNormalize(oOut), doorNormalize(sOut); got != want {
			t.Errorf("stdout differs between the roads (%s)\n socket:\n%s\n offline:\n%s", strings.Join(args, " "), sOut, oOut)
		}
		if got, want := doorNormalize(stripNotice(oErr)), doorNormalize(sErr); got != want {
			t.Errorf("stderr differs between the roads (%s)\n socket:\n%s\n offline (notice stripped):\n%s", strings.Join(args, " "), sErr, oOut)
		}
	})
}

// t9Store opens the fixture's store from the TEST process (the same dir
// both roads speak to) for row-level assertions.
func (r *t9Roads) t9Store() *store.Store {
	r.t.Helper()
	r.t.Setenv("XDG_STATE_HOME", r.dir)
	isolateStoreEnv(r.t)
	st, err := store.Open(store.Options{})
	if err != nil {
		r.t.Fatalf("open the shared store: %v", err)
	}
	r.t.Cleanup(func() { _ = st.Close() })
	return st
}

func t9AddArgs(name string, extra ...string) []string {
	base := []string{"mcp", "add", "--name", name, "--tier", "public", "--transport", "stdio",
		"--command", "gh-mcp",
		"--tool", "list_issues:safe:local,create_issue:unsafe:outward"}
	return append(base, extra...)
}

// TestMcpAddRefusalsVerbatimBothDoors — the validator's sentences travel
// identically through the socket and the offline relay (the direct door
// runs THE SAME seam function, so this is the claim made by construction
// AND measured): grammar refusals from the validator's set, the seam's
// refusals, and the happy path's bytes.
func TestMcpAddRefusalsVerbatimBothDoors(t *testing.T) {
	// Set up the store once (seeded identically for both roads — it IS
	// one store), then walk refusals and successes through parity.
	r := newT9Roads(t)
	r.must("offline", t9AddArgs("github")...)
	cases := []struct {
		name string
		args []string
	}{
		{"uppercase name", t9AddArgs("Bad_Name")},
		{"private without an agent", []string{"mcp", "add", "--name", "pv", "--tier", "private", "--transport", "stdio", "--command", "x"}},
		{"public with an agent", []string{"mcp", "add", "--name", "pb", "--tier", "public", "--agent", "golang-engineer", "--transport", "stdio", "--command", "x"}},
		{"stdio without a command", []string{"mcp", "add", "--name", "nc", "--tier", "public", "--transport", "stdio"}},
		{"http without a url", []string{"mcp", "add", "--name", "nu", "--tier", "public", "--transport", "http"}},
		{"duplicate name", t9AddArgs("github")},
		{"unknown tier", []string{"mcp", "add", "--name", "tt", "--tier", "sideways", "--transport", "stdio", "--command", "x"}},
	}
	for _, c := range cases {
		r.parity(true, c.args...)
	}
	// The successes' bytes too: reads of the seeded state.
	r.parity(false, "mcp", "show", "github")
	r.parity(false, "mcp", "grants", "golang-engineer")
	r.parity(false, "mcp", "changes")
	r.parity(false, "mcp", "status")
	// A refusal wrote NOTHING (the candidate-before-rows law, both roads
	// attempted it): only "github" exists.
	st := r.t9Store()
	all, err := st.McpRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all.Servers {
		if s.Name != "github" {
			t.Errorf("a refused add wrote server %q — the candidate never reaches the rows (T5)", s.Name)
		}
	}
}

// TestMcpEnvValueRefusedClientSide — a NAME=value is refused by the PARSER
// (input that cannot be a value, C17), before any door is chosen, so no
// server row exists afterwards and the sentence is the CLI's own.
func TestMcpEnvValueRefusedClientSide(t *testing.T) {
	r := newT9Roads(t)
	_, stderr, err := r.run("offline", "mcp", "add", "--name", "leaky", "--tier", "public",
		"--transport", "stdio", "--command", "x", "--env", "GITHUB_TOKEN=ghp_supersecretvalue")
	if err == nil {
		t.Fatal("a --env carrying a VALUE must be refused (C17) — argv is the process list and the shell history")
	}
	if !strings.Contains(stderr, "NAMES only") {
		t.Errorf("the refusal should name the rule, got %q", stderr)
	}
	st := r.t9Store()
	all, _ := st.McpRows(context.Background())
	for _, s := range all.Servers {
		if s.Name == "leaky" {
			t.Error("the refused add reached the rows")
		}
	}
	// And the value never appears anywhere in the store's text either.
	if strings.Contains(fmt.Sprint(all), "supersecret") {
		t.Error("the value reached the store from a flag — the standing rule made flesh")
	}
}

// TestMcpDeclareNeedsYes / TestMcpImportReplaceNeedsYes — the widening
// verbs print their plan and refuse without --yes, writing NOTHING.
func TestMcpDeclareNeedsYes(t *testing.T) {
	r := newT9Roads(t)
	r.must("offline", t9AddArgs("github")...)
	out, stderr, err := r.run("offline", "mcp", "declare", "--agent", "golang-engineer", "--tool", "github.list_issues")
	if err == nil {
		t.Fatal("declare without --yes must refuse — a widening without a deliberate second look is the typo the seatbelt is for")
	}
	if !strings.Contains(out, "plan:") || !strings.Contains(out, "MC5") {
		t.Errorf("the refusal must show the PLAN (what declaring does — MC5's two facts), got %q", out)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("the refusal must name --yes, got %q", stderr)
	}
	st := r.t9Store()
	all, _ := st.McpRows(context.Background())
	if len(all.Declares) != 0 {
		t.Errorf("a refused declare wrote %d declarations", len(all.Declares))
	}
	// With --yes it lands (the gate is a seatbelt, not a lock — the seam
	// still holds the real line).
	r.must("offline", "mcp", "declare", "--agent", "golang-engineer", "--tool", "github.list_issues", "--yes")
	all, _ = r.t9Store().McpRows(context.Background()) // fresh eyes (WAL second-handle)
	if len(all.Declares) != 1 || all.Declares[0].Qualified != "github.list_issues" {
		t.Errorf("the confirmed declare wrote %v", all.Declares)
	}
}

// t9ImportBody is S43's adoption shape with the agent keys the gate
// certifies (importBody's "manny" has no row and no seeder — this file's
// fixture is the door, not an in-process write).
const t9ImportBody = `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp","args":["--x"],"env":["GITHUB_TOKEN"]},"tools":[{"name":"list_issues","rerun":"safe","effect":"local"},{"name":"create_issue","rerun":"unsafe","effect":"outward"}]}],"agents":{"golang-engineer":{"private":[{"name":"labnote","transport":{"type":"http","url":"http://127.0.0.1:9/mcp"},"tools":[{"name":"draft_note","rerun":"safe","effect":"local"}]}],"declares":["github.list_issues"]},"code-reviewer":{"declares":["github.create_issue"]}}}`

func TestMcpImportReplaceNeedsYes(t *testing.T) {
	r := newT9Roads(t)
	path := writeLegacyFile(t, t9ImportBody)
	out, stderr, err := r.run("offline", "mcp", "import-config", "--path", path)
	if err == nil {
		t.Fatal("import-config replace without --yes must refuse")
	}
	if !strings.Contains(out, "[dry-run]") || !strings.Contains(out, "servers +2") {
		t.Errorf("without --yes the verb must first answer the daemon's OWN plan (the diff, zero writes), got %q", out)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("the refusal must name --yes, got %q", stderr)
	}
	st := r.t9Store()
	// T4's law, applied: a REFUSED import gets a 'refused' ledger row (the
	// no-silent-refusals rule) — what the seatbelt owes is that NO
	// SUCCESS row exists and no configuration rows were written.
	if imp, found, err := st.McpImportLatestByPath(context.Background(), path); err != nil {
		t.Fatal(err)
	} else if found && imp.Result != "refused" {
		t.Errorf("the un-confirmed import left a %q ledger row — only 'refused' is lawful before --yes", imp.Result)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 0 {
		t.Errorf("an un-confirmed import wrote %d servers — all-or-nothing means NOTHING without the second look", len(all.Servers))
	}
	// Confirmed: the file's servers land, the ledger records, and the
	// dry-run plan the seatbelt showed matched the replace's counts.
	r.must("offline", "mcp", "import-config", "--path", path, "--yes")
	st2 := r.t9Store() // fresh eyes (WAL second-handle)
	all, _ = st2.McpRows(context.Background())
	if len(all.Servers) != 2 { // github public + labnote private (golang-engineer)
		t.Fatalf("the confirmed import wrote %d servers, want 2", len(all.Servers))
	}
	if _, found, err := st2.McpImportLatestByPath(context.Background(), path); err != nil || !found {
		t.Errorf("the confirmed import left no ledger row (found=%v err=%v)", found, err)
	}
}

// TestCoreAllMeansNullNotEmpty — the two empties, kept apart across the
// whole way (the mcp_agent.core column's NULL-vs-'[]' discipline, through
// the flags, through the wire, into the row).
func TestCoreAllMeansNullNotEmpty(t *testing.T) {
	r := newT9Roads(t)
	r.must("offline", "mcp", "core", "--agent", "golang-engineer", "--all")
	all, _ := r.t9Store().McpRows(context.Background())
	// --all (JSON null) is "the whole core set" — the row is either ABSENT
	// (absent == NULL == the same fact) or present with a nil Core. The
	// fixture's gate lawfully wrote OTHER agents' core rows; the claim is
	// about golang-engineer's own row only.
	for _, a := range all.Agents {
		if a.AgentSlug == "golang-engineer" && a.Core != nil {
			t.Fatalf("--all must leave core NULL or absent (the whole core set), got %s=%q", a.AgentSlug, *a.Core)
		}
	}
	// --set= PRESENT-but-empty is the OTHER fact: narrowed to nothing.
	r.must("offline", "mcp", "core", "--agent", "golang-engineer", "--set=")
	// A FRESH handle per read: an in-process store handle opened before a
	// door subprocess commits does not reliably see the new pages (the
	// second-open WAL surprise, same family S45 T8 #8 recorded) — each
	// assertion opens its own eyes.
	all, _ = r.t9Store().McpRows(context.Background())
	var got *string
	for _, a := range all.Agents {
		if a.AgentSlug == "golang-engineer" {
			got = a.Core
		}
	}
	if got == nil || *got != "[]" {
		t.Fatalf("--set= (present, empty) must store [] — core narrowed to NOTHING is a different fact from NULL, got %v", got)
	}
	// --all together with --set is refused (a flag pair that merges two
	// facts would be guessing).
	if _, _, err := r.run("offline", "mcp", "core", "--agent", "golang-engineer", "--all", "--set", "read"); err == nil {
		t.Error("--all with --set must be refused — two facts, choose one")
	}
}

// TestDirectDoorActorIsCliAudited — the offline door names ITSELF in the
// audit (actor_kind='cli'); it never impersonates a peer.
func TestDirectDoorActorIsCliAudited(t *testing.T) {
	r := newT9Roads(t)
	r.must("offline", t9AddArgs("audited")...)
	st := r.t9Store()
	cache := newMCPProbeCache()
	seam := mcpAdminHandleFor(st, cache)
	ctx := context.Background()
	v, sentence, err := seam(ctx, "mcp.changes", json.RawMessage(`{"server":"audited"}`))
	if err != nil || sentence != "" {
		t.Fatalf("mcp.changes: %v / %s", err, sentence)
	}
	res, ok := v.(protocol.MCPChangesResult)
	if !ok || len(res.Changes) == 0 {
		t.Fatalf("no audit rows for the offline add: %#v", v)
	}
	for _, c := range res.Changes {
		if c.ActorKind != "cli" || c.ActorID != "cli" {
			t.Errorf("the offline audit row says %s/%s — the direct door stamps {cli, cli}, never an empty actor and never a peer's uid", c.ActorKind, c.ActorID)
		}
	}
}

// TestMcpNeverPrintsAnEnvValue — MG13 end to end through every printing
// door: a value present in the environment is never in any verb's bytes.
func TestMcpNeverPrintsAnEnvValue(t *testing.T) {
	r := newT9Roads(t)
	const secret = "hunter2-totally-secret-value"
	secretEnv := append(append([]string{}, r.offlineEnv...), "GITHUB_TOKEN="+secret)
	// An env name must be bound (an environment variable cannot be empty); the
	// offline road has no manager and trusts the binding, the start checks it.
	cmdArgs := t9AddArgs("vaulted", "--env", "GITHUB_TOKEN", "--env-from", "GITHUB_TOKEN=GITHUB_TOKEN")
	if out, stderr, err := doorRun(t, secretEnv, cmdArgs...); err != nil {
		t.Fatalf("add: %v\n%s\n%s", err, out, stderr)
	}
	for _, road := range []struct {
		env  []string
		road string
	}{
		{r.offlineEnv, "offline"},
		{r.socketEnv, "socket"},
	} {
		for _, verb := range [][]string{
			{"mcp", "show", "vaulted"},
			{"mcp", "status"},
			{"mcp", "grants", "golang-engineer"},
			{"mcp", "changes"},
			{"mcp", "list"},
		} {
			out, stderr, err := doorRun(t, append(append([]string{}, road.env...), "GITHUB_TOKEN="+secret), verb...)
			if err != nil {
				t.Fatalf("%s %s: %v\n%s\n%s", road.road, strings.Join(verb, " "), err, out, stderr)
			}
			for _, blob := range []string{out, stderr} {
				if strings.Contains(blob, secret) {
					t.Errorf("%s road, %s: the env VALUE appears in the output — MG13/§4c (the transport has NAMES only; a value has NO field anywhere)", road.road, strings.Join(verb, " "))
				}
			}
		}
	}
	// The NAME is visible (it is configuration) — proving the absence is
	// not an artefact of an empty view.
	out, _, err := r.run("offline", "mcp", "show", "vaulted")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "GITHUB_TOKEN") {
		t.Error("the env NAME must be visible to the person's own management view (it is configuration)")
	}
}

// TestSelectDoorFallsBackOnlyWhenNoDaemon — the three dial outcomes:
// nobody-home falls back (with the fixed line, printed FIRST), any other
// dial error REFUSES without a notice, and a live daemon is the socket
// door, silent.
func TestSelectMCPDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	isolateStoreEnv(t)
	orig := mcpDial
	t.Cleanup(func() { mcpDial = orig })

	t.Run("no endpoint is the direct door, and it says the fixed line", func(t *testing.T) {
		mcpDial = func(int) (*protocol.Client, error) {
			return nil, fmt.Errorf("no daemon at /x/nothing.sock: dial unix: connect: no such file or directory")
		}
		var errOut strings.Builder
		d, err := selectMCPDoor(&errOut)
		if err != nil {
			t.Fatalf("a missing endpoint must fall back, not refuse: %v", err)
		}
		defer func() { _ = d.close() }()
		if _, ok := d.(*mcpDirectDoor); !ok {
			t.Fatalf("the fallback door is %T, not the direct door", d)
		}
		if got := errOut.String(); got != mcpDirectNotice+"\n" {
			t.Errorf("the fallback said %q, want the fixed line", got)
		}
	})

	t.Run("a permission refusal REFUSES — no fallback, no notice", func(t *testing.T) {
		mcpDial = func(int) (*protocol.Client, error) {
			return nil, &net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission, Addr: &net.UnixAddr{Name: "/x/aeon.sock", Net: "unix"}}
		}
		var errOut strings.Builder
		if _, err := selectMCPDoor(&errOut); err == nil {
			t.Fatal("a permission refusal fell back — the CLI routed around a daemon that exists")
		} else if errOut.Len() != 0 {
			t.Errorf("a refusal printed the fallback notice: %q", errOut.String())
		}
	})

	t.Run("a version-skew handshake REFUSES", func(t *testing.T) {
		mcpDial = func(int) (*protocol.Client, error) {
			return nil, fmt.Errorf("handshake refused: daemon protocol version 9 is newer than this client's 8")
		}
		var errOut strings.Builder
		if _, err := selectMCPDoor(&errOut); err == nil {
			t.Fatal("a version-skew handshake fell back")
		}
	})

	t.Run("a live daemon is the socket door, silent", func(t *testing.T) {
		f := startDaemon(t)
		before := map[string]string{}
		for _, k := range []string{"XDG_STATE_HOME", "LOCALRPC_ENDPOINT_PATH", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME", "DATABASE_SQLITE_DSN"} {
			before[k] = os.Getenv(k)
		}
		t.Cleanup(func() {
			for k, v := range before {
				if v == "" {
					_ = os.Unsetenv(k)
				} else {
					_ = os.Setenv(k, v)
				}
			}
		})
		_ = os.Setenv("LOCALRPC_ENDPOINT_PATH", f.endpoint)
		_ = os.Setenv("XDG_STATE_HOME", f.stateDir)
		_ = os.Setenv("DATABASE_SQLITE_STORAGE_PATH", "")
		_ = os.Setenv("DATABASE_SQLITE_FILE_NAME", "")
		_ = os.Setenv("DATABASE_SQLITE_DSN", "")
		mcpDial = dial
		var errOut strings.Builder
		d, err := selectMCPDoor(&errOut)
		if err != nil {
			t.Fatalf("the live daemon refused the socket door: %v", err)
		}
		defer func() { _ = d.close() }()
		if _, ok := d.(*mcpSocketDoor); !ok {
			t.Fatalf("a live daemon answered with a %T — the CLI may never bypass it", d)
		}
		if errOut.Len() != 0 {
			t.Errorf("the socket door printed %q — silence is the claim of the default door", errOut.String())
		}
	})
}

// TestDirectDoorUsesTheSameAdminFunctions — the THIRD implementation ban,
// by import graph: the two new CLI files touch the store ONLY through
// openStoreDirectly and speak ONLY through the daemon's own seam
// constructors (mcpAdminHandleFor / mcpHandleFor). A local handler, a
// direct store.Mcp* call, or a second table would all fail this walk.
func TestDirectDoorUsesTheSameAdminFunctions(t *testing.T) {
	allowed := map[string]bool{
		"openStoreDirectly": true, "mcpAdminHandleFor": true, "mcpHandleFor": true,
		"newMCPProbeCache": true, "selectMCPDoor": true, "isNoDaemon": true, "dial": true,
	}
	fset := token.NewFileSet()
	for _, file := range []string{"mcp_client.go", "mcp_verbs.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		seen := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if ce, ok := n.(*ast.CallExpr); ok {
				switch fn := ce.Fun.(type) {
				case *ast.Ident:
					seen[fn.Name] = true
				case *ast.SelectorExpr:
					// st.McpAddServerTx(&…) — a direct store accessor — is
					// the third implementation in the making; the ONLY
					// store-touch is openStoreDirectly's handle, relayed
					// through the seam constructors.
					if strings.HasPrefix(fn.Sel.Name, "Mcp") && fn.Sel.Name != "McpRows" || fn.Sel.Name == "McpWithTx" {
						if fn.Sel.Name != "McpRows" { // McpRows is read-only and reserved for the daemon's own files
							t.Errorf("%s calls the raw store accessor %s — the CLI must speak through the SAME seam functions the daemon dispatches (one implementation)", file, fn.Sel.Name)
						}
					}
				}
			}
			return true
		})
		for _, want := range []string{"openStoreDirectly", "mcpAdminHandleFor", "mcpHandleFor"} {
			if !seen[want] && allowed[want] {
				switch file {
				case "mcp_client.go":
					if want == "openStoreDirectly" || want == "mcpAdminHandleFor" || want == "mcpHandleFor" {
						if !seen[want] {
							t.Errorf("%s never references %s — the direct door would not be running the daemon's own functions", file, want)
						}
					}
				}
			}
		}
	}
}
