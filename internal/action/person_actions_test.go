package action

// S61 T4 (GI8–GI12): the two person-door actions. The door is the first
// fact (a blueprint cannot name them, Dispatch refuses them, only a
// person Caller reaches them); then the instruments (scaffold guards,
// writes and commits, cleans up after itself; createRepo asks three keys
// before any request, GETs before it POSTs, and never retargets or
// forces).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/scaffold"
)

func scaffoldSVC(t *testing.T, target string) *Services {
	t.Helper()
	return &Services{
		Git: &GitActions{Git: gitReal()},
		Scaffold: &ScaffoldActions{
			Templates: func(_ context.Context, slug string) (scaffold.Template, int, bool, error) {
				if slug != "tiny" {
					return scaffold.Template{}, 0, false, nil
				}
				return scaffold.Template{Slug: "tiny", Files: []scaffold.TemplateFile{
					{Path: "README.md", Mode: 0o644, Content: "# {{.Name}}\nbranch {{.DefaultBranch}}\n"},
				}}, 1, true, nil
			},
			TargetFacts: func(context.Context) (scaffold.TargetFacts, error) {
				return scaffold.TargetFacts{Home: os.Getenv("HOME"), StateDir: t.TempDir()}, nil
			},
		},
	}
}

// --- GI8: the door -----------------------------------------------------------

func TestPersonDoorActionsAreInvisibleToBlueprints(t *testing.T) {
	names := map[string]bool{}
	for _, n := range Names() {
		names[n] = true
	}
	if names[RepoScaffold] || names[ForgeCreateRepo] {
		t.Fatal("a person door is in Names() — the blueprint validator would learn the words")
	}
	pn := PersonNames()
	if len(pn) != 2 || pn[0] != "forge.createRepo" || pn[1] != "repo.scaffold" {
		t.Fatalf("PersonNames: %v", pn)
	}
	if _, ok := Lookup(RepoScaffold); !ok {
		t.Fatal("the registry is the only definition — listed there, invisible to the validator")
	}
	for _, smuggle := range []string{RepoScaffold, ForgeCreateRepo} {
		raw := []byte("id: smuggle\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: x\n    step: {kind: code-action, action: " + smuggle + "}\n")
		report := blueprint.Analyze(raw, blueprint.ValidationOptions{Mode: "standard", Actions: Names()})
		found := false
		for _, d := range report.Diagnostics {
			if d.State == blueprint.Invalid && d.Kind == "unknown-action" {
				found = true
			}
		}
		if !found {
			t.Fatalf("a blueprint could name %s — GI8 exists exactly for this", smuggle)
		}
	}
}

func TestPersonDoorResolveDeniesBeforeTheFlag(t *testing.T) {
	for _, name := range PersonNames() {
		for _, attended := range []bool{true, false} {
			for _, outward := range []bool{true, false} {
				r, err := Resolve(name, attended, outward)
				if err != nil {
					t.Fatal(err)
				}
				if r.Verdict != VerdictDeny || !strings.Contains(r.Sentence, "person-door") {
					t.Fatalf("%s attended=%v outward=%v: %v %s — the flag lifts outward for the loop set; a person door is not in that set at all", name, attended, outward, r.Verdict, r.Sentence)
				}
			}
		}
	}
	// The class is still the fact it always was — the door gates the
	// route, it does not rename the effect.
	r, _ := Resolve(ForgeCreateRepo, false, false)
	if r.Class != EffectOutward {
		t.Fatalf("class: %s", r.Class)
	}
}

