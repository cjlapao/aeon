package narrow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	fmw_middleware "github.com/cjlapao/go-backend-scaffolding/framework/api/middleware"

	"github.com/cjlapao/aeon/internal/protocol"
)

// http_test.go carries T4's gates. The connect-attempt gate
// (TestNoPortWithoutAnExplicitAddress) lives where the process is —
// cmd/aeon's binary fixture — not here.

// countingCaller is the daemon side seen from the HTTP mounting: it records
// WHICH method arrived with WHAT params, so route assertions reach all the
// way to the socket call they translate into.
type countingCaller struct {
	mu    sync.Mutex
	calls []recordedCall
}

type recordedCall struct {
	method string
	params string
}

func (c *countingCaller) Call(method protocol.MethodName, params, result any) error {
	raw, _ := json.Marshal(params)
	c.mu.Lock()
	c.calls = append(c.calls, recordedCall{method: string(method), params: string(raw)})
	c.mu.Unlock()
	if p, ok := result.(*any); ok {
		*p = map[string]any{"ok": true, "method": string(method)}
	}
	return nil
}

func (c *countingCaller) last(t *testing.T) recordedCall {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		t.Fatal("the daemon caller was never reached")
	}
	return c.calls[len(c.calls)-1]
}

func (c *countingCaller) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// --- set equality (N18, D4, C5) --------------------------------------------

// TestStdioAndHTTPSetsAreEqual — the test that makes N18 real, both
// directions: table → stdio tool list, table → http mux, and nothing in
// either mounting that the table lacks. The CLI form a human can run
// (diff <(aeon narrow list --mounting=stdio) <(…http)) is gated in
// cmd/aeon; this is the same assertion against the two mountings' own
// rendering functions, which is where a divergence would actually be born.
func TestStdioAndHTTPSetsAreEqual(t *testing.T) {
	table := Actions()
	stdio := NewServer(newFakeCaller(), table).ToolNames()
	httpNames := muxNames(NewMux(newFakeCaller(), table))

	assertSameSet(t, "table→stdio", namesOf(table), stdio)
	assertSameSet(t, "table→http", namesOf(table), httpNames)
	assertSameSet(t, "stdio↔http", stdio, httpNames)
}

func muxNames(m *Mux) []string {
	var out []string
	for _, e := range m.Entries() {
		out = append(out, e.Action.Name)
	}
	return out
}

func namesOf(table []Action) []string {
	var out []string
	for _, a := range table {
		out = append(out, a.Name)
	}
	return out
}

func assertSameSet(t *testing.T, label string, want, got []string) {
	t.Helper()
	w := map[string]int{}
	for _, s := range want {
		w[s]++
	}
	for _, s := range got {
		w[s]--
		if _, ok := w[s]; !ok {
			t.Errorf("%s: %q is present on one side and absent from the other — a third surface (P25 says two)", label, s)
		}
	}
	for s, n := range w {
		if n != 0 {
			t.Errorf("%s: %s count off by %d", label, s, n)
		}
	}
}

// --- the mux is the surface (T3's emitted-list shape) -----------------------

// TestNoRouteOutsideTheTable — asserted ON THE MUX both ways, plus
// behaviourally: operational-control paths are not mounted, so they answer
// the discriminated not-found carrying the boundary hint — the absence,
// legible (N14), not a guess.
func TestNoRouteOutsideTheTable(t *testing.T) {
	c := newFakeCaller()
	m := NewMux(c, Actions())
	count := func() int { return len(c.calledMethods()) }
	entries := m.Entries()
	table := Actions()
	if len(entries) != len(table) {
		t.Fatalf("mux mounted %d routes for %d table entries", len(entries), len(table))
	}
	byName := map[string]MuxEntry{}
	for _, e := range entries {
		if _, dup := byName[e.Action.Name]; dup {
			t.Errorf("%s mounted twice — two routes for one action is two answers", e.Action.Name)
		}
		byName[e.Action.Name] = e
	}
	for _, a := range table {
		if _, ok := byName[a.Name]; !ok {
			t.Errorf("table entry %s has no route", a.Name)
		}
	}
	// No emitted path smuggles an operational token.
	forbidden := map[string]bool{}
	for _, w := range ForbiddenOperationalControl {
		forbidden[w] = true
	}
	for _, e := range entries {
		for _, tok := range tokenize(e.Path) {
			if forbidden[tok] {
				t.Errorf("route %s %s carries operational-control token %q", e.Method, e.Path, tok)
			}
		}
	}
	// Behavioural absence: the path a `stop` would want is a not-found with
	// the hint, and the daemon caller was not consulted for it.
	before := count()
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/loop/stop", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /v1/loop/stop: want 404, got %d", rec.Code)
	}
	var body map[string]map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"]["kind"] != string(ErrNotFound) {
		t.Errorf("the absence must arrive discriminated (O5): %v", body)
	}
	if !strings.Contains(body["error"]["message"], BoundaryAbsentHint) {
		t.Errorf("the not-found must carry the boundary hint: %v", body["error"])
	}
	// The method-mismatch case is also absence: the trigger is a POST, and
	// poking it with a GET must not reach it.
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/loops", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET on the trigger route must not dispatch (method mismatch is absence): %d", rec.Code)
	}
	if count() != before {
		t.Error("a 404 answered from the mux must not reach the daemon")
	}
}

