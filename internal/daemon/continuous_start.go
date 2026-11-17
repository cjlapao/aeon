package daemon

// continuous_start.go — CN14 made code: the decision row, the claim, the
// claimed-FROM transition, the one start through the ONE start door, the
// stamps, the outcome. The whole protocol lives on the adapter side of
// the seam because the ledger it writes IS the daemon's: the pure core
// only ever said "start this". The order is the law — claim BEFORE any
// transition or start (two starts never share an item), and a refusal is
// FINAL: no retry, no second door, no side door.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

// schedulerActor is who the decision log sees: the machine that authored
// the line — never an author of authority (the store's own words).
// continuousCrashHook is the ONLY window into the start seam, and it
// belongs to the tests (S27 T11): when non-nil it is called at two stages —
// "claimed" immediately after the claim row became ours and before any loop
// row exists, "started" after the loop exists and before its stamps land.
// Returning true abandons the rest of the start exactly as a killed
// process would: the claim stays open, the stamps never land, and what the
// next Reconcile sees is precisely what the crash left. Production never
// sets it — it cannot: the variable is package-private and
// TestContinuousCrashHookIsTestOnly walks every non-test file of this
// package to prove the declaration is the only non-test mention.
var continuousCrashHook func(d *Daemon, stage string) bool

// continuousCrashed asks the hook, honestly, whether this start ends here.
// The daemon rides along so a test can arm the hook for its own daemon
// only — e2e tests run in parallel, each with its own store and worker,
// and a hook that could not tell one daemon's start from another's would
// crash strangers.
func continuousCrashed(d *Daemon, stage string) bool {
	return continuousCrashHook != nil && continuousCrashHook(d, stage)
}

var schedulerActor = struct{ kind, id string }{"scheduler", "continuous"}

// Record appends one decision row per plan line and answers its id — the
// id the claim must name (a claim without a decision is refused by the
// store before SQL). The summary is the CN15 sentence: ids, enums,
// instants — no user text (SEC-033). A start verdict's row additionally
// carries the grant revision the pin WILL carry (resolved read-only here;
// the pin itself is resolved again at the door, and the door's answer
// governs — a revision that moved between the two reads is honest drift,
// not a second opinion).
func (a *continuousAdapter) Record(ctx context.Context, s continuous.Snapshot, d continuous.Decision) (int64, error) {
	word := map[continuous.Verdict]string{
		continuous.VerdictStart:  "started",
		continuous.VerdictDefer:  "deferred",
		continuous.VerdictSkip:   "skipped",
		continuous.VerdictRefuse: "refused",
	}[d.Verdict]
	kind, subjectID := decisionSubject(d.Subject)
	summary := continuous.Sentence(d.Reason, d.Detail)
	if d.Verdict == continuous.VerdictStart {
		summary = fmt.Sprintf("the tick admitted %s %d into %d weight against %d of max_slots %d — the claim names this decision",
			kind, subjectID, d.Weight, s.Slots.Used, s.Settings.MaxSlots)
	}
	rev := ""
	if d.Verdict == continuous.VerdictStart {
		// CN16's one-clause answer: the grant revision pinned, the item's
		// declared cap, and the effective permission left after the cap
		// narrowed the grant. The clause rides the started row so "which
		// rule allowed this?" stays one read.
		pin, perr := a.d.resolveAutonomy(ctx, autonomy.OriginScheduler, d.Root, nil)
		if perr == nil {
			cap := "grant"
			if c, err := a.itemCap(kind, subjectID); err == nil {
				cap = c
			}
			_, outward, merge := narrowPin(pin, cap)
			if pin != nil {
				rev = strconv.FormatInt(pin.Rev, 10)
			}
			summary += " — " + grantClause(pinRev(pin), pin != nil, cap, outward, merge)
		}
	}
	id, err := a.d.store.AppendDecision(store.SchedulerDecision{
		ComponentRoot: d.Root, SubjectKind: kind, SubjectID: subjectID,
		Decision: word, Reason: string(d.Reason), Summary: summary,
		GrantRevision: rev, ActorKind: schedulerActor.kind, ActorID: schedulerActor.id,
	})
	if err == nil && d.Verdict == continuous.VerdictRefuse &&
		d.Reason == continuous.ReasonTemplateInvalid && kind == "roadmap" {
		// CN13: the refusal row is half a sentence — the roadmap-sourced
		// unit also files one needs-you line (design: "refused:
		// template-invalid, item untouched, needs-you"). The store's
		// never-retried dedupe keeps every further tick AND every restart
		// from rewriting it (T11 scenario 7); a queue-sourced refusal
		// files nothing, because S24's own needs-you path is still doing
		// that job (the law: roadmap-sourced blocks and refusals are the
		// scheduler's to announce).
		if _, nerr := a.d.store.AppendDecision(store.SchedulerDecision{
			ComponentRoot: d.Root, SubjectKind: kind, SubjectID: subjectID,
			Decision: "needs-you", Reason: string(d.Reason), Summary: refusalNeedsYouSummary,
			ActorKind: schedulerActor.kind, ActorID: schedulerActor.id,
		}); nerr != nil {
			a.d.log.Infof("continuous: the needs-you line for roadmap %d's template refusal could not be written: %v", subjectID, nerr)
		}
	}
	return id, err
}

