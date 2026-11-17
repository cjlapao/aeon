package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// backup.go — S62 T6 (BK7, BK12): the five backup methods (run/list/verify/
// status/prune), the human door over the daemon's one backup service.
//
// The seam is the daemon's BackupService (internal/daemon/backup_service.go),
// which implements BackupStore. The wire types below are THIS package's,
// field-for-field twins of the backup package's views with the one honest
// difference (BK12): no absolute path crosses the wire except the CONFIGURED
// backup dir, in backup.status — a setting the person can already read. The
// file params take BASENAMES; a separator in one is a shape refusal, because
// a name with a path in it would let a caller ask about a file the daemon
// never meant to name.
//
// Registered CONDITIONALLY by RegisterBackup (V13: a nil seam leaves the five
// ABSENT, not registered-and-refusing — the settings/seed/watchdog shape).
// All five are RequiresInitialize and none is Debug. run/verify/prune are
// WRITES that stamp the actor from the CONNECTION (never a param, BK13) and
// map a backup.RunError onto ErrBackupRefused — one code for the family, the
// reason in data.what, the closed-set kind in data.kind (DB19, the watchdog
// shape).
//
// RESTORE IS ABSENT (BK9). There is no restore method, and there will not be
// one: restoring is the CLI's direct door (T7), because it must hold the
// database file open itself, and a socket method that swapped the live
// database under the daemon would be the daemon opening its own grave. The
// guard in cmd/aeon/backup_guard_test.go keeps the absence proven.

// --- the seam -------------------------------------------------------------------

// BackupStore is the daemon's one backup seam: five doors, one service. The
// wire views are this package's types; a *BackupRefusal is the engine's
// refusal rendered as data (DB19), never an error; a non-nil error is an
// infrastructure failure the door cannot name any better than the store's
// own sentence.
type BackupStore interface {
	// Run starts one backup now (reason manual from the human door; the
	// engine records it as such). verify is the per-call override of the
	// backup.verifyAfter setting: nil = the setting decides, true/false =
	// this call alone.
	Run(ctx context.Context, actor Actor, verify *bool) (BackupRunResult, *BackupRefusal, error)
	// List answers what files exist in the configured dirs (the pre-migration
	// subdir included iff includePreMigration), newest first, each joined
	// with its latest verify verdict and its canRestoreWithThisBinary.
	List(ctx context.Context, limit int, includePreMigration bool) (BackupListResult, error)
	// Verify checks one named file: drill=false is level 2 (verified — the
	// restore-level check), drill=true is level 3 (the drill). file is a
	// BASENAME resolved inside the configured dirs, never a path (the
	// handler refuses separators before the seam sees them).
	Verify(ctx context.Context, actor Actor, file string, drill bool) (BackupVerifyResult, *BackupRefusal, error)
	// Status answers the four-state machine (never/fresh/stale/failing)
	// with the numbers behind it, and the database's own size facts.
	Status(ctx context.Context) (BackupStatusView, error)
	// Prune plans (dryRun) or applies the retention policy, fenced.
	Prune(ctx context.Context, actor Actor, dryRun bool) (BackupPruneView, *BackupRefusal, error)
}

// BackupRefusal is a refused backup act as data (DB19): Kind is the run
// row's closed error_kind (the machine can branch on it), Detail the
// person's sentence, printed verbatim. It is the protocol twin of
// backup.RunError — the daemon's adapter is the only place that may know
// both.
type BackupRefusal struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// --- the wire twins ----------------------------------------------------------------

