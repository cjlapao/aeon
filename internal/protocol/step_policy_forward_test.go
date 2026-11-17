package protocol

// RunOpts forwarding (T8 review, DEF-034). Every production RunOpts the
// table builds must carry the policy gate — non-nil when one is wired — and
// the frozen-set constructor on the RESUME path. T8's protocol tests drove a
// scriptedRunner with nil gates throughout: a site that dropped `Policy:`
// would have changed nothing observable. Three assertions, each load-bearing
// in its own way:
//
//  1. the AST scan (every RunOpts literal in production code sets the
//     Policy field — mechanically exhaustive, catches a new site forgetting);
//  2. the Run behaviour (the gate the table was given is the gate the runner
//     RECEIVED, and the compiled request carries the server-assigned step id);
//  3. the Resume behaviour (same, plus PolicySet — the replay-honesty pair).

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/runtime"
)

// recordingGate is a NON-NIL runtime.PolicyGate — the thing nil-gate tests
// could not prove was forwarded.
type recordingGate struct {
	mu     sync.Mutex
	saw    []runtime.PolicyGateRequest
	decide func(runtime.PolicyGateRequest, time.Time) runtime.PolicyGateDecision
}

func (g *recordingGate) Decide(req runtime.PolicyGateRequest, at time.Time) runtime.PolicyGateDecision {
	g.mu.Lock()
	g.saw = append(g.saw, req)
	g.mu.Unlock()
	if g.decide != nil {
		return g.decide(req, at)
	}
	return runtime.PolicyGateDecision{Allowed: true}
}

