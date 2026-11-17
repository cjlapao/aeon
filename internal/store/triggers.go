package store

// triggers.go — S40 AU1/AU7/AU12: the event triggers beside S20's clock
// tables (the SQL here spells every table event_trigger / event_trigger_fire,
// never S20's trigger_*), and their fire ledger. Three properties this file
// owns: triggers are written by a person and only a person (AU14's door in
// Go, ahead of any SQL, with 0039's created_by_kind CHECK as its twin); a
// fire's undecided state IS the claim (AU8 — outcome NULL means claimed, and
// deciding is claimed-FROM); and nothing here reads the store clock except
// authoring — the dispatcher injects time into every fire function, so the
// ledger records when the dispatcher believed it acted.
//
// Triggers are never deleted (AU1): disabling with a reason is the whole
// lifecycle, so the ledger of what a person stood behind stays intact.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrActorNotHuman: a trigger write asked for by a non-person. Returned
// before s.w.Begin — the table cannot have moved, and the tests hold that
// promise by counting rows after the refusal.
var ErrActorNotHuman = errors.New("store: triggers are written by a person")

// requireUser is AU1's door. It does not trust Actor.human()'s untrimmed ID
// — a trigger authored by user " " is authored by nobody.
func requireUser(a Actor) error {
	if a.Kind != "user" || strings.TrimSpace(a.ID) == "" {
		return fmt.Errorf("%w; this actor is %q", ErrActorNotHuman, a.Kind)
	}
	return nil
}

// Trigger is an event_trigger row.
type Trigger struct {
	ID, Name, Source, RootID, ComponentID, MatchJSON, Blueprint, Kickoff string
	MaxRunsPerHour                                                       int
	MaxCostPerDayUSD                                                     *float64
	Enabled                                                              bool
	DisabledReason                                                       string
	FromOutboxID                                                         int64
	CreatedByKind, CreatedByID, CreatedAt, UpdatedAt                     string
	// ArchivedAt is "" when live. Archive (S49 AN6) is a state on top of
	// enabled/paused, never a delete: an archived trigger answers
	// skipped(disabled) through the ordinary paths and is hidden from the
	// operator lists unless they ask for it. omitempty keeps every S40
	// golden byte-identical for live rows — the archive appears only in
	// S49's own views.
	ArchivedAt     string `json:"ArchivedAt,omitempty"`
	ArchivedReason string `json:"ArchivedReason,omitempty"`
}

// Match parses the trigger's stored rule. A parse error here is a broken
// row, not a broken caller — the write path only ever stores Canonical()
// output, and the CHECK on json_valid guards the rest.
func (t Trigger) Match() (Match, error) { return ParseMatch([]byte(t.MatchJSON)) }

// Admits is scope AND match: a trigger scoped to a project or component
// admits only events that name the same one (” scope columns admit
// anything), and then the match's own rules. A rule that cannot be parsed
// admits nothing — fail closed, never fire on a guess.
func (t Trigger) Admits(ev AutomationEvent) bool {
	if t.RootID != "" && ev.RootID != t.RootID {
		return false
	}
	if t.ComponentID != "" && ev.ComponentID != t.ComponentID {
		return false
	}
	m, err := t.Match()
	if err != nil {
		return false
	}
	return m.Matches(ev)
}

// NewEventTrigger is the create input. FromOutboxID is not here: the store
// stamps it at birth (the trigger starts from the outbox's head, never
// dispatching history nobody authorised — AU8).
type NewEventTrigger struct {
	Name, RootID, ComponentID string
	Match                     Match
	Blueprint, Kickoff        string
	MaxRunsPerHour            int
	MaxCostPerDayUSD          *float64
}

// EventTriggerEdit is the edit input: pointers carry "was given", so an
// absent field keeps its value (the §4c discipline again — clearing the cost
// cap is its own intention, ClearMaxCost, never an empty string).
type EventTriggerEdit struct {
	Name, Blueprint, Kickoff *string
	Match                    *Match
	MaxRunsPerHour           *int
	MaxCostPerDayUSD         *float64
	ClearMaxCost             bool
}