// TestReadIsGETTriggerIsPOST — the mechanical mapping, asserted on the mux
// AND on the routes the framework will mount (HTTPRoutes is what the api
// plugin registers; asserting both keeps "on the mux" from meaning a
// private object the mount ignores).
func TestReadIsGETTriggerIsPOST(t *testing.T) {
	m := NewMux(newFakeCaller(), Actions())
	routes := NewHTTPService(newFakeCaller(), Actions()).HTTPRoutes()
	// HTTPRoutes carries the action routes (one per entry, the mechanical
	// mapping below) PLUS the seven catch-all REFUSAL routes (T8's walk:
	// unmounted shapes answered the framework's bare 404 before the mux's
	// reasoned one; the webhooks shape joined the set in S20 T6 — a POST to
	// /v1/webhooks/anything is still a refusal). A refusal is not an
	// action, so the entries count governs only the action routes — and the
	// refusal set is pinned exactly, so no capability can smuggle itself in
	// through it.
	const refusals = 7
	if len(routes) != len(m.Entries())+refusals {
		t.Fatalf("HTTPRoutes renders %d; want %d actions + %d refusals (the mux mounts %d)", len(routes), len(m.Entries()), refusals, len(m.Entries()))
	}
	for _, r := range routes {
		if strings.Contains(r.Path, "{rest:.*}") {
			if !strings.HasPrefix(r.Description, "narrow surface refusal") {
				t.Errorf("%s %s carries a non-refusal description — the catch-all set is closed", r.Method, r.Path)
			}
		}
	}
	for _, e := range m.Entries() {
		want := http.MethodGet
		if e.Action.Kind == KindTrigger || e.Action.Kind == KindWebhook || e.Action.Kind == KindCallback {
			want = http.MethodPost
		}
		if e.Method != want {
			t.Errorf("%s: want %s, got %s (reads GET, the trigger POSTS, the webhook POSTS its deliveries, the callback POSTS its completions)", e.Action.Name, want, e.Method)
		}
	}
	noneRoutes := 0
	for _, r := range routes {
		if r.NoPrefix != true {
			t.Errorf("%s %s: the narrow surface speaks /v1, not the product API's prefix", r.Method, r.Path)
		}
		// R-5 pinned HERE, where the level is rendered: the none-level set
		// is ENUMERATED, and both members are named — POST /v1/webhooks
		// (S20 T6) and POST /v1/completions (S21 T6), the two deliveries
		// whose handler authenticates by HMAC before anything else runs
		// and whose senders have never seen AEON_HTTP_KEY. Everything else
		// stays apikey; an unnamed none-level route is a keyless door with
		// a new name. The exemption remains ONE function (webhookLevel) —
		// growth of the SET is what this pin prices, and it grew exactly
		// once, in a table-changing commit, by the same argument.
		if r.SecurityRequirement == nil {
			t.Errorf("%s %s carries no security requirement at all", r.Method, r.Path)
			continue
		}
		if string(r.SecurityRequirement.SecurityLevel) == "none" {
			noneRoutes++
			if r.Method != http.MethodPost || (r.Path != "/v1/webhooks" && r.Path != "/v1/completions") {
				t.Errorf("%s %s carries none — the keyless set is POST /v1/webhooks and POST /v1/completions, both named, both HMAC-first", r.Method, r.Path)
			}
		} else if string(r.SecurityRequirement.SecurityLevel) != "apikey" {
			t.Errorf("%s %s carries %q: apikey or (delivery routes only) none", r.Method, r.Path, r.SecurityRequirement.SecurityLevel)
		}
	}
	if noneRoutes != 2 {
		t.Errorf("%d none-level routes; want exactly 2 (webhook + completion) — R-5 is an enumerated exception, not a policy", noneRoutes)
	}
}

