package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// S38 T15 (OL10/OL11): the human's half of the two doors. The catalog's
// socket projection and the five person-only methods arrive here as one
// adapter over the SAME RecordsService the tools call — one definition, two
// doors — and the identity rule lives on this side of the seam: the actor is
// the uid the KERNEL reported for the connecting process (P26), stamped by
// the daemon. Params never carry one (decode refuses the field, and the
// store refuses a non-human kind).

// socketRecords is the protocol.RecordsAPI the method table calls.
type socketRecords struct {
	d   *Daemon
	svc *RecordsService
}

func newSocketRecords(d *Daemon, svc *RecordsService) *socketRecords {
	return &socketRecords{d: d, svc: svc}
}

// actor names the person behind a connection. The uid is the kernel's, not
// a claim: localrpc reports it per connection, and a connection the
// transport never recorded answers uid 0 — the daemon's own user, which is
// the only person a storeless mount can honestly name.
func (r *socketRecords) actor(sess *protocol.Session) store.Actor {
	uid, _, _ := r.d.peer(sess.ConnID)
	return store.Actor{Kind: "user", ID: strconv.Itoa(uid)}
}

func (r *socketRecords) Do(ctx context.Context, sess *protocol.Session, tool string, params json.RawMessage) (any, string, error) {
	out, f, err := r.svc.Do(ctx, Actor{Kind: "user", ID: strconv.Itoa(r.uid(sess))}, tool, params)
	if f != nil {
		return nil, f.Sentence, err
	}
	return out, "", err
}

func (r *socketRecords) uid(sess *protocol.Session) int {
	uid, _, _ := r.d.peer(sess.ConnID)
	return uid
}

func (r *socketRecords) DecideGoal(ctx context.Context, sess *protocol.Session, p protocol.GoalDecideParams) (protocol.DecideView, string, error) {
	res, rf, err := r.d.store.DecideGoal(p.ID, p.Version, p.Decision, r.actor(sess), p.Reason)
	return decideAnswer(res, rf, err)
}

func (r *socketRecords) DecideBrief(ctx context.Context, sess *protocol.Session, p protocol.BriefDecideParams) (protocol.DecideView, string, error) {
	// Slug OR id (S38 T16: `aeon brief decide --loop SLUG`): one resolver for
	// every person-side loop coordinate, so brief.show and brief.decide can
	// never disagree about what a slug means.
	sc, f := r.svc.personLoopScope(p.LoopID, p.Component)
	if f != nil {
		return protocol.DecideView{}, f.Sentence, nil
	}
	res, rf, err := r.d.store.DecideBrief(sc, p.Version, p.Decision, r.actor(sess), p.Reason)
	return decideAnswer(res, rf, err)
}

func (r *socketRecords) DecideTask(ctx context.Context, sess *protocol.Session, p protocol.TaskDecideParams) (protocol.DecideView, string, error) {
	before, err := r.d.store.GetTaskByID(p.ID)
	if err != nil {
		return protocol.DecideView{}, fmt.Sprintf("refused: task %q is not a task this daemon has (%v)", p.ID, err), nil
	}
	res, rf, err := r.d.store.DecideTask(p.ID, p.Decision, r.actor(sess), p.Reason)
	if err != nil || rf != nil {
		return protocol.DecideView{}, refusalSentence(rf), err
	}
	if res.Changed {
		// The board moves only AFTER the ledger row committed — an event a
		// rolled-back decision would have announced is a lie already in the
		// log. At-most-once until S40's outbox (T15): the daemon that dies
		// between commit and publish loses the notification, never the row.
		after, aerr := r.d.store.GetTaskByID(p.ID)
		if aerr == nil && r.d.Table != nil {
			r.d.emitTaskChanged(after, before.Status)
		}
	}
	return decideAnswer(res, rf, err)
}

func decideAnswer(res store.DecideResult, rf *store.Refusal, err error) (protocol.DecideView, string, error) {
	if err != nil {
		return protocol.DecideView{}, "", err
	}
	if rf != nil {
		return protocol.DecideView{}, rf.Sentence, nil
	}
	return protocol.DecideView{Changed: res.Changed, State: res.State, DecidedBy: res.DecidedBy, DecidedAt: res.DecidedAt}, "", nil
}

func refusalSentence(rf *store.Refusal) string {
	if rf == nil {
		return ""
	}
	return rf.Sentence
}

