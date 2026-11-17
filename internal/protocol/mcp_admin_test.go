package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// S46 T3 — the five MCP management reads' protocol-side contract: the
// conditional registration (V13's absent-not-refusing), the params rules
// (identity keys refused, innocent unknowns ignored — the package's own
// strict-decoding discipline), and the seam's forwarding contract (the
// seam's typed *Error untouched, its refusal sentences verbatim under the
// MCP subject's own code, plain errors through as infrastructure).
// The seam itself (row reads, near-names sentences, the probe-cache join)
// belongs to cmd/aeon and is pinned there.

// mcpAdminMethodNames are the five reads RegisterMCPAdmin exists to register.
var mcpAdminMethodNames = []MethodName{
	MethodMCPServers, MethodMCPServer, MethodMCPGrants,
	MethodMCPVocabulary, MethodMCPChanges,
}

// mcpAdminStub is the seam under test: it records the method name each
// call arrived with (the name must be the one BOUND AT REGISTRATION, never
// trusted from params) and answers with whatever the case needs.
type mcpAdminStub struct {
	calls    []string
	result   any
	refusal  string
	typedErr error
	plainErr error
}

func (s *mcpAdminStub) fn() func(ctx context.Context, method string, params json.RawMessage) (any, string, error) {
	return func(_ context.Context, method string, params json.RawMessage) (any, string, error) {
		s.calls = append(s.calls, method)
		if s.plainErr != nil {
			return nil, "", s.plainErr
		}
		if s.typedErr != nil {
			return nil, "", s.typedErr
		}
		if s.refusal != "" {
			return nil, s.refusal, nil
		}
		if s.result != nil {
			return s.result, "", nil
		}
		return map[string]any{"ok": true}, "", nil
	}
}

// wiredMcpAdminTable registers the five over a fresh stub and initializes a
// session, so Dispatch reaches the handlers rather than the gate.
func wiredMcpAdminTable(t *testing.T) (*Table, *mcpAdminStub, string) {
	t.Helper()
	tbl := newTestTable(t)
	stub := &mcpAdminStub{}
	tbl.MCPAdmin = stub.fn()
	if !tbl.RegisterMCPAdmin() {
		t.Fatal("RegisterMCPAdmin returned false with the seam wired — the S46 reads are wired out of existence")
	}
	conn := "c-mcpadmin"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	return tbl, stub, conn
}

func nameIn(names []MethodName, want MethodName) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestMcpAdminAbsentWhenSeamNil is V13 with teeth (the task's named test):
// with a nil seam the five are ABSENT from Methods() and PublicMethods(),
// RegisterMCPAdmin returns false, and a Dispatch reaches JSON-RPC's own
// method-not-found sentinel — NOT a registered-and-refusing door.
// Absent-not-refusing is what lets a client tell "this daemon cannot do
// that" from "this daemon judged and declined" (ErrUnknownMethod vs an
// Aeon code); a registered stub that always answered ErrNotWired would
// collapse the two.
func TestMcpAdminAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.MCPAdmin != nil {
		t.Fatal("the bare test table already has the MCPAdmin seam — the absence test measures nothing")
	}
	if tbl.RegisterMCPAdmin() {
		t.Fatal("RegisterMCPAdmin returned true with a nil seam — registered-and-refusing is V13's forbidden shape")
	}
	all := tbl.Methods()
	public := tbl.PublicMethods()
	for _, name := range mcpAdminMethodNames {
		if _, ok := tbl.Lookup(name); ok {
			t.Fatalf("%s is registered without the seam — nil ⇒ ABSENT (V13)", name)
		}
		if nameIn(all, name) || nameIn(public, name) {
			t.Fatalf("%s appears in the Method(s)/PublicMethods listings without the seam", name)
		}
		if _, err := tbl.Dispatch(context.Background(), "c-none", name, nil); err == nil {
			t.Fatalf("%s dispatched without the seam", name)
		} else if _, ok := err.(ErrUnknownMethod); !ok {
			t.Fatalf("%s: err = %T (%v), want ErrUnknownMethod — absent means the table never heard of it", name, err, err)
		}
	}
	// The hand-built wired table registers them (the same construction the
	// daemon's wiring performs): field set ⇒ five names, correct gates.
	tbl2 := newTestTable(t)
	tbl2.MCPAdmin = (&mcpAdminStub{}).fn()
	if !tbl2.RegisterMCPAdmin() {
		t.Fatal("RegisterMCPAdmin returned false WITH the seam wired")
	}
	for _, name := range mcpAdminMethodNames {
		m, ok := tbl2.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered with the seam wired — the person's management view is gone", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a management read on an unnegotiated connection is unthinkable", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug — these are product surface; what keeps them off the narrow projection is the allow-list that never names them, never a Debug accident", name)
		}
	}
	// And no sixth mcp.* name joined uninvited (the write doors are T5's —
	// their names appear nowhere in the protocol package).
	// 2026-11-08: the two process reads (mcp.processes, mcp.process.logs) ride the
	// same seam registration as the five; the process WRITES register with the
	// person's write doors, never here.
	if got := countPrefix(tbl2, "mcp."); got != len(mcpAdminMethodNames)+2+2 {
		t.Fatalf("registered mcp.* with the seam = %d, want %d (five reads + the two process reads + the S34 status/check pair that predates them) — a write door entered uninvited",
			got, len(mcpAdminMethodNames)+2+2)
	}
}

