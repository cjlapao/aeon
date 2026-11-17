package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// The framework's `Store` lifecycle contract, implemented.
//
//	Name() · Init(ctx, *gorm.DB) · Health(ctx) · IsEnabled() · Dependencies() · Migrate()
//
// # Why Aeon implements it rather than ignoring it
//
// The framework's database service initialises the stores it manages through
// this contract, and it REFUSES TO START with none installed:
//
//	[InitializeWithDB] ERROR database stores not configured —
//	call ConfigureStores before init
//
// Found by running it, after registering the service and nothing else. It is
// the right refusal: a database service managing no stores is a connection
// nobody asked for.
//
// So Aeon's store is the store the framework manages. The methods are
// deliberately thin, because the work has already happened by the time they
// are called — `store.Open` migrated the schema before `bootstrap.Register`
// handed the handle over. What this contract buys is the framework's schema
// lock around the store phase and a health probe the orchestrator can ask.
//
// # The two migration services on one handle, and why that is not a conflict
//
// Aeon runs its own `migrations.MigrationService` inside `Open` (migrate.go).
// The framework builds a second one in `InitializeWithDB` and calls `RunAll` on
// it. They share the handle and the `_migrations` table, and the framework's
// has NO WORKERS REGISTERED — so its run is a clean no-op that ensures the
// ledger table exists, which it already does.
//
// That is deliberate rather than accidental. Registering Aeon's migrations on
// the framework's instance would mean the schema existed only when the whole
// framework had booted — and every CLI verb, every test and the cold-resume
// child process open the store WITHOUT booting it. One code path that always
// runs beats two that each run half the time.
//
// There is no interface assertion here: the contract is satisfied
// structurally, and `internal/bootstrap` is where the compiler checks it (it
// has to name the type to install the provider). That keeps the framework's
// database interfaces out of this package, which imports `internal/model` and
// nothing else under `internal/` (I1).

// Name is the store's identity in the framework's registry.
//
// One store, named for the application: Aeon's tables are one schema with one
// migration ledger, and splitting them into a store per table would mean a
// migration order spread across several registrations.
func (s *Store) Name() string { return "aeon" }

// Init adopts the connection the database service hands over.
//
// It is the SAME handle this store opened and passed in through
// `database.WithDB`, so the normal case is a no-op with an assertion. The
// assertion is the point: if the framework ever hands over a different
// connection, the store would quietly be reading one database and writing
// another, and nothing else would notice.
func (s *Store) Init(_ context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("store: the database service handed over a nil connection")
	}
	if s.gw != nil && db != s.gw {
		return fmt.Errorf("store: the database service handed over a different connection than %s was "+
			"opened on; the store would read one database and write another", s.path)
	}
	s.gw = db
	return nil
}

// Health pings the store, for the orchestrator's probe.
//
// Both pools, because an unreachable READER is a daemon whose every read fails
// while every write succeeds — and a probe that only checked the writer would
// report that as healthy.
func (s *Store) Health(ctx context.Context) error {
	if s.w == nil || s.r == nil || s.Closed() {
		return fmt.Errorf("store: %s is closed", s.path)
	}
	if err := s.w.PingContext(ctx); err != nil {
		return fmt.Errorf("store: the writer pool of %s is unreachable: %w", s.path, err)
	}
	if err := s.r.PingContext(ctx); err != nil {
		return fmt.Errorf("store: the reader pool of %s is unreachable: %w", s.path, err)
	}
	return nil
}

// IsEnabled is always true.
//
// A store that exists is a store that is wanted: it was opened before the
// service was registered, and the decision not to have one is made by not
// opening it. `Pins()` forces the database service on for the same reason — a
// daemon that cannot persist a step cannot suspend, resume or recover.
func (s *Store) IsEnabled() bool { return true }

// Dependencies declares the stores that must initialise first. There is one
// store, so there are none.
func (s *Store) Dependencies() []string { return nil }
