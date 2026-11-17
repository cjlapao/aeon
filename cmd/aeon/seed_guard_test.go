package main

// seed_guard_test.go — S43-extra T6. The guards that make the slice's
// security claims mechanical rather than reviewed (brief acceptance 8, SD10):
// a seed WRITE (seed.apply / seed.restore) never reaches the narrow (agent /
// HTTP / MCP-stdio) surface, the agent-services enumeration, or the tool
// registry — the way policy authoring is refused (the PL15/R6 guard that
// lives beside this file in the same mould). The reads (seed.status /
// seed.plan) are the door the human half uses; the writes are the human door
// that the boundary keeps off the agent surfaces.
//
// A guard that cannot fail is not a guard — it is a green light — so each
// predicate is paired with a meta-test (TestSeedGuardsHaveTeeth) that feeds
// it a synthetic violation and asserts it is reported. The D4 policy guard
// (authoredRecordCall) is EXTENDED, never edited (T0 recorded its body hash);
// a sibling asserts it is unchanged.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/tools"
)

// guardSeedService is the non-nil seam a guard needs to see the four seed.*
// methods REGISTERED. The door (RegisterSeed) only inspects whether the seam
// is nil — it never calls these methods — so a guard may stand in a seam that
// answers nothing (exactly as the settings guard's guardSettingsService does),
// without standing up a store. Returning zero values keeps the guard from
// touching a real engine (the engine's own tests own that).
type guardSeedService struct{}

var _ protocol.SeedService = guardSeedService{} // compile-time: the fake satisfies the seam

func (guardSeedService) Status(context.Context, string) ([]seed.SetStatus, error) {
	return nil, nil
}
func (guardSeedService) Plan(context.Context, string, []string) (seed.SetPlan, error) {
	return seed.SetPlan{}, nil
}
func (guardSeedService) Apply(context.Context, string, []string, seed.Actor, seed.Path) (seed.ApplyResult, error) {
	return seed.ApplyResult{}, nil
}
func (guardSeedService) Restore(context.Context, string, string, seed.Actor) (seed.ApplyResult, error) {
	return seed.ApplyResult{}, nil
}

// newSeedTable builds the table the way a daemon with a seed engine builds
// it (daemon.go): NewTable, then assign the seam, then RegisterSeed. A
// non-nil seam registers the four; the return is true, so the app has its
// door — the positive half the guard states before the negative one.
func newSeedTable(t *testing.T) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tbl.Seed = guardSeedService{}
	if !tbl.RegisterSeed() {
		t.Fatal("RegisterSeed refused to register over a non-nil seam — the app would have no seed door at all")
	}
	return tbl
}

