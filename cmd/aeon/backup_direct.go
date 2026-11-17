package main

// backup_direct.go — S62 T9 (BK12's door pair): the backup family's direct
// door, the one a "nobody is home" dial fault falls back to (and to no
// other fault — every other dial fault is refused as-is, the S45 T8 rule).
//
// The door is the daemon's seam's mirror: the SAME engine calls
// (backup.Run / List / Verify / Status / Plan / Apply) over the SAME
// Deps shape (RealDeps), with the differences this door owns —
//
//   - the store opened SkipMigrations: the direct door never migrates
//     and never trips the pre-migration hook (a backup that runs
//     migrations would back up the schema it is about to change);
//   - the actor stamped cli: the daemon stamps the peer, the ticker
//     stamps itself, the CLI stamps the CLI (BK13's direct-door twin);
//   - no events: the event stream is the daemon's, and a stopped daemon
//     has no stream — the absence is spoken by the fixed stderr line,
//     not by a forged event.
//
// The engine's run row is the engine's (it records itself, both doors);
// the verify and prune rows are the door's, recorded exactly as the
// daemon's backup_service.go records them — same kinds, reasons, fixed
// detail templates (SEC-033) and stamps. BK12's parity test holds the
// two doors' bytes to the same shape, door-specific fields normalised.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// directStampFormat is the ledger's stamp voice: the daemon seam's
// backupStampFormat, kept one definition per door because the two files
// never import each other and the ledger's column must not see two
// voices for one act.
const directStampFormat = "2006-01-02T15:04:05.000Z"

// directActorKind is the ledger's actor for the door this process is:
// the CLI, stamping itself (the closed set is user | daemon | cli).
const directActorKind = "cli"

// backupDirectDoor is the five verbs over the store this process opened
// SkipMigrations: the same engine calls the daemon's seam makes, with
// the actor and the state dir this door owns.
type backupDirectDoor struct {
	st  *store.Store
	dep backup.Deps
}

// openStoreSkipMigrations opens the state dir's store without running
// the migrations: the one open the surfaces that must not migrate use
// (the direct backup door, the doctor's mcp leg). A health check or a
// read that migrates the store would trip the pre-migration hook — the
// hook would back up the schema it is about to change.
func openStoreSkipMigrations() (*store.Store, error) {
	stateDir, err := paths.StoreDir()
	if err != nil {
		return nil, fmt.Errorf("aeon: the state dir will not resolve: %v", err)
	}
	st, err := store.Open(store.Options{
		Dir:            stateDir,
		SkipMigrations: true,
	})
	if err != nil {
		return nil, fmt.Errorf("aeon: the store will not open: %v", err)
	}
	return st, nil
}

// openDirectDoor opens the direct door: the state dir's store opened
// SkipMigrations, and the engine's Deps over it. The caller closes the
// door.
func openDirectDoor() (*backupDirectDoor, error) {
	stateDir, err := paths.StoreDir()
	if err != nil {
		return nil, fmt.Errorf("aeon: the state dir will not resolve: %v", err)
	}
	st, err := openStoreSkipMigrations()
	if err != nil {
		return nil, fmt.Errorf("aeon: the store will not open for the direct backup door: %v", err)
	}
	dep := backup.RealDeps(st, Version)
	dep.StateDir = stateDir
	return &backupDirectDoor{st: st, dep: dep}, nil
}

// Close closes the door's store.
func (d *backupDirectDoor) Close() error { return d.st.Close() }

// now is the door's clock: the engine's injected one when set, the
// process clock otherwise (RealDeps defaults it) — the daemon seam's
// twin.
func (d *backupDirectDoor) now() time.Time {
	if d.dep.Clock != nil {
		return d.dep.Clock()
	}
	return time.Now()
}

// resolved answers the ten backup keys. A settings state the resolver
// refuses is an error, not a guess (the daemon seam's twin, word for
// word).
func (d *backupDirectDoor) resolved() (backup.Resolved, error) {
	return backup.ResolveSettings(d.dep.Settings)
}

