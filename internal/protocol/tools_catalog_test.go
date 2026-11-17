package protocol

// Tests for S47 T5 — the person's tools catalog doors (`tools.list` ·
// `tools.get` · `tools.byAgent` · `tools.status`).
//
// What this file measures is the DOOR, not the catalog: the daemon's own suite
// (`internal/daemon/tools_catalog_test.go`) answers what the four reads SAY.
// Here the question is the shape of the seam: that a nil seam leaves the names
// absent rather than registered-and-refusing (V13), that the four are product
// surface and not debug surface (so what keeps them off the narrow projection is
// the allow-list that never names them, not a Debug accident), that params are
// filters and a subject and never an identity (TC9/MC2), that the seam's typed
// refusals travel untouched (only the seam reads the rows and the registry, so
// only it can name the nearest names), and that the list's empty-table sentence
// belongs to the layer that saw the filter.
//
// `tools.grants` (S34) is pinned here too — `TestExistingToolsGrantsUnchanged` —
// because the slice adds a catalog beside a door that already exists, and a
// change to the older one must be a decision somebody made on purpose.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/toolcatalog"
)

// --- the stub ----------------------------------------------------------------

// toolsCatalogStub is the seam the doors forward to, in the `mcpAdminStub` shape:
// it records what it was asked for, and the test decides what it answers — a
// result, the seam's typed `*Error`, or a plain infrastructure error.
type toolsCatalogStub struct {
	calls    []string // the bound method names, in order
	listArgs []ToolsListFilter
	getArgs  []string
	agentArg []string

	tools   []ToolEntry
	get     ToolsGetResult
	byAgent ToolsByAgentResult
	status  ToolsStatusResult
	vocab   ToolsVocabularyResult

	typedErr error // a *Error from the seam (the near-names refusal)
	plainErr error // infrastructure (DB19's other half)
}

func (s *toolsCatalogStub) ToolsList(_ context.Context, f ToolsListFilter) ([]ToolEntry, error) {
	s.calls = append(s.calls, string(MethodToolsList))
	s.listArgs = append(s.listArgs, f)
	if s.plainErr != nil {
		return nil, s.plainErr
	}
	if s.typedErr != nil {
		return nil, s.typedErr
	}
	if s.tools != nil {
		return s.tools, nil
	}
	return []ToolEntry{}, nil
}

func (s *toolsCatalogStub) ToolsGet(_ context.Context, name string) (ToolsGetResult, error) {
	s.calls = append(s.calls, string(MethodToolsGet))
	s.getArgs = append(s.getArgs, name)
	if s.plainErr != nil {
		return ToolsGetResult{}, s.plainErr
	}
	if s.typedErr != nil {
		return ToolsGetResult{}, s.typedErr
	}
	if s.get.Tool.Name != "" || s.get.Agents != nil || s.get.Policies != nil {
		return s.get, nil
	}
	return ToolsGetResult{Tool: ToolEntry{Name: name, Source: ToolsSourceSystem, Schema: json.RawMessage(`{}`)}}, nil
}

func (s *toolsCatalogStub) ToolsByAgent(_ context.Context, agent string) (ToolsByAgentResult, error) {
	s.calls = append(s.calls, string(MethodToolsByAgent))
	s.agentArg = append(s.agentArg, agent)
	if s.plainErr != nil {
		return ToolsByAgentResult{}, s.plainErr
	}
	if s.typedErr != nil {
		return ToolsByAgentResult{}, s.typedErr
	}
	if s.byAgent.Agent != "" || s.byAgent.Tools != nil {
		return s.byAgent, nil
	}
	return ToolsByAgentResult{Agent: agent, Dispatchable: true}, nil
}

func (s *toolsCatalogStub) ToolsStatus(_ context.Context) (ToolsStatusResult, error) {
	s.calls = append(s.calls, string(MethodToolsStatus))
	if s.plainErr != nil {
		return ToolsStatusResult{}, s.plainErr
	}
	if s.typedErr != nil {
		return ToolsStatusResult{}, s.typedErr
	}
	if s.status.Seed.Set != "" || s.status.Drift != nil || s.status.Counts.System != 0 {
		return s.status, nil
	}
	return ToolsStatusResult{Seed: ToolsSeedState{Set: "tools.core", State: "current"}}, nil
}

