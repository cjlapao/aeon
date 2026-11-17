// Package model is Aeon's canonical message format and its provider adapters.
//
// # Why a canonical format at all
//
// Three provider families disagree about thirteen things: where the system
// prompt goes, whether tool arguments arrive as a JSON string or a parsed
// object, where a tool result belongs, whether an output cap is required, and
// four distinct shapes of streaming event. **Passing a caller's messages
// straight to the wire fails at the second provider**, and it fails in a way
// that looks like a model problem.
//
// So: one format in the middle, and an adapter each side. Everything above this
// package builds a canonical request; nothing above it has ever seen a
// provider's JSON.
//
// # This package is a leaf (I1)
//
// It imports nothing else under `internal/`. `import_test.go` asserts it,
// because the violation is invisible — everything still works, and the layer
// stops being testable without the rest of the daemon.
package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// FormatVersion is the version of the canonical format on disk (M2).
//
// A transcript outlives the release that wrote it: it is persisted when a step
// suspends (S4), replayed on resume (S5) and recovered from after a crash (S7).
// So the stored envelope carries this number, and `MigrateTranscript` refuses a
// transcript from the future rather than parsing it best-effort.
const FormatVersion = 1

// --- roles and messages ------------------------------------------------------

type Role string

const (
	// RoleSystem is a role in the canonical slice (M5). The Anthropic adapter
	// hoists it to a top-level field; OpenAI's leaves it in the array. Keeping
	// it a role here means a caller writes one shape.
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn.
//
// `Content` is ALWAYS a block slice, never a bare string (M3). A format with
// two representations of the same thing has two code paths at every consumer,
// and the string one is always the one that silently drops a tool result.
type Message struct {
	Role    Role    `json:"role"`
	Content []Block `json:"content"`

	// Provenance records who produced an ASSISTANT turn (M26) — nil on every
	// other role, and required on that one. See provenance.go for the whole
	// argument; ValidateProvenance is the rule.
	Provenance *Provenance `json:"provenance,omitempty"`
}

// --- the block union ---------------------------------------------------------

type BlockKind string

// Six kinds from S69: the four of M4 (text, thinking, tool_use, tool_result)
// widened by image and document. Provider content that maps to none of them is
// dropped and COUNTED — a silent drop is how a `thinking` block disappears
// between two releases and nobody can say when.
const (
	BlockText       BlockKind = "text"
	BlockThinking   BlockKind = "thinking"
	BlockToolUse    BlockKind = "tool_use"
	BlockToolResult BlockKind = "tool_result"
	// image / document (S69) — capability content riding the person's turn,
	// the way the provider wires carry it: typed fields, bytes as a field,
	// never sentence decoration (the AT7 rule).
	BlockImage    BlockKind = "image"
	BlockDocument BlockKind = "document"
)

// Block is a tagged struct rather than an interface (M20).
//
// # The trade, stated rather than hidden
//
// Go has no sum types. An interface-based union needs hand-written
// `MarshalJSON`/`UnmarshalJSON` — and **transcripts are persisted, replayed and
// recovered from**, so JSON round-trip fidelity outranks compile-time
// exhaustiveness. A hand-written codec is exactly where a lost `thinking`
// signature would come from, and it would be lost in S7's recovery rather than
// here.
//
// The cost is real: this permits a `text` block carrying a `ToolUseID`, which
// the TypeScript union made impossible. `Validate` is what buys it back, and it
// runs at every boundary that accepts a Block from outside — so the invalid
// state exists in theory and never in a stored transcript.
type Block struct {
	Kind BlockKind `json:"kind"`

	// text, thinking
	Text string `json:"text,omitempty"`
	// thinking — the provider's signature over its own reasoning (M17).
	//
	// Round-trips BYTE-IDENTICALLY or Anthropic rejects the next tool-use turn.
	// Because S7 recovers a step by replaying a stored transcript, a signature
	// mangled here fails much later and somewhere else entirely.
	Signature string `json:"signature,omitempty"`

	// tool_use
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Input is raw JSON, not `any`.
	//
	// It round-trips byte-identically, which matters because a provider's tool
	// arguments are replayed on resume: `any` would renormalise key order and
	// number formatting, and the replayed call would differ from the original.
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"toolUseId,omitempty"`
	Content   string `json:"content,omitempty"`
	// IsError has NO omitempty, deliberately (O3).
	//
	// Otherwise a tool result that failed and one that succeeded serialise
	// identically, and the model sees no difference — which is a silent
	// correctness bug in the one direction that matters.
	IsError bool `json:"isError"`

	// image / document (S69). An either/or, enforced by Validate: Data is the
	// ON-WIRE shape, produced only by the daemon's send-time expansion;
	// Document+Rev is the STORED shape — a reference into the frozen document
	// revisions, so the transcript never carries bytes and live and replay
	// agree by construction (both expand the same pinned rev).
	Data      string `json:"data,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Document  string `json:"document,omitempty"`
	Rev       int    `json:"rev,omitempty"`
}

// TextBlock is the ordinary way to build one. A bare struct literal is the
// exception, and the one Validate catches.
func TextBlock(text string) Block {
	return Block{Kind: BlockText, Text: text}
}

// ThinkingBlock carries the provider's signature verbatim (M17).
func ThinkingBlock(text, signature string) Block {
	return Block{Kind: BlockThinking, Text: text, Signature: signature}
}

// ToolUseBlock takes already-encoded arguments, so nothing re-encodes them.
func ToolUseBlock(id, name string, input json.RawMessage) Block {
	return Block{Kind: BlockToolUse, ID: id, Name: name, Input: input}
}

// ToolResultBlock reports a tool's outcome back to the model.
func ToolResultBlock(toolUseID, content string, isError bool) Block {
	return Block{Kind: BlockToolResult, ToolUseID: toolUseID, Content: content, IsError: isError}
}

// ImageRefBlock is the STORED shape of an attached image: a pinned reference,
// never bytes (AT5). DocumentRefBlock is its document twin.
func ImageRefBlock(name, mediaType, document string, rev int) Block {
	return Block{Kind: BlockImage, Name: name, MediaType: mediaType, Document: document, Rev: rev}
}

func DocumentRefBlock(name, mediaType, document string, rev int) Block {
	return Block{Kind: BlockDocument, Name: name, MediaType: mediaType, Document: document, Rev: rev}
}

// ImageDataBlock and DocumentDataBlock are the ON-WIRE shapes, produced only
// by the send-time expansion of a reference. Nothing else builds them, and
// Validate is what makes a stored one illegal.
func ImageDataBlock(name, mediaType, data string) Block {
	return Block{Kind: BlockImage, Name: name, MediaType: mediaType, Data: data}
}

func DocumentDataBlock(name, mediaType, data string) Block {
	return Block{Kind: BlockDocument, Name: name, MediaType: mediaType, Data: data}
}

// Validate rejects a block carrying a field foreign to its kind.
//
// Enumerated per kind with no permissive `default`: a default that accepts an
// unknown kind is the hole through which a block from a future format version
// reaches the wire and is silently dropped by the adapter.
func (b Block) Validate() error {
	switch b.Kind {
	case BlockText:
		return b.reject("text", b.Signature != "", "signature",
			b.ID != "", "id",
			b.Name != "", "name",
			len(b.Input) > 0, "input",
			b.ToolUseID != "", "toolUseId",
			b.Content != "", "content",
			b.IsError, "isError",
			b.Data != "", "data",
			b.MediaType != "", "mediaType",
			b.Document != "", "document",
			b.Rev != 0, "rev")

	case BlockThinking:
		// Signature is permitted and expected; an unsigned thinking block is
		// legal because not every provider signs.
		return b.reject("thinking", b.ID != "", "id",
			b.Name != "", "name",
			len(b.Input) > 0, "input",
			b.ToolUseID != "", "toolUseId",
			b.Content != "", "content",
			b.IsError, "isError",
			b.Data != "", "data",
			b.MediaType != "", "mediaType",
			b.Document != "", "document",
			b.Rev != 0, "rev")

	case BlockToolUse:
		if b.ID == "" {
			return fmt.Errorf("model: a tool_use block has no id; a tool result could never be matched to it")
		}
		if b.Name == "" {
			return fmt.Errorf("model: tool_use %q has no name", b.ID)
		}
		return b.reject("tool_use", b.Text != "", "text",
			b.Signature != "", "signature",
			b.ToolUseID != "", "toolUseId",
			b.Content != "", "content",
			b.IsError, "isError",
			b.Data != "", "data",
			b.MediaType != "", "mediaType",
			b.Document != "", "document",
			b.Rev != 0, "rev")

	case BlockToolResult:
		if b.ToolUseID == "" {
			return fmt.Errorf("model: a tool_result block has no toolUseId; nothing could match it to a call")
		}
		return b.reject("tool_result", b.Text != "", "text",
			b.Signature != "", "signature",
			b.ID != "", "id",
			b.Name != "", "name",
			len(b.Input) > 0, "input",
			b.Data != "", "data",
			b.MediaType != "", "mediaType",
			b.Document != "", "document",
			b.Rev != 0, "rev")

	case BlockImage, BlockDocument:
		// S69. The name may be empty (a paste has none); the mediaType may not
		// — the provider wire needs it, and an empty one is an invention
		// waiting to happen at the adapter.
		what := "image"
		if b.Kind == BlockDocument {
			what = "document"
		}
		if b.MediaType == "" {
			return fmt.Errorf("model: an attached %s has no mediaType; the provider wire needs it", what)
		}
		hasData := b.Data != ""
		hasRef := b.Document != "" || b.Rev != 0
		switch {
		case hasData && hasRef:
			return fmt.Errorf("model: an attached %s carries both data and a document reference (S69: the stored form is the reference, the wire form is the data — never both)", what)
		case !hasData && !hasRef:
			return fmt.Errorf("model: an attached %s carries neither data nor a document reference — it could render as nothing", what)
		case hasRef && b.Document == "":
			return fmt.Errorf("model: an attached %s pins rev %d of no document", what, b.Rev)
		case hasRef && b.Rev <= 0:
			return fmt.Errorf("model: an attached %s references document %q without a revision", what, b.Document)
		}
		return b.reject(what, b.Text != "", "text",
			b.Signature != "", "signature",
			b.ID != "", "id",
			len(b.Input) > 0, "input",
			b.ToolUseID != "", "toolUseId",
			b.Content != "", "content",
			b.IsError, "isError")

	case "":
		return fmt.Errorf("model: a block has no kind")
	default:
		return fmt.Errorf("model: unknown block kind %q; the kinds are text, thinking, "+
			"tool_use, tool_result, image and document (M4 as widened by S69)", b.Kind)
	}
}

// reject reports the first foreign field present, named.
//
// Variadic (condition, name) pairs so each kind's rule reads as a list rather
// than a block of ifs — the list is the specification, and a missing line is
// visible where a missing `if` is not.
func (b Block) reject(kind string, pairs ...any) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		present, _ := pairs[i].(bool)
		name, _ := pairs[i+1].(string)
		if present {
			return fmt.Errorf("model: a %s block carries %q, which belongs to another kind", kind, name)
		}
	}
	return nil
}