// EventTriggerFilter narrows the listing.
type EventTriggerFilter struct {
	RootID, ComponentID string
	Enabled             *bool
	// IncludeArchived is S49 AN6: archived is a state, not a row's death,
	// and the operator lists hide it unless the caller asks. The
	// dispatcher's candidate read (ListEventTriggersFor) never filters on
	// it — an archived trigger answers skipped(disabled) like any disabled
	// one, and a silent skip is AU12's could-not-say dressed as an answer.
	IncludeArchived bool
}

const triggerColumns = `id, name, source, root_id, component_id, match_json, blueprint, kickoff,
	max_runs_per_hour, COALESCE(max_cost_per_day_usd, -1), enabled, disabled_reason, from_outbox_id,
	created_by_kind, created_by_id, created_at, updated_at,
	COALESCE(archived_at,''), archived_reason`

func scanTrigger(scan func(dest ...any) error) (Trigger, error) {
	var t Trigger
	var cost float64
	var enabled int
	if err := scan(&t.ID, &t.Name, &t.Source, &t.RootID, &t.ComponentID, &t.MatchJSON, &t.Blueprint, &t.Kickoff,
		&t.MaxRunsPerHour, &cost, &enabled, &t.DisabledReason, &t.FromOutboxID,
		&t.CreatedByKind, &t.CreatedByID, &t.CreatedAt, &t.UpdatedAt,
		&t.ArchivedAt, &t.ArchivedReason); err != nil {
		return Trigger{}, err
	}
	if cost >= 0 {
		t.MaxCostPerDayUSD = &cost
	}
	t.Enabled = enabled == 1
	return t, nil
}

// CreateEventTrigger files a standing human instruction (AU1). The name is
// unique, the match is stored in canonical form, and from_outbox_id is read
// inside the same transaction the row is written in — a trigger born while
// events are flowing starts beside the newest one, never before it.
func (s *Store) CreateEventTrigger(a Actor, in NewEventTrigger) (Trigger, error) {
	if err := requireUser(a); err != nil {
		return Trigger{}, err
	}
	if in.Name == "" || len(in.Name) > 80 {
		return Trigger{}, fmt.Errorf("trigger: name must be 1–80 characters, got %d", len([]rune(in.Name)))
	}
	if in.Blueprint == "" {
		return Trigger{}, fmt.Errorf("trigger: blueprint must be named — an event with nowhere to go is not a trigger, it is a wish")
	}
	if in.MaxRunsPerHour < 1 || in.MaxRunsPerHour > 1000 {
		return Trigger{}, fmt.Errorf("trigger: max_runs_per_hour must be 1–1000, got %d (AU11: a trigger without a rate is an outage with a name)", in.MaxRunsPerHour)
	}
	if in.MaxCostPerDayUSD != nil && *in.MaxCostPerDayUSD < 0 {
		return Trigger{}, fmt.Errorf("trigger: max_cost_per_day_usd cannot be negative")
	}
	if err := in.Match.Validate(); err != nil {
		return Trigger{}, err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Trigger{}, fmt.Errorf("store: create trigger: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var from int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM automation_event`).Scan(&from); err != nil {
		return Trigger{}, fmt.Errorf("store: create trigger: read the outbox head: %w", err)
	}
	now := s.Now()
	id := "trg-" + newID()
	if _, err := tx.Exec(`INSERT INTO event_trigger (id,name,source,root_id,component_id,match_json,blueprint,kickoff,
		max_runs_per_hour,max_cost_per_day_usd,enabled,disabled_reason,from_outbox_id,created_by_kind,created_by_id,created_at,updated_at)
		VALUES (?,?,'event',?,?,?,?,?,?,?,1,'',?, 'user',?,?,?)`,
		id, in.Name, in.RootID, in.ComponentID, in.Match.Canonical(), in.Blueprint, in.Kickoff,
		in.MaxRunsPerHour, in.MaxCostPerDayUSD, from, a.ID, now, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: event_trigger.name") {
			return Trigger{}, fmt.Errorf("trigger %q already exists", in.Name)
		}
		return Trigger{}, fmt.Errorf("store: create trigger %q: %w", in.Name, err)
	}
	if err := automationAudit(tx, "event", id, "create", "created event "+id, a, now); err != nil {
		return Trigger{}, err
	}
	if err := tx.Commit(); err != nil {
		return Trigger{}, fmt.Errorf("store: create trigger: %w", err)
	}
	return s.GetEventTrigger(id)
}

