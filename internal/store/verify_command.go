// verify_command.go — the gate's life: the five writes, the reads, the
// audit read and the effective set, over verify_command and
// verify_command_event. The types and the shared write machinery are in
// verify.go; the record they serve is documented there.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AddVerifyCommand adds one gate and its event row in one transaction (VG3,
// P3). A refused write answers a VerifyWrite with the refusal and writes
// NOTHING — no command row, no event row. The event's summary is a fixed
// template from the slug and the enums, never the command text (SEC-033).
func (s *Store) AddVerifyCommand(a Actor, in NewVerifyCommand) (VerifyWrite, error) {
	if r, why := a.verifyActorRefusal(); r != VerifyRefusalNone {
		return refuseVerify(r, why), nil
	}
	exists, archived, err := s.componentState(in.ComponentID)
	if err != nil {
		return VerifyWrite{}, err
	}
	if !exists {
		return refuseVerifyf(VerifyRefusalComponentUnknown,
			"refused: %s is not a component — a gate needs a node it can sit on", in.ComponentID), nil
	}
	if archived {
		return refuseVerifyf(VerifyRefusalComponentArchived,
			"refused: %s is archived — its gates cannot be written", in.ComponentID), nil
	}
	if !isGateSlug(in.Name) {
		return refuseVerifyf(VerifyRefusalNameNotASlug,
			"refused: %q is not a gate name — a slug is lowercase letters, digits and hyphens, 1 to 64", in.Name), nil
	}
	if s.liveGateNameTaken(in.ComponentID, in.Name) {
		return refuseVerifyf(VerifyRefusalNameTaken,
			"refused: %s already has a live gate named %q (archive it first)", in.ComponentID, in.Name), nil
	}
	if in.Command == "" {
		return refuseVerifyf(VerifyRefusalCommandEmpty, "refused: a gate with an empty command runs nothing"), nil
	}
	if len(in.Command) > 4096 {
		return refuseVerifyf(VerifyRefusalCommandTooLong,
			"refused: the command is %d bytes; the table bound is 4096", len(in.Command)), nil
	}
	if strings.ContainsRune(in.Command, 0) {
		return refuseVerifyf(VerifyRefusalCommandNUL, "refused: a gate command may not contain a NUL"), nil
	}
	if !requirementOK(in.Requirement) {
		return refuseVerifyf(VerifyRefusalRequirementInvalid,
			"refused: the requirement is %q; the closed set is required|advisory", in.Requirement), nil
	}
	if !timeoutOK(in.TimeoutS) {
		return refuseVerifyf(VerifyRefusalTimeoutOutOfRange,
			"refused: the timeout is %d s; the closed range is 1 to 600", in.TimeoutS), nil
	}
	if in.MemoryMB != nil && *in.MemoryMB <= 0 {
		return refuseVerifyf(VerifyRefusalMemoryNonPositive,
			"refused: the declared memory is %d MiB; a declaration is positive or absent", *in.MemoryMB), nil
	}

	now := s.Now()
	g := VerifyCommand{
		ID:          "vc-" + newID(),
		ComponentID: in.ComponentID,
		Name:        in.Name,
		Command:     in.Command,
		Requirement: in.Requirement,
		TimeoutS:    in.TimeoutS,
		MemoryMB:    in.MemoryMB,
		Origin:      "user",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if in.Position != nil {
		g.Position = *in.Position
	} else {
		pos, err := s.nextGatePosition(in.ComponentID)
		if err != nil {
			return VerifyWrite{}, err
		}
		g.Position = pos
	}
	if in.Enabled != nil {
		g.Enabled = *in.Enabled
	} else {
		g.Enabled = true
	}

	tx, err := s.w.Begin()
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: add gate %s: %w", g.ID, err)
	}
	defer tx.Rollback()
	if err := insertVerifyCommand(tx, g); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: add gate %s: %w", g.ID, err)
	}
	if err := appendGateEvent(tx, a, g, 1, "add", now, nil, &g); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: add gate %s: %w", g.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: add gate %s: %w", g.ID, err)
	}
	return VerifyWrite{Command: g}, nil
}

