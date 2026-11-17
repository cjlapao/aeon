package main

// mcp_import_test.go — S46 T4: MG3's adoption is once, all-or-nothing,
// never silent, and the file is untouchable. The twelve tests are the
// task's table; the loudest among them are the two the law exists for:
// TestAdoptInvalidFileRefusesAndStartsLevelZero (a broken file must not
// become a secretly-empty config) and TestAdoptUnresolvableAgentRefusesWholeImport
// (no half a world, ever).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// importBody: public github (2 tools, env NAMES), private labnote under
// golang-engineer, declarations across two agents, core omitted (the whole
// core set — the nil the rows must store as NULL).
const importBody = `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp","args":["--x"],"env":["GITHUB_TOKEN"]},"tools":[{"name":"list_issues","rerun":"safe","effect":"local"},{"name":"create_issue","rerun":"unsafe","effect":"outward"}]}],"agents":{"golang-engineer":{"private":[{"name":"labnote","transport":{"type":"http","url":"http://127.0.0.1:9/mcp"},"tools":[{"name":"draft_note","rerun":"safe","effect":"local"}]}],"declares":["github.list_issues"]},"manny":{"declares":["github.create_issue"]}}}`

// importHome: a per-test state dir with the agents the file names already
// existing (an import NEVER fabricates agents, so the happy path plants
// them the way the seeder plants — directly, which test files may).
func importHome(t *testing.T) *store.Store {
	t.Helper()
	isolateStoreEnv(t) // the leaked-DATABASE-keys pin (policy_golden_test.go:179):
	// a full package run boots daemons in-process and appenv.Apply leaves
	// DATABASE_SQLITE_STORAGE_PATH behind, where it OUTRANKS XDG — an
	// adoption test without this reads a neighbour's world.
	agentHome(t)
	st := openAgentStore(t)
	now := "2026-11-01T12:00:00Z"
	for _, name := range []string{"golang-engineer", "manny"} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: store.Slugify(name), Name: name, Version: "1"}, now); err != nil {
			t.Fatalf("plant agent %s: %v", name, err)
		}
	}
	return st
}

func writeLegacyFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func importActorCtx() context.Context {
	return protocol.WithMCPActor(context.Background(), "user", "1000")
}

func mustJSONRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// countQuery runs one scalar query through the store's OWN Tx door — the
// I2 exemption's home for test-side SQL (test files write and read rows
// directly; the production code never does).
func countQuery(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), query, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

func ledgerCount(t *testing.T, st *store.Store) int {
	t.Helper()
	return countQuery(t, st, `SELECT COUNT(*) FROM mcp_import`)
}

func changeCount(t *testing.T, st *store.Store, op string) int {
	t.Helper()
	return countQuery(t, st, `SELECT COUNT(*) FROM mcp_change WHERE op = ?`, op)
}

func configRowCount(t *testing.T, st *store.Store) (int, int, int) {
	t.Helper()
	var s, a, d int
	err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(),
			`SELECT (SELECT COUNT(*) FROM mcp_server), (SELECT COUNT(*) FROM mcp_agent), (SELECT COUNT(*) FROM mcp_declare)`).
			Scan(&s, &a, &d)
	})
	if err != nil {
		t.Fatalf("count the config: %v", err)
	}
	return s, a, d
}

func fileIno(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}

