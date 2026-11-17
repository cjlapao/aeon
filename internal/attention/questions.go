package attention

import (
	"context"
	"strconv"

	"github.com/cjlapao/aeon/internal/store"
)

// questions.go — the `questions` source (SV1): unanswered rows, one item
// each. The predicate is the store's — `answered_at IS NULL`, exactly the
// AwaitingSQL predicate (TestInboxQuestionsMatchStoreAwaiting holds the two
// truths together, the same way `store.awaiting` and the narrow monitor
// agree).
//
// The reason is FIXED ("waiting for your answer") because it is a shape
// fact, not row text; the question's PROMPT is local-only (SV6) — the human
// door may render it, and no copy of it goes into `reason`, so nothing
// downstream ever redacts the wrong field.

// QuestionsSource reads the pending questions.
type QuestionsSource struct {
	Questions store.PendingQuestionsReader // the store read; *store.Store satisfies it
	Owners    *OwnerMaps
}

// Name is the source's stable name in `sources[]`.
func (QuestionsSource) Name() string { return "questions" }

// Items turns live rows into items. Doors are resolved by the registry
// (Evaluate) over the whole collected set — one table, one wiring check,
// wherever an item came from.
func (q QuestionsSource) Items(ctx context.Context, query Query) ([]Item, error) {
	rows, err := q.Questions.PendingQuestionsForScope(query.Scope.RootPaths)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		it := Item{
			Key:      KeyOf("questions", strconv.FormatInt(r.QuestionID, 10), r.AskedAt),
			Kind:     "question",
			Severity: NeedsYou,
			Reason:   "waiting for your answer", // fixed sentence (SV1's example)
			Since:    r.AskedAt,
			LoopSlug: r.LoopSlug,
			ItemRef:  strconv.FormatInt(r.QuestionID, 10),
			Prompt:   r.Prompt, // local-only field, never reason
		}
		it.options = r.OptionsRaw
		if q.Owners != nil {
			it.ComponentID, it.RootID = q.Owners.Owner(r.ComponentRoot)
		}
		out = append(out, it)
	}
	return out, nil
}
