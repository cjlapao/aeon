package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// budget.go — the store layer behind S55's budget records (T6). A budget is a
// running aggregate (spend so far across many loops and days) compared against
// several dimensions at once, which is why it is its own table and not a policy
// row (LC7): the policy engine decides a REQUEST at a call site and carries no
// spend state. CRUD plus the applicability read (loop → epic → component+root →
// workspace) and the append-only audit. Nothing enforces here (T7).
//
// # What this file does NOT do
//
// It never enforces a cap — that is T7's engine. It never reads or writes
// the old epic budget column in production (only the T6 migration helper
// did, LC13); a migrated budget is read from the `budget` row, not the dead
// column. And it never sums an unknown as zero (P31): that is
// budget_spend.go's law.

// Budget is one cap per (scope, subject, window). A NULL dimension is "no cap
// on that dimension"; at least one must be set (a row with no cap is a no-op).
// Zero is a real cap (nothing may be spent), so a zero cap is a cap, not the
// absence of one — the cap is a pointer, and NULL (never the zero value) is
// how "no cap" is spoken.
type Budget struct {
	ID      string
	Scope   string // workspace | component | epic | loop
	Subject string // 'workspace' / a component id / an epic id / a loop id
	Window  string // lifetime | day

	USD    *float64 // NULL = no cap on USD
	Tokens *int64   // NULL = no cap on tokens
	WallMs *int64   // NULL = no cap on wall time
	Steps  *int64   // NULL = no cap on steps

	UnknownPolicy string // proceed | stop
	WarnPct       *int   // NULL = no warn line
	Enabled       bool
	Origin        string // user | migrated
	CreatedAt     string
	UpdatedAt     string
}

// BudgetRefusal is a refused budget write. It is a result, not an error (DB19):
// a refused cap names the reason and the thing refused, and the caller renders
// it verbatim. The database's CHECKs are the backstop; these refusals are the
// person-facing sentence in front of them.
type BudgetRefusal struct {
	Kind   string
	Detail string
}

func (r BudgetRefusal) Error() string { return fmt.Sprintf("refused: %s · %s", r.Kind, r.Detail) }

// NewBudgetIn is a caller's request to create or update a budget. The caps are
// pointers so a missing cap stays NULL (never the zero value — zero is a real
// cap). A budget with no cap is refused; a negative cap is refused.
type NewBudgetIn struct {
	Scope         string
	Subject       string
	Window        string
	USD           *float64
	Tokens        *int64
	WallMs        *int64
	Steps         *int64
	UnknownPolicy string
	WarnPct       *int
	Enabled       *bool
}

// budgetDimensionsSet reports whether at least one dimension is set — a cap with
// no dimension is a no-op, refused.
func budgetDimensionsSet(in NewBudgetIn) bool {
	return in.USD != nil || in.Tokens != nil || in.WallMs != nil || in.Steps != nil
}

// validateBudgetRefusal is the closed set of person-facing refusals (LC8): a
// no-dimension budget, a negative cap, a subject that does not exist, a
// duplicate (scope, subject, window), or a workspace budget naming another
// subject. A nil result means "admitted by the store" (the CHECKs still
// backstop the database).
func (s *Store) validateBudgetRefusal(in NewBudgetIn) (BudgetRefusal, bool, error) {
	if !budgetDimensionsSet(in) {
		return BudgetRefusal{Kind: "no-dimension", Detail: "a budget needs at least one cap (usd, tokens, wall-time or steps)"}, true, nil
	}
	// The nils here are TYPED nils boxed into the any (a NewBudgetIn with
	// only a USD cap carries a nil *int64, not an absent value): `cap == nil`
	// is false for them, and the dereference below would panic. The nil check
	// belongs in each case, where the concrete pointer is visible.
	for name, cap := range map[string]any{"usd": in.USD, "tokens": in.Tokens, "wall-time": in.WallMs, "steps": in.Steps} {
		switch v := cap.(type) {
		case *float64:
			if v != nil && *v < 0 {
				return BudgetRefusal{Kind: "negative-cap", Detail: fmt.Sprintf("the %s cap is %v — a cap cannot be negative (0 is a real cap)", name, *v)}, true, nil
			}
		case *int64:
			if v != nil && *v < 0 {
				return BudgetRefusal{Kind: "negative-cap", Detail: fmt.Sprintf("the %s cap is %d — a cap cannot be negative (0 is a real cap)", name, *v)}, true, nil
			}
		}
	}
	if in.WarnPct != nil && (*in.WarnPct < 1 || *in.WarnPct > 99) {
		return BudgetRefusal{Kind: "warn-pct", Detail: fmt.Sprintf("the warn line is %d%% — it must be between 1 and 99", *in.WarnPct)}, true, nil
	}
	// A workspace budget names 'workspace'; a non-workspace budget names a real id.
	if in.Scope == "workspace" && in.Subject != "workspace" {
		return BudgetRefusal{Kind: "workspace-subject", Detail: "a workspace budget must name 'workspace' as its subject"}, true, nil
	}
	if in.Scope != "workspace" && in.Subject == "workspace" {
		return BudgetRefusal{Kind: "workspace-subject", Detail: "only a workspace budget may name 'workspace' as its subject"}, true, nil
	}
	// The subject must exist (no FK: a store check, LC8). A dangling subject is
	// a budget that can never apply — refused, not allowed to go inert.
	if in.Subject != "workspace" {
		switch in.Scope {
		case "component":
			if err := s.r.QueryRow(`SELECT 1 FROM component WHERE id = ?`, in.Subject).Scan(new(int)); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return BudgetRefusal{Kind: "unknown-subject", Detail: fmt.Sprintf("no component %s — a budget's subject must exist", in.Subject)}, true, nil
				}
				return BudgetRefusal{}, false, fmt.Errorf("store: check the budget subject %s: %w", in.Subject, err)
			}
		case "epic":
			if err := s.r.QueryRow(`SELECT 1 FROM epic WHERE id = ?`, in.Subject).Scan(new(int)); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return BudgetRefusal{Kind: "unknown-subject", Detail: fmt.Sprintf("no epic %s — a budget's subject must exist", in.Subject)}, true, nil
				}
				return BudgetRefusal{}, false, fmt.Errorf("store: check the budget subject %s: %w", in.Subject, err)
			}
		case "loop":
			if err := s.r.QueryRow(`SELECT 1 FROM loop WHERE id = ?`, in.Subject).Scan(new(int)); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return BudgetRefusal{Kind: "unknown-subject", Detail: fmt.Sprintf("no loop %s — a budget's subject must exist", in.Subject)}, true, nil
				}
				return BudgetRefusal{}, false, fmt.Errorf("store: check the budget subject %s: %w", in.Subject, err)
			}
		}
	}
	return BudgetRefusal{}, false, nil
}

