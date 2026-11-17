package narrow

// errors_test.go — T7's discrimination gates. The distinction that makes
// the taxonomy worth having: a caller polling a loop must be able to tell
// "your key is wrong" from "that loop does not exist" from "the daemon is
// restarting" — and act differently on each. Merging refused with
// not-found would erase T5's absence (a 404 that is not a locked door);
// merging either with transport would tell a caller to retry a mistake.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// errCaller answers with a canned error per method, or a canned payload.
type errCaller struct {
	mu      sync.Mutex
	err     error
	payload any
	calls   int
}

func (c *errCaller) Call(method protocol.MethodName, params, result any) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if p, ok := result.(*any); ok {
		*p = c.payload
	}
	return nil
}

func (c *errCaller) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func get(t *testing.T, m *Mux, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func bodyKind(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if len(rec.Body.Bytes()) == 0 {
		return ""
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error bodies are JSON, always: %v (%q)", err, rec.Body.String())
	}
	return body.Error.Kind
}

func TestFourErrorKindsAreDistinct(t *testing.T) {
	kinds := []ErrorKind{ErrAuth, ErrNotFound, ErrRefused, ErrTransport}
	codes := map[ErrorKind]int{}
	for _, k := range kinds {
		codes[k] = statusFor(k)
	}
	if codes[ErrAuth] != 401 || codes[ErrNotFound] != 404 || codes[ErrRefused] != 409 || codes[ErrTransport] != 503 {
		t.Fatalf("the table: %v; want auth=401 not-found=404 refused=409 transport=503", codes)
	}
	seen := map[int]ErrorKind{}
	for k, c := range codes {
		if prior, dup := seen[c]; dup {
			t.Fatalf("kinds %s and %s share code %d — the taxonomy collapsed", prior, k, c)
		}
		seen[c] = k
	}
	// And three of them are LIVE through the mux (the fourth, auth, is the
	// framework middleware's own 401 — pinned live by T4's
	// TestUnauthorizedRequestsAnswerAuthNotTransport; here the validator's
	// own refusal carries the kind).
	if _, err := (&Validator{}).ValidateToken("anything"); err == nil || !strings.HasPrefix(err.Error(), "auth:") {
		t.Errorf("the validator's bearer refusal must be kind auth, got %v", err)
	}
	c := &errCaller{err: protocol.ErrStoreUnavailable.WithWhat("no store")}
	m := NewMux(c, Actions())
	rec := get(t, m, "/v1/loop/list")
	if rec.Code != 503 || bodyKind(t, rec) != string(ErrTransport) {
		t.Errorf("store unavailable answered %d/%s; want 503/transport", rec.Code, bodyKind(t, rec))
	}
	rec = get(t, m, "/v1/no/such/route")
	if rec.Code != 404 || bodyKind(t, rec) != string(ErrNotFound) {
		t.Errorf("route miss answered %d/%s; want 404/not-found", rec.Code, bodyKind(t, rec))
	}
	c.err = protocol.ErrInvalidScope.WithWhat("a scoped read needs both keys")
	rec = get(t, m, "/v1/loop/show")
	if rec.Code != 409 || bodyKind(t, rec) != string(ErrRefused) {
		t.Errorf("scope refusal answered %d/%s; want 409/refused", rec.Code, bodyKind(t, rec))
	}
}

// TestAbsentActionIs404NotRefused — T5's absence stays absence. The
// merge this gate forbids is the one that would make N14 a lie: an
// answering attempt is not a refusal of a real action, and a 403 (or a
// 409) would advertise a door behind a lock.
func TestAbsentActionIs404NotRefused(t *testing.T) {
	m := NewMux(&errCaller{}, Actions())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/questions/12/approve", strings.NewReader(`{"answer":"yes"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("answering attempt answered %d; want 404 — absence, not rejection", rec.Code)
	}
	if k := bodyKind(t, rec); k != string(ErrNotFound) {
		t.Errorf("kind %q; want not-found (refused would mean the action exists and said no)", k)
	}
	if !strings.Contains(rec.Body.String(), "requires a person") {
		t.Error("the 404 must still carry the reason and the alternative (O3)")
	}
}

