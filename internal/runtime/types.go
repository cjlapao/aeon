// Package runtime runs one bounded step: a model, a declared context, a serial
// tool loop, and one of four outcomes.
//
// # What it does not do, and the one that matters
//
// It PERSISTS NOTHING (K6). The caller owns the transcript — which is what lets
// S6 suspend by exiting, S7 resume from rows it read itself, and S9's `execute`
// call it per step. A runtime that wrote its own rows could do none of those,
// because each of them needs to hand the loop a transcript from a previous
// process.
//
// It also opens no store, reads no environment and touches no file it was not
// given: `model.Client`, the toolset and the event sink are all constructor
// parameters (I3, I5). That is what makes "no unit test contacts a provider"
// (D18) achievable rather than aspirational.
package runtime

import (
	"encoding/json"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// Outcome is what the WORK did. Four values, inherited unchanged.
//
// # Suspension is not a fifth outcome
//
// These four describe work; suspension describes a PAUSE. Adding it here would
// make `blocked` ambiguous — a caller could no longer tell "ran out of
// iterations" from "waiting for a human", and those need opposite responses:
// one raises a budget, the other answers a question.
type Outcome string

const (
	// OutcomeOK — the work succeeded and the handoff says so.
	OutcomeOK Outcome = "ok"

	// OutcomeReject — the step ran correctly and its answer is "no". A model
	// that was asked to verify something and found it wrong produces this, and
	// it is NOT a failure: conflating the two makes a correct refusal look like
	// a defect.
	OutcomeReject Outcome = "reject"

	// OutcomeBlocked — the work ran out of ROOM, not out of correctness.
	// Exceeding MaxIterations or DeadlineAt lands here, and the reason names
	// which bound and its value (O2). A caller seeing `failed` files a bug
	// report; one seeing `blocked` raises the budget.
	OutcomeBlocked Outcome = "blocked"

	// OutcomeFailed — the work could not be done: the provider refused, or the
	// model could not produce a valid handoff twice.
	OutcomeFailed Outcome = "failed"
)

// Outcomes is every outcome, for the test that proves there is no fifth.
func Outcomes() []Outcome {
	return []Outcome{OutcomeOK, OutcomeReject, OutcomeBlocked, OutcomeFailed}
}

// ResultKind distinguishes a finished step from a paused one.
//
// # Why `suspended` exists at S5 and is never produced (K21)
//
// S6 adds suspension. Shipping the union shape NOW costs one field; adding it
// at S6 means a mechanical sweep through every `switch` written in between —
// in the loop, the protocol handler, the CLI, the app. A test asserts nothing
// returns it, so the unused variant is honest rather than aspirational.
type ResultKind string

const (
	// ResultComplete — the step finished, with an Outcome and a Handoff.
	ResultComplete ResultKind = "complete"

	// ResultSuspended — the step is waiting for an answer. **Never produced in
	// S5** (D14), and `TestNothingProducesSuspended` proves it.
	ResultSuspended ResultKind = "suspended"
)

// StepResult is what a step produces: a tagged union over ResultKind.
type StepResult struct {
	Kind ResultKind `json:"kind"`

	// Outcome and Handoff are set when Kind is ResultComplete.
	Outcome Outcome  `json:"outcome,omitempty"`
	Handoff *Handoff `json:"handoff,omitempty"`

	// Question is set when Kind is ResultSuspended, which is never in S5.
	// S6 fills it; the field exists so S6 does not reshape this type.
	Question *PendingQuestion `json:"question,omitempty"`

	// Reason explains a `blocked` or a `failed`, naming the bound and its value
	// where there is one. Empty on success — a reason for a success is a
	// sentence somebody will render.
	Reason string `json:"reason,omitempty"`

	// Messages is the canonical transcript as it stands, whatever happened.
	// Present on EVERY outcome, including `failed`: the transcript is the
	// record of what was spent, and a failure that returns nothing loses it.
	Messages []model.Message `json:"messages"`

	// Manifest is the frozen context, with IterationsUsed as it ended.
	Manifest ContextManifest `json:"manifest"`

	// Usage and Cost are the accounting, partial where that is all there is
	// (M15 — a failed step still spent money).
	Usage          model.Usage `json:"usage"`
	Cost           model.Cost  `json:"cost"`
	AdditionalCost model.Cost  `json:"additionalCost,omitempty"`

	// ThinkingDropped is the step's total count of thinking blocks WITHHELD
	// from a model call because their producer is not that provider
	// (S18 T5, M4/M27: dropped and counted, and the count reaches the
	// record). The transcript keeps every block — the omission is wire
	// direction only (M1) — and this is the number that says so where S9
	// renders the step, answering "why did this turn behave differently"
	// without a log dive (O1).
	ThinkingDropped int `json:"thinkingDropped,omitempty"`
	// PendingRoute and PendingEffort carry a route switch a steer asked for
	// mid-step (S18 T8, O5/N9): refused for THIS step — the pair of a tool
	// call and its result cannot straddle providers — and deferred to the
	// loop, which applies the switch before the next dispatch. Empty means
	// nothing was owed.
	PendingRoute  string
	PendingEffort string

	// Events is what was emitted, in order. Returned as well as streamed so a
	// caller that did not subscribe can still see what happened.
	Events      []StepEvent        `json:"events"`
	Compactions []CompactionRecord `json:"compactions,omitempty"`

	// SinkFailures counts emits that panicked (T9). Reported rather than
	// swallowed: a sink that is throwing is a client that is not seeing the
	// step, and the step completing anyway is correct but worth knowing.
	SinkFailures int `json:"sinkFailures,omitempty"`

	// PauseReason (S55 T7) is set when Kind is ResultSuspended WITHOUT a
	// question: "cap" is the budget engine's in-step hit (E2). It is the
	// absence-of-a-question made explicit (DB19) — a suspended step whose
	// Question is nil and whose PauseReason is empty is the S6 shape
	// (a question the caller will find by step id), "cap" is a suspension
	// nobody may answer with an answer: the only door is loop.raise. The
	// step row's pause_reason column carries the same value, so a cold
	// recovery reads it from the row, not from memory.
	PauseReason string `json:"pauseReason,omitempty"`

	// CapHit is set beside PauseReason="cap": the raw numbers of the hit the
	// runtime's own check made. The runtime is pure (no store, no budget
	// package import — TestRuntimeStaysPure), so this is plain data the
	// daemon translates into the loop's CapHitState and the store's hit
	// event; the loop engine owns the durable record, the runtime only
	// reports.
	CapHit *CapHit `json:"capHit,omitempty"`
}

// CapHit is the runtime-local shape of a budget hit the in-step check (E2)
// made. Plain data — no import of internal/budget or internal/store (the
// purity guard): the daemon maps it to budget.Hit / loop.CapHitState. The
// fields are the hit's identity and its numbers as the check saw them, so
// a raise can name the budget, the scope, the dimension and the floor it
// is being raised from.
type CapHit struct {
	BudgetID   string  `json:"budgetId"`
	Scope      string  `json:"scope"`
	Subject    string  `json:"subject"`
	Dimension  string  `json:"dimension"`
	Window     string  `json:"window,omitempty"`
	SpendFloor float64 `json:"spendFloor"`
	Cap        float64 `json:"cap"`
	Reason     string  `json:"reason"`
	// Exact says the floor the hit measured was fully priced (no unpriced
	// steps rode it); the store's hit event carries the same honesty, so
	// the audit can tell a floor that IS the spend from a floor that is a
	// lower bound.
	Exact bool `json:"exact,omitempty"`
}

// BudgetVerdict is what the in-step budget check (E2) answers with: plain
// data the daemon's seam produces from the pure budget engine. OK true
// means spend on; OK false with CapHit set is a cap hit (the step
// suspends on it, pause_reason='cap', no question row); OK false without
// a hit is a refusal the step may not continue past (the daemon renders
// Reason). A seam ERROR is not a verdict at all — the step fails closed,
// it never spends past a cap it could not read.
type BudgetVerdict struct {
	OK     bool
	Reason string
	CapHit *CapHit
}

// Complete builds a finished result. A constructor rather than a struct
// literal at four call sites, so `Kind` cannot be forgotten — a result with no
// Kind would switch to neither branch.
func Complete(outcome Outcome, h *Handoff) StepResult {
	return StepResult{Kind: ResultComplete, Outcome: outcome, Handoff: h}
}

// PendingQuestion is S6's shape, declared here and never populated.
//
// Declared rather than left as `any`, because `any` is a field every switch
// has to type-assert and every test has to guess at. S6 fills these in; the
// names come from S4's `question` table so the two do not need translating.
type PendingQuestion struct {
	ID        int64    `json:"id"`
	StepID    int64    `json:"stepId,omitempty"`
	Kind      string   `json:"kind"`
	Body      string   `json:"body"`
	Options   []string `json:"options,omitempty"`
	ToolUseID string   `json:"toolUseId,omitempty"`
}

// Handoff is the structured report a step ends with.
//
// A map rather than a struct, because the SHAPE is the handler's: each handler
// declares a JSON Schema and the loop validates against it. A struct here would
// be a second declaration of every handler's report, and the two would drift.
type Handoff map[string]any

// StepHandler is what a step is asked to do.
//
// At S5 this is a literal built by the protocol handler. S13 brings the agent
// registry; the shape is the same either way, which is the point of declaring
// it now.
type StepHandler struct {
	// Name identifies the handler in events and in the transcript.
	Name string

	// System is the system prompt.
	System string

	// Sections are the context, in DECLARED ORDER (K13). The order is the
	// handler's and is preserved exactly: a model reads top to bottom, and
	// re-ordering sections changes what it emphasises.
	Sections []SectionSpec

	// Tools are the tool names this handler needs. Checked against the step's
	// GRANT LIST at construction (T3): a handler asking for a tool the step was
	// not granted fails before any model call, naming both.
	Tools []string

	// HandoffSchema is the JSON Schema the report must satisfy, as the model is
	// shown it. Derived once (T3), never per retry.
	HandoffSchema string

	// MaxOutputTokens is required. A missing value is a construction error
	// rather than a default, because the default would be wrong for one
	// provider and silently truncate for the other (M16).
	MaxOutputTokens int
}

// SectionSpec declares one piece of context.
//
// Exactly one of Text or DeclaredFile is set. Two fields rather than an
// interface, because the whole point is that a section is one of two simple
// things and a reader should see both at once.
type SectionSpec struct {
	// Title is the section's heading in the assembled prompt.
	Title string

	// Text is literal content.
	Text string

	// DeclaredFile is a path, relative to the workspace, read and HASHED into
	// the manifest at assembly (K13, K23).
	//
	// A declared file that does not exist is a CONSTRUCTION ERROR, not an empty
	// section (D13). An empty section reads to a model as "this file is empty",
	// which is a different and more damaging claim than "this file is missing"
	// — it will reason from the absence.
	DeclaredFile string

	// Retrieval asks the index, ONCE, at assembly (S22 T7, V7/R6): "this step
	// needs the code relevant to X". The resolution is a DECLARATION-time
	// event (I3, N7): whatever it returns is frozen into the manifest, and no
	// resume or recovery ever asks again — a re-resolved context could differ
	// from the one that suspended, unmaking K14's report and S7's recovery.
	//
	// Set with EXACTLY ONE of Text/DeclaredFile, same one-of discipline.
	Retrieval *RetrievalSpec

	// Category classifies what this section IS, for S33's composition
	// readout (TL6, D4). REQUIRED — no zero-value default, because an
	// unlabelled section is one the readout would have to guess at, and a
	// guessed category is exactly the confident wrong answer three-state
	// honesty (P31) exists to stop. AssembleContext refuses a section whose
	// Category is empty or off the two context-side values.
	//
	// Only the two CONTEXT categories are legal here (system_prompt for the
	// handler's own fixed instructional text, injected_context for a declared
	// file or a retrieval). The transcript categories (assistant_output,
	// tool_result) are recorded by the loop at its append points (T3, T4),
	// never declared, so a SectionSpec carrying one is a category error the
	// assembly refuses. tool_schema is absent from the vocabulary entirely
	// (TL6's measured finding).
	Category SectionCategory

	// Destination says WHICH MESSAGE the section rides (S68, B1) — required
	// with the same discipline as Category, no zero-value default.
	//
	// Category says what a section IS (whose words); Destination says where it
	// GOES (which wire message). The pre-S68 bug is exactly the collapse of
	// the two: instructional text rode the user message because there was no
	// field to say otherwise, and the chat window replayed policy prose as
	// something the operator had said. A section that does not choose is
	// refused at assembly, naming its title — the same argument D4 makes for
	// Category, applied to destinations.
	Destination SectionDestination
}

// SectionDestination is which opening message a section rides (S68, B1).
type SectionDestination string

const (
	// SectionSystem rides the SYSTEM message: instructions — how to work,
	// the workspace rules, the spoken policy, the skills offer and the
	// chosen-skill sentences, the task frame. Provider-native system content
	// (Anthropic hoists it to `system`; OpenAI carries the system-role
	// message), never something the transcript attributes to a person.
	SectionSystem SectionDestination = "system"

	// SectionTranscript rides the USER message: the person's own words and
	// the material a person chose to hand over (declared files, retrievals).
	// Byte-for-byte this half is what the chat window shows as "you".
	SectionTranscript SectionDestination = "transcript"
)

// RetrievalSpec is a retrieval declaration: the NAMED corpus and the query
// (O8 — there is no default corpus; a query with no corpus is the same
// under-specified ask the `search` tool refuses).
type RetrievalSpec struct {
	Corpus string
	Query  string
}

// ContextManifest is the context, frozen at assembly (K13, K23).
type ContextManifest struct {
	// Files is every declared file with the hash of what was actually read.
	Files []FileHash `json:"files"`
	// Retrievals records every retrieval DECLARED for this step, including
	// the ones that found nothing (S22 T7, O6): a step whose retrieval found
	// nothing behaves differently from one where retrieval was never asked —
	// the first knows the index had no opinion. Not an error, not silence.
	Retrievals []RetrievalRecord `json:"retrievals,omitempty"`

	// Sections is S33 TL6's composition record: one entry per assembled
	// section in declaration order, each carrying the category the DECLARER
	// stated and the character count of the body actually assembled. Frozen
	// with the manifest through the existing checkpoint (K14), read back
	// VERBATIM by telemetry.loop (T8). Pre-S33 manifests carry nil: the
	// composition answers empty and the readout says "not recorded", never
	// infers a category it was never given (hence omitempty).
	Sections []SectionRecord `json:"sections,omitempty"`

	// SystemChars counts the SYSTEM prompt the step was actually sent — the
	// handler's instructional text PLUS the embedded handoff-schema suffix,
	// because that suffix spends context whether anybody declared it or not.
	// Read with Sections; 0 alongside nil Sections reads as "not recorded".
	SystemChars int `json:"systemChars,omitempty"`
	// ObservedChanges records resume-time comparisons without replacing the
	// frozen file hashes or the original context quoted in the transcript.
	ObservedChanges []FileChange `json:"observedChanges,omitempty"`

	// IterationsUsed is incremented on the MANIFEST rather than in a local, so
	// S6 can persist it across a suspension without changing the loop.
	IterationsUsed int `json:"iterationsUsed"`

	// DeadlineAt is ABSOLUTE, never a remaining duration.
	//
	// A relative deadline cannot survive a suspension that lasts overnight: it
	// would grant the resumed step its whole budget again, and a step could
	// escape its bounds by suspending repeatedly.
	DeadlineAt time.Time `json:"deadlineAt"`

	// DeadlineWindowMs is the working-time window DeadlineAt was set from, kept so
	// a step resumed from a human answer can be given the same window again
	// (waiting on a person is not working time). 0 on a manifest that predates it:
	// the compiled default then stands.
	DeadlineWindowMs int64 `json:"deadlineWindowMs,omitempty"`

	// MaxIterations is carried with the manifest so a resumed step is bounded
	// by what the original step was bounded by, not by today's default.
	MaxIterations int `json:"maxIterations"`

	// Policies is the set in force when the step was decided — frozen exactly
	// like Files (S10, D12). Replay evaluates against THIS, not the live
	// store: a recovered step that consulted today's table would silently
	// re-decide the past, and a policy added after the crash could refuse a
	// call the step was entitled to make (or an old deny revoked after it
	// could permit one the step was refused).
	//
	// Nil means NOTHING was frozen. Whether that is a claim about the world
	// (the store held no policies and the step saw it — PoliciesFrozen) or a
	// claim about the manifest (it predates S10, or was assembled with no
	// policy source — !PoliciesFrozen) is exactly what the marker below
	// separates (DEF-042). The resume path falls back to the live gate ONLY
	// for a nil Policies with no marker; every manifest with a set — or with
	// the marker — is authoritative for its step.
	//
	// Spec bytes are configuration, not content: they entered through the
	// policy Add boundary (validated, bounded) and they are allowed here for
	// the same reason the file hashes are allowed — the manifest records what
	// was in force, and recording it without the spec would record a shape
	// nobody can re-evaluate.
	Policies []PolicyEntry `json:"policies,omitempty"`

	// PoliciesHash is hex(sha256(canonical lines)) over Policies (S10). What
	// it PROVES is what an unkeyed digest over a file a same-user process can
	// rewrite can prove: daemon.NewStepPolicyGate recomputes it from the
	// loaded entries at replay and compares (DEF-039) — a CHANGE DETECTOR
	// that catches a half-updated frozen set (entries edited, digest left
	// alone) and truncation. It is not an integrity proof: a forgery that
	// updates the digest alongside the entries sails through, and only an
	// HMAC would catch that — ruled out by legal constraint 6, which keeps
	// this digest stdlib crypto/sha256 and nothing else.
	//
	// The canonical line form is context.go's (the ONE digester), and a
	// replay test asserts this value is byte-stable across re-runs of the
	// same set.
	PoliciesHash string `json:"policiesHash,omitempty"`

	// PoliciesFrozen is the marker that separates the two empties (DEF-042).
	// An S10 step assembled over a store that held no policies froze NOTHING
	// — and "froze nothing" is a claim about the world, while "this manifest
	// predates S10 / was assembled with no policy source" is a claim about
	// the manifest. Without this bool the two marshal to the same bytes, and
	// the replay path resolved the ambiguity by consulting the LIVE store,
	// which is how a zero-policy step (the normal case on a fresh install)
	// recovered its permissions from today's table.
	//
	// Set by AssembleContext when and only when a set was handed to it. A
	// manifest written before this field existed cannot be told apart — the
	// information is not in its bytes — so the live fallback stays for
	// markerless manifests with no entries, and this is the one residual the
	// fix knowingly leaves. S10 has not shipped, so no such store exists
	// outside test fixtures.
	PoliciesFrozen bool `json:"policiesFrozen,omitempty"`
}

// PolicyEntry is one policy as it stood at assembly — the frozen shape the
// manifest carries (S10, D12). Field set mirrors what a policy IS to a step:
// what it is, what it targets, what it does, where it came from, when it
// stops, and its configuration. It carries no Subject and no CreatedAt on
// purpose: those are the record's business, not the step's; a frozen set
// re-evaluated on replay decides from the same fields the live set decided
// from.
type PolicyEntry struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TargetKind string          `json:"targetKind"`
	TargetID   string          `json:"targetId"`
	Action     string          `json:"action"`
	Source     string          `json:"source"`
	NotAfter   *time.Time      `json:"notAfter,omitempty"`
	Spec       json.RawMessage `json:"spec,omitempty"`
}

type FileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // changed or missing
}

// FileHash is a declared file and the hash of its content at assembly.
//
// S22 T7 adds its provenance (V14, D5, O2): a `declared` entry is a human's
// or a plan's assertion; a `retrieved` one is a GUESS — similarity picked
// it, and the file the step actually needs might not be there. A model that
// knows its context was guessed will verify with `grep`; a model that thinks
// it was declared will trust it. Same information, opposite behaviour —
// which is the whole reason the distinction is RECORDED rather than quietly
// merged. The score rides because it is already computed and it changes
// behaviour: the first result at 0.93 and the fourth at 0.61 deserve
// different amounts of trust (O2).
type FileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	// Bytes is the size read, so a manifest says how much context a file was.
	Bytes int `json:"bytes"`
	// Resolution is "declared" or "retrieved" — never empty on an entry a
	// step ran with; absent only in manifests written before S22 T7.
	Resolution string `json:"resolution,omitempty"`
	// Query is the retrieval query that surfaced this file; retrieved only.
	Query string `json:"query,omitempty"`
	// Score is the fusion score the hit carried; retrieved only (O2).
	Score float64 `json:"score,omitempty"`
}

// RetrievalRecord is one retrieval declaration's outcome in the manifest
// (S22 T7, O6). It is recorded even when nothing was found: the record is
// what tells a later reader — human or model — that the index was ASKED and
// had no opinion, which is a different state from retrieval never existing.
type RetrievalRecord struct {
	Corpus      string `json:"corpus"`
	Query       string `json:"query"`
	Hits        int    `json:"hits"`
	Files       int    `json:"files"`
	Found       bool   `json:"found"`
	KeywordOnly bool   `json:"keywordOnly,omitempty"`
	// Note carries the honest shape of a nothing-found that was not a plain
	// zero — hits returned but unreadable now (the index lags the tree; V18
	// says reindexing is T8's, not this reader's problem to hide).
	Note string `json:"note,omitempty"`
}

