package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/store"
)

// bpGoldenUpdate is this file's own rewrite switch (the S45 policy golden's
// `-golden-update` var lives in policy_golden_test.go; the flags coexist).
var bpGoldenUpdate = flag.Bool("bp-golden-update", false, "rewrite cmd/aeon/testdata/bpgolden from today's CLI output (Phase A ritual only)")

func bpGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	dir := filepath.Join("testdata", "bpgolden")
	path := filepath.Join(dir, name)
	if *bpGoldenUpdate {
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
		t.Fatalf("golden %s missing (%v) — generate it with -bp-golden-update on the PRE-change code and commit it", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden %s drifted.\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// blueprint_golden_test.go — S48 T3 Phase A: the CLI verbs' bytes are
// characterized too (list/fields/validate/path). `path` still resolves the
// LEGACY file resolver by design (retired only at T11) — its golden is the
// proof it keeps answering unchanged through T3; `list` prints the standard
// set today, and the one intentional T3 change (BP13: source names, custom
// included) is the documented drift this golden will catch and the task
// record names.

func TestBlueprintGoldenCLI(t *testing.T) {
	state := t.TempDir()
	t.Setenv("AEON_STATE_DIR", state)
	dir := filepath.Join(state, "blueprints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range blueprint.StandardNames() {
		raw, ok, err := blueprint.EmbeddedSource().Get(name)
		if err != nil || !ok {
			t.Fatalf("standard %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	custom := "id: golden-custom\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: go?\n    answers: [yes]\n"
	if err := os.WriteFile(filepath.Join(dir, "golden-custom.yaml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	// S48 T3: the verbs read rows — seed them from the same bytes (the
	// test-only loader mimicking T4), so Phase A vs Phase B is one compare
	// and the ONE listed drift (list includes custom, BP13) is visible.
	sv, err := openStoreDirectly(false)
	if err != nil {
		t.Fatal(err)
	}
	defer sv.Close()
	files := map[string]string{"golden-custom": custom}
	for _, name := range blueprint.StandardNames() {
		raw, _, _ := blueprint.EmbeddedSource().Get(name)
		files[name] = string(raw)
	}
	for name, content := range files {
		if _, err := blueprint.Load([]byte(content)); err != nil {
			continue // the adoption refusal
		}
		sum := sha256.Sum256([]byte(content))
		hash := hex.EncodeToString(sum[:])
		origin := "user"
		for _, std := range blueprint.StandardNames() {
			if std == name {
				origin = "system"
			}
		}
		row := store.BlueprintRow{Name: name, Version: 1, Content: content, ContentHash: hash, Origin: origin, UpdatedBy: "user:test"}
		rev := store.RevisionRow{Version: 1, Content: content, ContentHash: hash, Op: "adopt", ActorKind: "user", ActorID: "test"}
		if err := sv.BlueprintWithTx(context.Background(), func(tx *sql.Tx) error {
			return store.BlueprintPutTx(tx, row, rev, nil)
		}); err != nil {
			t.Fatal(err)
		}
	}

	var sb strings.Builder
	run := func(label string, args ...string) {
		var buf bytes.Buffer
		err := cmdBlueprint(&buf, args)
		rec := map[string]any{"out": strings.ReplaceAll(buf.String(), state, "<STATE>")}
		if err != nil {
			rec["error"] = strings.ReplaceAll(err.Error(), state, "<STATE>")
		}
		raw, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		sb.WriteString("### " + label + "\n")
		sb.Write(raw)
		sb.WriteString("\n")
	}
	run("list", "list")
	run("fields", "fields")
	run("path-two-step", "path", "two-step")
	run("path-custom", "path", "golden-custom")
	run("path-missing", "path", "no-such-blueprint")
	run("validate-name", "validate", "two-step")
	run("validate-name-json", "validate", "--json", "two-step")
	run("validate-custom", "validate", "golden-custom")
	run("validate-path-arg", "validate", filepath.Join(dir, "research-map.yaml"))
	run("validate-strict", "validate", "--strict", "two-step")
	run("validate-mode-simple", "validate", "--mode", "simple", "two-step")
	run("validate-bad-mode", "validate", "--mode", "nonsense", "two-step")
	run("validate-missing", "validate", "no-such-blueprint")
	run("unknown-verb", "nope")
	run("list-with-arg", "list", "extra")

	bpGolden(t, "cli.txt", []byte(sb.String()))
}
