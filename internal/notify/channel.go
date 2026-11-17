package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// channel.go — S58 T5 (SV7/SV12): the channel as an AUTHORITY record. A
// webhook is standing authority to move machine facts off the machine,
// so every write here is a person's decision: rows are created DISABLED,
// enabling requires the signing secret to actually exist, the actor is
// the socket peer (the service re-checks `user`, and the store re-checks
// it in SQL — the three locks, lock 2 and lock 3), and nothing here is
// ever seeded (the named exception to the rule that rows arrive from
// records).
//
// Every write and its notify_change row are ONE transaction — the store
// methods compose create/update/enable/archive/subscribe that way; this
// file adds no SQL and takes no audit shortcut.

// ErrNoSecret is the enable-time refusal the CLI quotes: an enabled
// channel with no secret would send unsigned bodies — the receiver could
// not tell a webhook from a forgery, and the whole point of the secret
// is that the receiver gets to check.
var ErrNoSecret = errors.New("the channel has no secret — set one with --secret-file")

// MaxChannelNameBytes / MaxChannelURLBytes are SV7's caps (2 KiB is the
// URL's design cap; 80 keeps listings honest).
const (
	MaxChannelNameBytes = 80
	MaxChannelURLBytes  = 2048
)

// ValidateChannel is the door's shape-check, run BEFORE any row exists:
// https only (http admitted only with allow_private AND a loopback or
// private HOST LITERAL — or "localhost"; the dial-time check is still the
// real gate, this is the honest refusal at write time), no userinfo, no
// empty host, both size caps. Every sentence here is the what that
// ErrNotifyChannelRefused carries; nothing is persisted on a refusal.
func ValidateChannel(name, rawURL string, allowPrivate bool) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("notify: a channel needs a name")
	}
	if len(name) > MaxChannelNameBytes {
		return fmt.Errorf("notify: channel name is %d bytes, the cap is %d", len(name), MaxChannelNameBytes)
	}
	if rawURL == "" {
		return fmt.Errorf("notify: a channel needs a url")
	}
	if len(rawURL) > MaxChannelURLBytes {
		return fmt.Errorf("notify: channel url is %d bytes, the cap is %d", len(rawURL), MaxChannelURLBytes)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("notify: channel url does not parse: %v", err)
	}
	if u.User != nil {
		return fmt.Errorf("notify: the channel url carries userinfo — refused (credentials belong in the secret, not the url)")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("notify: the channel url has no host")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowPrivate {
			return fmt.Errorf("notify: http is accepted only with allow_private=1 and a loopback or private host — for a local receiver, both")
		}
		if !isLocalHost(host) {
			return fmt.Errorf("notify: http with allow_private still requires a loopback or private host, not %q", host)
		}
	default:
		return fmt.Errorf("notify: the channel url scheme must be https (or http with allow_private), not %q", u.Scheme)
	}
	return nil
}

// isLocalHost is the write-time literal check: an IP in the loopback or
// private families, or localhost by name (the one name whose answer is
// fixed by convention).
func isLocalHost(host string) bool {
	if host == "localhost" || host == "ip6-localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if len(ip) == 16 && ip.To4() != nil {
		ip = ip.To4()
	}
	// The local family is loopback, private and CGNAT — the same
	// addresses refuseIP weighs. Link-local/metadata is NOT local (it is
	// the cloud's router, not the machine's), and it stays refused even
	// for an allow_private channel.
	return ip.IsLoopback() || ip.IsPrivate() || isCGNAT(ip)
}

// ChannelArgs / ChannelPatch are the service's verbs' inputs. SecretFile
// is a PATH on the daemon host and the only way a secret value enters
// (C17: never a flag value, a param, stdin or a prompt — T6's CLI maps
// --secret-file onto it and nothing else).
type ChannelArgs struct {
	Name         string
	URL          string
	AllowPrivate bool
	SecretFile   string
	ActorKind    string
	ActorID      string
}

type ChannelPatch struct {
	Name         *string
	URL          *string
	AllowPrivate *bool
	SecretFile   string
	ActorKind    string
	ActorID      string
}

// ChannelService is the daemon-side author of channels: the store under
// it (rows + audit, one transaction each) and the secret store beside it
// (value first, ref in the row).
type ChannelService struct {
	Store   *store.Store
	Secrets SecretStore
}