// --- the board read ----------------------------------------------------------

func (r *socketRecords) TaskList(ctx context.Context, sess *protocol.Session, p protocol.TaskListParams) (protocol.TaskListResult, error) {
	f := store.TaskFilter{
		Root:          p.Root,
		AssigneeKind:  p.AssigneeKind, Assignee: p.Assignee, Status: p.Status,
		Type: p.Type, Manual: p.Manual, ApprovalState: p.ApprovalState,
		GoalID: p.Goal, Limit: p.Limit,
	}
	if p.Component != "" {
		id, ok := r.resolveComponent(p.Component, f.Root)
		if !ok {
			return protocol.TaskListResult{Tasks: []protocol.TaskView{}}, nil
		}
		f.ComponentID = id
	}
	rows, err := r.d.store.ListTaskRows(f)
	if err != nil {
		return protocol.TaskListResult{}, err
	}
	out := protocol.TaskListResult{Tasks: make([]protocol.TaskView, 0, len(rows))}
	for _, row := range rows {
		out.Tasks = append(out.Tasks, taskView(row))
	}
	return out, nil
}

func taskView(row store.TaskRow) protocol.TaskView {
	slugs := row.LoopSlugs
	if slugs == nil {
		slugs = []string{}
	}
	return protocol.TaskView{
		ID: row.ID, TaskID: row.TaskID, LoopSlug: row.LoopSlug, ComponentRoot: row.ComponentRoot,
		ComponentID: row.ComponentID, Title: row.Title, Type: row.Type, Tech: row.Tech,
		Status: row.Status, Manual: row.Manual, AssigneeKind: row.AssigneeKind, Assignee: row.Assignee,
		GoalID: row.GoalID, ApprovalState: row.ApprovalState, DependsOn: row.DependsOn,
		LoopSlugs: slugs, CostUSD: row.CostUSD, CostIsFloor: row.CostIsFloor,
		CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// resolveComponent accepts a workspace root, a component name or slug, or an
// id — the three ways a person points at one repo. projectID is a root: when
// set, only that root's members answer.
func (r *socketRecords) resolveComponent(nameOrRoot, projectID string) (string, bool) {
	if c, err := r.d.store.ComponentForRoot(nameOrRoot); err == nil {
		return c.ID, true
	}
	comps, err := r.d.store.ListComponents(store.ComponentFilter{})
	if err != nil {
		return "", false
	}
	for _, c := range comps {
		if projectID != "" && c.ParentID != projectID {
			continue
		}
		if c.ID == nameOrRoot || c.Name == nameOrRoot || c.Slug == nameOrRoot {
			return c.ID, true
		}
	}
	return "", false
}

// --- the goal's whole picture -------------------------------------------------

func (r *socketRecords) GoalShow(ctx context.Context, sess *protocol.Session, p protocol.GoalShowParams) (protocol.GoalShowResult, string, error) {
	g, err := r.d.store.GetGoal(p.ID)
	if err != nil {
		return protocol.GoalShowResult{}, fmt.Sprintf("refused: goal %q is not a goal this daemon has (%v)", p.ID, err), nil
	}
	res := protocol.GoalShowResult{
		Goal: protocol.GoalView{
			ID: g.ID, RootID: g.ComponentID, EpicID: g.EpicID, Version: g.Version, Title: g.Title,
			Type: g.Type, BodyNote: g.BodyNote, SourceKind: g.SourceKind, SourceRef: g.SourceRef,
			SourceLoop: g.SourceLoop, ReviewState: g.ReviewState, CreatedByKind: g.CreatedByKind,
			CreatedByID: g.CreatedByID, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		},
		Components:  []protocol.GoalComponentView{},
		Documents:   []protocol.GoalDocumentView{},
		Attachments: []protocol.AttachmentView{},
		Reviews:     []protocol.ReviewView{},
		Loops:       []protocol.LoopStatusView{},
	}
	for _, c := range g.Components {
		res.Components = append(res.Components, protocol.GoalComponentView{ComponentID: c.ComponentID, Confidence: c.Confidence, Note: c.Rationale})
	}
	docs, err := r.d.store.GoalDocuments(g.ID)
	if err != nil {
		return protocol.GoalShowResult{}, "", err
	}
	for _, d := range docs {
		res.Documents = append(res.Documents, protocol.GoalDocumentView{Name: d.File, Rev: d.Rev, Bytes: d.Bytes,
			LoopSlug: d.LoopSlug, ComponentRoot: d.ComponentRoot, WrittenBy: d.WrittenBy, WrittenAt: d.WrittenAt})
	}
	atts, err := r.d.store.ListAttachments("goal", g.ID)
	if err != nil {
		return protocol.GoalShowResult{}, "", err
	}
	for _, a := range atts {
		res.Attachments = append(res.Attachments, protocol.AttachmentView{
			ID: a.ID, SubjectKind: a.SubjectKind, SubjectID: a.SubjectID, Document: a.Document,
			Rev: a.Rev, Currency: a.Currency, HeadRev: a.HeadRev, CreatedAt: a.CreatedAt,
		})
	}
	reviews, err := r.d.store.ListReviews("goal", g.ID)
	if err != nil {
		return protocol.GoalShowResult{}, "", err
	}
	for _, rv := range reviews {
		res.Reviews = append(res.Reviews, protocol.ReviewView{
			ID: rv.ID, SubjectKind: rv.SubjectKind, SubjectID: rv.SubjectID, SubjectVersion: rv.SubjectVersion,
			Decision: rv.Decision, ByKind: rv.ByKind, ByID: rv.ByID, Reason: rv.Reason, DecidedAt: rv.DecidedAt,
		})
	}
	loops, cost, err := r.goalLoopsAndCost(g.ComponentID, g.ID)
	if err != nil {
		return protocol.GoalShowResult{}, "", err
	}
	res.Loops = loops
	res.ExecutionState = protocol.DeriveExecutionState(loops)
	res.Cost = cost
	return res, "", nil
}

// goalLoopsAndCost walks the goal's project for the loops that name it, and
// sums their steps ONCE — a cost is composed from the rows, never stored
// beside the goal (the guard reads the schema to keep that promise).
func (r *socketRecords) goalLoopsAndCost(projectID, goalID string) ([]protocol.LoopStatusView, protocol.GoalCost, error) {
	comps, err := r.d.store.ListComponents(store.ComponentFilter{ParentID: projectID})
	if err != nil {
		return nil, protocol.GoalCost{}, err
	}
	loops := []protocol.LoopStatusView{}
	seen := map[string]bool{}
	cost := protocol.GoalCost{}
	for _, c := range comps {
		rows, err := r.d.store.ListLoops(store.ComponentScope{ComponentRoot: c.Root})
		if err != nil {
			return nil, protocol.GoalCost{}, err
		}
		for _, l := range rows {
			if l.GoalID != goalID || seen[l.ID] {
				continue
			}
			seen[l.ID] = true
			view := protocol.LoopStatusView{ID: l.ID, Slug: l.Slug, ComponentRoot: l.ComponentRoot, Status: l.Status}
			if bp, err := r.d.store.LoopBlueprintID(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot}); err == nil {
				view.Blueprint = bp
			}
			loops = append(loops, view)
			// The floor, honestly composed (P32): known steps add their
			// figure; a partial step adds its lower bound and flags the sum
			// a floor; an unpriced step flags it too — cost that hides is
			// cost the total does not include.
			crows, err := r.d.store.LoopCostRows(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot})
			if err != nil {
				return nil, protocol.GoalCost{}, err
			}
			addCostRows(&cost, crows)
		}
	}
	return loops, cost, nil
}

func addCostRows(cost *protocol.GoalCost, rows []model.CostRow) {
	for _, row := range rows {
		switch row.Cost.State {
		case model.CostKnown:
			cost.USD += row.Cost.USD
		case model.CostPartial:
			cost.USD += row.Cost.USDAtLeast
			cost.IsFloor = true
		default:
			cost.IsFloor = true
		}
	}
}

// emitTaskChanged is the board's one kind, published by the daemon AFTER the
// store call returned (T15: never inside the transaction — an event for a
// write that rolled back is a lie already in the log).
func (d *Daemon) emitTaskChanged(after store.Task, previous string) {
	if d.Table == nil {
		return
	}
	payload := protocol.TaskChangedPayload{
		ID: after.ID, Manual: after.Manual, Status: after.Status, Previous: previous,
		Assignee: after.Assignee, LoopSlug: after.LoopSlug, ComponentID: after.ComponentID,
	}
	d.publish(protocol.KindTaskChanged, after.ComponentRoot, payload)
}