// decisionSubject maps the core's subject kinds onto the store's: queue
// and roadmap carry their ids; repository-level and loop-level rows speak
// for the ROOT (subject kind "root", id 0 — the root names itself).
// pinRev answers the pinned revision or zero for no pin at all.
func pinRev(pin *store.AutonomyPin) int64 {
	if pin == nil {
		return 0
	}
	return pin.Rev
}

func decisionSubject(sub continuous.Subject) (string, int64) {
	switch sub.Kind {
	case "queue", "roadmap":
		id, _ := strconv.ParseInt(sub.ID, 10, 64)
		return sub.Kind, id
	default:
		return "root", 0
	}
}

// Start performs the claim-then-start for one start decision, serialized
// per repository: the occupancy re-check, the claim, and the claimed-FROM
// transition all happen under one lock, so two ticks (or a tick and a
// reconcile) cannot each believe they own a repository's next loop.
func (a *continuousAdapter) Start(ctx context.Context, d continuous.Decision, s continuous.Snapshot, decisionID int64) (string, error) {
	kind, subjectID := decisionSubject(d.Subject)
	if kind != "queue" && kind != "roadmap" {
		return "", fmt.Errorf("continuous: a start decision about a %s starts nothing", d.Subject.Kind)
	}
	lock := a.rootLock(d.Root)
	lock.Lock()
	defer lock.Unlock()

	// The world may have moved since the snapshot; the facts a START still
	// needs are re-read, and each move that invalidates the pick defers
	// honestly (a deferred row for the log) rather than acting on stale
	// permission.
	st, err := a.d.store.SchedulerState()
	if err != nil {
		return "", err
	}
	if !st.Enabled {
		a.deferRow(d, "master-off")
		return "", fmt.Errorf("the continuous scheduler is off")
	}
	if st.Paused {
		a.deferRow(d, "paused")
		return "", fmt.Errorf("the continuous scheduler is paused")
	}
	rootRow, ok := a.rootRow(d.Root)
	if !ok || !rootRow.Enabled {
		a.deferRow(d, "repo-off")
		return "", fmt.Errorf("the repository is not enabled for the continuous scheduler")
	}
	if occ, err := a.d.store.OccupyingLoops(d.Root); err == nil {
		for _, o := range occ {
			if trigger.StillGoing(o.Status) {
				a.deferRowDetail(d, continuous.ReasonRepoOccupied, fmt.Sprintf("%s (state %s)", o.Slug, o.Status))
				return "", fmt.Errorf("the repository is occupied by loop %s", o.Slug)
			}
		}
	}

	// (1) THE CLAIM — first, before any row moves. False means another
	// claim row already exists for this subject: say already-claimed, stop.
	claimed, err := a.d.store.ClaimStart(kind, subjectID, d.Root, decisionID, atStamp(s.Now))
	if err != nil {
		return "", err
	}
	if !claimed {
		a.refuseRow(d, "already-claimed", continuous.Sentence(continuous.ReasonAlreadyClaimed, ""))
		return "", fmt.Errorf("item %d was already claimed", subjectID)
	}
	if continuousCrashed(a.d, "claimed") {
		// The kill-mid-pick shape: the claim row is the only thing the
		// dead process left. Nothing else moved — no transition, no loop,
		// no stamp — and Reconcile owns the restart.
		return "", fmt.Errorf("continuous: the process ended after claiming %s %d", kind, subjectID)
	}

	// (2)+ The claimed-FROM transition, the start, the stamps, the outcome.
	return a.runClaimedStart(ctx, kind, subjectID, d.Root, rootRow, decisionID)
}