// TestMcpAdminParamsRefuseIdentityKeys: the five reads carry FILTERS only —
// params never carry an actor or any authorship key, and the package's own
// strict-decoding stand-in (rejectIdentityKeys, the policy-authoring
// discipline) refuses a bag that names one as the shape error (-32602 via
// ErrBadParams), BEFORE the seam is ever called. An honoured actor param
// would be a forged audit identity; an ignored one reads as accepted —
// refusal is the only honest third answer, and it is the one given.
// (Innocent unknown fields stay IGNORED by the package's T3 rule —
// TestNothingCallsDisallowUnknownFields forbids strict decoding; see
// TestMcpAdminInnocentUnknownFieldsAreIgnored.)
func TestMcpAdminParamsRefuseIdentityKeys(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	for _, name := range mcpAdminMethodNames {
		for _, key := range authoringIdentityKeys {
			raw := json.RawMessage(fmt.Sprintf(`{%q: "user:1000"}`, key))
			_, err := tbl.Dispatch(context.Background(), conn, name, raw)
			if err == nil {
				t.Fatalf("%s with an %q param was accepted — the actor is the connection's, never the caller's", name, key)
			}
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("%s with %q: err = %T (%v), want ErrBadParams (-32602 is JSON-RPC's own for a params shape)", name, key, err, err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", key)) {
				t.Errorf("%s with %q: the refusal must NAME the key it refused: %v", name, key, err)
			}
		}
	}
	if len(stub.calls) != 0 {
		t.Fatalf("the seam saw %d calls — an identity-keyed bag must be refused before the seam is consulted: %v", len(stub.calls), stub.calls)
	}
}

// TestMcpAdminInnocentUnknownFieldsAreIgnored is the forward-compat rule
// (T3/D3) applied to the five reads: a filter added by a future build must
// not break this one, and this one must not break that one — so an
// unknown-but-ordinary field is ignored and the request proceeds. Strict
// here means the IDENTITY keys above; strictness about everything would
// turn every additive change into a breaking one, which the package
// forbids by test (TestNothingCallsDisallowUnknownFields).
func TestMcpAdminInnocentUnknownFieldsAreIgnored(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	for _, name := range mcpAdminMethodNames {
		raw := json.RawMessage(`{"somethingFromTheFuture": true}`)
		if _, err := tbl.Dispatch(context.Background(), conn, name, raw); err != nil {
			t.Fatalf("%s with an unknown field was refused: %v — additive evolution is the contract", name, err)
		}
	}
	if len(stub.calls) != len(mcpAdminMethodNames) {
		t.Fatalf("seam calls = %v, want one per method", stub.calls)
	}
}

