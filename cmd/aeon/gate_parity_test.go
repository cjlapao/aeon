package main

// gate_parity_test.go — S57 T10 (VG20): the CLI↔socket parity table
// (T0 §4), the store-import guard, the no-logic-copy guard, the
// refusal-fold shape, and the generated-usage guard. Behaviour with a
// fake door lives in gate_verbs_test.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// gateVerbFixture is T0 §4's CLI↔socket parity table pinned as Go
// (docs/aeon/S57-verification-gates/tasks/T0.md:158-183): every CLI
// verb, mapped to the socket method it is a client of. The table and
// the protocol constants are cross-checked both ways below; a verb
// here without a method constant, or a method without a row, fails.
var gateVerbFixture = map[string]string{
	// verify — twelve verbs (the T0 §4 block).
	"verify list":           "verify.list",
	"verify show":           "verify.show",
	"verify add":            "verify.add",
	"verify update":         "verify.update",
	"verify archive":        "verify.archive",
	"verify restore":        "verify.restore",
	"verify apply-template": "verify.applyTemplate",
	"verify try":            "verify.try",
	"verify runs":           "verify.runs",
	"verify run-show":       "verify.runShow",
	"verify vocabulary":     "verify.vocabulary",
	"verify templates":      "verify.templates",
	// merge-rule — six verbs.
	"merge-rule get":        "mergerule.get",
	"merge-rule set":        "mergerule.set",
	"merge-rule clear":      "mergerule.clear",
	"merge-rule preview":    "mergerule.preview",
	"merge-rule changes":    "mergerule.changes",
	"merge-rule vocabulary": "mergerule.vocabulary",
	// dod — two verbs.
	"dod list": "dod.list",
	"dod mark": "dod.mark",
	// merge — two reads.
	"merge events":  "merge.events",
	"merge watches": "merge.watches",
}

// gateNonVerbs: the plane's socket methods that are DELIBERATELY not
// CLI verbs — the agent proposal twins and the agent mark (T10 spec:
// proposeAdd/proposeTighten/markAsAgent are explicit non-verbs; a
// person's door never proposes on their behalf, and an agent's mark
// has its own agent door, never the human `dod mark`).
var gateNonVerbs = map[string]string{
	"verify.proposeAdd":        "agent proposal — an agent asks, a person commits (aeon proposal)",
	"mergerule.proposeTighten": "agent proposal — tightening-only, same commit path",
	"dod.markAsAgent":          "the agent's own mark, its own door and its own limits",
}

// TestGateFixtureMatchesTheProtocolConstants: the fixture's values are
// exactly the protocol constants (compile-adjacent equality, verb by
// verb — a fixture that "drifted" would name the drifted row).
func TestGateFixtureMatchesTheProtocolConstants(t *testing.T) {
	constants := map[string]string{
		"verify.list":          string(protocol.MethodVerifyList),
		"verify.show":          string(protocol.MethodVerifyShow),
		"verify.add":           string(protocol.MethodVerifyAdd),
		"verify.update":        string(protocol.MethodVerifyUpdate),
		"verify.archive":       string(protocol.MethodVerifyArchive),
		"verify.restore":       string(protocol.MethodVerifyRestore),
		"verify.applyTemplate": string(protocol.MethodVerifyApplyTemplate),
		"verify.try":           string(protocol.MethodVerifyTry),
		"verify.runs":          string(protocol.MethodVerifyRuns),
		"verify.runShow":       string(protocol.MethodVerifyRunShow),
		"verify.vocabulary":    string(protocol.MethodVerifyVocabulary),
		"verify.templates":     string(protocol.MethodVerifyTemplates),
		"mergerule.get":        string(protocol.MethodMergeRuleGet),
		"mergerule.set":        string(protocol.MethodMergeRuleSet),
		"mergerule.clear":      string(protocol.MethodMergeRuleClear),
		"mergerule.preview":    string(protocol.MethodMergeRulePreview),
		"mergerule.changes":    string(protocol.MethodMergeRuleChanges),
		"mergerule.vocabulary": string(protocol.MethodMergeRuleVocabulary),
		"dod.list":             string(protocol.MethodDoDList),
		"dod.mark":             string(protocol.MethodDoDMark),
		"merge.events":         string(protocol.MethodMergeEvents),
		"merge.watches":        string(protocol.MethodMergeWatches),
	}
	if len(gateVerbFixture) != 22 {
		t.Fatalf("the fixture is %d rows — T0 §4 is 22", len(gateVerbFixture))
	}
	for verb, method := range gateVerbFixture {
		want, ok := constants[method]
		if !ok {
			t.Errorf("verb %q names %q, which is not one of the plane's 22 method constants", verb, method)
			continue
		}
		if want != method {
			t.Errorf("fixture says %q → %q, the constant says %q", verb, method, want)
		}
		delete(constants, method)
	}
	for method := range constants {
		t.Errorf("method %q has no CLI row in the fixture — a socket method the person's terminal cannot reach (or a new non-verb to declare)", method)
	}
	for m := range gateNonVerbs {
		for _, v := range gateVerbFixture {
			if v == m {
				t.Errorf("%s is declared a non-verb yet also has a CLI row — pick one", m)
			}
		}
	}
}

