package protocol

// surface_table_mergequeue.go — S60 T8: the merge-queue plane's own
// classification, beside its family's names (the split-by-cohesion pattern
// the automation/environment files set). The four reads are reads: rows and
// derived tables, no write, no authority. The six writes are the PERSON'S
// doors (MQ13): each one carries HumanDoorOnly, which is the walk's cue
// that the method never enters narrow.Actions(), the agent services, the
// tool registry or the records catalog — the four caller-classes that are
// not the person. (T9 adds the plane's ONE proposal twin,
// mergequeue.proposeEnqueue — a write by nature, never a door; it joins
// this table beside its family when it registers.)

func surfaceTableMergeQueue() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodMergeQueueList: {
			Kind:    SurfaceRead,
			Summary: "The queue across the machine or one repository — open rows, terminal history when asked, staleness three-state (MQ16).",
		},
		MethodMergeQueueShow: {
			Kind:    SurfaceRead,
			Summary: "One item whole: the row, its append-only ledger read back, the integration refs and worktree path — and the digest the land binds to.",
		},
		MethodMergeQueueStatus: {
			Kind:    SurfaceRead,
			Summary: "One repository's queue view: its settings as rows answer them, counts by state, the lane heads, the open items.",
		},
		MethodMergeQueueVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The closed sets derived from the driver's tables: legal edges probed through mergequeue.Legal, reasons, modes, policies, methods (MQ17).",
		},

		// — the six human doors (MQ13). Writes with a person behind them;
		// none of them may be reached from the machine's four caller-classes.
		MethodMergeQueueEnqueue: {
			Kind:          SurfaceWrite,
			Summary:       "Appends a branch to its lane: one shared validation (MQ14), idempotent on the open branch, attended declared here (G14).",
			HumanDoorOnly: true,
		},
		MethodMergeQueueDequeue: {
			Kind:          SurfaceWrite,
			Summary:       "Takes an open item out of the queue — its live loops are put down FIRST, through the loop plane's own door (MQ5, behaviour 3).",
			HumanDoorOnly: true,
		},
		MethodMergeQueueReorder: {
			Kind:          SurfaceWrite,
			Summary:       "Moves one queued item directly before another (empty before-id means last); position is never typed, only moved (MQ16).",
			HumanDoorOnly: true,
		},
		MethodMergeQueueRetry: {
			Kind:          SurfaceWrite,
			Summary:       "Re-runs a failed item: failed → queued, the reason cleared, the driver advancing it again on the next round.",
			HumanDoorOnly: true,
		},
		MethodMergeQueueLand: {
			Kind:          SurfaceWrite,
			Summary:       "Lands a ready item now, digest-bound to the row as read; refused from other states or mode off; the driver makes the move.",
			HumanDoorOnly: true,
		},
		MethodMergeQueueSettings: {
			Kind:          SurfaceWrite,
			Summary:       "Writes one repository's queue settings; widening (auto, resolve) applies only on the acknowledged digest of the daemon's statement.",
			HumanDoorOnly: true,
		},

		// — T9's ONE proposal twin (MQ14.3): a write by nature — it files
		// an inert row — and deliberately NOT a human door: it stands on
		// the agent's surface beside the reads, and what it writes
		// constrains nothing until a person commits it (S35 TD2). The
		// six doors above stay the person's alone; no other mergequeue
		// name reaches the agent's caller-classes (MQ17, T8's guard,
		// T9's allowlists).
		MethodMergeQueueProposeEnqueue: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT mergequeue_enqueue proposal — nothing moves until a person commits it (MQ14.3, TD2).",
		},
	}
}
