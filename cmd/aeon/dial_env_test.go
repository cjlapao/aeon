package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDialDoesNotPinDatabaseDefaults(t *testing.T) {
	keys := []string{"LOCALRPC_APP_NAMESPACE", "DATABASE_TYPE", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME", "DATABASE_SQLITE_DSN"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "missing.sock"))
	client, err := dial(0)
	if client != nil {
		_ = client.Close()
	}
	if err == nil {
		t.Fatal("unexpected daemon at a missing fixture endpoint")
	}
	for _, key := range keys {
		if got := os.Getenv(key); got != "" {
			t.Errorf("dial changed %s to %q", key, got)
		}
	}
}
