package protocol

// digest.go — S63 T5 (RT12): the "what did we learn" digest.
//
// ONE read over its own seam. The result is a content spec made of
// Go: RT12's seven sections are STRUCT FIELDS in RT12's order, so the
// fixed order is the wire order (json.Marshal writes fields in declared
// order) and TestDigestSectionsAreFixedOrder pins the emitted key order
// against the design's list. A digest that reordered itself would be a
// screen that redraws itself — S58 renders these sections top to bottom
// and a person reads them as an argument: window, outcomes, rework,
// blocked, cost, learned, attention.
//
// What it is NOT: delivery, scheduling, ranking, or free text (S58 owns
// the channel; the three top-rework blueprints are the most rework-heavy
// BY COUNT, stated with their counts — a ladder of integers, not an
// opinion; the largest loop is A LINK (root, slug), never its goal).
//
// Empty windows return every section with its cannot-say/zero shape and
// the sentence — not an error (RT12). The digest performs no delivery
// and schedules nothing.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MethodDigestLearned is the plane's one door.
const MethodDigestLearned MethodName = "digest.learned"

func digestMethodNames() []MethodName { return []MethodName{MethodDigestLearned} }

// DigestParams: ID resolves like a card subject (component id or a
// component_root verbatim). Empty window means the default lookback.
type DigestParams struct {
	ID    string `json:"id"`
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
}

// --- the sections, in RT12's fixed order --------------------------------------

// DigestOutcomeCount is one outcome kind's loops in a window; Previous
// holds the SAME shape read over the previous EQUAL window (RT12 §2's
// two reads — a comparison shown as two numbers, never as a delta).
type DigestOutcomeCount struct {
	Kind  string   `json:"kind"`
	Loops int      `json:"loops"`
	Rate  *float64 `json:"rate"`
}

type DigestOutcomesSection struct {
	CardWindow WindowWire             `json:"cardWindow"`
	ByKind     []DigestOutcomeCount   `json:"byKind"`
	Previous   *DigestOutcomesSection `json:"previous,omitempty"`
}

// DigestReworkSection: the four round totals (means over judged loops —
// they come off the card's own metrics, states and n included), plus
// the three most rework-heavy BLUEPRINTS by round-sum (ids and integers;
// item text never rides).
type DigestBlueprintRework struct {
	BlueprintID string `json:"blueprintId"`
	Rounds      int    `json:"rounds"`
}

type DigestReworkSection struct {
	ReviewRounds   *MetricWire             `json:"reviewRounds"`
	SecurityRounds *MetricWire             `json:"securityRounds"`
	RetryRounds    int                     `json:"retryRounds"`
	QARounds       int                     `json:"qaRounds"`
	TopBlueprints  []DigestBlueprintRework `json:"topBlueprints"`
}

// DigestBlockedSection: kinds only (S55's cap-hit/stuck and S57's gate
// kinds appear as themselves when rows carry them).
type DigestBlockedCount struct {
	Kind  string `json:"kind"`
	Loops int    `json:"loops"`
}

type DigestBlockedSection struct {
	Top []DigestBlockedCount `json:"top"`
}

// DigestCostSection: the floor sentence rendered by history's renderer
// (the one formatter's law — the daemon puts the RENDERED string in, the
// section never carries a raw sum for a client to re-render), the
// per-completed-item metric with its state, and the largest loop as a
// link.
type DigestCostSection struct {
	Floor            string      `json:"floor"`
	UnknownSteps     int         `json:"unknownSteps"`
	PerCompletedItem MetricWire  `json:"perCompletedItem"`
	LargestLoop      *DigestLink `json:"largestLoop"`
}

// DigestLink names a loop by (root, slug) — an id pair, never text.
type DigestLink struct {
	Root string `json:"root"`
	Loop string `json:"loop"`
}

// DigestLearningItem is one stored learning quoted VERBATIM: it is
// already-validated words (and a retro body is a template render), so
// the digest quotes rather than re-renders — a second formatter is a
// second truth.
type DigestLearningItem struct {
	ID             int64  `json:"id"`
	Scope          string `json:"scope"`
	ScopeKey       string `json:"scopeKey"`
	Provenance     string `json:"provenance"`
	ObservedCount  *int   `json:"observedCount"`
	SourceLoopSlug string `json:"sourceLoopSlug,omitempty"`
	CreatedAt      string `json:"createdAt"`
	Body           string `json:"body"`
}

// DigestLearnedSection groups by provenance in HONESTY ORDER — told,
// observed, assumed, relayed — a fixed four-entry slice, empty groups
// included: the order is the point, and a group that renders when
// non-empty only would let absence reorder the rest.
type DigestProvenanceGroup struct {
	Provenance string               `json:"provenance"`
	Items      []DigestLearningItem `json:"items"`
}

type DigestLearnedSection struct {
	Groups []DigestProvenanceGroup `json:"groups"`
}

// DigestAttentionSection is the person's half: what needs a human, as
// links and counts, and the explicit "nothing needs you" when the
// answer is nothing (RT12 — silence is not an answer shape).
type DigestBlockedNow struct {
	Root       string `json:"root"`
	Loop       string `json:"loop"`
	Kind       string `json:"kind"`
	AgeMinutes int64  `json:"ageMinutes"`
}

type DigestAttentionSection struct {
	BlockedNow       []DigestBlockedNow `json:"blockedNow"`
	PendingQuestions int                `json:"pendingQuestions"`
	ProposalsPending int                `json:"proposalsPending"`
	Statement        string             `json:"statement"`
}

// DigestResult is RT12's seven sections IN ORDER. Field order is the
// contract (the file header says why); the ordered-keys test reads the
// marshalled bytes, not this declaration, so a reordering fails.
type DigestResult struct {
	Window    WindowWire             `json:"window"`
	Statement string                 `json:"statement"`
	Coverage  CoverageWire           `json:"coverage"`
	Outcomes  DigestOutcomesSection  `json:"outcomes"`
	Rework    DigestReworkSection    `json:"rework"`
	Blocked   DigestBlockedSection   `json:"blocked"`
	Cost      DigestCostSection      `json:"cost"`
	Learned   DigestLearnedSection   `json:"learned"`
	Attention DigestAttentionSection `json:"attention"`
}

// --- the seam and registration --------------------------------------------------

type DigestService interface {
	Learned(ctx context.Context, p DigestParams) (DigestResult, error)
}

// RegisterDigest registers the digest door over a non-nil seam (V13).
func (t *Table) RegisterDigest() bool {
	if t.Digest == nil {
		return false
	}
	t.register(&Method{Name: MethodDigestLearned, RequiresInitialize: true, Handle: t.handleDigestLearned})
	return true
}

var digestIdentityKeys = []string{"actor", "actorKind", "identity", "peer", "uid"}

func (t *Table) handleDigestLearned(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if len(raw) > 0 {
		var bag map[string]json.RawMessage
		if err := json.Unmarshal(raw, &bag); err == nil {
			for _, k := range digestIdentityKeys {
				if _, ok := bag[k]; ok {
					return nil, ErrBadParams{Err: fmt.Errorf(
						"digest.learned takes no %q — a digest is about the component it names, and its attention section is counts, not caller identity", k)}
				}
			}
		}
	}
	if t.Digest == nil {
		return nil, ErrRetroUnavailable.WithWhat("no store is wired — there are no outcomes to digest")
	}
	var p DigestParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("digest.learned needs a component id (an unregistered id is read as a component_root verbatim)")}
	}
	return t.Digest.Learned(ctx, p)
}
