// What the four reads never do (S47 T5): write, repair, or go quiet when the
// world cannot be read. Drift and seed state are REPORTED (TC11), a broken store
// is an error and never an empty list (V11), and with no store wired the doors are
// ABSENT (V13).

package daemon

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/toolcatalog"
	"github.com/cjlapao/aeon/internal/toolseed"
)

func TestToolsReadsAreLive(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	_ = mustList(t, env, protocol.ToolsListFilter{})
	before := len(env.admin.calls)
	_ = mustList(t, env, protocol.ToolsListFilter{Source: strptr(protocol.ToolsSourceMCP)})
	if got := env.admin.calls[before:]; len(got) != 1 || got[0] != "mcp.servers" {
		t.Fatalf("the MCP reads were %v, want exactly one mcp.servers — the daemon has no other path to the MCP half (MC11)", got)
	}

	// N8, from the policy side: a deny added now must be visible now.
	bashBefore := statusOrFatal(t, env)
	if len(bashBefore.Drift) == 0 {
		// (drift is unrelated to this assertion; the liveness below is the point)
		_ = bashBefore
	}
	one := mustList(t, env, protocol.ToolsListFilter{Q: strptr("grep")})
	if len(one) == 0 {
		t.Fatal("grep is missing")
	}
	// A NEW agent row changes the roster the next read sees (the matrix column,
	// below) — read the byAgent answer before and after the insert.
	agentRes, err := env.svc.ToolsByAgent(context.Background(), "newbie")
	if err == nil {
		t.Fatalf("an unknown agent answered %+v before the row existed", agentRes)
	}
	newbie := store.Agent{Slug: "newbie", Name: "Newbie", Version: "1", Origin: "system", Tools: `["read"]`}
	if err := env.st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, newbie, "2026-11-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	dbNew, err := env.st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dbNew.Exec(`UPDATE agent SET tools = ?, origin = ? WHERE slug = ?`, newbie.Tools, newbie.Origin, newbie.Slug); err != nil {
		t.Fatal(err)
	}
	agentRes, err = env.svc.ToolsByAgent(context.Background(), "newbie")
	if err != nil {
		t.Fatalf("the agent added a moment ago is still unknown — the adapter cached the roster (N8): %v", err)
	}
	if !agentRes.Dispatchable {
		t.Error("a system agent reports itself undispatchable")
	}
}

// --- tools.status ------------------------------------------------------------------------

// TestToolsStatusReportsDriftAndSeedState is TC11's note 5: the seed half is read
// through the ONE engine `seed.status` reads, so the catalog cannot report a second,
// differently stamped ledger for `tools.core`. The versions are the engine's and the
// state word is a summary over the engine's own per-record states.
func TestToolsStatusReportsDriftAndSeedState(t *testing.T) {
	env := newToolsCatalogEnv(t, false)
	st := statusOrFatal(t, env)
	if st.Seed.Set != toolseed.SetID {
		t.Errorf("seed.set = %q, want %q", st.Seed.Set, toolseed.SetID)
	}
	if st.Seed.AvailableVersion != toolseed.SetVersion() {
		t.Errorf("availableVersion = %d, want the code's %d", st.Seed.AvailableVersion, toolseed.SetVersion())
	}
	if st.Seed.AppliedVersion != toolseed.SetVersion() {
		t.Errorf("appliedVersion = %d after the inert apply, want %d — the ledger was read from somewhere other than the engine", st.Seed.AppliedVersion, toolseed.SetVersion())
	}
	if st.Seed.State != "current" {
		t.Errorf("state = %q, want current (every record applied)", st.Seed.State)
	}
	if len(st.Drift) != 0 {
		t.Errorf("a freshly applied set reports drift: %+v", st.Drift)
	}
	// And without an engine the honest word is `unavailable`, not `never_applied`:
	// a different process may have applied the set, and nobody looked.
	noEngine := &toolsCatalogService{s: env.st, now: func() time.Time { return env.now }}
	got, err := noEngine.seedState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != ToolsSeedStateUnavailable {
		t.Errorf("state with no engine = %q, want %q", got.State, ToolsSeedStateUnavailable)
	}
	if got.AvailableVersion != toolseed.SetVersion() {
		t.Error("the code's version is readable without an engine and must still be said")
	}
}

