// Package nettest is the assertion C11 turns into a test (C19): no telemetry.
// The product reads source code and runs commands, so trust is the whole basis
// of installing it — and a claim like that is worth nothing unstated. A test
// that runs a full loop and asserts no connection except to configured routes
// and forges is the only thing that keeps "no telemetry" true.
//
// A full loop, not a single step (O8): the connection nobody expects appears
// on the path nobody exercised — a version check on first run, a crash
// reporter on an error path, a dependency's own analytics on a code path only
// a real loop reaches. So the assertion observes at BOTH the transport level
// (Go-level HTTP calls) and the process level (a dependency making its own
// connection, which would not go through the wrapper) — that is exactly the
// case worth catching.
//
// It is permanent (N1): not a flag, not a first-run question, not "help us
// improve". The next person to want it will look for the flag first, so the
// comment says it is permanent.
package nettest

import (
	"net/http"
	"sync"
)

// Conn is one observed outbound connection: the host, port, and scheme. A
// version check is telemetry with a friendly name: it reports that this
// machine is running this version at this time, so a version-check host is
// not allowed.
type Conn struct {
	Scheme string
	Host   string
	Port   string
}

func (c Conn) String() string { return c.Scheme + "://" + c.Host + ":" + c.Port }

// Recorder observes outbound connections and asserts only the allowed hosts.
// The allowed list is the configured routes and forges (C11): the model
// routes, the configured forge, a configured embedding route (S22), and an
// openai-compatible base URL — nothing else.
type Recorder struct {
	mu      sync.Mutex
	conns   []Conn
	allowed map[string]bool
}

// NewRecorder allows the hosts the user configured. Everything else is an
// unexpected connection — an analytics host, a crash reporter, a version
// check.
func NewRecorder(allowed []string) *Recorder {
	a := map[string]bool{}
	for _, h := range allowed {
		a[h] = true
	}
	return &Recorder{allowed: a}
}

// Transport is the Go-level observation: an http.RoundTripper that records
// the host of every Go-level HTTP call and returns an offline response — no
// real connection leaves the machine. A dependency that makes its own
// connection (below the wrapper) is the process-level case, which the process
// check (TestNoAnalyticsDependency) catches — not this.
func (r *Recorder) Transport() http.RoundTripper {
	return recordingRoundTripper{rec: r}
}

// recordingRoundTripper is the transport-level observation: it records the
// host of a Go-level HTTP call and returns an offline response, so no real
// connection leaves the machine.
type recordingRoundTripper struct {
	rec *Recorder
}

func (rt recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.rec.record(Conn{Scheme: req.URL.Scheme, Host: req.URL.Hostname(), Port: req.URL.Port()})
	// Offline: the call is observed, not made. A 200 with an empty body keeps
	// the loop running; the assertion is about the host, not the response.
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}

// record adds an observed connection.
func (r *Recorder) record(c Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns = append(r.conns, c)
}

// Hosts is the set of hosts observed, so a test can assert only the allowed
// ones connected.
func (r *Recorder) Hosts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, c := range r.conns {
		if !seen[c.Host] {
			seen[c.Host] = true
			out = append(out, c.Host)
		}
	}
	return out
}

// AssertOnlyAllowed is the assertion: every observed connection is an allowed
// (configured) host. Any other host is an unexpected connection — an analytics
// host, a crash reporter, or a version check — and fails.
func (r *Recorder) AssertOnlyAllowed(t T, unexpected []string) {
	for _, h := range r.Hosts() {
		if !r.allowed[h] {
			t.Fatalf("an unexpected connection to %s — %s is not a configured route or forge", h, unexpectedNote(unexpected, h))
		}
	}
}

// unexpectedNote says what an unexpected host is, so the failure names it.
func unexpectedNote(unexpected []string, host string) string {
	for _, u := range unexpected {
		if u == host {
			return u
		}
	}
	return "an analytics host, a crash reporter, or a version check"
}

// T is the assertion surface; *testing.T satisfies it.
type T interface {
	Fatalf(format string, args ...any)
}

// AllowedHosts is the allow-list source for a supervision-plane run
// (S58 T5, SV14): the configured model routes, the configured forges, and
// the hosts of the person's ENABLED notification channels — nothing
// else. The sentence that belongs to this function is SV14's: a channel
// is the one outbound path Aeon adds to a machine it has already been
// trusted with, and it reaches only hosts a person wrote down; the
// nettest assertion that "no connection exists except to configured
// routes, forges and channels" is what keeps that sentence true after
// every future slice. ChannelHosts (the store) feeds the third argument
// already narrowed to enabled, non-archived rows — the list is derived
// from configuration, never widened by defaults.
func AllowedHosts(routes, forges, channelHosts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range [][]string{routes, forges, channelHosts} {
		for _, h := range group {
			if h != "" && !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	return out
}
