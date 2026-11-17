package remote

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// callback.go — RA7 and RA8 made code: the completion of an async remote Task
// arrives as an INBOUND call to our own narrow surface, verified AND bound.
//
// # Why inbound (RA7)
//
// An async Task cannot be a held connection: a held connection dies with the
// daemon, and P2 exists precisely so a suspended step survives exactly that.
// So nothing is waiting anywhere: the suspension is a ROW in the task store,
// no goroutine and no socket is held, and the completion is a new call. This
// file opens nothing — it has no listener, no Addr, no Serve. The narrow
// action table mounts it (S19's one enumerated projection, P25); the
// `TestNoSecondListener` grep and the suspension test hold that.
//
// # Verification proves WHO. Binding proves WHAT THEY WERE ASKED (RA8).
//
// A callback that is authenticated but unbound lets anybody who can reach the
// endpoint — including a legitimate peer, misbehaving or compromised — inject
// a skill result into a transcript, and the loop then acts on it as the
// agent's work. That is a fabricated ANSWER, and S19's N6 refuses the same
// shape for questions ("a fabricated prompt, once answered, feeds a value
// into a transcript as though the model had asked for it"). The reasoning
// transfers exactly, so the order here is TR9's order, one layer up:
//
//	signature → lookup → awaiting → callee → land
//
// The signature first because a bad one must cost nothing but a hash and a
// constant-time compare — no task read, no record touched. The refusals that
// follow are the BINDING: three conditions, and every failure of one is a
// refusal that names the condition:
//
//	unknown task id          → we never asked for this
//	not awaiting completion  → already complete, or never async
//	callee ≠ sender          → peer A answering peer B's task
//
// and the landing itself is the replay guard: the task store's
// awaiting→complete compare-and-set lands a completion exactly once, which
// beats any cache because it survives the restart that would "lose" the cache
// (the same honesty TR9 argued for the digest, improved where the store
// allows it — the store IS the memory).
//
// # The secret (C17, RA9)
//
// The peer's key — the same KeyRef T1 made peers carry — is read at use time
// from the environment or a path, never argv, never a config value, never a
// log. A sender naming a peer we never configured cannot even locate a
// secret: unknown peer fails exactly like a wrong signature (401 is the whole
// answer before authentication; it must not enumerate which peers exist).

