// mergerule_service.go — S57 T8 stage 4: the daemon's merge plane — the
// eight methods of protocol.MergeRuleStore over the real store: the rule's
// five human doors (get · preview · changes · vocabulary · set · clear —
// get and friends read, set and clear write) plus the two merge-plane
// reads (events · watches).
//
// The adapter's jobs, and only these: translate wire shapes to store
// shapes, strip the store's "refused: " prefix into a refusal's Detail
// (the protocol renders the family's own prefix — see verify_service.go),
// and resolve the scopes the store cannot see: a loop slug to its root
// (unique per root, never global — unknown and ambiguous are typed
// refusals, not empty lists), and the preview's loop to the run at the PR
// head (else the root's single loop, else nothing — the DoD stays empty
// and the evaluation says dod-none honestly).
//
// mergerule.preview is the gate's question asked read-only: the SAME
// rule and the SAME Evaluate the merge's gate calls (TestMergeRulePreview
// MatchesTheGate), over the PR's LIVE forge facts, three-state — allowed,
// refused with every sentence, or cannot-say when the live facts cannot
// be gathered (no PR named, no forge identity, the read failed). A
// cannot-say never reads as an allow.

package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// MergeRuleService is the daemon's merge-plane half (see the file header).
type MergeRuleService struct {
	st  *store.Store
	log Logger
}

// NewMergeRuleService answers nil for a storeless daemon (V13) — the
// wiring leaves table.MergeRule nil and RegisterMergeRule leaves the eight
// ABSENT, not registered-and-refusing.
func NewMergeRuleService(st *store.Store, log Logger) *MergeRuleService {
	if st == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return &MergeRuleService{st: st, log: log}
}

var _ protocol.MergeRuleStore = (*MergeRuleService)(nil)

// --- the rule's doors -------------------------------------------------------------

// Get answers mergerule.get: the component's OWN row when effective is
// false (source "none" when it has no row — an honest absence, not an
// error), or the EFFECTIVE rule (own, else the root's, else the baseline)
// with its source, its last change and the autonomy floor — each missing
// ingredient named.
func (s *MergeRuleService) Get(ctx context.Context, componentID string, effective bool) (protocol.MergeRuleGetResult, error) {
	if _, err := s.component("merge rule get", componentID); err != nil {
		return protocol.MergeRuleGetResult{}, err
	}
	last, err := s.lastChange(componentID)
	if err != nil {
		return protocol.MergeRuleGetResult{}, err
	}
	if !effective {
		rule, err := s.st.GetMergeRule(componentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return protocol.MergeRuleGetResult{ComponentID: componentID, Source: "none", LastChange: last}, nil
			}
			return protocol.MergeRuleGetResult{}, err
		}
		v := mergeRuleView(componentID, rule, "own")
		return protocol.MergeRuleGetResult{ComponentID: componentID, Source: "own", Rule: &v, LastChange: last}, nil
	}
	rule, source, err := s.st.EffectiveMergeRule(componentID)
	if err != nil {
		return protocol.MergeRuleGetResult{}, err
	}
	v := mergeRuleView(componentID, rule, source)
	floor := s.floor(componentID, rule)
	return protocol.MergeRuleGetResult{ComponentID: componentID, Source: source, Rule: &v, LastChange: last, Floor: floor}, nil
}