// TestMcpAdminDispatchesByBoundName: the seam receives the name BOUND AT
// REGISTRATION, never one trusted from params (mcpCall's rule carried) —
// a params member claiming to be a different mcp method changes nothing.
func TestMcpAdminDispatchesByBoundName(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	if _, err := tbl.Dispatch(context.Background(), conn, MethodMCPServers,
		json.RawMessage(`{"method": "mcp.server.remove", "name": "reddit"}`)); err != nil {
		t.Fatalf("mcp.servers dispatch: %v", err)
	}
	if len(stub.calls) != 1 || stub.calls[0] != string(MethodMCPServers) {
		t.Fatalf("seam calls = %v, want exactly [mcp.servers] — the name is bound at registration", stub.calls)
	}
}

// TestMcpAdminRefusalTravelsVerbatimUnderTheMcpCode: a seam refusal string
// becomes ErrMcpRefused with the seam's sentence BYTE for BYTE in `what`
// (policyRefusal's rule carried onto the MCP subject) — under the MCP
// domain's OWN code, not ErrAgentRefused, so a client can tell which
// domain declined (the codes are the taxonomy; ErrMcpRefused's doc owns
// the preference).
func TestMcpAdminRefusalTravelsVerbatimUnderTheMcpCode(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	sentence := `refused: tier filter "publc" is not "public" or "private" — mcp.vocabulary answers the closed sets rather than guessing them`
	stub.refusal = sentence
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPServers, json.RawMessage(`{"tier":"publc"}`))
	var aeon *Error
	if !errors.As(err, &aeon) {
		t.Fatalf("err = %T (%v), want the typed *Error", err, err)
	}
	if aeon.Code != ErrMcpRefused.Code {
		t.Fatalf("code = %d, want %d (ErrMcpRefused) — the MCP domain's refusal is its own taxonomy entry, not the agent-services one", aeon.Code, ErrMcpRefused.Code)
	}
	if aeon.Data.What != sentence {
		t.Errorf("`what` = %q, want the seam's sentence verbatim: %q", aeon.Data.What, sentence)
	}
}

// TestMcpAdminTypedUnknownPassesThroughUntouched: ErrMcpUnknown's near-names
// sentence is the SEAM's — only the seam reads the rows — so the seam
// returns the typed error in `err` and the door ships it UNTOUCHED (the
// ProposalDecider contract). A paraphrase here would be a second authority
// over the fact of what exists.
func TestMcpAdminTypedUnknownPassesThroughUntouched(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	stub.typedErr = ErrMcpUnknown.WithWhat(
		"the mcp.server read named %q, which is not a server this store carries; the nearest names are: %q", "reditt", "reddit")
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPServer, json.RawMessage(`{"name":"reditt"}`))
	var aeon *Error
	if !errors.As(err, &aeon) {
		t.Fatalf("err = %T (%v), want the typed *Error", err, err)
	}
	if aeon.Code != ErrMcpUnknown.Code || aeon.Message != ErrMcpUnknown.Message {
		t.Fatalf("code/message = %d/%q, want %d/%q untouched", aeon.Code, aeon.Message, ErrMcpUnknown.Code, ErrMcpUnknown.Message)
	}
	if !strings.Contains(aeon.Data.What, "reditt") || !strings.Contains(aeon.Data.What, "nearest") {
		t.Errorf("`what` = %q — the seam's near-names sentence must travel byte for byte", aeon.Data.What)
	}
}

// TestMcpAdminPlainErrorIsInfrastructure: a non-typed error from the seam is
// NOT a refusal (DB19's discrimination) — it passes through for Dispatch's
// internal-error handling rather than wearing a refusal code. "The disk
// broke" must never read as "the door declined".
func TestMcpAdminPlainErrorIsInfrastructure(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	boom := errors.New("rows could not be read: disk went away")
	stub.plainErr = boom
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPChanges, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the seam's own error untouched", err)
	}
	var aeon *Error
	if errors.As(err, &aeon) {
		t.Fatalf("an infrastructure failure arrived under a refusal code (%d) — DB19's discrimination broken", aeon.Code)
	}
}