// CreateBudget adds one budget and its `created` event in one transaction. A
// refused budget (no dimension, negative, unknown subject, duplicate, a
// workspace subject) answers a BudgetRefusal and writes nothing — no row, no
// event.
func (s *Store) CreateBudget(actor BudgetActor, in NewBudgetIn) (Budget, BudgetRefusal, error) {
	if ref, refused, err := s.validateBudgetRefusal(in); err != nil {
		return Budget{}, BudgetRefusal{}, err
	} else if refused {
		return Budget{}, ref, nil
	}
	// The duplicate (scope, subject, window) is a refusal, not an error — the
	// UNIQUE is the backstop, but a duplicate names the thing already there.
	var dup string
	if err := s.r.QueryRow(`SELECT id FROM budget WHERE scope=? AND subject_id=? AND window=?`, in.Scope, in.Subject, in.Window).Scan(&dup); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return Budget{}, BudgetRefusal{}, fmt.Errorf("store: check the budget %s/%s/%s: %w", in.Scope, in.Subject, in.Window, err)
		}
	}
	if dup != "" {
		return Budget{}, BudgetRefusal{Kind: "duplicate", Detail: fmt.Sprintf("a budget already exists for %s/%s/%s (%s)", in.Scope, in.Subject, in.Window, dup)}, nil
	}
	now := s.Now()
	b := Budget{
		ID:            newBudgetID(),
		Scope:         in.Scope,
		Subject:       in.Subject,
		Window:        in.Window,
		USD:           in.USD,
		Tokens:        in.Tokens,
		WallMs:        in.WallMs,
		Steps:         in.Steps,
		UnknownPolicy: orDefault(in.UnknownPolicy, "proceed"),
		WarnPct:       in.WarnPct,
		Enabled:       in.Enabled == nil || *in.Enabled,
		Origin:        "user",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: create budget: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO budget (id,scope,subject_id,window,usd_cap,tokens_cap,wall_ms_cap,steps_cap,unknown_policy,warn_pct,enabled,origin,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.Scope, b.Subject, b.Window, nullableF(b.USD), nullableI64(b.Tokens), nullableI64(b.WallMs), nullableI64(b.Steps),
		b.UnknownPolicy, nullableI(b.WarnPct), enabledInt(b.Enabled), b.Origin, b.CreatedAt, b.UpdatedAt); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: insert budget: %w", err)
	}
	if err := appendBudgetEvent(tx, BudgetEvent{BudgetID: b.ID, At: now, Kind: "created", ActorKind: actor.Kind, ActorID: actor.ID, Dimension: "", NewValue: b.USD}); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: create budget: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: create budget: %w", err)
	}
	return b, BudgetRefusal{}, nil
}

