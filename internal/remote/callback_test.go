package remote

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// callback_test.go — the sharpest security task in the slice, tested as the
// ORDER it is: signature → lookup → awaiting → callee → land. The fakes are
// the witnesses: the TaskBook counts every touch, so a refusal that "should
// cost nothing but a hash" is proven not to have touched the store at all.

type fakeBook struct {
	mu           sync.Mutex
	tasks        map[string]fakeTask
	awaitingCall int
	landCalls    int
	landErr      error
	lastCost     *model.Cost // what the seam was asked to land beside the result (T8)
}

type fakeTask struct {
	callee   string
	awaiting bool
	lands    int
}

func newBook() *fakeBook {
	return &fakeBook{tasks: map[string]fakeTask{}}
}

func (b *fakeBook) put(id, callee string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks[id] = fakeTask{callee: callee, awaiting: true}
}

func (b *fakeBook) Awaiting(task string) (string, bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.awaitingCall++
	t, ok := b.tasks[task]
	return t.callee, t.awaiting, ok
}

func (b *fakeBook) Land(task, result string, cost *model.Cost) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.landCalls++
	b.lastCost = cost
	if b.landErr != nil {
		return false, b.landErr
	}
	t, ok := b.tasks[task]
	if !ok || !t.awaiting {
		return false, nil
	}
	t.awaiting = false
	t.lands++
	t.callee += "|" + result // prove the result text is what landed
	b.tasks[task] = t
	return true, nil
}

func peerWithKey(t *testing.T, id, secret string) Peer {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/" + id + ".key"
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	return Peer{ID: id, Key: &KeyRef{Path: path}}
}

// setup is the two-peer world every binding test needs: docs-peer and
// beta-peer, each with its own secret, one task 7c2e owed by docs-peer.
func setup(t *testing.T) (*Callbacks, *fakeBook, map[string]string) {
	t.Helper()
	// Secrets are unique per test: the replay cache is the daemon's one
	// surface (global, like TR9's webhookReplays), and HMAC over the same
	// bytes with the same secret is the same signature — shared secrets
	// across tests would make one test's landings shadow another's.
	secrets := map[string]string{"docs-peer": "secret-docs-" + t.Name(), "beta-peer": "secret-beta-" + t.Name()}
	peers := map[string]Peer{}
	for id, sec := range secrets {
		peers[id] = peerWithKey(t, id, sec)
	}
	book := newBook()
	book.put("7c2e", "docs-peer")
	c := &Callbacks{
		Peers: func(id string) (Peer, bool) { p, ok := peers[id]; return p, ok },
		Book:  book,
		Now:   func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) },
	}
	return c, book, secrets
}

func complete(peer, task, ts string) Completion {
	return Completion{Task: task, Peer: peer, Result: "the analysis: four findings", Timestamp: ts}
}

const noon = "2026-10-28T12:00:00Z"

func sign(sec string, req Completion) string { return SignCompletion(sec, req) }

// ————— the five gate tests (front-matter names, verbatim) —————

func TestUnsolicitedCompletionRefused(t *testing.T) {
	c, book, secrets := setup(t)
	// A correctly SIGNED completion for a Task we never created.
	req := complete("docs-peer", "deadbeef", noon)
	req.Signature = sign(secrets["docs-peer"], req)
	ans := c.Deliver(req)
	if ans.Outcome != OutcomeRefused || ans.HTTPStatus != statusNoTask {
		t.Fatalf("an unsolicited completion landed or mispriced: %+v", ans)
	}
	if !strings.Contains(ans.Reason, `completion for unknown task "deadbeef"`) ||
		!strings.Contains(ans.Reason, "signature verified") ||
		!strings.Contains(ans.Reason, "no such task was created by this daemon") {
		t.Errorf("the DEMO-pinned refusal sentence moved: %q", ans.Reason)
	}
	// The verified identity is exactly WHY the sentence can say "signature
	// verified": the probe learned something true about their own probe.
	// CONTROL (the pin can fail): the task we DID create lands.
	req2 := complete("docs-peer", "7c2e", noon)
	req2.Signature = sign(secrets["docs-peer"], req2)
	if ans2 := c.Deliver(req2); ans2.Outcome != OutcomeLanded || ans2.HTTPStatus != statusLanded {
		t.Fatalf("the solicited completion did not land: %+v", ans2)
	}
	_ = book
}

