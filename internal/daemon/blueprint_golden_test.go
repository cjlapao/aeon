package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_golden_test.go — S48 T3 Phase A (characterization FIRST): the
// file-era behaviour of EVERY blueprint reader is recorded byte-for-byte in
// testdata/bpgolden/daemon.json, green on untouched code. Phase B re-runs
// the same golden with the same bytes delivered from rows (seeded below by
// a test-only loader that mimics T4's adoption importer), and the only
// permitted drifts are the listed ones (T3 Notes). `BP_GOLDEN_UPDATE=1`
// rewrites the golden — legitimate ONLY on pre-change code or for a drift
// the task record names.

const bpGoldenCustom = `id: golden-custom
schemaVersion: 1
version: 1
lanes:
  - id: approve
    step: {kind: gate, question: go}
questions:
  go:
    prompt: Continue?
    answers: [yes, no]
`

const bpGoldenBroken = `id: golden-broken
schemaVersion: 1
version: 1
lanes: [oops
`

func bpGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	dir := filepath.Join("testdata", "bpgolden")
	path := filepath.Join(dir, name)
	if os.Getenv("BP_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden rewritten: %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (%v) — generate it with BP_GOLDEN_UPDATE=1 on the PRE-change code and commit it", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden %s drifted.\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func bpGoldenJSON(t *testing.T, v any, stateDir string) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte(stateDir), []byte("<STATE>"))
	// The atomic-save temp file name is random — the sentence matters, the
	// digits do not.
	raw = bpTempNameRE.ReplaceAll(raw, []byte(".blueprint-<RANDOM>.yaml"))
	return raw
}

var bpTempNameRE = regexp.MustCompile(`\.blueprint-\d+\.yaml`)

