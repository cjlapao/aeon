// retro.go — S63's fact half (T1): the three append-only fact tables and the
// template set, with the store functions every later S63 task reads through.
//
// The honesty rules this file is written under (RT1/RT2/RT16, P31/P32): a
// missing measurement is NULL, never 0 — every measured column on
// RetroOutcome/RetroAgent is a pointer and an absent one round-trips as
// SQL NULL; `complete=0` is the row's own "something here was not recorded"
// flag; cost is the FLOOR with its unknown counts, never a total; and there
// is no accessor that edits or deletes a fact — the triggers refuse even raw
// SQL, and this file offers no path around them.
//
// Idempotency (RT2): RecordRetroOutcome keys on
// UNIQUE (component_root, loop_slug, terminal_key) and lands with
// ON CONFLICT DO NOTHING; a replay returns inserted=false and the
// pre-existing id. terminal_key is composed by the recorder
// (outcome_kind + "@" + loop.updated_at, ms precision — the event seq is not
// reachable from a reactor, S63 T0 §1); the store only requires it non-empty.

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// RetroOutcome is one row of retro_outcome — one terminal event of one loop,
// as recorded at that event. Every measured field is a pointer: a nil reads
// as "not recorded", never as zero.
type RetroOutcome struct {
	ID               int64
	ComponentRoot    string
	LoopSlug         string
	TerminalKey      string
	OutcomeKind      string
	BlueprintID      string
	BlueprintVersion *int
	StartedByKind    string
	Attended         *bool
	GoalID           string
	GoalVersion      *int
	WallMS           *int64
	CostUSDFloor     *float64
	CostSteps        *int
	CostPartialSteps *int
	CostUnknownSteps *int
	TasksDone        *int
	TasksFailed      *int
	Retries          *int
	ReviewRounds     *int
	SecurityRounds   *int
	QARounds         *int
	TotalFailedGates *int
	SplitsDone       *int
	Escalations      *int
	UnverifiedSteps  *int
	VerifiedPass     *int
	VerifiedFail     *int
	Complete         bool
	Source           string // "recorder" | "backfill"
	RecordedAt       string
}

// RetroAgent is one row of retro_agent — one agent's share of one outcome,
// grouped by SLUG; AgentSlugKnown=false labels a legacy step row whose name
// resolved to no slug (S43 migrated the references; old rows may predate it).
type RetroAgent struct {
	ID               int64
	OutcomeID        int64
	Agent            string
	AgentSlugKnown   bool
	Steps            *int
	StepsDone        *int
	StepsRejected    *int
	StepsBlocked     *int
	VerifyPass       *int
	VerifyFail       *int
	VerifyUnverified *int
	CostUSDFloor     *float64
	CostPartialSteps *int
	CostUnknownSteps *int
	AgentMS          *int64
}

// RetroTemplate is one row of retro_template — the mutable half: the seed
// engine upserts system rows, and a body renders from THIS row at render
// time, never from code (rule 6).
type RetroTemplate struct {
	Kind      string
	Version   int
	Template  string
	Origin    string // "system" | "user"
	CreatedAt string
	UpdatedAt string
}

const retroOutcomeColumns = `id, component_root, loop_slug, terminal_key, outcome_kind,
	blueprint_id, blueprint_version, started_by_kind, attended, goal_id, goal_version,
	wall_ms, cost_usd_floor, cost_steps, cost_partial_steps, cost_unknown_steps,
	tasks_done, tasks_failed, retries, review_rounds, security_rounds, qa_rounds,
	total_failed_gates, splits_done, escalations, unverified_steps, verified_pass,
	verified_fail, complete, source, recorded_at`