func TestCompletionForAnotherTaskRefused(t *testing.T) {
	c, book, secrets := setup(t)
	// beta-peer — a legitimate, configured peer with a valid secret —
	// answers docs-peer's task. The signature verifies. THIS is when
	// binding earns its keep: verification proved WHO; only binding asks
	// WHAT THEY WERE ASKED.
	req := complete("beta-peer", "7c2e", noon)
	req.Signature = sign(secrets["beta-peer"], req)
	ans := c.Deliver(req)
	if ans.Outcome != OutcomeRefused || ans.HTTPStatus != statusBinding {
		t.Fatalf("peer B answering peer A's task was accepted: %+v", ans)
	}
	if !strings.Contains(ans.Reason, "completion for task 7c2e came from beta-peer, which was not its callee") {
		t.Errorf("the RA8 sentence moved (DEMO pins it): %q", ans.Reason)
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	if task := book.tasks["7c2e"]; !task.awaiting || task.lands != 0 {
		t.Errorf("the foreign completion altered docs-peer's task — awaiting=%v lands=%d (N6's fabricated answer, wearing a valid signature)", task.awaiting, task.lands)
	}
}

func TestReplayedCompletionRefused(t *testing.T) {
	c, book, secrets := setup(t)
	req := complete("docs-peer", "7c2e", noon)
	req.Signature = sign(secrets["docs-peer"], req)
	if ans := c.Deliver(req); ans.Outcome != OutcomeLanded {
		t.Fatalf("first delivery must land: %+v", ans)
	}
	for i := 0; i < 3; i++ {
		ans := c.Deliver(req)
		if ans.Outcome != OutcomeRefused || !strings.Contains(ans.Reason, "landed already") ||
			!strings.Contains(ans.Reason, "a resend is not an event") {
			t.Fatalf("replay %d accepted: %+v", i, ans)
		}
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	if book.tasks["7c2e"].lands != 1 {
		t.Errorf("the task landed %d times, want 1 — the CAS is the replay guard, and a completion delivered twice lands ONCE", book.tasks["7c2e"].lands)
	}
}

func TestNoSecondListener(t *testing.T) {
	// RA7's structural half: this package opens NOTHING. The completion is
	// a method on S19's existing table, not a server beside it.
	banned := []string{
		"net" + ".Listen", "ListenAndServe", "http" + ".Server",
		"Listen" + "(", "Serve" + "(", "ListenTCP", "Unix" + "(" + "\"",
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		if f.Name() == "dial.go" {
			// dial.go DIALS OUT; a dial is not a listen (the ban is on
			// opening an inbound surface, and dial.go's net.Dialer is the
			// outbound half this same slice mandates).
			continue
		}
		raw, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		seen++
		for _, b := range banned {
			if strings.Contains(string(raw), b) {
				t.Errorf("%s contains %q — a second listener would be a second server to secure, document and keep narrow (P25)", f.Name(), b)
			}
		}
	}
	if seen < 4 {
		t.Fatalf("the sweep saw %d source files; the hunter must not be vacuous", seen)
	}
	// The mount is the narrow table: the callback action exists THERE.
	table, err := os.ReadFile("../narrow/actions.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(table), "aeon_remote_complete") {
		t.Error("narrow/actions.go has no aeon_remote_complete entry — the endpoint must be an enumerated projection (P25), not a handler of its own")
	}
}

func TestAsyncRemoteTaskSuspendsWithNoHeldConnection(t *testing.T) {
	// AS4 inherited: the suspension is a ROW, no goroutine and no socket
	// waits. Three assertions, all structural, none needing a restart.
	c, book, secrets := setup(t)
	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		req := complete("docs-peer", "task"+itoa(i), noon)
		req.Signature = sign(secrets["docs-peer"], req)
		c.Deliver(req) // all refused (unknown task) — even refusals spawn nothing
	}
	// And 50 that land, to make the claim cover the happy path too.
	for i := 0; i < 50; i++ {
		id := "land" + itoa(i)
		book.put(id, "docs-peer")
		req := complete("docs-peer", id, noon)
		req.Signature = sign(secrets["docs-peer"], req)
		if ans := c.Deliver(req); ans.Outcome != OutcomeLanded {
			t.Fatalf("delivery %d refused: %+v", i, ans)
		}
	}
	// A settling window, then the count: no goroutine was ever owed.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before {
		t.Errorf("%d goroutines after 100 deliveries, want %d — an async remote Task suspends by EXITING, not by waiting (RA7)", got, before)
	}
	// The struct holds no connection, no channel, no timer: there is NOWHERE
	// a held connection could live.
	for _, f := range []string{"net.Conn", "tls.Conn", "chan ", "time.Timer", "time.Ticker"} {
		if reflectsField(Callbacks{}, f) {
			t.Errorf("Callbacks has a field whose type mentions %q — nothing here may hold anything open", f)
		}
	}
	// And the suspension lives ONLY in the book: the tasks were rows there.
	book.mu.Lock()
	defer book.mu.Unlock()
	if book.tasks["land0"].awaiting {
		t.Error("the landed task should be terminal in the store — the row IS the suspension, and it is the row that changes")
	}
}

