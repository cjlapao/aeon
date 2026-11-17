package main

// backup_guard_test.go — S62 T6: the sibling guards (T6.md §Guards). Never
// edits an existing guard body — this file is new, and the pre-existing guard
// family in policy_amend_test.go / surface tests stays byte-identical.
//
// The boundary in one sentence: the backup WRITES (run, verify, prune) are
// the person's door on the socket's human half; the backup READS (list,
// status) are plain reads; the read twin that an agent may reach is exactly
// one tool, backup_status; and the backup's RESTORE is never a remote door
// at all (BK9) — it is the CLI's direct door, the one documented exception,
// in a file this guard never scans (["cmd/aeon/backup_restore.go"]).
//
// Two scoping decisions, recorded here because the test enforces them:
//   - "named like restore" is scoped to the BACKUP's restore — a bare
//     "restore" name or a backup+restore compound. The other families'
//     restore doors (seed.restore, component.restore) are legitimate
//     pre-existing reads of their own families, and BK9 bans the backup's
//     restore gaining a remote door, not every occurrence of a word.
//   - the tool-name rule /restore|backup_(run|verify|prune)|export_run/ is
//     enforced the same way: a bare "restore" tool or a backup-scoped
//     compound, so the pre-existing component_restore tool of the component
//     family is not flagged.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// backupWriteMethods are the four human-door writes: the actor is stamped
// from the connection (BK13) and each moves something real. export.run
// joins at S62 T8 — the data leaves the machine, so it is the person's door
// exactly as the backup's three are.
var backupWriteMethods = []protocol.MethodName{
	protocol.MethodBackupRun,
	protocol.MethodBackupVerify,
	protocol.MethodBackupPrune,
	protocol.MethodExportRun, // S62 T8
}

// guardBackupService is a registration marker, the guardAuthoringService
// shape: enough of the seam to register, nothing to dispatch.
type guardBackupService struct{}

func (guardBackupService) Run(ctx context.Context, actor protocol.Actor, verify *bool) (protocol.BackupRunResult, *protocol.BackupRefusal, error) {
	return protocol.BackupRunResult{}, nil, nil
}

func (guardBackupService) List(ctx context.Context, limit int, includePreMigration bool) (protocol.BackupListResult, error) {
	return protocol.BackupListResult{}, nil
}

func (guardBackupService) Verify(ctx context.Context, actor protocol.Actor, file string, drill bool) (protocol.BackupVerifyResult, *protocol.BackupRefusal, error) {
	return protocol.BackupVerifyResult{}, nil, nil
}

func (guardBackupService) Status(ctx context.Context) (protocol.BackupStatusView, error) {
	return protocol.BackupStatusView{}, nil
}

func (guardBackupService) Prune(ctx context.Context, actor protocol.Actor, dryRun bool) (protocol.BackupPruneView, *protocol.BackupRefusal, error) {
	return protocol.BackupPruneView{}, nil, nil
}

// guardExportService is the export seam's registration marker (S62 T8), the
// guardBackupService shape: enough of the seam to register, nothing to
// dispatch.
type guardExportService struct{}

func (guardExportService) Run(ctx context.Context, actor protocol.Actor, params protocol.ExportRunParams) (protocol.ExportRunResult, *protocol.BackupRefusal, error) {
	return protocol.ExportRunResult{}, nil, nil
}

func (guardExportService) Sets(ctx context.Context) (protocol.ExportSetsResult, error) {
	return protocol.ExportSetsResult{}, nil
}

// guardBackupTable wires the backup AND export seams over a bare table. The
// export family is a sibling of the backup family (its ledger row is a
// backup_run of kind=export), so the guard's table carries both doors.
func guardBackupTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Backup = guardBackupService{}
	if !tbl.RegisterBackup() {
		t.Fatal("RegisterBackup refused a non-nil seam — the guard would test a table without its doors")
	}
	tbl.Export = guardExportService{}
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a non-nil seam — the guard would test a table without its doors")
	}
	return tbl
}

// ─── the predicates: the walks and the canaries run the same functions ──────