// Preview answers mergerule.preview: the effective rule with its source
// and floor, and — when a PR is named — the read-only dry evaluation over
// the PR's LIVE facts. Without a PR the verdict is nil and CannotSay
// carries the honest sentence (the gate's question has no PR to ask
// about). The evaluation is the merge gate's own — same rule shape, same
// Evaluate, same evidence keys — so a preview and a gate never drift.
func (s *MergeRuleService) Preview(ctx context.Context, componentID string, pr int) (protocol.MergeRulePreviewResult, error) {
	comp, err := s.component("merge rule preview", componentID)
	if err != nil {
		return protocol.MergeRulePreviewResult{}, err
	}
	rule, source, err := s.st.EffectiveMergeRule(componentID)
	if err != nil {
		return protocol.MergeRulePreviewResult{}, err
	}
	v := mergeRuleView(componentID, rule, source)
	res := protocol.MergeRulePreviewResult{ComponentID: componentID, Source: source, Rule: &v, Floor: s.floor(componentID, rule)}

	if pr <= 0 {
		res.CannotSay = "a preview without a PR cannot be evaluated — the gate's question (is this merge allowed) needs the PR it would judge; name a PR"
		return res, nil
	}
	f := forgeClientForRoot(comp.Root)
	if f == nil {
		res.CannotSay = "the root's origin has no forge identity this daemon can read — the preview has no live facts to ask about (the local half stands on its own)"
		return res, nil
	}
	prf, err := f.ReadPR(ctx, pr)
	if err != nil {
		res.CannotSay = fmt.Sprintf("PR %d cannot be read: %v — the preview needs the PR's head to gather the gate's facts", pr, err)
		return res, nil
	}
	facts, err := action.GatherGateFacts(ctx, f, pr, prf.HeadSHA)
	if err != nil {
		// The gatherer's sentence is already the refusal's sentence —
		// printed verbatim, the same as the merge gate prints it.
		res.CannotSay = err.Error()
		return res, nil
	}
	if rule.Enabled && !rule.IsBaseline() {
		s.fillPreviewFacts(&facts, comp.Root, prf.HeadSHA)
	}
	verdict := mergerule.Evaluate(rule, facts)
	res.Verdict = &protocol.VerdictView{Allowed: verdict.Allowed, Evidence: verdict.Evidence}
	for _, r := range verdict.Refusals {
		res.Verdict.Refusals = append(res.Verdict.Refusals, protocol.RefusalView{Code: r.Code, Sentence: r.Sentence})
	}
	return res, nil
}

// fillPreviewFacts fills the extended half of the preview's facts: the
// latest run at the PR's head (require_local's subject) and the loop's
// DoD ledger. The loop resolves from the run at that head — a run names
// its loop — else from the root's single loop (a root with one loop is
// the common case and the DoD's owner), else from nothing: the DoD stays
// empty and the evaluation says dod-none honestly, the way a merge would
// say it for the same facts.
func (s *MergeRuleService) fillPreviewFacts(facts *mergerule.Facts, root, headSHA string) {
	runs, err := s.st.ListVerifyRuns(store.VerifyRunFilter{ComponentRoot: root, HeadSHA: headSHA, Limit: 1})
	if err == nil && len(runs) > 0 {
		facts.LatestRun = &mergerule.Run{ID: runs[0].ID, HeadSHA: runs[0].HeadSHA, State: runs[0].State, Reason: runs[0].Reason}
		if err := s.fillDoDFacts(facts, runs[0].LoopSlug, root); err == nil {
			return
		}
	}
	if loops, err := s.st.ListLoops(store.ComponentScope{ComponentRoot: root}); err == nil && len(loops) == 1 {
		s.fillDoDFacts(facts, loops[0].Slug, root)
	}
}

func (s *MergeRuleService) fillDoDFacts(facts *mergerule.Facts, loopSlug, root string) error {
	items, err := s.st.ListDoD(store.Scope{LoopSlug: loopSlug, ComponentRoot: root})
	if err != nil {
		return err
	}
	facts.DoD = make([]mergerule.Item, 0, len(items))
	for _, it := range items {
		facts.DoD = append(facts.DoD, mergerule.Item{
			ID: it.ID, State: it.State, EvidenceKind: it.EvidenceKind,
			EvidenceRef: it.EvidenceRef, Text: it.Text,
		})
	}
	return nil
}

// Changes answers mergerule.changes: the rule's audit trail, capped —
// revision, op, who, when, the before/after images as raw JSON.
func (s *MergeRuleService) Changes(ctx context.Context, componentID string, limit int) (protocol.MergeRuleChangesResult, error) {
	if _, err := s.component("merge rule changes", componentID); err != nil {
		return protocol.MergeRuleChangesResult{}, err
	}
	events, err := s.st.ListMergeRuleEvents(store.MergeRuleEventFilter{ComponentID: componentID, Limit: limitOrDefault(limit, 50)})
	if err != nil {
		return protocol.MergeRuleChangesResult{}, err
	}
	out := protocol.MergeRuleChangesResult{Changes: make([]protocol.MergeRuleChangeView, 0, len(events))}
	for _, e := range events {
		out.Changes = append(out.Changes, protocol.MergeRuleChangeView{
			ComponentID: e.ComponentID, Revision: e.Revision, Op: e.Op,
			ActorKind: e.ActorKind, ActorID: e.ActorID, Summary: e.Summary,
			Before: e.BeforeJSON, After: e.AfterJSON, At: e.At,
		})
	}
	return out, nil
}