// SectionRecord is one assembled section's frozen composition entry (S33
// TL6): what it was called, which of the two context categories the declarer
// named, and how many characters of body the model was actually shown. Title
// and Category come straight from the SectionSpec; Chars is the length of the
// body AFTER resolution (a file's read bytes, a retrieval's returned text),
// so the number describes the context that existed, not the declaration that
// asked for it.
type SectionRecord struct {
	Title    string          `json:"title"`
	Category SectionCategory `json:"category"`
	Chars    int             `json:"chars"`
}

// StepRequest is a compiled step: everything needed to run it, and nothing
// that has to be looked up.
type StepRequest struct {
	Handler StepHandler

	// SkillsInjected is the catalog section this compile appended (nil
	// when no catalog is wired or it had nothing to offer). It rides the
	// REQUEST rather than the frozen manifest on purpose: the manifest
	// must stay byte-reproducible for resume (D12), and resume never
	// re-compiles — the marker is what the operator was TOLD at start,
	// carried by the event, not a fact the runtime re-derives.
	SkillsInjected *SkillsSection

	// Messages is the opening transcript — the system prompt and the assembled
	// context as a user turn.
	Messages []model.Message

	// Manifest is frozen. The loop increments IterationsUsed on it and changes
	// nothing else.
	Manifest ContextManifest

	// Route is which model to call (S3).
	Route model.Route

	// Tools is what the model is offered, already filtered by the grant list.
	Tools []model.ToolSpec

	// ReasoningEffort is what every model call in this step carries. Empty
	// takes the route's.
	//
	// It is on the STEP rather than read from the route per call, because
	// assembly may have lowered it: a step that offers tools cannot have
	// reasoning on, on the models that refuse the combination. See
	// `reasoningFor`.
	ReasoningEffort string

	// Thinking is the person's thinking switch for this step: "on", "off", or empty for the model's own default. It
	// rides the request so a step resumed after a question applies the same switch to the route it resumes on (the
	// switch is an extra request field on the route, which a resume re-resolves).
	Thinking string

	// Notes are what assembly decided on the caller's behalf, carried so the
	// `step.started` event can say them. A decision nobody can see is a
	// decision that reads as the provider's behaviour.
	Notes []string

	// Withheld is the MCP offer's withheld candidate lines (S36 T4, Q3),
	// carried so the `step.started` event can say them additively. Nil means
	// nothing was withheld (MD8: no MCP offer, byte-identical to today). The
	// daemon sets this where the bundle is known; the runtime only forwards it.
	Withheld []string

	// WorkspaceRoot is where file tools resolve against. It arrives here rather
	// than being read from the environment (I5): a tool that called `os.Getwd`
	// would resolve against the DAEMON's directory, which is not the client's.
	WorkspaceRoot string

	// StepID is the step row this request belongs to — assigned SERVER-SIDE by
	// whoever created the row (the protocol handler after CreateRunningStep,
	// the recovery path from the row it recovered), never by the model. It is
	// carried so a refusal can be attributed: RecordDisagreement names the
	// step whose agent attempted the refused thing, and an attribution read
	// from tool arguments would be the model naming its own crime scene
	// (PL1's trust boundary, same rule as the gate's Agent field).
	//
	// Zero means "not from a step row" (a pre-S10 snapshot, a caller that
	// predates the field). Adapters record refusals for those; they record no
	// disagreement, because an unattributable attempt is not yet an attempt
	// anybody can charge an agent with.
	StepID int64 `json:"stepId,omitempty"`

	// HasOperator is true exactly when somebody may exist to answer this
	// step's questions (S11). It is plain decision data: the daemon's step
	// adapter reads the loop's pinned operator declaration ONCE, at step
	// assembly, and stamps it here before handing the request in. The runtime
	// never reads a store, never reads the environment, and never decides the
	// declaration itself (K22 — the pin belongs to loop start, not to the
	// gate or the loop); it only consumes what the adapter carried.
	//
	// False — including a loop that never made the declaration, which the
	// adapter collapses to false fail-closed — means nobody can be vouched
	// for, and an asked decision is answered as a deny on the spot rather
	// than suspended behind a question nobody will read.
	//
	// The name is deliberately NOT the three-state word the store carries:
	// this package's grep-shaped guard forbids that vocabulary in any non-test
	// source (a runtime that could see the declaration as a state could start
	// setting it, and the pin belongs to loop start). The guard test is named
	// for the gate's clock rule; this field honours its word ban by never
	// naming the store's state in code or prose.
	HasOperator bool `json:"hasOperator,omitempty"`

	// AutoApprove is what a person has pre-approved for this step's loop: every asked call (All — the
	// chat's full-access switch), or the named tools. Plain decision data stamped at step assembly from the
	// loop's stored mode, like HasOperator. It answers an ASK only: an explicit deny rule still denies, and
	// a call the gate allows needs nothing. Nil is the default: ask.
	AutoApprove *AutoApprove `json:"autoApprove,omitempty"`
}

