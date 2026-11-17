package main

// history_guard_test.go — S53 T2 (P4 shape): the history slice's claims made
// mechanical, in the mould of the roadmap (RM), automation (AN) and blueprint
// (BP) guards in this directory. HV3 (history is read-only, everywhere) and
// HV5 (history.export is a person's door and nobody else's) are the claims;
// every guard has a canary proving it can FAIL, no guard passes on an empty
// walk, and no existing guard body is edited here.
//
// S25's own floor/coverage tests stay hash-pinned by T0 and are re-run
// unchanged by TestS25HistoryPinsAreUnchanged at the bottom — a pin that must
// be re-hashed to pass is a claim quietly rewritten.

import (
	"crypto/sha256"
	"encoding/hex"
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

// historyReads are the four reads; historyPersonOnly is the one door that
// stays a person's (HV5) — a person taking everything out of the record.
var historyReads = []protocol.MethodName{
	protocol.MethodHistoryList, protocol.MethodHistoryShow,
	protocol.MethodHistoryCoverage, protocol.MethodHistoryVocabulary,
}

var historyPersonOnly = []protocol.MethodName{protocol.MethodHistoryExport}

var historyAll = append(append([]protocol.MethodName{}, historyReads...), historyPersonOnly...)

// historyStoreMethods is the closed set of entry points the read-only facade
// may expose (HV3): the list/merge read, the drill-down, and T1's coverage
// read. A fifth exported method on the facade is a new door, and doors arrive
// with their own task and guard, never silently.
var historyStoreMethods = map[string]bool{
	"List": true, "Show": true, "Coverage": true,
}

// historyCLIVerbs is the closed verb table of `aeon history` (HV3: the CLI
// holds no mutating verb; T4 adds coverage/vocabulary to this same set).
var historyCLIVerbs = map[string]bool{
	"list": true, "show": true, "export": true, "coverage": true, "vocabulary": true,
}

// ── guard 1: history is read-only, on every door and in the package ───────

// historyWriteSQL sees every way production code could write through the
// history package: raw SQL verbs. The package's whole claim is that it reads.
var historyWriteSQL = regexp.MustCompile(`(?i)\b(INSERT\s+INTO|UPDATE\s+\w+\s+SET|DELETE\s+FROM|CREATE\s+TABLE|DROP\s+TABLE)\b`)

func historyWriteSQLOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if m := historyWriteSQL.FindString(src); m != "" {
			bad = append(bad, filepath.ToSlash(path)+" carries SQL writing — "+firstLineOf(m)+" — history is derived and read-only (HV3)")
		}
	}
	return bad
}

// historyStoreMethodOffenses walks the facade's method set.
var historyStoreMethod = regexp.MustCompile(`func \(st \*Store\) ([A-Z]\w*)`)

func historyStoreMethodOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for _, m := range historyStoreMethod.FindAllStringSubmatch(src, -1) {
			if !historyStoreMethods[m[1]] {
				bad = append(bad, filepath.ToSlash(path)+" exposes (*Store)."+m[1]+" — the read-only facade's method set is closed (HV3)")
			}
		}
	}
	return bad
}

// historyCLIVerbOffenses reads the dispatch switch of `aeon history`: every
// quoted case label must be a known read verb. A mutating word appearing as a
// verb — or a new verb nobody declared — is the same leak in CLI clothes.
var historyCLICase = regexp.MustCompile(`case "([a-z]+)":`)

func historyCLIVerbOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if filepath.Base(path) != "history.go" {
			continue
		}
		for _, m := range historyCLICase.FindAllStringSubmatch(src, -1) {
			if !historyCLIVerbs[m[1]] {
				bad = append(bad, filepath.ToSlash(path)+" dispatches the verb "+m[1]+" — `aeon history` has no verb outside the closed set (HV3)")
			}
		}
	}
	return bad
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func historyProductionSources() map[string]string {
	all := walkedSources([]string{filepath.Join("..", "..", "internal", "history")}, nil)
	out := map[string]string{}
	for path, src := range all {
		if !strings.HasSuffix(path, "_test.go") {
			out[path] = src
		}
	}
	return out
}

