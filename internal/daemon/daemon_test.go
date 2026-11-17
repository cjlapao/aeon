package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/go-backend-scaffolding/framework/localrpc"

	"github.com/cjlapao/aeon/internal/protocol"
)

// A recorder for what would have gone on the wire. The whole daemon is
// testable without binding a socket, which is how `client.connected carries a
// pid` is asserted without starting a second process.
type wire struct {
	mu   sync.Mutex
	sent []sentNotification
	logs []string
	// conns is what the transport would report for peer credentials.
	conns []localrpc.ConnInfo
}

type sentNotification struct {
	ConnID string
	Method string
	Event  protocol.Event
	// Raw keeps the params exactly as sent (S31 T7: `terminal.output` /
	// `terminal.exit` payloads are NOT protocol.Events — reading them only
	// through Event would silently drop every terminal field).
	Raw json.RawMessage
}

func (w *wire) notify(connID, method string, params any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var ev protocol.Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return err
	}
	w.sent = append(w.sent, sentNotification{ConnID: connID, Method: method, Event: ev,
		Raw: append(json.RawMessage(nil), raw...)})
	return nil
}

func (w *wire) Infof(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.logs = append(w.logs, fmt.Sprintf(format, args...))
}

func (w *wire) delivered() []sentNotification {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]sentNotification, len(w.sent))
	copy(out, w.sent)
	return out
}

func (w *wire) logLines() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.logs, "\n")
}

func newTestDaemon(t *testing.T) (*Daemon, *wire) {
	t.Helper()
	w := &wire{}
	d := New(Options{
		DaemonVersion: "9.9.9-test",
		BuildTime:     "2026-10-15T00:00:00Z",
		GitCommit:     "deadbee",
		Logger:        w,
		Notify:        w.notify,
		Connections:   func() []localrpc.ConnInfo { return w.conns },
	})
	return d, w
}

func initConn(t *testing.T, d *Daemon, ctx context.Context, connID, name string) {
	t.Helper()
	params, _ := json.Marshal(protocol.InitializeParams{
		ClientName: name, ClientVersion: "0.1.0", ProtocolVersion: protocol.PROTOCOL_VERSION,
	})
	if _, err := d.Table.Dispatch(ctx, connID, protocol.MethodInitialize, params); err != nil {
		t.Fatalf("initialize %s: %v", connID, err)
	}
}

func subscribe(t *testing.T, d *Daemon, connID string, p protocol.SubscribeParams) protocol.SubscribeResult {
	t.Helper()
	params, _ := json.Marshal(p)
	res, err := d.Table.Dispatch(context.Background(), connID, protocol.MethodSubscribe, params)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	return res.(protocol.SubscribeResult)
}

// D8 — the test T8 exists to make possible: a second client connecting produces
// `client.connected` on the FIRST client's subscription.
func TestConnectingASecondClientProducesClientConnectedOnTheFirstsSubscription(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()

	initConn(t, d, ctx, "c1", "watcher")
	sub := subscribe(t, d, "c1", protocol.SubscribeParams{})

	before := len(w.delivered())
	w.conns = []localrpc.ConnInfo{{ID: "c2", PeerUID: 1000, PeerPID: 4242}}
	initConn(t, d, ctx, "c2", "aeon-cli")

	var connected *protocol.Event
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindClientConnected {
			if s.ConnID != "c1" {
				t.Errorf("client.connected went to %s, want the subscriber c1", s.ConnID)
			}
			if s.Event.SubscriptionID != sub.SubscriptionID {
				t.Errorf("event tagged %q, want %q", s.Event.SubscriptionID, sub.SubscriptionID)
			}
			ev := s.Event
			connected = &ev
		}
	}
	if connected == nil {
		t.Fatal("the first client's subscription never received client.connected — this is " +
			"the event that makes subscriptions demonstrable before any domain exists (T8)")
	}
}

