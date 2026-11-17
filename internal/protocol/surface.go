package protocol

// ════════════════════════════════════════════════════════════════════════════
// The surface classification (S47 T6, TC6/TC8): what the daemon exposes, and to
// whom.
//
// `surface.list` and `surface.get` answer this question for a person, so the
// answer has to be a CLOSED TABLE with a completeness test rather than a
// guess assembled per screen. Two halves, deliberately kept apart:
//
//   - the CLASSIFICATION — kind, summary, and whether a method is a human door —
//     is declared here, in the table files (`surface_table_*.go`), one entry per
//     method. It is a function-built map: there is no package-level map to
//     append to, so a method enters the surface only by an edit a test then
//     checks both ways (`TestEveryRegisteredMethodIsClassified`,
//     `TestEveryEntryNamesARegisteredMethod`).
//   - the DOORS — socket, public, narrow, agent service, tool twin — are
//     COMPUTED from the live structures in `surface_doors.go` and never typed
//     here. A declared door flag is a second truth about a structure that
//     already states it; `TestSurfaceDoorsEqualTheRealStructures` is the
//     differential that proves the computation instead.
//
// # The four kinds, and the rule each one is
//
// `read` returns state and changes nothing. `write` changes stored state.
// `control` acts on a running thing the daemon owns — a step, a loop, a worker,
// a terminal, a schedule, a dial. `diagnostic` is what `Debug` marks: it exists
// for diagnosis, and S19 keeps it off every projection.
//
// The kinds are cross-checked, not trusted: a records twin is a `write` exactly
// when its spec mutates, an agent service method is a `write` exactly when it is
// not read-only, a narrow trigger/webhook/callback action is `control`, and every
// `Debug` method is `diagnostic` — and the reverse of that last one too, because
// a diagnostic entry nobody marked Debug would reach the public surface.
//
// # `HumanDoorOnly` is a claim with a proof
//
// Declared here, proven against the structures: a method may only be marked if
// the narrow surface, the agent-service enumeration and the tool registry all
// fail to reach it (`TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor`, with the
// one exception being a PROPOSE-ONLY twin, named in
// `surfaceProposeOnlyTwins` and nowhere else). The name families the guard
// slices made human doors — S45's authoring, S35/S45's proposal decisions,
// S46's MCP writes, S43-extra's seed apply/restore, S31's steering and
// terminals, S55's loop-control and budget writes, S56's grants — are asserted
// TO BE marked, so a table cannot quietly forget one.
// ════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"sort"
)

// SurfaceKind is the closed classification set. Four values, no more: a fifth
// would mean a method that neither reports, stores, nor acts — and nobody has
// named what that would be.
type SurfaceKind string

// The closed kind set (TC6).
const (
	SurfaceRead       SurfaceKind = "read"
	SurfaceWrite      SurfaceKind = "write"
	SurfaceControl    SurfaceKind = "control"
	SurfaceDiagnostic SurfaceKind = "diagnostic"
)

// AllSurfaceKinds is the set in table order — the enumeration a client validates
// against, and the one a test walks to prove the table uses no other word.
func AllSurfaceKinds() []SurfaceKind {
	return []SurfaceKind{SurfaceRead, SurfaceWrite, SurfaceControl, SurfaceDiagnostic}
}

// Surface is one method's declared classification. The doors are NOT here: they
// are computed, and `SurfaceEntry` carries both halves side by side so a reader
// sees the claim and the fact together.
type Surface struct {
	// Kind is what the method does, from the closed set above.
	Kind SurfaceKind
	// Summary is a fixed sentence — what the method answers, in the daemon's own
	// words, ≤ 140 characters and without control characters. It is data for a
	// person, so `TestSurfaceSummariesAreSafeAndDistinct` refuses a row that is
	// empty, unreadable, or a duplicate of another row (two methods with one
	// sentence tell a reader nothing).
	Summary string
	// HumanDoorOnly says the only door that reaches this method is the person at
	// the endpoint: absent from the narrow projection, from the agent-service
	// enumeration and from the tool registry. Declared, and then PROVEN — a mark
	// whose proof fails is a test failure, not a label.
	HumanDoorOnly bool
}