// runClaimedStart is everything after the claim is ours — shared with
// Reconcile, which arrives holding an open claim the previous process
// never finished. Every exit names its outcome in scheduler_start exactly
// once (the store guarantees the exactly-once; this function only chooses
// the word).
func (a *continuousAdapter) runClaimedStart(ctx context.Context, kind string, subjectID int64, root string, rootRow store.SchedulerRootRow, decisionID int64) (string, error) {
	q := queue.New(a.d.store)
	r := roadmap.New(a.d.store)
	var (
		p     protocol.LoopStartParams
		qitem queue.QueueItem
	)

	switch kind {
	case "queue":
		row, ok, err := a.d.store.QueueItemByID(subjectID)
		if err != nil {
			a.finishLost(kind, subjectID, "the queue row could not be read")
			return "", err
		}
		if !ok {
			a.finishLost(kind, subjectID, "the queue row is gone")
			return "", fmt.Errorf("queue item %d does not exist", subjectID)
		}
		qitem = queueItemFromRow(row)
		switch row.Status {
		case "ready":
			if fresh, ref := q.Transition(qitem, queue.StatusRunning); ref != nil {
				a.finishLost(kind, subjectID, ref.Reason)
				return "", fmt.Errorf("the claimed-FROM transition refused: %s", ref.Reason)
			} else {
				qitem = fresh
			}
		case "running":
			// The crash shape: claimed and transitioned, died before the
			// stamp. Re-running the transition would refuse (CG1 has no
			// running→running edge); the item stays as it is.
		default:
			a.finishLost(kind, subjectID, "the item moved out of the claimed-from state")
			return "", fmt.Errorf("queue item %d is %s at the claim", subjectID, row.Status)
		}
		if _, err := a.d.loadStartBlueprint(row.Blueprint); err != nil {
			a.refuseQueueStart(q, qitem, kind, subjectID, root, fmt.Sprintf("the item's blueprint %q no longer loads: %v", row.Blueprint, err))
			return "", err
		}
		ws := row.WorkspaceRoot
		if ws == "" {
			ws = root
		}
		var tech string
		if row.Tech != nil {
			tech = *row.Tech
		}
		p = protocol.LoopStartParams{
			Blueprint: row.Blueprint, Goal: row.Goal, WorkspaceRoot: ws,
			Tech: tech, With: decodeWithJSON(derefOrEmpty(row.WithJSON)),
			Slug: fmt.Sprintf("queue-item-%d", subjectID), Attended: attendedByNobody(),
		}

	case "roadmap":
		it, ok, err := r.Show(subjectID)
		if err != nil {
			a.finishLost(kind, subjectID, "the roadmap row could not be read")
			return "", err
		}
		if !ok {
			a.finishLost(kind, subjectID, "the roadmap row is gone")
			return "", fmt.Errorf("roadmap item %d does not exist", subjectID)
		}
		if it.Status == roadmap.StatusReady {
			deps, derr := r.Dependencies(subjectID)
			if derr == nil {
				if ready, why := roadmap.IsReadyWithDeps(it, deps); !ready {
					a.finishLost(kind, subjectID, why)
					return "", fmt.Errorf("item %d is not ready at the claim: %s", subjectID, why)
				}
			}
			if _, ref := r.TransitionAudited(subjectID, roadmap.StatusRunning, "materialize", roadmap.SystemReactor); ref != nil {
				a.finishLost(kind, subjectID, ref.Reason)
				return "", fmt.Errorf("the claimed-FROM transition refused: %s", ref.Reason)
			}
		} else if it.Status != roadmap.StatusRunning {
			a.finishLost(kind, subjectID, "the item moved out of the claimed-from state")
			return "", fmt.Errorf("roadmap item %d is %q at the claim", subjectID, it.Status)
		}
		if rootRow.Blueprint == "" {
			a.refuseRoadmapStart(kind, subjectID, root, "the repository's work template is unset — scheduler.root set names one")
			return "", fmt.Errorf("roadmap item %d: the work template is unset", subjectID)
		}
		if _, err := a.d.loadStartBlueprint(rootRow.Blueprint); err != nil {
			a.refuseRoadmapStart(kind, subjectID, root, fmt.Sprintf("the repository's work template no longer loads: %v", err))
			return "", fmt.Errorf("roadmap item %d: the work template no longer loads", subjectID)
		}
		with := decodeWithJSON(rootRow.WithJSON)
		for k, v := range roadmapLink(subjectID, "materialization") {
			with[k] = v
		}
		if it.BriefPath != "" {
			with["brief_path"] = it.BriefPath
		}
		if it.PlanPath != "" {
			with["plan_path"] = it.PlanPath
		}
		p = protocol.LoopStartParams{
			Blueprint: rootRow.Blueprint, Goal: it.Title, WorkspaceRoot: root,
			Tech: rootRow.Tech, With: with,
			Slug: fmt.Sprintf("roadmap-item-%d", subjectID), Attended: attendedByNobody(),
		}
	}

	// AL8 at the scheduler's door: the grant is consulted once, at pin
	// time. A store failure refuses (fail-toward-deny); no applicable
	// grant pins nothing — the loop is local-only, exactly the shape a
	// trigger fire without grants gets (outward stays the loop's own
	// opt-in: allow_outward false, merge false).
	pin, err := a.d.resolveAutonomy(ctx, autonomy.OriginScheduler, root, nil)
	if err != nil {
		a.refuseStartNoGate(kind, q, qitem, subjectID, root, err.Error())
		return "", err
	}
	// CN3: the item's cap narrows the pin and can only narrow it. cap
	// "none" pins nothing at all; cap "outward" drops the irreversible
	// entries. The legacy flags never move (S56 AL8) — the pin is the
	// permission carrier, so an unreadable cap falls to "grant" (the pin
	// as given), never to a guess, and the pin's own live re-check at
	// execution is the floor either way.
	itemCap := "grant"
	if c, err := a.itemCap(kind, subjectID); err == nil {
		itemCap = c
	} else {
		a.d.log.Infof("continuous: the cap of %s %d could not be read: %v", kind, subjectID, err)
	}
	pin, _, _ = narrowPin(pin, itemCap)
	// CN17's provenance, authored here: the start names the scheduler as
	// its author and the DECISION as its trigger — the decision id is the
	// audit's join key, the reason a loop's history can be read back to
	// the tick that made it.
	res, err := a.d.startLoopWith(ctx, p, startExtras{
		StartedBy: "scheduler", TriggerID: strconv.FormatInt(decisionID, 10), Depth: 0, Autonomy: pin,
	})
	if err != nil {
		a.refuseStartNoGate(kind, q, qitem, subjectID, root, err.Error())
		return "", err
	}
	slug := expectedSlug(kind, subjectID)
	if got, _ := res["slug"].(string); got != "" {
		slug = got
	}
	if continuousCrashed(a.d, "started") {
		// The kill-after-the-loop shape: the loop is the truth (D11) and
		// exists with its deterministic slug; the claim is open and the
		// pointers were never written. Reconcile's job is exactly to
		// stamp — never to start a second loop.
		return "", fmt.Errorf("continuous: the process ended after starting loop %s", slug)
	}

	// (3) The stamps. A failed pointer never unwinds a live loop — the
	// loop is the truth (D11); the loud line below is what reconcile and
	// a person read.
	if _, err := a.d.store.StampStart(kind, subjectID, root, slug); err != nil {
		a.d.log.Infof("continuous: LOOP %s EXISTS but claim %s/%d never got its stamp: %v — the loop is the truth now", slug, kind, subjectID, err)
	}
	switch kind {
	case "queue":
		if ok, err := a.d.store.SetQueueItemLoop(subjectID, root, slug); err != nil || !ok {
			a.d.log.Infof("continuous: LOOP %s EXISTS but queue item %d never got its pointer: %v — the loop is the truth now", slug, subjectID, err)
		}
	case "roadmap":
		if _, ref := r.RecordLoop(subjectID, root, slug); ref != nil {
			a.d.log.Infof("continuous: LOOP %s EXISTS but roadmap item %d never got its pointer: %s — the loop is the truth now", slug, subjectID, ref.Reason)
		}
	}
	if _, err := a.d.store.FinishStart(kind, subjectID, "started"); err != nil {
		a.d.log.Infof("continuous: the started outcome could not close claim %s/%d: %v", kind, subjectID, err)
	}
	a.d.log.Infof("continuous: %s %d started loop %s under the scheduler origin", kind, subjectID, slug)
	return slug, nil
}

