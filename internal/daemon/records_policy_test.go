package daemon

// records_policy_test.go — S45 T7 (PM9): the seven arms of
// records_policy.go, walked at the door an agent actually stands at.
// The behaviour rows the task names, each pinned here:
//
//   - the proposed row's SOURCE is forced to agent (input carries no such
//     field; one that arrives is a parse refusal), so PL1's own sentence
//     refuses an allow that got past the schema — verbatim, and no row;
//   - created_by is the caller Do was handed (the step's agent on the tool
//     path, the peer's uid on the socket half), never a typed field;
//   - a proposal is INERT: the arbiter test drives proposes THROUGH A TOOL
//     CALL and pins the whole read surface (policy_list / policy_why /
//     policy_in_force) byte-identical across it, and the policy table
//     unchanged;
//   - the create path is THE create path: same validator, refusal
//     sentence equal to proposal.create's, verbatim (TD3);
//   - the caps: desk.MaxOpenProposalsPerAgent open rows per caller, 8 KiB
//     per payload, 512 bytes per reason — counted refusals, never silence;
//   - the five reads answer identically at both doors (the seed_status
//     parity pattern, this time for a family);
//   - and unwired is STATED, not faked (V13).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
)

// policyProposeEnv is recordsEnv with the three handles the seven arms
// read and write through — and the socket half over THE SAME instances,
// because "one definition, two doors" is what most of these tests measure.
type policyProposeEnv struct {
	recordsEnv
	ps  *policy.Store
	reg *desk.Registry
	tbl *protocol.Table
}