// UpdateEventTrigger edits the given fields and re-validates the MERGED
// result — an edit that pairs a new match with an old event's key rules is
// still refused. created_by_*, from_outbox_id and source never move: the
// row remembers who stood behind it at birth, whoever edits it later.
func (s *Store) UpdateEventTrigger(a Actor, id string, e EventTriggerEdit) (Trigger, error) {
	if err := requireUser(a); err != nil {
		return Trigger{}, err
	}
	cur, err := s.GetEventTrigger(id)
	if err != nil {
		return Trigger{}, err
	}
	name, blueprint, kickoff, runs := cur.Name, cur.Blueprint, cur.Kickoff, cur.MaxRunsPerHour
	cost := cur.MaxCostPerDayUSD
	matchJSON := cur.MatchJSON
	if e.Name != nil {
		name = *e.Name
	}
	if e.Blueprint != nil {
		blueprint = *e.Blueprint
	}
	if e.Kickoff != nil {
		kickoff = *e.Kickoff
	}
	if e.MaxRunsPerHour != nil {
		runs = *e.MaxRunsPerHour
	}
	if e.ClearMaxCost {
		cost = nil
	} else if e.MaxCostPerDayUSD != nil {
		cost = e.MaxCostPerDayUSD
	}
	if e.Match != nil {
		matchJSON = e.Match.Canonical()
	}
	if name == "" || len([]rune(name)) > 80 {
		return Trigger{}, fmt.Errorf("trigger: name must be 1–80 characters")
	}
	if blueprint == "" {
		return Trigger{}, fmt.Errorf("trigger: blueprint must be named")
	}
	if runs < 1 || runs > 1000 {
		return Trigger{}, fmt.Errorf("trigger: max_runs_per_hour must be 1–1000, got %d", runs)
	}
	if cost != nil && *cost < 0 {
		return Trigger{}, fmt.Errorf("trigger: max_cost_per_day_usd cannot be negative")
	}
	m, err := ParseMatch([]byte(matchJSON))
	if err != nil {
		return Trigger{}, err
	}
	now := s.Now()
	changed := name != cur.Name || blueprint != cur.Blueprint || kickoff != cur.Kickoff ||
		runs != cur.MaxRunsPerHour || !sameCost(cost, cur.MaxCostPerDayUSD) ||
		m.Canonical() != cur.MatchJSON
	tx, err := s.w.Begin()
	if err != nil {
		return Trigger{}, fmt.Errorf("store: update trigger %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE event_trigger SET name=?, blueprint=?, kickoff=?, max_runs_per_hour=?, max_cost_per_day_usd=?, match_json=?, updated_at=? WHERE id=?`,
		name, blueprint, kickoff, runs, cost, m.Canonical(), now, id); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: event_trigger.name") {
			return Trigger{}, fmt.Errorf("trigger %q already exists", name)
		}
		return Trigger{}, fmt.Errorf("store: update trigger %s: %w", id, err)
	}
	if changed {
		// AN4: one audit row per CHANGING write — an edit that moved no
		// column is a touch, not a change.
		if err := automationAudit(tx, "event", id, "update", "updated event "+id, a, now); err != nil {
			return Trigger{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Trigger{}, fmt.Errorf("store: update trigger %s: %w", id, err)
	}
	return s.GetEventTrigger(id)
}

// SetEventTriggerEnabled is AU1's lifecycle: a trigger is disabled with a
// reason, never deleted, and re-enabled when the reason has passed. A
// no-op (already in that state) answers the row with updated_at unmoved —
// an edit that changed nothing must not claim a timestamp it did not earn.
func (s *Store) SetEventTriggerEnabled(a Actor, id string, enabled bool, reason string) (Trigger, error) {
	if err := requireUser(a); err != nil {
		return Trigger{}, err
	}
	cur, err := s.GetEventTrigger(id)
	if err != nil {
		return Trigger{}, err
	}
	if cur.Enabled == enabled {
		return cur, nil
	}
	if !enabled && strings.TrimSpace(reason) == "" {
		return Trigger{}, fmt.Errorf("trigger: disabling needs a reason — 'off' with no sentence is a trigger nobody can review back (AU1)")
	}
	disabled := ""
	if !enabled {
		disabled = reason
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Trigger{}, fmt.Errorf("store: set trigger %s enabled: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE event_trigger SET enabled=?, disabled_reason=?, updated_at=? WHERE id=?`,
		map[bool]int{true: 1, false: 0}[enabled], disabled, s.Now(), id); err != nil {
		return Trigger{}, fmt.Errorf("store: set trigger %s enabled: %w", id, err)
	}
	// The no-op branch answered above, before any SQL — a write that got
	// here changed the row, so it audits (AN4's once-per-change rule).
	op, summary := "enable", "enabled event "+id
	if !enabled {
		op, summary = "disable", "disabled event "+id
	}
	if err := automationAudit(tx, "event", id, op, summary, a, s.Now()); err != nil {
		return Trigger{}, err
	}
	if err := tx.Commit(); err != nil {
		return Trigger{}, fmt.Errorf("store: set trigger %s enabled: %w", id, err)
	}
	return s.GetEventTrigger(id)
}

// GetEventTrigger reads one row; absent is ErrNotFound.
func (s *Store) GetEventTrigger(id string) (Trigger, error) {
	row := s.r.QueryRow(`SELECT `+triggerColumns+` FROM event_trigger WHERE id = ?`, id)
	t, err := scanTrigger(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, fmt.Errorf("store: trigger %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("store: read trigger %s: %w", id, err)
	}
	return t, nil
}

// ListEventTriggers is the listing, ordered by name (a person scanning
// triggers reads names, not ids).
func (s *Store) ListEventTriggers(f EventTriggerFilter) ([]Trigger, error) {
	q := `SELECT ` + triggerColumns + ` FROM event_trigger WHERE 1=1`
	args := []any{}
	if f.RootID != "" {
		q += ` AND root_id = ?`
		args = append(args, f.RootID)
	}
	if f.ComponentID != "" {
		q += ` AND component_id = ?`
		args = append(args, f.ComponentID)
	}
	if f.Enabled != nil {
		q += ` AND enabled = ?`
		args = append(args, map[bool]int{true: 1, false: 0}[*f.Enabled])
	}
	if !f.IncludeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY name`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list triggers: %w", err)
	}
	defer rows.Close()
	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan trigger: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListEventTriggersFor is the dispatcher's candidate read: triggers born at
// or before the event's outbox id (a trigger is never authorised for
// history), still ordered by id, filtered in Go through Admits. It INCLUDES
// disabled triggers on purpose: the dispatcher must be able to record
// skipped(disabled) — a silent skip is AU12's could-not-say dressed as an
// answer.
func (s *Store) ListEventTriggersFor(ev AutomationEvent) ([]Trigger, error) {
	// S49 AN6: an archived row is engine-invisible. The invisibility lives
	// in THIS read — the store read the dispatcher already makes — because
	// dispatcher.go is engine and stays byte-identical; archived is also
	// enabled=0 (EventArchiveTx), so this filter and the engine's own word
	// agree by construction, never by luck.
	rows, err := s.r.Query(`SELECT `+triggerColumns+` FROM event_trigger WHERE source='event' AND archived_at IS NULL AND from_outbox_id < ? ORDER BY id`, ev.ID)
	if err != nil {
		return nil, fmt.Errorf("store: list triggers for event: %w", err)
	}
	defer rows.Close()
	var out []Trigger
	for rows.Next() {
		t, err := scanTrigger(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan trigger: %w", err)
		}
		if t.Admits(ev) {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

// ── the fire ledger (AU8/AU12) ──────────────────────────────────────────────

// The five outcomes (AU12), in the order the surface lists them. NULL in
// the database is not a sixth value: it is the claim itself.
const (
	FireStarted     = "started"
	FireRefused     = "refused"
	FireLimited     = "limited"
	FireSkipped     = "skipped"
	FireCouldNotSay = "could-not-say"
)

// EventFireOutcomes returns the closed five.
func EventFireOutcomes() []string {
	return []string{FireStarted, FireRefused, FireLimited, FireSkipped, FireCouldNotSay}
}

// EventFireReasons is AU12's closed list: the allowed reason CODES (the
// text before the first colon) per outcome. Widening this map without
// updating design.md AU12 in the same change is forbidden by the task's
// Do-NT — the list and the rule file move together or not at all.
var EventFireReasons = map[string][]string{
	FireStarted:     {},
	FireRefused:     {"depth-exceeded", "requires-unmet", "blueprint-refused", "no-target-component", "workspace-invalid", "subject-missing", "kickoff-invalid"},
	FireLimited:     {"max-runs-per-hour", "max-cost-per-day"},
	FireSkipped:     {"disabled", "overlap"},
	FireCouldNotSay: {"start-error", "trigger-edited-since-claim"},
}

// ErrEventFireDecided: somebody already answered this claim (the claimed-FROM
// UPDATE matched nothing because the row moved under us — S35 TD1's pattern,
// reused).
var ErrEventFireDecided = errors.New("store: this fire already has an outcome")

// NewEventFire is the claim input.
type NewEventFire struct {
	TriggerID              string
	OutboxID               int64
	SubjectKind, SubjectID string
	Depth                  int
}

// EventFireDecision is the answer.
type EventFireDecision struct {
	Outcome, Reason, LoopID, LoopSlug, CostNote string
}

// Fire is an event_trigger_fire row. Outcome "" is the claimed-undecided
// state — a crash between claim and start leaves a row that reads as
// "in flight", and OpenEventFires is how recovery finds it.
type Fire struct {
	ID                                          int64
	TriggerID                                   string
	OutboxID                                    int64
	Source, SubjectKind, SubjectID              string
	Depth                                       int
	Outcome, Reason, LoopID, LoopSlug, CostNote string
	ClaimedAt, DecidedAt                        string
}

// EventFireFilter narrows the fire log.
type EventFireFilter struct {
	TriggerID, Outcome string
	BeforeID           int64
	Limit              int
}

const fireColumns = `id, trigger_id, outbox_id, source, subject_kind, subject_id, depth,
	COALESCE(outcome,''), reason, COALESCE(loop_id,''), COALESCE(loop_slug,''), cost_note,
	claimed_at, COALESCE(decided_at,'')`

func scanFire(scan func(dest ...any) error) (Fire, error) {
	var f Fire
	err := scan(&f.ID, &f.TriggerID, &f.OutboxID, &f.Source, &f.SubjectKind, &f.SubjectID, &f.Depth,
		&f.Outcome, &f.Reason, &f.LoopID, &f.LoopSlug, &f.CostNote, &f.ClaimedAt, &f.DecidedAt)
	return f, err
}

// validateDecision enforces the closed vocabulary before any SQL.
func validateDecision(d EventFireDecision) error {
	if !containsKey(EventFireOutcomes(), d.Outcome) {
		return fmt.Errorf("fire: outcome %q is not one of %s", d.Outcome, strings.Join(EventFireOutcomes(), ", "))
	}
	if d.Outcome == FireStarted {
		if d.Reason != "" {
			return fmt.Errorf("fire: a started loop has no reason to give — reason %q belongs to a refusal", d.Reason)
		}
		if d.LoopID == "" {
			return fmt.Errorf("fire: outcome started without a loop id is the confident wrong answer AU12 refuses")
		}
	} else {
		code := d.Reason
		if i := strings.Index(code, ":"); i >= 0 {
			code = code[:i]
		}
		if !containsKey(EventFireReasons[d.Outcome], code) {
			return fmt.Errorf("fire: reason code %q is not allowed for outcome %s (the set is %s)", code, d.Outcome, strings.Join(EventFireReasons[d.Outcome], ", "))
		}
	}
	switch d.CostNote {
	case "", "cost-unknown", "cost-floor":
	default:
		return fmt.Errorf("fire: cost_note %q is not one of '' (known), cost-unknown, cost-floor (P31's three states)", d.CostNote)
	}
	return nil
}

// ClaimEventFire is AU8's claim-before-start: the UNIQUE(trigger_id,
// outbox_id) is the mechanism, outcome NULL is the claimed state, and a
// losing claim gets the EXISTING row back rather than an error — the
// dispatcher's answer is "somebody (me, before the crash) is on it".
func (s *Store) ClaimEventFire(in NewEventFire, at time.Time) (Fire, bool, error) {
	res, err := s.w.Exec(`INSERT INTO event_trigger_fire (trigger_id,outbox_id,source,subject_kind,subject_id,depth,outcome,reason,claimed_at,cost_note)
		VALUES (?,?, 'event', ?,?,?, NULL,'',?,'')
		ON CONFLICT(trigger_id, outbox_id) DO NOTHING`,
		in.TriggerID, in.OutboxID, in.SubjectKind, in.SubjectID, in.Depth, isoUTC(at))
	if err != nil {
		return Fire{}, false, fmt.Errorf("store: claim fire (trigger %s, event %d): %w", in.TriggerID, in.OutboxID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		f, err := s.getFireByKey(in.TriggerID, in.OutboxID)
		return f, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Fire{}, false, fmt.Errorf("store: claim fire id: %w", err)
	}
	f, err := s.GetEventFire(id)
	return f, true, err
}

func (s *Store) getFireByKey(triggerID string, outboxID int64) (Fire, error) {
	row := s.r.QueryRow(`SELECT `+fireColumns+` FROM event_trigger_fire WHERE trigger_id=? AND outbox_id=?`, triggerID, outboxID)
	f, err := scanFire(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Fire{}, fmt.Errorf("store: fire (trigger %s, event %d): %w", triggerID, outboxID, ErrNotFound)
	}
	if err != nil {
		return Fire{}, fmt.Errorf("store: read fire (trigger %s, event %d): %w", triggerID, outboxID, err)
	}
	return f, nil
}

// DecideEventFire answers a claim, claimed-FROM: the UPDATE fires only
// while outcome IS NULL, so two deciders cannot both speak, and a late
// answer to an already-answered fire says so rather than overwriting the
// first. `at` is injected — the ledger records the dispatcher's time.
func (s *Store) DecideEventFire(id int64, d EventFireDecision, at time.Time) error {
	if err := validateDecision(d); err != nil {
		return err
	}
	res, err := s.w.Exec(`UPDATE event_trigger_fire SET outcome=?, reason=?, loop_id=NULLIF(?,''), loop_slug=NULLIF(?,''), cost_note=?, decided_at=?
		WHERE id=? AND outcome IS NULL`,
		d.Outcome, d.Reason, d.LoopID, d.LoopSlug, d.CostNote, isoUTC(at), id)
	if err != nil {
		return fmt.Errorf("store: decide fire %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := s.r.QueryRow(`SELECT 1 FROM event_trigger_fire WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: fire %d: %w", id, ErrNotFound)
		} else if err != nil {
			return fmt.Errorf("store: check fire %d: %w", id, err)
		}
		return fmt.Errorf("fire %d: %w", id, ErrEventFireDecided)
	}
	return nil
}

// RecordEventFire is claim-and-answer in ONE insert — the path for verdicts
// that need no start (skipped/disabled): a row that was never undecided
// cannot be misread as in flight. A duplicate (trigger, event) comes back
// fresh=false with the existing row, exactly like the claim.
func (s *Store) RecordEventFire(in NewEventFire, d EventFireDecision, at time.Time) (Fire, bool, error) {
	if err := validateDecision(d); err != nil {
		return Fire{}, false, err
	}
	res, err := s.w.Exec(`INSERT INTO event_trigger_fire (trigger_id,outbox_id,source,subject_kind,subject_id,depth,outcome,reason,loop_id,loop_slug,cost_note,claimed_at,decided_at)
		VALUES (?,?,'event',?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(trigger_id, outbox_id) DO NOTHING`,
		in.TriggerID, in.OutboxID, in.SubjectKind, in.SubjectID, in.Depth,
		d.Outcome, d.Reason, nullIfEmpty(d.LoopID), nullIfEmpty(d.LoopSlug), d.CostNote, isoUTC(at), isoUTC(at))
	if err != nil {
		return Fire{}, false, fmt.Errorf("store: record fire (trigger %s, event %d): %w", in.TriggerID, in.OutboxID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		f, err := s.getFireByKey(in.TriggerID, in.OutboxID)
		return f, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Fire{}, false, fmt.Errorf("store: record fire id: %w", err)
	}
	f, err := s.GetEventFire(id)
	return f, true, err
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// GetEventFire reads one fire row.
func (s *Store) GetEventFire(id int64) (Fire, error) {
	row := s.r.QueryRow(`SELECT `+fireColumns+` FROM event_trigger_fire WHERE id=?`, id)
	f, err := scanFire(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Fire{}, fmt.Errorf("store: fire %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Fire{}, fmt.Errorf("store: read fire %d: %w", id, err)
	}
	return f, nil
}

// OpenEventFires is recovery's read (T6): claimed but undecided rows, the
// dispatcher's crashed in-flight work, oldest first.
func (s *Store) OpenEventFires() ([]Fire, error) {
	rows, err := s.r.Query(`SELECT ` + fireColumns + ` FROM event_trigger_fire WHERE outcome IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: open fires: %w", err)
	}
	defer rows.Close()
	var out []Fire
	for rows.Next() {
		f, err := scanFire(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan fire: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListEventFires is the fire log: newest first (a person debugging a
// trigger reads the recent end), bounded by limit (default 50, ceiling 500
// — a listing page, not a table dump).
func (s *Store) ListEventFires(f EventFireFilter) ([]Fire, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	q := `SELECT ` + fireColumns + ` FROM event_trigger_fire WHERE 1=1`
	args := []any{}
	if f.TriggerID != "" {
		q += ` AND trigger_id = ?`
		args = append(args, f.TriggerID)
	}
	if f.Outcome != "" {
		q += ` AND outcome = ?`
		args = append(args, f.Outcome)
	}
	if f.BeforeID > 0 {
		q += ` AND id < ?`
		args = append(args, f.BeforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list fires: %w", err)
	}
	defer rows.Close()
	var out []Fire
	for rows.Next() {
		f, err := scanFire(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan fire: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// EventFireCounts answers "what has this trigger done lately" with ALL FIVE
// outcomes always present, zero included (P31: a hidden zero reads as
// "nothing happened" when what happened is nobody asked). Claimed-undecided
// rows appear in no bucket — they are not an outcome, they are the claim;
// OpenEventFires counts those honestly. An EMPTY triggerID means ALL
// triggers (S40 T7's desk shows one counts row across the catalogue) —
// spelled `(? = ” OR trigger_id = ?)` so one SQL serves both readings.
func (s *Store) EventFireCounts(triggerID string) (map[string]int, error) {
	out := map[string]int{}
	for _, o := range EventFireOutcomes() {
		out[o] = 0
	}
	rows, err := s.r.Query(`SELECT outcome, COUNT(*) FROM event_trigger_fire WHERE (? = '' OR trigger_id = ?) AND outcome IS NOT NULL GROUP BY outcome`, triggerID, triggerID)
	if err != nil {
		return nil, fmt.Errorf("store: fire counts for %q: %w", triggerID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var o string
		var n int
		if err := rows.Scan(&o, &n); err != nil {
			return nil, fmt.Errorf("store: scan fire counts: %w", err)
		}
		out[o] = n
	}
	return out, rows.Err()
}

// ValidEventRefusedReason reports membership in the refused outcome's
// closed code list (AU12). The dispatcher asks it of every *Refusal a
// Starter returns and distrusts what it does not know: an unclassified
// refusal is a could-not-say, never a decision — a starter that invents
// vocabulary is a starter whose verdicts cannot be trusted.
func ValidEventRefusedReason(code string) bool {
	return containsKey(EventFireReasons[FireRefused], code)
}
