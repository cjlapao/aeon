package store

import (
	"testing"

	"gorm.io/gorm"
)

// attention_read_test.go — S58 T2: the three reads the attention sources
// stand on, each checked against the predicate it claims to share and the
// scope rule it claims to honour (SV1).

// mustExec runs one seed statement, failing the test on any error.
func mustExec(t *testing.T, g *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := g.Exec(sql, args...).Error; err != nil {
		t.Fatalf("seed %q: %v", sql, err)
	}
}

// seedAttentionLoop plants a loop row with FIXED timestamps (the reads under
// test report `updated_at`; a fixture clock that moves makes the assertion
// mean nothing).
func seedAttentionLoop(t *testing.T, g *gorm.DB, id, slug, root, status, at string) {
	t.Helper()
	mustExec(t, g, `INSERT INTO loop (
		id, slug, title, goal, root, component_root, mode, phase, status,
		started_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, slug, slug, "a goal", root, root, "serial", "build", status, at, at)
}

// seedAttentionQuestion plants one question, answered or not.
func seedAttentionQuestion(t *testing.T, g *gorm.DB, id int64, slug, root, askedAt string, answered bool) {
	t.Helper()
	var answeredAt any
	if answered {
		answeredAt = askedAt
	}
	mustExec(t, g, `INSERT INTO question (id, loop_slug, component_root, kind, body, options, asked_at, answered_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		id, slug, root, "clarify", "which way?", `["a","b"]`, askedAt, answeredAt)
}

func TestAttentionReadsScopeByRootTree(t *testing.T) {
	g, s := testStore(t)
	mustExec(t, g, `INSERT INTO component (id, name, slug, created_at, updated_at) VALUES
		('cmp-a','Alpha','alpha','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
		('cmp-b','Beta','beta','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`)
	mustExec(t, g, `UPDATE component SET parent_id='cmp-a' WHERE id='cmp-b'`)
	mustExec(t, g, `INSERT INTO component_root (component_id, component_root, is_primary, added_at) VALUES
		('cmp-a','/w/a',1,'2026-11-01T00:00:00Z'),
		('cmp-b','/w/a/b',1,'2026-11-01T00:00:00Z')`)
	mustExec(t, g, `INSERT INTO component_root (component_id, component_root, is_primary, added_at) VALUES
		('cmp-a','/w/a-alt',0,'2026-11-01T00:00:00Z')`)
	seedAttentionLoop(t, g, "lp-a", "root-a", "/w/a", "running", "2026-11-02T10:00:00Z")
	seedAttentionLoop(t, g, "lp-b", "child-b", "/w/a/b", "running", "2026-11-02T10:00:00Z")
	seedAttentionLoop(t, g, "lp-c", "other-c", "/w/c", "running", "2026-11-02T10:00:00Z")
	seedAttentionQuestion(t, g, 11, "root-a", "/w/a", "2026-11-02T09:00:00Z", false)
	seedAttentionQuestion(t, g, 12, "child-b", "/w/a/b", "2026-11-02T09:30:00Z", false)
	seedAttentionQuestion(t, g, 13, "other-c", "/w/c", "2026-11-02T09:40:00Z", false)
	seedAttentionQuestion(t, g, 14, "root-a", "/w/a", "2026-11-02T08:00:00Z", true)

	for _, tc := range []struct {
		name  string
		roots []string
		want  []int64 // question ids in scope
	}{
		{"root sees its own tree", []string{"/w/a"}, []int64{11, 12}},
		{"child scopes to the child", []string{"/w/a/b"}, []int64{12}},
		{"every when empty", nil, []int64{11, 12, 13}},
		// A component's SECONDARY path rides in the resolver's own output
		// (it feeds every attached path), and a lone non-primary path
		// selects only ITS subtree — /w/a-alt has no rows of its own.
		{"secondary path is honest about its own subtree", []string{"/w/a-alt"}, nil},
		{"resolver feeds every attached path", []string{"/w/a", "/w/a-alt"}, []int64{11, 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.PendingQuestionsForScope(tc.roots)
			if err != nil {
				t.Fatalf("pending questions: %v", err)
			}
			var ids []int64
			var seen = map[int64]bool{}
			for _, q := range got {
				ids = append(ids, q.QuestionID)
				seen[q.QuestionID] = true
			}
			// An ANSWERED question (14) is never pending (answered_at IS
			// NULL is the whole predicate, shared with store.awaiting).
			if seen[14] {
				t.Fatalf("answered question 14 is pending")
			}
			if len(ids) != len(tc.want) {
				t.Fatalf("scope %v answered %v, want %v", tc.roots, ids, tc.want)
			}
			for _, w := range tc.want {
				if !seen[w] {
					t.Fatalf("scope %v answered %v, missing %d", tc.roots, ids, w)
				}
			}
		})
	}
}

func TestAttentionReadsMatchStoreAwaiting(t *testing.T) {
	g, s := testStore(t)
	seedAttentionLoop(t, g, "lp-a", "a", "/w/a", "running", "2026-11-02T10:00:00Z")
	seedAttentionLoop(t, g, "lp-c", "c", "/w/c", "running", "2026-11-02T10:00:00Z")
	seedAttentionQuestion(t, g, 21, "a", "/w/a", "2026-11-02T09:00:00Z", false)
	seedAttentionQuestion(t, g, 22, "c", "/w/c", "2026-11-02T09:00:00Z", false)
	seedAttentionQuestion(t, g, 23, "a", "/w/a", "2026-11-02T09:10:00Z", true)

	awaiting, err := s.AwaitingLoops(ComponentScope{})
	if err != nil {
		t.Fatalf("awaiting: %v", err)
	}
	want := map[string]bool{}
	for _, sc := range awaiting {
		want[sc.LoopSlug] = true
	}
	if !want["a"] || !want["c"] {
		t.Fatalf("fixture: awaiting=%v, want a and c", awaiting)
	}
	pending, err := s.PendingQuestionsForScope(nil)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	got := map[string]bool{}
	for _, q := range pending {
		got[q.LoopSlug] = true
	}
	if len(got) != len(want) {
		t.Fatalf("the two truths disagree: awaiting=%v pending=%v", awaiting, pending)
	}
	for slug := range want {
		if !got[slug] {
			t.Fatalf("slug %s awaits per store.awaiting but not per the questions read", slug)
		}
	}
}

func TestAttentionBlockedLoopsCarryStoredReason(t *testing.T) {
	g, s := testStore(t)
	seedAttentionLoop(t, g, "lp-blk", "blk", "/w/a", "blocked", "2026-11-02T10:00:00Z")
	mustExec(t, g, `INSERT INTO loop_blueprint (loop_id, blueprint_id, blueprint_version, content, updated_at, runtime_json) VALUES ('lp-blk', 'none', 1, '', '2026-11-02T10:00:00Z', '{"lastReason":"unattended loop denied an outward action"}')`)
	seedAttentionLoop(t, g, "lp-ok", "ok", "/w/a", "running", "2026-11-02T10:00:00Z")
	if _, err := s.AppendEvent(NewEvent{LoopSlug: "blk", ComponentRoot: "/w/a", Kind: "loop.blocked",
		Payload: map[string]any{"loopId": "lp-blk", "reason": map[string]any{
			"kind": "action-denied", "detail": "unattended loop denied an outward action",
			"action": "git push"}}}); err != nil {
		t.Fatalf("append loop.blocked: %v", err)
	}

	loops, err := s.BlockedLoopsForScope(nil)
	if err != nil {
		t.Fatalf("blocked loops: %v", err)
	}
	if len(loops) != 1 || loops[0].LoopID != "lp-blk" {
		t.Fatalf("blocked read answered %+v, want only lp-blk", loops)
	}
	if loops[0].LastReason != "unattended loop denied an outward action" {
		t.Fatalf("lastReason = %q, want the stored sentence verbatim", loops[0].LastReason)
	}

	reasons, err := s.LatestBlockedReasons(nil)
	if err != nil {
		t.Fatalf("latest blocked reasons: %v", err)
	}
	r, ok := reasons["lp-blk"]
	if !ok || r.Kind != "action-denied" {
		t.Fatalf("reason kind = %+v, want action-denied read from the durable event", r)
	}
	if r.At == "" {
		t.Fatalf("reason carries no event instant: %+v", r)
	}
}

func TestAttentionStoppedLoops(t *testing.T) {
	g, s := testStore(t)
	seedAttentionLoop(t, g, "lp-stop", "stop", "/w/a", "waiting", "2026-11-02T10:00:00Z")
	mustExec(t, g, `INSERT INTO loop_blueprint (loop_id, blueprint_id, blueprint_version, content, updated_at, runtime_json) VALUES ('lp-stop', 'none', 1, '', '2026-11-02T10:00:00Z', '{"lastReason":"stopped"}')`)
	mustExec(t, g, `INSERT INTO step (loop_slug, component_root, taskid, phase, agent, attempt,
		status, started_at, ended_by, source, cost_state, pause_reason)
		VALUES ('stop','/w/a',NULL,'build','golang-engineer',1,'suspended','2026-11-02T10:00:00Z','open','live','unknown','user')`)
	// A waiting loop whose reason is NOT `stopped` is not a person-stop.
	seedAttentionLoop(t, g, "lp-wait", "wait", "/w/a", "waiting", "2026-11-02T10:00:00Z")
	mustExec(t, g, `INSERT INTO loop_blueprint (loop_id, blueprint_id, blueprint_version, content, updated_at, runtime_json) VALUES ('lp-wait', 'none', 1, '', '2026-11-02T10:00:00Z', '{"lastReason":"gate"}')`)
	// A stopped loop WAITING ON A QUESTION is the questions source's, not
	// this one's (the conjunction the read owns).
	seedAttentionLoop(t, g, "lp-stopq", "stopq", "/w/a", "waiting", "2026-11-02T10:00:00Z")
	mustExec(t, g, `INSERT INTO loop_blueprint (loop_id, blueprint_id, blueprint_version, content, updated_at, runtime_json) VALUES ('lp-stopq', 'none', 1, '', '2026-11-02T10:00:00Z', '{"lastReason":"stopped"}')`)
	seedAttentionQuestion(t, g, 31, "stopq", "/w/a", "2026-11-02T09:59:00Z", false)

	got, err := s.StoppedLoopsForScope(nil)
	if err != nil {
		t.Fatalf("stopped loops: %v", err)
	}
	if len(got) != 1 || got[0].LoopID != "lp-stop" {
		t.Fatalf("stopped read answered %+v, want only lp-stop", got)
	}
	if got[0].StepID <= 0 {
		t.Fatalf("stopped item lost the suspended step: %+v", got[0])
	}
}

func TestAttentionLiveComponentTree(t *testing.T) {
	g, s := testStore(t)
	mustExec(t, g, `INSERT INTO component (id, name, slug, created_at, updated_at) VALUES
		('cmp-a','Alpha','alpha','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
		('cmp-b','Beta','beta','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
		('cmp-arc','Gone','gone','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`)
	mustExec(t, g, `UPDATE component SET parent_id='cmp-a' WHERE id='cmp-b'`)
	mustExec(t, g, `UPDATE component SET archived_at='2026-11-02T00:00:00Z' WHERE id='cmp-arc'`)
	mustExec(t, g, `INSERT INTO component_root (component_id, component_root, is_primary, added_at) VALUES
		('cmp-a','/w/a',1,'2026-11-01T00:00:00Z'),
		('cmp-b','/w/a/b',1,'2026-11-01T00:00:00Z'),
		('cmp-arc','/w/arc',1,'2026-11-01T00:00:00Z')`)

	comps, err := s.LiveComponentTree()
	if err != nil {
		t.Fatalf("live tree: %v", err)
	}
	if len(comps) != 2 {
		t.Fatalf("live tree = %+v, want the two live components (archived is out)", comps)
	}
	for _, c := range comps {
		if c.ID == "cmp-arc" {
			t.Fatalf("archived component in the live tree")
		}
		if c.ID == "cmp-b" && (c.ParentID != "cmp-a" || len(c.Paths) != 1 || c.Paths[0] != "/w/a/b") {
			t.Fatalf("child shaped wrong: %+v", c)
		}
	}
}

// The three later reads: a gate's LATEST run decides (an older failure a rerun fixed is not shown), a
// proposal is open only while `proposed`, and bugs are counted per component, only while unvalidated.
func TestAttentionGatesProposalsAndBugsReads(t *testing.T) {
	g, s := testStore(t)
	attempt := 0
	run := func(id, root, slug, state, at string) {
		attempt++ // the (loop, root, position, attempt) slot is unique; each run is its own attempt
		mustExec(t, g, `INSERT INTO verify_run (id, loop_slug, component_root, component_id, task, attempt, trigger, actor_kind, actor_id, started_at, state)
			VALUES (?,?,?,?,?,?,'loop','system','daemon',?,?)`, id, slug, root, "cmp", "t1", attempt, at, state)
	}
	run("r1", "/w/a", "s1", "fail", "2026-11-02T01:00:00Z") // failed, then fixed by r2: not shown
	run("r2", "/w/a", "s1", "pass", "2026-11-02T02:00:00Z")
	run("r3", "/w/a", "s2", "fail", "2026-11-02T03:00:00Z") // latest of its slot and failed: shown
	run("r4", "/w/b", "s3", "fail", "2026-11-02T04:00:00Z") // another root

	all, err := s.FailedGatesForScope(nil)
	if err != nil || len(all) != 2 || all[0].RunID != "r3" || all[1].RunID != "r4" {
		t.Fatalf("failed gates = %+v, %v", all, err)
	}
	scoped, _ := s.FailedGatesForScope([]string{"/w/a"})
	if len(scoped) != 1 || scoped[0].RunID != "r3" {
		t.Fatalf("scoped failed gates = %+v", scoped)
	}

	open, err := s.CreateProposal("policy_add", `{}`, Actor{Kind: "agent", ID: "eng"}, "2026-11-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProposal("policy_add", `{}`, Actor{Kind: "agent", ID: "eng"}, "2026-11-02T00:30:00Z"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, g, `UPDATE proposal SET status='rejected' WHERE id <> ?`, open.ID)
	props, err := s.OpenProposals()
	if err != nil || len(props) != 1 || props[0].ID != open.ID || props[0].CreatedByID != "eng" {
		t.Fatalf("open proposals = %+v, %v", props, err)
	}

	mustExec(t, g, `INSERT INTO component (id, name, slug, created_at, updated_at) VALUES
		('cmp-a','Alpha','alpha','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z'),
		('cmp-b','Beta','beta','2026-11-01T00:00:00Z','2026-11-01T00:00:00Z')`)
	mustExec(t, g, `INSERT INTO component_root (component_id, component_root, is_primary, added_at) VALUES
		('cmp-a','/w/a',1,'2026-11-01T00:00:00Z'), ('cmp-b','/w/b',1,'2026-11-01T00:00:00Z')`)
	for _, title := range []string{"b1", "b2"} {
		if _, err := s.CreateBug(NewBugRow{ComponentRoot: "/w/a", Title: title, Body: "x", FiledBy: "person"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateBug(NewBugRow{ComponentRoot: "/w/b", Title: "b3", Body: "x", FiledBy: "person"}); err != nil {
		t.Fatal(err)
	}
	mustExec(t, g, `UPDATE bug SET validation_state='reproduced' WHERE title='b2'`)
	bugs, err := s.UnvalidatedBugsForScope(nil)
	if err != nil || len(bugs) != 2 {
		t.Fatalf("bug counts = %+v, %v", bugs, err)
	}
	counts := map[string]int{}
	for _, b := range bugs {
		counts[b.ComponentRoot] = b.Count
	}
	if counts["/w/a"] != 1 || counts["/w/b"] != 1 {
		t.Fatalf("counts = %v (b2 was validated, so /w/a has one left)", counts)
	}
}
