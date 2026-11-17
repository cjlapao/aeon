package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// `routes.trace` (test bench): one request against a route, sent once, with the wire kept on both sides — the JSON
// body the adapter built and the raw bytes the server answered — plus what the adapter made of those bytes. It is
// the tool for "the model sends something we are not showing": the per-call overrides (reasoning effort, an extra
// request body, an offered tool) change the request, and the raw reply says what the server did about them.
//
// A real request, so it is a control and not a read; nothing fires it but a person's call, and it stores nothing.

// Tracer is the optional second capability of the model layer's client.
type Tracer interface {
	Trace(ctx context.Context, req model.ModelRequest) model.TraceResult
}

type TraceRouteParams struct {
	Route  string `json:"route"`
	Prompt string `json:"prompt"`
	System string `json:"system,omitempty"`
	// ReasoningEffort overrides the route's for this call, as `ask` does. Empty takes the route's.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// ExtraBody is a JSON object merged over the route's own extra body for THIS call only: the way to try
	// `{"chat_template_kwargs":{"enable_thinking":false}}` or `{"reasoning_effort":"low"}` before saving it.
	ExtraBody       json.RawMessage `json:"extraBody,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	// OfferTool adds one harmless tool to the request, because a server can behave differently once tools are
	// offered (and every Aeon step offers them).
	OfferTool bool `json:"offerTool,omitempty"`
}

type TraceRouteResult struct {
	Route string `json:"route"`
	// Sent echoes what was overridden, so a saved result can be read without the form that produced it.
	Effort    string `json:"effort,omitempty"`
	ExtraBody string `json:"extraBody,omitempty"`
	model.TraceResult
}

// maxExtraBody bounds a saved or traced extra body: it is a handful of provider flags, not a document.
const maxExtraBody = 8 << 10

// forbiddenExtraKeys are the request fields an extra body may not replace: they are the call itself, and an
// override of them is a different call wearing this route's name.
var forbiddenExtraKeys = []string{"messages", "stream", "model"}

// ValidateExtraBody checks an extra-body text: empty (none), or one JSON object under the size cap that does not
// replace the call's own fields. It returns the compacted form.
func ValidateExtraBody(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || text == "{}" || text == "null" {
		return "", nil
	}
	if len(text) > maxExtraBody {
		return "", fmt.Errorf("extra body is %d bytes; the limit is %d", len(text), maxExtraBody)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return "", fmt.Errorf("extra body must be one JSON object: %v", err)
	}
	for _, key := range forbiddenExtraKeys {
		if _, bad := obj[key]; bad {
			return "", fmt.Errorf("extra body may not set %q: that is the call itself, not an option on it", key)
		}
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(text)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// mergeExtraBody lays over on top of base (top-level keys, the way an OpenAI client's `extra_body` merges).
func mergeExtraBody(base json.RawMessage, over string) (json.RawMessage, error) {
	if over == "" {
		return base, nil
	}
	merged := map[string]json.RawMessage{}
	if len(base) > 0 {
		if err := json.Unmarshal(base, &merged); err != nil {
			return nil, fmt.Errorf("the route's own extra body is not an object: %v", err)
		}
	}
	var add map[string]json.RawMessage
	if err := json.Unmarshal([]byte(over), &add); err != nil {
		return nil, err
	}
	for k, v := range add {
		merged[k] = v
	}
	return json.Marshal(merged)
}

func (t *Table) handleRoutesTrace(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	var p TraceRouteParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Route) == "" {
		return nil, ErrInvalidRoute.WithWhat("routes.trace needs a route id")
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("routes.trace needs a prompt")}
	}
	tracer, _ := t.Models.(Tracer)
	if t.Routes == nil || tracer == nil {
		return nil, ErrProbeUnavailable.WithWhat("this daemon has no model transport that can trace; routes.trace cannot send anything until it does")
	}
	route, ok := t.Routes.Route(p.Route)
	if !ok {
		return nil, ErrUnknownRoute.WithWhat("route %q is not configured; there is nothing to trace", p.Route)
	}
	effort := strings.TrimSpace(p.ReasoningEffort)
	if !model.ValidReasoningEffort(effort) {
		return nil, ErrBadParams{Err: fmt.Errorf("reasoningEffort %q is not one of %s", p.ReasoningEffort, strings.Join(model.ReasoningEfforts(), ", "))}
	}
	over, err := ValidateExtraBody(string(p.ExtraBody))
	if err != nil {
		return nil, ErrBadParams{Err: err}
	}
	if route.ExtraBody, err = mergeExtraBody(route.ExtraBody, over); err != nil {
		return nil, ErrBadParams{Err: err}
	}
	req := model.ModelRequest{Route: route, MaxOutputTokens: p.MaxOutputTokens, Temperature: p.Temperature, ReasoningEffort: effort}
	if p.System != "" {
		req.Messages = append(req.Messages, model.Message{Role: model.RoleSystem, Content: []model.Block{model.TextBlock(p.System)}})
	}
	req.Messages = append(req.Messages, model.Message{Role: model.RoleUser, Content: []model.Block{model.TextBlock(p.Prompt)}})
	if p.OfferTool {
		req.Tools = []model.ToolSpec{{
			Name:        "get_time",
			Description: "Returns the current time. Offered only so the request carries a tool.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		}}
	}
	res := tracer.Trace(ctx, req)
	// The body shows what was sent, but an extra body may carry a secret a person pasted: hide those values here too.
	if len(res.RequestBody) > 0 {
		res.RequestBody = json.RawMessage(redactExtraBody(res.RequestBody))
	}
	return TraceRouteResult{Route: route.ID, Effort: req.EffectiveReasoningEffort(), ExtraBody: redactExtraBody(json.RawMessage(over)), TraceResult: res}, nil
}

// --- extra body and credentials -----------------------------------------------------------------------------------
//
// An extra body is request options, but nothing stops a person pasting a token into it, and a route view must never
// carry a credential. So the view shows it with the VALUE of any secret-looking key replaced by a placeholder, and a
// save that sends the placeholder back keeps the stored value: the person can edit the options around a secret
// without ever seeing it again, the same rule `apiKey` follows.

const redactedValue = "••••"

func secretLooking(key string) bool {
	k := strings.ToLower(key)
	for _, part := range []string{"key", "token", "secret", "password", "passwd", "auth", "credential", "bearer"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, inner := range x {
			if secretLooking(k) {
				x[k] = redactedValue
			} else {
				x[k] = redactValue(inner)
			}
		}
	case []any:
		for i := range x {
			x[i] = redactValue(x[i])
		}
	}
	return v
}

// redactExtraBody is the view's form of a route's extra body: compact JSON, secret values hidden.
func redactExtraBody(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	out, err := json.Marshal(redactValue(v))
	if err != nil {
		return ""
	}
	return string(out)
}

// restoreRedacted puts the stored value back wherever the submitted text still carries the placeholder, and refuses
// a placeholder with nothing stored behind it (a typed "••••" is not a secret, it is a mistake).
func restoreRedacted(submitted string, stored json.RawMessage) (string, error) {
	if submitted == "" || !strings.Contains(submitted, redactedValue) {
		return submitted, nil
	}
	var sub, old any
	if err := json.Unmarshal([]byte(submitted), &sub); err != nil {
		return "", err
	}
	if len(stored) > 0 {
		_ = json.Unmarshal(stored, &old)
	}
	var walk func(s, o any) error
	walk = func(s, o any) error {
		switch x := s.(type) {
		case map[string]any:
			om, _ := o.(map[string]any)
			for k, v := range x {
				if str, ok := v.(string); ok && str == redactedValue {
					prev, found := om[k]
					if !found {
						return fmt.Errorf("%q is the placeholder for a hidden value, but nothing is saved under it; type the value", k)
					}
					x[k] = prev
					continue
				}
				if err := walk(v, om[k]); err != nil {
					return err
				}
			}
		case []any:
			for _, v := range x {
				if str, ok := v.(string); ok && str == redactedValue {
					return fmt.Errorf("a hidden value inside a list cannot be kept; type it again")
				}
				if err := walk(v, nil); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(sub, old); err != nil {
		return "", err
	}
	out, err := json.Marshal(sub)
	return string(out), err
}

// withThinking applies the person's thinking switch to a route for one step: it merges
// `chat_template_kwargs.enable_thinking` into the route's extra body (the field vLLM, Qwen and most compatible servers
// read), keeping any other key the route already sends, including other chat_template_kwargs. Empty changes nothing.
// A route whose extra body is not an object is left as it is: the switch cannot be applied, and an invalid saved body
// is the route's own problem, reported where it is saved.
func withThinking(route model.Route, thinking string) model.Route {
	if thinking != "on" && thinking != "off" {
		return route
	}
	body := map[string]json.RawMessage{}
	if len(route.ExtraBody) > 0 {
		if err := json.Unmarshal(route.ExtraBody, &body); err != nil {
			return route
		}
	}
	kwargs := map[string]json.RawMessage{}
	if raw, ok := body["chat_template_kwargs"]; ok {
		_ = json.Unmarshal(raw, &kwargs)
	}
	kwargs["enable_thinking"] = json.RawMessage(map[bool]string{true: "true", false: "false"}[thinking == "on"])
	merged, err := json.Marshal(kwargs)
	if err != nil {
		return route
	}
	body["chat_template_kwargs"] = merged
	out, err := json.Marshal(body)
	if err != nil {
		return route
	}
	route.ExtraBody = out
	return route
}