// GetVerifyCommand reads one gate by id. Absent is ErrNotFound.
func (s *Store) GetVerifyCommand(id string) (VerifyCommand, error) {
	return scanVerifyCommand(s.r.QueryRow(`SELECT `+verifyCommandColumns+` FROM verify_command WHERE id = ?`, id))
}

// ListVerifyCommands reads gates of a component (or all, with an empty
// filter) in run order — position, then name — with archived and disabled
// rows excluded by default (a listing is about what runs and what could run,
// and both questions have an explicit include for the rest).
func (s *Store) ListVerifyCommands(f VerifyCommandFilter) ([]VerifyCommand, error) {
	var where []string
	var args []any
	if f.ComponentID != "" {
		where = append(where, `component_id = ?`)
		args = append(args, f.ComponentID)
	}
	if !f.IncludeArchived {
		where = append(where, `archived_at IS NULL`)
	}
	if !f.IncludeDisabled {
		where = append(where, `enabled = 1`)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := s.r.Query(`SELECT `+verifyCommandColumns+` FROM verify_command`+clause+` ORDER BY position, name`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list gates: %w", err)
	}
	defer rows.Close()
	var out []VerifyCommand
	for rows.Next() {
		g, err := scanVerifyCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list gates: %w", err)
	}
	return out, nil
}

// --- the effective set (VG3: one level, replace-not-merge) -------------------

// EffectiveGate is one row of the answer with the node that supplied it.
type EffectiveGate struct {
	Command VerifyCommand
	Source  string // "own" | "root"
}

// EffectiveGates is the answer to "what runs here, and from where" (VG3).
// SourceID names the supplying node (the component itself, its root, or
// empty when the set is empty); Source is the kind of supply. A child with
// its own live gates runs exactly those (replace, not merge); a child with
// none runs its root's; a root has only its own; an archived node — child or
// root — supplies nothing and takes nothing (an archived root's gates do not
// reach its children: the archive froze them).
type EffectiveGates struct {
	ComponentID string
	SourceID    string
	Source      string
	Gates       []EffectiveGate
}

// EffectiveGates answers VG3 for one node: the node's own non-archived
// enabled rows if it has any (source own); else, for a child whose root is
// not archived, the root's (source root); else the empty set. Unknown
// component is an error (a read that cannot resolve its target is not an
// empty answer); an archived node is an honest empty set.
func (s *Store) EffectiveGates(componentID string) (EffectiveGates, error) {
	out := EffectiveGates{ComponentID: componentID}
	var (
		parentID sql.NullString
		archived sql.NullString
	)
	err := s.r.QueryRow(`SELECT parent_id, archived_at FROM component WHERE id = ?`, componentID).Scan(&parentID, &archived)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return out, fmt.Errorf("store: effective gates of %s: %w", componentID, ErrNotFound)
		}
		return out, fmt.Errorf("store: effective gates of %s: %w", componentID, err)
	}
	if archived.Valid {
		return out, nil // an archived node supplies nothing and takes nothing
	}
	own, err := s.liveGatesOf(componentID)
	if err != nil {
		return out, err
	}
	if len(own) > 0 {
		out.SourceID = componentID
		out.Source = "own"
		for _, g := range own {
			out.Gates = append(out.Gates, EffectiveGate{Command: g, Source: "own"})
		}
		return out, nil
	}
	if parentID.Valid && parentID.String != "" {
		var rootArchived sql.NullString
		err := s.r.QueryRow(`SELECT archived_at FROM component WHERE id = ?`, parentID.String).Scan(&rootArchived)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return out, fmt.Errorf("store: effective gates of %s: root %s vanished: %w", componentID, parentID.String, ErrNotFound)
			}
			return out, fmt.Errorf("store: effective gates of %s: %w", componentID, err)
		}
		if rootArchived.Valid {
			return out, nil // an archived root's gates do not reach its children
		}
		rootGates, err := s.liveGatesOf(parentID.String)
		if err != nil {
			return out, err
		}
		out.SourceID = parentID.String
		out.Source = "root"
		for _, g := range rootGates {
			out.Gates = append(out.Gates, EffectiveGate{Command: g, Source: "root"})
		}
	}
	return out, nil
}

