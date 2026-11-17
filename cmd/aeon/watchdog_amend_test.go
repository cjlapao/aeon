package main

// watchdog_amend_test.go — S55 T9 (P4). The guard amendment for the watchdog
// plane, in the mould of budget_amend_test.go (T8) and
// loop_control_amend_test.go (T5): the slice's security claim made mechanical
// rather than reviewed.
//
// The claim: watchdog.set, watchdog.setEnabled and watchdog.remove are
// PERSON doors on the daemon's wide socket (LC18) and are provably absent
// from every non-human door — the narrow (agent / HTTP / MCP-stdio) surface,
// the agent-services enumeration, the tool registry, the records catalog, and
// by text in the four packages that own those doors. An agent may READ the
// watchdog plane (T10's read twins); it never writes it, and no write exists
// in the tools at all (OD10). The other half of the slice's claim is that the
// watchdog itself never restarts anything (OD8): it parks, it blocks, it
// notifies — and a blocked loop stays blocked until a person moves it.
//
// Positive half first, as the family's style demands: the app has its door
// (TestWatchdogIsAHumanDoor), and the guards each have a canary that proves
// they can FAIL (TestWatchdogGuardsHaveTeeth) — a guard that cannot fail is
// not a guard, it is a green light.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

// guardWatchdogService is the non-nil seam a guard needs to see the six
// watchdog methods REGISTERED. The door (RegisterWatchdog) only inspects
// whether the seam is nil — it never calls these methods — so a guard may
// stand in a seam that answers nothing (the loop-control precedent,
// loop_control_amend_test.go), without standing up a store.
type guardWatchdogService struct{}

var _ protocol.WatchdogStore = guardWatchdogService{} // compile-time: the fake satisfies the seam

func (guardWatchdogService) List(context.Context, string, string, *bool) ([]protocol.WatchdogRuleView, error) {
	return nil, nil
}
func (guardWatchdogService) Show(context.Context, string) (protocol.WatchdogShowResult, error) {
	return protocol.WatchdogShowResult{}, nil
}
func (guardWatchdogService) Vocabulary(context.Context) (protocol.WatchdogVocabularyResult, error) {
	return protocol.WatchdogVocabularyResult{}, nil
}
func (guardWatchdogService) Set(context.Context, protocol.Actor, protocol.WatchdogSetParams) (protocol.WatchdogSetResult, *protocol.WatchdogRefusal, error) {
	return protocol.WatchdogSetResult{}, nil, nil
}
func (guardWatchdogService) SetEnabled(context.Context, protocol.Actor, string, bool, bool, string) (protocol.WatchdogSetEnabledResult, *protocol.WatchdogRefusal, error) {
	return protocol.WatchdogSetEnabledResult{}, nil, nil
}
func (guardWatchdogService) RemoveRule(context.Context, protocol.Actor, string, string) (protocol.WatchdogRemoveResult, *protocol.WatchdogRefusal, error) {
	return protocol.WatchdogRemoveResult{}, nil, nil
}

// newWatchdogTable builds the table the way a daemon with a store builds it
// (daemon.go): NewTable, assign the seam, RegisterWatchdog. A non-nil seam
// registers the six; the return is true, so the app has its door — the
// positive half the guards state before the negative one.
func newWatchdogTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Watchdog = guardWatchdogService{}
	if !tbl.RegisterWatchdog() {
		t.Fatal("RegisterWatchdog refused to register over a non-nil seam — the app would have no watchdog door at all")
	}
	return tbl
}

// The three PERSON writes (LC18) — the set a non-human door must never carry
// — and the six-plane names (the three writes, the three reads).
var (
	watchdogWrites = []protocol.MethodName{
		protocol.MethodWatchdogSet, protocol.MethodWatchdogSetEnabled,
		protocol.MethodWatchdogRemove,
	}
	watchdogReads = []protocol.MethodName{
		protocol.MethodWatchdogList, protocol.MethodWatchdogShow,
		protocol.MethodWatchdogVocabulary,
	}
	watchdogAll = func() []protocol.MethodName {
		out := append([]protocol.MethodName{}, watchdogWrites...)
		return append(out, watchdogReads...)
	}()
)

