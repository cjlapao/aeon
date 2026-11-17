// verify_actions.go — S57 T6: the verify.run code action.
//
// It is the action half of the verification gates: the loop's blueprint
// names the step, this action resolves WHAT the step proves (the
// component's effective gate set, VG3) and records WHERE the proof lives
// (a verify_run row, one transaction, VG6) — then hands the run's
// discriminable outcome back to the loop as evidence. The ENGINE half of
// the decision is mergerule (T5): nothing here decides whether the run
// is enough for a merge; it only produces the facts a rule can measure.
//
// The seams are interfaces, not the store: the action package is
// store-free by construction (the loop package imports it, and the loop
// must stay importable without the CGO store — TestVerifyPackageIsStoreFree
// pins the same direction on verify). The daemon adapts *store.Store to
// the four seams; the loop's tests adapt fakes.
//
// The attempt is ledger-derived (D-T6-8): the engine has no attempt
// counter of its own (the loop state deliberately carries none), so the
// Nth run of a loop at a position is attempt N+1 — counted from the
// finished runs the store already holds. A run is only visible finished
// (one transaction), so two dispatches of the same round race on the
// unique (loop, root, position, attempt) slot and one of them errors
// honestly instead of writing a second half-visible run.

package action

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/verify"
)

// VerifyRunInput is what Dispatch hands the service for verify.run: the
// code-supplied identity of the run (dir, loop, task, gate, agent) plus
// the trigger and actor the caller knows (the loop dispatches "loop" as
// the engine; a manual verb would pass "manual" and its own actor).
type VerifyRunInput struct {
	Dir     string // the loop's root — the tree the gates run in
	Loop    string // the loop's slug (the run's loop identity)
	Task    string // optional: the task the run verifies
	Agent   string // optional: the gate's agent identity (default VerifierAgent)
	Gate    string // optional: narrow the effective set to one gate (by id, then name)
	Trigger string // "loop" | "manual" (the store's closed set)
	Actor   RunActor
}

// RunActor names who ran the run — the ledger's authorship (VG6). The
// action carries its own type so the package stays store-free; the
// daemon maps it to the store's actor at the seam.
type RunActor struct {
	Kind string // the engine's kind for a loop run
	ID   string // anonymous is refused by the store, and by Dispatch
}

// VerifierAgent is the gate's default identity: the agent the policy
// verdicts are checked under when the blueprint names none.
const VerifierAgent = "verifier-engineer"

// VerifyRunOutput is the discriminable outcome Dispatch guarantees back
// as the five evidence keys (the registry's Evidence list).
type VerifyRunOutput struct {
	State   string // "pass" | "fail" | "unverified" (the closed run states)
	Reason  string // the aggregate's one-line reason (fail carries the summary)
	Summary string // the fixed-template counts
	Digest  string // the run's content digest (VG4)
	RunID   string // the ledger row's id ("" when no recorder is wired)
	HeadSHA string // the tree's head the run measured
	Attempt int    // the ledger-derived attempt number
}

// VerifyActions is the seam the registry dispatches verify.run to.
type VerifyActions interface {
	Run(ctx context.Context, in VerifyRunInput) (VerifyRunOutput, error)
}

// --- the service's seams (the daemon adapts the store to these) ----------

// ComponentResolver answers "which node does this root belong to" — the
// run's component identity (VG6: a run names the node it happened on).
type ComponentResolver interface {
	ComponentForRoot(ctx context.Context, root string) (componentID string, err error)
}

// GateReader answers "what runs here" (VG3's effective set) for one
// node. An honest empty set (an archived node, a node with no gates) is
// not an error — it is the unverified run the engine routes.
type GateReader interface {
	EffectiveGates(ctx context.Context, componentID string) ([]verify.GateSpec, error)
}

// RunRecorder is the ledger half: the attempt count and the one-transaction
// finish. A nil recorder is legal (the loop's tests wire one; a service
// without one still runs the gates and reports the run without a row).
type RunRecorder interface {
	// CountLoopRuns counts the finished loop runs of this loop at this
	// root (every position) — the attempt is count+1 (D-T6-8).
	CountLoopRuns(ctx context.Context, loopSlug, componentRoot string) (int, error)
	// Record persists the finished run and its gate results in one
	// transaction and returns the row's id.
	Record(ctx context.Context, in RecordRun) (string, error)
}

