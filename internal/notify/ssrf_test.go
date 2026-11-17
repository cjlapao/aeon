package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ssrf_test.go — S58 T5 (SV14): the dial is the security boundary. DNS
// happens INSIDE the guarded dialer and the checked IP is what gets
// dialled — so "it resolved public once and private next" (a rebind) has
// no window to slip through: every dial re-checks the answer it is
// about to use.

func TestSSRFDialRefusesPrivateLinkLocalMetadata(t *testing.T) {
	refused := []string{
		"127.0.0.1", "127.0.0.53", "10.0.0.5", "172.16.9.9", "192.168.1.1",
		"169.254.169.254",       // the cloud metadata address
		"0.0.0.0", "100.64.0.1", // CGNAT
		"::1", "fc00::1", "fd12:3456::7",
		"fd00:ec2::254",    // the IMDSv4-ish address, refused even allow_private
		"::ffff:127.0.0.1", // IPv4-mapped loopback — the classic bypass
		"::ffff:169.254.169.254",
	}
	for _, ip := range refused {
		tr := GuardedTransport(false, fixedLookup(ip))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := tr.DialContext(ctx, "tcp", "victim.example:443")
		cancel()
		var ra *RefusedAddressError
		if !errors.As(err, &ra) {
			t.Errorf("%s: dial error %v, want a refused-address refusal", ip, err)
			continue
		}
		if !strings.Contains(ra.Error(), ip) {
			t.Errorf("%s: refusal %q does not name the address it refused", ip, ra.Error())
		}
	}
}

func TestPrivateAddressesNeedTheAllowPrivateFlag(t *testing.T) {
	// Loopback and private are NOT categorically refused — the flag is
	// what admits them (a local receiver is a legitimate choice a person
	// made, and netplan-style private receivers are how self-hosted
	// webhooks work). What never gets admitted is the metadata family.
	for _, ip := range []string{"127.0.0.1", "10.0.0.5"} {
		tr := GuardedTransport(true, fixedLookup(ip))
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := tr.DialContext(ctx, "tcp", "local.example:9")
		cancel()
		var ra *RefusedAddressError
		if errors.As(err, &ra) {
			t.Errorf("%s with allow_private: refused (%v) — the flag means admitted", ip, err)
		}
		// (A connection error to a dead port is the PASSING case here:
		// the check said yes and the dial went.)
	}
	// Link-local is NOT private-family — it stays refused with the flag.
	tr := GuardedTransport(true, fixedLookup("169.254.169.254"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err := tr.DialContext(ctx, "tcp", "meta.example:80")
	cancel()
	var ra *RefusedAddressError
	if !errors.As(err, &ra) {
		t.Errorf("link-local with allow_private accepted (%v)", err)
	}
}

func TestSSRFRefusesAfterDNSRebind(t *testing.T) {
	// The rebind: the name answers PUBLIC to the first question and
	// PRIVATE to the second. Because the guard resolves inside the
	// dialer and dials the answer it just checked, the second dial meets
	// the private answer and refuses — there is no TOCTOU window between
	// a checked resolution and a separately-issued dial.
	var mu sync.Mutex
	n := 0
	rebinding := func(ctx context.Context, host string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return []string{"93.184.216.34"}, nil // public: the decoy
		}
		return []string{"169.254.169.254"}, nil // private on re-question
	}
	tr := GuardedTransport(false, LookupHost(rebinding))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, first := tr.DialContext(ctx, "tcp", "attacker.example:443")
	cancel()
	var ra *RefusedAddressError
	if errors.As(first, &ra) {
		t.Fatal("the first (public) dial was refused — the guard cannot tell names from intentions")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	_, second := tr.DialContext(ctx, "tcp", "attacker.example:443")
	cancel()
	if !errors.As(second, &ra) {
		t.Fatalf("the REBIND dial was not refused: %v", second)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var hits []string
	var mu sync.Mutex
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/first" {
			w.Header().Set("Location", srv.URL+"/second")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	// Resolve the server's own host to loopback through the seam so the
	// guarded (public-only) client can even be pointed at it.
	client := &http.Client{
		Transport:     GuardedTransport(true, nil),
		CheckRedirect: NoRedirects,
		Timeout:       5 * time.Second,
	}
	resp, err := client.Post(srv.URL+"/first", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status %d — the client followed the redirect instead of surfacing it", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hits {
		if h == "/second" {
			t.Fatal("/second was reached — a 3xx is one hop, and the hop is the guard's blind spot if we follow")
		}
	}
}

func TestProxyEnvIgnored(t *testing.T) {
	// A transport with Proxy:nil never consults the environment. This is
	// NOT cosmetic: HTTP_PROXY would send the signed envelope (and the
	// delivery id, and the slugs) through a third machine that the
	// person never configured — an SSRF guard with a proxy hole is no
	// guard.
	var proxyHit bool
	var mu sync.Mutex
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proxyHit = true
		mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	client := &http.Client{
		Transport:     GuardedTransport(true, nil),
		CheckRedirect: NoRedirects,
		Timeout:       5 * time.Second,
	}
	resp, err := client.Post(target.URL+"/hook", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if proxyHit {
		t.Fatal("the proxy saw the request — Proxy:nil must mean nil")
	}
}

func TestTLSOnlyAndMinimumVersion(t *testing.T) {
	tr := GuardedTransport(false, nil)
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Errorf("TLS floor %v, want >= TLS 1.2", tr.TLSClientConfig)
	}
	if tr.Proxy != nil {
		t.Error("Proxy is set on the guarded transport")
	}
	if !tr.DisableKeepAlives {
		t.Error("keep-alives enabled: a channel connection pooling to a receiver is a second, unguarded outbound path")
	}
}

// fixedLookup answers every name with one literal IP (the DNS seam).
func fixedLookup(ip string) LookupHost {
	return func(ctx context.Context, host string) ([]string, error) {
		if net.ParseIP(ip) == nil {
			return nil, errors.New("not an ip")
		}
		return []string{ip}, nil
	}
}
