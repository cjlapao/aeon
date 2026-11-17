package records

// catalog_scheduler.go — the scheduler family's four SPECS (S27 T8, CN23):
// the agent's read of what the continuous scheduler is doing. Four reads and
// nothing else — no enable, no disable, no pause, no root.set, no stopAll
// (S27 CN2: those eight controls are human doors on the 0600 surface,
// cmd/aeon/continuous_guard_test.go's TestSchedulerWritesNeverReachOtherDoors
// refuses every dotted and underscored spelling by name). CN21's line holds
// here: these specs let a step SEE the plan and the ledger; they let it
// change nothing, and the arm files answer through the SAME
// ContinuousService the person's four read doors answer through — one
// implementation, two doors (S38 OL10's shape).
//
// The scope word is the step's own repository: Identify resolves the step →
// loop → component → root, and every row an arm returns is filtered to that
// one component root (MC2's law — a foreign repository is absent, never
// refused with a revealing sentence). A person or CLI session reads every
// repository through the
// socket doors directly; there is no tool that can.

func schedulerSpecs() []Spec {
	return []Spec{
		// T8's scoping ruling: the schemas carry NO repository field at all.
		// The step's root is stamped by the arm from the store (ident), so a
		// `componentRoot` a model could type would only ever be overwritten —
		// a field that cannot be honoured is a field that lies. The answer
		// speaks the daemon's own words (CN21: the kinds, decisions and
		// reasons render verbatim, never re-labelled).
		{Name: "scheduler_status", Method: "scheduler.status", Family: "scheduler", Class: "safe",
			Description: "How the continuous scheduler is running for the repository THIS loop lives in: the master switch and pause state, the agent-slot budget with what is spent now, the next tick, and this repository's own rows — enabled, paused, running counts and what each is waiting for — quoted from the daemon's own view, never recomputed. Needs-you items appear as a count; their kinds are what asks for a person, shown exactly as the scheduler wrote them. A read: it changes nothing, and no control over the scheduler is reachable from here (S27 CN2 keeps enable, disable, pause and stopAll behind the human door).",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "scheduler_preview", Method: "scheduler.preview", Family: "scheduler", Class: "safe",
			Description: "The plan for the repository THIS loop lives in, one decision at a time, in the order the scheduler itself would spend its slots: each candidate with its kind (queue or roadmap), what it would start, the verdict word the scheduler would write (start or a named defer reason like capacity, dependency, or backstop) and the waiting it is under — every word quoted from the same plan the next tick will execute, so a step can see whether it is next and why not, with nothing started or changed by the asking.",
			Schema:      obj(map[string]any{}, nil)},
		{Name: "scheduler_decisions", Method: "scheduler.decisions", Family: "scheduler", Class: "safe",
			Description: "The scheduler's own ledger for the repository THIS loop lives in: one row per decision the scheduler has actually made — the verdict word it wrote, the subject it decided about (queue/<id>, roadmap/<id>, root or daemon), the reason word, and when. Newest first, capped by limit. What the ledger does not hold is absent, not empty: a loop that was never considered has no row, and reading this ledger starts, defers, or stops nothing.",
			Schema: obj(map[string]any{
				"subject":  str(`decide about one subject, spelled "kind/id" — kind one of queue, roadmap, root, daemon (the ledger's own vocabulary)`),
				"decision": str("verdict word to keep: start or defer (the ledger's two words, matched exactly)"),
				"reason":   str("reason word to keep, verbatim as the ledger wrote it (capacity, dependency, backstop, ...)"),
				"since":    str("RFC3339 time floor — rows older than it are left out; blank reads the whole ledger window the door allows"),
				"limit":    num("at most this many rows, newest first (the door caps it at 500; absent means the door's default)"),
			}, nil)},
		{Name: "scheduler_attention", Method: "scheduler.attention", Family: "scheduler", Class: "safe",
			Description: "What in the repository THIS loop lives in is waiting for a person right now: one entry per open needs-you decision, each with its kind shown exactly as the scheduler wrote it (cap-hit, review, conflicted, and the rest verbatim — CN21's no-interpretation rule), the subject it blocks, and when it opened. An empty list is an honest nothing-needs-you for this repository, not a silence; answering what is listed is a person's act at the human door, never a step's.",
			Schema:      obj(map[string]any{}, nil)},
	}
}
