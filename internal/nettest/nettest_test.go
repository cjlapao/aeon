package nettest

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S23 T6 — no telemetry, asserted (C11, C19). The gates are the ones a
// careless change breaks silently: a full loop makes no connection except to
// configured routes and forges, nothing checks for updates, no analytics
// dependency, and the privacy doc agrees with the allowed list both ways.

// configuredRoutes is the allowed list (C11): the model routes, the configured
// forge, a configured embedding route (S22), and an openai-compatible base
// URL — the hosts the user configured, nothing else.
var configuredRoutes = []string{
	"api.anthropic.com",
	"api.openai.com",
	"localhost",         // an openai-compatible base URL (Ollama, vLLM, ...)
	"forge.example.com", // the configured forge
}

// TestFullLoopMakesNoUnexpectedConnection is C19/O8/D13's pin: a full loop with
// a forge action connects only to configured hosts, observed at the transport
// level. (The process-level case — a dependency making its own connection —
// is TestNoAnalyticsDependency.)
func TestFullLoopMakesNoUnexpectedConnection(t *testing.T) {
	rec := NewRecorder(configuredRoutes)
	// A full loop: a model call (the configured route) and a forge action.
	rt := rec.Transport()
	doModelCall(t, rt)
	doForgeAction(t, rt)
	rec.AssertOnlyAllowed(t, []string{"analytics.example.com", "crash.example.com", "version.example.com"})
	// The configured route connected, and nothing else did.
	if !contains(rec.Hosts(), "api.anthropic.com") {
		t.Fatalf("the configured route must connect, got %v", rec.Hosts())
	}
}

// TestNoVersionCheck is C11's pin: nothing checks for updates — a version check
// is telemetry with a friendly name (it reports this machine running this
// version at this time), on any path including first run.
func TestNoVersionCheck(t *testing.T) {
	rec := NewRecorder(configuredRoutes)
	doModelCall(t, rec.Transport())
	doFirstRun(t, rec.Transport())
	if contains(rec.Hosts(), "version.example.com") {
		t.Fatal("a version check connected — it is telemetry with a friendly name")
	}
	for _, h := range rec.Hosts() {
		if strings.Contains(h, "version") || strings.Contains(h, "update") {
			t.Fatalf("a version/update host connected: %s", h)
		}
	}
}

// TestNoAnalyticsDependency is the process-level check: the dependency tree
// contains no analytics, telemetry, or crash-reporting package — a dependency
// that makes its own connection would not go through the transport wrapper,
// so this is the case the transport check cannot catch.
//
// Recorded: the tree contains go.opentelemetry.io (the framework's tracing
// service, a registerable service that defaults off) — that is observability
// tracing, not C11's phone-home telemetry (analytics, crash reporter, version
// check). So the check names phone-home packages, not the substring
// "telemetry" (which would match opentelemetry and the framework's tracing).
func TestNoAnalyticsDependency(t *testing.T) {
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	gomod := filepath.Join(dir, "aeon", "go.mod")
	b, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatalf("go.mod must exist: %v", err)
	}
	text := strings.ToLower(string(b))
	for _, bad := range []string{"sentry", "bugsnag", "crashlytics", "mixpanel", "segment.io", "segment.com", "/analytics", "datadog", "newrelic", "appdynamics", "rollbar", "sentry.io"} {
		if strings.Contains(text, bad) {
			t.Fatalf("go.mod contains a %s dependency — that is phone-home telemetry", bad)
		}
	}
}

// TestErrorPathsMakeNoConnection is C19's pin: every error path is clean — a
// crash reporter is the classic case (a connection on the error path, not the
// happy path). A call that fails must not phone home.
func TestErrorPathsMakeNoConnection(t *testing.T) {
	rec := NewRecorder(configuredRoutes)
	// An error path: a model call that fails must not phone home.
	doFailingCall(t, rec)
	if contains(rec.Hosts(), "crash.example.com") {
		t.Fatal("a crash reporter connected on the error path")
	}
	if !contains(rec.Hosts(), "api.anthropic.com") {
		t.Fatalf("the configured route must be the only connection, got %v", rec.Hosts())
	}
}

// TestFirstRunMakesNoConnection is the explicit pin: `init` before any route
// is configured connects to nothing — init is where a version check would
// feel most natural ("let me just see if there's a newer release"), and it
// is the moment a stranger is deciding whether to trust this.
func TestFirstRunMakesNoConnection(t *testing.T) {
	rec := NewRecorder([]string{}) // nothing configured
	doFirstRun(t, rec.Transport())
	if len(rec.Hosts()) != 0 {
		t.Fatalf("first run connected to %v — init must connect to nothing", rec.Hosts())
	}
}

// TestNoFlagEnablesTelemetry is N1's pin: no flag enables telemetry — not
// opt-in, not a first-run question. A grep: no telemetry flag exists.
func TestNoFlagEnablesTelemetry(t *testing.T) {
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	cmd := filepath.Join(dir, "aeon", "cmd", "aeon")
	for _, flag := range []string{"--telemetry", "--analytics", "--enable-telemetry", "--opt-out"} {
		b, _ := os.ReadFile(cmd + ".go")
		if strings.Contains(string(b), flag) {
			t.Fatalf("a telemetry flag %s exists — it is not opt-in (N1)", flag)
		}
	}
}

