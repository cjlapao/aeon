package daemon

// mergequeue_test.go — the S60 T7 harness: the daemon, a real sqlite
// store, REAL git repositories with a local bare origin (the walk's
// fixture shape — commit and push and merge are git facts, never mocks),
// and the forge.Fake standing where GitHub would be (it cannot ride a
// real binary; the walk's record). The landing and resolution loops run
// for REAL through the daemon's runner with the shipped blueprints
// seeded — the reactor decides, the engine executes, and the assertions
// read git, the ledger, and the fake's call log.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/landingseed"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
)

type mqEnv struct {
	t        *testing.T
	d        *Daemon
	s        *store.Store
	w        *wire       // captures the daemon's log lines for failure dumps (swap on reopen)
	ws       string      // the component root (a real clone)
	storeDir string      // where the sqlite file lives (the kill-and-reopen door)
	origin   string      // the bare origin's path
	forger   forge.Forge // the fake behind the test's own opening shim
	fake     *forge.Fake
	conn     string
	// resolveScript is the resolve lane's stand-in model: it runs IN the
	// integration worktree while the model step stands open. nil means the
	// lane must never run (a Model call then fails the test).
	resolveScript func(t *testing.T, dir string)
	// conflictSides are the two conflicting file bodies. The default is
	// the plain words; TestNoHunkTextAnywhere plants sentinel-looking
	// secrets (behaviour 7) and every scan then refuses them anywhere.
	sideFeature string
	sideBase    string
}

func mqGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=mq-fixture", "GIT_AUTHOR_EMAIL=mq@aeon.test",
		"GIT_COMMITTER_NAME=mq-fixture", "GIT_COMMITTER_EMAIL=mq@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C", "HOME="+dir)
	cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		if ee, ok := err.(*exec.ExitError); ok {
			detail = string(ee.Stderr)
		}
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, detail)
	}
	return strings.TrimSpace(string(out))
}

// landingSeedRows unpacks the shipped landing blueprints' seed envelopes
// into the map bpSeedRows writes as rows — the loops start from the SAME
// rows the seed set would write (the seed contract itself is asserted by
// TestLandingBlueprintsSeeded).
func landingSeedRows(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rec := range landingSource() {
		var body struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(rec.Content, &body); err != nil {
			t.Fatalf("landing seed record %s: %v", rec.Key, err)
		}
		out[body.Name] = body.Content
	}
	return out
}

