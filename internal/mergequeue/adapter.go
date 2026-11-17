package mergequeue

import (
	"fmt"
	"github.com/cjlapao/aeon/internal/store"
)

// adapter.go — the Service: the ONE front door the daemon's driver (T7),
// the protocol methods (T8), the blueprint action (T6) and the proposal
// commit (T9) all share. Every write runs the pure guards FIRST
// (ValidateEnqueue, Legal) and then the store's guarded method — the store
// is the last line, never the first, and the service NEVER retries a lost
// race blindly: a stale-state refusal travels back verbatim (the caller's
// facts are old; deciding again is the caller's job, not a loop's).
//
// The actor is a parameter of every method (the callers take it from the
// peer credential or the blueprint pin — P4, "never from params" means the
// service must never invent one).

// mergequeueStore is the narrow seam: exactly T2's nine store methods, as
// they are. (*store.Store) satisfies it; a fake answers the service's tests.
type mergequeueStore interface {
	EnqueueMergeItem(in store.NewMergeItem, actor store.MergeActor) (store.MergeItem, error)
	ReadMergeItem(id string) (store.MergeItem, error)
	ListMergeItems(f store.MergeFilter) ([]store.MergeItem, error)
	TransitionMergeItem(id, from, to string, facts store.TransitionFacts, actor store.MergeActor) (store.MergeItem, error)
	SetMergeItemFields(id, guard string, fields store.MergeFields, actor store.MergeActor) (store.MergeItem, error)
	ReorderMergeItems(id, beforeID string, actor store.MergeActor) (store.MergeItem, error)
	ReadMergeConfig(componentRoot string) (store.MergeConfig, error)
	WriteMergeConfig(in store.NewMergeConfig, actor store.MergeActor) (store.MergeConfig, error)
	ListMergeQueueEvents(itemID string) ([]store.MergeQueueEvent, error)
}

// Service is the queue's application layer.
type Service struct {
	st mergequeueStore
}

// NewService wires the seam.
func NewService(st mergequeueStore) *Service { return &Service{st: st} }

// ToItem maps a store row onto the pure Item the decisions and describer
// read — the ONLY direction the mapping goes (the core never imports
// shapes back).
func ToItem(m store.MergeItem) Item {
	return Item{
		ID:            m.ID,
		ComponentRoot: m.ComponentRoot,
		BaseBranch:    m.BaseBranch,
		Branch:        m.Branch,
		Status:        Status(m.Status),
		Position:      int(m.Position),
		Attended:      m.Attended,
		EndReason:     m.ReasonKind,
	}
}

// ConfigOf maps a store config row onto the pure Config (the store's
// Absent row already reads as the defaults).
func ConfigOf(c store.MergeConfig) Config {
	return Config{
		Mode:           Mode(c.Mode),
		ConflictPolicy: ConflictPolicy(c.ConflictPolicy),
		LandMethod:     LandMethod(c.LandMethod),
		MaxCatchUps:    c.MaxCatchUps,
	}
}

// Enqueue is the shared path of the three doors (MQ14). It is
// naturally-idempotent: a branch with an open item REUSES that row and
// answers it (the blueprint action may fire twice; the person's repeat is
// not an error). Fresh rows pass ValidateEnqueue first — the socket, the
// action and the proposal commit differ only in the actor and the
// attended default they bring, never in a check.
func (s *Service) Enqueue(req EnqueueRequest, facts RepoFacts, enq store.NewMergeItem, actor store.MergeActor) (store.MergeItem, error) {
	open, err := s.st.ListMergeItems(store.MergeFilter{ComponentRoot: req.ComponentRoot, Statuses: store.MergeOpenStatusSet})
	if err != nil {
		return store.MergeItem{}, err
	}
	for _, m := range open {
		if m.Branch == req.Branch {
			return m, nil // idempotent reuse — the open row IS the answer
		}
	}
	facts.OpenBranches = nil // reuse was answered above; the validator's last check guards any direct caller
	if err := ValidateEnqueue(req, facts); err != nil {
		return store.MergeItem{}, err
	}
	return s.st.EnqueueMergeItem(enq, actor)
}

// Dequeue is the MQ5 "open | person dequeues" edge: guarded straight to
// removed(removed-by-person). A terminal item refuses with the legal-moves
// sentence — the same words a wrong move always gets.
func (s *Service) Dequeue(id string, actor store.MergeActor) (store.MergeItem, error) {
	m, err := s.st.ReadMergeItem(id)
	if err != nil {
		return store.MergeItem{}, err
	}
	return s.Transition(id, StatusRemoved, store.TransitionFacts{Reason: string(ReasonRemovedByPerson)}, actor, m)
}

