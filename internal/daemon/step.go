package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/search"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// The step's composition point.
//
// # Four packages that deliberately cannot see each other
//
//	internal/runtime   the loop. Imports `model`, and NOT `tools` or `store`
//	internal/tools     the tools. Imports `model`, and NOT `runtime`
//	internal/protocol  the method table. Imports neither transport nor storage
//	internal/store     the rows. Imports `model` and nothing else
//
// Each is a leaf on purpose, and the reasons are different: the runtime must
// persist nothing (K6) so S6 and S7 can hand it transcripts; the tools must not
// depend on the loop so S11 can replace their policy; the protocol must import
// no transport so S19 can mount it on HTTP.
//
// So something has to join them, and it is this package — the one that already
// knows what a socket is. Everything here is adaptation: a `tools.Result`
// becomes a `runtime.ToolOutcome`, a `*store.Store` becomes the four methods
// the step handler writes through.
//
// It is dull by design. Dull adaptation in one file beats a shared type that
// four packages all have an opinion about.

// The compiler's check that these adapters are what the protocol asked for.
// Without it, a signature drifting here would fail at the wiring in daemon.go
// with a message about `*Table` rather than about this file.
var (
	_ protocol.StepRunner    = StepRunner{}
	_ protocol.StepStore     = (*StepStore)(nil)
	_ protocol.QuestionStore = (*QuestionStore)(nil)
)