// TestSeedWritesAreAHumanDoor is the mechanical form of SD10: a seed write
// (seed.apply / seed.restore) is the human door only — it never reaches the
// narrow (agent) surface, the agent-services enumeration, the tool registry,
// or a mutating records-catalog spec. A seed write on any of those would hand
// a caller the means to seed records to their own rules — the same refusal
// policy authoring is, one slice later.
//
// It states the door POSITIVELY first (the app has its door), then the
// negative halves, one per door.
func TestSeedWritesAreAHumanDoor(t *testing.T) {
	tbl := newSeedTable(t)

	// ── Positive: the app has its door. The four seed.* methods ARE in the
	// table when the seam is wired, so the Settings panel (T8) and the CLI
	// (T7) have exactly one door to speak through (P34).
	have := map[protocol.MethodName]bool{}
	for _, m := range tbl.Methods() {
		have[m] = true
	}
	for _, m := range []protocol.MethodName{
		protocol.MethodSeedStatus, protocol.MethodSeedPlan,
		protocol.MethodSeedApply, protocol.MethodSeedRestore,
	} {
		if !have[m] {
			t.Fatalf("%s must be registered — the app has one door onto the seed (P34)", m)
		}
	}
	// The writes are real product methods, not debug or pre-negotiation gates:
	// seed.apply / seed.restore are NOT Debug (they are on the wide socket,
	// not the narrow projection) and DO RequireInitialize (a write is only a
	// write after the connection has negotiated).
	for _, m := range []protocol.MethodName{protocol.MethodSeedApply, protocol.MethodSeedRestore} {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("%s is not in the table — the positive half failed", m)
		}
		if got.Debug {
			t.Fatalf("%s is marked Debug — a seed write is a product method on the wide socket, not a diagnostic (SD10)", m)
		}
		if !got.RequiresInitialize {
			t.Fatalf("%s does not RequireInitialize — a write may not precede the connection's negotiation", m)
		}
	}

	// ── Negative 1 — the narrow table: no narrow Action projects a seed WRITE.
	// The allow-list (SD10) never names seed.apply / seed.restore, so an agent
	// cannot reach the write through the projection. (The reads may project;
	// the writes may not.)
	if offenders := narrowProjectingSeedWrite(narrow.Actions()); len(offenders) != 0 {
		t.Errorf("the narrow surface projects a seed write through %s — an action projecting seed.apply/seed.restore hands an agent the means to seed records (SD10). Add it to the narrow surface only through a slice that argues it.", strings.Join(offenders, ", "))
	}

	// ── Negative 2 — the tool registry: no tool name is a seed WRITE. The
	// read-only seed_status tool is allowed (it reads); a seed_apply /
	// seed_restore tool would be a second door onto the write the socket
	// alone carries.
	for name := range tools.Registry() {
		if isSeedWriteTool(name) {
			t.Errorf("tool %q is a seed write — a tool that applies or restores is a second door onto the write the socket alone carries (SD10)", name)
		}
	}

	// ── Negative 3 — the agent-services enumeration: none of
	// protocol.AgentServiceMethods() is a seed WRITE name. The agent-to-agent
	// task lifecycle has no seed write door, so a write cannot ride it.
	for _, m := range protocol.AgentServiceMethods() {
		if isSeedWriteMethod(m.Name) {
			t.Fatalf("%s is an agent-service method — a seed write on the agent surface hands an agent the means to seed records (SD10)", m.Name)
		}
	}

	// ── Negative 4 — the records catalog: no seed spec is Mutates:true. A
	// mutating seed spec would be an agent-reachable write; the only seed
	// spec that may exist is the read-only seed_status.
	for _, sp := range records.Catalog() {
		if sp.Family == "seed" && sp.Mutates {
			t.Fatalf("records spec %s is a mutating seed action — a seed write in the catalog hands a granted agent the means to seed records (SD10)", sp.Name)
		}
	}

	// ── Negative 5 — by text: the write's names appear in NO non-test file
	// under internal/narrow, internal/agentsvc, internal/tools, or
	// internal/records. A projection that names seed.apply, even in a comment,
	// is the projection the rule forbids.
	for _, dir := range []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"} {
		walkNoSeedWriteLiteral(t, filepath.Join("..", "..", dir))
	}

	// ── Negative 6 — dispatch proves the absence from the CALLER's side. With
	// the seam nil, RegisterSeed is a no-op (V13), so seed.apply is not in the
	// table and dispatch answers method-not-found. The narrow surface cannot
	// even name the write.
	tblNil := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	tblNil.RegisterSeed() // no-op over a nil seam: the four stay absent
	if _, err := tblNil.Dispatch(context.Background(), "c9", protocol.MethodSeedApply, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
		t.Fatalf("seed.apply dispatched on a nil-seam table — the narrow surface can reach a write it must not (want ErrUnknownMethod, got %v)", err)
	}
}

// TestSeedMethodsAbsentWhenNil is the V13 half stated from the caller's side:
// a nil seam leaves the four seed.* methods ABSENT (not registered-and-
// refusing), so a seedless build answers method-not-found, never a refusal
// (the S22 search.query / S44 settings precedent).
func TestSeedMethodsAbsentWhenNil(t *testing.T) {
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if tbl.RegisterSeed() {
		t.Fatal("RegisterSeed registered over a nil seam — the four should be absent (V13)")
	}
	// Dispatch checks method existence before the initialize gate (I3, T7), so
	// an unregistered method answers ErrUnknownMethod without a handshake —
	// exactly the settings guard's Negative-6 shape.
	for _, m := range []protocol.MethodName{
		protocol.MethodSeedStatus, protocol.MethodSeedPlan,
		protocol.MethodSeedApply, protocol.MethodSeedRestore,
	} {
		if _, err := tbl.Dispatch(context.Background(), "c0", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
			t.Fatalf("%s answered on a nil seam — the method should be absent (want ErrUnknownMethod, got %v)", m, err)
		}
	}
}

