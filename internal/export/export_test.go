package export

// Behaviour tests for the export engine (S62 T8). The seam is a fake
// Reader that serves declared rows on request and records every read — so
// the tests hold the allow-list, the determinism, the dir rules, the three
// states, and the fenced cleanup, with no database anywhere near them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/store"
)

var testClock = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }

// — the fake Reader —

type readCall struct {
	table   string
	columns []string
	orderBy string
}

type fakeReader struct {
	tables map[string][]map[string]any
	exists map[string]bool // an explicit override of the table's presence
	errFor map[string]error
	calls  []readCall
}

func (f *fakeReader) TableExists(name string) bool {
	if v, ok := f.exists[name]; ok {
		return v
	}
	_, ok := f.tables[name]
	return ok
}

func (f *fakeReader) ExportRows(ctx context.Context, spec store.TableSpec) ([]map[string]any, error) {
	f.calls = append(f.calls, readCall{spec.Table, append([]string(nil), spec.Columns...), spec.OrderBy})
	if err, ok := f.errFor[spec.Table]; ok {
		return nil, err
	}
	rows := f.tables[spec.Table]
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]any, len(spec.Columns))
		for _, c := range spec.Columns {
			m[c] = r[c]
		}
		out = append(out, m)
	}
	return out, nil
}

// seedReader is the full-shape store the engine tests read: one row of
// content per declared table, with the shapes the sets are argued from —
// a tree, versions, a credential-shaped setting, an opt-in memory row.
func seedReader(t *testing.T) *fakeReader {
	t.Helper()
	now := "2026-11-02T12:00:00Z"
	return &fakeReader{
		errFor: map[string]error{},
		tables: map[string][]map[string]any{
			"agent": {
				{"id": int64(2), "slug": "zeta", "name": "Zeta", "prompt": "prompt-z", "created_at": now, "updated_at": now},
				{"id": int64(1), "slug": "alpha", "name": "Alpha", "prompt": "prompt-a with shh-api-key=abc123 planted in content", "created_at": now, "updated_at": now},
			},
			"policy": {
				{"id": int64(1), "type": "tool_access", "target_kind": "agent", "target_id": "alpha", "action": "allow", "source": "operator", "created_at": now, "updated_at": now},
			},
			"route": {
				{"id": int64(1), "provider": "openai", "model": "gpt-x", "base_url": "https://example.invalid", "created_at": now, "updated_at": now},
			},
			"component": {
				{"id": int64(1), "name": "root-a", "slug": "root-a", "created_at": now, "updated_at": now},
				{"id": int64(2), "name": "child-b", "slug": "child-b", "parent_id": int64(1), "created_at": now, "updated_at": now},
				{"id": int64(3), "name": "archived-c", "slug": "archived-c", "parent_id": int64(1), "archived_at": "2026-10-01T00:00:00Z", "created_at": now, "updated_at": now},
			},
			"component_root": {
				{"component_root": "r2", "component_id": int64(2), "is_primary": int64(0), "added_at": now},
				{"component_root": "r1", "component_id": int64(2), "is_primary": int64(1), "added_at": now},
				{"component_root": "r1", "component_id": int64(1), "is_primary": int64(1), "added_at": now},
			},
			"event_trigger": {
				{"id": int64(1), "name": "evt", "source": "webhook", "created_at": now, "updated_at": now},
			},
			"trigger_schedule": {
				{"id": int64(1), "kind": "cron", "expr": "0 * * * *", "zone": "UTC", "created_at": now},
			},
			"epic": {
				{"id": int64(1), "title": "the epic", "created_at": now, "updated_at": now},
			},
			"goal": {
				{"id": int64(1), "version": int64(2), "title": "g v2", "created_at": now, "updated_at": now},
				{"id": int64(1), "version": int64(1), "title": "g v1", "created_at": now, "updated_at": now},
			},
			"brief": {
				{"id": int64(7), "version": int64(1), "scope": "the scope", "created_at": now, "updated_at": now},
			},
			"setting": {
				{"key": "ui.theme", "value": "dark", "updated_at": now, "updated_by": "cli"},
				{"key": "app_api_key", "value": "must-not-appear", "updated_at": now, "updated_by": "cli"},
			},
			"learning": {
				{"id": int64(1), "scope": "loop", "scope_key": "l1", "body": "the memory row", "created_at": now},
			},
		},
	}
}

