// Package learnings owns the learnings' meaning: the three scopes (design
// D1), the four provenances (M1), the agent-scope validation that SQLite
// cannot express (design §3), the tag filter (M5/D2) and the one write
// path every surface calls. The SQL lives in internal/store (house rule:
// the store owns the handle); this package owns the MEANING. Rows cross
// the boundary as store.LearningRow and are converted once, in FromRow.
//
// The package is named learnings, never memory — internal/store's own
// Memory / AppendMemory / ReadMemory is S20's per-loop notes, a different,
// built mechanism (design §2), and a name one character away would let the
// two blur into one.
package learnings

import (
	"fmt"

	"github.com/cjlapao/aeon/internal/store"
)

// Scope is a learning's owner: who or what the learning is scoped to
// (design D1). Three values, the closed set — the DATABASE's CHECK is the
// backstop, this vocabulary is the door's, so a caller gets a sentence
// instead of a driver error.
type Scope string

const (
	// ScopePerson is cross-project, at the store level — what a person has
	// learned across every project. S4 made it per-user, the store's own
	// decision, so the Go door carries no root check here.
	ScopePerson Scope = "person"
	// ScopeProject is per root — what a project has learned. scope_key is a
	// project_root.
	ScopeProject Scope = "project"
	// ScopeAgent is per agent — what an agent has learned. scope_key is an
	// agent name, and it is the one scope whose scope_key the DATABASE
	// cannot validate (design §3), so the Go check lives here, not in the
	// CHECK.
	ScopeAgent Scope = "agent"
)

// Scopes is the closed three, in the order D1's diagram names them.
func Scopes() []Scope {
	return []Scope{ScopePerson, ScopeProject, ScopeAgent}
}

// ValidScope reports whether s is one of the three.
func ValidScope(s Scope) bool {
	for _, v := range Scopes() {
		if v == s {
			return true
		}
	}
	return false
}

// Provenance is how a learning came to be (M1). Four values, and the fourth
// is this slice's own addition — `relaxed` — the board's answer, a fact
// about the channel and not about the truth of the claim. It is written
// only by T7's board-answer hook; the CLI and socket Append refuse it
// (Append's write-protection, not a default).
type Provenance string

const (
	// ProvenanceTold — a person said it.
	ProvenanceTold Provenance = "told"
	// ProvenanceObserved — the loop saw it (its count, M3, is the strength).
	ProvenanceObserved Provenance = "observed"
	// ProvenanceAssumed — a hypothesis, a guess, a maybe.
	ProvenanceAssumed Provenance = "assumed"
	// ProvenanceRelayed — the board's answer (M1). Written only by T7; the
	// public Append refuses it.
	ProvenanceRelayed Provenance = "relayed"
)

// Provenances is the closed four, in honesty order.
func Provenances() []Provenance {
	return []Provenance{ProvenanceTold, ProvenanceObserved, ProvenanceAssumed, ProvenanceRelayed}
}

// ValidProvenance reports whether p is one of the four.
func ValidProvenance(p Provenance) bool {
	for _, v := range Provenances() {
		if v == p {
			return true
		}
	}
	return false
}

// Actor is who the daemon stamped behind a write (S52 LM4). It is not a
// parameter a caller composes — the socket fills it from the connection's
// peer credential (protocol.ActorFor) and the daemon's own hooks fill it
// with their system name — and the audit line that records an add names it
// verbatim. Kind is the closed four, the same vocabulary the learning_change
// CHECK closes: user (a person at the endpoint), cli (a direct door, the
// store opened without a socket), agent (a first-party loop's tool call),
// system (a daemon hook — the triage feed, the board).
//
// The KIND carries a law, not just a name (LM4): provenance `told` means "a
// person said it", and only a person-kind actor — user or cli — may say it.
// A loop filing an `observed`, a hook filing a `relayed`: the actor-kind and
// the provenance have to agree, and the agreement is checked here, once,
// before any row is written.
type Actor struct {
	Kind string
	ID   string
}

// ActorKinds is the closed four, the CHECK's own set.
func ActorKinds() []string {
	return []string{"user", "cli", "agent", "system"}
}

// ValidActorKind reports whether k is one of the four.
func ValidActorKind(k string) bool {
	for _, v := range ActorKinds() {
		if v == k {
			return true
		}
	}
	return false
}

// canSayTold reports whether an actor of kind k may say "a person said it".
// A person at the endpoint (user) and a person at a terminal (cli) are the
// two kinds where the words have a referent; agent and system do not have
// one (S52 LM4 — the actor-kind provenance rule).
func canSayTold(k string) bool { return k == "user" || k == "cli" }

