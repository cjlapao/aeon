package store

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// settings_owner_test.go — S44 T6, guard 2. CS2 and CS11 made mechanical:
// the setting / setting_event tables are owned by the store (the SQL) and
// reached by exactly three others — the daemon's one bridge and config's
// Repo seam (the ONE consumer). A file outside those owners that names the
// tables or the accessors is the ad hoc database-backed flag the whole slice
// exists to replace, caught here rather than by review.
//
// The walk reuses helpers_test.go's grepRepo — the same tree walk
// TestNoSQLLivesOutsideTheStore uses — so this guard does not re-implement the
// walk (T6.md's reuse mandate). Only the classification (which files are the
// owners) is this guard's own, and the meta-test below proves it can fail.

// sqlPattern matches an SQL statement that reads or writes the setting or
// setting_event tables. It is the SQL statement FORM, not the bare word — a
// doc comment that says `setting_event` is a description, not a statement
// about the table, and must not trip the guard. Matching only the forms
// (FROM / INTO / UPDATE / DELETE FROM, then the table) keeps the guard
// precise and free of comment false-positives.
var sqlPattern = regexp.MustCompile(`(?:FROM|INTO|UPDATE|DELETE FROM)\s+setting(?:_event)?\b`)

// accessorPattern names the store's Go accessors for the setting tables —
// the bridge a consumer crosses. The closed set, not a substring: a stray
// "SettingEvent" (the audit type) is not an accessor.
var accessorPattern = regexp.MustCompile(`\b(SettingGet|SettingPut|SettingDelete|SettingList|SettingEventList)\b`)

// allowedSQL is the one owner of the SQL text (CS2). The setting tables' SQL
// lives in exactly this file and its test; a statement about the tables
// anywhere else is a second truth.
var allowedSQL = map[string]bool{
	"internal/store/settings.go": true,
}

// allowedAccessors is every file that legitimately NAMES the accessors — the
// owner and the two others CS11 permits. This is the honest reading of the
// rule, wider than T6.md's two-file list because config's Repo interface (the
// ONE consumer's seam) and the store's own doc comment must name the
// accessors they bridge; a consumer that reached the table directly would be
// a violation, but the consumer reading through config's Repo is the design.
// See the Notes for the deviation.
var allowedAccessors = map[string]bool{
	"internal/store/settings.go":        true, // the definitions
	"internal/store/store.go":           true, // the store's own doc comment names them
	"internal/daemon/settings_repo.go":  true, // the one bridge: config.Repo -> store
	"internal/config/settings_store.go": true, // the one consumer's Repo seam + calls
}

// TestOnlyConfigAndTheDaemonAdapterTouchTheSettingTables is CS2 and CS11
// mechanical. It walks the module's non-test .go files (reusing grepRepo) and
// asserts: (1) the setting-table SQL appears only in the store's own
// settings.go, and (2) the accessors appear only in the store, the daemon's
// one bridge, and config's Repo seam. Both halves assert they found their
// owner, so a guard that walks an empty tree cannot pass (T6.md's Do-NOT).
func TestOnlyConfigAndTheDaemonAdapterTouchTheSettingTables(t *testing.T) {
	// (1) SQL (CS2): the setting / setting_event tables are written only by
	// the store. A second SQL statement about these tables is a second truth
	// — the ad hoc database-backed flag the slice replaces.
	sqlHits := grepRepo(t, sqlPattern)
	if !sawFile(sqlHits, "internal/store/settings.go") {
		t.Fatalf("the SQL walk found no setting-table SQL in its own owner (internal/store/settings.go) — the grep checked nothing")
	}
	for _, o := range settingTableOffenders(sqlHits, allowedSQL) {
		t.Errorf("%s writes setting-table SQL outside the store — SQL lives only in internal/store (CS2):\n    %s", o.file, o.line)
	}

	// (2) Accessors (CS11): a consumer reads config's Repo, never the table.
	// The accessors are reached only by the store's own definitions, the
	// daemon's one bridge, and config's Repo seam (the ONE consumer).
	accHits := grepRepo(t, accessorPattern)
	if !sawFile(accHits, "internal/store/settings.go") {
		t.Fatalf("the accessor walk found nothing in its owner — the grep checked nothing")
	}
	// Non-vacuous in the direction that matters: the walk must have seen the
	// consumer's own calls (config's Repo seam), or the allowed set names a
	// consumer that is not actually there.
	if !sawFile(accHits, "internal/config/settings_store.go") {
		t.Errorf("the accessor walk did not see the config consumer's calls — the allowed set names a consumer that is not in the tree")
	}
	for _, o := range settingTableOffenders(accHits, allowedAccessors) {
		t.Errorf("%s references the setting accessors — a consumer reads config's Repo, never the table (CS11):\n    %s", o.file, o.line)
	}
}

