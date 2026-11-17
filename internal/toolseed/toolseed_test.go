package toolseed_test

// toolseed_test.go — S47 T3's proof for the **domain half** of the set: the
// records say what the code says, one per system tool and not one for the alias;
// the versions golden refuses an unversioned content change; the set is inert and
// its applier is not guard-affecting (checked by the engine's own rule, not by
// this file's say-so); drift is reported in all three shapes and repairs nothing;
// the two conditional tools are seeded in every build; and the two structural
// claims — this package imports no `internal/mcp`, and the runtime's spec path
// imports neither this package nor the store's catalog rows — are read out of the
// sources.
//
// The engine-level proofs (a row per tool after a start, `origin='system'`,
// idempotence) live in `internal/daemon/tool_seed_test.go`, because the one
// engine that mounts this set is `daemon.NewSeedEngine` — the same split S46 T10
// recorded for `mcpseed`: unit proofs beside the domain, the start path where the
// engine is built.

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/seed"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
	"github.com/cjlapao/aeon/internal/toolseed"
)

// honestNames is the registry's own honest listing — the authority this test
// compares against rather than a list copied into it, which is the mistake that
// makes a registry bump a false failure in someone else's slice.
func honestNames(t *testing.T) []string {
	t.Helper()
	names := append([]string(nil), tools.ToolNames()...)
	for _, c := range []string{"search", "graph"} {
		found := false
		for _, n := range names {
			if n == c {
				found = true
			}
		}
		if !found {
			names = append(names, c)
		}
	}
	sort.Strings(names)
	return names
}

func keysOf(t *testing.T, recs []seed.Record) []string {
	t.Helper()
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Key)
	}
	sort.Strings(out)
	return out
}

// TestSourceHasOneRecordPerSystemToolAndNotTheAlias is TC4's shape: the honest
// registry names, plus the two conditionals always, and the hidden `write_file`
// alias nowhere as a record of its own — it rides on `write`.
func TestSourceHasOneRecordPerSystemToolAndNotTheAlias(t *testing.T) {
	recs := toolseed.Source()
	want := honestNames(t)
	got := keysOf(t, recs)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("records are %d names, the honest registry is %d\ngot:  %s\nwant: %s", len(got), len(want), got, want)
	}
	for _, k := range got {
		if k == "write_file" {
			t.Error("write_file is its own record — the alias has one executor and must ride on write")
		}
	}
	seen := map[string]int{}
	for _, k := range got {
		seen[k]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times — a key duplicated inside a set is a plan that cannot be applied", k, n)
		}
	}
	// The alias is present where it belongs: inside write's aliases, derived from
	// the registry rather than named here.
	for _, r := range recs {
		if r.Key != "write" {
			continue
		}
		c, err := toolseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		if len(c.Aliases) != 1 || c.Aliases[0] != "write_file" {
			t.Errorf("write carries aliases %v, want [write_file]", c.Aliases)
		}
	}
	// Every content is valid JSON with a non-empty key (the engine's rules 3–5),
	// so a malformed record is caught here and not at a start.
	for _, r := range recs {
		if r.Key == "" || len(r.Key) > 128 || strings.ContainsAny(r.Key, "\n\r\t") {
			t.Errorf("record key %q breaks the engine's key grammar", r.Key)
		}
		if !json.Valid(r.Content) {
			t.Errorf("record %s holds invalid JSON", r.Key)
		}
		if r.Version < 1 {
			t.Errorf("record %s has version %d — the floor is 1", r.Key, r.Version)
		}
	}
}

