package retroseed_test

// retroseed_test.go — S63 T3's seed half (RT16): the set is declared and
// inert, the golden ritual guards the shipped bodies, and the engine's
// plans are the honest ones — fresh stores get system rows, divergence is
// never overwritten, and an out-of-band vanished row is repaired only by
// an explicit apply (the set has no retire or edit door to walk through).
// The external test package imports the daemon's engine exactly as
// verifyseed_test.go does (a test-only cycle is allowed; verifyseed's
// recorded deviation about decl files living in the domain package
// applies verbatim here).

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/retro"
	retroseed "github.com/cjlapao/aeon/internal/retro/seed"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

var updateGolden = flag.Bool("update", false, "rewrite the versions golden")

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

func applySet(t *testing.T, e *seed.Engine) []seed.RecordResult {
	t.Helper()
	res, err := e.Apply(context.Background(), retroseed.SetID, nil, seed.Actor{Kind: "system", ID: "test"}, seed.PathExplicit)
	if err != nil {
		t.Fatalf("apply %s: %v", retroseed.SetID, err)
	}
	return res.Records
}

func countApplied(recs []seed.RecordResult) int {
	n := 0
	for _, r := range recs {
		if r.Op != seed.OpSkip {
			n++
		}
	}
	return n
}

// --- source shape -------------------------------------------------------

func TestSourceHasOneRecordPerShippedFamily(t *testing.T) {
	recs := retroseed.Source()
	want := []string{"aborted", "blocked", "default", "done-with-rework", "unverified"}
	if len(recs) != len(want) {
		t.Fatalf("records = %d, want %d (RT16's five families)", len(recs), len(want))
	}
	got := make([]string, 0, len(recs))
	for _, r := range recs {
		got = append(got, r.Key)
		c, err := retroseed.ContentOf(r)
		if err != nil {
			t.Fatalf("%s: %v", r.Key, err)
		}
		if strings.TrimSpace(c.Template) == "" {
			t.Errorf("%s: empty body", r.Key)
		}
		// The seed door and the render door share ONE grammar: the body
		// that ships must pass Validate (Source would panic at init if
		// not — this line names the family if it ever changes shape).
		if err := retro.Validate(c.Template); err != nil {
			t.Errorf("%s: body fails the closed grammar: %v", r.Key, err)
		}
		if r.Version != 1 {
			t.Errorf("%s: version %d — a shipped set starts at 1 (the golden owns later bumps)", r.Key, r.Version)
		}
	}
	// The SET of keys is the contract; the source's own order is the
	// narrative one (default first, the scar families, blocked last).
	sort.Strings(got)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v (five families, exactly)", got, want)
		}
	}
}

// --- the golden ritual ---------------------------------------------------

func TestRetroTemplateVersionsBumpWithContent(t *testing.T) {
	path := filepath.Join("testdata", "templates.versions.golden")
	recs := retroseed.Source()
	var lines []string
	for _, r := range recs {
		lines = append(lines, fmt.Sprintf("%s %d %s", r.Key, r.Version, seed.Hash(r.Content)))
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d lines to %s", len(lines), path)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the versions golden (run with -update to create it): %v", err)
	}
	want := map[string][2]string{}
	var order []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("golden line %q is not `key version hash`", line)
		}
		want[parts[0]] = [2]string{parts[1], parts[2]}
		order = append(order, parts[0])
	}
	got := map[string][2]string{}
	for _, l := range lines {
		parts := strings.Fields(l)
		got[parts[0]] = [2]string{parts[1], parts[2]}
	}
	for _, key := range order {
		g, ok := got[key]
		if !ok {
			t.Fatalf("golden line for %q has no record — a removed template? the golden is rewritten with -update only", key)
		}
		if g[1] != want[key][1] {
			if g[0] == want[key][0] {
				t.Errorf("%s: content changed but version did not — bump it (versions.go ritual), then -update the golden", key)
			} else {
				t.Errorf("%s: version moved %s→%s with the hash — re-run with -update and commit the golden's diff", key, want[key][0], g[0])
			}
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Fatalf("record %q has no golden line — a new template: add its line with -update", key)
		}
	}
}

// --- class and rights -----------------------------------------------------