// Create: validate → row (created DISABLED) → secret written to the
// secrets store under the row's own ref. The value never touches the
// row, the audit, or a log; the holder is zeroed the moment Put returns.
func (s *ChannelService) Create(a ChannelArgs) (store.NotifyChannel, error) {
	if a.ActorKind != "user" {
		return store.NotifyChannel{}, whatNotAPerson(a.ActorKind)
	}
	if err := ValidateChannel(a.Name, a.URL, a.AllowPrivate); err != nil {
		return store.NotifyChannel{}, err
	}
	var secret Secret
	if a.SecretFile != "" {
		var err error
		if secret, err = ReadSecretFile(a.SecretFile); err != nil {
			return store.NotifyChannel{}, err
		}
	}
	ch, err := s.Store.CreateChannel(store.CreateChannelIn{
		Name: a.Name, URL: a.URL, AllowPrivate: a.AllowPrivate,
		ActorKind: a.ActorKind, ActorID: a.ActorID})
	if err != nil {
		return store.NotifyChannel{}, err
	}
	if secret.Present() {
		if err := s.Secrets.Put(ch.SecretRef, secret.Bytes()); err != nil {
			clear(secret.v)
			// The row stands, disabled and unsendable — the honest report
			// is "created, secret not stored", never a silent half.
			return ch, fmt.Errorf("notify: the channel was created (disabled) but its secret was not stored: %w", err)
		}
		clear(secret.v)
	}
	return ch, nil
}

// Update touches only what the patch names. The secret is a separate act
// (Put + its own audit token): replacing a secret changed nothing about
// the row, and the audit says so truthfully.
func (s *ChannelService) Update(id string, p ChannelPatch) (store.NotifyChannel, error) {
	if p.ActorKind != "user" {
		return store.NotifyChannel{}, whatNotAPerson(p.ActorKind)
	}
	cur, found, err := s.Store.GetChannel(id)
	if err != nil {
		return store.NotifyChannel{}, err
	}
	if !found {
		return store.NotifyChannel{}, fmt.Errorf("notify: channel %q does not exist", id)
	}
	if p.Name != nil || p.URL != nil || p.AllowPrivate != nil {
		name, u := cur.Name, cur.URL
		priv := cur.AllowPrivate
		if p.Name != nil {
			name = *p.Name
		}
		if p.URL != nil {
			u = *p.URL
		}
		if p.AllowPrivate != nil {
			priv = *p.AllowPrivate
		}
		if err := ValidateChannel(name, u, priv); err != nil {
			return store.NotifyChannel{}, err
		}
	}
	row, err := s.Store.UpdateChannel(id, store.UpdateChannelIn{
		Name: p.Name, URL: p.URL, AllowPrivate: p.AllowPrivate,
		ActorKind: p.ActorKind, ActorID: p.ActorID})
	if err != nil {
		return store.NotifyChannel{}, err
	}
	if p.SecretFile != "" {
		secret, err := ReadSecretFile(p.SecretFile)
		if err != nil {
			return row, err
		}
		if err := s.Secrets.Put(row.SecretRef, secret.Bytes()); err != nil {
			clear(secret.v)
			return row, err
		}
		clear(secret.v)
		if err := s.Store.AuditChannelSecret(id, p.ActorKind, p.ActorID); err != nil {
			return row, err
		}
	}
	return row, nil
}

// Enable is the authority switch, and the secret gate: an enabled
// channel MUST have a secret (an unsigned webhook is a forgery with a
// green light).
func (s *ChannelService) Enable(id, actorKind, actorID string) (store.NotifyChannel, error) {
	if actorKind != "user" {
		return store.NotifyChannel{}, whatNotAPerson(actorKind)
	}
	cur, found, err := s.Store.GetChannel(id)
	if err != nil {
		return store.NotifyChannel{}, err
	}
	if !found {
		return store.NotifyChannel{}, fmt.Errorf("notify: channel %q does not exist", id)
	}
	if cur.ArchivedAt != "" {
		return store.NotifyChannel{}, fmt.Errorf("notify: channel %q is archived (%s) — an archive is not re-enabled, add a new channel", id, cur.ArchiveReason)
	}
	v, err := s.Secrets.Get(cur.SecretRef)
	if err != nil {
		return store.NotifyChannel{}, err
	}
	if len(v) == 0 {
		return store.NotifyChannel{}, fmt.Errorf("notify: %w", ErrNoSecret)
	}
	return s.Store.SetChannelEnabled(id, true, actorKind, actorID)
}

func (s *ChannelService) Disable(id, actorKind, actorID string) (store.NotifyChannel, error) {
	if actorKind != "user" {
		return store.NotifyChannel{}, whatNotAPerson(actorKind)
	}
	return s.Store.SetChannelEnabled(id, false, actorKind, actorID)
}

