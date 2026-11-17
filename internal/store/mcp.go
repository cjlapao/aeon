package store

// mcp.go — S46 T1: the MCP configuration as rows (design MG1). SQL only
// (I2): validation beyond this net lives in exactly one place,
// `internal/mcp`'s `Config.Validate`, which every writer runs on the
// assembled CANDIDATE before it calls these accessors — the database is a
// net beneath the validator, never the validator. `internal/mcp` is not
// imported here (the store has tables, not meaning); the row shapes are the
// plain ones `mcp.Assemble` consumes from the other side (T2), mapped to and
// from agent slugs by `cmd/aeon`'s admin seam (T3) because `internal/mcp`
// keys by the dispatch NAME and the database keys by S43's SLUG (MG2).
//
// The three timestamps are the caller's (D5 — the store takes no clock).
// `enabled` is a column, not a delete: a disabled server keeps its row, its
// tools and the declarations that name them (MG4); `origin` records where a
// row came from ('system' = seed, 'user' = human door, 'import' = the legacy
// file, MG3/MG10).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// McpServerRow is one mcp_server row with its tools. Args and Env hold JSON
// ARRAYS as text exactly as stored — the store never parses them (the
// validator does, through Assemble); Env entries are variable NAMES,
// never values (C17/MG13).
type McpServerRow struct {
	ID   int64
	Name string
	// Title is the friendly name the app shows, Description what the server is for;
	// both are display text (Name stays the slug) and may be empty.
	Title         string
	Description   string
	Tier          string // "public" | "private"
	AgentSlug     *string
	TransportType string // "stdio" | "http"
	Command       string
	Args          string // JSON array, e.g. ["a","b"]
	Env           string // JSON array of NAMES, e.g. ["TOKEN"]
	// EnvFrom binds each env NAME to a variable of the variable manager: a JSON
	// object NAME -> variable name. A name with no entry is an unbound legacy one.
	EnvFrom   string
	URL       string
	Enabled   bool
	Origin    string // "system" | "user" | "import"
	CreatedAt string
	UpdatedAt string
	Tools     []McpToolRow
}

// McpToolRow is one mcp_server_tool row. Empty Rerun/Effect is UNCLASSIFIED
// (MC6) — stored as "", never defaulted to a safe-looking class.
type McpToolRow struct {
	Name   string
	Rerun  string
	Effect string
}

// McpAgentRow is one mcp_agent row. Core nil means the WHOLE core set; a
// non-nil JSON array is the agent's core list — "[]" (none) is a different
// fact from nil and stays distinct across the round trip (MC's two empties).
type McpAgentRow struct {
	AgentSlug string
	Core      *string
}

// McpDeclareRow is one mcp_declare row.
type McpDeclareRow struct {
	AgentSlug string
	Qualified string
}

// McpAll is the whole configuration in one consistent read — the input to
// mcp.Assemble (T2). Empty slices are the level-0 state, not an error.
type McpAll struct {
	Servers  []McpServerRow
	Agents   []McpAgentRow
	Declares []McpDeclareRow
	// Shared names the public servers offered to every agent (mcp_share).
	Shared []string
}

// McpChange is one mcp_change audit row: append-only by trigger (MG1). The
// three id columns are pointers so absent means the column's NULL — history
// outlives rows, so nothing here FKs anywhere.
type McpChange struct {
	ID         int64
	Op         string // server_add | server_update | server_remove | server_enable | server_disable | grant_declare | grant_revoke | core_set | import | seed
	Server     *string
	AgentSlug  *string
	Qualified  *string
	Summary    string
	ActorKind  string // "user" | "cli" | "system"
	ActorID    string
	OccurredAt string
}

// McpImport is one mcp_import ledger row — the adoption ledger of MG3.
type McpImport struct {
	ID         int64
	Path       string
	Sha256     string
	Result     string // "imported" | "refused"
	Detail     string
	Servers    int
	ImportedAt string
}

// McpRef is one MCP reference holding an agent in place, for the named
// agent-delete refusal (T5): Kind is "server" (Name = the server's name),
// "declare" (Name = the qualified tool) or "agent" (Name = the slug, an
// mcp_agent row exists — its core narrowing).
type McpRef struct {
	Kind string
	Name string
}

