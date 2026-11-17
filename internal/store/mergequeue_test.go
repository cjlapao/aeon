package store

// mergequeue_test.go — S60 T2: the queue's rows are physical, the refusals
// are sentences, and the history is append-only by trigger. The schema
// identity, the rebuild guards, and the raw-SQL invariants below are the
// table in docs/aeon/S60-merge-queue/tasks/T2.md; the deviating test NAME
// (ListMergeQueueEvents, not the task's ListMergeEvents) exists because S33
// already owns the bare name in this package (merge_event.go).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func mqActor() MergeActor { return MergeActor{Kind: "user", ID: "t2"} }

func mqEnqueue(t *testing.T, s *Store, root, base, branch string) MergeItem {
	t.Helper()
	it, err := s.EnqueueMergeItem(NewMergeItem{
		ComponentRoot: root, BaseBranch: base, Branch: branch,
		HeadSHA: "head-" + branch, BaseSHA: "base-" + base,
		EnqueuedByKind: "user", EnqueuedByID: "t2",
	}, mqActor())
	if err != nil {
		t.Fatalf("enqueue %s: %v", branch, err)
	}
	return it
}

func mqRefusal(t *testing.T, err error, kind string) MergeRefusal {
	t.Helper()
	if err == nil {
		t.Fatalf("wanted a %s refusal, got success", kind)
	}
	var r MergeRefusal
	if !errors.As(err, &r) {
		t.Fatalf("wanted MergeRefusal(%s), got %T: %v", kind, err, err)
	}
	if r.Kind != kind {
		t.Fatalf("refusal kind = %q (%s), want %q", r.Kind, r.Reason, kind)
	}
	return r
}

// TestMergeQueueSchemaFreshEqualsUpgraded: a fresh store and one upgraded
// from the previous vintage (everything BEFORE the two S60 files) hold the
// same queue shape, and the upgrade carries the ledger rows.
func TestMergeQueueSchemaFreshEqualsUpgraded(t *testing.T) {
	// fresh
	fs := openTemp(t)
	seedRoot(t, fs, "/repo")
	for _, tb := range []string{"merge_queue_item", "merge_queue_event", "merge_queue_config"} {
		if countTable(t, fs.gw, tb) != 0 {
			t.Fatalf("fresh %s not empty", tb)
		}
	}
	mqEnqueue(t, fs, "/repo", "main", "feat-a")

	// upgraded from the PREVIOUS vintage — LITERAL 87 (45 frozen + 42 files,
	// the count before the two S60 files; the automation_schema_test lesson:
	// the vintage is a literal, never len(Migrations())-1).
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	func() {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		applyMigrations(t, g, Migrations()[:87])
		if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
			VALUES ('l1','mq-keep','t','g','/repo','/repo','standard','preflight','running','2026-10-27T00:00:00.000Z','2026-10-27T00:00:00.000Z')`).Error; err != nil {
			t.Fatalf("seed loop at the prior vintage: %v", err)
		}
		if err := g.Exec(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at)
			VALUES ('/repo','mq-keep','lane','a','/repo/.worktrees/a','aeon/mq-keep/a','sha1','2026-10-27T00:00:00.000Z')`).Error; err != nil {
			t.Fatalf("seed a live worktree row at the prior vintage: %v", err)
		}
		removed := "2026-10-28T00:00:00.000Z"
		if err := g.Exec(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at, removed_at)
			VALUES ('/repo','mq-keep','task','b','/repo/.worktrees/b','aeon/mq-keep/b','sha2','2026-10-27T00:00:00.000Z',?)`, removed).Error; err != nil {
			t.Fatalf("seed a removed worktree row at the prior vintage: %v", err)
		}
	}()
	us, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("open the prior-vintage store: %v", err)
	}
	defer us.Close()
	for _, tb := range []string{"merge_queue_item", "merge_queue_event", "merge_queue_config"} {
		if countTable(t, us.gw, tb) != 0 {
			t.Fatalf("upgraded %s not empty", tb)
		}
	}
	if n := countTable(t, us.gw, "loop_worktree"); n != 2 {
		t.Fatalf("the rebuild kept %d worktree rows, want 2 (live AND removed)", n)
	}
	// The widened CHECK is LIVE after the upgrade, not merely present.
	if err := us.gw.Exec(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at)
		VALUES ('/repo','mq-keep','integration','mq-x','/repo/.worktrees/i','integrate/mq-x','sha3','2026-10-27T00:00:00.000Z')`).Error; err != nil {
		t.Fatalf("the upgraded CHECK refuses 'integration': %v", err)
	}
	seedRoot(t, us, "/repo")
	mqEnqueue(t, us, "/repo", "main", "feat-b") // the queue works on an upgraded store too
}

