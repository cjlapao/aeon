package daemon

// records_mcp_test.go — S46 T8 (MG8, MG9, MC2, 2026-11-02): the five MCP
// catalog arms walked at the door an agent actually stands at, on the
// autonomy arms' precedent (S56 T8): the stamp, the forwarding, the person
// door, the inert create, and what a REFUSED propose never leaves behind.
//
// What this file pins, row by row:
//
//   - THE STAMP is the whole identity story (MC2's first half): each arm
//     enters the seam's scoped entry with the step's OWN agent on the
//     protocol.WithMCPActor channel and nothing else — the input that
//     smuggles an `agent` field dies at the strict decode, and an empty
//     slug (the step names no one) is refused rather than guessed (OL3);
//   - a PERSON session is turned back at every one of the five with its
//     pointer sentence and zero seam calls — the doors are agent-scoped in
//     both directions, and the pointer names the person's own door;
//   - unwired is STATED, not answered empty (V13): a mount without the MCP
//     seam refuses by name instead of telling the agent it has no servers
//     (an empty own-view is a confident wrong answer about an unwired
//     surface, P31);
//   - the proposes end in exactly ONE inert row through the ONE create path
//     — stamped author, the shared inert note, the copyable command — and a
//     seam that names any kind but the two narrowing ones is an
//     infrastructure fault that writes NOTHING (TD3, MG9's no-widening);
//   - the caps the arms inherit from that same path (the per-agent open
//     cap) fire at THIS door, not only at the policy twins'.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mcpAdminSpy is the test mirror of the seam's five scoped entries: it
// records what arrived (method, raw input, the ACTOR ON THE CHANNEL) and
// answers what the test loaded. The cmd/aeon half (visibility, the registry
// view, the identical-refusal discipline) is walked over the REAL entries in
// cmd/aeon's own suite; here the question is what the daemon's arms send and
// what they do with the answer.
type mcpAdminSpy struct {
	calls  []mcpSpyCall
	ready  any    // what the next call answers (nil ⇒ a fixed ok object)
	sent   string // what the next call answers as its refusal sentence
	err    error  // what the next call answers as infrastructure fault
	byCall []any  // optional per-call answers, consumed in order
}

type mcpSpyCall struct {
	Method string
	Input  string
	Kind   string
	ID     string
	HasAct bool
}

func (spy *mcpAdminSpy) handle(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	kind, id, ok := protocol.MCPActor(ctx)
	spy.calls = append(spy.calls, mcpSpyCall{Method: method, Input: string(params), Kind: kind, ID: id, HasAct: ok})
	if spy.err != nil {
		return nil, "", spy.err
	}
	if spy.sent != "" {
		return nil, spy.sent, nil
	}
	if len(spy.byCall) > 0 {
		out := spy.byCall[0]
		spy.byCall = spy.byCall[1:]
		return out, "", nil
	}
	if spy.ready != nil {
		return spy.ready, "", nil
	}
	return map[string]any{"seen": method}, "", nil
}

type mcpMineEnv struct {
	recordsEnv
	spy *mcpAdminSpy
	reg *desk.Registry
}

func newMCPMineEnv(t *testing.T) mcpMineEnv {
	t.Helper()
	e := mcpMineEnv{recordsEnv: newRecordsEnv(t, "")}
	spy := &mcpAdminSpy{}
	e.svc.mcpAdmin = spy.handle
	e.svc.proposals = proposalStore(e.s)
	// The test mirror of cmd/aeon's production seams for the two kinds:
	// desk's shape half is the registry's own; the membership halves read
	// the seeded rows (the same checks the production validators run —
	// cmd/aeon's parity test pins those against proposal.create).
	e.reg = desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, desk.Seams{
		desk.KindMCPServerRem:   {Validate: testMCPRemoveValidate(e.s)},
		desk.KindMCPGrantRevoke: {Validate: testMCPRevokeValidate(e.s)},
	})
	e.svc.validateProposal = e.reg.ValidateProposal
	e.spy = spy
	// Seed the rows the validators read: one public server with two tools
	// and one declared pair held by the env's own agent ("implementer").
	seedMCPRowsForDaemonTest(t, e.s)
	return e
}

