package learnings

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/store"
)

// Learnings is the one write path and the read door every surface calls.
// A surface (CLI, socket, the bench, the board) never touches the handle's
// rows directly — it calls here, and the scope validation (§3) and the
// relayed write-protection (M1) live in exactly one place. The handle is
// internal/store's; this is the meaning.
type Learnings struct {
	s        *store.Store
	route    *model.Route
	embedder Embedder
}

// New makes a learnings door over a store. route and embedder are nil
// until SetRoute wires an embedding route (T3), so a store with no
// embedding route is the normal path, not a fallback (D5).
func New(s *store.Store) *Learnings {
	return &Learnings{s: s}
}

// SetRoute configures the embedding route (T3): a model route and the
// seam that embeds through it. With a route, Query ranks the tag-filtered
// set by embedding similarity within it (D6); without one, the set is
// returned unranked, in the store's order — the normal, working state
// (D5), not a degraded one.
func (l *Learnings) SetRoute(route *model.Route, embedder Embedder) {
	l.route = route
	l.embedder = embedder
}

// validateAppend is the shape half of the one write path — every check
// that reads the INPUT, not the actor. AppendAs and the two legacy wrappers
// run it verbatim, so the validation S28 shipped rides every path exactly
// once, from one place. allowRelayed is true only on the board hook's
// path: the relayed write-protection (M1) is a sentence here, not a
// convention scattered across callers.
func (l *Learnings) validateAppend(in AppendInput, allowRelayed bool) *Refusal {
	if !ValidScope(in.Scope) {
		return &Refusal{Reason: fmt.Sprintf("scope %q is not one of the three — person, project or agent (D1)", in.Scope)}
	}
	if in.ScopeKey == "" {
		return &Refusal{Reason: "a learning's scope names its scope_key — the user, the root or the agent; an empty key is a learning with no owner"}
	}
	if in.Body == "" {
		return &Refusal{Reason: "a learning with no body has nothing to remember"}
	}
	if !ValidProvenance(in.Provenance) {
		return &Refusal{Reason: fmt.Sprintf("provenance %q is not one of the four — told, observed, assumed or relayed (M1)", in.Provenance)}
	}
	// M1: relayed is written only by the board-answer hook (T7). The public
	// path refuses it — a hand-authored relayed would make the label
	// meaningless, the label that says "this came by the board, not by a
	// person's word or a loop's seeing."
	if in.Provenance == ProvenanceRelayed && !allowRelayed {
		return &Refusal{Reason: "relayed is written only by the board-answer hook (T7); the CLI and socket Append surfaces refuse it — a hand-authored relayed is a label that lies about its own source (M1)"}
	}
	// M3: the strength is the observed count, set once at write time. An
	// observed learning may carry it; a non-observed one may not (it would
	// be a strength about a claim that was not seen).
	if in.Provenance != ProvenanceObserved && in.ObservedCount != nil {
		return &Refusal{Reason: fmt.Sprintf("observed_count is meaningful only for an observed learning (M3) — this one is %s, not observed", in.Provenance)}
	}
	if in.ObservedCount != nil && *in.ObservedCount < 0 {
		return &Refusal{Reason: fmt.Sprintf("observed_count %d is negative — a count of how many times a thing was seen cannot be below zero", *in.ObservedCount)}
	}
	return l.validateScope(in.Scope, in.ScopeKey)
}

// actorRule is S52 LM4 — the actor-kind provenance rule, checked on the
// one path, before SQL. The actor must be one of the four kinds and name
// itself; and the provenance must be a sentence that actor can honestly
// say: `told` says "a person said it", and only a person-kind actor — the
// user at the endpoint, the person at a direct door (cli) — has a person
// behind the words. An agent or a system hook filing `told` is the label
// lying about its source, and it is refused here, not by a CHECK three
// calls away.
func actorRule(a Actor, p Provenance) *Refusal {
	if !ValidActorKind(a.Kind) {
		return &Refusal{Reason: fmt.Sprintf("actor kind %q is not one of the four — user, cli, agent, system (S52 LM4); nobody unnamed and nothing uncategorised writes a learning", a.Kind)}
	}
	if a.ID == "" {
		return &Refusal{Reason: "an actor names itself — an empty actor id is the anonymous write the audit exists to refuse (S52 LM3)"}
	}
	if p == ProvenanceTold && !canSayTold(a.Kind) {
		return &Refusal{Reason: fmt.Sprintf("actor %s/%q may not add a told learning — told says \"a person said it\", and %s is not a person; file it observed or assumed, or say the words through the human door (S52 LM4)", a.Kind, a.ID, a.Kind)}
	}
	return nil
}

