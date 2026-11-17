package daemon

// S60 T7 (MQ2, MQ5, MQ10, MQ18): the merge-queue reactor — the F19 shape.
// One goroutine decides for every open merge_queue_item: gather the facts
// (config, loop states, staleness, gate/gear/rule reads — READS, never the
// reactor running git merges or landing PRs itself), ask
// mergequeue.Next(item, facts), and apply the Decision: the store's
// guarded transition, the loop start the decision named, the conflict
// question the policy asks for. Every effect is idempotent or guarded, so
// a wake-on-event, the 30s tick, a restart-reconcile and a person racing
// the door are all safe: the CAS on the item row is the arbiter.
//
// The reactor NEVER advances on unknown facts: pointer facts stay nil and
// Next holds. The one deliberate exception is honest degradation — a ready
// item whose landing PR cannot be read holds here (no Decide at all),
// because the alternative is answering HeadSHAMatches=false and ending a
// good item `head-moved` on a network outage.
//
// SEC-033: everything gathered and transitioned is ids, counts, enums and
// SHAs. Conflicted paths are COUNTED (the cap below); hunks never leave
// the worktree and never reach this file's outputs.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/workspace"
)

// mergeQueueReactorTick is the named-constant backstop under the event
// wakes (spec T7: "a periodic tick, default 30 s").
const mergeQueueReactorTick = 30 * time.Second

// mqConflictPathCap bounds how many conflicted PATHS the reactor reads at
// all. The count past the cap is still exact (len(paths) is); the cap is
// what keeps even the path-list itself a bounded fact (SEC-033).
const mqConflictPathCap = 100

// mqMergeActor is the reactor's identity on every queue write: a daemon
// actor, never a person's (the person's act is the door, the ask answer,
// or the land request — each of which stamps ITS OWN actor).
func mqMergeActor() store.MergeActor {
	return store.MergeActor{Kind: "daemon", ID: "merge-queue-reactor"}
}

// mqWorld is one item's gathered facts plus the raw reads the APPLY step
// needs (the SHA to record on merged, the paths to record on conflicted).
type mqWorld struct {
	f              mergequeue.Facts
	landing        *loop.LoopState // nil: no landing loop read
	resolve        *loop.LoopState
	landingStopped bool   // the landing loop is stopped/aborted (behaviour 10)
	integrationSHA string // the built integration SHA (facts + ready binding)
	mergeSHA       string // the merge SHA the forge or evidence reports
	conflictPaths  []string
	staleConflict  *loop.LoopState // blocked AS a conflict; index says none left
	degraded       bool            // a fact that must not advance the item is unknown
	degradeNote    string
}

// initMergeQueueReactor mounts the reactor on the daemon's lifetime (serve
// wires it after the terminals reaper, with the same stop context). The
// storeless daemon (tests, tools) has nothing to reconcile: absent.
func (d *Daemon) initMergeQueueReactor(ctx context.Context) {
	if d.store == nil {
		return
	}
	d.mqLand = map[string]bool{}
	d.mqNotify = make(chan struct{}, 1)
	go d.runMergeQueueReactor(ctx)
}

// mergeQueueWake pokes the reactor without blocking the caller — the
// Notify() the service doors call after enqueue/dequeue/reorder/settings/
// retry (spec: "reacts to ... a Notify() called by the service"). Coales-
// cing (buffered size 1 + default) is the point: ten enqueues are one pass.
func (d *Daemon) mergeQueueWake() {
	if d.mqNotify == nil {
		return
	}
	select {
	case d.mqNotify <- struct{}{}:
	default:
	}
}

// runMergeQueueReactor is the goroutine: reconcile at once (the restart
// pass — behaviour 9's "re-reads config, queue rows, loop states"), then
// wakes and ticks until the serve context ends.
func (d *Daemon) runMergeQueueReactor(ctx context.Context) {
	d.reconcileMergeQueue(ctx)
	t := time.NewTicker(mergeQueueReactorTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.mqNotify:
			d.reconcileMergeQueue(ctx)
		case <-t.C:
			d.reconcileMergeQueue(ctx)
		}
	}
}

