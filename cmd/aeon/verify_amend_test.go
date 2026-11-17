package main

// verify_amend_test.go — S57 T8: the gate amendment's guards, from the
// amendment's sibling (the autonomy slice's pattern, autonomy_guard_test.go):
// the surface table, the agent surfaces, and the write boundary are
// asserted from HERE, beside the amendment, not only from the packages
// they live in — a guard that lives only in the package it guards is a
// guard that gets edited out with the thing it guards.
//
// What is under test (T8.md, verbatim):
//
//  1. TestGateAuthoringIsAHumanDoor — the gate table holds EXACTLY the
//     nine write methods (verify.add/update/archive/restore/applyTemplate/
//     try, mergerule.set/clear, dod.mark) plus the reads T8 registered;
//     nothing else under the family prefixes; every entry is
//     RequiresInitialize and none Debug; with each service nil its family
//     is ABSENT and Dispatch answers ErrUnknownMethod (V13: absent, not
//     registered-and-refusing).
//  2. TestGateAuthoringNeverReachesTheNarrowOrAgentSurfaces — the nine
//     writes never project: narrow's Actions(), the agent services, the
//     tool registry, the records catalog (only the three named twins T9
//     may add may mutate the gate family), and a text grep of the four
//     agent-surface packages for the nine wire names and their Method
//     constants.
//  3. TestOnlyTheServicesCallTheGateWriteBoundary — the six store doors
//     (AddVerifyCommand / UpdateVerifyCommand / ArchiveVerifyCommand /
//     SetMergeRule / ClearMergeRule / MarkDoD) are crossed only from
//     internal/store, the three daemon services, the desk applier, and
//     cmd/aeon's direct door (T10).
//  4. TestGateGuardsHaveTeeth — each predicate above is run against a
//     poisoned input it must flag and a clean input it must not.
//
// The two PROPOSE-kind laws (T8's items 5 and 6 — the kinds are
// restrictive only, and inert until committed) live in the sibling
// verify_amend_propose_test.go, split by cohesion (SIZE1): this file is
// the SURFACE half (what no agent may reach), the sibling is the PROPOSE
// half (what an agent may file, and only what).
//
// No narrow/agentsvc/tools/records edits ride with this task (T8 DO-NOT
// §1); the assertions are all read-only against those surfaces, and the
// reserved names for T9's nine catalog entries are asserted the way the
// autonomy guard reserved T8's five tool names: the names may arrive,
// nothing else may.

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
	"github.com/cjlapao/aeon/internal/tools"
)

// ─── the gate family, as data ─────────────────────────────────────────────

// gateWriteMethods are THE nine write doors — the human door (VG17). An
// agent reaches a gate only through a person's commit (T8) or a mark that
// carries a passing run (T9's twins); there is no other path by design.
var gateWriteMethods = []protocol.MethodName{
	protocol.MethodVerifyAdd,
	protocol.MethodVerifyUpdate,
	protocol.MethodVerifyArchive,
	protocol.MethodVerifyRestore,
	protocol.MethodVerifyApplyTemplate,
	protocol.MethodVerifyTry,
	protocol.MethodMergeRuleSet,
	protocol.MethodMergeRuleClear,
	protocol.MethodDoDMark,
}

// gateReadMethods are the reads T8 registered on the same seams. They ride
// the same service, so they are gated and human-doored too — an agent
// reads gates through T9's tools, not through a read door it holds itself.
var gateReadMethods = []protocol.MethodName{
	protocol.MethodVerifyList,
	protocol.MethodVerifyShow,
	protocol.MethodVerifyRuns,
	protocol.MethodVerifyRunShow,
	protocol.MethodVerifyVocabulary,
	protocol.MethodVerifyTemplates,
	protocol.MethodMergeRuleGet,
	protocol.MethodMergeRulePreview,
	protocol.MethodMergeRuleChanges,
	protocol.MethodMergeRuleVocabulary,
	protocol.MethodMergeEvents,
	protocol.MethodMergeWatches,
	protocol.MethodDoDList,
}

