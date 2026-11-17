package daemon

// inbox.go — S58 T2: wiring the attention plane (SV1).
//
// The daemon is the one assembly point: the store under the three source
// readers, the component tree under the scope resolver and the owner maps,
// the protocol table's OWN method set under the door-wiring check (a door
// may name only a method THIS build registered — S19's F-1, checked against
// the live table and never a remembered list), and the T1 ack read behind
// the registry's Ack seam so an acknowledged item hides from the default
// view and reappears, counted, under includeAcked (SV3).
//
// # The sibling seam
//
// S55 (budget/stuck), S57 (gates) and S27 (scheduler) register their own
// sources through RegisterAttentionSource from their OWN files — parallel
// worktrees add files, not edits to this one (SD2). The registry's closed
// known-unwired list names their slots until they do, so the census in
// every inbox answer shows who has not reported rather than a silence that
// reads as an empty world.
//
// # Nothing is cached (N8)
//
// The registry holds SOURCES (stateless readers over the store), never
// items. Every inbox.list is a fresh evaluation against fresh rows; two
// reads straddling a state change differ, and no field on this service
// remembers an answer between them.

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/protocol"
)

// InboxService is the daemon's one inbox service (protocol.InboxService).
type InboxService struct {
	d        *Daemon
	registry *attention.Registry
}

// newInboxService builds the registry over the live store. Built only when
// the daemon has a store (the RegisterInbox nil seam is the same guard from
// the socket side; a storeless daemon leaves d.Inbox nil and both methods
// stay ABSENT, V13).
func (d *Daemon) newInboxService() *InboxService {
	if d.store == nil {
		return nil
	}
	st := d.store

	// The door-wiring check reads the LIVE table (lazily: the closure runs
	// per evaluation, so a method registered after this construction — a
	// later register block, a sibling slice's file — is wired the moment it
	// exists, and an absent one renders generic).
	wired := func(method string) bool {
		if d.Table == nil {
			return false
		}
		// Methods() is fresh per call; this is a handful of names per
		// evaluation, not a cache — building a set here would be the memo
		// N8 forbids.
		for _, m := range d.Table.Methods() {
			if string(m) == method {
				return true
			}
		}
		return false
	}

	registry := attention.NewRegistry(wired, func(keys []string) (map[string]attention.Ack, error) {
		marks, err := st.LatestAcks(keys)
		if err != nil {
			return nil, err
		}
		out := make(map[string]attention.Ack, len(marks))
		for k, m := range marks {
			switch m.Op {
			case "ack", "snooze":
				out[k] = attention.Ack{
					By:    m.ActorKind + ":" + m.ActorID,
					At:    m.OccurredAt,
					Until: m.Until,
				}
			} // "unack" (and any op a later migration declares) hides nothing
		}
		return out, nil
	})
	owners, err := attention.Owners(st)
	if err != nil {
		// The tree read failing at CONSTRUCTION would mean the store is
		// already unreadable; the sources' own per-evaluation reads surface
		// any real failure as `failed` states, so the service stands with
		// owner maps rebuilt per call below.
		owners = nil
	}
	svc := &InboxService{d: d, registry: registry}

	// The three sources T2 builds (SV1). The owners index is refreshed per
	// evaluation inside Items? No — sources hold the store; owners are
	// re-read per evaluation (scope changes are cheap and live): the sources
	// below take a live-computing wrapper.
	registry.Register(&liveOwnersSource{Inner: attention.QuestionsSource{Questions: st}, svc: svc, kind: "questions"})
	registry.Register(&blockedSourceWiring{Inner: attention.BlockedSource{Loops: st}, svc: svc})
	registry.Register(&stoppedSourceWiring{Inner: attention.StoppedSource{Loops: st}, svc: svc})
	registry.Register(&gatesSourceWiring{Inner: attention.GatesSource{Gates: st}, svc: svc})
	registry.Register(attention.ProposalsSource{Proposals: st})
	registry.Register(&triageSourceWiring{Inner: attention.TriageSource{Bugs: st}, svc: svc})
	_ = owners // the construction-time read above proved the tree; per-evaluation reads own the truth
	return svc
}

// RegisterAttentionSource is THE sibling seam (SD2): S55 registers
// budget/stuck, S57 registers gates, S27 registers scheduler, each from its
// own file, and the census in every inbox answer flips that name from
// `not-wired` to `reporting` with no edit to this slice.
func (d *Daemon) RegisterAttentionSource(s attention.Source) {
	if d.Inbox == nil {
		return // a storeless daemon has no inbox to contribute to
	}
	d.Inbox.registry.Register(s)
}

// liveOwners hands the current owner index (rebuilt per evaluation: the
// component tree is live state, and an owner map cached across evaluations
// is the cache N8 forbids).
func (svc *InboxService) liveOwners() *attention.OwnerMaps {
	om, err := attention.Owners(svc.d.store)
	if err != nil {
		return nil // items then carry no component ids rather than wrong ones; the read itself failed elsewhere and says so
	}
	return om
}

// The wiring wrappers re-point each source's Owners at the live index just
// before its read. Small, boring, and it keeps attention.Item assembly
// purely functional (the attention package never sees a store for owners).
type liveOwnersSource struct {
	Inner attention.QuestionsSource
	svc   *InboxService
	kind  string
}

func (s *liveOwnersSource) Name() string { return s.Inner.Name() }
func (s *liveOwnersSource) Items(ctx context.Context, q attention.Query) ([]attention.Item, error) {
	inner := s.Inner
	inner.Owners = s.svc.liveOwners()
	return inner.Items(ctx, q)
}

