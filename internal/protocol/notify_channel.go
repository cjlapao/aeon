package protocol

// notify_channel.go — S58 T5 (SV7, SV8): the channel plane's doors.
//
// Seven writes and three reads. A webhook channel is STANDING AUTHORITY
// to send machine facts off this machine — an exfiltration path if
// anything but the person at the endpoint could author one — so every
// write here is the human door in its strictest form: refused for an
// agent peer, refused for an unattributed connection, refused when the
// request itself names an actor (SV13's identity rule), created disabled,
// enabled only over a real secret, never seeded, never deleted (archived
// with a reason), and absent wholesale without the seam (V13).
//
// The view carries the HOST, never the url (the url is a routing target
// and its full form has no consumer — a host is what a person scans to
// recognise their own webhook), and never the secret — `secretPresent`
// is a bool, the same answer shape as the route list's `hasKey`: a
// credential goes in and never comes out (§4c).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NotifyChannelService is the daemon seam. Actors arrive validated as
// persons; the seam owns the store, the secret store and the dispatcher.
type NotifyChannelService interface {
	NotifyChannelCreate(ctx context.Context, actor Actor, p NotifyChannelCreateParams) (NotifyChannelView, error)
	NotifyChannelUpdate(ctx context.Context, actor Actor, p NotifyChannelUpdateParams) (NotifyChannelView, error)
	NotifyChannelEnable(ctx context.Context, actor Actor, id string) (NotifyChannelView, error)
	NotifyChannelDisable(ctx context.Context, actor Actor, id string) (NotifyChannelView, error)
	NotifyChannelArchive(ctx context.Context, actor Actor, p NotifyChannelArchiveParams) (NotifyChannelView, error)
	// NotifyChannelTest sends the fixed test envelope now and answers with
	// the delivery outcome — and when it did not land, an error whose
	// `what` is that outcome (ErrTestFailed). A test is a delivery, not a
	// dry lie.
	NotifyChannelTest(ctx context.Context, actor Actor, id string) (NotifyTestResult, error)
	NotifySubscriptionSet(ctx context.Context, actor Actor, p NotifySubscriptionSetParams) (NotifySubscriptionView, error)
	NotifyChannelList(ctx context.Context) (NotifyChannelListResult, error)
	NotifyChannelShow(ctx context.Context, id string) (NotifyChannelView, error)
	NotifyDeliveryList(ctx context.Context, p NotifyDeliveryListParams) (NotifyDeliveryListResult, error)
}

// --- wire types ---------------------------------------------------------------

// NotifyChannelView is one channel as the wire carries it. Host, not
// url; secretPresent, not a secret. Subscriptions are the channel's
// scope rows; LastOutcome and ConsecutiveFailures are the delivery log's
// two honest facts a listing needs ("is it working lately?").
type NotifyChannelView struct {
	ID                  string                   `json:"id"`
	Name                string                   `json:"name"`
	Kind                string                   `json:"kind"`
	Host                string                   `json:"host"`
	Enabled             bool                     `json:"enabled"`
	AllowPrivate        bool                     `json:"allowPrivate"`
	SecretPresent       bool                     `json:"secretPresent"`
	Subscriptions       []NotifySubscriptionView `json:"subscriptions"`
	CreatedAt           string                   `json:"createdAt"`
	UpdatedAt           string                   `json:"updatedAt"`
	ArchivedAt          string                   `json:"archivedAt,omitempty"`
	ArchiveReason       string                   `json:"archiveReason,omitempty"`
	LastOutcome         string                   `json:"lastOutcome,omitempty"`
	ConsecutiveFailures int                      `json:"consecutiveFailures"`
}

// NotifySubscriptionView is one (channel, root) scope row. Kinds is
// null when unset = every kind the class table raises; MaxPerHour is
// stored 0 for the daemon default and answered as the DEFAULT here (the
// view states what the dispatcher will enforce).
type NotifySubscriptionView struct {
	ChannelID   string   `json:"channelId"`
	RootID      string   `json:"rootId,omitempty"`
	MinSeverity string   `json:"minSeverity,omitempty"`
	Kinds       []string `json:"kinds"`
	QuietStart  string   `json:"quietStart,omitempty"`
	QuietEnd    string   `json:"quietEnd,omitempty"`
	QuietZone   string   `json:"quietZone,omitempty"`
	MaxPerHour  int      `json:"maxPerHour"`
}

// NotifyChannelCreateParams: the secret arrives as a FILE PATH on the
// daemon host (C17 — never a value here, on argv, on stdin or in a
// prompt); subscriptions ride the create so one act configures a whole
// channel (each is applied after the row, audited as its own set).
type NotifyChannelCreateParams struct {
	Name          string                        `json:"name"`
	URL           string                        `json:"url"`
	SecretFile    string                        `json:"secretFile,omitempty"`
	AllowPrivate  bool                          `json:"allowPrivate,omitempty"`
	Subscriptions []NotifySubscriptionSetParams `json:"subscriptions,omitempty"`
}

