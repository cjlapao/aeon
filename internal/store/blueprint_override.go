package store

import (
	"context"
	"fmt"
)

// blueprint_override.go — what a person may still choose on a LOCKED
// (shipped) blueprint. The blueprint's bytes never move; an override is a
// row beside them keyed by blueprint, lane id and field. Global to the
// daemon. A loop copies the rows it needs at start (loop.StartWith), so a
// later change never touches a loop in flight.

// BlueprintOverride is one override row.
type BlueprintOverride struct {
	Blueprint string `json:"blueprint"`
	LaneID    string `json:"laneId"`
	Field     string `json:"field"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// BlueprintOverrides lists every override of one blueprint, lane then field.
func (s *Store) BlueprintOverrides(ctx context.Context, blueprint string) ([]BlueprintOverride, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT blueprint, lane_id, field, value, updated_at, updated_by
		   FROM blueprint_override WHERE blueprint = ? ORDER BY lane_id, field`, blueprint)
	if err != nil {
		return nil, fmt.Errorf("store: reading overrides of %q: %w", blueprint, err)
	}
	defer rows.Close()
	var out []BlueprintOverride
	for rows.Next() {
		var o BlueprintOverride
		if err := rows.Scan(&o.Blueprint, &o.LaneID, &o.Field, &o.Value, &o.UpdatedAt, &o.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// BlueprintOverrideSet writes (or replaces) one override.
func (s *Store) BlueprintOverrideSet(ctx context.Context, o BlueprintOverride) error {
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO blueprint_override(blueprint, lane_id, field, value, updated_at, updated_by)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(blueprint, lane_id, field) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at, updated_by=excluded.updated_by`,
		o.Blueprint, o.LaneID, o.Field, o.Value, s.Now(), o.UpdatedBy)
	if err != nil {
		return fmt.Errorf("store: writing override of %q/%q: %w", o.Blueprint, o.LaneID, err)
	}
	return nil
}

// BlueprintOverrideClear removes one override; absent is not an error.
func (s *Store) BlueprintOverrideClear(ctx context.Context, blueprint, lane, field string) (bool, error) {
	res, err := s.w.ExecContext(ctx,
		`DELETE FROM blueprint_override WHERE blueprint = ? AND lane_id = ? AND field = ?`, blueprint, lane, field)
	if err != nil {
		return false, fmt.Errorf("store: clearing override of %q/%q: %w", blueprint, lane, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