// surfaceTableParts names the table slices, in the order a reader meets them.
// Every entry of every part lands in the one table; a name appearing twice is a
// programmer error and says so loudly at the first call rather than letting one
// family silently overwrite another.
func surfaceTableParts() []func() map[MethodName]Surface {
	return []func() map[MethodName]Surface{
		surfaceTableCore,
		surfaceTableAgents,
		surfaceTableGovernance,
		surfaceTableMCP,
		surfaceTableRecords,
		surfaceTableProjects,
		surfaceTableBackup,
		surfaceTableExport,
		surfaceTableGates,
		surfaceTableSupervision,
		// S59 T7 (2026-11-03): the execution-environment plane's own
		// classification — nine reads, eleven human doors, and the one
		// agent's propose twin. (T6 registered the twenty doors and left
		// them unclassified; the pre-existing red in
		// internal/daemon's TestSurfaceEveryRegisteredMethodIsClassified
		// is closed here, in the change that made the plane's agent
		// surface real.)
		surfaceTableEnvironment,
		surfaceTableAutomation,
		// S60 T8 (2026-11-04): the merge-queue plane's own classification —
		// four reads and the six human doors MQ13 names.
		surfaceTableMergeQueue,
		// S63 T5 (2026-11-04): the outcome-learning plane's own
		// classification — RT3.3 keeps every scorecard, digest and retro
		// door a read, and RT13 keeps retro.backfill a person's door.
		surfaceTableScorecard,
		// S61 T7 (2026-11-05): the goal-intake plane's own classification —
		// ten reads and the four human doors GI8/GI14/GI9 name.
		surfaceTableS61,
		// S27 T7 (2026-11-05): the continuous-scheduler plane's own
		// classification — four reads and the eight human doors CN2/CN18
		// name, scheduler.stopAll included (registered only over its
		// service, which exists over the store that is S55's home).
		surfaceTableScheduler,	}
}

// surfaceTable is the whole closed classification, built fresh on every call.
func surfaceTable() map[MethodName]Surface {
	table := map[MethodName]Surface{}
	for _, part := range surfaceTableParts() {
		for name, s := range part() {
			if _, dup := table[name]; dup {
				panic(fmt.Sprintf("protocol: the surface table classifies %q twice", name))
			}
			table[name] = s
		}
	}
	return table
}

// SurfaceMethodName is a method name in the table's own terms, so the door
// computation and the tests speak one vocabulary.
type SurfaceMethodName = MethodName

// surfaceProposeOnlyTwins names, and names ONLY, the catalog actions whose
// method is an agent's door to propose a narrowing. They are tool twins, and the
// human-door rule has exactly this one exception: a person's write door and the
// agent's inert proposal of the same change share a family, and the proposal
// constrains nothing until a person commits it (S45 TD2, S56 AL11).
//
// The set is closed and it is a function: a new entry is an edit that
// TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor then demands a justification
// for, not a line someone appends to a shared map.
func surfaceProposeOnlyTwins() map[MethodName]bool {
	return map[MethodName]bool{
		MethodPolicyProposeAdd:      true,
		MethodPolicyProposeRemove:   true,
		MethodAutonomyProposeRevoke: true,
		MethodAutonomyProposeDeny:   true,
		"mcp.mine.proposeRemove":    true,
		"mcp.mine.proposeRevoke":    true,
		// S48 T10 (2026-11-03): the blueprint design revision S35 TD1
		// names. The agent's door files an INERT blueprint_save row; the
		// five blueprint write doors stay the person's (BP9, T8's guard),
		// and this twin is the family's sole exception to the human-door
		// rule for exactly the reason the header states.
		MethodBlueprintPropose: true,
		// S57 T9 (VG19): the gate plane's three twins — the agent's door
		// to file the gate kinds' proposals and to mark its own loop's
		// ledger. Each files or marks only its own, and a proposal
		// constrains nothing until a person commits it.
		MethodVerifyProposeAdd:        true,
		MethodMergeRuleProposeTighten: true,
		MethodDoDMarkAsAgent:          true,
	}
}

