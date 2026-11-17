// dod.go — the definition-of-done ledger (VG13, S57 T5).
//
// The loop's acceptance criteria, as ROWS: the daemon materialises them
// from an APPROVED brief (never the agent — MaterializeDoD has no actor
// parameter because the daemon is its only caller, and T8's door says so
// at the socket), a person or the engine marks an item met or not with
// its evidence (MarkDoD), and the merge rule asks the ledger whether the
// loop's done is done (the evaluator's DoD facts, T5's package).
//
// Rows are loop-scoped (DB4: a slug alone is not a loop) and idempotent
// on (brief_id, idx). A brief version has its own id, so a new version is
// a new set of rows at the same idx; materialising it carries a previous
// version's `met` over to the new row ONLY when the text is byte-equal —
// a changed text is a new acceptance criterion, and its old state is
// stale (it is told in a `reset` event). The store never deletes an old
// version's rows: they are history, and the list read shows the loop's
// latest version.
//
// The ledger's doors (VG23's shape): a MARK admits a person (user) or the
// engine (trigger) — the engine marks items its own verification run
// met — and nothing else. A `met` always names its evidence
// (kind+ref); a manual evidence is a person's mark, a verify_run
// evidence the engine's, and the store enforces the pairing as a
// backstop. A refused mark is a discriminated DodRefusal with a
// sentence (DB19), never an error, and writes nothing.

package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// DodRefusal is the closed set of DoD write refusals (DB19).
type DodRefusal string

const (
	DodRefusalNone                  DodRefusal = ""
	DodRefusalActorNotMarking       DodRefusal = "actor-not-marking"
	DodRefusalStateInvalid          DodRefusal = "state-invalid"
	DodRefusalItemUnknown           DodRefusal = "item-unknown"
	DodRefusalMetWithoutEvidence    DodRefusal = "met-without-evidence"
	DodRefusalEvidenceActorMismatch DodRefusal = "evidence-actor-mismatch"
)

// DodItem is one acceptance criterion of one loop, at one brief version.
// State is unchecked|met|unmet|unverifiable; a `met` always carries its
// evidence (the store enforces it before SQL), and the actor is the one
// who last marked it.
type DodItem struct {
	ID            string
	LoopSlug      string
	ComponentRoot string
	BriefID       string
	BriefVersion  int
	IDx           int
	Text          string
	State         string
	EvidenceKind  string // "" | "verify_run" | "manual"
	EvidenceRef   string
	ActorKind     *string
	ActorID       *string
	UpdatedAt     string
}

// DoDBrief is the materialisation's input: the approved brief's identity
// and its acceptance list, in order (idx is the position).
type DoDBrief struct {
	BriefID      string
	BriefVersion int
	Items        []string
}

// DodEvidence is the evidence a `met` names. Note is the mark's
// free-text reason (S57 T9): it is audit text — the ledger's event row
// carries it — never evidence a verdict reads from.
type DodEvidence struct {
	Kind string // "verify_run" | "manual"
	Ref  string
	Note string
}

// DodWrite is the result of a DoD mark: the new item when accepted, or
// the refusal and its sentence when not (DB19: a refused mark is a
// discriminated result, never an error, and writes nothing).
type DodWrite struct {
	Item    DodItem
	Refusal DodRefusal
	Reason  string
}

// OK reports whether the mark was accepted.
func (w DodWrite) OK() bool { return w.Refusal == DodRefusalNone }

// dodColumns is the row's scan shape (the id and scope first, the ledger
// tail last).
const dodColumns = `id, loop_slug, component_root, brief_id, brief_version, idx, text,
	state, evidence_kind, evidence_ref, actor_kind, actor_id, updated_at`

func scanDodItem(row interface{ Scan(...any) error }) (DodItem, error) {
	var (
		it DodItem
		ak sql.NullString
		ai sql.NullString
	)
	err := row.Scan(&it.ID, &it.LoopSlug, &it.ComponentRoot, &it.BriefID, &it.BriefVersion,
		&it.IDx, &it.Text, &it.State, &it.EvidenceKind, &it.EvidenceRef, &ak, &ai, &it.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DodItem{}, fmt.Errorf("store: dod item: %w", ErrNotFound)
		}
		return DodItem{}, fmt.Errorf("store: read dod item: %w", err)
	}
	it.ActorKind = nullStringPtr(ak)
	it.ActorID = nullStringPtr(ai)
	return it, nil
}

