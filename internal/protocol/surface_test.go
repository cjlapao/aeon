package protocol

// Surface tests (S47 T6). The table is a claim; these tests are the proof, in
// both directions and against the structures the claim is about — a door flag is
// compared with the structure that owns it, never with another string.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// surfaceTestTable is the fully wired test table, with the surface doors
// registered and the narrow seam supplied (the nine actions, spelled out — the
// daemon package proves them against internal/narrow itself).
// surfaceNineActions is the narrow projection as cmd/aeon/serve.go supplies it:
// the nine actions and the socket method each one rides. Spelled out here because
// internal/narrow imports this package and this package must never import it back;
// internal/daemon's TestSurfaceDoorsEqualTheRealStructures compares the same nine
// against narrow.Actions() itself, so this list cannot drift from the real one.
func surfaceNineActions() map[MethodName]string {
	return map[MethodName]string{
		"blueprint.validate": "aeon_blueprint_validate",
		"loop.list":          "aeon_loop_list",
		"loop.show":          "aeon_loop_show",
		"loop.start":         "aeon_loop_start",
		"question.pending":   "aeon_questions_open",
		"remote.complete":    "aeon_remote_complete",
		"step.show":          "aeon_step_show",
		"store.awaiting":     "aeon_questions_pending",
		"webhook.deliver":    "aeon_webhook_deliver",
	}
}

func surfaceTestTable(t *testing.T) (*Table, string) {
	t.Helper()
	tbl, conn := wiredToolsCatalog(t, &toolsCatalogStub{})
	tbl.NarrowMethods = surfaceNineActions
	if !tbl.RegisterSurface() {
		t.Fatal("RegisterSurface returned false for a table that exists — the surface doors are wired out of existence")
	}
	return tbl, conn
}

func dispatchSurface[T any](t *testing.T, tbl *Table, conn string, name MethodName, params json.RawMessage) T {
	t.Helper()
	return dispatchToolsResult[T](t, tbl, conn, name, params)
}

func surfaceEntriesByName(entries []SurfaceEntry) map[MethodName]SurfaceEntry {
	out := make(map[MethodName]SurfaceEntry, len(entries))
	for _, e := range entries {
		out[e.Method] = e
	}
	return out
}

func surfaceHasName(names []MethodName, want MethodName) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// ── rule 2: the doors are computed, and the computation tells the truth ────────

func TestSurfaceDoorsEqualTheRealStructures(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	got := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, json.RawMessage(`{}`))
	if got.Methods == nil {
		t.Fatal("surface.list with no filter must answer every method, not null")
	}
	declared := tbl.Surface()
	if len(declared) != len(got.Methods) {
		t.Fatalf("surface.list answered %d rows and Table.Surface() computed %d — one listing, one answer", len(got.Methods), len(declared))
	}

	services := map[MethodName]bool{}
	for _, m := range AgentServiceMethods() {
		services[m.Name] = true
	}
	twins := map[MethodName]string{}
	for _, spec := range records.Catalog() {
		if spec.Method != "" {
			twins[MethodName(spec.Method)] = spec.Name
		}
	}
	narrow := tbl.NarrowMethods()

	for _, e := range got.Methods {
		m, ok := tbl.Lookup(e.Method)
		if !ok {
			t.Fatalf("%s: surface.list listed a method the table does not hold", e.Method)
		}
		if !e.Doors.Socket {
			t.Errorf("%s: registered on the table and the socket door says false", e.Method)
		}
		if e.Doors.Public != !m.Debug {
			t.Errorf("%s: public door %v but Debug is %v — the public door IS !Debug", e.Method, e.Doors.Public, m.Debug)
		}
		if e.Debug != m.Debug || e.RequiresInitialize != m.RequiresInitialize {
			t.Errorf("%s: the entry says debug=%v requiresInitialize=%v, the table says %v/%v",
				e.Method, e.Debug, e.RequiresInitialize, m.Debug, m.RequiresInitialize)
		}
		wantNarrow, projected := narrow[e.Method]
		if !projected {
			wantNarrow = SurfaceNarrowNone
		}
		if e.Doors.Narrow != wantNarrow {
			t.Errorf("%s: narrow door %q, the wired action table says %q", e.Method, e.Doors.Narrow, wantNarrow)
		}
		if e.Doors.AgentService != services[e.Method] {
			t.Errorf("%s: agentService door %v, AgentServiceMethods() says %v", e.Method, e.Doors.AgentService, services[e.Method])
		}
		if e.Doors.ToolTwin != twins[e.Method] {
			t.Errorf("%s: toolTwin %q, the records catalog's Method field says %q", e.Method, e.Doors.ToolTwin, twins[e.Method])
		}
	}
}

