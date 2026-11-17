package main

// mcp_proposals_test.go — S46 T6: the MCP proposal kinds move on the TABLES.
// S35 built the proposal machine around a file-era effect; T6 re-points the
// three MCP kinds (add / remove / revoke) at T5's audited row doors, so
// every assertion here is about rows: the row a commit WRITES, the audit it
// STAMPS with the committing person, and — TD2 — the fact that a proposal
// sitting in the table changes NOTHING an agent can be offered.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/store"
)

// stageProposal writes a row straight through the store (the create door
// is exercised separately, on purpose: TD2 needs proposals that EXIST in
// every state to prove they still decide nothing).
func stageProposal(t *testing.T, st *store.Store, kind, payload string) int64 {
	t.Helper()
	row, err := st.CreateProposal(kind, payload, store.Actor{Kind: "agent", ID: "manny"}, "2026-11-01T12:00:00Z")
	if err != nil {
		t.Fatalf("stage %s: %v", kind, err)
	}
	return row.ID
}

func commitProposal(t *testing.T, st *store.Store, id int64, payload string) (desk.CommitResult, error) {
	t.Helper()
	return desk.Commit(context.Background(), cliDecideDeps(st), deskEffects, id,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: "u1"})
}

func proposalRowOf(t *testing.T, st *store.Store, id int64) store.Proposal {
	t.Helper()
	row, found, err := st.GetProposal(id)
	if err != nil || !found {
		t.Fatalf("proposal %d: %v (found %v)", id, err, found)
	}
	return row
}

func addPayload(name string) string {
	b, _ := json.Marshal(mcpServerAddPayload{
		Tier: "public",
		Server: mcp.ServerConfig{
			Name:      name,
			Transport: mcp.TransportConfig{Type: "stdio", Command: name + "-mcp", Args: []string{"-y"}, Env: []string{"GITHUB_TOKEN"}, EnvFrom: map[string]string{"GITHUB_TOKEN": "GITHUB_TOKEN"}},
			Tools:     []mcp.ToolConfig{{Name: "do_thing", Rerun: "safe", Effect: "local"}},
		},
	})
	return string(b)
}

func revokePayload(agent, qualified string) string {
	b, _ := json.Marshal(mcpGrantRevokePayload{Agent: agent, Qualified: qualified, Reason: "not needed"})
	return string(b)
}

func removePayload(name string, cascade bool) string {
	b, _ := json.Marshal(mcpServerRemovePayload{Name: name, CascadeDeclares: cascade})
	return string(b)
}

// declareViaDoor plants a declaration through the audited door, so the
// rows under test are ones production itself wrote.
func declareViaDoor(t *testing.T, st *store.Store, agent, qualified string) {
	t.Helper()
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.grant.declare", map[string]any{"agent": agent, "qualified": qualified}); err != nil || refusal != "" {
		t.Fatalf("declare %s for %s: %v %s", qualified, agent, err, refusal)
	}
}

func auditRowOf(t *testing.T, st *store.Store, op string) (int, string, string, string) {
	t.Helper()
	var n int
	var kind, id, summary string
	row := st.GormDB().Raw(`SELECT COUNT(*), COALESCE(MAX(actor_kind),''), COALESCE(MAX(actor_id),''), COALESCE(MAX(summary),'') FROM mcp_change WHERE op = ?`, op).Row()
	if err := row.Scan(&n, &kind, &id, &summary); err != nil {
		t.Fatalf("audit %s: %v", op, err)
	}
	return n, kind, id, summary
}

// --- the commit writes ROWS, audited as the person who committed ---------

