package protocol

// notify_class.go — S58 T4: the event-class table's doors (SV4, SV13).
//
// Two reads and one write. `notify.class.list` and `notify.vocabulary` are
// the descriptor half the Settings panel drives (SV18: descriptor-driven,
// no hardcoded vocabulary in the app); `notify.class.update` is the ONE
// write a person holds over what notifies and how it reads — enabled,
// tone, and templates inside the closed placeholder set (SV6's door).
//
// # The person, not the agent (SV13, SV4)
//
// What notifies a person is that person's decision. The update refuses an
// agent peer and an unattributed connection exactly as the inbox writes
// do; the actor is the peer the socket authenticated, never a param. The
// list and the vocabulary are reads; an agent's read TOOL for the list
// arrives with T6's catalog (SV16), not here.
//
// # Absence (V13)
//
// All three register only over a non-nil NotifyClass seam.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NotifyClassService is the daemon seam over the class table. The update's
// actor arrives already validated as a person by the handler.
type NotifyClassService interface {
	// NotifyClassList reads the whole table (it is small by construction).
	NotifyClassList(ctx context.Context) (NotifyClassListResult, error)
	// NotifyClassUpdate applies one changing edit: the service validates
	// the templates against the closed set (notify.ValidateTemplate),
	// writes through the store's one audited door, and returns the row as
	// it now reads. A no-op edits nothing and earns no audit row.
	NotifyClassUpdate(ctx context.Context, actor Actor, p NotifyClassUpdateParams) (NotifyClassItem, error)
	// NotifyVocabulary is the plane's descriptor (SV13): everything the app
	// needs to render the editor without inventing a vocabulary of its own.
	NotifyVocabulary(ctx context.Context) (NotifyVocabularyResult, error)
}

// --- wire types ---------------------------------------------------------------

// NotifyClassItem is one class row as the wire carries it. Origin is the
// row's provenance ('system' for a seeded row, 'user' for an authored
// one); the VERSION is the seed engine's counter, and an edited system row
// keeps its origin while the LEDGER marks the divergence — the app shows
// "edited" from origin+version together with the seed census.
type NotifyClassItem struct {
	Kind          string `json:"kind"`
	Variant       string `json:"variant,omitempty"`
	Tone          string `json:"tone"`
	TitleTemplate string `json:"titleTemplate"`
	BodyTemplate  string `json:"bodyTemplate,omitempty"`
	Enabled       bool   `json:"enabled"`
	Origin        string `json:"origin"`
	Version       int    `json:"version"`
	UpdatedAt     string `json:"updatedAt"`
}

// NotifyClassListResult carries the whole table, ordered (kind, variant).
type NotifyClassListResult struct {
	Classes []NotifyClassItem `json:"classes"`
}