// onMergeQueueTerminal is the blueprint hook's merge-queue arm, called for
// every loop.finished/loop.blocked: if the finished/blocked loop is one
// THIS queue minted (a plain-text join on landing_loop_slug/
// resolve_loop_slug — F20), wake the reactor. The event carries nothing
// the reactor would trust anyway (states are re-read from the pin); all
// it earns is immediacy.
func (d *Daemon) onMergeQueueTerminal(_ string, root, slug string, _ map[string]any) {
	if d.store == nil || root == "" || slug == "" {
		return
	}
	items, err := d.store.ListOpenItemsByLoop(root, slug)
	if err != nil {
		d.logf("merge-queue: terminal join failed for %s: %v", slug, err)
		return
	}
	if len(items) > 0 {
		d.mergeQueueWake()
	}
}

// reconcileMergeQueue is one full pass over every open item (ordered by
// root, base, position — so decisions happen in queue order). One
// goroutine, items in order: decisions for one base serialise naturally,
// and every write is the store's guarded CAS besides (spec: "a second
// reactor instance or a person racing is safe").
func (d *Daemon) reconcileMergeQueue(ctx context.Context) {
	if d.store == nil {
		return
	}
	items, err := d.store.ListItemsNeedingReconcile()
	if err != nil {
		d.logf("merge-queue: reconcile listing failed: %v", err)
		return
	}
	svc := mergequeue.NewService(d.store)
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		d.reconcileMergeItem(ctx, svc, item)
	}
	d.mqReapOrphanLoops(ctx)
}

// mqConfig reads the repository's queue settings; an absent row (or an
// unreadable one) is DefaultConfig — dark (MQ4). The reactor cannot make
// a repository light by imagination.
func (d *Daemon) mqConfig(ctx context.Context, root string) mergequeue.Config {
	cfg, err := d.store.ReadMergeConfig(root)
	if err != nil || cfg.Absent {
		return mergequeue.DefaultConfig()
	}
	return mergequeue.ConfigOf(cfg)
}

// reconcileMergeItem decides one open item.
func (d *Daemon) reconcileMergeItem(ctx context.Context, svc *mergequeue.Service, item store.MergeItem) {
	cfg := d.mqConfig(ctx, item.ComponentRoot)
	group, err := d.store.ListMergeItems(store.MergeFilter{
		ComponentRoot: item.ComponentRoot, BaseBranch: item.BaseBranch,
		Statuses: store.MergeOpenStatusSet,
	})
	if err != nil {
		d.logf("merge-queue: item %s: base group unreadable: %v", item.ID, err)
		return
	}
	w := d.gatherMergeFacts(ctx, item, group, cfg)

	// The landing loop's forge.openLandingPR evidence owns the row's
	// PR number: the action recorded it, the queue copies it onto the
	// item once (the ReadPR facts — behaviour 9's legs, the head-SHA
	// binding — read the row, not the loop).
	if item.PRNumber == nil && w.landing != nil {
		for _, ev := range w.landing.Evidence {
			v, ok := ev["pr_number"]
			if !ok {
				continue
			}
			n, cerr := strconv.Atoi(v)
			if cerr != nil || n <= 0 {
				continue
			}
			up, serr := d.store.SetMergeItemFields(item.ID, item.Status,
				store.MergeFields{PRNumber: &n}, mqMergeActor())
			if serr != nil {
				d.logf("merge-queue: item %s: stamping the PR number refused: %v", item.ID, serr)
			} else {
				item = up
			}
			break
		}
	}

	if w.degraded {
		// A fact that could only guess is said and HELD (never advanced
		// on unknown — MQ10): the item keeps its status, the note is the
		// log's, and the next tick retries.
		d.logf("merge-queue: item %s holds: %s", item.ID, w.degradeNote)
		return
	}
	status := mergequeue.Status(item.Status)

	// The landing-race fix-up (behaviour 9's merged-but-unrecorded rung,
	// seen early): the forge says the PR MERGED while the item still reads
	// `ready` — the person's land went through the loop's own gate while
	// the queue had not been told. Move it to `merging` so Next's merging
	// arm runs the ancestor proof and lands it at `merged`; never back to
	// a state that would land it twice.
	if status == mergequeue.StatusReady && w.f.Landed {
		if _, err := svc.Transition(item.ID, mergequeue.StatusMerging, store.TransitionFacts{}, mqMergeActor(), item); err != nil {
			d.logf("merge-queue: item %s: landing-race fix-up refused: %v", item.ID, err)
			return
		}
		d.mergeQueueWake()
		return
	}

	// The stop contract (behaviour 10): a stopped or aborted landing loop
	// ENDS the item — an item nobody will drive again must not sit open
	// holding a slot. No integration was ever built ⇒ removed(loop-stopped)
	// (nothing to show for it); work exists ⇒ failed(loop-stopped). The
	// worktree/ledger cleanup rides the recovery pass (ReconcileLoops),
	// which settles integration rows for terminal items.
	if w.landingStopped && !(status == mergequeue.StatusMerging && w.f.Landed) {
		tf := store.TransitionFacts{Reason: string(mergequeue.ReasonLoopStopped)}
		to := mergequeue.StatusFailed
		if w.integrationSHA == "" && item.IntegrationSHA == "" {
			to = mergequeue.StatusRemoved
		}
		// MQ5's edges outrank the intent: failed is only reachable from
		// verifying, merging and conflicted. A stopped landing caught in
		// queued/catching_up/ready ENDS the item through removed — same
		// reason word, the row that cannot say failed says removed and
		// the event ledger keeps the truth. (Deviation recorded at T7.)
		if !mergequeue.Legal(status, to) {
			to = mergequeue.StatusRemoved
		}
		if _, err := svc.Transition(item.ID, to, tf, mqMergeActor(), item); err != nil {
			d.logf("merge-queue: item %s: stop-contract transition refused: %v", item.ID, err)
			return
		}
		d.mergeQueueWake()
		return
	}

	_, dec, err := svc.Decide(item.ID, w.f)
	if err != nil {
		d.logf("merge-queue: item %s: decision refused: %v", item.ID, err)
		return
	}
	d.applyMergeDecision(ctx, svc, item, cfg, dec, w)
}

