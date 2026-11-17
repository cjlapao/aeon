package runtime

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// T9's tests. The one that matters is the absence: `RunStep` persists nothing.

func runOpts(client Streamer, tools ToolCaller, sink EventSink) RunOpts {
	return RunOpts{
		Client:      client,
		Tools:       tools,
		Sink:        sink,
		HandoffSpec: DefaultHandoffSpec(),
		StatusField: "status",
	}
}

func testRequest(tools ...model.ToolSpec) StepRequest {
	handler := testHandler()
	return StepRequest{
		Handler:  handler,
		Messages: OpeningTranscript(handler, "", "do the thing", nil),
		Manifest: ContextManifest{
			MaxIterations: MaxIterations,
			DeadlineAt:    time.Now().Add(time.Hour),
		},
		Route:         testRoute(),
		Tools:         tools,
		WorkspaceRoot: "/workspace",
	}
}

func TestRunStepReturnsExactlyOnceForEveryOutcome(t *testing.T) {
	// Exactly once for all four. The panic boundary converts a panic into a
	// result rather than into a second return, and a step that returned twice
	// would write two rows.
	cases := []struct {
		name string
		turn scriptedTurn
		want Outcome
	}{
		{"ok", scriptedTurn{Text: okHandoff}, OutcomeOK},
		{"reject", scriptedTurn{Text: "```json\n{\"status\":\"reject\",\"summary\":\"no\"}\n```"}, OutcomeReject},
		{"blocked", scriptedTurn{Text: "```json\n{\"status\":\"blocked\",\"summary\":\"stuck\"}\n```"}, OutcomeBlocked},
		{"failed", scriptedTurn{Err: errors.New("the provider refused")}, OutcomeFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := RunStep(context.Background(), testRequest(), runOpts(newFakeClient(tc.turn), nil, nil))
			if res.Outcome != tc.want {
				t.Errorf("outcome %s, want %s (reason %q)", res.Outcome, tc.want, res.Reason)
			}
			if res.Kind != ResultComplete {
				t.Errorf("kind %s, want complete", res.Kind)
			}
		})
	}
}

func TestRunStepWritesNothingToDiskForTheWholeStep(t *testing.T) {
	// K6/D15, by observation rather than by reading the code. Three later
	// slices each hand this function a transcript it did not write:
	//
	//	S6  suspends by EXITING and resumes from rows a previous process wrote
	//	S7  recovers a crashed step by replaying `status='running'` rows
	//	S9  calls it per step from `execute`, owning the bookkeeping
	//
	// A runtime that wrote its own rows could do none of those.
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	before := treeOf(t, root)

	req := testRequest(toolSpec("read"))
	req.WorkspaceRoot = root
	res := RunStep(context.Background(), req, runOpts(
		newFakeClient(
			scriptedTurn{ToolUses: []model.Block{toolUse("t1", "read", `{"path":"x"}`)}},
			scriptedTurn{Text: okHandoff},
		),
		newSpyTools("read"), &recordingSink{}))

	if res.Outcome != OutcomeOK {
		t.Fatalf("the step did not complete: %s %s", res.Outcome, res.Reason)
	}

	after := treeOf(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the step touched the filesystem.\nbefore: %v\nafter:  %v", before, after)
	}

	// And the transcript came back on the RESULT, which is how the caller
	// persists it.
	if len(res.Messages) < 3 {
		t.Errorf("the result carries %d messages; the caller has nothing to store", len(res.Messages))
	}
}

func treeOf(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func TestThisPackageImportsNothingThatCouldPersist(t *testing.T) {
	// The import side of the same claim, by AST rather than by grep: a grep
	// over text matches a package name in a comment, and this package's
	// comments talk about the store constantly.
	forbidden := []string{
		"internal/store",
		"internal/database",
		"database/sql",
		"internal/protocol", // the protocol handler calls IN, never the other way
		"internal/tools",    // T8's loop takes a ToolCaller interface instead
		"net/http",
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse the package: %v", err)
	}

	seen := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			seen++
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if strings.Contains(path, bad) {
						t.Errorf("%s imports %s", name, path)
					}
				}
			}
		}
	}
	if seen < 5 {
		t.Fatalf("parsed %d files — a check over nothing passes forever", seen)
	}
}

