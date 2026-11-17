package backup

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// The pre-migration safety net (S62 T5, BK6).
//
// NewPreMigrate is the store.PreMigrateFunc the daemon and every CLI
// direct door pass to store.Open: when a store opens with pending
// migrations, a verified copy of it is made first, so a bad migration
// rolls back to a file, not to a memory. The hook
//
//   - resolves its dir WITHOUT any database read (OD8): the settings
//     seam, else DefaultDir(state dir) — at the hook's moment the store's
//     vintage may predate the settings table, so the database is not an
//     available source even when it exists;
//   - holds the single-flight lock for the whole run, the way every other
//     run does (BK4's twin: two openers take one backup);
//   - runs with reason pre-migration and verify level 2 MANDATORY,
//     whatever backup.verifyAfter says — an unverified undo is not an
//     undo;
//   - defers its ledger row: the table the row belongs to may not exist
//     until the pending migration creates it, so Open appends it after
//     Migrate succeeds, and a failed open appends nothing (the ledger is
//     unchanged when the migration is refused);
//   - prunes the pre-migration family only after a successful migration,
//     through the one fenced remover (BK8's twin: the prune is a removal
//     the fence sees).
//
// --no-backup (BK18) is a FLAG, wired by the door into NoBackup, and it is
// the only switch: no environment variable takes the safety net off,
// because the environment is set by whatever starts the process, and a
// migration that loses its copy to a stray variable is a migration that
// lost it to luck.

// preMigrationSubdir is the pre-migration family's home inside the backup
// dir, kept apart from the schedule's files so the two keeps never mix.
const preMigrationSubdir = "pre-migration"

// noBackupSentence is what the CLI prints when --no-backup took effect:
// the operator was told, in their own words, that the safety net is off.
const noBackupSentence = "no backup was taken before migrating (--no-backup)"

// PreMigrateOptions configures NewPreMigrate.
type PreMigrateOptions struct {
	// NoBackup is the operator's --no-backup: no file, a
	// skipped/operator row once the migration has succeeded, and the
	// sentence on Out. It is the only door off the safety net (BK18).
	NoBackup bool
	// ActorKind stamps the run row: the door that holds the store —
	// "daemon" for serve, "cli" for a verb. Never an agent: an agent has
	// no write path here (BK13's twin).
	ActorKind string
	// Out receives noBackupSentence. nil prints nothing (the daemon).
	Out io.Writer
}

// NewPreMigrate builds the hook. The Deps it is given must carry CheckFile,
// Clock, Settings, Version and OutsideDB; Store may be nil, because the
// live store does not exist yet — the hook receives it from Open and binds
// it at its moment.
func NewPreMigrate(d Deps, opts PreMigrateOptions) store.PreMigrateFunc {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if opts.ActorKind == "" {
		opts.ActorKind = "cli"
	}
	return func(ctx context.Context, s *store.Store, pending []store.Migration) (store.PreMigrateResult, error) {
		dir, err := preMigrationDir(s, d.Settings)
		if err != nil {
			return store.PreMigrateResult{}, err
		}
		if opts.NoBackup {
			if opts.Out != nil {
				fmt.Fprintln(opts.Out, noBackupSentence)
			}
			rec := store.NewBackupRun{
				Kind:           "backup",
				Reason:         "pre-migration",
				ActorKind:      opts.ActorKind,
				StartedAt:      stamp(d.Clock),
				Outcome:        "skipped",
				Detail:         "skipped: operator",
				FreeSpaceState: "unknown",
			}
			return store.PreMigrateResult{RunRecord: &rec}, nil
		}
		captured := &store.NewBackupRun{}
		dd := d
		dd.Store = &deferredStore{Store: s, capture: captured}
		one := true
		res, err := Run(ctx, dd, Options{
			Dir:         dir,
			Reason:      "pre-migration",
			ActorKind:   opts.ActorKind,
			VerifyAfter: &one,
		})
		if err != nil {
			return store.PreMigrateResult{}, hookError(dir, err)
		}
		if captured.Kind == "" {
			// Defensive: Run's success path always records through the
			// deferred store, so a success that captured nothing is a
			// changed Run, not a state this hook should paper over — but
			// the row it owes Open it can build from the result.
			captured = recordFromResult(res, opts.ActorKind, d.Clock)
		}
		return store.PreMigrateResult{
			RunRecord:    captured,
			AfterMigrate: pruneAfterSuccess(d, dir),
		}, nil
	}
}

