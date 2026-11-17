// harness_test.go — the whole-engine stand: a real migrated store, a real
// git (identity pinned so no operator's ~/.gitconfig is ever read), the T4
// action doors wired over a template book and a forge fake, the real
// charter service, and the engine over all of it. The one fiction is the
// remote: `push` and `ls-remote` answer a pretend origin (no test reaches a
// network), and every other git command runs the real binary.
package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/scaffold"
	"github.com/cjlapao/aeon/internal/store"
)

var testUser = store.Actor{Kind: "user", ID: "carlos"}
var testAgent = store.Actor{Kind: "agent", ID: "interview-loop"}

const charterBody = `{"goal":"ship the intake plane","constraints":["no new daemon"],` +
	`"nonGoals":["no UI rewrite"],"stack":["golang","sqlite"],` +
	`"acceptance":[{"id":"ac-1","text":"the door answers","check":"command","ref":"aeon intake status"},` +
	`{"id":"ac-2","text":"a human approves","check":"manual"}]}`

// --- the template book ----------------------------------------------------

type tmplBook struct {
	mu  sync.Mutex
	ver map[string]int
}

func newBook() *tmplBook { return &tmplBook{ver: map[string]int{"base": 1}} }

func (b *tmplBook) get(_ context.Context, slug string) (scaffold.Template, int, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.ver[slug]
	if !ok {
		return scaffold.Template{}, 0, false, nil
	}
	return scaffold.Template{
		Slug: slug,
		Files: []scaffold.TemplateFile{
			{Path: "README.md", Content: "{{.Name}}\n\n{{.Description}}\n", Mode: 0o644},
			{Path: "stack.txt", Content: "{{join .Stack \" \"}}\n", Mode: 0o644},
			{Path: "branch.txt", Content: "{{.DefaultBranch}}\n", Mode: 0o644},
		},
	}, v, true, nil
}

// bump is TestCommitRefusesStableDigest's world event: a template shipped
// a new version between the preview and the commit.
func (b *tmplBook) bump(slug string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ver[slug]++
}

// --- the hybrid git (real binary, pretend remote) --------------------------

func testGitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"GIT_AUTHOR_NAME=s61-fixture", "GIT_AUTHOR_EMAIL=s61@aeon.test",
		"GIT_COMMITTER_NAME=s61-fixture", "GIT_COMMITTER_EMAIL=s61@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	}
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), &git.GitError{Kind: git.KindExit, Detail: strings.TrimSpace(string(out)), Code: code}
	}
	return string(out), nil
}

// hybridGit answers `push` and `ls-remote` as if a remote had taken the
// push (the sha it reports is the REAL local head — the pretend origin
// never invents a sha the repo does not have); every other command runs the
// real git, so init, add, commit, log, status and remote-add are true.
func hybridGit() *git.Client {
	return &git.Client{Exec: func(ctx context.Context, dir string, args ...string) (string, error) {
		switch args[0] {
		case "push":
			return "To pretend-origin\n * [new branch]      " + args[len(args)-1] + " -> " + args[len(args)-1] + "\n", nil
		case "ls-remote":
			out, err := runGit(ctx, dir, "rev-parse", "HEAD")
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(out) + "\t" + args[len(args)-1] + "\n", nil
		}
		return runGit(ctx, dir, args...)
	}}
}

// --- the harness ------------------------------------------------------------