func TestAdoptValidFileOnce(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	ctx := context.Background()

	res, err := adoptLegacy(ctx, st, path)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if res.Outcome != importImported || res.Servers != 2 {
		t.Fatalf("outcome = %+v, want imported / 2 servers", res)
	}

	// The rows equal Disassemble(file) — the whole config, name by name,
	// including origin='import' and enabled.
	want := mcp.Disassemble(mcpConfigFromJSON(t, importBody))
	all, err := st.McpRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Servers) != len(want.Servers) {
		t.Fatalf("servers = %d, want %d", len(all.Servers), len(want.Servers))
	}
	gotByName := map[string]store.McpServerRow{}
	for _, r := range all.Servers {
		gotByName[r.Name] = r
		if r.Origin != "import" || !r.Enabled {
			t.Errorf("server %s: origin=%q enabled=%v, want import/true", r.Name, r.Origin, r.Enabled)
		}
	}
	for _, w := range want.Servers {
		g, ok := gotByName[w.Name]
		if !ok {
			t.Fatalf("server %s missing from the rows", w.Name)
		}
		if g.Tier != string(w.Tier) || g.TransportType != string(w.Transport.Type) ||
			g.Command != w.Transport.Command || g.URL != w.Transport.URL {
			t.Errorf("server %s shape: %+v", w.Name, g)
		}
		if g.Name == "github" {
			if g.Args != `["--x"]` {
				t.Errorf("github args = %s, want [\"--x\"]", g.Args)
			}
			if g.Env != `["GITHUB_TOKEN"]` {
				t.Errorf("github env = %s, want the NAME array", g.Env)
			}
		}
		var wantSlug *string
		if w.Tier == mcp.TierPrivate {
			slug := store.Slugify(w.AgentKey)
			wantSlug = &slug
		}
		if (wantSlug == nil) != (g.AgentSlug == nil) || (wantSlug != nil && *wantSlug != *g.AgentSlug) {
			t.Errorf("server %s agent = %v, want %v", w.Name, g.AgentSlug, wantSlug)
		}
		if len(g.Tools) != len(w.Tools) {
			t.Errorf("server %s tools = %v, want %v", w.Name, g.Tools, w.Tools)
		}
	}
	s, _, d := configRowCount(t, st)
	if s != 2 || d != 2 {
		t.Fatalf("config rows = %d servers / %d declares, want 2 / 2 (private tools are owned, never declared — MC12)", s, d)
	}

	// The ledger remembers ONE imported row for the hash; the audit one
	// row per server plus the one declarations row, all system:startup.
	raw, _ := os.ReadFile(path)
	sum := sha256.Sum256(raw)
	if led, found, err := st.McpImportByHash(ctx, hex.EncodeToString(sum[:])); err != nil || !found {
		t.Fatalf("the ledger holds no imported row: found=%v err=%v", found, err)
	} else if led.Result != "imported" || led.Servers != 2 {
		t.Fatalf("ledger row = %+v", led)
	}
	if n := changeCount(t, st, "import"); n != 3 {
		t.Errorf("audit import rows = %d, want 3 (two servers + one declarations)", n)
	}
	if n := ledgerCount(t, st); n != 1 {
		t.Errorf("ledger rows = %d, want 1", n)
	}
	var kind, id string
	if err := st.McpWithTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT actor_kind, actor_id FROM mcp_change WHERE op = 'import' LIMIT 1`).Scan(&kind, &id)
	}); err != nil {
		t.Fatal(err)
	}
	if kind != "system" || id != "startup" {
		t.Errorf("audit actor = %s/%s, want system/startup at boot", kind, id)
	}

	// Second start: the SAME bytes are already-imported — nothing is
	// written again (no double adoption).
	before := changeCount(t, st, "import")
	res2, err := adoptLegacy(ctx, st, path)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Outcome != importAlready {
		t.Fatalf("second adopt = %q, want already-imported", res2.Outcome)
	}
	if after := changeCount(t, st, "import"); after != before {
		t.Errorf("the second adopt wrote %d audit rows — adoption is once", after-before)
	}
	if n := ledgerCount(t, st); n != 1 {
		t.Errorf("the second adopt appended to the ledger: %d rows", n)
	}
}

func TestAdoptIsIdempotentByHash(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	ctx := context.Background()
	if res, err := adoptLegacy(ctx, st, path); err != nil || res.Outcome != importImported {
		t.Fatalf("first adopt = %+v, %v", res, err)
	}
	// The hash check runs BEFORE the non-empty check (the task's order):
	// even with a populated world, THIS file's bytes answer
	// already-imported, not skipped — and nothing at all is written.
	res, err := adoptLegacy(ctx, st, path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != importAlready {
		t.Fatalf("restart over the same bytes = %q, want already-imported (the hash speaks first)", res.Outcome)
	}
	if n := ledgerCount(t, st); n != 1 {
		t.Fatalf("ledger churned to %d rows", n)
	}
}

func TestAdoptInvalidFileRefusesAndStartsLevelZero(t *testing.T) {
	// The credential half: env entries are NAMES, and a value-shaped entry
	// refuses the WHOLE file (behaviour note 3 — no credential ever
	// lands). It must not become a silently-empty config either.
	st := importHome(t)
	body := `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp","env":["GITHUB_TOKEN=ghp_leakedvalue"]}}]}`
	path := writeLegacyFile(t, body)
	t.Setenv("AEON_MCP_CONFIG", path)
	ctx := context.Background()

	res, err := adoptLegacy(ctx, st, path)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if res.Outcome != importRefused {
		t.Fatalf("outcome = %q, want refused", res.Outcome)
	}
	if !strings.Contains(res.Detail, path) {
		t.Errorf("the refusal sentence must carry the file path: %q", res.Detail)
	}
	s, a, d := configRowCount(t, st)
	if s != 0 || a != 0 || d != 0 {
		t.Fatalf("a refused file still wrote config rows: %d/%d/%d", s, a, d)
	}
	if n := ledgerCount(t, st); n != 1 {
		t.Fatalf("ledger rows = %d, want the ONE refused row", n)
	}
	var result string
	if err := st.McpWithTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT result FROM mcp_import LIMIT 1`).Scan(&result)
	}); err != nil {
		t.Fatal(err)
	}
	if result != "refused" {
		t.Fatalf("ledger result = %q, want refused", result)
	}
	// LEVEL-0, loudly: the reads answer the empty-tables words, not a
	// pretend config.
	if _, err := loadMCPConfig(); !errors.Is(err, mcp.ErrNoConfig) {
		t.Errorf("loadMCPConfig after refusal = %v, want ErrNoConfig (level-0)", err)
	}
	var w bytes.Buffer
	mcpDoctorLines(&w)
	if !strings.Contains(w.String(), "refused at") {
		t.Errorf("doctor must repeat the refusal from the ledger, got: %s", w.String())
	}
}

func TestAdoptUnresolvableAgentRefusesWholeImport(t *testing.T) {
	st := importHome(t) // golang-engineer and manny exist; ghosts do not
	body := `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh-mcp"},"tools":[{"name":"list_issues","rerun":"safe","effect":"local"}]}],"agents":{"golang-engineer":{"declares":["github.list_issues"]},"ghost-a":{"declares":[]},"ghost-b":{"private":[{"name":"secret","transport":{"type":"stdio","command":"s"}}]}}}`
	path := writeLegacyFile(t, body)
	ctx := context.Background()

	res, err := adoptLegacy(ctx, st, path)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if res.Outcome != importRefused {
		t.Fatalf("outcome = %q, want refused", res.Outcome)
	}
	for _, name := range []string{"ghost-a", "ghost-b"} {
		if !strings.Contains(res.Detail, name) {
			t.Errorf("the refusal must name every unresolved agent, missing %q: %q", name, res.Detail)
		}
	}
	// ZERO rows — not even the valid github server, not even the valid
	// agent's declaration (S43 T2's all-or-nothing, carried).
	s, a, d := configRowCount(t, st)
	if s != 0 || a != 0 || d != 0 {
		t.Fatalf("partial import: %d/%d/%d rows", s, a, d)
	}
	if n := ledgerCount(t, st); n != 1 {
		t.Fatalf("ledger rows = %d, want the one refused row", n)
	}
}

