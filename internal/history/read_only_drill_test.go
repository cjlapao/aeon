package history

// S53 T5 close-out — the P9 drills for a slice with NO migration. The design
// (tasks/T5.md) replaces the migration drill with two proofs:
//
//  1. THE READ-ONLY DRILL: the store bytes are unchanged after EVERY history
//     call — a hash before/after, per table, re-checked after each call
//     individually so a late call cannot hide behind an earlier one. History
//     is a derived, read-only merge (HV1: it adds no table and writes
//     nothing); this drill is that claim at the byte level — including the
//     calls that READ CONTENT (the archived drill-down's inflate) and the
//     call that FAILS (the rotten archive: a read that cannot read must not
//     "fix" anything on its way out).
//  2. THE COVERAGE DIFFERENTIAL on a store with a PLANTED undecodable
//     archive: coverage's states must equal independently counted
//     reality — live = the live table, shown-archived = the WHOLE archive
//     table (T1's ruling: coverage never hides the row — the header shows
//     even when the content cannot be read), cannot-say = exactly what was
//     planted, named.
//
// The fingerprint is s56's recipe (`tableFingerprint`, measured here rather
// than trusted there): (count, sha256 over every row rendered as
// quote(col)‖… in table_info order, rows by rowid). Every regular table is
// hashed — not just the ones history is expected to touch — so a stray write
// ANYWHERE in the store is caught; FTS/vec virtual tables and their shadows
// are skipped (index, not data, and not rowid-addressable), and the skip
// rule is asserted to have skipped only names carrying those substrings, so
// no real table can hide behind it.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
	"gorm.io/gorm"
)

// drillSeed lays the richest history fixture the drills run over: a live
// loop with two steps (known + unknown cost) and a transcript, a blocked
// loop with its stored reason, a document, one sound archive WITH content
// files, and one rotten archive (T1's corruption recipe) — the same shape
// the coverage tests use, so the drills run on the DEMO's store, not a
// thinner one.
func drillSeed(t *testing.T) (*store.Store, string) {
	t.Helper()
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	_, sc := seedLoop(t, s, "/proj", "live-loop")
	step := seedStep(t, s, sc, store.NewStep{Phase: "build", Agent: "gpt"}, costKnown)
	seedStep(t, s, sc, store.NewStep{Phase: "test", Agent: "gpt"}, costUnknown)
	if err := s.AppendMessages(step.ID, []model.Message{
		{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: "kick the walk"}}},
	}); err != nil {
		t.Fatalf("append transcript: %v", err)
	}
	blk, _ := seedLoop(t, s, "/proj", "was-blocked")
	if _, err := s.AppendEvent(store.NewEvent{
		LoopSlug: blk.Slug, ComponentRoot: "/proj", Kind: "loop.blocked",
		Payload: map[string]any{"reason": "the gate answer had no rework target"},
	}); err != nil {
		t.Fatalf("append blocked event: %v", err)
	}
	if err := s.SetLoopStatus(store.Scope{LoopSlug: "was-blocked", ComponentRoot: "/proj"}, "blocked"); err != nil {
		t.Fatalf("block: %v", err)
	}
	for _, slug := range []string{"sound-archive", "rotten-archive"} {
		seedLoop(t, s, "/proj", slug)
		if _, err := s.WriteDocument(store.Scope{LoopSlug: slug, ComponentRoot: "/proj"}, "brief.md", "# the brief", "cli"); err != nil {
			t.Fatalf("write document: %v", err)
		}
		if _, err := s.ArchiveLoop(store.Scope{LoopSlug: slug, ComponentRoot: "/proj"}, "cold"); err != nil {
			t.Fatalf("archive %s: %v", slug, err)
		}
	}
	var rottenID string
	if err := s.GormDB().Raw(`SELECT id FROM loop_archive WHERE slug = 'rotten-archive'`).Scan(&rottenID).Error; err != nil || rottenID == "" {
		t.Fatalf("find the rotten archive: %q %v", rottenID, err)
	}
	if err := s.GormDB().Exec(`UPDATE loop_archive_file SET content = ? WHERE archive_id = ?`,
		[]byte{0x00, 0x01, 0x02}, rottenID).Error; err != nil {
		t.Fatalf("plant the rot: %v", err)
	}
	return s, rottenID
}

