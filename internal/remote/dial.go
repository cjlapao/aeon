package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/label"
	"github.com/cjlapao/aeon/internal/store"
)

// dial.go is where Aeon first puts bytes onto a machine it does not own, so
// the three properties this file carries are refusals to be helpful.
//
// **No plaintext mode (RA1).** Not a flag, not a scheme, not an exception for
// one host — a plaintext option is the one somebody uses in production, and
// the local case already has a unix socket with filesystem permissions and
// peer credentials (P26), which is strictly better than a plaintext TCP
// shortcut to the same machine. The grep test bans the cleartext schemes,
// the bare dial form, the HTTP client package, and any exception for the
// loopback host from this file.
//
// **One path, and it verifies (the ordering risk the plan names).** There is
// exactly ONE function that reaches the wire (dialVerified), it installs
// T2's pin hook unconditionally, and a peer with no Pin — one that slipped
// past the config door by struct-literal — is REFUSED rather than dialed
// unverified. A dial that worked without verification during development is
// the path that persists into production; this file is written so that path
// has never existed to persist.
//
// **A hung peer degrades; it does not hang (RA6, MC9).** Remotely the
// failure is somebody else's outage arriving on a schedule nobody controls,
// so the three outcomes — result, deadline, error — are three distinct
// renderings, and a failed call is never retried: a retry doubles the wait
// and the model cannot tell one outage from two. If a retry is wanted it is
// the model's call, visibly, above this file.
//
// One connection per call, closed on return. That is what "async is
// suspension, never a held connection" (RA7) means on the wire: there is no
// connection here to hold.
//
// Q3 — the deadline, per-call and per-Task, as numbers (answered here):
//
//   - Per-call (the synchronous invoke path): CONNECT 5 s, CALL 30 s. The
//     call bound is the same number MCP's degrade.go chose for the same
//     shape of thing — a third-party network round-trip, an order of
//     magnitude past "healthy" and short enough that a step's human sees
//     progress — and deliberately far short of bash's 600s, because the
//     failure modes differ: a local build runs for minutes; a peer that has
//     not answered in half a minute is not about to.
//   - Per-Task (the async path RA7 creates): 1 hour, and it is NOT a
//     connection timeout — there is no connection. It is an expiry stamped
//     at suspension and evaluated when a completion arrives (T6's callback)
//     or when the Task is swept: the point is that "the peer never came
//     back" becomes a fact on a schedule, not an open question forever.
//
// Both bounds are caller-shortenable, never caller-lengthenable (bash's rule
// verbatim: a tool that could raise its own ceiling has no ceiling).

// The numbers Q3 asks for. Named, so the DEMO can print them and the tests
// can lower them without editing the argument.
const (
	DialTimeout        = 5 * time.Second
	DefaultCallTimeout = 30 * time.Second
	DefaultTaskExpiry  = time.Hour
)

// Seams, the house shape (mcp degrade.go): tests lower the bound so
// "bounded" is testable without thirty-second waits. Nothing reads them
// upward.
var (
	callTimeout = DefaultCallTimeout
	dialTimeout = DialTimeout
)

// rawDial is the seam the tests count through — the ONLY place this package
// touches net, and even here only to hand the bytes to TLS. The bare net.Dial
// form is banned by the grep test; net.Dialer is used, with its context.
var rawDial = func(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.DialContext(ctx, "tcp", addr)
}

// OutcomeKind is the closed three (MC9): a result, a deadline, an error.
// The strings are the renderings' first word, so a transcript shows WHICH
// happened rather than a shape the model must guess at.
type OutcomeKind string

// The three outcomes, as data. A fourth would be a lie — every call ends as
// one of these.
const (
	OutcomeResult   OutcomeKind = "result"
	OutcomeDeadline OutcomeKind = "deadline"
	OutcomeError    OutcomeKind = "error"
)

// Outcome is one call's whole truth. T5 labels its payload's trust, T7
// records its chain edge, T8 renders its cost — all as methods in their own
// files over this one struct; nobody reshapes the wire's verdict after the
// fact.
type Outcome struct {
	Peer    string
	Kind    OutcomeKind
	Payload []byte        // result only — and an EMPTY result is still a result, with its own rendering
	Bound   time.Duration // deadline only — the number that fired, spoken
	Err     error         // error only — named, never swallowed
	Took    time.Duration
}

