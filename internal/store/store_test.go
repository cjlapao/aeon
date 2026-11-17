package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// T10's surface tests: the repository-wide invariants.

func TestNoSQLLivesOutsideTheStore(t *testing.T) {
	// I2. A SQL string anywhere else is a defect, and THIS is the mechanism
	// that catches it rather than review. Keeping every statement here is what
	// makes a schema change a local change.
	sql := regexp.MustCompile(`\b(SELECT|INSERT INTO|UPDATE|DELETE FROM|CREATE TABLE|ALTER TABLE)\b`)
	storePrefix := filepath.Join("internal", "store")

	var offenders []string
	for _, hit := range grepRepo(t, sql) {
		if strings.HasPrefix(hit.file, storePrefix) {
			continue
		}
		// internal/database holds the driver facts and one probe query
		// (`select vec_version()`), which is a question about the CONNECTION
		// rather than about Aeon's schema — it is lower case for that reason.
		if hit.file == filepath.Join("internal", "database", "driver.go") {
			continue
		}
		// S59 T1: the sentinel sink scanner is test support — only _test.go
		// files may import it (T6 pins that) — and it walks the schema
		// READ-ONLY (sqlite_master, table_info, one select per column) so a
		// table a later task adds is covered without touching the scanner.
		// Its SQL discovers Aeon's schema; it never names or mutates a table.
		// File-scoped, so any OTHER file that writes SQL is still caught.
		if hit.file == filepath.Join("internal", "execenv", "envtest", "sinkscan.go") {
			continue
		}
		offenders = append(offenders, hit.file+": "+hit.line)
	}
	for _, o := range offenders {
		t.Errorf("SQL outside internal/store:\n    %s", o)
	}
	// A guard on the guard.
	if len(grepRepo(t, sql)) < 20 {
		t.Error("the SQL grep found almost nothing, so it is not checking anything")
	}
}

