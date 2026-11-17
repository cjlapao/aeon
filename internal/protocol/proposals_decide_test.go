package protocol

// proposals_decide_test.go — S45 T5: the decision doors' protocol behaviour,
// on a hand-built table (the daemon's own wiring is exercised end-to-end in
// internal/daemon/proposal_decide_test.go). The stub decider records what
// the doors hand the service (the ACTOR the table stamped, the id, the
// digest) and answers canned results; the door-side rules — conditional
// registration, identity refusal, the peer stamp, the reason discipline,
// and the refusal-verbatim rule — are what these tests own.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
)

// decideStub records the last call per method and answers canned values.
type decideStub struct {
	lastActor  Actor
	lastID     int64
	lastDigest string
	lastReason string
	commitRes  ProposalCommitResult
	commitErr  error
	rejectRow  ProposalRow
	rejectErr  error
	getRow     ProposalRow
	getFound   bool
	getErr     error
}

func (s *decideStub) GetProposal(ctx context.Context, id int64) (ProposalRow, bool, error) {
	s.lastID = id
	if s.getErr != nil {
		return ProposalRow{}, false, s.getErr
	}
	return s.getRow, s.getFound, nil
}

func (s *decideStub) ProposalDigest(payload string) string {
	return desk.ProposalPayloadDigest(payload)
}

func (s *decideStub) CommitProposal(ctx context.Context, actor Actor, id int64, digest string) (ProposalCommitResult, error) {
	s.lastActor, s.lastID, s.lastDigest = actor, id, digest
	if s.commitErr != nil {
		return ProposalCommitResult{}, s.commitErr
	}
	return s.commitRes, nil
}

func (s *decideStub) RejectProposal(ctx context.Context, actor Actor, id int64, reason string) (ProposalRow, error) {
	s.lastActor, s.lastID, s.lastReason = actor, id, reason
	if s.rejectErr != nil {
		return ProposalRow{}, s.rejectErr
	}
	return s.rejectRow, nil
}

// isRefused compares by code: *protocol.Error carries no Is method (the
// package's own sentinel style — Dispatch rewrites by value, not by chain).
func isRefused(err error) bool {
	e, ok := err.(*Error)
	return ok && e.Code == ErrProposalDecideRefused.Code
}

