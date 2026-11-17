package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/store"
)

// dispatcher.go — S58 T5 (SV8): the one outbound path. Events arrive
// through a bounded queue from the SAME publish point the socket
// subscribers see — the queue is non-blocking on purpose: a slow or dead
// receiver must never slow a loop (N: "the dispatcher cannot slow a
// loop"), and the honest accounting for a dropped event is a
// `suppressed-overflow` row, not silence.
//
// Per (channel, subscription) the pipeline is the design's order —
// filter (scope, severity, kinds) → disabled check → dedup → rate →
// quiet hours → send — and every outcome, including every suppression,
// lands in the append-only delivery log. Filters are NOT suppressions:
// an event a subscription does not care about writes no row (there is no
// such row in the closed set — the design says so; a non-match is not an
// event that was due).

// Queue depth and retry shape are the design's constants (256; three
// attempts, 1 s / 4 s backoff; Retry-After honoured up to 60 s).
const (
	dispatchQueue      = 256
	senderCount        = 2
	maxAttempts        = 3
	dedupWindow        = 10 * time.Minute
	rateWindow         = time.Hour
	maxSummaryInterval = time.Hour
	respBodyCap        = 4096
)

var backoffs = [maxAttempts]time.Duration{time.Second, 4 * time.Second, 0}

// Dispatcher is the daemon's sender. One queue, two senders, no state
// outside the store except two small windows (dedup, summary) that are
// process-scoped on purpose: after a restart the dedup memory is gone
// and the honest compensation is SV-D6's digest, not a resurrected map.
type Dispatcher struct {
	st      *store.Store
	secrets SecretStore
	log     *slog.Logger

	queue chan ResolvedEvent
	wg    sync.WaitGroup

	// now/clock seams: the windows (dedup, rate, summary) are wall-clock,
	// while the DELIVERY ROW's `at` is the store clock, so the recorded
	// timeline and the store's timeline agree. Tests advance this clock
	// instead of sleeping.
	now func() time.Time

	// clientFor caches the two transports (allow_private on/off) — the
	// ONLY two http.Clients any channel delivery uses.
	clientOnce sync.Once
	clientPub  *http.Client
	clientPriv *http.Client
	lookup     LookupHost
	// transport is the T9 nettest observation seam (nil in production:
	// GuardedTransport stands; see DispatcherOptions.Transport).
	transport func(allowPrivate bool) http.RoundTripper

	version string

	mu       sync.Mutex
	stopped  bool                 // Stop ran: Submit drops instead of panicking on a closed queue
	seen     map[string]time.Time // dedup: (channel|attentionKey) → last CLAIM
	sleep    func(time.Duration)  // the retry wait's clock (production: time.Sleep)
	digested int                  // digests this process (each start digests once)
	// rateInflight is T9's race close: with senderCount workers the store
	// count is stale while a send is mid-flight, so two workers could both
	// see "under the ceiling" and BOTH send — the cap would cap only in
	// the quiet case. A send reserves here before attempting and releases
	// when its row is recorded; the ceiling test counts committed rows
	// PLUS reservations. (T1's "the count is the delivery table" deviation
	// stands for the WINDOW; the instant a reservation lives in, a row
	// does not yet exist.) S58 T10, found by the T10 acceptance battery.
	rateInflight map[string]int // channelID → sends between check and record
	// summaryClaimed is the same shape for the hourly summary: the instant
	// between deciding and the row landing (S58 T10).
	summaryClaimed map[string]time.Time // channelID → summary decided at
}

// DispatcherOptions wire the seams. Lookup exists so the nettest/rebind
// tests can pin DNS without a /etc/hosts edit; Version rides the
// User-Agent.
type DispatcherOptions struct {
	Log     *slog.Logger
	Lookup  LookupHost
	Version string
	Clock   func() time.Time
	// Sleep is the retry wait's clock too (production time.Sleep; a test
	// measures the WAITS without paying them — the backoff ladder and the
	// Retry-After cap are behaviour, not wall time).
	Sleep func(time.Duration)
	// Transport is the nettest observation seam (S58 T9): when set, both
	// delivery clients use it INSTEAD OF the per-policy GuardedTransport,
	// so the recording transport can watch which hosts a delivery reaches
	// for. Production never sets it — nil means the SSRF-guarded dial
	// stands, the same two clients as always. The seam swaps the
	// RoundTripper and nothing else: NoRedirects, timeouts, queueing and
	// the delivery rows are untouched.
	Transport func(allowPrivate bool) http.RoundTripper
}

