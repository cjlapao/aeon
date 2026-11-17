package protocol

// S24 T7 — the `queue.*` socket methods. CG9 is the named departure from
// PL15: queue authoring is socket-writable, because a queue item is
// ordinary project content at the same trust tier as `loop.start`'s own
// params. Every handler here marshals around the SAME internal/queue
// calls the CLI dials — "CLI vs socket" is a transport choice, never two
// authoring paths (design.md CG9), which is why there is no validation
// logic in this file beyond parameter shape.
//
// Unlike policy's four read-only methods, these are writes by design;
// the read shape is `queue.list`, which reads rows it never authored.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/queue"
)

// --- the names ---------------------------------------------------------------
//
// These ride here with their handlers (policy's live in method.go only for
// age; registration for the queue is registerQueue below, so one file owns
// the verb, its name, its params and its handler — nothing to forget to
// update twice).

const (
	MethodQueueAppend   MethodName = "queue.append"
	MethodQueueList     MethodName = "queue.list"
	MethodQueueReorder  MethodName = "queue.reorder"
	MethodQueueRemove   MethodName = "queue.remove"
	MethodQueueSetSize  MethodName = "queue.setSize"
	MethodQueueSettings MethodName = "queue.settings"
	MethodQueueAdvance  MethodName = "queue.advance"
)

// ErrQueueRefused is the one queue-flavoured error: a state refusal with
// its reason carried in `what` (T2's discipline — a refusal names the
// reason AND the thing refused). Not-found rides it too: the socket does
// not answer a missing row with a silence.
var ErrQueueRefused = &Error{
	Code:    -31700,
	Message: "the queue refused the act",
	Data: Data{
		What:  "the item's status, the project's mode, or the act itself broke a queue law",
		Means: "nothing moved — the row keeps the state it had",
		Todo:  "read the reason in `what`, then queue.list to see the board as the daemon sees it",
		Fatal: false,
	},
}

func queueRefusal(reason string) *Error {
	return ErrQueueRefused.WithWhat("%s", reason)
}

// --- params (S24's shape; JSON names are camelCase, like every verb) ---------

// QueueAppendParams mirrors the loop.start payload that CG3 demands: the
// materialized loop is EXACTLY these facts delivered through the existing
// door. `attended` is the per-item declaration or absent-for-the-project-
// default (D5/D6) — never inferred from who happens to be watching.
type QueueAppendParams struct {
	ComponentRoot string            `json:"componentRoot"`
	Blueprint     string            `json:"blueprint"`
	Goal          string            `json:"goal"`
	WorkspaceRoot string            `json:"workspaceRoot,omitempty"`
	Tech          string            `json:"tech,omitempty"`
	With          map[string]string `json:"with,omitempty"`
	Attended      *bool             `json:"attended,omitempty"`
}

type QueueListParams struct {
	ComponentRoot string `json:"componentRoot"`
}

type QueueReorderParams struct {
	ID       int64 `json:"id"`
	Position int   `json:"position"`
}

type QueueRemoveParams struct {
	ID int64 `json:"id"`
}

type QueueSetSizeParams struct {
	ID        int64  `json:"id"`
	SizeMin   int    `json:"sizeMin"`
	SizeMax   int    `json:"sizeMax"`
	SizeBasis string `json:"sizeBasis"`
}

// QueueSettingsParams: bare (both pointers nil) is the READ; setting
// either writes — and a write carries BOTH values, because a half-write
// would let two callers each change one knob and quietly agree on a third
// state nobody said. The CLI enforces the same pairing the socket does.
type QueueSettingsParams struct {
	ComponentRoot string  `json:"componentRoot"`
	QueueAdvance  *string `json:"queueAdvance,omitempty"`
	SizingMode    *string `json:"sizingMode,omitempty"`
}

type QueueAdvanceParams struct {
	ComponentRoot string `json:"componentRoot"`
}

// --- views -------------------------------------------------------------------

