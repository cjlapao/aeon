package store

// mergequeue.go — S60's queue rows: items, the append-only history, and the
// per-repository config (design MQ3/MQ4). Every invariant that matters is
// physical (the partial unique indexes and the event triggers from
// 20261104044821); this file adds the SENTENCES in front of them: a refusal
// names the holder, the current status, the existing item — a constraint
// failure names a column pair (DB19). The store is the only writer (I2); the
// driver (T7) and the service (T8) call these methods and nothing else.
//
// The state machine is NOT here: which transitions are legal is pure
// (internal/mergequeue, T3); the store only enforces the guarded write
// (UPDATE … WHERE status = from) and the physical slots. A lost race is a
// refusal with the current status, never an overwrite.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The closed vocabularies, Go mirrors of the migration's CHECKs — the lock
// tests compare these against the DDL text (the ProposalKinds pattern).
var (
	MergeStatusSet = []string{"queued", "catching_up", "verifying", "ready", "merging", "merged", "conflicted", "failed", "removed"}
	// MergeSlotStatusSet is the one-slot-per-(root, base) set: an item in one
	// of these holds the landing slot. `conflicted` deliberately releases it
	// (OD2 — a conflict must not stall every item behind it).
	MergeSlotStatusSet = []string{"catching_up", "verifying", "ready", "merging"}
	// MergeOpenStatusSet is the one-open-item-per-branch set: a terminal row
	// never blocks re-enqueueing the same branch.
	MergeOpenStatusSet = []string{"queued", "catching_up", "verifying", "ready", "merging", "conflicted"}
	MergeReasonSet     = []string{"denied", "conflict", "gate-failed", "head-moved", "protected", "forge", "git", "stale-exhausted", "removed-by-person", "loop-stopped", "unknown"}
	MergeEventKindSet  = []string{"transition", "reorder", "config"}
	// MergeEnqueuedBySet is who may have asked for the enqueue (the item's
	// column); MergeEventActorSet is who may have acted afterwards — the two
	// differ on purpose: 'proposal' enqueues, 'agent' acts (a resolution
	// loop), and neither crosses.
	MergeEnqueuedBySet = []string{"user", "cli", "blueprint", "daemon", "proposal"}
	MergeEventActorSet = []string{"user", "cli", "blueprint", "daemon", "agent"}
)

