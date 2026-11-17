package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_t3_test.go — S48 T3 (BP2/BP4/BP12): the rows are the source.

const t3GateYAML = "id: t3-edit\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: approve\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: Continue?\n    answers: [yes, no]\n"

func t3Env(t *testing.T) (*Daemon, *store.Store, string) {
	t.Helper()
	t.Setenv("AEON_STATE_DIR", t.TempDir())
	s := recoveryStore(t)
	d := New(Options{Store: s})
	root := t.TempDir()
	p, err := s.CreateComponent(store.NewComponent{Name: "t3 group", Slug: "t3-group", Root: "/repo/t3-group"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComponent(store.NewComponent{Name: "t3 child", Slug: "t3-child", Root: root, ParentID: p.Component.ID}); err != nil {
		t.Fatal(err)
	}
	return d, s, root
}

func TestStartPinsTheRowContentVerbatim(t *testing.T) {
	d, s, root := t3Env(t)
	content := strings.Replace(t3GateYAML, "id: t3-edit", "id: t3-pin", 1)
	bpSeedRows(t, s, map[string]string{"t3-pin": content})
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: "t3-pin", Goal: "pin me", WorkspaceRoot: root, Slug: "t3-pin-run"})
	l, err := s.GetLoop(store.Scope{LoopSlug: "t3-pin-run", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Content != content {
		t.Fatalf("the pin is not the row's bytes verbatim:\n%q\n%q", pl.Content, content)
	}
	raw, ok, err := blueprint.Get("t3-pin")
	if err != nil || !ok || string(raw) != content {
		t.Fatalf("source read disagrees: %v %v", ok, err)
	}
}

func TestEditVisibleToNextStartAndFireValidation(t *testing.T) {
	d, s, root := t3Env(t)
	bpSeedRows(t, s, map[string]string{"t3-edit": t3GateYAML})
	if err := loop.CheckBlueprint(s, "t3-edit", "standard"); err != nil {
		t.Fatalf("the first content should pass: %v", err)
	}
	// Break it in the table — the NEXT fire must see it with no restart
	// (N8: no cache anywhere on this path).
	bad := "id: t3-edit\nschemaVersion: 1\nversion: 1\nlanes: [oops\n"
	bpForceRow(t, s, "t3-edit", bad) // straight PutTx — the adoption seeder would REFUSE invalid bytes
	if err := loop.CheckBlueprint(s, "t3-edit", "standard"); err == nil {
		t.Fatal("the fire-time door read a cached copy — N8 broken")
	}
	if _, err := d.Table.StartBlueprintLoop(context.Background(), protocol.LoopStartParams{Blueprint: "t3-edit", Goal: "g", WorkspaceRoot: root, Slug: "t3-bad-start"}); err == nil {
		t.Fatal("a start sailed past the broken row")
	}
	// Fix it with a second valid content — visible immediately, and the
	// NEXT pin carries the NEW bytes, not the old ones.
	fixed := strings.Replace(t3GateYAML, "Continue?", "Continue now?", 1)
	bpSeedRows(t, s, map[string]string{"t3-edit": fixed})
	if err := loop.CheckBlueprint(s, "t3-edit", "standard"); err != nil {
		t.Fatalf("the fixed row is still refused: %v", err)
	}
	orgStart(t, d, s, protocol.LoopStartParams{Blueprint: "t3-edit", Goal: "g", WorkspaceRoot: root, Slug: "t3-fixed-run"})
	l, err := s.GetLoop(store.Scope{LoopSlug: "t3-fixed-run", ComponentRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := s.ReadPinnedLoop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Content != fixed {
		t.Fatal("the pin held stale bytes after the edit")
	}
}

// bpForceRow writes any bytes as a row (bypassing the Load-and-refuse of
// the adoption-style seeder): the table CAN hold content the validators
// refuse, and the fire-time path must say so live.
func bpForceRow(t *testing.T, s *store.Store, name, content string) {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	row := store.BlueprintRow{Name: name, Version: 1, Content: content,
		ContentHash: hex.EncodeToString(sum[:]), Origin: "user", UpdatedBy: "user:test"}
	rev := store.RevisionRow{Version: 1, Content: content,
		ContentHash: hex.EncodeToString(sum[:]), Op: "save", ActorKind: "user", ActorID: "test"}
	if err := s.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
		return store.BlueprintPutTx(tx, row, rev, nil)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownBlueprintRefusesWithTodaysSentence(t *testing.T) {
	d, s, _ := t3Env(t)
	want := blueprint.NewErrUnknownBlueprint("no-such-blueprint").Error()
	got := map[string]string{}
	if _, err := d.loadStartBlueprint("no-such-blueprint"); err == nil || err.Error() != want {
		got["loadStartBlueprint"] = errString(err)
	}
	if err := loop.CheckBlueprint(s, "no-such-blueprint", "standard"); err == nil || err.Error() != want {
		got["loop.CheckBlueprint"] = errString(err)
	}
	if _, err := d.Table.ValidateBlueprint(protocol.BlueprintValidateParams{Name: "no-such-blueprint"}); err == nil || err.Error() != want {
		got["blueprint.validate"] = errString(err)
	}
	if _, err := d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: "no-such-blueprint"}); err == nil || err.Error() != want {
		got["blueprint.show"] = errString(err)
	}
	if len(got) > 0 {
		t.Fatalf("doors disagreeing with the one three-part sentence (%q): %v", want, got)
	}
	// The Phase A golden records the same refusal for the same case.
	raw, err := os.ReadFile(filepath.Join("testdata", "bpgolden", "daemon.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		FireTimeCheck map[string]string `json:"fireTimeCheck"`
		LoadStart     map[string]string `json:"loadStart"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for label, m := range map[string]map[string]string{"fireTimeCheck": doc.FireTimeCheck, "loadStart": doc.LoadStart} {
		if got := m["no-such-blueprint"]; got != want {
			t.Fatalf("golden %s no-such-blueprint = %q, want the golden sentence %q", label, got, want)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil — the door SPOKE NOTHING>"
	}
	return err.Error()
}

func TestListShowCompatShapes(t *testing.T) {
	d, s, _ := t3Env(t)
	bpSeedRows(t, s, map[string]string{"t3-shape": t3GateYAML})
	// S48 T7 grew these answers ADDITIVELY (the spec's own DoD: names and
	// path stay, summaries join). The compat this test pins — names is
	// still the names list, path is still "" — is asserted on the typed
	// fields now; the key set is the T7 shape.
	list, err := d.Table.ListBlueprints(protocol.BlueprintListParams{})
	if err != nil {
		t.Fatal(err)
	}
	// The boot arrival net may or may not have run for this daemon yet
	// (suite-position dependent) — the pin is SHAPE: names present,
	// sorted, ours among them.
	sorted := true
	for i := 1; i < len(list.Names); i++ {
		if list.Names[i-1] > list.Names[i] {
			sorted = false
		}
	}
	if !sorted || !strings.Contains(strings.Join(list.Names, ","), "t3-shape") {
		t.Fatalf("names lost its shape (BP12): %v", list.Names)
	}
	if len(list.Blueprints) != len(list.Names) {
		t.Fatalf("the additive summaries do not match names: %+v", list.Blueprints)
	}
	show, err := d.Table.ShowBlueprint(protocol.BlueprintShowParams{Name: "t3-shape"})
	if err != nil {
		t.Fatal(err)
	}
	if show.Path != "" {
		t.Fatalf("show.path must be \"\" now, got %q", show.Path)
	}
	if show.Content != t3GateYAML {
		t.Fatal("show.content is not the row's bytes")
	}
}

func TestPinnedLoopsUnaffected(t *testing.T) {
	_, s, _ := t3Env(t)
	// A pin born BEFORE any source change (S8-style pinned content that
	// matches no row) must survive every later install, read, and start.
	pinned := "id: pre-switch\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: a\n    step: {kind: gate, question: q}\nquestions:\n  q:\n    prompt: q?\n    answers: [yes]\n"
	created, err := s.CreatePinnedLoop(store.NewLoop{Slug: "t3-pre", Title: "pre switch", Root: "/repo/t3-group", ComponentRoot: "/repo/t3-group", Mode: "standard", Status: "waiting"}, "pre-switch", 1, pinned, "{}")
	if err != nil {
		t.Fatal(err)
	}
	blueprint.UseEmbeddedForOfflineTools() // a source change after the pin
	pl, err := s.ReadPinnedLoop(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pl.Content != pinned {
		t.Fatal("a source switch disturbed a pinned loop's bytes")
	}
	// Re-pin at BeginRun keeps the pinned copy (T0 reference inventory:
	// loop_blueprint is the pin, rows are the definition — BP10).
	if err := s.SavePinnedRuntime(created.ID, "waiting", `{"note":"reread"}`); err != nil {
		t.Fatal(err)
	}
	if pl2, err := s.ReadPinnedLoop(created.ID); err != nil || pl2.Content != pinned {
		t.Fatalf("runtime save disturbed the pin: %v", err)
	}
}

// TestNoRuntimeReadOfABlueprintFile — acceptance 1, grep-shaped: the file
// trio and any os.ReadFile touching blueprint content live ONLY in the
// named legacy holders (the save seam until T6 and the CLI `path` verb
// until T11), the importer (T4's file), and tests.
func TestNoRuntimeReadOfABlueprintFile(t *testing.T) {
	allowed := map[string]bool{
		filepath.Join("internal", "blueprint", "standard.go"): true, // the definitions
		filepath.Join("internal", "daemon", "blueprint.go"):   true, // save until T6
		filepath.Join("cmd", "aeon", "blueprint.go"):          true, // path verb until T11
		filepath.Join("cmd", "aeon", "blueprint_adopt.go"):    true, // T4 adoption: the one sanctioned importer
		filepath.Join("cmd", "aeon", "doctor.go"):             true, // T4 doctor: read-only drift comparison
	}
	hits := walkRepoGo(t, allowed, func(line string) bool {
		if strings.Contains(line, "LegacyResolve") || strings.Contains(line, "LegacyNamedPath") || strings.Contains(line, "LegacyNames") {
			return true
		}
		return strings.Contains(line, "os.ReadFile") && strings.Contains(strings.ToLower(line), "blueprint")
	})
	if len(hits) > 0 {
		t.Fatalf("a runtime path reads blueprint files again:\n%s", strings.Join(hits, "\n"))
	}
}

// TestProductionNeverUsesEmbeddedSource — the offline install is reachable
// ONLY from the named CLI verbs and the daemon installer's re-arm; never
// from serve, never a fallback inside a reader.
func TestProductionNeverUsesEmbeddedSource(t *testing.T) {
	allowed := map[string]bool{
		filepath.Join("cmd", "aeon", "blueprint.go"):               true, // the named offline verbs
		filepath.Join("internal", "daemon", "blueprint_source.go"): true, // install re-arm (Notes)
	}
	hits := walkRepoGo(t, allowed, func(line string) bool {
		return strings.Contains(line, "UseEmbeddedForOfflineTools(") && !strings.Contains(line, "func UseEmbeddedForOfflineTools")
	})
	if len(hits) > 0 {
		t.Fatalf("UseEmbeddedForOfflineTools reached beyond the named offline verbs and the installer:\n%s", strings.Join(hits, "\n"))
	}
	// And serve itself never mentions the embedded source.
	serve, err := os.ReadFile(filepath.Join("..", "..", "cmd", "aeon", "serve.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(serve), "\n") {
		if strings.Contains(line, "Embedded") && !strings.Contains(line, "//") {
			t.Fatalf("serve installs an embedded source: %q", line)
		}
	}
}

// walkRepoGo scans non-test .go files under internal/ and cmd/ (the aeon
// tree) and returns file:line hits where `interesting` matches, excluding
// the allowed files.
func walkRepoGo(t *testing.T, allowed map[string]bool, interesting func(string) bool) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var hits []string
	for _, sub := range []string{"internal", "cmd"} {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if allowed[rel] {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for i, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				if interesting(line) {
					hits = append(hits, rel+":"+strconv.Itoa(i+1)+": "+trimmed)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(hits)
	return hits
}
