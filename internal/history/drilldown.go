package history

import (
	"strconv"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// The drill-down (D2/H2): what the merge deliberately does NOT carry.
//
// The merge (T1) is a time-ordered sequence of HEADERS — an entry is a loop's
// facts and its floored cost, nothing more. V15 (S22's embedding rule) does
// not apply to reading (H2): it is about what gets embedded, not what gets
// read, so a transcript is read when a person asks for it, never pre-fetched.
// Q3's answer: an archived loop's content is a SECOND, explicit fetch — the
// mirror of ArchiveLoop's own "read content before the transaction, decode as
// little as possible" discipline. The two doors below are the two fetches.
//
// NOTE on the live read: the per-loop transcript window (`ReadLoopTranscriptPage`,
// S42) is foreign WIP on another branch and is NOT at HEAD. This package stays
// self-contained on HEAD and reads the live transcript through the at-HEAD read
// the S9 views already issue — `ReadTranscript`, per step — called from this
// package rather than duplicated. It is the SAME read S9 issues, not a second
// implementation of it (the gate TestLiveDrilldownReadsStepMessageDirectly
// pins that this function calls `ReadTranscript`, and that an archived loop is
// refused by name rather than read as an empty transcript).

// LiveTranscript reads a live loop's transcript directly — the at-HEAD read
// S9's views issue (`ReadTranscript`, per step), called from this package
// rather than duplicated (D2/H2). It walks the loop's steps in seq order and
// concatenates their messages oldest-first, so a multi-step loop comes back as
// one continuous transcript the way a person would read it.
//
// It is refused for a non-live entry: an archived loop's step_message is gone
// from the live tables (archived = deleted, archive.go), so its transcript is
// the archived drill-down's, never this one's. A live loop asked of the
// archived door, or an archived loop asked of the live door, is a wrong
// fetch, not an empty one.
func LiveTranscript(s *store.Store, e Entry) ([]model.Message, error) {
	if e.Kind != KindLive {
		return nil, &WrongDoor{What: "live transcript", Entry: e}
	}
	steps, err := s.ListSteps(store.Scope{LoopSlug: e.Slug, ComponentRoot: e.ComponentRoot})
	if err != nil {
		return nil, err
	}
	var all []model.Message
	for _, st := range steps {
		// The same read S9 issues; not a second query.
		tr := s.ReadTranscript(st.ID)
		if !tr.OK {
			return nil, &ReadError{Step: st.ID, Reason: tr.Reason()}
		}
		all = append(all, tr.Messages...)
	}
	return all, nil
}

// ArchivedTranscript reads an archived loop's content ON DEMAND — a separate
// call (Q3), never bundled into the merge. The merge's entries carry only the
// Archive header (ListArchives reads no loop_archive_file); this call reads
// loop_archive_file exactly once, via ReadArchive with content. Header first,
// content on demand: the list never pulls a file a person never opened.
//
// It is refused for a non-archived entry: a live loop's content is its
// live transcript's (LiveTranscript), never the cold-stored archive.
func ArchivedTranscript(s *store.Store, e Entry) (store.Archive, []store.ArchiveFile, error) {
	if e.Kind != KindArchived {
		return store.Archive{}, nil, &WrongDoor{What: "archived content", Entry: e}
	}
	return s.ReadArchive(e.ID, true)
}

// WrongDoor is a refused drill-down that names the door it refused and the
// entry it was asked of (DB19): an entry asked of the wrong drill-down is a
// wrong fetch, not an empty one, and a bare empty result would read as "the
// loop has no transcript" — a lie for a loop whose content is simply in the
// other table.
type WrongDoor struct {
	What  string
	Entry Entry
}

func (e *WrongDoor) Error() string {
	return "history: " + e.What + " is a " + e.Entry.Kind.String() + " loop; read it through its own drill-down"
}

// ReadError is a live read that failed for a real reason (DB19) — the read
// refused, not the transcript empty — so a failed replay reads as a failure,
// never as "the loop has no transcript."
type ReadError struct {
	Step   int64
	Reason string
}

func (e *ReadError) Error() string {
	return "history: read the live transcript of step " + strconv.FormatInt(e.Step, 10) + ": " + e.Reason
}
