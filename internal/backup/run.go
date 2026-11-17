package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

// The error kinds Run, Verify and the restore door produce: the ledger's
// closed set (T3), all eleven, named here so the ledger and the package
// cannot drift — T7's restore door claimed "busy", and with it the package
// names the whole set. The lockstep test in guard_test.go walks both sides.
const (
	ErrKindDiskFull    = "disk-full"
	ErrKindDirUnwrit   = "dir-unwritable"
	ErrKindLockHeld    = "lock-held"
	ErrKindIntegrity   = "integrity"
	ErrKindFK          = "fk"
	ErrKindChecksum    = "checksum"
	ErrKindNewerSchema = "newer-schema"
	ErrKindIO          = "io"
	ErrKindCancelled   = "cancelled"
	ErrKindBusy        = "busy"
	ErrKindUnknown     = "unknown"
)

// RunError is a refused or failed run. Kind is the ledger's closed set (the
// run row's error_kind); What is the sentence the surfaces print verbatim —
// a refusal names its reason and the thing refused, per the house rule.
type RunError struct {
	Kind string
	What string
}

func (e RunError) Error() string { return e.What }

// isRefusal splits the two outcomes: a policy refusal (settings, lock,
// space, newer schema) records outcome "refused"; a failure in flight
// records "failed".
var refusalKinds = map[string]bool{
	ErrKindDiskFull:    true,
	ErrKindDirUnwrit:   true,
	ErrKindLockHeld:    true,
	ErrKindBusy:        true, // T7's restore door: a live daemon or a file holder
	ErrKindNewerSchema: true,
	ErrKindUnknown:     true,
}

func (e RunError) isRefusal() bool { return refusalKinds[e.Kind] }

// Store is the store-side seam: everything Run and the verify door need from
// the live database, on an interface so a test passes fakes and the package
// never touches a pool. *store.Store satisfies it.
type Store interface {
	// SnapshotTo copies the live database into destPath consistently (T2).
	SnapshotTo(ctx context.Context, destPath string) (store.SnapshotInfo, error)
	// AppliedNamesAndPending answers what the ledger says, without
	// creating anything (T2).
	AppliedNamesAndPending() (applied []string, pending []store.Migration, empty bool, err error)
	// Info reports the store's size for the free-space threshold (T2).
	Info() (store.Info, error)
	// RecordBackupRun appends the ledger row (T3).
	RecordBackupRun(in store.NewBackupRun) (store.BackupRun, error)
	// Now is the store's clock, for the started_at stamp.
	Now() string
	// Path names the store's own file, for reporting.
	Path() string
}

// Events is the event seam: the run's finished/failed kinds go out through
// it. T6 defines the exact payload shapes and the daemon's emitter; a nil
// Events is a no-op (the pre-T6 CLI door).
type Events interface {
	Emit(kind string, payload any)
}

// Deps is the injection set for one process: the store, the file checks,
// the drill opener, the clock, the settings, the events, the version this
// binary is, and the outside-db list the manifest carries.
type Deps struct {
	// Store is the live database. Required for Run.
	Store Store
	// CheckFile verifies a database file without touching it (T2's
	// store.CheckFile). Required for Run and for verify levels 2–3.
	CheckFile func(ctx context.Context, path string) (store.FileReport, error)
	// OpenStore opens a store at a directory with migrations enabled and
	// no pre-migrate hook — the drill's door (verify level 3). It returns
	// a closer; the caller closes it.
	OpenStore func(dir string) (io.Closer, error)
	// Clock is the instant source (I9's twin).
	Clock func() time.Time
	// Settings is the ten-key read seam.
	Settings Settings
	// Events, when non-nil, receives backup.finished / backup.failed.
	Events Events
	// Version is the aeon version the manifest carries.
	Version string
	// OutsideDB is the manifest's outside_db list; DefaultOutsideDB (T0's
	// measured list) when the caller has nothing better.
	OutsideDB []string
	// StateDir is the state directory, for DefaultDir when backup.dir is
	// unset.
	StateDir string

	// sameDevice answers the restore's same-filesystem pre-check (BK17)
	// for two dirs: (same, ok), ok=false the stated "cannot say".
	// Unexported: production leaves it nil, which runs the real Statfs
	// check (restore_device_*.go); a test in this package sets it to
	// simulate a backup dir on another device, which no single-mount box
	// can produce for real.
	sameDevice func(a, b string) (same bool, ok bool)
}

