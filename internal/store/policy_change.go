package store

// ════════════════════════════════════════════════════════════════════════════
// The `policy_change` table — the SQL text, and nothing but the SQL text (I2).
//
// # What this table is (S45 T3, PM8)
//
// The authoring audit: one append-only row per policy authoring WRITE —
// add, remove, seed, commit, reject — naming the actor the kernel stamped.
// It is a separate table from `policy_event` on purpose: that one holds
// VERDICT REFUSALS and is trimmed by retention (`PolicyEventTrim`, the
// 90-day D15 cap); an authoring audit must never be trimmed, so it lives
// where no trim path can reach it (T0 item 3.3's verdict, and
// TestPolicyChangeNotTrimmedByRetention is the proof, not the comment).
//
// # Who writes it, and who does not
//
// The writers are the T4/T5 authoring doors in internal/desk — a row goes
// in with the write it records, never for a read, a refusal, a dry-run or a
// no-op (PM8). T3 created the table and the accessors with NO production
// writer; T4/T5's doors wired them, and TestPolicyChangeWritersAreOnlyTheAuthoringDoors
// pins as a fact of the tree — not a promise — that the only production
// callers are the whitelisted door files. The readers are
// `policy.changes` (this slice's read method) and, later, T4's reconciliation
// read.
//
// The boundary upstairs (internal/policy) learns NOTHING from this file
// (PM11): the evaluator, the gate and the MCP grants stay blind to the
// audit — TestEnforcementNeverReadsPolicyChange greps the four blind files.
//
// # Append-only by trigger (the S44 setting_event precedent)
//
// The migration installs BEFORE UPDATE / BEFORE DELETE triggers that RAISE
// ABORT with their own sentence, and this file offers no Update or Delete
// accessor to go around them: an append is the only mutation (CS8's shape,
// carried from settings.go). The tests refuse an UPDATE and a DELETE at the
// statement level, so the trigger is the enforcement and the test is the
// proof.
//
// # No FK to `policy`
//
// There is deliberately no foreign key to the policy table: a removed
// policy's history must OUTLIVE it (TestPolicyChangeOutlivesThePolicy), and
// a cascade that deletes the audit of a deletion is the record that
// disappears when you ask about it.
//
// # The two NULLs, the vocabulary net, and the clock
//
//   - policy_id is NULL — not "" — for seed (many ids, naming none) and
//     reject (the refusal decided no rule). The accessor maps "" → NULL on
//     the way in and NULL → "" on the way out, the same discipline
//     PolicyAdd/PolicyListAll run for target_id.
//   - proposal_id is NULL for the three policy ops that name no proposal;
//     a *int64, so NULL is never 0 (a zero id would name a row that does
//     not exist; the id sequence starts at 1).
//   - The op / actor_kind CHECKs are the NET under the callers' vocabulary
//     (the same division policy.go's header documents): the CHECK refuses a
//     value nobody compiled; these guards only refuse the blank that names
//     no field.
//   - occurred_at arrives from the caller (D5 — the store takes no clock).
//
// Errors are plain `error` here: these are infrastructure outcomes (the disk
// broke, the statement failed). Domain refusals are *Rejects upstairs (S4's
// DB19).
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"database/sql"
	"fmt"
)

// PolicyChange is one row of `policy_change`, shaped as the table stores it.
//
// The fields are the columns, field for field. PolicyID is "" where the
// table holds NULL (seed names many ids, reject names none); ProposalID is
// nil where the table holds NULL (the three policy ops decide no proposal).
// OccurredAt is the caller-supplied instant as rendered — the store reads no
// clock (D5) — and it travels byte for byte, the same rule EventRow carries
// for policy_event.at.
type PolicyChange struct {
	ID         int64
	Op         string
	PolicyID   string // "" ↔ NULL (seed / reject name no single rule)
	ProposalID *int64 // nil ↔ NULL (no proposal was decided)
	Summary    string
	ActorKind  string // 'user' (socket peer) or 'cli' (the offline door)
	ActorID    string
	OccurredAt string
}