// refusedWhat is the sentence under test: *Error.Error() renders code and
// message only — the refusal's `what` (the verbatim sentence) lives in
// Data and must be read from the value, never from the formatted string.
func refusedWhat(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Data.What
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func decideTable(stub *decideStub, actor Actor, actorErr error) *Table {
	t := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	t.ProposalDecider = stub
	if actorErr == nil {
		t.ActorFor = func(ctx context.Context, sess *Session) Actor { return actor }
	} else {
		t.ActorFor = func(ctx context.Context, sess *Session) Actor { return Actor{} }
	}
	t.RegisterProposalDecider()
	return t
}

func decideDispatch(t *Table, method MethodName, params any) (any, error) {
	if t.Sessions == nil {
		panic("decideDispatch needs NewTable-built tables (use decideTable/newDecideTable)")
	}
	// Every decide door requires the handshake; stand one up per table per
	// connection id (the same route the real transport runs).
	if _, ok := t.Sessions.Get("peer-x"); !ok {
		if err := t.Sessions.Open(context.Background(), &Session{ConnID: "peer-x", Version: PROTOCOL_VERSION, ClientName: "t5", ClientVersion: "0"}); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(params)
	return t.Dispatch(context.Background(), "peer-x", method, raw)
}

func newDecideTable() *Table {
	return NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
}

func TestProposalDecideMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := &Table{}
	if tbl.RegisterProposalDecider() {
		t.Fatal("nil seam registered the doors")
	}
	for _, m := range []MethodName{MethodProposalGet, MethodProposalCommit, MethodProposalReject} {
		if _, reg := tbl.Lookup(m); reg {
			t.Errorf("%s registered with a nil seam — registered-and-refusing is the shape V13 forbids", m)
		}
		_, err := tbl.Dispatch(context.Background(), "peer-x", m, json.RawMessage(`{"id":1}`))
		if _, ok := err.(ErrUnknownMethod); !ok {
			t.Errorf("%s with a nil seam answered %v, want the ErrUnknownMethod sentinel", m, err)
		}
	}
}

func TestProposalDecideMethodsRegisteredAndNotDebug(t *testing.T) {
	tbl := decideTable(&decideStub{}, Actor{Kind: "user", ID: "4321"}, nil)
	for _, m := range []MethodName{MethodProposalGet, MethodProposalCommit, MethodProposalReject} {
		meth, reg := tbl.Lookup(m)
		if !reg {
			t.Fatalf("%s not registered with the seam wired", m)
		}
		if !meth.RequiresInitialize {
			t.Errorf("%s must RequireInitialize", m)
		}
		if meth.Debug {
			t.Errorf("%s must not be Debug (product surface; PM10 owns the narrow projection's exclusion)", m)
		}
	}
}

func TestProposalDecideActorFromTableNotParams(t *testing.T) {
	stub := &decideStub{}
	tbl := decideTable(stub, Actor{Kind: "user", ID: "4321"}, nil)

	// ORDER: the actor is resolved BEFORE params decode — a no-peer
	// connection with a malformed body answers the actor refusal, never the
	// decode error (the T4 authoring discipline, carried).
	noPeer := decideTable(stub, Actor{}, errors.New("no peer"))
	_, err := decideDispatch(noPeer, MethodProposalCommit, map[string]any{"id": 1, "digest": "d"})
	if !errors.Is(err, ErrPolicyNoActor) {
		t.Fatalf("commit without a resolvable peer: %v, want ErrPolicyNoActor", err)
	}

	// An `actor` key in params is a shape error naming the key (the caller
	// may not name its own identity), even with a good peer.
	_, err = decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 1, "digest": "d", "actor": map[string]any{"kind": "user", "id": "9999"}})
	if err == nil || !strings.Contains(refusedWhat(err)+err.Error(), "actor") {
		t.Fatalf("params actor accepted or unnamed: %v", err)
	}
	if !strings.Contains(refusedWhat(err)+err.Error(), "proposal.commit") {
		t.Errorf("the shape refusal must name the method: %v", err)
	}
	if stub.lastActor.ID == "9999" {
		t.Fatal("the params actor reached the service")
	}

	// A clean commit hands the service the CONNECTION's stamp, verbatim.
	if _, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 7, "digest": "abc"}); err != nil {
		t.Fatalf("clean commit: %v", err)
	}
	if stub.lastActor != (Actor{Kind: "user", ID: "4321"}) || stub.lastID != 7 || stub.lastDigest != "abc" {
		t.Fatalf("the service saw actor %+v id %d digest %q", stub.lastActor, stub.lastID, stub.lastDigest)
	}
}

func TestProposalDecideNoActorRefusesCommitAndReject(t *testing.T) {
	stub := &decideStub{}
	tbl := decideTable(stub, Actor{}, errors.New("unresolvable"))
	if _, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 1, "digest": "d"}); !errors.Is(err, ErrPolicyNoActor) {
		t.Errorf("commit with uid-0 peer: %v", err)
	}
	if _, err := decideDispatch(tbl, MethodProposalReject, map[string]any{"id": 1, "reason": "no"}); !errors.Is(err, ErrPolicyNoActor) {
		t.Errorf("reject with uid-0 peer: %v", err)
	}
	if stub.lastActor.ID != "" {
		t.Fatal("the service was reached despite the missing actor")
	}
	// get is a read — it still works without a person (the row is readable;
	// only the WRITING doors need a name).
	stub.getFound, stub.getRow = true, ProposalRow{ID: 1, Kind: "policy_add"}
	if _, err := decideDispatch(tbl, MethodProposalGet, map[string]any{"id": 1}); err != nil {
		t.Errorf("get without a peer should stand (a read is not a decision): %v", err)
	}
}

func TestProposalCommitRequiresDigest(t *testing.T) {
	stub := &decideStub{}
	tbl := decideTable(stub, Actor{Kind: "user", ID: "4321"}, nil)
	for _, d := range []string{"", "   "} {
		_, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 1, "digest": d})
		if !isRefused(err) || !strings.Contains(refusedWhat(err), "digest") {
			t.Fatalf("digest %q: %v, want the digest-bound refusal", d, err)
		}
	}
}

