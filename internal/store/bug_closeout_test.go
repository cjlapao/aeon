package store

// S51 T7 close-out drill (P9) — the pre-S51-vintage store migrates forward
// with the bug rows byte-identical, the filed_by law applied ON THE WAY,
// and the shape equal to fresh. The sibling tests (T1) pin each piece
// separately; this one walks the whole close-out claim in one store:
// counts and a fold of `bug` survive the upgrade, the planted unresolved
// name REFUSES the sweep NAMING it (then, fixed, the sweep succeeds),
// 'person' and 'system:gates' ride untouched, and the upgraded fingerprint
// equals a fresh database's — S50's closeout precedent, kept.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// bugFold folds the whole bug table into one string — id, title, body, the
// two state sets and the filer word, in id order. Any row the migration
// lost, reordered, or rewrote beyond the filed_by law changes it.
func bugFold(t *testing.T, g *gorm.DB) string {
	t.Helper()
	rows, err := g.Raw(`SELECT id, title, body, validation_state, priority_state, filed_by
		FROM bug ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var id int64
		var title, body, validation, priority, filedBy string
		if err := rows.Scan(&id, &title, &body, &validation, &priority, &filedBy); err != nil {
			t.Fatalf("fold scan: %v", err)
		}
		b.WriteString(strconv.FormatInt(id, 10) + "~" + title + "~" + body + "~" + validation + "~" + priority + "~" + filedBy + "|")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("fold rows: %v", err)
	}
	return b.String()
}

func TestS51CloseoutUpgradePreservesBugRowsAndAudit(t *testing.T) {
	fresh := openTemp(t)
	freshFp, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatalf("fresh fingerprint: %v", err)
	}

	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	applyMigrations(t, old.GormDB(), Migrations()[:bugChangeMigrationIndex()])
	root := "/repo/s51closeout"
	seedBugComponent(t, old, "s51closeout", root)
	seedAgentRowBeforeS73(t, old, "golang-engineer", "Golang Engineer", "2026-11-01T00:00:00Z")
	named := seedBugPlain(t, old, root, "named filer")
	person := seedBugPlain(t, old, root, "person filing")
	gated := seedBugPlain(t, old, root, "gate filing")
	orphan := seedBugPlain(t, old, root, "planted orphan")
	for row, word := range map[int64]string{
		named.ID:  "Golang Engineer",
		person.ID: "person",
		gated.ID:  "system:gates",
		orphan.ID: "Nobody Real",
	} {
		if _, err := old.w.Exec(`UPDATE bug SET filed_by = ? WHERE id = ?`, word, row); err != nil {
			t.Fatal(err)
		}
	}
	before := bugFold(t, old.GormDB())
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	// Upgrade: the schema moves forward through the real runner.
	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	upFp, err := s.SchemaFingerprint()
	if err != nil {
		t.Fatalf("upgraded fingerprint: %v", err)
	}
	if !freshFp.Equal(upFp) {
		t.Fatalf("fresh and upgraded shapes differ:\n%s", strings.Join(freshFp.Diff(upFp), "\n"))
	}
	assertBugChangeCatalogue(t, s)

	// The filed_by sweep THEN runs (the daemon-start Go migration) — and the
	// planted unresolved name refuses the WHOLE sweep, naming it.
	if _, err := s.MigrateBugFiledBySlugs(); err == nil {
		t.Fatal("the sweep accepted the planted orphan — the all-or-nothing law is broken")
	} else if !strings.Contains(err.Error(), "Nobody Real") {
		t.Fatalf("the refusal does not name the orphan: %v", err)
	}
	// A refused sweep moved NOTHING: the fold is the seed's fold.
	if bugFold(t, s.GormDB()) != before {
		t.Fatal("the refused sweep still moved rows — all-or-nothing is a lie")
	}

	// Fix the orphan (the operator registers the agent), re-run: success.
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "nobody-real", Name: "Nobody Real", Version: "1"}, "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("fixing the orphan: %v", err)
	}
	n, err := s.MigrateBugFiledBySlugs()
	if err != nil || n != 2 {
		t.Fatalf("sweep after the fix: %d rows, %v — want 2 (the named filer and the former orphan)", n, err)
	}
	var filedBy string
	if err := s.GormDB().Raw(`SELECT filed_by FROM bug WHERE id = ?`, named.ID).Scan(&filedBy).Error; err != nil {
		t.Fatal(err)
	}
	if filedBy != "golang-engineer" {
		t.Fatalf("named filer reads %q — want the slug", filedBy)
	}
	for id, want := range map[int64]string{person.ID: "person", gated.ID: "system:gates"} {
		if err := s.GormDB().Raw(`SELECT filed_by FROM bug WHERE id = ?`, id).Scan(&filedBy).Error; err != nil {
			t.Fatal(err)
		}
		if filedBy != want {
			t.Fatalf("literal filer %d reads %q — the literals ride untouched", id, filedBy)
		}
	}
	// The rows themselves are the seed's rows — states never moved, only
	// the filer words resolved. Count them in their seed shape.
	var intact int
	if err := s.GormDB().Raw(`SELECT count(*) FROM bug
		WHERE validation_state = 'unvalidated' AND priority_state = 'unknown'
		  AND title IN ('named filer','person filing','gate filing','planted orphan')`).
		Scan(&intact).Error; err != nil {
		t.Fatal(err)
	}
	if intact != 4 {
		t.Fatalf("the bug rows themselves changed shape: %d intact, want 4", intact)
	}
}