// AutoApprove is a person's standing pre-approval for a loop's asked calls.
type AutoApprove struct {
	All   bool     `json:"all,omitempty"`
	Tools []string `json:"tools,omitempty"`
	// By names who turned it on (the connection's actor), so the verdict on the stream can say so.
	By string `json:"by,omitempty"`
}

// Covers reports whether this pre-approval covers a call to `tool`.
func (a *AutoApprove) Covers(tool string) bool {
	if a == nil {
		return false
	}
	if a.All {
		return true
	}
	for _, t := range a.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// StepEvent is one thing that happened, in order.
//
// The vocabulary is the superseded slice's, UNCHANGED — S9 was specced against
// these names — plus `step.tool.start` and `step.tool.end`, which it did not
// have. Extending is additive; renaming would mean amending reviewed work.
// StepSkills describes the catalog section ONE step was given. Names,
// never contents — the chat renders them, and anyone wanting the text
// reads the catalog back through skills.list.
type StepSkills struct {
	Count     int      `json:"count"`
	Names     []string `json:"names"`
	Truncated bool     `json:"truncated,omitempty"`
	// Marker is the ONE sentence (the daemon's renderer); live and
	// replay render the same words because they ride the same field.
	Marker string `json:"marker,omitempty"`
}

type StepEvent struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`

	// Text is set for a delta.
	Text string `json:"text,omitempty"`

	// Tool, Summary and Result describe a tool call. Summary is an ARGUMENT
	// summary rather than the arguments: a tool call carrying a file's whole
	// content would put it in the event stream twice.
	Tool    string `json:"tool,omitempty"`
	Summary string `json:"summary,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"isError,omitempty"`

	// Withheld are the MCP offer's withheld candidate lines (S36 T4, Q3),
	// additive — an old client that decodes only the pre-existing fields is
	// unaffected (MD7 additive-only; the field is omitempty). Empty means
	// nothing was withheld.
	Withheld []string `json:"withheld,omitempty"`

	// Skills is S66's marker, additive by the same MD7 rule: it rides
	// `step.started` ONLY, because the injection is part of the step's
	// opening — the same event that carries the summary is the honest
	// carrier. nil means nothing was injected (no catalog wired, or the
	// catalog was empty); a client that ignores the field is unaffected.
	Skills *StepSkills `json:"skills,omitempty"`

	// Iteration is which pass through the loop this was, so a reader can see
	// the shape of a step rather than a flat list.
	Iteration int `json:"iteration,omitempty"`

	// Outcome and Reason are set on `step.done`.
	Outcome        Outcome     `json:"outcome,omitempty"`
	Reason         string      `json:"reason,omitempty"`
	Start          int         `json:"start,omitempty"`
	End            int         `json:"end,omitempty"`
	EstimateBefore int         `json:"estimateBefore,omitempty"`
	EstimateAfter  int         `json:"estimateAfter,omitempty"`
	Cost           *model.Cost `json:"cost,omitempty"`

	// SteerID names the durable steer row a `step.steer.delivered` carries
	// into the transcript (S31 T4); SkippedTools names the tool calls that
	// steer cost the turn (brief §5), in emission order.
	SteerID      int64    `json:"steerId,omitempty"`
	SkippedTools []string `json:"skippedTools,omitempty"`

	// MessageID names the durable agent_message row an
	// `agent.message.asked`/`agent.message.delivered` carries (S70 MB6) —
	// the same id the checkpoint flips delivered, so stream and store agree
	// by id. Additive by MD7's rule (the Withheld/Skills precedent): a
	// client that decodes only the pre-existing fields is unaffected.
	MessageID int64 `json:"messageId,omitempty"`

	// ToolUseID answers a `step.tool.skipped` to the exact call it closed —
	// and a `step.tool.end` to the start and transcript result it completes
	// (brief §5: `toolUseId` on both).
	ToolUseID string `json:"toolUseId,omitempty"`

	// Path and the line counts enrich `step.tool.end` for write-shaped
	// results (S31 D8): the event stream answers "what did that call cost
	// the file" without a reader re-reading anything. Zero for every tool
	// that is not about a file, and for calls that never ran.
	Path         string `json:"path,omitempty"`
	LinesAdded   int    `json:"linesAdded,omitempty"`
	LinesRemoved int    `json:"linesRemoved,omitempty"`

	// Route and ReasoningEffort announce a steer's provider change
	// (`step.route.changed`), and FromIteration is the iteration the new pair
	// applies FROM — the next call, never the in-flight one (D5).
	Route           string `json:"route,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	FromIteration   int    `json:"fromIteration,omitempty"`

	// Verdict is set on `step.policy.decided`: `allowed` | `denied` |
	// `asked`. Reason carries the verdict sentence verbatim.
	Verdict string `json:"verdict,omitempty"`

	// Command is a bash call's whole command, on `step.tool.start` (capped
	// at streamCommandCap runes). Output is its result content on
	// `step.tool.end` (capped at streamOutputCap bytes; OutputTruncated says
	// it was cut). Bash only — no other tool's content rides the stream.
	Command         string `json:"command,omitempty"`
	Output          string `json:"output,omitempty"`
	OutputTruncated bool   `json:"outputTruncated,omitempty"`

	// The replay fields (loop-replay step 2), all additive (MD7): what a reopened loop needs to show what a live one
	// showed, kept in the journal because the stream is the journal.
	//
	// DurationMs is how long a tool call took, on `step.tool.end`, measured by the runtime between the call's start
	// and end events. Detail is the call's result content on `step.tool.end` for every tool (capped at
	// streamDetailCap bytes; DetailTruncated says it was cut; the whole content stays in the step's transcript).
	// ApprovedBy says WHO let an asked call run, on the `allowed` `step.policy.decided` that follows an ask:
	// `person` (answered the question), `auto` (a standing pre-approval or full access for the loop).
	DurationMs      int64  `json:"durationMs,omitempty"`
	Detail          string `json:"detail,omitempty"`
	DetailTruncated bool   `json:"detailTruncated,omitempty"`
	ApprovedBy      string `json:"approvedBy,omitempty"`

	// `step.turn.completed`: one model call, finished. Text and Thinking are the whole turn (the streamed chunks
	// are not needed to rebuild it), Usage and TurnCost what it spent, StepCost the step's running total after it,
	// WallMs how long the call took, StopReason why it stopped, ToolCalls how many calls it asked for. Route is
	// the route that served it (also used by `step.route.changed`).
	Thinking   string       `json:"thinking,omitempty"`
	Usage      *model.Usage `json:"usage,omitempty"`
	TurnCost   *model.Cost  `json:"turnCost,omitempty"`
	StepCost   *model.Cost  `json:"stepCost,omitempty"`
	WallMs     int64        `json:"wallMs,omitempty"`
	StopReason string       `json:"stopReason,omitempty"`
	ToolCalls  int          `json:"toolCalls,omitempty"`
}

// The event vocabulary.
//
// `step.started`, `model.delta` and `step.done` are the superseded slice's
// names, kept exactly. `step.tool.start` and `step.tool.end` are new (T1's
// instruction): without them a tool call is invisible in the stream, and a
// step that spends forty seconds reading files looks like a step that hung.
const (
	EventStepStarted   = "step.started"
	EventModelDelta    = "model.delta"
	EventToolStart     = "step.tool.start"
	EventToolEnd       = "step.tool.end"
	EventStepDone      = "step.done"
	EventStepCompacted = "step.compacted"

	// EventTurnCompleted is one model call finished: its whole text and thinking, what it spent, how long it took
	// and the step's running cost (loop-replay step 2). The page's cost counter moves on this event; a reopened loop
	// rebuilds the turn from it.
	EventTurnCompleted = "step.turn.completed"

	// EventTodoNudged is S75 TD9/TD10's: the runtime reminded the agent about
	// open todo items. Reason is "stale" (calls without touching the list),
	// "completion" (a step tried to finish with items open) or
	// "closed-with-open" (the third attempt completed anyway); Summary carries
	// the open ids. The reminder TEXT is a runtime turn in the transcript, never
	// the person's message.
	EventTodoNudged = "step.todo.nudged"

	// EventTelemetryError is S33 T3's: a capture that failed is an honest
	// hole in an account, and a hole someone can see is a hole someone can
	// close. The step is never failed for it — what a step costs is
	// observed, not transactional with what the step DOES — but silence
	// would make the aggregate read as complete when a turn is missing.
	EventTelemetryError = "step.telemetry.error"

	// S31 T4's three. A steer that arrives, a steer that costs the turn its
	// tools, and a steer that changes who is answering — each is a thing a
	// person did, and D5 says overrides are ANNOUNCED: an effect nobody can
	// see in the stream is an effect nobody can trust.
	EventSteerDelivered = "step.steer.delivered"
	EventToolSkipped    = "step.tool.skipped"
	EventRouteChanged   = "step.route.changed"
	// EventRouteDeferred is the refusal of a mid-step route switch (S18 T8,
	// O5, N9): the steer's text was delivered; the route was not applied,
	// and the event's Text says so, naming the step and the next one.
	EventRouteDeferred = "step.route.deferred"

	// The gate's verdict on one call, published for allowed calls too, so
	// the chat can show "policy allowed write_file" rather than only the
	// refusals (which would read as "nothing was checked" for the rest).
	EventPolicyDecided = "step.policy.decided"

	// S70 MB6's two step-stream events: an ask this step filed (withheld
	// until the row exists to name — the door files, then the loop speaks)
	// and a peer message delivered into this step's transcript. The DURABLE
	// rows named on them (MessageID) are the same ids the checkpoint flips,
	// so a stream reader and the store agree by id about what happened when.
	EventAgentMessageAsked     = "agent.message.asked"
	EventAgentMessageDelivered = "agent.message.delivered"
)

// EventKinds is the vocabulary, for the test that asserts the inherited names
// survive.
func EventKinds() []string {
	return []string{
		EventStepStarted,
		EventModelDelta,
		EventToolStart,
		EventToolEnd,
		EventTurnCompleted,
		EventStepDone,
		EventStepCompacted,
		EventTodoNudged,
		EventSteerDelivered,
		EventToolSkipped,
		EventRouteChanged,
		EventRouteDeferred,
		EventPolicyDecided,
		EventAgentMessageAsked,
		EventAgentMessageDelivered,
	}
}

// EventSink receives events AS THEY HAPPEN (K19).
//
// An interface with one method, so the protocol handler can forward to S2's
// subscriptions and a test can record. A channel was considered and rejected:
// a channel needs a reader, and a step whose events nobody is draining would
// block — which turns "nobody subscribed" into "the step hangs".
type EventSink interface {
	Emit(StepEvent)
}

// SinkFunc adapts a function to an EventSink.
type SinkFunc func(StepEvent)

func (f SinkFunc) Emit(e StepEvent) { f(e) }

// DiscardSink drops everything. The default, so a caller that does not care
// passes nothing rather than nil — a nil sink would need a check at every
// emit.
type DiscardSink struct{}

func (DiscardSink) Emit(StepEvent) {}
