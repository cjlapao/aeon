package main

// loop_control_amend_test.go — S55 T5 (P4). The guard amendment for the
// loop-control plane, in the mould of the PL15/R6/CS9 guards already in this
// directory (policy_test.go, settings_guard_test.go): the slice's security
// claim made mechanical rather than reviewed.
//
// The claim: stop, resume, cancel and the stop-request desk are socket
// methods on the daemon's 0600 + peer-credential surface (LC5, P4 shape) and
// are PROBABLY nothing — no, provably absent from every non-human door:
// the narrow (agent / HTTP / MCP-stdio) surface, the agent-services
// enumeration, the tool registry, the records catalog, and by text in the
// four packages that own those doors. An agent may only record a stop
// REQUEST (the inert row; T10 files its tool), never act.
//
// Positive half first, as the family's style demands: the app has its door
// (TestLoopControlIsAHumanDoor), and the guards each have a canary that
// proves they can FAIL (TestLoopControlGuardsHaveTeeth) — a guard that
// cannot fail is not a guard, it is a green light.

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

// guardLoopControlService is the non-nil seam a guard needs to see the seven
// loop-control methods REGISTERED. The door (RegisterLoopControl) only
// inspects whether the seam is nil — it never calls these methods — so a
// guard may stand in a seam that answers nothing (the settings precedent,
// settings_guard_test.go), without standing up a store.
type guardLoopControlService struct{}

var _ protocol.LoopControlService = guardLoopControlService{} // compile-time: the fake satisfies the seam

func (guardLoopControlService) Stop(context.Context, protocol.Actor, string, string) (protocol.LoopStopResult, *protocol.ControlRefusal, error) {
	return protocol.LoopStopResult{}, nil, nil
}
func (guardLoopControlService) Resume(context.Context, protocol.Actor, string, string) (protocol.LoopResumeResult, *protocol.ControlRefusal, error) {
	return protocol.LoopResumeResult{}, nil, nil
}
func (guardLoopControlService) Cancel(context.Context, protocol.Actor, string, string) (protocol.LoopCancelResult, *protocol.ControlRefusal, error) {
	return protocol.LoopCancelResult{}, nil, nil
}
func (guardLoopControlService) Control(context.Context, string) (protocol.LoopControlResult, error) {
	return protocol.LoopControlResult{}, nil
}
func (guardLoopControlService) RequestStop(context.Context, string, string) (protocol.RequestStopResult, *protocol.ControlRefusal, error) {
	return protocol.RequestStopResult{}, nil, nil
}
func (guardLoopControlService) Requests(context.Context, string, string) ([]protocol.ControlRequestView, error) {
	return nil, nil
}
func (guardLoopControlService) Dismiss(context.Context, protocol.Actor, int64) (protocol.ControlRequestView, *protocol.ControlRefusal, error) {
	return protocol.ControlRequestView{}, nil, nil
}

// newControlTable builds the table the way a daemon with a store builds it
// (daemon.go): NewTable, assign the seam, RegisterLoopControl. A non-nil seam
// registers the seven; the return is true, so the app has its door — the
// positive half the guards state before the negative one.
func newControlTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.LoopControl = guardLoopControlService{}
	if !tbl.RegisterLoopControl() {
		t.Fatal("RegisterLoopControl refused to register over a non-nil seam — the app would have no control door at all")
	}
	return tbl
}

// The four PERSON writes (LC5) — the set a non-human door must never carry —
// and the seven-plane names (the four writes, the control read, the request
// desk's socket twin and its two reads).
var (
	controlWrites = []protocol.MethodName{
		protocol.MethodLoopStop, protocol.MethodLoopResume,
		protocol.MethodLoopCancel, protocol.MethodLoopControlDismiss,
	}
	controlReads = []protocol.MethodName{
		protocol.MethodLoopControl, protocol.MethodLoopControlRequests,
	}
	controlAll = func() []protocol.MethodName {
		out := append([]protocol.MethodName{}, controlWrites...)
		out = append(out, protocol.MethodLoopRequestStop)
		return append(out, controlReads...)
	}()
)