// NotifyChannelUpdateParams edits in place; nil means do not touch (the
// routes.ts discipline). Replacing the secret is secretFile again — the
// file's CONTENT is the new secret, written to the secrets store and
// audited by name only.
type NotifyChannelUpdateParams struct {
	ID           string  `json:"id"`
	Name         *string `json:"name,omitempty"`
	URL          *string `json:"url,omitempty"`
	SecretFile   string  `json:"secretFile,omitempty"`
	AllowPrivate *bool   `json:"allowPrivate,omitempty"`
}

type NotifyChannelEnableParams struct {
	ID string `json:"id"`
}

type NotifyChannelDisableParams struct {
	ID string `json:"id"`
}

// NotifyChannelArchiveParams: archive is the only retirement — a
// channel is never deleted, its delivery log outliving it (SV12). The
// reason is required: the row will outlive the memory.
type NotifyChannelArchiveParams struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type NotifyChannelTestParams struct {
	ID string `json:"id"`
}

// NotifySubscriptionSetParams upserts the (channel, root) row. RootId
// omitted or empty means every root (the S40 convention). MinSeverity is
// the attention enum; Kinds names wire kinds (null = all); the quiet
// window is all-or-none with an explicit IANA zone (TR6).
type NotifySubscriptionSetParams struct {
	ChannelID   string    `json:"channelId"`
	RootID      string    `json:"rootId,omitempty"`
	MinSeverity string    `json:"minSeverity,omitempty"`
	Kinds       *[]string `json:"kinds,omitempty"`
	QuietStart  string    `json:"quietStart,omitempty"`
	QuietEnd    string    `json:"quietEnd,omitempty"`
	QuietZone   string    `json:"quietZone,omitempty"`
	MaxPerHour  int       `json:"maxPerHour,omitempty"`
}

type NotifyChannelListResult struct {
	Channels []NotifyChannelView `json:"channels"`
}