func scanRetroOutcome(row interface{ Scan(...any) error }) (RetroOutcome, error) {
	var (
		o        RetroOutcome
		bpVer    sql.NullInt64
		att      sql.NullInt64
		goalVer  sql.NullInt64
		wall     sql.NullInt64
		floor    sql.NullFloat64
		cSteps   sql.NullInt64
		cPart    sql.NullInt64
		cUnk     sql.NullInt64
		tDone    sql.NullInt64
		tFail    sql.NullInt64
		retr     sql.NullInt64
		rev      sql.NullInt64
		sec      sql.NullInt64
		qa       sql.NullInt64
		gates    sql.NullInt64
		splits   sql.NullInt64
		esc      sql.NullInt64
		unver    sql.NullInt64
		vPass    sql.NullInt64
		vFail    sql.NullInt64
		complete int
	)
	err := row.Scan(&o.ID, &o.ComponentRoot, &o.LoopSlug, &o.TerminalKey, &o.OutcomeKind,
		&o.BlueprintID, &bpVer, &o.StartedByKind, &att, &o.GoalID, &goalVer,
		&wall, &floor, &cSteps, &cPart, &cUnk,
		&tDone, &tFail, &retr, &rev, &sec, &qa,
		&gates, &splits, &esc, &unver, &vPass, &vFail,
		&complete, &o.Source, &o.RecordedAt)
	if err != nil {
		return RetroOutcome{}, err
	}
	o.Complete = complete != 0
	o.BlueprintVersion = nullInt(bpVer)
	o.Attended = nullBool(att)
	o.GoalVersion = nullInt(goalVer)
	o.WallMS = nullInt64(wall)
	o.CostUSDFloor = nullFloat(floor)
	o.CostSteps = nullInt(cSteps)
	o.CostPartialSteps = nullInt(cPart)
	o.CostUnknownSteps = nullInt(cUnk)
	o.TasksDone = nullInt(tDone)
	o.TasksFailed = nullInt(tFail)
	o.Retries = nullInt(retr)
	o.ReviewRounds = nullInt(rev)
	o.SecurityRounds = nullInt(sec)
	o.QARounds = nullInt(qa)
	o.TotalFailedGates = nullInt(gates)
	o.SplitsDone = nullInt(splits)
	o.Escalations = nullInt(esc)
	o.UnverifiedSteps = nullInt(unver)
	o.VerifiedPass = nullInt(vPass)
	o.VerifiedFail = nullInt(vFail)
	return o, nil
}

func retroOutcomeArgs(o RetroOutcome) []any {
	return []any{o.ComponentRoot, o.LoopSlug, o.TerminalKey, o.OutcomeKind,
		o.BlueprintID, nullableIntPtr(o.BlueprintVersion), o.StartedByKind, boolPtrArg(o.Attended),
		o.GoalID, nullableIntPtr(o.GoalVersion), nullableInt64Ptr(o.WallMS),
		nullableFloatPtr(o.CostUSDFloor), nullableIntPtr(o.CostSteps), nullableIntPtr(o.CostPartialSteps),
		nullableIntPtr(o.CostUnknownSteps), nullableIntPtr(o.TasksDone), nullableIntPtr(o.TasksFailed),
		nullableIntPtr(o.Retries), nullableIntPtr(o.ReviewRounds), nullableIntPtr(o.SecurityRounds),
		nullableIntPtr(o.QARounds), nullableIntPtr(o.TotalFailedGates), nullableIntPtr(o.SplitsDone),
		nullableIntPtr(o.Escalations), nullableIntPtr(o.UnverifiedSteps), nullableIntPtr(o.VerifiedPass),
		nullableIntPtr(o.VerifiedFail), boolInt(o.Complete), o.Source, o.RecordedAt}
}