func TestHistoryIsReadOnly(t *testing.T) {
	// The package: no writing SQL, no widened facade.
	files := historyProductionSources()
	if len(files) < 5 {
		t.Fatalf("the walk saw %d production files under internal/history — no conclusion without the whole package", len(files))
	}
	for _, bad := range historyWriteSQLOffenses(files) {
		t.Error(bad)
	}
	for _, bad := range historyStoreMethodOffenses(files) {
		t.Error(bad)
	}

	// The CLI: the verb table is closed over reads.
	cli := map[string]string{"cmd/aeon/history.go": mustReadSource(t, "history.go")}
	if len(cli) != 1 {
		t.Fatal("the CLI history source could not be read")
	}
	for _, bad := range historyCLIVerbOffenses(cli) {
		t.Error(bad)
	}

	// The socket: every history method is a registered, negotiated, non-debug
	// READ — none of them is a write dressed as one.
	d := blueprintGuardDaemon(t)
	for _, m := range historyAll {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on a fully wired daemon", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug", m)
		}
	}
}

// mustReadSource reads a file next to this test (cmd/aeon).
func mustReadSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// ── guard 2: history.export is a person's door, four greps and a statement ─

func narrowHistoryExportOffenses(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if a.SocketMethod == protocol.MethodHistoryExport {
			bad = append(bad, "action "+a.Name+" projects history.export — emptying the record is a person's act (HV5)")
		}
	}
	return bad
}

// historyExportToolName matches ONLY the export twin: history_list/history_show
// are T3's arriving reads and must sail through this predicate untouched.
var historyExportToolName = regexp.MustCompile(`^history_export$`)

func toolsHistoryExportOffenses(names []string) []string {
	var bad []string
	for _, name := range names {
		if historyExportToolName.MatchString(name) {
			bad = append(bad, "tool "+name+" twins history.export — the export door has no agent twin (HV5)")
		}
	}
	return bad
}

func recordsHistoryExportOffenses(specs []records.Spec) []string {
	var bad []string
	for _, spec := range specs {
		if spec.Method == string(protocol.MethodHistoryExport) {
			bad = append(bad, "catalog spec "+spec.Name+" twins history.export (HV5)")
		}
	}
	return bad
}

func historyExportTextOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		if strings.Contains(src, `"history.export"`) || strings.Contains(src, "MethodHistoryExport") {
			bad = append(bad, filepath.ToSlash(path)+" speaks of history.export — the four door packages must not even name it (HV5)")
		}
	}
	return bad
}

func TestHistoryExportIsPersonOnly(t *testing.T) {
	for _, bad := range narrowHistoryExportOffenses(narrow.Actions()) {
		t.Error(bad)
	}
	for _, m := range protocol.AgentServiceMethods() {
		if m.Name == protocol.MethodHistoryExport {
			t.Error("history.export is an agent-service method (HV5)")
		}
	}
	var names []string
	for name := range tools.Registry() {
		names = append(names, name)
	}
	for _, bad := range toolsHistoryExportOffenses(names) {
		t.Error(bad)
	}
	for _, bad := range recordsHistoryExportOffenses(records.Catalog()) {
		t.Error(bad)
	}
	roots := []string{
		filepath.Join("..", "..", "internal", "narrow"),
		filepath.Join("..", "..", "internal", "agentsvc"),
		filepath.Join("..", "..", "internal", "tools"),
		filepath.Join("..", "..", "internal", "records"),
	}
	files := walkedSources(roots, nil)
	if len(files) < 10 {
		t.Fatalf("the walk saw %d files — a grep that reads nothing passes forever", len(files))
	}
	for _, bad := range historyExportTextOffenses(files) {
		t.Error(bad)
	}
	// The boundary statement accounts for what is ABSENT; naming the export
	// there would claim a projection that must never exist.
	if s := narrow.BoundaryStatement(); strings.Contains(s, "history.export") {
		t.Error("the narrow boundary statement names history.export (HV5)")
	}
}

// ── guard 3: no history method rides the narrow surface at all ─────────────

func narrowAnyHistoryOffenses(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		if strings.HasPrefix(string(a.SocketMethod), "history.") {
			bad = append(bad, "action "+a.Name+" projects "+string(a.SocketMethod)+" — no history door is narrow (HV3/HV5)")
		}
	}
	return bad
}