// Reconcile walks the open claims once (CN14): a claim whose deterministic
// slug EXISTS means the start beat the crash — stamp, point, close as
// started. One that does not exists gets its ONE retry (the claim is
// already held, so runClaimedStart continues where the dead process
// stopped). Refusals close refused; nothing re-claims, nothing re-decides
// the tick's pick.
func (a *continuousAdapter) Reconcile(ctx context.Context) error {
	rows, err := a.d.store.OpenStarts()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	a.d.log.Infof("continuous: reconcile opens with %d undecided claim(s)", len(rows))
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.startMu.Lock()
		lock := a.locks[row.ComponentRoot]
		a.startMu.Unlock()
		if lock != nil {
			lock.Lock()
		}
		err := a.reconcileOne(ctx, row)
		if lock != nil {
			lock.Unlock()
		}
		if err != nil {
			a.d.log.Infof("continuous: reconcile left claim %s/%d for a person: %v", row.SubjectKind, row.SubjectID, err)
		}
	}
	return nil
}

func (a *continuousAdapter) reconcileOne(ctx context.Context, row store.SchedulerStartRow) error {
	st, err := a.d.store.SchedulerState()
	if err != nil {
		return err
	}
	if !st.Enabled || st.Paused {
		return fmt.Errorf("the master switch is %s — an open claim under a silent scheduler waits for the person who silenced it",
			map[bool]string{true: "paused", false: "off"}[!st.Enabled])
	}
	rootRow, ok := a.rootRow(row.ComponentRoot)
	if !ok || !rootRow.Enabled {
		return fmt.Errorf("the repository is not enabled — the claim stays open for the person who disabled it")
	}
	slug := row.LoopSlug
	if slug == "" {
		slug = expectedSlug(row.SubjectKind, row.SubjectID)
	}
	loops, err := a.d.store.LoopsBySlug(slug)
	if err != nil {
		return err
	}
	exists := false
	for _, l := range loops {
		if l.ComponentRoot == row.ComponentRoot {
			exists = true
		}
	}
	if exists {
		// The loop won; only the records lag it.
		if row.LoopSlug == "" {
			if _, err := a.d.store.StampStart(row.SubjectKind, row.SubjectID, row.ComponentRoot, slug); err != nil {
				a.d.log.Infof("continuous: reconcile could not stamp claim %s/%d: %v", row.SubjectKind, row.SubjectID, err)
			}
		}
		a.pointItemAtLoop(row.SubjectKind, row.SubjectID, row.ComponentRoot, slug)
		if _, err := a.d.store.FinishStart(row.SubjectKind, row.SubjectID, "started"); err != nil {
			return err
		}
		a.d.log.Infof("continuous: reconcile closed claim %s/%d as started — loop %s is the truth", row.SubjectKind, row.SubjectID, slug)
		return nil
	}
	// The loop never came to be: the ONE retry, under the claim we hold.
	a.d.log.Infof("continuous: reconcile retries claim %s/%d once — loop %s does not exist", row.SubjectKind, row.SubjectID, slug)
	if _, err := a.runClaimedStart(ctx, row.SubjectKind, row.SubjectID, row.ComponentRoot, rootRow, row.DecisionID); err != nil {
		return err
	}
	return nil
}