func TestRunOptsHasNoStoreShapedDependency(t *testing.T) {
	// S7 adds two narrow writers for crash-safe checkpoints and receipts. They
	// carry no store handle or step id; all other dependencies remain explicit.
	typ := reflect.TypeOf(RunOpts{})
	allowed := map[string]bool{
		"Client": true, "Tools": true, "Sink": true,
		"Clock": true, "HandoffSpec": true, "StatusField": true, "Context": true,
		"Receipts": true, "Checkpoint": true,
		// Policy is the S10 gate, added DELIBERATELY: it mirrors
		// Receipts/Checkpoint — an injected interface the runtime asks and the
		// CALLER implements. It holds no store handle and persists nothing; the
		// runtime imports neither internal/store nor internal/policy (K22).
		"Policy": true,
		// PolicySet is the S10 replay-honesty constructor (T10), added
		// DELIBERATELY: a function from the manifest's own frozen entries to
		// a PolicyGate. Its input is a value the runtime already carries and
		// its output is the runtime's own interface — no store-shaped type is
		// reachable from it, and K22's import guard (above) is the proof, not
		// this list.
		"PolicySet": true,
		// Steers is the S31 T4 turn-boundary drain (D3/D5), added DELIBERATELY:
		// it is the read half, narrowed to one question, and it CANNOT deliver —
		// `Pending` only reports what the queue holds. Delivery is still the
		// Checkpoint seam's exclusive act (the writer flips rows in the SAME
		// transaction that writes their text), so K6 holds: Steers can no more
		// persist than Sink can report.
		"Steers": true,
		// Telemetry is the S33 T2 capture seam (TL12), added DELIBERATELY:
		// it is TelemetrySink — a package-local interface the runtime asks
		// and the CALLER implements, the same shape as Policy and Steers. It
		// holds no store handle and persists nothing BY THE RUNTIME: the
		// concrete adapter over the store lives in internal/daemon, and the
		// K22 import guard above (runtime imports neither internal/store nor
		// the store through any path) is the proof, not this list. Its write
		// half is narrower than Receipts' by design — capture failures never
		// fail the step (T2), so it cannot even pretend to be transactional.
		"Telemetry": true,
		// MailboxDrain is the S70 MB4 peer drain, added DELIBERATELY: it
		// is Steers' twin and CANNOT deliver — `Pending` only reports what
		// the mailbox holds. The flip is still the Checkpoint seam's
		// exclusive act (checkpointMailbox names the rows to the SAME
		// writer, which stamps them in its own transaction), so K6 holds
		// by the Steers argument, quoted.
		"MailboxDrain": true,
		// MailboxAsk is the S70 MB4 ask door, added DELIBERATELY: a
		// package-local interface the runtime asks and the CALLER
		// implements (K22, the Receipts/Policy shape) — json.RawMessage in,
		// runtime.AskOutcome out. No store-shaped type is reachable from
		// it, and the K22 import guard above is the proof, not this list.
		// The filing it triggers happens above the door, in the daemon's
		// adapter — the runtime itself still persists nothing.
		"MailboxAsk": true,
		// Budget is the S55 T7 in-step check seam (E2), added DELIBERATELY:
		// context in, runtime.BudgetVerdict out — plain data both ways, the
		// Telemetry/Policy shape. The verdict's CapHit is the runtime's OWN
		// struct (no import of internal/budget: TestRuntimeStaysPure is the
		// proof, not this list), and the store-backed seam that produces the
		// verdict lives in internal/daemon. The runtime asks, the caller
		// answers; nothing here can persist.
		"Budget": true,
		// Todo is S75's nudge/guard door, added DELIBERATELY: a package-local
		// interface (ActiveOpen, NudgeAfter, GuardApplies) the runtime asks and
		// the CALLER implements — plain ids and ints out, nothing persisted by
		// the runtime (K22, the MailboxDrain shape).
		"Todo": true,
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !allowed[f.Name] {
			t.Errorf("RunOpts grew a field %q of type %s — if it can persist, K6 is broken; "+
				"if it cannot, add it to this list deliberately", f.Name, f.Type)
		}
	}
}

func TestNothingIsReadFromTheEnvironment(t *testing.T) {
	// D18 made true rather than aspirational: a test supplies a scripted
	// client and the whole step runs offline. If the runtime read a key from
	// the environment, a test machine with one set would pass while a clean
	// one failed.
	for name, src := range packageSources(t) {
		if strings.Contains(src, "os.Getenv") || strings.Contains(src, "os.LookupEnv") {
			t.Errorf("%s reads the environment", name)
		}
	}
}

func TestAPanicInTheLoopBecomesAFailedResultNamingItADefect(t *testing.T) {
	// A nil map, an index, a type assertion in a path no test reached. A
	// daemon serving three projects should not die because one step hit a
	// defect — and the person running it should be told it was a defect
	// rather than left to read "the model call failed".
	res := RunStep(context.Background(), testRequest(), RunOpts{
		Client: nil, // the loop will call Stream on it
		Tools:  nil,
	})

	if res.Outcome != OutcomeFailed {
		t.Fatalf("outcome %s, want failed", res.Outcome)
	}
	if res.Kind != ResultComplete {
		t.Errorf("kind %s", res.Kind)
	}
	if !strings.Contains(res.Reason, "defect in Aeon") {
		t.Errorf("the reason does not say it is a defect: %q", res.Reason)
	}
	// The STACK is in the reason. A panic with no stack is a bug report with
	// no information, and the transcript is where somebody will look.
	if !strings.Contains(res.Reason, "runtime.RunLoop") && !strings.Contains(res.Reason, "goroutine") {
		t.Errorf("the reason carries no stack: %q", res.Reason)
	}
}