func gateAllMethods() []protocol.MethodName {
	return append(append([]protocol.MethodName{}, gateWriteMethods...), gateReadMethods...)
}

var gateMethodSet = func() map[protocol.MethodName]bool {
	m := make(map[protocol.MethodName]bool, len(gateWriteMethods)+len(gateReadMethods))
	for _, mthd := range gateAllMethods() {
		m[mthd] = true
	}
	return m
}()

var gateWriteSet = func() map[protocol.MethodName]bool {
	m := make(map[protocol.MethodName]bool, len(gateWriteMethods))
	for _, mthd := range gateWriteMethods {
		m[mthd] = true
	}
	return m
}()

// gateWirePrefixes: every method name the gate family can stand under. The
// merge. pair (merge.events / merge.watches) is part of the mergerule
// seam — the merge rule's own audit and watch reads — and stands with it.
var gateWirePrefixes = []string{"verify.", "mergerule.", "dod.", "merge."}

func isGateMethod(m protocol.MethodName) bool { return gateMethodSet[m] }

func gatePrefixOf(m protocol.MethodName) (string, bool) {
	for _, p := range gateWirePrefixes {
		if strings.HasPrefix(string(m), p) {
			return p, true
		}
	}
	return "", false
}

// The families per service: which methods die when that service goes nil.
var gateFamilies = map[string][]protocol.MethodName{
	"verify": {
		protocol.MethodVerifyList, protocol.MethodVerifyShow, protocol.MethodVerifyRuns,
		protocol.MethodVerifyRunShow, protocol.MethodVerifyVocabulary, protocol.MethodVerifyTemplates,
		protocol.MethodVerifyAdd, protocol.MethodVerifyUpdate, protocol.MethodVerifyArchive,
		protocol.MethodVerifyRestore, protocol.MethodVerifyApplyTemplate, protocol.MethodVerifyTry,
	},
	"mergerule": {
		protocol.MethodMergeRuleGet, protocol.MethodMergeRulePreview, protocol.MethodMergeRuleChanges,
		protocol.MethodMergeRuleVocabulary, protocol.MethodMergeRuleSet, protocol.MethodMergeRuleClear,
		protocol.MethodMergeEvents, protocol.MethodMergeWatches,
	},
	"dod": {protocol.MethodDoDList, protocol.MethodDoDMark},
}

// gateWriteToolRe is T8's own regex: a tool name twinned to one of the
// writes. The five writes the regex covers are the ones an agent could be
// tempted to name; dod_mark arrives with T9 as ONE of the three named
// twins (an evidence-bound mark, not a write door), and the reserved-set
// check below carries it.
var gateWriteToolRe = regexp.MustCompile(`verify_(add|update|archive|try)|mergerule_(set|clear)`)

// t9AllowedGateTools are the nine names T9's catalog may add (T9.md's
// table, verbatim as tool names). Asserted the way the autonomy guard
// reserved its five: these names may arrive in the registry and the
// catalog, and NOTHING else gate-shaped may.
var t9AllowedGateTools = map[string]bool{
	"verify_list":               true,
	"verify_show":               true,
	"verify_runs":               true,
	"mergerule_show":            true,
	"mergerule_preview":         true,
	"dod_list":                  true,
	"verify_propose_add":        true,
	"mergerule_propose_tighten": true,
	"dod_mark":                  true,
}

// gateFamilyTwinMethods are the THREE named twins: the only records specs
// that may mutate the gate family, and only as inert proposals (or an
// evidence-bound mark). T9 implements them; until then the catalog holds
// no gate-family spec at all, and the predicate below already allows
// exactly these three — no more, no less — so T9's landings do not trip
// this guard and a smuggled twelfth does.
var gateFamilyTwinMethods = map[string]bool{
	"verify.proposeAdd":        true,
	"mergerule.proposeTighten": true,
	"dod.markAsAgent":          true,
}