// TestOneSlotPerBaseBranchRawSQL: the partial unique index is the physical
// one-slot; a second slot-state row for one (root, base) aborts, a different
// base is independent, and `conflicted` holds no slot (OD2).
func TestOneSlotPerBaseBranchRawSQL(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	seedRoot(t, s, "/other")
	ins := func(root, base, branch, status string) error {
		return s.gw.Exec(`INSERT INTO merge_queue_item (id, component_root, base_branch, branch, head_sha, base_sha, status, position, created_at, updated_at, enqueued_by_kind, enqueued_by_id)
			VALUES (?, ?, ?, ?, 'h', 'b', ?, 1000, '2026-11-04T00:00:00.000Z','2026-11-04T00:00:00.000Z','user','t2')`,
			"x-"+root+"-"+branch, root, base, branch, status).Error
	}
	if err := ins("/repo", "main", "a", "ready"); err != nil {
		t.Fatalf("first slot holder: %v", err)
	}
	err := ins("/repo", "main", "b", "catching_up")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("second slot holder for one base: err = %v, want the one-slot UNIQUE abort", err)
	}
	if err := ins("/repo", "dev", "b", "ready"); err != nil {
		t.Fatalf("a different base is independent: %v", err)
	}
	if err := ins("/repo", "main", "c", "conflicted"); err != nil {
		t.Fatalf("conflicted releases the slot (OD2): %v", err)
	}
	if err := ins("/other", "main", "a", "merging"); err != nil {
		t.Fatalf("a different repository is independent: %v", err)
	}
}

// TestOneOpenItemPerBranchRawSQL: one live item per branch; a terminal row
// never blocks re-enqueueing the same branch.
func TestOneOpenItemPerBranchRawSQL(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	ins := func(id, branch, status string) error {
		return s.gw.Exec(`INSERT INTO merge_queue_item (id, component_root, base_branch, branch, head_sha, base_sha, status, position, created_at, updated_at, enqueued_by_kind, enqueued_by_id)
			VALUES (?, '/repo', 'main', ?, 'h', 'b', ?, 1000, '2026-11-04T00:00:00.000Z','2026-11-04T00:00:00.000Z','user','t2')`,
			id, branch, status).Error
	}
	if err := ins("m1", "feat", "queued"); err != nil {
		t.Fatal(err)
	}
	err := ins("m2", "feat", "queued")
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate open branch: err = %v, want the one-open UNIQUE abort", err)
	}
	if err := ins("m3", "feat", "merged"); err != nil {
		t.Fatalf("a merged row must not block: %v", err)
	}
	if err := ins("m4", "feat", "failed"); err != nil {
		t.Fatalf("a failed row must not block: %v", err)
	}
	if err := ins("m5", "feat", "conflicted"); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("conflicted IS open (it holds the branch): err = %v, want the UNIQUE abort", err)
	}
}

// TestEventTableIsAppendOnly: the migration's triggers abort UPDATE and
// DELETE with their own sentence.
func TestEventTableIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	it := mqEnqueue(t, s, "/repo", "main", "feat-a")
	evs, err := s.ListMergeQueueEvents(it.ID)
	if err != nil || len(evs) != 1 {
		t.Fatalf("events after enqueue = %d (%v), want the single queued birth", len(evs), err)
	}
	if err := s.gw.Exec(`UPDATE merge_queue_event SET detail = '{}' WHERE id = ?`, evs[0].ID).Error; err == nil ||
		!strings.Contains(err.Error(), "merge_queue_event is append-only") {
		t.Fatalf("UPDATE: err = %v, want the trigger sentence", err)
	}
	if err := s.gw.Exec(`DELETE FROM merge_queue_event WHERE id = ?`, evs[0].ID).Error; err == nil ||
		!strings.Contains(err.Error(), "merge_queue_event is append-only") {
		t.Fatalf("DELETE: err = %v, want the trigger sentence", err)
	}
}

