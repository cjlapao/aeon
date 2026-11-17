package daemon

// records_supervision.go — S58 T6 (SV16): the supervision family's five
// catalog reads, answered against the daemon's own services.
//
// # Why one file and one arm
//
// The catalog routes by name; the five actions share a door, a view rule
// (own-view for the three scope verbs) and a refusal sentence, so they are
// one concern. `Do` routes the family here and never sees the shape again.
//
// # Own view, by identity not by parameter (SV-D3)
//
// An agent calling `inbox.list`, `inbox.summary` or `mission.status` gets
// ITS ROOT's tree: no rootId is its own root (the identity's, resolved
// from the step — never a claimed param, the actor-not-param rule the ack
// writes set), and a rootId naming any other tree is refused with one
// sentence. There is no "all trees" for an agent and no way to ask; the
// person's half (OL10: Identity.User) answers freely, because the person
// owns the org.
//
// # What an agent never sees
//
// Channel answers are a projection: name, host, enabled, last outcome,
// consecutive failures — never a URL path (not even beyond the host),
// never the `secret:notify/<id>` reference, never a subscription. The
// class table carries no secrets by construction and rides whole. No
// supervision WRITE exists here: acking is a person's act (SV12) and the
// catalog is the whole vocabulary, so the absence is a fact, not an
// omission.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/protocol"
)

// supervisionFamily names the catalog actions this file answers — the
// routing table and the parity test read the same map.
var supervisionFamily = map[string]bool{
	"inbox_list":          true,
	"inbox_summary":       true,
	"mission_status":      true,
	"notify_class_list":   true,
	"notify_channel_list": true,
}

// SupervisionDoor is the seam the daemon injects at wiring (the run-door
// pattern): the five reads, already the socket's own services. Nil on a
// bare service — a storeless mount has no supervision plane to read.
type SupervisionDoor interface {
	InboxList(ctx context.Context, p protocol.InboxListParams) (protocol.InboxListResult, error)
	InboxSummary(ctx context.Context, p protocol.InboxSummaryParams) (protocol.InboxSummaryResult, error)
	MissionStatus(ctx context.Context, p protocol.MissionStatusParams) (protocol.MissionStatusResult, error)
	NotifyClassList(ctx context.Context) (protocol.NotifyClassListResult, error)
	NotifyChannelList(ctx context.Context) (protocol.NotifyChannelListResult, error)
}

// ownViewSentence is THE refusal the own-view rule speaks (guard-tested
// verbatim; a paraphrase would be a second fact about the same rule).
const ownViewSentence = "refused: supervision reads answer your own root's tree — the rootId named belongs to a different tree"

