package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/go-backend-scaffolding/framework/core/appctx"
	"github.com/cjlapao/go-backend-scaffolding/framework/database/migrations"
	migrationinterfaces "github.com/cjlapao/go-backend-scaffolding/framework/database/migrations/interfaces"
	"gorm.io/gorm"
)

// The migration runner.
//
// # The framework's machinery SUBSUMES Aeon's ledger, rather than sitting
// beside it
//
// T2 asked which it would be, and the answer came from running it. The
// framework ships `migrations.MigrationService`: numbered workers ordered by
// (Order, Version), each applied ONCE, each wrapped in ONE TRANSACTION, each
// recorded in a `_migrations` ledger, under a schema lock that is a no-op on
// SQLite. That is DB8 and DB9 exactly, so Aeon registers workers and owns no
// runner of its own. Measured, standalone, with no system.Initialize:
//
//	three opens of one file   →  _migrations holds one row per migration
//	an ALTER migration        →  applied, column present
//	a FAILING migration       →  its transaction rolled back (the table it
//	                             created is absent), the run STOPPED (the
//	                             migration after it never ran), the ledger
//	                             recorded `failed`, and RunAll reported errors
//	                             so the OPEN fails loudly (D5)
//
// Running two migration systems over one file is the thing that works until it
// does not, so there is one: theirs.
//
// # What Aeon still owns
//
// The ordered set in schema.go, and the decision that the open fails when the
// runner reports an error. The framework retries a failing migration before
// giving up (three attempts by default); Aeon lowers that to one, because a
// DDL statement that failed will fail again and two extra attempts only make
// the log harder to read.

// migrationRetries is how many times a failing migration is attempted.
//
// One. The framework's default is three, which is right for a migration that
// might be contending with another replica on PostgreSQL. A SQLite DDL
// statement that failed is a statement with a mistake in it, and retrying it
// twice more adds two misleading error lines before the real one.
const migrationRetries = 1

// Migrate applies every pending migration, in id order (DB8).
//
// Idempotent: already-applied migrations are skipped by the ledger, so calling
// it on every open is the intended use. It is also the framework `Store`
// contract's Migrate(), which is why it takes no arguments.
func (s *Store) Migrate() error {
	svc, err := s.migrationService()
	if err != nil {
		return err
	}
	ctx := appctx.NewContext(context.Background())
	if diag := svc.RunAll(ctx); diag != nil && diag.HasErrors() {
		// Loudly, and at the OPEN (D5). A partially migrated database is worse
		// than one that refuses to open, because the caller proceeds against a
		// shape nothing expects.
		return fmt.Errorf("store: migrating %s failed, and the schema is at its previous version: %s",
			s.path, diag.GetSummary())
	}
	// S52 LM1: the schema has the slug-law columns; now the agent
	// scope_keys move to slugs, once, before any reader trusts them. A
	// refusal here fails the OPEN, not just the step: memory left keyed on
	// old names would be memory the new scope check refuses to find, and
	// a store that answers half its own memory is the failure D5 refuses
	// at the open. Idempotent and check-first: every ordinary open pays
	// one SELECT (measured: MigrateLearningAgentScopeKeys).
	if _, err := s.MigrateLearningAgentScopeKeys(); err != nil {
		return err
	}
	return nil
}

// Pending reports the migrations not yet applied, for `aeon db upgrade
// --dry-run`.
//
// It answers WITHOUT applying anything, which is the whole point: the reason to
// ask is usually that you do not want them applied yet.
func (s *Store) Pending() ([]Migration, error) {
	applied, err := s.AppliedMigrations()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(applied))
	for _, a := range applied {
		seen[a.Name] = true
	}
	var out []Migration
	for _, m := range Migrations() {
		if !seen[m.Name] {
			out = append(out, m)
		}
	}
	return out, nil
}

// AppliedMigration is one row of the framework's ledger.
type AppliedMigration struct {
	Name      string
	Status    string
	AppliedAt string
}

