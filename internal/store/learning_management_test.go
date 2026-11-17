package store

// S52 T1 — the shape the memory law lands in: the retraction columns ride
// every read, learning_change refuses UPDATE and DELETE at the trigger and
// closes its vocabulary at the CHECK, CreateLearningAudited writes row and
// audit line in one transaction or neither, and the LM1 step moves legacy
// name keys to slugs or refuses naming every stray. The Go refusal checks
// run BEFORE the SQL CHECKs, so each is proven twice here — once as the
// Go sentence (no rows touched) and once raw, proving the schema law
// stands even when a writer bypasses the door.

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestLearningRetractionColumnsRoundTrip — a fresh learning reads back
// un-retracted: nil instant, two empty words. The defaults are the law —
// every row written before S52 rides out meaning "still remembered".
func TestLearningRetractionColumnsRoundTrip(t *testing.T) {
	s := openTemp(t)
	got, err := s.CreateLearning(NewLearningRow{Scope: "person", ScopeKey: "me", Body: "b", Provenance: "told"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.RetractedAt != nil {
		t.Fatalf("a fresh learning reads retracted at %q — the absent stamp must read nil", *got.RetractedAt)
	}
	if got.RetractedReason != "" || got.RetractedBy != "" {
		t.Fatalf("a fresh learning carries retraction words %q/%q — the empty defaults are the un-retracted state", got.RetractedReason, got.RetractedBy)
	}
}

// TestLearningChangeIsAppendOnly — LM3's schema half: the trigger, not a
// convention, owns the promise. Both roads are walked raw, through the
// writer, because a trigger that only the door avoids is no trigger.
func TestLearningChangeIsAppendOnly(t *testing.T) {
	s := openTemp(t)
	got, err := s.CreateLearningAudited(NewLearningRow{Scope: "person", ScopeKey: "me", Body: "b", Provenance: "told"},
		&NewLearningChange{Op: "add", Summary: "added told learning scoped to person/me", ActorKind: "user", ActorID: "1000"})
	if err != nil {
		t.Fatalf("audited create: %v", err)
	}
	_, err = s.w.Exec(`UPDATE learning_change SET summary = 'rewritten' WHERE learning_id = ?`, got.ID)
	if err == nil || !strings.Contains(err.Error(), "learning_change is append-only (S52 LM3)") {
		t.Fatalf("UPDATE of the audit %v, want the append-only trigger to kill it", err)
	}
	_, err = s.w.Exec(`DELETE FROM learning_change WHERE learning_id = ?`, got.ID)
	if err == nil || !strings.Contains(err.Error(), "learning_change is append-only (S52 LM3)") {
		t.Fatalf("DELETE of the audit %v, want the append-only trigger to kill it", err)
	}
}

// TestLearningChangeChecksCloseTheVocabulary — op, actor_kind, and the
// agent-retracts rule are CHECKs, walked raw so the schema law is proven
// independent of any Go door. The last case is LM3's sentence in SQL: an
// agent may appear on an add and nowhere else.
func TestLearningChangeChecksCloseTheVocabulary(t *testing.T) {
	s := openTemp(t)
	ins := `INSERT INTO learning_change (learning_id, op, summary, actor_kind, actor_id, occurred_at) VALUES (?,?,?,?,?,?)`
	cases := []struct{ why, op, kind string }{
		{"op outside the three", "erase", "user"},
		{"actor kind outside the four", "add", "ghost"},
		{"an agent retracting", "retract", "agent"},
		{"an agent restoring", "restore", "agent"},
	}
	for _, c := range cases {
		if _, err := s.w.Exec(ins, 1, c.op, "summary", c.kind, "x", "2026-11-04T00:00:00Z"); err == nil {
			t.Fatalf("%s: the raw INSERT was accepted — the CHECK must refuse it", c.why)
		}
	}
	// Positive control: the same table accepts the closed set.
	if _, err := s.w.Exec(ins, 1, "add", "summary", "agent", "loop-1", "2026-11-04T00:00:00Z"); err != nil {
		t.Fatalf("an agent add must be legal — LM3 bars agents from retract/restore, not from being remembered as writers: %v", err)
	}
}

// TestCreateLearningAuditedWritesRowAndLineTogether — the one writer, both
// halves, one stamp; and the Go shape refusals leave NOTHING behind: an
// audit-shaped-wrong call writes neither row nor line.
func TestCreateLearningAuditedWritesRowAndLineTogether(t *testing.T) {
	s := openTemp(t)
	got, err := s.CreateLearningAudited(NewLearningRow{Scope: "project", ScopeKey: "/r", Body: "b", Provenance: "observed"},
		&NewLearningChange{Op: "add", Summary: "added observed learning scoped to project//r", ActorKind: "cli", ActorID: "cli"})
	if err != nil {
		t.Fatalf("audited create: %v", err)
	}
	lines, err := s.ListLearningChanges(got.ID)
	if err != nil {
		t.Fatalf("list changes: %v", err)
	}
	if len(lines) != 1 || lines[0].Op != "add" || lines[0].ActorKind != "cli" || lines[0].ActorID != "cli" {
		t.Fatalf("the audit lines after one audited add = %+v, want exactly one add line by cli/cli", lines)
	}
	if lines[0].OccurredAt != got.CreatedAt {
		t.Fatalf("audit stamp %q differs from the row's %q — one act, one instant", lines[0].OccurredAt, got.CreatedAt)
	}
	// The shape refusals, each BEFORE any write: LearningID named, op wrong,
	// actor empty, actor kind unknown, summary empty.
	bad := []*NewLearningChange{
		{LearningID: 7, Op: "add", Summary: "s", ActorKind: "user", ActorID: "1"},
		{Op: "retract", Summary: "s", ActorKind: "user", ActorID: "1"},
		{Op: "add", Summary: "s", ActorKind: "user", ActorID: ""},
		{Op: "add", Summary: "s", ActorKind: "ghost", ActorID: "1"},
		{Op: "add", Summary: "", ActorKind: "user", ActorID: "1"},
	}
	for i, ch := range bad {
		if _, err := s.CreateLearningAudited(NewLearningRow{Scope: "person", ScopeKey: "me", Body: "b", Provenance: "told"}, ch); err == nil {
			t.Fatalf("bad audit %d (%+v): accepted — the Go check must refuse before SQL", i, ch)
		}
	}
	rest, err := s.CountAllLearningChanges()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if rest != 1 {
		t.Fatalf("audit lines after five refused creates = %d, want still 1 — a refused shape writes nothing", rest)
	}
}

// TestMigrateLearningAgentScopeKeys — LM1 end to end: name keys resolve to
// slugs through the newest row, a slug key is already law (skipped), two
// unresolved names refuse the WHOLE step naming both, and the refusal
// leaves every key exactly where it was.
func TestMigrateLearningAgentScopeKeys(t *testing.T) {
	s := openTemp(t)
	// Two agents share the name "worker" — newest wins; slug one.
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "worker-old", Name: "worker"}, "2026-11-01T00:00:00Z"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "worker-new", Name: "worker"}, "2026-11-02T00:00:00Z"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	seed := func(scope, key string) int64 {
		id, err := s.CreateLearning(NewLearningRow{Scope: scope, ScopeKey: key, Body: "b", Provenance: "observed"})
		if err != nil {
			t.Fatalf("seed learning %s/%s: %v", scope, key, err)
		}
		return id.ID
	}
	learnedBy := seed("agent", "worker")    // a legacy NAME
	already := seed("agent", "worker-new")  // already a slug
	projectRow := seed("project", "worker") // a project key rides untouched

	moved, err := s.MigrateLearningAgentScopeKeys()
	if err != nil {
		t.Fatalf("LM1 step: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved %d, want 1 (the slug key needs no move, the project key is not the step's)", moved)
	}
	row, ok, err := s.GetLearning(learnedBy)
	if err != nil || !ok {
		t.Fatalf("read moved row: %v %v", err, ok)
	}
	if row.ScopeKey != "worker-new" {
		t.Fatalf("the name key became %q, want the newest agent's slug worker-new", row.ScopeKey)
	}
	if row, _, _ = s.GetLearning(already); row.ScopeKey != "worker-new" {
		t.Fatalf("the slug key moved to %q — already-legal keys are left alone", row.ScopeKey)
	}
	if row, _, _ = s.GetLearning(projectRow); row.ScopeKey != "worker" {
		t.Fatalf("a PROJECT key named worker became %q — the step's scope is agent only", row.ScopeKey)
	}
	// A stray with no agent: the refusal names it and moves nothing else.
	stray := seed("agent", "nobody-here")
	if err := s.CreateAgentAudited(context.Background(), userAgentActor, Agent{Slug: "mover", Name: "mover"}, "2026-11-03T00:00:00Z"); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	seed("agent", "mover") // would move — must not, the refusal is all-or-nothing
	_, err = s.MigrateLearningAgentScopeKeys()
	if err == nil {
		t.Fatal("an unresolvable scope_key was accepted — the step must refuse, never guess")
	}
	if !strings.Contains(err.Error(), "nobody-here") {
		t.Fatalf("the refusal %q does not name the stray it refused on", err)
	}
	if row, _, _ := s.GetLearning(stray); row.ScopeKey != "nobody-here" {
		t.Fatalf("the refused step moved the stray to %q anyway — the refusal is all-or-nothing", row.ScopeKey)
	}
}