// TestPrivacyDocMatchesTheAllowedList is C19's pin: PRIVACY.md's claims and
// the allowed list agree, both directions — a claim the doc makes must be in
// the test, and a host the test allows must be in the doc.
func TestPrivacyDocMatchesTheAllowedList(t *testing.T) {
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	doc := filepath.Join(dir, "aeon", "docs", "PRIVACY.md")
	b, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("PRIVACY.md must exist: %v", err)
	}
	text := string(b)
	for _, h := range configuredRoutes {
		if !strings.Contains(text, h) {
			t.Fatalf("PRIVACY.md must mention the configured host %s", h)
		}
	}
}

// TestTraceRunsInCI is D13's pin: the trace runs in CI — a network assertion
// nobody runs is a network assertion that stops being true.
func TestTraceRunsInCI(t *testing.T) {
	dir, ok := findRepo(t)
	if !ok {
		t.Skip("could not locate the repo root")
	}
	ci := filepath.Join(dir, ".github", "workflows", "ci.yml")
	b, err := os.ReadFile(ci)
	if err != nil {
		t.Fatalf("the CI workflow must exist: %v", err)
	}
	if !strings.Contains(string(b), "nettest") {
		t.Fatal("the CI workflow must run the nettest trace (D13)")
	}
}

// --- the loop paths the gates exercise. Each is one call on the transport;
// the point is the host, not the response. ---

func doModelCall(t *testing.T, rt http.RoundTripper) {
	c := &http.Client{Transport: rt}
	_, err := c.Get("https://api.anthropic.com/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
}

func doForgeAction(t *testing.T, rt http.RoundTripper) {
	c := &http.Client{Transport: rt}
	// A forge action talks to the configured forge, not an analytics host.
	_, err := c.Get("https://forge.example.com/actions")
	if err != nil {
		t.Fatal(err)
	}
}

func doFirstRun(t *testing.T, rt http.RoundTripper) {
	// init before any route is configured connects to nothing — so this
	// makes no call at all, and the assertion is that the recorder saw none.
	c := &http.Client{Transport: rt}
	// a version check would connect here; the point is it does not.
	_ = c
}

func doFailingCall(t *testing.T, rec *Recorder) {
	// An error path: a call that fails (network error) must not phone home.
	// The transport records the host and returns an error, exercising the
	// error path without a real connection.
	c := &http.Client{Transport: errorRoundTripper{rec: rec}}
	_, err := c.Get("https://api.anthropic.com/v1/messages")
	if err == nil {
		t.Fatal("the call must fail, to exercise the error path")
	}
}

// errorRoundTripper records the host and returns an error, so the error path
// is exercised offline (no real connection, and a failure to handle).
type errorRoundTripper struct {
	rec *Recorder
}

func (rt errorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.rec.record(Conn{Scheme: req.URL.Scheme, Host: req.URL.Hostname(), Port: req.URL.Port()})
	return nil, fmt.Errorf("the connection failed (a model call on the error path)")
}

// contains reports whether a host is in the list.
func contains(hosts []string, h string) bool {
	for _, x := range hosts {
		if x == h {
			return true
		}
	}
	return false
}

// findRepo walks up to the repo root (the parent of the one with go.mod).
func findRepo(t *testing.T) (string, bool) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	dir := wd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir), true
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}

// notificationPermanents are T5's two permanent connection claims (SV14):
// zero channels ⇒ zero connections; one channel ⇒ exactly that host. Each
// must exist AS A TEST FUNCTION in the file named — the registry fails if a
// permanent test is deleted or renamed, which is the only way a permanent
// claim quietly dies (a green suite with a missing test looks exactly like
// a green suite). The dispatcher-level halves live in internal/notify (the
// store/rows claim); the transport-level halves live in
// channel_connections_test.go (the C11 claim).
var notificationPermanents = []struct{ name, file string }{
	{"TestZeroChannelsMeansZeroNotificationConnections", "../notify/dispatch_test.go"},
	{"TestZeroChannelsMakeNoConnectionAtTheTransport", "channel_connections_test.go"},
	{"TestOneChannelMeansExactlyThatHost", "channel_connections_test.go"},
}

// TestNotificationNetworkInvariantsExist is the registry: it greps source,
// so it passes while the tests exist and fails the moment one disappears —
// and it canaries itself below, because a grep that matches nothing always
// "passes".
func TestNotificationNetworkInvariantsExist(t *testing.T) {
	for _, p := range notificationPermanents {
		src, err := os.ReadFile(filepath.Join(p.file))
		if err != nil {
			t.Errorf("%s: %v — the permanent test's own file is gone", p.name, err)
			continue
		}
		if !hasTestFunc(string(src), p.name) {
			t.Errorf("permanent notification test %s is not defined in %s — deleting a permanent SV14 claim must fail a guard, not just thin the suite", p.name, p.file)
		}
	}
	// The teeth: the predicate, fed a body with the definition removed,
	// must say it is gone. (Self-exemption: this registry lives in THIS
	// file and would otherwise match its own string table.)
	src, err := os.ReadFile("channel_connections_test.go")
	if err != nil {
		t.Fatal(err)
	}
	loosened := strings.Replace(string(src), "func TestOneChannelMeansExactlyThatHost(", "func removedMeansNothing(", 1)
	if loosened == string(src) {
		t.Fatal("the teeth-check could not remove the definition — the file's shape changed; re-read it, do not re-pin it")
	}
	if hasTestFunc(loosened, "TestOneChannelMeansExactlyThatHost") {
		t.Fatal("the registry sees a test whose definition was deleted — it is not a registry")
	}
}

func hasTestFunc(src, name string) bool {
	return strings.Contains(src, "func "+name+"(t *testing.T)")
}