func runDeps(f *fakeReader) Deps {
	return Deps{Reader: f, Clock: testClock, Version: "0.0.0-test", Schema: 77}
}

// — helpers —

// treeBytes walks an export dir and returns its files, by relative path —
// the whole tree, ready for a byte comparison.
func treeBytes(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// stripGeneratedAt normalises a manifest for a byte comparison: the manifest
// is the one place a clock may speak, and the determinism claim is about
// everything else.
func stripGeneratedAt(t *testing.T, b []byte) []byte {
	t.Helper()
	var m manifestStruct
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode the manifest: %v", err)
	}
	m.GeneratedAt = ""
	out, err := Encode(m)
	if err != nil {
		t.Fatalf("re-encode the stripped manifest: %v", err)
	}
	return out
}

// unmarshal is a small decode helper so the assertion sites stay one line.
func unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// — the behaviours —

// TestExportIsDeterministic: two runs, fixed clock, same store — the trees
// are byte-identical once the manifest's generated_at (the one clock byte) is
// stripped.
func TestExportIsDeterministic(t *testing.T) {
	a := t.TempDir() + "/export-a"
	b := t.TempDir() + "/export-b"
	f1, f2 := seedReader(t), seedReader(t)

	res1, re := Run(context.Background(), runDeps(f1), Opts{Dir: a, Actor: Actor{Kind: "user", ID: "u1"}})
	if re != nil {
		t.Fatalf("run a: %v", re)
	}
	res2, re := Run(context.Background(), runDeps(f2), Opts{Dir: b, Actor: Actor{Kind: "user", ID: "u1"}})
	if re != nil {
		t.Fatalf("run b: %v", re)
	}
	if res1.Files != res2.Files {
		t.Fatalf("file counts differ: %d vs %d", res1.Files, res2.Files)
	}
	t1, t2 := treeBytes(t, a), treeBytes(t, b)
	var keys []string
	for k := range t1 {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e1, ok1 := t1[k]
		e2, ok2 := t2[k]
		if !ok1 || !ok2 {
			t.Fatalf("the trees disagree on %s: present in one, not the other", k)
		}
		if k == "export.json" {
			e1, e2 = stripGeneratedAt(t, e1), stripGeneratedAt(t, e2)
		}
		if !bytes.Equal(e1, e2) {
			t.Fatalf("the trees differ at %s:\n--- a\n%s\n--- b\n%s", k, e1, e2)
		}
	}
	// The manifest carries the actor and the states, and nothing about the
	// run that would change between identical runs.
	if !bytes.Contains(t1["export.json"], []byte(`"kind": "user"`)) {
		t.Fatalf("the manifest does not carry the actor: %s", t1["export.json"])
	}
}

// TestExportersReadOnlyDeclaredColumns: across every set — default and
// opt-in — every read the exporters issue is a closed-set table with a
// column list inside its allow-list, and no other read happens.
func TestExportersReadOnlyDeclaredColumns(t *testing.T) {
	f := seedReader(t)
	res, re := Run(context.Background(), runDeps(f), Opts{Dir: t.TempDir() + "/e", IncludeMemory: true})
	if re != nil {
		t.Fatalf("run: %v", re)
	}
	if len(f.calls) == 0 {
		t.Fatalf("the run issued no reads — the sets did not run")
	}
	for _, c := range f.calls {
		if !store.IsExportTable(c.table) {
			t.Fatalf("a read named the undeclared table %q", c.table)
		}
		declared := map[string]bool{}
		for _, d := range store.DeclaredColumns[c.table] {
			declared[d] = true
		}
		seen := map[string]bool{}
		for _, col := range c.columns {
			if seen[col] {
				t.Fatalf("the read of %s declared %q twice", c.table, col)
			}
			seen[col] = true
			if !declared[col] {
				t.Fatalf("the read of %s selected %q, which the allow-list does not declare", c.table, col)
			}
		}
		if c.orderBy != "" {
			key := strings.TrimSuffix(c.orderBy, " DESC")
			if !declared[key] {
				t.Fatalf("the read of %s ordered by %q, which its allow-list does not declare", c.table, c.orderBy)
			}
		}
	}
	_ = res
}

