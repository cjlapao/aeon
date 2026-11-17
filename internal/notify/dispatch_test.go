package notify

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/store"
)

// dispatch_test.go — S58 T5: the dispatcher's promises, each one paid
// for in delivery rows. The queue, the dedup window, the rate ceiling
// and its summary, the quiet window, the retry ladder, the overflow —
// every claim is asserted as OUTCOMES the store can show, because "the
// log is honest by construction" is worth nothing if the outcomes it
// records are wrong.

// capture is what a receiver saw, in arrival order.
type capture struct {
	mu      sync.Mutex
	bodies  [][]byte
	headers []http.Header
}

func (c *capture) record(body []byte, h http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, body)
	c.headers = append(c.headers, h)
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func (c *capture) last() ([]byte, http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return nil, nil
	}
	return c.bodies[len(c.bodies)-1], c.headers[len(c.headers)-1]
}

// receiver is an httptest loopback server with a programmable responder.
func receiver(t *testing.T, fn func(w http.ResponseWriter, r *http.Request, n int)) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	var n int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		mu.Lock()
		n++
		this := n
		mu.Unlock()
		c.record(body, r.Header.Clone())
		fn(w, r, this)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func okHandler(w http.ResponseWriter, _ *http.Request, _ int) { w.WriteHeader(http.StatusOK) }

// fixture wires a store, ONE enabled channel to the receiver, a secret,
// a permissive subscription, and a dispatcher whose clock and sleeps the
// test controls.
type fixture struct {
	st   *store.Store
	ch   store.NotifyChannel
	d    *Dispatcher
	cap  *capture
	now  time.Time
	nk   *[]time.Duration // sleeps land here when set
	home string
}

func newFixture(t *testing.T, srv *httptest.Server) *fixture {
	return newFixtureAt(t, srv.URL, true)
}

func newFixtureAt(t *testing.T, url string, allowPrivate bool) *fixture {
	t.Helper()
	st := openChannelStore(t)
	home := isolateSecrets(t)
	ch, err := st.CreateChannel(store.CreateChannelIn{Name: "ops", URL: url,
		AllowPrivate: allowPrivate, ActorKind: "user", ActorID: "carlos"})
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if _, err := st.SetChannelEnabled(ch.ID, true, "user", "carlos"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := secrets.SaveChannelSecret(ch.ID, "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetSubscription(store.SetSubscriptionIn{ChannelID: ch.ID,
		MinSeverity: "info", ActorKind: "user", ActorID: "carlos"}); err != nil {
		t.Fatalf("subscription: %v", err)
	}
	f := &fixture{st: st, ch: ch, home: home}
	var sleeps []time.Duration
	f.nk = &sleeps
	f.d = NewDispatcher(st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		// the clock follows the fixture (tests f.now = ... then Submit)
		Clock: func() time.Time { return f.now },
		Sleep: func(d time.Duration) { sleeps = append(sleeps, d) },
	})
	f.now = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	f.d.Start()
	t.Cleanup(f.d.Stop)
	return f
}

func (f *fixture) ev(key string) ResolvedEvent {
	return ResolvedEvent{Seq: nextSeq(), Kind: "loop.waiting", Severity: "blocked",
		RootSlug: "alpha", LoopSlug: "demo", Title: "Loop waiting for you",
		AttentionKey: key, ComponentRoot: "/w/alpha"}
}

var seqCounter struct {
	mu sync.Mutex
	n  int64
}

func nextSeq() int64 {
	seqCounter.mu.Lock()
	defer seqCounter.mu.Unlock()
	seqCounter.n++
	return seqCounter.n
}

