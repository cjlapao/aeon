package daemon

// backup_service.go — S62 T6 (BK7, BK12): the daemon's one backup service.
//
// It is the daemon half of the backup family: the five human-door methods
// of protocol.BackupStore (run/list/verify/status/prune) over the real
// store, and the scheduled maintenance task the ticker runs. It knows both
// shapes — the engine's views and the protocol's wire twins — and it is the
// only place allowed to (the adapter rule, the T0 design's seam).
//
// The ticker (BK7) is a maintenance task, NOT a trigger: it wakes at start
// and every hour, reads the latest successful backup row, and runs one
// scheduled backup when it is due — single-flight through the in-process
// flag (cross-process the engine's own lock answers), context bounded by
// the timeout setting, and "skipped: busy" on the ledger when a run is
// already going or the shutdown has landed on the tick. It never starts a
// loop or touches a trigger table (the TestTickerIsNotATrigger guard
// proves the absence).
//
// Absence is V13's: NewBackupService answers nil for a storeless daemon,
// the wiring leaves table.Backup nil, RegisterBackup leaves the five
// ABSENT — not registered-and-refusing.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/backup"
	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// backupStampFormat is the ledger's one timestamp voice: the store's CHECK
// admits only this shape (T3), and every row this service writes stamps it.
const backupStampFormat = "2006-01-02T15:04:05.000Z"

// backupTickInterval is BK7's wake cadence: at start, then every hour. The
// interval setting decides WHEN a run is due; the cadence decides how soon
// the daemon finds out — a restart-safe schedule, because the next wake
// re-derives the due instant from the ledger, not from memory.
const backupTickInterval = time.Hour

// wireEmitter is the service's backup.Events implementation: the engine's
// backup.finished / backup.failed events reach the socket's event stream
// through the daemon's one publish seam (table.OnEvent), the same path every
// other service's events take. Pre-T6 the CLI door ran the engine with this
// nil and the events went nowhere; the daemon is where they live (BK16).
type wireEmitter struct {
	publish func(kind string, payload any)
}

func (w wireEmitter) Emit(kind string, payload any) { w.publish(kind, payload) }

// BackupService is the daemon's one backup half (see the file header). The
// fields are the store (the real one), the engine's deps over it, the log
// voice, and the publish seam the engine's events and the service's own
// events (stale/pruned) both flow through.
type BackupService struct {
	st      *store.Store
	deps    backup.Deps
	log     Logger
	publish func(kind string, payload any)

	mu sync.Mutex
	// runActive is the in-process single-flight: a tick that lands while a
	// run is going records "skipped: busy" rather than start a second.
	runActive bool
	// staleArmed is the once-per-transition arm of the backup.stale event:
	// it fires on the transition INTO stale and re-arms when the state
	// leaves it. Shared by the ticker and the status door, so whichever
	// sees the transition first speaks — exactly once.
	staleArmed bool
}

// NewBackupService builds the service over the live store. A nil store
// answers nil (V13: no store, no service, the five doors absent). version is
// the daemon's binary version — the one the manifest and the run rows
// carry. settings is the daemon's shared *config.Store (S44 T4): when non-nil
// the ten backup keys are read through it (environment → database → default,
// T10), so a change written by the panel or the CLI is live on the next
// tick; when nil the service keeps the pre-S44 environment settings — a
// test mount with no settings store, not a production daemon, which always
// has one beside its store. publish is the daemon's event seam
// (table.OnEvent); nil answers a no-op, for a mount that keeps the methods
// but not the event stream.
func NewBackupService(st *store.Store, settings *config.Store, version string, publish func(kind string, payload any), log Logger) *BackupService {
	if st == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	if publish == nil {
		publish = func(string, any) {}
	}
	deps := backup.RealDeps(st, version)
	if settings != nil {
		// T10: the database layer joins the read. RealDeps' environment
		// settings are replaced, not augmented — a run sees one value per
		// key, from the highest layer that holds it.
		deps.Settings = backup.NewConfigSettings(settings)
	}
	// The state dir is the store's own dir: the backup's default dir hangs
	// off the database's location, and the database's location is the
	// framework's config — one resolver, no second one (the paths package's
	// rule).
	deps.StateDir = filepath.Dir(st.Path())
	deps.Events = wireEmitter{publish: publish}
	return &BackupService{st: st, deps: deps, log: log, publish: publish, staleArmed: true}
}