func TestAdoptSkipsWhenTablesNonEmpty(t *testing.T) {
	isolateStoreEnv(t)
	agentHome(t) // rows AND the ledger are per-test state (the TestMain
	// dir is shared, and MG3's hash law is per-database)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}]}`))
	st := openAgentStore(t)
	for _, name := range []string{"golang-engineer", "manny"} {
		_ = st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: store.Slugify(name), Name: name, Version: "1"}, "2026-11-01T12:00:00Z")
	}
	path := writeLegacyFile(t, importBody)
	res, err := adoptLegacy(context.Background(), st, path)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != importSkipped {
		t.Fatalf("outcome = %q, want skipped — the tables are the truth and a file never merges into them unasked", res.Outcome)
	}
	if !strings.Contains(res.Detail, "already has MCP configuration") {
		t.Errorf("the skip must say why: %q", res.Detail)
	}
	all, err := st.McpRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Servers) != 1 || all.Servers[0].Name != "gitforge" {
		t.Errorf("the seeded world was merged into: %+v", all.Servers)
	}
	if n := ledgerCount(t, st); n != 0 {
		t.Errorf("a skipped adoption wrote %d ledger rows — nothing happened, the ledger must agree", n)
	}
}

func TestAdoptNeverTouchesTheFile(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	rawBefore, _ := os.ReadFile(path)
	if _, err := adoptLegacy(context.Background(), st, path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("mtime moved: %v → %v", before.ModTime(), after.ModTime())
	}
	if before.Mode() != after.Mode() {
		t.Errorf("mode changed: %v → %v", before.Mode(), after.Mode())
	}
	if fileIno(before) != fileIno(after) {
		t.Errorf("inode changed — a rename-and-replace is a write wearing a disguise")
	}
	if rawAfter, _ := os.ReadFile(path); !bytes.Equal(rawBefore, rawAfter) {
		t.Error("the bytes moved — adoption reads, it never rewrites")
	}
}

