package daemon

// export_service_test.go — S62 T8 (BK14): the daemon half's behaviour. The
// store is real (a throwaway temp dir — tests never point at the real store)
// and the clock is the harness's fixed instant, so the ledger's timestamps
// are assertable. The one-row rule is the fact under test: the request
// reaches the engine, so the row is owed — ok or failed-with-mapped-kind —
// and a row the ledger takes is exactly one.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/export"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// exportTestNow is the calendar instant the harness shares: a Monday noon in
// UTC (the T6 test's point, carried).
var exportTestNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

type exportHarness struct {
	st  *store.Store
	svc *ExportService
}

// newExportHarness opens a throwaway store and builds the service over it,
// with the fixed clock on both the store's I9 seam and the service.
func newExportHarness(t *testing.T) *exportHarness {
	t.Helper()
	st, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: func() time.Time { return exportTestNow },
	})
	if err != nil {
		t.Fatalf("open the throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := NewExportService(st, "0.0.0-test", discardLogger{})
	if svc == nil {
		t.Fatalf("NewExportService over a live store answered nil — nil is the storeless daemon's answer (V13)")
	}
	svc.clock = func() time.Time { return exportTestNow }
	return &exportHarness{st: st, svc: svc}
}

func exportLedgerRows(t *testing.T, st *store.Store) []store.BackupRun {
	t.Helper()
	runs, err := st.ListBackupRuns(store.BackupRunFilter{Kind: "export"})
	if err != nil {
		t.Fatalf("list the export ledger rows: %v", err)
	}
	return runs
}

// TestExportServiceRecordsOneLedgerRow: the one backup_run row (kind export,
// reason manual) is written when the request reaches the engine — which from
// the service's seat is every request — with the outcome and the mapped
// error_kind, the actor stamped, the fixed detail, no file name.
func TestExportServiceRecordsOneLedgerRow(t *testing.T) {
	t.Parallel()
	h := newExportHarness(t)
	ctx := context.Background()
	actor := protocol.Actor{Kind: "user", ID: "1000"}

	// ok: a fresh dir, the default run on a fresh store — every set's tables
	// exist, so every non-optional set answers available (with zero files,
	// the store holds no rows), and the row is the success.
	dir := filepath.Join(t.TempDir(), "out")
	res, refusal, err := h.svc.Run(ctx, actor, protocol.ExportRunParams{Dir: dir})
	if err != nil {
		t.Fatalf("export.run: %v", err)
	}
	if refusal != nil {
		t.Fatalf("refusal = %+v, want a success on a fresh store", refusal)
	}
	if !res.Recorded {
		t.Error("recorded = false, want true — the request reached the engine, so the row was written")
	}
	if res.Dir != dir {
		t.Errorf("dir = %q, want %q", res.Dir, dir)
	}
	if got := len(res.Sets); got != len(export.Names())-1 {
		t.Errorf("the default run's view carries %d sets, want every non-optional one (%d)", got, len(export.Names())-1)
	}
	runs := exportLedgerRows(t, h.st)
	if len(runs) != 1 {
		t.Fatalf("the ledger holds %d export rows after a success, want exactly one", len(runs))
	}
	row := runs[0]
	if row.Kind != "export" || row.Reason != "manual" || row.ActorKind != "user" {
		t.Errorf("row = kind %q reason %q actor %q, want export manual user", row.Kind, row.Reason, row.ActorKind)
	}
	if row.Outcome != "ok" || row.ErrorKind != nil || row.Detail != "export: ok" {
		t.Errorf("row = outcome %q kind %v detail %q, want ok, no kind, the fixed detail", row.Outcome, row.ErrorKind, row.Detail)
	}
	if row.StartedAt != "2026-11-02T12:00:00.000Z" {
		t.Errorf("started_at = %q, want the harness instant in the ledger's voice", row.StartedAt)
	}
	if row.FileName != nil {
		t.Errorf("file_name = %v, want NULL — an export publishes no .aeonbak", *row.FileName)
	}

	// failed: a symlinked dir — the gate refuses dir-symlink, the engine's
	// sentence comes back as the refusal, and the row still stands: failed,
	// the kind mapped (a dir refusal is a shape fault the ledger does not
	// classify, so unknown), the fixed detail, the same started instant.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "nowhere"), link); err != nil {
		t.Fatalf("plant the symlink: %v", err)
	}
	res2, refusal2, err := h.svc.Run(ctx, actor, protocol.ExportRunParams{Dir: link})
	if err != nil {
		t.Fatalf("export.run on a symlink: %v — want the family's refusal result, not an error", err)
	}
	if res2.Recorded {
		t.Error("a refused run answered recorded = true — the files went nowhere")
	}
	if refusal2 == nil || refusal2.Kind != export.ErrKindDirSymlink {
		t.Fatalf("refusal = %+v, want kind dir-symlink", refusal2)
	}
	if refusal2.Detail == "" {
		t.Error("the refusal carries no sentence — the person's words are the refusal")
	}
	runs2 := exportLedgerRows(t, h.st)
	if len(runs2) != 2 {
		t.Fatalf("the ledger holds %d export rows after the refused run, want two (the row is owed when the request reaches the engine)", len(runs2))
	}
	var failed store.BackupRun
	for _, r := range runs2 {
		if r.Outcome == "failed" {
			failed = r
		}
	}
	if failed.ID == 0 {
		t.Fatal("no failed row in the ledger — the refused run's row is owed too")
	}
	if failed.ErrorKind == nil || *failed.ErrorKind != "unknown" {
		t.Errorf("failed row's error_kind = %v, want unknown (the mapping for a dir refusal)", failed.ErrorKind)
	}
	if failed.Detail != "export: failed" {
		t.Errorf("failed row's detail = %q, want the fixed template (the engine's sentence carries a path and never lands in a detail)", failed.Detail)
	}
}