// TestLoopControlIsAHumanDoor is the positive half (P4): the exact write set
// is registered, each RequiresInitialize, none Debug; the read set and the
// request twin are listed separately; and with the seam nil the seven stay
// ABSENT (V13 — a storeless daemon has no control surface, not a refusing
// one), so the narrow projection cannot even name them.
func TestLoopControlIsAHumanDoor(t *testing.T) {
	tbl := newControlTable(t)
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// The exact write set: registered, RequiresInitialize, none Debug.
	for _, m := range controlWrites {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the control plane (P34)", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a control write may not precede the connection's negotiation", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a control write is a product method on the wide socket, not a diagnostic", m)
		}
	}

	// The read set and the request twin, listed separately: registered,
	// RequiresInitialize (a person may speak the desk), none Debug.
	for _, m := range append(append([]protocol.MethodName{}, controlReads...), protocol.MethodLoopRequestStop) {
		if !have[m] {
			t.Fatalf("%s must be registered — the read door is missing", m)
		}
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table", m)
		}
		if !got.RequiresInitialize || got.Debug {
			t.Fatalf("%s is RequiresInitialize=%v Debug=%v, want true/nil", m, got.RequiresInitialize, got.Debug)
		}
	}

	// Nil seam: the seven stay ABSENT (V13), and dispatch answers
	// method-not-found — the absence from the CALLER's side (the settings
	// guard's negative 5, carried).
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterLoopControl() // no-op over a nil seam: the seven stay absent
	for _, m := range controlAll {
		if _, err := tblNil.Dispatch(context.Background(), "c1", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s dispatched on a nil-seam table — the narrow surface can reach a door it must not (want ErrUnknownMethod, got %v)", m, err)
		}
	}
}

