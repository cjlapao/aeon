package daemon

// Fixture and wiring helpers live here; autonomy_demo_rows_test.go holds the
// numbered walk and autonomy_demo_cli_test.go owns its subprocess build.

// autonomy_demo_walk_test.go — S56 T11, step 3: the demo walk. A real daemon
// (daemon.New over a real store, the real start door, the real decision site,
// the real method table, the real store) with the FORGE FAKE wired — a real
// forge is outside the demo, and is said so in the record. The loop's
// instruments are the daemon's own shape: a real git repository with a local
// bare origin (commit and push are real git facts, and the branch probe is
// real git), the forge.Fake in place of the GitHub API (openPR and merge are
// witnessed, never dialed), and the walk's merge probe asking the FAKE the
// post-merge guard's one question (the fake's merge SHA is fabricated and
// never lands in the tree, so the real git probe would call every correct
// merge a phantom — said so in the record).
//
// Rows 1–9, 11–13 drive the CLI's verbs through the daemon's method table —
// the same JSON-RPC surface the CLI calls over the socket (the transport
// itself is pinned by T6's conformance fixtures and T9's parity tests, not
// re-proven here). Row 10 runs the ACTUAL binary, built in-test, with the
// daemon "stopped" — made deterministic by pointing the CLI at a dead
// endpoint path and at the SAME state file the in-process daemon reads, so
// the offline write is the one the daemon's next read folds in.
//
// The walk blueprint is one lane: git.createBranch -> git.commitAndPush ->
// forge.openPR -> a gate -> forge.merge. The explicit createBranch step (the
// spec's "commit -> push -> open PR -> merge" with the branch step the
// action needs) and the gate are the walk's two design additions: the spec's
// revoke-mid-run row requires a loop paused AT A QUESTION, and an
// action-only lane has no question source. The action order is unchanged.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/trigger"
)

const walkBlueprint = `id: autonomy-walk
schemaVersion: 1
version: 1
lanes:
  - id: branch
    step: {kind: code-action, action: git.createBranch, params: {which: task}}
  - id: commit-push
    step: {kind: code-action, action: git.commitAndPush}
  - id: open-pr
    step: {kind: code-action, action: forge.openPR}
  - id: ship
    step: {kind: gate, question: ship}
  - id: merge
    step: {kind: code-action, action: forge.merge}
questions:
  ship:
    prompt: Ship it?
    answers: [yes, no]
`

// walkEnv is the walk's ground: a real store, a real git repository with a
// local bare origin, a root component over it, a real daemon, the schedule
// trigger row, the forge fake, and a CLI connection on the method table.
type walkEnv struct {
	store    *store.Store
	storeDir string // the directory holding state.db — the CLI's AEON_STATE_DIR
	d        *Daemon
	ws       string
	root     store.Component
	fake     *forge.Fake
	conn     string
	probe    *walkMergeProbe
}

// walkMergeProbe is the walk's stand-in for the real post-merge ancestry
// probe. The guard asks the TREE whether the merge SHA is an ancestor of the
// feature branch; the fake fabricates the merge SHA (it never lands in the
// tree), so the real git probe would call every correct merge a phantom.
// The probe asks the FAKE instead — the witness this walk stands on: "did
// you merge this PR, at that SHA, into that base?" The pr pointer is
// late-bound (set after each fire, once the fake has seen the openPR) and is
// read only when merge actually asks.
type walkMergeProbe struct {
	fake *forge.Fake
	pr   *int
}

func (p *walkMergeProbe) IsAncestor(ctx context.Context, _, sha, ref string) (bool, error) {
	if p.pr == nil {
		return false, fmt.Errorf("the walk's merge probe has no PR yet — the openPR never reached the fake")
	}
	pr, err := p.fake.ReadPR(ctx, *p.pr)
	if err != nil {
		return false, err
	}
	return pr.Merged && pr.MergeSHA == sha && pr.BaseRef == ref, nil
}

func walkGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		ee, _ := err.(*exec.ExitError)
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, ee.Stderr)
	}
	return strings.TrimSpace(string(out))
}

