package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

func TestValidateExtraBody(t *testing.T) {
	for _, empty := range []string{"", "  ", "{}", "null"} {
		if got, err := ValidateExtraBody(empty); err != nil || got != "" {
			t.Errorf("%q should clear, got %q %v", empty, got, err)
		}
	}
	got, err := ValidateExtraBody(`{ "chat_template_kwargs": { "enable_thinking": false } }`)
	if err != nil || got != `{"chat_template_kwargs":{"enable_thinking":false}}` {
		t.Errorf("compact form: %q %v", got, err)
	}
	for _, bad := range []string{`[1]`, `"x"`, `{"model":"x"}`, `{"messages":[]}`, `{"stream":false}`, `{oops`, `{"a":"` + strings.Repeat("x", maxExtraBody) + `"}`} {
		if _, err := ValidateExtraBody(bad); err == nil {
			t.Errorf("%.40q was accepted", bad)
		}
	}
}

func TestMergeExtraBodyLaysTheCallOverTheRoute(t *testing.T) {
	got, err := mergeExtraBody(json.RawMessage(`{"a":1,"b":2}`), `{"b":3,"c":4}`)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	_ = json.Unmarshal(got, &m)
	if m["a"] != 1 || m["b"] != 3 || m["c"] != 4 {
		t.Errorf("merged %v", m)
	}
	if same, _ := mergeExtraBody(json.RawMessage(`{"a":1}`), ""); string(same) != `{"a":1}` {
		t.Errorf("no override must leave the route's body alone: %s", same)
	}
}

func TestExtraBodyViewNeverShowsASecretValue(t *testing.T) {
	raw := json.RawMessage(`{"chat_template_kwargs":{"enable_thinking":false},"api_key":"sk-live-1","headers":{"Authorization":"Bearer x"}}`)
	view := redactExtraBody(raw)
	if strings.Contains(view, "sk-live") || strings.Contains(view, "Bearer") {
		t.Fatalf("a secret reached the view: %s", view)
	}
	if !strings.Contains(view, `"enable_thinking":false`) {
		t.Errorf("a plain option was hidden: %s", view)
	}
	// Saving the view back keeps the stored secret.
	back, err := restoreRedacted(view, raw)
	if err != nil || !strings.Contains(back, "sk-live-1") || !strings.Contains(back, "Bearer x") {
		t.Errorf("the stored secrets were not kept: %s %v", back, err)
	}
	// A placeholder with nothing behind it is refused.
	if _, err := restoreRedacted(`{"api_key":"••••"}`, nil); err == nil {
		t.Error("a placeholder with no stored value was accepted")
	}
}

func TestWithThinkingMergesTheSwitchAndKeepsTheRoutesOwnOptions(t *testing.T) {
	route := model.Route{ID: "r", ExtraBody: json.RawMessage(`{"reasoning_effort":"low","chat_template_kwargs":{"other":1}}`)}
	off := withThinking(route, "off")
	var body map[string]map[string]any
	_ = json.Unmarshal(off.ExtraBody, &struct{}{})
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(off.ExtraBody, &flat); err != nil {
		t.Fatal(err)
	}
	if string(flat["reasoning_effort"]) != `"low"` {
		t.Errorf("the route's own option was lost: %s", off.ExtraBody)
	}
	_ = json.Unmarshal(flat["chat_template_kwargs"], &body)
	var kw map[string]any
	_ = json.Unmarshal(flat["chat_template_kwargs"], &kw)
	if kw["enable_thinking"] != false || kw["other"] != float64(1) {
		t.Errorf("thinking off must set enable_thinking false and keep the other kwargs: %v", kw)
	}
	if on := withThinking(model.Route{ID: "r"}, "on"); string(on.ExtraBody) != `{"chat_template_kwargs":{"enable_thinking":true}}` {
		t.Errorf("on with no saved body: %s", on.ExtraBody)
	}
	if same := withThinking(route, ""); string(same.ExtraBody) != string(route.ExtraBody) {
		t.Error("an empty switch changed the route")
	}
	if string(route.ExtraBody) != `{"reasoning_effort":"low","chat_template_kwargs":{"other":1}}` {
		t.Error("the caller's route was mutated")
	}
}
