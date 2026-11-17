package daemon

// continuous.go — the ONLY file where the continuous scheduler (S27) meets
// the daemon: the serve-side goroutine doors, the reactor's ownership
// probe, and the adapter that reads the store into a continuous.Snapshot.
// The pure core lives in internal/continuous and imports nothing from
// here; this file imports it and fills its seams — Reader and
// SettingsReader in this file, Starter and Reconciler in
// continuous_start.go. The same bargain as automation.go: unwired seams
// answer honestly that there is nothing to run.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/continuous"
	"github.com/cjlapao/aeon/internal/queue"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/store"
)

// queueLiveStatuses is CN6.2's drain gate as the snapshot counts it: every
// queue row in these statuses keeps the repository's roadmap work out.
// Stricter than R4's "rows exist" probe on purpose — a done or cancelled
// row is history, not company.
var queueLiveStatuses = map[string]bool{
	string(queue.StatusPending): true, string(queue.StatusSizing): true,
	string(queue.StatusReady): true, string(queue.StatusRunning): true,
	string(queue.StatusBlocked): true,
}

// continuousWorker returns the daemon's one scheduler worker, built on
// first use and stable after: the reactors kick THAT instance, and Run
// ticks on it. Nothing starts here — construction cannot fail and starts
// nothing; serve.go decides whether this goroutine ever runs.
func (d *Daemon) continuousWorker() *continuous.Worker {
	d.continuousOnce.Do(func() {
		a := &continuousAdapter{d: d,
			locks:   map[string]*sync.Mutex{},
			weights: map[string]int{},
		}
		d.continuousAdp = a // S27 T7: the socket reads answer through the
		// SAME adapter the worker ticks on — one weight cache, one truth.
		d.continuous = continuous.NewWorker(a, a, nil, a, d.log)
	})
	return d.continuous
}

// continuousAdapterRef builds (lazily, exactly-once) the worker and answers
// its adapter: the socket's reads share the adapter the worker ticks on,
// so a preview and the next tick never read the world through two shapes.
func (d *Daemon) continuousAdapterRef() *continuousAdapter {
	d.continuousWorker()
	return d.continuousAdp
}