// dodActorRefusal answers the mark's door: a person (user), the engine
// (trigger), or the loop's own agent (S57 T9) — and nothing else. The
// engine's marks are its own verification runs; an agent's are the
// engine's runs too, but the pairing below decides that: an agent may
// mark met ONLY on verify_run evidence of its own loop's passing run
// (the store re-checks run-exists / pass / same-loop; the daemon's
// head-currency check is the service's half), never on manual, and a
// criterion met by a person's word is the person's mark alone. An agent
// claiming its own work is met without a run is the vacuous green the
// ledger exists to prevent (VG13).
func (a Actor) dodActorRefusal() (DodRefusal, string) {
	if a.ID == "" {
		return DodRefusalActorNotMarking, "refused: a DoD mark needs a named actor — an anonymous author cannot be audited (S57 T5)"
	}
	if a.Kind == "user" || a.Kind == "trigger" || a.Kind == "agent" {
		return DodRefusalNone, ""
	}
	return DodRefusalActorNotMarking, fmt.Sprintf("refused: only a person, the engine, or the loop's own agent may mark the DoD · caller kind %q cannot mark at all (S57 T5, T9)", a.Kind)
}

func dodStateOK(v string) bool {
	return v == "unchecked" || v == "met" || v == "unmet" || v == "unverifiable"
}

