package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// Handoff extraction, ported from the superseded slice.
//
// # The three strategies, quoted (Q1)
//
// From `docs/_archive/aeon-v1-horizontal/00-core-v1/tasks/T4.md`, verbatim:
//
//	Extraction order, first match wins:
//	  1. the LAST ```json fenced block
//	  2. the LAST ``` fenced block whose body parses as JSON
//	  3. the LAST balanced top-level {...} span in the text
//
//	"Last" throughout, because a model that reasons before answering emits example
//	objects first and its real answer last.
//
// **These encode what was learned about how models actually emit JSON** — fenced,
// bare, or wrapped in prose — and re-deriving them from first principles would
// discard it. They are ported in that order, each with its quote above the
// implementation so the port is auditable.
//
// Also inherited, and easy to get wrong: *"Balanced-span scanning must respect
// string literals and escapes — a brace inside a JSON string value must not end
// the span. Do not use a regex for strategy 3."* So strategy 3 is a scanner, and
// a test feeds it a JSON string containing a brace.
//
// # What this module does NOT do
//
// It performs no model call and decides no retry. The LOOP decides to re-ask
// (T8), and that split is what lets the retry's tool-free property be asserted
// on a request object rather than inferred from behaviour.

// ExtractResult is a found handoff, or why none was found.
type ExtractResult struct {
	OK bool

	// Handoff is the parsed object.
	Handoff Handoff

	// Raw is the candidate text that was tried, so a failure can show what the
	// model actually produced. A reason without the text leaves a reader
	// guessing at what was nearly right.
	Raw string

	// Reason names what went wrong, including the failing path on a schema
	// mismatch — `handoff.status: expected one of [...], got "done "` is
	// actionable; `validation failed` is not.
	Reason string

	// Strategy records which of the three found it, for `step show --requests`
	// and for the test that asserts the order.
	Strategy int
}

// ExtractHandoff finds the handoff JSON in a transcript's final assistant turn.
//
// It reads the LAST assistant message's text blocks, because that is where the
// report is: an earlier assistant turn is a tool-calling turn, and a handoff
// found there would be an example the model wrote while reasoning.
//
// Never panics (T4's validation): a nil transcript, an empty one, one with no
// assistant turn and one whose content is malformed all return `OK: false`
// with a reason.
func ExtractHandoff(messages []model.Message) ExtractResult {
	text := lastAssistantText(messages)
	if strings.TrimSpace(text) == "" {
		return ExtractResult{Reason: "the model's final turn has no text to extract a handoff from"}
	}
	return extractFromText(text)
}

// extractFromText is the three strategies, in order, stopping at the first that
// yields parseable JSON.
func extractFromText(text string) ExtractResult {
	// Strategy 1: "the LAST ```json fenced block".
	if candidate, found := lastFencedBlock(text, "json"); found {
		if h, err := parseHandoff(candidate); err == nil {
			return ExtractResult{OK: true, Handoff: h, Raw: candidate, Strategy: 1}
		} else if strings.TrimSpace(candidate) != "" {
			// A ```json block that does not parse is reported rather than
			// falling through silently: the model SAID this was JSON, and a
			// quiet fallback to strategy 3 would hide a near miss that is
			// usually a trailing comma.
			return ExtractResult{Raw: candidate, Strategy: 1, Reason: fmt.Sprintf(
				"the final ```json block does not parse as JSON: %v", err)}
		}
	}

	// Strategy 2: "the LAST ``` fenced block whose body parses as JSON".
	for _, candidate := range reversed(allFencedBlocks(text)) {
		if h, err := parseHandoff(candidate); err == nil {
			return ExtractResult{OK: true, Handoff: h, Raw: candidate, Strategy: 2}
		}
	}

	// Strategy 3: "the LAST balanced top-level {...} span in the text".
	if candidate, found := lastBalancedObject(text); found {
		h, err := parseHandoff(candidate)
		if err == nil {
			return ExtractResult{OK: true, Handoff: h, Raw: candidate, Strategy: 3}
		}
		return ExtractResult{Raw: candidate, Strategy: 3, Reason: fmt.Sprintf(
			"the final JSON-looking span does not parse: %v", err)}
	}

	return ExtractResult{Reason: "no JSON object was found in the model's final turn: no ```json block, " +
		"no fenced block containing JSON, and no balanced {…} span"}
}

