package protocol

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/platformview"
	"github.com/cjlapao/aeon/internal/records"
)

// S38 T15 (OL10): the parity that makes "one definition, two doors" a fact
// rather than a slogan — walked in BOTH directions every run.

func TestEveryCatalogActionHasAMethodAndViceVersa(t *testing.T) {
	tbl := newTestTable(t)
	// The seed tools (family "seed") are registered by RegisterSeed, not
	// registerRecords: their method ("seed.status") is the human door's,
	// answered by the seed handler. A seedless table legitimately does not
	// register them, so wire a fake seed engine to make the forward check
	// whole (the catalog keeps the spec for the agent tool projection).
	tbl.Seed = &seedFake{}
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused a wired seam")
	}
	// S45 T7 (PM9): the two propose twins are registered by RegisterPolicy
	// Propose, conditional on the records service (the seed rule for the
	// catalog's first mutating specs) — wire the stub so the forward walk
	// measures every Spec, the propose rows included. The absent side is
	// policy_propose_test.go's.
	tbl.Records = recordsListingStub{}
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused a wired records seam")
	}
	// S56 T8 (2026-11-01): the autonomy family's five names are registered
	// by T6's RegisterAutonomy (the three reads) and RegisterAutonomyPropose
	// (the two propose twins) — both conditional, both wired here so the
	// forward walk measures every Spec (the absent sides are pinned by
	// their own conditional-registration tests).
	tbl.Autonomy = &autonomyFake{}
	tbl.RegisterAutonomy()
	if !tbl.RegisterAutonomyPropose() {
		t.Fatal("RegisterAutonomyPropose refused a wired records seam")
	}
	// S46 T8 (2026-11-02): the mcp family's five forward twins are owned by
	// RegisterMCPMine, conditional on the records stub (wired above) AND the
	// MCP admin seam — wire a stand-in and register, so the forward walk
	// measures every Spec (the absent side is TestMcpAdminAbsentWhenSeamNil's).
	tbl.MCPAdmin = func(_ context.Context, _ string, _ json.RawMessage) (any, string, error) {
		return struct{}{}, "", nil
	}
	if !tbl.RegisterMCPMine() {
		t.Fatal("RegisterMCPMine refused the wired seams — the forward walk would miss the mcp family")
	}
	// The S55 T10 loop-control / budget / watchdog specs (families
	// "loopcontrol" / "budget" / "watchdog") twin to the planes' socket
	// doors, registered by their own Register* over their own seams — the
	// same zero-value fakes the handshake exact-set test wires: the doors
	// only inspect nil-ness at registration, the forward walk needs the
	// METHODS present, not the answers.
	tbl.LoopControl = testLoopControlService{}
	if !tbl.RegisterLoopControl() {
		t.Fatal("RegisterLoopControl refused a wired seam")
	}
	tbl.BudgetStore = testBudgetService{}
	if !tbl.RegisterBudget() {
		t.Fatal("RegisterBudget refused a wired seam")
	}
	tbl.Watchdog = testWatchdogService{}
	if !tbl.RegisterWatchdog() {
		t.Fatal("RegisterWatchdog refused a wired seam")
	}
	// S62 T6 (2026-11-02): the backup family's read (backup_status, method
	// backup.status) is owned by RegisterBackup over the Backup seam — the
	// same zero-value fake the handshake exact-set test wires: the forward
	// walk needs the METHODS present, not the answers (the absent side is
	// TestBackupMethodsAbsentWhenServiceNil's).
	tbl.Backup = testBackupService{}
	if !tbl.RegisterBackup() {
		t.Fatal("RegisterBackup refused a wired seam")
	}
	// S62 T8 (2026-11-02): the export family's read twin (export_sets,
	// method export.sets) is owned by RegisterExport over the Export seam —
	// the same zero-value fake; the forward walk needs the METHODS present,
	// not the answers.
	tbl.Export = testExportService{}
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a wired seam")
	}
	// S48 T10 (2026-11-03, BP9): the blueprint family's five names are
	// owned by RegisterBlueprintReads (the T7 doors — the fake the
	// conditional-registration tests already use; registration inspects
	// only nil-ness) and RegisterBlueprintPropose (conditional on the
	// records stub wired above). Both stand here so the forward walk
	// measures every Spec; the absent sides are blueprint_manage_test.go's
	// and blueprint_propose_test.go's.
	tbl.BlueprintReads = &fakeBlueprintReads{}
	if !tbl.RegisterBlueprintReads() {
		t.Fatal("RegisterBlueprintReads refused a wired seam")
	}
	if !tbl.RegisterBlueprintPropose() {
		t.Fatal("RegisterBlueprintPropose refused a wired records seam")
	}
	// S57 T9 (2026-11-03): the gate family's nine specs twin to the
	// gate plane's doors — the six reads owned by T8's three registers
	// over their own seams, and the three propose-only twins owned by
	// RegisterGatePropose over the records stub (wired above) — the
	// same zero-value fakes the handshake exact-set test wires: the
	// forward walk needs the METHODS present, not the answers (the
	// absent sides are pinned by their own conditional tests).
	tbl.Verify = testVerifyService{}
	tbl.RegisterVerify()
	tbl.MergeRule = testMergeRuleService{}
	tbl.RegisterMergeRule()
	tbl.DoD = testDoDService{}
	tbl.RegisterDoD()
	if !tbl.RegisterGatePropose() {
		t.Fatal("RegisterGatePropose refused a wired records seam")
	}
	// S58 T6 (2026-11-03): the supervision family's five reads — the five
	// safe door-reads the person's surfaces also ride (the records skip in
	// daemon-facing records.go keeps the PROJECTION from claiming these
	// names; the plane registrars own them, V13). The same zero-value
	// fakes mission_test.go parity uses: the forward walk needs the
	// METHODS present, not the answers (the absent sides are inbox_test.go's
	// storeless check and mission_test.go's TestMissionAbsentWithoutAStore).
	tbl.Inbox = supervInboxStub{}
	if !tbl.RegisterInbox() {
		t.Fatal("RegisterInbox refused a wired seam")
	}
	tbl.Mission = supervMissionStub{}
	if !tbl.RegisterMission() {
		t.Fatal("RegisterMission refused a wired seam")
	}
	tbl.NotifyClass = supervClassStub{}
	if !tbl.RegisterNotifyClasses() {
		t.Fatal("RegisterNotifyClasses refused a wired seam")
	}
	tbl.NotifyChannel = supervChannelStub{}
	if !tbl.RegisterNotifyChannels() {
		t.Fatal("RegisterNotifyChannels refused a wired seam")
	}
	// S59 T7 (2026-11-03, EV16/EV17): the environment family's five names are
	// owned by RegisterEnvironment and RegisterSecretRef over the plane's own
	// seams (the same fakes environment_conformance_test.go drives —
	// registration inspects only nil-ness, and the forward walk needs the
	// METHODS present, not the answers), and the propose twin by
	// RegisterEnvironmentProposeRestrict over the records stub wired above.
	// All three stand here so the forward walk measures every Spec; the
	// absent sides are environment_test.go's and environment_propose_test.go's.
	// registerRecords skips the whole `environment` family, so nothing here
	// can double-register.
	tbl.Environment = envSeam{}
	tbl.SecretRefs = refSeam{}
	if !tbl.RegisterEnvironment() || !tbl.RegisterSecretRef() {
		t.Fatal("the execution-environment plane refused its seams — the four read twins would have no door")
	}
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("RegisterEnvironmentProposeRestrict refused a wired records seam")
	}
	// S49 T7 (2026-11-04, AN9): the automation family's five names are owned
	// by RegisterAutomationReads over the plane's own seam (the same stub
	// method_table_test.go drives — registration inspects only nil-ness) and
	// the propose twin by RegisterAutomationProposeDisable over the records
	// stub wired above. registerRecords skips the whole `automation` family,
	// so nothing here can double-register.
	tbl.AutomationReads = automationReadsStub{}
	if !tbl.RegisterAutomationReads() {
		t.Fatal("RegisterAutomationReads refused a wired seam — the four read twins would have no door")
	}
	if !tbl.RegisterAutomationProposeDisable() {
		t.Fatal("RegisterAutomationProposeDisable refused a wired records seam")
	}
	// S50 T5 (2026-11-04, RM8 — the S49 shape carried): the roadmap family's
	// three names skip registerRecords (the family's read twins are doors
	// registerRoadmap() already opened, and the propose twin must not stand
	// unconditionally); the propose door is registered here over the same
	// records stub, and the read twins' methods already stand from
	// NewTable's registerRoadmap.
	if !tbl.RegisterRoadmapProposeIdea() {
		t.Fatal("RegisterRoadmapProposeIdea refused a wired records seam — the propose twin would have no door")
	}
	// S51 T5 (TM6): the bug_file twin rides the same records stub — the
	// bug family skips NewTable's projection (records.go), so without this
	// the filing spec would name a door nobody opened.
	if !tbl.RegisterTriageFileAs() {
		t.Fatal("RegisterTriageFileAs refused a wired records seam — the filing twin would have no door")
	}
	// S52 T4 (LM5, LM7): the memory four ride the same records stub — the
	// memory family skips NewTable's projection (records.go), so without
	// this the four specs would name doors nobody opened.
	if !tbl.RegisterLearningsMine() {
		t.Fatal("RegisterLearningsMine refused a wired records seam — the agent's own memory doors would have no socket half")
	}
	// S60 T9 (2026-11-04, MQ17): the mergequeue family's five names ride the
	// plane's own door service (RegisterMergeQueue over the same
	// mqConformanceStub its conformance file drives — registration inspects
	// only nil-ness, the forward walk needs the METHODS present, not the
	// answers) and the ask twin over the records stub wired above.
	// registerRecords skips the whole `mergequeue` family, so nothing here
	// can double-register.
	tbl.MergeQueue = &mqConformanceStub{calls: map[MethodName]int{}}
	if !tbl.RegisterMergeQueue() {
		t.Fatal("RegisterMergeQueue refused a wired seam — the queue's read twins would have no door")
	}
	if !tbl.RegisterMergeQueueProposeEnqueue() {
		t.Fatal("RegisterMergeQueueProposeEnqueue refused a wired records seam — the ask would name a door nobody opened")
	}
	// S54 T4 (SP4): the platform family's one read (index_status, method
	// platform.status) is owned by RegisterPlatformStatus over the composed
	// adapter — the same stub platform_test.go drives (registration inspects
	// only nil-ness; the forward walk needs the METHOD present, not the
	// panel's answer; the absent side is TestPlatformStatusAbsentWhenSeamNil).
	// registerRecords skips the whole `platform` family, so nothing here can
	// double-register.
	tbl.PlatformStatus = func(context.Context) (*platformview.StatusView, error) {
		return &platformview.StatusView{}, nil
	}
	if !tbl.RegisterPlatformStatus() {
		t.Fatal("RegisterPlatformStatus refused a wired seam — the freshness twin would have no door")
	}
	// S63 T6 (RT11): the scorecard family's three ride the plane's own
	// doors — the family skips NewTable's projection (records.go), and the
	// three Methods ARE T5's human doors (scorecard.agent · .component,
	// digest.learned) over the plane seams, the mergequeue shape. Without
	// this wiring the three specs would name doors nobody opened; with it
	// registerRecords skips the whole family, so nothing can double-register.
	tbl.Scorecard = &planeConfStub{calls: map[MethodName]int{}}
	tbl.Digest = tbl.Scorecard.(DigestService)
	if !tbl.RegisterScorecard() || !tbl.RegisterDigest() {
		t.Fatal("RegisterScorecard/RegisterDigest refused a wired seam — the three read twins would have no door")
	}
	// S27 T8 (CN23): the scheduler family's four reads ride the plane's own
	// doors — the family skips NewTable's projection (records.go), and the
	// four Methods ARE T7's human doors (scheduler.status · .preview ·
	// .decisions · .attention, beside the eight controls) over the
	// Continuous seam, the mergequeue shape. continuousFake is T7's own
	// seam from continuous_test.go (registration inspects only nil-ness;
	// the forward walk needs the METHODS present, not the answers). Without
	// this wiring the four specs would name doors nobody opened; with it
	// registerRecords skips the whole family, so nothing can double-register.
	tbl.Continuous = &continuousFake{}
	if !tbl.RegisterContinuous() {
		t.Fatal("RegisterContinuous refused a wired seam — the scheduler's four read twins would have no door")
	}
	// S61 T8 (2026-11-05, GI17): the goal-intake family's seven names are
	// owned by T7's three registers over the plane's own seams (the same
	// stubs the handshake exact-set test wires — registration inspects only
	// nil-ness, the forward walk needs the METHODS present, not the
	// answers) and by RegisterIntakePlanePropose over the records stub
	// wired above. registerRecords skips the whole `intake` family, so
	// nothing here can double-register (the absent side is
	// intake_s61_test.go's storeless check).
	tbl.Charter = charterTableStub{}
	if !tbl.RegisterCharter() {
		t.Fatal("RegisterCharter refused a wired seam — charter_list and charter_show would have no door")
	}
	tbl.Templates = templateTableStub{}
	if !tbl.RegisterTemplates() {
		t.Fatal("RegisterTemplates refused a wired seam — template_list and template_show would have no door")
	}
	tbl.Intake = intakeTableStub{}
	if !tbl.RegisterIntake() {
		t.Fatal("RegisterIntake refused a wired seam — intake_status would have no door")
	}
	if !tbl.RegisterIntakePlanePropose() {
		t.Fatal("RegisterIntakePlanePropose refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	// Forward: every Spec's dot-name twin stands as a method. A Spec whose
	// door nobody opened is a tool the app cannot reach — exactly the drift
	// the catalog's Method field exists to prevent.
	for _, sp := range records.Catalog() {
		if !registered[MethodName(sp.Method)] {
			t.Errorf("catalog action %q names method %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
	}
	// Backward: no decision is a catalog action (a review is a human act,
	// OL11), and no decide method exists that the catalog did not author.
	for _, sp := range records.Catalog() {
		if strings.Contains(sp.Name, "decide") || strings.HasSuffix(sp.Method, ".decide") {
			t.Errorf("catalog action %q (%s) is a decision — decides are never tools (OL11)", sp.Name, sp.Method)
		}
	}
	decides := map[MethodName]bool{MethodGoalDecide: true, MethodBriefDecide: true, MethodTaskDecide: true}
	for m := range registered {
		if strings.HasSuffix(string(m), ".decide") && !decides[m] {
			t.Errorf("%s is a decide-shaped method nobody declared — decide doors are three, named, and only these", m)
		}
	}
}

// TestDecideAndReadParamsCarryNoActorField is the params-shape half of OL3:
// an actor has nowhere to be carried, so a caller cannot pick its own kind —
// and forward-compat (T3) means an unknown field is ignored, which is why
// the assertion is on the TYPES having no destination for one.
func TestDecideAndReadParamsCarryNoActorField(t *testing.T) {
	for _, v := range []any{GoalDecideParams{}, BriefDecideParams{}, TaskDecideParams{}, TaskListParams{}, GoalShowParams{}} {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			tag := strings.ToLower(rt.Field(i).Tag.Get("json"))
			if strings.Contains(tag, "actor") || strings.Contains(tag, "by") || strings.Contains(tag, "decider") {
				t.Errorf("%T carries %q — identity is stamped by the daemon, never decoded from params (OL3)", v, tag)
			}
		}
	}
}

// TestDeriveExecutionStateIsTheWholeLadder walks the pure derivation — the
// five words criterion 10 names, including the blocked branch the daemon
// today reaches through the runner's own "blocked".
func TestDeriveExecutionStateIsTheWholeLadder(t *testing.T) {
	ls := func(statuses ...string) []LoopStatusView {
		out := make([]LoopStatusView, 0, len(statuses))
		for i, s := range statuses {
			out = append(out, LoopStatusView{ID: string(rune('a' + i)), Slug: "l", Status: s})
		}
		return out
	}
	cases := []struct {
		name  string
		loops []LoopStatusView
		want  string
	}{
		{"no loops is not_started", nil, "not_started"},
		{"pending is not_started", ls("pending", "pending"), "not_started"},
		{"any running wins", ls("done", "running"), "running"},
		{"waiting with nothing running is blocked", ls("done", "waiting"), "blocked"},
		{"the runner's blocked is blocked", ls("blocked"), "blocked"},
		{"running outranks blocked", ls("blocked", "running"), "running"},
		{"all done is done", ls("done", "done"), "done"},
		{"one aborted among done is failed", ls("done", "aborted"), "failed"},
		{"one failed among done is failed", ls("failed", "done"), "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeriveExecutionState(c.loops); got != c.want {
				t.Errorf("DeriveExecutionState = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRecordsParityPolicyFamily is S45 T7 by name: the two-direction walk
// over the two families the slice opened. Forward — every policy/proposal
// Spec's dot-name twin stands (the five reads reuse the standing methods,
// the propose twins stand through RegisterPolicyPropose). Backward — the
// family holds exactly two mutating specs and they are the propose twins,
// and no spec of either family is twinned to a decision or authoring
// method: an agent sees and proposes, never decides (PM9).
func TestRecordsParityPolicyFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	if !tbl.RegisterPolicyPropose() {
		t.Fatal("RegisterPolicyPropose refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The constants, never dotted literals: TestPolicyAuthoringNames
		// StayConfined (this package) confines those spellings to
		// method.go — this test names the same doors through the one
		// sanctioned spelling.
		string(MethodProposalCommit):   true,
		string(MethodProposalReject):   true,
		string(MethodProposalDiscard):  true,
		string(MethodPolicyAdd):        true,
		string(MethodPolicyRemove):     true,
		string(MethodPolicySeedCorpus): true,
	}
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "policy" && sp.Family != "proposal" {
			continue
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no decision or authoring method reaches an agent through the catalog (PM9)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Method != "policy.proposeAdd" && sp.Method != "policy.proposeRemove" {
				t.Errorf("%s mutates and is not a propose twin — an agent may PROPOSE, never decide, add, remove or seed", sp.Name)
			}
		}
	}
	if mutators != 2 {
		t.Errorf("%d mutating policy/proposal specs, want exactly 2 (the propose twins)", mutators)
	}
}

// TestRecordsParityAutonomyFamily is S56 T8 by name: the two-direction walk
// over the autonomy family. Forward — every autonomy Spec's dot-name twin
// stands (the three reads reuse T6's standing methods, the propose twins
// stand through RegisterAutonomyPropose). Backward — the family holds
// exactly two mutating specs and they are the propose twins, and no spec is
// twinned to a grant write: the agent sees and proposes NARROWING, never
// widens (AL11).
func TestRecordsParityAutonomyFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	tbl.Autonomy = &autonomyFake{}
	tbl.RegisterAutonomy()
	if !tbl.RegisterAutonomyPropose() {
		t.Fatal("RegisterAutonomyPropose refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The three person-only writes, by their one sanctioned spelling:
		// a spec twinned to any of them would be an agent door onto a
		// widening write (AL11's lock at the catalog level).
		string(MethodAutonomyGrant):  true,
		string(MethodAutonomyDeny):   true,
		string(MethodAutonomyRevoke): true,
	}
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "autonomy" {
			continue
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no grant write reaches an agent through the catalog (AL11)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Method != string(MethodAutonomyProposeRevoke) && sp.Method != string(MethodAutonomyProposeDeny) {
				t.Errorf("%s mutates and is not a propose twin — an agent may PROPOSE narrowing, never grant", sp.Name)
			}
		}
	}
	if mutators != 2 {
		t.Errorf("%d mutating autonomy specs, want exactly 2 (the propose twins)", mutators)
	}
}

// TestRecordsParityMcpFamily is S46 T8 by name: the two-direction walk over
// the mcp family. Forward — every mcp Spec's mcp.mine.* twin stands once the
// records stub AND the admin seam are wired (RegisterMCPMine). Backward —
// the family holds exactly two mutating specs and they are the two
// narrowing proposes, and NO spec is twinned to a config write: the agent
// sees and proposes NARROWING, never adds, updates, enables, declares,
// cores or imports (MG9's lock at the catalog level, the AL11 pattern).
func TestRecordsParityMcpFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	tbl.MCPAdmin = func(_ context.Context, _ string, _ json.RawMessage) (any, string, error) {
		return struct{}{}, "", nil
	}
	if !tbl.RegisterMCPMine() {
		t.Fatal("RegisterMCPMine refused the wired seams")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		string(MethodMCPServerAdd):        true,
		string(MethodMCPServerUpdate):     true,
		string(MethodMCPServerRemove):     true,
		string(MethodMCPServerSetEnabled): true,
		string(MethodMCPServerShare):      true,
		string(MethodMCPGrantDeclare):     true,
		string(MethodMCPGrantRevoke):      true,
		string(MethodMCPCoreSet):          true,
		string(MethodMCPImportFile):       true,
	}
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "mcp" {
			continue
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no config write reaches an agent through the catalog (MG9)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Method != string(MethodMCPMineProposeRemove) && sp.Method != string(MethodMCPMineProposeRevoke) {
				t.Errorf("%s mutates and is not a narrowing propose — agents PROPOSE, they never write", sp.Name)
			}
		}
	}
	if mutators != 2 {
		t.Errorf("%d mutating mcp specs, want exactly 2 (the two narrowing proposes)", mutators)
	}
}

// TestRecordsParityBackupFamily is S62 T6 by name: the two-direction walk over
// the backup family — the family whose one read spec must match its wire
// method field for field, because a tool input is a promise about what the
// method will honour.
//
// Forward (spec → wire): the spec exists, is read-only and safe, its
// dot-name twin backup.status stands on the wired table, and every input
// field the spec DECLARES is a field the wire method accepts. The method
// accepts nothing (its decode target is an empty struct — the machine is a
// fact of the daemon, not of the caller), so the spec must declare nothing:
// a field the tool promises that the method ignores is a tool that lies
// about its input.
//
// Backward (wire → spec): the family holds EXACTLY one spec at T6 (T8 adds
// export_sets and updates this count), nothing mutates, and no spec is
// twinned to a human door — run, verify and prune are person-only, and a
// catalog entry for any of them would be an agent door onto a write the
// socket's human half alone carries (BK12). restore is never a spec at all
// (BK9: the CLI's direct door only).
func TestRecordsParityBackupFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Backup = testBackupService{}
	if !tbl.RegisterBackup() {
		t.Fatal("RegisterBackup refused a wired seam — the read twin has no method to stand on")
	}
	// S62 T8 (2026-11-02): the export read twin stands on the Export seam.
	tbl.Export = testExportService{}
	if !tbl.RegisterExport() {
		t.Fatal("RegisterExport refused a wired seam — the export read twin has no method to stand on")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		string(MethodBackupRun):    true,
		string(MethodBackupVerify): true,
		string(MethodBackupPrune):  true,
		string(MethodExportRun):    true, // S62 T8: the export write is a human door too
	}

	var spec *records.Spec
	count := 0
	seen := []string{} // the family's spec names, in catalog order
	catalog := records.Catalog()
	for i := range catalog {
		sp := catalog[i]
		if sp.Family != "backup" {
			continue
		}
		count++
		seen = append(seen, sp.Name)
		if sp.Mutates {
			t.Errorf("%s mutates — the backup family's entries at T6 are READS; the writes are human doors and never tools (BK12)", sp.Name)
		}
		if sp.Class != "safe" {
			t.Errorf("%s is class %q, want safe — a read is a read (a crash may re-run it)", sp.Name, sp.Class)
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no human door reaches an agent through the catalog (BK12)", sp.Name, sp.Method)
		}
		spec = &catalog[i]
	}
	// S62 T8 (2026-11-02): the family holds EXACTLY two specs — T6's
	// backup_status plus the export's read twin export_sets (the count T6's
	// comment promised). The loop left the LAST one in `spec`; the names are
	// asserted as a set, in catalog order.
	if count != 2 {
		t.Fatalf("the backup family holds %d specs, want exactly 2 at T8 (backup_status at T6 plus export_sets at T8)", count)
	}
	if want := []string{"backup_status", "export_sets"}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("the family's two specs are %v in catalog order, want %v", seen, want)
	}
	if spec.Name != "export_sets" || spec.Method != string(MethodExportSets) {
		t.Fatalf("the family's second spec is %s on %q, want export_sets on %s", spec.Name, spec.Method, MethodExportSets)
	}

	// The field-for-field half, forward, for BOTH specs: each spec's declared
	// input properties vs its wire method's accepted fields. Each handler
	// decodes an empty struct, so the accepted set is empty — a declared
	// property is a lie either way the drift goes.
	initialized(t, tbl, "c-backup-parity", PROTOCOL_VERSION)
	for i := range catalog {
		sp := catalog[i]
		if sp.Family != "backup" {
			continue
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &schema); err != nil {
			t.Fatalf("%s: the spec's schema does not parse: %v", sp.Name, err)
		}
		if schema.Properties == nil {
			t.Fatalf("%s declares no properties block at all — the input shape is an empty object, and an empty object is a shape", sp.Name)
		}
		for name := range schema.Properties {
			t.Errorf("%s declares an input field %q that %s does not accept — the method decodes an empty struct (the machine is a fact of the daemon, not of the caller); a field the tool promises the wire ignores is a tool that lies about its input", sp.Name, name, sp.Method)
		}
		// ...and backward: the spec's input (its required fields, none) must
		// round-trip the wire as-is — dispatch the twin with the spec's
		// shape and take the view its method actually answers.
		res, err := tbl.Dispatch(context.Background(), "c-backup-parity", MethodName(sp.Method), []byte(`{}`))
		if err != nil {
			t.Fatalf("%s refused the spec's own input shape {}: %v", sp.Method, err)
		}
		switch sp.Method {
		case string(MethodBackupStatus):
			if _, ok := res.(BackupStatusView); !ok {
				t.Fatalf("backup.status answered %T to the spec's shape, want the status view", res)
			}
		case string(MethodExportSets):
			if _, ok := res.(ExportSetsResult); !ok {
				t.Fatalf("export.sets answered %T to the spec's shape, want the sets result", res)
			}
		default:
			t.Fatalf("the backup family's spec %s twins to a method this test does not know the view of", sp.Name)
		}
	}
}

