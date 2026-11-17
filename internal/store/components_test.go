package store

import "testing"

// sp is a pointer helper for the inheritable override fields.
func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

func TestCreateComponentAppearsBeforeAnyLoopAndRejectsDuplicateRoot(t *testing.T) {
	s := openTemp(t)
	w, err := s.CreateComponent(NewComponent{Name: "Aeon QA", Slug: "aeon-qa", Root: "/repo",
		Remote: "https://example.com/aeon.git", RemoteKey: "example/aeon", RepoKey: "aeon",
		Owner: "team", Tags: []string{"go", "qa"}})
	if err != nil {
		t.Fatal(err)
	}
	c := w.Component
	listed, err := s.ListComponents(ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != c.ID || listed[0].Root != "/repo" ||
		listed[0].Remote != c.Remote || listed[0].RemoteKey != c.RemoteKey ||
		listed[0].RepoKey != c.RepoKey || listed[0].Owner != c.Owner ||
		len(listed[0].Tags) != 2 {
		t.Fatalf("component metadata did not round trip: %+v", listed)
	}
	// A duplicate root path is a refusal (root-taken), not an error.
	if dup, err := s.CreateComponent(NewComponent{Name: "Duplicate", Slug: "duplicate", Root: "/repo"}); err != nil {
		t.Fatal(err)
	} else if dup.OK() {
		t.Fatal("the same root created a second component")
	}
	listed, err = s.ListComponents(ComponentFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("duplicate attempt changed stored components: %+v, %v", listed, err)
	}
}

func TestListComponentsReadsComponentWithoutTags(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateComponent(NewComponent{Name: "Older component", Slug: "older-component", Root: "/older-workspace"}); err != nil {
		t.Fatal(err)
	}
	components, err := s.ListComponents(ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || components[0].Root != "/older-workspace" || len(components[0].Tags) != 0 {
		t.Fatalf("component not listed: %+v", components)
	}
}

func TestUpdateComponentKeepsRootAndChangesMetadata(t *testing.T) {
	s := openTemp(t)
	created, err := s.CreateComponent(NewComponent{Name: "First", Slug: "first", Root: "/repo", Tags: []string{"old"}})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateComponent(ComponentEdit{ID: created.Component.ID, Name: "Second", Slug: "second", Owner: "team", Icon: "🚀", Tags: []string{"new"}})
	if err != nil {
		t.Fatal(err)
	}
	u := updated.Component
	if u.ID != created.Component.ID || u.Root != created.Component.Root || u.CreatedAt != created.Component.CreatedAt ||
		u.Name != "Second" || u.Slug != "second" || u.Icon != "🚀" ||
		u.Owner != "team" || len(u.Tags) != 1 || u.Tags[0] != "new" {
		t.Fatalf("unexpected updated component: %+v", u)
	}
	if _, err := s.UpdateComponent(ComponentEdit{ID: "missing", Name: "Missing", Slug: "missing"}); err == nil {
		t.Fatal("unknown component was accepted")
	}
	listed, err := s.ListComponents(ComponentFilter{})
	if err != nil || len(listed) != 1 || listed[0].Icon != "🚀" {
		t.Fatalf("updated component not listed: %+v, %v", listed, err)
	}
}

// — CT1: a component made with no parent is a live root, not an orphan.
// The old "implicit project" tier is gone; the tree's root IS the answer.
func TestCreateComponentIsARootWhenNotUnderAParent(t *testing.T) {
	s := openTemp(t)
	w, err := s.CreateComponent(NewComponent{Name: "Solo", Slug: "solo", Root: "/solo"})
	if err != nil {
		t.Fatal(err)
	}
	c := w.Component
	if c.ParentID != "" || c.Kind != "component" {
		t.Fatalf("a component made with no parent must be a live root (kind=component), not an orphan: %+v", c)
	}
}

// TestCreateComponentUnderAnExistingRoot: naming a root files the component
// under it, not a fresh one; and naming a root that does not exist is a
// refusal with a reason, not a silent fallback.
func TestCreateComponentUnderAnExistingRoot(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Gateway", Slug: "gateway", Root: "/gw", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !child.OK() || child.Component.ParentID != root.Component.ID {
		t.Fatalf("child filed under %q, not the named root %q", child.Component.ParentID, root.Component.ID)
	}
	// A root with a child reads as a project with one child.
	again, err := s.ReadComponent(root.Component.ID)
	if err != nil || again.Kind != "project" || again.ChildCount != 1 {
		t.Fatalf("a root with a child must read as a project: %+v %v", again, err)
	}
	// The root's children list answers to the child.
	members, err := s.ListChildren(root.Component.ID)
	if err != nil || len(members) != 1 || members[0].ID != child.Component.ID {
		t.Fatalf("the root's child must answer to the child list: %+v %v", members, err)
	}
	// A child under a non-existent root is refused (parent-missing).
	if w2, err := s.CreateComponent(NewComponent{Name: "Nowhere", Slug: "nowhere", Root: "/nw", ParentID: "no-such-root"}); err != nil {
		t.Fatal(err)
	} else if w2.OK() {
		t.Fatal("a component was created under a root that does not exist")
	}
}

// TestMovingAComponentKeepsItsLoops is CT1's load-bearing move: a component's
// loop scope is its ROOT, so moving it under a new parent must leave the loop
// exactly where it was.
func TestMovingAComponentKeepsItsLoops(t *testing.T) {
	s := openTemp(t)
	comp, err := s.CreateComponent(NewComponent{Name: "Engine", Slug: "engine", Root: "/engine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", Title: "Walk", Goal: "a goal", Root: "/engine",
		ComponentRoot: "/engine", Mode: "standard", Phase: "build", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	// A new root that becomes the parent (a "project" once it gains a child).
	parent, err := s.CreateComponent(NewComponent{Name: "Product", Slug: "product", Root: "/product"})
	if err != nil {
		t.Fatal(err)
	}
	// Move "engine" under "product": the loop rides through.
	if w, err := s.AttachComponent(comp.Component.ID, parent.Component.ID); err != nil || !w.OK() {
		t.Fatalf("attach: %+v %v", w, err)
	}
	loops, err := s.ListLoops(ComponentScope{ComponentRoot: "/engine"})
	if err != nil || len(loops) != 1 || loops[0].Slug != "walk" {
		t.Fatalf("the loop did not ride through the move: %+v %v", loops, err)
	}
	eng, err := s.ReadComponent(comp.Component.ID)
	if err != nil || eng.ParentID != parent.Component.ID {
		t.Fatalf("the component did not move: %+v %v", eng, err)
	}
}

// TestSameSlugTwoComponentsNoCrossRead is I4's mechanism under the new name:
// a slug chosen twice, in two component roots, stays two worlds.
func TestSameSlugTwoComponentsNoCrossRead(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateComponent(NewComponent{Name: "A", Slug: "a", Root: "/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(NewComponent{Name: "B", Slug: "b", Root: "/b"}); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/a", "/b"} {
		if _, err := s.CreateLoop(NewLoop{Slug: "refactor", Title: "R in " + root, Goal: "g", Root: root,
			ComponentRoot: root, Mode: "standard", Phase: "build", Status: "active"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{"/a", "/b"} {
		loops, err := s.ListLoops(ComponentScope{ComponentRoot: root})
		if err != nil || len(loops) != 1 {
			t.Fatalf("%s sees %d loops: %v", root, len(loops), err)
		}
		if loops[0].Title != "R in "+root {
			t.Fatalf("cross-component read: %s returned %q", root, loops[0].Title)
		}
	}
}

// ── New CT-tree tests (T4) ──────────────────────────────────────────────────

// TestCreateChildUnderRoot: a child under a root makes the root a project
// (derived, never stored) and the child a component.
func TestCreateChildUnderRoot(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Gateway", Slug: "gateway", Root: "/gw", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !child.OK() || child.Component.ParentID != root.Component.ID {
		t.Fatalf("child under root: %+v", child)
	}
	again, err := s.ReadComponent(root.Component.ID)
	if err != nil || again.Kind != "project" || again.ChildCount != 1 {
		t.Fatalf("root with a child must read as a project with one child: %+v %v", again, err)
	}
	if child.Component.Kind != "component" {
		t.Fatalf("a child must read as a component, not a project: %+v", child.Component)
	}
}

// TestCreateRefusesGrandchild: the tree is at most two levels — a child of a
// child is refused (parent-is-a-child).
func TestCreateRefusesGrandchild(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Gateway", Slug: "gateway", Root: "/gw", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	gc, err := s.CreateComponent(NewComponent{Name: "Grandchild", Slug: "grandchild", Root: "/gc", ParentID: child.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if gc.OK() {
		t.Fatal("a grandchild was accepted — the tree is at most two levels")
	}
	if gc.Refusal != ComponentRefusalParentIsChild {
		t.Fatalf("grandchild refused as %q, want parent-is-a-child", gc.Refusal)
	}
}

// TestCreateChildNeedsRoot: a child needs a workspace root; a repo-less node
// is made only by promote, so a child with no root is refused (root-taken).
func TestCreateChildNeedsRoot(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateComponent(NewComponent{Name: "NoRoot", Slug: "noroot", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if w.OK() {
		t.Fatal("a child with no workspace root was accepted")
	}
	if w.Refusal != ComponentRefusalRootTaken {
		t.Fatalf("no-root child refused as %q, want root-taken", w.Refusal)
	}
}

// TestPromoteKeepsIdLoopsAndRollups: promote makes a new root and makes the
// node its child — the node keeps its id, roots, loops (CT4/CT7).
func TestPromoteKeepsIdLoopsAndRollups(t *testing.T) {
	s := openTemp(t)
	comp, err := s.CreateComponent(NewComponent{Name: "Engine", Slug: "engine", Root: "/engine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLoop(NewLoop{Slug: "walk", Title: "Walk", Goal: "a goal", Root: "/engine",
		ComponentRoot: "/engine", Mode: "standard", Phase: "build", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	w, err := s.PromoteComponent(comp.Component.ID, PromoteInput{Name: "Engine project"})
	if err != nil || !w.OK() {
		t.Fatalf("promote: %+v %v", w, err)
	}
	eng, err := s.ReadComponent(comp.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eng.ParentID == "" {
		t.Fatal("promote did not make the node a child of a new root")
	}
	loops, err := s.ListLoops(ComponentScope{ComponentRoot: "/engine"})
	if err != nil || len(loops) != 1 || loops[0].Slug != "walk" {
		t.Fatalf("the loop did not ride through the promote: %+v %v", loops, err)
	}
}

// TestAttachDetachRoundTripIsExact: attaching then detaching a childless root
// leaves the tree exactly as it started (CT4).
func TestAttachDetachRoundTripIsExact(t *testing.T) {
	s := openTemp(t)
	parent, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := s.CreateComponent(NewComponent{Name: "Leaf", Slug: "leaf", Root: "/leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if w, err := s.AttachComponent(leaf.Component.ID, parent.Component.ID); err != nil || !w.OK() {
		t.Fatalf("attach: %+v %v", w, err)
	}
	if w, err := s.DetachComponent(leaf.Component.ID); err != nil || !w.OK() {
		t.Fatalf("detach: %+v %v", w, err)
	}
	leaf2, err := s.ReadComponent(leaf.Component.ID)
	if err != nil || leaf2.ParentID != "" {
		t.Fatalf("after detach the leaf is not a root: %+v %v", leaf2, err)
	}
	par, err := s.ReadComponent(parent.Component.ID)
	if err != nil || par.ChildCount != 0 || par.Kind != "component" {
		t.Fatalf("after the round trip the parent is not back to a childless root: %+v %v", par, err)
	}
}

// TestDetachMaterializesInheritedValues: detaching a child materializes its
// inherited settings as own (CT6) — the value is unchanged, the source is not.
func TestDetachMaterializesInheritedValues(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform", SizingMode: sp("manual")})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Leaf", Slug: "leaf", Root: "/leaf", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	eff, err := s.EffectiveComponent(child.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eff.SizingMode.Source != "root" || eff.SizingMode.Value != "manual" {
		t.Fatalf("a child should inherit the root's sizing mode: %+v", eff.SizingMode)
	}
	if w, err := s.DetachComponent(child.Component.ID); err != nil || !w.OK() {
		t.Fatalf("detach: %+v %v", w, err)
	}
	eff2, err := s.EffectiveComponent(child.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if eff2.SizingMode.Source != "own" || eff2.SizingMode.Value != "manual" {
		t.Fatalf("detach should materialize the inherited value as own: %+v", eff2.SizingMode)
	}
}

// TestDetachRefusesToEmptyAPureProject: detaching the only child of a pure
// (repo-less) project would leave it empty — refused (empty-parent, CT3).
func TestDetachRefusesToEmptyAPureProject(t *testing.T) {
	s := openTemp(t)
	leaf, err := s.CreateComponent(NewComponent{Name: "Leaf", Slug: "leaf", Root: "/leaf"})
	if err != nil {
		t.Fatal(err)
	}
	if w, err := s.PromoteComponent(leaf.Component.ID, PromoteInput{Name: "Pure"}); err != nil || !w.OK() {
		t.Fatalf("promote: %+v %v", w, err)
	}
	w, err := s.DetachComponent(leaf.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.OK() {
		t.Fatal("detaching the only child of a pure project was accepted")
	}
	if w.Refusal != ComponentRefusalEmptyParent {
		t.Fatalf("refused as %q, want empty-parent", w.Refusal)
	}
}

// TestArchiveRefusesWithChildren: archiving a node with non-archived children
// is refused (has-children, CT4).
func TestArchiveRefusesWithChildren(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(NewComponent{Name: "Child", Slug: "child", Root: "/child", ParentID: root.Component.ID}); err != nil {
		t.Fatal(err)
	}
	w, err := s.ArchiveComponent(root.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.OK() {
		t.Fatal("archiving a node with children was accepted")
	}
	if w.Refusal != ComponentRefusalHasChildren {
		t.Fatalf("refused as %q, want has-children", w.Refusal)
	}
}

// TestRestoreNeedsParentRestored: a child whose parent is still archived
// cannot be restored (parent-archived, CT4).
func TestRestoreNeedsParentRestored(t *testing.T) {
	s := openTemp(t)
	root, err := s.CreateComponent(NewComponent{Name: "Platform", Slug: "platform", Root: "/platform"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateComponent(NewComponent{Name: "Child", Slug: "child", Root: "/child", ParentID: root.Component.ID})
	if err != nil {
		t.Fatal(err)
	}
	if w, err := s.ArchiveComponent(child.Component.ID); err != nil || !w.OK() {
		t.Fatalf("archive child: %+v %v", w, err)
	}
	if w, err := s.ArchiveComponent(root.Component.ID); err != nil || !w.OK() {
		t.Fatalf("archive root: %+v %v", w, err)
	}
	w, err := s.RestoreComponent(child.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.OK() {
		t.Fatal("restoring a child whose parent is archived was accepted")
	}
	if w.Refusal != ComponentRefusalParentArchivedRestore {
		t.Fatalf("refused as %q, want parent-archived", w.Refusal)
	}
}

// TestArchivedComponentIsHiddenByDefault: archived nodes leave the default
// listing, and reappear only with IncludeArchived (CT4).
func TestArchivedComponentIsHiddenByDefault(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateComponent(NewComponent{Name: "Live", Slug: "live", Root: "/live"}); err != nil {
		t.Fatal(err)
	}
	arch, err := s.CreateComponent(NewComponent{Name: "Gone", Slug: "gone", Root: "/gone"})
	if err != nil {
		t.Fatal(err)
	}
	if w, err := s.ArchiveComponent(arch.Component.ID); err != nil || !w.OK() {
		t.Fatalf("archive: %+v %v", w, err)
	}
	listed, err := s.ListComponents(ComponentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listed {
		if c.ID == arch.Component.ID {
			t.Fatalf("an archived component appeared in the default listing: %+v", c)
		}
	}
	all, err := s.ListComponents(ComponentFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range all {
		if c.ID == arch.Component.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the archived component is missing from the include-archived listing")
	}
}

// TestEmptyProjectReported: a pure (repo-less) project is Empty only when it
// has no children; with a child it is a real project, not empty.
func TestEmptyProjectReported(t *testing.T) {
	s := openTemp(t)
	leaf, err := s.CreateComponent(NewComponent{Name: "Leaf", Slug: "leaf", Root: "/leaf"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.PromoteComponent(leaf.Component.ID, PromoteInput{Name: "Pure"})
	if err != nil {
		t.Fatal(err)
	}
	if !w.OK() {
		t.Fatalf("promote refused: %s (%s)", w.Refusal, w.Reason)
	}
	// The pure project is the new root; the promoted leaf is its child.
	pure, err := s.ReadComponent(w.Component.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	if pure.Empty {
		t.Fatalf("a pure project with a child must not be empty: %+v", pure)
	}
	comp, err := s.ReadComponent(leaf.Component.ID)
	if err != nil {
		t.Fatal(err)
	}
	if comp.ParentID != pure.ID {
		t.Fatalf("the promoted node must be a child of the pure project: %+v", comp)
	}
}
