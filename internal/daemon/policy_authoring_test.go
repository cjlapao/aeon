package daemon

// policy_authoring_test.go — S45 T4: the socket's human door, exercised
// over the daemon's OWN wiring (the same New(Options{Store…}) the product
// builds, so the table seams, the peer stamp and the conditional
// registration are the production ones, not test doubles).
//
// The reads behind each assertion are the socket's own reads dispatched on
// the same table (policy.list / policy.get / policy.inForce) — the door and
// the window share one store, one boundary, one gate, so a flip visible
// only to the door would fail here (PM11's no-cache rule, N8). Audit
// assertions read the T3 seam's table (PolicyChangeList/Count) and, for the
// "an add is not an event" half, PolicyEventStats (a stored rule is not a
// decision; policy_event belongs to the gate).

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"
)

// authoringDaemon mounts the REAL wiring: a temp store, the one engine, the
// table seams New() itself wires beside the reads. conns controls the peer
// stamp (an absent/unattributable conn answers uid 0 — T0's refusal case).
func authoringDaemon(t *testing.T, conns []localrpc.ConnInfo) (*Daemon, *wire, *store.Store, *seed.Engine) {
	t.Helper()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatalf("NewSeedEngine: %v", err)
	}
	w := &wire{}
	d := New(Options{
		DaemonVersion: "9.9.9-test",
		BuildTime:     "2026-10-15T00:00:00Z",
		GitCommit:     "deadbee",
		Logger:        w,
		Notify:        w.notify,
		Connections:   func() []localrpc.ConnInfo { return conns },
		Store:         s,
		Seed:          e,
	})
	return d, w, s, e
}

// authoringPeer is the attributed peer the door must stamp: uid 4321 is a
// number the WIRE never saw (it comes from the connection's credentials).
var authoringPeer = []localrpc.ConnInfo{{ID: "peer-1", PeerUID: 4321, PeerPID: 7}}

func mustJSONDaemon(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func toolAllow(id, agent, tool string) protocol.PolicyAddParams {
	return protocol.PolicyAddParams{
		ID: id, Type: "tool_access",
		Target: protocol.PolicyTargetP{Kind: "agent", ID: agent},
		Action: "allow",
		Spec:   json.RawMessage(`{"toolName":"` + tool + `","state":"allow"}`),
	}
}

func authoringErr(t *testing.T, err error) *protocol.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected a protocol error, got success")
	}
	var e *protocol.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %T (%v), want *protocol.Error", err, err)
	}
	return e
}

// authoringSvc builds the service directly over a temp store with a frozen
// clock (the door's behaviour is the subject; the dispatch shape is covered
// by the socket-path tests and internal/protocol's seam tests).
func authoringSvc(t *testing.T, s *store.Store, e *seed.Engine) *authoringService {
	t.Helper()
	svc := newAuthoringService(s, e, nil).(*authoringService)
	fixed := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	return svc
}

// --- Behaviour 1/5: the door is live on the socket, and the reads flip ----

func TestPolicyAddOverSocketWritesRow(t *testing.T) {
	ctx := context.Background()
	d, _, _, _ := authoringDaemon(t, authoringPeer)
	initConn(t, d, ctx, "peer-1", "s45-t4")

	res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd,
		mustJSONDaemon(t, toolAllow("allow-webfetch-boss", "boss", "webfetch")))
	if err != nil {
		t.Fatalf("policy.add over the socket: %v", err)
	}
	add := res.(protocol.PolicyAddResult)
	if add.Policy.ID != "allow-webfetch-boss" || add.Policy.Action != "allow" || add.Policy.Origin != "user" {
		t.Fatalf("the add answer is not the row it stored: %+v", add.Policy)
	}
	if add.Policy.Describe == "" {
		t.Error("the add answer carries no describe sentence (D9: one definition with policy.get)")
	}

	// policy.list sees it immediately — no cache, no queue, no pause (N8).
	lres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyList,
		mustJSONDaemon(t, protocol.PolicyListParams{}))
	if err != nil {
		t.Fatalf("policy.list: %v", err)
	}
	list := lres.(protocol.PolicyListResult)
	var inList bool
	for _, v := range list.Policies {
		if v.ID == "allow-webfetch-boss" {
			inList = true
		}
	}
	if !inList {
		t.Fatalf("policy.list does not carry the row the socket just wrote (PM11): %+v", list.Policies)
	}

	// policy.get returns the same detail the door returned — one definition.
	gres, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyGet,
		mustJSONDaemon(t, protocol.PolicyGetParams{ID: "allow-webfetch-boss"}))
	if err != nil {
		t.Fatalf("policy.get: %v", err)
	}
	if !reflect.DeepEqual(gres.(protocol.PolicyGetResult).Policy.PolicyView, add.Policy.PolicyView) {
		t.Errorf("the door and the read disagree about the same row: door %+v read %+v", add.Policy, gres)
	}

	// policy.inForce answers afresh: the rule is in force NOW.
	ires, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyInForce,
		mustJSONDaemon(t, protocol.PolicyInForceParams{Agent: "boss"}))
	if err != nil {
		t.Fatalf("policy.inForce: %v", err)
	}
	force := ires.(protocol.PolicyInForceResult)
	var seen bool
	for _, r := range force.Answers {
		if r.PolicyID == "allow-webfetch-boss" {
			// PL10: a fresh allow with no acceptance record answers
			// enforced_unacknowledged — still enforced, still HERE. The
			// point is presence, not the acceptance costume.
			seen = true
		}
	}
	if !seen {
		t.Fatalf("policy.inForce does not report the freshly added rule: %+v", force.Answers)
	}
}