// NotifyTestResult is the test delivery as it landed.
type NotifyTestResult struct {
	Outcome    string `json:"outcome"`
	ErrorKind  string `json:"errorKind,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Attempts   int    `json:"attempts"`
	At         string `json:"at"`
}

// NotifyDeliveryListParams reads a channel's outcomes. IncludeArchived
// covers the log of an archived channel (no FK, history outlives the
// channel — the listing must be able to say so).
type NotifyDeliveryListParams struct {
	ChannelID       string `json:"channelId"`
	Limit           int    `json:"limit,omitempty"`
	IncludeArchived bool   `json:"includeArchived,omitempty"`
}

type NotifyDeliveryItem struct {
	ID           int64  `json:"id"`
	ChannelID    string `json:"channelId"`
	EventSeq     int64  `json:"eventSeq,omitempty"`
	AttentionKey string `json:"attentionKey,omitempty"`
	Outcome      string `json:"outcome"`
	ErrorKind    string `json:"errorKind,omitempty"`
	HTTPStatus   int    `json:"httpStatus,omitempty"`
	Attempts     int    `json:"attempts"`
	At           string `json:"at"`
}

type NotifyDeliveryListResult struct {
	Deliveries []NotifyDeliveryItem `json:"deliveries"`
}

// --- handlers -----------------------------------------------------------------

// channelIdentityKeys is SV13's wire rule: the actor is the peer, never
// a param (the class plane's rule, same sentence shape).
var channelIdentityKeys = []string{"actor", "actorKind", "who"}

func rejectChannelIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range channelIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — configuring where Aeon sends data is a person's act and the person is the peer the socket authenticated, never a name in the request (SV13)",
				method, key)}
		}
	}
	return nil
}

// channelPersonActor is the door's authority check (SV7 lock 2, driven
// by the socket's own peer stamp): an agent peer and an unattributed
// connection are both refused, in the plane's own sentence.
func (t *Table) channelPersonActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("configuring where Aeon sends data is a person's act and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("configuring where Aeon sends data is a person's act and no identity is available on this connection — the daemon stamped none")}
	}
	if a.Kind == "agent" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("configuring where Aeon sends data is a person's act — this connection acts as agent %q, and an agent may not author an exfiltration path (SV7)", a.ID)}
	}
	return a, nil
}

func (t *Table) handleNotifyChannelCreate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectChannelIdentityKeys("notify.channel.create", params); err != nil {
		return nil, err
	}
	var p NotifyChannelCreateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.URL) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.create needs both name and url")}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyChannel.NotifyChannelCreate(ctx, actor, p)
}

func (t *Table) handleNotifyChannelUpdate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectChannelIdentityKeys("notify.channel.update", params); err != nil {
		return nil, err
	}
	var p NotifyChannelUpdateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.update needs the channel id")}
	}
	if p.Name == nil && p.URL == nil && p.AllowPrivate == nil && p.SecretFile == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.update names nothing to change — pass name, url, allowPrivate or secretFile")}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyChannel.NotifyChannelUpdate(ctx, actor, p)
}

func (t *Table) handleNotifyChannelEnable(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return channelIDDoor(ctx, sess, t, "notify.channel.enable", params, t.NotifyChannel.NotifyChannelEnable)
}

func (t *Table) handleNotifyChannelDisable(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return channelIDDoor(ctx, sess, t, "notify.channel.disable", params, t.NotifyChannel.NotifyChannelDisable)
}

// channelIDDoor is enable/disable's shared shape: id in, person checked,
// one seam call.
func channelIDDoor(ctx context.Context, sess *Session, t *Table, method string, params json.RawMessage,
	call func(context.Context, Actor, string) (NotifyChannelView, error)) (any, error) {
	if err := rejectChannelIdentityKeys(method, params); err != nil {
		return nil, err
	}
	var p NotifyChannelEnableParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("%s needs the channel id", method)}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return call(ctx, actor, p.ID)
}

func (t *Table) handleNotifyChannelArchive(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectChannelIdentityKeys("notify.channel.archive", params); err != nil {
		return nil, err
	}
	var p NotifyChannelArchiveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.archive needs the channel id")}
	}
	if strings.TrimSpace(p.Reason) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.archive needs a reason — the row and its log will outlive the memory")}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyChannel.NotifyChannelArchive(ctx, actor, p)
}

func (t *Table) handleNotifyChannelTest(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectChannelIdentityKeys("notify.channel.test", params); err != nil {
		return nil, err
	}
	var p NotifyChannelTestParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.test needs the channel id")}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyChannel.NotifyChannelTest(ctx, actor, p.ID)
}

func (t *Table) handleNotifySubscriptionSet(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectChannelIdentityKeys("notify.subscription.set", params); err != nil {
		return nil, err
	}
	var p NotifySubscriptionSetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ChannelID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.subscription.set needs the channel id (notify.channel.list shows them)")}
	}
	actor, err := t.channelPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyChannel.NotifySubscriptionSet(ctx, actor, p)
}

func (t *Table) handleNotifyChannelList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return t.NotifyChannel.NotifyChannelList(ctx)
}

func (t *Table) handleNotifyChannelShow(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p NotifyChannelShowParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.channel.show needs the channel id")}
	}
	return t.NotifyChannel.NotifyChannelShow(ctx, p.ID)
}

// NotifyChannelShowParams is show's one-field params.
type NotifyChannelShowParams struct {
	ID string `json:"id"`
}

func (t *Table) handleNotifyDeliveryList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p NotifyDeliveryListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ChannelID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.delivery.list needs the channel id (notify.channel.list shows them)")}
	}
	if p.Limit < 0 || p.Limit > 500 {
		return nil, ErrDeliveryBad.WithWhat("the limit %d is outside 0 (the default 50)..500", p.Limit)
	}
	return t.NotifyChannel.NotifyDeliveryList(ctx, p)
}

// RegisterNotifyChannels registers the plane over a non-nil seam (V13).
// Every write passes the person-door gauntlet above; the reads need no
// actor because they answer configuration and history — but note they
// answer HOST and BOOLS, never a url or a secret, which is why they can
// stay reads even though the plane they read is authority.
func (t *Table) RegisterNotifyChannels() bool {
	if t.NotifyChannel == nil {
		return false
	}
	for _, m := range []*Method{
		{Name: MethodNotifyChannelCreate, RequiresInitialize: true, Handle: t.handleNotifyChannelCreate},
		{Name: MethodNotifyChannelUpdate, RequiresInitialize: true, Handle: t.handleNotifyChannelUpdate},
		{Name: MethodNotifyChannelEnable, RequiresInitialize: true, Handle: t.handleNotifyChannelEnable},
		{Name: MethodNotifyChannelDisable, RequiresInitialize: true, Handle: t.handleNotifyChannelDisable},
		{Name: MethodNotifyChannelArchive, RequiresInitialize: true, Handle: t.handleNotifyChannelArchive},
		{Name: MethodNotifyChannelTest, RequiresInitialize: true, Handle: t.handleNotifyChannelTest},
		{Name: MethodNotifySubscriptionSet, RequiresInitialize: true, Handle: t.handleNotifySubscriptionSet},
		{Name: MethodNotifyChannelList, RequiresInitialize: true, Handle: t.handleNotifyChannelList},
		{Name: MethodNotifyChannelShow, RequiresInitialize: true, Handle: t.handleNotifyChannelShow},
		{Name: MethodNotifyDeliveryList, RequiresInitialize: true, Handle: t.handleNotifyDeliveryList},
	} {
		t.register(m)
	}
	return true
}
