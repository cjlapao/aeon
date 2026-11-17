package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
)

// blueprint_parity_test.go — S48 T11's guards. The claim under test is one
// sentence: `aeon blueprint` is a CLIENT of the daemon, and the offline road
// is the SAME code. Each direction gets its own test because the two drifts
// are different failures — a verb with no method is a capability the app and
// the agent cannot reach (P34), and a method with no verb is a door nobody
// built the CLI half of. The fixture (the spy door, the seeded store, the
// small readers) is in blueprint_parity_fixture_test.go.

func TestEveryBlueprintVerbHasASocketMethod(t *testing.T) {
	d := blueprintGuardDaemon(t)
	for verb, method := range blueprintVerbMethods() {
		m, ok := d.Table.Lookup(method)
		if !ok {
			t.Errorf("verb %q speaks %s, which is not registered — the CLI would be dialling a method the daemon never had", verb, method)
			continue
		}
		if !m.RequiresInitialize {
			t.Errorf("%s answers before Initialize — every blueprint door rides the negotiated connection", method)
		}
		if m.Debug {
			t.Errorf("%s is Debug — a verb a person uses cannot rest on a debug flag", method)
		}
	}
	// The compositions are compositions: every method they are built from
	// stands, and the composition itself is NOT a method (there is no
	// blueprint.edit to dial — the CLI calls show and save, nothing else).
	for verb, methods := range blueprintCompositionMethods() {
		for _, method := range methods {
			if _, ok := d.Table.Lookup(method); !ok {
				t.Errorf("verb %q is built from %s, which is not registered", verb, method)
			}
		}
		if _, ok := d.Table.Lookup(protocol.MethodName("blueprint." + verb)); ok {
			t.Errorf("blueprint.%s is a registered method — %q must stay a composition of the listed doors, not a third write path wearing a verb's name", verb, verb)
		}
	}
	// The retired verb has NO method and NO store read: `path` answers from a
	// sentence and nothing else.
	if _, ok := d.Table.Lookup(protocol.MethodName("blueprint.path")); ok {
		t.Error("blueprint.path is registered — the verb was retired, not promoted")
	}
	if src := blueprintFuncSource(t, "blueprint.go", "retiredBlueprintPath"); strings.Contains(src, "LegacyResolve(") || strings.Contains(src, "os.ReadFile") {
		t.Error("retiredBlueprintPath reads the filesystem or the mirror — the retirement must be a sentence, not a lookup")
	}
}

func TestEverySocketBlueprintMethodHasAVerb(t *testing.T) {
	d := blueprintGuardDaemon(t)
	spoken := map[protocol.MethodName]bool{}
	for _, m := range blueprintVerbMethods() {
		spoken[m] = true
	}
	for _, methods := range blueprintCompositionMethods() {
		for _, m := range methods {
			spoken[m] = true
		}
	}
	for _, m := range blueprintUnmethodedList() {
		spoken[m] = true
	}
	var orphans []string
	got := map[protocol.MethodName]bool{}
	for _, m := range d.Table.Methods() {
		if !strings.HasPrefix(string(m), "blueprint.") {
			continue
		}
		got[m] = true
		if !spoken[m] {
			orphans = append(orphans, string(m))
		}
	}
	if len(got) < 12 {
		t.Fatalf("the wired daemon answered %d blueprint methods — far too few to conclude anything", len(got))
	}
	if len(orphans) != 0 {
		sort.Strings(orphans)
		t.Errorf("blueprint methods with no CLI story and no named exception:\n  %s\nAdd the verb, name the composition, or name the exception in blueprintUnmethodedList with its reason.", strings.Join(orphans, "\n  "))
	}
}

// --- the one intentional behaviour change, listed --------------------------------------