type harness struct {
	t         *testing.T
	st        *store.Store
	eng       *Engine
	fg        *forge.Fake
	book      *tmplBook
	root      string // the directory target paths are built under (a temp dir)
	closed    int
	settingOn bool
	grantErr  error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	state := t.TempDir()
	st, err := store.Open(store.Options{Dir: state, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	h := &harness{t: t, st: st, fg: forge.NewFake(), book: newBook(), root: root, settingOn: true}
	gitC := hybridGit()
	svc := &action.Services{
		Git:   &action.GitActions{Git: gitC},
		Forge: &action.ForgeActions{Forge: h.fg},
		Scaffold: &action.ScaffoldActions{
			Templates: h.book.get,
			TargetFacts: func(context.Context) (scaffold.TargetFacts, error) {
				comps, err := st.ListComponents(store.ComponentFilter{IncludeArchived: true})
				if err != nil {
					return scaffold.TargetFacts{}, err
				}
				var roots []string
				for _, c := range comps {
					if c.Root != "" {
						roots = append(roots, c.Root)
					}
				}
				return scaffold.TargetFacts{ComponentRoots: roots, Home: "/home/tester", StateDir: "/home/tester/.state/aeon"}, nil
			},
		},
		CreateRepo: &action.CreateRepoActions{
			SettingEnabled: func(context.Context) (bool, error) { return h.settingOn, nil },
			Grant:          func(context.Context) error { return h.grantErr },
		},
	}
	eng := &Engine{
		Store: st, Charter: &charter.Service{Store: st}, Actions: svc, Git: gitC,
		CloseLoop: func(context.Context) error { h.closed++; return nil },
	}
	h.eng = eng
	return h
}

// openIntake creates the session row with a real 0700 scratch under the
// harness root.
func (h *harness) openIntake(name string) string {
	h.t.Helper()
	in, ref, err := h.st.CreateIntake(name, filepath.Join(h.root, "scratch"), testUser)
	if err != nil || ref != nil {
		h.t.Fatalf("open intake: err=%v ref=%v", err, ref)
	}
	if err := os.MkdirAll(filepath.Join(h.root, "scratch"), 0o700); err != nil {
		h.t.Fatal(err)
	}
	return in.ID
}

// singlePlan is the one-component plan with a full records set: an epic,
// two goals with criteria, two items (one labeled with an estimate, one
// bare).
func singlePlan(intakeID string) desk.IntakePlanProposal {
	return desk.IntakePlanProposal{
		IntakeID: intakeID, Charter: json.RawMessage(charterBody),
		Components: []desk.IntakePlanComponent{{
			Name: "Alpha", Slug: "alpha", Template: "base", Role: "standalone",
			Description: "the one component", Tags: []string{"golang"},
		}},
		Epics: []desk.IntakePlanEpic{{Key: "e1", Title: "First epic", Objective: "prove the plane"}},
		Goals: []desk.IntakePlanGoal{
			{EpicKey: "e1", Title: "Commit lands", Type: "feature", BodyNote: "the whole run",
				ComponentSlugs: []string{"alpha"},
				Criteria: []desk.IntakePlanCriterion{
					{Text: "the door answers", Check: "command", Ref: "aeon intake status"},
					{Text: "a human approves", Check: "manual"},
				}},
			{Title: "Resume holds", Type: "chore", ComponentSlugs: []string{"alpha"}},
		},
		Items: []desk.IntakePlanItem{
			{ComponentSlug: "alpha", EpicKey: "e1", Title: "Write the engine", Priority: "should", Estimate: "3-5"},
			{ComponentSlug: "alpha", Title: "Walk the demo"},
		},
		Reason: "the plan the interview settled on",
	}
}

func treePlan(intakeID string) desk.IntakePlanProposal {
	p := singlePlan(intakeID)
	p.ProjectName = "Proj Two"
	p.Components = []desk.IntakePlanComponent{
		{Name: "Head", Slug: "head", Template: "base", Role: "root", Description: "the head repo", Tags: []string{"golang"}},
		{Name: "Kid", Slug: "kid", Template: "base", Role: "child", Description: "a child repo", Tags: []string{"golang"}},
	}
	for i := range p.Goals {
		p.Goals[i].ComponentSlugs = []string{"head", "kid"}
	}
	p.Items[0].ComponentSlug = "head"
	p.Items[1].ComponentSlug = "kid"
	return p
}

func (h *harness) propose(plan desk.IntakePlanProposal) int64 {
	h.t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		h.t.Fatal(err)
	}
	p, err := h.st.CreateProposal(string(desk.KindIntakePlan), string(b), testAgent, h.st.Now())
	if err != nil {
		h.t.Fatalf("propose: %v", err)
	}
	return p.ID
}

// ask builds the commit input and fills the digest from a live preview.
func (h *harness) ask(intakeID string, proposalID int64, target, publish, owner string, confirm bool) CommitInput {
	h.t.Helper()
	in := CommitInput{IntakeID: intakeID, ProposalID: proposalID, TargetPath: target,
		Publish: publish, Owner: owner, ConfirmPublic: confirm}
	if publish == "" {
		in.Publish = "none"
	}
	pv, _, err := h.eng.Plan(context.Background(), in)
	if err != nil {
		h.t.Fatalf("plan: %v", err)
	}
	in.Digest = pv.Digest
	return in
}

func (h *harness) target() string { return filepath.Join(h.root, "proj") }

// --- census helpers ---------------------------------------------------------

// filesUnder lists every file under dir (.git excluded — a run's commit
// sha is a timestamp, not a census fact) as "rel/pos:sha256".
func filesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		out = append(out, fmt.Sprintf("%s:%s", filepath.ToSlash(rel), hex.EncodeToString(sum[:])[:12]))
		return nil
	})
	sort.Strings(out)
	return out
}