// TestEveryGateVerbIsDispatched: every fixture verb reaches door
// selection through its command family's dispatch (the permission
// sentinel proves "the verb exists and asked for a door"), and NO
// non-verb is dispatchable as a CLI verb (the spec's explicit
// non-verbs must answer "unknown verb").
func TestEveryGateVerbIsDispatched(t *testing.T) {
	sentinel := fmt.Errorf("dial: %w", &permissionFault{})
	oldDial := gateDial
	t.Cleanup(func() { gateDial = oldDial })
	gateDial = func(int) (*protocol.Client, error) { return nil, sentinel }
	gateStderr(t)

	calls := map[string][]string{
		"verify list":           {"verify", "list"},
		"verify show":           {"verify", "show", "g1"},
		"verify add":            {"verify", "add", "--component", "c", "--name", "n", "--command", "true", "--requirement", "required"},
		"verify update":         {"verify", "update", "g1", "--timeout", "1"},
		"verify archive":        {"verify", "archive", "g1", "--yes"},
		"verify restore":        {"verify", "restore", "g1"},
		"verify apply-template": {"verify", "apply-template", "--component", "c", "--template", "go/build"},
		"verify try":            {"verify", "try", "g1"},
		"verify runs":           {"verify", "runs"},
		"verify run-show":       {"verify", "run-show", "r1"},
		"verify vocabulary":     {"verify", "vocabulary"},
		"verify templates":      {"verify", "templates"},
		"merge-rule get":        {"merge-rule", "get", "--component", "c"},
		"merge-rule set":        {"merge-rule", "set", "--component", "c", "--yes"},
		"merge-rule clear":      {"merge-rule", "clear", "--component", "c", "--yes"},
		"merge-rule preview":    {"merge-rule", "preview", "--component", "c"},
		"merge-rule changes":    {"merge-rule", "changes"},
		"merge-rule vocabulary": {"merge-rule", "vocabulary"},
		"dod list":              {"dod", "list", "--loop", "L"},
		"dod mark":              {"dod", "mark", "i1", "--state", "unmet"},
		"merge events":          {"merge", "events"},
		"merge watches":         {"merge", "watches"},
	}
	for verb, a := range calls {
		t.Run(verb, func(t *testing.T) {
			var out strings.Builder
			var err error
			switch a[0] {
			case "verify":
				err = cmdVerify(&out, a[1:])
			case "merge-rule":
				err = cmdMergeRule(&out, a[1:])
			case "dod":
				err = cmdDoD(&out, a[1:])
			case "merge":
				err = cmdMerge(&out, a[1:])
			}
			if err == nil {
				t.Fatal("the sentinel never reached — the verb does not ask for a door")
			}
			if _, ok := err.(*usageError); ok && strings.Contains(err.Error(), "unknown verb") {
				t.Fatalf("the verb is not dispatched: %v", err)
			}
			if !strings.Contains(err.Error(), "permission") {
				t.Errorf("the verb answered %v — want the dial sentinel (it reached door selection)", err)
			}
		})
	}

	// the non-verbs must NOT be dispatchable as CLI verbs.
	for _, a := range [][]string{
		{"verify", "proposeAdd"},
		{"merge-rule", "proposeTighten"},
		{"dod", "markAsAgent"},
	} {
		var err error
		switch a[0] {
		case "verify":
			err = cmdVerify(&strings.Builder{}, []string{a[1]})
		case "merge-rule":
			err = cmdMergeRule(&strings.Builder{}, []string{a[1]})
		case "dod":
			err = cmdDoD(&strings.Builder{}, []string{a[1]})
		}
		if err == nil || !strings.Contains(err.Error(), "unknown verb") {
			t.Errorf("%s.%s answered %v — a declared non-verb must answer \"unknown verb\"", a[0], a[1], err)
		}
	}
}

// permissionFault is the "a daemon is there and said no" fault shape:
// selectGateDoor must pass it through, never fall back.
type permissionFault struct{}

func (*permissionFault) Error() string { return "permission denied" }