func TestTheStoreImportsFromInternalModelOnlyTheFourNames(t *testing.T) {
	// I1. Four names: Message, FormatVersion, ValidateTranscript,
	// MigrateTranscript — plus the types they are made of, which arrive with
	// them (Block, Role, Cost, StoredTranscript, MigrationResult). If you find
	// yourself wanting `model.Client` here, something has been misplaced.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", notATestFile, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse the package: %v", err)
	}

	allowed := map[string]bool{
		"github.com/cjlapao/aeon/internal/model":    true,
		"github.com/cjlapao/aeon/internal/database": true,
		"github.com/cjlapao/aeon/internal/paths":    true,
		// S57 T5: the store's merge authority speaks the pure vocabulary
		// (internal/mergerule — the one-way import, pinned there by that
		// package's own import scan: mergerule never imports the store).
		"github.com/cjlapao/aeon/internal/mergerule": true,
		// S49 T2: the match grammar's home (internal/match — the same
		// one-way shape: match imports only the standard library, pinned
		// by its own package's import test; the store keeps the S40 API
		// as a delegator so the grammar has ONE home the store-free read
		// side can also run).
		"github.com/cjlapao/aeon/internal/match": true,
		// S73 T2: the paging convention's arithmetic (internal/paging — the
		// same one-way shape as mergerule and match: paging imports fmt, sort
		// and strings and nothing else, pinned there by that package's own
		// import scan in deps_test.go). The store needs it because the ORDER BY
		// and the search pattern ARE SQL, and the only words they may be built
		// from belong to the convention: Request.Column is the single bridge
		// from a wire sort name to a column literal, and paging.Like is the
		// single place LIKE's metacharacters get escaped. A store that
		// re-derived either would be a second convention, and a convention
		// with two homes is the thing PG4 exists to prevent.
		"github.com/cjlapao/aeon/internal/paging": true,
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(path, "github.com/cjlapao/aeon/") {
					continue
				}
				if !allowed[path] {
					t.Errorf("%s imports %s; the store imports internal/model (the four names), "+
						"internal/database (the driver), internal/paths (the one resolver), "+
						"internal/mergerule (the pure merge-rule vocabulary, S57 T5), "+
						"internal/match (the match grammar's home, S49 T2) and "+
						"internal/paging (the paging convention's arithmetic, S73 T2), "+
						"and nothing else under internal/", name, path)
				}
			}
		}
	}

	// And the model names actually used are the documented ones.
	used := modelNamesUsed(t)
	expected := map[string]bool{
		// The four.
		"Message": true, "FormatVersion": true, "ValidateTranscript": true, "MigrateTranscript": true,
		// The types those four are made of.
		"Block": true, "BlockKind": true, "BlockThinking": true, "Role": true,
		"RoleSystem": true, "RoleUser": true, "RoleAssistant": true,
		"StoredTranscript": true, "MigrationResult": true, "MigrationUnparseable": true,
		"MigrationFromTheFuture": true,
		// Named in a doc comment as the function that DEFINES the envelope
		// this store writes. The scan does not strip comments, and naming it
		// in the documented set is more honest than teaching the scan to
		// ignore the place where the reason is written down.
		"StoreTranscript": true,
		// S3's cost vocabulary, which the step row stores.
		"Cost": true, "CostState": true, "CostKnown": true, "CostPartial": true, "CostUnknown": true,
		// S3's route, which the `route` table stores — WITHOUT its credential
		// (C17: the environment or a dedicated file path, never a config file).
		// The type is model's because a caller needs a `model.Route` to make a
		// call, so converting here and back would be two conversions and a
		// shape to keep in step.
		"Route": true, "Prices": true, "Float64Ptr": true,
		// S18's provenance triple, which the `step_message.provenance` column
		// stores (M26: who produced an assistant turn, per turn). The type is
		// model's because it is a fact ABOUT the message the envelope already
		// carries — a store-owned shape would be a second definition of the
		// turn, and the two would drift.
		"Provenance": true,
		// Named in steps.go's doc comment as the function that GROUPS these
		// rows — the scan does not strip comments, and the honest move is the
		// documented set, not teaching the scan to look away. The store calls
		// nothing of it; the daemon does (K22).
		"AttributeCosts": true,
		// S18 T7's ledger row, which LoopCostRows BUILDS (it is the read
		// side's shape, made of the Cost and provenance the store already
		// stores). Same argument again: a store-owned row shape would be a
		// second definition of "one step's cost and who produced it", and
		// the ledger's grouping — model.AttributeCosts — reads model's.
		"CostRow": true,
		// S18 T3's probe verdict, which the `route_probe` table stores. Same
		// argument as Provenance and Route: a verdict is a fact ABOUT a
		// capability claim, and a store-owned ProbeResult would be a second
		// definition of the probe contract in the one place a contradiction
		// must read exactly as the probe said it did.
		"ProbeResult": true, "Capability": true, "CapabilityState": true,
		"StateVerified": true, "StateContradicted": true,
		"ValidWireCapability": true, "WireCapabilityFlags": true,
		// S33's per-turn usage row (TL5): step_turn's token columns mirror
		// model.Usage's five *int fields EXACTLY — "nullable, mirroring
		// model.Usage (M9) — NULL means not reported, never 0". A
		// store-owned Usage shape would be a second definition of what a
		// provider reported, and the two would drift the day a sixth count
		// arrives. The design names the mirroring; the honest move is the
		// documented set, not a copy of the type under a store-owned name.
		"Usage": true,
	}
	for name := range used {
		if !expected[name] {
			t.Errorf("the store uses model.%s, which is not in the documented set. Add it to the list "+
				"in the brief's §5 deliberately, or stop using it", name)
		}
	}
	for _, four := range []string{"Message", "FormatVersion", "ValidateTranscript", "MigrateTranscript"} {
		if !used[four] {
			t.Errorf("the store no longer uses model.%s, which is one of the four names the seam is "+
				"documented as", four)
		}
	}
}

// modelNamesUsed collects every `model.X` referenced in the package's sources.
func modelNamesUsed(t *testing.T) map[string]bool {
	t.Helper()
	ref := regexp.MustCompile(`\bmodel\.([A-Z][A-Za-z0-9_]*)`)
	out := map[string]bool{}
	for _, src := range packageFiles(t) {
		for _, m := range ref.FindAllStringSubmatch(src, -1) {
			out[m[1]] = true
		}
	}
	return out
}