// AppliedMigrations reads the ledger, oldest first.
//
// It reads the framework's `_migrations` table directly rather than through the
// migration service, because `db info` must answer on a store that has only
// been opened — and because the ledger is a fact about the file, not about the
// service instance that happens to be in memory.
func (s *Store) AppliedMigrations() ([]AppliedMigration, error) {
	rows, err := s.r.Query(`SELECT name, status, applied_at FROM _migrations ORDER BY applied_at, name`)
	if err != nil {
		return nil, fmt.Errorf("store: read the migration ledger: %w", err)
	}
	defer rows.Close()

	var out []AppliedMigration
	for rows.Next() {
		var a AppliedMigration
		if err := rows.Scan(&a.Name, &a.Status, &a.AppliedAt); err != nil {
			return nil, fmt.Errorf("store: read the migration ledger: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// migrationService builds the framework's runner over Aeon's ordered set.
//
// A fresh service per call rather than the framework's singleton: the singleton
// is one per process, and a process here opens several stores — every test, and
// `aeon db info` beside a running daemon. NewMigrationService is public and
// non-singleton precisely for this.
func (s *Store) migrationService() (*migrations.MigrationService, error) {
	if s.gw == nil {
		return nil, fmt.Errorf("store: %s has no handle for the migration runner", s.path)
	}
	svc := migrations.NewMigrationService(s.gw)
	svc.SetMaxRetries(migrationRetries)
	set := Migrations()
	legacyIcon, err := s.hasLegacyProjectIcon()
	if err != nil {
		return nil, err
	}
	if legacyIcon {
		// Before the policy branch merged, the icon was shipped as 0016.
		// Keep 0017's ledger entry, but do not add the same column twice.
		for i := range set {
			if set[i].Name == "0017_project_icon" {
				set[i].SQL = "SELECT 1;"
			}
		}
	}
	legacyPause, err := s.hasLegacyPauseReason()
	if err != nil {
		return nil, err
	}
	if legacyPause {
		// The same column shipped once under 0019, before S11 took that number
		// for policy_ask_and_loop_attended and pushed pause_reason to 0020.
		// Keep the old ledger entry, but do not add the same column twice.
		for i := range set {
			if set[i].Name == "0020_step_pause_reason" {
				set[i].SQL = "SELECT 1;"
			}
		}
	}
	for _, w := range migrationWorkers(s.gw, set) {
		svc.Register(w)
	}
	return svc, nil
}

// hasLegacyProjectIcon recognises the one schema produced by the pre-merge
// branch. The old ledger entry alone is insufficient: a damaged or manually
// changed column must not be silently accepted as the current schema.
//
// S38 note, deliberate and load-bearing: the PRAGMA names `project` on
// PURPOSE, in the old vocabulary. This probe runs in migrationSet(), BEFORE
// any pending migration — the state it recognises is a pre-0035 store, where
// the repository table still answers to `project`. Renaming the probe's
// target is exactly what the legacy-fork tolerance forbids: a `component`
// query on such a store finds no table, the fork goes unrecognised, 0017
// re-runs, and the daemon refuses to open (measured in S38 T2, when the mass
// rename first hit this string). Same rule as the frozen test seeds: current
// code, vintage SQL, vintage names.
func (s *Store) hasLegacyProjectIcon() (bool, error) {
	var legacy int
	err := s.r.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = '0016_project_icon' AND status = 'applied'`).Scan(&legacy)
	if err != nil {
		if strings.Contains(err.Error(), "no such table: _migrations") {
			return false, nil // fresh database
		}
		return false, fmt.Errorf("store: inspect legacy migration ledger: %w", err)
	}
	if legacy == 0 {
		return false, nil
	}
	rows, err := s.r.Query(`PRAGMA table_info(project)`)
	if err != nil {
		return false, fmt.Errorf("store: inspect legacy project schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue *string
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("store: inspect legacy project schema: %w", err)
		}
		if name == "icon" {
			if typ != "TEXT" || notNull != 1 || defaultValue == nil || *defaultValue != "''" {
				return false, fmt.Errorf("store: legacy project.icon has an unexpected schema")
			}
			return true, nil
		}
	}
	return false, rows.Err() // an incomplete legacy migration still needs 0017
}

// hasLegacyPauseReason recognises the schema produced by the pre-renumber
// branch, where step.pause_reason shipped as 0019 before S11 took that number
// for policy_action_ask_and_loop_attended and pushed pause_reason to 0020.
// As with hasLegacyProjectIcon, the old ledger entry alone is insufficient: a
// damaged or manually changed column must not be silently accepted as the
// current schema.
func (s *Store) hasLegacyPauseReason() (bool, error) {
	var legacy int
	err := s.r.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = '0019_step_pause_reason' AND status = 'applied'`).Scan(&legacy)
	if err != nil {
		if strings.Contains(err.Error(), "no such table: _migrations") {
			return false, nil // fresh database
		}
		return false, fmt.Errorf("store: inspect legacy migration ledger: %w", err)
	}
	if legacy == 0 {
		return false, nil
	}
	rows, err := s.r.Query(`PRAGMA table_info(step)`)
	if err != nil {
		return false, fmt.Errorf("store: inspect legacy step schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue *string
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, fmt.Errorf("store: inspect legacy step schema: %w", err)
		}
		if name == "pause_reason" {
			if typ != "TEXT" || notNull != 0 || defaultValue != nil {
				return false, fmt.Errorf("store: legacy step.pause_reason has an unexpected schema")
			}
			return true, nil
		}
	}
	return false, rows.Err() // an incomplete legacy migration still needs 0020
}

// migrationWorkers adapts Aeon's migrations to the framework's contract.
//
// Order and Version are both the migration's id, so ordering is by id whatever
// order the slice is in (the framework sorts by Order then Version). The
// validation for that is a deliberately MISORDERED slice: a runner that
// happens to apply array order passes every test built from an ordered one.
func migrationWorkers(db *gorm.DB, set []Migration) []migrationinterfaces.MigrationWorker {
	ordered := make([]Migration, len(set))
	copy(ordered, set)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	out := make([]migrationinterfaces.MigrationWorker, 0, len(ordered))
	for _, m := range ordered {
		out = append(out, migrations.NewSQLMigrationWorkerFromContent(
			db, m.Name, m.Description, m.ID, m.SQL, "",
		))
	}
	return out
}

// Tables lists the tables in the database, for `aeon db info`.
//
// Sorted, and including the framework's `_migrations` — a person looking at
// this list wants to know everything in the file, and hiding the one table
// Aeon did not write would make the ledger harder to find than it should be.
func (s *Store) Tables() ([]string, error) {
	rows, err := s.r.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list tables: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("store: list tables: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// JournalMode reports the journal mode in force, read from the connection
// rather than inferred from the DSN (DB16's test rule).
func (s *Store) JournalMode() (string, error) {
	var mode string
	if err := s.r.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", fmt.Errorf("store: read journal_mode: %w", err)
	}
	return strings.ToLower(mode), nil
}