// bpGoldenFiles materializes every standard as a state-directory file
// exactly as Resolve does today (the embedded bytes verbatim), plus the
// listed variants: one EDITED standard copy (two-step gains a comment line
// — the file, not the embed, is what readers must serve) and two customs.
func bpGoldenFiles(t *testing.T, stateDir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range blueprint.StandardNames() {
		raw, ok, err := blueprint.EmbeddedSource().Get(name)
		if err != nil || !ok {
			t.Fatalf("standard %s missing from the embed: %v", name, err)
		}
		files[name] = string(raw)
	}
	files["two-step"] += "# edited by the operator\n"
	files["golden-custom"] = bpGoldenCustom
	files["golden-broken"] = bpGoldenBroken
	dir := filepath.Join(stateDir, "blueprints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// bpSeedRows is the TEST-ONLY loader that mimics T4's adoption
// importer: each VALID file becomes a row (op adopt, origin system for
// standards and user for customs, rev 1, hash of content); an invalid file
// is refused, exactly as T4 refuses — its absence from the table is itself
// a characterization fact.
func bpSeedRows(t *testing.T, s *store.Store, files map[string]string) {
	t.Helper()
	for name, content := range files {
		bp, err := blueprint.Load([]byte(content))
		if err != nil {
			continue // the adoption refusal
		}
		sum := sha256.Sum256([]byte(content))
		origin := "user"
		for _, std := range blueprint.StandardNames() {
			if std == name {
				origin = "system"
			}
		}
		row := store.BlueprintRow{Name: name, Version: bp.Version, Content: content,
			ContentHash: hex.EncodeToString(sum[:]), Origin: origin, UpdatedBy: "user:test"}
		rev := store.RevisionRow{Version: bp.Version, Content: content,
			ContentHash: hex.EncodeToString(sum[:]), Op: "adopt", ActorKind: "user", ActorID: "test"}
		if err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
			return store.BlueprintPutTx(tx, row, rev, nil)
		}); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
}

// seedEmbeddedRows lifts the shipped bytes of the named standards into the
// store as rows — the mechanical S48 T3 collateral for tests that ask a
// shipped NAME of a row-backed door (T5's seed set is the production side).
func seedEmbeddedRows(t *testing.T, s *store.Store, names ...string) {
	t.Helper()
	files := map[string]string{}
	for _, n := range names {
		raw, ok, err := blueprint.EmbeddedSource().Get(n)
		if err != nil || !ok {
			t.Fatalf("shipped %s: %v", n, err)
		}
		files[n] = string(raw)
	}
	bpSeedRows(t, s, files)
}

func TestBlueprintGoldenDaemon(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	s := recoveryStore(t)
	d := New(Options{Store: s})
	files := bpGoldenFiles(t, state)
	bpSeedRows(t, s, files)

	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	doc := map[string]any{}

	listAny, err := d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	// updatedAt is a WALL-CLOCK stamp (the boot arrival writes it fresh
	// every process) — a golden that pins a clock fails on the next
	// minute. The field's SHAPE is pinned by TestListKeepsNamesAndAddsSummaries.
	for i := range listAny.Blueprints {
		listAny.Blueprints[i].UpdatedAt = ""
	}
	doc["list"] = listAny
	doc["fields"] = d.Table.BlueprintFields()

	shows := map[string]any{}
	for _, name := range names {
		v, err := d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: name})
		if err != nil {
			shows[name] = map[string]any{"error": err.Error()}
			continue
		}
		// The REPORT is dropped from the golden: Analyze judges against
		// the LIVE handler/action registries, which other tests in this
		// package mutate (suite-position noise, not wire drift). Report
		// equality with Analyze-verbatim is pinned by
		// TestShowKeepsShapeAndAddsReportAndRefs, which compares against
		// the same process's Analyze and moves with the registries.
		v.Report = nil
		v.UpdatedAt = ""
		shows[name] = v
	}
	doc["show"] = shows

	validates := map[string]any{}
	validateOne := func(label string, p protocol.BlueprintValidateParams) {
		v, err := d.Table.ValidateBlueprint(p)
		if err != nil {
			validates[label] = map[string]any{"error": err.Error()}
			return
		}
		validates[label] = v
	}
	for _, name := range names {
		validateOne("name:"+name, protocol.BlueprintValidateParams{Name: name, Mode: "standard"})
	}
	validateOne("content:valid-custom", protocol.BlueprintValidateParams{Content: bpGoldenCustom, Mode: "standard"})
	validateOne("content:broken", protocol.BlueprintValidateParams{Content: bpGoldenBroken, Mode: "standard"})
	validateOne("content:garbage", protocol.BlueprintValidateParams{Content: "this is not yaml: [", Mode: "standard"})
	doc["validate"] = validates

	checks := map[string]any{}
	for _, name := range append(names, "no-such-blueprint") {
		if err := loop.CheckBlueprint(s, name, "standard"); err != nil {
			checks[name] = err.Error()
		} else {
			checks[name] = "ok"
		}
	}
	doc["fireTimeCheck"] = checks

	starts := map[string]any{}
	for _, name := range append(names, "no-such-blueprint") {
		if _, err := d.loadStartBlueprint(name); err != nil {
			starts[name] = err.Error()
		} else {
			starts[name] = "ok"
		}
	}
	doc["loadStart"] = starts

	// The pin: the parked gate custom starts for real (the orgStart pattern
	// keeps the runner from outliving the store) and the loop_blueprint
	// bytes are recorded verbatim.
	root := t.TempDir()
	p, err := s.CreateComponent(store.NewComponent{Name: "bp group", Slug: "bp-golden-group", Root: "/repo/bp-golden-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "child", Slug: "bp-golden-child", Root: root, ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: "golden-custom", Goal: "the golden run", WorkspaceRoot: root, Slug: "bp-golden"})
	l, err := s.GetLoop(store.Scope{LoopSlug: "bp-golden", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc["pin"] = map[string]any{"blueprintId": pl.BlueprintID, "version": pl.BlueprintVersion, "content": pl.Content}

	// Saves last: a successful save lands a new ROW (S48 T6 — rows, not
	// files) and would otherwise leak into list/show captured above.
	saves := map[string]any{}
	saveOne := func(label string, p protocol.BlueprintSaveParams) {
		v, err := d.Table.SaveBlueprint(context.Background(), protocol.Actor{Kind: "user", ID: "golden-op"}, p)
		if err != nil {
			saves[label] = fmt.Sprintf("%v", err)
			if v.Name != "" {
				saves[label+"#reportFailed"] = true
			}
			return
		}
		saves[label] = v
	}
	saveContent := "id: golden-save\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: go?\n    answers: [yes]\n"
	saveOne("valid-createOnly", protocol.BlueprintSaveParams{Name: "golden-save", Content: saveContent, CreateOnly: true})
	saveOne("collision-createOnly", protocol.BlueprintSaveParams{Name: "golden-save", Content: saveContent, CreateOnly: true})
	saveOne("id-mismatch", protocol.BlueprintSaveParams{Name: "golden-other", Content: saveContent})
	saveOne("invalid", protocol.BlueprintSaveParams{Name: "golden-bad-save", Content: bpGoldenBroken})
	saveOne("bad-name", protocol.BlueprintSaveParams{Name: "../escape", Content: saveContent})
	doc["save"] = saves

	bpGolden(t, "daemon.json", bpGoldenJSON(t, doc, state))
}