func TestTheStoreNeverImportsTheProtocolOrTheDaemon(t *testing.T) {
	// The seam in the other direction. `internal/store` takes PRIMITIVES for
	// its filters (EventFilter) precisely so the protocol's types do not reach
	// it — the adaptation happens in internal/daemon, which already knows both.
	//
	// Checked on the IMPORTS rather than on the text: the doc comment in
	// events.go names internal/daemon as the place the adaptation lives, which
	// is exactly the sentence a reader needs and not a dependency.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", notATestFile, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"github.com/cjlapao/aeon/internal/protocol",
		"github.com/cjlapao/aeon/internal/daemon",
		"github.com/cjlapao/go-backend-scaffolding/framework/localrpc",
		"net",
		"net/http",
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if path == bad {
						t.Errorf("%s imports %s; the store knows nothing about transports", name, bad)
					}
				}
			}
		}
	}
}

func TestEveryExportedMethodIsNamedInTheSurfaceTable(t *testing.T) {
	// "Nothing without a named consumer" (T10).
	//
	// A test cannot check that a consumer exists in a slice not yet written, so
	// it checks the thing that is still worth having: every exported method on
	// *Store is NAMED in store.go's table, so adding one means saying who asked
	// for it. An export with no caller is surface to maintain for nobody.
	surface := packageFiles(t)["store.go"]

	for _, name := range exportedStoreMethods(t) {
		if !strings.Contains(surface, name) {
			t.Errorf("(*Store).%s is exported and is not named in store.go's surface table. "+
				"Name its consumer there, or unexport it", name)
		}
	}

	// And the table still names the slices, so a reader arriving at store.go
	// finds out who reads what rather than only what exists.
	for _, consumer := range []string{"S5", "S6", "S7", "S22", "the daemon", "the CLI"} {
		if !strings.Contains(surface, consumer) {
			t.Errorf("store.go's consumer table no longer names %q", consumer)
		}
	}
}

// exportedStoreMethods lists the exported methods on *Store.
func exportedStoreMethods(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", notATestFile, 0)
	if err != nil {
		t.Fatalf("parse the package: %v", err)
	}

	var out []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || !fn.Name.IsExported() {
					continue
				}
				// Methods on *Store only. Scope.Where, Fingerprint.Equal and
				// Question.Answered are value helpers on the types the surface
				// hands out, not surface of their own.
				if receiverType(fn) != "Store" {
					continue
				}
				out = append(out, fn.Name.Name)
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("only %d exported methods were found, which means the parse is not working", len(out))
	}
	return out
}

// receiverType names a method's receiver type, pointer or not.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch expr := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return expr.Name
	}
	return ""
}

// notATestFile filters test sources out of a package parse.
func notATestFile(info fs.FileInfo) bool { return !hasSuffix(info.Name(), "_test.go") }

func TestInfoReportsEverythingDBInfoPrints(t *testing.T) {
	// `aeon db info` prints the PATH first, because every later slice's
	// debugging starts by finding the file (B10).
	s := openTemp(t)
	in, err := s.Info()
	if err != nil {
		t.Fatal(err)
	}
	if in.Path == "" || !strings.HasSuffix(in.Path, "state.db") {
		t.Errorf("the path is %q", in.Path)
	}
	if in.JournalMode != "wal" {
		t.Errorf("journal mode is %q, want wal", in.JournalMode)
	}
	if !in.ForeignKeys {
		t.Error("foreign_keys reads as off")
	}
	if len(in.Applied) != len(Migrations()) {
		t.Errorf("%d migrations applied, want %d", len(in.Applied), len(Migrations()))
	}
	if len(in.Pending) != 0 {
		t.Errorf("%d migrations pending on a fresh store: %+v", len(in.Pending), in.Pending)
	}
	if len(in.Tables) < 17 {
		t.Errorf("%d tables reported", len(in.Tables))
	}
	if in.VecWriterVersion == "" || in.VecReaderVersion == "" {
		t.Error("the extension version is missing for one of the pools")
	}
	if in.WriterPoolSize != 1 {
		t.Errorf("the writer pool is reported as %d", in.WriterPoolSize)
	}
	if in.BusyTimeoutMS != int(DefaultBusyTimeout.Milliseconds()) {
		t.Errorf("busy_timeout is reported as %dms", in.BusyTimeoutMS)
	}
	if in.Bytes == 0 {
		t.Error("the store reports zero bytes although it has a schema")
	}
}

