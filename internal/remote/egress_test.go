package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

// The spies are the test. RA4's whole argument is an ORDER — RouteAccess
// before assembly — and an order is only observable at a boundary you can
// watch: the assembler is an interface, the files it reads are counted, and
// the raw-dial seam counts the socket. A denial's proof is three zeroes.

type spyAssembler struct {
	calls   int
	t       *testing.T
	payload []byte
	err     error
	// fsReads counts os.ReadFile calls the spy made — the filesystem spy of
	// D5's concrete form: an assembled-and-discarded request has already
	// READ, so "no file was read" is only a sentence a counter can keep.
	fsReads *atomic.Int32
	readDir string
}

func (s *spyAssembler) Assemble(spec InvokeSpec) ([]byte, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.readDir != "" && s.fsReads != nil {
		// What the real assembler would do: read the material the skill asked
		// for. The counter is the test's whole interest in the bytes.
		ents, _ := os.ReadDir(s.readDir)
		for _, e := range ents {
			if _, err := os.ReadFile(s.readDir + "/" + e.Name()); err == nil {
				s.fsReads.Add(1)
			}
		}
	}
	return s.payload, nil
}

func routeRow(t *testing.T, id, targetID, action, route string, notAfter *time.Time) policy.Policy {
	t.Helper()
	target, rej := policy.ParseTarget(policy.AgentKind, targetID)
	if rej != nil {
		t.Fatalf("target: %s", rej.Reason)
	}
	spec, err := json.Marshal(map[string]string{"route": route, "state": action})
	if err != nil {
		t.Fatal(err)
	}
	return policy.Policy{ID: id, Type: policy.RouteAccess, Target: target,
		Action: policy.Action(action), Spec: spec, Source: policy.Operator, NotAfter: notAfter}
}

func rowsOf(rows ...policy.Policy) func(InvokeSpec) ([]policy.Policy, error) {
	return func(InvokeSpec) ([]policy.Policy, error) { return rows, nil }
}

func specFor(peer string) InvokeSpec {
	return InvokeSpec{Agent: "boss", Peer: peer, Skill: "docs.search", Args: []byte(`{"q":"x"}`)}
}

// livePeer spins the answering fake peer and hands back everything the
// allowed-path assertions need: the dialer, the configured peer, and the
// raw-dial counter that proves "denied means nothing sent".
func livePeer(t *testing.T) (*Dialer, Peer, *atomic.Int32) {
	t.Helper()
	lowerBands(t, 2*time.Second)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "peer.example", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) {
		_, _ = io.Copy(io.Discard, c)
		_, _ = c.Write([]byte("pong"))
	})
	tries := countingDial(t)
	return dialerFor(ca), peerAt(addr, leaf), tries
}

func TestRouteAccessEvaluatedBeforeAssembly(t *testing.T) {
	d, peer, tries := livePeer(t)

	// DENIED: the spy never fired, the files were never read, the socket was
	// never touched — the order is the result.
	spy := &spyAssembler{t: t, payload: []byte("request-with-transcripts")}
	gate := &EgressGate{Rows: rowsOf(routeRow(t, "no-egress", "boss", "deny", "remote:docs-peer", nil))}
	res := gate.Invoke(context.Background(), d, peer, spy, specFor("docs-peer"))
	if !res.Refused {
		t.Fatalf("a deny row did not refuse the egress: %+v", res)
	}
	if spy.calls != 0 {
		t.Errorf("the assembler was called %d times on a DENY — an assembled-and-discarded request has already read the files (RA4)", spy.calls)
	}
	if tries.Load() != 0 {
		t.Errorf("a denied egress dialled %d times — denied means NOTHING SENT", tries.Load())
	}
	// CONTROL (the pin can fail): the same gate with no deny row assembles
	// and calls — the zeroes above are the deny's doing, not a broken spy.
	spy2 := &spyAssembler{t: t, payload: []byte("request")}
	gate2 := &EgressGate{Rows: rowsOf(routeRow(t, "all-egress", "boss", "allow", "remote:*", nil))}
	res2 := gate2.Invoke(context.Background(), d, peer, spy2, specFor("docs-peer"))
	if res2.Refused || spy2.calls != 1 || tries.Load() != 1 {
		t.Errorf("allowed egress: refused=%v assembler=%d dials=%d — the control must fire", res2.Refused, spy2.calls, tries.Load())
	}
	if res2.Outcome.Kind != OutcomeResult || string(res2.Outcome.Payload) != "pong" {
		t.Errorf("the allowed path must reach the peer: %s", res2.Outcome.Render())
	}
}