// applyMergeDecision turns one Decision into the store write it names plus
// the effects (loop starts, questions, settles) it implies.
func (d *Daemon) applyMergeDecision(ctx context.Context, svc *mergequeue.Service, item store.MergeItem, cfg mergequeue.Config, dec mergequeue.Decision, w mqWorld) {
	status := mergequeue.Status(item.Status)

	// A person's standing land request is CONSUMED when ready was read
	// outside auto (the arm either used it or ended on it — leaving it
	// standing would land a head the person has since moved past).
	if status == mergequeue.StatusReady && cfg.Mode != mergequeue.ModeAuto {
		d.clearLandRequest(item.ID)
	}

	if w.staleConflict != nil && !mqDenyWord(*w.staleConflict) {
		d.mqRetireStaleLanding(ctx, item, *w.staleConflict)
	}

	if !dec.Advance {
		d.logf("merge-queue: item %s holds: %s", item.ID, dec.Sentence)
		if status == mergequeue.StatusConflicted {
			d.mqConflictDrive(ctx, item, cfg)
		}
		d.mqSelfHeal(ctx, item, w)
		return
	}

	tf := store.TransitionFacts{}
	switch {
	case (status == mergequeue.StatusReady || status == mergequeue.StatusMerging) && dec.To == mergequeue.StatusQueued:
		// The catch-up bump (behaviour 3) and the head-moved bounce share
		// the rung: a round spent is a round counted (MaxCatchUps).
		tf.BumpCatchUps = true
	}
	if dec.To == mergequeue.StatusCatchingUp {
		tf.LandingLoopSlug = d.mqNextLoopSlug(ctx, item.ComponentRoot, "land-"+item.ID, item.LandingLoopSlug, w.landing)
		tf.IntegrationBranch = d.mqIntegrationBranch(item)
	}
	if dec.To == mergequeue.StatusVerifying && w.integrationSHA != "" {
		tf.IntegrationSHA = w.integrationSHA
	}
	if dec.To == mergequeue.StatusReady && w.integrationSHA != "" {
		tf.VerifiedSHA = w.integrationSHA
	}
	if dec.To == mergequeue.StatusMerged && w.mergeSHA != "" {
		tf.LandedSHA = w.mergeSHA
	}
	if dec.To == mergequeue.StatusConflicted {
		tf.ConflictPaths = w.conflictPaths
		if cfg.ConflictPolicy == mergequeue.PolicyResolve {
			tf.ResolveLoopSlug = d.mqNextLoopSlug(ctx, item.ComponentRoot, "resolve-"+item.ID, item.ResolveLoopSlug, w.resolve)
		}
	}
	if dec.Reason != "" {
		tf.Reason = dec.Reason
	}

	if status == mergequeue.StatusReady && dec.To == mergequeue.StatusFailed {
		// The landing lane can be ATTEMPTED while the row still reads
		// ready: the engine's own door (auto mode, or a gate answer in
		// manual) runs the land step before the reactor's next pass
		// flips the row. A refusal or denial found THERE is a failed
		// LANDING, not a failed queue-wait — and the row's own legality
		// says a ready item never fell over without landing. Record the
		// attempt as merging, then let the terminal outcome ride from
		// where the attempt really stood.
		if _, hErr := svc.Transition(item.ID, mergequeue.StatusMerging,
			store.TransitionFacts{Reason: string(dec.Reason)}, mqMergeActor(), item); hErr != nil {
			d.logf("merge-queue: item %s: ready -> merging (landing attempt) refused: %v", item.ID, hErr)
			return
		}
		fresh, rErr := d.store.ReadMergeItem(item.ID)
		if rErr != nil {
			d.logf("merge-queue: item %s: reread after the merging hop: %v", item.ID, rErr)
			return
		}
		item = fresh
	}

	updated, err := svc.Transition(item.ID, dec.To, tf, mqMergeActor(), item)
	if err != nil {
		d.logf("merge-queue: item %s: %s -> %s refused: %v", item.ID, item.Status, dec.To, err)
		return
	}
	d.logf("merge-queue: item %s: %s -> %s (%s)", item.ID, item.Status, dec.To, dec.Sentence)

	switch dec.To {
	case mergequeue.StatusCatchingUp:
		d.ensureLandingLoop(ctx, updated)
	case mergequeue.StatusMerging:
		d.ensureLandingLoop(ctx, updated)
	case mergequeue.StatusConflicted:
		d.mqConflictDrive(ctx, updated, cfg)
	case mergequeue.StatusMerged, mergequeue.StatusFailed, mergequeue.StatusRemoved:
		d.mqSettleItemWorktree(ctx, updated)
	}
	d.mergeQueueWake()
}