// RealDps builds the production Deps over a live store: the real file
// checks, the real drill opener (a plain store.Open — migrations on, no
// hook), the process clock, the environment settings, T0's outside list.
// T6's daemon service and T9's direct door both start from this and
// override what their door owns (the emitter, the actor, the state dir).
func RealDeps(s *store.Store, version string) Deps {
	return Deps{
		Store:     s,
		CheckFile: store.CheckFile,
		OpenStore: func(dir string) (io.Closer, error) { return store.Open(store.Options{Dir: dir}) },
		Clock:     time.Now,
		Settings:  NewEnvSettings(),
		Version:   version,
		OutsideDB: DefaultOutsideDB,
	}
}

// Options configures one Run.
type Options struct {
	// Dir is the backup directory; "" resolves through the settings
	// (backup.dir, else DefaultDir(StateDir)).
	Dir string
	// Reason is manual | scheduled | pre-migration; "" is manual.
	Reason string
	// ActorKind is user | daemon | cli; "" is user. Never agent (BK13's
	// twin: the actor is stamped by the door that holds the store).
	ActorKind string
	// VerifyAfter forces the verify level for this run; nil takes the
	// setting (default on).
	VerifyAfter *bool
	// fault injects a failure at the named stage, for the atomic-publish
	// test. Unexported on purpose: a production Options cannot carry it.
	fault map[string]error
}

// Result is a finished run: what was made, where, and what was said about
// it.
type Result struct {
	FileName      string
	Path          string
	SizeBytes     int64
	SHA256        string
	Applied       int
	LastMigration string
	VerifyLevel   string // written | verified
	FreeSpace     FreeState
	LockReclaimed bool
	Recorded      bool
}

// detailFor is the fixed-template ledger detail per outcome stage (SEC-033:
// a template, never the raw error — the raw text stays in the returned
// error, where an operator reading the CLI wants it, and out of the row).
var detailFor = map[string]string{
	"ok":        "snapshot verified and published",
	"snapshot":  "the online snapshot failed",
	"check":     "the snapshot failed its file checks",
	"integrity": "the snapshot failed integrity_check",
	"fk":        "the snapshot carries foreign-key violations",
	"tar":       "the archive write failed",
	"sync":      "the archive will not flush",
	"rename":    "the publish rename failed",
	"fsync-dir": "the directory will not flush after the publish",
	"space":     "the free space does not clear the threshold",
	"lock":      "the backup lock is held by another process",
	"settings":  "the backup settings are invalid",
	"tmpdir":    "the build directory cannot be made",
	"record":    "the run row could not be recorded",
	"cancelled": "the run was cancelled",
}

