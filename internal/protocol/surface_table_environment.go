package protocol

// surface_table_environment.go — S59 T7 (EV16, EV17; the classification T6's
// plane was left without): the surface table's twenty-one execution-
// environment entries, split from the parts by cohesion exactly as the gate
// plane's are (the backup family's precedent).
//
// S49 T4, working in parallel on `origin/main`, found the same missing
// classification (its own completeness proof named it on a fully wired
// fixture) and fix-forwarded twenty rows here independently; the merge kept
// THIS file because it is the superset — every one of S49's twenty rows is
// classified the same way here (Kind and HumanDoorOnly compared row by row at
// the merge) and this one carries the plane's twenty-first, the propose twin
// below. The two files agreeing row for row is the classification's own
// cross-check.
//
// Nine READS are plain reads: the environments and what each caps, one row in
// full, what a command would REALLY run in (names, sources of names, reference
// names — never a value, EV7), the closed sets the forms render from, the
// plane's own audit, the provisioning ledger, and the three reference reads. A
// read about the environment is a read: four of these are also an agent's
// catalog tools (S59 T7), which is what a READ can be.
//
// ELEVEN WRITES are HumanDoorOnly (EV16): each stamps the actor from the
// connection's person and moves something real — an environment row is created
// or re-specified, archived or restored, a component is bound or released, a
// provision is removed, a reference's pointer is filed or moved. They are
// named here by CONSTANT (the LW8 discipline) and join
// surfaceHumanDoorFamilies in surface.go, so the family — the "what is true
// must be marked" direction — catches a table edit that forgets a row.
//
// The plane's ONE agent door is the twentieth-plus-one: environment.proposeRestrict,
// SurfaceWrite WITHOUT HumanDoorOnly, exempted by its single entry in
// surfaceProposeOnlyTwins. It files an inert proposal that can only TIGHTEN
// what a loop runs with, and it constrains nothing until a person commits it —
// the same shape as the gate plane's three twins and the blueprint revision,
// and for the same reason: an agent may ask for less of its own freedom.

func surfaceTableEnvironment() map[MethodName]Surface {
	return map[MethodName]Surface{
		// — the nine reads: what exists, what it means, what would run —
		MethodEnvironmentList: {
			Kind:    SurfaceRead,
			Summary: "Lists the execution environments: backend, what each caps, where it is bound, system or user origin, and its last change.",
		},
		MethodEnvironmentShow: {
			Kind:    SurfaceRead,
			Summary: "One environment in full: the spec as variable names and literals, capability chips, bindings, and its revision history.",
		},
		MethodEnvironmentEffective: {
			Kind:    SurfaceRead,
			Summary: "What a command on a component would really run in — backend, variable names with where each came from, reference names. Never values.",
		},
		MethodEnvironmentVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The plane's closed sets: backends with their strengths, homes, service states, and the provision idle TTL in its three states.",
		},
		MethodEnvironmentChanges: {
			Kind:    SurfaceRead,
			Summary: "The environment audit — who changed which row, when, and from which revision: the changing writes' own trail.",
		},
		MethodEnvironmentProvisions: {
			Kind:    SurfaceRead,
			Summary: "The provisioning ledger for sandboxed rows: state, kind, owner, age — the walk environment.teardown refuses to skip.",
		},
		MethodSecretRefList: {
			Kind:    SurfaceRead,
			Summary: "Lists the secret references: name, the variable each would inject as, and the kind of source. Never a value (EV7).",
		},
		MethodSecretRefShow: {
			Kind:    SurfaceRead,
			Summary: "One reference in full, with the environments that still inject it — still no value, and no length of one.",
		},
		MethodSecretRefCheck: {
			Kind:    SurfaceRead,
			Summary: "Two booleans and the resolver's refusal: does the source resolve, would the bind succeed. The value is read to decide and dropped.",
		},

		// — the eleven writes: the person's doors (EV16, EV7) —
		MethodEnvironmentCreate: {
			Kind:          SurfaceWrite,
			Summary:       "Creates an environment — a backend and a whole spec, validated by execenv, audited. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Replaces an environment's spec at a new revision, under the validator and a compare-and-set. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Archives an environment so no new binding may choose it, naming what is bound today first. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentRestore: {
			Kind:          SurfaceWrite,
			Summary:       "Brings an archived environment back so it may be bound again. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentBind: {
			Kind:          SurfaceWrite,
			Summary:       "Binds a component to an environment; dryRun answers in the applied write's own words. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentUnbind: {
			Kind:          SurfaceWrite,
			Summary:       "Releases a component's binding, returning it to the plane's default. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodEnvironmentTeardown: {
			Kind:          SurfaceWrite,
			Summary:       "Walks the provisioning ledger and removes a provision, refusing to mark an unknown row removed. The person's door (EV16).",
			HumanDoorOnly: true,
		},
		MethodSecretRefCreate: {
			Kind:          SurfaceWrite,
			Summary:       "Files a secret reference — a pointer (kind, source, injectAs), never a value. The person's door (EV7).",
			HumanDoorOnly: true,
		},
		MethodSecretRefUpdate: {
			Kind:          SurfaceWrite,
			Summary:       "Moves a reference's pointer; an empty source means keep the existing one. The person's door (EV7).",
			HumanDoorOnly: true,
		},
		MethodSecretRefArchive: {
			Kind:          SurfaceWrite,
			Summary:       "Archives a reference, naming the environments that still inject it before any of them is stranded. The person's door (EV7).",
			HumanDoorOnly: true,
		},
		MethodSecretRefRestore: {
			Kind:          SurfaceWrite,
			Summary:       "Brings an archived reference back so a spec may name it again. The person's door (EV7).",
			HumanDoorOnly: true,
		},

		// — the one agent door (S59 T7): a write by nature, never a door —
		MethodEnvironmentProposeRestrict: {
			Kind:    SurfaceWrite,
			Summary: "Files an INERT proposal to TIGHTEN an environment's spec; never a widening, never a backend change, nothing until a person commits.",
		},
	}
}