// UpdateBudget changes the caps of an existing budget and appends a `changed`
// event. A no-op change (nothing set differs) writes no event (LC11: a row that
// changes nothing writes nothing). A negative cap or a bad warn line is refused.
func (s *Store) UpdateBudget(actor BudgetActor, id string, in NewBudgetIn) (Budget, BudgetRefusal, error) {
	if ref, refused, err := s.validateBudgetRefusal(in); err != nil {
		return Budget{}, BudgetRefusal{}, err
	} else if refused {
		return Budget{}, ref, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: update budget %s: %w", id, err)
	}
	defer tx.Rollback()
	// Read the current budget; a missing one is ErrNotFound (a refused update
	// against a thing that is not there).
	cur, err := s.readBudget(tx, id)
	if err != nil {
		return Budget{}, BudgetRefusal{}, err
	}
	// Compare the set dimensions against the current; a no-op writes nothing.
	changed := false
	if in.USD != nil && (cur.USD == nil || *cur.USD != *in.USD) {
		changed = true
	}
	if in.Tokens != nil && (cur.Tokens == nil || *cur.Tokens != *in.Tokens) {
		changed = true
	}
	if in.WallMs != nil && (cur.WallMs == nil || *cur.WallMs != *in.WallMs) {
		changed = true
	}
	if in.Steps != nil && (cur.Steps == nil || *cur.Steps != *in.Steps) {
		changed = true
	}
	if in.UnknownPolicy != "" && in.UnknownPolicy != cur.UnknownPolicy {
		changed = true
	}
	if in.WarnPct != nil && (cur.WarnPct == nil || *cur.WarnPct != *in.WarnPct) {
		changed = true
	}
	if in.Enabled != nil && *in.Enabled != cur.Enabled {
		changed = true
	}
	if !changed {
		tx.Rollback()
		return cur, BudgetRefusal{}, nil
	}
	// Merge first, then write the MERGED values: writing `cur` before the
	// merge would store the old caps and hand back a Budget that lies about
	// the row — a cap change that is audited as `changed` but never lands.
	newUSD := orDefaultP(in.USD, cur.USD)
	newTokens := orDefaultP(in.Tokens, cur.Tokens)
	newWallMs := orDefaultP(in.WallMs, cur.WallMs)
	newSteps := orDefaultP(in.Steps, cur.Steps)
	now := s.Now()
	if _, err := tx.Exec(`UPDATE budget SET usd_cap=?, tokens_cap=?, wall_ms_cap=?, steps_cap=?, unknown_policy=?, warn_pct=?, enabled=?, updated_at=? WHERE id=?`,
		nullableF(newUSD), nullableI64(newTokens), nullableI64(newWallMs), nullableI64(newSteps),
		orDefault(in.UnknownPolicy, cur.UnknownPolicy), nullableI(orDefaultP(in.WarnPct, cur.WarnPct)), enabledInt(orDefaultB(in.Enabled, cur.Enabled)), now, id); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: update budget %s: %w", id, err)
	}
	cur.USD = newUSD
	cur.Tokens = newTokens
	cur.WallMs = newWallMs
	cur.Steps = newSteps
	if in.UnknownPolicy != "" {
		cur.UnknownPolicy = in.UnknownPolicy
	}
	cur.WarnPct = orDefaultP(in.WarnPct, cur.WarnPct)
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	cur.UpdatedAt = now
	if err := appendBudgetEvent(tx, BudgetEvent{BudgetID: id, At: now, Kind: "changed", ActorKind: actor.Kind, ActorID: actor.ID}); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: update budget %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: update budget %s: %w", id, err)
	}
	return cur, BudgetRefusal{}, nil
}

