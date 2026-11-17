package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// The `step` method: the skateboard's entry point, and **the only writer**
// (K22).
//
// # The order of operations, and why it matters for S7
//
//	1. create the step row with status='running'   ← BEFORE RunStep
//	2. call RunStep
//	3. persist the transcript and manifest
//	4. for a suspension, persist the question
//	5. set the status and cost
//
// **Creating the row first is what makes S7 possible.** A row written only on
// completion means a crashed step leaves no trace, and there is nothing to
// recover: S7's whole recovery query is `status='running'`, and it finds
// nothing if nothing was written before the work began.
//
// It costs one write per step and buys the ability to notice a step that died.
//
// # Why this handler persists and the runtime does not
//
// `RunStep` persists nothing (K6) so S6, S7 and S9 can each hand it a
// transcript from somewhere else. That makes THIS the writer, and a test
// asserts it is the only one: a grep for store writes in the step path finds
// hits here and nowhere else.

// StepParams is `step`'s input.
type StepParams struct {
	// Prompt is what the step is asked to do. Required.
	Prompt string `json:"prompt"`
	// Agent selects a registered handler for blueprint steps. Its grant list is
	// fixed by the registry; a blueprint cannot supply or widen it.
	Agent string `json:"agent,omitempty"`

	// Skill targets another agent's skill from a loop step whose Agent is the
	// caller — S14's `skill:` lane (T0 carries it; the invocation that consumes
	// it is T4). Format "<callee>/<skill-id>"; empty means an ordinary step.
	// The blueprint validator checked the target at pin time (three states);
	// the skill's typed request (T2) validates Params before any model call —
	// this layer carries, it does not interpret.
	Skill string `json:"skill,omitempty"`

	// Params is the skill request's parameter map, carried verbatim from the
	// pinned blueprint step. nil for ordinary steps — never an empty map, so a
	// pre-S14 request is byte-identical on the wire.
	Params map[string]any `json:"params,omitempty"`

	// Label names an ad-hoc step (one with no Agent) in events and in the step row, in place of the generic name, so
	// a daemon-made step such as a loop's title reads as what it is in the history. Ignored when Agent is set.
	Label string `json:"label,omitempty"`

	// Tools is the grant list — the only thing that decides what the step can
	// call (D1). Empty takes the default workspace file tools.
	//
	// **`bash` is never granted by default.** A caller asks for it explicitly,
	// and `aeon step --allow-bash` warns every time it does.
	Tools []string `json:"tools,omitempty"`

	// SubscriptionID streams `step.*` events to a subscription. Optional; a
	// step with none still runs and still returns its result.
	SubscriptionID string `json:"subscriptionId,omitempty"`

	// MaxIterations overrides the loop bound. Zero takes the default.
	MaxIterations int `json:"maxIterations,omitempty"`
	// StepMaxIterations is the blueprint step's declared bound (nil: none; 0: no
	// bound). The daemon resolves it against the agent's and the setting.
	StepMaxIterations *int `json:"stepMaxIterations,omitempty"`

	// WorkspaceRoot is where file tools resolve (Q10).
	//
	// **Required, with no default.** The daemon multiplexes projects, so its
	// own working directory is not the client's — a step defaulting to it would
	// read the daemon's files and look like it worked. `aeon step` sends the
	// client's working directory.
	WorkspaceRoot string `json:"workspaceRoot"`

	// Route is which model to call. Empty takes the only configured route when
	// there is exactly one, which is what makes the demo a single command.
	Route string `json:"route,omitempty"`

	// ReasoningEffort overrides the route's for this step.
	//
	// **Empty is not "the route's value" here** — it is "let assembly decide",
	// and assembly lowers a reasoning effort to `none` when the step offers
	// tools, because the models that have this dial refuse tools while it is
	// on. Naming a value takes it literally, including a value the provider
	// will refuse.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// Thinking is `on`, `off` or empty (the model's own default): the person's thinking switch for this step. It is
	// sent as the request field a thinking-capable server reads (`chat_template_kwargs.enable_thinking`), merged over
	// the route's own extra body for this step only.
	Thinking string `json:"thinking,omitempty"`

	// LoopSlug and ComponentRoot say where the step is filed. A step belongs to a
	// loop (S4's schema), and S8 will create loops properly; at S5 a step with
	// no loop is filed under one named for the workspace.
	LoopSlug string `json:"loopSlug,omitempty"`
	// ContextFiles are workspace-relative files frozen into the opening
	// context. Their hashes make changed-file notes meaningful on resume.
	ContextFiles []string `json:"contextFiles,omitempty"`

	// ContextRetrievals declares retrieval sections (S22 T7, V7/R6): each is
	// resolved once at declaration and frozen into the manifest, with the
	// entry marked retrieved + query + score so the model knows its context
	// was a similarity guess and verifies (V14, O2). Nothing-found is
	// RECORDED, not silenced (O6). Absent means the step declares only
	// files/text — retrieval was never asked, a different state from asking
	// and finding nothing.
	ContextRetrievals []ContextRetrievalDecl `json:"contextRetrievals,omitempty"`

	// TaskFrame is the loop-side instruction block (S68, B2): lane, blueprint
	// facts, artifact contract, the conversation frame. It renders as the
	// `## Your task` SYSTEM section — never as the operator's words, which
	// ride `prompt` alone. Empty means no frame (the plain `aeon step` door).
	TaskFrame string `json:"taskFrame,omitempty"`

	// History is the conversation so far (S68, B4) for a converse turn: real
	// alternating user/assistant turns the provider sees BEFORE the new
	// words. Only those two roles are legal — the runtime refuses any other
	// at construction, naming the offender. Adjacent same-role turns merge
	// into one message. Empty means a fresh conversation.
	History []HistoryTurn `json:"history,omitempty"`

	// Skills carries the RESOLVED instruction lines for the skills the
	// operator chose for THIS turn (S68, B6/B7), each already in the
	// canonical sentence family the daemon prints — this layer carries, it
	// does not interpret, exactly like `Skill`/`Params`. The name-to-line
	// resolution (and the refusal of unknown names) happens at the daemon's
	// doors that take names: `loop.start`, `loop.message`, `step.steer`.
	// The lines render as the `## Chosen skills` system section. nil means
	// nothing chosen — byte-identical to the pre-S68 step.
	Skills []string `json:"skills,omitempty"`

	// ChosenSkillsNote is the daemon's already-rendered sentence naming the
	// chosen skills that could NOT ride (unpickable at send, S68 B6). It is
	// appended to the step's opening line — the receipt says what actually
	// rode — and carries nothing else: the runtime prints it verbatim and
	// interprets none of it. Empty means nothing was skipped.
	ChosenSkillsNote string `json:"chosenSkillsNote,omitempty"`

	// Attachments are the PROMPT turn's receipts (S69 AT2/AT5): references
	// the door already resolved and filed — the client never sends bytes
	// here, and the expansion below refuses to trust anything the caller
	// claims about the bytes. ComponentRoot locates the store scope the
	// references resolve in; refs without it refuse.
	Attachments   []AttachmentReceiptView `json:"attachments,omitempty"`
	ComponentRoot string                  `json:"componentRoot,omitempty"`
}

// HistoryTurn is one conversation turn on the step door (S68, B4). Role is
// "user" or "assistant" — nothing else rides (the runtime refuses it, and
// the store's transcript validator refuses a second assistant in a row, so
// adjacent same-role turns arrive merged).
type HistoryTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
	// Attachments is the row's receipt as filed (S69 AT2) — references,
	// never bytes; the expansion below is the one door bytes cross on the
	// way to the wire.
	Attachments []AttachmentReceiptView `json:"attachments,omitempty"`
}