// TestLoopControlNeverReachesANonHumanDoor is the negative half, one subtest
// per door: the narrow surface (its table AND its own account of itself), the
// agent-services enumeration, the tool registry, the records catalog, the
// text of the four packages that own those doors, and the walk's
// non-vacuity. A control write on any of them would hand a non-person the
// means to steer a loop mid-flight — the same refusal as policy authoring
// (PL15) and settings writes (CS9), one plane over.
func TestLoopControlNeverReachesANonHumanDoor(t *testing.T) {
	// ── Negative 1 — the narrow table and the boundary statement: no
	// action projects one of the four writes, and no loop.* method reaches
	// the surface beyond S19's list (loop.list, loop.show, loop.start) —
	// the reads a CI poller needs, nothing that moves a loop.
	t.Run("narrow", func(t *testing.T) {
		s19Loop := map[string]bool{"loop.list": true, "loop.show": true, "loop.start": true}
		for _, a := range narrow.Actions() {
			for _, m := range controlWrites {
				if a.SocketMethod == m {
					t.Errorf("action %q projects the person write %q — an agent reaching a control write steers a loop mid-flight (LC5/R6)", a.Name, m)
				}
			}
			if sm := string(a.SocketMethod); strings.HasPrefix(sm, "loop.") && !s19Loop[sm] {
				t.Errorf("action %q projects loop.* method %q — S19's narrow surface names loop.list/loop.show/loop.start and nothing else loop-shaped (R6)", a.Name, sm)
			}
		}
		// The boundary statement is the surface's account of its ABSENCE:
		// it must not name a control door it does not have.
		s := narrow.BoundaryStatement()
		for _, m := range controlWrites {
			if strings.Contains(s, string(m)) {
				t.Errorf("the boundary statement names %q — the statement is the surface's account of what is ABSENT, and a control write is not one of its methods", m)
			}
		}
	})

	// ── Negative 2 — the agent-services enumeration: the agent-to-agent
	// task lifecycle (agent.*) has no control door, so a control write
	// cannot ride it either.
	t.Run("agent-services", func(t *testing.T) {
		for _, m := range protocol.AgentServiceMethods() {
			for _, c := range controlAll {
				if m.Name == c {
					t.Fatalf("%s is an agent-service method — an agent reaching the control plane steers loops with no person in the room (LC5)", m.Name)
				}
			}
		}
	})

	// ── Negative 3 — the tool registry: no tool name matches
	// /stop|cancel|resume|raise|dismiss/ except the NAMED read/request tools
	// T10 will register (loop_control_state, loop_request_stop — the request
	// is an agent act and its only one; the state read is the read). A tool
	// named after a control verb would be a second door onto the write the
	// socket alone carries.
	t.Run("tools-registry", func(t *testing.T) {
		controlVerb := regexp.MustCompile(`stop|cancel|resume|raise|dismiss`)
		namedExceptions := map[string]bool{
			"loop_control_state": true, // T10: the read
			"loop_request_stop":  true, // T10: the inert request (the only agent act)
		}
		for name := range tools.Registry() {
			if namedExceptions[name] {
				continue
			}
			if controlVerb.MatchString(name) {
				t.Errorf("tool %q names a control verb — a tool named after control is a second door onto the write the socket alone carries (LC5)", name)
			}
		}
	})

	// ── Negative 4 — the records catalog: no spec at all twined to the four
	// writes, and the ONLY control-family mutation an agent may file is the
	// inert stop request (loop.requestStop, T10's tool) — a Mutates spec
	// twined to any other control method would be a write through a read
	// catalog, which is the projection the rule forbids.
	t.Run("records-catalog", func(t *testing.T) {
		for _, spec := range records.Catalog() {
			for _, m := range controlWrites {
				if spec.Method == string(m) {
					t.Errorf("spec %q twines the person write %q — the records catalog is an agent's surface and a control write is a person's act (LC5)", spec.Name, spec.Method)
				}
			}
			if spec.Mutates && spec.Method != string(protocol.MethodLoopRequestStop) {
				for _, m := range controlAll {
					if spec.Method == string(m) {
						t.Errorf("spec %q is a Mutates record twined to control method %q — the only control-family mutation an agent may file is the inert stop request (LC15)", spec.Name, spec.Method)
					}
				}
			}
		}
	})

	// ── Negative 5 — by text: the four writes' wire literals and consts
	// appear in NO non-test file under the four packages that own the
	// non-human doors. A projection that names loop.stop, even in a comment,
	// is the projection the rule forbids.
	t.Run("text-grep", func(t *testing.T) {
		var totalWalked int
		for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
			offenders, walked := walkControlLiterals(filepath.Join("..", "..", dir))
			totalWalked += walked
			for _, o := range offenders {
				t.Errorf("%s names a control write — it must not reach a non-human door (LC5)", o)
			}
		}
		// ── Negative 6 — the walk saw enough to mean something. Measured
		// 2026-11-01 at T5: 40 non-test .go files across the four
		// directories (the spec's 50 was an estimate; the guard pins the
		// MEASURED floor, which is what anti-vacuity is for — a tree that
		// loses a guarded file fails the count).
		if totalWalked < 40 {
			t.Fatalf("the text-grep walk saw %d non-test files across the four guarded directories, want ≥ 40 — the guard would be checking almost nothing", totalWalked)
		}
	})
}

// narrowProjectingControlWrites returns the names of the narrow actions whose
// SocketMethod projects one of the four person writes — the guard's predicate,
// a function over tables (not over narrow.Actions()) precisely so the canary
// can feed it a poisoned table the real one never carries (the settings
// guard's narrowProjectingSettings shape).
func narrowProjectingControlWrites(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range controlWrites {
			if a.SocketMethod == m {
				bad = append(bad, a.Name)
			}
		}
	}
	return bad
}