// gateLiterals are what the text grep hunts: the nine wire names in quotes
// (the form they take at a call site or a projection) plus their Method
// constants (the form they take in Go). Both forms matter: a projection
// written as a literal and one written as a constant are the same door.
var gateLiterals = []string{
	`"verify.add"`, `"verify.update"`, `"verify.archive"`, `"verify.restore"`,
	`"verify.applyTemplate"`, `"verify.try"`, `"mergerule.set"`, `"mergerule.clear"`, `"dod.mark"`,
	"MethodVerifyAdd", "MethodVerifyUpdate", "MethodVerifyArchive", "MethodVerifyRestore",
	"MethodVerifyApplyTemplate", "MethodVerifyTry", "MethodMergeRuleSet", "MethodMergeRuleClear",
	"MethodDoDMark",
}

// gateSurfaceDirs are the four agent-facing packages the grep walks.
// Measured 2026-11-02: 45 non-test Go files (narrow 8, agentsvc 10,
// tools 25, records 2); the floor below sits under the measurement so the
// walk can never pass on a pruned tree, and the tree only grows.
var gateSurfaceDirs = []string{"internal/narrow", "internal/agentsvc", "internal/tools", "internal/records"}

const gateSurfaceWalkFloor = 40

// ─── the seam stubs ───────────────────────────────────────────────────────

// guardVerifyService / guardMergeRuleService / guardDoDService are the
// registration markers: zero-value returns, present so the table can hold
// the gate doors. The compile-time asserts are the point — if a seam
// interface changes shape, this file will not build and the human-door
// test below stops pretending.

type guardVerifyService struct{}

var _ protocol.VerifyStore = (*guardVerifyService)(nil)

func (*guardVerifyService) List(context.Context, protocol.VerifyListQuery) (protocol.VerifyListResult, error) {
	return protocol.VerifyListResult{}, nil
}
func (*guardVerifyService) Show(context.Context, string) (protocol.VerifyShowResult, error) {
	return protocol.VerifyShowResult{}, nil
}
func (*guardVerifyService) Runs(context.Context, protocol.VerifyRunsQuery) (protocol.VerifyRunsResult, error) {
	return protocol.VerifyRunsResult{}, nil
}
func (*guardVerifyService) RunShow(context.Context, string) (protocol.VerifyRunShowResult, error) {
	return protocol.VerifyRunShowResult{}, nil
}
func (*guardVerifyService) Vocabulary(context.Context) (protocol.VerifyVocabularyResult, error) {
	return protocol.VerifyVocabularyResult{}, nil
}
func (*guardVerifyService) Templates(context.Context) (protocol.VerifyTemplatesResult, error) {
	return protocol.VerifyTemplatesResult{}, nil
}
func (*guardVerifyService) Add(context.Context, protocol.Actor, protocol.VerifyAddInput) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyWriteResult{}, nil, nil
}
func (*guardVerifyService) Update(context.Context, protocol.Actor, protocol.VerifyUpdateInput) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyWriteResult{}, nil, nil
}
func (*guardVerifyService) Archive(context.Context, protocol.Actor, string) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyWriteResult{}, nil, nil
}
func (*guardVerifyService) Restore(context.Context, protocol.Actor, string) (protocol.VerifyWriteResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyWriteResult{}, nil, nil
}
func (*guardVerifyService) ApplyTemplate(context.Context, protocol.Actor, string, []string) (protocol.VerifyApplyResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyApplyResult{}, nil, nil
}
func (*guardVerifyService) Try(context.Context, protocol.Actor, string, string) (protocol.VerifyTryResult, *protocol.VerifyRefusal, error) {
	return protocol.VerifyTryResult{}, nil, nil
}

type guardMergeRuleService struct{}

var _ protocol.MergeRuleStore = (*guardMergeRuleService)(nil)

