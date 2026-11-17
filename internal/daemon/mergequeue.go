package daemon

// mergequeue.go — S60 T8: the daemon's queue door service, the socket half
// of the merge queue. It implements protocol.MergeQueueService over the
// SAME store, the SAME mergequeue.Service (T3) and the SAME driver the
// reactor runs (T7): one implementation, two doors — the person's socket
// here, the machine's driver there — and the parity of that pairing is the
// queue's honesty (the door never moves a state the driver would refuse,
// because the door asks the driver's own door to move it).
//
// The four rules this file exists to keep:
//
//   - The ACTOR is the connection's peer ({user, <uid>}), stamped into the
//     store's merge actor and every event. No param is ever consulted
//     (MQ13; protocol/mergequeue.go refused the identity keys before this
//     service ever ran).
//   - The DRIVER moves states. Land answers the loop's standing gate
//     question with the word the person's door means by it ("merge-now"),
//     records the demand (T7's hook), and reconciles the queue SYNCHRONOUSLY
//     so the answer comes back with the row already moved — through the
//     driver's door, never around it.
//   - A dequeue stops the item's loops FIRST (behaviour 3): a loop of a
//     removed item would land a merge nobody queued; each live loop dies
//     through the loop plane's own door (CancelLoop, person-actor), and a
//     loop parked on a question — which the engine will not cancel — is
//     answered with its own stop-word, the same honest exit the reaper
//     gives an orphan.
//   - Authority-widening settings (mode to auto, policy to resolve) answer
//     the daemon's OWN sentence (mergequeue.Statement, the one formatter)
//     and apply only with an `acknowledged` equal to that sentence's
//     digest. No digest, no change; the sentence rides the refusal.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/workspace"
)

type mergeQueueDoors struct{ d *Daemon }

// newMergeQueueDoors is the daemon's one queue door service.
func newMergeQueueDoors(d *Daemon) *mergeQueueDoors { return &mergeQueueDoors{d: d} }

var _ protocol.MergeQueueService = (*mergeQueueDoors)(nil)

// mqTerminalStatuses is the closed terminal trio the history listings show
// beyond the six open states (MQ5's ends; nothing else terminates a row).
var mqTerminalStatuses = []string{"merged", "failed", "removed"}

// --- views --------------------------------------------------------------------------

// mqLandDigest binds a land to the row AS READ: the id, the status, the
// head the person saw, and the position. A moved item digests differently,
// and the land refuses — the thing they approved is not the thing that
// would merge (T8's binding; the same sha256 the door recomputes).
func mqLandDigest(it store.MergeItem) string {
	sum := sha256.Sum256([]byte("s60.land|" + it.ID + "|" + it.Status + "|" + it.HeadSHA + "|" + fmt.Sprint(it.Position)))
	return hex.EncodeToString(sum[:])
}

// mqStatementDigest is the acknowledgement token for an authority-widening
// settings write: sha256 of the daemon's sentence. The sentence renders
// once (mergequeue.Statement) and every surface quotes the same bytes.
func mqStatementDigest(sentence string) string {
	sum := sha256.Sum256([]byte("s60.settings|" + sentence))
	return hex.EncodeToString(sum[:])
}

// mqItemView maps a row to the wire shape. The driver's Describe sentence
// is the nextStep, byte for byte (one formatter, S44's rule generalised);
// the staleness stays unset here — callers that answered it say so.
func mqItemView(it store.MergeItem) protocol.ItemView {
	return protocol.ItemView{
		ID:                it.ID,
		ComponentRoot:     it.ComponentRoot,
		Base:              it.BaseBranch,
		Branch:            it.Branch,
		Loop:              it.LoopSlug,
		Status:            it.Status,
		Reason:            it.ReasonKind,
		Position:          it.Position,
		Attended:          it.Attended,
		HeadSha:           it.HeadSHA,
		BaseSha:           it.BaseSHA,
		IntegrationBranch: it.IntegrationBranch,
		IntegrationSha:    it.IntegrationSHA,
		PrNumber:          it.PRNumber,
		VerifiedSha:       it.VerifiedSHA,
		LandedSha:         it.LandedSHA,
		LandingLoop:       it.LandingLoopSlug,
		ResolveLoop:       it.ResolveLoopSlug,
		CatchUps:          it.CatchUps,
		EnqueuedBy:        protocol.EnqueuedByView{Kind: it.EnqueuedByKind, ID: it.EnqueuedByID},
		Staleness:         protocol.StalenessView{State: "unknown", Reason: "not read"},
		NextStep:          mergequeue.Describe(mergequeue.ToItem(it)),
		Digest:            mqLandDigest(it),
		CreatedAt:         it.CreatedAt,
		UpdatedAt:         it.UpdatedAt,
	}
}