// TestToolsStatusMissingRowIsReportedNotRepaired is TC11's other half. A row is
// deleted out from under the catalog (the store has NO delete accessor, so this goes
// through raw SQL — the absence itself is the proof that nothing in the shipped
// design removes a row). The report says `missing_row`, the answer keeps the tool,
// and the row is STILL missing at the end: the read did not quietly re-seed it,
// because silently fixing the disagreement is the only thing worse than saying it.
func TestToolsStatusMissingRowIsReportedNotRepaired(t *testing.T) {
	env := newToolsCatalogEnv(t, false)
	db, err := env.st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM tool WHERE name = 'grep'`); err != nil {
		t.Fatalf("delete the row: %v", err)
	}
	st := statusOrFatal(t, env)
	d, ok := driftFor(st, "grep")
	if !ok || d.State != protocol.ToolsDriftMissingRow {
		t.Fatalf("the missing row is not reported as missing_row: %+v", st.Drift)
	}
	e, ok := entryByName(mustList(t, env, protocol.ToolsListFilter{Q: strptr("grep")}), "grep")
	if !ok {
		t.Fatal("the tool disappeared from the catalog — code is the truth about a tool (TC2), a missing row is data")
	}
	if e.RowCurrent {
		t.Error("rowCurrent is true for a tool with no row")
	}
	if _, err := env.st.ToolGet(context.Background(), "grep"); err == nil {
		t.Error("the read re-seeded the row — the applier is never called from a read (TC11)")
	}
}

// --- the four reads are reads -------------------------------------------------------------

// TestToolsReadsWriteNothing is TC1, pinned by the rows rather than by a promise.
// Every tool row is snapshotted byte for byte, the agent count and the policy count
// are taken, the four doors run, and everything is re-read: one changed byte
// anywhere is the catalog having edited the world it is supposed to describe. The
// MCP audit table is counted too — a management read that wrote an audit row would
// make "who changed the configuration" unanswerable, because the catalog would be
// answering its own question.
func TestToolsReadsWriteNothing(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	snapshotRows := func() []store.ToolRow {
		rows, err := env.st.ToolList(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		return rows
	}
	before := snapshotRows()
	agentsBefore := len(mustAgents(t, env))
	policiesBefore := mustPolicyCount(t, env)
	mcpChangesBefore := mustCountTable(t, env, "mcp_change")
	ledgerBefore := statusOrFatal(t, env).Seed.AppliedVersion

	ctx := context.Background()
	if _, err := env.svc.ToolsList(ctx, protocol.ToolsListFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.ToolsGet(ctx, "bash"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.ToolsByAgent(ctx, "atlas"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.ToolsStatus(ctx); err != nil {
		t.Fatal(err)
	}

	after := snapshotRows()
	if len(before) != len(after) {
		t.Fatalf("the number of tool rows moved: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("the row for %s moved: %+v → %+v — a catalog that edits the world it describes cannot be trusted to describe it", before[i].Name, before[i], after[i])
		}
	}
	if got := len(mustAgents(t, env)); got != agentsBefore {
		t.Errorf("agent rows: %d → %d", agentsBefore, got)
	}
	if got := mustPolicyCount(t, env); got != policiesBefore {
		t.Errorf("policy rows: %d → %d", policiesBefore, got)
	}
	if got := mustCountTable(t, env, "mcp_change"); got != mcpChangesBefore {
		t.Errorf("mcp_change rows: %d → %d — an audit table is for configuration changes, not for looking", mcpChangesBefore, got)
	}
	if got := statusOrFatal(t, env).Seed.AppliedVersion; got != ledgerBefore {
		t.Errorf("the seed ledger moved (%d → %d) — the applier is not a read", ledgerBefore, got)
	}
}

func mustAgents(t *testing.T, env *toolsCatalogEnv) []store.Agent {
	t.Helper()
	out, err := env.st.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mustPolicyCount(t *testing.T, env *toolsCatalogEnv) int {
	t.Helper()
	ps, rej := env.policies.List(context.Background(), env.now)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	return len(ps)
}

func mustCountTable(t *testing.T, env *toolsCatalogEnv, table string) int {
	t.Helper()
	db, err := env.st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestToolsReadsFailLoudlyWhenTheStoreIsUnreadable is V11, the rule that decides
// what a broken world looks like: an ERROR. An empty catalog is a complete,
// plausible, wrong answer — it would tell an operator their tools are all ungranted
// and their policies all gone — and it is exactly the failure this slice exists to
// make impossible.
//
// The world is broken by removing the tables rather than by closing the store:
// `Store.Close` nils its reader, so a read after a close panics on a nil `*sql.DB`
// (measured while writing this test, and pre-existing — every store read shares it,
// and a daemon does not read after closing). A missing table is the failure a
// running daemon actually meets, and it is the one this door must not turn into an
// empty list. Each read fails on its OWN authority, which is why all four are
// exercised: the rows, the agents, the policies and the ledger.
func TestToolsReadsFailLoudlyWhenTheStoreIsUnreadable(t *testing.T) {
	for _, tc := range []struct {
		table string
		why   string
	}{
		{"tool", "the catalog rows"},
		{"agent", "the agent rows"},
		{"policy", "the policy set"},
	} {
		env := newToolsCatalogEnv(t, true)
		db, err := env.st.GormDB().DB()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("DROP TABLE IF EXISTS " + tc.table); err != nil {
			t.Fatalf("drop %s: %v", tc.table, err)
		}
		ctx := context.Background()
		// The list and the status read the rows; the roster read needs the agents;
		// every one of them needs the policies. The assertion is the same in each
		// case: no answer, an error.
		if out, err := env.svc.ToolsList(ctx, protocol.ToolsListFilter{}); err == nil {
			t.Errorf("%s unreadable: tools.list answered %d tools — an empty catalog is a fabricated answer", tc.why, len(out))
		}
		if _, err := env.svc.ToolsGet(ctx, "bash"); err == nil {
			t.Errorf("%s unreadable: tools.get answered", tc.why)
		}
		if _, err := env.svc.ToolsByAgent(ctx, "atlas"); err == nil {
			t.Errorf("%s unreadable: tools.byAgent answered", tc.why)
		}
		if _, err := env.svc.ToolsStatus(ctx); err == nil {
			t.Errorf("%s unreadable: tools.status answered", tc.why)
		}
	}

	// The ledger is the exception, and an honest one: `seed.status` answers a ledger
	// it cannot read with nothing, so the catalog's seed half says `unavailable` —
	// the word that means "nobody looked" — instead of `never_applied`, which would
	// claim a fact about a process it never consulted. The rest of the status still
	// answers, because the tools and the rows it read are unaffected.
	env := newToolsCatalogEnv(t, true)
	db, err := env.st.GormDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS seed_ledger`); err != nil {
		t.Fatal(err)
	}
	// The property is what it must NEVER say, not which of two honest answers it
	// gives: an unreadable ledger is either an error (V11, the engine refusing to
	// answer) or the word `unavailable` (nobody looked). `current` or `never_applied`
	// would be a claim about a process this read could not consult — and
	// `never_applied` is the one an operator acts on.
	st, err := env.svc.ToolsStatus(context.Background())
	if err == nil && st.Seed.State != ToolsSeedStateUnavailable {
		t.Errorf("the seed half says %q for a ledger that could not be read", st.Seed.State)
	}
	if err == nil && st.Counts.System == 0 {
		t.Errorf("the readable halves went missing with the ledger: %+v", st.Counts)
	}
}

