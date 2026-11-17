package protocol

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cjlapao/aeon/internal/records"
)

// S38 T15 (OL10): the catalog is ONE definition with TWO doors. A tool call
// from a step and a socket call from a person answer the same Spec through
// the same service — this file projects the socket half. The pairing is the
// catalog's own (`Spec.Method`, pinned at authoring time in T9), so the two
// doors cannot drift into two actions; the parity test walks both directions
// every run.

// RecordsFailure is a domain refusal from the records door: a sentence, not
// an error channel (DB19). The handler renders it as ErrBadParams, because
// on the wire a refused action IS bad parameters from whoever asked.
type RecordsFailure struct {
	Sentence string
}

// RecordsAPI is the daemon's records service as the table sees it. The
// identity rule lives behind this seam (OL3/OL8): the DAEMON stamps the
// actor from the connection's peer credential — params that carry an `actor`
// field die in decode (DisallowUnknownFields), so a caller cannot pick its
// own kind.
type RecordsAPI interface {
	// Do answers one catalog action (by its tool name, "goal_create") as
	// the person behind sess. The second result is a refusal sentence
	// ("" when the action ran); infra failures travel as error.
	Do(ctx context.Context, sess *Session, tool string, params json.RawMessage) (any, string, error)

	// The three decisions are NOT catalog actions and never will be: a
	// review is a person's act, so its doors are here and nowhere else.
	// Each stamps store.Actor{Kind:"user", ID:<peer uid>} from sess.
	DecideGoal(ctx context.Context, sess *Session, p GoalDecideParams) (DecideView, string, error)
	DecideBrief(ctx context.Context, sess *Session, p BriefDecideParams) (DecideView, string, error)
	DecideTask(ctx context.Context, sess *Session, p TaskDecideParams) (DecideView, string, error)

	// The board read and the goal's whole picture (criteria 8, 10).
	TaskList(ctx context.Context, sess *Session, p TaskListParams) (TaskListResult, error)
	GoalShow(ctx context.Context, sess *Session, p GoalShowParams) (GoalShowResult, string, error)
}

// registerRecords projects every catalog Spec as a socket method. It runs
// from NewTable AFTER the explicit registrations, so a Spec whose dot-name
// twin already stands — project.* and component.* from S38 T3, loop.list
// from S30, and the person's task.list / goal.show from this task — is
// SKIPPED, never shadowed: the parity test requires a method per Spec, not
// a second door on one name.
// autonomyCatalogSkip is the set of dot-names the records projection must
// never claim, per the deviation recorded above registerRecords (S56 T8):
// the three reads T6 owns and the two propose twins this package's
// RegisterAutonomyPropose owns.
// mcpMineCatalogNames — the five agent-scoped MCP doors (S46 T8). Their
// registration is catalog-driven EXCEPT for the MCPAdmin-seam gate above.
var mcpMineCatalogNames = map[string]bool{
	string(MethodMCPMineServers):       true,
	string(MethodMCPMineGrants):        true,
	string(MethodMCPMineStatus):        true,
	string(MethodMCPMineProposeRemove): true,
	string(MethodMCPMineProposeRevoke): true,
}

var autonomyCatalogSkip = map[string]bool{
	string(MethodAutonomyList):          true,
	string(MethodAutonomyShow):          true,
	string(MethodAutonomyEffective):     true,
	string(MethodAutonomyProposeRevoke): true,
	string(MethodAutonomyProposeDeny):   true,
}