// mqStalenessRead is the listing's cheap three-state: cached refs only (no
// fetch on a READ — the driver fetches on its own clock), for the states
// where behind-ness decides anything. Every other row answers "settled" (a
// terminal row) or "not-yet" with the reason.
func (m *mergeQueueDoors) mqStalenessRead(ctx context.Context, v protocol.ItemView, it store.MergeItem) protocol.ItemView {
	switch it.Status {
	case "merged", "failed", "removed":
		v.Staleness = protocol.StalenessView{State: "settled"}
		return v
	case "ready", "merging":
	default:
		v.Staleness = protocol.StalenessView{State: "not-yet", Reason: "the queue has not read its base for this state yet"}
		return v
	}
	res, err := workspace.Staleness(ctx, it.ComponentRoot, it.BaseBranch, it.HeadSHA)
	if err != nil {
		v.Staleness = protocol.StalenessView{State: "unknown", Reason: "the base cannot be read (no fetch has run, or the root is not a repository)"}
		return v
	}
	switch res.State {
	case workspace.StaleOK:
		if res.Behind > 0 {
			b := int64(res.Behind)
			v.Staleness = protocol.StalenessView{State: "behind", Behind: &b}
		} else {
			v.Staleness = protocol.StalenessView{State: "current"}
		}
	case workspace.StaleNotARepo:
		v.Staleness = protocol.StalenessView{State: "unknown", Reason: "the component root is not a git repository"}
	default:
		v.Staleness = protocol.StalenessView{State: "unknown", Reason: "the base has never been fetched for this repository"}
	}
	return v
}

// mqDoorError speaks the plane's four refusals from the service's and the
// store's own sentences. A refusal the plane has no distinct word for is
// ErrMergeQueueRefused carrying it verbatim — never a paraphrase; a store
// FAULT (not a refusal) rides out as itself, because an unavailable store
// is not the queue declining.
func mqDoorError(err error) error {
	var mr store.MergeRefusal
	if errors.As(err, &mr) {
		switch mr.Kind {
		case "unknown-item":
			return protocol.ErrMergeQueueNotFound.WithWhat("%s (id %s)", mr.Reason, mr.Item)
		case "unknown-root":
			return protocol.ErrMergeQueueBaseUnknown.WithWhat("%s", mr.Reason)
		case "not-queued", "stale-state", "no-op", "slot-taken":
			cur := mr.Current
			if cur == "" {
				cur = "the state named in the sentence above"
			}
			if mr.Kind == "slot-taken" {
				return protocol.ErrMergeQueueRefused.WithWhat("%s (the slot is held by %s)", mr.Reason, mr.Holder)
			}
			return protocol.ErrMergeQueueConflictState.WithWhat("%s — the row is %s", mr.Reason, cur)
		default:
			return protocol.ErrMergeQueueRefused.WithWhat("%s", mr.Reason)
		}
	}
	var rf *mergequeue.Refusal
	if errors.As(err, &rf) {
		if rf.Kind == "base-unknown" {
			return protocol.ErrMergeQueueBaseUnknown.WithWhat("%s", rf.Reason)
		}
		// "not-retryable" IS the state refusal — the service's own words
		// ("item X is queued — a retry answers a failure") name the state
		// the row is in, which is exactly what -31955 promises.
		if rf.Kind == "not-retryable" {
			return protocol.ErrMergeQueueConflictState.WithWhat("%s", rf.Reason)
		}
		return protocol.ErrMergeQueueRefused.WithWhat("%s", rf.Reason)
	}
	// The store's own not-found sentinel (measured live 2026-11-04 during
	// T10's daemon walk): the id-taking doors read the row with
	// ReadMergeItem, which answers `store: merge item X: store: not
	// found` wrapping store.ErrNotFound — NOT a MergeRefusal. Before this
	// arm the plane's own -31953 never fired for a missing id on ANY door
	// that reads by id (show, dequeue, retry, land, and the store-read
	// half of reorder/transition); the client saw a bare JSON-RPC -32603
	// with three empty parts — exactly the -31002-has-to-go-and-be-looked-
	// up failure renderError exists against. A missing row IS the plane's
	// not-found; the store's sentence rides it verbatim.
	if errors.Is(err, store.ErrNotFound) {
		return protocol.ErrMergeQueueNotFound.WithWhat("%s", err.Error())
	}
	return err
}