// TestWatchdogIsAHumanDoor is the positive half (P4): the exact write set is
// registered, each RequiresInitialize, none Debug; the read set is listed
// separately; and with the seam nil the six stay ABSENT (V13 — a storeless
// daemon has no watchdog surface, not a refusing one), so the narrow
// projection cannot even name them.
func TestWatchdogIsAHumanDoor(t *testing.T) {
	tbl := newWatchdogTable(t)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// The exact write set: registered, RequiresInitialize, none Debug.
	for _, m := range watchdogWrites {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the watchdog writes (LC18)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a watchdog write may not precede the connection's negotiation", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a watchdog write is a product method on the wide socket, not a diagnostic", m)
		}
	}

	// The read set: registered, RequiresInitialize, none Debug.
	for _, m := range watchdogReads {
		if !have[m] {
			t.Fatalf("%s must be registered — the read door is missing", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table", m)
		}
		if !got.RequiresInitialize || got.Debug {
			t.Fatalf("%s is RequiresInitialize=%v Debug=%v, want true/false", m, got.RequiresInitialize, got.Debug)
		}
	}

	// Nil seam: the six stay ABSENT (V13), and dispatch answers
	// method-not-found — the absence from the CALLER's side (the settings
	// guard's negative 5, carried).
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterWatchdog() // no-op over a nil seam: the six stay absent
	for _, m := range watchdogAll {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s dispatched on a nil-seam table — the narrow surface can reach a door it must not (want ErrUnknownMethod, got %v)", m, err)
		}
	}
}