func inStringSet(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// MergeActor is the queue's author: the same closed pair as store.Actor in
// shape, a DIFFERENT closed set in content (the queue admits cli/blueprint/
// daemon, which the records tier does not). The kernel fills it — for socket
// writes from the peer, {blueprint, <slug>} for queue.enqueue, {daemon,
// mergequeue} for the driver. Never from params.
type MergeActor struct {
	Kind string
	ID   string
}

func (a MergeActor) refusal() (MergeRefusal, bool) {
	if !inStringSet(MergeEventActorSet, a.Kind) || a.ID == "" {
		return MergeRefusal{Kind: "bad-actor", Reason: fmt.Sprintf("the actor %q/%q is not in the queue's closed set (user, cli, blueprint, daemon, agent, each with an id)", a.Kind, a.ID)}, true
	}
	return MergeRefusal{}, false
}

// MergeRefusal is a refused queue write — a result, not an error (DB19): it
// names what it hit (the holder, the current status, the existing item) so
// the surface renders it verbatim. The database's indexes are the backstop;
// these refusals are the sentence in front of them.
type MergeRefusal struct {
	Kind    string // duplicate-open | slot-taken | stale-state | unknown-root | bad-actor | not-queued | unknown-item | detail-text | unknown-field | bad-config | no-op
	Reason  string
	Holder  string // slot-taken: the item holding the slot
	Current string // stale-state: the status found instead of the guard
	Item    string // duplicate-open: the live item already on the branch
}

func (r MergeRefusal) Error() string { return fmt.Sprintf("refused: %s · %s", r.Kind, r.Reason) }

// MergeItem is one row of merge_queue_item. The SHAs and slugs are the queue's
// whole memory — head_sha is what the gate certified against (MQ11's
// verified_sha binding), base_sha is the base the last integration saw.
type MergeItem struct {
	ID                string
	ComponentRoot     string
	BaseBranch        string
	Branch            string
	LoopSlug          string // "" = NULL (plain TEXT, no FK — F20)
	HeadSHA           string
	BaseSHA           string
	IntegrationBranch string
	IntegrationSHA    string
	PRNumber          *int // NULL until the landing PR exists
	VerifiedSHA       string
	LandedSHA         string
	LandingLoopSlug   string
	ResolveLoopSlug   string
	Status            string
	ReasonKind        string // "" = NULL
	Position          int64
	Attended          bool
	CatchUps          int
	EnqueuedByKind    string
	EnqueuedByID      string
	CreatedAt         string
	UpdatedAt         string
}

// NewMergeItem is EnqueueMergeItem's input: the caller supplies the identity
// (branch, base, head), never a position (the store assigns it), never a
// status (always 'queued'), never a timestamp (the store's clock writes).
type NewMergeItem struct {
	ComponentRoot  string
	BaseBranch     string
	Branch         string
	LoopSlug       string
	HeadSHA        string
	BaseSHA        string
	Attended       bool
	EnqueuedByKind string
	EnqueuedByID   string
}

// TransitionFacts are the fields a transition (or SetMergeItemFields) may
// carry; the zero string / nil pointer means "leave the column alone" —
// detail-side fields (Reason, ConflictPaths) travel with the states that
// produce them. ConflictPaths are PATHS only (SEC-033): ≤ 100 of them, each
// ≤ 512 bytes with no control characters, refused before SQL.
type TransitionFacts struct {
	HeadSHA           string
	BaseSHA           string
	IntegrationBranch string
	IntegrationSHA    string
	PRNumber          *int
	VerifiedSHA       string
	LandedSHA         string
	LandingLoopSlug   string
	ResolveLoopSlug   string
	Reason            string
	BumpCatchUps      bool
	ConflictPaths     []string
}

// eventDetail renders TransitionFacts into the event's detail JSON — the ONE
// function that builds event text (P3/SEC-033): ids, counts, enums, SHAs and
// paths only. No map iteration (the bytes must be stable), no prose channel.
func eventDetail(f TransitionFacts) (string, error) {
	if err := validateConflictPaths(f.ConflictPaths); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("{")
	add := func(kv string) {
		if b.Len() > 1 {
			b.WriteString(",")
		}
		b.WriteString(kv)
	}
	if f.HeadSHA != "" {
		add(`"head_sha":"` + f.HeadSHA + `"`)
	}
	if f.BaseSHA != "" {
		add(`"base_sha":"` + f.BaseSHA + `"`)
	}
	if f.IntegrationBranch != "" {
		add(`"integration_branch":"` + f.IntegrationBranch + `"`)
	}
	if f.IntegrationSHA != "" {
		add(`"integration_sha":"` + f.IntegrationSHA + `"`)
	}
	if f.PRNumber != nil {
		add(fmt.Sprintf(`"pr_number":%d`, *f.PRNumber))
	}
	if f.VerifiedSHA != "" {
		add(`"verified_sha":"` + f.VerifiedSHA + `"`)
	}
	if f.LandedSHA != "" {
		add(`"landed_sha":"` + f.LandedSHA + `"`)
	}
	if f.LandingLoopSlug != "" {
		add(`"landing_loop_slug":"` + f.LandingLoopSlug + `"`)
	}
	if f.ResolveLoopSlug != "" {
		add(`"resolve_loop_slug":"` + f.ResolveLoopSlug + `"`)
	}
	if f.Reason != "" {
		add(`"reason_kind":"` + f.Reason + `"`)
	}
	if f.BumpCatchUps {
		add(`"catch_ups":"bump"`)
	}
	if len(f.ConflictPaths) > 0 {
		if b.Len() > 1 {
			b.WriteString(",")
		}
		b.WriteString(`"conflict_paths":[`)
		for i, p := range f.ConflictPaths {
			if i > 0 {
				b.WriteString(",")
			}
			quoteJSON(&b, p)
		}
		b.WriteString("]")
	}
	b.WriteString("}")
	return b.String(), nil
}

// quoteJSON writes a JSON string with the only escapes the path validator
// still allows for (quotes and backslashes — control characters never get
// this far).
func quoteJSON(b *strings.Builder, s string) {
	b.WriteString(`"`)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteString(`"`)
}

// validateConflictPaths is the SEC-033 gate: ≤ 100 paths, each ≤ 512 BYTES,
// no control characters (< 0x20 or 0x7F). A hunk body cannot be a path — the
// newline alone ends it here.
func validateConflictPaths(paths []string) error {
	if len(paths) > 100 {
		return MergeRefusal{Kind: "detail-text", Reason: fmt.Sprintf("%d conflict paths — at most 100 are recorded", len(paths))}
	}
	for _, p := range paths {
		if len(p) == 0 || len(p) > 512 {
			return MergeRefusal{Kind: "detail-text", Reason: "a conflict path must be 1–512 bytes"}
		}
		for i := 0; i < len(p); i++ {
			if p[i] < 0x20 || p[i] == 0x7f {
				return MergeRefusal{Kind: "detail-text", Reason: "conflict paths carry no control characters — hunk text never enters the event detail (SEC-033)"}
			}
		}
	}
	return nil
}

// MergeQueueEvent is one merge_queue_event row. Rows are never updated, never
// deleted (the migration's triggers); Kind is transition|reorder|config.
type MergeQueueEvent struct {
	ID            int64
	ComponentRoot string
	ItemID        string // "" = NULL (a config change)
	Kind          string
	FromStatus    string // "" = NULL (the first event of an item's life)
	ToStatus      string
	ReasonKind    string
	ActorKind     string
	ActorID       string
	Detail        string
	At            string
}

// MergeConfig is merge_queue_config's row for one repository. Absent means
// mode 'off' (OD3): ReadMergeConfig answers an Absent config with exactly the
// migration's defaults, and nothing downstream invents a policy.
type MergeConfig struct {
	ComponentRoot  string
	Mode           string // off | manual | auto
	ConflictPolicy string // ask | resolve | block
	LandMethod     string // squash | merge
	MaxCatchUps    int
	UpdatedAt      string
	UpdatedByKind  string
	UpdatedByID    string
	Absent         bool
}

// NewMergeConfig is WriteMergeConfig's input. Every field is required — there
// is no partial patch, so the row after the write is exactly what the caller
// intended (one event says so).
type NewMergeConfig struct {
	ComponentRoot  string
	Mode           string
	ConflictPolicy string
	LandMethod     string
	MaxCatchUps    int
}

func (c NewMergeConfig) refusal() (MergeRefusal, bool) {
	switch c.Mode {
	case "off", "manual", "auto":
	default:
		return MergeRefusal{Kind: "bad-config", Reason: fmt.Sprintf("mode %q is not off|manual|auto", c.Mode)}, true
	}
	switch c.ConflictPolicy {
	case "ask", "resolve", "block":
	default:
		return MergeRefusal{Kind: "bad-config", Reason: fmt.Sprintf("conflict_policy %q is not ask|resolve|block", c.ConflictPolicy)}, true
	}
	switch c.LandMethod {
	case "squash", "merge":
	default:
		return MergeRefusal{Kind: "bad-config", Reason: fmt.Sprintf("land_method %q is not squash|merge", c.LandMethod)}, true
	}
	if c.MaxCatchUps < 1 || c.MaxCatchUps > 10 {
		return MergeRefusal{Kind: "bad-config", Reason: fmt.Sprintf("max_catch_ups %d is outside 1..10", c.MaxCatchUps)}, true
	}
	if c.ComponentRoot == "" {
		return MergeRefusal{Kind: "bad-config", Reason: "a merge config names its component_root"}, true
	}
	return MergeRefusal{}, false
}

// MergeFilter narrows ListMergeItems. An empty BaseBranch lists every base;
// an empty Statuses lists every status (pass MergeOpenStatusSet for "the
// queue as a person reads it").
type MergeFilter struct {
	ComponentRoot string // required — the queue is per-repository
	BaseBranch    string
	Statuses      []string
}

const mergeItemCols = `id, component_root, base_branch, branch, loop_slug, head_sha, base_sha,
	integration_branch, integration_sha, pr_number, verified_sha, landed_sha,
	landing_loop_slug, resolve_loop_slug, status, reason_kind, position, attended,
	catch_ups, enqueued_by_kind, enqueued_by_id, created_at, updated_at`

func scanMergeItem(sc interface{ Scan(dest ...any) error }) (MergeItem, error) {
	var (
		it       MergeItem
		loop     sql.NullString
		integ    sql.NullString
		integSHA sql.NullString
		pr       sql.NullInt64
		verified sql.NullString
		landed   sql.NullString
		landLoop sql.NullString
		resLoop  sql.NullString
		reason   sql.NullString
	)
	err := sc.Scan(&it.ID, &it.ComponentRoot, &it.BaseBranch, &it.Branch, &loop, &it.HeadSHA, &it.BaseSHA,
		&integ, &integSHA, &pr, &verified, &landed, &landLoop, &resLoop,
		&it.Status, &reason, &it.Position, &it.Attended,
		&it.CatchUps, &it.EnqueuedByKind, &it.EnqueuedByID, &it.CreatedAt, &it.UpdatedAt)
	if err != nil {
		return MergeItem{}, err
	}
	it.LoopSlug, it.IntegrationBranch, it.IntegrationSHA = loop.String, integ.String, integSHA.String
	it.VerifiedSHA, it.LandedSHA = verified.String, landed.String
	it.LandingLoopSlug, it.ResolveLoopSlug, it.ReasonKind = landLoop.String, resLoop.String, reason.String
	if pr.Valid {
		v := int(pr.Int64)
		it.PRNumber = &v
	}
	return it, nil
}

// EnqueueMergeItem appends one item to the queue: position = max(open
// positions of the (root, base) run) + 1000 — gaps let ReorderMergeItems move
// a row without rewriting the others. It refuses before SQL, in this order:
// a bad actor, a component_root that does not exist, an empty branch or base,
// and a branch that is ALREADY OPEN (the refusal names the live item). The
// merge_queue_one_open index is the backstop for a same-branch race; the
// 'queued' event (from NULL) rides in the same transaction.
func (s *Store) EnqueueMergeItem(in NewMergeItem, actor MergeActor) (MergeItem, error) {
	if r, bad := actor.refusal(); bad {
		return MergeItem{}, r
	}
	if !inStringSet(MergeEnqueuedBySet, in.EnqueuedByKind) || in.EnqueuedByID == "" {
		return MergeItem{}, MergeRefusal{Kind: "bad-actor", Reason: fmt.Sprintf("enqueued_by %q/%q is not in the closed set (user, cli, blueprint, daemon, proposal, each with an id)", in.EnqueuedByKind, in.EnqueuedByID)}
	}
	if strings.TrimSpace(in.Branch) == "" || strings.TrimSpace(in.BaseBranch) == "" {
		return MergeItem{}, MergeRefusal{Kind: "unknown-field", Reason: "a queue item names both the branch it carries and the base branch it lands on"}
	}
	if in.HeadSHA == "" {
		return MergeItem{}, MergeRefusal{Kind: "unknown-field", Reason: "a queue item carries the head SHA its branch points at right now"}
	}
	if err := s.checkRoot(in.ComponentRoot); err != nil {
		return MergeItem{}, *err
	}

	tx, err := s.w.Begin()
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: begin merge enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var dup string
	err = tx.QueryRow(`SELECT id FROM merge_queue_item
		WHERE component_root = ? AND branch = ? AND status IN ('queued','catching_up','verifying','ready','merging','conflicted')`,
		in.ComponentRoot, in.Branch).Scan(&dup)
	if err == nil {
		return MergeItem{}, MergeRefusal{Kind: "duplicate-open", Item: dup,
			Reason: fmt.Sprintf("branch %s is already queued as %s — one open item per branch (a merged row does not block; wait for it or remove it)", in.Branch, dup)}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return MergeItem{}, fmt.Errorf("store: check the open queue for %s: %w", in.Branch, err)
	}

	var pos sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(position) FROM merge_queue_item
		WHERE component_root = ? AND base_branch = ? AND status IN ('queued','catching_up','verifying','ready','merging','conflicted')`,
		in.ComponentRoot, in.BaseBranch).Scan(&pos); err != nil {
		return MergeItem{}, fmt.Errorf("store: read the queue tail for base %s: %w", in.BaseBranch, err)
	}
	position := int64(1000)
	if pos.Valid {
		position = pos.Int64 + 1000
	}

	now := s.Now()
	id := "mq-" + newID()
	if _, err := tx.Exec(`INSERT INTO merge_queue_item
		(id, component_root, base_branch, branch, loop_slug, head_sha, base_sha, status, position, attended, enqueued_by_kind, enqueued_by_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,'queued',?,?,?,?,?,?)`,
		id, in.ComponentRoot, in.BaseBranch, in.Branch, nullable(in.LoopSlug), in.HeadSHA, in.BaseSHA,
		position, b2i(in.Attended), in.EnqueuedByKind, in.EnqueuedByID, now, now); err != nil {
		return MergeItem{}, fmt.Errorf("store: enqueue merge item for %s: %w", in.Branch, err)
	}
	if err := appendMergeEvent(tx, in.ComponentRoot, &id, "transition", "", "queued", "", actor, "{}", now); err != nil {
		return MergeItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeItem{}, fmt.Errorf("store: commit merge enqueue: %w", err)
	}
	return s.ReadMergeItem(id)
}

// ReadMergeItem answers one item by id; a missing id is ErrNotFound.
func (s *Store) ReadMergeItem(id string) (MergeItem, error) {
	row := s.r.QueryRow(`SELECT `+mergeItemCols+` FROM merge_queue_item WHERE id = ?`, id)
	it, err := scanMergeItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeItem{}, fmt.Errorf("store: merge item %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: read merge item %s: %w", id, err)
	}
	return it, nil
}

// MergeItemTerminal is the reconcile read (S60 MQ7): a queue item is terminal
// when its status is merged, failed or removed — the same closed set the
// state machine names terminal, restated here so the loop package's worktree
// reconciliation never imports the queue's vocabulary. An absent item answers
// TERMINAL: no row means no live landing holding the integration worktree, so
// the reconcile settles the row rather than rebuilding a worktree for an item
// that cannot exist.
func (s *Store) MergeItemTerminal(id string) (bool, error) {
	var status string
	err := s.r.QueryRow(`SELECT status FROM merge_queue_item WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: merge item terminal read %s: %w", id, err)
	}
	switch status {
	case "merged", "failed", "removed":
		return true, nil
	}
	return false, nil
}