func (f *fixture) rows(t *testing.T) []store.NotifyDelivery {
	t.Helper()
	f.d.Stop() // drain: Stop is the sync point
	rows, err := f.st.ListDeliveries(f.ch.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func countOutcome(rows []store.NotifyDelivery, outcome string) int {
	n := 0
	for _, r := range rows {
		if r.Outcome == outcome {
			n++
		}
	}
	return n
}

func TestDeliverySentWithTheContractHeaders(t *testing.T) {
	srv, c := receiver(t, okHandler)
	f := newFixture(t, srv)
	f.d.Submit(f.ev("question:1"))
	rows := f.rows(t)
	if countOutcome(rows, "sent") != 1 {
		t.Fatalf("rows %+v, want one sent", rows)
	}
	body, h := c.last()
	if !bytes.Contains(body, []byte("loop.waiting")) {
		t.Errorf("envelope body %s", body)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type %q", h.Get("Content-Type"))
	}
	if h.Get("User-Agent") != "aeon/test" {
		t.Errorf("User-Agent %q", h.Get("User-Agent"))
	}
	sig := h.Get("X-Aeon-Signature")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("X-Aeon-Signature %q", sig)
	}
	if got := h.Get("X-Aeon-Delivery"); got != fmt.Sprintf("%s-%d", f.ch.ID, rows[0].EventSeq) {
		t.Errorf("X-Aeon-Delivery %q — the delivery id is channel-seq", got)
	}
	if h.Get("X-Aeon-Timestamp") == "" {
		t.Error("no X-Aeon-Timestamp")
	}
	// The row carries the seq and the key the log must be able to
	// explain a suppression by.
	if rows[0].AttentionKey != "question:1" || rows[0].Attempts != 1 {
		t.Errorf("row %+v", rows[0])
	}
}

func TestDedupSameAttentionKeyOnce(t *testing.T) {
	// The brief's example to the letter: ONE question produces both a
	// loop.waiting and a question.asked event; the receiver gets one
	// envelope, the log shows the second as suppressed-dedup.
	srv, c := receiver(t, okHandler)
	f := newFixture(t, srv)
	a := f.ev("question:42")
	b := f.ev("question:42")
	b.Kind = "question.asked"
	f.d.Submit(a)
	f.d.Submit(b)
	rows := f.rows(t)
	if c.count() != 1 {
		t.Fatalf("receiver got %d envelopes for one question", c.count())
	}
	if countOutcome(rows, "sent") != 1 || countOutcome(rows, "suppressed-dedup") != 1 {
		t.Fatalf("rows %+v, want 1 sent + 1 suppressed-dedup", rows)
	}
}

func TestRateLimitSummaryOncePerHour(t *testing.T) {
	srv, c := receiver(t, okHandler)
	f := newFixture(t, srv)
	// Tighten the ceiling to 2 (the permissive fixture subscription
	// re-set with the store verb, as a person would).
	if _, err := f.st.SetSubscription(store.SetSubscriptionIn{ChannelID: f.ch.ID,
		MinSeverity: "info", MaxPerHour: 2, ActorKind: "user", ActorID: "carlos"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		f.d.Submit(f.ev(fmt.Sprintf("k-%d", i)))
	}
	f.d.Stop() // partial drain
	mid, _ := f.st.ListDeliveries(f.ch.ID, 500)
	// Two sends earned, two rate suppressions, one summary — all now.
	// (The summary is itself a `sent` row: three sends, two of them real.)
	if countKeyed(mid, func(r store.NotifyDelivery) bool {
		return r.Outcome == "sent" && r.AttentionKey != "rate-summary"
	}) != 2 {
		t.Fatalf("want 2 real sends, rows %+v", mid)
	}
	if countOutcome(mid, "suppressed-rate") != 2 {
		t.Fatalf("want 2 suppressed-rate, rows %+v", mid)
	}
	if got := countSummary(mid); got != 1 {
		t.Fatalf("want exactly 1 rate.summary delivery, got %d", got)
	}
	// The window is still full; another suppression earns NO second
	// summary inside the hour.
	f.d = NewDispatcher(f.st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Clock: func() time.Time { return f.now }, Sleep: func(time.Duration) {}})
	f.d.Start()
	f.d.Submit(f.ev("k-9"))
	rows := f.rows(t)
	if countOutcome(rows, "suppressed-rate") != 3 {
		t.Fatalf("third suppression missing, rows %+v", rows)
	}
	if got := countSummary(rows); got != 1 {
		t.Fatalf("a second summary appeared inside the hour (%d)", got)
	}
	// The summary envelope itself: an honest count, kind rate.summary.
	if c.count() != 3 { // 2 real + 1 summary
		t.Fatalf("receiver got %d, want 3", c.count())
	}
	body, _ := c.last()
	if !bytes.Contains(body, []byte("rate.summary")) || !bytes.Contains(body, []byte("suppressed")) {
		t.Errorf("summary envelope %s", body)
	}
}

// TestCeilingHoldsUnderConcurrentSubmits — S58 T10's lock on the race
// the acceptance battery caught: two sender workers could both read the
// delivery count before either flight's row landed, and both send, so
// the ceiling capped only in the quiet case (TestRateLimitSummaryOnce-
// PerHour flaked 2/3 under load exactly that way). The close is the
// reservation in Dispatcher.rateInflight — the ceiling test counts
// committed rows PLUS in-flight reservations. A slow receiver forces the
// two workers to overlap; sends must never exceed the ceiling, and the
// two earned sends must never be starved by the mechanism either.
func TestCeilingHoldsUnderConcurrentSubmits(t *testing.T) {
	srv, _ := receiver(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		time.Sleep(25 * time.Millisecond) // the overlap the race needs
		w.WriteHeader(http.StatusOK)
	})
	f := newFixture(t, srv)
	if _, err := f.st.SetSubscription(store.SetSubscriptionIn{ChannelID: f.ch.ID,
		MinSeverity: "info", MaxPerHour: 2, ActorKind: "user", ActorID: "carlos"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		f.d.Submit(f.ev(fmt.Sprintf("race-%d", i)))
	}
	f.d.Stop()
	rows, err := f.st.ListDeliveries(f.ch.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	real := 0
	for _, r := range rows {
		if r.Outcome == "sent" && r.AttentionKey != "rate-summary" && r.AttentionKey != "digest" {
			real++
		}
	}
	if real != 2 {
		t.Fatalf("sends must be exactly the ceiling of 2 (never more; the two earned ones never starved): got %d, rows %+v", real, rows)
	}
	if got := countOutcome(rows, "suppressed-rate"); got != 6 {
		t.Fatalf("want 6 suppressed-rate, got %d (rows +%v)", got, rows)
	}
	// Six simultaneous suppressions must earn ONE summary, not a chorus —
	// the second race T10 caught (the once-an-hour guard was memory-free
	// and log-only, so two workers could both decide "none yet").
	if got := countSummary(rows); got != 1 {
		t.Fatalf("want exactly 1 rate.summary from six suppressions, got %d", got)
	}
}

func countKeyed(rows []store.NotifyDelivery, p func(store.NotifyDelivery) bool) int {
	n := 0
	for _, r := range rows {
		if p(r) {
			n++
		}
	}
	return n
}

func countSummary(rows []store.NotifyDelivery) int {
	n := 0
	for _, r := range rows {
		if r.AttentionKey == "rate-summary" {
			n++
		}
	}
	return n
}

func TestQuietHoursSuppressAndRecord(t *testing.T) {
	srv, c := receiver(t, okHandler)
	f := newFixture(t, srv)
	if _, err := f.st.SetSubscription(store.SetSubscriptionIn{ChannelID: f.ch.ID,
		MinSeverity: "info", QuietStart: "22:00", QuietEnd: "07:00",
		QuietZone: "Europe/Lisbon", ActorKind: "user", ActorID: "carlos"}); err != nil {
		t.Fatal(err)
	}
	f.now = time.Date(2026, 11, 3, 23, 30, 0, 0, time.UTC) // 23:30 Lisbon (UTC in November)
	f.d.Submit(f.ev("q-quiet"))
	f.d.Stop()
	if _, err := f.st.SetSubscription(store.SetSubscriptionIn{ChannelID: f.ch.ID,
		MinSeverity: "info", ActorKind: "user", ActorID: "carlos"}); err != nil {
		t.Fatal(err)
	}
	f.d = NewDispatcher(f.st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Clock: func() time.Time { return f.now }, Sleep: func(time.Duration) {}})
	f.d.Start()
	f.now = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) // noon: outside the window
	f.d.Submit(f.ev("q-day"))
	rows := f.rows(t)
	if countOutcome(rows, "suppressed-quiet") != 1 {
		t.Fatalf("quiet suppression missing, rows %+v", rows)
	}
	if countOutcome(rows, "sent") != 1 || c.count() != 1 {
		t.Fatalf("the daytime event did not send: rows %+v hits %d", rows, c.count())
	}
}

