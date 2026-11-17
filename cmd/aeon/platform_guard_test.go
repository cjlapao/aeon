package main

// platform_guard_test.go — S54 T3 (P4 shape): the platform slice's claims
// made mechanical, in the mould of the automation (AN), autonomy (AL) and
// history (HV) guards in this directory. SP3 (operations are a person's
// door, audited, single-flight), SP4 (agents get reads only) and SP6 (the
// doors are classified, off the narrow surface) are the claims. Every guard
// has a canary in platform_guard_teeth_test.go proving it can FAIL, no
// guard passes on an empty walk, and no existing guard body is edited here.
//
// The registration matrix is split by build variant: the positive halves
// that need the retrieval plane live in platform_guard_retrieval_test.go
// (tag !noretrieval) and their D1 mirror in
// platform_guard_noretrieval_test.go — the same test name answers in both
// builds, because "unwired" is a claim ABOUT a variant, not an absence of
// checking.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// platformOps are the four operations SP3 makes a person's door. T2 added
// no operation; these are the existing four with actor + audit + the
// single-flight named-starter answer.
var platformOps = []protocol.MethodName{
	protocol.MethodRetrievalReindex,
	protocol.MethodGraphBuild,
	protocol.MethodGraphReparse,
	protocol.MethodGraphDelete,
}

// platformReads are T2's three reads (SP6). platform.status is the one the
// T4 tool twin will classify beside; the other two stay HumanDoorOnly.
var platformReads = []protocol.MethodName{
	protocol.MethodPlatformStatus,
	protocol.MethodRetrievalReindexPlan,
	protocol.MethodPlatformChanges,
}

// platformOpWords is the OPERATION vocabulary — method strings and the
// tool-shaped names a surface might otherwise project or propose (SP4: no
// control tool, no proposal kind — not even a named absence). All four
// packages are checked against every one of these, forever.
var platformOpWords = []string{
	"retrieval.reindex", "graph.build", "graph.reparse", "graph.delete",
	"retrieval_reindex", "graph_build", "graph_reparse", "graph_delete",
	"index_rebuild", "index_build",
}

// platformReadWords are SP6's three READS. They are refused where no read
// belongs at all — the narrow projection and the agent-services
// enumeration — but NOT in the records catalog or the tools package: T4's
// index_status twin arrives exactly the way every catalog read twin
// arrives, naming its socket method in its spec. The T3 spec's "four
// internal/* greps" lists the OPERATIONS; applying the read names there
// would contradict T4's own spec (this correction recorded at T4).
var platformReadWords = []string{
	"platform.status", "retrieval.reindexPlan", "platform.changes",
}

func wordsOffenses(text string, words []string) []string {
	var bad []string
	for _, line := range strings.Split(text, "\n") {
		for _, w := range words {
			if strings.Contains(line, w) {
				bad = append(bad, strings.TrimSpace(line))
				break
			}
		}
	}
	return bad
}

func platformWordOffenses(text string) []string { return wordsOffenses(text, platformOpWords) }

func platformReadWordOffenses(text string) []string { return wordsOffenses(text, platformReadWords) }

// ── guard 1: the four operations are a human door on every surface ────────

func narrowProjectingOpsOffenses(actions []narrow.Action) []string {
	var bad []string
	for _, a := range actions {
		for _, m := range platformOps {
			if a.SocketMethod == m {
				bad = append(bad, fmt.Sprintf("narrow action %q projects %s", a.Name, m))
			}
		}
	}
	return bad
}

func registryOpsOffenses(reg map[string]tools.Tool) []string {
	var bad []string
	for name := range reg {
		for _, w := range []string{"retrieval_reindex", "graph_build", "graph_reparse", "graph_delete", "index_rebuild", "index_build"} {
			if name == w {
				bad = append(bad, fmt.Sprintf("the tool registry carries %q — an agent door onto an operation (SP4)", name))
			}
		}
	}
	return bad
}

func catalogOpsOffenses(specs []records.Spec) []string {
	var bad []string
	for _, sp := range specs {
		for _, w := range []string{"retrieval_reindex", "graph_build", "graph_reparse", "graph_delete", "index_rebuild", "index_build"} {
			if sp.Name == w {
				bad = append(bad, fmt.Sprintf("the records catalog carries control twin %q — a catalog entry is how a control reaches an agent surface (SP4)", w))
			}
		}
	}
	return bad
}

