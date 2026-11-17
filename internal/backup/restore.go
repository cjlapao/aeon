// restore.go — T7's restore engine (BK9). Plan runs steps 1–4 (read-only:
// probe, exclusive lock, level-2 verify, the facts of what will be
// replaced) and ends either in a refusal or in a plan the operator reads;
// Restore runs steps 5–8 (stage, re-check the staged file, drill a copy,
// keep the replaced set together, the swap, the cleanup).
//
// Two refusals shape everything here. The first is the daemon: the probe
// is non-mutating and anything answering the endpoint stops the restore
// before a byte is written, because the swap overwrites the file the
// daemon owns. The second is the file itself: the exclusive lock names
// the possibilities a holder has. And two rules are structural, not
// conventions: this package NEVER opens the live store (the drill opens
// a copy in a scratch dir, and the store's own next start is what
// migrates a restored file — with T5's pre-migration backup), and a
// refusal and a plan-only run change nothing, so they write no log.

package backup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/endpoint"
	"github.com/cjlapao/aeon/internal/store"
)

// The log's step names — BK9's eight steps, with the swap split into its
// three file moves, so the log (and the fault seam) can name which move a
// failure happened at. The set is closed: the log writer refuses anything
// not named here, and a fault at any of these points is classified by the
// position it leaves the database in (untouched, restored, or rolled
// back — never half).
const (
	StepProbe       = "probe"
	StepLock        = "lock"
	StepVerify      = "verify"
	StepPlan        = "plan"
	StepStage       = "stage"
	StepCheckStaged = "check-staged"
	StepDrill       = "drill"
	StepMoveOld     = "move-old"
	StepRenameIn    = "rename-in"
	StepRemoveStage = "remove-stage"
)

// LogEntry is one line of the restore log: the instant, the step, and the
// fact. Fact carries basenames and counts — never a path (the log writer
// refuses a / or a newline in a fact), never row content, never a secret.
type LogEntry struct {
	At   time.Time
	Step string
	Fact string
}

// PlanOpts configures one plan. fault is unexported on purpose:
// production cannot set it, and a test in this package can stop the
// sequence after any step. A fault fires AFTER its step completed — the
// step's work stands, and the classification of what a stop at that point
// leaves is the fault test's contract.
type PlanOpts struct {
	fault map[string]error
}

// RestorePlan is what a restore will do, what it saw, and what it has
// already touched. Every exported field is displayable; Log is the record
// the CLI writes when the swap was reached. A plan returned by Plan holds
// its exclusive lock on every path — success, refusal, fault — and
// Release is the caller's job on all of them (idempotent).
type RestorePlan struct {
	BackupPath string
	Manifest   Manifest
	Verify     VerifyReport

	// The live set as the plan found it. LiveFiles holds whichever of
	// state.db, state.db-wal, state.db-shm exist — exactly the set the
	// swap moves into the pre-restore dir.
	LiveExists   bool
	LiveSize     int64
	LiveModified time.Time
	LiveFiles    []string

	// StaleEndpoint: a socket file is present but nothing answers. It is
	// reported and left in place — its removal is Claim's job at the next
	// daemon start, and the probe never widens what it cleans up.
	StaleEndpoint bool
	// OlderSchema: the backup's ledger is behind this binary. Allowed:
	// the next start migrates it and takes its own pre-migration backup.
	OlderSchema bool

	// Swapped is true once the staged file has become state.db — after
	// that, nothing is rolled back. RolledBack is true when a failure
	// after the move put the replaced set back. The CLI writes the log
	// when either is set: a refusal and a plan-only run changed nothing.
	Swapped    bool
	RolledBack bool

	Log []LogEntry

	release   func()
	stateDir  string
	stageName string
	keepDir   string
	preHash   string
	clock     func() time.Time
	faults    map[string]error
}

// Release gives up the exclusive lock a plan holds. Idempotent, and safe
// on a plan whose lock was never acquired.
func (p *RestorePlan) Release() {
	if p.release != nil {
		p.release()
		p.release = nil
	}
}