// RecordRun is the recorder's input: the run's identity, its outcome,
// and the per-gate results the store snapshots (VG6: the command text
// as it ran, so a later gate edit never rewrites the row's story).
type RecordRun struct {
	LoopSlug      string
	ComponentRoot string
	ComponentID   string
	Task          string
	Position      int
	Attempt       int
	HeadSHA       string
	Trigger       string
	Actor         RunActor
	StartedAt     time.Time
	EndedAt       time.Time
	State         string
	Reason        string
	Summary       string
	Digest        string
	Results       []verify.GateResult
}

// DoDMaterializer materializes the loop's DoD from its approved brief
// (idempotent — a second call in the same run writes nothing, D-T6-2).
// The gate's own run does not read it; the merge rule does (T5's
// evaluate reads the DoD items the store holds).
type DoDMaterializer interface {
	MaterializeDoD(ctx context.Context, loopSlug, componentRoot string) (written int, err error)
}

// VerifyService is the concrete VerifyActions: the seam-backed
// implementation the daemon wires, and what the loop's tests drive.
type VerifyService struct {
	// HeadSHA reads the tree's current head for the run's dir (the git
	// client's rev-parse behind a seam, so the action never opens a
	// repository itself).
	HeadSHA func(ctx context.Context, dir string) (string, error)

	// Component, Gates and Recorder are the store-backed seams; Recorder
	// and DoD may be nil (the gates still run and report).
	Component ComponentResolver
	Gates     GateReader
	Recorder  RunRecorder
	DoD       DoDMaterializer

	// Runner is the gate engine (verify.T4): policy verdict, spawn,
	// mapping, aggregation. A nil Runner takes verify.New().
	Runner *verify.Runner

	// Now is the run's clock (started/ended); a nil Now takes time.Now.
	Now func() time.Time

	// Actor is the identity runs are recorded under when the caller
	// passes none — the daemon wires the engine's (a loop run is the
	// engine's work). A zero Actor here with a zero Actor in the input
	// is the anonymous refusal below.
	Actor RunActor
}

var _ VerifyActions = (*VerifyService)(nil)