// pointItemAtLoop re-does a pointer the crash lost; already-pointed items
// keep their pointer (SetQueueItemLoop is an idempotent write by
// id+root; RecordLoop answers a refusal we only log).
func (a *continuousAdapter) pointItemAtLoop(kind string, subjectID int64, root, slug string) {
	switch kind {
	case "queue":
		if row, ok, err := a.d.store.QueueItemByID(subjectID); err == nil && ok && row.ID != 0 && (row.LoopSlug == nil || *row.LoopSlug == "") {
			if _, err := a.d.store.SetQueueItemLoop(subjectID, root, slug); err != nil {
				a.d.log.Infof("continuous: reconcile could not point queue item %d at loop %s: %v", subjectID, slug, err)
			}
		}
	case "roadmap":
		r := roadmap.New(a.d.store)
		if it, ok, err := r.Show(subjectID); err == nil && ok && it.LoopSlug == "" {
			if _, ref := r.RecordLoop(subjectID, root, slug); ref != nil {
				a.d.log.Infof("continuous: reconcile could not point roadmap item %d at loop %s: %s", subjectID, slug, ref.Reason)
			}
		}
	}
}

// --- outcomes and ledger lines ------------------------------------------------

func (a *continuousAdapter) finishLost(kind string, subjectID int64, why string) {
	if _, err := a.d.store.FinishStart(kind, subjectID, "lost"); err != nil {
		a.d.log.Infof("continuous: the lost outcome could not close claim %s/%d: %v", kind, subjectID, err)
	}
	a.ledgerRow("", kind, subjectID, "lost", "transition-lost", "the claimed-FROM transition refused: "+why)
}