// TestExportServiceAbsentWithoutStore is the V13 side: a storeless daemon
// gets nil from the constructor, the wiring keeps table.Export nil, and
// RegisterExport leaves both doors ABSENT.
func TestExportServiceAbsentWithoutStore(t *testing.T) {
	t.Parallel()
	if svc := NewExportService(nil, "0.0.0-test", nil); svc != nil {
		t.Fatalf("NewExportService(nil) = non-nil, want nil (V13: no store, no service)")
	}
}

// TestExportSetsService: the sets door lists the closed set in registry
// order, each with the registry's own words, the state from this schema
// (a fresh store has every table, so the seven defaults answer available and
// the opt-in memory set answers available (opt-in) — the flag named), and
// rows null on every set: the listing door probes existence and does not owe
// a count.
func TestExportSetsService(t *testing.T) {
	t.Parallel()
	h := newExportHarness(t)
	view, err := h.svc.Sets(context.Background())
	if err != nil {
		t.Fatalf("export.sets: %v", err)
	}
	if len(view.Sets) != len(export.Names()) {
		t.Fatalf("the view carries %d sets, want the closed set's %d", len(view.Sets), len(export.Names()))
	}
	for i, v := range view.Sets {
		exp, ok := export.Get(v.Name)
		if !ok {
			t.Errorf("set %d = %q, not a declared set", i, v.Name)
			continue
		}
		if export.Names()[i] != v.Name {
			t.Errorf("set %d = %q, want the registry order (%q)", i, v.Name, export.Names()[i])
		}
		if v.Description != exp.Description || v.Optional != exp.Optional {
			t.Errorf("set %q carries %q/%v, want the registry's %q/%v", v.Name, v.Description, v.Optional, exp.Description, exp.Optional)
		}
		if v.Rows != nil {
			t.Errorf("set %q carries rows %v, want null — the listing door does not owe a count", v.Name, *v.Rows)
		}
		wantState := export.StateAvailable
		if exp.Optional {
			wantState = export.StateAvailableOptIn
		}
		if v.State != wantState {
			t.Errorf("set %q state = %q, want %q on a fresh store", v.Name, v.State, wantState)
		}
	}
}