// AppendAs is THE write (S52): the shape half of S28's Append, the LM4
// actor rule, and the one `add` audit line, through the store's one
// audited writer. This is what the socket handler, the triage feed and —
// from T2 on — every managed path calls. The audit summary is a fixed
// template: the audit line says what happened, in the daemon's words, not
// the actor's — free text from the writer is a summary that can lie about
// the act it files.
func (l *Learnings) AppendAs(in AppendInput, actor Actor) (Learning, *Refusal) {
	if r := actorRule(actor, in.Provenance); r != nil {
		return Learning{}, r
	}
	if r := l.validateAppend(in, false); r != nil {
		return Learning{}, r
	}
	row, err := l.s.CreateLearningAudited(in.toRow(), &store.NewLearningChange{
		Op:        "add",
		Summary:   fmt.Sprintf("added %s learning scoped to %s/%s", in.Provenance, in.Scope, in.ScopeKey),
		ActorKind: actor.Kind,
		ActorID:   actor.ID,
	})
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the write: %v", err)}
	}
	got, err := FromRow(row)
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	return got, nil
}

// AppendRelayedAs is the board-answer hook's write (M1's one permitted
// author of `relayed`) carrying its actor (S52 LM3/LM4): only a SYSTEM-kind
// actor — the board hook stamps {system, "learnings-board"} — files a
// relayed learning, because relayed is a fact about a channel a daemon
// hook watched, and no hand at a door may type it.
func (l *Learnings) AppendRelayedAs(in AppendInput, actor Actor) (Learning, *Refusal) {
	if in.Provenance != ProvenanceRelayed {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("AppendRelayedAs writes only a relayed learning; given %s", in.Provenance)}
	}
	if r := actorRule(actor, in.Provenance); r != nil {
		return Learning{}, r
	}
	if actor.Kind != "system" {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("relayed is filed by a daemon hook only — actor %s/%q is not one (S52 LM4): the board's channel is watched by the daemon, and by nobody else", actor.Kind, actor.ID)}
	}
	if r := l.validateAppend(in, true); r != nil {
		return Learning{}, r
	}
	row, err := l.s.CreateLearningAudited(in.toRow(), &store.NewLearningChange{
		Op:        "add",
		Summary:   fmt.Sprintf("added %s learning scoped to %s/%s", in.Provenance, in.Scope, in.ScopeKey),
		ActorKind: actor.Kind,
		ActorID:   actor.ID,
	})
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the write: %v", err)}
	}
	got, err := FromRow(row)
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	return got, nil
}

// Append is the LEGACY shape (pre-S52): the shape checks without an actor
// and without an audit line. Production paths call AppendAs; this wrapper
// survives for the fixture seeds and the pinned S28/S29 tests whose bodies
// predate the actor rule and are not edited (the deviation is numbered in
// the T1 Notes — a learning written here has no audit line, and
// `learnings.history` will honestly say the add was never audited).
func (l *Learnings) Append(in AppendInput) (Learning, *Refusal) {
	if r := l.validateAppend(in, false); r != nil {
		return Learning{}, r
	}
	row, err := l.s.CreateLearning(in.toRow())
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the write: %v", err)}
	}
	got, err := FromRow(row)
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	return got, nil
}

