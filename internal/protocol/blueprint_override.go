package protocol

// blueprint_override.go — blueprint.override: the one write a LOCKED (shipped)
// blueprint takes. The blueprint's bytes never change; a person sets or clears
// one allowed field (today the `route`) on one lane, and the value is kept
// beside the blueprint, global to the daemon. A loop copies what it needs at
// start, so a change never reaches a loop already running.

import (
	"context"
	"encoding/json"
	"fmt"
)

// BlueprintOverrideParams is blueprint.override's request. An empty Value
// clears the override (the lane inherits again).
type BlueprintOverrideParams struct {
	Name  string `json:"name"`
	Lane  string `json:"lane"`
	Field string `json:"field"`
	Value string `json:"value"`
}

// BlueprintOverrideResult answers with what is true afterwards: the value the
// lane now carries ("" when cleared) and whether anything moved.
type BlueprintOverrideResult struct {
	Name    string `json:"name"`
	Lane    string `json:"lane"`
	Field   string `json:"field"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
}

// BlueprintOverrideService is the daemon's override service. Refusals come
// back as ErrBlueprintRefused with the closed-set kind and the sentence.
type BlueprintOverrideService interface {
	Override(ctx context.Context, actor Actor, p BlueprintOverrideParams) (BlueprintOverrideResult, error)
}

func (t *Table) handleBlueprintOverride(ctx context.Context, sess *Session, raw json.RawMessage) (any, error) {
	if t.BlueprintOverrides == nil {
		return nil, ErrStoreUnavailable.WithWhat("the blueprint override needs the daemon's override service, and this build did not link it")
	}
	if err := rejectBlueprintIdentityKeys(string(MethodBlueprintOverride), raw, false); err != nil {
		return nil, err
	}
	var p BlueprintOverrideParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" || p.Lane == "" || p.Field == "" {
		return nil, ErrBadParams{Err: fmt.Errorf("name, lane and field are required — an override is scoped to one lane of one blueprint")}
	}
	actor, err := t.blueprintActor(ctx, sess)
	if err != nil {
		return nil, err
	}
	return t.BlueprintOverrides.Override(ctx, actor, p)
}
