package appenv

import (
	"os"
	"strings"
	"testing"
)

// Without this, the endpoint resolves under the FRAMEWORK's namespace —
// observed as /run/user/<uid>/go-backend-scaffolding/rpc.sock. Two apps on one
// host would collide, and the path would name the wrong product.
func TestAppliesAeonNamespaceWhenUnset(t *testing.T) {
	t.Setenv(localrpcNamespaceEnv, "")

	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := os.Getenv(localrpcNamespaceEnv); got != appNamespace {
		t.Fatalf("%s = %q, want %q", localrpcNamespaceEnv, got, appNamespace)
	}
}

func TestRespectsAnExplicitNamespace(t *testing.T) {
	t.Setenv(localrpcNamespaceEnv, "something-the-operator-chose")

	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := os.Getenv(localrpcNamespaceEnv); got != "something-the-operator-chose" {
		t.Fatalf("%s = %q, want it left alone", localrpcNamespaceEnv, got)
	}
}

// A blank value is absence, not a choice. Treating "" as set would leave the
// framework resolving its own namespace while looking configured.
func TestTreatsBlankAsUnset(t *testing.T) {
	t.Setenv(localrpcNamespaceEnv, "   ")

	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := os.Getenv(localrpcNamespaceEnv); got != appNamespace {
		t.Fatalf("%s = %q, want %q", localrpcNamespaceEnv, got, appNamespace)
	}
}

func TestIsIdempotent(t *testing.T) {
	t.Setenv(localrpcNamespaceEnv, "")

	for range 3 {
		if err := Apply(); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	if got := os.Getenv(localrpcNamespaceEnv); got != appNamespace {
		t.Fatalf("%s = %q, want %q", localrpcNamespaceEnv, got, appNamespace)
	}
}

// The namespace lands in a filesystem path, so a value needing escaping would
// produce a socket somewhere unexpected.
func TestNamespaceIsPathSafe(t *testing.T) {
	if strings.ContainsAny(appNamespace, `/\. `) {
		t.Fatalf("appNamespace = %q contains a path separator, dot or space", appNamespace)
	}
}

// ── The framework's database config ───────────────────────────────────────
//
// Aeon supplies values through the framework's config plane; it does not open
// its own connection.

func clearDB(t *testing.T) {
	t.Helper()
	t.Setenv(databaseTypeEnv, "")
	t.Setenv(sqliteFileNameEnv, "")
	t.Setenv(sqliteStoragePathEnv, "")
}

// The framework defaults StoragePath to "." — the process's current directory.
// For a daemon that means `aeon serve` from two different shells opens two
// different databases without saying so.
func TestPointsTheFrameworkAtTheStateDirectory(t *testing.T) {
	clearDB(t)
	t.Setenv("XDG_STATE_HOME", "/tmp/aeon-appenv-test")

	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got, want := os.Getenv(sqliteStoragePathEnv), "/tmp/aeon-appenv-test/aeon"; got != want {
		t.Fatalf("%s = %q, want %q", sqliteStoragePathEnv, got, want)
	}
	if got := os.Getenv(sqliteStoragePathEnv); got == "." {
		t.Fatal("storage path left at the framework default; the store would follow the working directory")
	}
	if got, want := os.Getenv(sqliteFileNameEnv), "state.db"; got != want {
		t.Fatalf("%s = %q, want %q", sqliteFileNameEnv, got, want)
	}
	if got, want := os.Getenv(databaseTypeEnv), "sqlite"; got != want {
		t.Fatalf("%s = %q, want %q", databaseTypeEnv, got, want)
	}
}

func TestRespectsAnExplicitStoragePath(t *testing.T) {
	clearDB(t)
	t.Setenv(sqliteStoragePathEnv, "/somewhere/an/operator/chose")

	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := os.Getenv(sqliteStoragePathEnv); got != "/somewhere/an/operator/chose" {
		t.Fatalf("%s = %q, want it left alone", sqliteStoragePathEnv, got)
	}
}

// sqlite-vec is a SQLite extension, so retrieval (S22) has no meaning on any
// other engine. Aeon must not silently come up on Postgres.
func TestDatabaseTypeIsSQLite(t *testing.T) {
	clearDB(t)
	if err := Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := os.Getenv(databaseTypeEnv); got != "sqlite" {
		t.Fatalf("%s = %q, want sqlite", databaseTypeEnv, got)
	}
}