func newWalkEnv(t *testing.T) *walkEnv {
	t.Helper()
	storeDir := t.TempDir()
	s, err := store.Open(store.Options{Dir: storeDir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// The loop's repository: a real git repo with a local bare origin, so
	// commit and push are real git facts and the branch probes are real.
	ws := t.TempDir()
	origin := t.TempDir() + "/origin.git"
	walkGit(t, ws, "init", "-b", "main")
	walkGit(t, ws, "config", "user.email", "walk@example.com")
	walkGit(t, ws, "config", "user.name", "Walk")
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("# walk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	walkGit(t, ws, "add", ".")
	walkGit(t, ws, "commit", "-m", "initial")
	if err := os.MkdirAll(filepath.Dir(origin), 0o755); err != nil {
		t.Fatal(err)
	}
	walkGit(t, ws, "clone", "--bare", ws, origin)
	walkGit(t, ws, "remote", "add", "origin", origin)
	walkGit(t, ws, "push", "-u", "origin", "main")

	cw, err := s.CreateComponent(store.NewComponent{Name: "walk root", Slug: "walk-root", Root: ws})
	if err != nil || !cw.OK() {
		t.Fatalf("create root component: %v %s", err, cw.Reason)
	}
	fake := forge.NewFake()
	d := New(Options{
		Store:            s,
		Effects:          walkEffects(s),
		ValidateProposal: walkValidateProposal(s),
	})

	// The schedule trigger row on the walk blueprint (the clock's fire walks
	// the same StartFromTrigger this harness fires).
	if err := s.SaveTriggerSchedule(context.Background(), store.TriggerScheduleRow{
		ID: "walk-trg", Kind: "schedule", Blueprint: "autonomy-walk",
		Expr: "30 3 * * *", Zone: "UTC", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("save trigger: %v", err)
	}
	if _, err := d.Table.SaveBlueprint(context.Background(), protocol.Actor{Kind: "user", ID: "demo-walk"}, protocol.BlueprintSaveParams{Name: "autonomy-walk", Content: walkBlueprint}); err != nil {
		t.Fatalf("save blueprint: %v", err)
	}
	// S48 T3: fire time reads rows (the save seam moves to rows at T6) —
	// the walk's blueprint must exist as a row to fire. Mechanical move.
	bpSeedRows(t, s, map[string]string{"autonomy-walk": walkBlueprint})

	conn := "walk-cli"
	init, _ := json.Marshal(protocol.InitializeParams{
		ClientName: "aeon-cli", ClientVersion: "1.0", ProtocolVersion: protocol.PROTOCOL_VERSION,
	})
	if _, err := d.Table.Dispatch(context.Background(), conn, protocol.MethodInitialize, init); err != nil {
		t.Fatalf("initialize the CLI connection: %v", err)
	}

	w := &walkEnv{store: s, storeDir: storeDir, d: d, ws: ws, root: cw.Component, fake: fake, conn: conn, probe: &walkMergeProbe{fake: fake}}
	t.Cleanup(func() { w.installRunner(t, walkPhase{budget: walkBudgetMet}) }) // a final settle with no phase gates
	return w
}

// walkPhase is the current seam stubs: Gates per phase (the row-7 triad),
// Budget always met (S55's budget is a separate slice — the walk's outward
// grant carries the default requireBudget, and a nil seam is cannot-say,
// which would make every grant inert and prove nothing about autonomy).
type walkPhase struct {
	gates  func(ctx context.Context, loopID string, pr int) autonomy.Answer
	budget func(ctx context.Context, loopID string) autonomy.Answer
}

// installRunner swaps the daemon's runner for one whose instruments are the
// walk's: real git, the forge fake, and the current phase's seams. This is
// the seam the automation tests use (automation_test.go) — the daemon's
// production wiring builds the real forge from the repository origin, and a
// fake forge cannot ride the real binary; that is said in the record. Each
// fire of the walk installs the runner it runs under, BEFORE the fire.
func (w *walkEnv) installRunner(t *testing.T, phase walkPhase) {
	t.Helper()
	w.d.loopRunner = loop.NewRunner(loop.Deps{
		Store:   w.store,
		Records: recordsReaderFor(w.store),
		Event:   func(string, any) {},
		Actions: func(root string) *action.Services {
			// The walk asserts what the POLICY says, so the git client has to
			// work. A client with no environment now REFUSES rather than
			// inheriting the test process's (S59 T5, EV10), and the walk read
			// that refusal as the policy's sentence — the block reason was my
			// git sentence instead of `git.commitAndPush denied — effect class
			// 'outward' resolves to deny`. Identity variables are named here so
			// the walk never reads an operator's ~/.gitconfig.
			gitC := git.NewClient([]string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + os.Getenv("HOME"),
				"GIT_AUTHOR_NAME=s59-fixture", "GIT_AUTHOR_EMAIL=s59@aeon.test",
				"GIT_COMMITTER_NAME=s59-fixture", "GIT_COMMITTER_EMAIL=s59@aeon.test",
				"GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C",
			})
			return &action.Services{
				Git: &action.GitActions{Git: gitC},
				Audit: func(context.Context) (int, error) {
					return w.store.CountActionIntents()
				},
				Forge: &action.ForgeActions{
					Forge:   w.fake,
					Git:     gitC, // BranchProbe — the remote branch is origin's truth
					Merge:   w.probe,
					Intents: storeIntents{s: w.store},
				},
			}
		},
		Model: func(context.Context, loop.LoopState, blueprint.Step, string, string) (loop.ModelResult, error) {
			t.Error("a model step was reached in the action-only walk blueprint")
			return loop.ModelResult{}, fmt.Errorf("no model in the walk")
		},
		Autonomy: loop.AutonomySeams{Seams: autonomy.Seams{
			Gates:  phase.gates,
			Budget: phase.budget,
			Uses: func(ctx context.Context, grantID string, since time.Time) (int, error) {
				return w.store.CountAutonomyUsesSince(grantID, since)
			},
		}},
	})
	w.d.loopRunner.SetPark(func(string) {})
}

// gatesMet is the walk's S57 stand-in for the row where the merge rule
// passes.
func gatesMet(ctx context.Context, loopID string, pr int) autonomy.Answer {
	return autonomy.AnswerMet("the walk's gates stub says the merge rule passed")
}

// fire is one clock tick on the walk trigger. The work file is written
// BEFORE the fire: untracked files survive createBranch's checkout, so each
// fire's commitAndPush commits its own content, and no two fires share a
// slug's minute (the loop's slug is the trigger id plus the UTC minute).
func (w *walkEnv) fire(t *testing.T, name string, instant time.Time) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.ws, name+".txt"), []byte("work "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := w.d.StartFromTrigger(context.Background(), trigger.Fire{
		TriggerID: "walk-trg", Blueprint: "autonomy-walk", Workspace: w.ws,
		Provenance: trigger.ProvenanceTrigger, Instant: instant, At: instant,
		With: map[string]string{
			"task":           "walk",
			"task_title":     "walk " + name,
			"commit_message": "walk " + name + ": the work file",
			"pr_summary":     "the walk " + name + " work file",
		},
	})
	if err != nil {
		t.Fatalf("trigger fire: %v", err)
	}
	awaitSettled(t, w.store, id)
	return id
}

// bindPR points the walk's merge probe at the PR the fires have opened and
// returns its number. The fake numbers its PRs from 1 and the walk is its
// only client, so the Nth openPR is PR #N. Late-bound: the probe reads the
// pointer only when merge actually asks.
func (w *walkEnv) bindPR(t *testing.T) int {
	t.Helper()
	n := 0
	for _, c := range w.fake.Calls() {
		if c.Method == "OpenPR" {
			n++
		}
	}
	if n == 0 {
		t.Fatalf("the fake saw no OpenPR — the merge probe has nothing to point at")
	}
	w.probe.pr = &n
	return n
}

// answerShip answers the lane's one gate yes and lets the loop settle.
func answerShip(t *testing.T, w *walkEnv, loopID string) {
	t.Helper()
	if _, err := w.d.Table.Dispatch(context.Background(), w.conn, protocol.MethodLoopAnswer,
		json.RawMessage(`{"id":"`+loopID+`","answer":"yes"}`)); err != nil {
		t.Fatalf("answer the ship gate of %s: %v", loopID, err)
	}
	awaitSettled(t, w.store, loopID)
}

// loopState reads the loop's row (status) and its runtime state (last
// reason, question) together — the status is the loop row's, the reason the
// runtime's.
type walkLoopState struct {
	store.Loop
	Runtime loop.RuntimeState
}

func (w *walkEnv) loopState(t *testing.T, loopID string) walkLoopState {
	t.Helper()
	pin, err := w.store.ReadPinnedLoop(loopID)
	if err != nil {
		t.Fatalf("read pinned loop %s: %v", loopID, err)
	}
	var st loop.RuntimeState
	if err := json.Unmarshal([]byte(pin.RuntimeJSON), &st); err != nil {
		t.Fatalf("read runtime state of %s: %v", loopID, err)
	}
	return walkLoopState{Loop: pin.Loop, Runtime: st}
}

// method drives one CLI verb through the daemon's method table.
func (w *walkEnv) method(t *testing.T, name protocol.MethodName, params any) any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s params: %v", name, err)
	}
	res, err := w.d.Table.Dispatch(context.Background(), w.conn, name, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// methodErr is method for the refusal rows: the refusal IS the expected
// answer, so the error is handed back for the walk to read instead of
// failing the run.
func (w *walkEnv) methodErr(t *testing.T, name protocol.MethodName, params any) error {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s params: %v", name, err)
	}
	_, err = w.d.Table.Dispatch(context.Background(), w.conn, name, raw)
	return err
}

// autonomyRefused unwraps the socket's typed refusal — the
// ErrAutonomyRefused family the write doors speak — and fails the walk when
// the error is not one: a transport failure wearing the refusal's clothes is
// a different bug.
func autonomyRefused(t *testing.T, err error) *protocol.Error {
	t.Helper()
	var perr *protocol.Error
	if err == nil {
		t.Fatalf("the door answered success where a typed refusal was expected")
	}
	if !errors.As(err, &perr) {
		t.Fatalf("the refusal is not a typed *protocol.Error: %v", err)
	}
	return perr
}

func walkRow(t *testing.T, n int, desc string) {
	t.Helper()
	t.Logf("ROW %d — %s", n, desc)
}

// — the walk's proposal-decision wiring (row 11) —
//
// The daemon's commit door needs the effect registry (Options.Effects) and
// the shared create validator (Options.ValidateProposal). In the real
// binary cmd/aeon owns both (deskSeams over its own store handle); the walk
// mirrors the SAME three halves over ITS store — not openStoreDirectly,
// which would point at the operator's state directory, and a test never
// points at the real store. There is no third autonomy kind: a proposal
// that creates or widens an allow is not a shape the desk knows.

func walkValidateAutonomyRevoke(s *store.Store) func(context.Context, []byte) error {
	return func(_ context.Context, payload []byte) error {
		if err := desk.ValidateAutonomyRevoke(context.Background(), payload); err != nil {
			return err
		}
		var p desk.AutonomyRevokePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		g, err := s.ReadAutonomyGrant(p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("refused: %s is not an autonomy grant — it cannot be revoked", p.ID)
			}
			return err
		}
		if g.State == "deny" {
			return fmt.Errorf("refused: %s is a veto — a veto is removed by a person, not proposed away", p.ID)
		}
		return nil
	}
}

