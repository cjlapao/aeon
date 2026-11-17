package daemon

// blueprint_override.go — the daemon half of blueprint.override. A shipped
// blueprint is locked; this is the one door that still changes how it runs:
// a person picks the route (provider and model) a lane's model steps use.
// Every refusal is named before a row moves, and a value that is not a
// configured route is refused rather than stored — a lane that cannot run is
// found now, not three steps into a loop.

import (
	"context"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprintOverrides is the override service: the store, and the reader of
// the routes a value may name.
type blueprintOverrides struct {
	store  *store.Store
	routes func() []string
}

func (o *blueprintOverrides) Override(ctx context.Context, actor protocol.Actor, p protocol.BlueprintOverrideParams) (protocol.BlueprintOverrideResult, error) {
	var zero protocol.BlueprintOverrideResult
	row, found, err := o.store.BlueprintGet(ctx, p.Name)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, blueprintRefused("from-unknown", "override of %q: no such blueprint", p.Name)
	}
	if row.Origin != "system" {
		return zero, blueprintRefused("not-locked", "%q is your own blueprint — edit it directly; overrides are for shipped blueprints", p.Name)
	}
	bp, err := blueprint.Load([]byte(row.Content))
	if err != nil {
		return zero, blueprintRefused("diagnostics", "%q does not load: %v", p.Name, err)
	}
	var lane *blueprint.Lane
	for i := range bp.Lanes {
		if bp.Lanes[i].ID == p.Lane {
			lane = &bp.Lanes[i]
		}
	}
	if lane == nil {
		return zero, blueprintRefused("lane-unknown", "%q has no lane %q", p.Name, p.Lane)
	}
	if !blueprint.IsOverridable(*lane, p.Field) {
		return zero, blueprintRefused("not-overridable", "lane %q of %q does not allow overriding %q — only fields the daemon declares overridable can change", p.Lane, p.Name, p.Field)
	}
	res := protocol.BlueprintOverrideResult{Name: p.Name, Lane: p.Lane, Field: p.Field, Value: p.Value}
	if p.Value == "" {
		res.Changed, err = o.store.BlueprintOverrideClear(ctx, p.Name, p.Lane, p.Field)
		return res, err
	}
	if p.Field == blueprint.FieldRoute {
		known := o.routes()
		ok := false
		for _, id := range known {
			if id == p.Value {
				ok = true
			}
		}
		if !ok {
			return zero, blueprintRefused("route-unknown", "route %q is not configured — the routes that are: %v", p.Value, known)
		}
	}
	existing, err := o.store.BlueprintOverrides(ctx, p.Name)
	if err != nil {
		return zero, err
	}
	res.Changed = true
	for _, e := range existing {
		if e.LaneID == p.Lane && e.Field == p.Field && e.Value == p.Value {
			res.Changed = false
		}
	}
	if !res.Changed {
		return res, nil
	}
	err = o.store.BlueprintOverrideSet(ctx, store.BlueprintOverride{Blueprint: p.Name, LaneID: p.Lane, Field: p.Field, Value: p.Value, UpdatedBy: actor.Kind + ":" + actor.ID})
	return res, err
}