func TestConstructionIsNotInsideThePanicBoundary(t *testing.T) {
	// The other half of the split (T9). A programmer error PANICS where the
	// stack names the caller; a step failure RETURNS. A boundary around
	// construction would turn every nil dependency into "the step failed", at
	// turn three, with the real cause four frames away.
	//
	// Asserted on the function that most wants to be forgiving: the toolset
	// builder panics on a missing workspace root, and this package's guard is
	// not in that path.
	for name, src := range packageSources(t) {
		if name != "runstep.go" && strings.Contains(src, "recover()") && name != "loop.go" {
			t.Errorf("%s recovers from a panic — the boundaries are runstep.go's two, deliberately", name)
		}
	}
}

func TestAPanickingSinkDoesNotKillTheStepAndIsCounted(t *testing.T) {
	// The sink is the CALLER's code: at S5 it forwards to S2's subscriptions,
	// at S9 it updates an app. A client that disconnected mid-step, a closed
	// channel, a nil map in a handler — none of them is a reason to lose a
	// step that is otherwise working.
	sink := &recordingSink{panicOn: EventModelDelta}
	res := RunStep(context.Background(), testRequest(), runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, sink))

	if res.Outcome != OutcomeOK {
		t.Fatalf("a panicking sink took the step down: %s %s", res.Outcome, res.Reason)
	}
	// COUNTED and reported (T9's point 3) rather than swallowed. A sink that
	// is throwing means a client is not seeing the step, and the step
	// completing anyway is correct but worth knowing about.
	if res.SinkFailures == 0 {
		t.Error("the sink panicked and the result reports no failures")
	}
}

func TestEvenTheFirstEventIsProtected(t *testing.T) {
	// The sink is wrapped BEFORE anything is emitted. A sink that panics on
	// `step.started` would otherwise take the step down before it began —
	// which is the one case a guard added later would miss.
	sink := &recordingSink{panicOn: EventStepStarted}
	res := RunStep(context.Background(), testRequest(), runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, sink))

	if res.Outcome != OutcomeOK {
		t.Fatalf("a panic on the first event killed the step: %s %s", res.Outcome, res.Reason)
	}
	if res.SinkFailures == 0 {
		t.Error("the first event's panic was not counted")
	}
}

func TestANilSinkMeansTheStepStillRuns(t *testing.T) {
	// A step nobody is watching still runs. The alternative — requiring a
	// sink — would make the CLI construct one to discard.
	res := RunStep(context.Background(), testRequest(), RunOpts{
		Client: newFakeClient(scriptedTurn{Text: okHandoff}),
	})
	if res.Outcome != OutcomeOK {
		t.Fatalf("outcome %s: %s", res.Outcome, res.Reason)
	}
	if res.SinkFailures != 0 {
		t.Errorf("%d sink failures with no sink", res.SinkFailures)
	}
}

func TestStepStartedComesFirstAndNamesWhatTheStepWasGiven(t *testing.T) {
	sink := &recordingSink{}
	req := testRequest(toolSpec("read"), toolSpec("other"))
	req.Manifest.Files = []FileHash{{Path: "a.md"}, {Path: "b.md"}}

	RunStep(context.Background(), req, runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, sink))

	events := sink.all()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	if events[0].Kind != EventStepStarted {
		t.Errorf("the first event is %s", events[0].Kind)
	}
	// The counts are in the summary, because "step.started" on its own tells a
	// watcher nothing about what the step is working with.
	if !strings.Contains(events[0].Summary, "2 declared file(s)") ||
		!strings.Contains(events[0].Summary, "2 tool(s)") ||
		!strings.Contains(events[0].Summary, "(read, other)") {
		t.Errorf("the summary does not name the counts: %q", events[0].Summary)
	}
	if events[len(events)-1].Kind != EventStepDone {
		t.Errorf("the last event is %s", events[len(events)-1].Kind)
	}
}

