package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// blueprint_records.go — S48 T1, the SQL half of blueprints-as-records
// (design BP1). The file `blueprint.go` stays what it always was: the pinned
// loop side (loop_blueprint). This file owns the four record tables: the
// current row, the append-only revision history, the derived references, and
// the file-adoption ledger. Content is stored VERBATIM — nothing here parses,
// re-serializes or normalizes YAML; the engine parses on use (BP4).
//
// One write path: every change to a blueprint row goes through
// BlueprintPutTx — row + revision + refs in one transaction (BP7, BP11).
// Revisions are append-only by trigger (BP7); the delete path removes the row
// and its refs and NEVER the revisions.

// BlueprintRow is one row of the `blueprint` table: the current content and
// its provenance. Content is the YAML text exactly as written; ContentHash is
// the SHA-256 hex of those bytes (computed by the writer, stored beside them
// so a reader never recomputes to compare).
type BlueprintRow struct {
	Name        string
	Version     int
	Content     string
	ContentHash string
	Origin      string // 'system' | 'user'
	CreatedAt   string
	UpdatedAt   string
	UpdatedBy   string
}

// RevisionRow is one row of `blueprint_revision`: an append-only history
// entry. Rev numbers per name never reset — even across delete-and-recreate
// the next revision continues (BP7).
type RevisionRow struct {
	ID          int64
	Name        string
	Rev         int
	Version     int
	Content     string
	ContentHash string
	Op          string // create|save|duplicate|revert|seed|update|restore|adopt
	Note        string
	ActorKind   string // user|cli|system
	ActorID     string
	OccurredAt  string
}

// RefRow is one derived row of `blueprint_ref`: a blueprint `Name` uses
// `Kind`:`Target`. The rows are rebuilt from content on every write (BP11);
// nothing outside the write path writes them.
type RefRow struct {
	Name   string
	Kind   string // agent|converse|skill|action|resolver
	Target string
}

// ImportRow is one row of the adoption ledger `blueprint_import` (BP3): a
// state-directory file was imported (or refused) once, by hash, with the
// per-file outcome. The ledger makes adoption idempotent and makes a refused
// file speak instead of vanishing.
type ImportRow struct {
	ID         int64
	Path       string
	SHA256     string
	Name       string
	Result     string // imported | refused
	Detail     string
	ImportedAt string
}

// Reference is one LIVE thing that names a blueprint — the half of
// `BlueprintReferences` a delete refusal must name (BP10). Kind is
// "schedule", "webhook", "trigger" (an event trigger) or "queue" (an open
// queue item). Status carries the queue item's status and is empty otherwise.
type Reference struct {
	Kind   string
	ID     string
	Name   string
	Status string
}

// References is the delete-safety answer for one blueprint (BP10): the live
// references that refuse a delete, plus an informational count of loops
// already started from it — which never refuses (they carry pinned content).
type References struct {
	Schedules    []Reference
	Triggers     []Reference
	QueueItems   []Reference
	LoopsStarted int
}

// Any returns whether any refusing reference exists.
func (r References) Any() bool {
	return len(r.Schedules) > 0 || len(r.Triggers) > 0 || len(r.QueueItems) > 0
}

// liveQueueStatuses are the queue_item statuses that could still start (or
// re-start, via BeginRun's re-pin) a loop. The queue machine's terminals are
// 'done' and 'cancelled' (internal/queue/types.go:83-92); every other
// status — including 'blocked', which can only reach 'cancelled' but whose
// root still carries a live item — refuses a delete (deny-leaning, T0 item 3).
var liveQueueStatuses = []string{"pending", "sizing", "ready", "running", "blocked"}

const blueprintColumns = `name, version, content, content_hash, origin, created_at, updated_at, updated_by`

func scanBlueprintRow(row interface{ Scan(dest ...any) error }) (BlueprintRow, error) {
	var b BlueprintRow
	err := row.Scan(&b.Name, &b.Version, &b.Content, &b.ContentHash, &b.Origin, &b.CreatedAt, &b.UpdatedAt, &b.UpdatedBy)
	return b, err
}

// BlueprintGet reads one blueprint row; false is no row (not an error). The
// read pool (O6, I6): this is what `blueprint.show` and the source read.
func (s *Store) BlueprintGet(ctx context.Context, name string) (BlueprintRow, bool, error) {
	b, err := scanBlueprintRow(s.r.QueryRowContext(ctx,
		`SELECT `+blueprintColumns+` FROM blueprint WHERE name = ?`, name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BlueprintRow{}, false, nil
		}
		return BlueprintRow{}, false, fmt.Errorf("store: reading blueprint %q: %w", name, err)
	}
	return b, true, nil
}

