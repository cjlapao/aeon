package daemon

// notify_channels.go — S58 T5: the channel seam. The protocol doors are
// thin (param shape, identity, person); everything with authority behind
// it lands here: the notify package's services over the store, the
// secrets store and the dispatcher, and the mapping of their plain
// sentences onto the typed refusals (-31923/-31924/-31928/-31929) —
// each error's `what` carries the real sentence verbatim (three-part
// data, never a paraphrase of a fact this layer does not own).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// notifyChannels is the protocol.NotifyChannelService and the owner of
// the dispatcher's lifecycle. One instance per daemon, built only with a
// store (V13): a storeless daemon has no seam and the ten doors are
// ABSENT, not registered-and-refusing.
type notifyChannels struct {
	st       *store.Store
	cs       *notify.ChannelService
	dp       *notify.Dispatcher
	inbox    *InboxService
	resolver *notify.Resolver
}

// newNotifyChannels builds the seam and starts the dispatcher. The
// version rides the User-Agent so a receiver can see who is calling.
func (d *Daemon) newNotifyChannels(version string) *notifyChannels {
	nc := &notifyChannels{
		st:    d.store,
		cs:    &notify.ChannelService{Store: d.store, Secrets: notify.FileSecrets{}},
		inbox: d.Inbox, resolver: d.classes,
	}
	nc.dp = notify.NewDispatcher(d.store, notify.FileSecrets{}, notify.DispatcherOptions{
		Version: version})
	notify.RegisterScopeResolver(d.store)
	nc.dp.Start()
	return nc
}

// Stop drains the dispatcher (serve's shutdown calls it): queued
// deliveries finish, nothing is lost mid-send without a row.
func (nc *notifyChannels) Stop() { nc.dp.Stop() }

// DigestAtStart is SV-D6, called by serve once the store is migrated
// and seeded: if attention items exist, ONE digest per enabled channel.
// "Daemon was down" is not a row — it is unknowable — so this is the
// compensation, and the missed per-event messages are never replayed
// (stale is stale).
func (nc *notifyChannels) DigestAtStart(ctx context.Context) {
	if nc.inbox == nil {
		return
	}
	sc, err := attention.EveryScope(nc.st)
	if err != nil {
		return
	}
	res, err := nc.inbox.registry.Evaluate(ctx, attention.Query{Scope: sc})
	if err != nil || res == nil {
		return // an evaluation failure is silence, not a guessed count — and never a startup error
	}
	nc.dp.SubmitDigest(len(res.Items))
}

// resolveForPublish is publishWithNotify's half-second: the ONE class
// resolution that both attaches the socket hint and feeds the channel
// dispatcher. The Submit happens after Append (the hint never enters the
// durable log, and the dispatcher's cursor is the real seq). Only
// class-RESOLVED events dispatch — an explicit caller-supplied hint is
// a toast, not a channel event with an unknown kind (T5 deviation,
// recorded). A nil hint dispatches nothing: no class, no envelope.
func (d *Daemon) resolveForPublish(kind string, payload any, ev *protocol.Event) *protocol.NotifyHint {
	h, re := d.classes.Resolve(kind, payload)
	if h == nil {
		return nil
	}
	if d.channels != nil {
		re.Seq = int64(ev.Seq)
		d.channels.dp.Submit(re)
	}
	return h
}

// NotifyDigestAtStart is the exported half of DigestAtStart (serve's
// recovery path calls it once, after seeding, before loops are accepted).
func (d *Daemon) NotifyDigestAtStart(ctx context.Context) {
	if d.channels != nil {
		d.channels.DigestAtStart(ctx)
	}
}

// --- the doors ----------------------------------------------------------------

func (nc *notifyChannels) NotifyChannelCreate(ctx context.Context, actor protocol.Actor, p protocol.NotifyChannelCreateParams) (protocol.NotifyChannelView, error) {
	row, err := nc.cs.Create(notify.ChannelArgs{
		Name: p.Name, URL: p.URL, AllowPrivate: p.AllowPrivate, SecretFile: p.SecretFile,
		ActorKind: actor.Kind, ActorID: actor.ID})
	if err != nil {
		return protocol.NotifyChannelView{}, channelRefused(err)
	}
	// The subscriptions ride the create but apply after the row (the row
	// must exist for the subscription's FK-less pairing to make sense),
	// each its own audited set.
	for _, sub := range p.Subscriptions {
		sp := sub
		sp.ChannelID = row.ID
		if _, serr := nc.NotifySubscriptionSet(ctx, actor, sp); serr != nil {
			// The channel stands; the bad subscription is reported as the
			// create's partial truth, never rolled back into a lie.
			return nc.view(row), serr
		}
	}
	return nc.view(row), nil
}

