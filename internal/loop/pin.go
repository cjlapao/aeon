package loop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/action"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/workspace"
)

// skillKnown is the loop package's injection seam for agent-services
// availability (S14 T4). It is the SEC-059 shape exactly once more: a seam
// defined in the package that NEEDS the answer, wired from OUTSIDE so the
// dependency direction stays agentsvc/cmd→loop and never loops back (AS11's
// deletability: remove internal/agentsvc and this var simply stays nil).
// nil means agent services are not configured, and skill lanes refuse at the
// pin with the honest sentence — a loop that is guaranteed to block at its
// first step is never made durable.
var skillKnown func(callee, skill string) bool

// SetSkillKnown wires the registry. It has EXACTLY ONE production call, in
// cmd/aeon's serve — pinned by TestTheSkillKnownWiringAppearsInExactlyOnePlace,
// because an unwired daemon refusing every skill lane is fine, and a daemon
// wired twice from two entry points is a second definition of who may be
// asked.
func SetSkillKnown(fn func(callee, skill string) bool) { skillKnown = fn }

type PinRefusal struct {
	Kind   string
	Detail string
}

func (r PinRefusal) Error() string { return r.Kind + ": " + r.Detail }

// Start validates source bytes before creating a loop and pins those exact
// bytes with the initial position in one transaction. The attended argument
// is the DECLARATION made at start (D2): it is written beside the blueprint
// pin and is never re-derived afterwards — a client connecting or
// disconnecting later changes nothing. Pass nil only when the start path
// genuinely cannot know (the CLI always knows).
// StartLinks are the S38 T13 goal/task coordinates a start may carry
// (OL9). Zero links is today's shape: no goal pinned, no task linked, the
// prompt whatever the caller typed.
type StartLinks struct {
	GoalID      string
	GoalVersion *int
}

// Start pins a loop with no goal/task coordinates — the call every
// existing caller makes, unchanged.
func Start(_ context.Context, s *store.Store, in store.NewLoop, name string,
	attended *bool, with map[string]string, route ...string) (LoopState, error) {
	return StartWith(context.Background(), s, in, StartLinks{}, name, attended, with, route...)
}