// McpWithTx opens one transaction on the single writer and runs fn over it,
// committing on success and rolling back on error — the same idiom as
// SeedWithTx (the store's read-then-write rule, DB15: rows and their audit
// row commit together, so a crash leaves the previous state intact).
func (s *Store) McpWithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: mcp with tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: mcp commit: %w", err)
	}
	return nil
}

// McpRows reads the whole configuration in ONE read-only transaction, so a
// concurrent writer can never yield a torn config — every slice of McpAll
// came from the same snapshot, which is what lets Assemble run its one
// validator on a set of rows that actually coexisted. Order is deterministic
// (servers by name, tools by name, agents and declares by slug/qualified)
// so Assemble's output is byte-stable.
func (s *Store) McpRows(ctx context.Context) (McpAll, error) {
	var out McpAll
	// The reader pool, under its own name — the ReadPinnedLoop idiom: a
	// read-only transaction belongs on s.r, and the named local keeps it
	// out of the O6 guard's write-verb grep (loops_test.go names the shape).
	reader := s.r
	tx, err := reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, fmt.Errorf("store: mcp rows: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	srows, err := tx.QueryContext(ctx, `
		SELECT id, name, title, description, tier, agent_slug, transport_type, command, args, env, env_from, url,
		       enabled, origin, created_at, updated_at
		FROM mcp_server
		ORDER BY name`)
	if err != nil {
		return out, fmt.Errorf("store: mcp rows: servers: %w", err)
	}
	defer srows.Close()
	for srows.Next() {
		var r McpServerRow
		if err := srows.Scan(&r.ID, &r.Name, &r.Title, &r.Description, &r.Tier, &r.AgentSlug, &r.TransportType,
			&r.Command, &r.Args, &r.Env, &r.EnvFrom, &r.URL, &r.Enabled, &r.Origin,
			&r.CreatedAt, &r.UpdatedAt); err != nil {
			return out, fmt.Errorf("store: mcp rows: scan server: %w", err)
		}
		out.Servers = append(out.Servers, r)
	}
	if err := srows.Err(); err != nil {
		return out, fmt.Errorf("store: mcp rows: servers: %w", err)
	}

	// One tools read for every server, ordered by server name then tool
	// name, attached by server id — cheaper than N+1 and deterministic.
	trows, err := tx.QueryContext(ctx, `
		SELECT t.server_id, t.name, t.rerun, t.effect
		FROM mcp_server_tool t
		JOIN mcp_server m ON m.id = t.server_id
		ORDER BY m.name, t.name`)
	if err != nil {
		return out, fmt.Errorf("store: mcp rows: tools: %w", err)
	}
	defer trows.Close()
	toolsByServer := map[int64][]McpToolRow{}
	for trows.Next() {
		var id int64
		var t McpToolRow
		if err := trows.Scan(&id, &t.Name, &t.Rerun, &t.Effect); err != nil {
			return out, fmt.Errorf("store: mcp rows: scan tool: %w", err)
		}
		toolsByServer[id] = append(toolsByServer[id], t)
	}
	if err := trows.Err(); err != nil {
		return out, fmt.Errorf("store: mcp rows: tools: %w", err)
	}
	for i := range out.Servers {
		out.Servers[i].Tools = toolsByServer[out.Servers[i].ID]
	}

	arows, err := tx.QueryContext(ctx, `
		SELECT agent_slug, core FROM mcp_agent ORDER BY agent_slug`)
	if err != nil {
		return out, fmt.Errorf("store: mcp rows: agents: %w", err)
	}
	defer arows.Close()
	for arows.Next() {
		var r McpAgentRow
		if err := arows.Scan(&r.AgentSlug, &r.Core); err != nil {
			return out, fmt.Errorf("store: mcp rows: scan agent: %w", err)
		}
		out.Agents = append(out.Agents, r)
	}
	if err := arows.Err(); err != nil {
		return out, fmt.Errorf("store: mcp rows: agents: %w", err)
	}

	drows, err := tx.QueryContext(ctx, `
		SELECT agent_slug, qualified FROM mcp_declare ORDER BY agent_slug, qualified`)
	if err != nil {
		return out, fmt.Errorf("store: mcp rows: declares: %w", err)
	}
	defer drows.Close()
	for drows.Next() {
		var r McpDeclareRow
		if err := drows.Scan(&r.AgentSlug, &r.Qualified); err != nil {
			return out, fmt.Errorf("store: mcp rows: scan declare: %w", err)
		}
		out.Declares = append(out.Declares, r)
	}
	if err := drows.Err(); err != nil {
		return out, fmt.Errorf("store: mcp rows: declares: %w", err)
	}

	if out.Shared, err = mcpSharedTx(ctx, tx); err != nil {
		return out, err
	}

	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("store: mcp rows: commit read: %w", err)
	}
	return out, nil
}

// McpServerGet reads one server by name with its tools; false is no row.
func (s *Store) McpServerGet(ctx context.Context, name string) (McpServerRow, bool, error) {
	var r McpServerRow
	err := s.r.QueryRowContext(ctx, `
		SELECT id, name, title, description, tier, agent_slug, transport_type, command, args, env, env_from, url,
		       enabled, origin, created_at, updated_at
		FROM mcp_server WHERE name = ?`, name).
		Scan(&r.ID, &r.Name, &r.Title, &r.Description, &r.Tier, &r.AgentSlug, &r.TransportType,
			&r.Command, &r.Args, &r.Env, &r.EnvFrom, &r.URL, &r.Enabled, &r.Origin,
			&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return McpServerRow{}, false, nil
		}
		return McpServerRow{}, false, fmt.Errorf("store: mcp server get %q: %w", name, err)
	}
	tools, err := s.mcpToolsByServerID(ctx, r.ID)
	if err != nil {
		return McpServerRow{}, false, err
	}
	r.Tools = tools
	return r, true, nil
}

func (s *Store) mcpToolsByServerID(ctx context.Context, id int64) ([]McpToolRow, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT name, rerun, effect FROM mcp_server_tool WHERE server_id = ? ORDER BY name`, id)
	if err != nil {
		return nil, fmt.Errorf("store: mcp tools of server %d: %w", id, err)
	}
	defer rows.Close()
	var out []McpToolRow
	for rows.Next() {
		var t McpToolRow
		if err := rows.Scan(&t.Name, &t.Rerun, &t.Effect); err != nil {
			return nil, fmt.Errorf("store: mcp tools of server %d: scan: %w", id, err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: mcp tools of server %d: %w", id, err)
	}
	return out, nil
}

// McpIsEmpty reports the level-0 state: no server, no agent row and no
// declaration anywhere. It is the question the daemon asks to decide between
// the rows and the legacy adoption check (T4), and what makes the
// mcpAbsentLines words print unchanged (T3).
func (s *Store) McpIsEmpty(ctx context.Context) (bool, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM mcp_server)
		     + (SELECT COUNT(*) FROM mcp_agent)
		     + (SELECT COUNT(*) FROM mcp_declare)`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: mcp empty: %w", err)
	}
	return n == 0, nil
}

// --- writers: every one is a Tx function, so rows and their audit row are
// one transaction (the caller opens it with McpWithTx and pairs the write
// with McpChangeInsertTx; a failed candidate validation never gets here).

// McpAddServerTx inserts one server and its tools in the open transaction;
// it returns the new id. The caller has validated the whole candidate
// (Assemble → Config.Validate); these statements are the net, not the gate.
func McpAddServerTx(ctx context.Context, tx *sql.Tx, r McpServerRow) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_server (name, title, description, tier, agent_slug, transport_type, command, args, env, env_from, url,
		                        enabled, origin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.Title, r.Description, r.Tier, r.AgentSlug, r.TransportType, r.Command, r.Args, r.Env, orEmptyObject(r.EnvFrom), r.URL,
		r.Enabled, r.Origin, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return 0, fmt.Errorf("store: mcp add server %q: %w", r.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: mcp add server %q: id: %w", r.Name, err)
	}
	for _, t := range r.Tools {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_server_tool (server_id, name, rerun, effect) VALUES (?, ?, ?, ?)`,
			id, t.Name, t.Rerun, t.Effect); err != nil {
			return 0, fmt.Errorf("store: mcp add server %q: tool %q: %w", r.Name, t.Name, err)
		}
	}
	return id, nil
}