// mqSelfHeal covers the crash rungs where the ROW says a loop should be
// driving and the pin says there is none (behaviour 9): a catching_up item
// whose landing loop vanished, or a merging item not yet landed with no
// loop left to land it. Both re-enter through the SAME start path (the
// action layer is crash-retryable: git.integrate's branch-already-merged
// rung, publishIntegration's pushed-sha rung, openLandingPR's PR-exists
// rung, forge.land's already-merged rung).
func (d *Daemon) mqSelfHeal(ctx context.Context, item store.MergeItem, w mqWorld) {
	switch mergequeue.Status(item.Status) {
	case mergequeue.StatusCatchingUp:
		if w.f.LandingLoop == mergequeue.LoopNone {
			d.ensureLandingLoop(ctx, d.mqHealSlug(ctx, item))
		}
	case mergequeue.StatusMerging:
		if w.f.LandingLoop == mergequeue.LoopNone && !w.f.Landed {
			d.ensureLandingLoop(ctx, d.mqHealSlug(ctx, item))
		}
	}
}

// mqHealSlug stamps the landing slug for an item whose crash rung died in
// the window BEFORE the slug was written — self-heal needs a name to
// start under, and the base slug is the only free one in that window.
// No slug to stamp (the common case) returns the item untouched.
func (d *Daemon) mqHealSlug(ctx context.Context, item store.MergeItem) store.MergeItem {
	if item.LandingLoopSlug != "" {
		return item
	}
	// The stamp is guarded on the row's CURRENT status, which this
	// round's decisions may already have moved — re-read the row rather
	// than trust the round's copy (a stamp refused on a stale guard used
	// to leave the row slugless, and every later round would mint yet
	// another loop slug: -2, -3, -4 of ghosts).
	fresh, err := d.store.ReadMergeItem(item.ID)
	if err != nil {
		d.logf("merge-queue: item %s: heal re-read failed: %v", item.ID, err)
		return item
	}
	if fresh.LandingLoopSlug != "" {
		return fresh
	}
	slug := d.mqNextLoopSlug(ctx, fresh.ComponentRoot, "land-"+fresh.ID, "", nil)
	if _, err := d.store.SetMergeItemFields(fresh.ID, fresh.Status, store.MergeFields{LandingLoopSlug: slug}, mqMergeActor()); err != nil {
		d.logf("merge-queue: item %s: healed landing slug %s not stamped: %v", item.ID, slug, err)
		return item
	}
	fresh.LandingLoopSlug = slug
	return fresh
}

