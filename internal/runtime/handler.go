package runtime

import "fmt"

// The default handler: what a step reads when no blueprint has declared it
// (Q9).
//
// # Why there is a default handler at all
//
// S8 brings blueprints, S13 brings the agent registry. At S5 the handler is a
// LITERAL (N7) — and it has to be a good one, because it is what the demo
// shows and what the first person to run `aeon step` is judged by.
//
// # What its context contains, in order
//
//	1. Task          the person's prompt, verbatim
//	2. Workspace     where the step is, and what that means for paths
//	3. How to work   the tool loop, and how to finish
//
// The TASK IS FIRST, deliberately. A model reads top to bottom, and a prompt
// buried under instructions about path conventions is a prompt the model
// weighs less than the conventions. Everything after it is support.
//
// There are no DECLARED FILES. The step reads what it needs with `read`, which
// is the whole point of having a tool loop — a handler that pre-read files
// would be choosing for the model, and at S5 nothing knows what to choose.
// That means the manifest is empty for a default step, which is correct and
// which the demo's `--manifest` output shows.

// DefaultHandlerName is what appears in events and in the step row's `agent`
// column.
//
// Not "default": a step row saying `agent=default` is one nobody can interpret
// in six months. S13's handlers will have real names, and this one says what it
// is.
//
// DASH, not colon, and that character is load-bearing (DEF-041/SEC-043): the
// policy gate asks with this name (`runTool` fills the gate request's Agent
// from Handler.Name), and the audit boundary's identifier policy refuses any
// subject id carrying a colon (SEC-013). The old "step:ad-hoc" therefore made
// EVERY ad-hoc refusal and disagreement audit-blind — enforcement held, but
// both policy_event kinds wrote zero rows and the operator's `aeon policy
// events` showed a blind zero. A colon-free mint is what keeps ad-hoc
// refusals auditable; `aeon policy events` additionally surfaces the unaudited
// state (policy.go) in case a mint ever regresses.
const DefaultHandlerName = "step-ad-hoc"

// DefaultGrantNames is the grant list for a step that did not choose one.
//
// **Workspace file tools, without a shell.** `bash` exists and is not here: granting a shell
// by default would make every example in every document an invitation, and the
// person running it would not have chosen anything (D1, W12).
//
// It is declared HERE rather than imported from `internal/tools`, because the
// runtime must not import the tools package (the dependency runs the other
// way). A test asserts the two agree, which is the honest way to have the same
// list in two packages that cannot see each other.
func DefaultGrantNames() []string { return []string{"read", "list", "write"} }

// DefaultHandoffSpec is what a default step's report must satisfy.
//
// Three required fields and one enumerated: enough that a report is usable and
// little enough that a model can produce it first time. `status` drives the
// outcome, so its set is closed — an unenumerated status becomes `failed`
// rather than a guess (see OutcomeFromHandoff).
func DefaultHandoffSpec() HandoffSpec {
	return HandoffSpec{
		Required: []string{"status", "summary"},
		Enums: map[string][]string{
			"status": {"ok", "reject", "blocked"},
		},
	}
}

// defaultHandoffSchema is the schema text the model is shown.
//
// Hand-authored rather than generated, because what the model is SHOWN and what
// is VALIDATED have different jobs: this one explains, and `DefaultHandoffSpec`
// enforces. A test asserts they agree on the field names, which is the drift
// that would otherwise go unnoticed — a schema promising a field the validator
// does not require is a field models stop sending.
const defaultHandoffSchema = `{
  "type": "object",
  "properties": {
    "status":   {"enum": ["ok", "reject", "blocked"],
                 "description": "ok if you did what was asked; reject if the honest answer is no; blocked if you could not"},
    "summary":  {"type": "string", "description": "what you found or did, in a few sentences"},
    "evidence": {"type": "array", "items": {"type": "string"},
                 "description": "file paths, commands or quotes supporting the summary"}
  },
  "required": ["status", "summary"],
  "additionalProperties": false
}`

// DefaultHandler builds the ad-hoc handler for one prompt.
func DefaultHandler(prompt string, granted []string) StepHandler {
	return StepHandler{
		Name:            DefaultHandlerName,
		System:          defaultSystemPrompt(granted),
		Sections:        defaultSections(prompt, granted),
		Tools:           granted,
		HandoffSchema:   defaultHandoffSchema,
		MaxOutputTokens: DefaultMaxOutputTokens,
	}
}

func defaultSystemPrompt(granted []string) string {
	return "You are running one bounded step inside Aeon. You have a small set of tools, a limited " +
		"number of turns, and a report to produce at the end.\n\n" +
		"Work from what you can verify. If you need to see a file, read it rather than assuming its " +
		"contents — and if something you were asked about is not there, say so rather than " +
		"describing what would usually be there.\n\n" +
		"If the honest answer to the task is \"no\" or \"this cannot be done\", say that in the " +
		"report. A rejection that is correct is a successful step; a plausible answer that is wrong " +
		"is not."
}

// defaultSections is the context, in order.
//
// The task first (see the note at the top of this file), then the facts the
// model needs to use its tools correctly.
func defaultSections(prompt string, granted []string) []SectionSpec {
	tools := "none"
	if len(granted) > 0 {
		tools = fmt.Sprintf("%v", granted)
	}

	return []SectionSpec{
		{
			// NO title, deliberately (S68, B3): this section IS the user
			// message, and the chat window renders it verbatim as "you" — a
			// `## Task` heading over a person's sentence is the daemon
			// decorating what the person said. The record still carries the
			// category; the words carry no frame.
			Title: "",
			Text:  prompt,
			// The handler's own words to the model, composed by the handler
			// side and frozen at assembly: system_prompt (S33 T4).
			Category: CategorySystemPrompt,
			// S68 B3: the Task section carries the person's own words (the
			// prompt typed at start or sent into the thread) — it rides the
			// USER message. The chat window renders this half as "you";
			// everything below is instruction and goes system-side.
			Destination: SectionTranscript,
		},
		{
			Title: "Workspace",
			Text: "Every path you give a tool is relative to the workspace root. Use `list` to find files " +
				"before guessing a path. Use `read` to inspect them and `write` to create or replace " +
				"a workspace file when the task asks for a change. Absolute paths and " +
				"`..` segments are refused, and so is a symlink that resolves outside the " +
				"workspace — if a path is refused, the error says which rule refused it.\n\n" +
				"You cannot see or change files outside the workspace.",
			Category:    CategorySystemPrompt,
			Destination: SectionSystem,
		},
		{
			Title: "How to work",
			Text: fmt.Sprintf("Your tools: %s.\n\n"+
				"Call them one at a time and read each result before deciding what to do next. "+
				"When you have what you need, stop calling tools and reply with ONLY the JSON "+
				"report in a ```json fenced block.\n\n"+
				"You have a bounded number of turns. If you run out, the step reports that it ran "+
				"out of room rather than that it failed — so spend turns on what the task actually "+
				"asked for.", tools),
			Category:    CategorySystemPrompt,
			Destination: SectionSystem,
		},
	}
}
