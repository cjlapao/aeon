// Package remote is S21: Aeon talking to a machine it does not own.
//
// Everything above the transport is S14's, unchanged (design.md): the card,
// the skill, the Task and the chain are reused, and D14 asserts by grep that
// this slice never edits card.go, skill.go, task.go, invoke.go. What lives
// here is only what the local case got for free and the remote case does not:
// identity (RA2, RA3, RA9), TLS-only transport (RA1), egress policy before
// the request is built (RA4), untrusted results (RA5), the bound callback
// (RA7, RA8) and cost honesty (RA10, P31).
package remote

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Peer is a configured remote agent (RA2, RA3, RA9). All three of its doors
// are required and none is a default: an explicit address, a pinned identity,
// and a reference to key material. RA9's sentence — a daemon that reaches
// out by default is a daemon somebody exfiltrated through by accident — is
// why the zero value of a Config carries no peer at all.
//
// The struct deliberately holds NO key bytes. A pin is public and may sit in
// a config file; a client key or shared secret may not (C17), so what a Peer
// carries is a KeyRef — a name of an environment variable or a path — and
// the bytes are read at use time by LoadKey, never stored here.
type Peer struct {
	ID      string  `json:"id"`
	Address string  `json:"address"` // host:port, schemeless — TLS is the only transport (RA1)
	Pin     *Pin    `json:"pin"`     // required: the peer's certificate or SPKI, sha-256 (RA2)
	Key     *KeyRef `json:"key"`     // required: env-var name or path, never literal bytes (C17)
}

// Pin is the pinned identity that makes "this is the peer we consented to"
// true (RA2). The system CA set is not sufficient on its own, and Peer
// validation refuses a peer that has no pin — not at dial time, here.
type Pin struct {
	// SHA256 is the lowercase hex sha-256 of the peer's certificate (DER) or
	// its SubjectPublicKeyInfo, as configured. Exactly 64 hex chars.
	SHA256 string `json:"sha256"`
	// Kind says which of the two was hashed, so a rotation story can say what
	// to re-read. It is closed: no third kind means no third story.
	Kind string `json:"kind"` // "certificate" | "spki"
}

// Pin kinds, the closed set.
const (
	PinCertificate = "certificate"
	PinSPKI        = "spki"
)

// KeyRef is a reference to key material, never the material itself (C17).
// Exactly one of Env or Path is set: the environment or a path, never argv,
// never a config file, never a log.
type KeyRef struct {
	Env  string `json:"env,omitempty"`
	Path string `json:"path,omitempty"`
}

// Config is the whole remote surface. Its zero value is the shipped default
// and it reaches nothing (RA9, D13): no peer, no dial, no plan to dial.
type Config struct {
	Peers []Peer `json:"peers"`
}

// LoadConfigFile reads a peer configuration. The shape is strict —
// DisallowUnknownFields means a config that tries to smuggle literal key
// bytes under any new name fails at the door (C17), and the refusal names
// the field, because a rejected config nobody can read is a bug nobody finds.
func LoadConfigFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("remote config %q is not readable: %w", path, err)
	}
	return parseConfig(raw)
}