func (t *Table) registerRecords() {
	for _, sp := range records.Catalog() {
		sp := sp
		// The seed tools (family "seed") are registered by RegisterSeed, not
		// here: their method ("seed.status") is the human door's, answered by
		// the seed handler. A records projection would double-register it and
		// answer it through the records handler, not the seed one. They reach
		// the agent only as tools (recordTools), never as a records method —
		// so the catalog keeps the spec but this loop skips it.
		if sp.Family == "seed" {
			continue
		}
		// S45 T7 (PM9): the two propose twins (policy.proposeAdd /
		// policy.proposeRemove) are registered by RegisterPolicyPropose,
		// not here — the RegisterSeed rule (V13) for the catalog's first
		// MUTATING specs: with no records service behind them they are
		// ABSENT, not registered-and-refusing, because the service is what
		// honours their safety (stamped actor, forced source, per-agent
		// cap). The catalog keeps the specs for the tool projection; this
		// loop skips them so the conditional door owns the names.
		if sp.Method == string(MethodPolicyProposeAdd) || sp.Method == string(MethodPolicyProposeRemove) {
			continue
		}
		// S56 T8 (2026-11-01): ALL FIVE autonomy names skip here, not just
		// the two propose twins — and that is a deviation from the header's
		// "skip a twin whose dot-name already stands" rule, recorded in
		// T8's Notes. The exists-check at :78 cannot protect them:
		// registerRecords runs inside NewTable, BEFORE the daemon calls
		// RegisterAutonomy (and RegisterAutonomyPropose), and those
		// registers call t.register UNCONDITIONALLY — t.register panics on
		// a duplicate, so if this loop claimed autonomy.list/show/effective
		// first, every NewTable + RegisterAutonomy table (the guard tests'
		// newAutonomyTable, every newTestTable that wires the seam) would
		// panic at construction. The three reads are owned by T6's
		// RegisterAutonomy; the two proposes by RegisterAutonomyPropose
		// (autonomy_propose.go). A catalog spec whose method nobody
		// registered is a parity-test failure, not a double door.
		if autonomyCatalogSkip[sp.Method] {
			continue
		}
		// S46 T8 (V13, the autonomy deviation's shape): the five mcp.mine.*
		// names are owned by RegisterMCPMine — conditional on BOTH the
		// records service (these are catalog doors) and the MCP admin seam
		// (their scoped answers forward to it). The projection skips them
		// unconditionally because NewTable runs before any seam is wired —
		// a gate here would read nil always and nobody would register them
		// at all; the explicit registerer is the same pattern RegisterAuto-
		// nomyPropose carries. TestMcpAdminAbsentWhenSeamNil counts every
		// mcp.* name and finds none when the seam is nil.
		if mcpMineCatalogNames[sp.Method] {
			continue
		}
		// S55 T10 — the loop-control / budget / watchdog reads (families
		// "loopcontrol" / "budget" / "watchdog") are registered by their
		// planes' own Register* (T5/T8/T9), which run AFTER NewTable — a
		// records projection here would double-register the twin (a panic)
		// and answer the plane's read through the records handler, not the
		// plane's door that stamps the actor and renders the typed refusal.
		// They reach the agent as tools (recordTools) and the person through
		// the plane's method; the seed precedent, carried.
		if sp.Family == "loopcontrol" || sp.Family == "budget" || sp.Family == "watchdog" {
			continue
		}
		// S62 T6 (2026-11-02): the backup family's reads (today
		// backup_status, method backup.status) are registered by the plane's
		// own RegisterBackup — CONDITIONAL on the Backup seam, the Register-
		// Seed rule (V13): nil seam means the door is ABSENT, not registered-
		// and-refusing. A records projection here would double-register the
		// name once the seam is wired (a t.register panic, the S55 T10
		// class) and keep backup.status alive in a build whose backup seam
		// never linked (the opposite of V13). The plane's method is the
		// person's door that answers through the engine's Status; the spec
		// still reaches the agent as the backup_status TOOL (recordTools),
		// which is exactly what the twin is for.
		if sp.Family == "backup" {
			continue
		}
		// S57 T9 (VG19): the nine gate specs (families "verify" /
		// "mergerule" / "dod") skip for the SAME reason as the backup
		// family — and for a reason the spec's exists-check line got wrong
		// (T9.md:24, corrected in T9's Notes): registerRecords runs inside
		// NewTable, BEFORE the daemon calls RegisterVerify /
		// RegisterMergeRule / RegisterDoD (and RegisterGatePropose), so the
		// exists-check below cannot protect the six read twins that reuse
		// T8's door names. A projection here would double-register (a
		// t.register panic) and answer the gates' reads through the records
		// handler — through a door that stamps the PERSON's peer, not the
		// step's agent identity the gate reads are scoped by. The six reads
		// are owned by T8's registers over the gate seams; the three twins
		// by RegisterGatePropose, conditional on the records service; all
		// nine reach the agent as TOOLS (recordTools) the way the backup
		// spec does.
		if sp.Family == "verify" || sp.Family == "mergerule" || sp.Family == "dod" {
			continue
		}
		// S58 T6 (2026-11-03): the five supervision specs skip for the
		// backup family's reason — all five dot-names are HUMAN doors owned
		// by their planes' own registers, which run after NewTable:
		// inbox.list/inbox.summary by RegisterInbox (over the live inbox
		// seam), mission.status by RegisterMission (nil seam ⇒ ABSENT, V13),
		// notify.class.list by RegisterNotifyClasses, notify.channel.list by
		// RegisterNotifyChannels. A projection here would double-register
		// (a t.register panic — this is exactly the bug the boot carried
		// before the skip landed) and answer a person's door through the
		// records handler, which scopes by the STEP's identity, not the
		// peer's. The five still reach the agent as TOOLS (recordTools) —
		// and the tool path is where the OWN-VIEW scope lives
		// (daemon/records_supervision.go), which a projection door could
		// never honour: it stamps nobody's root.
		if sp.Family == "supervision" {
			continue
		}
		// S48 T10 (2026-11-03, the autonomy deviation's shape, carried):
		// ALL FIVE blueprint names skip here. The four reads are owned by
		// RegisterBlueprintReads (T7) and the propose twin by
		// RegisterBlueprintPropose (T10) — both run AFTER NewTable, and
		// t.register panics on a duplicate, so if this loop claimed
		// blueprint.list first, every table that wires the reads (the
		// daemon, the guard tests, every newTestTable with the seam) would
		// panic at construction. The reads reach the person's door and the
		// agent's tool through the one standing implementation; propose
		// reaches the agent only over the records service (V13). The
		// exists-check below cannot help: at NewTable time none of the
		// five stands yet.
		if sp.Family == "blueprint" {
			continue
		}
		// S59 T7 (2026-11-03, the blueprint deviation's shape, carried): ALL
		// FIVE environment names skip here. The four reads (environment_list ·
		// environment_show · environment_effective · secretref_list) are owned
		// by RegisterEnvironment and RegisterSecretRef — T6's registers over the
		// environment seams, which run AFTER NewTable — and the propose twin by
		// RegisterEnvironmentProposeRestrict (environment_propose.go), which is
		// conditional on the records service. t.register panics on a duplicate,
		// so if this loop claimed environment.list first, every table that wires
		// the plane (the daemon, T6's guard, every newTestTable with the seam)
		// would panic at construction; and a projection would answer the plane's
		// read through the records handler on a build whose environment seam
		// never linked (the opposite of V13). The five reach the agent as TOOLS
		// (recordTools); the person keeps T6's twenty doors. The exists-check
		// below cannot help: at NewTable time none of the five stands yet.
		if sp.Family == "environment" {
			continue
		}
		// S60 T9 (2026-11-04, the environment deviation's shape, carried):
		// ALL FIVE mergequeue names skip here. The three reads
		// (mergequeue_list · mergequeue_show · mergequeue_status) and the
		// own-item dequeue ride the plane's OWN methods (mergequeue.list ·
		// .show · .status · .dequeue), owned by RegisterMergeQueue over the
		// queue seam, which runs AFTER NewTable; the propose twin belongs to
		// RegisterMergeQueueProposeEnqueue, conditional on the records
		// service (V13). t.register panics on a duplicate, and a projection
		// would answer the plane's read through the records handler on a
		// build whose queue seam never linked. The five reach the agent as
		// TOOLS (recordTools); the person keeps T8's ten doors.
		if sp.Family == "mergequeue" {
			continue
		}
		// S49 T7 (2026-11-04, the environment deviation's shape, carried): ALL
		// FIVE automation names skip here. The four reads (automation_list ·
		// automation_show · automation_fires · automation_health) are owned by
		// RegisterAutomationReads — T3's register over the automation-reads
		// seam, which runs AFTER NewTable — and the propose twin by
		// RegisterAutomationProposeDisable (automation_propose.go), which is
		// conditional on the records service. t.register panics on a duplicate,
		// so if this loop claimed automation.list first, every table that wires
		// the plane would panic at construction; and a projection would answer
		// the plane's read through the records handler on a build whose reads
		// seam never linked (the opposite of V13). The five reach the agent as
		// TOOLS (recordTools); the person keeps T3's ten doors and T4's three.
		if sp.Family == "automation" {
			continue
		}

		// S50 T5 (2026-11-04, the S49 shape carried): ALL THREE roadmap
		// names skip here. roadmap_list and roadmap_show twin doors that
		// registerRoadmap() already registered unconditionally (the
		// exists-check above would skip them too — the skip is for the
		// third name); roadmap_propose_idea must NOT be projected as an
		// unconditional door, because the records service is what honours
		// what makes it safe (the stamped step actor, the shared create
		// validator of the seam, the per-agent open-proposal cap). Its
		// register is RegisterRoadmapProposeIdea (roadmap_propose.go), a
		// V13 conditional that runs AFTER NewTable: no seam, no door —
		// absent, never registered-and-refusing.
		if sp.Family == "roadmap" {
			continue
		}

		// S51 T5 (2026-11-04, the S50 shape carried): ALL THREE bug names
		// skip here. bug_list and bug_show twin doors registerTriage()
		// already registered unconditionally (the exists-check above would
		// skip them too — the skip is for the third name); bug_file must
		// NOT be projected as an unconditional door, because the records
		// service is what honours what makes it safe (the stamped step
		// actor, the resolved slug, the SAME shared CreateAudited, the
		// counted per-agent open-claims cap of TM6). Its register is
		// RegisterTriageFileAs (triage_file_as.go), a V13 conditional that
		// runs AFTER NewTable: no seam, no door — absent, never
		// registered-and-refusing.
		if sp.Family == "bug" {
			continue
		}

		// S52 T4 (2026-11-04, the S46 mcp.mine shape carried): ALL FOUR
		// memory names skip here. learnings.mine.query/.show/.add/
		// .proposeRettract must NOT be projected as unconditional doors,
		// because the records service is what honours what makes them safe
		// (the stamped step identity behind LM1's readable scopes, the
		// STAMPED scope key and counted ledger cap behind learning_add,
		// LM4's provenance law, the shared create validator of the seam
		// and the per-agent open cap behind the propose). Their register
		// is RegisterLearningsMine (learnings_mine.go), a V13 conditional
		// that runs AFTER NewTable: no records service, no door — absent,
		// never registered-and-refusing.
		if sp.Family == "memory" {
			continue
		}
		// S54 T4 (2026-11-04, the backup family's shape carried): the ONE
		// platform spec skips here. platform.status is the person's panel
		// door, owned by RegisterPlatformStatus — CONDITIONAL on the
		// composed adapter (V13: a storeless, retrievalless, graphless
		// daemon has no panel and no door), which runs AFTER NewTable.
		// t.register panics on a duplicate, and a projection here would
		// both panic once the panel registers and keep platform.status
		// alive on a build whose seams never linked — answering the
		// person's composed read through the records handler, which stamps
		// the STEP's identity, not the peer's. The spec exists so
		// surface.ToolTwin can name the twin mechanically and so
		// recordTools lays the ONE agent read (index_status), which answers
		// through the same platformview composer over the step's own root
		// (daemon/records_platform.go); the projection loop owns nothing.
		if sp.Family == "platform" {
			continue
		}
		// S63 T6 (2026-11-05, the platform shape carried): ALL THREE
		// scorecard names skip here. scorecard_mine · scorecard_component ·
		// digest_learned twin doors RegisterScorecard / RegisterDigest own
		// over the plane's seams, which run AFTER NewTable — a projection
		// here would double-register (a t.register panic, the S55 T10 class)
		// and keep scorecard.agent alive in a build whose scorecard seam
		// never linked (the opposite of V13). The three reach the agent as
		// TOOLS (recordTools), where the OWN-VIEW scope lives — the stamped
		// slug and the step's root (daemon/records_scorecard.go), which a
		// projection door could never honour: it stamps nobody's identity.
		// The person keeps T5's ten doors.
		if sp.Family == "scorecard" {
			continue
		}
		// S27 T8 (2026-11-05, the S63 shape carried): ALL FOUR scheduler
		// names skip here. scheduler_status · scheduler_preview ·
		// scheduler_decisions · scheduler_attention twin the four READ
		// doors RegisterContinuous owns over the Continuous seam (S27 T7),
		// which runs AFTER NewTable — a projection here would
		// double-register (a t.register panic, the S55 T10 class) and keep
		// scheduler.status alive in a build whose scheduler seam never
		// linked (the opposite of V13; the eight CONTROLS never appear in
		// this catalog at all — CN2's human doors have no spec to skip).
		// The four reach the agent as TOOLS (recordTools), where the
		// OWN-ROOT scope lives — the step's component root, resolved by
		// Identify and stamped over every query
		// (daemon/records_continuous.go), which a projection door could
		// never honour: it stamps nobody's identity. The person keeps
		// T7's twelve doors.
		if sp.Family == "scheduler" {
			continue
		}
		// S61 T8 (2026-11-05, GI17): ALL SEVEN goal-intake names skip here.
		// charter_list · charter_show · template_list · template_show ·
		// intake_status twin the READ doors RegisterCharter/RegisterTemplates/
		// RegisterIntake own over their seams (S61 T7), which run AFTER
		// NewTable — a projection here would double-register (the S55 T10
		// class of t.register panic). charter_propose and intake_propose
		// twin the two MUTATING conditional doors RegisterIntakePlanePropose
		// owns over the records seam (the S45/S56 rule: an unwired runtime
		// leaves them ABSENT, not registered-and-refusing). The person keeps
		// T7's fourteen doors plus these two asks on the same socket.
		if sp.Family == "intake" {
			continue
		}
		name := MethodName(sp.Method)
		if _, exists := t.methods[name]; exists {
			continue
		}
		t.register(&Method{Name: name, RequiresInitialize: true, Handle: t.handleRecord(sp)})
	}
}

// handleRecord decodes params as raw JSON (the catalog's schemas own their
// shape; DisallowUnknownFields still fires at the catalog decode inside the
// service, which is where an smuggled `actor` field meets its refusal).
func (t *Table) handleRecord(sp records.Spec) func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
	return func(ctx context.Context, sess *Session, params json.RawMessage) (any, error) {
		if t.Records == nil {
			// Three-state honesty: an unwired runtime says so rather than
			// answering zero.
			return nil, ErrStoreUnavailable.WithWhat("records are unavailable in this runtime")
		}
		out, sentence, err := t.Records.Do(ctx, sess, sp.Name, params)
		if err != nil {
			return nil, err
		}
		if sentence != "" {
			return nil, ErrBadParams{Err: errors.New(sentence)}
		}
		return out, nil
	}
}