type census struct {
	Files      []string
	Components map[string]string // slug → id
	Goals      []store.Goal
	Items      []store.RoadmapItemRow
	Criteria   int
	Revisions  int
	Ledger     []string // "step=outcome", ledger order
	Rows       int      // total ledger rows (duplicates show here)
	Proposal   store.Proposal
	RootID     string
}

func (h *harness) census(intakeID string) census {
	h.t.Helper()
	ctx := context.Background()
	c := census{Components: map[string]string{}}
	comps, err := h.st.ListComponents(store.ComponentFilter{IncludeArchived: true})
	if err != nil {
		h.t.Fatal(err)
	}
	for _, comp := range comps {
		c.Components[comp.Slug] = comp.ID
	}
	for _, comp := range comps {
		goals, err := h.st.ListGoals(store.GoalFilter{ComponentID: comp.ID})
		if err != nil {
			h.t.Fatal(err)
		}
		for _, g := range goals {
			c.Goals = append(c.Goals, g)
			cs, err := h.st.GoalCriteria(g.ID, 1)
			if err != nil {
				h.t.Fatal(err)
			}
			c.Criteria += len(cs)
		}
		if comp.Root == "" {
			continue
		}
		items, err := h.st.ListRoadmapItems(comp.Root, store.RoadmapItemFilters{})
		if err != nil {
			h.t.Fatal(err)
		}
		c.Items = append(c.Items, items...)
	}
	in, err := h.st.GetIntake(intakeID)
	if err != nil {
		h.t.Fatal(err)
	}
	c.RootID = in.RootID
	if in.RootID != "" {
		revs, err := (&charter.Service{Store: h.st}).Revisions(ctx, in.RootID)
		if err != nil {
			h.t.Fatal(err)
		}
		c.Revisions = len(revs)
	}
	steps, err := h.st.IntakeSteps(intakeID)
	if err != nil {
		h.t.Fatal(err)
	}
	c.Rows = len(steps)
	for _, s := range steps {
		c.Ledger = append(c.Ledger, s.Step+"="+s.Outcome)
	}
	props, err := h.st.ListProposals("")
	if err != nil {
		h.t.Fatal(err)
	}
	for _, p := range props {
		if p.Kind == string(desk.KindIntakePlan) {
			c.Proposal = p
		}
	}
	c.Files = filesUnder(h.target())
	return c
}

// collapsedLedger reduces the ledger rows to the LAST word per step — the
// shape two runs must share even when one re-ran a step.
func collapsed(rows []string) []string {
	last := map[string]string{}
	var order []string
	for _, r := range rows {
		step, outcome, _ := strings.Cut(r, "=")
		if _, seen := last[step]; !seen {
			order = append(order, step)
		}
		last[step] = outcome
	}
	out := make([]string, 0, len(order))
	for _, s := range order {
		out = append(out, s+"="+last[s])
	}
	return out
}

func hexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