// BlueprintRowTx reads one blueprint row **inside the caller's transaction** —
// the seed applier's read (the SeedToolRow idiom): the fingerprint and the
// write must see the state the write lands in, not a straddle across the
// read pool.
func (s *Store) BlueprintRowTx(ctx context.Context, tx *sql.Tx, name string) (BlueprintRow, bool, error) {
	if tx == nil {
		return BlueprintRow{}, false, errors.New("store: BlueprintRowTx needs the caller's transaction")
	}
	b, err := scanBlueprintRow(tx.QueryRowContext(ctx,
		`SELECT `+blueprintColumns+` FROM blueprint WHERE name = ?`, name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BlueprintRow{}, false, nil
		}
		return BlueprintRow{}, false, fmt.Errorf("store: reading blueprint %q in the transaction: %w", name, err)
	}
	return b, true, nil
}

// BlueprintList reads every row, optionally filtered by origin and by a
// case-insensitive substring of the name, ordered by name. It answers
// `blueprint.list` (BP12) — daemon-side filters, not client-side.
func (s *Store) BlueprintList(ctx context.Context, origin, q string) ([]BlueprintRow, error) {
	query := `SELECT ` + blueprintColumns + ` FROM blueprint`
	var (
		wheres []string
		args   []any
	)
	if origin != "" {
		wheres = append(wheres, "origin = ?")
		args = append(args, origin)
	}
	if q != "" {
		wheres = append(wheres, `name LIKE '%' || ? || '%' COLLATE NOCASE`)
		args = append(args, q)
	}
	if len(wheres) > 0 {
		query += " WHERE " + wheres[0]
		for _, w := range wheres[1:] {
			query += " AND " + w
		}
	}
	query += " ORDER BY name"
	rows, err := s.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: listing blueprints: %w", err)
	}
	defer rows.Close()
	var out []BlueprintRow
	for rows.Next() {
		b, err := scanBlueprintRow(rows)
		if err != nil {
			return nil, fmt.Errorf("store: listing blueprints: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: listing blueprints: %w", err)
	}
	return out, nil
}

// BlueprintNames lists the names only — what `blueprint.list` answers today
// and what the source's Names() reads (T3). Ordered, deduped by PK.
func (s *Store) BlueprintNames(ctx context.Context) ([]string, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT name FROM blueprint ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: listing blueprint names: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("store: listing blueprint names: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// BlueprintWithTx opens one transaction on the single writer and runs fn over
// it, committing on success and rolling back on error — the store's existing
// read-then-write idiom (the SeedWithTx/McpWithTx shape, DB15), not a second
// one. Every blueprint write, its revision and its refs commit here (T8
// guards call this THE one write path).
func (s *Store) BlueprintWithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: blueprint with tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: blueprint commit: %w", err)
	}
	return nil
}