func newMQEnv(t *testing.T) *mqEnv {
	t.Helper()
	storeDir := t.TempDir()
	s, err := store.Open(store.Options{Dir: storeDir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ws := t.TempDir()
	origin := t.TempDir() + "/origin.git"
	mqGit(t, ws, "init", "-b", "main")
	mqGit(t, ws, "config", "user.email", "mq@aeon.test")
	mqGit(t, ws, "config", "user.name", "MQ Fixture")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("# mq\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqGit(t, ws, "add", ".")
	mqGit(t, ws, "commit", "-m", "initial")
	mqGit(t, ws, "clone", "--bare", ws, origin)
	mqGit(t, ws, "remote", "add", "origin", origin)
	mqGit(t, ws, "push", "-u", "origin", "main")
	mqGit(t, ws, "remote", "set-head", "origin", "main") // origin/HEAD names its default, like a real forge remote

	cw, err := s.CreateComponent(store.NewComponent{Name: "mq root", Slug: "mq-root", Root: ws})
	if err != nil || !cw.OK() {
		t.Fatalf("create root component: %v %s", err, cw.Reason)
	}

	fake := forge.NewFake()
	dw := &wire{}
	d := New(Options{Store: s, Logger: dw})

	bpSeedRows(t, s, landingSeedRows(t))

	// The repository has one gate, and it passes (the fixture's CI —
	// verify.run with ZERO gates reads as an unverified run, which the
	// landing lane refuses; a landing queue always verifies against
	// something).
	// …and the verifier's policy says that command may run (the gate is
	// judged under the same spawn policy a step's spawns are — one
	// source, two call sites).
	mspec, err := json.Marshal(struct {
		Command string `json:"command"`
		State   string `json:"state"`
	}{Command: "true", State: string(policy.Allow)})
	if err != nil {
		t.Fatal(err)
	}
	if rej := policy.NewStore(newPolicyRepo(s)).Add(context.Background(), policy.Policy{
		ID: "mq-allow-true", Type: policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: bindPolicySubject(action.VerifierAgent)},
		Action: policy.Allow, Source: policy.Operator, Spec: mspec,
	}, time.Now()); rej != nil {
		t.Fatalf("seed the command rule: %s", rej.Reason)
	}

	vw, err := s.AddVerifyCommand(store.Actor{Kind: "user", ID: "op"}, store.NewVerifyCommand{
		ComponentID: cw.Component.ID, Name: "always-pass", Command: "true", Requirement: "required", TimeoutS: 30,
	})
	if err != nil || !vw.OK() {
		t.Fatalf("seed the passing gate: %v %s", err, vw.Reason)
	}

	e := &mqEnv{d: d, w: dw, s: s, ws: ws, origin: origin, fake: fake, storeDir: storeDir}

	prevForge := mqForgeOverride
	forger := &mqForgeShim{Fake: fake, ws: ws, origin: origin, t: t, merged: map[int]string{}}
	e.forger = forger
	mqForgeOverride = func(string) forge.Forge { return forger }
	t.Cleanup(func() { mqForgeOverride = prevForge })

	e.conn = "mq-cli"
	initConn(t, d, context.Background(), e.conn, "aeon-cli")

	e.installRunner(t)
	e.d = d
	return e
}

// killAndReopen is behaviour 9's kill: close the store, build a FRESH
// daemon over the same rows, the same worktrees, the same origin, and the
// same forge (a real forge outlives the daemon — the PR rows do not
// evaporate when the process does). Nothing is carried in memory: the
// reopened reactor must re-derive every action from rows alone.
func (e *mqEnv) killAndReopen(t *testing.T) {
	t.Helper()
	_ = e.s.Close()
	s, err := store.Open(store.Options{Dir: e.storeDir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	e.s = s
	dw := &wire{}
	d := New(Options{Store: s, Logger: dw})
	e.w = dw
	e.d = d
	e.installRunner(t)
	initConn(t, d, context.Background(), e.conn, "aeon-cli")
}

// mqMergeProbe answers forge.land's post-merge guard the way the S59 walk
// answers it: the fake fabricates the merge SHA (it never lands in the
// tree), so the guard asks the WITNESS — did you merge that PR at that
// SHA into that base?
type mqMergeProbe struct {
	env *mqEnv
}

func (p mqMergeProbe) IsAncestor(ctx context.Context, _, sha, ref string) (bool, error) {
	base := strings.TrimPrefix(ref, "origin/")
	for _, c := range p.env.fake.Calls() {
		if c.Method != "MergePR" {
			continue
		}
		if len(c.Args) < 1 {
			continue
		}
		n, ok := c.Args[0].(int)
		if !ok {
			continue
		}
		pr, err := p.env.fake.ReadPR(ctx, n)
		if err != nil {
			return false, err
		}
		if pr.Merged && pr.MergeSHA == sha && pr.BaseRef == base {
			return true, nil
		}
	}
	return false, nil
}

func (e *mqEnv) installRunner(t *testing.T) {
	t.Helper()
	e.d.loopRunner = loop.NewRunner(loop.Deps{
		Store:   e.s,
		Records: recordsReaderFor(e.s),
		Event:   func(string, any) {},
		Actions: func(root string) *action.Services {
			svc := e.d.actionServices(root)
			gitC := svc.Git.Git
			svc.Forge = &action.ForgeActions{
				Forge:   e.forger,
				Git:     gitC,
				Merge:   mqMergeProbe{env: e},
				Intents: storeIntents{s: e.s},
				Rule:    mqRuleAdapter{d: e.d, root: root},
				Grants:  mqGrantsAdapter{d: e.d},
				Fetch:   svc.Git,
			}
			return svc
		},
		Model: func(ctx context.Context, st loop.LoopState, step blueprint.Step, _, _ string) (loop.ModelResult, error) {
			if e.resolveScript == nil {
				t.Errorf("a model step ran with no resolve script (blueprint %s, lane %s)", st.Blueprint.ID, step.ID)
				return loop.ModelResult{}, fmt.Errorf("no model in the merge-queue walk")
			}
			e.resolveScript(t, st.With["integration_dir"])
			// The engine checks a declared artifact at the LOOP ROOT
			// (missingArtifact(st.Root, …)) — a real model writes its
			// memo where the loop runs, not only in the worktree.
			if err := os.WriteFile(filepath.Join(st.Root, "resolve-notes.md"),
				[]byte("resolved: kept both sides of shared.md (queue fixture resolver)\n"), 0o644); err != nil {
				return loop.ModelResult{}, err
			}
			return loop.ModelResult{Kind: "completed", Outcome: "ok"}, nil
		},
		Autonomy: loop.AutonomySeams{Seams: autonomy.Seams{}},
	})
	e.d.loopRunner.SetPark(func(string) {})
}

// mustGitOut runs a read-mostly git command and returns its stdout.
func mustGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return mqGit(t, dir, args...)
}

// feature pushes a branch with one commit ahead of main and returns its
// name. base may name the branch to branch from (default main).
func (e *mqEnv) feature(t *testing.T, name, base string) string {
	t.Helper()
	if base == "" {
		base = "main"
	}
	mqGit(t, e.ws, "checkout", base)
	mqGit(t, e.ws, "pull", "--ff-only", "origin", base)
	mqGit(t, e.ws, "checkout", "-b", name)
	if err := os.WriteFile(filepath.Join(e.ws, name+".md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqGit(t, e.ws, "add", ".")
	mqGit(t, e.ws, "commit", "-m", name)
	mqGit(t, e.ws, "push", "-u", "origin", name)
	mqGit(t, e.ws, "checkout", base)
	return name
}

// baseMove commits directly to main ON A SECOND CLONE and pushes — the
// world moving under the queue's feet, the way it moves in fact.
func (e *mqEnv) baseMove(t *testing.T, name string) {
	t.Helper()
	elsewhere := t.TempDir() + "/elsewhere"
	mqGit(t, filepath.Dir(elsewhere), "clone", e.origin, elsewhere)
	mqGit(t, elsewhere, "config", "user.email", "other@aeon.test")
	mqGit(t, elsewhere, "config", "user.name", "Other")
	if err := os.WriteFile(filepath.Join(elsewhere, name+".md"), []byte("# moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqGit(t, elsewhere, "add", ".")
	mqGit(t, elsewhere, "commit", "-m", "base move "+name)
	mqGit(t, elsewhere, "push", "origin", "main")
}

// conflictFeature branches from main, edits the SAME line main later
// edits, and pushes — the fixture for behaviours 4 and 5. The caller
// moves the base with the same edit text for a guaranteed conflict.
func (e *mqEnv) conflictFeature(t *testing.T, name string) string {
	t.Helper()
	mqGit(t, e.ws, "checkout", "main")
	mqGit(t, e.ws, "checkout", "-b", name)
	if err := os.WriteFile(filepath.Join(e.ws, "shared.md"), []byte(e.sideOr(e.sideFeature, "feature side")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqGit(t, e.ws, "add", ".")
	mqGit(t, e.ws, "commit", "-m", name+" (conflicting side)")
	mqGit(t, e.ws, "push", "-u", "origin", name)
	mqGit(t, e.ws, "checkout", "main")
	return name
}

// conflictBaseMove is baseMove's twin with the EXACT same file written
// differently — the one-line conflict the queue must answer.
func (e *mqEnv) conflictBaseMove(t *testing.T) {
	t.Helper()
	elsewhere := t.TempDir() + "/elsewhere"
	mqGit(t, filepath.Dir(elsewhere), "clone", e.origin, elsewhere)
	mqGit(t, elsewhere, "config", "user.email", "other@aeon.test")
	mqGit(t, elsewhere, "config", "user.name", "Other")
	if err := os.WriteFile(filepath.Join(elsewhere, "shared.md"), []byte(e.sideOr(e.sideBase, "base side")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqGit(t, elsewhere, "add", ".")
	mqGit(t, elsewhere, "commit", "-m", "base side wins elsewhere")
	mqGit(t, elsewhere, "push", "origin", "main")
}

// sideOr is the conflict fixture's default: the planted side if the test
// set one, the plain word otherwise.
func (e *mqEnv) sideOr(planted, def string) string {
	if planted != "" {
		return planted
	}
	return def
}

func (e *mqEnv) svc() *mergequeue.Service { return mergequeue.NewService(e.s) }

func (e *mqEnv) enqueue(t *testing.T, branch string) store.MergeItem {
	t.Helper()
	return e.enqueueAttended(t, branch, true)
}

// enqueueAttended is the one door with the attendedness the item rides
// (behaviour 8's unattended leg needs it false).
func (e *mqEnv) enqueueAttended(t *testing.T, branch string, attended bool) store.MergeItem {
	t.Helper()
	facts, head, baseSHA, err := e.d.mqRepoFacts(context.Background(), e.ws, branch, "main")
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.svc().Enqueue(
		mergequeue.EnqueueRequest{ComponentRoot: e.ws, Branch: branch, BaseBranch: "main", Attended: attended},
		facts,
		store.NewMergeItem{ComponentRoot: e.ws, BaseBranch: "main", Branch: branch, HeadSHA: head, BaseSHA: baseSHA, Attended: attended,
			EnqueuedByKind: "cli", EnqueuedByID: "test"},
		store.MergeActor{Kind: "cli", ID: "test"},
	)
	if err != nil {
		t.Fatalf("enqueue %s: %v", branch, err)
	}
	return m
}

func (e *mqEnv) configure(t *testing.T, mode, policy string, maxCatchUps int) {
	t.Helper()
	_, _, err := e.svc().Settings(store.NewMergeConfig{
		ComponentRoot: e.ws, Mode: mode, ConflictPolicy: policy, LandMethod: "squash", MaxCatchUps: maxCatchUps,
	}, store.MergeActor{Kind: "cli", ID: "test"})
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
}

// grant stands real autonomy grants on the component (S56 rows, not
// stubs): the landing set a lightened repository's person stands —
// publish, PR, land. Tests name exactly which they need.
func (e *mqEnv) grant(t *testing.T, subjects ...string) {
	t.Helper()
	comp, err := e.s.ComponentForRoot(e.ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(subjects) == 0 {
		subjects = []string{"git.publishIntegration", "forge.openLandingPR", "forge.land"}
	}
	for _, sub := range subjects {
		_, refusal, err := e.s.CreateAutonomyGrant(store.Actor{Kind: "user", ID: "op"}, store.NewAutonomyGrant{
			NodeID: comp.ID, SubjectKind: "action", Subject: sub, State: "allow", Conditions: "{}",
		})
		if err != nil || refusal != nil {
			t.Fatalf("grant %s: %v %v", sub, err, refusal)
		}
	}
}

func (e *mqEnv) item(t *testing.T, id string) store.MergeItem {
	t.Helper()
	m, err := e.s.ReadMergeItem(id)
	if err != nil {
		t.Fatalf("read item %s: %v", id, err)
	}
	return m
}

// settle drives the reactor and lets the runner work, until every open
// item rests in a state the reactor will not move on its own OR the
// timeout — then one last tick so a decision made mid-window lands.
func (e *mqEnv) settle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	prev := e.dump() + " | " + e.dumpLoops()
	stable := 0
	for time.Now().Before(deadline) {
		e.d.reconcileMergeQueue(context.Background())
		e.waitQuiet(t)
		cur := e.dump() + " | " + e.dumpLoops()
		if cur == prev {
			stable++
			if stable >= 3 {
				return
			}
		} else {
			stable = 0
		}
		prev = cur
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("settle timed out; items: %s\nloops: %s\ndaemon log:\n%s", e.dump(), e.dumpLoops(), e.w.logLines())
}

// waitQuiet lets every active runner goroutine finish its pass (the
// engine writes its state on its own goroutine; the reactor must read
// AFTER the pass, never during it).
func (e *mqEnv) waitQuiet(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for e.d.loopRunner != nil && e.d.loopRunner.ActiveCount() > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("runner goroutines never settled")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// quiet answers "the reactor has nothing immediate left to do": no open
// item sits in a mover state whose driver is not standing busy.
func (e *mqEnv) quiet() bool {
	items, err := e.s.ListMergeItems(store.MergeFilter{ComponentRoot: e.ws, Statuses: store.MergeOpenStatusSet})
	if err != nil {
		return false
	}
	for _, it := range items {
		switch it.Status {
		case "queued", "catching_up", "verifying", "merging", "conflicted":
			if it.LandingLoopSlug != "" || it.ResolveLoopSlug != "" {
				st, found := e.d.mqLoopState(context.Background(), e.ws, it.LandingLoopSlug)
				if found && mqLoopEnum(st) == mergequeue.LoopPending {
					return false
				}
				st2, found2 := e.d.mqLoopState(context.Background(), e.ws, it.ResolveLoopSlug)
				if found2 && mqLoopEnum(st2) == mergequeue.LoopPending {
					return false
				}
				if !found && !found2 {
					return false // about to start
				}
			}
		}
	}
	if e.d.loopRunner != nil && e.d.loopRunner.ActiveCount() > 0 {
		return false
	}
	return true
}

// dumpLoops prints every queue-minted loop's pin status for failures.
func (e *mqEnv) dumpLoops() string {
	items, err := e.s.ListMergeItems(store.MergeFilter{ComponentRoot: e.ws})
	if err != nil {
		return err.Error()
	}
	parts := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		for _, slug := range []string{it.LandingLoopSlug, it.ResolveLoopSlug} {
			if slug == "" || seen[slug] {
				continue
			}
			seen[slug] = true
			st, found := e.d.mqLoopState(context.Background(), e.ws, slug)
			if !found {
				parts = append(parts, slug+"=absent")
				continue
			}
			q := ""
			if pq, found2, _ := e.s.PendingQuestion(store.Scope{ComponentRoot: e.ws, LoopSlug: slug}); found2 {
				q = " q=" + pq.Body
			}
			parts = append(parts, fmt.Sprintf("%s=%s/%s@%s(%s)%s", slug, st.Status, st.BlockKind, st.Position.Lane, st.LastReason, q))
		}
	}
	return strings.Join(parts, ", ")
}

func (e *mqEnv) dump() string {
	items, err := e.s.ListMergeItems(store.MergeFilter{ComponentRoot: e.ws, Statuses: store.MergeOpenStatusSet})
	if err != nil {
		return err.Error()
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s(%s %v landed=%q catch=%d)", it.ID, it.Status, it.ReasonKind, it.LandedSHA, it.CatchUps))
	}
	return strings.Join(parts, ", ")
}

// waitFor drives the reactor until the item reports one of want.
func (e *mqEnv) waitFor(t *testing.T, id string, want ...string) store.MergeItem {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		e.d.reconcileMergeQueue(context.Background())
		e.waitQuiet(t)
		it := e.item(t, id)
		for _, w := range want {
			if it.Status == w {
				return it
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("item %s never reached %v; queue says: %s\nloops: %s\ndaemon log:\n%s", id, want, e.dump(), e.dumpLoops(), e.w.logLines())
	return store.MergeItem{}
}

// answerGate answers the landing loop's standing gate question through the
// answered door (the CLI's act — the same dispatch a person's
// `aeon loop answer` rides).
// waitWaiting waits for the loop at this slug to stand waiting on its
// pending question and answers the test with the loop ID. A person answers
// a pending question; the test waits for the pending question rather than
// sleeping on a guess.
func (e *mqEnv) waitWaiting(t *testing.T, slug string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		e.d.reconcileMergeQueue(context.Background())
		rows, err := e.s.LoopsBySlug(slug)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.ComponentRoot == e.ws && r.Status == "waiting" {
				e.waitQuiet(t)
				return r.ID
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("loop %s never stood waiting on a question\nqueue: %s\nloops: %s\nlog:\n%s",
		slug, e.dump(), e.dumpLoops(), e.w.logLines())
	return ""
}

// waitForPR waits for the landing PR to exist on the forge — the loop
// opens it in its own lane; the test reads the number back rather than
// assuming it.
func (e *mqEnv) waitForPR(t *testing.T, id string) *int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		e.d.reconcileMergeQueue(context.Background())
		if n := e.item(t, id).PRNumber; n != nil && *n > 0 {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("item %s never opened a landing PR\nqueue: %s\nloops: %s\nlog:\n%s",
		id, e.dump(), e.dumpLoops(), e.w.logLines())
	return nil
}

func (e *mqEnv) answerGate(t *testing.T, loopID, answer string) any {
	t.Helper()
	params, err := json.Marshal(protocol.LoopAnswerParams{ID: loopID, Answer: answer})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.d.Table.Dispatch(context.Background(), e.conn, protocol.MethodLoopAnswer, params)
	if err != nil {
		t.Fatalf("answer %q on %s: %v", answer, loopID, err)
	}
	return res
}

// loopIDFor resolves a slug to its loop row's id (the answer door speaks
// loop ids; the queue keeps slugs).
func (e *mqEnv) loopIDFor(t *testing.T, slug string) string {
	t.Helper()
	rows, err := e.s.LoopsBySlug(slug)
	if err != nil || len(rows) == 0 {
		t.Fatalf("no loop at slug %s: %v", slug, err)
	}
	for _, r := range rows {
		if r.ComponentRoot == e.ws {
			return r.ID
		}
	}
	t.Fatalf("loop at %s is not in this root", slug)
	return ""
}

// loopStateFor reads the loop row at this slug in this env's root, when
// one exists.
func (e *mqEnv) loopStateFor(t *testing.T, slug string) (store.Loop, bool) {
	t.Helper()
	rows, err := e.s.LoopsBySlug(slug)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ComponentRoot == e.ws {
			return r, true
		}
	}
	return store.Loop{}, false
}

func (e *mqEnv) mergeCalls() int {
	n := 0
	for _, c := range e.fake.Calls() {
		if c.Method == "MergePR" {
			n++
		}
	}
	return n
}

func (e *mqEnv) openPRCalls() int {
	n := 0
	for _, c := range e.fake.Calls() {
		if c.Method == "OpenPR" {
			n++
		}
	}
	return n
}

// landRequest is the queue's land door (the T8 surface's core; T7 drives
// it directly).
func (e *mqEnv) landRequest(itemID string) { e.d.RequestMergeQueueLand(itemID) }

// resolveWorktree resolves the in-progress merge the way a person's
// resolver would: keep BOTH sides (the queue resolves, it does not
// choose winners for people), finish the merge commit.
func mqResolveBothSides(t *testing.T, dir string) {
	t.Helper()
	// scripted: take BOTH sides for the shared file, add, commit — the
	// resolver keeps both claims; the queue never picks winners for people.
	if _, err := os.Stat(dir); err != nil {
		// No integration worktree (a forced fixture state never built
		// one): the resolver has nothing to resolve; the engine's own
		// conclude lane is the one that will say so.
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "shared.md")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "shared.md"), []byte("base side\nfeature side\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mqGit(t, dir, "add", ".")
	mqGitOK(t, dir, "add", ".")
	if out, ok := mqGitOK(t, dir, "commit", "--no-edit"); !ok {
		// Not (or no longer) a merge in progress — a plain commit of the
		// resolved index says so itself, and a tree that is ALREADY the
		// resolution (the engine retried the step after the artifact
		// check, not the work) is the same answer twice: idempotent.
		if out2, ok2 := mqGitOK(t, dir, "commit", "-m", "resolve: keep both sides"); !ok2 && !strings.Contains(out2, "nothing to commit") {
			st, _ := mqGitOK(t, dir, "status", "--porcelain=v1", "--untracked-files=no")
			head, _ := mqGitOK(t, dir, "log", "--oneline", "-3")
			t.Fatalf("resolving commit refused:\n%s\n%s\nstatus: %q\nhead: %q", out, out2, st, head)
		}
	}
	// The model-step's declared artifact — a real resolver's memo of what
	// it kept; the engine checks the FILE, never the model's say-so.
	if err := os.WriteFile(filepath.Join(dir, "resolve-notes.md"),
		[]byte("resolved: kept both sides of shared.md (queue fixture resolver)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mqGitOK runs git where a refusal is an ANSWER, not a fixture failure.
func mqGitOK(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=mq-fixture", "GIT_AUTHOR_EMAIL=mq@aeon.test",
		"GIT_COMMITTER_NAME=mq-fixture", "GIT_COMMITTER_EMAIL=mq@aeon.test",
		"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C", "HOME="+dir)
	cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), false
	}
	return strings.TrimSpace(string(out)), true
}

// mqIntegrationDirsExist is the worktree-existence check behaviour 1 and
// 10 read (the composed path, not a guess).
func mqWorktreeGone(t *testing.T, root, itemID string) bool {
	t.Helper()
	_, err := os.Stat(action.IntegrationWorktreePath(root, itemID))
	return os.IsNotExist(err)
}

// landingSource is the shipped set's records — the same bytes the seed
// engine applies, read here for the row seeding above and the seed
// contract test below.
func landingSource() []seed.Record { return landingseed.Source() }

// mqForgeShim is the fixture's GitHub honesty: when a PR opens, the
// server reports the head branch's SHA (pull.head.sha) — the fake alone
// cannot know the git world's SHAs, so the shim asks the fixture's OWN
// repository for the pushed ref and stamps the row, and a reviewed PR is
// a PR with an approving review (the D10 gate reads it). One opened PR,
// one head SHA, one review — before OpenPR returns, so the land guard
// that runs a step later never races it.
type mqForgeShim struct {
	*forge.Fake
	ws     string
	origin string
	t      *testing.T
	merged map[int]string
}

func (s *mqForgeShim) OpenPR(ctx context.Context, req forge.PRRequest) (*forge.PR, error) {
	pr, err := s.Fake.OpenPR(ctx, req)
	if err != nil {
		return pr, err
	}
	head := strings.TrimSpace(mqGit(s.t, s.ws, "ls-remote", "--heads", "origin", req.Head))
	if fields := strings.Fields(head); len(fields) > 0 {
		s.Fake.SeedPR(forge.PR{Number: pr.Number, Head: pr.Head, BaseRef: pr.BaseRef, HeadSHA: fields[0]})
	}
	s.Fake.SeedReview(pr.Number, "queue walk-through", forge.VerdictApprove)
	return pr, nil
}

// MergePR is the git half of the lie: when the fake reports a merge, the
// fixture's REAL base moves — a merge commit onto main, pushed, exactly
// as a squash-free landing would move it. The row then carries the real
// merge SHA, so the queue's ancestry check against origin/main answers
// from the git world, not from a fabricated number. One real merge per
// PR, ever (behaviour 9's leg), remembered here.
func (s *mqForgeShim) MergePR(ctx context.Context, number int, method forge.MergeMethod) (*forge.MergeResult, error) {
	res, err := s.Fake.MergePR(ctx, number, method)
	if err != nil {
		return res, err
	}
	if sha, done := s.merged[number]; done {
		return &forge.MergeResult{SHA: sha, Merged: true}, nil
	}
	pr, err := s.Fake.ReadPR(ctx, number)
	if err != nil || pr == nil || pr.Head == "" {
		return res, err
	}
	dir := s.t.TempDir()
	mqGit(s.t, dir, "clone", s.origin, "w")
	repo := dir + "/w"
	mqGit(s.t, repo, "-c", "advice.detachedHead=false", "checkout", "main")
	mqGit(s.t, repo, "merge", "--no-ff", "--no-commit", "origin/"+pr.Head)
	mqGit(s.t, repo, "commit", "-m", "Merge "+pr.Head+" (#"+strconv.Itoa(number)+")")
	sha := strings.TrimSpace(mqGit(s.t, repo, "rev-parse", "HEAD"))
	mqGit(s.t, repo, "push", "origin", "main")
	s.merged[number] = sha
	s.Fake.SeedPR(forge.PR{Number: pr.Number, Head: pr.Head, BaseRef: pr.BaseRef, HeadSHA: pr.HeadSHA, Merged: true, MergeSHA: sha})
	return &forge.MergeResult{SHA: sha, Merged: true}, nil
}
