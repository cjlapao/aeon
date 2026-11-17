package records

// the memory family's four specs (S52 T4) — S60/slice split 2026-11-04 (SIZE1, see the pointer in Catalog()).
// The entries are byte-identical to what catalog.go held; only the
// container moved.

func memorySpecs() []Spec {
	return []Spec{
		// — S52 T4 (LM5, LM7): the memory family's four. Two safe reads of
		// the agent's OWN readable set, the agent's own filing (the one
		// memory WRITE an agent may make, capped), and ONE ask — the INERT
		// learning_retract proposal (LM8: inert until a person commits it,
		// and the COMMITTER is the actor, never the proposer). What no
		// input schema here carries: scope/scopeKey for WHO (LM1 — the
		// readable set and the stamp come from the step's identity, never
		// typed; learning_add's scopeKey may confirm the stamp, never
		// replace it), actor/agent (OL3), provenance beyond observed and
		// assumed (LM4: told says "a person said it"; relayed is the board
		// hook's word — neither is nameable at an agent door by field OR
		// by service), and any retract/restore/edit field at all (LM7 —
		// those doors are the person's, T3's guards refuse each such name;
		// the only ask here is the ONE that can only take a memory back).
		{Name: "learning_query", Method: "learnings.mine.query", Family: "memory", Class: "safe",
			Description: "Your own memory, as a prompt would see it: the person's learnings, the projects you have actually run in, and your OWN agent scope — merged, tag-filterable, and with queryText ranking WITHIN that set (never widening it). Identical bytes to the learnings.forAgent answer. There is deliberately no scope or scopeKey here: what you can read is what your identity covers, and other agents' memory answers like it does not exist.",
			Schema: obj(map[string]any{
				"queryText": str("rank the merged readable set by embedding similarity to this text — it reorders within your scopes, never reaches past them; omit for scope order with no embedding cost"),
				"tags": arr(map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
					"pattern": str("the tag to match"),
					"prefix":  boolProp("true matches by prefix (LIKE pattern%), false or omit means exact equality — the two promises of M5"),
				}}, "AND-of-patterns: every listed pattern must be carried; empty omits the filter"),
				"limit": map[string]any{"type": "integer", "description": "at most this many hits (default 20, cap 100 — the vocabulary answers both)"},
			}, nil)},
		{Name: "learning_show", Method: "learnings.mine.show", Family: "memory", Class: "safe",
			Description: "One learning by id, when it is yours to see: the person's word, one of your projects, or your own scope. A row outside those scopes answers with the SAME sentence as a nonexistent id — this door never proves that somebody else remembers something (S52 LM5's no-oracle law). A retracted row is history, not memory: it answers the same way.",
			Schema: obj(map[string]any{
				"id": map[string]any{"type": "integer", "description": "the learning's row id, as the query and board answers show them — 0 or negative is refused, never guessed at"},
			}, []string{"id"})},
		{Name: "learning_add", Method: "learnings.mine.add", Family: "memory", Class: "detectable", Mutates: true,
			Description: "File ONE thing worth remembering into YOUR OWN memory: provenance is observed (you saw it) or assumed (you infer it) — told is a person's word and relayed the board's, neither nameable here (S52 LM4). scope agent files under YOUR slug, scope project under the root your step runs in; the key is STAMPED, and a typed key naming another agent or another root is refused, not honoured. Body at most 2 KiB, tags at most 8, and the door is capped per agent at MaxSelfLogAddsPerAgent (32 per 24h) counted off the audit ledger — a counted refusal that names its number. Nothing is learned until this answers; there is deliberately no told/relayed/retract/restore/edit tool.",
			Schema: obj(map[string]any{
				"scope":      enum("what owns this memory: agent (your own scope, keyed by your slug) or project (the root your step runs in) — person is a person's door, not a scope you can file into", "agent", "project"),
				"scopeKey":   str("OPTIONAL and stamp-checked only: your own slug for scope agent, your running root for scope project; omit to stamp yourself, or a key naming another agent or another root is refused (LM1)"),
				"body":       str("the thing worth remembering, in one paragraph — required, non-blank, at most 2 KiB; documents ride attachments, memory rides sentences"),
				"tags":       arr(str("one tag (non-empty; at most 8 tags per learning) — tags are how queries and the board find this again"), "at most 8 short tags; an AND of patterns later, not an essay index"),
				"provenance": enum("observed — you saw it (a failing command, a passing one after the fix); assumed — you inferred it. Not told (a person's word, the human door's) and not relayed (the board hook's): S52 LM4 refuses both here by schema AND by service", "observed", "assumed"),
			}, []string{"scope", "body", "provenance"})},
		{Name: "learning_propose_retract", Method: "learnings.mine.proposeRetract", Family: "memory", Class: "detectable", Mutates: true,
			Description: "Ask a person to TAKE BACK one learning — the only ask an agent may make about memory (LM7). Refused at once with the no-such-learning sentence if the id is not a live memory YOUR scopes can reach (out-of-scope is indistinguishable from nonexistent by design). INERT: nothing is retracted until a person commits the ask, and when they do, THEY are the actor — the stamp, the audit line and the vector drop all carry the committing person, never you (LM8). There is deliberately no retract or restore tool, and no learning_edit kind: the ask can only take a memory back, and restoring is a person's act.",
			Schema: obj(map[string]any{
				"id":     map[string]any{"type": "integer", "description": "the live learning's row id — must be a memory your readable scopes can actually see, or the ask is refused like a false id"},
				"reason": str("why it should come back — required, non-blank, at most 256 bytes: the sentence the person reads before they decide, and the exact words that will ride the retraction stamp if they commit"),
			}, []string{"id", "reason"})},
	}
}