// liveGatesOf reads the node's own non-archived enabled rows in run order.
func (s *Store) liveGatesOf(componentID string) ([]VerifyCommand, error) {
	rows, err := s.r.Query(`SELECT `+verifyCommandColumns+` FROM verify_command
		WHERE component_id = ? AND archived_at IS NULL AND enabled = 1
		ORDER BY position, name`, componentID)
	if err != nil {
		return nil, fmt.Errorf("store: live gates of %s: %w", componentID, err)
	}
	defer rows.Close()
	var out []VerifyCommand
	for rows.Next() {
		g, err := scanVerifyCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: live gates of %s: %w", componentID, err)
	}
	return out, nil
}

// --- the changing writes: update, archive, restore ---------------------------

// UpdateVerifyCommand changes the named gate and writes its event row in the
// same transaction (P3). A no-op — nothing actually changes — writes nothing
// (no-ops write nothing) and returns the row unchanged. The name is
// immutable; renaming is an archive plus an add.
func (s *Store) UpdateVerifyCommand(a Actor, id string, u VerifyCommandUpdate) (VerifyWrite, error) {
	if r, why := a.verifyActorRefusal(); r != VerifyRefusalNone {
		return refuseVerify(r, why), nil
	}
	cur, err := s.GetVerifyCommand(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return refuseVerifyf(VerifyRefusalCommandUnknown, "refused: %s is not a gate", id), nil
		}
		return VerifyWrite{}, err
	}
	if cur.ArchivedAt != nil {
		return refuseVerifyf(VerifyRefusalCommandArchived,
			"refused: %s is archived — history is not edited in place; restore it first", id), nil
	}
	exists, archived, err := s.componentState(cur.ComponentID)
	if err != nil {
		return VerifyWrite{}, err
	}
	if !exists {
		return refuseVerifyf(VerifyRefusalComponentUnknown, "refused: the gate's component %s is gone", cur.ComponentID), nil
	}
	if archived {
		return refuseVerifyf(VerifyRefusalComponentArchived,
			"refused: the gate's component %s is archived", cur.ComponentID), nil
	}

	next := cur
	changed := false
	if u.Command != nil && *u.Command != cur.Command {
		if *u.Command == "" {
			return refuseVerifyf(VerifyRefusalCommandEmpty, "refused: a gate with an empty command runs nothing"), nil
		}
		if len(*u.Command) > 4096 {
			return refuseVerifyf(VerifyRefusalCommandTooLong, "refused: the command is %d bytes; the table bound is 4096", len(*u.Command)), nil
		}
		if strings.ContainsRune(*u.Command, 0) {
			return refuseVerifyf(VerifyRefusalCommandNUL, "refused: a gate command may not contain a NUL"), nil
		}
		next.Command = *u.Command
		changed = true
	}
	if u.Requirement != nil && *u.Requirement != cur.Requirement {
		if !requirementOK(*u.Requirement) {
			return refuseVerifyf(VerifyRefusalRequirementInvalid,
				"refused: the requirement is %q; the closed set is required|advisory", *u.Requirement), nil
		}
		next.Requirement = *u.Requirement
		changed = true
	}
	if u.TimeoutS != nil && *u.TimeoutS != cur.TimeoutS {
		if !timeoutOK(*u.TimeoutS) {
			return refuseVerifyf(VerifyRefusalTimeoutOutOfRange, "refused: the timeout is %d s; the closed range is 1 to 600", *u.TimeoutS), nil
		}
		next.TimeoutS = *u.TimeoutS
		changed = true
	}
	if u.MemoryMB != nil {
		if *u.MemoryMB <= 0 {
			// The same rule as the add (the spec's "memory_mb ≤ 0"): a
			// declaration is positive or absent — and absent is written by
			// the add, not reached through a zero here.
			return refuseVerifyf(VerifyRefusalMemoryNonPositive, "refused: the declared memory is %d MiB; a declaration is positive or absent", *u.MemoryMB), nil
		}
		if cur.MemoryMB == nil || *cur.MemoryMB != *u.MemoryMB {
			next.MemoryMB = u.MemoryMB
			changed = true
		}
	}
	if u.Position != nil && *u.Position != cur.Position {
		if *u.Position < 0 {
			return refuseVerifyf(VerifyRefusalPositionOutOfRange, "refused: the position is %d; positions are non-negative", *u.Position), nil
		}
		next.Position = *u.Position
		changed = true
	}
	if u.Enabled != nil && *u.Enabled != cur.Enabled {
		next.Enabled = *u.Enabled
		changed = true
	}
	if !changed {
		return VerifyWrite{Command: cur}, nil // a no-op: no row
	}

	now := s.Now()
	next.UpdatedAt = now
	revision, err := s.nextGateRevision(id)
	if err != nil {
		return VerifyWrite{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: update gate %s: %w", id, err)
	}
	defer tx.Rollback()
	// A guarded UPDATE: re-check the id in the WHERE so a row archived by a
	// concurrent writer between the read and this write updates nothing and
	// the event (written below only if the UPDATE matched) stays honest.
	res, err := tx.Exec(`UPDATE verify_command SET command=?, requirement=?, timeout_s=?, memory_mb=?, position=?, enabled=?, updated_at=?
		WHERE id = ? AND archived_at IS NULL`,
		next.Command, next.Requirement, next.TimeoutS, nullableInt(next.MemoryMB), next.Position,
		boolInt(next.Enabled), now, id)
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: update gate %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: update gate %s: %w", id, err)
	}
	if n != 1 {
		// It moved from under us: the read said live, the write found it not.
		// That is the archived refusal, not an error — the row exists, it is
		// just not one an update may touch.
		return refuseVerifyf(VerifyRefusalCommandArchived,
			"refused: %s is archived — history is not edited in place; restore it first", id), nil
	}
	if err := appendGateEvent(tx, a, next, revision, "update", now, &cur, &next); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: update gate %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: update gate %s: %w", id, err)
	}
	return VerifyWrite{Command: next}, nil
}