func TestDeniedEgressReadsNoFiles(t *testing.T) {
	d, peer, tries := livePeer(t)
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/transcript.jsonl", []byte("the product's own material"), 0o600); err != nil {
		t.Fatal(err)
	}
	reads := &atomic.Int32{}
	spy := &spyAssembler{t: t, payload: []byte("x"), fsReads: reads, readDir: dir}
	gate := &EgressGate{Rows: rowsOf(routeRow(t, "no-egress", "boss", "deny", "remote:docs-peer", nil))}
	res := gate.Invoke(context.Background(), d, peer, spy, specFor("docs-peer"))
	if !res.Refused {
		t.Fatal("want refusal")
	}
	if reads.Load() != 0 {
		t.Errorf("the filesystem spy counted %d reads on a DENIED egress — the data left the file and entered the process; the socket was never the boundary", reads.Load())
	}
	if tries.Load() != 0 {
		t.Errorf("socket touched %d times on a denied egress", tries.Load())
	}
	if !strings.Contains(res.Sentence, NoFileReadLine) {
		t.Errorf("the refusal must state the guarantee it kept: %q", res.Sentence)
	}
}

func TestRefusalCarriesPolicySentence(t *testing.T) {
	rows := []policy.Policy{routeRow(t, "no-egress", "boss", "deny", "remote:docs-peer", nil)}
	gate := &EgressGate{Rows: rowsOf(rows...)}
	dec := gate.Admit(specFor("docs-peer"))
	if dec.Allowed {
		t.Fatal("want refusal")
	}
	// S10's sentence, verbatim — computed by the engine itself, so this test
	// pins that remote REUSES the one formatter rather than paraphrasing it.
	req := policy.Request{Type: policy.RouteAccess, Agent: "boss", Route: EgressSubject("docs-peer")}
	want := policy.Evaluate(rows, req, time.Time{}).Sentence() + "\n" + NoFileReadLine
	if dec.Sentence != want {
		t.Errorf("refusal is\n%q\nwant engine sentence + guarantee line\n%q", dec.Sentence, want)
	}
}

func TestRouteAccessCanNameASpecificPeerAndAWildcard(t *testing.T) {
	gate := &EgressGate{Rows: rowsOf(
		routeRow(t, "peers-open", "boss", "allow", "remote:*", nil),
		routeRow(t, "evil-shut", "boss", "deny", "remote:evil", nil),
	)}
	if dec := gate.Admit(specFor("docs-peer")); !dec.Allowed {
		t.Errorf("the wildcard allow must cover docs-peer: %q", dec.Sentence)
	}
	if dec := gate.Admit(specFor("evil")); dec.Allowed {
		t.Errorf("deny over allow (PL4): the named peer must be shut even under a wildcard allow: %q", dec.Sentence)
	}
	// An exact row is exact: allowing ONLY alpha does not open beta.
	exact := &EgressGate{Rows: rowsOf(routeRow(t, "only-alpha", "boss", "allow", "remote:alpha", nil))}
	if dec := exact.Admit(specFor("alpha")); !dec.Allowed {
		t.Errorf("named peer under its own allow: %q", dec.Sentence)
	}
	if dec := exact.Admit(specFor("beta")); dec.Allowed {
		t.Errorf("remote:alpha must never reach remote:beta — the prefix IS the identity: %q", dec.Sentence)
	}
}