func TestNoHistoryMethodIsOnTheNarrowSurface(t *testing.T) {
	got := narrowAnyHistoryOffenses(narrow.Actions())
	// The empty case must be an honest empty, not a blind scanner.
	if len(narrow.Actions()) == 0 {
		t.Fatal("narrow.Actions() answered nothing — the guard cannot conclude")
	}
	for _, bad := range got {
		t.Error(bad)
	}
}

// ── guard 4: every history method is classified a read (S47), export claims
//    its person-only status in the table too ────────────────────────────────

func TestEveryHistoryMethodIsClassifiedRead(t *testing.T) {
	for _, m := range historyAll {
		s, ok := protocol.SurfaceClassified(m)
		if !ok {
			t.Errorf("%s is unclassified — the surface table must answer for every history door (S47)", m)
			continue
		}
		if s.Kind != protocol.SurfaceRead {
			t.Errorf("%s is classified %q — every history door reads and must say so", m, s.Kind)
		}
	}
	for _, m := range historyPersonOnly {
		s, _ := protocol.SurfaceClassified(m)
		if !s.HumanDoorOnly {
			t.Errorf("%s is not humanDoorOnly — the classification must claim what guard 2 proves (HV5)", m)
		}
	}
}

// ── guard 5: the guards have teeth ─────────────────────────────────────────

func TestHistoryGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow-export", func(t *testing.T) {
		fake := []narrow.Action{{Name: "history_export", SocketMethod: protocol.MethodHistoryExport}}
		if len(narrowHistoryExportOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting history.export passed the narrow guard")
		}
	})
	t.Run("narrow-any", func(t *testing.T) {
		fake := []narrow.Action{{Name: "history_coverage", SocketMethod: protocol.MethodHistoryCoverage}}
		if len(narrowAnyHistoryOffenses(fake)) == 0 {
			t.Error("a fake narrow action projecting history.coverage passed the any-history guard")
		}
	})
	t.Run("tools", func(t *testing.T) {
		if len(toolsHistoryExportOffenses([]string{"history_export"})) == 0 {
			t.Error("a fake history_export tool passed the tool guard")
		}
		// T3's reads must sail through — an over-broad predicate fails the slice it guards.
		if len(toolsHistoryExportOffenses([]string{"history_list", "history_show"})) != 0 {
			t.Error("the export predicate failed T3's READ names — it is over-broad and will fail T3")
		}
	})
	t.Run("records", func(t *testing.T) {
		fake := []records.Spec{{Name: "history_export", Method: "history.export"}}
		if len(recordsHistoryExportOffenses(fake)) == 0 {
			t.Error("a fake catalog spec twinned to history.export passed the catalog guard")
		}
	})
	t.Run("door-text", func(t *testing.T) {
		fake := map[string]string{"../../internal/tools/fake.go": `const x = "history.export"`}
		if len(historyExportTextOffenses(fake)) == 0 {
			t.Error("a fake file under internal/tools naming history.export passed the text guard")
		}
	})
	t.Run("write-sql", func(t *testing.T) {
		fake := map[string]string{"../../internal/history/fake.go": "s.Exec(`DELETE FROM loop WHERE id = ?`, id)"}
		if len(historyWriteSQLOffenses(fake)) == 0 {
			t.Error("a fake DELETE FROM loop inside internal/history passed the read-only guard")
		}
	})
	t.Run("facade", func(t *testing.T) {
		fake := map[string]string{"../../internal/history/store.go": "func (st *Store) DeleteLoop(id string) error { return nil }"}
		if len(historyStoreMethodOffenses(fake)) == 0 {
			t.Error("a fake (*Store).DeleteLoop on the facade passed the closed-methods guard")
		}
	})
	t.Run("cli-verb", func(t *testing.T) {
		fake := map[string]string{"history.go": "case \"purge\":\n\t\tpurgeAll()"}
		if len(historyCLIVerbOffenses(fake)) == 0 {
			t.Error("a fake mutating CLI verb passed the closed-verb guard")
		}
	})
}

// ── guard 6: S25's pins are the tests T0 hashed, unchanged, and they pass ──

