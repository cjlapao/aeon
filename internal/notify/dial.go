package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// dial.go — S58 T5 (SV8): the SSRF-safe transport. A channel is standing
// authority to make the daemon connect somewhere; this file is why that
// authority cannot be pointed at the machine's own plumbing or at a cloud
// metadata service. The rule is one sentence: the check runs on the
// DIALLED IP — every address the name resolves to, at dial time — so
// DNS rebinding and `http://127.0.0.1.evil` fail the same way, and
// whatever validate-time resolution said is irrelevant.
//
// Two more refusals belong to the same sentence:
//   - no redirect is followed (`ErrUseLastResponse`): a 302 to an
//     internal host is the classic way around an IP filter, so the
//     dispatcher records the status and stops;
//   - the environment proxy is IGNORED (`Proxy: nil`): HTTPS_PROXY must
//     not be able to reroute what a person configured host-by-host.

// NoRedirects is the http.Client redirect policy for channels: a 302 to
// an internal host is the classic way around an IP filter, so the last
// response stands and the dispatcher records its status. It belongs to
// the Client (that is where net/http keeps the policy); it travels with
// the transport's intent and the dispatcher pairs them.
func NoRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// RefusedAddressError names the closed refusal: the dialled address is
// one a channel may not contact. The dispatcher maps it to
// error_kind `refused-address` — the outcome is logged honestly, and the
// address the check refused is NOT in the sentence beyond the IP already
// being public knowledge of a URL the operator typed.
type RefusedAddressError struct {
	IP     string
	Reason string // loopback | unspecified | link-local or multicast | cloud metadata | private or CGNAT
}

func (e *RefusedAddressError) Error() string {
	return fmt.Sprintf("notify: refused to dial %s (%s) — not a public destination (a channel may not reach loopback, private, link-local, CGNAT, multicast or metadata addresses; allow_private admits loopback and private, never the rest)", e.IP, e.Reason)
}

// LookupHost is the DNS seam (net.Resolver.LookupHost's shape). Tests
// rebind; production passes net.DefaultResolver.
type LookupHost func(ctx context.Context, host string) ([]string, error)

// GuardedTransport builds the http.Transport for one channel policy:
//
//   - DialContext resolves the URL host itself and refuses ANY resolved
//     address the policy forbids before dialling — it then dials the
//     checked IP directly, so no second resolution can slip between
//     check and connect (that is the rebinding proof);
//   - TLS >= 1.2, keep-alives off (every delivery is its own
//     connection: no pooled socket sneaks past the per-dial check),
//     no redirect, no proxy.
func GuardedTransport(allowPrivate bool, lookup LookupHost) *http.Transport {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	t := &http.Transport{
		Proxy:               nil, // the environment does not route a person's channels
		DisableKeepAlives:   true,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   false,
		MaxIdleConnsPerHost: 0,
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		addrs, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, as := range addrs {
			ip := net.ParseIP(as)
			if ip == nil {
				lastErr = fmt.Errorf("notify: resolver answered %q with unparseable address %q", host, as)
				continue
			}
			if refused, why := refuseIP(ip, allowPrivate); refused {
				// A refused address is FINAL, not "try the next one": a
				// name that resolves to both a public and an internal IP
				// and gets the public one is a rebinding race waiting to
				// lose. The whole name is refused.
				return nil, &RefusedAddressError{IP: as, Reason: why}
			}
			d := net.Dialer{}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(as, port))
			if err != nil {
				lastErr = err
				continue
			}
			return conn, nil
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("notify: %q resolved to nothing", host)
		}
		return nil, lastErr
	}
	return t
}

// refuseIP is the closed address verdict. allowPrivate admits the PRIVATE
// FAMILY (loopback, RFC1918, ULA, CGNAT — the shapes a person legitimately
// runs a local receiver on); everything that is not a destination —
// link-local (every 169.254 address, which is where the metadata services
// live), multicast, the unspecified addresses, and the named IPv6
// metadata address — is refused even with allowPrivate.
func refuseIP(ip net.IP, allowPrivate bool) (bool, string) {
	if len(ip) == 16 && ip.To4() != nil {
		// An IPv4-mapped IPv6 address (::ffff:a.b.c.d) must not smuggle
		// 127.0.0.1 past the v6 branches: collapse to v4 first.
		ip = ip.To4()
	}
	switch {
	case ip.Equal(net.IPv6loopback) || ip.IsLoopback():
		if allowPrivate {
			return false, ""
		}
		return true, "loopback"
	case ip.IsUnspecified():
		return true, "unspecified" // 0.0.0.0 and :: are routes, not hosts
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast():
		return true, "link-local or multicast" // 169.254.169.254 lives here
	case strings.EqualFold(ip.String(), "fd00:ec2::254"):
		return true, "cloud metadata"
	case ip.IsPrivate() || isCGNAT(ip):
		if allowPrivate {
			return false, ""
		}
		return true, "private or CGNAT"
	default:
		return false, ""
	}
}

// cgnat is 100.64.0.0/10 — carrier NAT: shared infrastructure, not a
// destination anyone configured a webhook for.
var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func isCGNAT(ip net.IP) bool { return ip != nil && ip.To4() != nil && cgnat.Contains(ip) }

// IsRefusedAddress is the dispatcher's mapping predicate.
func IsRefusedAddress(err error) bool {
	var r *RefusedAddressError
	return errors.As(err, &r)
}