// TestTransitionIsGuarded: the wrong guard is a stale-state refusal naming
// the current status; two concurrent transitions of the same item leave
// exactly one winner (the guarded UPDATE, not an overwrite).
func TestTransitionIsGuarded(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	it := mqEnqueue(t, s, "/repo", "main", "feat-a")

	_, err := s.TransitionMergeItem(it.ID, "ready", "merging", TransitionFacts{}, mqActor())
	r := mqRefusal(t, err, "stale-state")
	if r.Current != "queued" {
		t.Fatalf("stale-state Current = %q, want the truth (queued)", r.Current)
	}

	// Two goroutines, same guard: exactly one lands. The slot itself admits
	// only one — but both aim at the SAME item, so the winner is the guard.
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.TransitionMergeItem(it.ID, "queued", "catching_up", TransitionFacts{}, mqActor())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent guarded transitions: %d winners, want exactly 1", wins)
	}
	one := 0
	if items, err := s.ListMergeItems(MergeFilter{ComponentRoot: "/repo", Statuses: MergeSlotStatusSet}); err == nil {
		one = len(items)
	}
	if one != 1 {
		t.Fatalf("slot-state items = %d, want the winner only", one)
	}
	// And exactly one 'queued→catching_up' event — the loser wrote nothing.
	evs, _ := s.ListMergeQueueEvents(it.ID)
	n := 0
	for _, e := range evs {
		if e.ToStatus == "catching_up" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("catching_up events = %d, want 1 (a refused write writes no history)", n)
	}
}

// TestSlotTakenNamesTheHolder: a SECOND item entering a slot state while the
// first holds it is refused with the holder's id — before SQL, with a
// sentence; the partial index is only the backstop.
func TestSlotTakenNamesTheHolder(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	a := mqEnqueue(t, s, "/repo", "main", "feat-a")
	b := mqEnqueue(t, s, "/repo", "main", "feat-b")
	if _, err := s.TransitionMergeItem(a.ID, "queued", "verifying", TransitionFacts{}, mqActor()); err != nil {
		t.Fatalf("first into the slot: %v", err)
	}
	_, err := s.TransitionMergeItem(b.ID, "queued", "verifying", TransitionFacts{}, mqActor())
	r := mqRefusal(t, err, "slot-taken")
	if r.Holder != a.ID {
		t.Fatalf("slot-taken Holder = %q, want %q", r.Holder, a.ID)
	}
}

