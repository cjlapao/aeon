package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// Projects, loops and phases.
//
// Reads use the READER pool and writes the writer (O6, I6). A read on the
// single-connection writer serialises behind every write, and the symptom is a
// UI that stalls whenever a loop is busy — which gets diagnosed as a UI
// problem, somewhere else entirely.

// Loop is a loop's row.
type Loop struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Goal          string `json:"goal"`
	Root          string `json:"root"`
	ComponentRoot string `json:"componentRoot"`
	ProjectKey    string `json:"projectKey,omitempty"`
	Mode          string `json:"mode"`
	Phase         string `json:"phase"`
	Status        string `json:"status"`

	DefaultBranch string `json:"defaultBranch,omitempty"`
	BaseBranch    string `json:"baseBranch,omitempty"`
	BranchMode    string `json:"branchMode,omitempty"`
	FeatureBranch string `json:"featureBranch,omitempty"`

	TotalFailedGates int `json:"totalFailedGates"`
	SplitsDone       int `json:"splitsDone"`
	QARounds         int `json:"qaRounds"`

	NowTask         string `json:"nowTask,omitempty"`
	NowAgent        string `json:"nowAgent,omitempty"`
	NowStep         string `json:"nowStep,omitempty"`
	NowDispatchedAt string `json:"nowDispatchedAt,omitempty"`

	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`

	// StartedBy and TriggerID are S20's provenance columns (R-3): "" for a
	// person's loop, "trigger"/"webhook" plus the schedule id for a fired
	// one. Two columns beside attendedness, never inside it — "who started
	// it" and "may it act outwardly" stay two questions.
	// GoalID and GoalVersion are the loop's goal coordinates (S38 T13,
	// OL9): which goal this loop works, at the version PINNED at start —
	// later goal versions do not silently move the loop's frame. "" / 0
	// mean the loop was started without a goal, which is every loop that
	// predates the coordinates and every unlinked start after it.
	GoalID      string `json:"goalId,omitempty"`
	GoalVersion int    `json:"goalVersion,omitempty"`
	// Scope names the goal's reach for this loop: component (one
	// workspace) or project (a whole group). Default component — rows
	// that predate the column were each one workspace's.
	Scope string `json:"scope,omitempty"`

	StartedBy string `json:"startedBy"`
	TriggerID string `json:"triggerId"`

	// AutomationDepth is S40 AU5's cascade counter: how many event-started
	// loops stand between a person and this one. A person's loop — socket,
	// CLI or S20's clock — is 0, always; the omitempty keeps every predating
	// row and projection byte-identical.
	AutomationDepth int `json:"automationDepth,omitempty"`
}

// NewLoop is what a caller supplies to create one. Everything else is the
// store's: the id, the timestamps, and the defaults.
type NewLoop struct {
	Slug          string
	Title         string
	Goal          string
	Root          string
	ComponentRoot string
	ProjectKey    string
	Mode          string
	Phase         string
	Status        string

	// S60 MQ15: the branch shape loop.start PINS for this loop's lifetime
	// — the component's effective default branch, the base a landing cuts
	// from, the branch scheme, and the feature-branch name derived under
	// the prefix that was effective at start. Empty is every loop started
	// before S60 (and every caller that passes nothing): the engine keeps
	// deriving names as it always did, and the queue refuses such a loop
	// rather than guessing (F14's first writer, the pin path).
	DefaultBranch string
	BaseBranch    string
	BranchMode    string
	FeatureBranch string

	// Attended is the D2 declaration made at start: true means a human is
	// here to answer questions, false means nobody is, and nil means it was
	// never recorded — three states, none collapsed into another (P31).
	// Both creation paths write it into loop.attended as it stands; nothing
	// re-derives it afterwards, so the caller's argument IS the record.
	Attended *bool

	// GoalID/GoalVersion are S38 T13's start-time coordinates (OL9): the
	// goal this loop works and the version its frame is read at. Both nil
	// is today's shape — no goal, nothing pinned.
	GoalID      string
	GoalVersion *int

	// AllowOutward is S19 T6's per-loop opt-in (D10, O2), made at the one
	// moment the operator decides what THIS run may do. It lifts outward
	// and only outward for an unattended loop; irreversible is untouched
	// by it, and no key, caller or configuration anywhere carries a
	// standing version of it — a permanent grant is a grant whose theft
	// is unbounded. Written once here; the loop runtime reads it from the
	// row the way it reads attendedness.
	AllowOutward bool

	// StartedBy and TriggerID are S20's provenance (R-3): "trigger" or
	// "webhook" beside the schedule's id, or "" for a person's loop.
	// They are NOT attendedness and never share a field with it — "who
	// started it" and "may it act outwardly" stay two questions with two
	// columns, because one column is how somebody sets the first to
	// change the second.
	StartedBy string
	TriggerID string

	// AutomationDepth is S40 AU5's cascade counter, written at the pin: the
	// event dispatcher passes the event's depth + 1, and nobody else sets it
	// — a person's loop is 0. It belongs to the pin INSERT, not a later
	// stamp: a loop whose depth arrives after its first step could outrun
	// the very counter meant to bound it.
	AutomationDepth int

	// FirstMessage is S40's kickoff: the chat row an event start speaks
	// before the runner could dispatch a step. nil means no message — every
	// start that is not an event fire. Written in CreatePinnedLoop's ONE
	// transaction (T4): a loop row that exists without its first words is
	// the half-written birth the pin transaction exists to prevent.
	FirstMessage *NewChatMessage

	// AutonomyPin is S56 AL8's pin: the immutable snapshot a policy-resolved
	// loop is pinned with. Internal only, never on the wire; written once in
	// CreatePinnedLoop's transaction; no UPDATE touches the pin. nil means the
	// loop was not policy-resolved (today's behaviour exactly). loop.allow_outward
	// stays 0 for a policy-pinned loop (AL8): the legacy flag keeps meaning "the
	// person typed it"; the new per-action permission rides the pin, not the flag.
	AutonomyPin *AutonomyPin
}

// ErrNotFound is returned when a row that was asked for by identity does not
// exist.
//
// An error rather than a discriminated result, deliberately: "there is no such
// loop" is not a domain REFUSAL (DB19's subject) — nothing was refused. It is
// a lookup that found nothing, and every caller handles it the same way.
var ErrNotFound = errors.New("store: not found")

// CreateLoop inserts a loop.
//
// A slug that already exists in the SAME component root is a conflict; the same
// slug in a DIFFERENT root is a separate loop, and that is the whole point of
// the composite key (DB4).
func (s *Store) CreateLoop(in NewLoop) (Loop, error) {
	if in.Slug == "" || in.ComponentRoot == "" {
		return Loop{}, fmt.Errorf("store: a loop needs a slug and a component root")
	}

	now := s.Now()
	l := Loop{
		ID:            newID(),
		Slug:          in.Slug,
		Title:         orDefault(in.Title, in.Slug),
		Goal:          in.Goal,
		Root:          orDefault(in.Root, in.ComponentRoot),
		ComponentRoot: in.ComponentRoot,
		ProjectKey:    in.ProjectKey,
		Mode:          orDefault(in.Mode, string(ModeStandard)),
		Phase:         orDefault(in.Phase, string(PhasePreflight)),
		Status:        orDefault(in.Status, "active"),
		StartedAt:     now,
		UpdatedAt:     now,
		// S60 MQ15: the store writes what the START DOOR pins — it never
		// invents a name or a base itself (the empty-input golden stays
		// empty; the four columns gained their first writer in T4, and
		// that writer is loop.start, not here).
		DefaultBranch: in.DefaultBranch,
		BaseBranch:    in.BaseBranch,
		BranchMode:    in.BranchMode,
		FeatureBranch: in.FeatureBranch,
	}

	// S33 TL2, extended to the count the design left unassigned: loops_run
	// bumps HERE, in the same transaction as the loop's own row. The slice's
	// design names the column but no write path — and the honest moment a
	// loop has "run" in this codebase is its creation (a loop row is born
	// when a person or a trigger starts it). A loop whose root maps to no
	// registered component counts nowhere: a rollup cannot be charged to a
	// component that does not exist (same rule as every RecordX bump).
	tx, err := s.w.Begin()
	if err != nil {
		return Loop{}, fmt.Errorf("store: create loop %s: %w", Scope{in.Slug, in.ComponentRoot}, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
		INSERT INTO loop (
			id, slug, title, goal, root, component_root, project_key,
			mode, phase, status, attended, allow_outward, started_at, updated_at,
			started_by, trigger_id, default_branch, base_branch, branch_mode, feature_branch
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.Slug, l.Title, l.Goal, l.Root, l.ComponentRoot, nullable(l.ProjectKey),
		l.Mode, l.Phase, l.Status, attendedArg(in.Attended), outwardArg(in.AllowOutward), l.StartedAt, l.UpdatedAt,
		in.StartedBy, in.TriggerID, nullable(l.DefaultBranch), nullable(l.BaseBranch), nullable(l.BranchMode), nullable(l.FeatureBranch)); err != nil {
		return Loop{}, fmt.Errorf("store: create loop %s: %w", Scope{in.Slug, in.ComponentRoot}, err)
	}
	if componentID, projectID, found := resolveTelemetryScopeByRoot(tx, in.ComponentRoot); found {
		if err := bumpTelemetryRollups(tx, s, componentID, projectID, telemetryDelta{loopsRun: 1}, now); err != nil {
			return Loop{}, fmt.Errorf("store: create loop %s: %w", Scope{in.Slug, in.ComponentRoot}, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Loop{}, fmt.Errorf("store: create loop %s: %w", Scope{in.Slug, in.ComponentRoot}, err)
	}
	return l, nil
}

// SetLoopProvenance stamps WHO started a loop — "trigger" or "webhook"
// beside the schedule id — written by the daemon's fire path immediately
// after the pin, never by the fire itself (a fire cannot name itself
// something it is not). R-3: this is NOT attendedness; it shares no column
// with it, and an empty pair is honestly a person's loop.
func (s *Store) SetLoopProvenance(ctx context.Context, id, startedBy, triggerID string) error {
	if startedBy == "" {
		return fmt.Errorf("store: loop %s: provenance needs a starter", id)
	}
	res, err := s.w.ExecContext(ctx, `UPDATE loop SET started_by=?, trigger_id=?, updated_at=? WHERE id=?`,
		startedBy, triggerID, s.Now(), id)
	if err != nil {
		return fmt.Errorf("store: provenance loop %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: provenance loop %s: %w", id, ErrNotFound)
	}
	return nil
}

// LoopStartedBy reads the provenance pair — the three questions stay
// separate reads because they are three separate columns: who started it
// (this), whether anybody is here (LoopAttended), and what it may do
// outwardly (LoopAllowOutward).
func (s *Store) LoopStartedBy(ctx context.Context, id string) (startedBy, triggerID string, err error) {
	err = s.r.QueryRowContext(ctx, `SELECT COALESCE(started_by,''), COALESCE(trigger_id,'') FROM loop WHERE id=?`, id).Scan(&startedBy, &triggerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("store: loop %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return "", "", fmt.Errorf("store: provenance loop %s: %w", id, err)
	}
	return startedBy, triggerID, nil
}

// loopColumns is the SELECT list, written once.
//
// One place, because a column added to the table and forgotten here reads back
// as a zero value — which looks like data loss and is actually a missing name.
// `attended` is the deliberate exception: its third state (NULL = never
// declared, D2/P31) has no honest non-pointer home in Loop, so it is read by
// name only where the three states are consumed — ReadPinnedLoop and
// LoopAttended. Adding it here as a plain bool would collapse "nobody said"
// into "nobody is here".
const loopColumns = `id, slug, title, goal, root, component_root, COALESCE(project_key,''),
	mode, phase, status,
	COALESCE(default_branch,''), COALESCE(base_branch,''), COALESCE(branch_mode,''), COALESCE(feature_branch,''),
	total_failed_gates, splits_done, qa_rounds,
	COALESCE(now_task,''), COALESCE(now_agent,''), COALESCE(now_step,''), COALESCE(now_dispatched_at,''),
	started_at, updated_at,
	COALESCE(started_by,''), COALESCE(trigger_id,''),
	COALESCE(goal_id,''), COALESCE(goal_version,0), COALESCE(scope,'component'),
	COALESCE(automation_depth,0)`

func scanLoop(row interface{ Scan(...any) error }) (Loop, error) {
	var l Loop
	err := row.Scan(
		&l.ID, &l.Slug, &l.Title, &l.Goal, &l.Root, &l.ComponentRoot, &l.ProjectKey,
		&l.Mode, &l.Phase, &l.Status,
		&l.DefaultBranch, &l.BaseBranch, &l.BranchMode, &l.FeatureBranch,
		&l.TotalFailedGates, &l.SplitsDone, &l.QARounds,
		&l.NowTask, &l.NowAgent, &l.NowStep, &l.NowDispatchedAt,
		&l.StartedAt, &l.UpdatedAt,
		&l.StartedBy, &l.TriggerID,
		&l.GoalID, &l.GoalVersion, &l.Scope, &l.AutomationDepth)
	return l, err
}

// GetLoop reads one loop by its scope.
// ReadLoopByID resolves a loop from its id alone — the door loop.archive
// needs (S33 T9): the bench names a loop by the id `aeon loop list`
// printed, and holds neither a slug nor a component root. ReadPinnedLoop
// cannot serve this: "pinned" means BLUEPRINT-pinned, and an ordinary
// loop.create loop has no blueprint row — a door that only opens on
// blueprint loops would archive half the loops it lists.
func (s *Store) ReadLoopByID(id string) (Loop, error) {
	row := s.r.QueryRow(`SELECT `+loopColumns+` FROM loop WHERE id=?`, id)
	l, err := scanLoop(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Loop{}, fmt.Errorf("store: no loop %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return Loop{}, fmt.Errorf("store: read loop %s: %w", id, err)
	}
	return l, nil
}

func (s *Store) GetLoop(sc Scope) (Loop, error) {
	if !sc.Valid() {
		return Loop{}, fmt.Errorf("store: GetLoop needs both a slug and a component root, got %s", sc)
	}
	where, args := sc.WhereLoop("")
	row := s.r.QueryRow(`SELECT `+loopColumns+` FROM loop WHERE `+where, args...)
	l, err := scanLoop(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Loop{}, fmt.Errorf("store: loop %s: %w", sc, ErrNotFound)
	}
	if err != nil {
		return Loop{}, fmt.Errorf("store: read loop %s: %w", sc, err)
	}
	return l, nil
}

// ListLoops reads loops, most recently touched first.
//
// The ordering matches the index that lands with the schema (2a) and the first
// question S25's history view asks: every loop by recency.
func (s *Store) ListLoops(ps ComponentScope) ([]Loop, error) {
	where, args := ps.Where("")
	rows, err := s.r.Query(`SELECT `+loopColumns+` FROM loop WHERE `+where+` ORDER BY updated_at DESC, slug`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list loops: %w", err)
	}
	defer rows.Close()

	var out []Loop
	for rows.Next() {
		l, err := scanLoop(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list loops: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LoopsBySlug answers "which roots carry a loop with this slug" — the
// slug is unique per (project_root, slug), never globally (two checkouts
// hold a loop called "refactor" without colliding), so the answer is a
// SET. An empty set is an honest no such loop; more than one is the
// ambiguity the caller refuses (a slug alone does not name a scope, I4).
func (s *Store) LoopsBySlug(slug string) ([]Loop, error) {
	rows, err := s.r.Query(`SELECT `+loopColumns+` FROM loop WHERE slug = ? ORDER BY component_root, slug`, slug)
	if err != nil {
		return nil, fmt.Errorf("store: loops by slug %q: %w", slug, err)
	}
	defer rows.Close()

	var out []Loop
	for rows.Next() {
		l, err := scanLoop(rows)
		if err != nil {
			return nil, fmt.Errorf("store: loops by slug %q: %w", slug, err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LoopAttended reads the loop's pinned attendedness declaration — the daemon
// adapter's read for step assembly (D2: the value is written once, at pin
// time, and every later reader takes it as data). The answer is three-state
// (P31): attended, unattended, or never recorded. NULL never reads as false;
// a row created before S11 says nobody declared, not that nobody was there.
func (s *Store) LoopAttended(ctx context.Context, id string) (*bool, error) {
	if id == "" {
		return nil, fmt.Errorf("store: LoopAttended needs a loop id")
	}
	var raw sql.NullInt64
	if err := s.r.QueryRowContext(ctx, `SELECT attended FROM loop WHERE id=?`, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: attendedness of loop %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("store: read attendedness of loop %s: %w", id, err)
	}
	return attendedFromNull(raw)
}

// LoopAllowOutward reads the loop's pinned outward opt-in — S19 T6. Its
// sibling LoopAttended is three-state; this is not, and the asymmetry is
// the point: attendedness is a CLAIM about a human (three states, none
// collapsed), while an outward permission is an ABSENCE by default, and
// the honest reading of an absent permission is deny-leaning (the zero
// value). The read exists for the loop runtime's decision site; the
// column is written exactly once, at creation, and nothing re-writes it —
// a grant rewritable mid-run would not be the per-moment decision the
// design argued for.
func (s *Store) LoopAllowOutward(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("store: LoopAllowOutward needs a loop id")
	}
	var raw int64
	if err := s.r.QueryRowContext(ctx, `SELECT allow_outward FROM loop WHERE id=?`, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("store: outward opt-in of loop %s: %w", id, ErrNotFound)
		}
		return false, fmt.Errorf("store: read outward opt-in of loop %s: %w", id, err)
	}
	return raw != 0, nil
}

// SetLoopPhase moves the loop's phase pointer and touches updated_at.
func (s *Store) SetLoopPhase(sc Scope, phase LoopPhase) error {
	where, args := sc.WhereLoop("")
	args = append([]any{string(phase), s.Now()}, args...)
	res, err := s.w.Exec(`UPDATE loop SET phase = ?, updated_at = ? WHERE `+where, args...)
	if err != nil {
		return fmt.Errorf("store: set the phase of %s: %w", sc, err)
	}
	return mustAffectOne(res, sc, "set phase")
}

// SetLoopStatus moves the loop's status and touches updated_at.
func (s *Store) SetLoopStatus(sc Scope, status string) error {
	where, args := sc.WhereLoop("")
	args = append([]any{status, s.Now()}, args...)
	res, err := s.w.Exec(`UPDATE loop SET status = ?, updated_at = ? WHERE `+where, args...)
	if err != nil {
		return fmt.Errorf("store: set the status of %s: %w", sc, err)
	}
	return mustAffectOne(res, sc, "set status")
}

// Phase is one row of the durable phase tracker.
type Phase struct {
	Phase     string `json:"phase"`
	Status    string `json:"status"`
	Ord       int    `json:"ord"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