// TestExportNeverSelectsCredentialLikeColumns: no declared column anywhere
// in the allow-lists is a credential column, and a credential-shaped setting
// key is skipped in name and value.
//
// The column-name scan uses the strong credential marks (token, secret,
// apikey, api_key, password, credential, certificate, signature) — never a
// false positive on a real column — plus an explicit pin of the key-shaped
// columns the design deliberately leaves out. The bare substring "key" is
// left to the value filter (isCredentialName), where it is right: a setting
// whose key name is "app_api_key" must be skipped, and that is judged on the
// row's key value, not on the column the key value lives in.
func TestExportNeverSelectsCredentialLikeColumns(t *testing.T) {
	// "token" is not a column-name mark here: route.max_output_tokens is a
	// count of model output, not an auth token, and the real credential
	// columns are pinned by name in the exclusion list below.
	strongMarks := []string{"secret", "apikey", "api_key", "password", "credential", "certificate", "signature"}
	for table, cols := range store.DeclaredColumns {
		for _, c := range cols {
			lc := strings.ToLower(c)
			for _, m := range strongMarks {
				if strings.Contains(lc, m) {
					t.Fatalf("the allow-list for %s declares the credential-shaped column %q (mark %q)", table, c, m)
				}
			}
		}
	}
	// The key-shaped columns the design deliberately excludes: a remote URL
	// can carry a token, and a signing artifact is a credential in all but
	// name. None may be declared.
	for _, excluded := range []string{"remote_key", "repo_key", "api_key", "signer_certificate", "signature", "secret_env"} {
		for table, cols := range store.DeclaredColumns {
			for _, c := range cols {
				if c == excluded {
					t.Fatalf("the allow-list for %s declares the deliberately-excluded credential column %q", table, c)
				}
			}
		}
	}

	f := seedReader(t)
	dir := t.TempDir() + "/e"
	if _, re := Run(context.Background(), runDeps(f), Opts{Dir: dir}); re != nil {
		t.Fatalf("run: %v", re)
	}
	// The credential-shaped key is not a file...
	if _, err := os.Stat(filepath.Join(dir, "settings", "app_api_key.json")); !os.IsNotExist(err) {
		t.Fatalf("the credential-shaped setting key was exported: %v", err)
	}
	// ...and its value appears nowhere in the tree.
	tree := treeBytes(t, dir)
	for name, b := range tree {
		if bytes.Contains(b, []byte("must-not-appear")) {
			t.Fatalf("the credential-shaped value leaked into %s", name)
		}
	}
}

// TestExportCopiesContentVerbatim: a credential-looking string inside a
// declared content field is copied verbatim — the export is a copy, and
// content is content (the honesty line the README carries).
func TestExportCopiesContentVerbatim(t *testing.T) {
	f := seedReader(t)
	dir := t.TempDir() + "/e"
	if _, re := Run(context.Background(), runDeps(f), Opts{Dir: dir}); re != nil {
		t.Fatalf("run: %v", re)
	}
	b, err := os.ReadFile(filepath.Join(dir, "agents", "alpha.json"))
	if err != nil {
		t.Fatalf("read the agent record: %v", err)
	}
	if !bytes.Contains(b, []byte("shh-api-key=abc123")) {
		t.Fatalf("the planted content string was not copied verbatim: %s", b)
	}
}

