package daemon

// S29 T3/T6/T4 — the triage's daemon side: the dispatch, the hand-off, and
// the dedup, all riding doors that already exist. Three seams and one
// reactor, filled in initTriage beside initRoadmap:
//
//   - validate (T3) starts an ORDINARY loop through the daemon's own
//     loop.start door, G14-pinned, with a prompt that reads the bug's
//     title and body and attempts to reproduce it. Nothing about running
//     that loop is new; the bug's embedding is stored beside it so a later
//     suggestion (T4) can rank. The bug stays unvalidated until the reactor
//     reacts to the loop's own terminal event — the attempt does not
//     pre-label the claim.
//   - handoff (T6) is S26's door: a validated, prioritized bug with no
//     roadmap_item_id yet creates an `idea`-status roadmap item
//     (provenance 'triaged') through internal/roadmap's own Create, and
//     kicks its ideation through the roadmap's own seam. Zero changes to
//     S26 — this is the one that was designed to need none.
//   - suggest (T4) is a READ: it computes the bug's embedding (the one
//     model concern the triage door itself must never reach), stores it,
//     and returns the nearest-neighbour candidates within the bug's OWN
//     project. No side effect — the detector never writes duplicate_of.
//   - the reactor is the terminal consumer, joined at publishLoop exactly
//     where the roadmap's and queue's join (the S24 T3 precedent carried):
//     a validation attempt's loop finishing moves the bug out of
//     unvalidated based on the attempt's outcome; an untracked loop is not
//     this reactor's business.
//
// The embedding here is a deterministic stand-in for the model's — the
// dedup MECHANISM (project-scoped vec query, suggest-only) is what this
// slice owns; the embedding QUALITY is a model/provider question outside
// it (design's own words, DEMO's own caveat). The seam is where a real
// embedding route would land.

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/triage"
)

// S51 T3 (TM4): the validation lane's name was a constant here; it is now
// the DECLARED DEFAULT of triage.validationBlueprint (its key is the const
// in triage_settings.go), read per attempt. grep finds no blueprint-name
// constant in this file on purpose: the registry is the only place that
// name is spelled.

// initTriage fills the Table's triage seams — called from initBlueprints
// beside initRoadmap. Nil-safe by construction: a daemon built without this
// call leaves the triage seams nil, and the methods answer
// ErrStoreUnavailable, loud, rather than pretending the table is empty.
func (d *Daemon) initTriage() {
	// S51 T3 (TM4): the settings seam answers per attempt (the closure
	// reads d.settings at CALL time, never a captured value), and the
	// health door is the daemon's own read — wired BEFORE the store
	// guard, because an honest unverifiable needs no store to say it.
	d.triageSettingView = func(ctx context.Context, key string) (config.View, error) {
		if d.settings != nil {
			return d.settings.Get(ctx, key)
		}
		decl, ok := config.DefaultRegistry().Lookup(key)
		if !ok {
			return config.View{}, fmt.Errorf("the registry carries no %q", key)
		}
		return config.View{Setting: decl, Value: decl.Default, Source: "default"}, nil
	}
	if d.Table != nil {
		d.Table.TriageHealth = d.triageHealth
	}
	if d.Table == nil || d.store == nil {
		return
	}
	d.Table.Triage = triage.New(d.store)
	d.Table.TriageValidate = func(ctx context.Context, p protocol.TriageValidateParams) (any, error) {
		return d.triageValidate(ctx, p)
	}
	d.Table.TriageHandoff = func(ctx context.Context, a triage.Actor, p protocol.TriageHandoffParams) (any, error) {
		return d.triageHandoff(ctx, a, p)
	}
	d.Table.TriageSuggest = func(ctx context.Context, p protocol.TriageSuggestParams) (any, error) {
		return d.triageSuggest(ctx, p)
	}
	// S51 T2: the board read (TM5) — attempts from the audit, SCORED
	// candidates from the vec index, hand-off status from the roadmap
	// rows. Read-only by construction (triage_board.go says why).
	d.Table.TriageBoard = func(ctx context.Context, p protocol.TriageBoardParams) (any, error) {
		return d.triageBoard(ctx, p)
	}
	// S51 T5 (TM6): the records arms answer through the SAME plane and the
	// VERY SAME closure the socket seam holds — one implementation, two
	// doors (rule 4). The service exists before initTriage (daemon.go
	// wires recordsSvc in the same boot); a mount without it has no
	// records surface at all, so there is nothing to wire.
	if d.recordsSvc != nil {
		d.recordsSvc.triagePlane = d.Table.Triage
		d.recordsSvc.triageBoard = d.triageBoard
	}
}