// TestListNowIncludesCustomBlueprints pins the documented drift: the verb's
// rows are the DATABASE's rows (custom included), in the DAEMON's order —
// which is the second half of the same change, since a client that re-sorted
// would be a client with an opinion of its own.
func TestListNowIncludesCustomBlueprints(t *testing.T) {
	st := blueprintCLIStore(t)
	ctx := context.Background()
	custom := "id: parity-custom\nschemaVersion: 1\nversion: 1\nlanes:\n  - id: work\n    step: {kind: gate, question: go}\nquestions:\n  go:\n    prompt: go?\n    answers: [yes]\n"
	blueprintSeedRow(t, st, ctx, "parity-custom", custom, "user")
	total := len(blueprint.StandardNames()) + 2 // the fixture's own golden-custom

	out := blueprintVerb(t, "list")
	lines := strings.Fields(out)
	if len(lines) != total {
		t.Fatalf("list printed %d names, want the %d rows: %q", len(lines), total, out)
	}
	if !strings.Contains(out, "parity-custom") {
		t.Errorf("list dropped the custom row (BP13's whole point): %q", out)
	}
	if !blueprintSorted(lines) {
		t.Errorf("list is not in the daemon's order — the client re-sorted: %v", lines)
	}
	// The filters are the DAEMON's, never a client-side grep: what travels is
	// the flag, and the answer comes back filtered.
	if got := blueprintVerb(t, "list", "--origin", "user"); !strings.Contains(got, "parity-custom") || !strings.Contains(got, "golden-custom") {
		t.Errorf("--origin user dropped a user row: %q", got)
	}
	if strings.Contains(blueprintVerb(t, "list", "--origin", "system"), "parity-custom") {
		t.Error("--origin system included a user row")
	}
	if got := strings.Fields(blueprintVerb(t, "list", "--q", "parity")); len(got) != 1 || got[0] != "parity-custom" {
		t.Errorf("--q parity printed %v, want parity-custom alone", got)
	}
	var res protocol.BlueprintListResult
	if err := json.Unmarshal([]byte(blueprintVerb(t, "list", "--json")), &res); err != nil {
		t.Fatalf("list --json is not a BlueprintListResult: %v", err)
	}
	if len(res.Blueprints) != total || len(res.Names) != total {
		t.Fatalf("list --json carried %d summaries / %d names, want %d each", len(res.Blueprints), len(res.Names), total)
	}
}

// --- what did NOT change -----------------------------------------------------------------

// TestValidateAndFieldsOutputUnchanged re-runs T3's validate/fields commands
// and compares to the bytes recorded in the golden BEFORE this task — the
// spec's "unchanged semantics" claim, checked rather than asserted.
func TestValidateAndFieldsOutputUnchanged(t *testing.T) {
	golden := blueprintGoldenSections(t)
	blueprintCLIStore(t)
	cases := map[string][]string{
		"fields":               {"fields"},
		"validate-name":        {"validate", "two-step"},
		"validate-name-json":   {"validate", "--json", "two-step"},
		"validate-custom":      {"validate", "golden-custom"},
		"validate-strict":      {"validate", "--strict", "two-step"},
		"validate-mode-simple": {"validate", "--mode", "simple", "two-step"},
		"validate-bad-mode":    {"validate", "--mode", "nonsense", "two-step"},
		"validate-missing":     {"validate", "no-such-blueprint"},
	}
	for label, args := range cases {
		rec := blueprintGoldenRecord(t, golden, label)
		got, gotErr := blueprintRun(t, args...)
		if (gotErr != nil) != (rec.Error != "") {
			t.Errorf("%s changed its exit outcome (err=%v, golden error=%q)", label, gotErr, rec.Error)
		}
		if got != rec.Out {
			t.Errorf("%s drifted from the golden.\n--- want ---\n%s\n--- got ---\n%s", label, rec.Out, got)
		}
	}
}

// --- path -------------------------------------------------------------------------------

func TestPathIsRetiredWithTheNamedPointer(t *testing.T) {
	blueprintCLIStore(t)
	const sentence = "blueprints live in the database — use `aeon blueprint export` for a file copy or `aeon blueprint edit NAME` to change one"
	out, err := blueprintRun(t, "path", "two-step")
	if err == nil {
		t.Fatalf("`path` still answers (%q) — a path means nothing now", out)
	}
	if err.Error() != sentence {
		t.Errorf("the refusal is %q, want the named pointer exactly:\n%s", err.Error(), sentence)
	}
	if out != "" {
		t.Errorf("`path` printed %q on its way out — a retired verb prints nothing", out)
	}
	for _, name := range []string{"export", "edit"} {
		_, err := blueprintRun(t, "path", name)
		if err == nil || !strings.Contains(err.Error(), "it is a verb, not a blueprint") {
			t.Errorf("path %s refused with %v — the pointer must name the confusion too", name, err)
		}
	}
	if src := blueprintFileSource(t, "blueprint.go"); strings.Contains(src, "LegacyResolve(") || strings.Contains(src, "LegacyNamedPath(") {
		t.Error("cmd/aeon/blueprint.go still resolves the legacy mirror")
	}
}