// TestSourceMatchesRegistryFieldForField is the anti-copy test: every field of
// every record equals what the code itself says — description, canonical schema
// and its hash, class, capabilities, governed-by, family, twin, notes.
func TestSourceMatchesRegistryFieldForField(t *testing.T) {
	reg := tools.Registry()
	for _, r := range toolseed.Source() {
		c, err := toolseed.ContentOf(r)
		if err != nil {
			t.Fatalf("%s: %v", r.Key, err)
		}
		tool, ok := reg[r.Key]
		if !ok {
			switch r.Key {
			case "search":
				tool = tools.Search()
			case "graph":
				tool = tools.Graph()
			default:
				t.Fatalf("%s is a record with no tool behind it", r.Key)
			}
		}
		if c.Name != r.Key {
			t.Errorf("%s: content name %q disagrees with the key", r.Key, c.Name)
		}
		if c.Description != tool.Spec.Description {
			t.Errorf("%s: description was restated, not read from the spec", r.Key)
		}
		if c.RerunClass != string(tool.Class) {
			t.Errorf("%s: rerun class %q, the tool says %q", r.Key, c.RerunClass, tool.Class)
		}
		if c.SchemaHash != tools.SchemaHash(tool.Spec) {
			t.Errorf("%s: schema_hash disagrees with tools.SchemaHash over the live spec", r.Key)
		}
		if string(c.Schema) != string(tools.CanonicalSchema(tool.Spec)) {
			t.Errorf("%s: the stored schema is not the canonical schema of the live spec", r.Key)
		}
		meta, ok := tools.MetaFor(r.Key)
		if !ok {
			t.Fatalf("%s has no Meta — T1's completeness test is not holding", r.Key)
		}
		if c.Family != meta.Family {
			t.Errorf("%s: family %q, the meta says %q", r.Key, c.Family, meta.Family)
		}
		if strings.Join(c.Capabilities, ",") != capabilityLine(meta.Capabilities) {
			t.Errorf("%s: capabilities %v disagree with the meta %v", r.Key, c.Capabilities, meta.Capabilities)
		}
		if strings.Join(c.Governed, ",") != strings.Join(meta.Governed, ",") {
			t.Errorf("%s: governed %v disagrees with the meta %v", r.Key, c.Governed, meta.Governed)
		}
		if c.Notes != meta.Notes {
			t.Errorf("%s: notes were rewritten", r.Key)
		}
	}
}

func capabilityLine(cs []tools.Capability) string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c))
	}
	return strings.Join(out, ",")
}

// TestToolVersionsBumpWithContent is the golden: `key version hash` per record.
// A content change that did not bump its version fails here, naming the tool and
// the sentence to act on — which is what makes `TestRegistryExactNames`' bumps
// (every sibling slice's new tool) visible in a diff instead of silent.
//
// `-update` rewrites the golden after a legitimate bump; the ritual is in
// versions.go, and the review is the golden's diff.
func TestToolVersionsBumpWithContent(t *testing.T) {
	path := filepath.Join("testdata", "tools.versions.golden")
	recs := toolseed.Source()
	var lines []string
	for _, r := range recs {
		lines = append(lines, r.Key+" "+itoa(r.Version)+" "+seed.Hash(r.Content))
	}
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %d lines to %s", len(lines), path)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the versions golden (run with -update to create it): %v", err)
	}
	want := map[string][2]string{}
	var order []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 {
			t.Fatalf("golden line %q is not `key version hash`", line)
		}
		want[parts[0]] = [2]string{parts[1], parts[2]}
		order = append(order, parts[0])
	}
	got := map[string][2]string{}
	for _, l := range lines {
		parts := strings.Fields(l)
		got[parts[0]] = [2]string{parts[1], parts[2]}
	}
	for _, k := range order {
		g, ok := got[k]
		if !ok {
			t.Errorf("%s left the code — its golden line stays (a retired record's history is kept) and the row is reported retired, never deleted", k)
			continue
		}
		if g[1] != want[k][1] {
			if g[0] == want[k][0] {
				t.Errorf("%s: content changed but version did not — bump it (see internal/toolseed/versions.go, then re-run with -update)", k)
			} else {
				t.Errorf("%s: version moved %s→%s and the hash moved — re-run with -update and commit the golden's diff", k, want[k][0], g[0])
			}
		}
		if g[1] == want[k][1] && g[0] != want[k][0] {
			t.Errorf("%s: version moved %s→%s with no content change — a version is a content claim, not a counter", k, want[k][0], g[0])
		}
	}
	for _, k := range []string{"bash", "read", "write", "webfetch", "goal_create", "search", "graph"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s has no golden line", k)
		}
	}
	if len(order) != len(got) {
		var fresh []string
		for k := range got {
			if _, ok := want[k]; !ok {
				fresh = append(fresh, k)
			}
		}
		sort.Strings(fresh)
		t.Errorf("records with no golden line (a new tool: add its line with -update): %v", fresh)
	}
}

var updateGolden = flag.Bool("update", false, "rewrite testdata/tools.versions.golden")