func (p *RestorePlan) log(step, fact string) {
	if p.clock == nil {
		p.clock = time.Now
	}
	p.Log = append(p.Log, LogEntry{At: p.clock(), Step: step, Fact: fact})
}

// faultAfter is the test seam: a fault named for a step fires AFTER that
// step completed, and the returned error is the injected one, verbatim.
func (p *RestorePlan) faultAfter(step string) error {
	if p.faults == nil {
		return nil
	}
	if err := p.faults[step]; err != nil {
		p.log(step, "stopped by an injected fault")
		return err
	}
	return nil
}

// PlanRestore runs BK9's steps 1–4 and returns the plan (or a refusal).
// It writes nothing: the lock is the only thing it holds, and it is
// released by Release on every path. (The spec's name for this function
// was Plan; retention.go already owns that name for the prune plan, so
// the restore's is PlanRestore — a deviation recorded in T7's Notes.)
func PlanRestore(ctx context.Context, d Deps, file string, opts PlanOpts) (RestorePlan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.CheckFile == nil {
		return RestorePlan{}, RunError{Kind: ErrKindUnknown, What: "restore: this door needs Deps.CheckFile"}
	}
	if d.StateDir == "" {
		return RestorePlan{}, RunError{Kind: ErrKindUnknown, What: "restore: this door needs Deps.StateDir"}
	}
	if d.Clock == nil {
		d.Clock = time.Now
	}
	p := RestorePlan{BackupPath: file, stateDir: d.StateDir, clock: d.Clock, faults: opts.fault}

	// Step 1 — no daemon. The probe touches nothing: a stale socket file
	// is reported and left for Claim at the next daemon start.
	res, perr := endpoint.Probe()
	if perr != nil {
		return p, RunError{Kind: ErrKindIO, What: "restore: the endpoint will not be probed: " + shortErr(perr)}
	}
	if res.Alive {
		peer := res.Peer
		if peer == "" {
			peer = "its holder"
		}
		p.log(StepProbe, "a daemon answered the endpoint")
		return p, RunError{Kind: ErrKindBusy, What: fmt.Sprintf("restore refused: a daemon is alive on the endpoint %s (%s) — the restore overwrites the file the daemon owns; stop the daemon and try again", res.Path, peer)}
	}
	p.StaleEndpoint = res.Stale
	if res.Stale {
		p.log(StepProbe, "the endpoint is stale — a socket file with no answer, reported and left in place")
	} else {
		p.log(StepProbe, "the endpoint is free")
	}
	if err := p.faultAfter(StepProbe); err != nil {
		return p, err
	}

	// Step 2 — no other holder of the file. The lock opens the file with
	// nothing but the lock; a missing file is allowed (a fresh machine).
	livePath := filepath.Join(p.stateDir, "state.db")
	rel, lerr := store.LockExclusive(livePath)
	if lerr != nil {
		if errors.Is(lerr, store.ErrLockBusy) {
			p.log(StepLock, "the file came back busy")
			return p, RunError{Kind: ErrKindBusy, What: "restore refused: another process has the database open — the exclusive lock on state.db came back busy. It may be aeon watch, a CLI verb, or a daemon on a different endpoint; close it and try again"}
		}
		return p, RunError{Kind: ErrKindIO, What: "restore: the exclusive lock will not be taken: " + shortErr(lerr)}
	}
	p.release = rel
	if _, serr := os.Stat(livePath); serr == nil {
		p.log(StepLock, "the lock is held on the live file")
	} else {
		p.log(StepLock, "the file is absent — a fresh machine, the lock is a no-op")
	}
	if err := p.faultAfter(StepLock); err != nil {
		return p, err
	}

	// Step 3 — the backup, verified at level 2. Level-1 failures (a bad
	// archive) come back as RunErrors with the container's own sentence;
	// level-2 failures come back as a report with a sentence and a nil
	// error, so the gate is rep.OK, not the error.
	vrep, verr := Verify(ctx, d, file, 2)
	if verr != nil {
		p.log(StepVerify, "the archive refused")
		return p, verr
	}
	if vrep.NewerSchema {
		p.Verify = vrep
		p.log(StepVerify, "the backup's schema is newer than this binary")
		return p, RunError{Kind: ErrKindNewerSchema, What: vrep.Sentence}
	}
	if !vrep.OK {
		p.Verify = vrep
		kind := ErrKindIntegrity
		if fr := vrep.FileReport; fr != nil && fr.ForeignKeyViolations > 0 {
			kind = ErrKindFK
		}
		p.log(StepVerify, "level 2 did not hold")
		return p, RunError{Kind: kind, What: vrep.Sentence}
	}
	m, merr := OpenHeader(file)
	if merr != nil {
		return p, RunError{Kind: ErrKindIO, What: "restore: the manifest will not re-read after its level-2 verify: " + shortErr(merr)}
	}
	p.Manifest = m
	p.Verify = vrep
	p.OlderSchema = m.AppliedMigrations < len(store.KnownMigrationNames())
	p.log(StepVerify, fmt.Sprintf("level 2 holds: %s", vrep.Sentence))
	if err := p.faultAfter(StepVerify); err != nil {
		return p, err
	}

	// Step 4 — the facts of what will be replaced. The move set is
	// exactly the database and its sidecars; everything else in the state
	// dir (the cursor key, the credential files) is untouched.
	if _, derr := os.Stat(p.stateDir); derr != nil {
		return p, RunError{Kind: ErrKindIO, What: "restore: the state dir will not be read: " + shortErr(derr)}
	}
	for _, n := range []string{"state.db", "state.db-wal", "state.db-shm"} {
		if _, err := os.Stat(filepath.Join(p.stateDir, n)); err == nil {
			p.LiveFiles = append(p.LiveFiles, n)
		}
	}
	if st, serr := os.Stat(livePath); serr == nil {
		p.LiveExists = true
		p.LiveSize = st.Size()
		p.LiveModified = st.ModTime().UTC()
	}
	if p.LiveExists {
		p.log(StepPlan, fmt.Sprintf("the live file: %d bytes, modified %s, the set to keep: %s",
			p.LiveSize, p.LiveModified.Format("2006-01-02T15:04:05Z"), strings.Join(p.LiveFiles, ", ")))
	} else {
		p.log(StepPlan, "nothing to keep — the target has no database files, a fresh machine")
	}
	if p.OlderSchema {
		p.log(StepPlan, fmt.Sprintf("the backup is at an older schema (%d migrations, last %s) — the next start will migrate it and take its own pre-migration backup",
			m.AppliedMigrations, m.LastMigration))
	}
	if err := p.faultAfter(StepPlan); err != nil {
		return p, err
	}
	return p, nil
}