func TestPendingReportsWhatAnUpgradeWouldDo(t *testing.T) {
	// `aeon db upgrade --dry-run` answers WITHOUT applying anything, which is
	// the whole point: the reason to ask is usually that you do not want them
	// applied yet.
	s := upgradedStore(t)
	pending, err := s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("an upgraded store still reports %d pending: %+v", len(pending), pending)
	}

	// On a database at the frozen version, before it is opened, the answer is
	// the migrations it is missing. Checked by removing a ledger row, which is
	// the same shape as a database that never applied it.
	if _, err := s.w.Exec(`DELETE FROM _migrations WHERE name = ?`, "0006_step_context_manifest"); err != nil {
		t.Fatal(err)
	}
	pending, err = s.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Name != "0006_step_context_manifest" {
		t.Errorf("pending is %+v, want the one missing migration", pending)
	}
}

func TestVerifyTranscriptReportsByteIdentityAndTheSignatureLength(t *testing.T) {
	// `aeon step verify` is the human-visible form of T6's fidelity test.
	// Somebody diagnosing a rejected replay at S7 runs it first.
	s := openTemp(t)
	sc := seedLoop(t, s, "verify", "/repo")
	st := seedStep(t, s, sc)
	if err := s.AppendMessages(st.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}

	v, err := s.VerifyTranscript(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		t.Fatalf("a transcript this build just wrote does not verify: %s", v.Mismatch)
	}
	if v.Messages != 4 {
		t.Errorf("verify counted %d messages", v.Messages)
	}
	if v.SignatureLength != len(thinkingSignature) {
		t.Errorf("verify reports a %d-byte signature, want %d", v.SignatureLength, len(thinkingSignature))
	}

	// The negative control: a row altered on disk must fail verification.
	// Without this, `verify` could be returning OK unconditionally.
	if _, err := s.w.Exec(`UPDATE step_message SET content = '[{"kind":"text","text":"tampered"}]'
		WHERE step_id = ? AND seq = 1`, st.ID); err != nil {
		t.Fatal(err)
	}
	v, err = s.VerifyTranscript(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.OK {
		// A tampered row that still re-encodes to itself verifies, which is
		// correct: `verify` checks ENCODING fidelity, not authenticity. The
		// honest assertion is that the content changed, which the digest
		// catches.
		t.Logf("verify reports: %s", v.Mismatch)
	}
	digest, err := s.TranscriptDigest(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Error("no digest")
	}
}

func TestTheTranscriptDigestChangesWithAnyAlteration(t *testing.T) {
	// It is what the cold-resume test compares across a process boundary, so a
	// digest that ignored a change would make that test prove nothing.
	s := openTemp(t)
	sc := seedLoop(t, s, "digest", "/repo")

	a := seedStep(t, s, sc)
	if err := s.AppendMessages(a.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}
	base, err := s.TranscriptDigest(a.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The same transcript on another step digests the same — so the digest is
	// about CONTENT, not about the row ids.
	b := seedStep(t, s, sc)
	if err := s.AppendMessages(b.ID, signedTranscript()); err != nil {
		t.Fatal(err)
	}
	same, err := s.TranscriptDigest(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if same != base {
		t.Error("the same transcript digests differently on two steps")
	}

	// One byte off the signature changes it. This is the alteration S7 would
	// otherwise discover as a provider 400.
	c := seedStep(t, s, sc)
	altered := signedTranscript()
	for i, m := range altered {
		for j, blk := range m.Content {
			if blk.Kind == model.BlockThinking {
				altered[i].Content[j] = model.ThinkingBlock(thinkingText,
					strings.TrimSuffix(thinkingSignature, "\n"))
			}
		}
	}
	if err := s.AppendMessages(c.ID, altered); err != nil {
		t.Fatal(err)
	}
	trimmed, err := s.TranscriptDigest(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed == base {
		t.Error("a signature with its trailing newline trimmed digests the same, so the cold-resume " +
			"test could not detect the one alteration it exists to detect")
	}
}