// Render is THE rendering (MC9's discipline, one formatter like policy's
// Verdict.Sentence): the deadline says the peer did not answer, and refuses
// to let that read as the peer reporting nothing; an empty result says the
// peer answered and reported nothing. The two sentences CANNOT be confused —
// each names the other's meaning and rejects it.
func (o Outcome) Render() string {
	switch o.Kind {
	case OutcomeDeadline:
		return "deadline: remote peer \"" + o.Peer + "\" did not answer within " + o.Bound.String() +
			" — the peer not answering is a different fact from the peer reporting nothing; " +
			"no retry was made, and another attempt is the model's call to make, visibly"
	case OutcomeError:
		why := "connection failed"
		if o.Err != nil {
			why = o.Err.Error()
		}
		return "error: remote peer \"" + o.Peer + "\" failed — " + why + " — no retry was made"
	default: // result
		if len(o.Payload) == 0 {
			return "result: remote peer \"" + o.Peer + "\" answered with an EMPTY result — the peer reported nothing, " +
				"which is the peer's answer, not a timeout dressed up as one"
		}
		return "result: remote peer \"" + o.Peer + "\" answered with " + itoa(len(o.Payload)) + " bytes"
	}
}

// DegradeNote is scope 6's report: a dead peer's skills are absent from what
// the agent can ask for, AND the absence is spoken — a skill silently
// missing is a model wondering why it cannot do what its prompt says it
// can. A live peer degrades nothing, and the note says so by being empty.
func (o Outcome) DegradeNote() string {
	if o.Kind == OutcomeResult {
		return ""
	}
	return "remote peer \"" + o.Peer + "\" is unavailable — " + o.Render() +
		"; every skill it advertises is absent from this step's reach, and the absence is reported here rather than left silent (RA6/MC9)"
}

// Dialer carries what a dial needs beyond the peer: the roots the chain
// verifies against (nil = the system pool — which is NECESSARY and never
// SUFFICIENT, because the pin is the identity half, RA2) and the clock.
type Dialer struct {
	// Roots is the test seam and the future operator seam for a private test
	// PKI (the fake peer's CA). It replaces the POOL, never the VERIFICATION:
	// every root set still runs chain + expiry + hostname + the pin.
	Roots func() (*x509.CertPool, error)
	Now   func() time.Time
}

func (d *Dialer) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dialer) roots() (*x509.CertPool, error) {
	if d.Roots != nil {
		return d.Roots()
	}
	return nil, nil // the system pool decides the chain half; the pin decides identity
}

// Call is the one synchronous exchange: one connection, one request, one
// response, closed. It NEVER returns an unlabelled error — the three
// outcomes are the return, and the caller cannot forget which one happened
// because there is no other return value to forget.
//
// Never retried, in code and by character: ONE pass at every stage. (A loop
// here would be the N5 violation the validation names; the counting seam
// makes "exactly one" a measured fact in the test file.)
func (d *Dialer) Call(ctx context.Context, peer Peer, request []byte) Outcome {
	started := d.now()
	ctx, cancel, bound := boundCall(ctx)
	defer cancel()

	roots, err := d.roots()
	if err != nil {
		return Outcome{Peer: peer.ID, Kind: OutcomeError, Err: err, Took: d.now().Sub(started)}
	}
	conn, err := dialVerified(ctx, peer, roots)
	if err == nil {
		defer conn.Close()
		if err = conn.SetDeadline(deadlineOf(ctx)); err == nil {
			if _, err = conn.Write(request); err == nil {
				// Half-close the write: the peer sees the end of the request
				// without us holding the read open past its answer.
				if cw, ok := conn.NetConn().(interface{ CloseWrite() error }); ok {
					_ = cw.CloseWrite()
				}
				var payload []byte
				payload, err = io.ReadAll(conn)
				if err == nil {
					return Outcome{Peer: peer.ID, Kind: OutcomeResult, Payload: payload, Took: d.now().Sub(started)}
				}
			}
		}
	}
	took := d.now().Sub(started)
	if causedByDeadline(err, ctx) {
		return Outcome{Peer: peer.ID, Kind: OutcomeDeadline, Bound: bound, Took: took}
	}
	if err == nil {
		err = errors.New("unknown transport failure")
	}
	return Outcome{Peer: peer.ID, Kind: OutcomeError, Err: err, Took: took}
}