// seedMCPRowsForDaemonTest writes through the store's own seam writers —
// exactly the writers T7's row-provenance guard names.
func seedMCPRowsForDaemonTest(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	// The agent rows the mcp foreign keys reference (the FK points at the
	// REAL agent registry — a fixture that invents slugs the agent table
	// does not know is a fixture that cannot exist in production).
	for _, slug := range []string{"implementer", "reviewer"} {
		if err := s.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: slug, Name: strings.Title(slug)}, "2026-11-01T12:00:00Z"); err != nil {
			t.Fatalf("agent %s: %v", slug, err)
		}
	}
	err := s.McpWithTx(ctx, func(tx *sql.Tx) error {
		// The mcp_agent rows the private server's and the declares' foreign
		// keys point at — written through the same raw-SQL path the store's
		// own writers take (the row-provenance guard counts PRODUCTION files,
		// and a test seeding rows is exactly what fixtures are for).
		for _, slug := range []string{"implementer", "reviewer"} {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_agent (agent_slug, core) VALUES (?, NULL)`, slug); err != nil {
				return err
			}
		}
		if _, err := store.McpAddServerTx(ctx, tx, store.McpServerRow{
			Name: "acme-crud", Tier: "public", TransportType: "http", URL: "https://mcp.acme.dev/tools",
			Args: "[]", Env: "[]", Enabled: true, Origin: "user", CreatedAt: "2026-11-01T12:00:00Z", UpdatedAt: "2026-11-01T12:00:00Z",
			Tools: []store.McpToolRow{
				{Name: "list_issues", Rerun: "safe", Effect: "local"},
				{Name: "create_issue", Rerun: "detectable", Effect: "outward"},
			},
		}); err != nil {
			return err
		}
		self := "implementer"
		if _, err := store.McpAddServerTx(ctx, tx, store.McpServerRow{
			Name: "reddit", Tier: "private", AgentSlug: &self, TransportType: "stdio",
			Command: "npx", Args: `["-y","@acme/mcp-reddit"]`, Env: `["REDDIT_TOKEN"]`,
			Enabled: true, Origin: "user", CreatedAt: "2026-11-01T12:00:00Z", UpdatedAt: "2026-11-01T12:00:00Z",
			Tools: []store.McpToolRow{{Name: "search", Rerun: "safe", Effect: "local"}},
		}); err != nil {
			return err
		}
		if _, err := store.McpDeclareTx(ctx, tx, "implementer", "acme-crud.list_issues"); err != nil {
			return err
		}
		if _, err := store.McpDeclareTx(ctx, tx, "reviewer", "acme-crud.create_issue"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func testMCPRemoveValidate(s *store.Store) func(context.Context, []byte) error {
	return func(_ context.Context, payload []byte) error {
		var p struct {
			Name            string `json:"name"`
			CascadeDeclares bool   `json:"cascadeDeclares"`
			Reason          string `json:"reason"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if p.Name == "" {
			return fmt.Errorf("refused: a removal without a name removes nothing")
		}
		all, err := s.McpRows(context.Background())
		if err != nil {
			return err
		}
		for _, srv := range all.Servers {
			if srv.Name == p.Name {
				return nil
			}
		}
		return fmt.Errorf("refused: no such server %q", p.Name)
	}
}

func testMCPRevokeValidate(s *store.Store) func(context.Context, []byte) error {
	return func(_ context.Context, payload []byte) error {
		var p struct {
			Agent     string `json:"agent"`
			Qualified string `json:"qualified"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if p.Agent == "" || p.Qualified == "" {
			return fmt.Errorf("refused: a revocation needs the holder and the pair")
		}
		all, err := s.McpRows(context.Background())
		if err != nil {
			return err
		}
		for _, d := range all.Declares {
			if d.AgentSlug == p.Agent && d.Qualified == p.Qualified {
				return nil
			}
		}
		return fmt.Errorf("refused: %s holds no grant %q", p.Agent, p.Qualified)
	}
}

// TestMineArmsForwardTheStamp — MC2's first half at the daemon door.
func TestMineArmsForwardTheStamp(t *testing.T) {
	cases := []struct {
		tool   string
		input  string
		method string
	}{
		{"mcp_servers", `{}`, "mcp.mine.servers"},
		{"mcp_grants", `{}`, "mcp.mine.grants"},
		{"mcp_status", `{}`, "mcp.mine.status"},
		{"mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`, "mcp.mine.proposeRemove"},
		{"mcp_propose_revoke_grant", `{"server":"acme-crud","tool":"list_issues","reason":"this loop never reads issues"}`, "mcp.mine.proposeRevoke"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			e := newMCPMineEnv(t)
			// The proposes need a ready answer from the seam; the reads
			// answer the spy's fixed object.
			e.spy.ready = nil
			if strings.HasPrefix(tc.tool, "mcp_propose") {
				kind := string(desk.KindMCPServerRem)
				payload := `{"name":"acme-crud","cascadeDeclares":false,"reason":"decommissioned; nothing reads it anymore"}`
				if tc.tool == "mcp_propose_revoke_grant" {
					kind = string(desk.KindMCPGrantRevoke)
					payload = `{"agent":"implementer","qualified":"acme-crud.list_issues","reason":"this loop never reads issues"}`
				}
				e.spy.ready = protocol.MineProposeReady{Kind: kind, Payload: json.RawMessage(payload)}
			}
			out, f, err := e.do(t, tc.tool, tc.input)
			if f != nil || err != nil {
				t.Fatalf("%s: got refusal %v / err %v", tc.tool, f, err)
			}
			if out == nil {
				t.Fatalf("%s: nil answer", tc.tool)
			}
			if len(e.spy.calls) != 1 {
				t.Fatalf("%s: seam calls = %d, want 1", tc.tool, len(e.spy.calls))
			}
			c := e.spy.calls[0]
			if c.Method != tc.method {
				t.Errorf("%s entered the seam as %q, want %q", tc.tool, c.Method, tc.method)
			}
			if !c.HasAct || c.Kind != "agent" || c.ID != "implementer" {
				t.Errorf("%s reached the seam with actor (%q,%q,ok=%v), want the stamp (agent,implementer,true) — identity is the CHANNEL, never a param", tc.tool, c.Kind, c.ID, c.HasAct)
			}
		})
	}
}