func TestBehaviourIdenticalBeforeAndAfterAdoption(t *testing.T) {
	// The T3 path (rows seeded, golden-verified) and the T4 path (file
	// adopted) must answer the SAME status — adoption changing behaviour
	// is the failure this test exists for.
	body := importBody

	// Two worlds, same facts, same test — the rows arrive by the T3
	// seeder first, then (after a wipe, so adoption is legal) by the T4
	// importer. The ONLY difference is provenance; the answers must not
	// differ by a byte.
	st := importHome(t)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, body))
	sa, refusal, err := mcpHandleFor(nil, newMCPProbeCache())(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("status seed door: %v %s", err, refusal)
	}

	// Wipe back to empty (the seeder's world-replacement does it), then
	// adopt the same facts from a file.
	seedMCPConfigRows(t, mcp.Config{})
	pathB := writeLegacyFile(t, body)
	if res, err := adoptLegacy(context.Background(), st, pathB); err != nil || res.Outcome != importImported {
		t.Fatalf("adopt door B: %+v %v", res, err)
	}
	sb, refusal, err := mcpHandleFor(nil, newMCPProbeCache())(context.Background(), "mcp.status", nil)
	if err != nil || refusal != "" {
		t.Fatalf("status adopt door: %v %s", err, refusal)
	}

	ra, rb := sa.(mcpStatusResult), sb.(mcpStatusResult)
	// configPath names where each world's legacy file would live — two
	// worlds, two paths; that difference is the point of the field, not a
	// behavioural difference. Every other fact must be identical.
	ra.ConfigPath, rb.ConfigPath = "", ""
	ja, jb := marshalForCompare(ra), marshalForCompare(rb)
	if ja != jb {
		t.Errorf("the two doors rendered different truths for the same config:\n seeded: %s\nadopted: %s", ja, jb)
	}
	if len(rb.Servers) != 2 {
		t.Fatalf("servers = %d, want 2 (github public + labnote private)", len(rb.Servers))
	}
}

func marshalForCompare(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "«unmarshalable»"
	}
	return string(b)
}