// Completion is the one request shape both mountings fold into (N18: the
// signature rides as a PARAM because stdio has no headers; the HTTP
// mounting's X-Aeon-Completion-* headers are sugar for the same fields).
// Task is the integer task id as a string — the same taskId the socket
// lifecycle events carry, so the peer echoes what it was given, unchanged.
type Completion struct {
	Task       string `json:"task"`
	Peer       string `json:"peer"`
	Result     string `json:"result"`
	Signature  string `json:"signature,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	DeliveryID string `json:"deliveryId,omitempty"`
	// Cost is the peer's claim about its own spend (RA10) — nil is the
	// normal case, and the normal case is UNKNOWN, never zero. It rides the
	// signed canonical (CanonicalCompletion): a number that is not bound to
	// the signature is not a claim.
	Cost *CostReport `json:"cost,omitempty"`
}

// CompletionAnswer is the discriminated result. HTTPStatus is the mounting's
// rendering hint (the answer IS the outcome — a 200-on-a-refusal would teach
// senders to stop reading the body; the webhook route argued this exact
// point at S20 T6).
type CompletionAnswer struct {
	TaskID     string `json:"taskId"`
	Peer       string `json:"peer"`
	Outcome    string `json:"outcome"` // "landed" | "refused" — closed set
	Reason     string `json:"reason,omitempty"`
	HTTPStatus int    `json:"httpStatus"`
}

// OutcomeLanded and OutcomeRefused are the closed outcome vocabulary.
// A completion that did not land is not an error — it is an ANSWER, and the
// transport only failed if the socket did.
const (
	OutcomeLanded = "landed"
	// OutcomeRefused is a VERDICT: the gates ran and one named its failure.
	OutcomeRefused = "refused"
	// OutcomeCouldNotSay is P31's third state on the landing path: the store
	// could not decide, and calling that "refused" would claim a verdict the
	// store never gave — the same reason the webhook vocabulary keeps
	// "could not say" apart from "did not fire".
	OutcomeCouldNotSay = "could_not_say"

	statusLanded    = 200
	statusAuth      = 401
	statusNoTask    = 404
	statusBinding   = 403
	statusUndecided = 503
)

// CompletionWindow bounds how stale a timestamped completion may be — the
// same ±5 minutes TR9 chose, and for the same honest reason: it is a bound
// on the sender's retry storm, not a claim about clocks nobody controls.
// A completion with NO timestamp relies on the landing CAS alone (refusing
// the honest sender would train senders to drop fields).
const CompletionWindow = 5 * time.Minute

// TaskBook is the daemon's task store seen from here: two questions and one
// action, no more. Keeping it an interface is not deference to testability —
// it is the direction of the dependency: remote must not learn how tasks are
// stored, only that they are awaited and land once.
type TaskBook interface {
	// Awaiting reports the task's callee peer and whether it still awaits a
	// completion. found=false means this daemon never created the task;
	// awaiting=false means it is terminal (already complete, or never was
	// async) — the two are deliberately distinct refusals.
	Awaiting(task string) (callee string, awaiting bool, found bool)
	// Land performs the awaiting→complete compare-and-set with the result
	// text AND the cost decided at the seam (CostFor): the cost enters the
	// same CAS as the result, because a task complete with a cost applied
	// twice or a result landed with its cost dropped are both half-states
	// the record cannot undo. NEVER nil: absence is a state (unknown), not
	// an omission.
	Land(task, result string, cost *model.Cost) (landed bool, err error)
}

// Callbacks is the mount point: configured peers (whose keys are the shared
// secrets), the task book, a clock seam, and a record sink for refusals
// (scope 6 — an endpoint being probed is something somebody should be able
// to SEE, and a refusal nobody can read is a refusal that never happened).
type Callbacks struct {
	// Peers resolves a configured peer by id; the zero value (no wiring)
	// resolves NOTHING, which is RA9's off-by-default reaching its last
	// surface: a daemon with no peers cannot honour any callback.
	Peers func(id string) (Peer, bool)
	// Book is the suspension the completion lands into.
	Book TaskBook
	// Now is the clock seam (the house discipline: injectable, never read
	// directly at decision points).
	Now func() time.Time
	// Records receives one line per refusal. It is called with text that
	// never contains the secret, the signature bytes, or the result body.
	Records func(entry string)
}

func (c *Callbacks) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

var (
	refusalsMu sync.Mutex
	refusals   int
)

// RefusalCount counts recorded refusals across all Callbacks (the probe
// counter a surface that refuses must keep for itself; per-instance sinks
// still get the sentence).
func RefusalCount() int {
	refusalsMu.Lock()
	defer refusalsMu.Unlock()
	return refusals
}

func (c *Callbacks) refuse(peerID, task, sentence string, status int) CompletionAnswer {
	refusalsMu.Lock()
	refusals++
	refusalsMu.Unlock()
	if c.Records != nil {
		// The record names the peer, the task and the reason — never the
		// secret, never the signature bytes, never the stranger's result
		// body (a probe log that stores payloads becomes the leak).
		c.Records("refused completion from peer \"" + peerID + "\" for task " + task + " — " + sentence)
	}
	return CompletionAnswer{TaskID: task, Peer: peerID, Outcome: OutcomeRefused, Reason: sentence, HTTPStatus: status}
}

// Deliver is the whole inbound surface, in TR9's order. It verifies, binds,
// and lands — or refuses with a sentence that names the condition that
// failed.
func (c *Callbacks) Deliver(req Completion) CompletionAnswer {
	// Shape first: an empty task or peer is not a probe, it is a typo, and
	// it answers before any crypto runs.
	if req.Task == "" || req.Peer == "" {
		return c.refuse(req.Peer, req.Task, "a completion must name its task id and its peer — nothing was verified", statusAuth)
	}
	// ── verification (WHO) ─────────────────────────────────────────────────
	// An unknown peer and a wrong signature answer IDENTICALLY: 401 with the
	// same sentence, so the endpoint cannot be used to enumerate which peers
	// this daemon configured (the answer before authentication proves
	// nothing, and must reveal nothing either).
	peer, known := c.lookupPeer(req.Peer)
	if !known || !c.verifies(peer, req) {
		return c.refuse(req.Peer, req.Task, "the completion signature did not verify — an unauthenticated completion lands nothing", statusAuth)
	}
	// A supplied timestamp must be inside the window; absent is honest and
	// relies on the landing CAS (argued above).
	if req.Timestamp != "" {
		at, err := time.Parse(time.RFC3339, req.Timestamp)
		if err != nil || c.now().Sub(at).Abs() > CompletionWindow {
			return c.refuse(req.Peer, req.Task, "the completion timestamp is unreadable or outside the ±5-minute window — a stale replay of a real completion is still a replay", statusAuth)
		}
	}
	// ── replay of a LANDED byte-sequence (distinct from a closed task) ─────
	// A byte-identical resend of a completion that landed is a different
	// FACT from "this task is closed" — P31 says say what you know. The
	// guard is TR9's own shape: an in-process, bounded, oldest-first cache
	// of landed signatures, honest about surviving only the process (the
	// store CAS below catches anything that arrives after a restart, with
	// the other sentence — both paths refuse; only the WORDING differs).
	if completionReplays.contains(req.Signature + "\x00" + req.Task) {
		return c.refuse(req.Peer, req.Task,
			"completion for task "+req.Task+" landed already — a resend is not an event", statusBinding)
	}
	// ── binding (WHAT THEY WERE ASKED) ─────────────────────────────────────
	// The signature has already proven WHO sent it; every refusal from here
	// speaks from a verified identity, which is exactly why it can afford to
	// say more (and why the unknown-task sentence states that the signature
	// DID verify — the sender learned something true about their own probe).
	callee, awaiting, found := c.Book.Awaiting(req.Task)
	if !found {
		return c.refuse(req.Peer, req.Task,
			"completion for unknown task \""+req.Task+"\" — signature verified · no such task was created by this daemon", statusNoTask)
	}
	if !awaiting {
		return c.refuse(req.Peer, req.Task,
			"completion for task "+req.Task+" found a task that is not awaiting completion — already complete, or never async", statusBinding)
	}
	if callee != req.Peer {
		// The RA8 sentence, DEMO-pinned: peer A answering peer B's task is
		// the fabrication N6 refuses for questions, wearing a valid
		// signature. Verification passing is precisely WHEN this check
		// earns its keep.
		return c.refuse(req.Peer, req.Task,
			"completion for task "+req.Task+" came from "+req.Peer+", which was not its callee", statusBinding)
	}
	// ── landing (the replay guard IS the store) ────────────────────────────
	cost := CostFor(req.Peer, req.Cost)
	landed, err := c.Book.Land(req.Task, req.Result, &cost)
	if err != nil {
		// A store failure is not a refusal — the truth is that we cannot
		// say, and saying refused would claim a verdict the store never
		// gave (P31's third state; it never lands and never lies). It is
		// still RECORDED: an endpoint whose store went quiet mid-decision
		// is exactly what somebody should be able to see.
		refusalsMu.Lock()
		refusals++
		refusalsMu.Unlock()
		if c.Records != nil {
			c.Records("could-not-say completion from peer " + strconv.Quote(req.Peer) + " for task " + req.Task + " — the task store could not decide")
		}
		return CompletionAnswer{TaskID: req.Task, Peer: req.Peer, Outcome: OutcomeCouldNotSay,
			Reason:     "the task store could not decide whether the completion landed — nothing was claimed: " + err.Error(),
			HTTPStatus: statusUndecided}
	}
	if !landed {
		// The store said it was not there to land and the byte-sequence is
		// not one we recorded — a concurrent landing won the CAS. Same
		// fact, same sentence: it landed once, just not by this hand.
		return c.refuse(req.Peer, req.Task,
			"completion for task "+req.Task+" landed already — a resend is not an event", statusBinding)
	}
	completionReplays.remember(req.Signature + "\x00" + req.Task)
	return CompletionAnswer{TaskID: req.Task, Peer: req.Peer, Outcome: OutcomeLanded, HTTPStatus: statusLanded}
}

func (c *Callbacks) lookupPeer(id string) (Peer, bool) {
	if c.Peers == nil {
		return Peer{}, false // unwired is unpeered (RA9 reaches every surface)
	}
	return c.Peers(id)
}

// verifies recomputes the HMAC over the canonical bytes and compares in
// constant time (TR9's discipline, imported as discipline not code: the
// webhook's own VerifyWebhookSignature answers a different message shape —
// the exact payload bytes — where a completion has no body, so its FIELDS
// are the message and the canonicalisation lives here, once, named).
func (c *Callbacks) verifies(peer Peer, req Completion) bool {
	if req.Signature == "" {
		return false
	}
	key := peer.Key
	if key == nil {
		return false
	}
	secret, err := key.LoadKey()
	if err != nil || len(secret) == 0 {
		// A peer whose key cannot be read honours no callbacks — that is
		// the key's ABSENCE speaking (C17's storage rule, not a bug to
		// paper over with a permissive default).
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(req.Signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(CanonicalCompletion(req))
	return subtle.ConstantTimeCompare(mac.Sum(nil), want) == 1
}

// CanonicalCompletion is the signed message: the four fields the peer is
// claiming, each LENGTH-PREFIXED, so no field's bytes can impersonate a
// field boundary (a completion's result is free text — an unprefixed join
// would let a result containing the delimiter forge a different claim over
// the same HMAC — the length prefix is what makes the canonical form
// injective). The signature is HMAC-SHA256 over exactly these bytes, hex.
func CanonicalCompletion(req Completion) []byte {
	// Deliberately does NOT include Signature: what is signed is the CLAIM.
	fields := []string{"v1", req.Peer, req.Task, req.Timestamp, req.Result, costClaim(req.Cost)}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(strconv.Itoa(len(f)))
		b.WriteByte('\n')
		b.WriteString(f)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// costClaim encodes the (optional) cost claim for the canonical form: one
// line, no free text, fixed formatting — a nil report is an explicit "none",
// so "no report" and "a report whose bytes happen to be empty" are different
// signed claims (they must be — they land as different records).
func costClaim(r *CostReport) string {
	if r == nil {
		return "cost:none"
	}
	return "cost:usd=" + strconv.FormatFloat(r.USD, 'f', -1, 64) + ",currency=" + strings.ToLower(r.Currency)
}

// SignCompletion is the sender's half, exported for the fake peer (test and
// DEMO) so the two halves share ONE canonicaliser — a fake that signs its
// own way would prove the fake, not the mechanism.
func SignCompletion(secret string, req Completion) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(CanonicalCompletion(req))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// seenCache is TR9's replayCache discipline, narrowed to what this surface
// needs: bounded (a flood cannot grow the daemon), oldest-first eviction,
// and the honesty the webhook file argued — in-process is enough because a
// resend is a retry seconds old, and the restart that "loses" the cache also
// hands the store's CAS the whole replay story.
type seenCache struct {
	mu    sync.Mutex
	seen  map[string]bool
	order []string
}

const completionReplayBound = 4096

var completionReplays = &seenCache{seen: map[string]bool{}}

func (c *seenCache) contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[key]
}

func (c *seenCache) remember(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.order = append(c.order, key)
	if len(c.order) > completionReplayBound {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.seen, oldest)
	}
}