// Run executes the verify.run action: resolve the node, read the tree's
// head, narrow and run the effective gates, materialize the DoD (first
// run), and record the finished run. The outcome is the run's own —
// pass, fail or unverified — never an error: a red gate is a RESULT the
// loop routes, and an error is reserved for the infrastructure (the
// store, the tree, the resolver) actually failing.
func (s *VerifyService) Run(ctx context.Context, in VerifyRunInput) (VerifyRunOutput, error) {
	if in.Dir == "" {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: no dir — the gates need the tree they run in")
	}
	// A loop run needs the loop it belongs to; a MANUAL run has none —
	// the ledger's loop_slug '' is its identity (the migration says so:
	// manual runs are exempt from the loop's attempt slot), and the
	// attempt count is the root's, not a loop's.
	if in.Loop == "" && in.Trigger != "manual" {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: no loop — the run needs the loop it belongs to")
	}
	// The caller's actor wins; a loop run (Dispatch) passes none and
	// takes the engine's — anonymous is the refusal, never a default.
	actor := in.Actor
	if actor.ID == "" {
		actor = s.Actor
	}
	if actor.ID == "" {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: an anonymous actor cannot be recorded (VG6)")
	}
	if s.Component == nil {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: no component resolver wired")
	}
	if s.Gates == nil {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: no gate reader wired")
	}
	runner := s.Runner
	if runner == nil {
		def := verify.New()
		runner = &def
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	componentID, err := s.Component.ComponentForRoot(ctx, in.Dir)
	if err != nil {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: resolving the component for %s: %w", in.Dir, err)
	}
	head, err := s.HeadSHA(ctx, in.Dir)
	if err != nil {
		// A run records the commit it measured, so a tree with no commit (a fresh
		// `git init`) or no repository at all has nothing to record against. Say so
		// in words, with git's own sentence kept beneath for the diagnosis.
		return VerifyRunOutput{}, fmt.Errorf("verify.run: %s has no commit to measure — a run records the commit it checked, so make one commit in this folder first (is it a git repository with at least one commit?): %w", in.Dir, err)
	}
	gates, err := s.Gates.EffectiveGates(ctx, componentID)
	if err != nil {
		return VerifyRunOutput{}, fmt.Errorf("verify.run: reading the effective gates: %w", err)
	}
	if in.Gate != "" {
		gates = narrowGates(gates, in.Gate)
		if gates == nil {
			return VerifyRunOutput{}, fmt.Errorf("verify.run: gate %q is not in the effective set (the set is the component's, not the blueprint's)", in.Gate)
		}
		if in.Trigger == "manual" {
			// A manual try names the ONE gate a person is asking about
			// (design:286): the run's verdict is that gate's own verdict,
			// advisory or not. The no-gates sentinel belongs to the
			// loop's round, where advisory never blocks (design:93) —
			// the close-out walk found an advisory gate named by try
			// recorded as no-gates-configured without ever spawning.
			gates = asRequired(gates)
		}
	}

	// The DoD materializes on the run that first needs it — idempotent,
	// so the rework rounds write nothing (D-T6-2).
	if s.DoD != nil {
		if _, err := s.DoD.MaterializeDoD(ctx, in.Loop, in.Dir); err != nil {
			return VerifyRunOutput{}, fmt.Errorf("verify.run: materializing the definition of done: %w", err)
		}
	}

	attempt := 1
	if s.Recorder != nil {
		count, err := s.Recorder.CountLoopRuns(ctx, in.Loop, in.Dir)
		if err != nil {
			return VerifyRunOutput{}, fmt.Errorf("verify.run: counting the loop's runs: %w", err)
		}
		attempt = count + 1
	}

	agent := in.Agent
	if agent == "" {
		agent = VerifierAgent
	}
	started := now()
	res := runner.Run(ctx, verify.RunInput{Gates: gates, Agent: agent, Dir: in.Dir})
	ended := now()

	out := VerifyRunOutput{
		State:   res.State,
		Reason:  res.Reason,
		Summary: res.Summary,
		Digest:  res.Digest,
		HeadSHA: head,
		Attempt: attempt,
	}
	if s.Recorder != nil {
		id, err := s.Recorder.Record(ctx, RecordRun{
			LoopSlug: in.Loop, ComponentRoot: in.Dir, ComponentID: componentID,
			Task: in.Task, Position: 0, Attempt: attempt,
			HeadSHA: head, Trigger: in.Trigger, Actor: actor,
			StartedAt: started, EndedAt: ended,
			State: res.State, Reason: res.Reason, Summary: res.Summary, Digest: res.Digest,
			Results: res.Results,
		})
		if err != nil {
			return VerifyRunOutput{}, fmt.Errorf("verify.run: recording the run: %w", err)
		}
		out.RunID = id
	}
	return out, nil
}

// narrowGates keeps the one gate the caller named — by id first, then
// name — or nil when the name matches nothing (the caller refuses).
func narrowGates(gates []verify.GateSpec, name string) []verify.GateSpec {
	for _, g := range gates {
		if g.ID == name {
			return []verify.GateSpec{g}
		}
	}
	for _, g := range gates {
		if g.Name == name {
			return []verify.GateSpec{g}
		}
	}
	return nil
}

// asRequired reports every gate as required. Only the manual try calls it,
// and only on the ONE gate the person named: the run's verdict is that
// gate's verdict, so the runner's "no required gate" sentinel must not
// silence it (the close-out walk found the advisory gate never spawning
// without this). The stored gate row keeps its own requirement — this
// shapes one run, never the authoring.
func asRequired(gates []verify.GateSpec) []verify.GateSpec {
	out := make([]verify.GateSpec, 0, len(gates))
	for _, g := range gates {
		g.Requirement = "required"
		out = append(out, g)
	}
	return out
}
