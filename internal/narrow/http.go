package narrow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	api_ifaces "github.com/cjlapao/go-backend-scaffolding/framework/api/interfaces"
	api_models "github.com/cjlapao/go-backend-scaffolding/framework/core/api/models"
	corecore "github.com/cjlapao/go-backend-scaffolding/framework/core/core"
	system_ifaces "github.com/cjlapao/go-backend-scaffolding/framework/core/system/interfaces"

	"github.com/cjlapao/aeon/internal/protocol"
)

// http.go is the CI-facing mounting of the narrow surface. It owns no
// listener: the framework's `api` service (registered from day one and
// pinned off — bootstrap.Pins) is the HTTP server this mounting rides, and
// the routes arrive through the framework's own discovery, the
// RouteContributor seam. Q3 answered: reuse, do not build a second
// listener — two HTTP servers in one binary is how the wrong one gets
// bound (S1's B8 lesson about registry presence generalised).
//
// Like stdio.go, the mapping is mechanical and the table is the only
// source: a `read` becomes a GET, the single `trigger` becomes a POST, and
// NO route exists that the table does not (N18). Like stdio's emitted
// list, what is asserted is the MUX — the object that hands handlers to
// the framework — because a route added directly to the mux would be the
// third surface arriving by the back door.

// ServiceName is the registry name of the HTTP mounting's service. The
// name matters: it is what the composition golden file records and what
// the api plugin's contributor discovery checks for enabledness.
const ServiceName = "narrow"

// maxBody bounds request bodies. Legitimate inputs are small (a blueprint
// to validate is the largest); the bound exists so "read everything" is not
// a property of any handler here.
const maxBody = 8 << 20

// Mux is the mux the narrow surface owns: table in, handlers out. It is
// constructed once and it is what HTTPRoutes() renders from — there is no
// other path by which a handler reaches the framework's server.
type Mux struct {
	caller  Caller
	mu      sync.RWMutex
	entries []MuxEntry
}

// MuxEntry is one mounted route, exposed for the assertions that must run
// on the mux rather than on the table (TestNoRouteOutsideTheTable's shape
// from T3's emitted-list assertion).
type MuxEntry struct {
	Method  string
	Path    string
	Action  Action
	Handler http.HandlerFunc
}

// NewMux mounts the table. Production calls it with Actions(); the set
// equality test drives the SAME object the framework will mount.
func NewMux(caller Caller, table []Action) *Mux {
	m := &Mux{caller: caller}
	for _, a := range table {
		method, path := httpProjection(a)
		m.entries = append(m.entries, MuxEntry{
			Method:  method,
			Path:    path,
			Action:  a,
			Handler: m.handler(a),
		})
	}
	return m
}

// httpProjection is the mechanical mapping, per KIND (T1's closed set) and
// never per action: reads are GETs at their name turned into a path, and
// the trigger is a POST to the collection it creates — `POST /v1/loops`
// with a body starts a loop, the way a POST to a collection should.
func httpProjection(a Action) (method, path string) {
	if a.Kind == KindTrigger {
		return http.MethodPost, "/v1/loops"
	}
	if a.Kind == KindWebhook {
		// POST to a collection: the deliveries are created here, and the
		// trigger id is a PARAM (R-6: the signature rides as a param
		// because stdio has no headers — the id rides with it so both
		// mountings speak one request shape).
		return http.MethodPost, "/v1/webhooks"
	}
	if a.Kind == KindCallback {
		// POST to a collection, the webhook's own reasoning carried over
		// (R-6): the completion is CREATED here by a caller who is not a
		// key-holder, and everything the daemon reads — including the
		// signature — rides as a param, headers being sugar.
		return http.MethodPost, "/v1/completions"
	}
	name := strings.TrimPrefix(a.Name, "aeon_")
	return http.MethodGet, "/v1/" + strings.ReplaceAll(name, "_", "/")
}