// McpUpdateServerTx replaces the provided whole fields of a server (its
// transport and/or its tools — whole-field replacement, no partial tool
// patches) and bumps updated_at. Renaming is not a thing (the name IS the
// identity, MG5): the WHERE keys by name.
func McpUpdateServerTx(ctx context.Context, tx *sql.Tx, r McpServerRow) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE mcp_server
		SET title = ?, description = ?, transport_type = ?, command = ?, args = ?, env = ?, env_from = ?, url = ?, updated_at = ?
		WHERE name = ?`,
		r.Title, r.Description, r.TransportType, r.Command, r.Args, r.Env, orEmptyObject(r.EnvFrom), r.URL, r.UpdatedAt, r.Name)
	if err != nil {
		return fmt.Errorf("store: mcp update server %q: %w", r.Name, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("store: mcp update server %q: no such server", r.Name)
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mcp_server WHERE name = ?`, r.Name).Scan(&id); err != nil {
		return fmt.Errorf("store: mcp update server %q: id: %w", r.Name, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_server_tool WHERE server_id = ?`, id); err != nil {
		return fmt.Errorf("store: mcp update server %q: clear tools: %w", r.Name, err)
	}
	for _, t := range r.Tools {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_server_tool (server_id, name, rerun, effect) VALUES (?, ?, ?, ?)`,
			id, t.Name, t.Rerun, t.Effect); err != nil {
			return fmt.Errorf("store: mcp update server %q: tool %q: %w", r.Name, t.Name, err)
		}
	}
	return nil
}