// Vocabulary answers mergerule.vocabulary: the five closed sets the rule's
// fields draw from, the closed refusal codes, the floor codes, and the
// baseline — the rule every component starts from, in code, never in rows.
func (s *MergeRuleService) Vocabulary(ctx context.Context) (protocol.MergeRuleVocabularyResult, error) {
	b := mergerule.Baseline()
	return protocol.MergeRuleVocabularyResult{
		RequireDoD:   []string{mergerule.RequireDoDNone, mergerule.RequireDoDAllMet},
		ChecksPolicy: []string{mergerule.ChecksReported, mergerule.ChecksRequired},
		NoChecks:     []string{mergerule.NoChecksAllow, mergerule.NoChecksRefuse},
		ReviewMode:   []string{mergerule.ReviewModeForge, mergerule.ReviewModeAeon, mergerule.ReviewModeEither, mergerule.ReviewModeBoth},
		Method:       []string{mergerule.MethodSquash, mergerule.MethodMerge, mergerule.MethodRebase},
		RefusalCodes: mergerule.AllRefusalCodes(),
		FloorCodes:   mergerule.AllFloorCodes(),
		Baseline:     ruleWire(b),
	}, nil
}

// Set answers mergerule.set: the full rule on the component, the peer as
// actor. The store's closed sets are the backstop and name the value they
// refused; a re-set of the standing rule is the store's quiet no-op.
func (s *MergeRuleService) Set(ctx context.Context, actor protocol.Actor, in protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, *protocol.MergeRuleRefusal, error) {
	w, err := s.st.SetMergeRule(gateStoreActor(actor), ruleFromWire(in.ComponentID, in))
	if err != nil {
		return protocol.MergeRuleSetResult{}, nil, err
	}
	if !w.OK() {
		return protocol.MergeRuleSetResult{}, &protocol.MergeRuleRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	v := mergeRuleView(in.ComponentID, w.Rule, "own")
	return protocol.MergeRuleSetResult{Rule: v}, nil, nil
}

// Clear answers mergerule.clear: the row is deleted (the component returns
// to its root's rule, or the baseline); the revisions survive in the audit.
func (s *MergeRuleService) Clear(ctx context.Context, actor protocol.Actor, componentID string) (protocol.MergeRuleSetResult, *protocol.MergeRuleRefusal, error) {
	w, err := s.st.ClearMergeRule(gateStoreActor(actor), componentID)
	if err != nil {
		return protocol.MergeRuleSetResult{}, nil, err
	}
	if !w.OK() {
		return protocol.MergeRuleSetResult{}, &protocol.MergeRuleRefusal{Kind: string(w.Refusal), Detail: gateDetail(w.Reason)}, nil
	}
	// The row is gone: the answer is the rule the component stands on now —
	// the effective resolution, its source saying where it came from.
	rule, source, err := s.st.EffectiveMergeRule(componentID)
	if err != nil {
		return protocol.MergeRuleSetResult{}, nil, err
	}
	v := mergeRuleView(componentID, rule, source)
	return protocol.MergeRuleSetResult{Rule: v}, nil, nil
}

// --- the merge plane ----------------------------------------------------------------

// Events answers merge.events: the merge-event trail, scoped by component
// or by loop (which the service resolves to the loop's own root — a loop
// names both halves of the scope) and capped. Neither scope names every
// event, capped.
func (s *MergeRuleService) Events(ctx context.Context, f protocol.MergeEventsQuery) (protocol.MergeEventsResult, error) {
	filter := store.MergeEventFilter{Limit: limitOrDefault(f.Limit, 50)}
	if f.LoopSlug != "" {
		root, err := s.loopRootFor(f.LoopSlug)
		if err != nil {
			return protocol.MergeEventsResult{}, err
		}
		filter.Scope = store.Scope{LoopSlug: f.LoopSlug, ComponentRoot: root}
	} else if f.ComponentID != "" {
		if _, err := s.component("merge events", f.ComponentID); err != nil {
			return protocol.MergeEventsResult{}, err
		}
		filter.ComponentID = &f.ComponentID
	}
	events, err := s.st.ListMergeEvents(filter)
	if err != nil {
		return protocol.MergeEventsResult{}, err
	}
	out := protocol.MergeEventsResult{Events: make([]protocol.MergeEventView, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, mergeEventView(e))
	}
	return out, nil
}

// Watches answers merge.watches: the standing merge watches (the merges
// the gate is still checking), optionally by state (the closed five).
func (s *MergeRuleService) Watches(ctx context.Context, f protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error) {
	rows, err := s.st.ListMergeWatches(store.MergeWatchFilter{State: f.State, Limit: 50})
	if err != nil {
		return protocol.MergeWatchesResult{}, err
	}
	out := protocol.MergeWatchesResult{Watches: make([]protocol.MergeWatchView, 0, len(rows))}
	for _, r := range rows {
		out.Watches = append(out.Watches, mergeWatchView(r))
	}
	return out, nil
}

// --- the small pieces ---------------------------------------------------------------

// component reads the node, answering the typed refusal for a name that is
// not one — the merge plane's read doors have no refusal slot in the seam,
// so the service builds the same *protocol.Error the write path's refusal
// carries (same family, same kind, the store's own sentence). verb is the
// door asking, so the sentence names the door it was refused on.
func (s *MergeRuleService) component(verb, id string) (store.Component, error) {
	c, err := s.st.ReadComponent(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			e := protocol.ErrMergeRuleRefused.WithWhat("%s refused: %s is not a component — a rule needs a node it can sit on", verb, id)
			e.Data.Kind = "component-unknown"
			return store.Component{}, e
		}
		return store.Component{}, err
	}
	return c, nil
}

