package action

// verify_test.go — S57 T6: the remaining named tests of the action
// package.
//
// TestMergeGateWithRuleRefusals — the extended half of the gate (VG10):
// every new refusal sentence verbatim, in the fixed collection order
// (review → checks → local → DoD → threads), each refusal BEFORE the
// forge is asked (no "MergePR" in the call log — the same ordering
// property the T1 goldens pin on the baseline), and the one allowed
// extended merge carrying the six extended evidence keys over the
// baseline five — with the rule's own method reaching the forge.
//
// TestVerifyRunIsLocalClass — the class table: verify.run is local, and
// local runs attended or not — verification never needs a person.
//
// TestVerifyRunHonoursCancel — the S55 contract at the action's own
// seam: the run's context cancelled ends the run unverified: cancelled,
// even though the gate's command would still be running.
//
// TestActionRegistryClosedSetUpdated — the closed set grew by exactly
// one (verify.run): its VG2 arg shape, its five evidence keys, its
// class and its strategy — and every name in the set is classified.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/verify"
)

// ruleHead is the PR head every extended case measures evidence against;
// staleHead is a different head — the "new commits invalidate evidence"
// half of VG10.
const (
	ruleHead  = "11111111111111111111111111111111111111111111111111111111111111"
	staleHead = "22222222222222222222222222222222222222222222222222222222222222"
)

// ruleWrap is the gate's wrapper (mergerule's, re-stated here so the
// expected sentences read as the gate speaks them — one wrapper, two
// eras).
func ruleWrap(detail string) string {
	return "forge.merge gate: " + detail + " — the merge is refused BEFORE the forge is asked (L34/D10)"
}

// extRule is a fresh NON-baseline rule: the baseline's fields plus the
// case's own, revision 1, authored — the minimum a stored row is.
func extRule(mutate func(*mergerule.Rule)) mergerule.Rule {
	r := mergerule.Baseline()
	r.Revision = 1
	r.Source = "own"
	if mutate != nil {
		mutate(&r)
	}
	return r
}

// ruleFake seeds the one PR every case merges: number 3, the 4a17 names
// (mergeArgsFor's), head pinned so the evidence can be measured against
// it.
func ruleFake() *forge.Fake {
	f := forge.NewFake()
	f.SeedPR(forge.PR{Number: 3, Head: "task/4a17-version/4a17", BaseRef: "feature/4a17-version", HeadSHA: ruleHead})
	return f
}

// approveReview gives the plain fake the one approve the forge-review
// half of every case expects (the mode-aeon cases never read it).
func approveReview(f *forge.Fake) {
	if _, err := f.PostReview(context.Background(), 3, "lgtm", forge.VerdictApprove); err != nil {
		panic("seeding the approve: " + err.Error())
	}
}

// ruleChecksForge gives the plain fake the status API with a fixed
// report — the capability a rule's named checks are measured against
// (gateForge, gate_test.go, stands in as the server's status API).
func ruleChecksForge(f *forge.Fake, checks []forge.Check) *gateForge {
	return &gateForge{Fake: f, checksN: true, checks: checks}
}

type ruleGateCase struct {
	name   string
	rule   mergerule.Rule
	token  string // the engine's review token (Args.AeonReview)
	run    *mergerule.Run
	dod    []mergerule.Item
	forge  func() (*forge.Fake, forge.Forge)
	want   []string          // wrapped sentences, in order; empty ⇒ the merge lands
	method string            // want: the merge method the forge saw (the allowed case)
	ev     map[string]string // want: the exact evidence values (the allowed case)
}

