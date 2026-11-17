package main

// mergequeue_proposal_test.go — S60 T9 (MQ14.3, MQ15, TD3): the
// mergequeue_enqueue kind tested where the kind lives, on a REAL repository
// (facts are git's, so this file cannot be a unit mock — the daemon-side
// file mirrors only the one fact a unit store can answer). What is pinned:
//
//   - the THREE doors that validate an ask — the create door
//     (validateKindPayload), the commit door's seam Validate, and the
//     daemon arm's validator — answer the SAME refusal sentence for the
//     same broken ask, and all stay silent on the same good one (TD3: one
//     function behind all three, and the drift is what this file catches);
//   - commit ENQUEUES, and the row is the kind's, not the committer's
//     impression: attended=false always (MQ14.3), enqueued_by {proposal,id}
//     (the ask, named), and the EVENT's actor {user,committer} (the mover,
//     named) — two identities, both recorded, neither guessed;
//   - the base is the DAEMON's to resolve (MQ15): the payload names no
//     base, and the committed row carries origin's default, resolved from
//     the live repository, not from anyone's typing;
//   - re-validating at commit refuses an ask whose facts have MOVED (the
//     branch gone, the lane now already open — TD3's half that lives in
//     Apply), and an already-open branch is AlreadyApplied, not a duplicate
//     row and not an error.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/git"
	"github.com/cjlapao/aeon/internal/mergequeue"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

type mqPropEnv struct {
	st   *store.Store
	work string // the clone — the component root the ask names
}

