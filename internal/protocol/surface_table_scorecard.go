package protocol

// surface_table_scorecard.go — S63 T5: the outcome-learning plane's own
// classification, beside its family's names (the split-by-cohesion
// pattern the merge-queue file set).
//
// This is RT3.3 on the wire: every `scorecard.*`, `digest.*` and
// `retro.*` method is `read`, and a test fails if any is registered
// with `Mutates` (TestScorecardMethodsAreReadsOnly). The plane's ONE
// write — retro.backfill — is a person's door (RT13): HumanDoorOnly is
// the walk's cue that the name never enters narrow.Actions(), the agent
// services, the tool registry or the records catalog, pinned by
// cmd/aeon/retro_guard_test.go.

func surfaceTableScorecard() map[MethodName]Surface {
	return map[MethodName]Surface{
		MethodScorecardAgent: {
			Kind:    SurfaceRead,
			Summary: "One agent's card, keyed by slug: n and state on every metric, the name-not-slug rows labelled, RT3's sentence on top (RT7, RT10).",
		},
		MethodScorecardBlueprint: {
			Kind:    SurfaceRead,
			Summary: "One blueprint's card, completion split by version so an upgrade never rewrites history (RT7).",
		},
		MethodScorecardComponent: {
			Kind:    SurfaceRead,
			Summary: "One component's card over its root (children on request): coverage strip, count-only breakdown — integers, nothing to rank (RT8/RT11).",
		},
		MethodScorecardCompare: {
			Kind:    SurfaceRead,
			Summary: "Two cards over two windows — the two numbers a person reads. No delta is computed or carried (RT8).",
		},
		MethodScorecardVocabulary: {
			Kind:    SurfaceRead,
			Summary: "The published sets: metrics with descriptions, states, minSample, window presets, the milestone ladder (RT7).",
		},

		MethodDigestLearned: {
			Kind:    SurfaceRead,
			Summary: "The what-did-we-learn digest in RT12's section order — ids and enums, learnings quoted verbatim, explicit nothing-needs-you (S58).",
		},

		MethodRetroList: {
			Kind:    SurfaceRead,
			Summary: "Outcome fact rows, paged id DESC with a cursor, each carrying its rendered describe sentence — ids and enums, never free text (RT10).",
		},
		MethodRetroShow: {
			Kind:    SurfaceRead,
			Summary: "One fact row whole: its per-agent rows and the one learning link its milestone wrote, body quoted verbatim.",
		},
		MethodRetroStatus: {
			Kind:    SurfaceRead,
			Summary: "Recorder health in the daemon's counters: recording start, dropped count, last write, reconcile state, model-retrospective state (RT6).",
		},

		// — the plane's ONE write (RT13). A person's door: dry-run
		// default, yes:true to write, the actor the connection's.
		MethodRetroBackfill: {
			Kind:          SurfaceWrite,
			Summary:       "Backfill fact rows for pre-recorder loops — history only, source=backfill, complete=0; preview default, writes only with yes:true (RT13).",
			HumanDoorOnly: true,
		},
	}
}