// ── rule 3: the kind is cross-checked against the facts that already exist ─────

func TestSurfaceKindAgreesWithKnownFacts(t *testing.T) {
	tbl, _ := surfaceTestTable(t)
	table := surfaceTable()

	for _, spec := range records.Catalog() {
		name := MethodName(spec.Method)
		s, ok := table[name]
		if !ok {
			t.Fatalf("%s (%s) is a catalog action with no surface entry", spec.Name, spec.Method)
		}
		// A spec that mutates is a `write` — or a `control`, which is the same
		// fact seen from the running side: `run_start`/`loop.run` starts a loop
		// AND writes rows. That one name is the reason the allowance exists, and
		// it stays a named allowance rather than an open door.
		if spec.Mutates && s.Kind != SurfaceWrite && s.Kind != SurfaceControl {
			t.Errorf("%s: the catalog spec mutates and the table calls it %q, not write or control", spec.Method, s.Kind)
		}
		if !spec.Mutates && s.Kind != SurfaceRead {
			t.Errorf("%s: the catalog spec mutates nothing and the table calls it %q, not %q", spec.Method, s.Kind, SurfaceRead)
		}
	}

	for _, m := range AgentServiceMethods() {
		s, ok := table[m.Name]
		if !ok {
			t.Fatalf("%s is an agent service with no surface entry", m.Name)
		}
		if m.ReadOnly && s.Kind != SurfaceRead {
			t.Errorf("%s: a read-only agent service is classified %q", m.Name, s.Kind)
		}
		if !m.ReadOnly && s.Kind != SurfaceWrite && s.Kind != SurfaceControl && s.Kind != SurfaceDiagnostic {
			t.Errorf("%s: a writing agent service is classified %q — write, control or diagnostic are the honest answers", m.Name, s.Kind)
		}
	}

	// The Debug set is exactly the diagnostic set, in both directions: a Debug
	// method called a read would be a diagnostic on the public surface, and a
	// diagnostic nobody marked Debug would reach HTTP.
	debug := map[MethodName]bool{}
	for _, n := range surfaceTestDebugNames(tbl) {
		debug[n] = true
	}
	for n, s := range table {
		if debug[n] && s.Kind != SurfaceDiagnostic {
			t.Errorf("%s is Debug-marked and the table calls it %q", n, s.Kind)
		}
		if s.Kind == SurfaceDiagnostic && !debug[n] {
			t.Errorf("%s is classified diagnostic and nobody marked it Debug — it would reach the public surface", n)
		}
	}
}

// ── rule 4: HumanDoorOnly is a claim with a proof, both directions ────────────

