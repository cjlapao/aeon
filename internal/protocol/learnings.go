package protocol

// S28 T4 — the `learnings.*` socket methods. The daemon-first rule (P34)
// is what this file IS: the capability reaches the user first as a method
// on the daemon's own surface, and the CLI is a secondary client of it,
// not a second door. A learning's write reaches the store through
// internal/learnings' one Append, exactly as the CLI and the app's
// bench do — the scope validation (§3) and the relayed write-protection
// (M1) live in that one place, so a second authoring path cannot appear.
//
// `relayed` is refused HERE by the seam it rides: the handler calls
// internal/learnings' Append, which refuses relayed (M1's write-
// protection), so the CLI and the socket cannot hand-author a board
// answer. The board hook (T7) is the one caller that may, through its
// own path. This file carries no validation beyond parameter shape — the
// door and the database CHECK are the two truths, neither rides twice.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/learnings"
)

// --- the names ---------------------------------------------------------------

const (
	MethodLearningsAdd   MethodName = "learnings.add"
	MethodLearningsList  MethodName = "learnings.list"
	MethodLearningsShow  MethodName = "learnings.show"
	MethodLearningsQuery MethodName = "learnings.query"
	MethodLearningsEmbed MethodName = "learnings.embed"
	// S52 T2 — the management doors. retract/restore are the human door
	// (LM8): the actor comes from the connection, a person or nothing
	// (agent and system are refused by name; agents propose, T4). The four
	// reads are open reads like list/query (M3's openness): board, history,
	// vocabulary and forAgent write nothing.
	MethodLearningsRetract    MethodName = "learnings.retract"
	MethodLearningsRestore    MethodName = "learnings.restore"
	MethodLearningsBoard      MethodName = "learnings.board"
	MethodLearningsHistory    MethodName = "learnings.history"
	MethodLearningsVocabulary MethodName = "learnings.vocabulary"
	MethodLearningsForAgent   MethodName = "learnings.forAgent"
	// The knowledge map: where each learning sits when laid out by what it means, and which are nearest.
	// An open read like board: it writes nothing and embeds nothing (a learning with no vector is
	// counted, never positioned by a guess; learnings.embed is the door that fills vectors).
	MethodLearningsMap MethodName = "learnings.map"
)

// ErrLearningsRefused is the one learnings-flavoured error: a scope or
// provenance refusal with its reason carried in `what` (the discipline
// every refusal here keeps — a refusal names the reason AND the thing
// refused). Not-found rides it too: the socket does not answer a missing
// row with a silence.
var ErrLearningsRefused = &Error{
	Code:    -31950,
	Message: "the learnings refused the act",
	Data: Data{
		What:  "the scope, the scope_key or the provenance broke a learnings law (D1's three scopes, §3's agent check, M1's relayed write-protection)",
		Means: "nothing was written — the learning is not in the store",
		Todo:  "read the reason in `what`, then learnings.list to see the record as the daemon sees it",
		Fatal: false,
	},
}

func learningsRefusal(reason string) *Error {
	return ErrLearningsRefused.WithWhat("%s", reason)
}

// ErrLearningUnknown is S52 T2's named absence (the name the plan.md task
// text asks for): a retract, a restore or a history asked about an id with
// no row. T0's record noted this error did not exist when the slice was
// drafted — it does now, beside the refusal, with its own code, because
// "you refused my act" and "the row you named is not there" are different
// answers and a client branches on the code, not the sentence. The older
// doors keep their existing refusal text unchanged (goldens and fixtures
// ride it; the change would buy nothing and break pins).
var ErrLearningUnknown = &Error{
	Code:    -31951,
	Message: "no learning answers to that id",
	Data: Data{
		What:  "a retract, restore or history named an id with no row",
		Means: "nothing was written or read — the learning is not in the store",
		Todo:  "learnings.list or learnings.board to see what IS there (retracted rows show on learnings.show and the board with includeRetracted)",
		Fatal: false,
	},
}

// --- params ------------------------------------------------------------------