// StepRunner adapts the runtime to the protocol's interface.
type StepRunner struct {
	// PolicyList fetches the step's EVALUATED policy set, once, at assembly
	// (S10, D12). It is wired by daemon.New whenever a store exists; the
	// zero-value StepRunner (tests, storeless mode) leaves it nil and the
	// manifest freezes nothing — the pre-S10 shape, honestly empty rather
	// than fetched from somewhere nobody named.
	//
	// The fetch happens when NO SET WAS SUPPLIED (deps.Policies == nil),
	// never when the supplied set is merely empty (DEF-042): a caller that
	// hands over an explicit empty set means "consulted and found empty — do
	// not go look", and overriding that with a live fetch is exactly how a
	// zero-policy step recovered its permissions from today's table.
	//
	// The fetch lives HERE, on the daemon's side of the Compile seam, because
	// this is the first point that has both the store and the step: the
	// manifest must freeze the SAME set the gate decides from, and a second
	// fetch anywhere else is a second instant at which they could disagree.
	PolicyList func(ctx context.Context, at time.Time) ([]runtime.PolicyEntry, error)

	// OperatorPresent reads, ONCE per step run, whether the step's loop has
	// PINNED a human who may answer questions — S11's collapse of the store's
	// three-state declaration (store.LoopAttended) into the one plain bool
	// the runtime consumes (StepRequest.HasOperator). Wired by daemon.New
	// whenever a store exists; nil means the storeless test daemon, where
	// nobody can be vouched for and every ask is therefore decided as a deny
	// on the spot — fail-closed, the same direction the gate takes when it
	// cannot read.
	//
	// The read is the adapter's, never the runtime's (K22): the loop row is
	// the declaration's only home, the runtime must not reach a store, and
	// the word for the state stays out of internal/runtime entirely — the
	// step sees a bool, not a three-state world it could start deciding.
	OperatorPresent func(ctx context.Context, stepID int64) bool

	// AutoApprove reads, ONCE per step run, what a person pre-approved for the step's loop (full access, or
	// named tools): the loop's stored mode as plain data for StepRequest.AutoApprove. Nil, an unreadable
	// mode or no mode all answer nil — the default, ask — never a widening.
	AutoApprove func(ctx context.Context, stepID int64) *runtime.AutoApprove

	// CommandVerdict is the step-side spawn-key seam (T13, G16): BuildTools
	// closes it over the step's agent and frozen manifest and hands the
	// result to `tools.ToolContext.CommandVerdict`, which is the ONLY thing
	// bash may mint a Spawn token from. Wired by daemon.New whenever a store
	// exists (CommandVerdictFor); nil means the storeless test daemon, and
	// bash answers that nil honestly — a toolset with no policy source
	// refuses to spawn rather than spawning unasked.
	//
	// It is a function field for the same reason PolicyList is: the tools
	// package must not reach a store (the onion), the runtime must not see
	// policy at all (K22), and the daemon is the one package that may join
	// them. The arguments are the pair the step's gate decides with — the
	// agent identity the gate is asked with (server-side, never from tool
	// input) and the manifest's frozen set (empty-and-unmarked means the
	// live store answers) — plus the exact command.
	CommandVerdict func(agent string, frozen runtime.FrozenManifest, command string) (policy.Verdict, bool)

	// Records is S38 T8's seam: the daemon fills it (T9 wires the store's
	// records calls, step identity read from the stepctx the runtime stamps at
	// dispatch) and BuildTools copies it verbatim into every step's
	// ToolContext. Nil means this daemon runs without a store — the tools
	// answer "records are unavailable in this runtime", never an empty table
	// dressed up as a fact.
	Records tools.RecordsInvoker
	// Todo is S75 T2's seam: the loop's persisted todo list, copied into every
	// step's ToolContext. Nil (a storeless daemon) leaves the tool keeping the
	// list for the step only, and saying so.
	Todo tools.TodoStore
	// Docs is the `doc_*` tools' seam: the loop's documents, copied into every
	// step's ToolContext. Nil (a storeless daemon) leaves the tools answering
	// that documents are unavailable.
	Docs tools.DocStore
	// LoopContext renders the loop's index for the `loop_context` tool and for the compile's `Loop context`
	// section. Nil (a storeless daemon) means neither.
	LoopContext *LoopContextService
	// Env is the `env_*` tools' seam: the variable manager, copied into every
	// step's ToolContext. Nil leaves the tools answering that variables are
	// unavailable.
	Env tools.EnvStore
	// TodoActive reads the loop's active list for the compile's `Open todo list`
	// section (S75 TD7). Nil means no store: no section.
	TodoActive func(ctx context.Context, loopSlug, componentRoot string) (items []tools.TodoEntry, active bool)
	// MaxIterations reads the daemon-wide step bound (setting step.maxIterations)
	// when a step starts; 0 means no bound. Nil keeps the runtime's compiled
	// default (a storeless daemon).
	MaxIterations func(ctx context.Context) int
	// AgentMaxIterations reads the agent's own bound from its metadata
	// (`maxIterations`): ok is false when the agent sets none. Nil: no agent
	// bounds.
	AgentMaxIterations func(agent string) (n int, ok bool)
	// Deadline reads the working-time window of a step (setting step.deadline):
	// ok false keeps the compiled default; a zero duration with ok true means no
	// deadline. Nil: the compiled default.
	Deadline func(ctx context.Context) (window time.Duration, ok bool)
	// ExecResolver is S59 T5's execution-environment seam: the door BuildTools
	// closes into `tools.ToolContext.Exec`, which is the ONLY thing bash may
	// obtain an executor (and therefore a child environment) from.
	//
	// Nil means this daemon was built without environments — a storeless test
	// daemon, or a build that has not wired the plane — and bash answers that
	// nil the way it answers a nil CommandVerdict: a tool that cannot ask what
	// a command runs WITH spawns nothing. Fail-closed, and the same shape on
	// purpose, because a tool that guessed an environment would be a daemon
	// running model-driven commands in whatever the process happens to hold.
	ExecResolver *StepExecResolver

	// ContradictionCheck is S18 T3's use-time gate for the step path (O3,
	// D6): it consults the cached probe verdicts and returns the refusal
	// sentence when a declared capability has been contradicted, or the
	// empty string to let the step run. Wired by daemon.New whenever a
	// store exists (ContradictionCheckerFor); nil is the storeless test
	// mode, where no verdict was ever earned and there is nothing that
	// could contradict — the honest zero, not a forgotten wire.
	//
	// It is a function field for the K22 reason every seam here has one:
	// the runtime must not reach a store or know what a probe is, and the
	// daemon is the only package allowed to join them.
	ContradictionCheck func(route model.Route) string

	// ThinkingCount persists a step's thinking-drop total (S18 T5, M4/O1):
	// the number the adapters withheld-to, the client collected, and the
	// loop accumulated. It is called once per run, only when blocks actually
	// fell and the run belongs to a step row. Wired by daemon.New whenever a
	// store exists (ThinkingRecorderFor); nil is the storeless test mode —
	// the count still rides the returned result for the caller, it simply
	// has no record to reach.
	//
	// A function field for the same K22 reason as every seam here: the
	// runtime accumulates the number and must not reach a store to file it.
	ThinkingCount func(stepID int64, dropped int)

	// Search is the retrieval resolver (S22 T7, V7/R6), typed in
	// internal/search so this package — and the runtime behind the Compile
	// seam — never import the hunted. Wired by daemon.New from
	// Options.Search (the SAME function value the `search` tool and the
	// `search.query` socket method hold, T6's one-resolver rule extending
	// to the third door); nil means this binary has no retrieval, and a
	// step that declares one is refused at construction naming the absence
	// rather than skipping the section.
	Search search.SearchResolver

	// CorpusStatus and Reindex are the S22 T8 doors on the runner (D15):
	// the SAME function values the socket methods hold — the step's
	// tool-side view and the socket's are one implementation seen through
	// two doors. Reindex here is the EXPLICIT door only: no step path
	// calls it (V21), it travels so the identity tests see all doors.
	CorpusStatus search.StatusFunc
	Reindex      search.ReindexFunc

	// OnWorkspaceWrite is the loop's own write signal (V8, Q7): BuildTools
	// calls it from the tool-result conversion point after a successful
	// `write`/`edit` (and the `write_file` spelling) that carries a path.
	// It is fire-and-forget and never blocks the step (V18/D10) — the
	// tracker drops duplicate passes and debounces bursts so a step that
	// writes thirty files asks for one reindex, not thirty. Nil (no
	// retrieval linked) means the write goes unannounced, which is the
	// no-retrieval state, not a lost signal.
	OnWorkspaceWrite func(root, path string)

	// StepMCP is S36's step seam (MD1), the value BuildTools calls to build
	// a step's MCP bundle. Wired by daemon.New from Options.StepMCP (the
	// SAME value the entry point hands the daemon — one implementation, the
	// daemon's step door is the only other consumer). A func value, not an
	// import: internal/daemon must never import internal/mcp (MC11). Nil
	// leaves BuildTools byte-identical to today (MD8) — the unwired state,
	// not a pretend. The bundle it returns is reaped at the four BuildTools
	// callers (T4, MD5).
	StepMCP func(ctx context.Context, agent string, frozen runtime.FrozenManifest, at time.Time) (*MCPBundle, error)
}