func walkAlreadyAppliedAutonomyRevoke(s *store.Store) func(context.Context, []byte) (bool, error) {
	return func(_ context.Context, payload []byte) (bool, error) {
		var p desk.AutonomyRevokePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return false, err
		}
		g, err := s.ReadAutonomyGrant(p.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return g.RevokedAt != nil, nil
	}
}

func walkApplyAutonomyRevoke(s *store.Store) func(context.Context, io.Writer, []byte) error {
	return func(ctx context.Context, _ io.Writer, payload []byte) error {
		dec, ok := desk.DeciderFrom(ctx)
		if !ok {
			return fmt.Errorf("refused: autonomy_revoke's apply needs the committer's identity from the commit door — a context without it is not a commit, and a write with a guessed actor is the audit gap (S56 T8)")
		}
		var p desk.AutonomyRevokePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		_, _, ref, err := s.RevokeAutonomyGrant(store.Actor{Kind: dec.Kind, ID: dec.ID}, p.ID, p.Reason)
		if err != nil {
			return err
		}
		if ref != nil {
			return errors.New(ref.Sentence)
		}
		return nil
	}
}

func walkEffects(s *store.Store) func() (*desk.Registry, error) {
	return func() (*desk.Registry, error) {
		return desk.New(desk.Deps{}, desk.Seams{
			desk.KindAutonomyRevoke: {
				Validate:       walkValidateAutonomyRevoke(s),
				AlreadyApplied: walkAlreadyAppliedAutonomyRevoke(s),
				Apply:          walkApplyAutonomyRevoke(s),
			},
		}), nil
	}
}