func TestMergeGateWithRuleRefusals(t *testing.T) {
	cases := []ruleGateCase{
		{
			name: "require-local-no-run",
			rule: extRule(func(r *mergerule.Rule) { r.RequireLocal = true }),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap(fmt.Sprintf("local gate verification is not green for head %s (state: no run)", ruleHead))},
		},
		{
			name: "require-local-stale-run",
			rule: extRule(func(r *mergerule.Rule) { r.RequireLocal = true }),
			run:  &mergerule.Run{ID: "vr-7", HeadSHA: staleHead, State: "pass"},
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap(fmt.Sprintf("the evidence is for %s, the PR head is %s — new commits invalidate it", staleHead, ruleHead))},
		},
		{
			name: "require-local-fail-run",
			rule: extRule(func(r *mergerule.Rule) { r.RequireLocal = true }),
			run:  &mergerule.Run{ID: "vr-8", HeadSHA: ruleHead, State: "fail"},
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap(fmt.Sprintf("local gate verification is not green for head %s (state: fail)", ruleHead))},
		},
		{
			name: "dod-not-recorded",
			rule: extRule(func(r *mergerule.Rule) { r.RequireDoD = "all_met" }),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap("no acceptance criteria are recorded for this loop")},
		},
		{
			name: "dod-not-met",
			rule: extRule(func(r *mergerule.Rule) { r.RequireDoD = "all_met" }),
			dod:  []mergerule.Item{{ID: "1", State: "met"}, {ID: "2", State: "met"}, {ID: "3", State: "unmet"}},
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap("definition of done: 1 of 3 items not met")},
		},
		{
			name: "named-check-missing",
			rule: extRule(func(r *mergerule.Rule) { r.RequiredChecks = []string{"ci"} }),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, ruleChecksForge(f, []forge.Check{{Name: "lint", State: forge.CheckSuccess}})
			},
			want: []string{ruleWrap(fmt.Sprintf("required check %q was not reported", "ci"))},
		},
		{
			name: "named-check-pending",
			// required_only: the case isolates the NAMED-check sentence —
			// under the baseline's reported_green an unrelated red (or a
			// pending) also carries today's not-green sentence, which the
			// T1 goldens already pin on the baseline side.
			rule: extRule(func(r *mergerule.Rule) {
				r.RequiredChecks = []string{"ci"}
				r.ChecksPolicy = "required_only"
			}),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, ruleChecksForge(f, []forge.Check{{Name: "ci", State: forge.CheckPending}})
			},
			want: []string{ruleWrap(fmt.Sprintf("check %s is pending (still running)", "ci"))},
		},
		{
			name: "named-check-unknown",
			rule: extRule(func(r *mergerule.Rule) {
				r.RequiredChecks = []string{"ci"}
				r.ChecksPolicy = "required_only" // isolate the named-check sentence (see the pending case)
			}),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, ruleChecksForge(f, []forge.Check{{Name: "ci", State: forge.CheckUnknown}})
			},
			want: []string{ruleWrap(fmt.Sprintf("required check %q is unknown — the forge cannot say, and unknown is not green", "ci"))},
		},
		{
			name: "no-checks-refused",
			rule: extRule(func(r *mergerule.Rule) { r.NoChecks = "refuse" }),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				approveReview(f)
				return f, ruleChecksForge(f, nil) // readable, and reports nothing
			},
			want: []string{ruleWrap("the forge reports no checks and the rule refuses to merge without them")},
		},
		{
			name: "aeon-missing",
			rule: extRule(func(r *mergerule.Rule) { r.ReviewMode = "aeon" }),
			// mode aeon: the forge reviews are not the authority — none
			// is seeded, and none is needed.
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				return f, f
			},
			want: []string{ruleWrap("Aeon's own review is required and was not recorded for this head")},
		},
		{
			name:  "aeon-stale-token",
			rule:  extRule(func(r *mergerule.Rule) { r.ReviewMode = "aeon" }),
			token: "ok@" + staleHead,
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				return f, f
			},
			want: []string{ruleWrap(fmt.Sprintf("the evidence is for %s, the PR head is %s — new commits invalidate it", staleHead, ruleHead))},
		},
		{
			name: "threads-unreportable-required",
			rule: extRule(func(r *mergerule.Rule) { r.RequireThreadsResolved = true }),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake() // the plain fake has no thread API
				approveReview(f)
				return f, f
			},
			want: []string{ruleWrap("the forge cannot report review threads and the rule requires them resolved")},
		},
		{
			name: "all-families-in-order",
			rule: extRule(func(r *mergerule.Rule) {
				r.ReviewMode = "both"
				r.RequiredChecks = []string{"ci"}
				r.RequireLocal = true
				r.RequireDoD = "all_met"
				r.RequireThreadsResolved = true
			}),
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()  // checks unreadable, threads unreportable
				approveReview(f) // the forge half passes; the aeon half does not
				return f, f
			},
			// The fixed collection order (VG11): review, checks, local,
			// DoD, threads — five families, five sentences, one join.
			want: []string{
				ruleWrap("Aeon's own review is required and was not recorded for this head"),
				ruleWrap(fmt.Sprintf("required check %q was not reported", "ci")),
				ruleWrap(fmt.Sprintf("local gate verification is not green for head %s (state: no run)", ruleHead)),
				ruleWrap("no acceptance criteria are recorded for this loop"),
				ruleWrap("the forge cannot report review threads and the rule requires them resolved"),
			},
		},
		{
			name: "allowed-extended",
			rule: extRule(func(r *mergerule.Rule) {
				r.ReviewMode = "aeon"
				r.RequireLocal = true
				r.RequireDoD = "all_met"
				r.Method = "rebase"
				r.Revision = 2
			}),
			token: "ok@" + ruleHead,
			run:   &mergerule.Run{ID: "vr-1", HeadSHA: ruleHead, State: "pass"},
			dod:   []mergerule.Item{{ID: "1", State: "met"}, {ID: "2", State: "met"}},
			forge: func() (*forge.Fake, forge.Forge) {
				f := ruleFake()
				f.SeedReviewAt(3, "lgtm", forge.VerdictApprove, "2026-10-26T09:12:00Z")
				return f, f
			},
			method: "rebase", // the rule's own — the caller named none
			// The baseline five, byte for byte, plus the six the
			// extended verdict adds (VG11). The plain fake CLAIMS the
			// status API and answers not-found: an empty READABLE
			// report (T1's split, the goldens pin it), so the checks
			// sentence is "no checks reported" and the aggregate state
			// of an empty report is unknown.
			ev: map[string]string{
				"review_state":    "approve",
				"review_by":       "fake",
				"review_at":       "2026-10-26T09:12:00Z",
				"checks":          "not-configured (N/A — no checks reported is no checks, not a red gate)",
				"threads":         "not-reported (the server reports no thread API — L34's threadless reference server)",
				"rule_revision":   "2",
				"verify_run_id":   "vr-1",
				"verify_head_sha": ruleHead,
				"dod":             "2 of 2 met",
				"checks_state":    forge.CheckUnknown,
				"aeon_review":     "ok@" + ruleHead,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, fg := tc.forge()
			a := &ForgeActions{
				Forge: fg,
				Merge: &mergeProbe{},
				RuleSource: func(ctx context.Context) (mergerule.Rule, error) {
					return tc.rule, nil
				},
				GateFacts: func(ctx context.Context, q GateFactsQuery, facts *mergerule.Facts) error {
					facts.LatestRun = tc.run
					facts.DoD = tc.dod
					return nil
				},
			}
			args := mergeArgsFor(3)
			args.AeonReview = tc.token
			res, err := a.MergePR(context.Background(), args)

			if len(tc.want) == 0 {
				if err != nil {
					t.Fatalf("the allowed extended merge was refused: %v", err)
				}
				if res.SHA == "" || res.AlreadyMerged {
					t.Fatalf("expected a fresh merge, got %+v", res)
				}
				if tc.method != "" {
					seen := ""
					for _, c := range fake.Calls() {
						if c.Method == "MergePR" {
							seen, _ = c.Args[1].(string)
						}
					}
					if seen != tc.method {
						t.Fatalf("the forge merged with method %q; the rule names %q", seen, tc.method)
					}
				}
				for k, v := range tc.ev {
					if got := res.Fields[k]; got != v {
						t.Fatalf("evidence %s = %q, want %q", k, got, v)
					}
				}
				return
			}

			if err == nil {
				t.Fatalf("expected the refusal %q, but the merge landed: %v", strings.Join(tc.want, "; "), res)
			}
			// The sentences ride the forge's one renderer (kind, why, do)
			// — the gate goldens pin that wrapper byte for byte, so the
			// test builds its expectation through the same renderer and
			// pins what the GATE adds: the sentences, and their order.
			want := forge.NewMsg(forge.KindRefused, strings.Join(tc.want, "; ")).Error()
			if err.Error() != want {
				t.Fatalf("the refusal drifted\n  got:  %s\n  want: %s", err.Error(), want)
			}
			for _, c := range fake.Calls() {
				if c.Method == "MergePR" {
					t.Fatalf("the refusal must land BEFORE the forge is asked; call log: %v", fake.Calls())
				}
			}
		})
	}
}