// --- PL1 verbatim -----------------------------------------------------------

func TestPolicyAddRefusesAllowFromNonOperatorVerbatim(t *testing.T) {
	ctx := context.Background()
	d, _, s, _ := authoringDaemon(t, authoringPeer)
	initConn(t, d, ctx, "peer-1", "s45-t4")

	params := protocol.PolicyAddParams{
		ID: "self-grant", Type: "tool_access",
		Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"},
		Action: "allow",
		Spec:   json.RawMessage(`{"toolName":"bash","state":"allow"}`),
		Source: "agent",
	}
	_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd, mustJSONDaemon(t, params))
	e := authoringErr(t, err)
	if e.Code != protocol.ErrPolicyAuthoringRefused.Code {
		t.Fatalf("code = %d, want %d (ErrPolicyAuthoringRefused)", e.Code, protocol.ErrPolicyAuthoringRefused.Code)
	}

	// Byte-for-byte the BOUNDARY's sentence: what the same row through a
	// fresh boundary over the same store refuses, verbatim.
	same := policy.Policy{
		ID: "self-grant", Type: "tool_access",
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Allow, Source: policy.Agent,
		Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`),
	}
	rej := policy.NewStore(newPolicyRepo(s)).Add(ctx, same, time.Now())
	if rej == nil {
		t.Fatal("the boundary itself accepted an agent-authored allow — PL1 is gone (the socket cannot be the only one enforcing)")
	}
	if e.Data.What != rej.Reason {
		t.Errorf("PL1's sentence drifted across the wire:\n socket: %q\n boundary: %q", e.Data.What, rej.Reason)
	}

	// The refused write stored nothing and audited nothing.
	if n, _ := s.PolicyChangeCount(ctx); n != 0 {
		t.Errorf("a refused add wrote %d audit rows; PL1's refusal owes none", n)
	}
	if rows, _ := s.PolicyListAll(ctx); len(rows) != 0 {
		t.Errorf("a refused add stored %d rows", len(rows))
	}
}

// --- The boundary's refusals are the door's refusals ------------------------

func TestPolicyAddRefusalsAreTheBoundarys(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	b := policy.NewStore(newPolicyRepo(s))
	actor := protocol.Actor{Kind: "user", ID: "4321"}

	cases := []struct {
		name string
		p    protocol.PolicyAddParams
		pol  policy.Policy
	}{
		{"closed-set", protocol.PolicyAddParams{ID: "x1", Type: "everything",
			Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"}, Action: "deny",
			Spec: json.RawMessage(`{}`)},
			policy.Policy{ID: "x1", Type: "everything", Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
				Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{}`)}},
		{"identifier", protocol.PolicyAddParams{ID: "bad id!", Type: "tool_access",
			Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"}, Action: "deny",
			Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)},
			policy.Policy{ID: "bad id!", Type: "tool_access", Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
				Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`)}},
		{"per-type spec", protocol.PolicyAddParams{ID: "x3", Type: "tool_access",
			Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"}, Action: "deny",
			Spec: json.RawMessage(`{}`)},
			policy.Policy{ID: "x3", Type: "tool_access", Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
				Action: policy.Deny, Source: policy.Operator, Spec: json.RawMessage(`{}`)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.Add(ctx, actor, c.p)
			e := authoringErr(t, err)
			if e.Code != protocol.ErrPolicyAuthoringRefused.Code {
				t.Fatalf("code = %d, want the authoring refusal", e.Code)
			}
			rej := b.Add(ctx, c.pol, time.Now())
			if rej == nil {
				t.Fatal("the boundary accepted what the door refused — the door IS a second validator")
			}
			if e.Data.What != rej.Reason {
				t.Errorf("refusal is not the boundary's verbatim:\n door: %q\n boundary: %q", e.Data.What, rej.Reason)
			}
			switch c.name {
			case "closed-set":
				if !strings.Contains(e.Data.What, "everything") {
					t.Errorf("the closed-set refusal must name the value it refused: %q", e.Data.What)
				}
			case "identifier":
				if !strings.Contains(e.Data.What, "bad id!") {
					t.Errorf("the identifier refusal must name the id: %q", e.Data.What)
				}
			case "per-type spec":
				if !strings.Contains(e.Data.What, "state") {
					t.Errorf("the spec refusal must name the missing field: %q", e.Data.What)
				}
			}
		})
	}
}

// --- One boundary round-trip (no double validation, no double write) --------

func TestPolicyAddIsOneBoundaryRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	hooks := newPolicyRepo(s)
	var adds int
	inner := hooks.Add
	hooks.Add = func(ctx context.Context, r policy.Row) error {
		adds++
		return inner(ctx, r)
	}
	fixed := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	svc := &authoringService{
		boundary: policy.NewStore(hooks),
		store:    s,
		log:      discardLogger{},
		now:      func() time.Time { return fixed },
	}
	if _, err := svc.Add(ctx, protocol.Actor{Kind: "user", ID: "4321"},
		toolAllow("once-round", "boss", "webfetch")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if adds != 1 {
		t.Fatalf("the accepted add reached the store %d times; exactly one boundary round-trip (PM2/PM4)", adds)
	}
}

// --- The actor is the connection's, never a claim ---------------------------

func TestPolicyAddActorFromPeerNotParams(t *testing.T) {
	ctx := context.Background()
	d, _, s, _ := authoringDaemon(t, authoringPeer)
	initConn(t, d, ctx, "peer-1", "s45-t4")

	// A params body that CARRIES the actor is a shape error, not a
	// suggestion: the key is refused by name (decodeParams ignores unknown
	// fields package-wide, so the door refuses the identity keys itself).
	raw := json.RawMessage(`{"id":"claim-me","type":"tool_access","target":{"kind":"agent","id":"boss"},` +
		`"action":"deny","spec":{"toolName":"bash"},"actor":{"kind":"user","id":"9999"}}`)
	_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd, raw)
	if err == nil {
		t.Fatal("params carrying an actor key were accepted")
	}
	var bp protocol.ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("actor param: err = %T (%v), want protocol.ErrBadParams (JSON-RPC's -32602 — the identity keys are refused)", err, err)
	}
	if !strings.Contains(bp.Error(), "actor") {
		t.Errorf("the shape refusal must name the key it refused, got %q", bp.Error())
	}
	for _, k := range []string{"status", "createdBy", "acceptedBy"} {
		bad := json.RawMessage(`{"id":"claim-me","type":"tool_access","target":{"kind":"agent","id":"boss"},` +
			`"action":"deny","spec":{"toolName":"bash"},"` + k + `":"whoever"}`)
		if _, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd, bad); err == nil {
			t.Errorf("params carrying %q were accepted — the identity keys must refuse", k)
		}
	}

	// A clean add: the AUDIT row carries the peer uid the connection
	// proved — 4321 — never anything the caller typed.
	if _, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd,
		mustJSONDaemon(t, toolAllow("peer-stamped", "boss", "webfetch"))); err != nil {
		t.Fatalf("add: %v", err)
	}
	changes, err := s.PolicyChangeList(ctx, "peer-stamped", 10)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes: %d rows, %v", len(changes), err)
	}
	if changes[0].ActorKind != "user" || changes[0].ActorID != "4321" {
		t.Errorf("audit actor = %s/%s, want user/4321 (the peer credential, not a param)",
			changes[0].ActorKind, changes[0].ActorID)
	}
}

func TestPolicyAddNoActorRefuses(t *testing.T) {
	ctx := context.Background()
	// The conn exists (initialize works) but resolves to NO peer: uid 0.
	d, _, s, _ := authoringDaemon(t, []localrpc.ConnInfo{{ID: "peer-1", PeerUID: 0, PeerPID: 7}})
	initConn(t, d, ctx, "peer-1", "s45-t4")

	_, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd,
		mustJSONDaemon(t, toolAllow("placeholder-me", "boss", "webfetch")))
	e := authoringErr(t, err)
	if e.Code != protocol.ErrPolicyNoActor.Code {
		t.Fatalf("code = %d, want %d (ErrPolicyNoActor — never a placeholder identity)",
			e.Code, protocol.ErrPolicyNoActor.Code)
	}
	if rows, _ := s.PolicyListAll(ctx); len(rows) != 0 {
		t.Errorf("an unattributable peer stored %d rows — the write must stop before the boundary", len(rows))
	}
	if n, _ := s.PolicyChangeCount(ctx); n != 0 {
		t.Errorf("an unattributable peer wrote %d audit rows", n)
	}
}

// --- Before/after (PM7.2 made wire) -----------------------------------------

func TestPolicyAddReturnsBeforeAndAfter(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	fixed := svc.now()
	b := policy.NewStore(newPolicyRepo(s))
	actor := protocol.Actor{Kind: "user", ID: "4321"}

	if _, err := svc.Add(ctx, actor, toolAllow("web-allow", "boss", "webfetch")); err != nil {
		t.Fatalf("allow add: %v", err)
	}
	// The expected BEFORE: the boundary's own answers right now (allow in
	// force), rendered by the ONE formatter.
	target := policy.Target{Kind: policy.AgentKind, ID: "boss"}
	answersBefore, rej := b.InForceFor(ctx, target, fixed)
	if rej != nil {
		t.Fatalf("in-force: %s", rej.Reason)
	}

	deny := protocol.PolicyAddParams{
		ID: "web-deny", Type: "tool_access",
		Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"},
		Action: "deny",
		Spec:   json.RawMessage(`{"toolName":"webfetch","state":"deny"}`),
	}
	res, err := svc.Add(ctx, actor, deny)
	if err != nil {
		t.Fatalf("deny add: %v", err)
	}
	if len(res.Before.Answers) != len(answersBefore) {
		t.Fatalf("before carries %d rows, the boundary answered %d", len(res.Before.Answers), len(answersBefore))
	}
	for i, a := range answersBefore {
		if res.Before.Answers[i].Render != a.Render() {
			t.Errorf("before[%d].Render = %q, want the boundary's Render() verbatim %q",
				i, res.Before.Answers[i].Render, a.Render())
		}
	}
	// after must DIFFER (a deny over a previously-allowed target — the flip
	// is the whole point of the pair).
	same := len(res.After.Answers) == len(res.Before.Answers)
	for i := 0; same && i < len(res.After.Answers); i++ {
		if res.After.Answers[i].Render != res.Before.Answers[i].Render {
			same = false
		}
	}
	if same {
		t.Error("before and after are identical — a deny added beside an allow must move the answer (PM7.2)")
	}
	answersAfter, rej := b.InForceFor(ctx, target, fixed)
	if rej != nil {
		t.Fatalf("in-force after: %s", rej.Reason)
	}
	for i, a := range answersAfter {
		if res.After.Answers[i].Render != a.Render() {
			t.Errorf("after[%d].Render = %q, want %q", i, res.After.Answers[i].Render, a.Render())
		}
	}
	if res.Before.Target != "agent:boss" || res.After.Target != "agent:boss" {
		t.Errorf("views must name the target: %q / %q", res.Before.Target, res.After.Target)
	}
}

// --- Exactly one audit row per accepted write (PM8) -------------------------

func TestPolicyAddWritesExactlyOneAuditRow(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	actor := protocol.Actor{Kind: "user", ID: "4321"}

	baseEvents, _, err := s.PolicyEventStats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := s.PolicyChangeCount(ctx)

	if _, err := svc.Add(ctx, actor, toolAllow("one-row", "boss", "webfetch")); err != nil {
		t.Fatalf("add: %v", err)
	}
	after, _ := s.PolicyChangeCount(ctx)
	if after != base+1 {
		t.Fatalf("audit rows after one add = %d, want exactly %d (one row per write, PM8)", after, base+1)
	}
	rows, _ := s.PolicyChangeList(ctx, "one-row", 10)
	if len(rows) != 1 {
		t.Fatalf("policy.changes for the id carries %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.Op != "add" || r.ActorKind != "user" || r.ActorID != "4321" {
		t.Errorf("row = %+v, want op=add actor user/4321", r)
	}
	if r.ProposalID != nil {
		t.Errorf("ProposalID = %v, want NULL — the direct door decides no proposal", *r.ProposalID)
	}
	// ids-only fixed template (SEC-033): the closed words and the gated
	// identifiers, nothing else.
	if want := "added tool_access allow for agent:boss"; r.Summary != want {
		t.Errorf("summary = %q, want the fixed template %q", r.Summary, want)
	}
	// A stored rule is not a decision: policy_event belongs to the gate.
	deep, _, err := s.PolicyEventStats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if deep != baseEvents {
		t.Errorf("an add grew policy_event by %d; authoring writes audit rows, never decision rows", deep-baseEvents)
	}

	// A REFUSED add audits nothing (PM8's other half).
	if _, err := svc.Add(ctx, actor, protocol.PolicyAddParams{
		ID: "refused-row", Type: "tool_access",
		Target: protocol.PolicyTargetP{Kind: "agent", ID: "boss"},
		Action: "allow", Source: "agent",
		Spec: json.RawMessage(`{"toolName":"bash","state":"allow"}`),
	}); err == nil {
		t.Fatal("PL1 did not refuse an agent-authored allow")
	}
	if n, _ := s.PolicyChangeCount(ctx); n != after {
		t.Errorf("a refused add wrote %d more audit rows; refusals owe none", n-after)
	}
}

// --- Warnings ride as data ---------------------------------------------------

func TestPolicyAddWarningsCarried(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	res, err := svc.Add(ctx, protocol.Actor{Kind: "user", ID: "4321"}, protocol.PolicyAddParams{
		ID: "loop-deny", Type: "command_access",
		Target: protocol.PolicyTargetP{Kind: "loop", ID: "aeon-s45"},
		Action: "deny",
		Spec:   json.RawMessage(`{"command":"git push*","state":"deny"}`),
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	want := desk.UnmatchableTargetWarn("loop")
	var carried bool
	for _, w := range res.Warnings {
		if w == want {
			carried = true
		}
	}
	if !carried {
		t.Errorf("DEF-038's warning did not ride verbatim: %q (want %q)", res.Warnings, want)
	}
	// An agent target carries none (the CLI is silent there too — parity).
	quiet, err := svc.Add(ctx, protocol.Actor{Kind: "user", ID: "4321"}, toolAllow("quiet-one", "boss", "webfetch"))
	if err != nil {
		t.Fatalf("agent add: %v", err)
	}
	if len(quiet.Warnings) != 0 {
		t.Errorf("an agent-target add warned %q; the CLI is silent there and so is the door", quiet.Warnings)
	}
}

// --- notAfter: null never becomes a sentinel (PL7) ---------------------------

func TestPolicyAddNotAfterNullNeverSentinel(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	fixed := svc.now()
	actor := protocol.Actor{Kind: "user", ID: "4321"}

	nilRes, err := svc.Add(ctx, actor, toolAllow("no-expiry", "boss", "webfetch"))
	if err != nil {
		t.Fatalf("null add: %v", err)
	}
	if nilRes.Policy.NotAfter != nil {
		t.Errorf("omitted notAfter stored %v — null/omitted is NO expiry, never a far-future sentinel (PL7)",
			*nilRes.Policy.NotAfter)
	}
	far := "2999-01-01T00:00:00Z"
	p := toolAllow("far-future", "boss", "webfetch")
	p.NotAfter = &far
	futRes, err := svc.Add(ctx, actor, p)
	if err != nil {
		t.Fatalf("far add: %v", err)
	}
	if futRes.Policy.NotAfter == nil {
		t.Fatal("the far-future string was dropped")
	}
	if got := futRes.Policy.NotAfter.UTC().Format(time.RFC3339Nano); got != far {
		t.Errorf("notAfter stored as %q, want exactly %q (stored as given)", got, far)
	}
	// And the ROW the store holds says the same (the door's answer is the
	// store's row, not a shape it assembled for itself).
	rows, rj := policy.NewStore(newPolicyRepo(s)).ListDetailed(ctx, fixed)
	if rj != nil {
		t.Fatalf("list: %s", rj.Reason)
	}
	sawNil, sawFar := false, false
	for _, r := range rows {
		if r.ID == "no-expiry" {
			sawNil = true
			if r.NotAfter != nil {
				t.Errorf("store row no-expiry carries notAfter %v", *r.NotAfter)
			}
		}
		if r.ID == "far-future" {
			sawFar = true
			if r.NotAfter == nil || !r.NotAfter.UTC().Equal(futRes.Policy.NotAfter.UTC()) {
				t.Errorf("store row far-future notAfter = %v, want %v", r.NotAfter, far)
			}
		}
	}
	if !sawNil || !sawFar {
		t.Fatalf("the fail-closed list lost a row (nil-seen %v far-seen %v)", sawNil, sawFar)
	}
}

// --- remove: the row as evidence, the states, the one audit ------------------

func TestPolicyRemoveCapturesRowAndStates(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	actor := protocol.Actor{Kind: "user", ID: "4321"}

	added, err := svc.Add(ctx, actor, toolAllow("gone-soon", "boss", "webfetch"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	base, _ := s.PolicyChangeCount(ctx)
	res, err := svc.Remove(ctx, actor, "gone-soon")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !res.Removed {
		t.Fatal("removed = false for a row that existed")
	}
	if res.Policy == nil || !reflect.DeepEqual(*res.Policy, added.Policy) {
		t.Errorf("the remove answer lost the row as it was: %+v", res.Policy)
	}
	if len(res.Before.Answers) == 0 || res.Before.Answers[0].PolicyID != "gone-soon" {
		t.Errorf("before must name the rule while it still exists: %+v", res.Before.Answers)
	}
	if len(res.After.Answers) != 0 {
		t.Errorf("after must be empty once removed: %+v", res.After.Answers)
	}
	if n, _ := s.PolicyChangeCount(ctx); n != base+1 {
		t.Fatalf("audit rows after one remove = %d, want %d", n, base+1)
	}
	rows, _ := s.PolicyChangeList(ctx, "gone-soon", 10)
	// The add named this id too (it is the same rule's history, newest
	// first); exactly ONE NEW row landed for the removal.
	if len(rows) != 2 || rows[0].Op != "remove" || rows[1].Op != "add" {
		t.Fatalf("the audit trail is wrong: %+v", rows)
	}
	if want := "removed tool_access allow for agent:boss"; rows[0].Summary != want {
		t.Errorf("summary = %q, want %q", rows[0].Summary, want)
	}
}

// TestPolicyRemoveSystemRowSaysSo pairs PM16's sentence with a genuinely
// system-origin row: T12's seed door (origin='system') writes it, this door
// removes it (PM16: an operator's choice), and the audit row SAYS so — the
// fixed template naming the origin, not free text (SEC-033 holds even for
// a sentence worth having).
func TestPolicyRemoveSystemRowSaysSo(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := authoringSvc(t, s, e)
	actor := protocol.Actor{Kind: "user", ID: "4321"}
	ensureCorpusAgentRow(t, s, "brief-author")
	if _, err := svc.SeedCorpus(ctx, actor, protocol.SeedCorpusParams{Agents: []string{"brief-author"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, rj := policy.NewStore(newPolicyRepo(s)).ListDetailed(ctx, svc.now())
	if rj != nil {
		t.Fatalf("list: %s", rj.Reason)
	}
	var sysID string
	for _, r := range rows {
		if r.Origin == "system" {
			sysID = r.ID
			break
		}
	}
	if sysID == "" {
		t.Fatal("the seed wrote no system-origin row to remove")
	}
	res, err := svc.Remove(ctx, actor, sysID)
	if err != nil || !res.Removed {
		t.Fatalf("removing a system row must succeed (PM16): removed=%v err=%v", res.Removed, err)
	}
	audit, _ := s.PolicyChangeList(ctx, sysID, 10)
	if len(audit) != 1 {
		t.Fatalf("system removal audit rows = %d, want 1", len(audit))
	}
	if !strings.Contains(audit[0].Summary, "system rule") || !strings.Contains(audit[0].Summary, "PM16") {
		t.Errorf("the removal of a seeded rule must say so in the audit, got %q", audit[0].Summary)
	}
}

func TestPolicyRemoveAbsentIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	svc := authoringSvc(t, s, nil)
	base, _ := s.PolicyChangeCount(ctx)
	res, err := svc.Remove(ctx, protocol.Actor{Kind: "user", ID: "4321"}, "never-existed")
	if err != nil {
		t.Fatalf("removing an absent id must not error (DB19): %v", err)
	}
	if res.Removed || res.Policy != nil {
		t.Errorf("absent removal answered %+v, want removed:false / policy:nil", res)
	}
	if res.Before.Answers == nil || res.After.Answers == nil {
		t.Errorf("absent removal must still answer with empty (never null) views: %+v / %+v",
			res.Before.Answers, res.After.Answers)
	}
	if n, _ := s.PolicyChangeCount(ctx); n != base {
		t.Errorf("a no-op removal wrote %d audit rows; it removed nothing", n-base)
	}
}

// --- the corpus seed door (thin over T12's engine door) ----------------------

// ensureCorpusAgentRow inserts the agent row the seed door's table check
// requires — through the store's own seed path (the applier's writers, not
// a test-side INSERT; the policyseed corpus_test pattern).
func ensureCorpusAgentRow(t *testing.T, s *store.Store, name string) {
	t.Helper()
	err := s.SeedWithTx(context.Background(), func(tx *sql.Tx) error {
		return s.SeedAgentInsert(context.Background(), tx, store.AgentContent{
			Slug: name, Name: name, Description: "S45 T4 door test row",
			Prompt: "", Tools: "[]", Handoff: "{}", Metadata: "{}",
			BodyHash: "s45-t4", Origin: "system",
			// S73 T3: the seed trio carries the engine's own stamp, the way
			// mcpseed stamps mcp_change.
		}, store.AgentActor{Kind: store.AgentActorSystem, ID: "seed-engine"}, "2026-11-06T09:00:00Z")
	})
	if err != nil {
		t.Fatalf("ensure agent %q: %v", name, err)
	}
}

func corpusRuleCount(t *testing.T, agent string) int {
	t.Helper()
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("corpus: %s", rej.Reason)
	}
	for _, a := range corpus.Agents {
		if a.Agent == agent {
			return len(a.Rules)
		}
	}
	t.Fatalf("agent %q is not in the corpus", agent)
	return 0
}

func TestPolicySeedDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := authoringSvc(t, s, e)
	baseEvents, _, _ := s.PolicyEventStats(ctx, "")
	baseChanges, _ := s.PolicyChangeCount(ctx)
	ensureCorpusAgentRow(t, s, "brief-author")
	want := corpusRuleCount(t, "brief-author")

	res, err := svc.SeedCorpus(ctx, protocol.Actor{Kind: "user", ID: "4321"},
		protocol.SeedCorpusParams{Agents: []string{"brief-author"}, DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.DryRun {
		t.Error("dryRun:true must answer dryRun:true")
	}
	if res.Seeded != want || len(res.PerAgent) != 1 || res.PerAgent[0].Agent != "brief-author" ||
		res.PerAgent[0].Seeded+res.PerAgent[0].Unchanged != want {
		t.Errorf("the dry run must PLAN honestly: %+v (corpus says %d rules for brief-author)", res, want)
	}
	// NOTHING wrote: no policy rows, no audit rows, no events, no ledger.
	if rows, _ := s.PolicyListAll(ctx); len(rows) != 0 {
		t.Errorf("the dry run stored %d policy rows — a dry run is a read", len(rows))
	}
	if n, _ := s.PolicyChangeCount(ctx); n != baseChanges {
		t.Errorf("the dry run wrote %d audit rows", n-baseChanges)
	}
	if deep, _, _ := s.PolicyEventStats(ctx, ""); deep != baseEvents {
		t.Error("the dry run touched policy_event")
	}
	if led, _ := s.SeedLedgerList(ctx, "policy.corpus"); len(led) != 0 {
		t.Errorf("the dry run applied %d ledger rows", len(led))
	}
}

func TestPolicySeedWritesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := authoringSvc(t, s, e)
	actor := protocol.Actor{Kind: "user", ID: "4321"}
	ensureCorpusAgentRow(t, s, "brief-author")
	want := corpusRuleCount(t, "brief-author")

	first, err := svc.SeedCorpus(ctx, actor, protocol.SeedCorpusParams{Agents: []string{"brief-author"}})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if first.DryRun {
		t.Error("a real run answers dryRun:false")
	}
	if first.Seeded != want || first.Unchanged != 0 {
		t.Errorf("first run: seeded %d unchanged %d, want %d/0 (the pinned per-agent subset)",
			first.Seeded, first.Unchanged, want)
	}
	if first.PerAgent[0].Seeded+first.PerAgent[0].Unchanged != want {
		t.Error("per-agent counts must add up to the agent's corpus rules")
	}
	rows, _ := s.PolicyListAll(ctx)
	if len(rows) != want {
		t.Errorf("first run stored %d rows, want %d", len(rows), want)
	}

	// ONE audit row per RUN (T12's door writes it; this door appends no
	// second one — the seed names no single id, so policy_id is NULL).
	changes, _ := s.PolicyChangeList(ctx, "", 500)
	if len(changes) != 1 {
		t.Fatalf("the seed run wrote %d audit rows, want exactly 1 (T12's per-run row; a second is the double-audit T12's Notes forbid)", len(changes))
	}
	if changes[0].Op != "seed" || changes[0].ActorKind != "user" || changes[0].ActorID != "4321" {
		t.Errorf("seed audit row = %+v, want op=seed actor user/4321", changes[0])
	}
	if changes[0].PolicyID != "" {
		t.Errorf("the seed run names no single rule: policy_id must be NULL, got %q", changes[0].PolicyID)
	}

	// Idempotent: a second real run reports unchanged and audits nothing new.
	second, err := svc.SeedCorpus(ctx, actor, protocol.SeedCorpusParams{Agents: []string{"brief-author"}})
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if second.Seeded != 0 || second.Unchanged != want {
		t.Errorf("second run: seeded %d unchanged %d, want 0/%d (D4's convergence — an existing rule is left alone)",
			second.Seeded, second.Unchanged, want)
	}
	if n, _ := s.PolicyChangeCount(ctx); n != 1 {
		t.Errorf("the second run wrote %d extra audit rows; an unchanged run audits nothing", n-1)
	}
	if rows, _ := s.PolicyListAll(ctx); len(rows) != want {
		t.Errorf("the second run changed the row count to %d", len(rows))
	}
}

func TestPolicySeedUnknownAgentNamesIt(t *testing.T) {
	ctx := context.Background()
	s := openSeedStore(t)
	e, err := NewSeedEngine(s, seed.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := authoringSvc(t, s, e)
	ensureCorpusAgentRow(t, s, "brief-author")

	_, err = svc.SeedCorpus(ctx, protocol.Actor{Kind: "user", ID: "4321"},
		protocol.SeedCorpusParams{Agents: []string{"ghost-agent", "brief-author"}})
	pe := authoringErr(t, err)
	if pe.Code != protocol.ErrPolicyAuthoringRefused.Code {
		t.Fatalf("code = %d, want the authoring refusal", pe.Code)
	}
	if !strings.Contains(pe.Data.What, "ghost-agent") {
		t.Errorf("the refusal must NAME the unknown agent, got %q", pe.Data.What)
	}
	// The whole run refuses — the known agent is NOT half-seeded.
	if rows, _ := s.PolicyListAll(ctx); len(rows) != 0 {
		t.Errorf("an unknown-agent run stored %d rows; the refusal is whole-run", len(rows))
	}
	if n, _ := s.PolicyChangeCount(ctx); n != 0 {
		t.Errorf("an unknown-agent run wrote %d audit rows", n)
	}
}

// --- The arbiter is unchanged by the authoring path --------------------------

// TestArbiterUnchangedByAuthoringPath is S35's arbiter property re-run
// across the new door: a rule written through the SOCKET and the same rule
// written through the CLI-shaped boundary produce byte-identical verdicts —
// the gate learned nothing from T4 (no socket-flavoured deciding, no
// origin-aware branch). The S35 slice test itself (cmd/aeon's
// TestProposalSliceAcrossAllKinds) is byte-pinned below and runs green in
// the validation bar; and structurally, the door file itself contains no
// DecideProposal call — the socket still never commits a proposal (S35's
// ruling), it only writes rows.
func TestArbiterUnchangedByAuthoringPath(t *testing.T) {
	ctx := context.Background()
	fixed := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

	// Door A: the socket service.
	sa := openSeedStore(t)
	svcA := newAuthoringService(sa, nil, nil).(*authoringService)
	svcA.now = func() time.Time { return fixed }
	rowA := toolAllow("parity-row", "boss", "webfetch")
	if _, err := svcA.Add(ctx, protocol.Actor{Kind: "user", ID: "4321"}, rowA); err != nil {
		t.Fatalf("socket add: %v", err)
	}
	// Door B: the boundary as the CLI's door reaches it.
	sb := openSeedStore(t)
	pol := policy.Policy{
		ID: "parity-row", Type: "tool_access",
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Allow, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"webfetch","state":"allow"}`),
	}
	if rej := policy.NewStore(newPolicyRepo(sb)).Add(ctx, pol, fixed); rej != nil {
		t.Fatalf("boundary add: %s", rej.Reason)
	}

	target := policy.Target{Kind: policy.AgentKind, ID: "boss"}
	va, rej := policy.NewStore(newPolicyRepo(sa)).InForceFor(ctx, target, fixed)
	if rej != nil {
		t.Fatalf("in-force A: %s", rej.Reason)
	}
	vb, rej := policy.NewStore(newPolicyRepo(sb)).InForceFor(ctx, target, fixed)
	if rej != nil {
		t.Fatalf("in-force B: %s", rej.Reason)
	}
	if len(va) != len(vb) {
		t.Fatalf("the two doors answer different row counts: %d vs %d", len(va), len(vb))
	}
	for i := range va {
		if va[i].Render() != vb[i].Render() {
			t.Errorf("verdict %d differs:\n socket: %q\n  direct: %q", i, va[i].Render(), vb[i].Render())
		}
	}

	// Structural half: the door never decides a proposal (S35 stands), and
	// the arbiter test itself is byte-identical after T4 (hash of its
	// function body — a silent edit to the slice test fails here loudly).
	src, err := os.ReadFile("policy_authoring.go")
	if err != nil {
		t.Fatalf("read the door: %v", err)
	}
	if strings.Contains(string(src), "DecideProposal") {
		t.Error("the authoring door calls DecideProposal — the socket still never commits (S35); rows yes, decisions no")
	}
	body := arbiterTestBody(t)
	sum := sha256.Sum256([]byte(body))
	const want = "1147c19cbcfb816b3c340c0c2e9d8fd5fdd63f30a3dee0337f977949af448aea"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("cmd/aeon's TestProposalSliceAcrossAllKinds (the S35 arbiter test) is no longer byte-identical: sha256 %s, want %s — T4 must not have touched it, and if a later task did, it owes the same supersession discipline", got, want)
	}
}