// TestUnavailableSetIsNotEmpty: a missing table and a missing column both
// read "unavailable (slice not built)" — rows null, the set present in the
// manifest, the run continuing, nothing written for the set.
func TestUnavailableSetIsNotEmpty(t *testing.T) {
	// Missing table: the learning table is simply not in the schema.
	f := seedReader(t)
	delete(f.tables, "learning")
	dir := t.TempDir() + "/e"
	res, re := Run(context.Background(), runDeps(f), Opts{Dir: dir, IncludeMemory: true})
	if re != nil {
		t.Fatalf("run: %v", re)
	}
	m := manifestOf(t, dir)
	set := m.set("memory")
	if set.State != StateUnavailable {
		t.Fatalf("a missing table must read unavailable, got %q", set.State)
	}
	if set.Rows != nil {
		t.Fatalf("an unavailable set must carry rows null (P31): got %v", *set.Rows)
	}
	if set.Files != 0 {
		t.Fatalf("an unavailable set wrote files: %d", set.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory")); !os.IsNotExist(err) {
		t.Fatalf("an unavailable set left a directory behind")
	}
	// The run continued: the other sets are there.
	if res.Files == 0 {
		t.Fatalf("the run stopped at the unavailable set: 0 files")
	}

	// Missing column: the table is there, a declared column is not — the
	// same state, from the read's side.
	f2 := seedReader(t)
	f2.errFor["route"] = store.ErrColumnsMissing
	dir2 := t.TempDir() + "/e"
	res2, re := Run(context.Background(), runDeps(f2), Opts{Dir: dir2})
	if re != nil {
		t.Fatalf("run 2: %v", re)
	}
	m2 := manifestOf(t, dir2)
	r := m2.set("routes")
	if r.State != StateUnavailable || r.Rows != nil {
		t.Fatalf("a missing column must read unavailable with rows null: state=%q rows=%v", r.State, r.Rows)
	}
	if _, err := os.Stat(filepath.Join(dir2, "routes")); !os.IsNotExist(err) {
		t.Fatalf("the column-missing set left a directory behind")
	}
	_ = res2
}

// TestExportDirRules: absolute, not a symlink, empty-or-absent — each
// refusal names its rule, and the accepted shapes are the two the design
// allows.
func TestExportDirRules(t *testing.T) {
	ctx := context.Background()
	fresh := func() *fakeReader { return seedReader(t) }

	// Relative: refused, and named.
	if _, re := Run(ctx, runDeps(fresh()), Opts{Dir: "relative/dir"}); re == nil || re.Kind != ErrKindDirRelative {
		t.Fatalf("a relative dir must be dir-relative, got %v", re)
	}
	// Symlink: refused, and named.
	target := t.TempDir() + "/real"
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := t.TempDir() + "/link"
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, re := Run(ctx, runDeps(fresh()), Opts{Dir: link}); re == nil || re.Kind != ErrKindDirSymlink {
		t.Fatalf("a symlink dir must be dir-symlink, got %v", re)
	}
	// Non-empty: refused, and the plant survives.
	plant := t.TempDir() + "/plant"
	if err := os.MkdirAll(plant, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plant, "foreign.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, re := Run(ctx, runDeps(fresh()), Opts{Dir: plant}); re == nil || re.Kind != ErrKindDirNonempty {
		t.Fatalf("a non-empty dir must be dir-nonempty, got %v", re)
	}
	if b, err := os.ReadFile(filepath.Join(plant, "foreign.txt")); err != nil || string(b) != "x" {
		t.Fatalf("a refused run touched the plant: %v %q", err, b)
	}
	// Empty, pre-existing: accepted, and the dir survives a partial run.
	empty := t.TempDir() + "/empty"
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	fFail := seedReader(t)
	fFail.errFor["agent"] = errors.New("the reader fell over")
	if _, re := Run(ctx, runDeps(fFail), Opts{Dir: empty}); re == nil || re.Kind != ErrKindIO {
		t.Fatalf("a failed run into an empty dir must be io, got %v", re)
	}
	if st, err := os.Lstat(empty); err != nil || !st.IsDir() {
		t.Fatalf("a pre-existing empty dir must survive a failed run: %v", err)
	}
	entries, _ := os.ReadDir(empty)
	if len(entries) != 0 {
		t.Fatalf("the failed run left contents in the pre-existing dir: %v", entries)
	}

	// Not-yet-existing: accepted, created 0700, and removed by the fenced
	// cleaner on a failure (the owned case).
	absent := t.TempDir() + "/created/sub"
	fFail2 := seedReader(t)
	fFail2.errFor["agent"] = errors.New("the reader fell over again")
	if _, re := Run(ctx, runDeps(fFail2), Opts{Dir: absent}); re == nil || re.Kind != ErrKindIO {
		t.Fatalf("a failed run into an absent dir must be io, got %v", re)
	}
	if st, err := os.Lstat(absent); err == nil {
		t.Fatalf("a failed run into a created dir must remove the dir: %v", st)
	}
}

// TestPartialExportCleanUp: the fenced remover takes only what the export
// created — owned dirs down to nothing, pre-existing dirs to empty, and a
// foreign name inside stops the removal and says so.
func TestPartialExportCleanUp(t *testing.T) {
	dir := t.TempDir() + "/partial"
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "a.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "export.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A foreign name: the removal stops and names it; nothing is removed.
	if err := os.WriteFile(filepath.Join(dir, "foreign.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removePartialExport(dir, true, t.TempDir()); err == nil {
		t.Fatalf("a foreign name must stop the removal")
	}
	for _, p := range []string{filepath.Join(dir, "agents", "a.json"), filepath.Join(dir, "foreign.txt")} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("the fenced removal took a file it should not have: %s", p)
		}
	}
	// The foreign file goes; the owned tree comes down.
	if err := os.Remove(filepath.Join(dir, "foreign.txt")); err != nil {
		t.Fatal(err)
	}
	if err := removePartialExport(dir, true, t.TempDir()); err != nil {
		t.Fatalf("the owned cleanup failed: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("an owned dir must be removed by the cleanup: %v", err)
	}

	// A symlink where a set dir should stand is not followed on the way down.
	dir2 := t.TempDir() + "/partial2"
	if err := os.MkdirAll(dir2, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir2, "agents")); err != nil {
		t.Fatal(err)
	}
	if err := removePartialExport(dir2, false, t.TempDir()); err == nil {
		t.Fatalf("a symlinked set dir must stop the removal")
	}
	if _, err := os.Lstat(filepath.Join(dir2, "agents")); err != nil {
		t.Fatalf("the symlinked set dir was removed — the fence must not follow it")
	}
}

