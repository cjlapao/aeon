package scaffoldseed_test

// scaffoldseed_test.go — S61 T3's seed half (GI14/GI15): the set is declared
// and inert, the golden guards the shipped bodies, the engine's plans are the
// honest ones (fresh stores get system rows; a person's edit diverges and is
// never clobbered; the schema itself makes the rows undeletable, so there is
// no resurrection road to walk), and the shipped bytes carry no host, no
// credential, no URL and no external action beyond the pinned checkout.
// The external test package imports the daemon's engine exactly as
// retroseed_test.go does (a test-only cycle is allowed; the recorded
// deviation about decl files living in the domain package applies verbatim).

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/scaffold"
	scaffoldseed "github.com/cjlapao/aeon/internal/scaffold/seed"
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
	res, err := e.Apply(context.Background(), scaffoldseed.SetID, nil, seed.Actor{Kind: "system", ID: "test"}, seed.PathExplicit)
	if err != nil {
		t.Fatalf("apply %s: %v", scaffoldseed.SetID, err)
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

func TestSourceHasOneRecordPerShippedTemplate(t *testing.T) {
	recs := scaffoldseed.Source()
	want := []string{"go-service", "minimal", "node-ts"}
	if len(recs) != len(want) {
		t.Fatalf("records = %d, want %d (GI14's three starters)", len(recs), len(want))
	}
	got := make([]string, 0, len(recs))
	for _, r := range recs {
		got = append(got, r.Key)
		c, err := scaffoldseed.ContentOf(r)
		if err != nil {
			t.Fatalf("%s: %v", r.Key, err)
		}
		if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Description) == "" {
			t.Errorf("%s: no pick-by words — a menu entry nobody can choose", r.Key)
		}
		// The seed's bytes must render under the CLOSED variable set — the
		// source and the renderer share one law (Source would panic at
		// init if a shipped path broke GI9's; this names the slug if the
		// shape ever changes).
		tmpl := scaffold.Template{Slug: r.Key}
		for _, f := range c.Files {
			tmpl.Files = append(tmpl.Files, scaffold.TemplateFile{Path: f.Path, Mode: os.FileMode(f.Mode), Content: f.Content})
		}
		if _, err := scaffold.Render(tmpl, scaffold.Vars{Name: "N", Slug: "n", Description: "D", Stack: []string{"go"}, DefaultBranch: "main"}); err != nil {
			t.Errorf("%s: shipped content does not render: %v", r.Key, err)
		}
		if r.Version != 1 {
			t.Errorf("%s: version %d — a shipped set starts at 1 (the golden owns later bumps)", r.Key, r.Version)
		}
	}
	sort.Strings(got)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v (three starters, exactly)", got, want)
		}
	}
}

// --- the golden ritual ----------------------------------------------------