func newPolicyProposeEnv(t *testing.T) policyProposeEnv {
	t.Helper()
	e := policyProposeEnv{recordsEnv: newRecordsEnv(t, "")}
	e.ps = policy.NewStore(newPolicyRepo(e.s))
	e.reg = desk.New(desk.Deps{Boundary: testBoundaryOpener(e.s)}, nil)
	e.svc.policyStore = e.ps
	e.svc.proposals = proposalStore(e.s)
	e.svc.validateProposal = e.reg.ValidateProposal

	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.PolicyStore = e.ps
	tbl.Proposals = proposalStore(e.s)
	tbl.ValidateProposal = e.reg.ValidateProposal
	tbl.Records = policyTestRecords{svc: e.svc, uid: "1000"}
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused a wired records seam")
	}
	init, err := json.Marshal(protocol.InitializeParams{
		ClientName: "t7", ClientVersion: "0", ProtocolVersion: protocol.PROTOCOL_VERSION,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ierr := tbl.Dispatch(context.Background(), "c1", protocol.MethodInitialize, init); ierr != nil {
		t.Fatalf("initialize: %v", ierr)
	}
	e.tbl = tbl
	return e
}

// policyTestRecords is the socket half of the two doors — the same stamp
// socketRecords applies (Actor user/uid from the PEER, never from params).
type policyTestRecords struct {
	svc *RecordsService
	uid string
}

func (a policyTestRecords) Do(ctx context.Context, _ *protocol.Session, tool string, params json.RawMessage) (any, string, error) {
	out, f, err := a.svc.Do(ctx, Actor{Kind: "user", ID: a.uid}, tool, params)
	if f != nil {
		return nil, f.Sentence, protocol.ErrBadParams{Err: errors.New(f.Sentence)}
	}
	return out, "", err
}
func (policyTestRecords) DecideGoal(context.Context, *protocol.Session, protocol.GoalDecideParams) (protocol.DecideView, string, error) {
	return protocol.DecideView{}, "", nil
}
func (policyTestRecords) DecideBrief(context.Context, *protocol.Session, protocol.BriefDecideParams) (protocol.DecideView, string, error) {
	return protocol.DecideView{}, "", nil
}
func (policyTestRecords) DecideTask(context.Context, *protocol.Session, protocol.TaskDecideParams) (protocol.DecideView, string, error) {
	return protocol.DecideView{}, "", nil
}
func (policyTestRecords) TaskList(context.Context, *protocol.Session, protocol.TaskListParams) (protocol.TaskListResult, error) {
	return protocol.TaskListResult{}, nil
}
func (policyTestRecords) GoalShow(context.Context, *protocol.Session, protocol.GoalShowParams) (protocol.GoalShowResult, string, error) {
	return protocol.GoalShowResult{}, "", nil
}

const t7Reason = `"reason":"the review lane never needs a shell; this agent proposes tightening its own reach"`

// t7AddInput is a minimal VALID policy_propose_add input (tool_access deny
// — the spec's state restates the action, as the per-type validator
// demands). extra joins the members, so smuggled keys and allow attempts
// reuse the same body.
func t7AddInput(id, action, specState, extra string) string {
	if extra != "" && !strings.HasPrefix(extra, ",") {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"id":%q,"type":"tool_access","target":{"kind":"agent","id":"implementer"},"action":%q,"spec":{"toolName":"bash","state":%q},%s%s}`,
		id, action, specState, t7Reason, extra)
}

func rows(t *testing.T, e policyProposeEnv, status string) []protocol.ProposalRow {
	t.Helper()
	got, err := e.svc.proposals.ListProposals(status)
	if err != nil {
		t.Fatalf("ListProposals(%q): %v", status, err)
	}
	return got
}

// — behaviour 1: the source is forced, and PL1 is the second line —

func TestProposeAddForcesAgentSource(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// Identity members are PARSE refusals (the schema has none; decode
	// refuses one that arrives) — a source the caller types cannot land.
	for _, smuggle := range []string{`"source":"operator"`, `"createdBy":{"kind":"user","id":"ghost"}`, `"status":"committed"`, `"kind":"policy_add"`, `"actor":"root"`} {
		_, f, err := e.do(t, "policy_propose_add", t7AddInput("smuggled", "deny", "deny", smuggle))
		if err != nil || f == nil || !strings.Contains(f.Sentence, "refused:") {
			t.Fatalf("smuggled %s: %v %+v — identity must be a PARSE refusal", smuggle, err, f)
		}
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows from smuggled calls — a parse refusal writes nothing", n)
	}
	// The clean call stores source "agent" although no input named one.
	out, f, err := e.do(t, "policy_propose_add", t7AddInput("no-bash", "deny", "deny", ""))
	if err != nil || f != nil {
		t.Fatalf("propose: %v %+v", err, f)
	}
	got := rows(t, e, "proposed")
	if len(got) != 1 {
		t.Fatalf("rows %d, want 1", len(got))
	}
	var payload map[string]any
	if jerr := json.Unmarshal(got[0].Payload, &payload); jerr != nil {
		t.Fatal(jerr)
	}
	if payload["source"] != "agent" {
		t.Fatalf("stored source %v, want the FORCED \"agent\"", payload["source"])
	}
	if payload["reason"] == nil || payload["reason"] == "" {
		t.Fatalf("reason did not ride into the payload: %v", payload)
	}
	res := out.(protocol.PolicyProposeResult)
	if res.Proposal.ID != got[0].ID || res.Proposal.Command != protocol.ProposalCommand(got[0].ID) {
		t.Fatalf("result row/command %+v — the commit command is THE one, copyable (PM7)", res.Proposal)
	}
	if res.Note != protocol.PolicyProposeInertNote {
		t.Fatalf("note %q, want the fixed inertness sentence (TD2)", res.Note)
	}
}

func TestProposeAddRefusesAllowEvenIfSchemaBypassed(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// The schema's enum forbids "allow"; the SERVICE forbids it again: the
	// source stays forced to agent and the shared validator answers with
	// PL1's OWN sentence — the same one the human authoring door carries.
	_, f, err := e.do(t, "policy_propose_add", t7AddInput("self-grant", "allow", "allow", ""))
	if err != nil || f == nil {
		t.Fatalf("allow proposal: %v %+v — the second line must refuse", err, f)
	}
	want := `refused: Allow authored by source "agent" requires an operator (PL1) — a Deny or an Ask from "agent" would be accepted`
	if f.Sentence != want {
		t.Fatalf("PL1 sentence drifted:\n got %q\nwant %q", f.Sentence, want)
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows after the refusal — a refused proposal writes NOTHING (TD3)", n)
	}
	// The direct-create proof: an allow authored by agent is refused even
	// when no tool, schema or service is involved at all — the boundary
	// itself is the wall the propose door can never be weaker than.
	if rej := e.ps.Add(context.Background(), policy.Policy{
		ID: "direct-allow", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.TargetKind("all-agents")},
		Action: policy.Allow, Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`),
		Source: policy.Agent,
	}, time.Now()); rej == nil || !strings.Contains(rej.Reason, "(PL1)") {
		t.Fatalf("direct agent-authored allow: %+v — checkAuthorship must refuse it", rej)
	}
}