// dialVerified is THE dial path (scope 2, asserted by grep and by shape):
// the only place that reaches net, the only place that builds a tls.Config,
// and it installs T2's pin hook UNCONDITIONALLY. A peer with no pin never
// gets here by validation; getting here by struct literal still dials
// nothing — refusal is the only behaviour that path has ever had.
func dialVerified(ctx context.Context, peer Peer, roots *x509.CertPool) (*tls.Conn, error) {
	if peer.Pin == nil {
		return nil, errors.New("refused: peer \"" + peer.ID + "\" carries no pin — Validate refuses such a peer at configuration time, so one reaching the dial is a bug, and the answer is to refuse, never to dial unverified (RA2)")
	}
	host, _, err := net.SplitHostPort(peer.Address)
	if err != nil {
		return nil, err
	}
	conn, err := rawDial(ctx, peer.Address)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots, // nil = system pool; a pool is the chain half, never the identity half
		// The pin is ADDITIONAL: chain, expiry and hostname verify against
		// the pool first (T2 scope 5), then this hook narrows identity.
		VerifyPeerCertificate: VerifierFor(peer.ID, *peer.Pin),
	}
	tlsConn := tls.Client(conn, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// boundCall: no deadline gets in. The caller's shorter bound always wins;
// this package cannot be talked into an unbounded call by a careless
// context.Background() — and no caller can talk it past callTimeout either.
func boundCall(ctx context.Context) (context.Context, context.CancelFunc, time.Duration) {
	bound := time.Duration(callTimeout)
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining < bound {
			bound = remaining
		}
	}
	cctx, cancel := context.WithTimeout(ctx, bound)
	return cctx, cancel, bound
}

func deadlineOf(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Now().Add(time.Duration(callTimeout))
}

// causedByDeadline separates the third outcome from the fourth: the bound
// firing is a DIFFERENT fact from a connection that broke, and this is where
// that separation is decided — once, here, not at every render site.
func causedByDeadline(err error, ctx context.Context) bool {
	if ctx.Err() != nil {
		return true // the bound fired, whatever shape the socket error took
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded)
}

// TaskExpired is Q3's async half: an async Task carries NO connection
// timeout (RA7 removed the connection; a connection timeout on a thing with
// no connection is a bug with a nice name). It carries an expiry instead —
// DefaultTaskExpiry from its creation — evaluated when a completion arrives
// (T6 refuses expired completions) or when the Task is swept. Both numbers,
// as the task demands: 5 s connect, 30 s call, 1 h task.
func (d *Dialer) TaskExpired(created time.Time) bool {
	return d.now().Sub(created) > DefaultTaskExpiry
}