// --- the reads -------------------------------------------------------------------------

func (m *mergeQueueDoors) List(ctx context.Context, p protocol.MergeQueueListParams) ([]protocol.ItemView, error) {
	statuses := append([]string{}, store.MergeOpenStatusSet...)
	if p.IncludeTerminal {
		statuses = append(statuses, mqTerminalStatuses...)
	}
	var rows []store.MergeItem
	var err error
	if strings.TrimSpace(p.ComponentRoot) == "" {
		rows, err = m.d.store.ListMergeItemsAcrossRoots(statuses)
	} else {
		rows, err = m.d.store.ListMergeItems(store.MergeFilter{ComponentRoot: p.ComponentRoot, Statuses: statuses})
	}
	if err != nil {
		return nil, mqDoorError(err)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ComponentRoot != rows[j].ComponentRoot {
			return rows[i].ComponentRoot < rows[j].ComponentRoot
		}
		if rows[i].BaseBranch != rows[j].BaseBranch {
			return rows[i].BaseBranch < rows[j].BaseBranch
		}
		return rows[i].Position < rows[j].Position
	})
	out := make([]protocol.ItemView, 0, len(rows))
	for _, it := range rows {
		out = append(out, m.mqStalenessRead(ctx, mqItemView(it), it))
	}
	return out, nil
}

func (m *mergeQueueDoors) Show(ctx context.Context, p protocol.MergeQueueShowParams) (protocol.ShowResult, error) {
	it, err := m.d.store.ReadMergeItem(p.ID)
	if err != nil {
		var mr store.MergeRefusal
		if errors.As(err, &mr) && mr.Kind == "unknown-item" {
			return protocol.ShowResult{}, protocol.ErrMergeQueueNotFound.WithWhat("no merge_queue_item row answers id %q", p.ID)
		}
		return protocol.ShowResult{}, mqDoorError(err)
	}
	events, err := m.d.store.ListMergeQueueEvents(it.ID)
	if err != nil {
		return protocol.ShowResult{}, err
	}
	transitions := make([]protocol.TransitionView, 0, len(events))
	for _, e := range events {
		transitions = append(transitions, protocol.TransitionView{
			Kind: e.Kind, FromStatus: e.FromStatus, ToStatus: e.ToStatus,
			ReasonKind: e.ReasonKind, ActorKind: e.ActorKind, ActorID: e.ActorID,
			Detail: e.Detail, At: e.At,
		})
	}
	res := protocol.ShowResult{
		Item:        m.mqStalenessRead(ctx, mqItemView(it), it),
		Transitions: transitions,
	}
	if it.IntegrationBranch != "" && it.Status != "merged" && it.Status != "failed" && it.Status != "removed" {
		res.Worktree = action.IntegrationWorktreePath(it.ComponentRoot, it.ID)
	}
	return res, nil
}

