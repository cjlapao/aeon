package protocol

// inbox.go — S58 T2: the attention plane's two reads (SV1, SV13).
//
// # What this layer is
//
// The wire shape of "what needs a person", plus the door's refusals. All
// derivation lives in `internal/attention` (sources, severity, doors) and
// all rows come from `internal/store`'s read accessors; this file maps
// params in, results out, and owns nothing else. The service seam returns
// the attention types' WIRE twins (InboxListResult...), assembled by the
// daemon — the same one-adapter rule the budget views follow ("types are
// this package's, field-for-field twins of what the daemon assembles").
//
// # Absence (V13)
//
// Both methods register only over a non-nil Inbox seam; a supervision-less
// build answers method-not-found, never a refusal.
//
// # Nothing is stored
//
// SV1 has an unusual consequence for a wire type: `ageSeconds` is computed
// at READ (an item's own stored timestamp against now), so a client that
// re-reads after ten minutes sees ten older items and no protocol field
// that could have been cached between the two calls.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/attention"
)

// InboxService is the daemon seam: the registry, evaluated. The service
// resolves the scope (an unknown rootId rides back as attention.ErrUnknownScope
// wrapped, and the handler renders ErrInboxScope — the ONLY place the
// protocol name appears), runs the sources, and assembles these types.
type InboxService interface {
	InboxList(ctx context.Context, p InboxListParams) (InboxListResult, error)
	InboxSummary(ctx context.Context, p InboxSummaryParams) (InboxSummaryResult, error)
}

// --- params -----------------------------------------------------------------

// InboxListParams are all optional (SV1): no rootId is every tree, no
// severity/kinds filter is every item, `limit` is capped by the registry
// (MaxLimit), not by a client's wish.
type InboxListParams struct {
	RootID       string   `json:"rootId,omitempty"`
	Severity     string   `json:"severity,omitempty"`     // one of the five SV2 classes
	Kinds        []string `json:"kinds,omitempty"`        // exact kind strings
	IncludeAcked bool     `json:"includeAcked,omitempty"` // SV3: acked items hide by default
	Limit        int      `json:"limit,omitempty"`        // default 100, max 500 (attention's limits)
}

// InboxSummaryParams: the scope, and nothing else — a count is not a filter.
type InboxSummaryParams struct {
	RootID string `json:"rootId,omitempty"`
}

// --- results ----------------------------------------------------------------

// InboxDoor is the exact action a person takes (SV2). An empty `method` is
// honest, not missing: read-only doors have no command on this build, and a
// gated door whose sibling plane is unwired renders the generic sentence
// with no method (S19's F-1 — never name a phantom command).
type InboxDoor struct {
	Method   string `json:"method,omitempty"`
	Sentence string `json:"sentence"`
}

// InboxAcked is an item's ack state (SV3): who hid it, when, and until when
// (a snooze only).
type InboxAcked struct {
	By    string `json:"by"`
	At    string `json:"at"`
	Until string `json:"until,omitempty"`
}

// AttentionItem is one thing that needs a person (SV1's table on the wire).
type AttentionItem struct {
	Key         string      `json:"key"`
	Kind        string      `json:"kind"`
	Severity    string      `json:"severity"`
	RootID      string      `json:"rootId,omitempty"`
	ComponentID string      `json:"componentId,omitempty"`
	LoopID      string      `json:"loopId,omitempty"`
	LoopSlug    string      `json:"loopSlug,omitempty"`
	ItemRef     string      `json:"itemRef,omitempty"`
	Reason      string      `json:"reason"`
	Since       string      `json:"since"`
	AgeSeconds  int64       `json:"ageSeconds"`
	Door        InboxDoor   `json:"door"`
	Prompt      string      `json:"prompt,omitempty"` // LOCAL-ONLY by contract (SV6): the human door renders it; no projection that is not the person's may carry it
	Acked       *InboxAcked `json:"acked,omitempty"`
}

// InboxSourceState is one source's account (SV1): reporting, not-wired, or
// failed — the census that makes "nothing needs you" auditable.
type InboxSourceState struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Error string `json:"error,omitempty"` // failed only, the source's own sentence
}

// InboxListResult is one evaluation: items in inbox order (severity, then
// oldest, then key), the full source census, and the read's instant.
type InboxListResult struct {
	Items       []AttentionItem    `json:"items"`
	Sources     []InboxSourceState `json:"sources"`
	EvaluatedAt string             `json:"evaluatedAt"`
}

// InboxSummaryResult counts (SV3: acked items COUNT, so the number never
// lies) and reports the oldest unacked item's time when anything waits.
type InboxSummaryResult struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"bySeverity"`
	// Acked is how many of `total` are currently hidden behind a mark
	// (S58 T3, SV3): total keeps counting them, and naming the split
	// means a reader never mistakes "total 3, shown 1" for a lying view.
	Acked       int                `json:"acked"`
	OldestSince string             `json:"oldestSince,omitempty"`
	Sources     []InboxSourceState `json:"sources"`
}

// --- handlers ----------------------------------------------------------------

func (t *Table) handleInboxList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p InboxListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if p.Severity != "" && !attention.ValidSeverity(p.Severity) {
		// The closed vocabulary refuses by name (DB19), like every other
		// filter param in this package: a filter matching nothing on a typo
		// is a lie-shaped answer.
		return nil, ErrInvalidFilter.WithWhat("severity %q is not one of needs-you, stuck, blocked, stopped, info", p.Severity)
	}
	if p.Limit < 0 {
		return nil, ErrInvalidFilter.WithWhat("limit must not be negative, got %d", p.Limit)
	}
	res, err := t.Inbox.InboxList(ctx, p)
	if err != nil {
		return nil, inboxScopeRefusal(err)
	}
	return res, nil
}

func (t *Table) handleInboxSummary(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p InboxSummaryParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	res, err := t.Inbox.InboxSummary(ctx, p)
	if err != nil {
		return nil, inboxScopeRefusal(err)
	}
	return res, nil
}

// inboxScopeRefusal maps the service's typed scope refusal onto the wire
// error, carrying the offending id in `what`; every other error passes
// through UNTOUCHED (the mcpCall discipline — a paraphrase here would be a
// second authority over a fact the service owns).
func inboxScopeRefusal(err error) error {
	if errors.Is(err, attention.ErrUnknownScope) {
		id := ""
		if u := err; u != nil {
			id = strings.TrimPrefix(u.Error(), attention.ErrUnknownScope.Error()+": ")
		}
		return ErrInboxScope.WithWhat("%s (rootId %q)", ErrInboxScope.Data.What, id)
	}
	return err
}

// RegisterInbox registers the two reads over a non-nil seam (V13, the
// RegisterBudget shape). It returns whether the methods are in the table,
// so the daemon's wiring can be asserted rather than trusted.
func (t *Table) RegisterInbox() bool {
	if t.Inbox == nil {
		return false
	}
	t.register(&Method{Name: MethodInboxList, RequiresInitialize: true, Handle: t.handleInboxList})
	t.register(&Method{Name: MethodInboxSummary, RequiresInitialize: true, Handle: t.handleInboxSummary})
	return true
}