func (nc *notifyChannels) NotifyChannelUpdate(ctx context.Context, actor protocol.Actor, p protocol.NotifyChannelUpdateParams) (protocol.NotifyChannelView, error) {
	row, err := nc.cs.Update(p.ID, notify.ChannelPatch{
		Name: p.Name, URL: p.URL, AllowPrivate: p.AllowPrivate, SecretFile: p.SecretFile,
		ActorKind: actor.Kind, ActorID: actor.ID})
	if err != nil {
		return nc.viewMaybe(p.ID, channelRefused(err))
	}
	return nc.view(row), nil
}

func (nc *notifyChannels) NotifyChannelEnable(ctx context.Context, actor protocol.Actor, id string) (protocol.NotifyChannelView, error) {
	row, err := nc.cs.Enable(id, actor.Kind, actor.ID)
	if err != nil {
		if errors.Is(err, notify.ErrNoSecret) {
			return protocol.NotifyChannelView{}, protocol.ErrSecretMissing.WithWhat("%v", err)
		}
		return nc.viewMaybe(id, channelRefused(err))
	}
	return nc.view(row), nil
}

func (nc *notifyChannels) NotifyChannelDisable(ctx context.Context, actor protocol.Actor, id string) (protocol.NotifyChannelView, error) {
	row, err := nc.cs.Disable(id, actor.Kind, actor.ID)
	if err != nil {
		return nc.viewMaybe(id, channelRefused(err))
	}
	return nc.view(row), nil
}

func (nc *notifyChannels) NotifyChannelArchive(ctx context.Context, actor protocol.Actor, p protocol.NotifyChannelArchiveParams) (protocol.NotifyChannelView, error) {
	row, err := nc.cs.Archive(p.ID, p.Reason, actor.Kind, actor.ID)
	if err != nil {
		return nc.viewMaybe(p.ID, channelRefused(err))
	}
	return nc.view(row), nil
}

func (nc *notifyChannels) NotifyChannelTest(ctx context.Context, actor protocol.Actor, id string) (protocol.NotifyTestResult, error) {
	// The person is the actor; the delivery is the dispatcher's act. A
	// test on a disabled/archived channel is a refusal (-31923); a test
	// that RAN and failed is ErrTestFailed (-31929) whose `what` is the
	// honest outcome — the row already exists either way.
	_ = actor
	res, err := nc.dp.Test(id)
	if err != nil {
		return protocol.NotifyTestResult{}, channelRefused(err)
	}
	out := protocol.NotifyTestResult{Outcome: res.Outcome, ErrorKind: res.ErrorKind,
		HTTPStatus: res.HTTPStatus, Attempts: res.Attempts, At: res.At}
	if res.Outcome != "sent" {
		return out, protocol.ErrTestFailed.WithWhat("the test delivery ended %q (error kind %q, HTTP %d, %d attempt(s) at %s)",
			res.Outcome, res.ErrorKind, res.HTTPStatus, res.Attempts, res.At)
	}
	return out, nil
}

func (nc *notifyChannels) NotifySubscriptionSet(ctx context.Context, actor protocol.Actor, p protocol.NotifySubscriptionSetParams) (protocol.NotifySubscriptionView, error) {
	kinds := ""
	if p.Kinds != nil {
		body, err := json.Marshal(*p.Kinds)
		if err != nil {
			return protocol.NotifySubscriptionView{}, protocol.ErrBadParams{Err: fmt.Errorf("kinds must be a JSON array of strings: %w", err)}
		}
		kinds = string(body)
		if *p.Kinds != nil && len(*p.Kinds) > 0 && kinds == "null" {
			kinds = ""
		}
	}
	row, err := nc.cs.SetSubscription(store.SetSubscriptionIn{
		ChannelID: p.ChannelID, RootID: p.RootID, MinSeverity: p.MinSeverity, Kinds: kinds,
		QuietStart: p.QuietStart, QuietEnd: p.QuietEnd, QuietZone: p.QuietZone, MaxPerHour: p.MaxPerHour,
		ActorKind: actor.Kind, ActorID: actor.ID})
	if err != nil {
		return protocol.NotifySubscriptionView{}, channelRefused(err)
	}
	return subView(row), nil
}

func (nc *notifyChannels) NotifyChannelList(ctx context.Context) (protocol.NotifyChannelListResult, error) {
	rows, err := nc.st.ListChannels(true)
	if err != nil {
		return protocol.NotifyChannelListResult{}, err
	}
	out := protocol.NotifyChannelListResult{Channels: []protocol.NotifyChannelView{}}
	for _, ch := range rows {
		out.Channels = append(out.Channels, nc.view(ch))
	}
	return out, nil
}

func (nc *notifyChannels) NotifyChannelShow(ctx context.Context, id string) (protocol.NotifyChannelView, error) {
	ch, found, err := nc.st.GetChannel(id)
	if err != nil {
		return protocol.NotifyChannelView{}, err
	}
	if !found {
		return protocol.NotifyChannelView{}, protocol.ErrNotifyChannelRefused.WithWhat("channel %q does not exist", id)
	}
	return nc.view(ch), nil
}

