package main

// backup.go — S62 T9 (BK12): the five socket-first verbs of the backup
// family.
//
//	aeon backup run [--no-verify] [--json]
//	aeon backup list [--json]
//	aeon backup verify FILE [--drill] [--json]
//	aeon backup status [--json]
//	aeon backup prune [--dry-run | --yes] [--json]
//
// Socket-first (P34): with the daemon home the verbs are clients of
// backup.run / backup.list / backup.verify / backup.status /
// backup.prune, and the ledger rows are the daemon's. "Nobody is home"
// (noDaemonError — the one dial fault the S45 T8 rule allows a fallback
// on; every other dial fault is refused as-is, never a fallback) is the
// DIRECT door (backup_direct.go): the store opened SkipMigrations, the
// actor cli, the fixed stderr line saying which door answered. Both
// doors render through the same printers, so the bytes a person reads
// are the door's, not the transport's (BK12's parity).
//
// restore is T7's verb (backup_restore.go) — the one door that never
// rides the socket (BK9) — and the family's parent, in that file,
// dispatches to it. The family's guards (backup_guard_test.go) hold the
// boundary: no restore method or tool exists, and the writes are the
// person's door.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/doctor"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// backupDirectLine is the direct door's fixed stderr line (pinned by
// test): which door answered. Fixed because a line that varies with the
// machine is a line a script cannot depend on.
const backupDirectLine = "daemon not running — applied directly to the store"

// backupDoor is the five verbs behind the door the select picks: the
// socket's when the daemon answers, the direct one when nobody is
// home. Refusals ride back as errors whose text the printers never
// see — the error path renders them three-part (renderError), both
// doors byte-identical, because the direct door builds the wire's own
// refusal shape (directRefused).
type backupDoor interface {
	Run(ctx context.Context, verify *bool) (protocol.BackupRunResult, error)
	List(ctx context.Context, limit int, includePreMigration bool) (protocol.BackupListResult, error)
	Verify(ctx context.Context, file string, drill bool) (protocol.BackupVerifyResult, error)
	Status(ctx context.Context) (protocol.BackupStatusView, error)
	Prune(ctx context.Context, dryRun bool) (protocol.BackupPruneView, error)
	Close() error
}

// nobodyIsHome answers whether a dial fault is the ONE fault the S45 T8
// rule allows a fallback on: the endpoint path is known but no file
// stands at it (ENOENT). The dial wraps every dial fault as a
// noDaemonError, so the classification is on the wrapped cause. A
// socket that IS there and refuses is not "nobody is home": the daemon
// binds its endpoint at Init, before Serve, so a ECONNREFUSED can be a
// daemon that is starting — a process that is about to stand on the
// store, and falling back onto the store in that window would be a
// second writer with the run lock the only guard between them. A dead
// daemon's leftover socket is the same bytes on the wire, and the
// refusal costs the person a "rm the socket" — while the fallback
// costs the store a race. EACCES (a peer the endpoint will not admit)
// and a handshake that answers a different protocol are refused as-is
// for the same reason: the fault is the connection, not the absence.
func nobodyIsHome(err error) bool {
	var nd *noDaemonError
	if !errors.As(err, &nd) {
		return false
	}
	return errors.Is(nd.Unwrap(), os.ErrNotExist)
}

// selectDoor picks the door the verb rides on: the socket's when the
// daemon answers, the direct one when nobody is home. The dial is once
// per verb — a door is picked per verb, not per process, so a daemon
// that stops between verbs is seen by the next one. The direct door
// speaks on stderr which door answered (the fixed line). The caller
// closes the door.
func selectDoor() (backupDoor, error) {
	c, err := dial(0)
	if err == nil {
		return &backupSocketDoor{c: c}, nil
	}
	if !nobodyIsHome(err) {
		return nil, err // a dial fault that is not "nobody is home" — refused as-is, never a fallback
	}
	d, derr := openDirectDoor()
	if derr != nil {
		return nil, derr
	}
	fmt.Fprintln(os.Stderr, backupDirectLine)
	return d, nil
}

// backupSocketDoor is the five verbs over the daemon's seam: the same wire
// results the app renders, and the ledger rows the daemon records.
type backupSocketDoor struct {
	c *protocol.Client
}

func (d *backupSocketDoor) Close() error { return d.c.Close() }

