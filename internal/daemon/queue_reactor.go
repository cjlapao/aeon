package daemon

// S24 T3 — the reactor: the one consumer of a loop's TERMINAL events that
// belongs to the queue. Its whole contract is CG2 and CG4:
//
//   - `done`  → the item reads done; then `queue_advance` decides: `auto`
//     materializes the next ready item at once, `ask` raises a pending
//     question instead and waits (T4 answers it).
//   - `blocked` (ANY kind — the queue never diagnoses which, CG2's
//     refusal to duplicate internal/loop's taxonomy) → the item reads
//     blocked and the question is raised REGARDLESS of `queue_advance`.
//     An exhausted loop is never a "the project configured itself past
//     this" outcome.
//   - CG4: NOTHING here polls or watches. The reactor runs only in
//     response to an event it actually received; materializing a project's
//     FIRST item is the explicit `advanceQueue` door (T6/T7 call it) —
//     the same way the first loop of a project has always been an
//     explicit `loop.start`.
//
// An untracked loop (a person started it directly) finishing is NOT this
// reactor's business: the lookup misses and the reactor does nothing,
// quietly. And it never blocks the emit path — every branch is a
// goroutine, because V18's promise is that the queue never delays a
// step; the event fires and the daemon moves on.

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/store"
)

// onQueueTerminal is the subscription, called from publishLoop at the
// daemon's own emission point (blueprint.go). It resolves nothing on the
// caller's thread — dispatch to the goroutine is the whole function.
func (d *Daemon) onQueueTerminal(kind, componentRoot, loopSlug string, fields map[string]any) {
	if d.store == nil {
		return
	}
	status, _ := fields["status"].(string)
	reason := queueReason(fields)
	go d.handleQueueTerminal(context.Background(), kind, componentRoot, loopSlug, status, reason, queueReasonKind(fields))
}

// queueReasonKind reads the CLOSED kind a blocked event carries — the
// sibling's taxonomy token (`model-error`, `cap-hit`, S57's `gate-failed`),
// never its prose. Only the struct arm of the emitter speaks it; the flat
// string arm carries no kind, and CN13 refuses to invent one: an
// unclassified blocked report files no needs-you row.
func queueReasonKind(fields map[string]any) string {
	if r, ok := fields["reason"].(*loop.BlockedReason); ok && r != nil {
		return r.Kind
	}
	return ""
}

// queueReason reads the reason a terminal event carries, in whatever shape
// the emitter spoke it: the loop engine speaks its blocked reason as a
// STRUCT (loop.BlockedReason — the kind and the detail, the refusal shape
// DB19 exists for), and a string is the flat form a test or an older
// emitter hands over. Neither may drop: a blocked loop whose reason is read
// as empty raises a queue question that says "blocked" without saying why —
// the exact plausible-wrong-answer the queue's question is there to avoid.
func queueReason(fields map[string]any) string {
	if r, ok := fields["reason"].(*loop.BlockedReason); ok && r != nil {
		return r.Detail
	}
	if s, ok := fields["reason"].(string); ok {
		return s
	}
	return ""
}