// — inertness: the arbiter, driven THROUGH A TOOL CALL —

func TestProposeIsInertAcrossAToolCall(t *testing.T) {
	e := newPolicyProposeEnv(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if rej := e.ps.Add(context.Background(), policy.Policy{
		ID: "keep-me", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.TargetKind("all-agents")},
		Action: policy.Deny, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
		Source: policy.Operator,
	}, at); rej != nil {
		t.Fatalf("seed policy: %s", rej.Reason)
	}
	before := t7ReadSurface(t, e, at)

	// Both proposes, through the AGENT door: propose a new deny and
	// propose removing the operator's row. Neither may move the surface.
	if _, f, err := e.do(t, "policy_propose_add", t7AddInput("deny-webfetch", "deny", "deny", "")); err != nil || f != nil {
		t.Fatalf("propose add: %v %+v", err, f)
	}
	if _, f, err := e.do(t, "policy_propose_remove", `{"id":"keep-me","reason":"the review lane tightened; this rule is stale"}`); err != nil || f != nil {
		t.Fatalf("propose remove: %v %+v", err, f)
	}
	after := t7ReadSurface(t, e, at)

	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("read surface %d moved across two proposes:\n before %s\n after  %s", i, before[i], after[i])
		}
	}
	listed, rej := e.ps.List(context.Background(), at)
	if rej != nil {
		t.Fatal(rej.Reason)
	}
	if len(listed) != 1 || listed[0].ID != "keep-me" {
		t.Fatalf("policy table after proposes: %+v — the propose door never touches it", listed)
	}
	// …and the proposals DID land (inert, not lost): two rows, both the
	// agent's, both waiting for a person.
	got := rows(t, e, "proposed")
	if len(got) != 2 {
		t.Fatalf("open proposals %d, want 2 — inertness is not silence", len(got))
	}
	for _, p := range got {
		if p.Status != "proposed" || p.CreatedByKind != "agent" || p.CreatedByID != "implementer" {
			t.Fatalf("row %+v — proposed rows stay proposed and carry their author", p)
		}
	}
}

// t7ReadSurface renders the tool door's reads for the inertness arbiter —
// each a byte string, marshalled exactly as the door answers.
func t7ReadSurface(t *testing.T, e policyProposeEnv, at time.Time) []string {
	t.Helper()
	atStr := at.Format(time.RFC3339)
	var out []string
	for _, call := range []struct{ name, in string }{
		{"policy_list", `{}`},
		{"policy_list", `{"type":"tool_access"}`},
		{"policy_why", fmt.Sprintf(`{"type":"tool_access","agent":"implementer","tool":"bash","at":%q}`, atStr)},
		{"policy_in_force", `{"agent":"implementer"}`},
		{"policy_show", `{"id":"keep-me"}`},
		// proposal_list is deliberately NOT in the inert set: proposals
		// arriving is exactly what the proposes do. The POLICY reads are
		// what must not move.
	} {
		res, f, err := e.do(t, call.name, call.in)
		if err != nil || f != nil {
			t.Fatalf("%s %s: %v %+v", call.name, call.in, err, f)
		}
		b, merr := json.Marshal(res)
		if merr != nil {
			t.Fatal(merr)
		}
		out = append(out, string(b))
	}
	return out
}

// — the create path is THE create path (TD3), and the actor is the caller —