func NewDispatcher(st *store.Store, secrets SecretStore, opts DispatcherOptions) *Dispatcher {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Now().UTC() }
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
	d := &Dispatcher{
		st: st, secrets: secrets, log: opts.Log, lookup: opts.Lookup,
		version: opts.Version, now: opts.Clock, sleep: opts.Sleep,
		transport:      opts.Transport,
		queue:          make(chan ResolvedEvent, dispatchQueue),
		seen:           map[string]time.Time{},
		rateInflight:   map[string]int{},
		summaryClaimed: map[string]time.Time{},
	}
	return d
}

// Start opens the senders. Stop drains: a closing daemon finishes the
// queued deliveries rather than losing them silently (bounded: the queue
// is finite and each send has its own timeout).
func (d *Dispatcher) Start() {
	for range senderCount {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			for ev := range d.queue {
				d.handle(ev)
			}
		}()
	}
}

// Stop is idempotent (a drain hook and a test cleanup may both call it):
// the first call closes the queue and waits for the senders; later calls
// wait for the same already-finished group.
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	if !d.stopped {
		d.stopped = true
		close(d.queue)
	}
	d.mu.Unlock()
	d.wg.Wait()
}

// Submit is the daemon's publish-point call: non-blocking, drop-oldest.
// Overflow is recorded — per enabled channel, because "the event will not
// reach you" is a fact about each channel, and an unrecorded drop is the
// exact silence SV12 forbids.
func (d *Dispatcher) Submit(ev ResolvedEvent) {
	d.mu.Lock()
	stopped := d.stopped
	d.mu.Unlock()
	if stopped {
		d.log.Info("notify: dropped an event after shutdown (the daemon is leaving; the row would outlive the sender)", "kind", ev.Kind)
		return
	}
	select {
	case d.queue <- ev:
	default:
		select {
		case <-d.queue: // dropped the oldest — say so honestly
		default:
		}
		chans, err := d.st.ListChannels(false)
		if err == nil {
			for _, ch := range chans {
				if !ch.Enabled {
					continue
				}
				_, _ = d.st.AppendDelivery(store.AppendDeliveryIn{
					ChannelID: ch.ID, EventSeq: ev.Seq, AttentionKey: ev.AttentionKey,
					Outcome: "suppressed-overflow",
				})
			}
		}
		select {
		case d.queue <- ev:
		default: // still full (a concurrent producer won) — this one goes, unrecorded-elsewhere
		}
	}
}

// SubmitDigest is SV-D6: one digest per enabled channel AT DAEMON START,
// only when items exist. Never a replay of the missed per-event
// messages — they are stale the moment they age.
func (d *Dispatcher) SubmitDigest(count int) {
	if count <= 0 {
		return
	}
	d.mu.Lock()
	first := d.digested == 0
	d.digested++
	d.mu.Unlock()
	if !first {
		return // "at daemon start" means once per start
	}
	d.queue <- ResolvedEvent{IsDigest: true, DigestCount: count, Severity: "info"}
}

// handle is one sender's unit of work.
func (d *Dispatcher) handle(ev ResolvedEvent) {
	chans, err := d.st.ListChannels(false)
	if err != nil {
		d.log.Error("notify: dispatcher cannot read channels", "err", err)
		return
	}
	for _, ch := range chans {
		if ev.IsDigest {
			d.handleDigest(ch, ev)
			continue
		}
		subs, err := d.st.ListSubscriptions(ch.ID)
		if err != nil {
			d.log.Error("notify: dispatcher cannot read subscriptions", "channel", ch.ID, "err", err)
			continue
		}
		for _, sub := range subs {
			d.pipeline(ch, sub, ev)
		}
	}
}

func (d *Dispatcher) handleDigest(ch store.NotifyChannel, ev ResolvedEvent) {
	if !ch.Enabled {
		return // the digest goes to enabled channels only; disabled ones miss nothing new
	}
	if d.quietNowForChannel(ch.ID, d.now()) {
		d.record(ch.ID, 0, "digest", "suppressed-quiet", "", 0, 0)
		return
	}
	ev.DeliveryID = ch.ID + "-digest-" + d.stamp()
	ev.At = d.storeNow()
	d.deliver(ch, BuildDigest(ev), "digest")
}

