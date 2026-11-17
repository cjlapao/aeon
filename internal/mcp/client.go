package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// # Q1, answered here (the dependency tree was in front of me)
//
// **Hand-rolled, deliberately, with the cost stated both ways.**
//
// The two candidate SDKs, measured with `go mod download` + their own
// `go.mod` on 2026-10-24: `github.com/modelcontextprotocol/go-sdk v1.8.0`
// pulls **8 direct requires** (golang-jwt/v5, google/go-cmp,
// google/jsonschema-go, segmentio/encoding, yosida95/uritemplate/v3,
// golang.org/x/oauth2, x/time, x/tools) and `github.com/mark3labs/mcp-go`
// pulls **7** (jsonschema-go, uuid, rogpeppe/go-internal,
// santhosh-tekuri/jsonschema/v6, spf13/cast, stretchr/testify,
// uritemplate) onto a tree this repo pins at **393 modules**.
//
// The modules named are the argument: JWT and OAuth2 are the *server's*
// authorization machinery (this slice is a client only, N1); jsonschema and
// uritemplate serve resources and schema validation S12 does not do (M22:
// this layer never inspects schemas); testify is a test-framework swap.
// What this slice actually speaks is **four messages** — `initialize`,
// `notifications/initialized`, `tools/list`, `tools/call` — plus the
// JSON-RPC 2.0 envelope S2 already handles elsewhere in the product.
//
// The honest cost of the choice: **we own this file's framing forever**, and
// the spec is the only contract either side has — so the fake server
// (testdata/fakeserver) is built to the spec's letter and the tests pin the
// handshake. The honest cost of the SDK: eleven-plus new modules, four of
// them serving surfaces this slice explicitly excludes, and a breaking
// change upstream becomes our release blocker.
//
// # Nothing is discovered (MC3), read alongside this whole file
//
// Every client here is built from a `ServerConfig` that came from the
// configuration file. There is no scan, no probe, no auto-registration:
// `TestNothingIsDiscovered` greps this package for walk/listen/lookup
// machinery, and `Registry.ApplyListings` **refuses** a server the config
// never named — a server announcing itself is not a server configured.

// ProtocolVersion is the MCP revision this client speaks.
const ProtocolVersion = "2025-06-18"

