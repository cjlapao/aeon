package protocol

// The method table, exactly. The want-list is the whole story of the
// table's contents, and it lives in its own file (SIZE1) — the handshake
// behaviour tests stay in handshake_test.go.

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
)

// The table as each slice leaves it. **This list grows additively** — a new
// method does not bump PROTOCOL_VERSION — and updating it is how a slice
// records what it added.
func TestTheMethodTableHasExactlyTheExpectedMethods(t *testing.T) {
	tbl := newTestTable(t)
	// S44 T5: the settings surface is conditionally registered (V13 — a nil
	// seam leaves it absent), so the exact-set test wires the seam before the
	// listing; a table that did not would see the five settings.* missing.
	tbl.Settings = settingsStore(t)
	tbl.RegisterSettings()
	// S45 T4 (PM1): the authoring doors are conditionally registered too
	// (V13 — the RegisterSeed pattern), so the exact-set test wires the
	// seam before the listing, exactly as it wires the settings one. With
	// the seam wired the three names are IN the table; without it
	// TestPolicyAuthoringRegistrationIsConditional pins them ABSENT.
	tbl.PolicyAuthoring = stubPolicyAuthoring{}
	tbl.RegisterPolicyAuthoring()
	// S45 T5 — same conditional wiring for the proposal DECISION doors: the
	// stub seam goes on the table before the listing (the settings/policy
	// pattern), so the three names are the REAL conditional registration
	// being measured, not a list asserted against nothing.
	tbl.ProposalDecider = &decideStub{}
	tbl.RegisterProposalDecider()
	// S45 T7 (PM9) — the propose twins, likewise CONDITIONAL: the records
	// service stub (its Do half belongs to the daemon's records suites;
	// this listing measures the registration itself), so the two names
	// stand here because the door is wired. Absent-without-the-seam is
	// pinned by TestPolicyProposeRegistrationIsConditional (V13).
	tbl.Records = recordsListingStub{}
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused a wired records seam")
	}
	// S46 T3 (MG5-reads/MG8) — the "S46 reads" block: the five MCP
	// management reads are conditionally registered by RegisterMCPAdmin
	// (V13 — the settings/proposal-decider pattern), so the exact-set
	// test wires the func seam before the listing and the five names
	// stand here BECAUSE the door is wired — the real registration being
	// measured, not a list asserted against nothing. (The handshake's
	// own `Capabilities()` is static — ["subscribe"] — and never
	// per-family: what a wired slice reports is its registered method
	// names, which is exactly what this listing measures.
	// TestMcpAdminAbsentWhenSeamNil pins the absent side.)
	tbl.MCPAdmin = func(_ context.Context, _ string, _ json.RawMessage) (any, string, error) {
		return struct{}{}, "", nil
	}
	if !tbl.RegisterMCPAdmin() {
		t.Fatal("RegisterMCPAdmin refused a wired seam — the S46 reads are wired out of existence")
	}
	// S46 T4's mcp.importFile is a WRITE door: it needs the actor stamp as
	// well as the seam (V13 applied twice — absent until BOTH are wired).
	tbl.ActorFor = func(context.Context, *Session) Actor { return Actor{Kind: "user", ID: "1000"} }
	// S46 T5: the seven human WRITE doors ride the seam (registered iff
	// MCPAdmin is wired; the actor is checked per call at the dispatch
	// edge — TestMcpWriteDoorsRefuseUnresolvablePeers pins the refusal).
	if !tbl.RegisterMCPWrites() {
		t.Fatal("RegisterMCPWrites refused a wired seam — the human write doors are wired out of existence")
	}
	if !tbl.RegisterMCPImportFile() {
		t.Fatal("RegisterMCPImportFile refused a wired seam AND actor stamp — the human import door is wired out of existence")
	}
	// S56 T6: the autonomy surface is conditionally registered the same way —
	// the ten autonomy.* methods are absent without the seam, so the
	// exact-set test wires a fake before the listing.
	tbl.Autonomy = &autonomyFake{}
	tbl.RegisterAutonomy()
	// S56 T8 (2026-11-01): the two propose twins ride the RECORDS seam (the
	// recordsListingStub above), so their conditional registration is wired
	// here too — Count +2, deliberately (the two restrictive kinds, AL11).
	if !tbl.RegisterAutonomyPropose() {
		t.Fatal("RegisterAutonomyPropose refused a wired records seam")
	}
	// S61 T8 (2026-11-05) — the two goal-intake propose twins ride the
	// RECORDS seam too (the recordsListingStub above), so their conditional
	// registration is wired here; Count +2, deliberately (GI17's two inert
	// asks; the plane's four writes appear nowhere on this list).
	if !tbl.RegisterIntakePlanePropose() {
		t.Fatal("RegisterIntakePlanePropose refused a wired records seam")
	}
	// S46 T8 (2026-11-02): the five agent-scoped mcp.mine.* twins ride BOTH
	// seams (records for their Do, MCPAdmin for the scoped rows half) — both
	// are wired above, so they register here; the absent side is pinned by
	// TestMcpAdminAbsentWhenSeamNil. Count +5, measured by this listing.
	if !tbl.RegisterMCPMine() {
		t.Fatal("RegisterMCPMine refused a wired records seam AND admin seam — the agent doors are wired out of existence")
	}
	// S55 T5 (2026-11-01): the loop-control plane is conditionally
	// registered (V13 — a nil seam leaves it absent), so the exact-set test
	// wires the seam before the listing, the way the settings surface is.
	// Count +7 DELIBERATE, measured by this test's own listing.
	tbl.LoopControl = testLoopControlService{}
	tbl.RegisterLoopControl()
	// S55 T8 (2026-11-01): the budget plane's eight doors — the four reads
	// (list/show/effective, spend.report) and the three human doors
	// (set/remove/raise) plus vocabulary. Conditionally registered (V13 — a
	// nil seam leaves them absent), so the exact-set test wires the seam
	// first, the way the settings and loop-control surfaces are. Count +8
	// DELIBERATE, measured by this test's own listing.
	tbl.BudgetStore = testBudgetService{}
	tbl.RegisterBudget()
	// S55 T9 (2026-11-01): the watchdog plane's six doors — the three reads
	// (list/show/vocabulary) and the three human doors (set/setEnabled/
	// remove). Conditionally registered (V13 — a nil seam leaves them
	// absent), so the exact-set test wires the seam first, the way the
	// budget surface is. Count +6 DELIBERATE, measured by this test's own
	// listing.
	tbl.Watchdog = testWatchdogService{}
	tbl.RegisterWatchdog()
	// S62 T6 (2026-11-02): the backup family's five doors (run/list/
	// verify/status/prune) are conditionally registered (V13 — a nil seam
	// leaves them ABSENT; TestBackupMethodsAbsentWhenServiceNil pins that
	// side), so the exact-set test wires the zero-valued seam first, the
	// way the budget and watchdog surfaces are. Count +5 DELIBERATE,
	// measured by this test's own listing.
	tbl.Backup = testBackupService{}
	tbl.RegisterBackup()
	// S62 T8 (2026-11-02): the export family's two doors (run/sets) are
	// conditionally registered the same way (V13 — a nil seam leaves them
	// ABSENT; TestExportMethodsAbsentWhenServiceNil pins that side), so the
	// exact-set test wires the zero-valued seam first, the way the backup
	// surface is. Count +2 DELIBERATE, measured by this test's own listing.
	tbl.Export = testExportService{}
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a wired seam")
	}
	// S57 T8 (2026-11-02): the gate plane's twenty-two doors — the verify
	// family's twelve (VG18), the merge plane's eight (the rule's six plus
	// merge.events and merge.watches riding the same seam) and the DoD's
	// two. All conditionally registered (V13 — a nil seam leaves them
	// ABSENT, as TestMethodsAbsentWhenServiceNil pins for these three
	// seams), so the exact-set test wires the three zero-valued seams
	// first, the way the backup surface is. Count +22 DELIBERATE,
	// measured by this test's own listing.
	tbl.Verify = testVerifyService{}
	tbl.RegisterVerify()
	tbl.MergeRule = testMergeRuleService{}
	tbl.RegisterMergeRule()
	tbl.DoD = testDoDService{}
	tbl.RegisterDoD()
	// S57 T9 (2026-11-03): the three gate twins ride the RECORDS seam
	// (the recordsListingStub above — their Do half belongs to the
	// daemon's records suites; this listing measures the registration
	// itself), conditional on it exactly as the policy and autonomy
	// twins are. Count +3 DELIBERATE, measured by this test's own
	// listing.
	if !tbl.RegisterGatePropose() {
		t.Fatal("RegisterGatePropose refused a wired records seam")
	}
	// S47 T5 (2026-11-02) — the person's tools catalog: the four READS
	// (`tools.list` · `tools.get` · `tools.byAgent` · `tools.status`) over the
	// code, the `tool` rows, the grant chain and the policy set, plus S47 T10's
	// vocabulary door (`tools.vocabulary`, the operator ruling of 2026-11-02
	// recorded at the constant). Conditionally registered by
	// RegisterToolsCatalog (V13 — a nil seam leaves the five
	// ABSENT; TestToolsMethodsAbsentWhenServiceNil pins that side, which is why
	// the stub is wired here rather than the doors being asserted against
	// nothing). Count +5 DELIBERATE, measured by this test's own listing.
	tbl.ToolsCatalog = testToolsCatalogService{}
	// S47 T6 (TC8): the narrow seam arrives the way cmd/aeon/serve.go supplies
	// it — a func value over the nine actions, because internal/narrow imports
	// this package and this package must never import it back. The nine are
	// spelled out here rather than imported; internal/daemon's
	// TestSurfaceDoorsEqualTheRealStructures proves the same nine against
	// narrow.Actions() itself, and a daemon that never sets this field reports
	// narrow:"unknown" for every method instead of a silent false.
	tbl.NarrowMethods = surfaceNineActions
	// S47 T6 (TC6): the two surface doors read the table itself, so they stand
	// whenever a table is built and registered.
	if !tbl.RegisterSurface() {
		t.Fatal("RegisterSurface returned false for a table that exists — the surface doors are wired out of existence")
	}
	if !tbl.RegisterToolsCatalog() {
		t.Fatal("RegisterToolsCatalog refused a wired seam — the S47 catalog is wired out of existence")
	}
	// S49 T3 (AN1/AN7) — the "S49 reads" block: the ten unified automation
	// reads are conditionally registered by RegisterAutomationReads (V13 —
	// the settings/tools-catalog pattern), so the exact-set test wires a
	// zero-state stub before the listing and the ten names stand here
	// BECAUSE the door is wired — the real registration being measured,
	// not a list asserted against nothing. TestAutomationReadsAbsentWhenSeam
	// Nil pins the absent side; the daemon's own suite pins the answers.
	tbl.AutomationReads = automationReadsStub{}
	if !tbl.RegisterAutomationReads() {
		t.Fatal("RegisterAutomationReads refused a wired seam — the S49 reads are wired out of existence")
	}
	// S60 T8 (2026-11-04, MQ13) — the merge-queue plane: the four reads
	// (list/show/status/vocabulary) and the six human-door writes (enqueue/
	// dequeue/reorder/retry/land/settings), conditionally registered over a
	// zero-state stub (V13 — TestMergeQueueMethodsRegisteredAndGated and
	// TestMergeQueueMethodsAbsentWhenSeamNil pin both sides; the daemon's
	// own suite pins the answers). Count +10 DELIBERATE, measured by this
	// test's own listing.
	tbl.MergeQueue = mergeQueueTableStub{}
	if !tbl.RegisterMergeQueue() {
		t.Fatal("RegisterMergeQueue refused a wired seam — the queue doors are wired out of existence")
	}
	// S63 T5 (2026-11-04, RT10) — the outcome-learning plane's ten doors:
	// the five scorecard reads, the three retro reads plus the one
	// human-door write (retro.backfill, RT13), and digest.learned — all
	// conditionally registered over zero-state stubs (V13; the absent
	// side is pinned in scorecard_test.go/retro_test.go/digest_test.go,
	// the real answers in the daemon's suite). Count +10 DELIBERATE,
	// measured by this test's own listing.
	tbl.Scorecard = scorecardTableStub{}
	tbl.Retro = retroTableStub{}
	tbl.Digest = digestTableStub{}
	if !tbl.RegisterScorecard() {
		t.Fatal("RegisterScorecard refused a wired seam — the scorecard doors are wired out of existence")
	}
	if !tbl.RegisterRetro() {
		t.Fatal("RegisterRetro refused a wired seam — the retro doors are wired out of existence")
	}
	if !tbl.RegisterDigest() {
		t.Fatal("RegisterDigest refused a wired seam — the digest door is wired out of existence")
	}
	// S61 T7 (2026-11-05, GI8/GI14/GI9) — the goal-intake plane's fourteen
	// doors: the five charter doors (four reads plus the human save), the
	// three template doors, and the six intake doors (start, three reads,
	// the wizard-shaped commit, the vocabulary) — all conditionally
	// registered over zero-state stubs (V13; the absent side is pinned in
	// charter_test.go/template_test.go/intake_test.go, the real answers in
	// the daemon's suite). Count +14 DELIBERATE, measured by this test's
	// own listing.
	tbl.Charter = charterTableStub{}
	tbl.Templates = templateTableStub{}
	tbl.Intake = intakeTableStub{}
	if !tbl.RegisterCharter() {
		t.Fatal("RegisterCharter refused a wired seam — the charter doors are wired out of existence")
	}
	if !tbl.RegisterTemplates() {
		t.Fatal("RegisterTemplates refused a wired seam — the template doors are wired out of existence")
	}
	if !tbl.RegisterIntake() {
		t.Fatal("RegisterIntake refused a wired seam — the intake doors are wired out of existence")
	}
	// S27 T7 (2026-11-05, CN18): the scheduler's twelve doors are
	// conditionally registered (V13 — a nil seam leaves ALL twelve
	// absent, scheduler.stopAll included, so the off switch can never
	// stand without S55 behind it), so the exact-set test wires a fake
	// before the listing. Count +12 DELIBERATE, measured by this test's
	// own listing.
	tbl.Continuous = &continuousFake{}
	if !tbl.RegisterContinuous() {
		t.Fatal("RegisterContinuous refused a wired seam — the scheduler surface is wired out of existence")
	}
	got := tbl.Methods()
	want := []MethodName{
		// S2
		MethodDebugSubscriptions, MethodInitialize, MethodPing,
		MethodSubscribe, MethodUnsubscribe, MethodVersion,
		// S14 T3 — the second debug method: the goroutine stacks that prove
		// an async Task owns no goroutine (AS4, walked in DEMO §4).
		MethodDebugGoroutines,
		// S3
		MethodAsk,
		// S3, amended: route management, so a client can configure a provider
		// without editing the environment and restarting.
		MethodRoutesList, MethodRoutesSet, MethodRoutesDelete,
		// S18 T3 — the operator's probe. Registered with its slice (N2);
		// it fires for nobody else (M32).
		MethodRoutesProbe,
		MethodRoutesTrace,
		// S4 — the store. The app's Store tab is `store.info`; the rest are
		// what let the test bench create something and read it back, where it
		// will be consumed rather than only from a terminal (AGENTS §4b).
		MethodStoreInfo, MethodStoreAwaiting,
		// S38 T15 (OL10/OL11) — the records catalog projected: a Spec is a
		// method, and the five doors only a person may knock on. Projected
		// names carry no consts (the catalog owns the spelling; the parity
		// test in records_parity_test.go walks both directions every run);
		// the person-only doors do, beside their handlers. Names a projection
		// SKIPS because a method already stands (project.*, component.list/
		// create/update, loop.list, and goal.show/task.list as the person's
		// richer doors) are absent from this block on purpose.
		MethodName("attachment.attach"), MethodName("attachment.list"), MethodName("attachment.put"), MethodName("attachment.read"),
		MethodName("brief.create"), MethodBriefDecide, MethodName("brief.show"),
		MethodName("component.show"),
		MethodName("epic.create"), MethodName("epic.list"), MethodName("epic.show"), MethodName("epic.update"),
		MethodGoalDecide, MethodName("goal.create"), MethodName("goal.list"),
		MethodName("goal.submit_review"), MethodName("goal.update"),
		MethodName("loop.get"),
		MethodName("task.bump_counter"), MethodName("task.create"), MethodTaskDecide,
		MethodTaskList, MethodName("task.set_status"), MethodName("task.show"), MethodName("task.update"),
		MethodGoalShow,
		// S19 T5 — the question READ, with its slice (N2). It is the only
		// question-facing method the narrow surface mounts, and it is a
		// read: answering stays a person's act (N16), and no future method
		// for answering may appear here with a projection (permanent, not
		// deferred — internal/narrow/questions.go says so where the next
		// person looking for the flag will read it).
		MethodQuestionPending,
		// S20 T6 — the webhook is a MOUNTING on this table (TR11), not a
		// listener with a private dialect; the delivery is a method with a
		// fixture, and the signature rides as a param because stdio has no
		// headers (N18).
		MethodWebhookDeliver,
		// S21 T6 — the completion of an async remote Task is a call to
		// THIS surface, not a held connection (RA7): the mounting
		// webhook.deliver established, with the binding that makes it
		// safe (RA8). Public by construction — the peer's inbound door is
		// what the narrow table must mount; the peer's signature is the
		// authentication, one layer down.
		MethodRemoteComplete,
		MethodTriggerList, MethodTriggerPause, MethodTriggerResume, MethodTriggerFireNow, MethodTriggerHistory, MethodTriggerAdd,
		MethodLoopCreate, MethodLoopList, MethodLoopWrite, MethodLoopRead,
		MethodStepShow, MethodStepVerify, MethodStepList,
		// S5 — the step runtime. One method: the skateboard's entry point.
		MethodStep,
		// S6 — suspension and cold continuation.
		MethodAnswer,
		// S8 — editable blueprints, validation and durable loop execution.
		MethodBlueprintList, MethodBlueprintFields, MethodBlueprintModel, MethodBlueprintRender, MethodBlueprintVocabulary, MethodBlueprintShow, MethodBlueprintValidate, MethodBlueprintSave,
		MethodLoopStart, MethodLoopShow, MethodLoopAnswer,
		// S42 — the durable, FIFO-windowed loop history (the chat panel's
		// backfill, distinct from the live event ring).
		MethodLoopTranscript,
		MethodLoopEvents,
		MethodLoopSteps,
		MethodLoopOptions,
		MethodLoopSetArchived, MethodLoopDelete, MethodLoopTombstones, MethodLoopDocuments,
		MethodVoiceTranscribe, MethodVoiceSpeak, MethodVoiceTurn, MethodVoiceEmbed,
		// S39 T4 — the human door out of rest: idle → done, claimed in the
		// store, refused as data for every other status (CR1).
		MethodLoopClose,
		// S39 T6 — the message that starts a conversation turn in a resting
		// loop (CR3): one ordinary step, refusals as data (CR10).
		MethodLoopMessage,
		// S39 T9 — the human run doors: a second blueprint in a resting
		// loop, and the options dialogue that precedes it. No ask in front
		// of either: a person typing is the confirmation (CR4).
		MethodLoopRun, MethodLoopRunOptions,
		// S13 T10 — the one narrow retry: a historical missing-handler block,
		// retried once, decided by the daemon.
		MethodLoopRetryHandlerBlock,
		MethodLoopRecover,
		// Bench demo runner — the handlers' two socket reads; refusals as data.
		MethodHandlersList, MethodHandlersVerify,
		// Product project and loop document reader.
		MethodLoopFiles, MethodLoopFileRead,
		MethodComponentCreate, MethodComponentUpdate, MethodComponentList,
		MethodComponentPromote, MethodComponentAttach,
		MethodComponentDetach, MethodComponentArchive, MethodComponentRestore,
		// S40 T7 — the automation desk's three reads (AU14). Added
		// 2026-10-28 with the slice; RequiresInitialize keeps the
		// public/hidden balance below unmoved.
		MethodAutomationTriggerList, MethodAutomationTriggerShow, MethodAutomationTriggerFires,
		// S40 T8 — the four human-only write doors (AU1). These are the
		// socket's ONLY writes whose actor is the connection's peer
		// credential: an agent is refused before anything is judged, and
		// tools/, narrow/ and the handler bodies are grep-pinned away from
		// them (TestNoTriggerAuthoringIsReachableFromAnAgentTool). This is
		// AU14's ruling, distinct from PL15's CLI-only: the kernel, not a
		// CLI, is the person's door here.
		MethodAutomationTriggerCreate, MethodAutomationTriggerUpdate,
		MethodAutomationTriggerEnable, MethodAutomationTriggerDisable,
		// S10 — the policy reads (ruling R4: the app's read-side window).
		// Four names, exactly; no authoring verb is in this list or can be
		// (R4c — policy.add/policy.remove are CLI-only, never registered).
		MethodPolicyList, MethodPolicyWhy, MethodPolicyInForce, MethodPolicyEvents,
		// S45 T2 (PM5/PM6) — the policy DETAIL reads: `policy.get` (the
		// whole row: identity + raw spec + describe/createdAt + the two
		// informational records) and `policy.vocabulary` (closed sets,
		// the authorship matrix COMPUTED from the rule, per-type spec
		// descriptors). Two more READS on the same *policy.Store handle,
		// RequiresInitialize, not Debug, zero writes — and still no
		// authoring verb joins this list or any future one by accident
		// (R4c/PL15; the cmd/aeon grep guard whitelists exactly these six
		// read names; the write doors arrive with T4/T5 as the operator's
		// human door). Count +2, measured by this test's own listing.
		MethodPolicyGet, MethodPolicyVocabulary,
		// S45 T3 (PM8) — the authoring AUDIT read: `policy.changes` (who
		// added/removed/seeded/committed/rejected, when, from which door),
		// over the append-only policy_change table through a reads-only
		// seam. One more READ — RequiresInitialize, not Debug, zero
		// writes; the audit has no write door here, now or later at this
		// task (the doors land with T4/T5 as the operator's human door).
		// Count +1, measured by this test's own listing.
		MethodPolicyChanges,
		// S45 T4 (PM1) — the authoring HUMAN door on the socket: the add,
		// remove and seedCorpus verbs (the operator ruling of 2026-10-30).
		// PL15's amendment, not its repeal: RequiresInitialize true, Debug
		// false, the actor stamped from the CONNECTION (never a param — an
		// actor key is refused as a shape error), every boundary refusal
		// verbatim, before/after in-force on every write, one audit row per
		// successful write (PM8) — and NOTHING of this reaches the narrow
		// surface, agent services, the tool registry or the records
		// catalog (PM10's negatives; the amended cmd/aeon walk owns them).
		// Count +3, measured by this test's own listing.
		MethodPolicyAdd, MethodPolicyRemove, MethodPolicySeedCorpus,
		// S45 T5 (PM1/PM7) — the proposal DECISION doors on the socket:
		// `proposal.get` (row + digest + advisory preview), `proposal.commit`
		// {id, digest} (the effect through desk's one registry, then the
		// claimed-from flip, then one policy_change row) and `proposal.reject`
		// {id, reason} (flip + audit, no effect — the reason is required and
		// ≤ 512 bytes). The same ruling as T4's three, applied to deciding:
		// conditionally registered iff Table.ProposalDecider is wired,
		// RequiresInitialize true, Debug false, the actor the CONNECTION's
		// peer stamp (never a param), agent_import commits refused by name
		// until T10. The S35 trio (create/list/discard) is unchanged — the
		// pre-S45 header story of proposals.go still holds. Count +3,
		// measured by this test's own listing.
		MethodProposalGet, MethodProposalCommit, MethodProposalReject,
		// S45 T7 (PM9) — the propose twins: `policy.proposeAdd` and
		// `policy.proposeRemove`, catalog actions on the socket answered
		// through the records service (stub above; registration conditional
		// on it, V13 — TestPolicyProposeRegistrationIsConditional pins the
		// absent side). They create INERT proposal rows and nothing else:
		// never a policy row, never a decision. RequiresInitialize true,
		// Debug false. The five policy/proposal READS add nothing here —
		// the projection skips dot-names that already stand, so the tools
		// reuse policy.list/get/why/inForce and proposal.list themselves.
		// Count +2, measured by this test's own listing.
		MethodPolicyProposeAdd, MethodPolicyProposeRemove,
		// S24 T7 (CG9): the queue's socket verbs — the named departure from
		// PL15 (authoring socket-writable BY DECISION). Seven new names join;
		// Count +7 DELIBERATE, measured by this test's own listing.
		MethodQueueAppend, MethodQueueList, MethodQueueReorder, MethodQueueRemove,
		MethodQueueSetSize, MethodQueueSettings, MethodQueueAdvance,
		// S26 T5 (CG9's extension, brief §5's D5): the roadmap's socket
		// verbs — the five the brief names (add/list/show/cancel/estimate)
		// plus the four doors that make the eight-status machine walkable
		// from the app (priority/ideate/plan/materialize): P34 — a state
		// machine nothing on the surface can walk is not accepted as
		// finished. Count +9, measured by this test's own listing.
		MethodRoadmapAdd, MethodRoadmapList, MethodRoadmapShow,
		MethodRoadmapCancel, MethodRoadmapEstimate, MethodRoadmapPriority,
		MethodRoadmapIdeate, MethodRoadmapPlan, MethodRoadmapMaterialize,
		// S50 T2 — the four management doors: update (the narrow, audited
		// edit), board/history/vocabulary (the reads that answer every
		// item's standing, its trail, and the machine's shape). Count +4,
		// deliberate; the lifecycle itself is unchanged (eleven edges,
		// pinned in internal/roadmap).
		MethodRoadmapBoard, MethodRoadmapHistory, MethodRoadmapUpdate, MethodRoadmapVocabulary,
		// S27 T3 (CN7): the roadmap's GRAPH doors. Two edges an item
		// authors — depend writes intent, undepend removes it, and neither
		// moves a status: readiness is read at the attempt, never stored
		// (D4). The refusal sentences are the store's laws (same root, no
		// cycle, no edge on history) rendered at the door. Count +2,
		// measured by this test's own listing.
		MethodRoadmapDepend, MethodRoadmapUndepend,
		// S50 T3 (RM6): the settings's honest read. Count +1, deliberate.
		MethodRoadmapHealth,
		// S28 T4 (P34's ordering): the learnings' socket verbs — the
		// classified, queryable record the app walks. add/list/show/query
		// ride one internal/learnings instance the CLI and the bench also
		// speak through; embed is the trigger door (T3's pass, the analogue
		// of retrieval.reindex) that fills learning_vec when a route is
		// configured; the scope validation (§3) and the relayed write-
		// protection (M1) live in that one place, so a second authoring
		// path cannot appear. Count +5, measured by this test's own listing.
		MethodLearningsAdd, MethodLearningsList, MethodLearningsShow,
		MethodLearningsQuery, MethodLearningsEmbed,
		// S52 T2 (LM2/LM5/LM6): the learnings' MANAGEMENT doors —
		// retract/restore are the human door's acts (the peer stamps; an
		// agent is refused by name and gets T4's proposal kind), and
		// board/history/vocabulary/forAgent are the open reads that write
		// nothing. Count +6, measured by this test's own listing.
		MethodLearningsRetract, MethodLearningsRestore, MethodLearningsBoard,
		MethodLearningsHistory, MethodLearningsVocabulary, MethodLearningsForAgent,
		// The knowledge map: an open read beside board. Count +1, measured by this test's own listing.
		MethodLearningsMap,
		// S29 T7 (P34's ordering): the triage's socket verbs — the bug
		// backlog's daemon surface the CLI and the bench also speak through
		// one internal/triage instance. file/list/show/set-priority/
		// confirm-duplicate are the CRUD doors; validate/handoff are the
		// daemon's dispatch doors (T3/T6); suggest-duplicates is the read
		// (T4, a suggest, never a write). Count +8, measured by this test's
		// own listing.
		MethodTriageFile, MethodTriageList, MethodTriageShow,
		MethodTriageSetPriority, MethodTriageConfirmDuplicate,
		MethodTriageValidate, MethodTriageHandoff, MethodTriageSuggest,
		// S51 T2 (TM5/TM7): the management edit and its three reads —
		// update is the human-door write, board/history/vocabulary the
		// backlog reads. Count +5, measured by this test's own listing:
		// S51 T3 (TM4) adds the setting's standing read.
		MethodTriageUpdate, MethodTriageBoard,
		MethodTriageHistory, MethodTriageVocabulary,
		MethodTriageHealth,
		// S25 — the history doors (registered at method.go; the want had
		// dropped them, a pre-existing gap on the base, fixed here so the
		// exact-set test matches what the table registers).
		MethodHistoryList, MethodHistoryShow, MethodHistoryExport,
		// S53 T1 (HV5): coverage (HV1's honesty read) and vocabulary (HV2's
		// closed sets) — same family, same seam, both reads. Count +2.
		MethodHistoryCoverage, MethodHistoryVocabulary,
		// S9's chat view — stopping and resuming an ad-hoc step a PERSON
		// paused, never the model. Added after S10; does not touch anything
		// S11 owns (step_stop.go's own doc comment says why).
		MethodStepStop, MethodStepContinue,
		// S31 — the loop workspace. The table registers at slice open as ONE
		// reviewable diff (the B11 argument applied to the socket); handlers
		// arrive with T4/T6/T7 and replace their `ErrNotWired` stubs. None of
		// these may reach the narrow HTTP surface (LW8) — the guard test
		// lands with T9, mirroring TestNoPolicyAuthoringReachesTheNarrowSurface.
		MethodStepSteer, MethodStepSteers, MethodStepSteerCancel,
		MethodWorkspaceGitStatus, MethodWorkspaceGitDiff, MethodWorkspaceGitChanges,
		MethodTerminalOpen, MethodTerminalInput, MethodTerminalResize,
		MethodTerminalAttach, MethodTerminalDetach, MethodTerminalClose, MethodTerminalList,
		// S14 — the agent-services surface, registered as ONE reviewable
		// diff at slice open (the same B11 argument); agent.go holds the one
		// forwarding handler and the enumeration of what each cannot do.
		// The three writes are Debug (socket-only, LW8's guard); the four
		// reads are the app's window, like policy.*.
		MethodAgentDiscover, MethodAgentCard, MethodAgentInvoke,
		MethodAgentTaskList, MethodAgentTaskShow,
		MethodAgentTaskComplete, MethodAgentTaskFail,
		// S15's one read — public (a read is the app's window), and the
		// only plugin-shaped name this table will ever carry: the four
		// state verbs are CLI/app doors, never socket methods (R6).
		MethodAgentPlugins,
		// S34 T1/T2 — the trust bench's two roster/authority reads.
		MethodAgentList, MethodToolsGrants,
		// S34 T3/T4 — the MCP config truth and the operator-triggered probe.
		MethodMCPStatus, MethodMCPCheck,
		// S35 T3 — the inert half of the staging line: a proposal row that
		// constrains nothing. Deliberately absent, here and forever:
		// proposal.commit and proposal.reject (brief §7's human-only door).
		MethodProposalCreate, MethodProposalList, MethodProposalDiscard,
		// S35 T4 — plain writes within already-bounded authority (brief
		// §3's line): the imported agent already passed blocklist +
		// consent at import, so moving it starts something already
		// permitted, unlike import itself (which stays CLI/app-only).
		MethodAgentEnable, MethodAgentDisable, MethodAgentRevoke,
		// S43 T7 — the agent-management family (create/import/export/
		// duplicate/delete/edit/view/migrate): the agent table's ONE CRUD
		// surface the CLI and the app speak through Table.AgentManage, one
		// seam dispatched by name (the same shape agent.enable/disable/
		// revoke use). Ordinary daemon writes, not PL15 policy authoring —
		// they touch the agent table, not the closed policy set — so they
		// are registered like loop.create, not the human-only door.
		MethodAgentCreate, MethodAgentImport, MethodAgentExport,
		MethodAgentDuplicate, MethodAgentDelete, MethodAgentEdit,
		MethodAgentView, MethodAgentMigrate,
		// S73 T2 — the management window joins the same seam and the same
		// registration shape. A READ, so it carries no actor: the person is
		// stamped at the acts (SP6), and this act is looking.
		MethodAgentRows,
		// S73 T5 — the ledger's window joins the same seam under the same
		// shape, for the same reason: it reads what the six writes above mint,
		// and a read stamps nobody.
		MethodAgentChanges,
		// S33 T8 — the telemetry reads (TL10): the primary surface for the
		// counting tiers. Four reads, zero writes — a write door here would
		// let a caller fabricate the numbers this slice exists to make
		// honest.
		MethodTelemetryLoop, MethodTelemetryComponent,
		MethodTelemetryProject, MethodTelemetryWorkspace,
		// S33 T9 (the bench's Delete): the loop's honest Delete — cold
		// storage with its state, never destruction; D8's claim is that
		// the telemetry rollups do not ride along.
		MethodLoopArchive,
		// S44 settings — the five settings.* doors (list/get/set/unset/
		// history), conditionally registered by RegisterSettings (V13: a nil
		// seam leaves them absent, not refusing). The only door the Settings
		// UI (T8) uses and the CLI (T7) is a client of (P34). All five are
		// RequiresInitialize, none is Debug, and none is projected onto the
		// narrow surface (CS9's allow-list never names them).
		MethodSettingsList, MethodSettingsGet, MethodSettingsSet,
		MethodSettingsUnset, MethodSettingsHistory,
		// S70 MB7 — the mailbox read: the ONE agent.messages door. It lists
		// a loop's agent↔agent rows (scope from the loop row) and MB7
		// forbids any authoring counterpart on the surface: a person
		// steers (loop.message), an agent files through the ask tool.
		MethodAgentMessages,
		// S46 T3 (MG5-reads/MG8) — the PERSON's MCP management reads:
		// `mcp.servers` (the list WITH details — env NAMES only, MG13),
		// `mcp.server` (everything, incl. the last 20 audit rows),
		// `mcp.grants` (S12's GrantView verbatim plus core-narrowing and
		// declares, for any agent — the person's view, MG8),
		// `mcp.vocabulary` (transports/rerun/effect from the validator's
		// own constants, grammars as regexp strings), and `mcp.changes`
		// (the append-only audit, three-state honest). Conditionally
		// registered iff Table.MCPAdmin is wired (V13 — stub above;
		// TestMcpAdminAbsentWhenSeamNil pins the absent side),
		// RequiresInitialize true, Debug false, zero writes (D9), no
		// probe from any read (MG11). The agent-scoped reads are T8's
		// tools and a different code path — no tool stands behind these
		// five names. Count +5, measured by this test's own listing.
		MethodMCPServers, MethodMCPServer, MethodMCPGrants,
		MethodMCPVocabulary, MethodMCPChanges,
		// S46 T4 (MG3) — `mcp.importFile`, the first MCP WRITE door: the
		// human deliberately adopts a legacy file (dry-run | replace).
		// Registered iff the seam AND the actor stamp are wired (an
		// unattributable configuration write is absent, not half-wired);
		// the actor is the peer credential, never a param. Count +6.
		MethodMCPImportFile,
		// S56 T6 — the autonomy human door (AL11), added 2026-11-01: three
		// person-only writes (grant/deny/revoke, the actor stamped from the
		// connection's peer, never params) and seven reads (list/show/
		// vocabulary/effective/pinned/changes/uses). Conditionally
		// registered by RegisterAutonomy (V13: a nil seam leaves the ten
		// ABSENT). All ten are RequiresInitialize, none is Debug, and none
		// is projected onto the narrow surface (AL17; T7's guards).
		MethodAutonomyList, MethodAutonomyShow, MethodAutonomyVocabulary,
		MethodAutonomyEffective, MethodAutonomyPinned, MethodAutonomyChanges,
		MethodAutonomyUses, MethodAutonomyGrant, MethodAutonomyDeny,
		MethodAutonomyRevoke,
		// S56 T8 (2026-11-01) — the two propose twins (AL11's agent door):
		// Count +2, deliberately. The restrictive-only rule holds on this
		// list: no grant method and no kind that widens appear here or in
		// the catalog.
		MethodAutonomyProposeRevoke, MethodAutonomyProposeDeny,
		// S46 T5 (MG5) — the seven human write doors (the server four and
		// the two grants + core narrowing). Conditional on the seam; the
		// peer stamp is enforced per call (an unresolvable peer is refused
		// with ErrMcpNoActor, nothing half-written); identity keys in
		// params are shape errors. Count +7.
		MethodMCPServerAdd, MethodMCPServerUpdate, MethodMCPServerRemove,
		MethodMCPServerSetEnabled, MethodMCPServerShare, MethodMCPGrantDeclare, MethodMCPGrantRevoke,
		MethodMCPCoreSet,
		// 2026-11-08 — the inspect door: runs a CANDIDATE server and lists its
		// tools (initialize + tools/list), stores nothing. Same human-door
		// edge as the seven above. Count +1, deliberate.
		MethodMCPServerInspect,
		// 2026-11-08 — the MCP servers' processes: two reads (state/PID, the log
		// ring) and the person's start / stop / restart. Count +5, deliberate.
		MethodMCPProcesses, MethodMCPProcessLogs,
		MethodMCPProcessStart, MethodMCPProcessStop, MethodMCPProcessRestart,
		// S46 T8 (MG8/MG9/MC2) — the five AGENT-SCOPED doors: mcp.mine.servers,
		// mcp.mine.grants, mcp.mine.status (own-view reads through the same
		// rows/registry/cache, scoped by the STAMP) and mcp.mine.proposeRemove /
		// mcp.mine.proposeRevoke (inert narrowing proposals through the records
		// door). They refuse a person session with a pointer sentence; a
		// param naming an agent is a parse error. Count +5, deliberate.
		MethodMCPMineServers, MethodMCPMineGrants, MethodMCPMineStatus,
		MethodMCPMineProposeRemove, MethodMCPMineProposeRevoke,
		// S55 T5 (2026-11-01) — the loop-control plane's seven doors
		// (LC2–LC5, LC15, LC16): the four person writes (stop/resume/cancel/
		// dismiss), the control read, the stop-request desk's socket twin and
		// its two reads. All RequiresInitialize, none Debug, and none of them
		// may reach the narrow surface (the guard amendment in
		// cmd/aeon/loop_control_amend_test.go).
		MethodLoopStop, MethodLoopResume, MethodLoopCancel, MethodLoopControl,
		MethodLoopRequestStop, MethodLoopControlRequests, MethodLoopControlDismiss,
		// S55 T8 (2026-11-01) — the budget plane's eight doors (LC5, LC8,
		// LC12–LC14, LC20): the four reads (list/show/effective and
		// spend.report), the three human doors (set/remove and loop.raise —
		// the person is stamped from the connection, never from params), and
		// vocabulary (the closed sets the clients build from — no client
		// copy of them exists). All RequiresInitialize, none Debug, and none
		// may reach the narrow surface (the guard amendment in
		// cmd/aeon/budget_amend_test.go).
		MethodBudgetList, MethodBudgetShow, MethodBudgetEffective, MethodSpendReport,
		MethodBudgetSet, MethodBudgetRemove, MethodBudgetVocabulary, MethodLoopRaise,
		// S55 T9 (2026-11-01) — the watchdog plane's six doors (LC17,
		// LC18, LC20): the three reads (list/show and vocabulary — the
		// closed sets WITH availability the clients build from) and the
		// three human doors (set/setEnabled/remove — the person is stamped
		// from the connection, never from params). All RequiresInitialize,
		// none Debug, and none may reach the narrow surface (the guard
		// amendment in cmd/aeon/watchdog_amend_test.go).
		MethodWatchdogList, MethodWatchdogShow, MethodWatchdogVocabulary,
		MethodWatchdogSet, MethodWatchdogSetEnabled, MethodWatchdogRemove,
		// S47 T5 (TC5/TC9/TC10/TC11) — the person's tools catalog: the four
		// READS and nothing else. `tools.list` (the whole catalog, filters
		// answered daemon-side), `tools.get` (one tool, every agent that
		// exists, the policy rows that touch it), `tools.byAgent` (one
		// agent's whole view with the engine's verbatim sentence per row),
		// `tools.status` (the `tools.core` seed state and the drift REPORT —
		// TC11: said out loud, never repaired). All four RequiresInitialize,
		// none Debug, none projected onto the narrow surface (TC9/MC2: the
		// human door; an agent's own view is T7's `tools_mine`), zero writes
		// (TC1) and zero audit rows. `tools.grants` (S34) is NOT on this list
		// and is not touched by the slice — TestExistingToolsGrantsUnchanged
		// pins its shape so "improving" it stays a reviewable diff.
		// S47 T10 (2026-11-02, the operator ruling recorded at the constant)
		// adds the fifth: `tools.vocabulary`, the capability vocabulary with
		// the daemon's one-line meanings beside the family, source and
		// re-run-class spellings a filter may be offered — same shape, same
		// door rules, still zero writes. Count +5, measured by this test's own
		// listing.
		MethodToolsList, MethodToolsGet, MethodToolsByAgent, MethodToolsStatus, MethodToolsVocabulary, MethodToolsMatchPattern,
		// S47 T6 (TC6/TC8) — the two SURFACE doors: `surface.list` (every method
		// this table holds, classified into the four kinds and with its doors
		// COMPUTED from the live structures) and `surface.get` (one method, the
		// nearest names when the one typed does not exist). Both RequiresInitialize,
		// neither Debug, neither projected onto the narrow surface nor enumerated
		// for the agent services nor twinned into the tool registry: a person's
		// question about what the daemon exposes is not an agent's question about
		// its own reach. Count +2, measured by this test's own listing.
		MethodSurfaceList, MethodSurfaceGet,
		// S62 T6 (2026-11-02) — the backup family's five doors (BK7, BK12):
		// the scheduled backup's human face (run/list/verify/status/prune).
		// All RequiresInitialize, none Debug, none projected onto the narrow
		// surface (the guard in cmd/aeon/backup_guard_test.go), and none
		// twinned into the tool registry except backup_status (the agent's
		// two read-only tools arrive with the catalog). Count +5, deliberate.
		MethodBackupRun, MethodBackupList, MethodBackupVerify,
		MethodBackupStatus, MethodBackupPrune,
		// S57 T8 (2026-11-02) — the gate plane's twenty-two doors (VG18,
		// CR-TC1): the verify family's twelve (list/show/runs/runShow/
		// vocabulary/templates and the six human-door writes add/update/
		// archive/restore/applyTemplate/try), the merge plane's eight
		// (the rule's get/preview/changes/vocabulary/set/clear plus
		// merge.events and merge.watches riding the same seam) and the
		// DoD's two. All RequiresInitialize, none Debug, none projected
		// onto the narrow surface (the guard in cmd/aeon/verify_amend_test.go),
		// none twinned into the tool registry (the propose-only twins
		// are T10's catalog specs, not this table's entries). Count +22,
		// deliberate, measured by this test's own listing.
		MethodVerifyList, MethodVerifyShow, MethodVerifyRuns, MethodVerifyRunShow,
		MethodVerifyVocabulary, MethodVerifyTemplates,
		MethodVerifyAdd, MethodVerifyUpdate, MethodVerifyArchive, MethodVerifyRestore,
		MethodVerifyApplyTemplate, MethodVerifyTry,
		MethodMergeRuleGet, MethodMergeRulePreview, MethodMergeRuleChanges,
		MethodMergeRuleVocabulary, MethodMergeRuleSet, MethodMergeRuleClear,
		MethodMergeEvents, MethodMergeWatches,
		MethodDoDList, MethodDoDMark,
		// S57 T9 (2026-11-03) — the gate plane's three propose-only
		// twins (VG19): the agent's doors to file the gate kinds'
		// proposals (verify_command add-only, merge_rule
		// tightening-only) and to mark its own loop's DoD ledger.
		// CONDITIONAL on the records seam, exactly as the policy and
		// autonomy twins; each files or marks only its own, inert
		// until a person commits. Count +3, deliberate, measured by
		// this test's own listing.
		MethodVerifyProposeAdd, MethodMergeRuleProposeTighten, MethodDoDMarkAsAgent,

		// S62 T8 (2026-11-02) — the export family's two doors (BK14): the
		// person's write (export.run, the human door, the guard in
		// cmd/aeon/backup_guard_test.go) and the one OPEN read (export.sets —
		// RequiresInitialize false, the registry plus this schema, asserted
		// separately from the gated list). No importer, no narrow projection,
		// no agent service, and the catalog's one read twin is export_sets.
		// Count +2, deliberate.
		MethodExportRun, MethodExportSets,

		// S27 T7 (2026-11-05, CN18): the scheduler plane's twelve doors —
		// four reads (status/preview/decisions/attention) and the eight
		// human-door controls (the master, the pause, the repository
		// template, the one-time tree expansion, the off switch). All
		// RequiresInitialize, none Debug, every control stamped from the
		// connection and none accepting an identity param (CN2). Count
		// +12, deliberate, measured by this test's own listing.
		MethodSchedulerStatus, MethodSchedulerPreview, MethodSchedulerDecisions, MethodSchedulerAttention,
		MethodSchedulerEnable, MethodSchedulerDisable, MethodSchedulerPause, MethodSchedulerResume,
		MethodSchedulerRootSet, MethodSchedulerRootDisable, MethodSchedulerEnableTree, MethodSchedulerStopAll,

		// S47 T7 (TC9/MC2/OL3): the agent's OWN tool view, `tools_mine` and
		// `tool_show` — the two socket twins the catalog specs carry, registered
		// by `registerRecords` from the spec's `Method` field (the `loop.get`
		// shape: one definition, two doors) rather than by an explicit
		// registerer, because their answers come from the daemon's own store,
		// registry and policy set through the records door, and
		// `handleRecord`'s "records are unavailable in this runtime" is the third
		// state. Both are reads, both `RequiresInitialize`, neither `Debug`,
		// neither projected onto the narrow surface nor enumerated for the agent
		// services; a PERSON is refused by name with the unscoped door that does
		// answer them. Count +2, measured by this test's own listing.
		MethodToolsMineList, MethodToolsMineShow,

		// S49 T3 (AN1/AN7) — the "S49 reads" block: the ten unified
		// automation reads over both families (list/show/fires/runs/
		// health/vocabulary/preview/test/dryrun/changes), wired from the
		// stub seam above exactly as the daemon wires them from its
		// adapter, and classified beside their own plane in
		// surface_table_automation.go. None is Debug; none reaches the
		// narrow surface (AN7 — T5 pins it). Count +10, deliberate.
		MethodAutomationList, MethodAutomationShow, MethodAutomationFires, MethodAutomationRuns,
		MethodAutomationHealth, MethodAutomationVocabulary, MethodAutomationPreview,
		MethodAutomationTest, MethodAutomationDryRun, MethodAutomationChanges,
		// S60 T8 (2026-11-04) — the merge-queue plane's ten doors (MQ13,
		// MQ16, MQ17): the four reads and the six human-door writes, all
		// RequiresInitialize, none Debug, the actor from the connection and
		// no state from params; none may reach the narrow projection, the
		// agent services, the tool registry or the records catalog (the
		// guard amendments in cmd/aeon/mergequeue_amend_test.go).
		MethodMergeQueueList, MethodMergeQueueShow, MethodMergeQueueStatus, MethodMergeQueueVocabulary,
		MethodMergeQueueEnqueue, MethodMergeQueueDequeue, MethodMergeQueueReorder,
		MethodMergeQueueRetry, MethodMergeQueueLand, MethodMergeQueueSettings,
		// S63 T5 (2026-11-04, RT10/RT12/RT13) — the outcome-learning
		// plane: five scorecard reads, three retro reads, retro.backfill
		// (the plane's one write, a person's door), digest.learned. All
		// RequiresInitialize, none Debug; none reaches the narrow
		// projection, the agent services, the tool registry or the
		// records catalog (cmd/aeon/retro_guard_test.go). Count +10,
		// deliberate.
		MethodScorecardAgent, MethodScorecardBlueprint, MethodScorecardComponent,
		MethodScorecardCompare, MethodScorecardVocabulary,
		MethodRetroList, MethodRetroShow, MethodRetroStatus, MethodRetroBackfill,
		MethodDigestLearned,
		// S61 T7 (2026-11-05) — the goal-intake plane's fourteen doors
		// (GI8, GI14, GI9..GI13).
		MethodCharterGet, MethodCharterList, MethodCharterRevisions,
		MethodCharterVocabulary, MethodCharterSave,
		MethodTemplateList, MethodTemplateGet, MethodTemplateSave,
		MethodIntakeStart, MethodIntakeList, MethodIntakeStatus,
		MethodIntakePlan, MethodIntakeCommit, MethodIntakeVocabulary,
		// S61 T9 (2026-11-05, GI13): the fifth human door of the plane.
		MethodIntakeAbandon,
		// S61 T8 (2026-11-05) — the two propose twins (GI17). Count +2,
		// deliberate; conditional on the records seam, wired above.
		MethodCharterPropose, MethodIntakePropose,
	}
	sortMethodNames(want)
	if len(got) != len(want) {
		t.Fatalf("methods = %v, want %v — a domain method arrives with its slice (N2)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("methods = %v, want %v", got, want)
		}
	}
}