// gatherMergeFacts reads everything Next may consult and NOTHING else.
// Every git/forge call here is a read: staleness counts, UnmergedPaths,
// ReadPR, IsAncestor — the reactor starts loops and reads facts; it never
// runs the integration or lands anything itself (the spec's Do-Not).
func (d *Daemon) gatherMergeFacts(ctx context.Context, item store.MergeItem, group []store.MergeItem, cfg mergequeue.Config) mqWorld {
	w := mqWorld{}
	status := mergequeue.Status(item.Status)
	root := item.ComponentRoot
	w.f.Config = cfg
	w.f.CatchUps = item.CatchUps

	// The landing slot: the first OTHER open item already inside the
	// landing half. Its own (item == holder) is passed as "" (spec of
	// Facts.SlotHolder).
	for _, g := range group {
		if g.ID == item.ID || !mqLandingSlotStatus(g.Status) {
			continue
		}
		w.f.SlotHolder = g.ID
		break
	}
	w.f.HeadOfBase = mqHeadOfBase(item, group)

	// The two loops, from the row's plain-text slugs (F20).
	if item.LandingLoopSlug != "" {
		if st, ok := d.mqLoopState(ctx, root, item.LandingLoopSlug); ok {
			w.landing = &st
			w.f.LandingLoop = mqLoopEnum(st)
			w.landingStopped = mqLoopKilled(st)
		}
	}
	if item.ResolveLoopSlug != "" {
		if st, ok := d.mqLoopState(ctx, root, item.ResolveLoopSlug); ok {
			w.resolve = &st
			w.f.ResolveLoop = mqLoopEnum(st)
		}
	}

	// The built integration SHA: fresh evidence while catching_up (a
	// resumed loop has not necessarily re-integrated yet — the ROW's sha
	// from a previous round is NOT proof for this one), the row's fact
	// once past it, and the resolution loop's conclude as the last
	// resort (the resolve half proved the merge clean).
	sha := ""
	if st := w.landing; st != nil {
		sha = st.Evidence["catch-up"]["integration_sha"]
	}
	if sha == "" && status != mergequeue.StatusCatchingUp {
		sha = item.IntegrationSHA
	}
	if sha == "" && w.f.ResolveLoop == mergequeue.LoopDone {
		sha = w.resolve.Evidence["conclude"]["integration_sha"]
	}
	w.integrationSHA = sha
	w.f.IntegrationSHA = sha

	// The conflict fact, while catching_up: the loop blocked at catch-up
	// is the SIGNAL; the worktree read answers the COUNT (three-state:
	// an unreadable worktree leaves Conflict nil = hold, never zero).
	if status == mergequeue.StatusCatchingUp {
		if st := w.landing; st != nil && mqConflictSignal(*st) {
			if paths, ok := d.mqUnmergedPaths(ctx, root, item.ID); ok {
				// A block that says conflict while the index reports NO
				// unmerged path is a stale block — the merge was already
				// resolved (by the resolution loop or by a person). The
				// worktree read is the arbiter (MQ9), so the stale signal
				// is cleared: the loop is woken and the item rides on.
				if len(paths) == 0 {
					w.staleConflict = w.landing
				} else {
					w.conflictPaths = paths
					n := len(paths)
					if n > mqConflictPathCap {
						w.conflictPaths = paths[:mqConflictPathCap]
					}
					w.f.Conflict = &mergequeue.ConflictFacts{Count: n}
				}
			}
		}
	}

	// The gate answer for THIS integration SHA only (Q7's binding):
	// verify evidence whose head is the SHA we would land.
	if st := w.landing; st != nil && sha != "" {
		ev := st.Evidence["verify"]
		if ev["verify_head_sha"] == sha {
			switch ev["verify_state"] {
			case "pass":
				good := true
				w.f.GateResult = &good
			case "fail":
				bad := false
				w.f.GateResult = &bad
			}
		}
	}

	// Behind-ness, where it decides (ready: the catch-up bump; merging:
	// the head-moved bounce inside refuse()). Three states always;
	// `unavailable` never renders as 0 (MQ10).
	if status == mergequeue.StatusReady || status == mergequeue.StatusMerging {
		head := sha
		if head == "" {
			head = item.VerifiedSHA
		}
		if head != "" {
			res, err := workspace.Staleness(ctx, root, item.BaseBranch, head)
			if err != nil {
				w.f.BehindState = mergequeue.BehindUnavailable
			} else {
				switch res.State {
				case workspace.StaleOK:
					w.f.BehindState = mergequeue.BehindOK
					behind := res.Behind
					w.f.Behind = &behind
				case workspace.StaleNotARepo:
					w.f.BehindState = mergequeue.BehindNotARepo
				default:
					w.f.BehindState = mergequeue.BehindUnavailable
				}
			}
		} else {
			w.f.BehindState = mergequeue.BehindUnavailable
		}
	}

	// The forge facts, once a landing PR exists. ReadPR is the same read
	// merge's does; nil forge (a non-GitHub origin) makes every
	// forge-dependent decision hold.
	var prHead string
	if item.PRNumber != nil {
		fc := d.mqForge(root)
		if fc == nil {
			if status == mergequeue.StatusReady {
				w.degraded = true
				w.degradeNote = "no forge client for this root — the landing PR cannot be read"
			}
		} else if pr, err := fc.ReadPR(ctx, *item.PRNumber); err != nil {
			if status == mergequeue.StatusReady {
				w.degraded = true
				w.degradeNote = fmt.Sprintf("the landing PR #%d cannot be read — the head binding is unknown", *item.PRNumber)
			}
		} else {
			prHead = pr.HeadSHA
			if pr.Merged {
				w.f.Landed = true
				w.mergeSHA = pr.MergeSHA
			} else {
				w.f.HeadSHAMatches = pr.HeadSHA != "" && pr.HeadSHA == item.VerifiedSHA
				// A refused landing names its kind honestly where the
				// reads can tell them apart: a moved head is head-moved,
				// anything else refused at the land step is the forge's.
				if st := w.landing; st != nil && st.BlockKind == "action-refused" && st.Position.Lane == "land" {
					if prHead != item.VerifiedSHA {
						w.f.Refused = string(mergequeue.ReasonHeadMoved)
					} else {
						w.f.Refused = string(mergequeue.ReasonForge)
					}
				}
			}
		}
	}
	if !w.f.Landed {
		// The crash rung after the merge call and before the record: the
		// land lane's OWN evidence proves the merge happened.
		if st := w.landing; st != nil {
			if ms := st.Evidence["land"]["merge_sha"]; ms != "" {
				w.f.Landed = true
				w.mergeSHA = ms
			}
		}
	}
	// The post-guard's proof (MQ12.4): the merge SHA is an ancestor of
	// the fetched base — merged is a PROVED word, here and in the loop.
	if w.f.Landed && w.mergeSHA != "" {
		if gitC := d.mqGitClient(root); gitC != nil {
			if err := gitC.Fetch(ctx, root, "origin", item.BaseBranch); err == nil {
				if ok, err := gitC.IsAncestor(ctx, root, w.mergeSHA, "origin/"+item.BaseBranch); err == nil {
					w.f.Ancestor = &ok
				}
			}
		}
	}

	// The land-step refusals the queue ends items on (never a retry loop
	// on the same refusal).
	if st := w.landing; st != nil && st.Position.Lane == "land" {
		switch st.BlockKind {
		case "action-denied", "action-denied-by-person":
			w.f.LandDenial = string(mergequeue.ReasonDenied)
		}
	}

	// auto mode's two floors, read only when ready could auto-land: the
	// S57 rule and the S56 grant. Both hold (never end) when unknown.
	if status == mergequeue.StatusReady && cfg.Mode == mergequeue.ModeAuto {
		w.f.RuleOK = d.mqRuleVerdict(ctx, root, item)
		w.f.GrantMerge = d.mqGrantCoversLand(ctx, root)
	}
	if status == mergequeue.StatusReady {
		w.f.LandRequested = d.landRequested(item.ID)
	}
	if status == mergequeue.StatusConflicted && d.mqConflictAnswer(item) == "abort" {
		w.f.Aborted = true
	}
	return w
}