// TestMcpAdminBeltRefusesWhenSeamUnsetAfterRegistration: registration is
// conditional (the nil branch is belt-only), but the seam is assigned after
// construction — so a table mutated underneath a registered method must
// answer ErrNotWired by name, never dereference nil (mcpCall's discipline;
// the panic here would be a daemon dying on a read).
func TestMcpAdminBeltRefusesWhenSeamUnsetAfterRegistration(t *testing.T) {
	tbl, _, conn := wiredMcpAdminTable(t)
	tbl.MCPAdmin = nil
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPVocabulary, nil)
	var aeon *Error
	if !errors.As(err, &aeon) {
		t.Fatalf("err = %T (%v), want ErrNotWired", err, err)
	}
	if aeon.Code != ErrNotWired.Code {
		t.Fatalf("code = %d, want %d (ErrNotWired) — the half-wired shape, named", aeon.Code, ErrNotWired.Code)
	}
	if !strings.Contains(aeon.Data.What, "MCPAdmin") {
		t.Errorf("`what` = %q — the wiring's own name belongs in the refusal, so the operator can find the seam", aeon.Data.What)
	}
}

// TestMCPVocabularyRejectsNonObjectParams: the vocabulary read has no
// params, but the params it IS given must still be an object — `[1]` or
// `"x"` are the caller's shape fault (ErrBadParams, JSON-RPC's -32602),
// not the seam's problem. Absence (nil/null/{}) is legitimate.
func TestMCPVocabularyRejectsNonObjectParams(t *testing.T) {
	tbl, stub, conn := wiredMcpAdminTable(t)
	for _, raw := range []json.RawMessage{json.RawMessage(`[1]`), json.RawMessage(`"x"`), json.RawMessage(`3`)} {
		_, err := tbl.Dispatch(context.Background(), conn, MethodMCPVocabulary, raw)
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("mcp.vocabulary with params %q: err = %T (%v), want ErrBadParams", raw, err, err)
		}
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{}`)} {
		if _, err := tbl.Dispatch(context.Background(), conn, MethodMCPVocabulary, raw); err != nil {
			t.Fatalf("mcp.vocabulary with params %q: %v — a params-less read must accept its absence", raw, err)
		}
	}
	if len(stub.calls) != 3 {
		t.Fatalf("seam calls = %v, want only the three legitimate ones", stub.calls)
	}
}

// TestMcpAdminReadsAreTheOnlyNames proves the MG8 separation protocol-side:
// the five names are READS (no mcp.* WRITE name exists in the protocol
// package at all — T5's doors arrive with their own constants and their
// own registration), and the S34 trust pair (status/check) stayed on
// Table.MCP rather than drifting onto the new seam.
func TestMcpAdminReadsAreTheOnlyNames(t *testing.T) {
	writes := []string{
		"mcp.server.add", "mcp.server.update", "mcp.server.remove",
		"mcp.server.setEnabled", "mcp.grant.declare", "mcp.grant.revoke",
		"mcp.core.set", "mcp.importFile", "mcp.server.inspect",
	}
	for _, name := range writes {
		if _, ok := defaultTableWithoutSeamsForTest(t).Lookup(MethodName(name)); ok {
			t.Fatalf("%s is registered — the write doors are T5's and their names must not exist yet (T3.md: do NOT add any write method here)", name)
		}
	}
}

// defaultTableWithoutSeamsForTest is newTestTable minus any helper state —
// separate name so the intent (the table as a storeless daemon leaves it)
// reads at the assertion.
func defaultTableWithoutSeamsForTest(t *testing.T) *Table {
	t.Helper()
	return newTestTable(t)
}

// --- mcp.importFile (S46 T4): the actor is the peer credential, never a param

func TestMcpImportFileActorStampedFromPeer(t *testing.T) {
	tbl := newTestTable(t)
	var seenKind, seenID string
	var sawActor bool
	tbl.MCPAdmin = func(ctx context.Context, _ string, _ json.RawMessage) (any, string, error) {
		seenKind, seenID, sawActor = MCPActor(ctx)
		return McpImportFileResult{Mode: "dry-run", Outcome: "dry-run"}, "", nil
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	if !tbl.RegisterMCPAdmin() || !tbl.RegisterMCPImportFile() {
		t.Fatal("the wired seam and actor stamp must register both doors")
	}
	conn := "c-import"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	res, err := tbl.Dispatch(context.Background(), conn, MethodMCPImportFile,
		mustJSON(t, map[string]any{"mode": "dry-run"}))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, ok := res.(McpImportFileResult); !ok {
		t.Fatalf("result type %T", res)
	}
	if !sawActor || seenKind != "user" || seenID != "1000" {
		t.Fatalf("the seam saw actor %q/%q (ok=%v) — dispatch must stamp the peer credential into the context", seenKind, seenID, sawActor)
	}
}

func TestMcpImportFileUnresolvablePeerRefused(t *testing.T) {
	// uid 0 (the transport recorded no peer) is refused rather than
	// stamped "user 0" — an unattributable configuration write is the
	// audit row that cannot name who widened what agents may do.
	tbl := newTestTable(t)
	called := false
	tbl.MCPAdmin = func(context.Context, string, json.RawMessage) (any, string, error) {
		called = true
		return nil, "should not reach", nil
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "0"} }
	if !tbl.RegisterMCPAdmin() || !tbl.RegisterMCPImportFile() {
		t.Fatal("registration must succeed with both seams")
	}
	conn := "c-import0"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPImportFile,
		mustJSON(t, map[string]any{"mode": "replace", "path": "/etc/aeon/mcp.json"}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrMcpNoActor.Code {
		t.Fatalf("err = %v, want ErrMcpNoActor (%d)", err, ErrMcpNoActor.Code)
	}
	if called {
		t.Fatal("the seam ran despite the refused peer — the write must stop at the dispatch edge")
	}
}

func TestMcpImportFileActorParamIsShapeError(t *testing.T) {
	tbl := newTestTable(t)
	stub := &mcpAdminStub{}
	tbl.MCPAdmin = stub.fn()
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	if !tbl.RegisterMCPAdmin() || !tbl.RegisterMCPImportFile() {
		t.Fatal("registration")
	}
	conn := "c-importforged"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPImportFile,
		mustJSON(t, map[string]any{"mode": "replace", "actor": "root"}))
	var bad ErrBadParams
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v, want the params shape error (-32602 family)", err)
	}
	for _, call := range stub.calls {
		if call == string(MethodMCPImportFile) {
			t.Fatal("params carrying an actor reached the seam — the identity keys are refused as a shape error (PM7.3 carried to MCP writes)")
		}
	}
}

func TestMcpImportFileAbsentWithoutActorStamp(t *testing.T) {
	// V13 applied to the write door: seam wired but NO actor stamp ⇒ the
	// five reads register and the write door stays ABSENT (absent, never a
	// refusing half-wired door).
	tbl := newTestTable(t)
	stub := &mcpAdminStub{}
	tbl.MCPAdmin = stub.fn()
	if !tbl.RegisterMCPAdmin() {
		t.Fatal("reads must register on the seam alone")
	}
	if tbl.RegisterMCPImportFile() {
		t.Fatal("the write door registered without an actor stamp")
	}
	for _, n := range tbl.Methods() {
		if n == MethodMCPImportFile {
			t.Fatal("mcp.importFile is listed although unattributable writes cannot be stamped")
		}
	}
}

// --- S46 T5 write edges -------------------------------------------------------------------

func wiredMcpWriteTable(t *testing.T, actor Actor) (*Table, *mcpAdminStub, string) {
	t.Helper()
	tbl := newTestTable(t)
	stub := &mcpAdminStub{}
	tbl.MCPAdmin = stub.fn()
	tbl.ActorFor = func(context.Context, *Session) Actor { return actor }
	if !tbl.RegisterMCPAdmin() || !tbl.RegisterMCPWrites() {
		t.Fatal("the wired seam must register the reads AND the write doors")
	}
	conn := "c-mcpwrite"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	return tbl, stub, conn
}

func TestMcpWriteEdgeRefusesIdentityKeys(t *testing.T) {
	tbl, stub, conn := wiredMcpWriteTable(t, Actor{Kind: "user", ID: "1000"})
	forged := []map[string]any{
		{"name": "gh", "tier": "public", "actor": "root"},
		{"name": "gh", "tier": "public", "origin": "system"},
		{"name": "gh", "tier": "public", "id": 7},
		{"name": "gh", "tier": "public", "createdAt": "2020-01-01T00:00:00Z"},
		{"agent": "a", "qualified": "s.t", "actorKind": "operator"},
	}
	for i, params := range forged {
		_, err := tbl.Dispatch(context.Background(), conn, MethodMCPServerAdd, mustJSON(t, params))
		var bad ErrBadParams
		if i < 4 && !errors.As(err, &bad) {
			t.Errorf("forged %d: err = %v, want the params shape refusal", i, err)
		}
	}
	for _, call := range stub.calls {
		if call == string(MethodMCPServerAdd) || call == string(MethodMCPGrantDeclare) {
			t.Fatal("a params-forged identity reached the seam — the actor, the origin and the timestamps are stamped by the daemon, never named (MG5)")
		}
	}
}

func TestMcpWriteEdgeRefusesUnresolvablePeer(t *testing.T) {
	tbl, stub, conn := wiredMcpWriteTable(t, Actor{Kind: "user", ID: "0"})
	_, err := tbl.Dispatch(context.Background(), conn, MethodMCPServerAdd,
		mustJSON(t, map[string]any{"name": "gh", "tier": "public", "transport": map[string]any{"type": "stdio", "command": "gh"}}))
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrMcpNoActor.Code {
		t.Fatalf("err = %v, want ErrMcpNoActor (%d)", err, ErrMcpNoActor.Code)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("uid 0 reached the seam (%v) — the write must stop at the dispatch edge", stub.calls)
	}
}

func TestMcpWriteEdgeStampsThePeer(t *testing.T) {
	tbl := newTestTable(t)
	var kind, id string
	var seen bool
	tbl.MCPAdmin = func(ctx context.Context, _ string, _ json.RawMessage) (any, string, error) {
		kind, id, seen = MCPActor(ctx)
		return MCPWriteResult{Changed: true}, "", nil
	}
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	if !tbl.RegisterMCPAdmin() || !tbl.RegisterMCPWrites() {
		t.Fatal("registration")
	}
	conn := "c-mcpwrite-stamp"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	if _, err := tbl.Dispatch(context.Background(), conn, MethodMCPCoreSet,
		mustJSON(t, map[string]any{"agent": "a", "core": []string{"read"}})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !seen || kind != "user" || id != "1000" {
		t.Fatalf("seam saw actor %q/%q (ok=%v) — dispatch stamps the peer credential for writes too", kind, id, seen)
	}
}

func TestMcpWriteDoorsAbsentWhenSeamNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.RegisterMCPWrites() {
		t.Fatal("write doors registered without the seam")
	}
	for _, n := range tbl.Methods() {
		switch n {
		case MethodMCPServerAdd, MethodMCPServerUpdate, MethodMCPServerRemove,
			MethodMCPServerSetEnabled, MethodMCPServerShare, MethodMCPGrantDeclare, MethodMCPGrantRevoke, MethodMCPCoreSet:
			t.Fatalf("write door %s is listed with no seam — absent, never refusing", n)
		}
	}
}
