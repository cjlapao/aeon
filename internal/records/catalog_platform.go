package records

// catalog_platform.go — the platform family's ONE spec (S54 T4, SP4): the
// agent's read of what the derived stores know. What is deliberately NOT
// here is every way the panel's operations could be reached: no reindex
// tool, no rebuild, no graph build/reparse/delete tool, no proposal kind
// twinned to any of them (SP3 — an operation spends a person's money and
// may discard a person's derived data; SP4/T3's guards refuse each such
// name by word, dotted and underscored).

func platformSpecs() []Spec {
	return []Spec{
		// S54 T4 (SP4, T0's visibility ruling): freshness of the STEP'S OWN
		// root, nothing else. The answer is composed over the SAME seams
		// the person's platform.status composes over — one implementation,
		// and the scope is the step's root because the step's root is what
		// its `search` and `graph` tools can act on anyway. Corpus names
		// are inside the answer (they are not secret — the search tool
		// prints them in its own refusals); the operator-view fields the
		// seam carries — another root, embedRoute, embedModel, embedDim —
		// are NOT rendered here, because the model-facing tools have never
		// rendered them (tools/search.go's line). An unwired subsystem
		// reads `unwired`, never an omitted key and never an empty list.
		{Name: "index_status", Method: "platform.status", Family: "platform", Class: "safe",
			Description: "How fresh the derived stores are for the repository THIS loop runs in: per corpus the file count, the pending-embed count and whether a reindex is flying, and beside it the code graph's state for the same root (built, files, nodes, edges) — every figure quoted from the subsystem that owns it, never recomputed. `unwired` means the build has no such subsystem; `stale` means a pass would change rows; `building` means a person's pass is already running. There is deliberately NO way to start, cancel, or price a pass from here — reindex and graph operations are a person's door (S54 SP3), and no tool or ask reaches them.",
			Schema:      obj(map[string]any{}, nil)},
	}
}