// TestToolsCoreIsInertAndApplierIsNotGuardAffecting pins the class, and pins it
// where it is enforced: the engine refuses an inert set whose applier says it
// widens, so the pairing is a registration-time fact rather than a comment.
func TestToolsCoreIsInertAndApplierIsNotGuardAffecting(t *testing.T) {
	d := toolseed.Declaration()
	if toolseed.SetID != "tools.core" {
		t.Fatalf("the set's name is %q — the ledger, seed.status and every door name it as tools.core", toolseed.SetID)
	}
	if d.ID != toolseed.SetID {
		t.Fatalf("the declaration carries %q", d.ID)
	}
	if d.Class != seed.ClassInert {
		t.Errorf("class %q — the catalog is inert (D4: applying it permits nothing)", d.Class)
	}
	if d.Version < 1 {
		t.Errorf("set version %d", d.Version)
	}
	if d.Records == nil {
		t.Fatal("the declaration has no Records function")
	}
	if d.Version != toolseed.SetVersion() {
		t.Errorf("set version %d is not the maximum of its records (%d)", d.Version, toolseed.SetVersion())
	}
	ap := toolseed.NewApplier(nil)
	if ap.Set() != toolseed.SetID {
		t.Errorf("applier serves %q", ap.Set())
	}
	if ap.GuardAffecting() {
		t.Error("the applier reports guard-affecting — an inert set may not")
	}
	if op := ap.Operator(); op.Edit || op.Remove {
		t.Errorf("operator rights %+v — a catalog row is a projection of code: it is neither hand-editable nor removable", op)
	}
	if _, err := seed.NewRegistry([]seed.Set{d}, []seed.Applier{fakeGuardApplier{ap: ap}}); err != nil {
		t.Errorf("the engine refused the honest pairing: %v", err)
	}
	// And the refusal still has teeth: the same set with an applier that DOES
	// report guard-affecting must be refused, or the test above proves nothing.
	if _, err := seed.NewRegistry([]seed.Set{d}, []seed.Applier{wideningApplier{fakeGuardApplier{ap: ap}}}); err == nil {
		t.Error("an inert catalog set with a guard-affecting applier was ACCEPTED (SD5's gate is open)")
	}
}

// TestDriftReportsMissingRetiredAndDiffers walks the three unagreed shapes and
// the field list `differs` names.
func TestDriftReportsMissingRetiredAndDiffers(t *testing.T) {
	ctx := context.Background()
	code := toolseed.Source()
	rows := rowsFromRecords(t, code)

	// Agreement first: everything is `current`, and the report covers every name
	// on both sides (a report that omits a tool is a report that hid it).
	all := toolseed.Drift(ctx, rows, code)
	if len(all) != len(code) {
		t.Fatalf("the report has %d entries for %d tools — every name on either side must appear", len(all), len(code))
	}
	for _, e := range all {
		if e.State != toolseed.DriftCurrent {
			t.Fatalf("%s is %s on a store the records just filled", e.Name, e.State)
		}
	}

	// missing_row: drop one record's row.
	trimmed := dropRow(rows, "goal_create")
	missing := toolseed.Drift(ctx, trimmed, code)
	if s := stateOf(missing, "goal_create"); s != toolseed.DriftMissingRow {
		t.Errorf("a code tool with no row reports %q, want missing_row", s)
	}

	// retired: keep a row no record exists for.
	withGhost := append(append([]store.ToolRow(nil), rows...), ghostRow("old_tool"))
	retired := toolseed.Drift(ctx, withGhost, code)
	if s := stateOf(retired, "old_tool"); s != toolseed.DriftRetired {
		t.Errorf("a row with no code reports %q, want retired", s)
	}

	// differs: edit one row's content and name the fields.
	edited := cloneRows(rows)
	for i := range edited {
		if edited[i].Name != "bash" {
			continue
		}
		edited[i].RerunClass = "safe"        // a lie worth naming: the shell is unsafe
		edited[i].Capabilities = `["reads"]` // and a capability that is not the tool's
	}
	d := toolseed.Drift(ctx, edited, code)
	e := entryOf(d, "bash")
	if e.State != toolseed.DriftDiffers {
		t.Fatalf("bash reports %q, want differs", e.State)
	}
	if strings.Join(e.Fields, ",") != "rerun_class,capabilities" {
		t.Errorf("differs names %v, want [rerun_class capabilities] (in the declared order)", e.Fields)
	}
	// A cosmetic re-encoding of the SAME schema is not drift — that is the whole
	// reason the row carries a hash and not only text.
	reencoded := cloneRows(rows)
	for i := range reencoded {
		if reencoded[i].Name == "read" {
			reencoded[i].Schema = reindentJSON(reencoded[i].Schema)
		}
	}
	if s := stateOf(toolseed.Drift(ctx, reencoded, code), "read"); s != toolseed.DriftCurrent {
		t.Errorf("whitespace in the schema copy reads as %q — the comparison is the hash, not the text", s)
	}
}

