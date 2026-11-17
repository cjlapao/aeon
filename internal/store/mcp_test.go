package store

// The tests for the MCP tables (S46 T1, MG1/MG2/MG3/MC6/MC8). The shape
// follows the policy_change/setting_event precedents: the migration gets its
// own catalogue proof (fresh apply AND upgrade pinned BY IDENTITY via
// migrationsBefore), the CHECK net is asked directly through raw writes (the
// sanctioned way to test the net rather than the accessors), and append-only
// is proven at the statement level — the trigger is the enforcement, this
// file is the proof.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const (
	mcpAt    = "2026-11-01T12:00:00Z"
	mcpAt2   = "2026-11-01T12:00:01Z"
	mcpTable = "_mcp_tables"
)

// mcpAgent plants an agent row with the two columns every MCP FK points at.
// The agent table's content columns default to ”; only slug/name/timestamps
// are NOT NULL.
func mcpAgent(t *testing.T, s *Store, slug string) {
	t.Helper()
	if _, err := s.w.Exec(`INSERT INTO agent (slug, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		slug, slug+"-name", mcpAt, mcpAt); err != nil {
		t.Fatalf("plant agent %q: %v", slug, err)
	}
}

// mcpServer builds a valid public stdio row for the accessor tests; callers
// vary the fields the case is about.
func mcpServer(name string) McpServerRow {
	return McpServerRow{
		Name: name, Tier: "public", TransportType: "stdio", Command: "run",
		Args: "[]", Env: "[]", Enabled: true, Origin: "user",
		CreatedAt: mcpAt, UpdatedAt: mcpAt,
	}
}

func mcpChangeRow(op string) McpChange {
	srv := "alpha"
	return McpChange{Op: op, Server: &srv, Summary: op + " alpha",
		ActorKind: "user", ActorID: "1000", OccurredAt: mcpAt}
}

func TestMcpMigrationCreatesTablesIndexesTriggers(t *testing.T) {
	want := ""
	for _, m := range Migrations() {
		if strings.HasSuffix(m.Name, mcpTable) {
			want = m.Name
		}
	}
	if want == "" {
		t.Fatal("no _mcp_tables migration in Migrations()")
	}

	s := openTemp(t)
	for _, tbl := range []string{"mcp_server", "mcp_server_tool", "mcp_agent", "mcp_declare", "mcp_change", "mcp_import"} {
		assertCatalogue(t, s, "table", tbl)
	}
	assertColumns(t, s, "mcp_server", "id", "name", "tier", "agent_slug", "transport_type",
		"command", "args", "env", "url", "enabled", "origin", "created_at", "updated_at")
	assertColumns(t, s, "mcp_server_tool", "server_id", "name", "rerun", "effect")
	assertColumns(t, s, "mcp_agent", "agent_slug", "core")
	assertColumns(t, s, "mcp_declare", "agent_slug", "qualified")
	assertColumns(t, s, "mcp_change", "id", "op", "server", "agent_slug", "qualified",
		"summary", "actor_kind", "actor_id", "occurred_at")
	assertColumns(t, s, "mcp_import", "id", "path", "sha256", "result", "detail", "servers", "imported_at")
	assertCatalogue(t, s, "index", "idx_mcp_server_name")
	assertCatalogue(t, s, "index", "idx_mcp_server_agent")
	assertCatalogue(t, s, "index", "idx_mcp_change_server")
	assertCatalogue(t, s, "index", "idx_mcp_import_hash")
	assertCatalogue(t, s, "trigger", "mcp_change_no_update")
	assertCatalogue(t, s, "trigger", "mcp_change_no_delete")
	if err := assertLedgerHas(s.AppliedMigrations, want); err != nil {
		t.Fatalf("fresh apply: %v", err)
	}

	// Upgrade from the vintage strictly BEFORE this file — pinned by
	// IDENTITY (the S22 T1 / S55 lesson), never `[:len-1]`.
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	g, closeDB := rawGorm(t, path)
	applyMigrations(t, g, migrationsBefore(t, mcpTable))
	closeDB()
	s2, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("reopen the pre-migration database: %v", err)
	}
	for _, tbl := range []string{"mcp_server", "mcp_server_tool", "mcp_agent", "mcp_declare", "mcp_change", "mcp_import"} {
		assertCatalogue(t, s2, "table", tbl)
	}
	if err := assertLedgerHas(s2.AppliedMigrations, want); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
}

func TestMcpServerTierOwnerCheck(t *testing.T) {
	s := openTemp(t)
	mcpAgent(t, s, "agent-a")
	// public with an agent — refused.
	_, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('pub','public','agent-a','stdio',?,?)`, mcpAt, mcpAt)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("public server with agent_slug was accepted: %v", err)
	}
	// private without one — refused.
	_, err = s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('priv','private',NULL,'stdio',?,?)`, mcpAt, mcpAt)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("private server without agent_slug was accepted: %v", err)
	}
	// The good shapes pass.
	if _, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('priv','private','agent-a','stdio',?,?)`, mcpAt, mcpAt); err != nil {
		t.Errorf("private server with agent refused: %v", err)
	}
}