// McpRemoveServerTx deletes a server; its tools cascade (ON DELETE CASCADE).
// False is no row. Declarations naming its tools are NOT touched here —
// refusing while declared (or cascading explicitly) is the change logic's
// decision (T2's With(RemoveServer), T5's handler), never a silent revoke.
func McpRemoveServerTx(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM mcp_server WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("store: mcp remove server %q: %w", name, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// McpSetEnabledTx flips enabled; False is no such server. A disable keeps
// the row, the tools and every declaration (MG4).
func McpSetEnabledTx(ctx context.Context, tx *sql.Tx, name string, enabled bool, updatedAt string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE mcp_server SET enabled = ?, updated_at = ? WHERE name = ?`, enabled, updatedAt, name)
	if err != nil {
		return false, fmt.Errorf("store: mcp set enabled %q: %w", name, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// McpDeclareTx adds a declaration; false is "already declared" (a no-op the
// caller reports as changed:false — idempotence with a reason, never an error).
func McpDeclareTx(ctx context.Context, tx *sql.Tx, agentSlug, qualified string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO mcp_declare (agent_slug, qualified) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		agentSlug, qualified)
	if err != nil {
		return false, fmt.Errorf("store: mcp declare %q for %q: %w", qualified, agentSlug, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// McpRevokeTx removes a declaration; false is "was not declared" (the no-op
// MG5's result shape carries as changed:false).
func McpRevokeTx(ctx context.Context, tx *sql.Tx, agentSlug, qualified string) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`DELETE FROM mcp_declare WHERE agent_slug = ? AND qualified = ?`, agentSlug, qualified)
	if err != nil {
		return false, fmt.Errorf("store: mcp revoke %q from %q: %w", qualified, agentSlug, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// McpUndeclarePrefixTx removes every declaration whose qualified name starts
// with prefix (the server-remove cascade, MG4's counterpart: removing a
// server takes its declarations with it — they name a tool that no longer
// exists; T2's candidate `RemoveServer` refuses by default and only cascades
// on the explicit flag). Returns how many rows went. The prefix is a
// `name.` string built by the caller (the dot is the namespace join, MC8 —
// a bare name could not match another server's longer prefix).
func McpUndeclarePrefixTx(ctx context.Context, tx *sql.Tx, prefix string) (int, error) {
	// substr-compare, NOT LIKE: server names legally contain `_` (serverNameRE),
	// which LIKE reads as a one-character wildcard — an exact byte prefix is
	// the fact being asked for.
	res, err := tx.ExecContext(ctx,
		`DELETE FROM mcp_declare WHERE substr(qualified, 1, ?) = ?`, len(prefix), prefix)
	if err != nil {
		return 0, fmt.Errorf("store: mcp undeclare prefix %q: %w", prefix, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// McpCoreSetTx upserts an agent's core list; core nil stores NULL (the
// WHOLE core set), core "[]" stores none — the two facts never collapse
// (the MC two-empties rule, carried into the rows).
func McpCoreSetTx(ctx context.Context, tx *sql.Tx, agentSlug string, core *string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_agent (agent_slug, core) VALUES (?, ?)
		ON CONFLICT(agent_slug) DO UPDATE SET core = excluded.core`,
		agentSlug, core)
	if err != nil {
		return fmt.Errorf("store: mcp core set %q: %w", agentSlug, err)
	}
	return nil
}

// McpAgentDeleteTx removes an agent's mcp_agent row when the agent itself
// leaves (S46 T5, agentmgr's delete path). It exists for that one caller:
// the narrowing row is the agent's own — deleting it with the agent orphans
// nothing (declarations are the referenced rows, and those block the delete
// by name long before this runs).
func McpAgentDeleteTx(ctx context.Context, tx *sql.Tx, agentSlug string) (bool, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM mcp_agent WHERE agent_slug = ?`, agentSlug)
	if err != nil {
		return false, fmt.Errorf("store: mcp agent delete %q: %w", agentSlug, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// McpChangeInsertTx appends one audit row. There is deliberately no update
// or delete accessor (MG1: append-only by trigger — the trigger is the
// enforcement, mcp_test.go is the proof).
func McpChangeInsertTx(ctx context.Context, tx *sql.Tx, c McpChange) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_change (op, server, agent_slug, qualified, summary, actor_kind, actor_id, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Op, c.Server, c.AgentSlug, c.Qualified, c.Summary, c.ActorKind, c.ActorID, c.OccurredAt)
	if err != nil {
		return fmt.Errorf("store: mcp change insert (%s): %w", c.Op, err)
	}
	return nil
}

// McpChangeList reads the newest audit rows, newest first, filtered by
// server and/or agent when given. limit is clamped to [1,500], default 50 —
// the same discipline PolicyChangeList carries.
func (s *Store) McpChangeList(ctx context.Context, server, agent string, limit int) ([]McpChange, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	var (
		where string
		args  []any
	)
	if server != "" && agent != "" {
		where = "WHERE (server = ? OR agent_slug = ?)"
		args = append(args, server, agent)
	} else if server != "" {
		where = "WHERE server = ?"
		args = append(args, server)
	} else if agent != "" {
		where = "WHERE agent_slug = ?"
		args = append(args, agent)
	}
	rows, err := s.r.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, op, server, agent_slug, qualified, summary, actor_kind, actor_id, occurred_at
		FROM mcp_change
		%s
		ORDER BY id DESC
		LIMIT ?`, where), append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: mcp change list: %w", err)
	}
	defer rows.Close()
	var out []McpChange
	for rows.Next() {
		var c McpChange
		if err := rows.Scan(&c.ID, &c.Op, &c.Server, &c.AgentSlug, &c.Qualified,
			&c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: mcp change list: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: mcp change list: %w", err)
	}
	return out, nil
}