func (d *backupSocketDoor) Run(ctx context.Context, verify *bool) (protocol.BackupRunResult, error) {
	var res protocol.BackupRunResult
	err := d.c.Call(protocol.MethodBackupRun, protocol.BackupRunParams{Verify: verify}, &res)
	return res, err
}

func (d *backupSocketDoor) List(ctx context.Context, limit int, includePreMigration bool) (protocol.BackupListResult, error) {
	var res protocol.BackupListResult
	err := d.c.Call(protocol.MethodBackupList, protocol.BackupListParams{Limit: limit, IncludePreMigration: includePreMigration}, &res)
	return res, err
}

func (d *backupSocketDoor) Verify(ctx context.Context, file string, drill bool) (protocol.BackupVerifyResult, error) {
	var res protocol.BackupVerifyResult
	err := d.c.Call(protocol.MethodBackupVerify, protocol.BackupVerifyParams{File: file, Drill: drill}, &res)
	return res, err
}

func (d *backupSocketDoor) Status(ctx context.Context) (protocol.BackupStatusView, error) {
	var res protocol.BackupStatusView
	err := d.c.Call(protocol.MethodBackupStatus, struct{}{}, &res)
	return res, err
}

func (d *backupSocketDoor) Prune(ctx context.Context, dryRun bool) (protocol.BackupPruneView, error) {
	var res protocol.BackupPruneView
	err := d.c.Call(protocol.MethodBackupPrune, protocol.BackupPruneParams{DryRun: dryRun}, &res)
	return res, err
}

// --- the five verbs -----------------------------------------------------------------

func cmdBackupRun(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("backup run", flag.ContinueOnError)
	fs.SetOutput(w)
	noVerify := fs.Bool("no-verify", false, "skip the post-run verify for this run alone (the setting decides when absent)")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon backup run: %q is not a flag — the flags are --no-verify and --json", fs.Arg(0))}
	}

	// The verify ask is a tri-state on the wire: nil = the setting
	// decides, false = skip for this call alone. The flag's absence is
	// the nil.
	var verify *bool
	if *noVerify {
		falseValue := false
		verify = &falseValue
	}

	door, err := selectDoor()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.Run(context.Background(), verify)
	if err != nil {
		return err
	}
	return renderBackupRun(w, res, *asJSON)
}

func cmdBackupList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("backup list", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon backup list: %q is not a flag — the only flag is --json", fs.Arg(0))}
	}

	door, err := selectDoor()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.List(context.Background(), 0, false)
	if err != nil {
		return err
	}
	return renderBackupList(w, res, *asJSON)
}

func cmdBackupVerify(w io.Writer, args []string) error {
	// The file is the one positional, and the usage puts it FIRST —
	// but the flag package stops parsing at the first non-flag arg, so
	// `verify FILE --json` would leave the flag behind it unparsed and
	// count it as a second positional. Peel the positional out first;
	// the flags may then stand on either side of the file.
	var pos []string
	flags := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			pos = append(pos, a)
		}
	}
	fs := flag.NewFlagSet("backup verify", flag.ContinueOnError)
	fs.SetOutput(w)
	drill := fs.Bool("drill", false, "run level 3: open the backup on a copy and apply the pending migrations to it")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(pos) != 1 || fs.NArg() != 0 {
		return &usageError{msg: "aeon backup verify FILE [--drill] [--json] — name the backup file (a basename from backup list)"}
	}
	file := pos[0]

	door, err := selectDoor()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.Verify(context.Background(), file, *drill)
	if err != nil {
		return err
	}
	return renderBackupVerify(w, res, *asJSON)
}

func cmdBackupStatus(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("backup status", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon backup status: %q is not a flag — the only flag is --json", fs.Arg(0))}
	}

	door, err := selectDoor()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.Status(context.Background())
	if err != nil {
		return err
	}
	return renderBackupStatus(w, res, *asJSON)
}

