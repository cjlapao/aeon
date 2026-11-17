package daemon

import (
	"context"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// lockedView answers what a shipped (locked) blueprint still lets a person
// choose: which fields each lane allows, and what has been set. A user
// blueprint is not locked and answers nothing.
func (r *blueprintReads) lockedView(row store.BlueprintRow) (bool, []protocol.BlueprintLaneOverridable, []protocol.BlueprintOverrideView, error) {
	if row.Origin != "system" {
		return false, nil, nil, nil
	}
	bp, err := blueprint.Load([]byte(row.Content))
	if err != nil {
		return true, nil, nil, nil
	}
	var lanes []protocol.BlueprintLaneOverridable
	for _, l := range bp.Lanes {
		if f := blueprint.OverridableFields(l); len(f) > 0 {
			lanes = append(lanes, protocol.BlueprintLaneOverridable{Lane: l.ID, Fields: f})
		}
	}
	rows, err := r.store.BlueprintOverrides(context.Background(), row.Name)
	if err != nil {
		return true, nil, nil, err
	}
	var set []protocol.BlueprintOverrideView
	for _, o := range rows {
		set = append(set, protocol.BlueprintOverrideView{Lane: o.LaneID, Field: o.Field, Value: o.Value})
	}
	return true, lanes, set, nil
}