// ToolsVocabulary answers the vocabulary door (S47 T10) the same way: recorded,
// pass-through typed/plain errors, and a canned answer of its own when the test
// did not say otherwise. The canned words are the shape of the real thing — the
// door itself validates nothing, because the seam that owns the catalog owns the
// words (a second vocabulary check at the wire would be the second definition
// TC3 exists to prevent).
func (s *toolsCatalogStub) ToolsVocabulary(_ context.Context) (ToolsVocabularyResult, error) {
	s.calls = append(s.calls, string(MethodToolsVocabulary))
	if s.plainErr != nil {
		return ToolsVocabularyResult{}, s.plainErr
	}
	if s.typedErr != nil {
		return ToolsVocabularyResult{}, s.typedErr
	}
	if s.vocab.Capabilities != nil || s.vocab.Families != nil || s.vocab.Sources != nil || s.vocab.RerunClasses != nil {
		return s.vocab, nil
	}
	return ToolsVocabularyResult{
		Capabilities: []ToolCapabilityNote{{Name: "workspace_read", Meaning: "Reads bytes under the step's workspace root through the one resolver."}},
		Families:     []string{"files"},
		Sources:      []string{ToolsSourceSystem},
		RerunClasses: []string{"safe"},
	}, nil
}

// wiredToolsCatalog returns a table with the catalog doors registered and the seam
// answering what the test asks it to.
func wiredToolsCatalog(t *testing.T, stub *toolsCatalogStub) (*Table, string) {
	t.Helper()
	tbl := newTestTable(t)
	tbl.ToolsCatalog = stub
	if !tbl.RegisterToolsCatalog() {
		t.Fatal("RegisterToolsCatalog returned false with the seam wired — the S47 catalog is wired out of existence")
	}
	conn := "c-toolscatalog"
	initialized(t, tbl, conn, PROTOCOL_VERSION)
	return tbl, conn
}

func dispatchToolsResult[T any](t *testing.T, tbl *Table, conn string, name MethodName, params json.RawMessage) T {
	t.Helper()
	raw, err := tbl.Dispatch(context.Background(), conn, name, params)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out T
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: the answer does not fit %T: %v", name, out, err)
	}
	return out
}

// testToolsCatalogService is the exact-set listing's stub (handshake_test.go
// wires it before `TestTheMethodTableHasExactlyTheExpectedMethods` measures the
// table): the five names stand in that list BECAUSE the door is wired, and the
// absent side is pinned by TestToolsMethodsAbsentWhenServiceNil. It answers the
// empty shape because the listing measures registration, not content — the
// content tests below use toolsCatalogStub, which records what it was asked.
type testToolsCatalogService struct{}

func (testToolsCatalogService) ToolsList(context.Context, ToolsListFilter) ([]ToolEntry, error) {
	return []ToolEntry{}, nil
}

func (testToolsCatalogService) ToolsGet(context.Context, string) (ToolsGetResult, error) {
	return ToolsGetResult{}, nil
}

func (testToolsCatalogService) ToolsByAgent(context.Context, string) (ToolsByAgentResult, error) {
	return ToolsByAgentResult{}, nil
}

func (testToolsCatalogService) ToolsStatus(context.Context) (ToolsStatusResult, error) {
	return ToolsStatusResult{}, nil
}

func (testToolsCatalogService) ToolsVocabulary(context.Context) (ToolsVocabularyResult, error) {
	return ToolsVocabularyResult{}, nil
}

var toolsCatalogMethodNames = []MethodName{
	MethodToolsList, MethodToolsGet, MethodToolsByAgent, MethodToolsStatus, MethodToolsVocabulary,
}

func toolEntryForTest(name, family, source string) ToolEntry {
	return ToolEntry{
		Name: name, Family: family, Source: source, Description: "the " + name + " tool",
		Schema: json.RawMessage(`{"type":"object"}`), SchemaHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		RerunClass: "safe", Capabilities: []string{"workspace_read"}, Governed: []string{"tool_access"},
		Conditional: false, Available: true, Classified: source == ToolsSourceMCP,
		Version: 1, Origin: "system", RowCurrent: true,
	}
}

// --- V13: the seam is the configuration speaking --------------------------------

