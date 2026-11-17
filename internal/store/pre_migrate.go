package store

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
)

// The pre-migration hook seam (S62 T5, BK6).
//
// Open calls the hook, if any, after the store is fully open and the
// migration unapplied, with the exact pending set Migrate is about to run.
// Its one production caller is the pre-migration backup: a verified copy of
// the store before the first pending migration's first byte runs, so a bad
// migration is rolled back to a copy, not to a memory.
//
// The seam owns two ordering facts, and only the two:
//
//  1. the hook runs BEFORE Migrate, so its copy captures the pre-migration
//     state — the first pending migration's effect is not yet visible at
//     the hook's moment;
//  2. the hook's RunRecord is appended AFTER Migrate succeeds, because the
//     ledger table the row belongs to may not exist until the very
//     migration about to run creates it.
//
// A hook that returns an error fails the open with its own sentence and
// the migration is refused. A hook whose AfterMigrate fails fails the open
// the same way, after the migration — the copy is made and the schema
// moved, and the open refuses to pretend the prune happened.

// PreMigrateFunc is the callback between the open and the migration.
// pending is exactly what Migrate would apply — the hook never re-derives
// the ledger.
type PreMigrateFunc func(ctx context.Context, s *Store, pending []Migration) (PreMigrateResult, error)

// PreMigrateResult is the hook's answer to Open. Every field is optional;
// a hook that only makes a copy and says nothing returns an empty result.
type PreMigrateResult struct {
	// RunRecord, when non-nil, is appended with RecordBackupRun once
	// Migrate has succeeded. It is the pre-migration run's ledger row,
	// deferred because its table may not exist yet (the seam's fact 2).
	RunRecord *NewBackupRun

	// AfterMigrate, when non-nil, is called after a successful Migrate —
	// never after a failed one. The pre-migration hook prunes old
	// pre-migration copies here, because only a success may prune (BK6).
	AfterMigrate func(ctx context.Context, s *Store) error
}

// runPreMigrateGate is the between-the-open-and-the-migration decision. It
// returns a non-nil result only when the hook actually ran, so the
// aftermath is a single nil check.
//
// The skip cases, each free and each recorded:
//
//   - no hook, or SkipMigrations: a door that migrates nothing is a door
//     the safety net does not guard;
//   - an in-memory DSN: there is no file to copy, so a row naming a
//     non-existent file is a lie the ledger must not hold;
//   - an empty store: nothing applied, nothing to lose (T2's probe creates
//     nothing, and the cost of the safety net on a fresh store is zero);
//   - a current store: no pending migration, nothing to guard.
func runPreMigrateGate(opts Options, cfg resolved, s *Store) (*PreMigrateResult, error) {
	if opts.PreMigrate == nil || opts.SkipMigrations {
		return nil, nil
	}
	if dsnIsMemory(cfg.writerDSN) {
		return nil, nil
	}
	applied, pending, empty, err := s.AppliedNamesAndPending()
	if err != nil {
		return nil, fmt.Errorf("store: the pre-migration probe cannot read the ledger: %w", err)
	}
	_ = applied
	if empty || len(pending) == 0 {
		return nil, nil
	}
	res, herr := opts.PreMigrate(appctx.NewContext(context.Background()), s, pending)
	if herr != nil {
		return nil, fmt.Errorf("store: pre-migration: %w", herr)
	}
	return &res, nil
}

// applyPreMigrateAftermath is the seam's fact 2: the row is written once
// the migration has succeeded, and the prune only after that.
//
// A record failure fails the open, deliberately: the row is owed, and the
// open that migrates and cannot say so is worse than one that refuses. It
// is a one-time event — the next open sees a current store, records
// nothing, and opens cleanly — while the backup file itself stands
// regardless, because the file is the truth and the row is an index.
func applyPreMigrateAftermath(premig *PreMigrateResult, s *Store) error {
	if premig == nil {
		return nil
	}
	if premig.RunRecord != nil {
		if _, err := s.RecordBackupRun(*premig.RunRecord); err != nil {
			return fmt.Errorf("store: record the pre-migration run: %w", err)
		}
	}
	if premig.AfterMigrate != nil {
		if err := premig.AfterMigrate(appctx.NewContext(context.Background()), s); err != nil {
			return fmt.Errorf("store: after the pre-migration prune: %w", err)
		}
	}
	return nil
}

// dsnIsMemory answers whether the writer's DSN names a database with no
// file. The check is over the DSN string, not a probe: at the hook's moment
// the store is open, but a memory database's "path" is a query parameter,
// and reading the file back would be the thing the check exists to skip.
func dsnIsMemory(dsn string) bool {
	base, query, _ := strings.Cut(dsn, "?")
	if base == ":memory:" || base == "file::memory:" {
		return true
	}
	if q, err := url.ParseQuery(query); err == nil && q.Get("mode") == "memory" {
		return true
	}
	return false
}