// mainBackupDir is the configured dir or the default under the state
// dir — the same resolution a run performs.
func (d *backupDirectDoor) mainBackupDir(resolved backup.Resolved) string {
	if resolved.Dir != "" {
		return resolved.Dir
	}
	if d.dep.StateDir != "" {
		return backup.DefaultDir(d.dep.StateDir)
	}
	return ""
}

// preMigrationBackupDir mirrors the pre-migration hook's own resolution
// (T5): the configured dir's pre-migration subfolder, or the default
// dir's — the door and the hook see the same files.
func (d *backupDirectDoor) preMigrationBackupDir(resolved backup.Resolved) string {
	base := d.mainBackupDir(resolved)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "pre-migration")
}

// backupDirs is the listing set for a given ask: the main dir, plus the
// pre-migration subfolder when asked.
func (d *backupDirectDoor) backupDirs(resolved backup.Resolved, includePreMigration bool) []string {
	dirs := []string{}
	main := d.mainBackupDir(resolved)
	if main != "" {
		dirs = append(dirs, main)
	}
	if includePreMigration {
		if pre := d.preMigrationBackupDir(resolved); pre != "" && pre != main {
			dirs = append(dirs, pre)
		}
	}
	return dirs
}

// resolveFile finds a backup's basename inside the configured dirs and
// answers the path it lives at — the daemon seam's twin, the check kept
// here and not only on the wire (a door that accepted a path would be a
// door that read arbitrary files).
func (d *backupDirectDoor) resolveFile(resolved backup.Resolved, file string) (string, bool) {
	if strings.ContainsAny(file, `/\`) {
		return "", false
	}
	for _, dir := range d.backupDirs(resolved, true) {
		full := filepath.Join(dir, file)
		if fi, err := os.Stat(full); err == nil && fi.Mode().IsRegular() {
			return full, true
		}
	}
	return "", false
}

// toWireView is the engine's list view in the wire's shape: the
// manifest's facts, the age against the door's clock, and the ledger's
// joined verdict. No path field at all (BK12) — the file's name is its
// identity.
func (d *backupDirectDoor) toWireView(v backup.BackupView, now time.Time) protocol.BackupView {
	age := int64(0)
	if !v.CreatedAt.IsZero() {
		age = int64(now.Sub(v.CreatedAt).Seconds())
	}
	created := ""
	if !v.CreatedAt.IsZero() {
		created = v.CreatedAt.UTC().Format(directStampFormat)
	}
	return protocol.BackupView{
		FileName:                 v.FileName,
		CreatedAt:                created,
		AgeSeconds:               age,
		Reason:                   v.Reason,
		AeonVersion:              v.AeonVersion,
		Applied:                  v.Applied,
		LastMigration:            v.LastMigration,
		SizeBytes:                v.SizeBytes,
		SHA256:                   v.SHA256,
		Verified:                 v.Verified,
		CanRestoreWithThisBinary: v.CanRestoreWithThisBinary,
	}
}

// directRefused renders the engine's refusal as the wire's (the
// protocol's backupRefusedError, mirrored here so the two doors'
// refusals render byte-identical through renderError): one code
// (ErrBackupRefused), the closed-set kind in Data.Kind, the person's
// sentence in What.
func directRefused(verb string, kind, what string) *protocol.Error {
	e := protocol.ErrBackupRefused.WithWhat("backup %s refused: %s", verb, what)
	e.Data.Kind = kind
	return e
}

// --- the five doors --------------------------------------------------------------

// Run is the backup.run door: one manual backup, the CLI's verify ask
// overriding the setting for this call alone.
func (d *backupDirectDoor) Run(ctx context.Context, verify *bool) (protocol.BackupRunResult, error) {
	res, err := backup.Run(ctx, d.dep, backup.Options{
		Reason:      "manual",
		ActorKind:   directActorKind,
		VerifyAfter: verify,
	})
	if err != nil {
		var re backup.RunError
		if errors.As(err, &re) {
			return protocol.BackupRunResult{}, directRefused("run", re.Kind, re.What)
		}
		return protocol.BackupRunResult{}, fmt.Errorf("aeon: backup.run (direct door): %v", err)
	}
	out := protocol.BackupRunResult{
		Run: protocol.BackupRunView{
			FileName:      res.FileName,
			SizeBytes:     res.SizeBytes,
			SHA256:        res.SHA256,
			Applied:       res.Applied,
			LastMigration: res.LastMigration,
			VerifyLevel:   res.VerifyLevel,
			FreeSpace:     string(res.FreeSpace),
			LockReclaimed: res.LockReclaimed,
			Recorded:      res.Recorded,
		},
	}
	// The file as the list will show it: the manifest's facts joined with
	// the ledger's verdict — the run's answer carries the list's own
	// shape, not a second one (the daemon seam's twin).
	if resolved, rerr := d.resolved(); rerr == nil {
		if views, lerr := backup.List(d.backupDirs(resolved, true), d.st); lerr == nil {
			for i := range views {
				if views[i].FileName == res.FileName {
					v := d.toWireView(views[i], d.now())
					out.Backup = &v
					break
				}
			}
		}
	}
	return out, nil
}

// List answers what files exist in the configured dirs (the pre-migration
// subdir included iff asked), newest first, each joined with its latest
// verify verdict and its can-restore answer.
func (d *backupDirectDoor) List(ctx context.Context, limit int, includePreMigration bool) (protocol.BackupListResult, error) {
	resolved, err := d.resolved()
	if err != nil {
		return protocol.BackupListResult{}, fmt.Errorf("aeon: backup.list (direct door): %v", err)
	}
	dirs := d.backupDirs(resolved, includePreMigration)
	views, err := backup.List(dirs, d.st)
	if err != nil {
		return protocol.BackupListResult{}, fmt.Errorf("aeon: backup.list (direct door): %v", err)
	}
	now := d.now()
	out := make([]protocol.BackupView, 0, len(views))
	for i := range views {
		out = append(out, d.toWireView(views[i], now))
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return protocol.BackupListResult{Backups: out, Dirs: dirs}, nil
}

// Verify checks one named file at level 2 (verified) or 3 (the drill),
// and records the verdict in the ledger (BK10 — the verdict lives in the
// ledger, the file is immutable once published): the daemon seam's
// recordVerifyRow, mirrored — same kind, reason, fixed detail
// templates, stamps; the actor this door stamps.
func (d *backupDirectDoor) Verify(ctx context.Context, file string, drill bool) (protocol.BackupVerifyResult, error) {
	resolved, err := d.resolved()
	if err != nil {
		return protocol.BackupVerifyResult{}, fmt.Errorf("aeon: backup.verify (direct door): %v", err)
	}
	path, found := d.resolveFile(resolved, file)
	if !found {
		detail := fmt.Sprintf("no backup file named %q — the configured backup dirs hold no such file", file)
		if strings.ContainsAny(file, `/\`) {
			detail = fmt.Sprintf("%q is a path, not a backup file name — the socket names files by basename; a path belongs to the CLI's direct door", file)
		}
		return protocol.BackupVerifyResult{}, directRefused("verify", "unknown", detail)
	}
	level := 2
	if drill {
		level = 3
	}
	started := d.now()
	rep, verr := backup.Verify(ctx, d.dep, path, level)
	var re backup.RunError
	if verr != nil && !errors.As(verr, &re) {
		return protocol.BackupVerifyResult{}, fmt.Errorf("aeon: backup.verify (direct door): %v", verr)
	}
	ok := rep.OK
	errorKind := ""
	if verr != nil {
		ok = false
		errorKind = re.Kind
	} else if !ok && rep.NewerSchema {
		errorKind = "newer-schema"
	}
	row, rerr := d.recordVerifyRow(started, file, level, ok, errorKind)
	if rerr != nil {
		// The verdict is a fact even when the ledger cannot take it: the
		// answer stands, the gap is spoken, never silent (the daemon seam
		// logs it; this door has no daemon log, so stderr takes the
		// sentence).
		fmt.Fprintln(os.Stderr, "aeon: the verify verdict could not be recorded in the ledger: "+rerr.Error())
	}
	if verr != nil {
		return protocol.BackupVerifyResult{}, directRefused("verify", re.Kind, re.What)
	}
	out := protocol.BackupVerifyResult{
		Run: protocol.BackupVerifyRunView{
			Outcome:    row.Outcome,
			ErrorKind:  strPtr(row.ErrorKind),
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
		},
		Report: protocol.BackupVerifyView{
			FileName:     rep.FileName,
			Level:        rep.Level,
			OK:           rep.OK,
			NewerSchema:  rep.NewerSchema,
			Unknown:      rep.Unknown,
			Sentence:     rep.Sentence,
			DrillApplied: rep.DrillApplied,
		},
	}
	return out, nil
}

// recordVerifyRow is the verify's ledger row (BK10): the daemon seam's
// recordVerifyRow, mirrored — the detail is one of the three fixed
// templates (SEC-033).
func (d *backupDirectDoor) recordVerifyRow(started time.Time, file string, level int, ok bool, errorKind string) (store.BackupRun, error) {
	levelName := "verified"
	switch level {
	case 1:
		levelName = "written"
	case 3:
		levelName = "drill"
	}
	detail := "verify: failed"
	if ok {
		detail = "verify: ok"
		if level == 3 {
			detail = "drill: ok"
		}
	}
	return d.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "verify",
		Reason:         "manual",
		ActorKind:      directActorKind,
		StartedAt:      started.UTC().Format(directStampFormat),
		Outcome:        outcomeOf(ok),
		ErrorKind:      errorKind,
		Detail:         detail,
		FileName:       file,
		VerifyLevel:    levelName,
		FreeSpaceState: "unknown",
	})
}

// Status answers the four-state machine (never/fresh/stale/failing) with
// the numbers behind it, the ten settings with their layers, and the
// database's own size facts from the store's one Info() call — the
// daemon seam's twin over this door's SkipMigrations handle (an Info of
// a not-yet-migrated file is the honest state of it, not a guess).
func (d *backupDirectDoor) Status(ctx context.Context) (protocol.BackupStatusView, error) {
	now := d.now()
	view, err := backup.Status(d.dep, d.st, now)
	if err != nil {
		return protocol.BackupStatusView{}, fmt.Errorf("aeon: backup.status (direct door): %v", err)
	}
	out := protocol.BackupStatusView{
		State:        view.State,
		Enabled:      view.Enabled,
		Dir:          view.Dir,
		LastSuccess:  view.LastSuccessAt,
		LastVerified: view.LastVerifyAt,
		AgeSeconds:   view.AgeSeconds,
		NextDueAt:    view.NextDueAt,
		Running:      view.Running,
		Settings:     settingsViews(view.Settings),
		DB:           d.dbView(),
	}
	return out, nil
}

// dbView is the database's own size facts — one Info() call covers the
// whole block (the daemon seam's twin). The one state the twin never
// meets: a store with a FILE but no TABLES — a fresh machine's first
// verb on this door, the file the door's own open just made. Info()
// needs the ledger table that does not exist yet, and the block then
// falls back to the file's own facts (store.ReadOnlyFacts, the store's
// own read-only answer) plus the registry's total: the journal the file
// runs in, the zero the missing ledger says, and the 79 the registry
// knows are pending — the truth stated, never the zeros of a failed
// call (P31).
func (d *backupDirectDoor) dbView() protocol.BackupDBView {
	info, err := d.st.Info()
	if err == nil {
		return protocol.BackupDBView{
			Bytes:       info.Bytes,
			WALBytes:    info.WALBytes,
			JournalMode: info.JournalMode,
			Applied:     len(info.Applied),
			Pending:     len(info.Pending),
		}
	}
	facts, ferr := store.ReadOnlyFacts(d.st.Path())
	if ferr != nil || !facts.Readable {
		return protocol.BackupDBView{}
	}
	pending := len(store.Migrations()) - facts.Applied
	if pending < 0 {
		pending = 0
	}
	view := protocol.BackupDBView{
		JournalMode: facts.JournalMode,
		Applied:     facts.Applied,
		Pending:     pending,
	}
	if fi, serr := os.Stat(d.st.Path()); serr == nil {
		view.Bytes = fi.Size()
	}
	if fi, serr := os.Stat(d.st.Path() + "-wal"); serr == nil {
		view.WALBytes = fi.Size()
	}
	return view
}

// Prune plans the retention policy (always) and applies it (fenced) when
// not a dry run — the daemon seam's twin, minus the event (a stopped
// daemon has no stream; the plan's removed list is the record, and the
// ledger row says the same count).
func (d *backupDirectDoor) Prune(ctx context.Context, dryRun bool) (protocol.BackupPruneView, error) {
	resolved, err := d.resolved()
	if err != nil {
		return protocol.BackupPruneView{}, fmt.Errorf("aeon: backup.prune (direct door): %v", err)
	}
	dirs := d.backupDirs(resolved, true)
	views, err := backup.List(dirs, d.st)
	if err != nil {
		return protocol.BackupPruneView{}, fmt.Errorf("aeon: backup.prune (direct door): %v", err)
	}
	now := d.now()
	plan := backup.Plan(views, resolved.Policy(), now)
	out := protocol.BackupPruneView{Plan: make([]protocol.BackupPruneDecision, 0, len(plan))}
	for _, pd := range plan {
		out.Plan = append(out.Plan, protocol.BackupPruneDecision{FileName: pd.FileName, Keep: pd.Keep, Reason: pd.Why})
	}
	if dryRun {
		return out, nil
	}
	removed, aerr := backup.Apply(plan)
	if aerr != nil {
		return out, fmt.Errorf("aeon: backup.prune (direct door): %v", aerr)
	}
	out.Removed = removed
	if rerr := d.recordPruneRow(now, len(removed)); rerr != nil {
		return out, fmt.Errorf("aeon: backup.prune (direct door): record the act: %v", rerr)
	}
	return out, nil
}

// recordPruneRow is the prune's ledger row: the act and its count in a
// fixed template (SEC-033) — the daemon seam's recordPruneRow,
// mirrored.
func (d *backupDirectDoor) recordPruneRow(started time.Time, removed int) error {
	_, err := d.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "prune",
		Reason:         "manual",
		ActorKind:      directActorKind,
		StartedAt:      started.UTC().Format(directStampFormat),
		Outcome:        "ok",
		Detail:         fmt.Sprintf("prune: %d removed", removed),
		FreeSpaceState: "unknown",
	})
	return err
}

// --- the small shared voices --------------------------------------------------------

// outcomeOf is the ledger's outcome column for a boolean: ok | failed
// (the daemon seam's voice, mirrored so the two doors stamp the same
// words).
func outcomeOf(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}

// strPtr is the *string -> "" voice for the ledger's nullable columns
// (the daemon seam's strvRow, mirrored).
func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// settingsViews is the status's settings block: the ten keys in the
// resolver's own order, each split into the value and the layer it came
// from — the daemon seam's voice, the split on the FIRST comma (a dir
// that carries a comma stays a value, not a source).
func settingsViews(flat map[string]string) []protocol.BackupSettingView {
	out := make([]protocol.BackupSettingView, 0, len(backup.AllKeys))
	for _, key := range backup.AllKeys {
		joined, ok := flat[key]
		if !ok {
			continue
		}
		i := strings.Index(joined, ",")
		if i < 0 {
			out = append(out, protocol.BackupSettingView{Key: key, Value: joined})
			continue
		}
		out = append(out, protocol.BackupSettingView{Key: key, Value: joined[:i], Source: joined[i+1:]})
	}
	return out
}