// TestToolsMethodsAbsentWhenServiceNil is V13 on the four catalog doors: without
// the seam the names are ABSENT — Register returns false, nothing is registered,
// and a client gets JSON-RPC's own -32601. A registered-and-refusing door would
// make "this build has no catalog" indistinguishable from "the catalog refused",
// and the two need different answers: one is a fact about the build, the other a
// fact about a decision.
//
// The wired half of the same test pins the two properties the narrow projection
// depends on: RequiresInitialize true (a catalog read on an unnegotiated
// connection is unthinkable) and Debug false (these are product surface; what
// keeps them off HTTP and MCP-stdio is the allow-list that never names them —
// T5's guard amendment walks that claim, and a Debug flag would let the doors
// disappear from the app by an accident nobody reviewed).
func TestToolsMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := newTestTable(t)
	if tbl.ToolsCatalog != nil {
		t.Fatal("the bare test table already has the ToolsCatalog seam — the absence test measures nothing")
	}
	if tbl.RegisterToolsCatalog() {
		t.Fatal("RegisterToolsCatalog returned true with a nil seam — registered-and-refusing is V13's forbidden shape")
	}
	all, public := tbl.Methods(), tbl.PublicMethods()
	for _, name := range toolsCatalogMethodNames {
		if _, ok := tbl.Lookup(name); ok {
			t.Fatalf("%s is registered without the seam — nil ⇒ ABSENT (V13)", name)
		}
		if nameIn(all, name) || nameIn(public, name) {
			t.Fatalf("%s appears in Methods()/PublicMethods() without the seam", name)
		}
		if _, err := tbl.Dispatch(context.Background(), "c-none", name, nil); err == nil {
			t.Fatalf("%s dispatched without the seam", name)
		} else if _, ok := err.(ErrUnknownMethod); !ok {
			t.Fatalf("%s: err = %T (%v), want ErrUnknownMethod — absent means the table never heard of it", name, err, err)
		}
	}

}

// --- the params are filters and a subject, never an identity --------------------

// TestToolsParamsRefuseIdentityKeys is TC9/MC2 at the parse edge: the four doors
// take FILTERS (`family`, `source`, `capability`, `available`, `q`) and a SUBJECT
// (`agent`, `name`) — never a caller. An agent cannot ask this door about another
// agent because there is no field in which to say so; its own view is T7's
// `tools_mine`/`tool_show`, a separate path through the same pure catalog with the
// identity the daemon stamps.
//
// The refusal is shape (`ErrBadParams`, -32602) and it happens BEFORE the seam: a
// door that forwarded a bag naming an actor and refused afterwards would let the
// seam's own logic see an identity it must never consult.
func TestToolsParamsRefuseIdentityKeys(t *testing.T) {
	stub := &toolsCatalogStub{}
	tbl, conn := wiredToolsCatalog(t, stub)
	for _, key := range authoringIdentityKeys {
		for _, tc := range []struct {
			name   MethodName
			params json.RawMessage
		}{
			{MethodToolsList, json.RawMessage(`{"` + key + `":"atlas","family":"workspace"}`)},
			{MethodToolsGet, json.RawMessage(`{"` + key + `":"atlas","name":"read"}`)},
			{MethodToolsByAgent, json.RawMessage(`{"` + key + `":"atlas","agent":"atlas"}`)},
			{MethodToolsStatus, json.RawMessage(`{"` + key + `":"atlas"}`)},
			{MethodToolsVocabulary, json.RawMessage(`{"` + key + `":"atlas"}`)},
		} {
			_, err := tbl.Dispatch(context.Background(), conn, tc.name, tc.params)
			if err == nil {
				t.Fatalf("%s with %q in params was accepted — an identity param is a caller claiming to be somebody", tc.name, key)
			}
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Fatalf("%s with %q: err = %T (%v), want ErrBadParams (-32602 is JSON-RPC's own for a params shape)", tc.name, key, err, err)
			}
			if !strings.Contains(err.Error(), `"`+key+`"`) {
				t.Errorf("%s with %q: the refusal must NAME the key it refused: %v", tc.name, key, err)
			}
		}
	}
	if len(stub.calls) != 0 {
		t.Fatalf("the seam saw %d calls (%v) — an identity bag must be refused before it, not after", len(stub.calls), stub.calls)
	}
}

// TestToolsInnocentUnknownFieldsAreIgnored is the other half of the same rule:
// the package forbids `DisallowUnknownFields`, so a client that sends a field the
// Go type does not declare is not punished for it. Only the identity keys are
// refused, because those are not unknown-by-accident but claimed-by-intent.
func TestToolsInnocentUnknownFieldsAreIgnored(t *testing.T) {
	stub := &toolsCatalogStub{tools: []ToolEntry{toolEntryForTest("read", "workspace", ToolsSourceSystem)}}
	tbl, conn := wiredToolsCatalog(t, stub)
	res := dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList,
		json.RawMessage(`{"family":"workspace","clientVersion":"9.9","observedAt":"2026-11-02T12:00:00Z"}`))
	if len(res.Tools) != 1 {
		t.Fatalf("tools = %v, want the one entry the seam answered", res.Tools)
	}
}