// Run makes one backup: snapshot, check, archive, atomic publish, record.
// It is the one function the daemon ticker, the CLI door and the
// pre-migration hook all call, and it holds the single-flight lock for the
// whole of it (BK4).
func Run(ctx context.Context, d Deps, o Options) (res Result, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.Store == nil {
		return Result{}, RunError{Kind: ErrKindUnknown, What: "backup: Deps.Store is nil — a run needs the live store"}
	}
	if d.CheckFile == nil {
		return Result{}, RunError{Kind: ErrKindUnknown, What: "backup: Deps.CheckFile is nil — a run needs the file checks"}
	}
	if d.OutsideDB == nil {
		d.OutsideDB = DefaultOutsideDB
	}
	now := d.Clock()
	reason := o.Reason
	if reason == "" {
		reason = "manual"
	}
	if !manifestReasons[reason] || reason == "post-restore-check" {
		// A file a run makes is one of the three run reasons. The fourth
		// (post-restore-check) stays in the ledger's closed set as
		// RESERVED and is never used: T7's restore door records no
		// ledger row at all — a restore is not recorded in the database
		// it replaces (BK10); its audit is the CLI's printed sentence and
		// the restore log beside the pre-restore dir.
		return Result{}, RunError{Kind: ErrKindUnknown, What: fmt.Sprintf("backup: reason %q is not a run reason (manual, scheduled, pre-migration)", reason)}
	}
	actor := o.ActorKind
	if actor == "" {
		actor = "user"
	}
	switch actor {
	case "user", "daemon", "cli":
	default:
		return Result{}, RunError{Kind: ErrKindUnknown, What: fmt.Sprintf("backup: actor %q is not one of user, daemon, cli — agents have no write path here", actor)}
	}

	// Resolve where and how.
	resolved, rerr := ResolveSettings(d.Settings)
	if rerr != nil {
		return Result{}, RunError{Kind: ErrKindUnknown, What: rerr.Error()}
	}
	dir := o.Dir
	if dir == "" {
		if resolved.Dir != "" {
			dir = resolved.Dir
		} else if d.StateDir != "" {
			dir = DefaultDir(d.StateDir)
		} else {
			return Result{}, RunError{Kind: ErrKindUnknown, What: "backup: no backup dir — set backup.dir or pass Options.Dir"}
		}
	}
	verifyAfter := resolved.VerifyAfter
	if o.VerifyAfter != nil {
		verifyAfter = *o.VerifyAfter
	}

	// Every failure records (or tries to) and emits, then returns.
	fail := func(stage string, e error) (Result, error) {
		var re RunError
		if !errors.As(e, &re) {
			re = RunError{Kind: ErrKindIO, What: fmt.Sprintf("backup: %s", shortErr(e))}
		}
		detail := detailFor[stage]
		if detail == "" {
			detail = detailFor["ok"]
		}
		recordRun(d, recordIn{
			Reason: reason, Actor: actor, Started: now, Outcome: outcomeFor(re),
			ErrorKind: re.Kind, Detail: detail,
			FreeSpace: FreeState(""),
		})
		emit(d, "backup.failed", map[string]any{
			"reason": reason, "actor_kind": actor, "error_kind": re.Kind, "stage": stage,
		})
		return Result{}, re
	}

	// 1 — the single-flight lock (BK4/OD6).
	lock, lerr := Acquire(dir, reason, now)
	if lerr != nil {
		if held, ok := lerr.(*LockHeldError); ok {
			re := RunError{Kind: ErrKindLockHeld, What: held.Error()}
			recordRun(d, recordIn{Reason: reason, Actor: actor, Started: now, Outcome: "refused", ErrorKind: re.Kind, Detail: detailFor["lock"]})
			emit(d, "backup.failed", map[string]any{"reason": reason, "actor_kind": actor, "error_kind": re.Kind, "stage": "lock"})
			return Result{}, re
		}
		return fail("lock", lerr)
	}
	defer func() { _ = lock.Release() }()
	if ferr := o.faultAt("lock"); ferr != nil {
		return fail("lock", ferr)
	}

	// 2 — free space (BK17): three-state, never a silent pass.
	info, ierr := d.Store.Info()
	if ierr != nil {
		return fail("space", ierr)
	}
	need := info.Bytes + info.WALBytes
	freeBytes, freeState := classifyFreeSpace(dir, need)
	if freeState == FreeLow {
		re := RunError{Kind: ErrKindDiskFull, What: fmt.Sprintf("backup: not enough free space — %d bytes free, about %d needed (1.2 × the store's %d)", freeBytes, int64(float64(need)*spaceThresholdFactor), need)}
		recordRun(d, recordIn{Reason: reason, Actor: actor, Started: now, Outcome: "refused", ErrorKind: re.Kind, Detail: detailFor["space"], FreeSpace: freeState})
		emit(d, "backup.failed", map[string]any{"reason": reason, "actor_kind": actor, "error_kind": re.Kind, "stage": "space"})
		return Result{}, re
	}
	if ferr := o.faultAt("space"); ferr != nil {
		return fail("space", ferr)
	}

	// 3 — the build directory, in the fence's name family.
	id, iderr := randomID()
	if iderr != nil {
		return fail("tmpdir", iderr)
	}
	tmpName := ".tmp-" + id
	if merr := os.Mkdir(filepath.Join(dir, tmpName), 0o700); merr != nil {
		return fail("tmpdir", fmt.Errorf("backup: make the build directory: %w", merr))
	}
	cleaned := false
	partialName := "" // set once the partial exists; cleanup removes it
	cleanup := func() {
		if !cleaned {
			cleaned = true
			if partialName != "" {
				_ = removeBackupFile(dir, partialName)
			}
			_ = removeBackupFile(dir, tmpName)
		}
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	if ferr := o.faultAt("tmpdir"); ferr != nil {
		return fail("tmpdir", ferr)
	}

	// 4 — the snapshot (T2): consistent, online, one file.
	dest := filepath.Join(dir, tmpName, "state.db")
	if _, serr := d.Store.SnapshotTo(ctx, dest); serr != nil {
		if ctx.Err() != nil {
			return fail("cancelled", RunError{Kind: ErrKindCancelled, What: "backup: the run was cancelled"})
		}
		return fail("snapshot", serr)
	}
	if ferr := o.faultAt("snapshot"); ferr != nil {
		return fail("snapshot", ferr)
	}

	// 5 — the file checks on the COPY (level 2 of BK5), which also supply
	// the manifest's counts, fingerprint and journal-mode fact. A snapshot
	// that fails its own integrity check is a store bug, and publishing it
	// as "written" would be a lie the checksum cannot catch — refuse.
	rep, cerr := d.CheckFile(ctx, dest)
	if cerr != nil {
		return fail("check", cerr)
	}
	if !rep.IntegrityOK {
		first := "unknown"
		if len(rep.IntegrityDetail) > 0 {
			first = strings.TrimSpace(rep.IntegrityDetail[0])
		}
		return fail("integrity", RunError{Kind: ErrKindIntegrity, What: fmt.Sprintf("backup: the snapshot failed integrity_check: %s", first)})
	}
	if rep.ForeignKeyViolations > 0 {
		return fail("fk", RunError{Kind: ErrKindFK, What: fmt.Sprintf("backup: the snapshot carries %d foreign-key violations", rep.ForeignKeyViolations)})
	}
	if ferr := o.faultAt("check"); ferr != nil {
		return fail("check", ferr)
	}

	// 6 — the manifest: counts and digests from the copy, applied set from
	// the ledger, no paths, no row content.
	applied, _, empty, aerr := d.Store.AppliedNamesAndPending()
	if aerr != nil {
		return fail("check", aerr)
	}
	if empty {
		// A fresh store has nothing to lose; backing it up anyway would
		// make an artefact whose manifest counts nothing. The hook (T5)
		// skips before calling; a direct run says so plainly.
		return Result{}, RunError{Kind: ErrKindUnknown, What: "backup: the store has no applied migrations — a fresh store has nothing to back up"}
	}
	fi, fierr := os.Stat(dest)
	if fierr != nil {
		return fail("check", fierr)
	}
	dbSum, sumErr := hashFile(dest)
	if sumErr != nil {
		return fail("checksum", sumErr)
	}
	fpr := sha256.Sum256([]byte(rep.Fingerprint.String()))
	m := Manifest{
		Format:               ManifestFormat,
		Name:                 Name,
		CreatedAt:            now.UTC().Format(createdFormat),
		Reason:               reason,
		AeonVersion:          d.Version,
		AppliedMigrations:    len(applied),
		LastMigration:        applied[len(applied)-1],
		SchemaFingerprintSHA: hex.EncodeToString(fpr[:]),
		Tables:               rep.Tables,
		DBBytes:              fi.Size(),
		DBSHA256:             dbSum,
		JournalModeInFile:    strings.ToLower(rep.JournalMode),
		CredentialsIncluded:  false,
		Confidential:         true,
		OutsideDB:            d.OutsideDB,
		Verified:             "unverified",
	}
	if m.JournalModeInFile != journalModeDelete {
		return fail("integrity", RunError{Kind: ErrKindIntegrity, What: fmt.Sprintf("backup: the snapshot's journal mode is %q, not delete — a one-file backup must have no sidecars", m.JournalModeInFile)})
	}
	if ferr := o.faultAt("manifest"); ferr != nil {
		return fail("manifest", ferr)
	}

	// 7 — the archive, to a .partial name (BK4's atomic publish).
	name := FileName(now, len(applied))
	partial := filepath.Join(dir, name+".partial")
	partialName = filepath.Base(partial)
	if perr := writePartial(dir, partial, m, dest); perr != nil {
		return fail("tar", perr)
	}
	if ferr := o.faultAt("tar"); ferr != nil {
		return fail("tar", ferr)
	}

	// 8 — flush the file: the bytes are a fact before the name is.
	if ferr := o.faultAt("sync"); ferr != nil {
		return fail("sync", ferr)
	}
	pf, oerr := os.Open(partial)
	if oerr != nil {
		return fail("sync", oerr)
	}
	serr := pf.Sync()
	_ = pf.Close()
	if serr != nil {
		return fail("sync", serr)
	}

	// 9 — the rename: after this moment the final name exists, complete.
	// Nothing between the sync and the rename can make it incomplete —
	// that is the whole of BK4's atomicity, and the fault-injection test
	// holds it at five points around here.
	final := filepath.Join(dir, name)
	if rerr2 := os.Rename(partial, final); rerr2 != nil {
		return fail("rename", rerr2)
	}

	// 10 — the after-rename crash point. A fault here simulates the
	// process dying once the publish is done: the final file is a
	// complete fact and stands, the tmp garbage stays (a crash cannot
	// clean up), and no ledger row is written — the file is the truth,
	// the row is an index, and an index a crash never wrote is a fact
	// the next run's sweep and the lister's join absorb.
	if ferr := o.faultAt("fsync-dir"); ferr != nil {
		return Result{
			FileName:      name,
			Path:          final,
			SizeBytes:     fi.Size(),
			SHA256:        dbSum,
			Applied:       len(applied),
			LastMigration: applied[len(applied)-1],
			VerifyLevel:   verifyLevelName(verifyAfter),
			FreeSpace:     freeState,
			LockReclaimed: lock.Reclaimed(),
			Recorded:      false,
		}, nil
	}
	partialName = "" // the rename moved it; cleanup's partial removal is a no-op

	// 11 — flush the directory, then drop the build directory. A
	// directory-flush failure is a risk a power cut materialises, not a
	// reason to unpublish: the artefact stands and the directory flushes
	// on its own time.
	_ = fsyncDir(dir)
	cleanup()

	// 12 — the ledger row (BK10): the run is a fact about a point in time.
	appliedN := int64(len(applied))
	size := fi.Size()
	recorded := false
	if _, rerr3 := d.Store.RecordBackupRun(store.NewBackupRun{
		Kind:              "backup",
		Reason:            reason,
		ActorKind:         actor,
		StartedAt:         now.UTC().Format(createdFormat),
		Outcome:           "ok",
		Detail:            detailFor["ok"],
		FileName:          name,
		SizeBytes:         &size,
		SHA256:            dbSum,
		AppliedMigrations: &appliedN,
		VerifyLevel:       verifyLevelName(verifyAfter),
		FreeSpaceState:    string(freeState),
	}); rerr3 != nil {
		// The file is published; a ledger refusal is reported, not fatal —
		// the row is an index into the file, never the file's proof.
	} else {
		recorded = true
	}

	emit(d, "backup.finished", map[string]any{
		"file_name": name, "size_bytes": size, "sha256": dbSum,
		"applied": appliedN, "verify_level": verifyLevelName(verifyAfter),
		"free_space_state": string(freeState), "reason": reason, "actor_kind": actor,
	})
	return Result{
		FileName:      name,
		Path:          final,
		SizeBytes:     size,
		SHA256:        dbSum,
		Applied:       len(applied),
		LastMigration: applied[len(applied)-1],
		VerifyLevel:   verifyLevelName(verifyAfter),
		FreeSpace:     freeState,
		LockReclaimed: lock.Reclaimed(),
		Recorded:      recorded,
	}, nil
}

func (o Options) faultAt(stage string) error {
	if o.fault == nil {
		return nil
	}
	return o.fault[stage]
}

func verifyLevelName(verifyAfter bool) string {
	if verifyAfter {
		return "verified"
	}
	return "written"
}

func outcomeFor(re RunError) string {
	if re.isRefusal() {
		return "refused"
	}
	return "failed"
}

// recordIn is the failure-path ledger shape, separate from the success
// path's because it carries the error's kind and a stage's detail.
type recordIn struct {
	Reason    string
	Actor     string
	Started   time.Time
	Outcome   string
	ErrorKind string
	Detail    string
	FreeSpace FreeState
}

func recordRun(d Deps, in recordIn) {
	if d.Store == nil {
		return
	}
	fs := string(in.FreeSpace)
	if fs == "" {
		fs = "unknown" // a failure before the space check has not measured it
	}
	_, _ = d.Store.RecordBackupRun(store.NewBackupRun{
		Kind:           "backup",
		Reason:         in.Reason,
		ActorKind:      in.Actor,
		StartedAt:      in.Started.UTC().Format(createdFormat),
		Outcome:        in.Outcome,
		ErrorKind:      in.ErrorKind,
		Detail:         in.Detail,
		FreeSpaceState: fs,
	})
}

// writePartial assembles the archive at a .partial name, 0600, O_EXCL —
// and if a crashed run left a partial under the same name, removes it
// through the fence first: a .partial is never a backup, so deleting one is
// deleting garbage by definition.
func writePartial(dir, partial string, m Manifest, dbPath string) error {
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			if rerr := removeBackupFile(dir, filepath.Base(partial)); rerr != nil {
				return fmt.Errorf("backup: a partial exists and will not be cleared: %w", rerr)
			}
			if f, err = os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err != nil {
				return fmt.Errorf("backup: open the partial for the archive: %w", err)
			}
		} else {
			return fmt.Errorf("backup: open the partial for the archive: %w", err)
		}
	}
	defer func() { _ = f.Close() }()
	if err := Write(f, m, dbPath); err != nil {
		return err
	}
	return nil
}

// hashFile is the db_sha256: the whole file, streamed.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("backup: hash the snapshot: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("backup: hash the snapshot: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func emit(d Deps, kind string, payload any) {
	if d.Events != nil {
		d.Events.Emit(kind, payload)
	}
}