// TestLearningsScopedToCounts — LM7's accessor: agent-scope rows by slug,
// active and retracted both (a stamp is not a departure), other scopes
// never.
func TestLearningsScopedToCounts(t *testing.T) {
	s := openTemp(t)
	own, err := s.CreateLearning(NewLearningRow{Scope: "agent", ScopeKey: "worker-old", Body: "b", Provenance: "observed"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.w.Exec(`UPDATE learning SET retracted_at = ? WHERE id = ?`, "2026-11-04T09:00:00Z", own.ID); err != nil {
		t.Fatalf("stamp retraction: %v", err)
	}
	if _, err := s.CreateLearning(NewLearningRow{Scope: "agent", ScopeKey: "worker-old", Body: "b2", Provenance: "observed"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.CreateLearning(NewLearningRow{Scope: "project", ScopeKey: "worker-old", Body: "b", Provenance: "observed"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	n, err := s.LearningsScopedTo("worker-old")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("LearningsScopedTo(worker-old) = %d, want 2 — the retracted row still belongs to the agent, and the project row never did", n)
	}
}

// --- S52 T2: the stamp, the index hygiene, the scope resolution ---------

// TestSetLearningRetractionRoundTrip — the store half of LM2: found and
// changed answered separately and honestly; the stamp carries instant +
// reason + kind/id; the audit line rides the same transaction and the same
// s.Now() as the stamp; the idempotence writes NOTHING (census stands);
// restore clears the instant and keeps the words.
func TestSetLearningRetractionRoundTrip(t *testing.T) {
	s := openTemp(t)
	got, err := s.CreateLearningAudited(NewLearningRow{Scope: "project", ScopeKey: "/r", Body: "the body", Provenance: "told"},
		&NewLearningChange{Op: "add", Summary: "added told learning scoped to project//r", ActorKind: "user", ActorID: "1000"})
	if err != nil {
		t.Fatalf("audited create: %v", err)
	}
	row, found, changed, err := s.SetLearningRetraction(got.ID, true, "stale", "user", "1000",
		&NewLearningChange{Op: "retract", Summary: "retracted learning 1 (scope project//r)", ActorKind: "user", ActorID: "1000"})
	if err != nil || !found || !changed {
		t.Fatalf("retract found %v changed %v err %v", found, changed, err)
	}
	if row.RetractedAt == nil || *row.RetractedAt == "" || row.RetractedReason != "stale" || row.RetractedBy != "user/1000" {
		t.Fatalf("stamp = %v/%q/%q", row.RetractedAt, row.RetractedReason, row.RetractedBy)
	}
	live, err := s.ListLearnings(LearningFilters{Scope: "project", ScopeKey: "/r"})
	if err != nil || len(live) != 0 {
		t.Fatalf("default ListLearnings = %d rows after the stamp — the shared predicate must exclude it", len(live))
	}
	all, err := s.ListLearnings(LearningFilters{Scope: "project", ScopeKey: "/r", IncludeRetracted: true})
	if err != nil || len(all) != 1 {
		t.Fatalf("includeRetracted ListLearnings = %d rows (err %v)", len(all), err)
	}
	// The idempotent second stamp: found, NOT changed, census unmoved.
	_, found, changed, err = s.SetLearningRetraction(got.ID, true, "again", "cli", "cli", nil)
	if err != nil || !found || changed {
		t.Fatalf("second retract found %v changed %v err %v", found, changed, err)
	}
	n, _ := s.CountAllLearningChanges()
	if n != 2 {
		t.Fatalf("census %d after the idempotent second stamp, want 2 — no audit line for a non-act", n)
	}
	restored, _, changed, err := s.SetLearningRetraction(got.ID, false, "", "cli", "cli",
		&NewLearningChange{Op: "restore", Summary: "restored learning 1 (scope project//r)", ActorKind: "cli", ActorID: "cli"})
	if err != nil || !changed || restored.RetractedAt != nil {
		t.Fatalf("restore changed %v at %v err %v", changed, restored.RetractedAt, err)
	}
	if restored.RetractedReason != "stale" || restored.RetractedBy != "user/1000" {
		t.Fatalf("restore cleared the WORDS too (%q/%q) — only the instant clears; the words are recent history",
			restored.RetractedReason, restored.RetractedBy)
	}
	n, _ = s.CountAllLearningChanges()
	if n != 3 {
		t.Fatalf("census %d, want add + retract + restore", n)
	}
	if _, _, _, err := s.SetLearningRetraction(9999, true, "r", "user", "1", nil); err != nil {
		t.Fatalf("unknown id returned an error: %v — absence is found:false, not a failure", err)
	}
	// Op-shape refusals ride before any read or write.
	if _, _, _, err := s.SetLearningRetraction(got.ID, true, "r", "user", "1",
		&NewLearningChange{Op: "add", Summary: "s", ActorKind: "user", ActorID: "1"}); err == nil {
		t.Fatal("an add-shaped audit on a retraction stamp was accepted")
	}
	if _, _, _, err := s.SetLearningRetraction(got.ID, true, "r", "user", "1",
		&NewLearningChange{LearningID: 55, Op: "retract", Summary: "s", ActorKind: "user", ActorID: "1"}); err == nil {
		t.Fatal("a retraction audit naming a DIFFERENT row was accepted")
	}
}

// TestDropLearningVectorsWalksEveryVecTable — LM5's hygiene: the walk
// covers EVERY learning_vec table that exists (a body may have been
// embedded under more than one (model, dim)), an id with no vectors
// anywhere returns 0 and no error, and other rows keep theirs.
func TestDropLearningVectorsWalksEveryVecTable(t *testing.T) {
	s := openTemp(t)
	a, err := s.CreateLearning(NewLearningRow{Scope: "person", ScopeKey: "me", Body: "a", Provenance: "told"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateLearning(NewLearningRow{Scope: "person", ScopeKey: "me", Body: "b", Provenance: "told"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureLearningVecTable("model-one", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureLearningVecTable("model-two", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertLearningVec(a.ID, "model-one", 2, []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertLearningVec(a.ID, "model-two", 3, []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertLearningVec(b.ID, "model-one", 2, []float32{0, 1}); err != nil {
		t.Fatal(err)
	}
	dropped, err := s.DropLearningVectors(a.ID)
	if err != nil || dropped != 2 {
		t.Fatalf("dropped %d (err %v), want both tables touched", dropped, err)
	}
	ids, err := s.LearningVecIDs("model-one", 2)
	if err != nil || len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("model-one ids = %v (err %v), want only b", ids, err)
	}
	ids, err = s.LearningVecIDs("model-two", 3)
	if err != nil || len(ids) != 0 {
		t.Fatalf("model-two ids = %v (err %v), want empty", ids, err)
	}
	if dropped, err := s.DropLearningVectors(9999); err != nil || dropped != 0 {
		t.Fatalf("dropping a never-embedded id = %d (err %v), want 0 with no error", dropped, err)
	}
}

// TestAgentProjectRootsReadsTheRunning — LM5's scope resolution read: the
// agent's runs are step rows — step.agent (the NAME the runtime writes)
// beside step.component_root (the key project-scoped learnings ride);
// three steps in two roots answer TWO roots; an agent that never ran
// answers none.
func TestAgentProjectRootsReadsTheRunning(t *testing.T) {
	s := openTemp(t)
	for i, spec := range []struct {
		slug, root string
	}{
		{"loop-a", "/srv/a"},
		{"loop-b", "/srv/b"},
		{"loop-c", "/srv/a"},
	} {
		if _, err := s.CreateLoop(NewLoop{Slug: spec.slug, Title: spec.slug, Root: spec.root, ComponentRoot: spec.root, Mode: "plan", Status: "running"}); err != nil {
			t.Fatalf("seed loop: %v", err)
		}
		att := i + 1
		if _, err := s.CreateStep(Scope{LoopSlug: spec.slug, ComponentRoot: spec.root}, NewStep{TaskID: "t", Agent: "worker", Attempt: att}); err != nil {
			t.Fatalf("seed step: %v", err)
		}
	}
	if _, err := s.CreateStep(Scope{LoopSlug: "loop-b", ComponentRoot: "/srv/b"}, NewStep{TaskID: "t2", Agent: "worker", Attempt: 9}); err != nil {
		t.Fatalf("seed second step: %v", err)
	}
	roots, err := s.AgentProjectRoots("worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0] != "/srv/a" || roots[1] != "/srv/b" {
		t.Fatalf("roots = %v, want the two distinct roots in order", roots)
	}
	if roots, err := s.AgentProjectRoots("nobody"); err != nil || len(roots) != 0 {
		t.Fatalf("an agent that never ran answered %v (err %v), want none", roots, err)
	}
}

// TestEveryDefaultReaderExcludesRetracted — LM2's "ONE predicate" law,
// checked at the file, not hoped for: every query in the store's learnings
// file that reads FROM learning must compose the live predicate (inline or
// through learningLiveSQL), except the three NAMED exceptions — GetLearning
// (Show answers a flagged row), LearningsScopedTo (LM7 counts what IS
// there, retraction is not absence), and MigrateLearningAgentScopeKeys
// (LM1 moves rows whatever their state). A fourth reader that forgets the
// predicate is a leak this test dies on.
func TestEveryDefaultReaderExcludesRetracted(t *testing.T) {
	src, err := readFileForGuard(t, "learnings.go")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"GetLearning": true, "LearningsScopedTo": true, "MigrateLearningAgentScopeKeys": true,
		// The stamper itself: the tool that MOVES the retraction state
		// must read the row whatever its state (LM2's idempotence is
		// decided ON the stamp), and its read is by exact id, never a
		// default reader of "what is memory now".
		"SetLearningRetraction": true,
	}
	// The three named exceptions plus SetLearningRetraction are the law's
	// own words beside the map; see the comments there. The list is short
	// because the law is one predicate.

	// Split into function chunks on the method-receiver lines.
	type chunk struct {
		name string
		body string
	}
	var chunks []chunk
	current := chunk{name: "(package scope)"}
	for _, line := range splitLinesForGuard(src) {
		if name := funcNameForGuard(line); name != "" {
			chunks = append(chunks, current)
			current = chunk{name: name}
		}
		current.body += line + "\n"
	}
	chunks = append(chunks, current)
	for _, c := range chunks {
		// Blank out the OTHER learning tables' names (learning_change,
		// learning_vec_*) so only reads of the learning table itself match.
		b := blankOtherLearningTables(c.body)
		if !readsLearningTable(b) {
			continue
		}
		if allowed[c.name] {
			continue
		}
		if !strings.Contains(c.body, "retracted_at IS NULL") && !strings.Contains(c.body, "learningLiveSQL") {
			t.Errorf("%s reads FROM learning without the shared live predicate — LM2's ONE predicate means exactly one WHERE for what is memory now; compose learningLiveSQL or name the exception here with a reason", c.name)
		}
	}
}

// --- the shared-predicate guard's own helpers (test-local, one home) ----

func readFileForGuard(t *testing.T, name string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(name)
	return string(b), err
}

func splitLinesForGuard(s string) []string { return strings.Split(s, "\n") }

var funcNameRE = regexp.MustCompile(`^func \([a-z] \*Store\) ([A-Z]\w*)`)

func funcNameForGuard(line string) string {
	m := funcNameRE.FindStringSubmatch(line)
	if m == nil {
		return ""
	}
	return m[1]
}

var otherLearningTableRE = regexp.MustCompile(`learning_[a-zA-Z_]+`)

func blankOtherLearningTables(s string) string {
	return otherLearningTableRE.ReplaceAllString(s, "X")
}

var readsLearningRE = regexp.MustCompile(`(?i)FROM learning\b`)

func readsLearningTable(s string) bool { return readsLearningRE.MatchString(s) }
