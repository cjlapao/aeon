package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
)

// `step_message`: transcript fidelity.
//
// # This file's worst failure happens three slices away
//
// A signed `thinking` block that survives storage ALMOST intact — a trimmed
// trailing newline, a re-marshal that reorders keys, a text column that
// normalises something — produces a replay Anthropic rejects, and the error
// surfaces in S7 as a provider 400 about signatures with nothing pointing at
// the store.
//
// So the content column holds the EXACT bytes `model.StoreTranscript` produced
// for that message, and the test asserts the `thinking` text and signature as
// exact strings rather than trusting a marshal/unmarshal cycle to be lossless.
//
// # One message per row, and what UNIQUE (step_id, seq) buys
//
// A transcript grows by append, resume reads it whole, and compaction rewrites
// a middle range. A blob makes every append a read-modify-write of the whole
// transcript — and the constraint is what makes a concurrent append DETECTABLY
// wrong rather than silently lossy.
//
// # No provider shape reaches this table
//
// Content is canonical `[]Block` JSON. A `choices` or `tool_calls` key
// appearing here means something in S3 leaked, and a test looking for them
// turns that into a visible failure HERE rather than a mystery later.

// TranscriptResult is what a read produces.
//
// A result rather than (messages, error) because a transcript can fail to be
// readable in three distinguishable ways, and a recovery needs to tell them
// apart: refused because a NEWER Aeon wrote it, refused because it is
// unparseable, or simply absent. S3's MigrationResult already draws those
// lines, so this carries it rather than flattening it.
type TranscriptResult struct {
	OK bool

	// Messages is the transcript in seq order, when OK.
	Messages []model.Message

	// Migration is S3's verdict, carried through unchanged. Its Outcome
	// distinguishes `from-the-future` from `unparseable`, which send a reader
	// to different fixes.
	Migration model.MigrationResult

	// Count is the number of rows read, which is a fact even when the parse
	// failed — "nine rows that will not parse" and "no rows" are different
	// problems.
	Count int
}

// Reason is the refusal, for a caller that only renders it.
func (r TranscriptResult) Reason() string {
	if r.OK {
		return ""
	}
	return r.Migration.Reason
}

