package main

// backup_restore.go — T7's human door for putting a verified backup back
// (BK9): `aeon backup restore FILE [--plan] [--yes]`.
//
// This file is the single allowed home of the word "restore" on a command
// surface (T6's guard exception list). The restore overwrites the file the
// daemon owns, so it is CLI-only by definition: no socket method, no tool,
// no catalog spec — ever. This file dials the daemon only through the
// engine's non-mutating probe (to refuse under a live daemon); it never
// opens the store and never starts the daemon. The archive path comes from
// argv and from no other place — a restore is a person's typed act.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/store"
)

// cmdBackup is the backup family's parent verb. T7 built restore; T9
// extended the switch with the rest of the family — the five verbs
// whose implementation is on the daemon and ride the socket first (per
// P34), in backup.go; restore stays in this file, the one door that
// never rides the socket (BK9).
func cmdBackup(w io.Writer, args []string) error {
	if len(args) == 0 {
		return &usageError{msg: "aeon backup: a subcommand is needed — run, list, verify FILE, status, prune, or restore FILE"}
	}
	switch args[0] {
	case "run":
		return cmdBackupRun(w, args[1:])
	case "list":
		return cmdBackupList(w, args[1:])
	case "verify":
		return cmdBackupVerify(w, args[1:])
	case "status":
		return cmdBackupStatus(w, args[1:])
	case "prune":
		return cmdBackupPrune(w, args[1:])
	case "restore":
		return cmdBackupRestore(w, args[1:])
	default:
		return &usageError{msg: fmt.Sprintf("aeon backup: %q is not a backup subcommand — the verbs are run, list, verify, status, prune, restore", args[0])}
	}
}

func cmdBackupRestore(w io.Writer, args []string) error {
	var file string
	yes := false
	for _, a := range args {
		switch {
		case a == "--yes":
			yes = true
		case a == "--plan":
			// Plan-then-stop is the default; the flag names it. It
			// changes no behaviour: --yes is the only flag that moves
			// a file.
		case strings.HasPrefix(a, "-"):
			return &usageError{msg: "aeon backup restore: " + a + " is not a flag — the flags are --plan (the default) and --yes"}
		default:
			if file != "" {
				return &usageError{msg: "aeon backup restore: one FILE, not two — the archive to put back"}
			}
			file = a
		}
	}
	if file == "" {
		return &usageError{msg: "aeon backup restore: FILE is needed — the archive to put back (an aeon-state-….aeonbak)"}
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return &usageError{msg: "aeon backup restore: the FILE will not resolve to a path: " + err.Error()}
	}
	file = abs

	stateDir, err := paths.StoreDir()
	if err != nil {
		return fmt.Errorf("aeon: the state dir will not resolve: %v", err)
	}
	// The restore's deps: the real file checks, the drill's door (a
	// plain store.Open over the drill's SCRATCH dir — never the state
	// dir; the backup package's test holds that fact on the seam), the
	// process clock, the state dir. Store stays nil: the live store is
	// not opened in this path, and nothing here migrates — the store's
	// own next start does that, with its own pre-migration backup. The
	// drill's open is drillOpenOptions, the one documented no-hook
	// migrating door (bk_hook_guard_test.go's walk accepts exactly it
	// beside storeOpenOptions and SkipMigrations).
	d := backup.Deps{
		CheckFile: store.CheckFile,
		OpenStore: func(dir string) (io.Closer, error) { return store.Open(drillOpenOptions(dir)) },
		Clock:     time.Now,
		StateDir:  stateDir,
		Version:   Version,
		OutsideDB: backup.DefaultOutsideDB,
	}

	ctx := context.Background()
	p, perr := backup.PlanRestore(ctx, d, file, backup.PlanOpts{})
	defer p.Release() // the plan holds its exclusive lock on every path
	if perr != nil {
		// A refusal: nothing changed, so no log — the sentence on stderr
		// is the whole record, and the tree stays byte-identical.
		return perr
	}

	printRestorePlan(w, p, stateDir)
	if !yes {
		fmt.Fprintln(w, "the plan is printed and the swap is not run — nothing changed. --yes is the operator's word for it.")
		return &usageError{msg: "aeon backup restore: the plan is printed and nothing changed; add --yes to run the swap"}
	}

	rerr := backup.Restore(ctx, d, &p, true)
	// The log is the record of an attempted swap: it is written when the
	// swap was reached — a full success, or a failure after the move
	// (the put-back, or the swap already done). A refusal and a
	// plan-only run changed nothing, and an unchanged tree writes none.
	if p.Swapped || p.RolledBack {
		if _, lerr := writeRestoreLog(stateDir, p, time.Now()); lerr != nil {
			fmt.Fprintf(os.Stderr, "aeon: %v\n", lerr)
		}
	}
	if rerr != nil {
		return rerr
	}
	fmt.Fprintln(w, "the restore is done: state.db is the backup's file.")
	fmt.Fprintln(w, "next: start the daemon; it will open the store and, if the backup is at an older schema, migrate it — taking its own pre-migration backup first.")
	return nil
}

// printRestorePlan is the step-4 print: what the backup is, what it
// verified against, what is live and what will be kept, and what the
// backup does NOT contain (BK2's honesty: a restore is a database, not a
// machine). Paths may appear here — this is the screen, not the log.
func printRestorePlan(w io.Writer, p backup.RestorePlan, stateDir string) {
	m := p.Manifest
	fmt.Fprintf(w, "restore plan — %s\n", filepath.Base(p.BackupPath))
	fmt.Fprintf(w, "  backup:      created %s, schema at v%d (last migration %s), %d tables, %d bytes\n",
		m.CreatedAt, m.AppliedMigrations, m.LastMigration, len(m.Tables), m.DBBytes)
	fmt.Fprintf(w, "  verify:      %s\n", p.Verify.Sentence)
	if p.LiveExists {
		fmt.Fprintf(w, "  live:        %s — %d bytes, last modified %s\n",
			filepath.Join(stateDir, "state.db"), p.LiveSize, p.LiveModified.Format("2006-01-02T15:04:05Z"))
	} else {
		fmt.Fprintln(w, "  live:        absent — a fresh machine, nothing to keep")
	}
	if p.StaleEndpoint {
		fmt.Fprintln(w, "  endpoint:    stale — a socket file with no answer; it stays where it is (the next daemon start reclaims it)")
	} else {
		fmt.Fprintln(w, "  endpoint:    free — nothing is alive on the daemon's endpoint")
	}
	if p.OlderSchema {
		fmt.Fprintf(w, "  older schema: at v%d (last %s) — the next start will migrate it and take its own pre-migration backup\n",
			m.AppliedMigrations, m.LastMigration)
	}
	if len(p.LiveFiles) > 0 {
		fmt.Fprintf(w, "  kept:        the replaced set (%s) moves to state.db.pre-restore-<now>/ and is never deleted by any retention pass\n",
			strings.Join(p.LiveFiles, ", "))
	} else {
		fmt.Fprintln(w, "  kept:        none — there is no replaced set")
	}
	fmt.Fprintf(w, "  not in it:   the backup carries the database and nothing else — no %s, and credentials_included is %v\n",
		strings.Join(m.OutsideDB, ", "), m.CredentialsIncluded)
	fmt.Fprintln(w, "  the swap:    one rename inside the state dir; the daemon is never started and the store is never opened here")
}