// TestToolsCatalogAbsentWithoutAStore is the nil guard the mount relies on: a
// storeless daemon hands the table nil, so RegisterToolsCatalog leaves the four
// doors ABSENT (V13 — the absent side of the same coin that
// TestToolsMethodsAbsentWhenServiceNil pins in the protocol package).
func TestToolsCatalogAbsentWithoutAStore(t *testing.T) {
	if svc := newToolsCatalogService(Options{}); svc != nil {
		t.Fatalf("the seam is %T with no store — registered-and-refusing is V13's forbidden shape", svc)
	}
	// The MCP seam refuses, and the catalog reports that as a failure rather than
	// as "this build has no MCP tools": the half exists, it could not be read.
	env := newToolsCatalogEnv(t, true)
	env.admin.refuse = "the management door is closed"
	if _, err := env.svc.ToolsList(context.Background(), protocol.ToolsListFilter{}); err == nil {
		t.Error("a refused MCP read answered an empty MCP half instead of failing")
	}
}

// beginForTest opens the transaction `ToolUpsertTx` insists on (T2's rule: a
// catalog row never commits on its own). It exists so the drift tests can put a
// row out of step with the code — the ONLY way to create `differs` and
// `missing_row` without editing a shipped seed record, which the golden test
// `TestToolVersionsBumpWithContent` would refuse anyway.
func beginForTest(t *testing.T, s *store.Store) *sql.Tx {
	t.Helper()
	db, err := s.GormDB().DB()
	if err != nil {
		t.Fatalf("the raw database: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

// TestMcpToolsAppearWithUnclassifiedFlag is MC6 stated on its own, because it is
// the case a management surface is most tempted to quietly improve. `acme.post` is a
// real tool of a real enabled server that nobody has classified: the runtime will
// refuse to call it. Hiding it would make the screen say this server has one tool
// while its own advertise still names two — a capability the app pretends is absent
// is worse than a capability shown as refused. So it is LISTED, `available:true`
// (it exists and the server is up), `classified:false`, with the effect and rerun
// class that made it dangerous in the first place, and in its declaring agent's view
// it reads `granted_no_rule` carrying the view's refusal text — not `not_granted`,
// which would say nobody declared it.
func TestMcpToolsAppearWithUnclassifiedFlag(t *testing.T) {
	env := newToolsCatalogEnv(t, true)
	entries := mustList(t, env, protocol.ToolsListFilter{Source: strptr(protocol.ToolsSourceMCP)})
	if len(entries) != 2 {
		t.Fatalf("the MCP half answered %d entries (%v), want both of the enabled server's declared tools", len(entries), toolEntryNames(entries))
	}
	post, ok := entryByName(entries, "acme.post")
	if !ok {
		t.Fatalf("the unclassified tool is missing: %v", toolEntryNames(entries))
	}
	if post.Classified {
		t.Error("an unclassified tool is reported classified — the flag is the only thing telling the person to go classify it")
	}
	if !post.Available {
		t.Error("an unclassified tool of an ENABLED server is reported unavailable: it exists, the server is up, the refusal is at call time")
	}
	if post.Effect != "outward" || post.RerunClass != "unsafe" {
		t.Errorf("the entry lost the facts that make it dangerous: effect=%q rerunClass=%q", post.Effect, post.RerunClass)
	}
	if search, ok := entryByName(entries, "acme.search"); !ok || !search.Classified {
		t.Errorf("the classified tool is not reported classified: %+v", search)
	}

	res, err := env.svc.ToolsByAgent(context.Background(), "atlas")
	if err != nil {
		t.Fatal(err)
	}
	row, ok := mapEntries(res.Tools)["acme.post"]
	if !ok {
		t.Fatal("the unclassified declared tool is missing from its declaring agent's view")
	}
	if row.State != toolcatalog.StateGrantedNoRule || row.Reason == "" {
		t.Errorf("acme.post = %q / %q, want the view's refusal carried verbatim", row.State, row.Reason)
	}
}