// Archive retires without deleting: the delivery log has no FK and must
// outlive the channel (SV12). The secret file stays — archive is the
// reversible act, and an operator who re-enables the SAME row later gets
// their secret back, which is the whole reason delete does not exist.
func (s *ChannelService) Archive(id, reason, actorKind, actorID string) (store.NotifyChannel, error) {
	if actorKind != "user" {
		return store.NotifyChannel{}, whatNotAPerson(actorKind)
	}
	if strings.TrimSpace(reason) == "" {
		return store.NotifyChannel{}, fmt.Errorf("notify: archiving a channel needs a reason — the row will outlive the memory")
	}
	return s.Store.ArchiveChannel(id, reason, actorKind, actorID)
}

// SecretPresent is what every view answers about a secret.
func (s *ChannelService) SecretPresent(ref string) bool {
	v, err := s.Secrets.Get(ref)
	return err == nil && len(v) > 0
}

// ValidateSubscription checks the filter's closed vocabularies before
// the row (SV12's all-or-none windows, TR6's zone rule): kinds must name
// kinds, severity must be a severity, a quiet window needs BOTH ends and
// an explicit zone that actually loads.
func ValidateSubscription(in store.SetSubscriptionIn) error {
	if in.MinSeverity != "" && !attention.ValidSeverity(in.MinSeverity) {
		return fmt.Errorf("notify: minSeverity %q is not a severity (the closed set is %s)", in.MinSeverity, strings.Join(attentionStrings(), ", "))
	}
	if in.Kinds != "" {
		var list []string
		if err := json.Unmarshal([]byte(in.Kinds), &list); err != nil {
			return fmt.Errorf("notify: kinds must be a JSON array of kind strings: %v", err)
		}
		if len(list) == 0 {
			return fmt.Errorf("notify: kinds is an empty array — an empty filter would deliver nothing; omit it for \"everything the class table raises\"")
		}
		known := map[string]bool{"inbox.digest": true}
		for _, k := range protocol.Kinds() {
			known[k] = true
		}
		for _, k := range list {
			if !known[k] {
				return fmt.Errorf("notify: kinds names %q, which is not a kind the daemon emits", k)
			}
		}
	}
	if in.MaxPerHour < 0 || in.MaxPerHour == 1 {
		// 0 = the default (30); 1 as a literal cap would silence
		// everything but the first event — the S40 floor is 2.
		return fmt.Errorf("notify: maxPerHour must be 0 (the default) or at least 2 (at most 1000)")
	}
	if in.MaxPerHour > 1000 {
		return fmt.Errorf("notify: maxPerHour %d is over the ceiling of 1000", in.MaxPerHour)
	}
	started, ended := in.QuietStart != "", in.QuietEnd != ""
	if started != ended {
		return fmt.Errorf("notify: a quiet window needs both quietStart and quietEnd")
	}
	if started {
		for _, hm := range []string{in.QuietStart, in.QuietEnd} {
			if _, err := time.Parse("15:04", hm); err != nil {
				return fmt.Errorf("notify: %q is not HH:MM", hm)
			}
		}
		if in.QuietZone == "" {
			return fmt.Errorf("notify: a quiet window needs an explicit IANA zone (quietZone) — wall-clock hours with no zone are a different promise depending on the daemon's machine (TR6)")
		}
		if _, err := time.LoadLocation(in.QuietZone); err != nil {
			return fmt.Errorf("notify: quietZone %q is not a zone this machine knows: %v", in.QuietZone, err)
		}
	} else if in.QuietZone != "" {
		return fmt.Errorf("notify: quietZone without a window names a zone nothing uses")
	}
	return nil
}

func attentionStrings() []string {
	out := []string{}
	for _, s := range attention.Severities() {
		out = append(out, string(s))
	}
	return out
}

// SetSubscription writes the filter row (root "" = every root — the S40
// convention), the store auditing it beside the write.
func (s *ChannelService) SetSubscription(in store.SetSubscriptionIn) (store.NotifySubscription, error) {
	if in.ActorKind != "user" {
		return store.NotifySubscription{}, whatNotAPerson(in.ActorKind)
	}
	if err := ValidateSubscription(in); err != nil {
		return store.NotifySubscription{}, err
	}
	if in.RootID != "" {
		// The store's own tree reader answers "is this a live component":
		// the scope sentence is attention's, reused verbatim (the same
		// refusal inbox.list makes for the same lie).
		if _, err := attention.ResolveScope(s.Store, in.RootID); err != nil {
			return store.NotifySubscription{}, fmt.Errorf("notify: %w", err)
		}
	}
	return s.Store.SetSubscription(in)
}

func whatNotAPerson(kind string) error {
	return fmt.Errorf("notify: what notifies is a person's decision — this connection acts as %s %q, which may not author a channel (SV13)", kind, kind)
}