func TestMcpServerAddProposalCommitWritesRows(t *testing.T) {
	_, st := writeFixture(t)

	payload := addPayload("github")
	id := stageProposal(t, st, "mcp_server_add", payload)
	res, err := commitProposal(t, st, id, payload)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !res.EffectApplied {
		t.Error("the effect landed; EffectApplied must say so")
	}

	all, aerr := st.McpRows(context.Background())
	if aerr != nil {
		t.Fatal(aerr)
	}
	var row *store.McpServerRow
	for i := range all.Servers {
		if all.Servers[i].Name == "github" {
			row = &all.Servers[i]
		}
	}
	if row == nil {
		t.Fatal("the committed effect wrote no server row")
	}
	if row.Tier != "public" || row.TransportType != "stdio" || row.Command != "github-mcp" {
		t.Fatalf("row drifted from the payload: %+v", row)
	}
	if row.Origin != "user" {
		t.Errorf("a proposal commit is a HUMAN act; origin must be user, got %q", row.Origin)
	}
	if !row.Enabled {
		t.Error("MC3: presence = enabled")
	}
	if row.Args != `["-y"]` || row.Env != `["GITHUB_TOKEN"]` {
		t.Errorf("MG13: env NAMES only, args verbatim — got args %s env %s", row.Args, row.Env)
	}
	if len(row.Tools) != 1 || row.Tools[0].Name != "do_thing" || row.Tools[0].Rerun != "safe" || row.Tools[0].Effect != "local" {
		t.Fatalf("tool rows not written: %+v", row.Tools)
	}

	n, kind, who, summary := auditRowOf(t, st, "server_add")
	if n != 1 || kind != "user" || who != "u1" {
		t.Fatalf("audit: %d rows actor %s/%s — the COMMITTING person (u1) must be the actor, not the proposer", n, kind, who)
	}
	if !strings.Contains(summary, "github") {
		t.Errorf("the audit names the thing it did: %q", summary)
	}
}

// --- remove: refused while declarations name it, at BOTH doors ------------