// callbackParams is the completion's fold: the ordinary merge (query then
// JSON body — the daemon reads FIELDS, and no re-serialization can break a
// signature that runs over canonicalised fields, unlike the webhook's
// exact-bytes payload), plus the sender headers as sugar filling params the
// body did not set (N18's one-request-shape rule, shared with stdio).
func callbackParams(r *http.Request) (map[string]any, *Error) {
	params, bad := paramsFor(r)
	if bad != nil {
		return nil, bad
	}
	for header, param := range map[string]string{
		"X-Aeon-Completion-Signature": "signature",
		"X-Aeon-Signature":            "signature",
		"X-Aeon-Completion-Timestamp": "timestamp",
		"X-Aeon-Timestamp":            "timestamp",
		"X-Aeon-Delivery":             "deliveryId",
	} {
		if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
			if _, set := params[param]; !set {
				params[param] = v
			}
		}
	}
	return params, nil
}

// Entries is the mux seen from outside — the assertion surface.
func (m *Mux) Entries() []MuxEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]MuxEntry(nil), m.entries...)
}

// ServeHTTP dispatches by exact method+path match — the same rule the
// framework's router will apply to the routes HTTPRoutes() hands it, and
// the reason the mux can be asserted directly: what answers here is what
// answers there. An unmatched path is the absence made legible (N14): a
// discriminated not-found carrying the boundary hint, never a guess.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	entries := m.entries
	m.mu.RUnlock()
	for _, e := range entries {
		if e.Method == r.Method && e.Path == r.URL.Path {
			e.Handler(w, r)
			return
		}
	}
	// The question-shaped attempts get the reason and the alternative, not
	// just the hint (T5): someone POSTing to the obvious URL is told WHY
	// there is no such action and WHERE answering lives. Still 404 — the
	// action does not exist (N14's reasoning); a 403 would advertise a
	// door behind a lock, and there is no door.
	if msg, refused := QuestionRefusalFor(r.Method, r.URL.Path); refused {
		writeError(w, NewMsg(ErrNotFound, msg))
		return
	}
	writeError(w, NewMsg(ErrNotFound, "no such route on the narrow surface — " + BoundaryAbsentHint))
}

// handler is the ONE handler factory; per-action handler code is absent by
// construction, which is what "mechanical" means here.
func (m *Mux) handler(a Action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ONE body read per request, chosen by kind: the delivery folds the
		// EXACT bytes as `payload` (the HMAC runs over what the sender
		// signed — re-serialization is not allowed to stand between a
		// signature and its proof, so this route may never JSON-merge the
		// body away), everything else merges query and JSON body as before.
		var params map[string]any
		var bad *Error
		if a.Kind == KindWebhook {
			// The GitHub headers fold INTO the same params stdio uses
			// (sugar, never a second path: a header that disagreed with
			// the param would be a fork, so params win).
			params, bad = webhookParams(r)
		} else if a.Kind == KindCallback {
			// S21 T6: the completion's fields ride the body (there is no
			// exact-bytes signature to protect — the HMAC runs over the
			// canonicalised FIELDS one layer down), so it merges like any
			// other request, with the sender headers as sugar for the
			// three fields stdio carries in the body itself.
			params, bad = callbackParams(r)
		} else {
			params, bad = paramsFor(r)
		}
		if bad != nil {
			writeError(w, bad)
			return
		}
		if a.Kind == KindTrigger {
			// The trigger's one transform (D9, G14): attended is forced
			// false here, on this mounting, through the same function the
			// stdio mounting calls. A CI caller claiming a human is
			// present is not believed.
			params = TriggerParamsFor(params)
		}
		if m.caller == nil {
			// A mounting fault, stated as one: the table is here but the
			// daemon behind it is not. Not the caller's mistake, and the
			// kind says so (O5).
			writeError(w, NewMsg(ErrTransport, "the narrow surface has no daemon behind it"))
			return
		}
		raw, _ := json.Marshal(params)
		// T7: every handler has a timeout, and it is this ONE dispatch site
		// rather than nine per-handler wrappers — a handler that would
		// otherwise hold a connection for a wedged daemon answers
		// transport/503 (the retryable kind) at the bound. The late
		// outcome lands on a buffered channel and the goroutine exits;
		// the dispatch underneath is itself bounded by the socket
		// client's bound, so nothing runs unbounded on the daemon side.
		type outcome struct {
			err    error
			result any
		}
		oc := make(chan outcome, 1)
		go func() {
			var res any
			oc <- outcome{err: m.caller.Call(a.SocketMethod, json.RawMessage(raw), &res), result: res}
		}()
		select {
		case o := <-oc:
			if (a.Kind == KindWebhook || a.Kind == KindCallback) && o.err == nil {
				// The delivery answers with its OWN status (401 for a bad
				// signature, 404 for an unknown id, 200 for a fire) — the
				// outcome carries the number because the outcome IS the
				// answer; a 200-on-a-refusal would teach senders to stop
				// reading the body.
				writeJSON(w, webhookResponseStatus(o.result), o.result)
				return
			}
			if o.err != nil {
				var e *Error
				if !errors.As(o.err, &e) {
					// The daemon's sentence verbatim (I5), discriminated
					// into the four kinds by type identity (T7) — the
					// process boundary is in-process, so errors.As/Is
					// read the ORIGINAL error, not a string shadow.
					e = classifyDaemonError(o.err)
				}
				writeError(w, e)
				return
			}
			writeJSON(w, http.StatusOK, o.result) // single door: no partials, stalled writes reaped
		case <-time.After(callTimeout()):
			writeError(w, callerTimedOut(callTimeout()))
		}
	}
}

