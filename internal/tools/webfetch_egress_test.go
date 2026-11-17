package tools

// T16's egress tests: the fake transport, the recorded wire, and the claim
// that no credential header ever leaves this process.
//
// This half is about the WIRE rather than the tool: `fixtureTransport` replays
// testdata/webfetch/ and records what arrived, and the tests here read that
// record instead of trusting the tool's own sentences. The credential sweep and
// the guard's own test live beside it because they are assertions about bytes
// on the wire, made next to the code that makes them possible.
//
// Nothing in either half of T16's tests dials — see
// TestWebfetchTestsUseTheFakeTransportOnly.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// the fixture transport
// ---------------------------------------------------------------------------

type fixtureExchange struct {
	Request struct {
		Method string      `json:"method"`
		URL    string      `json:"url"`
		Header http.Header `json:"header"`
	} `json:"request"`
	Response struct {
		Status     int         `json:"status"`
		Header     http.Header `json:"header"`
		Body       string      `json:"body"`
		BodyRepeat *struct {
			Text  string `json:"text"`
			Times int    `json:"times"`
		} `json:"body_repeat"`
		Chunked    bool `json:"chunked"`
		ContentLen *int `json:"content_length"`
		AbortAfter *int `json:"abort_after"`
	} `json:"response"`
}

type fixtureFile struct {
	Description string            `json:"description"`
	Exchanges   []fixtureExchange `json:"exchanges"`
}

type recordedRequest struct {
	method string
	url    string
	header http.Header
	host   string
}

// fixtureTransport replays one recording and records the wire.
//
// Matching is by URL (not consume-once) so a redirect loop can re-read its
// single exchange; an unmatched request is a hard failure AND a 599, never a
// plausible-looking fallback.

type fixtureTransport struct {
	t         *testing.T
	exchanges []fixtureExchange
	recorded  []recordedRequest
	unmatched []string
}

func loadFixture(t *testing.T, name string) *fixtureTransport {
	t.Helper()
	raw, err := os.ReadFile("testdata/webfetch/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var f fixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	if len(f.Exchanges) == 0 {
		t.Fatalf("fixture %s has no exchanges — a check over nothing passes forever", name)
	}
	return &fixtureTransport{t: t, exchanges: f.Exchanges}
}

func (f *fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	got := recordedRequest{
		method: req.Method,
		url:    req.URL.String(),
		header: req.Header.Clone(),
		host:   req.URL.Host,
	}
	f.recorded = append(f.recorded, got)

	for _, ex := range f.exchanges {
		if ex.Request.URL != req.URL.String() {
			continue
		}
		if ex.Request.Method != req.Method {
			f.t.Errorf("fixture %s: %s asked for %s, the recording has %s",
				f.describe(), req.Method, req.URL, ex.Request.Method)
			continue
		}
		// The recorded capture IS the assertion: same key set, same values.
		if !headerSetsEqual(ex.Request.Header, req.Header) {
			f.t.Errorf("fixture %s: the wire headers for %s are %v, want exactly %v",
				f.describe(), req.URL, headerNames(req.Header), headerNames(ex.Request.Header))
		}
		return f.response(ex, req), nil
	}
	f.unmatched = append(f.unmatched, req.URL.String())
	return &http.Response{
		StatusCode: 599,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("no fixture for this URL")),
		Request:    req,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}, nil
}

func (f *fixtureTransport) describe() string {
	if len(f.exchanges) > 0 {
		return f.exchanges[0].Request.URL
	}
	return "(empty)"
}

func (f *fixtureTransport) response(ex fixtureExchange, req *http.Request) *http.Response {
	body := ex.Response.Body
	if ex.Response.BodyRepeat != nil {
		body = strings.Repeat(ex.Response.BodyRepeat.Text, ex.Response.BodyRepeat.Times)
	}
	// Every branch of the switch assigns it, so no initializer is needed (and
	// one would be a value nobody reads).
	var declared int64
	switch {
	case ex.Response.ContentLen != nil:
		declared = int64(*ex.Response.ContentLen)
	case ex.Response.Chunked:
		declared = -1
	default:
		declared = int64(len(body))
	}
	var reader io.Reader = strings.NewReader(body)
	if ex.Response.AbortAfter != nil {
		reader = &errorReader{remaining: *ex.Response.AbortAfter, data: body}
	}
	return &http.Response{
		StatusCode:    ex.Response.Status,
		Header:        ex.Response.Header.Clone(),
		Body:          io.NopCloser(reader),
		ContentLength: declared,
		Request:       req,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
	}
}

// errorReader delivers the first n bytes and then fails: the stream that never
// finishes, which is the only way to reach the honest "at least" state without
// dialing a server that drops a connection.