// RunContinuous is serve.go's goroutine body for the scheduler, the same
// shape as RunAutomation: a storeless daemon (or one without the loop
// runner a start needs) has nothing to tick on; errors are logged by the
// caller, never fatal. CN19's cadence and coalesced kicks live inside
// Worker.Run — this door only holds the seams.
func (d *Daemon) RunContinuous(ctx context.Context) error {
	if d.store == nil || d.loopRunner == nil {
		return nil // the honest absence — no rows, no runner, nothing to schedule
	}
	if err := d.continuousWorker().Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// ReconcileContinuous is CN14's crash pass, run ONCE by serve.go after
// StartLoops and before RunContinuous — and not at all under
// --no-recover (CN17): a --no-recover boot leaves every open claim
// exactly as the crash left it, for a person.
func (d *Daemon) ReconcileContinuous(ctx context.Context) error {
	if d.store == nil || d.loopRunner == nil {
		return nil
	}
	return d.continuousWorker().Reconcile(ctx)
}

// continuousOwns answers the reactors' one question: does the continuous
// scheduler hold the wheel for this repository? True means the reactor
// KICKS the scheduler instead of advancing the queue itself; false means
// the S24 path runs byte-identically to today — a repository that never
// enabled the scheduler, a scheduler that is off, and a paused scheduler
// all leave the old door exactly where it was. Paused is deliberately on
// the "does not own" side: a person who paused the scheduler did not ask
// for their queue to stop advancing.
func (d *Daemon) continuousOwns(componentRoot string) bool {
	if d.store == nil || d.loopRunner == nil {
		return false
	}
	st, err := d.store.SchedulerState()
	if err != nil || !st.Enabled || st.Paused {
		return false
	}
	for _, row := range d.mustRoots() {
		if row.ComponentRoot == componentRoot && row.Enabled {
			return true
		}
	}
	return false
}

// continuousKick wakes the worker for this repository NOW (CN19's kick
// half). Non-blocking by contract; dropped silently when no worker was
// ever built — a kick into a daemon that never started the scheduler is
// nothing's news.
func (d *Daemon) continuousKick(componentRoot string) {
	if d.continuous == nil {
		return
	}
	d.continuous.Kick(componentRoot)
}

// rootRow reads one repository's scheduler_root row (false = no row —
// which is the SAME answer as "not enabled", because an unregistered
// repository has never opted in).
func (a *continuousAdapter) rootRow(root string) (store.SchedulerRootRow, bool) {
	for _, row := range a.d.mustRoots() {
		if row.ComponentRoot == root {
			return row, true
		}
	}
	return store.SchedulerRootRow{}, false
}

// mustRoots reads the scheduler_root rows; a read failure answers nothing
// (callers that must not act on a broken read say so themselves).
func (d *Daemon) mustRoots() []store.SchedulerRootRow {
	rows, err := d.store.SchedulerRoots()
	if err != nil {
		return nil
	}
	return rows
}

// continuousAdapter is the whole seam set: the snapshot read (here) and
// the claim-then-start (continuous_start.go) on one struct, because both
// need the same per-root serialization — the lock that claims must be the
// lock the snapshot's occupancy facts were read beside, or two ticks could
// each "see" an empty repository.
type continuousAdapter struct {
	d *Daemon

	startMu sync.Mutex             // guards locks and weights
	locks   map[string]*sync.Mutex // one per root: claim→start is atomic per repository
	weights map[string]int         // effective weight cache, one snapshot at a time
}

// rootLock returns (creating once) the repository's claim lock.
func (a *continuousAdapter) rootLock(root string) *sync.Mutex {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	l, ok := a.locks[root]
	if !ok {
		l = &sync.Mutex{}
		a.locks[root] = l
	}
	return l
}

// Load answers the Worker's ticker question. S27 T8 landed the scheduler.*
// declarations, so the answer is the four declared numbers read FRESH
// through the settings store (continuous_settings.go) — every tick, every
// status call, no cache (CS7). A change lands on the next tick.
func (a *continuousAdapter) Load(ctx context.Context) (continuous.Settings, error) {
	return a.liveSettings(ctx), nil
}

// Snapshot reads the whole world once, at `at`. Every fact below arrives
// through a store method — this file writes no SQL and parses no row text
// outside the store's reads — and every per-root read is ordered
// deterministically, so two snapshots of an unchanged database are equal
// data (the property tests in internal/continuous depend on that only for
// its shape; the ORDER law here is for the audit: a tick's rows and its
// snapshot agree on who was asked about first).
func (a *continuousAdapter) Snapshot(ctx context.Context, at time.Time) (continuous.Snapshot, error) {
	st, err := a.d.store.SchedulerState()
	if err != nil {
		return continuous.Snapshot{}, err
	}
	s := continuous.Snapshot{
		Now:      at,
		Master:   continuous.Master{Enabled: st.Enabled, Paused: st.Paused},
		Settings: a.liveSettings(ctx),
	}
	// The weight cache is per snapshot: effective caps may change between
	// ticks, and a tick must not mix two instants of them.
	a.startMu.Lock()
	a.weights = map[string]int{}
	a.startMu.Unlock()

	s.Slots.Used = a.usedWeights()

	rows := a.d.mustRoots()
	sort.Slice(rows, func(i, j int) bool { return rows[i].ComponentRoot < rows[j].ComponentRoot })
	for _, row := range rows {
		if ctx.Err() != nil {
			return continuous.Snapshot{}, ctx.Err()
		}
		s.Repos = append(s.Repos, a.repoView(ctx, row, at))
	}
	return s, nil
}

// usedWeights is CN9's Used: every loop row whose status reads `running`
// costs its own repository's effective weight — the same number a
// candidate would spend. The row is the truth because the continuous
// goroutine starts only AFTER recovery (serve.go): a running row was
// either recovered and is being run or was left running on purpose, and
// either way it occupies a slot. A person's loop spends capacity exactly
// as the scheduler's own does — CN8 never counts an audience.
func (a *continuousAdapter) usedWeights() int {
	slots, err := a.d.store.RunningLoopSlots()
	if err != nil {
		return 0 // an unreadable slots read cannot spend capacity it cannot count
	}
	used := 0
	for _, slot := range slots {
		root := ""
		if loops, err := a.d.store.LoopsBySlug(slot.Slug); err == nil && len(loops) == 1 {
			root = loops[0].ComponentRoot
		}
		used += a.weightOf(root)
	}
	return used
}

// weightOf reads (once per snapshot) the repository's effective
// max_parallel_agents — the component-tree cap that S44-extra computes and
// this scheduler only spends. A root outside the component tree has no cap
// row; the EffectiveForRoot default (1) is the honest floor.
func (a *continuousAdapter) weightOf(root string) int {
	a.startMu.Lock()
	defer a.startMu.Unlock()
	if w, ok := a.weights[root]; ok {
		return w
	}
	w := 1
	if eff, err := a.d.store.EffectiveForRoot(root); err == nil && eff.MaxParallelAgents.Value > 0 {
		w = eff.MaxParallelAgents.Value
	}
	a.weights[root] = w
	return w
}

// repoView reads one repository's whole world. Reads that fail answer the
// ZERO fact and log — the snapshot is a tick's input, and a tick that
// could not read a fact must say "no" with the facts it DID read rather
// than assume the optimistic one (a wrongly-assumed empty repository
// starts nothing; a wrongly-assumed free one starts something).
func (a *continuousAdapter) repoView(ctx context.Context, row store.SchedulerRootRow, at time.Time) continuous.Repo {
	repo := continuous.Repo{
		Root:        row.ComponentRoot,
		Enabled:     row.Enabled,
		Template:    row.Blueprint,
		TemplateOK:  row.Blueprint != "",
		Window:      continuous.Window{Start: row.WindowStart, End: row.WindowEnd, Days: row.WindowDays, Zone: row.Zone},
		MinPriority: roadmap.PriorityState(row.MinPriority),
		Weight:      a.weightOf(row.ComponentRoot),
	}
	// T6's pre-Plan seams (CN24): S55's start-time budget read over the
	// repository's own ground answers BudgetOK and carries its sentence
	// verbatim; the grant seam answers GrantAbsent ONLY when the policy
	// could not be read at all (V13's honest fact — the next tick
	// retries), never for a root that reads cleanly and holds no
	// grants: a grantless scheduler starts local-only, the build-only
	// machine the design promises.
	repo.BudgetOK, repo.BudgetReason, repo.GrantAbsent = a.seamFacts(ctx, row.ComponentRoot)
	if repo.TemplateOK {
		if _, err := a.d.loadStartBlueprint(row.Blueprint); err != nil {
			repo.TemplateOK = false
		}
	}
	occ, err := a.d.store.OccupyingLoops(row.ComponentRoot)
	if err == nil {
		for _, o := range occ {
			repo.Occupying = append(repo.Occupying, continuous.LoopRef{Slug: o.Slug, State: o.Status})
		}
	}
	qrows, err := a.d.store.ListQueueItems(row.ComponentRoot)
	if err == nil {
		for _, qr := range qrows {
			if queueLiveStatuses[qr.Status] {
				repo.QueueLive++
			}
		}
		if head, ok, err := a.d.store.NextReadyQueueItem(row.ComponentRoot); err == nil && ok {
			hu := &continuous.QueueUnit{ID: head.ID, Status: head.Status, AutonomyCap: "grant"}
			if cap, err := a.itemCap("queue", head.ID); err == nil {
				hu.AutonomyCap = cap
			} else {
				a.d.log.Infof("continuous: the cap of queue item %d could not be read: %v", head.ID, err)
			}
			if head.Attended != nil {
				att := *head.Attended != 0
				hu.Attended = &att
			}
			repo.QueueHead = hu
		}
	}
	ready, err := a.d.store.ListRoadmapItems(row.ComponentRoot, store.RoadmapItemFilters{Status: "ready"})
	if err == nil {
		for _, rr := range ready {
			it, err := roadmap.ItemFromRow(rr)
			if err != nil {
				continue
			}
			u := continuous.RoadmapUnit{
				ID:          it.ID,
				Priority:    it.Priority,
				BriefOK:     it.BriefPath != "",
				PlanOK:      it.PlanPath != "",
				AutonomyCap: "grant",
			}
			if cap, err := a.itemCap("roadmap", it.ID); err == nil {
				u.AutonomyCap = cap
			} else {
				a.d.log.Infof("continuous: the cap of roadmap item %d could not be read: %v", it.ID, err)
			}
			if deps, err := a.d.store.OpenDependencies(it.ID); err == nil {
				for _, dep := range deps {
					u.OpenDeps = append(u.OpenDeps, continuous.Dep{ID: dep.ID, Status: roadmap.Status(dep.Status)})
				}
			}
			if _, found, err := a.d.store.StartRecord("roadmap", it.ID); err == nil && found {
				u.Claimed = true
			}
			repo.Ready = append(repo.Ready, u)
			if first, found, err := a.d.store.FirstDeferral("roadmap", it.ID, "capacity"); err == nil && found {
				if t, err := time.Parse("2006-01-02T15:04:05.000Z", first.At); err == nil {
					if repo.FirstDeferral == nil {
						repo.FirstDeferral = map[continuous.SubjectKey]time.Time{}
					}
					repo.FirstDeferral[continuous.Subject{Kind: "roadmap", ID: fmt.Sprintf("%d", it.ID)}.Key()] = t
				}
			}
		}
	}
	if repo.QueueHead != nil {
		if first, found, err := a.d.store.FirstDeferral("queue", repo.QueueHead.ID, "capacity"); err == nil && found {
			if t, err := time.Parse("2006-01-02T15:04:05.000Z", first.At); err == nil {
				if repo.FirstDeferral == nil {
					repo.FirstDeferral = map[continuous.SubjectKey]time.Time{}
				}
				repo.FirstDeferral[continuous.Subject{Kind: "queue", ID: fmt.Sprintf("%d", repo.QueueHead.ID)}.Key()] = t
			}
		}
	}
	if last, found, err := a.d.store.LastSchedulerStart(row.ComponentRoot); err == nil && found {
		repo.LastStart = last
	}
	if starts, err := a.d.store.StartTimesSince(row.ComponentRoot, at.Add(-time.Hour)); err == nil {
		repo.StartsInHour = starts
	}
	return repo
}
