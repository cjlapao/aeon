package protocol

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipe is a bidirectional in-memory stream built from two io.Pipes.
//
// io rather than net.Pipe, because this package may not import `net` (T11) —
// and the constraint turns out to be free: a client that takes a stream is a
// client that can be tested without a socket.
type pipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipe) Close() error                { p.w.Close(); return p.r.Close() }

// newPipePair returns two ends of one stream.
func newPipePair() (client, server pipe) {
	cr, sw := io.Pipe() // server writes, client reads
	sr, cw := io.Pipe() // client writes, server reads
	return pipe{r: cr, w: cw}, pipe{r: sr, w: sw}
}

// fakeDaemon records every frame it receives, in order, and answers scripted
// results. It exists so the handshake can be asserted ON THE WIRE.
type fakeDaemon struct {
	mu       sync.Mutex
	received []string // method names, in arrival order
	stream   pipe
	enc      *json.Encoder

	// results maps a method to the raw result JSON to answer with.
	results map[string]string
}

func newFakeDaemon(t *testing.T, server pipe, results map[string]string) *fakeDaemon {
	f := &fakeDaemon{stream: server, enc: json.NewEncoder(server), results: results}
	go f.serve(t)
	return f
}

func (f *fakeDaemon) serve(t *testing.T) {
	dec := json.NewDecoder(f.stream)
	for {
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		f.mu.Lock()
		f.received = append(f.received, req.Method)
		raw, ok := f.results[req.Method]
		f.mu.Unlock()
		if !ok {
			raw = "{}"
		}
		line := `{"jsonrpc":"2.0","id":` + itoa(*req.ID) + `,"result":` + raw + "}\n"
		if _, err := f.stream.Write([]byte(line)); err != nil {
			return
		}
	}
}

func (f *fakeDaemon) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.received))
	copy(out, f.received)
	return out
}

func (f *fakeDaemon) sendEvent(t *testing.T, ev Event) {
	t.Helper()
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.stream.Write([]byte(`{"jsonrpc":"2.0","method":"event","params":` + string(raw) + "}\n")); err != nil {
		t.Fatal(err)
	}
}

const okInitialize = `{"protocolVersion":1,"daemonVersion":"9.9.9-fake","capabilities":["subscribe"]}`

// T6 · T7 — THE test: initialize is the first frame on the wire.
//
// Asserted on the wire rather than by reading the code, because a client
// library that let a caller skip the handshake would teach every user of it the
// wrong shape — and reading the code proves nothing about what was sent.
func TestTheClientPerformsInitializeBeforeAnyOtherCall(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	fake := newFakeDaemon(t, serverEnd, map[string]string{"initialize": okInitialize})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	if _, err := c.Connect(1); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(); err != nil {
		t.Fatal(err)
	}

	got := fake.methods()
	if len(got) < 2 {
		t.Fatalf("the daemon saw %v, want initialize then ping", got)
	}
	if got[0] != "initialize" {
		t.Fatalf("the first frame on the wire was %q, want initialize (T7)", got[0])
	}
	if got[1] != "ping" {
		t.Fatalf("the second frame was %q, want ping", got[1])
	}
}

// The client refuses on its own side too, so a caller cannot send a method
// before the handshake even if the daemon would have answered.
func TestTheClientRefusesACallBeforeConnect(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	newFakeDaemon(t, serverEnd, map[string]string{"initialize": okInitialize})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	err := c.Ping()
	if err == nil {
		t.Fatal("the client sent ping before Connect")
	}
	if !strings.Contains(err.Error(), "initialize") {
		t.Errorf("the refusal does not mention initialize: %v", err)
	}
}

// Before the handshake the client reports version 0, not PROTOCOL_VERSION.
// Reporting the constant would make `aeon version`'s "negotiated" label a lie.
func TestTheNegotiatedVersionIsZeroBeforeConnect(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	newFakeDaemon(t, serverEnd, map[string]string{"initialize": okInitialize})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	if got := c.NegotiatedVersion(); got != 0 {
		t.Fatalf("NegotiatedVersion = %d before Connect, want 0 — anything else is a constant", got)
	}
}

// T3's outbound half: the client accepts a result carrying a field it has never
// heard of. A client that rejects one turns every additive change into a
// breaking one.
func TestTheClientAcceptsAResultWithAnUnknownField(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	newFakeDaemon(t, serverEnd, map[string]string{
		"initialize": `{"protocolVersion":1,"daemonVersion":"9.9.9-fake","capabilities":["subscribe"],` +
			`"featureFromTheFuture":{"nested":true}}`,
	})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	res, err := c.Connect(1)
	if err != nil {
		t.Fatalf("a result with an unknown field was rejected: %v", err)
	}
	if res.ProtocolVersion != 1 || res.DaemonVersion != "9.9.9-fake" {
		t.Fatalf("the known fields did not survive: %+v", res)
	}
}

