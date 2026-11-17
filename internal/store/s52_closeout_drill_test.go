package store

// S52 T6 close-out drill (P9) — the pre-S52-vintage store migrates forward
// with the memory rows byte-identical, and the upgraded shape equals a fresh
// database's; a planted agent-scope key that resolves to NO agent refuses
// the WHOLE slug sweep at the open — naming it, moving nothing — and the
// store opens only once the operator makes it resolvable. The sibling
// tests pin each piece separately (T1's migration test, T2's retraction
// readers); this one walks the close-out claim end to end, S51's
// bug_closeout_drill precedent kept. The fold covers the three tables the
// close-out names: learning, learning_tag, proposal.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// s52LearningManagementMigrationIndex is the S52 schema migration's position
// BY NAME (S57 T7's lesson: "the last" is a positional shortcut that breaks
// the first time a later slice appends).
func s52LearningManagementMigrationIndex(t *testing.T) int {
	t.Helper()
	for i, m := range Migrations() {
		if strings.HasSuffix(m.Name, "_learning_management") {
			return i
		}
	}
	t.Fatal("the learning_management migration is missing from Migrations()")
	return 0
}

// memoryFold folds learning, learning_tag and proposal into one string in
// id order. Any row the upgrade lost, reordered or rewrote changes it. The
// learning columns folded are the PRE-S52 ones — the pre-slice rows must
// survive with exactly the values they had; the stamp columns the upgrade
// adds are NULL there and proven round-trip in T1's own tests.
func memoryFold(t *testing.T, g *gorm.DB) string {
	t.Helper()
	var b strings.Builder
	learn, err := g.Raw(`SELECT id, scope, scope_key, body, provenance, observed_count
		FROM learning ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("learning fold: %v", err)
	}
	for learn.Next() {
		var id int64
		var count int
		var scope, key, body, prov string
		if err := learn.Scan(&id, &scope, &key, &body, &prov, &count); err != nil {
			t.Fatalf("learning fold scan: %v", err)
		}
		b.WriteString("L" + strconv.FormatInt(id, 10) + "~" + scope + "~" + key + "~" + body + "~" + prov + "~" + strconv.Itoa(count) + "|")
	}
	_ = learn.Close()
	tags, err := g.Raw(`SELECT learning_id, tag FROM learning_tag ORDER BY learning_id, tag`).Rows()
	if err != nil {
		t.Fatalf("tag fold: %v", err)
	}
	for tags.Next() {
		var id int64
		var tag string
		if err := tags.Scan(&id, &tag); err != nil {
			t.Fatalf("tag fold scan: %v", err)
		}
		b.WriteString("T" + strconv.FormatInt(id, 10) + "~" + tag + "|")
	}
	_ = tags.Close()
	props, err := g.Raw(`SELECT id, kind, payload, status, created_by_kind, created_by_id
		FROM proposal ORDER BY id`).Rows()
	if err != nil {
		t.Fatalf("proposal fold: %v", err)
	}
	for props.Next() {
		var id int64
		var kind, payload, status, byKind, byID string
		if err := props.Scan(&id, &kind, &payload, &status, &byKind, &byID); err != nil {
			t.Fatalf("proposal fold scan: %v", err)
		}
		b.WriteString("P" + strconv.FormatInt(id, 10) + "~" + kind + "~" + payload + "~" + status + "~" + byKind + "~" + byID + "|")
	}
	_ = props.Close()
	if err := learn.Err(); err != nil {
		t.Fatalf("learn rows: %v", err)
	}
	return b.String()
}

// seedPreS52Memory writes the pre-slice vintage rows the drill carries
// forward: learnings in all three scopes (the agent row ALREADY slug-keyed
// — a legal key the sweep must not touch), tags riding with them, and a
// proposal of the vintage's own kinds.
func seedPreS52Memory(t *testing.T, s *Store) string {
	t.Helper()
	// The agent row is of the same older vintage as the learnings below, so
	// it is planted raw too: S73's audited door would mint a ledger row this
	// drill has no business carrying, and the vintage predates the table.
	seedAgentRowBeforeS73(t, s, "golang-engineer", "Golang Engineer", "2026-11-01T00:00:00Z")
	// The writes go RAW: the store\'s own CreateLearning reads its newest
	// shape back, and this table is deliberately of the older vintage —
	// seeding through the new code would defeat the drill.
	seed := func(scope, key, body string, tags []string) {
		id, err := s052SeedRaw(t, s, scope, key, body, 2)
		if err != nil {
			t.Fatalf("seed learning %s/%s: %v", scope, key, err)
		}
		for _, tag := range tags {
			if _, err := s.w.Exec(`INSERT INTO learning_tag (learning_id, tag) VALUES (?, ?)`, id, tag); err != nil {
				t.Fatalf("seed tag %s: %v", tag, err)
			}
		}
	}
	seed("agent", "golang-engineer", "the CGO shim header must match go-sqlite3's version", []string{"cgo"})
	seed("project", "/srv/repos/ledger", "the ledger migrations replay best in order", []string{"migrations", "replay"})
	seed("person", "", "never auto-commit on the demo box", []string{})
	if _, err := s.CreateProposal("policy_add", `{"rule":"no trailing whitespace"}`, Actor{Kind: "user", ID: "42"}, "2026-11-01T00:00:01Z"); err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	return memoryFold(t, s.GormDB())
}

func TestS52CloseoutUpgradePreservesMemoryRows(t *testing.T) {
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
	applyMigrations(t, old.GormDB(), Migrations()[:s52LearningManagementMigrationIndex(t)])
	before := seedPreS52Memory(t, old)
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	// Upgrade: the real runner, through the S52 schema AND the LM1 slug
	// step — an already-legal store must pay nothing for it.
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
	if got := memoryFold(t, s.GormDB()); got != before {
		t.Fatalf("the upgrade rewrote memory.\n before: %s\n   after: %s", before, got)
	}
	// Idempotence the daemon pays on every ordinary open: a second run
	// moves nothing (check-first, one SELECT).
	moved, err := s.MigrateLearningAgentScopeKeys()
	if err != nil || moved != 0 {
		t.Fatalf("the sweep was not idempotent: %d moved, %v", moved, err)
	}
}

func TestS52CloseoutPlantedUnresolvedScopeKeyRefusesTheOpen(t *testing.T) {
	dir := t.TempDir()
	old, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("open for seeding: %v", err)
	}
	applyMigrations(t, old.GormDB(), Migrations()[:s52LearningManagementMigrationIndex(t)])
	before := seedPreS52Memory(t, old)
	// The plant: a learning keyed on an agent NAME that answers to no
	// agent, plus a name-keyed row that COULD move — the refusal is
	// all-or-nothing, so neither may shift.
	if _, err := s052SeedRaw(t, old, "agent", "Ghost Nobody", "ghost fact", 1); err != nil {
		t.Fatalf("plant ghost: %v", err)
	}
	if _, err := s052SeedRaw(t, old, "agent", "Golang Engineer", "named, movable", 1); err != nil {
		t.Fatalf("plant movable: %v", err)
	}
	before = memoryFold(t, old.GormDB())
	if err := old.Close(); err != nil {
		t.Fatalf("close after seeding: %v", err)
	}

	// The open REFUSES: memory left keyed on a name is memory the new
	// scope law refuses to find, so the store stays shut, naming the key.
	_, err = Open(Options{Dir: dir})
	if err == nil {
		t.Fatal("the open accepted an unresolvable scope_key — LM1's law is optional now")
	}
	if !strings.Contains(err.Error(), "Ghost Nobody") {
		t.Fatalf("the refusal %q does not name the key it refused on", err)
	}
	// Nothing moved: the operator can still read the old shape raw.
	probe, err := Open(Options{Dir: dir, SkipMigrations: true})
	if err != nil {
		t.Fatalf("reopen raw to check: %v", err)
	}
	if got := memoryFold(t, probe.GormDB()); got != before {
		t.Fatalf("the refused open still moved memory.\n before: %s\n   after: %s", before, got)
	}
	// The operator registers the ghost (the only honest fix — the step
	// never guesses, so only a real agent row can resolve the key).
	if err := probe.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "ghost-fixed", Name: "Ghost Nobody"}, "2026-11-02T00:00:00Z"); err != nil {
		t.Fatalf("register the ghost: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatalf("close after fix: %v", err)
	}

	s, err := Open(Options{Dir: dir})
	if err != nil {
		t.Fatalf("open after the fix: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var key string
	if err := s.r.QueryRow(`SELECT scope_key FROM learning WHERE body = 'ghost fact'`).Scan(&key); err != nil {
		t.Fatalf("read the fixed row: %v", err)
	}
	if key != "ghost-fixed" {
		t.Fatalf("the fixed row kept key %q — the sweep should have moved it to ghost-fixed", key)
	}
	if err := s.r.QueryRow(`SELECT scope_key FROM learning WHERE body = 'named, movable'`).Scan(&key); err != nil {
		t.Fatalf("read the movable row: %v", err)
	}
	if key != "golang-engineer" {
		t.Fatalf("the movable row kept key %q — the name is a registered agent's, the slug law says slugs", key)
	}
}

// s052SeedRaw inserts one pre-slice learning through the raw writer so the
// drill can plant keys NewLearningRow's callers would never write.
func s052SeedRaw(t *testing.T, s *Store, scope, key, body string, observed int) (int64, error) {
	t.Helper()
	res, err := s.w.Exec(`INSERT INTO learning (scope, scope_key, body, provenance, observed_count, created_at)
		VALUES (?, ?, ?, 'observed', ?, '2026-11-01T12:00:00Z')`, scope, key, body, observed)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return id, err
}