// Retry is the failed→queued edge, and only that edge: the service reads
// the row so the pure Legal guard sees the REAL status (the store's guard
// still races-checks it underneath).
func (s *Service) Retry(id string, actor store.MergeActor) (store.MergeItem, error) {
	m, err := s.st.ReadMergeItem(id)
	if err != nil {
		return store.MergeItem{}, err
	}
	// A retry ANSWERS a failure; it is not a way to bounce a mid-flight
	// item (the driver's base-move bounce uses Transition directly with
	// its facts). The pure Legal guard plus this door own the semantics.
	if Status(m.Status) != StatusFailed {
		return store.MergeItem{}, &Refusal{Kind: "not-retryable",
			Reason: fmt.Sprintf("item %s is %s — a retry answers a failure; only failed items carry one", id, m.Status)}
	}
	return s.Transition(id, StatusQueued, store.TransitionFacts{}, actor, m)
}

// Reorder answers to the store — the queued-only guard and the gap
// arithmetic are T2's, and they refuse (no-op-free) in the same shape this
// service uses everywhere.
func (s *Service) Reorder(id, beforeID string, actor store.MergeActor) (store.MergeItem, error) {
	return s.st.ReorderMergeItems(id, beforeID, actor)
}

// Settings writes the repository's queue settings and returns them with
// the Statement sentence the door must render (authority-adjacent writes
// are shown, never silently applied).
func (s *Service) Settings(in store.NewMergeConfig, actor store.MergeActor) (store.MergeConfig, string, error) {
	cfg, err := s.st.WriteMergeConfig(in, actor)
	if err != nil {
		return store.MergeConfig{}, "", err
	}
	return cfg, Statement(Settings{
		ComponentRoot:  cfg.ComponentRoot,
		Mode:           Mode(cfg.Mode),
		ConflictPolicy: ConflictPolicy(cfg.ConflictPolicy),
		LandMethod:     LandMethod(cfg.LandMethod),
		MaxCatchUps:    cfg.MaxCatchUps,
	}), nil
}

// Transition is EVERY driver-side move through the same door: it reads the
// row, asks Legal (the pure table refuses what MQ5 does not carry, with
// the sentence naming the legal moves), and hands the guarded store write
// the from-status it saw. A lost race returns the store's stale-state
// refusal VERBATIM — no blind retry: the world moved, and re-deciding is
// the reactor's job on the next event, not this call's.
func (s *Service) Transition(id string, to Status, facts store.TransitionFacts, actor store.MergeActor, seen ...store.MergeItem) (store.MergeItem, error) {
	m := store.MergeItem{}
	if len(seen) > 0 {
		m = seen[0]
	} else {
		var err error
		if m, err = s.st.ReadMergeItem(id); err != nil {
			return store.MergeItem{}, err
		}
	}
	from := Status(m.Status)
	if !Legal(from, to) {
		return store.MergeItem{}, &Refusal{Kind: "illegal-move", Reason: RefusalSentence("item "+id, from, to)}
	}
	out, err := s.st.TransitionMergeItem(id, m.Status, string(to), facts, actor)
	if err != nil {
		// Every store refusal — stale-state above all — travels back
		// VERBATIM, never retried: the caller's facts are old by proof, and
		// re-deciding is the reactor's job on the next event, not this
		// call's.
		return store.MergeItem{}, err
	}
	return out, nil
}

// Decide answers Next for a row against freshly gathered facts — the
// convenience the reactor uses so "read, decide" is one call; obeying the
// answer is still the reactor's (obey == Transition).
func (s *Service) Decide(id string, f Facts) (Item, Decision, error) {
	m, err := s.st.ReadMergeItem(id)
	if err != nil {
		return Item{}, Decision{}, err
	}
	item := ToItem(m)
	return item, Next(item, f), nil
}

// Events reads an item's history — the append-only trail, oldest first.
func (s *Service) Events(id string) ([]store.MergeQueueEvent, error) {
	return s.st.ListMergeQueueEvents(id)
}

// Show composes the person's one-sentence view: the item, its standing,
// the settings sentence behind it.
func (s *Service) Show(id string) (Item, string, error) {
	m, err := s.st.ReadMergeItem(id)
	if err != nil {
		return Item{}, "", err
	}
	item := ToItem(m)
	return item, Describe(item), nil
}
