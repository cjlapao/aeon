package store

// S51 T1 (TM3) — the filed_by name→slug migration, pinned the way S43 T2
// pinned its policy twin: resolve, refuse NAMING EVERY orphan, idempotent,
// literal-exempt. The refusal test is the heart of it: half-migrated
// filed_by is the orphan the rule exists to prevent, so a single unresolved
// name refuses the WHOLE thing and the sentence names every one.

import (
	"context"
	"strings"
	"testing"
)

func TestFiledByMigrationResolvesNamesToSlugs(t *testing.T) {
	s := openTemp(t)
	root := "/repo/filedby"
	seedBugComponent(t, s, "filedby", root)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "golang-engineer", Name: "Golang Engineer", Version: "1"}, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatalf("seed the agent: %v", err)
	}
	// The pre-migration state: filed_by carries the agent NAME (what the
	// old wire param used to write unchecked), plus both literals.
	named := seedBugPlain(t, s, root, "named filer")
	if _, err := s.w.Exec(`UPDATE bug SET filed_by = 'Golang Engineer' WHERE id = ?`, named.ID); err != nil {
		t.Fatal(err)
	}

	n, err := s.MigrateBugFiledBySlugs()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n != 1 {
		t.Fatalf("re-pointed %d rows, want 1", n)
	}
	moved, _, _ := s.BugByID(named.ID)
	if moved.FiledBy != "golang-engineer" {
		t.Fatalf("the named filer now reads %q — want the slug", moved.FiledBy)
	}
}

// TestFiledByMigrationIsIdempotent — a value already equal to a slug is
// SKIPPED, which is what makes a re-run inert (S43's own discipline).
func TestFiledByMigrationIsIdempotent(t *testing.T) {
	s := openTemp(t)
	root := "/repo/idem"
	seedBugComponent(t, s, "idem", root)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "golang-engineer", Name: "Golang Engineer", Version: "1"}, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	b := seedBugPlain(t, s, root, "named once")
	if _, err := s.w.Exec(`UPDATE bug SET filed_by = 'Golang Engineer' WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	n, err := s.MigrateBugFiledBySlugs()
	if err != nil || n != 1 {
		t.Fatalf("first run: %d rows, err %v — want 1, nil", n, err)
	}
	again, err := s.MigrateBugFiledBySlugs()
	if err != nil || again != 0 {
		t.Fatalf("second run: %d rows, err %v — want 0, nil (slug-valued rows skip)", again, err)
	}
	third, err := s.MigrateBugFiledBySlugs()
	if err != nil || third != 0 {
		t.Fatalf("third run: %d rows, err %v — want 0, nil", third, err)
	}
}

// TestFiledByMigrationLeavesPersonUntouched — the two literals, 'person'
// (TM3's own word) and 'system:gates' (the S57 filer), stay byte for byte
// on every run: they are not agent references and have nothing to resolve.
func TestFiledByMigrationLeavesPersonUntouched(t *testing.T) {
	s := openTemp(t)
	root := "/repo/literals"
	seedBugComponent(t, s, "literals", root)
	person := seedBugPlain(t, s, root, "person filing")
	gated := seedBugPlain(t, s, root, "gate filing")
	if _, err := s.w.Exec(`UPDATE bug SET filed_by = 'system:gates' WHERE id = ?`, gated.ID); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		n, err := s.MigrateBugFiledBySlugs()
		if err != nil || n != 0 {
			t.Fatalf("a store of literals only re-pointed %d rows, err %v — want 0, nil", n, err)
		}
	}
	for _, keep := range []struct {
		id int64
		as string
	}{{person.ID, "person"}, {gated.ID, "system:gates"}} {
		row, _, _ := s.BugByID(keep.id)
		if row.FiledBy != keep.as {
			t.Fatalf("bug %d reads %q — want the literal %q untouched", keep.id, row.FiledBy, keep.as)
		}
	}
}

func TestFiledByMigrationRefusesTheWholeMigrationNamingUnresolved(t *testing.T) {
	s := openTemp(t)
	root := "/repo/orphans"
	seedBugComponent(t, s, "orphans", root)
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "golang-engineer", Name: "Golang Engineer", Version: "1"}, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatalf("seed the agent: %v", err)
	}
	good := seedBugPlain(t, s, root, "resolves")
	if _, err := s.w.Exec(`UPDATE bug SET filed_by = 'Golang Engineer' WHERE id = ?`, good.ID); err != nil {
		t.Fatal(err)
	}
	first := seedBugPlain(t, s, root, "ghost one")
	second := seedBugPlain(t, s, root, "ghost two")
	for _, pair := range []struct {
		id   int64
		name string
	}{{first.ID, "Ghost One"}, {second.ID, "Ghost Two"}} {
		if _, err := s.w.Exec(`UPDATE bug SET filed_by = ? WHERE id = ?`, pair.name, pair.id); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.MigrateBugFiledBySlugs()
	if err == nil {
		t.Fatal("orphaned names were accepted — the migration ran half-migrated")
	}
	if n != 0 {
		t.Fatalf("a refused migration re-pointed %d rows, want 0 — all-or-nothing", n)
	}
	msg := err.Error()
	// The refusal names EVERY orphan at once (the operator hears the whole
	// list) and keeps the one that resolves OUT of the orphan line.
	for _, want := range []string{`"Ghost One"`, `"Ghost Two"`, "2 name(s)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the refusal %q does not name %s", msg, want)
		}
	}
	resolves, _, _ := s.BugByID(good.ID)
	if resolves.FiledBy != "Golang Engineer" {
		t.Fatalf("the refused migration applied anyway — filed_by reads %q", resolves.FiledBy)
	}
	// Fix the orphans (the operator's path): insert the agent rows, re-run.
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "ghost-one", Name: "Ghost One", Version: "1"}, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "ghost-two", Name: "Ghost Two", Version: "1"}, "2026-11-04T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	n, err = s.MigrateBugFiledBySlugs()
	if err != nil || n != 3 {
		t.Fatalf("after fixing the orphans: %d rows, err %v — want 3, nil", n, err)
	}
}