// AppendMessages appends messages to a step's transcript, in ONE transaction.
//
// # The store assigns `seq` (O3)
//
// Monotonically from the current maximum for that step. The caller cannot
// supply it — there is no parameter for it — because a caller-supplied sequence
// is a race, and a half-written transcript is unresumable. All rows for one
// call go in one transaction for the same reason.
func (s *Store) AppendMessages(stepID int64, msgs []model.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	// # Validation happens TWICE, and the two checks are different
	//
	// S3's ValidateTranscript judges a WHOLE transcript — it knows that a
	// conversation cannot open with an assistant turn and that a tool_result
	// cannot precede its tool_use. An append is a FRAGMENT, so running that
	// over the fragment alone rejects every legitimate append of an assistant
	// turn. (Found by writing it the obvious way first: the cascade test
	// refused a four-message transcript as "beginning with an assistant
	// message", because it was validating message 2 on its own.)
	//
	// So: each BLOCK is checked here, where a malformed one names its index
	// before anything is written; and the resulting WHOLE transcript is checked
	// inside the transaction below, which is what actually guarantees that an
	// invalid transcript cannot be stored. S7 replays these rows, and "it was
	// valid when each piece arrived" is not the property recovery needs.
	for i, m := range msgs {
		switch m.Role {
		case model.RoleSystem, model.RoleUser, model.RoleAssistant:
		default:
			return fmt.Errorf("store: refusing to append message %d to step %d: unknown role %q",
				i, stepID, m.Role)
		}
		if len(m.Content) == 0 {
			return fmt.Errorf("store: refusing to append message %d to step %d: it has no content, "+
				"which is what a buggy accumulator produces and no provider accepts", i, stepID)
		}
		// # The provenance gate (M26) — a WRITE rule, deliberately not a read one
		//
		// A NEW assistant turn must name who produced it. The rule is enforced
		// HERE, at the gate new turns enter, and not inside ValidateTranscript:
		// transcripts written before migration 0027 have no producer on record,
		// and a read-path rule would refuse S7's recovery of its own history —
		// rejecting the past rather than protecting the future, and fabricating
		// a producer retroactively would be the exact thing M26 forbids.
		if err := m.ValidateProvenance(); err != nil {
			return fmt.Errorf("store: refusing to append message %d to step %d: %v", i, stepID, err)
		}
		for j, b := range m.Content {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("store: refusing to append message %d block %d to step %d: %w",
					i, j, stepID, err)
			}
		}
	}

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: append to the transcript of step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// COALESCE rather than a NULL check in Go: a step with no messages yet has
	// no MAX, and the first seq must be 1 rather than a special case.
	var next int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM step_message WHERE step_id = ?`, stepID).
		Scan(&next); err != nil {
		return fmt.Errorf("store: find the next seq for step %d: %w", stepID, err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
		VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("store: prepare the transcript insert: %w", err)
	}
	defer stmt.Close()

	now := s.Now()
	for i, m := range msgs {
		content, err := marshalBlocks(m)
		if err != nil {
			return fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		prov, err := marshalProvenance(m)
		if err != nil {
			return fmt.Errorf("store: encode message %d of step %d: %w", i, stepID, err)
		}
		if _, err := stmt.Exec(stepID, next+int64(i), model.FormatVersion, string(m.Role), content, prov, now); err != nil {
			return fmt.Errorf("store: append message %d to step %d: %w", i, stepID, err)
		}
	}

	// The whole transcript, as it now stands, must be valid — checked INSIDE
	// the transaction so an invalid one rolls back rather than being stored.
	if err := validateWhole(tx, stepID); err != nil {
		return fmt.Errorf("store: refusing the append to step %d: %w", stepID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit the transcript of step %d: %w", stepID, err)
	}
	return nil
}

// validateWhole reads a step's transcript inside a transaction and validates it
// as a whole, through S3's one validator.
//
// A suspended transcript — ending in an assistant turn with an unanswered
// tool_use — is VALID, and S3's validator knows that. This is the only reason
// the check can run on every append: the intermediate states of a real step are
// all legitimate transcripts.
func validateWhole(tx *sql.Tx, stepID int64) error {
	rows, err := tx.Query(`SELECT role, content, provenance FROM step_message WHERE step_id = ? ORDER BY seq`, stepID)
	if err != nil {
		return fmt.Errorf("re-read the transcript to validate it: %w", err)
	}
	defer rows.Close()

	var msgs []model.Message
	for rows.Next() {
		var role, content, prov string
		if err := rows.Scan(&role, &content, &prov); err != nil {
			return fmt.Errorf("re-read the transcript to validate it: %w", err)
		}
		var blocks []model.Block
		if err := json.Unmarshal([]byte(content), &blocks); err != nil {
			return fmt.Errorf("a stored message is not a JSON block array: %w", err)
		}
		provenance, err := unmarshalProvenance(prov)
		if err != nil {
			return fmt.Errorf("re-read the transcript to validate it: %w", err)
		}
		msgs = append(msgs, model.Message{Role: model.Role(role), Content: blocks, Provenance: provenance})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if res := model.ValidateTranscript(msgs); !res.OK {
		return fmt.Errorf("the resulting transcript would be invalid: %s (message %d)", res.Reason, res.Index)
	}
	return nil
}

// ReadTranscript reads a step's transcript in seq order.
//
// # A future `format_version` is refused LOUDLY (O4)
//
// Through S3's `MigrateTranscript`, which owns that decision — so the refusal
// is worded once, in the package that knows why. A newer Aeon wrote the row,
// and a best-effort parse would silently drop fields this build does not
// understand, plausibly including a signature.
//
// The rows are reassembled into S3's stored envelope and handed to that one
// function rather than parsed here, because two places deciding what a version
// number means is how they come to disagree.
func (s *Store) ReadTranscript(stepID int64) TranscriptResult {
	rows, err := s.r.Query(`
		SELECT seq, format_version, role, content, provenance
		FROM step_message WHERE step_id = ? ORDER BY seq`, stepID)
	if err != nil {
		return TranscriptResult{Migration: model.MigrationResult{
			Outcome: model.MigrationUnparseable,
			Reason:  fmt.Sprintf("store: read the transcript of step %d: %v", stepID, err),
		}}
	}
	defer rows.Close()

	version := model.FormatVersion
	var msgs []model.Message
	var count int

	for rows.Next() {
		var seq int64
		var rowVersion int
		var role, content, prov string
		if err := rows.Scan(&seq, &rowVersion, &role, &content, &prov); err != nil {
			return TranscriptResult{Migration: model.MigrationResult{
				Outcome: model.MigrationUnparseable,
				Reason:  fmt.Sprintf("store: read row %d of step %d: %v", seq, stepID, err),
			}}
		}
		count++
		// The HIGHEST row version decides, so one row from the future refuses
		// the whole transcript. Parsing the rest and dropping that one would
		// hand a recovery a transcript with a hole in it.
		if rowVersion > version {
			version = rowVersion
		}
		var blocks []model.Block
		if err := json.Unmarshal([]byte(content), &blocks); err != nil {
			return TranscriptResult{Count: count, Migration: model.MigrationResult{
				Outcome: model.MigrationUnparseable,
				Version: rowVersion,
				Reason:  fmt.Sprintf("the content of row %d of step %d is not a JSON block array: %v", seq, stepID, err),
			}}
		}
		provenance, err := unmarshalProvenance(prov)
		if err != nil {
			return TranscriptResult{Count: count, Migration: model.MigrationResult{
				Outcome: model.MigrationUnparseable,
				Version: rowVersion,
				Reason:  fmt.Sprintf("the provenance of row %d of step %d is unreadable: %v", seq, stepID, err),
			}}
		}
		msgs = append(msgs, model.Message{Role: model.Role(role), Content: blocks, Provenance: provenance})
	}
	if err := rows.Err(); err != nil {
		return TranscriptResult{Count: count, Migration: model.MigrationResult{
			Outcome: model.MigrationUnparseable,
			Reason:  fmt.Sprintf("store: read the transcript of step %d: %v", stepID, err),
		}}
	}

	if count == 0 {
		return TranscriptResult{Migration: model.MigrationResult{
			Outcome: model.MigrationUnparseable,
			Reason:  fmt.Sprintf("step %d has no transcript", stepID),
		}}
	}

	// Rebuilt as S3's envelope and migrated through the one function that
	// decides what a version means.
	envelope, err := json.Marshal(model.StoredTranscript{FormatVersion: version, Messages: msgs})
	if err != nil {
		return TranscriptResult{Count: count, Migration: model.MigrationResult{
			Outcome: model.MigrationUnparseable,
			Reason:  fmt.Sprintf("store: re-encode the transcript of step %d: %v", stepID, err),
		}}
	}
	res := model.MigrateTranscript(envelope)
	return TranscriptResult{OK: res.OK(), Messages: res.Messages, Migration: res, Count: count}
}

// ReplaceRange rewrites a contiguous range of a transcript, for compaction.
//
// # Why this exists here rather than at the caller
//
// It is the operation most likely to break the `seq` invariant, and compaction
// must never orphan a `tool_use` from its `tool_result` — a constraint the
// CALLER enforces, but one this function must not make impossible by
// renumbering unpredictably. So: the replacement occupies the range from
// `fromSeq`, everything after `toSeq` is shifted to sit immediately after it,
// and `seq` stays monotonic and contiguous. One transaction.
//
// A caller that wants to know what it is replacing reads first; this does not
// return the old rows, because a function that both destroys and returns
// invites treating the return as the backup.
func (s *Store) ReplaceRange(stepID, fromSeq, toSeq int64, msgs []model.Message) error {
	if fromSeq < 1 || toSeq < fromSeq {
		return fmt.Errorf("store: ReplaceRange needs 1 <= fromSeq <= toSeq, got %d..%d", fromSeq, toSeq)
	}
	for i, m := range msgs {
		for j, b := range m.Content {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("store: refusing replacement %d block %d for step %d: %w", i, j, stepID, err)
			}
		}
	}

	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: replace a transcript range of step %d: %w", stepID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM step_message WHERE step_id = ? AND seq BETWEEN ? AND ?`,
		stepID, fromSeq, toSeq); err != nil {
		return fmt.Errorf("store: remove rows %d..%d of step %d: %w", fromSeq, toSeq, stepID, err)
	}

	// The tail moves to sit immediately after the replacement. Done BEFORE the
	// insert and into a high range first, because SQLite applies UPDATE row by
	// row and a direct shift downward would collide with rows it has not moved
	// yet — the UNIQUE constraint would fire half way through, which is
	// correct behaviour from the constraint and a bug here.
	const parkingOffset = 1 << 30
	if _, err := tx.Exec(`UPDATE step_message SET seq = seq + ? WHERE step_id = ? AND seq > ?`,
		parkingOffset, stepID, toSeq); err != nil {
		return fmt.Errorf("store: park the tail of step %d: %w", stepID, err)
	}

	now := s.Now()
	for i, m := range msgs {
		content, err := marshalBlocks(m)
		if err != nil {
			return fmt.Errorf("store: encode replacement %d of step %d: %w", i, stepID, err)
		}
		prov, err := marshalProvenance(m)
		if err != nil {
			return fmt.Errorf("store: encode replacement %d of step %d: %w", i, stepID, err)
		}
		if _, err := tx.Exec(`
			INSERT INTO step_message (step_id, seq, format_version, role, content, provenance, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			stepID, fromSeq+int64(i), model.FormatVersion, string(m.Role), content, prov, now); err != nil {
			return fmt.Errorf("store: write replacement %d of step %d: %w", i, stepID, err)
		}
	}

	// Bring the parked tail down so seq is contiguous again.
	//
	// The first parked row held `toSeq + 1` and must now hold the seq just
	// after the replacement, so the shift is the difference between those two
	// (plus the parking offset). Worked out from the failing case rather than
	// guessed: an off-by-one here left seq as [1 2 4 5] after replacing two
	// messages with one, and in the neighbouring case collided with the row
	// just inserted — surfacing as a UNIQUE violation during the restore,
	// which reads as a constraint problem rather than as arithmetic.
	shift := parkingOffset + (toSeq + 1) - (fromSeq + int64(len(msgs)))
	if _, err := tx.Exec(`UPDATE step_message SET seq = seq - ? WHERE step_id = ? AND seq > ?`,
		shift, stepID, parkingOffset); err != nil {
		return fmt.Errorf("store: restore the tail of step %d: %w", stepID, err)
	}

	// Compaction is the operation most likely to produce an invalid transcript
	// — it is the one that can orphan a tool_use from its tool_result — so the
	// whole-transcript check matters more here than anywhere.
	if err := validateWhole(tx, stepID); err != nil {
		return fmt.Errorf("store: refusing the replacement in step %d: %w", stepID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit the replacement in step %d: %w", stepID, err)
	}
	return nil
}

// TranscriptRows counts a step's messages, for `step verify` and for the
// cascade test — which asserts by ROW COUNT, because a cascade that did not
// happen leaves rows that read back perfectly well on their own.
func (s *Store) TranscriptRows(stepID int64) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM step_message WHERE step_id = ?`, stepID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count the transcript of step %d: %w", stepID, err)
	}
	return n, nil
}