// TestWatchdogNeverReachesANonHumanDoor is the negative half, one subtest per
// door: the narrow surface, the agent-services enumeration, the tool
// registry, the records catalog, the text of the four packages that own those
// doors, and the walk's non-vacuity. A watchdog write on any of them would
// hand a non-person the means to loosen the daemon's own safety net — the
// same refusal as loop control (T5) and the budget plane (T8), one plane over.
func TestWatchdogNeverReachesANonHumanDoor(t *testing.T) {
	// ── Negative 1 — the narrow table: no action projects one of the three
	// writes, and NO watchdog-plane method reaches the surface at all — the
	// budget shape, where the narrow surface has no budget door, read or
	// write. Here the same: an agent that could list the daemon's watchdog
	// rules could read its own tripwires.
	t.Run("narrow", func(t *testing.T) {
		for _, a := range narrow.Actions() {
			for _, m := range watchdogWrites {
				if a.SocketMethod == m {
					t.Errorf("action %q projects the person write %q — an agent reaching a watchdog write loosens the daemon's safety net (LC18)", a.Name, m)
				}
			}
			sm := string(a.SocketMethod)
			if strings.HasPrefix(sm, "watchdog.") {
				t.Errorf("action %q projects watchdog-plane method %q — the narrow surface has no watchdog door, read or write (LC18)", a.Name, sm)
			}
		}
	})

	// ── Negative 2 — the agent-services enumeration: the agent-to-agent
	// task lifecycle (agent.*) has no watchdog door, so a watchdog method
	// cannot ride it either.
	t.Run("agent-services", func(t *testing.T) {
		for _, m := range protocol.AgentServiceMethods() {
			for _, w := range watchdogAll {
				if m.Name == w {
					t.Fatalf("%s is an agent-service method — an agent reaching the watchdog plane with no person in the room (LC18)", m.Name)
				}
			}
		}
	})

	// ── Negative 3 — the tool registry: no tool name carries a watchdog
	// write verb except the NAMED exceptions — the pre-existing task-lifecycle
	// tool (its "set" is a task's STATE, not a watchdog rule), and T10's
	// watchdog_list read twin, listed so the exception set is the record of
	// what is allowed to look watchdog-shaped. A tool named after a watchdog
	// write would be a second door onto the write the socket alone carries
	// (OD10: no agent write exists for this plane at all).
	t.Run("tools-registry", func(t *testing.T) {
		writeVerb := regexp.MustCompile(`set|remove`)
		namedExceptions := map[string]bool{
			"task_set_status": true, // pre-existing: the agent task lifecycle record tool; "set" = a task's state
			// T10's read twin (registered when T10 lands):
			"watchdog_list": true,
			// 2026-11-02 merge reconciliation: S45 T7's and S46 T8's
			// propose-remove twins carry the "remove" substring (proposals
			// FOR a removal, inert until a human decides — not the write).
			// The exception set stays the record of what is allowed to look
			// watchdog-shaped.
			"policy_propose_remove":     true, // S45 T7: the policy.remove PROPOSAL (inert row, not the write)
			"mcp_propose_remove_server": true, // S46 T8: the mcp server-removal PROPOSAL (inert row, not the write)
			// S62 T8 (2026-11-02): the export family's read twin over
			// export.sets — its "set" is a record SET (a declared export set),
			// not a watchdog write; a re-run re-lists, nothing to reconcile.
			"export_sets": true,
		}
		for name := range tools.Registry() {
			if namedExceptions[name] {
				continue
			}
			if writeVerb.MatchString(name) {
				t.Errorf("tool %q carries a watchdog write verb — a tool named after a watchdog write is a second door onto the write the socket alone carries (LC18, OD10)", name)
			}
		}
	})

	// ── Negative 4 — the records catalog: no spec at all twined to the
	// three writes, and NO Mutates spec twined to any watchdog method — the
	// read twins (T10) are the only watchdog-family specs an agent may file,
	// and they are not mutations (OD10).
	t.Run("records-catalog", func(t *testing.T) {
		for _, spec := range records.Catalog() {
			for _, m := range watchdogWrites {
				if spec.Method == string(m) {
					t.Errorf("spec %q twines the person write %q — the records catalog is an agent's surface and a watchdog write is a person's act (LC18)", spec.Name, m)
				}
			}
			if spec.Mutates {
				for _, m := range watchdogAll {
					if spec.Method == string(m) {
						t.Errorf("spec %q is a Mutates record twined to watchdog method %q — the only watchdog-family specs an agent may file are the reads (OD10)", spec.Name, m)
					}
				}
			}
		}
	})

	// ── Negative 5 — by text: the three writes' wire literals and consts
	// appear in NO non-test file under the four packages that own the
	// non-human doors. A projection that names watchdog.set, even in a
	// comment, is the projection the rule forbids.
	t.Run("text-grep", func(t *testing.T) {
		var totalWalked int
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			offenders, walked := walkWatchdogLiterals(filepath.Join("..", "..", dir))
			totalWalked += walked
			for _, o := range offenders {
				t.Errorf("%s names a watchdog write — it must not reach a non-human door (LC18)", o)
			}
		}
		// ── Negative 6 — the walk saw enough to mean something. Measured
		// 2026-11-01 at T9: 40 non-test .go files across the four
		// directories (the same floor T8's guard pins — the anti-vacuity
		// count, a tree that loses a guarded file fails it).
		if totalWalked < 40 {
			t.Fatalf("the text-grep walk saw %d non-test files across the four guarded directories, want ≥ 40 — the guard would be checking almost nothing", totalWalked)
		}
	})
}

