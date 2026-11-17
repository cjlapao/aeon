package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// question_args.go: reading the `question` tool's arguments the way models actually send them, kept out of loop.go so
// the shared loop stays about the loop.

// maxBadQuestions is how many badly formed question calls in a row the model gets to correct before the step stops.
const maxBadQuestions = 3

func pendingQuestion(use model.Block) (PendingQuestion, error) {
	var args struct {
		Text      string `json:"text"`
		Kind      string `json:"kind"`
		Options   []string
		Questions []askedQuestion
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(use.Input, &raw); err != nil {
		return PendingQuestion{}, err
	}
	if v, ok := raw["text"]; ok {
		_ = json.Unmarshal(v, &args.Text)
	}
	if v, ok := raw["kind"]; ok {
		_ = json.Unmarshal(v, &args.Kind)
	}
	// Models often send a list as a string of JSON, or a list of plain strings. Both are read as what was meant.
	args.Options = lenientStrings(raw["options"])
	qs, err := lenientQuestions(raw["questions"])
	if err != nil {
		return PendingQuestion{}, err
	}
	args.Questions = qs
	if args.Kind == "" {
		args.Kind = "free"
	}
	switch args.Kind {
	case "free", "clarify", "gate", "permission":
	default:
		return PendingQuestion{}, fmt.Errorf("kind %q is invalid", args.Kind)
	}
	// Several questions in one call are ONE question to the store: the body lists them, and each one rides in
	// Options as FormOptionPrefix+JSON so the screen can draw a single form and answer them all in one go.
	if len(args.Questions) > 1 {
		var body strings.Builder
		if t := strings.TrimSpace(args.Text); t != "" {
			body.WriteString(t + "\n\n")
		}
		opts := make([]string, 0, len(args.Questions))
		for i, q := range args.Questions {
			if strings.TrimSpace(q.Text) == "" {
				return PendingQuestion{}, fmt.Errorf("questions[%d].text is required", i)
			}
			fmt.Fprintf(&body, "%d. %s\n", i+1, strings.TrimSpace(q.Text))
			raw, _ := json.Marshal(map[string]any{"text": q.Text, "options": q.Options})
			opts = append(opts, FormOptionPrefix+string(raw))
		}
		kind := args.Kind
		if kind == "free" || kind == "permission" {
			kind = "clarify"
		}
		return PendingQuestion{ToolUseID: use.ID, Body: strings.TrimSpace(body.String()), Options: opts, Kind: kind}, nil
	}
	if len(args.Questions) == 1 {
		args.Text, args.Options = args.Questions[0].Text, args.Questions[0].Options
	}
	if strings.TrimSpace(args.Text) == "" {
		return PendingQuestion{}, fmt.Errorf("text is required")
	}
	return PendingQuestion{ToolUseID: use.ID, Body: args.Text, Options: args.Options, Kind: args.Kind}, nil
}

// askedQuestion is one sub-question of a question call.
type askedQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// lenientStrings reads a list of strings from a JSON array, or from a string that holds one (or is a single option).
func lenientStrings(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		var inner []string
		if json.Unmarshal([]byte(strings.TrimSpace(text)), &inner) == nil {
			return inner
		}
		if strings.TrimSpace(text) != "" {
			return []string{strings.TrimSpace(text)}
		}
	}
	return nil
}

// lenientQuestions reads `questions` from the shapes a model sends: an array of {text, options}, a string holding that
// array, or an array of plain question strings. Anything else is an error that says what to send.
func lenientQuestions(raw json.RawMessage) ([]askedQuestion, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var objs []askedQuestion
	if json.Unmarshal(raw, &objs) == nil {
		return objs, nil
	}
	var texts []string
	if json.Unmarshal(raw, &texts) == nil {
		out := make([]askedQuestion, 0, len(texts))
		for _, t := range texts {
			out = append(out, askedQuestion{Text: t})
		}
		return out, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		inner := strings.TrimSpace(text)
		if strings.HasPrefix(inner, "[") {
			return lenientQuestions(json.RawMessage(inner))
		}
		if inner != "" {
			return []askedQuestion{{Text: inner}}, nil
		}
	}
	return nil, fmt.Errorf("questions must be an array of {\"text\": string, \"options\": [string]}")
}

// FormOptionPrefix marks an Options entry as one sub-question of a multi-question form.
const FormOptionPrefix = "form:"