// narrowProjectingBackupWrite names the narrow actions that project a backup
// write method.
func narrowProjectingBackupWrite(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range backupWriteMethods {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

// agentServicesNamingBackupWrite names the agent-service methods that are a
// backup write.
func agentServicesNamingBackupWrite(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range ms {
		for _, w := range backupWriteMethods {
			if m.Name == w {
				bad = append(bad, string(m.Name))
			}
		}
	}
	return bad
}

// backupToolNameViolates is the tool-registry rule: a tool named for the
// backup's writes, for T8's would-be export write, or for the backup's
// restore. The component family's component_restore and the seed family's
// seed_restore are their own families' reads and do not match (the
// scoping decision in this file's header).
var backupToolWriteRE = regexp.MustCompile(`^backup_(run|verify|prune)$|^export_run$`)

func backupToolNameViolates(name string) bool {
	return backupToolWriteRE.MatchString(name) || isBackupRestoreName(name)
}

// catalogSpecsNamingBackupWrite names the backup-family specs that are not
// the sanctioned read twins: a spec that mutates, or a spec twinned to a
// method that is not a backup read.
func catalogSpecsNamingBackupWrite(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		if sp.Family != "backup" {
			continue
		}
		if sp.Mutates {
			bad = append(bad, sp.Name+" (mutates)")
			continue
		}
		switch sp.Method {
		case "backup.status", "export.sets": // the read twins: T6, and T8's
		default:
			bad = append(bad, sp.Name+" → "+sp.Method)
		}
	}
	return bad
}

// boundaryStatementNamingBackupWrite reports which of the four write names
// the rendered boundary statement lists.
func boundaryStatementNamingBackupWrite(stmt string) []string {
	var bad []string
	for _, m := range []string{"backup.run", "backup.verify", "backup.prune", "export.run"} {
		if strings.Contains(stmt, m) {
			bad = append(bad, m)
		}
	}
	return bad
}

// backupWriteLiteralInFile reports which of the guarded literals a source
// file names. The literals are the wire names (quoted, as they appear on the
// wire and in the table) and the Go constants that name them — a projection
// or registry that NAMES a write door, even in a comment, is the projection
// the rule forbids (the S45 TextGrep idiom). MethodExportRun is T8's
// would-be constant, held here now, so the export write's door is guarded in
// the same pass.
var backupWriteLiteralPatterns = []string{
	`"backup.run"`, `"backup.verify"`, `"backup.prune"`, `"export.run"`,
	"MethodBackupRun", "MethodBackupVerify", "MethodBackupPrune", "MethodExportRun",
}

func backupWriteLiteralInFile(src string) []string {
	var bad []string
	for _, p := range backupWriteLiteralPatterns {
		if strings.Contains(src, p) {
			bad = append(bad, p)
		}
	}
	return bad
}

// isBackupRestoreName is the BK9 name rule: a name that IS the backup's
// restore — a bare "restore", or a backup+restore compound in any of the
// name styles (dot, underscore, hyphen, camel). See the header.
var backupRestoreNameRE = regexp.MustCompile(`(?i)backup[_.-]?restore|restore[_.-]?backup`)

func isBackupRestoreName(name string) bool {
	return name == "restore" || backupRestoreNameRE.MatchString(name)
}

// backupRestoreLiteralInFile is the same rule over source text: a constant
// or string literal that names the backup's restore.
var backupRestoreLiteralRE = regexp.MustCompile(`(?i)backup[_.-]?restore|restore[_.-]?backup|\bmethodrestore\b`)

func backupRestoreLiteralInFile(src string) []string {
	if !backupRestoreLiteralRE.MatchString(src) {
		return nil
	}
	m := backupRestoreLiteralRE.FindString(src)
	return []string{m}
}

// ─── the positive half ──────────────────────────────────────────────────────

// TestBackupMethodsAreAHumanDoor states the rule positively: the five backup
// methods and export.run are on the socket, gated, product surface — and only
// when their service is wired (V13: absent with a nil seam, present never
// without it). export.sets is asserted separately below: it is OPEN
// (RequiresInitialize false — the one read that says nothing an
// uninitialised daemon could not already say), which the gated list does not
// admit (S62 T8).
func TestBackupMethodsAreAHumanDoor(t *testing.T) {
	tbl := guardBackupTable(t)
	all := []protocol.MethodName{
		protocol.MethodBackupRun, protocol.MethodBackupList, protocol.MethodBackupVerify,
		protocol.MethodBackupStatus, protocol.MethodBackupPrune,
		protocol.MethodExportRun, // S62 T8: the export write joins the gated list
	}

	// (1) the exact set is present.
	registered := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	for _, m := range all {
		if !registered[m] {
			t.Fatalf("%s is not registered with its service wired — the app has its door (P34), and the guard's table says it does not", m)
		}
	}

	// (2) each is gated and non-debug: a write before the handshake is
	// unthinkable, and Debug is never what keeps a method off the agent
	// surfaces (that absence is this file's negative half, owned there).
	for _, m := range all {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s vanished between Methods() and Lookup()", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a backup write on an unnegotiated connection is unthinkable", m)
		}
		if got.Debug {
			t.Errorf("%s is Debug — these are product methods; the narrow exclusion must never rest on a Debug flag", m)
		}
	}

	// (3) and the writes are the human door in the surface table, the reads
	// are not: the classification is asserted per method in the protocol
	// package (TestBackupSurfaceClassification, S47's ask); here the
	// app-level fact — a write that is not human-door-only is a write with
	// no person. The list now carries export.run (S62 T8), the family's one
	// write.
	for _, m := range backupWriteMethods {
		if s, ok := protocol.SurfaceClassified(m); !ok || !s.HumanDoorOnly {
			t.Errorf("%s is not classified HumanDoorOnly in the surface table — the actor is stamped from the connection (BK13) and a write without the person's door is a write without the person (BK12)", m)
		}
	}
	// The export read is the opposite fact, asserted here so a table edit
	// that flips either way is caught: export.sets is a plain read, never
	// human-door-only.
	if s, ok := protocol.SurfaceClassified(protocol.MethodExportSets); !ok || s.HumanDoorOnly {
		t.Errorf("export.sets is not a plain, non-HumanDoorOnly read in the surface table — it is the family's read twin and says nothing a person alone may pass (BK14)")
	}

	// (3b) export.sets, asserted separately because it is OPEN: registered
	// and non-debug, but NOT RequiresInitialize — the one read that says
	// nothing an uninitialised daemon could not already say (S62 T8).
	{
		got, ok := tbl.Lookup(protocol.MethodExportSets)
		if !ok {
			t.Fatalf("export.sets is not registered with its service wired — the app has its read (P34), and the guard's table says it does not")
		}
		if got.RequiresInitialize {
			t.Errorf("export.sets requires Initialize — it is the open read, and the gate must stay off it")
		}
		if got.Debug {
			t.Errorf("export.sets is Debug — it is a product read; the narrow exclusion must never rest on a Debug flag")
		}
	}

	// (4) V13 at the app level: with a nil seam the names are not in the
	// table at all — the door is a fact of the build, and a table without
	// the service answers JSON-RPC's own -32601, never a refusal.
	bare := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if bare.RegisterBackup() {
		t.Fatal("RegisterBackup registered over a NIL seam — presence without its service is the disagreement V13 exists to name")
	}
	if bare.RegisterExport() {
		t.Fatal("RegisterExport registered over a NIL seam — presence without its service is the disagreement V13 exists to name")
	}
	for _, m := range append(all, protocol.MethodExportSets) {
		if _, ok := bare.Lookup(m); ok {
			t.Errorf("%s is present on a table whose seam is nil — a method without its service (V13)", m)
		}
	}
}