// TestStoreUnavailableNamesWhatIsAbsent — S21 T9's walk found the flattening
// by DEMO-walking §1 against a real unwired daemon: the mount rendered
// ErrStoreUnavailable as Message + retryHint and dropped Data.What, so T6's
// promise that an unwired daemon answers the ABSENCE BY NAME ("no remote
// peers are wired in this daemon — completions have nothing to bind to") —
// and S20's two wiring answers before it — arrived at the peer as a generic
// store outage: a retry it can never succeed at and a doctor pointer at a
// store that is fine. A refusal names the thing refused, at every surface,
// not just at the handler; the retry hint still rides, because a genuine
// store outage shares this code and the prose is right for THAT caller.
func TestStoreUnavailableNamesWhatIsAbsent(t *testing.T) {
	c := &errCaller{err: protocol.ErrStoreUnavailable.WithWhat(
		"no remote peers are wired in this daemon — completions have nothing to bind to")}
	m := NewMux(c, Actions())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/completions",
		strings.NewReader(`{"task":"1","peer":"docs-peer","result":"x"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired completion answered %d; want 503", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "no remote peers are wired") {
		t.Errorf("the wiring fault was flattened to a store outage — the peer was told to retry and see the doctor, never WHAT is absent: %s", body)
	}
	if !strings.Contains(body, "retry") {
		t.Error("the hint must ride for the genuine store outage that shares this code")
	}
}

// TestRestartingIs503 — and a caller can tell it is worth retrying.
func TestRestartingIs503(t *testing.T) {
	c := &errCaller{err: protocol.ErrStoreUnavailable.WithWhat("the store is still opening")}
	m := NewMux(c, Actions())
	rec := get(t, m, "/v1/loop/list")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("restarting/unavailable answered %d; want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "retry") {
		t.Error("the transport body must say retrying is correct — that is the distinction the taxonomy exists for")
	}
}

// TestDaemonErrorsAreClassifiedByType — refused ≠ not-found ≠ transport,
// on the daemon's OWN typed errors: the store's miss is 404 (O4's "a miss
// is not-found"), the step-missing sentinel is 404, scope and junk
// refusals are 409, untyped infrastructure failure is 503-retry. Type
// identity, not string resemblance: same-process dispatch (T4) means
// errors.As reads the ORIGINAL error.
func TestDaemonErrorsAreClassifiedByType(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		kind ErrorKind
	}{
		{"store miss", fmt.Errorf("read loop: %w", store.ErrNotFound), 404, ErrNotFound},
		{"step sentinel", protocol.ErrStepNotFound.WithWhat("no step 99"), 404, ErrNotFound},
		{"scope refusal", protocol.ErrInvalidScope.WithWhat("needs slug and projectRoot"), 409, ErrRefused},
		{"params refusal", protocol.ErrBadParams{Err: fmt.Errorf("name or content is required")}, 409, ErrRefused},
		{"pin refusal", loop.PinRefusal{Kind: "invalid-blueprint", Detail: "1 invalid diagnostic(s)"}, 409, ErrRefused},
		{"untyped infra", fmt.Errorf("socket pipe died"), 503, ErrTransport},
		{"unknown domain code", &protocol.Error{Code: -31999, Message: "the gate is closed today", Data: protocol.Data{What: "a typed refusal", Todo: "open it from the app"}}, 409, ErrRefused},
	}
	for _, tc := range cases {
		c := &errCaller{err: tc.err}
		m := NewMux(c, Actions())
		rec := get(t, m, "/v1/loop/show?id=loop-1")
		if rec.Code != tc.code || bodyKind(t, rec) != string(tc.kind) {
			t.Errorf("%s: answered %d/%q; want %d/%q", tc.name, rec.Code, bodyKind(t, rec), tc.code, tc.kind)
		}
	}
	// The Todo reaches the refused body (O3's alternative, not a restated
	// refusal): a caller reading the prose learns the next step.
	c := &errCaller{err: &protocol.Error{Code: -31998, Message: "understood, declined", Data: protocol.Data{Todo: "answer from the app instead"}}}
	m := NewMux(c, Actions())
	if rec := get(t, m, "/v1/loop/show?id=loop-1"); !strings.Contains(rec.Body.String(), "answer from the app instead") {
		t.Errorf("the refused body dropped the daemon's Todo: %s", rec.Body.String())
	}
}

// TestEveryHandlerHasATimeout — enumerated over the mux, not sampled:
// every entry the table projects must answer transport/503 at the bound
// when the daemon behind it wedges. A handler without a timeout is a
// connection held for a process that stopped listening — the abandoned-CI
// failure this task exists to close.
func TestEveryHandlerHasATimeout(t *testing.T) {
	prevCall, prevWrite := WithSurfaceTimeouts(150*time.Millisecond, 5*time.Second)
	defer func() { _, _ = WithSurfaceTimeouts(prevCall, prevWrite) }()

	c := newFakeCaller()
	for _, a := range Actions() {
		c.delays[a.SocketMethod] = 3 * time.Second
	}
	m := NewMux(c, Actions())
	for _, e := range m.Entries() {
		start := time.Now()
		var rec *httptest.ResponseRecorder
		if e.Method == http.MethodPost {
			req := httptest.NewRequest(http.MethodPost, e.Path, strings.NewReader(`{"blueprint":"b","goal":"g","workspaceRoot":"/w"}`))
			rec = httptest.NewRecorder()
			m.ServeHTTP(rec, req)
		} else {
			rec = get(t, m, e.Path+"?id=loop-1&slug=s&projectRoot=/w&all=true")
		}
		elapsed := time.Since(start)
		if rec.Code != http.StatusServiceUnavailable || bodyKind(t, rec) != string(ErrTransport) {
			t.Errorf("%s %s: wedged daemon answered %d/%q; want 503/transport", e.Method, e.Path, rec.Code, bodyKind(t, rec))
		}
		if !strings.Contains(rec.Body.String(), "did not answer") {
			t.Errorf("%s %s: the timeout must name itself, got %q", e.Method, e.Path, rec.Body.String())
		}
		if elapsed > time.Second {
			t.Errorf("%s %s: answered after %s — the bound is 150ms, not the daemon's discretion", e.Method, e.Path, elapsed)
		}
	}
}

// TestHungStdioCallerDoesNotPreventShutdown — T5 left the never-returns
// drain case to T7; this is that case: a caller wedged in the daemon must
// not make EOF unreturnable. The stdio process behind a CI pipe exists
// when stdin closes, bounded by the same threshold the HTTP mounting uses
// to call a dispatch wedged.
func TestHungStdioCallerDoesNotPreventShutdown(t *testing.T) {
	prevCall, prevWrite := WithSurfaceTimeouts(200*time.Millisecond, 5*time.Second)
	defer func() { _, _ = WithSurfaceTimeouts(prevCall, prevWrite) }()

	c := newFakeCaller()
	c.delays[protocol.MethodLoopList] = 60 * time.Second // wedged, not slow
	srv := NewServer(c, Actions())
	inR, inW := io.Pipe()
	out := &discardWriter{}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background(), inR, out) }()
	if _, err := fmt.Fprintln(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"aeon_loop_list","arguments":{}}}`); err != nil {
		t.Fatal(err)
	}
	inW.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve never returned with a wedged caller — the drain is unbounded after all")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestNoErrorBodyCarriesAKey — I6, asserted on EVERY error path rather
// than trusted from the plumbing: the sentinel key is configured, every
// discriminated path is exercised, and the value appears in no response.
// (T2's <redacted> types are the structural half; this is the empirical
// half — the writer is where leaks would go, so the writer is watched.)
func TestNoErrorBodyCarriesAKey(t *testing.T) {
	const sentinel = "s3ntin3l-key-value-9f2c4a"
	t.Setenv(KeyEnv, sentinel)

	c := &errCaller{err: protocol.ErrStoreUnavailable.WithWhat("closed")}
	m := NewMux(c, Actions())
	var bodies []string
	rec := get(t, m, "/v1/no/such/route")
	bodies = append(bodies, rec.Body.String())
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/questions/1/approve", strings.NewReader(`{}`)))
	bodies = append(bodies, rec.Body.String())
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/loops", strings.NewReader(`not json`)))
	bodies = append(bodies, rec.Body.String())
	c.err = protocol.ErrInvalidScope.WithWhat("x")
	bodies = append(bodies, get(t, m, "/v1/loop/show").Body.String())
	c.err = protocol.ErrStoreUnavailable.WithWhat("x")
	bodies = append(bodies, get(t, m, "/v1/loop/list").Body.String())
	if _, err := (&Validator{}).ValidateToken("bearer"); err != nil {
		bodies = append(bodies, err.Error())
	}
	// The oversized-body path (409 refused) too — every path, not most.
	big := `{"pad":"` + strings.Repeat("x", maxBody+16) + `"}`
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/loops", strings.NewReader(big)))
	bodies = append(bodies, rec.Body.String())
	// The webhook path (S20 T6) errors like every other route — its
	// none-level SECURITY means the framework skips the key, not that its
	// transport failures stop being classified errors.
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/webhooks?id=ship&signature=deadbeef", strings.NewReader(`{"ref":"abc"}`)))
	bodies = append(bodies, rec.Body.String())

	if len(bodies) != 8 {
		t.Fatalf("exercised %d error paths; want 8 — the enumeration is the assertion", len(bodies))
	}
	for i, b := range bodies {
		if strings.Contains(b, sentinel) {
			t.Errorf("path %d leaked the configured key into the body: %s", i, b)
		}
		if strings.Contains(strings.ToLower(b), "authorization") {
			t.Errorf("path %d echoed the credential header's name/value shape: %s", i, b)
		}
	}
	// And the Key type itself stays mute even when handed to a formatter.
	k, err := ResolveKey("")
	if err != nil || k.IsEmpty() {
		t.Fatalf("the sentinel key should load from %s: %v", KeyEnv, err)
	}
	for _, s := range []string{fmt.Sprint(k), fmt.Sprintf("%#v", k), string(mustJSON(k))} {
		if strings.Contains(s, sentinel) {
			t.Errorf("Key rendered the secret: %q", s)
		}
	}
	_ = os.Getenv // (no env reads left unbalanced — t.Setenv owns cleanup)
}