func itoa(n int) string {
	// tiny, so no strconv import for one number; decimal, non-negative only
	// (payload length can never be negative).
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// --- T5: remote results are untrusted, always (RA5) ------------------------
//
// AS12 labels a non-first-party skill result; AP9 made first-party a registry
// field set by us, never read from the package, because a package that could
// claim it would skip the labelling. For a remote peer there is NO CLAIM TO
// EVALUATE AT ALL: first-party means reviewed in our pull request — a property
// of CODE — and nothing on another machine qualifies, whatever it says about
// itself. So there is no code path by which a remote result is unlabelled:
// not a flag, not a trusted-peer list, not a same-organisation exception. The
// grep test (untrusted_test.go) holds the absence; the renderer below is the
// presence, and it is S14's mechanism — internal/label's ONE grammar
// (W23/MC7/AS12/TR10 and this are one rule on five surfaces), imported, never
// re-implemented.

// Labelled renders the call's payload the way a model should receive it:
// verbatim inside the shared untrusted region, labelled with the PEER's name
// (a transcript mixing results from two peers stays readable). The absence
// renderings — deadline and error — are OUR OWN sentences about a peer that
// never spoke; they are not a stranger's data and ride unlabelled, the same
// reasoning that keeps S15's refusal text out of the region (wrapping our
// own words would hand the label back to the very text that replaces the
// stranger). Content is byte-for-byte verbatim: labelling is not filtering.
func (o Outcome) Labelled() string {
	switch o.Kind {
	case OutcomeResult:
		return label.Region("skill result from remote peer \""+o.Peer+"\" — untrusted (RA5): first-party is a property of code reviewed in a pull request, and nothing on another machine qualifies", string(o.Payload))
	default:
		return o.Render()
	}
}

// claimMarkers are what a peer can say about itself that we must notice and
// ignore. Over-reporting is the safe direction (P31): a result that merely
// DISCUSSES first-party status gets the same visible note as one claiming it,
// because suppressing the note to dodge false positives is one judgement call
// away from suppressing it for the true positives too.
var claimMarkers = []string{"first-party", "firstparty", "first_party"}

// Provenance states where the bytes came from, in the three honest states
// the surfaces render: the verdict for a result, plus the visible ignoring —
// a peer that TRIED to claim first-party is worth knowing about, so the
// attempt is recorded in the provenance sentence rather than silently
// dropped on the floor where nobody audits it.
func (o Outcome) Provenance() string {
	switch o.Kind {
	case OutcomeResult:
		base := "untrusted · remote peer \"" + o.Peer + "\" · nothing on another machine is first-party (RA5)"
		lower := strings.ToLower(string(o.Payload))
		for _, m := range claimMarkers {
			if strings.Contains(lower, m) {
				return base + " · peer claimed first-party — ignored, and the attempt is reported"
			}
		}
		return base
	default:
		return "aeon · generated locally — the peer did not answer; this sentence is ours, not a stranger's"
	}
}

// ---------------------------------------------------------------------------
// The chain through a remote peer (S21 T7 — AS6/AS7 extended; design Q4).
//
// A remote invocation is a chain edge like any other: AS6's rule is that the
// ask is RECORDED before the callee runs, so a crash mid-invocation leaves a
// traceable attempt rather than an untraceable cost, and the row — not a
// memory — is where the graph lives, which is what makes the whole structure
// survive a restart. A remote ask writes that same row, with the callee
// spelled `remote:<peer>` so the recorded graph says out loud, at the node,
// where our visibility ends.
//
// **Q4, answered: does a remote peer's internal fan-out count toward AS7's
// depth? No — and the reason is observability, not preference.** If
// `docs-peer` internally asks four other agents, we cannot see it, cannot
// record it, and cannot bound it. A bound we cannot check is not a guarantee;
// it is decoration that fails open precisely where it matters. So depth
// counts OUR hops, and the boundary is STATED rather than implied: a remote
// peer is a point past which our depth accounting stops. The honest
// consequence, spelled where a reader will hit it (VisibilityNote below):
// **AS7 bounds OUR delegation, not the transitive closure** — a cycle
// entirely inside a peer is not observable and is not ours; the peer's own
// depth bound, if it has one, is its business. Where a peer's ask DOES
// appear in our rows — it called back into us and the invocation was
// recorded — the cycle rule applies normally, because the rule reads rows and
// the rows are the graph.
//
// The graph's PROPERTIES (the walk, the bound, the two refusals) belong to
// S14's chain.go and stay there: the walker arrives injected (Check below),
// exactly as the rest of the agent-services layer arrives injected — remote
// must not import agentsvc (AS11's deletability), and a duplicated walker
// would be a second truth about the same graph. What this half guarantees is
// the ORDER around the walker: policy (T4) → graph → edge → task → wire, and
// that nothing a peer CLAIMS about its own internals — a depth, a fan-out, a
// cycle report in a result payload — can enter a bound. Bounds read rows.

// RemoteCallee is how a remote ask enters the recorded graph: the peer's id
// under the ONE egress namespace (EgressNamespace — the same canonical
// spelling policy judges at the door; a second spelling would be a second
// peer, and an ask the operator allowed by name would slip past a rule that
// matched the other one). The `remote:` prefix is not decoration: it is what
// makes "this node is past the edge of our sight" a fact IN the graph rather
// than a comment beside it.
func RemoteCallee(peerID string) string { return EgressNamespace + peerID }

// IsRemoteCallee reads that fact back out of a recorded edge.
func IsRemoteCallee(calleeAgent string) bool {
	return strings.HasPrefix(calleeAgent, EgressNamespace)
}

// VisibilityNote is Q4 as a sentence, attached to every remote edge's
// rendering — the alternative to a bound that quietly implies global reach.
// The claim is deliberately SMALL, because a small claim can be checked and
// the chain's rendering must not overstate what the rows can support.
func VisibilityNote(peerID string) string {
	return "past this edge, depth is not observable (Q4): AS7 bounds OUR delegation, not the transitive closure — " +
		"whatever " + peerID + " asks internally is invisible to this graph, unrecorded and unbounded by us; " +
		"what IS bounded here is every hop we ourselves asked"
}

// Chain is the recorded graph as the ask flow needs it: two questions before
// anything exists, and one row after the ask is legal. It is an interface for
// the reason every other door in this package is one — the order is only
// testable against a boundary you can watch, and the production implementation
// walks rows in a package remote must not import.
type Chain interface {
	// CheckAsk answers AS7's two questions — cycle and depth — for the
	// intended edge callerAgent → remoteCallee, over OUR rows only. The
	// signature is the scope 3 proof in the type system: the walker receives
	// two agent names and no payload, so nothing a peer reports about its
	// own internals is even reachable from a bound.
	CheckAsk(callerAgent, remoteCallee string) error
	// RecordEdge writes the AS6 row AFTER the ask is known legal and BEFORE
	// any request leaves — the recorded-attempt order S14 measured and
	// pinned; an illegal ask must leave no trace to audit.
	RecordEdge(in store.NewChainEdge) error
}

// StoreChain is the production Chain: rows in the same agent_chain table the
// local graph lives in (a remote edge is a row like any other — that is what
// makes it survive a restart), with the walker injected.
type StoreChain struct {
	// S is the store; RecordEdge defers to it entirely — the emptiness
	// refusals are the store's own words, not paraphrases written here.
	S *store.Store
	// Check is the graph walk — in production, agentsvc's (the entry point
	// that already imports agentsvc wires this; AS11 stays intact). **A nil
	// Check refuses the ask**: certifying "no cycle" from rows nobody walked
	// is the confident wrong answer chain.go exists to refuse, and the ask
	// is worth less than a wrong guarantee — it is worth nothing.
	Check func(callerAgent, remoteCallee string) error
}

func (c *StoreChain) CheckAsk(callerAgent, remoteCallee string) error {
	if c.Check == nil {
		return errors.New("chain refused: no graph walker is wired — certifying \"no cycle\" and \"within depth\" from rows nobody walked would be a guarantee nobody checked; the ask is refused and nothing was sent")
	}
	return c.Check(callerAgent, remoteCallee)
}

func (c *StoreChain) RecordEdge(in store.NewChainEdge) error {
	if c.S == nil {
		return errors.New("chain refused: no store is wired — an ask without a recorded edge is an untraceable cost, and untraceable cost is what AS6 exists to prevent")
	}
	return c.S.CreateAgentChainEdge(in)
}

// stepIdentityReader is the Chain extension the spine check needs: read the
// agent a step row says it runs. StoreChain implements it; the check is
// STRUCTURAL where the rows are reachable and cannot be silently skipped,
// because "who asked" is the one fact the whole graph keys on.
type stepIdentityReader interface {
	StepAgent(stepID int64) (string, error)
}

func (c *StoreChain) StepAgent(stepID int64) (string, error) {
	if c.S == nil {
		return "", errors.New("no store wired")
	}
	row, err := c.S.ReadStep(stepID)
	if err != nil {
		return "", err
	}
	return row.Agent, nil
}

// AskOptions carries what InvokeSpec deliberately does not: the CALLER's
// position in the graph (which policy never needed and the intention
// therefore never held) and the async door.
type AskOptions struct {
	// CallerStepID anchors the edge to the step that asks — the chain join
	// reads the caller's agent from the STEP row, so an ask filed under
	// another agent's step would put an edge the cycle walk attributes to
	// the wrong agent (S14's spine argument, unchanged for remote asks).
	CallerStepID int64
	// Depth is the caller's position; 0 means the root call's 1 (S14's own
	// default — one default, inherited, not re-spelled).
	Depth int
	// OpenTask mints the Task the peer will complete through T6's callback:
	// created BEFORE the request goes out, because a completion must never
	// be able to arrive for a task that does not exist yet — the ordering
	// that makes RA8's "no such task" refusal mean what it says. nil is the
	// one-shot ask (T3's Call semantics: the answer rides the response,
	// nothing suspends).
	OpenTask func(spec InvokeSpec, remoteCallee string) (taskID string, err error)
}

// AskResult is the whole flow's truth. Like the webhook's outcome, a refusal
// is an ANSWER, and the zero fields next to it are the proof: Refused true
// with Edge nil, TaskID "" and a dial counter that never moved.
type AskResult struct {
	Refused   bool
	Sentence  string // the refusing door's own words, verbatim — never re-worded here
	Edge      *store.NewChainEdge
	TaskID    string
	Outcome   Outcome
	Assembled []byte
}

// ChainLine renders the recorded edge for whoever reads the chain — with the
// visibility boundary riding on it, because a chain rendering that shows a
// remote node without the note implies a completeness the rows do not have.
func (r AskResult) ChainLine() string {
	if r.Edge == nil {
		return ""
	}
	peer := strings.TrimPrefix(r.Edge.CalleeAgent, EgressNamespace)
	return r.Edge.CalleeAgent + " · " + r.Edge.SkillID + " — " + VisibilityNote(peer)
}

// Ask is the chain through a peer, in the order the properties demand:
//
//	Admit (RA4) → CheckAsk (AS7) → RecordEdge (AS6) → OpenTask → Assemble → dial
//
// Three doors BEFORE the assembler — policy, graph, and the store's own
// identity refusals — so every refusal above the dial keeps T4's guarantee
// line honest: the request was not assembled, no file was read, no bytes
// left. The task is minted BEFORE the dial on purpose (OpenTask's comment
// says why); the edge is recorded BEFORE the task (S14's order: a crash
// between leaves a recorded attempt, never an untraceable cost). A degraded
// dial after the edge and the task is the honest half-state P2 already owns:
// the edge says we asked, the task sits awaiting, and T3's expiry turns
// silence into a stated deadline — suspension plus expiry is a fact, not a
// hang.
func (g *EgressGate) Ask(ctx context.Context, chain Chain, d *Dialer, peer Peer, a Assembler, spec InvokeSpec, opt AskOptions) AskResult {
	dec := g.Admit(spec)
	if !dec.Allowed {
		return AskResult{Refused: true, Sentence: dec.Sentence}
	}
	callee := RemoteCallee(spec.Peer)
	if chain == nil {
		return AskResult{Refused: true, Sentence: "chain refused: no graph is wired for this ask — an ask that cannot be checked against the recorded graph is exactly the ask AS7 refuses; " + NoFileReadLine}
	}
	// The caller's identity is the graph's spine (S14's argument, unchanged
	// for remote asks): the edge joins this STEP to its agent, and an ask
	// filed under another agent's step puts an edge the cycle walk attributes
	// to the wrong agent — every refusal then diagnoses someone else's loop.
	// Chains that can read steps MUST be checked; a Chain that cannot (a
	// test double) is vouching for the identity by construction.
	if sr, ok := chain.(stepIdentityReader); ok {
		who, err := sr.StepAgent(opt.CallerStepID)
		if err != nil {
			return AskResult{Refused: true, Sentence: "ask refused: the caller step cannot be read (" + err.Error() + ") — an ask whose caller is not on the record cannot be traced" + "\n" + NoFileReadLine}
		}
		if who != spec.Agent {
			return AskResult{Refused: true, Sentence: "invocation refused: step " + strconv.FormatInt(opt.CallerStepID, 10) + " is run by \"" + who + "\", not \"" + spec.Agent + "\" — an agent cannot delegate under another agent's step" + "\n" + NoFileReadLine}
		}
	}
	if err := chain.CheckAsk(spec.Agent, callee); err != nil {
		// The walker's words verbatim (one formatter per verdict): its
		// sentence already names the path or the depth. Nothing after this
		// line runs — no edge (an illegal ask leaves no trace to audit), no
		// task, no assembly, no socket.
		return AskResult{Refused: true, Sentence: err.Error() + "\n" + NoFileReadLine}
	}
	depth := opt.Depth
	if depth == 0 {
		depth = 1
	}
	edge := store.NewChainEdge{
		CallerLoopID: spec.Loop, CallerStepID: opt.CallerStepID,
		CalleeAgent: callee, SkillID: spec.Skill, Depth: depth,
	}
	if err := chain.RecordEdge(edge); err != nil {
		return AskResult{Refused: true, Sentence: "chain refused: the chain edge could not be recorded — without it the invocation would be an untraceable cost: " + err.Error() + "\n" + NoFileReadLine}
	}
	res := AskResult{Edge: &edge}
	if opt.OpenTask != nil {
		taskID, err := opt.OpenTask(spec, callee)
		if err != nil {
			// The edge survives and says so: WE asked, and the attempt was
			// recorded even though the task could not be minted.
			return AskResult{Refused: true, Sentence: "ask refused: the awaiting task could not be created — " + err.Error() + "\n" + NoFileReadLine, Edge: &edge}
		}
		res.TaskID = taskID
	}
	body, err := a.Assemble(spec)
	if err != nil {
		return AskResult{Refused: true, Sentence: "ask refused: the request could not be assembled — " + err.Error(), Edge: &edge, TaskID: res.TaskID}
	}
	res.Sentence = dec.Sentence
	res.Outcome = d.Call(ctx, peer, body)
	res.Assembled = body
	return res
}