// quietNowForChannel answers "is ANY of this channel's subscriptions in
// its quiet window right now" — used by the digest, which has no single
// subscription to own it.
func (d *Dispatcher) quietNowForChannel(channelID string, now time.Time) bool {
	subs, err := d.st.ListSubscriptions(channelID)
	if err != nil {
		return false
	}
	for _, s := range subs {
		if quietActive(s, now) {
			return true
		}
	}
	return false
}

// pipeline is the design's ladder, in the design's order.
func (d *Dispatcher) pipeline(ch store.NotifyChannel, sub store.NotifySubscription, ev ResolvedEvent) {
	// 1. filter — non-interest writes no row (see the file header).
	if !matchesSubscription(sub, ev) {
		return
	}
	// 2. disabled/archived between queue and send.
	if !ch.Enabled || ch.ArchivedAt != "" {
		d.record(ch.ID, ev.Seq, ev.AttentionKey, "suppressed-disabled", "", 0, 0)
		return
	}
	// 3. dedup: (channel, attentionKey) inside 10 minutes of a SEND. An
	// event whose key is empty (nothing repeatable) cannot dedup. The
	// CLAIM happens at the check, not after the send: two events about the
	// SAME fact (loop.waiting and question.asked for one question) can be
	// dequeued by the two senders at the same instant, and a mark written
	// only when the first send COMPLETES lets both pass the check and
	// delivers the same fact twice. At-most-once per window is the
	// guarantee; a failed first claim is not retried by its sibling —
	// a notification nobody got beats a notification received twice.
	if ev.AttentionKey != "" {
		key := ch.ID + "|" + ev.AttentionKey
		d.mu.Lock()
		last, ok := d.seen[key]
		fresh := !ok || d.now().Sub(last) >= dedupWindow
		if fresh {
			d.seen[key] = d.now()
		}
		d.mu.Unlock()
		if !fresh {
			d.record(ch.ID, ev.Seq, ev.AttentionKey, "suppressed-dedup", "", 0, 0)
			return
		}
	}
	// 4. rate: the ceiling is each subscription's, the COUNT is the
	// channel's (the delivery table has no subscription column — T1's
	// recorded deviation, and the dispatcher honours it here).
	ceiling := sub.MaxPerHour
	if ceiling == 0 {
		ceiling = 30
	}
	since := isoStore(d.now().Add(-rateWindow))
	n, err := d.st.RecentDeliveryCount(ch.ID, since)
	if err != nil {
		d.log.Error("notify: rate window read failed", "channel", ch.ID, "err", err)
	}
	d.mu.Lock()
	n += d.rateInflight[ch.ID] // a mid-flight send is a send (T10 race close)
	d.mu.Unlock()
	if n >= ceiling {
		d.record(ch.ID, ev.Seq, ev.AttentionKey, "suppressed-rate", "", 0, 0)
		d.rateSummary(ch)
		return
	}
	// Reserve before the quiet check: from this point the slot is spoken
	// for, so a second worker reading the count sees the truth even while
	// this send is in flight. Released when this pipeline pass returns —
	// the row (sent or not) is recorded by then, or nothing was sent.
	d.mu.Lock()
	d.rateInflight[ch.ID]++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.rateInflight[ch.ID]--
		d.mu.Unlock()
	}()
	// 5. quiet hours.
	if quietActive(sub, d.now()) {
		d.record(ch.ID, ev.Seq, ev.AttentionKey, "suppressed-quiet", "", 0, 0)
		return
	}
	// 6. send.
	ev.DeliveryID = fmt.Sprintf("%s-%d", ch.ID, ev.Seq)
	ev.At = d.storeNow()
	outcome, kind, status, attempts := d.attempt(ch, Build(ev))
	d.record(ch.ID, ev.Seq, ev.AttentionKey, outcome, kind, status, attempts)
}