// QueueItemView is the board as the daemon sees it. `size` is rendered
// the ONLY way D8 permits: the range and its basis together, or none at
// all — the three fields are all-present-or-all-absent, which the row's
// CHECK already guarantees, so a bare number is not expressible here.
// Timestamps ride the row's own text (the store's format is the record;
// a re-render would be a second truth).
type QueueItemView struct {
	ID            int64             `json:"id"`
	Position      int               `json:"position"`
	Status        string            `json:"status"`
	Blueprint     string            `json:"blueprint"`
	Goal          string            `json:"goal"`
	WorkspaceRoot string            `json:"workspaceRoot"`
	Tech          string            `json:"tech,omitempty"`
	With          map[string]string `json:"with,omitempty"`
	Attended      *bool             `json:"attended"`
	SizeMin       *int              `json:"sizeMin"`
	SizeMax       *int              `json:"sizeMax"`
	SizeBasis     *string           `json:"sizeBasis"`
	LoopSlug      string            `json:"loopSlug,omitempty"`
	CreatedAt     string            `json:"createdAt"`
	UpdatedAt     string            `json:"updatedAt"`
}

type QueueListResult struct {
	ComponentRoot string          `json:"componentRoot"`
	Items         []QueueItemView `json:"items"`
}

type QueueItemResult struct {
	Item QueueItemView `json:"item"`
}

type QueueSettingsResult struct {
	ComponentRoot string `json:"componentRoot"`
	// Known is the three-state answer (P31): false says the component was
	// not found — its defaults are then the slice's declared `ask`/`manual`,
	// STATED as such, never presented as a read fact.
	Known        bool   `json:"known"`
	QueueAdvance string `json:"queueAdvance"`
	SizingMode   string `json:"sizingMode"`
}

type QueueAdvanceResult struct {
	Started bool `json:"started"`
}

func queueItemView(it queue.QueueItem) QueueItemView {
	v := QueueItemView{
		ID: it.ID, Position: it.Position, Status: string(it.Status),
		Blueprint: it.Blueprint, Goal: it.Goal, WorkspaceRoot: it.WorkspaceRoot,
		Tech: it.Tech, Attended: it.Attended, LoopSlug: it.LoopSlug,
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
	if len(it.With) > 0 {
		v.With = it.With
	}
	if it.Size != nil {
		v.SizeMin = &it.Size.Min
		v.SizeMax = &it.Size.Max
		b := it.Size.Basis
		v.SizeBasis = &b
	}
	return v
}

// --- registration (called from method.go's table assembly) -------------------

func (t *Table) registerQueue() {
	t.register(&Method{Name: MethodQueueAppend, RequiresInitialize: true, Handle: t.handleQueueAppend})
	t.register(&Method{Name: MethodQueueList, RequiresInitialize: true, Handle: t.handleQueueList})
	t.register(&Method{Name: MethodQueueReorder, RequiresInitialize: true, Handle: t.handleQueueReorder})
	t.register(&Method{Name: MethodQueueRemove, RequiresInitialize: true, Handle: t.handleQueueRemove})
	t.register(&Method{Name: MethodQueueSetSize, RequiresInitialize: true, Handle: t.handleQueueSetSize})
	t.register(&Method{Name: MethodQueueSettings, RequiresInitialize: true, Handle: t.handleQueueSettings})
	t.register(&Method{Name: MethodQueueAdvance, RequiresInitialize: true, Handle: t.handleQueueAdvance})
}

// --- handlers ----------------------------------------------------------------

func (t *Table) requireQueue() (*queue.Queue, error) {
	if t.Queue == nil {
		return nil, ErrStoreUnavailable
	}
	return t.Queue, nil
}

func (t *Table) handleQueueAppend(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueAppendParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" || p.Blueprint == "" || p.Goal == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot, blueprint and goal are required — an item materializes as loop.start with exactly these (CG3)")}
	}
	// The blueprint is validated the way loop.start validates it — the
	// daemon fills this field with its OWN loader, and the error rides
	// out RAW (measured: loop.start's unknown-name answer is the loader's
	// own sentence at -32603; wrapping it here would prefix and re-code
	// it, and the T6 gate pins the two sentences byte-equal).
	if t.QueueValidateBlueprint != nil {
		if err := t.QueueValidateBlueprint(p.Blueprint); err != nil {
			return nil, err
		}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	it, qerr := q.Append(p.ComponentRoot, p.Blueprint, p.Goal, p.WorkspaceRoot, queue.NewItem{
		Tech: p.Tech, With: p.With, Attended: p.Attended,
	})
	if qerr != nil {
		return nil, queueRefusal(qerr.Error())
	}
	return QueueItemResult{Item: queueItemView(it)}, nil
}

