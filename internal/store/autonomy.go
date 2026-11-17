package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// autonomy.go — the store layer behind S56's autonomy authority (AL3, AL6,
// AL8, AL10, AL11). A grant is a human-authored record of standing authority
// keyed to a component node; it is NOT an S10 policy type (AL1) and NOT the
// S45 desk write path. This file owns the SQL half of the four tables
// (I2: SQL only in internal/store); the pure logic (the vocabulary, the
// validation, the resolver, the conditions, the sentences) lives in
// internal/autonomy, and the daemon bridges the two. Nothing here knows a
// registry, so the action set is never encoded in SQL — the caller passes it.
//
// # The door it is (AU1, AL11 lock 3)
//
// A grant write takes a store.Actor and refuses a non-user/non-cli kind
// BEFORE any SQL: an allow is a human decision, a deny (a veto) or a revoke
// is a human or a cli act (the restrictive class, AL13). The schema's CHECKs
// (created_by_kind state-aware: user for allow rows, user-or-cli for deny
// rows; the revoke vocabulary) are the net; this is the door. An unknown
// actor never becomes a row — a row nobody can name the author of is a row
// nobody can audit.
//
// # What this file does NOT do
//
// It never decides a verdict (the pure side does, AL9), never encodes the
// action registry in SQL (the expansion list is passed in, AL2), never seeds a
// grant (AL16), and never mutates a pin or a history row (AL6/AL8/AL10:
// append-only and immutable by trigger). A grant is never deleted; revoke sets
// the revoked_* trio once and writes a change row.

// AutonomyGrant is one row of autonomy_grant (AL10). The revoked_* trio is the
// only set ever updated, once; everything else is written at creation.
type AutonomyGrant struct {
	ID            string
	NodeID        string
	SubjectKind   string
	Subject       string
	State         string
	Conditions    string
	Note          string
	ExpiresAt     *string
	CreatedByKind string
	CreatedByID   string
	ProposedBy    string
	CreatedAt     string
	RevokedAt     *string
	RevokedByKind *string
	RevokedByID   *string
	RevokedReason *string
}

// NewAutonomyGrant is the caller's request to create a grant (allow) or a
// veto (deny). Conditions is canonical JSON already validated by the pure
// package; "{}" (or empty, which is normalised to "{}") means none. A deny
// carries no conditions and no expiry — the CHECK is the backstop.
type NewAutonomyGrant struct {
	NodeID      string
	SubjectKind string
	Subject     string
	State       string
	Conditions  string
	Note        string
	ExpiresAt   *time.Time
	ProposedBy  string
}

// AutonomyRefusal is the store's own discriminated result (DB19): a refused
// autonomy write names the reason and the thing refused, and the caller
// renders the Sentence verbatim. A nil *AutonomyRefusal means admitted.
type AutonomyRefusal struct {
	Kind     string
	Sentence string
}

func (r AutonomyRefusal) Error() string { return fmt.Sprintf("refused: %s · %s", r.Kind, r.Sentence) }

// AutonomyChange is one row of autonomy_change — the append-only audit, one
// row per changing write (grant, deny, revoke) in the same transaction.
type AutonomyChange struct {
	ID         int64
	At         string
	RootID     string
	GrantID    string
	ChangeKind string
	ActorKind  string
	ActorID    string
	ProposedBy string
	Summary    string
}

// AutonomyUse is one row of autonomy_use — the append-only use log. A begun
// row has no begun_id; a completed/failed row points at its begun.
type AutonomyUse struct {
	ID          int64
	At          string
	LoopID      string
	GrantID     string
	GrantRev    int64
	Action      string
	Phase       string
	BegunID     *int64
	EvidenceRef string
}

// AutonomyPin is the immutable snapshot a policy-resolved loop is pinned with
// (AL8). Internal only, never on the wire; written once in CreatePinnedLoop's
// transaction; no UPDATE touches the pin. The Labels and Allowed are canonical
// JSON (the caller builds them).
type AutonomyPin struct {
	RootID  string
	Rev     int64
	Origin  string
	Labels  string
	Allowed string
}

