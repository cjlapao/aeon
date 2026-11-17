package protocol

// agent_management_test.go — S73 T4: the write edge of the agent-management
// family, tested where the edge lives (this package), through Dispatch, with
// the family's ONE seam standing in for cmd/aeon.
//
// # What this file can see that the store's drills cannot
//
// internal/store/agent_audited_test.go proves that a door which is handed an
// actor stamps what it was given and mints its ledger row in the same
// transaction. That is the bottom half of AR2. The top half — WHERE the actor
// comes from, and what happens when there is none — is a property of the door,
// not of the store: the peer credential is read here, the identity keys are
// refused here, the reads are waved through here, and a road that arrives with
// no name must be stopped here or nowhere. A store test cannot express "the
// seam was never reached", and that refusal is the whole of AR3's promise.
//
// # The shape is the one S46 and S59 shipped for exactly this
//
// `writeDoorTable` (automation_write_test.go) and the environment door tests
// established the pattern: a fake seam that RECORDS the context it was handed,
// a table whose `ActorFor` is whatever the case needs, and Dispatch as the only
// way in. The stamp is read back with `AgentActor`, which is precisely what
// `cmd/aeon`'s closure does in production — so the test drives the same channel
// the caller drives, not a paraphrase of it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// agentManageRecord is one call the family's seam received, with the stamp that
// rode in its context — the three facts every case below argues about.
type agentManageRecord struct {
	method  string
	kind    string
	id      string
	stamped bool
}

// agentManageFake is the family's seam. It answers whatever the case asks it to
// and remembers who arrived.
type agentManageFake struct {
	calls   []agentManageRecord
	result  any
	refusal string
	err     error
}

func (f *agentManageFake) handle(ctx context.Context, method string, _ json.RawMessage) (any, string, error) {
	kind, id, ok := AgentActor(ctx)
	// An edit or a delete reads the agent through this seam (agent.view) to work out the policy
	// rules that go with its tools (agent_grants.go), on the write's own stamped context. That read
	// is not a write road, and this fake counts write roads.
	if method == string(MethodAgentView) && ok {
		return map[string]any{"slug": "x"}, "", nil
	}
	f.calls = append(f.calls, agentManageRecord{method: method, kind: kind, id: id, stamped: ok})
	switch {
	case f.err != nil:
		return nil, "", f.err
	case f.refusal != "":
		return nil, f.refusal, nil
	case f.result != nil:
		return f.result, "", nil
	}
	return map[string]any{"ok": true}, "", nil
}

// agentDoorTable wires the family's seam onto a real, initialised table. A nil
// actorFor leaves the table with no actor door at all — one of the refusals
// under test, not an oversight.
func agentDoorTable(t *testing.T, f *agentManageFake, actorFor func(context.Context, *Session) Actor) *Table {
	t.Helper()
	tbl := newTestTable(t)
	if actorFor != nil {
		tbl.ActorFor = actorFor
	}
	tbl.AgentManage = f.handle
	initialized(t, tbl, "c1", PROTOCOL_VERSION)
	return tbl
}

// userPeer is what the daemon's ActorFor answers for a real connection: the
// kernel's peer credential, kind `user`, the uid as the id.
func userPeer(id string) func(context.Context, *Session) Actor {
	return func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: id} }
}

// agentWriteRoads is the six, with one plausible set of params each — plausible
// because the edge runs BEFORE any decode, so what it refuses it refuses without
// ever looking at whether the verb's own shape is right.
func agentWriteRoads() []struct {
	name   MethodName
	params json.RawMessage
} {
	return []struct {
		name   MethodName
		params json.RawMessage
	}{
		{MethodAgentCreate, json.RawMessage(`{"name":"Golang Engineer","description":"writes Go"}`)},
		{MethodAgentDuplicate, json.RawMessage(`{"slug":"golang-engineer","name":"Golang Engineer (copy)"}`)},
		{MethodAgentEdit, json.RawMessage(`{"slug":"golang-engineer","fields":{"description":"changed"}}`)},
		{MethodAgentDelete, json.RawMessage(`{"slug":"golang-engineer"}`)},
		{MethodAgentImport, json.RawMessage(`{"path":"/tmp/card.json"}`)},
		{MethodAgentMigrate, json.RawMessage(`{}`)},
	}
}