func TestHumanDoorOnlyMeansAbsentFromEveryNonHumanDoor(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	list := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, json.RawMessage(`{}`))
	byName := surfaceEntriesByName(list.Methods)
	propose := surfaceProposeOnlyTwins()

	for _, e := range list.Methods {
		if !e.HumanDoorOnly {
			continue
		}
		if e.Doors.Narrow != SurfaceNarrowNone && e.Doors.Narrow != SurfaceNarrowUnknown {
			t.Errorf("%s is HumanDoorOnly and the narrow surface projects it as %q", e.Method, e.Doors.Narrow)
		}
		if e.Doors.AgentService {
			t.Errorf("%s is HumanDoorOnly and an agent service enumerates it", e.Method)
		}
		if e.Doors.ToolTwin != "" && !propose[e.Method] {
			t.Errorf("%s is HumanDoorOnly and the tool registry carries it as %q — the only exception is a named propose-only twin",
				e.Method, e.Doors.ToolTwin)
		}
	}

	// The other direction: the families the guard slices settled cannot be
	// quietly unmarked.
	for _, fam := range surfaceHumanDoorFamilies() {
		e, ok := byName[fam]
		if !ok {
			// A conditional family whose seam this table never wired is ABSENT
			// (V13), and absence is not a classification gap: the daemon package's
			// TestSurfaceEveryRegisteredMethodIsClassified walks a fully wired
			// table, where every one of these families does appear.
			if _, declared := surfaceClassify(fam); !declared {
				t.Errorf("%s is a settled human-door method and the table does not classify it at all", fam)
			}
			continue
		}
		if !e.HumanDoorOnly {
			t.Errorf("%s belongs to a settled human-door family and the table does not mark it", fam)
		}
	}
	for _, prefix := range surfaceHumanDoorPrefixes() {
		matched := 0
		for n, s := range surfaceTable() {
			if !strings.HasPrefix(string(n), prefix) {
				continue
			}
			matched++
			if !s.HumanDoorOnly {
				t.Errorf("%s matches the settled human-door prefix %q and the table leaves it unmarked", n, prefix)
			}
		}
		if matched == 0 {
			t.Errorf("the human-door prefix %q classifies no method at all — the family moved and the table did not follow", prefix)
		}
	}
}

// ── rule 5: the summaries are sentences a person can read ─────────────────────

func TestSurfaceSummariesAreSafeAndDistinct(t *testing.T) {
	table := surfaceTable()
	seen := map[string]MethodName{}
	for n, s := range table {
		if strings.TrimSpace(s.Summary) == "" {
			t.Errorf("%s has no summary — a blank row teaches a reader nothing", n)
			continue
		}
		if len(s.Summary) > 140 {
			t.Errorf("%s summary is %d characters, over the 140 the table promises", n, len(s.Summary))
		}
		if strings.IndexFunc(s.Summary, func(r rune) bool { return r < 0x20 }) >= 0 {
			t.Errorf("%s summary carries a control character: %q", n, s.Summary)
		}
		if strings.ContainsAny(s.Summary, "\n\t") {
			t.Errorf("%s summary is not a single line: %q", n, s.Summary)
		}
		key := strings.ToLower(s.Summary)
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s carry the same summary %q — a duplicate row tells a reader nothing", n, other, s.Summary)
		}
		seen[key] = n
	}
}

// ── the two doors answer, and answer daemon-side ──────────────────────────────