// AutonomyLiveView is the raw-facts read the decision site makes (AL6/AL9):
// the ceiling (the pin's covered grant, and whether it is revoked or expired)
// and the floor (an active veto on the root or the loop's component). It is
// facts, not a verdict — the pure side decides.
type AutonomyLiveView struct {
	Pinned        bool
	Rev           int64
	Covered       bool
	CoveringGrant string
	Revoked       bool
	Expired       bool
	Vetoed        bool
	VetoGrant     string
	VetoNode      string
}

// AutonomyGrantFilter is the list read (autonomy.list).
type AutonomyGrantFilter struct {
	RootID          string
	State           string
	IncludeInactive bool
}

// AutonomyChangeFilter is the audit read (autonomy.changes).
type AutonomyChangeFilter struct {
	RootID  string
	GrantID string
	Limit   int
}

// AutonomyUseFilter is the use-log read (autonomy.uses).
type AutonomyUseFilter struct {
	RootID  string
	GrantID string
	LoopID  string
	Limit   int
}

// autonomyRefusal answers the AU1 lock-3 door. A grant (allow) write needs a
// human (a user); a deny (veto) or a revoke — the two restrictive writes —
// need a user or a cli (AL13: a person at a terminal may pull authority,
// offline included). A non-user/non-cli kind (agent, trigger) or an anonymous
// actor is refused before any SQL — the CHECK is the backstop a store
// bypassed. A nil result means admitted.
func (a Actor) autonomyRefusal(allowCLI bool) *AutonomyRefusal {
	if a.ID == "" {
		return &AutonomyRefusal{Kind: "actor-anonymous",
			Sentence: "refused: an autonomy write needs a named actor — an anonymous author cannot be audited (AU1)"}
	}
	switch a.Kind {
	case "user":
		return nil
	case "cli":
		if allowCLI {
			return nil
		}
	}
	return &AutonomyRefusal{Kind: "actor-not-human",
		Sentence: fmt.Sprintf("refused: only a user may write a grant · caller kind %q is not a person (AL11 lock 3)", a.Kind)}
}

// changeSummary is the fixed-template audit sentence (SEC-033): it is built
// only from ids and enums (the change kind, the subject, the root, the
// actor), never from the note or the reason — those are user text that lives
// on the grant row, not in the audit.
func changeSummary(changeKind, subject, rootID, actorID string) string {
	return fmt.Sprintf("autonomy %s of %s on root %s by %s", changeKind, subject, rootID, actorID)
}

