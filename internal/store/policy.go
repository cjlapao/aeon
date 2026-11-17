package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ════════════════════════════════════════════════════════════════════════════
// The `policy` and `policy_acceptance` tables — the SQL text, and nothing but
// the SQL text (I2).
//
// # The split this file exists to honour
//
// Aeon's invariant I2 (enforced by TestNoSQLLivesOutsideTheStore) keeps every
// statement in this package, so the domain rule — PL1, the authorship rule —
// lives one layer UP, in internal/policy/store.go, which holds no SQL at all
// and imports nothing here. This file is the infrastructure the rule stands on:
//
//	internal/policy/store.go   Add/Remove/List — the ONE write boundary (PL1/D3)
//	                           RecordAcceptance/AcceptancesFor — the record
//	                           boundary (T7); enforcement NEVER consults it
//	                           (PL10: an unaccepted policy is still enforced)
//	           │  Repo seam (Row / AcceptanceRow / RepoFuncs — the bridge is
//	           │  the wiring's job)
//	           ▼
//	internal/store/policy.go   PolicyAdd · PolicyRemove · PolicyListAll ·
//	                           PolicyAcceptanceRecord · PolicyAcceptanceListFor ·
//	                           PolicyEventInsert · PolicyEventQuery ·
//	                           PolicyEventTrim · PolicyEventStats — here
//
// The vocabulary CHECKs on type / action / source / target_kind (0016) are the
// database-level net UNDER the boundary's refusal: a caller that skipped the
// boundary gets a CHECK error, and the boundary stays the one that produces
// the good message (the seed's T5 notes recorded this division; it is repeated
// here so the next editor does not "simplify" either layer away).
//
// # The columns, and the two NULL rules
//
// The column set is 0005's verbatim, rebuilt by 0016 to the closed CHECKs:
// id, type, target_kind, target_id, action, spec, source, not_after,
// created_at, updated_at. Two of them are nullable and each NULL MEANS
// something (the seed's T5 notes, measured):
//
//   - target_id is NULL — not "" — for the all-agents kind. The kind names
//     every agent; an empty-string sentinel would be a second "no name" that a
//     later reader has to remember to ignore, and every such sentinel is one
//     forgotten ignore away from matching things it should not.
//   - not_after is NULL for a policy that never expires (PL7). "" means the
//     same thing on this seam's Row (so the adapter never constructs a time
//     out of nothing), and PolicyListAll folds the NULL back to "" on read.
//
// # What this layer does NOT do
//
//   - No expiry filter. Evaluate owns expiry (D6/D7/D8) and takes `at` as a
//     parameter; a store that hid expired rows would make T3's nanosecond
//     boundary unreachable and make the audit trail lie about what existed.
//     PolicyListAll returns every row, expired included, ordered by id.
//   - No authorship rule. That is PL1's single home upstairs (D3); a second
//     check here would be a second place for it to drift — the exact gap the
//     grep test in internal/policy exists to make impossible.
//   - No vocabulary re-check. The CHECKs own the table's side; these guards
//     only name the missing field rather than letting SQLite report a raw
//     constraint failure with no field named.
//
// Errors are plain `error` here: these are infrastructure outcomes (the disk
// broke, the statement failed). Domain refusals are *Rejects upstairs, in the
// boundary (S4's DB19).
// ════════════════════════════════════════════════════════════════════════════

// PolicyRow is one row of `policy`, shaped as the table stores it.
//
// Every field is a string because this is the row, not the policy: the closed
// sets are CHECK-validated text here and enum-validated types upstairs. The
// adapter (internal/policy/store.go) converts between Policy and this shape,
// and its Repo seam declares its own identical struct — Go method identity
// means two packages cannot share one parameter type without one importing the
// other, and I2/K-style boundaries forbid that import (the wiring bridges the
// two shapes; see policy.RepoFuncs).
//
// Subject is the row's target rendered — `agent:boss`, `lane-type:review`, the
// bare `all-agents` — the subject the policy governs. It is NOT a column:
// 0016 carries no subject (the subject of a *refusal* belongs to
// policy_event.subject_kind / subject_id, where the design put it, and legal
// constraint 1 forbade a new free-text column), and no second Go renderer of
// it exists — PolicyListAll derives it from target_kind/target_id in the one
// SELECT below, and PolicyAdd ignores whatever the caller set, because there
// is no column to write it into and a value that silently vanished on the way
// in would read as a stored fact.
type PolicyRow struct {
	ID         string
	Type       string
	TargetKind string
	TargetID   string
	Action     string
	Source     string
	Subject    string
	SpecJSON   string
	NotAfter   string // "" == no expiry (stored as NULL; PL7)
	CreatedAt  string
	// Origin is the row's PROVENANCE (S45 T12, PM15): 'system' for a row the
	// seed process wrote, 'user' for a row an operator authored, 'import'
	// reserved. It is NOT policy content — the evaluator, the gate and
	// inforce read no origin (PM11 guard-blindness); the column tells a
	// listing where a rule came from, nothing more. A fresh PolicyAdd leaves
	// it empty and the column's DEFAULT 'user' decides: only the seed door's
	// PolicyAddTx carries an origin into the INSERT.
	Origin string
}

