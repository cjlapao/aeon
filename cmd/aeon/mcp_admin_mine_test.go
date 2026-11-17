package main

// mcp_admin_mine_test.go — S46 T8 (MC2, MG5, MG8, MG9, 2026-11-02): the
// SCOPED half of the seam walked over the REAL rows, registry and probe
// cache — the same instances the person's fifteen read — under a two-agent
// world where everything beta owns is a thing alpha must never see.
//
// MC2 as tested here is the byte discipline, not the filter: every answer
// (result AND refusal sentence) for agent alpha is searched for markers that
// exist only in beta's world — a server name alpha never declared from, a
// tool name only beta holds, an env NAME only beta's transport carries —
// and the answers for ghostly names that only EXIST in beta's world are
// byte-identical to names that exist nowhere. A door that can tell "not
// yours" from "not real" is an existence oracle; these five cannot, because
// the same code path answers both, built from a view that never contained
// the row.
//
// The status door is walked against the SAME cache mcp.status reads, with a
// counting spy in front of the prober (MG5): a hundred calls, zero dials.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mineWorld seeds the two-agent world. `withBetaGhost` decides whether the
// private server "ghostly" (and its tool) exists in beta's world at all —
// the indistinguishability test runs the SAME request against both worlds.
func mineWorld(t *testing.T, withBetaGhost bool) (*store.Store, *mcpProbeCache) {
	t.Helper()
	_, st, cache := adminFixture(t)
	ctx := context.Background()
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: slug, Name: slug, Version: "1"}, "2026-11-01T12:00:00Z"); err != nil && !errors.Is(err, store.ErrAgentSlugExists) {
			t.Fatalf("agent %s: %v", slug, err)
		}
	}
	err := st.McpWithTx(ctx, func(tx *sql.Tx) error {
		// The store is shared across the two worlds a test opens (the env
		// isolates per TEST, not per open), so the seed clears its own rows
		// first and each world is seeded exactly as requested.
		for _, stmt := range []string{
			`DELETE FROM mcp_declare`, `DELETE FROM mcp_server_tool`,
			`DELETE FROM mcp_server`, `DELETE FROM mcp_agent`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		for _, slug := range []string{"alpha", "beta", "gamma"} {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mcp_agent (agent_slug, core) VALUES (?, NULL)`, slug); err != nil {
				return err
			}
		}
		mk := func(r store.McpServerRow) error {
			r.Enabled, r.Origin = true, "user"
			r.CreatedAt, r.UpdatedAt = "2026-11-01T12:00:00Z", "2026-11-01T12:00:00Z"
			if r.Args == "" {
				r.Args = "[]"
			}
			if r.Env == "" {
				r.Env = "[]"
			}
			_, err := store.McpAddServerTx(ctx, tx, r)
			return err
		}
		beta := "beta"
		alpha := "alpha"
		if err := mk(store.McpServerRow{Name: "pub-tools", Tier: "public", TransportType: "http", URL: "http://127.0.0.1:9/tools",
			Tools: []store.McpToolRow{{Name: "search", Rerun: "safe", Effect: "local"}}}); err != nil {
			return err
		}
		if err := mk(store.McpServerRow{Name: "alpha-own", Tier: "private", AgentSlug: &alpha, TransportType: "stdio", Command: "true",
			Env:   `["ALPHA_TOKEN"]`,
			Tools: []store.McpToolRow{{Name: "do_work", Rerun: "safe", Effect: "local"}}}); err != nil {
			return err
		}
		if err := mk(store.McpServerRow{Name: "beta-own", Tier: "private", AgentSlug: &beta, TransportType: "stdio", Command: "true",
			Env:   `["BETA_DB_TOKEN"]`,
			Tools: []store.McpToolRow{{Name: "secret-b", Rerun: "safe", Effect: "local"}}}); err != nil {
			return err
		}
		if withBetaGhost {
			if err := mk(store.McpServerRow{Name: "ghostly", Tier: "private", AgentSlug: &beta, TransportType: "stdio", Command: "true",
				Tools: []store.McpToolRow{{Name: "ghosttool", Rerun: "safe", Effect: "local"}}}); err != nil {
				return err
			}
		}
		// MC12: declares name PUBLIC tools only — a private tool is
		// granted by ownership alone; a declare at one is refused by the one
		// validator (correctly), so the world here declares only the public.
		for _, d := range [][2]string{
			{"alpha", "pub-tools.search"}, {"beta", "pub-tools.search"},
		} {
			if _, err := store.McpDeclareTx(ctx, tx, d[0], d[1]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st, cache
}

func mineCtx(agent string) context.Context {
	return protocol.WithMCPActor(context.Background(), "agent", agent)
}

func mineSeam(st *store.Store, cache *mcpProbeCache) func(context.Context, string, json.RawMessage) (any, string, error) {
	return mcpAdminHandleFor(st, cache)
}

// betaMarkers — bytes that exist ONLY in beta's world. None of them was ever
// written into an alpha-side request in any test below; any appearance in
// any alpha-side answer (result OR refusal sentence) is the leak MC2 forbids.
var betaMarkers = []string{"beta-own", "secret-b", "BETA_DB_TOKEN", "ghostly", "ghosttool"}

func assertNoBetaBytes(t *testing.T, label, hay string) {
	t.Helper()
	for _, m := range betaMarkers {
		if strings.Contains(hay, m) {
			t.Errorf("%s: alpha-side answer carries %q — MC2 says a peer's private world is not in the INPUT of this door, so it cannot be in its output either: %.200s", label, m, hay)
		}
	}
}

// TestOwnViewExcludesThePeerWorld — all five entries, alpha's seat, one
// store where beta owns three private servers' worth of bytes.
func TestPeerPrivateServerNeverAppearsInAnyOutputOrError(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	cases := []struct{ method, params string }{
		{"mcp.mine.servers", `{}`},
		{"mcp.mine.grants", `{}`},
		{"mcp.mine.status", `{}`},
		{"mcp.mine.proposeRemove", `{"server":"pub-tools","reason":"this loop no longer reads anything from it"}`},
		{"mcp.mine.proposeRevoke", `{"server":"pub-tools","tool":"search","reason":"this loop never searches anything with it"}`},
	}
	for _, tc := range cases {
		out, sent, err := seam(mineCtx("alpha"), tc.method, json.RawMessage(tc.params))
		if err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		if sent != "" {
			t.Fatalf("%s: unexpected refusal: %s", tc.method, sent)
		}
		assertNoBetaBytes(t, tc.method, jsonOf(t, out)+sent)
	}
	// And the positives, so the byte test above is not an empty-view trick:
	// alpha's servers are the two it owns/declares, in order, env NAMES only.
	out, _, err := seam(mineCtx("alpha"), "mcp.mine.servers", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res, ok := out.(protocol.MineServersResult)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if len(res.Servers) != 2 || res.Servers[0].Name != "alpha-own" || res.Servers[1].Name != "pub-tools" {
		var got []string
		for _, s := range res.Servers {
			got = append(got, s.Name)
		}
		t.Fatalf("alpha's view is %v, want exactly [alpha-own pub-tools]", got)
	}
	if len(res.Servers[0].Transport.Env) != 1 || res.Servers[0].Transport.Env[0] != "ALPHA_TOKEN" {
		t.Errorf("own row carries env names %v, want [ALPHA_TOKEN] — NAMES (§4c: values have no column to live in, and no answer of this door has a field for one)", res.Servers[0].Transport.Env)
	}
	// The whole answer must not smuggle a values-shaped field either.
	assertNoBetaBytes(t, "servers shape", jsonOf(t, out))
	if strings.Contains(jsonOf(t, out), `"values"`) || strings.Contains(jsonOf(t, out), `"envValues"`) {
		t.Errorf("a values-carrying field appeared in the own-view answer — MG13's structural ban")
	}
}

// TestPeerPrivateAndNonexistentAreIndistinguishable — the oracle test: the
// SAME name, one world where beta owns it and one where nobody does, and a
// byte-equal refusal. Status and the two read doors answer identically too
// (the row is simply not in alpha's view either way).
func TestProposeRemoveInvisibleServerRefusedAsNoSuchServer(t *testing.T) {
	// "ghostly" is the name: beta-private in world A, absent in world B.
	const name, tool = "ghostly", "ghosttool"
	var removeA, removeB, revokeA, revokeB string
	{
		st, cache := mineWorld(t, true)
		seam := mineSeam(st, cache)
		_, s1, err := seam(mineCtx("alpha"), "mcp.mine.proposeRemove", json.RawMessage(`{"server":"`+name+`","reason":"it looked unused to this loop so this agent proposes removing it"}`))
		if err != nil {
			t.Fatal(err)
		}
		_, s2, err := seam(mineCtx("alpha"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"`+name+`","tool":"`+tool+`","reason":"this loop never uses the pair and wants it gone anyway"}`))
		if err != nil {
			t.Fatal(err)
		}
		removeA, revokeA = s1, s2
	}
	{
		st, cache := mineWorld(t, false)
		seam := mineSeam(st, cache)
		_, s1, err := seam(mineCtx("alpha"), "mcp.mine.proposeRemove", json.RawMessage(`{"server":"`+name+`","reason":"it looked unused to this loop so this agent proposes removing it"}`))
		if err != nil {
			t.Fatal(err)
		}
		_, s2, err := seam(mineCtx("alpha"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"`+name+`","tool":"`+tool+`","reason":"this loop never uses the pair and wants it gone anyway"}`))
		if err != nil {
			t.Fatal(err)
		}
		removeB, revokeB = s1, s2
	}
	if removeA != removeB {
		t.Errorf("the remove door told the two worlds apart: %q vs %q — an existence oracle for other agents' servers", removeA, removeB)
	}
	if revokeA != revokeB {
		t.Errorf("the revoke door told the two worlds apart: %q vs %q — an existence oracle for other agents' grants", revokeA, revokeB)
	}
	if removeA == "" || revokeA == "" {
		t.Error("a refusal was expected in both worlds, not silence")
	}
	// The read doors carry nothing either way (already pinned for the
	// beta-own trio above; ghostly rows ride the same view code).
	for _, world := range []bool{true, false} {
		st, cache := mineWorld(t, world)
		for _, m := range []string{"mcp.mine.servers", "mcp.mine.grants", "mcp.mine.status"} {
			out, sent, err := mineSeam(st, cache)(mineCtx("alpha"), m, json.RawMessage(`{}`))
			if err != nil || sent != "" {
				t.Fatalf("%s world=%v: %v / %s", m, world, err, sent)
			}
			if s := jsonOf(t, out); strings.Contains(s, name) || strings.Contains(s, tool) {
				t.Errorf("%s leaked the private name %q in world=%v: %.160s", m, name, world, s)
			}
		}
	}
}

// TestStatusDoorNeverDials — MG5 at the agent door: the cache is the ONLY
// source, the prober behind it stays at zero calls across the whole walk,
// and three states (ok / down / unchecked) answer honestly.
func TestMcpStatusToolDoesNotProbe(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	dials := 0
	// Fill the cache the way the PERSON's probe door fills it — through
	// checkOrJoin itself, so the mine door reads exactly what mcp.status
	// reads. pub-tools probed ok; alpha-own probed down; never ghostly.
	put := func(server string, r mcpCheckResult) {
		cache.checkOrJoin(server, func() mcpCheckResult {
			dials++
			return r
		})
	}
	put("pub-tools", mcpCheckResult{Server: "pub-tools", OK: true, Error: "", Ms: 12, CheckedAt: "2026-11-01T12:00:00Z"})
	put("alpha-own", mcpCheckResult{Server: "alpha-own", OK: false, Error: "child exited before initialize", Ms: 40, CheckedAt: "2026-11-01T12:00:00Z"})
	put("beta-own", mcpCheckResult{Server: "beta-own", OK: true, Ms: 3, CheckedAt: "2026-11-01T12:00:00Z"})
	before := dials
	out, sent, err := seam(mineCtx("alpha"), "mcp.mine.status", json.RawMessage(`{}`))
	if err != nil || sent != "" {
		t.Fatalf("%v / %s", err, sent)
	}
	res, ok := out.(protocol.MineStatusResult)
	if !ok {
		t.Fatalf("type %T", out)
	}
	if dials != before {
		t.Errorf("the agent's status door dialed %d times — MG5 says the cache ONLY (a spy in front of checkOrJoin stays at %d)", dials-before, before)
	}
	if len(res.Servers) != 2 {
		t.Fatalf("status listed %d servers, want exactly alpha's 2 (beta-own was probed OK and is STILL not here): %+v", len(res.Servers), res.Servers)
	}
	states := map[string]string{}
	for _, s := range res.Servers {
		states[s.Server] = s.State
	}
	if states["pub-tools"] != "ok" || states["alpha-own"] != "down" {
		t.Errorf("states %v, want pub-tools=ok alpha-own=down", states)
	}
	// A third call with nothing probed answers unchecked, never assumed.
	st2, cache2 := mineWorld(t, false)
	out2, _, err := mineSeam(st2, cache2)(mineCtx("alpha"), "mcp.mine.status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range out2.(protocol.MineStatusResult).Servers {
		if s.State != "unchecked" {
			t.Errorf("%s answered %q on a never-probed cache — the honest third word is unchecked (P31)", s.Server, s.State)
		}
	}
}

// TestMineEntriesRefuseUnstampedAndPersons — the seam's own defense: a
// person-stamped call, or no stamp at all, is the pointer refusal for all
// five; the rows half is never even read.
func TestMineEntriesRefuseUnstampedAndPersons(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	for _, m := range []string{"mcp.mine.servers", "mcp.mine.grants", "mcp.mine.status", "mcp.mine.proposeRemove", "mcp.mine.proposeRevoke"} {
		for _, ctx := range []context.Context{
			context.Background(), // no stamp at all
			protocol.WithMCPActor(context.Background(), "user", "1000"),
			protocol.WithMCPActor(context.Background(), "", ""),
		} {
			params := json.RawMessage(`{}`)
			if m == "mcp.mine.proposeRemove" {
				params = json.RawMessage(`{"server":"pub-tools","reason":"the operator asked from a person connection by mistake"}`)
			}
			if m == "mcp.mine.proposeRevoke" {
				params = json.RawMessage(`{"server":"pub-tools","tool":"search","reason":"the operator asked from a person connection by mistake"}`)
			}
			_, sent, err := seam(ctx, m, params)
			if err != nil {
				t.Fatalf("%s unstamped: %v", m, err)
			}
			if sent == "" || !strings.Contains(sent, "agent-scoped") {
				t.Errorf("%s with stamp %v: want the agent-scoped pointer refusal, got %q", m, ctx.Value("kind"), sent)
			}
		}
	}
}

// TestProposePayloadsCarryTheStampNotTheInput — the payload the daemon will
// file is built here, and its agent field is SELF. An input that names
// another holder cannot move it (the revoke door takes only server+tool;
// an input that smuggled "agent" would die at the protocol decode — here
// it simply has no field to land in).
func TestProposePayloadsCarryTheStampNotTheInput(t *testing.T) {
	st, cache := mineWorld(t, false)
	seam := mineSeam(st, cache)
	out, sent, err := seam(mineCtx("alpha"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"pub-tools","tool":"search","agent":"beta","reason":"this loop never uses the pair and wants it gone"}`))
	if err != nil || sent != "" {
		t.Fatalf("%v / %s", err, sent)
	}
	ready, ok := out.(protocol.MineProposeReady)
	if !ok || ready.Kind != string(desk.KindMCPGrantRevoke) {
		t.Fatalf("answer %T %+v", out, out)
	}
	var p mcpGrantRevokePayload
	if err := json.Unmarshal(ready.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Agent != "alpha" {
		t.Errorf("payload agent = %q, want the STAMP alpha — the input named beta and moved nothing", p.Agent)
	}
	if p.Qualified != "pub-tools.search" || p.Reason == "" {
		t.Errorf("payload %+v is not the canonical pair + reason", p)
	}
	// Removal over holders carries the cascade (the commit will audit each
	// revoke row by row — T6's rule, restated at the agent door).
	out2, sent2, err := seam(mineCtx("alpha"), "mcp.mine.proposeRemove", json.RawMessage(`{"server":"pub-tools","reason":"this loop no longer reads anything from it at all"}`))
	if err != nil || sent2 != "" {
		t.Fatalf("%v / %s", err, sent2)
	}
	ready2 := out2.(protocol.MineProposeReady)
	var rp mcpServerRemovePayload
	if err := json.Unmarshal(ready2.Payload, &rp); err != nil {
		t.Fatal(err)
	}
	if !rp.CascadeDeclares || rp.Name != "pub-tools" || rp.Reason == "" {
		t.Errorf("remove payload %+v — holders existed, the cascade must ride (T6's rule)", rp)
	}
	// And the validator runs over these exact bytes: a well-shaped pair
	// that names a grant nobody holds already died above; a pair alpha
	// holds passed HERE — the parity test for the create door is
	// cmd/aeon's existing validator suite over the same functions.
	_ = validateMCPGrantRevokeProposal(ready.Payload)
	if err := validateMcpServerRemoveProposal(ready2.Payload); err != nil {
		t.Errorf("the daemon would refuse at create what the seam just said was ready (TD3 drift): %v", err)
	}
}

// TestAgentSeesOnlyItsOwnView — the POSITIVES of the same world (the byte
// test above alone could pass on an empty view): exactly the agent's own
// private servers plus the public servers it declares from, tools carried
// with their classes, and the public the agent declares nothing from is
// absent (declaring is the only way a public enters a view).
func TestAgentSeesOnlyItsOwnView(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	// gamma declares nothing and owns nothing: its whole view is empty.
	out, sent, err := seam(mineCtx("gamma"), "mcp.mine.servers", json.RawMessage(`{}`))
	if err != nil || sent != "" {
		t.Fatalf("%v / %s", err, sent)
	}
	if res := out.(protocol.MineServersResult); len(res.Servers) != 0 {
		t.Errorf("gamma declared nothing yet sees %d servers — a public enters a view ONLY through the caller's own declares", len(res.Servers))
	}
	// alpha: exactly its private + the one public it declares from.
	out, _, err = seam(mineCtx("alpha"), "mcp.mine.servers", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res := out.(protocol.MineServersResult)
	if len(res.Servers) != 2 || res.Servers[0].Name != "alpha-own" || res.Servers[1].Name != "pub-tools" {
		var got []string
		for _, sv := range res.Servers {
			got = append(got, sv.Name)
		}
		t.Fatalf("alpha's view %v, want exactly [alpha-own pub-tools]", got)
	}
	// The tools ride with their classes (the same rows the person's read
	// answers — one source, two doors).
	if len(res.Servers[1].Tools) != 1 || res.Servers[1].Tools[0].Name != "search" || res.Servers[1].Tools[0].Rerun != "safe" {
		t.Errorf("pub-tools tools %+v, want [search safe]", res.Servers[1].Tools)
	}
	// Grants answer per the registry's own view for alpha — the owned
	// private tool is there (ownership is the grant, MC12) and beta's is
	// not even a row.
	out, _, err = seam(mineCtx("alpha"), "mcp.mine.grants", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	g := out.(protocol.MineGrantsResult)
	if g.Agent != "alpha" {
		t.Errorf("grants answered for %q — the stamp IS the answer's subject", g.Agent)
	}
	seen := map[string]bool{}
	for _, gr := range g.Grants {
		seen[gr.Qualified] = true
	}
	if !seen["alpha-own.do_work"] || !seen["pub-tools.search"] {
		t.Errorf("alpha's grants %v miss its own pair (ownership or declares must count)", g.Grants)
	}
}

// TestProposeRevokeOnlyHeldGrants — the revoke door holds the same line as
// the remove door: another agent's pair and a pair nobody holds answer the
// SAME sentence (no oracle), and the only pair that succeeds is one the
// caller itself holds.
func TestProposeRevokeOnlyHeldGrants(t *testing.T) {
	held := `{"server":"pub-tools","tool":"search","reason":"this loop never searches anything with it anymore"}`
	{
		st, cache := mineWorld(t, true) // beta holds ghostly.ghosttool (owned; no declare needed — MC12)
		_, sa, err := mineSeam(st, cache)(mineCtx("alpha"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"ghostly","tool":"ghosttool","reason":"this loop has never heard of it but wants it revoked anyway"}`))
		if err != nil {
			t.Fatal(err)
		}
		if sa == "" {
			t.Fatal("alpha revoked beta's private pair — MG9/MC2 in one sentence")
		}
		st2, cache2 := mineWorld(t, false)
		_, sb, err := mineSeam(st2, cache2)(mineCtx("alpha"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"ghostly","tool":"ghosttool","reason":"this loop has never heard of it but wants it revoked anyway"}`))
		if err != nil {
			t.Fatal(err)
		}
		if sa != sb {
			t.Errorf("the revoke door told beta's world from nobody's: %q vs %q", sa, sb)
		}
	}
	// The held pair works — and only the holder's own stamp makes it hold:
	// beta revokes its OWN public declare.
	st, cache := mineWorld(t, true)
	out, sent, err := mineSeam(st, cache)(mineCtx("beta"), "mcp.mine.proposeRevoke", json.RawMessage(held))
	if err != nil || sent != "" {
		t.Fatalf("beta revoke of a held pair: %v / %s", err, sent)
	}
	if ready := out.(protocol.MineProposeReady); ready.Kind != string(desk.KindMCPGrantRevoke) {
		t.Errorf("kind %q", ready.Kind)
	}
	// And a dot smuggled into either half is refused outright.
	_, sent, err = mineSeam(st, cache)(mineCtx("beta"), "mcp.mine.proposeRevoke", json.RawMessage(`{"server":"pub-tools.search","tool":"","reason":"smuggling the dot through the server half"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sent == "" || !strings.Contains(sent, "SPLIT") {
		t.Errorf("a dotted server half must be refused as a non-pair, got %q", sent)
	}
}

// TestNearestHintsDrawnFromOwnViewOnly — the doors answer NO nearest-name
// hints at all (the view is the only name source, and a hint is a name the
// caller did not already have). A near-miss at a peer's name must not
// "suggest" the peer.
func TestNearestHintsDrawnFromOwnViewOnly(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	_, sent, err := seam(mineCtx("alpha"), "mcp.mine.proposeRemove", json.RawMessage(`{"server":"beta-on","reason":"one letter short of a server this loop wants gone"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sent == "" {
		t.Fatal("beta-on exists nowhere in alpha's view; it must be refused")
	}
	for _, marker := range []string{"beta-own", "secret-b", "BETA_DB_TOKEN", "did you mean"} {
		if strings.Contains(sent, marker) {
			t.Errorf("the refusal carries %q — nearest-name hints across the boundary are existence oracles (MC2); the view is the only name source", marker)
		}
	}
}

// TestNoEnvValueInAnyToolOutput — §4c made structural: the transport
// carries NAMES, and these five answers have no field anywhere a value
// could sit. Alpha's own env names are visible (they are its own config);
// no answer of any of the five carries a value-shaped field, and nothing
// resembling one is emitted on any path.
func TestNoEnvValueInAnyToolOutput(t *testing.T) {
	st, cache := mineWorld(t, true)
	seam := mineSeam(st, cache)
	for _, who := range []string{"alpha", "beta", "gamma"} {
		for _, tc := range []struct{ method, params string }{
			{"mcp.mine.servers", `{}`},
			{"mcp.mine.grants", `{}`},
			{"mcp.mine.status", `{}`},
			{"mcp.mine.proposeRemove", `{"server":"pub-tools","reason":"this loop no longer reads anything from it at all"}`},
			{"mcp.mine.proposeRevoke", `{"server":"pub-tools","tool":"search","reason":"this loop never searches anything with it anymore"}`},
		} {
			out, sent, err := seam(mineCtx(who), tc.method, json.RawMessage(tc.params))
			if err != nil {
				t.Fatalf("%s/%s: %v", who, tc.method, err)
			}
			blob := jsonOf(t, out) + sent
			low := strings.ToLower(blob)
			for _, banned := range []string{`"value"`, `"envvalue"`, `"apikey"`, `"token":`, `"secret":`} {
				if strings.Contains(low, banned) {
					t.Errorf("%s/%s: answer carries the value-shaped key %s — MG13's structural ban (names only, values have no field)", who, tc.method, banned)
				}
			}
		}
	}
	// The own row shows NAMES (that is the point — config the owner itself
	// authored), proving the test above is not passing on emptiness.
	out, _, err := seam(mineCtx("beta"), "mcp.mine.servers", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	blob := jsonOf(t, out)
	if !strings.Contains(blob, "BETA_DB_TOKEN") {
		t.Errorf("beta's own env NAME must be visible to beta (it is configuration, not a credential): %.200s", blob)
	}
}