// BlueprintPutTx is the single write path (BP7, BP11): in the CALLER's
// transaction it upserts the `blueprint` row, APPENDS the next revision (rev
// = per-name max + 1, computed inside this transaction, so numbering never
// resets even across delete-and-recreate), and REPLACES that name's derived
// refs. Nothing else writes a blueprint row; a forced failure anywhere here
// leaves nothing (the transaction aborts). The content is written verbatim:
// this function touches no byte of it.
//
// rev.Rev is ignored — the store computes it; rev.Name/Content/ContentHash/
// Version/Op/ActorKind/ActorID and the row's fields are the caller's (the
// write service validated the content with Analyze before arriving, BP4).
// BlueprintMaxRevTx answers the highest revision number a name has on the
// caller's transaction (0 when none) - the number a changing write's answer
// reports as max+1, read in the same transaction that appends it, so a
// concurrent writer can never make two saves report the same revision
// (S48 T6; the SQL lives here, nowhere else).
func BlueprintMaxRevTx(ctx context.Context, tx *sql.Tx, name string) (int, error) {
	if tx == nil {
		return 0, errors.New("store: BlueprintMaxRevTx needs the caller's transaction")
	}
	var maxRev int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev), 0) FROM blueprint_revision WHERE name = ?`, name).Scan(&maxRev); err != nil {
		return 0, fmt.Errorf("store: next revision for %q: %w", name, err)
	}
	return maxRev, nil
}

func BlueprintPutTx(tx *sql.Tx, row BlueprintRow, rev RevisionRow, refs []RefRow) error {
	if tx == nil {
		return errors.New("store: BlueprintPutTx needs the caller's transaction")
	}
	if row.Name == "" {
		return errors.New("store: BlueprintPutTx needs a name")
	}
	if rev.Name != "" && rev.Name != row.Name {
		return fmt.Errorf("store: BlueprintPutTx revision name %q does not match row name %q", rev.Name, row.Name)
	}
	rev.Name = row.Name
	now := time.Now().UTC().Format(time.RFC3339)

	// 1. The row. Origin is set on create and kept on update (a system row
	//    edited by a person stays system — BP6; the seed side never flips
	//    origins either).
	if row.CreatedAt == "" {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if _, err := tx.Exec(`
		INSERT INTO blueprint (name, version, content, content_hash, origin, created_at, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			version      = excluded.version,
			content      = excluded.content,
			content_hash = excluded.content_hash,
			updated_at   = excluded.updated_at,
			updated_by   = excluded.updated_by`,
		row.Name, row.Version, row.Content, row.ContentHash, row.Origin, row.CreatedAt, row.UpdatedAt, row.UpdatedBy); err != nil {
		return fmt.Errorf("store: putting blueprint %q: %w", row.Name, err)
	}

	// 2. The revision — the next number per name, computed INSIDE this
	//    transaction so concurrent writers cannot both pick it and a
	//    delete-and-recreate continues the numbering (BP7).
	var maxRev int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(rev), 0) FROM blueprint_revision WHERE name = ?`, row.Name).Scan(&maxRev); err != nil {
		return fmt.Errorf("store: next revision for %q: %w", row.Name, err)
	}
	rev.Rev = maxRev + 1
	if rev.OccurredAt == "" {
		rev.OccurredAt = now
	}
	if _, err := tx.Exec(`
		INSERT INTO blueprint_revision (name, rev, version, content, content_hash, op, note, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rev.Name, rev.Rev, rev.Version, rev.Content, rev.ContentHash, rev.Op, rev.Note, rev.ActorKind, rev.ActorID, rev.OccurredAt); err != nil {
		return fmt.Errorf("store: appending revision %q r%d: %w", rev.Name, rev.Rev, err)
	}

	// 3. The refs — replaced wholesale for THIS name only (BP11). A derived
	//    table is rebuildable, so a write that changes content must rewrite
	//    it in the same transaction or the two disagree: the DELETE+INSERT
	//    here is why "who uses X" is a trustworthy query.
	if _, err := tx.Exec(`DELETE FROM blueprint_ref WHERE name = ?`, row.Name); err != nil {
		return fmt.Errorf("store: clearing refs of %q: %w", row.Name, err)
	}
	for _, r := range refs {
		if r.Kind == "" || r.Target == "" {
			return fmt.Errorf("store: ref of %q has an empty kind or target", row.Name)
		}
		if r.Name != "" && r.Name != row.Name {
			return fmt.Errorf("store: ref of %q names %q — refs never cross names", row.Name, r.Name)
		}
		if _, err := tx.Exec(`INSERT INTO blueprint_ref (name, kind, target) VALUES (?, ?, ?)`,
			row.Name, r.Kind, r.Target); err != nil {
			return fmt.Errorf("store: inserting ref %s→%s:%s: %w", row.Name, r.Kind, r.Target, err)
		}
	}
	return nil
}

// BlueprintDeleteTx removes the row and its derived refs — and NOTHING ELSE.
// The revisions outlive the name (BP7, BP10): history is never rewritten, and
// re-creating the name continues its numbering. The live-reference refusal
// is the caller's decision (BP10), asked through BlueprintReferences first.
func BlueprintDeleteTx(tx *sql.Tx, name string) error {
	if tx == nil {
		return errors.New("store: BlueprintDeleteTx needs the caller's transaction")
	}
	if name == "" {
		return errors.New("store: BlueprintDeleteTx needs a name")
	}
	if _, err := tx.Exec(`DELETE FROM blueprint_ref WHERE name = ?`, name); err != nil {
		return fmt.Errorf("store: deleting refs of %q: %w", name, err)
	}
	if _, err := tx.Exec(`DELETE FROM blueprint WHERE name = ?`, name); err != nil {
		return fmt.Errorf("store: deleting blueprint %q: %w", name, err)
	}
	return nil
}

// RevisionList reads a name's revisions newest first, clamped to [1,500],
// default 50 (the setting_event/seed_event precedent). History survives the
// row's deletion: a deleted blueprint's history stays readable (T7).
func (s *Store) RevisionList(ctx context.Context, name string, limit int) ([]RevisionRow, error) {
	switch {
	case limit <= 0:
		limit = 50
	case limit > 500:
		limit = 500
	}
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, name, rev, version, content, content_hash, op, note, actor_kind, actor_id, occurred_at
		FROM blueprint_revision WHERE name = ?
		ORDER BY rev DESC LIMIT ?`, name, limit)
	if err != nil {
		return nil, fmt.Errorf("store: listing revisions of %q: %w", name, err)
	}
	defer rows.Close()
	var out []RevisionRow
	for rows.Next() {
		var v RevisionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.Rev, &v.Version, &v.Content, &v.ContentHash,
			&v.Op, &v.Note, &v.ActorKind, &v.ActorID, &v.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: listing revisions of %q: %w", name, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// RevisionGet reads one (name, rev); false is no such revision (not an error).
func (s *Store) RevisionGet(ctx context.Context, name string, rev int) (RevisionRow, bool, error) {
	var v RevisionRow
	err := s.r.QueryRowContext(ctx, `
		SELECT id, name, rev, version, content, content_hash, op, note, actor_kind, actor_id, occurred_at
		FROM blueprint_revision WHERE name = ? AND rev = ?`, name, rev).
		Scan(&v.ID, &v.Name, &v.Rev, &v.Version, &v.Content, &v.ContentHash,
			&v.Op, &v.Note, &v.ActorKind, &v.ActorID, &v.OccurredAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RevisionRow{}, false, nil
		}
		return RevisionRow{}, false, fmt.Errorf("store: reading revision %q r%d: %w", name, rev, err)
	}
	return v, true, nil
}