// RawTranscriptContent returns the stored bytes of one row, unparsed.
//
// It exists for `aeon step verify` and for the fidelity test: the honest check
// of "byte-identical" compares what is ON DISK with what was handed in, and a
// comparison of two parsed structures would pass through the same encoder
// twice and prove nothing about the column.
func (s *Store) RawTranscriptContent(stepID, seq int64) (string, error) {
	var content string
	err := s.r.QueryRow(`SELECT content FROM step_message WHERE step_id = ? AND seq = ?`, stepID, seq).Scan(&content)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("store: step %d has no message %d: %w", stepID, seq, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("store: read message %d of step %d: %w", seq, stepID, err)
	}
	return content, nil
}

// marshalBlocks encodes one message's blocks exactly as AppendMessages does.
//
// ONE encoder, called by both the write and the verification. Two call sites
// doing their own json.Marshal would be two encoders, and `verify` would then
// be comparing its own output against the writer's — which is a test of the
// test.
func marshalBlocks(m model.Message) (string, error) {
	content, err := json.Marshal(m.Content)
	if err != nil {
		return "", fmt.Errorf("store: encode a message's blocks: %w", err)
	}
	return string(content), nil
}

// marshalProvenance encodes the provenance triple the way marshalBlocks does
// the blocks — a separate column because it is a fact ABOUT the turn, not
// content in it (migration 0027). ” means none, which is legal only on
// non-assistant rows; AppendMessages is the gate that says so.
func marshalProvenance(m model.Message) (string, error) {
	if m.Provenance == nil {
		return "", nil
	}
	b, err := json.Marshal(m.Provenance)
	if err != nil {
		return "", fmt.Errorf("store: encode a message's provenance: %w", err)
	}
	return string(b), nil
}