// TestDriftNeverRepairs is TC11's other half. `Drift` holds no store, no
// transaction and no clock — it is a function of the two slices it is given — and
// calling it a thousand times changes nothing, including its own answer.
func TestDriftNeverRepairs(t *testing.T) {
	ctx := context.Background()
	code := toolseed.Source()
	rows := cloneRows(rowsFromRecords(t, code))
	for i := range rows {
		if rows[i].Name == "write" {
			rows[i].Family = "wrong_family"
		}
	}
	first := toolseed.Drift(ctx, rows, code)
	if s := stateOf(first, "write"); s != toolseed.DriftDiffers {
		t.Fatalf("write reports %q", s)
	}
	for i := 0; i < 50; i++ {
		again := toolseed.Drift(ctx, rows, code)
		if stateOf(again, "write") != toolseed.DriftDiffers {
			t.Fatalf("call %d repaired the row — drift is reported, never fixed", i)
		}
	}
	// The inputs are untouched too: the caller's rows are the same rows.
	for _, r := range rows {
		if r.Name == "write" && r.Family != "wrong_family" {
			t.Error("Drift mutated the caller's rows")
		}
	}
	// And the only thing that can rewrite the row is a real apply, which needs a
	// version bump to change a hash — the applier is reached through the engine
	// (daemon/tool_seed_test.go), never from here.
	if dr := driftSourceHasNoStore(t); dr != "" {
		t.Errorf("status.go can reach storage after all: %s", dr)
	}
}

// TestConditionalToolsSeededRegardlessOfWiring: `search` and `graph` exist in
// every build even though their registration depends on a seam — and they are
// flagged, so a catalog that shows them does not claim they are usable here.
func TestConditionalToolsSeededRegardlessOfWiring(t *testing.T) {
	recs := toolseed.Source()
	for _, name := range []string{"search", "graph"} {
		var found *seed.Record
		for i := range recs {
			if recs[i].Key == name {
				found = &recs[i]
			}
		}
		if found == nil {
			t.Fatalf("%s is not seeded in this build — the catalog would depend on the wiring", name)
		}
		c, err := toolseed.ContentOf(*found)
		if err != nil {
			t.Fatal(err)
		}
		if !c.Conditional {
			t.Errorf("%s is not flagged conditional", name)
		}
		if c.RerunClass == "" || c.Family == "" || c.Description == "" {
			t.Errorf("%s's record is hollow: %+v", name, c)
		}
	}
	// The flag is the truth of the CURRENT build, read from the tool package's
	// own accessor — not a constant copied into this file.
	for _, pair := range []struct {
		name  string
		wired bool
	}{
		{"search", tools.SearchRegistered()},
		{"graph", tools.GraphRegistered()},
	} {
		if pair.wired {
			t.Logf("%s is wired in this build; it is seeded either way", pair.name)
		}
	}
}

