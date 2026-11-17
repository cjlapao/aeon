package daemon

// S60 T7 (MQ9): the conflict exits and the person's words. A conflicted
// item has three doors and they are all small: `ask` raises ONE gate
// question (count + ids, never hunks — SEC-033) answered {resolve, abort};
// `resolve` starts the resolution loop without asking anyone; `block`
// says so in the log and moves nothing. The answer door for these
// step-less questions is this file's answerMergeQueueGate — the landing
// loop's OWN gate questions (the land-check with a step id) stay the
// ladder's door (the same discriminator S24's queue learned the hard
// way).

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// mqConflictOptions is the closed word-set of a conflict question. The
// store's Ask takes them as options so every surface renders the same two
// words the door validates.
var mqConflictOptions = []string{"resolve", "abort"}

// mqConflictDrive is the conflicted item's policy arm, run both when an
// item ENTERS conflicted and while it HOLDS there (a policy flipped
// ask→resolve must take effect on the next pass — settings are read fresh
// every reconcile).
func (d *Daemon) mqConflictDrive(ctx context.Context, item store.MergeItem, cfg mergequeue.Config) {
	// A person's recorded answer outranks the standing policy: answering
	// `resolve` on an ask item starts the resolution loop just as the
	// resolve policy would (the answer IS a person's act). `abort` needs
	// nothing here — the gather feeds it to Next as Facts.Aborted and the
	// decision table ends the item.
	if ans := d.mqConflictAnswer(item); ans == "resolve" {
		d.ensureResolutionLoop(ctx, item)
		return
	}
	switch cfg.ConflictPolicy {
	case mergequeue.PolicyResolve:
		d.ensureResolutionLoop(ctx, item)
	case mergequeue.PolicyAsk:
		// Attended OR not: the question row is what makes a conflict
		// inbox-VISIBLE (behaviour 5: "no loop starts, an S58 attention
		// event fires, nothing else moves"). An unattended item's
		// question is answered over the socket by a person — or left
		// alone; the item simply holds.
		d.raiseMergeConflictQuestion(ctx, item)
	case mergequeue.PolicyBlock:
		// blocked-visible: the item's own status and the log line are
		// the event. No question, no loop, nothing else moves.
	}
}

// raiseMergeConflictQuestion asks ONCE (the S24 ask-once discipline: a
// step-less gate question already standing on this loop's slug means the
// question was raised and not yet answered; a fresh one after an answer
// would re-ask what was already said). The body names the item, the
// count, and the two words — paths at most (never hunks, never content).
func (d *Daemon) raiseMergeConflictQuestion(ctx context.Context, item store.MergeItem) {
	slug := item.LandingLoopSlug
	if slug == "" {
		d.logf("merge-queue: item %s: conflicted with no landing loop to ask on", item.ID)
		return
	}
	scope := store.Scope{ComponentRoot: item.ComponentRoot, LoopSlug: slug}
	if _, found, err := d.store.PendingQuestion(scope); err == nil && found {
		return // a question stands — one at a time
	} else if err != nil {
		d.logf("merge-queue: item %s: pending question unreadable: %v", item.ID, err)
		return
	}
	if last, ok, err := d.store.LatestLoopGateQuestion(item.ComponentRoot, slug); err == nil && ok && last.StepID == nil {
		return // this item's conflict question was already asked on this loop
	}
	count := 0
	if item.ID != "" {
		if paths, ok := d.mqUnmergedPaths(ctx, item.ComponentRoot, item.ID); ok {
			count = len(paths)
		}
	}
	body := fmt.Sprintf("merge-queue: item %s (%s into %s) reports %d conflicted path(s) in the integration worktree. Answer \"resolve\" to start the resolution loop, or \"abort\" to end the item. The hunks stay in the worktree.",
		item.ID, item.Branch, item.BaseBranch, count)
	if _, err := d.store.Ask(scope, store.NewQuestion{
		Kind:    store.QuestionGate,
		Body:    body,
		Options: mqConflictOptions,
	}); err != nil {
		d.logf("merge-queue: item %s: conflict question could not be raised: %v", item.ID, err)
		return
	}
	d.logf("merge-queue: item %s: conflict question raised on %s", item.ID, slug)
}

// mqConflictAnswer reads the person's word from the standing/answered
// step-less gate question: "" (nothing said), "resolve", or "abort".
// A junk answer is "" — it is neither word, and the door that recorded
// it refused it before it could stand (junk never claims, the queue's
// rule); a stray junk answer on the ledger must not decide anything.
func (d *Daemon) mqConflictAnswer(item store.MergeItem) string {
	if item.LandingLoopSlug == "" {
		return ""
	}
	q, ok, err := d.store.LatestLoopGateQuestion(item.ComponentRoot, item.LandingLoopSlug)
	if err != nil || !ok || q.StepID != nil || !q.Answered() {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(q.Answer)) {
	case "resolve":
		return "resolve"
	case "abort":
		return "abort"
	}
	return ""
}