// RecordRetroOutcome writes one outcome and its agent rows in ONE transaction
// — a store can never hold an outcome whose agents half-landed. The insert
// rides ON CONFLICT DO NOTHING on the unique terminal key: a replay of the
// same terminal event returns the pre-existing id with inserted=false, which
// is how idempotence is a fact rather than a hope. RecordedAt is stamped here
// when the caller leaves it empty.
func (s *Store) RecordRetroOutcome(o RetroOutcome, agents []RetroAgent) (id int64, inserted bool, err error) {
	if o.ComponentRoot == "" || o.LoopSlug == "" {
		return 0, false, fmt.Errorf("store: retro outcome needs both a component root and a loop slug, got %q/%q", o.ComponentRoot, o.LoopSlug)
	}
	if o.TerminalKey == "" {
		return 0, false, errors.New("store: retro outcome needs a terminal_key — an unkeyed fact cannot be idempotent")
	}
	if o.OutcomeKind == "" || len(o.OutcomeKind) > 48 {
		return 0, false, fmt.Errorf("store: outcome_kind %q is empty or over 48 characters — the closed bounded shape, not free text", o.OutcomeKind)
	}
	if o.Source != "recorder" && o.Source != "backfill" {
		return 0, false, fmt.Errorf("store: retro source %q is neither recorder nor backfill", o.Source)
	}
	if o.RecordedAt == "" {
		o.RecordedAt = s.Now()
	}
	tx, err := s.w.Begin()
	if err != nil {
		return 0, false, fmt.Errorf("store: record retro outcome %s/%s: %w", o.ComponentRoot, o.LoopSlug, err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO retro_outcome (component_root, loop_slug, terminal_key, outcome_kind,
			blueprint_id, blueprint_version, started_by_kind, attended, goal_id, goal_version,
			wall_ms, cost_usd_floor, cost_steps, cost_partial_steps, cost_unknown_steps,
			tasks_done, tasks_failed, retries, review_rounds, security_rounds, qa_rounds,
			total_failed_gates, splits_done, escalations, unverified_steps, verified_pass,
			verified_fail, complete, source, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (component_root, loop_slug, terminal_key) DO NOTHING`,
		retroOutcomeArgs(o)...)
	if err != nil {
		return 0, false, fmt.Errorf("store: record retro outcome %s/%s: %w", o.ComponentRoot, o.LoopSlug, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 1 {
		inserted = true
		if id, err = res.LastInsertId(); err != nil {
			return 0, false, fmt.Errorf("store: record retro outcome %s/%s: %w", o.ComponentRoot, o.LoopSlug, err)
		}
	} else {
		// The key already exists — the replay answer names the row it
		// deduped against, never a fresh zero.
		if err := tx.QueryRow(`SELECT id FROM retro_outcome
			WHERE component_root = ? AND loop_slug = ? AND terminal_key = ?`,
			o.ComponentRoot, o.LoopSlug, o.TerminalKey).Scan(&id); err != nil {
			return 0, false, fmt.Errorf("store: record retro outcome %s/%s replay: %w", o.ComponentRoot, o.LoopSlug, err)
		}
	}
	for _, a := range agents {
		if a.Agent == "" {
			return 0, false, fmt.Errorf("store: retro outcome %s/%s: an agent row with no name", o.ComponentRoot, o.LoopSlug)
		}
		if _, err := tx.Exec(`INSERT INTO retro_agent (outcome_id, agent, agent_slug_known,
				steps, steps_done, steps_rejected, steps_blocked,
				verify_pass, verify_fail, verify_unverified,
				cost_usd_floor, cost_partial_steps, cost_unknown_steps, agent_ms)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT (outcome_id, agent) DO NOTHING`,
			id, a.Agent, boolInt(a.AgentSlugKnown),
			nullableIntPtr(a.Steps), nullableIntPtr(a.StepsDone), nullableIntPtr(a.StepsRejected),
			nullableIntPtr(a.StepsBlocked), nullableIntPtr(a.VerifyPass), nullableIntPtr(a.VerifyFail),
			nullableIntPtr(a.VerifyUnverified), nullableFloatPtr(a.CostUSDFloor),
			nullableIntPtr(a.CostPartialSteps), nullableIntPtr(a.CostUnknownSteps),
			nullableInt64Ptr(a.AgentMS)); err != nil {
			return 0, false, fmt.Errorf("store: record retro agent %q for %s/%s: %w", a.Agent, o.ComponentRoot, o.LoopSlug, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("store: record retro outcome %s/%s: %w", o.ComponentRoot, o.LoopSlug, err)
	}
	return id, inserted, nil
}

// RetroTally is THE count (RT4): how many rows of (root, kind) exist. There
// is no stored tally anywhere — this derived COUNT is the number the
// milestone rule and every "n of" read, and a test pins that it equals a raw
// SELECT of the same rows.
func (s *Store) RetroTally(root, kind string) (int, error) {
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM retro_outcome WHERE component_root = ? AND outcome_kind = ?`,
		root, kind).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: retro tally %s/%s: %w", root, kind, err)
	}
	return n, nil
}

// RetroLoopHasFacts answers whether ANY terminal fact row exists for
// (root, slug) — the existence check the backfill makes before writing a
// loop's history row (RT13's "skip M (already recorded)"). It cannot ride
// the unique terminal key: a backfill row's key is built from history
// fields and differs from the recorder's, so idempotent apply needs the
// per-loop probe. The UNIQUE (root, slug, terminal_key) index serves the
// lookup as its prefix.
func (s *Store) RetroLoopHasFacts(root, slug string) (bool, error) {
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM retro_outcome WHERE component_root = ? AND loop_slug = ? LIMIT 1`,
		root, slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: retro facts exist %s/%s: %w", root, slug, err)
	}
	return true, nil
}

// HasRetroLearning is the single-outcome half of the reconcile lock (T3
// RT4): does THIS outcome already carry its learning link? The reactor's
// write path checks before rendering so a milestone outcome never gets a
// second learning; UnlinkedMilestoneOutcomes is the set half, this is the
// row half, and the UNIQUE (outcome_id) on retro_learning is the lock
// underneath both.
func (s *Store) HasRetroLearning(outcomeID int64) (bool, error) {
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM retro_learning WHERE outcome_id = ? LIMIT 1`,
		outcomeID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: retro learning link %d: %w", outcomeID, err)
	}
	return true, nil
}