// agentReadRoads are the family's reads. `agent.export` belongs here and not
// with the writes because it writes a FILE, not a row — design's law, and the
// ledger has no `export` op to mint, which is why AR2 stays enforceable.
func agentReadRoads() []struct {
	name   MethodName
	params json.RawMessage
} {
	return []struct {
		name   MethodName
		params json.RawMessage
	}{
		{MethodAgentView, json.RawMessage(`{"slug":"golang-engineer"}`)},
		{MethodAgentRows, json.RawMessage(`{"page":1,"pageSize":20}`)},
		{MethodAgentExport, json.RawMessage(`{"slug":"golang-engineer"}`)},
	}
}

// TestAgentWritesStampThePeerTheyArrivedOn is AR4's first six rows: every write
// reaches the seam carrying the connection's person, and only that person.
func TestAgentWritesStampThePeerTheyArrivedOn(t *testing.T) {
	f := &agentManageFake{}
	tbl := agentDoorTable(t, f, userPeer("1000"))
	for _, road := range agentWriteRoads() {
		if _, err := tbl.Dispatch(context.Background(), "c1", road.name, road.params); err != nil {
			t.Fatalf("%s: %v", road.name, err)
		}
	}
	if len(f.calls) != 6 {
		t.Fatalf("the seam saw %d calls, want 6 — one per write road", len(f.calls))
	}
	for i, c := range f.calls {
		if !c.stamped || c.kind != "user" || c.id != "1000" {
			t.Errorf("%s arrived as %+v — the stamp is ActorFor's answer (the peer), never a claim", f.calls[i].method, c)
		}
	}
}

// TestAgentWritesRefuseWhenThePeerIsNotAPerson is the fail-closed half: three
// unusable peers and one runtime with no actor door at all. Each refuses with
// ErrAgentNoActor BEFORE the seam runs, so the honest answer is a refusal with
// nothing behind it — no agent row, and (upstairs) no ledger row either.
func TestAgentWritesRefuseWhenThePeerIsNotAPerson(t *testing.T) {
	cases := []struct {
		whom  string
		gate  func(context.Context, *Session) Actor
		where string
	}{
		{whom: "uid 0", gate: userPeer("0"), where: "uid 0"},
		{whom: "a blank id", gate: userPeer(""), where: `stamped "user 0"`},
		{whom: "a blank kind", gate: func(context.Context, *Session) Actor { return Actor{Kind: "", ID: "1000"} },
			where: "no peer identity"},
	}
	for _, tc := range cases {
		f := &agentManageFake{}
		tbl := agentDoorTable(t, f, tc.gate)
		_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentCreate, json.RawMessage(`{"name":"Nobody"}`))
		var perr *Error
		if !errors.As(err, &perr) || perr.Code != ErrAgentNoActor.Code {
			t.Fatalf("%s: answered %v, want ErrAgentNoActor (%d)", tc.whom, err, ErrAgentNoActor.Code)
		}
		if len(f.calls) != 0 {
			t.Fatalf("%s reached the seam (%+v) — the refusal is BEFORE the write, not after it", tc.whom, f.calls)
		}
	}

	// And the runtime that wired no actor door at all: a registered door with
	// no way to name a caller is refused, not half-open.
	f := &agentManageFake{}
	tbl := agentDoorTable(t, f, nil)
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentDelete, json.RawMessage(`{"slug":"golang-engineer"}`))
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != ErrAgentNoActor.Code {
		t.Fatalf("no ActorFor wired: answered %v, want ErrAgentNoActor", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("the seam ran with no actor door: %+v", f.calls)
	}
	// The what-names-it is the part a client reads; check it names the runtime,
	// so the sentence is not the uid-0 one wearing a different hat.
	if !strings.Contains(perr.Data.What, "wired no actor stamp") {
		t.Errorf("the unwired-runtime case answered What=%q — it must say the runtime wired no stamp", perr.Data.What)
	}
}

