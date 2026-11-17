package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// T4's tests. The scoping one is the important one.

func TestTwoLoopsWithTheSameSlugInDifferentComponentRootsStaySeparate(t *testing.T) {
	// D13, and the test that makes the whole of scope.go worth having.
	//
	// THE SAME SLUG, deliberately. A test using distinct slugs cannot detect a
	// missing scope predicate — it would pass against a store with no scoping
	// at all. And `refactor` in two checkouts is not a contrived case; it is
	// what a git worktree produces for free.
	s := openTemp(t)
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")

	loopA, err := s.GetLoop(a)
	if err != nil {
		t.Fatalf("read %s: %v", a, err)
	}
	loopB, err := s.GetLoop(b)
	if err != nil {
		t.Fatalf("read %s: %v", b, err)
	}
	if loopA.ID == loopB.ID {
		t.Fatal("the two loops are the same row, so the second overwrote the first")
	}
	if loopA.ComponentRoot != "/repo-a" || loopB.ComponentRoot != "/repo-b" {
		t.Errorf("reads crossed: A is in %s, B is in %s", loopA.ComponentRoot, loopB.ComponentRoot)
	}

	// LIST, scoped.
	inA, err := s.ListLoops(ComponentScope{ComponentRoot: "/repo-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inA) != 1 || inA[0].ComponentRoot != "/repo-a" {
		t.Errorf("listing /repo-a returned %d loops: %+v", len(inA), inA)
	}

	// UPDATE, scoped: moving one must not move the other.
	if err := s.SetLoopPhase(a, PhaseBuild); err != nil {
		t.Fatalf("set the phase of %s: %v", a, err)
	}
	loopA, _ = s.GetLoop(a)
	loopB, _ = s.GetLoop(b)
	if loopA.Phase != string(PhaseBuild) {
		t.Errorf("A's phase is %q, want build", loopA.Phase)
	}
	if loopB.Phase == string(PhaseBuild) {
		t.Error("updating A moved B as well; the UPDATE is missing its scope predicate")
	}
}

func TestListingLoopsAcrossEveryProjectIsExplicit(t *testing.T) {
	// A daemon multiplexes every component on the machine, so an unscoped
	// default would show a person every repository's loops whenever they were
	// in one — and they would learn to ignore the list. `--all` is a flag.
	s := openTemp(t)
	seedLoop(t, s, "one", "/repo-a")
	seedLoop(t, s, "two", "/repo-b")

	all, err := s.ListLoops(ComponentScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("the unscoped list returned %d loops, want 2", len(all))
	}
	scoped, err := s.ListLoops(ComponentScope{ComponentRoot: "/repo-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 {
		t.Errorf("the scoped list returned %d loops, want 1", len(scoped))
	}
}

func TestEveryScopedQueryComposesTheScopeHelper(t *testing.T) {
	// I4: discipline is not a mechanism. A query that forgets `component_root`
	// returns another component's rows, and slugs repeat — so the predicate is
	// produced in one place and this grep asserts nothing hand-writes it.
	//
	// scope.go itself is where the predicate lives, so it is exempt.
	bare := regexp.MustCompile(`loop_slug\s*=\s*\?`)
	for name, src := range packageFiles(t) {
		if name == "scope.go" {
			continue
		}
		for _, line := range strings.Split(src, "\n") {
			if !bare.MatchString(line) {
				continue
			}
			// A hand-written predicate is acceptable only when the SAME
			// statement also constrains component_root — which is the property
			// the helper guarantees and this allows a caller to prove inline.
			if strings.Contains(line, "component_root") {
				continue
			}
			// ProvisionsForLoop is keyed by slug ON PURPOSE (its doc comment in
			// provisions.go): the loop.finished hook knows the loop and nothing
			// else, and supplying a root there is what hid the defect it fixes.
			// Exempt that ONE statement, not the file, so any other bare
			// predicate in provisions.go is still caught.
			if name == "provisions.go" && strings.Contains(line, "WHERE loop_slug = ? ORDER BY id DESC") {
				continue
			}
			// SetIntakeLoop (intake.go, S61 T1) mentions loop_slug in the SET
			// clause, never as a predicate: the row is selected by its own
			// primary key (`intake.id = ?`), and the intake table is not a
			// loop-scoped table — it has no component_root to scope by until
			// the commit gives it a root_id. The guard's law (a SELECT keyed
			// on a bare slug can cross components) does not reach this
			// statement. Exempt that ONE statement, not the file.
			if name == "intake.go" && strings.Contains(line, "UPDATE intake SET loop_slug = ? WHERE id = ?") {
				continue
			}
			t.Errorf("%s hand-writes `loop_slug = ?` without component_root:\n    %s\n"+
				"  Compose Scope.Where instead (I4)", name, strings.TrimSpace(line))
		}
	}
}

func TestReadsUseTheReaderPoolAndWritesTheWriter(t *testing.T) {
	// O6 / I6. A read on the single-connection writer serialises behind every
	// write, and the symptom is a UI that stalls whenever a loop is busy —
	// which gets diagnosed as a UI problem, somewhere else entirely.
	//
	// The grep is crude on purpose: it looks for a write verb issued on `s.r`
	// and a plain SELECT issued on `s.w`. The exceptions are named.
	writeVerbs := regexp.MustCompile(`s\.r\.(Exec|Begin)`)
	for name, src := range packageFiles(t) {
		if m := writeVerbs.FindString(src); m != "" {
			t.Errorf("%s writes through the reader pool (%s)", name, m)
		}
	}

	// And the reverse: a SELECT on the writer. Allowed in exactly two places,
	// each named with its reason.
	readOnWriter := regexp.MustCompile(`s\.w\.Query(Row)?\(`)
	allowed := map[string]string{
		// Both ask the CONNECTION about itself — vec_version() and the pragmas
		// are per-connection facts, so asking the reader would answer about the
		// wrong pool. That is the point of asking both (Q2).
		"open.go": "vec_version and the pragmas are per-connection facts, asked of each pool deliberately",
	}
	for name, src := range packageFiles(t) {
		if m := readOnWriter.FindString(src); m != "" {
			if _, ok := allowed[name]; !ok {
				t.Errorf("%s reads through the writer pool (%s); reads use s.r (O6)", name, m)
			}
		}
	}
}

func TestATaskStatusChangeTheMachineRefusesIsAResultNotAnError(t *testing.T) {
	// DB19. "This transition is not allowed" and "the disk is full" need
	// different handling, and a single error return makes every caller guess
	// which it got. So a refusal comes back with err == nil and OK == false.
	s := openTemp(t)
	sc := seedLoop(t, s, "ladder", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1", Title: "a task"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := s.SetTaskStatus(sc, "T1", StatusComplete)
	if err != nil {
		t.Fatalf("a refused transition returned an error: %v", err)
	}
	if res.OK {
		t.Fatal("pending -> complete was applied")
	}
	if res.Transition.Reason == "" {
		t.Error("the refusal carries no reason")
	}
	if len(res.Transition.Path) == 0 {
		t.Error("the refusal offers no path, so the caller has nothing to act on")
	}

	// And nothing changed.
	after, err := s.GetTask(sc, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != string(StatusPending) {
		t.Errorf("the refused transition still wrote: status is %q", after.Status)
	}
}

func TestATaskStatusChangeTheMachineAllowsSucceeds(t *testing.T) {
	// The negative control: a machine that refused everything would pass the
	// test above.
	s := openTemp(t)
	sc := seedLoop(t, s, "ladder", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.SetTaskStatus(sc, "T1", StatusInProgress)
	if err != nil {
		t.Fatalf("an allowed transition failed: %v", err)
	}
	if !res.OK {
		t.Fatalf("pending -> in-progress was refused: %s", res.Transition.Reason)
	}
	if res.Task.Status != string(StatusInProgress) {
		t.Errorf("the returned task says %q", res.Task.Status)
	}
}

func TestTheLoopsModeDecidesWhichTableApplies(t *testing.T) {
	// Read from the LOOP, not taken from the caller: a caller passing the mode
	// could pass a different one from the loop's and get a transition the
	// ladder does not actually allow.
	s := openTemp(t)
	std := seedLoop(t, s, "standard-loop", "/repo")
	if _, err := s.CreateLoop(NewLoop{Slug: "poc-loop", ComponentRoot: "/repo", Mode: string(ModePoc)}); err != nil {
		t.Fatal(err)
	}
	poc := Scope{LoopSlug: "poc-loop", ComponentRoot: "/repo"}

	for _, sc := range []Scope{std, poc} {
		if _, err := s.CreateTask(sc, NewTask{TaskID: "T1", Status: StatusSecure}); err != nil {
			t.Fatalf("create a task in %s: %v", sc, err)
		}
	}

	if res, _ := s.SetTaskStatus(std, "T1", StatusComplete); res.OK {
		t.Error("secure -> complete was applied in a standard loop")
	}
	res, err := s.SetTaskStatus(poc, "T1", StatusComplete)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Errorf("secure -> complete was refused in a poc loop: %s", res.Transition.Reason)
	}
}

func TestAnUnknownTaskStatusIsRefusedAtCreation(t *testing.T) {
	// A row holding a status no transition mentions can never move again, so
	// it is refused on the way in rather than discovered later.
	s := openTemp(t)
	sc := seedLoop(t, s, "unknown", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1", Status: "almost-done"}); err == nil {
		t.Fatal("a task was created with a status the ladder does not have")
	}
}

func TestPhasesRoundTripWithTheirStatus(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "phases", "/repo")

	if err := s.SetPhase(sc, "discovery", "done", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhase(sc, "build", "in-progress", 2); err != nil {
		t.Fatal(err)
	}
	// Upsert, not insert: the tracker is written twice per phase — in-progress
	// before dispatch, done after.
	if err := s.SetPhase(sc, "build", "done", 2); err != nil {
		t.Fatal(err)
	}

	phases, err := s.Phases(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d: %+v", len(phases), phases)
	}
	if phases[0].Phase != "discovery" || phases[1].Phase != "build" {
		t.Errorf("phases came back out of ord order: %+v", phases)
	}
	if phases[1].Status != "done" {
		t.Errorf("the second write did not update the row: %+v", phases[1])
	}
}

func TestTimestampsAreISO8601UTC(t *testing.T) {
	// I9, against a FIXED injected clock — so the assertion is on an exact
	// string rather than on a shape. Matching v1's format exactly, so the two
	// databases are comparable by eye during the cut-over, which is a real
	// operation somebody performs once, under pressure, by reading rows side by
	// side.
	s := openTemp(t)
	l, err := s.CreateLoop(NewLoop{Slug: "clock", ComponentRoot: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "2026-10-15T09:41:12.345Z"
	if l.StartedAt != want {
		t.Errorf("started_at is %q, want %q", l.StartedAt, want)
	}
	if l.UpdatedAt != want {
		t.Errorf("updated_at is %q, want %q", l.UpdatedAt, want)
	}
}

func TestAMissingLoopReadsAsNotFoundRatherThanAnEmptyRow(t *testing.T) {
	// A lookup that found nothing is not a domain refusal: nothing was
	// refused. It is an error every caller handles the same way, and
	// errors.Is is what makes that possible.
	s := openTemp(t)
	_, err := s.GetLoop(Scope{LoopSlug: "nope", ComponentRoot: "/repo"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing loop returned %v, want ErrNotFound", err)
	}
}

func TestAnUpdateAgainstAMissingLoopIsNotFoundRatherThanSilence(t *testing.T) {
	// Without the rows-affected check, an UPDATE against a scope that does not
	// exist SUCCEEDS silently and the caller believes it wrote something.
	s := openTemp(t)
	err := s.SetLoopPhase(Scope{LoopSlug: "nope", ComponentRoot: "/repo"}, PhaseBuild)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("updating a missing loop returned %v, want ErrNotFound", err)
	}
}

func TestAHalfScopeIsRefusedRatherThanTreatedAsAWildcard(t *testing.T) {
	// A Scope with one half set is the bug the type exists to prevent.
	// Silently treating an empty ComponentRoot as "any" would make every read
	// cross-component.
	s := openTemp(t)
	for _, sc := range []Scope{{LoopSlug: "x"}, {ComponentRoot: "/repo"}, {}} {
		if _, err := s.GetLoop(sc); err == nil {
			t.Errorf("GetLoop(%+v) succeeded with half a scope", sc)
		}
	}
}

func TestTaskDependenciesRoundTripAsAList(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "deps", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T2", DependsOn: []string{"T1"}}); err != nil {
		t.Fatal(err)
	}
	t2, err := s.GetTask(sc, "T2")
	if err != nil {
		t.Fatal(err)
	}
	if len(t2.DependsOn) != 1 || t2.DependsOn[0] != "T1" {
		t.Errorf("depends_on came back as %v", t2.DependsOn)
	}
	t1, err := s.GetTask(sc, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if t1.DependsOn == nil {
		t.Error("a task with no dependencies came back with a nil list rather than an empty one; the two " +
			"encode differently in JSON and the app renders them differently")
	}
}

func TestTouchingATaskTouchesItsLoop(t *testing.T) {
	// "Every loop by recency" (2a) is the one list a person looks at. A loop
	// whose tasks are moving and whose updated_at is stale sorts to the bottom
	// of it.
	s := openTemp(t)
	early := "2026-10-15T09:41:12.345Z"
	sc := seedLoop(t, s, "touch", "/repo")
	if _, err := s.CreateTask(sc, NewTask{TaskID: "T1"}); err != nil {
		t.Fatal(err)
	}

	// A later clock, so the touch is observable.
	s.clock = func() time.Time { return mustParseISO(t, "2026-10-16T10:00:00.000Z") }
	if _, err := s.SetTaskStatus(sc, "T1", StatusInProgress); err != nil {
		t.Fatal(err)
	}
	l, err := s.GetLoop(sc)
	if err != nil {
		t.Fatal(err)
	}
	if l.UpdatedAt == early {
		t.Error("moving a task did not touch its loop, so the loop list would show it as stale")
	}
}

// sameAttended compares the three states by VALUE: nil is never-declared, and
// only nil equals nil.
func sameAttended(got, want *bool) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}

func formatAttended(p *bool) string {
	if p == nil {
		return "never-recorded"
	}
	if *p {
		return "attended"
	}
	return "unattended"
}

func TestLoopAttendedAccessor(t *testing.T) {
	// D2's read side. loop.attended (0019) is three-state (P31): 1 attended,
	// 0 unattended, NULL never recorded — and NULL must never read as false,
	// because "nobody said" and "nobody is here" are different facts. A
	// default here would tell the operator "you chose unattended" about a
	// loop that arrived before the choice existed.
	s := openTemp(t)
	yes, no := true, false
	for _, tc := range []struct {
		name     string
		declared *bool
		want     *bool
		raw      *int64 // the stored column, asserted raw — 1 / 0 / NULL
	}{
		{"attended", &yes, &yes, func() *int64 { v := int64(1); return &v }()},
		{"unattended", &no, &no, func() *int64 { v := int64(0); return &v }()},
		{"never-recorded", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, err := s.CreatePinnedLoop(NewLoop{Slug: tc.name, ComponentRoot: "/repo", Attended: tc.declared}, "bp", 1, "raw blueprint text", `{"route":""}`)
			if err != nil {
				t.Fatal(err)
			}

			// Raw, so a defaulted column cannot hide behind a matching decode:
			// declared-unattended must be stored as 0, distinct from NULL.
			var stored sql.NullInt64
			if err := s.r.QueryRow(`SELECT attended FROM loop WHERE id=?`, l.ID).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.raw == nil && stored.Valid:
				t.Errorf("the undeclared creation stored %d; nil must arrive as NULL, not a default", stored.Int64)
			case tc.raw != nil && (!stored.Valid || stored.Int64 != *tc.raw):
				t.Errorf("the stored column is %v, want %d", stored, *tc.raw)
			}

			// ReadPinnedLoop returns the state beside the pin...
			pin, err := s.ReadPinnedLoop(l.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !sameAttended(pin.Attended, tc.want) {
				t.Errorf("ReadPinnedLoop returned %s, want %s", formatAttended(pin.Attended), formatAttended(tc.want))
			}

			// ...and so does the accessor the daemon adapter reads.
			got, err := s.LoopAttended(context.Background(), l.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !sameAttended(got, tc.want) {
				t.Errorf("LoopAttended returned %s, want %s", formatAttended(got), formatAttended(tc.want))
			}
		})
	}

	// The pre-T11 loop: a row written before S11 (or by any path that made no
	// declaration) reads honestly as never-recorded, not as false.
	pre, err := s.CreateLoop(NewLoop{Slug: "pre-t11", ComponentRoot: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LoopAttended(context.Background(), pre.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("the pre-T11 loop reads %s, want never-recorded", formatAttended(got))
	}

	// CreateLoop persists a declaration when its caller makes one — the input
	// type carries it on both creation paths, and neither may drop it.
	declared, err := s.CreateLoop(NewLoop{Slug: "declared", ComponentRoot: "/repo", Attended: &no})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.LoopAttended(context.Background(), declared.ID)
	if err != nil || got == nil || *got {
		t.Errorf("CreateLoop dropped the declaration: %s (%v)", formatAttended(got), err)
	}

	// A missing loop is ErrNotFound — the accessor may not answer "nobody
	// declared" about a loop that does not exist.
	if _, err := s.LoopAttended(context.Background(), "no-such-loop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing loop returned %v, want ErrNotFound", err)
	}
	if _, err := s.LoopAttended(context.Background(), ""); err == nil {
		t.Error("the empty id was not refused")
	}

	// A row holding a value nobody writes is refused BY NAME, not guessed
	// into one of the two declared states.
	corrupt, err := s.CreatePinnedLoop(NewLoop{Slug: "corrupt", ComponentRoot: "/repo"}, "bp", 1, "raw", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE loop SET attended = 2 WHERE id = ?`, corrupt.ID); err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func() error{
		"LoopAttended":   func() error { _, err := s.LoopAttended(context.Background(), corrupt.ID); return err },
		"ReadPinnedLoop": func() error { _, err := s.ReadPinnedLoop(corrupt.ID); return err },
	} {
		err := read()
		if err == nil || !strings.Contains(err.Error(), "2") {
			t.Errorf("%s on a corrupt row returned %v; the odd value must be named, never collapsed", name, err)
		}
	}
}