// TestSmuggledIdentityFieldsRefuseAtParse — OL3's parse wall: the schemas
// close their properties; a bag that NAMES an agent never reaches the seam.
func TestSmuggledIdentityFieldsRefuseAtParse(t *testing.T) {
	e := newMCPMineEnv(t)
	_, f, err := e.do(t, "mcp_servers", `{"agent":"reviewer"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatal("a params bag naming another agent was accepted — the view would have been this caller's while the request READ like someone else's")
	}
	if len(e.spy.calls) != 0 {
		t.Errorf("the seam saw a call that should have died at the decode: %+v", e.spy.calls)
	}
}

// TestPersonSessionsAreTurnedBackAtTheDoor — the pointer sentences; zero
// seam traffic either way.
func TestMineTwinsRefuseNonAgentSessions(t *testing.T) {
	e := newMCPMineEnv(t)
	for _, tc := range []struct{ tool, want string }{
		{"mcp_servers", "mcp.servers"},
		{"mcp_grants", "mcp.grants"},
		{"mcp_status", "mcp.status"},
		{"mcp_propose_remove_server", "mcp.server.remove"},
		{"mcp_propose_revoke_grant", "mcp.grant.revoke"},
	} {
		_, f, err := e.svc.Do(e.ctx, Actor{Kind: "user", ID: "1000"}, tc.tool, json.RawMessage(`{"server":"acme-crud","tool":"list_issues","reason":"because I am the operator and I said so firmly"}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.tool, err)
		}
		if f == nil {
			t.Fatalf("%s: a person answered the agent-scoped door", tc.tool)
		}
		if !strings.Contains(f.Sentence, "agent-scoped") || !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s: refusal %q must say agent-scoped AND name the person's own door %q", tc.tool, f.Sentence, tc.want)
		}
	}
	if len(e.spy.calls) != 0 {
		t.Errorf("the seam saw %d calls from person sessions; person traffic must never cross it", len(e.spy.calls))
	}
}