// RaiseBudget lifts one dimension's cap value (the only direction a cap may
// move while a loop is blocked on it, LC12) and appends a `raised` event. A
// raise that would lower the cap is refused — a cap only goes up here, never
// down. The dimension names which cap moves (usd | tokens | wall-time |
// steps); the value rides as a float for every dimension, the integer ones
// carrying whole numbers. A dimension the budget does not cap cannot be
// raised — there is no cap there to lift.
func (s *Store) RaiseBudget(actor BudgetActor, id string, dimension string, newCap *float64) (Budget, BudgetRefusal, error) {
	if newCap == nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: a raise needs a value", id)
	}
	if *newCap < 0 {
		return Budget{}, BudgetRefusal{Kind: "negative-cap", Detail: fmt.Sprintf("the %s cap is %v — a cap cannot be negative (0 is a real cap)", dimension, *newCap)}, nil
	}
	tx, err := s.w.Begin()
	if err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: %w", id, err)
	}
	defer tx.Rollback()
	cur, err := s.readBudget(tx, id)
	if err != nil {
		return Budget{}, BudgetRefusal{}, err
	}
	// The dimension's current cap, as the compare-and-lift reads it. A nil
	// current cap is a dimension the budget does not cap — a cap that is not
	// there cannot be raised.
	var current *float64
	var column string
	switch dimension {
	case "usd":
		current = cur.USD
		column = "usd_cap"
	case "tokens":
		if cur.Tokens != nil {
			v := float64(*cur.Tokens)
			current = &v
		}
		column = "tokens_cap"
	case "wall-time":
		if cur.WallMs != nil {
			v := float64(*cur.WallMs)
			current = &v
		}
		column = "wall_ms_cap"
	case "steps":
		if cur.Steps != nil {
			v := float64(*cur.Steps)
			current = &v
		}
		column = "steps_cap"
	default:
		tx.Rollback()
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: unknown dimension %q", id, dimension)
	}
	if current == nil {
		tx.Rollback()
		return Budget{}, BudgetRefusal{Kind: "no-cap", Detail: fmt.Sprintf("budget %s has no %s cap — a raise lifts a cap that is there", id, dimension)}, nil
	}
	// A raise that would lower the cap is refused: a cap only goes up here,
	// never down (LC12).
	if *newCap < *current {
		tx.Rollback()
		return Budget{}, BudgetRefusal{Kind: "lowering", Detail: fmt.Sprintf("raising %s %s to %v would lower the cap from %v — a cap only goes up here", id, dimension, *newCap, *current)}, nil
	}
	now := s.Now()
	switch dimension {
	case "usd":
		cur.USD = newCap
	case "tokens":
		v := int64(*newCap)
		cur.Tokens = &v
	case "wall-time":
		v := int64(*newCap)
		cur.WallMs = &v
	case "steps":
		v := int64(*newCap)
		cur.Steps = &v
	}
	cur.UpdatedAt = now
	if _, err := tx.Exec(`UPDATE budget SET `+column+`=?, updated_at=? WHERE id=?`, nullableF(newCap), now, id); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: %w", id, err)
	}
	if err := appendBudgetEvent(tx, BudgetEvent{BudgetID: id, At: now, Kind: "raised", ActorKind: actor.Kind, ActorID: actor.ID, Dimension: dimension, OldValue: current, NewValue: newCap}); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return Budget{}, BudgetRefusal{}, fmt.Errorf("store: raise budget %s: %w", id, err)
	}
	return cur, BudgetRefusal{}, nil
}

// RemoveBudget deletes the budget ROW only and appends a `removed` event (the
// audit stays — a budget's history outlives the budget, LC11). A missing budget
// is ErrNotFound, not a refusal.
func (s *Store) RemoveBudget(actor BudgetActor, id string) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: remove budget %s: %w", id, err)
	}
	defer tx.Rollback()
	if _, err := s.readBudget(tx, id); err != nil {
		return err
	}
	now := s.Now()
	if _, err := tx.Exec(`DELETE FROM budget WHERE id=?`, id); err != nil {
		return fmt.Errorf("store: remove budget %s: %w", id, err)
	}
	if err := appendBudgetEvent(tx, BudgetEvent{BudgetID: id, At: now, Kind: "removed", ActorKind: actor.Kind, ActorID: actor.ID}); err != nil {
		return fmt.Errorf("store: remove budget %s: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: remove budget %s: %w", id, err)
	}
	return nil
}

// BudgetActor is who acted on a budget, stamped by the caller (never from the
// request). Kind is closed (user|cli|system); ID is the peer uid or 'migration'.
type BudgetActor struct {
	Kind string
	ID   string
}

// GetBudget reads one budget by id. Absent is ErrNotFound.
func (s *Store) GetBudget(id string) (Budget, error) {
	return s.readBudget(s.r, id)
}

// readBudget reads one budget by id on the given reader (s.r or a tx).
func (s *Store) readBudget(r rowReader, id string) (Budget, error) {
	row := r.QueryRow(`SELECT id,scope,subject_id,window,usd_cap,tokens_cap,wall_ms_cap,steps_cap,unknown_policy,warn_pct,enabled,origin,created_at,updated_at
		FROM budget WHERE id=?`, id)
	b, err := scanBudget(row)
	if err != nil {
		return Budget{}, err
	}
	return b, nil
}