// TestAgentReadsCarryNoActorAtAll is SP6's line drawn in code: the three reads
// answer over a table that cannot name a peer, and the seam sees NO stamp. If a
// later change routed a read through the edge, the second assertion is what
// catches it — the first would still pass.
func TestAgentReadsCarryNoActorAtAll(t *testing.T) {
	f := &agentManageFake{}
	tbl := agentDoorTable(t, f, nil) // no ActorFor at all
	for _, road := range agentReadRoads() {
		if _, err := tbl.Dispatch(context.Background(), "c1", road.name, road.params); err != nil {
			t.Fatalf("%s: %v — a read must answer with no actor to speak of", road.name, err)
		}
	}
	if len(f.calls) != 3 {
		t.Fatalf("the seam saw %d calls, want 3", len(f.calls))
	}
	for _, c := range f.calls {
		if c.stamped {
			t.Errorf("%s arrived stamped %s:%s — a read carries no actor (SP6 draws the line at the acts)", c.method, c.kind, c.id)
		}
	}
}

// TestAgentWritesRefuseTheClosedIdentityVocabulary is AR3's door half: four
// named keys, six roads, each refused by NAME with ErrBadParams, each before the
// seam runs. Four keys and no grammar — S59's identity list is nine words
// because its plane has nine; this family has these four, and T3's
// forward-compat law forbids widening it into a guess.
func TestAgentWritesRefuseTheClosedIdentityVocabulary(t *testing.T) {
	for _, key := range agentIdentityKeys {
		for _, road := range agentWriteRoads() {
			f := &agentManageFake{}
			tbl := agentDoorTable(t, f, userPeer("1000"))
			raw := withKey(t, road.params, key)
			_, err := tbl.Dispatch(context.Background(), "c1", road.name, raw)
			var bad ErrBadParams
			if !errors.As(err, &bad) {
				t.Errorf("%s with %q: answered %v, want ErrBadParams (−32602)", road.name, key, err)
				continue
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", key)) {
				t.Errorf("%s with %q: the refusal does not name the key — %v", road.name, key, err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("%s with %q reached the seam (%+v)", road.name, key, f.calls)
			}
		}
	}
}

// TestAgentWritesIgnoreAKeyNobodyHasHeardOf is T3's forward-compat law, spoken
// at the write door rather than the read door: an unknown key must be IGNORED,
// and a request carrying one must SUCCEED. The closed vocabulary above is the
// exception, named in conformance/README.md's own words, and this test is the
// pair that keeps the exception from quietly becoming the rule.
func TestAgentWritesIgnoreAKeyNobodyHasHeardOf(t *testing.T) {
	f := &agentManageFake{}
	tbl := agentDoorTable(t, f, userPeer("1000"))
	for _, road := range agentWriteRoads() {
		raw := withKey(t, road.params, "someFutureField")
		if _, err := tbl.Dispatch(context.Background(), "c1", road.name, raw); err != nil {
			t.Fatalf("%s with an unknown key: %v — T3 says ignore it, not refuse it", road.name, err)
		}
	}
	if len(f.calls) != 6 {
		t.Fatalf("the seam saw %d calls, want 6 — an unknown key stopped at the door", len(f.calls))
	}
}

// TestAgentWriteEdgeRefusesWhatItCanReadAndDefersWhatItCannot states the edge's
// two honest limits, in the direction the code actually goes. A key is refused
// when the envelope can be parsed and the key is a TOP-LEVEL member of it. An
// envelope that cannot be parsed is deferred to the seam, because the family's
// decode error is a sentence S43 wrote for callers ("agent.create takes
// {name,description,…}") and replacing it with a JSON-parse complaint at the door
// would trade a useful refusal for a worse one. And a key nested inside
// `agent.edit`'s free-form `fields` blob is NOT refused — that is design
// deviation 8's recorded gap, pinned here as a test rather than as a paragraph,
// so the follow-up has a file to change and a name to change.
func TestAgentWriteEdgeRefusesWhatItCanReadAndDefersWhatItCannot(t *testing.T) {
	// Half 1: a malformed envelope is DEFERRED, not refused at the door — and
	// the deferral is visible, because the seam is reached and would be the one
	// to speak.
	g := &agentManageFake{}
	tbl := agentDoorTable(t, g, userPeer("1000"))
	if _, err := tbl.Dispatch(context.Background(), "c1", MethodAgentCreate,
		json.RawMessage(`{"name":`)); err != nil {
		t.Fatalf("the seam never saw the malformed envelope: %v — the edge must not decode the verb's shape", err)
	}
	if len(g.calls) != 1 {
		t.Fatalf("the seam saw %d calls, want 1", len(g.calls))
	}

	// Half 2: the nested gap, pinned. `fields` is free-form and the edge does
	// not walk it, so an identity key inside it passes the door. This is a
	// KNOWN HOLE, not an endorsement: the ledger still stamps the peer
	// regardless, so what the smuggled key buys is nothing — but the refusal
	// does not happen, and a test that pretended otherwise would hide it.
	f := &agentManageFake{}
	tbl2 := agentDoorTable(t, f, userPeer("1000"))
	if _, err := tbl2.Dispatch(context.Background(), "c1", MethodAgentEdit,
		json.RawMessage(`{"slug":"golang-engineer","fields":{"actor":"someone-else"}}`)); err != nil {
		t.Fatalf("the nested case: %v — the edge does not walk `fields` (deviation 8)", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("the seam saw %d calls, want 1", len(f.calls))
	}
	// What the smuggled key actually achieved: nothing. The stamp the seam saw
	// is still the peer's.
	if f.calls[0].kind != "user" || f.calls[0].id != "1000" {
		t.Fatalf("the nested key moved the stamp: %+v", f.calls[0])
	}

	// Half 3, the same shape done at the top level, for contrast: refused by
	// name. The pair of halves is the whole difference between a closed
	// vocabulary and a parser.
	h := &agentManageFake{}
	tbl3 := agentDoorTable(t, h, userPeer("1000"))
	var bad ErrBadParams
	_, err := tbl3.Dispatch(context.Background(), "c1", MethodAgentEdit,
		json.RawMessage(`{"slug":"golang-engineer","actor":"someone-else","fields":{}}`))
	if !errors.As(err, &bad) {
		t.Fatalf("top-level identity key: %v, want ErrBadParams — the closed set applies to every write road", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the refused key still reached the seam: %+v", h.calls)
	}
}

// TestAgentWriteEdgeRefusalsAreNotConfusedWithRefusals: the family already has a
// refusal voice (ErrAgentRefused) for "the daemon said no". A missing actor is a
// different fact and must not arrive in that coat — the app routes them
// differently, and the codes are the only thing that says which.
func TestAgentWriteEdgeRefusalsAreNotConfusedWithRefusals(t *testing.T) {
	if ErrAgentNoActor.Code == ErrAgentRefused.Code {
		t.Fatal("ErrAgentNoActor shares ErrAgentRefused's code — a client cannot tell the two apart")
	}
	f := &agentManageFake{refusal: "an agent with that slug is internal (AM1)"}
	tbl := agentDoorTable(t, f, userPeer("1000"))
	_, err := tbl.Dispatch(context.Background(), "c1", MethodAgentDelete, json.RawMessage(`{"slug":"golang-engineer"}`))
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != ErrAgentRefused.Code {
		t.Fatalf("the seam's refusal answered %v — it still rides ErrAgentRefused, unchanged", err)
	}
}

// --- helpers ------------------------------------------------------------------

// withKey merges one extra key into a JSON object, so a case can add the key
// under test to a road's own params without rewriting the params.
func withKey(t *testing.T, raw json.RawMessage, key string) json.RawMessage {
	t.Helper()
	var bag map[string]any
	if err := json.Unmarshal(raw, &bag); err != nil {
		t.Fatalf("the test's own params are not an object: %v", err)
	}
	bag[key] = "planted"
	out, err := json.Marshal(bag)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