// mqHeadOfBase answers "first in queue order on this base". Conflicted
// items do not hold the head (a parked conflict must not stop the line —
// deviation recorded at T7: the spec does not name it, and holding the
// line hostage to an unresolved conflict contradicts behaviour 5's
// "nothing else moves"). The tie-break mirrors the store's own order.
func mqHeadOfBase(item store.MergeItem, group []store.MergeItem) *bool {
	bestPos, bestID := int64(0), ""
	for _, g := range group {
		if mergequeue.Status(g.Status) == mergequeue.StatusConflicted {
			continue
		}
		if bestID == "" || g.Position < bestPos || (g.Position == bestPos && g.ID < bestID) {
			bestPos, bestID = g.Position, g.ID
		}
	}
	head := bestID == "" || bestID == item.ID
	return &head
}

// mqLandingSlotStatus is the landing half — the four statuses that occupy
// the one-at-a-time slot (MQ8): from catch-up through merging.
func mqLandingSlotStatus(s string) bool {
	switch mergequeue.Status(s) {
	case mergequeue.StatusCatchingUp, mergequeue.StatusVerifying, mergequeue.StatusReady, mergequeue.StatusMerging:
		return true
	}
	return false
}

// mqConflictSignal is the T5 contract read as data (MQ9): the loop's OWN
// BlockKind names an action-conflict — never a prose parse. The empty
// BlockKind blocked-at-catch-up shape stays a signal too (a loop blocked
// before T6 landed its kind could not say; the worktree read below is the
// arbiter either way).
func mqConflictSignal(st loop.LoopState) bool {
	if st.BlockKind == "action-conflict" {
		return true
	}
	return mqLoopEnum(st) == mergequeue.LoopBlocked && st.Position.Lane == "catch-up" && st.BlockKind == ""
}