// TestToolsListParamsArriveAsTheFilterTheyClaim is TC10's "filters answered
// daemon-side" seen from the door: the five optional filters reach the seam with
// their three states intact — absent is nil, present is a value, and an empty
// `q` is a pointer to "" rather than a nil, because "my search matched nothing"
// and "I did not search" are different answers (the first earns the table's
// sentence, the second must never claim the build has no tools).
func TestToolsListParamsArriveAsTheFilterTheyClaim(t *testing.T) {
	stub := &toolsCatalogStub{tools: []ToolEntry{toolEntryForTest("read", "workspace", ToolsSourceSystem)}}
	tbl, conn := wiredToolsCatalog(t, stub)

	_ = dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList, nil)
	got := stub.listArgs[0]
	if got.Family != nil || got.Source != nil || got.Capability != nil || got.Available != nil || got.Q != nil {
		t.Fatalf("an absent filter arrived as %#v — absent must be nil, not the zero value", got)
	}
	if got.AnyFilter() {
		t.Fatal("AnyFilter() is true with every field absent — the unfiltered-empty case would be told it filtered")
	}

	_ = dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList, json.RawMessage(`{"q":"","available":true}`))
	got = stub.listArgs[1]
	if got.Q == nil || *got.Q != "" {
		t.Fatalf("a present-and-empty q arrived as %v — the empty filter's own answer was lost", got.Q)
	}
	if got.Available == nil || *got.Available != true {
		t.Fatalf("available = %v, want a pointer to true", got.Available)
	}
	if !got.AnyFilter() {
		t.Fatal("AnyFilter() is false with two filters present")
	}
}

// --- the empty table's sentence belongs to the door ------------------------------

// TestToolsListStatementIsOnlyForAFilteredEmpty is the three states of an empty
// list, the `policy.changes`/`mcp.changes` discipline carried onto this door: no
// filter and empty answers with no sentence (that would claim the build contains
// no tools at all); a filter and empty answers with the table's sentence, byte for
// byte, pinned where the wire shape is defined.
func TestToolsListStatementIsOnlyForAFilteredEmpty(t *testing.T) {
	stub := &toolsCatalogStub{tools: []ToolEntry{}}
	tbl, conn := wiredToolsCatalog(t, stub)

	unfiltered := dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList, nil)
	if unfiltered.Statement != "" {
		t.Errorf("an unfiltered empty list carries %q — that sentence belongs to a filter that matched nothing, and printing it here claims the build has no tools", unfiltered.Statement)
	}
	if unfiltered.Tools == nil {
		t.Error("tools is null; an empty list is [] and never null (the settings-list contract)")
	}

	filtered := dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList, json.RawMessage(`{"q":"zzz"}`))
	if filtered.Statement != ToolsListNoMatchStatement {
		t.Errorf("statement = %q, want %q byte for byte", filtered.Statement, ToolsListNoMatchStatement)
	}

	// And a NON-empty answer carries none, whatever the filter: the sentence is
	// the empty table's, not a decoration on every answer.
	stub.tools = []ToolEntry{toolEntryForTest("read", "workspace", ToolsSourceSystem)}
	full := dispatchToolsResult[ToolsListResult](t, tbl, conn, MethodToolsList, json.RawMessage(`{"q":"read"}`))
	if full.Statement != "" {
		t.Errorf("a non-empty answer carries %q", full.Statement)
	}
}

// --- the seam's refusals travel untouched ----------------------------------------

// TestToolsTypedRefusalTravelsUntouched pins the one-formatter rule on this door:
// the seam read the registry and the rows, so the nearest-names sentence is its
// fact, and this package ships it verbatim under its own code. A paraphrase here
// would be a second authority over a fact it cannot see.
func TestToolsTypedRefusalTravelsUntouched(t *testing.T) {
	want := ErrToolUnknown.WithWhat("the tools.get read named %q, which is not a tool this build implements; the nearest names are: %q", "reed", "read")
	stub := &toolsCatalogStub{typedErr: want}
	tbl, conn := wiredToolsCatalog(t, stub)

	_, err := tbl.Dispatch(context.Background(), conn, MethodToolsGet, json.RawMessage(`{"name":"reed"}`))
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %T (%v), want a *Error", err, err)
	}
	if e.Code != want.Code || e.Message != want.Message {
		t.Fatalf("code/message = %d/%q, want %d/%q", e.Code, e.Message, want.Code, want.Message)
	}
	if e.Data.What != want.Data.What || e.Data.Means != want.Data.Means || e.Data.Todo != want.Data.Todo {
		t.Fatalf("the three parts were rewritten on the way out:\n got %q\nwant %q", e.Data.What, want.Data.What)
	}
}