// Compile assembles a step, reading declared files from disk.
//
// The `ReadFile` the runtime gets is CLOSED OVER THE WORKSPACE ROOT and goes
// through the one path resolver (W2, I1). That is what stops a declared file
// escaping the workspace: assembly reads through the same refusals a tool does,
// rather than through `os.ReadFile` on a path somebody wrote in a handler.
func (r StepRunner) Compile(ctx context.Context, in runtime.CompileInput, deps runtime.ContextDeps) runtime.CompileResult {
	if deps.ReadFile == nil {
		root := in.WorkspaceRoot
		deps.ReadFile = func(rel string) ([]byte, error) {
			resolved := tools.Resolve(root, rel)
			if !resolved.OK {
				return nil, fmt.Errorf("%s", resolved.Reason)
			}
			return readFileAt(resolved.Abs)
		}
	}
	// The policy set freezes at first assembly — ONCE, at one instant, and
	// pass-through from here on (D12). The fetch fires when the caller
	// supplied NO set, not when it supplied an EMPTY one (DEF-042): the
	// empty set is an answer — frozen-empty, do not go look — and a live
	// fetch overriding it lets a zero-policy step re-decide its own past
	// against today's table. A set that cannot be read FAILS the
	// construction: a step running blind about what binds it is the informing
	// half of PL9 silently absent, and "no policy section" would read to the
	// model as "nothing forbids you". Fail-closed says "this step could not
	// be assembled" instead.
	if r.PolicyList != nil && deps.Policies == nil {
		ctx, cancel := context.WithTimeout(context.Background(), policyGateTimeout)
		defer cancel()
		entries, err := r.PolicyList(ctx, time.Now())
		if err != nil {
			return runtime.CompileResult{Reason: fmt.Sprintf(
				"the policy set in force could not be read (%v); the step's manifest cannot freeze what it "+
					"was never given, and a step assembled without the section that says what binds it is a "+
					"step running blind (D12)", err)}
		}
		deps.Policies = entries
	}
	// The retrieval seam (S22 T7, V7/R6): CLOSED OVER THE WORKSPACE ROOT, the
	// same closure discipline as ReadFile — the runtime never learns what a
	// root is, and the resolution instant is exactly this compile. Resume and
	// recovery never compile, which is how "resolution happens once" is a
	// wiring fact rather than a rule to remember (N7; asserted at zero by
	// TestResumeDoesNotReResolve).
	if deps.Search == nil && r.Search != nil {
		root := in.WorkspaceRoot
		seam := r.Search
		deps.Search = func(c context.Context, corpus, query string, limit int) (search.SearchAnswer, error) {
			return seam(c, root, corpus, query, limit)
		}
	}
	if in.MaxIterations == 0 {
		in.MaxIterations = r.resolveMaxIterations(ctx, in)
	}
	// The working-time window (a question's wait is not counted, see ResumeStep).
	if r.Deadline != nil && deps.DeadlineWindow == 0 && !deps.NoDeadline {
		if window, ok := r.Deadline(ctx); ok {
			if window == 0 {
				deps.NoDeadline = true
			} else {
				deps.DeadlineWindow = window
			}
		}
	}
	// S75 TD7: the loop's active todo list, when there is one, rides the compile.
	if r.TodoActive != nil && in.TodoSection == nil && in.LoopSlug != "" {
		if items, ok := r.TodoActive(ctx, in.LoopSlug, in.ComponentRoot); ok && len(items) > 0 {
			open := 0
			for _, it := range items {
				if it.Status != "done" {
					open++
				}
			}
			in.TodoSection = &runtime.TodoSection{Text: tools.RenderTodoEntries(items), Open: open, Total: len(items)}
		}
	}
	// The loop's index (documents, tasks, answers, tools) rides every loop-bound step, so an agent starts knowing
	// what the loop has and that it lives in the database.
	if r.LoopContext != nil && in.LoopContext == "" && in.LoopSlug != "" {
		in.LoopContext = r.LoopContext.RenderFor(ctx, in.LoopSlug, in.ComponentRoot, in.Handler.Name, in.Granted)
	}
	return runtime.CompileRequest(ctx, in, deps)
}

