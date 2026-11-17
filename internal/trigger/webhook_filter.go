package trigger

import (
	"encoding/json"
	"fmt"
	"strings"
)

// webhook_filter.go — which deliveries a webhook trigger acts on, and the goal it composes from one.
//
// A GitHub webhook sends every event it is subscribed to, and an event is more than its kind: `issues` also means edited,
// labelled, closed. The filter names the kind and optionally the actions (`issues:opened`, `pull_request:opened,reopened`);
// anything else that arrives signed is answered as not-for-this-trigger rather than starting a loop. The goal template
// puts a few fields of the payload into the goal sentence, as short single-line text: the payload is written by whoever
// opened the issue, so it is data in the goal, never an instruction, and the full payload still rides labelled.

// ParseEventFilter splits `event` or `event:action,action`. Empty is valid and means every event.
func ParseEventFilter(s string) (event string, actions []string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil, nil
	}
	head, tail, hasActions := strings.Cut(s, ":")
	event = strings.TrimSpace(head)
	if event == "" || strings.ContainsAny(event, " \t,") {
		return "", nil, fmt.Errorf("event filter %q: name one event such as `issues`, optionally `issues:opened,reopened`", s)
	}
	if hasActions {
		for _, a := range strings.Split(tail, ",") {
			a = strings.TrimSpace(a)
			if a == "" || strings.ContainsAny(a, " \t") {
				return "", nil, fmt.Errorf("event filter %q: actions are a comma list such as `opened,reopened`", s)
			}
			actions = append(actions, a)
		}
	}
	return event, actions, nil
}

// MatchEventFilter reports whether a delivery (its event kind and its payload's `action`) is what the filter asks for, and
// when it is not, why, in a sentence that names both sides.
func MatchEventFilter(filter, event string, payload []byte) (bool, string) {
	want, actions, err := ParseEventFilter(filter)
	if err != nil || want == "" {
		return true, ""
	}
	if !strings.EqualFold(strings.TrimSpace(event), want) {
		got := strings.TrimSpace(event)
		if got == "" {
			got = "no event header"
		}
		return false, fmt.Sprintf("this webhook listens for %q; the delivery is %q", filter, got)
	}
	if len(actions) == 0 {
		return true, ""
	}
	var body struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(payload, &body)
	for _, a := range actions {
		if strings.EqualFold(a, body.Action) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("this webhook listens for %q; the delivery's action is %q", filter, body.Action)
}

// maxGoalField is the longest a payload value may be inside a goal.
const maxGoalField = 200

// RenderGoal fills `{a.b.c}` placeholders from the JSON payload (dotted paths into objects, numbers for array positions).
// A value is flattened to one line, stripped of control characters and cut at maxGoalField. A path that is absent renders
// as "(none)". The template is the person's own text and passes through untouched.
func RenderGoal(template string, payload []byte) string {
	if !strings.Contains(template, "{") {
		return template
	}
	var root any
	_ = json.Unmarshal(payload, &root)
	var out strings.Builder
	for i := 0; i < len(template); {
		open := strings.IndexByte(template[i:], '{')
		if open < 0 {
			out.WriteString(template[i:])
			break
		}
		out.WriteString(template[i : i+open])
		rest := template[i+open:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(goalField(root, rest[1:end]))
		i += open + end + 1
	}
	return out.String()
}

func goalField(root any, path string) string {
	cur := root
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		switch node := cur.(type) {
		case map[string]any:
			cur = node[part]
		case []any:
			var idx int
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 0 || idx >= len(node) {
				return "(none)"
			}
			cur = node[idx]
		default:
			return "(none)"
		}
	}
	var text string
	switch v := cur.(type) {
	case nil:
		return "(none)"
	case string:
		text = v
	case float64, bool:
		text = fmt.Sprint(v)
	default:
		return "(none)"
	}
	var b strings.Builder
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	flat := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(flat); len(r) > maxGoalField {
		flat = string(r[:maxGoalField-1]) + "…"
	}
	return flat
}
