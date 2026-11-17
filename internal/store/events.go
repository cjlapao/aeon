package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// `event`: the sequence, and the gap S2 admitted.
//
// # What this closes
//
// S2 emitted events with an in-memory counter that reset on every daemon
// restart. That was correct for a slice with no store, and S2 did the right
// thing: it said so in a doc comment on the counter and said so in its demo,
// naming this slice as the one that would fix it.
//
// Here `seq` is the `event` table's rowid (DB17): one global monotonic
// sequence, durable across a restart, so a reconnecting client's cursor answers
// "did I miss anything" across a daemon UPGRADE rather than only across a
// dropped connection.
//
// Measured, because it is the one promise this slice makes to another slice:
// AUTOINCREMENT never reuses an id — after a close, a reopen AND a delete of
// every row, the next id was 4 where three had been used.
//
// # Only the store allocates seq (DB20)
//
// Two allocators produce duplicates or regressions, and a cursor handed a
// duplicate silently delivers the same event twice. Asserted by a grep: no
// package outside this one writes `event.id`.
//
// # Append-only
//
// No update, no delete. An event is what happened; a record of what happened
// that can be edited is not a record. Asserted by the same grep.

// Event is one row of the sequence.
type Event struct {
	// Seq is the rowid. The field is named for what it MEANS to a client
	// rather than for the column it comes from, because `seq` is the name in
	// the protocol and in every cursor.
	Seq int64 `json:"seq"`

	// LoopSlug and ComponentRoot are empty for a daemon-lifecycle event, which
	// belongs to no loop and no component.
	LoopSlug      string `json:"loopSlug,omitempty"`
	ComponentRoot string `json:"componentRoot,omitempty"`

	// LoopID and StepID address the event inside the journal: set for every event that names a loop or a step, so
	// one loop's history is one indexed read (ReadLoopEvents). Empty/zero for events that name neither.
	LoopID string `json:"loopId,omitempty"`
	StepID int64  `json:"stepId,omitempty"`

	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt string          `json:"at"`
}

// NewEvent is what a caller appends.
type NewEvent struct {
	LoopSlug      string
	ComponentRoot string
	// LoopID and StepID, when the event names them (see Event).
	LoopID  string
	StepID  int64
	Kind    string
	Payload any
}

// EventFilter narrows a read.
//
// It takes PRIMITIVES rather than the protocol's Filter type, because
// `internal/store` imports `internal/model` and nothing else under
// `internal/` (I1). The adaptation from the protocol's filter happens in
// `internal/daemon`, which already knows both — so neither leaf has to import
// the other.
type EventFilter struct {
	// Kinds matches exactly, or by a single trailing `*` wildcard (`loop.*`),
	// which is the protocol's own shape. Empty means every kind.
	Kinds []string

	// ComponentRoot narrows to one component. Empty means every component —
	// including the daemon's own events, which have none.
	ComponentRoot string
}

// AppendEvent appends an event and returns the allocated seq.
//
// The seq comes from `last_insert_rowid()` via LastInsertId, which is safe
// precisely because the writer pool is at one connection (Q7): the id belongs
// to the statement just executed on that connection, and there is no other
// connection to interleave with it.
func (s *Store) AppendEvent(in NewEvent) (Event, error) {
	if in.Kind == "" {
		return Event{}, fmt.Errorf("store: an event needs a kind")
	}

	// `payload` is NOT NULL, and a missing payload is stored as the empty JSON
	// object rather than as NULL: every reader then parses one shape, and a
	// client written against "payload is an object" never meets a null.
	payload := []byte("{}")
	if in.Payload != nil {
		encoded, err := json.Marshal(in.Payload)
		if err != nil {
			return Event{}, fmt.Errorf("store: encode the payload of a %s event: %w", in.Kind, err)
		}
		payload = encoded
	}

	now := s.Now()
	res, err := s.w.Exec(`
		INSERT INTO event (loop_slug, component_root, kind, payload, created_at, loop_id, step_id)
		VALUES (?,?,?,?,?,?,?)`,
		nullable(in.LoopSlug), nullable(in.ComponentRoot), in.Kind, string(payload), now, nullable(in.LoopID), nullableID(in.StepID))
	if err != nil {
		return Event{}, fmt.Errorf("store: append a %s event: %w", in.Kind, err)
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return Event{}, fmt.Errorf("store: read the seq of the new %s event: %w", in.Kind, err)
	}

	s.applySummary(in.LoopID, in.StepID, in.Kind, payload, seq)

	return Event{
		Seq:           seq,
		LoopSlug:      in.LoopSlug,
		ComponentRoot: in.ComponentRoot,
		LoopID:        in.LoopID,
		StepID:        in.StepID,
		Kind:          in.Kind,
		Payload:       payload,
		CreatedAt:     now,
	}, nil
}