// unmarshalProvenance is its inverse; ” decodes to nil, NOT to a hollow
// Provenance — an empty triple would validate as a lie (M26).
func unmarshalProvenance(s string) (*model.Provenance, error) {
	if s == "" {
		return nil, nil
	}
	var p model.Provenance
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, fmt.Errorf("a stored provenance is not JSON: %w", err)
	}
	return &p, nil
}

// --- the loop-wide transcript window -----------------------------------------
//
// # A different read, for a different reader
//
// `ReadTranscript` above answers "replay step N" — one step, whole, validated
// as a coherent conversation, because S7's recovery and the provider's own
// signature check need exactly that. This answers a different question: "show
// a person a scrollable window of what a LOOP did," which may span many steps
// and many agents, and which a history view can present 49 of 50 turns of —
// naming the one it couldn't — without that being a failure the way a broken
// replay would be.
//
// # Keyset, never OFFSET
//
// `step_message.id` is a single, global, monotonic AUTOINCREMENT across every
// step this store has ever written (not reset per step) — which makes it a
// correct, forgery-proof pagination cursor on its own: `WHERE id > ?` always
// means the same rows regardless of what else was inserted meanwhile. OFFSET
// pagination does not have that property — a row inserted ahead of the window
// shifts every later page by one, and a sibling project hit that in production
// as a live chat's `$skip` cursor going stale mid-session, replaying the same
// page forever until the conversation was deleted. There is no reason to risk
// that failure class here when the table already hands us a cursor that cannot
// go stale.