// narrowProjectingWatchdogWrites returns the names of the narrow actions
// whose SocketMethod projects one of the three person writes — the guard's
// predicate, a function over action lists (not over narrow.Actions())
// precisely so the canary can feed it a poisoned table the real one never
// carries (the loop-control guard's shape).
func narrowProjectingWatchdogWrites(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range watchdogWrites {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

// recordsSpecTwinedToWatchdogWrite reports whether a records spec twines one
// of the three person writes — the catalog's predicate, over specs, so the
// canary can feed it a fake spec the real catalog never carries.
func recordsSpecTwinedToWatchdogWrite(spec records.Spec) bool {
	for _, m := range watchdogWrites {
		if spec.Method == string(m) {
			return true
		}
	}
	return false
}

// walkWatchdogLiterals walks one guarded directory's non-test .go files and
// returns the offending "path: literal" pairs plus the file count. The
// function-over-trees shape (not over a fixed dir list) is what lets the
// canary feed it a synthetic offender the real tree never carries.
func walkWatchdogLiterals(root string) (offenders []string, walked int) {
	forbidden := []string{
		"watchdog.set", "watchdog.setEnabled", "watchdog.remove",
		"MethodWatchdogSet", "MethodWatchdogSetEnabled", "MethodWatchdogRemove",
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs hold no Go code we own
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		text := string(b)
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				offenders = append(offenders, path+": "+f)
			}
		}
		return nil
	})
	return offenders, walked
}

// TestWatchdogGuardsHaveTeeth is the canary (the family's meta-test): each
// negative predicate fed a synthetic violation must report it, and must not
// report a clean input. A guard that cannot fail is a green light, not a
// guard — the T5/T8 tests assert the same about theirs.
func TestWatchdogGuardsHaveTeeth(t *testing.T) {
	// Canary 1 — a fake narrow action projecting watchdog.set (exactly what a
	// slice would add "for symmetry", the one thing the Do-NOT list forbids)
	// must be flagged; the real table must not be.
	poisoned := []narrow.Action{
		{Name: "aeon_watchdog_set", SocketMethod: protocol.MethodWatchdogSet, Kind: narrow.KindRead},
	}
	if got := narrowProjectingWatchdogWrites(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting watchdog.set was not flagged — the narrow guard cannot fail, so it is not a guard")
	}
	if got := narrowProjectingWatchdogWrites(narrow.Actions()); len(got) != 0 {
		t.Errorf("the real narrow table was flagged: %v", got)
	}

	// Canary 2 — a fake records Spec twined to the watchdog.remove write must
	// be flagged; a clean watchdog read spec must not.
	if !recordsSpecTwinedToWatchdogWrite(records.Spec{Name: "watchdog_remove", Method: "watchdog.remove", Mutates: true}) {
		t.Fatal("a records spec twined to watchdog.remove was not flagged — the catalog guard cannot fail")
	}
	if recordsSpecTwinedToWatchdogWrite(records.Spec{Name: "watchdog_list", Method: "watchdog.list"}) {
		t.Fatal("a clean watchdog read spec was flagged — the predicate fires on nothing")
	}

	// Canary 3 — a temp non-test .go file under a fake tools/ directory
	// naming "watchdog.set" must be flagged by the text walk; a clean file
	// must not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poison.go"),
		[]byte("package tools\n\nvar _ = \"watchdog.set\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkWatchdogLiterals(dir); len(off) == 0 {
		t.Fatal("a non-test file naming watchdog.set was not flagged — the text walk cannot fail")
	}
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "clean.go"),
		[]byte("package tools\n\nvar _ = \"watchdog.list\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkWatchdogLiterals(clean); len(off) != 0 {
		t.Errorf("a clean file was flagged: %v", off)
	}
}