func agentServicesOpsOffenses(ms []protocol.AgentServiceMethod) []string {
	var bad []string
	for _, m := range ms {
		for _, op := range platformOps {
			if m.Name == op {
				bad = append(bad, fmt.Sprintf("agent service %s is an operation — the services speak for a person, they do not spend a person's money or discard a person's derived data (SP3/SP4)", op))
			}
		}
	}
	return bad
}

func TestPlatformOperationsAreAHumanDoor(t *testing.T) {
	d := blueprintGuardDaemon(t) // the store-wired daemon: the graph plane registers
	for _, m := range []protocol.MethodName{protocol.MethodGraphBuild, protocol.MethodGraphReparse, protocol.MethodGraphDelete} {
		got, ok := d.Table.Lookup(m)
		if !ok {
			t.Fatalf("%s is not registered on the store-wired daemon — the app would have no door", m)
		}
		if !got.RequiresInitialize {
			t.Errorf("%s does not RequireInitialize — an operation may not precede negotiation", m)
		}
		if got.Debug {
			t.Errorf("%s is marked Debug — an operation is a product method, not a diagnostic", m)
		}
	}

	t.Run("narrow", func(t *testing.T) {
		actions := narrow.Actions()
		if len(actions) == 0 {
			t.Fatal("narrow.Actions() is empty — the projection has no table, so this guard would pass on nothing")
		}
		for _, bad := range narrowProjectingOpsOffenses(actions) {
			t.Error(bad)
		}
		stmt := narrow.BoundaryStatement()
		if stmt == "" {
			t.Fatal("narrow.BoundaryStatement() is empty — the surface has no account of itself to check")
		}
		low := strings.ToLower(stmt)
		for _, w := range []string{"reindex", "graph.build", "graph.delete", "graph.reparse"} {
			if strings.Contains(low, w) {
				t.Errorf("the boundary statement names %q — the statement is the surface's account of what is ABSENT and must not carry the operation vocabulary in", w)
			}
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		ms := protocol.AgentServiceMethods()
		if len(ms) == 0 {
			t.Fatal("protocol.AgentServiceMethods() is empty — the enumeration is gone, so this guard would pass on nothing")
		}
		for _, bad := range agentServicesOpsOffenses(ms) {
			t.Error(bad)
		}
	})

	t.Run("registry-and-catalog", func(t *testing.T) {
		reg := tools.Registry()
		if len(reg) == 0 {
			t.Fatal("tools.Registry() is empty — this guard would pass on nothing")
		}
		for _, bad := range registryOpsOffenses(reg) {
			t.Error(bad)
		}
		specs := records.Catalog()
		if len(specs) == 0 {
			t.Fatal("records.Catalog() is empty — this guard would pass on nothing")
		}
		for _, bad := range catalogOpsOffenses(specs) {
			t.Error(bad)
		}
	})

	t.Run("door-package-text", func(t *testing.T) {
		// Every package refuses the OPERATION vocabulary; the two surfaces
		// with no read business of their own also refuse the three READ
		// names (see platformReadWords' why beside it).
		strict := []string{
			filepath.Join("..", "..", "internal", "narrow"),
			filepath.Join("..", "..", "internal", "agentsvc"),
		}
		loose := []string{
			filepath.Join("..", "..", "internal", "tools"),
			filepath.Join("..", "..", "internal", "records"),
		}
		files := walkedSources(append(append([]string{}, strict...), loose...), nil)
		if len(files) < 50 {
			t.Fatalf("the four-package walk saw %d non-test files — the grep checked almost nothing", len(files))
		}
		strictFile := func(norm string) bool {
			return strings.Contains(norm, "/internal/narrow/") || strings.Contains(norm, "/internal/agentsvc/")
		}
		for path, src := range files {
			norm := filepath.ToSlash(path)
			for _, bad := range platformWordOffenses(src) {
				t.Errorf("%s names the operation vocabulary: %s", norm, bad)
			}
			if strictFile(norm) {
				for _, bad := range platformReadWordOffenses(src) {
					t.Errorf("%s names a platform read where no platform read belongs: %s", norm, bad)
				}
			}
		}
	})
}

// ── guard 2: the start record is written only through the audited path ────

// startRecordCallers is the closed set for the async_worker INSERT: the
// ledger package defines the row and the manager is the ONLY caller — every
// start, audited or plain, rides one of the manager's doors, and the audited
// doors are the manager's own. A caller outside this set writes a worker
// the audit never hears about: an operation with no person on it.
var startRecordCallers = map[string]bool{
	"internal/store/asyncworkers.go":   true,
	"internal/asyncworkers/manager.go": true,
}

var startRecordCall = regexp.MustCompile(`\.InsertAsyncWorker(Audited)?\(`)

func startRecordWriteOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		norm := filepath.ToSlash(path)
		for i, line := range strings.Split(src, "\n") {
			if !startRecordCall.MatchString(line) {
				continue
			}
			rel := norm
			if k := strings.Index(norm, "internal/"); k >= 0 {
				rel = norm[k:]
			} else if k := strings.Index(norm, "cmd/"); k >= 0 {
				rel = norm[k:]
			}
			if !startRecordCallers[rel] {
				bad = append(bad, fmt.Sprintf("%s:%d writes a start record outside the manager: %s", norm, i+1, strings.TrimSpace(line)))
			}
		}
	}
	return bad
}