// rateSummary is the design's "ONE summary delivery per hour reporting
// how many were suppressed" — first-suppression-then-report shape: the
// summary rides the moment the window fills, and says how many
// suppressions this window has already produced (the delivery log's
// recent page, capped at its 50 — "at least" is the honest floor and
// the sentence says so).
func (d *Dispatcher) rateSummary(ch store.NotifyChannel) {
	now := d.now()
	suppressed := 0
	var lastSummaryAt string
	// The decision is made under d.mu and CLAIMED there: with
	// senderCount workers, two simultaneous rate suppressions both read
	// "no summary yet" from the log and BOTH send one — "ONE summary per
	// hour" was two-headed exactly like the rate race T10's acceptance
	// battery caught. The claim lives in memory for the instant between
	// deciding and the row landing; history itself is read from the
	// delivery log, so a restart never re-arms the hour.
	d.mu.Lock()
	if claimed, ok := d.summaryClaimed[ch.ID]; ok {
		if at := isoStore(claimed); at > lastSummaryAt {
			lastSummaryAt = at
		}
	}
	rows, _ := d.st.ListDeliveries(ch.ID, 50)
	cutoff := isoStore(now.Add(-rateWindow))
	for _, r := range rows {
		switch {
		case r.AttentionKey == "rate-summary" && r.At > lastSummaryAt:
			lastSummaryAt = r.At
		case r.Outcome == "suppressed-rate" && r.At >= cutoff:
			suppressed++
		}
	}
	// The once-an-hour guard reads the DELIVERY LOG plus the in-flight
	// claim, not process memory ALONE: "at most one summary per hour per
	// channel" must survive a daemon restart, or restarting would be a way
	// to shout more often.
	if lastSummaryAt >= cutoff {
		d.mu.Unlock()
		return
	}
	d.summaryClaimed[ch.ID] = now
	d.mu.Unlock()
	ev := ResolvedEvent{
		DeliveryID:   ch.ID + "-rate-summary-" + d.stamp(),
		At:           d.storeNow(),
		AttentionKey: "rate-summary",
		Title:        fmt.Sprintf("At least %d notifications were suppressed by the rate limit in the last hour", suppressed),
		Severity:     "info",
	}
	e := Build(ev)
	e.Event = "rate.summary"
	e.Kind = "rate.summary"
	d.deliver(ch, e, "rate-summary")
}

// Test sends the fixed test envelope NOW (the person clicked — the reply
// is the outcome), through the same attempt machinery, logged like any
// delivery (SV7).
func (d *Dispatcher) Test(channelID string) (store.NotifyDelivery, error) {
	ch, found, err := d.st.GetChannel(channelID)
	if err != nil || !found {
		return store.NotifyDelivery{}, fmt.Errorf("notify: channel %q does not exist", channelID)
	}
	if ch.ArchivedAt != "" {
		return store.NotifyDelivery{}, fmt.Errorf("notify: channel %q is archived (%s)", channelID, ch.ArchiveReason)
	}
	if !ch.Enabled {
		return store.NotifyDelivery{}, fmt.Errorf("notify: channel %q is disabled — enabling it is the decision that lets it send", channelID)
	}
	ev := ResolvedEvent{IsTest: true, DeliveryID: ch.ID + "-test-" + d.stamp(),
		At: d.storeNow(), AttentionKey: "test", Severity: "info",
		Title: "Aeon test notification — if you can read this, the channel works"}
	outcome, kind, status, attempts := d.attempt(ch, Build(ev))
	id, err := d.st.AppendDelivery(store.AppendDeliveryIn{
		ChannelID: ch.ID, AttentionKey: "test", Outcome: outcome,
		ErrorKind: kind, HTTPStatus: status, Attempts: attempts})
	if err != nil {
		return store.NotifyDelivery{}, err
	}
	rows, _ := d.st.ListDeliveries(ch.ID, 1)
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return store.NotifyDelivery{ID: id, ChannelID: ch.ID, Outcome: outcome,
		ErrorKind: kind, HTTPStatus: status, Attempts: attempts}, nil
}

func (d *Dispatcher) record(channelID string, seq int64, key, outcome, kind string, status, attempts int) {
	if _, err := d.st.AppendDelivery(store.AppendDeliveryIn{
		ChannelID: channelID, EventSeq: seq, AttentionKey: key,
		Outcome: outcome, ErrorKind: kind, HTTPStatus: status, Attempts: attempts}); err != nil {
		d.log.Error("notify: cannot record a delivery outcome", "channel", channelID, "outcome", outcome, "err", err)
	}
}