// LoopTranscriptMessage is one row of a cross-step transcript page.
type LoopTranscriptMessage struct {
	// ID is step_message's own global id — the cursor Before/After compare against.
	ID     int64
	StepID int64
	Agent  string
	Phase  string
	Role   string
	// Content is the stored bytes, unchanged — the same reasoning as
	// Step.ContextManifestRaw: a re-encoding here would silently drop whatever
	// field this package does not itself declare.
	Content   json.RawMessage
	CreatedAt string
	// Unreadable names why THIS row could not be shown — a future
	// format_version, typically — without refusing the rest of the page
	// around it (O4's rule, applied to a reader that loses nothing by naming
	// one bad turn instead of discarding forty-nine good ones with it).
	Unreadable string
}

// LoopTranscriptPage is one window, oldest message first — the same order the
// live event stream renders in.
type LoopTranscriptPage struct {
	Messages []LoopTranscriptMessage
	HasOlder bool
	HasNewer bool
}

// ReadLoopTranscriptPage reads one window of a loop's transcript across every
// step it ran.
//
// Exactly one of before/after is meaningful; both nil means "the latest
// page" — what opening a loop, running or long finished, bootstraps the view
// with, scrolled to the bottom exactly as a live loop already does.
func (s *Store) ReadLoopTranscriptPage(sc Scope, before, after *int64, limit int) (LoopTranscriptPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	scopeWhere, scopeArgs := sc.Where("s")
	base := `SELECT sm.id, sm.step_id, s.agent, s.phase, sm.role, sm.content, sm.format_version, sm.created_at
		FROM step_message sm JOIN step s ON s.id = sm.step_id WHERE ` + scopeWhere

	var query string
	args := append([]any{}, scopeArgs...)
	// DESC-fetched pages (an explicit "before" cursor, and the cursor-less
	// bootstrap) come back newest-first for the LIMIT to bite the right end;
	// reversed below so the wire order is always oldest-first either way.
	descending := after == nil
	switch {
	case before != nil:
		query = base + ` AND sm.id < ? ORDER BY sm.id DESC LIMIT ?`
		args = append(args, *before, limit)
	case after != nil:
		query = base + ` AND sm.id > ? ORDER BY sm.id ASC LIMIT ?`
		args = append(args, *after, limit)
	default:
		query = base + ` ORDER BY sm.id DESC LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.r.Query(query, args...)
	if err != nil {
		return LoopTranscriptPage{}, fmt.Errorf("store: read the transcript of %s: %w", sc, err)
	}
	defer rows.Close()

	var msgs []LoopTranscriptMessage
	for rows.Next() {
		var m LoopTranscriptMessage
		var content string
		var version int
		if err := rows.Scan(&m.ID, &m.StepID, &m.Agent, &m.Phase, &m.Role, &content, &version, &m.CreatedAt); err != nil {
			return LoopTranscriptPage{}, fmt.Errorf("store: read a transcript row of %s: %w", sc, err)
		}
		// The HIGHEST version this build understands decides what it can
		// show — the same rule ReadTranscript enforces for a whole step,
		// applied per row here because a page is not a replay unit.
		if version > model.FormatVersion {
			m.Unreadable = fmt.Sprintf("written by a newer Aeon (format %d, this build reads up to %d)", version, model.FormatVersion)
		} else {
			m.Content = json.RawMessage(content)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return LoopTranscriptPage{}, fmt.Errorf("store: read the transcript of %s: %w", sc, err)
	}
	if descending {
		for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
			msgs[i], msgs[j] = msgs[j], msgs[i]
		}
	}

	page := LoopTranscriptPage{Messages: msgs}
	if len(msgs) == 0 {
		return page, nil
	}
	exists := func(cmp string, id int64) (bool, error) {
		existsArgs := append(append([]any{}, scopeArgs...), id)
		row := s.r.QueryRow(`SELECT EXISTS(SELECT 1 FROM step_message sm JOIN step s ON s.id = sm.step_id WHERE `+scopeWhere+` AND sm.id `+cmp+` ?)`, existsArgs...)
		var has bool
		return has, row.Scan(&has)
	}
	if page.HasOlder, err = exists("<", msgs[0].ID); err != nil {
		return LoopTranscriptPage{}, fmt.Errorf("store: check for older turns in %s: %w", sc, err)
	}
	if page.HasNewer, err = exists(">", msgs[len(msgs)-1].ID); err != nil {
		return LoopTranscriptPage{}, fmt.Errorf("store: check for newer turns in %s: %w", sc, err)
	}
	return page, nil
}