func (*guardMergeRuleService) Get(context.Context, string, bool) (protocol.MergeRuleGetResult, error) {
	return protocol.MergeRuleGetResult{}, nil
}
func (*guardMergeRuleService) Preview(context.Context, string, int) (protocol.MergeRulePreviewResult, error) {
	return protocol.MergeRulePreviewResult{}, nil
}
func (*guardMergeRuleService) Changes(context.Context, string, int) (protocol.MergeRuleChangesResult, error) {
	return protocol.MergeRuleChangesResult{}, nil
}
func (*guardMergeRuleService) Vocabulary(context.Context) (protocol.MergeRuleVocabularyResult, error) {
	return protocol.MergeRuleVocabularyResult{}, nil
}
func (*guardMergeRuleService) Set(context.Context, protocol.Actor, protocol.MergeRuleSetInput) (protocol.MergeRuleSetResult, *protocol.MergeRuleRefusal, error) {
	return protocol.MergeRuleSetResult{}, nil, nil
}
func (*guardMergeRuleService) Clear(context.Context, protocol.Actor, string) (protocol.MergeRuleSetResult, *protocol.MergeRuleRefusal, error) {
	return protocol.MergeRuleSetResult{}, nil, nil
}
func (*guardMergeRuleService) Events(context.Context, protocol.MergeEventsQuery) (protocol.MergeEventsResult, error) {
	return protocol.MergeEventsResult{}, nil
}
func (*guardMergeRuleService) Watches(context.Context, protocol.MergeWatchesQuery) (protocol.MergeWatchesResult, error) {
	return protocol.MergeWatchesResult{}, nil
}

type guardDoDService struct{}

var _ protocol.DoDStore = (*guardDoDService)(nil)

func (*guardDoDService) List(context.Context, string) (protocol.DoDListResult, error) {
	return protocol.DoDListResult{}, nil
}
func (*guardDoDService) Mark(context.Context, protocol.Actor, protocol.DoDMarkInput) (protocol.DoDWriteResult, *protocol.DoDRefusal, error) {
	return protocol.DoDWriteResult{}, nil, nil
}

// newGateTable wires the gate seams the way the autonomy guard wires its
// one: a fresh table, the stubs assigned, Register* called. A false
// Register* over a NON-nil seam is a wiring failure — the app would
// silently stand up with no gate door at all.
func newGateTable(t *testing.T, verify, mergeRule, doD bool) *protocol.Table {
	t.Helper()
	tbl := protocol.NewTable(protocol.NewSessions(), protocol.NewSubscriptions(), protocol.NewEventLog())
	if verify {
		tbl.Verify = &guardVerifyService{}
		if !tbl.RegisterVerify() {
			t.Fatal("RegisterVerify refused over a non-nil seam — the app would stand up with no verify door")
		}
	} else {
		tbl.RegisterVerify() // no-op over a nil seam: the family stays absent (V13)
	}
	if mergeRule {
		tbl.MergeRule = &guardMergeRuleService{}
		if !tbl.RegisterMergeRule() {
			t.Fatal("RegisterMergeRule refused over a non-nil seam — the app would stand up with no merge-rule door")
		}
	} else {
		tbl.RegisterMergeRule()
	}
	if doD {
		tbl.DoD = &guardDoDService{}
		if !tbl.RegisterDoD() {
			t.Fatal("RegisterDoD refused over a non-nil seam — the app would stand up with no DoD door")
		}
	} else {
		tbl.RegisterDoD()
	}
	return tbl
}

func gateDispatchUnknown(t *testing.T, tbl *protocol.Table, m protocol.MethodName) {
	t.Helper()
	if _, err := tbl.Dispatch(context.Background(), "c-gate-guard", m, nil); !errors.As(err, new(protocol.ErrUnknownMethod)) {
		t.Errorf("Dispatch(%s) on a service-less table answered %v, want the ErrUnknownMethod sentinel — a nil seam must be ABSENT, not registered-and-refusing (V13)", m, err)
	}
}

// ─── 1: the human door ────────────────────────────────────────────────────