// NotifyClassUpdateParams names the row (kind + optional variant) and the
// fields to change — present-and-different is what earns the audit row;
// an absent field keeps its value. There is no way to CREATE a class here:
// kinds are a closed list and the shipped set is the table (the seed
// engine is the only author of rows; SV4).
type NotifyClassUpdateParams struct {
	Kind    string  `json:"kind"`
	Variant string  `json:"variant,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
	Tone    *string `json:"tone,omitempty"`
	// Title and Body are TEMPLATE strings — validated at the door against
	// the closed placeholder set (notify.ValidateTemplate) before any row
	// is touched (SV4: nothing invalid is persisted).
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

// NotifyVocabularyResult is the descriptor (SV13). Every list is closed
// and comes from the daemon: the app renders them and holds no copy.
type NotifyVocabularyResult struct {
	// Kinds is the class table's distinct kinds — what actually notifies.
	Kinds []string `json:"kinds"`
	// Severities is the attention enum the dispatcher filters on (T5).
	Severities []string `json:"severities"`
	// Placeholders is the closed template set (SV4).
	Placeholders []string `json:"placeholders"`
	// Tones is protocol.NotifyTone's four values.
	Tones []string `json:"tones"`
	// ErrorKinds is the delivery log's closed error enum (SV8).
	ErrorKinds []string `json:"errorKinds"`
	// ChannelKinds is the channel CHECK set ('webhook' today).
	ChannelKinds []string `json:"channelKinds"`
	// Limits are the numbers the dispatcher and validator keep (SV4, SV8).
	Limits NotifyLimits `json:"limits"`
}

// NotifyLimits states the plane's constants so the editor warns before a
// refusal and the delivery screens render what the daemon will do.
type NotifyLimits struct {
	TitleMaxBytes     int `json:"titleMaxBytes"`
	BodyMaxBytes      int `json:"bodyMaxBytes"`
	DedupMinutes      int `json:"dedupMinutes"`
	DefaultMaxPerHour int `json:"defaultMaxPerHour"`
	MaxPerHourFloor   int `json:"maxPerHourFloor"`
	MaxPerHourCeiling int `json:"maxPerHourCeiling"`
	SendTimeoutSecs   int `json:"sendTimeoutSecs"`
	Attempts          int `json:"attempts"`
	QueueDepth        int `json:"queueDepth"`
}

// --- handlers -----------------------------------------------------------------

// notifyClassIdentityKeys is the SV12 wire rule for this plane's write:
// the actor is the peer, never a param (the inbox writes' shape).
var notifyClassIdentityKeys = []string{"actor", "actorKind", "who"}

func rejectNotifyClassIdentityKeys(method string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return nil // decodeParams reports the malformed envelope in its own words
	}
	for _, key := range notifyClassIdentityKeys {
		if _, has := bag[key]; has {
			return ErrBadParams{Err: fmt.Errorf(
				"%s params carry %q — what notifies is a person's decision and the person is the peer the socket authenticated, never a name in the request (SV13)",
				method, key)}
		}
	}
	return nil
}

// notifyClassPersonActor resolves the acting identity (SV13): the update
// belongs to a person at the endpoint. An agent peer is refused.
func (t *Table) notifyClassPersonActor(ctx context.Context, sess *Session) (Actor, error) {
	if t.ActorFor == nil {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("what notifies is a person's decision and no identity is available on this connection — the daemon stamped none")}
	}
	a := t.ActorFor(ctx, sess)
	if strings.TrimSpace(a.ID) == "" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("what notifies is a person's decision and no identity is available on this connection — the daemon stamped none")}
	}
	if a.Kind == "agent" {
		return Actor{}, ErrBadParams{Err: fmt.Errorf("what notifies is a person's decision — this connection acts as agent %q, and an agent cannot decide what reaches a person (SV13)", a.ID)}
	}
	return a, nil
}

func (t *Table) handleNotifyClassList(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return t.NotifyClass.NotifyClassList(ctx)
}

func (t *Table) handleNotifyClassUpdate(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	if err := rejectNotifyClassIdentityKeys("notify.class.update", params); err != nil {
		return nil, err
	}
	var p NotifyClassUpdateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Kind) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.class.update needs the kind of the class (notify.class.list shows the table)")}
	}
	if p.Tone != nil {
		switch NotifyTone(*p.Tone) {
		case NotifyInfo, NotifySuccess, NotifyWarning, NotifyDanger:
		default:
			return nil, ErrBadParams{Err: fmt.Errorf("notify.class.update tone %q is not one of: info, success, warning, danger", *p.Tone)}
		}
	}
	if p.Enabled == nil && p.Tone == nil && p.Title == nil && p.Body == nil {
		return nil, ErrBadParams{Err: fmt.Errorf("notify.class.update names nothing to change — pass enabled, tone, title or body")}
	}
	actor, err := t.notifyClassPersonActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.NotifyClass.NotifyClassUpdate(ctx, actor, p)
}

func (t *Table) handleNotifyVocabulary(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return t.NotifyClass.NotifyVocabulary(ctx)
}

// RegisterNotifyClasses registers the plane over a non-nil seam (V13).
func (t *Table) RegisterNotifyClasses() bool {
	if t.NotifyClass == nil {
		return false
	}
	t.register(&Method{
		Name:               MethodNotifyClassList,
		RequiresInitialize: true,
		Handle:             t.handleNotifyClassList,
	})
	t.register(&Method{
		Name:               MethodNotifyClassUpdate,
		RequiresInitialize: true,
		Handle:             t.handleNotifyClassUpdate,
	})
	t.register(&Method{
		Name:               MethodNotifyVocabulary,
		RequiresInitialize: true,
		Handle:             t.handleNotifyVocabulary,
	})
	return true
}