// triageValidate dispatches the validation attempt (T3): a bug that is
// already validated is refused (an attempt that ran cannot unrun, D1), and
// a fresh one gets its embedding stored (so a later suggestion can rank)
// and its attempt loop started through the daemon's own loop.start door,
// G14-pinned, with a prompt that reads the bug. The bug stays unvalidated
// — the reactor moves it, on the loop's own terminal.
func (d *Daemon) triageValidate(ctx context.Context, p protocol.TriageValidateParams) (any, error) {
	bug, ok, err := d.Table.Triage.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("triage: bug %d does not exist", p.ID)
	}
	if bug.Validation != triage.ValidationUnvalidated {
		return nil, fmt.Errorf("triage: bug %d is already %q — an attempt that ran cannot unrun (D1); the bug's state moves only out of unvalidated", p.ID, bug.Validation)
	}
	// S51 T3 (TM4): the blueprint name is read PER CALL through the
	// settings seam and checked against the daemon's own source (S48)
	// BEFORE anything else happens — a missing or invalid choice refuses
	// the attempt by the setting's name and runs nothing, falling back to
	// no one. (No side effect precedes this: not even the embedding.)
	blueprintName, _, err := d.triageBlueprintChoice(ctx)
	if err != nil {
		return nil, err
	}
	// store the embedding beside the bug, so a later suggestion can rank
	// (T4). A bug with no embedding yet is a normal state; this is what
	// makes it one. The embedding is a stand-in for the model's (see the
	// file header) — the seam is where a real route lands.
	if err := d.Table.Triage.StoreEmbedding(bug, triageEmbedDim, triageEmbedding(bug.Title+" "+bug.Body)); err != nil {
		d.log.Infof("triage: bug %d's embedding did not store: %v — the bug stays unvalidated, not a silent no-op", p.ID, err)
	}
	if d.Table == nil || d.Table.StartBlueprintLoop == nil {
		return nil, fmt.Errorf("the daemon has no loop.start door wired")
	}
	slug := fmt.Sprintf("triage-validate-%d", bug.ID)
	_, err = d.Table.StartBlueprintLoop(ctx, protocol.LoopStartParams{
		Blueprint:     blueprintName,
		Goal:          fmt.Sprintf("validate the bug %q: attempt to reproduce it, then report reproduced / not-reproduced / could-not-tell", bug.Title),
		WorkspaceRoot: bug.ComponentRoot,
		Slug:          slug, // deterministic: a re-dispatch while a run is in flight collides on the loop's own key
		With: map[string]string{
			"bug_id": strconv.FormatInt(bug.ID, 10),
			"role":   "validate",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("triage: bug %d started no validation attempt — %v; if a loop for this bug DOES exist this is the truth, otherwise the attempt waits for a person", p.ID, err)
	}
	return map[string]any{
		"bug":    triageViewMap(bug),
		"loopId": slug,
		"note":   "the validation attempt loop is started; the bug stays unvalidated until it finishes (the reactor moves it, on the loop's own terminal)",
	}, nil
}

// triageHandoff is D8 (T6): a validated, prioritized bug with no roadmap
// item yet becomes an `idea`-status S26 roadmap item (provenance
// 'triaged'), through internal/roadmap's own Create, and its ideation is
// kicked through the roadmap's own seam. Zero changes to S26 — the whole
// point of the hand-off's design.
func (d *Daemon) triageHandoff(ctx context.Context, a triage.Actor, p protocol.TriageHandoffParams) (any, error) {
	bug, ok, err := d.Table.Triage.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("triage: bug %d does not exist", p.ID)
	}
	if bug.RoadmapItemID != nil {
		return nil, fmt.Errorf("triage: bug %d already handed off to roadmap item %d — one hand-off per bug (D8)", p.ID, *bug.RoadmapItemID)
	}
	if !bug.Validated() {
		return nil, fmt.Errorf("triage: bug %d is %q — the hand-off (D8) needs a validated bug; an unvalidated claim has nothing to hand off", p.ID, bug.Validation)
	}
	if bug.PriorityState == triage.PriorityUnknown {
		return nil, fmt.Errorf("triage: bug %d is unknown priority — the hand-off (D8) needs a known or assumed priority; an unknown priority never hands off by waiting (D2)", p.ID)
	}
	// the hand-off rides S26's OWN door — its Create enters at `idea`
	// (R1's start) and the roadmap's own ideation seam kicks the loop.
	// Nothing here creates a second roadmap; this is the one that was
	// designed to need none.
	r := roadmap.New(d.store)
	// The triage pipeline creates the item, not a person at the socket —
	// the honest actor is the system that wrote it (S50 T1, RM2).
	item, ref := r.CreateAudited(bug.ComponentRoot, roadmap.NewItem{
		Title:         bug.Title,
		Priority:      roadmap.PriorityState(bug.PriorityState),
		PriorityLabel: bug.PriorityLabel,
		Provenance:    roadmap.ProvenanceTriaged,
	}, roadmap.Actor{Kind: "system", ID: "triage"})
	if ref != nil {
		return nil, fmt.Errorf("triage: the S26 hand-off was refused — %s", ref.Reason)
	}
	// record the hand-off on the bug (the one write that says it happened),
	// audited as the person's act — the record and its bug_change row commit
	// in one transaction (S51 T1, TM2).
	if _, ref := d.Table.Triage.RecordHandoffAudited(bug.ID, bug.ComponentRoot, item.ID, a); ref != nil {
		return nil, fmt.Errorf("triage: the hand-off landed but the record could not be written: %s", ref.Reason)
	}
	// kick the ideation through the roadmap's OWN seam (T3's D1), not a
	// second one.
	if d.Table.RoadmapIdeate != nil {
		if _, err := d.Table.RoadmapIdeate(ctx, roadmap.Actor{Kind: "system", ID: "triage"}, protocol.RoadmapIdeateParams{ID: item.ID, WorkspaceRoot: bug.ComponentRoot}); err != nil {
			d.log.Infof("triage: bug %d's hand-off to roadmap item %d is recorded but its ideation did not kick: %v — the item stays idea, a person can kick it", bug.ID, item.ID, err)
		}
	}
	return map[string]any{
		"bug":         triageViewMap(bug),
		"roadmapItem": item.ID,
	}, nil
}