func TestGateAuthoringIsAHumanDoor(t *testing.T) {
	tbl := newGateTable(t, true, true, true)
	have := make(map[protocol.MethodName]bool, len(tbl.Methods()))
	for _, m := range tbl.Methods() {
		have[m] = true
	}

	// (1) the exact twenty-two stand: nine writes, thirteen reads.
	for _, m := range gateAllMethods() {
		if !have[m] {
			t.Errorf("%s is not registered with its service wired — the human door is wired out of existence (P34)", m)
		}
	}

	// (2) nothing else slipped in under the family prefixes: an uninvited
	// door cannot enter the table while the family name is on it.
	for m := range have {
		if _, isFamily := gatePrefixOf(m); isFamily && !isGateMethod(m) {
			t.Errorf("%s stands under a gate prefix but is not one of the twenty-two — an uninvited door (VG17)", m)
		}
	}

	// (3) every entry is gated and non-debug: the gate doors open only on
	// an initialized human connection, and none is a backdoor.
	for _, m := range gateAllMethods() {
		got, ok := tbl.Lookup(m)
		if !ok {
			t.Fatalf("Lookup(%s) missed after a successful registration", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s is open before Initialize — the gate doors stand on the initialized connection like every other human door", m)
		}
		if got.Debug {
			t.Errorf("%s is a Debug method — a gate door is never a backdoor", m)
		}
	}

	// (4) every service nil at once: the whole family absent, and Dispatch
	// answers the sentinel for all twenty-two.
	none := newGateTable(t, false, false, false)
	for _, m := range gateAllMethods() {
		if _, ok := none.Lookup(m); ok {
			t.Errorf("%s registered with its service nil — registered-and-refusing is V13's forbidden shape", m)
		}
		gateDispatchUnknown(t, none, m)
	}

	// (5) one service nil at a time: its family absent, the other two
	// families intact — the seams are independent, the way the daemon
	// wires them.
	for name, family := range gateFamilies {
		var tbl2 *protocol.Table
		switch name {
		case "verify":
			tbl2 = newGateTable(t, false, true, true)
		case "mergerule":
			tbl2 = newGateTable(t, true, false, true)
		case "dod":
			tbl2 = newGateTable(t, true, true, false)
		}
		for _, m := range family {
			if _, ok := tbl2.Lookup(m); ok {
				t.Errorf("%s registered while the %s service is nil", m, name)
			}
			gateDispatchUnknown(t, tbl2, m)
		}
		// a spot check that a DIFFERENT family still stands: the seams are
		// independent, the way the daemon wires them.
		spotCheck := map[string]protocol.MethodName{
			"verify":    protocol.MethodMergeRuleGet,
			"mergerule": protocol.MethodVerifyList,
			"dod":       protocol.MethodMergeRuleGet,
		}[name]
		if _, ok := tbl2.Lookup(spotCheck); !ok {
			t.Errorf("with only the %s service nil, %s (a DIFFERENT family) is gone — the seams are not independent", name, spotCheck)
		}
	}
}

// ─── 2: the agent surfaces ────────────────────────────────────────────────

// narrowProjectingGate is the narrow predicate: an action whose projection
// reaches ANY gate method — the nine writes or the reads (the agent's gate
// reads ride T9's tools, not this surface, and no read stands before a
// write could).
func narrowProjectingGate(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if isGateMethod(a.SocketMethod) {
			bad = append(bad, fmtAction(a))
		}
	}
	return bad
}

func fmtAction(a narrow.Action) string {
	return a.Name + " → " + string(a.SocketMethod)
}

// catalogGateOffenses is the records predicate, per T9.md's law:
//   - no spec twinned to any of the nine writes, ever;
//   - no spec twinned to proposal.commit / proposal.reject (the commit
//     door is a person's CLI, not an agent's spec);
//   - a gate-family spec (a method under the family prefixes) may only
//     be a read — unless it is ONE of the three named twins, which T9
//     lands as the detectable, evidence-bound mutators.
func catalogGateOffenses(specs []records.Spec) []string {
	var bad []string
	for _, s := range specs {
		if s.Method == "proposal.commit" || s.Method == "proposal.reject" {
			bad = append(bad, s.Name+" → "+s.Method+" (the commit door is a person's)")
			continue
		}
		if gateWriteSet[protocol.MethodName(s.Method)] {
			bad = append(bad, s.Name+" → "+s.Method+" (twinned to a gate write)")
			continue
		}
		if _, isFamily := gatePrefixOf(protocol.MethodName(s.Method)); isFamily && s.Mutates && !gateFamilyTwinMethods[s.Method] {
			bad = append(bad, s.Name+" → "+s.Method+" (a gate-family mutator outside the three named twins)")
		}
		if gateWriteToolRe.MatchString(s.Name) {
			bad = append(bad, s.Name+" (a tool name twinned to a gate write)")
		}
	}
	return bad
}