// reflectsField reports whether any field's TYPE string contains needle.
func reflectsField(v any, needle string) bool {
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		if strings.Contains(t.Field(i).Type.String(), needle) {
			return true
		}
	}
	return false
}

// ————— the rest of the validation list —————

func TestUnsignedAndMisSignedRefuseWithoutTouchingTheStore(t *testing.T) {
	c, book, secrets := setup(t)
	cases := map[string]Completion{
		"no signature":    complete("docs-peer", "7c2e", noon),
		"wrong secret":    withSig(complete("docs-peer", "7c2e", noon), sign("the-wrong-secret", complete("docs-peer", "7c2e", noon))),
		"peer B's secret": withSig(complete("docs-peer", "7c2e", noon), sign(secrets["beta-peer"], complete("docs-peer", "7c2e", noon))),
		"truncated hex":   withSig(complete("docs-peer", "7c2e", noon), "sha256=abc"),
		"unsigned prefix": withSig(complete("docs-peer", "7c2e", noon), "sha512="+sign(secrets["docs-peer"], complete("docs-peer", "7c2e", noon))[7:]),
	}
	for name, req := range cases {
		book.mu.Lock()
		beforeAwait, beforeLand := book.awaitingCall, book.landCalls
		book.mu.Unlock()
		ans := c.Deliver(req)
		book.mu.Lock()
		touched := book.awaitingCall != beforeAwait || book.landCalls != beforeLand
		book.mu.Unlock()
		if ans.Outcome != OutcomeRefused || ans.HTTPStatus != statusAuth {
			t.Errorf("%s: accepted or mispriced: %+v", name, ans)
		}
		if touched {
			t.Errorf("%s: a bad signature touched the task store — a bad signature must cost nothing but a hash and a constant-time compare (TR9's order)", name)
		}
	}
}

func withSig(r Completion, s string) Completion { r.Signature = s; return r }