// triageSuggest is T4's read: compute the bug's embedding (the one model
// concern the triage door never reaches), store it, and return the
// nearest-neighbour candidates within the bug's OWN project. No side
// effect — the detector never writes duplicate_of (D5/N3).
func (d *Daemon) triageSuggest(ctx context.Context, p protocol.TriageSuggestParams) (any, error) {
	bug, ok, err := d.Table.Triage.Show(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("triage: bug %d does not exist", p.ID)
	}
	vec := triageEmbedding(bug.Title + " " + bug.Body)
	if err := d.Table.Triage.StoreEmbedding(bug, triageEmbedDim, vec); err != nil {
		d.log.Infof("triage: bug %d's embedding did not store: %v — the suggestion answers an empty list, not a silent no-op", p.ID, err)
	}
	cands, err := d.Table.Triage.SuggestCandidates(bug, triageEmbedDim, vec, p.Limit)
	if err != nil {
		return nil, err
	}
	views := make([]any, 0, len(cands))
	for _, c := range cands {
		views = append(views, triageViewMap(c))
	}
	return views, nil
}

// --- the reactor (terminal consumer, joined at publishLoop) -------------------

// onTriageTerminal is the subscription, called from publishLoop at the
// daemon's own emission point beside onRoadmapTerminal. It resolves nothing
// on the caller's thread — dispatch to the goroutine is the whole function
// (a triage reaction must never delay the step that just finished).
func (d *Daemon) onTriageTerminal(kind, componentRoot, loopSlug string, fields map[string]any) {
	if d.store == nil {
		return
	}
	status, _ := fields["status"].(string)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				d.log.Infof("triage: the terminal reactor for loop %s recovered from %v — the bug's own row remains the truth (P29)", loopSlug, rec)
			}
		}()
		d.handleTriageTerminal(context.Background(), kind, componentRoot, loopSlug, status)
	}()
}

