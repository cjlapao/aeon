package protocol

// ════════════════════════════════════════════════════════════════════════════
// The history reads (S25 T4): the app's window onto a project's history.
//
// # What this file is, and what it refuses to be
//
// Three read-only methods — list, show, export — exist **for the app only**
// (P34), mirroring policy.*'s four (S10 T13). They read the SAME
// internal/history the CLI reads (aeon history, T3): the ONE query
// (history.Query) and the ONE row shape (history.Row) live in internal/history,
// so the socket and the CLI cannot drift — H3 is structural here, not a test
// that could rot. This slice has nothing to author: every method is a read,
// there is no CG9-equivalent write door, and none of the three writes an audit
// row (there is nothing to audit — a read is not a refusal that happened).
//
// # The store seam (the four-package boundary)
//
// The handlers hold a *history.Store, not a *store.Store — the same seam
// policy.Store keeps (S10 T13). A *store.Store import would carry the
// persistence layer into the protocol, the leaf S19 mounts on HTTP (TestTheFour
// PackagesCannotSeeEachOther); the facade decouples it, so the protocol imports
// history, not store. The daemon wires the facade over opts.Store.
//
// # Zero writes (the gate)
//
// None of the three touches the recorder. history.* read internal/history over
// the store and return; they do not record a policy_event-style row, because a
// read is not an event. The gate pins this (no audit row anywhere).
//
// # Refusals travel verbatim
//
// A *WrongDoor from the drill-down (a live entry asked of the archived door,
// or vice versa) travels across the wire as a named refusal, never read as
// empty (DB19) — an archived loop read as empty would lie, a live one asked of
// the archive door would lie. A store-absence is the framework's own code, not
// a dereference; a not-found id is the protocol's own refusal by name.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/model"
)

// requireHistoryStore answers the one wiring question every history read faces
// first: does this daemon have a store at all. Nil is the storeless shape, and
// every handler says so with the framework's own store-absence code rather than
// dereferencing (memory.go's requireMemory idiom, carried here).
func (t *Table) requireHistoryStore() (*history.Store, error) {
	if t.HistoryStore == nil {
		return nil, ErrStoreUnavailable
	}
	return t.HistoryStore, nil
}

// --- history.list ----------------------------------------------------------

// HistoryListParams is the one question history.list answers: which component
// and (optionally) which grouping and lower time bound. Empty fields span every
// component and group by loop, exactly the CLI's flag defaults.
type HistoryListParams struct {
	ProjectRoot string `json:"projectRoot,omitempty"`
	GroupBy     string `json:"groupBy,omitempty"`
	Since       string `json:"since,omitempty"`
}

// HistoryListResult is history.list's answer: the rows, each the shared shape
// (H3), and a statement only when the set is empty (P31's honest empty — "no
// history" is a fact, not a gap, and never reads as "the store is broken").
type HistoryListResult struct {
	Rows      []history.HistoryRow `json:"rows"`
	Statement *string              `json:"statement,omitempty"`
}

// AnswerHistoryList is the ONE list answer (rule 4, S53 T3): the person's
// history.list door and the step's history_list tool twin both run THIS
// function, so the two doors cannot drift field for field. The handler below
// is its caller behind the seam check; the daemon's records arm calls it with
// the same facade the Table holds.
func AnswerHistoryList(s *history.Store, p HistoryListParams) (HistoryListResult, error) {
	entries, err := s.List(p.ProjectRoot, p.Since, history.GroupBy(p.GroupBy))
	if err != nil {
		return HistoryListResult{}, err
	}
	rows := make([]history.HistoryRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, history.Row(e))
	}
	res := HistoryListResult{Rows: rows}
	if len(rows) == 0 {
		statement := "no history — no live or archived loops in scope"
		res.Statement = &statement
	}
	return res, nil
}

func (t *Table) handleHistoryList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	s, err := t.requireHistoryStore()
	if err != nil {
		return nil, err
	}
	var p HistoryListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return AnswerHistoryList(s, p)
}

// --- history.export --------------------------------------------------------

// history.export is the SAME query as history.list (H3): the only difference is
// the rows are returned for NDJSON emission, not a table. It shares the facade's
// List, so the two cannot produce different rows — that is the whole of the gate.
func (t *Table) handleHistoryExport(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	s, err := t.requireHistoryStore()
	if err != nil {
		return nil, err
	}
	var p HistoryListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	entries, err := s.List(p.ProjectRoot, p.Since, history.GroupBy(p.GroupBy))
	if err != nil {
		return nil, err
	}
	rows := make([]history.HistoryRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, history.Row(e))
	}
	return HistoryListResult{Rows: rows}, nil
}

// --- history.show (the drill-down, S25 T2) ---------------------------------

// HistoryShowParams asks for ONE entry by id (a loop or archive id, or its
// slug) and, optionally, the scope to find it in. The drill-down reads the
// entry's own door — a live loop's transcript, an archived loop's cold-stored
// content.
type HistoryShowParams struct {
	ID          string `json:"id"`
	ProjectRoot string `json:"projectRoot,omitempty"`
}

