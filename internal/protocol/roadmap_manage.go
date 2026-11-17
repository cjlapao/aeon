package protocol

// S50 T2 (RM1, RM6, RM7) — the four management doors: the edit verb and
// the three reads the board was designed around. They ride this file for
// the house rule roadmap.go states (one file owns the verb, its name, its
// params and its handler); `roadmap.list` and friends stay UNTOUCHED —
// the board is an addition, not a replacement (RM6: compat stays).
//
// `roadmap.update` is the eighth roadmap WRITE: the actor is the
// connection's person exactly as the other seven stamp it, the params are
// refused by KEY before anything else (RM1's set is the whole allow-list,
// so an unknown key — including every identity key — is refused naming
// the fields that DO exist), and an edit that changes nothing audits
// nothing and answers changed:false.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/roadmap"
)

const (
	MethodRoadmapBoard      MethodName = "roadmap.board"
	MethodRoadmapHistory    MethodName = "roadmap.history"
	MethodRoadmapUpdate     MethodName = "roadmap.update"
	MethodRoadmapVocabulary MethodName = "roadmap.vocabulary"
)

// ErrRoadmapUnknown is RM6's honest not-found: a read of a row that is
// not there ANSWERS, naming the thing it looked for — the silence of an
// empty list is for filters, not for ids (−31931, ErrRoadmapNoActor's
// sibling in the −3193x roadmap block).
var ErrRoadmapUnknown = &Error{
	Code:    -31932,
	Message: "no roadmap item carries that id",
	Data: Data{
		What:  "the id named no row — roadmap.board and roadmap.list answer the live ids",
		Means: "nothing read, nothing written",
		Todo:  "run roadmap.board, then retry with an id from its rows",
		Fatal: false,
	},
}

// --- roadmap.update -----------------------------------------------------------

// roadmapUpdateFields is RM1's allow-list, spelled once and reused in the
// refusal so the message and the law cannot drift apart.
var roadmapUpdateFields = []string{"id", "title", "epic", "briefPath", "planPath", "clearEpic"}

// RoadmapUpdateParams: every field optional except id; nil means UNSET
// (leave the column alone). clearEpic is the one explicit removal.
type RoadmapUpdateParams struct {
	ID        int64   `json:"id"`
	Title     *string `json:"title,omitempty"`
	Epic      *string `json:"epic,omitempty"`
	BriefPath *string `json:"briefPath,omitempty"`
	PlanPath  *string `json:"planPath,omitempty"`
	ClearEpic bool    `json:"clearEpic,omitempty"`
}

// RoadmapUpdateResult carries the FRESH row either way. Changed:false is
// not an error — naming a field with its current value is a question that
// got answered, and the audit row that did not get written is the point.
type RoadmapUpdateResult struct {
	Item    RoadmapItemView `json:"item"`
	Changed bool            `json:"changed"`
}

// rejectRoadmapUpdateKeys refuses every key outside RM1's set BEFORE
// decoding, naming the key and the fields that exist. The lenient decoder
// would otherwise let `status` ride in silently — silently-ignored params
// are how clients learn the wrong contract.
func rejectRoadmapUpdateKeys(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return ErrBadParams{Err: fmt.Errorf("roadmap.update params must be a JSON object: %w", err)}
	}
	var bad []string
	for k := range bag {
		known := false
		for _, f := range roadmapUpdateFields {
			if k == f {
				known = true
				break
			}
		}
		if !known {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return ErrBadParams{Err: fmt.Errorf(
			"%s refuses the param %q — the edit reaches only %s; %s ride their own verbs (RM1)",
			MethodRoadmapUpdate, bad[0], strings.Join(roadmapUpdateFields, ", "),
			"status, priority, estimate and provenance")}
	}
	return nil
}

func (t *Table) handleRoadmapUpdate(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if err := rejectRoadmapUpdateKeys(raw); err != nil {
		return nil, err
	}
	var p RoadmapUpdateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	actor, err := t.roadmapActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return AnswerRoadmapUpdate(r, p, actor)
}

// AnswerRoadmapUpdate is `roadmap.update`'s ONE answer (S50 T6, rule 4's
// shape): the socket handler above and the CLI's offline door call THIS
// function, so the edit, its refusals and its audit stamp are one
// implementation two doors share (the blueprint rule: no second logic on
// the offline road). The actor arrives as an argument — the socket's
// handler stamps the connection's person; the CLI's direct door stamps
// {cli, cli} and says so. The shape law (a positive id) is checked here
// too, because the offline door has no handler in front of it.
func AnswerRoadmapUpdate(r *roadmap.Roadmap, p RoadmapUpdateParams, actor roadmap.Actor) (any, error) {
	if p.ID <= 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required and must be a positive row id")}
	}
	it, changed, ref := r.Update(p.ID, roadmap.UpdateFields{
		Title: p.Title, Epic: p.Epic, ClearEpic: p.ClearEpic,
		BriefPath: p.BriefPath, PlanPath: p.PlanPath,
	}, actor)
	if ref != nil {
		return nil, roadmapRefusal(ref.Reason)
	}
	v, err := roadmapGraphFields(r, it)
	if err != nil {
		return nil, err
	}
	return RoadmapUpdateResult{Item: v, Changed: changed}, nil
}

// --- roadmap.board -------------------------------------------------------------

// RoadmapBoardParams narrows the whole-board view; all optional (RM6 —
// the board is deliberately the cross-component view roadmap.list is not,
// and the daemon does the grouping, not the UI).
type RoadmapBoardParams struct {
	ComponentRoot string `json:"componentRoot,omitempty"`
	Status        string `json:"status,omitempty"`
	Epic          string `json:"epic,omitempty"`
}