type errorReader struct {
	remaining int
	data      string
	read      int
}

func (e *errorReader) Read(p []byte) (int, error) {
	if e.read >= e.remaining {
		return 0, fmt.Errorf("fixture: the connection was dropped mid-body")
	}
	if e.read >= len(e.data) {
		return 0, io.EOF
	}
	n := copy(p, e.data[e.read:])
	if e.read+n > e.remaining {
		n = e.remaining - e.read
	}
	e.read += n
	return n, nil
}

// preview keeps a failing assertion from printing two megabytes of body.
func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf(" … (%d bytes total)", len(s))
}

func headerNames(h http.Header) []string {
	var out []string
	for k := range h {
		out = append(out, k)
	}
	return out
}

func headerSetsEqual(want, got http.Header) bool {
	if len(want) != len(got) {
		return false
	}
	for k, vv := range want {
		gg := got.Values(k)
		if len(gg) != len(vv) {
			return false
		}
		for i := range vv {
			if gg[i] != vv[i] {
				return false
			}
		}
	}
	return true
}

func fetch(t *testing.T, rt http.RoundTripper, input string) Result {
	t.Helper()
	return webfetchWithTransport(rt).Run(context.Background(), ToolContext{}, json.RawMessage(input))
}

func fetchURL(t *testing.T, rt http.RoundTripper, u string) Result {
	t.Helper()
	return fetch(t, rt, fmt.Sprintf(`{"url":%q}`, u))
}

// ---------------------------------------------------------------------------
// behaviour: the happy path and the rendering
// ---------------------------------------------------------------------------

func urlsOf(rs []recordedRequest) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.method+" "+r.url)
	}
	return out
}

func TestWebfetchNeverSendsCredentialHeaders(t *testing.T) {
	credentialHeaders := []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "X-Api-Key", "X-Auth-Token",
		"Www-Authenticate", "Set-Cookie", "From",
	}

	entries, err := os.ReadDir("testdata/webfetch")
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			rt := loadFixture(t, e.Name())
			for _, ex := range rt.exchanges {
				fetchURL(t, rt, ex.Request.URL)
				ran++
			}
			if len(rt.recorded) == 0 {
				t.Fatalf("%s produced no requests on the wire — the assertions below would pass "+
					"on silence, which is the failure mode this sweep exists to avoid", e.Name())
			}
			for _, r := range rt.recorded {
				for _, forbidden := range credentialHeaders {
					if len(r.header.Values(forbidden)) > 0 {
						t.Errorf("%s: %s %s carried %s: %v", e.Name(), r.method, r.url, forbidden,
							r.header.Values(forbidden))
					}
				}
				for _, name := range headerNames(r.header) {
					if name != "Accept" && name != "User-Agent" {
						t.Errorf("%s: %s carried %q, which is not on the closed egress list",
							e.Name(), r.url, name)
					}
				}
				if r.method != "GET" {
					t.Errorf("%s: %s reached the transport with method %q", e.Name(), r.url, r.method)
				}
			}
		})
	}
	if ran < 12 {
		t.Fatalf("only %d fixtures ran — the sweep is checking almost nothing", ran)
	}
}

// TestNoCredentialHeaderEverSent is the other half of the claim, and the half
// that makes the first one worth reading: an assertion that nothing was ever
// set passes forever on code that merely never sets it. So the guard is FED a
// request that already carries credentials, and what reaches the transport is
// read back.

func TestNoCredentialHeaderEverSent(t *testing.T) {
	credentialHeaders := []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Cookie2", "X-Api-Key", "X-Auth-Token",
		"Www-Authenticate", "Set-Cookie", "From",
	}

	var seen *http.Request
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})
	guard := egressGuard{next: inner}

	// A request carrying every credential this file forbids. The guard's
	// answer must be an empty header set bar the two it builds itself.
	req, err := http.NewRequest(http.MethodGet, "http://docs.test/guide", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("Cookie", "session=hunter2")
	req.Header.Set("Proxy-Authorization", "Basic dXNlcjpwYXNz")
	req.Header.Set("X-Api-Key", "sk-super-secret")
	req.Header.Set("Referer", "http://everywhere.test/leak")
	res, err := guard.RoundTrip(req)
	if err != nil {
		t.Fatalf("the guard should not refuse a GET: %v", err)
	}
	_ = res.Body.Close()
	if seen == nil {
		t.Fatal("the inner transport never saw the request")
	}
	for _, forbidden := range credentialHeaders {
		if len(seen.Header.Values(forbidden)) > 0 {
			t.Errorf("the guard forwarded %s to the transport", forbidden)
		}
	}
	if names := headerNames(seen.Header); len(names) != 2 {
		t.Errorf("the wire set is %v, want exactly Accept and User-Agent", names)
	}
	if seen.Header.Get("User-Agent") != WebfetchUserAgent {
		t.Errorf("the stated identity is %q", seen.Header.Get("User-Agent"))
	}
	if seen.Header.Get("Accept") != WebfetchAccept {
		t.Errorf("the stated Accept is %q", seen.Header.Get("Accept"))
	}

	// Layer 2 is refusal-shaped for a verb, not tidy-up-shaped.
	post, _ := http.NewRequest(http.MethodPost, "http://docs.test/guide", strings.NewReader("payload"))
	if _, err := guard.RoundTrip(post); err == nil {
		t.Error("a POST reached the transport")
	} else if !strings.Contains(err.Error(), "only GET") {
		t.Errorf("the refusal does not say what it refused: %v", err)
	}

	// Nor does userinfo, even when it arrives past the argument layer.
	cred, _ := http.NewRequest(http.MethodGet, "http://user:***@docs.test/guide", nil)
	if _, err := guard.RoundTrip(cred); err == nil {
		t.Error("a credential-bearing URL reached the transport")
	}
}