// cmdBackupPrune plans the retention policy (always) and applies it
// (fenced) only past the seatbelt: --dry-run prints the plan and
// nothing moves; a bare call prints the plan and speaks that nothing
// moved; --yes applies it and records the act. The seatbelt is on a
// DELETION, and a deletion that cannot say what it would do before it
// does it is a deletion that cannot be reviewed (T4's rule, at the
// door).
func cmdBackupPrune(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("backup prune", flag.ContinueOnError)
	fs.SetOutput(w)
	dryRun := fs.Bool("dry-run", false, "print the plan; nothing is deleted")
	yes := fs.Bool("yes", false, "apply the plan: delete the files the retention policy does not keep")
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return &usageError{msg: fmt.Sprintf("aeon backup prune: %q is not a flag — the flags are --dry-run, --yes and --json", fs.Arg(0))}
	}
	if *dryRun && *yes {
		return &usageError{msg: "aeon backup prune: --dry-run and --yes are mutually exclusive — one asks, the other acts"}
	}

	// The seatbelt: a non-dry-run without --yes is a dry run that says
	// so — the plan prints, nothing moves, and the line names the flag
	// that would have.
	askOnly := !*dryRun && !*yes
	door, err := selectDoor()
	if err != nil {
		return err
	}
	defer door.Close()
	res, err := door.Prune(context.Background(), askOnly)
	if err != nil {
		return err
	}
	if *asJSON {
		if askOnly {
			// The wire result of the ASK (nothing removed) plus the
			// seatbelt line on stderr: the json on stdout stays the
			// door's bytes verbatim.
			fmt.Fprintln(os.Stderr, backupPruneSeatbeltLine)
			return printJSON(w, res)
		}
		return printJSON(w, res)
	}
	return renderBackupPrune(w, res, askOnly)
}

// backupPruneSeatbeltLine is the bare-call seatbelt's fixed line (pinned
// by test): the plan printed, nothing moved, and the flag that would
// have.
const backupPruneSeatbeltLine = "nothing was deleted — rerun with --yes to apply this plan"

// --- the printers -------------------------------------------------------------------

// renderBackupRun shows the run's answer: the file's name and size, the
// schema it carries, the verify level the run reached, the free-space
// state, and whether the ledger took its row (both doors record — the
// direct door's store is writable, only the daemon's is a stream).
func renderBackupRun(w io.Writer, res protocol.BackupRunResult, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	fmt.Fprintf(w, "backup  %s — %s\n", res.Run.FileName, humanBytes(res.Run.SizeBytes))
	fmt.Fprintf(w, "        schema %d applied, latest %s\n", res.Run.Applied, res.Run.LastMigration)
	fmt.Fprintf(w, "        verify %s · free space %s\n", res.Run.VerifyLevel, res.Run.FreeSpace)
	if res.Run.LockReclaimed {
		fmt.Fprintln(w, "        the stale backup lock was reclaimed before the run")
	}
	if res.Run.Recorded {
		fmt.Fprintln(w, "        the run is in the ledger (kind backup)")
	} else {
		fmt.Fprintln(w, "        the run is NOT in the ledger — the row could not be recorded")
	}
	if res.Backup != nil {
		fmt.Fprintf(w, "        as the list will show it: %s · %s · %s\n", res.Backup.CreatedAt, res.Backup.Reason, res.Backup.Verified)
	}
	return nil
}

func renderBackupList(w io.Writer, res protocol.BackupListResult, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	if len(res.Dirs) > 0 {
		fmt.Fprintf(w, "backups — %d file(s) in %s\n", len(res.Backups), res.Dirs[0])
	} else {
		fmt.Fprintln(w, "backups — no configured dir (the settings name none)")
	}
	for _, b := range res.Backups {
		restore := "restorable"
		if !b.CanRestoreWithThisBinary {
			restore = "newer schema — NOT restorable with this binary"
		}
		fmt.Fprintf(w, "  %-40s %8d s  %-13s %s  %s\n", b.FileName, b.AgeSeconds, b.Reason, b.Verified, restore)
	}
	return nil
}

func renderBackupVerify(w io.Writer, res protocol.BackupVerifyResult, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	state := "ok"
	if !res.Report.OK {
		state = "FAILED"
	}
	fmt.Fprintf(w, "verify  %s — level %d: %s\n", res.Report.FileName, res.Report.Level, state)
	if res.Report.Sentence != "" {
		fmt.Fprintf(w, "        %s\n", res.Report.Sentence)
	}
	if res.Report.NewerSchema {
		fmt.Fprintln(w, "        the backup carries a schema this binary has not shipped — the counts can never match")
	}
	for _, u := range res.Report.Unknown {
		fmt.Fprintf(w, "        unknown table in the backup: %s\n", u)
	}
	for _, m := range res.Report.DrillApplied {
		fmt.Fprintf(w, "        the drill applied: %s\n", m)
	}
	fmt.Fprintf(w, "        ledger: %s · started %s · finished %s\n", res.Run.Outcome, res.Run.StartedAt, res.Run.FinishedAt)
	if res.Run.ErrorKind != "" {
		fmt.Fprintf(w, "        error kind: %s\n", res.Run.ErrorKind)
	}
	return nil
}

