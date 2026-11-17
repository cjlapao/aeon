package loop

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// The requires evaluator (S38 T13, OL9) — pure, so the semantics are testable
// without a socket, a daemon, or a clock. A blueprint's `requires:` is an AND
// of ORs: every Requirement (one clause) must be satisfied by at least one of
// its any_of terms, against the goal and/or task the start was given.

// Candidate is one thing the person COULD start on — the refusal of an
// unmet requires names these instead of leaving a dead end (S39's dialogue
// reads them; the bound keeps a refusal a pointer, not a dump).
type Candidate struct {
	Kind  string `json:"kind"`  // goal | task
	ID    string `json:"id"`    // the id to pass back as goalId/taskId
	Title string `json:"title"` // what it is, in the record's own words
	State string `json:"state"` // its approval state, as stored
}

// RequiresResult is the evaluator's whole answer.
type RequiresResult struct {
	OK         bool
	Sentence   string // the refusal, rendered once — callers carry it verbatim
	Candidates []Candidate
	Goal       *store.Goal
	Task       *store.Task
}

// candidateBound is the offer's ceiling, per the spec: the refusal names
// enough to pick from, never a page.
const candidateBound = 20

// CheckRequires evaluates bp.Requires against the given goal/task. offer()
// is called ONLY when something is unmet (the happy path stays free of
// store reads — and the happy path is the common one).
func CheckRequires(bp *blueprint.Blueprint, goal *store.Goal, task *store.Task, offer func() []Candidate) RequiresResult {
	res := RequiresResult{Goal: goal, Task: task}
	if bp == nil || len(bp.Requires) == 0 {
		res.OK = true
		return res
	}
	var unmet []string
	for _, req := range bp.Requires {
		if len(req.AnyOf) == 0 {
			unmet = append(unmet, "an empty any_of (a clause nobody can satisfy)")
			continue
		}
		satisfied := false
		terms := make([]string, 0, len(req.AnyOf))
		for _, term := range req.AnyOf {
			terms = append(terms, term.Type+" "+term.State)
			if satisfies(term, goal, task) {
				satisfied = true
			}
		}
		if !satisfied {
			unmet = append(unmet, strings.Join(terms, " or "))
		}
	}
	if len(unmet) == 0 {
		res.OK = true
		return res
	}
	res.Sentence = fmt.Sprintf("blueprint %q needs %s — none was given", bp.ID, strings.Join(unmet, "; "))
	if offer != nil {
		cands := offer()
		if len(cands) > candidateBound {
			cands = cands[:candidateBound]
		}
		res.Candidates = cands
	}
	return res
}

// satisfies is the WHOLE vocabulary: a goal satisfies {goal, approved} iff
// its review state is the person's approval; a task satisfies {task,
// approved} iff it is a MANUAL task the person approved (a loop task's
// approval is its loop's business, not a prerequisite a start can lean on —
// OL9 reads the PERSON's verdict).
func satisfies(term blueprint.RequirementTerm, goal *store.Goal, task *store.Task) bool {
	switch term.Type {
	case "goal":
		return term.State == "approved" && goal != nil && goal.ReviewState == "approved"
	case "task":
		return term.State == "approved" && task != nil && task.Manual && task.ApprovalState == "approved"
	}
	return false
}