// PolicyAdd inserts one policy row. It is an INSERT, not an upsert: a policy
// that "changed" is removed and re-added, so a silent overwrite of a live rule
// is not available at this layer. The duplicate-id clash arrives as an error
// (infrastructure vocabulary); the boundary above folds it into a refusal that
// names the id.
//
// It runs on the writer pool (DB15/I6): every mutation goes through s.w, the
// single connection, and reads never touch it.
//
// created_at and updated_at both come from r.CreatedAt: a fresh insert is its
// own last update, and there is no update path through here at all (see the
// INSERT-not-upsert note), so a second clock value would be a fact with two
// sources.
func (s *Store) PolicyAdd(ctx context.Context, r PolicyRow) error {
	// Fail-fast preconditions BEFORE the statement (Learnings 2026-08-03): an
	// empty created_at is NOT NULL-passing but nonsense, and naming the missing
	// field beats a raw CHECK failure that names nothing. The CHECKs remain the
	// net for vocabulary the boundary above somehow let through.
	if r.ID == "" {
		return fmt.Errorf("store: a policy needs an id; a policy that cannot be named cannot be audited " +
			"or explained in a verdict")
	}
	if r.Type == "" {
		return fmt.Errorf("store: policy %q has no type", r.ID)
	}
	if r.Action == "" {
		return fmt.Errorf("store: policy %q has no action", r.ID)
	}
	if r.Source == "" {
		return fmt.Errorf("store: policy %q has no source; PL1 reads this field, so it cannot be blank", r.ID)
	}
	if r.TargetKind == "" {
		return fmt.Errorf("store: policy %q has no target kind", r.ID)
	}
	if r.CreatedAt == "" {
		return fmt.Errorf("store: policy %q has no created_at; every insert must supply it (0016 has no default)", r.ID)
	}
	spec := r.SpecJSON
	if spec == "" {
		spec = "{}" // the column default, stated rather than relied upon
	}

	_, err := s.w.ExecContext(ctx, `
		INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Type, r.TargetKind, nullable(r.TargetID), r.Action, spec, r.Source,
		nullable(r.NotAfter), r.CreatedAt, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: add policy %q: %w", r.ID, err)
	}
	return nil
}

// PolicyRemove deletes one policy by id, reporting whether a row existed.
//
// Removal widens what is possible (it can delete a Deny), which is exactly why
// this call has no domain judgement in it and the boundary above keeps it a
// straight pass-through: authoring is the gated direction, and the surfaces
// that reach removal are the operator's (the CLI, T12) — the socket registers
// no authoring method, ever (T13/PL15). The id travels as a bound parameter,
// never concatenated, whatever its shape.
func (s *Store) PolicyRemove(ctx context.Context, id string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM policy WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("store: remove policy %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: remove policy %q: %w", id, err)
	}
	return n > 0, nil
}

// PolicyAddTx inserts one policy row on the CALLER's open transaction, and it
// is the seed door's twin of PolicyAdd (S45 T12): the same columns plus
// `origin`, which only this path carries — the seed applier's row and the
// seed ledger row commit in one transaction (SD3), and only the boundary
// upstairs (policy.Store.AddInTx) decides what goes in. It validates nothing
// about the POLICY (the boundary owns that, in one place — PL1/D3); it keeps
// only the fail-fast preconditions PolicyAdd keeps, plus the origin the
// column CHECK will also refuse: a blank origin names no provenance, and a
// value the CHECK will refuse arrives here as a named refusal instead of a
// statement error that names nothing.
//
// The duplicate-id clash arrives as an error, the same vocabulary as
// PolicyAdd's; the boundary folds it into a refusal that names the id.
func (s *Store) PolicyAddTx(ctx context.Context, tx *sql.Tx, r PolicyRow) error {
	if tx == nil {
		return fmt.Errorf("store: add a policy on a nil transaction; the tx variant exists to join the " +
			"caller's transaction, and a nil there is the missing commit, not an absent one")
	}
	if r.ID == "" {
		return fmt.Errorf("store: a policy needs an id; a policy that cannot be named cannot be audited " +
			"or explained in a verdict")
	}
	if r.Type == "" {
		return fmt.Errorf("store: policy %q has no type", r.ID)
	}
	if r.Action == "" {
		return fmt.Errorf("store: policy %q has no action", r.ID)
	}
	if r.Source == "" {
		return fmt.Errorf("store: policy %q has no source; PL1 reads this field, so it cannot be blank", r.ID)
	}
	if r.TargetKind == "" {
		return fmt.Errorf("store: policy %q has no target kind", r.ID)
	}
	if r.CreatedAt == "" {
		return fmt.Errorf("store: policy %q has no created_at; every insert must supply it (0016 has no default)", r.ID)
	}
	switch r.Origin {
	case "system", "user", "import":
	default:
		return fmt.Errorf("store: policy %q has origin %q; origin is one of system, user, import (S45 T12 provenance)", r.ID, r.Origin)
	}
	spec := r.SpecJSON
	if spec == "" {
		spec = "{}" // the column default, stated rather than relied upon
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO policy (id, type, target_kind, target_id, action, spec, source, not_after, created_at, updated_at, origin)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Type, r.TargetKind, nullable(r.TargetID), r.Action, spec, r.Source,
		nullable(r.NotAfter), r.CreatedAt, r.CreatedAt, r.Origin)
	if err != nil {
		return fmt.Errorf("store: add policy %q: %w", r.ID, err)
	}
	return nil
}

// PolicyDeleteTx deletes one policy by id on the CALLER's open transaction,
// reporting whether a row existed — the seed door's twin of PolicyRemove
// (S45 T12): the engine's update path is delete-then-insert inside the
// transaction the applier shares with the ledger write (SD3), so the two
// halves of the replace commit together or neither does. Like PolicyRemove
// it holds no domain judgement — the judgement is the boundary's.
func (s *Store) PolicyDeleteTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("store: remove policy %q on a nil transaction; the tx variant exists to "+
			"join the caller's transaction, and a nil there is the missing commit, not an absent one", id)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM policy WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("store: remove policy %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: remove policy %q: %w", id, err)
	}
	return n > 0, nil
}

// PolicySeedRow reads one policy row's SEED-RELEVANT content by id on the
// caller's open transaction — the S43-extra T4 SeedAgentRow pattern carried
// onto policy (S45 T12): the applier's Fingerprint and Adoptable read exactly
// the columns a record's content covers, so a hand edit of any of them is
// seen as a divergence. origin is read (and never written by this accessor):
// the applier must be able to tell a system row from an operator row that
// happens to share an id. created_at rides along for the same reason the
// update path needs it — a re-version keeps the row's written-at instant,
// and the instant lives in exactly this one read. Absent is (zero, false,
// nil), never an error.
func (s *Store) PolicySeedRow(ctx context.Context, tx *sql.Tx, id string) (PolicyRow, bool, error) {
	if tx == nil {
		return PolicyRow{}, false, fmt.Errorf("store: read policy %q for seeding on a nil transaction", id)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, type, target_kind, COALESCE(target_id, ''), action, spec, source,
		       COALESCE(not_after, ''), origin, created_at
		FROM policy WHERE id = ?`, id)
	if err != nil {
		return PolicyRow{}, false, fmt.Errorf("store: read policy %q for seeding: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return PolicyRow{}, false, nil
	}
	var r PolicyRow
	if err := rows.Scan(&r.ID, &r.Type, &r.TargetKind, &r.TargetID, &r.Action,
		&r.SpecJSON, &r.Source, &r.NotAfter, &r.Origin, &r.CreatedAt); err != nil {
		return PolicyRow{}, false, fmt.Errorf("store: read policy %q for seeding: scan: %w", id, err)
	}
	if err := rows.Err(); err != nil {
		return PolicyRow{}, false, fmt.Errorf("store: read policy %q for seeding: %w", id, err)
	}
	return r, true, nil
}

// PolicyListAll reads every policy row, expired included, ordered by id.
//
// "Expired included" is the contract (D6/D8), not an oversight: expiry is
// evaluated against `at` by Evaluate, and the audit answer to "what policies
// existed" must not depend on when it is asked.
//
// Subject is derived here, in the one SELECT, from the two target columns —
// matching policy.Target.String() exactly, including the bare-kind rendering
// when a named kind carries an empty or NULL id (a hand-edited row renders as
// its kind and still matches nothing, because targetMatches refuses empty ids;
// the boundary's List refuses such a row outright, and this rendering is about
// what a raw reader sees, not about what is accepted).
//
// It runs on the reader pool: s.r, never the single writer (O6/I6).
func (s *Store) PolicyListAll(ctx context.Context) ([]PolicyRow, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, type, target_kind, COALESCE(target_id, ''), action, spec, source,
		       COALESCE(not_after, ''), created_at,
		       CASE WHEN target_id IS NULL OR target_id = '' THEN target_kind
		            ELSE target_kind || ':' || target_id END,
		       origin
		FROM policy ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list policies: %w", err)
	}
	defer rows.Close()

	var out []PolicyRow
	for rows.Next() {
		var r PolicyRow
		if err := rows.Scan(&r.ID, &r.Type, &r.TargetKind, &r.TargetID, &r.Action,
			&r.SpecJSON, &r.Source, &r.NotAfter, &r.CreatedAt, &r.Subject, &r.Origin); err != nil {
			return nil, fmt.Errorf("store: list policies: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list policies: %w", err)
	}
	return out, nil
}

// ════════════════════════════════════════════════════════════════════════════
// `policy_acceptance` — the record of *who was told, and since when* (T7).
//
// This is bookkeeping, NOT a gate, and the split of duties says so: the two
// methods below never filter, join or annotate the `policy` table. A caller
// that reaches for acceptance to decide anything has the wrong file — the
// evaluator (internal/policy/evaluate.go) holds the decision and does not read
// this table at all (PL10: an unaccepted policy is still enforced; the grep
// test in record_test.go is what keeps that true). What lives here answers
// two audit questions and nothing else: "which policies is this target under,
// since when?" and — paired with T10's disagreement record — "was it told, or
// did it act without being told?"
//
// The column set is exactly the five 0005 declared — policy_id, target_kind,
// target_id, accepted_by, accepted_at (schema_test.go pins it; legal
// constraint 2 forbids a sixth, because a free-text column here is where
// content or a secret would enter the record that every surface renders).
// ════════════════════════════════════════════════════════════════════════════

// AcceptanceRow is one row of `policy_acceptance`, shaped as the table stores
// it. All five fields are strings because this is the row, not the record:
// identifiers and one timestamp, nothing content-bearing and nothing
// secret-bearing (legal constraint 2 — the same five columns the DDL declares,
// field for field; a sixth field here would have no column to arrive from).
//
// AcceptedAt is the RFC3339Nano rendering of the FIRST instant the pair was
// recorded — re-recording does not move it (see PolicyAcceptanceRecord).
type AcceptanceRow struct {
	PolicyID   string
	TargetKind string
	TargetID   string
	AcceptedBy string
	AcceptedAt string
}

// PolicyAcceptanceRecord inserts one acceptance row, and a re-record is a
// no-op — not an error, and not a moved clock.
//
// # Why ON CONFLICT DO NOTHING, and not an upsert
//
// "Since when" is the question this row answers, and it only means anything
// if it names the FIRST time. An upsert (DO UPDATE SET accepted_at) would let
// a repeated accept silently rewrite history: a target that knew in March
// would report knowing since June, and T10's disagreement record — the
// difference between "it was not told" and "it was told and tried anyway" —
// would inherit the later date and become the wrong accusation. DO NOTHING
// keeps the first instant byte for byte (accepted_by included: the first
// declaration stands, whole), returns no clash, and lets the caller retry
// the record as often as it likes. The seed's T6 called this the idempotent-
// write instinct carried from the reference; it is carried with its reason.
//
// The conflict target is named — (policy_id, target_kind, target_id), the
// PRIMARY KEY — rather than left bare, so only the documented identity of an
// acceptance makes a re-record a no-op; any other constraint failure (the
// foreign key to `policy`, for one: acceptance of a policy that does not
// exist is a record of nothing) still arrives as an error.
//
// # The two NULL asymmetries, stated
//
//   - target_id is NOT NULL here, unlike on `policy`: the all-agents kind
//     arrives as the empty string, because the acceptance key needs a literal
//     to complete its PRIMARY KEY and a NULL in a primary-key column is not
//     a thing SQLite carries. The boundary above guarantees the empty string
//     only ever arrives from a target whose kind IS all-agents (ParseTarget
//     refuses any other shape).
//   - accepted_at comes from `at` and nothing else (D5 — the clock is a
//     parameter, never a wall this layer reads); a zero `at` is refused
//     fail-fast above the statement, because a record of "since when" with
//     no instant is the row that answers nothing.
//
// It runs on the writer pool (DB15/I6): every mutation goes through s.w.
func (s *Store) PolicyAcceptanceRecord(ctx context.Context, policyID, targetKind, targetID, acceptedBy string, at time.Time) error {
	// Fail-fast preconditions BEFORE the statement (Learnings 2026-08-03):
	// each names the missing field rather than letting NOT NULL pass a blank
	// or a foreign key fire with no field named. Vocabulary is the boundary's
	// job upstairs (ParseTarget owns the kinds); these guards only refuse the
	// shapeless.
	if policyID == "" {
		return fmt.Errorf("store: an acceptance needs the policy it accepts; an acceptance of nothing " +
			"is a record of nothing")
	}
	if targetKind == "" {
		return fmt.Errorf("store: acceptance of policy %q has no target kind; the acceptance key is "+
			"(policy_id, target_kind, target_id) and a half-key matches nobody", policyID)
	}
	if acceptedBy == "" {
		return fmt.Errorf("store: acceptance of policy %q by %s:%s has no accepted_by; \"who said so\" "+
			"is the point of the row", policyID, targetKind, targetID)
	}
	if at.IsZero() {
		return fmt.Errorf("store: acceptance of policy %q has no instant; \"since when\" is the question "+
			"this row answers (D5: the clock arrives as a parameter)", policyID)
	}

	_, err := s.w.ExecContext(ctx, `
		INSERT INTO policy_acceptance (policy_id, target_kind, target_id, accepted_by, accepted_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (policy_id, target_kind, target_id) DO NOTHING`,
		policyID, targetKind, targetID, acceptedBy, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: record acceptance of policy %q by %s:%s: %w", policyID, targetKind, targetID, err)
	}
	return nil
}

// listAcceptanceSQL is PolicyAcceptanceListFor's statement, a named constant for
// the same reason trimEventsSQL and statsEventsSQL are: the EXPLAIN test
// (TestTheAcceptancePerTargetReadUsesTheNewIndex) plans THIS text, so what is
// asserted is what runs. DEF-027's index claim — the per-target read walks
// idx_policy_acceptance_target, added by 0018 — must survive a rewrite of the
// method, and a rewrite that quietly drops the two-column WHERE back to a scan
// fails there rather than degrading production.
const listAcceptanceSQL = `
		SELECT policy_id, target_kind, target_id, accepted_by, accepted_at
		FROM policy_acceptance
		WHERE target_kind = ? AND target_id = ?
		ORDER BY policy_id`

// PolicyAcceptanceListFor reads every acceptance row for ONE target, ordered
// by policy_id. "Per target" is the shape the table is keyed for: the same
// policy accepted by agent:a and loop:l is two independently queryable rows,
// and this method is what makes each one answerable alone.
//
// It runs on the reader pool (s.r, never the single writer — O6/I6), filters
// by nothing but the two key columns, and returns the raw strings: whether a
// vocabulary value means anything is decided upstairs, in the boundary's
// decode, which refuses a tampered row loudly rather than dropping it
// silently (an acceptance you cannot read is an audit answer missing — say
// so, do not hide it).
//
// The two-column WHERE is the read DEF-027 added an index for (0018's
// idx_policy_acceptance_target; the PRIMARY KEY leads with policy_id and could
// not serve it). The statement is listAcceptanceSQL, planned and asserted
// verbatim.
func (s *Store) PolicyAcceptanceListFor(ctx context.Context, targetKind, targetID string) ([]AcceptanceRow, error) {
	rows, err := s.r.QueryContext(ctx, listAcceptanceSQL, targetKind, targetID)
	if err != nil {
		return nil, fmt.Errorf("store: list acceptances for %s:%s: %w", targetKind, targetID, err)
	}
	defer rows.Close()

	var out []AcceptanceRow
	for rows.Next() {
		var r AcceptanceRow
		if err := rows.Scan(&r.PolicyID, &r.TargetKind, &r.TargetID, &r.AcceptedBy, &r.AcceptedAt); err != nil {
			return nil, fmt.Errorf("store: list acceptances for %s:%s: scan: %w", targetKind, targetID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list acceptances for %s:%s: %w", targetKind, targetID, err)
	}
	return out, nil
}

// ════════════════════════════════════════════════════════════════════════════
// `policy_event` — the refusal audit trail and the retention queries (T8).
//
// # The columns, as actually found (verified before a single statement here)
//
// S4 created the table in 0005 and 0016 left it byte-untouched
// (TestTheRebuildLeavesTheAcceptanceAndEventTablesAlone pins that). The real
// column set is:
//
//	id           INTEGER PRIMARY KEY AUTOINCREMENT
//	policy_id    TEXT   REFERENCES policy(id) ON DELETE SET NULL   — NULL means
//	                   "no rule to name": the default deny (PL5) and the
//	                   unimplemented refusal (D10) decide without a policy, and
//	                   a NULL FK is the schema's own yes to that
//	subject_kind TEXT NOT NULL   } the subject — who the refusal was ABOUT —
//	subject_id   TEXT NOT NULL   } stored as two columns, never one string
//	outcome      TEXT NOT NULL   — the event kind: "refusal" here (T8);
//	                              "disagreement" arrives additively with T10
//	detail       TEXT             — the bound, content-free sentence (nullable
//	                              in the schema; the write boundary refuses an
//	                              empty one, so NULL is not reachable through it)
//	at           TEXT NOT NULL    — the instant the decision was made
//
// plus idx_policy_event_policy ON (policy_id, at), whose DDL comment names its
// two consumers: "The audit query, and the age-based cap." The spec's abstract
// "kind, subject, policy id" land as outcome, subject_kind+subject_id and
// policy_id — no column is missing, so there is no second migration (0016 is
// the slice's only one).
//
// # The single writer
//
// PolicyEventInsert has EXACTLY ONE caller in the tree: recordEvent, the
// unexported sole writer in internal/policy/record.go. No surface reaches this
// method around it (the grep in that file's tests is the assertion; the
// boundary is up the Repo seam, never a direct import — I2). That is legal
// constraint 1's structure: detail is capped to MaxEventDetailRunes (256
// runes, named const) ON WRITE at that one function, rendered only from
// verdict.go's Sentence() or a fixed reason constant — never tool arguments,
// prompt text, file contents, env values or Spec bytes.
//
// # Why `at` is stored fixed-width (and why the trim can then trust `<`)
//
// Rows render `at` as eventAtLayout — RFC3339 with a FORCED 9-digit fraction
// ("…T15:04:05.000000000Z07:00"), always UTC. Go's time.RFC3339Nano drops
// trailing fractional zeros, and the two renderings that produces ("…:00Z" and
// "…:00.5Z") compare backwards to each other lexicographically (the '.' byte
// sorts below 'Z'), which would make the age cap cut the wrong rows at
// sub-second boundaries. Fixed width restores the property strings must have
// for the trim: same length, zero-padded digits, same offset ⇒ byte order IS
// time order. The value still parses as RFC3339Nano on the read side (the
// layout accepts any fraction length), so nothing downstream notices.
//
// # The queries S4's index was built for — asserted, not hoped
//
// Measured with EXPLAIN QUERY PLAN against this exact schema
// (TestTheTrimAndAuditQueriesUseThePolicyEventIndex and
// TestTheAuditStatsPlansMatchWhatWasMeasured re-assert them as SQL):
//
//   - the age-based trim: DELETE ... WHERE rowid IN (SELECT rowid ... WHERE at
//     < ?) — the inner scan runs as SCAN ... USING COVERING INDEX
//     idx_policy_event_policy (the index carries `at` and the rowid, so the
//     scan reads the smaller index, not the table; the bare DELETE form was
//     measured doing a plain table SCAN, so the statement is written in the
//     form the planner proved).
//   - the audit answer (depth + oldest), UNFILTERED — statsEventsSQL, planned
//     exactly as measured:
//       SCAN policy_event USING COVERING INDEX idx_policy_event_policy
//     COUNT(*) and MIN(at) both live in that index, so the table is never read.
//   - the audit answer for ONE subject — statsEventsSubjectSQL, added by F-13,
//     planned exactly as measured (and asserted as this, not as a hope):
//       SCAN policy_event
//     subject_kind/subject_id are in no index and no expression over them is,
//     so this is a table scan; the control that proves the planner WOULD have
//     used the index (same COUNT/MIN shape, WHERE on the indexed policy_id)
//     answers SEARCH ... USING COVERING INDEX idx_policy_event_policy
//     (policy_id=?), which is what makes "SCAN policy_event" a measurement
//     rather than a planner that names nothing for anybody.
//
// The per-SUBJECT reads (PolicyEventQuery, and the filtered half of
// PolicyEventStats) provably cannot use that index. An index on
// subject_kind/subject_id would be a second migration, which this slice
// forbids; the table is bounded by the 90-day cap, so both scans are bounded
// too — the same argument T8 recorded for the listing, now covering the footer
// beside it. The listing's ordering (at DESC, id DESC) is pinned deterministic
// here so the newest-first contract is total even when two rows share an
// instant.
//
// # What this layer does NOT do (same divisions as the two sections above)
//
//   - No detail bound, no vocabulary check on outcome, no subject-shape
//     judgement beyond the split: those are the boundary's (D3's one-place
//     discipline). The guards below only refuse the shapeless, naming the
//     missing field rather than letting NOT NULL pass a blank with no name.
//   - No trim on the write path, ever: an INSERT here never deletes anything
//     (a refusal must not pay for housekeeping; trimming runs on daemon start
//     and on demand — the spy test in internal/policy counts zero).
//   - No clock reads (D5): `at` and the trim cutoff arrive as parameters.
//
// Errors are plain `error` (infrastructure), refusals are *Rejects upstairs —
// S4's DB19, unchanged.
// ════════════════════════════════════════════════════════════════════════════

// eventAtLayout renders policy_event.at: RFC3339Nano with the 9-digit
// fraction FORCED (".000000000" prints even when zero). See the header for why
// — it is the property that makes the trim's string comparison chronological.
// time.Parse(time.RFC3339Nano, …) reads this form back unchanged (the Nano
// layout accepts any fraction length).
const eventAtLayout = "2006-01-02T15:04:05.000000000Z07:00"

// EventRow is one row of `policy_event`, shaped as the table stores it.
//
// The fields are the columns, field for field — this is the row, not the
// record. PolicyID is "" where the table holds NULL (a default-deny or
// unimplemented refusal names no rule); Detail is "" where the table holds
// NULL, a state the write boundary never produces. At is the stored fixed-
// width rendering, byte for byte what the table holds; the boundary's decode
// refuses one that does not read as an instant. ID is the rowid: AUTOINCREMENT
// starts it at 1, so a zero ID on a read row means the double is lying.
type EventRow struct {
	ID          int64
	PolicyID    string
	SubjectKind string
	SubjectID   string
	Outcome     string
	Detail      string
	At          string
}

// trimEventsSQL is the age-based trim. The rowid-IN shape is measured, not
// stylistic: it makes the inner scan run on the COVERING INDEX
// idx_policy_event_policy (EQP names it — asserted by
// TestTheTrimAndAuditQueriesUseThePolicyEventIndex against THIS constant),
// where the bare DELETE form was measured doing a plain table SCAN. Do not
// "simplify" it back.
const trimEventsSQL = `
		DELETE FROM policy_event
		WHERE rowid IN (SELECT rowid FROM policy_event WHERE at < ?)`

// statsEventsSQL is the audit answer (depth + oldest). COUNT(*) and MIN(at)
// both live in idx_policy_event_policy, so the planner answers from the
// COVERING INDEX — also asserted against this constant in the same test.
// The statement is BYTE-IDENTICAL to what T8 measured (F-13 grew the
// per-subject answer as its OWN constant below rather than editing this one, so
// the covering-index plan the test asserts here cannot drift).
const statsEventsSQL = `
		SELECT COUNT(*), MIN(at) FROM policy_event`

// statsEventsSubjectSQL is the audit answer for ONE subject (F-13).
// subject_kind/subject_id are in no index — idx_policy_event_policy is
// (policy_id, at) — so this plan is a table scan over policy_event, measured
// and asserted as such by TestTheAuditStatsPlansMatchWhatWasMeasured. That is
// not an invitation to add a second index: the table is bounded by
// policy.PolicyEventAgeCap (90 days), which is the argument T8 recorded for the
// listing too. The unfiltered statement above keeps its covering-index plan
// untouched.
const statsEventsSubjectSQL = `
		SELECT COUNT(*), MIN(at) FROM policy_event WHERE subject_kind = ? AND subject_id = ?`

// PolicyEventInsert appends one policy_event row. It is an INSERT and nothing
// else — no delete, no upsert, no trim (a refusal never pays for
// housekeeping) — and its single caller is recordEvent in
// internal/policy/record.go, the sole writer (legal constraint 1; grep-pinned
// there).
//
// The subject arrives whole (`kind:id`, as Request.Subject() renders it) and is
// split at the FIRST colon into the two NOT NULL columns: a colon inside the
// id is not a separator — the boundary's identifier gate (the alphabet in
// internal/policy/store.go) excludes ':' on every id that reaches a table, so
// one Cut is total here and the half-empty case can only come from a caller
// that skipped the boundary; it gets named, not stored.
//
// policyID "" stores NULL (the schema's own way of saying "no rule named");
// detail "" is refused fail-fast — a row whose detail is empty answers nothing
// an auditor asks, and the boundary always supplies the rendered sentence
// (PolicyAcceptanceRecord's "who said so" discipline, applied to "what
// happened"). Vocabulary (outcome ∈ refusal/disagreement) is the boundary's;
// this guard only refuses the blank that names no field.
//
// It runs on the writer pool (DB15/I6): every mutation goes through s.w.
func (s *Store) PolicyEventInsert(ctx context.Context, kind, subject, policyID, detail string, at time.Time) error {
	if kind == "" {
		return fmt.Errorf("store: a policy event needs its kind (the outcome column); an event that says " +
			"nothing happened is not an event")
	}
	if subject == "" {
		return fmt.Errorf("store: a %q policy event has no subject; \"who was this about\" is half the audit "+
			"row, and the record boundary always carries it", kind)
	}
	subjectKind, subjectID, ok := strings.Cut(subject, ":")
	if !ok || subjectKind == "" || subjectID == "" {
		return fmt.Errorf("store: policy event subject %q does not split into a non-empty kind and id; the "+
			"columns are subject_kind and subject_id and a half-subject audits nobody", subject)
	}
	if detail == "" {
		return fmt.Errorf("store: the %q policy event for %s has no detail; the detail is what the row says "+
			"happened — the boundary renders it from the verdict sentence and refuses to send nothing", kind, subject)
	}
	if at.IsZero() {
		return fmt.Errorf("store: the %q policy event for %s has no instant; \"when did it refuse\" is the "+
			"third column of the audit row (D5: the clock arrives as a parameter)", kind, subject)
	}

	_, err := s.w.ExecContext(ctx, `
		INSERT INTO policy_event (policy_id, subject_kind, subject_id, outcome, detail, at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		nullable(policyID), subjectKind, subjectID, kind, detail, at.UTC().Format(eventAtLayout))
	if err != nil {
		return fmt.Errorf("store: record policy event: %s against %s: %w", kind, subject, err)
	}
	return nil
}

// PolicyEventInsertTx is PolicyEventInsert's twin on a transaction the
// caller already holds (S45 T12, PM8): the seed door writes a corpus rule
// and its authored record on the engine's one transaction, so the pair
// commits together. Same fail-fast gates, same columns, same rendering —
// the only difference is which handle the statement runs on. A nil tx is
// refused by name: a mutation "on no transaction" is an invisible commit
// waiting to happen, and the seed door's whole argument is that the row
// and its audit row share one commit.
func (s *Store) PolicyEventInsertTx(ctx context.Context, tx *sql.Tx, kind, subject, policyID, detail string, at time.Time) error {
	if tx == nil {
		return fmt.Errorf("store: a %q policy event arrived for a transaction write with no transaction; "+
			"the seed door writes on the transaction it was handed, never its own", kind)
	}
	if kind == "" {
		return fmt.Errorf("store: a policy event needs its kind (the outcome column); an event that says " +
			"nothing happened is not an event")
	}
	if subject == "" {
		return fmt.Errorf("store: a %q policy event has no subject; \"who was this about\" is half the audit "+
			"row, and the record boundary always carries it", kind)
	}
	subjectKind, subjectID, ok := strings.Cut(subject, ":")
	if !ok || subjectKind == "" || subjectID == "" {
		return fmt.Errorf("store: policy event subject %q does not split into a non-empty kind and id; the "+
			"columns are subject_kind and subject_id and a half-subject audits nobody", subject)
	}
	if detail == "" {
		return fmt.Errorf("store: the %q policy event for %s has no detail; the detail is what the row says "+
			"happened — the boundary renders it from the verdict sentence and refuses to send nothing", kind, subject)
	}
	if at.IsZero() {
		return fmt.Errorf("store: the %q policy event for %s has no instant; \"when did it refuse\" is the "+
			"third column of the audit row (D5: the clock arrives as a parameter)", kind, subject)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO policy_event (policy_id, subject_kind, subject_id, outcome, detail, at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		nullable(policyID), subjectKind, subjectID, kind, detail, at.UTC().Format(eventAtLayout))
	if err != nil {
		return fmt.Errorf("store: record policy event: %s against %s: %w", kind, subject, err)
	}
	return nil
}

// PolicyEventQuery reads the audit listing for one subject (or the newest rows
// at all, when subject is ""), newest first: ORDER BY at DESC, id DESC, capped
// at limit. The id tie-break makes the order TOTAL — two refusals in the same
// nanosecond still come back the same way twice, and an audit whose listing
// order is a coin flip reads like a race.
//
// limit must be positive: a 0 or negative limit that silently returns nothing
// is the audit answer that reads as "no refusals happened", which is the lie
// this method exists to prevent; name the defect instead.
//
// It runs on the reader pool (s.r, never the single writer — O6/I6). The
// subject split mirrors PolicyEventInsert exactly, so the write side and the
// read side of a subject can never disagree about what the same string means.
// The (policy_id, at) index provably does not serve this WHERE — see the
// header; what it pins here instead is determinism.
func (s *Store) PolicyEventQuery(ctx context.Context, subject string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("store: query policy events: limit %d is not a listing; pass a positive "+
			"depth (a zero-limit read reads as \"no refusals happened\" and is a defect, not an answer)", limit)
	}
	var (
		where  string
		label  = "all subjects"
		params []any
	)
	if subject != "" {
		subjectKind, subjectID, ok := strings.Cut(subject, ":")
		if !ok || subjectKind == "" || subjectID == "" {
			return nil, fmt.Errorf("store: query policy events: subject %q does not split into a non-empty "+
				"kind and id (the same shape PolicyEventInsert writes); refusing rather than listing nobody", subject)
		}
		where = "WHERE subject_kind = ? AND subject_id = ?"
		params = append(params, subjectKind, subjectID)
		label = subject
	}
	// Order of binding = order of appearance: the WHERE placeholders come
	// before LIMIT ? in the statement, so the limit goes on LAST. (The
	// upstream fake takes typed Go arguments and could never catch this —
	// which is exactly what the SQL-level test in policy_test.go is for.)
	params = append(params, limit)

	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, COALESCE(policy_id, ''), subject_kind, subject_id, outcome, COALESCE(detail, ''), at
		FROM policy_event
		%s
		ORDER BY at DESC, id DESC
		LIMIT ?`, where), params...)
	if err != nil {
		return nil, fmt.Errorf("store: query policy events for %s: %w", label, err)
	}
	defer rows.Close()

	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.ID, &r.PolicyID, &r.SubjectKind, &r.SubjectID, &r.Outcome, &r.Detail, &r.At); err != nil {
			return nil, fmt.Errorf("store: query policy events for %s: scan: %w", label, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query policy events for %s: %w", label, err)
	}
	return out, nil
}

// PolicyEventTrim deletes every row older than the cutoff and reports how many
// went — the age-based cap (PL11/D15) as one statement. The cutoff is
// exclusive: a row AT exactly olderThan survives (the boundary test calls the
// boundary; here the operator is `<`).
//
// # Why the rowid-IN shape, in this file, permanently
//
// The seed's own answer is "the trim uses S4's (policy_id, at) index". A bare
// DELETE FROM policy_event WHERE at < ? was measured doing a plain table SCAN
// — the planner ignores an index whose first column is unconstrained unless
// reading the index is strictly cheaper. Writing the predicate in the
// rowid-IN form makes the inner scan read the COVERING INDEX instead (EQP:
// SCAN policy_event USING COVERING INDEX idx_policy_event_policy — the index
// carries `at` and the rowid, so the filter runs on the smaller structure),
// and the assertion the spec pins — EXPLAIN names idx_policy_event_policy for
// the trim — is then a measured fact re-checked by
// TestTheTrimAndAuditQueriesUseThePolicyEventIndex, not a hope. This is the
// SQL text I2 keeps here; do not "simplify" it back to the bare form.
//
// A zero cutoff is refused before the statement: DELETE WHERE at < '<year 1>'
// would delete nothing and answer as if it had trimmed, and a caller that
// lost the clock (D5) deserves the error naming that, not a quiet zero.
//
// It runs on the writer pool (s.w). It is NEVER called from the insert path —
// trimming runs on daemon start and on demand (the zero-spy test lives in
// internal/policy/retention_test.go).
func (s *Store) PolicyEventTrim(ctx context.Context, olderThan time.Time) (int, error) {
	if olderThan.IsZero() {
		return 0, fmt.Errorf("store: trim policy events: no cutoff instant was given; the age cap is " +
			"measured from a clock the caller supplies (D5), and a lost clock must not read as a trim")
	}
	res, err := s.w.ExecContext(ctx, trimEventsSQL, olderThan.UTC().Format(eventAtLayout))
	if err != nil {
		return 0, fmt.Errorf("store: trim policy events older than %s: %w", olderThan.UTC().Format(eventAtLayout), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: trim policy events: counting the deleted rows: %w", err)
	}
	return int(n), nil
}

// PolicyEventStats answers the audit question the D15 line is built from, for
// the LISTING that was asked for: depth and oldest among `subject`'s rows, or
// the whole table when subject is "" (F-13). "How deep is it" about a filtered
// answer has to be about that filter, or the footer describes a table the
// caller did not ask about — the gap the slice DEMO recorded as F-14 (a
// `--subject` listing that matches nothing printed the whole log's depth and
// called it the answer). The Cap is still the boundary's const
// (policy.PolicyEventAgeCap); this is the measured half ReportEvents reports
// beside it.
//
// The split mirrors PolicyEventInsert/PolicyEventQuery exactly, one Cut at the
// FIRST colon, so the write side and both read sides of a subject can never
// disagree about what the same string means. A half-subject ("agent", "agent:",
// ":x") is refused NAMING the subject rather than answering a depth for nobody.
//
// The unfiltered case runs the untouched statsEventsSQL and keeps its covering
// index (EQP names it; TestTheTrimAndAuditQueriesUseThePolicyEventIndex asserts
// it). The filtered case runs statsEventsSubjectSQL — a table scan, because
// subject_kind/subject_id are in no index; see that constant for why no second
// index is warranted (the 90-day cap bounds the scan, exactly as it bounds the
// listing's scan). Both plans are asserted by
// TestTheAuditStatsPlansMatchWhatWasMeasured.
//
// An empty result answers depth 0 with the zero instant — NOT an error: a fresh
// store, and a subject nobody ever refused, both have a real answer, and "no
// rows" must be distinguishable from "could not read" (P31's honesty applies to
// the audit too — this is the answer that lets `aeon policy events --subject
// agent:nobody` say `depth 0` honestly instead of borrowing somebody else's).
//
// A stored `at` that does not parse is an error naming the row's text: a
// depth/oldest pair built on an unreadable instant is the audit answer that
// lies, and an error is the honest vocabulary for that. It runs on the reader
// pool (s.r).
func (s *Store) PolicyEventStats(ctx context.Context, subject string) (depth int, oldest time.Time, err error) {
	query, label := statsEventsSQL, "all subjects"
	var args []any
	if subject != "" {
		subjectKind, subjectID, ok := strings.Cut(subject, ":")
		if !ok || subjectKind == "" || subjectID == "" {
			return 0, time.Time{}, fmt.Errorf("store: policy event stats: subject %q does not split into a "+
				"non-empty kind and id (the same shape PolicyEventInsert writes); refusing rather than "+
				"reporting a depth for nobody", subject)
		}
		query, args, label = statsEventsSubjectSQL, []any{subjectKind, subjectID}, subject
	}
	var oldestText sql.NullString
	if err := s.r.QueryRowContext(ctx, query, args...).Scan(&depth, &oldestText); err != nil {
		return 0, time.Time{}, fmt.Errorf("store: policy event stats (%s): %w", label, err)
	}
	if !oldestText.Valid {
		return depth, time.Time{}, nil // empty result: depth 0, no oldest instant, no lie
	}
	oldest, err = time.Parse(time.RFC3339Nano, oldestText.String)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("store: policy event stats: the oldest stored instant %q does not "+
			"read as an RFC3339Nano time; the table holds a row that did not come through the write "+
			"boundary: %w", oldestText.String, err)
	}
	return depth, oldest, nil
}
