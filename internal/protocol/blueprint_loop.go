package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type LoopStartParams struct {
	Blueprint     string `json:"blueprint"`
	Goal          string `json:"goal"`
	WorkspaceRoot string `json:"workspaceRoot"`
	Route         string `json:"route,omitempty"`
	// ReasoningEffort and Thinking are the model options chosen at start (advanced options): the effort the route's
	// models accept (empty keeps the route's own), and `on` or `off` for models that can think (empty keeps the
	// model's default). They apply to every model step of the loop.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Slug            string `json:"slug,omitempty"`
	// Tech is the operator's tech tag for a `by-tech-tag` lane (T10). It
	// enters the loop's durable `with` map at pin time; the resolver reads
	// `with["tech"]`. Empty means unclaimed — a resolver step then refuses
	// naming the absence, because "no tag" is not "go by default" (A13).
	Tech string `json:"tech,omitempty"`
	// With carries the operator's loop-level `with` entries at start —
	// the same durable channel the tech tag rides (S14's). S16 T12's walk
	// added it: outward actions need the task identity, and until S17's
	// ladder materialises items, a single-task loop declares
	// with["task"] at start. Absent means absent: actions that need a
	// task refuse naming the absence, never guess one.
	With map[string]string `json:"with,omitempty"`
	// Attended is the D2 declaration made at start, carried to the daemon so
	// the pin can write it: true means a human is here to answer questions,
	// false means nobody is, absent means the client cannot say — which the
	// store records as never-declared, never as false (P31). The CLI always
	// sends it; a client that does not know must not send a guess.
	Attended *bool `json:"attended,omitempty"`
	// AllowOutward is S19 T6's per-loop opt-in (D10, O2): what THIS run
	// may do outward, declared by the person who starts it, at the one
	// moment that question is in front of them — not six weeks earlier in
	// a config file, and never as a property of a key or a caller (a
	// standing grant is a grant whose theft is unbounded). It lifts
	// OUTWARD for an unattended loop and nothing else: irreversible
	// stays S16's ask, and there is deliberately no field here reaching
	// merging. The narrow surface forces Attended=false on every trigger
	// (a CI job is not present in the sense a gate needs); this field is
	// the only outward lever any of them gets.
	AllowOutward bool `json:"allowOutward,omitempty"`
	// GoalID is S38 T13's start coordinate (OL9): start this loop WORKING
	// ON a goal — its version is pinned at start (later goal versions do
	// not move the loop's frame), and a blueprint that declares
	// `requires:` checks against it. Absent means "this loop works on
	// its own declared goal text", which is every start before S38.
	GoalID string `json:"goalId,omitempty"`
	// TaskID starts the loop ON a person-approved manual task: the loop
	// becomes that task's worker (task_loop role worker) the moment its
	// row exists. Like GoalID, absent is today's shape.
	TaskID string `json:"taskId,omitempty"`
	// Skills names the skills the operator CHOSE for this start (S68, B6).
	// The daemon validates them against the live catalog — unknown names
	// refuse the call naming every offender — files the chosen names on the
	// kickoff row (the durable receipt), and renders each chosen skill's
	// canonical instruction line into every step of THIS launch's system
	// message. They do not stick to the loop: the choice belongs to this
	// send. nil means nothing chosen — byte-identical to every pre-S68
	// start.
	Skills []string `json:"skills,omitempty"`
	// Attachments are references to files PREVIOUSLY PUT under this start's
	// own slug and workspaceRoot (S77: a loop may START with files attached
	// — the launch's kickoff row carries the receipts the way S68's chosen
	// skills do). Each {document, rev} names a frozen @attachments/ revision
	// this scope put; the daemon resolves every reference against the store
	// BEFORE any row is written and files the STORE's receipts, refusing
	// whole and naming every offender. The start must therefore name its
	// `slug` — a minted slug can never own bytes put earlier. Absent means
	// nothing attached; `[]` is refused; and the start's route must DECLARE
	// the capability each type asks (image / pdf / file), or the launch
	// refuses before anything exists.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}
type LoopShowParams struct {
	ID               string `json:"id"`
	IncludeBlueprint bool   `json:"includeBlueprint,omitempty"`
}
type LoopFileParams struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type LoopAnswerParams struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
	// Feedback is the person's words with a gate answer — what to change when they send a plan back to be revised.
	// It is filed in the loop's answers.md so the agent that runs again reads it.
	Feedback string `json:"feedback,omitempty"`
}