func TestProposeValidatorRefusalIsProposalCreatesRefusal(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// A policy type nobody compiled: the desk validator is the one to
	// notice, on BOTH doors, with the one sentence.
	_, tf, terr := e.do(t, "policy_propose_add", t7AddInput("bogus", "deny", "deny", `"type":"banana_access"`))
	if terr != nil || tf == nil {
		t.Fatalf("bogus-type propose: %v %+v — TD3 refuses at create", terr, tf)
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows written past the validator — a commit-door refusal must be a create refusal", n)
	}
	create, cerr := json.Marshal(map[string]any{
		"kind": "policy_add",
		"payload": map[string]any{
			"id": "bogus", "type": "banana_access",
			"target": map[string]string{"kind": "agent", "id": "implementer"},
			"action": "deny", "spec": map[string]string{"toolName": "bash", "state": "deny"},
			"source": "agent",
		},
	})
	if cerr != nil {
		t.Fatal(cerr)
	}
	_, serr := e.tbl.Dispatch(context.Background(), "c1", protocol.MethodProposalCreate, create)
	var pe *protocol.Error
	if serr == nil || !errors.As(serr, &pe) {
		t.Fatalf("proposal.create with the same payload: %v — it must refuse too", serr)
	}
	if pe.Data.What != tf.Sentence {
		t.Fatalf("the two create doors drifted:\n tool   %q\n socket %q", tf.Sentence, pe.Data.What)
	}
}