// HistoryShowResult is the drill-down's answer: the entry's kind, and — for the
// right door — its content. A live loop's transcript is the message sequence;
// an archived loop's content is its cold-stored files (Q3). Refused is a
// refusal that travels verbatim, never an empty read (DB19).
type HistoryShowResult struct {
	Kind       string                `json:"kind"`
	ID         string                `json:"id"`
	Transcript []model.Message       `json:"transcript,omitempty"`
	Files      []history.ArchiveFile `json:"files,omitempty"`
	Refused    *string               `json:"refused,omitempty"`
}

// AnswerHistoryShow is the ONE drill-down answer (rule 4, S53 T3): the door's
// words travel whole — an empty id and an unknown id are both refused by name
// (DB19), a wrong-door entry comes back as the verbatim refusal, never an
// empty read. Both the person's door and the step's history_show twin run it.
func AnswerHistoryShow(s *history.Store, p HistoryShowParams) (HistoryShowResult, error) {
	if p.ID == "" {
		return HistoryShowResult{}, ErrHistoryRefused.WithWhat("history.show needs an `id` — the drill-down is by one entry")
	}
	// The facade finds the entry in the shared query (so show reads the same
	// facts list does, never a second query) and reads it through its own door.
	res, err := s.Show(p.ProjectRoot, p.ID)
	if err != nil {
		return HistoryShowResult{}, err
	}
	if !res.Found {
		// DB19: name the id that was not found, rather than reading it as empty.
		return HistoryShowResult{}, ErrHistoryRefused.WithWhat("no loop or archive with id %q in scope", p.ID)
	}
	out := HistoryShowResult{Kind: res.Entry.Kind.String(), ID: res.Entry.ID}
	if res.Refused != nil {
		refused := res.Refused.What
		out.Refused = &refused
		return out, nil
	}
	if res.Transcript != nil {
		out.Transcript = res.Transcript
	}
	if res.Files != nil {
		out.Files = res.Files
	}
	return out, nil
}

func (t *Table) handleHistoryShow(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	s, err := t.requireHistoryStore()
	if err != nil {
		return nil, err
	}
	var p HistoryShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return AnswerHistoryShow(s, p)
}

// --- history.coverage (the honesty read, S53 T1 / HV1) ----------------------

// HistoryCoverageParams asks what the history view SHOWS, does NOT SHOW and
// CANNOT SAY for a scope. Scope keys (HV1): projectRoot (CT15's wire name for a
// root component, carried from history.list) and componentRoot (the same scope
// under the tree's own name). BOTH name the same one scope — a request naming
// both is refused by name (DB19), never one silently preferred; neither is the
// every-component span, exactly history.list's reading. groupBy/since are the
// same filter words history.list takes, so coverage can state what a LANE
// leaves out (the archived-from-step-lanes notShown, S25 Q3).
type HistoryCoverageParams struct {
	ProjectRoot   string `json:"projectRoot,omitempty"`
	ComponentRoot string `json:"componentRoot,omitempty"`
	GroupBy       string `json:"groupBy,omitempty"`
	Since         string `json:"since,omitempty"`
}

// HistoryCoverageResult is coverage's answer verbatim: history.CoverageView —
// the shown counts, the cannot-say archives, the not-shown statement and the
// P32 floor sentence, computed by the same internal/history code the query
// runs (HV1: no second merge).
type HistoryCoverageResult = history.CoverageView

// AnswerHistoryCoverage is the ONE coverage answer (rule 4, S53 T4): the
// person's door, the CLI's socket arm and the CLI's offline arm all run THIS
// function, so the both-roots refusal (DB19) is structural rather than
// copied — a client that names both scopes hears the same sentence from
// whichever door answered.
func AnswerHistoryCoverage(s *history.Store, p HistoryCoverageParams) (history.CoverageView, error) {
	if p.ProjectRoot != "" && p.ComponentRoot != "" {
		return history.CoverageView{}, ErrHistoryRefused.WithWhat("projectRoot and componentRoot name the same scope — set one, not both")
	}
	root := p.ProjectRoot
	if root == "" {
		root = p.ComponentRoot
	}
	return s.Coverage(root, p.Since, history.GroupBy(p.GroupBy))
}

func (t *Table) handleHistoryCoverage(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	s, err := t.requireHistoryStore()
	if err != nil {
		return nil, err
	}
	var p HistoryCoverageParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	return AnswerHistoryCoverage(s, p)
}

// --- history.vocabulary (the closed sets, S53 T1 / HV2) ---------------------

// handleHistoryVocabulary answers history.vocabulary: the closed lanes, outcome
// words and filter keys, derived from internal/history's constants (HV2) — the
// client renders words it is served and types none (acceptance criterion 2).
// It rides the same seam as its siblings: an unwired store answers
// ErrStoreUnavailable, because a vocabulary for a daemon that shows no history
// would advertise a surface that is not there.
func (t *Table) handleHistoryVocabulary(ctx context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if _, err := t.requireHistoryStore(); err != nil {
		return nil, err
	}
	return history.Vocabulary(), nil
}