type blockedSourceWiring struct {
	Inner attention.BlockedSource
	svc   *InboxService
}

func (s *blockedSourceWiring) Name() string { return s.Inner.Name() }
func (s *blockedSourceWiring) Items(ctx context.Context, q attention.Query) ([]attention.Item, error) {
	inner := s.Inner
	inner.Owners = s.svc.liveOwners()
	return inner.Items(ctx, q)
}

type stoppedSourceWiring struct {
	Inner attention.StoppedSource
	svc   *InboxService
}

func (s *stoppedSourceWiring) Name() string { return s.Inner.Name() }
func (s *stoppedSourceWiring) Items(ctx context.Context, q attention.Query) ([]attention.Item, error) {
	inner := s.Inner
	inner.Owners = s.svc.liveOwners()
	return inner.Items(ctx, q)
}

type gatesSourceWiring struct {
	Inner attention.GatesSource
	svc   *InboxService
}

func (s *gatesSourceWiring) Name() string { return s.Inner.Name() }
func (s *gatesSourceWiring) Items(ctx context.Context, q attention.Query) ([]attention.Item, error) {
	inner := s.Inner
	inner.Owners = s.svc.liveOwners()
	return inner.Items(ctx, q)
}

type triageSourceWiring struct {
	Inner attention.TriageSource
	svc   *InboxService
}

func (s *triageSourceWiring) Name() string { return s.Inner.Name() }
func (s *triageSourceWiring) Items(ctx context.Context, q attention.Query) ([]attention.Item, error) {
	inner := s.Inner
	inner.Owners = s.svc.liveOwners()
	return inner.Items(ctx, q)
}

// InboxList evaluates the registry (SV1) and maps to the wire shape.
func (svc *InboxService) InboxList(ctx context.Context, p protocol.InboxListParams) (protocol.InboxListResult, error) {
	scope, err := svc.scopeFor(p.RootID)
	if err != nil {
		return protocol.InboxListResult{}, err
	}
	q := attention.Query{
		Scope:        scope,
		IncludeAcked: p.IncludeAcked,
		Limit:        p.Limit,
	}
	if p.Severity != "" {
		q.Severities = []attention.Severity{attention.Severity(p.Severity)}
	}
	if len(p.Kinds) > 0 {
		q.Kinds = p.Kinds
	}
	res, err := svc.registry.Evaluate(ctx, q)
	if err != nil {
		return protocol.InboxListResult{}, err
	}
	return protocol.InboxListResult{
		Items:       mapItems(res.Items),
		Sources:     mapSources(res.Sources),
		EvaluatedAt: res.EvaluatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// InboxSummary is the same evaluation counted, with acked items INCLUDED
// (SV3: the total never lies about what a person already saw; hiding is a
// view, not a fact).
func (svc *InboxService) InboxSummary(ctx context.Context, p protocol.InboxSummaryParams) (protocol.InboxSummaryResult, error) {
	scope, err := svc.scopeFor(p.RootID)
	if err != nil {
		return protocol.InboxSummaryResult{}, err
	}
	res, err := svc.registry.Evaluate(ctx, attention.Query{
		Scope:        scope,
		IncludeAcked: true,
		Limit:        attention.MaxLimit,
	})
	if err != nil {
		return protocol.InboxSummaryResult{}, err
	}
	out := protocol.InboxSummaryResult{
		Total:      len(res.Items),
		BySeverity: map[string]int{},
		Sources:    mapSources(res.Sources),
	}
	var oldest string
	var oldestT time.Time
	for _, it := range res.Items {
		if it.Acked != nil {
			out.Acked++ // hidden by a live mark — counted, named (SV3)
		}
		out.BySeverity[string(it.Severity)]++
		if t, ok := parseRFC3339(it.Since); ok && (oldest == "" || t.Before(oldestT)) {
			oldest, oldestT = it.Since, t
		}
	}
	out.OldestSince = oldest
	return out, nil
}

// scopeFor resolves the optional rootId (SV1: every tree by default; a
// component's tree by id; an unknown id is attention.ErrUnknownScope, which
// the protocol handler renders as ErrInboxScope).
func (svc *InboxService) scopeFor(rootID string) (attention.Scope, error) {
	if rootID == "" {
		return attention.EveryScope(svc.d.store)
	}
	return attention.ResolveScope(svc.d.store, rootID)
}

func mapItems(items []attention.Item) []protocol.AttentionItem {
	out := make([]protocol.AttentionItem, 0, len(items))
	for _, it := range items {
		w := protocol.AttentionItem{
			Key:         it.Key,
			Kind:        it.Kind,
			Severity:    string(it.Severity),
			RootID:      it.RootID,
			ComponentID: it.ComponentID,
			LoopID:      it.LoopID,
			LoopSlug:    it.LoopSlug,
			ItemRef:     it.ItemRef,
			Reason:      it.Reason,
			Since:       it.Since,
			AgeSeconds:  it.AgeSeconds,
			Door:        protocol.InboxDoor{Method: it.DoorMethod, Sentence: it.DoorSentence},
			Prompt:      it.Prompt, // human door only (SV6) — inbox.list IS the human door
		}
		if it.Acked != nil {
			w.Acked = &protocol.InboxAcked{By: it.Acked.By, At: it.Acked.At, Until: it.Acked.Until}
		}
		out = append(out, w)
	}
	return out
}

func mapSources(states []attention.SourceStatus) []protocol.InboxSourceState {
	out := make([]protocol.InboxSourceState, 0, len(states))
	for _, s := range states {
		out = append(out, protocol.InboxSourceState{Name: s.Name, State: s.State, Error: s.Error})
	}
	return out
}

func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
