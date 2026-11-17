package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// # The client (T6)
//
// It goes through the same types and honours the same handshake rule as any
// third party would. **A CLI that shortcut `initialize` because it knows the
// daemon would prove nothing about the protocol**, which is the whole reason
// this library exists rather than a few inline `json.Marshal` calls.
//
// # Why it takes an io.ReadWriteCloser rather than dialing
//
// T6 puts this file in `internal/protocol`; T11 and I1 forbid this package from
// importing anything transport-related. Taken literally both cannot hold — a
// client that dials needs `net`.
//
// The resolution, which is the spirit of T11 rather than a workaround: the
// SEQUENCING is the protocol's (handshake first, correlate by id, demultiplex
// notifications) and the CONNECTING is the transport's. So this takes an
// already-open stream, and `cmd/aeon/dial.go` — thirty lines that import `net`
// — opens it. Two things follow that are worth more than the tidiness:
//
//  1. S19's HTTP and stdio mounts reuse this client unchanged, which is the
//     promise T11 was made for.
//  2. Every client test runs over an in-memory pipe, so "the client performs
//     initialize before any other call" is asserted ON THE WIRE rather than by
//     reading the code.
//
// Recorded as a deviation in T6's notes rather than resolved silently.

// Client speaks the protocol over one stream.
type Client struct {
	rw  io.ReadWriteCloser
	br  *bufio.Reader
	enc *json.Encoder

	name    string
	version string

	mu         sync.Mutex
	nextID     int
	negotiated int
	daemonVer  string
	caps       []string
	ready      bool

	// pending correlates responses to requests by id. One outstanding request
	// at a time would be simpler and would make `watch` impossible: it holds a
	// subscription open while still being able to call `unsubscribe`.
	pending map[int]chan *rpcResponse

	events    chan Event
	closed    chan struct{}
	closeOnce sync.Once
	readErr   error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
}

// EventBuffer is how many events the client queues before dropping.
//
// A guess, like the daemon's bounds. A client that stops reading its own events
// is a client with a bug; dropping is better than blocking the read loop, which
// would also stall every pending response.
const EventBuffer = 256

// NewClient wraps an open stream. It does NOT handshake — Connect does, so the
// handshake is an observable act rather than a side effect of construction.
func NewClient(rw io.ReadWriteCloser, clientName, clientVersion string) *Client {
	c := &Client{
		rw:      rw,
		br:      bufio.NewReader(rw),
		enc:     json.NewEncoder(rw),
		name:    clientName,
		version: clientVersion,
		pending: map[int]chan *rpcResponse{},
		events:  make(chan Event, EventBuffer),
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// Connect performs `initialize` and records the negotiated version.
//
// `want` is the version the client NEEDS; 0 means "whatever the daemon
// supports", which is what a client with no opinion should send.
func (c *Client) Connect(want int) (InitializeResult, error) {
	var res InitializeResult
	err := c.call(MethodInitialize, InitializeParams{
		ClientName:      c.name,
		ClientVersion:   c.version,
		ProtocolVersion: want,
	}, &res)
	if err != nil {
		return res, err
	}

	c.mu.Lock()
	c.negotiated = res.ProtocolVersion
	c.daemonVer = res.DaemonVersion
	c.caps = res.Capabilities
	c.ready = true
	c.mu.Unlock()
	return res, nil
}

// Call invokes a method. It refuses before Connect (T7), on the client side as
// well as the daemon's — a client library that let a caller skip the handshake
// would teach every user of it the wrong shape.
func (c *Client) Call(method MethodName, params, result any) error {
	c.mu.Lock()
	ready := c.ready
	c.mu.Unlock()
	if !ready && method != MethodInitialize {
		return fmt.Errorf("protocol: %s called before Connect; `initialize` is mandatory and first (T7)", method)
	}
	return c.call(method, params, result)
}

func (c *Client) call(method MethodName, params, result any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.enc.Encode(rpcRequest{JSONRPC: "2.0", ID: &id, Method: string(method), Params: params}); err != nil {
		return fmt.Errorf("protocol: send %s: %w", method, err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("protocol: decode %s result: %w", method, err)
			}
		}
		return nil
	case <-c.closed:
		if c.readErr != nil {
			return fmt.Errorf("protocol: connection closed while awaiting %s: %w", method, c.readErr)
		}
		return fmt.Errorf("protocol: connection closed while awaiting %s", method)
	}
}

// Subscribe registers a subscription. Events arrive on Events().
func (c *Client) Subscribe(filter Filter, after *uint64) (SubscribeResult, error) {
	var res SubscribeResult
	err := c.Call(MethodSubscribe, SubscribeParams{Filter: filter, After: after}, &res)
	return res, err
}

func (c *Client) Unsubscribe(id string) error {
	return c.Call(MethodUnsubscribe, UnsubscribeParams{SubscriptionID: id}, &UnsubscribeResult{})
}

func (c *Client) Ping() error { return c.Call(MethodPing, PingParams{}, &PingResult{}) }

func (c *Client) Version() (VersionResult, error) {
	var res VersionResult
	err := c.Call(MethodVersion, VersionParams{}, &res)
	return res, err
}

// Events is the notification stream.
func (c *Client) Events() <-chan Event { return c.events }

// NegotiatedVersion is what the daemon agreed to speak, or 0 before Connect.
//
// Zero before the handshake ON PURPOSE: a client that reports a version it has
// not negotiated is reporting a constant, and `aeon version` would print
// "negotiated 1" having never asked.
func (c *Client) NegotiatedVersion() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiated
}

func (c *Client) DaemonVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.daemonVer
}

func (c *Client) Capabilities() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps
}

// Closed is closed when the read loop ends, so a caller can wait on the
// connection rather than poll it.
func (c *Client) Closed() <-chan struct{} { return c.closed }

func (c *Client) Close() error {
	err := c.rw.Close()
	c.closeOnce.Do(func() { close(c.closed) })
	return err
}

// readLoop demultiplexes responses and notifications.
//
// A notification has no `id`; a response has one. That is the whole rule, and
// it is JSON-RPC's rather than Aeon's.
func (c *Client) readLoop() {
	defer c.closeOnce.Do(func() { close(c.closed) })

	for {
		line, err := c.br.ReadBytes('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.readErr = err
			}
			return
		}
	}
}

func (c *Client) handleLine(line []byte) {
	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		// A frame the client cannot parse is the daemon's problem, not a reason
		// to tear down a working connection — the same courtesy O6 requires of
		// the daemon.
		return
	}

	if resp.ID == nil {
		if resp.Method == "event" {
			var ev Event
			if err := json.Unmarshal(resp.Params, &ev); err == nil {
				select {
				case c.events <- ev:
				default:
					// Dropped rather than blocking the read loop, which would
					// also stall every pending response.
				}
			}
		}
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[*resp.ID]
	c.mu.Unlock()
	if ok {
		r := resp
		ch <- &r
	}
}
