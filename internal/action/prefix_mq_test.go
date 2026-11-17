package action

// prefix_mq_test.go — S60 T4: the prefix is ONE rule in ONE place, and a
// pinned base is where a fresh branch is CUT FROM.

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
)

func TestPrefixValidation(t *testing.T) {
	for _, ok := range []string{"", "aeon/", "a/", "x.y.z/", "loop-7/"} {
		if err := CheckBranchPrefix(ok); err != nil {
			t.Errorf("prefix %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"Aeon/", "aeon", "/aeon/", "aeon//", "a b/", "feature/x/", "-x/", "aeon/git"} {
		err := CheckBranchPrefix(bad)
		if err == nil {
			t.Fatalf("prefix %q accepted", bad)
		}
		if !strings.Contains(err.Error(), "optional lowercase identity") {
			t.Errorf("refusal for %q must NAME the rule: %v", bad, err)
		}
	}
	// NewBranchNamesWith enforces the same rule (one spelling, checked at
	// both doors by construction — CheckBranchPrefix IS what it calls).
	if _, err := NewBranchNamesWith("Bad/", "gld", "4a17"); err == nil {
		t.Fatal("constructor accepted a bad prefix")
	}
	// The identity rules still stand for the parts (L3/I3 unchanged).
	if _, err := NewBranchNamesWith("aeon/", "feat/ure", "t"); err == nil {
		t.Fatal("constructor accepted a bad loop identity")
	}
	n, err := NewBranchNamesWith("aeon/", "gld", "4a17")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.TaskBranch(); got != "aeon/task/gld/4a17" {
		t.Errorf("TaskBranch = %q", got)
	}
	if got := n.FeatureBranch(); got != "aeon/feature/gld" {
		t.Errorf("FeatureBranch = %q", got)
	}
	// No prefix: byte-identical to the old doors.
	old, err := NewBranchNames("gld", "4a17")
	if err != nil {
		t.Fatal(err)
	}
	if old.TaskBranch() != "task/gld/4a17" || old.FeatureBranch() != "feature/gld" {
		t.Errorf("empty prefix changed the default names: %q %q", old.TaskBranch(), old.FeatureBranch())
	}
}

func TestIntegrationBranchName(t *testing.T) {
	n, err := NewBranchNamesWith("aeon/", "gld", "4a17")
	if err != nil {
		t.Fatal(err)
	}
	got, err := n.IntegrationBranch("mq-7")
	if err != nil || got != "aeon/integrate/mq-7" {
		t.Fatalf("IntegrationBranch = %q (%v)", got, err)
	}
	if _, err := n.IntegrationBranch("MQ 7"); err == nil {
		t.Fatal("an unsafe item id built a branch name")
	}
}

func TestCreateBranchFromExplicitBase(t *testing.T) {
	ctx := context.Background()
	// Absent everywhere, base known on the REMOTE → cut from origin/<base>.
	f := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			switch {
			case strings.HasSuffix(args[len(args)-1], "/main"):
				return "abc\n", nil // the base exists on the remote
			default:
				return "", &git.GitError{Kind: git.KindExit, Code: 128} // the new branch does not
			}
		}
		return "", nil
	}}
	if _, err := actionsWith(f).CreateBranchFrom(ctx, "/repo", mustNames(t), BranchTask, "main"); err != nil {
		t.Fatal(err)
	}
	var cut []string
	for _, call := range f.log {
		if len(call) >= 2 && call[0] == "branch" {
			cut = call
		}
	}
	if len(cut) != 3 || cut[1] != "task/4a17-version/fix-parse-order" || cut[2] != "origin/main" {
		t.Fatalf("branch cut = %v, want [branch <task-branch> origin/main]", cut)
	}
	// Base known only LOCALLY → cut from the local ref, no origin/ lie.
	f2 := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case args[0] == "rev-parse" && strings.HasPrefix(strings.Join(args, " "), "rev-parse --verify origin/"):
			return "", &git.GitError{Kind: git.KindExit, Code: 128} // nothing remote at all
		}
		return "", nil
	}}
	if _, err := actionsWith(f2).CreateBranchFrom(ctx, "/repo", mustNames(t), BranchTask, "main"); err != nil {
		t.Fatal(err)
	}
	for _, call := range f2.log {
		if len(call) >= 2 && call[0] == "branch" && (len(call) == 3 && call[2] == "main" || len(call) == 2) {
			if len(call) != 3 || call[2] != "main" {
				t.Fatalf("local-only base must cut from the local ref, got %v", call)
			}
			return
		}
	}
	t.Fatal("no branch command was issued")
	// Empty base keeps today's cut: `branch <name>` — two words, no ref.
	f3 := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case args[0] == "rev-parse":
			return "", &git.GitError{Kind: git.KindExit, Code: 128}
		}
		return "", nil
	}}
	if _, err := actionsWith(f3).CreateBranchFrom(ctx, "/repo", mustNames(t), BranchTask, ""); err != nil {
		t.Fatal(err)
	}
	for _, call := range f3.log {
		if len(call) == 2 && call[0] == "branch" {
			return // today's shape
		}
	}
	t.Fatal("the no-base cut drifted from `branch <name>`")
}

func TestPrefixAppliedEverywhere(t *testing.T) {
	// The split-brain guard: when a loop carries a prefix, the git side and
	// the forge side see the SAME prefixed names — one Args, one door.
	ctx := context.Background()
	f := &logGit{resolver: func(args []string) (string, error) {
		switch {
		case args[0] == "show-ref":
			return "", &git.GitError{Kind: git.KindExit, Code: 1}
		case args[0] == "rev-parse":
			return "", &git.GitError{Kind: git.KindExit, Code: 128}
		}
		return "", nil
	}}
	ff := forge.NewFake()
	svc := &Services{Git: actionsWith(f), Forge: &ForgeActions{Forge: ff, Git: pfxProbe{}}}
	a := Args{Dir: "/repo", Loop: "gld", Task: "4a17", Prefix: "aeon/",
		Base: "aeon/feature/gld", BaseBranch: "main", Which: BranchTask, Message: "m",
		TaskTitle: "prefixed names everywhere", PRSummary: "s"}
	if _, err := Dispatch(ctx, svc, GitCreateBranch, a); err != nil {
		t.Fatal(err)
	}
	var created string
	for _, call := range f.log {
		if len(call) >= 2 && call[0] == "branch" {
			created = call[1]
		}
	}
	if created != "aeon/task/gld/4a17" {
		t.Fatalf("git created %q, want the prefixed task branch", created)
	}
	if _, err := Dispatch(ctx, svc, ForgeOpenPR, a); err != nil {
		t.Fatal(err)
	}
	var sawHead, sawBase bool
	for _, call := range ff.Calls() {
		if call.Method != "OpenPR" {
			continue
		}
		for _, arg := range call.Args {
			if req, ok := arg.(forge.PRRequest); ok {
				sawHead = sawHead || req.Head == "aeon/task/gld/4a17"
				sawBase = sawBase || req.Base == "aeon/feature/gld"
			}
		}
	}
	if !sawHead || !sawBase {
		t.Fatalf("the forge call did not carry the prefixed head/base: %+v", ff.Calls())
	}
}

// pfxProbe stands for a head commitAndPush already pushed (check-only).
type pfxProbe struct{}

func (pfxProbe) BranchExistsRemote(context.Context, string, string) (bool, error) { return true, nil }
