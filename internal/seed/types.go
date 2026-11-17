package seed

// SetStatus is the per-set status (SD10): the class, the available and
// applied versions, and the per-record state. It is what seed.status and
// the Settings panel render.
type SetStatus struct {
	ID               string
	Class            Class
	AvailableVersion int
	AppliedVersion   int
	Records          []RecordStatus
}

// RecordStatus is one record's status: its state and versions and the
// daemon's reason (shown verbatim).
type RecordStatus struct {
	Key              string
	State            State
	AvailableVersion int
	AppliedVersion   int
	Reason           string
}

// SetPlan is a set's plan (SD10): the decisions apply would make and any
// refusal (a downgrade). It is read-only.
type SetPlan struct {
	SetID     string
	Decisions []Decision
	Refusal   Refusal
}

// ApplyResult is what an apply (or restore) did, per record.
type ApplyResult struct {
	SetID   string
	Records []RecordResult
}

// RecordResult is one record's apply result: the op, versions and reason.
type RecordResult struct {
	Key         string
	Op          Op
	FromVersion int
	ToVersion   int
	Reason      string
}

// result maps a decision to its record result.
func (d Decision) result() RecordResult {
	return RecordResult{
		Key:         d.Key,
		Op:          d.Op,
		FromVersion: d.FromVersion,
		ToVersion:   d.ToVersion,
		Reason:      d.Reason,
	}
}