// TestToolsUnknownAgentCarriesItsOwnCode is the sibling: the two S47 codes are
// distinct facts and a client branches on the number, not the prose — a missing
// tool is a typo in a name the catalog could correct, a missing agent is a name
// nobody stored.
func TestToolsUnknownAgentCarriesItsOwnCode(t *testing.T) {
	stub := &toolsCatalogStub{typedErr: ErrToolAgentUnknown.WithWhat("the tools.byAgent read named %q; the known agents are: %q", "atlas", "sentry")}
	tbl, conn := wiredToolsCatalog(t, stub)
	_, err := tbl.Dispatch(context.Background(), conn, MethodToolsByAgent, json.RawMessage(`{"agent":"atlas"}`))
	e, ok := err.(*Error)
	if !ok || e.Code != ErrToolAgentUnknown.Code {
		t.Fatalf("err = %T (%v), want the %d code", err, err, ErrToolAgentUnknown.Code)
	}
	if e.Code == ErrToolUnknown.Code {
		t.Fatal("the two S47 codes collapsed into one")
	}
}

// TestToolsPlainErrorIsInfrastructure is DB19's discrimination carried onto the
// seam: "the rows could not be read" is not a refusal, and the door must not turn
// it into one — a caller that saw a refusal code would tell the person nothing is
// granted, when the truth is that nothing could be read.
func TestToolsPlainErrorIsInfrastructure(t *testing.T) {
	boom := errors.New("tools catalog: the tool rows could not be read: no such table")
	stub := &toolsCatalogStub{plainErr: boom}
	tbl, conn := wiredToolsCatalog(t, stub)
	_, err := tbl.Dispatch(context.Background(), conn, MethodToolsList, nil)
	if err == nil {
		t.Fatal("a store failure dispatched as a result — V11 would read an unreadable catalog as an empty one")
	}
	if e, ok := err.(*Error); ok && (e.Code == ErrToolUnknown.Code || e.Code == ErrToolAgentUnknown.Code) {
		t.Fatalf("a store failure was rendered as a domain refusal (%d) — an unreadable world is not a denied one", e.Code)
	}
}

// --- the params' own shape -------------------------------------------------------

// TestToolsStatusRejectsNonObjectParams keeps the params-less door honest: it
// takes nothing, and a params member that is not an object (`[1]`, `"x"`) is the
// caller's shape fault and takes JSON-RPC's -32602, not an Aeon code. Absent and
// `{}` both work, because there is nothing to name.
func TestToolsStatusRejectsNonObjectParams(t *testing.T) {
	stub := &toolsCatalogStub{}
	tbl, conn := wiredToolsCatalog(t, stub)
	if _, err := tbl.Dispatch(context.Background(), conn, MethodToolsStatus, json.RawMessage(`[1]`)); err == nil {
		t.Fatal("array params were accepted")
	} else {
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("err = %T (%v), want ErrBadParams", err, err)
		}
	}
	_ = dispatchToolsResult[ToolsStatusResult](t, tbl, conn, MethodToolsStatus, nil)
	_ = dispatchToolsResult[ToolsStatusResult](t, tbl, conn, MethodToolsStatus, json.RawMessage(`{}`))
	if got := stub.calls[len(stub.calls)-1]; got != string(MethodToolsStatus) {
		t.Fatalf("the seam saw %q, want %q", got, MethodToolsStatus)
	}

	// S47 T10: the vocabulary door takes nothing for the same reason, so it is
	// held to the same three answers — `[1]` is the caller's shape fault, absent
	// and `{}` both reach the seam.
	if _, err := tbl.Dispatch(context.Background(), conn, MethodToolsVocabulary, json.RawMessage(`"x"`)); err == nil {
		t.Fatal("string params were accepted by the vocabulary door")
	} else {
		var bad ErrBadParams
		if !errors.As(err, &bad) {
			t.Fatalf("vocabulary string params: err = %T (%v), want ErrBadParams", err, err)
		}
	}
	_ = dispatchToolsResult[ToolsVocabularyResult](t, tbl, conn, MethodToolsVocabulary, nil)
	_ = dispatchToolsResult[ToolsVocabularyResult](t, tbl, conn, MethodToolsVocabulary, json.RawMessage(`{}`))
	if got := stub.calls[len(stub.calls)-1]; got != string(MethodToolsVocabulary) {
		t.Fatalf("the seam saw %q, want %q", got, MethodToolsVocabulary)
	}
}

