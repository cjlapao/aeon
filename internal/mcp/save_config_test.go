package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func validConfig() Config {
	return Config{
		Public: []ServerConfig{
			{Name: "github", Transport: TransportConfig{Type: TransportStdio, Command: "gh-mcp"}},
		},
	}
}

func TestSaveConfigAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := []byte(`{"public":[{"name":"keepme","transport":{"type":"stdio","command":"keep-mcp"}}],"agents":{}}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// A successful write is readable immediately after.
	if err := SaveConfig(path, validConfig()); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got.Public) != 1 || got.Public[0].Name != "github" {
		t.Fatalf("read back %+v, want the just-saved config", got)
	}

	// No leftover temp files — the rename either happened or the temp file
	// would still be sitting beside the real one.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("a temp file survived: %s", e.Name())
		}
	}
}

func TestSaveConfigValidatesFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	original := []byte(`{"public":[{"name":"keepme","transport":{"type":"stdio","command":"keep-mcp"}}],"agents":{}}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	bad := Config{
		Public: []ServerConfig{
			{Name: "notanamespace!", Transport: TransportConfig{Type: TransportStdio, Command: "x"}},
		},
	}
	err := SaveConfig(path, bad)
	if err == nil {
		t.Fatal("an invalid config must be refused, writing nothing")
	}

	// Nothing was written — the original file is byte-identical.
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(raw) != string(original) {
		t.Fatalf("the file changed despite a validation failure:\nwant %s\ngot  %s", original, raw)
	}
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("a temp file was left behind by a refused write: %s", e.Name())
		}
	}
}

func TestSaveConfigConcurrentWritesSerialize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cfg := Config{Public: []ServerConfig{
				{Name: "server", Transport: TransportConfig{Type: TransportStdio, Command: "cmd" + string(rune('a'+n%26))}},
			}}
			if err := SaveConfig(path, cfg); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent SaveConfig returned an error: %v", err)
	}

	// The file is readable and well-formed — a torn write would fail to
	// parse at all; last-write-wins is fine (the test doesn't assert WHICH
	// write landed, only that landing never corrupts).
	got, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("the file is corrupted after concurrent writes: %v", err)
	}
	if len(got.Public) != 1 || got.Public[0].Name != "server" {
		t.Fatalf("read back %+v, want exactly one well-formed server row", got)
	}
}
