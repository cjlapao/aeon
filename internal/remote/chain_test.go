package remote

// chain_test.go — S21 T7: the chain through a remote peer. AS6's edge, AS7's
// two refusals, and the question the local case never had to ask (Q4): a
// remote peer's internal fan-out is INVISIBLE to our graph, so depth counts
// OUR hops and the boundary is stated rather than implied. The assertions are
// therefore about two things at once: the ORDER around the graph door (policy
// → graph → edge → task → wire), and the fact that a remote ask's edge is a
// ROW in the same table the walk reads — which is what makes "survives a
// restart" the same claim S14 already proved, one node wider.

import (
	"context"
	"crypto/tls"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// chainEnv: the real store over a throwaway directory and a pinned loop whose
// steps can run as named agents — the same harness shape S14's chain tests
// used (copied pattern, not imported helpers: those live in agentsvc's test
// binary, and remote must not import agentsvc — AS11).
type chainEnv struct {
	s   *store.Store
	dir string
	st  loop.LoopState
}

func newChainEnv(t *testing.T) *chainEnv {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "chainlane.yaml")
	raw := "id: chainlane\nschemaVersion: 1\nversion: 1\nlanes:\n" +
		"  - id: work\n    step: {kind: model-step, agent: golang-engineer, produces: OUT.md}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Start(context.Background(), s, store.NewLoop{Slug: "chain-loop", ComponentRoot: root, Root: root, Goal: "delegate"}, path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &chainEnv{s: s, dir: dir, st: st}
}