// mqUnmergedPaths is the conflict COUNT's source — a read of the
// integration worktree's index (git ls-files -u through the client). ok=
// false means the read failed (or the worktree is gone), which keeps the
// conflict fact UNKNOWN, and Next holds. No content is read or stored.
func (d *Daemon) mqUnmergedPaths(ctx context.Context, root, itemID string) ([]string, bool) {
	gitC := d.mqGitClient(root)
	if gitC == nil {
		return nil, false
	}
	paths, err := gitC.UnmergedPaths(ctx, action.IntegrationWorktreePath(root, itemID))
	if err != nil {
		return nil, false
	}
	return paths, true
}

// mqGitClient is the root's git instrument for the reactor's READS only.
func (d *Daemon) mqGitClient(root string) *git.Client {
	svc := d.actionServices(root)
	if svc == nil || svc.Git == nil {
		return nil
	}
	return svc.Git.Git
}

// mqRuleVerdict is the S57 read for the auto arm: the effective merge
// rule evaluated over the SAME GatherGateFacts the land step itself
// consults (one truth, read early only to decide whether to hand over).
func (d *Daemon) mqRuleVerdict(ctx context.Context, root string, item store.MergeItem) *bool {
	if item.PRNumber == nil {
		return nil
	}
	comp, err := d.store.ComponentForRoot(root)
	if err != nil {
		return nil
	}
	rule, _, err := d.store.EffectiveMergeRule(comp.ID)
	if err != nil {
		return nil
	}
	fc := d.mqForge(root)
	if fc == nil {
		return nil
	}
	pr, err := fc.ReadPR(ctx, *item.PRNumber)
	if err != nil {
		return nil
	}
	facts, err := action.GatherGateFacts(ctx, fc, *item.PRNumber, pr.HeadSHA)
	if err != nil {
		return nil
	}
	v := mergerule.Evaluate(rule, facts)
	ok := v.Allowed
	return &ok
}

// mqGrantCoversLand is the S56 read: does the root's live pin carry a
// standing forge.land grant? A read failure answers false — which holds
// the auto arm (the sentence names the grant), never lands.
func (d *Daemon) mqGrantCoversLand(ctx context.Context, root string) bool {
	pin, err := d.resolveAutonomy(ctx, autonomy.OriginEvent, root, nil)
	if err != nil || pin == nil {
		return false
	}
	var entries []pinEntry
	if err := json.Unmarshal([]byte(pin.Allowed), &entries); err != nil {
		return false
	}
	for _, e := range entries {
		if e.Subject == "forge.land" {
			return true
		}
	}
	return false
}

