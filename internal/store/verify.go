// verify.go — the verify records: the shared half of S57's verification
// store (VG3, VG6, VG21). The five tables — verify_command (the gate a
// component declares), verify_command_event (its append-only audit: one row
// per changing write, same transaction, revisions never reset, no FK to the
// record), verify_run + verify_run_result (the append-only evidence: a run
// is visible only finished, with its results, in one transaction; the
// command text is SNAPSHOTTED on the result), and verify_template (the inert
// rows T10's seed fills) — are served by four files split by concern:
//
//	verify.go          the record's types, the actor door, the shared write
//	                  machinery (the insert, the event row, the snapshots,
//	                  the probes)
//	verify_command.go  the gate's life: the five writes, the reads, the
//	                  audit read, the effective set
//	verify_run.go      the evidence: the draft, the one-transaction finish,
//	                  the reads
//	verify_template.go the inert templates and the all-or-nothing apply
//
// # The door it is (VG23)
//
// A gate command is authority — a command a loop will run — and is
// human-authored: a direct write takes a store.Actor of kind user or cli and
// refuses anything else BEFORE any SQL (an agent PROPOSES, T8's proposal
// kind, and the human commits it; the store's refusal is the backstop the
// daemon door forgets). The schema's CHECKs are the net this is the door.
//
// # What the store does NOT do
//
// It never runs a gate (T4's runner is the only spawner), never decides a
// merge (T5's pure evaluator does), never routes (T6's engine does), and
// never reads a tail out of a result row into an event, a bug body or an
// agent-visible list (VG6: only the T8 read returns tails). SQL for these
// tables lives only in this package (I2). No verify_command row is ever
// seeded (VG21); verify_template is empty until T10.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// --- the record ------------------------------------------------------------

// VerifyCommand is one row of verify_command (VG3). MemoryMB is a
// DECLARATION, not an enforcement (v1): every view says so.
type VerifyCommand struct {
	ID          string
	ComponentID string
	Name        string
	Command     string
	Requirement string
	TimeoutS    int
	MemoryMB    *int
	Position    int
	Enabled     bool
	Origin      string
	TemplateID  *string
	ArchivedAt  *string
	CreatedAt   string
	UpdatedAt   string
}

// NewVerifyCommand is the caller's request to add a gate (VG3). Position nil
// means "after the component's current last gate"; Enabled nil means true;
// Origin is "user" for a direct write — "template" rows arrive only through
// ApplyVerifyTemplates, which sets it itself.
type NewVerifyCommand struct {
	ComponentID string
	Name        string
	Command     string
	Requirement string
	TimeoutS    int
	MemoryMB    *int
	Position    *int
	Enabled     *bool
}

// VerifyCommandUpdate is a partial update: only the non-nil fields change.
// The name is the gate's identity and is never rewritten (rename is an
// archive plus an add — the honest history). MemoryMB non-nil and ≤ 0 is a
// refusal; nil means leave it.
type VerifyCommandUpdate struct {
	Command     *string
	Requirement *string
	TimeoutS    *int
	MemoryMB    *int
	Position    *int
	Enabled     *bool
}

// VerifyCommandFilter is the list read (verify.list).
type VerifyCommandFilter struct {
	ComponentID     string
	IncludeArchived bool
	IncludeDisabled bool
}

// VerifyRefusal names why a verify write was refused — the components.go
// shape (DB19: a reason is a discriminated result, not an error), and the
// caller renders the sentence verbatim.
type VerifyRefusal string

