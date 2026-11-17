package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// `CompileRequest`: a handler plus an input becomes a `StepRequest`, or fails
// before anything is called.
//
// # Everything here fails at CONSTRUCTION, and that is the whole point
//
// A handler declaring a tool the step was not granted, a missing output cap, a
// declared file that does not exist — each is caught here, with a `Client` spy
// showing zero calls (T3's assertion).
//
// The alternative is a failure three turns into a step: the model asks for
// `bash`, gets told it does not exist, tries something else, and the person
// reading the transcript sees a model working around a configuration mistake
// rather than an error naming the handler. The second is a bug report; the
// first is an afternoon.

// CompileResult is a compiled request, or why it could not be compiled.
type CompileResult struct {
	OK      bool
	Request StepRequest
	Reason  string

	// Notes are decisions assembly made that the person should see.
	//
	// Not warnings and not errors: things Aeon chose on their behalf, which
	// they would otherwise have to deduce from a provider's behaviour. They
	// reach the `step.started` event, so the CLI and the app both show them
	// without either having to know what a note is about.
	Notes []string
}

// CompileInput is what a step is compiled from.
type CompileInput struct {
	Handler StepHandler

	// Granted is the step's tool allow-list — the ONLY thing that decides what
	// the step can call (D1). A handler's `Tools` is a REQUEST; this is the
	// permission.
	Granted []string

	// Route is which model to call.
	Route model.Route

	// WorkspaceRoot is where file tools resolve. Carried on the request so no
	// tool reads ambient state (I5).
	WorkspaceRoot string

	// Specs override the handler's sections when non-nil. For the caller that
	// assembled context itself — the protocol handler adds the user's prompt as
	// a section, and a handler template should not have to know about it.
	Specs []SectionSpec

	// MaxIterations overrides the default when positive. `aeon step
	// --max-iterations` sets it, and the manifest carries it so a resumed step
	// is bounded by what the original was bounded by.
	MaxIterations int

	// StepMaxIterations is the blueprint step's declared bound (nil: none
	// declared; 0: no bound; N: a bound). It is resolved by the daemon, which
	// also knows the agent's own bound and the setting; the runtime only carries
	// it to the resolver.
	StepMaxIterations *int

	// ReasoningEffort overrides the route's for this step. Empty takes the
	// route's — and then the rule below may lower it.
	ReasoningEffort string

	// Thinking is the thinking switch ("on", "off", "") carried onto the request for a resume.
	Thinking string

	// ToolSpecs are the model-facing declarations for the granted tools,
	// supplied by `tools.BuildToolset`. Passed in rather than built here
	// because this package must not know what a tool DOES — only what the model
	// should be told exists.
	ToolSpecs []model.ToolSpec

	// History is the conversation so far (S68, B4): real alternating
	// user/assistant messages the provider sees BEFORE the operator's new
	// words — the converse thread's shape on the wire, replacing the old
	// flattened `[author] body` blob. Only `user` and `assistant` roles are
	// legal (a construction error names any other); the compiled transcript
	// is then [system, …history, user]. Empty means a fresh conversation —
	// byte-identical to the pre-S68 opening pair.
	History []model.Message

	// TaskFrame is the loop-side instruction block (S68, B2): what the step
	// is framed to do — lane, blueprint facts, artifact contract, the
	// conversation frame. It renders as the last system section under
	// `## Your task`; the person's own words stay alone in the user message.
	// Empty means no frame (the plain `aeon step` door), byte-identical to
	// the pre-S68 step.
	TaskFrame string

	// ChosenSkills are the RESOLVED instruction lines for the skills the
	// operator chose for this turn (S68, B6/B7), each already in the
	// canonical sentence family the daemon prints. The daemon resolves names
	// to lines; this package never resolves anything. Rendered as the
	// `## Chosen skills` system section, before the task frame.
	ChosenSkills []string

	// PromptAttachments are the EXPANDED content blocks for the operator's
	// prompt turn (S69 AT5), produced by the one expansion point beside
	// request compilation. nil keeps the user message byte-identical to
	// pre-S69 (D7).
	PromptAttachments []model.Block

	// LoopSlug and ComponentRoot say which loop the step belongs to, so the
	// daemon (StepRunner.Compile) can look up the loop's active todo list. They
	// decide nothing here.
	LoopSlug      string
	ComponentRoot string

	// TodoSection is the loop's active todo list, filled by the daemon at
	// compile (S75 TD7). nil — no list, a closed list, no store — leaves the
	// compile byte-identical to a step without the feature.
	TodoSection *TodoSection

	// LoopContext is the loop's index, rendered by the daemon at compile: the documents in the database, the
	// tasks, the answers the person gave, the tools this agent holds. It rides the system half so an agent starts
	// knowing what the loop has and where it lives. Empty (no loop, no store) leaves the compile byte-identical.
	LoopContext string
}