func TestDispatchAndCallersRefuseTheDoors(t *testing.T) {
	full := Args{Dir: "/abs/fresh", Template: "tiny@1", IntakeID: "gi-1"}
	svc := scaffoldSVC(t, "/abs/fresh")
	// Dispatch — the loop's door — refuses even with every instrument wired
	// and every required argument present: the door, not the wiring, is
	// what refuses.
	if _, err := Dispatch(context.Background(), svc, RepoScaffold, full); err == nil || !strings.Contains(err.Error(), "person-door") {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := Dispatch(context.Background(), svc, ForgeCreateRepo, full); err == nil || !strings.Contains(err.Error(), "person-door") {
		t.Fatalf("Dispatch: %v", err)
	}
	for _, c := range []Caller{{}, {Kind: "loop", ID: "4a17"}, {Kind: "agent", ID: "coder"}, {Kind: "system", ID: "seed"}} {
		_, err := DispatchFrom(context.Background(), svc, c, RepoScaffold, full)
		if err == nil || !strings.Contains(err.Error(), "person-door") {
			t.Fatalf("caller %+v reached the scaffold — GI8 says a loop, an agent or a blueprint is refused", c)
		}
	}
	// A person caller without the required arguments meets the SAME
	// required-argument sentence the loop door speaks — one schema, two
	// doors.
	_, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson, ID: "user:1000"}, RepoScaffold, Args{Dir: "/x"})
	if err == nil || !strings.Contains(err.Error(), "required argument") {
		t.Fatalf("missing args: %v", err)
	}
}

// --- GI9/GI12: the scaffold --------------------------------------------------

