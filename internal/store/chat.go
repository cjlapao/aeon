package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// `question` and `chat_message`: the pair.
//
// v1 could say THAT a loop was blocked — `awaiting_user`, a flag and a reason
// — and had nowhere to hold the conversation that unblocks it.
//
// # Two tables, because they fail differently (DB7)
//
// `question` is the suspension MECHANISM: `tool_use_id`, `options`,
// `answered_at`, read by the runtime to resume. `chat_message` is the
// human-facing THREAD: it holds steering messages and notes that answer
// nothing, and it is what the app renders in order. They join on `question_id`.
//
// # The transactional pairing is the property that matters
//
// `Ask` writes both rows or neither:
//
//	a question with NO THREAD ENTRY  → a loop waiting on something the user
//	                                   cannot see
//	a thread entry with NO QUESTION  → a prompt nobody can answer
//
// Both are silent. Neither errors. So the test forces a failure mid-`Ask` and
// asserts neither row exists.
//
// # There is no `awaiting_user` column (DB6)
//
// Whether a loop is waiting is an EXISTENCE QUERY over unanswered questions. A
// stored flag and an unanswered question can disagree, and when they do nothing
// says which is right — so the disagreement is made unrepresentable rather than
// detected.

// QuestionKind is the four-valued kind, with a CHECK behind it.
type QuestionKind string

const (
	// QuestionClarify — the model asking the person something it needs.
	QuestionClarify QuestionKind = "clarify"
	// QuestionGate — a phase gate, asked by the loop driver with no model turn
	// behind it. These have NO tool_use_id.
	QuestionGate QuestionKind = "gate"
	// QuestionPermission — an unsafe tool asking to proceed. Always carries the
	// tool_use_id of the call it is about.
	QuestionPermission QuestionKind = "permission"
	// QuestionFree — free-form, no options.
	QuestionFree QuestionKind = "free"
)

// QuestionKinds is every kind, for the CHECK's test and the CLI.
func QuestionKinds() []QuestionKind {
	return []QuestionKind{QuestionClarify, QuestionGate, QuestionPermission, QuestionFree}
}