// ListMergeItems lists a repository's queue in queue order (position asc, id
// as the tiebreaker so the answer is total). An empty result is an empty
// slice, not an error.
func (s *Store) ListMergeItems(f MergeFilter) ([]MergeItem, error) {
	if f.ComponentRoot == "" {
		return nil, MergeRefusal{Kind: "unknown-root", Reason: "the merge queue is per-repository; a listing names its component_root"}
	}
	q := `SELECT ` + mergeItemCols + ` FROM merge_queue_item WHERE component_root = ?`
	args := []any{f.ComponentRoot}
	if f.BaseBranch != "" {
		q += ` AND base_branch = ?`
		args = append(args, f.BaseBranch)
	}
	if len(f.Statuses) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")
		q += ` AND status IN (` + ph + `)`
		for _, st := range f.Statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY position ASC, id ASC`
	rows, err := s.r.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list merge items for %s: %w", f.ComponentRoot, err)
	}
	defer rows.Close()
	out := []MergeItem{}
	for rows.Next() {
		it, err := scanMergeItem(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan merge item: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// TransitionMergeItem is the ONLY status writer. One transaction: a guarded
// UPDATE (WHERE id = ? AND status = ?) plus the event row. A lost race — the
// status found is not `from` — is MergeRefusal{Kind:"stale-state", Current},
// never an overwrite. Entering a slot state while another item holds it is
// MergeRefusal{Kind:"slot-taken", Holder}; merge_queue_one_slot is the backstop
// for the race the check cannot close, and its constraint failure is
// translated to the same refusal. The legality of from→to is NOT checked
// here — that is internal/mergequeue (T3); the store guards the write, the
// core guards the walk.
func (s *Store) TransitionMergeItem(id, from, to string, facts TransitionFacts, actor MergeActor) (MergeItem, error) {
	if r, bad := actor.refusal(); bad {
		return MergeItem{}, r
	}
	if !inStringSet(MergeStatusSet, from) || !inStringSet(MergeStatusSet, to) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-field", Reason: fmt.Sprintf("a transition names two statuses of the closed set, got %q → %q", from, to)}
	}
	if facts.Reason != "" && !inStringSet(MergeReasonSet, facts.Reason) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-field", Reason: fmt.Sprintf("reason_kind %q is not in the closed set", facts.Reason)}
	}
	detail, err := eventDetail(facts)
	if err != nil {
		return MergeItem{}, err
	}

	tx, err := s.w.Begin()
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: begin merge transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var root, base string
	err = tx.QueryRow(`SELECT component_root, base_branch FROM merge_queue_item WHERE id = ?`, id).Scan(&root, &base)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-item", Reason: fmt.Sprintf("no queue item %s", id)}
	} else if err != nil {
		return MergeItem{}, fmt.Errorf("store: read queue item %s: %w", id, err)
	}

	if inStringSet(MergeSlotStatusSet, to) {
		var holder string
		err = tx.QueryRow(`SELECT id FROM merge_queue_item
			WHERE component_root = ? AND base_branch = ? AND id != ? AND status IN ('catching_up','verifying','ready','merging')`,
			root, base, id).Scan(&holder)
		if err == nil {
			return MergeItem{}, MergeRefusal{Kind: "slot-taken", Holder: holder,
				Reason: fmt.Sprintf("%s holds the landing slot for base %s — one item lands at a time (MQ3)", holder, base)}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return MergeItem{}, fmt.Errorf("store: check the landing slot for base %s: %w", base, err)
		}
	}

	now := s.Now()
	res, err := tx.Exec(`UPDATE merge_queue_item SET
			status = ?, updated_at = ?,
			head_sha          = COALESCE(NULLIF(?, ''), head_sha),
			base_sha          = COALESCE(NULLIF(?, ''), base_sha),
			integration_branch = COALESCE(NULLIF(?, ''), integration_branch),
			integration_sha   = COALESCE(NULLIF(?, ''), integration_sha),
			pr_number         = COALESCE(?, pr_number),
			verified_sha      = COALESCE(NULLIF(?, ''), verified_sha),
			landed_sha        = COALESCE(NULLIF(?, ''), landed_sha),
			landing_loop_slug = COALESCE(NULLIF(?, ''), landing_loop_slug),
			resolve_loop_slug = COALESCE(NULLIF(?, ''), resolve_loop_slug),
			reason_kind       = COALESCE(NULLIF(?, ''), reason_kind),
			catch_ups         = catch_ups + ?
		WHERE id = ? AND status = ?`,
		to, now, facts.HeadSHA, facts.BaseSHA, facts.IntegrationBranch, facts.IntegrationSHA,
		facts.PRNumber, facts.VerifiedSHA, facts.LandedSHA, facts.LandingLoopSlug,
		facts.ResolveLoopSlug, facts.Reason, b2i(facts.BumpCatchUps), id, from)
	if err != nil {
		if isSlotIndexFailure(err) {
			var holder string
			_ = tx.QueryRow(`SELECT id FROM merge_queue_item
				WHERE component_root = ? AND base_branch = ? AND id != ? AND status IN ('catching_up','verifying','ready','merging')`,
				root, base, id).Scan(&holder)
			return MergeItem{}, MergeRefusal{Kind: "slot-taken", Holder: holder,
				Reason: fmt.Sprintf("%s took the landing slot for base %s in the same moment — one item lands at a time (MQ3)", holder, base)}
		}
		return MergeItem{}, fmt.Errorf("store: transition merge item %s: %w", id, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return MergeItem{}, fmt.Errorf("store: settle merge transition for %s: %w", id, err)
	} else if n == 0 {
		var cur string
		_ = tx.QueryRow(`SELECT status FROM merge_queue_item WHERE id = ?`, id).Scan(&cur)
		return MergeItem{}, MergeRefusal{Kind: "stale-state", Current: cur,
			Reason: fmt.Sprintf("item %s is %s, not %s — the transition was guarded, nothing moved", id, cur, from)}
	}
	if err := appendMergeEvent(tx, root, &id, "transition", from, to, facts.Reason, actor, detail, now); err != nil {
		return MergeItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeItem{}, fmt.Errorf("store: commit merge transition: %w", err)
	}
	return s.ReadMergeItem(id)
}

// MergeFields is SetMergeItemFields' patch: facts-shaped, applied verbatim
// under a status guard. It is how the driver records what git and the forge
// answered (shas, pr number, slugs) without a status change.
type MergeFields = TransitionFacts

// SetMergeItemFields writes facts (never the status) to an item the caller
// believes sits in status guard. The write is guarded exactly like a
// transition (WHERE id AND status) and is a NO-OP without an event when the
// facts carry nothing to write; a changing write appends one 'transition'
// event with from = to = the current status (the kinds stay the closed
// three).
func (s *Store) SetMergeItemFields(id, guard string, fields MergeFields, actor MergeActor) (MergeItem, error) {
	if r, bad := actor.refusal(); bad {
		return MergeItem{}, r
	}
	if !inStringSet(MergeStatusSet, guard) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-field", Reason: fmt.Sprintf("the status guard %q is not in the closed set", guard)}
	}
	detail, err := eventDetail(fields)
	if err != nil {
		return MergeItem{}, err
	}
	if detail == "{}" {
		return s.ReadMergeItem(id) // a patch with nothing to write changes nothing — no event, no touch
	}
	tx, err := s.w.Begin()
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: begin merge field write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var cur string
	err = tx.QueryRow(`SELECT status FROM merge_queue_item WHERE id = ?`, id).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-item", Reason: fmt.Sprintf("no queue item %s", id)}
	} else if err != nil {
		return MergeItem{}, fmt.Errorf("store: read queue item %s: %w", id, err)
	}
	now := s.Now()
	res, err := tx.Exec(`UPDATE merge_queue_item SET
			updated_at        = ?,
			head_sha          = COALESCE(NULLIF(?, ''), head_sha),
			base_sha          = COALESCE(NULLIF(?, ''), base_sha),
			integration_branch = COALESCE(NULLIF(?, ''), integration_branch),
			integration_sha   = COALESCE(NULLIF(?, ''), integration_sha),
			pr_number         = COALESCE(?, pr_number),
			verified_sha      = COALESCE(NULLIF(?, ''), verified_sha),
			landed_sha        = COALESCE(NULLIF(?, ''), landed_sha),
			landing_loop_slug = COALESCE(NULLIF(?, ''), landing_loop_slug),
			resolve_loop_slug = COALESCE(NULLIF(?, ''), resolve_loop_slug),
			reason_kind       = COALESCE(NULLIF(?, ''), reason_kind),
			catch_ups         = catch_ups + ?
		WHERE id = ? AND status = ?`,
		now, fields.HeadSHA, fields.BaseSHA, fields.IntegrationBranch, fields.IntegrationSHA,
		fields.PRNumber, fields.VerifiedSHA, fields.LandedSHA, fields.LandingLoopSlug,
		fields.ResolveLoopSlug, fields.Reason, b2i(fields.BumpCatchUps), id, guard)
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: set merge item fields on %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var found string
		_ = tx.QueryRow(`SELECT status FROM merge_queue_item WHERE id = ?`, id).Scan(&found)
		if found == "" {
			return MergeItem{}, MergeRefusal{Kind: "unknown-item", Reason: fmt.Sprintf("no queue item %s", id)}
		}
		return MergeItem{}, MergeRefusal{Kind: "stale-state", Current: found,
			Reason: fmt.Sprintf("item %s is %s, not %s — the field write was guarded, nothing moved", id, found, guard)}
	}
	if err := appendMergeEvent(tx, s.mergeRootOf(tx, id), &id, "transition", cur, cur, fields.Reason, actor, detail, now); err != nil {
		return MergeItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeItem{}, fmt.Errorf("store: commit merge field write: %w", err)
	}
	return s.ReadMergeItem(id)
}

// ReorderMergeItems moves one QUEUED item in front of another queued item of
// the same (root, base) — or to the tail when `beforeID` is empty. Non-queued
// items (the ones already integrated, the ones conflicted) are refused: their
// positions are history. One transaction; one kind='reorder' event naming
// both ids.
func (s *Store) ReorderMergeItems(id, beforeID string, actor MergeActor) (MergeItem, error) {
	if r, bad := actor.refusal(); bad {
		return MergeItem{}, r
	}
	tx, err := s.w.Begin()
	if err != nil {
		return MergeItem{}, fmt.Errorf("store: begin merge reorder: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var root, base, status string
	var pos int64
	err = tx.QueryRow(`SELECT component_root, base_branch, status, position FROM merge_queue_item WHERE id = ?`, id).
		Scan(&root, &base, &status, &pos)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeItem{}, MergeRefusal{Kind: "unknown-item", Reason: fmt.Sprintf("no queue item %s", id)}
	} else if err != nil {
		return MergeItem{}, fmt.Errorf("store: read queue item %s: %w", id, err)
	}
	if status != "queued" {
		return MergeItem{}, MergeRefusal{Kind: "not-queued", Current: status,
			Reason: fmt.Sprintf("only a queued item moves — %s is %s", id, status)}
	}
	now := s.Now()
	newPos := pos
	if beforeID == "" {
		var max sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(position) FROM merge_queue_item
			WHERE component_root = ? AND base_branch = ? AND status = 'queued' AND id != ?`, root, base, id).Scan(&max); err != nil {
			return MergeItem{}, fmt.Errorf("store: read the queue tail: %w", err)
		}
		tail := int64(0)
		if max.Valid {
			tail = max.Int64
		}
		newPos = tail + 1000
	} else {
		var bStatus string
		var bPos int64
		err = tx.QueryRow(`SELECT status, position FROM merge_queue_item WHERE id = ?`, beforeID).Scan(&bStatus, &bPos)
		if errors.Is(err, sql.ErrNoRows) {
			return MergeItem{}, MergeRefusal{Kind: "unknown-item", Reason: fmt.Sprintf("no queue item %s to move in front of", beforeID)}
		} else if err != nil {
			return MergeItem{}, fmt.Errorf("store: read queue item %s: %w", beforeID, err)
		}
		if bStatus != "queued" {
			return MergeItem{}, MergeRefusal{Kind: "not-queued", Current: bStatus,
				Reason: fmt.Sprintf("a queued item moves in front of another QUEUED item — %s is %s", beforeID, bStatus)}
		}
		if bPos == 0 {
			return MergeItem{}, MergeRefusal{Kind: "no-op", Reason: "the first slot has no gap in front of it"}
		}
		newPos = bPos - 1
	}
	detail := `{"to_tail":true}`
	if beforeID != "" {
		detail = `{"before_id":"` + beforeID + `"}`
	}
	if newPos == pos {
		return s.ReadMergeItem(id) // the row is where the caller wants it — no write, no event (P3)
	}
	if _, err := tx.Exec(`UPDATE merge_queue_item SET position = ?, updated_at = ? WHERE id = ?`, newPos, now, id); err != nil {
		return MergeItem{}, fmt.Errorf("store: reorder merge item %s: %w", id, err)
	}
	if err := appendMergeEvent(tx, root, &id, "reorder", "queued", "queued", "", actor, detail, now); err != nil {
		return MergeItem{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeItem{}, fmt.Errorf("store: commit merge reorder: %w", err)
	}
	return s.ReadMergeItem(id)
}