// Run executes a compiled step.
//
// The operator bool is stamped HERE — at step assembly, once, from the pinned
// declaration — and never re-read per tool call: an ask suspends on the same
// answer its step started with.
func (r StepRunner) Run(ctx context.Context, req runtime.StepRequest, opts runtime.RunOpts) runtime.StepResult {
	// S18 T3 (O3, D6): a cached contradiction refuses the step BEFORE its
	// first request is paid for — the use-time half of refusing twice, the
	// one that actually protects Friday's loop from Monday's probe result.
	// The sentence arrives verbatim in `reason` (the one ContradictionSentence
	// produces, so every surface that prints it prints the probe's words).
	if r.ContradictionCheck != nil {
		if sentence := r.ContradictionCheck(req.Route); sentence != "" {
			return runtime.StepResult{
				Kind:     runtime.ResultComplete,
				Outcome:  runtime.OutcomeFailed,
				Reason:   sentence,
				Messages: req.Messages,
				Manifest: req.Manifest,
			}
		}
	}
	req.HasOperator = r.operatorPresent(ctx, req.StepID)
	req.AutoApprove = r.autoApprove(ctx, req.StepID)
	res := runtime.RunStep(ctx, req, opts)
	// The drop count files itself where a person reads the step (S18 T5,
	// O1): a transcript that kept every block while the wire carried fewer
	// is invisible unless the record says so. Zero is not written — the
	// column's NULL already answers "nothing fell" honestly.
	if r.ThinkingCount != nil && res.ThinkingDropped > 0 && req.StepID != 0 {
		r.ThinkingCount(req.StepID, res.ThinkingDropped)
	}
	return res
}

// ThinkingRecorderFor builds the StepRunner's drop-count recorder for a
// store (S18 T5). A write failure is SPOKEN through the logger and not
// fatal: the step ran, its cost is real, and a lost counter is an
// incompleteness of the record rather than a reason to fail the step — the
// same ranking FinishStep's trim failures get, said rather than swallowed.
func ThinkingRecorderFor(s *store.Store, log Logger) func(int64, int) {
	if s == nil {
		return nil
	}
	if log == nil {
		log = discardLogger{}
	}
	return func(stepID int64, dropped int) {
		if err := s.SetStepThinkingDropped(stepID, dropped); err != nil {
			log.Infof("step %d: %d thinking blocks were dropped and the count could not be recorded: %v",
				stepID, dropped, err)
		}
	}
}

// ContradictionCheckerFor builds the StepRunner's use-time probe check for
// a store. The rules it enforces are model's (Route.FirstContradiction —
// the declaration leads, the row only evidences); this is the wiring that
// lets the daemon's step path consult them.
//
// A cache that cannot be read REFUSES (fail-closed, the same direction as
// the ask path's refusal): sending the request anyway could replay the
// exact call a probe already proved would fail, and "we could not look" is
// not a licence to proceed. The refusal is spoken through the logger too —
// a broken cache should be noticed while it is still fixable.
func ContradictionCheckerFor(s *store.Store, log Logger) func(model.Route) string {
	if log == nil {
		log = discardLogger{}
	}
	return func(route model.Route) string {
		if s == nil || len(route.WireCapabilities) == 0 {
			return "" // nothing declared: nothing a row could contradict
		}
		rows, err := s.ProbeResults(route.ID)
		if err != nil {
			log.Infof("probe cache for route %q cannot be read, so the step is refused rather than trusted: %v", route.ID, err)
			return fmt.Sprintf("refused: route '%s' has a probe cache that cannot be read (%v) — proceeding could replay a call a probe already proved would fail; `aeon routes probe %s` re-establishes it",
				route.ID, err, route.ID)
		}
		if row, bad := route.FirstContradiction(rows); bad {
			return model.ContradictionSentence(route.ID, string(row.Capability), row.ProbedAt, row.Model, row.Detail)
		}
		return ""
	}
}