// LoopContextTitle is the system-half heading of the loop's index.
const LoopContextTitle = "Loop context"

// CompileRequest builds a `StepRequest`, or explains why it cannot.
func CompileRequest(ctx context.Context, in CompileInput, d ContextDeps) CompileResult {
	if in.Handler.Name == "" {
		return CompileResult{Reason: "the handler has no name; events and the transcript both carry it"}
	}

	// # The tool check, and why it is first
	//
	// Before assembly, because assembly READS FILES and this check is free. A
	// step that cannot legally run should not have touched the disk.
	if reason := checkGrants(in.Handler, in.Granted); reason != "" {
		return CompileResult{Reason: reason}
	}

	if in.Handler.MaxOutputTokens <= 0 {
		// Not defaulted (T3's instruction). The right cap depends on what the
		// handler asks for: one provider REQUIRES the field, and a silent
		// default truncates a long report with nothing saying why (M16).
		return CompileResult{Reason: fmt.Sprintf(
			"handler %q has no MaxOutputTokens. It is required rather than defaulted, because one "+
				"provider requires the field and a silent default truncates a long report with "+
				"nothing saying why", in.Handler.Name)}
	}
	if in.Route.ID == "" {
		return CompileResult{Reason: fmt.Sprintf(
			"handler %q has no route; a step needs a model to call", in.Handler.Name)}
	}

	specs := in.Specs
	if specs == nil {
		specs = in.Handler.Sections
	}
	if len(specs) == 0 {
		// A step with no declared context is a step with nothing to read (Q9).
		// Refused rather than run, because the model would be asked to report
		// on material it was never given and would invent the gap.
		return CompileResult{Reason: fmt.Sprintf(
			"handler %q declares no context sections; a step with nothing to read cannot report on "+
				"anything, and a model asked to will invent the gap", in.Handler.Name)}
	}

	// The skills catalog is NOT offered to the step (operator ruling
	// 2026-11-06, reversing S66's "rides every step"): the model sees a skill
	// only when the operator chose it with "/" — the Chosen skills section
	// below. The step never carries an injected-catalog marker, so `injected`
	// stays nil; the provider seam (SetSkillsProvider) is left in place.
	var injected *SkillsSection

	// S68 B6/B7: the skills the OPERATOR chose for this turn, spoken as one
	// instruction line each (the daemon resolved names to the canonical
	// sentence family). These ride the system half under their own heading;
	// they are instructions about how to answer, not the person's words —
	// the person's words are the transcript half alone (B3). nil/empty is
	// byte-identical to the pre-S68 step.
	if len(in.ChosenSkills) > 0 {
		specs = append(specs, SectionSpec{
			Title: "Chosen skills",
			Text:  strings.Join(in.ChosenSkills, "\n"),
			// The handler side instructing the model which skill to open:
			// system_prompt, the same category the persona's own words carry.
			Category: CategorySystemPrompt, Destination: SectionSystem,
		})
	}
	// S75 TD7: the loop's open todo list rides the SYSTEM half (instruction, not
	// the person's words), so the agent resumes its own plan.
	if in.TodoSection != nil {
		specs = append(specs, SectionSpec{
			Title: TodoSectionTitle, Text: in.TodoSection.body(),
			Category: CategoryInjectedContext, Destination: SectionSystem,
		})
	}
	// The loop's index, before the task frame so the frame (the instruction) stays closest to the message.
	if idx := strings.TrimSpace(in.LoopContext); idx != "" {
		specs = append(specs, SectionSpec{
			Title: LoopContextTitle, Text: in.LoopContext,
			Category: CategoryInjectedContext, Destination: SectionSystem,
		})
	}
	// S68 B2: the loop's task frame — the spoken instruction around the
	// person's goal (lane, blueprint facts, artifact contract, conversation
	// frame). It leads nothing the person said; it rides last among declared
	// system sections, closest to the message it frames.
	if frame := strings.TrimSpace(in.TaskFrame); frame != "" {
		specs = append(specs, SectionSpec{
			Title: "Your task", Text: in.TaskFrame,
			Category: CategorySystemPrompt, Destination: SectionSystem,
		})
	}

	// S68 B4: the history may only be the two conversation roles. Anything
	// else — a `tool` row smuggled as history, a `system` line trying to
	// ride mid-conversation — is refused at construction naming the offender,
	// not quietly dropped (the transcript the provider sees must be exactly
	// the conversation the store holds).
	for i, m := range in.History {
		if m.Role != model.RoleUser && m.Role != model.RoleAssistant {
			return CompileResult{Reason: fmt.Sprintf(
				"history message %d has role %q; conversation history may only be user or assistant — "+
					"system rides first and alone, and tool rows are the loop's own append points",
				i, string(m.Role))}
		}
	}
	// Adjacent same-role rows merge into ONE message (the thread can hold two
	// human notes with no reply between, or a system row dropped from the
	// wire leaves the two agents behind) — no provider accepts two in a row,
	// and ValidateTranscript says so at save time. Merging is the honest
	// shape: the words all still ride, in order, in one turn.
	history := mergeAdjacentHistory(in.History)

	assembled := AssembleContext(ctx, specs, d)
	if !assembled.OK {
		return CompileResult{Reason: assembled.Reason}
	}

	manifest := assembled.Manifest
	switch {
	case in.MaxIterations > 0:
		manifest.MaxIterations = in.MaxIterations
	case in.MaxIterations < 0:
		// Explicitly no bound (resolved by the daemon): the manifest carries 0 and
		// the loop reads 0 as "no bound".
		manifest.MaxIterations = 0
	}
	// S33 T4: the composition's system half, counted from the ONE function
	// that builds it (below) — frozen with the manifest, read back by
	// telemetry.loop (T8) as part of "what was the model actually shown".
	manifest.SystemChars = len(SystemPromptText(in.Handler))

	// S68 B3: a step whose every section chose the system half has nobody
	// speaking to it — the user message would be empty, and a provider with
	// no one's words to answer turns the step into a monologue. Refused here
	// by name, not shipped as an empty turn: this is the same Q9 argument one
	// level down (a step with nothing to read invents the gap; a step with
	// nobody talking to it invents the conversation).
	if strings.TrimSpace(assembled.TranscriptText) == "" {
		return CompileResult{Reason: fmt.Sprintf(
			"handler %q assembled an empty user message: every section declared destination %q, so "+
				"the operator's words or handed-over material are missing. At least one section must "+
				"ride the transcript — that half is what the person typed and what the chat window "+
				"shows as \"you\"", in.Handler.Name, string(SectionSystem))}
	}

	messages := OpeningTranscriptWithAttachments(in.Handler, assembled.SystemText, assembled.TranscriptText, history, in.PromptAttachments)
	if res := model.ValidateTranscript(messages); !res.OK {
		// Checked here so an invalid opening transcript is a construction
		// error rather than a provider rejection. S3's validator is the one
		// authority on what a transcript may be, and this is the first place a
		// transcript exists.
		return CompileResult{Reason: fmt.Sprintf(
			"the opening transcript for handler %q is not valid: %s", in.Handler.Name, res.Reason)}
	}

	effort, notes := reasoningFor(in)

	if in.TodoSection != nil {
		notes = append(notes, in.TodoSection.note())
	}
	if injected != nil {
		// A decision made on the operator's behalf belongs in the notes
		// (which ride step.started), not in silence: the model saw a
		// section the step's declared input never named.
		notes = append(notes, injected.MarkerSentence())
	}

	return CompileResult{OK: true, Notes: notes, Request: StepRequest{
		Handler:         in.Handler,
		SkillsInjected:  injected,
		Messages:        messages,
		Manifest:        manifest,
		Route:           in.Route,
		Tools:           in.ToolSpecs,
		WorkspaceRoot:   in.WorkspaceRoot,
		ReasoningEffort: effort,
		Thinking:        in.Thinking,
		Notes:           notes,
	}}
}