// TestEveryChangingWriteWritesOneEvent counts history against writes: the
// birth, one per transition, one per reorder, one per REAL config change —
// and NONE for reads, refusals, or no-ops.
func TestMergeQueueEveryChangingWriteWritesOneEvent(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	it := mqEnqueue(t, s, "/repo", "main", "feat-a")
	second := mqEnqueue(t, s, "/repo", "main", "feat-b")
	evs, _ := s.ListMergeQueueEvents(it.ID)
	if len(evs) != 1 || evs[0].FromStatus != "" || evs[0].ToStatus != "queued" || evs[0].Kind != "transition" {
		t.Fatalf("birth event = %+v, want transition (NULL → queued)", evs[0])
	}

	// Reads write nothing.
	if _, err := s.ReadMergeItem(it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListMergeItems(MergeFilter{ComponentRoot: "/repo"}); err != nil {
		t.Fatal(err)
	}
	// Refused writes write nothing.
	_, _ = s.TransitionMergeItem(it.ID, "merged", "queued", TransitionFacts{}, mqActor())
	_, _ = s.EnqueueMergeItem(NewMergeItem{ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-a",
		HeadSHA: "h", BaseSHA: "b", EnqueuedByKind: "user", EnqueuedByID: "t2"}, mqActor()) // duplicate-open
	// A no-op field patch writes nothing.
	if _, err := s.SetMergeItemFields(it.ID, "queued", TransitionFacts{}, mqActor()); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	// A no-op reorder writes nothing (moving the tail item "to the tail").
	if _, err := s.ReorderMergeItems(second.ID, "", mqActor()); err != nil {
		t.Fatalf("tail-to-tail reorder: %v", err)
	}
	// A config write is a change; the identical one again is not.
	if _, err := s.WriteMergeConfig(NewMergeConfig{ComponentRoot: "/repo", Mode: "auto", ConflictPolicy: "resolve", LandMethod: "squash", MaxCatchUps: 4}, mqActor()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteMergeConfig(NewMergeConfig{ComponentRoot: "/repo", Mode: "auto", ConflictPolicy: "resolve", LandMethod: "squash", MaxCatchUps: 4}, mqActor()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReorderMergeItems(second.ID, it.ID, mqActor()); err != nil {
		t.Fatalf("reorder before: %v", err)
	}
	if _, err := s.TransitionMergeItem(it.ID, "queued", "ready", TransitionFacts{}, mqActor()); err != nil {
		t.Fatal(err)
	}

	evs, _ = s.ListMergeQueueEvents(it.ID)
	if len(evs) != 2 { // birth + transition on THIS item (the reorder moved the other; refusals and no-ops wrote nothing)
		t.Fatalf("events on the first item = %d, want 2: %+v", len(evs), evs)
	}
	evs2, _ := s.ListMergeQueueEvents(second.ID)
	if len(evs2) != 2 { // birth + the real reorder (the tail-to-tail call was a no-op)
		t.Fatalf("events on the second item = %d, want 2: %+v", len(evs2), evs2)
	}
	if evs2[1].Kind != "reorder" || !strings.Contains(evs2[1].Detail, `"before_id":"`+it.ID+`"`) {
		t.Fatalf("the reorder event = %+v, want kind reorder naming the before id", evs2[1])
	}
	var cfgEvents int
	s.gw.Raw(`SELECT COUNT(*) FROM merge_queue_event WHERE kind = 'config' AND item_id IS NULL`).Scan(&cfgEvents)
	if cfgEvents != 1 {
		t.Fatalf("config events = %d, want exactly 1 (the identical second write is a no-op)", cfgEvents)
	}
}

// TestEventDetailCarriesNoFreeText (SEC-033): hunk-shaped prose cannot enter
// the event detail — a conflict path with control characters is refused
// outright, and the built detail is only the closed keys.
func TestEventDetailCarriesNoFreeText(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	it := mqEnqueue(t, s, "/repo", "main", "feat-a")

	_, err := s.TransitionMergeItem(it.ID, "queued", "conflicted", TransitionFacts{
		Reason:        "conflict",
		ConflictPaths: []string{"src/main.go\n<<<<<<< HEAD\nsecret hunk body"},
	}, mqActor())
	mqRefusal(t, err, "detail-text")

	// A clean path enters; nothing else about the item (branch, slug) can
	// ride into detail.
	if _, err := s.TransitionMergeItem(it.ID, "queued", "conflicted", TransitionFacts{
		Reason:        "conflict",
		ConflictPaths: []string{"src/main.go", "docs/plan.md"},
	}, mqActor()); err != nil {
		t.Fatalf("clean conflict paths: %v", err)
	}
	evs, _ := s.ListMergeQueueEvents(it.ID)
	d := evs[len(evs)-1].Detail
	if !strings.Contains(d, `"conflict_paths":["src/main.go","docs/plan.md"]`) || !strings.Contains(d, `"reason_kind":"conflict"`) {
		t.Fatalf("detail = %s, want the closed keys only", d)
	}
	if strings.Contains(d, "feat-a") || strings.Contains(d, "hunk") {
		t.Fatalf("detail leaked a branch or prose: %s", d)
	}

	// >100 paths is refused; each path ≤ 512 bytes.
	wide := make([]string, 101)
	for i := range wide {
		wide[i] = fmt.Sprintf("f%03d.go", i)
	}
	_, err = s.TransitionMergeItem(it.ID, "conflicted", "queued", TransitionFacts{ConflictPaths: wide}, mqActor())
	mqRefusal(t, err, "detail-text")
	long := make([]string, 1)
	long[0] = strings.Repeat("a", 513)
	if err := validateConflictPaths(long); err == nil {
		t.Fatal("a 513-byte path sailed through the SEC-033 gate")
	}
}

// TestReorderStableAndGuarded: positions move inside the gaps, only QUEUED
// items move, and the list order follows.
func TestReorderStableAndGuarded(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	a := mqEnqueue(t, s, "/repo", "main", "a")
	b := mqEnqueue(t, s, "/repo", "main", "b")
	c := mqEnqueue(t, s, "/repo", "main", "c")
	if a.Position != 1000 || b.Position != 2000 || c.Position != 3000 {
		t.Fatalf("enqueue positions = %d/%d/%d, want 1000/2000/3000 (gaps by design)", a.Position, b.Position, c.Position)
	}
	moved, err := s.ReorderMergeItems(c.ID, b.ID, mqActor()) // c in front of b
	if err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if moved.Position != 1999 {
		t.Fatalf("c after reorder = %d, want 1999 (one gap in front of b)", moved.Position)
	}
	list, _ := s.ListMergeItems(MergeFilter{ComponentRoot: "/repo", Statuses: []string{"queued"}})
	if len(list) != 3 || list[0].ID != a.ID || list[1].ID != c.ID || list[2].ID != b.ID {
		t.Fatalf("queue order after reorder = %v", orderIDs(list))
	}
	// A non-queued item refuses to move.
	if _, err := s.TransitionMergeItem(a.ID, "queued", "ready", TransitionFacts{}, mqActor()); err != nil {
		t.Fatal(err)
	}
	_, err = s.ReorderMergeItems(a.ID, c.ID, mqActor())
	mqRefusal(t, err, "not-queued")
	// Moving to the tail keeps it behind everything.
	tails, err := s.ReorderMergeItems(c.ID, "", mqActor())
	if err != nil {
		t.Fatalf("to tail: %v", err)
	}
	if tails.Position <= b.Position {
		t.Fatalf("tail position %d, want past %d", tails.Position, b.Position)
	}
}

// TestIntegrationWorktreeOwnerAccepted: the Go door and the CHECK both admit
// 'integration' after the rebuild; a fifth value still fails (the CHECK, via
// raw SQL, since the Go door refuses before it).
func TestIntegrationWorktreeOwnerAccepted(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	if _, err := s.CreateComponent(NewComponent{Name: "keep", Slug: "mqkeep", Root: "/repo"}); err != nil {
		t.Fatal(err)
	}
	l, err := s.CreateLoop(NewLoop{Slug: "mq-keep", Goal: "g", Root: "/repo", ComponentRoot: "/repo", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := s.CreateLoopWorktree(LoopWorktree{ComponentRoot: "/repo", LoopSlug: l.Slug, OwnerKind: "integration", OwnerID: "mq-x", Path: "/repo/.worktrees/i", Branch: "integrate/mq-x", BaseSHA: "sha1"})
	if err != nil {
		t.Fatalf("integration owner through the Go door: %v", err)
	}
	if wt.ID == 0 {
		t.Fatal("no ledger id came back")
	}
	live, err := s.ListLiveLoopWorktrees(Scope{LoopSlug: l.Slug, ComponentRoot: "/repo"})
	if err != nil || len(live) != 1 || live[0].OwnerKind != "integration" {
		t.Fatalf("live ledger = %+v (%v)", live, err)
	}
	err = s.gw.Exec(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at)
		VALUES ('/repo','mq-keep','bogus','z','/tmp/z','b','s','2026-11-04T00:00:00.000Z')`).Error
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("a fifth owner_kind: err = %v, want the CHECK abort", err)
	}
}

// TestLedgerRebuildKeepsEveryRow: live AND removed rows ride the rebuild with
// ids and content intact (the checksum) and the FK stays clean.
func TestLedgerRebuildKeepsEveryRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	var beforeCount int
	var beforeSum int64
	func() {
		g, closeDB := rawGorm(t, path)
		defer closeDB()
		applyMigrations(t, g, Migrations()[:87])
		if err := g.Exec(`INSERT INTO loop (id,slug,title,goal,root,component_root,mode,phase,status,started_at,updated_at)
			VALUES ('l1','rows','t','g','/repo','/repo','standard','preflight','running','2026-10-27T00:00:00.000Z','2026-10-27T00:00:00.000Z')`).Error; err != nil {
			t.Fatal(err)
		}
		for i, owner := range []string{"lane", "task", "lane", "task"} {
			removed := ", NULL)"
			if i%2 == 1 {
				removed = ", '2026-10-29T00:00:00.000Z')"
			}
			q := fmt.Sprintf(`INSERT INTO loop_worktree (component_root, loop_slug, owner_kind, owner_id, path, branch, base_sha, created_at, removed_at)
				VALUES ('/repo','rows','%s','o%d','/repo/.w/%d','aeon/rows/%d','sha%d','2026-10-27T00:00:00.000Z'%s`, owner, i, i, i, i, removed)
			if err := g.Exec(q).Error; err != nil {
				t.Fatalf("plant row %d: %v", i, err)
			}
		}
		var n int
		var sum int64
		if err := g.Raw(`SELECT COUNT(*), COALESCE(SUM(id),0) FROM loop_worktree`).Row().Scan(&n, &sum); err != nil {
			t.Fatal(err)
		}
		beforeCount, beforeSum = n, sum
	}()
	s, err := Open(Options{Dir: dir, Clock: fixedClock(), Env: emptyEnv})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s.Close()
	var n int
	var sum int64
	if err := s.gw.Raw(`SELECT COUNT(*), COALESCE(SUM(id),0) FROM loop_worktree`).Row().Scan(&n, &sum); err != nil {
		t.Fatal(err)
	}
	if n != beforeCount || sum != beforeSum {
		t.Fatalf("after = %d rows / id-sum %d, want %d / %d (the rebuild moved rows, not copies)", n, sum, beforeCount, beforeSum)
	}
	var fk int
	if err := s.gw.Raw(`SELECT COUNT(*) FROM pragma_foreign_key_check()`).Row().Scan(&fk); err != nil || fk != 0 {
		t.Fatalf("foreign_key_check after the rebuild = %d (%v), want empty", fk, err)
	}
}

// TestLedgerRebuildGuardsHaveTeeth: the copy-guard table is not decoration —
// with one row of the copy removed, its CHECK aborts the migration between
// the copy and the swap.
func TestLedgerRebuildGuardsHaveTeeth(t *testing.T) {
	g := openMigrated(t)
	db, err := g.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE gw_src (id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO gw_src VALUES (1,'a'),(2,'b'),(3,'c')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE gw_dst (id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO gw_dst SELECT * FROM gw_src`); err != nil {
		t.Fatal(err)
	}
	// Sabotage exactly as a failed copy would: one row short.
	if _, err := db.Exec(`DELETE FROM gw_dst WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE gw_guard (moved INTEGER NOT NULL, total INTEGER NOT NULL, CHECK (moved = total))`); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO gw_guard SELECT (SELECT COUNT(*) FROM gw_dst), (SELECT COUNT(*) FROM gw_src)`)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("the moved = total guard on a short copy: err = %v, want the CHECK abort", err)
	}
	// And the same table admits the honest copy — the guard is not always-red.
	if _, err := db.Exec(`INSERT INTO gw_dst VALUES (2,'b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gw_guard SELECT (SELECT COUNT(*) FROM gw_dst), (SELECT COUNT(*) FROM gw_src)`); err != nil {
		t.Fatalf("the guard on an honest copy: %v", err)
	}
}

// TestMergeVocabulariesMatchTheMigration is the ProposalKinds lock for the
// queue's six closed sets: the Go sets and the migration DDL say the same
// words, in the same lists.
func TestMergeVocabulariesMatchTheMigration(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("migrations", "20261104044821_merge_queue.sql"))
	if err != nil {
		t.Fatalf("read the migration: %v", err)
	}
	// The DDL wraps some CHECK lists across lines; one space per run of
	// whitespace makes the literal comparison about WORDS, not indentation.
	body := strings.Join(strings.Fields(string(raw)), " ")
	checks := map[string][]string{
		"'queued','catching_up','verifying','ready','merging','merged','conflicted','failed','removed'":                                           MergeStatusSet,
		"'catching_up','verifying','ready','merging'":                                                                                             MergeSlotStatusSet,
		"'queued','catching_up','verifying','ready','merging','conflicted'":                                                                       MergeOpenStatusSet,
		"'denied','conflict','gate-failed','head-moved','protected','forge','git','stale-exhausted','removed-by-person','loop-stopped','unknown'": MergeReasonSet,
		"'transition','reorder','config'":                                                                                                         MergeEventKindSet,
		"'user','cli','blueprint','daemon','proposal'":                                                                                            MergeEnqueuedBySet,
		"'user','cli','blueprint','daemon','agent'":                                                                                               MergeEventActorSet,
	}
	for literal, set := range checks {
		if !strings.Contains(body, "IN ("+literal+")") {
			t.Errorf("the migration DDL does not carry the exact list for %v", set)
		}
		for _, v := range set {
			if !strings.Contains(literal, "'"+v+"'") {
				t.Errorf("%q is in the Go set but not the DDL list", v)
			}
		}
	}
}

// TestQueueRowsOutliveTheLoop: F20 — no FK to loop; deleting the building
// loop leaves the queue row (and its history) standing.
func TestQueueRowsOutliveTheLoop(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	l, err := s.CreateLoop(NewLoop{Slug: "build-loop", Goal: "g", Root: "/repo", ComponentRoot: "/repo", Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.EnqueueMergeItem(NewMergeItem{ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-x",
		LoopSlug: l.Slug, HeadSHA: "h", BaseSHA: "b", EnqueuedByKind: "blueprint", EnqueuedByID: "build-loop"}, mqActor())
	if err != nil {
		t.Fatalf("enqueue with a loop slug: %v", err)
	}
	if err := s.gw.Exec(`DELETE FROM loop WHERE id = ?`, l.ID).Error; err != nil {
		t.Fatalf("delete the building loop: %v", err)
	}
	again, err := s.ReadMergeItem(it.ID)
	if err != nil || again.LoopSlug != "build-loop" {
		t.Fatalf("item after its loop is gone = %+v (%v) — the queue must outlive the loop (F20)", again, err)
	}
}

// TestMergeItemTerminalIsTheReconcileRead (S60 T5, MQ7): the closed terminal
// set is exactly {merged, failed, removed} — queued/catching_up/verifying/
// ready/conflicted are the LIVE half, the worktrees a next landing still
// needs. An ABSENT item answers terminal: its landing row has nothing left
// to keep a directory for (the cleanup read, not a rebuild read) — the one
// direction this function is allowed to be eager in, and named for it.
func TestMergeItemTerminalIsTheReconcileRead(t *testing.T) {
	s := openTemp(t)
	seedRoot(t, s, "/repo")
	it, err := s.EnqueueMergeItem(NewMergeItem{ComponentRoot: "/repo", BaseBranch: "main", Branch: "feat-t",
		HeadSHA: "h", BaseSHA: "b", EnqueuedByKind: "cli", EnqueuedByID: "t"}, mqActor())
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.MergeItemTerminal(it.ID)
	if err != nil || ok {
		t.Fatalf("queued is live: %v (%v)", ok, err)
	}
	for _, live := range []string{"catching_up", "verifying", "ready", "conflicted"} {
		if _, err := s.TransitionMergeItem(it.ID, statusBeforeForTest(live), live, TransitionFacts{}, mqActor()); err != nil {
			// Legality is not this read's business; walk the chain from the
			// current status whatever it is.
			cur := readStatus(t, s, it.ID)
			if _, err := s.TransitionMergeItem(it.ID, cur, live, TransitionFacts{}, mqActor()); err != nil {
				t.Fatalf("walk to %s from %s: %v", live, cur, err)
			}
		}
		if ok, err := s.MergeItemTerminal(it.ID); err != nil || ok {
			t.Fatalf("%s must read live: %v (%v)", live, ok, err)
		}
	}
	cur := readStatus(t, s, it.ID)
	if _, err := s.TransitionMergeItem(it.ID, cur, "merged", TransitionFacts{}, mqActor()); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.MergeItemTerminal(it.ID); err != nil || !ok {
		t.Fatalf("merged is terminal: %v (%v)", ok, err)
	}
	// A removed row is terminal; an absent id is terminal too — the row it
	// belonged to is gone, and the worktree has no reason to stand.
	if err := s.gw.Exec(`UPDATE merge_queue_item SET status = 'removed' WHERE id = ?`, it.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := s.MergeItemTerminal(it.ID); err != nil || !ok {
		t.Fatalf("removed is terminal: %v (%v)", ok, err)
	}
	if ok, err := s.MergeItemTerminal("mq-0000000000000000000000000000ab"); err != nil || !ok {
		t.Fatalf("absent answers terminal (the settle read): %v (%v)", ok, err)
	}
}

func statusBeforeForTest(string) string { return "queued" }

func readStatus(t *testing.T, s *Store, id string) string {
	t.Helper()
	var st string
	if err := s.r.QueryRow(`SELECT status FROM merge_queue_item WHERE id = ?`, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func orderIDs(items []MergeItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}
