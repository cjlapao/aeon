// gate_service_test.go — S57 T8, the daemon-side half of the gate plane: the
// three real services (verify · mergerule · dod) over the real store,
// exercised the way the socket speaks them — the peer as the actor, the
// policy gate in the run path, the store's typed refusals carried to the
// wire.
//
// The other half of T8's mandated test list is TestMigrationM4
// (internal/store), which pins the M4 migration itself. What this file pins
// is what a migration cannot: that the DOORS behave — a try runs under the
// policy gate with the scrubbed environment, the root's active loops are
// reported honestly, a preview matches the gate, refusals are typed with
// their alternatives, every write is stamped with the peer, and no method
// accepts a param for a self-chosen author.
//
// The harness follows the autonomy family's idiom (autonomyServiceEnv): a
// store over a throwaway directory, the daemon wired over it, and the
// services constructed directly so the tests speak the seam the socket
// speaks. The component root is a real git repository with one commit —
// the runner reads the head (rev-parse HEAD) before anything runs.

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/mergerule"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/store"
)

// ── the harness ──────────────────────────────────────────────────────────────

// newGateDaemonEnv builds the T8 daemon-side environment: one store, one daemon
// wired over it, one root component on a real git root, the three gate
// services, and the peer the socket would hand down (a person, uid 1000).
type gateDaemonEnv struct {
	s    *store.Store
	d    *Daemon
	root string // the component root — a git repository with one commit
	comp store.Component
	v    *VerifyService
	m    *MergeRuleService
	dd   *DoDService
	peer protocol.Actor
}

func newGateDaemonEnv(t *testing.T) *gateDaemonEnv {
	t.Helper()
	s, err := store.Open(store.Options{Dir: t.TempDir(), Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	root := gateGitRoot(t)
	cw, err := s.CreateComponent(store.NewComponent{Name: "gate root", Slug: "gate-root", Root: root})
	if err != nil || !cw.OK() {
		t.Fatalf("create the gate root component: %v %s", err, cw.Reason)
	}
	log := &wire{}
	d := New(Options{Store: s, Logger: log})
	return &gateDaemonEnv{
		s: s, d: d, root: root, comp: cw.Component,
		v:    NewVerifyService(s, log),
		m:    NewMergeRuleService(s, log),
		dd:   NewDoDService(s, log),
		peer: protocol.Actor{Kind: "user", ID: "1000"},
	}
}

// gateGitRoot is a component root the runner can actually read: a git
// repository with one commit, so LocalHEAD (rev-parse HEAD) answers before
// a gate is even asked.
func gateGitRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.email=gate@aeon", "-c", "user.name=gate", "commit", "--allow-empty", "-q", "-m", "gate root")
	return dir
}

// addGate writes one gate through the service's door and refuses to let a
// test continue over a silent refusal.
func (e *gateDaemonEnv) addGate(t *testing.T, name, command, requirement string) string {
	t.Helper()
	res, ref, err := e.v.Add(context.Background(), e.peer, protocol.VerifyAddInput{
		ComponentID: e.comp.ID, Name: name, Command: command, Requirement: requirement, TimeoutS: 30,
	})
	if err != nil {
		t.Fatalf("add gate %q: %v", name, err)
	}
	if ref != nil {
		t.Fatalf("add gate %q refused: %s · %s", name, ref.Kind, ref.Detail)
	}
	return res.Gate.ID
}

// seedCommandRule writes one command_access row through the real write
// boundary — the same door `aeon policy add` opens (PL15) — for the identity
// the gate is actually judged under: the runner's agent bound through
// bindPolicySubject, the same function the gate uses, so a rule that targets
// the unbound name cannot be the one the test is testing. The spec is
// JSON-encoded, not concatenated, so a command carrying quotes stays a
// valid row.
func (e *gateDaemonEnv) seedCommandRule(t *testing.T, id, pattern string, act policy.Action) {
	t.Helper()
	spec, err := json.Marshal(struct {
		Command string `json:"command"`
		State   string `json:"state"`
	}{Command: pattern, State: string(act)})
	if err != nil {
		t.Fatalf("encode %s's spec: %v", id, err)
	}
	subject := bindPolicySubject(action.VerifierAgent)
	rej := policy.NewStore(newPolicyRepo(e.s)).Add(context.Background(), policy.Policy{
		ID:     id,
		Type:   policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: subject},
		Action: act,
		Source: policy.Operator,
		Spec:   spec,
	}, time.Now())
	if rej != nil {
		t.Fatalf("seed %s (target %s): %s", id, subject, rej.Reason)
	}
}

// tryGate runs the gate through the service's try and refuses to let a
// test continue over a refusal.
func (e *gateDaemonEnv) tryGate(t *testing.T, gateID string) protocol.VerifyTryResult {
	t.Helper()
	res, ref, err := e.v.Try(context.Background(), e.peer, e.comp.ID, gateID)
	if err != nil {
		t.Fatalf("try %s: %v", gateID, err)
	}
	if ref != nil {
		t.Fatalf("try %s refused: %s · %s", gateID, ref.Kind, ref.Detail)
	}
	return res
}

func gateFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// gateVerdictSentence is the sentence the gate plane's policy gate renders
// for one command, taken from CommandVerdictFor — the SAME seam
// buildVerifyActionService wires the runner to (one policy source, one
// formatter). A test that asserts the gate's prose compares against this,
// never pins the sentence's text by hand:
// TestSentenceIsTheOnlyFormatterInTheModule owns that rule.
func gateVerdictSentence(t *testing.T, e *gateDaemonEnv, command string) string {
	t.Helper()
	cv := CommandVerdictFor(e.s, nil)
	v, ok := cv(action.VerifierAgent, runtime.FrozenManifest{}, command)
	if !ok {
		t.Fatalf("the verdict seam could not answer for %q", command)
	}
	return v.Sentence()
}

// ── the try: policy and environment ─────────────────────────────────────────

// TestVerifyTryRunsUnderPolicyAndScrubbedEnv — the try's two honesty laws in
// one place. The gate runs UNDER the policy gate: the empty set denies (and
// nothing spawns), an operator allow mints the token and the exact command
// runs, an explicit deny beats the allow. And the command runs with the
// gate's environment — the closed allowlist: the secret set in this process
// is not in the child, and CI is, always (GateEnv's constant).
func TestVerifyTryRunsUnderPolicyAndScrubbedEnv(t *testing.T) {
	e := newGateDaemonEnv(t)
	t.Setenv("AEON_TEST_GATE_SECRET", "leak-me-not")
	marker := func(name string) string { return filepath.Join(e.root, ".gate-ran-"+name) }

	// A — the empty policy set denies (PL5): the gate is asked, and nothing
	// is spawned. A bypass would leave the marker.
	gA := e.addGate(t, "gate-a", "touch "+marker("a"), "required")
	resA := e.tryGate(t, gA)
	if resA.Run.State != "unverified" {
		t.Fatalf("the run without policy is %q, want unverified: %+v", resA.Run.State, resA.Run)
	}
	gotA := resA.Results[0]
	if gotA.State != "unverified" || gotA.ExitCode != nil {
		t.Errorf("the refused gate reports state %q with exit code %v — no exit code may exist for a command that never ran", gotA.State, gotA.ExitCode)
	}
	// The expected sentence comes from the SAME seam the service runs under
	// (buildVerifyActionService wires the runner's Verdict to
	// CommandVerdictFor) — the one formatter's own output, not a fragment
	// copied by hand. TestSentenceIsTheOnlyFormatterInTheModule forbids this
	// file from pinning the sentence's text; comparing against the formatter
	// is the assertion the guard exists for: the gate prints it verbatim.
	if want := "policy: " + gateVerdictSentence(t, e, marker("a")); gotA.Reason != want {
		t.Errorf("the refusal's reason %q is not the policy's own default-deny sentence verbatim (%q)", gotA.Reason, want)
	}
	if gateFileExists(marker("a")) {
		t.Fatal("the denied command ran — verify.try bypassed the policy gate")
	}

	// B — an operator allow for the exact command runs it (the token is
	// minted, the spawn happens) and stamps the run with the peer.
	gB := e.addGate(t, "gate-b", "touch "+marker("b"), "required")
	e.seedCommandRule(t, "gate-b-allow", "touch "+marker("b"), policy.Allow)
	resB := e.tryGate(t, gB)
	gotB := resB.Results[0]
	if resB.Run.State != "pass" || gotB.State != "pass" || gotB.ExitCode == nil || *gotB.ExitCode != 0 {
		t.Fatalf("the allowed command did not pass: run %q, gate %+v", resB.Run.State, gotB)
	}
	if !gateFileExists(marker("b")) {
		t.Fatal("the allowed command left no marker — the allow was not honoured")
	}
	if resB.Run.ActorKind != e.peer.Kind || resB.Run.ActorID != e.peer.ID {
		t.Errorf("the manual run is stamped %s/%s, want the peer %s/%s", resB.Run.ActorKind, resB.Run.ActorID, e.peer.Kind, e.peer.ID)
	}

	// C — an explicit deny beats the allow (PL4): the gate is not bypassed
	// in either direction.
	gC := e.addGate(t, "gate-c", "touch "+marker("c"), "required")
	e.seedCommandRule(t, "gate-c-allow", "touch "+marker("c"), policy.Allow)
	e.seedCommandRule(t, "gate-c-deny", "touch "+marker("c"), policy.Deny)
	resC := e.tryGate(t, gC)
	gotC := resC.Results[0]
	if resC.Run.State != "unverified" || gotC.State != "unverified" {
		t.Fatalf("the denied command's run is %q, want unverified — a deny must beat the allow it looks past", resC.Run.State)
	}
	if gateFileExists(marker("c")) {
		t.Fatal("the denied command ran — the explicit deny was not honoured")
	}

	// D — the environment is the gate's: the secret set in THIS process is
	// not in the child (the closed allowlist drops it), and CI is present
	// (GateEnv's constant). A pass proves both halves; a failure retains
	// the tail, so a regression says which half broke.
	scrubCmd := `test "$CI" = 1 -a -z "$(printenv AEON_TEST_GATE_SECRET)"`
	gD := e.addGate(t, "gate-d", scrubCmd, "required")
	e.seedCommandRule(t, "gate-d-allow", scrubCmd, policy.Allow)
	resD := e.tryGate(t, gD)
	if resD.Run.State != "pass" || resD.Results[0].State != "pass" {
		t.Fatalf("the scrubbed-environment gate did not pass: run %q, reason %q, stderr tail %q — a fail means the secret leaked or CI was missing",
			resD.Run.State, resD.Results[0].Reason, resD.Results[0].StderrTail)
	}
}