// Restore runs BK9's steps 5–8 on a plan. The lock the plan holds stays
// held until the swap completes (or the put-back completes) — the caller
// releases it afterwards, on every path.
func Restore(ctx context.Context, d Deps, p *RestorePlan, yes bool) error {
	if !yes {
		return RunError{Kind: ErrKindUnknown, What: "restore: the swap is refused without --yes — the plan is the plan, and the operator's word is what moves the file"}
	}
	if p.release == nil {
		return RunError{Kind: ErrKindUnknown, What: "restore: this plan did not go through Plan — the lock is not held and the swap will not run"}
	}
	if d.CheckFile == nil {
		return RunError{Kind: ErrKindUnknown, What: "restore: this door needs Deps.CheckFile"}
	}

	// Step 5 — stage: the same-filesystem fact first (BK17), then the
	// stage dir in the state dir, the staged file, the re-check, the
	// drill. Every failure in step 5 cleans the stage and touches
	// nothing else.
	backupDir := filepath.Dir(p.BackupPath)
	same, ok := false, false
	if d.sameDevice != nil {
		same, ok = d.sameDevice(backupDir, p.stateDir)
	} else {
		same, ok = sameDeviceFS(backupDir, p.stateDir)
	}
	if ok && !same {
		p.log(StepStage, "the backup dir and the state dir are on different filesystems")
		return RunError{Kind: ErrKindIO, What: "restore refused: the backup dir and the state dir are on different filesystems — the swap must be one rename on one filesystem. Copy the archive to a path on the same filesystem as the state dir and try again"}
	}
	if !ok {
		p.log(StepStage, "the same-filesystem check cannot run on this platform — it is stated, not passed; the swap is one rename inside the state dir")
	} else {
		p.log(StepStage, "the backup dir and the state dir are on the same filesystem")
	}

	stageDir, cleanupStage, serr := makeStage(p.stateDir)
	if serr != nil {
		return RunError{Kind: ErrKindIO, What: "restore: the stage dir will not be made: " + shortErr(serr)}
	}
	p.stageName = filepath.Base(stageDir)
	stagedPath := filepath.Join(stageDir, "state.db")
	r, oerr := Open(p.BackupPath)
	if oerr != nil {
		_ = cleanupStage()
		kind := ErrKindIO
		if strings.Contains(oerr.Error(), "checksum mismatch") {
			kind = ErrKindChecksum
		}
		return RunError{Kind: kind, What: oerr.Error()}
	}
	if cerr := copyFile(r.DBPath(), stagedPath, 0o600); cerr != nil {
		_ = r.Close()
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the staged file will not be written: " + shortErr(cerr)}
	}
	_ = r.Close()
	if ferr := fsyncDir(stageDir); ferr != nil {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the stage dir will not be synced: " + shortErr(ferr)}
	}
	p.log(StepStage, "the archive is staged beside the state dir, 0600, the dir synced")
	if err := p.faultAfter(StepStage); err != nil {
		_ = cleanupStage()
		return err
	}

	// Step 5, continued — the staged file against the manifest: the
	// copy that will be swapped in is re-verified before it is trusted,
	// not only the archive that was verified at step 3.
	srep, cerr := d.CheckFile(ctx, stagedPath)
	if cerr != nil {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the staged file will not check: " + shortErr(cerr)}
	}
	if !srep.IntegrityOK {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIntegrity, What: "restore refused: the staged file failed integrity_check — the archive verified at level 2, its copy did not"}
	}
	if srep.ForeignKeyViolations > 0 {
		_ = cleanupStage()
		return RunError{Kind: ErrKindFK, What: fmt.Sprintf("restore refused: the staged file carries %d foreign-key violations", srep.ForeignKeyViolations)}
	}
	dig, derr := fileSHA256(stagedPath)
	if derr != nil {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the staged file's digest will not be computed: " + shortErr(derr)}
	}
	if dig != p.Manifest.DBSHA256 {
		_ = cleanupStage()
		return RunError{Kind: ErrKindChecksum, What: "restore refused: the staged file's digest does not equal the manifest's — the copy is not the backup"}
	}
	fprSum := fmt.Sprintf("%x", sha256.Sum256([]byte(srep.Fingerprint.String())))
	if fprSum != p.Manifest.SchemaFingerprintSHA {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIntegrity, What: "restore refused: the staged file's schema fingerprint does not equal the manifest's"}
	}
	if bad, what := countsAgree(p.Manifest.Tables, srep.Tables); bad {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIntegrity, What: "restore refused: the staged file's row counts disagree with the manifest — " + what}
	}
	p.log(StepCheckStaged, fmt.Sprintf("the staged file holds: digest, integrity, fk, fingerprint, %d table counts", len(p.Manifest.Tables)))
	if err := p.faultAfter(StepCheckStaged); err != nil {
		_ = cleanupStage()
		return err
	}

	// Step 5, continued — the drill on a COPY, in a scratch dir inside
	// the state dir: the staged file is never opened for writing here,
	// so an older-schema backup lands byte-for-byte and the store's own
	// next start is what migrates it (with T5's pre-migration backup).
	if d.OpenStore == nil {
		_ = cleanupStage()
		return RunError{Kind: ErrKindUnknown, What: "restore: the drill needs Deps.OpenStore — it opens the copy as a store, and this process has no door to do it"}
	}
	drillDir, drillName, dterr := makeDrillCopy(p.stateDir, stagedPath)
	if dterr != nil {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the drill copy will not be made: " + shortErr(dterr)}
	}
	ds, oerr := d.OpenStore(drillDir)
	if oerr != nil {
		_ = removeBackupFile(p.stateDir, drillName)
		_ = cleanupStage()
		return RunError{Kind: ErrKindIO, What: "restore: the drill will not open the copy: " + shortErr(oerr)}
	}
	_ = ds.Close()
	post, perr := d.CheckFile(ctx, filepath.Join(drillDir, "state.db"))
	_ = removeBackupFile(p.stateDir, drillName) // fenced — the scratch is removed before the drill's verdict
	if perr != nil || !post.IntegrityOK || post.ForeignKeyViolations > 0 {
		_ = cleanupStage()
		return RunError{Kind: ErrKindIntegrity, What: "restore refused: the drill's copy did not open and check clean — the backup is not restorable by this binary"}
	}
	preSet := make(map[string]bool, len(srep.Applied))
	for _, n := range srep.Applied {
		preSet[n] = true
	}
	applied := 0
	for _, mig := range store.Migrations() {
		if !preSet[mig.Name] {
			applied++
		}
	}
	if applied == 0 {
		p.log(StepDrill, "the drill opened the copy; the ledger was already current")
	} else {
		p.log(StepDrill, fmt.Sprintf("the drill opened the copy and applied %d migrations to it", applied))
	}
	if err := p.faultAfter(StepDrill); err != nil {
		_ = cleanupStage()
		return err
	}

	// Step 6 — keep the replaced database as a set. db + wal + shm move
	// together into the pre-restore dir; after the move the state dir
	// holds no sidecars, so the restored file cannot inherit a stale
	// WAL. The pre-restore name is matched by no retention regex.
	if len(p.LiveFiles) > 0 {
		p.keepDir = "state.db.pre-restore-" + p.clock().UTC().Format("20060102T150405") + "Z"
		keepFull := filepath.Join(p.stateDir, p.keepDir)
		if _, err := os.Stat(keepFull); err == nil {
			_ = cleanupStage()
			return RunError{Kind: ErrKindIO, What: fmt.Sprintf("restore refused: the pre-restore dir %s already exists — two restores in one second; try again", p.keepDir)}
		}
		if err := os.Mkdir(keepFull, 0o700); err != nil {
			_ = cleanupStage()
			return RunError{Kind: ErrKindIO, What: "restore: the pre-restore dir will not be made: " + shortErr(err)}
		}
		livePath := filepath.Join(p.stateDir, "state.db")
		hash, herr := fileSHA256(livePath)
		if herr != nil {
			_ = cleanupStage()
			return RunError{Kind: ErrKindIO, What: "restore: the live file's pre-restore digest will not be computed: " + shortErr(herr)}
		}
		p.preHash = hash
		for _, n := range p.LiveFiles {
			if err := os.Rename(filepath.Join(p.stateDir, n), filepath.Join(keepFull, n)); err != nil {
				return p.putBack(RunError{Kind: ErrKindIO, What: "the move into the pre-restore dir failed: " + shortErr(err)})
			}
		}
		_ = fsyncDir(p.stateDir)
		p.log(StepMoveOld, fmt.Sprintf("kept %s in %s", strings.Join(p.LiveFiles, ", "), p.keepDir))
	} else {
		p.log(StepMoveOld, "nothing to keep — the target had no database files")
	}
	if err := p.faultAfter(StepMoveOld); err != nil {
		if len(p.LiveFiles) > 0 {
			return p.putBack(err)
		}
		_ = cleanupStage()
		return err
	}

	// Step 7 — the swap. One rename inside the state dir: the point of
	// the stage being there. A failure here puts the replaced set back;
	// a fault after the rename means the swap HAS completed, and
	// nothing after a swap is rolled back.
	livePath := filepath.Join(p.stateDir, "state.db")
	if err := os.Rename(stagedPath, livePath); err != nil {
		return p.putBack(RunError{Kind: ErrKindIO, What: "the swap's rename failed: " + shortErr(err)})
	}
	if err := os.Chmod(livePath, 0o600); err != nil {
		return p.putBack(RunError{Kind: ErrKindIO, What: "the swapped file will not be chmod 0600: " + shortErr(err)})
	}
	if err := fsyncDir(p.stateDir); err != nil {
		return p.putBack(RunError{Kind: ErrKindIO, What: "the state dir will not be synced after the swap: " + shortErr(err)})
	}
	p.Swapped = true
	p.log(StepRenameIn, "the staged file is now state.db, 0600, the dir synced")
	if err := p.faultAfter(StepRenameIn); err != nil {
		return p.swapDoneError(err, true)
	}

	// Step 8 — remove the stage dir (through the fence's restore
	// variant: the only other deletion this path owns) and leave the
	// rest to the store's next start.
	if err := removeRestoreStageDir(p.stateDir, p.stageName); err != nil {
		p.log(StepRemoveStage, "the stage dir will not be removed — it is named on stderr")
		return p.swapDoneError(RunError{Kind: ErrKindIO, What: "the stage dir will not be removed: " + shortErr(err)}, true)
	}
	p.log(StepRemoveStage, "the stage dir is gone")
	if err := p.faultAfter(StepRemoveStage); err != nil {
		return p.swapDoneError(err, false)
	}
	return nil
}