// reasoningFor decides what reasoning effort this step's calls will carry.
//
// # The rule, and why it is a choice rather than a refusal
//
// OpenAI's current models **refuse function tools unless reasoning is off** on
// `/v1/chat/completions`:
//
//	Function tools with reasoning_effort are not supported for gpt-6-luna in
//	/v1/chat/completions. To use function tools, use /v1/responses or set
//	reasoning_effort to 'none'.
//
// Every step offers tools. So a route set to `medium` — which is the right
// setting for `aeon ask` — makes every step fail, and the person is left
// maintaining two routes for one model to work around a provider's rule.
//
// There is exactly ONE configuration in which a tool-using step works on such a
// model, and Aeon knows which one. So it is used, and **said out loud** in the
// step's opening line rather than done quietly.
//
// # What makes this safe, where injecting the parameter would not be
//
// This only ever LOWERS an effort the route already set. A route carrying
// `medium` has proved the model accepts the parameter — so sending `none`
// cannot produce `400 Unrecognized request argument supplied: reasoning_effort`,
// which is exactly what injecting it into a route that set nothing could.
//
// An effort named on the CALL is left alone. Explicit means explicit: the
// person gets the provider's refusal and a message saying where they said it.
func reasoningFor(in CompileInput) (string, []string) {
	if in.ReasoningEffort != "" {
		return in.ReasoningEffort, nil
	}

	effort := in.Route.ReasoningEffort
	if len(in.ToolSpecs) == 0 || effort == "" || effort == "none" {
		return effort, nil
	}
	// # The route's API may accept both
	//
	// Asked of the ROUTE rather than decided here, because which APIs refuse
	// the combination is the model layer's knowledge, not the loop's — and
	// naming a provider's endpoint path in this package would put a wire detail
	// above the adapter that owns it (S3's D14, which caught exactly that).
	// On the `responses` endpoint nothing is lowered, which is the whole reason
	// it is selectable.
	if !in.Route.ToolsRefuseReasoning() {
		return effort, nil
	}

	return "none", []string{fmt.Sprintf(
		"reasoning lowered from %q to `none` for this step, because it offers %d tool(s) and this "+
			"route's API refuses tools while reasoning is on. `aeon ask` on route %q still uses %q. "+
			"To keep reasoning in a step, move the route to the API that accepts both: "+
			"`aeon routes set --id %s --provider %s --model %s --openai-endpoint responses`",
		effort, len(in.ToolSpecs), in.Route.ID, effort,
		in.Route.ID, in.Route.Provider, in.Route.Model)}
}