func TestProposalRejectReasonDiscipline(t *testing.T) {
	stub := &decideStub{rejectRow: ProposalRow{ID: 1, Status: "rejected"}}
	tbl := decideTable(stub, Actor{Kind: "user", ID: "4321"}, nil)
	for _, r := range []string{"", "   ", "\t\n"} {
		if _, err := decideDispatch(tbl, MethodProposalReject, map[string]any{"id": 1, "reason": r}); !isRefused(err) {
			t.Fatalf("blank reason %q: %v", r, err)
		}
	}
	long := strings.Repeat("x", 513)
	_, err := decideDispatch(tbl, MethodProposalReject, map[string]any{"id": 1, "reason": long})
	if !isRefused(err) || !strings.Contains(refusedWhat(err), "512") {
		t.Fatalf("513-byte reason: %v", err)
	}
	if _, err := decideDispatch(tbl, MethodProposalReject, map[string]any{"id": 1, "reason": strings.Repeat("x", 512)}); err != nil {
		t.Fatalf("512-byte reason must pass the door: %v", err)
	}
	if _, err := decideDispatch(tbl, MethodProposalReject, map[string]any{"id": 1}); !isRefused(err) {
		t.Fatalf("missing reason: %v", err)
	}
}

func TestProposalDecideRefusalsTravelVerbatim(t *testing.T) {
	stub := &decideStub{commitErr: errors.New("refused: the proposal changed since you read it — its digest is A, the commit carried B")}
	tbl := decideTable(stub, Actor{Kind: "user", ID: "4321"}, nil)
	_, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 1, "digest": "B"})
	e, ok := err.(*Error)
	if !ok || e.Code != ErrProposalDecideRefused.Code {
		t.Fatalf("plain service error not mapped: %T %v", err, err)
	}
	if e.Data.What != "refused: the proposal changed since you read it — its digest is A, the commit carried B" {
		t.Errorf("the sentence did not travel verbatim: %q", e.Data.What)
	}
	// A typed service error (infrastructure) travels UNCHANGED — never
	// dressed as a refusal.
	stub.commitErr = ErrStoreUnavailable.WithWhat("the disk broke")
	_, err = decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 1, "digest": "B"})
	if e2, ok := err.(*Error); !ok || e2.Code != ErrStoreUnavailable.Code {
		t.Fatalf("typed error rewritten: %v", err)
	}
}

func TestCommitAgentImportCommitsLikeEveryKind(t *testing.T) {
	// S45 T10 superseded TestCommitAgentImportRefusedByName (T5's by-name
	// refusal, built to stand only until S43 merged). The kind now runs the
	// GENERIC path: the stub delegates to the REAL desk.Commit over a
	// store-shaped fake with agent_import seams registered (the daemon's
	// adapter shape — same desk, same registry call), and the door must
	// answer like it does for every kind: digest-bound success, the flip
	// stamped user:<peer uid>, the consent print riding back as DATA on the
	// result, the agent before/after pair from S37's renderers, and ZERO
	// policy_change rows (the authoring audit is policy's, T10's ruling).
	stub := &agentImportToDesk{
		row: desk.Row{ID: 3, Kind: "agent_import", Payload: `{"pkgPath":"/abs/agents/groot"}`, Status: "proposed"},
		preview: func(context.Context, string) (*AgentImportPreview, string, error) {
			return &AgentImportPreview{Agent: "groot", Consent: "CONSENT: requests bash", CarriedDeny: []AgentImportCarried{}}, "", nil
		},
	}
	tbl := newDecideTable()
	tbl.ProposalDecider = stub
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "4321"} }
	tbl.RegisterProposalDecider()
	out, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 3, "digest": desk.ProposalPayloadDigest(`{"pkgPath":"/abs/agents/groot"}`)})
	if err != nil {
		t.Fatalf("agent_import commit refused where T10 promised success: %v", err)
	}
	res, ok := out.(ProposalCommitResult)
	if !ok {
		t.Fatalf("commit answer shape: %T", out)
	}
	if !res.EffectApplied || res.Proposal.Status != "committed" {
		t.Fatalf("half a commit: %+v", res)
	}
	if res.Proposal.DecidedByKind != "user" || res.Proposal.DecidedByID != "4321" {
		t.Errorf("decided_by = %s:%s — the peer stamp must ride the row (PM7.3)", res.Proposal.DecidedByKind, res.Proposal.DecidedByID)
	}
	if !stub.store.flipRecorded || stub.store.flipStatus != "committed" {
		t.Fatalf("the flip never reached the store: %+v", stub.store)
	}
	if stub.store.audits != 0 {
		t.Fatalf("agent_import wrote %d policy_change rows — PM8's audit is policy authoring only", stub.store.audits)
	}
	var joined string
	for _, w := range res.Warnings {
		joined += w
	}
	if !strings.Contains(joined, "imported groot as DISABLED") {
		t.Errorf("the consent print never reached the socket result: %+v", res.Warnings)
	}
	if res.Before.Target != "agent:groot" || res.Before.Answers[0].Render != "CONSENT: requests bash" {
		t.Errorf("before-view is not S37's preview verbatim: %+v", res.Before)
	}
	if res.After.Target != "agent:groot" || !strings.Contains(res.After.Answers[0].Render, "imported groot as DISABLED") {
		t.Errorf("after-view is not the body's own post-import print: %+v", res.After)
	}
	// The digest gate still bites on this kind — a commit not bound to what
	// was read commits nothing (PM7.1 is not relaxed for the new path).
	if _, err := decideDispatch(tbl, MethodProposalCommit, map[string]any{"id": 3, "digest": "deadbeef"}); !isRefused(err) {
		t.Errorf("a wrong digest committed agent_import: %v", err)
	}
}

