package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/tools"
)

// T9: the adoption story, as a test (MC11). "An agent with no tools is
// valid, and the whole suite passes with this layer switched off" — where
// switched off is not a flag (scope 5: a flag is a second state to test and
// a thing that gets set wrong) but the ordinary state of a machine that
// never wrote an MCP config.

const importPath = `"github.com/cjlapao/aeon/internal/mcp"`

// moduleFiles walks the aeon module (the package directory is
// aeon/internal/mcp, so ../.. is the module root) and yields every .go
// file's path and contents, skipping testdata.
func moduleFiles(t *testing.T) [][2]string {
	t.Helper()
	var out [][2]string
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "testdata" || base == ".git" || base == ".dev" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, [2]string{path, string(src)})
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir %s: %v", root, err)
	}
	if len(out) < 100 {
		t.Fatalf("walked %d Go files — the module root is wrong or the walk is blind", len(out))
	}
	return out
}

// TestNoUnconditionalImportOfMCP is scope 3's grep. The seam rule:
// **cmd/aeon may import the layer (that is the seam — the CLI verbs); no
// runtime-side package may import it at all.** The runtime importing MCP
// unconditionally is how "optional" quietly stops being true: one stray
// `var _ = mcp.X` in the loop and deleting the layer breaks the daemon.
func TestNoUnconditionalImportOfMCP(t *testing.T) {
	importers := map[string]bool{}
	for _, f := range moduleFiles(t) {
		path, src := f[0], f[1]
		if !strings.Contains(src, importPath) {
			continue
		}
		// Normalise: aeon/internal/mcp/*.go may mention its own package in
		// doc comments, not imports.
		rel := strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "../../")
		if strings.HasPrefix(rel, "internal/mcp/") {
			continue
		}
		importers[rel] = true
	}
	for path := range importers {
		if !strings.HasPrefix(path, "cmd/aeon/") {
			t.Errorf("%s imports internal/mcp — the runtime must not know the layer exists (MC11: deletability decays one convenience import at a time)", path)
		}
	}
	// Positive control: cmd/aeon/mcp.go MUST be an importer, or this test
	// certifies that the seam was deleted while the CLI still claims to
	// exist (T1's lesson: a grep that finds nothing because nothing exists
	// to find is the dangerous silence).
	if !importers["cmd/aeon/mcp.go"] {
		t.Error("cmd/aeon/mcp.go does not import the layer — where did the CLI go?")
	}
	// And the named-forbidden list, said out loud (the task's own phrasing):
	for _, banned := range []string{"internal/runtime", "internal/tools", "internal/model", "internal/policy", "internal/daemon", "internal/loop", "internal/protocol"} {
		for path := range importers {
			if strings.HasPrefix(path, banned) {
				t.Errorf("%s imports internal/mcp — explicitly forbidden territory", path)
			}
		}
	}
}

// TestNoFeatureFlagWasAdded is scope 5's grep: the layer's switch is the
// presence of the config file. A boolean env/flag is a second state to
// test and a thing that gets set wrong — and a flag that disables would
// also be a flag that ENABLES, which is a door.
func TestNoFeatureFlagWasAdded(t *testing.T) {
	banned := []string{
		"AEON_MCP_ENABLED", "AEON_MCP_DISABLE", "AEON_ENABLE_MCP",
		"mcp.enabled", "mcpDisabled", "MCPEnabled", "featureFlag",
	}
	for _, f := range moduleFiles(t) {
		path, src := f[0], f[1]
		if strings.HasSuffix(path, "_test.go") {
			continue // a test may name a ban to ban it (this file)
		}
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Errorf("%s mentions %q — MCP's flag is the config file's existence (scope 5: deletability means absent works, not that a switch exists)", path, b)
			}
		}
	}
	// The config-file-existence mechanism must actually be present —
	// AEON_MCP_CONFIG is the override name paths.go advertises.
	pathsSrc, err := os.ReadFile(filepath.Join("..", "paths", "paths.go"))
	if err != nil {
		t.Fatalf("ReadFile paths.go: %v", err)
	}
	if !strings.Contains(string(pathsSrc), "AEON_MCP_CONFIG") {
		t.Error("paths.go no longer honours AEON_MCP_CONFIG — the override is the config's only switch, and it is the file's own absence")
	}
}

