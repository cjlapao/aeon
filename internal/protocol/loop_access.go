package protocol

// loop_access.go — a loop's permission mode, the chat's "full access" switch and an unattended loop's
// pre-approved tools. Two methods:
//
//   - loop.access     reads the mode (a read: anyone who can read the loop).
//   - loop.setAccess  turns a mode on or off. It is the HUMAN door: the actor is the connection's peer, never
//     a param, and no agent surface can reach it (HumanDoorOnly) — a model that could widen its own
//     permissions would have no ceiling. Every change publishes loop.access.changed with who and what.
//
// The mode answers an ASK only (runtime.AutoApprove): an explicit deny rule still denies. "ask" is the default
// and means no stored mode.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	MethodLoopAccess    MethodName = "loop.access"
	MethodLoopSetAccess MethodName = "loop.setAccess"
)

// The three modes a person can ask for.
const (
	AccessAsk   = "ask"
	AccessTools = "tools"
	AccessFull  = "full"
)

type LoopAccessParams struct {
	ID string `json:"id"`
}

type LoopSetAccessParams struct {
	ID    string   `json:"id"`
	Mode  string   `json:"mode"`
	Tools []string `json:"tools,omitempty"`
}

// LoopAccessView is a loop's mode as the app shows it. SetBy and SetAt are empty on the default.
type LoopAccessView struct {
	ID    string   `json:"id"`
	Mode  string   `json:"mode"`
	Tools []string `json:"tools"`
	SetBy string   `json:"setBy,omitempty"`
	SetAt string   `json:"setAt,omitempty"`
	// AskingTools are the tools that ask a person by default (the product's one list), so the app can offer to
	// pre-approve exactly those instead of keeping a copy of the list.
	AskingTools []string `json:"askingTools"`
}

// LoopAccessService is the daemon's seam: reads and writes the stored mode and publishes the change event.
type LoopAccessService interface {
	Get(ctx context.Context, loopID string) (LoopAccessView, error)
	Set(ctx context.Context, actor Actor, p LoopSetAccessParams) (LoopAccessView, error)
}

// RegisterLoopAccess mounts the two methods when the seam is wired (absent, not refusing, otherwise).
func (t *Table) RegisterLoopAccess() bool {
	if t.LoopAccess == nil {
		return false
	}
	t.register(&Method{Name: MethodLoopAccess, RequiresInitialize: true, Handle: t.handleLoopAccess})
	t.register(&Method{Name: MethodLoopSetAccess, RequiresInitialize: true, Handle: t.handleLoopSetAccess})
	return true
}

func (t *Table) handleLoopAccess(ctx context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p LoopAccessParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.access needs the loop's id")}
	}
	return t.LoopAccess.Get(ctx, p.ID)
}

func (t *Table) handleLoopSetAccess(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	var p LoopSetAccessParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ID) == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("loop.setAccess needs the loop's id")}
	}
	switch p.Mode {
	case AccessAsk, AccessFull:
		if len(p.Tools) > 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("mode %q takes no tool list", p.Mode)}
		}
	case AccessTools:
		if len(p.Tools) == 0 {
			return nil, ErrBadParams{Err: fmt.Errorf("mode tools needs at least one tool name")}
		}
	default:
		return nil, ErrBadParams{Err: fmt.Errorf("mode %q is not one of ask, tools, full", p.Mode)}
	}
	return t.LoopAccess.Set(ctx, t.ActorFor(ctx, sess), p)
}