func TestDoctorReportsDriftAfterEdit(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	t.Setenv("AEON_MCP_CONFIG", path)
	ctx := context.Background()
	if res, err := adoptLegacy(ctx, st, path); err != nil || res.Outcome != importImported {
		t.Fatalf("adopt: %+v %v", res, err)
	}
	var w bytes.Buffer
	mcpDoctorLines(&w)
	if !strings.Contains(w.String(), "database authoritative (2 servers)") {
		t.Fatalf("doctor after a clean adoption: %s", w.String())
	}
	// Hand-edit the file AFTER adoption: reported as drift, never
	// re-imported (the tables stay authoritative until asked — only
	// mcp.importFile replace re-imports).
	edited := strings.Replace(importBody, `"command":"gh-mcp"`, `"command":"gh-mcp","args":["--extra"]`, 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	var w2 bytes.Buffer
	mcpDoctorLines(&w2)
	if !strings.Contains(w2.String(), "differs from what was imported") {
		t.Errorf("the edit must report drift, got: %s", w2.String())
	}
	if strings.Contains(w2.String(), "--extra") {
		t.Errorf("doctor re-imported content it should only compare: %s", w2.String())
	}
	s, _, d := configRowCount(t, st)
	if s != 2 || d != 2 {
		t.Errorf("the drift report changed the world: %d servers, %d declares", s, d)
	}
}

func TestImportFileDryRunWritesNothingAndShowsDiff(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	res, refusal, err := seam(importActorCtx(), "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "dry-run", "path": path}))
	if err != nil {
		t.Fatalf("seam err: %v", err)
	}
	if refusal != "" {
		t.Fatalf("refused: %s", refusal)
	}
	out := res.(protocol.McpImportFileResult)
	if out.Outcome != importDryRun {
		t.Fatalf("outcome = %q", out.Outcome)
	}
	if out.Diff == nil {
		t.Fatal("the dry run must show the diff")
	}
	if !contains(out.Diff.ServersAdded, "github") || !contains(out.Diff.ServersAdded, "labnote") {
		t.Errorf("serversAdded = %v, want the file's servers", out.Diff.ServersAdded)
	}
	if !contains(out.Diff.ToolsAdded, "github.list_issues") || !contains(out.Diff.ToolsAdded, "labnote.draft_note") {
		t.Errorf("toolsAdded = %v", out.Diff.ToolsAdded)
	}
	if !contains(out.Diff.DeclaresAdded, "golang-engineer declares github.list_issues") ||
		!contains(out.Diff.DeclaresAdded, "manny declares github.create_issue") {
		t.Errorf("declaresAdded = %v", out.Diff.DeclaresAdded)
	}
	if contains(out.Diff.DeclaresAdded, "golang-engineer declares labnote.draft_note") {
		t.Errorf("a private tool was declared — MC12 would refuse the file, the diff must never plan the impossible")
	}
	if !contains(out.Diff.EnvNames, "GITHUB_TOKEN") {
		t.Errorf("envNames = %v, want the NAME", out.Diff.EnvNames)
	}
	// A plan is not a fact: NOTHING was written — not config, not audit,
	// not even a ledger row.
	s, _, d := configRowCount(t, st)
	if s != 0 || d != 0 {
		t.Errorf("the dry run wrote config: %d servers / %d declares", s, d)
	}
	if n := ledgerCount(t, st); n != 0 {
		t.Errorf("the dry run wrote %d ledger rows", n)
	}
	if n := changeCount(t, st, "import"); n != 0 {
		t.Errorf("the dry run wrote %d audit rows", n)
	}
}

func TestImportFileReplaceIsAllOrNothing(t *testing.T) {
	isolateStoreEnv(t)
	agentHome(t) // same: the replace writes config, audit AND ledger — all
	// three must live in this test's own database (the shared TestMain dir
	// is why a second -count pass saw a stale hash and a stale roster)
	seedMCPConfigRows(t, mcpConfigFromJSON(t, `{"public":[{"name":"gitforge","transport":{"type":"stdio","command":"gf"},"tools":[{"name":"pr","rerun":"safe","effect":"local"}]}],"agents":{"manny":{"declares":["gitforge.pr"]}}}`))
	st := openAgentStore(t)
	// golang-engineer and manny must exist for the good replace (the seed
	// above planted manny; an import never fabricates the other).
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: store.Slugify("golang-engineer"), Name: "golang-engineer", Version: "1"}, "2026-11-01T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	ctx := importActorCtx()

	// (a) An unresolvable agent: refused, the OLD world stands, nothing
	// half-replaced (wipe and insert are one transaction or none).
	bad := writeLegacyFile(t, `{"public":[{"name":"github","transport":{"type":"stdio","command":"gh"}}],"agents":{"ghost":{"declares":[]}}}`)
	res, refusal, err := seam(ctx, "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "replace", "path": bad}))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if refusal != "" {
		t.Fatalf("refused: %s", refusal)
	}
	if out := res.(protocol.McpImportFileResult); out.Outcome != importRefused {
		t.Fatalf("bad replace outcome = %q, want refused", out.Outcome)
	}
	all, _ := st.McpRows(context.Background())
	if len(all.Servers) != 1 || all.Servers[0].Name != "gitforge" {
		t.Fatalf("the refused replace tore the old world: %+v", all.Servers)
	}

	// (b) The valid replace: the whole world swaps, atomically, with the
	// audit and the before/after grants for every agent it touched.
	good := writeLegacyFile(t, importBody)
	res, refusal, err = seam(ctx, "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "replace", "path": good}))
	if err != nil || refusal != "" {
		t.Fatalf("good replace: %v %s", err, refusal)
	}
	out := res.(protocol.McpImportFileResult)
	if out.Outcome != importReplaced || out.Servers != 2 || out.Declares != 2 {
		t.Fatalf("replace result = %+v", out)
	}
	if len(out.Affected) == 0 {
		t.Fatal("the replace must show before/after grants for every agent it touched")
	}
	var touched []string
	for _, af := range out.Affected {
		touched = append(touched, af.Agent)
	}
	sort.Strings(touched)
	if strings.Join(touched, ",") != "golang-engineer,manny" {
		t.Errorf("affected = %v, want both agents (gitforge.pr fell away for manny; new declares arrived for golang-engineer)", touched)
	}
	for _, af := range out.Affected {
		if af.Agent == "manny" && (len(af.Before) != 1 || af.Before[0] != "gitforge.pr") {
			t.Errorf("manny before = %v, want [gitforge.pr]", af.Before)
		}
		if af.Agent == "manny" && !contains(af.After, "github.create_issue") {
			t.Errorf("manny after = %v, want the new declare", af.After)
		}
	}
	all, _ = st.McpRows(context.Background())
	names := []string{}
	for _, r := range all.Servers {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "github,labnote" {
		t.Errorf("the world after replace = %v, want github,labnote (gitforge is gone — replace, never merge)", names)
	}
	if n := changeCount(t, st, "import"); n != 3 {
		t.Errorf("audit import rows = %d, want 3", n)
	}
}

func TestImportFileActorFromPeer(t *testing.T) {
	st := importHome(t)
	path := writeLegacyFile(t, importBody)
	seam := mcpAdminHandleFor(st, newMCPProbeCache())

	// No actor stamp on the context ⇒ ErrMcpNoActor — the door refuses an
	// unattributable write (params carrying an actor is the protocol
	// layer's shape refusal, pinned in internal/protocol).
	_, _, err := seam(context.Background(), "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "replace", "path": path}))
	var pe *protocol.Error
	if !errors.As(err, &pe) || pe.Code != protocol.ErrMcpNoActor.Code {
		t.Fatalf("no-actor err = %v, want ErrMcpNoActor (%d)", err, protocol.ErrMcpNoActor.Code)
	}
	s, _, d := configRowCount(t, st)
	if s != 0 || d != 0 {
		t.Fatalf("the refused write touched rows: %d servers / %d declares", s, d)
	}
	// With the stamp (dispatch's job on the socket; the direct door's job
	// elsewhere): the write lands and the audit names the person.
	if _, refusal, err := seam(importActorCtx(), "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "replace", "path": path})); err != nil || refusal != "" {
		t.Fatalf("stamped write: %v %s", err, refusal)
	}
	var kind, id string
	if err := st.McpWithTx(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT actor_kind, actor_id FROM mcp_change WHERE op = 'import' LIMIT 1`).Scan(&kind, &id)
	}); err != nil {
		t.Fatal(err)
	}
	if kind != "user" || id != "1000" {
		t.Errorf("audit actor = %s/%s, want the stamped person user/1000", kind, id)
	}
}

func TestImportFilePathMustBeAbsoluteRegular(t *testing.T) {
	st := importHome(t)
	seam := mcpAdminHandleFor(st, newMCPProbeCache())
	ctx := importActorCtx()
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link-to-dir")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("no symlink privilege: %v", err)
	}
	cases := []struct {
		name string
		path string
	}{
		{"relative", "mcp.json"},
		{"directory", dir},
		{"symlink-to-directory", link},
		{"missing", filepath.Join(dir, "absent.json")},
	}
	for _, tc := range cases {
		_, _, err := seam(ctx, "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": "replace", "path": tc.path}))
		var bad protocol.ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want the params shape refusal (ErrBadParams)", tc.name, err)
		}
	}
	// mode itself is not optional magic: only the two named acts.
	for _, mode := range []string{"", "yolo", "Replace", "dryrun"} {
		_, _, err := seam(ctx, "mcp.importFile", mustJSONRaw(t, map[string]any{"mode": mode, "path": writeLegacyFile(t, importBody)}))
		var bad protocol.ErrBadParams
		if !errors.As(err, &bad) {
			t.Errorf("mode %q: err = %v, want the params shape refusal", mode, err)
		}
	}
	// Nothing was written by any of the refusals.
	s, _, d := configRowCount(t, st)
	if s != 0 || d != 0 {
		t.Fatalf("refused doors wrote rows: %d/%d", s, d)
	}
}