// TestArmsWithoutTheSeamSaySo — V13: refusal by name, not an empty view.
func TestArmsWithoutTheSeamSaySo(t *testing.T) {
	e := newMCPMineEnv(t)
	e.svc.mcpAdmin = nil
	for _, tool := range []string{"mcp_servers", "mcp_grants", "mcp_status", "mcp_propose_remove_server", "mcp_propose_revoke_grant"} {
		_, f, err := e.do(t, tool, `{"server":"acme-crud","tool":"list_issues","reason":"decommissioned; this loop has no use for it anymore"}`)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if f == nil || !strings.Contains(f.Sentence, "did not wire it") {
			t.Errorf("%s: unwired must refuse by name (V13), got %v", tool, f)
		}
	}
	rows, err := e.svc.proposals.ListProposals("proposed")
	if err != nil || len(rows) != 0 {
		t.Errorf("refused proposes wrote %d rows — TD3", len(rows))
	}
}

// TestEmptySlugRefuses — a step that stamps no agent gets the OL3 sentence,
// never a guess.
func TestEmptySlugRefuses(t *testing.T) {
	e := newMCPMineEnv(t)
	// The arm is called with the Identity the store would hand a step that
	// names no agent (CreateStep normalizes a blank step otherwise — the
	// broken stamp is what the arm must catch, so it is supplied directly).
	_, f, err := e.svc.mcpMineServers(context.Background(), Identity{StepID: 0, Agent: ""}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "never guessed") {
		t.Errorf("an empty slug must be refused as identity-never-guessed (OL3), got %v", f)
	}
	if len(e.spy.calls) != 0 {
		t.Errorf("the seam saw %d calls behind a broken stamp", len(e.spy.calls))
	}
}