// TestSuitePassesWithNoMCPConfig is the claim as a unit test of this
// package's own hygiene: no test in this package may consult a machine's
// real configuration — every fixture arrives as a literal or a t.TempDir()
// file, so "everything else passes with no MCP config" is not just about
// this package's behaviour but about its demands. (The whole-suite run
// itself is the measured evidence in the task notes: `go test ./...` with
// AEON_MCP_CONFIG pointed at a nonexistent path and HOME at a temp dir.)
func TestSuitePassesWithNoMCPConfig(t *testing.T) {
	// The environment this package must tolerate: no config anywhere.
	t.Setenv("AEON_MCP_CONFIG", filepath.Join(t.TempDir(), "definitely-not-here.json"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "no-xdg-either"))

	path, err := paths.MCPConfigFile()
	if err != nil {
		t.Fatalf("MCPConfigFile: %v", err)
	}
	_, err = LoadConfigFile(path)
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("absent configuration answered %v, want ErrNoConfig — absent is a state, not an error", err)
	}
	// And the registry built from NOTHING: zero config, zero complaints,
	// zero MCP tools.
	r, err := New(Config{})
	if err != nil {
		t.Fatalf("New(Config{}) — the level-0 state must load: %v", err)
	}
	// The core capability set, asserted against the registry as it is —
	// never against a remembered count (AGENTS §4a: measure, don't
	// recollect; and the DEMO's "core nine" wording is the WALK's to test
	// in T10, not this test's to hard-code). What this asserts: exactly
	// tools.Registry() minus the hidden write_file alias, nothing else.
	wantCore := map[string]bool{}
	for name := range tools.Registry() {
		if name != aliasWriteFile {
			wantCore[name] = true
		}
	}
	got := r.ToolsFor("any-agent")
	if len(got) != len(wantCore) {
		names := make([]string, 0, len(got))
		for _, tl := range got {
			names = append(names, tl.Name)
		}
		t.Fatalf("unconfigured agent got %d tools %v, want the %d registered core tools", len(got), names, len(wantCore))
	}
	for _, tl := range got {
		if !wantCore[tl.Name] {
			t.Errorf("unconfigured agent has %q — not in tools.Registry()", tl.Name)
		}
		if tl.Tier != TierCore {
			t.Errorf("%q tier %s, want core", tl.Name, tl.Tier)
		}
	}
	if got := r.DownServers(); len(got) != 0 {
		t.Errorf("DownServers with no configuration = %v — a name nobody configured must never be mourned", got)
	}
}

// TestAgentWithNoToolsRunsAStep is the runtime claim's mcp-side half. The
// runtime half is asserted by TestNoUnconditionalImportOfMCP (the loop does
// not know this package exists, so it cannot require it) plus S5/S8's own
// `TestAStepWithNoToolsKeepsTheRoutesReasoning` in internal/runtime —
// quoted here as the witness, not re-run from the wrong package. What THIS
// test pins is the registry's side of the bargain: an agent that declares
// nothing, in a config that configures nothing MCP, is a complete, valid,
// answerable answer — the step's dispatch needs no registry entry to
// proceed.
func TestAgentWithNoToolsRunsAStep(t *testing.T) {
	r := mustRegistry(t, Config{})

	tools := r.ToolsFor("body-with-nothing")
	if len(tools) == 0 {
		t.Error("an agent with no declarations got no tools at all — the core nine must be there; an empty capability list makes the model think it is mute")
	}
	for _, tl := range tools {
		if tl.Tier != TierCore {
			t.Errorf("an unconfigured agent acquired %q (%s) — where from?", tl.Qualified, tl.Tier)
		}
	}
	// The prompt surface renders from this with zero MCP imports and zero
	// policy consultation.
	specs := r.ToolSpecsFor("body-with-nothing")
	if len(specs) != len(tools) {
		t.Errorf("specs %d != capability tools %d — the prompt and the registry disagree", len(specs), len(tools))
	}
	for _, s := range specs {
		if strings.Contains(s.Description, "untrusted") {
			t.Errorf("core description %q labelled untrusted at level 0", s.Name)
		}
	}
	// And the one thing it can still do: refuse, honestly.
	res, err := r.Call(t.Context(), "body-with-nothing", "reddit.post", nil, nil, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if want := "unknown tool: reddit.post"; res.Content != want {
		t.Errorf("level-0 refusal = %q, want exactly %q", res.Content, want)
	}
}

// TestAbsentIsReportedNotSilentCallPath is scope 4's second half (the CLI
// wording itself belongs to cmd/aeon and is walked in T10): a call for a
// tool of an UNCONFIGURED server is told, in the same bytes a stranger
// gets — the layer does not become chatty about what it does not have.
func TestAbsentIsReportedNotSilentCallPath(t *testing.T) {
	r := mustRegistry(t, Config{})
	res, err := r.Call(t.Context(), "someone", "docs.read", json.RawMessage(`{}`), nil, time.Now())
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if want := "unknown tool: docs.read"; res.Content != want {
		t.Errorf("absent-server call answered %q, want exactly %q (no suggestions, no server-name gossip)", res.Content, want)
	}
}