// ReadMergeConfig answers the repository's config; an absent row is the
// defaults with Absent = true (mode 'off' — OD3), never an error.
func (s *Store) ReadMergeConfig(componentRoot string) (MergeConfig, error) {
	var (
		c        MergeConfig
		updByKnd sql.NullString
	)
	err := s.r.QueryRow(`SELECT component_root, mode, conflict_policy, land_method, max_catch_ups,
		updated_at, updated_by_kind, updated_by_id FROM merge_queue_config WHERE component_root = ?`, componentRoot).
		Scan(&c.ComponentRoot, &c.Mode, &c.ConflictPolicy, &c.LandMethod, &c.MaxCatchUps, &c.UpdatedAt, &updByKnd, &c.UpdatedByID)
	if errors.Is(err, sql.ErrNoRows) {
		return MergeConfig{ComponentRoot: componentRoot, Mode: "off", ConflictPolicy: "ask", LandMethod: "squash", MaxCatchUps: 3, Absent: true}, nil
	}
	if err != nil {
		return MergeConfig{}, fmt.Errorf("store: read merge config for %s: %w", componentRoot, err)
	}
	c.UpdatedByKind = updByKnd.String
	return c, nil
}

// WriteMergeConfig sets a repository's config — an UPSERT — and appends the
// kind='config' event (item_id NULL) in the same transaction. Identical input
// is a NO-OP: no write, no event (P3 counts changes, not calls).
func (s *Store) WriteMergeConfig(in NewMergeConfig, actor MergeActor) (MergeConfig, error) {
	if r, bad := in.refusal(); bad {
		return MergeConfig{}, r
	}
	if r, bad := actor.refusal(); bad {
		return MergeConfig{}, r
	}
	if err := s.checkRoot(in.ComponentRoot); err != nil {
		return MergeConfig{}, *err
	}
	tx, err := s.w.Begin()
	if err != nil {
		return MergeConfig{}, fmt.Errorf("store: begin merge config: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var (
		mode, policy, land string
		max                int
	)
	err = tx.QueryRow(`SELECT mode, conflict_policy, land_method, max_catch_ups FROM merge_queue_config WHERE component_root = ?`, in.ComponentRoot).
		Scan(&mode, &policy, &land, &max)
	noop := false
	if errors.Is(err, sql.ErrNoRows) {
		noop = false
	} else if err != nil {
		return MergeConfig{}, fmt.Errorf("store: read merge config for %s: %w", in.ComponentRoot, err)
	} else {
		noop = mode == in.Mode && policy == in.ConflictPolicy && land == in.LandMethod && max == in.MaxCatchUps
	}
	if noop {
		return s.ReadMergeConfig(in.ComponentRoot)
	}
	now := s.Now()
	if _, err := tx.Exec(`INSERT INTO merge_queue_config
		(component_root, mode, conflict_policy, land_method, max_catch_ups, updated_at, updated_by_kind, updated_by_id)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(component_root) DO UPDATE SET
			mode = excluded.mode, conflict_policy = excluded.conflict_policy,
			land_method = excluded.land_method, max_catch_ups = excluded.max_catch_ups,
			updated_at = excluded.updated_at, updated_by_kind = excluded.updated_by_kind,
			updated_by_id = excluded.updated_by_id`,
		in.ComponentRoot, in.Mode, in.ConflictPolicy, in.LandMethod, in.MaxCatchUps, now, actor.Kind, actor.ID); err != nil {
		return MergeConfig{}, fmt.Errorf("store: write merge config for %s: %w", in.ComponentRoot, err)
	}
	detail := fmt.Sprintf(`{"mode":"%s","conflict_policy":"%s","land_method":"%s","max_catch_ups":%d}`, in.Mode, in.ConflictPolicy, in.LandMethod, in.MaxCatchUps)
	if err := appendMergeEvent(tx, in.ComponentRoot, nil, "config", "", "", "", actor, detail, now); err != nil {
		return MergeConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return MergeConfig{}, fmt.Errorf("store: commit merge config: %w", err)
	}
	return s.ReadMergeConfig(in.ComponentRoot)
}

// ListMergeEvents answers an item's whole history in append order — every
// change ever made to it, including the `queued` birth (from_status NULL).
// The history outlives the record; nothing deletes it.
func (s *Store) ListMergeQueueEvents(itemID string) ([]MergeQueueEvent, error) {
	rows, err := s.r.Query(`SELECT id, component_root, item_id, kind, from_status, to_status, reason_kind,
		actor_kind, actor_id, detail, at FROM merge_queue_event WHERE item_id = ? ORDER BY id ASC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("store: list merge-queue events for %s: %w", itemID, err)
	}
	defer rows.Close()
	out := []MergeQueueEvent{}
	for rows.Next() {
		var (
			ev     MergeQueueEvent
			item   sql.NullString
			from   sql.NullString
			to     sql.NullString
			reason sql.NullString
		)
		if err := rows.Scan(&ev.ID, &ev.ComponentRoot, &item, &ev.Kind, &from, &to, &reason, &ev.ActorKind, &ev.ActorID, &ev.Detail, &ev.At); err != nil {
			return nil, fmt.Errorf("store: scan merge event: %w", err)
		}
		ev.ItemID, ev.FromStatus, ev.ToStatus, ev.ReasonKind = item.String, from.String, to.String, reason.String
		out = append(out, ev)
	}
	return out, rows.Err()
}

// --- internal helpers, all file-private to this concern ---------------------

func (s *Store) checkRoot(componentRoot string) *MergeRefusal {
	var one int
	err := s.r.QueryRow(`SELECT 1 FROM component_root WHERE component_root = ?`, componentRoot).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return &MergeRefusal{Kind: "unknown-root", Reason: fmt.Sprintf("no component_root %s — the queue is per-repository, and a row nobody can point at is a row nobody can land", componentRoot)}
	}
	if err != nil {
		return &MergeRefusal{Kind: "unknown-root", Reason: fmt.Sprintf("store: checking component_root %s: %v", componentRoot, err)}
	}
	return nil
}

func (s *Store) mergeRootOf(tx *sql.Tx, id string) string {
	var root string
	_ = tx.QueryRow(`SELECT component_root FROM merge_queue_item WHERE id = ?`, id).Scan(&root)
	return root
}

// isSlotIndexFailure recognizes the merge_queue_one_slot partial unique index
// ending a slot race the pre-check could not close, so the caller answers
// with the same sentence rather than a constraint dump.
func isSlotIndexFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), "merge_queue_one_slot")
}

// appendMergeEvent writes one history row. The caller passes the transaction
// — an event never commits apart from the change it records (P3).
func appendMergeEvent(tx *sql.Tx, root string, itemID *string, kind, from, to, reason string, actor MergeActor, detail, at string) error {
	if !inStringSet(MergeEventKindSet, kind) {
		return fmt.Errorf("store: merge event kind %q is not in the closed set", kind)
	}
	if _, err := tx.Exec(`INSERT INTO merge_queue_event
		(component_root, item_id, kind, from_status, to_status, reason_kind, actor_kind, actor_id, detail, at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		root, itemID, kind, nullable(from), nullable(to), nullable(reason), actor.Kind, actor.ID, detail, at); err != nil {
		return fmt.Errorf("store: append merge %s event: %w", kind, err)
	}
	return nil
}