// answerMergeQueueGate is the answer-door arm for conflict questions.
// It claims a question ONLY under three facts (the S24 order — cheapest
// and loudest first): the slug is a loop this queue minted and is the
// landing loop OF an open item; that item reads conflicted; the pending
// question is a step-less gate (the landing loop's own land-check gate
// has a step id and belongs to the ladder's door). Everything else
// falls through to the ordinary ladder answer, answered nowhere near
// here.
//
// The door records the WORD; the reactor moves the item (abort →
// failed(conflict) through Next's Aborted fact; resolve → the resolution
// loop through the policy arm). A replay is refused with the first
// answer's timestamp, never re-executed — the claim-once rule.
func (d *Daemon) answerMergeQueueGate(ctx context.Context, p protocol.LoopAnswerParams, root, slug string) (bool, any, error) {
	items, err := d.store.ListOpenItemsByLoop(root, slug)
	if err != nil {
		return false, nil, nil // an unreadable ledger is not a claim; the ladder answers
	}
	var item *store.MergeItem
	for i := range items {
		if items[i].LandingLoopSlug == slug && items[i].Status == string(mergequeue.StatusConflicted) {
			item = &items[i]
			break
		}
	}
	if item == nil {
		return false, nil, nil
	}
	q, found, err := d.store.PendingQuestion(store.Scope{ComponentRoot: root, LoopSlug: slug})
	if err != nil {
		return false, nil, err
	}
	if !found {
		last, ok, lerr := d.store.LatestLoopGateQuestion(root, slug)
		if lerr != nil {
			return true, nil, lerr // an unreadable ledger answers NOTHING
		}
		if ok && last.Answered() && last.StepID == nil {
			return true, map[string]any{"refused": map[string]string{
				"kind":   "already-answered",
				"detail": fmt.Sprintf("question %d was already answered at %s; a second answer is refused because the first may already have started a loop", last.ID, last.AnsweredAt),
			}}, nil
		}
		return false, nil, nil
	}
	if q.Kind != string(store.QuestionGate) || q.StepID != nil {
		return false, nil, nil
	}
	word := strings.ToLower(strings.TrimSpace(p.Answer))
	if word != "resolve" && word != "abort" {
		return true, map[string]any{"refused": map[string]string{
			"kind":   "unknown-answer",
			"detail": `the merge queue's conflict question is answered "resolve" or "abort" — nothing was recorded`,
		}}, nil
	}
	ans, err := d.store.Answer(q.ID, word)
	if err != nil {
		return true, nil, err
	}
	if !ans.OK {
		return true, map[string]any{"refused": map[string]string{
			"kind":   "already-answered",
			"detail": fmt.Sprintf("question %d was already answered (%s); the queue claims a conflict answer exactly once", q.ID, ans.Reason),
		}}, nil
	}
	// An abort also stops the item's loops NOW (the resolution loop was
	// never started; the blocked landing loop is retired with the item's
	// verdict arriving on the next pass — behaviour 4's sibling).
	if word == "abort" {
		d.StopMergeQueueItemLoops(ctx, *item)
	}
	d.mergeQueueWake()
	return true, map[string]any{
		"item":       item.ID,
		"questionId": q.ID,
		"answer":     word,
	}, nil
}

// RequestMergeQueueLand is T8's door hook: a person's `mergequeue.land`
// stands as a fact the reactor consumes when it next reads a ready item
// (the standing request, not a queue write — the write is the decision
// table's, and it happens exactly once, with the head-binding checked at
// the moment of landing rather than the moment of typing).
func (d *Daemon) RequestMergeQueueLand(itemID string) {
	d.mqLandMu.Lock()
	if d.mqLand == nil {
		d.mqLand = map[string]bool{}
	}
	d.mqLand[itemID] = true
	d.mqLandMu.Unlock()
	d.mergeQueueWake()
}

// landRequested reads the standing request fact (the answer is a
// bool by design: the map is the ledger of the person's intent between
// the door and the decision; the durable trail is the event the decision
// writes).
func (d *Daemon) landRequested(itemID string) bool {
	d.mqLandMu.Lock()
	defer d.mqLandMu.Unlock()
	return d.mqLand[itemID]
}

// clearLandRequest retires a consumed (or ended-upon) request.
func (d *Daemon) clearLandRequest(itemID string) {
	d.mqLandMu.Lock()
	delete(d.mqLand, itemID)
	d.mqLandMu.Unlock()
}