// S19 mounts the narrowed table on HTTP and stdio. A debug method there would
// be operational control by another name, which the narrow surface exists to
// not have (R6) — so the exclusion is mechanical rather than remembered.

func sortMethodNames(names []MethodName) {
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
}

// mergeQueueTableStub is the S60 T8 zero-state seam: it exists so the ten
// queue doors STAND in the exact-set listing (the registration being
// measured is real, not asserted against nothing); the answers are pinned
// by mergequeue_test.go, the conformance fixtures and the daemon's suite.
type mergeQueueTableStub struct{}

func (mergeQueueTableStub) List(context.Context, MergeQueueListParams) ([]ItemView, error) {
	return nil, nil
}
func (mergeQueueTableStub) Show(context.Context, MergeQueueShowParams) (ShowResult, error) {
	return ShowResult{}, nil
}
func (mergeQueueTableStub) Status(context.Context, MergeQueueStatusParams) (StatusResult, error) {
	return StatusResult{}, nil
}
func (mergeQueueTableStub) Vocabulary(context.Context) (VocabularyResult, error) {
	return VocabularyResult{}, nil
}
func (mergeQueueTableStub) Enqueue(context.Context, Actor, MergeQueueEnqueueParams) (ItemView, error) {
	return ItemView{}, nil
}
func (mergeQueueTableStub) Dequeue(context.Context, Actor, MergeQueueIDParams) (ItemView, error) {
	return ItemView{}, nil
}
func (mergeQueueTableStub) Reorder(context.Context, Actor, MergeQueueReorderParams) (ItemView, error) {
	return ItemView{}, nil
}
func (mergeQueueTableStub) Retry(context.Context, Actor, MergeQueueIDParams) (ItemView, error) {
	return ItemView{}, nil
}
func (mergeQueueTableStub) Land(context.Context, Actor, MergeQueueLandParams) (ItemView, error) {
	return ItemView{}, nil
}
func (mergeQueueTableStub) Settings(context.Context, Actor, MergeQueueSettingsParams) (ConfigView, string, error) {
	return ConfigView{}, "", nil
}

