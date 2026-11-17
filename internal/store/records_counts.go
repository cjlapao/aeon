package store

import (
	"fmt"
)

// The counting doors of S38 T12 — what the RUNTIME asks when a lane
// declares `emits:` and the model says ok. These are reads through the
// reader pool (O6), on the columns identity stamped at write time: the
// contract counts rows whose source_step is this step, so the question is
// always "what did THIS step file", never "what exists somewhere".

// recordCountTables is the closed whitelist (the BumpTaskCounter argument:
// the whitelist is what keeps a caller's string from becoming a table
// name).
var recordCountTables = map[string]string{
	"goal":  "goal",
	"epic":  "epic",
	"brief": "brief",
	"task":  "task",
}

// CountRecordsByStep counts the records of one catalog entity filed by one
// step (its source_step). An unknown kind is refused naming the closed
// set — a typo'd entity must not read as "zero of those", which would
// reject a good step forever.
func (s *Store) CountRecordsByStep(kind string, stepID int64) (int, error) {
	table, ok := recordCountTables[kind]
	if !ok {
		return 0, fmt.Errorf("store: CountRecordsByStep speaks %v, not %q", []string{"goal", "epic", "brief", "task"}, kind)
	}
	var n int
	if err := s.r.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE source_step = ?`, stepID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count %s records of step %d: %w", kind, stepID, err)
	}
	return n, nil
}

// StepAttachments lists the documents the step pinned through attach (any
// subject — the contract asks "did it pin this document at all", and the
// subject half is answered by the row's own authorship).
func (s *Store) StepAttachments(stepID int64) ([]string, error) {
	rows, err := s.r.Query(`SELECT document FROM attachment WHERE step_id = ? ORDER BY document`, stepID)
	if err != nil {
		return nil, fmt.Errorf("store: attachments of step %d: %w", stepID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("store: attachment scan (step %d): %w", stepID, err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: attachments of step %d: %w", stepID, err)
	}
	return out, nil
}