// ─── the negative half ──────────────────────────────────────────────────────

// TestBackupNeverReachesTheNarrowOrAgentSurfaces walks the four caller
// classes that are not the person — the narrow HTTP/MCP-stdio projection
// (and its rendered boundary statement), the agent-services enumeration, the
// tool registry, the records catalog — plus a literal grep of their sources:
// none of them may name a backup write, and the catalog holds only the read
// twin.
func TestBackupNeverReachesTheNarrowOrAgentSurfaces(t *testing.T) {
	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		if bad := narrowProjectingBackupWrite(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects a backup write through %s — a key-holder's projection must not hand out the person's door (BK12)", strings.Join(bad, ", "))
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		if bad := boundaryStatementNamingBackupWrite(stmt); len(bad) != 0 {
			t.Errorf("the projection's boundary statement lists %s — a surface that DISPLAYS a door its table must not hold is a second truth", strings.Join(bad, ", "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		if bad := agentServicesNamingBackupWrite(ms); len(bad) != 0 {
			t.Errorf("the agent-services enumeration names %s — an agent surface carrying a backup write is the refusal made mechanical (BK12)", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — the registry guard would pass on nothing")
		}
		var bad []string
		for name := range reg {
			// The only backup_ name an agent may see is the read twin.
			if strings.HasPrefix(name, "backup_") && name != "backup_status" {
				bad = append(bad, name+" (a backup_ tool other than backup_status)")
				continue
			}
			if backupToolNameViolates(name) {
				bad = append(bad, name)
			}
		}
		if len(bad) != 0 {
			t.Errorf("the tool registry carries %s — a tool that runs, verifies, prunes or restores a backup is a second door onto what the socket's human half alone carries (BK12, BK9)", strings.Join(bad, ", "))
		}
		if _, has := reg["backup_status"]; !has {
			t.Error("the tool registry has no backup_status — the read twin is the one sanctioned agent reach, and it is absent (P34: the app has its read)")
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — the catalog guard would pass on nothing")
		}
		if bad := catalogSpecsNamingBackupWrite(specs); len(bad) != 0 {
			t.Errorf("the records catalog carries %s — the family's entries are READS twinned to registered read methods, nothing else (BK12)", strings.Join(bad, ", "))
		}
		// The exact set, at T8: the two read twins — T6's backup_status and
		// T8's export_sets (safe, Mutates:false, twinned to export.sets).
		// export.run is a human door and never a catalog entry (BK14).
		var family []string
		for _, sp := range specs {
			if sp.Family == "backup" {
				family = append(family, sp.Name)
			}
		}
		want := []string{"backup_status", "export_sets"}
		if len(family) != len(want) || family[0] != want[0] || family[1] != want[1] {
			t.Errorf("the backup family holds %v, want exactly %v", family, want)
		}
	})

	t.Run("TextGrep", func(t *testing.T) {
		for _, dir := range forbiddenAgentSurfaces {
			root := filepath.Join("..", "..", dir)
			var walked, nonTest int
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") {
					return nil
				}
				walked++
				if strings.HasSuffix(path, "_test.go") {
					return nil // the guards' own text is not the guarded text
				}
				nonTest++
				src, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				if bad := backupWriteLiteralInFile(string(src)); len(bad) != 0 {
					t.Errorf("%s names %s — a projection or registry that NAMES a backup write door, even in a comment, is the projection the rule forbids", filepath.ToSlash(path), strings.Join(bad, ", "))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if nonTest == 0 {
				t.Fatalf("%s: the walk saw no non-test files — the grep checked nothing", dir)
			}
			t.Logf("%s: %d files seen (%d non-test)", dir, walked, nonTest)
		}
	})
}

// TestNoRestoreMethodOrToolExists is BK9's mechanical half: the backup's
// restore gains no remote door — no registered method, tool, catalog spec,
// or constant or literal under the six scanned directories. The documented
// exception is the CLI's direct door in cmd/aeon/backup_restore.go (T7), a
// path outside every directory this test scans; the exception list is
// recorded, not a loophole — a restore door inside these directories is a
// failure whatever file it is in.
func TestNoRestoreMethodOrToolExists(t *testing.T) {
	// exception list (T7's direct door; never scanned by this test):
	exceptionFiles := []string{"cmd/aeon/backup_restore.go"}
	_ = exceptionFiles

	// (1) registered methods: none is the backup's restore.
	tbl := guardBackupTable(t)
	var bad []string
	for _, m := range tbl.Methods() {
		if isBackupRestoreName(string(m)) {
			bad = append(bad, string(m))
		}
	}
	if len(bad) != 0 {
		t.Errorf("the table registers %s — the backup's restore is the CLI's direct door only; a socket method for it is the remote door BK9 refuses (seed.restore and component.restore are their own families' reads and do not match)", strings.Join(bad, ", "))
	}

	// (2) the tool registry.
	bad = nil
	for name := range tools.Registry() {
		if isBackupRestoreName(name) {
			bad = append(bad, name)
		}
	}
	if len(bad) != 0 {
		t.Errorf("the tool registry carries %s — an agent tool that restores a backup is a remote door onto the direct door", strings.Join(bad, ", "))
	}

	// (3) the records catalog: no spec named for the restore.
	bad = nil
	for _, sp := range records.Catalog() {
		if isBackupRestoreName(sp.Name) || isBackupRestoreName(sp.Method) {
			bad = append(bad, sp.Name)
		}
	}
	if len(bad) != 0 {
		t.Errorf("the records catalog carries a restore spec %s — restore is never a tool", strings.Join(bad, ", "))
	}

	// (4) constants and literals: no non-test source under the six
	// directories names the backup's restore.
	for _, dir := range []string{
		"internal/protocol", "internal/records", "internal/tools",
		"internal/narrow", "internal/agentsvc", "internal/daemon",
	} {
		root := filepath.Join("..", "..", dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if hit := backupRestoreLiteralInFile(string(src)); len(hit) != 0 {
				t.Errorf("%s names %s — a constant or literal for the backup's restore inside a remote-door package is the door itself, wearing a different name", filepath.ToSlash(path), hit[0])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// ─── the canaries: a guard that cannot fail is not a guard ──────────────────

// TestBackupGuardsHaveTeeth feeds every predicate a synthetic violation and a
// clean input, as the walks use them — the same functions, never copies
// (S45's canary idiom).
func TestBackupGuardsHaveTeeth(t *testing.T) {
	// 1. The narrow projection: an action projecting a write is flagged; a
	// real read is not. export.run is S62 T8's would-be projection, canaried
	// in the same pass as the backup's three.
	for _, write := range []struct {
		name string
		m    protocol.MethodName
	}{
		{"aeon_backup_run", protocol.MethodBackupRun},
		{"aeon_export_run", protocol.MethodExportRun},
	} {
		if got := narrowProjectingBackupWrite([]narrow.Action{
			{Name: write.name, SocketMethod: write.m, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
		}); len(got) == 0 {
			t.Fatalf("an action projecting %s was not flagged — the narrow predicate cannot fail, so it is not a guard", write.m)
		}
	}
	if got := narrowProjectingBackupWrite([]narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}); len(got) != 0 {
		t.Fatalf("a clean read action was flagged as a backup write (%v) — the predicate fires on nothing", got)
	}

	// 2. The agent-services enumeration.
	for _, write := range []protocol.MethodName{protocol.MethodBackupPrune, protocol.MethodExportRun} {
		if got := agentServicesNamingBackupWrite([]protocol.AgentServiceMethod{
			{Name: write, Cannot: "must not exist"},
		}); len(got) == 0 {
			t.Fatalf("an agent-service method named %s was not flagged — the enumeration predicate cannot fail", write)
		}
	}
	if got := agentServicesNamingBackupWrite([]protocol.AgentServiceMethod{
		{Name: protocol.MethodAgentPlugins, ReadOnly: true, Cannot: "must not exist"},
	}); len(got) != 0 {
		t.Fatalf("a clean agent-service read was flagged (%v) — the predicate fires on the reads", got)
	}

	// 3. The tool names: the violating set, and the names that must NOT be
	// flagged (backup_status is the sanctioned read twin; component_restore
	// is the component family's own read — a predicate that flags it would
	// delete a live tool).
	for _, bad := range []string{"backup_run", "backup_verify", "backup_prune", "export_run", "restore", "backup_restore"} {
		if !backupToolNameViolates(bad) {
			t.Fatalf("tool name %q violates the registry rule and was not flagged — the predicate cannot fail", bad)
		}
	}
	for _, ok := range []string{"backup_status", "backup_list", "backup_show", "component_restore", "seed_restore"} {
		if backupToolNameViolates(ok) {
			t.Fatalf("tool name %q is sanctioned or another family's read and was flagged — the predicate fires on the reads it must allow", ok)
		}
	}

	// 4. The catalog: a spec twinned to a write is flagged (backup.run and
	// S62 T8's export.run); the real read twins are not.
	for _, sp := range []records.Spec{
		{Name: "backup_run", Method: "backup.run", Family: "backup", Class: "safe"},
		{Name: "export_run", Method: "export.run", Family: "backup", Class: "safe"},
	} {
		if got := catalogSpecsNamingBackupWrite([]records.Spec{sp}); len(got) == 0 {
			t.Fatalf("a catalog spec twinned to %s was not flagged — the catalog predicate cannot fail", sp.Method)
		}
	}
	clean := records.Catalog()
	if got := catalogSpecsNamingBackupWrite(clean); len(got) != 0 {
		t.Fatalf("the real catalog was flagged (%v) — the predicate fires on the sanctioned read twins", got)
	}

	// 5. The file scan, through a real temp file under a fake tools
	// directory: the violating source is flagged on read-back, the clean
	// source is not.
	dir := t.TempDir()
	for name, item := range map[string]struct {
		src  string
		want bool
	}{
		"poisoned.go": {`package tools
var m = "backup.prune"`, true},
		"poisoned_export.go": {`package tools
var m = "export.run"`, true}, // S62 T8
		"clean.go": {`package tools
var m = "backup.status"`, false},
	} {
		p := filepath.Join(dir, "internal", "tools", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(item.src), 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		got := backupWriteLiteralInFile(string(data))
		if item.want && len(got) == 0 {
			t.Fatalf("%s names a backup write and was not flagged — the file predicate cannot fail", name)
		}
		if !item.want && len(got) != 0 {
			t.Fatalf("%s is clean and was flagged (%v) — the file predicate fires on the reads", name, got)
		}
	}

	// 6. The restore name rule: the backup's restore is flagged in every
	// style; the other families' restore reads are not.
	for _, bad := range []string{"restore", "backup.restore", "backup_restore", "backup-restore", "BackupRestore", "restore_backup"} {
		if !isBackupRestoreName(bad) {
			t.Fatalf("name %q is the backup's restore and was not flagged — the BK9 name predicate cannot fail", bad)
		}
	}
	for _, ok := range []string{"seed.restore", "component.restore", "seed_restore", "component_restore", "CanRestoreWithThisBinary", "post-restore-check"} {
		if isBackupRestoreName(ok) {
			t.Fatalf("name %q is another family's read (or a field, or a reason) and was flagged — the predicate fires on the names it must allow", ok)
		}
	}

	// 7. The boundary statement: a statement listing a write is flagged
	// (backup.run and S62 T8's export.run); the real one is not.
	for _, stmt := range []string{
		"this surface offers backup.run and little else",
		"this surface offers export.run and little else",
	} {
		if got := boundaryStatementNamingBackupWrite(stmt); len(got) == 0 {
			t.Fatalf("a boundary statement listing a write (%s) was not flagged — the statement predicate cannot fail", stmt)
		}
	}
	if got := boundaryStatementNamingBackupWrite(narrow.BoundaryStatement()); len(got) != 0 {
		t.Fatalf("the real boundary statement was flagged (%v) — the predicate fires on the truth", got)
	}
}

// ─── the ticker is a maintenance task, not a trigger ────────────────────────

// tickerTableCounts is the row count of each of the six automation/loop
// tables the ticker must never touch (BK7/AU1: the schedule is a maintenance
// task — a trigger or a loop would give it a row, and a row is an
// automation's existence).
func tickerTableCounts(t *testing.T, st *store.Store) map[string]int {
	t.Helper()
	out := map[string]int{}
	count := func(name string, n int, err error) {
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		out[name] = n
	}
	trs, err := st.ListEventTriggers(store.EventTriggerFilter{})
	count("event_trigger", len(trs), err)
	scheds, err := st.TriggerSchedules(context.Background())
	count("trigger_schedule", len(scheds), err)
	fires, err := st.ListEventFires(store.EventFireFilter{Limit: 500})
	count("trigger_fire", len(fires), err)
	evts, err := st.ReadAutomationEvents(0, 1000)
	count("automation_event", len(evts), err)
	loops, err := st.ListLoops(store.ComponentScope{})
	count("loop", len(loops), err)
	steps, err := st.ListSteps(store.Scope{})
	count("step", len(steps), err)
	return out
}

// TestTickerIsNotATrigger is BK7's mechanical half. Static: the ticker's
// file imports no trigger writer and calls no loop starter. Dynamic: a FULL
// scheduled run — the real thing, on a throwaway store — leaves the six
// automation/loop tables exactly as it found them.
func TestTickerIsNotATrigger(t *testing.T) {
	// Static: the ticker's file.
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "daemon", "backup_service.go"))
	if err != nil {
		t.Fatalf("read the ticker's file: %v", err)
	}
	for _, bad := range []string{
		`"github.com/cjlapao/aeon/internal/trigger"`,
		"loop.Start",
		"StartRun",
	} {
		if strings.Contains(string(src), bad) {
			t.Errorf("the ticker's file contains %q — the schedule is a maintenance task, and a trigger import or loop starter turns it into an automation (BK7/AU1)", bad)
		}
	}

	// Dynamic: a full scheduled run, counted against the six tables.
	// The service's own clock (the due check) is the real one — deps is
	// unexported, and the due arithmetic itself is the daemon package's
	// tests' business, with an injected clock. So the store stamps its
	// rows far in the past: the last success is then due against ANY real
	// clock, and the guard's question stays what it is — which tables a
	// full run touches.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	st, err := store.Open(store.Options{
		Dir:   t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: func() time.Time { return past },
	})
	if err != nil {
		t.Fatalf("open the throwaway store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed the last success a day before the fake stamp: far enough back
	// that the start wake is due on any clock.
	if _, err := st.RecordBackupRun(store.NewBackupRun{
		Kind:           "backup",
		Reason:         "scheduled",
		ActorKind:      "daemon",
		StartedAt:      past.Add(-24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z"),
		Outcome:        "ok",
		Detail:         "the online snapshot succeeded",
		FileName:       backupGuardSeedFile,
		FreeSpaceState: "unknown",
	}); err != nil {
		t.Fatalf("seed the last success: %v", err)
	}

	before := tickerTableCounts(t, st)

	// nil settings store: this guard drives the ticker with its own
	// environment settings (T10's signature only adds the database layer
	// for mounts that have a settings store).
	svc := daemon.NewBackupService(st, nil, "9.9.9-test", func(string, any) {}, guardDiscardLogger{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = svc.TickLoop(ctx) }() // wakes at start; the run is due

	// Wait for the ledger's scheduled row (the run is small and local).
	deadline := time.Now().Add(30 * time.Second)
	var rows []store.BackupRun
	for time.Now().Before(deadline) {
		var err error
		rows, err = st.ListBackupRuns(store.BackupRunFilter{Kind: "backup", Limit: 100})
		if err != nil {
			t.Fatalf("read the ledger: %v", err)
		}
		if len(rows) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(rows) < 2 {
		t.Fatalf("the scheduled run never landed on the ledger (rows: %d) — the dynamic half tests a run that did not happen", len(rows))
	}
	// The new row must be a run that happened and succeeded: a scheduled
	// ok row naming a file the seed did not name.
	happened := false
	for _, r := range rows {
		if r.Outcome == "ok" && r.FileName != nil && *r.FileName != backupGuardSeedFile {
			happened = true
		}
	}
	if !happened {
		t.Fatalf("no scheduled ok run with a fresh file landed on the ledger (%v) — the tables would be untouched for the wrong reason", rows)
	}
	cancel()

	after := tickerTableCounts(t, st)
	for table, beforeN := range before {
		if after[table] != beforeN {
			t.Errorf("a full scheduled run left %s at %d rows, was %d — the schedule wrote to the automation's tables, and a row there is an automation's existence (BK7/AU1)", table, after[table], beforeN)
		}
		if after[table] != 0 {
			t.Errorf("a full scheduled run left %d rows in %s on a fresh store — the schedule touches that table", after[table], table)
		}
	}
}

// guardDiscardLogger is the Logger the service takes; the guard does not
// read the log.
type guardDiscardLogger struct{}

func (guardDiscardLogger) Infof(format string, args ...any) {}

var backupGuardSeedFile = "aeon-state-20261101T110000Z-v76.aeonbak"
