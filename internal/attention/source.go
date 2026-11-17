// Package attention derives "what needs a person" from live state (S58, SV1).
//
// There is no alert table and no cache: an inbox query EVALUATES a registry
// of sources over the rows that are already the truth (the unanswered
// question, the loop's status, the pinned runtime JSON). A stored alert is a
// second truth that drifts from the loop it describes — the DB6 argument that
// removed `awaiting_user` — and SV1 keeps that argument: age is computed at
// read, severity and door are shape in CODE (rule 6, "shape, not records"),
// and the only stored rows in this plane are acks and snoozes (SV3, T3).
//
// The package imports the standard library and `internal/store`'s read
// accessors and nothing else: no SQL lives here (the store files
// `attention_read.go` carry it), no protocol types (the daemon maps Item to
// the wire shape), no clock beyond time.Now at read.
package attention

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Query is one inbox evaluation. RootPaths is the scope ALREADY resolved to
// component root paths (scope.go turns a `rootId` into its tree; empty means
// every component — the ComponentScope convention, a deliberate absence).
type Query struct {
	Scope        Scope
	Severities   []Severity // nil/empty = every severity
	Kinds        []string   // nil/empty = every kind
	IncludeAcked bool
	Limit        int // 0 = DefaultLimit; capped at MaxLimit
}

// The limits the socket applies (T2's behaviour 1): a default an exhausted
// inbox still fits on one screen, and a ceiling a client cannot exceed with
// params, so a huge `limit` is a smaller answer and never a memory event.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Ack is the ack/snooze state of one key (SV3): latest row wins, an item with
// an ack is HIDDEN from the default view and shown with this attached when
// includeAcked; a snooze with a future Until is still hiding.
type Ack struct {
	By    string `json:"by"`
	At    string `json:"at"`
	Until string `json:"until,omitempty"`
}

// AckLookup reads the latest ack for each key (T3 wires it to
// store.LatestAcks; nil means "no acks exist", the honest zero state before
// the write half lands).
type AckLookup func(keys []string) (map[string]Ack, error)

// Source is one contributor to the inbox (SV1's interface, verbatim in
// shape): a stable name and live rows only. A source reads; it writes
// nothing, and it never sees a clock it has to remember between calls.
type Source interface {
	Name() string                                       // "questions", "blocked", "stopped" — and, when their slices land, "budget", "stuck", "gates", "scheduler"
	Items(ctx context.Context, q Query) ([]Item, error) // live rows only
}

// Registry holds the sources and evaluates them (SV1). It is constructed at
// daemon start, filled by Register, and holds NOTHING else — no items, no
// last answer, no memo (N8; TestInboxCachesNothing greps for package-level
// mutable state for the same reason this sentence exists).
type Registry struct {
	sources []Source
	ack     AckLookup
	wired   func(method string) bool // the door guard (SV2, S19's F-1)
}

// knownUnwired is the CLOSED list of source names the sibling slices own
// (SV1's example set, one per slice): budget and stuck (S55), gates (S57),
// scheduler (S27). They appear in every answer as `not-wired` until the
// sibling registers one by this name, so "nothing needs you" can never mean
// "nothing was looking" — the claim is visible per source, not implied by an
// empty list. It is a closed slice in code, and a new sibling source is a
// reviewable line here plus the sibling's own Register call.
var knownUnwired = []string{"budget", "stuck", "gates", "scheduler"}

// NewRegistry builds the registry. wired answers "is this protocol method
// registered?" for the door table (the daemon passes a closure over its real
// table — a door gated on a sibling slice's method that is NOT wired renders
// its generic sentence instead, S19's F-1: never name a phantom door); scope
// resolves a rootId to component paths; ack is T3's lookup (nil until then).
func NewRegistry(wired func(string) bool, ack AckLookup) *Registry {
	if wired == nil {
		wired = func(string) bool { return false }
	}
	return &Registry{wired: wired, ack: ack}
}

// Register adds a source. A duplicate name PANICS, and only at init: two
// sources claiming one name would put two truths behind one `sources[]`
// entry, and the silent version — first or last wins — is exactly the drift
// this slice exists to prevent. Panic at init is loud at boot, which is the
// only safe place for a construction bug.
func (r *Registry) Register(s Source) {
	if s == nil {
		panic("attention: Register(nil source)")
	}
	for _, have := range r.sources {
		if have.Name() == s.Name() {
			panic("attention: two sources named " + s.Name())
		}
	}
	r.sources = append(r.sources, s)
}

// SourceStatus is one source's account of THIS evaluation (SV1): reporting
// (it answered), not-wired (a known sibling source nobody has registered),
// failed (it answered with an error — its items are absent, the error is
// shown, and the OTHER sources still reported).
type SourceStatus struct {
	Name  string
	State string
	Error string
}

// Result is one evaluation. Items are sorted per Less and limited; Sources
// lists every source that was consulted, in registration order then the
// known-unwired names — the full census, not just the ones that found
// something.
type Result struct {
	Items       []Item
	Sources     []SourceStatus
	EvaluatedAt time.Time
}