func TestSeededTemplatesGolden(t *testing.T) {
	path := filepath.Join("testdata", "templates.versions.golden")
	recs := scaffoldseed.Source()
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

// --- shipped bytes: no host, no credential, no stranger's action ----------

func TestShippedContentCarriesNoHostNoCredentialNoURL(t *testing.T) {
	banned := []*regexp.Regexp{
		regexp.MustCompile(`/Users/`),
		regexp.MustCompile(`/home/`),
		regexp.MustCompile(`C:\\`),
		regexp.MustCompile(`ghp_`),
		regexp.MustCompile(`sk-`),
		regexp.MustCompile(`AKIA`),
		regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`), // a 40+ char base64 run
		regexp.MustCompile(`secrets\.`),
		regexp.MustCompile(`https?://`),
		regexp.MustCompile(`\b\w+\.\w\w+/\w+`), // any URL-ish host/path without a scheme too
	}
	for _, r := range scaffoldseed.Source() {
		body := string(r.Content)
		for _, re := range banned {
			if m := re.FindString(body); m != "" {
				// The 40+ base64 rule may flash on ordinary long prose words
				// with no spaces; report WHAT matched so a human sees.
				t.Errorf("%s: shipped content holds %q (banned shape %s)", r.Key, m, re)
			}
		}
	}
}

func TestShippedCIFilesUseOnlyThePinnedCheckout(t *testing.T) {
	uses := regexp.MustCompile(`uses:\s*(\S+)`)
	seen := 0
	for _, r := range scaffoldseed.Source() {
		c, err := scaffoldseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range c.Files {
			if !strings.HasPrefix(f.Path, ".github/workflows/") {
				continue
			}
			for _, m := range uses.FindAllStringSubmatch(f.Content, -1) {
				seen++
				if m[1] != "actions/checkout@v7" {
					t.Errorf("%s %s: external action %q — the only action a shipped CI file may reference is the pinned checkout", r.Key, f.Path, m[1])
				}
			}
		}
	}
	if seen != 2 {
		t.Fatalf("saw %d action references, want 2 (the two CI starters each pin checkout)", seen)
	}
}

// --- class and rights ------------------------------------------------------

func TestTemplatesScaffoldIsInertWithAnEditDoorAndNoRemove(t *testing.T) {
	set, ok := seed.DefaultRegistry().Lookup(scaffoldseed.SetID)
	if !ok {
		t.Fatalf("set %s is not declared at init", scaffoldseed.SetID)
	}
	if set.Class != seed.ClassInert {
		t.Fatalf("class %q, want inert (a menu entry widens nothing)", set.Class)
	}
	ap := scaffoldseed.NewApplier(nil)
	if ap.GuardAffecting() {
		t.Fatal("GuardAffecting true — an inert set may not claim to change what is permitted")
	}
	op := ap.Operator()
	if !op.Edit || op.Remove {
		t.Fatalf("operator rights %+v, want {Edit:true, Remove:false} (GI14: a person edits a template through their door; removal has no door)", op)
	}
	if err := ap.Retire(nil, "minimal"); err == nil {
		t.Fatal("Retire returned nil — the set must refuse to retire a shipped template")
	}
}

// --- engine behaviours -----------------------------------------------------

func TestFreshStoreSeedsEverythingWithSystemOrigin(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	recs := applySet(t, e)
	if n := countApplied(recs); n != len(scaffoldseed.Source()) {
		t.Fatalf("applied %d, want %d: %+v", n, len(scaffoldseed.Source()), recs)
	}
	rows, err := s.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("live rows %d, want 3", len(rows))
	}
	for _, row := range rows {
		if row.Origin != "system" {
			t.Errorf("%s: origin %q, want system", row.Slug, row.Origin)
		}
		revs, err := s.TemplateRevisions(row.Slug)
		if err != nil {
			t.Fatal(err)
		}
		if len(revs) != 1 || revs[0].ActorKind != "system" || revs[0].ActorID != "seed" {
			t.Errorf("%s: revisions = %+v, want one row authored system:seed", row.Slug, revs)
		}
	}
	// A freshly laid row fingerprints EQUAL to its record — a drift here
	// means the fingerprint rebuild lies about bytes that never moved (the
	// empty-StackTags round-trip was exactly this bug, caught by the
	// doctor-up golden's "1 diverged" line).
	ap := scaffoldseed.NewApplier(s)
	for _, r := range scaffoldseed.Source() {
		if err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
			h, found, err := ap.Fingerprint(tx, r.Key)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("%s: row absent after apply", r.Key)
			}
			if h != seed.Hash(r.Content) {
				return fmt.Errorf("%s: live fingerprint %s, record %s — the rebuild is unfaithful", r.Key, h, seed.Hash(r.Content))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
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
	_, ref, err := s.SaveTemplate(store.NewTemplate{
		Slug: "minimal", Title: "My minimal", Description: "mine now",
		Files: []store.TemplateFile{{Path: "README.md", Mode: 0o644, Content: "MY README\n"}},
	}, store.Actor{Kind: "user", ID: "cj"})
	if ref != nil || err != nil {
		t.Fatalf("the person's edit door: ref=%v err=%v", ref, err)
	}
	applySet(t, e) // the fingerprint diverged — the engine must skip, never clobber
	row, err := s.GetTemplate("minimal")
	if err != nil {
		t.Fatalf("minimal row gone: %v", err)
	}
	if row.Files[0].Content != "MY README\n" || row.Title != "My minimal" {
		t.Fatalf("the apply overwrote the diverged template: %+v", row)
	}
	if row.Origin != "system" {
		t.Fatalf("origin %q — a shipped template's origin is its history, an edit says diverged through the version", row.Origin)
	}
}

// TestDeletedTemplateRowIsRefusedByTheSchema is the never-resurrected law's
// stronger shape here: GI14 gave the table no delete path at all (T1's
// trigger), so an out-of-band vanishing has no SQL door — the only
// "resurrection" possible is the engine re-laying a row the ledger already
// calls applied, and that is what an ordinary apply must never do.
func TestDeletedTemplateRowIsRefusedByTheSchema(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	applySet(t, e)
	err := s.VerifyWithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM scaffold_template WHERE slug = 'minimal'`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "never deleted") {
		t.Fatalf("a DELETE on scaffold_template: %v — the no-delete trigger must refuse it", err)
	}
	if n := countApplied(applySet(t, e)); n != 0 {
		t.Fatal("an ordinary apply re-laid rows the ledger already calls applied")
	}
}

func TestSeedStatusListsTheSet(t *testing.T) {
	s := newSeedStore(t)
	e := seedEngine(t, s)
	statuses, err := e.Status(context.Background(), scaffoldseed.SetID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range statuses {
		if statuses[i].ID == scaffoldseed.SetID {
			if statuses[i].Class != seed.ClassInert {
				t.Fatalf("status class %q, want inert", statuses[i].Class)
			}
			return
		}
	}
	t.Fatal("seed.status does not list templates.scaffold")
}

// TestNoSQLOutsideTheStore holds the applier to its one table: it writes
// through the store's Tx doors, and nothing shaped like SQL lives here.
func TestNoSQLOutsideTheStore(t *testing.T) {
	for _, name := range []string{"applier.go", "source.go", "decl.go", "versions.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		for _, bad := range []string{"INSERT INTO", "UPDATE scaffold", "DELETE FROM"} {
			if strings.Contains(text, bad) {
				t.Fatalf("%s touches %q — the applier's doors are store methods, and SQL lives in internal/store", name, bad)
			}
		}
	}
}