// loopRootFor resolves a loop slug to its root: exactly one loop with the
// slug ⇒ its root; none ⇒ loop-unknown; more than one ⇒ loop-slug-ambiguous
// (the slug is unique per root, never global). Both refusals are typed —
// an empty answer would read as "no merges", which is a lie.
func (s *MergeRuleService) loopRootFor(slug string) (string, error) {
	loops, err := s.st.LoopsBySlug(slug)
	if err != nil {
		return "", err
	}
	switch len(loops) {
	case 0:
		e := protocol.ErrMergeRuleRefused.WithWhat("merge events refused: %s is not a loop", slug)
		e.Data.Kind = "loop-unknown"
		return "", e
	case 1:
		return loops[0].ComponentRoot, nil
	default:
		e := protocol.ErrMergeRuleRefused.WithWhat("merge events refused: %s is a loop slug in %d roots — a slug is unique per root, not global; name the component", slug, len(loops))
		e.Data.Kind = "loop-slug-ambiguous"
		return "", e
	}
}

// floor is the autonomy floor over the effective rule and gates: ok, and
// each missing ingredient with its code and its sentence (printed
// verbatim — the checklists render them). A component whose gates cannot
// be read answers no floor (the honest absent, not a computed lie).
func (s *MergeRuleService) floor(componentID string, rule mergerule.Rule) *protocol.FloorView {
	eg, err := s.st.EffectiveGates(componentID)
	if err != nil {
		return nil
	}
	ok, missing := mergerule.AutonomyFloor(rule, mergeruleGates(eg))
	v := &protocol.FloorView{OK: ok}
	for _, m := range missing {
		v.Missing = append(v.Missing, protocol.FloorMissing{Code: m.Code, Sentence: m.Sentence})
	}
	return v
}