// scorecardTableStub answers the census: wired seam ⇒ registered doors.
type scorecardTableStub struct{}

func (scorecardTableStub) Agent(context.Context, ScorecardAgentParams) (ScorecardCardResult, error) {
	return ScorecardCardResult{}, nil
}
func (scorecardTableStub) Blueprint(context.Context, ScorecardBlueprintParams) (ScorecardCardResult, error) {
	return ScorecardCardResult{}, nil
}
func (scorecardTableStub) Component(context.Context, ScorecardComponentParams) (ScorecardComponentResult, error) {
	return ScorecardComponentResult{}, nil
}
func (scorecardTableStub) Compare(context.Context, ScorecardCompareParams) (ScorecardCompareResult, error) {
	return ScorecardCompareResult{}, nil
}
func (scorecardTableStub) Vocabulary(context.Context) (ScorecardVocabularyResult, error) {
	return ScorecardVocabularyResult{}, nil
}

// retroTableStub likewise.
type retroTableStub struct{}

func (retroTableStub) List(context.Context, RetroListParams) (RetroListResult, error) {
	return RetroListResult{}, nil
}
func (retroTableStub) Show(context.Context, RetroShowParams) (RetroShowResult, error) {
	return RetroShowResult{}, nil
}
func (retroTableStub) Status(context.Context) (RetroStatusResult, error) {
	return RetroStatusResult{}, nil
}
func (retroTableStub) Backfill(context.Context, Actor, RetroBackfillParams) (RetroBackfillWire, error) {
	return RetroBackfillWire{}, nil
}