func TestMcpServerNameUniqueGlobally(t *testing.T) {
	s := openTemp(t)
	mcpAgent(t, s, "agent-a")
	mcpAgent(t, s, "agent-b")
	if _, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, transport_type, created_at, updated_at)
		VALUES ('shared','public','stdio',?,?)`, mcpAt, mcpAt); err != nil {
		t.Fatalf("plant public 'shared': %v", err)
	}
	// Same name as a private server — refused (MC8: one namespace).
	_, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('shared','private','agent-a','stdio',?,?)`, mcpAt, mcpAt)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("private re-using a public name was accepted: %v", err)
	}
	// Two private servers under two agents sharing a name — refused.
	if _, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('pv','private','agent-a','stdio',?,?)`, mcpAt, mcpAt); err != nil {
		t.Fatalf("plant private 'pv' under agent-a: %v", err)
	}
	_, err = s.w.Exec(`INSERT INTO mcp_server (name, tier, agent_slug, transport_type, created_at, updated_at)
		VALUES ('pv','private','agent-b','stdio',?,?)`, mcpAt, mcpAt)
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("the same private name under two agents was accepted: %v", err)
	}
}

func TestMcpServerNameChecksAreNoStricterThanValidator(t *testing.T) {
	// Every name Config.Validate's serverNameRE accepts (^[a-z][a-z0-9_-]*$,
	// within the 1–64 the validator also enforces) must pass the SQL net:
	// the net may be looser, never tighter (T1.md; a net that rejects a
	// valid config makes the one validator a liar).
	names := []string{"a", "srv", "srv1", "a_b", "a-b", "a-1_2", "git-forge", "x0",
		"mcp__deepwiki", "search_http", "a2", strings.Repeat("z", 64)}
	s := openTemp(t)
	for _, n := range names {
		if _, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, transport_type, created_at, updated_at)
			VALUES (?, 'public', 'stdio', ?, ?)`, n, mcpAt, mcpAt); err != nil {
			t.Errorf("SQL refused a name the validator's grammar accepts (%q): %v", n, err)
		}
	}
}

func TestMcpAgentFKRestrict(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	mcpAgent(t, s, "agent-a")

	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpAddServerTx(ctx, tx, McpServerRow{Name: "priv", Tier: "private",
			AgentSlug: strp("agent-a"), TransportType: "stdio", Command: "c",
			Args: "[]", Env: "[]", Enabled: true, Origin: "user", CreatedAt: mcpAt, UpdatedAt: mcpAt})
		return err
	}); err != nil {
		t.Fatalf("add private server: %v", err)
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpCoreSetTx(ctx, tx, "agent-a", nil) }); err != nil {
		t.Fatalf("core set: %v", err)
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpDeclareTx(ctx, tx, "agent-a", "pub.tool")
		return err
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	// Deleting the referenced agent fails from all three directions.
	if _, err := s.w.Exec(`DELETE FROM agent WHERE slug = 'agent-a'`); err == nil ||
		!strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Errorf("agent delete was not RESTRICT-ed (got %v)", err)
	}
	// An unresolvable slug is refused at write time.
	if _, err := s.w.Exec(`INSERT INTO mcp_declare (agent_slug, qualified) VALUES ('no-such', 'x.y')`); err == nil ||
		!strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Errorf("declare for an unknown slug was accepted (got %v)", err)
	}
}