// TestRecordsParityBlueprintFamily is S48 T10 (BP9) by name: the
// two-direction walk over the blueprint family. Forward — every blueprint
// Spec's dot-name twin stands (the four reads are the person's STANDING
// doors, registered by RegisterBlueprintReads over the read service; the
// propose twin stands through RegisterBlueprintPropose over the records
// seam). Backward — the family holds exactly ONE mutating spec and it is
// the propose twin, and no spec is twinned to a write door: save,
// duplicate, delete and revert are the person's human doors (T8's guard
// marks them humanDoorOnly and refuses any tool that wears one of those
// names); the agent sees, checks, and PROPOSES.
func TestRecordsParityBlueprintFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	tbl.BlueprintReads = &fakeBlueprintReads{}
	if !tbl.RegisterBlueprintReads() {
		t.Fatal("RegisterBlueprintReads refused a wired read service")
	}
	if !tbl.RegisterBlueprintPropose() {
		t.Fatal("RegisterBlueprintPropose refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The constants, never dotted literals (TestPolicyAuthoringNames-
		// StayConf confines the spellings to method.go).
		string(MethodBlueprintSave):      true,
		string(MethodBlueprintDuplicate): true,
		string(MethodBlueprintDelete):    true,
		string(MethodBlueprintRevert):    true,
		string(MethodProposalCommit):     true,
	}
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "blueprint" {
			continue
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no blueprint write door reaches an agent through the catalog (BP9)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Name != "blueprint_propose" {
				t.Errorf("%s mutates and is not the propose twin — an agent may PROPOSE, never save, duplicate, delete or revert", sp.Name)
			}
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating blueprint specs, want exactly 1 (the propose twin)", mutators)
	}
}

