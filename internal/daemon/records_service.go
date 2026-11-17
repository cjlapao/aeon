package daemon

// records_service.go — the management catalog's service: who is asking
// (Actor, Identity), how a domain refusal travels (Failure, DB19), and the
// seams the service answers through — one field per plane, each the SAME
// value the person's socket door holds, so a tool and its twin cannot drift
// (rule 4) and an unwired plane says so instead of answering zero (V13).
//
// The companion, records.go, is the ONE switch that dispatches a catalog
// action to its arm. The split is the two concerns of the same service:
// what it IS (identity, seams, construction) and what it RUNS (the switch
// and its shared decoders). Measured after the split: 2026-11-03, S59 T7.
//
// The security core, carried from S38 T9 whole: every identity a record
// carries (project, component, source loop, source step, author) is read from
// the STEP whose id rides the context, never from the input the model typed.
// An argument a model can type is not an identity (OL3); that is why the write
// schemas do not even name these fields, and why the decoder refuses one that
// arrives anyway.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/roadmap"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/stepctx"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/triage"
)

// Actor names who asked, for the store's provenance columns. From the tool
// path it is always the agent the step runs as; the socket's human half
// (T15) passes the peer's user id.
type Actor struct {
	Kind string // "agent" or "user"
	ID   string
}

// Failure is a domain refusal — the sentence a caller shows, never a Go
// error (DB19: "this is not allowed" and "the disk is full" must not share a
// channel).
type Failure struct {
	Sentence string
}

func (f *Failure) Error() string { return f.Sentence } // for error-passing convenience; the switch never wraps it

// Identity is what a step IS, as the store knows it — the stamped half of
// every record write.
type Identity struct {
	StepID        int64
	Agent         string
	LoopSlug      string
	ComponentRoot string
	ComponentID   string
	// RootID names the ROOT component of the loop's component — the tree
	// the loop acts within. A goal or epic hangs on a root (CT10), so the
	// identity's owning coordinate is the root, never the component's own
	// id and never its parent for a standalone root.
	RootID string
	// User marks the socket's human half (S38 T15, OL10): there is no step
	// to resolve, the person owns the org, and the loop-scope checks below
	// read this instead of comparing against a loop no person carries.
	User bool
}