// ledgerRow writes one audit line outside the Worker's plan loop (the
// claim protocol's own moves — lost, refused, the re-check defers — are
// the adapter's news, not a decision the plan made).
func (a *continuousAdapter) ledgerRow(root, kind string, subjectID int64, word, reason, summary string) {
	if _, err := a.d.store.AppendDecision(store.SchedulerDecision{
		ComponentRoot: root, SubjectKind: kind, SubjectID: subjectID, Decision: word, Reason: reason,
		Summary: summary, ActorKind: schedulerActor.kind, ActorID: schedulerActor.id,
	}); err != nil {
		a.d.log.Infof("continuous: the %s line for %s %d could not be written: %v", word, kind, subjectID, err)
	}
}

func (a *continuousAdapter) refuseRow(d continuous.Decision, reason string, summary string) {
	kind, subjectID := decisionSubject(d.Subject)
	a.ledgerRow(d.Root, kind, subjectID, "refused", reason, summary)
}

func (a *continuousAdapter) deferRow(d continuous.Decision, reason string) {
	kind, subjectID := decisionSubject(d.Subject)
	a.ledgerRow(d.Root, kind, subjectID, "deferred", reason, continuous.Sentence(continuous.Reason(reason), ""))
}

func (a *continuousAdapter) deferRowDetail(d continuous.Decision, reason continuous.Reason, detail string) {
	kind, subjectID := decisionSubject(d.Subject)
	a.ledgerRow(d.Root, kind, subjectID, "deferred", string(reason), continuous.Sentence(reason, detail))
}