func parseHandoff(candidate string) (Handoff, error) {
	var h Handoff
	if err := json.Unmarshal([]byte(candidate), &h); err != nil {
		return nil, err
	}
	if h == nil {
		// `null` parses into a nil map. A handoff that is literally `null` is
		// not a report, and treating it as an empty one would make a model
		// saying nothing look like a model reporting nothing wrong.
		return nil, fmt.Errorf("the JSON is `null` rather than an object")
	}
	return h, nil
}

// lastAssistantText concatenates the text blocks of the last assistant message.
//
// `thinking` blocks are EXCLUDED. A model that reasons in a thinking block may
// well write an example object there, and strategy 1's "last" would then find
// the reasoning rather than the answer.
func lastAssistantText(messages []model.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != model.RoleAssistant {
			continue
		}
		var b strings.Builder
		for _, blk := range messages[i].Content {
			if blk.Kind == model.BlockText {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}

// lastFencedBlock finds the last ```<lang> block's body.
func lastFencedBlock(text, lang string) (string, bool) {
	open := "```" + lang
	var last string
	var found bool
	rest := text
	offset := 0
	for {
		i := strings.Index(rest[offset:], open)
		if i < 0 {
			break
		}
		start := offset + i + len(open)
		// The fence may be followed by a newline, or by nothing at all on a
		// malformed block.
		if nl := strings.IndexByte(rest[start:], '\n'); nl >= 0 {
			start += nl + 1
		}
		end := strings.Index(rest[start:], "```")
		if end < 0 {
			// An unterminated fence: take the remainder. A model that was cut
			// off mid-block has still said what it meant, and refusing it
			// would turn a truncated reply into no reply.
			last, found = rest[start:], true
			break
		}
		last, found = rest[start:start+end], true
		offset = start + end + 3
	}
	return last, found
}

// allFencedBlocks returns every fenced block's body, in order.
func allFencedBlocks(text string) []string {
	var out []string
	offset := 0
	for {
		i := strings.Index(text[offset:], "```")
		if i < 0 {
			break
		}
		start := offset + i + 3
		if nl := strings.IndexByte(text[start:], '\n'); nl >= 0 {
			start += nl + 1
		} else {
			break
		}
		end := strings.Index(text[start:], "```")
		if end < 0 {
			out = append(out, text[start:])
			break
		}
		out = append(out, text[start:start+end])
		offset = start + end + 3
	}
	return out
}

// lastBalancedObject finds the last balanced top-level {…} span.
//
// # A scanner, not a regex, and the reason is inherited
//
// *"Balanced-span scanning must respect string literals and escapes — a brace
// inside a JSON string value must not end the span. Do not use a regex for
// strategy 3."*
//
// So this tracks whether it is inside a string and whether the previous byte was
// a backslash. A regex counting braces finds the wrong end for
// `{"summary":"fixed the {weird} case"}` — and the failure is a handoff that
// almost parses, which reads as a model mistake.
func lastBalancedObject(text string) (string, bool) {
	// Scanned from the END for the last closing brace, then matched backwards.
	// Scanning forward and keeping the last complete span costs the same and
	// reads worse, because every candidate has to be remembered.
	spans := balancedSpans(text)
	if len(spans) == 0 {
		return "", false
	}
	last := spans[len(spans)-1]
	return text[last[0] : last[1]+1], true
}

// balancedSpans returns the [start, end] of every top-level {…} span.
func balancedSpans(text string) [][2]int {
	var out [][2]int
	depth := 0
	start := -1
	inString := false
	escaped := false

	for i := 0; i < len(text); i++ {
		c := text[i]

		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}

		switch c {
		case '"':
			// Only meaningful inside an object: a quote in prose is prose.
			if depth > 0 {
				inString = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				// A stray closing brace in prose. Ignored rather than treated
				// as an error: the text is a model's reply, not a document
				// this code owns.
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, [2]int{start, i})
				start = -1
			}
		}
	}
	return out
}

func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

// --- validation --------------------------------------------------------------

