package blueprintseed_test

// blueprintseed_test — S48 T5 (BP5/BP6): the blueprints.standard seed set.
// The scary tests are the ones that protect an operator's edit: a diverged
// shipped blueprint must survive a seed run untouched and only
// seed.restore replaces it; every hash the retired pristineUpgrades table
// recognised must still classify pristine and be updated IN PLACE.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/blueprintseed"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

func newSeedStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedEngine(t *testing.T, s *store.Store) *seed.Engine {
	t.Helper()
	e, err := daemon.NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("seed engine: %v", err)
	}
	return e
}

func applySet(t *testing.T, e *seed.Engine) seed.ApplyResult {
	t.Helper()
	res, err := e.Apply(context.Background(), blueprintseed.SetID, nil, seed.Actor{Kind: "system", ID: "test"}, seed.PathExplicit)
	if err != nil {
		t.Fatalf("apply %s: %v", blueprintseed.SetID, err)
	}
	return res
}

func putRow(t *testing.T, s *store.Store, row store.BlueprintRow, rev store.RevisionRow) {
	t.Helper()
	err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSourceHasOneRecordPerEmbeddedStandard(t *testing.T) {
	recs := blueprintseed.Source()
	names := blueprint.StandardNames()
	if len(recs) != len(names) {
		t.Fatalf("one record per shipped blueprint: %d records for %d names", len(recs), len(names))
	}
	for i, r := range recs {
		if r.Key != names[i] {
			t.Fatalf("record %d is %q, want %q — order and membership are the shipped list", i, r.Key, names[i])
		}
		c, err := blueprintseed.DecodeRecord(r)
		if err != nil {
			t.Fatal(err)
		}
		raw, ok, err := blueprint.EmbeddedSource().Get(r.Key)
		if err != nil || !ok {
			t.Fatalf("embedded %q: %v", r.Key, err)
		}
		if c.Content != string(raw) {
			t.Fatalf("record %q does not carry the embedded bytes verbatim", r.Key)
		}
		if r.Version < 1 || c.Version != r.Version {
			t.Fatalf("record %q version %d vs inner %d", r.Key, r.Version, c.Version)
		}
	}
	if blueprintseed.SetVersion() < 1 {
		t.Fatal("set version below the record floor")
	}
	for _, r := range recs {
		if r.Version > blueprintseed.SetVersion() {
			t.Fatalf("rule 3: record %q v%d exceeds the set v%d", r.Key, r.Version, blueprintseed.SetVersion())
		}
	}
}

func TestBlueprintVersionsBumpWithContent(t *testing.T) {
	var want []string
	if b, err := os.ReadFile("testdata/blueprints.versions.golden"); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" {
				want = append(want, l)
			}
		}
	}
	recs := blueprintseed.Source()
	var got []string
	for _, r := range recs {
		sum := sha256.Sum256(r.Content)
		got = append(got, r.Key+"\t"+itoa(r.Version)+"\t"+hex.EncodeToString(sum[:]))
	}
	if strings.Join(want, "\n") == strings.Join(got, "\n") {
		return
	}
	if os.Getenv("BPSEED_GOLDEN_UPDATE") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/blueprints.versions.golden", []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden rewritten — commit it and say so in the record")
		return
	}
	t.Fatalf("content changed but version did not — bump it, or the golden is stale:\nwant:\n%s\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestBlueprintsStandardIsInertAndNotGuardAffecting(t *testing.T) {
	set, ok := seed.DefaultRegistry().Lookup(blueprintseed.SetID)
	if !ok {
		t.Fatal(blueprintseed.SetID + " is not declared")
	}
	if set.Class != seed.ClassInert {
		t.Fatalf("class %q — the shipped blueprints apply unasked at start; that is only legal because they widen nothing", set.Class)
	}
	if set.Records == nil || len(set.Records()) == 0 {
		t.Fatal("rule 3: Records is never nil and complete")
	}
	// The registry's rule-6 stance: the set stands inert ONLY while its
	// applier reports GuardAffecting false.
	if blueprintseed.NewApplier(nil).GuardAffecting() {
		t.Fatal("GuardAffecting true on an inert set — the daemon would apply a permission change unasked")
	}
	if op := blueprintseed.NewApplier(nil).Operator(); !op.Edit || op.Remove {
		t.Fatalf("operator rights %+v — a person may edit a shipped blueprint (it diverges); they may not remove it", op)
	}
}