// TestVerifyTryReportsActiveLoops — the race, named: the root's active
// loops at the moment the try started. A quiet root reports an empty,
// NON-NIL list (the three-state honesty: nothing is running is a fact, not
// an omission); parked loops and loops in other roots are not this root's
// race.
func TestVerifyTryReportsActiveLoops(t *testing.T) {
	e := newGateDaemonEnv(t)
	g := e.addGate(t, "loops-gate", "true", "required")
	e.seedCommandRule(t, "loops-gate-allow", "true", policy.Allow)

	// Quiet: the list is empty and present.
	res := e.tryGate(t, g)
	if res.ActiveLoops == nil || len(res.ActiveLoops) != 0 {
		t.Fatalf("a quiet root reports %#v — a quiet root reports an empty, non-nil list", res.ActiveLoops)
	}

	// One active in this root, one not, one active in a different root:
	// exactly the first is reported.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "loop-live", ComponentRoot: e.root}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "loop-parked", ComponentRoot: e.root, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	cw2, err := e.s.CreateComponent(store.NewComponent{Name: "gate root two", Slug: "gate-root-two", Root: gateGitRoot(t)})
	if err != nil || !cw2.OK() {
		t.Fatalf("create the second root: %v %s", err, cw2.Reason)
	}
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "loop-elsewhere", ComponentRoot: cw2.Component.Root}); err != nil {
		t.Fatal(err)
	}

	res2 := e.tryGate(t, g)
	want := []string{"loop-live"}
	if !reflect.DeepEqual(res2.ActiveLoops, want) {
		t.Fatalf("the active loops report %#v, want exactly %#v — parked loops and loops in other roots are not this root's race", res2.ActiveLoops, want)
	}

	// Two active in this root: both, in sorted order.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "loop-also", ComponentRoot: e.root}); err != nil {
		t.Fatal(err)
	}
	res3 := e.tryGate(t, g)
	want2 := []string{"loop-also", "loop-live"}
	if !reflect.DeepEqual(res3.ActiveLoops, want2) {
		t.Fatalf("two active loops report %#v, want sorted %#v", res3.ActiveLoops, want2)
	}
}