// CreateAutonomyGrant adds one grant (allow) or veto (deny) and its change row
// in one transaction (AL10). A refused write (a non-user actor, an unknown
// node, an allow on a non-root) answers an *AutonomyRefusal and writes nothing
// — no grant row, no change row. The change row's summary is a fixed template
// from ids and enums, never the note (SEC-033).
func (s *Store) CreateAutonomyGrant(a Actor, in NewAutonomyGrant) (AutonomyGrant, *AutonomyRefusal, error) {
	// The actor door is state-aware (AL13, corrected at T9's pre-flight): an
	// ALLOW is a widening and admits only a user; a DENY is a veto, the
	// restrictive class of revoke, and admits a user or a cli — the direct
	// CLI door writes vetoes as {cli, cli} when the daemon is not running.
	// A grant admitted here is still CHECKed below: created_by_kind is
	// 'user' for allow rows and 'user' or 'cli' for deny rows, never more.
	if ref := a.autonomyRefusal(in.State == "deny"); ref != nil {
		return AutonomyGrant{}, ref, nil
	}
	// The node must exist (no FK: a store check, AL10). For an allow it must be
	// a live root; the trigger is the backstop a store bypassed, the sentence
	// here is the door.
	var (
		parentID sql.NullString
		archived sql.NullString
	)
	if err := s.r.QueryRow(`SELECT parent_id, archived_at FROM component WHERE id = ?`, in.NodeID).Scan(&parentID, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutonomyGrant{}, &AutonomyRefusal{Kind: "unknown-node",
				Sentence: fmt.Sprintf("refused: %s is not a component — a grant needs a node it can sit on", in.NodeID)}, nil
		}
		return AutonomyGrant{}, nil, fmt.Errorf("store: probe node %s: %w", in.NodeID, err)
	}
	isRoot := !parentID.Valid || parentID.String == ""
	if in.State == "allow" {
		if !isRoot {
			return AutonomyGrant{}, &AutonomyRefusal{Kind: "not-a-root",
				Sentence: fmt.Sprintf("refused: allow grants sit on a root component; %s is not a root (AL3)", in.NodeID)}, nil
		}
		if archived.Valid {
			return AutonomyGrant{}, &AutonomyRefusal{Kind: "node-archived",
				Sentence: fmt.Sprintf("refused: %s is archived — its grants cannot be written", in.NodeID)}, nil
		}
	}
	// The root the change row and the rev count key on: the node's own id for a
	// root, its parent for a child (RootOf, CT1).
	rootID := in.NodeID
	if parentID.Valid && parentID.String != "" {
		rootID = parentID.String
	}

	now := s.Now()
	g := AutonomyGrant{
		ID:            "ag-" + newID(),
		NodeID:        in.NodeID,
		SubjectKind:   in.SubjectKind,
		Subject:       in.Subject,
		State:         in.State,
		Conditions:    autonomyConditions(in.Conditions),
		Note:          in.Note,
		CreatedByKind: a.Kind,
		CreatedByID:   a.ID,
		ProposedBy:    in.ProposedBy,
		CreatedAt:     now,
	}
	if in.ExpiresAt != nil {
		e := isoUTC(*in.ExpiresAt)
		g.ExpiresAt = &e
	}
	// A grant's change is "grant"; a veto's is "deny" (AL10's closed set).
	changeKind := "grant"
	if in.State == "deny" {
		changeKind = "deny"
	}

	tx, err := s.w.Begin()
	if err != nil {
		return AutonomyGrant{}, nil, fmt.Errorf("store: create grant %s: %w", g.ID, err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO autonomy_grant (id, node_id, subject_kind, subject, state, conditions, note, expires_at, created_by_kind, created_by_id, proposed_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.NodeID, g.SubjectKind, g.Subject, g.State, g.Conditions, g.Note, nullableStr(g.ExpiresAt),
		g.CreatedByKind, g.CreatedByID, g.ProposedBy, g.CreatedAt); err != nil {
		return AutonomyGrant{}, nil, fmt.Errorf("store: insert grant %s: %w", g.ID, err)
	}
	if _, err := tx.Exec(`INSERT INTO autonomy_change (at, root_id, grant_id, change_kind, actor_kind, actor_id, proposed_by, summary)
		VALUES (?,?,?,?,?,?,?,?)`,
		now, rootID, g.ID, changeKind, a.Kind, a.ID, in.ProposedBy, changeSummary(changeKind, g.Subject, rootID, a.ID)); err != nil {
		return AutonomyGrant{}, nil, fmt.Errorf("store: create grant %s: %w", g.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return AutonomyGrant{}, nil, fmt.Errorf("store: create grant %s: %w", g.ID, err)
	}
	return g, nil, nil
}

// RevokeAutonomyGrant sets the revoked_* trio once and writes a change row in
// the same transaction (AL5/AL6). A grant already revoked is a no-op that
// writes nothing (no-ops write nothing) and returns the grant unchanged; a
// missing grant is ErrNotFound. The affected running loops (pins that name
// the grant, on a non-terminal loop) are returned so the caller can stop them
// with S55's loop.stop — AL does not stop loops itself (OD8).
func (s *Store) RevokeAutonomyGrant(a Actor, id, reason string) (AutonomyGrant, []string, *AutonomyRefusal, error) {
	if ref := a.autonomyRefusal(true); ref != nil {
		return AutonomyGrant{}, nil, ref, nil
	}
	g, err := s.ReadAutonomyGrant(id)
	if err != nil {
		return AutonomyGrant{}, nil, nil, err
	}
	if g.RevokedAt != nil {
		// already revoked: a no-op, no change row (AL5/AL6).
		return g, nil, nil, nil
	}
	rootID, err := s.RootOf(g.NodeID)
	if err != nil {
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: root of grant %s: %w", id, err)
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: revoke grant %s: %w", id, err)
	}
	defer tx.Rollback()
	// the revoked_* trio, once; guarded on revoked_at IS NULL so a concurrent
	// second revoke writes nothing (the same no-op the read guards).
	if _, err := tx.Exec(`UPDATE autonomy_grant SET revoked_at=?, revoked_by_kind=?, revoked_by_id=?, revoked_reason=? WHERE id=? AND revoked_at IS NULL`,
		now, a.Kind, a.ID, reason, id); err != nil {
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: revoke grant %s: %w", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO autonomy_change (at, root_id, grant_id, change_kind, actor_kind, actor_id, proposed_by, summary)
		VALUES (?,?,?,?,?,?,?,?)`,
		now, rootID, id, "revoke", a.Kind, a.ID, "", changeSummary("revoke", g.Subject, rootID, a.ID)); err != nil {
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: revoke grant %s: %w", id, err)
	}
	// the affected running loops: a pin names the grant in its allowed snapshot,
	// and the loop is not in a terminal state. The pin outlives the loop (no
	// FK), so the join is over the pin, not the loop's archive.
	rows, err := tx.Query(`SELECT l.id FROM loop l
		JOIN loop_autonomy_pin p ON p.loop_id = l.id
		JOIN json_each(p.allowed) j ON j.value->>'grant' = ?
		WHERE l.status NOT IN ('done','parked','archived') ORDER BY l.id`, id)
	if err != nil {
		tx.Rollback()
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: find affected loops of %s: %w", id, err)
	}
	var affected []string
	for rows.Next() {
		var lid string
		if err := rows.Scan(&lid); err != nil {
			rows.Close()
			tx.Rollback()
			return AutonomyGrant{}, nil, nil, fmt.Errorf("store: scan affected loops of %s: %w", id, err)
		}
		affected = append(affected, lid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		tx.Rollback()
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: affected loops of %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return AutonomyGrant{}, nil, nil, fmt.Errorf("store: revoke grant %s: %w", id, err)
	}
	g.RevokedAt = &now
	g.RevokedByKind = &a.Kind
	g.RevokedByID = &a.ID
	g.RevokedReason = &reason
	return g, affected, nil, nil
}

// PinnedLoopsForGrant lists the running loops pinned with a grant (the
// autonomy.show read): a pin names the grant in its allowed snapshot and the
// loop is not in a terminal state. It is the same join RevokeAutonomyGrant
// runs for affectedLoops (AL: one join, two readers — the revoke's
// transactional half and this read-only one), minus the transaction: a read
// never opens the writer. The pin outlives the loop (no FK), so the join is
// over the pin, not the loop's archive.
func (s *Store) PinnedLoopsForGrant(id string) ([]string, error) {
	rows, err := s.r.Query(`SELECT l.id FROM loop l
		JOIN loop_autonomy_pin p ON p.loop_id = l.id
		JOIN json_each(p.allowed) j ON j.value->>'grant' = ?
		WHERE l.status NOT IN ('done','parked','archived') ORDER BY l.id`, id)
	if err != nil {
		return nil, fmt.Errorf("store: pinned loops of %s: %w", id, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var lid string
		if err := rows.Scan(&lid); err != nil {
			return nil, fmt.Errorf("store: scan pinned loops of %s: %w", id, err)
		}
		out = append(out, lid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: pinned loops of %s: %w", id, err)
	}
	return out, nil
}

// ReadAutonomyGrant reads one grant by id. Absent is ErrNotFound.
func (s *Store) ReadAutonomyGrant(id string) (AutonomyGrant, error) {
	g, err := scanAutonomyGrant(s.r.QueryRow(`SELECT `+autonomyGrantColumns+` FROM autonomy_grant WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AutonomyGrant{}, fmt.Errorf("store: grant %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AutonomyGrant{}, fmt.Errorf("store: read grant %s: %w", id, err)
	}
	return g, nil
}

// ListAutonomyGrants reads grants, newest first. A root filter matches grants
// on that root and on any of its children (an allow on the root; a veto on the
// root or a child); a state filter matches the polarity; IncludeInactive keeps
// revoked rows (by default they are excluded — a revoked grant is not a live
// authority).
func (s *Store) ListAutonomyGrants(f AutonomyGrantFilter) ([]AutonomyGrant, error) {
	var where []string
	var args []any
	if f.RootID != "" {
		where = append(where, `(node_id = ? OR node_id IN (SELECT id FROM component WHERE parent_id = ?))`)
		args = append(args, f.RootID, f.RootID)
	}
	if f.State != "" {
		where = append(where, `state = ?`)
		args = append(args, f.State)
	}
	if !f.IncludeInactive {
		where = append(where, `revoked_at IS NULL`)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.r.Query(`SELECT `+autonomyGrantColumns+` FROM autonomy_grant`+clause+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list grants: %w", err)
	}
	defer rows.Close()
	var out []AutonomyGrant
	for rows.Next() {
		g, err := scanAutonomyGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list grants: %w", err)
	}
	return out, nil
}

// AutonomyChanges reads the append-only audit, by root or grant, newest first
// (the change id is the rev, so higher id is newer). A limit caps the read.
func (s *Store) AutonomyChanges(f AutonomyChangeFilter) ([]AutonomyChange, error) {
	var where []string
	var args []any
	if f.RootID != "" {
		where = append(where, `root_id = ?`)
		args = append(args, f.RootID)
	}
	if f.GrantID != "" {
		where = append(where, `grant_id = ?`)
		args = append(args, f.GrantID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	query := `SELECT id, at, root_id, grant_id, change_kind, actor_kind, actor_id, proposed_by, summary FROM autonomy_change` + clause + ` ORDER BY id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read changes: %w", err)
	}
	defer rows.Close()
	var out []AutonomyChange
	for rows.Next() {
		var c AutonomyChange
		if err := rows.Scan(&c.ID, &c.At, &c.RootID, &c.GrantID, &c.ChangeKind, &c.ActorKind, &c.ActorID, &c.ProposedBy, &c.Summary); err != nil {
			return nil, fmt.Errorf("store: read changes: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read changes: %w", err)
	}
	return out, nil
}

// AutonomyUses reads the append-only use log, by grant or loop, newest first.
// A root filter maps through the grant's node and its root. A limit caps it.
func (s *Store) AutonomyUses(f AutonomyUseFilter) ([]AutonomyUse, error) {
	var where []string
	var args []any
	if f.GrantID != "" {
		where = append(where, `grant_id = ?`)
		args = append(args, f.GrantID)
	}
	if f.LoopID != "" {
		where = append(where, `loop_id = ?`)
		args = append(args, f.LoopID)
	}
	if f.RootID != "" {
		where = append(where, `grant_id IN (SELECT id FROM autonomy_grant WHERE node_id = ? OR node_id IN (SELECT id FROM component WHERE parent_id = ?))`)
		args = append(args, f.RootID, f.RootID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	query := `SELECT id, at, loop_id, grant_id, grant_rev, action, phase, begun_id, evidence_ref FROM autonomy_use` + clause + ` ORDER BY id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read uses: %w", err)
	}
	defer rows.Close()
	var out []AutonomyUse
	for rows.Next() {
		var u AutonomyUse
		var began sql.NullInt64
		if err := rows.Scan(&u.ID, &u.At, &u.LoopID, &u.GrantID, &u.GrantRev, &u.Action, &u.Phase, &began, &u.EvidenceRef); err != nil {
			return nil, fmt.Errorf("store: read uses: %w", err)
		}
		if began.Valid {
			v := began.Int64
			u.BegunID = &v
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read uses: %w", err)
	}
	return out, nil
}

// AutonomyRev is the root's revision: MAX(autonomy_change.id) over the root,
// 0 if there is no change yet (AL8). It is monotonic — the change id only
// rises — so it is the pin's rev with no extra table.
func (s *Store) AutonomyRev(rootID string) (int64, error) {
	var rev sql.NullInt64
	if err := s.r.QueryRow(`SELECT MAX(id) FROM autonomy_change WHERE root_id = ?`, rootID).Scan(&rev); err != nil {
		return 0, fmt.Errorf("store: rev of root %s: %w", rootID, err)
	}
	if !rev.Valid {
		return 0, nil
	}
	return rev.Int64, nil
}

// ReadAutonomyPin reads the pin a loop was started with (AL8), or (nil, false,
// nil) when the loop was started with no pin (byte-identical to today — a no-
// grant start writes no row). The pin is immutable (AL6); no UPDATE or DELETE
// touches it. A read error (the row exists but the store could not read it) is
// returned, never folded into "no pin".
func (s *Store) ReadAutonomyPin(loopID string) (*AutonomyPin, bool, error) {
	var p AutonomyPin
	err := s.r.QueryRow(`SELECT root_id, rev, origin, labels, allowed FROM loop_autonomy_pin WHERE loop_id = ?`, loopID).Scan(&p.RootID, &p.Rev, &p.Origin, &p.Labels, &p.Allowed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("store: read pin for loop %s: %w", loopID, err)
	}
	return &p, true, nil
}

// CountAutonomyUses counts the begun rows of a grant since a timestamp — the
// maxPerDay input (AL4): the rate condition is the count of begun effects in
// the last 24 h. A begun row is one effect; a completed/failed row is its
// outcome, not a new one.
func (s *Store) CountAutonomyUses(grantID, since string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM autonomy_use WHERE grant_id = ? AND phase = 'begun' AND at >= ?`, grantID, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count uses of %s: %w", grantID, err)
	}
	return n, nil
}

// CountAutonomyUsesSince is the decision site's seam (AL4, R1): the Uses
// condition takes a time, and this renders it in the store's own ISO format
// (isoUTC) so the `at >= since` comparison is apples-to-apples. It is the
// one place the daemon's autonomy seam meets the use log.
func (s *Store) CountAutonomyUsesSince(grantID string, since time.Time) (int, error) {
	return s.CountAutonomyUses(grantID, isoUTC(since))
}

// RecordAutonomyUse appends one use row (AL10): begun before the action runs,
// completed/failed after. For a begun row beginID is 0 (no begun_id); for a
// completed/failed row it names the begun it closes. The CHECK ties phase and
// begun_id together; this is the row-mechanical half.
func (s *Store) RecordAutonomyUse(loopID, grantID string, rev int64, action, phase string, beginID int64, ref string) (int64, error) {
	now := s.Now()
	res, err := s.w.Exec(`INSERT INTO autonomy_use (at, loop_id, grant_id, grant_rev, action, phase, begun_id, evidence_ref)
		VALUES (?,?,?,?,?,?,?,?)`,
		now, loopID, grantID, rev, action, phase, begunIDArg(phase, beginID), ref)
	if err != nil {
		return 0, fmt.Errorf("store: record use of %s: %w", grantID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: record use of %s: %w", grantID, err)
	}
	return id, nil
}

// AutonomyLive is the one read the decision site makes (AL6/AL9): it resolves
// the loop's component and its root, reads the pin (if any), and returns the
// raw facts — whether the action is covered by the pin's snapshot and the
// covering grant's live status (revoked, expired), and whether an active veto
// on the root or the loop's component covers the action. It returns facts, not
// a verdict: the pure side decides.
//
// The outward class is matched by the CALLER's expansion, never by a literal
// in SQL (AL2: "class/outward is shorthand for every current outward action,"
// and the store never encodes the registry). A class/outward veto covers the
// action only when `outward` names it — an expansion the caller (the decision
// site, from action.Classes()) supplies. Without it a class/outward veto would
// match every action (even a local or irreversible one), a veto that overreaches.
// A read error returns the error (the decision site fails toward deny).
func (s *Store) AutonomyLive(ctx context.Context, loopID, action string, outward []string, now time.Time) (AutonomyLiveView, error) {
	var view AutonomyLiveView
	var componentRoot string
	if err := s.r.QueryRowContext(ctx, `SELECT component_root FROM loop WHERE id = ?`, loopID).Scan(&componentRoot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return view, fmt.Errorf("store: autonomy live for loop %s: %w", loopID, ErrNotFound)
		}
		return view, fmt.Errorf("store: autonomy live for loop %s: %w", loopID, err)
	}
	// The component for the loop's root, then the root of its tree. A missing
	// component (ErrNotFound) is NOT a broken store — it is "no autonomy
	// authority on this workspace", byte-identical to a no-grant loop (the
	// legacy Resolve verdict), exactly as the start path (resolveAutonomy)
	// treats a missing root. Only a genuine read failure (the component exists
	// but cannot be read) is returned, so the runner turns it into runner-error
	// and fails toward deny (AL9). Collapsing no-component into an error is the
	// regression that turned every unattended loop on an unregistered workspace
	// into a runner-error.
	comp, err := s.ComponentForRoot(componentRoot)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return view, nil
		}
		return view, fmt.Errorf("store: resolve component for loop %s: %w", loopID, err)
	}
	rootID, err := s.RootOf(comp.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return view, nil
		}
		return view, fmt.Errorf("store: root for loop %s: %w", loopID, err)
	}
	// The pin: read it if a row exists. An absent pin is today's behaviour
	// exactly (Pinned=false, the action falls back to the legacy Resolve).
	var (
		pinRev  sql.NullInt64
		pinJSON string
	)
	err = s.r.QueryRowContext(ctx, `SELECT rev, allowed FROM loop_autonomy_pin WHERE loop_id = ?`, loopID).Scan(&pinRev, &pinJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return view, fmt.Errorf("store: read pin for loop %s: %w", loopID, err)
	}
	if err == nil {
		view.Pinned = true
		view.Rev = pinRev.Int64
		entries, perr := parsePinnedAllowed(pinJSON)
		if perr != nil {
			return view, fmt.Errorf("store: parse pin allowed for loop %s: %w", loopID, perr)
		}
		for _, e := range entries {
			if e.Subject == action {
				view.Covered = true
				view.CoveringGrant = e.Grant
				break
			}
		}
	}
	// The ceiling's live status: is the covering grant revoked or expired?
	if view.Covered {
		g, gerr := s.ReadAutonomyGrant(view.CoveringGrant)
		if gerr != nil {
			return view, gerr
		}
		if g.RevokedAt != nil {
			view.Revoked = true
		}
		if g.ExpiresAt != nil && *g.ExpiresAt <= isoUTC(now) {
			view.Expired = true
		}
	}
	// The floor: an active veto on the root or the loop's component that covers
	// the action. A veto by name matches directly; a class/outward veto matches
	// only when the caller's outward expansion names the action — the store
	// never encodes the registry, so a class/outward veto cannot overreach a
	// local or irreversible action (AL2).
	var (
		vetoGrant string
		vetoNode  string
	)
	err = s.r.QueryRowContext(ctx, `SELECT id, node_id FROM autonomy_grant
		WHERE state = 'deny' AND revoked_at IS NULL
		  AND (node_id = ? OR node_id = ?)
		  AND subject = ?
		ORDER BY id LIMIT 1`, comp.ID, rootID, action).Scan(&vetoGrant, &vetoNode)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return view, fmt.Errorf("store: read veto for loop %s: %w", loopID, err)
	}
	// A class/outward veto covers the action only when it is an outward action
	// in the caller's expansion — the store does not know the registry.
	if err == nil {
		view.Vetoed = true
		view.VetoGrant = vetoGrant
		view.VetoNode = vetoNode
	} else if inSlice(outward, action) {
		var (
			outID   string
			outNode string
		)
		err2 := s.r.QueryRowContext(ctx, `SELECT id, node_id FROM autonomy_grant
			WHERE state = 'deny' AND revoked_at IS NULL
			  AND (node_id = ? OR node_id = ?)
			  AND subject_kind = 'class' AND subject = 'outward'
			ORDER BY id LIMIT 1`, comp.ID, rootID).Scan(&outID, &outNode)
		if err2 != nil && !errors.Is(err2, sql.ErrNoRows) {
			return view, fmt.Errorf("store: read veto for loop %s: %w", loopID, err2)
		}
		if err2 == nil {
			view.Vetoed = true
			view.VetoGrant = outID
			view.VetoNode = outNode
		}
	}
	return view, nil
}

// --- helpers -----------------------------------------------------------------

const autonomyGrantColumns = `id, node_id, subject_kind, subject, state, conditions, note, expires_at,
	created_by_kind, created_by_id, proposed_by, created_at, revoked_at, revoked_by_kind, revoked_by_id, revoked_reason`

// scanAutonomyGrant reads one grant row; the nullable columns ride sql.Null*
// so an absent value never reads as the zero string.
func scanAutonomyGrant(row interface{ Scan(...any) error }) (AutonomyGrant, error) {
	var (
		g             AutonomyGrant
		expiresAt     sql.NullString
		revokedAt     sql.NullString
		revokedBy     sql.NullString
		revokedById   sql.NullString
		revokedReason sql.NullString
	)
	err := row.Scan(&g.ID, &g.NodeID, &g.SubjectKind, &g.Subject, &g.State, &g.Conditions, &g.Note, &expiresAt,
		&g.CreatedByKind, &g.CreatedByID, &g.ProposedBy, &g.CreatedAt, &revokedAt, &revokedBy, &revokedById, &revokedReason)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AutonomyGrant{}, fmt.Errorf("store: grant: %w", ErrNotFound)
		}
		return AutonomyGrant{}, fmt.Errorf("store: read grant: %w", err)
	}
	g.ExpiresAt = nullStringPtr(expiresAt)
	g.RevokedAt = nullStringPtr(revokedAt)
	g.RevokedByKind = nullStringPtr(revokedBy)
	g.RevokedByID = nullStringPtr(revokedById)
	g.RevokedReason = nullStringPtr(revokedReason)
	return g, nil
}

// autonomyConditions normalises an empty conditions to "{}" (a valid-JSON
// no-condition set); the CHECK (json_valid) backstops a malformed one.
func autonomyConditions(c string) string {
	if c == "" {
		return "{}"
	}
	return c
}

// begunIDArg renders the begun_id column: a begun row has no begun_id (NULL),
// a completed/failed row names the begun it closes. The CHECK ties the two.
func begunIDArg(phase string, beginID int64) any {
	if phase == "begun" {
		return nil
	}
	return beginID
}

// pinnedEntry is one entry of a pin's allowed snapshot (AL8): the grant id and
// the action it covers (expanded from the class shorthand at pin time).
type pinnedEntry struct {
	Grant   string `json:"grant"`
	Subject string `json:"subject"`
}

// parsePinnedAllowed decodes a pin's allowed snapshot. It is the one place a
// pin's JSON is read; the store never re-expands the class (the pin already
// carries the expanded action names).
func parsePinnedAllowed(allowed string) ([]pinnedEntry, error) {
	var entries []pinnedEntry
	if err := json.Unmarshal([]byte(allowed), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// inSlice reports whether want is in the slice. AutonomyLive uses it to test
// an action against the caller's outward expansion — the store does not hold
// the registry, so a class/outward veto is confirmed by the caller, not by a
// literal in SQL.
func inSlice(hay []string, want string) bool {
	for _, s := range hay {
		if s == want {
			return true
		}
	}
	return false
}
