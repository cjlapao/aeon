package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/vault"
)

// fakeInspectClient stands in for a live server: it answers the handshake facts
// and one tools/list, and records that it was released.
type fakeInspectClient struct {
	tools  []mcp.RemoteTool
	listed error
	closed bool
}

func (f *fakeInspectClient) ServerInfo() mcp.ServerInfo {
	return mcp.ServerInfo{Name: "github", Version: "0.6.2"}
}
func (f *fakeInspectClient) ProtocolVersion() string { return "2025-06-18" }
func (f *fakeInspectClient) ListTools(context.Context) ([]mcp.RemoteTool, error) {
	return f.tools, f.listed
}
func (f *fakeInspectClient) Close() error { f.closed = true; return nil }

func withInspectDial(t *testing.T, dial func(context.Context, mcp.ServerConfig, func(string) string) (mcpInspectClient, error)) {
	t.Helper()
	prev := mcpInspectDial
	mcpInspectDial = dial
	t.Cleanup(func() { mcpInspectDial = prev })
}

func yes() *bool { v := true; return &v }

func TestInspectListsTheServersToolsWithDescriptionsAndStopsIt(t *testing.T) {
	call, st := writeFixture(t)
	fake := &fakeInspectClient{tools: []mcp.RemoteTool{
		{Name: "create_issue", Description: "Create an issue.\nSecond line.", InputSchema: json.RawMessage(`{"type":"object"}`),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: yes()}},
		{Name: "list_repos", Description: "List repositories."},
	}}
	var dialled mcp.ServerConfig
	withInspectDial(t, func(_ context.Context, srv mcp.ServerConfig, _ func(string) string) (mcpInspectClient, error) {
		dialled = srv
		return fake, nil
	})
	out, refusal, err := call("mcp.server.inspect", map[string]any{"transport": map[string]any{
		"type": "stdio", "command": "npx", "args": []string{"-y", "@modelcontextprotocol/server-github"}, "env": []string{"GITHUB_PERSONAL_ACCESS_TOKEN"}}})
	if err != nil || refusal != "" {
		t.Fatalf("inspect: %v %q", err, refusal)
	}
	res := out.(protocol.MCPServerInspectResult)
	if res.ServerName != "github" || res.ProtocolVersion != "2025-06-18" || len(res.Tools) != 2 {
		t.Fatalf("result: %+v", res)
	}
	if res.Tools[0].Description != "Create an issue.\nSecond line." || string(res.Tools[0].InputSchema) != `{"type":"object"}` ||
		res.Tools[0].Annotations == nil || res.Tools[0].Annotations.DestructiveHint == nil || !*res.Tools[0].Annotations.DestructiveHint {
		t.Fatalf("the server's own description, schema and hints travel: %+v", res.Tools[0])
	}
	if dialled.Transport.Command != "npx" || len(dialled.Transport.Args) != 2 || dialled.Transport.Env[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Fatalf("the candidate transport was dialled as typed: %+v", dialled.Transport)
	}
	if !fake.closed {
		t.Fatal("an inspect stops the server it started")
	}
	// Nothing was stored: no server row, no audit row.
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_server`); n != 0 {
		t.Fatalf("inspect stored %d server rows", n)
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_change`); n != 0 {
		t.Fatalf("inspect wrote %d audit rows", n)
	}
}

func TestInspectRefusesWhatItCannotRunInThePersonsOwnWords(t *testing.T) {
	call, _ := writeFixture(t)
	withInspectDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpInspectClient, error) {
		return nil, errors.New(`spawning nope: executable file not found in $PATH`)
	})
	if _, refusal, err := call("mcp.server.inspect", map[string]any{"transport": map[string]any{"type": "stdio", "command": "nope"}}); err != nil ||
		!strings.Contains(refusal, "could not start the server and complete its handshake") || !strings.Contains(refusal, "not found") {
		t.Fatalf("a command that does not start is the person's fact, not a daemon failure: %v %q", err, refusal)
	}
	// The transport grammar is the stored server's grammar: a stdio transport with a url is refused before anything runs.
	ran := false
	withInspectDial(t, func(context.Context, mcp.ServerConfig, func(string) string) (mcpInspectClient, error) {
		ran = true
		return &fakeInspectClient{}, nil
	})
	if _, refusal, _ := call("mcp.server.inspect", map[string]any{"transport": map[string]any{"type": "stdio", "command": "x", "url": "http://x"}}); !strings.HasPrefix(refusal, "refused: stdio transport carries a url") || ran {
		t.Fatalf("validated before running: %q ran=%v", refusal, ran)
	}
	if _, refusal, _ := call("mcp.server.inspect", map[string]any{"transport": map[string]any{"type": "stdio", "command": "x", "env": []string{"A=b"}}}); !strings.Contains(refusal, "not a variable NAME") || ran {
		t.Fatalf("env values never ride the wire: %q", refusal)
	}
}