func (m *mergeQueueDoors) Status(ctx context.Context, p protocol.MergeQueueStatusParams) (protocol.StatusResult, error) {
	root := strings.TrimSpace(p.ComponentRoot)
	cfg, err := m.d.store.ReadMergeConfig(root)
	if err != nil {
		return protocol.StatusResult{}, mqDoorError(err)
	}
	all, err := m.d.store.ListMergeItems(store.MergeFilter{ComponentRoot: root,
		Statuses: append(append([]string{}, store.MergeOpenStatusSet...), mqTerminalStatuses...)})
	if err != nil {
		return protocol.StatusResult{}, mqDoorError(err)
	}
	res := protocol.StatusResult{
		ComponentRoot: root,
		Counts:        map[string]int{},
		Lanes:         []protocol.LaneView{},
		Items:         []protocol.ItemView{},
		Config: protocol.ConfigView{
			ComponentRoot: cfg.ComponentRoot, Mode: cfg.Mode, ConflictPolicy: cfg.ConflictPolicy,
			LandMethod: cfg.LandMethod, MaxCatchUps: cfg.MaxCatchUps,
			UpdatedAt: cfg.UpdatedAt, UpdatedBy: cfg.UpdatedByKind + "/" + cfg.UpdatedByID,
			Absent: cfg.Absent,
		},
	}
	if res.Config.UpdatedBy == "/" {
		res.Config.UpdatedBy = ""
	}
	laneHead := map[string]string{}
	order := []string{}
	for _, it := range all {
		res.Counts[it.Status]++
		if _, seen := laneHead[it.BaseBranch]; !seen {
			order = append(order, it.BaseBranch)
		}
		if it.Status != "merged" && it.Status != "failed" && it.Status != "removed" {
			if cur, held := laneHead[it.BaseBranch]; !held || cur == "" {
				laneHead[it.BaseBranch] = it.ID
			}
			v := m.mqStalenessRead(ctx, mqItemView(it), it)
			res.Items = append(res.Items, v)
		}
	}
	for _, base := range order {
		res.Lanes = append(res.Lanes, protocol.LaneView{Base: base, HeadItem: laneHead[base]})
	}
	return res, nil
}

// Vocabulary answers the plane's closed sets DERIVED from the driver's own
// tables: the status list walks mergequeue.StatusSet and probes every edge
// through mergequeue.Legal — the exact table the driver moves by — so a
// screen can never show an edge the driver would refuse, and a new status
// constant lands in this answer with no edit here (MQ17).
func (m *mergeQueueDoors) Vocabulary(ctx context.Context) (protocol.VocabularyResult, error) {
	meanings := map[string]string{
		"queued":      "on the lane, waiting its turn; the loop has not started or has not been needed",
		"catching_up": "the integration branch is being built (or rebuilt after the base moved)",
		"verifying":   "the gate runs against the integration head — the queue's own S57 verification",
		"ready":       "the gate is green for this head; a manual queue waits for a person's land, an auto queue lands on its grants",
		"merging":     "the landing is in flight: the PR and the merge are the loop's work now",
		"merged":      "landed on base; landed_sha names the commit; the worktrees are cleaned",
		"conflicted":  "the integration merge conflicts; the resolve-or-ask plane owns the exit (paths and counts only, SEC-033)",
		"failed":      "ended short — reason_kind names the family (denied, gate-failed, head-moved, catchup-limit, loop-stopped, …)",
		"removed":     "taken out of the queue by a person or the stop contract; the branch and its commits are untouched",
	}
	states := make([]protocol.StateView, 0, len(mergequeue.StatusSet))
	for _, name := range mergequeue.StatusSet {
		from := mergequeue.Status(name)
		next := []string{}
		for _, cand := range mergequeue.StatusSet {
			to := mergequeue.Status(cand)
			if from == to {
				continue
			}
			if mergequeue.Legal(from, to) {
				next = append(next, cand)
			}
		}
		meaning := meanings[name]
		if meaning == "" {
			meaning = "no meaning is recorded for this state — the table has a row a sentence never arrived for"
		}
		states = append(states, protocol.StateView{Name: name, Meaning: meaning, LegalNext: next})
	}
	return protocol.VocabularyResult{
		States:           states,
		Reasons:          append([]string{}, mergequeue.ReasonSet...),
		Modes:            append([]string{}, mergequeue.ModeSet...),
		ConflictPolicies: append([]string{}, mergequeue.PolicySet...),
		LandMethods:      append([]string{}, mergequeue.MethodSet...),
		MergeActors:      []string{"user", "cli", "blueprint", "daemon", "agent"},
	}, nil
}