// ListBudgets reads every enabled budget, oldest first.
func (s *Store) ListBudgets() ([]Budget, error) {
	rows, err := s.r.Query(`SELECT id,scope,subject_id,window,usd_cap,tokens_cap,wall_ms_cap,steps_cap,unknown_policy,warn_pct,enabled,origin,created_at,updated_at
		FROM budget ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list budgets: %w", err)
	}
	defer rows.Close()
	var out []Budget
	for rows.Next() {
		b, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list budgets: %w", err)
	}
	return out, nil
}

// FindBudget reads the budget by its unique key (scope, subject, window) —
// the (scope,subject,window) triple the create-time CHECK makes a key. It is
// the budget.set door's create-or-update discriminator (T8): the same triple
// that CreateBudget would refuse as "duplicate" is the row UpdateBudget must
// find. found=false is the honest absence (no budget at that key yet), not an
// error.
func (s *Store) FindBudget(scope, subject, window string) (Budget, bool, error) {
	row := s.r.QueryRow(`SELECT id,scope,subject_id,window,usd_cap,tokens_cap,wall_ms_cap,steps_cap,unknown_policy,warn_pct,enabled,origin,created_at,updated_at
		FROM budget WHERE scope=? AND subject_id=? AND window=?`, scope, subject, window)
	b, err := scanBudget(row)
	if errors.Is(err, ErrNotFound) {
		// scanBudget converts the no-rows sentinel to the store's
		// ErrNotFound; that IS the honest absence this method reports as
		// found=false (S55 T8: the create-or-update discriminator depends
		// on it, and the epic view's derivation on the same path).
		return Budget{}, false, nil
	}
	if err != nil {
		return Budget{}, false, fmt.Errorf("store: find budget %s %s %s: %w", scope, subject, window, err)
	}
	return b, true, nil
}

// OpenLoopsForBudget lists the ids of the OPEN loops the budget applies to —
// the show door's "who is this cap over, right now" (T8): a loop-scope budget
// names its own loop; an epic budget, the loops whose goal works the epic; a
// component budget, the loops under its component or root; the workspace
// budget, every loop. Terminal loops (done, aborted) are not open and are not
// listed — a cap over a finished loop is history, visible in the audit, not a
// live threat.
func (s *Store) OpenLoopsForBudget(b Budget) ([]string, error) {
	q := `SELECT id FROM loop WHERE status NOT IN ('done','aborted')`
	args := []any{}
	switch b.Scope {
	case "loop":
		q += ` AND id = ?`
		args = append(args, b.Subject)
	case "epic":
		q += ` AND goal_id IS NOT NULL`
		q += ` AND goal_id IN (SELECT id FROM goal WHERE epic_id = ?)`
		args = append(args, b.Subject)
	case "component":
		// The subject is a component id (or a root path); the loops under it
		// are the ones whose component_root IS that id or IS its root — the
		// same RootOf walk ApplicableBudgets uses in the other direction. A
		// subject with no component row (ErrNotFound) keeps only the id: the
		// tree of a component that was never registered is the id itself.
		root, err := s.RootOf(b.Subject)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("store: open loops for budget %s: %w", b.ID, err)
		}
		q += ` AND component_root IN (?, ?)`
		args = append(args, b.Subject, root)
	case "workspace":
		// Every loop, every root: the workspace cap is the floor of floors.
	default:
		return nil, fmt.Errorf("store: open loops for budget %s: unknown scope %q", b.ID, b.Scope)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: open loops for budget %s: %w", b.ID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: open loops for budget %s: %w", b.ID, err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: open loops for budget %s: %w", b.ID, err)
	}
	return out, nil
}

// subjectTier is one tier of a subject's applicability set, in TIGHTNESS
// order (LC9): loop 0, epic 1, component 2, root 3, workspace 4. A subject
// that appears at two tiers (a component that is its own root) keeps the
// tighter one.
type subjectTier struct {
	subject string
	tier    int
}

const (
	tierLoop = iota
	tierEpic
	tierComponent
	tierRoot
	tierWorkspace
)

// filterEnabledBudgetsByTiers reads every enabled budget and keeps the ones
// whose subject is in the tier list, ordered tightest first (the tier the
// spend is checked against first, listed first; within a tier, the list's
// order). A disabled row is not a cap over anything and is excluded.
func (s *Store) filterEnabledBudgetsByTiers(tiers []subjectTier) ([]Budget, error) {
	rank := map[string]int{}
	for _, t := range tiers {
		if t.subject == "" {
			continue
		}
		if _, ok := rank[t.subject]; !ok {
			rank[t.subject] = t.tier
		}
	}
	all, err := s.ListBudgets()
	if err != nil {
		return nil, err
	}
	out := []Budget{}
	for _, b := range all {
		if !b.Enabled {
			continue
		}
		if _, ok := rank[b.Subject]; ok {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Subject] != rank[out[j].Subject] {
			return rank[out[i].Subject] < rank[out[j].Subject]
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// loopSubjectTiers walks a loop's applicability set as an ordered tier list:
// the loop, its epic (loop → goal → epic), its component, the component's
// root (S44-extra RootOf; a root with no component row omits the tier, the
// same tolerance the unsorted walk has), and the workspace.
func (s *Store) loopSubjectTiers(loopID string) ([]subjectTier, error) {
	var loop struct {
		ComponentRoot string
		GoalID        sql.NullString
	}
	if err := s.r.QueryRow(`SELECT component_root, goal_id FROM loop WHERE id=?`, loopID).Scan(&loop.ComponentRoot, &loop.GoalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, ErrNotFound)
		}
		return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
	}
	epicID := ""
	if loop.GoalID.Valid {
		var g sql.NullString
		if err := s.r.QueryRow(`SELECT epic_id FROM goal WHERE id=?`, loop.GoalID.String).Scan(&g); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
			}
		} else if g.Valid {
			epicID = g.String
		}
	}
	rootID := ""
	if loop.ComponentRoot != "" {
		r, err := s.RootOf(loop.ComponentRoot)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
			}
		} else {
			rootID = r
		}
	}
	return []subjectTier{
		{loopID, tierLoop},
		{epicID, tierEpic},
		{loop.ComponentRoot, tierComponent},
		{rootID, tierRoot},
		{"workspace", tierWorkspace},
	}, nil
}

// ApplicableBudgetsOrdered is ApplicableBudgets ordered TIGHTEST FIRST (LC9's
// tier order: loop, epic, component, root, workspace) — the order the
// budget.effective read serves, so the list a person reads is the order the
// enforcement side checks. It re-runs the same subject walk (one function,
// two callers, so the read and the gate cannot disagree about WHAT applies),
// only the ordering differs: ApplicableBudgets stays in the store's list
// order, where the engine's own sorting of hits makes the order irrelevant
// to the verdict.
func (s *Store) ApplicableBudgetsOrdered(loopID string) ([]Budget, error) {
	tiers, err := s.loopSubjectTiers(loopID)
	if err != nil {
		return nil, err
	}
	return s.filterEnabledBudgetsByTiers(tiers)
}

// ApplicableBudgetsForOrdered is ApplicableBudgetsFor ordered tightest first
// (epic, component, root, workspace — no loop tier: the loop does not exist
// yet, or the read is for a component or root directly): the budget.effective
// door's order for its componentId/rootId arms, the same "the read and the
// gate cannot disagree" argument as ApplicableBudgetsOrdered.
func (s *Store) ApplicableBudgetsForOrdered(componentRoot, epicID string) ([]Budget, error) {
	tiers := []subjectTier{{epicID, tierEpic}, {componentRoot, tierComponent}}
	if componentRoot != "" {
		r, err := s.RootOf(componentRoot)
		if err == nil {
			tiers = append(tiers, subjectTier{r, tierRoot})
		} else if !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("store: applicable budgets for start: %w", err)
		}
	}
	tiers = append(tiers, subjectTier{"workspace", tierWorkspace})
	return s.filterEnabledBudgetsByTiers(tiers)
}

// ApplicableBudgets is the set of budgets that apply to a loop (LC9): its own
// loop-scope budget, the epic's (loop → goal → epic), the component and its
// root (S44-extra RootOf), and the workspace — disabled rows excluded. A loop
// with no goal, no component or no root simply omits that tier; a budget a loop
// cannot reach is not a cap over it.
func (s *Store) ApplicableBudgets(loopID string) ([]Budget, error) {
	var loop struct {
		ComponentRoot string
		GoalID        sql.NullString
	}
	if err := s.r.QueryRow(`SELECT component_root, goal_id FROM loop WHERE id=?`, loopID).Scan(&loop.ComponentRoot, &loop.GoalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, ErrNotFound)
		}
		return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
	}
	// The epic the loop's goal belongs to (loop → goal → epic).
	epicID := ""
	if loop.GoalID.Valid {
		var g sql.NullString
		if err := s.r.QueryRow(`SELECT epic_id FROM goal WHERE id=?`, loop.GoalID.String).Scan(&g); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
			}
		} else if g.Valid {
			epicID = g.String
		}
	}
	// The root of the component tree (S44-extra): a component's budget covers its
	// tree, and the loop's component is one node in it. A root with NO component
	// row (a workspace that was never registered as a component) simply has no
	// component tier over it — the spend side already treats "no row" as a zero
	// (budget_spend's telemetryFor), and the applicability side must agree, or
	// every loop at a bare workspace root would be unpriceable.
	rootID, err := s.RootOf(loop.ComponentRoot)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("store: applicable budgets for %s: %w", loopID, err)
		}
		rootID = ""
	}
	// The subjects to look up, deduped: the loop, the epic, the component, its
	// root, and the workspace.
	subjects := map[string]struct{}{}
	add := func(s string) {
		if s != "" {
			subjects[s] = struct{}{}
		}
	}
	add(loopID)
	add(epicID)
	add(loop.ComponentRoot)
	add(rootID)
	subjects["workspace"] = struct{}{}

	// Read every enabled budget, then keep the ones whose (scope, subject) is in
	// the set. A loop-scope budget names the loop id; an epic budget names the
	// epic id; a component budget names the component or its root; the workspace
	// budget names 'workspace'.
	all, err := s.ListBudgets()
	if err != nil {
		return nil, err
	}
	var out []Budget
	for _, b := range all {
		if !b.Enabled {
			continue
		}
		if _, ok := subjects[b.Subject]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// RecordBudgetHit appends one budget_event{hit} row: the audit record that a
// cap was hit at a boundary (E1/E2/E3). The actor is the engine (system),
// never a person — a hit is the spend reaching the cap, not somebody acting.
// The numbers are the floor that tripped the dimension and the cap it
// tripped; exact says whether the floor was a total (P31: a floor with
// unknowns beside it is not one, and the audit says so).
func (s *Store) RecordBudgetHit(loopID, budgetID, dimension string, spendFloor, cap float64, exact bool) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record budget hit %s: %w", budgetID, err)
	}
	defer tx.Rollback()
	floor := spendFloor
	newCap := cap
	exactVal := exact
	if err := appendBudgetEvent(tx, BudgetEvent{
		BudgetID: budgetID, At: s.Now(), Kind: "hit",
		ActorKind: "system", ActorID: "engine",
		LoopID: loopID, Dimension: dimension,
		NewValue: &newCap, SpendFloor: &floor, Exact: &exactVal,
	}); err != nil {
		return fmt.Errorf("store: record budget hit %s: %w", budgetID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: record budget hit %s: %w", budgetID, err)
	}
	return nil
}

// RecordBudgetWarned appends one budget_event{warned} row: the store's
// memory of the last warn for a (budget, dimension), which is how the engine
// honours "once per period" without re-reading the audit on every call (the
// engine's dedup is LastBudgetWarned). The numbers are the floor that crossed
// the line and the cap it is climbing.
func (s *Store) RecordBudgetWarned(budgetID, dimension string, spendFloor, cap float64) error {
	tx, err := s.w.Begin()
	if err != nil {
		return fmt.Errorf("store: record budget warn %s: %w", budgetID, err)
	}
	defer tx.Rollback()
	floor := spendFloor
	newCap := cap
	if err := appendBudgetEvent(tx, BudgetEvent{
		BudgetID: budgetID, At: s.Now(), Kind: "warned",
		ActorKind: "system", ActorID: "engine",
		Dimension: dimension,
		NewValue:  &newCap, SpendFloor: &floor,
	}); err != nil {
		return fmt.Errorf("store: record budget warn %s: %w", budgetID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: record budget warn %s: %w", budgetID, err)
	}
	return nil
}

// LastBudgetWarned reads the newest warned row per (budget, dimension), keyed
// `budgetID|dimension` with its timestamp — the engine's once-per-period warn
// dedup (T7): a lifetime warn fires once, ever; a day warn fires again only
// after the rolling 24 h has passed since the last one.
func (s *Store) LastBudgetWarned() (map[string]time.Time, error) {
	rows, err := s.r.Query(`SELECT budget_id, dimension, at FROM budget_event WHERE kind='warned' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: last budget warned: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var bid, dim, at string
		if err := rows.Scan(&bid, &dim, &at); err != nil {
			return nil, fmt.Errorf("store: last budget warned: %w", err)
		}
		ts, err := time.Parse("2006-01-02T15:04:05.000Z", at)
		if err != nil {
			return nil, fmt.Errorf("store: last budget warned: parse the warn time %q: %w", at, err)
		}
		// Rows are oldest-first, so each key's last write is the newest warn.
		out[bid+"|"+dim] = ts
	}
	return out, rows.Err()
}