// TestGateVerbFilesImportNoStoreAndNoDaemon: the four verb files speak
// only through the door interface; `internal/store` and
// `internal/daemon` may appear in gate_client.go ONLY (the direct
// door's one home). Parsed from the import blocks, not grepped.
func TestGateVerbFilesImportNoStoreAndNoDaemon(t *testing.T) {
	verbs := []string{"verify_verbs.go", "mergerule_verbs.go", "dod_verbs.go", "merge_verbs.go"}
	for _, name := range verbs {
		f := parseGateFile(t, name)
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(path, "/internal/store") || strings.HasSuffix(path, "/internal/daemon") {
				t.Errorf("%s imports %s — the verbs ride the door, never the store or the service (P34)", name, path)
			}
		}
	}
	// and the store import in the package's gate files lives exactly
	// where it may: gate_client.go.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var storeFiles []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		switch e.Name() {
		case "verify_verbs.go", "mergerule_verbs.go", "dod_verbs.go", "merge_verbs.go", "gate_client.go":
		default:
			continue // this guard owns only the gate-plane files
		}
		f := parseGateFile(t, e.Name())
		for _, imp := range f.Imports {
			if strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), "/internal/store") {
				storeFiles = append(storeFiles, e.Name())
			}
		}
	}
	if len(storeFiles) != 1 || storeFiles[0] != "gate_client.go" {
		t.Errorf("the gate-plane files importing internal/store = %v — want exactly [gate_client.go]", storeFiles)
	}
}

func parseGateFile(t *testing.T, name string) *ast.File {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	f, perr := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if perr != nil {
		t.Fatalf("parse %s: %v", name, perr)
	}
	return f
}

// TestDirectDoorHasNoLogicCopy: the spec-named go/parser guard (the
// autonomy precedent's shape): the direct door's constructor builds the
// daemon's OWN three services over the opened store, every direct
// method speaks its service through exactly one seam call, and the
// verb files never call a store or service method themselves.
func TestDirectDoorHasNoLogicCopy(t *testing.T) {
	f := parseGateFile(t, "gate_client.go")
	var ctor *ast.FuncDecl
	doorMethods := 0
	wantSeam := map[string]string{
		"VerifyList": "List", "VerifyShow": "Show", "VerifyRuns": "Runs", "VerifyRunShow": "RunShow",
		"VerifyVocabulary": "Vocabulary", "VerifyTemplates": "Templates", "VerifyAdd": "Add",
		"VerifyUpdate": "Update", "VerifyArchive": "Archive", "VerifyRestore": "Restore",
		"VerifyApplyTemplate": "ApplyTemplate",
		"MergeRuleGet":        "Get", "MergeRulePreview": "Preview", "MergeRuleChanges": "Changes",
		"MergeRuleVocabulary": "Vocabulary", "MergeRuleSet": "Set", "MergeRuleClear": "Clear",
		"MergeEvents": "Events", "MergeWatches": "Watches",
		"DoDList": "List", "DoDMark": "Mark",
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv == nil && fd.Name.Name == "openGateDirectDoor" {
			ctor = fd
			continue
		}
		if fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		rt := fd.Recv.List[0].Type
		if st, ok := rt.(*ast.StarExpr); ok {
			rt = st.X
		}
		id, ok := rt.(*ast.Ident)
		if !ok || id.Name != "gateDirectDoor" || fd.Name.Name == "Close" {
			continue
		}
		doorMethods++
		if fd.Name.Name == "VerifyTry" {
			// the one method with NO seam call: it answers the fixed
			// offline refusal (proven behaviourally in
			// TestVerifyTryRefusesOffline).
			continue
		}
		want, known := wantSeam[fd.Name.Name]
		if !known {
			t.Errorf("the direct door carries %q, which is not a seam method", fd.Name.Name)
			continue
		}
		var seam []string
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			se, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if inner, ok := se.X.(*ast.SelectorExpr); ok {
				if x, ok := inner.X.(*ast.Ident); ok && x.Name == "d" {
					seam = append(seam, inner.Sel.Name+"."+se.Sel.Name)
				}
			}
			return true
		})
		servicePrefix := ""
		switch fd.Name.Name[:1] {
		case "M":
			if strings.HasPrefix(fd.Name.Name, "MergeRule") {
				servicePrefix = "rule."
			} else {
				servicePrefix = "rule."
			}
		case "D":
			servicePrefix = "dod."
		default:
			servicePrefix = "verify."
		}
		if len(seam) != 1 || seam[0] != servicePrefix+want {
			t.Errorf("%s speaks the services as %v — want exactly d.%s%s (one engine, two doors)", fd.Name.Name, seam, servicePrefix, want)
		}
	}
	if doorMethods != 22 {
		t.Errorf("the direct door carries %d methods — the closed set is 22", doorMethods)
	}
	if ctor == nil {
		t.Fatal("openGateDirectDoor is gone — the constructor moved or was renamed")
	}
	ctorCalls := map[string]bool{}
	ast.Inspect(ctor.Body, func(n ast.Node) bool {
		if ce, ok := n.(*ast.CallExpr); ok {
			if se, ok := ce.Fun.(*ast.SelectorExpr); ok {
				if x, ok := se.X.(*ast.Ident); ok {
					ctorCalls[x.Name+"."+se.Sel.Name] = true
				}
			}
			if id, ok := ce.Fun.(*ast.Ident); ok {
				ctorCalls[id.Name] = true
			}
		}
		return true
	})
	for _, want := range []string{"openStoreSkipMigrations", "daemon.NewVerifyService", "daemon.NewMergeRuleService", "daemon.NewDoDService"} {
		if !ctorCalls[want] {
			t.Errorf("the constructor does not call %s — the direct door no longer builds the real services (it IS the services, not a copy)", want)
		}
	}
}