// watchdogRestartPatterns are the calls a watchdog file may never make
// (behaviour 8: NO automatic retry exists). An advance (the loop's
// forward motion), a runner start (a fresh dispatch), a retry-handler-block
// (a re-dispatch of the same position), or a resume (a blocked loop moving
// again) from the watchdog would be the auto-retry the slice's whole
// argument forbids: the watchdog parks, blocks or notifies, and a blocked
// loop stays blocked until a PERSON moves it. The patterns are call-shaped
// (a receiver dot and a capital), so the lowercase words in these files'
// comments — "an advance, a start or a resume" — cannot be read as calls.
var watchdogRestartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\.Advance[A-Z]\w*\(`),    // loop.AdvanceLoop / AdvanceLoopParallel
	regexp.MustCompile(`Runner\.Start\(`),        // loop.Runner.Start — a fresh dispatch
	regexp.MustCompile(`RetryHandlerBlock\w*\(`), // the retry-handler-block re-dispatch
	regexp.MustCompile(`\.Resume[A-Z]\w*\(`),     // loop.Resume / the daemon's resume doors
}

// watchdogRestartCall reports whether a line of the watchdog service carries
// one of the restart patterns — the grep's predicate, over LINES (not over
// the real files) precisely so the canary can feed it a poisoned line the
// real tree never carries.
func watchdogRestartCall(line string) *regexp.Regexp {
	for _, p := range watchdogRestartPatterns {
		if p.MatchString(line) {
			return p
		}
	}
	return nil
}

// TestWatchdogNeverRestarts is behaviour 8 made mechanical: the two watchdog
// service files (the ticker and the doors) carry no call to advance, start,
// retry-handler-block or resume. A watchdog that retried would be a second
// dispatcher — the loop's progress would have two drivers, and the block the
// watchdog took would be undone by the same daemon that made it.
func TestWatchdogNeverRestarts(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, rel := range []string{
		"internal/daemon/watchdog.go",
		"internal/daemon/watchdog_doors.go",
	} {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if p := watchdogRestartCall(line); p != nil {
				t.Errorf("%s:%d: %s matches %s — the watchdog never advances, starts, retries or resumes a loop (OD8)",
					rel, i+1, strings.TrimSpace(line), p)
			}
		}
	}

	// The canary: a poisoned line must be flagged, a clean one not.
	if p := watchdogRestartCall("\tparked := s.d.store.ParkLoopStopped(id, kind, actorID, reason) // s.runner.AdvanceLoop(ctx)"); p == nil {
		t.Fatal("an AdvanceLoop call was not flagged — the never-restarts grep cannot fail, so it is not a guard")
	}
	if p := watchdogRestartCall("\t// it never calls an advance, a start or a resume"); p != nil {
		t.Fatalf("a lowercase comment was flagged as a restart: %v", p)
	}
}

// TestOnlyTheWatchdogServiceCallsBlockStuck is the call-graph half of the
// CAS discipline (LC2, carried from T5's TestOnlyTheControlServiceCallsTheCAS,
// which pins ParkLoopStopped / ResumeLoop / CancelLoop and already admits
// internal/daemon/watchdog.go): BlockLoopStuck — the unattended move, a
// compare-and-set of a running loop to blocked with kind stuck — is callable
// only from the watchdog service. A block from anywhere else is a second
// door onto a transition that is supposed to have one, and a loop blocked by
// something other than the watchdog's rule would read as a stuck that is not.
func TestOnlyTheWatchdogServiceCallsBlockStuck(t *testing.T) {
	pattern := regexp.MustCompile(`\.BlockLoopStuck\(`)
	root := filepath.Join("..", "..")
	var walked int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() || strings.HasSuffix(path, "_test.go") || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
			return nil
		}
		if rel != "internal/daemon/watchdog.go" {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			for _, line := range strings.Split(string(b), "\n") {
				if pattern.MatchString(line) {
					t.Errorf("%s calls BlockLoopStuck outside the watchdog service — the stuck block is the watchdog's move and nobody else's (LC2)", rel)
				}
			}
		}
		walked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("walked %d non-test files under internal/ and cmd/, want ≥ 50 — the call-graph guard would be checking almost nothing", walked)
	}
}

// TestWatchdogAmendmentLeavesThePriorGuardsByteIdentical re-pins T8's guard
// file set (plus T8's own amendment, the prior guard from T9's vantage): the
// T9 amendment adds this one test file and changes no existing guard body,
// and this makes that a fact the build checks rather than a promise the
// review remembers. T8's TestBudgetAmendmentLeavesThePriorGuardsByteIdentical
// enforces the same set against the pre-T8 tree; this enforces it against
// T9, and the digest of budget_amend_test.go itself is the pin T8's
// amendment never had (it did not yet exist).
func TestWatchdogAmendmentLeavesThePriorGuardsByteIdentical(t *testing.T) {
	pinned := map[string]string{
		"narrow_degradation_test.go": "440b8fc1c6365b5b823bdebf5cbfa52796138735681db4e0ddb7d83578602ee1",
		"narrow_http_test.go":        "455be5aa2cf9d64e455fc0e6209ba83d3336298c13d670265ab0a30b835d4a75",
		"seed_guard_test.go":         "2f02c98b52b20fe0b1fadef3d2b7a85c2c062e2361a64efe4f42e69a65c436ee",
		"settings_guard_test.go":     "fc8a4e36f775db10aec884e65e854f51cc3d909c3e4941004747482408dfb10d",
		"skills_guard_test.go":       "d6a9a485537417ea5aeb39d1380ad7bb474961afad9f1ea2ed3410b557088b8d",
		"loop_control_amend_test.go": "13920fc523c793b75fc2c35ca9523917ea3a830ff11f62874c22ec67edc06b8d",// // re-pinned 2026-11-04 (S60 T7): TestOnlyTheControlServiceCallsTheCAS names the merge queue's two files as callers of the daemon's control-service door (d.CancelLoop/d.ResumeLoop), the one deliberate edit; prior re-pin: 2026-11-03 (S49 T5) — earlier: re-pinned 2026-11-03 (S49 T5): the cascade from the cited records_guard_test.go fix-forward (see that file's re-pin comment and S49's T5 Notes); the amendments this test owns remain unedited
		// 2026-11-02 merge reconciliation: re-pinned after the T8 file's
		// own merge edits (the seam rename to RemoveBudget and the S45/S46
		// named exceptions) — the T9 pin is the record, and the record
		// moves only when the file does, on the record.
		// 2026-11-02 S62 T8: re-pinned again — the export family's read twin
		// (export_sets) joined the file's own namedExceptions set, the same
		// data-only edit the S45/S46 entries above are (its "set" is a
		// record SET, not a budget write); no guard body moved.
		"budget_amend_test.go": "0777827e107af856293478473923d97989561c44f6017b3494e840b9b17b4632",// re-pinned 2026-11-03 (S49 T5): the cascade from the cited records_guard_test.go fix-forward (see that file's re-pin comment and S49's T5 Notes); the amendments this test owns remain unedited // re-pinned 2026-11-04 (S60 T7): cascade of the loop_control_amend_test.go re-pin recorded in budget_amend_test.go
		filepath.Join("..", "..", "internal", "narrow", "records_guard_test.go"): "34e9df0d456a3e0119d4e4b13f2a6d2f480cbd9696a555ade2b86237af5f05aa", // re-pinned 2026-11-03 (S49 T5): this pin guards the guard, not its silence — the file had been main-RED since S48 T10's catalog twin (a8759842) met the 2026-10-27 aeon_blueprint_validate action, and T5's fix-forward widened allowedOverlap on the loop.list precedent (blame and reasoning in S49's T5 Notes); the amendments this test owns remain unedited
		filepath.Join("..", "..", "internal", "narrow", "actions_test.go"):       "0f060df721d36921520cdfcbfec24944a461f03d0df239b172c1bcb1e65c47d8",
		filepath.Join("..", "..", "internal", "narrow", "questions_test.go"):     "f789cf18a11ccaa6066d4a9c4128235f7db72f7a1b9dc081cf6a4c6a17e22fa3",
	}
	for path, want := range pinned {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed since the T8 pins (want %s, got %s) — the T9 amendment edits no existing guard body", path, want, got)
		}
	}
}