// --- the writes -------------------------------------------------------------------------

func (m *mergeQueueDoors) Enqueue(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueEnqueueParams) (protocol.ItemView, error) {
	svc := mergequeue.NewService(m.d.store)
	req := mergequeue.EnqueueRequest{
		ComponentRoot: strings.TrimSpace(p.ComponentRoot),
		Branch:        strings.TrimSpace(p.FeatureBranch),
		BaseBranch:    strings.TrimSpace(p.BaseBranch),
		Attended:      p.Attended,
	}
	facts, headSHA, baseSHA, err := m.d.mqRepoFacts(ctx, req.ComponentRoot, req.Branch, req.BaseBranch)
	if err != nil {
		return protocol.ItemView{}, protocol.ErrMergeQueueRefused.WithWhat("the enqueue could not be checked against its repository: %s", err)
	}
	if base := mergequeue.ResolveBase(req, facts); base != req.BaseBranch {
		// The caller gave no base and the repository answered one; re-gather
		// so the row's base_sha is the resolved branch's real commit.
		req.BaseBranch = base
		_, _, baseSHA, err = m.d.mqRepoFacts(ctx, req.ComponentRoot, req.Branch, base)
		if err != nil {
			return protocol.ItemView{}, protocol.ErrMergeQueueRefused.WithWhat("the enqueue could not be checked against its repository: %s", err)
		}
	}
	ma := store.MergeActor{Kind: "user", ID: actor.ID}
	it, err := svc.Enqueue(req, facts, store.NewMergeItem{
		ComponentRoot:  req.ComponentRoot,
		BaseBranch:     req.BaseBranch,
		Branch:         req.Branch,
		HeadSHA:        headSHA,
		BaseSHA:        baseSHA,
		Attended:       req.Attended,
		EnqueuedByKind: "user",
		EnqueuedByID:   actor.ID,
	}, ma)
	if err != nil {
		return protocol.ItemView{}, mqDoorError(err)
	}
	m.d.mergeQueueWake()
	return mqItemView(it), nil
}

// mqStopItemLoops puts the item's live loops down through the loop plane's
// own door, before anything touches the row (behaviour 3: a loop of a
// removed item must never land). A loop the engine will not cancel because
// a question stands receives its OWN stop-word — the answer the dequeue has
// already spoken (the reaper's honest exit, reused).
func (m *mergeQueueDoors) mqStopItemLoops(ctx context.Context, it store.MergeItem, actor protocol.Actor) {
	person := store.Actor{Kind: "user", ID: actor.ID}
	for _, slug := range []string{it.LandingLoopSlug, it.ResolveLoopSlug} {
		if slug == "" {
			continue
		}
		loops, err := m.d.store.LoopsBySlug(slug)
		if err != nil {
			continue
		}
		for _, l := range loops {
			if l.ComponentRoot != it.ComponentRoot {
				continue
			}
			switch l.Status {
			case "done", "idle", "aborted", "cancelled", "failed":
				continue
			}
			if _, refusal, err := m.d.mqControlCancel(ctx, l.ID, person, "merge-queue: dequeued by "+actor.ID); err != nil {
				return // a fault is the dispatcher's to speak
			} else if refusal != nil {
				m.d.mqRetireByAnswer(ctx, l)
			}
		}
	}
}

func (m *mergeQueueDoors) Dequeue(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	it, err := m.d.store.ReadMergeItem(p.ID)
	if err != nil {
		var mr store.MergeRefusal
		if errors.As(err, &mr) && mr.Kind == "unknown-item" {
			return protocol.ItemView{}, protocol.ErrMergeQueueNotFound.WithWhat("no merge_queue_item row answers id %q", p.ID)
		}
		return protocol.ItemView{}, mqDoorError(err)
	}
	m.mqStopItemLoops(ctx, it, actor)
	svc := mergequeue.NewService(m.d.store)
	fresh, err := svc.Dequeue(p.ID, store.MergeActor{Kind: "user", ID: actor.ID})
	if err != nil {
		return protocol.ItemView{}, mqDoorError(err)
	}
	m.d.mergeQueueWake()
	return mqItemView(fresh), nil
}