// digestTableStub likewise.
type digestTableStub struct{}

func (digestTableStub) Learned(context.Context, DigestParams) (DigestResult, error) {
	return DigestResult{}, nil
}

// charterTableStub answers the census: wired seam ⇒ registered doors.
type charterTableStub struct{}

func (charterTableStub) Get(context.Context, CharterGetParams) (CharterView, error) {
	return CharterView{}, nil
}
func (charterTableStub) List(context.Context) (CharterListResult, error) {
	return CharterListResult{}, nil
}
func (charterTableStub) Revisions(context.Context, CharterRevisionsParams) (CharterRevisionsResult, error) {
	return CharterRevisionsResult{}, nil
}
func (charterTableStub) Vocabulary(context.Context) (CharterVocabularyResult, error) {
	return CharterVocabularyResult{}, nil
}
func (charterTableStub) Save(context.Context, Actor, CharterSaveParams) (CharterView, error) {
	return CharterView{}, nil
}

// templateTableStub likewise.
type templateTableStub struct{}

func (templateTableStub) List(context.Context, TemplateListParams) (TemplateListResult, error) {
	return TemplateListResult{}, nil
}
func (templateTableStub) Get(context.Context, TemplateGetParams) (TemplateView, error) {
	return TemplateView{}, nil
}
func (templateTableStub) Save(context.Context, Actor, TemplateSaveParams) (TemplateView, error) {
	return TemplateView{}, nil
}

// intakeTableStub likewise.
type intakeTableStub struct{}

func (intakeTableStub) Start(context.Context, Actor, IntakeStartParams) (IntakeView, error) {
	return IntakeView{}, nil
}
func (intakeTableStub) List(context.Context) (IntakeListResult, error) {
	return IntakeListResult{}, nil
}
func (intakeTableStub) Status(context.Context, IntakeStatusParams) (IntakeStatusView, error) {
	return IntakeStatusView{}, nil
}
func (intakeTableStub) Plan(context.Context, IntakePlanParams) (IntakePreview, error) {
	return IntakePreview{}, nil
}
func (intakeTableStub) Abandon(context.Context, Actor, IntakeAbandonParams) (IntakeAbandonResult, error) {
	return IntakeAbandonResult{}, nil
}
func (intakeTableStub) Commit(context.Context, Actor, IntakeCommitParams) (IntakeCommitResult, error) {
	return IntakeCommitResult{}, nil
}
func (intakeTableStub) Vocabulary(context.Context) (IntakeVocabularyResult, error) {
	return IntakeVocabularyResult{}, nil
}