// StartWith is Start plus the coordinates: the goal version is PINNED at
// start (later goal versions never move the loop's frame), written in the
// same transaction as the pin — a loop row that claims a goal only half-
// written is a loop working on air.
func StartWith(_ context.Context, s *store.Store, in store.NewLoop, links StartLinks, name string,
	attended *bool, with map[string]string, route ...string) (LoopState, error) {
	in.GoalID = links.GoalID
	in.GoalVersion = links.GoalVersion
	bp, raw, err := pinSource(s, name, in.Mode)
	if err != nil {
		return LoopState{}, err
	}
	initial := InitialState(bp)
	if len(route) > 0 {
		initial.Route = route[0]
	}
	// T10: the operator's `--tech` (and any sibling) enters the durable state
	// at pin time, so a later `by-tech-tag` dispatch resolves against the
	// SAME map the blueprint's `with:` edges will extend — one With, one
	// resolution, no second place a tag could hide.
	for k, v := range with {
		initial.With[k] = v
	}
	// A locked blueprint's per-lane route overrides are copied into the state
	// NOW: the pin is the one moment they are read, so changing one later
	// never moves a loop already running.
	overrides, err := s.BlueprintOverrides(context.Background(), name)
	if err != nil {
		return LoopState{}, err
	}
	for _, o := range overrides {
		if o.Field == blueprint.FieldRoute {
			if initial.RouteOverrides == nil {
				initial.RouteOverrides = map[string]string{}
			}
			initial.RouteOverrides[o.LaneID] = o.Value
		}
	}
	// S60 T7: the queue's two stamps, BOTH conditional on the run being a
	// queue run at all (an item id and a component root in the `with` —
	// the reactor composes both from the item row; no other start path
	// carries them, so nothing here moves for the 100-odd loops that are
	// not queue landings).
	//
	// WorkDir: a RESOLUTION run starts inside the integration worktree
	// where the merge already sits in progress — the pin is the only door
	// that may set the field before evidence does (the single-writer test
	// admits it BY NAME: pin.go, exactly one assignment). And it is the
	// ONLY value the pin honours — `integration_dir` must equal what
	// action.IntegrationWorktreePath composes for this root and item, or
	// the stamp is ignored and the run starts un-moved: a hand-made With
	// cannot point a loop's workspace anywhere the queue did not build.
	// (A LANDING run passes no integration_dir — it moves by its own
	// git.integrate evidence, applyEvidenceWorkDir, the T5 seam.)
	//
	// auto_land: auto mode is the repository's SETTINGS decision (MQ3),
	// read by the reactor at start; stamping it as a FLAG (flags.*, not
	// `with`) is what makes the land-check lane skip by CODE — a flag the
	// pin wrote is a blueprint fact, and AnswerGate/Condition already
	// refuse to let a step's params forge flags.* writes. Manual and ask
	// modes leave it false: the person answers `land-now`.
	if item := with["item"]; item != "" && in.ComponentRoot != "" {
		if dir := with["integration_dir"]; dir != "" && dir == action.IntegrationWorktreePath(in.ComponentRoot, item) {
			initial.WorkDir = dir
		}
		if with["queue_mode"] == "auto" && initial.Flags != nil {
			initial.Flags["auto_land"] = true
		}
	}
	jsonState, err := (LoopState{RuntimeState: initial}).RuntimeJSON()
	if err != nil {
		return LoopState{}, err
	}
	if in.Status == "" {
		in.Status = "running"
	}
	// S60 MQ15: the branch shape is PINNED for this loop's whole life, at
	// the one moment its settings can still be asked. A later settings
	// edit changes the next loop, never this one — an in-flight loop whose
	// branches moved under it is the orphan incident this pin exists to
	// prevent (F14: the first and only writer of those four columns).
	// Resolution ORDER, verbatim: a request/base the start carries (its
	// `with["base_branch"]`) wins; then the component's effective
	// default_branch (EffectiveForRoot is the SOLE reader of settings);
	// then refs/remotes/origin/HEAD through the scrubbed runner; then
	// empty — and empty keeps today's derivation exactly (no base is ever
	// REQUIRED of a loop.start; the queue refuses unknown bases at enqueue
	// instead, which is a door with a person or a plan in front of it).
	in.BranchMode = "standard"
	prefix := ""
	effDefault := ""
	if eff, err := s.EffectiveForRoot(in.ComponentRoot); err == nil {
		prefix = eff.BranchPrefix.Value
		effDefault = eff.DefaultBranch.Value
	}
	resolvedDefault := effDefault
	if resolvedDefault == "" {
		root := in.Root
		if root == "" {
			root = in.ComponentRoot
		}
		resolvedDefault = workspace.OriginDefaultBranch(context.Background(), root)
	}
	in.DefaultBranch = resolvedDefault
	in.BaseBranch = with["base_branch"]
	if in.BaseBranch == "" {
		in.BaseBranch = resolvedDefault
	}
	// The name is built ONLY under the prefix rule; a component carrying
	// junk (set before the seam validated, or through an old door) pins no
	// name and the loop keeps deriving today's names — the refuse-the-
	// setting-not-the-loop order, same as the base being optional.
	if names, err := action.NewBranchNamesWith(prefix, in.Slug, "feature"); err == nil {
		in.FeatureBranch = names.FeatureBranch()
	} else {
		in.BranchMode = "" // nothing pinned: the row says so honestly
	}
	// The one assignment that makes QAP-11 checkable: the declaration enters
	// the row in the same transaction as the blueprint pin, and no later path
	// writes it.
	in.Attended = attended
	l, err := s.CreatePinnedLoop(in, bp.ID, bp.Version, string(raw), jsonState)
	if err != nil {
		return LoopState{}, err
	}
	pin, err := s.ReadPinnedLoop(l.ID)
	if err != nil {
		return LoopState{}, err
	}
	return FromPinned(pin)
}
func countInvalid(r blueprint.Report) int { _, n, _ := r.Counts(); return n }

// Pinned reads only the durable copy. An edited or deleted source file never
// changes a running loop's interpretation.
func Pinned(_ context.Context, s *store.Store, id string) (LoopState, error) {
	pin, err := s.ReadPinnedLoop(id)
	if err != nil {
		return LoopState{}, err
	}
	st, err := FromPinned(pin)
	if err != nil {
		return LoopState{}, PinRefusal{Kind: "pin-unparseable", Detail: err.Error()}
	}
	return st, nil
}

// futureHandlerRefusal is the named-follow-up door: a step whose agent is a
// declared future body is a VALID blueprint reference that cannot run today,
// and a durable loop guaranteed to block at its first such step is never
// created. Pure — no store, no clock, no package state — so the door stays
// pinned after S13 T16 emptied futureAgents: the test feeds the exact
// diagnostic the validator produces for a future name (blueprint's
// TestTheThirdStateSurvivesAnEmptyFollowUpList pins the generator side).
func futureHandlerRefusal(bp blueprint.Blueprint, report blueprint.Report) (PinRefusal, bool) {
	for _, lane := range bp.Lanes {
		paths := []string{"lanes[" + lane.ID + "].step.agent"}
		for _, step := range lane.Ladder {
			paths = append(paths, "lanes["+lane.ID+"].ladder["+step.ID+"].agent")
		}
		for _, path := range paths {
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Path == path && diagnostic.Kind == "agent-registry" {
					return PinRefusal{Kind: "unavailable-handler", Detail: fmt.Sprintf("%s cannot run: %s", diagnostic.Path, diagnostic.Msg)}, true
				}
			}
		}
	}
	return PinRefusal{}, false
}

// pinSource resolves, loads and validates a blueprint exactly as loop start
// does, and returns its parsed form and raw bytes. Extracted from Start at
// S39 T8 so a SECOND blueprint pinned onto a resting loop (StartRun) is
// validated by the SAME code as the first — a run that could bypass
// futureHandlerRefusal or the skill gate would be a second door nobody
// audited. Behavior-neutral by the pin_test.go suite, which pins every
// refusal, green across the extraction.
// PreviewBlueprint validates a blueprint for a run without pinning it
// (requires checks, the run options list — T9's dialogue asks against this
// answer, and the answer must be the one StartRun would honour).
func PreviewBlueprint(s *store.Store, name, mode string) (blueprint.Blueprint, error) {
	bp, _, err := pinSource(s, name, mode)
	return bp, err
}

