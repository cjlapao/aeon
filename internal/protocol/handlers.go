package protocol

// The handlers' socket reads, added for the bench demo runner (spec
// 2026-10-25-bench-demo-runner). Both answer REFUSALS AS DATA — the T10
// retry pattern — because a demo step that fails is a VERDICT the runner
// renders, not an exception that toasts the panel away. Neither writes
// anything (PL15's door stays shut: policy and the registry are CLI-authored;
// the socket only reads them), and the original-body read is NAME-listed with
// a basename-only answer: the caller learns which tokens an original names,
// never where the file is.

import (
	"context"
	"encoding/json"
	"fmt"
)

// HandlersListParams takes nothing: the registry is embedded, so the answer
// is the same for every caller and needs no scope.
type HandlersListParams struct{}

// HandlerView is one registry entry. Kind is the closed three: "body" (an
// S13 rewrite), "preset" (the daemon's two pre-S13 names — the truth behind
// "27 on the socket, 25 in the corpus"), and it is what keeps the listing
// honest without the reader knowing which files exist.
type HandlerView struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Tools []string `json:"tools,omitempty"`
	// Tech is what the shipped body declares it serves (empty when none).
	Tech []string `json:"tech,omitempty"`
}

// TechTagView is one entry of the closed tech vocabulary: what an agent may
// declare and what `agent: by-tech-tag` may ask for.
type TechTagView struct {
	Tag   string `json:"tag"`
	Label string `json:"label"`
	Group string `json:"group"` // language | framework | platform | area
}

// ToolKitView is one group of tools offered as a single choice: a plain-language ability with the
// registered tools that make it up. A kit is a convenience for picking, not a permission.
type ToolKitView struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Effect is how far the kit reaches: reads | changes | runs | reaches | asks.
	Effect string   `json:"effect"`
	Tools  []string `json:"tools"`
}

type HandlersListView struct {
	Handlers  []HandlerView `json:"handlers"`
	Resolvers []string      `json:"resolvers"`
	// TechTags is the closed vocabulary, in display order — the list the agent
	// form and the flow builder offer, so the app never hardcodes it.
	TechTags []TechTagView `json:"techTags"`
	// ToolKits are the groups of tools an agent can be given in one choice, in display order — held
	// against the tool registry by a test, so the app never carries a tool name of its own.
	ToolKits []ToolKitView `json:"toolKits"`
}

// HandlersVerifyParams selects one of exactly two sweeps. Scope "corpus"
// re-verifies the shipped bodies; scope "original" verifies one named
// negative-control original — by NAME only. A name with any path shape is
// ErrBadParams, not a lookup: this method cannot be pointed at files.
type HandlersVerifyParams struct {
	Scope string `json:"scope"`
	Name  string `json:"name,omitempty"`
}

type HandlersFindingView struct {
	Handler string `json:"handler"`
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Msg     string `json:"msg"`
}

// HandlersRefusalView is the refusal sentence — the thing refused and the
// remedy — arriving as the ANSWER, verbatim-renderable.
type HandlersRefusalView struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type HandlersVerifyView struct {
	Scope    string                `json:"scope"`
	Name     string                `json:"name,omitempty"`
	Passed   int                   `json:"passed,omitempty"`
	Refused  bool                  `json:"refused"`
	Findings []HandlersFindingView `json:"findings,omitempty"`
	Reason   *HandlersRefusalView  `json:"reason,omitempty"`
}

func (t *Table) handleHandlersList(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p HandlersListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if t.HandlersList == nil {
		return nil, ErrStoreUnavailable
	}
	return t.HandlersList(ctx, p)
}

var verifyScopes = map[string]bool{"corpus": true, "original": true}

func (t *Table) handleHandlersVerify(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p HandlersVerifyParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if !verifyScopes[p.Scope] {
		return nil, ErrBadParams{Err: fmt.Errorf("scope %q names no sweep — corpus verifies the shipped bodies, original verifies one named original", p.Scope)}
	}
	if p.Scope == "original" && p.Name == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("`name` is required for scope original — originals are verified one at a time, by name, never by path")}
	}
	if t.HandlersVerify == nil {
		return nil, ErrStoreUnavailable
	}
	return t.HandlersVerify(ctx, p)
}