// ListVerifyCommandEvents reads the append-only audit of one gate (or one
// node's whole audit) in revision order — the order the story was told in.
func (s *Store) ListVerifyCommandEvents(f VerifyCommandEventFilter) ([]VerifyCommandEvent, error) {
	var where []string
	var args []any
	if f.CommandID != "" {
		where = append(where, `command_id = ?`)
		args = append(args, f.CommandID)
	}
	if f.ComponentID != "" {
		where = append(where, `component_id = ?`)
		args = append(args, f.ComponentID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	query := `SELECT id, command_id, component_id, revision, op, actor_kind, actor_id, summary, before_json, after_json, at
		FROM verify_command_event` + clause + ` ORDER BY command_id, revision`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.r.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read gate events: %w", err)
	}
	defer rows.Close()
	var out []VerifyCommandEvent
	for rows.Next() {
		var (
			e          VerifyCommandEvent
			beforeJSON sql.NullString
			afterJSON  sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.CommandID, &e.ComponentID, &e.Revision, &e.Op, &e.ActorKind, &e.ActorID, &e.Summary, &beforeJSON, &afterJSON, &e.At); err != nil {
			return nil, fmt.Errorf("store: read gate events: %w", err)
		}
		e.BeforeJSON = nullStringPtr(beforeJSON)
		e.AfterJSON = nullStringPtr(afterJSON)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read gate events: %w", err)
	}
	return out, nil
}

// ArchiveVerifyCommand sets archived_at once and writes its event row in the
// same transaction (P3). Archiving is the only "delete" the record admits
// (Do NOT: no DELETE) — the name frees up for a live gate, the audit keeps
// the row's whole story. Already archived is a no-op that writes nothing and
// returns the row unchanged.
func (s *Store) ArchiveVerifyCommand(a Actor, id string) (VerifyWrite, error) {
	if r, why := a.verifyActorRefusal(); r != VerifyRefusalNone {
		return refuseVerify(r, why), nil
	}
	cur, err := s.GetVerifyCommand(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return refuseVerifyf(VerifyRefusalCommandUnknown, "refused: %s is not a gate", id), nil
		}
		return VerifyWrite{}, err
	}
	if cur.ArchivedAt != nil {
		return VerifyWrite{Command: cur}, nil // already archived: no row
	}
	now := s.Now()
	next := cur
	next.ArchivedAt = &now
	next.UpdatedAt = now
	revision, err := s.nextGateRevision(id)
	if err != nil {
		return VerifyWrite{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: archive gate %s: %w", id, err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE verify_command SET archived_at = ?, updated_at = ? WHERE id = ? AND archived_at IS NULL`, now, now, id)
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: archive gate %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return refuseVerifyf(VerifyRefusalCommandArchived, "refused: %s is already archived", id), nil
	}
	if err := appendGateEvent(tx, a, next, revision, "archive", now, &cur, nil); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: archive gate %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: archive gate %s: %w", id, err)
	}
	return VerifyWrite{Command: next}, nil
}