// LearningsAddParams is the one write. provenance is REQUIRED on the wire
// (the column has no default at the database) and `relaxed` is refused at
// the door (M1 — written only by the board-answer hook, never here).
// observedCount is meaningful only for an observed learning (M3).
type LearningsAddParams struct {
	Scope          string   `json:"scope"`
	ScopeKey       string   `json:"scopeKey"`
	Body           string   `json:"body"`
	Provenance     string   `json:"provenance"`
	ObservedCount  *int     `json:"observedCount,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	SourceLoopSlug string   `json:"sourceLoopSlug,omitempty"`
	SourceStepID   *int     `json:"sourceStepId,omitempty"`
}

type LearningsListParams struct {
	Scope    string `json:"scope"`
	ScopeKey string `json:"scopeKey"`
	// IncludeRetracted (S52 LM2): default false — the listing answers what
	// the daemon would act on. Set it deliberately to see the history too.
	IncludeRetracted bool `json:"includeRetracted,omitempty"`
}

type LearningsShowParams struct {
	ID int64 `json:"id"`
}

// LearningsQueryParams is the tag-filtered read (D5) with an optional
// embedding rank (T3). Tags are ANDed, each by prefix or exact; QueryText,
// when an embedding route is configured, ranks the tag-filtered set
// within it only (D6 — never a widening).
type LearningsQueryParams struct {
	Scope            string         `json:"scope,omitempty"`
	ScopeKey         string         `json:"scopeKey,omitempty"`
	Tags             []LearningsTag `json:"tags,omitempty"`
	QueryText        string         `json:"queryText,omitempty"`
	IncludeRetracted bool           `json:"includeRetracted,omitempty"`
}

// LearningsTag is one tag the query asks for: a Pattern matched by Prefix
// (LIKE 'Pattern%') or by exact equality (M5's two promises).
type LearningsTag struct {
	Pattern string `json:"pattern"`
	Prefix  bool   `json:"prefix,omitempty"`
}

// LearningsEmbedParams is the trigger (T3's embedding pass, surfaced as a
// door the app and the CLI can call — the analogue of retrieval.reindex).
// It runs one scope's learnings through EmbedPending: every learning with no
// vector yet gets one, idempotently. Scope and scopeKey name the set, as
// every other learnings method does.
type LearningsEmbedParams struct {
	Scope    string `json:"scope"`
	ScopeKey string `json:"scopeKey"`
}

// LearningsMapParams is the board's filter set plus how many nearest neighbours each learning links to
// (0 = the daemon's default). The map and the board read one set with one filter, so they cannot disagree
// about what is on screen.
type LearningsMapParams struct {
	LearningsBoardParams
	Neighbours int `json:"neighbours,omitempty"`
}

// --- views -------------------------------------------------------------------

// LearningsView renders a learning the honest way: provenance is ALWAYS
// shown (M3 — the label is never collapsed), and the observed count rides
// only when the provenance is observed (M3's strength, set once at write
// time); a non-observed learning carries no count, never a zero.
type LearningsView struct {
	ID             int64    `json:"id"`
	Scope          string   `json:"scope"`
	ScopeKey       string   `json:"scopeKey"`
	Body           string   `json:"body"`
	Provenance     string   `json:"provenance"`
	ObservedCount  *int     `json:"observedCount,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	SourceLoopSlug string   `json:"sourceLoopSlug,omitempty"`
	SourceStepID   *int     `json:"sourceStepId,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	// S52 LM2, additive: a retracted learning shows FLAGGED (Show by id,
	// the board with includeRetracted) and never silently: the state, the
	// instant, and what the last retraction said. A live learning carries
	// none of these keys at all — omitempty keeps the pre-S52 bytes of
	// every live row byte-identical on the wire.
	Retracted       bool   `json:"retracted,omitempty"`
	RetractedAt     string `json:"retractedAt,omitempty"`
	RetractedReason string `json:"retractedReason,omitempty"`
	RetractedBy     string `json:"retractedBy,omitempty"`
}

func learningsView(lr learnings.Learning) LearningsView {
	v := LearningsView{
		ID:           lr.ID,
		Scope:        string(lr.Scope),
		ScopeKey:     lr.ScopeKey,
		Body:         lr.Body,
		Provenance:   string(lr.Provenance),
		Tags:         lr.Tags,
		SourceStepID: lr.SourceStepID,
		CreatedAt:    lr.CreatedAt,
	}
	// A nil source pointer is an absent pointer, not an empty-string one;
	// the view's sourceLoopSlug is a plain string, so nil renders absent.
	if lr.SourceLoopSlug != nil {
		v.SourceLoopSlug = *lr.SourceLoopSlug
	}
	// The count rides only with its provenance — M3's strength is the
	// observed count, and a non-observed learning carrying a count is a
	// label that lies about what it measured.
	if lr.Provenance == learnings.ProvenanceObserved && lr.ObservedCount != nil {
		v.ObservedCount = lr.ObservedCount
	}
	v.Retracted = lr.Retracted
	v.RetractedAt = lr.RetractedAt
	v.RetractedReason = lr.RetractedReason
	v.RetractedBy = lr.RetractedBy
	return v
}

type LearningsItemResult struct {
	Learning LearningsView `json:"learning"`
}

type LearningsListResult struct {
	Learnings []LearningsView `json:"learnings"`
}

// LearningsQueryResult is the tag-filtered (and, where a route is
// configured, ranked) set — every view carries its provenance and, for an
// observed learning, its count (M3).
type LearningsQueryResult struct {
	Learnings []LearningsView `json:"learnings"`
}

// LearningsEmbedResult is the embedding pass's honest count: how many
// learnings got a vector this run. 0 is a real answer (a second run with
// nothing new embeds nothing — the pass is idempotent), not a silence; the
// no-route case is a refusal, not a zero (the door names it, so the caller
// never reads "nothing to embed" as "embeddings failed").
type LearningsEmbedResult struct {
	Embedded int `json:"embedded"`
}

// LearningsMapPoint is one positioned learning. The coordinates are a deterministic 2-D projection scaled
// into ±300; they are a hint about distance, and the links are the evidence.
type LearningsMapPoint struct {
	ID int64   `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

// LearningsMapLink joins two learnings that are nearest neighbours by cosine similarity in the FULL
// vector space (not in the 2-D shadow).
type LearningsMapLink struct {
	A          int64   `json:"a"`
	B          int64   `json:"b"`
	Similarity float64 `json:"similarity"`
}

// LearningsMapResult is the map and its honest accounting. `projected` is false when there is nothing to
// place (no vectors, or fewer than three); `embedded` of `total` is how many learnings have a vector;
// `canEmbed` says whether a route exists to give the rest one; `omitted` counts embedded learnings left off
// to keep the call bounded; `basis` is the daemon's own sentence for what the positions are.
type LearningsMapResult struct {
	Projected bool                `json:"projected"`
	Model     string              `json:"model,omitempty"`
	Dim       int                 `json:"dim"`
	Total     int                 `json:"total"`
	Embedded  int                 `json:"embedded"`
	Omitted   int                 `json:"omitted"`
	CanEmbed  bool                `json:"canEmbed"`
	Points    []LearningsMapPoint `json:"points"`
	Links     []LearningsMapLink  `json:"links"`
	Basis     string              `json:"basis"`
}

// LearningsRetractParams is LM2's act: a named row, a required reason
// (1..256 — the reason is the point of the act, not a comment on it).
// There is no force flag and no batch: retract one memory at a time, each
// with its own sentence.
type LearningsRetractParams struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

type LearningsRestoreParams struct {
	ID int64 `json:"id"`
}

// LearningsActResult answers retract and restore: the row as it now stands
// and whether this call CHANGED it (LM2's idempotence — a second retract
// answers the row with changed:false, and wrote nothing, not even an audit
// line).
type LearningsActResult struct {
	Learning LearningsView `json:"learning"`
	Changed  bool          `json:"changed"`
}

// LearningsBoardParams is LM6's filter set — every field optional, every
// set field ANDed, every filter run IN THE DAEMON.
type LearningsBoardParams struct {
	Scope            string `json:"scope,omitempty"`
	Provenance       string `json:"provenance,omitempty"`
	Tag              string `json:"tag,omitempty"`
	Agent            string `json:"agent,omitempty"`
	Project          string `json:"project,omitempty"`
	Loop             string `json:"loop,omitempty"`
	IncludeRetracted bool   `json:"includeRetracted,omitempty"`
}

// LearningsBoardRow is one board row: the full view, plus the daemon's own
// Describe sentence for the provenance (LM6 — every surface quotes the one
// formatter; the app renders a pill from it, the CLI prints it, neither
// re-composes the claim in its own words).
type LearningsBoardRow struct {
	LearningsView
	Describe string `json:"describe"`
}

type LearningsBoardResult struct {
	Learnings []LearningsBoardRow `json:"learnings"`
}

// LearningsHistoryParams names the learning whose audit career is asked
// for. History is per-row by design: "what happened to THIS memory".
type LearningsHistoryParams struct {
	ID int64 `json:"id"`
}

type LearningsChangeView struct {
	ID         int64  `json:"id"`
	LearningID int64  `json:"learningId"`
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

type LearningsHistoryResult struct {
	Changes []LearningsChangeView `json:"changes"`
}

// LearningsVocabularyResult is the door that makes the vocabulary readable
// instead of remembered (LM6): the three scopes, the FOUR provenances in
// honesty order from Provenances(), the four actor kinds, the M1 ordering
// law quoted from its one constant, the limits named as Go constants
// (reason 1..256, audit summary 1..1024, forAgent 20/100), and the retract
// law in words. No params.
type LearningsVocabularyResult struct {
	Scopes      []string `json:"scopes"`
	Provenances []string `json:"provenances"`
	ActorKinds  []string `json:"actorKinds"`
	M1Ordering  string   `json:"m1Ordering"`
	RetractLaw  string   `json:"retractLaw"`
	Limits      struct {
		ReasonMin        int `json:"reasonMin"`
		ReasonMax        int `json:"reasonMax"`
		SummaryMax       int `json:"summaryMax"`
		ForAgentLimitDef int `json:"forAgentLimitDefault"`
		ForAgentLimitMax int `json:"forAgentLimitMax"`
	} `json:"limits"`
	TagGrammar struct {
		Exact  bool `json:"exact"`
		Prefix bool `json:"prefix"`
	} `json:"tagGrammar"`
}

// LearningsForAgentParams is LM5's question: which agent (by SLUG — the
// identity since S43/LM1), optionally with a query, optionally capped.
type LearningsForAgentParams struct {
	Agent string `json:"agent"`
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

// LearningsForAgentWhy is the LM5 `why` on the wire: rank (1-based, over
// the merged set BEFORE the limit was applied), the M1 ordering note
// quoted from its constant, and the match basis stated honestly —
// including the no-route and no-query truths (D5).
type LearningsForAgentWhy struct {
	Rank   int    `json:"rank"`
	M1Note string `json:"m1Note"`
	Basis  string `json:"basis"`
}

type LearningsForAgentHit struct {
	Learning LearningsView        `json:"learning"`
	Why      LearningsForAgentWhy `json:"why"`
}

type LearningsForAgentResult struct {
	Hits []LearningsForAgentHit `json:"hits"`
}

// --- registration (called from method.go's table assembly) -------------------

func (t *Table) registerLearnings() {
	t.register(&Method{Name: MethodLearningsAdd, RequiresInitialize: true, Handle: t.handleLearningsAdd})
	t.register(&Method{Name: MethodLearningsList, RequiresInitialize: true, Handle: t.handleLearningsList})
	t.register(&Method{Name: MethodLearningsShow, RequiresInitialize: true, Handle: t.handleLearningsShow})
	t.register(&Method{Name: MethodLearningsQuery, RequiresInitialize: true, Handle: t.handleLearningsQuery})
	t.register(&Method{Name: MethodLearningsEmbed, RequiresInitialize: true, Handle: t.handleLearningsEmbed})
	t.register(&Method{Name: MethodLearningsRetract, RequiresInitialize: true, Handle: t.handleLearningsRetract})
	t.register(&Method{Name: MethodLearningsRestore, RequiresInitialize: true, Handle: t.handleLearningsRestore})
	t.register(&Method{Name: MethodLearningsBoard, RequiresInitialize: true, Handle: t.handleLearningsBoard})
	t.register(&Method{Name: MethodLearningsHistory, RequiresInitialize: true, Handle: t.handleLearningsHistory})
	t.register(&Method{Name: MethodLearningsVocabulary, RequiresInitialize: true, Handle: t.handleLearningsVocabulary})
	t.register(&Method{Name: MethodLearningsForAgent, RequiresInitialize: true, Handle: t.handleLearningsForAgent})
	t.register(&Method{Name: MethodLearningsMap, RequiresInitialize: true, Handle: t.handleLearningsMap})
}

// --- handlers ----------------------------------------------------------------

func (t *Table) requireLearnings() (*learnings.Learnings, error) {
	if t.Learnings == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Learnings, nil
}

func (t *Table) handleLearningsAdd(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	// S52 LM3/LM4: WHO writes is answered before WHAT is written is even
	// decoded, and the answer comes from the connection, never the params
	// (P26 — the peer credential is the kernel's, not a claim). A daemon
	// with no ActorFor seam cannot answer who, and an anonymous memory is
	// exactly what the audit exists to refuse — so this door is closed
	// until the seam is wired, refusing BY NAME rather than writing rows
	// nobody signed.
	if t.ActorFor == nil {
		return nil, learningsRefusal("the daemon cannot resolve who is writing — the ActorFor seam is unwired, and an anonymous learning is the thing the audit (S52 LM3) refuses; nothing was written")
	}
	a := t.ActorFor(ctx, sess)
	if a.Kind == "" || a.ID == "" {
		return nil, learningsRefusal("the daemon cannot resolve who is writing — the connection named no actor (S52 LM4); an empty actor is the anonymous write the audit refuses; nothing was written")
	}
	var p LearningsAddParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Scope == "" || p.ScopeKey == "" || p.Body == "" || p.Provenance == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scope, scopeKey, body and provenance are required — a learning names its owner, its body and how it came to be")}
	}
	// M1's write-protection, stated at the seam the verb rides: relayed is
	// the board's label, written only by its hook (T7). A hand-authored
	// relayed here would make the label lie about its own source.
	if p.Provenance == string(learnings.ProvenanceRelayed) {
		return nil, learningsRefusal("provenance `relaxed` is written only by the board-answer hook (T7); the CLI and socket Append surfaces refuse it — a hand-authored relayed is a label that lies about its own source (M1)")
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	in := learnings.AppendInput{
		Scope:          learnings.Scope(p.Scope),
		ScopeKey:       p.ScopeKey,
		Body:           p.Body,
		Provenance:     learnings.Provenance(p.Provenance),
		ObservedCount:  p.ObservedCount,
		Tags:           p.Tags,
		SourceLoopSlug: &p.SourceLoopSlug,
		SourceStepID:   p.SourceStepID,
	}
	got, ref := l.AppendAs(in, learnings.Actor{Kind: a.Kind, ID: a.ID})
	if ref != nil {
		return nil, learningsRefusal(ref.Reason)
	}
	return LearningsItemResult{Learning: learningsView(got)}, nil
}

func (t *Table) handleLearningsList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Scope == "" || p.ScopeKey == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scope and scopeKey are required — a learning is scoped to a person, a project or an agent, each named")}
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	out, err := l.ListOpts(learnings.Scope(p.Scope), p.ScopeKey, p.IncludeRetracted)
	if err != nil {
		return nil, err
	}
	views := make([]LearningsView, 0, len(out))
	for _, lr := range out {
		views = append(views, learningsView(lr))
	}
	return LearningsListResult{Learnings: views}, nil
}

func (t *Table) handleLearningsShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	// S52 T5: the answer lives in AnswerLearningsShow — this door and the
	// CLI's offline arm call the one function (P5, no second renderer).
	var p LearningsShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	res, aerr := AnswerLearningsShow(l, p)
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsQuery(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsQueryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	tags := make([]learnings.TagPattern, 0, len(p.Tags))
	for _, tg := range p.Tags {
		if tg.Pattern == "" {
			continue
		}
		tags = append(tags, learnings.TagPattern{Pattern: tg.Pattern, Prefix: tg.Prefix})
	}
	out, err := l.Query(ctx, learnings.QueryFilter{
		IncludeRetracted: p.IncludeRetracted,
		Scope:            learnings.Scope(p.Scope),
		ScopeKey:         p.ScopeKey,
		Tags:             tags,
		QueryText:        p.QueryText,
	})
	if err != nil {
		return nil, err
	}
	views := make([]LearningsView, 0, len(out))
	for _, lr := range out {
		views = append(views, learningsView(lr))
	}
	return LearningsQueryResult{Learnings: views}, nil
}

// handleLearningsEmbed is the embedding-pass door (T3's EmbedPending,
// surfaced): the analogue of retrieval.reindex for learnings — one scope's
// learnings, each without a vector, gets one, idempotently. The refusal
// (no route configured) is a named answer, not a zero: the door carries the
// reason, so a caller never reads "nothing to embed" as "embeddings failed"
// — D5's no-route state is the normal path, spoken, not swallowed.
func (t *Table) handleLearningsEmbed(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsEmbedParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Scope == "" || p.ScopeKey == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("scope and scopeKey are required — an embedding pass runs over one scope's learnings, each named")}
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	n, err := l.EmbedPending(ctx, learnings.Scope(p.Scope), p.ScopeKey)
	if err != nil {
		// A refusal (the no-route state D5 names) is a decided answer with a
		// reason, not an error the caller must guess. Carry it verbatim.
		return nil, learningsRefusal(err.Error())
	}
	return LearningsEmbedResult{Embedded: n}, nil
}

// --- the management doors (S52 T2) --------------------------------------------

// personActorOrRefusal is LM8's gate, shared by retract and restore: the
// ActorFor seam must be wired and the connection must name itself (the
// same two refusals learnings.add learned in T1, word for word where the
// act is an add — here the act is management, and the sentence names the
// acts). Then the KIND gate: user or cli. An agent-kind actor is refused
// BY NAME with its alternative (T4's proposal kind), because LM3's CHECK
// will refuse an agent retract anyway — the door says why, in the caller's
// language, before SQL has to say it in driver's.
func (t *Table) personActorOrRefusal(ctx context.Context, sess *Session, act string) (Actor, *Error) {
	if t.ActorFor == nil {
		return Actor{}, learningsRefusal("the daemon cannot resolve who is acting — the ActorFor seam is unwired, and " + act + " without an author is the anonymous act the audit (S52 LM3) refuses; nothing was written")
	}
	a := t.ActorFor(ctx, sess)
	if a.Kind == "" || a.ID == "" {
		return Actor{}, learningsRefusal("the daemon cannot resolve who is acting — the connection named no actor (S52 LM4); nothing was written")
	}
	return a, nil
}

func (t *Table) handleLearningsRetract(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	a, bad := t.personActorOrRefusal(ctx, sess, "a retraction")
	if bad != nil {
		return nil, bad
	}
	// S52 T5: the answer function holds the id law, the stamping and every
	// refusal sentence — the socket's actor is the PEER's person, the
	// offline arm's is {cli, cli}, and both roads say the same words
	// because they run the same code.
	var p LearningsRetractParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	res, aerr := AnswerLearningsRetract(l, p, learnings.Actor{Kind: a.Kind, ID: a.ID})
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsRestore(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	a, bad := t.personActorOrRefusal(ctx, sess, "a restore")
	if bad != nil {
		return nil, bad
	}
	var p LearningsRestoreParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	res, aerr := AnswerLearningsRestore(l, p, learnings.Actor{Kind: a.Kind, ID: a.ID})
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsBoard(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsBoardParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	res, aerr := AnswerLearningsBoard(l, p)
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsMap(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsMapParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	res, aerr := AnswerLearningsMap(l, p)
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsHistory(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsHistoryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	// An EMPTY career is honest and means one thing only: the row predates
	// the audit (S52's legacy writes leave no add line) — the empty list is
	// the truth, never a silence dressed as one. That law lives in
	// AnswerLearningsHistory, shared with the CLI's offline arm.
	res, aerr := AnswerLearningsHistory(l, p)
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}

func (t *Table) handleLearningsVocabulary(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if len(raw) > 0 {
		var probe map[string]any
		if err := decodeParams(raw, &probe); err != nil {
			return nil, err
		}
	}
	// The seam must be wired for any door of the family to answer (V13);
	// the answer itself is the closed sets as the code ships them —
	// AnswerLearningsVocabulary, shared with the CLI's offline arm (T5).
	if _, err := t.requireLearnings(); err != nil {
		return nil, err
	}
	return AnswerLearningsVocabulary(), nil
}

func (t *Table) handleLearningsForAgent(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LearningsForAgentParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	l, err := t.requireLearnings()
	if err != nil {
		return nil, err
	}
	// S52 T4: the rendering moved to AnswerLearningsForAgent — the ONE
	// renderer this door, the CLI's `for-agent` and the agent's own
	// learning_query twin share (T5 lifted the whole answer — the named-
	// agent law, the ranking, the `why` — into AnswerLearningsForAgentQuery).
	res, aerr := AnswerLearningsForAgentQuery(ctx, l, p)
	if aerr != nil {
		return nil, aerr
	}
	return res, nil
}