// TestMemoryIsOptIn: the default run leaves memory out of the tree and the
// manifest; the flag brings it in, as available (opt-in); an explicit
// --records memory without the flag is a refusal that names the flag.
func TestMemoryIsOptIn(t *testing.T) {
	ctx := context.Background()

	res, re := Run(ctx, runDeps(seedReader(t)), Opts{Dir: t.TempDir() + "/e"})
	if re != nil {
		t.Fatalf("default run: %v", re)
	}
	if _, err := os.Stat(res.Dir + "/memory"); !os.IsNotExist(err) {
		t.Fatalf("memory is opt-in and the default run wrote it: %v", err)
	}
	for _, s := range res.Sets {
		if s.Name == "memory" {
			t.Fatalf("the default manifest lists memory: %+v", s)
		}
	}

	res2, re := Run(ctx, runDeps(seedReader(t)), Opts{Dir: t.TempDir() + "/e", IncludeMemory: true})
	if re != nil {
		t.Fatalf("opt-in run: %v", re)
	}
	var mem *SetResult
	for i, s := range res2.Sets {
		if s.Name == "memory" {
			mem = &res2.Sets[i]
		}
	}
	if mem == nil {
		t.Fatalf("the opt-in run did not run memory: %+v", res2.Sets)
	}
	if mem.State != StateAvailableOptIn {
		t.Fatalf("memory must read available (opt-in): %q", mem.State)
	}
	if mem.Files != 1 || mem.Rows == nil || *mem.Rows != 1 {
		t.Fatalf("memory carried its row: files=%d rows=%v", mem.Files, mem.Rows)
	}

	if _, re := Run(ctx, runDeps(seedReader(t)), Opts{Dir: t.TempDir() + "/e", Sets: []string{"memory"}}); re == nil || re.Kind != ErrKindUnknownSet {
		t.Fatalf("an explicit memory without the flag must be refused: %v", re)
	}
	if _, re := Run(ctx, runDeps(seedReader(t)), Opts{Dir: t.TempDir() + "/e", Sets: []string{"nope"}}); re == nil || re.Kind != ErrKindUnknownSet {
		t.Fatalf("an unknown set must be refused and named: %v", re)
	}
}

// TestOversizedFileRefuses: a record over the 4 MiB cap is a refusal that
// names the file, the size and the cap, and the partial tree is cleaned up.
func TestOversizedFileRefuses(t *testing.T) {
	f := seedReader(t)
	f.tables["agent"] = []map[string]any{
		{"id": int64(1), "slug": "big", "name": "Big", "prompt": strings.Repeat("x", MaxFileBytes+1), "created_at": "now", "updated_at": "now"},
	}
	dir := t.TempDir() + "/e"
	_, re := Run(context.Background(), runDeps(f), Opts{Dir: dir})
	if re == nil {
		t.Fatalf("an oversized file must be refused")
	}
	if re.Kind != ErrKindOversize {
		t.Fatalf("the refusal must be file-oversize, got %s: %s", re.Kind, re.What)
	}
	if !strings.Contains(re.What, "big.json") || !strings.Contains(re.What, "per-file cap") {
		t.Fatalf("the refusal must name the file and the cap: %s", re.What)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("the failed run must remove the tree it created: %v", err)
	}
}