// LoopCloseParams names the resting loop (S39 CR1). One id, like the retry:
// closing is scoped to THE loop the operator is looking at. loop.close moves
// idle -> done; a running loop is refused with a sentence, and an already
// finished one is refused too — closing what never rested is a no-op dressed
// as a command.
type LoopCloseParams struct {
	ID string `json:"id"`
}

// LoopArchiveParams is loop.archive's request (S33 T9, the bench's Delete
// door): the loop by id, and an optional note explaining WHY a person is
// putting it into cold storage. The archive is the honest Delete for loops —
// nothing is destroyed: the loop and its documents move to cold storage
// WITH their state, and the telemetry rollups do not move at all (D8: the
// counted work was counted; archiving the ledger's subject does not
// un-count it). No write method on the telemetry surface itself may ever
// appear to fill this gap — the rollups are facts, not deletable rows.
type LoopArchiveParams struct {
	ID   string `json:"id"`
	Note string `json:"note,omitempty"`
}

// LoopMessageParams is a message to an idle loop's conversation (S39 CR3):
// the loop by id, the person's text, and an optional route for the turn's
// model call. `text` is bounded by the steer's bound — it becomes a thread
// row AND the tail of a model prompt, and the tighter bound is the right
// one for both.
type LoopMessageParams struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Route string `json:"route,omitempty"`
	// Skills names the skills the operator CHOSE for THIS turn (S68, B6):
	// validated against the live catalog (unknown names refuse the call
	// naming every offender), filed on the thread row as the receipt, and
	// rendered into this turn's system message only. They do not stick to
	// the loop; nil means nothing chosen.
	Skills []string `json:"skills,omitempty"`
	// Attachments are references to previously PUT files (S69 AT3): each
	// {document, rev} names a frozen @attachments/ revision this loop put.
	// The door resolves every reference against the store BEFORE the row is
	// written and files receipts from the STORE's facts; absent means
	// nothing attached, `[]` is refused.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
}

// LoopRetryHandlerBlockParams names the blocked loop. One id: the retry is
// scoped to THE loop the operator is looking at — there is no bulk retry
// surface, because a bulk retry is a second dispatcher wearing a toolbar.
type LoopRetryHandlerBlockParams struct {
	ID string `json:"id"`
}
// LoopRecoverParams names the blocked loop to take back one step. One id, never a sweep.
type LoopRecoverParams struct {
	ID string `json:"id"`
}

type BlueprintValidateParams struct {
	Name       string `json:"name,omitempty"`
	Content    string `json:"content,omitempty"`
	Mode       string `json:"mode,omitempty"`
	CreateOnly bool   `json:"createOnly,omitempty"`
}

func (t *Table) handleLoopStart(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopStartParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Blueprint == "" || p.Goal == "" || p.WorkspaceRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("blueprint, goal and workspaceRoot are required")}
	}
	if p.Attachments != nil && p.Slug == "" {
		// S77: the put scope is a slug and a workspace root fixed BEFORE the
		// launch. A start that files receipts for bytes put under a slug must
		// name that slug — a minted slug owns no earlier put, so the field
		// and an unnamed slug are refused here rather than resolved to
		// nothing down the door.
		return nil, ErrBadParams{Err: fmt.Errorf("attachments are put under a slug and workspace root before the launch — start with an explicit `slug` so the files put there can ride; a minted slug owns no earlier puts")}
	}
	if t.StartBlueprintLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.StartBlueprintLoop(ctx, p)
}
func (t *Table) handleLoopShow(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopShowParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.ShowBlueprintLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ShowBlueprintLoop(ctx, p)
}
func (t *Table) handleLoopFiles(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopFileParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.ListLoopFiles == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ListLoopFiles(ctx, p)
}
func (t *Table) handleLoopFileRead(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopFileParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" || p.Path == "" || p.Path == "." {
		return nil, ErrBadParams{Err: fmt.Errorf("id and file path are required")}
	}
	if t.ReadLoopFile == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ReadLoopFile(ctx, p)
}
func (t *Table) handleLoopAnswer(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopAnswerParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" || p.Answer == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and answer are required")}
	}
	if t.AnswerBlueprintLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.AnswerBlueprintLoop(ctx, p)
}

// handleLoopClose ends a RESTING loop (S39 CR1): decode, one required id,
// and the daemon decides — the table never guesses whether the loop is idle.
func (t *Table) handleLoopClose(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopCloseParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required")}
	}
	if t.CloseLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.CloseLoop(ctx, p)
}