// ValidateHandoff checks a handoff against the handler's required fields.
//
// # Why this is a field list and not a JSON Schema validator
//
// The handler carries a schema as TEXT, because that text is what the model is
// shown. Validating against it properly would mean a JSON Schema
// implementation, and this slice does not need one: what has to be caught is a
// model that omitted a required field or used a value outside an enumerated
// set, and both are checkable from a declaration a handler can write in two
// lines.
//
// Stated as a limitation rather than left implicit: a handoff with a field of
// the wrong TYPE is not caught here. S8 brings blueprint validation and is
// where a real schema belongs.
type HandoffSpec struct {
	// Required are field names that must be present and non-empty.
	Required []string

	// Enums constrain a field to a set. The commonest real failure is a status
	// outside the set — often with trailing whitespace, which is why the check
	// reports the value quoted.
	Enums map[string][]string
}

// ValidateHandoff reports whether a handoff satisfies the spec.
//
// The reason NAMES THE FAILING PATH, inherited from the superseded slice:
// `handoff.status: expected one of [...], got "done "` is actionable, and
// `validation failed` is not. The quoting is deliberate — it is how trailing
// whitespace becomes visible.
func ValidateHandoff(h Handoff, spec HandoffSpec) (ok bool, reason string) {
	if h == nil {
		return false, "the handoff is empty"
	}

	for _, field := range spec.Required {
		value, present := h[field]
		if !present {
			return false, fmt.Sprintf("handoff.%s: required and missing", field)
		}
		if s, isString := value.(string); isString && strings.TrimSpace(s) == "" {
			return false, fmt.Sprintf("handoff.%s: required and empty", field)
		}
	}

	for field, allowed := range spec.Enums {
		value, present := h[field]
		if !present {
			continue // a missing field is Required's business, not this one's
		}
		s, isString := value.(string)
		if !isString {
			return false, fmt.Sprintf("handoff.%s: expected one of [%s], got a non-string value",
				field, strings.Join(allowed, ", "))
		}
		if !containsExactly(allowed, s) {
			return false, fmt.Sprintf("handoff.%s: expected one of [%s], got %q",
				field, strings.Join(allowed, ", "), s)
		}
	}
	return true, ""
}

// containsExactly matches without trimming.
//
// Deliberately exact: `"done "` must FAIL rather than be accepted, because
// accepting it means the next slice compares a status against `"done"` and
// finds it does not match. The error quotes the value so the whitespace is
// visible — which is the whole reason the inherited message quoted it.
func containsExactly(allowed []string, value string) bool {
	for _, a := range allowed {
		if a == value {
			return true
		}
	}
	return false
}

// OutcomeFromHandoff maps a handoff's status to one of the four outcomes.
//
// `reject` is NOT a failure (the handler decided the answer is no), and a status
// the handler did not enumerate is `failed` rather than a guess — guessing
// would turn a malformed report into a successful one.
func OutcomeFromHandoff(h Handoff, statusField string) Outcome {
	if h == nil {
		return OutcomeFailed
	}
	status, _ := h[statusField].(string)
	switch status {
	case "ok", "success", "complete", "completed":
		return OutcomeOK
	case "reject", "rejected", "no":
		return OutcomeReject
	case "blocked":
		return OutcomeBlocked
	default:
		return OutcomeFailed
	}
}

// RetryMessages builds the re-ask, carrying the validation error.
//
// The error text goes to the model because it is the actionable part: a model
// told "that was not valid JSON" produces another invalid object, and one told
// `handoff.status: expected one of [ok, reject], got "done "` fixes it.
//
// **This builds MESSAGES, not a request.** The loop assembles the request and
// omits the tools (T8), because that is where the tool-free property can be
// asserted on an object rather than inferred.
func RetryMessages(previous []model.Message, reason string) []model.Message {
	out := append([]model.Message(nil), previous...)
	return append(out, model.Message{
		Role: model.RoleUser,
		Content: []model.Block{model.TextBlock(fmt.Sprintf(
			"That reply did not contain a usable handoff: %s\n\n"+
				"Reply with ONLY the JSON object, in a ```json fenced block, matching the schema you "+
				"were given. Do not call any tools and do not do any further work — the work is "+
				"done, this is the report.", reason))},
	})
}
