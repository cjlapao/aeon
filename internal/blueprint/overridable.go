package blueprint

// overridable.go — what a person may still choose on a LOCKED (shipped)
// blueprint. A shipped blueprint's bytes never change; the fields named here
// are the only ones a person can set beside them, one value per lane. The
// table is the whole policy: a field joins by one row, and nothing else in
// the lock logic changes.

// FieldRoute is the provider/model route a lane's model steps run on. An
// absent override inherits the loop's own route.
const FieldRoute = "route"

// OverridableFields names the fields a person may override on this lane:
// FieldRoute when the lane runs at least one model step (its single step or
// any ladder rung), nothing otherwise — a code action or a gate has no model
// to choose.
func OverridableFields(l Lane) []string {
	if l.Step != nil && l.Step.Kind == ModelStep {
		return []string{FieldRoute}
	}
	for _, s := range l.Ladder {
		if s.Kind == ModelStep {
			return []string{FieldRoute}
		}
	}
	return nil
}

// IsOverridable reports whether field may be overridden on lane.
func IsOverridable(l Lane, field string) bool {
	for _, f := range OverridableFields(l) {
		if f == field {
			return true
		}
	}
	return false
}