func (d *Dispatcher) deliver(ch store.NotifyChannel, e Envelope, key string) {
	outcome, kind, status, attempts := d.attempt(ch, e)
	d.record(ch.ID, 0, key, outcome, kind, status, attempts)
}

// attempt is the bounded retry: at most 3 sends, 1 s / 4 s apart, and
// ONLY transport errors, timeouts, 5xx and 429 retry (a 4xx is final —
// the receiver has told us it will not take this). A refused address is
// final at once (retrying an SSRF check is how lockouts get disabled).
func (d *Dispatcher) attempt(ch store.NotifyChannel, e Envelope) (string, string, int, int) {
	secret, err := d.secrets.Get(ch.SecretRef)
	if err != nil || len(secret) == 0 {
		return "failed", "secret-missing", 0, 0
	}
	body, err := Marshal(e)
	if err != nil {
		return "failed", "url-invalid", 0, 0 // unreachable by construction; honest kind if it ever fires
	}
	u, err := url.Parse(ch.URL)
	if err != nil {
		return "failed", "url-invalid", 0, 0
	}
	client := d.client(ch.AllowPrivate)
	kind, status, lastErr := "", 0, error(nil)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			cancel()
			return "failed", "url-invalid", 0, attempt - 1
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "aeon/"+d.version)
		req.Header.Set("X-Aeon-Signature", SignatureHeader(secret, body))
		req.Header.Set("X-Aeon-Delivery", e.ID)
		req.Header.Set("X-Aeon-Timestamp", e.At)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			kind = "transport"
			if IsRefusedAddress(err) {
				return "failed", "refused-address", 0, attempt
			}
			if isTimeout(err) {
				kind = "timeout"
			}
			if attempt < maxAttempts {
				d.sleep(backoffs[attempt-1])
			}
			continue
		}
		// Read the body — capped and DISCARDED (SV12: no response text
		// is kept; the status is the whole of what we learn).
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, respBodyCap))
		_ = resp.Body.Close()
		cancel()
		status = resp.StatusCode
		if status >= 200 && status < 300 {
			return "sent", "", status, attempt
		}
		kind = "status"
		retryable := status >= 500 || status == http.StatusTooManyRequests
		if !retryable {
			return "failed", kind, status, attempt // a 4xx is final
		}
		if attempt < maxAttempts {
			d.sleep(d.retryWait(resp, attempt))
		}
	}
	if lastErr != nil && IsRefusedAddress(lastErr) {
		return "failed", "refused-address", 0, maxAttempts
	}
	return "failed", kind, status, maxAttempts
}

// retryWait honours Retry-After (delta-seconds form) up to 60 s —
// a longer wait than a webhook retry is the receiver telling us to stop,
// and the ceiling that keeps a channel from being hammered is the cap.
func (d *Dispatcher) retryWait(resp *http.Response, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(ra)); err == nil && secs > 0 {
			if secs > 60 {
				secs = 60
			}
			return time.Duration(secs) * time.Second
		}
	}
	return backoffs[attempt-1]
}

func (d *Dispatcher) client(allowPrivate bool) *http.Client {
	d.clientOnce.Do(func() {
		// The seam is resolved ONCE here, beside the production default it
		// replaces — a test never sees a half-swapped pair of clients.
		rt := func(priv bool) http.RoundTripper {
			if d.transport != nil {
				return d.transport(priv)
			}
			return GuardedTransport(priv, d.lookup)
		}
		d.clientPriv = &http.Client{Transport: rt(true), CheckRedirect: NoRedirects, Timeout: 10 * time.Second}
		d.clientPub = &http.Client{Transport: rt(false), CheckRedirect: NoRedirects, Timeout: 10 * time.Second}
	})
	if allowPrivate {
		return d.clientPriv
	}
	return d.clientPub
}

func (d *Dispatcher) stamp() string { return isoStore(d.now()) }

func (d *Dispatcher) storeNow() string {
	// The delivery row's `at` is the store clock (empty = now there, too);
	// the envelope's `at` matches it so the receiver sees the same instant
	// the log records.
	return d.st.Now()
}

// ---------- the pure parts (table-tested below) ----------