// RoadmapBoardLoopView is the loop an item rode, when it rode one.
type RoadmapBoardLoopView struct {
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

// RoadmapBoardRowView is one item as RM6 draws it: the full item view,
// the Describe sentence, the readiness answer PAIRED with the exact
// sentence that produced it, and the loop block.
type RoadmapBoardRowView struct {
	Item     RoadmapItemView       `json:"item"`
	Describe string                `json:"describe"`
	Ready    bool                  `json:"ready"`
	Blocker  string                `json:"blocker,omitempty"`
	Loop     *RoadmapBoardLoopView `json:"loop,omitempty"`
}

// RoadmapBoardResult: rows in the store's order (status rank, then age),
// groups counting the FILTERED rows — the counts describe what is shown.
type RoadmapBoardResult struct {
	Rows   []RoadmapBoardRowView `json:"rows"`
	Groups map[string]int        `json:"groups"`
}

func (t *Table) handleRoadmapBoard(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p RoadmapBoardParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	// S50 T5 (rule 4): the shared answer — the step's roadmap_list twin
	// runs this same function, so the board the person sees and the board
	// the step reads are one rendering, not two that agree.
	return AnswerRoadmapBoard(r, p)
}

// --- roadmap.history ------------------------------------------------------------

type RoadmapHistoryParams struct {
	ID    int64 `json:"id"`
	Limit int   `json:"limit,omitempty"`
}

// RoadmapHistoryView is one audit row as the wire carries it — the trail
// is facts already decided; the view reshapes nothing about them.
type RoadmapHistoryView struct {
	ID         int64  `json:"id"`
	ItemID     int64  `json:"itemId"`
	Op         string `json:"op"`
	Summary    string `json:"summary"`
	ActorKind  string `json:"actorKind"`
	ActorID    string `json:"actorId"`
	OccurredAt string `json:"occurredAt"`
}

type RoadmapHistoryResult struct {
	Changes []RoadmapHistoryView `json:"changes"`
}

func (t *Table) handleRoadmapHistory(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p RoadmapHistoryParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	r, err := t.requireRoadmap()
	if err != nil {
		return nil, err
	}
	// S50 T5 (rule 4): the shared answer — the step's roadmap_show twin
	// gets this same page beside the item.
	return AnswerRoadmapHistory(ctx, r, p)
}

// --- roadmap.vocabulary -----------------------------------------------------------

// roadmap.health (RM6) — the settings' standing, not a gate. The daemon
// answers over the two blueprint settings: the EFFECTIVE name, and whether
// the named blueprint is ok / missing / invalid in the source right now —
// or unverifiable when the daemon cannot see the source at all (an unwired
// S48 never reads ok). A read: it answers, it never blocks a dispatch;
// dispatch checks the same facts at its own moment (RM3's refuse-not-
// fallback), which is why a green health line and a refused dispatch can
// honestly coexist one second apart.
type RoadmapHealthCounts struct {
	Valid        int `json:"valid"`
	Invalid      int `json:"invalid"`
	Unverifiable int `json:"unverifiable"`
}

type RoadmapHealthSetting struct {
	Key          string               `json:"key"`
	Name         string               `json:"name"`
	State        string               `json:"state"` // ok | missing | invalid | unverifiable
	ReportCounts *RoadmapHealthCounts `json:"reportCounts,omitempty"`
}

type RoadmapHealthResult struct {
	Settings    []RoadmapHealthSetting `json:"settings"`
	EvaluatedAt string                 `json:"evaluatedAt"`
}

func (t *Table) handleRoadmapHealth(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if len(raw) > 0 && string(raw) != "{}" && string(raw) != "null" {
		return nil, ErrBadParams{Err: fmt.Errorf("roadmap.health takes no params — it answers over the whole settings plane, and a filter would imply it gates something (RM6)")}
	}
	if t.RoadmapHealth == nil {
		return nil, ErrStoreUnavailable
	}
	return t.RoadmapHealth(ctx)
}

func (t *Table) handleRoadmapVocabulary(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	if len(raw) > 0 && string(raw) != "null" && string(raw) != "{}" {
		return nil, ErrBadParams{Err: fmt.Errorf("roadmap.vocabulary takes no params — the vocabulary is the daemon's, not a view of yours")}
	}
	if _, err := t.requireRoadmap(); err != nil {
		return nil, err
	}
	return roadmap.Vocabulary(), nil
}

// --- registration (rids registerRoadmap) ------------------------------------------

func (t *Table) registerRoadmapManage() {
	t.register(&Method{Name: MethodRoadmapBoard, RequiresInitialize: true, Handle: t.handleRoadmapBoard})
	t.register(&Method{Name: MethodRoadmapHistory, RequiresInitialize: true, Handle: t.handleRoadmapHistory})
	t.register(&Method{Name: MethodRoadmapUpdate, RequiresInitialize: true, Handle: t.handleRoadmapUpdate})
	t.register(&Method{Name: MethodRoadmapVocabulary, RequiresInitialize: true, Handle: t.handleRoadmapVocabulary})
	// roadmap.health (RM6): the daemon answers over its own settings; the
	// seam (not t.Roadmap) carries it, because the facts are the daemon's —
	// the store holds the settings and the blueprint rows, the reading is
	// the daemon's per-moment discipline.
	t.register(&Method{Name: MethodRoadmapHealth, RequiresInitialize: true, Handle: t.handleRoadmapHealth})
}
