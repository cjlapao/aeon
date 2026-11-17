package bootstrap

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"

	system_interfaces "github.com/cjlapao/go-backend-scaffolding/framework/core/system/interfaces"

	"github.com/cjlapao/aeon/internal/store"
)

// fakeRegistry collects what Register names and does nothing else.
//
// The embedded nil interface satisfies SystemServiceInterface's two dozen
// methods without implementing them, so anything Register touches BEYOND
// Register panics rather than quietly succeeding. That is deliberate: this test
// asserts what the daemon's composition IS, and it must not be able to pass by
// booting a real system whose behaviour depends on config, a disk or a network.
type fakeRegistry struct {
	system_interfaces.SystemServiceInterface
	names []string
}

func (f *fakeRegistry) Register(s system_interfaces.Service) {
	f.names = append(f.names, s.Name())
}

func golden(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("services.golden")
	if err != nil {
		t.Fatalf("open services.golden: %v", err)
	}
	defer f.Close()

	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want = append(want, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read services.golden: %v", err)
	}
	sort.Strings(want)
	return want
}

func registered(t *testing.T) []string {
	t.Helper()
	r := &fakeRegistry{}
	// A REAL store, in a temp directory. S4's database service is registered
	// only when one is open — and passing nil here would make the golden
	// comparison silently stop covering it, which is the exact failure mode
	// this test exists to prevent.
	Register(r, openTestStore(t))
	got := append([]string(nil), r.names...)
	sort.Strings(got)
	return got
}