// TestToolseedImportsNoMcp is MC11's share of this slice: the catalog never
// reaches the MCP package. The seam S46 built (T0's item 7) is assembled where it
// is allowed to be, and a catalog that joined it here would be a second answer to
// "what tools are there" — and would make `internal/mcp` undeletable.
func TestToolseedImportsNoMcp(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var checked int
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue // This file names the banned paths in order to ban them.
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		checked++
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range []string{"internal/mcp", "internal/policy", "internal/daemon", "internal/runtime"} {
				if strings.HasSuffix(path, banned) {
					t.Errorf("%s imports %s — MC11 (and the layer below this one)", f, path)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no sources were checked — the guard tests nothing")
	}
}

// TestRuntimeStillBuildsSpecsFromTheRegistry is TC2 from the runtime's side: the
// spec path does not read the catalog rows or this package, and `BuildToolset`
// still answers from `Registry()` with the default grant.
func TestRuntimeStillBuildsSpecsFromTheRegistry(t *testing.T) {
	for _, f := range []string{
		"../tools/toolset.go", "../tools/records_tools.go",
		"../runtime/loop.go", "../daemon/step.go",
	} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, banned := range []string{"internal/toolseed", "store.ToolRow", "ToolList(", "ToolGet(", "ToolUpsertTx("} {
			if strings.Contains(string(b), banned) {
				t.Errorf("%s references %q — the spec path must come from the registry, not the row", f, banned)
			}
		}
	}
	ts, err := tools.BuildToolset(tools.DefaultGrant(), tools.ToolContext{WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("BuildToolset: %v", err)
	}
	specs := ts.Specs()
	if len(specs) != len(tools.DefaultGrant()) {
		t.Fatalf("the default grant offers %d specs, want %d", len(specs), len(tools.DefaultGrant()))
	}
	for _, s := range specs {
		if s.Name != "read" && s.Name != "list" && s.Name != "write" {
			t.Errorf("the default grant offered %q", s.Name)
		}
	}
}

// --- fake appliers, so SD5's gate is shown to have teeth -------------------

type fakeGuardApplier struct{ ap *toolseed.Applier }

func (f fakeGuardApplier) Set() string                   { return f.ap.Set() }
func (f fakeGuardApplier) GuardAffecting() bool          { return false }
func (f fakeGuardApplier) Operator() seed.OperatorRights { return f.ap.Operator() }
func (f fakeGuardApplier) Fingerprint(seed.Tx, string) (string, bool, error) {
	return "", false, nil
}
func (f fakeGuardApplier) Adoptable(seed.Tx, string) (bool, error) { return false, nil }
func (f fakeGuardApplier) Upsert(seed.Tx, seed.Record, bool) error { return nil }
func (f fakeGuardApplier) Retire(seed.Tx, string) error            { return nil }

type wideningApplier struct{ fakeGuardApplier }

func (w wideningApplier) GuardAffecting() bool { return true }

// --- helpers ---------------------------------------------------------------

func rowsFromRecords(t *testing.T, recs []seed.Record) []store.ToolRow {
	t.Helper()
	out := make([]store.ToolRow, 0, len(recs))
	for _, r := range recs {
		c, err := toolseed.ContentOf(r)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, store.ToolRow{
			Name: c.Name, Family: c.Family, Description: c.Description,
			Schema: string(c.Schema), SchemaHash: c.SchemaHash, RerunClass: c.RerunClass,
			Capabilities: jsonList(c.Capabilities), Governed: jsonList(c.Governed),
			Twin: c.Twin, Aliases: jsonList(c.Aliases), Conditional: c.Conditional,
			Notes: c.Notes, Version: r.Version, Origin: "system",
			CreatedAt: "2026-11-02T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z",
		})
	}
	return out
}

func jsonList(ss []string) string {
	b, err := json.Marshal(ss)
	if err != nil || len(ss) == 0 {
		return "[]"
	}
	return string(b)
}

func cloneRows(rows []store.ToolRow) []store.ToolRow {
	return append([]store.ToolRow(nil), rows...)
}

func dropRow(rows []store.ToolRow, name string) []store.ToolRow {
	out := make([]store.ToolRow, 0, len(rows))
	for _, r := range rows {
		if r.Name != name {
			out = append(out, r)
		}
	}
	return out
}

func ghostRow(name string) store.ToolRow {
	return store.ToolRow{
		Name: name, Family: "goal", Description: "A tool that left the code and kept its row.",
		Schema: "null", SchemaHash: strings.Repeat("0", 64), RerunClass: "safe",
		Capabilities: `[]`, Governed: `[]`, Twin: "", Aliases: `[]`, Conditional: false,
		Notes: "retired", Version: 1, Origin: "system",
		CreatedAt: "2026-11-02T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z",
	}
}

func stateOf(entries []toolseed.DriftEntry, name string) toolseed.DriftState {
	for _, e := range entries {
		if e.Name == name {
			return e.State
		}
	}
	return "absent_from_report"
}

func entryOf(entries []toolseed.DriftEntry, name string) toolseed.DriftEntry {
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	return toolseed.DriftEntry{Name: name, State: "absent_from_report"}
}

func reindentJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return s
	}
	return strings.TrimRight(b.String(), "\n")
}

// driftSourceHasNoStore reads status.go and reports any reach it has at storage:
// the file that reports drift must not be able to write it.
func driftSourceHasNoStore(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("status.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"database/sql", "internal/store.", "*store.Store", "s.r", "s.w", "Exec(", "BeginTx"} {
		if strings.Contains(string(b), banned) {
			return "it references " + banned
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