// autoApprove is the guarded read of a loop's pre-approval: no seam or an unattributable step is the default.
func (r StepRunner) autoApprove(ctx context.Context, stepID int64) *runtime.AutoApprove {
	if r.AutoApprove == nil || stepID <= 0 {
		return nil
	}
	return r.AutoApprove(ctx, stepID)
}

// AutoApproveFor builds the adapter's read for a store: step row → loop → the loop's stored access mode.
// Every failure answers nil (ask): a mode that cannot be read never widens what a loop may do.
func AutoApproveFor(s *store.Store, log Logger) func(ctx context.Context, stepID int64) *runtime.AutoApprove {
	if log == nil {
		log = discardLogger{}
	}
	return func(ctx context.Context, stepID int64) *runtime.AutoApprove {
		if s == nil || stepID <= 0 {
			return nil
		}
		st, err := s.ReadStep(stepID)
		if err != nil {
			return nil
		}
		l, err := s.GetLoop(store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot})
		if err != nil {
			return nil
		}
		a, present, err := s.LoopAccessGet(ctx, l.ID)
		if err != nil {
			log.Infof("access mode: loop %s could not be read, so the default (ask) stands: %v", l.ID, err)
			return nil
		}
		if !present {
			return nil
		}
		return &runtime.AutoApprove{All: a.Mode == store.LoopAccessFull, Tools: a.Tools, By: a.SetByKind + ":" + a.SetByID}
	}
}

// operatorPresent is the one guarded call site: no seam (storeless daemon,
// hand-built test runner) or unattributable step reads anything and gets
// false — nobody is vouched for, so an ask is a deny.
func (r StepRunner) operatorPresent(ctx context.Context, stepID int64) bool {
	if r.OperatorPresent == nil || stepID <= 0 {
		return false
	}
	return r.OperatorPresent(ctx, stepID)
}

// OperatorPresentFor builds the adapter's read for a store: step row → loop
// scope → the loop's pinned declaration, through the T11 accessor. The
// collapse to one bool is fail-closed in every direction:
//
//   - declared attended       ⇒ true (a suspend can wait for an answer);
//   - declared unattended     ⇒ false;
//   - NEVER DECLARED (NULL)   ⇒ false — "nobody said" is not "somebody is
//     here", and the honest reading of an unrecorded declaration is that no
//     answerer may be assumed (D2/P31, fail-closed);
//   - any read failure        ⇒ false, spoken through the logger. A step
//     that cannot check whether anyone is watching must not suspend on the
//     assumption that someone is.
func OperatorPresentFor(s *store.Store, log Logger) func(ctx context.Context, stepID int64) bool {
	if log == nil {
		log = discardLogger{}
	}
	return func(ctx context.Context, stepID int64) bool {
		if s == nil || stepID <= 0 {
			return false
		}
		// nobodyVouched is the ONE collapse exit: every read failure says the
		// same thing — spoken, and answered false. (The GetLoop arm has no
		// public trigger a test can drive: the step→loop FK cascades, so a
		// live step's loop row cannot be removed underneath it; only a store
		// fault reaches that line. The LoopAttended arm is reached for real
		// by a canceled context — GetLoop reads without one.)
		nobodyVouched := func(what string, err error) bool {
			log.Infof("ask suspension: %s of step %d could not be read, so nobody is vouched for answering (the ask denies): %v", what, stepID, err)
			return false
		}
		st, err := s.ReadStep(stepID)
		if err != nil {
			return nobodyVouched("the step row", err)
		}
		l, err := s.GetLoop(store.Scope{LoopSlug: st.LoopSlug, ComponentRoot: st.ComponentRoot})
		if err != nil {
			return nobodyVouched("the loop", err)
		}
		declared, err := s.LoopAttended(ctx, l.ID)
		if err != nil {
			return nobodyVouched("the operator declaration", err)
		}
		return declared != nil && *declared
	}
}