// agentImportToDesk plays the daemon adapter's role for the protocol doors:
// it runs the REAL desk.Commit over a faked row read and write triple, with
// agent_import seams registered — Apply prints the consent to the writer
// desk hands it (DecideDeps.ApplyWriter), exactly as cmd/aeon's seam prints
// it in the product, and the door shapes the answer with the SAME exported
// evidence builders the daemon adapter uses (one definition, T10).
type agentImportToDesk struct {
	row     desk.Row
	preview AgentImportRenderer
	store   decisionRecord
}

func (a *agentImportToDesk) GetProposal(ctx context.Context, id int64) (ProposalRow, bool, error) {
	return ProposalRow{ID: a.row.ID, Kind: a.row.Kind, Payload: json.RawMessage(a.row.Payload), Status: a.row.Status,
		DecidedByKind: a.row.DecidedByKind, DecidedByID: a.row.DecidedByID}, true, nil
}

func (a *agentImportToDesk) ProposalDigest(payload string) string {
	return desk.ProposalPayloadDigest(payload)
}

func (a *agentImportToDesk) CommitProposal(ctx context.Context, actor Actor, id int64, digest string) (ProposalCommitResult, error) {
	agent, before := AgentImportBeforeView(ctx, a.preview, []byte(a.row.Payload))
	var imported strings.Builder
	res, err := desk.Commit(ctx, desk.DecideDeps{
		Get:         func(int64) (desk.Row, bool, error) { return a.row, true, nil },
		Store:       &a.store,
		ApplyWriter: &imported,
	}, agentImportRegistry(), id, digest, desk.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return ProposalCommitResult{}, err
	}
	// The row as the store left it (the stub's flip records stand in for
	// the re-read the real store answers).
	after := AgentImportAfterView(agent, imported.String())
	return ProposalCommitResult{
		Proposal: ProposalRow{ID: res.Row.ID, Kind: res.Row.Kind, Status: "committed",
			DecidedByKind: "user", DecidedByID: actor.ID},
		EffectApplied: res.EffectApplied,
		Before:        before,
		After:         after,
		Warnings:      append(res.Warnings, strings.TrimSpace(imported.String())),
	}, nil
}

func (a *agentImportToDesk) RejectProposal(ctx context.Context, actor Actor, id int64, reason string) (ProposalRow, error) {
	return ProposalRow{}, nil
}

func agentImportRegistry() *desk.Registry {
	return desk.New(desk.Deps{Boundary: func() (desk.Boundary, func(), error) {
		return nil, func() {}, errors.New("no boundary in this stub")
	}}, desk.Seams{desk.KindAgentImport: {
		Validate:       func(context.Context, []byte) error { return nil },
		AlreadyApplied: func(context.Context, []byte) (bool, error) { return false, nil },
		Apply: func(_ context.Context, w io.Writer, _ []byte) error {
			_, err := io.WriteString(w, "imported groot as DISABLED (AP2: import runs the check and stops)\n")
			return err
		},
	}})
}

// decisionRecord is the write triple with a memory: the flip (and who won
// it), applied_error writes, and the audit count — T10 pins audits at zero
// for agent_import, so the counter is the assertion's evidence.
type decisionRecord struct {
	flipRecorded bool
	flipStatus   string
	flipByKind   string
	flipByID     string
	audits       int
}