// handleQueueTerminal does the deciding. Package-visible to the tests —
// they call it synchronously through publishLoop, which is the wired
// path; the seam under test is behaviour, not the goroutine.
func (d *Daemon) handleQueueTerminal(ctx context.Context, kind, componentRoot, loopSlug, status, reason, blockedKind string) {
	if kind == protocol.KindLoopBlocked && d.continuousOwns(componentRoot) {
		// CN13 (S27 T11): for a roadmap-sourced unit the queue path does
		// not exist — the item stays `running` (D6/F8) — so this is the
		// one place that can say "needs you" durably. It files BEFORE the
		// queue lookup below, because a roadmap loop is nobody's queue
		// item and the lookup would only miss.
		d.fileBlockedNeedsYou(componentRoot, loopSlug, blockedKind)
	}
	q := queue.New(d.store)
	item, ok, err := d.store.QueueItemByLoopSlug(componentRoot, loopSlug)
	if err != nil || !ok {
		// Untracked (or unreadable — say it, never act on it). A person's
		// own loop finishing is not the queue's news (T3 §3).
		if err != nil {
			d.log.Infof("queue: terminal lookup failed for loop %s: %v", loopSlug, err)
		}
		return
	}
	switch kind {
	case protocol.KindLoopBlocked:
		fresh, r := q.Transition(queueItemFromRow(item), queue.StatusBlocked)
		if r != nil {
			d.log.Infof("queue: the blocked report was refused: %s (item %d)", r.Reason, item.ID)
			return
		}
		// CG2: halt and ask, ALWAYS. The question rides the just-blocked
		// loop's slug (design §2 — the FK needs an existing loop row).
		d.raiseQueueQuestion(ctx, componentRoot, loopSlug, blockedQuestionBody(fresh, reason))
		if d.continuousOwns(componentRoot) {
			// S27 T5 (CN19): the scheduler holds the wheel for this
			// repository — the halt still blocks and still asks (the
			// status write and the question are never skipped), and the
			// kick only hastens the tick that will SEE the halt.
			d.continuousKick(componentRoot)
		}
	case protocol.KindLoopFinished:
		// §2 measured `done` as the only status this event carries —
		// ASSERT it, do not assume it holds forever: anything else is a
		// wire surprise worth a log, and not the queue's business.
		if status != "done" {
			d.log.Infof("queue: loop.finished for %s carried status %q — a status this reactor does not know moves nothing", loopSlug, status)
			return
		}
		if _, r := q.Transition(queueItemFromRow(item), queue.StatusDone); r != nil {
			d.log.Infof("queue: the done report was refused: %s (item %d)", r.Reason, item.ID)
			return
		}
		if d.continuousOwns(componentRoot) {
			// S27 T5: the scheduler holds the wheel for this repository
			// (enabled root + master on), so its tick picks the next unit
			// — the queue does not auto-advance beneath the scheduler, and
			// the ask-question is not raised when the answer would start a
			// second thing the scheduler already claimed. An unenabled or
			// paused scheduler never enters here: T4's path below runs
			// byte-identical for every repository it does not own.
			d.continuousKick(componentRoot)
			return
		}
		advance, _, ok, err := d.store.ComponentQueueSettings(componentRoot)
		if err != nil || !ok {
			d.log.Infof("queue: the finished loop's component settings could not be read for %s: %v", componentRoot, err)
			return
		}
		if advance == "ask" {
			// T4's door: a gate question naming the NEXT item, answered →
			// materialize. No next item means nothing to ask about.
			next, has, qr := q.NextReady(componentRoot)
			if qr != nil || !has {
				return
			}
			d.raiseQueueQuestion(ctx, componentRoot, loopSlug,
				fmt.Sprintf("next queued item: %s — advance?", next.Goal))
			return
		}
		d.advanceNext(ctx, componentRoot)
	}
}

// advanceNext is the ONE materialization code path (T4's answered-answer
// branch calls this same function — two callers, never two paths). It
// claims the next ready item claimed-FROM, starts it through the EXISTING
// `loop.start` door (d.Table.StartBlueprintLoop — brief §4's "no second
// execution path", and the call-site test pins it), and stamps the item
// with the loop it produced.
//
// A failed start lands the item `blocked` with a question, not back to
// `ready`: the machine (CG1) has no `running → ready` edge — a claim
// that started nothing is a stall a person should SEE, and `blocked` is
// the vocabulary's honest word for it (the same edge carries a blocked
// loop's halt, and the cancel-escape hatch is there either way).
func (d *Daemon) advanceNext(ctx context.Context, componentRoot string) (started bool) {
	q := queue.New(d.store)
	item, has, qr := q.NextReady(componentRoot)
	if qr != nil {
		return false
	}
	if !has {
		// Nothing is ready. In `skill` mode that usually means the head of
		// the backlog has never been sized — so the advance the person
		// (or the auto branch) just invoked DISPATCHES the sizing instead
		// of silently doing nothing. Manual mode stays out of it (D7) and
		// the call behaves exactly as before.
		d.startSizingRun(ctx, componentRoot)
		return false
	}
	running, r := q.Transition(item, queue.StatusRunning)
	if r != nil {
		if !r.Moved {
			d.log.Infof("queue: the claim was refused: %s (item %d)", r.Reason, item.ID)
		}
		return false
	}
	if d.Table == nil || d.Table.StartBlueprintLoop == nil {
		d.blockUndauntedStart(ctx, q, running, "the daemon has no loop.start door wired")
		return false
	}
	res, err := d.Table.StartBlueprintLoop(ctx, protocol.LoopStartParams{
		Blueprint:     running.Blueprint,
		Goal:          running.Goal,
		WorkspaceRoot: running.WorkspaceRoot,
		Tech:          running.Tech,
		With:          running.With,
		Attended:      running.Attended, // carried UNCHANGED (D5) — declared or
		// never-declared, never inferred from the daemon starting it.
	})
	if err != nil {
		d.blockUndauntedStart(ctx, q, running, fmt.Sprintf("loop.start refused: %v", err))
		return false
	}
	m, ok := res.(map[string]any)
	if !ok {
		d.blockUndauntedStart(ctx, q, running, "loop.start answered with a shape this reactor cannot read")
		return false
	}
	slug, _ := m["slug"].(string)
	if slug == "" {
		d.blockUndauntedStart(ctx, q, running, "loop.start answered without a slug to point at")
		return false
	}
	stamped, err := d.store.SetQueueItemLoop(running.ID, running.ComponentRoot, slug)
	if err != nil || !stamped {
		// The loop EXISTS; un-stamping it would orphan a running loop —
		// log loudly, leave the item running (the D11 rule: the loop's
		// own rows are the truth now).
		d.log.Infof("queue: LOOP %s EXISTS but item %d never got its pointer (stamped=%v): %v — the loop is the truth now (D11)", slug, running.ID, stamped, err)
	}
	d.log.Infof("queue: item %d advanced into loop %s", running.ID, slug)
	return true
}