// ReadEvents is the cursor's implementation: everything after `after` matching
// the filter, in seq order, bounded by `limit`.
//
// # Gaps are expected and are not an error
//
// A subscription filtered to one component receives ids with gaps in them, which
// is correct — the cursor's contract is "everything after N that matches my
// filter". A client that compares consecutive seq values and infers loss is
// reading the wrong thing, which is why the protocol's `from` reports where a
// subscription actually starts.
func (s *Store) ReadEvents(after int64, filter EventFilter, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultEventLimit
	}

	query := `SELECT id, COALESCE(loop_slug,''), COALESCE(component_root,''), kind, payload, created_at,
		       COALESCE(loop_id,''), COALESCE(step_id,0)
		FROM event WHERE id > ?`
	args := []any{after}

	if filter.ComponentRoot != "" {
		query += ` AND component_root = ?`
		args = append(args, filter.ComponentRoot)
	}
	if clause, kindArgs := kindPredicate(filter.Kinds); clause != "" {
		query += ` AND ` + clause
		args = append(args, kindArgs...)
	}
	query += ` ORDER BY id LIMIT ?`
	args = append(args, limit)

	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read events after %d: %w", after, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var ev Event
		var payload string
		if err := rows.Scan(&ev.Seq, &ev.LoopSlug, &ev.ComponentRoot, &ev.Kind, &payload, &ev.CreatedAt, &ev.LoopID, &ev.StepID); err != nil {
			return nil, fmt.Errorf("store: read events after %d: %w", after, err)
		}
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// DefaultEventLimit bounds an unbounded read.
//
// A guess. It exists so a client that asks for "everything after 0" on a store
// with a year of history gets a page rather than the whole table — and so the
// caller finds out early that it has to page, rather than at the point where it
// has too much history to load.
const DefaultEventLimit = 1000

// HeadSeq is the highest allocated seq, or 0 before anything is appended.
//
// This is what makes `seq` survive a restart: the daemon asks the TABLE where
// it is rather than starting a counter at zero.
func (s *Store) HeadSeq() (int64, error) {
	// COALESCE over MAX rather than sqlite_sequence: MAX reads the table, which
	// is the thing a cursor is compared against. sqlite_sequence holds the
	// highest id ever ALLOCATED, which is larger after a delete — and handing a
	// client a head it will never receive an event at makes `--after head`
	// silently empty.
	var head int64
	if err := s.r.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM event`).Scan(&head); err != nil {
		return 0, fmt.Errorf("store: read the head of the event sequence: %w", err)
	}
	return head, nil
}

// kindPredicate builds the kind filter, honouring one trailing wildcard.
//
// `loop.*` becomes a LIKE with the prefix escaped, and an exact kind stays an
// equality — so a kind containing a `%` cannot turn into a wildcard by
// accident, which is the injection-shaped version of this mistake.
func kindPredicate(kinds []string) (string, []any) {
	if len(kinds) == 0 {
		return "", nil
	}
	var clauses []string
	var args []any
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if k == "" || k == "*" {
			// A bare `*` means everything, so the whole predicate collapses.
			return "", nil
		}
		if prefix, ok := strings.CutSuffix(k, "*"); ok {
			clauses = append(clauses, `kind LIKE ? ESCAPE '\'`)
			args = append(args, escapeLike(prefix)+"%")
			continue
		}
		clauses = append(clauses, `kind = ?`)
		args = append(args, k)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	return strings.ReplaceAll(s, `_`, `\_`)
}

// --- memory ------------------------------------------------------------------