// TestVerifyRunIsLocalClass — verify.run is local (the class table,
// VG5), and local runs attended or not: the gates run in the tree, so
// verification never needs a person — no ask, no deny, either way.
func TestVerifyRunIsLocalClass(t *testing.T) {
	if got := Classes()[VerifyRun]; got != EffectLocal {
		t.Fatalf("verify.run's effect class is %q; want local — the gates run in the tree", got)
	}
	for _, attended := range []bool{false, true} {
		for _, allowOutward := range []bool{false, true} {
			r, err := Resolve(VerifyRun, attended, allowOutward)
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict != VerdictRun {
				t.Fatalf("attended=%v allowOutward=%v: verify.run resolves %q; want run — a verification cannot need a human",
					attended, allowOutward, r.Verdict)
			}
		}
	}
	r, err := Check(VerifyRun)
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdict != VerdictRun || r.Class != EffectLocal {
		t.Fatalf("Check(verify.run) = %+v; want local · run", r)
	}
}

// cancelComponent and cancelGates stand where the store-backed seams
// stand for this one test: the action is store-free by construction, so
// the test supplies the smallest honest answers (one node, one gate).
type cancelComponent struct{}

func (cancelComponent) ComponentForRoot(ctx context.Context, root string) (string, error) {
	return "c1", nil
}