// The version the client reports must come from the WIRE. A fake answering a
// version the code could not have guessed is the only way to tell the
// difference between negotiating and printing a constant.
func TestTheClientReportsTheVersionTheDaemonSentNotAConstant(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	newFakeDaemon(t, serverEnd, map[string]string{
		// 7 is not PROTOCOL_VERSION and never will be by accident.
		"initialize": `{"protocolVersion":7,"daemonVersion":"from-the-wire","capabilities":[]}`,
	})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	if _, err := c.Connect(0); err != nil {
		t.Fatal(err)
	}
	if got := c.NegotiatedVersion(); got != 7 {
		t.Fatalf("NegotiatedVersion = %d, want the 7 the daemon sent — the client is "+
			"reporting its own constant", got)
	}
	if got := c.DaemonVersion(); got != "from-the-wire" {
		t.Fatalf("DaemonVersion = %q, want the wire's", got)
	}
}

// An error response becomes an *Error with all three parts intact, so the CLI
// can render what happened, what it means and what to do.
func TestAnErrorResponseArrivesAsAThreePartError(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	go func() {
		dec := json.NewDecoder(serverEnd)
		var req struct {
			ID int `json:"id"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		serverEnd.Write([]byte(`{"jsonrpc":"2.0","id":` + itoa(req.ID) + `,"error":{"code":-31002,` +
			`"message":"protocol version not supported","data":{"what":"client needs 99; daemon speaks 1",` +
			`"means":"incompatible releases","todo":"upgrade the older side","fatal":true}}}` + "\n"))
	}()
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	_, err := c.Connect(99)
	if err == nil {
		t.Fatal("a refusal was not surfaced")
	}
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("want *protocol.Error, got %T: %v", err, err)
	}
	if e.Code != -31002 {
		t.Errorf("code = %d", e.Code)
	}
	for name, part := range map[string]string{"what": e.Data.What, "means": e.Data.Means, "todo": e.Data.Todo} {
		if part == "" {
			t.Errorf("the %s part was lost in transit", name)
		}
	}
	if !e.Data.Fatal {
		t.Error("the fatal marker was lost")
	}
}

// Notifications are demultiplexed from responses by the absence of an id —
// JSON-RPC's rule, not Aeon's.
func TestEventsArriveOnTheEventChannelAndNotAsResponses(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	fake := newFakeDaemon(t, serverEnd, map[string]string{"initialize": okInitialize})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	if _, err := c.Connect(1); err != nil {
		t.Fatal(err)
	}
	fake.sendEvent(t, Event{Seq: 42, Kind: "client.connected", SubscriptionID: "s-1", At: "now"})

	select {
	case ev := <-c.Events():
		if ev.Seq != 42 || ev.Kind != "client.connected" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the event never arrived on the event channel")
	}

	// And the request path still works afterwards, which is what proves the
	// notification did not consume a pending response.
	if err := c.Ping(); err != nil {
		t.Fatalf("a request after an event failed: %v", err)
	}
}

// A frame the client cannot parse does not tear down a working connection —
// the same courtesy O6 requires of the daemon.
func TestAMalformedFrameFromTheDaemonDoesNotKillTheClient(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	fake := newFakeDaemon(t, serverEnd, map[string]string{"initialize": okInitialize})
	c := NewClient(clientEnd, "test", "0.1.0")
	defer c.Close()

	if _, err := c.Connect(1); err != nil {
		t.Fatal(err)
	}
	if _, err := serverEnd.Write([]byte("this is not json\n")); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(); err != nil {
		t.Fatalf("the client died on a malformed frame: %v", err)
	}
	if len(fake.methods()) != 2 {
		t.Errorf("the daemon saw %v", fake.methods())
	}
}

// A closed connection surfaces as an error on the pending call rather than a
// hang, which is what a CLI needs in order to say "the daemon went away".
func TestAClosedConnectionFailsThePendingCallRatherThanHanging(t *testing.T) {
	clientEnd, serverEnd := newPipePair()
	c := NewClient(clientEnd, "test", "0.1.0")

	go func() {
		time.Sleep(50 * time.Millisecond)
		serverEnd.Close()
	}()

	done := make(chan error, 1)
	go func() { _, err := c.Connect(1); done <- err }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect succeeded against a closed connection")
		}
		if !strings.Contains(err.Error(), "closed") {
			t.Errorf("the error does not say the connection closed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Connect hung on a closed connection")
	}
}
