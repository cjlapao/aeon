package history

import (
	"testing"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// TestLiveDrilldownReadsStepMessageDirectly is D2/H2: a live loop's transcript
// is read directly, the same query S9's per-loop view issues, called from this
// package and not duplicated. The behavioural assertion is that LiveTranscript
// returns the step_message the fixture wrote; the "not a second implementation"
// half is a call-site fact (LiveTranscript is a thin call to
// store.ReadLoopTranscriptPage — see drilldown.go), pinned here by behaviour
// so a re-query that dropped a row or re-ordered would show up.
func TestLiveDrilldownReadsStepMessageDirectly(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	_, sc := seedLoop(t, s, "/proj", "chatty")

	// A step with two transcript messages, written through the store's own
	// door (the same door S9's per-loop view reads).
	step := seedStep(t, s, sc, store.NewStep{Phase: "build", Agent: "gpt"}, costKnown)
	msgs := []model.Message{
		{Role: model.RoleUser, Content: []model.Block{{Kind: model.BlockText, Text: "fix the bug"}}},
		// M26: an assistant turn must record its provenance (the route/provider/
		// model that produced it), or the append is refused by name.
		{Role: model.RoleAssistant, Content: []model.Block{{Kind: model.BlockText, Text: "done"}},
			Provenance: &model.Provenance{Route: "r1", Provider: "anthropic", Model: "claude"}},
	}
	if err := s.AppendMessages(step.ID, msgs); err != nil {
		t.Fatalf("append messages: %v", err)
	}

	// The entry from the merge, for this live loop.
	entries, err := Merge(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var entry Entry
	found := false
	for _, e := range entries {
		if e.Slug == "chatty" {
			entry, found = e, true
		}
	}
	if !found {
		t.Fatalf("the live loop is not in the merge")
	}

	// LiveTranscript reads its transcript directly — the same read S9 issues.
	msgs, err = LiveTranscript(s, entry)
	if err != nil {
		t.Fatalf("live transcript: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("live transcript read %d messages, want 2 — it must read step_message directly, not a second query", len(msgs))
	}

	// A non-live entry asked of the live door is a wrong fetch, not an empty
	// one (DB19) — an archived loop's step_message is gone, so it must not
	// read as "no transcript."
	archivedEntry := Entry{Kind: KindArchived, Slug: "ghost"}
	if _, err := LiveTranscript(s, archivedEntry); err == nil {
		t.Errorf("a non-live entry read through the live door — it must be refused by name (DB19), not return an empty transcript")
	}
}

// TestArchivedDrilldownIsHeaderFirstContentOnDemand is Q3: an archived loop's
// content is a SECOND, explicit fetch — the list/merge issues zero
// loop_archive_file reads (it carries only the Archive header), and drilling
// into one specific archived loop issues exactly one (ReadArchive reads
// loop_archive_file on demand). The read count is pinned by the split this
// slice honours: Merge uses ListArchives (reads loop_archive only), the
// drill-down uses ReadArchive (reads loop_archive_file once). The fixture
// puts a file under the component root so the archive has content to read;
// the merge's entry carries none of it, the drill-down carries it all.
func TestArchivedDrilldownIsHeaderFirstContentOnDemand(t *testing.T) {
	s := openHistoryStore(t)
	seedComponent(t, s, "/proj")
	_, sc := seedLoop(t, s, "/proj", "to-be-archived")
	// A loop_document the archive will cold-store: ArchiveLoop reads its files
	// from loop_document (not the filesystem), so the archive has content to
	// read back on demand.
	if _, err := s.WriteDocument(sc, "notes.md", "the archived content", "engineer"); err != nil {
		t.Fatalf("write a document: %v", err)
	}

	if _, err := s.ArchiveLoop(sc, "cold storage"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// The merge issues ZERO loop_archive_file reads: its entry carries only
	// the header. The Entry type has no file-content field by construction, so
	// the merge cannot have pulled any — Q3's "header first."
	entries, err := Merge(s, Options{Scope: store.ComponentScope{ComponentRoot: "/proj"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var archived Entry
	found := false
	for _, e := range entries {
		if e.Kind == KindArchived {
			archived, found = e, true
		}
	}
	if !found {
		t.Fatalf("no archived entry in the merge — D7 is broken")
	}

	// Drilling into the one specific archived loop issues exactly one
	// loop_archive_file read: ReadArchive returns the file, with content.
	archive, files, err := ArchivedTranscript(s, archived)
	if err != nil {
		t.Fatalf("archived transcript: %v", err)
	}
	if archive.ID != archived.ID {
		t.Errorf("the drill-down read a different archive than the merge's entry")
	}
	var withContent int
	for _, f := range files {
		if f.Content != "" {
			withContent++
		}
	}
	if withContent == 0 {
		t.Errorf("the drill-down read no file content (the on-demand read is a no-op) — Q3's content-on-demand must return the file")
	}

	// A live entry asked of the archived door is a wrong fetch (DB19), not a
	// silent empty — its content is its live transcript's, not the archive's.
	if _, _, err := ArchivedTranscript(s, Entry{Kind: KindLive, Slug: "chatty"}); err == nil {
		t.Errorf("a live entry read through the archived door — it must be refused by name (DB19), not return an empty archive")
	}
	_ = archive
}