func TestRetroTemplatesIsInertWithNoHumanDoors(t *testing.T) {
	set, ok := seed.DefaultRegistry().Lookup(retroseed.SetID)
	if !ok {
		t.Fatalf("set %s is not declared at init", retroseed.SetID)
	}
	if set.Class != seed.ClassInert {
		t.Fatalf("class %q, want inert (a template renders memory, widens nothing)", set.Class)
	}
	ap := retroseed.NewApplier(nil)
	if ap.GuardAffecting() {
		t.Fatal("GuardAffecting true — an inert set may not claim to change what is permitted")
	}
	op := ap.Operator()
	if op.Edit || op.Remove {
		t.Fatalf("operator rights %+v, want {Edit:false, Remove:false} (RT16: a person edits nothing through the set — the rows they own were never the set's to hold)", op)
	}
	// No retire door: the applier itself refuses, store or no store.
	if err := ap.Retire(nil, "default"); err == nil {
		t.Fatal("Retire returned nil — the set must refuse to retire a shipped body")
	}
}

// --- engine behaviours ------------------------------------------------------

func TestFreshStoreSeedsEverythingWithSystemOrigin(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	recs := applySet(t, e)
	if n := countApplied(recs); n != len(retroseed.Source()) {
		t.Fatalf("applied %d, want %d: %+v", n, len(retroseed.Source()), recs)
	}
	rows, err := s.ListRetroTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(retroseed.Source()) {
		t.Fatalf("live rows %d, want %d", len(rows), len(retroseed.Source()))
	}
	for _, row := range rows {
		if row.Origin != "system" {
			t.Errorf("%s: origin %q, want system", row.Kind, row.Origin)
		}
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	if n := countApplied(applySet(t, e)); n != 0 {
		t.Fatalf("second apply wrote %d, want 0", n)
	}
}

func TestDivergedTemplateIsNeverOverwritten(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	const mine = "Blocked loops on {blueprint}. Mine now, covering {window}."
	if err := s.UpsertRetroTemplate("blocked", 1, mine, "user"); err != nil {
		t.Fatal(err)
	}
	applySet(t, e) // the fingerprint diverged — the engine must skip, never clobber
	row, found, err := s.GetRetroTemplate("blocked")
	if err != nil || !found {
		t.Fatalf("blocked row gone: found=%v err=%v", found, err)
	}
	if row.Template != mine {
		t.Fatalf("the apply overwrote the diverged body:\n got %q\nwant %q", row.Template, mine)
	}
	if row.Origin != "user" {
		t.Fatalf("the apply re-stamped origin %q — the row belongs to its writer", row.Origin)
	}
}

// TestOutOfBandLossNeverResurrectedRestoredExplicitly is RT16's restore
// half read honestly for a set with NO remove door: nothing in the system
// can retire or delete a template row (no store method, no engine plan),
// so the only way a row vanishes is out-of-band damage. An ordinary
// apply — even the explicit human door's apply — does NOT resurrect it:
// the ledger says applied, and the engine never re-lays an applied row it
// did not plan. What puts the row back is the engine's ONE restore door,
// attributed to an actor, writing the ledger and the event itself.
func TestOutOfBandLossNeverResurrectedRestoredExplicitly(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM retro_template WHERE kind = 'aborted'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.GetRetroTemplate("aborted"); found {
		t.Fatal("the row survived the delete")
	}
	applySet(t, e) // silent resurrection would fail HERE
	if _, found, _ := s.GetRetroTemplate("aborted"); found {
		t.Fatal("apply resurrected a row the engine never planned — removal is never silent")
	}
	res, err := e.Restore(context.Background(), retroseed.SetID, "aborted", seed.Actor{Kind: "system", ID: "test"})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(res.Records) == 0 || res.Records[0].Op == seed.OpSkip {
		t.Fatalf("restore skipped: %+v", res.Records)
	}
	row, found, err := s.GetRetroTemplate("aborted")
	if err != nil || !found {
		t.Fatalf("restore did not lay the row: found=%v err=%v", found, err)
	}
	if row.Origin != "system" {
		t.Errorf("restored origin %q, want system", row.Origin)
	}
}

func TestSeedStatusListsTheSet(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	statuses, err := e.Status(context.Background(), retroseed.SetID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range statuses {
		if statuses[i].ID == retroseed.SetID {
			if statuses[i].Class != seed.ClassInert {
				t.Fatalf("status class %q, want inert", statuses[i].Class)
			}
			return
		}
	}
	t.Fatal("seed.status does not list retro.templates")
}

// TestNoSecondOutcomeWriter guards RT4's one-door rule from the seed
// side: the set writes template rows through the store's Tx door only,
// and nothing named like an outcome INSERT lives in the package.
func TestNoSecondOutcomeWriter(t *testing.T) {
	for _, name := range []string{"applier.go", "source.go", "decl.go", "versions.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		for _, bad := range []string{"retro_outcome", "retro_learning", "INSERT INTO"} {
			if strings.Contains(text, bad) {
				t.Fatalf("%s touches %q — the applier's one table is retro_template, and SQL lives in internal/store", name, bad)
			}
		}
	}
}