// --- usage -------------------------------------------------------------------

// Usage is what a provider reported. **Every field is a pointer** (M9).
//
// # Why pointers rather than values with presence flags
//
// Both were on the table and the choice has to be stated, because Go's zero
// value makes "absent" and "zero" the same thing by default — and that is
// precisely the confusion M9 exists to prevent: a step that reported no usage
// must not look like a step that cost nothing.
//
// A pointer IS "absent or present", in one field, and it survives a JSON round
// trip in both directions with nothing to keep in sync. The alternative —
// `InputTokens int` plus `HasInputTokens bool` — doubles the field count and
// introduces a state where the two disagree, which no test would catch because
// both halves look right in isolation.
//
// The cost is `*int` at every read site. That is the price of the distinction,
// and `cost.go` is the only place that pays it often.
type Usage struct {
	InputTokens      *int `json:"inputTokens,omitempty"`
	OutputTokens     *int `json:"outputTokens,omitempty"`
	CacheReadTokens  *int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int `json:"cacheWriteTokens,omitempty"`
	// ReasoningTokens is reported by some providers for thinking output. Billed
	// as output where it is reported separately.
	ReasoningTokens *int `json:"reasoningTokens,omitempty"`
	// ReportedCostUSD is the cost the PROVIDER itself reported for the call, when it does (OpenRouter-style and
	// vLLM gateways send `usage.cost`). It is the provider's own figure, so it needs no price on the route and is
	// exact; absent means the provider said nothing, which is not zero.
	ReportedCostUSD *float64 `json:"reportedCostUsd,omitempty"`
}