// parseConfig decodes with the strictness the security story depends on. It
// is separate from LoadConfigFile so a test can hand it bytes without a
// round-trip through the filesystem.
func parseConfig(raw []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("remote config refused: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate is the configuration-time door (T1's promise): every peer must
// arrive complete — an explicit schemeless address, a pin, and a key
// reference — and it is refused HERE, not at dial time, because a peer that
// reaches the dial unvalidated is a peer that dials anyway and learns later.
func (c Config) Validate() error {
	seen := map[string]bool{}
	for i, p := range c.Peers {
		where := fmt.Sprintf("peer %d", i)
		if p.ID != "" {
			where = fmt.Sprintf("peer %q", p.ID)
		}
		if err := validPeerID(p.ID); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if seen[p.ID] {
			return fmt.Errorf("%s: two peers carry the same id — the id is how a fire, a policy row and a log line name who was reached, and two peers answering to one name makes all three lie", where)
		}
		seen[p.ID] = true
		if err := validAddress(p.Address); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if p.Pin == nil {
			return fmt.Errorf("%s has no pinned identity: a valid certificate from the system CA set proves TLS, not identity — anyone with a CA-issued certificate for any host would be accepted, so the CA set alone is not sufficient (RA2). Pin the peer's certificate or SPKI by sha-256 so \"this is the peer we consented to\" is a fact the daemon checks, not a claim it trusts", where)
		}
		if err := p.Pin.validate(); err != nil {
			return fmt.Errorf("%s: pin: %w", where, err)
		}
		if p.Key == nil {
			return fmt.Errorf("%s has no key material: RA9 requires address, pinned identity AND a key to enable a peer — a peer anybody can authenticate as is a door, not a peer. Give it an environment variable name or a file path (never the bytes themselves, never argv: C17)", where)
		}
		if err := p.Key.validate(); err != nil {
			return fmt.Errorf("%s: key: %w", where, err)
		}
	}
	return nil
}

// validPeerID keeps ids inside the charset that makes them safe to drop into
// log lines, policy targets and fire rows without quoting games.
func validPeerID(id string) error {
	if id == "" {
		return fmt.Errorf("id is required — it is the name policy rows and fire rows use to say WHO was reached")
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return fmt.Errorf("id %q carries %q: ids are lowercase letters, digits, '-' and '.' only, so they can appear verbatim in a log line or a policy target", id, string(r))
		}
	}
	return nil
}

// validAddress keeps the config shape schemeless. An address carrying
// "http://" is refused by name (D1: no plaintext mode exists — not for
// localhost, not for a flag, not for development); "https://" is refused too,
// because the scheme is not ours to carry — the transport decides TLS (RA1)
// and a config that spells the scheme is a config that is one edit away from
// spelling a different one.
func validAddress(addr string) error {
	if addr == "" {
		return fmt.Errorf("address is required — a peer is configured, never discovered (RA3)")
	}
	if strings.Contains(addr, "://") {
		scheme := addr[:strings.Index(addr, "://")]
		if strings.EqualFold(scheme, "http") {
			return fmt.Errorf("address %q names http: — there is no plaintext mode to reach, not for localhost and not for development (RA1); the socket already exists for local and is better (P26); if a peer is remote, it is remote, and it is TLS", addr)
		}
		return fmt.Errorf("address %q carries scheme %q: addresses are host:port and schemeless — TLS is the only transport and the transport says so, not the config (RA1)", addr, scheme)
	}
	if strings.Count(addr, ":") != 1 || strings.HasSuffix(addr, ":") || strings.HasPrefix(addr, ":") {
		return fmt.Errorf("address %q is not host:port", addr)
	}
	return nil
}

// validate checks the pin's shape, not its match — matching is the trust
// store's (T2, trust.go), and this file's job is that nothing incomplete
// arrives there.
func (p *Pin) validate() error {
	if len(p.SHA256) != 64 {
		return fmt.Errorf("sha256 is %d chars, want 64 hex characters of the peer's certificate or SPKI", len(p.SHA256))
	}
	if _, err := hex.DecodeString(p.SHA256); err != nil {
		return fmt.Errorf("sha256 is not hex: %w", err)
	}
	if p.SHA256 != strings.ToLower(p.SHA256) {
		return fmt.Errorf("sha256 must be lowercase hex — one spelling, so two configs naming the same pin are provably the same pin")
	}
	switch p.Kind {
	case PinCertificate, PinSPKI:
		return nil
	default:
		return fmt.Errorf("kind %q is not %q or %q", p.Kind, PinCertificate, PinSPKI)
	}
}

// validate keeps the reference a reference: exactly one source, so a peer
// cannot be configured "from the environment, and also this file, and the
// winner is whatever read first".
func (k *KeyRef) validate() error {
	if k.Env == "" && k.Path == "" {
		return fmt.Errorf("neither env nor path is set — key material is read from the environment or a path, never argv, never a config file, never a log (C17)")
	}
	if k.Env != "" && k.Path != "" {
		return fmt.Errorf("both env and path are set — one source, so a config review can see which one wins")
	}
	return nil
}

// String redacts (C17: never a log). The reference is safe to print — it is a
// NAME; the bytes are what never appear, and they are not held here to begin
// with. This method exists so %v of a Peer can never become the place a key
// leaks, even by a struct-literal field added later that forgot: the only
// key-shaped thing in this package is a KeyRef, and it prints its shape only.
func (k *KeyRef) String() string {
	if k == nil {
		return "key:<none>"
	}
	if k.Env != "" {
		return "key:env:" + k.Env
	}
	return "key:path:" + k.Path
}

// GoString matches String so %#v, the debugging verb that normally prints
// every field, stays redacted too.
func (k *KeyRef) GoString() string { return k.String() }

// getenv is the injectable seam the mcp client's C17 read uses
// (internal/mcp/client.go:117-120): tests hand a function, production hands
// the OS.
var getenv = os.Getenv

// LoadKey reads the key material at USE time (T3's dial is the first caller).
// It is deliberately not called by Validate or by any decode path: reading a
// key while loading a config file would be reading a secret to find out
// whether the config is shaped right, which is how secrets end up in stack
// traces. A missing source is refused with a sentence naming which one,
// because "key not found" across two sources makes an operator debug the
// wrong one.
func (k *KeyRef) LoadKey() ([]byte, error) {
	if err := k.validate(); err != nil {
		return nil, err
	}
	if k.Env != "" {
		v := getenv(k.Env)
		if v == "" {
			return nil, fmt.Errorf("key material: environment variable %s is set to nothing (or unset) — the key comes from the environment or a path, never argv, never a config file (C17)", k.Env)
		}
		return []byte(v), nil
	}
	raw, err := os.ReadFile(k.Path)
	if err != nil {
		return nil, fmt.Errorf("key material: %q is not readable: %w — the key comes from the environment or a path, never argv, never a config file (C17)", k.Path, err)
	}
	return raw, nil
}

// OutboundState is the fresh install stated, not implied (D13): the empty
// config does not merely reach nothing, it SAYS nothing outbound is
// reachable, so a health line, a log line or a demo can quote the fact
// instead of inferring it from the absence of rows. N15's sentence turned
// outward: a daemon that reaches out by default is one somebody
// exfiltrated through by accident.
func (c Config) OutboundState() string {
	if len(c.Peers) == 0 {
		return "nothing outbound is reachable — remote peers are off by default, and enabling one takes an explicit address, a pinned identity and a key (RA9)"
	}
	parts := make([]string, 0, len(c.Peers))
	for _, p := range c.Peers {
		parts = append(parts, fmt.Sprintf("%s at %s", p.ID, p.Address))
	}
	return "outbound peers configured: " + strings.Join(parts, "; ")
}
