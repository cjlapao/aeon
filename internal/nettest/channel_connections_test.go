package nettest

// channel_connections_test.go — S58 T9 (SV14, N1): the notification plane's
// two permanent connection claims, asserted HERE, where C11's recorder lives.
// T5 shipped the dispatcher-level versions in internal/notify (a store, a
// secret file, delivery rows); what was deferred to this task is the same
// behaviour observed through the nettest transport — the case the loop
// itself faces: a daemon running a full loop with channels wired, watched at
// the one place that records every Go-level HTTP call a delivery makes.
//
// The wiring seam is DispatcherOptions.Transport (S58 T9): the observation
// replaces the RoundTripper and nothing else, and production leaves it nil,
// so the SSRF-guarded dial stands outside these tests exactly as before.
//
// TestNotificationNetworkInvariantsExist (nettest_test.go) is the registry
// that fails if either permanent test in this pair is ever deleted.

import (
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/secrets"
	"github.com/cjlapao/aeon/internal/store"
)

// openNotifyStore is the same scratch store internal/notify's fixtures use —
// replicated, because a test helper is not an import surface.
func openNotifyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(store.Options{Dir: t.TempDir(),
		Env:   func(string) string { return "" },
		Clock: func() time.Time { return time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newRecordingDispatcher wires a dispatcher whose delivery transport IS the
// recorder: every host a send reaches for is observed, and the offline 200
// keeps the sender's behaviour (a sent row) intact.
func newRecordingDispatcher(t *testing.T, st *store.Store, rec *Recorder) *notify.Dispatcher {
	t.Helper()
	d := notify.NewDispatcher(st, notify.FileSecrets{}, notify.DispatcherOptions{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
		Sleep:     func(time.Duration) {},
		Transport: func(bool) http.RoundTripper { return rec.Transport() },
	})
	return d
}

// TestZeroChannelsMeansZeroNotificationConnections — see internal/notify's
// dispatcher-level sibling of the same name (that one is the store/rows
// claim). This is the C11-level claim: a full event load through a dispatcher
// with NO channel configured makes not one connection, observed at the
// transport, not merely zero delivery rows.
func TestZeroChannelsMakeNoConnectionAtTheTransport(t *testing.T) {
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	st := openNotifyStore(t)
	rec := NewRecorder(AllowedHosts(configuredRoutes, nil, nil))
	d := newRecordingDispatcher(t, st, rec)
	d.Start()
	for i := 0; i < 5; i++ {
		d.Submit(notify.ResolvedEvent{Seq: int64(i) + 1, Kind: "loop.finished",
			Severity: "info", Title: "Loop finished"})
	}
	d.Stop()
	if hosts := rec.Hosts(); len(hosts) != 0 {
		t.Fatalf("zero channels made connections to %v — an event with nowhere to go must reach for NO host (SV14)", hosts)
	}
}

// TestOneChannelMeansExactlyThatHost is T5's permanent claim run through the
// recorder: with exactly one enabled channel, the ONLY host a delivery loop
// connects to is that channel's host. One is the smallest number a mistake
// can hide in — a second, unnamed connection (a redirect, a telemetry
// mirror, a debug ping) fails this test by name, which the channel-level
// fixture in internal/notify could not see because it never watched the
// transport.
func TestOneChannelMeansExactlyThatHost(t *testing.T) {
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	st := openNotifyStore(t)
	ch, err := st.CreateChannel(store.CreateChannelIn{
		Name: "ops", URL: "https://hooks.example.test/hook",
		AllowPrivate: false, ActorKind: "user", ActorID: "carlos",
	})
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	if _, err := st.SetChannelEnabled(ch.ID, true, "user", "carlos"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := secrets.SaveChannelSecret(ch.ID, "nettest-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetSubscription(store.SetSubscriptionIn{
		ChannelID: ch.ID, MinSeverity: "info", ActorKind: "user", ActorID: "carlos",
	}); err != nil {
		t.Fatalf("subscription: %v", err)
	}

	rec := NewRecorder(AllowedHosts(configuredRoutes, nil, []string{"hooks.example.test"}))
	d := newRecordingDispatcher(t, st, rec)
	d.Start()
	d.Submit(notify.ResolvedEvent{Seq: 1, Kind: "loop.finished",
		Severity: "info", Title: "Loop finished"})
	d.Stop()

	// The delivery actually happened — a test that observes zero hosts while
	// sending nothing is a vacuous pass, so the row is the anti-vacuity half.
	rows, err := st.ListDeliveries(ch.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Outcome != "sent" {
		t.Fatalf("delivery rows %+v, want exactly one sent — the loop never reached the channel, so nothing was observed", rows)
	}
	// And EXACTLY that host: nothing else in the recorded set.
	if hosts := rec.Hosts(); len(hosts) != 1 || hosts[0] != "hooks.example.test" {
		t.Fatalf("hosts %+v, want exactly [hooks.example.test] — one channel means exactly that host (SV14)", hosts)
	}
	rec.AssertOnlyAllowed(t, []string{"analytics.example.com", "crash.example.com", "version.example.com"})
}