// blockUndauntedStart is the failed-claim aftermath named once: running →
// blocked (CG1's only honest edge out of a claim) plus the same pending
// question every halt earns.
func (d *Daemon) blockUndauntedStart(ctx context.Context, q *queue.Queue, item queue.QueueItem, why string) {
	if _, r := q.Transition(item, queue.StatusBlocked); r != nil {
		d.log.Infof("queue: a failed start could not even mark item %d blocked: %s", item.ID, r.Reason)
		return
	}
	d.raiseQueueQuestion(ctx, item.ComponentRoot, item.LoopSlug,
		fmt.Sprintf("queued item %q could not start — %s; the queue is halted until a person says otherwise", item.Goal, why))
}

// raiseQueueQuestion is T4's shared door (T3 calls it for every halt;
// T4's answered-branch completes the pair). The question rides the
// LOOP's slug — design §2's FK finding: `question.loop_slug` is NOT NULL
// with an FK to `loop`, and the just-finished loop's row still exists
// while a not-yet-started one's does not. An existing pending gate
// question for the same loop is left alone (the daemon restarted mid-
// halt — re-asking is noise, and the person already has one to answer).
func (d *Daemon) raiseQueueQuestion(ctx context.Context, componentRoot, loopSlug, body string) {
	if loopSlug == "" {
		// No loop to hang the question on — say so; the item's blocked
		// status stands as the observable halt.
		d.log.Infof("queue: a halt in %s had no loop to attach its question to", componentRoot)
		return
	}
	sc := store.Scope{ComponentRoot: componentRoot, LoopSlug: loopSlug}
	if existing, found, err := d.store.PendingQuestion(sc); err == nil && found && existing.Kind == string(store.QuestionGate) {
		return
	}
	if _, err := d.store.Ask(sc, store.NewQuestion{
		Kind:    store.QuestionGate,
		Body:    body,
		Options: []string{"advance", "hold"},
	}); err != nil {
		d.log.Infof("queue: the halt could not raise its question on %s/%s: %v", componentRoot, loopSlug, err)
	}
}

func blockedQuestionBody(item queue.QueueItem, reason string) string {
	if reason == "" {
		reason = "the loop reported itself blocked"
	}
	return fmt.Sprintf("queued item %q is blocked — %s; the queue is halted until a person says otherwise (cancel the item to let the next one run)", item.Goal, reason)
}

// queueItemFromRow re-does the store-row conversion here because the
// package's own converter is unexported and the reactor reads rows
// directly for the lookup fast path.
func queueItemFromRow(r store.QueueItemRow) queue.QueueItem {
	it, err := queue.ItemFromRow(r)
	if err != nil {
		// A corrupt row converts to the zero item, whose status is not one
		// of the seven — every Transition from it refuses naming the
		// status, which is the honest outcome for a row this broken.
		return queue.QueueItem{}
	}
	return it
}