// Question is a question's row.
type Question struct {
	ID            int64  `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	StepID        *int64 `json:"stepId,omitempty"`

	// ToolUseID is empty for a gate and set for a permission prompt.
	//
	// Nullable in the column ON PURPOSE: a permission prompt or a clarify
	// question comes from a tool call and has one; a phase gate is asked by the
	// loop driver and does not. A NOT NULL column would force the driver to
	// fabricate an id — and a fabricated id is one some resume path will
	// eventually try to match.
	ToolUseID string `json:"toolUseId,omitempty"`

	Kind    string   `json:"kind"`
	Body    string   `json:"body"`
	Options []string `json:"options,omitempty"`

	Answer     string `json:"answer,omitempty"`
	AskedAt    string `json:"askedAt"`
	AnsweredAt string `json:"answeredAt,omitempty"`
}

// Answered reports whether this question has been answered.
//
// Derived from the timestamp, not from a separate flag — one fact, one place.
func (q Question) Answered() bool { return q.AnsweredAt != "" }

// NewQuestion is what a caller supplies to ask one.
type NewQuestion struct {
	StepID    *int64
	ToolUseID string
	Kind      QuestionKind
	Body      string
	Options   []string

	// ThreadBody is what the person reads in the thread, when it should differ
	// from Body. Empty means the thread entry carries Body — which is the
	// common case and the reason this is optional rather than required.
	ThreadBody string
}

// ChatMessage is one entry in the thread.
type ChatMessage struct {
	ID            int64  `json:"id"`
	LoopSlug      string `json:"loopSlug"`
	ComponentRoot string `json:"componentRoot"`
	Seq           int64  `json:"seq"`
	Author        string `json:"author"`
	Kind          string `json:"kind"`
	Body          string `json:"body"`
	QuestionID    *int64 `json:"questionId,omitempty"`
	CreatedAt     string `json:"createdAt"`
	// Source is where the message came from (S39 CR6): user | template |
	// automation | blueprint — or "" when NOBODY typed it (agent, system,
	// legacy rows). The empty string is the third state (P31): it means
	// "no source was recorded", never "user".
	Source string `json:"source,omitempty"`
	// SourceRef names the specific author behind a templated source — for
	// S40 kickoffs, the event trigger's id. omitempty: rows that predate
	// the column, and every row without one, project byte-identically.
	SourceRef string `json:"sourceRef,omitempty"`
	// Skills are the skill NAMES the operator chose for this turn (S68,
	// B6) — the receipt of what rode beside their words. nil is the third
	// state (P31): nothing was chosen (or the row predates S68), never an
	// empty guess. The daemon renders each name to its canonical
	// instruction line at send; the row stays names, so a replay and a
	// fresh run read the same choice from the same row.
	Skills []string `json:"skills,omitempty"`

	// Attachments is the S69 receipt — references into the frozen
	// @attachments/ document revisions, never bytes (AT2). Empty means
	// nothing was attached; the daemon expands each pinned revision to
	// content blocks at send time only.
	Attachments []AttachmentReceipt `json:"attachments,omitempty"`
}

// ValidSource is the closed vocabulary (CR6): the four words plus "" for
// the rows nobody typed. A fifth value is refused at append, and the
// migration's CHECK refuses it in the database — the Go set mirrors the SQL
// set and both are the whole truth (the DB19 discipline at the door).
func ValidSource(s string) bool {
	switch s {
	case "", "user", "template", "automation", "blueprint":
		return true
	}
	return false
}

// NewChatMessage is what a caller supplies to append a note or a steer.
type NewChatMessage struct {
	Author string
	Kind   string
	Body   string
	// Source labels where a HUMAN-authored message came from (S39 CR6):
	// "" defaults to user for a human author and stays NULL for the rest;
	// template/automation/blueprint arrive from a kickoff render, a
	// schedule, or the engine itself.
	Source string
	// SourceRef names WHO wrote a templated kickoff — for S40, the event
	// trigger's id (T1 added the column; the plan's AU9 asks which trigger
	// spoke these words). NULL is honest absence: only Source=automation/
	// template rows ever carry one, and nobody invents one.
	SourceRef string
	// Attachments are the S69 receipts filed by the message door.
	Attachments []AttachmentReceipt

	// Skills are the skill NAMES chosen for this turn (S68, B6). Empty
	// means nothing chosen and stores NULL; the names are already resolved
	// against the live catalog by the daemon door, which refused anything
	// it could not find.
	Skills []string
}

// encodeSkills is the ONE writer of the skills column: a chosen list becomes
// its JSON array, nothing chosen becomes NULL — never "[]" standing in for
// absence (the two read differently forever, P31).
func encodeSkills(names []string) any {
	if len(names) == 0 {
		return nil
	}
	raw, err := json.Marshal(names)
	if err != nil {
		// []string cannot fail to marshal; a non-nil error here is a Go bug,
		// and silently storing NULL would hide it — so it panics loudly.
		panic("store: encoding chosen skills: " + err.Error())
	}
	return string(raw)
}

// decodeSkills reads the column back. Absence (NULL, or the empty string a
// COALESCE would produce) is nil — nothing chosen. Corrupt bytes are an
// ERROR, not a silent empty list: a receipt that cannot be read must say so
// rather than let the operator's chosen skills vanish quietly.
func decodeSkills(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, fmt.Errorf("store: the chosen-skills receipt %q is not a JSON array: %w", raw.String, err)
	}
	return out, nil
}

// encodeAttachments / decodeAttachments are the ONE writer and reader of the
// attachments column (S69 AT2), sharing the skills codec's honesty: absent
// means NULL (nothing attached), corrupt is an error naming the row's value,
// never a silent empty.
func encodeAttachments(rs []AttachmentReceipt) any {
	if len(rs) == 0 {
		return nil
	}
	raw, err := json.Marshal(rs)
	if err != nil {
		panic("store: encoding attachment receipts: " + err.Error())
	}
	return string(raw)
}

func decodeAttachments(raw sql.NullString) ([]AttachmentReceipt, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var out []AttachmentReceipt
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return nil, fmt.Errorf("store: the attachment receipt %q is not a JSON array: %w", raw.String, err)
	}
	return out, nil
}

// AnswerResult is the outcome of answering.
//
// A result, not an error (DB19): "already answered" is a domain refusal with
// something useful to say, and "the disk is full" is not.
type AnswerResult struct {
	OK bool

	// Question is the row as it now stands, whether the answer landed or was
	// refused — a caller refused because it was already answered needs to see
	// the answer that is there.
	Question Question

	// Reason names the existing timestamp when the refusal is a double answer.
	Reason string
}

// Ask writes the question and its thread entry in ONE transaction (D16).
func (s *Store) Ask(sc Scope, q NewQuestion) (Question, error) {
	if !sc.Valid() {
		return Question{}, fmt.Errorf("store: Ask needs both a slug and a component root, got %s", sc)
	}
	if strings.TrimSpace(q.Body) == "" {
		return Question{}, fmt.Errorf("store: a question needs a body")
	}
	kind := q.Kind
	if kind == "" {
		kind = QuestionFree
	}

	var options any
	if len(q.Options) > 0 {
		encoded, err := json.Marshal(q.Options)
		if err != nil {
			return Question{}, fmt.Errorf("store: encode the options of a question: %w", err)
		}
		options = string(encoded)
	}

	tx, err := s.w.Begin()
	if err != nil {
		return Question{}, fmt.Errorf("store: ask in %s: %w", sc, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := s.Now()
	res, err := tx.Exec(`
		INSERT INTO question (loop_slug, component_root, step_id, tool_use_id, kind, body, options, asked_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		sc.LoopSlug, sc.ComponentRoot, q.StepID, nullable(q.ToolUseID), string(kind), q.Body, options, now)
	if err != nil {
		return Question{}, fmt.Errorf("store: ask in %s: %w", sc, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Question{}, fmt.Errorf("store: read the id of the new question in %s: %w", sc, err)
	}

	body := q.ThreadBody
	if body == "" {
		body = q.Body
	}
	if _, err := appendThread(tx, s.Now(), sc, NewChatMessage{
		Author: "agent", Kind: "question", Body: body,
	}, &id); err != nil {
		return Question{}, fmt.Errorf("store: write the thread entry for the question in %s: %w", sc, err)
	}

	if err := tx.Commit(); err != nil {
		return Question{}, fmt.Errorf("store: commit the question in %s: %w", sc, err)
	}
	return s.ReadQuestion(id)
}

const questionColumns = `id, loop_slug, component_root, step_id, COALESCE(tool_use_id,''),
	kind, body, COALESCE(options,''), COALESCE(answer,''), asked_at, COALESCE(answered_at,'')`

func scanQuestion(row interface{ Scan(...any) error }) (Question, error) {
	var q Question
	var options string
	if err := row.Scan(&q.ID, &q.LoopSlug, &q.ComponentRoot, &q.StepID, &q.ToolUseID,
		&q.Kind, &q.Body, &options, &q.Answer, &q.AskedAt, &q.AnsweredAt); err != nil {
		return Question{}, err
	}
	if options != "" {
		if err := json.Unmarshal([]byte(options), &q.Options); err != nil {
			return Question{}, fmt.Errorf("the options of question %d are not a JSON array: %w", q.ID, err)
		}
	}
	return q, nil
}

// ReadQuestion reads one question by id.
func (s *Store) ReadQuestion(id int64) (Question, error) {
	row := s.r.QueryRow(`SELECT `+questionColumns+` FROM question WHERE id = ?`, id)
	q, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, fmt.Errorf("store: question %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Question{}, fmt.Errorf("store: read question %d: %w", id, err)
	}
	return q, nil
}

// Answer records an answer and appends its thread entry, in one transaction.
//
// # Answering twice is a race, not an edit
//
// The first answer may already have resumed the step, so a second is REFUSED
// rather than applied — and the refusal names the existing timestamp, because
// "already answered" without saying when leaves the caller unable to tell a
// double submit from a stale window.
//
// An overwrite would leave the resumed step acting on a value the person
// believes they replaced.
func (s *Store) Answer(id int64, text string) (AnswerResult, error) {
	existing, err := s.ReadQuestion(id)
	if err != nil {
		return AnswerResult{}, err
	}
	if existing.Answered() {
		return AnswerResult{
			OK:       false,
			Question: existing,
			Reason: fmt.Sprintf("question %d was already answered at %s; a second answer is refused "+
				"because the first may already have resumed the step", id, existing.AnsweredAt),
		}, nil
	}

	tx, err := s.w.Begin()
	if err != nil {
		return AnswerResult{}, fmt.Errorf("store: answer question %d: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := s.Now()
	// The WHERE clause carries `answered_at IS NULL`, so two concurrent answers
	// cannot both land even though the read above happened outside the
	// transaction. The check above is for the MESSAGE; this is the guarantee.
	res, err := tx.Exec(`UPDATE question SET answer = ?, answered_at = ? WHERE id = ? AND answered_at IS NULL`,
		text, now, id)
	if err != nil {
		return AnswerResult{}, fmt.Errorf("store: answer question %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return AnswerResult{}, fmt.Errorf("store: answer question %d: %w", id, err)
	}
	if n == 0 {
		// Lost the race. Re-read, so the refusal names the timestamp that won.
		current, readErr := s.ReadQuestion(id)
		if readErr != nil {
			return AnswerResult{}, readErr
		}
		return AnswerResult{
			OK:       false,
			Question: current,
			Reason: fmt.Sprintf("question %d was answered at %s by something else while this answer was "+
				"being written; it is refused rather than applied", id, current.AnsweredAt),
		}, nil
	}

	sc := Scope{LoopSlug: existing.LoopSlug, ComponentRoot: existing.ComponentRoot}
	if _, err := appendThread(tx, now, sc, NewChatMessage{
		Author: "human", Kind: "answer", Body: text,
	}, &id); err != nil {
		return AnswerResult{}, fmt.Errorf("store: write the thread entry for the answer to %d: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return AnswerResult{}, fmt.Errorf("store: commit the answer to question %d: %w", id, err)
	}

	answered, err := s.ReadQuestion(id)
	if err != nil {
		return AnswerResult{}, err
	}
	return AnswerResult{OK: true, Question: answered}, nil
}

// PendingQuestion returns the single OLDEST unanswered question.
//
// Oldest, not newest: questions are answered in the order asked, and a
// newest-first reader strands the first one indefinitely — which looks like a
// loop that is stuck for no reason.
func (s *Store) PendingQuestion(sc Scope) (Question, bool, error) {
	where, args := sc.Where("")
	row := s.r.QueryRow(`SELECT `+questionColumns+` FROM question
		WHERE `+where+` AND answered_at IS NULL ORDER BY id LIMIT 1`, args...)
	q, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, fmt.Errorf("store: read the pending question of %s: %w", sc, err)
	}
	return q, true, nil
}

// StepQuestions returns every question of one step, answered and pending, in the
// order asked (S76): a chat reopened mid-conversation shows the whole run of
// questions as one block, with the answers already given, and answers the rest.
func (s *Store) StepQuestions(stepID int64) ([]Question, error) {
	rows, err := s.r.Query(`SELECT `+questionColumns+` FROM question WHERE step_id = ? ORDER BY id`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: questions of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// LoopQuestions returns every question a loop asked, answered and pending, in
// the order asked: the replay of a finished chat draws each with its answer.
func (s *Store) LoopQuestions(sc Scope) ([]Question, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`SELECT `+questionColumns+` FROM question WHERE `+where+` AND step_id IS NOT NULL ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: questions of %s: %w", sc, err)
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// AllLoopQuestions is every question of a loop, in the order asked: the questions its agents put through the `question`
// tool AND the gates the blueprint asked (a gate has no step). `LoopQuestions` is the steps' alone. It is what the
// loop's answers document and its context index are built from, so the person's decisions at a gate are in them too.
func (s *Store) AllLoopQuestions(sc Scope) ([]Question, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`SELECT `+questionColumns+` FROM question WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: all questions of %s: %w", sc, err)
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// PendingStepQuestion scopes the answer to one step, even when its loop has
// other suspended steps. The oldest pending question wins.
func (s *Store) PendingStepQuestion(stepID int64) (Question, bool, error) {
	row := s.r.QueryRow(`SELECT `+questionColumns+` FROM question
		WHERE step_id = ? AND answered_at IS NULL ORDER BY id LIMIT 1`, stepID)
	q, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, fmt.Errorf("store: pending question for step %d: %w", stepID, err)
	}
	return q, true, nil
}

// LatestStepQuestion lets a caller explain a repeated answer with the first
// answer's timestamp after there is no pending row left.
func (s *Store) LatestStepQuestion(stepID int64) (Question, bool, error) {
	row := s.r.QueryRow(`SELECT `+questionColumns+` FROM question WHERE step_id = ? ORDER BY id DESC LIMIT 1`, stepID)
	q, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, fmt.Errorf("store: latest question for step %d: %w", stepID, err)
	}
	return q, true, nil
}

// AwaitingSQL is the derived query, published as a string.
//
// # Why the SQL itself is public
//
// `aeon db query awaiting` PRINTS it (T11). Anyone arriving from v1 will look
// for an `awaiting_user` column and not find one; showing the query is how they
// learn why it is derived rather than stored (DB6). A second copy of it in the
// CLI would eventually print something the store does not run.
const AwaitingSQL = `SELECT 1 FROM question
WHERE component_root = ? AND loop_slug = ? AND answered_at IS NULL LIMIT 1`

// IsAwaiting is that existence query. There is no column and no cached flag.
func (s *Store) IsAwaiting(sc Scope) (bool, error) {
	var one int
	err := s.r.QueryRow(AwaitingSQL, sc.ComponentRoot, sc.LoopSlug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: ask whether %s is awaiting an answer: %w", sc, err)
	}
	return true, nil
}

// AwaitingLoops lists the loops with an unanswered question, across a component
// scope. For `aeon db query awaiting` and the app.
func (s *Store) AwaitingLoops(ps ComponentScope) ([]Scope, error) {
	where, args := ps.Where("q")
	rows, err := s.r.Query(`
		SELECT DISTINCT q.component_root, q.loop_slug FROM question q
		WHERE `+where+` AND q.answered_at IS NULL
		ORDER BY q.component_root, q.loop_slug`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list the loops awaiting an answer: %w", err)
	}
	defer rows.Close()

	var out []Scope
	for rows.Next() {
		var sc Scope
		if err := rows.Scan(&sc.ComponentRoot, &sc.LoopSlug); err != nil {
			return nil, fmt.Errorf("store: list the loops awaiting an answer: %w", err)
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// Note appends a thread entry that answers nothing — a note, or a steer.
func (s *Store) Note(sc Scope, m NewChatMessage) (ChatMessage, error) {
	if !sc.Valid() {
		return ChatMessage{}, fmt.Errorf("store: Note needs both a slug and a component root, got %s", sc)
	}
	tx, err := s.w.Begin()
	if err != nil {
		return ChatMessage{}, fmt.Errorf("store: note in %s: %w", sc, err)
	}
	defer func() { _ = tx.Rollback() }()

	id, err := appendThread(tx, s.Now(), sc, m, nil)
	if err != nil {
		return ChatMessage{}, fmt.Errorf("store: note in %s: %w", sc, err)
	}
	if err := tx.Commit(); err != nil {
		return ChatMessage{}, fmt.Errorf("store: commit the note in %s: %w", sc, err)
	}
	return s.readChatMessage(id)
}

// Thread reads the thread in seq order, after a cursor.
//
// `after` is a seq, and 0 means from the beginning — the same cursor shape as
// the event stream, so a reader that knows one knows the other.
func (s *Store) Thread(sc Scope, after int64) ([]ChatMessage, error) {
	where, args := sc.Where("")
	args = append(args, after)
	rows, err := s.r.Query(`
		SELECT id, loop_slug, component_root, seq, author, kind, body, question_id, created_at, COALESCE(source,''), COALESCE(source_ref,''), skills, attachments
		FROM chat_message WHERE `+where+` AND seq > ? ORDER BY seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read the thread of %s: %w", sc, err)
	}
	defer rows.Close()

	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var skills, attachments sql.NullString
		if err := rows.Scan(&m.ID, &m.LoopSlug, &m.ComponentRoot, &m.Seq,
			&m.Author, &m.Kind, &m.Body, &m.QuestionID, &m.CreatedAt, &m.Source, &m.SourceRef, &skills, &attachments); err != nil {
			return nil, fmt.Errorf("store: read the thread of %s: %w", sc, err)
		}
		if m.Skills, err = decodeSkills(skills); err != nil {
			return nil, fmt.Errorf("store: read the thread of %s: %w", sc, err)
		}
		if m.Attachments, err = decodeAttachments(attachments); err != nil {
			return nil, fmt.Errorf("store: read the thread of %s: %w", sc, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReadChatMessage reads one thread row by id — S68's replay door: the
// daemon reloads a launch's chosen-skills receipt from the kickoff row the
// run points at. Absence is ErrNotFound, corrupt receipts an error, never a
// silent empty list (P31).
func (s *Store) ReadChatMessage(id int64) (ChatMessage, error) {
	return s.readChatMessage(id)
}

func (s *Store) readChatMessage(id int64) (ChatMessage, error) {
	var m ChatMessage
	var skills, attachments sql.NullString
	err := s.r.QueryRow(`
		SELECT id, loop_slug, component_root, seq, author, kind, body, question_id, created_at, COALESCE(source,''), COALESCE(source_ref,''), skills, attachments
		FROM chat_message WHERE id = ?`, id).
		Scan(&m.ID, &m.LoopSlug, &m.ComponentRoot, &m.Seq, &m.Author, &m.Kind, &m.Body, &m.QuestionID, &m.CreatedAt, &m.Source, &m.SourceRef, &skills, &attachments)
	if errors.Is(err, sql.ErrNoRows) {
		return ChatMessage{}, fmt.Errorf("store: chat message %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return ChatMessage{}, fmt.Errorf("store: read chat message %d: %w", id, err)
	}
	if m.Skills, err = decodeSkills(skills); err != nil {
		return ChatMessage{}, fmt.Errorf("store: read chat message %d: %w", id, err)
	}
	if m.Attachments, err = decodeAttachments(attachments); err != nil {
		return ChatMessage{}, fmt.Errorf("store: read chat message %d: %w", id, err)
	}
	return m, nil
}

// threadSeqRetries bounds the collision retry below.
//
// Four. Two goroutines append here — a runner and the server — so a collision
// is expected occasionally; four consecutive losses on a single-connection
// writer pool would mean something other than contention, and retrying forever
// would turn that into a hang instead of an error.
const threadSeqRetries = 4

// appendThread inserts one thread entry, assigning `seq` (O3).
//
// # The store assigns seq, and what happens under contention
//
// `MAX(seq) + 1` inside the caller's transaction, with the UNIQUE constraint as
// the guarantee rather than the read. Two appenders can compute the same seq;
// the constraint catches the loser, and it retries with a recomputed value,
// bounded. The alternative — trusting the read — is a lost message, which is
// the one outcome a thread must not have.
//
// It takes the transaction rather than the store because both of its callers
// are already inside one: a question and its thread entry are written together
// or not at all.
func appendThread(tx *sql.Tx, now string, sc Scope, m NewChatMessage, questionID *int64) (int64, error) {
	author := orDefault(m.Author, "system")
	kind := orDefault(m.Kind, "note")
	// Source (S39 CR6): an explicit label wins; a human with none was a
	// person typing — 'user' is knowable from the row itself, which is the
	// same honesty rule as the migration's backfill. Everyone else keeps
	// NULL: no source was recorded, and none is invented (P31).
	source := m.Source
	if source == "" && author == "human" {
		source = "user"
	}
	if !ValidSource(source) {
		return 0, fmt.Errorf("store: chat message source %q is not one of user, template, automation, blueprint", source)
	}

	var lastErr error
	for attempt := 0; attempt < threadSeqRetries; attempt++ {
		var next int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM chat_message
			WHERE component_root = ? AND loop_slug = ?`, sc.ComponentRoot, sc.LoopSlug).Scan(&next); err != nil {
			return 0, err
		}
		res, err := tx.Exec(`
			INSERT INTO chat_message (loop_slug, component_root, seq, author, kind, body, question_id, created_at, source, source_ref, skills, attachments)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			sc.LoopSlug, sc.ComponentRoot, next, author, kind, m.Body, questionID, now, nullable(source), nullable(m.SourceRef), encodeSkills(m.Skills), encodeAttachments(m.Attachments))
		if err == nil {
			return res.LastInsertId()
		}
		if !isUniqueViolation(err) {
			return 0, err
		}
		lastErr = err
	}
	return 0, fmt.Errorf("the thread of %s took %d colliding appends in a row, which is contention this "+
		"store should not see: %w", sc, threadSeqRetries, lastErr)
}

// isUniqueViolation recognises the driver's constraint error.
//
// By message, because the alternative is importing the driver's error type into
// this package — and this package names its driver in exactly one place
// (internal/database), which is what keeps a driver change a one-file change.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