// D12 / T4 — the payload carries the client's name, version and pid.
func TestClientConnectedCarriesTheClientNameVersionAndPid(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()

	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	w.conns = []localrpc.ConnInfo{{ID: "c2", PeerUID: 1000, PeerPID: 4242}}
	initConn(t, d, ctx, "c2", "aeon-cli")

	for _, s := range w.delivered() {
		if s.Event.Kind != protocol.KindClientConnected {
			continue
		}
		var p ClientConnectedPayload
		if err := json.Unmarshal(s.Event.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.ConnID != "c2" {
			continue
		}
		if p.ClientName != "aeon-cli" || p.ClientVersion != "0.1.0" {
			t.Errorf("client = %s/%s, want aeon-cli/0.1.0", p.ClientName, p.ClientVersion)
		}
		if p.ProtocolVersion != protocol.PROTOCOL_VERSION {
			t.Errorf("protocolVersion = %d, want the negotiated %d", p.ProtocolVersion, protocol.PROTOCOL_VERSION)
		}
		if p.PeerPID != 4242 || !p.PeerPIDAvailable {
			t.Errorf("peerPid = %d (available=%v), want 4242 and true", p.PeerPID, p.PeerPIDAvailable)
		}
		if p.PeerUID != 1000 {
			t.Errorf("peerUid = %d, want 1000", p.PeerUID)
		}
		return
	}
	t.Fatal("no client.connected for c2 was delivered")
}

// A platform with no pid must say so rather than report 0 as though it were
// one: inventing a pid sends somebody to kill the wrong process.
func TestAnUnavailablePidIsANamedAbsenceNotAZero(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	// A platform that reports no pid: PeerPID stays 0.
	w.conns = []localrpc.ConnInfo{{ID: "c2", PeerUID: 1000, PeerPID: 0}}
	initConn(t, d, ctx, "c2", "aeon-cli")

	for _, s := range w.delivered() {
		if s.Event.Kind != protocol.KindClientConnected {
			continue
		}
		var p ClientConnectedPayload
		_ = json.Unmarshal(s.Event.Payload, &p)
		if p.ConnID != "c2" {
			continue
		}
		if p.PeerPIDAvailable {
			t.Error("peerPidAvailable is true for a platform reporting no pid")
		}
	}
	if !strings.Contains(w.logLines(), "pid=unavailable-on-this-platform") {
		t.Errorf("the accept log does not name the absence (D12):\n%s", w.logLines())
	}
}

// D12 — uid and pid in the accept log where the platform allows.
func TestTheAcceptLogNamesUidAndPid(t *testing.T) {
	d, w := newTestDaemon(t)
	w.conns = []localrpc.ConnInfo{{ID: "c1", PeerUID: 1000, PeerPID: 4242}}
	initConn(t, d, context.Background(), "c1", "aeon-cli")

	line := w.logLines()
	for _, want := range []string{"uid=1000", "pid=4242", fmt.Sprintf("protocol=%d", protocol.PROTOCOL_VERSION), "aeon-cli"} {
		if !strings.Contains(line, want) {
			t.Errorf("the accept log does not contain %q:\n%s", want, line)
		}
	}
}

func TestAllFourLifecycleKindsAreEmitted(t *testing.T) {
	d, w := newTestDaemon(t)

	// The watcher must OUTLIVE the disconnect it is watching for. First
	// attempt put the watcher on the cancelled context, so `client.disconnected`
	// was published to nobody — correct behaviour, wrong test.
	initConn(t, d, context.Background(), "watcher", "watcher")
	subscribe(t, d, "watcher", protocol.SubscribeParams{})

	d.Started("/tmp/test.sock")

	ctx, cancel := context.WithCancel(context.Background())
	initConn(t, d, ctx, "c2", "other")
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// The four LIFECYCLE kinds, named rather than taken from
		// `protocol.Kinds()` — that list grows with every slice that adds an
		// event, and S3's `ask.*` made this fail on correct code. A test
		// demanding everything the protocol can emit is a test that fails on
		// each addition and gets deleted rather than understood.
		if containsKinds(w.delivered(), []string{
			protocol.KindDaemonStarted,
			protocol.KindClientConnected,
			protocol.KindClientDisconnected,
			protocol.KindSubscriptionCreated,
		}) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	var got []string
	for _, s := range w.delivered() {
		got = append(got, s.Event.Kind)
	}
	t.Fatalf("not all four lifecycle kinds were emitted; saw %v", got)
}

func containsKinds(sent []sentNotification, want []string) bool {
	seen := map[string]bool{}
	for _, s := range sent {
		seen[s.Event.Kind] = true
	}
	for _, k := range want {
		if !seen[k] {
			return false
		}
	}
	return true
}

// T5 / D6 — the registry empties when the connection's context is cancelled,
// which is the only close signal the transport provides.
func TestClosingAConnectionReapsItsSubscriptionsAndAnnouncesIt(t *testing.T) {
	d, w := newTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())

	initConn(t, d, context.Background(), "watcher", "watcher")
	subscribe(t, d, "watcher", protocol.SubscribeParams{})

	initConn(t, d, ctx, "c2", "leaving")
	subscribe(t, d, "c2", protocol.SubscribeParams{})
	if d.Table.Subs.Len() != 2 {
		t.Fatalf("registry holds %d, want 2", d.Table.Subs.Len())
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && d.Table.Subs.LenConn("c2") != 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if n := d.Table.Subs.LenConn("c2"); n != 0 {
		t.Fatalf("c2 still holds %d subscriptions after its connection closed", n)
	}
	if n := d.Table.Subs.LenConn("watcher"); n != 1 {
		t.Fatalf("the watcher's subscription was reaped too (%d)", n)
	}

	var announced bool
	for _, s := range w.delivered() {
		if s.Event.Kind == protocol.KindClientDisconnected {
			announced = true
		}
	}
	if !announced {
		t.Error("client.disconnected was never emitted")
	}
}

// The error mapping, which is where a code could silently become -32603.
func TestErrorMapping(t *testing.T) {
	d, _ := newTestDaemon(t)

	t.Run("an Aeon error keeps its code and carries the three parts", func(t *testing.T) {
		err := d.toWireError(protocol.ErrNotInitialized.WithWhat("method %q too early", "ping"))
		var wireErr *localrpc.Error
		if !errors.As(err, &wireErr) {
			t.Fatalf("want *localrpc.Error, got %T", err)
		}
		if wireErr.Code != -31001 {
			t.Fatalf("code = %d, want -31001", wireErr.Code)
		}
		var data protocol.Data
		if err := json.Unmarshal(wireErr.Data, &data); err != nil {
			t.Fatalf("the three-part data did not survive: %v (%s)", err, wireErr.Data)
		}
		if data.What == "" || data.Means == "" || data.Todo == "" || !data.Fatal {
			t.Fatalf("data lost a part: %+v", data)
		}
		if !strings.Contains(data.What, "ping") {
			t.Errorf("the specifics were lost: %q", data.What)
		}
	})

	t.Run("an unknown method becomes JSON-RPC's own -32601", func(t *testing.T) {
		err := d.toWireError(protocol.ErrUnknownMethod{Name: "nope"})
		var wireErr *localrpc.Error
		if !errors.As(err, &wireErr) || wireErr.Code != -32601 {
			t.Fatalf("want -32601, got %v", err)
		}
	})

	t.Run("bad params become JSON-RPC's own -32602", func(t *testing.T) {
		err := d.toWireError(protocol.ErrBadParams{Err: errors.New("boom")})
		var wireErr *localrpc.Error
		if !errors.As(err, &wireErr) || wireErr.Code != -32602 {
			t.Fatalf("want -32602, got %v", err)
		}
	})
}

// The replay reaches the client BEFORE `subscription.created`, or a client
// would read its own backlog as live traffic that arrived after creation.
func TestReplayIsDeliveredBeforeSubscriptionCreated(t *testing.T) {
	d, w := newTestDaemon(t)
	initConn(t, d, context.Background(), "c1", "watcher")

	// Three events exist before anyone subscribes.
	for i := 0; i < 3; i++ {
		d.Publish("loop.started", "/r", nil)
	}
	zero := uint64(0)
	subscribe(t, d, "c1", protocol.SubscribeParams{After: &zero})

	sent := w.delivered()
	var createdAt = -1
	var lastReplayAt = -1
	for i, s := range sent {
		if s.Event.Kind == protocol.KindSubscriptionCreated {
			createdAt = i
		}
		if s.Event.Kind == "loop.started" {
			lastReplayAt = i
		}
	}
	if createdAt < 0 {
		t.Fatal("subscription.created was never delivered")
	}
	if lastReplayAt < 0 {
		t.Fatal("the cursor's replay was never delivered")
	}
	if lastReplayAt > createdAt {
		t.Fatalf("the replay arrived after subscription.created (%d > %d); a client would "+
			"treat its backlog as live traffic", lastReplayAt, createdAt)
	}
}

// Only this package may import the transport (T11's other half).
func TestOnlyTheDaemonPackageImportsTheTransport(t *testing.T) {
	// Asserted from the protocol side in internal/protocol/import_test.go; here
	// the complement: this package DOES import it, so the seam is real rather
	// than an accident of nothing importing anything.
	var _ = localrpc.Notify
}