func TestNoNewPolicyTypeWasAdded(t *testing.T) {
	// types.go, read from the neighbouring package: the closed set still holds
	// exactly seven types, none of them named for egress.
	raw, err := os.ReadFile("../policy/types.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, banned := range []string{`PolicyType = "egress"`, `PolicyType = "agent_egress"`, `PolicyType = "remote_access"`} {
		if strings.Contains(src, banned) {
			t.Errorf("types.go declares %s — RA4 says the SUBJECT is extended, no second authorship story arrives", banned)
		}
	}
	if got := len(policy.AllPolicyTypes()); got != 7 {
		t.Errorf("the closed set holds %d types, want 7 (PL3)", got)
	}
	// And the gate actually asked the ENGINE's type — the extension lives in
	// the route VALUE, not a new constant.
	gate := &EgressGate{Rows: func(spec InvokeSpec) ([]policy.Policy, error) {
		if spec.Peer == "" {
			return nil, errors.New("empty")
		}
		// capture via rows closure: the gate builds the request itself; this
		// test verifies the request it BUILDS by evaluating a row that only
		// the RouteAccess type can match.
		return rowsOf(routeRow(t, "cap", "boss", "allow", "remote:*", nil))(spec)
	}}
	if dec := gate.Admit(specFor("docs-peer")); !dec.Allowed {
		t.Errorf("the gate's request did not carry the RouteAccess type into the engine: %q", dec.Sentence)
	}
}

func TestEgressEvaluatedPerInvocation(t *testing.T) {
	// Scope 5: a peer allowed yesterday is denied today, and the mechanism is
	// the row's NotAfter — which only PER-INVOCATION evaluation can show.
	notAfter := time.Date(2026, 10, 28, 12, 30, 0, 0, time.UTC)
	rows := []policy.Policy{routeRow(t, "morning-only", "boss", "allow", "remote:docs-peer", &notAfter)}
	now := &atomic.Value{}
	var clock time.Time = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	now.Store(clock)
	gate := &EgressGate{Rows: rowsOf(rows...), Now: func() time.Time { v := now.Load().(time.Time); return v }}
	if dec := gate.Admit(specFor("docs-peer")); !dec.Allowed {
		t.Fatalf("the 12:00 invocation must be allowed: %q", dec.Sentence)
	}
	now.Store(time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC)) // past the row's NotAfter
	dec := gate.Admit(specFor("docs-peer"))
	if dec.Allowed {
		t.Fatal("the 13:00 invocation of the SAME gate must be denied — a cached verdict is a permission that outlived its grant")
	}
	if !strings.Contains(dec.Sentence, NoFileReadLine) {
		t.Errorf("the expired denial still keeps the guarantee line: %q", dec.Sentence)
	}
	// Same rows, same gate object — nothing was rebuilt between the calls.
	_ = clock
}

func TestEmptyPolicySetDeniesAndUnreadableFailsClosed(t *testing.T) {
	// PL5: absence of rules is denial, not permission.
	gate := &EgressGate{Rows: rowsOf()}
	if dec := gate.Admit(specFor("docs-peer")); dec.Allowed {
		t.Error("no rules must deny egress (PL5) — peers are off by default, and unruled is off")
	}
	// Rows that cannot be read must NOT be read as rows that allow.
	broken := &EgressGate{Rows: func(InvokeSpec) ([]policy.Policy, error) { return nil, errors.New("store on fire") }}
	dec := broken.Admit(specFor("docs-peer"))
	if dec.Allowed || !strings.Contains(dec.Sentence, "could not be read") || !strings.Contains(dec.Sentence, "failing closed") || !strings.Contains(dec.Sentence, NoFileReadLine) {
		t.Errorf("unreadable policy must fail closed with a legible refusal, got %q", dec.Sentence)
	}
}

func TestEgressSubjectCanonicalSpelling(t *testing.T) {
	if got := EgressSubject("docs-peer"); got != "remote:docs-peer" {
		t.Errorf("the canonical route spelling is %q, want remote:docs-peer — both ends of the string are pinned by tests on their own side", got)
	}
	// A deny row spelled with the canonical prefix matches what Admit builds
	// (the two ends agreeing is the whole subject extension).
	gate := &EgressGate{Rows: rowsOf(routeRow(t, "d", "boss", "deny", EgressSubject("docs-peer"), nil))}
	if dec := gate.Admit(specFor("docs-peer")); dec.Allowed {
		t.Errorf("a deny row built from EgressSubject must refuse the gate's own subject: %q", dec.Sentence)
	}
}