// checkGrants refuses a handler asking for a tool the step was not granted.
//
// The reason names BOTH — what was asked for and what is available — because
// "tool not granted" leaves a reader to work out which of the two lists is
// wrong, and the answer is usually that the step was started without a flag.
func checkGrants(h StepHandler, granted []string) string {
	have := make(map[string]bool, len(granted))
	for _, g := range granted {
		have[g] = true
	}

	var missing []string
	for _, want := range h.Tools {
		if !have[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)

	available := "nothing"
	if len(granted) > 0 {
		sorted := append([]string(nil), granted...)
		sort.Strings(sorted)
		available = strings.Join(sorted, ", ")
	}
	return fmt.Sprintf(
		"handler %q needs the tool(s) %s, and this step was granted %s. The grant list is the only "+
			"thing that decides what a step can call, and it is checked here rather than at the "+
			"first tool call — so the failure names the handler instead of looking like a model "+
			"working around a mistake",
		h.Name, strings.Join(missing, ", "), available)
}

// OpeningTranscript builds the first messages (S68, B1–B4): the system
// message — persona plus every instruction-destination section and the
// spoken policy — then the conversation history as real alternating
// messages, then the operator's own words as the closing user turn.
//
// # Why the halves are split, and why the handoff schema is in the SYSTEM message
//
// The report's shape is an instruction about how to answer, not part of the
// material to reason about — so it lives system-side with everything else
// that instructs. Before S68 the assembled WHOLE rode the user message, and
// the chat window faithfully replayed workspace rules and policy prose as
// something the operator had said; the split makes that impossible by
// construction, because the two halves are never concatenated into one
// message again.
//
// It is embedded once, here, from the schema the handler already carries —
// derived at the handler, never regenerated per retry (T3's point 4). The retry
// re-sends this same text, which is what makes "the retry carries no tools"
// the only difference between the two requests.
func OpeningTranscript(h StepHandler, systemText, transcriptText string, history []model.Message) []model.Message {
	return openingTranscript(h, systemText, transcriptText, history, nil)
}

// OpeningTranscriptWithAttachments is S69 AT5's second half: the operator's
// expanded content blocks join the prompt's user message — words first
// (B3 untouched), blocks beside them. nil blocks is byte-identical to
// OpeningTranscript (D7), which the plain door still calls.
func OpeningTranscriptWithAttachments(h StepHandler, systemText, transcriptText string, history []model.Message, attachments []model.Block) []model.Message {
	return openingTranscript(h, systemText, transcriptText, history, attachments)
}

func openingTranscript(h StepHandler, systemText, transcriptText string, history []model.Message, attachments []model.Block) []model.Message {
	persona := SystemPromptText(h)
	if systemText != "" {
		if !strings.HasSuffix(persona, "\n") {
			persona += "\n"
		}
		persona += "\n" + systemText
	}
	messages := make([]model.Message, 0, len(history)+2)
	messages = append(messages, model.Message{
		Role:    model.RoleSystem,
		Content: []model.Block{model.TextBlock(persona)},
	})
	messages = append(messages, history...)
	// The transcript half carries the renderer's seam newline (every section
	// is terminated with one). The user message is the operator's words, so
	// that seam is trimmed here — the ONE place the two halves are turned
	// into wire messages (B3: the user message is what they typed, alone).
	// `Text` back-compat keeps its trailing newline untouched; this boundary
	// is the only consumer that wanted it gone.
	userText := strings.TrimSuffix(transcriptText, "\n")
	userContent := append([]model.Block{model.TextBlock(userText)}, attachments...)
	messages = append(messages, model.Message{
		Role:    model.RoleUser,
		Content: userContent,
	})
	return messages
}

// mergeAdjacentHistory folds adjacent same-role history messages into one
// message, blocks concatenated in order (S68, B4). A thread row is one note;
// two human notes in a row, or an agent row left adjacent by a system row
// that never rides the wire, would be two turns in a row — which
// ValidateTranscript (and every provider) refuses. The words all still ride,
// in order, attributed to the same author.
func mergeAdjacentHistory(history []model.Message) []model.Message {
	if len(history) < 2 {
		return history
	}
	out := make([]model.Message, 0, len(history))
	for _, m := range history {
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			out[n-1].Content = append(out[n-1].Content, m.Content...)
			continue
		}
		// Copy blocks so later merges never write into the caller's slice.
		blocks := make([]model.Block, len(m.Content))
		copy(blocks, m.Content)
		out = append(out, model.Message{Role: m.Role, Content: blocks})
	}
	return out
}

// SystemPromptText is the SYSTEM half of the opening transcript, extracted
// (S33 T4): the handler's instructional text plus the embedded handoff-schema
// suffix. One definition so the manifest's SystemChars counts the EXACT
// characters the model was sent, not a nearby number — a second place that
// builds the system text would be a second truth and the count would drift
// the day the suffix changed.
func SystemPromptText(h StepHandler) string {
	var system strings.Builder
	system.WriteString(h.System)
	if h.HandoffSchema != "" {
		if !strings.HasSuffix(h.System, "\n") {
			system.WriteString("\n")
		}
		system.WriteString("\nWhen the work is done, end your reply with a single JSON object matching " +
			"this schema, in a ```json fenced block:\n\n")
		system.WriteString(h.HandoffSchema)
		system.WriteString("\n")
	}
	return system.String()
}