// deferredStore forwards every Store method to the live store except
// RecordBackupRun, which captures: the row is owed to Open, which appends
// it after Migrate succeeds (the seam's fact 2). A failed run captures its
// failed row too, and the hook drops it with the error — the ledger is
// unchanged when the migration is refused.
type deferredStore struct {
	Store   // *store.Store
	capture *store.NewBackupRun
}

func (d *deferredStore) RecordBackupRun(in store.NewBackupRun) (store.BackupRun, error) {
	*d.capture = in
	return store.BackupRun{}, nil
}

// recordFromResult builds the ledger row from a finished run — the same
// facts Run's own recorder writes, minus what the result does not carry
// (the stamp is the clock's, as Run's own row uses it).
func recordFromResult(res Result, actor string, clock func() time.Time) *store.NewBackupRun {
	size := res.SizeBytes
	applied := int64(res.Applied)
	rec := store.NewBackupRun{
		Kind:              "backup",
		Reason:            "pre-migration",
		ActorKind:         actor,
		StartedAt:         stamp(clock),
		Outcome:           "ok",
		Detail:            detailFor["ok"],
		FileName:          res.FileName,
		SizeBytes:         &size,
		SHA256:            res.SHA256,
		AppliedMigrations: &applied,
		VerifyLevel:       res.VerifyLevel,
		FreeSpaceState:    string(res.FreeSpace),
	}
	return &rec
}

// preMigrationDir answers where the pre-migration copy lives: the backup
// dir's pre-migration subdirectory. The dir comes from the settings seam
// (AEON_BACKUP_DIR pre-T10) or DefaultDir of the store's own directory —
// resolved without any database read (OD8), because the settings table may
// not exist at the hook's moment and the database is not an available
// source even when it does.
func preMigrationDir(s *store.Store, settings Settings) (string, error) {
	resolved, err := ResolveSettings(settings)
	if err != nil {
		return "", err
	}
	base := resolved.Dir
	if base == "" {
		base = DefaultDir(filepath.Dir(s.Path()))
	}
	return filepath.Join(base, preMigrationSubdir), nil
}

// hookError turns a failed run into Open's refusal: the reason (Run's own
// sentence), the dir the copy was owed to, and the one door off it —
// --no-backup, the flag, never the environment (BK18).
func hookError(dir string, err error) error {
	return fmt.Errorf("the pre-migration backup failed and the migration is refused, and the store is untouched: %s — the backup dir is %s; pass --no-backup to migrate without a copy", err, dir)
}

// pruneAfterSuccess is the hook's AfterMigrate: the pre-migration family,
// pruned to keepPreMigration through the one fenced remover. Open calls it
// only after a successful migration (BK6: only a success prunes), and it
// records no ledger row — the rows the pruned files made stay (P3: history
// outlives files), and the prune event is the daemon ticker's to emit (T6).
func pruneAfterSuccess(d Deps, dir string) func(ctx context.Context, s *store.Store) error {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	return func(ctx context.Context, s *store.Store) error {
		resolved, err := ResolveSettings(d.Settings)
		if err != nil {
			return err
		}
		views, err := List([]string{dir}, s)
		if err != nil {
			return err
		}
		_, err = Apply(Plan(views, resolved.Policy(), d.Clock()))
		return err
	}
}

func stamp(clock func() time.Time) string {
	return clock().UTC().Format(createdFormat)
}