// RemoteTool is one tool as its SERVER reports it. It is deliberately not
// Tool: what a server says about itself is untrusted data on its way to
// being classified by OUR configuration (Q4), and a type boundary is the
// honest place to say that.
type RemoteTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`

	// Annotations is the server's self-report — a HINT that may raise and
	// never lower our classes (T3/Q4), and never classify anything.
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// CallResult is a tools/call answer, flattened to what a step needs: the
// text content and the error flag. (W23's labelling of this text happens at
// the prompt boundary — T4 — not here, where the bytes are still just the
// wire.)
type CallResult struct {
	Text    string
	IsError bool
}

// ServerError is a JSON-RPC error answer — the server spoke and said no.
// It is a distinct type because "the server refused this call" and "the
// transport died" are different facts (three-state honesty); transport and
// context failures stay ordinary errors.
type ServerError struct {
	Code    int
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("mcp server error %d: %s", e.Code, e.Message)
}

// Client speaks MCP to exactly one configured server. The two transports
// are two mountings of the same adapter (N7): the methods below behave
// identically over stdio and HTTP, and the tests run the SAME assertions
// against both, because "two clients" is where transport parity goes to
// drift.
type Client struct {
	t         transport
	info      ServerInfo
	proto     string
	closeOnce sync.Once
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Dial builds a client for one configured server and performs the handshake
// (initialize → notifications/initialized). A server that does not complete
// the handshake is never a usable client — the half-open case must not
// reach the tool-list path.
//
// getenv resolves the env NAMES a stdio transport declares; values never
// come from the configuration file (C17). nil means os.Getenv.
func Dial(ctx context.Context, srv ServerConfig, getenv func(string) string) (*Client, error) {
	return DialWith(ctx, srv, getenv, DialOptions{})
}

// DialOptions are what a supervisor adds to a dial: somewhere for the server's
// stderr to go (the log it keeps), nothing else. The zero value is Dial's own
// behaviour — stderr discarded, exactly as before.
type DialOptions struct {
	// Stderr receives the server process's standard error. Nil discards it.
	Stderr io.Writer
}

// DialWith is Dial with options. The handshake rules are Dial's.
func DialWith(ctx context.Context, srv ServerConfig, getenv func(string) string, opts DialOptions) (*Client, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	var (
		t   transport
		err error
	)
	switch srv.Transport.Type {
	case TransportStdio:
		t, err = newStdioTransport(ctx, srv, getenv, opts)
	case TransportHTTP:
		t, err = newHTTPTransport(srv)
	default:
		return nil, fmt.Errorf("mcp: server %q: unknown transport type %q", srv.Name, srv.Transport.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("mcp: server %q: %w", srv.Name, err)
	}

	c := &Client{t: t}
	var initRes struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	if err := c.t.request(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aeon", "version": "s12"},
	}, &initRes); err != nil {
		t.close()
		return nil, fmt.Errorf("mcp: server %q: initialize: %w", srv.Name, err)
	}
	if initRes.ProtocolVersion == "" {
		t.close()
		return nil, fmt.Errorf("mcp: server %q: initialize answered no protocolVersion", srv.Name)
	}
	if err := c.t.notify("notifications/initialized", map[string]any{}); err != nil {
		t.close()
		return nil, fmt.Errorf("mcp: server %q: initialized: %w", srv.Name, err)
	}
	c.info, c.proto = initRes.ServerInfo, initRes.ProtocolVersion
	return c, nil
}

// PID is the server process's id, or 0 for a transport with no process of ours
// (http). It is the process the daemon owns and may terminate.
func (c *Client) PID() int {
	if t, ok := c.t.(*stdioTransport); ok && t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

// Exited is closed when the server process has ended and its output is drained:
// a crash, a kill, or a close. A transport with no process of ours never closes it.
func (c *Client) Exited() <-chan struct{} {
	if t, ok := c.t.(*stdioTransport); ok {
		return t.exited
	}
	return nil
}

// ExitError is how the process ended, once Exited is closed: nil for a clean
// exit, an *exec.ExitError (code, signal) otherwise.
func (c *Client) ExitError() error {
	if t, ok := c.t.(*stdioTransport); ok {
		select {
		case <-t.exited:
			return t.exitErr
		default:
		}
	}
	return nil
}

func (c *Client) ServerInfo() ServerInfo  { return c.info }
func (c *Client) ProtocolVersion() string { return c.proto }

// ListTools asks the server what it offers. NOTE what this is not: the
// answer alone makes nothing available. Existence is configuration (MC3,
// MC10) — this is the server's untrusted self-report, and it reaches a
// capability list only through Registry.ApplyListings, which annotates
// configured tools and refuses everything else.
func (c *Client) ListTools(ctx context.Context) ([]RemoteTool, error) {
	var res struct {
		Tools []RemoteTool `json:"tools"`
	}
	if err := c.t.request(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool invokes one tool by its UNQUALIFIED name — namespacing is the
// registry's (MC8) and this layer never sees qualified names, which keeps
// "who may call what" entirely above the transport (MC10).
//
// **No automatic retry, ever** (T8 states the rule): a retry doubles the
// wait and the model cannot tell one hang from two.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	if args == nil {
		args = json.RawMessage(`{}`)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.t.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": json.RawMessage(args),
	}, &res); err != nil {
		return CallResult{}, err
	}
	var texts []string
	for _, item := range res.Content {
		if item.Type == "text" || item.Type == "" {
			texts = append(texts, item.Text)
		}
	}
	return CallResult{Text: joinTexts(texts), IsError: res.IsError}, nil
}

func joinTexts(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "\n"
		}
		out += x
	}
	return out
}

// Close ends the session. stdio processes are killed, not trusted to exit:
// a third-party server that ignores stdin EOF must not outlive the step
// that spawned it (the daemon owns every process — P29).
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.t.close() })
	return err
}

// --- transports --------------------------------------------------------------

type transport interface {
	request(ctx context.Context, method string, params any, out any) error
	notify(method string, params any) error
	close() error
}

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// --- stdio -------------------------------------------------------------------

type stdioTransport struct {
	cmd  *cmdHandle
	wmu  sync.Mutex
	w    *bufio.Writer
	pmu  sync.Mutex
	next int64
	mu   sync.Mutex
	wait map[int64]chan *rpcEnvelope
	done chan struct{}
	once sync.Once

	// readDone closes when stdout is drained; exited closes when the process has
	// been reaped AFTER that (waiting earlier would close the pipe under a reader
	// still holding the server's last bytes). exitErr is valid once exited is closed.
	readDone chan struct{}
	exited   chan struct{}
	exitErr  error
}

type cmdHandle struct {
	*exec.Cmd
	stdin io.WriteCloser
}

func newStdioTransport(ctx context.Context, srv ServerConfig, getenv func(string) string, opts DialOptions) (*stdioTransport, error) {
	// Least privilege at the process boundary: the server gets PATH, HOME
	// and the variables its config NAMED — not the daemon's whole
	// environment. A docs server has no business reading unrelated
	// credentials out of the process environment it was spawned into.
	env := []string{}
	for _, key := range []string{"PATH", "HOME"} {
		if v := getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	for _, name := range srv.Transport.Env {
		if v := getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}

	// # `scrubbed-own`, named (S59 T5, EV10)
	//
	// This child gets an environment BUILT FROM THE RECORD and nothing else:
	// the server's declared env names, read through the injected `getenv`, plus
	// the fixed base. Nothing is inherited wholesale — an MCP server is a
	// third-party process launched by the daemon, and the set of variables it
	// may see is exactly the set a person declared for it (S46's record is the
	// authorising document, not the process environment). EV10 classifies this
	// path `scrubbed-own`, and T1's `TestMCPStdioEnvironmentGolden` pins the
	// bytes; T5 changed no behaviour here, only this paragraph.
	cmd := exec.Command(srv.Transport.Command, srv.Transport.Args...)
	cmd.Env = env
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr // a supervisor keeps the server's log
	} else {
		cmd.Stderr = nil // a server's stderr is T8's degradation report, not this layer's
	}
	// Its own process group: a terminate reaches the whole tree (an npx wrapper and
	// the node process under it), and a Ctrl-C at the daemon's terminal does not
	// reach servers the daemon is meant to stop in an orderly way.
	setProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawning %s: %w", srv.Transport.Command, err)
	}

	t := &stdioTransport{
		cmd:  &cmdHandle{Cmd: cmd, stdin: stdin},
		w:    bufio.NewWriter(stdin),
		wait: map[int64]chan *rpcEnvelope{},
		done: make(chan struct{}),

		readDone: make(chan struct{}),
		exited:   make(chan struct{}),
	}
	go t.readLoop(bufio.NewScanner(stdout))
	go func() {
		<-t.readDone
		t.exitErr = cmd.Wait()
		close(t.exited)
	}()
	return t, nil
}

func (t *stdioTransport) readLoop(in *bufio.Scanner) {
	defer close(t.readDone)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var env rpcEnvelope
		if err := json.Unmarshal(line, &env); err != nil || env.ID == nil {
			continue // unsolicited notifications: understood, unused (tool progress arrives and goes nowhere)
		}
		t.mu.Lock()
		ch, ok := t.wait[*env.ID]
		t.mu.Unlock()
		if ok {
			select {
			case ch <- &env:
			default:
			}
		}
	}
	// stdout closed: every waiter gets a nil so no request hangs forever
	// when the server dies mid-flight — the step continues, the absence is
	// a fact (T8), not a hang.
	t.mu.Lock()
	for id, ch := range t.wait {
		select {
		case ch <- nil:
		default:
		}
		delete(t.wait, id)
	}
	t.mu.Unlock()
}

func (t *stdioTransport) request(ctx context.Context, method string, params any, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := atomic.AddInt64(&t.next, 1)
	ch := make(chan *rpcEnvelope, 1)
	t.mu.Lock()
	t.wait[id] = ch
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.wait, id)
		t.mu.Unlock()
	}()

	env := rpcEnvelope{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	if err := t.write(&env); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err() // cancelled WITHOUT retry (T8): one attempt, reported
	case resp := <-ch:
		if resp == nil {
			return fmt.Errorf("mcp: server closed the connection during %s", method)
		}
		if resp.Error != nil {
			return &ServerError{Code: resp.Error.Code, Message: resp.Error.Message}
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (t *stdioTransport) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return t.write(&rpcEnvelope{JSONRPC: "2.0", Method: method, Params: raw})
}

func (t *stdioTransport) write(env *rpcEnvelope) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if _, err := t.w.Write(raw); err != nil {
		return err
	}
	if err := t.w.WriteByte('\n'); err != nil {
		return err
	}
	return t.w.Flush()
}

func (t *stdioTransport) close() error {
	var err error
	t.once.Do(func() {
		_ = t.cmd.stdin.Close()
		// Kill, then reap. A hung server (the hang tool is the test case for
		// exactly this) must not linger as a zombie the daemon owns forever. The
		// kill reaches the whole process group, so a wrapper's child dies with it.
		if t.cmd.Process != nil {
			killProcessGroup(t.cmd.Process)
		}
		select {
		case <-t.exited: // reaped by the watcher; a killed process exits non-zero, which is expected
		case <-time.After(2 * time.Second):
			err = errors.New("mcp: stdio server did not exit after kill")
		}
		close(t.done)
	})
	return err
}

// --- http --------------------------------------------------------------------

type httpTransport struct {
	url  string
	hc   *http.Client
	next atomic.Int64
}

func newHTTPTransport(srv ServerConfig) (*httpTransport, error) {
	return &httpTransport{
		url: srv.Transport.URL,
		// No redirect following: a configured URL answers, or the server is
		// absent (T8). Redirects are a second server showing up that the
		// configuration never named — discovery by another name.
		hc: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}, nil
}

func (t *httpTransport) request(ctx context.Context, method string, params any, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	// A request carries an id; a notification does not — the fake (like any
	// spec-following server) answers one and swallows the other, and the
	// first draft of this transport forgot the id and "hung on EOF".
	id := t.next.Add(1)
	env := rpcEnvelope{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		return err // ordinary error: unreachable server, T8's degradation input
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp: http server answered %d", resp.StatusCode)
	}
	var got rpcEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return fmt.Errorf("mcp: http server answered unparseable JSON: %w", err)
	}
	if got.Error != nil {
		return &ServerError{Code: got.Error.Code, Message: got.Error.Message}
	}
	return json.Unmarshal(got.Result, out)
}

func (t *httpTransport) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	env := rpcEnvelope{JSONRPC: "2.0", Method: method, Params: raw}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (t *httpTransport) close() error { return nil }