// TestTableGuardCanFail proves guard 2's walk is not vacuous: feed it a temp
// dir holding a file with `FROM setting` OUTSIDE the allowed owners and it
// must report the offender. A guard that cannot fail is not a guard.
//
// grepRepo (used by the real guard) is fixed to internal/cmd/scripts from the
// module root, so it cannot be pointed at a temp dir; this meta-test carries a
// parameterized twin of the same walk and shares the SAME classification
// predicate (settingTableOffenders) the real guard uses, so it proves the
// decision logic, not a parallel one.
func TestTableGuardCanFail(t *testing.T) {
	dir := t.TempDir()
	poison := filepath.Join(dir, "cmd", "evil.go")
	if err := os.MkdirAll(filepath.Dir(poison), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file outside the allowed owners that writes setting-table SQL.
	if err := os.WriteFile(poison, []byte(
		"package evil\n\nfunc f() {\n\t_ = `SELECT key FROM setting WHERE key = ?`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := walkRoot(t, dir, sqlPattern)
	if len(hits) == 0 {
		t.Fatal("the walk found nothing in a poisoned tree — it cannot detect a violation (vacuous)")
	}
	if bad := settingTableOffenders(hits, allowedSQL); len(bad) == 0 {
		t.Fatalf("a file with `FROM setting` outside the owner was not reported — the guard cannot fail (vacuous)")
	}
	// And an allowed file is NOT an offender (the predicate is specific): the
	// store's own SQL is allowed, never a violation.
	owner := []repoHit{{file: "internal/store/settings.go", line: "SELECT ... FROM setting"}}
	if bad := settingTableOffenders(owner, allowedSQL); len(bad) != 0 {
		t.Fatalf("the owner itself was reported as an offender — the predicate fires on what it should allow: %v", bad)
	}
}

// settingTableOffenders returns the hits that fall outside the allowed owners
// — the classification shared by the real guard and its meta-test, so the
// meta-test proves the guard's decision rather than a parallel walk.
func settingTableOffenders(hits []repoHit, allowed map[string]bool) []repoHit {
	var bad []repoHit
	for _, h := range hits {
		if !allowed[h.file] {
			bad = append(bad, h)
		}
	}
	return bad
}

// sawFile reports whether any hit is in the named file — the non-vacuous
// assertion: the guard must have found its owner, or it checked nothing.
func sawFile(hits []repoHit, file string) bool {
	for _, h := range hits {
		if h.file == file {
			return true
		}
	}
	return false
}

// walkRoot mirrors grepRepo's walk but takes the root, so a meta-test can
// point it at a temp dir (grepRepo is fixed to internal/cmd/scripts from the
// module root and cannot be). Same shape: .go, non-test, per-line.
func walkRoot(t *testing.T, root string, pattern *regexp.Regexp) []repoHit {
	t.Helper()
	var hits []repoHit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, line := range strings.Split(string(b), "\n") {
			if pattern.MatchString(line) {
				hits = append(hits, repoHit{file: rel, line: strings.TrimSpace(line)})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}