func TestSurfaceListFilters(t *testing.T) {
	tbl, conn := surfaceTestTable(t)

	all := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, json.RawMessage(`{}`))
	if all.Counts.Total != all.Counts.Unfiltered {
		t.Fatalf("an unfiltered listing must say the same total twice, got %d and %d", all.Counts.Total, all.Counts.Unfiltered)
	}
	if all.Statement != "" {
		t.Errorf("an unfiltered listing carries %q — the no-match sentence belongs to a filter", all.Statement)
	}
	// Every kind bucket is present and the buckets add up: a missing bucket is an
	// absence a client would read as zero without the daemon having said it.
	sum := 0
	for _, k := range AllSurfaceKinds() {
		v, ok := all.Counts.ByKind[k]
		if !ok {
			t.Fatalf("counts.byKind has no %q bucket", k)
		}
		sum += v
	}
	if sum != all.Counts.Total {
		t.Errorf("the kind buckets add to %d of %d rows", sum, all.Counts.Total)
	}
	for _, d := range doorWords() {
		if _, ok := all.Counts.ByDoor[d]; !ok && d != SurfaceDoorNarrow {
			t.Fatalf("counts.byDoor has no %q bucket", d)
		}
	}

	kind := SurfaceWrite
	writes := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Kind: &kind}))
	if writes.Counts.Total == 0 || writes.Counts.Total >= all.Counts.Total {
		t.Fatalf("the kind filter answered %d of %d — it filtered nothing", writes.Counts.Total, all.Counts.Total)
	}
	for _, e := range writes.Methods {
		if e.Kind != SurfaceWrite {
			t.Fatalf("a kind=write listing carries %s as %q", e.Method, e.Kind)
		}
	}
	if writes.Counts.Unfiltered != all.Counts.Total {
		t.Errorf("a filtered answer must still say how big the surface is: %d vs %d", writes.Counts.Unfiltered, all.Counts.Total)
	}

	door := SurfaceDoorAgentService
	svc := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Door: &door}))
	if len(svc.Methods) != len(AgentServiceMethods()) {
		t.Errorf("the agentService filter answered %d rows, the enumeration holds %d", len(svc.Methods), len(AgentServiceMethods()))
	}
	for _, e := range svc.Methods {
		if !e.Doors.AgentService {
			t.Fatalf("the agentService filter carried %s, which the enumeration does not hold", e.Method)
		}
	}

	human := true
	hdoor := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{HumanDoorOnly: &human}))
	if hdoor.Counts.Total == 0 {
		t.Fatal("no method is a human door — the guard slices would be unproven")
	}
	for _, e := range hdoor.Methods {
		if !e.HumanDoorOnly {
			t.Fatalf("humanDoorOnly=true answered %s, which is not one", e.Method)
		}
	}

	q := "webhook"
	qs := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Q: &q}))
	if !surfaceHasName(namesOfSurface(qs.Methods), MethodWebhookDeliver) {
		t.Errorf("the q filter dropped webhook.deliver, whose name carries the word")
	}
	for _, e := range qs.Methods {
		if !strings.Contains(strings.ToLower(string(e.Method)), q) && !strings.Contains(strings.ToLower(e.Summary), q) {
			t.Errorf("the q filter carried %s, which matches neither its name nor its summary", e.Method)
		}
	}

	// A closed set is closed: a kind nobody compiled is a refusal, not an empty list.
	bad := SurfaceKind("audit")
	_, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Kind: &bad}))
	var bp ErrBadParams
	if !errors.As(err, &bp) {
		t.Fatalf("kind=audit answered %v, want a refusal that names the closed set", err)
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("the kind refusal does not name the vocabulary it refused: %v", err)
	}
	badDoor := "narrower"
	if _, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Door: &badDoor})); err == nil {
		t.Fatal("door=narrower answered; the door set is closed")
	}
}

func TestSurfaceListFilteredEmptyIsTheFiltersAnswer(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	q := "no-method-can-carry-this-word"
	got := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Q: &q}))
	if got.Counts.Total != 0 {
		t.Fatalf("the filter matched %d rows", got.Counts.Total)
	}
	if got.Statement != surfaceListNoMatchStatement {
		t.Errorf("a filtered-empty listing carries %q, want the filter's own sentence", got.Statement)
	}
	if got.Methods == nil {
		t.Error("an empty listing must be [], never null")
	}
	if got.Counts.Unfiltered == 0 {
		t.Error("the answer must still say how many methods exist, so 'nothing matched' is not 'nothing exists'")
	}
}

func TestSurfaceGetAnswersOneMethodAndRefusesByNaming(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	got := dispatchSurface[SurfaceGetResult](t, tbl, conn, MethodSurfaceGet, json.RawMessage(`{"method":"loop.show"}`))
	if got.Entry.Method != MethodLoopShow {
		t.Fatalf("surface.get answered %q", got.Entry.Method)
	}
	if got.Entry.Kind != SurfaceRead {
		t.Errorf("loop.show is %q; the records spec is the authority and it does not mutate", got.Entry.Kind)
	}
	if !got.Entry.Doors.Socket || got.Entry.Doors.Narrow != "aeon_loop_show" {
		t.Errorf("loop.show's doors are %+v — it is registered and the narrow surface projects it", got.Entry.Doors)
	}

	_, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceGet, json.RawMessage(`{"method":"loop.shwo"}`))
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != ErrSurfaceUnknown.Code {
		t.Fatalf("a typo named %v, want the typed %d refusal", err, ErrSurfaceUnknown.Code)
	}
	if !strings.Contains(ae.Data.What, "loop.show") {
		t.Errorf("the refusal does not name the nearest method: %v", ae)
	}
	if _, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceGet, json.RawMessage(`{"method":"  "}`)); err == nil {
		t.Fatal("an empty method name answered; the read needs a name")
	}
	// The human door refuses an actor in the params (TC9/MC2): this answer is
	// about the daemon's surface and nobody's identity.
	if _, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceGet, json.RawMessage(`{"method":"loop.show","actor":{"kind":"agent","id":"atlas"}}`)); err == nil {
		t.Fatal("surface.get accepted an actor in params")
	} else if !strings.Contains(err.Error(), "actor") {
		t.Errorf("the refusal does not name the key it refused: %v", err)
	}
}