// ListBudgetEvents reads a budget's audit trail (LC11): created, changed,
// raised, hit, warned, removed — oldest first, at most `limit` rows (a
// non-positive limit reads them all). It is the reader a raise's proof goes
// through: a `raised` row names the cap it lifted from and the one it set,
// a `hit` row names the floor that tripped the dimension and whether that
// floor was a total — the history a person reads when a cap's numbers are in
// question, and the one T8's socket read will serve.
func (s *Store) ListBudgetEvents(budgetID string, limit int) ([]BudgetEvent, error) {
	q := `SELECT budget_id,at,kind,actor_kind,actor_id,loop_id,dimension,old_value,new_value,spend_floor,exact
		FROM budget_event WHERE budget_id=? ORDER BY id`
	args := []any{budgetID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list budget events: %w", err)
	}
	defer rows.Close()
	var out []BudgetEvent
	for rows.Next() {
		var e BudgetEvent
		var old, neu, floor sql.NullFloat64
		var exact sql.NullInt64
		if err := rows.Scan(&e.BudgetID, &e.At, &e.Kind, &e.ActorKind, &e.ActorID, &e.LoopID, &e.Dimension, &old, &neu, &floor, &exact); err != nil {
			return nil, fmt.Errorf("store: list budget events: %w", err)
		}
		if old.Valid {
			v := old.Float64
			e.OldValue = &v
		}
		if neu.Valid {
			v := neu.Float64
			e.NewValue = &v
		}
		if floor.Valid {
			v := floor.Float64
			e.SpendFloor = &v
		}
		if exact.Valid {
			v := exact.Int64 == 1
			e.Exact = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ApplicableBudgetsFor is the budget set that WOULD apply to a loop that is
// about to start (E3): the epic's budget, the component and its root, and the
// workspace — ApplicableBudgets minus the loop row, because at start the loop
// does not exist yet. A start with no epic or no component simply omits that
// tier; a budget a loop cannot reach is not a cap over it.
func (s *Store) ApplicableBudgetsFor(componentRoot, epicID string) ([]Budget, error) {
	subjects := map[string]struct{}{}
	add := func(x string) {
		if x != "" {
			subjects[x] = struct{}{}
		}
	}
	add(epicID)
	add(componentRoot)
	if componentRoot != "" {
		// "No component row" means no component tier (the same tolerance the
		// live loop's applicability has): a start at a bare workspace root
		// must be priceable, not refused as unreadable.
		rootID, err := s.RootOf(componentRoot)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("store: applicable budgets for start: %w", err)
			}
		} else {
			add(rootID)
		}
	}
	subjects["workspace"] = struct{}{}
	all, err := s.ListBudgets()
	if err != nil {
		return nil, err
	}
	var out []Budget
	for _, b := range all {
		if !b.Enabled {
			continue
		}
		if _, ok := subjects[b.Subject]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// scanBudget reads one budget row. The caps ride sql.Null* so a NULL cap never
// becomes the zero value by accident — zero is a real cap, P31.
func scanBudget(row interface{ Scan(...any) error }) (Budget, error) {
	var (
		b      Budget
		usd    sql.NullFloat64
		tok    sql.NullInt64
		wall   sql.NullInt64
		steps  sql.NullInt64
		warn   sql.NullInt64
		enable sql.NullInt64
	)
	err := row.Scan(&b.ID, &b.Scope, &b.Subject, &b.Window, &usd, &tok, &wall, &steps, &b.UnknownPolicy, &warn, &enable, &b.Origin, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Budget{}, fmt.Errorf("store: budget: %w", ErrNotFound)
		}
		return Budget{}, fmt.Errorf("store: read budget: %w", err)
	}
	if usd.Valid {
		v := usd.Float64
		b.USD = &v
	}
	if tok.Valid {
		v := tok.Int64
		b.Tokens = &v
	}
	if wall.Valid {
		v := wall.Int64
		b.WallMs = &v
	}
	if steps.Valid {
		v := steps.Int64
		b.Steps = &v
	}
	if warn.Valid {
		v := int(warn.Int64)
		b.WarnPct = &v
	}
	b.Enabled = enable.Int64 == 1
	return b, nil
}

// nullableF/nullableI/nullableI64 render a nil pointer as SQL NULL and a set one
// as its value (so a missing cap stays NULL, never the zero value).
func nullableF(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullableI64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullableI(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
func enabledInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func nullableBool(v *bool) any {
	if v == nil {
		return nil
	}
	if *v {
		return 1
	}
	return 0
}
func orDefaultP[T any](v, def *T) *T {
	if v != nil {
		return v
	}
	return def
}
func orDefaultB(v *bool, def bool) bool {
	if v != nil {
		return *v
	}
	return def
}

// f64 reads the current usd cap for a raised event's old_value (a raised cap
// always had a usd value to lift from).
func f64(b Budget) *float64 {
	return b.USD
}

// rowReader is satisfied by *sql.DB and *sql.Tx — readBudget reads a budget the
// same way whether it is in the read pool or in a write transaction.
type rowReader interface {
	QueryRow(query string, args ...any) *sql.Row
}

// newBudgetID mints a budget id. A migrated budget keeps the migration's
// 'bud-<epic id>' shape; a user budget is 'bud-' beside a fresh id.
func newBudgetID() string { return "bud-" + newID() }

// BudgetEvent is one row of budget_event (the append-only audit, LC11). The
// values are numbers (a cap, a spend floor), never user text. SpendFloor and
// Exact ride the hit/warned rows: the floor is the spend that triggered the
// row (a FLOOR, P31) and Exact says whether it was a total — a floor with
// unknowns beside it is not one, and the audit says so rather than the reader
// having to guess.
type BudgetEvent struct {
	BudgetID   string
	At         string
	Kind       string // created | changed | removed | raised | hit | warned
	ActorKind  string // user | cli | system
	ActorID    string
	LoopID     string
	Dimension  string
	OldValue   *float64
	NewValue   *float64
	SpendFloor *float64
	Exact      *bool
}

// appendBudgetEvent writes one budget_event row, on the shared transaction.
// A row that does not change the budget writes nothing (the caller decides).
func appendBudgetEvent(tx *sql.Tx, e BudgetEvent) error {
	if _, err := tx.Exec(`INSERT INTO budget_event (budget_id,at,kind,actor_kind,actor_id,loop_id,dimension,old_value,new_value,spend_floor,exact)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.BudgetID, e.At, e.Kind, e.ActorKind, e.ActorID, e.LoopID, e.Dimension,
		nullableF(e.OldValue), nullableF(e.NewValue), nullableF(e.SpendFloor), nullableBool(e.Exact)); err != nil {
		return fmt.Errorf("store: append budget event: %w", err)
	}
	return nil
}
