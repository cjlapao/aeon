package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/export"
)

// exportSetNames and exportSetNameList are the closed set's names in the
// engine's own words: the wire refuses a param that spells a set the engine
// does not declare, and the engine's registry is the one definition of the
// set — a second literal here would be a drift site. The wire judges ONLY
// spelling; the memory/flag interplay is the engine's refusal (an explicit
// "memory" without the flag reaches the engine and is refused there,
// naming the flag).
var exportSetNameList = export.Names()

var exportSetNames = func() map[string]bool {
	m := make(map[string]bool, len(exportSetNameList))
	for _, n := range exportSetNameList {
		m[n] = true
	}
	return m
}()

// export.go — S62 T8 (BK14): the two export doors (run/sets), the human door
// over the daemon's one export service.
//
// The seam is the daemon's ExportService (internal/daemon/export_service.go),
// which implements ExportStore over the export engine (internal/export) and
// its store seam (internal/store/export.go). The engine is a pure leaf: the
// daemon's service is the only layer that may know both the engine's types
// and this package's wire twins, the same direction as the backup family.
//
// Registered CONDITIONALLY by RegisterExport (V13: a nil seam leaves both
// ABSENT, not registered-and-refusing — the settings/seed/watchdog shape).
// run is RequiresInitialize (it reads the live schema and stamps the ledger's
// row when it reaches the engine); sets is OPEN — it answers what the binary
// can export from the schema it holds, and a not-yet-initialised daemon still
// knows that answer, so it cannot join the guard's `all` list the way the
// backup family does. Neither is Debug.
//
// run is the family's one WRITE: it stamps the actor from the CONNECTION
// (never a param, BK13) and rides the backup family's refusal voice —
// ErrBackupRefused, -31863, the code S62 T0 allocated to the block and T8
// records reusing in errors.go: one code for every refusal the export meets,
// the closed-set kind (dir-relative, dir-symlink, dir-nonempty,
// dir-unwritable, unknown-set, file-oversize, io, cancelled) in Data.Kind,
// the person's sentence in Data.What (DB19). A refusal that happens before
// the engine (the param shape) is a -32602 shape refusal, not a ledger
// refusal: no row is written for either, because the row is written only
// when the request reaches the engine.

// --- the seam -------------------------------------------------------------------

// ExportStore is the daemon's one export seam: two doors, one service. The
// wire views are this package's types; a *BackupRefusal is the engine's
// refusal rendered as data (DB19), never an error; a non-nil error is an
// infrastructure failure the door cannot name any better than the store's
// own sentence.
type ExportStore interface {
	// Run exports the requested sets (the default when Sets is empty: every
	// non-optional set, with memory joining only when IncludeMemory) into
	// Dir. It records the backup_run ledger row when — and only when — the
	// request reaches the engine: the row's outcome is ok, or failed with
	// the engine's error_kind mapped into the ledger's closed set.
	Run(ctx context.Context, actor Actor, params ExportRunParams) (ExportRunResult, *BackupRefusal, error)
	// Sets answers what the binary can export from this schema: the eight
	// declared sets, each with its state and — where the tables exist — no
	// row count (a count would be a read the listing door does not owe;
	// Rows is nil and the state says what the schema says).
	Sets(ctx context.Context) (ExportSetsResult, error)
}

// --- the wire twins ----------------------------------------------------------------

// ExportRunParams is the export.run request. Dir is the export dir's
// ABSOLUTE path — the engine refuses a relative one and a symlink, and the
// wire does not add a second shape rule the engine would re-judge. Sets is
// the closed set's names (the default when empty); IncludeMemory brings the
// opt-in memory set into an otherwise-default run, and is the ONLY door to
// it: an explicit "memory" without the flag is a refusal that names it.
type ExportRunParams struct {
	Sets          []string `json:"sets,omitempty"`
	Dir           string   `json:"dir"`
	IncludeMemory bool     `json:"includeMemory,omitempty"`
}

// ExportSetView is one declared set as the two doors show it: its name, the
// person's one-liner, and its state — available, available (opt-in) or
// unavailable (slice not built) — with Rows nil wherever the set does not
// carry a count (the sets door always, the run's manifest for an
// unavailable set: three-state honesty, P31).
type ExportSetView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional,omitempty"`
	State       string `json:"state"`
	Rows        *int   `json:"rows"`
}

// ExportRunResult is the export.run answer: the dir as the engine left it,
// the sets as the manifest says them, the file count, and whether the
// ledger's row was written (it is, whenever the request reached the engine).
type ExportRunResult struct {
	Dir      string          `json:"dir"`
	Sets     []ExportSetView `json:"sets"`
	Files    int             `json:"files"`
	Recorded bool            `json:"recorded"`
}

// ExportSetsResult is the export.sets answer: the closed set, in registry
// order, as this schema sees it.
type ExportSetsResult struct {
	Sets []ExportSetView `json:"sets"`
}

// --- registration -------------------------------------------------------------------

// RegisterExport registers the two export methods over a non-nil seam (V13).
// It returns whether the methods are in the table, so the daemon's wiring can
// be asserted rather than trusted — the settings/seed/watchdog shape.
func (t *Table) RegisterExport() bool {
	if t.Export == nil {
		return false
	}
	t.register(&Method{Name: MethodExportRun, RequiresInitialize: true, Handle: t.handleExportRun})
	t.register(&Method{Name: MethodExportSets, RequiresInitialize: false, Handle: t.handleExportSets})
	return true
}