func renderBackupStatus(w io.Writer, res protocol.BackupStatusView, asJSON bool) error {
	if asJSON {
		return printJSON(w, res)
	}
	last := "never"
	if res.LastSuccess != "" {
		last = fmt.Sprintf("%d s ago", res.AgeSeconds)
	}
	fmt.Fprintf(w, "backup  %s — dir %s · last success %s\n", res.State, res.Dir, last)
	if res.LastVerified != "" {
		fmt.Fprintf(w, "        last verified: %s\n", res.LastVerified)
	}
	if res.NextDueAt != "" {
		fmt.Fprintf(w, "        next due: %s\n", res.NextDueAt)
	}
	if res.Running {
		fmt.Fprintln(w, "        a backup is running now (the lock is held)")
	}
	for _, s := range res.Settings {
		fmt.Fprintf(w, "  %-22s %s (%s)\n", s.Key, s.Value, s.Source)
	}
	fmt.Fprintf(w, "        db: %s + %s WAL · journal %s · %d applied / %d pending\n",
		humanBytes(res.DB.Bytes), humanBytes(res.DB.WALBytes), res.DB.JournalMode, res.DB.Applied, res.DB.Pending)
	return nil
}

func renderBackupPrune(w io.Writer, res protocol.BackupPruneView, askOnly bool) error {
	kept, removed := 0, 0
	for _, p := range res.Plan {
		if p.Keep {
			kept++
		} else {
			removed++
		}
	}
	if askOnly {
		fmt.Fprintf(w, "prune — the plan: %d kept, %d would be removed\n", kept, removed)
		for _, p := range res.Plan {
			fmt.Fprintf(w, "  %-40s %s — %s\n", p.FileName, keepOrRemove(p.Keep), p.Reason)
		}
		fmt.Fprintln(w, backupPruneSeatbeltLine)
		return nil
	}
	if res.Removed == nil {
		fmt.Fprintf(w, "prune — dry run: %d kept, %d would be removed (nothing was deleted)\n", kept, removed)
	} else {
		fmt.Fprintf(w, "prune — %d kept, %d removed\n", kept, removed)
	}
	for _, p := range res.Plan {
		fmt.Fprintf(w, "  %-40s %s — %s\n", p.FileName, keepOrRemove(p.Keep), p.Reason)
	}
	if res.Removed != nil {
		fmt.Fprintln(w, "the act is in the ledger (kind prune)")
	}
	return nil
}

func keepOrRemove(keep bool) string {
	if keep {
		return "keep"
	}
	return "remove"
}

// (The age's voice for a person — the coarsest unit that still carries
// the fact — is internal/doctor.AgeHuman: the doctor's four lines and
// these printers share one definition, so the two surfaces never render
// two ages.)

// dbInfoBackupLine renders `aeon db info`'s one final line (S62 T9,
// BK15): `backup  <age or never> · <dir>` — the label six plus two
// spaces. The age is the machine's own definition of the last success
// (backup.Status over the handle db info already opened — the ledger's
// newest ok backup row, its finished stamp), and the dir is the
// resolver's. No separate facts, no second definition.
func dbInfoBackupLine(s *store.Store) (string, error) {
	stateDir, err := paths.StoreDir()
	if err != nil {
		return "", err
	}
	dep := backup.RealDeps(s, Version)
	dep.StateDir = stateDir
	now := time.Now()
	view, err := backup.Status(dep, s, now)
	if err != nil {
		return "", err
	}
	age := "never"
	if view.LastSuccessAt != "" {
		if t, perr := time.Parse(directStampFormat, view.LastSuccessAt); perr == nil {
			age = doctor.AgeHuman(now.Sub(t)) + " ago"
		} else {
			age = view.LastSuccessAt // the stamp as recorded, never a guess
		}
	}
	return fmt.Sprintf("backup   %s · %s", age, view.Dir), nil
}