// gateTextOffenses is the text predicate: a non-test file in an
// agent-surface package that names one of the nine — wire literal or
// Method constant.
func gateTextOffenses(text string) []string {
	var bad []string
	for _, lit := range gateLiterals {
		if strings.Contains(text, lit) {
			bad = append(bad, "names "+lit)
		}
	}
	return bad
}

func TestGateAuthoringNeverReachesTheNarrowOrAgentSurfaces(t *testing.T) {
	t.Run("NarrowSurface", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() returned nothing — the surface is empty and this check verified nothing")
		}
		if bad := narrowProjectingGate(actions); len(bad) != 0 {
			t.Errorf("the narrow surface projects %s — a gate door on the agent surface is the hole VG17 keeps shut", strings.Join(bad, "; "))
		}
	})

	t.Run("AgentServices", func(t *testing.T) {
		methds := protocol.AgentServiceMethods()
		if len(methds) == 0 {
			t.Fatal("the agent service list is empty — the check verified nothing")
		}
		var bad []string
		for _, m := range methds {
			if isGateMethod(m.Name) {
				bad = append(bad, string(m.Name))
			}
		}
		if len(bad) != 0 {
			t.Errorf("an agent service holds %s — the gate doors are the human connection's", strings.Join(bad, ", "))
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() returned nothing — the check verified nothing")
		}
		var bad []string
		for name := range reg {
			switch {
			case gateWriteToolRe.MatchString(name):
				bad = append(bad, name+" (twinned to a gate write)")
			case gateShapedName(name) && !t9AllowedGateTools[name]:
				bad = append(bad, name+" (gate-shaped, and not one of T9's nine reserved names)")
			}
		}
		if len(bad) != 0 {
			t.Errorf("the tool registry holds %s", strings.Join(bad, "; "))
		}
	})

	t.Run("RecordsCatalog", func(t *testing.T) {
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() returned nothing — the check verified nothing")
		}
		if bad := catalogGateOffenses(specs); len(bad) != 0 {
			t.Errorf("the records catalog holds %s", strings.Join(bad, "; "))
		}
	})

	t.Run("TextGrep", func(t *testing.T) {
		var walked int
		for _, dir := range gateSurfaceDirs {
			err := filepath.WalkDir(filepath.Join("..", "..", dir), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil // unreadable dirs hold no Go code we own
				}
				if d.IsDir() {
					return nil
				}
				if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				src, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil
				}
				walked++
				if bad := gateTextOffenses(string(src)); len(bad) != 0 {
					t.Errorf("%s: %s — a gate write named in an agent-surface package (VG17)", filepath.ToSlash(path), strings.Join(bad, ", "))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if walked < gateSurfaceWalkFloor {
			t.Fatalf("the walk saw only %d non-test files across the four agent-surface packages (floor %d) — it checked nothing", walked, gateSurfaceWalkFloor)
		}
	})

	t.Run("BoundaryStatement", func(t *testing.T) {
		stmt := narrow.BoundaryStatement()
		if strings.TrimSpace(stmt) == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface cannot account for its own absence")
		}
		if bad := gateTextOffenses(stmt); len(bad) != 0 {
			t.Errorf("the boundary statement %s — it is rendered from the action table, so a gate door in the statement means a gate door in the table", strings.Join(bad, ", "))
		}
	})
}

func gateShapedName(name string) bool {
	return strings.HasPrefix(name, "verify_") || strings.HasPrefix(name, "mergerule_") || strings.HasPrefix(name, "dod_")
}

// ─── 3: the write boundary ────────────────────────────────────────────────