// Memory is one harvested note.
type Memory struct {
	ID            int64  `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	At            string `json:"at"`
	Agent         string `json:"agent,omitempty"`
	TaskID        string `json:"taskid,omitempty"`
	Note          string `json:"note"`
}

// AppendMemory records a note against a loop.
func (s *Store) AppendMemory(sc Scope, agent, taskID, note string) (Memory, error) {
	if !sc.Valid() {
		return Memory{}, fmt.Errorf("store: AppendMemory needs both a slug and a component root, got %s", sc)
	}
	if strings.TrimSpace(note) == "" {
		return Memory{}, fmt.Errorf("store: a memory needs a note")
	}
	now := s.Now()
	res, err := s.w.Exec(`
		INSERT INTO memory (loop_slug, component_root, ts, agent, taskid, note)
		VALUES (?,?,?,?,?,?)`,
		sc.LoopSlug, sc.ComponentRoot, now, nullable(agent), nullable(taskID), note)
	if err != nil {
		return Memory{}, fmt.Errorf("store: append a memory to %s: %w", sc, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Memory{}, fmt.Errorf("store: read the id of the new memory in %s: %w", sc, err)
	}
	return Memory{ID: id, LoopSlug: sc.LoopSlug, ComponentRoot: sc.ComponentRoot,
		At: now, Agent: agent, TaskID: taskID, Note: note}, nil
}

// ReadMemory reads a loop's notes, oldest first.
func (s *Store) ReadMemory(sc Scope) ([]Memory, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`
		SELECT id, loop_slug, component_root, ts, COALESCE(agent,''), COALESCE(taskid,''), note
		FROM memory WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read the memory of %s: %w", sc, err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.LoopSlug, &m.ComponentRoot, &m.At, &m.Agent, &m.TaskID, &m.Note); err != nil {
			return nil, fmt.Errorf("store: read the memory of %s: %w", sc, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoopEventsWindow is one page of a loop's journal, oldest first.
type LoopEventsWindow struct {
	Events []Event
	// HasOlder and HasNewer say whether the journal holds events beyond this window in each direction, so a page
	// knows whether scrolling can load more without asking and being told "nothing".
	HasOlder bool
	HasNewer bool
}

// DefaultLoopEventLimit and MaxLoopEventLimit bound a window. A caller asking for more than the maximum is
// clamped, not refused: the window size carries no security weight.
const (
	DefaultLoopEventLimit = 300
	MaxLoopEventLimit     = 2000
)

// ReadLoopEvents reads one loop's journal as a window. Exactly one of before and after may be set (0 = unset); with
// neither it returns the LATEST window, which is what opening a loop needs. The cursor is the event id itself,
// the same seq every subscription uses, so a window and the live tail join with no gap and no overlap: subscribe
// from the window's last seq.
//
// Kinds filters like ReadEvents (exact or one trailing `*`). `excludeKinds` removes kinds the caller does not want
// as history, which is how the streamed text chunks stay out of a page of history that already holds the finished
// turn.
func (s *Store) ReadLoopEvents(loopID string, before, after int64, limit int, kinds, excludeKinds []string) (LoopEventsWindow, error) {
	return s.ReadStepEvents(loopID, 0, before, after, limit, kinds, excludeKinds)
}

// ReadStepEvents is ReadLoopEvents narrowed to one step of the loop (0 means every step): what a step's own page
// needs to show its calls without reading the rest of the loop's journal.
func (s *Store) ReadStepEvents(loopID string, stepID, before, after int64, limit int, kinds, excludeKinds []string) (LoopEventsWindow, error) {
	if loopID == "" {
		return LoopEventsWindow{}, fmt.Errorf("store: ReadLoopEvents needs a loop id")
	}
	if before > 0 && after > 0 {
		return LoopEventsWindow{}, fmt.Errorf("store: ReadLoopEvents takes before OR after, never both")
	}
	if limit <= 0 {
		limit = DefaultLoopEventLimit
	}
	if limit > MaxLoopEventLimit {
		limit = MaxLoopEventLimit
	}
	where := `loop_id = ?`
	args := []any{loopID}
	if stepID > 0 {
		where += ` AND step_id = ?`
		args = append(args, stepID)
	}
	if clause, kindArgs := kindPredicate(kinds); clause != "" {
		where += ` AND ` + clause
		args = append(args, kindArgs...)
	}
	for _, kind := range excludeKinds {
		clause, kindArgs := kindPredicate([]string{kind})
		if clause == "" {
			continue
		}
		where += ` AND NOT (` + clause + `)`
		args = append(args, kindArgs...)
	}
	order, cursor := "DESC", ""
	switch {
	case after > 0:
		order, cursor = "ASC", ` AND id > ?`
		args = append(args, after)
	case before > 0:
		cursor = ` AND id < ?`
		args = append(args, before)
	}
	// One extra row says whether more exists in the direction of travel.
	rows, err := s.r.Query(`SELECT id, COALESCE(loop_slug,''), COALESCE(component_root,''), kind, payload, created_at,
		       COALESCE(loop_id,''), COALESCE(step_id,0)
		FROM event WHERE `+where+cursor+` ORDER BY id `+order+` LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return LoopEventsWindow{}, fmt.Errorf("store: read the events of loop %s: %w", loopID, err)
	}
	defer rows.Close()
	var got []Event
	for rows.Next() {
		var ev Event
		var payload string
		if err := rows.Scan(&ev.Seq, &ev.LoopSlug, &ev.ComponentRoot, &ev.Kind, &payload, &ev.CreatedAt, &ev.LoopID, &ev.StepID); err != nil {
			return LoopEventsWindow{}, fmt.Errorf("store: read the events of loop %s: %w", loopID, err)
		}
		ev.Payload = json.RawMessage(payload)
		got = append(got, ev)
	}
	if err := rows.Err(); err != nil {
		return LoopEventsWindow{}, err
	}
	more := len(got) > limit
	if more {
		got = got[:limit]
	}
	if order == "DESC" { // latest/before: read newest-first, served oldest-first
		for i, j := 0, len(got)-1; i < j; i, j = i+1, j-1 {
			got[i], got[j] = got[j], got[i]
		}
	}
	win := LoopEventsWindow{Events: got}
	if order == "ASC" {
		win.HasNewer = more
		win.HasOlder = after > 0
	} else {
		win.HasOlder = more
		win.HasNewer = before > 0
	}
	return win, nil
}
