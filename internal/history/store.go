package history

import (
	"errors"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// Store is the read-only facade the protocol's history.* handlers hold (S25
// T4), mirroring policy.Store: it owns the *store.Store and exposes the reads
// through history types only, so the protocol package need not import store
// (the four-package boundary — the protocol is a leaf S19 mounts on HTTP, and a
// store import would carry the persistence layer into that leaf, exactly the
// policy.Store seam the socket already keeps). The CLI keeps calling the free
// functions over *store.Store directly; this facade is the socket's door.
type Store struct {
	s *store.Store
}

// NewStore wraps a *store.Store as the facade the daemon wires onto the
// protocol's Table.HistoryStore.
func NewStore(s *store.Store) *Store { return &Store{s: s} }

// List is the read the protocol's history.list and history.export call (H3).
// root is a component root ("" spans every component), since an RFC3339 lower
// bound, by the grouping — folded into Options here so the protocol never
// builds a store.ComponentScope, which would pull store into the protocol.
func (st *Store) List(root, since string, by GroupBy) ([]Entry, error) {
	return Query(st.s, Options{Scope: store.ComponentScope{ComponentRoot: root}, Since: since}, by)
}

// Coverage is the read the protocol's history.coverage calls (S53 T1, HV1):
// the same facade door as List, over the same merge code — the protocol never
// builds a store.ComponentScope and never sees *store.Store.
func (st *Store) Coverage(root, since string, by GroupBy) (CoverageView, error) {
	return Coverage(st.s, Options{Scope: store.ComponentScope{ComponentRoot: root}, Since: since}, by)
}

// ArchiveFile is a cold-stored file the drill-down returns (Q3), decoupled from
// store.ArchiveFile so the protocol need not import store. The CLI reads the
// same content through store.ArchiveFile; the two carry the same fields, so
// the socket and the CLI render the same file by the same name.
type ArchiveFile struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified,omitempty"`
	Content  string `json:"content,omitempty"`
}

// ShowResult is the drill-down's answer (T2): the entry, its transcript (a live
// loop) or its cold-stored files (an archived loop), and — for a wrong door —
// the refusal that travels verbatim (DB19). Found is false when the id names no
// in-scope entry, which the protocol renders as its own not-found refusal
// (never an empty read).
type ShowResult struct {
	Entry      Entry
	Found      bool
	Transcript []model.Message
	Files      []ArchiveFile
	Refused    *WrongDoor
}

// Show is the drill-down (T2): it finds the entry by id (id OR slug) in the
// shared query (so show reads the same facts list does, never a second query —
// H3) and reads it through its own door — a live loop's transcript, an archived
// loop's cold-stored content. A wrong door is a named refusal, never an empty
// read (DB19); a not-found id is Found:false, which the protocol turns into its
// own refusal by name.
func (st *Store) Show(root, id string) (ShowResult, error) {
	entries, err := st.List(root, "", "")
	if err != nil {
		return ShowResult{}, err
	}
	var found *Entry
	for i := range entries {
		if entries[i].ID == id || entries[i].Slug == id {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		return ShowResult{Found: false}, nil
	}
	res := ShowResult{Entry: *found, Found: true}
	if found.Kind == KindLive {
		msgs, err := LiveTranscript(st.s, *found)
		if err != nil {
			if wd, ok := asWrongDoor(err); ok {
				res.Refused = wd
				return res, nil
			}
			return ShowResult{}, err
		}
		res.Transcript = msgs
		return res, nil
	}
	_, files, err := ArchivedTranscript(st.s, *found)
	if err != nil {
		if wd, ok := asWrongDoor(err); ok {
			res.Refused = wd
			return res, nil
		}
		return ShowResult{}, err
	}
	res.Files = make([]ArchiveFile, len(files))
	for i, f := range files {
		res.Files[i] = ArchiveFile{Path: f.Path, Bytes: f.Bytes, SHA256: f.SHA256, Modified: f.Modified, Content: f.Content}
	}
	return res, nil
}

// asWrongDoor extracts a *WrongDoor from a drill-down error so the refusal
// travels verbatim (DB19) rather than as a generic failure.
func asWrongDoor(err error) (*WrongDoor, bool) {
	var wd *WrongDoor
	if errors.As(err, &wd) {
		return wd, true
	}
	return nil, false
}