// TestVerifyListShowsSourceAndPolicyPreview — the list-with-details: every
// gate carries its last run and the policy's own preview for its command
// (the one formatter, per command — an allow turns exactly that gate's
// line), and the effective list names the supply: own gates on their own
// node, the supplying component included.
func TestVerifyListShowsSourceAndPolicyPreview(t *testing.T) {
	e := newGateDaemonEnv(t)
	ctx := context.Background()
	e.addGate(t, "alpha", "make alpha", "required")
	e.addGate(t, "beta", "make beta", "advisory")

	res, err := e.v.List(ctx, protocol.VerifyListQuery{ComponentID: e.comp.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Gates) != 2 {
		t.Fatalf("the list carries %d gates, want 2: %+v", len(res.Gates), res.Gates)
	}
	// The preview must be the one formatter's own sentence, taken from the
	// same seam List builds its previews with (gateVerdictSentence) — not a
	// fragment pinned by hand (TestSentenceIsTheOnlyFormatterInTheModule).
	for _, g := range res.Gates {
		if want := gateVerdictSentence(t, e, g.Command); g.PolicyPreview != want {
			t.Errorf("gate %s previews %q — with no policy set, the preview is the default deny's own sentence (%q)", g.Name, g.PolicyPreview, want)
		}
	}

	// An allow for one command turns exactly that gate's preview.
	e.seedCommandRule(t, "alpha-allow", "make alpha", policy.Allow)
	res2, err := e.v.List(ctx, protocol.VerifyListQuery{ComponentID: e.comp.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range res2.Gates {
		switch g.Name {
		case "alpha":
			if !strings.HasPrefix(g.PolicyPreview, "allowed:") {
				t.Errorf("the allowed gate %s previews %q, want the allow's own sentence (allowed: …)", g.Name, g.PolicyPreview)
			}
			if want := gateVerdictSentence(t, e, g.Command); g.PolicyPreview != want {
				t.Errorf("the allowed gate %s previews %q, not the allow's own sentence verbatim (%q)", g.Name, g.PolicyPreview, want)
			}
		case "beta":
			if want := gateVerdictSentence(t, e, g.Command); g.PolicyPreview != want {
				t.Errorf("the untouched gate %s previews %q — an allow for another command must not move it (%q)", g.Name, g.PolicyPreview, want)
			}
		}
	}

	// The effective list names the supply: this root's own gates, its own
	// node as the supplying component.
	res3, err := e.v.List(ctx, protocol.VerifyListQuery{ComponentID: e.comp.ID, Effective: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Gates) != 2 {
		t.Fatalf("the effective list carries %d gates, want 2", len(res3.Gates))
	}
	for _, g := range res3.Gates {
		if g.Source != "own" || g.SourceComponent != e.comp.ID {
			t.Errorf("gate %s is supplied as %q from %q — this root's own gates are source own from its own node", g.Name, g.Source, g.SourceComponent)
		}
	}
}

// TestMergeRulePreviewMatchesTheGate — the preview and the merge gate call
// the SAME evaluation (mergerule.Evaluate over the gathered facts), so a
// preview that disagrees with the gate is a lie. Without a PR the preview
// cannot be evaluated at all — it says so and carries no verdict; with a PR
// but no forge identity it still cannot — and the gate's own answer for the
// same facts is the same state. The one-evaluator pin below is structural:
// a second Evaluate call in either file is the drift this pair exists to
// forbid.
func TestMergeRulePreviewMatchesTheGate(t *testing.T) {
	e := newGateDaemonEnv(t)
	ctx := context.Background()

	// No PR: the gate's question has nothing to ask about.
	p, err := e.m.Preview(ctx, e.comp.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Verdict != nil {
		t.Fatal("a preview without a PR carries a verdict — it fabricated the gate's answer")
	}
	if !strings.Contains(p.CannotSay, "PR") {
		t.Errorf("the no-PR preview's cannot-say %q does not name the missing PR", p.CannotSay)
	}

	// A PR named, no forge identity: still no verdict, and the GATE's own
	// answer for the same facts is the same state.
	p2, err := e.m.Preview(ctx, e.comp.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Verdict != nil {
		t.Fatal("the preview carried a verdict for a PR it cannot read — no forge identity, no live facts")
	}
	if !strings.Contains(p2.CannotSay, "forge") {
		t.Errorf("the no-forge preview's cannot-say %q does not name the missing forge identity", p2.CannotSay)
	}
	loop, err := e.s.CreateLoop(store.NewLoop{Slug: "gate-loop", ComponentRoot: e.root})
	if err != nil {
		t.Fatal(err)
	}
	ans := e.d.mergeGateAnswer(ctx, loop.ID, 7)
	if ans.State != action.CondCannotSay {
		t.Fatalf("the merge gate answered %q for a PR it cannot read — the preview's cannot-say is the gate's", ans.Sentence)
	}
	if !strings.Contains(ans.Sentence, "forge") {
		t.Errorf("the gate's cannot-say %q does not name the missing forge identity", ans.Sentence)
	}

	// One evaluator: the preview and the gate each call mergerule.Evaluate
	// exactly once. A second call in either file is a second truth.
	for _, file := range []string{"mergerule_service.go", "verify_wiring.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(b), "mergerule.Evaluate("); n != 1 {
			t.Errorf("%s calls mergerule.Evaluate %d times — the preview and the gate share one evaluation, not two", file, n)
		}
	}

	// The preview carries the rule it would evaluate, with its source: a
	// set rule is own, the baseline is baseline.
	if _, err := e.m.Preview(ctx, e.comp.ID, 0); err != nil {
		t.Fatal(err)
	}
	_, ref, err := e.m.Set(ctx, e.peer, protocol.MergeRuleSetInput{
		ComponentID:      e.comp.ID,
		Enabled:          true,
		RequireDoD:       mergerule.RequireDoDNone,
		RequiredChecks:   []string{"ci"},
		ChecksPolicy:     mergerule.ChecksReported,
		NoChecks:         mergerule.NoChecksAllow,
		ReviewMode:       mergerule.ReviewModeForge,
		AeonReviewAgents: []string{"code-reviewer"},
		Method:           mergerule.MethodSquash,
	})
	if err != nil || ref != nil {
		t.Fatalf("set a rule for the preview: %v %v", err, ref)
	}
	p3, err := e.m.Preview(ctx, e.comp.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p3.Source != "own" || p3.Rule == nil {
		t.Errorf("the preview after a set is source %q with rule %v — a set rule is source own, with the rule it would evaluate", p3.Source, p3.Rule)
	}
}

// TestRefusalsAreTypedWithAlternatives — a refusal is data, not prose:
// the closed-set kind (the client branches on it), the sentence (it names
// the thing refused), and the alternative (what to do instead). The read
// doors ride a typed protocol error (registered code, kind in Data.Kind,
// remedy in Data.Todo, fatal false); the write doors ride the typed
// refusal result (kind + detail). Every arm below fails when a refusal is
// untyped or carries no way out.
func TestRefusalsAreTypedWithAlternatives(t *testing.T) {
	e := newGateDaemonEnv(t)
	ctx := context.Background()

	// A slug in two roots, for the ambiguous-resolution arms.
	cw2, err := e.s.CreateComponent(store.NewComponent{Name: "gate root two", Slug: "gate-root-two", Root: gateGitRoot(t)})
	if err != nil || !cw2.OK() {
		t.Fatalf("create the second root: %v %s", err, cw2.Reason)
	}
	for _, root := range []string{e.root, cw2.Component.Root} {
		if _, err := e.s.CreateLoop(store.NewLoop{Slug: "twice", ComponentRoot: root}); err != nil {
			t.Fatal(err)
		}
	}

	// The read doors: a typed protocol error.
	readDoors := []struct {
		name string
		kind string
		code int
		want []string // substrings the sentence (Data.What) must carry
		run  func() error
	}{
		{"verify.list on an unknown component", "component-unknown", -31870, []string{"is not a component"}, func() error {
			_, err := e.v.List(ctx, protocol.VerifyListQuery{ComponentID: "nope"})
			return err
		}},
		{"verify.show on an unknown gate", "command-unknown", -31870, []string{"is not a gate"}, func() error {
			_, err := e.v.Show(ctx, "nope")
			return err
		}},
		{"verify.runs on an unknown loop", "loop-unknown", -31870, []string{"is not a loop"}, func() error {
			_, err := e.v.Runs(ctx, protocol.VerifyRunsQuery{LoopSlug: "nowhere"})
			return err
		}},
		{"verify.runs on a slug in two roots", "loop-slug-ambiguous", -31870, []string{"name the component"}, func() error {
			_, err := e.v.Runs(ctx, protocol.VerifyRunsQuery{LoopSlug: "twice"})
			return err
		}},
		{"mergerule.get on an unknown component", "component-unknown", -31871, []string{"is not a component"}, func() error {
			_, err := e.m.Get(ctx, "nope", false)
			return err
		}},
		{"dod.list on an unknown loop", "loop-unknown", -31872, []string{"is not a loop"}, func() error {
			_, err := e.dd.List(ctx, "nowhere")
			return err
		}},
		{"dod.list on a slug in two roots", "loop-slug-ambiguous", -31872, []string{"must name one"}, func() error {
			_, err := e.dd.List(ctx, "twice")
			return err
		}},
	}
	for _, tc := range readDoors {
		err := tc.run()
		var pe *protocol.Error
		if !errors.As(err, &pe) {
			t.Errorf("%s: the read door returned %T, not a typed protocol error — the client cannot branch on a kind", tc.name, err)
			continue
		}
		if pe.Code != tc.code {
			t.Errorf("%s: the error rides code %d, want the registered %d", tc.name, pe.Code, tc.code)
		}
		if pe.Data.Kind != tc.kind {
			t.Errorf("%s: data.kind is %q, want the closed-set kind %q", tc.name, pe.Data.Kind, tc.kind)
		}
		for _, w := range tc.want {
			if !strings.Contains(pe.Data.What, w) {
				t.Errorf("%s: the sentence %q does not carry %q — a refusal that does not name the way out is not a refusal", tc.name, pe.Data.What, w)
			}
		}
		if pe.Data.Todo == "" {
			t.Errorf("%s: data.todo is empty — the remedy is part of the contract", tc.name)
		}
		if pe.Data.Fatal {
			t.Errorf("%s: a refusal is not fatal — the connection survives", tc.name)
		}
	}

	// The write doors: the typed refusal result. A DoD item for the mark
	// arms (the state is checked before the item, but met-without-evidence
	// needs a row to stand on) — and the item belongs to a loop, so the
	// row must exist before the brief can land.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "dod-refusals", ComponentRoot: e.root}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.MaterializeDoD(store.Scope{LoopSlug: "dod-refusals", ComponentRoot: e.root},
		store.DoDBrief{BriefID: "refusal-brief", BriefVersion: 1, Items: []string{"the gate passes"}}); err != nil {
		t.Fatal(err)
	}
	items, err := e.s.ListDoD(store.Scope{LoopSlug: "dod-refusals", ComponentRoot: e.root})
	if err != nil || len(items) != 1 {
		t.Fatalf("materialise the refusal item: %v (n=%d)", err, len(items))
	}
	itemID := items[0].ID
	// A live gate for the taken-name arm: the duplicate add must hit it.
	e.addGate(t, "loops-gate", "true", "required")

	writeDoors := []struct {
		name string
		kind string
		want []string // substrings the detail must carry (the alternative)
		run  func() (kind, detail string)
	}{
		{"verify.add with a name that is not a slug", "name-not-a-slug", []string{"lowercase letters, digits and hyphens"}, func() (string, string) {
			_, ref, err := e.v.Add(ctx, e.peer, protocol.VerifyAddInput{ComponentID: e.comp.ID, Name: "Not A Slug", Command: "true", Requirement: "required", TimeoutS: 30})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"verify.add over a taken name", "name-taken", []string{"archive it first"}, func() (string, string) {
			_, ref, err := e.v.Add(ctx, e.peer, protocol.VerifyAddInput{ComponentID: e.comp.ID, Name: "loops-gate", Command: "true", Requirement: "required", TimeoutS: 30})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"verify.update on an unknown gate", "command-unknown", []string{"is not a gate"}, func() (string, string) {
			_, ref, err := e.v.Update(ctx, e.peer, protocol.VerifyUpdateInput{ID: "nope", Command: strPtr("true")})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"mergerule.set with a method outside the set", "method-invalid", []string{"squash|merge|rebase"}, func() (string, string) {
			_, ref, err := e.m.Set(ctx, e.peer, protocol.MergeRuleSetInput{
				ComponentID: e.comp.ID, Enabled: true,
				RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
				NoChecks: mergerule.NoChecksAllow, ReviewMode: mergerule.ReviewModeForge,
				Method: "fast-forward",
			})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"mergerule.clear on a component with no rule", "merge-rule-unknown", []string{"no merge rule to clear"}, func() (string, string) {
			_, ref, err := e.m.Clear(ctx, e.peer, e.comp.ID)
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"dod.mark on an unknown item", "item-unknown", []string{"is not a DoD item"}, func() (string, string) {
			_, ref, err := e.dd.Mark(ctx, e.peer, protocol.DoDMarkInput{ID: "nope", State: "met", Evidence: &protocol.DoDEvidence{Kind: "manual", Ref: "x"}})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"dod.mark with a state outside the set", "state-invalid", []string{"unchecked|met|unmet|unverifiable"}, func() (string, string) {
			_, ref, err := e.dd.Mark(ctx, e.peer, protocol.DoDMarkInput{ID: itemID, State: "done"})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
		{"dod.mark a met item without its evidence", "met-without-evidence", []string{"evidence"}, func() (string, string) {
			_, ref, err := e.dd.Mark(ctx, e.peer, protocol.DoDMarkInput{ID: itemID, State: "met"})
			if err != nil || ref == nil {
				t.Fatalf("expected a refusal, got %v %+v", err, ref)
			}
			return ref.Kind, ref.Detail
		}},
	}
	for _, tc := range writeDoors {
		kind, detail := tc.run()
		if kind != tc.kind {
			t.Errorf("%s: the refusal's kind is %q, want the closed-set kind %q — an untyped refusal is a prose reason", tc.name, kind, tc.kind)
		}
		for _, w := range tc.want {
			if !strings.Contains(detail, w) {
				t.Errorf("%s: the detail %q does not carry %q — a refusal that does not name the way out is not a refusal", tc.name, detail, w)
			}
		}
	}

	// The try's own refusals, and the typed component-mismatch error.
	archived := e.addGate(t, "archived-gate", "true", "required")
	if _, ref, err := e.v.Archive(ctx, e.peer, archived); err != nil || ref != nil {
		t.Fatalf("archive the try's gate: %v %+v", err, ref)
	}
	if _, ref, err := e.v.Try(ctx, e.peer, e.comp.ID, archived); err != nil || ref == nil ||
		ref.Kind != "command-archived" || !strings.Contains(ref.Detail, "restore it first") {
		t.Errorf("trying an archived gate: kind %q, detail %q (err %v) — want command-archived naming the restore", refStr(ref), refDetail(ref), err)
	}
	if _, _, err := e.v.Try(ctx, e.peer, "another-component", archived); !isBadParams(err) {
		t.Errorf("a try naming the wrong component returned %v — the mismatch is a typed ErrBadParams, not a refusal", err)
	}
}

func isBadParams(err error) bool {
	var bp protocol.ErrBadParams
	return errors.As(err, &bp)
}

func refStr(r *protocol.VerifyRefusal) string {
	if r == nil {
		return "<no refusal>"
	}
	return r.Kind
}

func refDetail(r *protocol.VerifyRefusal) string {
	if r == nil {
		return ""
	}
	return r.Detail
}

func strPtr(s string) *string { return &s }

// TestEveryWriteStampsThePeerActor — the actor is the peer's (P26) and the
// stamp is the audit: every write the gate plane makes carries the peer
// who made it on the row's event (verify · mergerule), on the row itself
// (a DoD mark), and on the run (a try). The materialisation — the daemon's
// own act — carries no actor, because it was not a person's.
func TestEveryWriteStampsThePeerActor(t *testing.T) {
	e := newGateDaemonEnv(t)
	ctx := context.Background()

	// verify: add, update, archive — the event trail carries the peer.
	g := e.addGate(t, "stamp-gate", "true", "required")
	evs, err := e.s.ListVerifyCommandEvents(store.VerifyCommandEventFilter{CommandID: g})
	if err != nil || len(evs) == 0 {
		t.Fatalf("read the gate's events: %v (n=%d)", err, len(evs))
	}
	if ev := evs[len(evs)-1]; ev.Op != "add" || ev.ActorKind != e.peer.Kind || ev.ActorID != e.peer.ID {
		t.Errorf("the add event is op %q by %s/%s — the peer %s/%s made the write", ev.Op, ev.ActorKind, ev.ActorID, e.peer.Kind, e.peer.ID)
	}
	if _, ref, err := e.v.Update(ctx, e.peer, protocol.VerifyUpdateInput{ID: g, Requirement: strPtr("advisory")}); err != nil || ref != nil {
		t.Fatalf("update the gate: %v %+v", err, ref)
	}
	evs, err = e.s.ListVerifyCommandEvents(store.VerifyCommandEventFilter{CommandID: g})
	if err != nil {
		t.Fatal(err)
	}
	if ev := evs[len(evs)-1]; ev.Op != "update" || ev.ActorKind != e.peer.Kind || ev.ActorID != e.peer.ID {
		t.Errorf("the update event is op %q by %s/%s — the peer made the write", ev.Op, ev.ActorKind, ev.ActorID)
	}
	if _, ref, err := e.v.Archive(ctx, e.peer, g); err != nil || ref != nil {
		t.Fatalf("archive the gate: %v %+v", err, ref)
	}
	evs, err = e.s.ListVerifyCommandEvents(store.VerifyCommandEventFilter{CommandID: g})
	if err != nil {
		t.Fatal(err)
	}
	if ev := evs[len(evs)-1]; ev.Op != "archive" || ev.ActorKind != e.peer.Kind || ev.ActorID != e.peer.ID {
		t.Errorf("the archive event is op %q by %s/%s — the peer made the write", ev.Op, ev.ActorKind, ev.ActorID)
	}

	// mergerule: set and clear — the audit trail carries the peer.
	_, ref, err := e.m.Set(ctx, e.peer, protocol.MergeRuleSetInput{
		ComponentID: e.comp.ID, Enabled: true,
		RequireDoD: mergerule.RequireDoDNone, ChecksPolicy: mergerule.ChecksReported,
		NoChecks: mergerule.NoChecksAllow, ReviewMode: mergerule.ReviewModeForge,
		Method: mergerule.MethodSquash,
	})
	if err != nil || ref != nil {
		t.Fatalf("set the rule: %v %+v", err, ref)
	}
	mevs, err := e.s.ListMergeRuleEvents(store.MergeRuleEventFilter{ComponentID: e.comp.ID})
	if err != nil || len(mevs) == 0 {
		t.Fatalf("read the rule's events: %v (n=%d)", err, len(mevs))
	}
	if ev := mevs[len(mevs)-1]; ev.Op != "set" || ev.ActorKind != e.peer.Kind || ev.ActorID != e.peer.ID {
		t.Errorf("the set event is op %q by %s/%s — the peer made the write", ev.Op, ev.ActorKind, ev.ActorID)
	}
	if _, ref, err := e.m.Clear(ctx, e.peer, e.comp.ID); err != nil || ref != nil {
		t.Fatalf("clear the rule: %v %+v", err, ref)
	}
	mevs, err = e.s.ListMergeRuleEvents(store.MergeRuleEventFilter{ComponentID: e.comp.ID})
	if err != nil {
		t.Fatal(err)
	}
	if ev := mevs[len(mevs)-1]; ev.Op != "clear" || ev.ActorKind != e.peer.Kind || ev.ActorID != e.peer.ID {
		t.Errorf("the clear event is op %q by %s/%s — the peer made the write", ev.Op, ev.ActorKind, ev.ActorID)
	}

	// dod: the materialisation is the daemon's act (no actor on the row);
	// the mark is the peer's (the actor stamps the row). The item belongs
	// to a loop — the row must exist before the brief can land.
	if _, err := e.s.CreateLoop(store.NewLoop{Slug: "stamp-dod", ComponentRoot: e.root}); err != nil {
		t.Fatal(err)
	}
	sc := store.Scope{LoopSlug: "stamp-dod", ComponentRoot: e.root}
	if n, err := e.s.MaterializeDoD(sc, store.DoDBrief{BriefID: "stamp-brief", BriefVersion: 1, Items: []string{"the gate plane passes"}}); err != nil || n != 1 {
		t.Fatalf("materialise the DoD: %v (n=%d)", err, n)
	}
	items, err := e.s.ListDoD(sc)
	if err != nil || len(items) != 1 {
		t.Fatalf("read the DoD: %v (n=%d)", err, len(items))
	}
	if items[0].ActorKind != nil || items[0].ActorID != nil {
		t.Errorf("the materialised item carries actor %v/%v — the materialisation is the daemon's act, not a person's", *items[0].ActorKind, items[0].ActorID)
	}
	if _, ref, err := e.dd.Mark(ctx, e.peer, protocol.DoDMarkInput{ID: items[0].ID, State: "met", Evidence: &protocol.DoDEvidence{Kind: "manual", Ref: "a person said so"}}); err != nil || ref != nil {
		t.Fatalf("mark the item: %v %+v", err, ref)
	}
	it, err := e.s.GetDoDItem(items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.ActorKind == nil || *it.ActorKind != e.peer.Kind || it.ActorID == nil || *it.ActorID != e.peer.ID {
		t.Errorf("the marked item is stamped %v/%v — the mark is the peer's word, and the row says otherwise", it.ActorKind, it.ActorID)
	}
}

// TestNoMethodAcceptsAnActorParam — the author is the peer's and never a
// param: a self-chosen author is a forgery the wire must not be able to
// carry. Every write input and read query of the gate plane is checked, by
// name (an actor-shaped field) and by type (a protocol.Actor field). The
// service methods take the actor as a separate argument — the seam — which
// is exactly why the PARAMS must stay clean.
func TestNoMethodAcceptsAnActorParam(t *testing.T) {
	authorNames := regexp.MustCompile(`(?i)^(actor|actor_id|actor_kind|author|user|user_id|identity|peer|origin|uid|caller)$`)
	types := []any{
		// the wire params — the byte surface the socket decodes
		protocol.VerifyAddParams{}, protocol.VerifyUpdateParams{}, protocol.VerifyArchiveParams{},
		protocol.VerifyRestoreParams{}, protocol.VerifyApplyTemplateParams{}, protocol.VerifyTryParams{},
		protocol.MergeRuleSetParams{}, protocol.MergeRuleClearParams{}, protocol.MergeRuleGetParams{},
		protocol.MergeRulePreviewParams{}, protocol.VerifyListParams{}, protocol.VerifyRunsParams{},
		protocol.MergeEventsParams{}, protocol.DoDListParams{}, protocol.DoDMarkParams{},
		// the service inputs — what the seam hands the store
		protocol.VerifyAddInput{}, protocol.VerifyUpdateInput{}, protocol.MergeRuleSetInput{},
		protocol.DoDMarkInput{}, protocol.VerifyListQuery{}, protocol.VerifyRunsQuery{},
	}
	for _, v := range types {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			if authorNames.MatchString(f.Name) {
				t.Errorf("%s carries a field named %s — the author is the peer, never a param", typ.Name(), f.Name)
			}
			if f.Type == reflect.TypeOf(protocol.Actor{}) {
				t.Errorf("%s.%s is a protocol.Actor — a self-chosen author on the wire", typ.Name(), f.Name)
			}
		}
	}
}

// TestMethodsAbsentWhenServiceNil — V13 at the daemon: a storeless daemon
// keeps the three gate seams nil, and the three Register calls leave the
// twenty-two doors ABSENT, not registered-and-refusing. Absent is the
// honest state for a door whose service does not exist.
func TestMethodsAbsentWhenServiceNil(t *testing.T) {
	d, _ := newTestDaemon(t)
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "gate-absence")
	methods := []protocol.MethodName{
		protocol.MethodVerifyList, protocol.MethodVerifyShow, protocol.MethodVerifyRuns,
		protocol.MethodVerifyRunShow, protocol.MethodVerifyVocabulary, protocol.MethodVerifyTemplates,
		protocol.MethodVerifyAdd, protocol.MethodVerifyUpdate, protocol.MethodVerifyArchive,
		protocol.MethodVerifyRestore, protocol.MethodVerifyApplyTemplate, protocol.MethodVerifyTry,
		protocol.MethodMergeRuleGet, protocol.MethodMergeRulePreview, protocol.MethodMergeRuleChanges,
		protocol.MethodMergeRuleVocabulary, protocol.MethodMergeRuleSet, protocol.MethodMergeRuleClear,
		protocol.MethodMergeEvents, protocol.MethodMergeWatches,
		protocol.MethodDoDList, protocol.MethodDoDMark,
	}
	if len(methods) != 22 {
		t.Fatalf("the gate plane has %d doors, not 22 — the list drifted from the registration", len(methods))
	}
	for _, m := range methods {
		_, err := d.Table.Dispatch(ctx, "c1", m, nil)
		var unknown protocol.ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Errorf("%s on a storeless daemon answered %v — a nil seam leaves the door absent (V13), not refusing", m, err)
		}
	}
}

// TestWriteResultsStampTheScrubbedEnvironmentNote — S57 T10 (VG20): the
// scrubbed-environment sentence rides the WRITE RESULT (the daemon
// decides, reading the command with the redactor's own rules), and the
// CLI prints what it is handed. A token-shaped command gets the note;
// a plain one does not; an update that moves a command out of token
// shape clears it. The note is advice on the response, never a refusal:
// the write lands either way.
func TestWriteResultsStampTheScrubbedEnvironmentNote(t *testing.T) {
	e := newGateDaemonEnv(t)
	ctx := context.Background()

	res, ref, err := e.v.Add(ctx, e.peer, protocol.VerifyAddInput{
		ComponentID: e.comp.ID, Name: "gh-peek", Requirement: "required", TimeoutS: 30,
		Command: "gh pr view --log auth_token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
	})
	if err != nil || ref != nil {
		t.Fatalf("add a token-shaped gate: %v %v", err, ref)
	}
	if res.Note != protocol.VerifyEnvNoteSentence {
		t.Errorf("a token-shaped add carried note %q, want the scrubbed-environment sentence verbatim", res.Note)
	}

	plain, ref, err := e.v.Add(ctx, e.peer, protocol.VerifyAddInput{
		ComponentID: e.comp.ID, Name: "build", Requirement: "required", TimeoutS: 30,
		Command: "go build ./...",
	})
	if err != nil || ref != nil {
		t.Fatalf("add a plain gate: %v %v", err, ref)
	}
	if plain.Note != "" {
		t.Errorf("a plain add carried note %q — the note is the token-shape trigger, not a tax on every write", plain.Note)
	}

	// An update that moves the plain command INTO token shape stamps
	// the note on the update result…
	upd, ref, err := e.v.Update(ctx, e.peer, protocol.VerifyUpdateInput{
		ID: plain.Gate.ID, Command: strPtr("curl -H 'Authorization: Bearer sk-proj-ABCDEFGHIJKLMNOPQRST' https://api.example"),
	})
	if err != nil || ref != nil {
		t.Fatalf("update into token shape: %v %v", err, ref)
	}
	if upd.Note != protocol.VerifyEnvNoteSentence {
		t.Errorf("the update into token shape carried note %q, want the sentence", upd.Note)
	}
	// …and out of it clears the note (the absence IS the answer).
	down, ref, err := e.v.Update(ctx, e.peer, protocol.VerifyUpdateInput{
		ID: upd.Gate.ID, Command: strPtr("go vet ./..."),
	})
	if err != nil || ref != nil {
		t.Fatalf("update out of token shape: %v %v", err, ref)
	}
	if down.Note != "" {
		t.Errorf("the update out of token shape carried note %q, want none", down.Note)
	}
}