// TestProposeCreatesOneInertStampedRow — MG9's landing: the ONLY row a
// propose leaves is the inert proposal, authored by the stamp, carrying the
// one shared inert note (not a second sentence about the family).
func TestProposalsCreatedAreInertAndByTheAgentActor(t *testing.T) {
	e := newMCPMineEnv(t)
	e.spy.ready = protocol.MineProposeReady{
		Kind:    string(desk.KindMCPServerRem),
		Payload: json.RawMessage(`{"name":"acme-crud","cascadeDeclares":true,"reason":"decommissioned; nothing reads it anymore"}`),
	}
	out, f, err := e.do(t, "mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`)
	if f != nil || err != nil {
		t.Fatalf("refusal/err: %v / %v", f, err)
	}
	res, ok := out.(protocol.PolicyProposeResult)
	if !ok {
		t.Fatalf("answer is %T, want the shared PolicyProposeResult — a second answer shape would be a second truth", out)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Errorf("note %q != the shared inert sentence", res.Note)
	}
	if res.Proposal.Kind != string(desk.KindMCPServerRem) || res.Proposal.Status != "proposed" {
		t.Errorf("row is (%s,%s), want (mcp_server_remove,proposed)", res.Proposal.Kind, res.Proposal.Status)
	}
	if res.Proposal.CreatedByKind != "agent" || res.Proposal.CreatedByID != "implementer" {
		t.Errorf("author is (%s,%s), want the STAMPED (agent,implementer)", res.Proposal.CreatedByKind, res.Proposal.CreatedByID)
	}
	if !strings.Contains(res.Proposal.Command, fmt.Sprintf("%d", res.Proposal.ID)) {
		t.Errorf("command %q is not the copyable commit line for row %d", res.Proposal.Command, res.Proposal.ID)
	}
	rows, _ := e.svc.proposals.ListProposals("proposed")
	if len(rows) != 1 {
		t.Errorf("%d rows, want exactly 1 — a propose that writes more is not inert", len(rows))
	}
}

// TestSeamKindDriftIsInfraFaultAndWritesNothing — MG9 from the seam side:
// an answer naming any kind but the two narrowing ones never becomes a row.
func TestSeamKindDriftIsInfraFaultAndWritesNothing(t *testing.T) {
	for _, kind := range []string{"mcp_server_add", "policy_propose_add", "", "mcp_server_remove "} {
		e := newMCPMineEnv(t)
		e.spy.ready = protocol.MineProposeReady{
			Kind:    kind,
			Payload: json.RawMessage(`{"name":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`),
		}
		_, _, err := e.do(t, "mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`)
		if err == nil {
			t.Fatalf("kind %q accepted — the two narrowing kinds are an exact list (MG9)", kind)
		}
		rows, _ := e.svc.proposals.ListProposals("proposed")
		if len(rows) != 0 {
			t.Errorf("kind %q refused AFTER a row was written (TD3): %d rows", kind, len(rows))
		}
	}
}

// TestProposeRefusalsCostNoSeamTraffic — the reason rule and the pair rule
// refuse BEFORE the rows are read (cheap refusals, one sentence each).
func TestProposeRefusalsCostNoSeamTraffic(t *testing.T) {
	e := newMCPMineEnv(t)
	cases := []struct{ tool, input, want string }{
		{"mcp_propose_remove_server", `{"server":"acme-crud"}`, "reason"},
		{"mcp_propose_remove_server", `{"server":"acme-crud","reason":"   "}`, "reason"},
		{"mcp_propose_remove_server", `{"reason":"decommissioned; nothing reads it anymore in this loop at all"}`, "server"},
		{"mcp_propose_revoke_grant", `{"server":"acme-crud","tool":"list_issues"}`, "reason"},
		{"mcp_propose_revoke_grant", `{"server":"acme-crud","reason":"the ask only slows the loop down for no benefit at all"}`, "server and tool"},
	}
	for _, tc := range cases {
		_, f, err := e.do(t, tc.tool, tc.input)
		if err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		if f == nil || !strings.Contains(f.Sentence, tc.want) {
			t.Errorf("%s %s: want a refusal naming %q, got %v", tc.tool, tc.input, tc.want, f)
		}
	}
	if len(e.spy.calls) != 0 {
		t.Errorf("refused proposes reached the seam %d times — refusals cost no rows and no reads", len(e.spy.calls))
	}
}

// TestSeamRefusalTravelsVerbatim — the seam's sentence IS the answer.
func TestSeamRefusalTravelsVerbatim(t *testing.T) {
	e := newMCPMineEnv(t)
	e.spy.sent = `refused: no such server "ghost" — mcp_propose_remove_server removes a server THIS agent can see; see the view with mcp_servers`
	_, f, err := e.do(t, "mcp_propose_remove_server", `{"server":"ghost","reason":"it was never real, and now it is gone"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || f.Sentence != e.spy.sent {
		t.Errorf("the seam's refusal must travel verbatim, got %v", f)
	}
}

// TestOpenCapAppliesAtTheAgentDoor — the inherited gate: twenty open rows
// and the twenty-first names the cap (the same path that answers the policy
// twins, measured HERE so the MCP arms cannot drift from it).
func TestProposalLimitAndSize(t *testing.T) {
	e := newMCPMineEnv(t)
	e.spy.ready = protocol.MineProposeReady{
		Kind:    string(desk.KindMCPServerRem),
		Payload: json.RawMessage(`{"name":"acme-crud","cascadeDeclares":false,"reason":"decommissioned; nothing reads it anymore"}`),
	}
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		if _, f, err := e.do(t, "mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`); f != nil || err != nil {
			t.Fatalf("proposal %d refused: %v / %v", i+1, f, err)
		}
	}
	_, f, err := e.do(t, "mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(f.Sentence, "per-agent cap") || !strings.Contains(f.Sentence, fmt.Sprint(desk.MaxOpenProposalsPerAgent)) {
		t.Errorf("the 21st must be refused NAMING the cap, got %v", f)
	}
	// The size half of the same inherited gate: a seam that answered a
	// payload past the 8 KiB bound is refused by name and writes nothing
	// (the bound is createInertProposal's, shared with the policy twins —
	// measured HERE so the MCP arms cannot drift from it).
	e2 := newMCPMineEnv(t)
	big := strings.Repeat("u", 9*1024)
	e2.spy.ready = protocol.MineProposeReady{
		Kind:    string(desk.KindMCPServerRem),
		Payload: json.RawMessage(`{"name":"acme-crud","cascadeDeclares":false,"reason":"` + big + `"}`),
	}
	_, f, err = e2.do(t, "mcp_propose_remove_server", `{"server":"acme-crud","reason":"decommissioned; nothing reads it anymore"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil || !strings.Contains(strings.ToLower(f.Sentence), "8192") && !strings.Contains(f.Sentence, "8 KiB") && !strings.Contains(strings.ToLower(f.Sentence), "8 kib") && !strings.Contains(strings.ToLower(f.Sentence), "too large") {
		t.Errorf("a > 8 KiB payload must be refused NAMING the bound, got %v", f)
	}
	rows, _ := e2.svc.proposals.ListProposals("proposed")
	if len(rows) != 0 {
		t.Errorf("an oversized proposal row was written — TD3: %d rows", len(rows))
	}
}