// StepResult is what `step` answers.
//
// The call returns its id and outcome after the bounded run. Events stream
// during the request, and the transcript remains in the store.
//
// The outcome is ALSO returned, because a caller without a subscription needs
// it: `aeon step` subscribes, but a script calling the method once should not
// have to.
type StepResult struct {
	StepID int64 `json:"stepId"`

	Kind    string `json:"kind"`
	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`

	Handoff  json.RawMessage          `json:"handoff,omitempty"`
	Question *runtime.PendingQuestion `json:"question,omitempty"`

	// PauseReason mirrors StepView's: set only when `Kind` is `suspended`
	// without a real question — "user" is the person's stop (S31), "cap" is
	// the S55 T7 in-step budget hit (E2). The absence of a question is a
	// fact, not a nil to guess at (DB19): a suspended result whose
	// PauseReason is empty is the question shape.
	PauseReason string `json:"pauseReason,omitempty"`

	// CapHit is set beside PauseReason="cap" (S55 T7): the raw numbers of
	// the hit the step's own check made — plain data for the loop engine,
	// which owns the durable record (its CapHitState and the hit event).
	// Empty for every other kind and every pre-T7 result.
	CapHit *runtime.CapHit `json:"capHit,omitempty"`

	Usage model.Usage `json:"usage"`
	Cost  model.Cost  `json:"cost"`

	// Iterations is how many passes the loop made, so a caller can see a step
	// that nearly hit its bound.
	Iterations int `json:"iterations"`

	// PendingRoute and PendingEffort carry the route switch a mid-step steer
	// was refused (S18 T8, O5/N9) — empty for the ordinary step. They are
	// data for the loop engine, not a verdict for the caller.
	PendingRoute  string `json:"pendingRoute,omitempty"`
	PendingEffort string `json:"pendingEffort,omitempty"`

	// SinkFailures is non-zero when the event stream could not be delivered.
	// Reported because a client seeing no events and a successful result would
	// otherwise have no way to tell which half broke.
	SinkFailures int `json:"sinkFailures,omitempty"`
}

// StepRunner is the runtime, as this package needs it.
//
// An interface so the handler's tests drive a scripted runner with no provider,
// and so `internal/protocol` keeps its own shape rather than depending on the
// runtime's construction details.
// ContextRetrievalDecl is one retrieval declaration on the step door: the
// NAMED corpus and the query. No defaults on either (O8).
type ContextRetrievalDecl struct {
	Corpus string `json:"corpus"`
	Query  string `json:"query"`
}

// historyMessages maps the door's shape to the model's (S68, B4). The only
// rules about what may ride the history are the runtime's (role refusal at
// construction, adjacency merge at compile) — this maps, never filters.
func historyMessages(turns []HistoryTurn, blocks [][]model.Block) []model.Message {
	if len(turns) == 0 {
		return nil
	}
	out := make([]model.Message, 0, len(turns))
	for i, t := range turns {
		role := model.Role(t.Role)
		switch t.Role {
		case "user":
			role = model.RoleUser
		case "assistant":
			role = model.RoleAssistant
		}
		// Anything else passes through unchanged: the runtime's role gate
		// refuses it by name at construction (S68 B4) — a mapping that
		// quietly "fixed" an unknown role would hide the offender.
		content := []model.Block{model.TextBlock(t.Text)}
		// S69 AT5: the row's expanded blocks ride beside the words, in the
		// row's own message — history keeps its shape, the receipts keep
		// their bytes next to the text they came with.
		if i < len(blocks) {
			content = append(content, blocks[i]...)
		}
		out = append(out, model.Message{Role: role, Content: content})
	}
	return out
}

// loopSlugOr is the loop's own name for the expander's scope argument; the
// ad-hoc step's "step" fallback needs no store scope and the expander
// refuses it by name if references claim it.
func loopSlugOr(slug string) string {
	if slug == "" {
		return "step"
	}
	return slug
}

type StepRunner interface {
	// Compile validates and assembles. It fails BEFORE any model call, which
	// is the property T3's spy asserts.
	Compile(ctx context.Context, in runtime.CompileInput, deps runtime.ContextDeps) runtime.CompileResult

	// Run executes a compiled step.
	Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult

	// BuildTools constructs the granted toolset, or refuses an unknown name.
	BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error)
}

// StepStore is the store, as this handler needs it.
//
// Narrow on purpose: four methods, each one the handler calls. A handler given
// the whole store could persist anything, and "the handler is the only writer"
// would stop being checkable by reading this interface.
type StepStore interface {
	CreateRunningStep(loopSlug, componentRoot, agent string) (int64, error)
	AppendStepMessages(stepID int64, msgs []model.Message) error
	SaveStepContext(stepID int64, manifest json.RawMessage) error
	FinishStep(stepID int64, status, outcome string, cost model.Cost) error
	// SetPauseReason tells apart a step suspended because a PERSON stopped it
	// from one suspended because the model asked a real question — both share
	// `status='suspended'` (P2). Empty clears it; called that way on every
	// successful `step.continue` so a running step never carries a stale
	// reason from the time it was not.
	SetPauseReason(stepID int64, reason string) error
}

// QuestionStore owns the durable question and transcript operations needed by
// suspension. It is separate from StepStore so ordinary steps retain their
// narrow writer contract.
type QuestionStore interface {
	AskStep(stepID int64, loopSlug, componentRoot string, q runtime.PendingQuestion) (int64, error)
	CheckStepQuestion(stepID, questionID int64) (AnsweredQuestion, error)
	AnswerStep(stepID, questionID int64, answer string) (AnsweredQuestion, error)
	LoadResume(stepID int64) (ResumeSnapshot, error)
	ReplaceStepMessages(stepID int64, msgs []model.Message) error
	MarkRunning(stepID int64) error
	// ContinueWithMessages marks the step running AND replaces its transcript
	// in ONE transaction (S31 T2, D6) — `step.continue {message}` uses this
	// instead of MarkRunning, so the injected user message is delivered
	// exactly once across a crash between the continue and the first model
	// call: the row stops saying `suspended` at the same instant the message
	// reaches the transcript, so a retried continue is refused by the very
	// guard that made the flip atomic.
	ContinueWithMessages(stepID int64, msgs []model.Message) error
	// StepState reads the current status and pause reason, so `step.continue`
	// can refuse a step that is not actually suspended-by-a-stop before it
	// touches the transcript — a step still running, or one waiting on a
	// real question, needs a different refusal than "cannot be resumed".
	StepState(stepID int64) (status, pauseReason string, err error)
	// StepRoute reads the CURRENT route of the loop that owns this step
	// (S18 T8, Q6): the loop's state is the single route a dispatch reads,
	// and a resume must not resurrect the route the step was created under.
	// ok=false for a step no loop owns (an ask step's saved route is the
	// only route there is) or when the state cannot be read.
	StepRoute(stepID int64) (routeName string, ok bool)
}

type AnsweredQuestion struct {
	Question   runtime.PendingQuestion
	Answer     string
	AnsweredAt string
	Refusal    string
}

type ResumeSnapshot struct {
	Manifest json.RawMessage
	Messages []model.Message
}

type stepSnapshot struct {
	runtime.ContextManifest
	Request     runtime.StepRequest        `json:"request"`
	LoopSlug    string                     `json:"loopSlug"`
	Usage       model.Usage                `json:"usage"`
	Compactions []runtime.CompactionRecord `json:"compactions,omitempty"`
}

// StepSnapshot is the durable request envelope read by crash recovery.
type StepSnapshot = stepSnapshot

// recoveryWriter is scoped to one step. The runtime can ask it to checkpoint
// or write a receipt but cannot address any other step's rows.
type recoveryWriter struct {
	store interface {
		SaveCheckpoint(context.Context, int64, []model.Message, json.RawMessage, model.Cost) error
		BeginReceipt(context.Context, int64, string, string, string) error
		CompleteReceipt(context.Context, int64, string, string, bool) error
	}
	stepID   int64
	loopSlug string
}

func (w recoveryWriter) Begin(ctx context.Context, useID, tool, digest string) error {
	return w.store.BeginReceipt(ctx, w.stepID, useID, tool, digest)
}
func (w recoveryWriter) Complete(ctx context.Context, useID, result string, isError bool) error {
	return w.store.CompleteReceipt(ctx, w.stepID, useID, result, isError)
}
func (w recoveryWriter) Save(ctx context.Context, cp runtime.Checkpoint) error {
	raw, err := json.Marshal(stepSnapshot{ContextManifest: cp.Manifest, Request: cp.Request,
		LoopSlug: w.loopSlug, Usage: cp.Usage, Compactions: cp.Compactions})
	if err != nil {
		return err
	}
	if len(cp.SteerIDs) > 0 {
		// D4: the transcript carrying the steers' text and the rows' delivery
		// flip are ONE write — which is why they cannot be observed apart.
		// A backing store without the delivery half is refused rather than
		// silently half-delivered: an honest step failure beats a transcript
		// whose steers read as read.
		deliver, ok := w.store.(interface {
			CheckpointWithSteers(context.Context, int64, []model.Message, json.RawMessage, []int64, int) error
		})
		if !ok {
			return fmt.Errorf("protocol: this daemon's store cannot deliver steers — the checkpoint "+
				"carrying %d steer(s) for step %d was refused rather than half-written",
				len(cp.SteerIDs), w.stepID)
		}
		if err := deliver.CheckpointWithSteers(ctx, w.stepID, cp.Messages, raw, cp.SteerIDs, cp.Iteration); err != nil {
			return err
		}
		if len(cp.MailboxIDs) == 0 {
			return nil
		}
		// Both queues spoke at one boundary: each queue's flip is atomic in
		// its own transaction (its own D4 join); a death between them leaves
		// the undelivered queue's rows pending, and only those re-fold —
		// exactly the single-queue crash window, composed.
	}
	if len(cp.MailboxIDs) > 0 {
		// The mailbox's D4 join (S70 MB4), same law, different queue.
		deliver, ok := w.store.(interface {
			CheckpointWithMailbox(context.Context, int64, []model.Message, json.RawMessage, []int64, int) error
		})
		if !ok {
			return fmt.Errorf("protocol: this daemon's store cannot deliver mailbox rows — the checkpoint "+
				"carrying %d message(s) for step %d was refused rather than half-written",
				len(cp.MailboxIDs), w.stepID)
		}
		return deliver.CheckpointWithMailbox(ctx, w.stepID, cp.Messages, raw, cp.MailboxIDs, cp.Iteration)
	}
	return w.store.SaveCheckpoint(ctx, w.stepID, cp.Messages, raw, cp.Cost)
}

func (t *Table) recoveryWriter(stepID int64, loopSlug string) *recoveryWriter {
	backing, ok := t.Steps.(interface {
		SaveCheckpoint(context.Context, int64, []model.Message, json.RawMessage, model.Cost) error
		BeginReceipt(context.Context, int64, string, string, string) error
		CompleteReceipt(context.Context, int64, string, string, bool) error
	})
	if !ok {
		return nil
	}
	return &recoveryWriter{store: backing, stepID: stepID, loopSlug: loopSlug}
}

// RunStepForLoop uses the same writer, bounds, receipts and suspension path as
// the public step method. It is called by the daemon's S8 loop runner.
type blueprintStepCreatedKey struct{}

// LoopStepIdentity names the loop move a step is executing, when the loop
// engine runs it (S31 D1/D2). It is SERVER-supplied — the daemon's blueprint
// caller passes it into RunStepForLoop, which carries it to handleStep on the
// context exactly like the onCreated seam. It is never decoded from client
// params: an identity a client could set is an identity a client chose (the
// PL1 argument, applied to the stream).
type LoopStepIdentity struct {
	LoopID string `json:"loopId,omitempty"`
	Lane   string `json:"lane,omitempty"`
	Step   string `json:"step,omitempty"`
}

type loopStepIdentityKey struct{}

func (t *Table) RunStepForLoop(ctx context.Context, p StepParams, id LoopStepIdentity, onCreated func(int64) error) (StepResult, error) {
	ctx = context.WithValue(ctx, blueprintStepCreatedKey{}, onCreated)
	ctx = context.WithValue(ctx, loopStepIdentityKey{}, id)
	raw, err := json.Marshal(p)
	if err != nil {
		return StepResult{}, err
	}
	value, err := t.handleStep(ctx, nil, raw)
	if err != nil {
		return StepResult{}, err
	}
	result, ok := value.(StepResult)
	if !ok {
		return StepResult{}, fmt.Errorf("step handler returned %T", value)
	}
	return result, nil
}

func (t *Table) handleStep(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	var p StepParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}

	if t.Runner == nil {
		return nil, ErrStepUnavailable
	}
	if t.Steps == nil {
		return nil, ErrStoreUnavailable
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("`prompt` is required")}
	}
	if strings.TrimSpace(p.WorkspaceRoot) == "" {
		// Named rather than defaulted (Q10). The daemon's working directory is
		// not the client's, and a step that read the daemon's files would look
		// like it had worked.
		return nil, ErrNoWorkspace
	}

	granted := p.Tools
	var handler runtime.StepHandler
	if p.Agent != "" {
		if len(p.Tools) > 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("agent steps take their tool grants from the handler registry")}
		}
		if t.HandlerForStep == nil {
			return nil, ErrStepUnavailable
		}
		var ok bool
		handler, ok = t.HandlerForStep(p.Agent, p.Prompt)
		if !ok {
			return nil, ErrBadParams{Err: fmt.Errorf("unknown handler %q", p.Agent)}
		}
		granted = handler.Tools
	} else {
		if len(granted) == 0 {
			granted = runtime.DefaultGrantNames()
		}
		handler = runtime.DefaultHandler(p.Prompt, granted)
		if l := strings.TrimSpace(p.Label); l != "" {
			handler.Name = l
		}
	}
	// Every step that belongs to a loop can read what the loop holds, ask the person, and use the code index. The
	// loop's files are in the database, so an agent without `doc_read` could not open its own spec; one without
	// `question` guesses instead of asking; one without `search`/`graph` greps blind. Added to the grant and to the
	// frozen handler so a resumed step rebuilds the same set.
	if p.LoopSlug != "" {
		granted = withLoopTools(granted, t.ToolRegistered)
		handler.Tools = granted
	}

	if p.Thinking != "" && p.Thinking != "on" && p.Thinking != "off" {
		return nil, ErrBadParams{Err: fmt.Errorf("thinking %q is not on, off, or empty", p.Thinking)}
	}
	route, err := t.routeForStep(p.Route)
	if err != nil {
		return nil, err
	}
	route = withThinking(route, p.Thinking)

	// The subscription is resolved BEFORE the step starts, so a bad id fails
	// immediately rather than after a minute of work nobody is watching. The
	// SINK is not built yet — every step event carries the step id, and the
	// id does not exist until the row is created below (K22). Resolving and
	// publishing are deliberately separated for that reason.
	var sub *Subscription
	if p.SubscriptionID != "" {
		sub = t.Subs.Find(sess.ConnID, p.SubscriptionID)
		if sub == nil {
			// A named error, not silence (T10's point 4). Silently ignoring it
			// leaves the client waiting for a stream that never arrives, which
			// reads as a hung step.
			return nil, ErrUnknownSubscription.WithWhat(
				"subscription %q is not registered on this connection, so the step's events would "+
					"have gone nowhere. A subscription belongs to the connection that created it",
				p.SubscriptionID)
		}
	}

	toolset, specs, err := t.Runner.BuildTools(granted, p.WorkspaceRoot, handler.Name, runtime.FrozenManifest{})
	if err != nil {
		return nil, ErrUnknownTool.WithWhat("%v", err)
	}
	// MD5: a toolset may carry cached MCP server children (a composite caller);
	// close them when this function returns — after the step has finished using
	// the toolset, not immediately after BuildTools. An io.Closer check is a
	// no-op when no MCP bundle is wired (MD8: byte-identical to today).
	defer func() {
		if c, ok := toolset.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	for _, path := range p.ContextFiles {
		if strings.TrimSpace(path) == "" {
			return nil, ErrBadParams{Err: fmt.Errorf("contextFiles cannot contain an empty path")}
		}
		handler.Sections = append(handler.Sections, runtime.SectionSpec{
			Title: "Declared file: " + path, DeclaredFile: path,
			Category: runtime.CategoryInjectedContext,
			// S68: material a person chose to hand over rides the USER
			// message — it is part of what the step is asked to reason on.
			Destination: runtime.SectionTranscript,
		})
	}
	// Retrieval declarations (S22 T7, V7/R6): "this step needs the code
	// relevant to X", resolved ONCE here at declaration — the sections
	// assemble into the frozen manifest and no resume or recovery re-asks
	// (N7; runtime/manifest_test.go keeps the spy at zero on both paths).
	// Refusals at this door mirror the `search` tool's own: the corpus is
	// NAMED, both options are said, and absence is refused rather than
	// silently skipped — skipping would make "the index had no opinion" and
	// "this binary has no index" one unreadable declaration (V13, O8).
	for _, r := range p.ContextRetrievals {
		if r.Corpus != "code" && r.Corpus != "history" {
			return nil, ErrBadParams{Err: fmt.Errorf(
				"contextRetrievals corpus %q is not named: it must be %q or %q — there is no default corpus",
				r.Corpus, "code", "history")}
		}
		if strings.TrimSpace(r.Query) == "" {
			return nil, ErrBadParams{Err: fmt.Errorf("contextRetrievals cannot carry an empty query")}
		}
		if t.Search == nil {
			return nil, ErrBadParams{Err: fmt.Errorf(
				"contextRetrievals asks the index for %q over the %q corpus, but this daemon has no "+
					"retrieval resolver wired (V13) — the door is absent, so the declaration is refused "+
					"rather than skipped", r.Query, r.Corpus)}
		}
		handler.Sections = append(handler.Sections, runtime.SectionSpec{
			Title:     fmt.Sprintf("Retrieved for query %q (%s corpus)", r.Query, r.Corpus),
			Retrieval: &runtime.RetrievalSpec{Corpus: r.Corpus, Query: r.Query},
			// S33 T4: a retrieval is material the step was GIVEN, resolved once
			// at assembly — injected_context, not the handler's own text.
			Category: runtime.CategoryInjectedContext,
			// S68: given material reasons in the USER message.
			Destination: runtime.SectionTranscript,
		})
	}
	if !model.ValidReasoningEffort(strings.TrimSpace(p.ReasoningEffort)) {
		return nil, ErrBadParams{Err: fmt.Errorf(
			"reasoningEffort %q is not one of %s", p.ReasoningEffort,
			strings.Join(model.ReasoningEfforts(), ", "))}
	}

	// S69 AT5: the last moment, and the only one. References resolve to
	// bytes through the daemon-wired expander just before the request
	// compiles; a revision the store no longer has speaks its absence (the
	// sentence lands in the step's notes below) instead of rendering as a
	// silent hole — ruling 3, and P31's missing-is-not-zero.
	var promptBlocks []model.Block
	var histBlocks [][]model.Block
	var attMissing []string
	if len(p.Attachments) > 0 && p.ComponentRoot == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("attachments need a componentRoot to resolve in — references without a scope are claims, not references")}
	}
	if t.AttachmentExpand != nil {
		if len(p.Attachments) > 0 {
			b, miss := t.AttachmentExpand(p.ComponentRoot, loopSlugOr(p.LoopSlug), p.Attachments)
			promptBlocks, attMissing = b, miss
		}
		if len(p.History) > 0 {
			histBlocks = make([][]model.Block, len(p.History))
			for i, h := range p.History {
				if len(h.Attachments) == 0 {
					continue
				}
				b, miss := t.AttachmentExpand(p.ComponentRoot, loopSlugOr(p.LoopSlug), h.Attachments)
				histBlocks[i], attMissing = b, append(attMissing, miss...)
			}
		}
	}
	compiled := t.Runner.Compile(ctx, runtime.CompileInput{
		Handler:           handler,
		Granted:           granted,
		Route:             route,
		WorkspaceRoot:     p.WorkspaceRoot,
		MaxIterations:     p.MaxIterations,
		StepMaxIterations: p.StepMaxIterations,
		ToolSpecs:         specs,
		ReasoningEffort:   strings.TrimSpace(p.ReasoningEffort),
		Thinking:          p.Thinking,
		// S68: the three new halves ride the compile input whole. History
		// maps at this one boundary (protocol shape → model shape); the
		// runtime owns every rule about what may ride it.
		TaskFrame:    p.TaskFrame,
		ChosenSkills: p.Skills,
		// S75: which loop this step belongs to, so the daemon can look up the
		// loop's active todo list at compile.
		LoopSlug:      p.LoopSlug,
		ComponentRoot: p.ComponentRoot,
		History:       historyMessages(p.History, histBlocks),
		// S69 AT5: the ONE expansion point. Pinned references become
		// base64 content blocks immediately before the request compiles —
		// nowhere else, never earlier: the rows keep references and the
		// replay path expands the same pinned revisions, so live and
		// replay agree by construction, not by discipline.
		PromptAttachments: promptBlocks,
	}, runtime.ContextDeps{})
	if !compiled.OK {
		// A construction failure, before any model call. The reason names the
		// handler and the missing piece.
		return nil, ErrStepNotCompiled.WithWhat("%s", compiled.Reason)
	}

	loopSlug := p.LoopSlug
	if loopSlug == "" {
		loopSlug = "step"
	}

	// 1. The row FIRST, with status='running' — so a crash leaves something
	//    S7 can find.
	stepID, err := t.Steps.CreateRunningStep(loopSlug, p.WorkspaceRoot, handler.Name)
	if err != nil {
		return nil, ErrProviderFailed.WithWhat("the step could not be recorded: %v", err)
	}
	// The row's id goes ON the request (S10): it is what a refusal is
	// attributed to downstream (RecordDisagreement's charge-sheet field), and
	// it is assigned here — by whoever created the row — because an identity
	// read from anywhere the model can write is an identity the model chose
	// (PL1). Every checkpoint, snapshot and gate question from this step
	// carries it.
	compiled.Request.StepID = stepID
	// S68 B6: a chosen skill that could not ride is PRINTED on the step's
	// opening line — the receipt says what actually rode. The sentence was
	// composed by the daemon door that resolved the names; here it is only
	// appended to the notes the opening summary already carries (the same
	// channel S14's notes use; nothing compiles from notes, so the prompt
	// bytes are untouched).
	if p.ChosenSkillsNote != "" {
		compiled.Request.Notes = append(compiled.Request.Notes, p.ChosenSkillsNote)
	}
	// S69 ruling 3: an attachment the store no longer has is SPOKEN — the
	// turn runs with its words and the note names exactly what is gone.
	// Nothing about the turn is silently diminished; the absence itself is
	// part of what the operator is owed.
	for _, m := range attMissing {
		compiled.Request.Notes = append(compiled.Request.Notes, m)
	}
	// Q3: ride the MCP offer's withheld candidate lines additively on the
	// step.started event. The composite caller (if one was built at BuildTools
	// above) exposes them; the first-party caller has no such method, so the
	// assertion is a no-op when no MCP bundle is wired (MD8: the event is
	// byte-identical to today). One sentence per withheld candidate (MD9).
	if wp, ok := toolset.(interface{ Withheld() []string }); ok {
		compiled.Request.Withheld = wp.Withheld()
	}
	if created, ok := ctx.Value(blueprintStepCreatedKey{}).(func(int64) error); ok && created != nil {
		if err := created(stepID); err != nil {
			return nil, ErrProviderFailed.WithWhat("the blueprint could not attach step %d: %v", stepID, err)
		}
	}

	// The sink is built now that the id exists, so EVERY event it publishes —
	// the first included — carries `stepId`, the step's `agent` and the
	// project root the events are filed under (S31 D1). A loop step gets the
	// real publisher even with nobody subscribed: the event log is what a
	// subscriber opened later replays, and an empty root would make the stream
	// match every project-filtered subscriber there is (CODEMAP (g)). An
	// ad-hoc step with no subscription keeps discarding — its events still
	// reach nothing, which is what a step that asked for no stream means.
	var sink runtime.EventSink = runtime.DiscardSink{}
	if sub != nil {
		sink = t.stepSink(stepID, p.WorkspaceRoot, handler.Name)
	}
	if id, ok := ctx.Value(loopStepIdentityKey{}).(LoopStepIdentity); ok {
		sink = t.loopStepSink(stepID, p.WorkspaceRoot, id, handler.Name)
	}

	// 2. Run.
	writer := t.recoveryWriter(stepID, loopSlug)
	var receipts runtime.ReceiptWriter
	var checkpoints runtime.CheckpointWriter
	if writer != nil {
		receipts, checkpoints = writer, writer
		if err := writer.Save(ctx, runtime.Checkpoint{Request: compiled.Request,
			Messages: compiled.Request.Messages, Manifest: compiled.Request.Manifest}); err != nil {
			return nil, ErrProviderFailed.WithWhat("the opening recovery checkpoint could not be saved: %v", err)
		}
	}
	// Tracked under its OWN cancellable context, derived from the connection's
	// but independent of it: a separate `step.stop` call, on a separate
	// connection, has to be able to reach exactly this run and no other.
	// `TrackStep` is refused rather than skipped when absent — a step that
	// cannot be tracked cannot be stopped, and running it anyway would make
	// the stop button a lie.
	if t.TrackStep == nil {
		return nil, ErrStepUnavailable.WithWhat("this daemon cannot track a step for stopping")
	}
	runCtx, doneTracking := t.TrackStep(ctx, stepID)
	defer doneTracking()

	result := t.Runner.Run(runCtx, compiled.Request, runtime.RunOpts{
		Client:       t.Models,
		Tools:        toolset,
		Sink:         sink,
		HandoffSpec:  runtime.DefaultHandoffSpec(),
		Receipts:     receipts,
		Checkpoint:   checkpoints,
		Policy:       t.PolicyGate,
		Steers:       t.steerSource(stepID),
		MailboxDrain: t.mailboxDrainFor(stepID),
		MailboxAsk:   t.mailboxAskFor(stepID),
		// S75: the nudge and the completion guard, bound to this step (the lane
		// rides the loop identity on the context; a turn has none).
		Todo: t.todoPolicyFor(ctx, stepID),
		// S33 T2: the capture seam rides every RunOpts the table builds —
		// explicitly, never left to a zero value by omission (plan Q2); the
		// daemon sets the value (daemon.go), and nil is the OFF decision.
		Telemetry: t.Telemetry,
		// S55 T7 (E2): the in-step budget check rides the same seam (I2):
		// nil means unenforced, the pre-T7 path byte for byte.
		Budget: t.budgetCheck(loopSlug, p.WorkspaceRoot),
	})
	// Read ONCE, right after the run that id belongs to — see stepcancel.go
	// for why this, and not the result's own text, is the truth of whether a
	// user asked for this stop.
	stopped := t.StepWasStopped != nil && t.StepWasStopped(stepID)
	// S55 T4 (LC19): the shutdown's truth is the run context's CAUSE, read
	// ONCE here beside the stop bit — the same one-place rule, different
	// fact. A person's stop wins over the shutdown (checked first, below);
	// a real question the model asked also wins (the suspended branch
	// commits it). Only a run the drain cancelled with the daemon's typed
	// cause reaches the shutdown branch, where the row is left running at
	// its last checkpoint and nothing is committed: the recovery walk
	// resumes it cold on the next start, and a step is never committed
	// failed because the daemon went away.
	shutDown := !stopped && t.ShutdownCause != nil && t.ShutdownCause(runCtx)

	// Save history before the question. A failed history write prevents Ask,
	// leaving a running row for S7 to reconcile.
	var persistErrs []string
	if writer == nil {
		if err := t.Steps.AppendStepMessages(stepID, result.Messages); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the transcript: %v", err))
		}
		if manifest, err := json.Marshal(stepSnapshot{ContextManifest: result.Manifest, Request: compiled.Request, LoopSlug: loopSlug, Usage: result.Usage, Compactions: result.Compactions}); err == nil {
			if err := t.Steps.SaveStepContext(stepID, manifest); err != nil {
				persistErrs = append(persistErrs, fmt.Sprintf("the manifest: %v", err))
			}
		} else {
			persistErrs = append(persistErrs, fmt.Sprintf("the manifest: %v", err))
		}
	}
	status := "done"
	if stopped {
		// A PERSON asked for this, not the model — `suspended` for the same
		// reason a real question is (P2: no process, resumable), told apart
		// by `pause_reason` rather than by a different status (see migration
		// `0020`). No question to ask, so none of the branch below applies.
		status = "suspended"
		if err := t.Steps.SetPauseReason(stepID, "user"); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the pause reason: %v", err))
		}
		if err := t.Steps.FinishStep(stepID, status, "", result.Cost); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the paused status: %v", err))
		} else if t.OnEvent != nil {
			// S31 T2 (D6): brief §5's `step.stopped`, replacing the S9-era
			// `step.paused` (nothing consumed the old name — one event per
			// stop, not two). It carries the identity D1 put on every step
			// event plus `loopId` when the step belongs to a loop, and is
			// filed under the step's own project root.
			evp := StepEventPayload{StepID: stepID, Agent: handler.Name, LoopID: t.loopIDFor(loopSlug, p.WorkspaceRoot)}
			evp.componentRoot = p.WorkspaceRoot
			t.OnEvent("step.stopped", evp)
		}
	} else if result.Kind == runtime.ResultSuspended && result.PauseReason == "cap" {
		// S55 T7 (E2): an in-step budget hit. The step parks suspended with
		// pause_reason='cap' and NO question row — a cap has no one to ask
		// (the unattended shape; the answer to a cap is a raise, not a
		// reply). It is checked BEFORE the question branch, which would
		// otherwise file the nil question as a persistence error.
		status = "suspended"
		if err := t.Steps.SetPauseReason(stepID, "cap"); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the pause reason: %v", err))
		}
		if err := t.Steps.FinishStep(stepID, status, "", result.Cost); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the paused status: %v", err))
		} else if t.OnEvent != nil {
			evp := StepEventPayload{StepID: stepID, Agent: handler.Name, LoopID: t.loopIDFor(loopSlug, p.WorkspaceRoot)}
			evp.componentRoot = p.WorkspaceRoot
			t.OnEvent("step.capped", evp)
		}
	} else if result.Kind == runtime.ResultSuspended {
		status = "suspended"
		if result.Question == nil || t.Questions == nil || len(persistErrs) > 0 {
			persistErrs = append(persistErrs, "the question was not saved because its transcript or manifest was unavailable")
		} else if id, err := t.Questions.AskStep(stepID, loopSlug, p.WorkspaceRoot, *result.Question); err != nil {
			persistErrs = append(persistErrs, fmt.Sprintf("the question: %v", err))
		} else {
			result.Question.ID = id
			result.Question.StepID = stepID
			if err := t.Steps.FinishStep(stepID, status, "", result.Cost); err != nil {
				persistErrs = append(persistErrs, fmt.Sprintf("the suspended status: %v", err))
			} else if t.OnEvent != nil {
				t.OnEvent(KindQuestionAsked, result.Question)
			}
		}
	} else if shutDown {
		// S55 T4 (LC19): the daemon's drain cancelled this run. The row is
		// left RUNNING at its last checkpoint — the runtime saves one every
		// turn, and the opening checkpoint was written before the first
		// call — so the recovery walk has exactly what a crash would have
		// left and resumes it cold on the next start. No FinishStep (a
		// committed outcome would be a failure that is really an exit), no
		// event (the drain's one-line report is the log). The row's status
		// stays "running" because nothing below may write it.
	} else if err := t.Steps.FinishStep(stepID, status, string(result.Outcome), result.Cost); err != nil {
		persistErrs = append(persistErrs, fmt.Sprintf("the outcome: %v", err))
	}

	out := StepResult{
		StepID:       stepID,
		Kind:         string(result.Kind),
		Outcome:      string(result.Outcome),
		Reason:       result.Reason,
		Usage:        result.Usage,
		Cost:         result.Cost,
		Iterations:   result.Manifest.IterationsUsed,
		Question:     result.Question,
		SinkFailures: result.SinkFailures,
		// A route switch the step OWED to the next step (S18 T8, O5/N9):
		// refused mid-step, carried out as data for the loop to apply —
		// route is loop state, never the step's to write.
		PendingRoute:  result.PendingRoute,
		PendingEffort: result.PendingEffort,
	}
	if stopped {
		// The RESPONSE reflects what was actually written, not whatever
		// generic "cancelled" shape the runtime's own ctx.Err() handling
		// produced — that text is still true (the call WAS cancelled), but
		// `suspended`/no outcome is what a caller polling `step.show` will
		// see, and the two must agree.
		out.Kind = "suspended"
		out.Outcome = ""
		out.Reason = "stopped by the user; call step.continue to resume it"
		out.PauseReason = "user"
	}
	if result.Kind == runtime.ResultSuspended && result.PauseReason == "cap" && len(persistErrs) == 0 {
		// The same one rule as the stop: the response reflects what was
		// written. The step is suspended with pause_reason='cap' and no
		// question — the only door that moves it is loop.raise, and the
		// sentence says so instead of promising an answer that will never
		// come.
		out.Kind = "suspended"
		out.Outcome = ""
		out.Reason = "the step hit a budget cap; raise it with loop.raise"
		out.PauseReason = "cap"
		out.CapHit = result.CapHit
	}
	if result.Handoff != nil {
		if encoded, err := json.Marshal(result.Handoff); err == nil {
			out.Handoff = encoded
		}
	}
	if len(persistErrs) > 0 {
		// Appended to the reason rather than replacing it: what the step DID is
		// the more important fact, and a caller needs both.
		out.Reason = strings.TrimSpace(out.Reason + fmt.Sprintf(
			" [the step ran, and parts of it could not be saved: %s]", strings.Join(persistErrs, "; ")))
		if result.Kind == runtime.ResultSuspended {
			out.Kind = string(runtime.ResultComplete)
			out.Outcome = string(runtime.OutcomeFailed)
			if out.Question != nil && out.Question.ID == 0 {
				out.Question = nil
			}
		}
	}
	return out, nil
}

func (t *Table) continueAnsweredStep(stepID int64, loopSlug string, runner resumeRunner, req runtime.ResumeRequest, toolset runtime.ToolCaller) {
	writer := t.recoveryWriter(stepID, loopSlug)
	var receipts runtime.ReceiptWriter
	var checkpoints runtime.CheckpointWriter
	if writer != nil {
		receipts, checkpoints = writer, writer
	}
	// The row's id is re-asserted server-side (S10): the snapshot may predate
	// the field, and a disagreement must be charged to the step being
	// continued, not to whatever the stored bytes happened to carry.
	req.Request.StepID = stepID
	// Tracked the same way the first run was (handleStep): a resumed step can
	// be stopped again, and `step.stop` reaches it by the same id either way.
	runCtx := context.Background()
	var doneTracking func()
	if t.TrackStep != nil {
		runCtx, doneTracking = t.TrackStep(runCtx, stepID)
		defer doneTracking()
	}
	result := runner.Resume(runCtx, req, runtime.RunOpts{Client: t.Models, Tools: toolset,
		Receipts: receipts, Checkpoint: checkpoints, Policy: t.PolicyGate, PolicySet: t.PolicySet,
		Steers: t.steerSource(stepID), MailboxDrain: t.mailboxDrainFor(stepID), MailboxAsk: t.mailboxAskFor(stepID), Telemetry: t.Telemetry,
		// S75: a resumed step has no lane on hand, so its guard stays off (the
		// safe direction); the nudge still applies.
		Todo: t.todoPolicyFor(context.Background(), stepID),
		// S55 T7 (E2): the re-run of a raised (or re-run) step is a step like
		// any other — the in-step check rides this resume too, and a second
		// hit suspends it again (the hand-back above says `capped`).
		Budget: t.budgetCheck(loopSlug, req.Request.WorkspaceRoot),
		Sink: runtime.SinkFunc(func(e runtime.StepEvent) {
			if t.OnEvent == nil {
				return
			}
			// The same D6 suppression the two named sinks apply: a stopped
			// step publishes no `step.done` from ANY path — including the
			// resumed run this continue just started, whose cancellation the
			// stop button caused and whose terminal event would otherwise
			// accuse it of failing.
			if e.Kind == runtime.EventStepDone && t.stopWillTellTheStory(stepID) {
				return
			}
			p := stepEventPayload(e, stepID, req.Request.Handler.Name)
			p.componentRoot = req.Request.WorkspaceRoot
			t.OnEvent(e.Kind, p)
		}), HandoffSpec: runtime.DefaultHandoffSpec()})
	var failures []string
	if err := t.Questions.ReplaceStepMessages(stepID, result.Messages); err != nil {
		failures = append(failures, "transcript: "+err.Error())
	}
	if raw, err := json.Marshal(stepSnapshot{ContextManifest: result.Manifest, Request: req.Request, LoopSlug: loopSlug, Usage: result.Usage, Compactions: result.Compactions}); err != nil {
		failures = append(failures, "manifest: "+err.Error())
	} else if err := t.Steps.SaveStepContext(stepID, raw); err != nil {
		failures = append(failures, "manifest: "+err.Error())
	}
	// Read ONCE, right after the run it belongs to — same rule as handleStep.
	stopped := t.StepWasStopped != nil && t.StepWasStopped(stepID)
	outcome := string(result.Outcome)
	status := "done"
	if stopped {
		status = "suspended"
		outcome = ""
		if err := t.Steps.SetPauseReason(stepID, "user"); err != nil {
			failures = append(failures, "pause reason: "+err.Error())
		}
	} else if result.Kind == runtime.ResultSuspended && result.PauseReason == "cap" {
		// S55 T7 (E2): the continued run hit the cap again (a second hit
		// during the raise's re-run, or a re-run that never raised). The
		// row finishes suspended with pause_reason='cap' and NO question
		// row — a cap has no one to ask, and no question.asked event
		// follows (the branch below never sees it; the hand-back says
		// `capped`, and the loop's arm re-blocks on cap-hit).
		status = "suspended"
		if err := t.Steps.SetPauseReason(stepID, "cap"); err != nil {
			failures = append(failures, "pause reason: "+err.Error())
		}
	} else if result.Kind == runtime.ResultSuspended {
		status = "suspended"
		if result.Question == nil || len(failures) > 0 {
			failures = append(failures, "pending question could not be saved")
		} else if id, err := t.Questions.AskStep(stepID, loopSlug, req.Request.WorkspaceRoot, *result.Question); err != nil {
			failures = append(failures, "question: "+err.Error())
		} else {
			result.Question.ID = id
			result.Question.StepID = stepID
		}
	}
	if len(failures) == 0 {
		if err := t.Steps.FinishStep(stepID, status, outcome, result.Cost); err != nil {
			failures = append(failures, "status: "+err.Error())
		}
	}
	if t.OnEvent != nil {
		if len(failures) > 0 {
			t.OnEvent("step.persistence_failed", map[string]any{"stepId": stepID, "reason": strings.Join(failures, "; ")})
		}
		if stopped && len(failures) == 0 {
			// The same event the first run emits for a stop (S31 D6), so the
			// UI sees ONE shape for "this step was stopped" whichever run
			// incarnation it happened in.
			evp := StepEventPayload{StepID: stepID, Agent: req.Request.Handler.Name, LoopID: t.loopIDFor(loopSlug, req.Request.WorkspaceRoot)}
			evp.componentRoot = req.Request.WorkspaceRoot
			t.OnEvent("step.stopped", evp)
		} else if status == "suspended" && len(failures) == 0 {
			t.OnEvent(KindQuestionAsked, result.Question)
		}
		if status == "done" && len(failures) == 0 {
			t.OnEvent("step.saved", map[string]any{"stepId": stepID, "outcome": result.Outcome})
		}
	}
	if len(failures) == 0 && t.OnStepContinued != nil {
		questionID := int64(0)
		if result.Question != nil {
			questionID = result.Question.ID
		}
		kind := string(result.Kind)
		if stopped {
			// The handback must say what ACTUALLY happened — the person
			// stopped this continued run too. Passing the runtime's generic
			// cancelled kind through would let the loop record a "completed"
			// ladder move for a step that never completed. `stopped` sends
			// CompleteSuspendedStep down its re-park branch (D6).
			kind = "stopped"
		} else if result.PauseReason == "cap" {
			// S55 T7: the continued run hit the cap again. `capped` sends
			// CompleteSuspendedStep down its re-block branch (the loop
			// parks blocked/cap-hit with the NEW hit; only loop.raise
			// moves it). Passing the suspended kind through would make the
			// loop record a completed ladder move for a step that hit a wall.
			kind = "capped"
		}
		cost, _ := json.Marshal(result.Cost)
		// The mid-step route switch this run owed (S18 T8, O5): the handback
		// carries it so the LOOP applies it — route is loop state, never the
		// runtime's to write.
		t.OnStepContinued(stepID, kind, string(result.Outcome), result.Reason, questionID, cost, result.PendingRoute)
	}
}

// budgetCheck binds the table's S55 T7 budget seam to one step's loop (E2).
// Nil-tolerant twice over, the steerSource shape: no Budget seam (storeless
// daemon, or T7 unwired) means unenforced — the pre-T7 path, byte for byte
// (I2) — and a slug-and-root the loop registry does not resolve answers ""
// for the loop id, which the daemon's adapter treats as "no loop, no
// budgets": a step outside a loop has no loop-scoped row to spend against.
func (t *Table) budgetCheck(loopSlug, componentRoot string) func(context.Context) (runtime.BudgetVerdict, error) {
	if t.Budget == nil {
		return nil
	}
	return func(ctx context.Context) (runtime.BudgetVerdict, error) {
		return t.Budget(ctx, t.loopIDFor(loopSlug, componentRoot))
	}
}

// loopIDFor names the loop behind a step's slug-and-root for event payloads
// (S31 D6). Absent seam, empty slug or empty root returns "": the loopId
// field is then absent from the event, honestly — never guessed.
// steerSource binds the runtime's turn-boundary drain to one step (S31 T4).
// Nil-tolerant twice over: no factory (daemon without the wiring) and a
// factory that answers nil both mean the same honest thing — this step is
// running unsteered, and the pre-T4 loop runs untouched.
func (t *Table) steerSource(stepID int64) runtime.SteerSource {
	if t.Steers == nil {
		return nil
	}
	return t.Steers(stepID)
}

// mailboxDrainFor and mailboxAskFor bind S70's two seams to one step, with
// the same double nil-tolerance as steerSource: no factory, or a factory
// answering nil, both mean the step simply has no mailbox (I2).
// todoPolicyFor binds S75's nudge and guard to one step. Nil means the daemon
// wires none and the step runs as it did before the feature (I2).
func (t *Table) todoPolicyFor(ctx context.Context, stepID int64) runtime.TodoPolicy {
	if t.TodoPolicy == nil {
		return nil
	}
	lane := ""
	if id, ok := ctx.Value(loopStepIdentityKey{}).(LoopStepIdentity); ok {
		lane = id.Lane
	}
	return t.TodoPolicy(stepID, lane)
}

func (t *Table) mailboxDrainFor(stepID int64) runtime.MailboxDrain {
	if t.MailboxDrain == nil {
		return nil
	}
	return t.MailboxDrain(stepID)
}

func (t *Table) mailboxAskFor(stepID int64) runtime.MailboxAskDoor {
	if t.MailboxAsk == nil {
		return nil
	}
	return t.MailboxAsk(stepID)
}

func (t *Table) loopIDFor(slug, root string) string {
	if t.LookupLoopID == nil || slug == "" || root == "" {
		return ""
	}
	return t.LookupLoopID(slug, root)
}

// routeForStep picks the route.
//
// With one configured route and no explicit choice, it takes that one — which
// is what lets the demo be a single command on a fresh machine. With several it
// REFUSES rather than guessing: picking one would spend money on a model the
// person did not choose.
func (t *Table) routeForStep(id string) (model.Route, error) {
	if t.Routes == nil {
		return model.Route{}, ErrProviderFailed.WithWhat("this daemon has no routes configured; " +
			"`aeon routes set` configures one")
	}
	if id != "" {
		route, ok := t.Routes.Route(id)
		if !ok {
			return model.Route{}, ErrUnknownRoute.WithWhat(
				"route %q is not configured; the configured routes are %s",
				id, strings.Join(t.Routes.RouteIDs(), ", "))
		}
		return route, nil
	}

	ids := t.Routes.RouteIDs()
	switch len(ids) {
	case 0:
		return model.Route{}, ErrUnknownRoute.WithWhat("no routes are configured; " +
			"`aeon routes set --id … --provider … --model …` configures one")
	case 1:
		route, _ := t.Routes.Route(ids[0])
		return route, nil
	default:
		return model.Route{}, ErrUnknownRoute.WithWhat(
			"this daemon has %d routes (%s) and the step did not name one. It is refused rather "+
				"than guessed, because guessing spends money on a model you did not choose",
			len(ids), strings.Join(ids, ", "))
	}
}

// RouteForLaunch answers "which route will this start's steps speak through",
// reusing routeForStep's rules verbatim (named id, else the single configured
// route, else the named refusal) so the S77 launch-attachment check at the
// start door and the first step's dispatch can never disagree about the
// default: one rule, two callers.
func (t *Table) RouteForLaunch(id string) (model.Route, error) {
	return t.routeForStep(id)
}

// stepSink forwards runtime events to a subscription as ordinary protocol
// events (K24, D17).
//
// # Why they are ordinary events and not a new mechanism
//
// S2 already has subscriptions, filters and a cursor. A second streaming path
// for steps would mean two things to subscribe to, two cursors, and two places
// for a client to miss something — and `PROTOCOL_VERSION` is unchanged by this
// addition, because a new event kind is additive.
// # The subscription is the GATE, not the route
//
// The caller only builds this sink when the step asked to stream (its
// SubscriptionID resolved on that connection); delivery itself goes through
// `OnEvent`, which fans out to EVERY matching subscription — one log, one
// filter, no second stream to keep coherent.
func (t *Table) stepSink(stepID int64, componentRoot, agent string) runtime.EventSink {
	return runtime.SinkFunc(func(e runtime.StepEvent) {
		if t.OnEvent == nil {
			return
		}
		if e.Kind == runtime.EventStepDone && t.stopWillTellTheStory(stepID) {
			return
		}
		p := stepEventPayload(e, stepID, agent)
		p.componentRoot = componentRoot
		t.OnEvent(e.Kind, p)
	})
}

// stopWillTellTheStory is D6's suppression rule at the one place every step
// event passes: once a person has stopped a step, the runtime's terminal
// `step.done` — whatever outcome it carries — is a lie about that step. The
// run that produced it was interrupted mid-sentence (outcome `failed`), or
// completed in the race before the cancellation landed (outcome `ok`); the
// stored row says `suspended` with pause reason `user`, and the one event
// that matches that truth is the `step.stopped` handleStep emits after
// persisting it. Consulted through the daemon's single-read stop flag — no
// string-sniffing of the runtime's reason.
func (t *Table) stopWillTellTheStory(stepID int64) bool {
	return t.StepIsStopped != nil && t.StepIsStopped(stepID)
}

// loopStepSink publishes a loop step's runtime events (S31 D1/D2): every
// event carries the step id, the agent running the step, the loop move it
// executes (`loopId`, `lane`, blueprint `step`) and the step's project ROOT.
// The root is the load-bearing field: an event with an EMPTY root matches
// every project-filtered subscriber (CODEMAP (g)), so publishing loop steps
// with "" leaked every loop's stream into every project's view.
//
// There is no subscription gate here: a loop step publishes whether or not
// anybody is watching, because the durable log is what a subscriber opened
// mid-run (or after a reconnect) replays.
func (t *Table) loopStepSink(stepID int64, componentRoot string, id LoopStepIdentity, agent string) runtime.EventSink {
	return runtime.SinkFunc(func(e runtime.StepEvent) {
		if t.OnEvent == nil {
			return
		}
		if e.Kind == runtime.EventStepDone && t.stopWillTellTheStory(stepID) {
			return
		}
		p := stepEventPayload(e, stepID, agent)
		p.componentRoot = componentRoot
		p.LoopID = id.LoopID
		p.Lane = id.Lane
		p.BlueprintStep = id.Step
		t.OnEvent(e.Kind, p)
	})
}

// stepEventPayload is the ONE construction of a step-event envelope. The id
// and the agent ride EVERY event; keeping it one function is what stops a new
// sink (T2's stop path, S32's parallel steps) from "forgetting" a field the
// way stepSink forgot StepID for three slices.
func stepEventPayload(e runtime.StepEvent, stepID int64, agent string) StepEventPayload {
	// T5/D1, stated ONCE here rather than at every call site: every step
	// event carries the agent, and an agent nobody named reads `default` —
	// the honest name for "the step ran as the default handler", and never
	// an empty string a chat screen would have to invent a label for.
	if agent == "" {
		agent = "default"
	}
	return StepEventPayload{
		StepID:         stepID,
		Agent:          agent,
		Tool:           e.Tool,
		MessageID:      e.MessageID,
		Text:           e.Text,
		Summary:        e.Summary,
		Skills:         e.Skills,
		Result:         e.Result,
		IsError:        e.IsError,
		Iteration:      e.Iteration,
		Outcome:        string(e.Outcome),
		Reason:         e.Reason,
		Start:          e.Start,
		End:            e.End,
		EstimateBefore: e.EstimateBefore,
		EstimateAfter:  e.EstimateAfter,
		Cost:           e.Cost,

		// S31 T4's steer vocabulary (brief §5): the row a delivery names, the
		// calls a steer cost the turn, and the pair a route change moved.
		SteerID:         e.SteerID,
		SkippedTools:    e.SkippedTools,
		ToolUseID:       e.ToolUseID,
		Route:           e.Route,
		ReasoningEffort: e.ReasoningEffort,
		FromIteration:   e.FromIteration,
		Verdict:         e.Verdict,
		Command:         e.Command,
		Output:          e.Output,
		OutputTruncated: e.OutputTruncated,
		Path:            e.Path,
		LinesAdded:      e.LinesAdded,
		LinesRemoved:    e.LinesRemoved,
		DurationMs:      e.DurationMs,
		Detail:          e.Detail,
		DetailTruncated: e.DetailTruncated,
		ApprovedBy:      e.ApprovedBy,
		Thinking:        e.Thinking,
		Usage:           e.Usage,
		TurnCost:        e.TurnCost,
		StepCost:        e.StepCost,
		WallMs:          e.WallMs,
		StopReason:      e.StopReason,
		ToolCalls:       e.ToolCalls,
	}
}

// StepEventPayload is what a `step.*` event carries.
//
// S31 (D1) adds the identity the loop-chat screen reads live: `stepId` on
// EVERY event (the resume sink always set it; the ad-hoc stepSink dropping
// it was the bug), `agent` — the handler the step runs as — and, for steps
// the loop engine runs, `loopId`, `lane` and the blueprint `step` that link
// the stream to the ladder move whose `loop.step.dispatched` opened it (D2).
type StepEventPayload struct {
	StepID        int64  `json:"stepId,omitempty"`
	LoopID        string `json:"loopId,omitempty"`
	Lane          string `json:"lane,omitempty"`
	BlueprintStep string `json:"step,omitempty"`
	Agent         string `json:"agent,omitempty"`
	Tool          string `json:"tool,omitempty"`
	// MessageID names the agent_message row an ask or a delivered message
	// speaks of (S70 MB6), additive (MD7) and present only on those events
	// — the runtime's StepEvent carries it from the seam that acted.
	MessageID int64  `json:"messageId,omitempty"`
	Text      string `json:"text,omitempty"`
	Summary   string `json:"summary,omitempty"`
	// Skills is S66's injection marker, additive (MD7) and present only
	// on step.started: the chat renders it as its own row, and the
	// transcript replay reads the SAME field from the SAME stored event,
	// so live and reload cannot diverge. nil = nothing was injected.
	Skills *runtime.StepSkills `json:"skills,omitempty"`
	// Attachments decorates step.steer.queued with the filed receipts
	// (S69 AT8) — present only when files rode, so every other event
	// frame stays byte-identical (D7).
	Attachments    []AttachmentReceiptView `json:"attachments,omitempty"`
	Result         string                  `json:"result,omitempty"`
	IsError        bool                    `json:"isError,omitempty"`
	Iteration      int                     `json:"iteration,omitempty"`
	Outcome        string                  `json:"outcome,omitempty"`
	Reason         string                  `json:"reason,omitempty"`
	Start          int                     `json:"start,omitempty"`
	End            int                     `json:"end,omitempty"`
	EstimateBefore int                     `json:"estimateBefore,omitempty"`
	EstimateAfter  int                     `json:"estimateAfter,omitempty"`
	Cost           *model.Cost             `json:"cost,omitempty"`

	// S31 T4 (D3/D5): the steer events' fields. A `step.steer.delivered`
	// names the row (`steerId`) and, at a tool-skipping boundary, the exact
	// calls it closed (`skippedTools`); `step.tool.skipped` answers to one
	// `toolUseId` with reason `steered`; `step.route.changed` announces the
	// new `route`/`reasoningEffort` pair and the iteration it applies FROM —
	// the next call, never the in-flight one.
	SteerID         int64    `json:"steerId,omitempty"`
	SkippedTools    []string `json:"skippedTools,omitempty"`
	ToolUseID       string   `json:"toolUseId,omitempty"`
	Route           string   `json:"route,omitempty"`
	ReasoningEffort string   `json:"reasoningEffort,omitempty"`
	FromIteration   int      `json:"fromIteration,omitempty"`

	// Path and the line counts enrich `step.tool.end` for write-shaped
	// results (S31 D8): `{stepId, tool, toolUseId, path, linesAdded,
	// linesRemoved}` — what the call cost the file, in the stream, without
	// anyone re-reading the file. Zero for every tool not about a file and
	// for calls that never ran (a steered skip publishes no tool.end at all).
	Path         string `json:"path,omitempty"`
	LinesAdded   int    `json:"linesAdded,omitempty"`
	LinesRemoved int    `json:"linesRemoved,omitempty"`

	// Verdict is `step.policy.decided`'s: allowed | denied | asked, with the
	// verdict sentence in Reason.
	Verdict string `json:"verdict,omitempty"`

	// Command / Output are a bash call's whole command (on step.tool.start)
	// and its capped output (on step.tool.end) — see runtime.StepEvent.
	Command         string `json:"command,omitempty"`
	Output          string `json:"output,omitempty"`
	OutputTruncated bool   `json:"outputTruncated,omitempty"`

	// The replay fields — see runtime.StepEvent. A tool call's duration and whole (capped) result, who approved an
	// asked call, and a finished model turn's text, thinking, usage, cost, wall time and stop reason.
	DurationMs      int64        `json:"durationMs,omitempty"`
	Detail          string       `json:"detail,omitempty"`
	DetailTruncated bool         `json:"detailTruncated,omitempty"`
	ApprovedBy      string       `json:"approvedBy,omitempty"`
	Thinking        string       `json:"thinking,omitempty"`
	Usage           *model.Usage `json:"usage,omitempty"`
	TurnCost        *model.Cost  `json:"turnCost,omitempty"`
	StepCost        *model.Cost  `json:"stepCost,omitempty"`
	WallMs          int64        `json:"wallMs,omitempty"`
	StopReason      string       `json:"stopReason,omitempty"`
	ToolCalls       int          `json:"toolCalls,omitempty"`

	// HasText serves `step.steer.queued` (brief §5): the chat screen shows
	// "you added an instruction" versus "you changed the provider" without
	// the instruction's text riding the stream a second time — the text is
	// durable in the steer row and, from the next boundary, in the
	// transcript. The event announces; the row remembers.
	HasText bool `json:"hasText,omitempty"`

	// componentRoot is deliberately NOT a wire field: the Event envelope owns
	// the project root, and repeating it inside the payload would be a second
	// copy free to drift. It rides unexported to the daemon's publisher
	// through ComponentRooted — which is how OnEvent gained a project root for
	// step events without a signature change that would have rippled through
	// every unrelated emitter in this package.
	componentRoot string
}

// EventComponentRoot implements ComponentRooted.
func (p StepEventPayload) EventComponentRoot() string { return p.componentRoot }

// loopBaseTools are what a loop-bound step is always given beside its own grant. The three read-only doors to the
// loop's documents and index, because the loop's files are in the database; and `question`, because an agent that
// has a doubt must be able to ask the person instead of guessing (a step that cannot ask writes its open questions
// into a document nobody answers, which is how a planner ended up with no answers). Nothing here writes.
var loopBaseTools = []string{"doc_read", "doc_list", "loop_context", "question"}

// loopCodeTools are the code-understanding doors: the search index and the code graph. They exist only when the
// daemon wired them, so they are added only when registered. An agent without them greps blind and says "I have no
// code graph tool" about a project whose graph was built.
var loopCodeTools = []string{"search", "graph"}

// withLoopTools returns granted plus the loop tools it does not already hold, in a new slice. registered says
// whether an optional tool exists in this daemon (nil means none of them do).
func withLoopTools(granted []string, registered func(string) bool) []string {
	have := map[string]bool{}
	out := make([]string, 0, len(granted)+len(loopBaseTools)+len(loopCodeTools))
	for _, name := range granted {
		have[name] = true
		out = append(out, name)
	}
	add := func(name string) {
		if !have[name] {
			have[name] = true
			out = append(out, name)
		}
	}
	for _, name := range loopBaseTools {
		add(name)
	}
	if registered != nil {
		for _, name := range loopCodeTools {
			if registered(name) {
				add(name)
			}
		}
	}
	return out
}
