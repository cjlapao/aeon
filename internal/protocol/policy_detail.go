package protocol

// ════════════════════════════════════════════════════════════════════════════
// The detail reads (S45 T2, PM5/PM6): the list gains its details, and the
// vocabulary gains a server.
//
// # What this file is
//
// Two methods, both additive reads over the SAME *policy.Store the gate and
// the four S10 reads use (daemon.go wires `Table.PolicyStore` once) — the
// app is never shown a set the runtime is not deciding with, and never shown
// a detail that left the fail-closed read by a side door:
//
//   - `policy.get {id}` answers the WHOLE row: the identity view (with its
//     additive describe/createdAt), the raw spec — here and only here, the
//     list stays the identity view — and the two informational records: who
//     accepted this rule for its target (PL10: the enforcement path does not
//     know this question exists), and whether the audit carries the
//     authoring record (the RecordAuthored row). Unknown ids REFUSE with the
//     typed code naming the id and the nearest ids — a wrong guess gets a
//     pointer, not a shrug (the S44 settings refusal's shape).
//   - `policy.vocabulary {}` answers the closed sets, the authorship matrix
//     COMPUTED from the rule, and the per-type spec descriptors — from ONE
//     daemon-side source, so no client hardcodes what the engine owns (PM6:
//     a UI that guesses the vocabulary drifts from the engine). It needs no
//     store: the answer is a function of the compiled code, and refusing it
//     with ErrStoreUnavailable would claim the daemon cannot state facts it
//     ships in its own binary.
//
// # Zero writes (D9), as every read here
//
// None of the two touches the recorder, the trim, or the tables. The audit
// rows a detail reports are READ (ReportEvents / AcceptancesFor — the
// boundary's own fail-closed readers); an answer with no record is a stated
// absence (false), never a half-written one, and a reader that cannot answer
// refuses loudly rather than printing a reassuring default.
//
// # The sentences travel rendered
//
// describe is policy.Describe's output from the engine — this file assembles
// nothing (D9; the verdict-sentence rule carried onto rules). A test pins
// the wire value against a direct call so a drift here is a build failure.
// ════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/cjlapao/aeon/internal/policy"
)

// --- policy.get ---------------------------------------------------------------

// PolicyGetParams is `policy.get`'s params: the one id.
type PolicyGetParams struct {
	ID string `json:"id"`
}

// PolicyDetail is one policy WHOLE — the identity view plus the raw spec
// (which rides `policy.get` and never `policy.list`: the list is the
// identity view, so an app browsing a table of rules does not pull every
// rule's content), plus the two records the read side is allowed to speak
// of. Both records are INFORMATIONAL (PL10's rule, carried from acceptances
// to the authored audit): nothing in the enforcement path consults either.
type PolicyDetail struct {
	PolicyView
	// Spec is the row's spec, raw and untouched — the same bytes the write
	// boundary validated. A client that wants to RENDER it uses describe;
	// a client that wants to RESUBMIT it (T4's edit) round-trips this.
	Spec json.RawMessage `json:"spec"`
	// AcceptedBy/AcceptedAt are the acceptance record for this rule at
	// this target, when one exists (""/absent = nobody on record was told
	// — which is a fact the UI may render as such, never an error).
	AcceptedBy string `json:"acceptedBy,omitempty"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
	// Authored reports the audit carries the RecordAuthored row for this
	// rule at this subject. Subjects outside the audit's two kinds
	// (everything but agent/loop) can carry no such row — parseSubject
	// says so at the write side — so false there is the stated truth, not
	// a gap.
	Authored bool `json:"authored"`
}

// PolicyGetResult is `policy.get`'s answer envelope.
type PolicyGetResult struct {
	Policy PolicyDetail `json:"policy"`
}

// policyAuthoredScanLimit bounds nothing: the existence question ("does the
// audit carry this rule's authored record") is answered over the subject's
// whole 90-day-capped slice, because a windowed scan could report false for
// a record that exists — and an informational field is only worth having if
// it never lies. The rows are the same bounded set policy.events already
// reads; this is not an unbounded walk of an unbounded table.
const policyAuthoredScanLimit = math.MaxInt32

func (t *Table) handlePolicyGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	store, err := t.requirePolicyStore()
	if err != nil {
		return nil, err
	}
	var p PolicyGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	// S45 T7 (PM9): the whole answer moved VERBATIM to PolicyGetAnswer
	// (policy_reads.go) — one read path for every policy answer, shared
	// with the records tool so policy_show and policy.get cannot become
	// two implementations.
	res, err := PolicyGetAnswer(ctx, store, p, now())
	if err != nil {
		return nil, err
	}
	return res, nil
}

// nearestPolicyIDs ranks the store's ids by edit distance to the one asked
// for (case-insensitive; the ids people mistype are names, not hashes) and
// renders up to three, quoted. Ties break by id so the refusal is stable.
// An empty store says "none — the table holds no policies", which is its own
// useful answer.
func nearestPolicyIDs(asked string, rows []policy.Listed) string {
	if len(rows) == 0 {
		return "none — the table holds no policies"
	}
	type ranked struct {
		id       string
		distance int
	}
	const want = 3
	var top []ranked
	for _, r := range rows {
		top = append(top, ranked{id: r.ID, distance: editDistance(strings.ToLower(asked), strings.ToLower(r.ID))})
	}
	for i := 1; i < len(top); i++ { // tiny insertion sort; the ids-per-store set a refusal quotes is small
		for j := i; j > 0 && (top[j].distance < top[j-1].distance ||
			(top[j].distance == top[j-1].distance && top[j].id < top[j-1].id)); j-- {
			top[j], top[j-1] = top[j-1], top[j]
		}
	}
	if len(top) > want {
		top = top[:want]
	}
	quoted := make([]string, 0, len(top))
	for _, r := range top {
		quoted = append(quoted, fmt.Sprintf("%q", r.id))
	}
	return strings.Join(quoted, ", ")
}

// editDistance is the bounded Levenshtein over RUNES (ids are gated to 64
// runes at the write boundary, so this DP is bounded too). Insertions,
// deletions and substitutions each cost one; it is the measure a human
// mistyping a name actually violates.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// --- policy.vocabulary ----------------------------------------------------------

// PolicyVocabularyResult is `policy.vocabulary`'s answer: the whole closed
// picture, served by the daemon so no client hardcodes it (PM6).
type PolicyVocabularyResult struct {
	Vocabulary policy.VocabularyView `json:"vocabulary"`
}

func (t *Table) handlePolicyVocabulary(_ context.Context, _ *Session, params json.RawMessage) (any, error) {
	// The params are none, and the answer needs no store: the closed sets,
	// the authorship matrix and the spec descriptors are properties of the
	// compiled engine (types.go, store.go's rule, the registered
	// validators). A storeless daemon can still state its own vocabulary —
	// refusing it would be the answer that lies about what it cannot do.
	if err := decodeParams(params, &struct{}{}); err != nil {
		return nil, err
	}
	return PolicyVocabularyResult{Vocabulary: policy.Vocabulary()}, nil
}