func TestFreshStoreSeedsEverythingWithSystemOrigin(t *testing.T) {
	s := newSeedStore(t)
	applySet(t, seedEngine(t, s))
	ctx := context.Background()
	rows, err := s.BlueprintList(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(blueprint.StandardNames()) {
		t.Fatalf("fresh store seeded %d rows, want %d", len(rows), len(blueprint.StandardNames()))
	}
	for _, r := range rows {
		if r.Origin != "system" || r.UpdatedBy != "system:seed" {
			t.Fatalf("%s: origin %q updated_by %q — provenance wrong", r.Name, r.Origin, r.UpdatedBy)
		}
		revs, err := s.RevisionList(ctx, r.Name, 5)
		if err != nil || len(revs) != 1 || revs[0].Op != "seed" || revs[0].ActorKind != "system" || revs[0].ActorID != "seed" {
			t.Fatalf("%s: first revision %+v %v — want op seed actor system:seed", r.Name, revs, err)
		}
	}
	// Refs are derived at the seed write: two-step names an agent, so its
	// row must have derived refs.
	refs, err := s.BlueprintOwnRefs(ctx, "two-step")
	if err != nil || len(refs) == 0 {
		t.Fatalf("two-step derived refs empty: %v", err)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	ctx := context.Background()
	total := 0
	for _, n := range blueprint.StandardNames() {
		revs, err := s.RevisionList(ctx, n, 100)
		if err != nil {
			t.Fatal(err)
		}
		total += len(revs)
	}
	applySet(t, e) // second start
	again := 0
	for _, n := range blueprint.StandardNames() {
		revs, err := s.RevisionList(ctx, n, 100)
		if err != nil {
			t.Fatal(err)
		}
		again += len(revs)
	}
	if again != total {
		t.Fatalf("second apply wrote %d more revisions — churn (first run %d, now %d)", again-total, total, again)
	}
}

func TestEveryPreviouslyRecognisedPristineHashIsStillPristine(t *testing.T) {
	// (a) The move lost nothing: blueprintseed's list equals the legacy
	// resolver's table, per name, both directions.
	for _, name := range []string{"two-step", "dev-loop"} {
		mine := blueprintseed.PristineHashes(name)
		theirs := blueprint.PristineHashes(name)
		if strings.Join(mine, ",") != strings.Join(theirs, ",") || len(mine) == 0 {
			t.Fatalf("%s: moved list %v != legacy table %v — acceptance 8 lost a hash", name, mine, theirs)
		}
	}
	// (b) A live system row whose fingerprint is a recognised pristine hash
	// is adopted and UPDATED IN PLACE to current — the seed engine doing
	// what upgradePristineStandard did, for every shipped blueprint
	// (behaviour 2). The historical bytes themselves are not reproducible,
	// so the fingerprint column is planted with the historical hash, which
	// is exactly what the engine reads.
	for _, name := range []string{"two-step", "dev-loop"} {
		for _, h := range blueprintseed.PristineHashes(name) {
			s := newSeedStore(t)
			putRow(t, s,
				store.BlueprintRow{Name: name, Version: 1, Content: "# an old shipped copy\n", ContentHash: h, Origin: "system", UpdatedBy: "system:adoption"},
				store.RevisionRow{Version: 1, Content: "# an old shipped copy\n", ContentHash: h, Op: "adopt", ActorKind: "system", ActorID: "startup"})
			applySet(t, seedEngine(t, s))
			ctx := context.Background()
			row, ok, err := s.BlueprintGet(ctx, name)
			if err != nil || !ok {
				t.Fatalf("%s: row vanished: %v", name, err)
			}
			raw, _, _ := blueprint.EmbeddedSource().Get(name)
			if row.Content != string(raw) {
				t.Fatalf("%s @ %s: old pristine NOT updated in place — it stayed the old copy", name, h[:12])
			}
			revs, _ := s.RevisionList(ctx, name, 10)
			if len(revs) != 2 || revs[0].Op != "update" || revs[1].Op != "adopt" {
				t.Fatalf("%s: revisions (rev,op) %v — want adopt@1 then update@2", name, revOps(revs))
			}
			if !blueprintseed.KnownPristineHash(h) {
				t.Fatalf("%s %s: in the table but not KnownPristineHash", name, h)
			}
		}
	}
}

func TestEditedShippedBlueprintIsNeverOverwritten(t *testing.T) {
	s := newSeedStore(t)
	ctx := context.Background()
	applySet(t, seedEngine(t, s))
	// The operator edits dev-loop (the save door's shape: same name, their
	// bytes, op save, a person's actor).
	edited := "# the operator's dev-loop\n"
	sum := sha256.Sum256([]byte(edited))
	row, ok, _ := s.BlueprintGet(ctx, "dev-loop")
	if !ok {
		t.Fatal("dev-loop was not seeded")
	}
	row.Content = edited
	row.ContentHash = hex.EncodeToString(sum[:])
	putRow(t, s, row, store.RevisionRow{Version: row.Version, Content: edited, ContentHash: row.ContentHash, Op: "save", ActorKind: "user", ActorID: "operator"})
	// The next start must skip it, report it, and NEVER write their bytes
	// away.
	res := applySet(t, seedEngine(t, s))
	var skipped bool
	for _, r := range res.Records {
		if r.Key == "dev-loop" && r.Op == seed.OpSkip && strings.Contains(r.Reason, "diverged") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("the edited shipped copy was not reported as a diverged skip: %+v", res.Records)
	}
	got, _, _ := s.BlueprintGet(ctx, "dev-loop")
	if got.Content != edited {
		t.Fatal("OPERATOR'S EDIT CLOBBERED by the seed set")
	}
	// The explicit way back: seed.restore re-applies shipped content as a
	// NEW revision (op restore) — the history keeps their edit.
	if _, err := seedEngine(t, s).Restore(ctx, blueprintseed.SetID, "dev-loop", seed.Actor{Kind: "user", ID: "operator"}); err != nil {
		t.Fatal(err)
	}
	restored, _, _ := s.BlueprintGet(ctx, "dev-loop")
	raw, _, _ := blueprint.EmbeddedSource().Get("dev-loop")
	if restored.Content != string(raw) {
		t.Fatal("restore did not re-apply the shipped content")
	}
	revs, _ := s.RevisionList(ctx, "dev-loop", 10) // rev DESC
	if len(revs) != 3 || revs[0].Op != "restore" || revs[1].Op != "save" || revs[2].Op != "seed" {
		t.Fatalf("revisions (rev,op) %v — want seed@1, save@2 (theirs, kept), restore@3", revOps(revs))
	}
	if revs[1].Content != edited {
		t.Fatal("the restore rewrote their save revision — history is append-only")
	}
}

func TestRetiredBlueprintRowKept(t *testing.T) {
	s := newSeedStore(t)
	ctx := context.Background()
	applySet(t, seedEngine(t, s))
	before, _, err := s.BlueprintGet(ctx, "dev-loop")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the release dropping dev-loop: a registry whose set carries
	// every record EXCEPT dev-loop. The engine's plan sees the ledger entry
	// without a record ⇒ OpRetire: flag only, the row is kept (SD6).
	var recs []seed.Record
	for _, r := range blueprintseed.Source() {
		if r.Key != "dev-loop" {
			recs = append(recs, r)
		}
	}
	reg, err := seed.NewRegistry([]seed.Set{{ID: blueprintseed.SetID, Class: seed.ClassInert, Version: blueprintseed.SetVersion(), Records: func() []seed.Record { return recs }}}, []seed.Applier{blueprintseed.Adapter{}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := daemon.NewSeedEngine(s, seed.Options{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(ctx, blueprintseed.SetID, nil, seed.Actor{Kind: "system", ID: "test"}, seed.PathExplicit); err != nil {
		t.Fatal(err)
	}
	after, ok, err := s.BlueprintGet(ctx, "dev-loop")
	if err != nil || !ok {
		t.Fatal("a retired record's row was deleted — SD6 forbids it")
	}
	if after.Content != before.Content || after.ContentHash != before.ContentHash {
		t.Fatal("retire touched the row — it flags only")
	}
	// The ledger's own state is the truth (an ALREADY-retired record is a
	// no-decision no-op in status — engine plan line 86 — so read the
	// ledger, the append-only memory, directly).
	ledger, err := s.SeedLedgerList(ctx, blueprintseed.SetID)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, l := range ledger {
		if l.RecordKey == "dev-loop" {
			seen = true
			if l.State != "retired" {
				t.Fatalf("ledger says %q, want retired", l.State)
			}
		}
	}
	if !seen {
		t.Fatal("dev-loop has no ledger row — the retire never reached the seed ledger")
	}
}

func TestSystemBlueprintIsEditableDuplicableNotDeletable(t *testing.T) {
	t.Skip("the edit/duplicate/delete DOORS are S48 T6 (the save rewrite); the store-level divergence half is covered by TestEditedShippedBlueprintIsNeverOverwritten")
}

func TestNoProductionReaderOfPristineUpgrades(t *testing.T) {
	// The old mechanism must be gone from every production path. The two
	// named files are the allowed holders: blueprintseed (the canonical
	// production list) and standard.go's LEGACY file resolver — the `path`
	// verb, retired at T11, whose test (standard_upgrade_test.go) and
	// materialise path still carry their own copy. Any other reader is a
	// second upgrade mechanism alive.
	allowed := map[string]bool{
		filepath.Join("internal", "blueprint", "standard.go"):     true, // legacy resolver until T11 (named in the record)
		filepath.Join("internal", "blueprintseed", "pristine.go"): true, // the canonical list
	}
	root := repoRoot(t)
	var hits []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // a walk error on one entry never stops the audit
		}
		rel, _ := filepath.Rel(root, p)
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(rel, string(filepath.Separator)+".tmp"+string(filepath.Separator)) {
			return nil
		}
		if allowed[rel] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil //nolint:nilerr // unreadable files are not readers of anything
		}
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, "pristineUpgrades") || strings.Contains(line, "upgradePristineStandard") {
				hits = append(hits, rel+":"+itoa(i+1)+": "+trimmed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("the retired pristine table has a production reader again (two upgrade mechanisms is two truths):\n%s", strings.Join(hits, "\n"))
	}
}

func TestBlueprintseedImportsNothingForbidden(t *testing.T) {
	// Layering: the seed domain reads the embedded bytes and the engine
	// contract — never the outward plane (mcp), never policy, never files.
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"internal/mcp", "internal/policy", "internal/handler", "\"os\""}
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, fb := range forbidden {
				if strings.Contains(path, fb) {
					t.Errorf("%s imports %q — forbidden for blueprintseed", f, path)
				}
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// this package sits at <aeon>/internal/blueprintseed — audit the aeon tree
	return filepath.Dir(filepath.Dir(wd))
}

func revOps(revs []store.RevisionRow) []string {
	out := make([]string, 0, len(revs))
	for _, r := range revs {
		out = append(out, fmt.Sprintf("%d:%s", r.Rev, r.Op))
	}
	return out
}

var _ = ast.IsExported