func TestMcpServerRemoveProposalRefusesWhileDeclared(t *testing.T) {
	_, st := writeFixture(t)
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.server.add", addParams("gitforge", "public")); err != nil || refusal != "" {
		t.Fatalf("plant server: %v %s", err, refusal)
	}
	declareViaDoor(t, st, "manny", "gitforge.do_thing")

	payload := removePayload("gitforge", false)

	// The create door (the socket's proposal.create runs this exact
	// function): refused — and naming why.
	if err := validateProposalPayload(context.Background(), "mcp_server_remove", payload); err == nil {
		t.Fatal("creating a remove over a declared server must be refused")
	} else if !strings.Contains(err.Error(), "gitforge") {
		t.Fatalf("the refusal must name the server: %v", err)
	}

	// The commit door re-runs the SAME validator (TD3): the row stays
	// proposed with the refusal recorded; the world is untouched.
	id := stageProposal(t, st, "mcp_server_remove", payload)
	if _, err := commitProposal(t, st, id, payload); err == nil {
		t.Fatal("commit must refuse while declarations name the server")
	}
	row := proposalRowOf(t, st, id)
	if row.Status != "proposed" {
		t.Errorf("a refused re-validation leaves the row proposed, got %s", row.Status)
	}
	if row.AppliedError == "" {
		t.Error("the refusal is recorded in applied_error (S35 #5)")
	}
	if err := gitforgePresent(st); err != nil {
		t.Errorf("nothing was applied: the planted server row is missing: %v", err)
	}

	// With the cascade declared in the payload, the same row's SECOND
	// commit lands — the proposal MEANS the revokes.
	cascade := removePayload("gitforge", true)
	cid := stageProposal(t, st, "mcp_server_remove", cascade)
	if _, err := commitProposal(t, st, cid, cascade); err != nil {
		t.Fatalf("cascade commit: %v", err)
	}
	if err := gitforgePresent(st); err == nil {
		t.Error("the server row must be gone")
	}
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_declare`); n != 0 {
		t.Errorf("the cascade revoked the declarations; %d remain", n)
	}
}

func gitforgePresent(st *store.Store) error {
	all, err := st.McpRows(context.Background())
	if err != nil {
		return err
	}
	for _, s := range all.Servers {
		if s.Name == "gitforge" {
			return nil
		}
	}
	return errors.New("no gitforge row")
}

// --- revoke: the narrowing kind ------------------------------------------------

func TestGrantRevokeProposalCreateRefusesUndeclared(t *testing.T) {
	_, st := writeFixture(t)
	_ = st
	if err := validateProposalPayload(context.Background(), "mcp_grant_revoke",
		revokePayload("manny", "gitforge.never")); err == nil {
		t.Fatal("a revoke of something never declared is refused at create (there is nothing to narrow)")
	}
	// And by NAME — both the agent and the qualified are named in the payload.
	if err := validateProposalPayload(context.Background(), "mcp_grant_revoke", `{"agent":"","qualified":"gitforge.do_thing"}`); err == nil {
		t.Fatal("an empty agent is refused, not resolved to a default")
	}
}

func TestGrantRevokeCommitRevokesAndShowsBeforeAfter(t *testing.T) {
	_, st := writeFixture(t)
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.server.add", addParams("gitforge", "public")); err != nil || refusal != "" {
		t.Fatalf("plant server: %v %s", err, refusal)
	}
	declareViaDoor(t, st, "manny", "gitforge.do_thing")

	// BEFORE: the declaration exists and is offered.
	if !strings.Contains(callGrants(t, st, "manny"), "gitforge.do_thing") {
		t.Fatal("before-state: the declared tool must be in the grant view")
	}

	payload := revokePayload("manny", "gitforge.do_thing")
	id := stageProposal(t, st, "mcp_grant_revoke", payload)
	res, err := commitProposal(t, st, id, payload)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !res.EffectApplied {
		t.Error("the revoke landed")
	}

	// AFTER: the row is gone and the view lost the tool.
	if n := countQuery(t, st, `SELECT COUNT(*) FROM mcp_declare WHERE qualified = 'gitforge.do_thing'`); n != 0 {
		t.Errorf("the declaration row survived the revoke: %d", n)
	}
	if strings.Contains(callGrants(t, st, "manny"), "gitforge.do_thing") {
		t.Error("after-state: the grant view must no longer name the revoked tool")
	}

	n, kind, who, summary := auditRowOf(t, st, "grant_revoke")
	if n != 1 || kind != "user" || who != "u1" {
		t.Fatalf("the revoke audit: %d rows actor %s/%s — the committer owns it", n, kind, who)
	}
	if !strings.Contains(summary, "gitforge.do_thing") {
		t.Errorf("the audit summary names the revoked qualified: %q", summary)
	}
}

func TestGrantRevokeAlreadyAppliedCompletesFlip(t *testing.T) {
	_, st := writeFixture(t)
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.server.add", addParams("gitforge", "public")); err != nil || refusal != "" {
		t.Fatalf("plant server: %v %s", err, refusal)
	}
	declareViaDoor(t, st, "manny", "gitforge.do_thing")

	// The crash window TD4 exists for: the effect landed, the flip never
	// completed. The row is still proposed while the world says revoked
	// (the revoke ran; the proposal row never flipped).
	payload := revokePayload("manny", "gitforge.do_thing")
	if err := st.GormDB().Exec(`DELETE FROM mcp_declare WHERE qualified = 'gitforge.do_thing'`).Error; err != nil {
		t.Fatal(err)
	}
	id := stageProposal(t, st, "mcp_grant_revoke", payload)

	// Re-running commit COMPLETES the flip without reapplying: no error,
	// the row flips, and no second audit row appears.
	res, err := commitProposal(t, st, id, payload)
	if err != nil {
		t.Fatalf("the AlreadyApplied gate must complete the lost flip: %v", err)
	}
	if res.EffectApplied {
		t.Error("nothing was reapplied — EffectApplied must be false (TD4)")
	}
	row := proposalRowOf(t, st, id)
	if row.Status != "committed" {
		t.Errorf("the flip completed: %s", row.Status)
	}
	if n := auditCount(t, st, "grant_revoke"); n != 0 {
		t.Errorf("an already-applied commit writes no second audit: %d rows", n)
	}
}

// --- TD2: a proposal is a row, not a permission --------------------------

func TestMcpProposalsAreInert(t *testing.T) {
	_, st := writeFixture(t)
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.server.add", addParams("gitforge", "public")); err != nil || refusal != "" {
		t.Fatalf("plant server: %v %s", err, refusal)
	}
	declareViaDoor(t, st, "manny", "gitforge.do_thing")

	before := struct{ servers, grants, changes string }{
		servers: dumpJSON(t, st, call, "mcp.servers", map[string]any{}),
		grants:  callGrants(t, st, "manny"),
		changes: dumpJSON(t, st, call, "mcp.changes", map[string]any{"limit": 100}),
	}

	// One proposal of every MCP kind, staged and LEFT proposed.
	stageProposal(t, st, "mcp_server_add", addPayload("another"))
	stageProposal(t, st, "mcp_server_remove", removePayload("gitforge", true))
	stageProposal(t, st, "mcp_grant_revoke", revokePayload("manny", "gitforge.do_thing"))

	after := struct{ servers, grants, changes string }{
		servers: dumpJSON(t, st, call, "mcp.servers", map[string]any{}),
		grants:  callGrants(t, st, "manny"),
		changes: dumpJSON(t, st, call, "mcp.changes", map[string]any{"limit": 100}),
	}
	if before != after {
		t.Fatalf("proposals moved the world — TD2 broken:\n servers %d/%d bytes\n grants %d/%d bytes\n changes %d/%d bytes",
			len(before.servers), len(after.servers), len(before.grants), len(after.grants),
			len(before.changes), len(after.changes))
	}
}

func dumpJSON(t *testing.T, st *store.Store, call func(string, any) (any, string, error), method string, params any) string {
	t.Helper()
	res, refusal, err := call(method, params)
	if err != nil || refusal != "" {
		t.Fatalf("%s: %v %s", method, err, refusal)
	}
	b, merr := json.Marshal(res)
	if merr != nil {
		t.Fatal(merr)
	}
	return string(b)
}

// --- TD3: one validator, both doors ---------------------------------------

func TestCreateAndCommitValidatorsAreOneFunction(t *testing.T) {
	_, st := writeFixture(t)
	call := writeCallOn(st)
	if _, refusal, err := call("mcp.server.add", addParams("gitforge", "public")); err != nil || refusal != "" {
		t.Fatalf("plant server: %v %s", err, refusal)
	}

	cases := []struct{ kind, payload string }{
		{"mcp_grant_revoke", revokePayload("manny", "gitforge.nope")},                                                // never declared
		{"mcp_server_remove", `{"name":"gitforge"}`},                                                                 // declared elsewhere? no declares: valid — use a bad shape below
		{"mcp_server_add", `{"tier":"governance","server":{"name":"x","transport":{"type":"stdio","command":"x"}}}`}, // bad tier
	}
	for _, c := range cases {
		if c.kind == "mcp_server_remove" {
			c.payload = removePayload("", false) // no name: both doors must say the same sentence
		}
		create := validateProposalPayload(context.Background(), c.kind, c.payload)
		commit := deskEffects.ValidateProposal(context.Background(), c.kind, c.payload)
		if create == nil || commit == nil {
			t.Fatalf("%s: expected a refusal, got create=%v commit=%v", c.kind, create, commit)
		}
		if create.Error() != commit.Error() {
			t.Errorf("%s: the two doors grew two validators:\n create: %v\n commit: %v", c.kind, create, commit)
		}
	}
}

func TestMcpPayloadWithUnresolvedAgentRefusedByName(t *testing.T) {
	writeFixture(t) // the three real agents exist; "nobody-at-all" does not
	if err := validateProposalPayload(context.Background(), "mcp_grant_revoke",
		revokePayload("nobody-at-all", "gitforge.do_thing")); err == nil || !strings.Contains(err.Error(), "nobody-at-all") {
		t.Fatalf("an unresolvable agent is refused BY NAME: %v", err)
	}
	// A private add naming a ghost agent is the same refusal, not a
	// fabricated row (T5's law, now at the create door too).
	ghost, _ := json.Marshal(mcpServerAddPayload{
		Tier:   "private",
		Agent:  "nobody-at-all",
		Server: mcp.ServerConfig{Name: "s", Transport: mcp.TransportConfig{Type: "stdio", Command: "s-mcp"}},
	})
	if err := validateProposalPayload(context.Background(), "mcp_server_add", string(ghost)); err == nil || !strings.Contains(err.Error(), "nobody-at-all") {
		t.Fatalf("a ghost agent in a private add is refused BY NAME: %v", err)
	}
}

// --- the import law: the desk never learns the mcp package ----------------

func TestDeskImportsNoMcpPackage(t *testing.T) {
	// desk holds the Kind CONSTANT (a string) — that is all the desk may
	// ever know about MCP. Effects live in cmd/aeon (PM4's one registry,
	// defined beside the doors), never in the desk.
	matches, gerr := filepath.Glob(filepath.Join("..", "..", "internal", "desk", "*.go"))
	if gerr != nil || len(matches) == 0 {
		t.Fatalf("glob internal/desk: %v (%d files)", gerr, len(matches))
	}
	// Every file, not a named list — S56 added desk files beside ours, and
	// the law (the desk may know a KIND STRING, never the config grammar)
	// binds the whole package, present and future.
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue // test files may import anything; the SHIP code is the law
		}
		name := filepath.Base(path)
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read internal/desk/%s: %v", name, rerr)
		}
		src := string(raw)
		if strings.Contains(src, `"github.com/cjlapao/aeon/internal/mcp"`) {
			t.Errorf("internal/desk/%s imports internal/mcp — the desk would then need the whole config grammar to name a kind", name)
		}
	}
}