// putBack undoes step 6: the kept files go back to where they were, and
// the previous database's digest must equal the one recorded before the
// move. A put-back that cannot finish names the exact mv commands on
// stderr — nothing is ever deleted, and the pre-restore dir stays until
// a person has checked it.
func (p *RestorePlan) putBack(cause error) error {
	for i, n := range p.LiveFiles {
		if err := os.Rename(filepath.Join(p.stateDir, p.keepDir, n), filepath.Join(p.stateDir, n)); err != nil {
			return p.putBackBroken(p.LiveFiles[i:], cause)
		}
	}
	_ = fsyncDir(p.stateDir)
	if p.LiveExists {
		if got, herr := fileSHA256(filepath.Join(p.stateDir, "state.db")); herr != nil || got != p.preHash {
			return p.putBackBroken(nil, cause)
		}
	}
	_ = removeRestoreStageDir(p.stateDir, p.stageName) // fenced — the staged file is no longer wanted
	p.RolledBack = true
	p.log(StepMoveOld, "the restore was aborted and the previous database was put back")
	return fmt.Errorf("restore failed and the previous database was put back: %w", cause)
}

// putBackBroken is the put-back that cannot verify: it lists the exact
// commands for whatever is still in the pre-restore dir, and says what
// cannot be said by a hash.
func (p *RestorePlan) putBackBroken(pending []string, cause error) error {
	p.RolledBack = true
	p.log(StepMoveOld, "the put-back did not verify — the commands are on stderr")
	var what string
	if len(pending) == 0 {
		what = "restore failed and the put-back will not verify: state.db is back in place, but its digest does not equal the recorded pre-restore digest — inspect it by hand; the pre-restore dir " + p.keepDir + " holds what is left"
		return fmt.Errorf("%s; cause: %w", what, cause)
	}
	var cmds strings.Builder
	for _, n := range pending {
		fmt.Fprintf(&cmds, "\n  mv %s %s", filepath.Join(p.stateDir, p.keepDir, n), filepath.Join(p.stateDir, n))
	}
	what = "restore failed and the put-back did not complete: the previous database is in " + p.keepDir + cmds.String()
	return fmt.Errorf("%s; nothing was deleted; cause: %w", what, cause)
}

// swapDoneError reports a failure AFTER the swap has completed: the
// database is the backup's file, the replaced set is in the pre-restore
// dir, and nothing after a swap is rolled back. stageLeft says whether
// the stage dir outlived the failure, so the operator knows what to
// remove by hand.
func (p *RestorePlan) swapDoneError(cause error, stageLeft bool) error {
	p.Swapped = true
	what := "the restore completed — state.db is now the backup's file; the previous database is in " + p.keepDir
	if stageLeft {
		what += fmt.Sprintf("; the stage dir %s is left behind (the failure came after the swap and before its cleanup) — remove it by hand, keeping the pre-restore dir", p.stageName)
	}
	p.log(StepRemoveStage, "the failure came after the swap — no rollback after a swap")
	return fmt.Errorf("%s; cause: %w", what, cause)
}