// IntPtr is a helper for building a Usage in a test or an adapter.
func IntPtr(n int) *int { return &n }

// Any reports whether the provider reported ANY usage. Every field is
// optional (M9), so "nothing at all arrived" is a fact — the stream-usage
// probe's whole question — and it must be askable without five nil checks at
// each caller drifting into five different answers.
func (u Usage) Any() bool {
	return u.InputTokens != nil || u.OutputTokens != nil ||
		u.CacheReadTokens != nil || u.CacheWriteTokens != nil || u.ReasoningTokens != nil || u.ReportedCostUSD != nil
}

// Empty reports whether a provider told us nothing at all.
func (u Usage) Empty() bool {
	return u.InputTokens == nil && u.OutputTokens == nil &&
		u.CacheReadTokens == nil && u.CacheWriteTokens == nil && u.ReasoningTokens == nil
}

// --- cost --------------------------------------------------------------------

// CostState is one of three answers, never two (P31).
type CostState string

const (
	// CostKnown — every token count needed was reported and priced.
	CostKnown CostState = "known"
	// CostPartial — some counts arrived; USDAtLeast is a floor and Missing says
	// what is absent.
	CostPartial CostState = "partial"
	// CostUnknown — nothing usable arrived. Reason says why.
	CostUnknown CostState = "unknown"
)

