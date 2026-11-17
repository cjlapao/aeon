package store

import (
	"reflect"
	"testing"
)

// mission_read_test.go — S58 T6 (SV11): `TestMissionLoopCountsMatchLoopTable`
// — the rollup must equal a DIRECT query on the loop table, and "a root
// includes its children" must be the RootScopeSQL path rule doing the work,
// not a wish. The last-activity read is pinned beside it: the newest touch,
// and the honest empty string when a tree has never been touched.

func TestMissionLoopCountsMatchLoopTable(t *testing.T) {
	s := newStore(t)
	root, err := s.CreateComponent(NewComponent{Name: "Alpha", Slug: "alpha", Root: "/w/alpha"})
	if err != nil {
		t.Fatalf("root component: %v", err)
	}
	if !root.OK() {
		t.Fatalf("root component refused: %s %s", root.Refusal, root.Reason)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Beta", Slug: "beta", Root: "/w/alpha/beta", ParentID: root.Component.ID})
	if err != nil {
		t.Fatalf("child component: %v", err)
	}
	if !child.OK() {
		t.Fatalf("child component refused: %s %s", child.Refusal, child.Reason)
	}
	if root.Component.ID == child.Component.ID {
		t.Fatal("test setup: parent and child share an id")
	}
	loops := []struct {
		slug, componentRoot, status string
	}{
		{"run-root", "/w/alpha", "running"},
		{"wait-root", "/w/alpha", "waiting"},
		{"block-child", "/w/alpha/beta", "blocked"},
		{"run-child", "/w/alpha/beta", "running"},
		{"done-root", "/w/alpha", "done"}, // terminal: the table holds it, mission does not count it
	}
	for _, l := range loops {
		if _, err := s.CreateLoop(NewLoop{Slug: l.slug, ComponentRoot: l.componentRoot, Goal: "a goal"}); err != nil {
			t.Fatalf("create loop %s: %v", l.slug, err)
		}
		if err := s.SetLoopStatus(Scope{LoopSlug: l.slug, ComponentRoot: l.componentRoot}, l.status); err != nil {
			t.Fatalf("status %s: %v", l.slug, err)
		}
	}
	// One loop OUTSIDE the tree, so the scope filter is doing real work.
	if g, err := s.CreateComponent(NewComponent{Name: "Gamma", Slug: "gamma", Root: "/w/gamma"}); err != nil || !g.OK() {
		t.Fatalf("other component: %v", err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "elsewhere", ComponentRoot: "/w/gamma", Goal: "a goal"}); err != nil {
		t.Fatalf("create other loop: %v", err)
	}
	if err := s.SetLoopStatus(Scope{LoopSlug: "elsewhere", ComponentRoot: "/w/gamma"}, "blocked"); err != nil {
		t.Fatalf("status elsewhere: %v", err)
	}

	got, err := s.LoopStatusCountsForScope([]string{"/w/alpha"})
	if err != nil {
		t.Fatalf("LoopStatusCountsForScope: %v", err)
	}
	// THE equality: exactly what a direct GROUP BY on the same scope answers.
	rows, err := s.r.Query(`SELECT status, COUNT(*) FROM loop WHERE component_root = ? OR component_root LIKE ? GROUP BY status`,
		"/w/alpha", "/w/alpha/%")
	if err != nil {
		t.Fatalf("direct query: %v", err)
	}
	defer rows.Close()
	want := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		want[status] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollup %v != direct query %v", got, want)
	}
	// And the expected shape in words, so a drift in RootScopeSQL cannot
	// hide behind both sides drifting together: children counted, the other
	// tree NOT counted, the terminal row carried as the table holds it.
	wantShape := map[string]int{"running": 2, "waiting": 1, "blocked": 1, "done": 1}
	if !reflect.DeepEqual(got, wantShape) {
		t.Fatalf("rollup = %v, want %v", got, wantShape)
	}

	last, err := s.LastActivityForScope([]string{"/w/alpha"})
	if err != nil {
		t.Fatalf("LastActivityForScope: %v", err)
	}
	if last == "" {
		t.Fatal("last activity empty while five loops carry updated_at")
	}
	empty, err := s.LastActivityForScope([]string{"/w/nothing-here"})
	if err != nil {
		t.Fatalf("LastActivityForScope (empty scope): %v", err)
	}
	if empty != "" {
		t.Fatalf("untouched scope answered %q, want the honest empty string", empty)
	}
}