// RecordsService answers catalog actions against the store.
type RecordsService struct {
	st *store.Store

	// taskChanged publishes the board's one kind AFTER the row commits
	// (S38 T15). Set once by the daemon; nil on a bare service, where the
	// board has no event log to lie to.
	taskChanged func(after store.Task, previous string)

	// supervision is S58 T6 (SV16): the five supervision reads' door,
	// injected by the daemon at wiring (the run pattern) — nil on a bare
	// service, which refuses by name rather than answering zero.
	supervision SupervisionDoor

	// run is the run door (S39 T10): the ONE loop action an agent may take,
	// and it is the daemon's startRun — the same function the socket's human
	// door calls, so the two doors cannot drift. Nil on a bare service
	// (a storeless mount has no runs to start and says so rather than
	// answering zero).
	run func(context.Context, runInput) (any, error)

	// seedEngine is the one *seed.Engine the seed_status tool reads through
	// (S43-extra T6): the SAME instance the socket's seed.status reads, so
	// the two doors answer identically (the parity test pins it). Nil on a
	// seedless mount (V13) — the arm then refuses, never answers zero.
	seedEngine *seed.Engine

	// policyStore is the SAME *policy.Store the socket's policy reads read
	// (S45 T7, PM9) — the daemon assigns table.PolicyStore, wired over the
	// one RepoFuncs bridge the enforcement gate reads through, so the tool
	// door and the socket door can never show two sets. Nil on a policyless
	// mount (V13): the seven arms refuse, never answer zero.
	policyStore *policy.Store

	// platformIndex is S54 T4 (SP4): the agent's ONE platform read. The
	// daemon injects the read built over the SAME PlatformDeps the person's
	// platform.status composes over (platform_reads.go), scoped per call to
	// the step's own component root — freshness of the step's repository,
	// with the operator-view fields dropped (T0's visibility ruling). Nil
	// on a retrievalless, graphless mount (V13): the arm refuses by name,
	// it never answers a wall of unwired blocks.
	platformIndex func(context.Context, string) (*IndexFreshnessView, error)

	// proposals is the SAME protocol.ProposalStore the socket's proposal
	// methods write and read through (S45 T7): the tool's proposal_list
	// and the two propose arms use this adapter, so `proposal.create`'s
	// create path and the tools' are ONE path — one adapter, one store,
	// no second write site. Nil on an unwired mount; the arms refuse (V13).
	proposals protocol.ProposalStore

	// toolsCatalog is the SAME catalog service the socket's four `tools.*` reads
	// answer through (S47 T7), assigned once by the daemon beside the seam. The
	// two own-view arms forward here rather than re-deriving a grant chain: one
	// `toolcatalog.ForAgent` for the person's `tools.byAgent` and the agent's
	// `tools_mine`, so the two doors cannot answer one question twice. Nil on a
	// catalogless mount (V13) — the arms refuse, never answer an empty inventory.
	toolsCatalog toolsViewAPI

	// validateProposal is S35 T2's seam, assigned the SAME function value
	// the table's ValidateProposal holds: the tool-created proposal is
	// refused at create for the reason the commit door would re-refuse
	// (TD3), with that sentence travelling verbatim.
	validateProposal func(ctx context.Context, kind, payload string) error

	// autonomyService is the SAME protocol.AutonomyService the socket's
	// ten autonomy.* methods read through (S56 T8, AL11): the three
	// autonomy reads answer through it, so the tool door and the socket
	// door show one surface (the parity walk pins both directions). Nil
	// on a mount without the autonomy seam (V13) — the arms refuse, never
	// answer zero. The two propose arms do NOT use it: they create
	// proposals through the shared proposals/validateProposal path, never
	// grants.
	autonomyService protocol.AutonomyService

	// mcpAdmin is S46 T8's seam: the SAME func value the table's MCPAdmin
	// holds (Options.MCPAdmin — the cmd/aeon admin seam), entered by the
	// five mcp.mine.* arms with self stamped through protocol.WithMCPActor.
	// Nil on a mount without the MCP surface — the arms refuse with a named
	// sentence (V13), never answer an empty view.
	mcpAdmin func(ctx context.Context, method string, params json.RawMessage) (any, string, error)

	// blueprints is S48 T10's seam: the SAME protocol.BlueprintReadService
	// the table's blueprint doors answer through (the daemon assigns
	// table.BlueprintReads), so the four read arms and the socket doors
	// cannot drift (rule 4). Nil on a reads-less mount (V13): the arms
	// refuse by name, never answer zero or an all-clear.
	blueprints protocol.BlueprintReadService

	// listBlueprints / showBlueprint are the two STANDING read seams the
	// person's blueprint.list/show doors answer through (assigned long
	// ago; T7 grew their shapes). The tool arms forward to these very
	// function values — one implementation, two doors (rule 4).
	listBlueprints func(protocol.BlueprintListParams) (protocol.BlueprintListResult, error)
	showBlueprint  func(protocol.BlueprintShowParams) (protocol.BlueprintShowResult, error)

	// validateBlueprint is the standing validate seam (table.ValidateBlueprint,
	// assigned by initBlueprints): blueprint_validate answers the ONE
	// validator through it, over text, persisting nothing.
	validateBlueprint func(params protocol.BlueprintValidateParams) (any, error)
	// verify is the SAME protocol.VerifyStore the socket's seven verify.*
	// doors answer through (S57 T9, VG19): the four gate read arms
	// (verify_list / verify_show / verify_runs, and the dod_mark run half)
	// forward here with the scope stamped from the step, so the tool door
	// and the human door read one set of rows and cannot drift. Nil on a
	// gateless mount (V13) — the arms refuse, never answer zero.
	verify protocol.VerifyStore

	// mergeRule is the SAME protocol.MergeRuleStore the socket's
	// mergerule.* doors answer through (S57 T9, VG19): mergerule_show and
	// mergerule_preview forward here, scope stamped. Nil on a gateless
	// mount (V13) — the arms refuse, never answer zero.
	mergeRule protocol.MergeRuleStore

	// dod is the SAME protocol.DoDStore the socket's two dod.* doors
	// answer through (S57 T9, VG19): dod_list forwards here and
	// dod_mark's write goes through its Mark with the agent stamped —
	// the store's pairing backstop and in-DB run re-check are the second
	// line. Nil on a gateless mount (V13) — the arms refuse, never
	// answer zero.
	dod protocol.DoDStore

	// environments is the SAME protocol.EnvironmentService the socket's
	// thirteen environment doors answer through (S59 T7, EV16/EV17): the
	// three environment read arms forward here with the scope stamped from
	// the step, so the tool door and the person's panel read one set of rows
	// and cannot drift (rule 4). Nil on an environmentless mount (V13) — the
	// arms refuse by name, never answer zero or an empty listing.
	environments protocol.EnvironmentService

	// mergeQueue is the SAME protocol.MergeQueueService the socket's ten
	// queue doors answer through (S60 T9, rule 4): the agent's three reads
	// forward there, and TestToolReadsMatchSocketReads pins the bytes. Nil
	// means a mount without the queue plane — the five arms state absent,
	// never zero (V13).
	mergeQueue protocol.MergeQueueService

	// secretRefs is the SAME protocol.SecretRefService the socket's seven
	// secretref doors answer through (S59 T7, EV7): secretref_list forwards
	// here and projects the answer through protocol.SecretRefView.AgentView,
	// which is where `source` stops — the agent's type has no field for it.
	// Nil on a referenceless mount (V13): the arm refuses, never answers an
	// empty inventory.
	secretRefs protocol.SecretRefService

	// automationReads is the SAME protocol.AutomationReads the socket's ten
	// automation read doors answer through (S49 T7, AN9/rule 4): the four
	// read arms forward through protocol.AnswerAutomation* — the one
	// rendering each door shares — so the tool answer IS the socket answer,
	// statements, outcome tally and evaluatedAt stamp included. Nil on an
	// automationless mount (V13) — the arms refuse by name, never answer
	// zero or an empty listing.
	automationReads protocol.AutomationReads

	// roadmapPlane is the SAME *roadmap.Roadmap the socket's roadmap doors
	// answer through (S50 T5, RM8/rule 4) — wired from initRoadmap beside
	// the Table seam, so one service instance serves both planes and the
	// two read arms forward through protocol.AnswerRoadmap* — the one
	// rendering the person's doors run. Nil on a storeless mount (V13) —
	// the arms refuse by name, never answer an empty board.
	roadmapPlane *roadmap.Roadmap

	// learningsPlane is S52 T4 (LM5, rule 4): the SAME *learnings.Learnings
	// the socket's learnings.* doors answer through, injected by the daemon
	// at wiring — nil on a bare service, whose four memory arms refuse by
	// name rather than answer zero (V13). The readable-scope resolution,
	// the stamped self-log, and the ledger-counted cap all live on this
	// one plane, so the agent's doors and the person's cannot drift.
	learningsPlane *learnings.Learnings

	// triagePlane is S51 T5 (TM6): the SAME *triage.Triage the socket's
	// triage doors answer through — wired from initTriage beside the Table
	// seam, so bug_show and bug_file run the SAME service (one Create, one
	// Show) and cannot drift from the person's doors.
	triagePlane *triage.Triage

	// triageBoard is bug_list's ONE answer: the daemon's triageBoard
	// closure — the very function value Table.TriageBoard holds — so the
	// board a step reads is byte-for-byte the board the person reads.
	triageBoard func(ctx context.Context, p protocol.TriageBoardParams) (any, error)

	// historyPlane is S53 T3 (HV4, rule 4): the SAME *history.Store facade
	// the socket's five history doors answer through — wired beside the
	// Table seam at boot, so the step's history_list/history_show twins
	// forward through protocol.AnswerHistory*, the ONE rendering the
	// person's doors run. Nil on a storeless mount (V13) — the two arms
	// refuse by name, never answer an empty list.
	historyPlane *history.Store

	// scorecardPlane and digestPlane are S63 T6 (RT11, rule 4): the SAME
	// services the person's scorecard.*/digest.learned doors answer
	// through — assigned beside the Table seams at boot, so the tool and
	// its twin run ONE implementation and cannot drift. Nil on a storeless
	// mount (V13) — the three arms refuse by name, never answer a card
	// computed from nothing.
	// charterPlane, templatePlane and intakePlane are S61 T8 (GI17, rule 4):
	// the SAME door structs the person's charter/template/intake doors
	// dispatch through (wired by the daemon's S61 block), so the seven goal-
	// intake arms answer byte-identically to their socket twins and a mount
	// that never wired the plane refuses each arm by name (V13).
	charterPlane   protocol.CharterService
	templatePlane  protocol.TemplateService
	intakePlane    protocol.IntakeService
	scorecardPlane protocol.ScorecardService
	digestPlane    protocol.DigestService

	// continuousPlane is S27 T8 (CN23, rule 4): the SAME
	// protocol.ContinuousAPI the socket's four scheduler READ doors answer
	// through — assigned beside the Table seams at boot (daemon.go), so
	// scheduler_status · scheduler_preview · scheduler_decisions ·
	// scheduler_attention run the person's own service and cannot drift
	// from it. The scope is the step's component root, stamped after the
	// shared filter grammar parses (daemon/records_continuous.go). Nil on a
	// storeless mount (V13) — the four arms refuse by name, never answer a
	// plan composed from nothing.
	continuousPlane protocol.ContinuousAPI

	// d is the owning daemon (S55 T10): the seven loop-control / budget /
	// watchdog arms read through the SAME Table seams the socket's doors
	// call (Table.LoopControl / Table.BudgetStore / Table.Watchdog), so the
	// tool and its twin share one implementation and cannot drift. Nil on a
	// bare service — the arms then refuse (V13), never answer zero.
	d *Daemon
}