var _ protocol.BackupStore = (*BackupService)(nil)

// --- the small helpers -----------------------------------------------------------

func (s *BackupService) logf(format string, args ...any) { s.log.Infof(format, args...) }

// now is the service's clock: the engine's injected one when a test set it,
// the process clock otherwise (RealDeps defaults it).
func (s *BackupService) now() time.Time {
	if s.deps.Clock != nil {
		return s.deps.Clock()
	}
	return time.Now()
}

// resolved answers the ten backup keys. A settings state the resolver
// refuses is an error, not a guess: a status answered from an invalid
// settings state would be a confident wrong answer (P31).
func (s *BackupService) resolved() (backup.Resolved, error) {
	return backup.ResolveSettings(s.deps.Settings)
}

// mainBackupDir is the configured dir or the default under the state dir —
// the same resolution a run performs. The daemon never passes Options.Dir,
// so a run always lands where the settings name it.
func (s *BackupService) mainBackupDir(resolved backup.Resolved) string {
	if resolved.Dir != "" {
		return resolved.Dir
	}
	if s.deps.StateDir != "" {
		return backup.DefaultDir(s.deps.StateDir)
	}
	return ""
}

// preMigrationBackupDir mirrors the pre-migration hook's own resolution
// (T5): the configured dir's pre-migration subfolder, or the default dir's
// — so the doors and the hook see the same files, and a list that shows the
// pre-migration family can also plan and apply a retention over it.
func (s *BackupService) preMigrationBackupDir(resolved backup.Resolved) string {
	base := s.mainBackupDir(resolved)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "pre-migration")
}

// backupDirs is the listing set for a given ask: the main dir, plus the
// pre-migration subfolder when asked.
func (s *BackupService) backupDirs(resolved backup.Resolved, includePreMigration bool) []string {
	dirs := []string{}
	main := s.mainBackupDir(resolved)
	if main != "" {
		dirs = append(dirs, main)
	}
	if includePreMigration {
		if pre := s.preMigrationBackupDir(resolved); pre != "" && pre != main {
			dirs = append(dirs, pre)
		}
	}
	return dirs
}

