package protocol

import (
	"github.com/cjlapao/aeon/internal/asyncworkers"
	"github.com/cjlapao/aeon/internal/graph"
	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/platformview"
	"github.com/cjlapao/aeon/internal/skills"
)

// conformance_types_test.go — the two tables that say which Go type answers
// each method's params and its result (moved out of conformance_test.go for
// SIZE1 in S47 T7, by cohesion: this file is the type table and nothing
// else). It is the half of the conformance suite that GROWS — every slice
// that adds a method adds a row to each switch here, and
// `TestEveryFixtureRoundTripsThroughTheGoTypes` refuses a fixture whose
// fields the Go type does not carry and a Go field the fixtures never show.
// A method missing from these tables is a method whose wire shape nobody has
// checked against the code that produces it.

func paramsTypeFor(m MethodName) any {
	switch m {
	// S25 T4 / S53 T1: the five history doors' request shapes. list/export
	// share one params type (the export IS the list, H3); show keys by id;
	// coverage takes the two scope words; vocabulary takes nothing.
	case MethodHistoryList, MethodHistoryExport:
		return &HistoryListParams{}
	case MethodHistoryShow:
		return &HistoryShowParams{}
	case MethodHistoryCoverage:
		return &HistoryCoverageParams{}
	case MethodHistoryVocabulary:
		return &struct{}{}
	// S58 T5: the channel plane's params (T5 adds them so the fixtures
	// round-trip the real shapes, not just the results).
	case MethodNotifyChannelCreate:
		return &NotifyChannelCreateParams{}
	case MethodNotifyChannelUpdate:
		return &NotifyChannelUpdateParams{}
	case MethodNotifyChannelEnable, MethodNotifyChannelDisable:
		return &NotifyChannelEnableParams{}
	case MethodNotifyChannelArchive:
		return &NotifyChannelArchiveParams{}
	case MethodNotifyChannelTest:
		return &NotifyChannelTestParams{}
	case MethodNotifySubscriptionSet:
		return &NotifySubscriptionSetParams{}
	case MethodNotifyChannelShow:
		return &NotifyChannelShowParams{}
	case MethodNotifyDeliveryList:
		return &NotifyDeliveryListParams{}
	case MethodInitialize:
		return &InitializeParams{}
	case MethodPing:
		return &PingParams{}
	case MethodVersion:
		return &VersionParams{}
	case MethodSubscribe:
		return &SubscribeParams{}
	case MethodUnsubscribe:
		return &UnsubscribeParams{}
	case MethodDebugSubscriptions, MethodDebugGoroutines:
		return &struct{}{}
	case MethodAsk:
		return &AskParams{}
	// S70 T3 (MB7): the mailbox read — one method, read-only.
	case MethodAgentMessages:
		return &AgentMessagesParams{}
	// S30 T5: the graph family's request shapes (componentRoot required on
	// each, T5 §5 — the round-trip proves the fixtures use the real names).
	case MethodGraphExplain:
		return &GraphExplainParams{}
	case MethodGraphPath:
		return &GraphPathParams{}
	case MethodGraphQuery:
		return &GraphQueryParams{}
	case MethodGraphBuild, MethodGraphDelete, MethodGraphStatus:
		return &GraphBuildParams{}
	case MethodGraphReparse:
		return &GraphReparseParams{}
	case MethodGraphRoots:
		return &struct{}{}
	// S33 T8 — the telemetry reads' request shapes (round-trip proves the
	// fixtures use the real parameter names; workspace takes none).
	case MethodTelemetryLoop:
		return &TelemetryLoopParams{}
	case MethodTelemetryComponent:
		return &TelemetryComponentParams{}
	case MethodTelemetryProject:
		return &TelemetryProjectParams{}
	case MethodTelemetryWorkspace:
		return &struct{}{}
	case MethodLoopArchive:
		return &LoopArchiveParams{}
	// S44 T5: the settings verbs — params are protocol's own (the same shape
	// the fixtures pin), the five doors the Settings UI (T8) uses.
	case MethodSettingsList:
		return &SettingsListParams{}
	case MethodSettingsGet:
		return &SettingsGetParams{}
	case MethodSettingsSet:
		return &SettingsSetParams{}
	case MethodSettingsUnset:
		return &SettingsUnsetParams{}
	case MethodSettingsHistory:
		return &SettingsHistoryParams{}
	// S65 T3: the skills doors — params are protocol's own (the same
	// shape the fixtures pin).
	case MethodSkillsList:
		return &SkillsListParams{}
	case MethodSkillsGet:
		return &SkillsGetParams{}
	case MethodSkillsValidate:
		return &SkillsValidateParams{}
	case MethodSkillsInstall:
		return &SkillsInstallParams{}
	case MethodSkillsRemove:
		return &SkillsRemoveParams{}
	case MethodRoutesList:
		return &struct{}{}
	case MethodRoutesSet:
		return &SetRouteParams{}
	case MethodRoutesDelete:
		return &DeleteRouteParams{}
	case MethodLoopStart:
		return &LoopStartParams{}
	case MethodComponentCreate:
		return &ComponentCreateParams{}
	case MethodComponentUpdate:
		return &ComponentUpdateParams{}
	case MethodComponentList:
		return &struct{}{}
	case MethodAutomationTriggerList:
		return &EventTriggerListParams{}
	case MethodAutomationTriggerShow:
		return &EventTriggerShowParams{}
	case MethodAutomationTriggerFires:
		return &EventTriggerFiresParams{}
	case MethodAutomationTriggerCreate:
		return &EventTriggerCreateParams{}
	case MethodAutomationTriggerUpdate:
		return &EventTriggerUpdateParams{}
	case MethodAutomationTriggerEnable:
		return &EventTriggerEnableParams{}
	case MethodAutomationTriggerDisable:
		return &EventTriggerDisableParams{}
	case MethodLoopShow:
		return &LoopShowParams{}
	case MethodLoopAnswer:
		return &LoopAnswerParams{}
	case MethodLoopClose:
		return &LoopCloseParams{}
	case MethodLoopMessage:
		return &LoopMessageParams{}
	case MethodLoopRun:
		return &LoopRunParams{}
	case MethodLoopRunOptions:
		return &LoopRunOptionsParams{}
	case MethodBlueprintShow:
		return &BlueprintShowParams{}
	case MethodBlueprintValidate:
		return &BlueprintValidateParams{}
	// S48 T6: the four write doors' request shapes. None carries an
	// identity member — the actor/origin/rev/version/timestamp key bag is
	// refused by name, so no fixture can carry one (BP12).
	case MethodBlueprintSave:
		return &BlueprintSaveParams{}
	case MethodBlueprintDuplicate:
		return &BlueprintDuplicateParams{}
	case MethodBlueprintDelete:
		return &BlueprintDeleteParams{}
	case MethodBlueprintRevert:
		return &BlueprintRevertParams{}
	case MethodBlueprintFields, MethodBlueprintVocabulary:
		return &struct{}{}
	case MethodBlueprintModel:
		return &BlueprintModelParams{}
	case MethodBlueprintRender:
		return &BlueprintRenderParams{}
	// S48 T7: the read doors' params — list grew {origin?, q?} additively
	// (the filters answer daemon-side), the five new doors each name one
	// thing.
	case MethodBlueprintList:
		return &BlueprintListParams{}
	case MethodBlueprintUsage:
		return &BlueprintUsageParams{}
	case MethodBlueprintHistory:
		return &BlueprintHistoryParams{}
	case MethodBlueprintRevision:
		return &BlueprintRevisionParams{}
	case MethodBlueprintDiff:
		return &BlueprintDiffParams{}
	case MethodBlueprintLint:
		return &BlueprintLintParams{}
	case MethodPolicyList:
		// S45 T2 (PM5): the list's params grew additively — the filters.
		// The zero request stays exactly the pre-S45 read; the fixture
		// policy-list-filtered.json pins the filtered shape here.
		return &PolicyListParams{}
	case MethodPolicyGet:
		// S45 T2 (PM5): the detail read — one id.
		return &PolicyGetParams{}
	case MethodPolicyVocabulary:
		// S45 T2 (PM6): the vocabulary read — no params.
		return &struct{}{}
	case MethodPolicyChanges:
		// S45 T3 (PM8): the authoring audit read — optional filter and
		// limit; the zero request reads the whole audit.
		return &PolicyChangesParams{}
	case MethodPolicyWhy:
		return &PolicyWhyParams{}
	case MethodPolicyInForce:
		return &PolicyInForceParams{}
	case MethodPolicyEvents:
		return &PolicyEventsParams{}
	// S45 T7 (PM9): the propose twins' documented request shapes (the
	// catalog schema owns the strictness; these types make the fixtures
	// round-trip). Neither carries an identity member (OL3).
	case MethodPolicyProposeAdd:
		return &PolicyProposeAddParams{}
	case MethodPolicyProposeRemove:
		return &PolicyProposeRemoveParams{}
	// S45 T4 (PM1): the authoring doors' request shapes — the fixtures pin
	// the human door's params (no actor key anywhere; the identity keys are
	// refused by name, so no fixture can carry one).
	case MethodPolicyAdd:
		return &PolicyAddParams{}
	case MethodPolicyRemove:
		return &PolicyRemoveParams{}
	case MethodPolicySeedCorpus:
		return &SeedCorpusParams{}
	// S24 T7: the queue verbs — params are protocol's own (CG9: socket and
	// CLI are one surface; the fixtures prove the shape on both sides).
	case MethodQueueAppend:
		return &QueueAppendParams{}
	case MethodQueueList:
		return &QueueListParams{}
	case MethodQueueReorder:
		return &QueueReorderParams{}
	case MethodQueueRemove:
		return &QueueRemoveParams{}
	case MethodQueueSetSize:
		return &QueueSetSizeParams{}
	case MethodQueueSettings:
		return &QueueSettingsParams{}
	case MethodQueueAdvance:
		return &QueueAdvanceParams{}
	// S26 T5: the roadmap verbs — params are protocol's own (CG9's
	// extension: socket and CLI are one surface; the fixtures prove the
	// shape on both sides).
	case MethodRoadmapAdd:
		return &RoadmapAddParams{}
	case MethodRoadmapList:
		return &RoadmapListParams{}
	case MethodRoadmapShow:
		return &RoadmapShowParams{}
	case MethodRoadmapCancel:
		return &RoadmapCancelParams{}
	case MethodRoadmapEstimate:
		return &RoadmapEstimateParams{}
	case MethodRoadmapPriority:
		return &RoadmapPriorityParams{}
	case MethodRoadmapIdeate:
		return &RoadmapIdeateParams{}
	case MethodRoadmapPlan:
		return &RoadmapPlanParams{}
	case MethodRoadmapMaterialize:
		return &RoadmapMaterializeParams{}
	// S27 T3: the graph's two edges.
	case MethodRoadmapDepend:
		return &RoadmapDependParams{}
	case MethodRoadmapUndepend:
		return &RoadmapUndependParams{}
	// S28 T4: the learnings verbs — params are protocol's own (P34's
	// ordering: socket and CLI are one surface; the fixtures prove the
	// shape on both sides).
	case MethodLearningsAdd:
		return &LearningsAddParams{}
	case MethodLearningsList:
		return &LearningsListParams{}
	case MethodLearningsShow:
		return &LearningsShowParams{}
	case MethodLearningsQuery:
		return &LearningsQueryParams{}
	case MethodLearningsEmbed:
		return &LearningsEmbedParams{}
	// S52 T2: the management doors. retract/restore answer the act,
	// board/history/vocabulary/forAgent the reads — every param a protocol
	// type, no actor among them (the peer stamps; LM4/LM8).
	case MethodLearningsRetract:
		return &LearningsRetractParams{}
	case MethodLearningsRestore:
		return &LearningsRestoreParams{}
	case MethodLearningsBoard:
		return &LearningsBoardParams{}
	case MethodLearningsHistory:
		return &LearningsHistoryParams{}
	case MethodLearningsVocabulary:
		return &struct{}{}
	case MethodLearningsForAgent:
		return &LearningsForAgentParams{}
	case MethodLoopAccess:
		return &LoopAccessParams{}
	case MethodLoopSetAccess:
		return &LoopSetAccessParams{}
	case MethodLearningsMap:
		return &LearningsMapParams{}
	// S38 T15 (OL10): the five person-only doors carry real Go types —
	// they are protocol's own methods, not catalog projections. The 29
	// projected catalog methods answer nil on purpose: their params are
	// decoded by the records catalog's schemas, not by protocol types, so
	// a second Go type for them would be a second truth that can drift.
	case MethodGoalDecide:
		return &GoalDecideParams{}
	case MethodBriefDecide:
		return &BriefDecideParams{}
	case MethodTaskDecide:
		return &TaskDecideParams{}
	case MethodTaskList:
		return &TaskListParams{}
	case MethodGoalShow:
		return &GoalShowParams{}
	// S46 T3 (MG5-reads): the person's MCP management reads — params are
	// protocol's own (the same shapes the fixtures pin; filters are
	// pointers so absent-vs-empty stays distinct, and no param carries an
	// identity key — rejectIdentityKeys refuses any bag that names one).
	// mcp.vocabulary takes no params (policy.vocabulary's precedent).
	case MethodMCPServers:
		return &MCPServersParams{}
	case MethodMCPServer:
		return &MCPServerParams{}
	case MethodMCPGrants:
		return &MCPGrantsParams{}
	case MethodMCPVocabulary:
		return &struct{}{}
	case MethodMCPChanges:
		return &MCPChangesParams{}
	// S46 T8 (MG8/MG9/MC2): the five AGENT-SCOPED mcp.mine.* twins. The
	// three reads take nothing (the view IS the caller — no param can name
	// an agent; the catalog schema's DisallowUnknownFields is the parse
	// wall); the proposes carry the name(s) and the required reason, and
	// no identity member exists anywhere (OL3 — self is the stamp).
	case MethodMCPMineServers:
		return &MineServersParams{}
	case MethodMCPMineGrants:
		return &MineGrantsParams{}
	case MethodMCPMineStatus:
		return &MineStatusParams{}
	case MethodMCPMineProposeRemove:
		return &MineProposeRemoveParams{}
	case MethodMCPMineProposeRevoke:
		return &MineProposeRevokeParams{}
	// S56 T6: the autonomy human door — ten methods (AL11). The reads carry
	// protocol's own params; vocabulary takes none; the three writes carry
	// no identity field (the actor is the peer, never params).
	case MethodAutonomyList:
		return &AutonomyListParams{}
	case MethodAutonomyShow:
		return &AutonomyShowParams{}
	case MethodAutonomyVocabulary:
		return &struct{}{}
	case MethodAutonomyEffective:
		return &AutonomyEffectiveParams{}
	case MethodAutonomyPinned:
		return &AutonomyPinnedParams{}
	case MethodAutonomyChanges:
		return &AutonomyChangesParams{}
	case MethodAutonomyUses:
		return &AutonomyUsesParams{}
	case MethodAutonomyGrant:
		return &AutonomyGrantParams{}
	case MethodAutonomyDeny:
		return &AutonomyDenyParams{}
	case MethodAutonomyRevoke:
		return &AutonomyRevokeParams{}
	// S56 T8 (AL11, 2026-11-01): the two propose twins' params — the
	// catalog schema's shape (no identity field; the actor is stamped by
	// the service, never carried).
	case MethodAutonomyProposeRevoke:
		return &AutonomyProposeRevokeParams{}
	case MethodAutonomyProposeDeny:
		return &AutonomyProposeDenyParams{}
	// S61 T8 (GI17, 2026-11-05): the goal-intake propose twins' params —
	// the catalog schema's shape, no identity field and NO intakeId (the
	// daemon stamps the row; the strict decode refuses a typed one).
	case MethodCharterPropose:
		return &CharterProposeParams{}
	case MethodIntakePropose:
		return &IntakeProposeParams{}
	// S55 T5 (LC2–LC5, LC15, LC16): the loop-control plane's request shapes —
	// params are protocol's own (the same shape the fixtures pin; the
	// round-trip below is what proves the fixtures use the real names).
	case MethodLoopStop:
		return &LoopStopParams{}
	case MethodLoopResume:
		return &LoopResumeParams{}
	case MethodLoopCancel:
		return &LoopCancelParams{}
	case MethodLoopControl:
		return &LoopControlParams{}
	case MethodLoopRequestStop:
		return &LoopRequestStopParams{}
	case MethodLoopControlRequests:
		return &LoopControlRequestsParams{}
	case MethodLoopControlDismiss:
		return &LoopControlDismissParams{}
	// S55 T8 (2026-11-01): the budget plane's request shapes — params are
	// protocol's own (the same shape the fixtures pin; the round-trip below
	// is what proves the fixtures use the real names).
	case MethodBudgetList:
		return &BudgetListParams{}
	case MethodBudgetShow:
		return &BudgetShowParams{}
	case MethodBudgetEffective:
		return &BudgetEffectiveParams{}
	case MethodSpendReport:
		return &SpendReportParams{}
	case MethodBudgetSet:
		return &BudgetSetParams{}
	case MethodBudgetRemove:
		return &BudgetRemoveParams{}
	case MethodBudgetVocabulary:
		return &struct{}{}
	case MethodLoopRaise:
		return &LoopRaiseParams{}
	// S55 T9 (2026-11-01): the watchdog plane's request shapes — params are
	// protocol's own (the same shape the fixtures pin; the round-trip below
	// is what proves the fixtures use the real names).
	case MethodWatchdogList:
		return &WatchdogListParams{}
	case MethodWatchdogShow:
		return &WatchdogShowParams{}
	case MethodWatchdogVocabulary:
		return &struct{}{}
	case MethodWatchdogSet:
		return &WatchdogSetParams{}
	case MethodWatchdogSetEnabled:
		return &WatchdogSetEnabledParams{}
	case MethodWatchdogRemove:
		return &WatchdogRemoveParams{}
	// S47 T5 (TC10) — the person's tools catalog. The params are FILTERS and a
	// SUBJECT and never a caller identity (rejectIdentityKeys refuses a bag
	// that names an actor — TC9/MC2, this is the human door and an agent's own
	// view is T7's separate path); `tools.status` takes none, because its
	// answer is a function of the compiled build and a filter there would be a
	// request to invent a fact the code already settles.
	case MethodToolsList:
		return &ToolsListFilter{}
	case MethodToolsGet:
		return &ToolsGetParams{}
	case MethodToolsByAgent:
		return &ToolsByAgentParams{}
	case MethodToolsMineList:
		return &ToolsMineParams{}
	case MethodToolsMineShow:
		return &ToolShowParams{}
	case MethodToolsStatus:
		return &struct{}{}
	// S47 T10: the vocabulary door takes no params either, for the same reason
	// `tools.status` takes none — its answer is a function of the compiled build
	// (and of which servers are enabled), and a filter here would ask the door to
	// invent a fact the code already settles.
	case MethodToolsVocabulary:
		return &struct{}{}

	// S47 T6 (TC6/TC8): the surface doors. `surface.list` takes the closed
	// filter (kind · door · q · humanDoorOnly, every one optional and every one
	// answered daemon-side); `surface.get` names one method. The results carry
	// the classification beside the computed doors, so the shapes are
	// SurfaceListResult and SurfaceGetResult and nothing else.
	case MethodSurfaceList:
		return &SurfaceListFilter{}
	case MethodSurfaceGet:
		return &SurfaceGetParams{}

	// S62 T6 (2026-11-02): the backup family's request shapes — the params
	// are the wire table's own (design.md L324–328). `backup.status` takes
	// none: its answer is the machine's state, and a param there would be a
	// request to steer a measurement.
	case MethodBackupRun:
		return &BackupRunParams{}
	case MethodBackupList:
		return &BackupListParams{}
	case MethodBackupVerify:
		return &BackupVerifyParams{}
	case MethodBackupStatus:
		return &struct{}{}
	case MethodBackupPrune:
		return &BackupPruneParams{}

	// S62 T8 (2026-11-02): the export family's request shapes. `export.sets`
	// takes none: its answer is a fact of the binary and the schema, and a
	// param there would be a request to steer a listing.
	case MethodExportRun:
		return &ExportRunParams{}
	case MethodExportSets:
		return &struct{}{}

	// S27 T7 (2026-11-05, CN18): the scheduler plane's request shapes.
	// The four reads take the filter pairs; the controls carry a reason,
	// a template, or NOTHING — every one of them refuses an identity key
	// by name, so no params type here can even hold an actor.
	case MethodSchedulerStatus, MethodSchedulerEnable, MethodSchedulerDisable, MethodSchedulerResume:
		return &struct{}{}
	case MethodSchedulerPreview, MethodSchedulerAttention:
		return &continuousRepoFilterParams{}
	case MethodSchedulerDecisions:
		return &schedulerDecisionsParams{}
	case MethodSchedulerPause, MethodSchedulerStopAll:
		return &schedulerReasonParams{}
	case MethodSchedulerRootSet:
		return &schedulerRootSetParams{}
	case MethodSchedulerRootDisable:
		return &struct {
			ComponentRoot string `json:"componentRoot"`
		}{}
	case MethodSchedulerEnableTree:
		return &schedulerEnableTreeParams{}

	// S57 T8 (2026-11-02): the gate plane's request shapes (VG18, 22 of
	// the slice's methods). The six verify reads, the six human-door
	// writes (the actor is stamped, so no params carry one), the rule's
	// six plus the two merge reads on the same seam, and the DoD's two.
	case MethodVerifyList:
		return &VerifyListParams{}
	case MethodVerifyShow:
		return &VerifyShowParams{}
	case MethodVerifyRuns:
		return &VerifyRunsParams{}
	case MethodVerifyRunShow:
		return &VerifyRunShowParams{}
	case MethodVerifyVocabulary:
		return &struct{}{}
	case MethodVerifyTemplates:
		return &struct{}{}
	case MethodVerifyAdd:
		return &VerifyAddParams{}
	case MethodVerifyUpdate:
		return &VerifyUpdateParams{}
	case MethodVerifyArchive:
		return &VerifyArchiveParams{}
	case MethodVerifyRestore:
		return &VerifyRestoreParams{}
	case MethodVerifyApplyTemplate:
		return &VerifyApplyTemplateParams{}
	case MethodVerifyTry:
		return &VerifyTryParams{}
	case MethodMergeRuleGet:
		return &MergeRuleGetParams{}
	case MethodMergeRulePreview:
		return &MergeRulePreviewParams{}
	case MethodMergeRuleChanges:
		return &MergeRuleChangesParams{}
	case MethodMergeRuleVocabulary:
		return &struct{}{}
	case MethodMergeRuleSet:
		return &MergeRuleSetParams{}
	case MethodMergeRuleClear:
		return &MergeRuleClearParams{}
	case MethodMergeEvents:
		return &MergeEventsParams{}
	case MethodMergeWatches:
		return &MergeWatchesParams{}
	case MethodDoDList:
		return &DoDListParams{}
	case MethodDoDMark:
		return &DoDMarkParams{}

	// S57 T9 (2026-11-03): the three gate twins' documented request
	// shapes (VG19). The two proposes' fields are the schema's field
	// names (the rule's fields ride as a nested object); dod_mark takes
	// the item, the state, the run it names and a note. None carries an
	// identity member (OL3) — TestNoMethodAcceptsAnActorParam holds them
	// with the rest.
	case MethodVerifyProposeAdd:
		return &VerifyProposeAddParams{}
	case MethodMergeRuleProposeTighten:
		return &MergeRuleProposeTightenParams{}
	case MethodDoDMarkAsAgent:
		return &DoDMarkAsAgentParams{}

	// S54 T2 (2026-11-04): the platform plane's three reads. status takes
	// nothing (the panel is the daemon's own platform, there is nothing to
	// scope); plan takes the component root and the NAMED corpus (O8: no
	// default to guess); changes takes the subject filter and a limit the
	// store clamps. None carries an identity member — a read audits
	// nothing (SP6), TestNoMethodAcceptsAnActorParam holds them too.
	case MethodPlatformStatus:
		return &struct{}{}
	case MethodRetrievalReindexPlan:
		return &RetrievalPlanParams{}
	case MethodPlatformChanges:
		return &PlatformChangesParams{}
	}
	return nil
}