// RetroOutcomeByID reads one outcome by id. false is honest "no such row".
func (s *Store) RetroOutcomeByID(id int64) (RetroOutcome, bool, error) {
	row := s.r.QueryRow(`SELECT `+retroOutcomeColumns+` FROM retro_outcome WHERE id = ?`, id)
	o, err := scanRetroOutcome(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RetroOutcome{}, false, nil
	}
	if err != nil {
		return RetroOutcome{}, false, fmt.Errorf("store: retro outcome %d: %w", id, err)
	}
	return o, true, nil
}

// RetroFilter narrows ListRetroOutcomes. Every field is an exact match or a
// bound; empty means all. Agent filters through retro_agent (an outcome
// qualifies if it carries a row naming that agent). Cursor is an id: rows
// strictly BELOW it (older) are returned, newest first — the standard
// newest-first page walk of this store.
type RetroFilter struct {
	Root      string
	Agent     string
	Blueprint string
	Outcome   string
	Since     string // recorded_at >= Since (inclusive)
	Until     string // recorded_at <= Until (inclusive)
	Limit     int
	Cursor    int64 // 0 = first page
}

// ListRetroOutcomes is the paged read behind retro.list and the scorecard
// windows. Deterministic order (id DESC) is what makes the cursor honest.
func (s *Store) ListRetroOutcomes(f RetroFilter) ([]RetroOutcome, error) {
	q := `SELECT ` + retroOutcomeColumns + ` FROM retro_outcome o WHERE 1=1`
	args := []any{}
	if f.Root != "" {
		q += ` AND o.component_root = ?`
		args = append(args, f.Root)
	}
	if f.Outcome != "" {
		q += ` AND o.outcome_kind = ?`
		args = append(args, f.Outcome)
	}
	if f.Blueprint != "" {
		q += ` AND o.blueprint_id = ?`
		args = append(args, f.Blueprint)
	}
	if f.Since != "" {
		q += ` AND o.recorded_at >= ?`
		args = append(args, f.Since)
	}
	if f.Until != "" {
		q += ` AND o.recorded_at <= ?`
		args = append(args, f.Until)
	}
	if f.Cursor > 0 {
		q += ` AND o.id < ?`
		args = append(args, f.Cursor)
	}
	if f.Agent != "" {
		q += ` AND EXISTS (SELECT 1 FROM retro_agent a WHERE a.outcome_id = o.id AND a.agent = ?)`
		args = append(args, f.Agent)
	}
	q += ` ORDER BY o.id DESC`
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += ` LIMIT ?`
	args = append(args, limit)
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list retro outcomes: %w", err)
	}
	defer rows.Close()
	var out []RetroOutcome
	for rows.Next() {
		o, err := scanRetroOutcome(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list retro outcomes: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RetroAgentsFor reads one outcome's agent rows (ordered by id — insertion
// order, which is the recorder's grouping order).
func (s *Store) RetroAgentsFor(outcomeID int64) ([]RetroAgent, error) {
	rows, err := s.r.Query(`SELECT id, outcome_id, agent, agent_slug_known,
			steps, steps_done, steps_rejected, steps_blocked,
			verify_pass, verify_fail, verify_unverified,
			cost_usd_floor, cost_partial_steps, cost_unknown_steps, agent_ms
		FROM retro_agent WHERE outcome_id = ? ORDER BY id`, outcomeID)
	if err != nil {
		return nil, fmt.Errorf("store: retro agents for %d: %w", outcomeID, err)
	}
	defer rows.Close()
	var out []RetroAgent
	for rows.Next() {
		var (
			a       RetroAgent
			known   int
			steps   sql.NullInt64
			sDone   sql.NullInt64
			sRej    sql.NullInt64
			sBlk    sql.NullInt64
			vPass   sql.NullInt64
			vFail   sql.NullInt64
			vUnver  sql.NullInt64
			floor   sql.NullFloat64
			cPart   sql.NullInt64
			cUnk    sql.NullInt64
			agentMs sql.NullInt64
		)
		if err := rows.Scan(&a.ID, &a.OutcomeID, &a.Agent, &known,
			&steps, &sDone, &sRej, &sBlk, &vPass, &vFail, &vUnver,
			&floor, &cPart, &cUnk, &agentMs); err != nil {
			return nil, fmt.Errorf("store: retro agents for %d: %w", outcomeID, err)
		}
		a.AgentSlugKnown = known != 0
		a.Steps = nullInt(steps)
		a.StepsDone = nullInt(sDone)
		a.StepsRejected = nullInt(sRej)
		a.StepsBlocked = nullInt(sBlk)
		a.VerifyPass = nullInt(vPass)
		a.VerifyFail = nullInt(vFail)
		a.VerifyUnverified = nullInt(vUnver)
		a.CostUSDFloor = nullFloat(floor)
		a.CostPartialSteps = nullInt(cPart)
		a.CostUnknownSteps = nullInt(cUnk)
		a.AgentMS = nullInt64(agentMs)
		out = append(out, a)
	}
	return out, rows.Err()
}

// LinkRetroLearning records that this outcome's milestone filed learning
// `learningID`. Insert-only, like the fact it references: the link IS the
// lock reconcile uses (RT4) — whoever holds it has written; a second write
// finds the row and stops. inserted=false is "already linked".
func (s *Store) LinkRetroLearning(outcomeID, learningID int64) (bool, error) {
	res, err := s.w.Exec(`INSERT INTO retro_learning (outcome_id, learning_id, written_at)
		VALUES (?,?,?) ON CONFLICT (outcome_id) DO NOTHING`,
		outcomeID, learningID, s.Now())
	if err != nil {
		return false, fmt.Errorf("store: link retro learning %d/%d: %w", outcomeID, learningID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: link retro learning %d/%d: %w", outcomeID, learningID, err)
	}
	return n == 1, nil
}

// MilestoneOutcome is one unlinked outcome carrying its RANK — the
// milestone number it sits on, which is the exact n the learning's
// observed_count must name (M3). A reconcile-time RetroTally would name
// the GROUP's current total, not the row's milestone; the rank is what the
// recorder would have held at write time, so reconcile and the reactor
// write the same number at the same row (T3's WriteAt takes n, not an id).
type MilestoneOutcome struct {
	Outcome RetroOutcome
	Rank    int
}

// UnlinkedMilestoneOutcomes is the reconcile query (RT4): outcomes whose
// (root, kind) tally sits ON one of the milestones — counted by write order,
// the same order the milestone rule counts — that carry no retro_learning
// row. A crash between the Append and the Link leaves a row here exactly
// once; passing more milestones than rows exist costs nothing.
func (s *Store) UnlinkedMilestoneOutcomes(milestones []int) ([]MilestoneOutcome, error) {
	if len(milestones) == 0 {
		return nil, nil
	}
	q := `SELECT ` + retroOutcomeColumns + ` FROM (
			SELECT o.*, (SELECT COUNT(*) FROM retro_outcome o2
					WHERE o2.component_root = o.component_root AND o2.outcome_kind = o.outcome_kind
					  AND o2.id <= o.id) AS rank
			FROM retro_outcome o
		) WHERE rank IN (`
	args := []any{}
	for i, m := range milestones {
		if i > 0 {
			q += `,`
		}
		q += `?`
		args = append(args, m)
	}
	q += `) AND NOT EXISTS (SELECT 1 FROM retro_learning l WHERE l.outcome_id = id) ORDER BY id`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: unlinked milestone outcomes: %w", err)
	}
	defer rows.Close()
	var out []MilestoneOutcome
	for rows.Next() {
		o, err := scanRetroOutcome(rows)
		if err != nil {
			return nil, fmt.Errorf("store: unlinked milestone outcomes: %w", err)
		}
		// The rank re-counted per row: the window function already picked
		// these rows, this restates WHICH milestone each sits on (M3 needs
		// the number to travel with the row). Small by construction — the
		// rows are milestone hits without links, which is rare.
		var rank int
		if err := s.r.QueryRow(`SELECT COUNT(*) FROM retro_outcome o2
				WHERE o2.component_root = ? AND o2.outcome_kind = ? AND o2.id <= ?`,
			o.ComponentRoot, o.OutcomeKind, o.ID).Scan(&rank); err != nil {
			return nil, fmt.Errorf("store: unlinked milestone outcomes rank: %w", err)
		}
		out = append(out, MilestoneOutcome{Outcome: o, Rank: rank})
	}
	return out, rows.Err()
}

// RetroCoverage answers the coverage half of RT9: since when has anything
// been recorded, and how many distinct loops are held. (The dropped counter
// is the daemon's in-memory number — the store exposes none, so there is no
// second truth to drift.)
func (s *Store) RetroCoverage() (since string, loops int, err error) {
	var minAt sql.NullString
	var n int
	if err := s.r.QueryRow(`SELECT MIN(recorded_at),
			COUNT(DISTINCT component_root || char(31) || loop_slug) FROM retro_outcome`).
		Scan(&minAt, &n); err != nil {
		return "", 0, fmt.Errorf("store: retro coverage: %w", err)
	}
	return minAt.String, n, nil
}

// UpsertRetroTemplate lays one template row (the seed engine's path, T3's
// applier, and the only writer a system template has). Created_at rides the
// first insert; updated_at is stamped every write; origin comes from the
// caller — the engine passes "system", and the engine's own divergence rule
// (never overwrite a row that drifted from its fingerprint) sits ABOVE this,
// so this store half stays a plain upsert.
func (s *Store) UpsertRetroTemplate(kind string, version int, template, origin string) error {
	if kind == "" || template == "" || version <= 0 {
		return fmt.Errorf("store: retro template %q: kind, a positive version and a body are all required", kind)
	}
	if origin != "system" && origin != "user" {
		return fmt.Errorf("store: retro template %q: origin %q is neither system nor user", kind, origin)
	}
	now := s.Now()
	_, err := execRetroTemplateUpsert(s.w, kind, version, template, origin, now)
	return err
}

// UpsertRetroTemplateTx is the seed engine's door (T3): the same upsert
// inside the caller's transaction, so a template row and its ledger row
// commit together (SD3 — the verifyTemplateUpsertTx precedent).
func (s *Store) UpsertRetroTemplateTx(tx *sql.Tx, kind string, version int, template, origin string) error {
	if tx == nil {
		return errors.New("store: UpsertRetroTemplateTx needs the caller's transaction")
	}
	if kind == "" || template == "" || version <= 0 {
		return fmt.Errorf("store: retro template %q: kind, a positive version and a body are all required", kind)
	}
	if origin != "system" && origin != "user" {
		return fmt.Errorf("store: retro template %q: origin %q is neither system nor user", kind, origin)
	}
	now := s.Now()
	_, err := execRetroTemplateUpsert(tx, kind, version, template, origin, now)
	return err
}

type retroTemplateExec interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func execRetroTemplateUpsert(x retroTemplateExec, kind string, version int, template, origin, now string) (sql.Result, error) {
	res, err := x.Exec(`INSERT INTO retro_template (kind, version, template, origin, created_at, updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT (kind) DO UPDATE SET version = excluded.version,
			template = excluded.template, origin = excluded.origin, updated_at = excluded.updated_at`,
		kind, version, template, origin, now, now)
	if err != nil {
		return nil, fmt.Errorf("store: upsert retro template %q: %w", kind, err)
	}
	return res, nil
}

// GetRetroTemplate reads one template; false is honest "no such kind".
func (s *Store) GetRetroTemplate(kind string) (RetroTemplate, bool, error) {
	var t RetroTemplate
	err := s.r.QueryRow(`SELECT kind, version, template, origin, created_at, updated_at
		FROM retro_template WHERE kind = ?`, kind).
		Scan(&t.Kind, &t.Version, &t.Template, &t.Origin, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RetroTemplate{}, false, nil
	}
	if err != nil {
		return RetroTemplate{}, false, fmt.Errorf("store: retro template %q: %w", kind, err)
	}
	return t, true, nil
}

// GetRetroTemplateTx is the seed engine's read of one template, inside
// the caller's transaction (the Fingerprint door — a hand edit of the
// body column reads as a diverge through this exact row).
func (s *Store) GetRetroTemplateTx(tx *sql.Tx, kind string) (RetroTemplate, bool, error) {
	if tx == nil {
		return RetroTemplate{}, false, errors.New("store: GetRetroTemplateTx needs the caller's transaction")
	}
	var t RetroTemplate
	err := tx.QueryRow(`SELECT kind, version, template, origin, created_at, updated_at
		FROM retro_template WHERE kind = ?`, kind).
		Scan(&t.Kind, &t.Version, &t.Template, &t.Origin, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RetroTemplate{}, false, nil
	}
	if err != nil {
		return RetroTemplate{}, false, fmt.Errorf("store: retro template %q: %w", kind, err)
	}
	return t, true, nil
}

// ListRetroTemplates reads the set in kind order (stable for a golden).
func (s *Store) ListRetroTemplates() ([]RetroTemplate, error) {
	rows, err := s.r.Query(`SELECT kind, version, template, origin, created_at, updated_at
		FROM retro_template ORDER BY kind`)
	if err != nil {
		return nil, fmt.Errorf("store: list retro templates: %w", err)
	}
	defer rows.Close()
	var out []RetroTemplate
	for rows.Next() {
		var t RetroTemplate
		if err := rows.Scan(&t.Kind, &t.Version, &t.Template, &t.Origin, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: list retro templates: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountEscalationQuestions counts the loop's escalation questions — the
// recorder's only source for `escalations` (T0 §1.6 measured: no loop-level
// count exists). The signature is exact: a gate question whose options are
// exactly ["continue","abort"] is what escalate.go's CommitGate writes;
// gate questions with other options are gates, not escalations. The JSON
// shape is produced here (not hand-typed) so a json.Marshal change cannot
// silently stop the match.
func (s *Store) CountEscalationQuestions(sc Scope) (int, error) {
	if !sc.Valid() {
		return 0, fmt.Errorf("store: escalation count needs both halves of a scope, got %s", sc)
	}
	raw, err := json.Marshal([]string{"continue", "abort"})
	if err != nil {
		return 0, fmt.Errorf("store: escalation count: %w", err)
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM question
		WHERE component_root = ? AND loop_slug = ? AND kind = 'gate' AND options = ?`,
		sc.ComponentRoot, sc.LoopSlug, string(raw)).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: escalation count %s: %w", sc, err)
	}
	return n, nil
}

// nullInt maps a scanned NULL integer to a nil pointer — the NULL-not-zero
// round trip P31 demands.
func nullInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func nullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func nullBool(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

func nullableIntPtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableFloatPtr(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func boolPtrArg(p *bool) any {
	if p == nil {
		return nil
	}
	return boolInt(*p)
}