// BackupView is one file as backup.list shows it: the manifest's facts, the
// ledger's joined verdict, and the age. No path at all (BK12) — the file's
// NAME is where its identity lives, and the dir it sits in is a fact
// backup.status and backup.list's dirs already report.
type BackupView struct {
	FileName      string `json:"file"`
	CreatedAt     string `json:"createdAt"`
	AgeSeconds    int64  `json:"ageSeconds"`
	Reason        string `json:"reason"`
	AeonVersion   string `json:"aeonVersion"`
	Applied       int    `json:"applied"`
	LastMigration string `json:"lastMigration"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
	// Verified is the joined verdict: "verified" when the ledger's newest
	// verify row for this file succeeded at a restore level, "unverified"
	// otherwise — including "no row at all", which is a state, not a failure.
	Verified                 string `json:"verified"`
	CanRestoreWithThisBinary bool   `json:"canRestoreWithThisBinary"`
}

// BackupRunView is a finished backup.run: what was made and what was said
// about it. The file's NAME, never its path (BK12).
type BackupRunView struct {
	FileName      string `json:"file"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
	Applied       int    `json:"applied"`
	LastMigration string `json:"lastMigration"`
	// VerifyLevel is the level the run's own verify reached: written |
	// verified (a run never drills — the drill is verify's door).
	VerifyLevel string `json:"verifyLevel"`
	// FreeSpace is the run's free-space state: ok | low | unknown.
	FreeSpace     string `json:"freeSpace"`
	LockReclaimed bool   `json:"lockReclaimed"`
	Recorded      bool   `json:"recorded"`
}

// BackupRunResult is the backup.run answer: the run, and — when the ledger
// took its row — the file as the list will show it, so a client sees its
// fresh backup in the list's own shape without a second call.
type BackupRunResult struct {
	Run    BackupRunView `json:"run"`
	Backup *BackupView   `json:"backup,omitempty"`
}

// BackupListResult is the backup.list answer: newest first, and the dirs
// that were scanned (the configured dir, and the pre-migration subdir when
// it is included) — the one place a dir is named, as a setting.
type BackupListResult struct {
	Backups []BackupView `json:"backups"`
	Dirs    []string     `json:"dirs"`
}

// BackupVerifyView is the report half of a backup.verify answer: the level
// that was asked, whether it held, and the engine's sentence. A level that
// failed still names the level it reached, so a client can say "level 2
// failed at the counts" rather than "failed".
type BackupVerifyView struct {
	FileName    string   `json:"file"`
	Level       int      `json:"level"`
	OK          bool     `json:"ok"`
	NewerSchema bool     `json:"newerSchema"`
	Unknown     []string `json:"unknown,omitempty"`
	Sentence    string   `json:"sentence"`
	// DrillApplied names the migrations the drill ran on the copy (level 3
	// only).
	DrillApplied []string `json:"drillApplied,omitempty"`
}

