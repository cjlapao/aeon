package action

// T2's tests, written first. This task closes the gap that was invisible
// until it was written down: S11's evaluator guards `bash`; code actions
// did not go through it, so expressing an action as a code action rather
// than a command would have laundered it past everything S11 built. The
// asymmetry is the bug — and the asymmetry is why the two governance
// routes are asserted in ONE test below, not two.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/policy"
)

func ghStewardMergeAskRow(t *testing.T) policy.Policy {
	t.Helper()
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		t.Fatalf("the embedded corpus must parse (it is CI-pinned): %s", rej.Reason)
	}
	rules, ok := corpus.RulesFor("github-steward")
	if !ok {
		t.Fatal("the committed corpus has no github-steward — the route this test proves needs it")
	}
	target, rej := policy.ParseTarget(policy.AgentKind, "github-steward")
	if rej != nil {
		t.Fatalf("target: %s", rej.Reason)
	}
	for i, r := range rules {
		if strings.HasPrefix(r.Pattern, "gh pr merge") && r.Verdict == policy.Ask {
			spec, err := json.Marshal(policy.CommandAccessSpec{Command: r.Pattern, State: string(policy.Ask)})
			if err != nil {
				t.Fatalf("spec: %v", err)
			}
			return policy.Policy{ID: corpusRowID(i), Type: policy.CommandAccess, Target: target, Action: policy.Ask, Spec: spec, Source: policy.Operator}
		}
	}
	t.Fatal("github-steward does not mark gh pr merge* as ask — the corpus is the evidence this route asks")
	return policy.Policy{}
}

func corpusRowID(i int) string { return "corpus-merge-ask-" + string(rune('a'+i)) }

// TestBothGovernanceRoutesReachAHuman is the only test that would have
// caught the gap (D9, C7). ONE test, not two: two separate tests each pass
// while the asymmetry between them survives, and the asymmetry is the bug.
func TestBothGovernanceRoutesReachAHuman(t *testing.T) {
	row := ghStewardMergeAskRow(t)
	v := policy.Evaluate([]policy.Policy{row}, policy.Request{
		Type:    policy.CommandAccess,
		Agent:   "github-steward",
		Command: "gh pr merge 15",
	}, time.Unix(1730000000, 0))
	if !v.Asked {
		t.Fatalf("route 1 (bash): `gh pr merge 15` under github-steward must ASK a human, got %+v", v)
	}

	r, err := Check(ForgeMerge)
	if err != nil {
		t.Fatalf("route 2: %v", err)
	}
	if r.Verdict != VerdictAsk {
		t.Fatalf("route 2 (code action): forge.merge must ASK a human too, got %q — a blueprint naming forge.merge would be laundering the same effect past the guard", r.Verdict)
	}
	if r.Class != EffectIrreversible {
		t.Fatalf("forge.merge is the irreversible class, got %q", r.Class)
	}
	// Side by side means the reader can compare them: both routes hand a
	// sentence to the human, and the policy route's sentence names the
	// rule it decided with (G11) — an empty or ruleless sentence is not a
	// human-routed decision.
	if v.Sentence() == "" || v.By == nil {
		t.Fatalf("route 1 must hand the human a deciding-rule sentence, got %q (%+v)", v.Sentence(), v)
	}
}