func TestAlreadyCompleteTaskRefused(t *testing.T) {
	c, _, secrets := setup(t)
	req := complete("docs-peer", "7c2e", noon)
	req.Signature = sign(secrets["docs-peer"], req)
	c.Deliver(req) // lands
	// A DIFFERENT completion (fresh timestamp, fresh delivery id — not a
	// byte-replay) for the same, now-terminal task: refused as not awaiting,
	// which is the distinct refusal from a replay of the same bytes only in
	// that it passed the same gates and found the row closed.
	req2 := complete("docs-peer", "7c2e", "2026-10-28T12:00:30Z")
	req2.DeliveryID = "second"
	req2.Signature = sign(secrets["docs-peer"], req2)
	ans := c.Deliver(req2)
	if ans.Outcome != OutcomeRefused || !strings.Contains(ans.Reason, "not awaiting completion") ||
		!strings.Contains(ans.Reason, "already complete, or never async") {
		t.Errorf("a completion for a terminal task: %+v", ans)
	}
}

func TestUnknownPeerAnswersExactlyLikeBadSignature(t *testing.T) {
	// The endpoint must not enumerate which peers this daemon configured:
	// "no such peer" and "wrong signature" are ONE answer, byte-identical.
	c, _, _ := setup(t)
	known := withSig(complete("docs-peer", "7c2e", noon), sign("whatever", complete("docs-peer", "7c2e", noon)))
	unknown := withSig(complete("no-such-peer", "7c2e", noon), sign("whatever", complete("no-such-peer", "7c2e", noon)))
	a, b := c.Deliver(known), c.Deliver(unknown)
	if a.Reason != b.Reason || a.HTTPStatus != b.HTTPStatus {
		t.Errorf("the pre-authentication answer differs by peer: %q vs %q — the endpoint just admitted whether no-such-peer is configured", a.Reason, b.Reason)
	}
}

func TestTimestampWindow(t *testing.T) {
	c, _, secrets := setup(t)
	stale := complete("docs-peer", "7c2e", "2026-10-28T11:00:00Z") // an hour old
	stale.Signature = sign(secrets["docs-peer"], stale)
	if ans := c.Deliver(stale); ans.Outcome != OutcomeRefused || !strings.Contains(ans.Reason, "window") {
		t.Errorf("a stale timestamped completion was accepted: %+v", ans)
	}
	// No timestamp is honest and relies on the landing CAS (TR9's argument,
	// inherited): it lands.
	none := complete("docs-peer", "7c2e", "")
	none.Signature = sign(secrets["docs-peer"], none)
	if ans := c.Deliver(none); ans.Outcome != OutcomeLanded {
		t.Errorf("an untimestamped completion must not be refused for honesty's sake: %+v", ans)
	}
}

func TestStoreIndecisionIsNeverRefusedNorLanded(t *testing.T) {
	// P31's third state on the landing path: the store could not decide, and
	// claiming refused would be claiming a verdict the store never gave.
	c, book, secrets := setup(t)
	book.landErr = os.ErrClosed
	req := complete("docs-peer", "7c2e", noon)
	req.Signature = sign(secrets["docs-peer"], req)
	ans := c.Deliver(req)
	if ans.Outcome != OutcomeCouldNotSay || !strings.Contains(ans.Reason, "could not decide") || !strings.Contains(ans.Reason, "nothing was claimed") {
		t.Errorf("the store's silence was rendered as a verdict: %+v", ans)
	}
	if ans.HTTPStatus != statusUndecided {
		t.Errorf("indecision renders %d, want %d — a 200 on silence would teach senders the store answered", ans.HTTPStatus, statusUndecided)
	}
}