const (
	// VerifyRefusalNone means the write was accepted.
	VerifyRefusalNone VerifyRefusal = ""
	// VerifyRefusalActorNotHuman — a direct write is a person's act.
	VerifyRefusalActorNotHuman VerifyRefusal = "actor-not-human"
	// VerifyRefusalComponentUnknown — the named component is not a component.
	VerifyRefusalComponentUnknown VerifyRefusal = "component-unknown"
	// VerifyRefusalComponentArchived — gates of an archived node are frozen.
	VerifyRefusalComponentArchived VerifyRefusal = "component-archived"
	// VerifyRefusalNameNotASlug — the name is not a validated slug.
	VerifyRefusalNameNotASlug VerifyRefusal = "name-not-a-slug"
	// VerifyRefusalNameTaken — the name is used by a live gate of the node.
	VerifyRefusalNameTaken VerifyRefusal = "name-taken"
	// VerifyRefusalCommandEmpty — a gate with no command runs nothing.
	VerifyRefusalCommandEmpty VerifyRefusal = "command-empty"
	// VerifyRefusalCommandTooLong — over the 4096-byte table bound (VG3).
	VerifyRefusalCommandTooLong VerifyRefusal = "command-too-long"
	// VerifyRefusalCommandNUL — a command cannot carry a NUL.
	VerifyRefusalCommandNUL VerifyRefusal = "command-nul"
	// VerifyRefusalTimeoutOutOfRange — outside the 1..600 s closed range.
	VerifyRefusalTimeoutOutOfRange VerifyRefusal = "timeout-out-of-range"
	// VerifyRefusalRequirementInvalid — not required|advisory.
	VerifyRefusalRequirementInvalid VerifyRefusal = "requirement-invalid"
	// VerifyRefusalMemoryNonPositive — a memory of ≤ 0 is not a declaration.
	VerifyRefusalMemoryNonPositive VerifyRefusal = "memory-non-positive"
	// VerifyRefusalCommandUnknown — an update/archive/restore named a row
	// that does not exist.
	VerifyRefusalCommandUnknown VerifyRefusal = "command-unknown"
	// VerifyRefusalCommandArchived — an update on an archived gate (restore
	// it first; history is not edited in place).
	VerifyRefusalCommandArchived VerifyRefusal = "command-archived"
	// VerifyRefusalTemplateUnknown — an apply named a template that is not
	// a template (or an archived one).
	VerifyRefusalTemplateUnknown VerifyRefusal = "template-unknown"
	// VerifyRefusalPositionOutOfRange — a negative position.
	VerifyRefusalPositionOutOfRange VerifyRefusal = "position-out-of-range"
	// VerifyRefusalTemplateCollision — a template's name is already taken by
	// a live gate of the node; every collision is reported and nothing is
	// written (all or nothing).
	VerifyRefusalTemplateCollision VerifyRefusal = "template-name-collision"
)

// VerifyWrite is the outcome of a gate write.
type VerifyWrite struct {
	Command VerifyCommand
	Refusal VerifyRefusal
	Reason  string
}

// OK reports whether the write was accepted.
func (w VerifyWrite) OK() bool { return w.Refusal == VerifyRefusalNone }

// refuseVerify builds the refused result from a finished sentence — the
// sentence is NOT interpreted as a format (it may legitimately contain a
// %), so the one that needs interpolation is refuseVerifyf.
func refuseVerify(r VerifyRefusal, sentence string) VerifyWrite {
	return VerifyWrite{Refusal: r, Reason: sentence}
}

// refuseVerifyf is refuseVerify with a printf-style sentence, for the
// refusals that name the thing they refused.
func refuseVerifyf(r VerifyRefusal, format string, args ...any) VerifyWrite {
	return VerifyWrite{Refusal: r, Reason: fmt.Sprintf(format, args...)}
}

// verifyActor answers the VG23 door: a gate write admits a person (user) or
// the cli direct door, and nothing else. An anonymous actor and any agent or
// trigger kind are refused before any SQL — a gate is authority, and
// authority over a loop's commands is not something a loop's agent takes
// for itself (it proposes; a human commits).
func (a Actor) verifyActorRefusal() (VerifyRefusal, string) {
	if a.ID == "" {
		return VerifyRefusalActorNotHuman, "refused: a gate write needs a named actor — an anonymous author cannot be audited (VG23)"
	}
	if a.Kind == "user" || a.Kind == "cli" {
		return VerifyRefusalNone, ""
	}
	return VerifyRefusalActorNotHuman, fmt.Sprintf("refused: only a person may write a gate · caller kind %q proposes, it does not author (VG23)", a.Kind)
}