func (e *chainEnv) stepAs(t *testing.T, agent string) int64 {
	t.Helper()
	step, err := e.s.CreateStep(store.Scope{LoopSlug: e.st.Slug, ComponentRoot: e.st.Root},
		store.NewStep{Phase: "model", Agent: agent, Attempt: 1, Status: store.StepRunning, Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return step.ID
}

// walkCheck mirrors S14's chain.go walk over the SAME store reads — the two
// refusals, in the same order (cycle first, then depth = up + 1 + down), at
// the same bound. In production the walker is agentsvc's and arrives injected
// (StoreChain.Check); the mirror exists here to prove the PRODUCTION claim
// from this side: the edges Ask records are rows inside the graph a real
// walk reads, so a remote ask is counted as a hop and a recorded callback
// edge really does close a cycle.
func walkCheck(s *store.Store, bound int) func(caller, callee string) error {
	reach := func(from, target string) bool {
		seen := map[string]bool{from: true}
		queue := []string{from}
		for len(queue) > 0 {
			h := queue[0]
			queue = queue[1:]
			edges, err := s.ChainEdgesByCallerAgent(h)
			if err != nil {
				panic(err)
			}
			for _, e := range edges {
				if e.CalleeAgent == target {
					return true
				}
				if !seen[e.CalleeAgent] {
					seen[e.CalleeAgent] = true
					queue = append(queue, e.CalleeAgent)
				}
			}
		}
		return false
	}
	var longest func(agent string, down bool, visiting map[string]bool) int
	longest = func(agent string, down bool, visiting map[string]bool) int {
		if visiting[agent] {
			return 0
		}
		visiting[agent] = true
		best := 0
		var edges []store.ChainEdge
		var err error
		if down {
			edges, err = s.ChainEdgesByCallerAgent(agent)
		} else {
			edges, err = s.ChainEdgesToCallee(agent)
		}
		if err != nil {
			panic(err)
		}
		for _, e := range edges {
			next := e.CalleeAgent
			if !down {
				next = e.CallerAgent
			}
			if d := longest(next, down, visiting); 1+d > best {
				best = 1 + d
			}
		}
		delete(visiting, agent)
		return best
	}
	return func(caller, callee string) error {
		if caller == callee {
			return &depthOrCycle{cycle: true, sentence: "chain refused: " + caller + " cannot ask its own skill — a cycle of one, fix the graph, not a cap"}
		}
		if reach(callee, caller) {
			return &depthOrCycle{cycle: true, sentence: "chain refused: asking " + callee + " from " + caller + " closes a cycle — " + caller + " → " + callee + " → " + caller + " — a structural mistake in the graph, not a cost failure"}
		}
		up := longest(caller, false, map[string]bool{})
		down := longest(callee, true, map[string]bool{})
		if d := up + 1 + down; d > bound {
			return &depthOrCycle{sentence: "chain refused: asking " + callee + " from " + caller + " would reach depth " + strconv.Itoa(d) + " and the bound is " + strconv.Itoa(bound) + " — a runaway, not a cycle"}
		}
		return nil
	}
}

type depthOrCycle struct {
	cycle    bool
	sentence string
}

func (e *depthOrCycle) Error() string { return e.sentence }

// orderChain wraps a Chain and records the ORDER of the two graph doors
// against a shared log; the logging assembler probes the store and the dial
// counter AT ASSEMBLY TIME, which is how "edge before wire" is observed
// without a sleep or a race.
type orderChain struct {
	inner Chain
	log   *[]string
}

func (c *orderChain) CheckAsk(caller, callee string) error {
	*c.log = append(*c.log, "check")
	return c.inner.CheckAsk(caller, callee)
}

func (c *orderChain) RecordEdge(in store.NewChainEdge) error {
	*c.log = append(*c.log, "edge")
	return c.inner.RecordEdge(in)
}

type probeAssembler struct {
	inner     Assembler
	log       *[]string
	s         *store.Store
	loopID    string
	stepID    int64
	dials     *atomic.Int32
	edgeAtAsk int
	conns     int32
}

func (a *probeAssembler) Assemble(spec InvokeSpec) ([]byte, error) {
	*a.log = append(*a.log, "assemble")
	n, err := a.s.CountAgentChainEdges(a.loopID, a.stepID)
	if err != nil {
		return nil, err
	}
	a.edgeAtAsk = n
	a.conns = a.dials.Load()
	return a.inner.Assemble(spec)
}

// remotePeer spins the fake TLS peer under its configured name.
func remotePeer(t *testing.T) (*Dialer, Peer, *atomic.Int32) {
	t.Helper()
	lowerBands(t, 2*time.Second)
	ca := newTestCA(t)
	key := newTestKey(t)
	leaf := ca.mintLeaf(t, "docs-peer", key, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	addr, _, _ := servingPeer(t, leaf, key, func(c *tls.Conn) {
		_, _ = io.Copy(io.Discard, c)
		_, _ = c.Write([]byte(`{"internalDepth":9,"internalAgents":40}`))
	})
	tries := countingDial(t)
	p := peerAt(addr, leaf)
	p.ID = "docs-peer"
	return dialerFor(ca), p, tries
}

func allowedRows(t *testing.T) func(InvokeSpec) ([]policy.Policy, error) {
	return allowedRowsFor(t, "boss")
}

// allowedRowsFor lets the named agents egress to any peer — the depth test
// asks under the agents deeper in its chain, and PL5 means every one of them
// needs the allow (the door judges the AGENT asking, and it should).
func allowedRowsFor(t *testing.T, agents ...string) func(InvokeSpec) ([]policy.Policy, error) {
	rows := make([]policy.Policy, 0, len(agents))
	for _, a := range agents {
		rows = append(rows, routeRow(t, "r-"+a, a, "allow", "remote:*", nil))
	}
	return rowsOf(rows...)
}

func askSpec(agent string, stepID int64) (InvokeSpec, AskOptions) {
	return InvokeSpec{Agent: agent, Loop: "chain-loop", Peer: "docs-peer", Skill: "deep-analysis", Args: []byte(`{"q":"x"}`)},
		AskOptions{CallerStepID: stepID, Depth: 1}
}

// Gate 1 — TestRemoteInvocationRecordsChainEdge: the ask records its edge in
// the REAL graph, marked remote, in the order the properties demand — and
// the edge SURVIVES a dial that fails (the recorded attempt is the whole
// point of AS6: we asked, even if the wire said no).
func TestRemoteInvocationRecordsChainEdge(t *testing.T) {
	env := newChainEnv(t)
	stepID := env.stepAs(t, "boss")
	var log []string
	d, peer, dials := remotePeer(t)
	gate := &EgressGate{Rows: allowedRows(t), Now: func() time.Time { return time.Now() }}
	chain := &orderChain{inner: &StoreChain{S: env.s, Check: func(caller, callee string) error {
		if callee != "remote:docs-peer" {
			t.Errorf("the walker was handed callee %q, want the canonical remote spelling", callee)
		}
		if strings.Contains(callee, "q") || strings.Contains(callee, "{") {
			t.Error("a payload fragment reached the bound")
		}
		return nil
	}}, log: &log}
	spec, opt := askSpec("boss", stepID)
	probe := &probeAssembler{inner: &spyAssembler{payload: []byte(`{"q":"x"}`)}, log: &log, s: env.s, loopID: "chain-loop", stepID: stepID, dials: dials}
	res := gate.Ask(context.Background(), chain, d, peer, probe, spec, opt)
	if res.Refused {
		t.Fatalf("ask refused: %s", res.Sentence)
	}
	// The order: both graph doors before the assembler, and at assembly time
	// the edge row ALREADY EXISTS while no dial has happened.
	if len(log) < 3 || log[0] != "check" || log[1] != "edge" || log[2] != "assemble" {
		t.Fatalf("order was %v, want check → edge → assemble", log)
	}
	if probe.edgeAtAsk != 1 {
		t.Errorf("edge rows at assembly time: %d, want 1 — the row must exist BEFORE the wire (AS6)", probe.edgeAtAsk)
	}
	if probe.conns != 0 {
		t.Errorf("dials at assembly time: %d, want 0", probe.conns)
	}
	// The row itself, read back through the chain's own reads.
	edges, err := env.s.ChainEdgesByCallerAgent("boss")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("edges: %d", len(edges))
	}
	e := edges[0]
	if e.CalleeAgent != "remote:docs-peer" {
		t.Errorf("callee %q — the edge must be MARKED remote where the graph is read, not beside it", e.CalleeAgent)
	}
	if !IsRemoteCallee(e.CalleeAgent) {
		t.Error("IsRemoteCallee disagrees with the row")
	}
	if e.SkillID != "deep-analysis" || e.Depth != 1 || e.CallerLoopID != "chain-loop" {
		t.Errorf("edge fields: %+v", e)
	}
	if RemoteCallee("docs-peer") != EgressSubject("docs-peer") {
		t.Error("the chain spelling and the policy spelling of a peer must be ONE string")
	}
	line := res.ChainLine()
	if !strings.Contains(line, "remote:docs-peer") || !strings.Contains(line, "not observable") || !strings.Contains(line, "docs-peer") {
		t.Errorf("ChainLine renders without the visibility boundary: %q", line)
	}
	// A dial that fails keeps the edge: the attempt was made and is recorded.
	dead := peer
	dead.Address = "127.0.0.1:1" // refused instantly; nothing listens
	spec2, opt2 := askSpec("boss", env.stepAs(t, "boss"))
	spec2.Skill = "second-analysis"
	res2 := gate.Ask(context.Background(), &orderChain{inner: &StoreChain{S: env.s, Check: func(string, string) error { return nil }}, log: &log}, d, dead, &spyAssembler{payload: []byte("x")}, spec2, opt2)
	if res2.Outcome.Kind == OutcomeResult {
		t.Fatal("a dead peer produced a result")
	}
	edges, _ = env.s.ChainEdgesByCallerAgent("boss")
	if len(edges) != 2 {
		t.Fatalf("the failed ask erased/failed to record its edge: %d rows, want 2 — AS6 records attempts, not successes", len(edges))
	}
}

// Gate 2 — TestDepthCountsOurHops: the bound reads ROWS. Our four hops
// (including the remote one) pass; a fifth of OURS is refused; and what the
// peer CLAIMS about its internals — the payload above reports internalDepth 9
// across 40 agents — changes nothing, because there are no rows under the
// remote node and a bound reads rows, not claims.
func TestDepthCountsOurHops(t *testing.T) {
	env := newChainEnv(t)
	d, peer, dials := remotePeer(t)
	gate := &EgressGate{Rows: allowedRowsFor(t, "boss", "a1", "a2", "a3", "a4"), Now: func() time.Time { return time.Now() }}
	check := walkCheck(env.s, 4)
	// Lay a local chain: boss → a1 → a2 → a3, all through the SAME RecordEdge
	// path (edges recorded by THIS package's door must be rows the walk reads).
	for _, pair := range [][2]string{{"boss", "a1"}, {"a1", "a2"}, {"a2", "a3"}} {
		step := env.stepAs(t, pair[0])
		if err := (&StoreChain{S: env.s}).RecordEdge(store.NewChainEdge{
			CallerLoopID: env.st.ID, CallerStepID: step, CalleeAgent: pair[1], SkillID: "do-thing", Depth: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// a3 asks the peer: our hops = 3 up + 1 = 4 = at the bound → allowed, and
	// the peer's self-reported internal depth of 9 is NOT added (a fifth hop
	// would have refused; it did not — the bound counted OUR edges).
	spec, opt := askSpec("a3", env.stepAs(t, "a3"))
	res := gate.Ask(context.Background(), &StoreChain{S: env.s, Check: check}, d, peer, &spyAssembler{payload: []byte("x")}, spec, opt)
	if res.Refused {
		t.Fatalf("the remote ask was refused at the bound (%s) — the bound must count our hops only; the peer's claimed internals are not rows", res.Sentence)
	}
	if under := mustEdges(t, env.s, "remote:docs-peer"); len(under) != 0 {
		t.Errorf("the peer's claimed fan-out appeared as rows: %+v", under)
	}
	if dials.Load() == 0 {
		t.Error("the allowed ask never reached the wire")
	}
	// A fifth of OUR hops: boss → a1..a4, then a4 asks the peer → 5 > 4.
	step := env.stepAs(t, "a3")
	if err := (&StoreChain{S: env.s}).RecordEdge(store.NewChainEdge{
		CallerLoopID: env.st.ID, CallerStepID: step, CalleeAgent: "a4", SkillID: "do-thing", Depth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	before := dials.Load()
	spec5, opt5 := askSpec("a4", env.stepAs(t, "a4"))
	res5 := gate.Ask(context.Background(), &StoreChain{S: env.s, Check: check}, d, peer, &spyAssembler{payload: []byte("x")}, spec5, opt5)
	if !res5.Refused {
		t.Fatal("a fifth of our own hops passed — the bound is decoration")
	}
	if !strings.Contains(res5.Sentence, "depth 5") {
		t.Errorf("the refusal must name the depth it counted: %q", res5.Sentence)
	}
	if !strings.Contains(res5.Sentence, NoFileReadLine) {
		t.Errorf("a refusal that happened before assembly must still carry the guarantee line: %q", res5.Sentence)
	}
	if res5.Edge != nil {
		t.Error("the refused ask recorded an edge — an illegal ask leaves no trace to audit")
	}
	if dials.Load() != before {
		t.Error("the refused ask reached the wire")
	}
}

// Gate 3 — TestObservableCycleThroughRemoteRefused: the cycle rule reads
// rows, so when a peer's call-back INTO us appears in the graph, AS7's cycle
// refusal fires normally — before the edge, before the wire. And its twin: a
// cycle the peer merely REPORTS inside itself is not in the rows, is not
// observable, and is not ours — the ask proceeds, because refusing on a
// claim would be guessing dressed as caution.
func TestObservableCycleThroughRemoteRefused(t *testing.T) {
	env := newChainEnv(t)
	d, peer, dials := remotePeer(t)
	gate := &EgressGate{Rows: allowedRows(t), Now: func() time.Time { return time.Now() }}
	// The observable case: docs-peer called back into us, and that invocation
	// is a recorded edge — a step run under the remote node's name asking
	// boss. (Where such rows come from is the inbound door's business; the
	// graph rule is THIS file's, and it reads rows.)
	cbStep := env.stepAs(t, "remote:docs-peer")
	if err := (&StoreChain{S: env.s}).RecordEdge(store.NewChainEdge{
		CallerLoopID: env.st.ID, CallerStepID: cbStep, CalleeAgent: "boss", SkillID: "serve", Depth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	bossStep := env.stepAs(t, "boss")
	spec, opt := askSpec("boss", bossStep)
	res := gate.Ask(context.Background(), &StoreChain{S: env.s, Check: walkCheck(env.s, 4)}, d, peer,
		&spyAssembler{payload: []byte("x"), t: t}, spec, opt)
	if !res.Refused {
		t.Fatal("the observable cycle passed — the rule must read rows wherever they came from")
	}
	if !strings.Contains(res.Sentence, "closes a cycle") || !strings.Contains(res.Sentence, "remote:docs-peer") {
		t.Errorf("the cycle refusal must name the path: %q", res.Sentence)
	}
	if res.Edge != nil {
		t.Error("the cycle ask recorded an edge — no trace to audit")
	}
	if n := countEdgesFor(t, env.s, "chain-loop", bossStep); n != 0 {
		t.Errorf("the refused ask left %d edge rows", n)
	}
	if dials.Load() != 0 {
		t.Error("the cycle ask reached the wire")
	}
	// The unobservable twin, on a FRESH graph with NO callback row in it: the
	// fake peer's payload LIES about a 9-deep internal cycle; the walk reads
	// rows, finds nothing, and the ask proceeds. (Not the env above — that
	// one genuinely contains a cycle through us, and refusing there above is
	// the point; the twin must differ in exactly the rows.)
	env2 := newChainEnv(t)
	spec2, opt2 := askSpec("boss", env2.stepAs(t, "boss"))
	spec2.Skill = "another-ask"
	res2 := gate.Ask(context.Background(), &StoreChain{S: env2.s, Check: walkCheck(env2.s, 4)}, d, peer, &spyAssembler{payload: []byte("x")}, spec2, opt2)
	if res2.Refused {
		t.Fatalf("the ask was refused on the peer's SELF-REPORTED internal cycle — not observable, not ours: %s", res2.Sentence)
	}
}

// TestRemoteEdgeSurvivesRestart: AS6's promise, one node wider. The chain
// with a remote edge is a row like any other — it survives the restart that
// deletes every in-memory answer, and the graph reads it back IDENTICALLY.
func TestRemoteEdgeSurvivesRestart(t *testing.T) {
	env := newChainEnv(t)
	d, peer, _ := remotePeer(t)
	gate := &EgressGate{Rows: allowedRows(t), Now: func() time.Time { return time.Now() }}
	spec, opt := askSpec("boss", env.stepAs(t, "boss"))
	res := gate.Ask(context.Background(), &StoreChain{S: env.s, Check: walkCheck(env.s, 4)}, d, peer, &spyAssembler{payload: []byte("x")}, spec, opt)
	if res.Refused {
		t.Fatalf("ask refused: %s", res.Sentence)
	}
	before, err := env.s.ChainEdgesByCallerAgent("boss")
	if err != nil || len(before) != 1 {
		t.Fatalf("edges before: %v", err)
	}
	// Close and reopen the DIRECTORY — the same thing a daemon restart does.
	if err := env.s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := store.Open(store.Options{Dir: env.dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	after, err := s2.ChainEdgesByCallerAgent("boss")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("edges after restart: %d, want 1 — the row IS the chain", len(after))
	}
	if after[0].CalleeAgent != before[0].CalleeAgent || after[0].SkillID != before[0].SkillID ||
		after[0].Depth != before[0].Depth || after[0].CallerLoopID != before[0].CallerLoopID {
		t.Errorf("the remote edge came back different: %+v vs %+v", after[0], before[0])
	}
	// And the graph's verdict survives too: the same walk, fresh from rows,
	// still sees the ask as legal (no cycle) and the edge as one of our hops.
	if err := walkCheck(s2, 4)("boss", "remote:docs-peer"); err != nil {
		t.Errorf("post-restart walk changed its mind: %v", err)
	}
	if d := walkDepthOne(t, s2, "boss"); d != 1 {
		t.Errorf("post-restart depth: %d, want 1 — our hop, counted from rows", d)
	}
}

func walkDepthOne(t *testing.T, s *store.Store, agent string) int {
	t.Helper()
	edges, err := s.ChainEdgesByCallerAgent(agent)
	if err != nil {
		t.Fatal(err)
	}
	return len(edges)
}

func mustEdges(t *testing.T, s *store.Store, caller string) []store.ChainEdge {
	t.Helper()
	edges, err := s.ChainEdgesByCallerAgent(caller)
	if err != nil {
		t.Fatal(err)
	}
	return edges
}

func countEdgesFor(t *testing.T, s *store.Store, loopID string, stepID int64) int {
	t.Helper()
	n, err := s.CountAgentChainEdges(loopID, stepID)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