func TestOperationsAreWrittenOnlyThroughTheAuditedPath(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "internal"),
		filepath.Join("..", "..", "cmd"),
	}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the tree walk saw %d non-test files — the guard checked almost nothing", len(files))
	}
	for _, bad := range startRecordWriteOffenses(files) {
		t.Error(bad)
	}
	// Positive control: the guard's own walk DID see the lawful callers —
	// otherwise the empty offense list could mean the walk missed the
	// vocabulary entirely.
	seen := 0
	for path := range files {
		if strings.HasSuffix(filepath.ToSlash(path), "internal/asyncworkers/manager.go") ||
			strings.HasSuffix(filepath.ToSlash(path), "internal/store/asyncworkers.go") {
			seen++
		}
	}
	if seen != 2 {
		t.Fatalf("the walk did not see both lawful callers (saw %d) — the claim it passed on is not the claim it makes", seen)
	}
}

// ── guard 3: the ledger is append-only, mechanically ─────────────────────

var platformChangeMutation = regexp.MustCompile(`(?i)\b(UPDATE\s+platform_change|DELETE\s+FROM\s+platform_change)\b`)

func platformChangeMutationOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if platformChangeMutation.MatchString(line) {
				bad = append(bad, fmt.Sprintf("%s:%d mutates the append-only ledger: %s", filepath.ToSlash(path), i+1, trimmed))
			}
		}
	}
	return bad
}

func TestPlatformChangeIsAppendOnly(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "internal"),
		filepath.Join("..", "..", "cmd"),
	}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the tree walk saw %d non-test files — the guard checked almost nothing", len(files))
	}
	for _, bad := range platformChangeMutationOffenses(files) {
		t.Error(bad)
	}
	// The triggers exist where append-only is actually enforced: the
	// migration text carries both refusals.
	dir := filepath.Join("..", "..", "internal", "store", "migrations")
	mig, err := os.ReadFile(filepath.Join(dir, "20261104135327_platform_change.sql"))
	if err != nil {
		t.Fatalf("the platform_change migration is unreadable: %v", err)
	}
	text := string(mig)
	// The trigger statements verbatim: BEFORE ... ON platform_change.
	for _, want := range []string{"BEFORE UPDATE ON platform_change", "BEFORE DELETE ON platform_change"} {
		if !strings.Contains(text, want) {
			t.Errorf("the migration does not carry the append-only trigger for %q", want)
		}
	}
}

// ── guard 4: no second status computation anywhere ────────────────────────

// statusComputers are the figures' owning walks. Each token's allow-set is
// the package the subsystem S54 only READS from — a caller elsewhere means
// a second computation of someone else's truth, which SP1 forbids the
// panel to even have the shape of.
var statusComputers = []struct {
	token string
	allow []string
}{
	{"PendingChunkCount(", []string{"internal/retrieval", "cmd/aeon"}},
	{"EstimateReindex(", []string{"internal/retrieval", "cmd/aeon"}},
	{"RetrievalComponentRoots(", []string{"internal/store", "internal/daemon"}},
}

func statusComputeOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		norm := filepath.ToSlash(path)
		for _, sc := range statusComputers {
			if !strings.Contains(src, sc.token) {
				continue
			}
			ok := false
			for _, a := range sc.allow {
				if strings.Contains(norm, a+"/") {
					ok = true
					break
				}
			}
			if !ok {
				bad = append(bad, fmt.Sprintf("%s calls %s outside its owning packages %v", norm, sc.token, sc.allow))
			}
		}
	}
	return bad
}