// supervisionRead answers the family's five actions. The bool says the
// name belongs to the family at all; the Failure is a refusal, never a
// half-answer.
func (r *RecordsService) supervisionRead(ctx context.Context, name string, ident Identity, input json.RawMessage) (any, *Failure, error, bool) {
	if !supervisionFamily[name] {
		return nil, nil, nil, false
	}
	if r.supervision == nil {
		return nil, &Failure{Sentence: fmt.Sprintf("refused: %s needs the supervision plane and this daemon does not carry one", name)}, nil, true
	}
	// ownScope maps a requested rootId onto the caller's tree (agent) or
	// through untouched (the person, OL10).
	ownScope := func(rootID string) (string, *Failure) {
		if ident.User {
			return rootID, nil
		}
		if rootID == "" {
			if ident.RootID == "" {
				return "", &Failure{Sentence: "refused: this caller carries no root to answer for"}
			}
			return ident.RootID, nil
		}
		if rootID != ident.RootID {
			return "", &Failure{Sentence: ownViewSentence}
		}
		return rootID, nil
	}
	switch name {
	case "inbox_list":
		var in struct {
			RootID       string   `json:"root_id"`
			Severity     string   `json:"severity"`
			Kinds        []string `json:"kinds"`
			IncludeAcked bool     `json:"include_acked"`
			Limit        int      `json:"limit"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err, true
		}
		scope, f := ownScope(in.RootID)
		if f != nil {
			return nil, f, nil, true
		}
		res, err := r.supervision.InboxList(ctx, protocol.InboxListParams{
			RootID: scope, Severity: in.Severity, Kinds: in.Kinds,
			IncludeAcked: in.IncludeAcked, Limit: in.Limit,
		})
		if err != nil {
			return nil, nil, err, true
		}
		return res, nil, nil, true
	case "inbox_summary":
		var in struct {
			RootID string `json:"root_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err, true
		}
		scope, f := ownScope(in.RootID)
		if f != nil {
			return nil, f, nil, true
		}
		res, err := r.supervision.InboxSummary(ctx, protocol.InboxSummaryParams{RootID: scope})
		if err != nil {
			return nil, nil, err, true
		}
		return res, nil, nil, true
	case "mission_status":
		var in struct {
			RootID string `json:"root_id"`
		}
		if f, err := decode(input, &in); f != nil || err != nil {
			return nil, f, err, true
		}
		scope, f := ownScope(in.RootID)
		if f != nil {
			return nil, f, nil, true
		}
		res, err := r.supervision.MissionStatus(ctx, protocol.MissionStatusParams{RootID: scope})
		if err != nil {
			return nil, nil, err, true
		}
		return res, nil, nil, true
	case "notify_class_list":
		res, err := r.supervision.NotifyClassList(ctx)
		if err != nil {
			return nil, nil, err, true
		}
		return res, nil, nil, true
	case "notify_channel_list":
		res, err := r.supervision.NotifyChannelList(ctx)
		if err != nil {
			return nil, nil, err, true
		}
		out := make([]agentChannelView, 0, len(res.Channels))
		for _, c := range res.Channels {
			if c.ArchivedAt != "" {
				continue // an archived channel is off the plane; the person's door still lists it
			}
			out = append(out, agentChannelView{
				ID: c.ID, Name: c.Name, Kind: c.Kind, Host: c.Host,
				Enabled: c.Enabled, LastOutcome: c.LastOutcome,
				ConsecutiveFailures: c.ConsecutiveFailures,
			})
		}
		return out, nil, nil, true
	}
	// The map and the switch have drifted — loud, the same stance as the
	// catalog's unimplemented default.
	return nil, &Failure{Sentence: fmt.Sprintf("refused: %s is in the supervision family but not implemented here", name)}, nil, true
}

// agentChannelView is the SV16 projection: the facts an agent may know
// about where notifications go, and nothing else. No URL (a full URL, a
// path, a query), no secret reference, no subscription detail.
type agentChannelView struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Kind                string `json:"kind"`
	Host                string `json:"host"`
	Enabled             bool   `json:"enabled"`
	LastOutcome         string `json:"lastOutcome,omitempty"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

// daemonSupervision is the adapter the daemon injects. Every method
// delegates at CALL time to the daemon's live services (nil-safe), so the
// adapter can be wired before those services are — the wiring order stays
// the store's, not this file's problem.
type daemonSupervision struct{ d *Daemon }

func (s *daemonSupervision) InboxList(ctx context.Context, p protocol.InboxListParams) (protocol.InboxListResult, error) {
	if s.d.Inbox == nil {
		return protocol.InboxListResult{}, errNoSupervision("inbox.list")
	}
	return s.d.Inbox.InboxList(ctx, p)
}

func (s *daemonSupervision) InboxSummary(ctx context.Context, p protocol.InboxSummaryParams) (protocol.InboxSummaryResult, error) {
	if s.d.Inbox == nil {
		return protocol.InboxSummaryResult{}, errNoSupervision("inbox.summary")
	}
	return s.d.Inbox.InboxSummary(ctx, p)
}

func (s *daemonSupervision) MissionStatus(ctx context.Context, p protocol.MissionStatusParams) (protocol.MissionStatusResult, error) {
	if s.d.missionSvc == nil {
		return protocol.MissionStatusResult{}, errNoSupervision("mission.status")
	}
	return s.d.missionSvc.MissionStatus(ctx, p)
}

func (s *daemonSupervision) NotifyClassList(ctx context.Context) (protocol.NotifyClassListResult, error) {
	if s.d.store == nil {
		return protocol.NotifyClassListResult{}, errNoSupervision("notify.class.list")
	}
	svc := &NotifyClassService{store: s.d.store, resolver: s.d.classes}
	return svc.NotifyClassList(ctx)
}

func (s *daemonSupervision) NotifyChannelList(ctx context.Context) (protocol.NotifyChannelListResult, error) {
	if s.d.channels == nil {
		return protocol.NotifyChannelListResult{}, errNoSupervision("notify.channel.list")
	}
	return s.d.channels.NotifyChannelList(ctx)
}

func errNoSupervision(method string) error {
	return fmt.Errorf("the daemon carries no supervision plane to answer %s", method)
}