func TestMcpToolCascadeOnServerDelete(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	r := mcpServer("alpha")
	r.Tools = []McpToolRow{{Name: "t1", Rerun: "safe", Effect: "outward"}, {Name: "t2"}}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpAddServerTx(ctx, tx, r)
		return err
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		ok, err := McpRemoveServerTx(ctx, tx, "alpha")
		if !ok || err != nil {
			return fmt.Errorf("remove ok=%v err=%v", ok, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM mcp_server_tool`).Scan(&n); err != nil || n != 0 {
		t.Errorf("orphan tools survived the server delete: n=%d err=%v", n, err)
	}
}

func TestMcpUnclassifiedIsStoredEmptyNotDefaulted(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	r := mcpServer("alpha")
	r.Tools = []McpToolRow{{Name: "x"}} // no rerun, no effect — UNCLASSIFIED (MC6)
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpAddServerTx(ctx, tx, r)
		return err
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, found, err := s.McpServerGet(ctx, "alpha")
	if err != nil || !found || len(got.Tools) != 1 {
		t.Fatalf("get: found=%v err=%v tools=%d", found, err, len(got.Tools))
	}
	if got.Tools[0].Rerun != "" || got.Tools[0].Effect != "" {
		t.Errorf("unclassified was defaulted to %q/%q — MC6 stores '' (\"refused until classified\"), never a safe-looking class",
			got.Tools[0].Rerun, got.Tools[0].Effect)
	}
}

func TestMcpCoreNullVersusEmptyArrayDistinct(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	mcpAgent(t, s, "a1")
	mcpAgent(t, s, "a2")
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpCoreSetTx(ctx, tx, "a1", nil) }); err != nil {
		t.Fatalf("core nil: %v", err)
	}
	empty := "[]"
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpCoreSetTx(ctx, tx, "a2", &empty) }); err != nil {
		t.Fatalf("core []: %v", err)
	}
	all, err := s.McpRows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	bySlug := map[string]*string{}
	for _, a := range all.Agents {
		bySlug[a.AgentSlug] = a.Core
	}
	if v, ok := bySlug["a1"]; !ok || v != nil {
		t.Errorf("core NULL came back as %v — NULL means the WHOLE core set", bySlug["a1"])
	}
	if v, ok := bySlug["a2"]; !ok || v == nil || *v != "[]" {
		t.Errorf("core '[]' came back as %v — \"none\" must stay distinct from \"the whole set\"", v)
	}
}

func TestMcpEnvIsJSONArrayOfNamesOnly(t *testing.T) {
	s := openTemp(t)
	// A JSON object (not an array) in env is refused by the net.
	_, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, transport_type, env, created_at, updated_at)
		VALUES ('bad','public','stdio','{"TOKEN":"hunter2"}',?,?)`, mcpAt, mcpAt)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("a non-array env was accepted (got %v)", err)
	}
	// An array of names passes — and round-trips untouched.
	if _, err := s.w.Exec(`INSERT INTO mcp_server (name, tier, transport_type, env, args, created_at, updated_at)
		VALUES ('good','public','stdio','["TOKEN","OTHER"]','["serve"]',?,?)`, mcpAt, mcpAt); err != nil {
		t.Fatalf("array env refused: %v", err)
	}
	got, _, err := s.McpServerGet(context.Background(), "good")
	if err != nil || got.Env != `["TOKEN","OTHER"]` || got.Args != `["serve"]` {
		t.Errorf("transport JSON did not round-trip verbatim: %q/%q err=%v", got.Env, got.Args, err)
	}
	var names []string
	if err := json.Unmarshal([]byte(got.Env), &names); err != nil || len(names) != 2 {
		t.Errorf("env is not a JSON array of names: %q (%v)", got.Env, err)
	}
}

func TestMcpChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpChangeInsertTx(ctx, tx, mcpChangeRow("server_add")) }); err != nil {
		t.Fatalf("insert audit: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE mcp_change SET summary = 'rewritten'`); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE on mcp_change was allowed (got %v)", err)
	}
	if _, err := s.w.Exec(`DELETE FROM mcp_change`); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE on mcp_change was allowed (got %v)", err)
	}
	// The CHECK vocabularies refuse too (the net under the callers).
	bad := mcpChangeRow("widen_everything")
	err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpChangeInsertTx(ctx, tx, bad) })
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("an unknown audit op was accepted (got %v)", err)
	}
}

func TestMcpImportLedgerIsUniquePerHash(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	sha := strings.Repeat("ab", 32) // 64 hex chars
	imp := func(result string) error {
		return s.McpWithTx(ctx, func(tx *sql.Tx) error {
			_, err := McpImportInsertTx(ctx, tx, McpImport{Path: "/p/mcp.json", Sha256: sha,
				Result: result, Detail: result, Servers: 3, ImportedAt: mcpAt})
			return err
		})
	}
	if err := imp("imported"); err != nil {
		t.Fatalf("first imported: %v", err)
	}
	if err := imp("imported"); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("a second 'imported' row for the same hash was accepted (got %v)", err)
	}
	if err := imp("refused"); err != nil {
		t.Errorf("a second (refused) row for the same hash must be allowed: %v", err)
	}
	if err := imp("refused"); err != nil {
		t.Errorf("refused rows must repeat freely (each a dated answer): %v", err)
	}
	got, found, err := s.McpImportByHash(ctx, sha)
	if err != nil || !found || got.Result != "imported" || got.Servers != 3 {
		t.Errorf("McpImportByHash: found=%v got=%+v err=%v", found, got, err)
	}
	if _, found, _ := s.McpImportByHash(ctx, strings.Repeat("cd", 32)); found {
		t.Error("McpImportByHash found a row that was never written")
	}
	// A non-64-char hash is refused.
	err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpImportInsertTx(ctx, tx, McpImport{Path: "/p/x", Sha256: "tooshort",
			Result: "refused", Detail: "d", ImportedAt: mcpAt})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("a 8-char sha256 was accepted (got %v)", err)
	}
}