func TestInspectTimeoutNamesTheNpxCacheAndStillStopsTheServer(t *testing.T) {
	call, _ := writeFixture(t)
	prev := mcpInspectTimeout
	mcpInspectTimeout = 20 * time.Millisecond
	t.Cleanup(func() { mcpInspectTimeout = prev })
	withInspectDial(t, func(ctx context.Context, _ mcp.ServerConfig, _ func(string) string) (mcpInspectClient, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	_, refusal, err := call("mcp.server.inspect", map[string]any{"transport": map[string]any{"type": "stdio", "command": "npx"}})
	if err != nil || !strings.Contains(refusal, "did not start the server and complete its handshake within") || !strings.Contains(refusal, "warm npm's cache") {
		t.Fatalf("timeout: %v %q", err, refusal)
	}
}

func TestInspectIsAHumanDoorThatNeedsAPerson(t *testing.T) {
	_ = writeFixture // the fixture isolates state; this call carries no actor stamp
	isolateStoreEnv(t)
	st := openAgentStore(t)
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	_, _, err := seam(context.Background(), "mcp.server.inspect", mustJSONRaw(t, map[string]any{"transport": map[string]any{"type": "stdio", "command": "x"}}))
	if err == nil || !strings.Contains(err.Error(), "no actor") {
		t.Fatalf("an unattributable inspect is refused before anything runs: %v", err)
	}
}

func TestAServerWithNoToolsIsNotAdded(t *testing.T) {
	call, st := writeFixture(t)
	_, refusal, err := call("mcp.server.add", map[string]any{"name": "github", "tier": "public",
		"transport": map[string]any{"type": "stdio", "command": "npx"}})
	if err != nil || !strings.Contains(refusal, "a server needs at least one tool") || !strings.Contains(refusal, "Inspect") {
		t.Fatalf("no tools, no server: %v %q", err, refusal)
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_server`); n != 0 {
		t.Fatalf("a refused add stored %d rows", n)
	}
	// The same server with one tool is added.
	if _, refusal, err := call("mcp.server.add", addParams("github", "public")); err != nil || refusal != "" {
		t.Fatalf("with a tool: %v %q", err, refusal)
	}
}

// The REAL path: a stdio server process is spawned, speaks initialize and
// tools/list, and is stopped — no fake client in between. The stub is the
// internal/mcp test server, built once for this test.
func TestInspectRunsARealStdioServerEndToEnd(t *testing.T) {
	call, st := writeFixture(t)
	bin := filepath.Join(t.TempDir(), "fakeserver")
	build := exec.Command("go", "build", "-o", bin, "../../internal/mcp/testdata/fakeserver")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stub server: %v\n%s", err, out)
	}
	out, refusal, err := call("mcp.server.inspect", map[string]any{"transport": map[string]any{
		"type": "stdio", "command": bin, "args": []string{"-name", "e2e-server"}}})
	if err != nil || refusal != "" {
		t.Fatalf("inspect: %v %q", err, refusal)
	}
	res := out.(protocol.MCPServerInspectResult)
	if res.ServerName != "e2e-server" || res.ProtocolVersion == "" || len(res.Tools) == 0 {
		t.Fatalf("a real handshake and listing: %+v", res)
	}
	var withDescription int
	for _, tool := range res.Tools {
		if tool.Name == "" {
			t.Fatalf("a tool without a name: %+v", tool)
		}
		if tool.Description != "" {
			withDescription++
		}
	}
	if withDescription == 0 {
		t.Fatalf("the server's descriptions travel: %+v", res.Tools)
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_server`); n != 0 {
		t.Fatalf("inspect stored %d rows", n)
	}
}

// A stored secret reaches a candidate command only when the request asks for it.
func TestInspectGivesTheCommandStoredVariablesOnlyOnRequest(t *testing.T) {
	call, st := writeFixture(t)
	v, err := vault.Open(st, filepath.Join(t.TempDir(), "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	secret := "ghp_stored_secret"
	if _, err := v.Set(context.Background(), vault.SetInput{Name: "GITHUB_TOKEN", Value: &secret}, store.Actor{Kind: "user", ID: "1000"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN", "from-the-daemon-env")
	prevV, prevS := mcpVault, mcpVaultStore
	t.Cleanup(func() { mcpVault, mcpVaultStore = prevV, prevS })
	setMCPVault(v, st)

	var seen string
	withInspectDial(t, func(_ context.Context, _ mcp.ServerConfig, getenv func(string) string) (mcpInspectClient, error) {
		seen = getenv("GITHUB_TOKEN")
		return &fakeInspectClient{tools: []mcp.RemoteTool{{Name: "t"}}}, nil
	})
	transport := map[string]any{"type": "stdio", "command": "npx", "env": []string{"GITHUB_TOKEN"}}

	if _, refusal, err := call("mcp.server.inspect", map[string]any{"transport": transport}); err != nil || refusal != "" {
		t.Fatalf("inspect: %v %q", err, refusal)
	}
	if seen != "from-the-daemon-env" {
		t.Fatalf("by default a candidate command sees the daemon's own environment, never a stored secret: %q", seen)
	}
	if _, refusal, err := call("mcp.server.inspect", map[string]any{"transport": transport, "useVariables": true}); err != nil || refusal != "" {
		t.Fatalf("inspect with variables: %v %q", err, refusal)
	}
	if seen != secret {
		t.Fatalf("on request the stored value is passed: %q", seen)
	}
}