func resultTypeFor(m MethodName) any {
	switch m {
	// S25 T4 / S53 T1: the five history doors' result shapes — the merge's
	// rows (with the additive describe block, HV2), the drill-down, the
	// honesty view (HV1) and the closed sets (HV2).
	case MethodHistoryList, MethodHistoryExport:
		return &HistoryListResult{}
	case MethodHistoryShow:
		return &HistoryShowResult{}
	case MethodHistoryCoverage:
		return &history.CoverageView{}
	case MethodHistoryVocabulary:
		return &history.VocabularyView{}
	// S70 T3 (MB7): the mailbox read's shape is its own.
	case MethodAgentMessages:
		return &AgentMessagesResponse{}
	// S58 T2/T3: the inbox reads and the three visibility writes. The
	// writes share one shape (the item as it now reads, or nothing for a
	// no-op) and one Go type.
	case MethodInboxList:
		return &InboxListResult{}
	// S58 T6: the overview — needs-you is literally the summary result
	// type on the wire; the equality is pinned here too.
	case MethodMissionStatus:
		return &MissionStatusResult{}
	case MethodInboxSummary:
		return &InboxSummaryResult{}
	case MethodInboxAck, MethodInboxSnooze, MethodInboxUnack:
		return &InboxAckResult{}
	// S58 T4: the class table's doors (the update answers the row as it
	// now reads; the list the whole table; the vocabulary the plane).
	case MethodNotifyClassList:
		return &NotifyClassListResult{}
	case MethodNotifyClassUpdate:
		return &NotifyClassItem{}
	case MethodNotifyVocabulary:
		return &NotifyVocabularyResult{}
	// S58 T5: the channel plane. The seven writes answer the channel view
	// (or the test's delivery outcome, or the subscription row); the
	// three reads answer the listing, the view and the log.
	case MethodNotifyChannelCreate, MethodNotifyChannelUpdate, MethodNotifyChannelEnable,
		MethodNotifyChannelDisable, MethodNotifyChannelArchive:
		return &NotifyChannelView{}
	case MethodNotifyChannelTest:
		return &NotifyTestResult{}
	case MethodNotifySubscriptionSet:
		return &NotifySubscriptionView{}
	case MethodNotifyChannelList:
		return &NotifyChannelListResult{}
	case MethodNotifyChannelShow:
		return &NotifyChannelView{}
	case MethodNotifyDeliveryList:
		return &NotifyDeliveryListResult{}
	// S48 T6: the write doors' results (save, duplicate and revert share
	// one shape; delete answers its own).
	case MethodBlueprintSave, MethodBlueprintDuplicate, MethodBlueprintRevert:
		return &BlueprintSaveResult{}
	case MethodBlueprintDelete:
		return &BlueprintDeleteResult{}
	case MethodInitialize:
		return &InitializeResult{}
	case MethodPing:
		return &PingResult{}
	case MethodVersion:
		return &VersionResult{}
	case MethodSubscribe:
		return &SubscribeResult{}
	case MethodUnsubscribe:
		return &UnsubscribeResult{}
	// S30 T5: the four-state results are the protocol's wire types —
	// internal/graph defines them once and this projection restates them
	// by reference, the same way search.query answers with internal/search.
	case MethodGraphExplain:
		return &graph.ExplainResult{}
	case MethodGraphPath:
		return &graph.PathResult{}
	case MethodGraphQuery:
		return &graph.QueryResult{}
	// S33 T8 — the telemetry reads answer with the STORE's own shapes (the
	// handlers restate nothing; the fixtures proving they round-trip
	// through these types is what keeps wire and store one shape).
	case MethodTelemetryLoop:
		return &TelemetryLoopData{}
	case MethodTelemetryComponent:
		return &ComponentTelemetry{}
	case MethodTelemetryProject:
		return &ProjectTelemetry{}
	case MethodTelemetryWorkspace:
		return &WorkspaceTelemetry{}
	// S64: graph.build answers with a HANDLE now — the report lives in the
	// ledger row and arrives through asyncworkers.get, because a build is
	// a worker, not a response (the whole point of the slice).
	case MethodGraphBuild:
		return &GraphBuildAccepted{}
	case MethodGraphReparse:
		return &graph.BuildReport{}
	// S64 T3 — the async-worker ledger reads answer with the manager's own
	// wire shapes (the telemetry rule, carried: no restatement, the
	// fixture round-trip IS the shape contract).
	case MethodAsyncWorkersList:
		return &asyncworkers.ListResult{}
	case MethodAsyncWorkersGet, MethodAsyncWorkersCancel:
		return &asyncworkers.View{}
	case MethodGraphDelete:
		return &struct {
			Root         string `json:"root"`
			NodesDeleted int    `json:"nodesDeleted"`
		}{}
	case MethodGraphStatus:
		return &graph.Status{}
	case MethodGraphRoots:
		return &[]string{}
	// S44 T5: the settings verbs' results — the wire twins (SettingView /
	// SettingEventView), the five doors the Settings UI (T8) reads.
	case MethodSettingsList:
		return &SettingsListResult{}
	case MethodSettingsGet:
		return &SettingsGetResult{}
	case MethodSettingsSet:
		return &SettingsSetResult{}
	case MethodSettingsUnset:
		return &SettingsUnsetResult{}
	case MethodSettingsHistory:
		return &SettingsHistoryResult{}
	// S65 T3: the skills doors answer with internal/skills' own wire
	// shapes (the asyncworkers rule, carried: no restatement, the fixture
	// round-trip IS the shape contract).
	case MethodSkillsList:
		return &skills.ScanResult{}
	case MethodSkillsGet:
		return &skills.GetResult{}
	case MethodSkillsValidate:
		return &SkillsValidateResult{}
	case MethodSkillsInstall:
		return &skills.InstallResult{}
	case MethodSkillsRemove:
		return &skills.RemoveResult{}
	case MethodDebugSubscriptions:
		return &DebugSubscriptionsResult{}
	case MethodDebugGoroutines:
		return &DebugGoroutinesResult{}
	case MethodAsk:
		return &AskResult{}
	case MethodRoutesList:
		return &RoutesListResult{}
	// S45 T5 (PM1/PM7): the proposal decision doors' result shapes — the
	// fixtures below are round-tripped through these (the DATA map's
	// entries, added with the fixtures; the round-trip test is the check
	// that the wire shape and the Go shape are one shape).
	case MethodProposalGet:
		return &ProposalGetResult{}
	case MethodProposalCommit:
		return &ProposalCommitResult{}
	case MethodProposalReject:
		return &ProposalRejectResult{}
	// S45 T7 (PM9): the propose twins' result — the stored inert row plus
	// the fixed inertness note, one Go shape for the wire shape. S56 T8
	// (2026-11-01) reuses the SAME shape for the autonomy twins: the
	// inertness sentence is about the proposal machinery, not the policy
	// (PolicyProposeResult / PolicyProposeInertNote, not autonomy twins).
	// S57 T9 (2026-11-03) reuses it for the gate plane's two propose
	// twins (VG19) the same way: the inert row plus the fixed note.
	case MethodPolicyProposeAdd, MethodPolicyProposeRemove,
		MethodAutonomyProposeRevoke, MethodAutonomyProposeDeny,
		// S48 T10 (BP9): the blueprint twin answers the SAME inertness
		// shape — one machinery, one sentence.
		MethodBlueprintPropose, MethodVerifyProposeAdd, MethodMergeRuleProposeTighten,
		// S61 T8 (GI17, 2026-11-05): the goal-intake asks answer the SAME
		// inertness shape — one machinery, one sentence.
		MethodCharterPropose, MethodIntakePropose:
		return &PolicyProposeResult{}
	case MethodRoutesSet:
		return &SetRouteResult{}
	case MethodRoutesDelete:
		return &DeleteRouteResult{}
	case MethodLoopStart, MethodLoopShow, MethodLoopAnswer, MethodLoopClose, MethodLoopMessage,
		MethodLoopRun, MethodLoopRunOptions,
		MethodComponentCreate, MethodComponentUpdate,
		MethodBlueprintFields, MethodBlueprintValidate,
		MethodBlueprintModel, MethodBlueprintRender, MethodBlueprintVocabulary:
		return &map[string]any{}
	// S48 T7: list and show TYPED their answers (additively — names and
	// path:"" stay), and the five read doors arrive typed from the start.
	case MethodBlueprintList:
		return &BlueprintListResult{}
	case MethodBlueprintShow:
		return &BlueprintShowResult{}
	case MethodBlueprintUsage:
		return &BlueprintUsageResult{}
	case MethodBlueprintHistory:
		return &BlueprintHistoryResult{}
	case MethodBlueprintRevision:
		return &BlueprintRevisionResult{}
	case MethodBlueprintDiff:
		return &BlueprintDiffResult{}
	case MethodBlueprintLint:
		return &BlueprintLintResult{}
	case MethodComponentList:
		return &ComponentListResult{}
	case MethodAutomationTriggerList:
		return &EventTriggerListResult{}
	case MethodAutomationTriggerShow:
		return &EventTriggerView{}
	case MethodAutomationTriggerFires:
		return &EventTriggerFiresResult{}
	case MethodAutomationTriggerCreate, MethodAutomationTriggerUpdate,
		MethodAutomationTriggerEnable, MethodAutomationTriggerDisable:
		return &EventTriggerView{}
	case MethodPolicyList:
		return &PolicyListResult{}
	case MethodPolicyWhy:
		return &PolicyWhyResult{}
	case MethodPolicyInForce:
		return &PolicyInForceResult{}
	case MethodPolicyEvents:
		return &PolicyEventsResult{}
	// S45 T2 (PM5/PM6): the detail reads' results — the whole row and the
	// whole vocabulary.
	case MethodPolicyGet:
		return &PolicyGetResult{}
	case MethodPolicyVocabulary:
		return &PolicyVocabularyResult{}
	// S45 T3 (PM8): the authoring audit read's result — the rows, the
	// empty-table statement, and the always-present reconciliation field.
	case MethodPolicyChanges:
		return &PolicyChangesResult{}
	// S45 T4 (PM1): the authoring doors' results — the row with its
	// warnings and before/after in-force views, the remove answer (the row
	// as it was or the stated no-op), and the seed run's counts.
	case MethodPolicyAdd:
		return &PolicyAddResult{}
	case MethodPolicyRemove:
		return &PolicyRemoveResult{}
	case MethodPolicySeedCorpus:
		return &SeedCorpusResult{}
	// S24 T7: the queue verbs' results.
	case MethodQueueAppend, MethodQueueReorder, MethodQueueRemove:
		return &QueueItemResult{}
	case MethodQueueList:
		return &QueueListResult{}
	case MethodQueueSettings:
		return &QueueSettingsResult{}
	case MethodQueueAdvance:
		return &QueueAdvanceResult{}
	// S26 T5: the roadmap verbs' results. The three dispatch doors answer
	// the daemon's own map (seam results ride `any`, webhook's precedent)
	// — nil, so the round-trip check passes them through; the fixture's
	// own shape and the daemon tests carry their contract.
	case MethodRoadmapAdd, MethodRoadmapCancel,
		MethodRoadmapEstimate, MethodRoadmapPriority:
		return &RoadmapItemResult{}
	case MethodRoadmapShow:
		// S50 T6: show answers the item PLUS the daemon's Describe sentence.
		return &RoadmapShowResult{}
	case MethodRoadmapList:
		return &RoadmapListResult{}
	// S28 T4: the learnings verbs' results. add/show answer the item;
	// list and query answer the set — every view carries its provenance and,
	// for an observed learning, its count (M3).
	case MethodLearningsAdd, MethodLearningsShow:
		return &LearningsItemResult{}
	case MethodLearningsList:
		return &LearningsListResult{}
	case MethodLearningsQuery:
		return &LearningsQueryResult{}
	case MethodLearningsEmbed:
		return &LearningsEmbedResult{}
	// S52 T2: retract/restore answer the row and whether the call changed
	// it (LM2's honest idempotence); board rows carry their Describe
	// sentence; forAgent hits carry their why.
	case MethodLearningsRetract, MethodLearningsRestore:
		return &LearningsActResult{}
	case MethodLearningsBoard:
		return &LearningsBoardResult{}
	case MethodLearningsHistory:
		return &LearningsHistoryResult{}
	case MethodLearningsVocabulary:
		return &LearningsVocabularyResult{}
	case MethodLearningsForAgent:
		return &LearningsForAgentResult{}
	case MethodLoopAccess, MethodLoopSetAccess:
		return &LoopAccessView{}
	case MethodLearningsMap:
		return &LearningsMapResult{}
	// S38 T15: the person-only doors' results (the catalog projections'
	// results are their stores' rows — nil, for the reason above).
	case MethodGoalDecide, MethodBriefDecide, MethodTaskDecide:
		return &DecideView{}
	case MethodTaskList:
		return &TaskListResult{}
	case MethodGoalShow:
		return &GoalShowResult{}
	// S46 T3 (MG5-reads/MG7/MG8/MG13): the person's MCP management reads'
	// results — protocol's own wire types (the seam fills them; the
	// fixture round-trip IS the shape contract, the settings/asyncworkers
	// rule carried). mcp.grants' GrantView twins are field-for-field
	// S12's — a second derivation is the bug S46 exists to prevent.
	case MethodMCPServers:
		return &MCPServersResult{}
	case MethodMCPServer:
		return &MCPServerResult{}
	case MethodMCPGrants:
		return &MCPGrantsResult{}
	case MethodMCPVocabulary:
		return &MCPVocabularyResult{}
	case MethodMCPChanges:
		return &MCPChangesResult{}
	// S46 T8: the own-view results. MineGrantsResult reuses MCPGrantView
	// (one view type, two doors); the proposes answer the SAME
	// PolicyProposeResult as every other inert create (the inertness
	// sentence is about the proposal machinery, not the family).
	case MethodMCPMineServers:
		return &MineServersResult{}
	case MethodMCPMineGrants:
		return &MineGrantsResult{}
	case MethodMCPMineStatus:
		return &MineStatusResult{}
	case MethodMCPMineProposeRemove, MethodMCPMineProposeRevoke:
		return &PolicyProposeResult{}
	// S56 T6: the autonomy reads answer protocol's own views (the daemon's
	// wire twins). vocabulary and effective answer their views at TOP LEVEL
	// (no wrapper), exactly as the handlers return them.
	case MethodAutonomyList:
		return &AutonomyListResult{}
	case MethodAutonomyShow:
		return &AutonomyShowResult{}
	case MethodAutonomyVocabulary:
		return &VocabularyView{}
	case MethodAutonomyEffective:
		return &EffectiveView{}
	case MethodAutonomyPinned:
		return &PinnedView{}
	case MethodAutonomyChanges:
		return &AutonomyChangesResult{}
	case MethodAutonomyUses:
		return &AutonomyUsesResult{}
	case MethodAutonomyGrant:
		return &AutonomyGrantResult{}
	case MethodAutonomyDeny:
		return &AutonomyDenyResult{}
	case MethodAutonomyRevoke:
		return &AutonomyRevokeResult{}
	// S55 T5: the loop-control plane's results — the wire twins the
	// fixtures round-trip through (the same no-restatement rule as the
	// telemetry reads: the fixture IS the shape contract).
	case MethodLoopStop:
		return &LoopStopResult{}
	case MethodLoopResume:
		return &LoopResumeResult{}
	case MethodLoopCancel:
		return &LoopCancelResult{}
	case MethodLoopControl:
		return &LoopControlResult{}
	case MethodLoopRequestStop:
		return &RequestStopResult{}
	case MethodLoopControlRequests:
		return &LoopControlRequestsResult{}
	case MethodLoopControlDismiss:
		return &LoopControlDismissResult{}
	// S55 T8: the budget plane's results — the wire twins the fixtures
	// round-trip through (the no-restatement rule carried: the fixture IS
	// the shape contract).
	case MethodBudgetList:
		return &BudgetListResult{}
	case MethodBudgetShow:
		return &BudgetShowResult{}
	case MethodBudgetEffective:
		return &BudgetEffectiveResult{}
	case MethodSpendReport:
		return &SpendReportResult{}
	case MethodBudgetSet:
		return &BudgetSetResult{}
	case MethodBudgetRemove:
		return &BudgetRemoveResult{}
	case MethodBudgetVocabulary:
		return &BudgetVocabularyResult{}
	case MethodLoopRaise:
		return &LoopRaiseResult{}
	// S55 T9 (2026-11-01): the watchdog plane's results — the wire twins the
	// fixtures round-trip through (the no-restatement rule carried: the
	// fixture IS the shape contract).
	case MethodWatchdogList:
		return &WatchdogListResult{}
	case MethodWatchdogShow:
		return &WatchdogShowResult{}
	case MethodWatchdogVocabulary:
		return &WatchdogVocabularyResult{}
	case MethodWatchdogSet:
		return &WatchdogSetResult{}
	case MethodWatchdogSetEnabled:
		return &WatchdogSetEnabledResult{}
	case MethodWatchdogRemove:
		return &WatchdogRemoveResult{}
	// S47 T5 (TC10/TC11): the four catalog answers. `ToolsListResult` carries
	// the table's filtered-empty sentence, `ToolsGetResult` the whole per-agent
	// column (every agent that EXISTS, not every agent that holds) plus the
	// `tool_access` rows that touch the tool with `policy.Describe` verbatim,
	// `ToolsByAgentResult` the engine's own sentence per row, and
	// `ToolsStatusResult` the drift REPORT that TC11 refuses to repair.
	case MethodToolsMineList:
		return &ToolsMineResult{}
	case MethodToolsMineShow:
		return &ToolShowResult{}
	case MethodToolsList:
		return &ToolsListResult{}
	case MethodToolsGet:
		return &ToolsGetResult{}
	case MethodToolsByAgent:
		return &ToolsByAgentResult{}
	case MethodToolsStatus:
		return &ToolsStatusResult{}

	// S47 T10 (TC3): the catalog's own words. `ToolsVocabularyResult` is the one
	// place the capability names arrive WITH the daemon's one-line meanings,
	// beside the family, source and re-run-class spellings a filter may be
	// offered — the answer a screen renders instead of authoring.
	case MethodToolsVocabulary:
		return &ToolsVocabularyResult{}

	// S62 T6 (2026-11-02): the backup family's results — the wire twins the
	// fixtures round-trip through (the no-restatement rule carried: the
	// fixture IS the shape contract, design.md L324–328).
	case MethodBackupRun:
		return &BackupRunResult{}
	case MethodBackupList:
		return &BackupListResult{}
	case MethodBackupVerify:
		return &BackupVerifyResult{}
	case MethodBackupStatus:
		return &BackupStatusView{}
	case MethodBackupPrune:
		return &BackupPruneView{}

	// S62 T8 (2026-11-02): the export family's results — the wire twins the
	// fixtures round-trip through (Rows is a pointer, so the fixtures' null
	// for an unavailable set round-trips to a nil pointer, never a zero).
	case MethodExportRun:
		return &ExportRunResult{}
	case MethodExportSets:
		return &ExportSetsResult{}

	// S57 T8 (2026-11-02): the gate plane's result shapes (VG18) — the
	// wire twins the fixtures round-trip through.
	case MethodVerifyList:
		return &VerifyListResult{}
	case MethodVerifyShow:
		return &VerifyShowResult{}
	case MethodVerifyRuns:
		return &VerifyRunsResult{}
	case MethodVerifyRunShow:
		return &VerifyRunShowResult{}
	case MethodVerifyVocabulary:
		return &VerifyVocabularyResult{}
	case MethodVerifyTemplates:
		return &VerifyTemplatesResult{}
	case MethodVerifyAdd:
		return &VerifyWriteResult{}
	case MethodVerifyUpdate:
		return &VerifyWriteResult{}
	case MethodVerifyArchive:
		return &VerifyWriteResult{}
	case MethodVerifyRestore:
		return &VerifyWriteResult{}
	case MethodVerifyApplyTemplate:
		return &VerifyApplyResult{}
	case MethodVerifyTry:
		return &VerifyTryResult{}
	case MethodMergeRuleGet:
		return &MergeRuleGetResult{}
	case MethodMergeRulePreview:
		return &MergeRulePreviewResult{}
	case MethodMergeRuleChanges:
		return &MergeRuleChangesResult{}
	case MethodMergeRuleVocabulary:
		return &MergeRuleVocabularyResult{}
	case MethodMergeRuleSet:
		return &MergeRuleSetResult{}
	case MethodMergeRuleClear:
		return &MergeRuleSetResult{}
	case MethodMergeEvents:
		return &MergeEventsResult{}
	case MethodMergeWatches:
		return &MergeWatchesResult{}
	case MethodDoDList:
		return &DoDListResult{}
	case MethodDoDMark, MethodDoDMarkAsAgent:
		return &DoDWriteResult{}

	// S27 T7 (2026-11-05, CN18): the scheduler plane's answers — the
	// wire twins the fixtures round-trip through (the status view is the
	// descriptor's home; the controls answer their one fact plus CN18's
	// honest afterwords).
	case MethodSchedulerStatus:
		return &ContinuousStatusView{}
	case MethodSchedulerPreview:
		return &ContinuousPreview{}
	case MethodSchedulerDecisions:
		return &[]ContinuousDecisionRow{}
	case MethodSchedulerAttention:
		return &ContinuousAttention{}
	case MethodSchedulerEnable:
		return &struct {
			Enabled bool `json:"enabled"`
		}{}
	case MethodSchedulerDisable:
		return &struct {
			Enabled bool `json:"enabled"`
		}{}
	case MethodSchedulerPause:
		return &struct {
			Paused       bool   `json:"paused"`
			RunningLoops string `json:"runningLoops"`
		}{}
	case MethodSchedulerResume:
		return &struct {
			Paused bool `json:"paused"`
		}{}
	case MethodSchedulerRootSet:
		return &ContinuousRepoView{}
	case MethodSchedulerRootDisable:
		return &struct {
			ComponentRoot string `json:"componentRoot"`
			Enabled       bool   `json:"enabled"`
		}{}
	case MethodSchedulerEnableTree:
		return &struct {
			Roots []string `json:"roots"`
			Count int      `json:"count"`
			Note  string   `json:"note"`
		}{}
	case MethodSchedulerStopAll:
		return &ContinuousStopAllResult{}

	// S54 T2 (2026-11-04): the platform plane's answers. The panel shape is
	// platformview's own (the composer owns the shape; protocol handlers
	// never reshape — the fixture round-trips the type that actually
	// rides the wire). plan answers the priced preview; changes answers
	// the ledger's rows newest-first.
	case MethodPlatformStatus:
		return &platformview.StatusView{}
	case MethodRetrievalReindexPlan:
		return &RetrievalPlanAnswer{}
	case MethodPlatformChanges:
		return &[]PlatformChangeView{}
	}
	return nil
}
