// Package appenv sets the environment defaults Aeon needs before the framework
// reads configuration.
//
// The framework's defaults are the right defaults for a general-purpose
// framework and the wrong ones for Aeon. Where that is true, Aeon supplies its
// own — in code, so it holds on a clean machine with no config file.
//
// Everything here MUST run before system.Initialize: the config service reads
// the environment as it initialises, and a value set afterwards is one it never
// saw. See also internal/secrets, which has the same ordering constraint for a
// different reason.
//
// Every function here RESPECTS AN EXISTING VALUE. An operator who sets one of
// these explicitly always wins; Aeon only fills a gap.
package appenv

import (
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/paths"
)

const (
	// localrpcNamespaceEnv names the application whose endpoint path is resolved.
	localrpcNamespaceEnv = "LOCALRPC_APP_NAMESPACE"

	// The framework's database config. Aeon supplies VALUES here rather than
	// opening its own connection: the framework owns the connector, the config
	// contract and the lifecycle, and Aeon owns the opinion about what the
	// values should be.
	databaseTypeEnv      = "DATABASE_TYPE"
	sqliteStoragePathEnv = "DATABASE_SQLITE_STORAGE_PATH"
	sqliteFileNameEnv    = "DATABASE_SQLITE_FILE_NAME"
)

const (
	// appNamespace is Aeon's slug, matching ApplicationInfo.Slug.
	appNamespace = "aeon"

	// databaseType is the only engine Aeon supports. sqlite-vec is a SQLite
	// extension, so retrieval (S22) has no meaning on any other engine.
	databaseType = "sqlite"

	// storeFileName is the database file inside the storage path.
	storeFileName = "state.db"
)

// Apply fills in Aeon's environment defaults, leaving anything already set
// alone. Safe to call more than once.
func Apply() error {
	if err := setDefault(localrpcNamespaceEnv, appNamespace); err != nil {
		return err
	}
	return applyDatabaseDefaults()
}

// applyDatabaseDefaults points the framework's SQLite config at Aeon's state
// directory.
//
// The framework defaults to StoragePath "." and FileName "database.db" — the
// PROCESS'S CURRENT DIRECTORY. That is a reasonable default for a server
// started from its install directory and a bad one for a daemon: the store
// would move depending on where it was launched from, and `aeon serve` run from
// two different shells would open two different databases without saying so.
//
// Aeon supplies the XDG-correct values through the framework's own config plane
// rather than opening its own connection, so the framework keeps the connector,
// the pool and the lifecycle, and the one thing Aeon knows better — where a
// user's state belongs — is the one thing it overrides.
func applyDatabaseDefaults() error {
	if err := setDefault(databaseTypeEnv, databaseType); err != nil {
		return err
	}
	if err := setDefault(sqliteFileNameEnv, storeFileName); err != nil {
		return err
	}

	dir, err := paths.StoreDir()
	if err != nil {
		// Not fatal: the framework validates its own config and warns when the
		// storage path is unset. Failing the boot here would turn a resolvable
		// home-directory problem into a daemon that will not start.
		return nil //nolint:nilerr // reported by the framework's own validation
	}
	return setDefault(sqliteStoragePathEnv, dir)
}

// setDefault sets key to value only when it is unset or blank.
func setDefault(key, value string) error {
	if strings.TrimSpace(os.Getenv(key)) != "" {
		return nil
	}
	return os.Setenv(key, value)
}