// gateBoundaryNeedles are the six store doors that ARE the gate-write
// boundary (VG17's store half). A seventh caller is a second door.
var gateBoundaryNeedles = []string{
	"AddVerifyCommand(",
	"UpdateVerifyCommand(",
	"ArchiveVerifyCommand(",
	"SetMergeRule(",
	"ClearMergeRule(",
	"MarkDoD(",
}

// gateBoundaryAllowed is the contract, path by path: the store (the doors'
// own house), the three daemon services (the only socket doors), and
// cmd/aeon (the desk applier — the commit's Apply half — and T10's direct
// door). Every other crossing is a second door.
func gateBoundaryAllowed(rel string) bool {
	if strings.HasPrefix(rel, "internal/store/") {
		return true
	}
	switch rel {
	case "internal/daemon/verify_service.go",
		"internal/daemon/mergerule_service.go",
		"internal/daemon/dod_service.go":
		return true
	}
	return strings.HasPrefix(rel, "cmd/aeon/")
}

func gateBoundaryOffenses(rel, text string) []string {
	if gateBoundaryAllowed(rel) {
		return nil
	}
	var bad []string
	for _, n := range gateBoundaryNeedles {
		if strings.Contains(text, n) {
			bad = append(bad, "crosses "+n)
		}
	}
	return bad
}