// Cost is what a call cost, in one of three honest states (M10).
//
// **A two-state cost is a cost that lies.** The third state exists because
// providers do not always report usage on a streaming request, and the
// alternative is reporting `$0.00` for a call that certainly cost something —
// which reaches the UI at S9 looking like a cheap step.
type Cost struct {
	// State has NO omitempty, deliberately (O2).
	//
	// A genuinely-known zero cost must still serialise its state. This struct
	// tag is the single place M9's "absent is not zero" can be undone silently,
	// and a test asserts it.
	State CostState `json:"state"`

	USD        float64  `json:"usd,omitempty"`
	USDAtLeast float64  `json:"usdAtLeast,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// --- routes ------------------------------------------------------------------

// Route is a provider, a model and its prices — **data, not code** (M12).
//
// Adding Gemini is a new adapter file and a new route, not a change here.
//
// # No ambient state (I5)
//
// Credentials and base URLs arrive ON THE ROUTE. **No adapter calls
// `os.Getenv`.** That invariant is what makes this layer testable without
// secrets, and its violation passes review every time because it works on the
// author's machine.
type Route struct {
	DisplayName        string   `json:"displayName,omitempty"`
	DisplayDescription string   `json:"displayDescription,omitempty"`
	Icon               string   `json:"icon,omitempty"`
	Capabilities       []string `json:"capabilities"`
	ReasoningEfforts   []string `json:"reasoningEfforts"`
	ID                 string   `json:"id"`
	Provider           string   `json:"provider"`
	Model              string   `json:"model"`
	BaseURL            string   `json:"baseUrl,omitempty"`

	// APIKey is never serialised (`json:"-"`).
	//
	// A Route can end up inside an event payload or a log line by accident —
	// and this slice is the one most likely to commit a key. Making it
	// unmarshallable means the accident cannot leak it, rather than relying on
	// every call site to remember.
	APIKey string `json:"-"`

	// MaxOutputTokens is REQUIRED (M16). Anthropic rejects a call without it,
	// so a zero here is a configuration error caught before the wire.
	MaxOutputTokens int `json:"maxOutputTokens"`

	// ContextWindow is the route's known input budget. Zero means unknown;
	// compaction cannot make a defensible decision without a bound.
	ContextWindow int `json:"contextWindow,omitempty"`

	// Prices is nil when the route has none, which yields an `unknown` cost
	// rather than a free one.
	Prices *Prices `json:"prices,omitempty"`

	// NoPrice is the explicit declaration that a route is UNMETERED (O8). It
	// is what separates "someone decided there is no price" from "the price
	// was forgotten" — and without it the second masquerades as a permanent,
	// honest-looking `unknown` cost, because `unknown` is a legitimate answer
	// (M9) that nobody thinks to re-check.
	//
	// The refusal of a silently missing price fires where NEW routes are
	// persisted (the store's write gate); rows that predate the rule load and
	// render `Priced:false`, which is the honest warning `RouteView` already
	// carries. Environment routes are untouched: they are ephemeral
	// configuration, not the ledger.
	NoPrice bool `json:"noPrice,omitempty"`

	// WireCapabilities declares how this route's SERVER behaves on the wire
	// (S18 Q8) — the five flags validated in capability.go. A different axis
	// from `Capabilities` above, which says what the MODEL is for: both may
	// contain the string "thinking", they validate through separate code
	// paths, and no coupling rule between them is invented (a route may
	// declare `tool-calling` as a model capability while its server lacks
	// wire `tools` — a coherent statement about two different things).
	//
	// nil means UNDECLARED; the empty slice means DECLARED-NONE. The state
	// these claims are in (declared/verified/contradicted) is NOT stored on
	// the route — it belongs to the probe cache (T3), because a route row
	// asserting `verified` would be a route claiming a fact it never earned.
	WireCapabilities []string `json:"wireCapabilities,omitempty"`

	// ReasoningEffort is how hard a reasoning model should think, when the
	// provider has such a dial. Empty leaves it unset, which means the model's
	// own default applies.
	//
	// # Why this is a first-class field and not ExtraBody
	//
	// It was ExtraBody, and that was wrong for a reason a real 400 made
	// obvious. OpenAI's current models refuse FUNCTION TOOLS and reasoning
	// together on `/v1/chat/completions`:
	//
	//	Function tools with reasoning_effort are not supported for gpt-6-luna
	//	in /v1/chat/completions. To use function tools, use /v1/responses or
	//	set reasoning_effort to 'none'.
	//
	// Aeon's steps ALWAYS offer tools (S5). So on those models every step fails
	// until this is set to `none` — which makes it a field somebody has to
	// reach for on their first day, and the escape hatch becoming the main road
	// is how an escape hatch stops being reviewable.
	//
	// **Per PROVIDER, not per request.** Anthropic has no such parameter; its
	// equivalent is a thinking budget, which this format does not model yet.
	// The Anthropic adapter therefore IGNORES this field, asserted by a test —
	// a parameter sent to the provider that does not know it is a 400 about a
	// field the caller never set.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// OpenAIEndpoint picks WHICH OpenAI API a route talks to: `chat` (the
	// default) or `responses`.
	//
	// # Why a route needs this at all
	//
	// `/v1/chat/completions` **refuses function tools while reasoning is on**,
	// and `/v1/responses` does not. So the question "can this step reason while
	// using tools" is answered by the endpoint, not by the model — and a person
	// who wants both has to be able to say so.
	//
	// They are two different wire formats in both directions, not one path with
	// a flag: `input` instead of `messages`, top-level `function_call` items
	// instead of nested tool calls, and a `response.*` event vocabulary instead
	// of `choices[].delta`. Hence two adapter implementations behind one
	// provider id.
	//
	// Empty means `chat`, so every route that worked before this field existed
	// still works exactly the same way.
	//
	// Ignored by every provider but OpenAI, the way `ReasoningEffort` is — with
	// a test asserting it never reaches an Anthropic body.
	OpenAIEndpoint string `json:"openaiEndpoint,omitempty"`

	// AudioAPI says how this route's speech endpoints are spoken, for a route with the `asr` or `tts` capability.
	// Servers disagree on the path and the body, so the person says which one this is. Empty means `openai`.
	// Ignored by a route that neither hears nor speaks.
	AudioAPI string `json:"audioApi,omitempty"`

	// ExtraBody is the ONE escape hatch for a provider field this format does
	// not model (M11). Merged into the wire body by the adapter; unknown
	// provider fields are otherwise dropped.
	//
	// It still overrides `ReasoningEffort`, because it merges last — which is
	// what makes it an escape hatch rather than a suggestion.
	ExtraBody json.RawMessage `json:"extraBody,omitempty"`
}

// Prices are USD PER MILLION TOKENS, and every field name says so (M25).
//
// # Why the unit is in the name
//
// A unit ambiguity here produces figures wrong by a factor of a million that
// look plausible in both directions: $0.000003 and $3.00 are both believable
// for one call, and only one is right. A field called `PriceInput` would be
// read as per-token by half its readers.
//
// Each is a pointer, so an unpriced field is ABSENT rather than zero — a price
// of zero is a legitimate thing to configure (a free local model), and it must
// not be confused with "nobody told us the price".
type Prices struct {
	InputUSDPerMTok      *float64 `json:"inputUsdPerMTok,omitempty"`
	OutputUSDPerMTok     *float64 `json:"outputUsdPerMTok,omitempty"`
	CacheReadUSDPerMTok  *float64 `json:"cacheReadUsdPerMTok,omitempty"`
	CacheWriteUSDPerMTok *float64 `json:"cacheWriteUsdPerMTok,omitempty"`
}

// Any reports whether at least one field is priced. A Prices with nothing set
// is indistinguishable from no Prices at all, and both mean `unknown`.
func (p *Prices) Any() bool {
	if p == nil {
		return false
	}
	return p.InputUSDPerMTok != nil || p.OutputUSDPerMTok != nil ||
		p.CacheReadUSDPerMTok != nil || p.CacheWriteUSDPerMTok != nil
}

// Float64Ptr is a helper for building a Route in a test or a config loader.
func Float64Ptr(f float64) *float64 { return &f }

// --- requests ----------------------------------------------------------------

// ToolSpec is a tool offered to the model.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Parameters is a JSON Schema, and **this layer never inspects it** (M22).
	//
	// The schema is authored where the tool is (S6 owns the tool set). Parsing
	// it here would mean validating it here, which is a second owner for a
	// contract that already has one — and the two would disagree.
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ModelRequest is the wire-level request (M18).
//
// `StepRequest` — the runtime's notion of a step, with its budget and its
// tools — belongs to S5. Keeping them separate is what stops this layer
// knowing what a step is.
type ModelRequest struct {
	// Route carries the provider, the model, the credential and the prices.
	//
	// On the request rather than a separate parameter because the route is part
	// of what is being asked for: the same messages against a different route
	// are a different call, with a different cost. An earlier draft passed it
	// alongside and the redundancy showed up immediately in `Stream`, which had
	// to choose which one won.
	Route Route `json:"route"`

	Messages []Message  `json:"messages"`
	Tools    []ToolSpec `json:"tools,omitempty"`

	// MaxOutputTokens overrides the route's when non-zero.
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`

	// ThinkingBudgetTokens asks for extended thinking where the provider
	// supports it. Zero means "do not ask".
	ThinkingBudgetTokens int `json:"thinkingBudgetTokens,omitempty"`

	// ReasoningEffort overrides the route's for THIS call. Empty takes the
	// route's.
	//
	// # Why per-request and not only per-route
	//
	// One route cannot serve both halves of the product. A chat wants the model
	// to think; a STEP cannot have it thinking at all, because OpenAI's current
	// models refuse function tools unless reasoning is off — and every step
	// offers tools.
	//
	// So a route carrying `medium` is right for `aeon ask` and fatal for `aeon
	// step`, and asking somebody to keep two routes for one model is asking
	// them to maintain a workaround. The call decides; the route is the
	// default.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// EffectiveReasoningEffort is what this call will actually send.
//
// One function rather than the same two-line precedence in the adapter, the
// error message and the tests — which is where "the request wins" would stop
// being true in one of them.
func (r ModelRequest) EffectiveReasoningEffort() string {
	if r.ReasoningEffort != "" {
		return r.ReasoningEffort
	}
	return r.Route.ReasoningEffort
}

// Validate checks a request before any adapter sees it.
func (r ModelRequest) Validate() error {
	if err := r.Route.ValidateEffort(r.EffectiveReasoningEffort()); err != nil {
		return err
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("model: a request has no messages")
	}
	for i, m := range r.Messages {
		if len(m.Content) == 0 {
			return fmt.Errorf("model: message %d (%s) has empty content", i, m.Role)
		}
		for j, b := range m.Content {
			if err := b.Validate(); err != nil {
				return fmt.Errorf("model: message %d block %d: %w", i, j, err)
			}
		}
	}
	for i, t := range r.Tools {
		if t.Name == "" {
			return fmt.Errorf("model: tool %d has no name", i)
		}
	}
	return nil
}

// --- streaming ---------------------------------------------------------------

type DeltaKind string

const (
	// DeltaText is incremental assistant text.
	DeltaText DeltaKind = "text"
	// DeltaThinking is incremental reasoning.
	DeltaThinking DeltaKind = "thinking"
	// DeltaToolUse carries a COMPLETE tool_use block.
	DeltaToolUse DeltaKind = "tool_use"
	// DeltaStop ends the stream. Reason says why.
	DeltaStop DeltaKind = "stop"
)

// Delta is one canonical streaming increment.
//
// # This type is the enforcement point for M8
//
// **There is no field an adapter could use to express a fragment.** No index,
// no partial-arguments string, no "more to come" flag — so an adapter *cannot*
// yield half a tool call, which is a stronger guarantee than a rule in a
// document. A tool call arrives fragmented on both providers; reassembly
// happens inside the adapter and nothing partial escapes.
//
// A consumer that received half a `tool_use` would have to know how to finish
// it, and reassembly would then live in two places — one of which gets tested.
type Delta struct {
	Kind DeltaKind `json:"kind"`

	// Text is set for DeltaText and DeltaThinking.
	Text string `json:"text,omitempty"`

	// Block is set only for DeltaToolUse, and **is always complete** — every
	// field of the tool call is populated and `Validate` passes. A partial
	// block is not representable here; see the type's doc comment.
	Block *Block `json:"block,omitempty"`

	// Reason is set for DeltaStop: the provider's stop reason, verbatim.
	Reason string `json:"reason,omitempty"`
}

// --- the adapter contract ----------------------------------------------------

// Adapter translates between the canonical format and one provider's wire.
//
// # Why this interface lives in `model` and not in `adapters`
//
// **The task's layout put it in `adapters/registry.go`, and that cannot
// compile.** Its method signatures mention `ModelRequest`, `Frame`, `Delta` and
// `Usage`, so `adapters` must import `model` — and `client.go` lives in `model`
// and needs an adapter, so `model` would import `adapters`. That is an import
// cycle, demonstrated before this was changed rather than assumed.
//
// The resolution is Go's own driver pattern, the one `database/sql` uses and
// the one this project already relies on for `sqlite_vec`: **the contract and
// the registry belong to the consumer, and implementations register
// themselves.** `adapters/registry.go` is still the aggregation point — it
// holds every `RegisterAdapter` call — so M12's promise is unchanged: a third
// provider is one file in `adapters/` plus one line there.
//
// Recorded as a deviation in T5's notes.
type Adapter interface {
	// Provider is the id a Route names.
	Provider() string

	// ToWire builds the HTTP request. **It performs no I/O** — it returns the
	// request and sends nothing, which is what makes it testable without a
	// network and what keeps `client.go` the only place a request is sent.
	ToWire(req ModelRequest) (*http.Request, error)

	// FromWire translates provider frames into canonical deltas. It closes the
	// returned channel when `frames` closes.
	FromWire(ctx context.Context, frames <-chan Frame) <-chan Delta

	// Finalise reports what the stream added up to: usage, the stop reason, and
	// the number of content blocks that mapped to no canonical kind (M4).
	//
	// # Why this is separate from the stream
	//
	// Usage arrives at different points per provider — a final chunk for
	// OpenAI, `message_start` AND `message_delta` for Anthropic — so a caller
	// needs it **after** the stream is exhausted. Including when the stream was
	// cut short by cancellation, which is how M15 reports the spend on a
	// cancelled call rather than discarding it.
	Finalise() (usage Usage, stopReason string, dropped int)
}

// AdapterFactory builds a fresh adapter. One per call, never shared: an adapter
// accumulates fragment state across a stream, so a shared instance would
// interleave two concurrent calls' tool arguments.
type AdapterFactory func() Adapter

var (
	adapterMu sync.RWMutex
	adapters  = map[string]AdapterFactory{}
)

// RegisterAdapter registers a provider's adapter. Called from an `init` in
// `adapters/registry.go`.
//
// Panics on a duplicate, like `sql.Register`: two adapters for one provider id
// means the one that wins depends on initialisation order, which is not
// something to debug at runtime.
func RegisterAdapter(provider string, factory AdapterFactory) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	if _, dup := adapters[provider]; dup {
		panic(fmt.Sprintf("model: adapter for provider %q registered twice", provider))
	}
	adapters[provider] = factory
}