func (f *decisionRecord) DecideProposal(id int64, status string, by desk.ActorPair, reason, at string) error {
	f.flipRecorded, f.flipStatus, f.flipByKind, f.flipByID = true, status, by.Kind, by.ID
	return nil
}
func (f *decisionRecord) SetProposalAppliedError(id int64, msg string) error { return nil }
func (f *decisionRecord) PolicyChangeInsert(ctx context.Context, c desk.PolicyChangeRow) error {
	f.audits++
	return nil
}

func TestProposalGetCarriesDigestAndPreview(t *testing.T) {
	payload := `{"id":"deny-webfetch-boss","type":"tool_access","target":{"kind":"agent","id":"boss"},"action":"deny","spec":{"toolName":"webfetch"},"source":"operator"}`
	stub := &decideStub{
		getFound: true,
		getRow:   ProposalRow{ID: 9, Kind: "policy_add", Payload: json.RawMessage(payload), Status: "proposed"},
	}
	tbl := decideTable(stub, Actor{Kind: "user", ID: "4321"}, nil)
	// The preview reads through the policy store handle — mount a real
	// boundary holding one rule for agent:boss so the inForce half is a
	// real answer, not a stub.
	tbl.PolicyStore = decidePreviewStore(t)
	res, err := decideDispatch(tbl, MethodProposalGet, map[string]any{"id": 9})
	if err != nil {
		t.Fatalf("proposal.get: %v", err)
	}
	got := res.(ProposalGetResult)
	if got.Digest != desk.ProposalPayloadDigest(payload) {
		t.Errorf("digest mismatch: %q", got.Digest)
	}
	if got.Proposal.Command == "" {
		t.Error("the row carries no command spelling (proposal.list's, one definition)")
	}
	if got.Preview == nil || got.Preview.Kind != "policy_add" || got.Preview.Target != "agent:boss" {
		t.Fatalf("preview missing or wrong target: %+v", got.Preview)
	}
	var pol policy.Policy
	if err := json.Unmarshal([]byte(payload), &pol); err != nil {
		t.Fatal(err)
	}
	if got.Preview.Describe != policy.Describe(pol) {
		t.Errorf("preview describe %q is not policy.Describe (the one formatter)", got.Preview.Describe)
	}
	if got.Preview.InForce == nil {
		t.Error("preview inForce is null — an empty answer is [] (never a shrug)")
	}
}

func TestProposalCreateAndDiscardUnchanged(t *testing.T) {
	// The S35 door stands byte-identical with the decisions mounted beside
	// it: the same three S35 methods, the same handlers, unchanged behaviour
	// (the conformance fixtures pin the shapes; this pins the SET).
	tbl := decideTable(&decideStub{}, Actor{Kind: "user", ID: "4321"}, nil)
	for _, m := range []MethodName{MethodProposalCreate, MethodProposalList, MethodProposalDiscard} {
		if _, reg := tbl.Lookup(m); !reg {
			t.Errorf("%s vanished from the wired table", m)
		}
	}
	// And the S35 header story still holds on the UNWIRED table: create,
	// list, discard stay registered (they are unconditional); only the
	// decide trio is conditional.
	plain := newDecideTable()
	for _, m := range []MethodName{MethodProposalCreate, MethodProposalList, MethodProposalDiscard} {
		if _, reg := plain.Lookup(m); !reg {
			t.Errorf("%s must stay registered unconditionally (S35 door)", m)
		}
	}
}

// decidePreviewStore is a boundary over in-memory RepoFuncs carrying one
// acknowledged-free rule for agent:boss — enough for the preview's inForce
// half to answer with a real Render() row.
func decidePreviewStore(t *testing.T) *policy.Store {
	t.Helper()
	at := time.Now().UTC()
	stored := []policy.Row{{
		ID: "deny-shell-boss", Type: "command_access",
		TargetKind: "agent", TargetID: "boss", Action: "deny",
		SpecJSON: `{"command":"git push"}`, Source: "operator",
		CreatedAt: at.Format(time.RFC3339Nano),
	}}
	repo := policy.RepoFuncs{
		ListAll: func(context.Context) ([]policy.Row, error) { return stored, nil },
		AcceptanceList: func(context.Context, string, string) ([]policy.AcceptanceRow, error) {
			return nil, nil
		},
	}
	return policy.NewStore(repo)
}