// --- save / the write verbs --------------------------------------------------------------

func TestSaveSendsFileContentVerbatim(t *testing.T) {
	spy := &blueprintSpyDoor{saveRes: protocol.BlueprintSaveResult{Name: "draft", Changed: true, Revision: 3, Origin: "user"}}
	raw := []byte("id: draft\n# bytes a person wrote, odd trailing whitespace included   \nversion: 7\n\n\n")
	path := filepath.Join(t.TempDir(), "draft.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := blueprintRunDoor(t, spy, "save", "draft", "--file", path, "--note", "from the parity test"); err != nil {
		t.Fatalf("save refused: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0].method != "save" {
		t.Fatalf("the verbs spoke %v, want exactly one save", spy.calls)
	}
	if got := spy.calls[0].params["content"]; got != string(raw) {
		t.Errorf("the wire content was altered client-side:\n got: %q\nwant: %q", got, string(raw))
	}
	if got := spy.calls[0].params["name"]; got != "draft" {
		t.Errorf("name %q, want draft", got)
	}
	if got := spy.calls[0].params["note"]; got != "from the parity test" {
		t.Errorf("note %q did not travel", got)
	}
	spy.calls = nil
	if _, err := blueprintRunDoor(t, spy, "save", "draft"); err == nil || !strings.Contains(err.Error(), "--file") {
		t.Errorf("save without --file answered %v — the missing file is the one thing the CLI may refuse", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("save without --file still called the door %v — the refusal must come before the call", spy.calls)
	}
	spy.calls = nil
	if _, err := blueprintRunDoor(t, spy, "save", "draft", "--file", filepath.Join(t.TempDir(), "gone.yaml")); err == nil || len(spy.calls) != 0 {
		t.Errorf("save read a missing file and still spoke (%v, calls=%v)", err, spy.calls)
	}
}

func TestDeleteAndRevertNeedYes(t *testing.T) {
	spy := &blueprintSpyDoor{}
	for _, args := range [][]string{{"delete", "two-step"}, {"revert", "two-step", "--rev", "2"}} {
		spy.calls = nil
		if _, err := blueprintRunDoor(t, spy, args...); err == nil {
			t.Errorf("`aeon %s` succeeded without --yes — the seatbelt is decorative", strings.Join(args, " "))
			continue
		}
		if len(spy.calls) != 0 {
			t.Errorf("`aeon %s` reached the daemon (%v) without --yes — the refusal must be before the call", strings.Join(args, " "), spy.calls)
		}
	}
	spy.calls = nil
	spy.deleteRes = protocol.BlueprintDeleteResult{Deleted: true, LoopsStartedFromIt: 2, Note: "two loops started from it keep their pinned content"}
	out, err := blueprintRunDoor(t, spy, "delete", "two-step", "--yes")
	if err != nil {
		t.Fatalf("delete --yes refused: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0].method != "delete" {
		t.Errorf("delete --yes spoke %v, want one delete", spy.calls)
	}
	if !strings.Contains(out, "two loops started from it") {
		t.Errorf("delete dropped the daemon's note (T6's out-loud truth): %q", out)
	}
	spy.calls = nil
	if _, err := blueprintRunDoor(t, spy, "revert", "two-step", "--rev", "last", "--yes"); err == nil || len(spy.calls) != 0 {
		t.Errorf("--rev last answered %v (calls=%v) — a value that cannot be a number stops at flag parsing", err, spy.calls)
	}
}

func TestDiffPrintsTheDaemonsUnifiedDiffVerbatim(t *testing.T) {
	weird := "--- a/two-step\n+++ b/two-step\n@@ -1,3 +1,3 @@\n-id: two-step\n+id: two-step-copy\n\\ No newline at end of file"
	spy := &blueprintSpyDoor{diffRes: protocol.BlueprintDiffResult{Diff: weird, ALabel: "shipped", BLabel: "current"}}
	out, err := blueprintRunDoor(t, spy, "diff", "two-step")
	if err != nil {
		t.Fatalf("diff refused: %v", err)
	}
	if out != weird+"\n" {
		t.Errorf("the diff was not the daemon's bytes verbatim:\n got: %q\nwant: %q", out, weird+"\n")
	}
	if spy.calls[0].params["a"] != "shipped" || spy.calls[0].params["b"] != "current" {
		t.Errorf("the sides sent were %q/%q, want the documented defaults shipped/current", spy.calls[0].params["a"], spy.calls[0].params["b"])
	}
	// Equal bytes: stdout stays the (empty) diff so a piped diff is still a
	// valid empty diff; the explanation rides the side channel.
	spy.calls = nil
	spy.diffRes = protocol.BlueprintDiffResult{ALabel: "current", BLabel: "current", Equal: true}
	side := blueprintCaptureStderr(t, func() {
		var err error
		out, err = blueprintRunDoor(t, spy, "diff", "two-step", "--a", "current", "--b", "current")
		if err != nil {
			t.Errorf("an equal diff refused: %v", err)
		}
	})
	if out != "" {
		t.Errorf("an equal diff printed %q on stdout", out)
	}
	if !strings.Contains(side, "no difference") {
		t.Errorf("an equal diff said nothing on the side channel: %q", side)
	}
	if spy.calls[0].params["a"] != "current" {
		t.Errorf("--a current sent %q", spy.calls[0].params["a"])
	}
}

// --- edit -------------------------------------------------------------------------------

func TestEditSavesOnChangeKeepsTempOnInvalid(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	// The scripted $EDITOR: $1 is the file it must edit. S59 EV1 (2026-11-03)
	// gave the editor child the closed allowlist env, so the scripted outcome
	// is baked into the script per subtest rather than carried by a MODE
	// variable the allowlist strips — and no content ever rides a command
	// line, which is the claim this fixture was built to hold.
	writeEditor := func(mode string) {
		t.Helper()
		body := "true"
		switch mode {
		case "change":
			body = `printf 'id: two-step\nversion: 9\n' > "$1"`
		case "broken":
			body = `printf 'id: two-step\nlanes: []\n' > "$1"`
		}
		if err := os.WriteFile(editor, []byte("#!/bin/sh\nset -e\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeEditor("nothing")
	t.Setenv("VISUAL", editor)
	t.Setenv("EDITOR", editor)
	content := "id: two-step\nversion: 8\n"

	t.Run("changed and valid saves once and cleans up", func(t *testing.T) {
		writeEditor("change")
		spy := &blueprintSpyDoor{showRes: protocol.BlueprintShowResult{Name: "two-step", Content: content},
			saveRes: protocol.BlueprintSaveResult{Name: "two-step", Changed: true, Revision: 4, Origin: "system", Version: 1}}
		out, err := blueprintRunDoor(t, spy, "edit", "two-step", "--note", "typed in the editor")
		if err != nil {
			t.Fatalf("edit refused: %v (out %q)", err, out)
		}
		if len(spy.calls) != 2 || spy.calls[1].method != "save" {
			t.Fatalf("edit spoke %v, want show then save", spy.calls)
		}
		if got := spy.calls[1].params["content"]; got != "id: two-step\nversion: 9\n" {
			t.Errorf("the edited bytes sent were %q", got)
		}
		if spy.calls[1].params["note"] != "typed in the editor" {
			t.Errorf("the note did not ride the save: %q", spy.calls[1].params["note"])
		}
		first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
		path := strings.TrimSpace(strings.TrimPrefix(first, "edited and saved through "))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the scratch file %s survived a successful save", path)
		}
	})

	t.Run("changed and invalid keeps the file and names it", func(t *testing.T) {
		writeEditor("broken")
		spy := &blueprintSpyDoor{showRes: protocol.BlueprintShowResult{Name: "two-step", Content: content}}
		spy.saveErr = fmt.Errorf(`refused: blueprint_save_invalid · report {"state":"invalid","diagnostics":[{"path":"lanes","msg":"a blueprint needs at least one lane"}]}`)
		var err error
		side := blueprintCaptureStderr(t, func() {
			_, err = blueprintRunDoor(t, spy, "edit", "two-step")
		})
		if err == nil {
			t.Fatal("edit accepted an invalid edit")
		}
		if !strings.Contains(err.Error(), `"state":"invalid"`) {
			t.Errorf("the daemon's report did not travel verbatim: %v", err)
		}
		path := blueprintKeptPath(t, side)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the person's edit was thrown away: %v", err)
		}
		if string(raw) != "id: two-step\nlanes: []\n" {
			t.Errorf("the kept file holds %q, want the broken text as written", raw)
		}
	})

	t.Run("unchanged saves nothing and removes the file", func(t *testing.T) {
		writeEditor("nothing")
		spy := &blueprintSpyDoor{showRes: protocol.BlueprintShowResult{Name: "two-step", Content: content}}
		out, err := blueprintRunDoor(t, spy, "edit", "two-step")
		if err != nil {
			t.Fatalf("an unchanged edit refused: %v", err)
		}
		if len(spy.calls) != 1 || spy.calls[0].method != "show" {
			t.Fatalf("an unchanged edit spoke %v, want show alone", spy.calls)
		}
		if !strings.Contains(out, "no changes") || !strings.Contains(out, "removed") {
			t.Errorf("an unchanged edit said %q", out)
		}
	})
}

func TestEditRefusesWithoutEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	spy := &blueprintSpyDoor{showRes: protocol.BlueprintShowResult{Name: "two-step", Content: "id: two-step\n"}}
	_, err := blueprintRunDoor(t, spy, "edit", "two-step")
	if err == nil {
		t.Fatal("edit ran with no editor set — it would have hung or guessed at vi")
	}
	if !strings.Contains(err.Error(), "$VISUAL") || !strings.Contains(err.Error(), "$EDITOR") {
		t.Errorf("the refusal names neither variable: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Errorf("edit reached the daemon (%v) before it knew it had no editor", spy.calls)
	}
}

// --- export -------------------------------------------------------------------------------

func TestExportIsAMirrorNeverReadBack(t *testing.T) {
	st := blueprintCLIStore(t)
	out := t.TempDir()
	printed, err := blueprintRun(t, "export", "--out", out)
	if err != nil {
		t.Fatalf("export refused: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(printed), "\n")
	paths := lines[:len(lines)-1]
	if len(paths) < 2 {
		t.Fatalf("export printed %q — too few paths to mean anything", printed)
	}
	for _, path := range paths {
		if filepath.Dir(path) != out {
			t.Errorf("export wrote %s outside %s", path, out)
			continue
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v, want 0600", path, fi.Mode())
		}
		row, ok, err := st.BlueprintGet(context.Background(), strings.TrimSuffix(filepath.Base(path), ".yaml"))
		if err != nil || !ok {
			t.Fatalf("export printed a path for a row that is not there: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != row.Content {
			t.Errorf("%s is not the row's bytes verbatim", path)
		}
	}
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "disposable mirror") || !strings.Contains(footer, "never read back") {
		t.Errorf("export's footer does not say what the directory is: %q", footer)
	}
	// The claim's teeth: nothing in the export path READS. The function body
	// is inspected, so a future ReadFile inside it fails this test rather than
	// the reviewer's memory.
	body := blueprintFuncSource(t, "blueprint_verbs.go", "runBlueprintExport")
	for _, needle := range []string{"os.ReadFile", "os.ReadDir", "os.Open(", "filepath.Walk", "ioutil"} {
		if strings.Contains(body, needle) {
			t.Errorf("runBlueprintExport calls %s — the mirror is written and never read (a second source of truth starts with a second reader)", needle)
		}
	}
	// And the default destination stays a relative directory of the person's
	// choosing, never the state directory where the legacy mirror once lived.
	if !strings.Contains(blueprintFileSource(t, "blueprint_verbs.go"), `filepath.Join(".", "blueprints")`) {
		t.Error(`export's default --out moved — it must stay a relative "blueprints", not the state directory`)
	}
}

// --- the doors ----------------------------------------------------------------------------

func TestSelectBlueprintDoorFallsBackOnlyWhenNoDaemon(t *testing.T) {
	restore := blueprintDial
	t.Cleanup(func() { blueprintDial = restore })

	t.Run("no endpoint is the direct door, and it says the fixed line first", func(t *testing.T) {
		blueprintDial = func(int) (*protocol.Client, error) {
			return nil, fmt.Errorf("no daemon at /x/nothing.sock: dial unix: connect: no such file or directory")
		}
		var errOut strings.Builder
		d, err := selectBlueprintDoor(&errOut)
		if err != nil {
			t.Fatalf("a missing endpoint must fall back, not refuse: %v", err)
		}
		if _, ok := d.(*blueprintDirectDoor); !ok {
			t.Fatalf("the fallback door is %T, not the direct door", d)
		}
		if got := errOut.String(); got != blueprintDirectNotice+"\n" {
			t.Errorf("the fallback said %q, want the fixed line %q", got, blueprintDirectNotice+"\n")
		}
	})

	t.Run("a permission refusal REFUSES — it does not fall back", func(t *testing.T) {
		blueprintDial = func(int) (*protocol.Client, error) {
			return nil, &net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission, Addr: &net.UnixAddr{Name: "/x/aeon.sock", Net: "unix"}}
		}
		var errOut strings.Builder
		if _, err := selectBlueprintDoor(&errOut); err == nil {
			t.Fatal("a locked-out socket fell back — the CLI routed around a daemon that exists")
		} else if errOut.Len() != 0 {
			t.Errorf("a refusal printed the fallback notice: %q", errOut.String())
		}
	})

	t.Run("a handshake fault REFUSES — no fallback", func(t *testing.T) {
		blueprintDial = func(int) (*protocol.Client, error) {
			return nil, fmt.Errorf("handshake refused: daemon protocol version 9 is newer than this client's 8")
		}
		var errOut strings.Builder
		if _, err := selectBlueprintDoor(&errOut); err == nil {
			t.Fatal("version skew fell back")
		} else if errOut.Len() != 0 {
			t.Errorf("a refusal printed the fallback notice: %q", errOut.String())
		}
	})

	t.Run("a live daemon is the socket door, silent", func(t *testing.T) {
		f := startDaemon(t)
		// dial() ends in appenv.Apply(), which os.Setenvs the store paths
		// in-process; t.Setenv pins them for this test and restores after.
		t.Setenv("LOCALRPC_ENDPOINT_PATH", f.endpoint)
		t.Setenv("DATABASE_SQLITE_STORAGE_PATH", "")
		t.Setenv("DATABASE_SQLITE_FILE_NAME", "")
		t.Setenv("DATABASE_SQLITE_DSN", "")
		blueprintDial = dial
		var errOut strings.Builder
		d, err := selectBlueprintDoor(&errOut)
		if err != nil {
			t.Fatalf("the live daemon refused the dial: %v", err)
		}
		if _, ok := d.(*blueprintSocketDoor); !ok {
			t.Fatalf("the live road answered with %T, not the socket door", d)
		}
		if errOut.Len() != 0 {
			t.Errorf("the socket road said %q — it says nothing", errOut.String())
		}
	})
}

// TestBlueprintSocketAndDirectDoorsProduceTheSameOutput is the claim that the
// offline road is the SAME code. The spec's name for this test is S47's
// already — `TestSocketAndDirectDoorsProduceTheSameOutput` belongs to
// tools_parity_test.go in this package — so the blueprint one carries the
// plane's prefix (recorded in the task's Notes). the real binary, one verb, two roads, identical
// bytes (the fixed notice aside, which stripNotice removes — the blueprint
// door's notice is the SAME sentence the policy door's is).
//
// `lint` is deliberately NOT in the list: the direct door has no tools
// catalog wired, so lint says the explicit UNAVAILABLE there while the
// daemon's answers with advisories. That is V13's absence stated out loud on
// both sides, not a divergence to hide — and the parity harness forgives
// nothing, so the exclusion is written down instead of the comparison being
// loosened.
func TestBlueprintSocketAndDirectDoorsProduceTheSameOutput(t *testing.T) {
	b := newBothDoors(t)
	// S60 T7: the daemon applies the inert blueprints.landing set (land-feature,
	// resolve-conflict) at START, which the offline door never does — so which
	// list the socket road returns depended on whether that startup goroutine
	// had finished (a race that went one way on a fast box and the other under
	// load). Make it deterministic: let the daemon finish, then give the
	// offline store the same two records through the same seed door.
	b.f.waitStartupQuiet()
	b.must("offline", "seed", "apply", "--set", "blueprints.landing")
	// S61 T9 (2026-11-05): the daemon likewise applies the inert
	// blueprints.intake set (goal-intake) at start — same treatment, same
	// seed door, the same determinism for the interview's row.
	b.must("offline", "seed", "apply", "--set", "blueprints.intake")
	// The shipped rows' REVISION STATE is two databases' history, not a road
	// difference (the paragraph below): the daemon's boot path re-seeds them to
	// revision 2 / system:seed while the offline store's own seed leaves
	// revision 1 / system:blueprints.standard, and the timestamps are each
	// store's clock. This list passed only while it won a race against the
	// daemon's startup seed. Forgiven by name, for this test only — the names,
	// the ids and the hashes (the answers a road could actually get wrong) still
	// have to match byte for byte.
	guard := doorNormalizations
	t.Cleanup(func() { doorNormalizations = guard })
	for _, n := range []struct{ re, repl string }{
		{`"revision": [0-9]+`, `"revision": <REV>`},
		{`"updatedAt": "[^"]*"`, `"updatedAt": "<TIME>"`},
		{`"updatedBy": "[^"]*"`, `"updatedBy": "<WHO>"`},
	} {
		doorNormalizations = append(doorNormalizations, struct {
			re   *regexp.Regexp
			repl string
		}{regexp.MustCompile(n.re), n.repl})
	}
	// The two stores are NOT the same store, and the shipped rows do not
	// arrive identically: the daemon's boot path leaves `two-step` on revision
	// 2 (an op `update` row from the arrival seed) while a store the offline
	// door seeded itself is on revision 1. That is T4/T5's arrival net, not
	// this door's doing, and comparing it would be comparing two databases
	// rather than two roads. So the row-level comparison runs on a row BOTH
	// roads wrote themselves, from one file, and the shipped-name comparison
	// is limited to answers that carry no revision state (names, reports,
	// fields). The arrival net's double write is recorded in the task's Notes
	// as an observation, not silently routed around.
	draft := filepath.Join(t.TempDir(), "parity-door.yaml")
	if err := os.WriteFile(draft, []byte(blueprintParityContent), 0o600); err != nil {
		t.Fatal(err)
	}
	b.must("socket", "blueprint", "save", "parity-door", "--file", draft)
	b.must("offline", "blueprint", "save", "parity-door", "--file", draft)

	for _, args := range [][]string{
		{"blueprint", "list"},
		{"blueprint", "list", "--json"},
		{"blueprint", "list", "--origin", "system"},
		{"blueprint", "list", "--q", "parity"},
		{"blueprint", "fields"},
		{"blueprint", "fields", "--json"},
		{"blueprint", "validate", "two-step"},
		{"blueprint", "validate", "--json", "two-step"},
		{"blueprint", "show", "parity-door"},
		{"blueprint", "show", "parity-door", "--json"},
		{"blueprint", "validate", "parity-door"},
		{"blueprint", "usage", "parity-door"},
		{"blueprint", "usage", "parity-door", "--json"},
		{"blueprint", "history", "parity-door"},
		{"blueprint", "diff", "parity-door", "--a", "current", "--b", "current"},
		{"blueprint", "diff", "parity-door", "--a", "1", "--b", "current"},
	} {
		b.parity(false, args...)
	}
	for _, args := range [][]string{
		{"blueprint", "show", "no-such-blueprint"},
		{"blueprint", "usage", "no-such-blueprint"},
		{"blueprint", "history", "no-such-blueprint"},
		{"blueprint", "validate", "no-such-blueprint"},
		{"blueprint", "list", "--origin", "banana"},
		{"blueprint", "delete", "parity-door"},                               // needs --yes on both roads
		{"blueprint", "save", "parity-door", "--file", "/no/such/file.yaml"}, // the client-side read refusal
	} {
		b.parity(true, args...)
	}
	// And the destructive verb, once --yes is given, deletes on both roads and
	// says the same thing (the note is the daemon's own sentence).
	b.must("socket", "blueprint", "delete", "parity-door", "--yes")
	b.must("offline", "blueprint", "delete", "parity-door", "--yes")
	b.parity(false, "blueprint", "list", "--q", "parity") // gone from both roads
}

// blueprintParityContent is a complete, valid blueprint small enough to read
// at a glance — the row both doors write in the parity fixture.
const blueprintParityContent = `id: parity-door
schemaVersion: 1
version: 1
lanes:
  - id: work
    step: {kind: gate, question: go}
questions:
  go:
    prompt: go?
    answers: [yes]
`

// --- file input, which needs no store at all ----------------------------------------------

// TestFileInputNeedsNoStoreAtAll is the spec's line "`validate`/`lint --file`
// need no store at all": both are pointed at a state dir that CANNOT hold a
// store, and both answer. The last case is the canary that makes the first two
// mean something — the same broken dir must make a stored-name verb fail, or
// the two passes above prove nothing.
func TestFileInputNeedsNoStoreAtAll(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEON_STATE_DIR", blocked)
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(blocked, "nothing.sock"))

	file := filepath.Join(t.TempDir(), "draft.yaml")
	if err := os.WriteFile(file, []byte(blueprintParityContent), 0o600); err != nil {
		t.Fatal(err)
	}
	var got string
	var runErr error
	errs := blueprintCaptureStderr(t, func() {
		got, runErr = blueprintRun(t, "validate", "--file", file)
	})
	if runErr != nil || !strings.Contains(got, "parity-door schema 1 version 1") {
		t.Errorf("validate --file over an unopenable store answered %q, err %v", got, runErr)
	}
	if !strings.Contains(errs, blueprintOfflineFileNotice) {
		t.Errorf("validate --file did not say the offline-tools note (said %q)", errs)
	}
	if strings.Contains(errs, "applied directly to the store") {
		t.Errorf("validate --file announced a door it never used (said %q)", errs)
	}

	errs = blueprintCaptureStderr(t, func() {
		got, runErr = blueprintRun(t, "lint", "--file", file)
	})
	if runErr != nil || !strings.Contains(got, "tools catalog is not wired") {
		t.Errorf("lint --file answered %q, err %v — the offline answer is the daemon's own UNAVAILABLE, stated, not invented", got, runErr)
	}
	if strings.Contains(errs, "applied directly to the store") {
		t.Errorf("lint --file announced a store door it never opened (said %q)", errs)
	}

	if _, err := blueprintRun(t, "show", "two-step"); err == nil {
		t.Error("a store that cannot open answered a stored-name verb — the two passes above would prove nothing")
	}
}

// --- the one-implementation guards ---------------------------------------------------------

// TestBlueprintCLINeverImportsStoreOutsideDirectDoor keeps the offline door the
// ONLY place the blueprint CLI touches a store: a verb that opened the store
// itself would be a verb that reopened the file-era design.
func TestBlueprintCLINeverImportsStoreOutsideDirectDoor(t *testing.T) {
	files, err := filepath.Glob("blueprint*.go")
	if err != nil || len(files) < 4 {
		t.Fatalf("the glob saw %d blueprint files (%v) — the pattern is wrong, not the code", len(files), err)
	}
	// blueprint_client.go is the one door. blueprint_adopt.go is the NAMED
	// exception: S48 T9's file-ADOPTION tool (`aeon blueprint adopt`), whose
	// whole job is a store write of a file a person points at — its store use
	// is pre-T11, allowed by its own task's ruling, and carries its own guard
	// (TestAdoptWritesThroughTheOneWritePath). It is listed rather than the
	// glob narrowed, so the exception is visible and countable.
	allowed := map[string]bool{"blueprint_client.go": true, "blueprint_adopt.go": true}
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue // the tests may seed rows however they like
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value != `"github.com/cjlapao/aeon/internal/store"` {
				continue
			}
			if !allowed[filepath.Base(path)] {
				t.Errorf("%s imports internal/store — only the direct door may open a store; the verbs speak the door", filepath.Base(path))
			}
		}
	}
	if checked < 4 {
		t.Fatalf("only %d non-test blueprint CLI files were checked", checked)
	}
}

// TestDirectDoorUsesTheSameWriteService reads the CLI's blueprint files for a
// LOCAL copy of the write logic: any call into the store's blueprint writes,
// or any YAML serialization of a blueprint, is a third implementation of a
// judgment the daemon owns.
func TestDirectDoorUsesTheSameWriteService(t *testing.T) {
	needles := []string{
		"BlueprintPutTx(", "BlueprintDeleteTx(", ".BlueprintPut(", ".BlueprintDelete(",
		"BlueprintWithTx(", "yaml.Marshal", "yaml.Unmarshal",
	}
	for _, name := range []string{"blueprint.go", "blueprint_verbs.go", "blueprint_edit.go", "blueprint_client.go"} {
		src := blueprintFileSource(t, name)
		for _, n := range needles {
			if strings.Contains(src, n) {
				t.Errorf("cmd/aeon/%s calls %s — the write path is internal/daemon's, reached through daemon.NewBlueprintWriteDoors", name, n)
			}
		}
	}
	// And the direct door must actually go through it: the constructor calls
	// are the evidence, counted rather than remembered.
	client := blueprintFileSource(t, "blueprint_client.go")
	if got := strings.Count(client, "daemon.NewBlueprintWriteDoors("); got != 4 {
		t.Errorf("the direct door reaches the write service %d times, want 4 (save, duplicate, delete, revert)", got)
	}
	if got := strings.Count(client, "daemon.NewBlueprintReadDoors("); got < 5 {
		t.Errorf("the direct door reaches the read service %d times, want one arm per read door", got)
	}
	if !strings.Contains(client, "daemon.BlueprintValidateAnswer(") {
		t.Error("the direct door does not call the daemon's one validate answer")
	}
}
