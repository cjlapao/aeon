package protocol

import (
	"context"
	"encoding/json"
	"fmt"
)

// The form half of the blueprint doors: the daemon turns a flow's text into the
// structure a form edits and back, and states every closed set a form offers.
// None of them judges the flow (blueprint.validate does) and none of them
// writes (blueprint.save does) — they are the translation the browser is not
// allowed to do itself.

// BlueprintModelParams carries the text to read.
type BlueprintModelParams struct {
	Content string `json:"content"`
}

// BlueprintRenderParams carries the structure to write as text. The model is
// the blueprint's own JSON shape and is decoded by the daemon, so a field it
// does not know is refused there, not guessed here.
type BlueprintRenderParams struct {
	Model json.RawMessage `json:"model"`
}

func (t *Table) handleBlueprintModel(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p BlueprintModelParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if t.BlueprintModel == nil {
		return nil, ErrStoreUnavailable
	}
	return t.BlueprintModel(p)
}

func (t *Table) handleBlueprintRender(_ context.Context, _ *Session, raw json.RawMessage) (any, error) {
	var p BlueprintRenderParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Model) == 0 {
		return nil, ErrBadParams{Err: fmt.Errorf("model is required")}
	}
	if t.BlueprintRender == nil {
		return nil, ErrStoreUnavailable
	}
	return t.BlueprintRender(p)
}

func (t *Table) handleBlueprintVocabulary(_ context.Context, _ *Session, _ json.RawMessage) (any, error) {
	if t.BlueprintVocabulary == nil {
		return nil, ErrStoreUnavailable
	}
	return t.BlueprintVocabulary(), nil
}