// paramsFor merges query values and a JSON body, body last (the more
// deliberate of the two). Both are optional; `GET /v1/loop/show?id=x` and
// `GET /v1/loop/show` with a body are the same request to the daemon.
func paramsFor(r *http.Request) (map[string]any, *Error) {
	params := map[string]any{}
	if r.URL != nil {
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				params[k] = vs[0] // mechanical: last wire-wins is the daemon's problem, not the mux's
			}
		}
	}
	if r.Body == nil {
		return params, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, NewMsg(ErrTransport, "cannot read the request: " + err.Error())
	}
	if len(body) > maxBody {
		// Caller's mistake, not the daemon's: kind `transport` PROMISES a
		// retry is correct, and resending an oversized body never helps.
		// The four-kind table (errors.go) puts "understood and declined"
		// in `refused`, and the message says what to fix.
		return nil, NewMsg(ErrRefused, fmt.Sprintf("request body over %d bytes — send a smaller body; the daemon is not asked", maxBody))
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return params, nil
	}
	var fromBody map[string]any
	if err := json.Unmarshal(body, &fromBody); err != nil {
		return nil, NewMsg(ErrRefused, "request body must be a JSON object — fix the body; resending it unchanged will fail again: " + err.Error())
	}
	for k, v := range fromBody {
		params[k] = v
	}
	return params, nil
}

// writeError and writeJSON moved to errors.go with T7's discrimination —
// one file holds kinds, codes, classification and the single response door.

// StartupLine is the O8 sentence, a pure function so the assertion "no key
// in it" (I6) is testable without a listener: the address and the FACT a
// key is configured, and T2 makes the second half of that sentence true
// by construction — a keyless bind refuses to start, so listening already
// implies a key.
func StartupLine(addr string) string {
	return fmt.Sprintf("listening on %s (narrow surface, key configured)", addr)
}

// DoctorLines is the same statement at question time — "a default only
// helps the first time; six weeks later the running process is what someone
// is looking at". What the key IS never appears (I6); what listening PROVES
// (a key was configured) is what gets said.
func DoctorLines(addr string, listening bool, keyEnvSet bool) []string {
	if addr == "" {
		return []string{"narrow surface   not listening — nothing binds without an explicit address, and an address without a key refuses to start (that is the design, not a limitation)"}
	}
	lines := make([]string, 0, 3)
	if listening {
		lines = append(lines, fmt.Sprintf("narrow surface   listening on %s (key configured — a keyless bind refuses to start, so the listener itself is the proof)", addr))
	} else {
		line := fmt.Sprintf("narrow surface   %s — nothing listening there", addr)
		if keyEnvSet {
			line += " (a key IS set in the environment; the daemon is not running, or runs without --http)"
		}
		lines = append(lines, line)
	}
	if host, _, err := net.SplitHostPort(addr); err == nil && !isLoopback(host) {
		lines = append(lines, fmt.Sprintf("                 note: the bind (%s) is NOT host-local — this surface is reachable from the network", host))
	}
	return lines
}