func TestTheDeadlineCanBeExceededWithoutWaiting(t *testing.T) {
	// The clock is injected, so the 30-minute default is testable in
	// microseconds. A test that actually waited would be a test nobody runs.
	req := testRequest()
	req.Manifest.DeadlineAt = time.Now().Add(DefaultDeadline)

	client := newFakeClient(scriptedTurn{Text: okHandoff})
	opts := runOpts(client, nil, nil)
	opts.Clock = func() time.Time { return time.Now().Add(DefaultDeadline + time.Minute) }

	res := RunStep(context.Background(), req, opts)
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome %s, want blocked: %s", res.Outcome, res.Reason)
	}
	if !strings.Contains(res.Reason, "DeadlineAt") {
		t.Errorf("the reason does not name the bound: %q", res.Reason)
	}
	// And nothing was spent discovering it: bounds are checked BEFORE the
	// call, and a step with no room should cost nothing more.
	if client.requestCount() != 0 {
		t.Errorf("%d model calls were made past the deadline", client.requestCount())
	}
}

func TestTheRequestsManifestIsCopiedNotMutated(t *testing.T) {
	// The caller's `StepRequest` is its own, and a step that incremented the
	// caller's `IterationsUsed` in place would make a retried request start
	// part-spent.
	req := testRequest()
	before := req.Manifest.IterationsUsed

	res := RunStep(context.Background(), req, runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))

	if req.Manifest.IterationsUsed != before {
		t.Errorf("the caller's manifest was mutated: %d → %d", before, req.Manifest.IterationsUsed)
	}
	// The RESULT's manifest is the spent one, which is what gets stored.
	if res.Manifest.IterationsUsed != 1 {
		t.Errorf("the result's manifest reports %d iterations", res.Manifest.IterationsUsed)
	}
}

func TestTheCallersMessagesAreCopiedNotAppendedTo(t *testing.T) {
	req := testRequest()
	before := len(req.Messages)

	RunStep(context.Background(), req, runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))

	if len(req.Messages) != before {
		t.Errorf("the caller's transcript grew from %d to %d", before, len(req.Messages))
	}
}

func TestACancelledContextIsFailedRatherThanBlocked(t *testing.T) {
	// Cancellation is not running out of room. A caller that cancelled knows
	// why, and one that did not needs to see that it happened.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := RunStep(ctx, testRequest(), runOpts(
		newFakeClient(scriptedTurn{Text: okHandoff}), nil, nil))

	if res.Outcome != OutcomeFailed {
		t.Errorf("outcome %s, want failed: %s", res.Outcome, res.Reason)
	}
	if !strings.Contains(res.Reason, "cancelled") {
		t.Errorf("the reason does not say it was cancelled: %q", res.Reason)
	}
}

func TestEveryResultCarriesTheTranscriptAndTheAccounting(t *testing.T) {
	// Whatever happened. A failed step's transcript is the thing somebody will
	// read to find out why, and a result that dropped it on failure would be
	// useless in the one case it is needed.
	cases := []scriptedTurn{
		{Text: okHandoff},
		{Text: "no handoff at all"},
		{Err: errors.New("the provider refused")},
	}

	for _, turn := range cases {
		res := RunStep(context.Background(), testRequest(), runOpts(newFakeClient(turn), nil, nil))
		if len(res.Messages) == 0 {
			t.Errorf("a %s result carries no transcript", res.Outcome)
		}
		if res.Manifest.MaxIterations == 0 {
			t.Errorf("a %s result carries no manifest", res.Outcome)
		}
		if res.Events == nil {
			t.Errorf("a %s result carries no events", res.Outcome)
		}
	}
}

func TestTheCostStateSurvivesToTheResult(t *testing.T) {
	// The fourth place this distinction has to survive (S3's D12): a `partial`
	// keeps its `Missing` list and stays distinguishable from a `known` with
	// the same number. The result is what the protocol handler writes, so a
	// collapse here is a row that claims a price nobody measured.
	res := RunStep(context.Background(), testRequest(), runOpts(newFakeClient(scriptedTurn{
		Text: okHandoff,
		Cost: model.Cost{State: model.CostPartial, USD: 0.01, Missing: []string{"cacheWrite"}},
	}), nil, nil))

	if res.Cost.State != model.CostPartial {
		t.Errorf("cost state is %q, want partial", res.Cost.State)
	}
	if len(res.Cost.Missing) != 1 || res.Cost.Missing[0] != "cacheWrite" {
		t.Errorf("the missing list did not survive: %v", res.Cost.Missing)
	}
}

func TestRunStepIsTheOnlyEntryPoint(t *testing.T) {
	// One way in, so the panic boundary and the guarded sink cannot be
	// bypassed by a caller that found `RunLoop` more convenient. `RunLoop` is
	// exported for the loop's own tests and for S6, and this test is the
	// record of that being deliberate rather than an oversight.
	src := packageSources(t)["runstep.go"]
	if !strings.Contains(src, "runGuarded") {
		t.Error("RunStep no longer goes through the panic boundary")
	}
	if !strings.Contains(src, "newGuardedSink") {
		t.Error("RunStep no longer guards the sink")
	}
}
