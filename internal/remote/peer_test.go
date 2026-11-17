package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// goodPin is the shape a configured pin has, lowercase hex, 64 chars.
const goodPin = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"

func goodPeer() Peer {
	return Peer{
		ID:      "peer-alpha",
		Address: "peer.example:8443",
		Pin:     &Pin{SHA256: goodPin, Kind: PinCertificate},
		Key:     &KeyRef{Env: "AEON_TEST_PEER_KEY"},
	}
}

// TestNothingIsDiscovered is the grep test D4 asks for, run over THIS file's
// own bytes: no well-known path, no mDNS, no SRV, no registry, no HTTP
// client, no dial — dialing is T3's file (T1's Validation list names both).
// A banned token in a comment is banned too: the sentence that says "we do
// not crawl /.well-known" is one refactor away from the code that crawls it.
func TestNothingIsDiscovered(t *testing.T) {
	raw, err := os.ReadFile("peer.go")
	if err != nil {
		t.Fatalf("reading peer.go: %v", err)
	}
	src := string(raw)
	banned := []string{
		"well-known", "mdns", "MDNS", "mDNS", "SRV", "srv", "registry", "Registry",
		"net/http", "http.Get", "http.Post", "Dial(", "dns.", "Lookup",
	}
	for _, b := range banned {
		if strings.Contains(src, b) {
			t.Errorf("peer.go contains banned token %q — a peer is configured, never discovered (RA3), and this file builds no client at all", b)
		}
	}
	// The injectable seam and the strict decoder are allowed; a live client
	// is not. State the positive too, so an over-eager ban list is caught.
	if !strings.Contains(src, "DisallowUnknownFields") {
		t.Error("peer.go lost DisallowUnknownFields — the strict shape is what refuses a smuggled key field (C17)")
	}
}

// TestFreshInstallReachesNothing is D13: the shipped default reaches nothing,
// and the empty state is STATED — not an empty list the caller must interpret.
func TestFreshInstallReachesNothing(t *testing.T) {
	var c Config
	if len(c.Peers) != 0 {
		t.Fatalf("the zero value carries %d peers — peers are off by default (RA9)", len(c.Peers))
	}
	want := "nothing outbound is reachable — remote peers are off by default, and enabling one takes an explicit address, a pinned identity and a key (RA9)"
	if got := c.OutboundState(); got != want {
		t.Errorf("OutboundState() =\n%q\nwant (the state stated, not implied):\n%q", got, want)
	}
}

// TestPeerWithoutPinRefusedAtConfig is the configuration-time door: a peer
// missing its pin never reaches a dial, because it never leaves Validate.
func TestPeerWithoutPinRefusedAtConfig(t *testing.T) {
	p := goodPeer()
	p.Pin = nil
	c := Config{Peers: []Peer{p}}
	err := c.Validate()
	if err == nil {
		t.Fatal("an unpinned peer passed Validate — the pin is required (RA2), refused HERE not at dial time")
	}
	if !strings.Contains(err.Error(), "no pinned identity") {
		t.Errorf("refusal %q does not name what is missing", err)
	}
}

// TestTheUnpinnedRefusalExplainsWhyTheCASetIsInsufficient keeps the refusal
// from reading as bureaucracy: "add a pin" with no reason is a sentence an
// operator cannot argue with, so the refusal carries the argument (RA2).
func TestTheUnpinnedRefusalExplainsWhyTheCASetIsInsufficient(t *testing.T) {
	p := goodPeer()
	p.Pin = nil
	err := Config{Peers: []Peer{p}}.Validate()
	if err == nil {
		t.Fatal("want the refusal")
	}
	for _, phrase := range []string{"system CA set", "proves TLS, not identity", "this is the peer we consented to"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("refusal missing %q — the refusal must explain, not just refuse:\n%v", phrase, err)
		}
	}
}

