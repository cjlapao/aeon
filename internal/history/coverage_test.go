package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/store"
)

// TestCoverageEqualsTheRealQuery is HV1's differential: coverage and the real
// query are run over the SAME fixture store and the numbers must agree —
// live.loops and archived.loops equal the Merge walk's per-kind counts, and
// each side's oldest/newest equal the extremes computed from the same shown
// entries. A coverage that counts its own way — a second merge, a second SQL —
// eventually disagrees with what the list shows, and the honesty read becomes
// the thing that lies.
func TestCoverageEqualsTheRealQuery(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	seedLoop(t, s, "/proj", "one")
	seedLoop(t, s, "/proj", "two")
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "one", ComponentRoot: "/proj"}, "done"); err != nil {
		t.Fatalf("archive one: %v", err)
	}

	opts := Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}
	entries, err := Merge(s, opts)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	view, err := Coverage(s, opts, ByLoop)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}

	var live, archived int
	liveOld, liveNew, arcOld, arcNew := "", "", "", ""
	for _, e := range entries {
		if e.Kind == KindArchived {
			archived++
			if arcOld == "" || e.Timeline < arcOld {
				arcOld = e.Timeline
			}
			if arcNew == "" || e.Timeline > arcNew {
				arcNew = e.Timeline
			}
			continue
		}
		live++
		if liveOld == "" || e.Timeline < liveOld {
			liveOld = e.Timeline
		}
		if liveNew == "" || e.Timeline > liveNew {
			liveNew = e.Timeline
		}
	}

	if view.Live.Loops != live || view.Archived.Loops != archived {
		t.Errorf("coverage counts (%d live / %d archived) disagree with the real query (%d / %d)",
			view.Live.Loops, view.Archived.Loops, live, archived)
	}
	if view.Live.Oldest != liveOld || view.Live.Newest != liveNew {
		t.Errorf("the live span coverage states (%s..%s) disagrees with the shown sequence (%s..%s)",
			view.Live.Oldest, view.Live.Newest, liveOld, liveNew)
	}
	if view.Archived.Oldest != arcOld || view.Archived.Newest != arcNew {
		t.Errorf("the archived span coverage states (%s..%s) disagrees with the shown sequence (%s..%s)",
			view.Archived.Oldest, view.Archived.Newest, arcOld, arcNew)
	}
	if view.Undecodable.Count != 0 || len(view.Undecodable.IDs) != 0 {
		t.Errorf("a healthy store reported %d undecodable archives — a probe that cries wolf is worse than no probe", view.Undecodable.Count)
	}
	if view.Scope != "/proj" {
		t.Errorf("coverage echoed scope %q, not the scope asked", view.Scope)
	}
	if len(view.NotShown) != 0 {
		t.Errorf("the loop lane reported notShown %v — the merge shows everything it holds", view.NotShown)
	}

	// The other three fixture shapes the differential must walk: a scope with
	// live only, and an empty scope (P31's honest empty — zeros, empty spans,
	// and never a gap dressed as a number).
	seedComponent(t, s, "/only-live")
	seedLoop(t, s, "/only-live", "just-live")
	liveOnly, err := Coverage(s, Options{Scope: store.ComponentScope{ComponentRoot: "/only-live"}}, ByLoop)
	if err != nil {
		t.Fatalf("live-only coverage: %v", err)
	}
	if liveOnly.Live.Loops != 1 || liveOnly.Archived.Loops != 0 || liveOnly.Archived.Oldest != "" || liveOnly.Archived.Newest != "" {
		t.Errorf("live-only scope coverage = %+v — an absent side is zeros and empty spans, never a gap", liveOnly)
	}
	empty, err := Coverage(s, Options{Scope: store.ComponentScope{ComponentRoot: "/nothing-here"}}, ByLoop)
	if err != nil {
		t.Fatalf("empty coverage: %v", err)
	}
	if empty.Live.Loops != 0 || empty.Archived.Loops != 0 || empty.Undecodable.Count != 0 || len(empty.Undecodable.IDs) != 0 ||
		empty.Live.Oldest != "" || empty.Archived.Newest != "" {
		t.Errorf("an empty scope answered %+v — P31's honest empty is zeros, not a fake span", empty)
	}
}