// isLoopback is the honesty check behind the doctor's note. 0.0.0.0 and ::
// are NOT loopback: they are "every interface", which is exactly what
// doctor exists to make someone notice.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Late defers the daemon connection to request time. The registry (and the
// routes it discovers) must exist BEFORE svc.Start, and the daemon's
// dispatcher exists only after it; a nil-at-registration caller would
// freeze that ordering wrong, so what is captured is the GETTER and what a
// too-early request gets is the truth, as a discriminated transport
// condition (O5) — never a panic and never a silent empty answer.
func Late(get func() Caller) Caller { return lateCaller{get: get} }

type lateCaller struct{ get func() Caller }

func (l lateCaller) Call(method protocol.MethodName, params, result any) error {
	var c Caller
	if l.get != nil {
		c = l.get()
	}
	if c == nil {
		return NewMsg(ErrTransport, "the narrow surface has no daemon behind it yet")
	}
	return c.Call(method, params, result)
}

// --- the framework service -------------------------------------------------

// HTTPService is the registered presence of the HTTP mounting: a framework
// service whose Serve does nothing (the listener belongs to `api`) and
// whose only real work is answering HTTPRoutes() when the api plugin
// discovers route contributors.
type HTTPService struct {
	mux *Mux
}

func NewHTTPService(caller Caller, table []Action) *HTTPService {
	return &HTTPService{mux: NewMux(caller, table)}
}

// Mux exposes the mounted mux — the same object HTTPRoutes() renders, so
// asserting here IS asserting what the server mounts.
func (s *HTTPService) Mux() *Mux { return s.mux }

func (s *HTTPService) Name() string                 { return ServiceName }
func (s *HTTPService) Init(_ context.Context) error { return nil }

// IsEnabled is true unconditionally, and this is NOT a listener decision:
// the enable/disable gate for HTTP is the `api` pin (Pins(): off unless an
// explicit --http+key inverted it). With api off, the orchestrator never
// asks api to Serve, and nobody ever calls HTTPRoutes(). A narrow service
// that could disable itself would add a second way to be on — N15 wants
// exactly one way.
func (s *HTTPService) IsEnabled() bool { return true }

func (s *HTTPService) Health(_ context.Context) error           { return nil }
func (s *HTTPService) HealthCheck(_ context.Context) bool       { return true }
func (s *HTTPService) Dependencies() []system_ifaces.Dependency { return nil }

// HTTPRoutes is the discovery seam the framework's api plugin pulls during
// its Serve, for registry-enabled services only. SecurityRequirement says
// `apikey`, which routes the framework's auth middleware at T2's
// TokenValidator before any handler runs — the refusal for a missing or
// wrong key therefore happens UPSTREAM of this mux, and no unauthenticated
// request reaches an action. NoPrefix mounts at the server root: the
// narrow surface speaks /v1, not /api/v1 — the prefix belongs to the
// product API this surface is not.
func (s *HTTPService) HTTPRoutes() []api_models.Route {
	entries := s.mux.Entries()
	routes := make([]api_models.Route, 0, len(entries))
	for _, e := range entries {
		routes = append(routes, api_models.Route{
			Method:  e.Method,
			Path:    e.Path,
			Handler: e.Handler,
			Description: fmt.Sprintf("%s — %s over the daemon socket (narrow surface, S19)",
				e.Action.Name, e.Action.SocketMethod),
			SecurityRequirement: &api_models.SecurityRequirement{
				SecurityLevel: webhookLevel(e.Action),
			},
			NoPrefix:   true,
			Idempotent: e.Action.Kind == KindRead,
		})
	}
	// The catch-all REFUSAL routes (T8's walk caught what T4 carried to T7
	// and T7's package tests could not see): the framework's router answers
	// an UNMOUNTED path with its own bare "404 page not found" BEFORE this
	// mux ever speaks — so the hinted, reasoned 404 (T5's sentence for the
	// question family, N14's boundary hint otherwise) was true in the
	// package and false on the wire. Mounting these shapes as routes is
	// what makes ServeHTTP's own absence answers reach callers. They are
	// NOT actions: Entries() is unchanged (the table is the boundary, and
	// a refusal is not a capability), stdio's parity is unaffected (an
	// unknown tool already redirects to the boundary there, T3).
	for _, shape := range []struct{ method, path string }{
		{http.MethodPost, "/v1/webhooks/{rest:.*}"},
		{http.MethodPost, "/v1/questions/{rest:.*}"},
		{http.MethodGet, "/v1/questions/{rest:.*}"},
		{http.MethodDelete, "/v1/questions/{rest:.*}"},
		{http.MethodPost, "/v1/loops/{rest:.*}"},
		{http.MethodGet, "/v1/loops/{rest:.*}"},
		{http.MethodDelete, "/v1/loops/{rest:.*}"},
	} {
		routes = append(routes, api_models.Route{
			Method:      shape.method,
			Path:        shape.path,
			Handler:     http.HandlerFunc(s.mux.ServeHTTP),
			Description: "narrow surface refusal — no such action (the 404 carries the reason and the alternative)",
			SecurityRequirement: &api_models.SecurityRequirement{
				SecurityLevel: corecore.ApiKeySecurityLevelApiKey,
			},
			NoPrefix: true,
		})
	}
	return routes
}

