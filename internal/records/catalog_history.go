package records

// the history family's two specs (S53 T3) — S60/slice split 2026-11-04 (SIZE1, see the pointer in Catalog()).
// The entries are byte-identical to what catalog.go held; only the
// container moved.

func historySpecs() []Spec {
	return []Spec{
		// S53 T3 (HV4): the history family's two — reads only, and exactly
		// two. History (S25) is a derived, read-only merge over live and
		// archived loops; both tools answer through protocol.AnswerHistory*,
		// the ONE rendering the person's history.list/history.show doors run
		// (rule 4), over the same read-only facade the socket seam holds.
		// What is deliberately absent (HV4/HV5): no history_export twin —
		// the NDJSON dump is a person's door, and T2's guard refuses such a
		// tool by name; no coverage or vocabulary twin either — those two
		// S53 doors are the app's census, not a step's question. Schemas
		// carry filter words only: projectRoot/groupBy/since are the
		// history query's own scope words, and there is no actor, no loop
		// slug smuggled as identity — the step's identity arrives stamped
		// (OL3) and the reads use none of it.
		{Name: "history_list", Method: "history.list", Family: "history", Class: "safe",
			Description: "The project's history as the person's own list renders it: live loops and archived ones merged into one time-ordered set, each row carrying its lane, its honest outcome word, its duration, its cost with the three-state honesty (a floor reads as a floor, never a bare number), and the links it rode — loop, task, goal, roadmap. An optional grouping regroups the live steps by phase, task or agent. A read: it files nothing and changes nothing.",
			Schema: obj(map[string]any{
				"projectRoot": str("narrow to one component root (omit for every component the history spans — the same scope word history.list takes)"),
				"groupBy":     enum("regroup the live steps (the four lanes of H1): loop is the default merge · phase · task · agent", "loop", "phase", "task", "agent"),
				"since":       str("lower time bound, RFC3339 (e.g. 2026-11-01T00:00:00Z) — entries older than it are left out; blank spans all time"),
			}, nil)},
		{Name: "history_show", Method: "history.show", Family: "history", Class: "safe",
			Description: "One history entry in full — the drill-down the person's history.show door answers: a live loop comes back with its message transcript, an archived loop with its cold-stored files (header first, content on demand, S25 Q3). An id asked of the wrong door and an id that does not exist are both refused BY NAME, never answered as an empty read; what the lane cannot say about an archive it says, never hides.",
			Schema: obj(map[string]any{
				"id":          str("the entry's id or slug, as history_list shows them — a loop id for a live entry, an archive id for a cold one"),
				"projectRoot": str("the scope to find it in (same word history_list takes; omit to search every component)"),
			}, []string{"id"})},
	}
}