func TestEveryActionDeclaresAClass(t *testing.T) {
	// S57 T6 added the tenth: verify.run (local — the gates run in the
	// tree, so the class is the one a verification needs: run, attended
	// or not). S60 T5 added the queue's three (MQ8, dated 2026-11-04):
	// integrate and concludeIntegration are LOCAL (a worktree is visible
	// to nobody outside the tree) and publishIntegration is the queue's
	// only OUTWARD name — a plain push of a ref only it can name.
	// S60 T6 (dated 2026-11-04) added the landing half's three (MQ8/MQ12):
	// queue.enqueue is LOCAL (a row in our own database), openLandingPR is
	// the queue's second OUTWARD name, and forge.land is the second
	// IRREVERSIBLE name — the merge the queue lands, guarded like its
	// sibling and classed exactly as strictly.
	// S61 T4 (dated 2026-11-05): the two bootstrap names join the census —
	// repo.scaffold LOCAL and forge.createRepo OUTWARD, both classified
	// before either is dispatchable, both behind the person door.
	want := []string{GitCreateBranch, GitCommitAndPush, GitMergeBranch, GitIntegrate, GitConcludeIntegration, GitPublishIntegration, QueueEnqueue, ForgeOpenIssue, ForgeOpenPR, ForgeOpenLandingPR, ForgePostReview, ForgeMerge, ForgeLand, ForgeCloseIssue, Audit, VerifyRun, RepoScaffold, ForgeCreateRepo}
	got := Classes()
	if len(got) != len(want) {
		t.Fatalf("nine actions plus audit, got %d entries: %v", len(got), got)
	}
	for _, name := range want {
		c, ok := got[name]
		if !ok {
			t.Fatalf("action %q declares no class", name)
		}
		if !Valid(c) {
			t.Fatalf("action %q carries a zero or unknown class %q", name, c)
		}
	}
	if Valid(EffectClass("")) {
		t.Fatal("the zero value must mean nothing")
	}
}

func TestOutwardAndIrreversibleAsk(t *testing.T) {
	// S60 T5: git.publishIntegration joins the outward row (MQ8 — it pushes
	// a ref to a remote; attendedness decides), and the queue's two worktree
	// halves join the local row (nothing leaves the tree).
	for _, name := range []string{GitCommitAndPush, GitPublishIntegration, ForgeOpenIssue, ForgeOpenPR, ForgePostReview, ForgeCloseIssue} {
		r, err := Check(name)
		if err != nil || r.Verdict != VerdictAsk || r.Class != EffectOutward {
			t.Fatalf("%s: outward must ask, got %+v (%v)", name, r, err)
		}
	}
	r, err := Check(ForgeMerge)
	if err != nil || r.Verdict != VerdictAsk || r.Class != EffectIrreversible {
		t.Fatalf("forge.merge: irreversible must ask, got %+v (%v)", r, err)
	}
	for _, name := range []string{GitCreateBranch, GitMergeBranch, GitIntegrate, GitConcludeIntegration, Audit} {
		r, err := Check(name)
		if err != nil || r.Verdict != VerdictRun {
			t.Fatalf("%s: local does not ask, got %+v (%v)", name, r, err)
		}
	}
}