// AppendRelayed is the LEGACY board-hook shape (pre-S52), no audit line —
// kept only so the pinned S28 board tests keep compiling and running
// unedited. The wiring contract from S52 is AppendRelayedAs with the
// {system, "learnings-board"} actor; the daemon's board seam names it.
func (l *Learnings) AppendRelayed(in AppendInput) (Learning, *Refusal) {
	if in.Provenance != ProvenanceRelayed {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("AppendRelayed writes only a relayed learning; given %s", in.Provenance)}
	}
	if r := l.validateAppend(in, true); r != nil {
		return Learning{}, r
	}
	row, err := l.s.CreateLearning(in.toRow())
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the store refused the write: %v", err)}
	}
	got, err := FromRow(row)
	if err != nil {
		return Learning{}, &Refusal{Reason: fmt.Sprintf("learnings: the row that came back is not a legal learning: %v", err)}
	}
	return got, nil
}

// Query reads the tag-filtered set (D5) and, where an embedding route is
// configured (T3), ranks it within that set only (D6). Every result carries
// its provenance and, for an observed learning, its count — M3's honesty,
// never collapsed into a single score.
func (l *Learnings) Query(ctx context.Context, f QueryFilter) ([]Learning, error) {
	rows, err := l.s.QueryLearnings(store.LearningQuery{
		Scope:    string(f.Scope),
		ScopeKey: f.ScopeKey,
		Tags:     storeTagPatterns(f.Tags),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Learning, 0, len(rows))
	for _, r := range rows {
		lr, err := FromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	// T3's ranker reorders within the tag-filtered set only; it can never
	// pull in a learning the tag filter excluded (D6). No route = no
	// ranking, the set stays in its store order (the normal state, D5).
	return l.rankWithinSet(ctx, f.QueryText, out), nil
}

// storeTagPatterns converts the learnings package's tag patterns to the
// store's, at the one boundary where the SQL reads them. A caller outside
// the learnings package (the protocol layer, which must not import store)
// builds learnings.TagPattern; this is the single place the store's shape
// is read.
func storeTagPatterns(in []TagPattern) []store.TagPattern {
	out := make([]store.TagPattern, 0, len(in))
	for _, tg := range in {
		out = append(out, store.TagPattern{Pattern: tg.Pattern, Prefix: tg.Prefix})
	}
	return out
}

// List reads by scope and scope_key, each with its tags. The plain read
// (no tag filter, no ranking) — the "what has this agent learned" listing.
func (l *Learnings) List(scope Scope, scopeKey string) ([]Learning, error) {
	rows, err := l.s.ListLearnings(store.LearningFilters{Scope: string(scope), ScopeKey: scopeKey})
	if err != nil {
		return nil, err
	}
	out := make([]Learning, 0, len(rows))
	for _, r := range rows {
		lr, err := FromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, nil
}

// ListOpts is List with LM2's explicit exception: the same plain read, and
// when includeRetracted is set, the retracted rows read too. List keeps its
// two-argument default-live shape because the embedding pass and every
// pinned caller mean "what is memory now" — the exception is spelled at
// the call site that wants history, never defaulted in.
func (l *Learnings) ListOpts(scope Scope, scopeKey string, includeRetracted bool) ([]Learning, error) {
	rows, err := l.s.ListLearnings(store.LearningFilters{Scope: string(scope), ScopeKey: scopeKey, IncludeRetracted: includeRetracted})
	if err != nil {
		return nil, err
	}
	out := make([]Learning, 0, len(rows))
	for _, r := range rows {
		lr, err := FromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, nil
}

// Show reads one learning by id; false is honest absence, not a refusal.
func (l *Learnings) Show(id int64) (Learning, bool, error) {
	row, ok, err := l.s.GetLearning(id)
	if err != nil || !ok {
		return Learning{}, false, err
	}
	lr, err := FromRow(row)
	return lr, err == nil, err
}

// Exists answers the one question the management doors must ask before
// they can name an absence: is there a row with this id at all (retracted
// included — a retracted row IS there, flagged). It is Show minus the body:
// no tags are read, nothing is rendered, one EXISTS-shaped read.
func (l *Learnings) Exists(id int64) bool {
	_, ok, _ := l.s.GetLearning(id)
	return ok
}