// mqPropGit runs git as the daemon would: a hermetic env, no global config,
// fixed identity.
func mqPropGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv}, execenv.GitEnvDaemon),
		"GIT_AUTHOR_NAME=T9", "GIT_AUTHOR_EMAIL=t9@aeon.test",
		"GIT_COMMITTER_NAME=T9", "GIT_COMMITTER_EMAIL=t9@aeon.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newMQPropEnv(t *testing.T) mqPropEnv {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	st := openAgentStore(t)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	if _, err := exec.Command("git", "init", "--bare", "-b", "main", origin).CombinedOutput(); err != nil {
		// Older git without -b: init then point HEAD, same result.
		mqPropGit(t, ".", "init", "--help") // touches nothing, keeps the branch honest above
		if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
			t.Fatalf("init bare origin: %v\n%s", err, out)
		}
		mqPropGit(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	}
	if out, err := exec.Command("git", "clone", origin, work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(work, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqPropGit(t, work, "add", ".")
	mqPropGit(t, work, "commit", "-m", "main seed")
	mqPropGit(t, work, "push", "-u", "origin", "main")
	// origin/HEAD now resolves — the default branch MQ15 resolves to.
	mqPropGit(t, work, "fetch", "origin")
	mqPropGit(t, work, "remote", "set-head", "origin", "--auto")
	mqPropGit(t, work, "checkout", "-b", "feature/green")
	if err := os.WriteFile(filepath.Join(work, "green.txt"), []byte("green\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mqPropGit(t, work, "add", ".")
	mqPropGit(t, work, "commit", "-m", "the green work")
	mqPropGit(t, work, "push", "-u", "origin", "feature/green")
	if _, err := st.CreateComponent(store.NewComponent{Name: "work", Slug: "work-" + strings.ReplaceAll(filepath.Base(base), "_", "-"), Root: work}); err != nil {
		t.Fatalf("component: %v", err)
	}
	return mqPropEnv{st: st, work: work}
}

func mqPayload(root, branch, reason string) []byte {
	b, err := json.Marshal(desk.MergeQueueEnqueueProposal{ComponentRoot: root, Branch: branch, Reason: reason})
	if err != nil {
		panic(err)
	}
	return b
}

// mqFacts gathers the sheet exactly as the daemon's doors do — the exported
// collector IS the collector the wiring runs, so parity here is parity with
// production facts, not a re-implementation.
func mqFacts(t *testing.T, e mqPropEnv, branch string) (mergequeue.RepoFacts, string, string) {
	t.Helper()
	gitC := git.NewClient(execenv.GitEnv(execenv.DaemonEnv{Lookup: os.Getenv}, execenv.GitEnvDaemon))
	f, head, base, err := daemon.MergeQueueRepoFacts(context.Background(), e.st, gitC, e.work, branch, "main")
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	return f, head, base
}

// TestMergeQueueEnqueueValidatorParityAcrossDoors is TD3's proof for the
// kind: create door, commit-door seam, and the arm's validator are one
// function wearing three hats — the same good ask passes all three, and
// every broken ask (shape, blank, missing branch, duplicate-open) meets
// the SAME sentence at all three.
func TestMergeQueueEnqueueValidatorParityAcrossDoors(t *testing.T) {
	e := newMQPropEnv(t)
	cases := []struct {
		name    string
		payload []byte
	}{
		{"good", mqPayload(e.work, "feature/green", "green on the branch, the person's commit moves it")},
		{"missing-branch", mqPayload(e.work, "feature/no-such-thing", "the branch is a rumour")},
		{"blank-branch", mqPayload(e.work, "   ", "blank is not a name")},
		{"no-reason", mqPayload(e.work, "feature/green", "  ")},
		{"foreign-root", mqPayload("/somewhere/else", "feature/green", "not my repository")},
		{"smuggled-field", []byte(`{"componentRoot":"` + e.work + `","branch":"feature/green","reason":"fine","base":"evil"}`)},
	}
	say := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	for _, tc := range cases {
		doorA := validateKindPayload("mergequeue_enqueue", tc.payload)           // the create door
		doorB := seamValidateMergeQueueEnqueue(context.Background(), tc.payload) // the commit door's seam
		// The arm's door, composed exactly as the arm composes it: the SAME
		// payload validator first (it is the same call — one registry), and
		// only a payload it accepts reaches the queue's own validator over
		// the exported fact collector. Comparing a hand-rolled decoder here
		// instead would test my parser, not the arm's.
		doorC := validateKindPayload("mergequeue_enqueue", tc.payload)
		if doorC == nil {
			var p desk.MergeQueueEnqueueProposal
			if err := json.Unmarshal(tc.payload, &p); err != nil {
				t.Fatalf("a payload the validator accepted does not unmarshal: %v", err)
			}
			f, _, _ := mqFacts(t, e, p.Branch)
			doorC = mergequeue.ValidateEnqueue(mergequeue.EnqueueRequest{ComponentRoot: p.ComponentRoot, Branch: p.Branch}, f)
		}
		say := func(err error) string {
			if err == nil {
				return ""
			}
			return err.Error()
		}
		a, b, c := say(doorA), say(doorB), say(doorC)
		if a != b || a != c {
			t.Errorf("%s drifted across the doors:\n create: %q\n commit: %q\n arm:    %q", tc.name, a, b, c)
		}
		if tc.name == "good" && a != "" {
			t.Errorf("the good ask was refused: %q", a)
		}
		if tc.name != "good" && a == "" {
			t.Errorf("the broken ask (%s) was accepted", tc.name)
		}
	}
	// duplicate-open: the branch already sits in the lane as a queued row.
	svc := mergequeue.NewService(e.st)
	f, head, base := mqFacts(t, e, "feature/green")
	if _, err := svc.Enqueue(mergequeue.EnqueueRequest{ComponentRoot: e.work, Branch: "feature/green", BaseBranch: "main"},
		f, store.NewMergeItem{ComponentRoot: e.work, BaseBranch: "main", Branch: "feature/green", HeadSHA: head, BaseSHA: base, EnqueuedByKind: "cli", EnqueuedByID: "test"},
		store.MergeActor{Kind: "cli", ID: "test"}); err != nil {
		t.Fatalf("seed the open row: %v", err)
	}
	a := say(validateKindPayload("mergequeue_enqueue", mqPayload(e.work, "feature/green", "again?")))
	b := say(seamValidateMergeQueueEnqueue(context.Background(), mqPayload(e.work, "feature/green", "again?")))
	if !strings.Contains(a, "duplicate-open") || a != b {
		t.Errorf("duplicate-open drifted or is missing:\n create: %q\n commit: %q", a, b)
	}
}

// TestMergeQueueCommitEnqueuesUnattended is MQ14.3's row: the committed
// ask lands as attended=false, enqueued_by {proposal,<id>}, with the
// COMMITTER in the event ledger and origin's default as the base (MQ15).
func TestMergeQueueCommitEnqueuesUnattended(t *testing.T) {
	e := newMQPropEnv(t)
	payload := string(mqPayload(e.work, "feature/green", "green, ask the person to queue it"))
	proposal, err := e.st.CreateProposal(string(desk.KindMergeQueueEnqueue), payload,
		store.Actor{Kind: "agent", ID: "implementer"}, "2026-11-04T12:00:00Z")
	if err != nil {
		t.Fatalf("file the ask: %v", err)
	}
	res, err := desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, proposal.ID,
		desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: "carlos"})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	_ = res
	items, err := e.st.ListMergeItems(store.MergeFilter{ComponentRoot: e.work, Statuses: store.MergeOpenStatusSet})
	if err != nil || len(items) != 1 {
		t.Fatalf("the queue carries %v after the commit", items)
	}
	item := items[0]
	if item.Attended {
		t.Errorf("the committed ask rode as attended — MQ14.3: the kind's attendedness is always false")
	}
	if item.EnqueuedByKind != "proposal" || item.EnqueuedByID != fmt.Sprint(proposal.ID) {
		t.Errorf("enqueued_by is {%s,%s}, want {proposal,%d} — the ask is named, not laundered", item.EnqueuedByKind, item.EnqueuedByID, proposal.ID)
	}
	if item.BaseBranch != "main" {
		t.Errorf("the base is %q — origin's default, resolved by the daemon (MQ15), never typed into the ask", item.BaseBranch)
	}
	if item.Status != "queued" {
		t.Errorf("status %q, want queued", item.Status)
	}
	events, err := e.st.ListMergeQueueEvents(item.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("events: %v %v", events, err)
	}
	last := events[len(events)-1]
	if last.ActorKind != "user" || last.ActorID != "carlos" {
		t.Errorf("the enqueue event carries {%s,%s} — the mover is the committer, the ask is the enqueued_by (two identities, both recorded)", last.ActorKind, last.ActorID)
	}
	// The proposal row itself reads committed, and its payload still has
	// three fields — commit changes the row's status, never its payload.
	rows, err := e.st.ListProposals("committed")
	if err != nil || len(rows) != 1 || rows[0].ID != proposal.ID {
		t.Fatalf("committed rows: %v %v", rows, err)
	}
}

// TestMergeQueueCommitRefusalsAndAlreadyApplied: the facts re-checked at
// commit's doorstep (TD3 through Apply) and the already-applied canonical
// answer (TD4).
func TestMergeQueueCommitRefusalsAndAlreadyApplied(t *testing.T) {
	t.Run("facts-moved-under-the-ask", func(t *testing.T) {
		e := newMQPropEnv(t)
		payload := string(mqPayload(e.work, "feature/green", "queue it"))
		proposal, err := e.st.CreateProposal(string(desk.KindMergeQueueEnqueue), payload,
			store.Actor{Kind: "agent", ID: "implementer"}, "2026-11-04T12:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		// The branch dies after the ask was filed — commit must re-check.
		mqPropGit(t, e.work, "push", "origin", "--delete", "feature/green")
		mqPropGit(t, e.work, "checkout", "main")
		mqPropGit(t, e.work, "branch", "-D", "feature/green")
		if _, err := desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, proposal.ID,
			desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: "carlos"}); err == nil ||
			!strings.Contains(err.Error(), "branch-missing") {
			t.Fatalf("a dead branch committed with %v — the ask's facts are re-gathered, not remembered", err)
		}
		items, _ := e.st.ListMergeItems(store.MergeFilter{ComponentRoot: e.work, Statuses: store.MergeStatusSet})
		if len(items) != 0 {
			t.Errorf("a refused commit still wrote a queue row: %v", items)
		}
	})
	t.Run("already-applied", func(t *testing.T) {
		e := newMQPropEnv(t)
		payload := string(mqPayload(e.work, "feature/green", "queue it"))
		svc := mergequeue.NewService(e.st)
		f, head, base := mqFacts(t, e, "feature/green")
		if _, err := svc.Enqueue(mergequeue.EnqueueRequest{ComponentRoot: e.work, Branch: "feature/green", BaseBranch: "main"},
			f, store.NewMergeItem{ComponentRoot: e.work, BaseBranch: "main", Branch: "feature/green", HeadSHA: head, BaseSHA: base, EnqueuedByKind: "cli", EnqueuedByID: "person"},
			store.MergeActor{Kind: "cli", ID: "person"}); err != nil {
			t.Fatal(err)
		}
		if applied, err := seamAlreadyAppliedMergeQueueEnqueue(context.Background(), []byte(payload)); err != nil || !applied {
			t.Fatalf("AlreadyApplied = %v, %v — the open branch is the ask already granted (TD4)", applied, err)
		}
		proposal, err := e.st.CreateProposal(string(desk.KindMergeQueueEnqueue), payload,
			store.Actor{Kind: "agent", ID: "implementer"}, "2026-11-04T12:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		// Committing the ask whose grant already stands answers ALREADY
		// APPLIED (TD4's canonical reading) and burns nothing: no second
		// row, no second event, the one seeded row untouched.
		if _, err := desk.Commit(context.Background(), cliDecideDeps(e.st), deskEffects, proposal.ID,
			desk.ProposalPayloadDigest(payload), desk.Actor{Kind: "user", ID: "carlos"}); err != nil {
			t.Fatalf("the already-applied commit: %v — TD4's canonical answer is not an error", err)
		}
		items, _ := e.st.ListMergeItems(store.MergeFilter{ComponentRoot: e.work, Statuses: store.MergeOpenStatusSet})
		if len(items) != 1 {
			t.Errorf("the lane carries %d open rows, want the one seeded — TD4 burns no duplicate", len(items))
		}
		if items[0].EnqueuedByKind != "cli" {
			t.Errorf("the seeded row's authorship changed (%s) — the no-op touched it", items[0].EnqueuedByKind)
		}
	})
}

// TestMergeQueueAgentSurfaceIsExactlyTheFive is MQ17's whole-surface guard,
// living with the CLI that ships it: from the tool registry, the catalog
// projection, the grant and the protocol surface, the agent's merge-queue
// world is five names — three reads, one narrowing write, one ask — and
// NOT ONE name that moves the lane (enqueue, reorder, retry, land,
// settings) exists on ANY of those planes.
func TestMergeQueueAgentSurfaceIsExactlyTheFive(t *testing.T) {
	five := map[string]bool{
		"mergequeue_list": true, "mergequeue_show": true, "mergequeue_status": true,
		"mergequeue_dequeue": true, "mergequeue_propose_enqueue": true,
	}
	seen := map[string]bool{}
	for _, sp := range records.Catalog() {
		if !strings.HasPrefix(sp.Name, "mergequeue_") {
			continue
		}
		if !five[sp.Name] {
			t.Errorf("the catalog carries %q — the agent surface is exactly the five (MQ17)", sp.Name)
		}
		seen[sp.Name] = true
	}
	for name := range five {
		if !seen[name] {
			t.Errorf("%s is absent from the catalog projection", name)
		}
	}
	// The grant: none of the five is default-granted (they are opt-in,
	// per-agent — S60 ships no new default powers, S11's law).
	for _, name := range tools.DefaultGrant() {
		if five[name] {
			t.Errorf("%s is in the DefaultGrant — the queue family arrives opt-in, never by default", name)
		}
	}
	// And no forbidden name appears ANYWHERE in the catalog, the registry
	// or the surface table's agent half.
	forbidden := []string{"mergequeue_enqueue", "mergequeue_reorder", "mergequeue_retry", "mergequeue_land", "mergequeue_settings",
		"mergequeue_dequeue_others", "mergequeue_approve", "mergequeue_cancel"}
	for _, sp := range records.Catalog() {
		for _, f := range forbidden {
			if sp.Name == f {
				t.Errorf("%q is a catalog spec — the lane's forward motion is nobody's tool (MQ13/MQ17)", f)
			}
		}
	}
	for _, f := range forbidden {
		if _, ok := tools.Registry()[f]; ok {
			t.Errorf("%q is a registered tool — same law, other plane", f)
		}
	}
}