// TestUnattendedDeniesOutwardActions — G7/G14 follow from the pinned
// attendedness rather than being a new rule, and the sentence NAMES the
// class so the operator learns which door said no and why.
func TestUnattendedDeniesOutwardActions(t *testing.T) {
	r, err := Resolve(ForgeMerge, false, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Verdict != VerdictDeny {
		t.Fatalf("unattended forge.merge must deny, got %q", r.Verdict)
	}
	if !strings.Contains(r.Sentence, "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)") {
		t.Fatalf("the deny sentence must name the class exactly as the demo shows it, got %q", r.Sentence)
	}
	r, err = Resolve(ForgeOpenPR, false, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Verdict != VerdictDeny || !strings.Contains(r.Sentence, "effect class 'outward' resolves to deny (loop is unattended)") {
		t.Fatalf("unattended forge.openPR must deny naming 'outward', got %+v", r)
	}
	// local runs either way — the classes govern what is visible to others.
	r, err = Resolve(GitCreateBranch, false, false)
	if err != nil || r.Verdict != VerdictRun {
		t.Fatalf("local runs unattended, got %+v (%v)", r, err)
	}
	// Attended asks again (the deny is the unattended resolution, not the class).
	r, err = Resolve(ForgeMerge, true, false)
	if err != nil || r.Verdict != VerdictAsk {
		t.Fatalf("attended forge.merge asks, got %+v (%v)", r, err)
	}
}

// TestAskCarriesWhatIsAffected — O1, applied harder than S11's G18:
// "approve forge.merge?" tells a human NOTHING about what is being merged
// or where. The builder refuses to produce such an ask.
func TestAskCarriesWhatIsAffected(t *testing.T) {
	a, err := BuildAsk(ForgeMerge, Target{
		Repository: "cjlapao/throwaway",
		PRNumber:   15,
		BaseRef:    "feature/4a17-version",
	})
	if err != nil {
		t.Fatalf("BuildAsk: %v", err)
	}
	for _, want := range []string{"forge.merge", "#15", "feature/4a17-version", "cjlapao/throwaway", "irreversible", "cannot be undone by another action"} {
		if !strings.Contains(a.Body, want) {
			t.Fatalf("the merge ask must carry %q, body is:\n%s", want, a.Body)
		}
	}
	if a.Kind != "permission" {
		t.Fatalf("Q5: the ask reuses S11's permission kind (same affordance, same answer verbs); got %q", a.Kind)
	}
	if strings.Join(a.Options, ",") != "approve,deny" {
		t.Fatalf("answer verbs must match S11's (U14 renders it the same way), got %v", a.Options)
	}
	if a.Payload["pr"] != "15" || a.Payload["base"] != "feature/4a17-version" || a.Payload["repository"] != "cjlapao/throwaway" {
		t.Fatalf("structured payload must carry pr/base/repository for the UI to render from, got %v", a.Payload)
	}

	// The negative half IS the test: an ask naming only the action is refused.
	if _, err := BuildAsk(ForgeMerge, Target{}); err == nil {
		t.Fatal(`"approve forge.merge?" alone must fail to build`)
	}
	if _, err := BuildAsk(ForgeOpenPR, Target{Repository: "r"}); err == nil {
		t.Fatal("an openPR ask without head/base/title must fail to build — approving a blind PR is what the split prevents")
	}
}

// TestClassesAreNotConfigurable (N7, I4) — a config-defined class is a way
// to downgrade irreversible to local. Grep the package: no environment,
// no config path, no setter reaches a class.
func TestClassesAreNotConfigurable(t *testing.T) {
	// Built by concatenation so this file never contains its own indictments.
	banned := []string{
		"os." + "Getenv",
		"os." + "LookupEnv",
		"Set" + "Class",
		"Override" + "Class",
		"Register" + "Action",
		"yaml",
	}
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s contains %q — a class is code (N7)", path, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

func TestClassSetIsClosed(t *testing.T) {
	if _, err := Check("forge.launchRockets"); err == nil {
		t.Fatal("unknown action must fail — the registry is closed (L8), which is why classes suffice")
	}
	if _, err := Check(""); err == nil {
		t.Fatal("the empty name must fail")
	}
	if Valid(EffectClass("local-ish")) || Valid(EffectClass("LOCAL")) {
		t.Fatal("the class set is closed: {local outward irreversible}, exactly")
	}
	if !Valid(EffectLocal) || !Valid(EffectOutward) || !Valid(EffectIrreversible) {
		t.Fatal("the three specced classes must be exactly valid")
	}
}

func TestActionCheckPrintsClassAndVerdict(t *testing.T) {
	out, err := CheckAnswer(ForgeMerge)
	if err != nil {
		t.Fatalf("CheckAnswer: %v", err)
	}
	for _, want := range []string{"forge.merge", "irreversible", "ask"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the CLI line must carry %q, got %q", want, out)
		}
	}
	// The comparison line the demo uses side by side needs the command
	// route printed the same way — the §3 demo pastes both lines.
	if _, err := Check(GitCreateBranch); err == nil {
		out2, _ := CheckAnswer(GitCreateBranch)
		if !strings.Contains(out2, "local") || !strings.Contains(out2, "run") {
			t.Fatalf("local line reads like a verdict: %q", out2)
		}
	}
	_, err = CheckAnswer("nope")
	if err == nil {
		t.Fatal("unknown action must be refused by the CLI too")
	}
}

// context import guard for future resolver tests; the resolver itself lives
// in protocol (answer.go) and its test rides along there.
var _ = context.Background