// TestAKeyIsNeverReadFromArgvOrAConfigFile is C17 as shape, not manners:
// the config decoder is strict, so literal key bytes under ANY new field
// name are refused at the door; and no type in this file can hold them.
func TestAKeyIsNeverReadFromArgvOrAConfigFile(t *testing.T) {
	// A config that tries to hand bytes inline is refused by name.
	for _, bad := range []string{
		`{"peers":[{"id":"a","address":"h:1","pin":{"sha256":"` + goodPin + `","kind":"certificate"},"key":{"literal":"SECRET"}}]}`,
		`{"peers":[{"id":"a","address":"h:1","pin":{"sha256":"` + goodPin + `","kind":"certificate"},"key":{"env":"E","path":"/p","value":"SECRET"}}]}`,
		`{"peers":[{"id":"a","address":"h:1","pin":{"sha256":"` + goodPin + `","kind":"certificate"},"key":"SECRET"}]}`,
		`{"peers":[],"argvKey":"SECRET"}`,
	} {
		if _, err := parseConfig([]byte(bad)); err == nil {
			t.Errorf("smuggled key bytes accepted by the strict decoder: %s", bad)
		}
	}
	// And structurally: no field of Peer, Pin or KeyRef holds bytes — every
	// leaf is a string, so there is nowhere a secret could be *stored*.
	for _, typ := range []reflect.Type{reflect.TypeOf(Peer{}), reflect.TypeOf(Pin{}), reflect.TypeOf(KeyRef{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8 {
				t.Errorf("%s.%s is []byte — key bytes must never be a stored field (C17)", typ.Name(), f.Name)
			}
		}
	}
}

// TestKeyRefHasExactlyOneSource: the environment or a path. Neither is
// nothing-to-load; both is an undefined winner.
func TestKeyRefHasExactlyOneSource(t *testing.T) {
	for name, k := range map[string]*KeyRef{
		"neither":   {},
		"both":      {Env: "E", Path: "/p"},
		"empty-env": {Env: "", Path: ""},
	} {
		p := goodPeer()
		p.Key = k
		if err := (Config{Peers: []Peer{p}}).Validate(); err == nil {
			t.Errorf("%s: KeyRef accepted — exactly one of env or path, never both, never neither", name)
		}
	}
}

func TestLoadKeyFromEnvAndPath(t *testing.T) {
	t.Setenv("AEON_TEST_PEER_KEY", "the-key-bytes")
	k := &KeyRef{Env: "AEON_TEST_PEER_KEY"}
	got, err := k.LoadKey()
	if err != nil || string(got) != "the-key-bytes" {
		t.Fatalf("LoadKey(env) = %q, %v", got, err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "peer.key")
	if err := os.WriteFile(path, []byte("bytes-from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	kp := &KeyRef{Path: path}
	got, err = kp.LoadKey()
	if err != nil || string(got) != "bytes-from-file" {
		t.Fatalf("LoadKey(path) = %q, %v", got, err)
	}
	// The two missing sources refuse with sentences naming WHICH one, so the
	// operator debugs the right one ("key not found" across two sources is a
	// coin flip).
	k.Env = "AEON_TEST_PEER_UNSET"
	_, err = k.LoadKey()
	if err == nil || !strings.Contains(err.Error(), "AEON_TEST_PEER_UNSET") {
		t.Errorf("missing-env refusal must name the variable: %v", err)
	}
	kp.Path = filepath.Join(dir, "absent.key")
	_, err = kp.LoadKey()
	if err == nil || !strings.Contains(err.Error(), "absent.key") {
		t.Errorf("missing-path refusal must name the path: %v", err)
	}
	for _, s := range []string{err.Error(), func() string { _, e := (&KeyRef{Env: "AEON_TEST_PEER_UNSET"}).LoadKey(); return e.Error() }()} {
		if !strings.Contains(s, "never argv") || !strings.Contains(s, "never a config file") {
			t.Errorf("key refusal must restate C17 so the fix is not 'put it in the config': %s", s)
		}
	}
}

// TestRedactionNeverPrintsTheKey: %v and %#v of anything config-shaped print
// the REFERENCE only. The bytes exist in the process only after LoadKey, and
// never in a value a format verb can reach.
func TestRedactionNeverPrintsTheKey(t *testing.T) {
	const secret = "SUPER-SECRET-KEY-BYTES"
	t.Setenv("AEON_TEST_PEER_KEY", secret)
	p := goodPeer()
	for _, form := range []string{
		fmt.Sprintf("%v", p),
		fmt.Sprintf("%#v", p),
		fmt.Sprintf("%v", p.Key),
		fmt.Sprintf("%#v", p.Key),
		fmt.Sprintf("%v", Config{Peers: []Peer{p}}),
		fmt.Sprintf("%v", p.Key),
	} {
		if strings.Contains(form, secret) {
			t.Errorf("a format verb reached the key bytes: %s", form)
		}
	}
	if got := fmt.Sprintf("%v", p.Key); !strings.Contains(got, "key:env:AEON_TEST_PEER_KEY") {
		t.Errorf("the reference should still print usefully, got %q", got)
	}
}

// TestPlaintextSchemeRefusedByName is D1 at the config door: there is no
// plaintext mode to reach, and the refusal says so — https:// is refused too,
// because the scheme belongs to the transport (RA1).
func TestPlaintextSchemeRefusedByName(t *testing.T) {
	p := goodPeer()
	p.Address = "http://peer.example:8080"
	err := (Config{Peers: []Peer{p}}).Validate()
	if err == nil {
		t.Fatal("an http: address passed — no plaintext mode exists, not for localhost, not for a flag, not for development (RA1)")
	}
	if !strings.Contains(err.Error(), "no plaintext mode") {
		t.Errorf("the http: refusal must say there is nothing to reach: %v", err)
	}
	p.Address = "https://peer.example:8443"
	if err := (Config{Peers: []Peer{p}}).Validate(); err == nil || !strings.Contains(err.Error(), "schemeless") {
		t.Errorf("an https:// address must be refused too — TLS is the transport's decision, not the config's: %v", err)
	}
	for _, bad := range []string{"", "peer.example", ":8443", "peer.example:", "peer.example:8443:extra"} {
		p.Address = bad
		if err := (Config{Peers: []Peer{p}}).Validate(); err == nil {
			t.Errorf("address %q accepted — the shape is host:port, schemeless", bad)
		}
	}
}

func TestPeerIdentityRules(t *testing.T) {
	p1, p2 := goodPeer(), goodPeer()
	p2.Address = "other.example:8443"
	if err := (Config{Peers: []Peer{p1, p2}}).Validate(); err == nil {
		t.Error("two peers with the same id passed — the id is how fire rows and policy rows name WHO was reached")
	}
	p2.ID = "Peer Two"
	if err := (Config{Peers: []Peer{goodPeer(), p2}}).Validate(); err == nil || !strings.Contains(err.Error(), "lowercase") {
		t.Errorf("an id outside [a-z0-9.-] must be refused so ids are log- and policy-safe verbatim: %v", err)
	}
	p3 := goodPeer()
	p3.ID = ""
	if err := (Config{Peers: []Peer{p3}}).Validate(); err == nil {
		t.Error("an empty id passed Validate")
	}
}

func TestPinShapeIsClosed(t *testing.T) {
	for name, pin := range map[string]*Pin{
		"short":      {SHA256: "0f1e", Kind: PinCertificate},
		"nonhex":     {SHA256: strings.Repeat("z", 64), Kind: PinCertificate},
		"uppercase":  {SHA256: strings.ToUpper(goodPin), Kind: PinCertificate},
		"bad-kind":   {SHA256: goodPin, Kind: "fingerprint"},
		"empty-kind": {SHA256: goodPin, Kind: ""},
	} {
		p := goodPeer()
		p.Pin = pin
		if err := (Config{Peers: []Peer{p}}).Validate(); err == nil {
			t.Errorf("pin %s accepted — 64 lowercase hex, kind in {certificate, spki}", name)
		}
	}
	p := goodPeer()
	p.Pin = &Pin{SHA256: goodPin, Kind: PinSPKI}
	if err := (Config{Peers: []Peer{p}}).Validate(); err != nil {
		t.Errorf("a valid spki pin refused: %v", err)
	}
}

// TestEmptyConfigIsValid is the house pattern (mcp config): the shipped
// default must never be a validation error — off-by-default has to load.
func TestEmptyConfigIsValid(t *testing.T) {
	if _, err := parseConfig([]byte(`{"peers":[]}`)); err != nil {
		t.Errorf("the empty config must load: %v", err)
	}
	if _, err := parseConfig([]byte(`{}`)); err != nil {
		t.Errorf("a bare object must load: %v", err)
	}
}

// TestLoadConfigFileIsTheDoorTheSameDoor: a file is validated exactly like
// the in-memory shape — no weaker path for "it came from disk".
func TestLoadConfigFileCarriesTheSameDoor(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "peers.json")
	if err := os.WriteFile(bad, []byte(`{"peers":[{"id":"a","address":"h:1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfigFile(bad); err == nil || !strings.Contains(err.Error(), "no pinned identity") {
		t.Errorf("LoadConfigFile let an unpinned peer through: %v", err)
	}
	good := filepath.Join(dir, "good.json")
	body := fmt.Sprintf(`{"peers":[{"id":"peer-alpha","address":"peer.example:8443","pin":{"sha256":%q,"kind":"certificate"},"key":{"env":"AEON_TEST_PEER_KEY"}}]}`, goodPin)
	if err := os.WriteFile(good, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfigFile(good)
	if err != nil {
		t.Fatalf("a good config must load: %v", err)
	}
	if len(c.Peers) != 1 || c.Peers[0].ID != "peer-alpha" {
		t.Fatalf("loaded wrong: %+v", c.Peers)
	}
	if !strings.Contains(c.OutboundState(), "peer-alpha at peer.example:8443") {
		t.Errorf("OutboundState must name the configured peers verbatim: %q", c.OutboundState())
	}
}