func TestScaffoldHappyPathLaysLandsAndCommits(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "fresh")
	svc := scaffoldSVC(t, target)
	ev, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson, ID: "user:1000"}, RepoScaffold, Args{
		Dir: target, Template: "tiny@1", IntakeID: "gi-7",
		Vars: `{"name":"Demo","slug":"demo","description":"a demo","stack":["go"],"defaultBranch":"trunk"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev["branch"] != "trunk" || ev["files"] != "1" || len(ev["commit_sha"]) != 40 {
		t.Fatalf("evidence: %v", ev)
	}
	body, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil || !strings.Contains(string(body), "# Demo") || !strings.Contains(string(body), "branch trunk") {
		t.Fatalf("rendered file: %q %v", body, err)
	}
	// The commit is real, on the branch, with the intake named in its
	// message (GI9's message is the audit trail from the repository side).
	out, err := exec.Command("git", "-C", target, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "Initial commit (Aeon intake gi-7)" {
		t.Fatalf("commit message: %q %v", out, err)
	}
	out, err = exec.Command("git", "-C", target, "symbolic-ref", "--short", "HEAD").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "trunk" {
		t.Fatalf("branch: %q %v", out, err)
	}
}

// TestScaffoldMissingIdentityRefusesAndCreatesNothing — GI12 in one
// assertion: the identity is resolved BEFORE the first write, so the
// refusal sentence and the empty disk are the same fact. The unresolvable
// answer is SCRIPTED (a host's fallback ident is the host's business).
func TestScaffoldMissingIdentityRefusesAndCreatesNothing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	svc := scaffoldSVC(t, target)
	svc.Git = &GitActions{Git: &git.Client{Exec: func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "var" {
			return "", &git.GitError{Kind: git.KindExit, Detail: "unable to auto-detect email address", Code: 128}
		}
		t.Errorf("git ran %v after the identity refusal — GI12 means NOTHING is created", args)
		return "", nil
	}}}
	_, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson, ID: "user:1000"}, RepoScaffold,
		Args{Dir: target, Template: "tiny@1", IntakeID: "gi-7", Vars: `{"name":"Demo"}`})
	if err == nil || !strings.Contains(err.Error(), "no git identity") || !strings.Contains(err.Error(), "Nothing was created") {
		t.Fatalf("refusal: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the scaffold wrote before asking who would author the commit")
	}
}

func TestScaffoldPinsAreEnforced(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	cases := []struct{ template, want string }{
		{"tiny", "names no pinned version"},
		{"tiny@0", "not a positive number"},
		{"other@1", "does not exist"},
		{"tiny@2", "is at version 1 and the plan pinned 2"},
	}
	for _, tc := range cases {
		svc := scaffoldSVC(t, target)
		_, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson, ID: "u"}, RepoScaffold,
			Args{Dir: target, Template: tc.template, IntakeID: "gi", Vars: `{"name":"D"}`})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("template %q: %v — want %q", tc.template, err, tc.want)
		}
	}
}

func TestScaffoldVarsAreTheClosedSet(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	_, err := DispatchFrom(context.Background(), scaffoldSVC(t, target), Caller{Kind: CallerPerson}, RepoScaffold,
		Args{Dir: target, Template: "tiny@1", IntakeID: "gi", Vars: `{"name":"D","charter":"everything the plan believes"}`})
	if err == nil || !strings.Contains(err.Error(), "closed set") || !strings.Contains(err.Error(), "charter") {
		t.Fatalf("unknown var: %v — the offending key must be named", err)
	}
}

func TestScaffoldRefusesGroundItDoesNotOwn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "somebody-elses.txt", "x") // non-empty → the target below is inside a non-empty world? No — target is a sibling.
	target := filepath.Join(root, "full")
	writeFile(t, target, "keep.txt", "mine")
	_, err := DispatchFrom(context.Background(), scaffoldSVC(t, target), Caller{Kind: CallerPerson}, RepoScaffold,
		Args{Dir: target, Template: "tiny@1", IntakeID: "gi", Vars: `{"name":"D"}`})
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("non-empty target: %v", err)
	}
	// Inside a registered component root.
	svc := scaffoldSVC(t, target)
	svc.Scaffold.TargetFacts = func(context.Context) (scaffold.TargetFacts, error) {
		return scaffold.TargetFacts{ComponentRoots: []string{root}}, nil
	}
	_, err = DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson}, RepoScaffold,
		Args{Dir: filepath.Join(root, "fresh"), Template: "tiny@1", IntakeID: "gi", Vars: `{"name":"D"}`})
	if err == nil || !strings.Contains(err.Error(), "component root") {
		t.Fatalf("inside a root: %v", err)
	}
}

// TestScaffoldTakesBackEverythingItCreated — the commit fails after the
// files are laid and after `git init` ran. The ledger takes back its own
// paths; the .git this run made is taken back too (it is this run's
// creation); the target directory itself is gone. And a stranger's file
// in there would have stopped the broom — asserted next.
func TestScaffoldTakesBackEverythingItCreated(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	svc := scaffoldSVC(t, target)
	svc.Git = &GitActions{Git: &git.Client{Exec: func(_ context.Context, _ string, args ...string) (string, error) {
		switch args[0] {
		case "var":
			return "Aeon Test <aeon@aeon.test> 1730000000 +0000", nil
		case "commit":
			return "", &git.GitError{Kind: git.KindExit, Detail: "fatal: unable to write object", Code: 128}
		}
		return "", nil
	}}}
	_, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson}, RepoScaffold,
		Args{Dir: target, Template: "tiny@1", IntakeID: "gi", Vars: `{"name":"D"}`})
	if err == nil || !strings.Contains(err.Error(), "taken back") {
		t.Fatalf("failed commit: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the half-run left %s behind — files, .git and the directory it created are all this run's to remove", target)
	}
}

func TestScaffoldCleanupNeverForcesAStrangersContent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "fresh")
	svc := scaffoldSVC(t, target)
	svc.Git = &GitActions{Git: &git.Client{Exec: func(_ context.Context, dir string, args ...string) (string, error) {
		switch args[0] {
		case "var":
			return "Aeon Test <aeon@aeon.test> 1730000000 +0000", nil
		case "init":
			// A stranger moves in while the scaffold is working.
			writeFile(t, dir, "stranger.txt", "not yours")
			return "", nil
		case "commit":
			return "", &git.GitError{Kind: git.KindExit, Detail: "fatal: boom", Code: 128}
		}
		return "", nil
	}}}
	_, err := DispatchFrom(context.Background(), svc, Caller{Kind: CallerPerson}, RepoScaffold,
		Args{Dir: target, Template: "tiny@1", IntakeID: "gi", Vars: `{"name":"D"}`})
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("stranger present: %v — the cleanup must REPORT, never force", err)
	}
	if _, err := os.Stat(filepath.Join(target, "stranger.txt")); err != nil {
		t.Fatal("the broom took a stranger's file")
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); !os.IsNotExist(err) {
		t.Fatal("this run's own file survived the cleanup it was ledgered for")
	}
}

// --- GI10: the repository door ----------------------------------------------

func repoSVC(setting bool, settingErr error, grantErr error, grantWired bool) (*Services, *forge.Fake, *logGit) {
	f := forge.NewFake()
	lg := &logGit{}
	s := &Services{
		Git:   actionsWith(lg),
		Forge: &ForgeActions{Forge: f},
		CreateRepo: &CreateRepoActions{
			SettingEnabled: func(context.Context) (bool, error) { return setting, settingErr },
		},
	}
	if grantWired {
		s.CreateRepo.Grant = func(context.Context) error { return grantErr }
	}
	return s, f, lg
}

func repoArgs(publish string) Args {
	return Args{Dir: "/repo", Owner: "acme", RepoName: "widget", OwnerKind: "user", Publish: publish, Description: "a widget"}
}

// TestCreateRepoAsksThreeKeysBeforeAnyRequest — each key refused means
// ZERO forge calls and ZERO git calls: a refusal that still POSTed is a
// refusal that acted.
func TestCreateRepoAsksThreeKeysBeforeAnyRequest(t *testing.T) {
	// seam unwired — the S44 default is the answer
	s, f, lg := repoSVC(false, nil, nil, false)
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private")); err == nil || !strings.Contains(err.Error(), "intake.allowRemoteCreate") {
		t.Fatalf("unwired setting: %v", err)
	}
	// setting off
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private")); err == nil || !strings.Contains(err.Error(), "is off") {
		t.Fatalf("setting off: %v", err)
	}
	// grant unwired — S56's fixed sentence, verbatim
	s, f, lg = repoSVC(true, nil, nil, false)
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private")); err == nil || err.Error() != "remote repository creation needs an autonomy grant (S56)" {
		t.Fatalf("grant unwired: %v — the sentence is contracted verbatim", err)
	}
	// setting ON, grant wired and REFUSED — the grant's own sentence travels
	s, f, lg = repoSVC(true, nil, fmt.Errorf("autonomy: forge.createRepo is not granted for this root"), true)
	_, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "not granted for this root") {
		t.Fatalf("grant refusal: %v — verbatim", err)
	}
	// key 3: public without the confirmation — and a publish value that is
	// neither of the two words.
	s, f, lg = repoSVC(true, nil, nil, true)
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("public")); err == nil || !strings.Contains(err.Error(), "confirm_public") {
		t.Fatalf("public unconfirmed: %v", err)
	}
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("sideways")); err == nil || !strings.Contains(err.Error(), "sideways") {
		t.Fatalf("bad publish: %v — the value is named", err)
	}
	if len(f.Calls()) != 0 || lg.len() != 0 {
		t.Fatalf("refused runs made %d forge and %d git calls", len(f.Calls()), lg.len())
	}
}

// TestCreateRepoPublishesPrivatelyAndSendsOnlyTheThreeFields — the happy
// creation: private by the person's own answer, and the RECORDED REQUEST
// carries name, visibility and the short description. The plan's charter
// prose has no field to arrive through — a name that would smuggle a path
// is refused before the wire.
func TestCreateRepoPublishesPrivatelyAndSendsOnlyTheThreeFields(t *testing.T) {
	s, f, lg := repoSVC(true, nil, nil, true)
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	a := repoArgs("private")
	a.Vars = "" // the scaffold's variables are NOT this action's business
	ev, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, a)
	if err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 || calls[0].Method != "GetRepo" || calls[1].Method != "CreateRepo" {
		t.Fatalf("calls: %v — GET before POST, exactly once", calls)
	}
	req := calls[1].Args[0].(forge.RepoRequest)
	if !req.Private || req.Description != "a widget" || req.OwnerKind != "user" {
		t.Fatalf("request: %+v", req)
	}
	encoded, _ := json.Marshal(req)
	for _, leak := range []string{"charter", "plan", "everything the loop believes"} {
		if strings.Contains(string(encoded), leak) {
			t.Fatalf("request JSON carries %q — GI18", leak)
		}
	}
	if ev["repo_url"] != "example.invalid/acme/widget" || ev["default_branch"] != "main" || ev["pushed_sha"] != pushSHA {
		t.Fatalf("evidence: %v", ev)
	}
	// origin was ADDED (never retargeted) and the push carried the branch.
	cmds := logCommands(lg)
	if !containsWord(cmds, "remote", "add") || !containsWord(cmds, "push", "-u", "origin", "main") {
		t.Fatalf("git log: %v", cmds)
	}
}

func TestCreateRepoPublicNeedsItsConfirmationAndMeansPublic(t *testing.T) {
	s, f, lg := repoSVC(true, nil, nil, true)
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	a := repoArgs("public")
	a.ConfirmPublic = true
	if _, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, a); err != nil {
		t.Fatal(err)
	}
	req := f.Calls()[1].Args[0].(forge.RepoRequest)
	if req.Private {
		t.Fatal("the person said PUBLIC, confirm_public, and the request still says private")
	}
}

// TestCreateRepoAdoptRules — an EMPTY existing repository is adopted only
// when told; a POPULATED one is a conflict and a stop; and the re-run of
// a success answers the same evidence without touching anything (the
// behavior-6 no-op is the strategy's whole point).
func TestCreateRepoAdoptRules(t *testing.T) {
	// empty, not adopted → refused, naming the field.
	s, f, lg := repoSVC(true, nil, nil, true)
	f.SeedRepo("acme", "widget", forge.Repo{Empty: true})
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	_, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "adopt_existing") {
		t.Fatalf("empty unadopted: %v", err)
	}
	for _, c := range f.Calls() {
		if c.Method == "CreateRepo" {
			t.Fatal("POSTed after refusing")
		}
	}

	// empty, adopted → proceeds without a POST; the remote points at the
	// ADOPTED clone URL.
	s, f, lg = repoSVC(true, nil, nil, true)
	f.SeedRepo("acme", "widget", forge.Repo{Empty: true, DefaultBranch: "main"})
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	a := repoArgs("private")
	a.AdoptExisting = true
	ev, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, a)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		if c.Method == "CreateRepo" {
			t.Fatal("an adopted repository was POSTed over")
		}
	}
	joined := strings.Join(flatten(logCommands(lg)), " ")
	if !strings.Contains(joined, "origin example.invalid/acme/widget.git") {
		t.Fatalf("add remote: %s", joined)
	}
	if ev["repo_url"] != "example.invalid/acme/widget" {
		t.Fatalf("evidence: %v", ev)
	}

	// populated → conflict naming what it is.
	s, f, lg = repoSVC(true, nil, nil, true)
	f.SeedRepo("acme", "widget", forge.Repo{Empty: false, DefaultBranch: "main"})
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	_, err = DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("populated: %v", err)
	}

	// the re-run: populated, origin already points at the clone URL, the
	// remote ref already holds this run's head — same evidence, no forge
	// call beyond the GET, no push.
	s, f, lg = repoSVC(true, nil, nil, true)
	f.SeedRepo("acme", "widget", forge.Repo{Empty: false, DefaultBranch: "main",
		URL: "example.invalid/acme/widget", CloneURL: "example.invalid/acme/widget.git"})
	lg.resolver = func(args []string) (string, error) {
		if len(args) >= 3 && args[0] == "remote" && args[1] == "get-url" {
			return "example.invalid/acme/widget.git", nil
		}
		return pushSHA, nil // rev-parse, ls-remote — all one SHA: already published
	}
	ev, err = DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if ev["pushed_sha"] != pushSHA {
		t.Fatalf("re-run evidence: %v", ev)
	}
	if len(f.Calls()) != 1 {
		t.Fatalf("re-run forge calls: %v — one GET, no POST", f.Calls())
	}
	if containsWord(logCommands(lg), "push") {
		t.Fatal("the no-op re-run pushed again")
	}
}

func TestCreateRepoNeverRetargetsSomebodysOrigin(t *testing.T) {
	s, _, lg := repoSVC(true, nil, nil, true)
	lg.resolver = func(args []string) (string, error) {
		if len(args) >= 3 && args[0] == "remote" && args[1] == "get-url" {
			return "git@somewhere.else:other/repo.git", nil
		}
		return gitExit2(args)
	}
	_, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "never retargets") {
		t.Fatalf("foreign origin: %v", err)
	}
	joined := strings.Join(flatten(logCommands(lg)), "\n")
	if strings.Contains(joined, "set-url") || strings.Contains(joined, "remove") {
		t.Fatalf("a remote was rewritten anyway:\n%s", joined)
	}
}

// TestCreateRepoForgeErrorsTravelVerbatim — the hub's conflict and the
// fake's injected auth refusal reach the caller in the forge's own
// three-part words; a paraphrase would lose the alternatives.
func TestCreateRepoForgeErrorsTravelVerbatim(t *testing.T) {
	s, f, lg := repoSVC(true, nil, nil, true)
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	f.FailMethod("CreateRepo", forge.NewMsg(forge.KindConflict, "conflict: name already exists"))
	_, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "conflict: name already exists") {
		t.Fatalf("verbatim: %v", err)
	}
}

func TestCreateRepoNeedsAForgeThatCanCreate(t *testing.T) {
	s, _, lg := repoSVC(true, nil, nil, true)
	s.Forge = &ForgeActions{Forge: nonRepoForge{}} // a Forge without the optional doors
	lg.resolver = func(args []string) (string, error) { return gitExit2(args) }
	_, err := DispatchFrom(context.Background(), s, Caller{Kind: CallerPerson}, ForgeCreateRepo, repoArgs("private"))
	if err == nil || !strings.Contains(err.Error(), "cannot create repositories") {
		t.Fatalf("absent capability: %v", err)
	}
}

// nonRepoForge is a forge.Forge with NO repository doors — the type that
// proves the capability is asserted, not assumed. It deliberately does
// NOT embed *forge.Fake (whose GetRepo/CreateRepo would arrive through
// the embedding and make the assertion vacuous): ten stubs, each
// unreachable in this test, because the door must refuse BEFORE the first
// call of any kind.
type nonRepoForge struct{}

var _ forge.Forge = nonRepoForge{}

func (nonRepoForge) err() (int, error) {
	return 0, fmt.Errorf("a plain forge answers no repository scenario")
}
func (n nonRepoForge) FindOpenIssue(context.Context, string, string) (*forge.Issue, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) OpenIssue(context.Context, forge.IssueRequest) (*forge.Issue, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) ReadIssue(context.Context, int) (*forge.Issue, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) CloseIssue(context.Context, int) error { _, err := n.err(); return err }
func (n nonRepoForge) FindOpenPR(context.Context, string) (*forge.PR, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) OpenPR(context.Context, forge.PRRequest) (*forge.PR, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) ReadPR(context.Context, int) (*forge.PR, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) MergePR(context.Context, int, forge.MergeMethod) (*forge.MergeResult, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) ListReviews(context.Context, int) ([]forge.Review, error) {
	_, err := n.err()
	return nil, err
}
func (n nonRepoForge) PostReview(context.Context, int, string, forge.Verdict) (*forge.Review, error) {
	_, err := n.err()
	return nil, err
}

// --- helpers -----------------------------------------------------------------

const pushSHA = "0123456789012345678901234567890123456789"

// gitExit2 answers the scripted git world: remote get-url says ABSENT
// (exit 2 is the absence), every other command succeeds, and the two
// shas (rev-parse, ls-remote) are one SHA — published.
func gitExit2(args []string) (string, error) {
	if len(args) >= 2 && args[0] == "remote" && args[1] == "get-url" {
		return "", &git.GitError{Kind: git.KindExit, Detail: "no such remote", Code: 2}
	}
	if len(args) >= 1 && (args[0] == "rev-parse" || args[0] == "ls-remote") {
		if args[0] == "ls-remote" {
			return pushSHA + "\trefs/heads/main\n", nil
		}
		return pushSHA, nil
	}
	return "", nil
}

func logCommands(lg *logGit) [][]string {
	return lg.since(0)
}

func containsWord(cmds [][]string, words ...string) bool {
	for _, c := range cmds {
		for i := 0; i+len(words) <= len(c); i++ {
			match := true
			for j, w := range words {
				if c[i+j] != w {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func flatten(cmds [][]string) []string {
	var out []string
	for _, c := range cmds {
		out = append(out, strings.Join(c, " "))
	}
	return out
}
