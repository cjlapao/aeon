package daemon

// export_service.go — S62 T8 (BK14): the daemon's one export service.
//
// It is the daemon half of the export family: the two doors of
// protocol.ExportStore (run/sets) over the real store. It knows both shapes —
// the engine's (internal/export, a pure leaf) and the protocol's wire twins —
// and it is the only place allowed to, the adapter rule the backup family's
// service carries. The engine itself knows nothing of the protocol, the
// ledger or the settings: the service is where those meet the engine's
// Reader, which is the store's own export seam (internal/store/export.go).
//
// The ledger row is the service's, not the engine's: the engine is a leaf
// that writes files, and the row is a fact of the daemon's bookkeeping,
// written when — and only when — the request reached the engine, which from
// this service's seat is always (the protocol door has already refused the
// param-shape faults before the seam). outcome ok, or failed with the
// engine's kind mapped into the ledger's closed error_kind set; a row the
// store refuses is logged, never retried (the T6 precedent).
//
// Absence is V13's: NewExportService answers nil for a storeless daemon, the
// wiring leaves table.Export nil, RegisterExport leaves both doors ABSENT —
// not registered-and-refusing.

import (
	"context"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/export"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// ExportService is the daemon's one export half (see the file header). The
// fields are the store (the real one), the binary version the manifest
// carries, the log voice, and the clock a test may set.
type ExportService struct {
	st      *store.Store
	version string
	log     Logger
	// clock is the test seam for the process clock; nil runs on time.Now().
	clock func() time.Time
}

// NewExportService builds the service over the live store. A nil store
// answers nil (V13: no store, no service, the two doors absent). version is
// the daemon's binary version — the one the manifest's aeon_version field
// carries.
func NewExportService(st *store.Store, version string, log Logger) *ExportService {
	if st == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return &ExportService{st: st, version: version, log: log}
}

func (s *ExportService) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *ExportService) logf(format string, args ...any) { s.log.Infof(format, args...) }

var _ protocol.ExportStore = (*ExportService)(nil)

// deps is the engine's Deps over the live store: the Reader is the store's
// own export seam (the allow-listed reads), the Clock the service's, and the
// version and schema the binary's — the daemon migrates the store at open,
// so the binary's migration count is the store's schema while it serves.
func (s *ExportService) deps() export.Deps {
	return export.Deps{
		Reader:  s.st,
		Clock:   s.now,
		Version: s.version,
		Schema:  len(store.Migrations()),
	}
}

// exportErrorKind maps the engine's closed RunError kinds into the ledger's
// closed error_kind set (the store's CHECK admits exactly the set, and
// nothing outside it): the kinds that name a state the ledger knows keep a
// name the ledger has, the rest are unknown rather than guessed. file-oversize
// is a disk-full in the ledger's vocabulary (the file refused to fit), the
// rest of the engine's kinds (the dir refusals, unknown-set) are shape faults
// the ledger does not classify and stay unknown.
func exportErrorKind(kind string) string {
	switch kind {
	case export.ErrKindOversize:
		return "disk-full"
	case export.ErrKindIO:
		return "io"
	case export.ErrKindCancelled:
		return "cancelled"
	case export.ErrKindDirUnwritable:
		return "dir-unwritable"
	default:
		return "unknown"
	}
}

// Run is the export.run door: it runs the engine over the requested sets
// into the named dir, records the ledger's row, and answers the wire view.
// The actor is the door's stamp (the protocol handler already refused the
// unstamped and the identity-keyed); the engine stamps it into the manifest
// as the person who asked. A RunError is the family's refusal result (kind
// plus the person's sentence, the -31863 the protocol door renders) — the
// ledger's row still stands, because the request reached the engine.
//
// The engine's sentence is re-voiced by this door on the wire: the protocol
// handler renders "export run refused: " + the detail, so the engine's own
// "export: " prefix is stripped here or the wire would carry it twice. The
// CLI's direct door keeps the prefix (it has no handler to re-voice the
// sentence) — the asymmetry is the door's, recorded where the direct door
// lives (cmd/aeon/export.go).
func (s *ExportService) Run(ctx context.Context, actor protocol.Actor, params protocol.ExportRunParams) (protocol.ExportRunResult, *protocol.BackupRefusal, error) {
	started := s.now()
	res, runErr := export.Run(ctx, s.deps(), export.Opts{
		Dir:           params.Dir,
		Sets:          params.Sets,
		IncludeMemory: params.IncludeMemory,
		Actor:         export.Actor{Kind: actorKindOf(actor), ID: actor.ID},
	})
	s.recordRow(started, actor, runErr)
	if runErr != nil {
		detail := strings.TrimPrefix(runErr.What, "export: ")
		return protocol.ExportRunResult{}, &protocol.BackupRefusal{Kind: runErr.Kind, Detail: detail}, nil
	}
	view := protocol.ExportRunResultFromEngine(res)
	view.Recorded = true // the row was written above
	return view, nil, nil
}

// recordRow is the export's ledger row (BK10's one row per run): kind
// export, reason manual from this door, the actor stamped, the outcome and
// the mapped error kind. The detail is a fixed template (SEC-033 — the
// engine's sentence carries the dir's path, and a path in a detail is a
// path that could name the operator's files; the kind and the outcome are
// the columns that say what happened). A row the store refuses is logged,
// never fatal: the files are already where they are, and the answer stands.
func (s *ExportService) recordRow(started time.Time, actor protocol.Actor, runErr *export.RunError) {
	outcome := "ok"
	errorKind := ""
	detail := "export: ok"
	if runErr != nil {
		outcome = "failed"
		errorKind = exportErrorKind(runErr.Kind)
		detail = "export: failed"
	}
	if _, err := s.st.RecordBackupRun(store.NewBackupRun{
		Kind:           "export",
		Reason:         "manual",
		ActorKind:      actorKindOf(actor),
		StartedAt:      started.UTC().Format(backupStampFormat),
		Outcome:        outcome,
		ErrorKind:      errorKind,
		Detail:         detail,
		FreeSpaceState: "unknown",
	}); err != nil {
		s.logf("export.run: record the ledger row: %v", err)
	}
}

// Sets is the export.sets door: the declared sets, in registry order, as
// the schema this store holds sees them. The mapping is protocol's own
// (the CLI's direct door runs the same one over its own probe), with the
// probe being this store's TableExists.
func (s *ExportService) Sets(ctx context.Context) (protocol.ExportSetsResult, error) {
	return protocol.ExportSetsFromProbe(s.st.TableExists), nil
}