// Learning is the package's reading of a learning row — the meaning the
// handle's rows cross the boundary as. It carries its tags (M5) and its
// provenance (M3's honest label), and, for an observed learning, its
// count: the strength, set once at write time (M3's one-count-at-write).
type Learning struct {
	ID             int64
	Scope          Scope
	ScopeKey       string
	Body           string
	Provenance     Provenance
	ObservedCount  *int
	Tags           []string
	SourceLoopSlug *string
	SourceStepID   *int
	CreatedAt      string
	// S52 (LM2): the retraction stamp. Retracted is the state — the body is
	// still there (retract is a stamp, never a delete); Reason and By are
	// the person's words and the actor who stamped them, and the audit line
	// is the history. Restore leaves Reason/By standing on an active row —
	// the audit carries the truth, and blanking them would erase the reason
	// a retraction was once made from the row a curious reader opens.
	Retracted       bool
	RetractedAt     string
	RetractedReason string
	RetractedBy     string
}

// FromRow converts a store.LearningRow into a Learning, refusing a row the
// database's CHECK should have kept out — a second place that knows a bad
// value is a second truth (the house rule), so the conversion re-checks
// against the package's own vocabulary and says so.
func FromRow(r store.LearningRow) (Learning, error) {
	sc := Scope(r.Scope)
	if !ValidScope(sc) {
		return Learning{}, fmt.Errorf("learnings: row %d carries scope %q, which is not one of the three — the CHECK should have kept it out", r.ID, r.Scope)
	}
	pr := Provenance(r.Provenance)
	if !ValidProvenance(pr) {
		return Learning{}, fmt.Errorf("learnings: row %d carries provenance %q, which is not one of the four — the CHECK should have kept it out", r.ID, r.Provenance)
	}
	return Learning{
		ID:              r.ID,
		Scope:           sc,
		ScopeKey:        r.ScopeKey,
		Body:            r.Body,
		Provenance:      pr,
		ObservedCount:   r.ObservedCount,
		Tags:            r.Tags,
		SourceLoopSlug:  r.SourceLoopSlug,
		SourceStepID:    r.SourceStepID,
		CreatedAt:       r.CreatedAt,
		Retracted:       r.RetractedAt != nil && *r.RetractedAt != "",
		RetractedAt:     derefStr(r.RetractedAt),
		RetractedReason: r.RetractedReason,
		RetractedBy:     r.RetractedBy,
	}, nil
}

// derefStr is the nil-safe read of a nullable TEXT: absent reads as the
// empty word, which is what "not retracted" sounds like downstream.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// AppendInput is a learning before it is written. Identity (ID) and
// CreatedAt are the store's; the rest is a caller's, validated at the door
// (scope, §3) and at the database (the CHECKs) but never by a second
// authoring path.
type AppendInput struct {
	Scope          Scope
	ScopeKey       string
	Body           string
	Provenance     Provenance
	ObservedCount  *int
	Tags           []string
	SourceLoopSlug *string
	SourceStepID   *int
}

// toRow is the one conversion a write crosses — the AppendInput's shape
// into the store's insert shape. Nothing else reaches the handle's rows.
func (in AppendInput) toRow() store.NewLearningRow {
	return store.NewLearningRow{
		Scope:          string(in.Scope),
		ScopeKey:       in.ScopeKey,
		Body:           in.Body,
		Provenance:     string(in.Provenance),
		ObservedCount:  in.ObservedCount,
		SourceLoopSlug: in.SourceLoopSlug,
		SourceStepID:   in.SourceStepID,
		Tags:           in.Tags,
	}
}

// TagPattern is one tag filter the query narrows by. It is the learnings
// package's own type, not store.TagPattern: a query that crossed the protocol
// boundary would otherwise drag store into the protocol layer (which must not
// import store — TestTheFourPackagesCannotSeeEachOther), and the query filter
// is the one place a caller builds tags. The Query method converts this to the
// store's pattern, so the SQL still reads the store's shape.
type TagPattern struct {
	// Pattern is the tag to match, by prefix when Prefix is set, else by exact
	// equality.
	Pattern string
	Prefix  bool
}

// QueryFilter narrows a read (D5): scope, scope_key and a set of tag
// patterns. An empty Scope/ScopeKey is no filter on that column; an empty
// Tags set is no tag filter. The tag filter runs first and always (D5);
// the embedding route (T3) ranks only within the set this filter returns
// (D6).
type QueryFilter struct {
	// IncludeRetracted is S52 LM2's explicit exception, identical in
	// meaning to the store's: default false — a retracted body is history,
	// not memory, and the default query answers what the daemon would act
	// on.
	IncludeRetracted bool
	Scope            Scope
	ScopeKey         string
	// Tags: each is matched by prefix (Prefix) or exact equality. A set is
	// AND — the learning must carry a row matching every one.
	Tags []TagPattern
	// QueryText, when set and an embedding route is configured (T3), is
	// embedded and used to RANK the tag-filtered set — never to widen it
	// (D6). Empty means no ranking: the set is returned in the store's
	// order. A tag filter with no QueryText is the normal, working query
	// (D5), not a degraded one.
	QueryText string
}

// Refusal is the learnings-flavoured refusal (DB19): a reason, and the
// thing refused. A refusal is not an error — a denied write is a decided
// answer, not a broken pipe.
type Refusal struct {
	// Reason is the sentence: what was refused and why, naming the value.
	Reason string
}