// matchesSubscription is the filter: the root scope (the event's root,
// ANY ancestor or child of the subscription's root, or every-root),
// min_severity (rank order: a needs-you event clears every ceiling; an
// info event only an info ceiling), and kinds.
func matchesSubscription(sub store.NotifySubscription, ev ResolvedEvent) bool {
	if sub.RootID != "" && !inScope(sub.RootID, ev.ComponentRoot) {
		return false
	}
	if sub.MinSeverity != "" && severityRank(ev.Severity) > severityRank(sub.MinSeverity) {
		return false
	}
	if sub.Kinds != "" {
		var list []string
		if err := json.Unmarshal([]byte(sub.Kinds), &list); err != nil {
			return false // a stored filter that cannot parse matches nothing; visible in the row's absence
		}
		name := ev.Kind
		if ev.IsTest {
			name = "test"
		} else if ev.IsDigest {
			name = "inbox.digest"
		}
		if name == "inbox.digest" || name == "rate.summary" || name == "test" {
			// synthetics pass a kinds filter only when named — they are
			// not "the events", they are the channel's own housekeeping.
			if !containsStr(list, name) {
				return false
			}
			return true
		}
		if !containsStr(list, name) {
			return false
		}
	}
	return true
}

// inScope is attention's tree rule over PATHS (subscription roots are
// component ids; the dispatcher was handed the event's component ROOT
// PATH, and ResolveScope turns an id into the tree's paths — a child's
// paths count, so a subscription on a parent covers a child's loops).
func inScope(rootID, eventRootPath string) bool {
	scopeMu.RLock()
	covers := scopeCovers
	scopeMu.RUnlock()
	return covers(rootID, eventRootPath)
}

// scopeCovers is split for the tests (a store reader is the real answer;
// the pure form is exercised through the daemon wiring).
// scopeMu guards the resolver seam: the daemon installs it at wiring, and
// daemons constructed concurrently (S27 T11's parallel test law) install
// concurrently. Readers take the value once and call it outside the lock.
var scopeMu sync.RWMutex

var scopeCovers = func(rootID, eventRootPath string) bool { return false }

// RegisterScopeResolver installs the store-backed tree answer (the daemon
// calls it once at wiring). Uninstalled ⇒ subscriptions never match:
// fail-closed, a channel that silently stops matching is louder in the
// delivery log's ABSENCE than a channel that guesses the tree.
func RegisterScopeResolver(st *store.Store) {
	covers := func(rootID, eventRootPath string) bool {
		if eventRootPath == "" {
			return false
		}
		sc, err := attention.ResolveScope(st, rootID)
		if err != nil {
			return false
		}
		for _, p := range sc.RootPaths {
			if eventRootPath == p || strings.HasPrefix(eventRootPath, strings.TrimSuffix(p, "/")+"/") {
				return true
			}
		}
		return false
	}
	scopeMu.Lock()
	scopeCovers = covers
	scopeMu.Unlock()
}

func severityRank(s string) int {
	for i, v := range attention.Severities() {
		if string(v) == s {
			return i
		}
	}
	return len(attention.Severities()) // unknown ranks worst: never silently over-deliver
}

// quietActive is the window rule: [start, end) in the named IANA zone,
// wrapping midnight when start > end (22:00–07:00 is one window, not a
// bug). DST is the zone's business — LoadLocation gives the offsets and
// wall-clock comparisons handle the shift (TR6's rule: the window is
// LOCAL wall-clock, which is exactly why the zone must be explicit).
func quietActive(sub store.NotifySubscription, now time.Time) bool {
	if sub.QuietStart == "" || sub.QuietEnd == "" {
		return false
	}
	loc, err := time.LoadLocation(sub.QuietZone)
	if err != nil {
		return false // validated at write time; an operator edit that broke it fails OPEN (loud in the log)
	}
	start, err1 := minuteOfDay(sub.QuietStart)
	end, err2 := minuteOfDay(sub.QuietEnd)
	if err1 != nil || err2 != nil {
		return false
	}
	t := now.In(loc)
	cur := t.Hour()*60 + t.Minute()
	if start <= end {
		return cur >= start && cur < end
	}
	return cur >= start || cur < end
}

func minuteOfDay(hm string) (int, error) {
	parts := strings.Split(hm, ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("notify: %q is not HH:MM", hm)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	return h*60 + m, nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// isoStore formats a time in the store's own layout so string comparisons
// against delivery rows stay lexicographic-truthful.
func isoStore(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne interface{ Timeout() bool }
	return errors.As(err, &ne) && ne.Timeout()
}