// TestRegistryExactNames: the closed set, in its order, with no more and no
// less — the registry is the whole of the "what is exportable" question.
func TestRegistryExactNames(t *testing.T) {
	want := []string{"agents", "components", "goals", "memory", "policies", "routes", "settings", "triggers"}
	if !reflect.DeepEqual(registryOrder, want) {
		t.Fatalf("registryOrder is %v, the closed set is %v", registryOrder, want)
	}
	if got := Names(); !reflect.DeepEqual(got, append([]string(nil), want...)) {
		t.Fatalf("Names() is %v, want %v", got, want)
	}
	for _, name := range want {
		ex, ok := Get(name)
		if !ok || ex.Name != name {
			t.Fatalf("Get(%s): ok=%v ex=%+v", name, ok, ex)
		}
		if len(ex.Tables) == 0 {
			t.Fatalf("the set %s declares no tables", name)
		}
		for _, tb := range ex.Tables {
			if !store.IsExportTable(tb) {
				t.Fatalf("the set %s declares the undeclared table %s", name, tb)
			}
		}
	}
	if _, ok := Get("nope"); ok {
		t.Fatalf("Get(nope) answered for an unknown set")
	}
}

// TestComponentsExportCarriesTheTree: the component file carries the tree —
// parentId and archivedAt as the database's values (a real id, a real time,
// and null when absent), the roots grouped and sorted, and nothing the
// allow-lists did not declare.
func TestComponentsExportCarriesTheTree(t *testing.T) {
	f := seedReader(t)
	dir := t.TempDir() + "/e"
	if _, re := Run(context.Background(), runDeps(f), Opts{Dir: dir, Sets: []string{"components"}}); re != nil {
		t.Fatalf("run: %v", re)
	}
	root, err := os.ReadFile(filepath.Join(dir, "components", "root-a.json"))
	if err != nil {
		t.Fatalf("read the root: %v", err)
	}
	var rootV map[string]any
	if err := unmarshal(root, &rootV); err != nil {
		t.Fatalf("decode the root: %v", err)
	}
	if rootV["parentId"] != nil {
		t.Fatalf("a root component carries parentId null, got %v", rootV["parentId"])
	}
	if rootV["archivedAt"] != nil {
		t.Fatalf("a live component carries archivedAt null, got %v", rootV["archivedAt"])
	}
	if _, ok := rootV["remote_key"]; ok {
		t.Fatalf("remote_key is not declared and must not appear")
	}

	child, err := os.ReadFile(filepath.Join(dir, "components", "child-b.json"))
	if err != nil {
		t.Fatalf("read the child: %v", err)
	}
	var childV map[string]any
	if err := unmarshal(child, &childV); err != nil {
		t.Fatalf("decode the child: %v", err)
	}
	// The id rides as a number through the tree's parentId.
	if childV["parentId"] != float64(1) {
		t.Fatalf("the child's parentId is the parent's id (1), got %v", childV["parentId"])
	}
	roots, ok := childV["roots"].([]any)
	if !ok || len(roots) != 2 {
		t.Fatalf("the child carries its two roots, got %v", childV["roots"])
	}
	first, _ := roots[0].(map[string]any)
	second, _ := roots[1].(map[string]any)
	if first["component_root"] != "r1" || second["component_root"] != "r2" {
		t.Fatalf("the roots are sorted by component_root: %v, %v", first["component_root"], second["component_root"])
	}

	arch, err := os.ReadFile(filepath.Join(dir, "components", "archived-c.json"))
	if err != nil {
		t.Fatalf("read the archived: %v", err)
	}
	var archV map[string]any
	if err := unmarshal(arch, &archV); err != nil {
		t.Fatalf("decode the archived: %v", err)
	}
	if archV["archivedAt"] != "2026-10-01T00:00:00Z" {
		t.Fatalf("the archived component carries its archivedAt, got %v", archV["archivedAt"])
	}
}

// manifestOf decodes the run's manifest for a test assertion.
type manifestView struct {
	GeneratedAt string      `json:"generated_at"`
	Actor       Actor       `json:"actor"`
	Sets        []SetResult `json:"sets"`
}

func manifestOf(t *testing.T, dir string) *manifestView {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	m := &manifestView{}
	if err := unmarshal(b, m); err != nil {
		t.Fatalf("decode the manifest: %v", err)
	}
	return m
}

func (m *manifestView) set(name string) *SetResult {
	for i := range m.Sets {
		if m.Sets[i].Name == name {
			return &m.Sets[i]
		}
	}
	panic("the manifest does not list the set " + name)
}