// handleTriageTerminal does the deciding: a validation attempt's loop
// finishing moves the bug out of unvalidated, based on the attempt's
// outcome. The bug id is read from the loop's `with` pin (the same channel
// roadmap reads); a loop carrying no triage link is not this reactor's
// business. The outcome reads the attempt's carried verdict, defaulting to
// `could-not-tell` (the honest "an attempt ran and did not land a
// definitive reproduced/not-reproduced") — never a silent collapse into
// `reproduced`.
func (d *Daemon) handleTriageTerminal(ctx context.Context, kind, componentRoot, loopSlug, status string) {
	if kind != protocol.KindLoopFinished && kind != protocol.KindLoopBlocked {
		return
	}
	if d.Table == nil || d.Table.Triage == nil {
		return
	}
	bugID := triageBugIDFromSlug(loopSlug)
	if bugID <= 0 {
		return // an untracked loop (no triage link) is not this reactor's business
	}
	bug, ok, err := d.Table.Triage.Show(bugID)
	if err != nil || !ok {
		d.log.Infof("triage: the terminal reactor could not read bug %d: %v", bugID, err)
		return
	}
	if bug.Validation != triage.ValidationUnvalidated {
		return // the bug already moved out of unvalidated; a second attempt is a new record (D3), not a re-move
	}
	if kind == protocol.KindLoopBlocked {
		// a blocked attempt leaves the bug unvalidated — a person decides;
		// nothing here silently advances or reverts (the roadmap's D6).
		return
	}
	// the attempt finished; move the bug out of unvalidated. The outcome
	// reads the attempt's verdict if it carried one, else defaults to
	// could-not-tell — the honest "an attempt ran and did not land a
	// definitive reproduced / not-reproduced".
	to := triageValidationFromStatus(status)
	// TM2: the reactor's own move, audited as {system, reactor} — a person
	// reading the bug's history sees WHAT MOVED ON ITS OWN, named.
	if _, ref := d.Table.Triage.TransitionAudited(bugID, to, triage.SystemReactor); ref != nil {
		d.log.Infof("triage: bug %d did not move to %q after its validation attempt finished: %s", bugID, to, ref.Reason)
		return
	}
	d.log.Infof("triage: bug %d moved to %q after its validation attempt loop finished", bugID, to)
	// D10: the attempt's conclusion is filed to S28 — one observed,
	// project-scoped learning, automatic (the feed is a caller of
	// internal/learnings.Append, not a contributor to it). A feed refusal
	// is spoken, not fatal — the bug's own move is the truth; a learning
	// that could not be written is a learning not written, not a bug that
	// did not move.
	if _, ref := d.feedValidationLearning(bug, to); ref != nil {
		d.log.Infof("triage: bug %d's validation conclusion did not file as a learning: %s", bugID, ref.Reason)
	}
}

// triageBugIDFromSlug reads the bug id from a `triage-validate-<id>` slug —
// the deterministic key triageValidate chose, the retry guard.
func triageBugIDFromSlug(slug string) int64 {
	const prefix = "triage-validate-"
	if len(slug) <= len(prefix) || slug[:len(prefix)] != prefix {
		return 0
	}
	id, err := strconv.ParseInt(slug[len(prefix):], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// triageValidationFromStatus maps a finished attempt's status onto a
// validation state. A generic completion (done) is the honest
// could-not-tell — the attempt ran but did not carry a definitive
// reproduced / not-reproduced verdict. The specific verdicts ride the
// attempt's report, not the loop's own status.
func triageValidationFromStatus(status string) triage.ValidationState {
	switch status {
	case "reproduced":
		return triage.ValidationReproduced
	case "not-reproduced":
		return triage.ValidationNotReproduced
	default:
		return triage.ValidationCouldNotTell
	}
}

// triageEmbedDim is the validation attempt's embedding dimension — a
// stand-in for the model's (the seam is where a real route lands), fixed
// here so the vec table name is stable across a project's bugs.
const triageEmbedDim = 8

// triageEmbedding is the stand-in embedding: a deterministic fnv-fold of
// the bug's text into the fixed dimension. It is NOT the model's embedding
// (quality is a model/provider question outside this slice — the design's
// own caveat); it is the seam's shape so the project-scoped vec query and
// the suggest-only discipline are real and testable, not faked.
func triageEmbedding(text string) []float32 {
	vec := make([]float32, triageEmbedDim)
	h := fnv.New32a()
	for i, c := range []byte(text) {
		_ = i
		h.Write([]byte{c})
	}
	sum := h.Sum32()
	for i := range vec {
		// a stable, non-zero vector per text; the vec table ranks by it.
		vec[i] = float32((sum>>uint(i*4))&0xff) / 255.0
	}
	return vec
}

// triageViewMap is the daemon's ONE bug→map shape for the action results
// (validate/handoff/suggest): validation and priority as TWO separate
// fields (D2), never one combined badge (R4's guard the renderer obeys by
// having two fields to render). A bug with no duplicate_of or no
// roadmap_item_id renders the honest absence, not a zero. It mirrors the
// protocol's triageView so the two transports show the same bug the same
// way.
func triageViewMap(b triage.Bug) map[string]any {
	m := map[string]any{
		"id":            b.ID,
		"componentRoot": b.ComponentRoot,
		"title":         b.Title,
		"body":          b.Body,
		"validation":    string(b.Validation),
		"priorityState": string(b.PriorityState),
		"filedBy":       b.FiledBy,
		"validated":     b.Validated(),
		"createdAt":     b.CreatedAt,
		"updatedAt":     b.UpdatedAt,
	}
	if b.PriorityLabel != "" {
		m["priorityLabel"] = b.PriorityLabel
	}
	if b.DuplicateOf != nil {
		m["duplicateOf"] = *b.DuplicateOf
	}
	if b.RoadmapItemID != nil {
		m["roadmapItemId"] = *b.RoadmapItemID
	}
	return m
}
