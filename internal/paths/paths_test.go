package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreDirHonoursXDGStateHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG is POSIX")
	}
	t.Setenv("XDG_STATE_HOME", "/tmp/aeon-test-state")

	got, err := StoreDir()
	if err != nil {
		t.Fatalf("StoreDir: %v", err)
	}
	if want := "/tmp/aeon-test-state/aeon"; got != want {
		t.Fatalf("StoreDir() = %q, want %q", got, want)
	}
}

// A relative XDG_STATE_HOME resolves against the process's working directory,
// so the store would move depending on how the daemon was launched. The spec
// requires absolute; this asserts we fall back rather than honour it.
func TestStoreDirIgnoresRelativeXDGStateHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG is POSIX")
	}
	t.Setenv("XDG_STATE_HOME", "relative/path")

	got, err := StoreDir()
	if err != nil {
		t.Fatalf("StoreDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("StoreDir() = %q, want an absolute path", got)
	}
	if got == "relative/path/aeon" {
		t.Fatal("a relative XDG_STATE_HOME was honoured; the store would move with the working directory")
	}
}

func TestStoreDirFallsBackWhenXDGUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG is POSIX")
	}
	t.Setenv("XDG_STATE_HOME", "")

	got, err := StoreDir()
	if err != nil {
		t.Fatalf("StoreDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("StoreDir() = %q, want an absolute path", got)
	}
	if filepath.Base(got) != "aeon" {
		t.Fatalf("StoreDir() = %q, want it to end in aeon", got)
	}
}

func TestStoreFileIsUnderStoreDir(t *testing.T) {
	dir, err := StoreDir()
	if err != nil {
		t.Fatalf("StoreDir: %v", err)
	}
	file, err := StoreFile()
	if err != nil {
		t.Fatalf("StoreFile: %v", err)
	}
	if filepath.Dir(file) != dir {
		t.Fatalf("StoreFile() = %q, not under StoreDir() = %q", file, dir)
	}
	if filepath.Base(file) != "state.db" {
		t.Fatalf("StoreFile() = %q, want it to end in state.db", file)
	}
}

// ── MCP configuration (S12) ────────────────────────────────────────────────

// The override sits above the XDG chain, exactly as AEON_STATE_DIR does for
// the store: the QA walk points it at a throwaway file and never reads a
// real operator's configuration.
func TestMCPConfigHonoursOverrideEnv(t *testing.T) {
	t.Setenv("AEON_MCP_CONFIG", "/tmp/aeon-walk/mcp.json")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/aeon-walk-config")

	got, err := MCPConfigFile()
	if err != nil {
		t.Fatalf("MCPConfigFile: %v", err)
	}
	if want := "/tmp/aeon-walk/mcp.json"; got != want {
		t.Fatalf("MCPConfigFile() = %q, want %q", got, want)
	}
}

func TestMCPConfigIgnoresRelativeOverride(t *testing.T) {
	t.Setenv("AEON_MCP_CONFIG", "relative/mcp.json")

	got, err := MCPConfigFile()
	if err != nil {
		t.Fatalf("MCPConfigFile: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("MCPConfigFile() = %q, want an absolute path", got)
	}
	if got == "relative/mcp.json" {
		t.Fatal("a relative AEON_MCP_CONFIG was honoured; the config would move with the working directory")
	}
}

func TestMCPConfigHonoursXDGConfigHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG is POSIX")
	}
	t.Setenv("AEON_MCP_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/aeon-walk-config")

	got, err := MCPConfigFile()
	if err != nil {
		t.Fatalf("MCPConfigFile: %v", err)
	}
	if want := "/tmp/aeon-walk-config/aeon/mcp.json"; got != want {
		t.Fatalf("MCPConfigFile() = %q, want %q", got, want)
	}
}

// The reported path is reported whether or not the file exists: MC11's
// level-0 state is an ANSWER ("no MCP configuration"), never a failure to
// name where it would have been.
func TestMCPConfigFileResolvesWithoutFile(t *testing.T) {
	dir := t.TempDir()
	got, err := MCPConfigFileFor(func(k string) string {
		if k == MCPConfigEnv {
			return filepath.Join(dir, "nowhere", "mcp.json")
		}
		return ""
	})
	if err != nil {
		t.Fatalf("MCPConfigFileFor: %v", err)
	}
	if got != filepath.Join(dir, "nowhere", "mcp.json") {
		t.Fatalf("got %q", got)
	}
}