// Evaluate runs EVERY source and assembles the answer. Only a scope
// refusal is an error out of Evaluate; a source's error is recorded as a
// `failed` status and the call still answers (T2 behaviour 2 — one broken
// source must never blank the inbox, because a person who sees an error and
// an empty list learns nothing about what still needs them).
func (r *Registry) Evaluate(ctx context.Context, q Query) (*Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	var (
		items    []Item
		statuses []SourceStatus
	)
	for _, s := range r.sources {
		found, err := s.Items(ctx, q)
		if err != nil {
			statuses = append(statuses, SourceStatus{Name: s.Name(), State: "failed", Error: err.Error()})
			continue
		}
		items = append(items, found...)
		statuses = append(statuses, SourceStatus{Name: s.Name(), State: "reporting"})
	}

	// Doors resolve centrally, against the REAL table's method set: every
	// kind has exactly one door template (SV2), gated templates fall back to
	// the generic sentence when their sibling plane is not wired (S19's
	// F-1), and one table is the only place a door can be defined.
	for i := range items {
		items[i].DoorMethod, items[i].DoorSentence = Door(items[i].Kind, items[i], r.wired)
	}

	for _, name := range knownUnwired {
		registered := false
		for _, s := range r.sources {
			if s.Name() == name {
				registered = true
			}
		}
		if !registered {
			statuses = append(statuses, SourceStatus{Name: name, State: "not-wired"})
		}
	}

	// Filter, then ack state, then order, then limit — so `limit` truncates
	// the FINAL ordered answer (a truncated view of an unfiltered list could
	// hide the items that would have survived filtering).
	filtered := items[:0]
	for _, it := range items {
		if len(q.Severities) > 0 && !severityIn(it.Severity, q.Severities) {
			continue
		}
		if len(q.Kinds) > 0 && !kindIn(it.Kind, q.Kinds) {
			continue
		}
		filtered = append(filtered, it)
	}
	items = filtered

	sort.SliceStable(items, func(i, j int) bool { return Less(items[i], items[j]) })

	now := time.Now().UTC()
	for i := range items {
		items[i].AgeSeconds = ageSeconds(items[i].Since, now)
	}

	if !q.IncludeAcked || r.ack == nil {
		// Hide acked/snoozed items from the default view; the ack still
		// COUNTS in summary totals (SV3: the number never lies), so the
		// unhidden list and the count are computed from different projections
		// of the same evaluation — the count is the caller's summary path.
		if r.ack != nil && !q.IncludeAcked {
			keys := make([]string, 0, len(items))
			for _, it := range items {
				keys = append(keys, it.Key)
			}
			acks, err := r.ack(keys)
			if err != nil {
				return nil, err
			}
			kept := items[:0]
			for _, it := range items {
				if a, ok := acks[it.Key]; ok && !snoozeExpired(a, now) {
					continue // acked, or snoozed and still inside the horizon
				}
				kept = append(kept, it)
			}
			items = kept
		}
	} else if r.ack != nil {
		keys := make([]string, 0, len(items))
		for _, it := range items {
			keys = append(keys, it.Key)
		}
		acks, err := r.ack(keys)
		if err != nil {
			return nil, err
		}
		for i := range items {
			if a, ok := acks[items[i].Key]; ok && !snoozeExpired(a, now) {
				ack := a
				items[i].Acked = &ack
			}
		}
	}

	if len(items) > limit {
		items = items[:limit]
	}

	return &Result{Items: items, Sources: statuses, EvaluatedAt: now}, nil
}

// snoozeExpired reports whether an ack hides nothing anymore: a plain ack
// hides until undone (never expires on its own); a snooze stops hiding at
// its Until.
func snoozeExpired(a Ack, now time.Time) bool {
	if a.Until == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, a.Until)
	if err != nil {
		return false // an unparsable horizon hides rather than leaks; the row is the truth and a bad one is T3's problem to refuse
	}
	return now.After(t)
}

// ageSeconds is SV1's "age computed at read": from the row's own timestamp,
// clamped at zero for a clock ahead of the stored time, and -1 for a stored
// time that cannot be parsed (honest unknown, never a fake 0 — the
// cost-unknown convention).
func ageSeconds(since string, now time.Time) int64 {
	t, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return -1
	}
	d := now.Sub(t).Seconds()
	if d < 0 {
		return 0
	}
	return int64(d)
}

func severityIn(s Severity, set []Severity) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func kindIn(k string, set []string) bool {
	for _, x := range set {
		if x == k {
			return true
		}
	}
	return false
}

// parseSince is one definition of the stored time format (RFC3339, UTC, as
// the store writes every timestamp column).
func parseSince(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// KeyOf is the stable subject key (SV3): <source>:<id>:<since>. A
// recurrence has a NEW since — a new block, a new stop, a new question row —
// and therefore a new key, which is the mechanism that stops an old ack
// hiding a fresh problem (TestRecurrenceIsANewKey).
func KeyOf(source, id, since string) string {
	return strings.Join([]string{source, id, since}, ":")
}

// Item is one thing that needs a person (SV1's table, in Go). Door is filled
// by doors.go at source time; Prompt is LOCAL-ONLY (SV6) — the daemon's
// human-door methods may render it, and the narrow/agent projections must
// not carry it at all.
type Item struct {
	Key          string
	Kind         string
	Severity     Severity
	RootID       string // the component id (resolved from the root path by the source)
	ComponentID  string
	LoopID       string
	LoopSlug     string
	ItemRef      string // the question id, the step id — what the door acts on
	Reason       string // the daemon's own stored sentence, verbatim
	Since        string
	AgeSeconds   int64
	DoorMethod   string
	DoorSentence string
	Prompt       string // local-only; see the type comment
	Acked        *Ack

	options string // the stored options TEXT, question items only (the door renders it)
}

// String names an item for logs and error text with its key only — never its
// reason or prompt, so a log line cannot leak user text (the SEC pattern
// every sentence-rendering rule in this repo follows).
func (it Item) String() string { return fmt.Sprintf("attention:%s (%s)", it.Key, it.Kind) }