// TestToolsDispatchesByBoundName is the forwarding discipline the MCP doors use:
// the method dispatched is the one BOUND AT REGISTRATION, so nothing in params can
// redirect a call to another door.
func TestToolsDispatchesByBoundName(t *testing.T) {
	stub := &toolsCatalogStub{}
	tbl, conn := wiredToolsCatalog(t, stub)
	_ = dispatchToolsResult[ToolsGetResult](t, tbl, conn, MethodToolsGet,
		json.RawMessage(`{"name":"read","method":"tools.status"}`))
	if len(stub.calls) != 1 || stub.calls[0] != string(MethodToolsGet) {
		t.Fatalf("the seam saw %v, want exactly [%s]", stub.calls, MethodToolsGet)
	}
}

// TestToolsResultsCarryTheirShapes is the wire shape itself: the per-agent column
// is every agent that EXISTS (granted or not) and the policy rows carry
// `describe` — the sentence S45's one describer produced, shipped verbatim, so the
// app never assembles a rule sentence from parts.
func TestToolsResultsCarryTheirShapes(t *testing.T) {
	stub := &toolsCatalogStub{
		get: ToolsGetResult{
			Tool:   toolEntryForTest("write", "workspace", ToolsSourceSystem),
			Agents: []AgentGrantRow{{Tool: "write", Agent: "atlas", State: "granted_allowed", Reason: "allow · operator · agent:atlas", GrantedBy: []string{"default"}, Dispatchable: true}},
			Policies: []PolicyRef{{ID: "p-1", Action: "allow", Source: "operator", Target: "agent:atlas",
				ToolName: "write", Describe: "allow · tool_access · write · agent:atlas", Expired: false,
				Link: Link{Kind: "policy", ID: "p-1"}}},
		},
	}
	tbl, conn := wiredToolsCatalog(t, stub)
	res := dispatchToolsResult[ToolsGetResult](t, tbl, conn, MethodToolsGet, json.RawMessage(`{"name":"write_file"}`))
	if res.Tool.Name != "write" {
		t.Fatalf("tool = %q", res.Tool.Name)
	}
	if len(res.Agents) != 1 || res.Agents[0].Reason == "" {
		t.Fatalf("the per-agent column lost the engine's sentence: %+v", res.Agents)
	}
	if len(res.Policies) != 1 || res.Policies[0].Describe == "" {
		t.Fatalf("the policy rows lost `describe`: %+v", res.Policies)
	}
	if res.Policies[0].Link.Kind != "policy" || res.Policies[0].Link.ID != "p-1" {
		t.Errorf("the policy row carries no data link: %+v — navigation is the daemon's to name, not the app's to build", res.Policies[0].Link)
	}
	if res.MCP != nil {
		t.Error("a system tool carries an MCP link — there is no server to link")
	}
	if stub.getArgs[0] != "write_file" {
		t.Fatalf("the seam got %q; alias resolution is the seam's, not the door's", stub.getArgs[0])
	}
}