func TestQuietHoursOverMidnightAndDST(t *testing.T) {
	sub := store.NotifySubscription{QuietStart: "22:00", QuietEnd: "07:00", QuietZone: "Europe/Lisbon"}
	yes := []time.Time{
		time.Date(2026, 11, 3, 23, 30, 0, 0, time.UTC), // before midnight leg
		time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC),   // after-midnight leg
		time.Date(2026, 11, 3, 6, 59, 0, 0, time.UTC),  // the closing minute is IN
	}
	no := []time.Time{
		time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC), // the endpoint is EXCLUSIVE
		time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 3, 21, 59, 0, 0, time.UTC),
	}
	for _, tt := range yes {
		if !quietActive(sub, tt) {
			t.Errorf("%v (UTC): quiet window 22:00-07:00 Lisbon must be active", tt)
		}
	}
	for _, tt := range no {
		if quietActive(sub, tt) {
			t.Errorf("%v (UTC): window must be inactive", tt)
		}
	}
	// DST: Lisbon springs forward 2026-03-29 01:00→02:00. 01:30 local
	// DOES NOT EXIST — time.Now in that zone will never report it, but a
	// 02:30 UTC instant is 03:30 local: still inside 22:00-07:00. The
	// window must not silently open because a wall hour vanished.
	dst := time.Date(2026, 3, 29, 2, 30, 0, 0, time.UTC)
	if !quietActive(sub, dst) {
		t.Error("the DST night opened the quiet window")
	}
}