// surfaceHumanDoorFamilies is the closed set of name families the guard slices
// settled as human doors. It is the OTHER direction of the proof: not "what is
// marked must be true" but "what is true must be marked", so forgetting a row is
// caught by the family rather than by the row.
func surfaceHumanDoorFamilies() []MethodName {
	// The authoring names are the CONSTANTS, not a second spelling: S45's PM1
	// guard keeps every dotted authoring verb written exactly once in this
	// package, and a table that retyped them would be a second registration
	// waiting to happen.
	return []MethodName{
		MethodPolicyAdd, MethodPolicyRemove, MethodPolicySeedCorpus,
		MethodProposalCommit, MethodProposalReject,
		"mcp.server.add", "mcp.server.update", "mcp.server.remove", "mcp.server.setEnabled", "mcp.server.share",
		"mcp.grant.declare", "mcp.grant.revoke", "mcp.core.set", "mcp.importFile",
		"seed.apply", "seed.restore",
		// S62 T6: the backup's three write doors are the person's — run
		// publishes a file, verify runs the drill, prune deletes through the
		// fence; the two reads are not doors at all. S62 T8: export.run is
		// the export family's one write door — it creates a tree of files
		// outside the store and stamps the ledger's row; export.sets is a
		// read and not a door at all. Named by constant, the LW8 discipline.
		MethodBackupRun, MethodBackupVerify, MethodBackupPrune,
		MethodExportRun,
		// S57 T8: the gate plane's nine write doors are the person's (VG17) —
		// the six verify writes (add, update, archive, restore, applyTemplate
		// and the spawning try), the rule's set/clear, and the DoD mark. The
		// thirteen reads are not doors at all. Named by constant, the LW8
		// discipline.
		MethodVerifyAdd, MethodVerifyUpdate, MethodVerifyArchive, MethodVerifyRestore,
		MethodVerifyApplyTemplate, MethodVerifyTry,
		MethodMergeRuleSet, MethodMergeRuleClear,
		MethodDoDMark,
		// S59 T7 (EV16, EV7; the classification T6's plane was left
		// without): the execution-environment plane's ELEVEN writes are the
		// person's — seven environment doors (create, update, archive,
		// restore, bind, unbind, teardown) and four reference doors
		// (create, update, archive, restore). What is NOT here is as
		// deliberate as what is: the nine READS are not doors at all (the
		// gate plane's shape), four of them are an agent's catalog tools
		// (S59 T7), and the plane's one agent door —
		// environment.proposeRestrict, a TIGHTENING only — rides
		// surfaceProposeOnlyTwins. Named by constant, the LW8 discipline.
		MethodEnvironmentCreate, MethodEnvironmentUpdate,
		MethodEnvironmentArchive, MethodEnvironmentRestore,
		MethodEnvironmentBind, MethodEnvironmentUnbind, MethodEnvironmentTeardown,
		MethodSecretRefCreate, MethodSecretRefUpdate,
		MethodSecretRefArchive, MethodSecretRefRestore,
		// S60 T8 (MQ13): the six merge-queue writes are the person's doors —
		// enqueue, dequeue, reorder, retry, land, settings. The four reads
		// are nobody's door and stay unmarked.
		MethodMergeQueueEnqueue, MethodMergeQueueDequeue, MethodMergeQueueReorder,
		MethodMergeQueueRetry, MethodMergeQueueLand, MethodMergeQueueSettings,
		// S48 T8: the blueprint write doors are the person's (BP2) — save
		// (rewritten in T6), duplicate, delete, revert. The reads (list,
		// show, fields, validate and the five T7 doors) are nobody's door
		// and stay unmarked; validate in particular is narrow-projected.
		MethodBlueprintSave, MethodBlueprintDuplicate,
		MethodBlueprintDelete, MethodBlueprintRevert, MethodBlueprintOverride,
		MethodStepStop, MethodLoopAnswer, MethodStepSteer, MethodStepSteerCancel,
		MethodTerminalOpen, MethodTerminalInput, MethodTerminalResize, MethodTerminalAttach,
		MethodTerminalDetach, MethodTerminalClose,
		// LW8's discipline: these are named by constant, not by a second dotted
		// spelling, which is what keeps the steering and terminal doors on the
		// socket's one table. The mcp./seed. families below are the same names the
		// S46 and S43-extra guard slices settled, spelled once here.
	}
}

// surfaceHumanDoorPrefixes is the prefix form of the same claim, for the families
// named by prefix rather than by whole method (S46's seven writes share two
// prefixes; S56's grants share one verb set).
func surfaceHumanDoorPrefixes() []string {
	// No "terminal." prefix: the two terminal READS (list) are nobody's door but
	// the person's either way, and a prefix that sweeps them in would assert a
	// human door the slice never claimed. The six terminal write doors are named
	// whole in surfaceHumanDoorFamilies.
	return []string{"mcp.server.", "mcp.grant.", "mcp.core.", "autonomy.grant", "autonomy.deny", "autonomy.revoke"}
}

// surfaceClassifiedNames is the table's domain, sorted — the enumeration the
// completeness tests diff against the live table.
func surfaceClassifiedNames() []MethodName {
	table := surfaceTable()
	names := make([]MethodName, 0, len(table))
	for n := range table {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// ── the accessors the daemon package's completeness tests read the table
// through. The table itself stays unexported: the only way to change a
// classification is to edit a table file, and these three functions answer
// questions rather than hand out the map.

// SurfaceClassified answers the declared classification for one method, and
// whether the table holds it at all.
func SurfaceClassified(name MethodName) (Surface, bool) { return surfaceClassify(name) }

// SurfaceClassifiedNames is the table's domain, sorted.
func SurfaceClassifiedNames() []MethodName { return surfaceClassifiedNames() }

// ValidSurfaceKind says whether a word is one of the four kinds — the same check
// the filter runs, so a client and a test cannot disagree about the vocabulary.
func ValidSurfaceKind(k SurfaceKind) bool { return validSurfaceKind(k) }