// RestoreVerifyCommand clears archived_at and writes its event row in the
// same transaction (P3). The name is re-checked: a gate that was archived so
// someone else could take its name does not come back over the new holder
// (the partial unique index is the backstop; this refusal is the door).
// Not archived is a no-op that writes nothing and returns the row unchanged.
func (s *Store) RestoreVerifyCommand(a Actor, id string) (VerifyWrite, error) {
	if r, why := a.verifyActorRefusal(); r != VerifyRefusalNone {
		return refuseVerify(r, why), nil
	}
	cur, err := s.GetVerifyCommand(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return refuseVerifyf(VerifyRefusalCommandUnknown, "refused: %s is not a gate", id), nil
		}
		return VerifyWrite{}, err
	}
	if cur.ArchivedAt == nil {
		return VerifyWrite{Command: cur}, nil // not archived: no row
	}
	exists, archived, err := s.componentState(cur.ComponentID)
	if err != nil {
		return VerifyWrite{}, err
	}
	if !exists {
		return refuseVerifyf(VerifyRefusalComponentUnknown, "refused: the gate's component %s is gone", cur.ComponentID), nil
	}
	if archived {
		return refuseVerifyf(VerifyRefusalComponentArchived,
			"refused: the gate's component %s is archived — restore the component first", cur.ComponentID), nil
	}
	if s.liveGateNameTaken(cur.ComponentID, cur.Name) {
		return refuseVerifyf(VerifyRefusalNameTaken,
			"refused: %s already has a live gate named %q — the archived row's name is taken", cur.ComponentID, cur.Name), nil
	}
	now := s.Now()
	next := cur
	next.ArchivedAt = nil
	next.UpdatedAt = now
	revision, err := s.nextGateRevision(id)
	if err != nil {
		return VerifyWrite{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: restore gate %s: %w", id, err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE verify_command SET archived_at = NULL, updated_at = ? WHERE id = ? AND archived_at IS NOT NULL`, now, id)
	if err != nil {
		return VerifyWrite{}, fmt.Errorf("store: restore gate %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return VerifyWrite{Command: cur}, nil // raced: already restored, no row
	}
	if err := appendGateEvent(tx, a, next, revision, "restore", now, &cur, &next); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: restore gate %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return VerifyWrite{}, fmt.Errorf("store: restore gate %s: %w", id, err)
	}
	return VerifyWrite{Command: next}, nil
}