type cancelGates struct{}

func (cancelGates) EffectiveGates(ctx context.Context, componentID string) ([]verify.GateSpec, error) {
	return []verify.GateSpec{{ID: "g1", Name: "build", Command: "go build ./...", Requirement: "required"}}, nil
}

// TestVerifyRunHonoursCancel — the S55 contract at the action's own
// seam: a cancel of the run's context ends the run unverified: cancelled
// (the closed state set), even though the gate's command would still be
// running. The run is a RESULT (the loop routes on it), never an error.
func TestVerifyRunHonoursCancel(t *testing.T) {
	// The spawn that only returns once its context ends: the model of a
	// gate mid-flight when the loop is cancelled.
	spawn := func(ctx context.Context, command string, opts tools.SpawnOptions, token policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
		<-ctx.Done()
		return tools.RunResult{ExitCode: 0}, nil
	}
	svc := &VerifyService{
		HeadSHA:   func(ctx context.Context, dir string) (string, error) { return fakeHeadSHA, nil },
		Component: cancelComponent{},
		Gates:     cancelGates{},
		Runner: &verify.Runner{
			Verdict: func(agent, command string) (policy.Verdict, bool) { return policy.Verdict{Allowed: true}, true },
			Now:     time.Now,
			Spawn:   spawn,
		},
		Now:   time.Now,
		Actor: RunActor{Kind: "system", ID: "loop-engine"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	out, err := svc.Run(ctx, VerifyRunInput{Dir: "/repo", Loop: "vm", Trigger: "loop"})
	if err != nil {
		t.Fatalf("a cancel is a result, not an error: %v", err)
	}
	if out.State != "unverified" {
		t.Fatalf("the cancelled run's state is %q; want unverified — only a completed command may read as pass or fail", out.State)
	}
	if !strings.HasPrefix(out.Reason, "unverified: cancelled") {
		t.Fatalf("the cancelled run's reason is %q; want the closed word 'cancelled'", out.Reason)
	}
	if out.RunID != "" {
		t.Fatalf("no recorder is wired, yet the run claims the ledger id %q", out.RunID)
	}
}

// fakeHeadSHA is the tree's head the cancel test measures against —
// a fixed string, like the loop harness's (the action package has no
// repository of its own: the seam stands where git stands).
const fakeHeadSHA = "abcdef0123456789abcdef0123456789abcdef01"

// TestActionRegistryClosedSetUpdated — the closed set grew by exactly
// one (L8): verify.run, with its VG2 arg shape (dir/loop required,
// task/gate/agent optional — all code-supplied), its five evidence
// keys, its local class and its strategy. Every name in the set stays
// classified: a new action without a class is a permission nobody
// granted.
func TestActionRegistryClosedSetUpdated(t *testing.T) {
	names := Names()
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			t.Fatalf("the registry lists %s twice — the closed set is a set", n)
		}
		seen[n] = true
		if c := Classes()[n]; !Valid(c) {
			t.Fatalf("action %s carries no class (or an invalid one): the closed set is classified (L8)", n)
		}
		if Strategy(n) == "" {
			t.Fatalf("action %s has no strategy: idempotency is declared, never guessed (D7)", n)
		}
	}
	if !seen[VerifyRun] {
		t.Fatalf("verify.run is missing from the registry's %d names: %v", len(names), names)
	}

	e, ok := Lookup(VerifyRun)
	if !ok {
		t.Fatal("verify.run is not in the registry")
	}
	// VG2: dir and loop required; task, gate and agent optional. Every
	// one code-supplied (Source code) — the model never names a gate.
	wantArgs := map[string]ArgSpec{
		"dir":   {Type: "string", Required: true, Source: "code"},
		"loop":  {Type: "string", Required: true, Source: "code"},
		"task":  {Type: "string", Required: false, Source: "code"},
		"gate":  {Type: "string", Required: false, Source: "code"},
		"agent": {Type: "string", Required: false, Source: "code"},
	}
	if len(e.Args) != len(wantArgs) {
		t.Fatalf("verify.run declares %d args %v; want exactly %v", len(e.Args), e.Args, wantArgs)
	}
	for name, want := range wantArgs {
		got, ok := e.Args[name]
		if !ok {
			t.Fatalf("verify.run is missing the arg %q", name)
		}
		if got != want {
			t.Fatalf("verify.run arg %s: %+v; want %+v — the model may not name a gate", name, got, want)
		}
	}
	wantEvidence := []string{"verify_state", "verify_run_id", "verify_summary", "verify_head_sha", "verify_digest"}
	if len(e.Evidence) != len(wantEvidence) {
		t.Fatalf("verify.run's evidence list is %v; want exactly %v", e.Evidence, wantEvidence)
	}
	for i, k := range wantEvidence {
		if e.Evidence[i] != k {
			t.Fatalf("verify.run's evidence key %d is %q; want %q — the keys are the contract the loop reads", i, e.Evidence[i], k)
		}
	}
	if got := Classes()[VerifyRun]; got != EffectLocal {
		t.Fatalf("verify.run's class in the registry is %q; want local", got)
	}
	if got := Strategy(VerifyRun); got != StrategyNaturallyIdempotent {
		t.Fatalf("verify.run's strategy is %q; want naturally-idempotent — a run is evidence, each round its own attempt row", got)
	}
}