// handleLoopMessage accepts a message for an idle loop (S39 CR3): bounds at
// the door (the steer's bound, reused verbatim), and the daemon decides —
// the table never guesses whether the loop rests, whether a step runs, or
// whether a turn is already in flight. A refusal comes back from the daemon
// as DATA (refused.kind), like the retry's and the close's.
// handleLoopArchive ends a loop's live life by putting it into cold
// storage (S33 T9). Like its siblings the table only bounds the arguments
// and defers to the daemon — the daemon resolves the loop, assembles the
// state (the store does, deliberately: a caller deciding "the loop's state"
// would decide differently), and the rollups ride through untouched.
func (t *Table) handleLoopArchive(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopArchiveParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — `aeon loop list` names the loops that exist")}
	}
	if t.ArchiveLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ArchiveLoop(ctx, p)
}

func (t *Table) handleLoopMessage(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopMessageParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	p.Text = strings.TrimSpace(p.Text)
	if p.ID == "" || p.Text == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and text are required")}
	}
	if len(p.Text) > maxSteerTextBytes {
		return nil, ErrBadParams{Err: fmt.Errorf("text is %d bytes; the bound is %d", len(p.Text), maxSteerTextBytes)}
	}
	if t.MessageLoop == nil {
		return nil, ErrStoreUnavailable
	}
	return t.MessageLoop(ctx, p)
}

// LoopRunParams starts a NEW run of a blueprint inside an existing resting
// loop (S39 CR2/CR4): a human peer's door — a person typing is the
// confirmation, so no ask stands in front of it. The daemon answers domain
// refusals as DATA (refused.kind); an error here means params or plumbing.
type LoopRunParams struct {
	ID        string            `json:"id"`
	Blueprint string            `json:"blueprint"`
	GoalID    string            `json:"goalId,omitempty"`
	TaskID    string            `json:"taskId,omitempty"`
	With      map[string]string `json:"with,omitempty"`
	Message   string            `json:"message,omitempty"` // a first message the person typed
	Route     string            `json:"route,omitempty"`
}

// LoopRunOptionsParams asks what this loop could run next, and why not.
type LoopRunOptionsParams struct {
	ID string `json:"id"`
}

// handleLoopRun: bounds at the door, decisions at the daemon.
func (t *Table) handleLoopRun(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	var p LoopRunParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Blueprint) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id and blueprint are required")}
	}
	if len(p.Message) > maxSteerTextBytes {
		return nil, ErrBadParams{Err: fmt.Errorf("message is %d bytes; the bound is %d", len(p.Message), maxSteerTextBytes)}
	}
	if t.RunBlueprint == nil {
		return nil, ErrStoreUnavailable
	}
	return t.RunBlueprint(ctx, sess, p)
}

// handleLoopRunOptions lists blueprints with their runnability verdict.
func (t *Table) handleLoopRunOptions(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopRunOptionsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("id is required — options are answered per loop")}
	}
	if t.RunOptions == nil {
		return nil, ErrStoreUnavailable
	}
	return t.RunOptions(ctx, p)
}

func (t *Table) handleLoopRetryHandlerBlock(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopRetryHandlerBlockParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("`id` is required — the retry is scoped to one blocked loop, never a sweep")}
	}
	if t.RetryHandlerBlock == nil {
		return nil, ErrStoreUnavailable
	}
	return t.RetryHandlerBlock(ctx, p)
}

func (t *Table) handleLoopRecover(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopRecoverParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("`id` is required — a recovery is scoped to one blocked loop, never a sweep")}
	}
	if t.RecoverBlockedStep == nil {
		return nil, ErrStoreUnavailable
	}
	return t.RecoverBlockedStep(ctx, p)
}

// handleBlueprintList and handleBlueprintShow moved to blueprint_manage.go
// with S48 T7 — their registrations stay here; the handlers grew their
// answers additively (params filter daemon-side, summaries/report/refs join;
// names and path:"" stay byte-compatibly).
func (t *Table) handleBlueprintFields(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if t.BlueprintFields == nil {
		return nil, ErrStoreUnavailable
	}
	return t.BlueprintFields(), nil
}
func (t *Table) handleBlueprintValidate(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p BlueprintValidateParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" && p.Content == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name or content is required")}
	}
	if t.ValidateBlueprint == nil {
		return nil, ErrStoreUnavailable
	}
	return t.ValidateBlueprint(p)
}

// handleBlueprintSave moved to blueprint_manage.go with S48 T6 — the save
// door is REWRITTEN onto rows there (params, actor stamping and the write
// service seam all live beside the other three write doors).