// isGateSlug is the name's closed shape: lowercase alphanumerics and hyphens,
// 1..64. The migration's CHECK (name NOT GLOB '*[^a-z0-9-]*' + the length
// bound) is the backstop a store bypassed; this is the door, and it is the
// same shape the event summary may quote (SEC-033).
func isGateSlug(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// gateSlugOK is the requirement's closed set (VG3).
func requirementOK(r string) bool { return r == "required" || r == "advisory" }

// timeoutOK is the 1..600 s closed range (VG3: the bash ceiling).
func timeoutOK(t int) bool { return t >= 1 && t <= 600 }

// eventSnapshot is the canonical JSON a change row carries in before_json /
// after_json — ids and values only, field order fixed by the struct, so the
// audit is diffable and never carries a writer's formatting.
type eventSnapshot struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Requirement string `json:"requirement"`
	TimeoutS    int    `json:"timeout_s"`
	MemoryMB    *int   `json:"memory_mb"`
	Position    int    `json:"position"`
	Enabled     bool   `json:"enabled"`
	Archived    bool   `json:"archived"`
}

func (c VerifyCommand) snapshot() eventSnapshot {
	return eventSnapshot{
		Name:        c.Name,
		Command:     c.Command,
		Requirement: c.Requirement,
		TimeoutS:    c.TimeoutS,
		MemoryMB:    c.MemoryMB,
		Position:    c.Position,
		Enabled:     c.Enabled,
		Archived:    c.ArchivedAt != nil,
	}
}

func snapshotJSON(c VerifyCommand) string {
	raw, err := json.Marshal(c.snapshot())
	if err != nil {
		// A fixed struct of strings and numbers cannot fail to marshal;
		// failing here is a broken store, not a condition to handle.
		panic("store: verify event snapshot: " + err.Error())
	}
	return string(raw)
}

// eventSummary is the fixed-template audit sentence (SEC-033): built ONLY
// from the op, the validated slug and — for the add — the enums, never from
// the command text (which is user text that lives on the row, not in the
// audit) or from any note.
func eventSummary(op, name, requirement string, timeout int) string {
	switch op {
	case "add":
		return fmt.Sprintf("add gate %q (%s, %d s)", name, requirement, timeout)
	case "apply_template":
		return fmt.Sprintf("apply template as gate %q", name)
	default: // update, archive, restore
		return fmt.Sprintf("%s gate %q", op, name)
	}
}

const verifyCommandColumns = `id, component_id, name, command, requirement, timeout_s, memory_mb,
	position, enabled, origin, template_id, archived_at, created_at, updated_at`

func scanVerifyCommand(row interface{ Scan(...any) error }) (VerifyCommand, error) {
	var (
		c          VerifyCommand
		memoryMB   sql.NullInt64
		enabled    int
		templateID sql.NullString
		archivedAt sql.NullString
	)
	err := row.Scan(&c.ID, &c.ComponentID, &c.Name, &c.Command, &c.Requirement, &c.TimeoutS, &memoryMB,
		&c.Position, &enabled, &c.Origin, &templateID, &archivedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VerifyCommand{}, fmt.Errorf("store: verify command: %w", ErrNotFound)
		}
		return VerifyCommand{}, fmt.Errorf("store: read verify command: %w", err)
	}
	c.Enabled = enabled == 1
	if memoryMB.Valid {
		v := int(memoryMB.Int64)
		c.MemoryMB = &v
	}
	c.TemplateID = nullStringPtr(templateID)
	c.ArchivedAt = nullStringPtr(archivedAt)
	return c, nil
}