func TestSurfaceUnknownNarrowSeamReportsUnknownNotFalse(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	tbl.NarrowMethods = nil
	list := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, json.RawMessage(`{}`))
	for _, e := range list.Methods {
		if e.Doors.Narrow != SurfaceNarrowUnknown {
			t.Fatalf("%s reports narrow %q with the seam unwired — unknown is a value and must not collapse into none",
				e.Method, e.Doors.Narrow)
		}
	}
	// And the filter that would need it refuses, rather than answering empty.
	door := SurfaceDoorNarrow
	_, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceList, mustMarshal(t, SurfaceListFilter{Door: &door}))
	var ne *Error
	if !errors.As(err, &ne) || !strings.Contains(ne.Data.What, "unknown") {
		t.Fatalf("filtering by the narrow door without the seam answered %v; want the refusal that says why", err)
	}
}

func TestSurfaceReadsWriteNothing(t *testing.T) {
	tbl, conn := surfaceTestTable(t)
	events := 0
	tbl.OnEvent = func(kind string, payload any) { events++ }
	before := len(tbl.Methods())
	if _, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceList, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tbl.Dispatch(context.Background(), conn, MethodSurfaceGet, json.RawMessage(`{"method":"loop.show"}`)); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Errorf("the surface reads published %d events; a read is not an event", events)
	}
	if after := len(tbl.Methods()); after != before {
		t.Errorf("the table grew from %d to %d under two reads", before, after)
	}
	if got := dispatchSurface[SurfaceListResult](t, tbl, conn, MethodSurfaceList, json.RawMessage(`{}`)); got.Counts.Total != before {
		t.Errorf("a third read answered %d of %d — the answer is not stable across reads", got.Counts.Total, before)
	}
}

// TestSurfaceTableIsFunctionBuiltAndImportsNothingNew reads the package the way
// the compiler does: an appendable package-level table is a data structure a
// future change can grow without a test noticing, and a new import here would
// quietly change who can reach whom.
func TestSurfaceTableIsFunctionBuiltAndImportsNothingNew(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	file, ok := pkgs["protocol"]
	if !ok {
		t.Fatal("no protocol package in this directory")
	}
	imports := map[string]bool{}
	for name := range file.Files {
		if !strings.HasPrefix(name, "surface") {
			continue
		}
		f := file.Files[name]
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.GenDecl:
				if decl.Tok == token.VAR {
					t.Errorf("%s declares a package-level var (%v) — the table is built by a function, not appended to",
						name, decl.Specs)
				}
			}
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			imports[p] = true
		}
	}
	// The direction the seam exists to protect.
	// The strings are built from parts so this test cannot be the thing it
	// forbids: the package's own import guards scan for these spellings.
	const root = "github.com/cjlapao/aeon/internal/"
	for _, banned := range []string{
		root + "nar" + "row",
		root + "daem" + "on",
		root + "sto" + "re",
		root + "mc" + "p",
		"net", "net/" + "http", "net/" + "rpc", "os/" + "exec",
	} {
		if imports[banned] {
			t.Errorf("the surface files import %s — %s", banned,
				"the surface answers from the table and the records catalog; the narrow projection and every transport arrive through a seam or not at all")
		}
	}
	if !imports["github.com/cjlapao/aeon/internal/records"] {
		t.Error("the surface files do not import internal/records — the toolTwin door is computed FROM the catalog, not typed")
	}
}

// ── helpers used by the daemon package's completeness test too ────────────────

// surfaceTestDebugNames is the Debug set, read from the table rather than
// remembered: a list typed here would let a sixth Debug method join the public
// surface unnoticed.
func surfaceTestDebugNames(tbl *Table) []MethodName {
	var out []MethodName
	for _, n := range tbl.Methods() {
		if m, ok := tbl.Lookup(n); ok && m.Debug {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func namesOfSurface(entries []SurfaceEntry) []MethodName {
	out := make([]MethodName, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Method)
	}
	return out
}

var _ = fmt.Sprintf