// lastChange is the newest audit row for the component (nil when the rule
// has never moved — a row with no history).
func (s *MergeRuleService) lastChange(componentID string) (*protocol.MergeRuleChangeView, error) {
	events, err := s.st.ListMergeRuleEvents(store.MergeRuleEventFilter{ComponentID: componentID, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	v := mergeChangeView(events[0])
	return &v, nil
}

// mergeruleGates is the store's effective set in the evaluator's shape —
// the floor's "at least one required gate" ingredient (VG12).
func mergeruleGates(eg store.EffectiveGates) mergerule.EffectiveGates {
	out := mergerule.EffectiveGates{ComponentID: eg.ComponentID, Source: eg.Source, SourceID: eg.SourceID}
	out.Gates = make([]mergerule.EffectiveGate, 0, len(eg.Gates))
	for _, g := range eg.Gates {
		out.Gates = append(out.Gates, mergerule.EffectiveGate{
			ID: g.Command.ID, Name: g.Command.Name,
			Requirement: g.Command.Requirement, Enabled: g.Command.Enabled,
		})
	}
	return out
}

func ruleFromWire(componentID string, in protocol.MergeRuleSetInput) mergerule.Rule {
	r := mergerule.Rule{ComponentID: componentID}
	wireToRule(&r, in)
	return r
}

func wireToRule(r *mergerule.Rule, in protocol.MergeRuleSetInput) {
	r.Enabled = in.Enabled
	r.RequireLocal = in.RequireLocal
	r.RequireDoD = in.RequireDoD
	r.RequiredChecks = in.RequiredChecks
	r.ChecksPolicy = in.ChecksPolicy
	r.NoChecks = in.NoChecks
	r.ReviewMode = in.ReviewMode
	r.AeonReviewAgents = in.AeonReviewAgents
	r.RequireThreadsResolved = in.RequireThreadsResolved
	r.Method = in.Method
}

func ruleWire(r mergerule.Rule) protocol.MergeRuleWire {
	return protocol.MergeRuleWire{
		Enabled: r.Enabled, RequireLocal: r.RequireLocal, RequireDoD: r.RequireDoD,
		RequiredChecks: r.RequiredChecks, ChecksPolicy: r.ChecksPolicy, NoChecks: r.NoChecks,
		ReviewMode: r.ReviewMode, AeonReviewAgents: r.AeonReviewAgents,
		RequireThreadsResolved: r.RequireThreadsResolved, Method: r.Method,
	}
}

func mergeRuleView(componentID string, r mergerule.Rule, source string) protocol.MergeRuleView {
	return protocol.MergeRuleView{
		ComponentID:   componentID,
		MergeRuleWire: ruleWire(r),
		Revision:      r.Revision,
		Source:        source,
		UpdatedAt:     r.UpdatedAt,
	}
}

func mergeChangeView(e store.MergeRuleEvent) protocol.MergeRuleChangeView {
	return protocol.MergeRuleChangeView{
		ComponentID: e.ComponentID, Revision: e.Revision, Op: e.Op,
		ActorKind: e.ActorKind, ActorID: e.ActorID, Summary: e.Summary,
		Before: e.BeforeJSON, After: e.AfterJSON, At: e.At,
	}
}

func mergeEventView(e store.MergeEvent) protocol.MergeEventView {
	return protocol.MergeEventView{
		ID: e.ID, LoopSlug: e.LoopSlug, ComponentRoot: e.ComponentRoot, ComponentID: e.ComponentID,
		PR: e.PR, MergeSHA: e.MergeSHA, Method: e.Method, BaseRef: e.BaseRef, HeadSHA: e.HeadSHA,
		RuleRevision: e.RuleRevision, Evidence: e.Evidence, VerifyRunID: e.VerifyRunID,
		MergedAt: e.MergedAt, ActorKind: e.ActorKind, ActorID: e.ActorID,
	}
}

func mergeWatchView(r store.DueMergeWatchRow) protocol.MergeWatchView {
	return protocol.MergeWatchView{
		MergeEventID: r.MergeEventID, ComponentID: r.ComponentID, BaseRef: r.BaseRef, SHA: r.SHA,
		State: r.State, Attempts: r.Attempts, NextCheckAt: r.NextCheckAt, LastCheckedAt: r.LastCheckedAt,
		Detail: r.Detail, CreatedAt: r.CreatedAt,
		ComponentRoot: r.ComponentRoot, LoopSlug: r.LoopSlug, PR: r.PR,
		RuleRevision: r.RuleRevision, VerifyRunID: r.VerifyRunID,
	}
}