func (t *Table) handleQueueList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required — the queue is per-project (CG8)")}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	items, lerr := q.List(p.ComponentRoot)
	if lerr != nil {
		return nil, queueRefusal(lerr.Error())
	}
	res := QueueListResult{ComponentRoot: p.ComponentRoot, Items: []QueueItemView{}}
	for _, it := range items {
		res.Items = append(res.Items, queueItemView(it))
	}
	return res, nil
}

func (t *Table) handleQueueReorder(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueReorderParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	it, r := q.Reorder(p.ID, p.Position)
	if r != nil {
		return nil, queueRefusal(r.Reason)
	}
	return QueueItemResult{Item: queueItemView(it)}, nil
}

func (t *Table) handleQueueRemove(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueRemoveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	// D12: remove is CANCEL, never delete — the row stays and says so.
	it, r := q.Remove(p.ID)
	if r != nil {
		return nil, queueRefusal(r.Reason)
	}
	return QueueItemResult{Item: queueItemView(it)}, nil
}

func (t *Table) handleQueueSetSize(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueSetSizeParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	it, r := q.SetSize(p.ID, queue.Size{Min: p.SizeMin, Max: p.SizeMax, Basis: p.SizeBasis})
	if r != nil {
		return nil, queueRefusal(r.Reason)
	}
	return QueueItemResult{Item: queueItemView(it)}, nil
}

func (t *Table) handleQueueSettings(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueSettingsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required")}
	}
	if p.QueueAdvance != nil || p.SizingMode != nil {
		// A write carries BOTH knobs (see the params comment); a read
		// carries neither. Half a pair is a shape error, not a merge —
		// decided BEFORE the store is touched.
		if p.QueueAdvance == nil || p.SizingMode == nil {
			return nil, ErrBadParams{Err: fmt.Errorf("a settings write names BOTH queueAdvance and sizingMode — the read names neither")}
		}
	}
	q, err := t.requireQueue()
	if err != nil {
		return nil, err
	}
	if p.QueueAdvance != nil || p.SizingMode != nil {
		ok, serr := q.SetSettings(p.ComponentRoot, *p.QueueAdvance, *p.SizingMode)
		if serr != nil {
			return nil, queueRefusal(serr.Error())
		}
		if !ok {
			return nil, queueRefusal(fmt.Sprintf("no component owns %s — there are no settings to set", p.ComponentRoot))
		}
	}
	advance, sizing, known, gerr := q.Settings(p.ComponentRoot)
	if gerr != nil {
		return nil, queueRefusal(gerr.Error())
	}
	if !known {
		// R4's declared defaults are the DAEMON's sentence, not each
		// client's guess — an unregistered root reads as ask/manual with
		// `known=false` saying so (P31's third state), never as empties
		// a reader must interpret.
		advance, sizing = "ask", "manual"
	}
	return QueueSettingsResult{ComponentRoot: p.ComponentRoot, Known: known, QueueAdvance: advance, SizingMode: sizing}, nil
}

func (t *Table) handleQueueAdvance(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p QueueAdvanceParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("componentRoot is required")}
	}
	if t.QueueAdvance == nil {
		return nil, ErrStoreUnavailable
	}
	// CG4's explicit door: materializing the next item is ALWAYS an
	// explicit call (the design names this method); asking it in `ask`
	// mode IS the human decision (the call itself is the approval).
	started, err := t.QueueAdvance(ctx, p.ComponentRoot)
	if err != nil {
		return nil, queueRefusal(err.Error())
	}
	return QueueAdvanceResult{Started: started}, nil
}