// insertPolicyChangeSQL is the one INSERT — one statement, two execution
// points (the ctx accessor on the writer pool, the tx variant inside a
// caller's transaction). A const so the two paths cannot drift apart.
const insertPolicyChangeSQL = `
		INSERT INTO policy_change (op, policy_id, proposal_id, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

// PolicyChangeInsert appends one authoring-audit row on the writer pool
// (DB15/I6: every mutation goes through s.w; reads never touch it).
//
// One row per authoring write (PM8) — the CALLER owns when a write counts
// (an add that stored a row, a reject that settled a proposal), and this
// accessor owns only that the row is complete before it reaches the CHECK
// net: every NOT NULL field must be named (an audit row that does not say
// what happened answers nothing an auditor asks), and the vocabulary itself
// — op ∈ add/remove/seed/commit/reject, actor_kind ∈ user/cli — belongs to
// the CHECKs, which is where a value nobody compiled is refused (DB19's
// division again, repeated from policy.go so the next editor does not
// "simplify" either layer away).
//
// The authoring doors call this (or the tx variant) beside the write they
// record; the grep-pinned proof that nothing OUTSIDE THE DOORS does is
// TestPolicyChangeWritersAreOnlyTheAuthoringDoors.
func (s *Store) PolicyChangeInsert(ctx context.Context, c PolicyChange) error {
	if err := checkPolicyChangeShape(c); err != nil {
		return err
	}
	if _, err := s.w.ExecContext(ctx, insertPolicyChangeSQL,
		c.Op, nullablePolicyChangeID(c.PolicyID), c.ProposalID,
		c.Summary, c.ActorKind, c.ActorID, c.OccurredAt); err != nil {
		return fmt.Errorf("store: append policy_change %s %q: %w", c.Op, c.Summary, err)
	}
	return nil
}

// PolicyChangeInsertTx appends one authoring-audit row on a transaction the
// CALLER already holds. It exists because T4/T5 want the audit row in the
// SAME transaction as the write it records wherever the store allows it
// (PM8): the row and the change commit together or not at all, so the audit
// can never name a write the table did not make — the setting_event rule
// (S44 CS8), carried onto the authoring audit.
//
// It takes the caller's ctx (the deviation from the task sketch, recorded in
// the task Notes): every store statement honours cancellation, and a tx
// variant without a ctx would be the one seam that could not be interrupted.
// The same const statement as the ctx variant — one INSERT, two paths, zero
// drift.
func (s *Store) PolicyChangeInsertTx(ctx context.Context, tx *sql.Tx, c PolicyChange) error {
	if tx == nil {
		return fmt.Errorf("store: append policy_change on a nil transaction; the tx variant exists to " +
			"join the caller's transaction, and a nil there is the missing commit, not an absent one")
	}
	if err := checkPolicyChangeShape(c); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, insertPolicyChangeSQL,
		c.Op, nullablePolicyChangeID(c.PolicyID), c.ProposalID,
		c.Summary, c.ActorKind, c.ActorID, c.OccurredAt); err != nil {
		return fmt.Errorf("store: append policy_change %s %q: %w", c.Op, c.Summary, err)
	}
	return nil
}

// PolicySeedAuditTx appends the ONE op='seed' authoring-audit row an apply run
// of the policy.corpus set owes (S45 T12, PM15), on the seed engine's open
// transaction. It is a named sibling of PolicyChangeInsertTx rather than a
// caller of it, for exactly one reason: TestPolicyChangeWriterHasNoProduction
// Caller pins the two T3 accessors as having no production caller until the
// T4/T5 desk doors land, and the seed door's writer is neither of them — the
// same INSERT const, the same CHECK net, a name that says WHO may call it.
// The pin's own text anticipated this day; the seed door is the first door
// that opens it, and the caller whitelist lives beside the PL15 guards
// (cmd/aeon/policy_guards_cases_test.go's maps cite this task).
//
// It keeps every shape check PolicyChangeInsertTx keeps (one fail-fast half,
// the CHECKs as the vocabulary net), refuses anything but op='seed' — a door
// that writes only seed rows cannot forge an add or a remove — and shares the
// one INSERT const so the three paths cannot drift.
func (s *Store) PolicySeedAuditTx(ctx context.Context, tx *sql.Tx, c PolicyChange) error {
	if tx == nil {
		return fmt.Errorf("store: append the seed audit on a nil transaction; the seed audit joins the " +
			"engine's transaction, and a nil there is the missing commit, not an absent one")
	}
	if c.Op != "seed" {
		return fmt.Errorf("store: PolicySeedAuditTx writes the seed row only; it was handed op %q — "+
			"every other authoring op travels its own door's accessor (S45 T12)", c.Op)
	}
	if err := checkPolicyChangeShape(c); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, insertPolicyChangeSQL,
		c.Op, nullablePolicyChangeID(c.PolicyID), c.ProposalID,
		c.Summary, c.ActorKind, c.ActorID, c.OccurredAt); err != nil {
		return fmt.Errorf("store: append policy_change %s %q: %w", c.Op, c.Summary, err)
	}
	return nil
}

// checkPolicyChangeShape is the fail-fast half (Learnings 2026-08-03): every
// NOT NULL column named before the statement runs, so the missing field is
// named rather than reported as a raw CHECK failure. Vocabulary stays the
// CHECKs' (the net under the callers); this refuses only the blanks.
func checkPolicyChangeShape(c PolicyChange) error {
	if c.Op == "" {
		return fmt.Errorf("store: a policy_change needs its op (add/remove/seed/commit/reject); " +
			"an audit row that does not say which authoring write happened is not an audit row")
	}
	if c.Summary == "" {
		return fmt.Errorf("store: the %q policy_change has no summary; the summary is what the row says "+
			"happened, and an empty one answers nothing an auditor asks (SEC-033: a fixed-template "+
			"sentence built from ids, never free text)", c.Op)
	}
	if c.ActorKind == "" {
		return fmt.Errorf("store: the %q policy_change has no actor kind; who widened the guard is half "+
			"the audit row (PM8), and the door that cannot say is the door that must not write", c.Op)
	}
	if c.ActorID == "" {
		return fmt.Errorf("store: the %q policy_change has no actor id; an audit row naming no actor is "+
			"the unsigned log entry PM8 exists to delete (T4/T5 refuse an unresolved peer rather than "+
			"stamp a placeholder)", c.Op)
	}
	if c.OccurredAt == "" {
		return fmt.Errorf("store: the %q policy_change has no instant; when the guard was widened is "+
			"the third column of the audit row (D5: the clock arrives as a parameter)", c.Op)
	}
	return nil
}

// nullablePolicyChangeID maps "" onto NULL for policy_id — the table's way
// of saying "this authoring write names no single rule" (seed, reject).
func nullablePolicyChangeID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// PolicyChangeList reads the authoring audit newest first.
//
// policyID=="" spans every policy; a named id is that rule's trail — the
// (policy_id, id) index the migration installed serves exactly this read
// (ORDER BY id DESC on the index's second column). id orders the unfiltered
// read: newest first is the trail the UI shows, and the id tie-break makes
// the order TOTAL (the same reason PolicyEventQuery orders by id last — two
// authoring writes in one second still come back the same way twice).
//
// limit is clamped to [1,500], defaulting to 50 — the settings-audit clamp
// rule (S44), restated here because the contract is this accessor's: an
// unset limit reads as "give me everything" and is a defect, not an answer;
// the clamp keeps the answer bounded without refusing the call.
//
// It runs on the reader pool (O6/I6: s.r, never the single writer).
func (s *Store) PolicyChangeList(ctx context.Context, policyID string, limit int) ([]PolicyChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var (
		where string
		args  = []any{}
	)
	if policyID != "" {
		where = "WHERE policy_id = ?"
		args = append(args, policyID)
	}
	// Binding order = order of appearance: the WHERE placeholder comes
	// before LIMIT ? in the statement, so the limit goes on LAST.
	args = append(args, limit)

	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, op, COALESCE(policy_id, ''), proposal_id, summary, actor_kind, actor_id, occurred_at
		FROM policy_change
		%s
		ORDER BY id DESC
		LIMIT ?`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list policy changes: %w", err)
	}
	defer rows.Close()

	var out []PolicyChange
	for rows.Next() {
		var (
			c        PolicyChange
			proposal sql.NullInt64
		)
		if err := rows.Scan(&c.ID, &c.Op, &c.PolicyID, &proposal, &c.Summary,
			&c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: list policy changes: scan: %w", err)
		}
		if proposal.Valid {
			v := proposal.Int64
			c.ProposalID = &v
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list policy changes: %w", err)
	}
	return out, nil
}

// PolicyChangeCount answers the three-state question the `policy.changes`
// statement turns on: is the audit table EMPTY (the statement
// "no authoring changes recorded." belongs to THAT answer alone), or merely
// unmatched by a filter? Counting the whole table separately is why a
// filtered-empty read can never print the empty-table sentence — the
// honesty PolicyEventStats carries for policy_event, carried onto this
// audit (DEF-041: an audit answer must not collapse "nothing happened" and
// "you did not ask about the right thing").
//
// It runs on the reader pool (s.r).
func (s *Store) PolicyChangeCount(ctx context.Context) (int, error) {
	var n int
	if err := s.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_change`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count policy changes: %w", err)
	}
	return n, nil
}