func (r StepRunner) Resume(ctx context.Context, req runtime.ResumeRequest, opts runtime.RunOpts) runtime.StepResult {
	if len(req.Manifest.Files) > 0 {
		root := req.Request.WorkspaceRoot
		opts.Context.ReadFile = func(rel string) ([]byte, error) {
			resolved := tools.Resolve(root, rel)
			if !resolved.OK {
				return nil, fmt.Errorf("%s", resolved.Reason)
			}
			return readFileAt(resolved.Abs)
		}
	}
	// The operator bool is stamped again for THIS run of the step (the
	// caller has just re-asserted StepID server-side): the declaration is
	// pinned and cannot have moved, but the decision data arrives through
	// the adapter on every entry, never from stored request bytes — a
	// snapshot may predate the field, and a resumed step that assumed an
	// answerer from stale JSON is exactly the fail-open this collapses
	// against.
	req.Request.HasOperator = r.operatorPresent(ctx, req.Request.StepID)
	req.Request.AutoApprove = r.autoApprove(ctx, req.Request.StepID)
	return runtime.ResumeStep(ctx, req, opts)
}

// BuildTools constructs the granted toolset and the specs the model is shown.
//
// Returns BOTH because they are two different audiences: the `ToolCaller` is
// what the loop calls, and the specs are what the provider is told exists. A
// single return would make one of them derived, and the derivation would be
// the place a granted tool stopped being offered.
//
// The two policy arguments (T13) are the pair the step's gate decides with:
// `agent` is the server-side handler identity the gate is asked with (the
// protocol passes `Request.Handler.Name` — the same value
// runtime.runTool stamps into PolicyGateRequest, never anything from tool
// input), and `frozen` is the manifest's frozen set as gateForStep would
// route it (empty AND unmarked ⇒ the live store answers). BuildTools closes
// CommandVerdict over them per step, so every bash dispatch in this step is
// keyed by the same read the gate has. A first run passes the zero
// FrozenManifest (the manifest is not assembled yet — the seam answers from
// the live store, and any move a concurrent author makes between the gate's
// instant and this read can only lose the spawn, never gain one).
func (r StepRunner) BuildTools(granted []string, workspaceRoot, agent string, frozen runtime.FrozenManifest) (runtime.ToolCaller, []model.ToolSpec, error) {
	tc := tools.ToolContext{WorkspaceRoot: workspaceRoot, Records: r.Records}
	if r.Todo != nil {
		tc.Todo = r.Todo
	}
	if r.LoopContext != nil {
		tc.LoopContext = r.LoopContext
	}
	if r.Docs != nil {
		tc.Docs = r.Docs
	}
	if r.Env != nil {
		tc.Env = r.Env
	}
	if r.CommandVerdict != nil {
		mint := r.CommandVerdict
		tc.CommandVerdict = func(command string) (policy.Verdict, bool) {
			return mint(agent, frozen, command)
		}
	}
	if r.ExecResolver != nil {
		// Per command, not per toolset: the resolution reads the step from the
		// context the runtime stamps at dispatch, so two steps sharing a
		// toolset can never share an environment or a redactor (EV9). The
		// workspace root is the one this toolset was built for, so the
		// directory a command runs in and the environment it runs with are
		// resolved from the same root — EV15's point in one line.
		resolver := r.ExecResolver
		root := workspaceRoot
		tc.Exec = func(ctx context.Context) (tools.CommandExecutor, error) {
			return resolver.For(ctx, root)
		}
	}
	set, err := tools.BuildToolset(granted, tc)
	if err != nil {
		return nil, nil, err
	}
	tc2 := toolCaller{set: set}
	if r.OnWorkspaceWrite != nil {
		root := workspaceRoot
		notify := r.OnWorkspaceWrite
		tc2.onWrite = func(path string) { notify(root, path) }
	}
	// S36 (MD8): the unwired path is a real branch, not an implicit
	// fallthrough — a nil StepMCP returns exactly today's two values, byte-
	// identical, so a no-MCP build cannot change a step's tools in any way
	// (error wrapping, ordering, nothing).
	if r.StepMCP == nil {
		return tc2, set.Specs(), nil
	}
	// The offer is a build-time read. Q2 (the gate's instant) is settled in
	// T2; this task uses time.Now, the same instant the design names.
	// BuildTools has no ctx param (its signature is unchanged by this task),
	// so the offer runs on a background ctx; the dial budget (T3, MD4)
	// bounds it via WithTimeout, which is why the offer can never hang.
	// A first run arrives with the zero manifest: the step's set is frozen later,
	// at assembly. Judging the offer against that empty set would withhold every
	// MCP tool ("no allow rule"), so a first run is judged against the set in force
	// now — the same set assembly is about to freeze; the gate re-decides at call.
	if !frozen.Frozen && len(frozen.Entries) == 0 && r.PolicyList != nil {
		ctx, cancel := context.WithTimeout(context.Background(), policyGateTimeout)
		entries, perr := r.PolicyList(ctx, time.Now())
		cancel()
		if perr == nil {
			frozen = runtime.FrozenManifest{Entries: entries, Frozen: true}
		}
	}
	bundle, err := r.StepMCP(context.Background(), agent, frozen, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if bundle == nil {
		// An empty offer is exactly today's behavior (MD8) — the same two
		// values, not a composite with a nil bundle.
		return tc2, set.Specs(), nil
	}
	// MD10 pin #2: a bundle may never publish a dot-free spec name — it would
	// be indistinguishable from a first-party tool, and the dot is the router
	// (MD1). Refuse at construction, not at runtime.
	for _, s := range bundle.Specs {
		if !strings.Contains(s.Name, ".") {
			return nil, nil, fmt.Errorf("mcp seam: tool %q is not a qualified server.tool name", s.Name)
		}
	}
	// The offered specs join the first-party specs (the model is told both
	// exist); the composite routes by the dot (MD1/MD10).
	specs := make([]model.ToolSpec, 0, len(set.Specs())+len(bundle.Specs))
	specs = append(specs, set.Specs()...)
	specs = append(specs, bundle.Specs...)
	return compositeCaller{firstParty: tc2, bundle: bundle}, specs, nil
}

// frozenManifest is the runtime.FrozenManifest a step's gate is built from,
// read off the context manifest the same step carries. It lives beside
// BuildTools because the pair (agent, frozen) is exactly the argument pair
// gateForStep routes on — a caller that assembles half of it wrong (drops
// the marker, loses the hash) gets a spawn key from a different set than
// the gate decided with, and the disagreement always costs the spawn.
func frozenManifest(m runtime.ContextManifest) runtime.FrozenManifest {
	return runtime.FrozenManifest{Entries: m.Policies, Hash: m.PoliciesHash, Frozen: m.PoliciesFrozen}
}

// toolCaller adapts `tools.Toolset` to `runtime.ToolCaller`.
//
// Two identical-looking result types, converted here. The alternative — one
// shared type — would mean `runtime` and `tools` importing each other's
// package for a three-field struct, and the dependency would outlive the
// convenience.
type toolCaller struct {
	set tools.Toolset
	// onWrite is the V8/Q7 trigger, closed over the step's root by
	// BuildTools. The ONE conversion point sees every tool result, so the
	// write signal is a property of the only place writes become results
	// — no watcher, no polling, no second observer of the tree (N2).
	onWrite func(path string)
}

// writingTools is the Q7 answer, stated as a closed set: the tools that
// change WORKTREE files (write, edit, and the hidden compatibility spelling
// write_file). git.commitAndPush is deliberately absent — it pushes what is
// already written; the writes triggered when they happened, and it changing
// the index would embed the same bytes twice.
var writingTools = map[string]bool{"write": true, "edit": true, "write_file": true}

func (c toolCaller) Call(ctx context.Context, name string, input json.RawMessage) runtime.ToolOutcome {
	res := c.set.Call(ctx, name, input)
	if c.onWrite != nil && !res.IsError && writingTools[name] && res.Path != "" {
		c.onWrite(res.Path)
	}
	return runtime.ToolOutcome{
		Content: res.Content,
		IsError: res.IsError,
		Summary: res.Summary,
		// The write line counts cross with the result (S31 D8) — this is
		// the ONE conversion point, and the ONLY place their field names
		// appear on both sides; nothing re-derives them downstream.
		Path:         res.Path,
		LinesAdded:   res.LinesAdded,
		LinesRemoved: res.LinesRemoved,
		// S33 T6: the command facts cross here too — this is THE conversion
		// point, so the loop reads them without importing tools (I1 keeps
		// the leaves apart; the field names meet only once).
		ExitCode: res.ExitCode,
		WallMs:   res.WallMs,
	}
}

func (c toolCaller) Granted() []string        { return c.set.Granted() }
func (c toolCaller) Class(name string) string { return string(c.set.RerunClass(name)) }

// StepStore adapts the store to the four writes the step handler makes (K22).
//
// Narrow deliberately: the handler can create a running step, append a
// transcript, save a manifest and finish. It cannot write anything else, and
// that is checkable by reading this type rather than by auditing the handler.
type StepStore struct {
	store *store.Store
	// resolveChosen is the S68 (B6/B7) door's catalog check for chosen
	// steer skills: names in, canonical lines + skipped out, unknown names
	// back as an error. Set by the daemon only when a skills catalog is
	// mounted; nil means a chosen skill cannot be checked and is REFUSED.
	resolveChosen func(names []string) (*chosenSkillResolution, error)
}

// NewStepStore wraps a store for the step handler. A nil store yields nil, so a
// daemon without one wires nothing rather than something that panics.
func NewStepStore(s *store.Store) *StepStore {
	if s == nil {
		return nil
	}
	return &StepStore{store: s}
}

// CreateRunningStep writes the row BEFORE the work starts.
//
// `status='running'` is what S7's recovery query looks for, so this write is
// the thing that makes a crashed step findable. The loop is created if it does
// not exist: at S5 a step is ad-hoc and S8 brings real loops, and refusing to
// run because nobody had created a loop first would be bookkeeping standing in
// front of the work.
func (s *StepStore) CreateRunningStep(loopSlug, componentRoot, agent string) (int64, error) {
	sc := store.Scope{LoopSlug: loopSlug, ComponentRoot: componentRoot}
	if _, err := s.store.GetLoop(sc); err != nil {
		if _, createErr := s.store.CreateLoop(store.NewLoop{
			Slug:          loopSlug,
			ComponentRoot: componentRoot,
			Title:         loopSlug,
			Goal:          "ad-hoc steps (S8 brings real loops)",
		}); createErr != nil {
			return 0, createErr
		}
	}

	st, err := s.store.CreateStep(sc, store.NewStep{
		Phase:  "build",
		Agent:  agent,
		Status: store.StepRunning,
	})
	if err != nil {
		return 0, err
	}
	return st.ID, nil
}

// AppendStepMessages persists the transcript.
func (s *StepStore) AppendStepMessages(stepID int64, msgs []model.Message) error {
	return s.store.AppendMessages(stepID, msgs)
}

func (s *StepStore) SaveCheckpoint(ctx context.Context, stepID int64, msgs []model.Message, manifest json.RawMessage, cost model.Cost) error {
	return s.store.SaveRunningCheckpoint(ctx, stepID, msgs, manifest, cost)
}

func (s *StepStore) BeginReceipt(ctx context.Context, stepID int64, useID, tool, digest string) error {
	return s.store.BeginReceipt(ctx, stepID, useID, tool, digest)
}

func (s *StepStore) CompleteReceipt(ctx context.Context, stepID int64, useID, result string, isError bool) error {
	return s.store.CompleteReceipt(ctx, stepID, useID, result, isError)
}

// SaveStepContext persists the frozen manifest, BYTE FOR BYTE.
//
// # The typed hop that was here lost two fields
//
// It unmarshalled into `store.ContextManifest` and let the store re-encode —
// which dropped every field that view does not declare. The demo walk caught
// it: `step show --manifest` printed `iterations 2 of 0`, because
// `maxIterations` never reached the column.
//
// The manifest's shape belongs to the runtime (K13). This adapter's job is to
// carry it, not to restate it — so the bytes go through untouched, and the
// store refuses them if they are not JSON.
func (s *StepStore) SaveStepContext(stepID int64, manifest json.RawMessage) error {
	return s.store.SetContextManifestJSON(stepID, manifest)
}

// FinishStep records the cost and the final status.
//
// The cost goes through the store's ONE cost function, which decomposes it into
// the three columns — so a `partial` keeps its `Missing` list and stays
// distinguishable from a `known` with the same number (S4's DB11, D12). This is
// the fourth place that distinction has to survive and the first where this
// slice's flow writes it to disk.
func (s *StepStore) FinishStep(stepID int64, status, outcome string, cost model.Cost) error {
	if err := s.store.SetStepCost(stepID, cost); err != nil {
		return err
	}
	return s.store.SetStepStatus(stepID, store.StepStatus(status), outcome)
}

// SetPauseReason records, or clears, why a step is suspended with no pending
// question. See migration `0019` and the interface's own doc comment.
func (s *StepStore) SetPauseReason(stepID int64, reason string) error {
	return s.store.SetStepPauseReason(stepID, reason)
}

// readFileAt reads an already-resolved absolute path.
//
// A one-line wrapper so the `os` import stays in one place in this file, and so
// a reader can see that the only filesystem access here is a read of a path the
// resolver already approved.
func readFileAt(abs string) ([]byte, error) { return os.ReadFile(abs) }