// SetPhase upserts one phase row.
//
// Upsert rather than insert-or-update by the caller: the tracker is written
// twice per phase (in-progress before dispatch, done after), and a caller
// deciding which statement to use is a caller who will get it wrong on a
// retry.
func (s *Store) SetPhase(sc Scope, phase string, status string, ord int) error {
	if !sc.Valid() {
		return fmt.Errorf("store: SetPhase needs both a slug and a component root, got %s", sc)
	}
	now := s.Now()
	_, err := s.w.Exec(`
		INSERT INTO loop_phase (loop_slug, component_root, phase, status, ord, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (component_root, loop_slug, phase)
		DO UPDATE SET status = excluded.status, ord = excluded.ord, updated_at = excluded.updated_at`,
		sc.LoopSlug, sc.ComponentRoot, phase, status, ord, now, now)
	if err != nil {
		return fmt.Errorf("store: set phase %s of %s: %w", phase, sc, err)
	}
	return nil
}

// Phases reads the tracker in order.
func (s *Store) Phases(sc Scope) ([]Phase, error) {
	where, args := sc.Where("")
	rows, err := s.r.Query(`
		SELECT phase, status, ord, COALESCE(created_at,''), updated_at
		FROM loop_phase WHERE `+where+` ORDER BY ord, phase`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: read the phases of %s: %w", sc, err)
	}
	defer rows.Close()

	var out []Phase
	for rows.Next() {
		var p Phase
		if err := rows.Scan(&p.Phase, &p.Status, &p.Ord, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: read the phases of %s: %w", sc, err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertComponent was the S16 component/working-directory door that also made
// a component's implicit project group; the component tree (CT1) has no
// project tier, so the door is gone. CreateComponent is the tree's door.

// --- small shared helpers ----------------------------------------------------

// newID is a random hex id, matching v1's TEXT primary keys.
//
// Random rather than sequential: an id that encodes creation order is an id
// somebody will sort by, and then the ordering becomes a contract nobody meant
// to make. `updated_at` is the ordering, and it is a column.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is a broken machine, not a condition to handle.
		panic("store: crypto/rand is unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// nullable turns the empty string into a SQL NULL.
//
// "Absent" and "empty" are different facts (M9's rule, applied to columns):
// an empty `project_key` means nobody computed one yet, and storing ” makes
// that indistinguishable from a component whose key is genuinely blank.
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// attendedArg maps the *bool declaration onto the INTEGER column: nil stays
// NULL (never declared), and the two declared states become 1 and 0 exactly.
// No default is supplied here, because a default would be the collapse P31
// forbids — "absent" and "unattended" are different facts.
// outwardArg is the plain-binary sibling of attendedArg — no NULL, no
// third state (see LoopAllowOutward for why the two differ).
func outwardArg(allow bool) any {
	if allow {
		return 1
	}
	return 0
}

// goalVersionArg pins the goal version or leaves the column NULL (S38
// T13): 0 is not "unpinned" — a pinned version is always ≥ 1, but NULL is
// the honest "no goal" and must stay distinguishable from a read glitch.
func goalVersionArg(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func attendedArg(attended *bool) any {
	if attended == nil {
		return nil
	}
	if *attended {
		return 1
	}
	return 0
}

// attendedFromNull decodes the column back to the three states. A stored
// value outside 0/1 is not a third opinion to guess about — it is a corrupt
// row, and reading it as either declared state would invent a decision nobody
// made, so it is refused by name.
func attendedFromNull(raw sql.NullInt64) (*bool, error) {
	if !raw.Valid {
		return nil, nil
	}
	switch raw.Int64 {
	case 0:
		return ptr(false), nil
	case 1:
		return ptr(true), nil
	}
	return nil, fmt.Errorf("store: loop.attended holds %d; only 0, 1 and NULL are honest states", raw.Int64)
}

// ptr takes the address of a copy — the one way a *bool literal is written.
func ptr(v bool) *bool { return &v }

// mustAffectOne turns "no rows changed" into ErrNotFound.
//
// Without it, an UPDATE against a scope that does not exist succeeds silently
// — and the caller believes it wrote something. That is the failure mode this
// whole file's scoping exists to prevent, one level up.
func mustAffectOne(res sql.Result, sc Scope, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %s on %s: %w", what, sc, err)
	}
	if n == 0 {
		return fmt.Errorf("store: %s on %s: %w", what, sc, ErrNotFound)
	}
	return nil
}

// LoopCounterColumns is the closed set of loop-row counters, the whitelist
// that keeps a caller string out of BumpLoopCounter's SQL. S17's
// internal/loop/counters.go owns the table (which counter, which level,
// which cap) and passes the cap in; this is the row-mechanical half.
var LoopCounterColumns = []string{"total_failed_gates", "splits_done", "qa_rounds"}

// BumpLoopCounter adds one to a loop-level counter and reports where it
// landed. capAt <= 0 means uncapped; reaching a positive cap is inclusive
// and clamps without a write, exactly as BumpTaskCounter does — one
// discipline for budgets at both levels, so neither can be pushed past its
// cap by a misbehaving caller.
func (s *Store) BumpLoopCounter(sc Scope, column string, capAt int) (int, bool, error) {
	legal := false
	for _, c := range LoopCounterColumns {
		if c == column {
			legal = true
			break
		}
	}
	if !legal {
		return 0, false, fmt.Errorf("store: unknown loop counter column %q — the set is %v", column, LoopCounterColumns)
	}

	where, args := sc.WhereLoop("")
	var current int
	if err := s.r.QueryRow(`SELECT `+column+` FROM loop WHERE `+where, args...).Scan(&current); err != nil {
		return 0, false, fmt.Errorf("store: read counter %s of %s: %w", column, sc, err)
	}
	if capAt > 0 && current >= capAt {
		return capAt, true, nil
	}

	update, uargs := sc.WhereLoop("")
	if _, err := s.w.Exec(`UPDATE loop SET `+column+` = `+column+` + 1, updated_at = ? WHERE `+update, append([]any{s.Now()}, uargs...)...); err != nil {
		return 0, false, fmt.Errorf("store: bump %s of %s: %w", column, sc, err)
	}
	value := current + 1
	return value, capAt > 0 && value >= capAt, nil
}

// SetLoopTitle replaces a loop's display title (its goal text is untouched). A loop starts titled with its goal and
// is renamed once a short title has been made for it.
func (s *Store) SetLoopTitle(sc Scope, title string) error {
	if !sc.Valid() {
		return fmt.Errorf("store: SetLoopTitle needs both a slug and a component root, got %s", sc)
	}
	res, err := s.w.Exec(`UPDATE loop SET title = ? WHERE component_root = ? AND slug = ?`, title, sc.ComponentRoot, sc.LoopSlug)
	if err != nil {
		return fmt.Errorf("store: retitle loop %s: %w", sc, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: loop %s: %w", sc, ErrNotFound)
	}
	return nil
}