func TestRunOptsSitesForwardThePolicyGate(t *testing.T) {
	// The behavioural half: with a NON-NIL gate wired on the table, the
	// RunOpts that reaches the runner must carry it (a dropped `Policy:`
	// would arrive nil). The scripted runner captures the real struct — the
	// assertion reads what the production handler built, not what a mock
	// assumed.
	t.Run("Run", func(t *testing.T) {
		steps := &spyStepStore{}
		runner := &scriptedRunner{}
		tbl, _ := stepTable(t, runner, steps)
		gate := &recordingGate{}
		tbl.PolicyGate = gate
		if _, err := callStep(t, tbl, StepParams{Prompt: "do the thing", WorkspaceRoot: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		runner.mu.Lock()
		defer runner.mu.Unlock()
		if runner.lastOpts.Policy == nil {
			t.Fatal("RunOpts from handleStep carries NO gate — the Run site dropped the forward")
		}
		if runner.lastOpts.Policy != runtime.PolicyGate(gate) {
			t.Error("RunOpts carries a different gate than the table was wired with — the forward renamed the gate")
		}
		// The step-row identity rides the request (S10): assigned by the
		// handler after the row was created, visible to the runner.
		if runner.lastReq.StepID == 0 || runner.lastReq.StepID != steps.nextID {
			t.Errorf("compiled request StepID = %d, want the row's id (the row was %d) — refusals attribute to it",
				runner.lastReq.StepID, steps.nextID)
		}
	})

	t.Run("Resume", func(t *testing.T) {
		steps := &spyStepStore{}
		base := &scriptedRunner{}
		runner := &resumeSpy{scriptedRunner: base, entered: make(chan struct{}), release: make(chan struct{})}
		tbl, _ := stepTable(t, runner, steps)
		gate := &recordingGate{}
		sentinel := runtime.PolicyGate(&recordingGate{})
		tbl.PolicyGate = gate
		tbl.PolicySet = func(runtime.FrozenManifest) runtime.PolicyGate { return sentinel }

		request := runtime.StepRequest{Handler: runtime.DefaultHandler("choose", []string{"read"}),
			WorkspaceRoot: t.TempDir(), Route: pricedRoute("r1", "openai")}
		raw, err := json.Marshal(stepSnapshot{Request: request, LoopSlug: "step",
			ContextManifest: runtime.ContextManifest{MaxIterations: 5}})
		if err != nil {
			t.Fatal(err)
		}
		questions := &questionSpy{steps: steps,
			resume: ResumeSnapshot{Manifest: raw, Messages: []model.Message{
				{Role: model.RoleSystem, Content: []model.Block{model.TextBlock("system")}}}},
			answer:   AnsweredQuestion{Question: runtime.PendingQuestion{ID: 9, ToolUseID: "q1"}, Answer: "yes", AnsweredAt: "2026-10-17T10:00:00Z"},
			replaced: make(chan struct{})}
		tbl.Questions = questions
		initializedOnce(t, tbl, "c1")
		if _, err := tbl.Dispatch(context.Background(), "c1", MethodAnswer,
			mustJSON(t, AnswerParams{StepID: 4, Answer: "yes"})); err != nil {
			t.Fatal(err)
		}
		close(runner.release)
		<-questions.replaced

		runner.mu.Lock()
		defer runner.mu.Unlock()
		if runner.lastOpts.Policy == nil {
			t.Fatal("RunOpts from continueAnsweredStep carries NO gate — the Resume site dropped the forward")
		}
		if runner.lastOpts.Policy != runtime.PolicyGate(gate) {
			t.Error("the resume RunOpts carries a different gate than the table's")
		}
		if runner.lastOpts.PolicySet == nil {
			t.Fatal("the resume RunOpts carries NO PolicySet — a frozen manifest would replay against the " +
				"live store, which is D12's drift (T10 wired the pair)")
		}
		if got := runner.lastOpts.PolicySet(runtime.FrozenManifest{}); got != sentinel {
			t.Error("PolicySet is not the table's constructor — the forward swapped it")
		}
		if runner.lastReq.Request.StepID != 4 {
			t.Errorf("resumed request StepID = %d, want the row being continued (4) — the server re-asserts it",
				runner.lastReq.Request.StepID)
		}
	})
}

func TestEveryRunOptsLiteralSetsPolicy(t *testing.T) {
	// The mechanical half: every `runtime.RunOpts{…}` (or RunOpts{…})
	// composite literal in NON-TEST production code under internal/ sets
	// the `Policy:` field. Behavior tests prove the three known sites work;
	// this scan is what fails a FOURTH site that forgets, before it ships.
	root := filepath.Join("..", "..", "internal")
	fset := token.NewFileSet()
	sites, missing := 0, []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("parse %s: %w", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := ""
			switch t := lit.Type.(type) {
			case *ast.Ident:
				name = t.Name
			case *ast.SelectorExpr:
				name = t.Sel.Name
			}
			if name != "RunOpts" {
				return true
			}
			sites++
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Policy" {
						return true
					}
				}
			}
			missing = append(missing, fset.Position(lit.Pos()).String())
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk the tree: %v", err)
	}
	// The scan must not be over nothing: today the sites are the two in
	// protocol/step.go and the one in daemon/recover.go.
	if sites < 3 {
		t.Fatalf("found only %d RunOpts literals — the scan is looking at the wrong tree and certifies nothing", sites)
	}
	if len(missing) > 0 {
		t.Errorf("these RunOpts literals set NO Policy field: %v — every RunOpts must forward the gate "+
			"(a site without Policy: is an ungated dispatch path)", missing)
	}
	// And the fields the known sites forward must include the replay pair on
	// resume sites (PolicySet is the frozen-set constructor).
	var resumeSites, policySetSites int
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			// `runCtx` replaced the inline `context.Background()`, S9's
			// stop/continue work: the call is now tracked under its own
			// cancellable context (stepcancel.go) so `step.stop` can reach
			// a resumed step too, same as a fresh one. The literal text
			// moved; what it is counting did not.
			resumeSites += strings.Count(string(src), "runner.Resume(runCtx, req, runtime.RunOpts{")
			policySetSites += strings.Count(string(src), "PolicySet: t.PolicySet")
			return nil
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk for resume sites: %v", err)
	}
	if resumeSites != policySetSites {
		t.Errorf("%d resume RunOpts literals but %d forward PolicySet — a resume site without the frozen-set "+
			"constructor replays against today's store (D12)", resumeSites, policySetSites)
	}
}

func TestPolicyGateDecisionReachesNothingFromPrompt(t *testing.T) {
	// The protocol must not put anything model-shaped into a gate question:
	// the Table's fields are injected interfaces and functions — no field
	// whose value flows from a step's prompt reaches PolicyGateRequest. This
	// asserts the TYPE stays closed (mirror of the runtime test, from the
	// wiring side: the forwarder cannot widen what it forwards).
	typ := reflect.TypeOf(runtime.PolicyGateRequest{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8 {
			t.Errorf("PolicyGateRequest.%s is byte-slice shaped — raw content has no business in the "+
				"gate question (PL9)", f.Name)
		}
	}
}