// TestToolsStatusDriftIsNeverNull is the healthy-case shape: an empty drift list is
// `[]`, because a panel that must nil-check "no drift" cannot tell an empty report
// from a report that never ran — which is precisely the distinction TC11 is about.
func TestToolsStatusDriftIsNeverNull(t *testing.T) {
	stub := &toolsCatalogStub{status: ToolsStatusResult{
		Seed:   ToolsSeedState{Set: "tools.core", AvailableVersion: 3, AppliedVersion: 3, State: "current"},
		Counts: ToolsCounts{System: 68, MCP: 3, ConditionalUnwired: 2},
	}}
	tbl, conn := wiredToolsCatalog(t, stub)
	raw, err := tbl.Dispatch(context.Background(), conn, MethodToolsStatus, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(raw)
	var probe struct {
		Drift json.RawMessage `json:"drift"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatal(err)
	}
	if string(probe.Drift) != "[]" {
		t.Fatalf("drift = %s, want [] (never null)", probe.Drift)
	}
}

// --- the older door, pinned -------------------------------------------------------

// TestExistingToolsGrantsUnchanged keeps S34's promise while S47 builds beside it:
// `tools.grants` is not this slice's door, its handler is not this slice's handler,
// and its conformance fixture has not moved. The whole point of pinning it is that
// "improve the grants read while you are in there" is a decision somebody has to
// make in a diff, not a side effect of a neighbour.
func TestExistingToolsGrantsUnchanged(t *testing.T) {
	fixture, err := os.ReadFile("conformance/agents/tools-grants.json")
	if err != nil {
		t.Fatalf("the tools.grants fixture is gone (%v) — S47's slice may add doors, not delete theirs", err)
	}
	if !json.Valid(fixture) {
		t.Fatal("the tools.grants fixture is not valid JSON")
	}
	var doc struct {
		Method   string          `json:"method"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(fixture, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Method != string(MethodToolsGrants) {
		t.Fatalf("fixture method = %q, want %q", doc.Method, MethodToolsGrants)
	}
	// The handler is still agent_roster.go's, not a copy in this slice's file: the
	// seam it forwards to is still the func value, and the four catalog doors are
	// the only thing tools_catalog.go registers.
	if tbl := (Table{}); tbl.ToolGrants != nil {
		t.Fatal("Table.ToolGrants is not a func seam any more — S34's shape moved")
	}
	var src []byte
	if src, err = os.ReadFile("tools_catalog.go"); err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"t.register(&Method{Name: MethodToolsGrants", "handleToolsGrants"} {
		if containsString(string(src), banned) {
			t.Errorf("tools_catalog.go contains %q — the S34 door is registered by NewTable and handled in agent_roster.go, and a second copy is a second truth", banned)
		}
	}
}

func containsString(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// --- the vocabulary the wire carries ----------------------------------------------

// TestToolsWireVocabularyIsClosed keeps the closed word sets on the door honest: a
// fifth state, a third source or a fifth drift word would be a value the panel has
// no rendering for and the CLI no branch for, and both packages would read it as
// the nearest known one — which is how "unknown" becomes a confident wrong answer
// (P31).
func TestToolsWireVocabularyIsClosed(t *testing.T) {
	if got := []string{ToolsSourceSystem, ToolsSourceMCP}; !reflect.DeepEqual(got, []string{"system", "mcp"}) {
		t.Fatalf("sources = %v, want exactly system and mcp", got)
	}
	if got := []string{ToolsDriftCurrent, ToolsDriftMissingRow, ToolsDriftRetired, ToolsDriftDiffers}; !reflect.DeepEqual(got, []string{"current", "missing_row", "retired", "differs"}) {
		t.Fatalf("drift words = %v, want the four TC11 names", got)
	}
	if ToolsListNoMatchStatement != "no tools match this filter." {
		t.Fatalf("the empty-table sentence moved: %q", ToolsListNoMatchStatement)
	}
	// The states the per-agent column carries are `internal/toolcatalog`'s five,
	// and this package adds none: a wire twin of the vocabulary would be a second
	// definition to drift.
	for _, s := range []string{"not_granted", "granted_allowed", "granted_ask", "granted_denied", "granted_no_rule"} {
		var row AgentGrantRow
		row.State = toolcatalog.State(s)
		if !row.State.Valid() {
			t.Errorf("state %q is not in the five — the wire carries a word the catalog does not have", s)
		}
	}
}

// TestToolsCatalogMethodsNotDebugRequireInit is the wired half of the same coin,
// named apart because these two flags are what keeps the four doors out of the
// narrow projection: `RequiresInitialize` true (a catalog read on an unnegotiated
// connection is unthinkable) and `Debug` false (they are PRODUCT surface — what
// keeps them off HTTP and MCP-stdio is the allow-list that never names them, which
// T5's guard amendment walks; a Debug flag would let them vanish from the app by an
// accident nobody reviewed). Alongside them: the SET — four reads plus S34's
// `tools.grants` plus S47 T7's two own-view twins (`tools.mine.list`,
// `tools.mine.show`) is seven `tools.` names, checked BY NAME rather than by
// count, because a bare count is exactly how a write door in TC1's clothes gets
// waved in — and the belt, which answers the wiring's own name rather than
// dereferencing a seam that was unset after registration.
func TestToolsCatalogMethodsNotDebugRequireInit(t *testing.T) {
	stub := &toolsCatalogStub{}
	tbl2, conn := wiredToolsCatalog(t, stub)
	for _, name := range toolsCatalogMethodNames {
		m, ok := tbl2.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered with the seam wired — the person's catalog is gone", name)
		}
		if !m.RequiresInitialize {
			t.Errorf("%s does not require Initialize — a catalog read on an unnegotiated connection is unthinkable", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug — these are product surface; what keeps them off the narrow projection is the allow-list that never names them, never a Debug accident", name)
		}
	}
	// No uninvited name: the catalog is five READS (TC1) plus S34's `tools.grants`
	// plus S47 T7's two own-view twins — eight `tools.` names and exactly those.
	// (S55's precedent for amending a count by NAME rather than by loosening it:
	// a guard that only counts starts passing whatever arrives, which is precisely
	// how a write door in TC1's clothes would be welcomed. So the seventh, eighth
	// and ninth names are named here — the ninth being S47 T10's
	// `tools.vocabulary`, added under the operator ruling of 2026-11-02 — and a
	// new name without an amendment still fails. Every one of the eight must be a
	// read-shaped door: registered, `RequiresInitialize`, never `Debug`.
	wantTools := map[MethodName]bool{
		MethodToolsList:         true,
		MethodToolsGet:          true,
		MethodToolsByAgent:      true,
		MethodToolsStatus:       true,
		MethodToolsVocabulary:   true,
		MethodToolsMatchPattern: true, // S74 T2: a read that names the tools a pattern matches
		MethodToolsGrants:       true,
		MethodToolsMineList:     true,
		MethodToolsMineShow:     true,
	}
	gotTools := map[MethodName]bool{}
	for _, name := range tbl2.Methods() {
		if !strings.HasPrefix(string(name), "tools.") {
			continue
		}
		gotTools[name] = true
		m, ok := tbl2.Lookup(name)
		if !ok {
			t.Errorf("%s is listed but not lookup-able", name)
			continue
		}
		if !m.RequiresInitialize {
			t.Errorf("%s does not require Initialize — no tools.* door answers an unnegotiated connection", name)
		}
		if m.Debug {
			t.Errorf("%s is Debug — a tools.* door is product surface, and what keeps it off the narrow projection is the allow-list, never a Debug accident", name)
		}
	}
	for name := range wantTools {
		if !gotTools[name] {
			t.Errorf("%s is not registered — the catalog lost a door it promised", name)
		}
	}
	for name := range gotTools {
		if !wantTools[name] {
			t.Errorf("a tools.* door nobody named has arrived: %s — TC1 says this slice ships reads only, and S55's rule is that a new name is amended here by name or it does not arrive", name)
		}
	}
	// The belt: a seam UNSET after registration answers with the wiring's own
	// name rather than dereferencing nil — a panic in a handler is the daemon
	// answering nothing to everybody.
	tbl2.ToolsCatalog = nil
	if _, err := tbl2.Dispatch(context.Background(), conn, MethodToolsList, nil); err == nil {
		t.Fatal("the belt let a nil seam through")
	} else if e, ok := err.(*Error); !ok || e.Code != ErrNotWired.Code {
		t.Fatalf("unset seam: err = %T (%v), want the ErrNotWired code", err, err)
	}
}

// TestToolsVocabularyDoorSendsListsNeverNull is the wire contract of the
// vocabulary door (S47 T10): every list is a `[]`, never `null`. The distinction
// is not cosmetics — a screen that reads `null` renders NOTHING where the daemon
// answered something, and the one screen that consumes this door builds its
// filter selects and its capability pills straight from these four arrays. The
// stub answers with only `families` set, so the other three arrive as nil from
// the seam and must leave the door as empty arrays.
func TestToolsVocabularyDoorSendsListsNeverNull(t *testing.T) {
	stub := &toolsCatalogStub{vocab: ToolsVocabularyResult{Families: []string{"files"}}}
	tbl, conn := wiredToolsCatalog(t, stub)
	res := dispatchToolsResult[ToolsVocabularyResult](t, tbl, conn, MethodToolsVocabulary, json.RawMessage(`{}`))
	if len(res.Families) != 1 || res.Families[0] != "files" {
		t.Fatalf("the seam's families did not ride through: %v", res.Families)
	}
	if res.Capabilities == nil {
		t.Error("capabilities arrived as null — the pill row would render nothing where the daemon answered an empty set")
	}
	if res.Sources == nil {
		t.Error("sources arrived as null")
	}
	if res.RerunClasses == nil {
		t.Error("rerunClasses arrived as null")
	}

	// The shaper on its own, because the door is not the only caller (the CLI's
	// direct door calls it too): nil in, empty arrays out, and data given is
	// data kept.
	empty := ShapeToolsVocabularyResult(ToolsVocabularyResult{})
	if empty.Capabilities == nil || empty.Families == nil || empty.Sources == nil || empty.RerunClasses == nil {
		t.Errorf("ShapeToolsVocabularyResult left a nil list: %+v", empty)
	}
	given := ToolsVocabularyResult{Capabilities: []ToolCapabilityNote{{Name: "workspace_read", Meaning: "Reads under the workspace."}}, RerunClasses: []string{"safe"}}
	if got := ShapeToolsVocabularyResult(given); len(got.Capabilities) != 1 || len(got.RerunClasses) != 1 || got.Families == nil {
		t.Errorf("ShapeToolsVocabularyResult lost or invented data: %+v", got)
	}
}