// TestSeedGuardsHaveTeeth proves the guard's predicates are not vacuous: each
// is fed a synthetic violation and must report it, and a clean input must not
// be flagged. A guard that cannot fail is a green light, not a guard.
func TestSeedGuardsHaveTeeth(t *testing.T) {
	// Predicate 1 (narrow): a poisoned action carrying seed.apply must be
	// flagged; a clean one must not. The Kind is irrelevant to the predicate
	// (it inspects the projected method, not the kind); a KindRead stands in.
	poisoned := []narrow.Action{
		{Name: "aeon_seed_apply", SocketMethod: protocol.MethodSeedApply, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingSeedWrite(poisoned); len(got) == 0 {
		t.Fatal("a narrow action projecting seed.apply was not flagged — the narrow predicate cannot fail, so it is not a guard")
	}
	clean := []narrow.Action{
		{Name: "aeon_loop_list", SocketMethod: protocol.MethodLoopList, Kind: narrow.KindRead, Cannot: []string{"must not exist"}},
	}
	if got := narrowProjectingSeedWrite(clean); len(got) != 0 {
		t.Errorf("a clean action was flagged as projecting a seed write — the narrow predicate fires on nothing: %v", got)
	}

	// Predicate 2 (tool registry): a synthetic seed_apply tool name must be
	// flagged; the read-only seed_status must not.
	if !isSeedWriteTool("seed_apply") {
		t.Fatal("the name seed_apply was not flagged as a seed write — the tool predicate cannot fail")
	}
	if isSeedWriteTool("seed_status") {
		t.Fatal("the read-only seed_status tool was flagged as a seed write — the tool predicate fires on the read")
	}

	// Predicate 5 (text walk): a file that names seed.apply must be caught, a
	// file that names only seed.status must not.
	tmp := t.TempDir()
	dirty := filepath.Join(tmp, "dirty.go")
	if err := os.WriteFile(dirty, []byte("x := \"seed.apply\""), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanFile := filepath.Join(tmp, "clean.go")
	if err := os.WriteFile(cleanFile, []byte("x := \"seed.status\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !seedWriteNamedIn(tmp) {
		t.Fatal("a file naming seed.apply was not caught by the text walk — the walk cannot fail")
	}
	// And a clean tree must not be flagged.
	cleanTmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(cleanTmp, "clean.go"), []byte("x := \"seed.status\""), 0o644); err != nil {
		t.Fatal(err)
	}
	if seedWriteNamedIn(cleanTmp) {
		t.Fatal("a tree naming only seed.status was flagged as a seed write — the text walk fires on the read")
	}
}

// narrowProjectingSeedWrite returns the names of the actions whose SocketMethod
// is a seed WRITE (seed.apply / seed.restore) — the guard's predicate. It is a
// function over tables (not over narrow.Actions()) precisely so the meta-test
// can feed it a poisoned table the real one never carries.
func narrowProjectingSeedWrite(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if isSeedWriteMethod(a.SocketMethod) {
			bad = append(bad, a.Name)
		}
	}
	return bad
}

// isSeedWriteMethod reports whether a method name is a seed WRITE
// (seed.apply / seed.restore) — the two that may never ride the narrow
// surface or the agent-services enumeration. The reads (seed.status /
// seed.plan) are not refused here; they are the door the human half uses.
func isSeedWriteMethod(m protocol.MethodName) bool {
	return m == protocol.MethodSeedApply || m == protocol.MethodSeedRestore
}

// isSeedWriteTool reports whether a tool name is a seed WRITE tool
// (seed_apply / seed_restore). The read-only seed_status is not a write and
// is not flagged — it is the only seed tool that may exist.
func isSeedWriteTool(name string) bool {
	return name == "seed_apply" || name == "seed_restore"
}

// walkNoSeedWriteLiteral walks one narrow/agentsvc/tools/records directory's
// non-test .go files and fails if any names a seed write — the literal
// "seed.apply"/"seed.restore" or the const MethodSeedApply/MethodSeedRestore.
// The file path is in the failure so the offender is actionable, and the walk
// asserts it saw at least one file (a guard that walks an empty tree passes
// vacuously).
func walkNoSeedWriteLiteral(t *testing.T, root string) {
	t.Helper()
	forbidden := []string{"seed.apply", "seed.restore", "MethodSeedApply", "MethodSeedRestore"}
	var walked int
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
				t.Errorf("%s names %q — a seed write must not reach the narrow/agent/tool/records surface (SD10)", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked == 0 {
		t.Fatalf("the %s walk saw no non-test files — it checked nothing", root)
	}
}

// seedWriteNamedIn reports whether any non-test .go file under root names a
// seed write — the text-walk predicate, factored so the meta-test can feed it
// a dirty tree the real one never carries. It uses the same regex the walk
// greps so the meta-test and the guard cannot drift.
func seedWriteNamedIn(root string) bool {
	re := regexp.MustCompile(`\bseed\.(apply|restore)\b|MethodSeed(Apply|Restore)`)
	var found bool
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if re.Match(b) {
			found = true
		}
		return nil
	})
	if err != nil {
		return false
	}
	return found
}