// resolveFile finds a backup's basename inside the configured dirs and
// answers the path it lives at — the only place a path is built from the
// wire's file name (which the protocol layer already stripped of
// separators, and which the ledger's file_name column carries as a
// basename too).
func (s *BackupService) resolveFile(resolved backup.Resolved, file string) (string, bool) {
	// The wire's file is a BASENAME: a separator in it would make the join
	// below a path, and a ../ in the name an escape out of the configured
	// dirs. The socket refuses the same shape at the wire (the -32602 the
	// protocol layer owns), and this is the service's own twin — the shared
	// door the T7 direct door and any future caller reach, so the check
	// stands here too, not only on the wire (the TestBackupMethodsRefuse
	// Separators guard pins it).
	if strings.ContainsAny(file, `/\`) {
		return "", false
	}
	for _, dir := range s.backupDirs(resolved, true) {
		full := filepath.Join(dir, file)
		if fi, err := os.Stat(full); err == nil && fi.Mode().IsRegular() {
			return full, true
		}
	}
	return "", false
}

// actorKindOf is the ledger's actor from the door's stamp. The socket door
// stamps the peer's person (Kind "user"); the ticker stamps itself
// ("daemon"); the T7 CLI door will stamp "cli". A kind outside the closed
// set is the store's refusal to find, not this service's guess (BK10: the
// actor is stamped, never read from a request).
func actorKindOf(actor protocol.Actor) string {
	switch actor.Kind {
	case "user", "daemon", "cli":
		return actor.Kind
	default:
		return "user"
	}
}

func outcomeOf(ok bool) string {
	if ok {
		return "ok"
	}
	return "failed"
}

// strvRow is the *string -> "" voice for the ledger's nullable columns.
func strvRow(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// toWireView is the engine's list view in the wire's shape: the manifest's
// facts, the age against the service's clock, and the ledger's joined
// verdict. No path field at all (BK12) — the file's name is its identity,
// and the dir it sits in is a fact the list's dirs and the status already
// report.
func (s *BackupService) toWireView(v backup.BackupView, now time.Time) protocol.BackupView {
	age := int64(0)
	if !v.CreatedAt.IsZero() {
		age = int64(now.Sub(v.CreatedAt).Seconds())
	}
	created := ""
	if !v.CreatedAt.IsZero() {
		created = v.CreatedAt.UTC().Format(backupStampFormat)
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

// --- the five doors ----------------------------------------------------------------

// Run is the backup.run door: one manual backup, the socket's verify ask
// overriding the setting for this call alone. The engine's refusals are
// results here — a second return the handler shapes into ErrBackupRefused
// with the reason in `what` (BK12) — never errors, which are reserved for
// the infrastructure failing under the door.
func (s *BackupService) Run(ctx context.Context, actor protocol.Actor, verify *bool) (protocol.BackupRunResult, *protocol.BackupRefusal, error) {
	res, err := backup.Run(ctx, s.deps, backup.Options{
		Reason:      "manual",
		ActorKind:   actorKindOf(actor),
		VerifyAfter: verify,
	})
	if err != nil {
		var re backup.RunError
		if errors.As(err, &re) {
			return protocol.BackupRunResult{}, &protocol.BackupRefusal{Kind: re.Kind, Detail: re.What}, nil
		}
		return protocol.BackupRunResult{}, nil, fmt.Errorf("daemon: backup.run: %w", err)
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
	// the ledger's verdict. The run's answer carries the list's own shape —
	// not a second one — so a client sees its fresh backup the way every
	// other backup is shown.
	if resolved, rerr := s.resolved(); rerr == nil {
		if views, lerr := backup.List(s.backupDirs(resolved, true), s.st); lerr == nil {
			for i := range views {
				if views[i].FileName == res.FileName {
					v := s.toWireView(views[i], s.now())
					out.Backup = &v
					break
				}
			}
		}
	}
	return out, nil, nil
}

// List answers what files exist in the configured dirs (the pre-migration
// subdir included iff asked), newest first, each joined with its latest
// verify verdict and its can-restore answer. Dirs is the one place a dir
// is named on the wire of this family — as a setting.
func (s *BackupService) List(ctx context.Context, limit int, includePreMigration bool) (protocol.BackupListResult, error) {
	resolved, err := s.resolved()
	if err != nil {
		return protocol.BackupListResult{}, fmt.Errorf("daemon: backup.list: %w", err)
	}
	dirs := s.backupDirs(resolved, includePreMigration)
	views, err := backup.List(dirs, s.st)
	if err != nil {
		return protocol.BackupListResult{}, fmt.Errorf("daemon: backup.list: %w", err)
	}
	now := s.now()
	out := make([]protocol.BackupView, 0, len(views))
	for i := range views {
		out = append(out, s.toWireView(views[i], now))
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return protocol.BackupListResult{Backups: out, Dirs: dirs}, nil
}

// Verify checks one named file at level 2 (verified) or 3 (the drill), and
// records the verdict in the ledger (BK10 — the verdict lives in the
// ledger, the file is immutable once published): kind verify, reason manual
// from this door, the actor stamped, the level, the outcome, and one of the
// three fixed detail templates. A file the configured dirs do not hold is a
// refusal that names the absence; the engine's hard refusals ride back as
// the family's one refusal result; a level that failed softly (the counts
// disagree) is a report with OK false — an answer, not an error.
func (s *BackupService) Verify(ctx context.Context, actor protocol.Actor, file string, drill bool) (protocol.BackupVerifyResult, *protocol.BackupRefusal, error) {
	resolved, err := s.resolved()
	if err != nil {
		return protocol.BackupVerifyResult{}, nil, fmt.Errorf("daemon: backup.verify: %w", err)
	}
	path, found := s.resolveFile(resolved, file)
	if !found {
		detail := fmt.Sprintf("no backup file named %q — the configured backup dirs hold no such file", file)
		if strings.ContainsAny(file, `/\`) {
			detail = fmt.Sprintf("%q is a path, not a backup file name — the socket names files by basename; a path belongs to the CLI's direct door", file)
		}
		return protocol.BackupVerifyResult{}, &protocol.BackupRefusal{Kind: "unknown", Detail: detail}, nil
	}
	level := 2
	if drill {
		level = 3
	}
	started := s.now()
	rep, verr := backup.Verify(ctx, s.deps, path, level)
	var re backup.RunError
	if verr != nil && !errors.As(verr, &re) {
		return protocol.BackupVerifyResult{}, nil, fmt.Errorf("daemon: backup.verify: %w", verr)
	}
	ok := rep.OK
	errorKind := ""
	if verr != nil {
		ok = false
		errorKind = re.Kind
	} else if !ok && rep.NewerSchema {
		// The one soft failure the report names by its kind: the ledger's
		// column gets it too, and the rest stay unclassified rather than
		// guessed (the report carries no kind for them).
		errorKind = "newer-schema"
	}
	row, rerr := s.recordVerifyRow(started, actor, file, level, ok, errorKind)
	if rerr != nil {
		// The verdict is a fact even when the ledger cannot take it: the
		// answer stands, the gap is spoken, never silent.
		s.logf("backup.verify: record the verdict: %v", rerr)
	}
	if verr != nil {
		return protocol.BackupVerifyResult{}, &protocol.BackupRefusal{Kind: re.Kind, Detail: re.What}, nil
	}
	out := protocol.BackupVerifyResult{
		Run: protocol.BackupVerifyRunView{
			Outcome:    row.Outcome,
			ErrorKind:  strvRow(row.ErrorKind),
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
	return out, nil, nil
}

// recordVerifyRow is the verify's ledger row (BK10). The detail is one of
// three fixed templates (SEC-033) — the level and the outcome are columns,
// and the sentence that explains the failure stays on the wire, where the
// person reading it wants it.
func (s *BackupService) recordVerifyRow(started time.Time, actor protocol.Actor, file string, level int, ok bool, errorKind string) (store.BackupRun, error) {
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
	return s.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "verify",
		Reason:         "manual",
		ActorKind:      actorKindOf(actor),
		StartedAt:      started.UTC().Format(backupStampFormat),
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
// database's own size facts from the store's one Info() call. Answering it
// also observes the stale transition (the status door may see the
// transition before the next tick does — and may be the only door, on a
// daemon whose ticker the operator is not waiting on).
func (s *BackupService) Status(ctx context.Context) (protocol.BackupStatusView, error) {
	now := s.now()
	view, err := backup.Status(s.deps, s.st, now)
	if err != nil {
		return protocol.BackupStatusView{}, fmt.Errorf("daemon: backup.status: %w", err)
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
		Settings:     s.settingsViews(view.Settings),
		DB:           s.dbView(),
	}
	if resolved, rerr := s.resolved(); rerr == nil {
		s.observeStale(out.State, out.AgeSeconds, resolved.StaleAfter)
	}
	return out, nil
}

// settingsViews is the status's settings block: the ten keys in the
// resolver's own order, each split into the value and the layer it came
// from. The split is on the FIRST comma — valueSource's voice is
// value-comma-source — so a dir that carries a comma stays a value, not a
// source.
func (s *BackupService) settingsViews(flat map[string]string) []protocol.BackupSettingView {
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

// dbView is the database's own size facts — one Info() call covers the
// whole block: the bytes, the WAL, the journal mode, the applied and
// pending migration counts. A store that cannot answer its own facts gets
// the honest zeros, not a guess (P31's third value in a block that has no
// room for one).
func (s *BackupService) dbView() protocol.BackupDBView {
	info, err := s.st.Info()
	if err != nil {
		return protocol.BackupDBView{}
	}
	return protocol.BackupDBView{
		Bytes:       info.Bytes,
		WALBytes:    info.WALBytes,
		JournalMode: info.JournalMode,
		Applied:     len(info.Applied),
		Pending:     len(info.Pending),
	}
}

// Prune plans the retention policy (always) and applies it (fenced) when
// not a dry run. The plan covers BOTH dirs — the pre-migration family is
// governed by its own keep count inside the same plan, so a plan that
// cannot see it would be a plan it cannot execute. The act is recorded
// (kind prune) and the backup.pruned event fires when at least one file
// went — the event the pre-migration hook's post-success prune pointed at
// (T5: the daemon ticker's to emit).
func (s *BackupService) Prune(ctx context.Context, actor protocol.Actor, dryRun bool) (protocol.BackupPruneView, *protocol.BackupRefusal, error) {
	resolved, err := s.resolved()
	if err != nil {
		return protocol.BackupPruneView{}, nil, fmt.Errorf("daemon: backup.prune: %w", err)
	}
	dirs := s.backupDirs(resolved, true)
	views, err := backup.List(dirs, s.st)
	if err != nil {
		return protocol.BackupPruneView{}, nil, fmt.Errorf("daemon: backup.prune: %w", err)
	}
	now := s.now()
	plan := backup.Plan(views, resolved.Policy(), now)
	out := protocol.BackupPruneView{Plan: make([]protocol.BackupPruneDecision, 0, len(plan))}
	for _, d := range plan {
		out.Plan = append(out.Plan, protocol.BackupPruneDecision{FileName: d.FileName, Keep: d.Keep, Reason: d.Why})
	}
	if dryRun {
		return out, nil, nil
	}
	removed, aerr := backup.Apply(plan)
	if aerr != nil {
		return out, nil, fmt.Errorf("daemon: backup.prune: %w", aerr)
	}
	out.Removed = removed
	if rerr := s.recordPruneRow(now, actor, len(removed)); rerr != nil {
		return out, nil, fmt.Errorf("daemon: backup.prune: record the act: %w", rerr)
	}
	if len(removed) > 0 {
		s.publish(protocol.KindBackupPruned, map[string]any{
			"removed":    len(removed),
			"file_names": removed,
		})
	}
	return out, nil, nil
}

// recordPruneRow is the prune's ledger row: the act and its count in a
// fixed template (SEC-033 — the count is a number, the names are the
// event's and the files' own, and the detail never carries a path).
func (s *BackupService) recordPruneRow(started time.Time, actor protocol.Actor, removed int) error {
	_, err := s.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "prune",
		Reason:         "manual",
		ActorKind:      actorKindOf(actor),
		StartedAt:      started.UTC().Format(backupStampFormat),
		Outcome:        "ok",
		Detail:         fmt.Sprintf("prune: %d removed", removed),
		FreeSpaceState: "unknown",
	})
	return err
}

// --- the stale transition ----------------------------------------------------------

// observeStale is the once-per-transition arm (BK16): backup.stale fires
// when the machine enters stale while armed, and re-arms the moment the
// machine leaves stale, so the next transition fires again. The payload is
// typed — the state's name, the age, the threshold: ids/enums/numbers, no
// strings the engine did not own.
func (s *BackupService) observeStale(state string, ageSeconds int64, staleAfter time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case state == backup.StateStale && s.staleArmed:
		s.staleArmed = false
		s.publish(protocol.KindBackupStale, map[string]any{
			"state":               state,
			"age_seconds":         ageSeconds,
			"stale_after_seconds": int64(staleAfter / time.Second),
		})
	case state != backup.StateStale:
		s.staleArmed = true
	}
}

// refreshStale recomputes the four-state machine and observes the
// transition, from the ticker's side: the daemon that was down through the
// stale window is the one that most needs the notice, and the first wake
// after the downtime is when it finds out.
func (s *BackupService) refreshStale(now time.Time) {
	resolved, err := s.resolved()
	if err != nil {
		return
	}
	view, err := backup.Status(s.deps, s.st, now)
	if err != nil {
		return
	}
	s.observeStale(view.State, view.AgeSeconds, resolved.StaleAfter)
}

// --- the ticker (BK7) ------------------------------------------------------------------

// TickLoop is BK7's loop: wake at start, then every hour, run the one
// scheduled backup when it is due. The first tick also sweeps the crash
// garbage a dead daemon left in the backup dirs. It returns when the
// context is cancelled — the daemon's recovery/run context, so SIGTERM
// stops it within the shutdown grace — and it never starts a loop or
// touches a trigger table: the schedule is a maintenance task, and the
// TestTickerIsNotATrigger guard pins the absence.
func (s *BackupService) TickLoop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(backupTickInterval)
	defer ticker.Stop()
	s.tick(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.tick(ctx, false)
		}
	}
}

// tick is one wake. The order is the schedule: sweep (first wake only),
// observe the stale transition, decide whether a run is due, and run it
// single-flight under the timeout. Every skip that IS a fact (a run in
// progress, a shutdown on the tick) is recorded on the ledger as
// "skipped: busy" — the schedule's own voice, a fixed template.
func (s *BackupService) tick(ctx context.Context, first bool) {
	now := s.now()
	resolved, err := s.resolved()
	if err != nil {
		s.logf("backup schedule: the settings do not resolve: %v", err)
		return
	}
	if first {
		// The crash garbage of a dead daemon, swept at the first wake
		// (BK4): the .partial tars and the .tmp-* build dirs in BOTH dirs —
		// the fence deletes its own names only, everything else stays.
		for _, dir := range s.backupDirs(resolved, true) {
			if removed, serr := backup.SweepPartials(dir); serr != nil {
				s.logf("backup schedule: sweep the partials: %v", serr)
			} else if len(removed) > 0 {
				s.logf("backup schedule: swept %d partial file(s) at start", len(removed))
			}
		}
	}
	s.refreshStale(now)
	// A shutdown that landed on the tick does not start a run it cannot
	// finish: the tick records its skip and stops (the run in progress gets
	// the timeout's cancel, not a second run).
	if ctx.Err() != nil {
		s.recordSkippedBusy()
		return
	}
	s.mu.Lock()
	if s.runActive {
		s.mu.Unlock()
		s.recordSkippedBusy()
		return
	}
	s.runActive = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.runActive = false
		s.mu.Unlock()
	}()
	if !resolved.Enabled {
		return // disabled ⇒ never
	}
	// The latest successful backup (kind=backup, outcome=ok), newest first:
	// the due instant is re-derived from the ledger on every wake, which is
	// what makes the schedule restart-safe — no memory of a run that was
	// ever made.
	rows, err := s.st.ListBackupRuns(store.BackupRunFilter{Kind: "backup", Outcome: "ok", Limit: 1})
	if err != nil {
		s.logf("backup schedule: read the ledger: %v", err)
		return
	}
	if len(rows) > 0 {
		if lastAt, perr := time.Parse(backupStampFormat, rows[0].FinishedAt); perr == nil && now.Sub(lastAt) < resolved.Interval {
			return // not due yet
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, resolved.Timeout)
	defer cancel()
	if _, rerr := backup.Run(runCtx, s.deps, backup.Options{Reason: "scheduled", ActorKind: "daemon"}); rerr != nil {
		s.logf("backup schedule: the run ended: %v", rerr)
	}
	s.refreshStale(s.now())
}

// recordSkippedBusy is the ledger's voice for a tick that cannot run: the
// run in progress (single-flight) or the shutdown the tick landed on. The
// detail is the fixed template "skipped: busy" — never the reason's text
// (SEC-033), and a row with a reason, so the schedule's history shows the
// skip as a fact of the schedule, not of the caller.
func (s *BackupService) recordSkippedBusy() {
	_, err := s.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "backup",
		Reason:         "scheduled",
		ActorKind:      "daemon",
		StartedAt:      s.now().UTC().Format(backupStampFormat),
		Outcome:        "skipped",
		Detail:         "skipped: busy",
		FreeSpaceState: "unknown",
	})
	if err != nil {
		s.logf("backup schedule: record the skipped tick: %v", err)
	}
}

// --- the daemon half --------------------------------------------------------------------

// RunBackups is serve's backup maintenance task (F15): the schedule,
// started after recovery like the other tickers and on the recovery
// context, so SIGTERM cancels it within the shutdown grace. The nil guards
// are V13's: no store, no schedule, nothing to cancel — and a daemon built
// without the wiring gets the service here (lazily), once, before the loop.
func (d *Daemon) RunBackups(ctx context.Context) error {
	if d.store == nil {
		return nil
	}
	if d.backup == nil {
		d.backup = NewBackupService(d.store, d.settings, d.Table.DaemonVersion, func(kind string, payload any) {
			d.Table.OnEvent(kind, payload)
		}, d.log)
	}
	return d.backup.TickLoop(ctx)
}