// RegisterAdapterReplacing registers an adapter, replacing any existing one.
//
// Test-only in practice: production registration goes through RegisterAdapter,
// which panics on a duplicate so that initialisation order never decides which
// adapter a provider gets. A test substituting a fake needs the opposite, and
// giving it a differently-named function keeps the panic in the path that
// matters.
func RegisterAdapterReplacing(provider string, factory AdapterFactory) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	adapters[provider] = factory
}

// RegisteredProviders lists what is registered, sorted. Used by the error below
// and by `aeon debug roundtrip`.
func RegisteredProviders() []string {
	adapterMu.RLock()
	defer adapterMu.RUnlock()
	out := make([]string, 0, len(adapters))
	for name := range adapters {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// AdapterFor resolves a provider id to a fresh adapter.
//
// The failure NAMES WHAT IS AVAILABLE (O6). "provider not found" on its own is
// a support question, and at this stage the list is short enough to print —
// which is also true of the route list, so both failures print their options.
func AdapterFor(provider string) (Adapter, error) {
	adapterMu.RLock()
	factory, ok := adapters[provider]
	adapterMu.RUnlock()
	if !ok {
		available := RegisteredProviders()
		if len(available) == 0 {
			return nil, fmt.Errorf("model: no adapter for provider %q, and none is registered at "+
				"all — something must import internal/model/adapters for its init to run", provider)
		}
		return nil, fmt.Errorf("model: no adapter for provider %q; registered providers are %s",
			provider, strings.Join(available, ", "))
	}
	return factory(), nil
}

// resetAdaptersForTest clears the registry. Test-only, and unexported so
// nothing in production can empty it.
func resetAdaptersForTest() {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	adapters = map[string]AdapterFactory{}
}

// ReasoningEfforts is the accepted set for `Route.ReasoningEffort`.
//
// # Why validate at all, rather than forwarding whatever was typed
//
// A typo becomes a provider 400 about a field the person thought they had set
// correctly, three layers from where they typed it. The set is small, documented
// and stable enough to check — and a value this list does not know is far more
// likely to be a typo than a new tier.
//
// `none` is the one that matters most: it is what makes function tools work at
// all on OpenAI's reasoning models over `/v1/chat/completions`, and it is the
// value the provider's own error message names.
func ReasoningEfforts() []string {
	return []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
}

// ValidReasoningEffort reports whether a value is one Aeon will send.
//
// Empty is valid and means "unset" — the model's own default applies, which is
// the right behaviour for a provider with no such dial.
func ValidReasoningEffort(v string) bool {
	if v == "" {
		return true
	}
	for _, allowed := range ReasoningEfforts() {
		if allowed == v {
			return true
		}
	}
	return false
}

// The OpenAI API surfaces a route can name.
const (
	// OpenAIEndpointChat is `/v1/chat/completions` — the default, and the only
	// one before this existed.
	//
	// It refuses function tools while reasoning is on, which is the whole
	// reason the other value exists.
	OpenAIEndpointChat = "chat"

	// OpenAIEndpointResponses is `/v1/responses`, which accepts tools AND
	// reasoning together.
	OpenAIEndpointResponses = "responses"
)

// OpenAIEndpoints is the accepted set for `Route.OpenAIEndpoint`.
func OpenAIEndpoints() []string {
	return []string{OpenAIEndpointChat, OpenAIEndpointResponses}
}

// ValidOpenAIEndpoint reports whether a value names an API Aeon can speak.
//
// Empty is valid and means `chat`: a route written before this field existed
// must keep working, and defaulting it in one predicate is better than
// defaulting it in each of the four layers a route passes through.
func ValidOpenAIEndpoint(v string) bool {
	if v == "" {
		return true
	}
	for _, allowed := range OpenAIEndpoints() {
		if allowed == v {
			return true
		}
	}
	return false
}

// EffectiveOpenAIEndpoint is the endpoint this route talks to.
func (r Route) EffectiveOpenAIEndpoint() string {
	if r.OpenAIEndpoint == "" {
		return OpenAIEndpointChat
	}
	return r.OpenAIEndpoint
}

// The speech APIs a route can speak. Which applies depends on the capability: `openai` is `/audio/speech` and
// `/audio/transcriptions`; `synthesize` and `synthesize-stream` are speech-only (`/synthesize`, `/synthesize/stream`);
// `chat` is an omni model that hears and speaks through chat completions, one model for both.
const (
	AudioAPIOpenAI           = "openai"
	AudioAPISynthesize       = "synthesize"
	AudioAPISynthesizeStream = "synthesize-stream"
	AudioAPIChat             = "chat"
)

// AudioAPIs is the accepted set for `Route.AudioAPI`.
func AudioAPIs() []string {
	return []string{AudioAPIOpenAI, AudioAPISynthesize, AudioAPISynthesizeStream, AudioAPIChat}
}

// ValidAudioAPI reports whether a value names a speech API Aeon can speak. Empty is valid and means `openai`.
func ValidAudioAPI(v string) bool {
	if v == "" {
		return true
	}
	for _, allowed := range AudioAPIs() {
		if allowed == v {
			return true
		}
	}
	return false
}

// EffectiveAudioAPI is the speech API this route talks to.
func (r Route) EffectiveAudioAPI() string {
	if r.AudioAPI == "" {
		return AudioAPIOpenAI
	}
	return r.AudioAPI
}

// ProviderOpenAI is the one provider id this package reasons about.
//
// `adapters` owns the canonical constant and a test there asserts the two
// agree. The duplication is deliberate and bounded: `model` cannot import
// `adapters` (the adapter contract is made of `model`'s types, so the import
// would be a cycle — demonstrated before this shape was chosen), and the
// alternative was an optional interface on the Adapter just to answer one
// question about a route.
const ProviderOpenAI = "openai"

// ToolsRefuseReasoning reports whether this route's API refuses function tools
// while reasoning is on.
//
// # The measured rule
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// True for OpenAI on `/v1/chat/completions`, which is the default. **False on
// `/v1/responses`**, which accepts both — and false for every other provider:
// Anthropic's thinking coexists with tools, and inventing a restriction for a
// provider that does not have one would remove a capability for nothing.
//
// It lives on the ROUTE because the route is what carries both facts, and
// because a step has to answer this question before any adapter is chosen.
func (r Route) ToolsRefuseReasoning() bool {
	return r.Provider == ProviderOpenAI && r.EffectiveOpenAIEndpoint() == OpenAIEndpointChat
}