func TestOnlyTheServicesCallTheGateWriteBoundary(t *testing.T) {
	var walked int
	seen := map[string]bool{}
	err := filepath.WalkDir("../../", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable dirs (dist/, node_modules) hold no Go code we own
		}
		if d.IsDir() {
			switch d.Name() {
			case "dist", "node_modules", "ui", ".git", ".dev", "vendor", ".tmp":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		walked++
		seen[rel] = true
		if bad := gateBoundaryOffenses(rel, string(src)); len(bad) != 0 {
			t.Errorf("%s: %s — a second door onto the gate write boundary (VG17)", rel, strings.Join(bad, ", "))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 {
		t.Fatalf("the walk saw only %d non-test files — it checked nothing (never pass on an empty walk)", walked)
	}

	// The contract is a contract both ways for the three named service
	// files: each must exist and still cross at least one needle, or the
	// exemption is hiding a file that moved.
	for rel := range map[string]bool{
		"internal/daemon/verify_service.go":    true,
		"internal/daemon/mergerule_service.go": true,
		"internal/daemon/dod_service.go":       true,
	} {
		if !seen[rel] {
			t.Errorf("the contract lists %s but the walk never saw it — the file moved or is gone", rel)
			continue
		}
		src, readErr := os.ReadFile(filepath.Join("../../", rel))
		if readErr != nil {
			t.Errorf("the contract lists %s and the walk saw it, but it cannot be read: %v", rel, readErr)
			continue
		}
		n := 0
		for _, needle := range gateBoundaryNeedles {
			if strings.Contains(string(src), needle) {
				n++
			}
		}
		if n == 0 {
			t.Errorf("the contract lists %s as a service, but it crosses no gate door any more — drop the entry (an exemption without its reason is a hole waiting for a caller)", rel)
		}
	}
}

// ─── 4: the guards have teeth ─────────────────────────────────────────────

func TestGateGuardsHaveTeeth(t *testing.T) {
	t.Run("NarrowCanary", func(t *testing.T) {
		sneaky := []narrow.Action{{Name: "sneaky_verify_add", SocketMethod: protocol.MethodVerifyAdd}}
		if bad := narrowProjectingGate(sneaky); len(bad) == 0 {
			t.Fatal("a poisoned narrow action projecting verify.add was NOT flagged — the narrow guard is a no-op")
		}
		clean := []narrow.Action{{Name: "loop_list", SocketMethod: protocol.MethodLoopList}}
		if bad := narrowProjectingGate(clean); len(bad) != 0 {
			t.Fatalf("a clean read projection was flagged: %v — the guard false-positives", bad)
		}
	})

	t.Run("CatalogCanary", func(t *testing.T) {
		sneaky := []records.Spec{{Name: "verify_add", Method: "verify.add", Family: "verify", Class: "detectable", Mutates: true}}
		if bad := catalogGateOffenses(sneaky); len(bad) == 0 {
			t.Fatal("a poisoned records spec twinned to verify.add was NOT flagged — the catalog guard is a no-op")
		}
		// a gate write under a different name is the same offense
		renamed := []records.Spec{{Name: "gate_creator", Method: "mergerule.set", Family: "mergerule", Class: "detectable", Mutates: true}}
		if bad := catalogGateOffenses(renamed); len(bad) == 0 {
			t.Fatal("a spec twinned to mergerule.set under a friendly name was NOT flagged")
		}
		// the commit door, even as a read, is a person's
		commitTwin := []records.Spec{{Name: "commit_proposal", Method: "proposal.commit", Family: "proposal", Mutates: true}}
		if bad := catalogGateOffenses(commitTwin); len(bad) == 0 {
			t.Fatal("a spec twinned to proposal.commit was NOT flagged — the commit door is not an agent's spec")
		}
		// the clean side: a read and each of the three named twins stand.
		clean := []records.Spec{
			{Name: "verify_list", Method: "verify.list", Family: "verify", Class: "safe", Mutates: false},
			{Name: "dod_list", Method: "dod.list", Family: "dod", Class: "safe", Mutates: false},
			{Name: "verify_propose_add", Method: "verify.proposeAdd", Family: "verify", Class: "detectable", Mutates: true},
			{Name: "mergerule_propose_tighten", Method: "mergerule.proposeTighten", Family: "mergerule", Class: "detectable", Mutates: true},
			{Name: "dod_mark", Method: "dod.markAsAgent", Family: "dod", Class: "detectable", Mutates: true},
		}
		if bad := catalogGateOffenses(clean); len(bad) != 0 {
			t.Fatalf("clean specs (reads + the three named twins) were flagged: %v — the guard false-positives on T9's own landings", bad)
		}
	})

	t.Run("TextGrepCanary", func(t *testing.T) {
		if bad := gateTextOffenses(`m := MethodVerifyAdd`); len(bad) == 0 {
			t.Fatal("a file naming MethodVerifyAdd was NOT flagged — the text guard is a no-op")
		}
		if bad := gateTextOffenses(`wire "mergerule.set" to the handler`); len(bad) == 0 {
			t.Fatal("a file naming the wire literal \"mergerule.set\" was NOT flagged")
		}
		if bad := gateTextOffenses(`list := verifyReads // the reads are fine`); len(bad) != 0 {
			t.Fatalf("a file naming only reads was flagged: %v — the guard false-positives", bad)
		}
	})

	t.Run("BoundaryWalkCanary", func(t *testing.T) {
		sneakySrc := "package daemon\n\nfunc sneaky(st *store.Store) {\n\tst.SetMergeRule(actor, in)\n}\n"
		if bad := gateBoundaryOffenses("internal/daemon/sneaky.go", sneakySrc); len(bad) == 0 {
			t.Fatal("a SetMergeRule caller in a non-contracted daemon file was NOT flagged — the boundary walk is a no-op")
		}
		if bad := gateBoundaryOffenses("internal/daemon/mergerule_service.go", sneakySrc); len(bad) != 0 {
			t.Fatalf("the SAME crossing under a contracted path was flagged: %v — the allowlist false-positives", bad)
		}
		if bad := gateBoundaryOffenses("internal/store/whatever.go", sneakySrc); len(bad) != 0 {
			t.Fatalf("the store's own house was flagged: %v", bad)
		}
		if bad := gateBoundaryOffenses("cmd/aeon/direct_door.go", sneakySrc); len(bad) != 0 {
			t.Fatalf("cmd/aeon (the desk applier and T10's direct door) was flagged: %v", bad)
		}
		if bad := gateBoundaryOffenses("internal/daemon/sneaky.go", "package daemon\n\nfunc clean() {}\n"); len(bad) != 0 {
			t.Fatalf("a needle-free file under a non-contracted path was flagged: %v", bad)
		}
	})
}