func TestRetryOnlyOnRetryableAndBounded(t *testing.T) {
	// 500 then 200: sent, two attempts, the 1 s rung slept.
	var n5 struct {
		mu sync.Mutex
		n  int
	}
	srv, _ := receiver(t, func(w http.ResponseWriter, _ *http.Request, n int) {
		n5.mu.Lock()
		n5.n = n
		n5.mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	f := newFixture(t, srv)
	f.d.Submit(f.ev("retry-1"))
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Outcome != "sent" || rows[0].Attempts != 2 {
		t.Fatalf("500-then-200 rows %+v, want one sent with 2 attempts", rows)
	}
	if len(*f.nk) != 1 || (*f.nk)[0] != time.Second {
		t.Errorf("sleeps %v, want the 1s rung exactly", *f.nk)
	}

	// 400: final — one attempt, no sleep, error kind status.
	srv2, _ := receiver(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(http.StatusBadRequest)
	})
	f2 := newFixture(t, srv2)
	f2.d.Submit(f2.ev("final-1"))
	rows2 := f2.rows(t)
	if len(rows2) != 1 || rows2[0].Outcome != "failed" || rows2[0].Attempts != 1 ||
		rows2[0].ErrorKind != "status" || rows2[0].HTTPStatus != 400 {
		t.Fatalf("400 rows %+v, want failed/1 attempt/status/400", rows2)
	}
	if len(*f2.nk) != 0 {
		t.Errorf("a final 4xx slept %v — no retry storm against a receiver that said no", *f2.nk)
	}

	// 429 forever: three attempts, ladder 1s then 4s, failed/status.
	srv3, _ := receiver(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	f3 := newFixture(t, srv3)
	f3.d.Submit(f3.ev("429-1"))
	rows3 := f3.rows(t)
	if len(rows3) != 1 || rows3[0].Outcome != "failed" || rows3[0].Attempts != 3 {
		t.Fatalf("429 rows %+v, want failed after 3 attempts", rows3)
	}
	if len(*f3.nk) != 2 || (*f3.nk)[0] != time.Second || (*f3.nk)[1] != 4*time.Second {
		t.Errorf("429 sleeps %v, want the 1s/4s ladder", *f3.nk)
	}
}

func TestRetryAfterHonouredAndCapped(t *testing.T) {
	// A 429 carrying Retry-After: 3600 is honoured (wait the receiver
	// asked for) but CAPPED at 60 s — a wait longer than a webhook retry
	// is the receiver telling us to stop, and the queue would age.
	srv, _ := receiver(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	f := newFixture(t, srv)
	f.d.Submit(f.ev("ra-1"))
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Outcome != "sent" || rows[0].Attempts != 2 {
		t.Fatalf("Retry-After rows %+v", rows)
	}
	if len(*f.nk) != 1 || (*f.nk)[0] != 60*time.Second {
		t.Errorf("sleeps %v, want the capped 60 s", *f.nk)
	}
}

func TestDeliveryLogHasNoBodyURLOrResponse(t *testing.T) {
	// The receiver answers 400 with text that must never be kept, and
	// the channel url carries a path secret. Neither the rows nor the
	// raw database file may contain either string — SV12's honesty is
	// structural: no column to put them in.
	home := isolateSecrets(t)
	st, storeDir := openChannelStoreIn(t, filepath.Join(home, "db"))
	const leak = "RESPONSE-LEAK-the-body-you-sent-was-bad"
	const pathSecret = "PATHSECRET-deep-token-path"
	srv, _ := receiver(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(leak))
	})
	ch, err := st.CreateChannel(store.CreateChannelIn{Name: "ops", URL: srv.URL + "/" + pathSecret,
		AllowPrivate: true, ActorKind: "user", ActorID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetChannelEnabled(ch.ID, true, "user", "c"); err != nil {
		t.Fatal(err)
	}
	if err := secrets.SaveChannelSecret(ch.ID, "file-secret-value"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetSubscription(store.SetSubscriptionIn{ChannelID: ch.ID,
		MinSeverity: "info", ActorKind: "user", ActorID: "c"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Sleep: func(time.Duration) {}})
	d.Start()
	d.Submit(ResolvedEvent{Seq: 1, Kind: "loop.finished", Severity: "info", Title: "done", AttentionKey: "k"})
	d.Stop()
	rows, _ := st.ListDeliveries(ch.ID, 10)
	if len(rows) != 1 || rows[0].Outcome != "failed" {
		t.Fatalf("rows %+v", rows)
	}
	for _, r := range rows {
		blob := fmt.Sprintf("%+v", r)
		for _, banned := range []string{leak, pathSecret, srv.URL, "file-secret-value"} {
			if strings.Contains(blob, banned) {
				t.Errorf("delivery row %q carries %q", blob, banned)
			}
		}
	}
	// The raw database bytes: the RESPONSE text is not there (no column
	// could hold it). The URL and the REFERENCE are — both the row needs.
	var raw []byte
	if err := filepath.Walk(storeDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, rerr := os.ReadFile(p); rerr == nil {
				raw = append(raw, b...)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(leak)) {
		t.Error("the receiver's response text is in the database bytes")
	}
	if bytes.Contains(raw, []byte("file-secret-value")) {
		t.Error("the secret VALUE is in the database bytes")
	}
}

func TestDispatcherNeverBlocksOrErrorsTheEventPath(t *testing.T) {
	// No senders are ever started: Submit must still return, bounded,
	// and EVERY overflow writes its row (per enabled channel). A loop
	// facing a dead receiver must never wait on the channel plane.
	st := openChannelStore(t)
	isolateSecrets(t)
	srv, c := receiver(t, okHandler)
	ch, err := st.CreateChannel(store.CreateChannelIn{Name: "ops", URL: srv.URL,
		AllowPrivate: true, ActorKind: "user", ActorID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetChannelEnabled(ch.ID, true, "user", "c"); err != nil {
		t.Fatal(err)
	}
	if err := secrets.SaveChannelSecret(ch.ID, "s"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetSubscription(store.SetSubscriptionIn{ChannelID: ch.ID,
		MinSeverity: "info", ActorKind: "user", ActorID: "c"}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Clock: func() time.Time { return time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) }})
	// deliberately NOT Start()ed: the queue is a closed room.
	start := time.Now()
	for i := 0; i < dispatchQueue+40; i++ {
		d.Submit(ResolvedEvent{Seq: int64(i), Kind: "loop.finished", Severity: "info",
			Title: "t", AttentionKey: fmt.Sprintf("k-%d", i)})
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("Submit blocked %v with a full queue — the loop would be paying for a dead receiver", el)
	}
	rows, _ := st.ListDeliveries(ch.ID, 500)
	if got := countOutcome(rows, "suppressed-overflow"); got != 40 {
		t.Errorf("overflow rows %d, want 40 (one per dropped event, per enabled channel)", got)
	}
	if c.count() != 0 {
		t.Error("something sent without a sender running")
	}
}

func TestDigestOnStartOnlyWhenItemsExist(t *testing.T) {
	srv, c := receiver(t, okHandler)
	f := newFixture(t, srv)
	f.d.SubmitDigest(0) // nothing needs attention: NOTHING is sent
	f.d.Stop()
	if c.count() != 0 {
		b, _ := c.last()
		t.Fatalf("digest fired with zero items: %s", b)
	}
	// A fresh dispatcher = a fresh start.
	f.d = NewDispatcher(f.st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Clock: func() time.Time { return f.now }, Sleep: func(time.Duration) {}})
	f.d.Start()
	f.d.SubmitDigest(3)
	f.d.SubmitDigest(9) // the SAME start digests once, whatever arrives later
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Outcome != "sent" {
		t.Fatalf("digest rows %+v, want exactly one sent", rows)
	}
	body, _ := c.last()
	if !bytes.Contains(body, []byte("inbox.digest")) || !bytes.Contains(body, []byte("3")) {
		t.Errorf("digest envelope %s — want event inbox.digest and the count", body)
	}
}

func TestZeroChannelsMeansZeroNotificationConnections(t *testing.T) {
	// The SV14 baseline for the nettest assertion: a daemon with no
	// channels makes NO outbound connection at all — the dispatcher with
	// nothing enabled dials nobody, however many events it is handed.
	st := openChannelStore(t)
	isolateSecrets(t)
	d := NewDispatcher(st, FileSecrets{}, DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Sleep: func(time.Duration) {}})
	d.Start()
	for i := 0; i < 5; i++ {
		d.Submit(ResolvedEvent{Seq: int64(i), Kind: "loop.finished", Severity: "info", Title: "t"})
	}
	d.Stop()
	rows, err := st.ListDeliveries("nc-none", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows without channels: %+v", rows)
	}
}