// --- the actor and the refusals -------------------------------------------------------

// exportSeam is the one nil-guard shape the two handlers share: registration
// is conditional on the seam, so a nil here is a registration/seam
// disagreement the guard tests catch — spoken, not silent (the backup shape).
func (t *Table) exportSeam() error {
	if t.Export == nil {
		return ErrStoreUnavailable.WithWhat("export.* needs the export seam, and this build did not link it — the export surface is absent for the same reason (V13)")
	}
	return nil
}

// exportActor is the write door's audit identity for export.run: the person
// comes from the CONNECTION (the daemon's ActorFor seam, the kernel's peer
// credential), never from params. The honest absence (a daemon with no
// identity seam has no persons) refuses the write, and an empty id that is
// refused as well (the backupActor shape, the seedActor sentence).
func (t *Table) exportActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("export.run is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("export.run is a person's door and no identity is available on this connection — the daemon stamped none")}
	}
	return a, nil
}

// exportRefusedError renders the engine's refusal as the socket's (DB19, the
// backupRefusedError shape): one code (ErrBackupRefused, the code S62 T0
// allocated to the backup block and T8 records reusing — -31864 stays
// reserved), the closed-set kind in Data.Kind so a client can branch on the
// state found, the person's sentence in What.
func exportRefusedError(verb string, r *BackupRefusal) *Error {
	e := ErrBackupRefused.WithWhat("export %s refused: %s", verb, r.Detail)
	e.Data.Kind = r.Kind
	return e
}

// --- the handlers ----------------------------------------------------------------------

func (t *Table) handleExportRun(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := t.exportSeam(); err != nil {
		return nil, err
	}
	// The actor is stamped by the door that holds the connection; a param
	// that names it would let a caller forge the audit identity of a write
	// to their own data — the backup family's rule, reused (BK13).
	if err := rejectBackupIdentityKeys(string(MethodExportRun), params); err != nil {
		return nil, err
	}
	var p ExportRunParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// The dir is the engine's to judge (absolute, not a symlink, empty or
	// absent); the wire adds no second shape rule it would re-judge. What it
	// DOES refuse is a Sets entry outside the closed set: the engine's
	// refusal names the set too, but a param spelling a set that does not
	// exist is a shape error the door can say in its own words, before the
	// actor is even asked.
	for _, name := range p.Sets {
		if _, ok := exportSetNames[name]; !ok {
			return nil, ErrBadParams{Err: fmt.Errorf("sets names the declared sets — %q is not one of %v", name, exportSetNameList)}
		}
	}
	actor, err := t.exportActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	view, refusal, err := t.Export.Run(ctx, actor, p)
	if refusal != nil {
		return nil, exportRefusedError("run", refusal)
	}
	if err != nil {
		return nil, err
	}
	if view.Sets == nil {
		view.Sets = []ExportSetView{}
	}
	return view, nil
}

func (t *Table) handleExportSets(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if err := t.exportSeam(); err != nil {
		return nil, err
	}
	if err := rejectBackupIdentityKeys(string(MethodExportSets), params); err != nil {
		return nil, err
	}
	// No params: the set is a fact of the binary and the schema, not of the
	// caller (the backup.status shape).
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	view, err := t.Export.Sets(ctx)
	if err != nil {
		return nil, err
	}
	if view.Sets == nil {
		view.Sets = []ExportSetView{}
	}
	return view, nil
}

// --- the one mapping, shared by the daemon's door and the CLI's direct door ------
//
// The wire view is a fact the engine's Result plus the registry already
// settle; mapping it in two places (daemon service and CLI direct door)
// would be two renderers that can drift, so both call these (P5's parity
// is the point). Recorded is NOT set here: it is the CALLER's fact — the
// daemon's door wrote the ledger row (true), the direct door wrote none
// (false), and only each caller knows which.

// ExportRunResultFromEngine is the engine's Result as the wire view: the
// sets gain the registry's Description and Optional (the engine's
// SetResult carries the run's facts, the registry carries the set's
// identity).
func ExportRunResultFromEngine(res export.Result) ExportRunResult {
	sets := make([]ExportSetView, 0, len(res.Sets))
	for _, sr := range res.Sets {
		exp, _ := export.Get(sr.Name)
		sets = append(sets, ExportSetView{
			Name:        sr.Name,
			Description: exp.Description,
			Optional:    exp.Optional,
			State:       sr.State,
			Rows:        sr.Rows,
		})
	}
	return ExportRunResult{Dir: res.Dir, Sets: sets, Files: res.Files}
}

// ExportSetsFromProbe is the sets view over a schema that answers table
// existence: each set's state is the engine's own test, run without
// reading (every declared table exists or the set is unavailable), and
// Rows is nil on every set — the listing door probes and does not owe a
// count.
func ExportSetsFromProbe(exists func(string) bool) ExportSetsResult {
	out := make([]ExportSetView, 0, len(export.Names()))
	for _, name := range export.Names() {
		exp, _ := export.Get(name)
		built := true
		for _, t := range exp.Tables {
			if !exists(t) {
				built = false
				break
			}
		}
		state := export.StateAvailable
		switch {
		case !built:
			state = export.StateUnavailable
		case exp.Optional:
			state = export.StateAvailableOptIn
		}
		out = append(out, ExportSetView{
			Name:        name,
			Description: exp.Description,
			Optional:    exp.Optional,
			State:       state,
			Rows:        nil,
		})
	}
	return ExportSetsResult{Sets: out}
}