func NewRecordsService(st *store.Store) *RecordsService { return &RecordsService{st: st} }

// Identify resolves the ctx's step id through the rows the store owns: step
// → loop's component root → registered component → its root component. Every miss
// refuses with a sentence naming what is missing — a step whose row does not
// exist, a workspace no component registers — rather than inventing an
// identity or answering from an empty that reads as "no records".
func (r *RecordsService) Identify(ctx context.Context) (Identity, *Failure) {
	stepID, ok := stepctx.StepID(ctx)
	if !ok {
		return Identity{}, &Failure{Sentence: "refused: records actions need a step — this call carries no step identity, so authorship could not be stamped (OL3)"}
	}
	step, err := r.st.ReadStep(stepID)
	if err != nil {
		return Identity{}, &Failure{Sentence: fmt.Sprintf("refused: step %d cannot be read (%v) — identity is read from the store, never taken on faith", stepID, err)}
	}
	sc := store.Scope{LoopSlug: step.LoopSlug, ComponentRoot: step.ComponentRoot}
	loop, err := r.st.GetLoop(sc)
	if err != nil {
		return Identity{}, &Failure{Sentence: fmt.Sprintf("refused: step %d names loop %s, which the store does not have — an action cannot be filed in a loop that is not there", stepID, sc)}
	}
	comp, err := r.st.ComponentForRoot(loop.ComponentRoot)
	if err != nil {
		return Identity{}, &Failure{Sentence: fmt.Sprintf("refused: workspace %s is not a registered component — records need a component to be filed in, and none claims that root", loop.ComponentRoot)}
	}
	rootID, err := r.st.RootOf(comp.ID)
	if err != nil {
		return Identity{}, &Failure{Sentence: fmt.Sprintf("refused: the root of component %s cannot be read (%v)", comp.ID, err)}
	}
	return Identity{StepID: stepID, Agent: step.Agent, LoopSlug: loop.Slug,
		ComponentRoot: loop.ComponentRoot, ComponentID: comp.ID, RootID: rootID}, nil
}
