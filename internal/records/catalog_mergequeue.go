package records

// the merge-queue family's five specs (S60 T9) — S60/slice split 2026-11-04 (SIZE1, see the pointer in Catalog()).
// The entries are byte-identical to what catalog.go held; only the
// container moved.

func mergequeueSpecs() []Spec {
	return []Spec{
		// S60 T9 (MQ14/MQ17, standing rule 2): the merge-queue family's
		// five — three reads, ONE narrowing write, and the ONE ask an
		// agent may make of the plane: that a BRANCH join the queue. An
		// agent sees items of the repositories it has actually run in
		// (own-view, the memory plane's rule — what you can see is what
		// your identity covers); it may take its OWN loop's still-waiting
		// or conflicted item out of the queue, because a removal can only
		// NARROW what the queue may do; and it may file an INERT
		// mergequeue_enqueue proposal a person commits through S45 — the
		// committed row lands UNATTENDED with the PROPOSAL as its stamp
		// (MQ14.3), never the agent as an owner. What is deliberately NOT
		// here is every way the queue could be made to move: no enqueue
		// tool, no reorder, no retry, no land, no settings, no dequeue of
		// anyone else's item (MQ17 — those six names are the person's
		// doors, and T8's guards refuse each by word). No schema here
		// carries `status`, `actor`, `attended`, `position`, `mode` or a
		// base: identity is the step's (OL3), attendedness is the kind's
		// (always false, MQ14.3), position is the queue's assignment, and
		// the base is the daemon's to resolve (MQ15).
		{Name: "mergequeue_list", Method: "mergequeue.list", Family: "mergequeue", Class: "safe",
			Description: "The merge queue as you may see it: items of the repositories YOUR loops have actually run in — branch, base, state, position, the reason it stalled, its PR number and integration refs, and three-state staleness. Items of repositories you have never worked in are not withheld loudly, they are simply not your view (S60 MQ16/own-view). There is deliberately no status filter, no actor and no position here: the state is what it is, and you never choose it.",
			Schema: obj(map[string]any{
				"componentRoot":   str("OPTIONAL: narrow the view to one repository — a root outside your own view answers the empty list, never the other project's rows"),
				"includeTerminal": map[string]any{"type": "boolean", "description": "true = include merged/failed/removed history too; by default the open items only (queued · catching_up · verifying · ready · merging · conflicted)"},
				"limit":           map[string]any{"type": "integer", "description": "at most this many items per repository lane (the plane caps it; 0 means the plane's own default)"},
			}, nil)},
		{Name: "mergequeue_show", Method: "mergequeue.show", Family: "mergequeue", Class: "safe",
			Description: "One queue item in full: its row, its append-only ledger read back (every state move with its actor and reason), its integration branch and PR, and the sentence of what happens next. An item of a repository you have never run in answers the SAME sentence as an id that never existed — this door never proves that somebody else has something queued (the plane's no-oracle, S52 LM5's law).",
			Schema: obj(map[string]any{
				"id": str("the item's id (an mq-… string, as mergequeue_list shows them) — unknown and out-of-view answer alike, by design"),
			}, []string{"id"})},
		{Name: "mergequeue_status", Method: "mergequeue.status", Family: "mergequeue", Class: "safe",
			Description: "One repository's queue health as one answer: the settings the daemon actually runs (the statement they widen by, if any), the lane heads and counts by state, the slot holder, and staleness THREE-STATE — ok, behind, or unverifiable when the remote could not be read (never a confident wrong answer). The repository must be one your loops have run in.",
			Schema: obj(map[string]any{
				"componentRoot": str("the repository to ask about, exactly as mergequeue_list spells its componentRoot — an outside root is refused by name, not answered empty"),
			}, []string{"componentRoot"})},
		{Name: "mergequeue_dequeue", Method: "mergequeue.dequeue", Family: "mergequeue", Class: "detectable", Mutates: true,
			Description: "Take YOUR OWN loop's item out of the queue — the one queue write an agent may ever perform, because leaving can only narrow what the queue may do (S60 MQ17). It must be the item YOUR running loop carries, and only while it is queued or conflicted: anything further along is being integrated, and stopping that takes a person at the door — the refusal says so and names what they can do. The row is REMOVED, not paused; rejoining is a proposal like anyone's ask.",
			Schema: obj(map[string]any{
				"id": str("the item's id (mq-…) — it must belong to the loop THIS call runs under (the item your blueprint enqueued you for); another loop's item is refused by name, never silently allowed"),
			}, []string{"id"})},
		{Name: "mergequeue_propose_enqueue", Method: "mergequeue.proposeEnqueue", Family: "mergequeue", Class: "detectable", Mutates: true,
			Description: "Ask a person to put ONE BRANCH into the merge queue (S60 MQ14.3) — the only ask an agent may make of the plane. The payload is the repository, the branch and a reason of 1 to 512 bytes; NO base is proposed (the daemon resolves it, MQ15), no attendedness (a committed ask lands UNATTENDED — asking never buys a watched landing), and no identity: the row is stamped with the PROPOSAL, never with you. Refused at once with the queue's own words if the branch does not exist, the base is unknown, or the branch is already queued. INERT: nothing moves until a person commits it, and per agent at most 20 open asks stand.",
			Schema: obj(map[string]any{
				"componentRoot": str("the repository — a component root, exactly as mergequeue_list spells them"),
				"branch":        str("the branch to queue — it must exist locally or on origin (the same validation the person's own enqueue runs, and it refuses with the same words)"),
				"reason":        str("why this branch belongs in the queue — required, 1 to 512 bytes: the sentence the person reads before they decide"),
			}, []string{"componentRoot", "branch", "reason"})},
	}
}