// TestGateRefusalFoldsMatchTheWireShape: the CLI's folds use the SAME
// exported error builders and the SAME pinned sentences the socket
// handlers fold with (verifyRefusedError et al. are package-private in
// protocol; their format strings are pinned here beside the builders —
// drift in either direction fails one test or the other).
func TestGateRefusalFoldsMatchTheWireShape(t *testing.T) {
	cases := []struct {
		name string
		got  error
		want *protocol.Error
	}{
		{"verify add", gateVerifyRefused("add", &protocol.VerifyRefusal{Kind: "name-taken", Detail: "d"}),
			folded(protocol.ErrVerifyRefused.WithWhat("verify %s refused: %s", "add", "d"), "name-taken")},
		{"merge rule set", gateMergeRuleRefused("set", &protocol.MergeRuleRefusal{Kind: "revision", Detail: "d2"}),
			folded(protocol.ErrMergeRuleRefused.WithWhat("merge rule %s refused: %s", "set", "d2"), "revision")},
		{"dod mark", gateDoDRefused("mark", &protocol.DoDRefusal{Kind: "evidence", Detail: "d3"}),
			folded(protocol.ErrDodRefused.WithWhat("dod %s refused: %s", "mark", "d3"), "evidence")},
	}
	for _, c := range cases {
		var got *protocol.Error
		if !errors.As(c.got, &got) {
			t.Fatalf("%s: the fold does not return a *protocol.Error: %v", c.name, c.got)
		}
		gb, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		wb, err := json.Marshal(c.want)
		if err != nil {
			t.Fatal(err)
		}
		if string(gb) != string(wb) {
			t.Errorf("%s: the CLI fold drifted from the wire shape:\n cli:  %s\n want: %s", c.name, gb, wb)
		}
		if got.Data.Kind == "" {
			t.Errorf("%s: the fold lost Data.Kind — the kind is how the app routes the refusal", c.name)
		}
	}
}

// folded stamps the refusal's Kind onto the builder's error the way the
// socket handlers' folds do (verifyRefusedError sets Data.Kind; the
// builder call above pins the sentence, this pins the kind).
func folded(e *protocol.Error, kind string) *protocol.Error {
	e.Data.Kind = kind
	return e
}

// TestHelpCarriesTheVocabularySetsBothWays: the usage strings' closed
// sets are BUILT from the shared Go constants (the same values
// *.vocabulary answers over), and the live vocabulary answer contains
// the same words — a set typed twice (typed a third time) fails.
func TestHelpCarriesTheVocabularySetsBothWays(t *testing.T) {
	var out strings.Builder
	printVerifyUsage(&out)
	text := out.String()
	for _, want := range []string{
		strings.Join(protocol.VerifyRequirementValues, " | "),
		strings.Join(protocol.VerifyStateValues, " | "),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the verify usage lost the set %q — usage must be built from the shared constants", want)
		}
	}
	out.Reset()
	printMergeRuleUsage(&out)
	text = out.String()
	for _, want := range []string{
		strings.Join(mergeruleRequireDodWords(), " | "),
		strings.Join(mergeruleChecksPolicyWords(), " | "),
		strings.Join(mergeruleNoChecksWords(), " | "),
		strings.Join(mergeruleReviewModeWords(), " | "),
		strings.Join(mergeruleMethodWords(), " | "),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the merge-rule usage lost the set %q", want)
		}
	}
	out.Reset()
	printDoDUsage(&out)
	if !strings.Contains(out.String(), strings.Join(protocol.DoDMarkStateValues, " | ")) {
		t.Error("the dod usage lost the mark's closed set")
	}
}