// mqItemFromLoopSlug recovers the item behind a queue-owned loop slug —
// "land-<itemID>" or "resolve-<itemID>" with the optional restart suffix
// "-<n>" (mqNextLoopSlug's numbering). Any other slug is not the
// queue's and the reaper keeps walking.
func mqItemFromLoopSlug(slug string) string {
	rest := strings.TrimPrefix(slug, "land-")
	if rest == slug {
		rest = strings.TrimPrefix(slug, "resolve-")
	}
	if rest == slug || !strings.HasPrefix(rest, "mq-") {
		return ""
	}
	if i := strings.LastIndex(rest, "-"); i > 0 {
		tail := rest[i+1:]
		numeric := tail != ""
		for _, ch := range tail {
			if ch < '0' || ch > '9' {
				numeric = false
				break
			}
		}
		if numeric {
			rest = rest[:i]
		}
	}
	return rest
}

// mqReapOrphanLoops is behaviour 11's other half and the recovery of
// dequeues: a DEQUEUED (or otherwise terminal) item must not leave a
// live landing loop behind — the loop would keep catching up, publishing
// and possibly LANDING an item that no longer exists in the queue. The
// loop dies through its own door (S55's CancelLoop with the queue's
// control actor, never a back door), and the integration worktree is
// settled by the terminal-item sweep that already runs. Sweeping here —
// rather than only inside the dequeue door — is what makes the crash
// rung work: a daemon that died mid-dequeue reaps the orphan on the
// next tick exactly as a live dequeue would.
func (d *Daemon) mqReapOrphanLoops(ctx context.Context) {
	rows, err := d.store.ListLoops(store.ComponentScope{})
	if err != nil {
		d.logf("merge-queue: orphan sweep could not list loops: %v", err)
		return
	}
	for _, l := range rows {
		switch l.Status {
		case "done", "idle", "aborted", "cancelled", "failed":
			continue
		}
		if ctx.Err() != nil {
			return
		}
		itemID := mqItemFromLoopSlug(l.Slug)
		if itemID == "" {
			continue
		}
		term, err := d.store.MergeItemTerminal(itemID)
		if err != nil || !term {
			continue
		}
		if _, refusal, err := d.mqControlCancel(ctx, l.ID, mqControlActor, "merge-queue: the item is terminal — its loop is reaped"); err != nil {
			d.logf("merge-queue: reaping %s failed: %v", l.Slug, err)
		} else if refusal != nil {
			// The engine refuses to cancel a loop parked on a question
			// ("answer it first") — and a TERMINAL item's parked question
			// has no answer that continues the work. The queue answers
			// the way the person's dequeue already said: the stop-word in
			// the question's own options (abort, deny), the loop's free
			// exit (OD1). No stop-word, nothing to answer — the refusal
			// is spoken and the sweep moves on.
			d.mqRetireByAnswer(ctx, l)
		} else {
			d.logf("merge-queue: reaped orphan loop %s (item %s is terminal)", l.Slug, itemID)
		}
	}
}

// mqRetireByAnswer answers a reaped loop's standing gate question with
// its own stop-word — the only exit the engine's cancel refusal leaves,
// and the honest one: the person who dequeued the item has already said
// no to the landing.
func (d *Daemon) mqRetireByAnswer(ctx context.Context, l store.Loop) {
	q, found, err := d.store.PendingQuestion(store.Scope{ComponentRoot: l.ComponentRoot, LoopSlug: l.Slug})
	if err != nil || !found || q.Kind != string(store.QuestionGate) || q.StepID != nil {
		d.logf("merge-queue: reaping %s refused and no gate question stands to answer", l.Slug)
		return
	}
	word := ""
	for _, o := range q.Options {
		switch strings.ToLower(strings.TrimSpace(o)) {
		case "abort", "deny", "no", "reject":
			word = strings.ToLower(strings.TrimSpace(o))
		}
		if word != "" {
			break
		}
	}
	if word == "" {
		d.logf("merge-queue: reaping %s refused and its question offers no stop-word: %s", l.Slug, q.Body)
		return
	}
	ans, err := d.store.Answer(q.ID, word)
	if err != nil || !ans.OK {
		d.logf("merge-queue: reaping %s: answering %s failed or was already answered", l.Slug, word)
		return
	}
	d.logf("merge-queue: reaped orphan loop %s by answering its standing question %q (item terminal)", l.Slug, word)
}