// The two seams the framework will ask about, asserted at compile time.
var (
	_ system_ifaces.Service       = (*HTTPService)(nil)
	_ api_ifaces.RouteContributor = (*HTTPService)(nil)
)

// webhookLevel is R-5 made code: the surface authenticates everything with
// the key, EXCEPT the one webhook route, whose handler authenticates by
// HMAC before anything else runs. The exception is a FUNCTION with a NAME,
// so a grep finds it and a test can pin that exactly one route uses it.
func webhookLevel(a Action) corecore.ApiKeySecurityLevel {
	if a.Kind == KindWebhook || a.Kind == KindCallback {
		// R-5 now covers TWO delivery routes, by the same argument and one
		// function: the surface authenticates everything with the key,
		// except the routes whose HANDLER authenticates by HMAC before
		// anything else runs. A peer knows its own pinned key; it has
		// never seen AEON_HTTP_KEY, and a key-401'd completion would
		// prove the wrong thing (and teach peers to give up). The
		// exemption stays a FUNCTION with a NAME so a grep finds it and a
		// test pins that exactly these two routes use it.
		return corecore.ApiKeySecurityLevelNone
	}
	return corecore.ApiKeySecurityLevelApiKey
}

// webhookParams is the delivery's ONE param fold, shared rule with stdio
// (N18): everything the daemon reads comes from params; the well-known
// sender headers are sugar that FILL a param when the query did not set
// one — never a second source that could disagree with the first.
func webhookParams(r *http.Request) (map[string]any, *Error) {
	params := map[string]any{}
	if r.URL != nil {
		for k, vs := range r.URL.Query() {
			if len(vs) > 0 {
				params[k] = vs[0]
			}
		}
	}
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			return nil, NewMsg(ErrTransport, "cannot read the request: " + err.Error())
		}
		if len(body) > maxBody {
			return nil, NewMsg(ErrRefused, fmt.Sprintf("request body over %d bytes — send a smaller body; the daemon is not asked", maxBody))
		}
	}
	// The exact bytes, string-carried (Go strings are byte-exact): the
	// HMAC must run over what the sender signed, and nothing rewrites
	// bytes between a signature and its proof.
	params["payload"] = string(body)
	for header, param := range map[string]string{
		"X-Hub-Signature-256": "signature",
		"X-Aeon-Signature":    "signature",
		"X-GitHub-Delivery":   "deliveryId",
		"X-Aeon-Delivery":     "deliveryId",
		"X-Aeon-Timestamp":    "timestamp",
		"X-GitHub-Event":      "event",
		"X-Aeon-Event":        "event",
	} {
		if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
			if _, set := params[param]; !set {
				params[param] = v
			}
		}
	}
	return params, nil
}

// webhookResponseStatus reads the httpStatus hint the delivery answer
// carries. The socket client decodes results through JSON, so the honest
// shape here is a decoded object; anything unrecognised answers 200 (the
// transport SUCCEEDED — the outcome's own words still say what happened).
func webhookResponseStatus(result any) int {
	m, ok := result.(map[string]any)
	if !ok {
		return http.StatusOK
	}
	f, ok := m["httpStatus"].(float64)
	if !ok || f < 200 || f > 599 {
		return http.StatusOK
	}
	return int(f)
}