// CheckBlueprint is PreviewBlueprint's yes/no — the door S40 T8's trigger
// writes walk before filing a standing instruction. It is deliberately the
// SAME pinSource the start path runs (the extraction was S39 T8's, so there
// is nothing to extract here, only a name to lend): a trigger whose
// blueprint refuses here would refuse at its first fire with the identical
// sentence, and a blueprint that passes here has passed every gate Start
// would apply.
func CheckBlueprint(s *store.Store, name, mode string) error {
	_, err := PreviewBlueprint(s, name, mode)
	return err
}

func pinSource(s *store.Store, name, mode string) (blueprint.Blueprint, []byte, error) {
	// S48 T3 (BP2/BP4): the fire-time door reads through the source — the
	// ROW of the store the start itself writes to (the caller's store, so
	// check and start can never disagree by reading two places). An
	// explicit PATH argument stays a file read: it is a person's argument,
	// not the state mirror (the same distinction the CLI validate verb
	// keeps). An unknown name refuses with the one three-part sentence,
	// never a file, never the embed (no fallback).
	var raw []byte
	if strings.ContainsAny(name, "/\\") || (strings.HasSuffix(name, ".yaml") && filepath.IsAbs(name)) {
		b, err := os.ReadFile(name)
		if err != nil {
			return blueprint.Blueprint{}, nil, err
		}
		raw = b
	} else {
		if s == nil {
			return blueprint.Blueprint{}, nil, errors.New("loop: CheckBlueprint needs the store whose rows it checks")
		}
		row, ok, err := s.BlueprintGet(context.Background(), name)
		if err != nil {
			return blueprint.Blueprint{}, nil, err
		}
		if !ok {
			return blueprint.Blueprint{}, nil, blueprint.NewErrUnknownBlueprint(name)
		}
		raw = []byte(row.Content)
	}
	bp, err := blueprint.Load(raw)
	if err != nil {
		return blueprint.Blueprint{}, nil, PinRefusal{Kind: "invalid-blueprint", Detail: err.Error()}
	}
	report := blueprint.Validate(bp, blueprint.ValidationOptions{Mode: mode, Agents: handler.Names(), Resolvers: handler.Resolvers(), Actions: action.Names(), AgentTools: handler.ToolsFor, SkillKnown: skillKnown})
	// T0 / T11: a lane targeting a skill while agent services are unconfigured
	// (or naming a target a configured registry does not know) never becomes a
	// durable loop that is guaranteed to block at its first step. The refusal
	// is structured and names the lane, not a count. (SkillKnown is wired from
	// the agentsvc registry at T4; until a registry exists the honest answer
	// is "not configured", and these lanes refuse.)
	for _, lane := range bp.Lanes {
		paths := []string{"lanes[" + lane.ID + "].step.skill"}
		for _, step := range lane.Ladder {
			paths = append(paths, "lanes["+lane.ID+"].ladder["+step.ID+"].skill")
		}
		for _, path := range paths {
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Path == path && diagnostic.Kind == "agentsvc" {
					return blueprint.Blueprint{}, nil, PinRefusal{Kind: "unavailable-skill", Detail: fmt.Sprintf("%s cannot run: %s", diagnostic.Path, diagnostic.Msg)}
				}
			}
		}
	}
	if !report.OK(false) {
		return blueprint.Blueprint{}, nil, PinRefusal{Kind: "invalid-blueprint", Detail: fmt.Sprintf("%d invalid diagnostic(s)", countInvalid(report))}
	}
	// A future agent is a valid blueprint reference but cannot run today. Do not
	// create a durable loop that is guaranteed to block at its first such step.
	// (T10: the resolver-registry half of this check went with D13's flip —
	// `by-tech-tag` is a real check now and pins cleanly. The named-follow-up
	// half is futureHandlerRefusal below. S13 T16 shipped the last follow-up
	// and the list reached empty — the door STAYS: it is the one the next
	// named follow-up walks through, and TestFutureHandlerDoorRefusesTheNamedFollowUp
	// pins it with a synthetic diagnostic now that no real name can reach it.)
	if refusal, refused := futureHandlerRefusal(bp, report); refused {
		return blueprint.Blueprint{}, nil, refusal
	}
	return bp, raw, nil
}

// SetModelOptions records the reasoning effort and thinking switch a person chose at start on the pinned loop, so
// every model step of it carries them. Empty values leave the route's own in force. Called once, right after the loop
// is pinned and before its runner starts.
func SetModelOptions(ctx context.Context, s *store.Store, id, effort, thinking string) error {
	if effort == "" && thinking == "" {
		return nil
	}
	st, err := Pinned(ctx, s, id)
	if err != nil {
		return err
	}
	st.ReasoningEffort, st.Thinking = effort, thinking
	return save(Deps{Store: s}, st)
}