// s25HistoryPins are T0's five hashes; the extraction is the whole function
// text — signature line through the closing brace, trailing newline kept —
// the recipe T0's numbers were produced with (re-derived here, and the five
// agreed on first try, which is the extraction's own proof).
var s25HistoryPins = []struct {
	name, file, sha string
}{
	{"TestRollupFloorsOnAnyUnknownCost", "internal/history/rollup_test.go", "72171f3b5b167e6d71ee71c3c6d6e235a5bdfb4baf992767b6604e81e2a579dd"},
	{"TestLoopCostFloorsThroughStore", "internal/history/rollup_test.go", "b6b372e82d3b1cad3b555785354783cbae3b00d06306975bb1e84c76b68fec36"},
	{"TestMergeIncludesArchivedLoops", "internal/history/query_test.go", "8a526d7fe5a154cf69f9311a11783579294df451d1f16a3e171b4548ac68da5a"},
	{"TestArchivedDrilldownIsHeaderFirstContentOnDemand", "internal/history/drilldown_test.go", "653b0627c50336a12003f59eae5d072695ab30956708395a01dc5b46380000de"},
	{"TestFourLanesNoNewJoinShape", "internal/history/lanes_test.go", "304d73030401977d74e0e9d2dc95814f649be0c35b7b7d97070652ad4c2f3ef0"},
}

func TestS25HistoryPinsAreUnchanged(t *testing.T) {
	for _, p := range s25HistoryPins {
		path := filepath.Join("..", "..", p.file)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", p.file, err)
		}
		lines := strings.Split(string(raw), "\n")
		start := -1
		for i, l := range lines {
			if strings.HasPrefix(l, "func "+p.name+"(") {
				start = i
				break
			}
		}
		if start < 0 {
			t.Errorf("%s is gone from %s — the pinned test was deleted, not re-run", p.name, p.file)
			continue
		}
		end := -1
		for j := start + 1; j < len(lines); j++ {
			if lines[j] == "}" {
				end = j
				break
			}
		}
		if end < 0 {
			t.Errorf("%s has no closing brace in %s", p.name, p.file)
			continue
		}
		body := strings.Join(lines[start:end+1], "\n") + "\n"
		sum := sha256.Sum256([]byte(body))
		if got := hex.EncodeToString(sum[:]); got != p.sha {
			t.Errorf("%s's assertion body changed (%s): got %s, pinned %s — re-running a pin means UNCHANGED, and a pin re-hashed to pass is a claim quietly rewritten",
				p.name, p.file, got, p.sha)
		}
	}
}

// TestHistoryVerbIsDispatchedByTheBinary is S53 T5's dispatch guard: every
// other guard here reads cmdHistory's own file — the walk found that the
// BINARY never reached it. `b5620631` ("wip") deleted the `case "history":`
// from main.go's dispatch exactly as it deleted the boot wiring T3 restored
// (the same silent outage, second surface): the S25 goldens call cmdHistory
// directly, so a verb dead in the real binary stayed green. A capability
// reachable only by a test's direct call is not a capability (P34, read at
// its widest): the dispatch itself must be pinned. Canary: this assertion
// fails verbatim against the main.go of the b5620631 era — the case it
// requires is precisely the text that deletion removed.
func TestHistoryVerbIsDispatchedByTheBinary(t *testing.T) {
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, "case \"history\":") {
		t.Fatal(`main.go lost case "history": — the real binary answers "unknown command" while every cmdHistory test stays green; this is exactly how b5620631 hid the outage`)
	}
	dispatch := regexp.MustCompile(`(?s)case "history":.*?err = cmdHistory\(os\.Stdout, args\)`)
	if !dispatch.MatchString(src) {
		t.Fatal(`main.go's history case no longer dispatches to cmdHistory(os.Stdout, args) — the CLI is ONE implementation; anything else is a second door`)
	}
	// The usage list must name the verb, or `aeon` without arguments cannot
	// teach a person it exists (the same deletion habit deletes both halves).
	if !strings.Contains(src, "history           list, show, export, coverage, vocabulary") {
		t.Error("the usage list no longer advertises `history` — a hidden verb is a deleted verb waiting to happen")
	}
}