func (nc *notifyChannels) NotifyDeliveryList(ctx context.Context, p protocol.NotifyDeliveryListParams) (protocol.NotifyDeliveryListResult, error) {
	ch, found, err := nc.st.GetChannel(p.ChannelID)
	if err != nil {
		return protocol.NotifyDeliveryListResult{}, err
	}
	if !found {
		return protocol.NotifyDeliveryListResult{}, protocol.ErrDeliveryBad.WithWhat("channel %q does not exist", p.ChannelID)
	}
	if ch.ArchivedAt != "" && !p.IncludeArchived {
		return protocol.NotifyDeliveryListResult{}, protocol.ErrDeliveryBad.WithWhat(
			"channel %q is archived (%s) — its log is kept (no FK, history outlives the channel); pass includeArchived to read it", p.ChannelID, ch.ArchiveReason)
	}
	rows, err := nc.st.ListDeliveries(p.ChannelID, p.Limit)
	if err != nil {
		return protocol.NotifyDeliveryListResult{}, err
	}
	out := protocol.NotifyDeliveryListResult{Deliveries: []protocol.NotifyDeliveryItem{}}
	for _, r := range rows {
		out.Deliveries = append(out.Deliveries, protocol.NotifyDeliveryItem{
			ID: r.ID, ChannelID: r.ChannelID, EventSeq: r.EventSeq, AttentionKey: r.AttentionKey,
			Outcome: r.Outcome, ErrorKind: r.ErrorKind, HTTPStatus: r.HTTPStatus,
			Attempts: r.Attempts, At: r.At})
	}
	return out, nil
}

// --- views ---------------------------------------------------------------------

// view assembles the wire shape. HOST, never url; secretPresent, never
// the secret; the delivery log's two facts (last outcome, consecutive
// failures) read from its recent page — a channel never auto-disables,
// so the LISTING is where "it has been failing" becomes visible (the CLI
// verb and doctor's ≥3 warning ride these numbers, T6).
func (nc *notifyChannels) view(ch store.NotifyChannel) protocol.NotifyChannelView {
	v := protocol.NotifyChannelView{
		ID: ch.ID, Name: ch.Name, Kind: ch.Kind, Host: hostOf(ch.URL),
		Enabled: ch.Enabled, AllowPrivate: ch.AllowPrivate,
		SecretPresent: nc.cs.SecretPresent(ch.SecretRef),
		Subscriptions: []protocol.NotifySubscriptionView{},
		CreatedAt:     ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
		ArchivedAt: ch.ArchivedAt, ArchiveReason: ch.ArchiveReason,
	}
	if subs, err := nc.st.ListSubscriptions(ch.ID); err == nil {
		for _, s := range subs {
			v.Subscriptions = append(v.Subscriptions, subView(s))
		}
	}
	if rows, err := nc.st.ListDeliveries(ch.ID, 50); err == nil && len(rows) > 0 {
		v.LastOutcome = rows[0].Outcome
		for _, r := range rows {
			if r.Outcome != "failed" {
				break // the log is newest-first; the streak counts from the top
			}
			v.ConsecutiveFailures++
		}
	}
	return v
}

func (nc *notifyChannels) viewMaybe(id string, refused error) (protocol.NotifyChannelView, error) {
	return protocol.NotifyChannelView{}, refused
}

func subView(s store.NotifySubscription) protocol.NotifySubscriptionView {
	v := protocol.NotifySubscriptionView{
		ChannelID: s.ChannelID, RootID: s.RootID, MinSeverity: s.MinSeverity,
		QuietStart: s.QuietStart, QuietEnd: s.QuietEnd, QuietZone: s.QuietZone,
		Kinds: nil,
	}
	if s.Kinds != "" {
		var list []string
		if json.Unmarshal([]byte(s.Kinds), &list) == nil {
			v.Kinds = list
		}
	}
	v.MaxPerHour = s.MaxPerHour
	if v.MaxPerHour == 0 {
		v.MaxPerHour = 30 // the view answers what the dispatcher will enforce
	}
	return v
}

// hostOf parses the host out of the url. A url the store accepted has a
// host by construction (the store's channelHost checked it at the write
// that made this row); the fallback names the impossibility rather than
// returning empty and presenting a channel as hostless.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "(unreadable host)"
	}
	return u.Host
}

// channelRefused maps a service sentence onto the typed family. The
// unknown-channel and archive sentences are family members (the family
// is one because every member says "this channel as written may not
// exist or may not send"); anything genuinely foreign rides through
// unchanged rather than being mislabelled.
func channelRefused(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case strings.HasPrefix(err.Error(), "notify:"), strings.HasPrefix(err.Error(), "store: "),
		strings.HasPrefix(err.Error(), "attention:"):
		return protocol.ErrNotifyChannelRefused.WithWhat("%v", err)
	default:
		return err
	}
}