// refuseStartNoGate is the FINAL refusal (CN14: never retried): the claim
// closes refused and the subject's row carries the human's word — a queue
// item blocks with its question (blockUndauntedStart, the same aftermath
// advanceNext writes), a roadmap item answers needs-you, because for the
// roadmap it IS a person's move that is missing, not the queue's halt.
func (a *continuousAdapter) refuseStartNoGate(kind string, q *queue.Queue, qitem queue.QueueItem, subjectID int64, root, why string) {
	if _, err := a.d.store.FinishStart(kind, subjectID, "refused"); err != nil {
		a.d.log.Infof("continuous: the refused outcome could not close claim %s/%d: %v", kind, subjectID, err)
	}
	a.refuseDoorSide(kind, q, qitem, subjectID, root, why)
}

func (a *continuousAdapter) refuseRoadmapStart(kind string, subjectID int64, root, why string) {
	if _, err := a.d.store.FinishStart(kind, subjectID, "refused"); err != nil {
		a.d.log.Infof("continuous: the refused outcome could not close claim %s/%d: %v", kind, subjectID, err)
	}
	a.refuseDoorSide(kind, nil, queue.QueueItem{}, subjectID, root, why)
}

func (a *continuousAdapter) refuseQueueStart(q *queue.Queue, qitem queue.QueueItem, kind string, subjectID int64, root, why string) {
	if _, err := a.d.store.FinishStart(kind, subjectID, "refused"); err != nil {
		a.d.log.Infof("continuous: the refused outcome could not close claim %s/%d: %v", kind, subjectID, err)
	}
	a.refuseDoorSide(kind, q, qitem, subjectID, root, why)
}

func (a *continuousAdapter) refuseDoorSide(kind string, q *queue.Queue, qitem queue.QueueItem, subjectID int64, root, why string) {
	switch kind {
	case "queue":
		a.ledgerRow(root, kind, subjectID, "refused", "start-refused", why)
		a.d.blockUndauntedStart(context.WithoutCancel(context.Background()), q, qitem, why)
	case "roadmap":
		// needs-you deduped: the closed claim means no second attempt can
		// land here for the same subject, and the store's LAST word check
		// keeps even a reconcile-and-live-tick overlap from doubling it.
		if last, found, err := a.d.store.LastDecision(kind, subjectID); err == nil && found && last.Decision == "needs-you" {
			return
		}
		if _, err := a.d.store.AppendDecision(store.SchedulerDecision{
			ComponentRoot: root, SubjectKind: kind, SubjectID: subjectID,
			Decision: "needs-you", Reason: "start-refused", Summary: why,
			ActorKind: schedulerActor.kind, ActorID: schedulerActor.id,
		}); err != nil {
			a.d.log.Infof("continuous: the needs-you line for roadmap %d could not be written: %v", subjectID, err)
		}
		a.d.log.Infof("continuous: roadmap item %d needs a person — %s", subjectID, why)
	}
}

// --- shapes the door needs ----------------------------------------------------

// attendedByNobody authors the fact the start path OWNS (D1's law, from
// trigger_fire): a scheduler-started loop has nobody at the keyboard by
// definition, and the pin is a hard false, never an inference from the
// item row. Outward and merge stay the loop's opt-ins: absent means false.
func attendedByNobody() *bool {
	attended := false
	return &attended
}

// expectedSlug is the deterministic name the start carries (CN14's
// recovery anchor: reconcile finds the loop by NAME even when no record
// survived).
func expectedSlug(kind string, subjectID int64) string {
	return fmt.Sprintf("%s-item-%d", kind, subjectID)
}

func atStamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

func decodeWithJSON(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out) // a corrupt with_json decodes to empty — the start carries no link; the queue's own words call this an ordinary item
	return out
}
