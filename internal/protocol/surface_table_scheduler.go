package protocol

// surface_table_scheduler.go — S27 T7: the continuous-scheduler plane's own
// classification, beside its family's names (the split-by-cohesion pattern
// the merge-queue and outcome-learning files set). The four reads answer
// the board: state, the tick RUN preview, the audit ledger, needs-you. The
// eight controls are the PERSON'S doors (CN2, CN18): each carries
// HumanDoorOnly, which is the walk's cue that the name never enters
// narrow.Actions(), the agent services, the tool registry or the records
// catalog — the four caller-classes that are not the person
// (cmd/aeon/continuous_guard_test.go). The scheduler's rows are authority
// records: no agent path registers, enables, pauses or stops through them;
// the plane ships no propose twin — an agent may never even ask on paper
// (CN20's tools are reads only, and they arrive beside their own twin
// when their task registers anything new).

func surfaceTableScheduler() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodSchedulerStatus: {
			Kind:    SurfaceRead,
			Summary: "Master and pause, settings with sources, slots, repository rows with their live state word, the descriptor (CN18/CN22).",
		},
		MethodSchedulerPreview: {
			Kind:    SurfaceRead,
			Summary: "The tick RUN without the tick: every line it would write with its reason, the would-start subset — a read that writes nothing.",
		},
		MethodSchedulerDecisions: {
			Kind:    SurfaceRead,
			Summary: "The decision ledger newest-first, filtered by root, subject, word, reason and since; bounded at the door (1..500).",
		},
		MethodSchedulerAttention: {
			Kind:    SurfaceRead,
			Summary: "Needs-you now: every needs-you decision whose loop is still blocked or waiting, derived at read from the loop row (CN13).",
		},

		// — the eight human doors (CN2, CN18). Each one moves authority;
		// the actor is the connection's peer, never a param; none may be
		// reached from the machine's four caller-classes.
		MethodSchedulerEnable: {
			Kind:          SurfaceWrite,
			Summary:       "The master switch on, alone: it touches no repository row — one authority per act (CN2).",
			HumanDoorOnly: true,
		},
		MethodSchedulerDisable: {
			Kind:          SurfaceWrite,
			Summary:       "The master switch off — not an uninstall: every repository row keeps its own setting untouched.",
			HumanDoorOnly: true,
		},
		MethodSchedulerPause: {
			Kind:          SurfaceWrite,
			Summary:       "The operational off with a required reason: stops PICKING, never running work; needs the master on (CN18).",
			HumanDoorOnly: true,
		},
		MethodSchedulerResume: {
			Kind:          SurfaceWrite,
			Summary:       "Lifts the pause; the master stays where the person left it.",
			HumanDoorOnly: true,
		},
		MethodSchedulerRootSet: {
			Kind:          SurfaceWrite,
			Summary:       "Authors one repository template, CN5-validated at the door; an invalid template is a typed refusal, never a stored row.",
			HumanDoorOnly: true,
		},
		MethodSchedulerRootDisable: {
			Kind:          SurfaceWrite,
			Summary:       "One repository's own switch off; a root with no row is refused as never-enabled (CN2).",
			HumanDoorOnly: true,
		},
		MethodSchedulerEnableTree: {
			Kind:          SurfaceWrite,
			Summary:       "CN2's bulk expansion: template validated ONCE, rows for repositories that exist NOW; a later child keeps its own authority (CN6).",
			HumanDoorOnly: true,
		},
		MethodSchedulerStopAll: {
			Kind:          SurfaceWrite,
			Summary:       "The composed off: pause FIRST, then every scheduler-started loop through S55's loop.stop door; the pause stands (CN18).",
			HumanDoorOnly: true,
		},
	}
}