func (m *mergeQueueDoors) Reorder(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueReorderParams) (protocol.ItemView, error) {
	svc := mergequeue.NewService(m.d.store)
	it, err := svc.Reorder(p.ID, strings.TrimSpace(p.BeforeID), store.MergeActor{Kind: "user", ID: actor.ID})
	if err != nil {
		return protocol.ItemView{}, mqDoorError(err)
	}
	m.d.mergeQueueWake()
	return mqItemView(it), nil
}

func (m *mergeQueueDoors) Retry(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueIDParams) (protocol.ItemView, error) {
	svc := mergequeue.NewService(m.d.store)
	it, err := svc.Retry(p.ID, store.MergeActor{Kind: "user", ID: actor.ID})
	if err != nil {
		return protocol.ItemView{}, mqDoorError(err)
	}
	m.d.mergeQueueWake()
	return mqItemView(it), nil
}

// Land is the person pushing the lane forward: bound to the row AS READ
// (the digest from show), refused from any state but ready and any mode but
// manual/auto, and delivered through the driver. The `forge.land`
// ask/deny still happens in the loop (S11/S56): this door answers the
// loop's standing land-check gate with its own "merge-now" option — the
// word the person at the socket is meaning — and records the demand.
func (m *mergeQueueDoors) Land(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueLandParams) (protocol.ItemView, error) {
	it, err := m.d.store.ReadMergeItem(p.ID)
	if err != nil {
		var mr store.MergeRefusal
		if errors.As(err, &mr) && mr.Kind == "unknown-item" {
			return protocol.ItemView{}, protocol.ErrMergeQueueNotFound.WithWhat("no merge_queue_item row answers id %q", p.ID)
		}
		return protocol.ItemView{}, mqDoorError(err)
	}
	if want := mqLandDigest(it); p.Digest != want {
		return protocol.ItemView{}, protocol.ErrMergeQueueRefused.WithWhat(
			"the land is bound to the item as it was read: this row now digests %s and the request carries %s — the head or the position moved since mergequeue.show answered; show it again and approve the item you actually saw", want, p.Digest)
	}
	if it.Status != "ready" {
		return protocol.ItemView{}, protocol.ErrMergeQueueConflictState.WithWhat("mergequeue.land belongs to a ready item and %s is %s", it.ID, it.Status)
	}
	cfg := m.d.mqConfig(ctx, it.ComponentRoot)
	if string(cfg.Mode) == "off" {
		return protocol.ItemView{}, protocol.ErrMergeQueueRefused.WithWhat(
			"the queue for %s is dark (mode off): nothing lands from it, even by hand — mergequeue.settings is the door that says otherwise", it.ComponentRoot)
	}
	// Answer the standing land-check gate with its own merge-word, so an
	// attended loop parked on the question continues instead of waiting on
	// an answer that has already been given at the socket.
	if slug := it.LandingLoopSlug; slug != "" {
		if q, found, err := m.d.store.PendingQuestion(store.Scope{ComponentRoot: it.ComponentRoot, LoopSlug: slug}); err == nil && found &&
			q.Kind == string(store.QuestionGate) && !q.Answered() {
			offers := false
			for _, o := range q.Options {
				if strings.EqualFold(strings.TrimSpace(o), "merge-now") {
					offers = true
				}
			}
			// The gate is the landing loop's standing land-check question —
			// a STEP gate at @land-check, so it belongs to the loop plane's
			// answer door, not to a raw store write: that door answers,
			// records any verdict the step decides, and re-dispatches the
			// runner (blueprint.go's AnswerBlueprintLoop tail). The merge
			// queue borrows the mechanism, never the shortcut — an answer
			// nobody woke for is a park that outlives its own approval.
			if offers && m.d.Table != nil && m.d.Table.AnswerBlueprintLoop != nil {
				if loopID, ok := mqLoopIDForSlug(m.d.store, slug, it.ComponentRoot); ok {
					// "Already answered underneath us" returns a refused
					// body, not an error; any real error means the loop
					// moved on its own — the demand still stands.
					_, _ = m.d.Table.AnswerBlueprintLoop(ctx, protocol.LoopAnswerParams{ID: loopID, Answer: "merge-now"})
				}
			}
		}
	}
	m.d.RequestMergeQueueLand(it.ID)
	// The driver moves the row (ready → merging is ITS edge); reconciling
	// synchronously is what lets this door answer with the moved row.
	m.d.reconcileMergeQueue(ctx)
	fresh, err := m.d.store.ReadMergeItem(p.ID)
	if err != nil {
		return protocol.ItemView{}, err
	}
	return mqItemView(fresh), nil
}