func TestWebfetchTransportIsNotTheDefaultOne(t *testing.T) {
	tr := newWebfetchTransport()
	if tr.Proxy != nil {
		t.Error("Proxy must be nil: an environment proxy may carry user:pass and produce a " +
			"Proxy-Authorization header from ambient state")
	}
	if !tr.DisableCompression {
		t.Error("compression must stay off, or the transport adds an Accept-Encoding the tests do not assert")
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("certificate verification is never off")
	}
	if tr.DialContext == nil {
		t.Error("a dialer with a connect timeout is required, or a dead host costs Client.Timeout per attempt")
	}
	// The client built over it carries no jar (net/http then has nowhere to
	// store or restore a Cookie from) and this file's redirect policy.
	c := newWebfetchClient(tr, &redirectTrace{}, WebfetchTimeout)
	if c.Jar != nil {
		t.Error("a cookie jar would let net/http store and re-send a Cookie")
	}
	if c.CheckRedirect == nil {
		t.Error("the redirect policy is this file's, never the library default")
	}
	if c.Timeout != WebfetchTimeout {
		t.Errorf("the client timeout is %s, want %s", c.Timeout, WebfetchTimeout)
	}
}

func TestWebfetchNilTransportIsAConstructionPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a nil transport must panic at construction, where the stack names the caller")
		}
	}()
	webfetchWithTransport(nil)
}

func TestWebfetchHonoursCancellation(t *testing.T) {
	rt := loadFixture(t, "plain-text.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := webfetchWithTransport(rt).Run(ctx, ToolContext{}, json.RawMessage(`{"url":"http://docs.test/guide"}`))
	if !got.IsError || !strings.Contains(got.Content, "cancelled") {
		t.Errorf("a cancelled call should not start a fetch: %+v", got)
	}
	if len(rt.recorded) != 0 {
		t.Errorf("a cancelled step still dialed: %v", urlsOf(rt.recorded))
	}

	// A cancellation that lands DURING the read is a different fact from a
	// timeout, and the two must not read the same (spawn.go's split).
	slow := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	during, cancelDuring := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelDuring()
	got = webfetchWithTransport(slow).Run(during, ToolContext{}, json.RawMessage(`{"url":"http://docs.test/slow"}`))
	if !got.IsError {
		t.Fatal("a fetch that never finished reported success")
	}
	if !strings.Contains(got.Content, "cancelled") {
		t.Errorf("the caller's cancellation must not read as a timeout: %q", got.Content)
	}

	// And an infrastructure failure is reported as one, in the third voice:
	// nothing was refused, the host simply did not answer as a host.
	broken := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})
	got = fetchURL(t, broken, "http://docs.test/down")
	if !got.IsError || !strings.Contains(got.Content, "could not fetch") ||
		!strings.Contains(got.Content, "connection refused") {
		t.Errorf("a socket failure must name the failure: %+v", got)
	}
	if strings.Contains(got.Content, "refused: webfetch") {
		t.Errorf("an infrastructure failure must not be worded as a policy refusal: %q", got.Content)
	}
}

// TestWebfetchEchoesTheURLItActuallyFetched pins a behaviour found by trying
// it, not by assuming it: url.Parse REMOVES ASCII control characters rather
// than refusing (Go does this for WHATWG compatibility), so a URL with a tab
// embedded fetches the stripped form.
//
// That is only acceptable because the result names the stripped URL — a
// transcript that echoed the caller's spelling while the wire carried a
// different one would be a silent difference between what was asked for and
// what was fetched, which is the class of lie this tool cannot afford.

func TestWebfetchCancellationMidReadIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reqCtx := req.Context()
		// The response is handed back IMMEDIATELY and the body is what stalls:
		// blocking inside RoundTrip instead would race the client's own
		// cancellation and could land in "cancelled while fetching" instead,
		// which is the branch the test above already pins.
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body: io.NopCloser(readerFunc(func([]byte) (int, error) {
				<-reqCtx.Done()
				return 0, reqCtx.Err()
			})),
			Request: req,
		}, nil
	})
	got := webfetchWithTransport(rt).Run(ctx, ToolContext{}, json.RawMessage(`{"url":"http://docs.test/stalled"}`))
	if !got.IsError {
		t.Fatal("a body that never arrived reported success")
	}
	if !strings.Contains(got.Content, "cancelled while reading") {
		t.Errorf("the answer must say the read was cut short and by whom: %q", got.Content)
	}
	if !strings.Contains(got.Content, "http://docs.test/stalled") {
		t.Errorf("the refusal must name the URL: %q", got.Content)
	}
}

func TestWebfetchTimeoutIsNotCancellation(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: context.DeadlineExceeded}
	})
	got := fetchURL(t, rt, "http://docs.test/dead")
	if !got.IsError {
		t.Fatal("a timeout reported success")
	}
	if !strings.Contains(got.Content, "timed out") || strings.Contains(got.Content, "cancelled") {
		t.Errorf("a dead host must read as a timeout, not as the caller leaving: %q", got.Content)
	}
	if !strings.Contains(got.Content, WebfetchTimeout.String()) {
		t.Errorf("the answer must state the bound it hit (%s): %q", WebfetchTimeout, got.Content)
	}
}

// TestWebfetchMalformedURLArguments covers the two decode/parse refusals that
// the argument tables do not reach: a `url` of the wrong JSON type, and a
// string that is not a URL at all.

func TestReadCappedBodyStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	kept, observed, exact := readCappedBody(ctx, strings.NewReader("nothing was read"), -1)
	if exact {
		t.Error("a cancelled read may never claim a finished count")
	}
	if len(kept) != 0 || observed != 0 {
		t.Errorf("a cancelled read still consumed %d bytes", observed)
	}
	// And the sentence that goes with it states the lower bound, not a cap.
	if _, note := renderWebfetchBody(kept, observed, exact); note == "" {
		t.Error("a partial read with nothing kept must still say so")
	}
}

// TestWebfetchCancellationMidReadIsNotATimeout covers the third timing case:
// the headers arrived, the body did not, and it is the CALLER who went away.

func TestWebfetchTestsUseTheFakeTransportOnly(t *testing.T) {
	// Both halves: the ban is on the tests, and the tests live in two files.
	var both strings.Builder
	for _, file := range []string{"webfetch_test.go", "webfetch_egress_test.go"} {
		self, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(self)
		both.WriteString(src)
		for _, forbidden := range []string{
			dotted("http", "DefaultTransport"),
			dotted("http", "DefaultClient"),
			dotted("http", "Get("),
			dotted("http", "Post("),
			dotted("net", "Dial"),
			dotted("httptest", "NewServer"),
			dotted("httptest", "NewRecorder"),
		} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s uses %q — the fake transport is the only transport here", file, forbidden)
			}
		}
		// The imports carry the same ban: no test-only HTTP server, and no
		// bare networking package either (nothing here may open a socket).
		for _, imp := range []string{quoted(dotted("net", "http", "httptest")), quoted("net")} {
			if strings.Contains(src, "\n\t"+imp+"\n") {
				t.Errorf("%s imports %s", file, imp)
			}
		}
	}
	if !strings.Contains(both.String(), "webfetchWithTransport(") {
		t.Error("the tests do not go through the fixture seam at all")
	}
	// The seam stays unexported: the registry gets the real transport, and an
	// injectable one is this package's tests' privilege alone.
	if strings.Contains(packageSources(t)["webfetch.go"], "func WebfetchWithTransport") {
		t.Error("the transport seam became exported; a caller outside this package could then choose it")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// TestWebfetchTimeoutIsNotCancellation covers the client's OWN deadline: the
// caller is still there, the host simply never answered.
//
// It SIMULATES the error net/http returns when Client.Timeout fires (a
// DeadlineExceeded arriving while the caller's context is untouched) rather
// than sleeping for WebfetchTimeout: the branch under test is the one that
// distinguishes the two sources of a deadline, and waiting 30 s for it would
// make the package's suite ten times slower to prove nothing new.

func dotted(parts ...string) string { return strings.Join(parts, ".") }

func quoted(s string) string { return `"` + s + `"` }

// TestWebfetchIsRegisteredButNotDefaultGranted checks the two facts that must
// never be confused: the tool exists, and no step may call it by default.
