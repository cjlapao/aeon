//go:build !noretrieval

package retrieval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// index_test.go — T1's gates. The deletability claim (D1) and the four
// reasons (D3) are the two that matter; everything else here keeps the index
// honest while the corpora arrive in T2–T5.

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open the test store (a failure naming fts5 means the build lacks the sqlite_fts5 tag — build through make, B6): %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCorpus(s *store.Store, root, name string) error {
	_, err := s.EnsureRetrievalCorpus(root, name)
	return err
}

func must(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// --- D1, the claim: the suite passes without the package. ---------------

// TestSuitePassesWithoutThePackage is D1's Go-side half: it BUILDS the whole
// product at `-tags noretrieval` — every package, not a subset — which is
// the compile-level proof that nothing outside this package depends on the
// real implementation (I2). The full suite at that tag is `make
// test-noretrieval`, and CI runs it (O1): a compile passes on a stale cache
// more easily than a test suite does, which is exactly why both doors exist
// and only the CI one is a property.
//
// SCOPE, recorded at T3 because a rehearsal must not read as a regression:
// this gate is a COMPILE check, and it fails on any mid-flight tree whose
// foreign work-in-progress does not compile at any tag — S41's notify WIP
// (`notifyHintFor` living in the uncommitted `notify_test.go`) broke it at
// the rehearsal tree while the main tree compiled and the full suite passed
// it. The authoritative compile proof stays `make test-noretrieval`/CI
// (the paragraph above); what this gate catches in-place is the local
// mistake — a file that forgot its build tag.
func TestSuitePassesWithoutThePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("nested whole-product build; run make test-go for the full suite")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this file to find the module root")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	// -buildvcs=false when the harness asks: the gate exec's its OWN build,
	// and a detached `git worktree` rehearsing a commit makes VCS stamping
	// exit 128 — the same env discipline as AEON_GO_TAGS (S22 T1) and the
	// mcp fakeserver (S22 T3); a build-contract gate must not die on where
	// the tree happens to live.
	args := []string{"build", "-tags", "sqlite_fts5 noretrieval", "./..."}
	if os.Getenv("AEON_NO_BUILDVCS") == "1" {
		args = []string{"build", "-buildvcs=false", "-tags", "sqlite_fts5 noretrieval", "./..."}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GOPRIVATE=github.com/cjlapao/*", "CGO_ENABLED=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the no-retrieval build failed — something outside internal/retrieval came to depend on it (I2), or a tag-untagged file crept in:\n%s", out)
	}
}

// TestSearchAbsentWithoutThePackage is D1's other half, seen from where a
// model looks: the registry. T6 has now landed, and it kept the promise in
// BOTH directions: the tool is registered only while the resolver seam is
// wired (the sole wiring caller is the entry point's !noretrieval link),
// and this test binary never wires it — so even WITH the package compiled
// in, an unwired registry lacks search (a wrongly-eager unconditional
// registration fails here), and the tagged subprocess says the same from
// the build where the tool cannot exist at all: its
// TestSearchAbsentInNoRetrievalBuild must pass there, and the committed
// class fixture must stay consistent in both builds (V13, Q6).
func TestSearchAbsentWithoutThePackage(t *testing.T) {
	if testing.Short() {
		t.Skip("nested alternate-tag test; run make test-go for the full suite")
	}
	reg := tools.Registry()
	if _, ok := reg["search"]; ok {
		t.Fatal("the registry holds a tool named search — V13: absent means it does not exist, not that it returns nothing")
	}
	// The floor is the guard on the guard: an empty registry also lacks
	// search, and a test that passes on nothing teaches nothing.
	if len(reg) < 9 {
		t.Fatalf("the registry holds %d tools, which is too few to be the real one", len(reg))
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this file to find the module root")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	args := []string{"test", "-tags", "sqlite_fts5 noretrieval", "-count=1",
		"-run", "TestSearchAbsentInNoRetrievalBuild|TestRerunClassesMatchCommittedList|TestFixtureCoversAllCurrentAndPendingTools|TestEveryToolDeclaresARerunClass|TestBashIsTheOnlyUnsafeTool",
		"./internal/tools/"}
	if os.Getenv("AEON_NO_BUILDVCS") == "1" {
		args = []string{"test", "-buildvcs=false", "-tags", "sqlite_fts5 noretrieval", "-count=1",
			"-run", "TestSearchAbsentInNoRetrievalBuild|TestRerunClassesMatchCommittedList|TestFixtureCoversAllCurrentAndPendingTools|TestEveryToolDeclaresARerunClass|TestBashIsTheOnlyUnsafeTool",
			"./internal/tools/"}
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GOPRIVATE=github.com/cjlapao/*", "CGO_ENABLED=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the no-retrieval tools package failed (absent-tool or class-fixture inconsistency, V13/Q6):\n%s", out)
	}
}

// --- D3: the four reasons, and no fifth. ---------------------------------

func TestFourAvailabilityReasons(t *testing.T) {
	built := store.RetrievalCorpus{Kind: "retrieved", EmbedModel: "text-embedding-3-small", EmbedDim: 4}
	naked := store.RetrievalCorpus{Kind: "retrieved"}
	route := &Route{Model: "text-embedding-3-small", Dim: 4}

	cases := []struct {
		name        string
		exists      bool
		chunks      int
		row         store.RetrievalCorpus
		route       *Route
		wantAvail   bool
		wantKeyword bool
		wantReasons []Reason
	}{
		{"index-empty: corpus absent", false, 0, naked, route, false, false, []Reason{ReasonIndexEmpty}},
		{"index-empty: corpus exists, nothing indexed", true, 0, naked, route, false, false, []Reason{ReasonIndexEmpty}},
		{"no-provider: keyword-only says so (V9, D4)", true, 7, built, nil, true, true, []Reason{ReasonNoProvider}},
		{"model-mismatch refuses (V6, V17)", true, 7, built, &Route{Model: "other-model", Dim: 4}, false, false, []Reason{ReasonModelMismatch}},
		{"dimension-mismatch refuses (V6)", true, 7, built, &Route{Model: "text-embedding-3-small", Dim: 3072}, false, false, []Reason{ReasonDimensionMismatch}},
		{"both mismatch: both named, neither hidden", true, 7, built, &Route{Model: "other", Dim: 3072}, false, false, []Reason{ReasonModelMismatch, ReasonDimensionMismatch}},
		{"route exists, nothing embedded yet: keyword-only, no blame to assign", true, 7, naked, route, true, true, nil},
		{"the answerable state: ok, full", true, 7, built, route, true, false, nil},
	}

	seen := map[Reason]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := availabilityFor(tc.exists, tc.chunks, tc.row, tc.route)
			if got.Available != tc.wantAvail || got.KeywordOnly != tc.wantKeyword {
				t.Fatalf("state wrong: %+v", got)
			}
			if fmt.Sprint(got.Reasons) != fmt.Sprint(tc.wantReasons) && !(len(got.Reasons) == 0 && len(tc.wantReasons) == 0) {
				t.Fatalf("reasons: got %v want %v", got.Reasons, tc.wantReasons)
			}
			if got.Available && len(got.Reasons) > 0 && !got.KeywordOnly {
				t.Fatalf("an available answer carries reasons without being keyword-only — reasons answer WHY NOT (interface.go)")
			}
			seen[ReasonIndexEmpty] = seen[ReasonIndexEmpty] || contains(got.Reasons, ReasonIndexEmpty)
			seen[ReasonNoProvider] = seen[ReasonNoProvider] || contains(got.Reasons, ReasonNoProvider)
			seen[ReasonModelMismatch] = seen[ReasonModelMismatch] || contains(got.Reasons, ReasonModelMismatch)
			seen[ReasonDimensionMismatch] = seen[ReasonDimensionMismatch] || contains(got.Reasons, ReasonDimensionMismatch)
		})
	}
	for _, r := range []Reason{ReasonIndexEmpty, ReasonNoProvider, ReasonModelMismatch, ReasonDimensionMismatch} {
		if !seen[r] {
			t.Errorf("reason %q never came out of the table — four reasons means four REACHABLE ones (D3)", r)
		}
	}
}

func contains(rs []Reason, want Reason) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

// TestNoExtensionMissingState is D3's grep half (I6, E5): the FIFTH reason —
// the extension being absent — died with S4's DB18, and a string for it
// would be a state someone writes a handler for. Scanned over the package's
// own bytes INCLUDING this file and comments (a banned token counts in
// comments — S21's rule), with a floor so an empty scan cannot pass by
// scanning nothing.
func TestNoExtensionMissingState(t *testing.T) {
	entries, err := os.ReadDir(".")
	must(t, err, "read the package directory")
	scanned := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		raw, err := os.ReadFile(e.Name())
		must(t, err, "read "+e.Name())
		scanned++
		// The ONE permitted occurrence is this gate's own name, stripped
		// here (assembled so stripping it does not plant it): a state
		// constant or handler string named for the impossible reason is
		// what the ban hunts, not the test that hunts it.
		raw = []byte(strings.ReplaceAll(string(raw), "TestNo"+"Extension"+"Missing"+"State", ""))
		// The tokens are ASSEMBLED so the hunting file cannot contain what
		// it hunts (S21 T2's rule, applied to itself): a literal here would
		// make this test the thing it scans for.
		for _, banned := range []string{
			"extension" + "-" + "missing",
			"extension" + "_" + "missing",
			"Extension" + "Missing",
		} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s names %q — there are FOUR availability reasons (V19); the extension is compiled in (DB18) and cannot be missing", e.Name(), banned)
			}
		}
	}
	if scanned < 5 {
		t.Fatalf("scanned only %d .go files, which is too few to be this package", scanned)
	}
}

// --- The corpus kind is data (Stage 5's row-not-migration decision). -----

func TestCorpusKindIsAColumn(t *testing.T) {
	s := openStore(t)
	db := s.GormDB()

	// The negative shape: a table per corpus would look like
	// retrieval_code_chunk / retrieval_history_chunk. It fails this.
	var n int
	must(t, db.Raw(`SELECT count(*) FROM sqlite_master WHERE type='table'
		AND (name LIKE 'retrieval_code%' OR name LIKE 'retrieval_history%')`).Scan(&n).Error, "scan")
	if n != 0 {
		t.Fatalf("%d corpus-named tables exist — the kind is a COLUMN (T1's decision), two hardcoded corpora would make Stage 5 a migration", n)
	}

	// The positive shape: adding a third kind is a ROW, executable here.
	must(t, mustCorpus(s, "/root/a", "code"), "ensure corpus")
	_, ok, err := s.RetrievalCorpusRow("/root/a", "code")
	if err != nil || !ok {
		t.Fatalf("the corpus row did not come back: ok=%v err=%v", ok, err)
	}
	must(t, db.Exec(`INSERT INTO retrieval_corpus
		(component_root, name, kind, provenance, observation_count)
		VALUES (?, ?, 'concluded', 'observed', 3)`, "/root/b", "person-memory").Error, "concluded row as a row")
	row, ok, err := s.RetrievalCorpusRow("/root/b", "person-memory")
	must(t, err, "read the concluded row")
	if !ok || row.Kind != "concluded" {
		t.Fatalf("a concluded corpus is a row, not a migration — got ok=%v kind=%q", ok, row.Kind)
	}
}

func TestConcludedColumnsAreReservedAndChecked(t *testing.T) {
	s := openStore(t)
	db := s.GormDB()

	// A belief without provenance is unwritable — asserted by ATTEMPTING it
	// (the task's form), both directions of the CHECK.
	err := db.Exec(`INSERT INTO retrieval_corpus
		(component_root, name, kind, provenance, observation_count)
		VALUES ('/r', 'project-memory', 'concluded', NULL, NULL)`).Error
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a concluded row without the reserved columns was accepted (%v) — the guard is decorative", err)
	}
	err = db.Exec(`INSERT INTO retrieval_corpus
		(component_root, name, kind, provenance, observation_count)
		VALUES ('/r', 'x', 'concluded', 'guessed', 1)`).Error
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a concluded row named a provenance outside told/observed/assumed and was accepted (%v)", err)
	}
	err = db.Exec(`INSERT INTO retrieval_corpus
		(component_root, name, kind, provenance)
		VALUES ('/r', 'y', 'retrieved', 'told')`).Error
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a retrieved row carried a provenance and was accepted (%v) — retrieved results cannot be beliefs", err)
	}
	// And the reserved columns are NULL for every retrieved row (the row
	// this slice actually writes).
	c, err := s.EnsureRetrievalCorpus("/r", "code")
	must(t, err, "ensure")
	var reserved struct {
		Provenance       *string
		ObservationCount *int64
	}
	must(t, db.Raw(`SELECT provenance, observation_count FROM retrieval_corpus WHERE id = ?`, c.ID).
		Scan(&reserved).Error, "read reserved columns")
	prov, obs := reserved.Provenance, reserved.ObservationCount
	if prov != nil || obs != nil {
		t.Fatalf("the retrieved row carries %v/%v — reserved columns are NULL until Stage 5", prov, obs)
	}
}

func TestConcludedKindIsRejectedForNow(t *testing.T) {
	x := New(openStore(t))
	err := x.CreateCorpus(context.Background(), "/root", "code", "concluded")
	if err == nil {
		t.Fatal("the concluded kind was silently accepted — it must be rejected naming Stage 5 (the S11 six-tools shape)")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Stage 5") || !strings.Contains(msg, "not-yet-implemented") {
		t.Fatalf("the refusal must name Stage 5 and say not-yet-implemented, got: %v", err)
	}
	// The ordinary door still works, and an invented kind is refused by name.
	must(t, x.CreateCorpus(context.Background(), "/root", "history", ""), "ordinary create")
	err = x.CreateCorpus(context.Background(), "/root", "vibes", "")
	if err == nil || !strings.Contains(err.Error(), `"vibes"`) {
		t.Fatalf("an invented corpus must be refused naming what was typed: %v", err)
	}
}

// --- V12: two roots have two indexes. -------------------------------------

func TestIndexIsPerProjectRoot(t *testing.T) {
	s := openStore(t)
	x := New(s)
	ctx := context.Background()

	// Same corpus name, same words, two roots — and a worktree-shaped near
	// miss (a prefix of the other path), because "distinct root" that only
	// separates /a from /b separates nothing real.
	for _, root := range []string{"/repo/alpha", "/repo/alpha-extra"} {
		must(t, x.EnsureCorpus(ctx, root, CorpusCode), "ensure")
		must(t, x.InsertChunk(root, CorpusCode, "retry.go", 10, 20, "h1",
			"the retry policy bounds every attempt"), "chunk")
	}
	must(t, x.EnsureCorpus(ctx, "/repo/beta", CorpusCode), "ensure-beta")
	must(t, x.InsertChunk("/repo/beta", CorpusCode, "other.go", 1, 2, "h2",
		"the retry policy lives here"), "chunk-beta")

	for _, probe := range []string{"/repo/alpha", "/repo/alpha-extra"} {
		hits, _, err := x.KeywordSearch(ctx, probe, CorpusCode, "retry", nil, 10)
		must(t, err, "search "+probe)
		if len(hits) != 1 || hits[0].Source != "retry.go" {
			t.Fatalf("root %s saw %v — the index is per project root (V12) and every query filters", probe, hits)
		}
	}
	hits, _, err := x.KeywordSearch(ctx, "/repo/beta", CorpusCode, "retry", nil, 10)
	must(t, err, "search beta")
	if len(hits) != 1 || hits[0].Source != "other.go" {
		t.Fatalf("root /repo/beta saw %v — the roots leaked into each other", hits)
	}
}

// --- Q1: the migration ledger accepts the virtual table. ------------------

func TestMigrationLedgerAccepts(t *testing.T) {
	s := openStore(t)
	db := s.GormDB()

	// The ledger row exists beside 0039's.
	var name string
	must(t, db.Raw(`SELECT name FROM _migrations WHERE name = '0040_retrieval_index'`).Scan(&name).Error, "ledger row")
	if name != "0040_retrieval_index" {
		t.Fatalf("no ledger row for 0040 (got %q) — the schema is written in one place and this migration is not it", name)
	}

	// Every piece is present after a plain open (fresh), including FTS5's
	// shadow tables — which the equivalence test (D4) compares fresh against
	// upgraded like everything else. Q1's answer, measured: virtual tables DO
	// compare equal, because a single DDL source replays identically into
	// both paths; no exception was needed, so none was stated, and the
	// fingerprint floor test in schema_test keeps the comparison honest.
	for _, table := range []string{"retrieval_corpus", "retrieval_chunk", "retrieval_chunk_fts", "retrieval_chunk_fts_data"} {
		var n int
		must(t, db.Raw(`SELECT count(*) FROM sqlite_master WHERE name = ?`, table).Scan(&n).Error, "sqlite_master "+table)
		if n != 1 {
			t.Fatalf("table %s present %d times — 0040 did not land its pieces", table, n)
		}
	}

	// V19's basis, measured rather than cited: the extension answers HERE,
	// in the store's own connections — present on every connection (DB18),
	// which is WHY there is no fifth-reason state (D3).
	var v string
	if err := db.Raw(`SELECT vec_version()`).Scan(&v).Error; err != nil || v == "" {
		t.Fatalf("vec_version() did not answer (%v, %q) — sqlite-vec is supposed to be compiled in (DB18)", err, v)
	}

	// The vec0 table itself: created on demand at a dimension the migration
	// could not know (the Q1 answer's second half — T1 proves the mechanism
	// exists, T3 gives it a route to come from).
	table, err := New(s).EnsureVecTable("/repo/alpha", CorpusCode, 4)
	must(t, err, "ensure vec table")
	if !strings.HasPrefix(table, "retrieval_vec_code_d4_") {
		t.Fatalf("vec table named %q — it carries corpus, dimension and root-hash so two roots and two dimensions never share one table", table)
	}
	var cols int
	must(t, db.Raw(`SELECT count(*) FROM pragma_table_info(?)`, table).Scan(&cols).Error, "vec cols")
	if cols < 2 {
		t.Fatalf("vec table %s holds %d columns, want embedding + chunk_id", table, cols)
	}
}

// --- D4: keyword-only, said out loud, on the real door. -------------------

func TestKeywordOnlySaysSoAndResultsAreLocations(t *testing.T) {
	s := openStore(t)
	x := New(s)
	ctx := context.Background()
	must(t, x.EnsureCorpus(ctx, "/kw", CorpusHistory), "ensure")
	must(t, x.InsertChunk("/kw", CorpusHistory, "docs/decisions.md", 41, 58, "h",
		"partial cost stays unknown and never renders as a number"), "chunk")

	hits, av, err := x.KeywordSearch(ctx, "/kw", CorpusHistory, "partial", nil, 5)
	must(t, err, "search")
	if len(hits) != 1 {
		t.Fatalf("the FTS5 door found %d chunks for a word it indexes", len(hits))
	}
	if !av.Available || !av.KeywordOnly {
		t.Fatalf("a no-route query answered without saying keyword-only (D4): %+v", av)
	}
	if !contains(av.Reasons, ReasonNoProvider) {
		t.Fatalf("the keyword-only label travels without its reason (V9's sentence): %+v", av)
	}
	h := hits[0]
	if h.Source != "docs/decisions.md" || h.LineStart != 41 || h.LineEnd != 58 || h.Score <= 0 {
		t.Fatalf("hit %+v is not a file:line range with a score (D8)", h)
	}
}

// --- The no-op is a shape, not a silence (V13 at the seam). --------------

// consumeAnswer is the consumer every backend must satisfy — written against
// Backend only, which is what "the rest of the system can satisfy with a
// no-op" (I2) means in code.
func consumeAnswer(ctx context.Context, b Backend) (string, error) {
	if !b.Built() {
		return "absent — reach for grep", nil
	}
	hits, av, err := b.KeywordSearch(ctx, "/r", CorpusCode, "anything", nil, 5)
	if err != nil {
		return "", err
	}
	if !av.Available {
		return fmt.Sprintf("unavailable: %v", av.Reasons), nil
	}
	return fmt.Sprintf("%d hits", len(hits)), nil
}

func TestNoOpInterfaceSatisfiesCallers(t *testing.T) {
	var b Backend = Noop{}
	got, err := consumeAnswer(context.Background(), b)
	must(t, err, "consume the no-op")
	if got != "absent — reach for grep" {
		t.Fatalf("the consumer read the no-op as %q — absence must be its own shape (V13)", got)
	}
	// The no-op can NEVER make the empty-result claim, misused or not: its
	// search refuses rather than returning a nil slice dressed as an answer.
	if _, _, err := (Noop{}).KeywordSearch(context.Background(), "/r", CorpusCode, "x", nil, 5); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("the misused no-op returned %v — it must refuse with ErrNotBuilt, never answer empty", err)
	}
	// The real backend satisfies the SAME consumer with the SAME call.
	idx := New(openStore(t))
	must(t, idx.EnsureCorpus(context.Background(), "/r", CorpusCode), "ensure")
	got, err = consumeAnswer(context.Background(), idx)
	must(t, err, "consume the real index")
	if got != "unavailable: [index-empty]" {
		t.Fatalf("the real index read as %q — empty must be index-empty, not absent", got)
	}
}

// --- I2: the boundary is the package. -------------------------------------

// TestOnlyTheInterfaceIsImported is E1's grep: no package under aeon/
// imports internal/retrieval except this package itself. Today the correct
// answer is ZERO importers (the wiring lands in T6/T7 through Backend values
// built at the entry point — cmd/aeon, which imports this package, is the
// single named exception T6 records when it arrives); the rule the test
// enforces is that that exception list never grows past the entry point.
func TestOnlyTheInterfaceIsImported(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	self := filepath.Join(root, "internal", "retrieval")
	allowed := map[string]bool{self: true}
	// T6's recorded exception — the one this test's T1 comment
	// pre-authorised: the entry point builds the resolver values and hands
	// them to both doors (cmd/aeon/retrieval_link.go, behind the
	// !noretrieval tag). The rule this enforces is that the list never
	// grows past the entry point.
	allowed[filepath.Join(root, "cmd", "aeon")] = true

	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && (info.Name() == ".git" || info.Name() == "ui" || info.Name() == "dist" || info.Name() == ".tmp") {
				return filepath.SkipDir
			}
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "github.com/cjlapao/aeon/internal/retrieval") {
			dir := filepath.Dir(path)
			if !allowed[dir] {
				t.Errorf("%s imports internal/retrieval directly (I2, E1) — talk to Backend, or be the entry point and record the exception here", strings.TrimPrefix(path, root+"/"))
			}
		}
		return nil
	})
	must(t, err, "walk the module")
	if scanned < 300 {
		t.Fatalf("scanned %d .go files — too few to be the whole module; a scan that scans nothing passes by deletion (S21's floor rule)", scanned)
	}
}