func TestEveryRefusalIsRecorded(t *testing.T) {
	// Scope 6: an endpoint being probed is something somebody should be able
	// to SEE. The sink is the seeing.
	c, _, secrets := setup(t)
	var mu sync.Mutex
	var lines []string
	c.Records = func(entry string) { mu.Lock(); lines = append(lines, entry); mu.Unlock() }
	before := RefusalCount()
	probes := []Completion{
		complete("docs-peer", "deadbeef", noon), // unsigned + unknown task
		withSig(complete("beta-peer", "7c2e", noon), sign(secrets["beta-peer"], complete("beta-peer", "7c2e", noon))),                                     // callee mismatch
		withSig(complete("no-such-peer", "1", noon), "sha256=deadbeef"),                                                                                   // unknown peer
		withSig(complete("docs-peer", "7c2e", "2026-01-01T00:00:00Z"), sign(secrets["docs-peer"], complete("docs-peer", "7c2e", "2026-01-01T00:00:00Z"))), // stale
		Completion{Task: "", Peer: "docs-peer"}, // shape
	}
	for _, p := range probes {
		if p.Signature == "" && p.Task == "deadbeef" {
			// leave unsigned on purpose
		} else if p.Task == "deadbeef" {
			p.Signature = sign(secrets["docs-peer"], p)
		}
		c.Deliver(p)
	}
	mu.Lock()
	got := len(lines)
	mu.Unlock()
	if got != len(probes) {
		t.Errorf("%d probes produced %d records — a refusal nobody can read is a refusal that never happened", len(probes), got)
	}
	if RefusalCount() < before+len(probes) {
		t.Errorf("the probe counter did not advance: %d → %d", before, RefusalCount())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range lines {
		if !strings.HasPrefix(l, "refused completion from peer") {
			t.Errorf("record does not name the peer and the fact: %q", l)
		}
		if strings.Contains(l, "secret-docs-1") || strings.Contains(l, "sha256=") {
			t.Errorf("the probe log carries secret material (%q) — records name the probe, never replay it", l)
		}
	}
}

func TestCanonicalIsInjective(t *testing.T) {
	// The length prefix is load-bearing: a result that CONTAINS the field
	// delimiter must not be able to spell a different claim over the same
	// bytes.
	a := Completion{Peer: "p", Task: "t", Timestamp: "12", Result: "x"}
	// A field-shifting forgery: peer "p", task "t", timestamp "12\n1\nx"
	// with empty result would join to the same unprefixed string.
	b := Completion{Peer: "p", Task: "t", Timestamp: "12\n1\nx", Result: ""}
	if string(CanonicalCompletion(a)) == string(CanonicalCompletion(b)) {
		t.Error("two different claims canonicalise to the same bytes — the HMAC cannot tell them apart, and neither can anyone else")
	}
	// Round-trip through the shared sender half.
	sig := SignCompletion("s3cret", a)
	c := &Callbacks{Peers: func(string) (Peer, bool) { return Peer{ID: "p", Key: &KeyRef{Path: writeTmpKey(t, "s3cret")}}, true }, Book: newBook()}
	if !c.verifies(Peer{ID: "p", Key: &KeyRef{Path: writeTmpKey(t, "s3cret")}}, withSig(a, sig)) {
		t.Error("the sender's signer and the receiver's verifier disagree — the fake peer would prove the fake, not the mechanism")
	}
}

func writeTmpKey(t *testing.T, secret string) string {
	t.Helper()
	p := t.TempDir() + "/k"
	if err := os.WriteFile(p, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOutcomeVocabularyIsClosed(t *testing.T) {
	c, _, secrets := setup(t)
	seen := map[string]bool{}
	for _, r := range []Completion{
		complete("docs-peer", "nope", noon),
		withSig(complete("docs-peer", "7c2e", noon), sign(secrets["docs-peer"], complete("docs-peer", "7c2e", noon))),
		Completion{Task: "", Peer: ""},
	} {
		seen[c.Deliver(r).Outcome] = true
	}
	for o := range seen {
		if o != OutcomeLanded && o != OutcomeRefused && o != OutcomeCouldNotSay {
			t.Errorf("outcome vocabulary leaked %q — the answer set is closed (landed | refused | could_not_say — the webhook's five-word vocabulary narrowed to the three this surface can honestly mean)", o)
		}
	}
}