// arbiterTestBody extracts the S35 slice test's function text (func line to
// closing brace at column zero — the awk the record hashes were taken with,
// ported to Go so the pin fails at TEST time, not at review time).
func arbiterTestBody(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../cmd/aeon/proposal_slice_test.go")
	if err != nil {
		t.Fatalf("read the arbiter test: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func TestProposalSliceAcrossAllKinds") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("TestProposalSliceAcrossAllKinds is gone from cmd/aeon/proposal_slice_test.go")
	}
	for i := start + 1; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n") + "\n"
		}
	}
	t.Fatal("the arbiter test never closes")
	return ""
}

// --- Behaviour 6: the crash-gap reconciliation read --------------------------

// TestPolicyChangesReportsUnauditedUserRules pins T4's fill of T3's field:
// a policy the audit cannot name is the crash-gap state (written, then the
// process died before its audit row) and `policy.changes` reports it by id
// rather than hiding it. The candidate set is the OPERATOR's own rows
// (origin user — every authoring door writes user-origin rows and owes
// them an audit row); a SEEDED row is not a candidate (T12's per-run
// `op='seed'` row covers the batch and names no single id — an empty
// PolicyID must never read as "names everything"), and an audited row is
// clean by construction.
func TestPolicyChangesReportsUnauditedUserRules(t *testing.T) {
	ctx := context.Background()
	d, _, s, e := authoringDaemon(t, authoringPeer)
	initConn(t, d, ctx, "peer-1", "s45-t4")

	// (a) an audited add through the door itself — clean.
	if _, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd,
		mustJSONDaemon(t, toolAllow("audited-row", "boss", "webfetch"))); err != nil {
		t.Fatalf("audited add: %v", err)
	}
	// (b) a user-origin row with NO audit row — the crash gap, written
	// straight through the boundary as the CLI's door would, minus the row.
	gap := policy.Policy{
		ID: "gap-row", Type: "tool_access",
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Deny, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	if rej := policy.NewStore(newPolicyRepo(s)).Add(ctx, gap, time.Now()); rej != nil {
		t.Fatalf("gap row: %s", rej.Reason)
	}
	// (c) seeded system rows — covered collectively by the run's one row.
	ensureCorpusAgentRow(t, s, "brief-author")
	svc := newAuthoringService(s, e, nil).(*authoringService)
	if _, err := svc.SeedCorpus(ctx, protocol.Actor{Kind: "user", ID: "4321"},
		protocol.SeedCorpusParams{Agents: []string{"brief-author"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyChanges, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("policy.changes: %v", err)
	}
	changes := res.(protocol.PolicyChangesResult)
	if len(changes.UnauditedPolicyIDs) != 1 || changes.UnauditedPolicyIDs[0] != "gap-row" {
		t.Fatalf("unauditedPolicyIds = %v, want exactly [gap-row] (the audited row is clean; the seeded rows are not candidates; an empty audit PolicyID names nothing)",
			changes.UnauditedPolicyIDs)
	}
}

// TestPolicyChangesUnauditedProbeAboveScanCap proves the OTHER branch:
// once the audit outgrew one bounded scan (more than scanCap
// rows), membership is answered by per-candidate probes, not by pretending
// the 500-row window is the whole table — the confident wrong answer there
// would report audited rows as gaps or gaps as clean.
func TestPolicyChangesUnauditedProbeAboveScanCap(t *testing.T) {
	// The bound is protocol's unexported policyChangeFullScanCap (the store
	// accessor caps a scan there too); 500 is the measured number this test
	// exists to cross.
	const scanCap = 500
	ctx := context.Background()
	d, _, s, _ := authoringDaemon(t, authoringPeer)
	initConn(t, d, ctx, "peer-1", "s45-t4")

	// 501 audited adds (each writes its row AND its audit row) — the scan
	// bound is 500; this puts the audit over it on purpose.
	for i := 0; i < scanCap+1; i++ {
		p := toolAllow(fmt.Sprintf("bulk-%03d", i), "boss", "webfetch")
		if _, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyAdd, mustJSONDaemon(t, p)); err != nil {
			t.Fatalf("bulk add %d: %v", i, err)
		}
	}
	if n, _ := s.PolicyChangeCount(ctx); n <= scanCap {
		t.Fatalf("the audit holds %d rows; the probe branch needs > %d", n, scanCap)
	}
	// One un-audited user row among them.
	gap := policy.Policy{
		ID: "silent-gap", Type: "tool_access",
		Target: policy.Target{Kind: policy.AgentKind, ID: "boss"},
		Action: policy.Deny, Source: policy.Operator,
		Spec: json.RawMessage(`{"toolName":"bash","state":"deny"}`),
	}
	if rej := policy.NewStore(newPolicyRepo(s)).Add(ctx, gap, time.Now()); rej != nil {
		t.Fatalf("gap row: %s", rej.Reason)
	}

	res, err := d.Table.Dispatch(ctx, "peer-1", protocol.MethodPolicyChanges, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("policy.changes: %v", err)
	}
	changes := res.(protocol.PolicyChangesResult)
	if len(changes.UnauditedPolicyIDs) != 1 || changes.UnauditedPolicyIDs[0] != "silent-gap" {
		t.Fatalf("probe branch: unauditedPolicyIds = %v, want exactly [silent-gap] (the probe path must find the gap among %d audited rows without truncating membership to the scan window)",
			changes.UnauditedPolicyIDs, scanCap+1)
	}
}