// BackupVerifyRunView is the run half of a backup.verify answer: the ledger
// row the verify wrote (BK10 — the verdict lives in the ledger, the file is
// immutable once published).
type BackupVerifyRunView struct {
	Outcome    string `json:"outcome"` // ok | failed
	ErrorKind  string `json:"errorKind,omitempty"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}

// BackupVerifyResult is the backup.verify answer: the recorded row and the
// report, together.
type BackupVerifyResult struct {
	Run    BackupVerifyRunView `json:"run"`
	Report BackupVerifyView    `json:"report"`
}

// BackupSettingView is one backup.* key as backup.status reports it — the
// value and the LAYER it came from (env | default, pre-S44), so a person
// can tell a setting they made from one they inherited.
type BackupSettingView struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// BackupDBView is the database's own size facts, from the store (the "and
// the current database size" half of the read-only tool's promise).
type BackupDBView struct {
	Bytes       int64  `json:"bytes"`
	WALBytes    int64  `json:"walBytes"`
	JournalMode string `json:"journalMode"`
	Applied     int    `json:"applied"`
	Pending     int    `json:"pending"`
}

// BackupStatusView is the backup.status facts (BK12): the machine's state
// and the numbers behind it. Dir is the ONE path on the wire of this
// family: the configured backup dir — a setting the person can already
// read, not a secret location.
type BackupStatusView struct {
	State   string `json:"state"` // never | fresh | stale | failing
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
	// LastSuccess is ISO-8601 UTC of the newest successful backup row; ""
	// when never.
	LastSuccess string `json:"lastSuccess,omitempty"`
	// LastVerified is the newest verify row (any file), "" when none.
	LastVerified string `json:"lastVerified,omitempty"`
	// AgeSeconds since the last success; 0 when never.
	AgeSeconds int64 `json:"ageSeconds,omitempty"`
	// NextDueAt is the last success + the configured interval, when enabled;
	// "" otherwise.
	NextDueAt string              `json:"nextDueAt,omitempty"`
	Running   bool                `json:"running"`
	Settings  []BackupSettingView `json:"settings"`
	DB        BackupDBView        `json:"db"`
}

// BackupPruneDecision is one file's place in a prune plan: kept or not, and
// why — a deletion that cannot say why is a deletion that cannot be
// reviewed.
type BackupPruneDecision struct {
	FileName string `json:"file"`
	Keep     bool   `json:"keep"`
	Reason   string `json:"reason"`
}

// BackupPruneView is a backup.prune answer: the plan (always) and what it
// did (when not a dry run). Files by NAME — the fenced deletion happened in
// the engine, and its rows already named the files (T4).
type BackupPruneView struct {
	Plan    []BackupPruneDecision `json:"plan"`
	Removed []string              `json:"removed,omitempty"`
}

// --- the params --------------------------------------------------------------------

// BackupRunParams: the actor is stamped by the door (BK13), never named; the
// reason is manual from the human door by definition (scheduled and
// pre-migration are the daemon's own reasons, and a caller naming one would
// be forging the ledger); verify is the one per-call override.
type BackupRunParams struct {
	Reason string `json:"reason,omitempty"`
	// Verify is a tri-state: nil = the setting decides (absent key), true =
	// verify at level 2 after the run, false = skip it for this call alone.
	Verify *bool `json:"verify,omitempty"`
}

// BackupListParams: the list is newest first over the configured dirs;
// limit caps it (0 = the engine's own cap), includePreMigration adds the
// pre-migration subdir the bootstrap constraint keeps separate.
type BackupListParams struct {
	Limit               int  `json:"limit,omitempty"`
	IncludePreMigration bool `json:"includePreMigration,omitempty"`
}

// BackupVerifyParams names one file and whether to drill. File is a
// BASENAME: a path separator in it is a shape refusal (no arbitrary-file
// reads on the wire — the engine resolves the name inside the configured
// dirs only).
type BackupVerifyParams struct {
	File  string `json:"file"`
	Drill bool   `json:"drill,omitempty"`
}

// BackupPruneParams: dryRun is the only knob. The policy (keep/keepDaily/
// keepWeekly/keepPreMig) is a setting, not a param — a caller-tuned
// retention is a second settings writer, which S62 does not build.
type BackupPruneParams struct {
	DryRun bool `json:"dryRun,omitempty"`
}

// --- registration -------------------------------------------------------------------

// RegisterBackup registers the five backup methods over a non-nil seam (V13).
// It returns whether the methods are in the table, so the daemon's wiring can
// be asserted rather than trusted — the settings/seed/watchdog shape.
func (t *Table) RegisterBackup() bool {
	if t.Backup == nil {
		return false
	}
	t.register(&Method{Name: MethodBackupRun, RequiresInitialize: true, Handle: t.handleBackupRun})
	t.register(&Method{Name: MethodBackupList, RequiresInitialize: true, Handle: t.handleBackupList})
	t.register(&Method{Name: MethodBackupVerify, RequiresInitialize: true, Handle: t.handleBackupVerify})
	t.register(&Method{Name: MethodBackupStatus, RequiresInitialize: true, Handle: t.handleBackupStatus})
	t.register(&Method{Name: MethodBackupPrune, RequiresInitialize: true, Handle: t.handleBackupPrune})
	return true
}

// --- the actor and the refusals -------------------------------------------------------

// backupSeam is the one nil-guard shape the five handlers share: registration
// is conditional on the seam, so a nil here is a registration/seam
// disagreement the guard tests catch — spoken, not silent (the watchdog shape).
func (t *Table) backupSeam() error {
	if t.Backup == nil {
		return ErrStoreUnavailable.WithWhat("backup.* needs the backup seam, and this build did not link it — the backup surface is absent for the same reason (V13)")
	}
	return nil
}

// backupActor is the write boundary's audit identity for the three WRITE
// doors (run/verify/prune): the person comes from the CONNECTION (the
// daemon's ActorFor seam, the kernel's peer credential), never from params.
// The honest absence (a daemon with no identity seam has no persons) refuses
// the write, and an empty id that is refused as well (the seedActor shape).
func (t *Table) backupActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the backup write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("the backup write is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// backupIdentityKeys names the identity keys a backup call may never carry:
// the actor is stamped by the door that holds the connection, and a param
// that names it would let a caller forge the audit identity of a write to
// their own data (BK13's wire-side twin of the ledger's closed set).
var backupIdentityKeys = []string{"actor", "actorKind", "who"}

func rejectBackupIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range backupIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — the actor is stamped by the daemon from the connection, never named by the caller (BK13)",
				method, key)}
		}
	}
	return nil
}

// refuseBackupFileParam is the wire's no-arbitrary-file-reads rule (BK12):
// the file params take a BASENAME — the engine resolves it inside the
// configured backup dirs, and a name carrying a separator would let a
// caller ask about a file the daemon never meant to name. A shape refusal
// (-32602): the request is wrong-shaped, not the daemon's refusal of a
// well-shaped act.
func refuseBackupFileParam(file string) error {
	if file == "" {
		return ErrBadParams{Err: fmt.Errorf("file is required — name the backup file to check (a basename from backup.list)")}
	}
	if strings.ContainsAny(file, `/\`) {
		return ErrBadParams{Err: fmt.Errorf(
			"file is a basename, not a path — %q carries a separator and the socket refuses it; the full path belongs to the CLI's direct door", file)}
	}
	return nil
}

// backupRefusedError renders the engine's refusal as the socket's (DB19, the
// watchdogRefusedError shape): one code (ErrBackupRefused), the closed-set
// kind in Data.Kind so a client can branch on the state found, the person's
// sentence in What.
func backupRefusedError(verb string, r *BackupRefusal) *Error {
	e := ErrBackupRefused.WithWhat("backup %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

// --- the handlers ----------------------------------------------------------------------

func (t *Table) handleBackupRun(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.backupSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodBackupRun), params); err != nil {
		return nil, err
	}
	var p BackupRunParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The reason is manual from the human door by definition. An absent key
	// is manual; a PRESENT key that is not manual is a ledger forgery by the
	// caller — refused, naming the value, before the actor is even asked.
	if p.Reason != "" && p.Reason != "manual" {
		return nil, ErrBadParams{Err: fmt.Errorf("reason is manual from this door — %q is the daemon's own (scheduled) or the store's own (pre-migration), and a caller naming one would be forging the ledger", p.Reason)}
	}
	actor, err := t.backupActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, refusal, err := t.Backup.Run(ctx, actor, p.Verify)
	if refusal != nil {
		return nil, backupRefusedError("run", refusal)
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (t *Table) handleBackupList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := t.backupSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodBackupList), params); err != nil {
		return nil, err
	}
	var p BackupListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Limit < 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("limit is a cap on how many to show, not a count to skip — %d is refused", p.Limit)}
	}
	view, err := t.Backup.List(ctx, p.Limit, p.IncludePreMigration)
	if err != nil {
		return nil, err
	}
	if view.Backups == nil {
		view.Backups = []BackupView{}
	}
	return view, nil
}

func (t *Table) handleBackupVerify(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.backupSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodBackupVerify), params); err != nil {
		return nil, err
	}
	var p BackupVerifyParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if err := refuseBackupFileParam(p.File); err != nil {
		return nil, err
	}
	actor, err := t.backupActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, refusal, err := t.Backup.Verify(ctx, actor, p.File, p.Drill)
	if refusal != nil {
		return nil, backupRefusedError("verify", refusal)
	}
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (t *Table) handleBackupStatus(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := t.backupSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodBackupStatus), params); err != nil {
		return nil, err
	}
	// No params: the machine is a fact of the daemon, not of the caller.
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	view, err := t.Backup.Status(ctx)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (t *Table) handleBackupPrune(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.backupSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodBackupPrune), params); err != nil {
		return nil, err
	}
	var p BackupPruneParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	actor, err := t.backupActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, refusal, err := t.Backup.Prune(ctx, actor, p.DryRun)
	if refusal != nil {
		return nil, backupRefusedError("prune", refusal)
	}
	if err != nil {
		return nil, err
	}
	if view.Plan == nil {
		view.Plan = []BackupPruneDecision{}
	}
	return view, nil
}