func TestProposeRemoveRefusesWhatCommitWouldNotDo(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// No policy by that name: the commit door would remove nothing; the
	// create door refuses the PROPOSAL of it (TD3, the desk's own check).
	_, f, err := e.do(t, "policy_propose_remove", `{"id":"never-existed","reason":"nothing carries this id anywhere"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, `no policy called "never-existed"`) {
		t.Fatalf("remove of an unknown id: %v %+v", err, f)
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows — a create-time refusal writes nothing", n)
	}
}

func TestProposeCreatedByIsTheCallerNotTheInput(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// Tool path: the step's agent (the Actor Do was handed from the step).
	if _, f, err := e.do(t, "policy_propose_add", t7AddInput("a1", "deny", "deny", "")); err != nil || f != nil {
		t.Fatalf("tool propose: %v %+v", err, f)
	}
	// Socket twin: the PEER's uid, stamped by the records socket half.
	params := json.RawMessage(t7AddInput("a2", "deny", "deny", ""))
	out, derr := e.tbl.Dispatch(context.Background(), "c1", protocol.MethodPolicyProposeAdd, params)
	if derr != nil {
		t.Fatalf("socket propose twin: %v", derr)
	}
	got := rows(t, e, "proposed")
	if len(got) != 2 {
		t.Fatalf("rows %d, want 2", len(got))
	}
	// Position-free: the store lists newest-first; the two stamps are what
	// matter, each attached to its own row.
	byPayload := map[string]protocol.ProposalRow{}
	for _, p := range got {
		var m struct {
			ID string `json:"id"`
		}
		if jerr := json.Unmarshal(p.Payload, &m); jerr != nil {
			t.Fatal(jerr)
		}
		byPayload[m.ID] = p
	}
	if p := byPayload["a1"]; p.CreatedByKind != "agent" || p.CreatedByID != "implementer" {
		t.Fatalf("tool row stamped %s/%s, want the step's agent implementer", p.CreatedByKind, p.CreatedByID)
	}
	if p := byPayload["a2"]; p.CreatedByKind != "user" || p.CreatedByID != "1000" {
		t.Fatalf("socket row stamped %s/%s, want the peer user/1000", p.CreatedByKind, p.CreatedByID)
	}
	res := out.(protocol.PolicyProposeResult)
	if res.Proposal.CreatedByKind != "user" || res.Proposal.CreatedByID != "1000" {
		t.Fatalf("socket result row %+v — the answer carries the stamp too", res.Proposal)
	}
}

// — the caps (behaviour 4) —

func TestProposePerAgentCapIsCountedNotSilent(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// A second caller is a different caller: its rows do not spend the
	// agent's budget (the cap counts THE CALLER's open rows).
	if _, err := e.svc.proposals.CreateProposal(context.Background(), "policy_add", json.RawMessage(`{"id":"user-row","reason":"a person's own"}`), "user", "1000"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < desk.MaxOpenProposalsPerAgent; i++ {
		id := fmt.Sprintf("flood-%02d", i)
		if _, f, err := e.do(t, "policy_propose_add", t7AddInput(id, "deny", "deny", "")); err != nil || f != nil {
			t.Fatalf("propose %d: %v %+v — the first %d must pass", i, err, f, desk.MaxOpenProposalsPerAgent)
		}
	}
	_, f, err := e.do(t, "policy_propose_add", t7AddInput("flood-21", "deny", "deny", ""))
	if err != nil || f == nil {
		t.Fatalf("the 21st passed — the cap is enforced by counting, not hope")
	}
	if !strings.Contains(f.Sentence, fmt.Sprintf("%d", desk.MaxOpenProposalsPerAgent)) || !strings.Contains(f.Sentence, "proposal_list") {
		t.Fatalf("cap refusal %q — must name the limit and the way to SEE the queue", f.Sentence)
	}
	held := 0
	for _, p := range rows(t, e, "proposed") {
		if p.CreatedByKind == "agent" && p.CreatedByID == "implementer" {
			held++
		}
	}
	if held != desk.MaxOpenProposalsPerAgent {
		t.Fatalf("%d open agent rows after the cap — the refusal wrote nothing", held)
	}
	// The cap is per CALLER, not global: the person's door still files.
	if _, derr := e.tbl.Dispatch(context.Background(), "c1", protocol.MethodPolicyProposeAdd, json.RawMessage(t7AddInput("person-row", "deny", "deny", ""))); derr != nil {
		t.Fatalf("the second caller was capped too — the cap counts the caller: %v", derr)
	}
}

func TestProposePayloadSizeCap(t *testing.T) {
	e := newPolicyProposeEnv(t)
	// A spec whose filler alone blows the 8 KiB ceiling. (The validator
	// would answer too — but the cap answers FIRST, and never truncates.)
	filler := strings.Repeat("x", maxProposePayloadBytes+512)
	in := fmt.Sprintf(`{"id":"fat","type":"tool_access","target":{"kind":"agent","id":"implementer"},"action":"deny","spec":{"toolName":%q,"state":"deny"},"reason":"a fat spec tests the ceiling"}`, filler)
	_, f, err := e.do(t, "policy_propose_add", in)
	if err != nil || f == nil {
		t.Fatalf("oversized payload accepted — the ceiling is a wall, not a warning")
	}
	if !strings.Contains(f.Sentence, fmt.Sprintf("%d", maxProposePayloadBytes)) || !strings.Contains(f.Sentence, "bytes") {
		t.Fatalf("size refusal %q — must name the size and the cap", f.Sentence)
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows — refuse above, never truncate into", n)
	}
}

func TestProposeReasonIsRequiredReadably(t *testing.T) {
	e := newPolicyProposeEnv(t)
	for _, in := range []string{
		`{"id":"nr","type":"tool_access","target":{"kind":"agent","id":"implementer"},"action":"deny","spec":{"toolName":"bash","state":"deny"}}`,
		`{"id":"nr","type":"tool_access","target":{"kind":"agent","id":"implementer"},"action":"deny","spec":{"toolName":"bash","state":"deny"},"reason":"   "}`,
	} {
		_, f, err := e.do(t, "policy_propose_add", in)
		if err != nil || f == nil || !strings.Contains(f.Sentence, "reason") {
			t.Fatalf("reasonless propose %s: %v %+v", in, err, f)
		}
	}
	long := strings.Repeat("y", 513)
	_, f, err := e.do(t, "policy_propose_add", `{"id":"lr","type":"tool_access","target":{"kind":"agent","id":"implementer"},"action":"deny","spec":{"toolName":"bash","state":"deny"},"reason":"`+long+`"}`)
	if err != nil || f == nil || !strings.Contains(f.Sentence, "512") {
		t.Fatalf("513-byte reason: %v %+v — the why is for reading", err, f)
	}
	if n := len(rows(t, e, "proposed")); n != 0 {
		t.Fatalf("%d rows — every reason refusal above wrote nothing", n)
	}
}

// — the five reads answer identically at both doors —

func TestPolicyToolReadsMatchSocketReads(t *testing.T) {
	e := newPolicyProposeEnv(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if rej := e.ps.Add(context.Background(), policy.Policy{
		ID: "keep-me", Type: policy.ToolAccess,
		Target: policy.Target{Kind: policy.TargetKind("all-agents")},
		Action: policy.Deny, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
		Source: policy.Operator,
	}, at); rej != nil {
		t.Fatal(rej.Reason)
	}
	if _, f, err := e.do(t, "policy_propose_add", t7AddInput("seen", "deny", "deny", "")); err != nil || f != nil {
		t.Fatalf("propose: %v %+v", err, f)
	}
	pairs := []struct {
		tool   string
		method protocol.MethodName
		in     string
	}{
		{"policy_list", protocol.MethodPolicyList, `{}`},
		{"policy_list", protocol.MethodPolicyList, `{"type":"tool_access","source":"operator"}`},
		{"policy_show", protocol.MethodPolicyGet, `{"id":"keep-me"}`},
		{"policy_show", protocol.MethodPolicyGet, `{"id":"nope"}`},
		{"policy_why", protocol.MethodPolicyWhy, `{"type":"tool_access","agent":"implementer","tool":"bash"}`},
		{"policy_in_force", protocol.MethodPolicyInForce, `{"agent":"implementer"}`},
		{"proposal_list", protocol.MethodProposalList, `{}`},
	}
	for _, p := range pairs {
		toolOut, f, err := e.do(t, p.tool, p.in)
		if err != nil {
			t.Fatalf("tool %s %s: %v", p.tool, p.in, err)
		}
		sock, serr := e.tbl.Dispatch(context.Background(), "c1", p.method, json.RawMessage(p.in))
		if f != nil {
			// A refusal is a fact too: both doors refuse, and the tool's
			// sentence is the socket error rendered by the one helper.
			if serr == nil {
				t.Fatalf("%s %s: tool refused %q, socket accepted", p.tool, p.in, f.Sentence)
			}
			if s := wireRefusal(serr); s == nil || s.Sentence != f.Sentence {
				t.Fatalf("%s %s refusal drifted: tool %q, socket %v", p.tool, p.in, f.Sentence, serr)
			}
			continue
		}
		if serr != nil {
			t.Fatalf("socket %s %s: %v", p.method, p.in, serr)
		}
		a, aerr := json.Marshal(toolOut)
		b, berr := json.Marshal(sock)
		if aerr != nil || berr != nil {
			t.Fatal(aerr, berr)
		}
		if string(a) != string(b) {
			t.Fatalf("%s %s drifted between the doors:\n tool   %s\n socket %s", p.tool, p.in, a, b)
		}
	}
}

// — unwired is stated, never faked (V13) —

func TestPolicyArmsRefuseWithoutTheirStores(t *testing.T) {
	e := newRecordsEnv(t, "") // a bare service: no policy store, no proposals
	for _, c := range []struct{ name, in, want string }{
		{"policy_list", `{}`, "policy store"},
		{"policy_show", `{"id":"x"}`, "policy store"},
		{"policy_why", `{"type":"tool_access","agent":"a"}`, "policy store"},
		{"policy_in_force", `{}`, "policy store"},
		{"proposal_list", `{}`, "proposal store"},
		{"policy_propose_add", t7AddInput("x", "deny", "deny", ""), "proposal store"},
		{"policy_propose_remove", `{"id":"x","reason":"unwired world"}`, "proposal store"},
	} {
		out, f, err := e.do(t, c.name, c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if out != nil || f == nil || !strings.Contains(f.Sentence, c.want) {
			t.Fatalf("%s unwired: %v %+v — must refuse naming the missing half (V13), never answer zero", c.name, out, f)
		}
	}
}