func TestMcpRowsIsOneConsistentRead(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	// Plant one server, then start an UNCOMMITTED writer that adds a second
	// and flips the first. McpRows must read the pre-write snapshot for
	// every table — a torn read would hand Assemble a set of rows that never
	// coexisted, and the one validator would refuse a config nobody wrote.
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpAddServerTx(ctx, tx, mcpServer("first"))
		return err
	}); err != nil {
		t.Fatalf("plant: %v", err)
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin concurrent writer: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO mcp_server (name, tier, transport_type, created_at, updated_at)
		VALUES ('second','public','stdio',?,?)`, mcpAt, mcpAt); err != nil {
		t.Fatalf("concurrent insert: %v", err)
	}
	if _, err := tx.Exec(`UPDATE mcp_server SET enabled = 0 WHERE name = 'first'`); err != nil {
		t.Fatalf("concurrent update: %v", err)
	}
	all, err := s.McpRows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(all.Servers) != 1 || all.Servers[0].Name != "first" || !all.Servers[0].Enabled {
		t.Errorf("McpRows read uncommitted state: %+v", all.Servers)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit the concurrent writer: %v", err)
	}
	all, err = s.McpRows(ctx)
	if err != nil {
		t.Fatalf("rows after commit: %v", err)
	}
	if len(all.Servers) != 2 {
		t.Errorf("the committed write is not visible to the next read: %+v", all.Servers)
	}
}

func TestMcpWriteAndChangeAreAtomic(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	// A forced audit failure (unknown op) must roll the server write back —
	// no server without its history.
	err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		if _, err := McpAddServerTx(ctx, tx, mcpServer("doomed")); err != nil {
			return err
		}
		bad := mcpChangeRow("not_an_op")
		return McpChangeInsertTx(ctx, tx, bad)
	})
	if err == nil {
		t.Fatal("the transaction committed despite a failing audit row")
	}
	if empty, err := s.McpIsEmpty(ctx); err != nil || !empty {
		t.Errorf("the rolled-back write left rows: empty=%v err=%v", empty, err)
	}
	// The good path writes both. (The audit row names the server it is
	// about — the helper's row names "alpha", so build the row here.)
	kept := "kept"
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		if _, err := McpAddServerTx(ctx, tx, mcpServer("kept")); err != nil {
			return err
		}
		return McpChangeInsertTx(ctx, tx, McpChange{Op: "server_add", Server: &kept,
			Summary: "server_add kept", ActorKind: "user", ActorID: "1000", OccurredAt: mcpAt})
	}); err != nil {
		t.Fatalf("good write: %v", err)
	}
	if empty, _ := s.McpIsEmpty(ctx); empty {
		t.Error("the good write left nothing")
	}
	changes, err := s.McpChangeList(ctx, "kept", "", 0)
	if err != nil || len(changes) != 1 || changes[0].Summary != "server_add kept" {
		t.Errorf("audit row missing: %+v err=%v", changes, err)
	}
}

func TestMcpChangeListFilteringAndClamp(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	srvA, srvB, slug := "alpha", "beta", "agent-a"
	for i := 0; i < 3; i++ {
		if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
			return McpChangeInsertTx(ctx, tx, McpChange{Op: "server_add", Server: &srvA,
				Summary: fmt.Sprintf("a%d", i), ActorKind: "user", ActorID: "1", OccurredAt: mcpAt})
		}); err != nil {
			t.Fatalf("seed a%d: %v", i, err)
		}
	}
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		return McpChangeInsertTx(ctx, tx, McpChange{Op: "grant_declare", Server: &srvB, AgentSlug: &slug,
			Qualified: strp("beta.t1"), Summary: "b0", ActorKind: "cli", ActorID: "cli", OccurredAt: mcpAt2})
	}); err != nil {
		t.Fatalf("seed b0: %v", err)
	}
	got, err := s.McpChangeList(ctx, "alpha", "", 0)
	if err != nil || len(got) != 3 {
		t.Fatalf("filter by server: %+v err=%v", got, err)
	}
	if got[0].Summary != "a2" {
		t.Errorf("newest-first violated: %+v", got)
	}
	got, err = s.McpChangeList(ctx, "", "agent-a", 0)
	if err != nil || len(got) != 1 || got[0].Op != "grant_declare" {
		t.Fatalf("filter by agent: %+v err=%v", got, err)
	}
	// The clamp: 0 → 50 default, 1000 → 500.
	got, err = s.McpChangeList(ctx, "", "", 1000)
	if err != nil || len(got) != 4 {
		t.Fatalf("unfiltered: %+v err=%v", got, err)
	}
	got, err = s.McpChangeList(ctx, "", "", -5)
	if err != nil || len(got) != 4 {
		t.Fatalf("negative limit: %+v err=%v", got, err)
	}
	// limit=1 keeps only the newest.
	got, err = s.McpChangeList(ctx, "", "", 1)
	if err != nil || len(got) != 1 || got[0].Summary != "b0" {
		t.Errorf("limit=1: %+v err=%v", got, err)
	}
}

func TestMcpSetEnabledAndDeclareRevokeNoOps(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	mcpAgent(t, s, "agent-a")
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		_, err := McpAddServerTx(ctx, tx, mcpServer("alpha"))
		return err
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	var ok bool
	var err error
	if err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		ok, err = McpSetEnabledTx(ctx, tx, "alpha", false, mcpAt2)
		return err
	}); err != nil || !ok {
		t.Fatalf("disable: ok=%v err=%v", ok, err)
	}
	if err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
		ok, err = McpSetEnabledTx(ctx, tx, "ghost", true, mcpAt2)
		return err
	}); err != nil || ok {
		t.Errorf("enabling a nonexistent server reported success: ok=%v err=%v", ok, err)
	}
	got, _, _ := s.McpServerGet(ctx, "alpha")
	if got.Enabled {
		t.Error("enabled did not flip")
	}
	// declare twice: second is a changed:false no-op, not an error.
	for i, want := range []bool{true, false} {
		if err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
			ok, err = McpDeclareTx(ctx, tx, "agent-a", "alpha.t1")
			return err
		}); err != nil || ok != want {
			t.Errorf("declare #%d: changed=%v want=%v err=%v", i+1, ok, want, err)
		}
	}
	for i, want := range []bool{true, false} {
		if err = s.McpWithTx(ctx, func(tx *sql.Tx) error {
			ok, err = McpRevokeTx(ctx, tx, "agent-a", "alpha.t1")
			return err
		}); err != nil || ok != want {
			t.Errorf("revoke #%d: changed=%v want=%v err=%v", i+1, ok, want, err)
		}
	}
}

func TestMcpReferencesFor(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	mcpAgent(t, s, "agent-a")
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		if _, err := McpAddServerTx(ctx, tx, McpServerRow{Name: "pv", Tier: "private",
			AgentSlug: strp("agent-a"), TransportType: "stdio", Command: "c",
			Args: "[]", Env: "[]", Enabled: true, Origin: "user", CreatedAt: mcpAt, UpdatedAt: mcpAt}); err != nil {
			return err
		}
		if _, err := McpDeclareTx(ctx, tx, "agent-a", "alpha.t1"); err != nil {
			return err
		}
		return McpCoreSetTx(ctx, tx, "agent-a", nil)
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	refs, err := s.McpReferencesFor(ctx, "agent-a")
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	want := map[string]bool{"server:pv": false, "declare:alpha.t1": false, "agent:agent-a": false}
	for _, r := range refs {
		k := r.Kind + ":" + r.Name
		if _, known := want[k]; !known {
			t.Errorf("unexpected reference %+v", r)
			continue
		}
		want[k] = true
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing reference %s", k)
		}
	}
	// An unreferenced agent reports none.
	mcpAgent(t, s, "agent-z")
	refs, err = s.McpReferencesFor(ctx, "agent-z")
	if err != nil || len(refs) != 0 {
		t.Errorf("unreferenced agent: %+v err=%v", refs, err)
	}
}

func strp(s string) *string { return &s }

func TestMcpServerCarriesATitleAndADescriptionBesideItsSlug(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	r := mcpServer("github")
	r.Title, r.Description = "GitHub", "Issues and pull requests."
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { _, err := McpAddServerTx(ctx, tx, r); return err }); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.McpServerGet(ctx, "github")
	if err != nil || !found {
		t.Fatalf("get: %v %v", found, err)
	}
	if got.Name != "github" || got.Title != "GitHub" || got.Description != "Issues and pull requests." {
		t.Fatalf("the slug stays the name and the display text rides beside it: %+v", got)
	}
	got.Title, got.Description = "GitHub (work)", ""
	got.UpdatedAt = mcpAt2
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { return McpUpdateServerTx(ctx, tx, got) }); err != nil {
		t.Fatal(err)
	}
	all, err := s.McpRows(ctx)
	if err != nil || len(all.Servers) != 1 || all.Servers[0].Title != "GitHub (work)" || all.Servers[0].Description != "" {
		t.Fatalf("update must replace the display text: %+v, %v", all.Servers, err)
	}
	// A server that never had either reads empty — never a guessed label.
	if err := s.McpWithTx(ctx, func(tx *sql.Tx) error { _, err := McpAddServerTx(ctx, tx, mcpServer("plain")); return err }); err != nil {
		t.Fatal(err)
	}
	plain, _, _ := s.McpServerGet(ctx, "plain")
	if plain.Title != "" || plain.Description != "" {
		t.Fatalf("an untitled server has no title: %+v", plain)
	}
}