// componentState probes the component a gate write names: (exists, archived,
// error). An unknown component is a refusal, not an error (DB19); a read
// failure IS an error.
func (s *Store) componentState(id string) (exists, archived bool, err error) {
	var (
		archivedAt sql.NullString
	)
	err = s.r.QueryRow(`SELECT archived_at FROM component WHERE id = ?`, id).Scan(&archivedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("store: probe component %s: %w", id, err)
	}
	return true, archivedAt.Valid, nil
}

func insertVerifyCommand(tx *sql.Tx, g VerifyCommand) error {
	enabled := 0
	if g.Enabled {
		enabled = 1
	}
	_, err := tx.Exec(`INSERT INTO verify_command (id, component_id, name, command, requirement, timeout_s, memory_mb,
		position, enabled, origin, template_id, archived_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.ComponentID, g.Name, g.Command, g.Requirement, g.TimeoutS, nullableInt(g.MemoryMB),
		g.Position, enabled, g.Origin, nullableStr(g.TemplateID), nullableStr(g.ArchivedAt), g.CreatedAt, g.UpdatedAt)
	return err
}

// appendGateEvent writes the change row beside the state change, same
// transaction (P3): revision is the per-command number, never reset;
// before/after are canonical snapshots (nil where the row does not exist on
// that side — the add has no before, the archive no after); at carries the
// SAME store-clock reading as the state change it audits.
func appendGateEvent(tx *sql.Tx, a Actor, g VerifyCommand, revision int, op string, at string, before, after *VerifyCommand) error {
	var beforeJSON, afterJSON any
	if before != nil {
		b := snapshotJSON(*before)
		beforeJSON = &b
	}
	if after != nil {
		aJSON := snapshotJSON(*after)
		afterJSON = &aJSON
	}
	_, err := tx.Exec(`INSERT INTO verify_command_event (command_id, component_id, revision, op, actor_kind, actor_id, summary, before_json, after_json, at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.ComponentID, revision, op, a.Kind, a.ID, eventSummary(op, g.Name, g.Requirement, g.TimeoutS),
		beforeJSON, afterJSON, at)
	return err
}

// liveGateNameTaken reports whether the component has a non-archived gate
// with the name (the partial unique index's Go half).
func (s *Store) liveGateNameTaken(componentID, name string) bool {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM verify_command WHERE component_id = ? AND name = ? AND archived_at IS NULL`,
		componentID, name).Scan(&n); err != nil {
		// A probe that cannot read is a broken store; the write must not
		// proceed on a guess.
		panic("store: probe gate name: " + err.Error())
	}
	return n > 0
}

// nextGatePosition is one past the component's highest live position (the
// add's default: gates run in position order, VG4).
func (s *Store) nextGatePosition(componentID string) (int, error) {
	var n sql.NullInt64
	if err := s.r.QueryRow(`SELECT MAX(position) FROM verify_command WHERE component_id = ? AND archived_at IS NULL`,
		componentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: next position of %s: %w", componentID, err)
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64) + 1, nil
}

// nextGateRevision is the per-command revision number the next event row
// takes: one past the command's highest recorded revision (0 when it has
// none). The number never resets (P3) — it is the audit's own ordering.
func (s *Store) nextGateRevision(commandID string) (int, error) {
	var n sql.NullInt64
	if err := s.r.QueryRow(`SELECT MAX(revision) FROM verify_command_event WHERE command_id = ?`, commandID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: revision of gate %s: %w", commandID, err)
	}
	if !n.Valid {
		return 1, nil
	}
	return int(n.Int64) + 1, nil
}

// VerifyCommandEvent is one row of verify_command_event — the append-only
// audit, one row per changing write (P3).
type VerifyCommandEvent struct {
	ID          int64
	CommandID   string
	ComponentID string
	Revision    int
	Op          string
	ActorKind   string
	ActorID     string
	Summary     string
	BeforeJSON  *string
	AfterJSON   *string
	At          string
}

// VerifyCommandEventFilter is the audit read (verify.events).
type VerifyCommandEventFilter struct {
	CommandID   string
	ComponentID string
	Limit       int
}