// panelFiles are the files SP1 keeps pure: the composer, the daemon
// adapter, the protocol doors. They pass figures through and quote
// sentences; they compute nothing and open no SQL.
var panelFileCompute = regexp.MustCompile(`(?i)\b(SELECT |INSERT INTO|UPDATE |DELETE FROM|PendingChunkCount\(|EstimateReindex\(|walkStaleness|priceDrift\()`)

func panelFileComputeOffenses(files map[string]string) []string {
	var bad []string
	for path, src := range files {
		norm := filepath.ToSlash(path)
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if panelFileCompute.MatchString(line) {
				bad = append(bad, fmt.Sprintf("%s:%d computes where it must only pass through: %s", norm, i+1, trimmed))
			}
		}
	}
	return bad
}

func TestNoSecondStatusComputation(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "internal"),
		filepath.Join("..", "..", "cmd"),
	}
	files := walkedSources(roots, nil)
	if len(files) < 50 {
		t.Fatalf("the tree walk saw %d non-test files — the guard checked almost nothing", len(files))
	}
	for _, bad := range statusComputeOffenses(files) {
		t.Error(bad)
	}
	panel := map[string]string{}
	for path, src := range files {
		norm := filepath.ToSlash(path)
		if strings.HasSuffix(norm, "internal/platformview/platformview.go") ||
			strings.HasSuffix(norm, "internal/daemon/platform_reads.go") ||
			strings.HasSuffix(norm, "internal/protocol/platform.go") {
			panel[norm] = src
		}
	}
	if len(panel) != 3 {
		t.Fatalf("the panel's three files were not all found (%d) — the purity claim checked less than it claims", len(panel))
	}
	for _, bad := range panelFileComputeOffenses(panel) {
		t.Error(bad)
	}
}

// ── guard 5: unwired is a word, never an empty rendering ─────────────────

// unwiredEmptyRender catches the dishonest shape in the RENDERED door
// answer: a block claiming no subsystem while rendering empty lists — an
// agent reading `[]` sees "nothing exists", not "not wired here".
var unwiredEmptyRender = regexp.MustCompile(`"state":"unwired"[^}\[\]]*\[\]|"(components|corpora|roots|blocks)":\[\]`)

func unwiredRenderOffenses(rendered string) []string {
	if m := unwiredEmptyRender.FindString(rendered); m != "" {
		return []string{"an unwired/absent block renders an empty list — " + m}
	}
	return nil
}

func TestUnwiredIsNeverRenderedAsEmpty(t *testing.T) {
	d := blueprintGuardDaemon(t)
	got, ok := d.Table.Lookup(protocol.MethodPlatformStatus)
	if !ok {
		t.Fatal("platform.status must be registered on the store-wired daemon — the panel exists even when every subsystem is unwired")
	}
	if !got.RequiresInitialize || got.Debug {
		t.Errorf("platform.status flags = %+v, want RequiresInitialize and not Debug", got)
	}
	initializeForGuard(t, d.Table, "unwired-guard")
	raw, err := d.Table.Dispatch(context.Background(), "unwired-guard", protocol.MethodPlatformStatus, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("the panel door refused on a store-wired daemon: %v", err)
	}
	rendered, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(rendered)
	if !strings.Contains(s, `"state":"unwired"`) {
		t.Errorf("with no retrieval link, no telemetry seam and never-built graph roots the door rendered %s — the honest word for this variant is unwired", s)
	}
	for _, bad := range unwiredRenderOffenses(s) {
		t.Errorf("%s: %s", protocol.MethodPlatformStatus, bad)
	}
}

// ── guard 6: every platform method is classified (with S47's predicate) ──

func TestEveryPlatformMethodIsClassified(t *testing.T) {
	d := blueprintGuardDaemon(t)
	registered := map[protocol.MethodName]bool{}
	for _, m := range d.Table.Methods() {
		registered[m] = true
	}
	var mine []string
	for _, m := range append(append([]protocol.MethodName{}, platformReads...), platformOps...) {
		// The claim is per variant: a V13-absent door is not registered
		// here to carry a classification. platformWiredDaemon's variant (the
		// !noretrieval sibling) asserts the full set is there and classified.
		if registered[m] {
			mine = append(mine, string(m))
		}
	}
	if len(mine) < 3 {
		t.Fatalf("only %d platform methods are registered on the store-wired daemon — the panel lost a door", len(mine))
	}
	if bad := unclassifiedMethodOffenses(mine, d.Table.Surface()); len(bad) > 0 {
		t.Errorf("a platform method a real daemon registers carries no classification:\n%s", strings.Join(bad, "\n"))
	}
}