// advisoryGates stands where the store stands: one node, ONE advisory
// gate — the shape the close-out walk found the runner refusing to run.
type advisoryGates struct{}

func (advisoryGates) EffectiveGates(ctx context.Context, componentID string) ([]verify.GateSpec, error) {
	return []verify.GateSpec{{ID: "g1", Name: "lint", Command: "golangci-lint run", Requirement: "advisory"}}, nil
}

func advisoryRunSvc(saw *int) *VerifyService {
	spawn := func(ctx context.Context, command string, opts tools.SpawnOptions, token policy.AllowToken, _ execenv.ChildEnv) (tools.RunResult, error) {
		*saw++
		return tools.RunResult{ExitCode: 1}, nil
	}
	return &VerifyService{
		HeadSHA:   func(ctx context.Context, dir string) (string, error) { return fakeHeadSHA, nil },
		Component: cancelComponent{},
		Gates:     advisoryGates{},
		Runner: &verify.Runner{
			Verdict: func(agent, command string) (policy.Verdict, bool) { return policy.Verdict{Allowed: true}, true },
			Now:     time.Now,
			Spawn:   spawn,
		},
		Now:   time.Now,
		Actor: RunActor{Kind: "user", ID: "operator"},
	}
}

// TestTryOfAnAdvisoryGateRunsItAndRecordsItsVerdict — design:286 says
// verify.try runs the NAMED gate once. The close-out walk found an
// advisory gate named by try recorded as `unverified: no-gates-configured`
// without ever spawning: the runner's sentinel belongs to the loop's
// round, not to the door that names the gate. The manual run must spawn
// the gate and answer with the gate's own verdict.
func TestTryOfAnAdvisoryGateRunsItAndRecordsItsVerdict(t *testing.T) {
	ran := 0
	svc := advisoryRunSvc(&ran)
	out, err := svc.Run(context.Background(), VerifyRunInput{
		Dir: "/repo", Trigger: "manual", Gate: "g1",
		Actor: RunActor{Kind: "user", ID: "operator"},
	})
	if err != nil {
		t.Fatalf("a red gate is a result, not an error: %v", err)
	}
	if ran != 1 {
		t.Fatalf("the named advisory gate spawned %d times; want 1 — try runs the one gate the person named", ran)
	}
	if out.State != "fail" {
		t.Fatalf("the manual try's state is %q; want fail — the named gate exited 1 and the run answers with the gate's own verdict", out.State)
	}
}

// TestLoopNarrowToAnAdvisoryGateKeepsTheNoGatesSentinel — the same
// advisory gate, reached the LOOP's way: advisory never blocks
// (design:93), so a round that proves nothing required does not happen.
// The walk's fix is scoped to the manual door and moves nothing here.
func TestLoopNarrowToAnAdvisoryGateKeepsTheNoGatesSentinel(t *testing.T) {
	ran := 0
	svc := advisoryRunSvc(&ran)
	out, err := svc.Run(context.Background(), VerifyRunInput{
		Dir: "/repo", Loop: "vm", Trigger: "loop", Gate: "g1",
	})
	if err != nil {
		t.Fatalf("the no-gates round is a result, not an error: %v", err)
	}
	if ran != 0 {
		t.Fatalf("the advisory round spawned %d gates; want 0 — advisory never blocks the loop", ran)
	}
	if out.State != "unverified" || out.Reason != "unverified: no-gates-configured" {
		t.Fatalf("the advisory round is %s (%q); want the no-gates sentinel unchanged", out.State, out.Reason)
	}
}