// BlueprintOwnRefs reads the refs a blueprint ITSELF uses (the `show` and
// `usage` reads, T7): what this blueprint references. Ordered by kind/target.
// BlueprintMaxRevs answers each name's highest revision in one grouped
// read — the list summary's `revision` column without an N+1 walk
// (S48 T7). Names with no revision row yet are absent from the map (read
// as 0 by the caller).
func (s *Store) BlueprintMaxRevs(ctx context.Context) (map[string]int, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT name, MAX(rev) FROM blueprint_revision GROUP BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: blueprint revision summary: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var name string
		var maxRev int
		if err := rows.Scan(&name, &maxRev); err != nil {
			return nil, fmt.Errorf("store: blueprint revision summary row: %w", err)
		}
		out[name] = maxRev
	}
	return out, rows.Err()
}

// BlueprintUsageCounts answers how many derived blueprint_ref rows each
// name owns in one grouped read — the list summary's `usedByCount` without
// an N+1 walk (S48 T7).
func (s *Store) BlueprintUsageCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT name, COUNT(*) FROM blueprint_ref GROUP BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: blueprint usage summary: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, fmt.Errorf("store: blueprint usage summary row: %w", err)
		}
		out[name] = n
	}
	return out, rows.Err()
}

func (s *Store) BlueprintOwnRefs(ctx context.Context, name string) ([]RefRow, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT name, kind, target FROM blueprint_ref WHERE name = ? ORDER BY kind, target`, name)
	if err != nil {
		return nil, fmt.Errorf("store: own refs of %q: %w", name, err)
	}
	defer rows.Close()
	var out []RefRow
	for rows.Next() {
		var r RefRow
		if err := rows.Scan(&r.Name, &r.Kind, &r.Target); err != nil {
			return nil, fmt.Errorf("store: own refs of %q: %w", name, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BlueprintRefsFor answers "which blueprints use this?" — the query the
// derived table exists for (BP11): agent deletion (S43's AM4 half, T6) and
// the pickers ask SQL, never a text scan. Targets are matched exactly.
func (s *Store) BlueprintRefsFor(ctx context.Context, kind, target string) ([]string, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT name FROM blueprint_ref WHERE kind = ? AND target = ? ORDER BY name`, kind, target)
	if err != nil {
		return nil, fmt.Errorf("store: ref lookup %s=%q: %w", kind, target, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("store: ref lookup %s=%q: %w", kind, target, err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// BlueprintReferences is the delete-safety read (BP10): the LIVE references
// that refuse a delete — every trigger_schedule and event_trigger row naming
// it (no delete path exists for either and pause/disable is reversible, so a
// row can always fire again — T0 item 3) and queue items in a live status.
// History (done/cancelled items, loop_run rows) never refuses. LoopsStarted
// is informational: started loops carry pinned content and are reported
// unaffected, never a reason to refuse.
func (s *Store) BlueprintReferences(ctx context.Context, name string) (References, error) {
	var out References
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, kind FROM trigger_schedule WHERE blueprint = ? ORDER BY id`, name)
	if err != nil {
		return out, fmt.Errorf("store: schedule references of %q: %w", name, err)
	}
	for rows.Next() {
		var ref Reference
		if err := rows.Scan(&ref.ID, &ref.Kind); err != nil {
			rows.Close()
			return out, fmt.Errorf("store: schedule references of %q: %w", name, err)
		}
		out.Schedules = append(out.Schedules, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("store: schedule references of %q: %w", name, err)
	}

	rows, err = s.r.QueryContext(ctx, `
		SELECT id, name FROM event_trigger WHERE blueprint = ? ORDER BY name`, name)
	if err != nil {
		return out, fmt.Errorf("store: trigger references of %q: %w", name, err)
	}
	for rows.Next() {
		var ref Reference
		ref.Kind = "trigger"
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			rows.Close()
			return out, fmt.Errorf("store: trigger references of %q: %w", name, err)
		}
		out.Triggers = append(out.Triggers, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("store: trigger references of %q: %w", name, err)
	}

	ph := ""
	args := []any{name}
	for i, st := range liveQueueStatuses {
		if i > 0 {
			ph += ","
		}
		ph += "?"
		args = append(args, st)
	}
	rows, err = s.r.QueryContext(ctx, `
		SELECT id, status, goal FROM queue_item
		WHERE blueprint = ? AND status IN (`+ph+`)
		ORDER BY id`, args...)
	if err != nil {
		return out, fmt.Errorf("store: queue references of %q: %w", name, err)
	}
	for rows.Next() {
		var (
			id   int64
			ref  Reference
			goal string
		)
		ref.Kind = "queue"
		if err := rows.Scan(&id, &ref.Status, &goal); err != nil {
			rows.Close()
			return out, fmt.Errorf("store: queue references of %q: %w", name, err)
		}
		ref.ID = strconv.FormatInt(id, 10)
		ref.Name = goal
		out.QueueItems = append(out.QueueItems, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("store: queue references of %q: %w", name, err)
	}

	if err := s.r.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM loop_run WHERE blueprint = ?`, name).Scan(&out.LoopsStarted); err != nil {
		return out, fmt.Errorf("store: counting loops started from %q: %w", name, err)
	}
	return out, nil
}

// ImportInsertTx appends one adoption-ledger row on the caller's transaction
// (BP3): one transaction per file, so a file's row, its adopt-revision, its
// refs and its ledger row land together. The partial unique index refuses a
// SECOND 'imported' row for the same hash (idempotence); 'refused' rows may
// repeat (a re-run reports the same bad file honestly).
func ImportInsertTx(tx *sql.Tx, row ImportRow) error {
	if tx == nil {
		return errors.New("store: ImportInsertTx needs the caller's transaction")
	}
	if row.SHA256 == "" || row.Path == "" || row.Result == "" {
		return errors.New("store: ImportInsertTx needs path, sha256 and result")
	}
	if row.ImportedAt == "" {
		row.ImportedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := tx.Exec(`
		INSERT INTO blueprint_import (path, sha256, name, result, detail, imported_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		row.Path, row.SHA256, row.Name, row.Result, row.Detail, row.ImportedAt); err != nil {
		return fmt.Errorf("store: inserting the import ledger row for %q: %w", row.Path, err)
	}
	return nil
}

// ImportByHash reads whether a file hash was ever IMPORTED (the idempotence
// read, BP3): true means this exact bytes have been adopted, so a re-run
// writes nothing.
func (s *Store) ImportByHash(ctx context.Context, sha256 string) (ImportRow, bool, error) {
	var r ImportRow
	err := s.r.QueryRowContext(ctx, `
		SELECT id, path, sha256, name, result, detail, imported_at
		FROM blueprint_import WHERE sha256 = ? AND result = 'imported'`, sha256).
		Scan(&r.ID, &r.Path, &r.SHA256, &r.Name, &r.Result, &r.Detail, &r.ImportedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ImportRow{}, false, nil
		}
		return ImportRow{}, false, fmt.Errorf("store: import ledger lookup %q: %w", sha256, err)
	}
	return r, true, nil
}

// ImportRefusals lists the refused adoption rows (doctor's line, BP3): the
// files that never became rows and must never read as "nothing happened".
func (s *Store) ImportRefusals(ctx context.Context) ([]ImportRow, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, path, sha256, name, result, detail, imported_at
		FROM blueprint_import WHERE result = 'refused'
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: listing import refusals: %w", err)
	}
	defer rows.Close()
	var out []ImportRow
	for rows.Next() {
		var r ImportRow
		if err := rows.Scan(&r.ID, &r.Path, &r.SHA256, &r.Name, &r.Result, &r.Detail, &r.ImportedAt); err != nil {
			return nil, fmt.Errorf("store: listing import refusals: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