// ── guard 7: S22/S30/S33/S47's pins are the tests T0 hashed, unchanged ───

// s54Pins are T0's seventeen hashes (design SP-adjacent guard inventory).
// The extraction is the whole function text — signature line through the
// closing brace, trailing newline kept — the recipe T0's numbers were
// produced with. Sixteen agree. The seventeenth is re-pinned with its
// history: S54 T1's OWN documented change (git 9ab5cbbc, T1 Notes "fakeGraph
// ... shims in the S64/S22 daemon fixtures") extended that test with the
// audited-twin stubs the Graph interface gained; the old digest (eb0afc2b
// 0f2d593c) is recorded beside the new, because a pin re-hashed without
// attribution is a claim quietly rewritten and a pin re-hashed WITH its
// commit is exactly what happened.
var s54Pins = []struct {
	name, file, sha string
}{
	{"TestSearchQueryAbsentWhenRetrievalIsNot", "internal/protocol/retrieval_test.go", "27db11f72079b883"},
	{"TestStatusAndReindexAreAbsentWithoutTheirSeams", "internal/protocol/retrieval_t8_test.go", "45cf2e0378979a82"},
	{"TestSearchAbsentWhenRetrievalIsNot", "internal/tools/search_test.go", "50f06ded16a56b43"},
	{"TestSearchAbsentInNoRetrievalBuild", "internal/tools/search_noretrieval_test.go", "6569647c9cd28cf0"},
	{"TestRetrievalReindexSocketNeverAutoFires", "internal/daemon/reindex_async_test.go", "bed958615ecc5e23"},
	{"TestSingleFlightPerRootAndCorpus", "internal/daemon/reindex_async_test.go", "5784aef315012162"},
	{"TestStatusMergesTheRunningFigures", "internal/daemon/reindex_async_test.go", "94467da51637d138"},
	{"TestRetrievalDoorsShareOneWiring", "internal/daemon/reindex_async_test.go", "85aea92539d162d2"},
	{"TestGraphMethodsAbsentWithoutSeam", "internal/protocol/graph_test.go", "56dbf9398224aea8"},
	{"TestGraphMethodsRegisterWithSeam", "internal/protocol/graph_test.go", "99b955adaa880bbf"}, // was eb0afc2b0f2d593c until 9ab5cbbc (S54 T1's audited-twin shim)
	{"TestGraphToolAndSocketShareOneService", "internal/daemon/graph_test.go", "a08d010e60ebb27b"},
	{"TestGraphWriteHookWiredAtBothStepRunners", "internal/daemon/graph_test.go", "990c9797a919a55f"},
	{"TestTelemetryReadsRefuseBadArgumentsAndAbsentStores", "internal/protocol/telemetry_test.go", "a35ac6f078bf5cc2"},
	{"TestNewTelemetrySinkNilStoreIsNil", "internal/daemon/telemetry_seam_test.go", "8914379525d40bc5"},
	{"TestSurfaceEveryRegisteredMethodIsClassified", "internal/daemon/surface_completeness_test.go", "994518e9978460ae"},
	{"TestSurfaceDoorsEqualTheRealStructures", "internal/protocol/surface_test.go", "a0c5698c88520b16"},
	{"TestEveryMethodIsClassified", "cmd/aeon/tools_guard_test.go", "8703f5545d0f0b69"},
}

// pinnedBody extracts the whole function text — signature line through the
// closing brace, trailing newline kept — the recipe T0's digests were
// produced with. ok=false says the function is gone from the file.
func pinnedBody(file, name string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join("..", "..", file))
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func "+name+"(") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "}") {
			return strings.Join(lines[start:j+1], "\n") + "\n", true
		}
	}
	return "", false
}

func pinnedDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])[:16]
}

func TestS54NeighbourPinsAreUnchanged(t *testing.T) {
	for _, p := range s54Pins {
		body, ok := pinnedBody(p.file, p.name)
		if !ok {
			t.Errorf("%s is gone from %s — the pinned test was deleted, not re-run", p.name, p.file)
			continue
		}
		if got := pinnedDigest(body); got != p.sha {
			t.Errorf("%s's assertion body changed (%s): got %s, pinned %s — re-running a pin means UNCHANGED, and a pin re-hashed to pass is a claim quietly rewritten",
				p.name, p.file, got, p.sha)
		}
	}
}