// --- the adoption ledger (MG3)

// McpImportInsertTx appends one ledger row (imported or refused). The
// partial unique index makes a second 'imported' row for the same file
// content impossible (idempotence by content hash, never by path — the
// path can move); 'refused' rows may repeat, each a dated answer.
func McpImportInsertTx(ctx context.Context, tx *sql.Tx, r McpImport) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_import (path, sha256, result, detail, servers, imported_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		r.Path, r.Sha256, r.Result, r.Detail, r.Servers, r.ImportedAt)
	if err != nil {
		return 0, fmt.Errorf("store: mcp import insert (%s, %s): %w", r.Result, r.Path, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: mcp import insert: id: %w", err)
	}
	return id, nil
}

// McpImportByHash reads the 'imported' ledger row for a content hash, if any
// — the adoption's idempotence question (T4).
func (s *Store) McpImportByHash(ctx context.Context, sha256 string) (McpImport, bool, error) {
	var r McpImport
	err := s.r.QueryRowContext(ctx, `
		SELECT id, path, sha256, result, detail, servers, imported_at
		FROM mcp_import WHERE sha256 = ? AND result = 'imported'`, sha256).
		Scan(&r.ID, &r.Path, &r.Sha256, &r.Result, &r.Detail, &r.Servers, &r.ImportedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return McpImport{}, false, nil
		}
		return McpImport{}, false, fmt.Errorf("store: mcp import by hash: %w", err)
	}
	return r, true, nil
}