func walkValidateProposal(s *store.Store) func(context.Context, string, string) error {
	return func(ctx context.Context, kind, payload string) error {
		if kind == string(desk.KindAutonomyRevoke) {
			return walkValidateAutonomyRevoke(s)(ctx, []byte(payload))
		}
		return fmt.Errorf("refused: the walk wires only the autonomy_revoke create validator — kind %q is out of the walk's scope", kind)
	}
}

// walkBudgetMet is the walk's S55 stand-in: met, always (see walkPhase).
func walkBudgetMet(ctx context.Context, loopID string) autonomy.Answer {
	return autonomy.AnswerMet("the walk's budget stub answers met (S55 is a separate slice)")
}

// outwardActionSubjects is the outward class expanded to its action names —
// the pin's one entry per action. A fresh map per call: row 4 consumes its
// map by marking entries, and row 9 must not see those marks.
func outwardActionSubjects() map[string]bool {
	return map[string]bool{
		"forge.closeIssue": true, "forge.openIssue": true, "forge.openPR": true,
		"forge.postReview": true, "git.commitAndPush": true,
		// S60 T5 (2026-11-04): the merge queue's publish is the sixth outward
		// name. The list stays CLOSED on purpose — the day the registry grows
		// an outward action, this line is a conscious decision, never a drift.
		"git.publishIntegration": true,
		// S60 T6 (same day): the landing PR is the seventh — an outward
		// OpenPR the class grant covers; forge.land is NOT here because it
		// is irreversible, and the class grant never reaches that class
		// (the merge grant is its own subject, S56).
		"forge.openLandingPR": true,
	}
}

func strP(s string) *string { return &s }