// TestQ3Recorded — the scaffolding's listener was REUSED, and the record
// of how that is true is machine-checkable: the bootstrap registers the
// framework's api plugin (Q3's answer), and the narrow package contains NO
// listener of its own — a second `net.Listen` in this binary is the
// failure Q3 exists to prevent (S1's B8 generalised: two of a resource is
// how the wrong one wins).
func TestQ3Recorded(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "bootstrap", "register.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"api.New(svc)", "WithTokenValidator", "WithNarrow"} {
		if !strings.Contains(src, want) {
			t.Errorf("register.go must contain %s — Q3's answer is the framework's listener, wired here", want)
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range entries {
		if !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(de.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"net.Listen", "http.Server", "http.ListenAndServe"} {
			if strings.Contains(string(b), banned) {
				t.Errorf("%s contains %s — narrow owns routes, not listeners (Q3: the scaffolding's server is reused, never duplicated)", de.Name(), banned)
			}
		}
	}
}

// --- auth (T2's validator, mounted) ----------------------------------------

// TestUnauthenticatedRequestIsRefused — through the framework's OWN auth
// middleware (the same constructor the api server wires per-route), a
// missing or wrong key never reaches an action: the handler count is the
// assertion, not the status text. And Bearer-shaped headers answer
// refused from ValidateToken — one key, no second door.
func TestUnauthenticatedRequestIsRefused(t *testing.T) {
	secret := "aeon-test-key-0123456789abcdef"
	key, err := ResolveKey(writeKeyFile(t, secret))
	if err != nil {
		t.Fatal(err)
	}
	c := &countingCaller{}
	svc := NewHTTPService(c, Actions())
	route := svc.HTTPRoutes()[0] // aeon_loop_list — GET /v1/loop/list
	mw := fmw_middleware.NewAuthorizationPreMiddleware(NewValidator(key), nil, &route)
	// The same chain the api server builds per route (api/service/server.go:
	// AddPreMiddleware(auth), handler runs only when the middleware says
	// Continue) — reproduced here so the assertion is the framework's real
	// gate, not a lookalike. One recorder per request, RETURNED: the
	// response asserted is the response the middleware actually wrote
	// (an httptest recorder's zero Code is 200 — a fresh recorder is not
	// evidence, a written one is).
	h := func(req *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		if res := mw.Execute(rec, req); res.Continue {
			route.Handler(rec, req)
		}
		return rec
	}

	// Missing key.
	if got := h(httptest.NewRequest(http.MethodGet, route.Path, nil)); got.Code != http.StatusUnauthorized {
		t.Errorf("missing key: want 401, got %d", got.Code)
	}
	// Wrong key.
	req := httptest.NewRequest(http.MethodGet, route.Path, nil)
	req.Header.Set("Authorization", "wrong-key")
	if got := h(req); got.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: want 401, got %d", got.Code)
	}
	// Bearer-shaped: the JWT arm — refused on an apikey-level route, not
	// guessed (the framework's own door; T2's ValidateToken is the second
	// refusing door behind it).
	req = httptest.NewRequest(http.MethodGet, route.Path, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	if got := h(req); got.Code != http.StatusUnauthorized {
		t.Errorf("bearer must be refused on the narrow surface: got %d", got.Code)
	}
	if c.count() != 0 {
		t.Errorf("a refused request reached the daemon %d times — auth refusal must happen UPSTREAM of any action", c.count())
	}
	// The right key (bare Authorization, the middleware's apikey arm) works.
	req = httptest.NewRequest(http.MethodGet, route.Path, nil)
	req.Header.Set("Authorization", secret)
	if got := h(req); got.Code != http.StatusOK {
		t.Fatalf("good key: want 200, got %d body=%s", got.Code, got.Body.String())
	}
	if got := c.last(t); got.method != "loop.list" {
		t.Errorf("good key reached %q, want loop.list", got.method)
	}
}

// TestParamsReachTheDaemon — query merges under JSON body, mechanically,
// for every action the same way: `?id=x` and `{"id":"x"}` are the same
// request to the daemon.
func TestParamsReachTheDaemon(t *testing.T) {
	c := &countingCaller{}
	m := NewMux(c, Actions())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/loop/show?id=loop-77", nil))
	got := c.last(t)
	if !strings.Contains(got.params, `"id":"loop-77"`) {
		t.Errorf("query param did not reach the daemon: %s", got.params)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/loops", strings.NewReader(`{"blueprint":"dev-loop","prompt":"go"}`))
	m.ServeHTTP(rec, req)
	got = c.last(t)
	if got.method != "loop.start" || !strings.Contains(got.params, `"blueprint":"dev-loop"`) {
		t.Errorf("body params did not reach the trigger: %s", got.params)
	}
	// Body wins over query on the same key (the more deliberate source).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/loop/show?id=query", strings.NewReader(`{"id":"body"}`))
	m.ServeHTTP(rec, req)
	if got := c.last(t); !strings.Contains(got.params, `"id":"body"`) {
		t.Errorf("body must outrank query: %s", got.params)
	}
	// A non-object body is a CALLER mistake: T7's table reserves
	// transport/503 for failures where retrying is correct, and resending
	// `not json` never becomes JSON. Understood-and-declined is refused/409
	// (errors.go), with the fix in the message (O3). The daemon is not asked.
	before := c.count()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/loop/show", strings.NewReader(`not json`))
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("bad body: want a refused-class refusal (caller mistake, retry cannot help), got %d", rec.Code)
	}
	if c.count() != before {
		t.Error("a malformed body must not reach the daemon")
	}
}

// --- saying it is open (O8, I6) ---------------------------------------------

// TestStartupAndDoctorSayItIsOpen — both sentences carry the address and
// the fact of the key, and NEITHER ever carries the key. The key value is
// chosen to be greppable against every line.
func TestStartupAndDoctorSayItIsOpen(t *testing.T) {
	secret := "SUPERSECRET-aeon-key-do-not-print-me"
	key, err := ResolveKey(writeKeyFile(t, secret))
	if err != nil {
		t.Fatal(err)
	}
	_ = key // resolution is the path under test; the lines are pure.
	startup := StartupLine("127.0.0.1:7777")
	for _, want := range []string{"127.0.0.1:7777", "narrow surface", "key configured"} {
		if !strings.Contains(startup, want) {
			t.Errorf("the startup line must say %q: %q", want, startup)
		}
	}
	if strings.Contains(startup, secret) {
		t.Error("the startup line must NEVER contain the key (I6)")
	}
	doctor := strings.Join(DoctorLines("127.0.0.1:7777", true, true), "\n")
	for _, want := range []string{"127.0.0.1:7777", "listening", "key"} {
		if !strings.Contains(doctor, want) {
			t.Errorf("doctor must say %q: %q", want, doctor)
		}
	}
	if strings.Contains(doctor, secret) {
		t.Error("doctor must NEVER contain the key (I6)")
	}
	// The not-listening case is stated as the design, not a fault — and the
	// key-set hint distinguishes "no --http" from "no daemon".
	closed := strings.Join(DoctorLines("", false, false), "\n")
	if !strings.Contains(closed, "nothing binds without an explicit address") {
		t.Errorf("the ordinary state must be SAID: %q", closed)
	}
	if strings.Contains(closed, "(a key IS set") {
		t.Error("no key is set in this scenario; the hint must not fire")
	}
	hinted := strings.Join(DoctorLines("127.0.0.1:7777", false, true), "\n")
	if !strings.Contains(hinted, "a key IS set") {
		t.Errorf("a set key with no listener is worth saying: %q", hinted)
	}
}

// TestDoctorFlagsANonLocalBind — 0.0.0.0 is permitted (a CI runner in a
// container needs it) and worth NOTICING; loopback is not.
func TestDoctorFlagsANonLocalBind(t *testing.T) {
	all := strings.Join(DoctorLines("0.0.0.0:7777", true, true), "\n")
	if !strings.Contains(all, "NOT host-local") {
		t.Errorf("0.0.0.0 must be flagged as worth attention: %q", all)
	}
	local := strings.Join(DoctorLines("127.0.0.1:7777", true, true), "\n")
	if strings.Contains(local, "NOT host-local") {
		t.Errorf("loopback must not cry wolf: %q", local)
	}
	localhost := strings.Join(DoctorLines("localhost:7777", true, true), "\n")
	if strings.Contains(localhost, "NOT host-local") {
		t.Errorf("localhost is host-local: %q", localhost)
	}
	specific := strings.Join(DoctorLines("10.0.0.5:7777", true, true), "\n")
	if !strings.Contains(specific, "NOT host-local") {
		t.Errorf("a routable bind must be flagged: %q", specific)
	}
}

// --- one table, both doors ---------------------------------------------------

// TestBothMountingsCanRunAtOnce — one table (the SAME slice value), one
// daemon caller, both mountings answering concurrently. Two instances of
// the table would be two answers waiting to drift; this asserts they share
// the one registration point's product.
func TestBothMountingsCanRunAtOnce(t *testing.T) {
	shared := Actions()
	c := &countingCaller{}
	stdio := NewServer(c, shared)
	m := NewMux(c, shared)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = stdio.ToolNames()
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/loop/list", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("http answer under concurrency: %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	// Both mountings reached the SAME caller: eight http dispatches and no
	// table-copy divergence between the two renderings.
	if c.count() != 8 {
		t.Errorf("http dispatch count = %d, want 8 — both mountings share one caller", c.count())
	}
	assertSameSet(t, "shared table", namesOf(shared), stdio.ToolNames())
	assertSameSet(t, "shared table", namesOf(shared), muxNames(m))
}

// writeKeyFile drops a key into a temp file and returns its path.
func writeKeyFile(t *testing.T, secret string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCatchAllRefusalRoutesReachTheWire — T8's walk killed a defect T4
// carried and T7's package tests could not see: the framework router
// answers an unmounted path with a bare "404 page not found" BEFORE the
// mux speaks, so the reasoned 404 was true in-process and false on the
// wire. The fix mounts the attempt shapes as routes into the same
// ServeHTTP; this pins the mountings (six shapes, not actions) AND that
// the routed handler produces the reasoned body, not the bare one.
func TestCatchAllRefusalRoutesReachTheWire(t *testing.T) {
	svc := NewHTTPService(&errCaller{}, Actions())
	routes := svc.HTTPRoutes()
	want := map[string]bool{
		"POST /v1/questions/{rest:.*}":   false,
		"GET /v1/questions/{rest:.*}":    false,
		"DELETE /v1/questions/{rest:.*}": false,
		"POST /v1/loops/{rest:.*}":       false,
		"GET /v1/loops/{rest:.*}":        false,
		"DELETE /v1/loops/{rest:.*}":     false,
	}
	for _, r := range routes {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
			if r.Handler == nil {
				t.Errorf("%s mounted without a handler", key)
			}
		}
		if strings.Contains(r.Path, "answer") || strings.Contains(r.Path, "approve") {
			t.Errorf("a specific answering path %s was mounted — that would be an action", r.Path)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("refusal route %s is not mounted — the framework's bare 404 answers there", key)
		}
	}
	// And the routes ANSWER with the reasoned 404, not the bare one.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/questions/41/approve", strings.NewReader(`{"answer":"approve"}`))
	// Drive it exactly as the framework would: through the ROUTED handler,
	// not the mux directly.
	var handler http.HandlerFunc
	for _, r := range svc.HTTPRoutes() {
		if r.Path == "/v1/questions/{rest:.*}" && r.Method == http.MethodPost {
			handler = r.Handler
		}
	}
	if handler == nil {
		t.Fatal("no POST catch-all to drive")
	}
	handler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("the routed answer was %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "page not found") {
		t.Errorf("the framework's bare 404 words on the wire: %s", body)
	}
	if !strings.Contains(body, "requires a person") {
		t.Errorf("the answering refusal sentence did not reach the routed response: %s", body)
	}
}