// TestRecordsParityGateFamilies is S57 T9 by name: the two-direction walk
// over the three gate families the nine specs open. Forward — every
// verify/mergerule/dod Spec's dot-name twin stands on the wired table (the
// six reads stand through T8's three registers over the gate seams, the
// three propose-only twins through RegisterGatePropose over the records
// stub). Backward — no spec is twinned to the gate plane's nine human
// writes (a catalog entry for any of them would be an agent door onto a
// write the socket's human half alone carries — VG17's lock at the
// catalog level), and the family holds exactly three mutating specs and
// they are the twins: an agent FILES the gate kinds' proposals and marks
// its own ledger, and nothing on the gate plane moves until a person
// commits (TD2/TD3; dod_mark marks only its own loop, evidence-bound).
func TestRecordsParityGateFamilies(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = recordsListingStub{}
	tbl.Verify = testVerifyService{}
	tbl.RegisterVerify()
	tbl.MergeRule = testMergeRuleService{}
	tbl.RegisterMergeRule()
	tbl.DoD = testDoDService{}
	tbl.RegisterDoD()
	if !tbl.RegisterGatePropose() {
		t.Fatal("RegisterGatePropose refused a wired records seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The nine human writes, by their one sanctioned spelling: the
		// gate plane's write half is the person's door (VG17), and a
		// catalog twin for any of them is an agent door onto it.
		string(MethodVerifyAdd):           true,
		string(MethodVerifyUpdate):        true,
		string(MethodVerifyArchive):       true,
		string(MethodVerifyRestore):       true,
		string(MethodVerifyApplyTemplate): true,
		string(MethodVerifyTry):           true,
		string(MethodMergeRuleSet):        true,
		string(MethodMergeRuleClear):      true,
		string(MethodDoDMark):             true,
	}
	twins := map[string]bool{
		string(MethodVerifyProposeAdd):        true,
		string(MethodMergeRuleProposeTighten): true,
		string(MethodDoDMarkAsAgent):          true,
	}
	mutators := 0
	for _, sp := range records.Catalog() {
		if sp.Family != "verify" && sp.Family != "mergerule" && sp.Family != "dod" {
			continue
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no gate write reaches an agent through the catalog (VG17)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if !twins[sp.Method] {
				t.Errorf("%s mutates and is not one of the three twins — the gate plane's agent doors are the three, named (VG19)", sp.Name)
			}
			if sp.Class != "detectable" {
				t.Errorf("%s mutates with class %q, want detectable — an inert row or a ledger event is visible on a re-run", sp.Name, sp.Class)
			}
		} else if sp.Class != "safe" {
			t.Errorf("%s is a read with class %q, want safe — a re-run re-reads", sp.Name, sp.Class)
		}
	}
	if mutators != 3 {
		t.Errorf("%d mutating gate specs, want exactly 3 (the twins)", mutators)
	}
}

// TestRecordsParityEnvironmentFamily is S59 T7 by name: the two-direction walk
// over the execution-environment family's five specs. Forward — every
// environment-family Spec's dot-name twin stands on a wired table: the four
// reads through RegisterEnvironment / RegisterSecretRef over the plane's two
// seams, and the one ask through RegisterEnvironmentProposeRestrict over the
// records seam (it is answered by the records service, never the authoring
// service — it files a proposal, not an environment). Backward — no spec is
// twinned to any of the plane's eleven human writes (seven environment, four
// secretref), and the family holds exactly ONE mutating spec, which is the ask:
// an agent FILES a tightening and nothing about what a command runs in changes
// until a person commits it (EV16, S35 TD3). A fifth spec, or a twin onto
// environment.create or secretref.archive, fails here at the data level even
// if no handler ever arrived to match it.
func TestRecordsParityEnvironmentFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Records = &envProposeConformanceRecords{recordNameSpy: &recordNameSpy{}}
	tbl.Environment = envSeam{}
	tbl.SecretRefs = refSeam{}
	if !tbl.RegisterEnvironment() || !tbl.RegisterSecretRef() {
		t.Fatal("the plane refused its two seams — the four read twins would have no door")
	}
	if !tbl.RegisterEnvironmentProposeRestrict() {
		t.Fatal("the ask refused its records seam — environment_propose_restrict would have no door")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	forbiddenTwins := map[string]bool{
		// The constants, never dotted literals (TestPolicyAuthoringNamesStayConf
		// confines the spellings to method.go).
		string(MethodEnvironmentCreate):   true,
		string(MethodEnvironmentUpdate):   true,
		string(MethodEnvironmentArchive):  true,
		string(MethodEnvironmentRestore):  true,
		string(MethodEnvironmentBind):     true,
		string(MethodEnvironmentUnbind):   true,
		string(MethodEnvironmentTeardown): true,
		string(MethodSecretRefCreate):     true,
		string(MethodSecretRefUpdate):     true,
		string(MethodSecretRefArchive):    true,
		string(MethodSecretRefRestore):    true,
	}
	mutators := 0
	seen := map[string]bool{}
	for _, sp := range records.Catalog() {
		if sp.Family != "environment" {
			continue
		}
		seen[sp.Name] = true
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the table does not register — the two doors are drifting (S38: one definition, two doors)", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — no execution-environment or secret-reference WRITE door reaches an agent through the catalog (EV16)", sp.Name, sp.Method)
		}
		if sp.Mutates {
			mutators++
			if sp.Name != "environment_propose_restrict" {
				t.Errorf("%s mutates and is not the ask — an agent may PROPOSE a tightening, never create, update, bind, unbind, tear down or reference", sp.Name)
			}
		}
	}
	if mutators != 1 {
		t.Errorf("%d mutating environment specs, want exactly 1 (the ask)", mutators)
	}
	if len(seen) != 5 {
		t.Errorf("the environment family holds %d specs (%v), want exactly 5 — four reads and one ask", len(seen), seen)
	}
	// The reverse half: the plane's one agent door stands, and it stands
	// because a spec points at it. A registered method with no spec is a door
	// the model cannot see; a spec with no method is a promise nobody answers.
	if !registered[MethodEnvironmentProposeRestrict] {
		t.Error("environment.proposeRestrict is not registered over a wired records seam — the ask's own tool would have nothing to call")
	}
	for _, m := range []MethodName{MethodEnvironmentList, MethodEnvironmentShow, MethodEnvironmentEffective, MethodSecretRefList} {
		if !registered[m] {
			t.Errorf("%s is not registered — a read twin of the person's standing door is missing, so the tool and the door are no longer one implementation", m)
		}
	}
}