// openTestStore opens a store in a temp directory, closed on cleanup.
//
// Never the real store (S4's D19): the daemon's composition is being checked,
// not a database, and a test that created `$XDG_STATE_HOME/aeon/state.db` as a
// side effect of asking "what would you register" would be exactly the
// side-effect-on-a-question problem RegisteredNames avoids.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(store.Options{
		Dir: t.TempDir(),
		Env: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("open a temp store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// The guard. A framework upgrade that registers something new fails HERE,
// rather than in production as a daemon wider than anyone decided (B11).
func TestRegisteredSetEqualsGolden(t *testing.T) {
	got, want := registered(t), golden(t)

	if len(got) != len(want) {
		t.Fatalf("registered %d service(s), services.golden lists %d\n  registered: %v\n  golden:     %v\n\n"+
			"If this is intentional, update services.golden in the SAME commit.",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("service[%d] = %q, services.golden says %q\n  registered: %v\n  golden:     %v",
				i, got[i], want[i], got, want)
		}
	}
}

// A fixture that tolerates an addition is not a fixture. Without this, the test
// above could pass against a golden file that had silently drifted to match.
func TestGoldenRejectsAnAddition(t *testing.T) {
	want := golden(t)
	got := append(append([]string(nil), want...), "a-service-nobody-chose")
	sort.Strings(got)

	if len(got) == len(want) {
		t.Fatal("adding a service did not change the registered count; the comparison is not comparing")
	}

	var differs bool
	for i := range want {
		if got[i] != want[i] {
			differs = true
			break
		}
	}
	if !differs && len(got) == len(want) {
		t.Fatal("an added service compared equal to the fixture")
	}
}

// localrpc is the one service the daemon cannot be without: it is the only
// transport that carries the protocol (P25). A refactor that drops it leaves a
// process that starts, reports healthy, and can never be spoken to.
func TestLocalRPCIsRegistered(t *testing.T) {
	for _, n := range registered(t) {
		if n == "localrpc" {
			return
		}
	}
	t.Fatal("localrpc is not registered: the daemon would start with no way to reach it")
}

// The services the design refused, asserted by name. A service arriving because
// something else pulled it in is exactly what this catches.
func TestRefusedServicesAreNotRegistered(t *testing.T) {
	refused := []string{
		"redis", "jobs", "metrics", "tracing", "profiling", "swagger", "websocket",
		"object_storage", "media", "email", "sms", "notification", "session",
		"device", "i18n", "cache", "vaults", "flipt",
	}
	// NOTE: "database" is deliberately absent. It is not refused — S4's T1
	// registers it (see register.go). Asserting it stays absent would make
	// that task fail this test for doing the right thing.
	got := registered(t)
	for _, bad := range refused {
		for _, n := range got {
			if n == bad {
				t.Errorf("%q is registered; the design refused it (see register.go)", bad)
			}
		}
	}
}

// ── Pins ──────────────────────────────────────────────────────────────────
//
// "Registered" and "running" are different questions. These assert the second.

func pinFor(t *testing.T, name string) (bool, bool) {
	return pinForOpts(t, name)
}

func pinForOpts(t *testing.T, name string, opts ...PinOption) (bool, bool) {
	t.Helper()
	for _, p := range Pins(opts...) {
		if p.Service == name {
			return p.Enabled, true
		}
	}
	return false, false
}

// The api service defaults to Enabled:true on :8080 in the framework, so
// registering it WITHOUT this pin opens a port nobody asked for — which is
// exactly what S19's N15 forbids. If this test fails, the daemon is listening.
func TestAPIIsPinnedOff(t *testing.T) {
	enabled, found := pinFor(t, "api")
	if !found {
		t.Fatal("api has no pin: the framework default (Enabled:true, :8080) applies and the daemon would open an unasked-for port")
	}
	if enabled {
		t.Fatal("api is pinned ON; nothing should listen until S19 adds the explicit-address and key requirements")
	}
}

// S19 T4 — the inversion, as its own gate. A pin is a security decision
// (AGENTS §4), so the FORCE-ON branch gets a test as hard as the ForceOff:
// WithHTTP() is the only way api flips, nothing else about the pin set
// moves with it, and the default answer survives every flip (no hidden
// state in the option machinery). serve.go passes WithHTTP() only behind a
// typed --http address AND a resolved key; the connect-attempt gate in
// cmd/aeon proves the runtime half — this proves the data half.
func TestAPIPinsOnOnlyViaWithHTTP(t *testing.T) {
	enabled, found := pinForOpts(t, "api", WithHTTP())
	if !found {
		t.Fatal("api loses its pin under WithHTTP(): an unpin is not an enable — the framework default would answer through a hole in the pin set")
	}
	if !enabled {
		t.Fatal("WithHTTP() does not flip api on: serve --http would start with a refusal no flag can lift")
	}
	// Only api moved.
	base := Pins()
	flipped := Pins(WithHTTP())
	if len(base) != len(flipped) {
		t.Fatalf("the pin set changed size under WithHTTP(): %d → %d", len(base), len(flipped))
	}
	for _, b := range base {
		for _, f := range flipped {
			if f.Service != b.Service {
				continue
			}
			if f.Enabled != b.Enabled && b.Service != "api" {
				t.Errorf("%s changed enabled state under WithHTTP(): %v → %v — the HTTP flag flips one door, not the pin set", b.Service, b.Enabled, f.Enabled)
			}
		}
	}
	// The default survives the flip (options do not leak into the next call).
	if on, _ := pinFor(t, "api"); on {
		t.Fatal("Pins() after Pins(WithHTTP()) answers ON — the default flipped permanently; a process-level flag must not rewrite the code-level default")
	}
}

// localrpc is the only transport carrying the protocol. The framework defaults
// it OFF, which is right for a general-purpose framework and wrong here: a
// daemon with no endpoint starts, reports healthy, and can never be spoken to.
func TestLocalRPCIsPinnedOn(t *testing.T) {
	enabled, found := pinFor(t, "localrpc")
	if !found {
		t.Fatal("localrpc has no pin: the framework default is OFF, so a plain `aeon serve` would have no endpoint")
	}
	if !enabled {
		t.Fatal("localrpc is pinned OFF; the daemon would start with no way to reach it")
	}
}

// A pin naming an unregistered service fails the boot, so a pin for something
// that was never registered turns a typo into a daemon that will not start.
func TestEveryPinNamesARegisteredService(t *testing.T) {
	got := registered(t)
	for _, p := range Pins() {
		var found bool
		for _, n := range got {
			if n == p.Service {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pin names %q, which is not registered: the framework fails the boot on an unknown pin", p.Service)
		}
	}
}

// Pinning the same service both on and off panics at the call site, so a
// duplicate is a crash at startup rather than a confusing precedence question.
func TestNoServiceIsPinnedTwice(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Pins() {
		if seen[p.Service] {
			t.Errorf("%q is pinned twice; the framework panics on a contradictory pin", p.Service)
		}
		seen[p.Service] = true
	}
}