// Settings writes one repository's queue settings through T3's one door.
// A change that WIDENS authority (mode to auto, conflict policy to resolve)
// is refused UNTIL the caller resends with `acknowledged` carrying the
// digest of the daemon's own statement sentence — the S49/S50 shape
// (statement first, action on the acknowledgement), never a silent widening.
func (m *mergeQueueDoors) Settings(ctx context.Context, actor protocol.Actor, p protocol.MergeQueueSettingsParams) (protocol.ConfigView, string, error) {
	root := strings.TrimSpace(p.ComponentRoot)
	cur, err := m.d.store.ReadMergeConfig(root)
	if err != nil {
		return protocol.ConfigView{}, "", mqDoorError(err)
	}
	mode, policy, method, catch := cur.Mode, cur.ConflictPolicy, cur.LandMethod, cur.MaxCatchUps
	if cur.Absent {
		mode, policy, method, catch = "manual", "ask", "squash", 3 // the store's defaults for a row never written
	}
	if p.Mode != "" {
		mode = p.Mode
	}
	if p.ConflictPolicy != "" {
		policy = p.ConflictPolicy
	}
	if p.LandMethod != "" {
		method = p.LandMethod
	}
	if p.MaxCatchUps != nil {
		catch = *p.MaxCatchUps
	}
	widening := (mode == "auto" && cur.Mode != "auto") || (policy == "resolve" && cur.ConflictPolicy != "resolve")
	if cur.Absent && (mode == "auto" || policy == "resolve") {
		widening = true // never written means the defaults stood; from them, auto/resolve still widen
	}
	target := mergequeue.Settings{
		ComponentRoot: root, Mode: mergequeue.Mode(mode),
		ConflictPolicy: mergequeue.ConflictPolicy(policy),
		LandMethod:     mergequeue.LandMethod(method), MaxCatchUps: catch,
	}
	if widening {
		sentence := mergequeue.Statement(target)
		digest := mqStatementDigest(sentence)
		if p.Acknowledged != digest {
			return protocol.ConfigView{}, "", protocol.ErrMergeQueueRefused.WithWhat(
				"%s — this widens the queue's authority. Nothing changed. Resend with acknowledged %q to apply the sentence you have now read", sentence, digest)
		}
	}
	cfg, statement, err := mergequeue.NewService(m.d.store).Settings(store.NewMergeConfig{
		ComponentRoot: root, Mode: mode, ConflictPolicy: policy, LandMethod: method, MaxCatchUps: catch,
	}, store.MergeActor{Kind: "user", ID: actor.ID})
	if err != nil {
		return protocol.ConfigView{}, "", mqDoorError(err)
	}
	m.d.mergeQueueWake()
	return protocol.ConfigView{
		ComponentRoot: cfg.ComponentRoot, Mode: cfg.Mode, ConflictPolicy: cfg.ConflictPolicy,
		LandMethod: cfg.LandMethod, MaxCatchUps: cfg.MaxCatchUps,
		UpdatedAt: cfg.UpdatedAt, UpdatedBy: cfg.UpdatedByKind + "/" + cfg.UpdatedByID,
	}, statement, nil
}

// mqLoopIDForSlug resolves a landing-loop slug to the loop row id in this
// root (the answer door speaks loop ids; the queue keeps slugs).
func mqLoopIDForSlug(st *store.Store, slug, root string) (string, bool) {
	rows, err := st.LoopsBySlug(slug)
	if err != nil {
		return "", false
	}
	for _, l := range rows {
		if l.ComponentRoot == root {
			return l.ID, true
		}
	}
	return "", false
}