// TestUndecodableIsCannotSayNeverSilentlyAbsent pins P31's third state (HV1):
// an archive whose blob will not decode is NEITHER shown-fine NOR absent — it
// is named. Before coverage the corruption was invisible until drill-down; the
// count names it and the ids list it (≤ UndecodableIDsListed), and the header
// still stands in the list — coverage reports the differential, it never hides
// the row or invents its content.
func TestUndecodableIsCannotSayNeverSilentlyAbsent(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	seedLoop(t, s, "/proj", "rotten")
	seedLoop(t, s, "/proj", "sound")
	// A document is what makes an archive carry files at all: ArchiveLoop
	// takes the loop's documents with it (and deletes them live).
	if _, err := s.WriteDocument(store.Scope{LoopSlug: "rotten", ComponentRoot: "/proj"}, "brief.md", "# the brief", "cli"); err != nil {
		t.Fatalf("write a document: %v", err)
	}
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "rotten", ComponentRoot: "/proj"}, "cold"); err != nil {
		t.Fatalf("archive rotten: %v", err)
	}
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "sound", ComponentRoot: "/proj"}, "cold"); err != nil {
		t.Fatalf("archive sound: %v", err)
	}

	// Corrupt exactly one archive's stored blob at the raw level the merge
	// never touches — the LIST stays intact (ListArchives reads headers only),
	// the drill-down's ungzip of the document will die.
	var rottenID string
	if err := s.GormDB().Raw(`SELECT id FROM loop_archive WHERE slug = 'rotten'`).Scan(&rottenID).Error; err != nil {
		t.Fatalf("find the rotten archive id: %v", err)
	}
	if rottenID == "" {
		t.Fatal("the archived loop is not in loop_archive — the fixture broke")
	}
	if err := s.GormDB().Exec(`UPDATE loop_archive_file SET content = ? WHERE archive_id = ?`,
		[]byte{0x00, 0x01, 0x02}, rottenID).Error; err != nil {
		t.Fatalf("corrupt the blob: %v", err)
	}

	view, err := Coverage(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}, ByLoop)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if view.Undecodable.Count != 1 {
		t.Fatalf("undecodable count = %d, want 1 — one blob rotted", view.Undecodable.Count)
	}
	if len(view.Undecodable.IDs) != 1 || view.Undecodable.IDs[0] != rottenID {
		t.Errorf("undecodable ids = %v, want [%s] — the cannot-say state must NAME what it cannot say", view.Undecodable.IDs, rottenID)
	}
	if view.Archived.Loops != 2 {
		t.Errorf("the rotten archive's header stopped showing (archived.loops = %d) — coverage reports the differential, it never hides the row", view.Archived.Loops)
	}
}

// TestUndecodableIDsAreCappedNotTheCount: the ids list carries at most
// UndecodableIDsListed entries while the count stays the truth — a view that
// buried a hundred ids under a count would say the honest number and hide the
// honest name; capping the LIST (never the count) is the honest middle.
func TestUndecodableIDsAreCappedNotTheCount(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	const n = UndecodableIDsListed + 3
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		slug := string(rune('a'+i%26)) + string(rune('a'+i/26)) + "loop"
		seedLoop(t, s, "/proj", slug)
		if _, err := s.WriteDocument(store.Scope{LoopSlug: slug, ComponentRoot: "/proj"}, "brief.md", "# brief", "cli"); err != nil {
			t.Fatalf("write a document for %s: %v", slug, err)
		}
		if _, err := s.ArchiveLoop(store.Scope{LoopSlug: slug, ComponentRoot: "/proj"}, "cold"); err != nil {
			t.Fatalf("archive %s: %v", slug, err)
		}
		var id string
		if err := s.GormDB().Raw(`SELECT id FROM loop_archive WHERE slug = ?`, slug).Scan(&id).Error; err != nil {
			t.Fatalf("find archive id for %s: %v", slug, err)
		}
		if id == "" {
			t.Fatalf("no archive row for %s", slug)
		}
		res := s.GormDB().Exec(`UPDATE loop_archive_file SET content = ? WHERE archive_id = ?`, []byte{0xFF}, id)
		if res.Error != nil {
			t.Fatalf("corrupt %s: %v", slug, res.Error)
		}
		if res.RowsAffected == 0 {
			t.Fatalf("corrupt %s matched no file rows — the archive carries no files", slug)
		}
		if _, _, err := s.ReadArchive(id, true); err == nil {
			t.Fatalf("the probe itself does not fail for %s — the corruption did not reach the decode path", slug)
		}
		ids = append(ids, id)
	}
	view, err := Coverage(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}, ByLoop)
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if view.Undecodable.Count != n {
		t.Errorf("undecodable count = %d, want %d — the count is never capped", view.Undecodable.Count, n)
	}
	if len(view.Undecodable.IDs) != UndecodableIDsListed {
		t.Errorf("undecodable ids listed %d, want the cap %d", len(view.Undecodable.IDs), UndecodableIDsListed)
	}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	for _, id := range view.Undecodable.IDs {
		if !known[id] {
			t.Errorf("coverage named id %q, which never rotted", id)
		}
	}
}

// TestCoverageNotShownNamesWhatTheLaneLeavesOut: on a step lane the archived
// loops are honestly NOT SHOWN (S25 Q3 — their steps live cold and the lanes
// regroup live steps only); coverage says so with the reason and the count,
// while the loop lane reports none.
func TestCoverageNotShownNamesWhatTheLaneLeavesOut(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	seedLoop(t, s, "/proj", "steppy")
	seedLoop(t, s, "/proj", "gone")
	if _, err := s.ArchiveLoop(store.Scope{LoopSlug: "gone", ComponentRoot: "/proj"}, "cold"); err != nil {
		t.Fatalf("archive gone: %v", err)
	}

	opts := Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}}
	loopLane, err := Coverage(s, opts, ByLoop)
	if err != nil {
		t.Fatalf("loop-lane coverage: %v", err)
	}
	if len(loopLane.NotShown) != 0 {
		t.Errorf("the loop lane reported %v — it shows every loop it holds", loopLane.NotShown)
	}
	phaseLane, err := Coverage(s, opts, ByPhase)
	if err != nil {
		t.Fatalf("phase-lane coverage: %v", err)
	}
	if len(phaseLane.NotShown) != 1 || phaseLane.NotShown[0].Count != 1 {
		t.Fatalf("the phase lane must name the 1 archived loop it leaves out, got %v", phaseLane.NotShown)
	}
	if phaseLane.NotShown[0].Reason != NotShownArchivedSteps {
		t.Errorf("notShown reason is a re-phrasing (%q), not the slice's own words", phaseLane.NotShown[0].Reason)
	}
}