// McpReferencesFor lists every MCP row that keeps an agent in place — the
// single accessor T5's agent-delete refusal consults beside agentmgr's own
// blueprint/policy checks: private servers, declarations, and an mcp_agent
// row (its core narrowing). The refusal it feeds names every reference (MG1).
func (s *Store) McpReferencesFor(ctx context.Context, slug string) ([]McpRef, error) {
	var out []McpRef
	srows, err := s.r.QueryContext(ctx, `SELECT name FROM mcp_server WHERE agent_slug = ? ORDER BY name`, slug)
	if err != nil {
		return nil, fmt.Errorf("store: mcp references for %q: servers: %w", slug, err)
	}
	defer srows.Close()
	for srows.Next() {
		var n string
		if err := srows.Scan(&n); err != nil {
			return nil, fmt.Errorf("store: mcp references for %q: scan server: %w", slug, err)
		}
		out = append(out, McpRef{Kind: "server", Name: n})
	}
	if err := srows.Err(); err != nil {
		return nil, fmt.Errorf("store: mcp references for %q: servers: %w", slug, err)
	}
	drows, err := s.r.QueryContext(ctx, `SELECT qualified FROM mcp_declare WHERE agent_slug = ? ORDER BY qualified`, slug)
	if err != nil {
		return nil, fmt.Errorf("store: mcp references for %q: declares: %w", slug, err)
	}
	defer drows.Close()
	for drows.Next() {
		var q string
		if err := drows.Scan(&q); err != nil {
			return nil, fmt.Errorf("store: mcp references for %q: scan declare: %w", slug, err)
		}
		out = append(out, McpRef{Kind: "declare", Name: q})
	}
	if err := drows.Err(); err != nil {
		return nil, fmt.Errorf("store: mcp references for %q: declares: %w", slug, err)
	}
	var one int
	err = s.r.QueryRowContext(ctx, `SELECT 1 FROM mcp_agent WHERE agent_slug = ?`, slug).Scan(&one)
	if err == nil {
		out = append(out, McpRef{Kind: "agent", Name: slug})
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: mcp references for %q: agent row: %w", slug, err)
	}
	return out, nil
}

// McpWipeConfigTx removes ALL configuration rows — servers (their tools
// ride the CASCADE), declarations, and agent narrowings — leaving the
// append-only audit and the import ledger exactly where they stand. It
// exists for ONE caller: `mcp.importFile mode:replace` (S46 T4), whose law
// (MG3) is that an import REPLACES the whole configuration, never merges
// into it, and does so inside the same transaction that writes the new
// world (all-or-nothing — a torn replace is forbidden by construction by
// never existing outside a Tx). The audit is append-only by trigger, so
// wipe-then-insert leaves the who-did-what history intact by design.
func McpWipeConfigTx(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DELETE FROM mcp_declare`,
		`DELETE FROM mcp_server`,
		`DELETE FROM mcp_agent`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("store: mcp replace wipe (%s): %w", q, err)
		}
	}
	return nil
}

// McpImportLatestByPath reads the newest ledger row for one file path,
// imported or refused — the doctor's "refused at <date>: <sentence>" line
// reads through here (the only reader that asks what the importer last
// SAID about a specific file).
func (s *Store) McpImportLatestByPath(ctx context.Context, path string) (McpImport, bool, error) {
	var r McpImport
	err := s.r.QueryRowContext(ctx, `
		SELECT id, path, sha256, result, detail, servers, imported_at
		FROM mcp_import WHERE path = ? ORDER BY id DESC LIMIT 1`, path).
		Scan(&r.ID, &r.Path, &r.Sha256, &r.Result, &r.Detail, &r.Servers, &r.ImportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return McpImport{}, false, nil
	}
	if err != nil {
		return McpImport{}, false, fmt.Errorf("store: mcp import ledger by path %q: %w", path, err)
	}
	return r, true, nil
}

// orEmptyObject keeps an unset binding column a valid JSON object.
func orEmptyObject(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}