// recordsSpecTwinedToControlWrite reports whether a records spec twines one
// of the four person writes — the catalog's predicate, over specs, so the
// canary can feed it a fake spec the real catalog never carries.
func recordsSpecTwinedToControlWrite(spec records.Spec) bool {
	for _, m := range controlWrites {
		if spec.Method == string(m) {
			return true
		}
	}
	return false
}

// walkControlLiterals walks one guarded directory's non-test .go files and
// returns the offending "path: literal" pairs plus the file count. The
// function-over-trees shape (not over a fixed dir list) is what lets the
// canary feed it a synthetic offender the real tree never carries.
func walkControlLiterals(root string) (offenders []string, walked int) {
	forbidden := []string{
		"loop.stop", "loop.resume", "loop.cancel", "loop.control.dismiss",
		"MethodLoopStop", "MethodLoopResume", "MethodLoopCancel", "MethodLoopControlDismiss",
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

// TestLoopControlGuardsHaveTeeth is the canary (the family's meta-test):
// each negative predicate fed a synthetic violation must report it, and must
// not report a clean input. A guard that cannot fail is a green light, not a
// guard — the PL15/CS9 tests assert the same about theirs.
func TestLoopControlGuardsHaveTeeth(t *testing.T) {
	// Canary 1 — a fake narrow action projecting loop.cancel (exactly what a
	// slice would add "for symmetry", the one thing the Do-NOT list forbids)
	// must be flagged; the real table must not be.
	poisoned := []narrow.Action{
		{Name: "aeon_loop_cancel", SocketMethod: protocol.MethodLoopCancel, Kind: narrow.KindRead},
	}
	if got := narrowProjectingControlWrites(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting loop.cancel was not flagged — the narrow guard cannot fail, so it is not a guard")
	}
	if got := narrowProjectingControlWrites(narrow.Actions()); len(got) != 0 {
		t.Errorf("the real narrow table was flagged: %v", got)
	}

	// Canary 2 — a fake records Spec twined to the loop.stop write must be
	// flagged; a clean loop read spec must not.
	if !recordsSpecTwinedToControlWrite(records.Spec{Name: "loop_stop", Method: "loop.stop", Mutates: true}) {
		t.Fatal("a records spec twined to loop.stop was not flagged — the catalog guard cannot fail")
	}
	if recordsSpecTwinedToControlWrite(records.Spec{Name: "loop_list", Method: "loop.list"}) {
		t.Fatal("a clean loop.read spec was flagged — the predicate fires on nothing")
	}

	// Canary 3 — a temp non-test .go file under a fake tools/ directory
	// naming "loop.stop" must be flagged by the text walk; a clean file must
	// not.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "poison.go"),
		[]byte("package tools\n\nvar _ = \"loop.stop\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	offenders, _ := walkControlLiterals(dir)
	if len(offenders) == 0 {
		t.Fatal("a non-test file naming loop.stop was not flagged — the text walk cannot fail")
	}
	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "clean.go"),
		[]byte("package tools\n\nvar _ = \"loop.list\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if off, _ := walkControlLiterals(clean); len(off) != 0 {
		t.Errorf("a clean file was flagged: %v", off)
	}
}

// TestOnlyTheControlServiceCallsTheCAS is the call-graph half (the spec's
// "only the control service calls the CAS"): ParkLoopStopped / ResumeLoop /
// CancelLoop — the store's compare-and-set transitions — may be called only
// from the control core (internal/loop/control.go), the daemon's control
// files (internal/daemon/loop_control*.go), the watchdog (T9, absent until
// it lands), the merge queue's two files (S60 T7, through the daemon's door) and tests. A CAS call anywhere else is a second door onto a
// transition that is supposed to have one.
func TestOnlyTheControlServiceCallsTheCAS(t *testing.T) {
	pattern := regexp.MustCompile(`\.(ParkLoopStopped|ResumeLoop|CancelLoop)\(`)
	allowed := func(rel string) bool {
		switch {
		case rel == "internal/loop/control.go":
			return true
		case strings.HasPrefix(rel, "internal/daemon/loop_control"):
			return true
		case rel == "internal/daemon/watchdog.go": // T9
			return true
		// S60 T7: the merge queue retires and resumes its catch-up loops
		// through the DAEMON's control-service door (d.CancelLoop / d.ResumeLoop
		// in loop_control*.go, with mqControlActor stamped on the transition) —
		// the one door this guard exists to protect, not a second one onto the
		// store's CAS. Named by file so any other caller is still flagged.
		case rel == "internal/daemon/mergequeue_loops.go", rel == "internal/daemon/mergequeue_reactor.go":
			return true
		}
		return false
	}
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
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		walked++
		for _, line := range strings.Split(string(b), "\n") {
			if pattern.MatchString(line) && !allowed(rel) {
				t.Errorf("%s calls a control CAS (%s) outside the control service — a transition with two doors (LC2–LC4)", rel, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("walked %d non-test files under internal/ and cmd/, want ≥ 50 — the call-graph guard would be checking almost nothing", walked)
	}
}

// TestOriginalNarrowGuardsAreByteIdentical pins the PRE-AMENDMENT guard files
// (the digests were recorded 2026-11-01 in T5.md's Notes, before T5 touched a
// file, because T0's spec line pointed at digests T0 never recorded). The P4
// amendment adds this new test file and appends words to
// ForbiddenOperationalControl — it does not edit an existing guard body, and
// this makes that a fact the build checks rather than a promise the review
// remembers.
func TestOriginalNarrowGuardsAreByteIdentical(t *testing.T) {
	pinned := map[string]string{
		"narrow_degradation_test.go": "440b8fc1c6365b5b823bdebf5cbfa52796138735681db4e0ddb7d83578602ee1",
		"narrow_http_test.go":        "455be5aa2cf9d64e455fc0e6209ba83d3336298c13d670265ab0a30b835d4a75",
		"seed_guard_test.go":         "2f02c98b52b20fe0b1fadef3d2b7a85c2c062e2361a64efe4f42e69a65c436ee",
		"settings_guard_test.go":     "fc8a4e36f775db10aec884e65e854f51cc3d909c3e4941004747482408dfb10d",
		"skills_guard_test.go":       "d6a9a485537417ea5aeb39d1380ad7bb474961afad9f1ea2ed3410b557088b8d",
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
			t.Errorf("%s changed since the pre-amendment pin (want %s, got %s) — the P4 amendment edits no existing guard body", path, want, got)
		}
	}
}

// TestOperationalControlStillAbsentFromNarrow re-asserts S19's permanence
// with the S55 verbs added to the synonym list: the enumeration is a WORD
// set (never a list of specific actions to exclude), and the loop-control
// plane's verbs are in it — the "fire" precedent, the word forbidden before
// the action that carries it exists. The boundary statement renders its
// ABSENT line from the same data, so a lost word is a lost claim a client
// would read as "restricted" rather than "absent".
func TestOperationalControlStillAbsentFromNarrow(t *testing.T) {
	have := map[string]bool{}
	for _, w := range narrow.ForbiddenOperationalControl {
		have[w] = true
	}
	// S19's permanent set plus S55's two (2026-11-01).
	for _, w := range []string{
		"stop", "kill", "pause", "resume", "retry", "answer", "abort", "cancel", "fire",
		"dismiss", "raise",
	} {
		if !have[w] {
			t.Errorf("ForbiddenOperationalControl lost %q — the enumeration is the machine; a lost word is a word a projection may carry (R6)", w)
		}
	}
	s := narrow.BoundaryStatement()
	for _, w := range []string{"stop", "resume", "cancel", "dismiss", "raise"} {
		if !strings.Contains(s, w) {
			t.Errorf("the boundary statement no longer enumerates %q — the ABSENT line renders from the list, never a hand-copied sentence", w)
		}
	}
}