// storeFingerprint hashes EVERY regular table of the store (see the file
// header). The result maps table name → "count:hash", plus the sorted table
// NAME LIST travels with it: a table created or dropped by a read call is
// caught even before its rows are compared.
func storeFingerprint(t *testing.T, s *store.Store) map[string]string {
	t.Helper()
	var names []string
	rows, err := s.GormDB().Raw(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`).Rows()
	if err != nil {
		t.Fatalf("enumerate tables: %v", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		if strings.HasPrefix(n, "sqlite_") {
			continue // schema bookkeeping
		}
		if strings.Contains(n, "_fts") || strings.Contains(n, "_vec") {
			continue // FTS5/vec0 indexes and their shadows: index bytes, not data, not rowid-addressable
		}
		names = append(names, n)
	}
	rows.Close()
	out := map[string]string{}
	for _, n := range names {
		out[n] = tableDigest(t, s.GormDB(), n)
	}
	// The skip rule may skip nothing but index machinery: assert the kept set
	// still holds every table history READS, so no data table hid behind it.
	for _, must := range []string{"loop", "step", "step_message", "loop_archive", "loop_archive_file", "loop_document", "component", "policy_event"} {
		if _, ok := out[must]; !ok {
			t.Fatalf("the fingerprint skipped real data table %q — the skip rule is eating tables it must not", must)
		}
	}
	return out
}

// tableDigest is s56's recipe, measured here: count + sha256 over every row
// rendered as quote(col) joined in table_info column order, rows by rowid.
func tableDigest(t *testing.T, g *gorm.DB, table string) string {
	t.Helper()
	var cols []string
	{
		rows, err := g.Raw(fmt.Sprintf(`SELECT name FROM pragma_table_info(%q) ORDER BY cid`, table)).Rows()
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan column of %s: %v", table, err)
			}
			cols = append(cols, c)
		}
		rows.Close()
	}
	if len(cols) == 0 {
		t.Fatalf("table %s has no columns", table)
	}
	rendered := make([]string, len(cols))
	for i, c := range cols {
		rendered[i] = fmt.Sprintf("quote(%q)", c)
	}
	h := sha256.New()
	n := 0
	rows, err := g.Raw(fmt.Sprintf(`SELECT %s FROM %q ORDER BY rowid`, strings.Join(rendered, " || char(31) || "), table)).Rows()
	if err != nil {
		t.Fatalf("fingerprint %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("read row of %s: %v", table, err)
		}
		fmt.Fprintf(h, "%s\n", line)
		n++
	}
	return fmt.Sprintf("%d:%s", n, hex.EncodeToString(h.Sum(nil)))
}

func assertUnchanged(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	var diffs []string
	for n, want := range before {
		if got, ok := after[n]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: the table VANISHED under a read", n))
		} else if got != want {
			diffs = append(diffs, fmt.Sprintf("%s: %s → %s", n, want[:12], got[:12]))
		}
	}
	for n := range after {
		if _, ok := before[n]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: a READ CREATED A TABLE", n))
		}
	}
	if len(diffs) != 0 {
		sort.Strings(diffs)
		t.Errorf("after %s the store was not the same store: %s", label, strings.Join(diffs, "; "))
	}
}

// TestHistoryReadsTouchNothing is the read-only drill: every history call —
// the merge, every coverage lane, every describe, the content-drill-down,
// and the drill-down that FAILS — runs with the store fingerprint checked
// after EACH one. History holds no write; this is the byte-level proof, and
// it includes the failure paths (a read that cannot read must leave nothing
// behind, least of all a "repair").
func TestHistoryReadsTouchNothing(t *testing.T) {
	s, rottenID := drillSeed(t)
	drillSeed := storeFingerprint(t, s)
	opts := Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}
	check := func(label string) { assertUnchanged(t, label, drillSeed, storeFingerprint(t, s)) }

	entries, err := Merge(s, opts)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("fixture: %d entries, want 4", len(entries))
	}
	check("Merge")

	for _, by := range []GroupBy{ByLoop, ByPhase, ByTask, ByAgent} {
		if _, err := Coverage(s, opts, by); err != nil {
			t.Fatalf("coverage %s: %v", by, err)
		}
		check(fmt.Sprintf("Coverage(%s)", by))
	}
	for _, e := range entries {
		_ = Describe(e) // pure over the entry — pure, but the drill proves rather than asserts it
	}
	check("Describe over every entry")
	_ = Vocabulary()
	check("Vocabulary")

	// The drill-down, both ways: the sound archive inflating its files, and
	// the rotten one failing. ReadArchive is what history.show's archived
	// leg runs (protocol's handler calls it with content).
	var soundID string
	if err := s.GormDB().Raw(`SELECT id FROM loop_archive WHERE slug = 'sound-archive'`).Scan(&soundID).Error; err != nil || soundID == "" {
		t.Fatalf("find the sound archive: %q %v", soundID, err)
	}
	_, files, err := s.ReadArchive(soundID, true)
	if err != nil || len(files) == 0 {
		t.Fatalf("the sound archive's content read failed: %v (%d files)", err, len(files))
	}
	check("ReadArchive(sound, content)")
	if _, _, err := s.ReadArchive(rottenID, true); err == nil {
		t.Fatal("the rotten archive decoded — the drill's fixture rotted away, and with it the failure path")
	}
	check("ReadArchive(rotten, failing)")

	// The unscoped span too — the every-component path Merge takes with no
	// scope, whose reads walk the same tables from the other side.
	if _, err := Merge(s, Options{}); err != nil {
		t.Fatalf("unscoped merge: %v", err)
	}
	check("Merge(every component)")
}

// TestCoverageDifferentialWithPlantedRot is close-out drill 2: on the same
// rich store, coverage's three states vs. reality counted INDEPENDENTLY of
// the history package — plain SQL over the tables. The planted id is the
// cannot-say entry and only it; shown equals archive-total minus planted.
func TestCoverageDifferentialWithPlantedRot(t *testing.T) {
	s, rottenID := drillSeed(t)
	opts := Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}

	var liveSQL, archivesSQL int
	if err := s.GormDB().Raw(`SELECT COUNT(*) FROM loop WHERE component_root = '/proj'`).Scan(&liveSQL).Error; err != nil {
		t.Fatalf("count live: %v", err)
	}
	if err := s.GormDB().Raw(`SELECT COUNT(*) FROM loop_archive WHERE component_root = '/proj'`).Scan(&archivesSQL).Error; err != nil {
		t.Fatalf("count archives: %v", err)
	}
	view, err := Coverage(s, opts, ByLoop)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if view.Live.Loops != liveSQL {
		t.Errorf("live: coverage %d, raw table %d", view.Live.Loops, liveSQL)
	}
	if view.Archived.Loops != archivesSQL {
		t.Errorf("shown %d ≠ the archive table's %d — T1's ruling: coverage reports the differential and never hides the row; the header shows even when the content cannot be read",
			view.Archived.Loops, archivesSQL)
	}
	if view.Undecodable.Count != 1 || len(view.Undecodable.IDs) != 1 || view.Undecodable.IDs[0] != rottenID {
		t.Errorf("cannot-say = %+v, want exactly the planted %s", view.Undecodable, rottenID)
	}
	// The loop lane shows everything it holds: no reason rows on this lane;
	// the reason rows belong to the live-step lanes, where they must appear.
	if len(view.NotShown) != 0 {
		t.Errorf("the loop lane grew reason rows: %v", view.NotShown)
	}
	if v2, err := Coverage(s, opts, ByTask); err != nil {
		t.Fatal(err)
	} else if len(v2.NotShown) != 1 || v2.NotShown[0].Reason != NotShownArchivedSteps || v2.NotShown[0].Count != view.Archived.Loops {
		t.Errorf("the task lane's not-shown row = %+v, want one %q row for %d", v2.NotShown, NotShownArchivedSteps, view.Archived.Loops)
	}
}