// MaterializeDoD files a loop's acceptance list from the approved brief,
// idempotently: re-running the same brief version writes nothing, and a
// new version's rows are decided one idx at a time against the loop's
// previous version — a `met` carries over ONLY for a byte-equal text, a
// changed text is reset to unchecked (and told in a `reset` event), and
// a new idx is a new unchecked row (a `materialize` event). One
// transaction, and the items the list drops are left in place: the
// store does not delete history. Returns the number of rows written.
func (s *Store) MaterializeDoD(sc Scope, b DoDBrief) (int, error) {
	if !sc.Valid() {
		return 0, fmt.Errorf("store: MaterializeDoD needs both a slug and a component root, got %s", sc)
	}
	if b.BriefID == "" {
		return 0, fmt.Errorf("store: MaterializeDoD: the brief names no id")
	}
	if b.BriefVersion < 1 {
		return 0, fmt.Errorf("store: MaterializeDoD: the brief's version is %d; a filed brief is version 1 or more", b.BriefVersion)
	}
	var one int
	if err := s.r.QueryRow(`SELECT 1 FROM loop WHERE component_root = ? AND slug = ?`,
		sc.ComponentRoot, sc.LoopSlug).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: loop %s/%s does not exist — a DoD item belongs to a loop", sc.ComponentRoot, sc.LoopSlug)
		}
		return 0, fmt.Errorf("store: check loop %s: %w", sc, err)
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
	}
	defer tx.Rollback()
	where, args := sc.Where("")
	written := 0
	for i, text := range b.Items {
		// Idempotent on (brief_id, idx): a re-materialisation of the same
		// version finds its own rows and writes nothing.
		var existing string
		err := tx.QueryRow(`SELECT id FROM dod_item WHERE brief_id = ? AND idx = ?`, b.BriefID, i).Scan(&existing)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
		}
		// The previous version's row at this idx (the newest one), if any.
		var (
			prevState, prevText string
			prevKind, prevRef   string
			prevAK, prevAI      sql.NullString
			hasPrev             bool
		)
		err = tx.QueryRow(`SELECT state, text, evidence_kind, evidence_ref, actor_kind, actor_id
			FROM dod_item WHERE `+where+` AND idx = ? AND brief_id <> ?
			ORDER BY brief_version DESC, id DESC LIMIT 1`,
			append(append([]any{}, args...), i, b.BriefID)...).
			Scan(&prevState, &prevText, &prevKind, &prevRef, &prevAK, &prevAI)
		if err == nil {
			hasPrev = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
		}
		// The decision: carry a `met` only for a byte-equal text; a
		// changed text is a new criterion (reset); no previous is a
		// fresh one.
		state, kind, ref := "unchecked", "", ""
		var actorKind, actorID *string
		op, fromState := "materialize", ""
		switch {
		case hasPrev && prevText == text && prevState == "met":
			state, kind, ref = "met", prevKind, prevRef
			actorKind, actorID = nullStringPtr(prevAK), nullStringPtr(prevAI)
		case hasPrev && prevText != text:
			op, fromState = "reset", prevState
		}
		id := "dod-" + newID()
		if _, err := tx.Exec(`INSERT INTO dod_item (id, loop_slug, component_root, brief_id, brief_version, idx,
			text, state, evidence_kind, evidence_ref, actor_kind, actor_id, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, sc.LoopSlug, sc.ComponentRoot, b.BriefID, b.BriefVersion, i, text,
			state, kind, ref, nullableStr(actorKind), nullableStr(actorID), now); err != nil {
			return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
		}
		ev := ""
		if state == "met" {
			ev = kind + ":" + ref
		}
		if err := appendDodEvent(tx, id, op, fromState, state, ev, actorKind, actorID, now); err != nil {
			return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
		}
		written++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: materialize DoD for %s: %w", sc, err)
	}
	return written, nil
}

// ListDoD reads the loop's CURRENT acceptance list: the rows of its
// latest brief version, in idx order. A loop with no materialised items
// is an empty list, not an error — the rule's dod-none refusal is the
// evaluator's call with that answer (VG13).
func (s *Store) ListDoD(sc Scope) ([]DodItem, error) {
	if !sc.Valid() {
		return nil, fmt.Errorf("store: ListDoD needs both a slug and a component root, got %s", sc)
	}
	where, args := sc.Where("")
	q := `SELECT ` + dodColumns + ` FROM dod_item WHERE ` + where + `
		AND brief_version = (SELECT MAX(brief_version) FROM dod_item WHERE ` + where + `)
		ORDER BY idx`
	rows, err := s.r.Query(q, append(append([]any{}, args...), args...)...)
	if err != nil {
		return nil, fmt.Errorf("store: list DoD for %s: %w", sc, err)
	}
	defer rows.Close()
	var out []DodItem
	for rows.Next() {
		it, err := scanDodItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list DoD for %s: %w", sc, err)
	}
	return out, nil
}

// MarkDoD marks one item with its state and (for a `met`) its evidence,
// with its event in the same transaction. The doors: the actor's (user,
// trigger, or the loop's own agent — S57 T9), the state's closed set,
// and a `met` that names its evidence — manual evidence by a person,
// verify_run evidence by the engine or by the loop's own agent (whose
// run the store re-checks: it exists, it passed, it is this item's loop
// — the daemon's head-currency check is the service's half), and the
// pairing enforced as a backstop. A non-met mark CLEARS the evidence: a
// criterion that is not met has nothing to point at. A no-op mark (the
// same state, evidence and actor) writes nothing. ev.Note rides the
// audit row only (see DodEvidence).
func (s *Store) MarkDoD(a Actor, id, state string, ev DodEvidence) (DodWrite, error) {
	refuse := func(r DodRefusal, why string) (DodWrite, error) {
		return DodWrite{Refusal: r, Reason: "refused: " + why}, nil
	}
	if r, why := a.dodActorRefusal(); r != DodRefusalNone {
		return refuse(r, why)
	}
	if !dodStateOK(state) {
		return refuse(DodRefusalStateInvalid,
			fmt.Sprintf("the state is %q; the closed set is unchecked|met|unmet|unverifiable", state))
	}
	cur, err := s.GetDoDItem(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return refuse(DodRefusalItemUnknown, fmt.Sprintf("%s is not a DoD item", id))
		}
		return DodWrite{}, err
	}
	kind, ref := "", ""
	if state == "met" {
		switch ev.Kind {
		case "verify_run", "manual":
		default:
			return refuse(DodRefusalMetWithoutEvidence,
				fmt.Sprintf("a met item needs its evidence — the kind is %q; the closed set is verify_run|manual", ev.Kind))
		}
		if ev.Ref == "" {
			return refuse(DodRefusalMetWithoutEvidence, "a met item names its evidence — the ref is empty")
		}
		// The pairing backstop: the evidence's kind says whose mark it is,
		// and the actor must agree with it. Manual is a person's mark, full
		// stop; verify_run is the engine's, or the loop's own agent's — and
		// the agent's run is re-checked here (the store's half; the daemon's
		// head-currency check is the service's half, since git is not a
		// store concern): the run exists, it passed, and it is THIS item's
		// loop. An agent marking met on its own word is the vacuous green
		// VG13 exists to prevent.
		if ev.Kind == "manual" && a.Kind != "user" {
			return refuse(DodRefusalEvidenceActorMismatch,
				fmt.Sprintf("manual evidence is a person's mark — caller kind %q is not user", a.Kind))
		}
		if ev.Kind == "verify_run" && a.Kind != "trigger" && a.Kind != "agent" {
			return refuse(DodRefusalEvidenceActorMismatch,
				fmt.Sprintf("verify_run evidence is the engine's or the loop's agent's mark — caller kind %q is neither", a.Kind))
		}
		if ev.Kind == "verify_run" && a.Kind == "agent" {
			run, _, rerr := s.GetVerifyRun(ev.Ref)
			if rerr != nil {
				return refuse(DodRefusalMetWithoutEvidence,
					fmt.Sprintf("the named run %s is not a verify run — an agent's met mark needs a real run of its own loop", ev.Ref))
			}
			if run.State != "pass" {
				return refuse(DodRefusalMetWithoutEvidence,
					fmt.Sprintf("the run %s ended %s, not pass — a met mark names a passing run", ev.Ref, run.State))
			}
			if run.LoopSlug != cur.LoopSlug {
				return refuse(DodRefusalMetWithoutEvidence,
					fmt.Sprintf("the run %s is loop %s, this item is loop %s — the evidence must be the item's own loop", ev.Ref, run.LoopSlug, cur.LoopSlug))
			}
		}
		kind, ref = ev.Kind, ev.Ref
	}
	// A no-op: nothing moved, nothing is told.
	if cur.State == state && cur.EvidenceKind == kind && cur.EvidenceRef == ref &&
		sameActor(cur.ActorKind, cur.ActorID, a) {
		return DodWrite{Item: cur}, nil
	}
	now := s.Now()
	tx, err := s.w.Begin()
	if err != nil {
		return DodWrite{}, fmt.Errorf("store: mark dod %s: %w", id, err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE dod_item SET state=?, evidence_kind=?, evidence_ref=?, actor_kind=?, actor_id=?, updated_at=?
		WHERE id = ?`, state, kind, ref, a.Kind, a.ID, now, id)
	if err != nil {
		return DodWrite{}, fmt.Errorf("store: mark dod %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return refuse(DodRefusalItemUnknown, fmt.Sprintf("%s is not a DoD item", id))
	}
	evStr := ""
	switch {
	case state == "met":
		evStr = kind + ":" + ref
		if ev.Note != "" {
			evStr += " · note: " + ev.Note // audit text after the kind:ref ref; nothing parses past the ref
		}
	case ev.Note != "":
		evStr = "note: " + ev.Note // a non-met mark's note is its only audit text (evidence is NULL)
	}
	if err := appendDodEvent(tx, id, "mark", cur.State, state, evStr, &a.Kind, &a.ID, now); err != nil {
		return DodWrite{}, fmt.Errorf("store: mark dod %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return DodWrite{}, fmt.Errorf("store: mark dod %s: %w", id, err)
	}
	next, err := scanDodItem(s.r.QueryRow(`SELECT `+dodColumns+` FROM dod_item WHERE id = ?`, id))
	if err != nil {
		return DodWrite{}, err
	}
	return DodWrite{Item: next}, nil
}

// GetDoDItem reads one item by id (the id is globally unique — the PK —
// so the read needs no scope; the WRITE's scope is the ledger's own,
// checked at materialisation).
func (s *Store) GetDoDItem(id string) (DodItem, error) {
	return scanDodItem(s.r.QueryRow(`SELECT `+dodColumns+` FROM dod_item WHERE id = ?`, id))
}

// appendDodEvent writes the ledger's audit row (op materialize|mark|reset).
// fromState is NULL when the row did not stand before (a materialisation);
// evidence is "kind:ref" for a met state and NULL otherwise; the actor is
// the marker (NULL on a materialisation — the daemon's act, and T8's door
// keeps it out of every surface an agent reaches).
func appendDodEvent(tx *sql.Tx, itemID, op, fromState, toState, evidence string,
	actorKind, actorID *string, at string) error {
	_, err := tx.Exec(`INSERT INTO dod_event (item_id, op, from_state, to_state, evidence, actor_kind, actor_id, at)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, op, nullableStringOrNil(fromState), toState, nullableStringOrNil(evidence),
		nullableStr(actorKind), nullableStr(actorID), at)
	if err != nil {
		return fmt.Errorf("append dod event (%s): %w", op, err)
	}
	return nil
}

// nullableStringOrNil is the event's honest NULL: an empty string is the
// ABSENCE of the value (no from-state, no evidence), and the audit says
// NULL, not the empty string.
func nullableStringOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// sameActor compares a row's stored actor with the one proposing the
// mark: NULL means nobody, and nobody is not a person.
func sameActor(storedKind, storedID *string, a Actor) bool {
	if storedKind == nil || storedID == nil {
		return false
	}
	return *storedKind == a.Kind && *storedID == a.ID
}
